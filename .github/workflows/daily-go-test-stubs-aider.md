---
private: true
emoji: "🧪"
description: Weekly assertion-based Go tests that must measurably improve production coverage before opening a PR
on:
  schedule: weekly
  workflow_dispatch:
  skip-if-match: 'is:pr is:open in:body "gh-aw-tracker-id: daily-go-test-stubs-aider"'
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
sandbox:
  agent:
    id: awf
tracker-id: daily-go-test-stubs-aider
engine:
  id: aider
model: copilot/auto
strict: true
network:
  allowed: []
runtimes:
  go:
    version: "1.27.3"
tools:
  edit:
  bash:
    - "*"
safe-outputs:
  create-pull-request:
    max: 1
    allowed-files:
      - "pkg/**/aider_coverage_test.go"
    title-prefix: "[aider] "
    labels: [automation, testing]
    draft: false
  missing-tool:
timeout-minutes: 30
imports:
  - shared/aider.md
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: true
---

# Daily Go Test Stubs — Aider

Improve real Go behavior coverage, not the number of test files. The legacy workflow
name/path is retained for continuity. Aider has no MCP client; use the `safeoutputs`
MCP CLI. Use one shell command per line, no heredocs, and SEARCH/REPLACE blocks for edits.
Never create skipped tests, placeholders, empty tests, or tests that merely call code
without checking its result. A quiet run is better than a low-value PR.

## Step 1 — Measure a bounded coverage gap

No files need to be attached. Your first reply must run this exact command.
It inspects at most three small, non-generated library packages, prefers recent
changes, records a fresh baseline, and prints the selected production source,
coverage by function, and existing tests. It does not generate tests.
An existing open PR from this workflow suppresses further work until reviewed.

```bash
python3 scripts/aider_go_test_coverage.py discover
```

Consume its output before editing. A discovery/test/tool failure is incomplete,
not evidence that coverage is complete. Do not retry with insecure TLS settings.

## Step 2 — Add one useful test file

Choose an uncovered function whose behavior is clear from the printed source.
Prefer a branch returning a literal string, boolean, or integer: the publication
gate must prove the new tests detect an incorrect return value.
In exactly the printed `TARGET` file, write real tests with explicit expected
results, including a normal case and an error/boundary case where applicable.
Use the actual package declaration and existing assertion style. Tests should
catch an incorrect return value or branch condition, not just execute a function.
Do not change production code, existing tests, modules, workflow files, skills,
or the helper. Do not rerun discovery or modify its baseline.

## Step 3 — Verify the improvement and publish only that file

After adding tests, run this command. It formats only the target file, runs the
whole selected package's tests without the test cache, and requires strictly
more covered production statements with no lost baseline coverage. It rejects
skips, placeholders, missing assertions, existing-file edits, and unrelated
files. It also checks up to three mutations of newly covered literal returns
using temporary Go compiler overlays. At least one mutation must fail an assertion
in the new tests alone, not just an existing test or compilation. Production files
are never edited by these probes. Coverage without this behavioral witness is
insufficient. Only the verified new test file is staged, never Aider history or skills.
The PR includes measured before/after coverage; no speculative coverage claims.

```bash
python3 scripts/aider_go_test_coverage.py publish || safeoutputs report_incomplete --reason "Could not verify a real coverage gain or publish the focused test improvement; no pull request confirmed."
```

Only if discovery succeeded with `NO_CANDIDATE`, or source inspection establishes
no useful bounded assertion-based test, run instead (do not claim every package
is covered):

```bash
safeoutputs noop --message "No useful bounded coverage improvement found; no placeholder PR created."
```

## Step 4 — Exit rule

**Always** invoke exactly one `safeoutputs` CLI command before finishing.
Use `report_incomplete` for failed discovery, failed tests, unchanged coverage,
or unfinished work. Never publish a partial improvement or fix a gate by weakening it.