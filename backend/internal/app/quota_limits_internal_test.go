package app

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestQuotaPeriodSwitchesAreIndependent(t *testing.T) {
	amount, zero := 5.0, 0.0
	for _, tc := range []struct {
		name          string
		daily, weekly *float64
		available     float64
		unlimited     bool
	}{
		{"weekly_only", nil, &amount, amount, false},
		{"daily_only", &amount, nil, amount, false},
		{"weekly_zero", nil, &zero, 0, false},
		{"daily_zero", &zero, nil, 0, false},
		{"both_off", nil, nil, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newQuotaTestApp(t)
			ctx := context.Background()
			id := seedQuotaTestUser(t, a, "switch-user-"+tc.name)
			before, err := a.updateUserQuota(ctx, id, userQuotaPayload{DailyQuotaUSD: tc.daily, WeeklyQuotaUSD: tc.weekly})
			if err != nil || before.Unlimited != tc.unlimited || before.LimitsRemainingUSD != tc.available || before.AvailableUSD != tc.available || before.CanCreateKeys != (tc.unlimited || tc.available > 0) {
				t.Fatalf("incorrect independent limit: %+v %v", before, err)
			}
			if (before.DailyQuotaUSD == nil) != (tc.daily == nil) || (before.WeeklyQuotaUSD == nil) != (tc.weekly == nil) {
				t.Fatal("disabled period was coerced to zero")
			}
			seedQuotaCard(t, a, id, "credit", 2, nil)
			after, err := a.userQuotaStatus(ctx, id)
			if err != nil || after.Paused || !after.CanCreateKeys || after.AvailableUSD != tc.available+2 {
				t.Fatalf("card not usable with independent limits: %+v %v", after, err)
			}
		})
	}
}

func TestQuotaOnlyEnabledPeriodControlsExpiry(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, appTimeLocation)
	dayEnd, weekEnd := quotaPeriodEnds(now)
	amount := 5.0
	for _, tc := range []struct {
		user   UserRecord
		expiry time.Time
	}{
		{UserRecord{QuotaDailyUSD: &amount}, dayEnd},
		{UserRecord{QuotaWeeklyUSD: &amount}, weekEnd},
		{UserRecord{QuotaDailyUSD: &amount, QuotaWeeklyUSD: &amount}, dayEnd},
	} {
		if actual := quotaLimitExpiry(tc.user, now); !actual.Equal(tc.expiry) {
			t.Fatalf("period expires %v, want %v", actual, tc.expiry)
		}
	}
}

