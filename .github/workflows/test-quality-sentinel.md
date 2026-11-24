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
model: router/economy
engine:
  id: copilot
  max-continuations: 15
skip-if-decision:
  - >-
    Do @/tmp/gh-aw/agent/pr-meta.json, @/tmp/gh-aw/agent/test-diff.txt, and
    @/tmp/gh-aw/agent/missing-build-tags.txt conclusively establish that no
    supported Go or JavaScript behavioral test changed and no hard guideline
    violation exists, so the act-vs-noop rubric requires no review? Abstain
    for unavailable, stale, or truncated evidence, any hard violation, or any
    behavioral test change; clean behavioral tests still require approval.
cache:
  key: pr-test-prefetch-v2-${{ github.event.pull_request.number || github.event.issue.number || (fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_type == 'pull_request' && fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_number) }}-${{ github.event.pull_request.head.sha || 'unknown' }}-${{ github.run_id }}-${{ github.run_attempt }}
  path: /tmp/gh-aw/agent
  restore-keys:
    - pr-test-prefetch-v2-${{ github.event.pull_request.number || github.event.issue.number || (fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_type == 'pull_request' && fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_number) }}-${{ github.event.pull_request.head.sha && format('{0}-', github.event.pull_request.head.sha) || '' }}
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
  bash:
    - "git diff:*"
    - "grep:*"
    - "cat:*"
    - "echo:*"
pre-agent-steps:
  - name: Pre-fetch PR data
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      PR_NUMBER: ${{ github.event.issue.number || github.event.pull_request.number || (fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_type == 'pull_request' && fromJSON(github.event.inputs.aw_context || github.event.client_payload.aw_context || '{}').item_number) }}
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
      if [[ ! "$PR_NUMBER" =~ ^[0-9]+$ ]]; then
        write_prefetch_fallback "unable to determine the pull request number"
        exit 0
      fi
      CURRENT_HEAD_SHA="${PR_HEAD_SHA:-}"
      if [ -z "$CURRENT_HEAD_SHA" ]; then
        CURRENT_HEAD_SHA=$(gh pr view "$PR_NUMBER" --json headRefOid --jq '.headRefOid' 2>/dev/null || true)
      fi
      CACHE_HEAD_SHA=""
      if [ -f /tmp/gh-aw/agent/test-data-head-sha.txt ]; then
        CACHE_HEAD_SHA="$(tr -d '\n' < /tmp/gh-aw/agent/test-data-head-sha.txt)"
      fi
      if [ -n "$CURRENT_HEAD_SHA" ] && [ "$CURRENT_HEAD_SHA" = "$CACHE_HEAD_SHA" ] && \
         [ ! -f /tmp/gh-aw/agent/test-prefetch-unavailable.txt ] && \
         [ -f /tmp/gh-aw/agent/pr-meta.json ] && \
         [ -f /tmp/gh-aw/agent/test-files.txt ] && \
         [ -f /tmp/gh-aw/agent/test-diff.txt ] && \
         [ -f /tmp/gh-aw/agent/diff-numstat.txt ] && \
         jq -e --arg sha "$CURRENT_HEAD_SHA" --argjson number "$PR_NUMBER" \
           '.headRefOid == $sha and .number == $number and (.files | type == "array")' \
           /tmp/gh-aw/agent/pr-meta.json >/dev/null 2>&1; then
        echo "Cache hit: using pre-fetched test data for head ${CURRENT_HEAD_SHA}"
        rm -f /tmp/gh-aw/agent/test-prefetch-unavailable.txt
        exit 0
      fi

      # PR metadata
      if ! gh pr view "$PR_NUMBER" \
        --json number,files,additions,deletions,baseRefName,baseRefOid,headRefName,headRefOid \
        > /tmp/gh-aw/agent/pr-meta.json; then
        write_prefetch_fallback "unable to fetch PR metadata"
        exit 0
      fi
      if [ -z "$CURRENT_HEAD_SHA" ]; then
        CURRENT_HEAD_SHA="$(jq -r '.headRefOid // empty' /tmp/gh-aw/agent/pr-meta.json)"
      fi
      if [ -z "$CURRENT_HEAD_SHA" ] || \
         ! jq -e --arg sha "$CURRENT_HEAD_SHA" --argjson number "$PR_NUMBER" \
           '.headRefOid == $sha and .number == $number and (.files | type == "array")' \
           /tmp/gh-aw/agent/pr-meta.json >/dev/null 2>&1; then
        write_prefetch_fallback "PR metadata does not match the requested head"
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
        # gh pr diff accepts a PR argument, not git-style pathspecs.
        if ! gh pr diff "$PR_NUMBER" --color=never \
          | awk '
              /^diff --git / { keep = ($0 ~ / "?b\/.*(_test\.go|\.test\.cjs|\.test\.js)"?$/) }
              keep { print }
            ' > /tmp/gh-aw/agent/test-diff-full.txt; then
          write_prefetch_fallback "unable to fetch test file diff"
          exit 0
        fi
        if [ ! -s /tmp/gh-aw/agent/test-diff-full.txt ] || \
           [ "$(wc -c < /tmp/gh-aw/agent/test-diff-full.txt)" -gt 40000 ] || \
           [ "$(grep -c '^diff --git ' /tmp/gh-aw/agent/test-diff-full.txt)" != \
             "$(grep -c . /tmp/gh-aw/agent/test-files.txt)" ]; then
          write_prefetch_fallback "test file diff is missing or exceeds the 40 KB review limit"
          exit 0
        fi
        mv /tmp/gh-aw/agent/test-diff-full.txt /tmp/gh-aw/agent/test-diff.txt
      else
        : > /tmp/gh-aw/agent/test-diff.txt
      fi

      BASE_SHA="${EXPR_GITHUB_EVENT_PULL_REQUEST_BASE_SHA:-}"
      if [ -z "$BASE_SHA" ]; then
        BASE_SHA="$(jq -r '.baseRefOid // empty' /tmp/gh-aw/agent/pr-meta.json)"
      fi
      if [ -z "$BASE_SHA" ] || \
         ! git diff "$BASE_SHA...HEAD" --numstat > /tmp/gh-aw/agent/diff-numstat.txt; then
        write_prefetch_fallback "unable to compute PR diff statistics from local base history"
        exit 0
      fi

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
        if ! git diff "$BASE_SHA...HEAD" --diff-filter=A --name-only \
          > /tmp/gh-aw/agent/new-files.txt; then
          write_prefetch_fallback "unable to identify new test files from local base history"
          exit 0
        fi
        : > /tmp/gh-aw/agent/missing-build-tags.txt
        while IFS= read -r f; do
          case "$f" in
            *_test.go)
              if [ ! -r "$f" ]; then
                write_prefetch_fallback "new test file is unavailable in the PR checkout"
                exit 0
              fi
              if ! head -1 "$f" | grep -qE '^//go:build'; then
                printf 'MISSING BUILD TAG: %s\n' "$f" >> /tmp/gh-aw/agent/missing-build-tags.txt
              fi
              ;;
          esac
        done < /tmp/gh-aw/agent/new-files.txt
        rm -f /tmp/gh-aw/agent/new-files.txt
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
        for file in /tmp/gh-aw/agent/go-new-test-funcs.txt \
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
      fi
      if ! FINAL_HEAD_SHA=$(gh pr view "$PR_NUMBER" --json headRefOid --jq '.headRefOid') || \
         [ "$FINAL_HEAD_SHA" != "$CURRENT_HEAD_SHA" ]; then
        write_prefetch_fallback "PR head changed or became unavailable while fetching test data"
        exit 0
      fi
      printf '%s\n' "$CURRENT_HEAD_SHA" > /tmp/gh-aw/agent/test-data-head-sha.txt
      rm -f /tmp/gh-aw/agent/test-prefetch-unavailable.txt

      echo "Pre-fetched $(grep -c . /tmp/gh-aw/agent/test-files.txt || echo 0) test files"
