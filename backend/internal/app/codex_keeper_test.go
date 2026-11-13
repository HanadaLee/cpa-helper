package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	backendApp "cpa-helper/backend/internal/app"
)

type keeperStatusResponse struct {
	Running       bool     `json:"running"`
	RunningModes  []string `json:"running_modes"`
	DaemonRunning bool     `json:"daemon_running"`
	Logs          []string `json:"logs"`
}

type keeperSettingsResponse struct {
	ConditionalRefreshIntervalSeconds int  `json:"conditional_refresh_interval_seconds"`
	AccountRefreshCacheMinutes        int  `json:"account_refresh_cache_minutes"`
	EnableCredentialWebsockets        bool `json:"enable_credential_websockets"`
}

type keeperAccountsResponse struct {
	Items []struct {
		Name           string                      `json:"name"`
		Provider       string                      `json:"provider"`
		AuthIndex      *string                     `json:"auth_index"`
		AccountType    *string                     `json:"account_type"`
		PlanType       *string                     `json:"plan_type"`
		Weight         *int                        `json:"weight"`
		RequestRetry   *int                        `json:"request_retry"`
		Success        *int                        `json:"success"`
		Failed         *int                        `json:"failed"`
		Quota          map[string]any              `json:"quota"`
		ModelQuotas    map[string]any              `json:"model_quotas"`
		Disabled       bool                        `json:"disabled"`
		Priority       *int                        `json:"priority"`
		PrimaryResetAt *string                     `json:"primary_reset_at"`
		LastStatusCode *int                        `json:"last_status_code"`
		LastError      *string                     `json:"last_error"`
		LatestAction   *string                     `json:"latest_action"`
		LastCheckedAt  *string                     `json:"last_checked_at"`
		LastHealthyAt  *string                     `json:"last_healthy_at"`
		ResetCredits   *keeperResetCreditsResponse `json:"reset_credits"`
	} `json:"items"`
	PriorityRules []struct {
		AccountType string `json:"account_type"`
		Priority    int    `json:"priority"`
	} `json:"priority_rules"`
	Degraded bool `json:"degraded"`
}

type keeperResetCreditsResponse struct {
	AvailableCount   int     `json:"available_count"`
	TotalEarnedCount *int    `json:"total_earned_count"`
	EarliestExpires  *string `json:"earliest_expires_at"`
	CachedAt         string  `json:"cached_at"`
}

type keeperOAuthStartResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

type keeperOAuthStatusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

type accountStatusAccessSettingsResponse struct {
	AllowUserAccountStatus bool `json:"allow_user_account_status"`
}

type accountStatusAccessUserResponse struct {
	CanViewAccountStatus bool `json:"can_view_account_status"`
}

type collectorStatusTimeResponse struct {
	LastPollAt    *string `json:"last_poll_at"`
	LastSuccessAt *string `json:"last_success_at"`
}

type userTimesResponse []struct {
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	APIKeys   []struct {
		CreatedAt *string `json:"created_at"`
		UpdatedAt *string `json:"updated_at"`
	} `json:"api_keys"`
}

