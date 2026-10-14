package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"cpa-helper/backend/internal/platform/database"
	backendMigrations "cpa-helper/backend/migrations"
	"github.com/pressly/goose/v3"
)

func TestTutorialsCRUDAndPermissions(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	handler := a.Routes()
	request := func(method, path string, body any, cookies []*http.Cookie, expected int) {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != expected {
			t.Fatalf("%s %s = %d, want %d: %s", method, path, w.Code, expected, w.Body.String())
		}
	}
	admin := requestJSONForPricingTest(t, handler, "POST", "/api/auth/setup", map[string]any{"username": "admin", "password": "test-password", "nickname": "Admin"}, nil, nil)
	requestJSONForPricingTest(t, handler, "POST", "/api/users", map[string]any{"username": "reader", "password": "test-password", "nickname": "Reader"}, admin, nil)
	reader := requestJSONForPricingTest(t, handler, "POST", "/api/auth/login", map[string]any{"username": "reader", "password": "test-password"}, nil, nil)
	var initial []Tutorial
	requestJSONForPricingTest(t, handler, "GET", "/api/tutorials", nil, reader, &initial)
	if len(initial) != 3 {
		t.Fatalf("seed count = %d", len(initial))
	}
	for i, item := range initial {
		if !item.Published || !strings.Contains(item.Markdown, "{{api_key}}") || !strings.Contains(item.MarkdownEN, "{{api_base_url}}") {
			t.Fatalf("invalid seed: %+v", item)
		}
		if want := []string{"Codex CLI Windows", "Codex CLI macOS", "Codex CLI Linux"}[i]; item.Title != want || item.TitleEN != want {
			t.Fatalf("seed titles = %q / %q, want %q", item.Title, item.TitleEN, want)
		}
	}
	var response []map[string]any
	requestJSONForPricingTest(t, handler, "GET", "/api/tutorials", nil, reader, &response)
	for _, item := range response {
		for _, field := range []string{"category", "client", "platform"} {
			if _, exists := item[field]; exists {
				t.Fatalf("removed field %q still present in API", field)
			}
		}
	}
	payload := Tutorial{Title: "Codex Windows Desktop", Markdown: "# Draft\n{{api_key}}", SortOrder: 0}
	request("GET", "/api/tutorials", nil, nil, 401)
	request("GET", "/api/settings/tutorials", nil, reader, 403)
	request("POST", "/api/settings/tutorials", payload, reader, 403)
	request("PUT", "/api/settings/tutorials/1", payload, reader, 403)
	request("DELETE", "/api/settings/tutorials/1", nil, reader, 403)
	request("POST", "/api/tutorials", payload, reader, 405)
	var created Tutorial
	requestJSONForPricingTest(t, handler, "POST", "/api/settings/tutorials", payload, admin, &created)
	path := "/api/settings/tutorials/" + strconv.FormatInt(created.ID, 10)
	// Both creation and editing reject duplicate labels, even for drafts.
	request("POST", "/api/settings/tutorials", payload, admin, 409)
	duplicate := payload
	duplicate.Title = "  " + payload.Title + "  "
	request("POST", "/api/settings/tutorials", duplicate, admin, 409)
	duplicate.Title = "Another tutorial"
	duplicate.TitleEN = payload.Title // Collides with the English fallback.
	request("POST", "/api/settings/tutorials", duplicate, admin, 409)
	duplicate.Title = initial[0].Title
	duplicate.TitleEN = ""
	request("PUT", path, duplicate, admin, 409)
	// An unchanged title must remain editable, and an English title must not
	// collide with a later article's fallback label either.
	payload.TitleEN = "Windows Desktop guide"
	requestJSONForPricingTest(t, handler, "PUT", path, payload, admin, &created)
	duplicate.Title = payload.TitleEN
	request("POST", "/api/settings/tutorials", duplicate, admin, 409)
	var published, managed []Tutorial
	requestJSONForPricingTest(t, handler, "GET", "/api/tutorials", nil, reader, &published)
	requestJSONForPricingTest(t, handler, "GET", "/api/settings/tutorials", nil, admin, &managed)
	if len(published) != 3 || len(managed) != 4 {
		t.Fatal("draft was exposed or missing")
	}
	payload.Published = true
	payload.SortOrder = 100
	requestJSONForPricingTest(t, handler, "PUT", path, payload, admin, &created)
	requestJSONForPricingTest(t, handler, "GET", "/api/tutorials", nil, reader, &published)
	if len(published) != 4 || published[3].ID != created.ID {
		t.Fatal("publish/order failed")
	}
	payload.Published = false
	requestJSONForPricingTest(t, handler, "PUT", path, payload, admin, nil)
	requestJSONForPricingTest(t, handler, "GET", "/api/tutorials", nil, reader, &published)
	if len(published) != 3 {
		t.Fatal("unpublish failed")
	}
	request("DELETE", path, nil, admin, 204)
	request("DELETE", path, nil, admin, 404)
	request("PUT", path, payload, admin, 404)
	request("DELETE", "/api/settings/tutorials/bad-id", nil, admin, 404)
	for _, mutate := range []func(*Tutorial){
		func(v *Tutorial) { v.Title = " " },
		func(v *Tutorial) { v.Markdown = " " },
		func(v *Tutorial) { v.SortOrder = -1 },
		func(v *Tutorial) { v.SortOrder = 10001 },
		func(v *Tutorial) { v.Markdown = strings.Repeat("x", 128*1024+1) },
		func(v *Tutorial) { v.Title = strings.Repeat("中", 121) },
		func(v *Tutorial) { v.TitleEN = strings.Repeat("中", 121) },
	} {
		invalid := payload
		mutate(&invalid)
		request("POST", "/api/settings/tutorials", invalid, admin, 422)
	}
	// Seed only during migration: edits/deletions survive restart.
	payload.Title = "Edited seed"
	request("PUT", "/api/settings/tutorials/1", payload, admin, 200)
	request("DELETE", "/api/settings/tutorials/2", nil, admin, 204)
	a.Close()
	a, err = New()
	if err != nil {
		t.Fatal(err)
	}
	managed, err = a.listTutorials(t.Context(), false)
	if err != nil || len(managed) != 2 || managed[1].Title != "Edited seed" {
		t.Fatalf("restart lost changes: %#v, %v", managed, err)
	}
}

