---
# CI Data Analysis
# Shared module for analyzing CI run data
#
# Usage:
#   imports:
#     - shared/ci-data-analysis.md
#
# This import provides:
# - Pre-download CI runs and artifacts
# - Build and test the project
# - Collect performance metrics

imports:
  - ../skills/jqschema/SKILL.md

tools:
  cache-memory: true
  bash: ["*"]

steps:
  - name: Download CI workflow runs from last 7 days
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      # Download workflow runs for split CI workflows (ci, cgo, cjs)
      gh run list --repo "$GITHUB_REPOSITORY" --workflow=ci.yml --limit 30 --json databaseId,status,conclusion,createdAt,updatedAt,displayTitle,headBranch,event,url,workflowDatabaseId,number > /tmp/gh-aw/agent/ci-runs-ci.json
      gh run list --repo "$GITHUB_REPOSITORY" --workflow=cgo.yml --limit 30 --json databaseId,status,conclusion,createdAt,updatedAt,displayTitle,headBranch,event,url,workflowDatabaseId,number > /tmp/gh-aw/agent/ci-runs-cgo.json
      gh run list --repo "$GITHUB_REPOSITORY" --workflow=cjs.yml --limit 30 --json databaseId,status,conclusion,createdAt,updatedAt,displayTitle,headBranch,event,url,workflowDatabaseId,number > /tmp/gh-aw/agent/ci-runs-cjs.json
      jq -s 'add | sort_by(.createdAt) | reverse | .[0:60]' /tmp/gh-aw/agent/ci-runs-ci.json /tmp/gh-aw/agent/ci-runs-cgo.json /tmp/gh-aw/agent/ci-runs-cjs.json > /tmp/gh-aw/agent/ci-runs.json
      
      # Create directory for artifacts
      mkdir -p /tmp/gh-aw/agent/ci-artifacts
      
      # Download artifacts from recent successful runs across split workflows
      echo "Downloading artifacts from recent CI/cgo/cjs runs..."
      {
        gh run list --repo "$GITHUB_REPOSITORY" --workflow=ci.yml --status success --limit 2 --json databaseId
        gh run list --repo "$GITHUB_REPOSITORY" --workflow=cgo.yml --status success --limit 2 --json databaseId
        gh run list --repo "$GITHUB_REPOSITORY" --workflow=cjs.yml --status success --limit 2 --json databaseId
      } | jq -s 'add | .[].databaseId' -r | while read -r run_id; do
        echo "Processing run $run_id"
        gh run download "$run_id" --repo "$GITHUB_REPOSITORY" --dir "/tmp/gh-aw/agent/ci-artifacts/$run_id" 2>/dev/null || echo "No artifacts for run $run_id"
      done
      
      echo "CI runs data saved to /tmp/gh-aw/agent/ci-runs.json"
      echo "Artifacts saved to /tmp/gh-aw/agent/ci-artifacts/"
      
  - name: Build CI summary for optimization analysis
    run: |
      jq '
      def safe_duration:
        if (.createdAt and .updatedAt) then
          ((.updatedAt | fromdateiso8601) - (.createdAt | fromdateiso8601))
        else null end;
      {
        generated_at: now | todateiso8601,
        total_runs: length,
        status_counts: (group_by(.status) | map({status: .[0].status, count: length})),
        conclusion_counts: (map(select(.conclusion != null)) | group_by(.conclusion) | map({conclusion: .[0].conclusion, count: length})),
        branch_counts: (group_by(.headBranch) | map({branch: .[0].headBranch, count: length}) | sort_by(-.count) | .[0:10]),
        avg_duration_seconds: ([.[] | safe_duration | select(. != null)] | if length > 0 then (add / length) else null end),
        top_recent_failures: ([.[] | select(.conclusion == "failure" or .conclusion == "cancelled") | {id: .databaseId, run_number: .number, title: .displayTitle, branch: .headBranch, event: .event, url: .url, updated_at: .updatedAt}] | sort_by(.updated_at) | reverse | .[0:10])
      }' /tmp/gh-aw/agent/ci-runs.json > /tmp/gh-aw/agent/ci-summary.json

      echo "## CI Summary" >> "$GITHUB_STEP_SUMMARY"
      jq -r '"- runs analyzed: \(.total_runs)\n- avg duration (sec): \(.avg_duration_seconds // "n/a")\n- recent failure records: \(.top_recent_failures | length)"' /tmp/gh-aw/agent/ci-summary.json >> "$GITHUB_STEP_SUMMARY"
  
  - name: Setup Node.js
    uses: actions/setup-node@v7.0.0
    with:
      node-version: "24"
      cache: npm
      cache-dependency-path: actions/setup/js/package-lock.json
  
  - name: Setup Go
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: true
  
  # Pre-flight validation. All steps run non-fatally so the agent can analyze
  # broken CI state instead of the job dying before the agent starts. Each
  # step's exit code and tail of output are recorded under
  # /tmp/gh-aw/agent/validation/ and summarized in validation-status.json.
  - name: Pre-flight validation (non-fatal)
    id: preflight
    continue-on-error: true
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set +e
      mkdir -p /tmp/gh-aw/agent/validation
      STATUS_FILE=/tmp/gh-aw/agent/validation/validation-status.json
      echo '{"steps":[]}' > "$STATUS_FILE"

      run_step() {
        local name="$1"; shift
        local workdir="$1"; shift
        local log="/tmp/gh-aw/agent/validation/${name}.log"
        echo "::group::preflight: $name"
        echo "+ $*" | tee "$log"
        if [ -n "$workdir" ]; then
          (cd "$workdir" && "$@") >>"$log" 2>&1
        else
          "$@" >>"$log" 2>&1
        fi
        local code=$?
        echo "::endgroup::"
        echo "preflight: $name exit=$code"
        # tail kept short so the agent can read all logs cheaply
        tail -c 8000 "$log" > "${log}.tail"
        jq --arg n "$name" --arg log "$log" --argjson code "$code" \
           '.steps += [{name:$n, exit_code:$code, log:$log, ok:($code==0)}]' \
           "$STATUS_FILE" > "$STATUS_FILE.tmp" && mv "$STATUS_FILE.tmp" "$STATUS_FILE"
      }

      run_step deps-dev    ""                       make deps-dev
      run_step lint        ""                       make lint
      run_step lint-errors ""                       make lint-errors
      run_step npm-ci      ./actions/setup/js       npm ci
      run_step build       ""                       make build
      run_step recompile   ""                       make recompile

      mkdir -p /tmp/gh-aw/agent
      go test -v -json -count=1 -timeout=3m -tags '!integration' -run='^Test' ./... \
        | tee /tmp/gh-aw/agent/test-results.json >/dev/null
      TEST_EXIT=${PIPESTATUS[0]}
      jq --argjson code "$TEST_EXIT" \
         '.steps += [{name:"test-unit", exit_code:$code, log:"/tmp/gh-aw/agent/test-results.json", ok:($code==0)}]' \
         "$STATUS_FILE" > "$STATUS_FILE.tmp" && mv "$STATUS_FILE.tmp" "$STATUS_FILE"

      # Summary line for the agent and step summary
      OK=$(jq '[.steps[] | select(.ok)] | length' "$STATUS_FILE")
      TOTAL=$(jq '.steps | length' "$STATUS_FILE")
      FAILED=$(jq -r '[.steps[] | select(.ok|not) | .name] | join(",")' "$STATUS_FILE")
      echo "preflight summary: $OK/$TOTAL ok; failed=[$FAILED]"
      {
        echo "## Pre-flight validation"
        echo ""
        echo "- passed: $OK / $TOTAL"
        echo "- failed: ${FAILED:-none}"
      } >> "$GITHUB_STEP_SUMMARY"

      # Always succeed: the agent owns the decision about how to react.
      exit 0
