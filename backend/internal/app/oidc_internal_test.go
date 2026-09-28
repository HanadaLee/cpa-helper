package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-helper/backend/internal/security"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type oidcTestProvider struct {
	server           *httptest.Server
	key              *rsa.PrivateKey
	mu               sync.Mutex
	nonce, challenge string
	claims           map[string]any
	userInfo         map[string]any
	badSignature     bool
	noLogout         bool
}

func newOIDCTestProvider(t *testing.T) *oidcTestProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &oidcTestProvider{key: key, claims: map[string]any{
		"sub": "subject-one", "preferred_username": "sso-member",
		"name": "SSO Member", "email": "member@example.test", "picture": "https://example.test/avatar.png",
	}}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			data := map[string]any{
				"issuer": p.server.URL, "authorization_endpoint": p.server.URL + "/authorize?tenant=shared",
				"token_endpoint": p.server.URL + "/token", "jwks_uri": p.server.URL + "/keys",
				"userinfo_endpoint": p.server.URL + "/userinfo", "id_token_signing_alg_values_supported": []string{"RS256"},
			}
			if !p.noLogout {
				data["end_session_endpoint"] = p.server.URL + "/logout?tenant=shared"
			}
			writeJSON(w, 200, data)
		case "/keys":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			_ = r.ParseForm()
			hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(hash[:]) != p.challenge {
				t.Error("incorrect PKCE verifier")
				http.Error(w, "PKCE", 400)
				return
			}
			if r.Form.Get("redirect_uri") != "https://helper.example.test/oidc/callback" {
				t.Error("incorrect redirect URI")
			}
			id, secret, _ := r.BasicAuth()
			if id != "helper" || secret != "test-client-secret" {
				t.Errorf("missing confidential client authentication")
				http.Error(w, "client", 401)
				return
			}
			claims := map[string]any{"iss": p.server.URL, "aud": "helper", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": p.nonce}
			for key, value := range p.claims {
				claims[key] = value
			}
			signingKey := p.key
			if p.badSignature {
				signingKey, _ = rsa.GenerateKey(rand.Reader, 2048)
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signingKey}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			token, err := jwt.Signed(signer).Claims(claims).Serialize()
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			writeJSON(w, 200, map[string]any{"access_token": "test-access", "token_type": "Bearer", "id_token": token})
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer test-access" {
				t.Error("missing userinfo access token")
			}
			if p.userInfo == nil {
				http.Error(w, "unavailable", 503)
				return
			}
			writeJSON(w, 200, p.userInfo)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.server.Close)
	return p
}

func oidcTestConfig(t *testing.T, a *App, p *oidcTestProvider) AppConfig {
	t.Helper()
	cfg, err := a.loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg.OIDC = OIDCConfig{Enabled: true, Issuer: p.server.URL, ClientID: "helper", ClientSecret: "test-client-secret",
		PublicURL: "https://helper.example.test", UsernameClaim: "preferred_username", AutoCreateUsers: true}
	cfg.CAS = CASConfig{Enabled: true, AutoCreateUsers: true, BaseURL: "https://cas.example.test", PublicURL: "https://helper.example.test"}
	cfg.NewUserQuota = NewUserQuotaConfig{DailyUSD: 10, WeeklyUSD: 40}
	if err := a.saveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func oidcTestRequest(t *testing.T, a *App, method, target string, payload any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, target, &body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	a.Routes().ServeHTTP(rec, req)
	return rec
}

func oidcTestStart(t *testing.T, a *App, p *oidcTestProvider, returnTo string) (*url.URL, []*http.Cookie) {
	t.Helper()
	start := oidcTestRequest(t, a, "GET", "/oidc/login?returnTo="+url.QueryEscape(returnTo), nil, nil)
	if start.Code != 302 {
		t.Fatalf("start: %d %s", start.Code, start.Body.String())
	}
	target, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := target.Query()
	if q.Get("tenant") != "shared" {
		t.Fatal("provider authorization endpoint query lost")
	}
	if q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge_method") != "S256" || q.Get("scope") != "openid profile email" {
		t.Fatalf("invalid auth request: %v", q)
	}
	p.mu.Lock()
	p.nonce, p.challenge = q.Get("nonce"), q.Get("code_challenge")
	p.mu.Unlock()
	cookies := start.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("insecure flow cookie: %+v", cookies)
	}
	return target, cookies
}

