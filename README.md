# Relay

A durable execution harness for tool-using LLM workflows, written in Go on PostgreSQL.

Relay runs an agent loop — model proposes tool calls, harness executes them — in a way that survives process death. Every model call and tool call is a durable checkpoint; a killed process restarts, replays completed work from storage without re-invoking the model or re-running side effects, and continues where it stopped. Dangerous tool calls pause the run as durable database state until a human approves or rejects them.

> The model proposes actions. The harness owns validation, authorization, execution, persistence, limits, and observation.

## What it does

- **Crash recovery without replaying side effects.** Each step is checkpointed by a stable key and an input hash. Completed steps return their recorded result on recovery; interrupted external effects retry through an idempotency ledger that collapses retries into one logical effect.
- **Durable human approval.** A tool call gated by policy transitions the run to `waiting` in one transaction — a database row, never a blocked goroutine. The process can exit; approval later resumes the run, and completed work replays from checkpoints.
- **Policy on declared authority.** Tools register as `read` or `effect`; an allowlist decides allow / deny / require-approval on the registered authority, not the model-proposed name. No policy means deny.
- **Append-only event timeline.** Everything observable is an immutable, ordered, safe event (identity and status only — never prompt text or credentials), enforced append-only by the database itself.
- **Bounded execution.** Step limits, per-call timeouts, and a byte-budgeted model context with checkpointed summarization of evicted history.
- **HTTP + SSE surface.** Run projections, paged event history, a resumable live event stream, and create / cancel / approval commands.

## How it works

Short version: six PostgreSQL tables (`runs`, `events`, `steps`, `effects`, `approval_requests`, `approval_signals`), explicit SQL through `pgx`, and an engine loop where every transition is a guarded transaction pairing a projection update with its lifecycle event.

The full walkthrough — data model, engine loop, checkpoint and recovery mechanics, the approval gate, the failure model — is in **[docs/architecture.md](docs/architecture.md)**.

## Running it

Requires Go 1.26+ and Docker.

```sh
make db-up          # PostgreSQL 18 in Docker Compose (localhost:5434)
make migrate-up     # apply Goose migrations
```

Deterministic in-memory demo of the engine loop:

```sh
go run ./cmd/relay
```

HTTP API on `127.0.0.1:4000`:

```sh
make api
```

```sh
curl -s -X POST localhost:4000/v1/runs                # create a durable pending run
curl -s localhost:4000/v1/runs                        # list run projections
curl -s localhost:4000/v1/runs/<id>/events            # paged event history
curl -N localhost:4000/v1/events/stream?after=0       # live SSE event stream
```

Inspect the durable event log from the CLI:

```sh
make relayctl ARGS='events'
make relayctl ARGS='events -run <id> -after 42'
```

## Tests

```sh
make test               # unit tests (no database)
make test-integration   # durability tests against the migrated local database
make check              # full gate: tests, race detector, lint
```

The integration tests prove the durability claims directly: they kill the first connection pool mid-run, reopen it, and assert that completed steps replay with zero model calls, an interrupted credit effect completes as one logical credit, and approval state survives restart.

## Project layout

```text
cmd/api/            HTTP/SSE server with graceful shutdown
cmd/relay/          deterministic in-memory workflow demo
cmd/relayctl/       read-only event log inspection
internal/run/       run identity and guarded state transitions
internal/event/     immutable safe event envelope
internal/model/     provider-independent model port and scripted fake
internal/tool/      tool contract, registry, declared authority
internal/policy/    authority allowlist
internal/workflow/  engine loop, checkpoints, approval gate, context budget
internal/postgres/  pgx pool and explicit-SQL store
migrations/         Goose schema migrations
```

## What it is not

Relay is honest about its guarantees. It does not claim exactly-once arbitrary external side effects (completed checkpoints are replay-safe; interrupted attempts retry through the idempotency ledger), it is not a sandbox, and it is not a Temporal replacement. The model boundary is a small interface currently backed by a deterministic scripted client, which is what keeps every durability test reproducible.
