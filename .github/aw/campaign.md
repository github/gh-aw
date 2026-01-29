# Campaign Orchestrator Core Rules

These are generic orchestrator rules intended to be included via `{{#runtime-import ...}}`.

## Operating Model

- The orchestrator coordinates a single campaign: discover state, decide deterministically, apply minimal writes, and report.
- Delegate repo/code changes (PRs, commits) to worker workflows unless the campaign explicitly grants direct repo authority.
- The GitHub Project board (when used) is the authoritative campaign state; do not invent state.

## Non-Negotiables

- Separate **reads** and **writes**. Do all discovery first, then perform all writes.
- Be deterministic and idempotent: safe to re-run with the same inputs.
- Minimize API calls; enforce strict pagination budgets.
- Prefer incremental discovery over full rescans.
- If throttled (HTTP 429 / rate-limit 403), back off and end the run after reporting what remains.

## Budgets & Pacing

- Enforce page and item budgets strictly; stop early and defer remaining work to the next run.
- Use stable ordering in discovery (e.g., `updatedAt` with a deterministic tiebreak like ID/number).
- Never “catch up” by expanding scope or blowing budgets.

## Repo-Memory Cursor & Metrics

If this campaign uses repo-memory:

- **Cursor file path**: `/tmp/gh-aw/repo-memory/campaigns/<campaign_id>/cursor.json`
  - If it exists: read first and continue from its boundary.
  - If it does not exist: create it by end of run.
  - Always write the updated cursor back to the same path.

- **Metrics snapshots path**: `/tmp/gh-aw/repo-memory/campaigns/<campaign_id>/metrics/*.json`
  - Write **one new** append-only JSON snapshot per run (do not rewrite history).
  - Use UTC date in the filename (example: `metrics/<YYYY-MM-DD>.json`).

## Correlation & Status Mapping

- Correlation must be explicit and stable (e.g., tracker-id plus labels); avoid fuzzy matching.
- Determine status only from explicit GitHub state:
  - Open → active backlog state (e.g., `Todo`)
  - Closed (issue/discussion) → `Done`
  - Merged (PR) → `Done`

## Execution Phases (Required Order)

1. Read state (discovery) — NO WRITES
2. Decide (planning) — NO WRITES
3. Apply updates (write phase) — WRITES
4. Dispatch workers (optional)
5. Report

## Writes (Safe-Outputs Only)

- Use only allowlisted safe outputs.
- Keep writes deterministic and minimal.
- Do not interleave reads and writes.

## Reporting

Always report:

- Discovered counts (by type)
- Processed counts (by action: add/status_update/backfill/noop/failed)
- Deferred counts (due to budgets)
- Failures (with reasons)
- Whether cursor was advanced and where the next run should resume

## Authority

- If any campaign instructions conflict with Project update instructions, Project update instructions win for project writes.
