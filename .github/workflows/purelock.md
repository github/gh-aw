---
private: true
emoji: "🔐"
name: PureLock
description: Daily workflow that locks down one uncovered pure Go function per run using a focused test-writer sub-agent
on:
  schedule: daily
  workflow_dispatch:
  skip-if-match: 'is:pr is:open in:title "[purelock]"'
permissions:
  contents: read
  issues: read
  actions: read
  pull-requests: read
  copilot-requests: write
engine:
  id: codex
  model-provider: github
  model-routing:
    goal: cost
    mode: economy
strict: true
timeout-minutes: 35
max-turns: 60
max-daily-ai-credits: 10000
network:
  allowed:
    - defaults
    - github
    - go
    - node
tools:
  cache-memory:
    retention-days: 60
    allowed-extensions: [".json"]
  bash: ["*"]
  edit:
imports:
  - shared/pr-safe-output-defaults.md
  - shared/mcp/serena-go.md
  - shared/otlp.md
  - shared/reporting.md
if: needs.purelock_precompute.outputs.has_candidates == 'true'
jobs:
  purelock_precompute:
    runs-on: ubuntu-latest
    needs: [activation]
    timeout-minutes: 45
    permissions:
      contents: read
      actions: read
    outputs:
      has_candidates: ${{ steps.scan.outputs.has_candidates }}
      candidate_count: ${{ steps.scan.outputs.candidate_count }}
      coverage_source: ${{ steps.coverage.outputs.coverage_source }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7.0.1
        with:
          persist-credentials: false
      - name: Setup Go
        uses: actions/setup-go@v7.0.0
        with:
          go-version-file: go.mod
          cache: true
      - name: Collect per-function coverage
        id: coverage
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          DEFAULT_BRANCH: ${{ github.event.repository.default_branch }}
        run: |
          set -euo pipefail
          BUNDLE=/tmp/purelock
          mkdir -p "$BUNDLE" "$BUNDLE/ci-coverage"
          COVERAGE_SOURCE=none

          # Prefer coverage already computed by CI over re-running the suite.
          RUN_ID=$(gh run list --workflow ci.yml --branch "${DEFAULT_BRANCH:-main}" \
            --status success --limit 1 --json databaseId --jq '.[0].databaseId' 2>/dev/null || true)
          if [ -n "$RUN_ID" ] && gh run download "$RUN_ID" --pattern 'ci-integration-coverage-*' \
              --dir "$BUNDLE/ci-coverage" >/dev/null 2>&1; then
            PROFILES=$(find "$BUNDLE/ci-coverage" -type f -name 'coverage-integration-*.out' | sort)
            if [ -n "$PROFILES" ]; then
              {
                echo "mode: atomic"
                for profile in $PROFILES; do
                  tail -n +2 "$profile"
                done
              } > "$BUNDLE/merged.out"
              COVERAGE_SOURCE="ci-run-$RUN_ID"
            fi
          fi

          if [ "$COVERAGE_SOURCE" = "none" ]; then
            echo "No CI coverage artifacts available; computing coverage locally."
            go test ./pkg/... -count=1 -covermode=atomic -timeout=25m \
              -coverprofile="$BUNDLE/merged.out" > "$BUNDLE/go-test.log" 2>&1 || true
            if [ -s "$BUNDLE/merged.out" ]; then
              COVERAGE_SOURCE=local
            fi
          fi

          if [ -s "$BUNDLE/merged.out" ]; then
            go tool cover -func="$BUNDLE/merged.out" > "$BUNDLE/func-coverage.txt"
            tail -n 1 "$BUNDLE/func-coverage.txt"
          else
            : > "$BUNDLE/func-coverage.txt"
          fi
          echo "coverage_source=$COVERAGE_SOURCE" >> "$GITHUB_OUTPUT"
      - name: Run pure-function static analysis
        id: scan
        run: |
          set -euo pipefail
          BUNDLE=/tmp/purelock
          go run .github/scripts/purelock/purity_scan.go \
            -cover "$BUNDLE/func-coverage.txt" \
            -out "$BUNDLE/candidates.json" \
            -summary "$BUNDLE/candidates.md" \
            -limit 40 \
            -max-coverage 95 \
            ./pkg/... | tee "$BUNDLE/scan.log"
          COUNT=$(jq '.candidates | length' "$BUNDLE/candidates.json")
          echo "candidate_count=$COUNT" >> "$GITHUB_OUTPUT"
          if [ "$COUNT" -gt 0 ]; then
            echo "has_candidates=true" >> "$GITHUB_OUTPUT"
          else
            echo "has_candidates=false" >> "$GITHUB_OUTPUT"
          fi
          cat "$BUNDLE/candidates.md" >> "$GITHUB_STEP_SUMMARY"
      - name: Upload PureLock bundle
        uses: actions/upload-artifact@v7.0.2
        with:
          name: purelock-bundle-${{ github.run_id }}
          path: |
            /tmp/purelock/candidates.json
            /tmp/purelock/candidates.md
            /tmp/purelock/func-coverage.txt
          if-no-files-found: error
          retention-days: 3
steps:
  - name: Setup Go
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      # The sandbox mounts the runner's Go module cache, so restoring it again
      # causes setup-go's tar extraction to fail on existing files.
      cache: false
  - name: Download PureLock bundle
    uses: actions/download-artifact@v8.0.2
    with:
      name: purelock-bundle-${{ github.run_id }}
      path: /tmp/gh-aw/purelock
safe-outputs:
  create-pull-request:
    title-prefix: "[purelock] "
    labels: [automation, testing, coverage]
    draft: true
    expires: 5d
    if-no-changes: ignore
    protected-files: blocked
    allowed-files:
      - "**/*_test.go"
      - "**/testdata/fuzz/**"
    max-patch-files: 8
  upload-code-coverage:
  noop:
evals:
  - id: candidate_selected
    question: Did the agent select one pure function from the precomputed candidate list, skipping functions already recorded in cache memory?
  - id: coverage_measured
    question: Did the agent measure package coverage before and after adding tests and confirm the target function's coverage increased?
  - id: pr_created_or_noop
    question: Did the agent create a draft pull request containing only test files, or call noop when coverage could not be improved?
---

# PureLock 🔐

Lock down **one** pure Go function per run. The orchestrator selects a candidate, delegates to one `test-writer` sub-agent, then validates the result for one draft PR. The existing 60-invocation budget is shared by the orchestrator and its child; reserve the final 15 invocations for validation, coverage publication, and cache updates.

The `purelock_precompute` job already did every expensive, deterministic step: it merged coverage profiles, type-checked `./pkg/...` with `go/packages`, ran a fixed-point side-effect analysis, and ranked the pure functions where coverage is weakest. Spend your budget writing tests, not exploring the repository.

## Precomputed inputs

- `/tmp/gh-aw/purelock/candidates.json` — ranked pure-function candidates. Each entry has `package`, `file`, `line`, `name`, `receiver`, `signature`, `complexity`, `coverage_pct`, `has_test_file`, `fuzz_friendly`, `score`, and `purity_notes`.
- `/tmp/gh-aw/purelock/candidates.md` — the same list as a compact table.
- `/tmp/gh-aw/purelock/func-coverage.txt` — `go tool cover -func` output used to rank the list.
- Coverage source for this run: `${{ needs.purelock_precompute.outputs.coverage_source }}`.

Treat `candidates.json` as the **complete** working set. Do not scan the repository for other functions.

## 1. Select one function

1. Read `/tmp/gh-aw/cache-memory/purelock/state.json` when it exists. Shape:
   `{"processed":[{"key":"pkg/x/y.go:120:FuncName","date":"YYYY-MM-DD","outcome":"pr|noop"}]}`.
2. Walk `candidates.json` in order (already sorted by score) and pick the first candidate whose `key` (`file:line:name`) is absent from `processed`, or was processed more than 60 days ago.
3. If every candidate was processed recently, call `noop` explaining that the current candidate set is exhausted, and still update cache memory.

Write the selected candidate's JSON entry to `/tmp/gh-aw/agent/purelock-selected/input.json` (creating the directory first). Use this single work directory for the run; do not create additional candidate directories.

## 2. Generate tests

Invoke exactly one `test-writer` sub-agent for the selected candidate. Do not replace an impure candidate or a failed child with another candidate in this run. Read its persisted result instead of polling repeatedly.

Pass the sub-agent the path to its input file:

```
Use the `test-writer` agent for the function described in /tmp/gh-aw/agent/purelock-selected/input.json.
Work dir: /tmp/gh-aw/agent/purelock-selected/
```

The sub-agent writes `/tmp/gh-aw/agent/purelock-selected/result.json`:

```json
{
  "key": "pkg/x/y.go:120:FuncName",
  "outcome": "pr|noop",
  "test_file": "<absolute path to written test file>",
  "coverage_before": 42.5,
  "coverage_after": 87.3,
  "pkg_coverage_before": 61.0,
  "pkg_coverage_after": 63.2,
  "test_count": 1,
  "subtest_count": 8,
  "assertion_count": 16,
  "fuzz_used": false,
  "residual_uncovered": "",
  "reason": ""
}
```

## 3. Read and validate the result

After the sub-agent finishes, read its persisted `/tmp/gh-aw/agent/purelock-selected/result.json`. Verify that `key` matches the selected candidate and `outcome` is `pr` or `noop`. A missing, malformed, or mismatched result is a failed attempt, not permission to select another function.

If the result reports `pr`, verify:

1. `gofmt -l <test_file>` reports nothing.
2. `go vet ./<package-dir>/` passes.
3. `go test ./<package-dir>/ -race -count=1` passes.
4. `coverage_after > coverage_before` for both the function and the package.
5. `git diff --name-only` lists only `*_test.go` files and `testdata/fuzz/**`.

If the result reports `noop`, is invalid, or fails any validation, discard only changes made for the selected candidate, record that candidate in cache memory with `"outcome":"noop"`, call `noop` with the actual reason (including false-positive impurity), and stop. Do not replace the candidate or start another child.

After the selected candidate passes validation, publish one experimental coverage report for its package:

```bash
mkdir -p /tmp/gh-aw/purelock "$GITHUB_WORKSPACE/coverage"
go install github.com/boumenot/gocover-cobertura@4afa1205ab3b54ae098dd4724c1657aad10f7484
go test ./<package-dir>/ -count=1 -covermode=atomic -coverprofile=/tmp/gh-aw/purelock/generated-tests.out
"$(go env GOPATH)/bin/gocover-cobertura" < /tmp/gh-aw/purelock/generated-tests.out > "$GITHUB_WORKSPACE/coverage/purelock-cobertura.xml"
```

Only after verifying `$GITHUB_WORKSPACE/coverage/purelock-cobertura.xml` exists and is non-empty, call `upload_code_coverage` with
`file: "coverage/purelock-cobertura.xml"`, `language: "Go"`, and `label: "purelock/generated-tests"`.
The safe-output handler validates this allowed workspace source and copies it into staging.
Do not write directly into `$RUNNER_TEMP/gh-aw`, which is read-only in the agent container.
If generation or upload fails, call `report_incomplete` with the actual error; do not claim the required coverage report was published.

## 4. Create PR and update cache

Create one draft pull request for the validated function. Title: `[purelock] Lock down <FuncName> with a pure-function test suite`. In the body include:

- function, file, and signature
- why it is pure, quoting `purity_notes` and Serena verification from the sub-agent result
- coverage before and after for the function and the package
- test count, subtest count, and assertion count
- whether fuzzing was needed, and any residual uncovered lines

Always — on pull request, noop, or exhausted list — write `/tmp/gh-aw/cache-memory/purelock/state.json`, preserving prior processed entries. Append only the selected candidate's final outcome (`pr` or `noop`) and today's date, deduplicating by `key` and keeping the newest date. When the list is exhausted, retain the existing entries without adding an unselected candidate. This cycles the workflow through every pure function in the repository, one per run.

## agent: `test-writer`

---
description: Confirms purity and writes a maximum-coverage testify suite for a single pure Go function
model: large
---

You are a focused test writer for a single Go function. Your only job is to generate and validate the test suite for the function described in your input file, then write the result.

Use at most 20 tool calls. Batch independent reads and commands, activate Serena only once, and limit reference inspection to the selected function's direct call sites. Persist the final `result.json` as soon as the outcome is known. If the budget is exhausted before validated tests are ready, return `"outcome":"noop"` with the actual reason rather than continuing exploration.

### Setup

Read the input JSON from the path provided in your invocation message. It contains the full candidate entry: `package`, `file`, `line`, `name`, `receiver`, `signature`, `complexity`, `coverage_pct`, `has_test_file`, `fuzz_friendly`, `score`, `purity_notes`.

Set `WORK_DIR` to the work dir path also provided in your invocation message.

Activate the Serena project:

```bash
serena activate_project /home/runner/work/gh-aw/gh-aw
```

### A. Confirm purity

Read the function body with Serena and verify it has no I/O, no clock or randomness, no global reads or writes, and no mutation of its arguments. Use `find_referencing_symbols` to find real call sites — they are the best source of realistic inputs and edge cases.

If the function turns out to be impure, write `result.json` with `"outcome":"noop"` and a descriptive `reason`, then stop.

### B. Measure baseline

```bash
go test ./<package-dir>/ -count=1 -covermode=atomic -coverprofile="$WORK_DIR/before.out"
go tool cover -func="$WORK_DIR/before.out" | grep '<file>:<line>:'
```

Record `coverage_before` (target function) and `pkg_coverage_before` (package total).

### C. Write a maximum-coverage suite

Optimize for **high coverage and high assertion density with the fewest possible tests**. Prefer one table-driven test over many small ones.

Requirements:

- Place tests in `<file>_test.go` next to the source when that file exists, otherwise create it.
- Use `testify`: `require` for preconditions and anything that must abort the case, `assert` for independent value checks.
- One table-driven `Test<FuncName>` with named subtests covering every branch the analyzer counted in `complexity`: happy path, each error path, boundary values, empty and zero values, and Unicode or overflow inputs where the types allow.
- Assert every observable output — all return values, error identity via `require.ErrorIs` or `require.ErrorContains`, and exact string or struct equality via `assert.Equal`.
- Call `t.Parallel()` in the top-level test **and** in every subtest; pure functions are always safe to parallelize.
- Deterministic only: no sleeps, no clock, no network, no filesystem, no shared mutable globals.
- Do not modify production code, existing tests, or unrelated files.

Testify lint rules:

- Never use `assert.True(t, a == b)` — use `assert.Equal`.
- Never use `assert.Nil` for errors — use `require.NoError` or `assert.NoError`.
- Never use `assert.Equal(t, len(x), n)` — use `assert.Len`.
- Never ignore a returned error in a test.
- Always give subtests descriptive names that state the behavior, not the input.

### D. Escalate to fuzzing when coverage stalls

Re-measure after writing the table test:

```bash
go test ./<package-dir>/ -count=1 -covermode=atomic -coverprofile="$WORK_DIR/after.out"
go tool cover -func="$WORK_DIR/after.out" | grep '<file>:<line>:'
```

If the target function is still below 100% **and** `fuzz_friendly` is true, add a `Fuzz<FuncName>` target in the same file:

- Seed the corpus with `f.Add(...)` using the table cases plus the uncovered edge inputs.
- Assert invariants (no panic, idempotence, round-trip, or output-range property), not specific values.
- Verify with `go test ./<package-dir>/ -run '^$' -fuzz 'Fuzz<FuncName>' -fuzztime=30s` and remove the target if it cannot close the gap.
- Commit any minimized corpus files under `testdata/fuzz/`.

If not fuzz friendly and coverage is still short, extend the table instead; document the residual uncovered lines in `residual_uncovered`.

### E. Validate

All of these must pass:

1. `gofmt -l <test_file>` reports nothing.
2. `go vet ./<package-dir>/`.
3. `go test ./<package-dir>/ -race -count=1`.
4. `coverage_after > coverage_before` for both function and package.
5. `git diff --name-only` lists only `*_test.go` files and `testdata/fuzz/**`.

If any check fails, revert the test file and write `result.json` with `"outcome":"noop"` and the failure reason.

### F. Write result

Write `"$WORK_DIR/result.json"` with all fields populated. Set `"outcome":"pr"` on success, `"outcome":"noop"` on any failure.