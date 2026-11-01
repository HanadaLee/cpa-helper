package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestUpstreamV8ConditionalWritesPreserveNativeGroups(t *testing.T) {
	revision := `"` + strings.Repeat("a", 64) + `"`
	groups := []map[string]any{{"name": "production", "base-url": "https://example.invalid", "priority": 3,
		"keys": []any{map[string]any{"api-key": "one", "websockets": true}, map[string]any{"api-key": "two", "priority": nil}}}}
	writes, legacyCalls := 0, 0
	h, cookies := cpaManagementTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v0/management/config") || r.URL.Path == "/v0/management/codex-api-key" {
			legacyCalls++
		}
		w.Header().Set("ETag", revision)
		switch {
		case r.URL.Path == "/v8/management/config/api-keys" && r.Method == http.MethodGet:
			writeTestJSON(t, w, map[string]any{"codex": groups})
		case r.URL.Path == "/v8/management/config/api-keys/codex" && r.Method == http.MethodPut:
			writes++
			if r.Header.Get("If-Match") != revision {
				http.Error(w, "conflict", http.StatusPreconditionFailed)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&groups); err != nil {
				t.Error(err)
			}
			writeTestJSON(t, w, map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	var response struct {
		Sections map[string][]map[string]any `json:"sections"`
		Version  int                         `json:"api_version"`
		Revision string                      `json:"revision"`
	}
	requestJSON(t, h, http.MethodGet, "/api/upstreams", nil, cookies, &response)
	items := response.Sections["codex-api-key"]
	if response.Version != 8 || response.Revision != revision || len(items) != 2 || items[0]["websockets"] != true {
		t.Fatalf("invalid V8 snapshot: %+v", response)
	}
	items[0]["priority"] = 5
	requestJSONExpectStatus(t, h, http.MethodPut, "/api/upstreams/codex-api-key", items, cookies, http.StatusConflict)
	requestJSONExpectStatus(t, h, http.MethodPut, "/api/upstreams/codex-api-key", map[string]any{
		"items": items, "api_version": 8, "revision": `"` + strings.Repeat("b", 64) + `"`,
	}, cookies, http.StatusConflict)
	if writes != 0 || legacyCalls != 0 {
		t.Fatal("outdated editor wrote to Core")
	}
	requestJSON(t, h, http.MethodPut, "/api/upstreams/codex-api-key", map[string]any{
		"items": items, "api_version": 8, "revision": revision,
	}, cookies, nil)
	if writes != 1 || legacyCalls != 0 || len(groups) != 1 || groups[0]["name"] != "production" || groups[0]["priority"] != float64(3) {
		t.Fatalf("native grouping was lost: %+v", groups)
	}
	keys := groups[0]["keys"].([]any)
	if len(keys) != 2 || keys[0].(map[string]any)["priority"] != float64(5) || keys[1].(map[string]any)["priority"] != nil {
		t.Fatalf("key-specific changes lost inheritance: %+v", keys)
	}
}