type modelPriceTimesResponse []struct {
	LastSyncedAt *string `json:"last_synced_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type usageRecordsTimeResponse struct {
	Start *string `json:"start"`
	End   *string `json:"end"`
	Items []struct {
		Timestamp string `json:"timestamp"`
	} `json:"items"`
}

func TestKeeperAutoStartReportsDaemonRunning(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer func() {
		if app != nil {
			app.Close()
		}
	}()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     "http://127.0.0.1:1",
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":     "0 0 29 2 *",
		"auto_start_daemon": true,
	}, cookies, nil)
	app.Close()
	app = nil

	app, err = backendApp.New()
	if err != nil {
		t.Fatalf("New() with auto-start enabled failed: %v", err)
	}
	handler = app.Routes()

	status := keeperStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &status)
	if !status.DaemonRunning {
		t.Fatal("daemon_running = false, want true after auto-start")
	}
	if status.Running {
		t.Fatal("running = true, want false while daemon is only waiting for the next cron tick")
	}
	if status.RunningModes == nil {
		t.Fatal("running_modes is nil, want empty list")
	}

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/stop", nil, cookies, nil)
	status = keeperStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &status)
	if status.DaemonRunning {
		t.Fatal("daemon_running = true, want false after stop")
	}
}

func TestKeeperSettingsExposeConditionalRefreshConfig(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	settings := keeperSettingsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/settings", nil, cookies, &settings)
	if settings.ConditionalRefreshIntervalSeconds != 30 {
		t.Fatalf("conditional_refresh_interval_seconds = %d, want 30", settings.ConditionalRefreshIntervalSeconds)
	}
	if settings.AccountRefreshCacheMinutes != 10 {
		t.Fatalf("account_refresh_cache_minutes = %d, want 10", settings.AccountRefreshCacheMinutes)
	}
	if settings.EnableCredentialWebsockets {
		t.Fatal("enable_credential_websockets = true, want false by default")
	}

	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"conditional_refresh_interval_seconds": 0,
		"account_refresh_cache_minutes":        15,
		"enable_credential_websockets":         true,
	}, cookies, &settings)
	if settings.ConditionalRefreshIntervalSeconds != 0 {
		t.Fatalf("saved conditional_refresh_interval_seconds = %d, want 0", settings.ConditionalRefreshIntervalSeconds)
	}
	if settings.AccountRefreshCacheMinutes != 15 {
		t.Fatalf("saved account_refresh_cache_minutes = %d, want 15", settings.AccountRefreshCacheMinutes)
	}
	if !settings.EnableCredentialWebsockets {
		t.Fatal("saved enable_credential_websockets = false, want true")
	}

	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"conditional_refresh_interval_seconds": 7,
	}, cookies, http.StatusUnprocessableEntity)
	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"account_refresh_cache_minutes": 0,
	}, cookies, http.StatusUnprocessableEntity)
}

func TestKeeperAccountStatusCanBeSharedReadOnly(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	adminCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "member",
		"password": "member-password",
		"nickname": "Member",
		"is_admin": false,
	}, adminCookies, nil)

	member := accountStatusAccessUserResponse{}
	memberCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "member",
		"password": "member-password",
	}, nil, &member)
	if member.CanViewAccountStatus {
		t.Fatal("can_view_account_status = true, want false before access is enabled")
	}

	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, memberCookies, http.StatusForbidden)

	settings := accountStatusAccessSettingsResponse{}
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"allow_user_account_status": true,
	}, adminCookies, &settings)
	if !settings.AllowUserAccountStatus {
		t.Fatal("allow_user_account_status = false, want true after update")
	}

	member = accountStatusAccessUserResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/auth/me", nil, memberCookies, &member)
	if !member.CanViewAccountStatus {
		t.Fatal("can_view_account_status = false, want true after access is enabled")
	}

	status := keeperStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, memberCookies, &status)
	accounts := keeperAccountsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, memberCookies, &accounts)
	if len(accounts.PriorityRules) == 0 {
		t.Fatal("priority_rules is empty, want read-only priority rules in account response")
	}

	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/codex-keeper/settings", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/codex-keeper/auth-files/member.json", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/auth-files", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/oauth/start", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodDelete, "/api/codex-keeper/oauth/session?state=member-state", nil, memberCookies, http.StatusForbidden)
	for _, action := range []string{"core-refresh", "core-verify", "siwc-bind"} {
		requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/accounts/member.json/"+action, nil, memberCookies, http.StatusForbidden)
	}
	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/codex-keeper/oauth/status?state=member-state", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/oauth/callback", map[string]any{
		"redirect_url": "http://localhost:1455/auth/callback?code=test",
	}, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/accounts/refresh", map[string]any{
		"auth_names": []string{"member.json"},
	}, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/accounts/sync", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/accounts/member.json/disable", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/accounts/member.json/reset-credits/query", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/accounts/member.json/reset-credits/consume", nil, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodPatch, "/api/codex-keeper/accounts/member.json/priority", map[string]any{
		"priority": 10,
	}, memberCookies, http.StatusForbidden)
	requestJSONExpectStatus(t, handler, http.MethodDelete, "/api/codex-keeper/accounts/member.json", nil, memberCookies, http.StatusForbidden)
}

func TestKeeperResetCreditsQueryCachesAndConsumeRefreshes(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	const authName = "reset-credit.json"
	var mu sync.Mutex
	availableCount := 2
	queryCalls := 0
	consumeCalls := 0
	invalidRequest := ""
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer test-management-key" {
			http.Error(w, "missing management authorization", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{{"name": authName, "type": "codex"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			if r.URL.Query().Get("name") != authName {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": authName, "type": "codex", "email": "reset@example.com",
				"auth_index": authName, "account_id": "account-reset", "access_token": "test-token",
				"account_type": "plus", "priority": 4,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			var payload struct {
				AuthIndex string            `json:"auth_index"`
				Method    string            `json:"method"`
				URL       string            `json:"url"`
				Header    map[string]string `json:"header"`
				Data      string            `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.AuthIndex != authName || payload.Header["Chatgpt-Account-Id"] != "account-reset" {
				mu.Lock()
				invalidRequest = "missing account routing fields"
				mu.Unlock()
			}
			switch {
			case strings.HasSuffix(payload.URL, "/wham/usage"):
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status_code": http.StatusOK,
					"body": map[string]any{
						"plan_type": "plus",
						"rate_limit_reset_credits": map[string]any{
							"available_count":    2,
							"total_earned_count": 3,
							"credits": []map[string]any{
								{"id": "embedded", "status": "available", "expires_at": "2026-09-06T00:00:00Z"},
							},
						},
						"rate_limit": map[string]any{
							"primary_window":   map[string]any{"used_percent": 25, "reset_after_seconds": 3600},
							"secondary_window": map[string]any{"used_percent": 10, "reset_after_seconds": 86400},
						},
					},
				})
			case strings.HasSuffix(payload.URL, "/rate-limit-reset-credits") && payload.Method == http.MethodGet:
				mu.Lock()
				queryCalls++
				count := availableCount
				mu.Unlock()
				credits := []map[string]any{}
				if count > 0 {
					credits = append(credits, map[string]any{"id": "later", "status": "available", "expires_at": "2026-09-10T00:00:00Z"})
				}
				if count > 1 {
					credits = append(credits, map[string]any{"id": "earlier", "status": "available", "expires_at": "2026-09-05T00:00:00Z"})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status_code": http.StatusOK,
					"body":        map[string]any{"available_count": count, "total_earned_count": 3, "credits": credits},
				})
			case strings.HasSuffix(payload.URL, "/rate-limit-reset-credits/consume") && payload.Method == http.MethodPost:
				var data struct {
					RedeemRequestID string `json:"redeem_request_id"`
				}
				if err := json.Unmarshal([]byte(payload.Data), &data); err != nil || !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(data.RedeemRequestID) {
					mu.Lock()
					invalidRequest = "invalid redeem_request_id"
					mu.Unlock()
				}
				mu.Lock()
				consumeCalls++
				if availableCount > 0 {
					availableCount--
				}
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{"status_code": http.StatusOK, "body": map[string]any{"status": "ok"}})
			default:
				http.Error(w, "unexpected api-call target", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url": cpa.URL, "management_key": "test-management-key", "collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron": "0 0 29 2 *", "dry_run": true, "worker_threads": 1,
		"cpa_timeout_seconds": 1, "usage_timeout_seconds": 1,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/refresh", map[string]any{
		"auth_names": []string{authName},
	}, cookies, nil)
	waitForKeeperAccounts(t, handler, cookies, 1)

	accountsBefore := keeperAccountsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &accountsBefore)
	if accountsBefore.Items[0].ResetCredits == nil || accountsBefore.Items[0].ResetCredits.AvailableCount != 2 {
		t.Fatalf("embedded reset_credits = %+v, want available=2", accountsBefore.Items[0].ResetCredits)
	}
	mu.Lock()
	if queryCalls != 0 {
		t.Fatalf("standalone reset-credit query calls after embedded usage = %d, want 0", queryCalls)
	}
	mu.Unlock()
	if accountsBefore.Items[0].ResetCredits.EarliestExpires == nil || !strings.HasPrefix(*accountsBefore.Items[0].ResetCredits.EarliestExpires, "2026-09-06T08:00:00") {
		t.Fatalf("embedded reset-credit expiry = %v", accountsBefore.Items[0].ResetCredits.EarliestExpires)
	}

	first := keeperResetCreditsResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/reset-credit.json/reset-credits/query", nil, cookies, &first)
	if first.AvailableCount != 2 || first.TotalEarnedCount == nil || *first.TotalEarnedCount != 3 {
		t.Fatalf("first reset credits = %+v, want available=2 total=3", first)
	}
	if first.EarliestExpires == nil || !strings.HasPrefix(*first.EarliestExpires, "2026-09-05T08:00:00") {
		t.Fatalf("earliest expiry = %v, want 2026-09-05 in app timezone", first.EarliestExpires)
	}

	accountsAfter := keeperAccountsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &accountsAfter)
	if accountsAfter.Items[0].ResetCredits == nil || accountsAfter.Items[0].ResetCredits.AvailableCount != 2 {
		t.Fatalf("cached reset credits in account response = %+v, want available=2", accountsAfter.Items[0].ResetCredits)
	}
	mu.Lock()
	if queryCalls != 1 {
		t.Fatalf("query calls after cached account reload = %d, want 1", queryCalls)
	}
	mu.Unlock()

	second := keeperResetCreditsResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/reset-credit.json/reset-credits/query", nil, cookies, &second)
	consumed := keeperResetCreditsResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/reset-credit.json/reset-credits/consume", nil, cookies, &consumed)
	if consumed.AvailableCount != 1 {
		t.Fatalf("available after consume = %d, want 1", consumed.AvailableCount)
	}
	mu.Lock()
	finalQueryCalls := queryCalls
	finalConsumeCalls := consumeCalls
	finalInvalidRequest := invalidRequest
	mu.Unlock()
	if finalQueryCalls != 3 || finalConsumeCalls != 1 {
		t.Fatalf("query calls = %d, consume calls = %d, want 3 and 1", finalQueryCalls, finalConsumeCalls)
	}
	if finalInvalidRequest != "" {
		t.Fatal(finalInvalidRequest)
	}
}

func TestKeeperOAuthProxyUsesManagementAuthAndCodexProvider(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	startCalls := 0
	statusState := ""
	callbackBody := map[string]string{}
	managementAuthFailures := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-management-key" {
			managementAuthFailures++
			http.Error(w, "missing management authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/codex-auth-url":
			startCalls++
			if r.URL.Query().Get("is_webui") != "true" {
				http.Error(w, "missing is_webui query", http.StatusBadRequest)
				return
			}
			if startCalls == 1 {
				_ = json.NewEncoder(w).Encode(map[string]string{
					"url":   "https://auth.example.com/authorize?client=codex",
					"state": "state+with/chars",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"url":   "https://auth.example.com/authorize?client=codex",
				"state": "",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/get-auth-status":
			statusState = r.URL.Query().Get("state")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/oauth-callback":
			if err := json.NewDecoder(r.Body).Decode(&callbackBody); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)

	started := keeperOAuthStartResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/oauth/start", nil, cookies, &started)
	if started.URL != "https://auth.example.com/authorize?client=codex" || started.State != "state+with/chars" {
		t.Fatalf("OAuth start response = %#v", started)
	}

	status := keeperOAuthStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/oauth/status?state=state%2Bwith%2Fchars", nil, cookies, &status)
	if status.Status != "pending" || statusState != "state+with/chars" {
		t.Fatalf("OAuth status = %#v, upstream state = %q", status, statusState)
	}

	redirectURL := "http://localhost:1455/auth/callback?code=oauth-code&state=state%2Bwith%2Fchars"
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/oauth/callback", map[string]any{
		"redirect_url": redirectURL,
	}, cookies, nil)
	if len(callbackBody) != 2 || callbackBody["provider"] != "codex" || callbackBody["redirect_url"] != redirectURL {
		t.Fatalf("OAuth callback body = %#v", callbackBody)
	}
	if managementAuthFailures != 0 {
		t.Fatalf("management authorization failures = %d, want 0", managementAuthFailures)
	}

	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/oauth/start", nil, cookies, http.StatusUnprocessableEntity)
	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/codex-keeper/oauth/status", nil, cookies, http.StatusUnprocessableEntity)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/oauth/callback", map[string]any{
		"redirect_url": "not-a-url",
	}, cookies, http.StatusUnprocessableEntity)
}

