---
emoji: "🏗️"
name: Architecture Guardian
description: Daily analysis of commits from the last 24 hours to detect code structure violations in Go and JavaScript files, such as large files, oversized functions, high export counts, and import cycles
on:
  schedule: "daily around 14:00 on weekdays"  # ~2 PM UTC, weekdays only
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
tracker-id: architecture-guardian
imports:
  - uses: shared/skip-if-issue-open.md
    with:
      title-prefix: "[architecture-guardian]"
  - uses: shared/daily-issue-base.md
    with:
      title-prefix: "[architecture-guardian] "
      expires: "2d"
      labels: [architecture, automated-analysis, cookie]
      assignees: [copilot]
  - shared/otlp.md
  - shared/graders.md
tools:
  cli-proxy: true
  bash:
    - "cat:*"
safe-outputs:
  messages:
    footer: "> 🏛️ *Architecture report by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    footer-workflow-recompile: "> 🛠️ *Workflow maintenance by [{workflow_name}]({run_url}) for {repository}*"
    run-started: "🏛️ Architecture Guardian scanning code structure on `{event_type}`. [{workflow_name}]({run_url})"
    run-success: "✅ Architecture scan complete. [{workflow_name}]({run_url}) reviewed code structure and delivered a report."
    run-failure: "❌ Architecture scan failed. [{workflow_name}]({run_url}) {status}. Scan did not complete."
