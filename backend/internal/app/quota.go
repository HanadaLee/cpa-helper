package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

const quotaPauseReasonExhausted = "quota_exhausted"

type userQuotaPayload struct {
	WeeklyQuotaUSD *float64 `json:"weekly_quota_usd"`
	DailyQuotaUSD  *float64 `json:"daily_quota_usd"`
}

func decodeUserQuota(r *http.Request, payload *userQuotaPayload) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(payload); err != nil {
		return validationError("配额仅支持日配额和周配额")
	}
	return nil
}

type UserQuotaStatusResponse struct {
	Unlimited          bool       `json:"unlimited"`
	CardsRemainingUSD  float64    `json:"cards_remaining_usd"`
	CardsTotalUSD      float64    `json:"cards_total_usd"`
	LimitsRemainingUSD float64    `json:"limits_remaining_usd"`
	AvailableUSD       float64    `json:"available_usd"`
	DailyResetsAt      time.Time  `json:"daily_resets_at"`
	WeeklyResetsAt     time.Time  `json:"weekly_resets_at"`
	WeeklyQuotaUSD     *float64   `json:"weekly_quota_usd"`
	WeeklyUsedUSD      float64    `json:"weekly_used_usd"`
	WeeklyRemainingUSD *float64   `json:"weekly_remaining_usd"`
	QuotaWeek          string     `json:"quota_week"`
	DailyQuotaUSD      *float64   `json:"daily_quota_usd"`
	DailyUsedUSD       float64    `json:"daily_used_usd"`
	DailyRemainingUSD  *float64   `json:"daily_remaining_usd"`
	QuotaDay           string     `json:"quota_day"`
	Paused             bool       `json:"paused"`
	PausedAt           *time.Time `json:"paused_at"`
	PauseReason        *string    `json:"pause_reason"`
	SyncError          *string    `json:"sync_error"`
	UnpricedRecords    int        `json:"unpriced_records"`
	CanCreateKeys      bool       `json:"can_create_keys"`
	StartedAt          *time.Time `json:"started_at"`
}

func (a *App) handleCurrentUserQuota(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	status, err := a.userQuotaStatus(r.Context(), user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, status)
	return nil
}

func (a *App) updateUserQuota(ctx context.Context, userID int, payload userQuotaPayload) (UserQuotaStatusResponse, error) {
	daily, err := normalizedQuotaAmount(payload.DailyQuotaUSD)
	if err != nil {
		return UserQuotaStatusResponse{}, err
	}
	weekly, err := normalizedQuotaAmount(payload.WeeklyQuotaUSD)
	if err != nil {
		return UserQuotaStatusResponse{}, err
	}
	// Both null means explicitly unlimited. A missing bucket in a limited plan is zero.
	if daily != nil || weekly != nil {
		if daily == nil {
			value := 0.0
			daily = &value
		}
		if weekly == nil {
			value := 0.0
			weekly = &value
		}
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return UserQuotaStatusResponse{}, err
	}
	defer tx.Rollback()
	user, err := scanUser(tx.QueryRowContext(ctx, "SELECT "+userSelectColumns+" FROM users WHERE id = ?"+tx.ForUpdate(), userID))
	if errors.Is(err, sql.ErrNoRows) {
		return UserQuotaStatusResponse{}, notFoundError("用户不存在")
	}
	if err != nil {
		return UserQuotaStatusResponse{}, err
	}
	now := time.Now()
	user = quotaPeriodsAt(user, now)
	_, err = tx.ExecContext(ctx, `
		UPDATE users SET quota_daily_usd = ?, quota_weekly_usd = ?,
		    quota_started_at = COALESCE(quota_started_at, ?),
		    quota_day = ?, quota_day_used_usd = ?, quota_week = ?, quota_week_used_usd = ?,
		    updated_at = ? WHERE id = ?
	`, quotaAmountArg(daily), quotaAmountArg(weekly), dbTime(now),
		quotaDay(now), user.QuotaDayUsedUSD, quotaWeek(now), user.QuotaWeekUsedUSD, dbTime(now), userID)
	if err != nil {
		return UserQuotaStatusResponse{}, err
	}
	if err = tx.Commit(); err != nil {
		return UserQuotaStatusResponse{}, err
	}
	return a.userQuotaStatus(ctx, userID)
}