func TestKeeperOAuthSupportsCredentialProviders(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	var mu sync.Mutex
	startQueries := map[string]url.Values{}
	callbackBodies := []map[string]string{}
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-management-key" {
			http.Error(w, "missing management authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "-auth-url") {
			mu.Lock()
			startQueries[r.URL.Path] = r.URL.Query()
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{
				"url":   "https://auth.example.com/authorize",
				"state": strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v0/management/"), "-auth-url") + "-state",
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v0/management/oauth-callback" {
			body := map[string]string{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			callbackBodies = append(callbackBodies, body)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		http.NotFound(w, r)
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url": cpa.URL, "management_key": "test-management-key", "collector_enabled": false,
	}, cookies, nil)

	providers := map[string]string{
		"codex":       "codex-auth-url",
		"anthropic":   "anthropic-auth-url",
		"antigravity": "antigravity-auth-url",
		"gemini-cli":  "gemini-cli-auth-url",
		"kimi":        "kimi-auth-url",
		"xai":         "xai-auth-url",
	}
	for provider, endpoint := range providers {
		payload := map[string]any{"provider": provider}
		if provider == "gemini-cli" {
			payload["project_id"] = "test-project"
		}
		started := keeperOAuthStartResponse{}
		requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/oauth/start", payload, cookies, &started)
		if started.URL == "" || started.State == "" {
			t.Fatalf("%s OAuth start response = %#v", provider, started)
		}
		query := startQueries["/v0/management/"+endpoint]
		if provider == "kimi" {
			if query.Has("is_webui") {
				t.Fatalf("Kimi OAuth query = %v, want no is_webui", query)
			}
		} else if query.Get("is_webui") != "true" {
			t.Fatalf("%s OAuth query = %v, want is_webui=true", provider, query)
		}
		if provider == "gemini-cli" && query.Get("project_id") != "test-project" {
			t.Fatalf("Gemini CLI OAuth query = %v, want project_id", query)
		}
	}

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/oauth/callback", map[string]any{
		"provider": "gemini-cli", "redirect_url": "http://localhost:8085/oauth-callback?code=test",
	}, cookies, nil)
	if len(callbackBodies) != 1 || callbackBodies[0]["provider"] != "gemini" {
		t.Fatalf("Gemini CLI callback bodies = %#v", callbackBodies)
	}
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/oauth/start", map[string]any{
		"provider": "unsupported",
	}, cookies, http.StatusUnprocessableEntity)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/codex-keeper/oauth/start", map[string]any{
		"provider": "gemini-cli", "project_id": strings.Repeat("p", 257),
	}, cookies, http.StatusUnprocessableEntity)
}

