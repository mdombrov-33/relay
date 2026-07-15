# Database migrations

Relay uses [Goose](https://github.com/pressly/goose) for PostgreSQL migrations.
The repository pins Goose 3.27.1 as a Go tool; install all pinned tools with:

```sh
go install tool
```

Start the local PostgreSQL 18 service and apply the schema with:

```sh
make db-up
make migrate-validate
make migrate-status
make migrate-up
```

`make db-logs` follows PostgreSQL logs and `make db-shell` opens `psql` as the
local `relay` user. `make db-reset` deletes the disposable local database
volume, recreates it, and starts PostgreSQL again; it must never be used for a
database with data worth keeping.

The default local connection is
`postgres://relay:relay@localhost:5433/relay?sslmode=disable`. Override
`DATABASE_URL` when connecting to another PostgreSQL instance.

Each numbered `.sql` file has a `-- +goose Up` section and a matching
`-- +goose Down` section. Goose applies versions in numeric order and records
them in its `goose_db_version` table. Future capabilities belong in new
migrations; never rewrite an applied migration.

`000001_create_runs_and_events` establishes the current run projection and
append-only event history.
