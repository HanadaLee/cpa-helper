package app

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"
)

func TestStoredUsageCostRollupCleanupAndPermanentDedup(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()

	now := dbTime(time.Now())
	if _, err := app.db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million,
			source, updated_at
		) VALUES ('openai', 'gpt-fixed-cost', 1, 2, 0.5, 0, 'manual', ?)
	`, now); err != nil {
		t.Fatalf("insert model price: %v", err)
	}
	userID := seedQuotaTestUser(t, app, "fixed-cost-user")
	seedQuotaTestAPIKey(t, app, userID, "sk-fixed-cost")
	if _, err := app.db.Exec(`UPDATE users SET quota_daily_usd = 0, quota_weekly_usd = 0 WHERE id = ?`, userID); err != nil {
		t.Fatalf("enable test quota: %v", err)
	}
	cardID := seedQuotaCard(t, app, userID, "credit", 100, nil)

	usageTime := time.Now().In(appTimeLocation).AddDate(0, 0, -40).Truncate(time.Hour)
	raw := fmt.Sprintf(`{
		"timestamp": %q,
		"api_key": "sk-fixed-cost",
		"auth_type": "api_key",
		"provider": "openai",
		"model": "gpt-fixed-cost",
		"endpoint": "/v1/chat/completions",
		"source": "retention-source",
		"input_tokens": 1000000,
		"output_tokens": 0,
		"cached_tokens": 0,
		"cache_read_tokens": 123,
		"cache_creation_tokens": 456,
		"total_tokens": 1000000
	}`, dbTime(usageTime))
	record, created, err := app.saveUsageMessage(context.Background(), []byte(raw))
	if err != nil || !created {
		t.Fatalf("save usage = created %v, err %v", created, err)
	}
	if math.Abs(record.CostUSD-1) > 0.00000001 || record.Unpriced || !record.CostStored {
		t.Fatalf("stored usage cost = %.8f, unpriced %v, stored %v; want 1/false/true", record.CostUSD, record.Unpriced, record.CostStored)
	}

	if _, err := app.db.Exec(`
		UPDATE model_prices
		SET input_usd_per_million = 9, output_usd_per_million = 9, updated_at = ?
		WHERE provider = 'openai' AND model = 'gpt-fixed-cost'
	`, dbTime(time.Now())); err != nil {
		t.Fatalf("update model price: %v", err)
	}
	app.invalidateUsagePrices()
	prices, err := app.priceMap(context.Background())
	if err != nil {
		t.Fatalf("reload prices: %v", err)
	}
	storedRecord, err := app.getUsageRecord(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("get stored usage: %v", err)
	}
	amount, unpriced := recordCost(storedRecord, prices)
	if math.Abs(amount-1) > 0.00000001 || unpriced {
		t.Fatalf("cost after price update = %.8f, unpriced %v; want original 1/false", amount, unpriced)
	}

	processed, err := app.rollupUsageBatch(context.Background(), usageRollupBatchSize)
	if err != nil || processed != 1 {
		t.Fatalf("rollup batch = processed %d, err %v", processed, err)
	}
	var rolledCacheRead, rolledCacheCreation int
	if err := app.db.QueryRow(`SELECT cache_read_tokens, cache_creation_tokens FROM usage_hourly_rollups`).Scan(&rolledCacheRead, &rolledCacheCreation); err != nil {
		t.Fatalf("query complete rollup token composition: %v", err)
	}
	if rolledCacheRead != 123 || rolledCacheCreation != 456 {
		t.Fatalf("rollup cache tokens = %d/%d, want 123/456", rolledCacheRead, rolledCacheCreation)
	}
	start := usageTime
	end := usageTime.Add(time.Hour)
	aggregate, err := app.buildUsageAggregate(context.Background(), UsageFilters{Start: &start, End: &end}, usageAggregateSummary, nil)
	if err != nil {
		t.Fatalf("build rollup aggregate: %v", err)
	}
	if aggregate.summary.records != 1 || aggregate.summary.tokens != 1000000 || math.Abs(aggregate.summary.cost-1) > 0.00000001 {
		t.Fatalf("rollup summary = records %d, tokens %d, cost %.8f", aggregate.summary.records, aggregate.summary.tokens, aggregate.summary.cost)
	}

	deleted, err := app.cleanupUsageBatch(context.Background(), minimumUsageRetentionDays, usageCleanupBatchSize)
	if err != nil || deleted != 1 {
		t.Fatalf("cleanup batch = deleted %d, err %v", deleted, err)
	}
	var detailCount, dedupCount int
	var dedupUsageRecordID, chargeUsageRecordID any
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM usage_records WHERE id = ?`, record.ID).Scan(&detailCount); err != nil {
		t.Fatalf("count detail: %v", err)
	}
	if err := app.db.QueryRow(`SELECT COUNT(*), MAX(usage_record_id) FROM usage_ingest_dedup WHERE dedupe_key = ?`, record.DedupeKey).Scan(&dedupCount, &dedupUsageRecordID); err != nil {
		t.Fatalf("query permanent dedup: %v", err)
	}
	if detailCount != 0 || dedupCount != 1 || dedupUsageRecordID != nil {
		t.Fatalf("post-cleanup detail/dedup = %d/%d/%v, want 0/1/nil", detailCount, dedupCount, dedupUsageRecordID)
	}
	var chargedAmount float64
	if err := app.db.QueryRow(`SELECT usage_record_id, amount_usd FROM user_quota_charges WHERE usage_dedupe_key = ?`, record.DedupeKey).Scan(&chargeUsageRecordID, &chargedAmount); err != nil {
		t.Fatalf("query retained quota audit: %v", err)
	}
	if chargeUsageRecordID != nil || math.Abs(chargedAmount-1) > 0.00000001 {
		t.Fatalf("quota audit after cleanup = usage record %v, amount %.8f; want nil/1", chargeUsageRecordID, chargedAmount)
	}
	var cardDeduction float64
	if err := app.db.QueryRow(`SELECT amount_usd FROM quota_card_deductions WHERE card_id = ?`, cardID).Scan(&cardDeduction); err != nil || cardDeduction != 1 {
		t.Fatalf("card audit must survive detail cleanup: %v %v", cardDeduction, err)
	}
	options, err := app.loadUsageOptionsResponse(context.Background(), usageAccessScope{IsAdmin: true}, UsageFilters{Start: &start, End: &end})
	if err != nil {
		t.Fatalf("load retained usage options: %v", err)
	}
	sources := options["sources"].([]map[string]string)
	if len(sources) != 1 || sources[0]["key"] != *usageSourceKey(stringPtr("retention-source")) || sources[0]["label"] != maskSecret(stringPtr("retention-source")) {
		t.Fatalf("retained source options = %#v", sources)
	}

	if _, created, err := app.saveUsageMessage(context.Background(), []byte(raw)); err != nil || created {
		t.Fatalf("replayed cleaned usage = created %v, err %v; want false/nil", created, err)
	}
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM usage_records`).Scan(&detailCount); err != nil {
		t.Fatalf("count replayed details: %v", err)
	}
	if detailCount != 0 {
		t.Fatalf("replayed cleaned usage inserted %d details, want 0", detailCount)
	}
	aggregate, err = app.buildUsageAggregate(context.Background(), UsageFilters{Start: &start, End: &end}, usageAggregateSummary, nil)
	if err != nil {
		t.Fatalf("build aggregate after replay: %v", err)
	}
	if aggregate.summary.records != 1 || math.Abs(aggregate.summary.cost-1) > 0.00000001 {
		t.Fatalf("aggregate after replay = records %d, cost %.8f; want 1/1", aggregate.summary.records, aggregate.summary.cost)
	}
}

func TestCleanupRejectsRetentionBelowMinimum(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()
	if _, err := app.cleanupUsageBatch(context.Background(), minimumUsageRetentionDays-1, usageCleanupBatchSize); err == nil {
		t.Fatal("cleanup accepted a retention period below the minimum")
	}
}

func TestUsageAggregateCombinesPartialHoursRollupAndWatermarkTail(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()

	now := time.Now().In(appTimeLocation)
	base := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, appTimeLocation).Add(-4 * time.Hour)
	insertUsage := func(timestamp time.Time, requestID string, tokens int) {
		t.Helper()
		raw := fmt.Sprintf(`{
			"timestamp": %q,
			"provider": "unknown",
			"model": "hybrid-model",
			"request_id": %q,
			"input_tokens": %d,
			"total_tokens": %d
		}`, dbTime(timestamp), requestID, tokens, tokens)
		if _, created, err := app.saveUsageMessage(context.Background(), []byte(raw)); err != nil || !created {
			t.Fatalf("save %s = created %v, err %v", requestID, created, err)
		}
	}

	insertUsage(base.Add(20*time.Minute), "hybrid-left", 10)
	insertUsage(base.Add(80*time.Minute), "hybrid-full", 20)
	insertUsage(base.Add(140*time.Minute), "hybrid-right", 30)
	if processed, err := app.rollupUsageBatch(context.Background(), usageRollupBatchSize); err != nil || processed != 3 {
		t.Fatalf("initial rollup = processed %d, err %v", processed, err)
	}
	insertUsage(base.Add(90*time.Minute), "hybrid-tail", 40)

	start := base.Add(15 * time.Minute)
	end := base.Add(165 * time.Minute)
	aggregate, err := app.buildUsageAggregate(context.Background(), UsageFilters{Start: &start, End: &end}, usageAggregateSummary, nil)
	if err != nil {
		t.Fatalf("build hybrid aggregate: %v", err)
	}
	if aggregate.summary.records != 4 || aggregate.summary.tokens != 100 || aggregate.summary.unpriced != 4 {
		t.Fatalf("hybrid summary = records %d, tokens %d, unpriced %d; want 4/100/4", aggregate.summary.records, aggregate.summary.tokens, aggregate.summary.unpriced)
	}
}

func TestUsageAggregateRejectsExpiredPartialHourRange(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatalf("NewWithOptions failed: %v", err)
	}
	defer app.Close()

	now := time.Now().In(appTimeLocation).AddDate(0, 0, -defaultUsageRetentionDays-1)
	start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 15, 0, 0, appTimeLocation)
	end := start.Add(30 * time.Minute)
	if _, err := app.buildUsageAggregate(context.Background(), UsageFilters{Start: &start, End: &end}, usageAggregateSummary, nil); err == nil {
		t.Fatal("expired partial-hour aggregate query unexpectedly succeeded")
	}
}
