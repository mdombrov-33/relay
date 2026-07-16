# Relay

Relay is a learning-focused, durable agent harness written in Go. It will make an agent workflow observable, recoverable after interruption, and safe to pause for human approval.

The project uses a synthetic support-credit workflow to demonstrate event history, checkpoints, idempotent effects, and an eventual inspection UI.

## Status

Milestone 2 is complete: the deterministic in-memory workflow emits a typed,
append-only event timeline. Milestone 3 is adding PostgreSQL persistence.

## Local PostgreSQL

The development database is PostgreSQL 18 in Docker Compose. Start it and
apply the Goose migrations with:

```sh
make db-up
make migrate-up
make test-integration
```

The local connection URL is
`postgres://relay:relay@localhost:5433/relay?sslmode=disable`.
Use `make db-logs` to follow PostgreSQL logs, `make db-shell` to inspect the
database, and `make db-reset` to recreate the disposable local database.

## Project notes

The implementation plan and learning record live in the accompanying Obsidian
playbook: `Projects/Relay/Relay Harness - Project Playbook.md`.
