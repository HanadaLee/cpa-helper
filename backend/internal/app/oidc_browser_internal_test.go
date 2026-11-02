package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"cpa-helper/backend/internal/security"
)

func assertOIDCErrorPage(t *testing.T, rec *httptest.ResponseRecorder, message string) {
	t.Helper()
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" || rec.Header().Get("Location") != "" {
		t.Fatalf("error is not a browser page: %v", rec.Header())
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("OIDC error page may be cached or leak callback parameters")
	}
	for _, text := range []string{`content="3;url=/login?skipsso=true"`, `href="/login?skipsso=true"`, "3 秒后自动跳转到登录页面", message} {
		if !strings.Contains(rec.Body.String(), text) {
			t.Fatalf("missing %q: %s", text, rec.Body.String())
		}
	}
}

func oidcTestSilentRestart(t *testing.T, p *oidcTestProvider, rec *httptest.ResponseRecorder) (*url.URL, []*http.Cookie) {
	t.Helper()
	target, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || rec.Code != http.StatusFound || target.Scheme+"://"+target.Host != p.server.URL || target.Path != "/authorize" {
		t.Fatalf("no fresh authorization: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	q := target.Query()
	if q.Get("prompt") != "none" || !strings.HasPrefix(q.Get("state"), oidcSilentStatePrefix) || q.Get("nonce") == "" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("invalid silent authorization: %v", q)
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == security.SessionCookieName() && cookie.MaxAge > 0 {
			t.Fatal("invalid callback authenticated without fresh verification")
		}
	}
	p.mu.Lock()
	p.nonce, p.challenge = q.Get("nonce"), q.Get("code_challenge")
	p.mu.Unlock()
	return target, rec.Result().Cookies()
}

func TestOIDCExpiredFlowRecoversSilently(t *testing.T) {
	a := newQuotaTestApp(t)
	p := newOIDCTestProvider(t)
	oidcTestConfig(t, a, p)
	target, cookies := oidcTestStart(t, a, p, "/account/profile")
	if _, err := a.db.Exec("UPDATE oidc_flows SET expires_at = 1"); err != nil {
		t.Fatal(err)
	}
	retry, retryCookies := oidcTestSilentRestart(t, p, oidcTestCallback(t, a, target, cookies))
	for _, name := range []string{"state", "nonce", "code_challenge"} {
		if retry.Query().Get(name) == target.Query().Get(name) {
			t.Fatalf("reused expired %s", name)
		}
	}
	callback := oidcTestCallback(t, a, retry, retryCookies)
	if callback.Code != http.StatusFound || callback.Header().Get("Location") != "/" {
		t.Fatalf("silent login failed: %d %s", callback.Code, callback.Body.String())
	}
	if me := oidcTestRequest(t, a, "GET", "/api/auth/me", nil, callback.Result().Cookies()); me.Code != http.StatusOK {
		t.Fatalf("silent login did not establish session: %d %s", me.Code, me.Body.String())
	}
	// A user refreshing an old callback after success keeps their authenticated session.
	refreshed := oidcTestCallback(t, a, target, callback.Result().Cookies())
	if refreshed.Code != http.StatusFound || refreshed.Header().Get("Location") != "/" {
		t.Fatalf("existing session was interrupted: %d %s", refreshed.Code, refreshed.Body.String())
	}
	// Even a replay of the silent callback cannot consume its code twice.
	replay := oidcTestCallback(t, a, retry, retryCookies)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("silent replay accepted: %d", replay.Code)
	}
	assertOIDCErrorPage(t, replay, errOIDCFlowInvalid.Message)
}

func TestOIDCSilentRecoveryStopsWhenCookiesAreMissing(t *testing.T) {
	a := newQuotaTestApp(t)
	p := newOIDCTestProvider(t)
	oidcTestConfig(t, a, p)
	retry, _ := oidcTestSilentRestart(t, p, oidcTestRequest(t, a, "GET", "/oidc/callback?state=stale&code=old-code", nil, nil))
	rec := oidcTestCallback(t, a, retry, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("silent recovery looped: %d", rec.Code)
	}
	assertOIDCErrorPage(t, rec, errOIDCFlowInvalid.Message)
}

func TestOIDCSilentRecoveryRequiresInteraction(t *testing.T) {
	a := newQuotaTestApp(t)
	p := newOIDCTestProvider(t)
	oidcTestConfig(t, a, p)
	retry, cookies := oidcTestSilentRestart(t, p, oidcTestRequest(t, a, "GET", "/oidc/callback", nil, nil))
	rec := oidcTestRequest(t, a, "GET", "/oidc/callback?state="+url.QueryEscape(retry.Query().Get("state"))+"&error=login_required&error_description=SECRET-UPSTREAM-DETAIL", nil, cookies)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("interaction failure: %d", rec.Code)
	}
	assertOIDCErrorPage(t, rec, "身份服务登录会话已过期，需要重新登录")
	if strings.Contains(rec.Body.String(), "SECRET-UPSTREAM-DETAIL") {
		t.Fatal("provider error description leaked")
	}
}

func TestOIDCBrowserErrorPaths(t *testing.T) {
	a := newQuotaTestApp(t)
	for _, path := range []string{"/oidc/login", "/oidc/callback", "/oidc/logged-out"} {
		rec := oidcTestRequest(t, a, "GET", path, nil, nil)
		assertOIDCErrorPage(t, rec, "")
	}
	// API authentication failures retain their existing JSON contract.
	api := oidcTestRequest(t, a, "GET", "/api/auth/me", nil, nil)
	if api.Code != http.StatusUnauthorized || !strings.HasPrefix(api.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API error changed: %d %s", api.Code, api.Body.String())
	}
	if err := a.db.Close(); err != nil {
		t.Fatal(err)
	}
	rec := oidcTestRequest(t, a, "GET", "/oidc/callback", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("database error: %d", rec.Code)
	}
	assertOIDCErrorPage(t, rec, "服务器暂时无法完成 OIDC 认证，请稍后重试")
	if strings.Contains(rec.Body.String(), "database is closed") {
		t.Fatal("internal database error leaked")
	}
}

func TestOIDCErrorPageEscapesMessages(t *testing.T) {
	r := httptest.NewRequest("GET", "/oidc/callback?code=SECRET-CODE", nil)
	rec := httptest.NewRecorder()
	writeOIDCErrorPage(rec, r, authenticationError(`<script>alert("test")</script>`))
	assertOIDCErrorPage(t, rec, "&lt;script&gt;")
	if strings.Contains(rec.Body.String(), "<script>") || strings.Contains(rec.Body.String(), "SECRET-CODE") {
		t.Fatal("unsafe callback data in error page")
	}
	rec = httptest.NewRecorder()
	writeOIDCErrorPage(rec, r, errors.New("PRIVATE-DATABASE-ERROR"))
	assertOIDCErrorPage(t, rec, "服务器暂时无法完成 OIDC 认证，请稍后重试")
	if strings.Contains(rec.Body.String(), "PRIVATE-DATABASE-ERROR") {
		t.Fatal("internal error leaked")
	}
}
