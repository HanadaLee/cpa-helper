// Package database provides the small SQL dialect boundary shared by SQLite and PostgreSQL.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type DB struct {
	*sql.DB
	Postgres bool
}
type Tx struct {
	*sql.Tx
	postgres bool
}
type Stmt struct {
	*sql.Stmt
	postgres bool
}

func SQLite(db *sql.DB) *DB { return &DB{DB: db} }

func OpenPostgres(dsn string, maxConnections int) (*DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid CPA_HELPER_DATABASE_URL (expected PostgreSQL connection URL)")
	}
	cfg.RuntimeParams["timezone"] = "Asia/Shanghai"
	cfg.RuntimeParams["application_name"] = "cpa-helper"
	cfg.RuntimeParams["search_path"] = "public"
	cfg.ConnectTimeout = 10 * time.Second
	raw := stdlib.OpenDB(*cfg)
	raw.SetMaxOpenConns(maxConnections)
	raw.SetMaxIdleConns(min(4, maxConnections))
	raw.SetConnMaxIdleTime(5 * time.Minute)
	return &DB{DB: raw, Postgres: true}, nil
}

func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.DB.ExecContext(ctx, queryFor(query, db.Postgres), argsFor(args, db.Postgres)...)
}
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.DB.QueryContext(ctx, queryFor(query, db.Postgres), argsFor(args, db.Postgres)...)
}
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return db.DB.QueryRowContext(ctx, queryFor(query, db.Postgres), argsFor(args, db.Postgres)...)
}
func (db *DB) Exec(query string, args ...any) (sql.Result, error) {
	return db.ExecContext(context.Background(), query, args...)
}
func (db *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return db.QueryContext(context.Background(), query, args...)
}
func (db *DB) QueryRow(query string, args ...any) *sql.Row {
	return db.QueryRowContext(context.Background(), query, args...)
}

type insertedID int64

func (id insertedID) LastInsertId() (int64, error) { return int64(id), nil }
func (id insertedID) RowsAffected() (int64, error) { return 1, nil }

// InsertContext is explicit: use only for inserts into tables with a generated id.
// PostgreSQL does not implement LastInsertId; RETURNING retrieves the id atomically.
func (db *DB) InsertContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !db.Postgres {
		return db.ExecContext(ctx, query, args...)
	}
	var id insertedID
	err := db.QueryRowContext(ctx, strings.TrimSuffix(strings.TrimSpace(query), ";")+" RETURNING id", args...).Scan(&id)
	return id, err
}
func (tx *Tx) InsertContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !tx.postgres {
		return tx.ExecContext(ctx, query, args...)
	}
	var id insertedID
	err := tx.QueryRowContext(ctx, strings.TrimSuffix(strings.TrimSpace(query), ";")+" RETURNING id", args...).Scan(&id)
	return id, err
}

func (db *DB) BeginTx(ctx context.Context, options *sql.TxOptions) (*Tx, error) {
	if db.Postgres && options != nil && options.ReadOnly && options.Isolation == sql.LevelDefault {
		copy := *options
		copy.Isolation = sql.LevelRepeatableRead
		options = &copy
	}
	tx, err := db.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, postgres: db.Postgres}, nil
}
func (tx *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, queryFor(query, tx.postgres), argsFor(args, tx.postgres)...)
}
func (tx *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.Tx.QueryContext(ctx, queryFor(query, tx.postgres), argsFor(args, tx.postgres)...)
}
func (tx *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return tx.Tx.QueryRowContext(ctx, queryFor(query, tx.postgres), argsFor(args, tx.postgres)...)
}
func (tx *Tx) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	stmt, err := tx.Tx.PrepareContext(ctx, queryFor(query, tx.postgres))
	if err != nil {
		return nil, err
	}
	return &Stmt{Stmt: stmt, postgres: tx.postgres}, nil
}
func (s *Stmt) ExecContext(ctx context.Context, args ...any) (sql.Result, error) {
	return s.Stmt.ExecContext(ctx, argsFor(args, s.postgres)...)
}
func (tx *Tx) ForUpdate() string {
	if tx.postgres {
		return " FOR UPDATE"
	}
	return ""
}

// Both schemas store flags as 0/1. This keeps existing SQL predicates and migration values identical.
func argsFor(args []any, postgres bool) []any {
	if !postgres {
		return args
	}
	out := append([]any(nil), args...)
	for i, value := range out {
		switch v := value.(type) {
		case bool:
			if v {
				out[i] = int64(1)
			} else {
				out[i] = int64(0)
			}
		case *bool:
			if v == nil {
				out[i] = nil
			} else if *v {
				out[i] = int64(1)
			} else {
				out[i] = int64(0)
			}
		case sql.NullBool:
			if !v.Valid {
				out[i] = nil
			} else if v.Bool {
				out[i] = int64(1)
			} else {
				out[i] = int64(0)
			}
		}
	}
	return out
}

var julianPredicate = regexp.MustCompile("julianday\\(([^()]+)\\)")

func queryFor(query string, postgres bool) string {
	if !postgres {
		return query
	}
	// Runtime uses julianday only for expiry comparisons, never for stored values.
	// A cast of the literal 'now' is frozen when PostgreSQL prepares a statement.
	// CURRENT_TIMESTAMP is evaluated for each transaction, so expiring cards stay correct.
	query = strings.ReplaceAll(query, "julianday('now')", "EXTRACT(EPOCH FROM CURRENT_TIMESTAMP)")
	query = julianPredicate.ReplaceAllString(query, "EXTRACT(EPOCH FROM ($1)::timestamptz)")
	return Rebind(query)
}

// Rebind converts positional parameters, skipping SQL strings, identifiers and comments.
func Rebind(query string) string {
	var out strings.Builder
	n := 0
	for i := 0; i < len(query); {
		start := i
		switch {
		case query[i] == '\'' || query[i] == '"':
			quote := query[i]
			i++
			for i < len(query) {
				if query[i] == quote {
					i++
					if i < len(query) && query[i] == quote {
						i++
						continue
					}
					break
				}
				i++
			}
		case strings.HasPrefix(query[i:], "--"):
			for i < len(query) && query[i] != '\n' {
				i++
			}
		case strings.HasPrefix(query[i:], "/*"):
			i += 2
			depth := 1
			for i < len(query) && depth > 0 {
				if strings.HasPrefix(query[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(query[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
		case query[i] == '?':
			n++
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(n))
			i++
			continue
		default:
			i++
		}
		out.WriteString(query[start:i])
	}
	return out.String()
}
