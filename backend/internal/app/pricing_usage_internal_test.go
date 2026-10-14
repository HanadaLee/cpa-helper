package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRecordCostUsesClaudeCacheReadAndCreationTokens(t *testing.T) {
	provider := "claude"
	model := "claude-sonnet-test"
	record := UsageRecord{
		Provider:            &provider,
		Model:               &model,
		InputTokens:         100,
		OutputTokens:        10,
		CachedTokens:        999,
		CacheReadTokens:     20,
		CacheCreationTokens: 30,
		TotalTokens:         110,
	}
	prices := map[[2]string]ModelPrice{
		priceKey("anthropic", model): {
			Provider:                   "anthropic",
			Model:                      model,
			InputUSDPerMillion:         10,
			OutputUSDPerMillion:        20,
			CacheReadUSDPerMillion:     1,
			CacheCreationUSDPerMillion: 12,
		},
	}

	amount, unpriced := recordCost(record, prices)
	if unpriced {
		t.Fatal("record should be priced")
	}
	want := mathRound((100*10+20*1+30*12+10*20)/1_000_000.0, 8)
	if amount != want {
		t.Fatalf("cost = %v, want %v", amount, want)
	}
}

func TestRecordCostTruncatesGenericCachedTokens(t *testing.T) {
	provider := "openai"
	model := "gpt-test"
	record := UsageRecord{
		Provider:     &provider,
		Model:        &model,
		InputTokens:  100,
		OutputTokens: 10,
		CachedTokens: 150,
		TotalTokens:  110,
	}
	prices := map[[2]string]ModelPrice{
		priceKey(provider, model): {
			Provider:               provider,
			Model:                  model,
			InputUSDPerMillion:     10,
			OutputUSDPerMillion:    20,
			CacheReadUSDPerMillion: 1,
		},
	}

	amount, unpriced := recordCost(record, prices)
	if unpriced {
		t.Fatal("record should be priced")
	}
	want := mathRound((100*1+10*20)/1_000_000.0, 8)
	if amount != want {
		t.Fatalf("cost = %v, want %v", amount, want)
	}
}

func TestRecordCostMatchesModelAcrossDifferentProviders(t *testing.T) {
	requestProvider := "openai-compatible-opencode"
	model := "deepseek-flash"
	prices := pricesByKey([]ModelPrice{{
		ID:                     1,
		Provider:               "deepseek",
		Model:                  model,
		InputUSDPerMillion:     0.3,
		OutputUSDPerMillion:    1.2,
		CacheReadUSDPerMillion: 0.006,
	}})

	amount, unpriced := recordCost(UsageRecord{
		Provider:     &requestProvider,
		Model:        &model,
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
		TotalTokens:  2_000_000,
	}, prices)
	if unpriced || amount != 1.5 {
		t.Fatalf("cross-provider model cost = %v unpriced=%v, want 1.5/false", amount, unpriced)
	}
}

func TestPricesByKeyPrefersManualAndMostRecentlyUpdatedModelPrice(t *testing.T) {
	model := "shared-model"
	older := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	prices := pricesByKey([]ModelPrice{
		{ID: 1, Provider: "provider-a", Model: model, InputUSDPerMillion: 1, UpdatedAt: older},
		{ID: 2, Provider: "provider-b", Model: model, InputUSDPerMillion: 2, UpdatedAt: newer},
		{ID: 3, Provider: "provider-c", Model: model, InputUSDPerMillion: 9, AutoSynced: true, UpdatedAt: newer.Add(time.Hour)},
	})

	matched := findMatchingPrice(prices, &model)
	if matched == nil || matched.ID != 2 || matched.InputUSDPerMillion != 2 {
		t.Fatalf("matched price = %#v, want newest manual price", matched)
	}
}

func TestRecordCostUsesRequestPriceForImageModels(t *testing.T) {
	provider := "openai"
	model := "gpt-image-2"
	requestUSD := 1.25
	prices := map[[2]string]ModelPrice{
		priceKey(provider, model): {
			Provider:           provider,
			Model:              model,
			InputUSDPerMillion: 5,
			RequestUSD:         &requestUSD,
		},
	}

	amount, unpriced := recordCost(UsageRecord{
		Provider:     &provider,
		Model:        &model,
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
		TotalTokens:  2_000_000,
	}, prices)
	if unpriced || amount != 1.25 {
		t.Fatalf("image cost = %v unpriced=%v, want 1.25 false", amount, unpriced)
	}

	amount, unpriced = recordCost(UsageRecord{
		Provider: &provider,
		Model:    &model,
		Failed:   true,
	}, prices)
	if unpriced || amount != 0 {
		t.Fatalf("failed image cost = %v unpriced=%v, want 0 false", amount, unpriced)
	}
}