timeout-minutes: 20
steps:
  - name: Collect architecture metrics
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent

      # Read thresholds from .architecture.yml or use defaults
      FILE_LINES_BLOCKER=1000
      FILE_LINES_WARNING=500
      FUNCTION_LINES=80
      MAX_EXPORTS=10

      if [ -f .architecture.yml ]; then
        b=$(grep -E '^\s*file_lines_blocker:' .architecture.yml 2>/dev/null | awk '{print $2}' | tr -d '"' | head -1 || true)
        w=$(grep -E '^\s*file_lines_warning:' .architecture.yml 2>/dev/null | awk '{print $2}' | tr -d '"' | head -1 || true)
        f=$(grep -E '^\s*function_lines:' .architecture.yml 2>/dev/null | awk '{print $2}' | tr -d '"' | head -1 || true)
        e=$(grep -E '^\s*max_exports:' .architecture.yml 2>/dev/null | awk '{print $2}' | tr -d '"' | head -1 || true)
        [[ -n "${b:-}" && "$b" =~ ^[0-9]+$ ]] && FILE_LINES_BLOCKER=$b
        [[ -n "${w:-}" && "$w" =~ ^[0-9]+$ ]] && FILE_LINES_WARNING=$w
        [[ -n "${f:-}" && "$f" =~ ^[0-9]+$ ]] && FUNCTION_LINES=$f
        [[ -n "${e:-}" && "$e" =~ ^[0-9]+$ ]] && MAX_EXPORTS=$e
      fi

      # Get changed Go/JS files in last 24 hours, excluding tests and vendor paths
      CHANGED_FILES=$(git log --since="24 hours ago" --name-only --pretty=format: \
        | sort -u \
        | grep -E '\.(go|js|cjs|mjs)$' \
        | grep -vE '(node_modules/|vendor/|\.git/|_test\.go$)' \
        | while IFS= read -r f; do [ -f "$f" ] && echo "$f"; done \
        || true)

      if [ -z "$CHANGED_FILES" ]; then
        jq -n \
          --argjson blocker "$FILE_LINES_BLOCKER" \
          --argjson warning "$FILE_LINES_WARNING" \
          --argjson func_lines "$FUNCTION_LINES" \
          --argjson max_exports "$MAX_EXPORTS" \
          '{noop: true, thresholds: {file_lines_blocker: $blocker, file_lines_warning: $warning, function_lines: $func_lines, max_exports: $max_exports}, files: [], import_cycles: ""}' \
          > /tmp/gh-aw/agent/arch-metrics.json
        echo "No changed Go/JS files found in the last 24 hours."
        exit 0
      fi

      # Build file metrics array
      FILES_JSON="[]"
      while IFS= read -r FILE; do
        [ -z "$FILE" ] && continue
        LINES=$(wc -l < "$FILE" 2>/dev/null | tr -d ' ' || echo 0)
        EXT="${FILE##*.}"

        if [[ "$EXT" == "go" ]]; then
          # Function sizes: "func declaration\tline_count" per function
          # Pattern matches both regular functions (^func Name) and receiver methods (^func (r *T) Name)
          FUNC_DATA=$(awk '/^func /{if(start>0 && name!="") printf "%s\t%d\n", name, NR-start; name=$0; start=NR} END{if(start>0 && name!="") printf "%s\t%d\n", name, NR-start+1}' "$FILE" 2>/dev/null | head -50 || true)
          # Export count and names (top-level exported identifiers start with uppercase)
          EXPORT_COUNT=$(grep -cE "^func [A-Z]|^type [A-Z]|^var [A-Z]|^const [A-Z]" "$FILE" 2>/dev/null || echo 0)
          EXPORT_NAMES=$(grep -nE "^func [A-Z]|^type [A-Z]|^var [A-Z]|^const [A-Z]" "$FILE" 2>/dev/null | head -20 || true)
        else
          # JS/CJS/MJS: capture named functions, arrow functions, and class methods
          FUNC_DATA=$(grep -nE "^function |^const [a-zA-Z_$][a-zA-Z0-9_$]* = (function|\(|async \(|async function)|^(export (default )?function|export const [a-zA-Z_$][a-zA-Z0-9_$]* =)|^[a-zA-Z_$][a-zA-Z0-9_$]*\s*\([^)]*\)\s*\{" "$FILE" 2>/dev/null | head -50 || true)
          CJS_COUNT=$(grep -cE "^module\.exports|^exports\." "$FILE" 2>/dev/null || echo 0)
          ESM_COUNT=$(grep -cE "^export " "$FILE" 2>/dev/null || echo 0)
          EXPORT_COUNT=$((CJS_COUNT + ESM_COUNT))
          EXPORT_NAMES=$(grep -nE "^export |^module\.exports|^exports\." "$FILE" 2>/dev/null | head -20 || true)
        fi

        FILES_JSON=$(jq \
          --arg file "$FILE" \
          --argjson lines "$LINES" \
          --argjson exports "$EXPORT_COUNT" \
          --arg func_data "${FUNC_DATA:-}" \
          --arg export_names "${EXPORT_NAMES:-}" \
          '. + [{file: $file, lines: $lines, export_count: $exports, func_data: $func_data, export_names: $export_names}]' \
          <<< "$FILES_JSON")
      done <<< "$CHANGED_FILES"

      # Check Go import cycles once across all packages
      # Note: go list may also emit errors for syntax issues; grep filters to only cycle errors
      IMPORT_CYCLES=$(go list ./... 2>&1 | grep -iE "import cycle|cycle not allowed" || true)

      jq -n \
        --argjson blocker "$FILE_LINES_BLOCKER" \
        --argjson warning "$FILE_LINES_WARNING" \
        --argjson func_lines "$FUNCTION_LINES" \
        --argjson max_exports "$MAX_EXPORTS" \
        --argjson files "$FILES_JSON" \
        --arg import_cycles "$IMPORT_CYCLES" \
        '{noop: false, thresholds: {file_lines_blocker: $blocker, file_lines_warning: $warning, function_lines: $func_lines, max_exports: $max_exports}, files: $files, import_cycles: $import_cycles}' \
        > /tmp/gh-aw/agent/arch-metrics.json

      FILE_COUNT=$(echo "$CHANGED_FILES" | wc -l | tr -d ' ')
      echo "✅ Pre-computed metrics for $FILE_COUNT file(s) → /tmp/gh-aw/agent/arch-metrics.json"
features:
  gh-aw-detection: true
evals:
  - id: violations-analyzed
    question: Did the workflow analyze changed Go or JavaScript files from the last 24 hours for architecture violations, or correctly conclude there were none?
  - id: issue-created-or-noop
    question: Was an architecture report issue created when violations were found, or was noop or skip behavior used appropriately when there was nothing actionable?

---

# Architecture Guardian

