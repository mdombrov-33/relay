# Relay

Relay is a learning-focused, durable agent harness written in Go. It will make an agent workflow observable, recoverable after interruption, and safe to pause for human approval.

The project uses a synthetic support-credit workflow to demonstrate event history, checkpoints, idempotent effects, and an eventual inspection UI.

## Status

Milestone 3 is complete: Relay persists its run projection and append-only
event history in PostgreSQL, and exposes a small read-only event inspector.

## Local PostgreSQL

The development database is PostgreSQL 18 in Docker Compose. Start it and
apply the Goose migrations with:

```sh
make db-up
make migrate-up
make test-integration
```

The local connection URL is
`postgres://relay:relay@localhost:5434/relay?sslmode=disable`.
Use `make db-logs` to follow PostgreSQL logs, `make db-shell` to inspect the
database, and `make db-reset` to recreate the disposable local database.

## Event inspection

After applying migrations, inspect the first ordered global event page with:

```sh
make relayctl ARGS='events'
```

Filter by a run and continue after an exclusive sequence cursor with:

```sh
make relayctl ARGS='events -run run-123 -after 42'
```

`relayctl` is read-only and returns at most 100 events per call. Use the last
printed sequence as the next `-after` value.

## Project notes

The implementation plan and learning record live in the accompanying Obsidian
playbook: `Projects/Relay/Relay Harness - Project Playbook.md`.
