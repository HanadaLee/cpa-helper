package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	usageRollupBatchSize   = 2000
	usageCleanupBatchSize  = 1000
	usageMaintenanceTick   = time.Minute
	usageMaintenanceBudget = 5 * time.Second
)

type UsageMaintenanceRunner struct {
	app  *App
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

type usageRollupKey struct {
	bucketStart       string
	usageUsername     string
	apiKeyDescription string
	provider          string
	model             string
	sourceKey         string
	source            string
	auth              string
	endpoint          string
	failed            bool
}

type usageRollupAccumulator struct {
	metrics usageAggregateMetrics
}

func NewUsageMaintenanceRunner(app *App) *UsageMaintenanceRunner {
	return &UsageMaintenanceRunner{app: app}
}

func (r *UsageMaintenanceRunner) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done != nil {
		select {
		case <-r.done:
		default:
			return
		}
	}
	r.stop = make(chan struct{})
	r.done = make(chan struct{})
	go r.loop()
}

func (r *UsageMaintenanceRunner) Stop() {
	r.mu.Lock()
	stop, done := r.stop, r.done
	if stop == nil || done == nil {
		r.mu.Unlock()
		return
	}
	select {
	case <-stop:
	default:
		close(stop)
	}
	r.mu.Unlock()
	<-done
}

func (r *UsageMaintenanceRunner) loop() {
	defer func() {
		r.mu.Lock()
		if r.done != nil {
			close(r.done)
		}
		r.mu.Unlock()
	}()
	ticker := time.NewTicker(usageMaintenanceTick)
	defer ticker.Stop()
	for {
		r.runCycle()
		select {
		case <-r.stop:
			return
		case <-ticker.C:
		}
	}
}

func (r *UsageMaintenanceRunner) runCycle() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	deadline := time.Now().Add(usageMaintenanceBudget)
	for time.Now().Before(deadline) {
		processed, err := r.app.rollupUsageBatch(ctx, usageRollupBatchSize)
		if err != nil {
			r.app.setUsageMaintenanceError(ctx, err)
			return
		}
		if processed < usageRollupBatchSize {
			break
		}
	}
	if !r.app.usageCleanupDue(ctx, time.Now()) {
		return
	}
	cfg, err := r.app.loadConfig(ctx)
	if err != nil {
		r.app.setUsageMaintenanceError(ctx, err)
		return
	}
	deletedTotal := 0
	for time.Now().Before(deadline) {
		deleted, err := r.app.cleanupUsageBatch(ctx, cfg.UsageDetailRetentionDays, usageCleanupBatchSize)
		if err != nil {
			r.app.setUsageMaintenanceError(ctx, err)
			return
		}
		deletedTotal += deleted
		if deleted < usageCleanupBatchSize {
			r.app.completeUsageCleanup(ctx, deletedTotal)
			return
		}
	}
}