func TestRecordCostAppliesFastMultiplierToPriorityAndFastRequests(t *testing.T) {
	provider := "openai"
	model := "gpt-fast-test"
	prices := map[[2]string]ModelPrice{
		priceKey(provider, model): {
			Provider:           provider,
			Model:              model,
			InputUSDPerMillion: 2,
			FastEnabled:        true,
			FastMultiplier:     2.5,
		},
	}
	base := UsageRecord{
		Provider:    &provider,
		Model:       &model,
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}

	for _, test := range []struct {
		serviceTier string
		wantCost    float64
	}{
		{serviceTier: "priority", wantCost: 5},
		{serviceTier: "fast", wantCost: 5},
		{serviceTier: " FAST ", wantCost: 5},
		{serviceTier: "standard", wantCost: 2},
	} {
		base.RequestServiceTier = &test.serviceTier
		amount, unpriced := recordCost(base, prices)
		if unpriced || amount != test.wantCost {
			t.Fatalf("%q cost = %v unpriced=%v, want %v false", test.serviceTier, amount, unpriced, test.wantCost)
		}
	}
}

func TestRecordCostDoesNotApplyFastMultiplierWhenDisabled(t *testing.T) {
	provider, model, serviceTier := "openai", "gpt-no-fast", "fast"
	price := ModelPrice{
		Provider: provider, Model: model, InputUSDPerMillion: 2,
		FastEnabled: false, FastMultiplier: 2.5,
	}
	record := UsageRecord{
		Provider: &provider, Model: &model, RequestServiceTier: &serviceTier,
		InputTokens: 1_000_000, TotalTokens: 1_000_000,
	}
	amount, unpriced := recordCost(record, pricesByKey([]ModelPrice{price}))
	if unpriced || amount != 2 {
		t.Fatalf("disabled FAST cost = %v unpriced=%v, want 2 false", amount, unpriced)
	}
}

func TestRecordCostUsesLongContextRatesAboveInputThreshold(t *testing.T) {
	provider := "openai"
	model := "gpt-long-context-test"
	prices := map[[2]string]ModelPrice{
		priceKey(provider, model): {
			Provider:                              provider,
			Model:                                 model,
			InputUSDPerMillion:                    2,
			OutputUSDPerMillion:                   4,
			CacheReadUSDPerMillion:                1,
			FastEnabled:                           true,
			FastMultiplier:                        2,
			LongContextEnabled:                    true,
			LongContextThresholdTokens:            100,
			LongContextInputUSDPerMillion:         6,
			LongContextOutputUSDPerMillion:        12,
			LongContextCacheReadUSDPerMillion:     3,
			LongContextCacheCreationUSDPerMillion: 8,
		},
	}

	base := UsageRecord{
		Provider:     &provider,
		Model:        &model,
		InputTokens:  100,
		CachedTokens: 20,
		OutputTokens: 10,
		TotalTokens:  110,
	}
	amount, unpriced := recordCost(base, prices)
	wantBase := mathRound((80*2+20*1+10*4)/1_000_000.0, 8)
	if unpriced || amount != wantBase {
		t.Fatalf("at-threshold cost = %v unpriced=%v, want base cost %v false", amount, unpriced, wantBase)
	}

	base.InputTokens = 101
	base.TotalTokens = 111
	amount, unpriced = recordCost(base, prices)
	wantLong := mathRound((81*6+20*3+10*12)/1_000_000.0, 8)
	if unpriced || amount != wantLong {
		t.Fatalf("above-threshold cost = %v unpriced=%v, want long-context cost %v false", amount, unpriced, wantLong)
	}

	serviceTier := "fast"
	base.RequestServiceTier = &serviceTier
	amount, unpriced = recordCost(base, prices)
	if unpriced || amount != mathRound(wantLong*2, 8) {
		t.Fatalf("fast long-context cost = %v unpriced=%v, want %v false", amount, unpriced, mathRound(wantLong*2, 8))
	}
}

