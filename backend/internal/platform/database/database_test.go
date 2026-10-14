package database

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestRebindDoesNotChangeQuotedTextOrComments(t *testing.T) {
	input := `SELECT ?, 'it''s ?', "?", ? -- comment ?
/* ? /* nested ? */ ? */ WHERE field=?`
	want := `SELECT $1, 'it''s ?', "?", $2 -- comment ?
/* ? /* nested ? */ ? */ WHERE field=$3`
	if got := Rebind(input); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPostgresExpiryAndFlags(t *testing.T) {
	got := queryFor("julianday(expires_at) > julianday('now') AND kind = ?", true)
	want := "EXTRACT(EPOCH FROM (expires_at)::timestamptz) > EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AND kind = $1"
	if got != want {
		t.Fatal(got)
	}
	flag := true
	var missing *bool
	args := []any{true, false, &flag, missing, sql.NullBool{}, sql.NullBool{Bool: true, Valid: true}, 3.5, "?"}
	expected := []any{int64(1), int64(0), int64(1), nil, nil, int64(1), 3.5, "?"}
	if got := argsFor(args, true); !reflect.DeepEqual(got, expected) {
		t.Fatalf("%#v", got)
	}
	if _, ok := args[0].(bool); !ok {
		t.Fatal("mutated caller arguments")
	}
}
