---
description: Worked patterns for stateful agentic workflows — baseline metric comparison with cache-memory, and "alert on new findings" scanning with repo-memory.
---

# Stateful Memory Patterns

Worked examples for the two most common persistent-state patterns. Tool decision guide and configuration reference: [memory.md](memory.md).

## Baseline Comparison (cache-memory)

Persist a baseline metric with `cache-memory` and alert on regression — test coverage, build duration, benchmark scores, audit counts. Requires runs at least every 7 days (default cache retention) and tolerance for losing a baseline: the next run simply re-establishes it.

If a lost baseline would cause serious side-effects — e.g. a security-finding baseline where "cache miss" floods the repo with duplicate issues — use `repo-memory` (see below).

**Worked example: coverage delta on every PR**

```markdown
---
description: Post a PR comment when test coverage drops by more than 1 percentage point
on:
  pull_request:
    types: [opened, synchronize]
permissions:
  pull-requests: read
  contents: read
engine: copilot
tools:
  github:
    toolsets: [pull_requests]
  cache-memory: true
safe-outputs:
  add-pr-comment:
    max: 1
timeout-minutes: 15
---

Run the test suite and collect the overall line-coverage percentage as a
single float (e.g. `82.5`).

Load `/tmp/gh-aw/cache-memory/coverage-baseline.json` if it exists.
The file stores: `{ "coverage": 82.5, "updated": "2026-05-01-09-00-00" }`.

**First run** (file missing): write the current coverage to the file and use
the `noop` safe output — no comment is needed yet.

**Subsequent runs** (baseline found): compute `delta = current − baseline`.

- If `delta >= −1.0` (coverage held or improved), use the `noop` safe output.
- If `delta < −1.0` (coverage fell by more than 1 pp), post an `add-pr-comment`
  that includes baseline coverage, current coverage and delta (e.g. "82.5% →
  79.3% (−3.2 pp)") plus which files lost the most coverage.

Regardless of the outcome, overwrite `/tmp/gh-aw/cache-memory/coverage-baseline.json`
with the current coverage and a filesystem-safe timestamp `YYYY-MM-DD-HH-MM-SS`
(no colons, no `T`, no `Z`).
```

**Key design decisions**

- **`cache-memory` not `repo-memory`** — coverage deltas are short-lived quality gates; a cache miss just means "no comparison this run" and the baseline is silently refreshed — no false-positive flood
- **First-run handling** — treat a missing baseline as "no data yet": write it and skip the comparison; the second run is the first real gate
- **Threshold guard** — ignore sub-1 pp fluctuations to reduce noise; tune the threshold to your team's standards
- **Filename safety** — use `YYYY-MM-DD-HH-MM-SS` (no colons) in any timestamped filenames written to `cache-memory` (artifacts reject colons; see [memory.md](memory.md#filename-safety))

## Stateful Scanning (repo-memory)

Use `repo-memory` to persist a baseline JSON file between scheduled runs so the workflow only alerts on *new* findings — vulnerability scans, dependency audits, licence checks, or any "track changes over time" scenario.

**Worked example: nightly npm vulnerability scan**

```markdown
---
description: Nightly npm vulnerability scan — alerts only on new advisories
on:
  schedule:
    - cron: "0 2 * * *"
permissions:
  issues: write
  contents: read
engine: claude
tools:
  repo-memory:
    allowed-extensions: [".json"]
network:
  allowed:
    - registry.npmjs.org
safe-outputs:
  create-issue:
    title-prefix: "[vuln] "
    labels: [security, automated]
    max: 5
timeout-minutes: 20
---

Load `/tmp/gh-aw/repo-memory/default/vuln-baseline.json`.
If missing, treat the baseline as `[]` (first run).

Run `npm audit --json`. Collect each advisory's id, severity, title, and URL.

Diff against the baseline:
- **New** (in current, not in baseline) → open a `create-issue` per finding (max 5).
- **Resolved** (in baseline, not in current) → log only.
- If no new findings, use the `noop` safe output.

Write the current advisory IDs to `/tmp/gh-aw/repo-memory/default/vuln-baseline.json` as a JSON array.
```

**Key design decisions**

- **`repo-memory` for baselines, not `cache-memory`** — caches expire after 7 days; a lost baseline makes every known finding appear "new" on the next run, flooding the repo with duplicate issues
- **First-run handling** — treat a missing baseline file as `[]` and write it at the end of the first run, giving subsequent runs a clean starting point
- **`max:` flood guard** — caps issues opened per run; use `max: 5` for nightly scans, `max: 1` for secret alerts, `max: 10` for weekly audits
- **Engine restriction** — `repo-memory` requires Claude or a custom engine; it is **not available** for the Copilot engine
- **Baseline schema** — store only stable identifiers (advisory ID strings), not mutable fields like severity, to avoid false "new" alerts when metadata changes
