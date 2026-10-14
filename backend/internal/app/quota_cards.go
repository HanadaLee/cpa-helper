package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cpa-helper/backend/internal/platform/database"
)

type quotaQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type QuotaCardResponse struct {
	ID           int        `json:"id"`
	UserID       int        `json:"user_id"`
	Username     string     `json:"username"`
	Kind         string     `json:"kind"`
	Name         string     `json:"name"`
	AmountUSD    float64    `json:"amount_usd"`
	UsedUSD      float64    `json:"used_usd"`
	RemainingUSD float64    `json:"remaining_usd"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expires_at"`
	ActivatedAt  *time.Time `json:"activated_at"`
	UsedAt       *time.Time `json:"used_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	CreatedAt    *time.Time `json:"created_at"`
	BatchID      string     `json:"batch_id"`
}

const quotaCardColumns = `c.id, c.user_id, u.username, c.kind, c.name, c.amount_usd, c.used_usd,
	CAST(c.expires_at AS TEXT), CAST(c.activated_at AS TEXT), CAST(c.used_at AS TEXT),
	CAST(c.revoked_at AS TEXT), CAST(c.created_at AS TEXT), c.batch_id`

func scanQuotaCard(row userScanner, now time.Time) (QuotaCardResponse, error) {
	var card QuotaCardResponse
	var expires, activated, used, revoked, created sql.NullString
	err := row.Scan(&card.ID, &card.UserID, &card.Username, &card.Kind, &card.Name, &card.AmountUSD,
		&card.UsedUSD, &expires, &activated, &used, &revoked, &created, &card.BatchID)
	if err != nil {
		return card, err
	}
	card.ExpiresAt, card.ActivatedAt, card.UsedAt = timePtr(expires), timePtr(activated), timePtr(used)
	card.RevokedAt, card.CreatedAt = timePtr(revoked), timePtr(created)
	card.RemainingUSD = mathRound(maxQuotaAmount(card.AmountUSD-card.UsedUSD, 0), 8)
	switch {
	case card.RevokedAt != nil:
		card.Status = "revoked"
	case card.UsedAt != nil:
		card.Status = "used"
	case card.Kind == "credit" && card.RemainingUSD <= 0:
		card.Status = "exhausted"
	case card.ExpiresAt != nil && !card.ExpiresAt.After(now):
		card.Status = "expired"
	case card.Kind == "reset":
		card.Status = "unused"
	default:
		card.Status = "active"
	}
	return card, nil
}

func quotaCardsForUser(ctx context.Context, db quotaQuerier, userID int) ([]QuotaCardResponse, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+quotaCardColumns+` FROM quota_cards c JOIN users u ON u.id = c.user_id
		WHERE c.user_id = ? AND c.revoked_at IS NULL AND c.kind = 'credit' AND c.activated_at IS NOT NULL
		AND c.amount_usd > c.used_usd AND (c.expires_at IS NULL OR julianday(c.expires_at) > julianday('now')) ORDER BY c.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []QuotaCardResponse{}
	now := time.Now()
	for rows.Next() {
		card, err := scanQuotaCard(rows, now)
		if err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}

type quotaTargets struct {
	UserIDs  []int `json:"user_ids"`
	AllUsers bool  `json:"all_users"`
}

func quotaTargetIDs(ctx context.Context, tx *database.Tx, target quotaTargets) ([]int, error) {
	if target.AllUsers && len(target.UserIDs) != 0 {
		return nil, validationError("不能同时指定全部用户和用户列表")
	}
	if !target.AllUsers && (len(target.UserIDs) == 0 || len(target.UserIDs) > 1000) {
		return nil, validationError("请选择 1 至 1000 位用户")
	}
	ids := []int{}
	if target.AllUsers {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM users ORDER BY id")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		rows.Close()
		return ids, lockQuotaUsers(ctx, tx, ids)
	}
	seen := map[int]bool{}
	for _, id := range target.UserIDs {
		if id < 1 {
			return nil, validationError("用户 ID 无效")
		}
		if seen[id] {
			continue
		}
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT id FROM users WHERE id = ?", id).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, notFoundError("用户不存在")
			}
			return nil, err
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, lockQuotaUsers(ctx, tx, ids)
}

type quotaCardPayload struct {
	quotaTargets
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	AmountUSD float64    `json:"amount_usd"`
	ExpiresAt *time.Time `json:"expires_at"`
	Count     int        `json:"count"`
}

func validateQuotaCardPayload(p *quotaCardPayload) error {
	if p.Kind != "credit" && p.Kind != "reset" {
		return validationError("卡片类型无效")
	}
	p.Name = strings.TrimSpace(p.Name)
	if len([]rune(p.Name)) > 120 {
		return validationError("卡片名称不能超过 120 个字符")
	}
	value, err := normalizedQuotaAmount(&p.AmountUSD)
	if err != nil {
		return err
	}
	p.AmountUSD = *value
	if p.Kind == "credit" && p.AmountUSD <= 0 {
		return validationError("额度卡金额必须大于 0")
	}
	if p.Kind == "reset" {
		p.AmountUSD = 0
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(time.Now()) {
		return validationError("有效期必须晚于当前时间")
	}
	if p.Count == 0 {
		p.Count = 1
	}
	if p.Count < 1 || p.Count > 100 {
		return validationError("每位用户发放数量必须为 1 至 100")
	}
	return nil
}

func (a *App) issueQuotaCards(ctx context.Context, actorID int, p quotaCardPayload) (map[string]any, error) {
	if err := validateQuotaCardPayload(&p); err != nil {
		return nil, err
	}
	batch := make([]byte, 16)
	if _, err := rand.Read(batch); err != nil {
		return nil, err
	}
	batchID := hex.EncodeToString(batch)
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids, err := quotaTargetIDs(ctx, tx, p.quotaTargets)
	if err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	for _, id := range ids {
		for i := 0; i < p.Count; i++ {
			var activated any
			if p.Kind == "credit" {
				activated = now
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO quota_cards
				(user_id, kind, name, amount_usd, expires_at, activated_at, issued_by, batch_id, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, p.Kind, p.Name, p.AmountUSD, dbTimePtr(p.ExpiresAt), activated, actorID, batchID, now, now)
			if err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	a.syncQuotaUsers(ctx, ids)
	return map[string]any{"issued": len(ids) * p.Count, "batch_id": batchID}, nil
}

func (a *App) syncQuotaUsers(ctx context.Context, ids []int) {
	for _, id := range ids {
		if _, err := a.userQuotaStatus(ctx, id); err != nil {
			_ = a.setQuotaSyncError(ctx, id, err)
		}
	}
}

func (a *App) listQuotaCards(w http.ResponseWriter, r *http.Request, userID int) error {
	page, size := quotaPage(r)
	where := "1=1"
	args := []any{}
	if userID != 0 {
		where += " AND c.user_id = ?"
		args = append(args, userID)
	}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		if kind != "credit" && kind != "reset" {
			return validationError("卡片类型无效")
		}
		where += " AND c.kind = ?"
		args = append(args, kind)
	}
	if status := r.URL.Query().Get("status"); status != "" {
		conditions := map[string]string{
			"revoked":   "c.revoked_at IS NOT NULL",
			"used":      "c.revoked_at IS NULL AND c.used_at IS NOT NULL",
			"exhausted": "c.revoked_at IS NULL AND c.used_at IS NULL AND c.kind = 'credit' AND c.amount_usd <= c.used_usd",
			"expired":   "c.revoked_at IS NULL AND c.used_at IS NULL AND (c.kind = 'reset' OR c.amount_usd > c.used_usd) AND julianday(c.expires_at) <= julianday('now')",
			"active":    "c.revoked_at IS NULL AND c.used_at IS NULL AND c.kind = 'credit' AND c.amount_usd > c.used_usd AND (c.expires_at IS NULL OR julianday(c.expires_at) > julianday('now'))",
			"unused":    "c.revoked_at IS NULL AND c.used_at IS NULL AND c.kind = 'reset' AND (c.expires_at IS NULL OR julianday(c.expires_at) > julianday('now'))",
		}
		condition, ok := conditions[status]
		if !ok {
			return validationError("卡片状态无效")
		}
		where += " AND (" + condition + ")"
	}
	var total int
	if err := a.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM quota_cards c WHERE "+where, args...).Scan(&total); err != nil {
		return err
	}
	args = append(args, size, (page-1)*size)
	rows, err := a.db.QueryContext(r.Context(), "SELECT "+quotaCardColumns+" FROM quota_cards c JOIN users u ON u.id = c.user_id WHERE "+where+" ORDER BY c.id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	cards := []QuotaCardResponse{}
	for rows.Next() {
		card, err := scanQuotaCard(rows, time.Now())
		if err != nil {
			return err
		}
		cards = append(cards, card)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": cards, "total": total, "page": page, "page_size": size})
	return nil
}

func quotaPage(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	return clampInt(page, 1, 1000000, 1), clampInt(size, 1, 100, 20)
}

func (a *App) handleAdminQuota(w http.ResponseWriter, r *http.Request) error {
	actor, err := a.adminUser(r.Context(), r)
	if err != nil {
		return err
	}
	parts := splitPath(r.URL.Path, "/api/quota/")
	if len(parts) == 1 && parts[0] == "reset" {
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		var p quotaTargets
		if err := decodeJSON(r, &p); err != nil {
			return err
		}
		count, err := a.resetUserQuotas(r.Context(), actor.ID, p, 0)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"reset": count})
		return nil
	}
	if len(parts) == 1 && parts[0] == "cards" {
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		id := 0
		if raw := r.URL.Query().Get("user_id"); raw != "" {
			id, err = parseIntPath(raw)
			if err != nil || id < 1 {
				return validationError("用户 ID 无效")
			}
		}
		return a.listQuotaCards(w, r, id)
	}
	if len(parts) == 2 && parts[0] == "cards" {
		switch parts[1] {
		case "issue":
			if err := requireMethod(r, http.MethodPost); err != nil {
				return err
			}
			var p quotaCardPayload
			if err := decodeJSON(r, &p); err != nil {
				return err
			}
			result, err := a.issueQuotaCards(r.Context(), actor.ID, p)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, result)
			return nil
		case "revoke":
			if err := requireMethod(r, http.MethodPost); err != nil {
				return err
			}
			var p quotaRevokePayload
			if err := decodeJSON(r, &p); err != nil {
				return err
			}
			count, err := a.revokeQuotaCards(r.Context(), actor.ID, p)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, map[string]any{"revoked": count})
			return nil
		default:
			if err := requireMethod(r, http.MethodPut); err != nil {
				return err
			}
			id, err := parseIntPath(parts[1])
			if err != nil {
				return err
			}
			var p quotaCardPayload
			if err := decodeJSON(r, &p); err != nil {
				return err
			}
			if err := a.editQuotaCard(r.Context(), id, p); err != nil {
				return err
			}
			writeNoContent(w)
			return nil
		}
	}
	return notFoundError("Not Found")
}

func (a *App) editQuotaCard(ctx context.Context, id int, p quotaCardPayload) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID int
	if err := tx.QueryRowContext(ctx, "SELECT user_id FROM quota_cards WHERE id = ?", id).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFoundError("卡片不存在")
		}
		return err
	}
	if err := lockQuotaUsers(ctx, tx, []int{userID}); err != nil {
		return err
	}
	card, err := scanQuotaCard(tx.QueryRowContext(ctx, "SELECT "+quotaCardColumns+" FROM quota_cards c JOIN users u ON u.id = c.user_id WHERE c.id = ?", id), time.Now())
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundError("卡片不存在")
	}
	if err != nil {
		return err
	}
	if card.RevokedAt != nil || card.UsedAt != nil {
		return conflictError("已吊销或已使用的卡片不能编辑")
	}
	p.Kind = card.Kind
	if err := validateQuotaCardPayload(&p); err != nil {
		return err
	}
	if p.AmountUSD < card.UsedUSD {
		return validationError("卡片总额度不能小于已用额度")
	}
	_, err = tx.ExecContext(ctx, "UPDATE quota_cards SET name = ?, amount_usd = ?, expires_at = ?, updated_at = ? WHERE id = ?", p.Name, p.AmountUSD, dbTimePtr(p.ExpiresAt), dbTime(time.Now()), id)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.syncQuotaUsers(ctx, []int{card.UserID})
	return nil
}

