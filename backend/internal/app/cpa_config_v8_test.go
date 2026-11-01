package app_test

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

func cpaManagementTestClient(t *testing.T, upstream http.Handler) (http.Handler, []*http.Cookie) {
	t.Helper()
	cpa := httptest.NewServer(upstream)
	t.Cleanup(cpa.Close)
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := backendApp.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	h := app.Routes()
	cookies := requestJSON(t, h, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)
	requestJSON(t, h, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url": cpa.URL, "management_key": "test-management-key",
	}, cookies, nil)
	return h, cookies
}

func TestCPAConfigV8UsesOriginalRevisionAndNeverDowngradesWrites(t *testing.T) {
	content := "# native config\nconfig-version: 8\nserver: {port: 18316}\naccess: {api-keys: [client-key]}\napi-keys: {codex: []}\n"
	revision := func() string { return fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(content))) }
	puts, legacyCalls := 0, 0
	var seenRevision string
	h, cookies := cpaManagementTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v0/management/config") {
			legacyCalls++
			http.Error(w, "legacy config write forbidden", http.StatusConflict)
			return
		}
		if r.URL.Path != "/v8/management/config.yaml" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-management-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("ETag", revision())
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, content)
			return
		}
		puts++
		seenRevision = r.Header.Get("If-Match")
		if seenRevision != revision() {
			http.Error(w, "config_revision_conflict", http.StatusPreconditionFailed)
			return
		}
		if r.Header.Get("Content-Type") != "application/yaml" {
			t.Error("wrong YAML content type")
		}
		body, _ := io.ReadAll(r.Body)
		content = string(body)
		w.Header().Del("ETag")
		_, _ = io.WriteString(w, `{}`)
	}))
	var loaded struct {
		Content string `json:"content"`
		Version int    `json:"api_version"`
		ETag    string `json:"revision"`
	}
	requestJSON(t, h, http.MethodGet, "/api/settings/cpa-config", nil, cookies, &loaded)
	if loaded.Version != 8 || loaded.Content != content || loaded.ETag != revision() {
		t.Fatalf("incorrect snapshot: %+v", loaded)
	}
	draft := loaded.Content + "requests: {proxy-url: ''}\n"
	oldRevision := loaded.ETag
	content += "# concurrent edit\n"
	requestJSONExpectStatus(t, h, http.MethodPut, "/api/settings/cpa-config", map[string]any{
		"content": draft, "api_version": 8, "revision": oldRevision,
	}, cookies, http.StatusConflict)
	if seenRevision != oldRevision || !strings.Contains(content, "concurrent edit") || puts != 1 {
		t.Fatal("stale draft was retried with a newer revision or overwrote the file")
	}
	for _, invalid := range []map[string]any{
		{"content": draft},
		{"content": draft, "api_version": 8},
		{"content": draft, "api_version": 8, "revision": "*"},
		{"content": draft, "api_version": 8, "revision": "W/" + revision()},
	} {
		requestJSONExpectStatus(t, h, http.MethodPut, "/api/settings/cpa-config", invalid, cookies, http.StatusConflict)
	}
	if puts != 1 || legacyCalls != 0 {
		t.Fatal("invalid editor issued an upstream write")
	}
	requestJSON(t, h, http.MethodPut, "/api/settings/cpa-config", map[string]any{
		"content": draft, "api_version": 8, "revision": revision(),
	}, cookies, &loaded)
	if content != draft || loaded.Content != draft || loaded.ETag != revision() || puts != 2 {
		t.Fatal("save did not round-trip content and fresh revision")
	}
}

func TestCPAConfigV8DoesNotFallbackOnAuthenticationErrors(t *testing.T) {
	legacyCalls := 0
	h, cookies := cpaManagementTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v0/management/config") {
			legacyCalls++
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	requestJSONExpectStatus(t, h, http.MethodGet, "/api/settings/cpa-config", nil, cookies, http.StatusUnprocessableEntity)
	if legacyCalls != 0 {
		t.Fatal("authentication failure was mistaken for an old Core")
	}
}
