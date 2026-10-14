package app

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Tutorial struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	TitleEN    string `json:"title_en"`
	Markdown   string `json:"markdown"`
	MarkdownEN string `json:"markdown_en"`
	SortOrder  int    `json:"sort_order"`
	Published  bool   `json:"published"`
	UpdatedAt  string `json:"updated_at"`
}

func (a *App) listTutorials(ctx context.Context, publishedOnly bool) ([]Tutorial, error) {
	query := `SELECT id, title, title_en, markdown, markdown_en, sort_order, published, updated_at FROM tutorials`
	if publishedOnly {
		query += ` WHERE published = 1`
	}
	rows, err := a.db.QueryContext(ctx, query+` ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Tutorial{}
	for rows.Next() {
		var item Tutorial
		if err := rows.Scan(&item.ID, &item.Title, &item.TitleEN, &item.Markdown, &item.MarkdownEN, &item.SortOrder, &item.Published, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *App) handleTutorials(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.readyUser(r.Context(), r); err != nil {
		return err
	}
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	items, err := a.listTutorials(r.Context(), true)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, items)
	return nil
}

func validateTutorial(item *Tutorial) error {
	item.Title = strings.TrimSpace(item.Title)
	item.TitleEN = strings.TrimSpace(item.TitleEN)
	item.Markdown = strings.TrimSpace(item.Markdown)
	item.MarkdownEN = strings.TrimSpace(item.MarkdownEN)
	if item.Title == "" || item.Markdown == "" {
		return validationError("教程标题和正文不能为空")
	}
	if utf8.RuneCountInString(item.Title) > 120 || utf8.RuneCountInString(item.TitleEN) > 120 || len(item.Markdown) > 128*1024 || len(item.MarkdownEN) > 128*1024 {
		return validationError("教程内容超出长度限制")
	}
	if item.SortOrder < 0 || item.SortOrder > 10000 {
		return validationError("教程排序必须在 0 到 10000 之间")
	}
	return nil
}

func (a *App) handleTutorialManagement(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(r.URL.Path, "/api/settings/tutorials")
	var id int64
	if path != "" {
		var err error
		id, err = strconv.ParseInt(strings.TrimPrefix(path, "/"), 10, 64)
		if err != nil || id <= 0 {
			return notFoundError("教程不存在")
		}
	}
	if r.Method == http.MethodGet && id == 0 {
		items, err := a.listTutorials(r.Context(), false)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, items)
		return nil
	}
	var result sql.Result
	var err error
	var item Tutorial
	switch {
	case r.Method == http.MethodPost && id == 0, r.Method == http.MethodPut && id > 0:
		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		if err := decodeJSON(r, &item); err != nil {
			return err
		}
		if err := validateTutorial(&item); err != nil {
			return err
		}
		item.ID = id
		item.UpdatedAt = apiDateTime(time.Now())
		args := []any{item.Title, item.TitleEN, item.Markdown, item.MarkdownEN, item.SortOrder, item.Published, item.UpdatedAt}
		if id == 0 {
			result, err = a.db.InsertContext(r.Context(), `INSERT INTO tutorials (title, title_en, markdown, markdown_en, sort_order, published, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, args...)
		} else {
			result, err = a.db.ExecContext(r.Context(), `UPDATE tutorials SET title = ?, title_en = ?, markdown = ?, markdown_en = ?, sort_order = ?, published = ?, updated_at = ? WHERE id = ?`, append(args, id)...)
		}
	case r.Method == http.MethodDelete && id > 0:
		result, err = a.db.ExecContext(r.Context(), `DELETE FROM tutorials WHERE id = ?`, id)
	default:
		return methodNotAllowed()
	}
	if err != nil {
		if isUniqueConstraintError(err) {
			return conflictError("教程标题重复，请修改标题或英文标题")
		}
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFoundError("教程不存在")
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	status := http.StatusOK
	if id == 0 {
		item.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		status = http.StatusCreated
	}
	writeJSON(w, status, item)
	return nil
}