type quotaRevokePayload struct {
	quotaTargets
	CardIDs []int  `json:"card_ids"`
	Kind    string `json:"kind"`
}

func (a *App) revokeQuotaCards(ctx context.Context, actorID int, p quotaRevokePayload) (int64, error) {
	if p.Kind != "" && p.Kind != "credit" && p.Kind != "reset" {
		return 0, validationError("卡片类型无效")
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	where := "revoked_at IS NULL AND used_at IS NULL"
	args := []any{}
	if len(p.CardIDs) > 0 {
		if len(p.CardIDs) > 1000 || p.AllUsers || len(p.UserIDs) > 0 {
			return 0, validationError("请选择卡片或用户，不能同时指定")
		}
		where += " AND id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(p.CardIDs)), ",") + ")"
		for _, id := range p.CardIDs {
			if id < 1 {
				return 0, validationError("卡片 ID 无效")
			}
			args = append(args, id)
		}
	} else {
		ids, err := quotaTargetIDs(ctx, tx, p.quotaTargets)
		if err != nil {
			return 0, err
		}
		if len(ids) == 0 {
			return 0, nil
		}
		where += " AND user_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if p.Kind != "" {
		where += " AND kind = ?"
		args = append(args, p.Kind)
	}
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT user_id FROM quota_cards WHERE "+where, args...)
	if err != nil {
		return 0, err
	}
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if err := lockQuotaUsers(ctx, tx, ids); err != nil {
		return 0, err
	}
	now := dbTime(time.Now())
	updateArgs := append([]any{now, actorID, now}, args...)
	result, err := tx.ExecContext(ctx, "UPDATE quota_cards SET revoked_at = ?, revoked_by = ?, updated_at = ? WHERE "+where, updateArgs...)
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
	a.syncQuotaUsers(ctx, ids)
	return count, nil
}

