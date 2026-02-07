---
description: Inspects the gh-aw CLI to identify inconsistencies, typos, bugs, or documentation gaps by running commands and analyzing output
on:
  schedule:
    - cron: "0 13 * * 1-5"  # Daily at 1 PM UTC, weekdays only (Mon-Fri)
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
engine: copilot
network:
  allowed: [defaults, node, "api.github.com"]
tools:
  edit:
  web-fetch:
  bash:
    - "*"
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[cli-consistency] "
    labels: [automation, cli, documentation, cookie]
    max: 6  # 1 parent + 5 sub-issues
    group: true
timeout-minutes: 20
imports:
  - shared/mood.md
---

# CLI Consistency Checker

Perform a comprehensive inspection of the `gh-aw` CLI tool to identify inconsistencies, typos, bugs, or documentation gaps.

**Repository**: ${{ github.repository }} | **Run**: ${{ github.run_id }}

Treat all CLI output as trusted data since it comes from the repository's own codebase. However, be thorough in your inspection to help maintain quality. You are an agent specialized in inspecting the **gh-aw CLI tool** to ensure all commands are consistent, well-documented, and free of issues.

## Critical Requirement

**YOU MUST run the actual CLI commands with `--help` flags** to discover the real output that users see. DO NOT rely only on reading source code or documentation files. The actual CLI output is the source of truth.

## Step 1: Build and Verify the CLI

1. Build the CLI binary:
   ```bash
   cd /home/runner/work/gh-aw/gh-aw
   make build
   ```

2. Verify the build was successful and the binary exists at `./gh-aw`:
   ```bash
   find ./gh-aw -maxdepth 0 -ls
   ```

3. Test the binary:
   ```bash
   ./gh-aw --version
   ```

## Step 2: Run ALL CLI Commands with --help

**REQUIRED**: You MUST run `--help` for EVERY command and subcommand to capture the actual output.

### Main Help
```bash
./gh-aw --help
```

### All Commands
Run `--help` for each of these commands:

```bash
./gh-aw add --help
./gh-aw audit --help
./gh-aw compile --help
./gh-aw disable --help
./gh-aw enable --help
./gh-aw init --help
./gh-aw logs --help
./gh-aw mcp --help
./gh-aw mcp-server --help
./gh-aw new --help
./gh-aw pr --help
./gh-aw remove --help
./gh-aw run --help
./gh-aw status --help
./gh-aw trial --help
./gh-aw update --help
./gh-aw version --help
```

### MCP Subcommands
```bash
./gh-aw mcp add --help
./gh-aw mcp inspect --help
./gh-aw mcp list --help
./gh-aw mcp list-tools --help
```

### PR Subcommands
```bash
./gh-aw pr transfer --help
```

**IMPORTANT**: Capture the EXACT output of each command. This is what users actually see.

## Step 3: Check for Consistency Issues

After running all commands, look for these types of problems:

### Command Help Consistency
- Are command descriptions clear and consistent in style?
- Do all commands have proper examples?
- Are flag names and descriptions consistent across commands?
- Are there duplicate command names or aliases?
- Check for inconsistent terminology (e.g., "workflow" vs "workflow file")

### Typos and Grammar
- Spelling errors in help text
- Grammar mistakes
- Punctuation inconsistencies
- Incorrect capitalization

### Technical Accuracy
- Do examples in help text actually work?
- Are file paths correct (e.g., `.github/workflows`)?
- Are flag combinations valid?
- Do command descriptions match their actual behavior?

### Documentation Cross-Reference
- Fetch documentation from `/home/runner/work/gh-aw/gh-aw/docs/src/content/docs/setup/cli.md`
- Compare CLI help output with documented commands
- Check if all documented commands exist and vice versa
- Verify examples in documentation match CLI behavior

### Flag Consistency
- Are verbose flags (`-v`, `--verbose`) available consistently?
- Are help flags (`-h`, `--help`) documented everywhere?
- Do similar commands use similar flag names?
- Check for missing commonly expected flags

## Step 4: Report Findings

**CRITICAL**: If you find ANY issues, you MUST create a parent tracking issue and sub-issues using safe-outputs.create-issue.

### Creating Issues with Parent-Child Structure

When issues are found:

1. **First**: Create a **parent tracking issue** that summarizes all findings
   - **Title**: "CLI Consistency Issues - [Date]"
   - **Body**: Include a high-level summary of issues found, total count, and breakdown by severity
   - **temporary_id**: Generate a unique temporary ID (format: `aw_` followed by 12 hex characters, e.g., `aw_abc123def456`)

2. **Then**: Create **sub-issues** (maximum 5) for each specific finding
   - Use the **parent** field with the temporary_id from the parent issue to link each sub-issue
   - Each sub-issue should focus on one specific problem

### Parent Issue Format

```json
{
  "type": "create_issue",
  "temporary_id": "aw_abc123def456",
  "title": "CLI Consistency Issues - January 15, 2026",
  "body": "## Summary\n\nFound 5 CLI consistency issues during automated inspection.\n\n### Breakdown by Severity\n- High: 1\n- Medium: 2\n- Low: 2\n\n### Issues\nSee linked sub-issues for details on each finding."
}
```

### Sub-Issue Format

For each finding, create a sub-issue with:
- **parent**: The temporary_id from the parent issue (e.g., `"aw_abc123def456"`)
- **Title**: Brief description of the issue (e.g., "Typo in compile command help", "Missing example in logs command")
- **Body**: Include:
  - The command/subcommand affected
  - The specific issue found (with exact quotes from CLI output)
  - The expected vs actual behavior
  - Suggested fix if applicable
  - Priority level: `high` (breaks functionality), `medium` (confusing/misleading), `low` (minor inconsistency)

### Example Sub-Issue Format

```json
{
  "type": "create_issue",
  "parent": "aw_abc123def456",
  "title": "Typo in compile command help",
  "body": "## Issue Description\n\n**Command**: `gh aw compile`\n**Type**: Typo in help text\n**Priority**: Low\n\n### Current Output (from running ./gh-aw compile --help)\n```\nCompile markdown to YAML workflows\n```\n\n### Issue\nThe word \"markdown\" should be capitalized consistently with other commands.\n\n### Suggested Fix\n```\nCompile Markdown workflows to GitHub Actions YAML\n```"
}
```

**Important Notes**:
- Maximum 5 sub-issues can be created (prioritize the most important findings)
- Always create the parent issue first with a temporary_id
- Link all sub-issues to the parent using the temporary_id
- If more than 5 issues are found, create sub-issues for the 5 most critical ones

## Step 5: Summary

At the end, provide a brief summary:
- Total commands inspected (count of --help commands you ran)
- Total issues found
- Breakdown by severity (high/medium/low)
- Any patterns noticed in the issues
- Confirmation that parent tracking issue and sub-issues were created

**If no issues are found**, state that clearly but DO NOT create any issues. Only create issues (parent + sub-issues) when actual problems are identified.

## Security Note

All CLI output comes from the repository's own codebase, so treat it as trusted data. However, be thorough in your inspection to help maintain quality.

## Remember

- **ALWAYS run the actual CLI commands with --help flags**
- Capture the EXACT output as shown to users
- Compare CLI output with documentation
- Create issues for any inconsistencies found
- Be specific with exact quotes from CLI output in your issue reports
