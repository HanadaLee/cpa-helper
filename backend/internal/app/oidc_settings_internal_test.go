package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestOIDCSettingsPolicyAndSecretHandling(t *testing.T) {
	a := newQuotaTestApp(t)
	ctx := context.Background()
	admin := seedQuotaTestUser(t, a, "admin")
	if _, err := a.db.Exec("UPDATE users SET is_admin = 1 WHERE id = ?", admin); err != nil {
		t.Fatal(err)
	}
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := setSessionCookie(rec, admin, cfg.SessionSecret); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	settings := oidcTestRequest(t, a, "PUT", "/api/settings", map[string]any{
		"oidc_enabled": true, "oidc_issuer": "https://iam.example.test",
		"oidc_client_id": "helper", "oidc_client_secret": "client-secret-never-echo",
		"oidc_public_url": "https://helper.example.test/", "sso_default_login": "oidc",
	}, cookies)
	if settings.Code != 200 {
		t.Fatal(settings.Body.String())
	}
	if strings.Contains(settings.Body.String(), "client-secret-never-echo") {
		t.Fatal("secret leaked in settings response")
	}
	cfg, err = a.loadConfig(ctx)
	if err != nil || defaultSSOLogin(cfg) != "oidc" || cfg.CAS.DefaultLogin || cfg.OIDC.UsernameClaim != "preferred_username" {
		t.Fatalf("config: %+v %v", cfg.OIDC.Enabled, err)
	}
	state := oidcTestRequest(t, a, "GET", "/api/auth/setup", nil, nil)
	if !strings.Contains(state.Body.String(), `"sso_default_login":"oidc"`) || strings.Contains(state.Body.String(), "client-secret") {
		t.Fatalf("public state: %s", state.Body.String())
	}
	// Unrelated saves do not erase a masked secret.
	if update := oidcTestRequest(t, a, "PUT", "/api/settings", map[string]any{"oidc_username_claim": "name"}, cookies); update.Code != 200 {
		t.Fatal(update.Body.String())
	}
	cfg, _ = a.loadConfig(ctx)
	if cfg.OIDC.ClientSecret != "client-secret-never-echo" {
		t.Fatal("masked secret lost")
	}
	for _, payload := range []map[string]any{
		{"sso_default_login": "other"},
		{"sso_default_login": "cas"},
		{"oidc_enabled": false, "sso_default_login": "oidc"},
		{"oidc_client_secret": ""},
		{"oidc_issuer": "http://iam.example.test"},
		{"oidc_issuer": "https://user:password@iam.example.test"},
		{"oidc_public_url": "https://helper.example.test/#hash"},
	} {
		if result := oidcTestRequest(t, a, "PUT", "/api/settings", payload, cookies); result.Code != 422 {
			t.Fatalf("invalid config accepted: %v status=%d", payload, result.Code)
		}
	}
	cfg, _ = a.loadConfig(ctx)
	auth := AuthUser{}
	applyAuthUserConfig(&auth, cfg, true)
	if auth.CanChangePassword {
		t.Fatal("forced SSO permits password changes")
	}
	if result := oidcTestRequest(t, a, "POST", "/api/auth/change-credentials", map[string]any{"password": "new-password", "current_password": "old-password"}, cookies); result.Code != 403 {
		t.Fatalf("password change allowed: %d", result.Code)
	}
	if result := oidcTestRequest(t, a, "PUT", "/api/settings", map[string]any{"sso_default_login": ""}, cookies); result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	cfg, _ = a.loadConfig(ctx)
	applyAuthUserConfig(&auth, cfg, true)
	if !auth.CanChangePassword {
		t.Fatal("local password mode not restored")
	}
	// Local sessions do not sign out another enabled SSO provider.
	result := oidcTestRequest(t, a, "POST", "/api/auth/logout", nil, cookies)
	if !strings.Contains(result.Body.String(), `"logout_url":"/login?skipsso=true"`) {
		t.Fatal(result.Body.String())
	}
}

func TestOIDCMigrationPreservesCASDefaultAndUsers(t *testing.T) {
	a := newQuotaTestApp(t)
	id := seedQuotaTestUser(t, a, "migration-user")
	if err := goose.DownTo(a.db, ".", 202609260002); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE app_settings SET cas_enabled = 1, cas_default_login = 1, cas_base_url = 'https://cas.example.test', cas_public_url = 'https://helper.example.test'"); err != nil {
		t.Fatal(err)
	}
	if err := a.runMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg, err := a.loadConfig(context.Background())
	if err != nil || cfg.SSODefaultLogin != "cas" || defaultSSOLogin(cfg) != "cas" || cfg.OIDC.Enabled {
		t.Fatalf("legacy policy lost: %s %v", cfg.SSODefaultLogin, err)
	}
	user, _, _, _, err := a.userCredentialsByUsername(context.Background(), "migration-user")
	if err != nil || user.ID != id {
		t.Fatalf("migration changed user: %v", err)
	}
	if err := requireSchemaShape(context.Background(), a.db); err != nil {
		t.Fatal(err)
	}
}

func TestOIDCAddressValidation(t *testing.T) {
	for _, address := range []string{"https://iam.example.test/authorize?tenant=shared", "http://127.0.0.1:8080/token", "http://[::1]:8080/token"} {
		if got, err := normalizeOIDCEndpoint(address, "OIDC 端点"); err != nil || got != address {
			t.Fatalf("valid endpoint rejected: %s %v", address, err)
		}
	}
	for _, address := range []string{"http://iam.example.test/token", "https://user:password@iam.example.test/token", "https://iam.example.test/token#fragment", "//iam.example.test/token", "javascript:alert(1)"} {
		if _, err := normalizeOIDCEndpoint(address, "OIDC 端点"); err == nil {
			t.Fatalf("invalid endpoint accepted: %s", address)
		}
	}
	if _, err := normalizeOIDCURL("https://iam.example.test?tenant=shared", "OIDC Issuer"); err == nil {
		t.Fatal("issuer accepted query parameters")
	}
}
