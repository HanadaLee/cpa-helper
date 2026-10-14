package migrations

import "testing"

func TestPostgresImportBaselineTracksLatestSchema(t *testing.T) {
	if PostgreSQLBaselineVersion != LatestVersion {
		t.Fatal("update the PostgreSQL import baseline and manifest with the schema migration")
	}
	if len(PostgreSQLTables) == 0 {
		t.Fatal("missing PostgreSQL import manifest")
	}
}
