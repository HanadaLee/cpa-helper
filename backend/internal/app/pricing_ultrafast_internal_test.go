package app

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestUltraFastBillingUsesIndependentActualTier(t *testing.T) {
	provider, model, requested := "openai", "gpt-ultrafast-test", "ultrafast"
	base := ModelPrice{Provider: provider, Model: model, InputUSDPerMillion: 2,
		FastEnabled: true, FastMultiplier: 2, UltraFastEnabled: true, UltraFastMultiplier: 6}
	for _, tc := range []struct {
		name, metadata string
		fast, ultra    bool
		want           float64
	}{
		{"request fallback", `{}`, true, true, 12},
		{"actual ultra", `{"effective_service_tier":" UltraFast "}`, true, true, 12},
		{"response priority downgrade", `{"response_service_tier":"priority"}`, true, true, 4},
		{"actual default downgrade", `{"effective_service_tier":"default","response_service_tier":"ultrafast"}`, true, true, 2},
		{"outbound default downgrade", `{"outbound_service_tier":"default"}`, true, true, 2},
		{"ultra without fast", `{}`, false, true, 12},
		{"disabled ultra does not use fast", `{}`, true, false, 2},
		{"unknown actual is not charged as requested ultra", `{"effective_service_tier":"future-tier"}`, true, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			price := base
			price.FastEnabled, price.UltraFastEnabled = tc.fast, tc.ultra
			record := UsageRecord{Provider: &provider, Model: &model, RequestServiceTier: &requested,
				CPAMetadataJSON: tc.metadata, InputTokens: 1_000_000, TotalTokens: 1_000_000}
			got, unpriced := calculateRecordCost(record, pricesByKey([]ModelPrice{price}))
			if unpriced || got != tc.want {
				t.Fatalf("cost=%v unpriced=%v want=%v", got, unpriced, tc.want)
			}
		})
	}
}

func TestUltraFastPeakLongContextAndPerRequest(t *testing.T) {
	provider, model, tier := "openai", "gpt-ultra-long", "ultrafast"
	price := ModelPrice{Provider: provider, Model: model, InputUSDPerMillion: 1,
		FastEnabled: true, FastMultiplier: 2, UltraFastEnabled: true, UltraFastMultiplier: 6,
		LongContextEnabled: true, LongContextThresholdTokens: 100, LongContextInputUSDPerMillion: 3,
		OffPeakEnabled: true, PeakInputUSDPerMillion: 2, LongContextPeakInputUSDPerMillion: 6,
		calendar: &pricingCalendar{years: map[int]map[string]bool{2026: {"2026-09-25": true}}}}
	for _, tc := range []struct {
		hour, day, tokens int
		unsupported       bool
		want              float64
	}{
		{10, 24, 100, false, .0012}, {10, 24, 101, false, .003636},
		{12, 24, 101, false, .001818}, {10, 25, 101, false, .001818},
		{10, 24, 101, true, .000606},
	} {
		price.LongContextUltraFastUnsupported = tc.unsupported
		got, unpriced := calculateRecordCost(UsageRecord{Provider: &provider, Model: &model, RequestServiceTier: &tier,
			Timestamp: time.Date(2026, 9, tc.day, tc.hour, 0, 0, 0, appTimeLocation), InputTokens: tc.tokens}, pricesByKey([]ModelPrice{price}))
		if unpriced || got != tc.want {
			t.Fatalf("%+v cost=%v unpriced=%v", tc, got, unpriced)
		}
	}
	model = "gpt-image-ultra"
	request, peak := .5, 2.0
	price.Model, price.RequestUSD, price.PeakRequestUSD = model, &request, &peak
	price.LongContextEnabled = false
	for _, tc := range []struct {
		hour int
		want float64
	}{{10, 12}, {12, 3}} {
		got, unpriced := calculateRecordCost(UsageRecord{Provider: &provider, Model: &model, RequestServiceTier: &tier,
			Timestamp: time.Date(2026, 9, 24, tc.hour, 0, 0, 0, appTimeLocation), TotalTokens: 1}, pricesByKey([]ModelPrice{price}))
		if unpriced || got != tc.want {
			t.Fatalf("request price=%v want=%v unpriced=%v", got, tc.want, unpriced)
		}
	}
}

func TestUltraFastPricePersistenceValidationAndSync(t *testing.T) {
	a := newQuotaTestApp(t)
	ctx := context.Background()
	multiplier := 6.0
	payload := modelPricePayload{Provider: "openai", Model: "gpt-ultra", InputUSDPerMillion: 2, OutputUSDPerMillion: 4,
		UltraFastEnabled: true, UltraFastMultiplier: &multiplier, LongContextEnabled: true, LongContextThresholdTokens: 100,
		LongContextUltraFastUnsupported: true}
	price, err := a.createPrice(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !price.UltraFastEnabled || price.UltraFastMultiplier != 6 || !price.LongContextUltraFastUnsupported || price.FastEnabled {
		t.Fatalf("created %+v", price)
	}
	payload.UltraFastMultiplier = nil
	price, err = a.updatePrice(ctx, price.ID, payload)
	if err != nil || price.UltraFastMultiplier != 6 {
		t.Fatalf("omitted multiplier not preserved: %+v %v", price, err)
	}
	if _, err = a.db.Exec(`UPDATE model_prices SET source='models.dev',auto_synced=1 WHERE id=?`, price.ID); err != nil {
		t.Fatal(err)
	}
	// Local-only tier edits must retain automatic synchronization.
	price, err = a.updatePrice(ctx, price.ID, payload)
	if err != nil || !price.AutoSynced {
		t.Fatalf("local tier edit disabled sync: %+v %v", price, err)
	}
	catalog := map[string]any{"openai": map[string]any{"models": map[string]any{"gpt-ultra": map[string]any{"cost": map[string]any{"input": 3.0, "output": 5.0}}}}}
	if _, err = a.syncModelsDevPrices(ctx, defaultModelsDevPricingURL, catalog); err != nil {
		t.Fatal(err)
	}
	prices, err := a.listPrices(ctx)
	if err != nil || len(prices) != 1 {
		t.Fatalf("prices=%v err=%v", prices, err)
	}
	price = prices[0]
	if !price.UltraFastEnabled || price.UltraFastMultiplier != 6 || !price.LongContextUltraFastUnsupported || price.InputUSDPerMillion != 3 {
		t.Fatalf("sync lost local ultra tiers: %+v", price)
	}
	payload.UltraFastEnabled = false
	price, err = a.updatePrice(ctx, price.ID, payload)
	if err != nil || price.UltraFastEnabled || price.UltraFastMultiplier != 1 || price.LongContextUltraFastUnsupported {
		t.Fatalf("disable=%+v %v", price, err)
	}
	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		payload.UltraFastEnabled = true
		payload.UltraFastMultiplier = &value
		if _, err := validatePricePayload(payload); err == nil {
			t.Fatalf("accepted multiplier %v", value)
		}
	}
}
