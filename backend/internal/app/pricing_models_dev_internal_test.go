package app

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

func TestModelsDevPricesUseMillionTokenUnitsAndContextThresholds(t *testing.T) {
	var catalog map[string]any
	if err := json.Unmarshal([]byte(`{
		"openai":{"models":{
			"gpt-test":{"cost":{"input":2.5,"output":15,"cache_read":0.25,
				"tiers":[{"input":5,"output":22.5,"cache_read":0.5,"tier":{"type":"context","size":272000}}],
				"context_over_200k":{"input":5,"output":22.5,"cache_read":0.5}}},
			"gpt-image-test":{"cost":{"input":5,"output":40}},
			"free-model":{"cost":{"input":0,"output":0,"cache_read":0,"cache_write":0}},
			"unknown-price":{},
			"invalid-price":{"cost":{"input":-1,"output":1}}
		}},
		"anthropic":{"models":{"claude-test":{"cost":{"input":3,"output":15,"cache_read":0.3,"cache_write":3.75}}}},
		"google":{"models":{"gemini-test":{"cost":{"input":1.25,"output":10,"context_over_200k":{"input":2.5,"output":15}}}}}
	}`), &catalog); err != nil {
		t.Fatal(err)
	}
	rows, total, invalid, duplicates := modelsDevPriceRows(catalog)
	if len(rows) != 5 || total != 7 || invalid != 2 || duplicates != 0 {
		t.Fatalf("catalog counts: %d/%d/%d/%d", len(rows), total, invalid, duplicates)
	}
	prices := map[string]modelPricePayload{}
	for _, row := range rows {
		prices[row.payload.Model] = row.payload
	}
	gpt := prices["gpt-test"]
	if gpt.InputUSDPerMillion != 2.5 || gpt.OutputUSDPerMillion != 15 || gpt.CacheReadUSDPerMillion != 0.25 ||
		!gpt.LongContextEnabled || gpt.LongContextThresholdTokens != 272000 || gpt.LongContextInputUSDPerMillion != 5 || gpt.LongContextOutputUSDPerMillion != 22.5 {
		t.Fatalf("wrong units or context threshold: %+v", gpt)
	}
	claude := prices["claude-test"]
	if claude.CacheReadUSDPerMillion != 0.3 || claude.CacheCreationUSDPerMillion != 3.75 {
		t.Fatalf("cache mapping: %+v", claude)
	}
	if prices["gemini-test"].LongContextThresholdTokens != 200000 {
		t.Fatal("legacy context_over_200k was not mapped")
	}
	image := prices["gpt-image-test"]
	if image.RequestUSD != nil || image.LongContextEnabled || image.FastEnabled {
		t.Fatalf("invented image/request or FAST prices: %+v", image)
	}
	if prices["free-model"].InputUSDPerMillion != 0 || prices["free-model"].CacheReadUSDPerMillion != 0 {
		t.Fatal("explicit free pricing changed")
	}
	if image.CacheReadUSDPerMillion != 5 || image.CacheCreationUSDPerMillion != 5 {
		t.Fatal("missing cache rates should use regular input pricing")
	}
}

func TestModelsDevDuplicateModelPricesPreferOriginalProvider(t *testing.T) {
	var catalog map[string]any
	if err := json.Unmarshal([]byte(`{
		"cheap-proxy":{"models":{"gpt-test":{"cost":{"input":0.1,"output":0.2}},"custom-model":{"canonical_model_id":"original/custom-model","cost":{"input":0,"output":0}}}},
		"openai":{"models":{"gpt-test":{"cost":{"input":2.5,"output":15}}}},
		"original":{"models":{"custom-model":{"canonical_model_id":"original/custom-model","cost":{"input":4,"output":8}}}}
	}`), &catalog); err != nil {
		t.Fatal(err)
	}
	first, total, invalid, duplicates := modelsDevPriceRows(catalog)
	if len(first) != 2 || total != 4 || invalid != 0 || duplicates != 2 || first[0].payload.Provider != "openai" || first[0].payload.InputUSDPerMillion != 2.5 || first[1].payload.Provider != "original" {
		t.Fatalf("original provider lost: %+v", first)
	}
	for range 20 {
		rows, _, _, _ := modelsDevPriceRows(catalog)
		if !reflect.DeepEqual(first, rows) {
			t.Fatal("map iteration changed which price is billed")
		}
	}
}

