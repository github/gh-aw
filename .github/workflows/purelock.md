---
private: true
emoji: "🔐"
name: PureLock
description: Daily workflow that locks down one uncovered pure Go function per run with native repository tools
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
  id: copilot
  copilot-sdk: true
  model: gpt-5.3-codex
sandbox:
  agent: awf
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
  profile: go
  cache-memory:
    retention-days: 60
    allowed-extensions: [".json"]
  bash: false
  cli-proxy: false
  edit:
  github:
    mode: local
    min-integrity: none
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
  noop:
evals:
  - id: candidate_selected
    question: Did the agent select one pure function from the precomputed candidate list, skipping functions already recorded in cache memory?
  - id: repository_validated
    question: Did the agent format and validate the projected repository after adding tests?
  - id: pr_created_or_noop
    question: Did the agent create a draft pull request containing only test files, or call noop when no candidate could be safely tested?
---

# PureLock 🔐

This experimental workflow uses the shell-free Go repository profile. The
`purelock_precompute` job already merged coverage profiles, type-checked
`./pkg/...`, ran fixed-point side-effect analysis, and ranked candidates. Use
only the configured native repository, editing, and MCP tools; do not request
shell, task, or CLI-proxy access.

## Precomputed inputs

- `/tmp/gh-aw/purelock/candidates.json` — the complete ranked candidate set;
  select only from this file.
- `/tmp/gh-aw/purelock/candidates.md` — a human-readable summary of the ranked
  candidates.
- `/tmp/gh-aw/purelock/func-coverage.txt` — coverage evidence used by the
  precompute job to rank candidates; do not regenerate it.

## Select, write, and validate tests

1. Read `/tmp/gh-aw/cache-memory/purelock/state.json` when it exists. Select
   the first unprocessed candidate from `candidates.json`, or one whose result
   is older than 60 days. If none qualify, call `safeoutputs-noop` and update
   the state.
2. Use Serena and the native inspection tools to confirm that the candidate is
   pure and identify its branches and realistic call-site inputs. Do not scan
   beyond the selected candidate's package.
3. Add a focused table-driven `Test<FuncName>` in the source package. Modify
   only allowed `*_test.go` files. Cover happy paths, errors, boundaries, zero
   values, and applicable Unicode or overflow inputs. Use `testify` assertions
   consistent with adjacent tests; keep tests deterministic and parallel only
   when their inputs are independent.
4. Call `go_repository.format`, which formats changed eligible Go files; then
   call `go_repository.readiness`, which compiles the projected repository's
   tests; finally call `go_repository.validate`, which checks Go formatting
   across the projected tree and runs `go test -count=1 ./...`, `go vet ./...`,
   and `go build`. If any operation fails, revert the test change, record the
   candidate as `noop` in cache memory, and complete with `safeoutputs-noop`.
5. On successful validation, call `go_repository.commit`, then
   `safeoutputs-create_pull_request` with a draft title
   `[purelock] Lock down <FuncName> with a pure-function test suite`. Explain
   the function's purity, the tested behavior, and the native validation
   result.

Always update `/tmp/gh-aw/cache-memory/purelock/state.json` with the processed
candidate, deduplicated by `key` and retaining the newest date.