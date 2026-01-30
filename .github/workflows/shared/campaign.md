---
# Shared safe-outputs defaults for campaign orchestator.
# Intended to be included via frontmatter `imports:`.
# Workflows may override any specific safe-output type by defining it at top-level.

safe-outputs:
  update-project:
    max: 100
  create-project-status-update:
    max: 1
  create-issue:
    max: 5
---
# Campaign Orchestrator Core Rules

These are generic orchestrator rules.

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

## No-Work Default

If discovery finds **no** work items to process:

- If the campaign uses a GitHub Project, post exactly one `create_project_status_update` with status `INACTIVE`.
- Then call `noop` with a short message and end the run.

## Project Status Updates (Default)

If the campaign uses a GitHub Project, post exactly **one** `create_project_status_update` per run.

- `status`: use `INACTIVE` when no work was found; otherwise prefer `ON_TRACK` (or `AT_RISK` if partial failures).
- `start_date`: today (YYYY-MM-DD)
- `body`: include the discovery query, counts (found / updated / created), and next steps

## Authority

- If any campaign instructions conflict with Project update instructions, Project update instructions win for project writes.

## Project Field Defaults (When Using GitHub Projects)

If the campaign uses a GitHub Project to track state, use these as **defaults** for `update_project` writes.

Notes:
- These are defaults. A specific workflow may override them.
- Only set fields that exist in the target Project schema; omit unknown fields.

Defaults (recommended field keys):

- `campaign_id`: derive from the workflow's **Campaign ID** in its Config section.
- `target_repo`: derive from the workflow's **Target repo** in its Config section.
- `worker_workflow`: set to the discovery source (e.g. the orchestrator/workflow name or the system that surfaced the item).

Status defaults:

- If the workflow is tracking **open** work items, set `status` to an active backlog state (commonly `"Todo"`).
- If tracking **completed** work items (merged PRs / closed issues), set `status` to a done state (commonly `"Done"`).

Optional, best-effort fields (only if the Project has them):

- `priority`: High/Medium/Low
- `size`: Small/Medium/Large
- `start_date`: YYYY-MM-DD