safe-outputs:
  add-comment:
    max: 1
    replace-older-comments: true
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
evals:
  - id: review_decision_consistent
    question: Did the agent either post a Test Quality Score with an APPROVE or REQUEST_CHANGES decision that matches the tests and guideline violations it flagged, call report_incomplete (not noop) when pre-fetch data was unavailable, or call noop with a scope reason the workflow's act-vs-noop rubric allows (no changed or only unsupported-language test files, or no behavioral test change and no hard violation)? Return UNKNOWN when the output records a skip-if-decision no-op before agent execution.

---

# Test Quality Sentinel 🧪

The AWF decision gate applies the act-vs-noop rubric before invocation. Behavioral changes, hard violations, and uncertain evidence preserve execution.

Analyze added or modified Go and JavaScript tests in this PR. Produce a **Test Quality Score** (0–100) and flag tests that lack genuine behavioral coverage.

- **Repository**: ${{ github.repository }}
- **Pull Request**: #${{ github.event.pull_request.number }} — "${{ github.event.pull_request.title }}"
- **Actor**: ${{ github.actor }}

Review whether tests cover behavioral contracts and design invariants, not only happy-path implementation details.

## Step 1: Check evidence and scope

If `/tmp/gh-aw/agent/test-prefetch-unavailable.txt` exists, read its reason and immediately call `report_incomplete`. Never analyze fallback files, call `noop`, or post a report. Missing, stale, truncated, or oversized required evidence also requires `report_incomplete`, never approval or `noop`.

