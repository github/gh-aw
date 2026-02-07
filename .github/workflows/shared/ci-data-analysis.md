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
  - shared/jqschema.md

tools:
  cache-memory: true
  bash: ["*"]

steps:
  - name: Download CI workflow runs from last 7 days
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      # Download workflow runs for the ci workflow
      gh run list --repo ${{ github.repository }} --workflow=ci.yml --limit 100 --json databaseId,status,conclusion,createdAt,updatedAt,displayTitle,headBranch,event,url,workflowDatabaseId,number > /tmp/ci-runs.json
      
      # Create directory for artifacts
      mkdir -p /tmp/ci-artifacts
      
      # Download artifacts from recent runs (last 5 successful runs)
      echo "Downloading artifacts from recent CI runs..."
      gh run list --repo ${{ github.repository }} --workflow=ci.yml --status success --limit 5 --json databaseId | jq -r '.[].databaseId' | while read -r run_id; do
        echo "Processing run $run_id"
        gh run download "$run_id" --repo ${{ github.repository }} --dir "/tmp/ci-artifacts/$run_id" 2>/dev/null || echo "No artifacts for run $run_id"
      done
      
      echo "CI runs data saved to /tmp/ci-runs.json"
      echo "Artifacts saved to /tmp/ci-artifacts/"
      
      # Summarize downloaded artifacts
      echo "## Downloaded Artifacts" >> "$GITHUB_STEP_SUMMARY"
      find /tmp/ci-artifacts -type f -name "*.txt" -o -name "*.html" -o -name "*.json" | head -20 | while read -r f; do
        echo "- $(basename "$f")" >> "$GITHUB_STEP_SUMMARY"
      done
  
  - name: Setup Node.js
    uses: actions/setup-node@v6
    with:
      node-version: "24"
      cache: npm
      cache-dependency-path: actions/setup/js/package-lock.json
  
  - name: Setup Go
    uses: actions/setup-go@v6
    with:
      go-version-file: go.mod
      cache: true
  
  - name: Install dev dependencies
    run: make deps-dev
  
  - name: Run linter
    run: make lint
  
  - name: Lint error messages
    run: make lint-errors
  
  - name: Install npm dependencies
    run: npm ci
    working-directory: ./actions/setup/js
  
  - name: Build code
    run: make build
  
  - name: Rebuild lock files
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: make recompile
  
  - name: Run unit tests
    continue-on-error: true
    run: |
      mkdir -p /tmp/gh-aw
      go test -v -json -count=1 -timeout=3m -tags '!integration' -run='^Test' ./... | tee /tmp/gh-aw/test-results.json
---

# CI Data Analysis

Pre-downloaded CI run data and artifacts are available for analysis:

## Available Data

1. **CI Runs**: `/tmp/ci-runs.json`
   - Last 100 workflow runs with status, timing, and metadata
   
2. **Artifacts**: `/tmp/ci-artifacts/`
   - Coverage reports and benchmark results from recent successful runs
   - **Fuzz test results**: `*/fuzz-results/*.txt` - Output from fuzz tests
   - **Fuzz corpus data**: `*/fuzz-results/corpus/*` - Input corpus for each fuzz test
   
3. **CI Configuration**: `.github/workflows/ci.yml`
   - Current CI workflow configuration
   
4. **Cache Memory**: `/tmp/cache-memory/`
   - Historical analysis data from previous runs
   
5. **Test Results**: `/tmp/gh-aw/test-results.json`
   - JSON output from Go unit tests with performance and timing data

## Test Case Locations

Go test cases are located throughout the repository:
- **Command tests**: `./cmd/gh-aw/*_test.go`
- **Workflow tests**: `./pkg/workflow/*_test.go`
- **CLI tests**: `./pkg/cli/*_test.go`
- **Parser tests**: `./pkg/parser/*_test.go`
- **Campaign tests**: `./pkg/campaign/*_test.go`
- **Other package tests**: Various `./pkg/*/test.go` files

## Environment Setup

The workflow has already completed:
- ✅ **Linting**: Dev dependencies installed, linters run successfully
- ✅ **Building**: Code built with `make build`, lock files compiled with `make recompile`
- ✅ **Testing**: Unit tests run (with performance data collected in JSON format)

This means you can:
- Make changes to code or configuration files
- Validate changes immediately by running `make lint`, `make build`, or `make test-unit`
- Ensure proposed optimizations don't break functionality before creating a PR

## Analyzing Run Data

Parse the downloaded CI runs data:

```bash
# Analyze run data
cat /tmp/ci-runs.json | jq '
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
find /tmp/ci-artifacts -type f -name "*.txt" -o -name "*.html" -o -name "*.json"

# Analyze coverage reports if available
# Check benchmark results for performance trends
```

## Historical Context

Check cache memory for previous analyses:

```bash
# Read previous optimization recommendations
if [ -f /tmp/cache-memory/ci-coach/last-analysis.json ]; then
  cat /tmp/cache-memory/ci-coach/last-analysis.json
fi

# Check if previous recommendations were implemented
# Compare current metrics with historical baselines
```
