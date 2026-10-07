---
private: true
on:
  schedule: daily
  workflow_dispatch: null
permissions:
  contents: read
  issues: read
  pull-requests: read
env:
  GOTOOLCHAIN: auto
network:
  allowed:
    - defaults
    - go


imports:
- shared/reporting.md
- shared/otlp.md
safe-outputs:
  steer: true
  create-pull-request:
    draft: false
    expires: 2d
    labels:
    - enhancement
    - automation
    title-prefix: "[log] "
steps:
- name: Build deterministic logger manifest
  id: logger_manifest
  run: |
    set -euo pipefail
    cache_dir="/tmp/gh-aw/cache-memory/go-logger"
    out_dir="/tmp/gh-aw/agent/go-logger"
    mkdir -p "$cache_dir" "$out_dir"

    current_sha="$(git rev-parse HEAD)"
    current_files="$out_dir/current-files.txt"
    processed_files="$cache_dir/processed-files.txt"
    find pkg -name '*.go' -type f ! -name '*_test.go' | sort > "$current_files"
    [ -f "$processed_files" ] || : > "$processed_files"

    new_files="$out_dir/new-files.txt"
    comm -23 "$current_files" "$processed_files" > "$new_files" || true

    last_sha=""
    if [ -f "$cache_dir/last-run.json" ]; then
      last_sha="$(jq -r '.commit_sha // empty' "$cache_dir/last-run.json" 2>/dev/null || true)"
    fi

    files_needing_logger="$out_dir/files-needing-logger.txt"
    files_missing_logger_import="$out_dir/files-missing-logger-import.txt"
    call_sites="$out_dir/call-sites.tsv"
    : > "$files_needing_logger"
    : > "$files_missing_logger_import"
    : > "$call_sites"

    while IFS= read -r rel; do
      [ -f "$rel" ] || continue
      if ! grep -q "var log = logger.New" "$rel"; then
        echo "$rel" >> "$files_needing_logger"
      fi
      if grep -q "log\\." "$rel" && ! grep -q '"github.com/github/gh-aw/pkg/logger"' "$rel"; then
        echo "$rel" >> "$files_missing_logger_import"
      fi
    done < "$current_files"

    # Bound manifest size: the agent only ever selects up to 5 files per PR, so
    # candidate call sites (the bulk of manifest.json) are only computed for a
    # capped pool of candidates instead of every file in pkg/. This keeps
    # manifest.json well under the 256KB / 25K-token read limits regardless of
    # how large the repository grows. New files (not yet processed) are
    # prioritized so they surface first.
    candidate_pool_cap=40
    call_sites_per_file_cap=10
    candidate_files="$out_dir/candidate-files.txt"
    { comm -12 "$new_files" "$files_needing_logger" 2>/dev/null || true
      comm -23 "$files_needing_logger" "$new_files" 2>/dev/null || true
    } | awk '!seen[$0]++' | head -n "$candidate_pool_cap" > "$candidate_files" || true

    while IFS= read -r rel; do
      [ -f "$rel" ] || continue
      while IFS=: read -r line_number match_line; do
        function_name="$(printf '%s' "$match_line" | sed -E 's/^[[:space:]]*func[[:space:]]+([A-Za-z0-9_]+).*/\\1/')"
        printf "%s\\t%s\\t%s\\n" "$rel" "$line_number" "$function_name" >> "$call_sites"
      done < <(grep -nE "^[[:space:]]*func[[:space:]]+[A-Za-z0-9_]+" "$rel" | head -n "$call_sites_per_file_cap" || true)
    done < "$candidate_files"

    # Write each JSON payload to a file to avoid exceeding ARG_MAX with large datasets.
    jq -R -s 'split("\n") | map(select(length > 0))' "$files_needing_logger" \
      > "$out_dir/files-needing-logger.json"
    jq -R -s 'split("\n") | map(select(length > 0))' "$files_missing_logger_import" \
      > "$out_dir/files-missing-logger-import.json"
    jq -R -s 'split("\n") | map(select(length > 0) | split("\t") | {file: .[0], line: (.[1] | tonumber), function: .[2]})' "$call_sites" \
      > "$out_dir/candidate-call-sites.json"
    jq -R -s 'split("\n") | map(select(length > 0))' "$candidate_files" \
      > "$out_dir/candidate-files.json"

    files_needing_logger_count="$(wc -l < "$files_needing_logger" | tr -d ' ')"

    # Build manifest by reading files via --slurpfile (no large args on argv).
    # `files_needing_logger` is capped to `candidate_files` (the bounded pool
    # analyzed above) so the agent's source-of-truth list matches the
    # candidates with call sites; `files_needing_logger_total` reports the
    # full uncapped backlog size for visibility.
    jq -n \
      --slurpfile files_needing_logger "$out_dir/candidate-files.json" \
      --slurpfile missing_logger_import "$out_dir/files-missing-logger-import.json" \
      --slurpfile candidate_call_sites "$out_dir/candidate-call-sites.json" \
      --argjson files_needing_logger_total "$files_needing_logger_count" \
      '{files_needing_logger: $files_needing_logger[0], files_needing_logger_total: $files_needing_logger_total, missing_logger_import: $missing_logger_import[0], candidate_call_sites: $candidate_call_sites[0]}' \
      > "$out_dir/manifest.json"

    # Defense in depth: if manifest.json is still unexpectedly large (e.g. a
    # pathological file with very long lines), drop the largest field rather
    # than let the agent retry oversized reads until the step times out.
    manifest_size="$(wc -c < "$out_dir/manifest.json" | tr -d ' ')"
    manifest_size_limit=200000
    if [ "$manifest_size" -gt "$manifest_size_limit" ]; then
      echo "::warning::manifest.json size ($manifest_size bytes) exceeds $manifest_size_limit bytes; dropping candidate_call_sites" >&2
      jq -c 'del(.candidate_call_sites) | .candidate_call_sites_truncated = true' "$out_dir/manifest.json" \
        > "$out_dir/manifest.json.tmp"
      mv "$out_dir/manifest.json.tmp" "$out_dir/manifest.json"
    fi

    should_run=true
    if [ "$current_sha" = "$last_sha" ] && [ ! -s "$new_files" ]; then
      should_run=false
    fi
    echo "{\"should_run\": \"$should_run\", \"current_sha\": \"$current_sha\", \"last_sha\": \"$last_sha\", \"manifest\": \"$out_dir/manifest.json\", \"new_files\": \"$new_files\"}" > "$out_dir/preflight.json"
    echo "should_run=$should_run" >> "$GITHUB_OUTPUT"