Read `/tmp/gh-aw/agent/pr-meta.json`, `test-files.txt`, `test-diff.txt` (maximum 40 KB), `diff-numstat.txt`, `go-new-test-funcs.txt`, `go-modified-test-funcs.txt`, `js-new-test-funcs.txt`, `js-changed-test-files.txt`, `go-testmain-funcs.txt`, `go-goleak-entries.txt`, `missing-build-tags.txt`, `go-test-stats.txt`, and `js-test-stats.txt` from `/tmp/gh-aw/agent/`.

Review only added or modified Go (`*_test.go`) and JavaScript (`*.test.cjs`, `*.test.js`) behavioral tests; note other languages without scoring. Use extracted signatures and stats as a guide, then read full test bodies in the diff. If no supported Go/JS test files changed, or only unsupported-language tests changed, call `noop` with the specific scope reason. If supported files changed but no behavioral tests changed, apply the infrastructure-only rule below; otherwise use `noop` only when there is no hard violation.

### Step 2: Review changed tests

For each behavioral test, record its name, file, approximate line, design invariant (`behavioral_contract`, `implementation_detail`, `unknown`), value if deleted (`high_value`, `low_value`, `duplicated`), and type (`design_test`, `implementation_test`). Do not score `TestMain` as a behavioral test.

If all of the following hold, this is an **infrastructure-only PR** — see the scoring guidance in Step 6:
- `go-new-test-funcs.txt` is empty
- `go-modified-test-funcs.txt` is empty
- `js-new-test-funcs.txt` is empty
- `js-changed-test-files.txt` is empty
- `go-testmain-funcs.txt` is non-empty

### Step 3: Classify quality

Use the Go/JS stats for assertions, errors, table-driven cases, and mocks. Count each Go `t.Run` row as a scenario; one valid error assertion earns edge-case credit. Flag happy-path-only tests, missing assertions, Go assertions without descriptive failure context, duplicate patterns shared by 3+ tests, JS mock-heavy tests without observable assertions, and test inflation (>2:1 added test lines to production lines from `diff-numstat.txt`). External-I/O/runtime mocks are acceptable. Go `gomock`, `testify/mock`, `.EXPECT()`, or `.On()` usage and new Go test files without a line-1 `//go:build !integration` or `//go:build integration` tag are hard violations.

### Step 4: Apply the outcome rubric

Treat `TestMain` with `goleak.VerifyTestMain(m)` as a package-level design invariant; note it positively and do not penalize missing traditional assertions.

No behavioral tests changed and no hard violation: `noop`. A hard violation or implementation-test ratio `>30%`: report concrete evidence and request changes. Other red flags: report only evidenced concerns without escalation. Clean behavioral tests: report and approve; do not invent findings.

## Step 5: Score quality

Compute the added-test/production added-line ratio for each changed Go/JS test using `diff-numstat.txt` (`foo_test.go` → `foo.go`; `foo.test.cjs` → `foo.cjs`; `foo.test.js` → `foo.js`). A ratio above 2:1 is inflation; ignore inflation for `TestMain`-only files.

```
score = 40 * design_tests / total_new_tests
      + 30 * tests_with_edge_cases / total_new_tests
      + 20 - min(duplicate_clusters * 5, 20)
      + (0 if any inflation_ratio > 2:1 else 10)
```

Clamp to 0–100. Labels: `>=80` Excellent, `60–79` Acceptable, `40–59` Needs improvement, `<40` Poor. If `go-new-test-funcs.txt`, `go-modified-test-funcs.txt`, `js-new-test-funcs.txt`, and `js-changed-test-files.txt` are empty and `go-testmain-funcs.txt` is non-empty, assign `N/A — Infrastructure`; do not apply the implementation ratio, but still flag hard violations. Note `goleak.VerifyTestMain` positively.

A hard violation or `implementation_tests / total_new_tests > 0.30` requires `REQUEST_CHANGES`. Guideline violations always fail regardless of score.

## Step 6: Publish the result

Read `test-report-templates`; post one report with `add-comment` (not bash; omit `item_number`). Do not comment for a `noop`. Analyze at most 50 tests, prioritizing new tests; disclose sampling. Keep flagged-test details to 2–3 sentences and wrap classification tables and findings in `<details>`.

Then submit exactly one safe-output action:

- No action: `{"noop":{"message":"No action needed: [scope reason]"}}`
- Passing behavioral tests: `{"event":"APPROVE","body":"✅ Test Quality Sentinel: {SCORE}/100. {IMPL_PCT}% implementation tests (threshold: 30%)."}`
- Passing infrastructure-only PR: `{"event":"APPROVE","body":"✅ Test Quality Sentinel: Infrastructure only. {GOLEAK_NOTE} No violations."}`
- Failure: `{"event":"REQUEST_CHANGES","body":"❌ Test Quality Sentinel: {SCORE}/100. {FAIL_REASON} Review flagged tests in the comment above."}`

<!-- instruction-prefix-end -->

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
