package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-helper/backend/internal/platform/database"
)

// The supplied URL is only a maintenance connection. Every test creates its own
// uniquely named database; it never clears or drops the database in the URL.
func isolatedPostgres(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("CPA_HELPER_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set CPA_HELPER_TEST_POSTGRES_URL (test role needs CREATEDB)")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := database.OpenPostgres(dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	suffix := make([]byte, 12)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "cpa_test_" + hex.EncodeToString(suffix)
	if _, err := admin.ExecContext(context.Background(), "CREATE DATABASE "+quoteSQL(name)+" TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE "+quoteSQL(name)+" WITH (FORCE)"); err != nil {
			t.Errorf("cleanup %s: %v", name, err)
		}
	})
	parsed.Path = "/" + name
	return parsed.String()
}

func TestPostgresReadSnapshotAndConcurrentSetup(t *testing.T) {
	t.Setenv("CPA_HELPER_DATABASE_URL", isolatedPostgres(t))
	a := newQuotaTestApp(t)
	ctx := context.Background()
	snapshot, err := a.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Rollback()
	var first string
	if err := snapshot.QueryRowContext(ctx, "SELECT brand_name_en FROM app_settings WHERE id=1").Scan(&first); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE app_settings SET brand_name_en='after-snapshot' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	var second string
	if err := snapshot.QueryRowContext(ctx, "SELECT brand_name_en FROM app_settings WHERE id=1").Scan(&second); err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("read-only transaction has inconsistent snapshots")
	}
	snapshot.Rollback()
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			request := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(fmt.Sprintf(`{"username":"admin-%d","password":"test-password","nickname":"test"}`, i)))
			response := httptest.NewRecorder()
			if err := a.handleSetupFirstAdmin(response, request); err == nil {
				successes.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("created %d first admins", successes.Load())
	}
}

func TestPostgresApplicationParity(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{"quota-expiry", TestQuotaDeductsAllBucketsByExpirationAndDedupes},
		{"quota-concurrent", TestQuotaConcurrentChargesDoNotLoseBalanceUpdates},
		{"quota-reset", TestQuotaResetCardSingleUseOwnershipAndFixedPeriods},
		{"quota-bulk", TestQuotaBulkIssueRevokeAndGlobalReset},
		{"quota-periods", TestQuotaPeriodsAndMondayBoundary},
		{"quota-unpriced", TestQuotaChargesImageAndUnpricedUsage},
		{"quota-unlimited", TestQuotaUnlimitedUsageSkipsCharges},
		{"retention-dedup-cost", TestStoredUsageCostRollupCleanupAndPermanentDedup},
		{"aggregate-boundaries", TestUsageAggregateCombinesPartialHoursRollupAndWatermarkTail},
		{"aggregate-old-boundary", TestUsageAggregateRejectsExpiredPartialHourRange},
		{"tutorials", TestTutorialsCRUDAndPermissions},
		{"pricing", TestModelPriceAPIRoundTripsLongContextRates},
		{"pricing-calendar", TestModelPriceOffPeakRoundTripAndHolidayCalendarAPI},
		{"pricing-local-overrides", TestUpdateAutoSyncedPriceLocalOverridesKeepLiteLLMSync},
		{"pricing-sync", TestSyncLiteLLMPricesReplacesLiteLLMSource},
		{"keeper", TestKeeperRunStoresRemoteAuthIndex},
		{"keeper-enable", TestManualKeeperRefreshReenablesRecoverableUnauthorizedDisabledAccount},
		{"alias-billing", TestSaveUsageMessageUsesAliasForStoredModelAndCost},
		{"fast-billing", TestSaveUsageMessageStoresFastTierAndFixedFastCost},
		{"long-context-billing", TestSaveUsageMessageStoresFixedLongContextCost},
		{"oidc-cas", TestOIDCSharedCASIdentityAndLogout},
		{"oidc-registration", TestOIDCNewUserCanUseCASAndRetainsDefaultQuotas},
		{"oidc-security", TestOIDCRejectsInvalidIdentityAndState},
		{"oidc-settings", TestOIDCSettingsPolicyAndSecretHandling},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CPA_HELPER_DATABASE_URL", isolatedPostgres(t))
			test.run(t)
		})
	}
}

