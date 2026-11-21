---
private: true
emoji: "🔧"
name: Daily Go Function Namer
description: Analyzes one Go package per day with Serena and implements validated internal function renames in a bounded draft PR, rotating packages via cache-memory
intent: Improve Go function discoverability for coding agents without changing behavior or duplicating existing work.
on:
  schedule: daily
  workflow_dispatch:
  skip-if-match: 'is:pr is:open in:title "[function-namer]"'

max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write

tracker-id: daily-function-namer

model: router/economy
engine:
  id: pi
  model-provider: github
  env:
    GOCACHE: /tmp/gh-aw/agent/function-namer-go-build
    GOMODCACHE: /tmp/gh-aw/agent/function-namer-go-mod
imports:
  - shared/mcp/serena-go.md
  - shared/otlp.md
  - shared/reporting.md
safe-outputs:
  threat-detection:
    report-as-issue: false
  report-failure-as-issue: false
  report-failed-jobs: false
  report-missing-tool:
    create-issue: false
  report-missing-data:
    create-issue: false
  create-pull-request:
    title-prefix: "[function-namer] "
    labels: [refactoring, code-quality, automated-analysis, cookie]
    draft: true
    max: 1
    expires: 7d
    if-no-changes: ignore
    fallback-as-issue: false
    github-token-for-extra-empty-commit: none
    protected-files: blocked
    allowed-files:
      - "pkg/**/*.go"
    max-patch-files: 10
    max-patch-size: 100
  noop:
    report-as-issue: false
  report-incomplete:
    create-issue: false

sandbox:
  agent:
    id: awf
runtimes:
  go: {}
network:
  allowed: [defaults, go]
tools:
  cli-proxy: true
  cache-memory: true
  github:
    mode: gh-proxy
    toolsets: [default, issues, pull_requests]
  edit:
  bash: true

timeout-minutes: 30
strict: true
features:
  gh-aw-detection: true
evals:
  - id: package_analyzed
    question: Did the agent analyze one real Go package's naming, or stop safely for duplicates or missing semantic evidence?
  - id: pr_created_or_noop
    question: Did the agent create at most one draft PR with validated internal Go renames, or call noop with a concrete reason when no safe novel change was available?
  - id: rename_guardrails
    question: Did the agent preserve behavior/APIs, avoid generated files/fixtures, and withhold unverified or out-of-scope changes?
---

# Daily Go Function Namer

Analyze **one entire real Go package** per run in round-robin order under `pkg/`.
Implement at most **three clearly justified internal function renames** and all
their references in **one draft PR**, never an issue or discussion.

## Mandatory guardrails

- Make pure identifier renames only: no behavior, signatures, exported APIs,
  dependency, workflow, configuration, or unrelated refactoring changes.
- Rename only unexported functions, not methods, interface contracts,
  constructors (`New*`, `Make*`), callbacks/closures, or descriptive names.
- Analyze non-test Go files; update test references with bash/edit as needed
  (the Serena import excludes test-file analysis). All edited files must be
  tracked, non-generated `.go` files in the selected package: at most 10 files
  and 100 KiB. Never edit `testdata/`, fixtures, vendor, or other packages.
- Require Serena semantic evidence for the declaration and every reference.
  Search repository-wide, including tests, for textual references, build tags,
  reflection, string-based lookup, and generated callers. Skip uncertain
  references/constraints, collisions, or out-of-scope callers.
- Keep GitHub access read-only. Publish only through `create_pull_request`.
  Do not merge, dispatch workflows, assign/close issues, or fall back to issues.
- Before editing and again before publication, check open `[function-namer]`
  PRs and open issues/PRs covering the package or proposed old/new names. If a
  PR or assigned issue already covers the work, call `noop` with its URL.
  For unassigned legacy plans, verify suggestions against current code and
  check timelines for linked PRs. Implement only verified, uncovered candidates;
  cite the issue with `Relates to #N`, never a closing keyword.
- If analysis finds no meaningful rename, call `noop`. If semantic tools,
  duplicate checks, scope, or validation fail, restore only your candidate
  edits, call `noop` with the blocker, and stop. Never treat a failed command
  as successful analysis, publish unvalidated edits, or bypass denials.

## 1. Select the package

Repository: `${{ github.repository }}`. Workspace: `${{ github.workspace }}`.
Date: `date +%Y-%m-%d`.
Cache: `/tmp/gh-aw/cache-memory/function-namer-state.json`.