func TestRecordCostSkipsFastMultiplierWhenLongContextDoesNotSupportFast(t *testing.T) {
	provider := "openai"
	model := "gpt-long-context-no-fast"
	serviceTier := "fast"
	prices := map[[2]string]ModelPrice{
		priceKey(provider, model): {
			Provider:                      provider,
			Model:                         model,
			InputUSDPerMillion:            2,
			FastEnabled:                   true,
			FastMultiplier:                3,
			LongContextEnabled:            true,
			LongContextThresholdTokens:    100,
			LongContextInputUSDPerMillion: 6,
			LongContextFastUnsupported:    true,
		},
	}
	record := UsageRecord{
		Provider:           &provider,
		Model:              &model,
		RequestServiceTier: &serviceTier,
		InputTokens:        101,
		TotalTokens:        101,
	}

	amount, unpriced := recordCost(record, prices)
	wantLong := mathRound(101*6/1_000_000.0, 8)
	if unpriced || amount != wantLong {
		t.Fatalf("unsupported long-context FAST cost = %v unpriced=%v, want %v false", amount, unpriced, wantLong)
	}

	record.InputTokens = 100
	record.TotalTokens = 100
	amount, unpriced = recordCost(record, prices)
	wantShort := mathRound(100*2*3/1_000_000.0, 8)
	if unpriced || amount != wantShort {
		t.Fatalf("short-context FAST cost = %v unpriced=%v, want %v false", amount, unpriced, wantShort)
	}
}

func TestUsagePromptTokensIncludesSeparatelyReportedCachedInput(t *testing.T) {
	provider := "openai"
	record := UsageRecord{
		Provider:        &provider,
		InputTokens:     2_555,
		CachedTokens:    198_400,
		CacheReadTokens: 198_400,
	}
	if got := usagePromptTokens(record); got != 200_955 {
		t.Fatalf("usagePromptTokens = %d, want 200955", got)
	}
}

func TestValidatePricePayloadRequiresPositiveLongContextThreshold(t *testing.T) {
	payload := modelPricePayload{
		Provider:           "openai",
		Model:              "gpt-long-context-test",
		LongContextEnabled: true,
	}
	if _, err := validatePricePayload(payload); err == nil {
		t.Fatal("enabled long-context pricing without a threshold should fail")
	}
	payload.LongContextThresholdTokens = 200_000
	if _, err := validatePricePayload(payload); err != nil {
		t.Fatalf("valid long-context pricing failed: %v", err)
	}
}

func TestRecordCostTreatsImageWithoutRequestPriceAsUnpriced(t *testing.T) {
	provider := "openai"
	model := "custom-image-model"
	prices := map[[2]string]ModelPrice{
		priceKey(provider, model): {
			Provider:               provider,
			Model:                  model,
			InputUSDPerMillion:     100,
			OutputUSDPerMillion:    100,
			CacheReadUSDPerMillion: 100,
		},
	}

	amount, unpriced := recordCost(UsageRecord{
		Provider: &provider,
		Model:    &model,
	}, prices)
	if amount != 0 || !unpriced {
		t.Fatalf("image without request price cost = %v unpriced=%v, want 0 true", amount, unpriced)
	}
}

func TestModelPriceAPIUpdatesImageRequestPrice(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSONForPricingTest(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "管理员",
	}, nil, nil)

	var created ModelPrice
	requestJSONForPricingTest(t, handler, http.MethodPost, "/api/model-prices", map[string]any{
		"provider":                       "openai",
		"model":                          "gpt-image-2",
		"input_usd_per_million":          0,
		"output_usd_per_million":         0,
		"cache_read_usd_per_million":     0,
		"cache_creation_usd_per_million": 0,
		"request_usd":                    1,
	}, cookies, &created)
	if created.RequestUSD == nil || *created.RequestUSD != 1 || created.BillingUnit != modelBillingUnitRequest {
		t.Fatalf("created image price = %#v, want request_usd=1 request billing", created)
	}
	if created.FastMultiplier != 1 {
		t.Fatalf("created fast multiplier = %v, want default 1", created.FastMultiplier)
	}

	var updated ModelPrice
	requestJSONForPricingTest(t, handler, http.MethodPut, fmt.Sprintf("/api/model-prices/%d", created.ID), map[string]any{
		"provider":                       "openai",
		"model":                          "gpt-image-2",
		"input_usd_per_million":          0,
		"output_usd_per_million":         0,
		"cache_read_usd_per_million":     0,
		"cache_creation_usd_per_million": 0,
		"request_usd":                    2.5,
	}, cookies, &updated)
	if updated.RequestUSD == nil || *updated.RequestUSD != 2.5 || updated.BillingUnit != modelBillingUnitRequest {
		t.Fatalf("updated image price = %#v, want request_usd=2.5 request billing", updated)
	}
}

