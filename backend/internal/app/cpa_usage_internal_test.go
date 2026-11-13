package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCPACoreManagedCredentialsDoNotOverwriteLegacyQuotaState(t *testing.T) {
	a := newQuotaTestApp(t)
	index, priority, used, disabled := "fixture-index", 3, 25, true
	if err := a.upsertKeeperState(context.Background(), keeperAccountResult{
		Name: "fixture.json", Result: "healthy", CheckedAt: time.Now(), AuthIndex: &index,
		Priority: &priority, PrimaryUsedPercent: &used, Disabled: &disabled,
	}); err != nil {
		t.Fatal(err)
	}
	result := a.processKeeperAuth(context.Background(), AppConfig{}, map[string]any{
		"name": "fixture.json", "_helper_v8": true, "requires_sign_in": true,
	}, func(string) {}, true)
	if result.Result != "skipped" || result.LatestAction == nil {
		t.Fatal("Core-managed credentials entered legacy inspection")
	}
	var gotIndex string
	var gotPriority int
	var gotUsed int
	var gotDisabled bool
	if err := a.db.QueryRow("SELECT auth_index, priority, primary_used_percent, disabled FROM codex_keeper_auth_states WHERE auth_name=?", "fixture.json").Scan(&gotIndex, &gotPriority, &gotUsed, &gotDisabled); err != nil {
		t.Fatal(err)
	}
	if gotIndex != index || gotPriority != priority || gotUsed != used || gotDisabled != disabled {
		t.Fatal("skipping Core-managed recovery erased cached quota or credential state")
	}
}

func TestCPAUsageMetadataPersistsWithoutChangingHistoricalCharges(t *testing.T) {
	a := newQuotaTestApp(t)
	raw := []byte(`{"api_key":"synthetic-key","provider":"codex","model":"test","request_service_tier":"priority","effective_service_tier":"default","execution_id":"execution-fixture","session_id":"session-fixture","ttfb_ms":5,"tokens":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`)
	record, created, err := a.saveUsageMessage(context.Background(), raw)
	if err != nil || !created {
		t.Fatalf("save: created=%v err=%v", created, err)
	}
	loaded, err := a.getUsageRecord(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CPAMetadataJSON == "{}" || strings.Contains(loaded.CPAMetadataJSON, "synthetic-key") {
		t.Fatal("metadata missing or copied the API key")
	}
	tier, evidence := cpaServiceTier(loaded)
	if tier != "default" || evidence != "effective" {
		t.Fatalf("tier=%q evidence=%q", tier, evidence)
	}
	if cpaUsageDetails(loaded, usageRedactionOptions{})["execution_id"] != "execution-fixture" {
		t.Fatal("execution ID not exposed")
	}
	if _, err := a.db.Exec(`UPDATE usage_records SET cost_usd=7.25, unpriced=0 WHERE id=?`, record.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = a.getUsageRecord(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	amount, unpriced := recordCost(loaded, nil)
	if amount != 7.25 || unpriced {
		t.Fatal("stored historical cost was recomputed")
	}
	items, err := a.pagedUsageRecords(context.Background(), UsageFilters{}, 1, 10)
	if err != nil || len(items) != 1 || items[0].CPAMetadataJSON != loaded.CPAMetadataJSON {
		t.Fatalf("paged metadata: count=%d err=%v", len(items), err)
	}
}

func TestCPAUsageUnknownEvidenceDoesNotGuessFastOrTokenFees(t *testing.T) {
	model, requested := "audit-model", "priority"
	price := ModelPrice{Model: model, FastEnabled: true, FastMultiplier: 2, InputUSDPerMillion: 1}
	record := UsageRecord{Model: &model, RequestServiceTier: &requested, InputTokens: 1000000, TotalTokens: 1000000,
		CPAMetadataJSON: `{"effective_service_tier":"future-tier","response_service_tier":"priority"}`}
	if usageFastMultiplier(record, &price) != 1 {
		t.Fatal("unknown authoritative tier fell back to Fast")
	}
	for _, metadata := range []string{
		`{"accounting_version":3}`, `{"accounting_version":2}`, `{"token_breakdown":{"schema_version":2,"quality":"unclassified","total_tokens":1000000}}`,
	} {
		record.CPAMetadataJSON = metadata
		amount, unpriced := calculateRecordCost(record, map[[2]string]ModelPrice{priceKey("openai", model): price})
		if amount != 0 || !unpriced {
			t.Fatalf("unknown accounting was billed: %s => %v/%v", metadata, amount, unpriced)
		}
	}
}

func TestCPAUpstreamPoolRoundTripPreservesPartialKeyOverride(t *testing.T) {
	groups := testNativeGroups(t, `[{"name":"pool","connection-pool":{"enabled":false,"max-idle-conns-per-host":100},"keys":[{"api-key":"fixture","connection-pool":{"max-idle-conns-per-host":2}}]}]`)
	items, err := upstreamV8Items(groups, "codex-api-key")
	if err != nil {
		t.Fatal(err)
	}
	got, err := rebuildUpstreamsV8(groups, items, "codex-api-key")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(groups)
	after, _ := json.Marshal(got)
	if string(before) != string(after) {
		t.Fatal("unchanged flattened form wrote inherited values as explicit overrides")
	}
}

// Regression coverage for Core 0.2.0 accounting and grouped configuration.
func TestCPAUsageEffectiveServiceTierBilling(t *testing.T) {
	for _, tc := range []struct {
		name, requested, effective string
		want                       float64
	}{
		{"priority_downgraded", "priority", "default", 1},
		{"standard_upgraded", "default", "priority", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]any{
				"provider": "codex", "model": "audit-model", "api_key": "non-secret-audit-fixture",
				"request_service_tier": tc.requested, "response_service_tier": tc.effective,
				"effective_service_tier": tc.effective,
				"tokens":                 map[string]any{"input_tokens": 1000000, "output_tokens": 0, "total_tokens": 1000000},
			})
			normalized, err := normalizeUsage(payload)
			if err != nil {
				t.Fatal(err)
			}
			prices := map[[2]string]ModelPrice{priceKey("openai", "audit-model"): {
				Provider: "openai", Model: "audit-model", InputUSDPerMillion: 1, FastEnabled: true, FastMultiplier: 2,
			}}
			got, unpriced := calculateRecordCost(usageRecordFromNormalized(normalized), prices)
			if unpriced || got != tc.want {
				t.Errorf("charge=%v, want=%v using effective tier %q (requested=%q)", got, tc.want, tc.effective, tc.requested)
			}
		})
	}
}