func TestKeeperManagesNonCodexCredentials(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	var statusPayload map[string]any
	var priorityPayload map[string]any
	deletedName := ""
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{{
				"name": "claude.json", "type": "anthropic", "priority": 4,
			}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/status":
			if err := json.NewDecoder(r.Body).Decode(&statusPayload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			if err := json.NewDecoder(r.Body).Decode(&priorityPayload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case r.Method == http.MethodDelete && r.URL.Path == "/v0/management/auth-files":
			deletedName = r.URL.Query().Get("name")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url": cpa.URL, "management_key": "test-management-key", "collector_enabled": false,
	}, cookies, nil)

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/claude.json/disable", nil, cookies, nil)
	if statusPayload["name"] != "claude.json" || statusPayload["disabled"] != true {
		t.Fatalf("non-Codex status payload = %#v", statusPayload)
	}
	requestJSON(t, handler, http.MethodPatch, "/api/codex-keeper/accounts/claude.json/priority", map[string]any{"priority": 17}, cookies, nil)
	if priorityPayload["name"] != "claude.json" || priorityPayload["priority"] != float64(17) {
		t.Fatalf("non-Codex priority payload = %#v", priorityPayload)
	}
	requestJSONExpectStatus(t, handler, http.MethodPatch, "/api/codex-keeper/accounts/claude.json/priority", map[string]any{"priority": 1000001}, cookies, http.StatusUnprocessableEntity)
	requestJSON(t, handler, http.MethodDelete, "/api/codex-keeper/accounts/claude.json", nil, cookies, nil)
	if deletedName != "claude.json" {
		t.Fatalf("deleted non-Codex credential = %q", deletedName)
	}
}

func TestKeeperAuthFileManagementUsesMultipartAndSupportsInvalidPreview(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	uploadedNames := []string{}
	uploadedContents := []string{}
	var updatedFields map[string]any
	remoteNote := "CPA 中的初始备注"
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-management-key" {
			http.Error(w, "missing management authorization", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/models":
			if r.URL.Query().Get("name") != "valid.json" {
				http.Error(w, "unexpected auth file name", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
				{"id": "z-model", "owned_by": "provider"},
				{"id": "a-model", "display_name": "A Model"},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"name": "valid.json", "type": "codex", "note": remoteNote},
					{"name": "challenge.json", "type": "codex"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/auth-files":
			if err := r.ParseMultipartForm(12 << 20); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			defer file.Close()
			content, err := io.ReadAll(file)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			uploadedNames = append(uploadedNames, header.Filename)
			uploadedContents = append(uploadedContents, string(content))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			switch r.URL.Query().Get("name") {
			case "valid.json":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"type":       "codex",
					"prefix":     "team-a",
					"proxy_url":  "socks5://127.0.0.1:1080",
					"priority":   10,
					"websockets": true,
				})
			case "challenge.json":
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<!doctype html><html><body>Cloudflare challenge</body></html>"))
			default:
				http.NotFound(w, r)
			}
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			if err := json.NewDecoder(r.Body).Decode(&updatedFields); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if note, ok := updatedFields["note"].(string); ok {
				remoteNote = note
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)

	var uploadBody bytes.Buffer
	uploadWriter := multipart.NewWriter(&uploadBody)
	uploadPart, err := uploadWriter.CreateFormFile("file", "valid.json")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := uploadPart.Write([]byte(`{"type":"codex","access_token":"secret"}`)); err != nil {
		t.Fatalf("write upload part: %v", err)
	}
	secondUploadPart, err := uploadWriter.CreateFormFile("file", "second.json")
	if err != nil {
		t.Fatalf("CreateFormFile second file: %v", err)
	}
	if _, err := secondUploadPart.Write([]byte(`{"type":"codex","access_token":"second-secret"}`)); err != nil {
		t.Fatalf("write second upload part: %v", err)
	}
	sub2APIUploadPart, err := uploadWriter.CreateFormFile("file", "sub2api-export.json")
	if err != nil {
		t.Fatalf("CreateFormFile Sub2API file: %v", err)
	}
	sub2APIContent := `{
		"exported_at":"2026-08-29T00:00:00.000Z",
		"proxies":[],
		"accounts":[
			{"name":"First","platform":"openai","type":"oauth","credentials":{"access_token":"first-access","chatgpt_account_id":"account-first","email":"first@example.com","plan_type":"plus"}},
			{"name":"Second","platform":"openai","type":"oauth","credentials":{"access_token":"second-access","refresh_token":"second-refresh","id_token":"second-id","chatgpt_account_id":"account-second","email":"second@example.com"}},
			{"name":"Ignored","platform":"anthropic","type":"oauth","credentials":{"access_token":"ignored-access"}}
		]
	}`
	if _, err := sub2APIUploadPart.Write([]byte(sub2APIContent)); err != nil {
		t.Fatalf("write Sub2API upload part: %v", err)
	}
	if err := uploadWriter.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/codex-keeper/auth-files", &uploadBody)
	uploadRequest.Header.Set("Content-Type", uploadWriter.FormDataContentType())
	for _, cookie := range cookies {
		uploadRequest.AddCookie(cookie)
	}
	uploadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadRecorder, uploadRequest)
	if uploadRecorder.Code != http.StatusOK {
		t.Fatalf("upload returned %d: %s", uploadRecorder.Code, uploadRecorder.Body.String())
	}
	if len(uploadedNames) != 4 || uploadedNames[0] != "valid.json" || uploadedNames[1] != "second.json" || uploadedNames[2] != "codex-first@example.com.json" || uploadedNames[3] != "codex-second@example.com.json" {
		t.Fatalf("upstream uploaded names = %#v", uploadedNames)
	}
	if len(uploadedContents) != 4 || uploadedContents[0] != `{"type":"codex","access_token":"secret"}` || uploadedContents[1] != `{"type":"codex","access_token":"second-secret"}` {
		t.Fatalf("upstream uploaded contents = %#v", uploadedContents)
	}
	firstConverted := map[string]any{}
	if err := json.Unmarshal([]byte(uploadedContents[2]), &firstConverted); err != nil {
		t.Fatalf("decode first converted Sub2API account: %v", err)
	}
	if firstConverted["type"] != "codex" || firstConverted["access_token"] != "first-access" || firstConverted["account_id"] != "account-first" || firstConverted["email"] != "first@example.com" || firstConverted["plan_type"] != "plus" || firstConverted["id_token_synthetic"] != true {
		t.Fatalf("first converted Sub2API account = %#v", firstConverted)
	}
	if idToken, _ := firstConverted["id_token"].(string); len(strings.Split(idToken, ".")) != 3 {
		t.Fatalf("first converted id_token = %q, want three JWT segments", idToken)
	}
	secondConverted := map[string]any{}
	if err := json.Unmarshal([]byte(uploadedContents[3]), &secondConverted); err != nil {
		t.Fatalf("decode second converted Sub2API account: %v", err)
	}
	if secondConverted["type"] != "codex" || secondConverted["access_token"] != "second-access" || secondConverted["refresh_token"] != "second-refresh" || secondConverted["id_token"] != "second-id" {
		t.Fatalf("second converted Sub2API account = %#v", secondConverted)
	}
	if _, exists := secondConverted["expired"]; exists {
		t.Fatalf("refreshable converted account unexpectedly has expired: %#v", secondConverted)
	}
	uploadResponse := struct {
		Uploaded int      `json:"uploaded"`
		Files    []string `json:"files"`
		Failed   []any    `json:"failed"`
	}{}
	if err := json.Unmarshal(uploadRecorder.Body.Bytes(), &uploadResponse); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploadResponse.Uploaded != 4 || len(uploadResponse.Files) != 4 || len(uploadResponse.Failed) != 0 {
		t.Fatalf("upload response = %#v", uploadResponse)
	}

	validDetail := struct {
		JSON map[string]any `json:"json"`
	}{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/auth-files/valid.json", nil, cookies, &validDetail)
	if validDetail.JSON["prefix"] != "team-a" || validDetail.JSON["websockets"] != true || validDetail.JSON["note"] != remoteNote {
		t.Fatalf("valid auth detail = %#v", validDetail.JSON)
	}
	modelsResponse := struct {
		Models []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			OwnedBy     string `json:"owned_by"`
		} `json:"models"`
	}{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/auth-files/valid.json/models", nil, cookies, &modelsResponse)
	if len(modelsResponse.Models) != 2 || modelsResponse.Models[0].ID != "a-model" || modelsResponse.Models[0].DisplayName != "A Model" || modelsResponse.Models[1].OwnedBy != "provider" {
		t.Fatalf("auth file models = %#v", modelsResponse.Models)
	}

	invalidDetail := struct {
		JSON          map[string]any `json:"json"`
		RawText       string         `json:"raw_text"`
		InvalidReason string         `json:"invalid_reason"`
	}{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/auth-files/challenge.json", nil, cookies, &invalidDetail)
	if invalidDetail.JSON != nil || invalidDetail.InvalidReason != "html_challenge" || !strings.Contains(invalidDetail.RawText, "Cloudflare challenge") {
		t.Fatalf("invalid auth detail = %#v", invalidDetail)
	}

	requestJSON(t, handler, http.MethodPatch, "/api/codex-keeper/auth-files/valid.json", map[string]any{
		"prefix":        "team-b",
		"priority":      12,
		"weight":        4,
		"request_retry": 2,
		"websockets":    false,
		"note":          "CPA 中更新后的备注",
		"headers": map[string]string{
			"X-Test": "value",
		},
	}, cookies, nil)
	if updatedFields["name"] != "valid.json" || updatedFields["prefix"] != "team-b" || updatedFields["priority"] != float64(12) || updatedFields["weight"] != float64(4) || updatedFields["request_retry"] != float64(2) || updatedFields["websockets"] != false || updatedFields["note"] != remoteNote {
		t.Fatalf("updated fields = %#v", updatedFields)
	}
	headers, ok := updatedFields["headers"].(map[string]any)
	if !ok || headers["X-Test"] != "value" {
		t.Fatalf("updated headers = %#v", updatedFields["headers"])
	}
	validDetail.JSON = nil
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/auth-files/valid.json", nil, cookies, &validDetail)
	if validDetail.JSON["note"] != "CPA 中更新后的备注" {
		t.Fatalf("updated note was not reloaded from CPA: %#v", validDetail.JSON)
	}
}

func TestKeeperLogsUseStandardFileFormatAndCanBeCleared(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     "http://127.0.0.1:1",
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron": "0 0 29 2 *",
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/start", nil, cookies, nil)

	status := keeperStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &status)
	if len(status.Logs) == 0 {
		t.Fatal("status logs are empty, want daemon start log")
	}
	lastLog := status.Logs[len(status.Logs)-1]
	assertStandardKeeperLogLine(t, lastLog)

	logPath := filepath.Join(dataDir, "logs", "codex-keeper-"+keeperLogDate(t, lastLog)+".log")
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read keeper log file: %v", err)
	}
	fileLines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(fileLines) == 0 {
		t.Fatal("keeper log file is empty")
	}
	assertStandardKeeperLogLine(t, strings.TrimSpace(fileLines[len(fileLines)-1]))

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/logs/clear", nil, cookies, nil)
	matches, err := filepath.Glob(filepath.Join(dataDir, "logs", "codex-keeper-*.log"))
	if err != nil {
		t.Fatalf("glob keeper log files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("keeper log files after clear = %v, want none", matches)
	}
}

func TestKeeperStatusRestoresRecentLogFileLines(t *testing.T) {
	dataDir := t.TempDir()
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("create log dir: %v", err)
	}
	expected := `time=2026-05-16T16:03:05.571+08:00 level=INFO msg="demo.json: 巡检正常，类型 free" component=codex_keeper`
	if err := os.WriteFile(filepath.Join(logDir, "codex-keeper-2026-05-16.log"), []byte(expected+"\n"), 0o644); err != nil {
		t.Fatalf("write keeper log fixture: %v", err)
	}
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	status := keeperStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &status)
	if len(status.Logs) != 1 || status.Logs[0] != expected {
		t.Fatalf("restored logs = %#v, want %#v", status.Logs, []string{expected})
	}
}

