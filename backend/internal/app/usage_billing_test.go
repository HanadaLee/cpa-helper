package app_test

import (
	"net/http"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

func TestUsageBillingRequiresAdministrator(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	a, err := backendApp.New()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	handler := a.Routes()
	admin := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "member", "password": "member-password", "nickname": "Member", "is_admin": false,
	}, admin, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{"allow_user_usage_history": true}, admin, nil)
	member := requestJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "member", "password": "member-password",
	}, nil, nil)
	for _, scope := range []string{"", "?scope=admin", "?scope=account", "?scope=shared"} {
		requestJSONExpectStatus(t, handler, http.MethodGet, "/api/usage/billing"+scope, nil, member, http.StatusForbidden)
	}
	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/usage/billing", nil, nil, http.StatusUnauthorized)
	var response struct {
		Items []map[string]any `json:"items"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/usage/billing?scope=admin", nil, admin, &response)
	if len(response.Items) != 2 {
		t.Fatalf("expected both zero-usage users, got %+v", response)
	}
	for index, expected := range []struct{ username, label string }{{"admin", "Admin"}, {"member", "Member"}} {
		item := response.Items[index]
		if item["username"] != expected.username || item["label"] != expected.label {
			t.Fatalf("missing user identity in billing HTTP response: %+v", item)
		}
		if id, ok := item["user_id"].(float64); !ok || id <= 0 {
			t.Fatalf("invalid user_id in billing HTTP response: %+v", item)
		}
		for _, key := range []string{"cost_usd", "records", "failed_records", "total_tokens", "unpriced_records"} {
			if value, ok := item[key].(float64); !ok || value != 0 {
				t.Fatalf("zero-usage user must include numeric %s=0: %+v", key, item)
			}
		}
	}
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/usage/billing", nil, admin, http.StatusMethodNotAllowed)
}