func TestModelsDevInvalidOrUnsupportedRatesAreNotFlattened(t *testing.T) {
	for _, cost := range []map[string]any{
		{"input": 1},
		{"input": math.NaN(), "output": 1},
		{"input": 1, "output": math.Inf(1)},
		{"input": 1, "output": 1, "cache_read": -1},
		{"input": 1, "output": 1, "tiers": []any{
			map[string]any{"input": 2, "output": 3, "tier": map[string]any{"type": "context", "size": 32000}},
			map[string]any{"input": 4, "output": 6, "tier": map[string]any{"type": "context", "size": 128000}},
		}},
		{"input": 1, "output": 1, "tiers": []any{
			map[string]any{"input": 2, "output": 3, "tier": map[string]any{"type": "context", "size": 0}},
		}},
	} {
		if _, ok := modelsDevEntryToPrice("test", "test", map[string]any{"cost": cost}); ok {
			t.Fatalf("unsupported price accepted: %#v", cost)
		}
	}
}

func TestModelsDevInvalidCatalogLeavesExistingPricesUntouched(t *testing.T) {
	a := newQuotaTestApp(t)
	if _, err := a.db.Exec(`INSERT INTO model_prices(provider, model, input_usd_per_million, source, updated_at) VALUES ('openai','existing',5,'litellm','2026-11-03T00:00:00+08:00')`); err != nil {
		t.Fatal(err)
	}
	for _, catalog := range []map[string]any{nil, {}, {"error": "unavailable"}, {"openai": map[string]any{"models": map[string]any{"existing": map[string]any{}}}}} {
		if _, err := a.syncModelsDevPrices(context.Background(), defaultModelsDevPricingURL, catalog); err == nil {
			t.Fatal("invalid/empty catalog accepted")
		}
	}
	prices, err := a.listPrices(context.Background())
	if err != nil || len(prices) != 1 || prices[0].InputUSDPerMillion != 5 || prices[0].Source != "litellm" {
		t.Fatalf("existing prices changed: %v %v", prices, err)
	}
}

func TestModelsDevResyncPreservesManualPricesAndLocalTiers(t *testing.T) {
	a := newQuotaTestApp(t)
	var catalog map[string]any
	if err := json.Unmarshal([]byte(`{"openai":{"models":{
		"gpt-manual":{"cost":{"input":1,"output":2}},
		"gpt-test":{"cost":{"input":2.5,"output":15,"tiers":[{"input":5,"output":22.5,"tier":{"type":"context","size":272000}}]}}
	}}}`), &catalog); err != nil {
		t.Fatal(err)
	}
	if _, err := a.createPrice(context.Background(), modelPricePayload{Provider: "custom", Model: "gpt-manual", InputUSDPerMillion: 9}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result, err := a.syncModelsDevPrices(context.Background(), defaultModelsDevPricingURL, catalog)
		if err != nil || result["imported"] != 1 || result["skipped_manual"] != 1 {
			t.Fatalf("sync failed: %v %v", result, err)
		}
	}
	prices, err := a.listPrices(context.Background())
	if err != nil || len(prices) != 2 || prices[0].Source != "manual" || prices[0].InputUSDPerMillion != 9 ||
		prices[1].Source != "models.dev" || !prices[1].LongContextEnabled || prices[1].LongContextThresholdTokens != 272000 {
		t.Fatalf("resync lost manual price or context tier: %v %v", prices, err)
	}
}

// Optional smoke check against a downloaded official catalog, never a network-dependent unit test.
func TestModelsDevDownloadedCatalog(t *testing.T) {
	path := os.Getenv("CPA_HELPER_MODELS_DEV_FIXTURE")
	if path == "" {
		t.Skip("no downloaded catalog configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	rows, total, invalid, duplicates := modelsDevPriceRows(catalog)
	if len(rows) == 0 {
		t.Fatal("official catalog has no usable prices")
	}
	t.Logf("models.dev: total=%d imported=%d skipped_invalid=%d skipped_duplicates=%d", total, len(rows), invalid, duplicates)
}