---

# CI Data Analysis

Pre-downloaded CI run data and artifacts are available for analysis:

## Available Data

1. **CI Runs**: `/tmp/gh-aw/agent/ci-runs.json`
   - Last 60 workflow runs with status, timing, and metadata from `ci.yml`, `cgo.yml`, and `cjs.yml`

2. **CI Summary**: `/tmp/gh-aw/agent/ci-summary.json`
   - Pre-computed totals, failure patterns, branch distribution, and average duration
    
3. **Artifacts**: `/tmp/gh-aw/agent/ci-artifacts/`
   - Coverage reports and benchmark results from recent successful runs
   - **Fuzz test results**: `*/fuzz-results/*.txt` - Output from fuzz tests
   - **Fuzz corpus data**: `*/fuzz-results/corpus/*` - Input corpus for each fuzz test
   
4. **CI Configuration**:
   - `.github/workflows/ci.yml`
   - `.github/workflows/cgo.yml`
   - `.github/workflows/cjs.yml`
   
5. **Cache Memory**: `/tmp/gh-aw/cache-memory/`
   - Historical analysis data from previous runs
   
6. **Test Results**: `/tmp/gh-aw/agent/test-results.json`
   - JSON output from Go unit tests with performance and timing data

7. **Pre-flight Validation Status**: `/tmp/gh-aw/agent/validation/validation-status.json`
   - Per-step exit codes for `deps-dev`, `lint`, `lint-errors`, `npm-ci`, `build`, `recompile`, `test-unit`
   - Per-step logs at `/tmp/gh-aw/agent/validation/<step>.log` (full) and `<step>.log.tail` (last 8KB)
   - Steps are run non-fatally — if any of them failed, **that is itself a high-priority CI problem** the coach should investigate and fix.

