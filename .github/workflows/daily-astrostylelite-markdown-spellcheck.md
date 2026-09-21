---
private: true
emoji: "📝"
name: Daily AstroStyleLite Markdown Spellcheck
description: Runs daily American English spellcheck for AstroStyleLite docs content and opens a safe PR only when findings exist
on:
  schedule:
    - cron: daily
  workflow_dispatch:
max-daily-ai-credits: 10000
timeout-minutes: 30

permissions:
  contents: read
  issues: read
  pull-requests: read

tracker-id: daily-astrostylelite-markdown-spellcheck
engine: claude
strict: true

if: needs.spellcheck.outputs.has_findings == 'true'

jobs:
  spellcheck:
    runs-on: ubuntu-latest
    needs: [activation]
    permissions:
      contents: read
    outputs:
      has_findings: ${{ steps.run_spellcheck.outputs.has_findings }}
      findings_count: ${{ steps.run_spellcheck.outputs.findings_count }}
      files_checked: ${{ steps.run_spellcheck.outputs.files_checked }}
      dictionary_path: ${{ steps.run_spellcheck.outputs.dictionary_path }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7.0.1
        with:
          persist-credentials: false

      - name: Setup Node.js
        uses: actions/setup-node@v7.0.0
        with:
          node-version: "24"

      - name: Run Markdown spellcheck (pinned)
        id: run_spellcheck
        shell: bash
        run: |
          set -euo pipefail

          ARTIFACT_DIR="/tmp/gh-aw/agent/spellcheck"
          mkdir -p "$ARTIFACT_DIR"

          find "$GITHUB_WORKSPACE/docs/src/content" -type f \( -name '*.md' -o -name '*.mdx' \) | sort > "$ARTIFACT_DIR/files.txt"

          FILES_CHECKED=$(wc -l < "$ARTIFACT_DIR/files.txt" | tr -d ' ')
          CSPELL_RESULTS_PATH="$ARTIFACT_DIR/cspell-results.json"
          RUNTIME_CONFIG_PATH="$ARTIFACT_DIR/cspell-runtime-config.json"

          echo "::group::Spellcheck setup"
          echo "Repository root: $GITHUB_WORKSPACE"
          echo "Files discovered for spellcheck: $FILES_CHECKED"
          echo "Pinned tool version: $(npx --yes cspell@8.19.4 --version)"
          echo "Sample file list entries:"
          sed -n '1,5p' "$ARTIFACT_DIR/files.txt"
          echo "::endgroup::"

          DICTIONARY_PATH=""
          DICTIONARY_PATH_REL=""
          for candidate in \
            docs/.cspell-words.txt \
            docs/.spellcheck-ignore.txt \
            .cspell-words.txt \
            .spellcheck-ignore.txt \
            .github/spellcheck-ignore.txt
          do
            if [ -f "$candidate" ]; then
              DICTIONARY_PATH_REL="$candidate"
              DICTIONARY_PATH="$GITHUB_WORKSPACE/$candidate"
              break
            fi
          done

          jq \
            --arg dict "$DICTIONARY_PATH" \
            '.
            | .dictionaryDefinitions = (
                if $dict == "" then
                  []
                else
                  [{ name: "workflow-dictionary", path: $dict, addWords: true }]
                end
              )
            | .dictionaries = (
                if $dict == "" then
                  ["en", "en-US"]
                else
                  ["en", "en-US", "workflow-dictionary"]
                end
              )' \
            docs/.cspell.docs.json > "$RUNTIME_CONFIG_PATH"

          if [ "$FILES_CHECKED" -eq 0 ]; then
            echo '{"issues":[],"info":[],"debug":[],"error":[]}' > "$CSPELL_RESULTS_PATH"
            CSPELL_EXIT_CODE=0
          else
            # cspell v8 removed --format json for lint; use JSON reporter instead.
            set +e
            npx --yes cspell@8.19.4 lint \
              --no-progress \
              --no-summary \
              --show-suggestions \
              --reporter @cspell/cspell-json-reporter \
              --config "$RUNTIME_CONFIG_PATH" \
              --file-list "$ARTIFACT_DIR/files.txt" \
              > "$CSPELL_RESULTS_PATH"
            CSPELL_EXIT_CODE=$?
            set -e
          fi

          echo "::group::Spellcheck diagnostics"
          echo "Selected dictionary: ${DICTIONARY_PATH_REL:-none}"
          echo "Runtime config path: $RUNTIME_CONFIG_PATH"
          echo "cspell exit code: $CSPELL_EXIT_CODE"
          echo "::endgroup::"

          if ! jq -e . "$CSPELL_RESULTS_PATH" >/dev/null; then
            echo "cspell output was not valid JSON"
            sed -n '1,40p' "$CSPELL_RESULTS_PATH"
            exit 2
          fi

          CSPELL_ERROR_COUNT=$(jq '[.. | objects | select(has("error")) | .error[]?] | length' "$CSPELL_RESULTS_PATH")
          if [ "$CSPELL_ERROR_COUNT" -gt 0 ]; then
            echo "cspell reported runtime errors: $CSPELL_ERROR_COUNT"
            jq '.error' "$CSPELL_RESULTS_PATH"
            exit 2
          fi

          FINDINGS_COUNT=$(jq '[.. | objects | select(has("issues")) | .issues[]? | select((.isFlagged // true) == true)] | length' "$CSPELL_RESULTS_PATH")

          jq -c '.. | objects | select(has("issues")) | .issues[]? | select((.isFlagged // true) == true) | {
            file: (.uri // .filename // .file // ""),
            line: (.row // .line // null),
            column: (.col // .column // null),
            word: (.text // .word // ""),
            suggestions: (.suggestions // []),
            flagged: (.isFlagged // true)
          }' "$CSPELL_RESULTS_PATH" > "$ARTIFACT_DIR/findings.ndjson"

          jq -n \
            --arg tool "cspell@8.19.4" \
            --arg locale "en-US" \
            --arg scope "docs/src/content/" \
            --arg dict "$DICTIONARY_PATH_REL" \
            --argjson files_checked "$FILES_CHECKED" \
            --argjson findings "$FINDINGS_COUNT" \
            --argjson cspell_exit_code "$CSPELL_EXIT_CODE" \
            --argjson cspell_error_count "$CSPELL_ERROR_COUNT" \
            '{
              tool: $tool,
              locale: $locale,
              scope: $scope,
              file_patterns: ["**/*.md", "**/*.mdx"],
              files_checked: $files_checked,
              findings: $findings,
              has_findings: ($findings > 0),
              cspell_exit_code: $cspell_exit_code,
              cspell_error_count: $cspell_error_count,
              dictionary: {
                supported: true,
                path: (if $dict == "" then null else $dict end)
              }
            }' > "$ARTIFACT_DIR/summary.json"

          {
            if [ "$FINDINGS_COUNT" -gt 0 ]; then
              echo "has_findings=true"
            else
              echo "has_findings=false"
            fi
            echo "findings_count=$FINDINGS_COUNT"
            echo "files_checked=$FILES_CHECKED"
            echo "dictionary_path=$DICTIONARY_PATH_REL"
          } >> "$GITHUB_OUTPUT"

      - name: Render spellcheck report to step summary
        if: success()
        shell: bash
        run: |
          ARTIFACT_DIR="/tmp/gh-aw/agent/spellcheck"
          FINDINGS_COUNT=$(jq -r '.findings' "$ARTIFACT_DIR/summary.json")
          FILES_CHECKED=$(jq -r '.files_checked' "$ARTIFACT_DIR/summary.json")
          DICT_PATH=$(jq -r '.dictionary.path // "none"' "$ARTIFACT_DIR/summary.json")
          LOCALE=$(jq -r '.locale' "$ARTIFACT_DIR/summary.json")

          {
            echo "## Spellcheck Report"
            echo ""
            echo "| Metric | Value |"
            echo "|--------|-------|"
            echo "| Locale | \`$LOCALE\` |"
            echo "| Files checked | $FILES_CHECKED |"
            echo "| Findings | $FINDINGS_COUNT |"
            echo "| Dictionary | $DICT_PATH |"
            echo ""
            if [ "$FINDINGS_COUNT" -gt 0 ]; then
              echo "### Findings"
              echo ""
              echo "| File | Line | Column | Word | Suggestions |"
              echo "|------|------|--------|------|-------------|"
              jq -r '. | "\(.file | ltrimstr(env.GITHUB_WORKSPACE) | ltrimstr("/")) | \(.line // "-") | \(.column // "-") | `\(.word)` | \(.suggestions | join(", ")) |"' \
                "$ARTIFACT_DIR/findings.ndjson"
            else
              echo "_No spelling findings._"
            fi
          } >> "$GITHUB_STEP_SUMMARY"

      - name: Upload spellcheck artifact
        if: success()
        uses: actions/upload-artifact@v7.0.1
        with:
          name: spellcheck-results
          path: |
            /tmp/gh-aw/agent/spellcheck/summary.json
            /tmp/gh-aw/agent/spellcheck/cspell-results.json
            /tmp/gh-aw/agent/spellcheck/cspell-runtime-config.json
            /tmp/gh-aw/agent/spellcheck/findings.ndjson
            /tmp/gh-aw/agent/spellcheck/files.txt
            docs/.cspell.docs.json
          if-no-files-found: error
          retention-days: 3

safe-outputs:
  steer: true
  create-pull-request:
    expires: 3d
    title-prefix: "[docs] "
    labels: [documentation, spellcheck, automation]
    draft: false
    fallback-as-issue: false
    preserve-branch-name: true
    allowed-files:
      - docs/src/content/**/*.md
      - docs/src/content/**/*.mdx

steps:
  - name: Download spellcheck artifact
    uses: actions/download-artifact@v8.0.1
    with:
      name: spellcheck-results
      path: /tmp/gh-aw/agent/spellcheck

tools:
  cli-proxy: true
  bash: true
  edit:

experiments:
  prompt_style:
    variants: [concise, detailed]
    description: "Test whether a concise agent prompt achieves the same fix quality as the current detailed prompt while reducing token usage"
    hypothesis: "Concise prompt reduces token consumption ≥20% without degrading fix precision. H0: no difference in fix rate."
    metric: aic
    secondary_metrics: [pr_acceptance_rate, output_length_chars]
    guardrail_metrics:
      - name: empty_output_rate
        threshold: "<0.10"
      - name: run_success_rate
        threshold: ">=0.90"
    min_samples: 30
    weight: [50, 50]

imports:
  - shared/otlp.md
features:
  gh-aw-detection: true
sandbox:
  agent:
    id: awf
evals:
  - id: spellcheck_completed
    question: Did the agent run American English spellcheck on AstroStyleLite docs content?
  - id: pr_created_or_noop
    question: Was a PR created with spelling corrections, or was noop used when no misspellings were found?
---

# Daily AstroStyleLite Markdown Spellcheck

You maintain spelling quality for AstroStyleLite documentation under `docs/src/content/`.

## Scope

- Only process files under `docs/src/content/`
- Only modify markdown content files:
  - `*.md`
  - `*.mdx`
- Use American English conventions

## Inputs from the Spellcheck Job

The spellcheck job runs after activation and before the agent job, and stores machine-readable results at:

- `/tmp/gh-aw/agent/spellcheck/summary.json`
- `/tmp/gh-aw/agent/spellcheck/cspell-results.json`
- `/tmp/gh-aw/agent/spellcheck/findings.ndjson`
- `/tmp/gh-aw/agent/spellcheck/files.txt`
- `docs/.cspell.docs.json`

Dictionary source files referenced by `docs/.cspell.docs.json` are optional:

- `docs/.cspell-words.txt`
- `docs/.spellcheck-ignore.txt`
- `.cspell-words.txt`
- `.spellcheck-ignore.txt`
- `.github/spellcheck-ignore.txt`

If a dictionary file is absent, spellcheck continues without it.

Spellcheck summary:

- Files checked: `${{ needs.spellcheck.outputs.files_checked }}`
- Findings: `${{ needs.spellcheck.outputs.findings_count }}`
- Dictionary file (if any): `${{ needs.spellcheck.outputs.dictionary_path }}`

## Conditional Execution

This workflow is intentionally gated so the agent path only runs when `needs.spellcheck.outputs.has_findings == 'true'`.
When no findings exist, the workflow stops after spellcheck and skips agent execution.

{{#if experiments.prompt_style == 'concise'}}
Fix spelling errors in `docs/src/content/` markdown files.

Inputs: `/tmp/gh-aw/agent/spellcheck/findings.ndjson` and `summary.json`.
Preserve technical terms, product names, and code symbols.
Branch: `spellcheck/YYYY-MM-DD`. Call `noop` if no safe fixes exist.
{{#else}}
## Task

1. Read `/tmp/gh-aw/agent/spellcheck/summary.json` and `/tmp/gh-aw/agent/spellcheck/findings.ndjson`.
2. Apply only justified spelling fixes in `docs/src/content/**/*.md` and `docs/src/content/**/*.mdx`.
3. Preserve technical terms, product names, code symbols, and intentional capitalization.
4. Do not re-run spellcheck in the agent job; use the provided artifact as the source of truth.
5. If there are no safe fixes to apply, call `noop`.

## Pull Request Requirements

When creating the pull request:

- Use branch name format: `spellcheck/YYYY-MM-DD` (prefix must be `spellcheck/`)
- Provide the full branch name in the `create_pull_request` call (the prefix is not auto-added)
- State that the run is automated
- State the scope is `docs/src/content/`
- State changes are markdown spellcheck fixes only
- Summarize changed files
- Mention dictionary path only if one was used

## Safety Constraints

- Do not modify files outside `docs/src/content/`
- Do not modify non-markdown files
- Use only the safe output `create_pull_request` for repository writes

If no action is needed, call:

```json
{"noop": {"message": "No valid markdown spellcheck fixes needed for docs/src/content/."}}
```
{{#endif}}

### Output Format

Wrap long content with `<details><summary><b>View Details</b></summary>...</details>`.