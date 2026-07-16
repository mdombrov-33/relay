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

Milestones 3 through 8 are complete. Milestone 9, HTTP transport, SSE, and the
inspector, is active.

The repository already has a bounded in-memory model/tool loop, typed redacted
events, CLI timelines, a `runs`/append-only-`events` Goose migration, PostgreSQL
18 Compose service, a ping-verified `pgx/v5` pool, and a concrete store that
atomically creates a pending run or applies a terminal transition with its
lifecycle event, and reads bounded ordered event pages by run or global cursor.
`make test-integration` connects to the migrated local database and proves
committed run/event history survives reopening a pool.

The `steps` migration and Store API provide a durable, unique checkpoint
projection keyed by run and step. A matching completed checkpoint returns its
JSON result after reopening a pool. Recovery increments a running checkpoint's
attempt; completion requires that attempt, preventing a stale worker from
overwriting the current attempt.

The workflow now has an opt-in durable step runner. It hashes a model request,
claims or recovers its checkpoint, and returns the stored response without
calling the model when that checkpoint is complete. An integration test closes
the first PostgreSQL pool and proves a recovery engine makes zero model calls.

The same runner now wraps tool calls, hashing the full call and storing the
serialized tool output. A pool-restart test proves a completed model/tool/model
sequence returns its durable results without calling the recovered model client
or tool executable.

The `effects` ledger records one completed synthetic effect for a global stable
idempotency key. Tool execution receives harness-owned run and step identity;
the synthetic `issue_credit` tool derives its key from that identity and returns
the original recorded credit on recovery. An integration test interrupts after
the first credit record but before its tool checkpoint completes, reopens the
pool, and proves retry completes attempt two with one logical credit.

The pure `workflow.ContextHydrator` builds a new model-message slice from pinned
input and durable-history candidates. Its explicit serialized-byte budget
preserves pinned input, retains the newest contiguous whole-message suffix that
fits, rejects an oversized pinned task, and does not mutate caller-owned data.

`workflow.Engine` now keeps the original request messages pinned and accumulates
assistant/tool messages separately. It hydrates a fresh request before every
model call, using an explicit `ContextBudgetBytes` or the 16 KiB default. A
multi-turn test proves the bounded request retains the task and newest exchange
while omitting an older exchange.

`workflow.CompactionPlanner` now separates over-budget history into an evicted
oldest prefix and retained newest suffix at a lower watermark. It never splits a
message and always keeps the latest message verbatim, even if that one message
is larger than the lower watermark.

The engine can now opt into a paired `CompactionPlanner` and `SummaryStep`.
Before a later model turn it summarizes an evicted prefix through a stable
`memory/summary/<turn>` checkpoint, retains the newest suffix, and hydrates the
original pins plus the summary plus that suffix. It records a safe-count-only
`memory.compacted.v1` event; summary text and history never enter the event.

Registered tools now declare validated `read` or `effect` authority. The engine
retrieves stored registry metadata for a configured policy before executable
resolution, so `policy.Allowlist` decides declared authority rather than a
model-proposed name; missing policy remains deny by default. A distinct
`require_approval` decision routes through a durable approval gate before the
tool can be resolved.

Milestone 8, durable human approval, is complete.
The in-memory run lifecycle now permits `running -> waiting -> running`, treats
waiting as non-terminal, and permits cancellation while waiting. Success and
failure remain legal only from running.

Goose migration `000004` permits the persisted waiting status and adds an
`approval_requests` projection with one pending request per run. The Store
atomically inserts a safe pending request, transitions only a running run to
waiting, and appends its matching `approval.requested.v1` event. An injected
event-insert failure proves all three writes roll back together.

Goose migration `000005` adds one durable approval signal per request. The
Store locks the pending request, records the first approved or rejected
decision, resolves the request, resumes only its matching waiting run, and
appends `approval.resolved.v1` in one transaction. Repeated matching decisions
are idempotent; conflicting decisions fail explicitly.

`workflow.ApprovalGate` derives a stable request ID from the run and tool step,
persists the first wait, and consumes the stored status on recovery. The engine
returns `ErrApprovalPending` without holding a goroutine, executes the original
checkpointed call only after approval, and gives the model a safe rejection
result without resolving the executable. Integration tests restart while the
request is pending and again after its signal.

The PostgreSQL Store reads one run and its pending approval as a consistent
projection. `internal/httpapi` exposes it through `GET /v1/runs/{id}` with
stable JSON responses and without leaking storage errors or sensitive payloads.
`GET /v1/runs/{id}/events?after=N` returns the existing bounded ordered event
page with an exclusive cursor and a deterministic `nextAfter` value.
`POST /v1/runs/{id}/signals/approval` accepts an approved or rejected decision,
derives its step from the durable request, and supplies server-owned signal and
event identity plus time to the existing atomic resolution transaction. A
matching duplicate returns the same successful response; a conflict is explicit.

Next: add durable run cancellation through PostgreSQL and
`POST /v1/runs/{id}/cancel`, committing the canceled projection and lifecycle
event together for any nonterminal run.

## Repository map

```text
cmd/relay/          deterministic in-memory workflow demo
cmd/relayctl/       read-only PostgreSQL event inspection command
internal/run/       run identity, status, guarded transitions
internal/event/     immutable safe event envelope and payloads
internal/model/     provider-independent port and scripted fake
internal/tool/      tool contract, registry, deterministic lookups
internal/workflow/  bounded orchestration loop
internal/httpapi/   HTTP projections and commands
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
- Event inspection: `make relayctl ARGS='events'`
- Unit tests: `make test`
- Full gate: `make check`
- Database: `make db-up`, `make migrate-up`, `make test-integration`
- Default URL: `postgres://relay:relay@localhost:5434/relay?sslmode=disable`

The full roadmap, contract detail, and decision rationale live in the linked
Obsidian notes. Keep this file an orientation snapshot, not a second playbook.