func TestModelPriceAPIRoundTripsLongContextRates(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSONForPricingTest(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "管理员",
	}, nil, nil)

	var created ModelPrice
	requestJSONForPricingTest(t, handler, http.MethodPost, "/api/model-prices", map[string]any{
		"provider":                                    "openai",
		"model":                                       "gpt-long-context-api",
		"input_usd_per_million":                       1,
		"output_usd_per_million":                      2,
		"cache_read_usd_per_million":                  0.1,
		"cache_creation_usd_per_million":              0,
		"request_usd":                                 nil,
		"fast_multiplier":                             1.5,
		"fast_enabled":                                true,
		"long_context_enabled":                        true,
		"long_context_threshold_tokens":               200000,
		"long_context_input_usd_per_million":          3,
		"long_context_output_usd_per_million":         6,
		"long_context_cache_read_usd_per_million":     0.3,
		"long_context_cache_creation_usd_per_million": 0.5,
		"long_context_fast_unsupported":               true,
	}, cookies, &created)
	if !created.FastEnabled || created.FastMultiplier != 1.5 ||
		!created.LongContextEnabled || created.LongContextThresholdTokens != 200_000 ||
		created.LongContextInputUSDPerMillion != 3 || created.LongContextOutputUSDPerMillion != 6 ||
		created.LongContextCacheReadUSDPerMillion != 0.3 || created.LongContextCacheCreationUSDPerMillion != 0.5 ||
		!created.LongContextFastUnsupported {
		t.Fatalf("created long-context price = %#v", created)
	}
}

