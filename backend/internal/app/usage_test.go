package app_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	backendApp "cpa-helper/backend/internal/app"
)

type usageRecordsResponse struct {
	Items []struct {
		Timestamp          string   `json:"timestamp"`
		ID                 int      `json:"id"`
		Source             string   `json:"source"`
		RequestID          *string  `json:"request_id"`
		Model              *string  `json:"model"`
		ReasoningEffort    *string  `json:"reasoning_effort"`
		RequestServiceTier *string  `json:"request_service_tier"`
		TTFTMS             *float64 `json:"ttft_ms"`
		AuthIndex          *string  `json:"auth_index"`
	} `json:"items"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type usageRecordDetailResponse struct {
	Source             string         `json:"source"`
	Model              *string        `json:"model"`
	ReasoningEffort    *string        `json:"reasoning_effort"`
	RequestServiceTier *string        `json:"request_service_tier"`
	TTFTMS             *float64       `json:"ttft_ms"`
	AuthIndex          *string        `json:"auth_index"`
	RawJSON            map[string]any `json:"raw_json"`
}

type usageSummaryResponse struct {
	Start            string   `json:"start"`
	End              string   `json:"end"`
	TotalRecords     int      `json:"total_records"`
	TotalTokens      int      `json:"total_tokens"`
	AverageTTFTMS    *float64 `json:"average_ttft_ms"`
	ZeroTokenRecords int      `json:"zero_token_records"`
}

type usageOverviewDistributionsResponse struct {
	Summary       usageSummaryResponse `json:"summary"`
	Distributions struct {
		Models []usageDistributionItem `json:"models"`
	} `json:"distributions"`
}

type usageOptionsTestResponse struct {
	Users []struct {
		Label  string `json:"label"`
		UserID *int   `json:"user_id"`
	} `json:"users"`
	APIKeyDescriptions []struct {
		Key string `json:"key"`
	} `json:"api_key_descriptions"`
	Providers []string `json:"providers"`
	Models    []string `json:"models"`
	Sources   []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	} `json:"sources"`
	Endpoints []string `json:"endpoints"`
}

type usageOverviewOptionsResponse struct {
	Options usageOptionsTestResponse `json:"options"`
}

type optimizedUsageOverviewResponse struct {
	Summary struct {
		TotalRecords int `json:"total_records"`
	} `json:"summary"`
	FailedSummary struct {
		TotalRecords int `json:"total_records"`
	} `json:"failed_summary"`
	FailedTrends               []map[string]any          `json:"failed_trends"`
	FailedEndpointDistribution []usageDistributionItem   `json:"failed_endpoint_distribution"`
	TodayTrends                []map[string]any          `json:"today_trends"`
	RealtimeSummary            usageSummaryResponse      `json:"realtime_summary"`
	Options                    *usageOptionsTestResponse `json:"options"`
}

type usageHistoryAccessUserResponse struct {
	CanViewUsageHistory bool `json:"can_view_usage_history"`
}

type usageHistoryOverviewResponse struct {
	Summary struct {
		TotalRecords int `json:"total_records"`
	} `json:"summary"`
}

type usageDistributionItem struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Records     int    `json:"records"`
	TotalTokens int    `json:"total_tokens"`
}

func TestUsageRecordsReturnBeijingOffsetTimes(t *testing.T) {
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
	seedUsageRecord(t, dataDir, "2026-05-16T16:37:00+08:00")

	const recordsPath = "/api/usage/records?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00"
	records := usageRecordsResponse{}
	requestJSON(t, handler, http.MethodGet, recordsPath, nil, cookies, &records)
	if len(records.Items) != 1 {
		t.Fatalf("usage record count = %d, want 1", len(records.Items))
	}
	if records.Items[0].Timestamp != "2026-05-16T16:37:00+08:00" {
		t.Fatalf("timestamp = %q, want Beijing offset value", records.Items[0].Timestamp)
	}
	if records.Start != "2026-05-16T00:00:00+08:00" || records.End != "2026-05-17T00:00:00+08:00" {
		t.Fatalf("range = %q - %q, want Beijing offset range", records.Start, records.End)
	}

	const summaryPath = "/api/usage/summary?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00"
	summary := usageSummaryResponse{}
	requestJSON(t, handler, http.MethodGet, summaryPath, nil, cookies, &summary)
	if summary.Start != "2026-05-16T00:00:00+08:00" || summary.End != "2026-05-17T00:00:00+08:00" {
		t.Fatalf("summary range = %q - %q, want Beijing offset range", summary.Start, summary.End)
	}
}

func TestUsageRecordsExposeReasoningEffortTTFTAndSummaryAverage(t *testing.T) {
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

	firstTTFT := 710.0
	secondTTFT := 290.0
	zeroTTFT := 0.0
	firstID := seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:          "2026-05-16T16:37:00+08:00",
		Username:           "admin",
		APIKeyDescription:  "VSCode",
		Provider:           "openai",
		Model:              "gpt-5.5",
		Endpoint:           "/v1/responses",
		Source:             "code10001",
		RequestID:          "req-ttft-1",
		Auth:               "bearer",
		DedupeKey:          "ttft-1",
		RawJSON:            `{"request_id":"req-ttft-1"}`,
		ReasoningEffort:    "xhigh",
		RequestServiceTier: "priority",
		TTFTMS:             &firstTTFT,
		InputTokens:        10,
		OutputTokens:       2,
		TotalTokens:        12,
	})
	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:    "2026-05-16T16:38:00+08:00",
		Username:     "admin",
		Source:       "code10002",
		RequestID:    "req-ttft-2",
		Auth:         "bearer",
		DedupeKey:    "ttft-2",
		RawJSON:      `{"request_id":"req-ttft-2"}`,
		TTFTMS:       &secondTTFT,
		InputTokens:  0,
		OutputTokens: 0,
		TotalTokens:  0,
	})
	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:    "2026-05-16T16:39:00+08:00",
		Username:     "admin",
		Source:       "code10003",
		RequestID:    "req-ttft-zero",
		Auth:         "bearer",
		DedupeKey:    "ttft-zero",
		RawJSON:      `{"request_id":"req-ttft-zero"}`,
		TTFTMS:       &zeroTTFT,
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  12,
	})

	const recordsPath = "/api/usage/records?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00"
	records := usageRecordsResponse{}
	requestJSON(t, handler, http.MethodGet, recordsPath, nil, cookies, &records)
	if len(records.Items) != 3 {
		t.Fatalf("usage record count = %d, want 3", len(records.Items))
	}
	var firstItem *struct {
		Timestamp          string   `json:"timestamp"`
		ID                 int      `json:"id"`
		Source             string   `json:"source"`
		RequestID          *string  `json:"request_id"`
		Model              *string  `json:"model"`
		ReasoningEffort    *string  `json:"reasoning_effort"`
		RequestServiceTier *string  `json:"request_service_tier"`
		TTFTMS             *float64 `json:"ttft_ms"`
		AuthIndex          *string  `json:"auth_index"`
	}
	for index := range records.Items {
		if records.Items[index].RequestID != nil && *records.Items[index].RequestID == "req-ttft-1" {
			firstItem = &records.Items[index]
			break
		}
	}
	if firstItem == nil || firstItem.Model == nil || *firstItem.Model != "gpt-5.5" || firstItem.ReasoningEffort == nil || *firstItem.ReasoningEffort != "xhigh" || firstItem.RequestServiceTier == nil || *firstItem.RequestServiceTier != "priority" || firstItem.TTFTMS == nil || *firstItem.TTFTMS != 710 {
		t.Fatalf("first list item = %#v, want model/reasoning/ttft", firstItem)
	}

	detail := usageRecordDetailResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records/"+strconv.Itoa(firstID)+"?scope=admin", nil, cookies, &detail)
	if detail.ReasoningEffort == nil || *detail.ReasoningEffort != "xhigh" || detail.RequestServiceTier == nil || *detail.RequestServiceTier != "priority" || detail.TTFTMS == nil || *detail.TTFTMS != 710 {
		t.Fatalf("detail reasoning/ttft = %#v/%#v, want xhigh/710", detail.ReasoningEffort, detail.TTFTMS)
	}

	summary := usageSummaryResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/summary?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00", nil, cookies, &summary)
	if summary.AverageTTFTMS == nil || *summary.AverageTTFTMS != 500 {
		t.Fatalf("average_ttft_ms = %#v, want 500", summary.AverageTTFTMS)
	}
	if summary.ZeroTokenRecords != 1 {
		t.Fatalf("zero_token_records = %d, want 1", summary.ZeroTokenRecords)
	}

	overview := usageOverviewDistributionsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/overview?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00", nil, cookies, &overview)
	if overview.Summary.AverageTTFTMS == nil || *overview.Summary.AverageTTFTMS != 500 {
		t.Fatalf("overview average_ttft_ms = %#v, want 500", overview.Summary.AverageTTFTMS)
	}
}

func TestUsageOverviewIncludesModelDistribution(t *testing.T) {
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
	seedUsageRecord(t, dataDir, "2026-05-16T16:37:00+08:00")

	const overviewPath = "/api/usage/overview?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00"
	overview := usageOverviewDistributionsResponse{}
	requestJSON(t, handler, http.MethodGet, overviewPath, nil, cookies, &overview)
	if len(overview.Distributions.Models) != 1 {
		t.Fatalf("model distribution count = %d, want 1", len(overview.Distributions.Models))
	}
	item := overview.Distributions.Models[0]
	if item.Key != "gpt-5.5" || item.Label != "gpt-5.5" {
		t.Fatalf("model distribution item = %#v, want gpt-5.5", item)
	}
	if item.Records != 1 || item.TotalTokens != 12 {
		t.Fatalf("model distribution totals = records %d tokens %d, want 1 and 12", item.Records, item.TotalTokens)
	}
}

func TestUsageOverviewReturnsAuxiliaryMetricsWithoutOptions(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)
	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp: "2026-05-16T16:37:00+08:00", Username: "admin", Source: "source-success",
		RequestID: "req-overview-success", Auth: "bearer", DedupeKey: "overview-success",
		RawJSON: `{"request_id":"req-overview-success"}`, InputTokens: 10, OutputTokens: 2, TotalTokens: 12,
	})
	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp: "2026-05-16T16:38:00+08:00", Username: "admin", Source: "source-failed",
		RequestID: "req-overview-failed", Auth: "bearer", DedupeKey: "overview-failed", Failed: true,
		RawJSON: `{"request_id":"req-overview-failed","failed":true}`, InputTokens: 5, OutputTokens: 1, TotalTokens: 6,
	})

	const path = "/api/usage/overview?scope=admin&include_options=false&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00"
	overview := optimizedUsageOverviewResponse{}
	requestJSON(t, handler, http.MethodGet, path, nil, cookies, &overview)
	if overview.Summary.TotalRecords != 2 || overview.FailedSummary.TotalRecords != 1 {
		t.Fatalf("overview totals = %d/%d, want 2/1", overview.Summary.TotalRecords, overview.FailedSummary.TotalRecords)
	}
	if len(overview.FailedTrends) != 1 || len(overview.FailedEndpointDistribution) != 1 {
		t.Fatalf("failed auxiliary metrics = trends %d/endpoints %d, want 1/1", len(overview.FailedTrends), len(overview.FailedEndpointDistribution))
	}
	if overview.Options != nil {
		t.Fatal("overview options should be omitted when include_options=false")
	}

	records := map[string]any{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records?scope=admin&include_total=false&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00&page=1&page_size=10", nil, cookies, &records)
	if _, ok := records["total"]; ok {
		t.Fatal("records total should be omitted when include_total=false")
	}
}

func TestUsageOverviewRealtimeSummaryIgnoresSelectedTimeRange(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)

	// Use UTC even on a Beijing-time workstation: query parameters may carry any
	// offset, while records written to the database must use the canonical +08:00.
	now := time.Now().UTC()
	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), Username: "admin", Source: "realtime-source",
		RequestID: "req-realtime", Auth: "bearer", DedupeKey: "realtime-window",
		RawJSON: `{"request_id":"req-realtime"}`, InputTokens: 500, OutputTokens: 100, TotalTokens: 600,
	})

	loadOverview := func(start, end time.Time) optimizedUsageOverviewResponse {
		query := url.Values{
			"scope":           {"admin"},
			"include_options": {"false"},
			"start":           {start.Format(time.RFC3339Nano)},
			"end":             {end.Format(time.RFC3339Nano)},
		}
		response := optimizedUsageOverviewResponse{}
		requestJSON(t, handler, http.MethodGet, "/api/usage/overview?"+query.Encode(), nil, cookies, &response)
		return response
	}

	recentRange := loadOverview(now.Add(-30*time.Minute), now.Add(time.Minute))
	historicRange := loadOverview(now.Add(-48*time.Hour), now.Add(-47*time.Hour))
	for label, response := range map[string]optimizedUsageOverviewResponse{
		"recent": recentRange, "historic": historicRange,
	} {
		if response.RealtimeSummary.TotalRecords != 1 || response.RealtimeSummary.TotalTokens != 600 {
			t.Fatalf("%s realtime summary = records %d tokens %d, want 1/600", label,
				response.RealtimeSummary.TotalRecords, response.RealtimeSummary.TotalTokens)
		}
	}
}

func TestUsageOptionsRespectDateRange(t *testing.T) {
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
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "member",
		"password": "member-password",
		"nickname": "Member",
		"is_admin": false,
	}, cookies, nil)

	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:         "2026-05-16T16:37:00+08:00",
		Username:          "admin",
		APIKeyDescription: "VSCode",
		Provider:          "openai",
		Model:             "gpt-5.5",
		Endpoint:          "/v1/chat/completions",
		Source:            "code10001",
		RequestID:         "req-time-options-inside",
		Auth:              "bearer",
		DedupeKey:         "time-options-inside",
		RawJSON:           `{"request_id":"req-time-options-inside"}`,
		InputTokens:       10,
		OutputTokens:      2,
		TotalTokens:       12,
	})
	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:         "2026-05-18T16:37:00+08:00",
		Username:          "member",
		APIKeyDescription: "Minis",
		Provider:          "claude",
		Model:             "claude-sonnet-4",
		Endpoint:          "/v1/messages",
		Source:            "code10002",
		RequestID:         "req-time-options-outside",
		Auth:              "bearer",
		DedupeKey:         "time-options-outside",
		RawJSON:           `{"request_id":"req-time-options-outside"}`,
		InputTokens:       10,
		OutputTokens:      2,
		TotalTokens:       12,
	})

	const optionsPath = "/api/usage/options?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00"
	options := usageOptionsTestResponse{}
	requestJSON(t, handler, http.MethodGet, optionsPath, nil, cookies, &options)
	assertStringSlice(t, options.Providers, []string{"openai"})
	assertStringSlice(t, options.Models, []string{"gpt-5.5"})
	if len(options.Sources) != 1 || options.Sources[0].Label != "code10001" || options.Sources[0].Key != sourceKeyForTest("code10001") {
		t.Fatalf("source options = %#v, want only code10001", options.Sources)
	}
	assertStringSlice(t, options.Endpoints, []string{"/v1/chat/completions"})
	if len(options.APIKeyDescriptions) != 1 || options.APIKeyDescriptions[0].Key != "VSCode" {
		t.Fatalf("api key description options = %#v, want only VSCode", options.APIKeyDescriptions)
	}
	if len(options.Users) != 1 || options.Users[0].Label != "Admin" || options.Users[0].UserID == nil {
		t.Fatalf("user options = %#v, want only Admin", options.Users)
	}

	const overviewPath = "/api/usage/overview?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00"
	overview := usageOverviewOptionsResponse{}
	requestJSON(t, handler, http.MethodGet, overviewPath, nil, cookies, &overview)
	assertStringSlice(t, overview.Options.Providers, []string{"openai"})
	assertStringSlice(t, overview.Options.Models, []string{"gpt-5.5"})
	if len(overview.Options.Sources) != 1 || overview.Options.Sources[0].Label != "code10001" || overview.Options.Sources[0].Key != sourceKeyForTest("code10001") {
		t.Fatalf("overview source options = %#v, want only code10001", overview.Options.Sources)
	}
	assertStringSlice(t, overview.Options.Endpoints, []string{"/v1/chat/completions"})
}

func TestSharedUsageHistoryRequiresSettingAndExcludesRecords(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

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
	member := usageHistoryAccessUserResponse{}
	memberCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "member",
		"password": "member-password",
	}, nil, &member)
	if member.CanViewUsageHistory {
		t.Fatal("can_view_usage_history = true, want false before access is enabled")
	}

	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:   "2026-05-16T10:00:00+08:00",
		Username:    "admin",
		Provider:    "openai",
		Model:       "gpt-5.5",
		DedupeKey:   "shared-history-admin",
		RawJSON:     `{"request_id":"shared-history-admin"}`,
		InputTokens: 10,
		TotalTokens: 10,
	})
	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:   "2026-05-16T11:00:00+08:00",
		Username:    "member",
		Provider:    "openai",
		Model:       "gpt-5.5",
		DedupeKey:   "shared-history-member",
		RawJSON:     `{"request_id":"shared-history-member"}`,
		InputTokens: 20,
		TotalTokens: 20,
	})

	const overviewPath = "/api/usage/overview?scope=shared&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00"
	requestJSONExpectStatus(t, handler, http.MethodGet, overviewPath, nil, memberCookies, http.StatusForbidden)

	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"allow_user_usage_history": true,
	}, adminCookies, nil)
	member = usageHistoryAccessUserResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/auth/me", nil, memberCookies, &member)
	if !member.CanViewUsageHistory {
		t.Fatal("can_view_usage_history = false, want true after access is enabled")
	}

	overview := usageHistoryOverviewResponse{}
	requestJSON(t, handler, http.MethodGet, overviewPath, nil, memberCookies, &overview)
	if overview.Summary.TotalRecords != 2 {
		t.Fatalf("shared total_records = %d, want 2 across all users", overview.Summary.TotalRecords)
	}
	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/usage/records?scope=shared", nil, memberCookies, http.StatusForbidden)
}

func TestUsageRecordsFilterBySourceForAdmin(t *testing.T) {
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

	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:    "2026-05-16T16:37:00+08:00",
		Username:     "admin",
		Source:       "vscode-source",
		RequestID:    "req-source-filter-a",
		Auth:         "bearer",
		DedupeKey:    "source-filter-a",
		RawJSON:      `{"request_id":"req-source-filter-a"}`,
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  12,
	})
	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:    "2026-05-16T16:38:00+08:00",
		Username:     "admin",
		Source:       "browser-source",
		RequestID:    "req-source-filter-b",
		Auth:         "bearer",
		DedupeKey:    "source-filter-b",
		RawJSON:      `{"request_id":"req-source-filter-b"}`,
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  12,
	})

	records := usageRecordsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records?scope=admin&source_key="+sourceKeyForTest("vscode-source")+"&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00&page=1&page_size=10", nil, cookies, &records)
	if len(records.Items) != 1 {
		t.Fatalf("usage record count = %d, want 1", len(records.Items))
	}
	if records.Items[0].Source != "vscode-source" {
		t.Fatalf("source-filtered record source = %q, want vscode-source", records.Items[0].Source)
	}
}

func TestUsageResponsesRedactAPIKeySourceWithoutChangingStoredRecord(t *testing.T) {
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

	const source = "test-api-key-source-redaction-secret-value"
	id := seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:    "2026-05-16T16:37:00+08:00",
		Username:     "admin",
		Source:       source,
		RequestID:    "req-api-key-source",
		Auth:         "apikey",
		DedupeKey:    "api-key-source-test",
		RawJSON:      `{"auth_type":"apikey","source":"` + source + `","api_key":"` + source + `","request_id":"req-api-key-source"}`,
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  12,
	})

	records := usageRecordsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00&page=1&page_size=1", nil, cookies, &records)
	if len(records.Items) != 1 {
		t.Fatalf("usage record count = %d, want 1", len(records.Items))
	}
	if records.Items[0].Source == source || !strings.Contains(records.Items[0].Source, "...") {
		t.Fatalf("list source = %q, want masked API key", records.Items[0].Source)
	}

	detail := usageRecordDetailResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records/"+strconv.Itoa(id)+"?scope=admin", nil, cookies, &detail)
	if detail.Source == source || !strings.Contains(detail.Source, "...") {
		t.Fatalf("detail source = %q, want masked API key", detail.Source)
	}
	if got, _ := detail.RawJSON["source"].(string); got == source || !strings.Contains(got, "...") {
		t.Fatalf("raw_json.source = %q, want masked API key", got)
	}
	if got, _ := detail.RawJSON["api_key"].(string); got == source || !strings.Contains(got, "...") {
		t.Fatalf("raw_json.api_key = %q, want masked API key", got)
	}
	options := usageOptionsTestResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/options?scope=admin&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00", nil, cookies, &options)
	if len(options.Sources) != 1 {
		t.Fatalf("source options = %#v, want masked API key source", options.Sources)
	}
	if options.Sources[0].Key != sourceKeyForTest(source) || options.Sources[0].Label == source || !strings.Contains(options.Sources[0].Label, "...") {
		t.Fatalf("source option = %#v, want masked API key label with stable source key", options.Sources[0])
	}

	storedSource, storedRaw := storedUsageSourceAndRawJSON(t, dataDir, id)
	if storedSource != source {
		t.Fatalf("stored source = %q, want original", storedSource)
	}
	if !strings.Contains(storedRaw, source) {
		t.Fatalf("stored raw_json = %q, want original API key", storedRaw)
	}
}

func TestUsageDetailRedactsSourceWhenRawJSONAuthTypeIsAPIKey(t *testing.T) {
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

	const source = "test-api-key-source-redaction-secret-value"
	id := seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:    "2026-05-16T16:37:00+08:00",
		Username:     "admin",
		Source:       source,
		RequestID:    "req-raw-auth-type-api-key",
		Auth:         "oauth",
		DedupeKey:    "raw-auth-type-api-key-test",
		RawJSON:      `{"auth_type":"apikey","source":"` + source + `","request_id":"req-raw-auth-type-api-key"}`,
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  12,
	})

	detail := usageRecordDetailResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records/"+strconv.Itoa(id)+"?scope=admin", nil, cookies, &detail)
	if detail.Source == source || !strings.Contains(detail.Source, "...") {
		t.Fatalf("detail source = %q, want masked API key source for admin", detail.Source)
	}
	if got, _ := detail.RawJSON["source"].(string); got == source || !strings.Contains(got, "...") {
		t.Fatalf("raw_json.source = %q, want masked API key source for admin", got)
	}
}

func TestUsageRecordDetailRedactsAccountSourceForNonAdminOnly(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

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
	memberCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "member",
		"password": "member-password",
	}, nil, nil)

	const source = "codex-member@example.com"
	const authIndex = "319a3ed7ef9c3080"
	id := seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:    "2026-05-16T16:37:00+08:00",
		Username:     "member",
		Source:       source,
		RequestID:    "req-account-source",
		Auth:         "oauth",
		AuthIndex:    authIndex,
		DedupeKey:    "account-source-test",
		RawJSON:      `{"auth_type":"oauth","auth_index":"` + authIndex + `","source":"` + source + `","request_id":"req-account-source"}`,
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  12,
	})

	adminDetail := usageRecordDetailResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records/"+strconv.Itoa(id)+"?scope=admin", nil, adminCookies, &adminDetail)
	if adminDetail.Source != source {
		t.Fatalf("admin detail source = %q, want original account source", adminDetail.Source)
	}
	if got, _ := adminDetail.RawJSON["source"].(string); got != source {
		t.Fatalf("admin raw_json.source = %q, want original account source", got)
	}
	if adminDetail.AuthIndex == nil || *adminDetail.AuthIndex != authIndex {
		t.Fatalf("admin detail auth_index = %v, want original auth index", adminDetail.AuthIndex)
	}
	if got, _ := adminDetail.RawJSON["auth_index"].(string); got != authIndex {
		t.Fatalf("admin raw_json.auth_index = %q, want original auth index", got)
	}

	memberDetail := usageRecordDetailResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records/"+strconv.Itoa(id)+"?scope=account", nil, memberCookies, &memberDetail)
	if memberDetail.Source == source || !strings.Contains(memberDetail.Source, "...") {
		t.Fatalf("member detail source = %q, want masked account source", memberDetail.Source)
	}
	if got, _ := memberDetail.RawJSON["source"].(string); got == source || !strings.Contains(got, "...") {
		t.Fatalf("member raw_json.source = %q, want masked account source", got)
	}
	if memberDetail.AuthIndex == nil || *memberDetail.AuthIndex == authIndex || !strings.Contains(*memberDetail.AuthIndex, "...") {
		t.Fatalf("member detail auth_index = %v, want masked auth index", memberDetail.AuthIndex)
	}
	if got, _ := memberDetail.RawJSON["auth_index"].(string); got == authIndex || !strings.Contains(got, "...") {
		t.Fatalf("member raw_json.auth_index = %q, want masked auth index", got)
	}

	memberRecords := usageRecordsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records?scope=account&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00&page=1&page_size=1", nil, memberCookies, &memberRecords)
	if len(memberRecords.Items) != 1 {
		t.Fatalf("member usage record count = %d, want 1", len(memberRecords.Items))
	}
	if memberRecords.Items[0].AuthIndex == nil || *memberRecords.Items[0].AuthIndex == authIndex || !strings.Contains(*memberRecords.Items[0].AuthIndex, "...") {
		t.Fatalf("member list auth_index = %v, want masked auth index", memberRecords.Items[0].AuthIndex)
	}
	memberOptions := usageOptionsTestResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/options?scope=account&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00", nil, memberCookies, &memberOptions)
	if len(memberOptions.Sources) != 0 {
		t.Fatalf("member source options = %#v, want hidden sources", memberOptions.Sources)
	}
	filteredMemberRecords := usageRecordsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records?scope=account&source_key=not-present&start=2026-05-16T00:00:00&end=2026-05-17T00:00:00&page=1&page_size=1", nil, memberCookies, &filteredMemberRecords)
	if len(filteredMemberRecords.Items) != 1 {
		t.Fatalf("member source-filtered usage record count = %d, want source filter ignored", len(filteredMemberRecords.Items))
	}

	storedSource, storedRaw := storedUsageSourceAndRawJSON(t, dataDir, id)
	if storedSource != source {
		t.Fatalf("stored source = %q, want original", storedSource)
	}
	if !strings.Contains(storedRaw, source) {
		t.Fatalf("stored raw_json = %q, want original account source", storedRaw)
	}
}

type usageRecordSeed struct {
	Timestamp          string
	Username           string
	APIKeyDescription  string
	Provider           string
	Model              string
	Endpoint           string
	Source             string
	RequestID          string
	Auth               string
	AuthIndex          string
	DedupeKey          string
	RawJSON            string
	ReasoningEffort    string
	RequestServiceTier string
	TTFTMS             *float64
	Failed             bool
	InputTokens        int
	OutputTokens       int
	TotalTokens        int
}

func seedUsageRecord(t *testing.T, dataDir string, timestamp string) {
	t.Helper()
	seedUsageRecordWithValues(t, dataDir, usageRecordSeed{
		Timestamp:    timestamp,
		Username:     "admin",
		Source:       "code10001",
		RequestID:    "req-time-test",
		Auth:         "bearer",
		DedupeKey:    "time-test-1",
		RawJSON:      `{"request_id":"req-time-test"}`,
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  12,
	})
}

func seedUsageRecordWithValues(t *testing.T, dataDir string, seed usageRecordSeed) int {
	t.Helper()

	// This fixture bypasses saveUsageMessage, so mirror its dbTime normalization.
	// Comparing raw UTC/local strings with canonical database timestamps is invalid.
	timestamp, err := time.Parse(time.RFC3339Nano, seed.Timestamp)
	if err != nil {
		t.Fatalf("invalid usage fixture timestamp %q: %v", seed.Timestamp, err)
	}
	storedTimestamp := timestamp.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02T15:04:05.999999-07:00")

	db := openUsageTestDB(t, dataDir)
	defer db.Close()

	description := valueOrDefault(seed.APIKeyDescription, "VSCode")
	provider := valueOrDefault(seed.Provider, "openai")
	model := valueOrDefault(seed.Model, "gpt-5.5")
	endpoint := valueOrDefault(seed.Endpoint, "/v1/chat/completions")
	result, err := db.Exec(`
		INSERT INTO usage_records (
			created_at, timestamp, usage_username, api_key_description, provider, model,
			reasoning_effort, request_service_tier, endpoint, source, source_key, request_id, auth, auth_index, latency_ms, ttft_ms, failed, input_tokens,
			output_tokens, cached_tokens, reasoning_tokens, total_tokens, dedupe_key, raw_json
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1000, ?, ?, ?, ?, 0, 0, ?, ?, ?
		)
	`, storedTimestamp, storedTimestamp, seed.Username, description, provider, model, nullableSeedString(seed.ReasoningEffort), nullableSeedString(seed.RequestServiceTier), endpoint, seed.Source, nullableSeedSourceKey(seed.Source), seed.RequestID, seed.Auth, nullableSeedString(seed.AuthIndex), nullableSeedFloat(seed.TTFTMS), seed.Failed, seed.InputTokens, seed.OutputTokens, seed.TotalTokens, seed.DedupeKey, seed.RawJSON)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func nullableSeedString(value string) any {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil
	}
	return normalized
}

func nullableSeedFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableSeedSourceKey(value string) any {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil
	}
	return sourceKeyForTest(normalized)
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("string slice = %#v, want %#v", got, want)
	}
}

func sourceKeyForTest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func storedUsageSourceAndRawJSON(t *testing.T, dataDir string, id int) (string, string) {
	t.Helper()

	db := openUsageTestDB(t, dataDir)
	defer db.Close()

	var source, rawJSON string
	if err := db.QueryRow(`SELECT source, raw_json FROM usage_records WHERE id = ?`, id).Scan(&source, &rawJSON); err != nil {
		t.Fatal(err)
	}
	return source, rawJSON
}

func openUsageTestDB(t *testing.T, dataDir string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "db", "cpa_helper.sqlite3")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	return db
}