func oidcTestCallback(t *testing.T, a *App, target *url.URL, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return oidcTestRequest(t, a, "GET", "/oidc/callback?code=test-code&state="+url.QueryEscape(target.Query().Get("state")), nil, cookies)
}

func TestOIDCSharedCASIdentityAndLogout(t *testing.T) {
	a := newQuotaTestApp(t)
	p := newOIDCTestProvider(t)
	cfg := oidcTestConfig(t, a, p)
	ctx := context.Background()
	casUser, err := a.resolveCASUser(ctx, cfg, casProfile{Username: "sso-member", DisplayName: "Original nickname"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE users SET is_admin = 1, quota_day_used_usd = 3, quota_day = ? WHERE id = ?", time.Now().In(appTimeLocation).Format("2006-01-02"), casUser.ID); err != nil {
		t.Fatal(err)
	}
	seedQuotaTestAPIKey(t, a, casUser.ID, "sk-existing")
	seedQuotaCard(t, a, casUser.ID, "credit", 25, nil)

	target, flowCookies := oidcTestStart(t, a, p, "/account/quota")
	callback := oidcTestCallback(t, a, target, flowCookies)
	if callback.Code != 302 || callback.Header().Get("Location") != "/account/quota" {
		t.Fatalf("callback: %d %s", callback.Code, callback.Body.String())
	}
	sessionCookies := callback.Result().Cookies()
	me := oidcTestRequest(t, a, "GET", "/api/auth/me", nil, sessionCookies)
	var user AuthUser
	if err := json.Unmarshal(me.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if me.Code != 200 || user.ID != casUser.ID || !user.IsAdmin || !user.CASBound || !user.OIDCBound || user.Nickname != "Original nickname" || user.Avatar != "https://example.test/avatar.png" {
		t.Fatalf("identity changed: %s", me.Body.String())
	}
	var users, cards, keys int
	var dailyUsed float64
	_ = a.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&users)
	_ = a.db.QueryRow("SELECT COUNT(*) FROM quota_cards WHERE user_id = ?", user.ID).Scan(&cards)
	_ = a.db.QueryRow("SELECT COUNT(*) FROM user_api_keys WHERE user_id = ?", user.ID).Scan(&keys)
	_ = a.db.QueryRow("SELECT quota_day_used_usd FROM users WHERE id = ?", user.ID).Scan(&dailyUsed)
	if users != 1 || cards != 1 || keys != 1 || dailyUsed != 3 {
		t.Fatalf("shared data lost: %d %d %d %v", users, cards, keys, dailyUsed)
	}
	if retry := oidcTestCallback(t, a, target, flowCookies); retry.Code != 401 {
		t.Fatalf("replay accepted: %d", retry.Code)
	}

	// A renamed OIDC claim cannot change the stable identity or steal another user.
	p.mu.Lock()
	p.claims["preferred_username"] = "renamed"
	p.mu.Unlock()
	target, flowCookies = oidcTestStart(t, a, p, "/")
	again := oidcTestCallback(t, a, target, flowCookies)
	if again.Code != 302 {
		t.Fatalf("renamed identity rejected: %s", again.Body.String())
	}
	meAgain := oidcTestRequest(t, a, "GET", "/api/auth/me", nil, again.Result().Cookies())
	var sameUser AuthUser
	_ = json.Unmarshal(meAgain.Body.Bytes(), &sameUser)
	if sameUser.ID != user.ID || sameUser.Username != user.Username {
		t.Fatalf("unstable identity: %+v", sameUser)
	}

	logout := oidcTestRequest(t, a, "POST", "/api/auth/logout", nil, sessionCookies)
	var result map[string]any
	_ = json.Unmarshal(logout.Body.Bytes(), &result)
	logoutURL, err := url.Parse(fmt.Sprint(result["logout_url"]))
	if err != nil || logoutURL.Path != "/logout" || logoutURL.Query().Get("tenant") != "shared" || logoutURL.Query().Get("id_token_hint") == "" || result["cas_logout_url"] != nil {
		t.Fatalf("wrong logout: %s", logout.Body.String())
	}
	if rec := oidcTestRequest(t, a, "GET", "/api/auth/me", nil, sessionCookies); rec.Code != 401 {
		t.Fatalf("logged-out session still valid: %d", rec.Code)
	}
	end := oidcTestRequest(t, a, "GET", "/oidc/logged-out?state="+url.QueryEscape(logoutURL.Query().Get("state")), nil, logout.Result().Cookies())
	if end.Code != 302 || end.Header().Get("Location") != "/login?skipsso=true" {
		t.Fatalf("logout callback: %d %s", end.Code, end.Body.String())
	}
}

func TestOIDCNewUserCanUseCASAndRetainsDefaultQuotas(t *testing.T) {
	a := newQuotaTestApp(t)
	p := newOIDCTestProvider(t)
	cfg := oidcTestConfig(t, a, p)
	target, cookies := oidcTestStart(t, a, p, "//evil.example")
	callback := oidcTestCallback(t, a, target, cookies)
	if callback.Code != 302 || callback.Header().Get("Location") != "/" {
		t.Fatalf("unsafe return: %d %s", callback.Code, callback.Body.String())
	}
	user, _, _, _, err := a.userCredentialsByUsername(context.Background(), "sso-member")
	if err != nil || user.CASBound || !user.OIDCBound || user.IsAdmin {
		t.Fatalf("new user: %+v %v", user, err)
	}
	status, err := a.userQuotaStatus(context.Background(), user.ID)
	if err != nil || status.Unlimited || status.DailyQuotaUSD == nil || *status.DailyQuotaUSD != 10 || status.WeeklyQuotaUSD == nil || *status.WeeklyQuotaUSD != 40 {
		t.Fatalf("default quota: %+v %v", status, err)
	}
	casUser, err := a.resolveCASUser(context.Background(), cfg, casProfile{Username: "sso-member"})
	if err != nil || casUser.ID != user.ID || !casUser.OIDCBound || !casUser.CASBound {
		t.Fatalf("reverse CAS link: %+v %v", casUser, err)
	}
	var encrypted []byte
	if err := a.db.QueryRow("SELECT id_token FROM oidc_sessions LIMIT 1").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	raw, err := unsealOIDCToken(encrypted, cfg.SessionSecret)
	if err != nil || !strings.HasPrefix(raw, "eyJ") {
		t.Fatalf("ID token cannot be decrypted: %v", err)
	}
	if bytes.Equal(encrypted, []byte(raw)) {
		t.Fatal("ID token stored unencrypted")
	}
	if _, err := unsealOIDCToken(encrypted, "wrong-secret"); err == nil {
		t.Fatal("incorrect encryption key accepted")
	}
}

func TestOIDCRejectsInvalidIdentityAndState(t *testing.T) {
	for _, mode := range []string{"state", "cookie", "expired-state", "config-change", "nonce", "issuer", "audience", "expired-token", "signature", "missing-username", "userinfo-subject", "disabled", "local-collision", "bound-collision", "auto-create-disabled"} {
		t.Run(mode, func(t *testing.T) {
			a := newQuotaTestApp(t)
			p := newOIDCTestProvider(t)
			cfg := oidcTestConfig(t, a, p)
			switch mode {
			case "nonce":
				p.claims["nonce"] = "wrong"
			case "issuer":
				p.claims["iss"] = "https://other.example.test"
			case "audience":
				p.claims["aud"] = "other-app"
			case "expired-token":
				p.claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "signature":
				p.badSignature = true
			case "missing-username":
				delete(p.claims, "preferred_username")
			case "userinfo-subject":
				delete(p.claims, "preferred_username")
				p.userInfo = map[string]any{"sub": "another-subject", "preferred_username": "sso-member"}
			case "local-collision":
				seedQuotaTestUser(t, a, "sso-member")
			case "disabled", "bound-collision":
				user, err := a.resolveCASUser(context.Background(), cfg, casProfile{Username: "sso-member"})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "disabled" {
					_, _ = a.db.Exec("UPDATE users SET disabled_at = ? WHERE id = ?", dbTime(time.Now()), user.ID)
				}
				if mode == "bound-collision" {
					_, _ = a.db.Exec("INSERT INTO oidc_identities(issuer, subject, user_id) VALUES (?, 'another-subject', ?)", cfg.OIDC.Issuer, user.ID)
				}
			case "auto-create-disabled":
				cfg.OIDC.AutoCreateUsers = false
				if err := a.saveConfig(context.Background(), cfg); err != nil {
					t.Fatal(err)
				}
			}
			target, cookies := oidcTestStart(t, a, p, "/")
			switch mode {
			case "state":
				query := target.Query()
				query.Set("state", "wrong")
				target.RawQuery = query.Encode()
			case "cookie":
				cookies = nil
			case "expired-state":
				_, _ = a.db.Exec("UPDATE oidc_flows SET expires_at = 1")
			case "config-change":
				cfg.OIDC.ClientID = "changed"
				if err := a.saveConfig(context.Background(), cfg); err != nil {
					t.Fatal(err)
				}
			}
			rec := oidcTestCallback(t, a, target, cookies)
			want := http.StatusUnauthorized
			if mode == "local-collision" || mode == "bound-collision" {
				want = http.StatusConflict
			}
			if mode == "auto-create-disabled" {
				want = http.StatusForbidden
			}
			if rec.Code != want {
				t.Fatalf("invalid %s accepted: %d %s", mode, rec.Code, rec.Body.String())
			}
			for _, cookie := range rec.Result().Cookies() {
				if cookie.Name == security.SessionCookieName() && cookie.MaxAge > 0 {
					t.Fatal("invalid callback created session")
				}
			}
		})
	}
}

func TestOIDCUserInfoAndExplicitUsernameClaim(t *testing.T) {
	a := newQuotaTestApp(t)
	p := newOIDCTestProvider(t)
	cfg := oidcTestConfig(t, a, p)
	cfg.OIDC.UsernameClaim = "login_name"
	if err := a.saveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	p.userInfo = map[string]any{"sub": "subject-one", "login_name": "cas-compatible-name"}
	target, cookies := oidcTestStart(t, a, p, "/")
	callback := oidcTestCallback(t, a, target, cookies)
	if callback.Code != 302 {
		t.Fatalf("userinfo: %d %s", callback.Code, callback.Body.String())
	}
	if _, _, _, _, err := a.userCredentialsByUsername(context.Background(), "cas-compatible-name"); err != nil {
		t.Fatal(err)
	}
}

func TestOIDCLocalLogoutAndDisabledRoutes(t *testing.T) {
	a := newQuotaTestApp(t)
	if rec := oidcTestRequest(t, a, "GET", "/oidc/login", nil, nil); rec.Code != 503 {
		t.Fatalf("disabled: %d", rec.Code)
	}
	p := newOIDCTestProvider(t)
	p.noLogout = true
	oidcTestConfig(t, a, p)
	target, cookies := oidcTestStart(t, a, p, "/")
	callback := oidcTestCallback(t, a, target, cookies)
	if callback.Code != 302 {
		t.Fatal(callback.Body.String())
	}
	logout := oidcTestRequest(t, a, "POST", "/api/auth/logout", nil, callback.Result().Cookies())
	if !strings.Contains(logout.Body.String(), "sso_logout=local") {
		t.Fatalf("missing local-only notice: %s", logout.Body.String())
	}
}
