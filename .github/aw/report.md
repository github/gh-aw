---
description: Guidelines for creating agentic workflows that generate reports — output type selection, formatting style, and automatic cleanup.
---

# Report Generation

For workflows that generate reports — status updates, audits, summaries — posted as GitHub issues, discussions, or comments.

## Choosing the Output Type

| Use case | Recommended output |
|---|---|
| Report (default) | `create-issue` with `close-older-issues` |
| Inline update on an existing issue or PR | `add-comment` with `hide-older-comments` |
| Discussion-based report (only when explicitly requested) | `create-discussion` with `close-older-discussions` |

Default to `create-issue`. Use `create-discussion` only when the requester explicitly wants threaded async collaboration.

## Automatic Cleanup

- **`expires`** — auto-close after a window (e.g. `7`, `2w`, `1m`).
- **`close-older-issues: true`** — close previous open issues from the same workflow, matched via an embedded workflow-id marker (no `title-prefix` or `labels` needed). Use `close-older-key` for an explicit dedup key instead of the default workflow-id match.
- **`close-older-discussions: true`** — close older matching discussions as "OUTDATED", matched the same way as issues.
- **`hide-older-comments: true`** — minimize previous comments. Useful for rolling status updates.

**Recommended for recurring reports**: `create-issue` with `close-older-issues: true` and a stable `title-prefix`.

```yaml
safe-outputs:
  create-issue:
    title-prefix: "Weekly Status:"
    labels: [report]
    close-older-issues: true
    expires: 30
```

## Scheduled Report Window Scoping

Define the report window explicitly in the prompt so runs are deterministic and comparable.

Window examples:

- `last 24 full hours ending at workflow start (UTC)`
- `last 7 full days ending at workflow start (UTC)`
- `since previous successful run timestamp`
- `current calendar week to date (UTC, Monday 00:00 to now)`

Strategy: fixed durations for trend comparisons, run-based windows for continuous monitoring, calendar windows for stakeholder reporting.

Whenever a report window is defined, also fix its grouping dimensions and deduplication key up front (see [Recurring Digest Defaults](#recurring-digest-defaults) below) — a window alone is not sufficient to make a recurring report deterministic and non-duplicating.

When the window has no qualifying updates, call `noop` with the evaluated window in the message:
`noop("No updates in last 24 full hours ({{window_start_utc}} to {{window_end_utc}})")`

## Recurring Digest Defaults

For recurring PM, stakeholder, and information-worker digests, fix all three elements up front — window, grouping dimensions, and deduplication key — before generating the report:

| Element | Default guidance | Examples |
|---|---|---|
| Report window | Closed, explicit UTC window or `since previous successful run` (see above) | `last 7 full days ending at run start (UTC)`, `previous calendar month (UTC)` |
| Grouping dimensions | Group by the dimensions the audience already uses to decide | team, area, milestone, owner, severity, status, repository |
| Deduplication key | One stable key per scope and window; week-based for weekly, calendar-date for daily/monthly | `pm-digest:platform:2026-W27`, `stakeholder-digest:mobile:2026-07-02` |

Duplicate-suppression: search for an existing open issue by the stable key (title prefix or dedicated label) before creating; if one exists, update it with `add-comment` instead of opening a duplicate. Use `create-issue` with `close-older-issues: true` for recurring issue-style digests.

## Fallback for Incomplete Metadata

When the digest or report depends on labels, metadata, or classification fields (for example customer-impact labels, priority tiers, team assignments, or area tags) that are absent or inconsistent:

- summarize what data *was* available and note which fields are missing
- group by the next-best available dimension (for example repository, author, date, or milestone)
- use an explicit "Unclassified" bucket for items without required metadata — do not invent or assume classifications
- call `noop` only when the reporting window itself has zero events; missing metadata alone is not a reason to skip the report

## Report Style and Structure

### Header Levels

- Use `###` (h3) for main sections — e.g., `### Test Summary`
- Use `####` (h4) for subsections — e.g., `#### Device-Specific Results`
- Never use `##` (h2) or `#` (h1) — those are reserved for titles

### Progressive Disclosure

Wrap verbose logs, secondary info, and per-item breakdowns in `<details><summary>Section Name</summary>`. Keep summary, critical issues, and key metrics visible.

### Alerts Instead of Emojis

- `> [!NOTE]` — neutral status
- `> [!WARNING]` — warnings
- `> [!CAUTION]` — high-risk or blocking

Do not use emoji severity markers (`✅`, `⚠️`, `❌`, `🧪`).

### Structure Pattern

1. **Overview** — 1–2 paragraphs of key findings
2. **Critical info** — summary stats, critical issues (always visible)
3. **Details** — `<details><summary>...</summary>` for expanded content
4. **Context** — workflow run, date, trigger

### Example Report Structure

```markdown
### Summary
- Key metric 1: value
- Key metric 2: value

> [!WARNING]
> Status: degradation detected in one or more checks.

### Critical Issues
[Always visible - these are important]

<details>
<summary>View Detailed Results</summary>

[Comprehensive details, logs, traces]

</details>

<details>
<summary>View All Warnings</summary>

[Minor issues and potential problems]

</details>

### Recommendations
[Actionable next steps - keep visible]
```

## Workflow Run References

- Format run IDs as links: `[§12345](https://github.com/owner/repo/actions/runs/12345)`
- Include up to 3 most relevant run URLs at the end under `**References:**`
- Do NOT add footer attribution — the system appends it automatically

## Avoiding Mentions and Backlinks

Without filtering, `@username` notifies users and `#123` creates backlinks every run.

- **`mentions: false`** — escapes all `@mentions`.
- **`allowed-github-references: []`** — escapes `#123` / `owner/repo#123`.
- **`max-bot-mentions: 0`** — neutralizes bot-trigger phrases like `fixes #123` / `closes #456`.

```yaml
safe-outputs:
  mentions: false
  allowed-github-references: []
  max-bot-mentions: 0
  create-issue:
    title-prefix: "Weekly Status:"
    labels: [report]
    close-older-issues: true
    expires: 30
```

Applies globally to all safe-output types (issues, comments, discussions).
