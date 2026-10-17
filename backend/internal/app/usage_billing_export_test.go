package app

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestUsageBillingExportMatchesDashboard(t *testing.T) {
	a := newQuotaTestApp(t)
	handler := a.Routes()
	admin := requestJSONForPricingTest(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)
	start := time.Now().In(appTimeLocation).AddDate(0, 0, -2).Truncate(time.Hour)
	end := start.Add(3 * time.Hour)
	for i := range 55 {
		seedQuotaTestUser(t, a, fmt.Sprintf("user-%02d", i))
	}
	var selected int
	if err := a.db.QueryRow("SELECT id FROM users WHERE username='user-00'").Scan(&selected); err != nil {
		t.Fatal(err)
	}
	// User-editable text must remain text, never an executable spreadsheet formula.
	nickname := `=HYPERLINK("https://example.com", "用户")`
	if _, err := a.db.Exec("UPDATE users SET nickname=? WHERE id=?", nickname, selected); err != nil {
		t.Fatal(err)
	}
	seedQuotaTestPrice(t, a, "openai", "test-model", 999999)
	insert := func(name string, offset int, cost float64, failed, unpriced bool) {
		t.Helper()
		_, err := a.db.Exec(`INSERT INTO usage_records (created_at, timestamp, usage_username,
			provider, model, endpoint, failed, total_tokens, cost_usd, unpriced, dedupe_key, raw_json)
			VALUES (?, ?, ?, 'openai', 'test-model', '/v1/responses', ?, 1234567, ?, ?, ?, '{}')`,
			dbTime(start), dbTime(start.Add(time.Duration(offset)*time.Minute)), name, failed, cost, unpriced, fmt.Sprintf("%s-%d", name, offset))
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("user-00", -1, 999, false, false)
	insert("user-00", 15, 12.34567891, false, false)
	insert("user-00", 20, 0, true, true)
	insert("former-user", 25, 0.00000013, false, false)
	insert("", 30, 1, false, false)
	insert("user-00", 180, 999, false, false)
	if _, err := a.rollupUsageBatch(context.Background(), usageRollupBatchSize); err != nil {
		t.Fatal(err)
	}
	insert("user-01", 75, 2.5, false, false) // Unaggregated tail.

	for _, tc := range []struct{ name, language, extra string }{
		{"all", "zh", ""},
		{"filtered", "en", fmt.Sprintf("&user_id=%d&provider=openai&model=test-model&endpoint=%%2Fv1%%2Fresponses&failed=false", selected)},
		{"zero-cost", "zh", "&failed=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := "?scope=admin&start=" + url.QueryEscape(usageAPITime(start)) + "&end=" + url.QueryEscape(usageAPITime(end)) + tc.extra
			var bill usageBillingResponse
			requestJSONForPricingTest(t, handler, http.MethodGet, "/api/usage/billing"+query, nil, admin, &bill)
			request := httptest.NewRequest(http.MethodGet, "/api/usage/billing/export"+query+"&lang="+tc.language+"&page=2&page_size=1", nil)
			for _, cookie := range admin {
				request.AddCookie(cookie)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != billingWorkbookContentType {
				t.Fatalf("export status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			media, params, err := mime.ParseMediaType(recorder.Header().Get("Content-Disposition"))
			if err != nil || media != "attachment" || !strings.HasSuffix(params["filename"], ".xlsx") || recorder.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("export download headers=%v err=%v", recorder.Header(), err)
			}
			book, err := excelize.OpenReader(recorder.Body)
			if err != nil {
				t.Fatal(err)
			}
			defer book.Close()
			sheet := book.GetSheetList()[0]
			if (tc.language == "en" && sheet != "Cost Accounting") || (tc.language == "zh" && sheet != "费用核算") {
				t.Fatalf("unexpected localized sheet name: %s", sheet)
			}
			get := func(cell string) string {
				t.Helper()
				value, err := book.GetCellValue(sheet, cell, excelize.Options{RawCellValue: true})
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			assertNumber := func(cell string, want float64) {
				t.Helper()
				got, err := strconv.ParseFloat(get(cell), 64)
				if err != nil || got != want {
					t.Fatalf("%s numeric value=%v err=%v want=%v", cell, got, err, want)
				}
			}
			assertNumber("B5", bill.Summary.CostUSD)
			assertNumber("D5", float64(bill.Summary.Records))
			assertNumber("F5", float64(bill.Summary.TotalTokens))
			assertNumber("H5", float64(len(bill.Items)))
			if !strings.Contains(get("B2"), start.Format("2006-01-02 15:04:05")) || !strings.Contains(get("B2"), end.Format("2006-01-02 15:04:05")) {
				t.Fatalf("wrong export period: %s", get("B2"))
			}
			if tc.name == "all" && len(bill.Items) != 58 {
				t.Fatalf("missing zero-usage/orphan users or export paginated: %d", len(bill.Items))
			}
			if tc.name == "filtered" && (len(bill.Items) != 1 || !strings.Contains(get("B3"), "test-model") || !strings.Contains(get("B3"), "Success")) {
				t.Fatalf("filters missing: %s, %d items", get("B3"), len(bill.Items))
			}
			for i, item := range bill.Items {
				row := i + 9
				if get(fmt.Sprintf("B%d", row)) != item.Username {
					t.Fatalf("row %d has wrong username", row)
				}
				label := get(fmt.Sprintf("A%d", row))
				if label == "" || (item.Label != "" && label != item.Label) {
					t.Fatalf("row %d has wrong label: %q", row, label)
				}
				formula, err := book.GetCellFormula(sheet, fmt.Sprintf("A%d", row))
				if err != nil || formula != "" {
					t.Fatalf("nickname was interpreted as formula: %q %v", formula, err)
				}
				share := 0.0
				if bill.Summary.CostUSD > 0 {
					share = item.CostUSD / bill.Summary.CostUSD
				}
				for column, value := range map[string]float64{
					"C": float64(item.Records), "D": float64(item.FailedRecords), "E": float64(item.TotalTokens),
					"F": item.CostUSD, "G": share, "H": float64(item.UnpricedRecords),
				} {
					assertNumber(fmt.Sprintf("%s%d", column, row), value)
				}
			}
			last := len(bill.Items) + 9
			assertNumber(fmt.Sprintf("F%d", last), bill.Summary.CostUSD)
			assertNumber(fmt.Sprintf("C%d", last), float64(bill.Summary.Records))
			assertNumber(fmt.Sprintf("H%d", last), float64(bill.Summary.UnpricedRecords))
			rows, err := book.GetRows(sheet)
			if err != nil || len(rows) != last {
				t.Fatalf("unexpected row count: %d want %d err=%v", len(rows), last, err)
			}
		})
	}
}

func TestBillingWorkbookEmpty(t *testing.T) {
	for _, english := range []bool{false, true} {
		bill := usageBillingResponse{Start: "2026-10-16T00:00:00+08:00", End: "2026-10-17T00:00:00+08:00"}
		buffer, err := billingWorkbook(bill, UsageFilters{}, english, time.Date(2026, 10, 17, 1, 2, 3, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		book, err := excelize.OpenReader(buffer)
		if err != nil {
			t.Fatal(err)
		}
		defer book.Close()
		sheet := book.GetSheetList()[0]
		rows, err := book.GetRows(sheet)
		if err != nil || len(rows) != 9 {
			t.Fatalf("empty workbook rows=%d err=%v", len(rows), err)
		}
		value, _ := book.GetCellValue(sheet, "B4")
		if value != "2026-10-17 09:02:03" {
			t.Fatalf("export time not in Beijing: %s", value)
		}
		share, _ := book.GetCellValue(sheet, "G9", excelize.Options{RawCellValue: true})
		if share != "0" {
			t.Fatalf("empty bill share=%s", share)
		}
	}
}