- name: Setup Node.js
  uses: actions/setup-node@v7.0.0
  with:
    cache: npm
    cache-dependency-path: actions/setup/js/package-lock.json
    node-version: "24"
- name: Setup Go
  uses: actions/setup-go@v7.0.0
  with:
    cache: true
    go-version-file: go.mod
- name: Install npm dependencies
  run: npm ci
  working-directory: ./actions/setup/js
description: Analyzes and enhances Go logging practices across the codebase for improved debugging and observability
emoji: 📝
engine: claude
name: Go Logger Enhancement
timeout-minutes: 30
tools:
  bash:
  - cat /tmp/gh-aw/agent/go-logger/preflight.json
  - cat /tmp/gh-aw/agent/go-logger/manifest.json
  - cat /tmp/gh-aw/agent/go-logger/new-files.txt
  - make build
  - make fmt
  - make recompile
  - ./gh-aw compile
  - git
  cache-memory: null
  cli-proxy: true
  edit: null
  github:
    mode: gh-proxy
    toolsets:
    - default
evals:
  - id: pr-created
    question: Did the agent create a pull request with Go logging improvements?
  - id: logging-changes-made
    question: Does the agent output confirm that logging statements were added or modified in Go source files?
  - id: validation-run
    question: Does the agent output show that it ran validation commands to verify the logging changes compile correctly?
---

# Go Logger Enhancement

You are an AI agent that improves Go code by adding debug logging statements to help with troubleshooting and development.

## Validation Commands

Use **bash** for all build and validation commands in this workflow to avoid MCP connection timeouts during long file-exploration phases.

```bash
make build && make fmt       # Build the project and check formatting
make recompile               # Recompile workflows only if you changed .md files
```

## Efficiency First: Use Pre-flight Outputs

Before analyzing files, read `/tmp/gh-aw/agent/go-logger/preflight.json` and `/tmp/gh-aw/agent/go-logger/manifest.json`.

- The pre-flight step already computed whether this run should proceed.
- `manifest.json` is deliberately capped to a bounded pool of candidate files (with their call sites) — it never contains the full repository backlog, so it should always be small enough to read in one shot. `files_needing_logger_total` reports the full uncapped backlog count for context.
- If a file read ever reports the content is too large or exceeds a token limit (including for `manifest.json` itself), do **not** retry with other tools (`jq`, `cd`, `cp`, `wc`, etc.) — those are not permitted outside the working directory and will not succeed. Immediately retry the same read with an `offset`/`limit` (or equivalent chunked read) as the error message suggests, or fall back to the `cat` bash command already allow-listed for this file.
- If cache files are missing (cold cache / first run), treat that as expected and continue.
- Only report `missing_data` when cache files exist but are unreadable/corrupted.
- Update cache after processing:
  - Save list of processed files to `processed-files.txt`
  - Save current commit SHA to `last-run.json`
  - Save summary of changes made