func (a *App) resetUserQuotas(ctx context.Context, actorID int, target quotaTargets, cardID int) (int, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	ids, err := quotaTargetIDs(ctx, tx, target)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	if cardID != 0 {
		if len(ids) != 1 || ids[0] != actorID {
			return 0, forbiddenError("只能使用自己的重置卡")
		}
		card, err := scanQuotaCard(tx.QueryRowContext(ctx, "SELECT "+quotaCardColumns+" FROM quota_cards c JOIN users u ON u.id = c.user_id WHERE c.id = ? AND c.user_id = ?", cardID, actorID), now)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, notFoundError("卡片不存在")
		}
		if err != nil {
			return 0, err
		}
		if card.Kind != "reset" || card.Status != "unused" {
			return 0, conflictError("重置卡不可用或已使用")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE quota_cards SET used_at = ?, updated_at = ? WHERE id = ?", dbTime(now), dbTime(now), cardID); err != nil {
			return 0, err
		}
	}
	for _, id := range ids {
		user, err := scanUser(tx.QueryRowContext(ctx, "SELECT "+userSelectColumns+" FROM users WHERE id = ?", id))
		if err != nil {
			return 0, err
		}
		user = quotaPeriodsAt(user, now)
		var card any
		if cardID != 0 {
			card = cardID
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO quota_resets(user_id, actor_id, card_id, daily_used_usd, weekly_used_usd, quota_day, quota_week, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, actorID, card, user.QuotaDayUsedUSD, user.QuotaWeekUsedUSD, user.QuotaDay, user.QuotaWeek, dbTime(now))
		if err != nil {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE users SET quota_day = ?, quota_week = ?, quota_day_used_usd = 0, quota_week_used_usd = 0, updated_at = ? WHERE id = ?`, user.QuotaDay, user.QuotaWeek, dbTime(now), id)
		if err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	a.syncQuotaUsers(ctx, ids)
	return len(ids), nil
}

func (a *App) handleAccountQuotaByPath(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	parts := splitPath(r.URL.Path, "/api/account/quota/")
	if len(parts) == 1 && r.Method == http.MethodGet {
		switch parts[0] {
		case "cards":
			return a.listQuotaCards(w, r, user.ID)
		case "history":
			return a.quotaHistory(w, r, user.ID)
		case "resets":
			return a.quotaResetHistory(w, r, user.ID)
		}
	}
	if len(parts) == 3 && parts[0] == "cards" && parts[2] == "use" {
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		id, err := parseIntPath(parts[1])
		if err != nil {
			return err
		}
		if _, err := a.resetUserQuotas(r.Context(), user.ID, quotaTargets{UserIDs: []int{user.ID}}, id); err != nil {
			return err
		}
		return a.handleCurrentUserQuotaResponse(w, r, user.ID)
	}
	return notFoundError("Not Found")
}

func (a *App) handleCurrentUserQuotaResponse(w http.ResponseWriter, r *http.Request, userID int) error {
	status, err := a.userQuotaStatus(r.Context(), userID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, status)
	return nil
}

type QuotaChargeResponse struct {
	ID           int                  `json:"id"`
	AmountUSD    float64              `json:"amount_usd"`
	DailyUSD     float64              `json:"daily_usd"`
	WeeklyUSD    float64              `json:"weekly_usd"`
	LimitUSD     float64              `json:"limit_usd"`
	CardsUSD     float64              `json:"cards_usd"`
	LegacyUSD    float64              `json:"legacy_usd"`
	UncoveredUSD float64              `json:"uncovered_usd"`
	Unpriced     bool                 `json:"unpriced"`
	Timestamp    *time.Time           `json:"timestamp"`
	CreatedAt    *time.Time           `json:"created_at"`
	Cards        []QuotaCardDeduction `json:"cards"`
}

type QuotaCardDeduction struct {
	CardID    int     `json:"card_id"`
	Name      string  `json:"name"`
	AmountUSD float64 `json:"amount_usd"`
}

func (a *App) quotaHistory(w http.ResponseWriter, r *http.Request, userID int) error {
	page, size := quotaPage(r)
	var total int
	if err := a.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM user_quota_charges WHERE user_id = ?", userID).Scan(&total); err != nil {
		return err
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id, amount_usd, daily_deducted_usd, weekly_deducted_usd, limit_deducted_usd,
		cards_deducted_usd, monthly_deducted_usd + lifetime_deducted_usd, uncovered_usd, unpriced, CAST(usage_timestamp AS TEXT), CAST(created_at AS TEXT)
		FROM user_quota_charges WHERE user_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, userID, size, (page-1)*size)
	if err != nil {
		return err
	}
	items := []QuotaChargeResponse{}
	for rows.Next() {
		var item QuotaChargeResponse
		var timestamp, created sql.NullString
		if err := rows.Scan(&item.ID, &item.AmountUSD, &item.DailyUSD, &item.WeeklyUSD, &item.LimitUSD, &item.CardsUSD, &item.LegacyUSD, &item.UncoveredUSD, &item.Unpriced, &timestamp, &created); err != nil {
			rows.Close()
			return err
		}
		item.Timestamp, item.CreatedAt, item.Cards = timePtr(timestamp), timePtr(created), []QuotaCardDeduction{}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(items) > 0 {
		rows, err := a.db.QueryContext(r.Context(), `SELECT d.charge_id, d.card_id, c.name, d.amount_usd
			FROM quota_card_deductions d JOIN quota_cards c ON c.id = d.card_id
			WHERE c.user_id = ? AND d.charge_id BETWEEN ? AND ? ORDER BY d.id`, userID, items[len(items)-1].ID, items[0].ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		byID := map[int]int{}
		for i := range items {
			byID[items[i].ID] = i
		}
		for rows.Next() {
			var chargeID int
			var deduction QuotaCardDeduction
			if err := rows.Scan(&chargeID, &deduction.CardID, &deduction.Name, &deduction.AmountUSD); err != nil {
				return err
			}
			if i, ok := byID[chargeID]; ok {
				items[i].Cards = append(items[i].Cards, deduction)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
	return nil
}

func (a *App) quotaResetHistory(w http.ResponseWriter, r *http.Request, userID int) error {
	page, size := quotaPage(r)
	var total int
	if err := a.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM quota_resets WHERE user_id = ?", userID).Scan(&total); err != nil {
		return err
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id, card_id, daily_used_usd, weekly_used_usd, CAST(created_at AS TEXT)
		FROM quota_resets WHERE user_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, userID, size, (page-1)*size)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int
		var card sql.NullInt64
		var daily, weekly float64
		var created sql.NullString
		if err := rows.Scan(&id, &card, &daily, &weekly, &created); err != nil {
			return err
		}
		var cardID any
		if card.Valid {
			cardID = card.Int64
		}
		items = append(items, map[string]any{"id": id, "card_id": cardID, "daily_used_usd": daily, "weekly_used_usd": weekly, "created_at": timePtr(created)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
	return nil
}
