package app

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestQuotaCardProgressMonthStart(t *testing.T) {
	for _, tc := range []struct{ now, want string }{
		{"2026-09-30T15:59:59Z", "2026-09-01T00:00:00+08:00"},
		{"2026-09-30T16:00:00Z", "2026-10-01T00:00:00+08:00"},
		{"2026-12-31T16:00:00Z", "2027-01-01T00:00:00+08:00"},
		{"2028-02-29T16:00:00Z", "2028-03-01T00:00:00+08:00"},
	} {
		now, err := time.Parse(time.RFC3339, tc.now)
		if err != nil {
			t.Fatal(err)
		}
		if got := quotaCardProgressMonthStart(now); got != tc.want {
			t.Errorf("month boundary at %s = %s, want %s", tc.now, got, tc.want)
		}
	}
}

func TestQuotaCardProgressExcludesExpiredAndOldDepletedPermanentCards(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			if engine == "postgres" {
				t.Setenv("CPA_HELPER_DATABASE_URL", isolatedPostgres(t))
			} else {
				t.Setenv("CPA_HELPER_DATABASE_URL", "")
			}
			a := newQuotaTestApp(t)
			ctx := context.Background()
			now := time.Now()
			monthStart, ok := parseDBTime(quotaCardProgressMonthStart(now))
			if !ok {
				t.Fatal("invalid month start")
			}
			beforeMonth := monthStart.Add(-time.Second)
			expired, future := now.Add(-time.Hour), now.Add(24*time.Hour)
			cases := []struct {
				name                        string
				used                        float64
				expires                     *time.Time
				deductions                  []time.Time
				updated                     time.Time
				revoked, reset, unactivated bool
				wantTotal, wantRemaining    float64
			}{
				{name: "active dated", used: 4, expires: &future, wantTotal: 10, wantRemaining: 6},
				{name: "expired with balance", used: 4, expires: &expired},
				{name: "expired and depleted", used: 10, expires: &expired},
				{name: "permanent with old partial usage", used: 4, deductions: []time.Time{beforeMonth}, wantTotal: 10, wantRemaining: 6},
				{name: "permanent depleted before month", used: 10, deductions: []time.Time{beforeMonth}},
				{name: "permanent depleted exactly at Beijing month start", used: 10, deductions: []time.Time{monthStart}, wantTotal: 10},
				{name: "permanent last deduction in current month", used: 10, deductions: []time.Time{beforeMonth, monthStart}, wantTotal: 10},
				{name: "dated depleted before month but not expired", used: 10, expires: &future, deductions: []time.Time{beforeMonth}, wantTotal: 10},
				{name: "legacy depleted before month without ledger", used: 10, updated: beforeMonth},
				{name: "legacy depleted this month without ledger", used: 10, updated: monthStart, wantTotal: 10},
				{name: "revoked", used: 4, revoked: true},
				{name: "reset card", reset: true},
				{name: "unactivated credit", unactivated: true},
			}
			userIDs := make(map[int]int)
			for index, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					username := fmt.Sprintf("progress-%d", index)
					userID := seedQuotaTestUser(t, a, username)
					userIDs[userID] = index
					kind := "credit"
					if tc.reset {
						kind = "reset"
					}
					cardID := seedQuotaCard(t, a, userID, kind, 10, tc.expires)
					updated := tc.updated
					if updated.IsZero() {
						updated = now // Later metadata changes must not bring back an old depleted card.
					}
					var revoked any
					var activated any = dbTime(beforeMonth)
					if tc.revoked {
						revoked = dbTime(now)
					}
					if tc.unactivated {
						activated = nil
					}
					if _, err := a.db.Exec(`UPDATE quota_cards SET used_usd = ?, updated_at = ?, revoked_at = ?, activated_at = ? WHERE id = ?`,
						tc.used, dbTime(updated), revoked, activated, cardID); err != nil {
						t.Fatal(err)
					}
					for i, at := range tc.deductions {
						amount := tc.used / float64(len(tc.deductions))
						result, err := a.db.InsertContext(ctx, `INSERT INTO user_quota_charges
							(usage_dedupe_key, usage_timestamp, user_id, usage_username, amount_usd, cards_deducted_usd, quota_month, created_at)
							VALUES (?, ?, ?, ?, ?, ?, '', ?)`, fmt.Sprintf("progress-%d-%d", cardID, i), dbTime(at), userID, username, amount, amount, dbTime(at))
						if err != nil {
							t.Fatal(err)
						}
						chargeID, err := result.LastInsertId()
						if err != nil {
							t.Fatal(err)
						}
						// Store UTC to ensure comparisons use instants, not timestamp text prefixes.
						if _, err := a.db.Exec(`INSERT INTO quota_card_deductions (card_id, charge_id, amount_usd, created_at) VALUES (?, ?, ?, ?)`,
							cardID, chargeID, amount, at.UTC().Format(time.RFC3339)); err != nil {
							t.Fatal(err)
						}
					}
					status, err := a.userQuotaStatus(ctx, userID)
					if err != nil {
						t.Fatal(err)
					}
					if status.CardsTotalUSD != tc.wantTotal || status.CardsRemainingUSD != tc.wantRemaining || status.AvailableUSD != tc.wantRemaining {
						t.Fatalf("unexpected quota progress: %+v, want total=%g remaining=%g", status, tc.wantTotal, tc.wantRemaining)
					}
					user, err := a.userByUsername(ctx, username)
					if err != nil || user.QuotaCardsTotalUSD != tc.wantTotal {
						t.Fatalf("username lookup differs: %+v %v", user, err)
					}
					// Adjusting the cutoff simulates next month's first read without a cleanup job.
					nextMonth := monthStart.AddDate(0, 1, 0)
					user, err = scanUser(a.db.QueryRowContext(ctx, "SELECT "+userSelectColumns+" FROM users WHERE id = ?", dbTime(nextMonth), userID))
					wantNext := tc.wantTotal
					if tc.expires == nil && tc.used == 10 {
						wantNext = 0
					}
					if err != nil || user.QuotaCardsTotalUSD != wantNext || user.QuotaCardsRemainingUSD != tc.wantRemaining {
						t.Fatalf("next month: %+v %v, want total=%g", user, err, wantNext)
					}
					var used float64
					if err := a.db.QueryRow("SELECT used_usd FROM quota_cards WHERE id = ?", cardID).Scan(&used); err != nil || used != tc.used {
						t.Fatalf("card was removed or modified: used=%g err=%v", used, err)
					}
					var deductions int
					if err := a.db.QueryRow("SELECT COUNT(*) FROM quota_card_deductions WHERE card_id = ?", cardID).Scan(&deductions); err != nil || deductions != len(tc.deductions) {
						t.Fatalf("deduction history changed: count=%d err=%v", deductions, err)
					}
				})
			}
			// The admin user table must use the same totals as the current user's quota.
			users, err := a.listUsers(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, user := range users {
				index, exists := userIDs[user.ID]
				if !exists {
					continue
				}
				tc := cases[index]
				if user.Quota.CardsTotalUSD != tc.wantTotal || user.Quota.CardsRemainingUSD != tc.wantRemaining {
					t.Errorf("user table disagrees for %s: %+v", tc.name, user.Quota)
				}
				delete(userIDs, user.ID)
			}
			if len(userIDs) != 0 {
				t.Fatalf("users missing from table: %v", userIDs)
			}
		})
	}
}