func TestKeeperRunMaintainsSystemPriorityRules(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	authNames := []string{
		"free-null.json",
		"free-quota-null.json",
		"k12-wrong.json",
		"plus-wrong.json",
		"manual-quota-high.json",
		"manual-high.json",
		"manual-low.json",
	}
	usagePercents := map[string]int{
		"free-quota-null.json":   100,
		"manual-quota-high.json": 100,
	}
	authDetails := map[string]map[string]any{
		"free-null.json": {
			"name":         "free-null.json",
			"type":         "codex",
			"email":        "user001@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     nil,
			"access_token": "test-token",
		},
		"free-quota-null.json": {
			"name":         "free-quota-null.json",
			"type":         "codex",
			"email":        "user005@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     nil,
			"access_token": "test-token",
		},
		"plus-wrong.json": {
			"name":         "plus-wrong.json",
			"type":         "codex",
			"email":        "user002@example.com",
			"account_type": "plus",
			"disabled":     false,
			"priority":     0,
			"access_token": "test-token",
		},
		"k12-wrong.json": {
			"name":         "k12-wrong.json",
			"type":         "codex",
			"email":        "user007@example.com",
			"auth_index":   "k12-auth",
			"account_type": "oauth",
			"id_token":     map[string]any{"plan_type": "k12"},
			"disabled":     false,
			"priority":     0,
			"access_token": "test-token",
		},
		"manual-quota-high.json": {
			"name":         "manual-quota-high.json",
			"type":         "codex",
			"email":        "user006@example.com",
			"account_type": "plus",
			"disabled":     false,
			"priority":     100,
			"access_token": "test-token",
		},
		"manual-high.json": {
			"name":         "manual-high.json",
			"type":         "codex",
			"email":        "user003@example.com",
			"account_type": "plus",
			"disabled":     false,
			"priority":     21,
			"access_token": "test-token",
		},
		"manual-low.json": {
			"name":         "manual-low.json",
			"type":         "codex",
			"email":        "user004@example.com",
			"account_type": "team",
			"disabled":     false,
			"priority":     -2,
			"access_token": "test-token",
		},
	}
	var mu sync.Mutex
	patches := map[string][]int{}

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			files := make([]map[string]any, 0, len(authNames))
			for _, name := range authNames {
				files = append(files, map[string]any{"name": name, "type": "codex"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"files": files})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			detail, ok := authDetails[r.URL.Query().Get("name")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(detail)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			var payload struct {
				AuthIndex string `json:"auth_index"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			usedPercent := usagePercents[payload.AuthIndex]
			if usedPercent == 0 {
				usedPercent = 10
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"body": map[string]any{
					"rate_limit": map[string]any{
						"primary_window": map[string]any{
							"used_percent":        usedPercent,
							"reset_after_seconds": 3600,
						},
					},
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			var payload struct {
				Name     string `json:"name"`
				Priority *int   `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Priority == nil {
				http.Error(w, "priority is required", http.StatusBadRequest)
				return
			}
			mu.Lock()
			patches[payload.Name] = append(patches[payload.Name], *payload.Priority)
			if detail, ok := authDetails[payload.Name]; ok {
				detail["priority"] = *payload.Priority
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":       "0 0 29 2 *",
		"dry_run":             false,
		"quota_threshold":     100,
		"worker_threads":      1,
		"cpa_timeout_seconds": 1,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/run-once", nil, cookies, nil)
	response := waitForKeeperAccounts(t, handler, cookies, len(authNames))

	expectedPriorities := map[string]int{
		"free-null.json":         0,
		"free-quota-null.json":   -1,
		"k12-wrong.json":         2,
		"plus-wrong.json":        4,
		"manual-quota-high.json": -1,
		"manual-high.json":       21,
		"manual-low.json":        -2,
	}
	assertKeeperPriorities(t, response, expectedPriorities)

	mu.Lock()
	expectedPatches := map[string][]int{
		"free-quota-null.json":   {-1},
		"k12-wrong.json":         {2},
		"plus-wrong.json":        {4},
		"manual-quota-high.json": {-1},
	}
	assertKeeperPriorityPatchesLocked(t, patches, expectedPatches)
	mu.Unlock()

	usagePercents["manual-quota-high.json"] = 10
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/run-once", nil, cookies, nil)
	response = waitForKeeperAccounts(t, handler, cookies, len(authNames))
	expectedPriorities["manual-quota-high.json"] = 100
	assertKeeperPriorities(t, response, expectedPriorities)

	mu.Lock()
	expectedPatches["manual-quota-high.json"] = []int{-1, 100}
	assertKeeperPriorityPatchesLocked(t, patches, expectedPatches)
	mu.Unlock()
}

func TestKeeperRefreshAccountsOnlyProcessesSelectedAuths(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	authDetails := map[string]map[string]any{
		"refresh-me.json": {
			"name":         "refresh-me.json",
			"type":         "codex",
			"email":        "refresh@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     0,
			"access_token": "test-token",
		},
		"skip-me.json": {
			"name":         "skip-me.json",
			"type":         "codex",
			"email":        "skip@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     0,
			"access_token": "test-token",
		},
	}
	var mu sync.Mutex
	usageCalls := map[string]int{}

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"name": "refresh-me.json", "type": "codex"},
					{"name": "skip-me.json", "type": "codex"},
					{"name": "not-codex.json", "type": "openai"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			detail, ok := authDetails[r.URL.Query().Get("name")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(detail)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			var payload struct {
				AuthIndex string `json:"auth_index"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			usageCalls[payload.AuthIndex]++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"body": map[string]any{
					"plan_type": "free",
					"rate_limit": map[string]any{
						"primary_window": map[string]any{
							"used_percent":        10,
							"reset_after_seconds": 3600,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":       "0 0 29 2 *",
		"dry_run":             false,
		"quota_threshold":     100,
		"worker_threads":      1,
		"cpa_timeout_seconds": 1,
	}, cookies, nil)

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/refresh", map[string]any{
		"auth_names": []string{"refresh-me.json"},
	}, cookies, nil)
	waitForKeeperAccountChecked(t, handler, cookies, "refresh-me.json")

	mu.Lock()
	defer mu.Unlock()
	if got := usageCalls["refresh-me.json"]; got != 1 {
		t.Fatalf("refresh-me usage calls = %d, want 1", got)
	}
	if got := usageCalls["skip-me.json"]; got != 0 {
		t.Fatalf("skip-me usage calls = %d, want 0", got)
	}
}

func TestKeeperSyncAccountListOnlyReconcilesMembership(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	var mu sync.Mutex
	listCalls := 0
	detailCalls := 0
	usageCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			mu.Lock()
			listCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"name": "kept.json", "type": "codex", "account_type": "oauth", "disabled": true, "priority": 99},
					{"name": "new.json", "type": "codex", "email": "new@example.com", "account_type": "plus", "disabled": true, "priority": 7},
					{"name": "ignored.json", "type": "openai", "auth_index": "openai-index", "weight": 3, "request_retry": 2, "success": 8, "failed": 2, "quota": map[string]any{"signals": map[string]string{"remaining": "75%"}}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			mu.Lock()
			detailCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"name": r.URL.Query().Get("name")})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			mu.Lock()
			usageCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"status_code": http.StatusOK})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "db", "cpa_helper.sqlite3")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()
	now := "2026-08-29 10:00:00"
	_, err = db.Exec(`
		INSERT INTO codex_keeper_auth_states (
			auth_name, email, account_type, disabled, priority, last_checked_at, created_at, updated_at
		) VALUES
			('kept.json', 'kept@example.com', 'pro_20x', 0, 11, ?, ?, ?),
			('stale.json', 'stale@example.com', 'plus', 0, 3, ?, ?, ?)
	`, now, now, now, now, now, now)
	if err != nil {
		t.Fatalf("seed keeper account states: %v", err)
	}

	syncResponse := struct {
		Added   int `json:"added"`
		Removed int `json:"removed"`
	}{}
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/sync", nil, cookies, &syncResponse)
	if syncResponse.Added != 1 || syncResponse.Removed != 1 {
		t.Fatalf("sync result = added %d, removed %d; want 1 and 1", syncResponse.Added, syncResponse.Removed)
	}

	response := keeperAccountsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &response)
	if len(response.Items) != 3 {
		t.Fatalf("accounts length = %d, want 3", len(response.Items))
	}
	items := map[string]struct {
		Provider      string
		AuthIndex     *string
		AccountType   *string
		PlanType      *string
		Weight        *int
		RequestRetry  *int
		Success       *int
		Failed        *int
		Quota         map[string]any
		Disabled      bool
		Priority      *int
		LastCheckedAt *string
	}{}
	for _, item := range response.Items {
		items[item.Name] = struct {
			Provider      string
			AuthIndex     *string
			AccountType   *string
			PlanType      *string
			Weight        *int
			RequestRetry  *int
			Success       *int
			Failed        *int
			Quota         map[string]any
			Disabled      bool
			Priority      *int
			LastCheckedAt *string
		}{item.Provider, item.AuthIndex, item.AccountType, item.PlanType, item.Weight, item.RequestRetry, item.Success, item.Failed, item.Quota, item.Disabled, item.Priority, item.LastCheckedAt}
	}
	kept, ok := items["kept.json"]
	if !ok {
		t.Fatal("kept.json missing after sync")
	}
	if !kept.Disabled || kept.Priority == nil || *kept.Priority != 99 {
		t.Fatalf("remote account fields not reflected after sync: disabled=%v priority=%v", kept.Disabled, kept.Priority)
	}
	if kept.AccountType == nil || *kept.AccountType != "oauth" || kept.PlanType == nil || *kept.PlanType != "pro_20x" {
		t.Fatalf("kept credential type/plan = account_type %v plan_type %v", kept.AccountType, kept.PlanType)
	}
	var storedDisabled bool
	var storedPriority int
	if err := db.QueryRow(`SELECT disabled, priority FROM codex_keeper_auth_states WHERE auth_name = ?`, "kept.json").Scan(&storedDisabled, &storedPriority); err != nil {
		t.Fatalf("read retained keeper state: %v", err)
	}
	if storedDisabled || storedPriority != 11 {
		t.Fatalf("sync changed retained keeper state: disabled=%v priority=%d", storedDisabled, storedPriority)
	}
	added, ok := items["new.json"]
	if !ok {
		t.Fatal("new.json missing after sync")
	}
	if !added.Disabled || added.Priority == nil || *added.Priority != 7 || added.AccountType == nil || *added.AccountType != "plus" {
		t.Fatalf("new account list fields = disabled=%v priority=%v account_type=%v", added.Disabled, added.Priority, added.AccountType)
	}
	if added.LastCheckedAt != nil {
		t.Fatalf("new account last_checked_at = %v, want nil without inspection", added.LastCheckedAt)
	}
	if _, ok := items["stale.json"]; ok {
		t.Fatal("stale.json still present after sync")
	}
	ignored, ok := items["ignored.json"]
	if !ok {
		t.Fatal("non-Codex account missing from credential list")
	}
	if ignored.Provider != "openai" {
		t.Fatalf("non-Codex provider = %q, want openai", ignored.Provider)
	}
	if ignored.AuthIndex == nil || *ignored.AuthIndex != "openai-index" || ignored.Weight == nil || *ignored.Weight != 3 || ignored.RequestRetry == nil || *ignored.RequestRetry != 2 {
		t.Fatalf("non-Codex common fields = %#v", ignored)
	}
	if ignored.Success == nil || *ignored.Success != 8 || ignored.Failed == nil || *ignored.Failed != 2 || ignored.Quota == nil {
		t.Fatalf("non-Codex observations = %#v", ignored)
	}
	var ignoredStateCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM codex_keeper_auth_states WHERE auth_name = 'ignored.json'`).Scan(&ignoredStateCount); err != nil {
		t.Fatalf("read non-Codex keeper state count: %v", err)
	}
	if ignoredStateCount != 0 {
		t.Fatalf("non-Codex keeper state count = %d, want 0", ignoredStateCount)
	}

	var runCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM codex_keeper_runs`).Scan(&runCount); err != nil {
		t.Fatalf("read keeper run count: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("keeper run count = %d, want 0", runCount)
	}
	mu.Lock()
	defer mu.Unlock()
	if listCalls != 2 || detailCalls != 0 || usageCalls != 0 {
		t.Fatalf("CPA calls = list %d, detail %d, usage %d; want 2, 0, 0", listCalls, detailCalls, usageCalls)
	}
}

func TestKeeperAccountListMarksLocalFallbackAsDegraded(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url": cpa.URL, "management_key": "test-management-key", "collector_enabled": false,
	}, cookies, nil)

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "db", "cpa_helper.sqlite3")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()
	now := "2026-09-04 10:00:00"
	if _, err := db.Exec(`
		INSERT INTO codex_keeper_auth_states (auth_name, email, disabled, priority, created_at, updated_at)
		VALUES ('cached.json', 'cached@example.com', 0, 4, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("seed cached keeper state: %v", err)
	}

	response := keeperAccountsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &response)
	if !response.Degraded || len(response.Items) != 1 || response.Items[0].Name != "cached.json" || response.Items[0].Provider != "codex" {
		t.Fatalf("degraded credential response = %+v", response)
	}
}

func TestKeeperRefreshAccountsDisablesBadCredentialAndAppliesPriorityPolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	authDetails := map[string]map[string]any{
		"quota-high.json": {
			"name":         "quota-high.json",
			"type":         "codex",
			"email":        "quota@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     0,
			"access_token": "test-token",
		},
		"bad-token.json": {
			"name":         "bad-token.json",
			"type":         "codex",
			"email":        "bad@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     0,
			"access_token": "bad-token",
		},
		"overloaded.json": {
			"name":         "overloaded.json",
			"type":         "codex",
			"email":        "overloaded@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     0,
			"access_token": "overloaded-token",
		},
	}
	var mu sync.Mutex
	statusPatchCount := 0
	priorityPatchCount := 0

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"name": "quota-high.json", "type": "codex"},
					{"name": "bad-token.json", "type": "codex"},
					{"name": "overloaded.json", "type": "codex"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			detail, ok := authDetails[r.URL.Query().Get("name")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(detail)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			var payload struct {
				AuthIndex string `json:"auth_index"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			statusCode := 200
			usedPercent := 100
			if payload.AuthIndex == "bad-token.json" {
				statusCode = 401
				usedPercent = 0
			}
			body := map[string]any{
				"plan_type": "free",
				"rate_limit": map[string]any{
					"primary_window": map[string]any{
						"used_percent":        usedPercent,
						"reset_after_seconds": 3600,
					},
				},
			}
			if payload.AuthIndex == "overloaded.json" {
				statusCode = 401
				body = map[string]any{
					"error": map[string]any{
						"type":    "service_unavailable_error",
						"code":    "server_is_overloaded",
						"message": "Our servers are currently overloaded. Please try again later.",
						"param":   nil,
					},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": statusCode,
				"body":        body,
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/status":
			mu.Lock()
			statusPatchCount++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			mu.Lock()
			priorityPatchCount++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":       "0 0 29 2 *",
		"dry_run":             false,
		"quota_threshold":     50,
		"worker_threads":      1,
		"cpa_timeout_seconds": 1,
	}, cookies, nil)

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/refresh", map[string]any{
		"auth_names": []string{"quota-high.json", "bad-token.json", "overloaded.json"},
	}, cookies, nil)
	response := waitForKeeperAccounts(t, handler, cookies, 3)

	items := map[string]struct {
		Disabled       bool
		Priority       *int
		LastStatusCode *int
		LastError      *string
		LatestAction   *string
	}{}
	for _, item := range response.Items {
		items[item.Name] = struct {
			Disabled       bool
			Priority       *int
			LastStatusCode *int
			LastError      *string
			LatestAction   *string
		}{
			Disabled:       item.Disabled,
			Priority:       item.Priority,
			LastStatusCode: item.LastStatusCode,
			LastError:      item.LastError,
			LatestAction:   item.LatestAction,
		}
	}

	quota := items["quota-high.json"]
	if quota.Priority == nil || *quota.Priority != -1 {
		t.Fatalf("quota-high priority = %v, want -1", quota.Priority)
	}
	if quota.LastError != nil {
		t.Fatalf("quota-high last_error = %v, want nil", *quota.LastError)
	}
	bad := items["bad-token.json"]
	if !bad.Disabled {
		t.Fatal("bad-token disabled = false, want refresh to disable bad credential")
	}
	if bad.LastStatusCode == nil || *bad.LastStatusCode != 401 {
		t.Fatalf("bad-token last_status_code = %v, want 401", bad.LastStatusCode)
	}
	if bad.LastError == nil || !strings.Contains(*bad.LastError, "凭证不可用") {
		t.Fatalf("bad-token last_error = %v, want credential error", bad.LastError)
	}
	if bad.LatestAction == nil || !strings.Contains(*bad.LatestAction, "禁用凭证") {
		t.Fatalf("bad-token latest_action = %v, want keeper disable action", bad.LatestAction)
	}
	overloaded := items["overloaded.json"]
	if overloaded.Disabled {
		t.Fatal("overloaded credential disabled = true, want transient warning to remain enabled")
	}
	if overloaded.LastStatusCode == nil || *overloaded.LastStatusCode != 401 {
		t.Fatalf("overloaded last_status_code = %v, want 401", overloaded.LastStatusCode)
	}
	if overloaded.LastError == nil || !strings.Contains(*overloaded.LastError, "server_is_overloaded") {
		t.Fatalf("overloaded last_error = %v, want overload warning", overloaded.LastError)
	}
	if overloaded.LatestAction == nil || strings.Contains(*overloaded.LatestAction, "禁用凭证") {
		t.Fatalf("overloaded latest_action = %v, want no disable action", overloaded.LatestAction)
	}

	mu.Lock()
	defer mu.Unlock()
	if statusPatchCount != 1 {
		t.Fatalf("status patch count = %d, want 1", statusPatchCount)
	}
	if priorityPatchCount != 2 {
		t.Fatalf("priority patch count = %d, want 2", priorityPatchCount)
	}
}

func TestKeeperRunPreservesBadCredentialDiagnosisAfterRemoteDisabled(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	const authName = "bad-token.json"
	authDetails := map[string]map[string]any{
		authName: {
			"name":         authName,
			"type":         "codex",
			"email":        "bad@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     4,
			"access_token": "bad-token",
			"auth_index":   authName,
		},
	}
	var mu sync.Mutex
	usageCalls := 0
	statusPatchCount := 0
	priorityPatchCount := 0

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{{"name": authName, "type": "codex"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			mu.Lock()
			detail, ok := authDetails[name]
			if !ok {
				mu.Unlock()
				http.NotFound(w, r)
				return
			}
			copied := map[string]any{}
			for key, value := range detail {
				copied[key] = value
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(copied)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			mu.Lock()
			usageCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 401,
				"body": map[string]any{
					"error": map[string]any{
						"message": "Your authentication token has been invalidated.",
						"code":    "token_invalidated",
					},
					"status": 401,
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/status":
			var payload struct {
				Name     string `json:"name"`
				Disabled bool   `json:"disabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			statusPatchCount++
			if detail, ok := authDetails[payload.Name]; ok {
				detail["disabled"] = payload.Disabled
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			var payload struct {
				Name     string `json:"name"`
				Priority *int   `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			priorityPatchCount++
			if detail, ok := authDetails[payload.Name]; ok {
				if payload.Priority == nil {
					delete(detail, "priority")
				} else {
					detail["priority"] = *payload.Priority
				}
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":       "0 0 29 2 *",
		"dry_run":             false,
		"worker_threads":      1,
		"cpa_timeout_seconds": 1,
	}, cookies, nil)

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/run-once", nil, cookies, nil)
	response := waitForKeeperAccounts(t, handler, cookies, 1)
	item := response.Items[0]
	if !item.Disabled {
		t.Fatal("bad-token disabled = false, want Keeper to disable bad credential")
	}
	if item.LastStatusCode == nil || *item.LastStatusCode != 401 {
		t.Fatalf("bad-token first last_status_code = %v, want 401", item.LastStatusCode)
	}
	if item.LastError == nil || !strings.Contains(*item.LastError, "凭证不可用") {
		t.Fatalf("bad-token first last_error = %v, want credential error", item.LastError)
	}
	if item.LatestAction == nil || !strings.Contains(*item.LatestAction, "禁用凭证") {
		t.Fatalf("bad-token first latest_action = %v, want keeper disable action", item.LatestAction)
	}

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/run-once", nil, cookies, nil)
	response = waitForKeeperAccounts(t, handler, cookies, 1)
	item = response.Items[0]
	if item.LastStatusCode == nil || *item.LastStatusCode != 401 {
		t.Fatalf("bad-token second last_status_code = %v, want preserved 401", item.LastStatusCode)
	}
	if item.LastError == nil || !strings.Contains(*item.LastError, "凭证不可用") {
		t.Fatalf("bad-token second last_error = %v, want preserved credential error", item.LastError)
	}
	if item.LatestAction == nil || !strings.Contains(*item.LatestAction, "禁用凭证") {
		t.Fatalf("bad-token second latest_action = %v, want preserved keeper disable action", item.LatestAction)
	}

	mu.Lock()
	defer mu.Unlock()
	if usageCalls != 2 {
		t.Fatalf("usage calls = %d, want 2 because recoverable 401-disabled account is rechecked", usageCalls)
	}
	if statusPatchCount != 1 {
		t.Fatalf("status patch count = %d, want 1", statusPatchCount)
	}
	if priorityPatchCount != 2 {
		t.Fatalf("priority patch count = %d, want 2", priorityPatchCount)
	}
}

func TestKeeperRefreshDisabledAccountChecksUsageAndRecordsBadCredential(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	const authName = "disabled-bad-token.json"
	authDetails := map[string]map[string]any{
		authName: {
			"name":         authName,
			"type":         "codex",
			"email":        "disabled-bad@example.com",
			"account_type": "free",
			"disabled":     true,
			"priority":     4,
			"access_token": "bad-token",
			"auth_index":   authName,
		},
	}
	var mu sync.Mutex
	usageCalls := 0
	statusPatchCount := 0
	priorityPatchCount := 0

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{{"name": authName, "type": "codex"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			mu.Lock()
			detail, ok := authDetails[name]
			if !ok {
				mu.Unlock()
				http.NotFound(w, r)
				return
			}
			copied := map[string]any{}
			for key, value := range detail {
				copied[key] = value
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(copied)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			mu.Lock()
			usageCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 401,
				"body": map[string]any{
					"error": map[string]any{
						"message": "Your authentication token has been invalidated.",
						"type":    "invalid_request_error",
						"code":    "token_invalidated",
					},
					"status": 401,
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/status":
			mu.Lock()
			statusPatchCount++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			var payload struct {
				Name     string `json:"name"`
				Priority *int   `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			priorityPatchCount++
			if detail, ok := authDetails[payload.Name]; ok {
				if payload.Priority == nil {
					delete(detail, "priority")
				} else {
					detail["priority"] = *payload.Priority
				}
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":       "0 0 29 2 *",
		"dry_run":             false,
		"worker_threads":      1,
		"cpa_timeout_seconds": 1,
	}, cookies, nil)

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/refresh", map[string]any{
		"auth_names": []string{authName},
	}, cookies, nil)
	response := waitForKeeperAccounts(t, handler, cookies, 1)
	item := response.Items[0]
	if !item.Disabled {
		t.Fatal("disabled-bad-token disabled = false, want true")
	}
	if item.LastStatusCode == nil || *item.LastStatusCode != 401 {
		t.Fatalf("disabled-bad-token last_status_code = %v, want 401", item.LastStatusCode)
	}
	if item.LastError == nil || !strings.Contains(*item.LastError, "凭证不可用") {
		t.Fatalf("disabled-bad-token last_error = %v, want credential error", item.LastError)
	}
	if item.LatestAction == nil || !strings.Contains(*item.LatestAction, "禁用凭证") {
		t.Fatalf("disabled-bad-token latest_action = %v, want keeper disable action", item.LatestAction)
	}

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "db", "cpa_helper.sqlite3")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()
	var runCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM codex_keeper_runs`).Scan(&runCount); err != nil {
		t.Fatalf("read keeper run count: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("keeper run count = %d, want 0 because account refresh is not persisted", runCount)
	}

	mu.Lock()
	defer mu.Unlock()
	if usageCalls != 1 {
		t.Fatalf("usage calls = %d, want 1", usageCalls)
	}
	if statusPatchCount != 0 {
		t.Fatalf("status patch count = %d, want 0 because account was already disabled", statusPatchCount)
	}
	if priorityPatchCount != 1 {
		t.Fatalf("priority patch count = %d, want 1", priorityPatchCount)
	}
}

func TestKeeperAccountsReturnBeijingOffsetTimeStrings(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "db", "cpa_helper.sqlite3")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		INSERT INTO codex_keeper_auth_states (
			auth_name, email, disabled, primary_reset_at, last_checked_at,
			last_healthy_at, created_at, updated_at
		) VALUES (?, ?, 0, ?, ?, ?, ?, ?)
	`,
		"sample.json",
		"user001@example.com",
		"2026-05-14 01:02:03.654321",
		"2026-05-13 12:00:01.123456",
		"2026-05-13 12:00:02.123456",
		"2026-05-13 11:59:58.000000",
		"2026-05-13 11:59:58.000000",
	)
	if err != nil {
		t.Fatalf("insert keeper account state: %v", err)
	}

	response := keeperAccountsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &response)
	if len(response.Items) != 1 {
		t.Fatalf("accounts length = %d, want 1", len(response.Items))
	}
	item := response.Items[0]
	if item.Name != "sample.json" {
		t.Fatalf("account name = %q, want sample.json", item.Name)
	}
	if item.Priority == nil || *item.Priority != 0 {
		t.Fatalf("priority = %v, want 0 when stored value is NULL", item.Priority)
	}
	if got := stringPtrValue(item.LastCheckedAt); got != "2026-05-13T12:00:01+08:00" {
		t.Fatalf("last_checked_at = %q, want Beijing offset time", got)
	}
	if got := stringPtrValue(item.LastHealthyAt); got != "2026-05-13T12:00:02+08:00" {
		t.Fatalf("last_healthy_at = %q, want Beijing offset time", got)
	}
	if got := stringPtrValue(item.PrimaryResetAt); got != "2026-05-14T01:02:03+08:00" {
		t.Fatalf("primary_reset_at = %q, want Beijing offset time", got)
	}
}

func TestDBBackedAPIsReturnBeijingOffsetTimeStrings(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "db", "cpa_helper.sqlite3")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		UPDATE users
		SET created_at = ?, updated_at = ?
		WHERE id = 1
	`, "2026-05-06 16:04:17.286273", "2026-05-09 20:44:15.891099")
	if err != nil {
		t.Fatalf("update user times: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
		VALUES (?, 1, ?, ?, ?, ?)
	`, "hash-for-time-test", "sk-test", "time key", "2026-05-08 22:04:51.729598", "2026-05-09 20:44:15.891099")
	if err != nil {
		t.Fatalf("insert api key times: %v", err)
	}
	_, err = db.Exec(`
		UPDATE collector_state
		SET last_poll_at = ?, last_success_at = ?, updated_at = ?
		WHERE id = 1
	`, "2026-05-13 12:34:56.123456", "2026-05-13 12:35:01.123456", "2026-05-13 12:35:02.123456")
	if err != nil {
		t.Fatalf("update collector times: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million, source,
			source_model, auto_synced, last_synced_at, updated_at
		) VALUES (?, ?, 1, 2, 0, 0, 'manual', NULL, 0, ?, ?)
	`, "openai", "gpt-time-test", "2026-05-10 08:09:10.123456", "2026-05-10 08:09:11.123456")
	if err != nil {
		t.Fatalf("insert model price times: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO usage_records (
			created_at, timestamp, usage_username, api_key_description, provider,
			model, endpoint, source, request_id, auth, latency_ms, failed,
			input_tokens, output_tokens, cached_tokens, reasoning_tokens,
			total_tokens, dedupe_key, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 1, 2, 0, 0, 3, ?, '{}')
	`, "2026-05-13T12:47:53+08:00", "2026-05-13T12:47:44+08:00", "admin", "time key", "openai", "gpt-time-test", "/v1/chat/completions", "test", "req-time", "auth", 123.0, "dedupe-time-test")
	if err != nil {
		t.Fatalf("insert usage record times: %v", err)
	}

	collector := collectorStatusTimeResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/collector/status", nil, cookies, &collector)
	if got := stringPtrValue(collector.LastPollAt); got != "2026-05-13T12:34:56+08:00" {
		t.Fatalf("collector last_poll_at = %q, want Beijing offset string", got)
	}
	if got := stringPtrValue(collector.LastSuccessAt); got != "2026-05-13T12:35:01+08:00" {
		t.Fatalf("collector last_success_at = %q, want Beijing offset string", got)
	}

	users := userTimesResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/users", nil, cookies, &users)
	if len(users) == 0 {
		t.Fatal("users response is empty")
	}
	if got := users[0].CreatedAt; got != "2026-05-06T16:04:17+08:00" {
		t.Fatalf("user created_at = %q, want Beijing offset string", got)
	}
	if got := users[0].UpdatedAt; got != "2026-05-09T20:44:15+08:00" {
		t.Fatalf("user updated_at = %q, want Beijing offset string", got)
	}
	if len(users[0].APIKeys) == 0 {
		t.Fatal("user api_keys response is empty")
	}
	if got := stringPtrValue(users[0].APIKeys[0].CreatedAt); got != "2026-05-08T22:04:51+08:00" {
		t.Fatalf("api key created_at = %q, want Beijing offset string", got)
	}

	prices := modelPriceTimesResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/model-prices", nil, cookies, &prices)
	if len(prices) == 0 {
		t.Fatal("model prices response is empty")
	}
	if got := stringPtrValue(prices[0].LastSyncedAt); got != "2026-05-10T08:09:10+08:00" {
		t.Fatalf("model price last_synced_at = %q, want Beijing offset string", got)
	}
	if got := prices[0].UpdatedAt; got != "2026-05-10T08:09:11+08:00" {
		t.Fatalf("model price updated_at = %q, want Beijing offset string", got)
	}

	records := usageRecordsTimeResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records?scope=admin&page=1&page_size=1&start=2026-05-13T00:00:00&end=2026-05-14T00:00:00", nil, cookies, &records)
	if len(records.Items) != 1 {
		t.Fatalf("usage records length = %d, want 1", len(records.Items))
	}
	if got := records.Items[0].Timestamp; got != "2026-05-13T12:47:44+08:00" {
		t.Fatalf("usage timestamp = %q, want Beijing offset string", got)
	}
}