## Test Case Locations

Go test cases are located throughout the repository:
- **Command tests**: `./cmd/gh-aw/*_test.go`
- **Workflow tests**: `./pkg/workflow/*_test.go`
- **CLI tests**: `./pkg/cli/*_test.go`
- **Parser tests**: `./pkg/parser/*_test.go`
- **Campaign tests**: `./pkg/campaign/*_test.go`
- **Other package tests**: Various `./pkg/*/test.go` files

## Environment Setup

The workflow has attempted (non-fatally) to:
- Install dev dependencies (`make deps-dev`)
- Lint (`make lint`, `make lint-errors`)
- Install JS deps (`npm ci` in `actions/setup/js`)
- Build (`make build`)
- Recompile lock files (`make recompile`)
- Run unit tests (`go test ... -tags '!integration'`)

**Each step may have failed**. Check `/tmp/gh-aw/agent/validation/validation-status.json` first:

```bash
jq '.steps[] | {name, exit_code, ok}' /tmp/gh-aw/agent/validation/validation-status.json
# For any failed step, read the tail of its log:
cat /tmp/gh-aw/agent/validation/<step>.log.tail
```

If pre-flight validation is broken, fixing it is the most valuable thing the coach can do this run — it unblocks all future CI runs. You can still:
- Make changes to code or configuration files
- Re-run only the steps you affected (e.g., `make lint` after JS edits, `make recompile` after workflow markdown edits)
- Ensure proposed changes don't break functionality before creating a PR

## Analyzing Run Data

Start with the pre-computed summary:

```bash
cat /tmp/gh-aw/agent/ci-summary.json | jq .
```

Only use raw run data for deeper validation:

```bash
# Analyze run data
cat /tmp/gh-aw/agent/ci-runs.json | jq '
{
  total_runs: length,
  by_status: group_by(.status) | map({status: .[0].status, count: length}),
  by_conclusion: group_by(.conclusion) | map({conclusion: .[0].conclusion, count: length}),
  by_branch: group_by(.headBranch) | map({branch: .[0].headBranch, count: length}),
  by_event: group_by(.event) | map({event: .[0].event, count: length})
}'
```

**Metrics to extract:**
- Success rate per job
- Average duration per job
- Failure patterns (which jobs fail most often)
- Cache hit rates from step summaries
- Resource usage patterns

## Review Artifacts

Examine downloaded artifacts for insights:

```bash
# List downloaded artifacts
find /tmp/gh-aw/agent/ci-artifacts -type f -name "*.txt" -o -name "*.html" -o -name "*.json"

# Analyze coverage reports if available
# Check benchmark results for performance trends
```

## Historical Context

Check cache memory for previous analyses:

```bash
# Read previous optimization recommendations
if [ -f /tmp/gh-aw/cache-memory/ci-coach/last-analysis.json ]; then
  cat /tmp/gh-aw/cache-memory/ci-coach/last-analysis.json
fi

# Check if previous recommendations were implemented
# Compare current metrics with historical baselines
```
