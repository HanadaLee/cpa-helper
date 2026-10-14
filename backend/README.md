# CPA-Helper Backend

Go backend for the local CPA-Helper application.

Run from this directory:

```powershell
go mod download
go run ./cmd/cpa-helper
```

The executable provides explicit operational subcommands:

```powershell
go run ./cmd/cpa-helper migrate  # run database migrations and exit
go run ./cmd/cpa-helper serve    # start only after read-only startup checks pass
go run ./cmd/cpa-helper doctor   # run read-only startup checks and exit
```

Running without a subcommand is the user-facing startup path: it runs
migrations, performs startup checks, then starts the service.

Useful checks:

```powershell
go fmt ./...
go test ./...
```

Local build output goes under `bin/`:

```powershell
go build -o bin/cpa-helper.exe ./cmd/cpa-helper
```

Database migrations are managed by embedded goose migrations in `migrations/`.
Use `cpa-helper migrate` for an explicit migration-only operation. Use
`cpa-helper serve` when an operator wants the service to fail instead of
modifying the database at startup. The Docker image default command runs the
same integrated startup path as the binary with no subcommand; Alembic is not
part of the Go runtime.
For SQLite Docker upgrades, the new image only needs the persisted SQLite database;
it does not require the old Python source tree or Alembic files.

## PostgreSQL

Set `CPA_HELPER_DATABASE_URL` to a PostgreSQL URL. `CPA_HELPER_DB_MAX_CONNS`
(default 16, range 2–100) controls the connection pool. Leaving the URL unset
preserves SQLite behavior. PostgreSQL has its own embedded migrations under
`migrations/postgres/`; future schema changes must update both dialects.

For existing SQLite data, **stop all writers**, back up the database, and use
`cpa-helper import-sqlite --source <snapshot>` with an empty PostgreSQL target.
Then run `verify-sqlite --source <snapshot>` and `doctor` before starting the
service. Read the [complete migration/rollback runbook](../docs/postgresql-migration.md)
before using these commands. Never start a migration rehearsal with live CPA settings.

PostgreSQL integration tests are opt-in:

```powershell
$env:CPA_HELPER_TEST_POSTGRES_URL = "postgres://test_user:password@127.0.0.1:5432/postgres?sslmode=disable"
go test ./internal/app -run "^TestPostgres" -count=1 -v
```

The test role needs CREATEDB. Tests create/drop only their own random databases;
the database named in the URL is not cleared. Do not use production credentials.