func TestQuotaWeeklyOnlyBillsByExpiryAndSwitchingDoesNotResetUsage(t *testing.T) {
	a := newQuotaTestApp(t)
	ctx := context.Background()
	id := seedQuotaTestUser(t, a, "weekly-only-user")
	weekly := 5.0
	if _, err := a.updateUserQuota(ctx, id, userQuotaPayload{WeeklyQuotaUSD: &weekly}); err != nil {
		t.Fatal(err)
	}
	seedQuotaTestAPIKey(t, a, id, "sk-weekly-only")
	seedQuotaTestPrice(t, a, "openai", "weekly-only", 1)
	now := time.Now()
	_, weekEnd := quotaPeriodEnds(now)
	expiry := now.Add(weekEnd.Sub(now) / 2)
	cardID := seedQuotaCard(t, a, id, "credit", 1, &expiry)
	for i, tokens := range []int{500000, 1000000} {
		raw := fmt.Sprintf(`{"api_key":"sk-weekly-only","model":"weekly-only","input_tokens":%d,"request_id":"request-%d"}`, tokens, i)
		if _, _, err := a.saveUsageMessage(ctx, []byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	var used float64
	if err := a.db.QueryRow("SELECT used_usd FROM quota_cards WHERE id=?", cardID).Scan(&used); err != nil || used != 1 {
		t.Fatalf("earlier card used=%v err=%v", used, err)
	}
	before, err := a.userQuotaStatus(ctx, id)
	if err != nil || before.DailyQuotaUSD != nil || before.WeeklyUsedUSD != 0.5 || before.AvailableUSD != 4.5 {
		t.Fatalf("weekly-only billing: %+v %v", before, err)
	}
	daily := 2.0
	if _, err := a.updateUserQuota(ctx, id, userQuotaPayload{DailyQuotaUSD: &daily, WeeklyQuotaUSD: &weekly}); err != nil {
		t.Fatal(err)
	}
	after, err := a.updateUserQuota(ctx, id, userQuotaPayload{WeeklyQuotaUSD: &weekly})
	if err != nil || after.WeeklyUsedUSD != before.WeeklyUsedUSD || after.DailyUsedUSD != before.DailyUsedUSD || after.QuotaDay != before.QuotaDay || after.QuotaWeek != before.QuotaWeek {
		t.Fatalf("switch reset counters or periods: %+v %v", after, err)
	}
	if _, err := a.db.Exec("UPDATE users SET quota_day='2000-01-01' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	after, err = a.userQuotaStatus(ctx, id)
	if err != nil || after.WeeklyUsedUSD != 0.5 || after.AvailableUSD != 4.5 {
		t.Fatalf("daily rollover changed weekly availability: %+v %v", after, err)
	}
}

func TestQuotaLimitsAreJointAndCardsRemainUsable(t *testing.T) {
	for _, pair := range [][2]float64{{1, 5}, {5, 1}, {0, 5}, {5, 0}} {
		t.Run(fmt.Sprintf("daily_%g_weekly_%g", pair[0], pair[1]), func(t *testing.T) {
			a := newQuotaTestApp(t)
			ctx := context.Background()
			id := seedQuotaTestUser(t, a, "joint-user")
			if _, err := a.updateUserQuota(ctx, id, userQuotaPayload{DailyQuotaUSD: &pair[0], WeeklyQuotaUSD: &pair[1]}); err != nil {
				t.Fatal(err)
			}
			seedQuotaTestAPIKey(t, a, id, "sk-joint")
			seedQuotaTestPrice(t, a, "openai", "joint", 1)
			limit := minQuotaAmount(pair[0], pair[1])
			before, err := a.userQuotaStatus(ctx, id)
			if err != nil || before.AvailableUSD != limit || before.LimitsRemainingUSD != limit {
				t.Fatalf("limits summed instead of intersected: %+v %v", before, err)
			}
			if _, _, err := a.saveUsageMessage(ctx, []byte(`{"api_key":"sk-joint","model":"joint","input_tokens":1200000,"request_id":"first"}`)); err != nil {
				t.Fatal(err)
			}
			status, err := a.userQuotaStatus(ctx, id)
			if err != nil || !status.Paused || status.DailyUsedUSD != limit || status.WeeklyUsedUSD != limit || status.CanCreateKeys {
				t.Fatalf("exhausted limit was bypassed: %+v %v", status, err)
			}
			if pair[1] == 1 {
				if _, err := a.db.Exec("UPDATE users SET quota_day = '2000-01-01' WHERE id = ?", id); err != nil {
					t.Fatal(err)
				}
				status, err = a.userQuotaStatus(ctx, id)
				if err != nil || !status.Paused || status.DailyUsedUSD != 0 || status.WeeklyUsedUSD != 1 {
					t.Fatalf("new day bypassed weekly limit: %+v %v", status, err)
				}
			}
			previousDay, previousWeek := status.DailyUsedUSD, status.WeeklyUsedUSD
			seedQuotaCard(t, a, id, "credit", 2, nil)
			// No keys/config in this isolated test: test availability independent of remote restoration.
			if _, err := a.db.Exec("DELETE FROM user_api_keys WHERE user_id = ?", id); err != nil {
				t.Fatal(err)
			}
			status, err = a.userQuotaStatus(ctx, id)
			if err != nil || status.Paused || status.AvailableUSD != 2 || status.CardsTotalUSD != 2 {
				t.Fatalf("card did not bypass exhausted limit: %+v %v", status, err)
			}
			seedQuotaTestAPIKey(t, a, id, "sk-joint")
			if _, _, err := a.saveUsageMessage(ctx, []byte(`{"api_key":"sk-joint","model":"joint","input_tokens":300000,"request_id":"card"}`)); err != nil {
				t.Fatal(err)
			}
			status, err = a.userQuotaStatus(ctx, id)
			if err != nil || status.DailyUsedUSD != previousDay || status.WeeklyUsedUSD != previousWeek || status.CardsRemainingUSD != 1.7 {
				t.Fatalf("card spend consumed limit counters: %+v %v", status, err)
			}
		})
	}
}