func TestUpdateAutoSyncedPriceLocalOverridesKeepLiteLLMSync(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	now := dbTime(time.Now().In(appTimeLocation))
	result, err := app.db.InsertContext(context.Background(), `
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million,
			fast_multiplier, source, source_model, auto_synced, last_synced_at, updated_at
		) VALUES ('openai', 'gpt-fast-sync', 1, 2, 0.1, 0.2, 1, 'litellm', 'gpt-fast-sync', 1, ?, ?)
	`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	fastMultiplier := 3.0
	updated, err := app.updatePrice(context.Background(), int(id), modelPricePayload{
		Provider:                              "openai",
		Model:                                 "gpt-fast-sync",
		InputUSDPerMillion:                    1,
		OutputUSDPerMillion:                   2,
		CacheReadUSDPerMillion:                0.1,
		CacheCreationUSDPerMillion:            0.2,
		FastMultiplier:                        &fastMultiplier,
		FastEnabled:                           true,
		LongContextEnabled:                    true,
		LongContextThresholdTokens:            200_000,
		LongContextInputUSDPerMillion:         2,
		LongContextOutputUSDPerMillion:        4,
		LongContextCacheReadUSDPerMillion:     0.2,
		LongContextCacheCreationUSDPerMillion: 0.4,
		LongContextFastUnsupported:            true,
		OffPeakEnabled:                        true,
		PeakInputUSDPerMillion:                0.5,
		LongContextPeakInputUSDPerMillion:     1.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.AutoSynced || updated.Source != "litellm" || updated.SourceModel == nil || *updated.SourceModel != "gpt-fast-sync" {
		t.Fatalf("updated source state = %#v, want LiteLLM auto sync preserved", updated)
	}
	if updated.FastMultiplier != 3 {
		t.Fatalf("updated fast multiplier = %v, want 3", updated.FastMultiplier)
	}
	if !updated.LongContextEnabled || updated.LongContextThresholdTokens != 200_000 || updated.LongContextInputUSDPerMillion != 2 || !updated.LongContextFastUnsupported {
		t.Fatalf("updated long-context overrides = %#v, want enabled threshold 200000 input 2", updated)
	}
	if !updated.OffPeakEnabled || updated.PeakInputUSDPerMillion != 0.5 || updated.LongContextPeakInputUSDPerMillion != 1.5 {
		t.Fatalf("updated off-peak overrides = %#v", updated)
	}
	_, err = app.syncLiteLLMPrices(context.Background(), "https://example.com/prices.json", map[string]any{
		"gpt-fast-sync": map[string]any{"litellm_provider": "openai", "input_cost_per_token": 0.000004},
	})
	if err != nil {
		t.Fatal(err)
	}
	var synced ModelPrice
	prices, err := app.listPrices(context.Background())
	if err != nil || len(prices) != 1 {
		t.Fatalf("prices after LiteLLM sync: %v, %v", prices, err)
	}
	synced = prices[0]
	if synced.InputUSDPerMillion != 4 || !synced.OffPeakEnabled || synced.PeakInputUSDPerMillion != 0.5 || synced.LongContextPeakInputUSDPerMillion != 1.5 {
		t.Fatalf("LiteLLM sync lost off-peak overrides: %#v", synced)
	}
}

func TestUsageAggregatesClaudeCacheReadAndCreationTokens(t *testing.T) {
	provider := "claude"
	model := "claude-sonnet-test"
	record := UsageRecord{
		Timestamp:           time.Date(2026, 5, 19, 10, 0, 0, 0, appTimeLocation),
		Provider:            &provider,
		Model:               &model,
		InputTokens:         10,
		OutputTokens:        5,
		CachedTokens:        20,
		CacheReadTokens:     20,
		CacheCreationTokens: 30,
		ReasoningTokens:     7,
		TotalTokens:         15,
	}
	prices := map[[2]string]ModelPrice{
		priceKey("anthropic", model): {
			Provider:                   "anthropic",
			Model:                      model,
			InputUSDPerMillion:         1,
			OutputUSDPerMillion:        2,
			CacheReadUSDPerMillion:     0.5,
			CacheCreationUSDPerMillion: 1.25,
		},
	}
	filters := UsageFilters{}
	start := time.Date(2026, 5, 19, 0, 0, 0, 0, appTimeLocation)
	end := start.Add(24 * time.Hour)
	filters.Start = &start
	filters.End = &end

	summary := usageSummaryFromRecords(filters, []UsageRecord{record}, prices)
	if summary["input_tokens"].(int) != 60 {
		t.Fatalf("summary input = %v, want 60", summary["input_tokens"])
	}
	if summary["total_tokens"].(int) != 72 {
		t.Fatalf("summary total = %v, want 72", summary["total_tokens"])
	}
	trends := trendPointsFromRecords(filters, []UsageRecord{record}, prices)
	if len(trends) != 1 || trends[0]["total_tokens"].(int) != 72 {
		t.Fatalf("trend totals = %#v, want one item with total 72", trends)
	}
	ranking := rankingFromRecords([]UsageRecord{record}, prices, "model", nil)
	items := ranking["items"].([]map[string]any)
	if len(items) != 1 || items[0]["total_tokens"].(int) != 72 {
		t.Fatalf("ranking totals = %#v, want one item with total 72", items)
	}
	distributions := distributionsFromRecords([]UsageRecord{record}, prices)
	models := distributions["models"].([]map[string]any)
	if len(models) != 1 || models[0]["total_tokens"].(int) != 72 {
		t.Fatalf("distribution totals = %#v, want one item with total 72", models)
	}
}

func TestSyncLiteLLMPricesReplacesLiteLLMSource(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	now := dbTime(time.Now().In(appTimeLocation))
	if _, err := app.db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million, source, updated_at
		) VALUES
			('openai', 'old-litellm-model', 1, 1, 1, 1, 'litellm', ?),
			('openai', 'manual-model', 9, 9, 9, 9, 'manual', ?)
	`, now, now); err != nil {
		t.Fatalf("seed prices: %v", err)
	}
	if _, err := app.db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million, fast_enabled, fast_multiplier,
			long_context_enabled, long_context_threshold_tokens,
			long_context_input_usd_per_million, long_context_output_usd_per_million,
			long_context_cache_read_usd_per_million, long_context_cache_creation_usd_per_million,
			long_context_fast_unsupported,
			source, source_model, auto_synced, last_synced_at, updated_at
		) VALUES ('openai', 'gpt-new-model', 9, 9, 9, 9, 1, 2.5, 1, 200000, 3, 6, 0.3, 0.6, 1,
		          'litellm', 'gpt-new-model', 1, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("seed customized LiteLLM price: %v", err)
	}

	rawData := map[string]any{
		"gpt-new-model": map[string]any{
			"litellm_provider":            "openai",
			"input_cost_per_token":        0.000001,
			"output_cost_per_token":       0.000002,
			"cache_read_input_token_cost": 0.0000001,
		},
		"claude-new-model": map[string]any{
			"litellm_provider":                "anthropic",
			"input_cost_per_token":            0.000003,
			"output_cost_per_token":           0.000015,
			"cache_read_input_token_cost":     0.0000003,
			"cache_creation_input_token_cost": 0.00000375,
		},
		"manual-model": map[string]any{
			"litellm_provider":     "openai",
			"input_cost_per_token": 0.000001,
		},
	}
	result, err := app.syncLiteLLMPrices(context.Background(), "https://example.com/prices.json", rawData)
	if err != nil {
		t.Fatalf("syncLiteLLMPrices failed: %v", err)
	}
	if result["imported"].(int) != 2 || result["skipped_manual"].(int) != 1 {
		t.Fatalf("sync result = %#v, want imported 2 skipped_manual 1", result)
	}

	var oldCount int
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM model_prices WHERE source = 'litellm' AND model = 'old-litellm-model'`).Scan(&oldCount); err != nil {
		t.Fatalf("query old litellm count: %v", err)
	}
	if oldCount != 0 {
		t.Fatalf("old litellm rows = %d, want 0", oldCount)
	}
	var manualInput float64
	if err := app.db.QueryRow(`SELECT input_usd_per_million FROM model_prices WHERE source = 'manual' AND model = 'manual-model'`).Scan(&manualInput); err != nil {
		t.Fatalf("query manual price: %v", err)
	}
	if manualInput != 9 {
		t.Fatalf("manual price = %v, want preserved 9", manualInput)
	}
	var cacheRead, cacheCreation float64
	if err := app.db.QueryRow(`SELECT cache_read_usd_per_million, cache_creation_usd_per_million FROM model_prices WHERE source = 'litellm' AND model = 'claude-new-model'`).Scan(&cacheRead, &cacheCreation); err != nil {
		t.Fatalf("query claude price: %v", err)
	}
	if cacheRead != 0.3 || cacheCreation != 3.75 {
		t.Fatalf("claude cache prices = read %v creation %v, want 0.3 and 3.75", cacheRead, cacheCreation)
	}
	var syncedInput, fastMultiplier, longInput float64
	var fastEnabled, longEnabled, longFastUnsupported bool
	var longThreshold int
	if err := app.db.QueryRow(`
		SELECT input_usd_per_million, fast_enabled, fast_multiplier, long_context_enabled,
		       long_context_threshold_tokens, long_context_input_usd_per_million,
		       long_context_fast_unsupported
		FROM model_prices WHERE source = 'litellm' AND model = 'gpt-new-model'
	`).Scan(&syncedInput, &fastEnabled, &fastMultiplier, &longEnabled, &longThreshold, &longInput, &longFastUnsupported); err != nil {
		t.Fatalf("query customized LiteLLM price: %v", err)
	}
	if syncedInput != 1 || !fastEnabled || fastMultiplier != 2.5 || !longEnabled || longThreshold != 200_000 || longInput != 3 || !longFastUnsupported {
		t.Fatalf("synced base/local overrides = %v/%v/%v/%v/%v/%v/%v, want 1/true/2.5/true/200000/3/true", syncedInput, fastEnabled, fastMultiplier, longEnabled, longThreshold, longInput, longFastUnsupported)
	}
}

func TestListPricesOrdersManualBeforeSynced(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	now := dbTime(time.Now().In(appTimeLocation))
	if _, err := app.db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million, source,
			auto_synced, updated_at
		) VALUES
			('aaa-synced', 'aaa-model', 1, 1, 0, 0, 'litellm', 1, ?),
			('zzz-manual', 'zzz-model', 1, 1, 0, 0, 'manual', 0, ?)
	`, now, now); err != nil {
		t.Fatalf("seed prices: %v", err)
	}

	prices, err := app.listPrices(context.Background())
	if err != nil {
		t.Fatalf("listPrices failed: %v", err)
	}
	if len(prices) != 2 {
		t.Fatalf("prices length = %d, want 2", len(prices))
	}
	if prices[0].AutoSynced || prices[0].Source != "manual" || prices[0].Model != "zzz-model" {
		t.Fatalf("first price = %#v, want manual price first", prices[0])
	}
	if !prices[1].AutoSynced || prices[1].Source != "litellm" || prices[1].Model != "aaa-model" {
		t.Fatalf("second price = %#v, want synced price second", prices[1])
	}
}

func TestModelPriceCatalogListsCPAModelsWithMatchedPrices(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	var seenAuth string
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-priced", "name": "GPT Priced", "owner": "openai", "object": "model"},
				{"id": "missing/model", "object": "model"},
			},
		})
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	cfg, err := app.loadConfig(context.Background())
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	cfg.Collector.CLIProxyURL = cpa.URL
	if err := app.saveConfig(context.Background(), cfg); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	handler := app.Routes()
	cookies := requestJSONForPricingTest(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "管理员",
	}, nil, nil)

	now := dbTime(time.Now().In(appTimeLocation))
	apiKey := "sk-catalog-test"
	if _, err := app.db.Exec(`
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
		VALUES (?, 1, ?, 'Admin Key', ?, ?)
	`, hashAPIKey(apiKey), apiKey, now, now); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	pausedUser, err := app.db.Exec(`
		INSERT INTO users (username, is_admin, nickname, quota_paused_at, quota_pause_reason, created_at, updated_at)
		VALUES ('paused-user', 0, 'Paused user', ?, ?, ?, ?)
	`, now, quotaPauseReasonExhausted, now, now)
	if err != nil {
		t.Fatalf("seed paused user: %v", err)
	}
	pausedUserID, err := pausedUser.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
		VALUES (?, ?, ?, 'Paused Key', ?, ?)
	`, hashAPIKey("sk-paused-catalog"), pausedUserID, "sk-paused-catalog", now, now); err != nil {
		t.Fatalf("seed paused api key: %v", err)
	}
	if _, err := app.db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million, source,
			source_model, auto_synced, last_synced_at, updated_at
		) VALUES ('openai', 'gpt-priced', 1, 2, 0.1, 0, 'litellm', 'gpt-priced', 1, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("seed model price: %v", err)
	}

	var catalog ModelPriceCatalogResponse
	requestJSONForPricingTest(t, handler, http.MethodGet, "/api/model-prices/catalog", nil, cookies, &catalog)
	if seenAuth != "Bearer "+apiKey {
		t.Fatalf("Authorization = %q, want bearer api key", seenAuth)
	}
	if catalog.APIKeyCount != 2 || catalog.QueryableAPIKeyCount != 1 || len(catalog.Errors) != 0 {
		t.Fatalf("key counts/errors = %d/%d/%d, want 2/1/0", catalog.APIKeyCount, catalog.QueryableAPIKeyCount, len(catalog.Errors))
	}
	if catalog.PricedModels != 1 || catalog.UnpricedModels != 1 {
		t.Fatalf("priced/unpriced = %d/%d, want 1/1", catalog.PricedModels, catalog.UnpricedModels)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("models length = %d, want 2", len(catalog.Models))
	}
	if catalog.Models[0].ID != "missing/model" || catalog.Models[0].Price != nil || catalog.Models[0].SuggestedProvider != "missing" {
		t.Fatalf("first model = %#v, want missing/model unpriced with suggested provider", catalog.Models[0])
	}
	if catalog.Models[1].ID != "gpt-priced" || catalog.Models[1].Price == nil || catalog.Models[1].Price.Source != "litellm" {
		t.Fatalf("second model = %#v, want gpt-priced with litellm price", catalog.Models[1])
	}
	if len(catalog.Models[1].Sources) != 1 || catalog.Models[1].Sources[0].Description != "Admin Key" || catalog.Models[1].Sources[0].UserLabel != "管理员" {
		t.Fatalf("sources = %#v, want key description and user label", catalog.Models[1].Sources)
	}
	pausedModels, err := app.availableModelsForUser(context.Background(), int(pausedUserID))
	if err != nil {
		t.Fatal(err)
	}
	if !pausedModels.HasAPIKeys || pausedModels.APIKeyCount != 1 || pausedModels.QueryableAPIKeyCount != 0 || !pausedModels.QuotaPaused || len(pausedModels.Errors) != 0 {
		t.Fatalf("paused user's model catalog = %#v", pausedModels)
	}
	if _, err := app.db.Exec(`UPDATE users SET quota_paused_at = ?, quota_pause_reason = ? WHERE id = 1`, now, quotaPauseReasonExhausted); err != nil {
		t.Fatal(err)
	}
	requestJSONForPricingTest(t, handler, http.MethodGet, "/api/model-prices/catalog", nil, cookies, &catalog)
	if !catalog.HasAPIKeys || catalog.APIKeyCount != 2 || catalog.QueryableAPIKeyCount != 0 || len(catalog.Errors) != 0 || len(catalog.Models) != 0 {
		t.Fatalf("all quota-paused model catalog = %#v", catalog)
	}
}

