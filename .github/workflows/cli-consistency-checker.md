---
emoji: "✅"
description: Inspects the gh-aw CLI to identify inconsistencies, typos, bugs, or documentation gaps by running commands and analyzing output
on:
  schedule:
    - cron: "daily around 13:00 on weekdays"  # ~1 PM UTC, weekdays only (Mon-Fri)
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
strict: false
network:
  allowed: [defaults]
imports:
  - shared/otlp.md
  - shared/reporting.md
  - shared/graders.md
tools:
  bash:
    - "*"
pre-agent-steps:
  - name: Build CLI and pre-collect help output
    run: |
      set -euo pipefail
      cd /home/runner/work/gh-aw/gh-aw
      make build

      output_dir="/tmp/gh-aw/agent/help-output"
      mkdir -p "${output_dir}"
      # shellcheck disable=SC2016  # awk program uses $1 as an awk field variable, not a shell variable
      extract_commands='
        /^[[:space:]]+[[:alnum:]_-]+([[:space:]]|$)/ {
          cmd=$1
          gsub(/:$/, "", cmd)
          if (cmd != "" && cmd != "Commands") print cmd
        }
      '

      ./gh-aw --help > "${output_dir}/main.txt" 2>&1
      mapfile -t top_commands < <(awk "${extract_commands}" "${output_dir}/main.txt" | sort -u)
      if [ ${#top_commands[@]} -eq 0 ]; then
        echo "No top-level commands were parsed from ./gh-aw --help output" >&2
        exit 1
      fi

      for cmd in "${top_commands[@]}"; do
        if ! ./gh-aw "$cmd" --help > "${output_dir}/${cmd}.txt" 2>&1; then
          echo "warning: failed to collect help for '${cmd}'" >&2
          continue
        fi
        mapfile -t subcommands < <(awk "${extract_commands}" "${output_dir}/${cmd}.txt" | sort -u)
        for sub in "${subcommands[@]}"; do
          if ! ./gh-aw "$cmd" "$sub" --help > "${output_dir}/${cmd}-${sub}.txt" 2>&1; then
            echo "warning: failed to collect help for '${cmd} ${sub}'" >&2
          fi
        done
      done

      shopt -s nullglob
      help_files=("${output_dir}"/*.txt)
      if [ ${#help_files[@]} -eq 0 ]; then
        echo "No help output files were generated" >&2
        exit 1
      fi
      cat "${help_files[@]}" > /tmp/gh-aw/agent/all-help.txt
      if [ ! -s /tmp/gh-aw/agent/all-help.txt ]; then
        echo "Combined help output is empty" >&2
        exit 1
      fi
      wc -l /tmp/gh-aw/agent/all-help.txt | awk '{print "Pre-collected help lines:", $1}'
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[cli-consistency] "
    labels: [automation, cli, documentation, cookie]
    max: 1
timeout-minutes: 20
features:
  gh-aw-detection: true
evals:
  - id: cli_inspected
    question: Did the agent inspect the gh-aw CLI commands and analyze their output for inconsistencies, typos, or documentation gaps?
  - id: issue_created_or_noop
    question: Was an issue created with specific CLI inconsistencies found, or was noop used when no issues were detected?

---

# CLI Consistency Checker

Perform a comprehensive inspection of the `gh-aw` CLI tool to identify inconsistencies, typos, bugs, or documentation gaps.

**Repository**: ${{ github.repository }} | **Run**: ${{ github.run_id }}

Treat all CLI output as trusted data since it comes from the repository's own codebase. However, be thorough in your inspection to help maintain quality. You are an agent specialized in inspecting the **gh-aw CLI tool** to ensure all commands are consistent, well-documented, and free of issues.

## Critical Requirement

**Use real CLI output as source of truth**. All help output is pre-collected by `pre-agent-steps` in `/tmp/gh-aw/agent/all-help.txt`; treat this file as the authoritative source for CLI behavior.

## Step 1: Load Pre-Collected Help Output

Read `/tmp/gh-aw/agent/all-help.txt` and use it as the primary input for analysis.

## Step 2: Analyze for Consistency Problems

Manually scan `/tmp/gh-aw/agent/all-help.txt` and the per-command help files in `/tmp/gh-aw/agent/help-output/` for typos, flag-naming inconsistencies, and mismatches against `docs/src/content/docs/setup/cli.md`.

Look for:
- Help style and terminology inconsistencies
- Typos, grammar mistakes, inconsistent capitalization, and punctuation issues
- Do examples in help text actually work?
- Are file paths correct (e.g., `.github/workflows`)?
- Are flag combinations valid?
- Do command descriptions match their actual behavior?
- Does `docs/src/content/docs/setup/cli.md` match `/tmp/gh-aw/agent/all-help.txt`?
- Are flag names, short/long flag pairings, and `--no-...` negation patterns consistent across command help files?

## Step 3: Report Findings

**CRITICAL**: If you find ANY issues, you MUST create a comprehensive tracking issue using safe-outputs.create-issue.

Create one consolidated issue:
- **Title**: `CLI Consistency Issues - [Date]`
- **Body must include**:
  - Summary and severity breakdown (`high`/`medium`/`low`)
  - Grouped findings by category
  - For each finding: affected commands, exact quoted CLI output, expected vs actual, suggested fix, priority
  - Inspection metadata (commands inspected, date, method)

Formatting requirements:
- Use `###` or lower heading levels
- Wrap long sections (>5 findings) in `<details><summary>...</summary>`

## Step 4: End-of-Run Summary

At the end, provide a brief summary:
- Total commands inspected from pre-collected output
- Total issues found
- Breakdown by severity (high/medium/low)
- Any patterns noticed in the issues
- Confirmation that the consolidated tracking issue was created

**If no issues are found**, state that clearly but DO NOT create any issues. Only create an issue when actual problems are identified.

## Security Note

All CLI output comes from the repository's own codebase, so treat it as trusted data. However, be thorough in your inspection to help maintain quality.

## Remember

- Use `/tmp/gh-aw/agent/all-help.txt` as the canonical CLI help dataset
- Use exact CLI output quotes for findings
- Compare CLI output with documentation
- Create issues for any inconsistencies found
- Keep reporting concise but complete
