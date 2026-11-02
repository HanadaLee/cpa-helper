package app_test

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

func TestV8ConfigRevisionsWorkThroughCompressionProxy(t *testing.T) {
	revision := `"` + strings.Repeat("a", 64) + `"`
	yaml := "config-version: 8\nserver: {port: 18316}\n"
	seen := map[string]int{}
	compressedResponses := 0
	lastIfMatch := ""
	h, cookies := cpaManagementTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v8/management/config") {
			http.NotFound(w, r)
			return
		}
		seen[r.Method+" "+r.URL.Path]++
		if r.Header.Get("Accept-Encoding") == "identity" {
			t.Error("configuration requests disabled compression")
		}
		if r.Method == http.MethodPut {
			lastIfMatch = r.Header.Get("If-Match")
		}
		if r.Method == http.MethodPut && r.Header.Get("If-Match") != revision {
			http.Error(w, "revision conflict", http.StatusPreconditionFailed)
			return
		}
		var body string
		switch r.URL.Path {
		case "/v8/management/config/api-keys":
			body = `{"codex":[{"name":"production","keys":[{"api-key":"one","priority":2}]}]}`
		case "/v8/management/config/api-keys/codex":
			body = `{}`
		case "/v8/management/config.yaml":
			if r.Method == http.MethodPut {
				data, _ := io.ReadAll(r.Body)
				yaml = string(data)
			}
			body = yaml
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", revision)
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			// Model a reverse proxy compressing the representation and weakening
			// its ETag, including Go's automatically negotiated gzip responses.
			w.Header().Set("ETag", "W/"+revision)
			w.Header().Set("Content-Encoding", "gzip")
			compressedResponses++
			compressed := gzip.NewWriter(w)
			_, _ = io.WriteString(compressed, body)
			_ = compressed.Close()
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	var upstreams struct {
		Sections map[string][]map[string]any `json:"sections"`
		Version  int                         `json:"api_version"`
		Revision string                      `json:"revision"`
	}
	requestJSON(t, h, http.MethodGet, "/api/upstreams", nil, cookies, &upstreams)
	if upstreams.Version != 8 || upstreams.Revision != revision || len(upstreams.Sections["codex-api-key"]) != 1 {
		t.Fatalf("configuration rejected behind compression proxy: %+v", upstreams)
	}
	requestJSON(t, h, http.MethodPut, "/api/upstreams/codex-api-key", map[string]any{
		"items": upstreams.Sections["codex-api-key"], "api_version": 8, "revision": revision,
	}, cookies, nil)
	var config struct {
		Content  string `json:"content"`
		Version  int    `json:"api_version"`
		Revision string `json:"revision"`
	}
	requestJSON(t, h, http.MethodGet, "/api/settings/cpa-config", nil, cookies, &config)
	if config.Content != yaml || config.Version != 8 || config.Revision != revision {
		data, _ := json.Marshal(config)
		t.Fatalf("YAML configuration lost its strong revision: %s", data)
	}
	requestJSON(t, h, http.MethodPut, "/api/settings/cpa-config", map[string]any{
		"content": yaml + "# saved\n", "api_version": 8, "revision": revision,
	}, cookies, nil)
	if compressedResponses == 0 || lastIfMatch != revision {
		t.Fatal("compression was disabled or the original config revision was lost")
	}
	previousRevision, savedYAML := revision, yaml
	revision = `"` + strings.Repeat("b", 64) + `"`
	requestJSONExpectStatus(t, h, http.MethodPut, "/api/settings/cpa-config", map[string]any{
		"content": "# stale draft\n", "api_version": 8, "revision": previousRevision,
	}, cookies, http.StatusConflict)
	if lastIfMatch != previousRevision || yaml != savedYAML {
		t.Fatal("a concurrent change was overwritten after a compressed config read")
	}
	for _, request := range []string{"GET /v8/management/config/api-keys", "PUT /v8/management/config/api-keys/codex", "GET /v8/management/config.yaml", "PUT /v8/management/config.yaml"} {
		if seen[request] == 0 {
			t.Errorf("missing configuration request: %s", request)
		}
	}
}

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