func TestTutorialCategoryMigrationPreservesArticles(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "tutorial-upgrade.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	goose.SetBaseFS(backendMigrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, db, ".", 202609210001); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tutorials SET title = 'Custom title', title_en = 'Custom EN', markdown = 'custom {{api_key}}', markdown_en = 'custom {{api_base_url}}', published = 0, sort_order = 88 WHERE id = 1;
		DELETE FROM tutorials WHERE id = 2;`); err != nil {
		t.Fatal(err)
	}
	platformLabels := map[string]string{"all": "", "windows": " Windows", "macos": " macOS", "linux": " Linux", "ios": " iOS", "android": " Android"}
	for _, platform := range []string{"all", "windows", "macos", "linux", "ios", "android"} {
		if _, err := db.Exec(`INSERT INTO tutorials (title, title_en, client, platform, markdown, markdown_en, sort_order, published, updated_at)
			VALUES ('Custom article', 'EN article', 'My Client', ?, 'body {{api_key}}', 'EN {{responses_url}}', 5, 1, '2026-09-21T01:00:00Z')`, platform); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Query(`SELECT id, title, title_en, client, platform, markdown, markdown_en, sort_order, published, updated_at FROM tutorials ORDER BY sort_order, id`)
	if err != nil {
		t.Fatal(err)
	}
	type categorizedTutorial struct {
		Tutorial
		Category string
	}
	var expected []categorizedTutorial
	for rows.Next() {
		var item categorizedTutorial
		var client, platform string
		if err := rows.Scan(&item.ID, &item.Title, &item.TitleEN, &client, &platform, &item.Markdown, &item.MarkdownEN, &item.SortOrder, &item.Published, &item.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		item.Category = client + platformLabels[platform]
		expected = append(expected, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	verify := func() {
		t.Helper()
		if !testColumnExists(t, db, "tutorials", "category") || testColumnExists(t, db, "tutorials", "platform") || testColumnExists(t, db, "tutorials", "client") {
			t.Fatal("tutorial schema still has multiple dimensions")
		}
		rows, err := db.Query(`SELECT id, title, title_en, category, markdown, markdown_en, sort_order, published, updated_at FROM tutorials ORDER BY sort_order, id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var actual []categorizedTutorial
		for rows.Next() {
			var item categorizedTutorial
			if err := rows.Scan(&item.ID, &item.Title, &item.TitleEN, &item.Category, &item.Markdown, &item.MarkdownEN, &item.SortOrder, &item.Published, &item.UpdatedAt); err != nil {
				t.Fatal(err)
			}
			actual = append(actual, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("migration changed article data: got %+v, want %+v; err=%v", actual, expected, err)
		}
		if !testIndexExists(t, db, "ix_tutorials_published_order") {
			t.Fatal("migration removed the tutorial listing index")
		}
	}
	for range 2 {
		if err := goose.UpToContext(ctx, db, ".", 202609210002); err != nil {
			t.Fatal(err)
		}
		verify()
	}
	// A downgrade preserves the merged name as a general client. Re-upgrading
	// must not append a second OS suffix or alter any article content.
	if err := goose.DownToContext(ctx, db, ".", 202609210001); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, db, ".", 202609210002); err != nil {
		t.Fatal(err)
	}
	verify()
}