func (a *App) userQuotaStatus(ctx context.Context, userID int) (UserQuotaStatusResponse, error) {
	a.quotaSyncMu.Lock()
	defer a.quotaSyncMu.Unlock()
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return UserQuotaStatusResponse{}, err
	}
	user, err = a.ensureQuotaPeriods(ctx, user)
	if err != nil {
		return UserQuotaStatusResponse{}, err
	}
	if !quotaHasAvailable(user) {
		_ = a.pauseUserKeysForQuota(ctx, user.ID, quotaPauseReasonExhausted)
		user, err = a.getUser(ctx, userID)
		if err != nil {
			return UserQuotaStatusResponse{}, err
		}
	} else if user.QuotaPausedAt != nil {
		_ = a.restoreQuotaPausedUserIfAvailable(ctx, user.ID)
		user, err = a.getUser(ctx, userID)
		if err != nil {
			return UserQuotaStatusResponse{}, err
		}
	}
	return quotaStatusFromUser(user), nil
}

func (a *App) ensureUserQuotaReadyForKeys(ctx context.Context, userID int) error {
	status, err := a.userQuotaStatus(ctx, userID)
	if err != nil {
		return err
	}
	if !status.CanCreateKeys {
		return conflictError("用户额度已用尽，API KEY 已暂停，请联系管理员补充额度")
	}
	return nil
}