func TestModelPriceCatalogTreatsImageWithoutRequestPriceAsUnpriced(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-image-2", "name": "GPT Image", "owner": "openai", "object": "model"},
			},
		})
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	cfg, err := app.loadConfig(context.Background())
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	cfg.Collector.CLIProxyURL = cpa.URL
	if err := app.saveConfig(context.Background(), cfg); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	handler := app.Routes()
	cookies := requestJSONForPricingTest(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "管理员",
	}, nil, nil)

	now := dbTime(time.Now().In(appTimeLocation))
	apiKey := "sk-catalog-image-test"
	if _, err := app.db.Exec(`
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
		VALUES (?, 1, ?, 'Admin Key', ?, ?)
	`, hashAPIKey(apiKey), apiKey, now, now); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	if _, err := app.db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million, source,
			source_model, auto_synced, last_synced_at, updated_at
		) VALUES ('openai', 'gpt-image-2', 5, 10, 1.25, 0, 'litellm', 'gpt-image-2', 1, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("seed model price: %v", err)
	}

	var catalog ModelPriceCatalogResponse
	requestJSONForPricingTest(t, handler, http.MethodGet, "/api/model-prices/catalog", nil, cookies, &catalog)
	if catalog.PricedModels != 0 || catalog.UnpricedModels != 1 {
		t.Fatalf("priced/unpriced = %d/%d, want 0/1", catalog.PricedModels, catalog.UnpricedModels)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].Price == nil {
		t.Fatalf("catalog models = %#v, want matched unpriced image price", catalog.Models)
	}
	if catalog.Models[0].Price.BillingUnit != modelBillingUnitRequest || catalog.Models[0].Price.RequestUSD != nil {
		t.Fatalf("image price = %#v, want request billing with nil request_usd", catalog.Models[0].Price)
	}
}