func TestCPAUsageCanonicalReasoningBilling(t *testing.T) {
	for _, provider := range []string{"gemini", "claude"} {
		t.Run(provider, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]any{
				"provider": provider, "model": "audit-model", "api_key": "non-secret-audit-fixture", "accounting_version": 2,
				"tokens": map[string]any{"input_tokens": 100, "output_tokens": 50, "reasoning_tokens": 10, "total_tokens": 160},
				"token_breakdown": map[string]any{
					"schema_version": 2, "quality": "complete", "total_tokens": 160, "unclassified_tokens": 0,
					"input":  map[string]any{"total_tokens": 100, "uncached_tokens": 100, "cache_read_tokens": 0, "cache_write_tokens": 0},
					"output": map[string]any{"total_tokens": 60, "non_reasoning_tokens": 50, "reasoning_tokens": 10},
				},
			})
			normalized, err := normalizeUsage(payload)
			if err != nil {
				t.Fatal(err)
			}
			prices := map[[2]string]ModelPrice{priceKey(provider, "audit-model"): {Provider: provider, Model: "audit-model", OutputUSDPerMillion: 1000000}}
			got, unpriced := calculateRecordCost(usageRecordFromNormalized(normalized), prices)
			if unpriced || got != 60 {
				t.Errorf("output charge=%v, want=60 including canonical reasoning output", got)
			}
		})
	}
}

func TestCPAUpstreamConnectionPoolInheritance(t *testing.T) {
	group := map[string]any{"connection-pool": map[string]any{"enabled": false, "max-idle-conns-per-host": 100}}
	key := map[string]any{"connection-pool": map[string]any{"enabled": nil, "max-idle-conns-per-host": 2}}
	effective := upstreamV8Effective(group, key)
	pool := effective["connection-pool"].(map[string]any)
	if pool["enabled"] != false {
		t.Errorf("effective connection-pool lost group enabled=false: %v", pool)
	}
	if !upstreamSharedField("connection-pool") {
		t.Errorf("connection-pool missing from V8 shared-field list")
	}
}
