# Relay project context

## Read order

This file is the repository-local orientation. For implementation or planning,
then read the compact external [[Projects/Relay/Relay Harness - Project Playbook]] at
`../obsidian-notes/Projects/Relay/Relay Harness - Project Playbook.md`. Read only the
linked curriculum, architecture contract, or decision note needed by the active
slice; do not preload the whole vault.

## What Relay is

Relay is a Go runtime for tool-using LLM workflows. It will become observable,
resumable after process failure, bounded, policy-controlled, and safe to pause
for durable human approval.

> The model proposes actions. The harness owns validation, authorization,
> execution, persistence, limits, and observation.

The final demo kills and restarts a synthetic support-credit workflow, resumes
completed work without replay, approves one gated effect, and shows the durable
event timeline.

## Current phase

Milestone 3 is active: PostgreSQL event log and run projection.

The repository already has a bounded in-memory model/tool loop, typed redacted
events, CLI timelines, a `runs`/append-only-`events` Goose migration, PostgreSQL
18 Compose service, a ping-verified `pgx/v5` pool, and a concrete store that
atomically creates a pending run plus its queued event. `make test-integration`
connects to the migrated local database.

Next: transactionally persist terminal run transitions with their lifecycle
events.

## Repository map

```text
cmd/relay/          deterministic in-memory workflow demo
internal/run/       run identity, status, guarded transitions
internal/event/     immutable safe event envelope and payloads
internal/model/     provider-independent port and scripted fake
internal/tool/      tool contract, registry, deterministic lookups
internal/workflow/  bounded orchestration loop
internal/postgres/  direct PostgreSQL pool and integration test
migrations/         Goose PostgreSQL schema migrations
```

## Guardrails

- Keep the model and tool boundaries replaceable and deterministic in tests.
- Stable run IDs and step keys make recovery explainable.
- Events are immutable facts; run state is a mutable projection.
- Completed checkpoints return recorded results; interrupted external effects
  can retry and therefore need stable idempotency keys.
- Full history is durable; model context is reconstructed and bounded.
- Waiting approval is durable state, never a blocked goroutine.
- Do not persist credentials or unredacted sensitive model data in events.
- Do not claim arbitrary exactly-once effects, a hardened sandbox, or a
  Temporal replacement.

## Local commands

- Demo: `go run ./cmd/relay`
- Unit tests: `make test`
- Full gate: `make check`
- Database: `make db-up`, `make migrate-up`, `make test-integration`
- Default URL: `postgres://relay:relay@localhost:5434/relay?sslmode=disable`

The full roadmap, contract detail, and decision rationale live in the linked
Obsidian notes. Keep this file an orientation snapshot, not a second playbook.
