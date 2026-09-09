---
private: true
emoji: "🔐"
name: PureLock
description: Daily workflow that locks down up to 3 uncovered pure Go functions per run using parallel test-writer sub-agents
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
model: copilot/gpt-5.3-codex
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
        uses: actions/upload-artifact@v7.0.1
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
    uses: actions/download-artifact@v8.0.1
    with:
      name: purelock-bundle-${{ github.run_id }}
      path: /tmp/gh-aw/purelock
safe-outputs:
  steer: true
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
sandbox:
  agent:
    runtime: cloud-hypervisor
evals:
  - id: candidate_selected
    question: Did the agent select one pure function from the precomputed candidate list, skipping functions already recorded in cache memory?
  - id: coverage_measured
    question: Did the agent measure package coverage before and after adding tests and confirm the target function's coverage increased?
  - id: pr_created_or_noop
    question: Did the agent create a draft pull request containing only test files, or call noop when coverage could not be improved?
---

# PureLock 🔐

Lock down up to **3** pure Go functions per run. The orchestrator selects candidates, fans out to parallel `test-writer` sub-agents for test generation, then merges results into one draft PR.

The `purelock_precompute` job already did every expensive, deterministic step: it merged coverage profiles, type-checked `./pkg/...` with `go/packages`, ran a fixed-point side-effect analysis, and ranked the pure functions where coverage is weakest. Spend your budget writing tests, not exploring the repository.

## Precomputed inputs

- `/tmp/gh-aw/purelock/candidates.json` — ranked pure-function candidates. Each entry has `package`, `file`, `line`, `name`, `receiver`, `signature`, `complexity`, `coverage_pct`, `has_test_file`, `fuzz_friendly`, `score`, and `purity_notes`.
- `/tmp/gh-aw/purelock/candidates.md` — the same list as a compact table.
- `/tmp/gh-aw/purelock/func-coverage.txt` — `go tool cover -func` output used to rank the list.
- Coverage source for this run: `${{ needs.purelock_precompute.outputs.coverage_source }}`.

Treat `candidates.json` as the **complete** working set. Do not scan the repository for other functions.

## 1. Select up to 3 functions

1. Read `/tmp/gh-aw/cache-memory/purelock/state.json` when it exists. Shape:
   `{"processed":[{"key":"pkg/x/y.go:120:FuncName","date":"YYYY-MM-DD","outcome":"pr|noop"}]}`.
2. Walk `candidates.json` in order (already sorted by score) and pick the first **up to 3** candidates whose `key` (`file:line:name`) is absent from `processed`, or was processed more than 60 days ago.
3. If every candidate was processed recently, call `noop` explaining that the current candidate set is exhausted, and still update cache memory.

For each selected candidate, write its JSON entry to `/tmp/gh-aw/agent/purelock-<name>/input.json` (creating the directory first).

## 2. Generate tests in parallel

Invoke the `test-writer` sub-agent **simultaneously** for every selected candidate — start all invocations at once without waiting for any to finish first.

Pass each agent the path to its input file:

```
Use the `test-writer` agent for the function described in /tmp/gh-aw/agent/purelock-<name>/input.json.
Work dir: /tmp/gh-aw/agent/purelock-<name>/
```

Each agent writes `/tmp/gh-aw/agent/purelock-<name>/result.json`:

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

## 3. Collect and validate results

After all agents finish, read every `result.json`. Separate results into `succeeded` (outcome=pr) and `failed` (outcome=noop).

For each failed entry record it in cache memory with `"outcome":"noop"`. If **all** agents reported noop, call `noop` explaining the reasons, update cache memory, and stop.

For each succeeded entry verify:

1. `gofmt -l <test_file>` reports nothing.
2. `go vet ./<package-dir>/` passes.
3. `go test ./<package-dir>/ -race -count=1` passes.
4. `coverage_after > coverage_before` for both the function and the package.

Drop any entry that fails validation and record it as noop. If no entries remain, call `noop`, update cache, and stop.

After the final succeeded set is known, publish one experimental coverage report for the changed package set:

```bash
mkdir -p /tmp/gh-aw/purelock
go install github.com/boumenot/gocover-cobertura@4afa1205ab3b54ae098dd4724c1657aad10f7484
go test ./<unique-succeeded-package-dir>/... -count=1 -covermode=atomic -coverprofile=/tmp/gh-aw/purelock/generated-tests.out
"$(go env GOPATH)/bin/gocover-cobertura" < /tmp/gh-aw/purelock/generated-tests.out > /tmp/gh-aw/purelock/cobertura.xml
```

Only after verifying `/tmp/gh-aw/purelock/cobertura.xml` exists and is non-empty, call `upload_code_coverage` with
`file: "/tmp/gh-aw/purelock/cobertura.xml"`, `language: "Go"`, and `label: "purelock/generated-tests"`.

## 4. Create PR and update cache

Create one draft pull request. Title: `[purelock] Lock down <FuncA>, <FuncB>, … with pure-function test suites` (list all succeeded function names). In the body include a section per function:

- function, file, and signature
- why it is pure, quoting `purity_notes` and Serena verification from the sub-agent result
- coverage before and after for the function and the package
- test count, subtest count, and assertion count
- whether fuzzing was needed, and any residual uncovered lines

Always — on pull request, noop, or exhausted list — write `/tmp/gh-aw/cache-memory/purelock/state.json` with **all** processed entries (both pr and noop) appended, deduplicated by `key`, keeping the newest date. This is what cycles the workflow through every pure function in the repository.

## agent: `test-writer`

---
description: Confirms purity and writes a maximum-coverage testify suite for a single pure Go function
model: large
---

You are a focused test writer for a single Go function. Your only job is to generate and validate the test suite for the function described in your input file, then write the result.

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