func (a *App) applyQuotaCharge(ctx context.Context, record UsageRecord) error {
	if record.UsageUsername == nil || strings.TrimSpace(*record.UsageUsername) == "" {
		return nil
	}
	// Read balances, deduplicate, allocate and update all buckets in one transaction.
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	user, err := scanUser(tx.QueryRowContext(ctx, "SELECT "+userSelectColumns+" FROM users WHERE username = ?"+tx.ForUpdate(), *record.UsageUsername))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if quotaIsUnlimited(user) {
		return nil
	}
	var existing int
	err = tx.QueryRowContext(ctx, "SELECT id FROM user_quota_charges WHERE usage_dedupe_key = ?", record.DedupeKey).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now()
	user = quotaPeriodsAt(user, now)
	cards, err := quotaCardsForUser(ctx, tx, user.ID)
	if err != nil {
		return err
	}
	type bucket struct {
		kind      string
		id        int
		expires   time.Time
		remaining float64
		deducted  float64
	}
	dayEnd, _ := quotaPeriodEnds(now)
	buckets := []bucket{
		{kind: "limit", expires: dayEnd, remaining: quotaLimitsRemaining(user)},
	}
	for _, card := range cards {
		if card.Status != "active" || card.Kind != "credit" {
			continue
		}
		expiry := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
		if card.ExpiresAt != nil {
			expiry = *card.ExpiresAt
		}
		buckets = append(buckets, bucket{kind: "card", id: card.ID, expires: expiry, remaining: card.RemainingUSD})
	}
	// Daily and weekly constrain one shared bucket. Its next expiry is midnight.
	// Equal deadlines prefer the base limit, then card ID.
	sort.SliceStable(buckets, func(i, j int) bool { return buckets[i].expires.Before(buckets[j].expires) })
	amount, unpriced := recordCost(record, nil)
	amount = mathRound(amount, 8)
	remaining := amount
	limitDeducted, cardsDeducted := 0.0, 0.0
	if !unpriced {
		for i := range buckets {
			b := &buckets[i]
			b.deducted = minQuotaAmount(remaining, b.remaining)
			if b.deducted <= 0 {
				continue
			}
			remaining = mathRound(remaining-b.deducted, 8)
			switch b.kind {
			case "limit":
				limitDeducted += b.deducted
			case "card":
				cardsDeducted = mathRound(cardsDeducted+b.deducted, 8)
			}
		}
	} else {
		user.QuotaUnpricedRecords++
	}
	result, err := tx.InsertContext(ctx, `
		INSERT INTO user_quota_charges (
			usage_record_id, usage_dedupe_key, usage_timestamp, user_id, usage_username, amount_usd,
			daily_deducted_usd, weekly_deducted_usd, limit_deducted_usd, cards_deducted_usd, uncovered_usd, unpriced,
			quota_day, quota_week, quota_month, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?)
	`, record.ID, record.DedupeKey, dbTime(record.Timestamp), user.ID, user.Username, amount,
		limitDeducted, limitDeducted, limitDeducted, cardsDeducted, remaining, unpriced, user.QuotaDay, user.QuotaWeek, dbTime(now))
	if err != nil {
		return err
	}
	chargeID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	for _, b := range buckets {
		if b.kind != "card" || b.deducted <= 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE quota_cards SET used_usd = ROUND(used_usd + ?, 8), updated_at = ? WHERE id = ?`, b.deducted, dbTime(now), b.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO quota_card_deductions(card_id, charge_id, amount_usd, created_at) VALUES (?, ?, ?, ?)`, b.id, chargeID, b.deducted, dbTime(now)); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE users SET quota_day = ?, quota_day_used_usd = ?, quota_week = ?, quota_week_used_usd = ?,
		    quota_unpriced_records = ?, updated_at = ? WHERE id = ?
	`, user.QuotaDay, mathRound(user.QuotaDayUsedUSD+limitDeducted, 8),
		user.QuotaWeek, mathRound(user.QuotaWeekUsedUSD+limitDeducted, 8), user.QuotaUnpricedRecords, dbTime(now), user.ID)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	_, err = a.userQuotaStatus(ctx, user.ID)
	return err
}

func quotaPeriodsAt(user UserRecord, now time.Time) UserRecord {
	if user.QuotaDay != quotaDay(now) {
		user.QuotaDay = quotaDay(now)
		user.QuotaDayUsedUSD = 0
	}
	if user.QuotaWeek != quotaWeek(now) {
		user.QuotaWeek = quotaWeek(now)
		user.QuotaWeekUsedUSD = 0
	}
	return user
}

func (a *App) ensureQuotaPeriods(ctx context.Context, user UserRecord) (UserRecord, error) {
	now := time.Now()
	// Conditional SQL avoids overwriting a concurrent charge or manual reset.
	_, err := a.db.ExecContext(ctx, `
		UPDATE users SET
		    quota_day_used_usd = CASE WHEN quota_day = ? THEN quota_day_used_usd ELSE 0 END,
		    quota_week_used_usd = CASE WHEN quota_week = ? THEN quota_week_used_usd ELSE 0 END,
		    quota_day = ?, quota_week = ?, updated_at = ?
		WHERE id = ? AND (quota_day <> ? OR quota_week <> ?)
	`, quotaDay(now), quotaWeek(now), quotaDay(now), quotaWeek(now), dbTime(now), user.ID, quotaDay(now), quotaWeek(now))
	if err != nil {
		return UserRecord{}, err
	}
	return a.getUser(ctx, user.ID)
}

func quotaPeriodEnds(now time.Time) (time.Time, time.Time) {
	local := now.In(appTimeLocation)
	day := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, appTimeLocation)
	days := (8 - int(local.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	week := time.Date(local.Year(), local.Month(), local.Day()+days, 0, 0, 0, 0, appTimeLocation)
	return day, week
}

func quotaValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func (a *App) pauseUserKeysForQuota(ctx context.Context, userID int, reason string) error {
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return err
	}
	if user.QuotaPausedAt != nil && user.QuotaSyncError == nil {
		return nil
	}
	now := dbTime(time.Now())
	_, err = a.db.ExecContext(ctx, `
		UPDATE users
		SET quota_paused_at = COALESCE(quota_paused_at, ?),
		    quota_pause_reason = ?, quota_sync_error = NULL, updated_at = ?
		WHERE id = ?
	`, now, reason, now, userID)
	if err != nil {
		return err
	}
	keys, err := a.userAPIKeys(ctx, userID)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := a.removeRemoteAPIKeyHash(ctx, key.APIKeyHash); err != nil {
			_ = a.setQuotaSyncError(ctx, userID, err)
			return nil
		}
	}
	return nil
}

func (a *App) restoreQuotaPausedUserIfAvailable(ctx context.Context, userID int) error {
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return err
	}
	user, err = a.ensureQuotaPeriods(ctx, user)
	if err != nil {
		return err
	}
	if user.QuotaPausedAt == nil || user.DisabledAt != nil || !quotaHasAvailable(user) {
		return nil
	}
	keys, err := a.userAPIKeys(ctx, userID)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if key.Disabled {
			continue
		}
		if key.APIKey == nil {
			return a.setQuotaSyncMessage(ctx, userID, "存在无法恢复的 API KEY，请重新绑定后再恢复")
		}
	}
	restored := []string{}
	for _, key := range keys {
		if key.Disabled || key.APIKey == nil {
			continue
		}
		if err := a.addRemoteAPIKey(ctx, *key.APIKey); err != nil {
			for _, hash := range restored {
				_ = a.removeRemoteAPIKeyHash(ctx, hash)
			}
			_ = a.setQuotaSyncError(ctx, userID, err)
			return nil
		}
		restored = append(restored, key.APIKeyHash)
	}
	_, err = a.db.ExecContext(ctx, `
		UPDATE users
		SET quota_paused_at = NULL, quota_pause_reason = NULL,
		    quota_sync_error = NULL, updated_at = ?
		WHERE id = ?
	`, dbTime(time.Now()), userID)
	return err
}

func (a *App) setQuotaSyncError(ctx context.Context, userID int, err error) error {
	return a.setQuotaSyncMessage(ctx, userID, err.Error())
}

func (a *App) setQuotaSyncMessage(ctx context.Context, userID int, message string) error {
	if len(message) > 1000 {
		message = message[:1000]
	}
	_, err := a.db.ExecContext(ctx, `UPDATE users SET quota_sync_error = ?, updated_at = ? WHERE id = ?`, message, dbTime(time.Now()), userID)
	return err
}

func quotaStatusFromUser(user UserRecord) UserQuotaStatusResponse {
	dayEnd, weekEnd := quotaPeriodEnds(time.Now())
	return UserQuotaStatusResponse{
		Unlimited:          quotaIsUnlimited(user),
		CardsRemainingUSD:  mathRound(user.QuotaCardsRemainingUSD, 8),
		CardsTotalUSD:      mathRound(user.QuotaCardsTotalUSD, 8),
		LimitsRemainingUSD: quotaLimitsRemaining(user),
		AvailableUSD:       mathRound(quotaLimitsRemaining(user)+user.QuotaCardsRemainingUSD, 8),
		DailyResetsAt:      dayEnd, WeeklyResetsAt: weekEnd,
		WeeklyQuotaUSD: user.QuotaWeeklyUSD, WeeklyUsedUSD: mathRound(user.QuotaWeekUsedUSD, 8),
		WeeklyRemainingUSD: quotaWeeklyRemaining(user), QuotaWeek: user.QuotaWeek,
		DailyQuotaUSD: user.QuotaDailyUSD, DailyUsedUSD: mathRound(user.QuotaDayUsedUSD, 8),
		DailyRemainingUSD: quotaDailyRemaining(user), QuotaDay: user.QuotaDay,
		Paused: user.QuotaPausedAt != nil, PausedAt: user.QuotaPausedAt,
		PauseReason: user.QuotaPauseReason, SyncError: user.QuotaSyncError,
		UnpricedRecords: user.QuotaUnpricedRecords,
		CanCreateKeys:   user.DisabledAt == nil && user.QuotaPausedAt == nil && quotaHasAvailable(user),
		StartedAt:       user.QuotaStartedAt,
	}
}

func quotaIsUnlimited(user UserRecord) bool {
	return user.QuotaDailyUSD == nil && user.QuotaWeeklyUSD == nil
}

func quotaHasAvailable(user UserRecord) bool {
	return quotaIsUnlimited(user) || quotaLimitsRemaining(user) > 0 || user.QuotaCardsRemainingUSD > 0
}

func quotaLimitsRemaining(user UserRecord) float64 {
	return minQuotaAmount(quotaValue(quotaDailyRemaining(user)), quotaValue(quotaWeeklyRemaining(user)))
}

func quotaDailyRemaining(user UserRecord) *float64 {
	if user.QuotaDailyUSD == nil {
		return nil
	}
	remaining := mathRound(*user.QuotaDailyUSD-user.QuotaDayUsedUSD, 8)
	remaining = maxQuotaAmount(remaining, 0)
	return &remaining
}

func quotaWeeklyRemaining(user UserRecord) *float64 {
	if user.QuotaWeeklyUSD == nil {
		return nil
	}
	remaining := mathRound(*user.QuotaWeeklyUSD-user.QuotaWeekUsedUSD, 8)
	remaining = maxQuotaAmount(remaining, 0)
	return &remaining
}

func normalizedQuotaAmount(value *float64) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return nil, validationError("额度金额不能小于 0")
	}
	normalized := mathRound(*value, 8)
	if math.IsNaN(normalized) || math.IsInf(normalized, 0) {
		return nil, validationError("额度金额超出范围")
	}
	return &normalized, nil
}

func newUserQuotaArgs(cfg NewUserQuotaConfig) (lifetime, monthly, weekly, daily any) {
	if cfg.Unlimited {
		return nil, nil, nil, nil
	}
	return 0, 0, cfg.WeeklyUSD, cfg.DailyUSD
}

func quotaAmountArg(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func quotaWeek(value time.Time) string {
	year, week := value.In(appTimeLocation).ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

func quotaDay(value time.Time) string {
	return value.In(appTimeLocation).Format("2006-01-02")
}

func minQuotaAmount(left, right float64) float64 {
	if left < right {
		return mathRound(left, 8)
	}
	return mathRound(right, 8)
}

func maxQuotaAmount(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func isUniqueConstraintError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}