func (a *App) rollupUsageBatch(ctx context.Context, limit int) (processed int, err error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var watermark int
	if err := tx.QueryRowContext(ctx, `SELECT last_rolled_usage_id FROM usage_rollup_state WHERE id = 1`+tx.ForUpdate()).Scan(&watermark); err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, CAST(timestamp AS TEXT), usage_username, api_key_description, provider, model,
		       source_key, source, auth, endpoint, ttft_ms, failed, input_tokens, output_tokens, cached_tokens,
		       cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, cost_usd, unpriced
		FROM usage_records
		WHERE id > ?
		ORDER BY id
		LIMIT ?
	`, watermark, limit)
	if err != nil {
		return 0, err
	}
	rollups := map[usageRollupKey]*usageRollupAccumulator{}
	maxID := watermark
	for rows.Next() {
		var id int
		var timestamp string
		var username, description, provider, model, sourceKey, source, auth, endpoint sql.NullString
		var ttft sql.NullFloat64
		var record UsageRecord
		if err := rows.Scan(&id, &timestamp, &username, &description, &provider, &model, &sourceKey, &source, &auth, &endpoint,
			&ttft, &record.Failed, &record.InputTokens, &record.OutputTokens, &record.CachedTokens,
			&record.CacheReadTokens, &record.CacheCreationTokens, &record.ReasoningTokens, &record.TotalTokens,
			&record.CostUSD, &record.Unpriced); err != nil {
			_ = rows.Close()
			return 0, err
		}
		parsed, ok := parseDBTime(timestamp)
		if !ok {
			_ = rows.Close()
			return 0, fmt.Errorf("usage record %d has invalid timestamp %q", id, timestamp)
		}
		record.Timestamp = parsed
		record.Provider = nullableString(provider)
		record.Model = nullableString(model)
		record.TTFTMS = nullableFloat(ttft)
		record.CostStored = true
		bucket := parsed.In(appTimeLocation)
		bucket = time.Date(bucket.Year(), bucket.Month(), bucket.Day(), bucket.Hour(), 0, 0, 0, appTimeLocation)
		key := usageRollupKey{
			bucketStart:       dbTime(bucket),
			usageUsername:     normalizedRollupDimension(username),
			apiKeyDescription: normalizedRollupDimension(description),
			provider:          normalizedRollupDimension(provider),
			model:             normalizedRollupDimension(model),
			sourceKey:         normalizedRollupDimension(sourceKey),
			source:            normalizedRollupDimension(source),
			auth:              normalizedRollupDimension(auth),
			endpoint:          normalizedRollupDimension(endpoint),
			failed:            record.Failed,
		}
		cost, unpriced := recordCost(record, nil)
		metrics := usageMetricsForRecord(record, cost, unpriced)
		accumulator := rollups[key]
		if accumulator == nil {
			accumulator = &usageRollupAccumulator{}
			rollups[key] = accumulator
		}
		mergeUsageAggregateMetrics(&accumulator.metrics, metrics)
		if id > maxID {
			maxID = id
		}
		processed++
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	now := dbTime(time.Now())
	for key, accumulator := range rollups {
		metrics := accumulator.metrics
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage_hourly_rollups (
				bucket_start, usage_username, api_key_description, provider, model, source_key, source, auth, endpoint, failed,
				record_count, failed_count, input_tokens, output_tokens, cached_tokens,
				cache_read_tokens, cache_creation_tokens, cache_hit_tokens,
				reasoning_tokens, total_tokens, zero_token_records, cost_usd, unpriced_records, ttft_ms_sum, ttft_sample_count,
				first_timestamp, last_timestamp, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(bucket_start, usage_username, api_key_description, provider, model, source_key, source, auth, endpoint, failed)
			DO UPDATE SET
				record_count = usage_hourly_rollups.record_count + excluded.record_count,
				failed_count = usage_hourly_rollups.failed_count + excluded.failed_count,
				input_tokens = usage_hourly_rollups.input_tokens + excluded.input_tokens,
				output_tokens = usage_hourly_rollups.output_tokens + excluded.output_tokens,
				cached_tokens = usage_hourly_rollups.cached_tokens + excluded.cached_tokens,
				cache_read_tokens = usage_hourly_rollups.cache_read_tokens + excluded.cache_read_tokens,
				cache_creation_tokens = usage_hourly_rollups.cache_creation_tokens + excluded.cache_creation_tokens,
				cache_hit_tokens = usage_hourly_rollups.cache_hit_tokens + excluded.cache_hit_tokens,
				reasoning_tokens = usage_hourly_rollups.reasoning_tokens + excluded.reasoning_tokens,
				total_tokens = usage_hourly_rollups.total_tokens + excluded.total_tokens,
				zero_token_records = usage_hourly_rollups.zero_token_records + excluded.zero_token_records,
				cost_usd = ROUND(usage_hourly_rollups.cost_usd + excluded.cost_usd, 8),
				unpriced_records = usage_hourly_rollups.unpriced_records + excluded.unpriced_records,
				ttft_ms_sum = usage_hourly_rollups.ttft_ms_sum + excluded.ttft_ms_sum,
				ttft_sample_count = usage_hourly_rollups.ttft_sample_count + excluded.ttft_sample_count,
				first_timestamp = CASE WHEN usage_hourly_rollups.first_timestamp < excluded.first_timestamp THEN usage_hourly_rollups.first_timestamp ELSE excluded.first_timestamp END,
				last_timestamp = CASE WHEN usage_hourly_rollups.last_timestamp > excluded.last_timestamp THEN usage_hourly_rollups.last_timestamp ELSE excluded.last_timestamp END,
				updated_at = excluded.updated_at
		`, key.bucketStart, key.usageUsername, key.apiKeyDescription, key.provider, key.model, key.sourceKey, key.source, key.auth, key.endpoint, key.failed,
			metrics.records, metrics.failed, metrics.input, metrics.output, metrics.cached,
			metrics.cacheRead, metrics.cacheCreate, metrics.cacheHit,
			metrics.reasoning, metrics.tokens, metrics.zeroToken, mathRound(metrics.cost, 8), metrics.unpriced, metrics.ttftTotal, metrics.ttftCount,
			dbTime(metrics.firstSeenAt), dbTime(metrics.lastSeenAt), now); err != nil {
			return 0, err
		}
	}
	phase := "backfilling"
	if processed < limit {
		phase = "ready"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE usage_rollup_state
		SET last_rolled_usage_id = ?, phase = ?, last_success_at = ?, last_error = NULL, updated_at = ?
		WHERE id = 1
	`, maxID, phase, now, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	committed = true
	return processed, nil
}

func normalizedRollupDimension(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return strings.TrimSpace(value.String)
}

func mergeUsageAggregateMetrics(target *usageAggregateMetrics, value usageAggregateMetrics) {
	target.records += value.records
	target.failed += value.failed
	target.input += value.input
	target.output += value.output
	target.cached += value.cached
	target.cacheRead += value.cacheRead
	target.cacheCreate += value.cacheCreate
	target.cacheHit += value.cacheHit
	target.reasoning += value.reasoning
	target.tokens += value.tokens
	target.zeroToken += value.zeroToken
	target.cost = mathRound(target.cost+value.cost, 8)
	target.unpriced += value.unpriced
	target.ttftTotal += value.ttftTotal
	target.ttftCount += value.ttftCount
	if target.firstSeenAt.IsZero() || value.firstSeenAt.Before(target.firstSeenAt) {
		target.firstSeenAt = value.firstSeenAt
	}
	if target.lastSeenAt.IsZero() || value.lastSeenAt.After(target.lastSeenAt) {
		target.lastSeenAt = value.lastSeenAt
	}
}

func (a *App) usageCleanupDue(ctx context.Context, now time.Time) bool {
	var lastCleanup sql.NullString
	if err := a.db.QueryRowContext(ctx, `SELECT CAST(last_cleanup_at AS TEXT) FROM usage_rollup_state WHERE id = 1`).Scan(&lastCleanup); err != nil {
		return false
	}
	if !lastCleanup.Valid {
		return true
	}
	parsed, ok := parseDBTime(lastCleanup.String)
	if !ok {
		return true
	}
	return now.Sub(parsed) >= 24*time.Hour
}

func (a *App) cleanupUsageBatch(ctx context.Context, retentionDays, limit int) (deleted int, err error) {
	if retentionDays < minimumUsageRetentionDays {
		return 0, fmt.Errorf("usage detail retention days must be at least %d", minimumUsageRetentionDays)
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var watermark int
	if err := tx.QueryRowContext(ctx, `SELECT last_rolled_usage_id FROM usage_rollup_state WHERE id = 1`+tx.ForUpdate()).Scan(&watermark); err != nil {
		return 0, err
	}
	cutoff := dbTime(time.Now().In(appTimeLocation).AddDate(0, 0, -retentionDays))
	result, err := tx.ExecContext(ctx, `
		DELETE FROM usage_records
		WHERE id IN (
			SELECT id FROM usage_records
			WHERE id <= ? AND timestamp < ?
			ORDER BY id
			LIMIT ?
		)
	`, watermark, cutoff, limit)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	committed = true
	if count > 0 {
		a.invalidateUsageOptions()
	}
	return int(count), nil
}

func (a *App) completeUsageCleanup(ctx context.Context, deleted int) {
	now := dbTime(time.Now())
	_, _ = a.db.ExecContext(ctx, `
		UPDATE usage_rollup_state
		SET last_cleanup_at = ?, last_cleanup_count = ?, last_error = NULL, updated_at = ?
		WHERE id = 1
	`, now, deleted, now)
}

func (a *App) setUsageMaintenanceError(ctx context.Context, maintenanceErr error) {
	message := maintenanceErr.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, _ = a.db.ExecContext(ctx, `
		UPDATE usage_rollup_state SET phase = 'error', last_error = ?, updated_at = ? WHERE id = 1
	`, message, dbTime(time.Now()))
}
