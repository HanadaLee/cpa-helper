package migrations

import (
	"fmt"
	"testing"
)

func TestPostgresImportBaselineTracksLatestSchema(t *testing.T) {
	if PostgreSQLBaselineVersion != LatestVersion {
		t.Fatal("update the PostgreSQL import baseline and manifest with the schema migration")
	}
	if len(PostgreSQLTables) == 0 {
		t.Fatal("missing PostgreSQL import manifest")
	}
	if _, err := PostgresFS.ReadFile(fmt.Sprintf("postgres-import/%d_baseline.sql", PostgreSQLBaselineVersion)); err != nil {
		t.Fatalf("missing current atomic-import schema: %v", err)
	}
}
