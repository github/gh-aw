---
description: Loop-engineering workflow patterns and implementation guidance for gh-aw workflows.
---

# Loop Engineering Patterns

Use as a playbook for long-running iterative workflows.

## What “loop engineering” means

A loop workflow repeatedly: 1) selects one work item, 2) makes one bounded improvement step, 3) verifies with concrete evidence, 4) preserves accepted progress, 5) records durable state for the next run.

Design for reliability across repeated runs, merges, CI failures, and human steering.

## Pattern inventory

### Pattern A — Item selection and fairness

Select one item per run (Autoloop: one program, Goal: one goal issue, Crane: one migration). Bounds run cost, preserves round-robin fairness.

Use a deterministic pre-step scheduler that writes a compact selection artifact (e.g. `/tmp/gh-aw/autoloop.json`; match the file name to your workflow) with: selected item, deferred items, due/not-due flags, existing PR/branch metadata. Do not discover candidates ad hoc in-prompt.

### Pattern B — Canonical branch invariants

Each item owns one stable, deterministic, suffix-free branch (`autoloop/<program>`, `goal/<issue>-<slug>`, `crane/<migration>`). Always use ahead/behind logic against default branch:

- `ahead=0, behind>0`: fast-forward/reset branch to default,
- `ahead>0, behind>0`: merge default into branch,
- else: checkout as-is.

When a force-push is required, use `--force-with-lease` (not `--force`). Keep canonical branches single-writer (the workflow) to minimize push conflicts.

### Pattern C — One PR per item

Each item owns one draft PR; accumulate accepted commits on it over time. Never create multiple active PRs for the same item. Resolve in order: 1) scheduler-provided `existing_pr`, 2) state-file PR fallback, 3) create exactly one PR if none exists.

### Pattern D — Improve → push → gate → accept

Three-phase accept path: 1) metric/contract improvement check, 2) push and wait for CI/checks, 3) accept only on green. Avoids sandbox-only false positives. On failure, discard the change but still record the run.

### Pattern E — CI fix loop with circuit breakers

When CI fails after an improved change: collect failing jobs and error signatures, attempt bounded fix retries, stop on repeated identical signature, pause with structured reason (`ci-fix-exhausted`, `stuck`, `ci-timeout`). Do not retry forever.

### Pattern F — Structured state file

Persist state as markdown in a dedicated repo-memory branch (`memory/autoloop`, `memory/goal`, `memory/crane`), machine-readable and human-editable. Keep a stable layout with: machine-state table (iteration count, last run, best metric, pause/completion fields), current focus/checkpoint, lessons learned, foreclosed avenues/blockers, iteration history (newest first).

### Pattern G — Setup guard and safety rails

Use sentinel-based configuration checks before first real run (Autoloop: `<!-- AUTOLOOP:UNCONFIGURED -->`, Crane: `<!-- CRANE:UNCONFIGURED -->`). If unconfigured, create/refresh setup issue and skip execution.

### Pattern H — Direction-aware metrics

Support both directions: `higher` is better (default), `lower` is better. Use direction in: improvement test, signed delta reporting, target-metric completion check.

### Pattern I — Completion by evidence, not intent

Completion requires explicit evidence gates. Goal enforces issue-defined completion contracts; Crane separates reaching target metric from deterministic completion-gate pass; Autoloop supports target-metric completion with explicit label transition. Never mark complete on belief alone.

### Pattern J — Unified run reporting

Each item has one canonical control-plane issue carrying a durable status comment sentinel (`<!-- ...:STATUS -->`), per-run log comments, and human steering directives. On every run (accepted/rejected/error/blocked): update the status comment, append a per-run summary comment, include run URL, checkpoint, evidence, result, next step. Creates an auditable run narrative.

## Comparative notes by project

| Project | Primary loop unit | Unique strength | Key reusable pattern |
|---|---|---|---|
| Autoloop | Program | General metric-driven optimization with rich iteration memory | Improvement ratchet + CI-gated accept/reject |
| Goal | Goal-labeled issue | Contract-first execution and definition-quality gating | “Needs action” path before implementation |
| Crane | Migration | Milestone plan + strategy selection (`in-place` vs `greenfield`) | iteration 0 planning commit and migration-specific completion gate |

## Implementation blueprint for new loop workflows in gh-aw

Implement in this order:

1. **Define the loop unit** (issue/program/migration/task).
2. **Add scheduler pre-step** that selects one item and emits JSON context.
3. **Define canonical branch and single-PR invariant**.
4. **Add durable state schema** in repo-memory.
5. **Implement run phases**: read state → choose checkpoint → change → verify.
6. **Add accept/reject logic** with direction-aware metric handling.
7. **Gate acceptance on CI/check health**.
8. **Add bounded fix loop** with failure-signature no-progress guard.
9. **Implement completion semantics** with explicit evidence gate.
10. **Add status + per-run issue comments** for observability.
11. **Add pause/recovery policy** for blocked or repeated failures.
12. **Document command-mode overrides** (slash command steering).

## Minimal loop run-state model

Conceptual states: `active`, `accepted`, `rejected`, `error`, `needs_action`, `blocked`, `paused`, `completed`. Transitions must be deterministic and evidence-backed.

## Common failure modes to avoid

- Branch name drift (suffixes/hashes/run IDs)
- Multiple PRs per item
- Marking completion without deterministic evidence
- Repeating the same failed CI signature without pause
- Losing long-term context by storing state only in ephemeral run logs
- Unbounded scope growth per iteration

## Practical guidance for prompt authors

For loop prompts, explicitly require: one checkpoint per run, smallest useful change, explicit evidence command output, explicit `noop`/blocked behavior, state updates every run, strict branch/PR invariants.

Keep prompts short. Move durable policy to state + structured workflow rules.

## Reusable checklist

- [ ] One selected item per run
- [ ] Canonical branch name with no suffix
- [ ] Single draft PR per item
- [ ] Durable state file updated every run
- [ ] Improvement criterion defined (direction-aware)
- [ ] Acceptance gated on CI/checks
- [ ] Fix-loop retry cap and signature-based stop
- [ ] Explicit blocked/paused handling
- [ ] Deterministic completion gate
- [ ] Status comment + per-run comment updated
