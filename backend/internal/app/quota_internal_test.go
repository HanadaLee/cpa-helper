package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func newQuotaTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	a, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func seedQuotaCard(t *testing.T, a *App, userID int, kind string, amount float64, expires *time.Time) int {
	t.Helper()
	now := dbTime(time.Now())
	result, err := a.db.InsertContext(context.Background(), `INSERT INTO quota_cards(user_id, kind, name, amount_usd, expires_at, activated_at, created_at, updated_at)
		VALUES (?, ?, 'test card', ?, ?, ?, ?, ?)`, userID, kind, amount, dbTimePtr(expires), now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}

func TestQuotaDeductsAllBucketsByExpirationAndDedupes(t *testing.T) {
	a := newQuotaTestApp(t)
	ctx := context.Background()
	userID := seedQuotaTestUser(t, a, "expiry-user")
	daily, weekly := 0.5, 0.75
	if _, err := a.updateUserQuota(ctx, userID, userQuotaPayload{DailyQuotaUSD: &daily, WeeklyQuotaUSD: &weekly}); err != nil {
		t.Fatal(err)
	}
	seedQuotaTestAPIKey(t, a, userID, "sk-expiry")
	seedQuotaTestPrice(t, a, "openai", "quota-expiry", 1)
	now := time.Now()
	dayEnd, weekEnd := quotaPeriodEnds(now)
	early := now.Add(dayEnd.Sub(now) / 2)
	late := weekEnd.Add(time.Hour)
	earlyID := seedQuotaCard(t, a, userID, "credit", 0.25, &early)
	lateID := seedQuotaCard(t, a, userID, "credit", 0.75, &late)
	permanentID := seedQuotaCard(t, a, userID, "credit", 1, nil)
	expired := now.Add(-time.Hour)
	expiredID := seedQuotaCard(t, a, userID, "credit", 100, &expired)
	revokedID := seedQuotaCard(t, a, userID, "credit", 100, nil)
	if _, err := a.db.Exec("UPDATE quota_cards SET revoked_at = ? WHERE id = ?", dbTime(now), revokedID); err != nil {
		t.Fatal(err)
	}

	// An earlier-expiring card must be used even though daily credit is still available.
	raw := `{"api_key":"sk-expiry","provider":"openai","model":"quota-expiry","input_tokens":100000,"request_id":"early"}`
	if _, _, err := a.saveUsageMessage(ctx, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	user, _ := a.getUser(ctx, userID)
	if user.QuotaDayUsedUSD != 0 {
		t.Fatalf("daily charged before earlier card: %v", user.QuotaDayUsedUSD)
	}
	raw = `{"api_key":"sk-expiry","provider":"openai","model":"quota-expiry","input_tokens":2400000,"request_id":"rest"}`
	if _, created, err := a.saveUsageMessage(ctx, []byte(raw)); err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if _, created, err := a.saveUsageMessage(ctx, []byte(raw)); err != nil || created {
		t.Fatalf("duplicate created=%v err=%v", created, err)
	}
	user, _ = a.getUser(ctx, userID)
	if user.QuotaDayUsedUSD != 0.5 || user.QuotaWeekUsedUSD != 0.5 || user.QuotaCardsRemainingUSD != 0 {
		t.Fatalf("bad allocation: %+v", user)
	}
	for id, want := range map[int]float64{earlyID: 0.25, lateID: 0.75, permanentID: 1, expiredID: 0, revokedID: 0} {
		var got float64
		if err := a.db.QueryRow("SELECT used_usd FROM quota_cards WHERE id = ?", id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("card %d deducted=%v want=%v", id, got, want)
		}
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM user_quota_charges").Scan(&count); err != nil || count != 2 {
		t.Fatalf("dedup count=%v err=%v", count, err)
	}
	recorder := httptest.NewRecorder()
	if err := a.quotaHistory(recorder, httptest.NewRequest("GET", "/api/account/quota/history", nil), userID); err != nil {
		t.Fatal(err)
	}
	var history struct {
		Items []QuotaChargeResponse `json:"items"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if history.Total != 2 || len(history.Items) != 2 || len(history.Items[0].Cards) != 3 {
		t.Fatalf("missing deduction history: %+v", history)
	}
	for _, charge := range history.Items {
		if charge.DailyUSD != charge.LimitUSD || charge.WeeklyUSD != charge.LimitUSD {
			t.Fatalf("limits did not count the same spend: %+v", charge)
		}
		if math.Abs(charge.AmountUSD-charge.LimitUSD-charge.CardsUSD-charge.UncoveredUSD) > 1e-8 {
			t.Fatalf("charge breakdown does not match cost: %+v", charge)
		}
		cardTotal := 0.0
		for _, card := range charge.Cards {
			cardTotal += card.AmountUSD
		}
		if math.Abs(cardTotal-charge.CardsUSD) > 1e-8 {
			t.Fatalf("card deduction details do not match: %+v", charge)
		}
	}
}

func TestQuotaConcurrentChargesDoNotLoseBalanceUpdates(t *testing.T) {
	a := newQuotaTestApp(t)
	ctx := context.Background()
	userID := seedQuotaTestUser(t, a, "parallel-user")
	daily, weekly := 1.0, 1.0
	if _, err := a.updateUserQuota(ctx, userID, userQuotaPayload{DailyQuotaUSD: &daily, WeeklyQuotaUSD: &weekly}); err != nil {
		t.Fatal(err)
	}
	seedQuotaTestAPIKey(t, a, userID, "sk-parallel")
	seedQuotaTestPrice(t, a, "openai", "quota-parallel", 1)
	cardID := seedQuotaCard(t, a, userID, "credit", 10, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raw := fmt.Sprintf(`{"api_key":"sk-parallel","provider":"openai","model":"quota-parallel","input_tokens":100000,"request_id":"p-%d"}`, i)
			_, _, err := a.saveUsageMessage(ctx, []byte(raw))
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var used float64
	if err := a.db.QueryRow("SELECT used_usd FROM quota_cards WHERE id = ?", cardID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if math.Abs(used-2.2) > 1e-8 {
		t.Fatalf("card used=%v want=2.2", used)
	}
	user, _ := a.getUser(ctx, userID)
	if user.QuotaDayUsedUSD != 1 || user.QuotaWeekUsedUSD != 1 {
		t.Fatalf("lost updates: %+v", user)
	}
}

func TestQuotaResetCardSingleUseOwnershipAndFixedPeriods(t *testing.T) {
	a := newQuotaTestApp(t)
	ctx := context.Background()
	id := seedQuotaTestUser(t, a, "reset-user")
	other := seedQuotaTestUser(t, a, "other-user")
	daily, weekly := 10.0, 20.0
	if _, err := a.updateUserQuota(ctx, id, userQuotaPayload{DailyQuotaUSD: &daily, WeeklyQuotaUSD: &weekly}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE users SET quota_day_used_usd = 3, quota_week_used_usd = 7 WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	before, _ := a.userQuotaStatus(ctx, id)
	card := seedQuotaCard(t, a, id, "reset", 0, nil)
	credit := seedQuotaCard(t, a, id, "credit", 9, nil)
	if _, err := a.resetUserQuotas(ctx, other, quotaTargets{UserIDs: []int{other}}, card); err == nil {
		t.Fatal("other user used reset card")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.resetUserQuotas(ctx, id, quotaTargets{UserIDs: []int{id}}, card)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("reset success count=%v", success)
	}
	after, _ := a.userQuotaStatus(ctx, id)
	if after.DailyUsedUSD != 0 || after.WeeklyUsedUSD != 0 || !after.DailyResetsAt.Equal(before.DailyResetsAt) || !after.WeeklyResetsAt.Equal(before.WeeklyResetsAt) {
		t.Fatalf("reset shifted periods or did not clear usage: %+v", after)
	}
	var used float64
	a.db.QueryRow("SELECT used_usd FROM quota_cards WHERE id = ?", credit).Scan(&used)
	if used != 0 {
		t.Fatal("reset changed credit card")
	}
	var auditDaily, auditWeekly float64
	if err := a.db.QueryRow("SELECT daily_used_usd, weekly_used_usd FROM quota_resets WHERE card_id = ?", card).Scan(&auditDaily, &auditWeekly); err != nil {
		t.Fatal(err)
	}
	if auditDaily != 3 || auditWeekly != 7 {
		t.Fatalf("reset audit=%v/%v", auditDaily, auditWeekly)
	}
	expired := time.Now().Add(-time.Second)
	expiredCard := seedQuotaCard(t, a, id, "reset", 0, &expired)
	if _, err := a.resetUserQuotas(ctx, id, quotaTargets{UserIDs: []int{id}}, expiredCard); err == nil {
		t.Fatal("expired reset used")
	}
}

func TestQuotaBulkIssueRevokeAndGlobalReset(t *testing.T) {
	a := newQuotaTestApp(t)
	ctx := context.Background()
	id := seedQuotaTestUser(t, a, "bulk1")
	id2 := seedQuotaTestUser(t, a, "bulk2")
	for _, uid := range []int{id, id2} {
		daily, weekly := 1.0, 2.0
		if _, err := a.updateUserQuota(ctx, uid, userQuotaPayload{DailyQuotaUSD: &daily, WeeklyQuotaUSD: &weekly}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.db.Exec("UPDATE users SET quota_day_used_usd = 1, quota_week_used_usd = 2 WHERE id = ?", uid); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.issueQuotaCards(ctx, id, quotaCardPayload{quotaTargets: quotaTargets{UserIDs: []int{id, id2}}, Kind: "credit", AmountUSD: 5, Count: 2})
	if err != nil || result["issued"] != 4 {
		t.Fatalf("bulk=%v err=%v", result, err)
	}
	if _, err := a.issueQuotaCards(ctx, id, quotaCardPayload{quotaTargets: quotaTargets{UserIDs: []int{id, 99999}}, Kind: "credit", AmountUSD: 5}); err == nil {
		t.Fatal("unknown user accepted")
	}
	var count int
	a.db.QueryRow("SELECT COUNT(*) FROM quota_cards").Scan(&count)
	if count != 4 {
		t.Fatal("partial issue persisted")
	}
	revoked, err := a.revokeQuotaCards(ctx, id, quotaRevokePayload{quotaTargets: quotaTargets{UserIDs: []int{id}}, Kind: "credit"})
	if err != nil || revoked != 2 {
		t.Fatalf("revoke=%v err=%v", revoked, err)
	}
	status, _ := a.userQuotaStatus(ctx, id)
	if !status.Paused || status.CardsRemainingUSD != 0 {
		t.Fatalf("revoked balance still available: %+v", status)
	}
	status, _ = a.userQuotaStatus(ctx, id2)
	if status.Paused || status.CardsRemainingUSD != 10 {
		t.Fatalf("other user affected: %+v", status)
	}
	reset, err := a.resetUserQuotas(ctx, id, quotaTargets{AllUsers: true}, 0)
	if err != nil || reset != 2 {
		t.Fatalf("reset=%v err=%v", reset, err)
	}
	status, _ = a.userQuotaStatus(ctx, id)
	if status.Paused || status.AvailableUSD != 1 {
		t.Fatalf("global reset failed: %+v", status)
	}
}

func TestQuotaPeriodsAndMondayBoundary(t *testing.T) {
	before := time.Date(2026, 1, 4, 23, 59, 59, 0, appTimeLocation)
	day, week := quotaPeriodEnds(before)
	want := time.Date(2026, 1, 5, 0, 0, 0, 0, appTimeLocation)
	if !day.Equal(want) || !week.Equal(want) || quotaWeek(before) != "2026-W01" {
		t.Fatalf("before Monday day=%v week=%v", day, week)
	}
	_, next := quotaPeriodEnds(want)
	if !next.Equal(want.AddDate(0, 0, 7)) || quotaWeek(want) != "2026-W02" {
		t.Fatal("Monday did not start fixed next week")
	}
	a := newQuotaTestApp(t)
	ctx := context.Background()
	id := seedQuotaTestUser(t, a, "period-user")
	daily, weekly := 1.0, 2.0
	if _, err := a.updateUserQuota(ctx, id, userQuotaPayload{DailyQuotaUSD: &daily, WeeklyQuotaUSD: &weekly}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE users SET quota_day = '2000-01-01', quota_day_used_usd = 1, quota_week_used_usd = 1 WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	status, err := a.userQuotaStatus(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if status.DailyUsedUSD != 0 || status.WeeklyUsedUSD != 1 {
		t.Fatalf("daily reset changed week: %+v", status)
	}
	if _, err := a.db.Exec("UPDATE users SET quota_week = '2000-W01' WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	status, err = a.userQuotaStatus(ctx, id)
	if err != nil || status.WeeklyUsedUSD != 0 {
		t.Fatalf("weekly reset=%+v %v", status, err)
	}
}

func TestQuotaChargesImageAndUnpricedUsage(t *testing.T) {
	a := newQuotaTestApp(t)
	ctx := context.Background()
	id := seedQuotaTestUser(t, a, "image-user")
	daily, weekly := 1.0, 2.0
	if _, err := a.updateUserQuota(ctx, id, userQuotaPayload{DailyQuotaUSD: &daily, WeeklyQuotaUSD: &weekly}); err != nil {
		t.Fatal(err)
	}
	seedQuotaTestAPIKey(t, a, id, "sk-image")
	seedQuotaTestRequestPrice(t, a, "openai", "quota-image", 1.25)
	for _, raw := range []string{`{"api_key":"sk-image","provider":"openai","model":"quota-image","request_id":"img"}`, `{"api_key":"sk-image","provider":"unknown","model":"unknown-model","input_tokens":1000,"request_id":"unpriced"}`} {
		if _, _, err := a.saveUsageMessage(ctx, []byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	user, _ := a.getUser(ctx, id)
	if user.QuotaDayUsedUSD != 1 || user.QuotaWeekUsedUSD != 1 || user.QuotaUnpricedRecords != 1 {
		t.Fatalf("image/unpriced charge=%+v", user)
	}
}

func TestQuotaUnlimitedUsageSkipsCharges(t *testing.T) {
	a := newQuotaTestApp(t)
	ctx := context.Background()
	id := seedQuotaTestUser(t, a, "unlimited")
	seedQuotaTestAPIKey(t, a, id, "sk-unlimited")
	seedQuotaTestPrice(t, a, "openai", "unlimited-model", 1)
	if _, _, err := a.saveUsageMessage(ctx, []byte(`{"api_key":"sk-unlimited","provider":"openai","model":"unlimited-model","input_tokens":1000000}`)); err != nil {
		t.Fatal(err)
	}
	var count int
	a.db.QueryRow("SELECT COUNT(*) FROM user_quota_charges").Scan(&count)
	if count != 0 {
		t.Fatal("unlimited user was charged")
	}
	status, err := a.userQuotaStatus(ctx, id)
	if err != nil || !status.Unlimited || !status.CanCreateKeys {
		t.Fatalf("unlimited status=%+v %v", status, err)
	}
}

func seedQuotaTestUser(t *testing.T, app *App, username string) int {
	t.Helper()
	now := dbTime(time.Now())
	result, err := app.db.InsertContext(context.Background(), `
		INSERT INTO users (username, is_admin, nickname, created_at, updated_at)
		VALUES (?, 0, ?, ?, ?)
	`, username, username, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}

func seedQuotaTestAPIKey(t *testing.T, app *App, userID int, apiKey string) {
	t.Helper()
	now := dbTime(time.Now())
	if _, err := app.db.Exec(`
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
		VALUES (?, ?, ?, 'VSCode', ?, ?)
	`, hashAPIKey(apiKey), userID, apiKey, now, now); err != nil {
		t.Fatal(err)
	}
}

func seedQuotaTestPrice(t *testing.T, app *App, provider, model string, inputUSDPerMillion float64) {
	t.Helper()
	if _, err := app.db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million, updated_at
		) VALUES (?, ?, ?, 0, 0, 0, ?)
	`, provider, model, inputUSDPerMillion, dbTime(time.Now())); err != nil {
		t.Fatal(err)
	}
}

func seedQuotaTestRequestPrice(t *testing.T, app *App, provider, model string, requestUSD float64) {
	t.Helper()
	if _, err := app.db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million, request_usd, updated_at
		) VALUES (?, ?, 0, 0, 0, 0, ?, ?)
	`, provider, model, requestUSD, dbTime(time.Now())); err != nil {
		t.Fatal(err)
	}
}