func assertStandardKeeperLogLine(t *testing.T, line string) {
	t.Helper()
	pattern := regexp.MustCompile(`^time=\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}\+08:00 level=INFO msg=.+ component=codex_keeper$`)
	if !pattern.MatchString(line) {
		t.Fatalf("keeper log line %q does not match standard format", line)
	}
}

func keeperLogDate(t *testing.T, line string) string {
	t.Helper()
	if len(line) < len("time=2006-01-02") || !strings.HasPrefix(line, "time=") {
		t.Fatalf("keeper log line %q does not include a leading timestamp", line)
	}
	return line[len("time="):len("time=2006-01-02")]
}

func waitForKeeperAccounts(t *testing.T, handler http.Handler, cookies []*http.Cookie, expected int) keeperAccountsResponse {
	t.Helper()
	response := keeperAccountsResponse{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		statusBefore := keeperStatusResponse{}
		requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &statusBefore)
		if statusBefore.Running {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		response = keeperAccountsResponse{}
		requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &response)
		statusAfter := keeperStatusResponse{}
		requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &statusAfter)
		if len(response.Items) == expected && !statusAfter.Running {
			return response
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("accounts length = %d, want %d after keeper run completed", len(response.Items), expected)
	return response
}

func waitForKeeperAccountChecked(t *testing.T, handler http.Handler, cookies []*http.Cookie, name string) keeperAccountsResponse {
	t.Helper()
	response := keeperAccountsResponse{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		statusBefore := keeperStatusResponse{}
		requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &statusBefore)
		if statusBefore.Running {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		response = keeperAccountsResponse{}
		requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &response)
		statusAfter := keeperStatusResponse{}
		requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &statusAfter)
		for _, item := range response.Items {
			if item.Name == name && item.LastCheckedAt != nil && !statusAfter.Running {
				return response
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s was not checked after keeper run completed", name)
	return response
}

func assertKeeperPriorities(t *testing.T, response keeperAccountsResponse, expected map[string]int) {
	t.Helper()
	priorities := map[string]int{}
	for _, item := range response.Items {
		if item.Priority == nil {
			t.Fatalf("%s priority is nil, want maintained priority", item.Name)
		}
		priorities[item.Name] = *item.Priority
	}
	for name, want := range expected {
		if got := priorities[name]; got != want {
			t.Fatalf("%s priority = %d, want %d", name, got, want)
		}
	}
}

func assertKeeperPriorityPatchesLocked(t *testing.T, patches map[string][]int, expected map[string][]int) {
	t.Helper()
	if len(patches) != len(expected) {
		t.Fatalf("priority patches = %#v, want %#v", patches, expected)
	}
	for name, want := range expected {
		got := patches[name]
		if len(got) != len(want) {
			t.Fatalf("%s patched priorities = %#v, want %#v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s patched priorities = %#v, want %#v", name, got, want)
			}
		}
	}
}

func requestJSON(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body any,
	cookies []*http.Cookie,
	target any,
) []*http.Cookie {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code < 200 || recorder.Code >= 300 {
		t.Fatalf("%s %s returned %d: %s", method, path, recorder.Code, recorder.Body.String())
	}
	if target != nil {
		if err := json.NewDecoder(recorder.Body).Decode(target); err != nil {
			t.Fatalf("decode %s %s response: %v", method, path, err)
		}
	}
	return append(cookies, recorder.Result().Cookies()...)
}

func requestJSONExpectStatus(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body any,
	cookies []*http.Cookie,
	expected int,
) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != expected {
		t.Fatalf("%s %s returned %d, want %d: %s", method, path, recorder.Code, expected, recorder.Body.String())
	}
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
