---
private: true
emoji: "🧪"
name: Test Quality Sentinel
description: Analyzes test quality beyond code coverage percentages on every PR, detecting implementation-detail tests, happy-path-only tests, test inflation, and duplication
on:
  pull_request:
    types: [ready_for_review]
  slash_command:
    strategy: centralized
    name: review
    events: [pull_request_comment, pull_request_review_comment]
permissions:
  contents: read
  pull-requests: read
  copilot-requests: write
model: "${{ needs.activation.outputs.model_size }}"
engine:
  id: copilot
  max-continuations: 15
cache:
  key: pr-test-prefetch-${{ github.event.pull_request.head.sha || github.event.issue.number }}
  path: /tmp/gh-aw/agent
  restore-keys:
    - pr-test-prefetch-${{ github.event.pull_request.number || github.event.issue.number }}-
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
  bash:
    - "git diff:*"
    - "grep:*"
    - "cat:*"
    - "echo:*"
steps:
  - name: Pre-fetch PR data
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      PR_NUMBER: ${{ github.event.issue.number || github.event.pull_request.number }}
      PR_HEAD_SHA: ${{ github.event.pull_request.head.sha }}
      EXPR_GITHUB_EVENT_PULL_REQUEST_BASE_SHA: ${{ github.event.pull_request.base.sha }}
    run: |
      set -uo pipefail
      mkdir -p /tmp/gh-aw/agent
      write_prefetch_fallback() {
        local reason="$1"
        echo "::warning::Test Quality Sentinel pre-fetch unavailable: ${reason}"
        printf '%s\n' "$reason" > /tmp/gh-aw/agent/test-prefetch-unavailable.txt
        printf '{}\n' > /tmp/gh-aw/agent/pr-meta.json
        for file in /tmp/gh-aw/agent/test-files.txt \
                    /tmp/gh-aw/agent/test-diff.txt \
                    /tmp/gh-aw/agent/diff-numstat.txt \
                    /tmp/gh-aw/agent/go-new-test-funcs.txt \
                    /tmp/gh-aw/agent/js-new-test-funcs.txt \
                    /tmp/gh-aw/agent/go-modified-test-funcs.txt \
                    /tmp/gh-aw/agent/js-changed-test-files.txt \
                    /tmp/gh-aw/agent/missing-build-tags.txt \
                    /tmp/gh-aw/agent/go-test-stats.txt \
                    /tmp/gh-aw/agent/js-test-stats.txt \
                    /tmp/gh-aw/agent/go-testmain-funcs.txt \
                    /tmp/gh-aw/agent/go-goleak-entries.txt; do
          : > "$file"
        done
        rm -f /tmp/gh-aw/agent/changed-files.txt /tmp/gh-aw/agent/test-diff-full.txt
        rm -f /tmp/gh-aw/agent/test-data-head-sha.txt
      }
      CURRENT_HEAD_SHA="${PR_HEAD_SHA:-}"
      if [ -z "$CURRENT_HEAD_SHA" ]; then
        CURRENT_HEAD_SHA=$(gh pr view "$PR_NUMBER" --json headRefOid --jq '.headRefOid' 2>/dev/null || true)
      fi
      CACHE_HEAD_SHA=""
      if [ -f /tmp/gh-aw/agent/test-data-head-sha.txt ]; then
        CACHE_HEAD_SHA="$(tr -d '\n' < /tmp/gh-aw/agent/test-data-head-sha.txt)"
      fi
      if [ -n "$CURRENT_HEAD_SHA" ] && [ "$CURRENT_HEAD_SHA" = "$CACHE_HEAD_SHA" ] && \
         [ -f /tmp/gh-aw/agent/pr-meta.json ] && \
         [ -f /tmp/gh-aw/agent/test-files.txt ] && \
         [ -f /tmp/gh-aw/agent/test-diff.txt ] && \
         [ -f /tmp/gh-aw/agent/diff-numstat.txt ]; then
        echo "Cache hit: using pre-fetched test data for head ${CURRENT_HEAD_SHA}"
        rm -f /tmp/gh-aw/agent/test-prefetch-unavailable.txt
        exit 0
      fi

      # PR metadata
      if ! gh pr view "$PR_NUMBER" \
        --json files,additions,deletions,baseRefName,headRefName,headRefOid \
        > /tmp/gh-aw/agent/pr-meta.json; then
        write_prefetch_fallback "unable to fetch PR metadata"
        exit 0
      fi

      # List of changed test files
      if ! gh pr diff "$PR_NUMBER" --name-only > /tmp/gh-aw/agent/changed-files.txt; then
        write_prefetch_fallback "unable to fetch PR file list"
        exit 0
      fi
      grep -E '(_test\.go|\.test\.cjs|\.test\.js)$' \
        /tmp/gh-aw/agent/changed-files.txt > /tmp/gh-aw/agent/test-files.txt || true

      # Diff for test files only; capped at 40 KB to control cache token costs
      if [ -s /tmp/gh-aw/agent/test-files.txt ]; then
        # shellcheck disable=SC2046
        if ! gh pr diff "$PR_NUMBER" \
          -- $(tr '\n' ' ' < /tmp/gh-aw/agent/test-files.txt) \
          > /tmp/gh-aw/agent/test-diff-full.txt 2>/dev/null; then
          write_prefetch_fallback "unable to fetch test file diff"
          exit 0
        fi
        head -c 40000 /tmp/gh-aw/agent/test-diff-full.txt > /tmp/gh-aw/agent/test-diff.txt
        rm -f /tmp/gh-aw/agent/test-diff-full.txt
      else
        touch /tmp/gh-aw/agent/test-diff.txt
      fi

      git diff "$EXPR_GITHUB_EVENT_PULL_REQUEST_BASE_SHA...HEAD" --numstat \
        > /tmp/gh-aw/agent/diff-numstat.txt 2>/dev/null || true

      # Extract new/modified test function signatures from the diff
      if [ -s /tmp/gh-aw/agent/test-diff.txt ]; then
        # Behavioral test functions only — exclude TestMain (infrastructure, not a test case)
        grep -E "^\+func Test" /tmp/gh-aw/agent/test-diff.txt \
          | grep -v "^\+func TestMain\b" \
          > /tmp/gh-aw/agent/go-new-test-funcs.txt || true
        # Modified Go behavioral test functions (hunk headers catch body-only edits)
        grep -E "^@@ .*func Test" /tmp/gh-aw/agent/test-diff.txt \
          | grep -v "func TestMain\b" \
          > /tmp/gh-aw/agent/go-modified-test-funcs.txt || true
        # TestMain infrastructure entries (separate from behavioral tests; include body-only edits)
        {
          grep -E "^\+func TestMain\b" /tmp/gh-aw/agent/test-diff.txt || true
          grep -E "^@@ .*func TestMain\b" /tmp/gh-aw/agent/test-diff.txt || true
        } \
          > /tmp/gh-aw/agent/go-testmain-funcs.txt || true
        # Goroutine-leak guard detection (goleak.VerifyTestMain calls)
        grep -E "goleak\.VerifyTestMain" /tmp/gh-aw/agent/test-diff.txt \
          > /tmp/gh-aw/agent/go-goleak-entries.txt || true
        grep -E "^\+(it|test|describe)\(" /tmp/gh-aw/agent/test-diff.txt \
          > /tmp/gh-aw/agent/js-new-test-funcs.txt || true
        # Any changed JS test file disqualifies infrastructure-only mode
        grep -E '(\.test\.cjs|\.test\.js)$' /tmp/gh-aw/agent/test-files.txt \
          > /tmp/gh-aw/agent/js-changed-test-files.txt || true
        # Check for new Go test files missing mandatory build tags
        git diff "$EXPR_GITHUB_EVENT_PULL_REQUEST_BASE_SHA...HEAD" \
          --diff-filter=A --name-only 2>/dev/null \
          | grep '_test\.go$' | while read -r f; do
            if ! head -1 "$f" | grep -qE '^//go:build'; then
              echo "MISSING BUILD TAG: $f"
            fi
          done > /tmp/gh-aw/agent/missing-build-tags.txt || true
        # Go test structural stats (assertions, error checks, table-driven, forbidden mocks)
        # TestMain is excluded: it is infrastructure, not a behavioral test case
        awk '
          /^\+func Test/ {
            if (test_name) print test_name, "assertions=" assertions, "errors=" errors, "table_driven=" table_driven, "forbidden_mocks=" forbidden_mocks
            match($0, /func (Test[^(]+)/, arr); test_name=arr[1]; assertions=0; errors=0; table_driven=0; forbidden_mocks=0
            if (test_name == "TestMain") test_name=""
          }
          test_name && /^\+.*(assert\.|require\.)/ { assertions++ }
          test_name && /^\+.*t\.(Error|Errorf|Fatal|Fatalf)\(/ { assertions++; errors++ }
          test_name && /^\+.*(assert\.Error|require\.Error|assert\.NoError|require\.NoError)/ { errors++ }
          test_name && /^\+.*t\.Run\(/ { table_driven++ }
          test_name && /^\+.*(gomock\.|testify\/mock|\.EXPECT\(\)|\.On\(|\.Return\()/ { forbidden_mocks++ }
          test_name && /^\+\}$/ { print test_name, "assertions=" assertions, "errors=" errors, "table_driven=" table_driven, "forbidden_mocks=" forbidden_mocks; test_name="" }
          END { if (test_name) print test_name, "assertions=" assertions, "errors=" errors, "table_driven=" table_driven, "forbidden_mocks=" forbidden_mocks }
        ' /tmp/gh-aw/agent/test-diff.txt > /tmp/gh-aw/agent/go-test-stats.txt || true
        # JS test structural stats (assertions, error matchers, vi.* mocks)
        awk '
          /^\+(it|test)\(/ {
            if (test_name) print test_name, "assertions=" assertions, "errors=" errors, "mocks=" mocks
            match($0, /(it|test)\(["\047]([^"\047]+)/, arr); test_name=arr[2]; assertions=0; errors=0; mocks=0
          }
          test_name && /^\+.*expect\(/ { assertions++ }
          test_name && /^\+.*(\.toThrow|\.rejects|\.toThrowError)/ { errors++ }
          test_name && /^\+.*(vi\.mock|vi\.spyOn|vi\.fn)/ { mocks++ }
          test_name && /^\+\}\)/ { print test_name, "assertions=" assertions, "errors=" errors, "mocks=" mocks; test_name="" }
          END { if (test_name) print test_name, "assertions=" assertions, "errors=" errors, "mocks=" mocks }
        ' /tmp/gh-aw/agent/test-diff.txt > /tmp/gh-aw/agent/js-test-stats.txt || true
      else
        touch /tmp/gh-aw/agent/go-new-test-funcs.txt \
              /tmp/gh-aw/agent/js-new-test-funcs.txt \
              /tmp/gh-aw/agent/go-modified-test-funcs.txt \
              /tmp/gh-aw/agent/js-changed-test-files.txt \
              /tmp/gh-aw/agent/missing-build-tags.txt \
              /tmp/gh-aw/agent/go-test-stats.txt \
              /tmp/gh-aw/agent/js-test-stats.txt \
              /tmp/gh-aw/agent/go-testmain-funcs.txt \
              /tmp/gh-aw/agent/go-goleak-entries.txt
      fi
      if [ -z "$CURRENT_HEAD_SHA" ]; then
        CURRENT_HEAD_SHA="$(jq -r '.headRefOid // empty' /tmp/gh-aw/agent/pr-meta.json)"
      fi
      if [ -n "$CURRENT_HEAD_SHA" ]; then
        printf '%s\n' "$CURRENT_HEAD_SHA" > /tmp/gh-aw/agent/test-data-head-sha.txt
      else
        rm -f /tmp/gh-aw/agent/test-data-head-sha.txt
      fi
      rm -f /tmp/gh-aw/agent/test-prefetch-unavailable.txt

      echo "Pre-fetched $(grep -c . /tmp/gh-aw/agent/test-files.txt || echo 0) test files"
safe-outputs:
  add-comment:
    max: 1
    hide-older-comments: true
  submit-pull-request-review:
    max: 1
  noop:
  messages:
    footer: "> 🧪 *Test quality analysis by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🔬 [{workflow_name}]({run_url}) is analyzing test quality on this {event_type}..."
    run-success: "✅ [{workflow_name}]({run_url}) completed test quality analysis."
    run-failure: "❌ [{workflow_name}]({run_url}) {status} during test quality analysis."
timeout-minutes: 15
imports:
  - shared/reporting.md
  - shared/otlp.md
features:
  gh-aw-detection: true
experiments:
  model_size:
    variants: [claude-haiku-4.5, claude-sonnet-5]
    description: "Tests whether a smaller model can preserve test-review decision quality at lower cost versus a larger reasoning-capable model."
    hypothesis: "H0: model-size variant does not improve review usefulness acceptance rate. H1: a larger reasoning-capable model improves review usefulness acceptance rate by >=15 percentage points without materially increasing false-positive change requests."
    metric: review_usefulness_acceptance_rate
    secondary_metrics: [ai_credits_total, false_positive_change_request_rate]
    guardrail_metrics:
      - name: false_positive_change_request_rate
        threshold: "<=0.20"
      - name: run_success_rate
        threshold: ">=0.90"
    min_samples: 70
    weight: [50, 50]
    start_date: "2026-07-05"
    issue: 43530
evals:
  - id: model_size_goal_met
    question: Does the agent output show that the objective for experiment model_size was successfully completed?

---

# Test Quality Sentinel 🧪

You are the Test Quality Sentinel. Analyze new and changed tests in this PR to produce a **Test Quality Score** (0–100) and flag tests that create false comfort without genuine behavioral coverage.

- **Repository**: ${{ github.repository }}
- **Pull Request**: #${{ github.event.pull_request.number }} — "${{ github.event.pull_request.title }}"
- **Actor**: ${{ github.actor }}

High test counts can create an illusion of safety. The real signal is whether tests cover behavioral contracts and design invariants — not just happy-path implementations.

## Step 1: Load Pre-fetched PR Data and Identify Test Files

First check whether `/tmp/gh-aw/agent/test-prefetch-unavailable.txt` exists. If it exists, read the reason from that file and immediately call `noop` with the reason:

```json
{"noop": {"message": "Test Quality Sentinel skipped because pre-fetch PR data was unavailable: <reason>"}}
```

Do not analyze the empty fallback files when this marker exists.

PR data has already been fetched before the agent started. Read from:

- `/tmp/gh-aw/agent/pr-meta.json` — PR metadata (files, additions, deletions, branch names)
- `/tmp/gh-aw/agent/test-files.txt` — list of changed test files
- `/tmp/gh-aw/agent/test-diff.txt` — diff for test files only _(capped at 40 KB; if truncated, prioritize newly added `func Test*` functions)_
- `/tmp/gh-aw/agent/diff-numstat.txt` — numstat for all changed files
- `/tmp/gh-aw/agent/go-testmain-funcs.txt` — newly added `TestMain` infrastructure entries _(separate from behavioral tests)_
- `/tmp/gh-aw/agent/go-goleak-entries.txt` — `goleak.VerifyTestMain` calls added in this PR _(goroutine-leak guard signal)_

Then identify all **new and modified test files** in the diff:

- **Go** *(analyzed)*: files ending in `_test.go` with `func Test*` functions; both `//go:build !integration` (unit) and `//go:build integration` files are analyzed
- **JavaScript** *(analyzed)*: the primary format is `*.test.cjs` (co-located with source in `actions/setup/js/`); also `*.test.js` (scripts); test framework is **vitest** (not jest)
- **Other languages** *(detected but not scored)*: Python (`test_*.py`, `*_test.py`), Rust (`#[test]` blocks). Note their presence in the report but exclude them from scoring.

If **no test files were added or modified**, call `noop`:

```json
{"noop": {"message": "No test files were added or modified in this PR. Test Quality Sentinel skipped."}}
```

Otherwise, collect the list of changed test files and their diffs.

### Step 2: Extract Test Functions

For each changed test file, extract the individual test functions / test cases that were **added or modified** (not just context lines).

For each test, collect:
- **Test name / identifier**
- **Test body** (assertions, setup, mocking calls)
- **File path and approximate line number**

New Go behavioral test function signatures (lines matching `+func Test*`, excluding `TestMain`) are pre-extracted to `/tmp/gh-aw/agent/go-new-test-funcs.txt`. Modified Go behavioral tests detected from hunk headers (`@@ ... func Test...`) are in `/tmp/gh-aw/agent/go-modified-test-funcs.txt`. New JavaScript test blocks (`it(`, `test(`, `describe(`) are in `/tmp/gh-aw/agent/js-new-test-funcs.txt`. Changed JS test files are in `/tmp/gh-aw/agent/js-changed-test-files.txt`. Use these as a starting point, then read `test-diff.txt` for full function bodies.

**Infrastructure vs. behavioral**: `TestMain(m *testing.M)` entries in `go-testmain-funcs.txt` are test infrastructure, not behavioral test cases. Do not score them as test functions. Instead, note them separately in the report and credit any `goleak.VerifyTestMain` usage (see `go-goleak-entries.txt`) as a goroutine-leak design invariant.

If all of the following hold, this is an **infrastructure-only PR** — see the scoring guidance in Step 6:
- `go-new-test-funcs.txt` is empty
- `go-modified-test-funcs.txt` is empty
- `js-new-test-funcs.txt` is empty
- `js-changed-test-files.txt` is empty
- `go-testmain-funcs.txt` is non-empty

Also check `/tmp/gh-aw/agent/missing-build-tags.txt` — any newly added Go test files missing the mandatory `//go:build` tag on line 1 are listed there.

### Step 3: Structural Stats

Read the pre-computed stats from the pre-fetch step:

- Go: `cat /tmp/gh-aw/agent/go-test-stats.txt` — per-function counts: `assertions`, `errors`, `table_driven`, `forbidden_mocks`
- JS: `cat /tmp/gh-aw/agent/js-test-stats.txt` — per-test counts: `assertions`, `errors`, `mocks`

Use these counts directly in Step 4 classification. Accepted signals:
- **Assertions**: Go (`assert.*`, `require.*`, `t.Error*`), JS (`expect(...).to*`)
- **Error coverage**: explicit error assertions (`assert.Error`, `.toThrow`, `.rejects`, etc.)
- **Table-driven credit (Go)**: credit each `t.Run(...)` subtest row; use `table_driven` count from stats
- **Mocking policy**: external-I/O/runtime mocks are acceptable; `forbidden_mocks > 0` in Go stats is a hard violation
- **Assertion-message policy (Go)**: flag assertions without descriptive failure context

### Step 4: AI Quality Review of Each Test

For each new/modified test from Step 2, classify with this compact rubric:

| Question | Classify as |
|---|---|
| **Design invariant** — what guarantee does the test enforce? | `behavioral_contract` / `implementation_detail` / `unknown` |
| **Value if deleted** — what regression would escape? | `high_value` / `low_value` / `duplicated` |
| **Contract vs implementation** — what does it mostly verify? | `design_test` / `implementation_test` |

Red flags (mark **suspicious** when present):
1. JS mock-heavy test with no observable behavior assertion (internal-call assertions only)
2. Go mock libraries (`gomock`, `testify/mock`, `.EXPECT()`, `.On()`) — hard violation
3. New Go `*_test.go` missing line-1 build tag (`//go:build !integration` or `//go:build integration`) — hard violation
4. Happy-path only (no error/edge assertions)
5. Test inflation (test:prod added lines > 2:1)
6. Duplicated assertion patterns across 3+ tests
7. No assertions
8. Go assertion lacks descriptive failure context

**Goroutine-leak guards**: `TestMain` with `goleak.VerifyTestMain(m)` enforces that no goroutines are leaked after each test run. Classify each such entry as `behavioral_contract`, `high_value`, `design_test` — this is a package-level design invariant that protects all future tests. Do not penalize its absence of traditional assertions. If `go-goleak-entries.txt` is non-empty, note it as a positive quality signal in the report.

### Act-vs-noop rubric

Decide whether there is a reviewable signal before drafting a report. Do not use `noop` to avoid reporting a concrete red flag, and do not manufacture a finding merely because a test changed.

| Evidence after the scoped review | Required outcome |
|---|---|
| Pre-fetch unavailable, no changed test files, or only unsupported-language tests | Call `noop` immediately with the specific scope reason; do not post a report. |
| Recognized test files changed but no behavioral test case changed and no hard violation exists | Call `noop` with the specific scope reason; do not post a report. |
| A hard violation exists, or the implementation-test ratio exceeds 30% | Act: include each concrete file, test, and evidence in the report, then request changes. |
| A red flag exists but does not meet the failure threshold | Act: flag only the evidenced test in the report; do not escalate a speculative concern. |
| Behavioral tests were reviewed and no red flags or violations exist | Act: report the clean result and approve; a clean review is not a reason to invent a finding. |

Scope for this step:
- Analyze only new/changed Go (`*_test.go`) and JavaScript (`*.test.cjs`, `*.test.js`) tests; note other languages without scoring.
- Treat Go mocking with `gomock`, `testify/mock`, `.EXPECT()`, or `.On()` as a hard violation.
- JavaScript vitest mocks for external I/O are acceptable unless business logic is mocked without output assertions.
- Do not score `TestMain(m *testing.M)` entries as behavioral tests; they are infrastructure.

## Step 5: Count Lines in Test Files vs. Production Files

Calculate the test inflation ratio for each changed test file using the pre-fetched `/tmp/gh-aw/agent/diff-numstat.txt`.

For each **Go and JavaScript** test file, find the corresponding production file and compare the ratio of lines added:

- `foo_test.go` → `foo.go`
- `foo.test.cjs` → `foo.cjs` (primary in `actions/setup/js/`)
- `foo.test.js` → `foo.js` (used in `scripts/`)

If the ratio of new lines added to the test file vs. the production file exceeds 2:1, flag it as potential **test inflation**. Inflation scoring does not apply to `TestMain`-only files (pure infrastructure).

## Step 6: Calculate Test Quality Score

Compute **Test Quality Score** (0–100):

```
score = ((design_tests / total_new_tests) * 40) +
        ((tests_with_edge_cases / total_new_tests) * 30) +
        (20 - min(duplicate_clusters * 5, 20)) +
        (0 if any inflation_ratio > 2:1 else 10)
score = max(0, min(100, score))
```

Thresholds: `>=80 ✅ Excellent`, `60-79 ⚠️ Acceptable`, `40-59 🔶 Needs improvement`, `<40 ❌ Poor`.

**Infrastructure-only PRs**: If `go-new-test-funcs.txt`, `go-modified-test-funcs.txt`, `js-new-test-funcs.txt`, and `js-changed-test-files.txt` are all empty (no behavioral tests added/modified) and `go-testmain-funcs.txt` is non-empty (only `TestMain` infrastructure changed), skip the numeric score formula and assign **Score: N/A — Infrastructure**. Do not fail the PR on implementation ratio (there are no behavioral tests to evaluate). Still flag hard violations (missing build tags, go mock library usage). If `go-goleak-entries.txt` is non-empty, report it as a quality improvement and approve.

Fail if either condition is true:
- `implementation_tests / total_new_tests > 0.30`
- Any coding-guideline violation exists (Go mock library usage, or new Go test missing required build tag)

Guideline violations always force `REQUEST_CHANGES` regardless of numeric score.

## Step 7: Post PR Comment with Results

Read the `test-report-templates` skill and post the report using `add-comment` (not bash; omit `item_number` — runtime infers the PR). Use the standard template, or the infrastructure-only template when the PR contains only `TestMain`/setup changes.

## Step 8: Submit PR Review Based on Result

After posting the comment, submit exactly one safe-output action based on the analysis outcome:
- When no tests required action: `{"noop": {"message": "No action needed: [brief explanation]"}}`
- When quality passes (`implementation_tests / total <= 30%` and no violations): `{"event": "APPROVE", "body": "✅ Test Quality Sentinel: {SCORE}/100. {IMPL_PCT}% implementation tests (threshold: 30%)."}`
- When this is an infrastructure-only PR (only `TestMain` changes, no behavioral tests) with no violations: `{"event": "APPROVE", "body": "✅ Test Quality Sentinel: Infrastructure only. {GOLEAK_NOTE} No violations."}`
- When quality fails (ratio `> 30%` **or** any guideline violation): `{"event": "REQUEST_CHANGES", "body": "❌ Test Quality Sentinel: {SCORE}/100. {FAIL_REASON} Review flagged tests in the comment above."}`

## Guidelines

Calibration rules:
- **Edge-case credit is generous**: one valid error assertion is enough (`assert.Error`, `t.Fatalf` on error, `.toThrow`, `.rejects`, etc.)
- **Table-driven tests**: count each row as a scenario; credit error/edge rows individually
- **Behavioral credit is strict**: mark `design_test` only when assertions verify user-visible behavior
- **Go assertion messages required**: flag assertions without descriptive failure context
- **Duplicate detection threshold**: report duplicates only when 3+ tests share the same pattern with trivial constant changes
- **Goroutine-leak guards**: `TestMain` with `goleak.VerifyTestMain` is a strong design invariant; in infrastructure-only PRs with no hard violations, approve and note it as a positive quality signal
- **Infrastructure-only PRs**: PRs adding only `TestMain` and test setup infrastructure carry no behavioral test ratio and must not be failed on that basis; evaluate only hard violations (build tags, mock libraries)

**Token Budget**: Analyze at most **50 test functions** per run. If more exist, prioritize newly added functions over modified ones; add a sampling note in the PR comment. Keep individual test analysis concise — 2–3 sentences per test in the flagged section. Always wrap the per-test classification table and flagged-test details in `<details>` tags.

## skill: `test-report-templates`
---
description: PR comment templates for the Test Quality Sentinel report (standard and infrastructure-only).
---

Standard report template:

```markdown
### 🧪 Test Quality Sentinel Report

{SCORE_EMOJI} **Test Quality Score: {SCORE}/100 — {SCORE_LABEL}**

> Analyzed {TOTAL} test(s): {DESIGN_COUNT} design, {IMPL_COUNT} implementation, {VIOLATIONS} violation(s).

<details>
<summary>📊 Metrics ({TOTAL} tests)</summary>

| Metric | Value |
|---|---|
| Analyzed | {TOTAL} (Go: {GO_COUNT}, JS: {JS_COUNT}) |
| ✅ Design | {DESIGN_COUNT} ({DESIGN_PCT}%) |
| ⚠️ Implementation | {IMPL_COUNT} ({IMPL_PCT}%) |
| Edge/error coverage | {EDGE_COUNT} ({EDGE_PCT}%) |
| Duplicate clusters | {DUP_COUNT} |
| Inflation | {YES/NO} |
| 🚨 Violations | {VIOLATIONS} |

| Test | File | Classification | Issues |
|---|---|---|---|

</details>

{If violations or flagged tests:}
<details>
<summary>⚠️ Flagged Tests ({FLAGGED_COUNT})</summary>

**`TestName`** (`file:line`) — classification, issue, and fix.

</details>

### Verdict

> {✅/❌} **{passed/failed}.** {IMPL_PCT}% implementation tests (threshold: 30%).
```

Infrastructure-only report template (no numeric score or ratio fields):

```markdown
### 🧪 Test Quality Sentinel Report

✅ **Test Quality Score: N/A — Infrastructure**

> Infrastructure-only change detected (`TestMain` / setup only). No behavioral tests were added or modified.

<details>
<summary>📊 Infrastructure Signals</summary>

| Signal | Value |
|---|---|
| `TestMain` entries | {TESTMAIN_COUNT} |
| `goleak.VerifyTestMain` entries | {GOLEAK_COUNT} |
| 🚨 Violations | {VIOLATIONS} |

</details>

### Verdict

> ✅ **passed.** Infrastructure-only PR; behavioral test ratio not applicable.
```