You are the Architecture Guardian, a code quality agent that enforces structural discipline in the codebase. Your mission is to prevent "spaghetti code" by detecting structural violations in commits landed in the last 24 hours before they accumulate.

## Current Context

- **Repository**: ${{ github.repository }}
- **Analysis Period**: Last 24 hours
- **Run ID**: ${{ github.run_id }}

## Step 1: Read Pre-Computed Metrics

All file metrics have been collected by the pre-step. Read the JSON summary:

```bash
cat /tmp/gh-aw/agent/arch-metrics.json
```

The JSON has this structure:
- `noop` (bool) — `true` when no Go/JS files changed in the last 24 hours
- `thresholds` — effective thresholds (from `.architecture.yml` or defaults)
- `files[]` — one entry per changed file with:
  - `file` — file path
  - `lines` — total line count
  - `export_count` — number of exported identifiers
  - `func_data` — function declarations with sizes (`name\tline_count` per line for Go; line numbers for JS)
  - `export_names` — list of exported identifier declarations
- `import_cycles` — output of `go list ./...` filtered for cycle errors (empty if none)

If `noop` is `true`, call the `noop` safe-output tool and stop:

```json
{"noop": {"message": "No Go or JavaScript source files changed in the last 24 hours. Architecture scan skipped."}}
```

## Step 2: Classify Violations by Severity

Read the metrics JSON already loaded in Step 1. Apply the following rules using the `thresholds` values directly:

- **BLOCKER**: `import_cycles` non-empty → import cycle; `files[].lines > thresholds.file_lines_blocker` → oversized file
- **WARNING**: `files[].lines > thresholds.file_lines_warning` → near-limit file; Go `func_data` entries with line count > `thresholds.function_lines` → oversized function
- **INFO**: `files[].export_count > thresholds.max_exports` → excessive exports

Build `blockers`, `warnings`, and `infos` arrays from this analysis and proceed to Step 3.

## Step 3: Post Report

### If NO violations are found

Call the `noop` safe-output tool:

```json
{"noop": {"message": "No architecture violations found in the last 24 hours. All changed files are within configured thresholds."}}
```

### If violations are found

Create an issue with a structured report. Only create ONE issue (the `max: 1` limit applies and an existing open issue skips the run via `skip-if-match`).

Use the `blockers`, `warnings`, and `infos` arrays built in Step 2 to populate the violation rows in each section. Replace all `[PLACEHOLDER]` values with actual data, and replace `N` with actual counts.

**Issue title**: Architecture Violations Detected — [DATE]

**Issue body format**:

```markdown
### Summary

- **Analysis Period**: Last 24 hours
- **Files Analyzed**: [NUMBER]
- **Total Violations**: [NUMBER]
- **Date**: [DATE]

| Severity | Count |
|----------|-------|
| 🚨 BLOCKER | N |
| ⚠️ WARNING | N |
| ℹ️ INFO | N |

---

### 🚨 BLOCKER Violations

> These violations indicate serious structural problems that require prompt attention.

- `path/to/file.go` — N lines (limit: 1000) · **Fix**: split into focused sub-files, one responsibility per file
- Import cycle detected: [cycle description] · **Fix**: introduce an interface or move shared types to a lower-level package

---

### ⚠️ WARNING Violations

> These violations should be addressed soon to prevent further structural debt.

- `path/to/file.go` — N lines (limit: 500) · **Fix**: extract related functions into a new file
- `path/to/file.go::FunctionName` — N lines (limit: 80) · **Fix**: decompose into smaller helper functions

---

### ℹ️ INFO Violations

> Informational findings. Consider addressing in future refactoring.

- `path/to/file.go`: N exported identifiers (limit: 10) — consider splitting into focused packages

---

### Configuration

Thresholds (from `.architecture.yml` or defaults):
- File size BLOCKER: N lines
- File size WARNING: N lines
- Function size: N lines
- Max public exports: N

### Action Checklist

- [ ] Review all BLOCKER violations and plan refactoring
- [ ] Address WARNING violations in upcoming PRs
- [ ] Consider splitting INFO modules if they grow further
- [ ] Close this issue once all violations are resolved

> 🏛️ *To configure thresholds, add a `.architecture.yml` file to the repository root.*
```
