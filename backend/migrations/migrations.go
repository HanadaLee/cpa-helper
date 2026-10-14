package migrations

import "embed"

// LatestVersion is the newest embedded migration version this binary expects.
const LatestVersion int64 = 202609280001

// FS contains SQL migrations embedded into the application binary.
//
//go:embed *.sql
var FS embed.FS

// PostgresFS has an independent SQL migration chain. SQLite's historical Go migrations
// must never be registered against PostgreSQL.
//
//go:embed postgres/*.sql
var PostgresFS embed.FS