Run the [Package selection script](#package-selection-script). Use its outputs
directly; do not recompute them.

**On cold start** (the state file is missing), treat this as expected
initialization. Do not call `report_missing_data`. Run the package selection
script as written and accept its random starting package.
Malformed cache: stop explicitly, never silently reset.

## 2. Inventory and semantic analysis

Run `grep -n "^func "` once on the listed files. Use Serena `activate_project`
with the workspace path, then `get_symbols_overview` and bounded `read_file`
ranges on each selected non-generated file. Avoid repeated searches.

Prefer clear verb+noun names discoverable from a task description. Look for
generic verbs, obscure abbreviations, implementation details, or mismatched
behavior. Reject stylistic swaps like `makeConfig` to `createConfig`.

Use `find_referencing_symbols`; inspect declarations/callers and apply guardrails.

## 3. Implement and validate

Record initial worktree state. Rename declarations, callers, and related
doc-comment identifiers. Preserve behavior and tests; do not add name-only tests.

Run `git diff --check` and `git diff --stat`. Review the full diff for only
approved identifier/comment changes in the selected package. Search old names
again and explain unrelated matches.
Run `gofmt` on modified files, `go test "./$PKG_DIR" -count=1`,
`make build`, `make test-unit`, and `make lint`. Require every command to pass.
Record results. Use configured Go caches. Never change dependencies or
validation rules to pass.

Restore out-of-scope validation outputs before final review. On failure revert
only your edits, never pre-existing work.

## 4. Persist completed analysis

After full analysis, including a genuine no-candidate result, save
`last_package_index` as `new_last_package_index` from Step 1 and append
`{"package":"<selected_package>","analyzed_at":"<YYYY-MM-DD>"}` to
`analyzed_packages`. Keep only the most recent 30 entries. Preserve existing
entries. If the state file was missing at the start of the run, initialize it
from scratch here instead of reporting missing cache data.
Do not advance state on incomplete analysis, duplicate blocking, or failed
validation. Cache state is not a repository change and must not enter the PR.

## 5. Publish or noop

Recheck duplicates and the diff. Stage only verified paths and commit locally.
Call `create_pull_request` once for a draft titled `Go function renames: <selected_package>`.
Include package/date/rotation/function counts, rename rationale table, reference
completeness, validation commands and results, skips, and related legacy issue
URL. Use `###` or lower headers and collapse long evidence with `<details>`.

Otherwise call `noop` with the package and reason.

<!-- instruction-prefix-end -->

## Package selection script

```bash
LAST_INDEX=$(python3 -c "
import json, os, sys
p = '/tmp/gh-aw/cache-memory/function-namer-state.json'
if not os.path.exists(p):
    print(-1)
else:
    try:
        d = json.load(open(p))
        i = d.get('last_package_index', -1)
        if type(i) is not int or i < -1:
            raise ValueError('invalid last_package_index')
        print(i)
    except (ValueError, TypeError) as e:
        print('Invalid function-namer cache: ' + str(e), file=sys.stderr)
        sys.exit(1)
") || exit 1

mapfile -t ALL_PKGS < <(find pkg -type d \( -name testdata -o -name vendor \) -prune -o -name '*.go' ! -name '*_test.go' -type f -print | xargs -r -I{} dirname {} | sort -u)
TOTAL=${#ALL_PKGS[@]}
if [ "$TOTAL" -eq 0 ]; then
  echo "ERROR: no Go packages found in pkg/" >&2
  exit 1
fi
if [ "$LAST_INDEX" -eq -1 ]; then
  LAST_INDEX=$(( RANDOM % TOTAL ))
  echo "cache_miss=true (random start at index ${LAST_INDEX})"
fi
PKG_DIR="${ALL_PKGS[$(( LAST_INDEX % TOTAL ))]}"
NEW_INDEX=$(( (LAST_INDEX + 1) % TOTAL ))
mapfile -t PKG_FILES < <(find "$PKG_DIR" -maxdepth 1 -name '*.go' ! -name '*_test.go' -type f | sort)
FILE_COUNT=${#PKG_FILES[@]}
TOTAL_FNS=$(grep -hc "^func " "${PKG_FILES[@]}" | awk '{s+=$1} END {print s+0}')
echo "total_packages=${TOTAL}"
echo "last_package_index=${LAST_INDEX}"
echo "selected_package=${PKG_DIR}"
echo "file_count=${FILE_COUNT}"
echo "new_last_package_index=${NEW_INDEX}"
echo "total_functions_approx=${TOTAL_FNS}"
echo "--- selected files ---"
printf '%s\n' "${PKG_FILES[@]}"
```