func TestPostgresSQLiteTransfer(t *testing.T) {
	dsn := isolatedPostgres(t)
	t.Setenv("CPA_HELPER_DATABASE_URL", "")
	a := newQuotaTestApp(t)
	ctx := context.Background()
	id := seedQuotaTestUser(t, a, "migration-用户")
	seedQuotaTestAPIKey(t, a, id, "sk-test-migrate")
	seedQuotaTestPrice(t, a, "openai", "migrate-model", 2.12345678)
	daily, weekly := 0.0, 0.0
	if _, err := a.updateUserQuota(ctx, id, userQuotaPayload{DailyQuotaUSD: &daily, WeeklyQuotaUSD: &weekly}); err != nil {
		t.Fatal(err)
	}
	seedQuotaCard(t, a, id, "credit", 100, nil)
	seedQuotaCard(t, a, id, "reset", 0, nil)
	stamp := dbTime(time.Now().AddDate(0, 0, -40).Truncate(time.Hour))
	raw := []byte(fmt.Sprintf(`{"timestamp":%q,"api_key":"sk-test-migrate","model":"migrate-model","input_tokens":1000000,"request_id":"retained"}`, stamp))
	if _, _, err := a.saveUsageMessage(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := a.rollupUsageBatch(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := a.cleanupUsageBatch(ctx, 31, 100); err != nil {
		t.Fatal(err)
	}
	// Cross the import batch boundary with retained, priced details and nullable fields.
	for i := range 205 {
		if _, _, err := a.saveUsageMessage(ctx, []byte(fmt.Sprintf(`{"model":"migrate-model","request_id":"batch-%d","input_tokens":1234,"latency_ms":12.345,"ttft_ms":0.123}`, i))); err != nil {
			t.Fatal(err)
		}
	}
	// A deleted high ID must not be reused after import; watermark/dedup depend on this.
	if _, err := a.db.Exec("UPDATE sqlite_sequence SET seq=9000000001 WHERE name='usage_records'"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("INSERT INTO oidc_identities(issuer,subject,user_id) VALUES ('https://id.example','subject',?)", id); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(a.dataDir, "db", "cpa_helper.sqlite3")
	a.Close()
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CPA_HELPER_DATABASE_URL", dsn)
	var report bytes.Buffer
	if err := TransferSQLite(ctx, source, false, &report); err != nil {
		t.Fatal(err)
	}
	if err := TransferSQLite(ctx, source, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := TransferSQLite(ctx, source, false, io.Discard); err == nil {
		t.Fatal("nonempty target accepted")
	}
	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("source was modified")
	}
	migrated, err := NewWithOptions(ctx, NewOptions{RequireReady: true, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	user, err := migrated.getUser(ctx, id)
	if err != nil || user.QuotaCardsRemainingUSD != 97.87654322 {
		t.Fatalf("balance %+v %v", user, err)
	}
	if _, created, err := migrated.saveUsageMessage(ctx, raw); err != nil || created {
		t.Fatalf("replay after cleanup/import: %v %v", created, err)
	}
	record, created, err := migrated.saveUsageMessage(ctx, []byte(`{"model":"migrate-model","input_tokens":10,"request_id":"after-import"}`))
	if err != nil || !created || record.ID <= 9000000001 {
		t.Fatalf("new usage %d %v %v", record.ID, created, err)
	}
	if err := TransferSQLite(ctx, source, true, io.Discard); err == nil {
		t.Fatal("changed target was not detected")
	}
}

func TestPostgresTransferRollsBackSchemaOnFailure(t *testing.T) {
	dsn := isolatedPostgres(t)
	t.Setenv("CPA_HELPER_DATABASE_URL", "")
	a := newQuotaTestApp(t)
	if _, err := a.db.Exec("ALTER TABLE users ADD COLUMN unexpected TEXT"); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(a.dataDir, "db", "cpa_helper.sqlite3")
	a.Close()
	t.Setenv("CPA_HELPER_DATABASE_URL", dsn)
	if err := TransferSQLite(context.Background(), source, false, io.Discard); err == nil || !strings.Contains(err.Error(), "missing in target") {
		t.Fatalf("want column rejection: %v", err)
	}
	db, err := database.OpenPostgres(dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial schema left: %d %v", count, err)
	}
}

func TestPostgresConcurrentResetAndRollup(t *testing.T) {
	t.Setenv("CPA_HELPER_DATABASE_URL", isolatedPostgres(t))
	a := newQuotaTestApp(t)
	ctx := context.Background()
	id := seedQuotaTestUser(t, a, "parallel-reset")
	quota := 100.0
	if _, err := a.updateUserQuota(ctx, id, userQuotaPayload{DailyQuotaUSD: &quota, WeeklyQuotaUSD: &quota}); err != nil {
		t.Fatal(err)
	}
	reset := seedQuotaCard(t, a, id, "reset", 0, nil)
	var wg sync.WaitGroup
	var succeeded atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.resetUserQuotas(ctx, id, quotaTargets{UserIDs: []int{id}}, reset); err == nil {
				succeeded.Add(1)
			}
		}()
	}
	wg.Wait()
	if succeeded.Load() != 1 {
		t.Fatalf("reset applied %d times", succeeded.Load())
	}
	errs := make(chan error, 65)
	for i := range 64 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := a.saveUsageMessage(ctx, []byte(fmt.Sprintf(`{"model":"parallel","request_id":"r-%d","total_tokens":1}`, i)))
			errs <- err
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 32 {
			if _, err := a.rollupUsageBatch(ctx, 5); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.rollupUsageBatch(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	aggregate, err := a.buildUsageAggregate(ctx, UsageFilters{}, usageAggregateSummary, nil)
	if err != nil || aggregate.summary.records != 64 {
		t.Fatalf("aggregate=%+v err=%v", aggregate, err)
	}
	var rolled int
	if err := a.db.QueryRow("SELECT SUM(record_count) FROM usage_hourly_rollups").Scan(&rolled); err != nil || rolled != 64 {
		t.Fatalf("rollup=%d %v", rolled, err)
	}
}

func TestPostgresPreparedQueriesObserveCardExpiry(t *testing.T) {
	t.Setenv("CPA_HELPER_DATABASE_URL", isolatedPostgres(t))
	a := newQuotaTestApp(t)
	a.db.SetMaxOpenConns(1)
	ctx := context.Background()
	id := seedQuotaTestUser(t, a, "expiring-prepared-query")
	card := seedQuotaCard(t, a, id, "credit", 3, nil)
	expires := time.Now().Add(2 * time.Second)
	if _, err := a.db.Exec("UPDATE quota_cards SET expires_at=? WHERE id=?", dbTime(expires), card); err != nil {
		t.Fatal(err)
	}
	first, err := a.getUser(ctx, id)
	if err != nil || first.QuotaCardsRemainingUSD != 3 {
		t.Fatalf("before expiry: %+v %v", first, err)
	}
	time.Sleep(time.Until(expires) + 100*time.Millisecond)
	// Same SQL on the same physical connection exercises pgx's prepared-statement cache.
	second, err := a.getUser(ctx, id)
	if err != nil || second.QuotaCardsRemainingUSD != 0 {
		t.Fatalf("after expiry: %+v %v", second, err)
	}
}
