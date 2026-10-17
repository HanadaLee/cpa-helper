package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestUsageBillingFixedCostsAndRollups(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			if engine == "postgres" {
				t.Setenv("CPA_HELPER_DATABASE_URL", isolatedPostgres(t))
			}
			a := newQuotaTestApp(t)
			ctx := context.Background()
			alice := seedQuotaTestUser(t, a, "alice")
			seedQuotaTestUser(t, a, "bob")
			seedQuotaTestUser(t, a, "idle")
			seedQuotaTestPrice(t, a, "openai", "model", 999999)
			start := time.Now().In(appTimeLocation).AddDate(0, 0, -2).Truncate(time.Hour)
			end := start.Add(3 * time.Hour)
			insert := func(name string, minute int, cost float64, failed, unpriced bool) {
				t.Helper()
				_, err := a.db.Exec(`INSERT INTO usage_records (created_at, timestamp, usage_username,
					provider, model, endpoint, failed, total_tokens, cost_usd, unpriced, dedupe_key, raw_json)
					VALUES (?, ?, ?, 'openai', 'model', '/v1/responses', ?, 100, ?, ?, ?, '{}')`,
					dbTime(start), dbTime(start.Add(time.Duration(minute)*time.Minute)), name, failed, cost, unpriced, fmt.Sprintf("%s-%d", name, minute))
				if err != nil {
					t.Fatal(err)
				}
			}
			insert("alice", -1, 100, false, false) // Outside the start boundary.
			insert("alice", 15, 1.25, false, false)
			insert("bob", 70, 2.5, false, false)
			insert("bob", 80, 0, true, true)
			insert("former-user", 90, 0.25, false, false)
			insert("", 100, 0.5, false, false)
			insert("alice", 180, 100, false, false) // End is exclusive.
			filters := UsageFilters{Start: &start, End: &end}
			before, err := a.buildUsageBilling(ctx, filters)
			if err != nil {
				t.Fatal(err)
			}
			if before.Summary.CostUSD != 4.5 || before.Summary.Records != 5 || before.Summary.UnpricedRecords != 1 || before.Summary.TotalTokens != 500 || before.ActiveUsers != 4 {
				t.Fatalf("unexpected totals: %+v", before)
			}
			if len(before.Items) != 5 || len(before.Users) != 3 || before.Items[0].Username != "bob" || before.Items[4].Username != "idle" {
				t.Fatalf("missing zero/orphan users or wrong order: %+v", before)
			}
			assertUsageBillingHTTPResponse(t, a, filters, before)
			if _, err := a.rollupUsageBatch(ctx, usageRollupBatchSize); err != nil {
				t.Fatal(err)
			}
			after, err := a.buildUsageBilling(ctx, filters)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("rollups changed bill: before=%+v after=%+v err=%v", before, after, err)
			}
			assertUsageBillingHTTPResponse(t, a, filters, after)
			// Unaggregated tail must be counted once alongside existing rollups.
			insert("alice", 140, 0.75, false, false)
			tail, err := a.buildUsageBilling(ctx, filters)
			if err != nil || tail.Summary.CostUSD != 5.25 || tail.Summary.Records != 6 {
				t.Fatalf("tail: %+v %v", tail, err)
			}
			assertUsageBillingHTTPResponse(t, a, filters, tail)
			partialStart, partialEnd := start.Add(30*time.Minute), start.Add(150*time.Minute)
			partial, err := a.buildUsageBilling(ctx, UsageFilters{Start: &partialStart, End: &partialEnd})
			if err != nil || partial.Summary.CostUSD != 4 || partial.Summary.Records != 5 {
				t.Fatalf("partial boundaries: %+v %v", partial, err)
			}
			filters.UserID = &alice
			filtered, err := a.buildUsageBilling(ctx, filters)
			if err != nil || len(filtered.Items) != 1 || filtered.Summary.CostUSD != 2 {
				t.Fatalf("user filter: %+v %v", filtered, err)
			}
			assertUsageBillingHTTPResponse(t, a, filters, filtered)
			model, provider, endpoint, failed := "model", "openai", "/v1/responses", true
			filtered, err = a.buildUsageBilling(ctx, UsageFilters{Start: &start, End: &end, Model: &model, Provider: &provider, Endpoint: &endpoint, Failed: &failed})
			if err != nil || filtered.Summary.Records != 1 || filtered.Summary.UnpricedRecords != 1 {
				t.Fatalf("dimension filters: %+v %v", filtered, err)
			}
			provider = "missing"
			empty, err := a.buildUsageBilling(ctx, UsageFilters{Start: &start, End: &end, Provider: &provider})
			if err != nil || empty.Summary.Records != 0 || len(empty.Items) != 3 {
				t.Fatalf("zero users: %+v %v", empty, err)
			}
			if _, err := a.rollupUsageBatch(ctx, usageRollupBatchSize); err != nil {
				t.Fatal(err)
			}
			if _, err := a.db.Exec(`DELETE FROM usage_records`); err != nil {
				t.Fatal(err)
			}
			retained, err := a.buildUsageBilling(ctx, UsageFilters{Start: &start, End: &end})
			if err != nil || !reflect.DeepEqual(tail, retained) {
				t.Fatalf("bill after detail deletion: %+v %v", retained, err)
			}
			assertUsageBillingHTTPResponse(t, a, UsageFilters{Start: &start, End: &end}, retained)
			missing := -1
			if _, err := a.buildUsageBilling(ctx, UsageFilters{Start: &start, End: &end, UserID: &missing}); err == nil {
				t.Fatal("missing user exposed all users")
			}
			if _, err := a.buildUsageBilling(ctx, UsageFilters{Start: &end, End: &start}); err == nil {
				t.Fatal("inverted range accepted")
			}
			if _, err := a.buildUsageBilling(ctx, UsageFilters{Start: &start, End: &start}); err == nil {
				t.Fatal("empty range accepted")
			}
		})
	}
}

// Exercise the actual HTTP writer, not only the in-memory aggregate. Its time
// normalization must preserve the flat JSON contract, including zero and null.
func assertUsageBillingHTTPResponse(t *testing.T, a *App, filters UsageFilters, want usageBillingResponse) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/usage/billing", nil)
	recorder := httptest.NewRecorder()
	if err := a.usageBilling(recorder, request, filters); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("billing HTTP status = %d: %s", recorder.Code, recorder.Body.String())
	}
	expectedJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual map[string]any
	if err := json.Unmarshal(expectedJSON, &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("billing HTTP fields changed or missing:\ngot  %s\nwant %s", recorder.Body.String(), expectedJSON)
	}
}