func TestTutorialTitleMigrationPreservesArticles(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "tutorial-titles.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	goose.SetBaseFS(backendMigrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, db, ".", 202609210002); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tutorials SET title = 'My Windows guide', title_en = 'Custom EN', markdown = 'custom {{api_key}}', markdown_en = 'custom {{api_base_url}}', published = 0, sort_order = 88 WHERE id = 1;
		DELETE FROM tutorials WHERE id = 2;
		INSERT INTO tutorials (title, title_en, category, markdown, markdown_en, sort_order, published, updated_at) VALUES
		('Shared title', 'Shared EN', 'Same category', 'Article one', '{{api_key}}', 5, 1, '2026-09-21T01:00:00Z'),
		('Shared title', 'Shared EN', 'Same category', 'Article two', '{{responses_url}}', 6, 0, '2026-09-21T01:00:00Z'),
		('Codex CLI 接入教程', 'Codex CLI setup', 'Codex CLI Windows', 'User-created article', '', 7, 1, '2026-09-21T01:00:00Z');`); err != nil {
		t.Fatal(err)
	}
	a := &App{db: database.SQLite(db)}
	// The new reader does not depend on category, so it can snapshot the old
	// schema before upgrading, including duplicate titles and draft articles.
	expected, err := a.listTutorials(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range expected {
		if expected[i].ID == 3 {
			expected[i].Title = "Codex CLI Linux"
			expected[i].TitleEN = "Codex CLI Linux"
		}
		if expected[i].ID == 5 {
			expected[i].Title = "Shared title (2)"
			expected[i].TitleEN = "Shared EN (2)"
		}
	}
	verify := func() {
		t.Helper()
		for _, field := range []string{"category", "platform", "client"} {
			if testColumnExists(t, db, "tutorials", field) {
				t.Fatalf("removed field %q still present in schema", field)
			}
		}
		actual, err := a.listTutorials(ctx, false)
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("migration changed articles: got %+v, want %+v; err=%v", actual, expected, err)
		}
		if !testIndexExists(t, db, "ix_tutorials_published_order") {
			t.Fatal("migration removed the tutorial listing index")
		}
		for _, index := range []string{"ux_tutorials_title", "ux_tutorials_english_title"} {
			if !testIndexExists(t, db, index) {
				t.Fatalf("missing unique title index %q", index)
			}
		}
		for _, query := range []string{
			`UPDATE tutorials SET title = 'Shared title' WHERE id = 5`,
			`UPDATE tutorials SET title_en = 'Shared EN' WHERE id = 5`,
			`UPDATE tutorials SET title = 'Shared EN', title_en = '' WHERE id = 5`,
		} {
			if _, err := db.Exec(query); err == nil || !isUniqueConstraintError(err) {
				t.Fatalf("database accepted a duplicate tutorial title: %v", err)
			}
		}
	}
	for range 2 {
		// Exercise tutorial rollback before later, irreversible balance migrations.
		if err := goose.UpToContext(ctx, db, ".", 202609210004); err != nil {
			t.Fatal(err)
		}
		verify()
	}
	if err := goose.DownToContext(ctx, db, ".", 202609210002); err != nil {
		t.Fatal(err)
	}
	if err := a.runMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	verify()
}
