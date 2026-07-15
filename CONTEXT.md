# Relay project context

## Fast orientation

Relay is a Go runtime for tool-using LLM workflows that will become observable, resumable after process failure, bounded, policy-controlled, and safe to pause for durable human approval.

The central distinction is:

> The model decides what action to propose. The harness owns whether and how that action executes.

This is runtime infrastructure, not a chatbot application or a thin model API wrapper. The final portfolio centerpiece is a visible kill/restart/resume demonstration in which completed work is not replayed and an interrupted side effect produces one logical effect through a stable idempotency key.

## Source of truth

The canonical implementation and teaching plan currently lives outside the repository in the accompanying Obsidian vault:

- `../obsidian-notes/Durable Agent Harness — Project Playbook.md` — decisions, milestones, acceptance criteria, verification ledger, current state, and session handoff.
- `../obsidian-notes/Harness Engineering.md` — conceptual curriculum and deeper reasoning.

Read the playbook completely for architecture or implementation planning. Read only curriculum sections linked by the active milestone unless broader conceptual review is needed. These paths are local to the development workspace; the repository does not currently contain a public copy of either document.

## Runtime mental model

```text
load run
  -> hydrate bounded context
  -> ask the current model for the next action
  -> validate and authorize the action
  -> durably record intent
  -> execute or suspend
  -> checkpoint the result
  -> append observable events
  -> repeat until terminal or bounded limit
```

Important concepts:

- A command requests a change, such as start, cancel, approve, or reject.
- An event is an immutable fact that already happened.
- Run state is the current mutable operational projection.
- A checkpoint stores the outcome of a named non-deterministic step for recovery.
- A signal is durable external input correlated to a waiting run.
- Stable run IDs, step keys, and idempotency keys make recovery behavior explainable.

## System shape

- The execution runtime and HTTP services are written in Go.
- PostgreSQL stores runs, ordered events, checkpoints, signals, leases, and the synthetic effect ledger.
- HTTP endpoints separate commands from queries; SSE carries ordered live events and supports reconnect from a cursor.
- A React and TypeScript inspector renders the runtime as a run list, event timeline, checkpoint detail, and approval surface.
- Model providers sit behind a small internal Go interface so provider SDK details do not leak into workflow code.
- Relay owns a deliberately bounded checkpoint engine. Temporal is a comparison point, not an implementation detail hidden inside the core design.
- The demonstration domain is a synthetic support-credit workflow with differently privileged triage and billing roles.

The project does not claim to be a general Temporal replacement, a hardened multi-tenant sandbox, or an exactly-once external-effects system.

## Repository shape today

```text
cmd/relay/          deterministic in-memory workflow demo
internal/run/       run identity, status, and guarded lifecycle transitions
internal/model/     provider-independent messages, client port, and scripted fake
internal/tool/      tool contract, registry, and deterministic lookup tools
internal/workflow/  orchestration of a run through the model boundary
```

The current engine executes an in-memory bounded model/tool loop. It starts the run, requires positive `MaxSteps`, `ModelTimeout`, and `ToolTimeout` limits, checks cancellation before each model turn, and derives a deadline-bound child context for every model or tool call. It executes returned tool calls through the registry and appends assistant plus correlated tool-result messages before the next turn. A response without tool calls completes the run; exhausted step limits and expired call deadlines produce typed failed runs. `go run ./cmd/relay` wires the scripted client, two deterministic lookup tools, registry, and engine into a runnable three-turn demo. Progress remains in memory and is not yet durable.

The existing boundaries already establish several important contracts:

- `model.Client` owns only the provider call. It does not control run state or execute tools.
- `tool.Tool` exposes a specification and executes a typed call through `context.Context`.
- `tool.Registry` validates registrations and resolves tools by the model-visible name.
- `tool.CustomerLookup` and `tool.IncidentLookup` are safe deterministic read-only demo tools with typed argument and not-found failures.
- `model.Message` can represent system, user, assistant, and correlated tool-result messages.
- `model.ScriptedClient` makes workflow behavior deterministic and records defensive copies of model requests for transcript assertions.
- `run.Run` is the authority for legal lifecycle transitions.

When orienting in the codebase, follow behavior through these contracts and their tests instead of inferring architecture from directory names alone.

## Core invariants to preserve

1. Every run has a stable unique identity.
2. Every non-deterministic operation eventually receives a stable step key.
3. A completed checkpoint returns its recorded result rather than re-executing.
4. Tools are resolved through a registry; the model never invokes host functions directly.
5. Tool authorization and policy are enforced by the harness, not by prompt wording.
6. Loops, context, payloads, time, and concurrency are bounded.
7. Waiting approval becomes durable state and must not occupy a goroutine indefinitely.
8. Full history remains durable even when the model sees bounded context.
9. External side effects may retry after an ambiguous crash window and therefore require stable idempotency.
10. Claims in the README and CV must be backed by executable evidence.

## Learner and collaboration context

The learner knows Go basics and has some Go experience, but is still developing confidence with interfaces, goroutines, cancellation, and advanced runtime concepts. By default, the learner types the implementation while the LLM explains the exact edits, flow, rationale, failure cases, and verification. Full patches are appropriate when explicitly requested.

Work proceeds in small verified slices. During a slice, use focused formatting and package tests for quick feedback; before a meaningful Go commit, run the repository gate. After each verified slice, provide a scoped one-line commit message and update the durable playbook.

## Local toolchain

- Go module: `github.com/mdombrov-33/relay`
- Go policy currently pinned by `go.mod`: Go 1.26.3
- Local development platform recorded so far: macOS arm64
- Run: `go run ./cmd/relay`
- Unit tests: `make test`
- Full quality gate: `make check`
- CI: push to `main`, using the Go version from `go.mod` and golangci-lint v2.12.2

The README is intentionally brief. Use the playbook for changing project state, unresolved decisions, implementation order, and the exact session handoff; keep this file focused on stable orientation.
