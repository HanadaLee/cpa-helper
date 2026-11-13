package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCPACredentialsV8LifecycleAndNativeOperations(t *testing.T) {
	calls := map[string]int{}
	h, cookies := cpaManagementTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.Method+" "+r.URL.Path]++
		if r.Header.Get("Authorization") != "Bearer test-management-key" {
			t.Error("missing management auth")
		}
		switch r.URL.Path {
		case "/v8/management/credentials":
			_, _ = io.WriteString(w, `{"files":[{"name":"fixture.json","provider":"codex","requires_sign_in":true,"renewal_state":"sign_in_required","recovery_stage":"sign_in_required","siwc_bind_supported":true,"siwc_status":"off","refresh_error":{"code":"invalid_grant","message":"Sign in again","http_status":400}}]}`)
		case "/v8/management/credentials/refresh", "/v8/management/credentials/verify", "/v8/management/oauth/siwc/bind":
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["name"] != "fixture.json" {
				t.Error("wrong native operation target")
			}
			if strings.HasSuffix(r.URL.Path, "/bind") {
				_, _ = io.WriteString(w, `{"url":"https://auth.example.invalid/authorize","state":"synthetic-state"}`)
			} else {
				_, _ = io.WriteString(w, `{"ok":true,"auth":{"access_token":"never-relay-fixture"}}`)
			}
		case "/v8/management/oauth/auth-url":
			if r.URL.Query().Get("name") != "fixture.json" || r.URL.Query().Get("provider") != "codex" || r.URL.Query().Has("verify") {
				t.Error("targeted reauthorization changed target or opted into inference")
			}
			_, _ = io.WriteString(w, `{"url":"https://auth.example.invalid/authorize","state":"synthetic-state"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	var list struct {
		Items []map[string]any `json:"items"`
	}
	requestJSON(t, h, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &list)
	if len(list.Items) != 1 || list.Items[0]["v8_supported"] != true || list.Items[0]["requires_sign_in"] != true || list.Items[0]["siwc_bind_supported"] != true {
		t.Fatalf("lifecycle missing: %+v", list.Items)
	}
	for _, action := range []string{"core-refresh", "core-verify", "siwc-bind"} {
		var result map[string]any
		requestJSON(t, h, http.MethodPost, "/api/codex-keeper/accounts/fixture.json/"+action, nil, cookies, &result)
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "never-relay-fixture") {
			t.Fatal("native response leaked credential material")
		}
	}
	requestJSON(t, h, http.MethodPost, "/api/codex-keeper/oauth/start", map[string]any{"provider": "codex", "name": "fixture.json"}, cookies, nil)
	if calls["GET /v0/management/auth-files"] != 0 {
		t.Fatal("V8 credential list fell back to legacy")
	}
}

func TestCPACredentialsAuthenticationErrorNeverFallsBack(t *testing.T) {
	legacy := 0
	h, cookies := cpaManagementTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v0/management/auth-files" {
			legacy++
		}
		http.NotFound(w, r)
	}))
	var result map[string]any
	requestJSON(t, h, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &result)
	if legacy != 0 || result["degraded"] != true {
		t.Fatal("authentication error was masked by a legacy retry")
	}
}

func TestCPATargetedOAuthNeverFallsBackToUntargetedLogin(t *testing.T) {
	legacy := 0
	h, cookies := cpaManagementTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v0/") {
			legacy++
		}
		http.NotFound(w, r)
	}))
	requestJSONExpectStatus(t, h, http.MethodPost, "/api/codex-keeper/oauth/start", map[string]any{"provider": "codex", "name": "fixture.json"}, cookies, http.StatusUnprocessableEntity)
	if legacy != 0 {
		t.Fatal("targeted OAuth silently became an untargeted legacy login")
	}
}

func TestCPAJSONBusinessNotFoundNeverFallsBackToLegacy(t *testing.T) {
	legacy := 0
	h, cookies := cpaManagementTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v0/") {
			legacy++
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"OAuth session not found"}`)
	}))
	requestJSONExpectStatus(t, h, http.MethodGet, "/api/codex-keeper/oauth/status?state=synthetic-state", nil, cookies, http.StatusUnprocessableEntity)
	if legacy != 0 {
		t.Fatal("business 404 was masked by legacy retry")
	}
}