func TestLiteLLMSyncUsesConfiguredHTTPProxy(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls++
		http.Error(w, "direct request should not be used", http.StatusBadGateway)
	}))
	defer target.Close()

	proxyCalls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls++
		if r.URL.String() != target.URL+"/prices.json" {
			t.Errorf("proxied request URL = %q, want %q", r.URL.String(), target.URL+"/prices.json")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"proxy-model": map[string]any{
				"litellm_provider":     "openai",
				"input_cost_per_token": 0.000001,
			},
		})
	}))
	defer proxy.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSONForPricingTest(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	var settings struct {
		Enabled  bool   `json:"enabled"`
		ProxyURL string `json:"proxy_url"`
	}
	requestJSONForPricingTest(t, handler, http.MethodGet, "/api/model-prices/litellm-proxy", nil, cookies, &settings)
	if settings.Enabled || settings.ProxyURL != "" {
		t.Fatalf("default proxy settings = %#v, want disabled empty proxy", settings)
	}
	requestJSONForPricingTest(t, handler, http.MethodPut, "/api/model-prices/litellm-proxy", map[string]any{
		"enabled":   true,
		"proxy_url": proxy.URL,
	}, cookies, &settings)
	if !settings.Enabled || settings.ProxyURL != proxy.URL {
		t.Fatalf("saved proxy settings = %#v, want enabled %q", settings, proxy.URL)
	}

	var syncResult struct {
		Imported int `json:"imported"`
	}
	requestJSONForPricingTest(t, handler, http.MethodPost, "/api/model-prices/sync/litellm", map[string]any{
		"source_url": target.URL + "/prices.json",
	}, cookies, &syncResult)
	if syncResult.Imported != 1 {
		t.Fatalf("imported = %d, want 1", syncResult.Imported)
	}
	if proxyCalls != 1 || targetCalls != 0 {
		t.Fatalf("proxy/direct calls = %d/%d, want 1/0", proxyCalls, targetCalls)
	}
}

func TestNormalizeLiteLLMProxyURLAcceptsSock5Alias(t *testing.T) {
	normalized, err := normalizeLiteLLMProxyURL("sock5://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("normalizeLiteLLMProxyURL failed: %v", err)
	}
	if normalized != "socks5://127.0.0.1:1080" {
		t.Fatalf("normalized proxy URL = %q, want socks5://127.0.0.1:1080", normalized)
	}
}

func requestJSONForPricingTest(
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
