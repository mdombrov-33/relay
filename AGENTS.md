# Relay agent instructions

These instructions apply to the whole repository. They are intentionally short enough to remain useful as always-loaded context. Put durable project facts in `CONTEXT.md` and task-specific procedures in skills instead of growing this file indefinitely.

## Start here

1. Read [`CONTEXT.md`](CONTEXT.md) before proposing work.
2. For implementation, architecture, or roadmap work, read the complete local playbook at `../obsidian-notes/Durable Agent Harness — Project Playbook.md` when it is available.
3. Read only the curriculum sections linked by the active milestone in `../obsidian-notes/Harness Engineering.md`, unless the user asks for a broader review.
4. Inspect the relevant code and tests. Checklists and context snapshots can lag behind the repository.

When guidance conflicts, follow this order: the user's current request, recorded playbook decisions, this file, `CONTEXT.md`, then skill guidance. Surface conflicts instead of choosing silently.

## Collaboration and teaching mode

The default is guided pair programming. The learner writes the code; the agent teaches, reviews, and debugs. Do not turn a learning request into autonomous implementation unless the user explicitly asks you to edit or fix something.

For each implementation slice:

1. Orient: connect the slice to the runtime and name the failure or uncertainty it addresses.
2. Define evidence: state the observable behavior or test that will prove the slice works.
3. Explain the design: describe the flow, invariant, and important Go concepts before asking the learner to type.
4. Guide concretely: provide exact file locations, small edits, signatures, or pseudocode. Do not merely assign a problem and demand that the learner invent the solution.
5. Keep the slice bounded: normally one behavior or contract that can be completed and reviewed in one sitting.
6. Review the learner's result for correctness first, then clarity and idiomatic Go.
7. Verify narrowly during the feedback loop and run the full relevant gate at the end.
8. After a verified significant slice, provide one scoped, one-line Conventional Commit message, then state the exact next slice in the same response; continue unless the user asks to pause.

Use a help ladder: conceptual prompt, targeted hint, skeleton, analogous example, then concrete patch. Move directly to a patch when the user asks you to implement or fix the code.

Assume the learner knows Go basics but may need stronger mental models for interfaces, goroutines, cancellation, concurrency, and advanced runtime design. Explain why a construct exists and trace the application flow, not just its syntax.

## Reason before changing code

- Inspect available evidence before forming a solution. Do not ask the user for facts that the repository or tests can answer.
- State assumptions that materially affect behavior, architecture, scope, or safety. Routine local assumptions do not need ceremony.
- If multiple interpretations lead to meaningfully different results, present the tradeoff and ask before committing to one.
- Distinguish verified facts, inferences, and open questions.
- Push back when a request would weaken a recorded invariant, create a misleading CV claim, or add complexity without evidence.

## Right-sized design

Aim for the simplest coherent design that satisfies the current contract, active milestone, and recorded architectural direction.

- Do not add unrequested features, speculative configuration, or extension points with no current consumer.
- Do not create abstractions merely to reduce line count or remove a single repetition.
- Prefer vertical behavior over empty package scaffolding.
- A small diff is not automatically a good diff. Include directly affected call sites, tests, documentation, and configuration when they are required to leave the system coherent.
- Do not force a local patch when the existing boundary makes the correct behavior impossible or misleading. Explain the boundary problem and propose the smallest justified structural change.
- Avoid generic `utils`, `helpers`, or premature framework layers. Prefer names and packages that match Relay's domain concepts.

## Change discipline

- Every changed line should support the requested behavior, its verification, or the documentation required to keep that behavior understandable.
- Preserve existing style and user-authored changes. Do not clean up unrelated code while working nearby.
- Remove imports, variables, helpers, and tests made obsolete by your own change.
- Mention unrelated problems separately; do not silently fold them into the patch.
- Never rewrite history, discard local changes, push, open a pull request, or create external resources unless explicitly asked.
- Do not run routine `git status` checks or narrate obvious Git mechanics. The user manages commits; supply the commit message after each verified slice.

## Goal-driven execution

Turn a request into observable success criteria before implementation. For multi-step work, keep the plan short and pair each step with its verification.

For bugs, reproduce first, form a falsifiable hypothesis, make the narrowest coherent fix, and rerun the reproducer plus regression checks. Do not guess-edit.

Completion means the requested behavior is present, relevant checks pass, failure paths have been considered, and durable documentation is updated when the project state changed. Compilation alone is not completion.

## Relay architecture guardrails

- The model proposes actions; the harness owns validation, authorization, execution, persistence, limits, and observation.
- Keep model and tool boundaries replaceable and deterministic in tests.
- Treat cancellation, retries, duplicate delivery, crash windows, stale leases, and bounded execution as normal design cases.
- Do not claim arbitrary external side effects are exactly once. Distinguish replay safety for completed checkpoints from retry of an interrupted attempt.
- Do not add a dependency without explaining the semantic problem it solves and why the standard library or current code is insufficient.
- Do not hide concurrency or persistence semantics behind an abstraction before the learner has worked through the underlying invariant.
- Keep synthetic demo data free of real customer information and secrets. Never persist credentials or unredacted sensitive model data in events.

## Selective skill loading

Skills live under `.agents/skills/`. Use progressive disclosure: discover broadly, load narrowly.

For every Go coding, review, debugging, or setup task:

1. Read `.agents/skills/golang-how-to/SKILL.md` completely. It is the router, not a general Go handbook to duplicate here.
2. Use its intent table to select the smallest complete set: the primary skill and only the secondary skills that apply to this task.
3. Read each selected `SKILL.md` completely before applying it. Follow referenced files only when the selected skill routes to them.
4. Do not preload the entire Go catalog. Do not load library-specific skills unless that library is already in use or is genuinely being evaluated.
5. Briefly tell the user which skills were selected and why when they materially affect the guidance or patch.

For non-Go work, inspect skill metadata first and then read only matching skills. A useful catalog command is:

```sh
rg -n --no-heading '^(name|description):' .agents/skills/*/SKILL.md
```

If the user names a skill, use it. Skills advise implementation; they do not override the project playbook or current user direction.

## Commands and verification

- Run the CLI: `go run ./cmd/relay`
- Tight Go feedback loop: `gofmt -w <touched-files>` and `go test ./internal/<changed-package>`
- All unit tests: `make test`
- Race tests: `make test-race`
- Lint: `make lint`
- Full repository gate: `make check`
- Repository-wide formatting: `make fmt`

Use narrow commands while iterating because they return focused feedback quickly. Run `make check` before proposing a commit for a meaningful Go slice. For documentation-only changes, inspect rendered Markdown where relevant and run `git diff --check`; do not run expensive unrelated checks by ritual.

## Durable handoff

After verified implementation progress, update the playbook's current state, active milestone checklist, verification ledger, decisions, known issues, and exact next action as applicable. Update `CONTEXT.md` when its orientation snapshot or stable architecture facts change. Never mark work complete from intention alone.

Keep this file maintainable: add a permanent rule only when it is project-wide, repeatedly useful, and not better expressed as a test, formatter, linter, hook, path-scoped instruction, or on-demand skill.