## Mission

Add meaningful debug logging calls to Go files in the `pkg/` directory following the project's logging guidelines from AGENTS.md.

## Important Constraints

1. **Maximum 5 files per pull request** - Keep changes focused and reviewable
2. **Skip test files** - Never modify files ending in `_test.go`
3. **No side effects** - Logger arguments must NOT compute anything or cause side effects
4. **Follow logger naming convention** - Use `pkg:filename` pattern (e.g., `workflow:compiler`)

## Logger Guidelines from AGENTS.md

Read the **Debug Logging** section of `AGENTS.md` with the read or bash tools, then follow its logger naming convention (`pkg:filename`), usage patterns, and "When to Add Logging" guidance.

## Task Steps

### 1. Read Deterministic Candidate Manifest

Use `/tmp/gh-aw/agent/go-logger/manifest.json` as the source of truth for:
- `files_needing_logger`
- `missing_logger_import`
- `candidate_call_sites`

### 1.5. Keep Selection Deterministic and Bounded

- Do **not** launch sub-agents for file discovery or complexity scoring.
- Do **not** scan `pkg/` broadly once `manifest.json` is available.
- Select files only from `files_needing_logger` in `manifest.json`.
- If more than 5 files are listed, take the first 5 entries and continue directly to edits.

### 2. Select Files for Enhancement

From the list of Go files:
1. Prioritize files without loggers or with minimal logging
2. Focus on files with complex logic (workflows, parsers, compilers)
3. Avoid trivial files with just simple functions
4. **Select exactly 5 files maximum** for this PR

### 3. Analyze Each Selected File

For each selected file:
1. Read the file content to understand its structure
2. Identify functions that would benefit from logging
3. Check if the file already has a logger declaration
4. Plan where to add logging calls

### 4. Add Logger and Logging Calls

For each file:

1. **Add logger declaration if missing:**
   - Add import: `"github.com/github/gh-aw/pkg/logger"`
   - Add logger variable using correct naming: `var log = logger.New("pkg:filename")`

2. **Add meaningful logging calls:**
   - Add logging at function entry for important functions
   - Add logging before/after state changes
   - Add logging for control flow decisions
   - Ensure log arguments don't have side effects
   - Use `log.Enabled()` check for expensive debug info

3. **Keep it focused:**
   - 2-5 logging calls per file is usually sufficient
   - Don't over-log - focus on the most useful information
   - Ensure messages are meaningful and helpful for debugging

### 5. Validation (After All Files)

After adding logging to **all selected files**, validate your changes before creating a PR:

1. **Build the project and check formatting:**
   ```bash
   make build && make fmt
   ```
   This catches compilation errors and import formatting issues without the full unit test suite.

2. **If needed, recompile workflows:**
   ```bash
   make recompile
   ```
   Only run this if you changed any `.md` workflow files during this session.

### 6. Create Pull Request

After validating your changes:

1. Choose exactly one terminal outcome: `create_pull_request` after successful changes, `noop` when no changes are needed, or `report_incomplete` when a blocking failure prevents completion.
2. Call the chosen safe-output command exactly once, as your final action. Do not call any other safe-output command before or after it.
3. Do not probe safe outputs with `which`, `type`, `--help`, or schema-inspection commands.
4. If the safe-output gateway rejects the call, stop immediately and surface its exact rejection message. Do not retry the call or switch to another terminal safe output.
5. The PR title will automatically have the "[log] " prefix.

## Quality Checklist

Before creating the PR, verify:

- [ ] Maximum 5 files modified
- [ ] No test files modified (`*_test.go`)
- [ ] Each file has logger declaration with correct naming convention
- [ ] Logger arguments don't compute anything or cause side effects
- [ ] Logging messages are meaningful and helpful
- [ ] No duplicate logging with existing logs
- [ ] Import statements are properly formatted
- [ ] Changes validated with `make build && make fmt`

## Important Notes

- You have access to the edit tool to modify files
- You have access to bash commands to explore the codebase
- The safe-outputs create-pull-request will automatically create the PR
- Focus on quality over quantity - 5 well-logged files is better than 10 poorly-logged files
- Remember: debug logs are for developers, not end users

## Structured Patch Output

When proposing per-file logger changes, use this compact schema in your reasoning/output to reduce verbose prose:

```json
{
  "patches": [
    {
      "file": "pkg/path/file.go",
      "logger_name": "pkg:filename",
      "add_import": true,
      "add_logger_var": true,
      "call_sites": [
        {"line": 123, "function": "Run", "message": "enter Run"}
      ]
    }
  ]
}
```

Good luck enhancing the codebase with better logging!