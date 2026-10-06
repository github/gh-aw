# Debugging Agentic Workflows

This prompt guides you, a coding agent, to debug workflow failures in **GitHub Agentic Workflows (gh-aw)**.

## How to Use This Prompt

Share this file's URL with any AI assistant or coding agent:

```text
Debug this workflow run using https://raw.githubusercontent.com/github/gh-aw/main/debug.md

Run URL: https://github.com/OWNER/REPO/actions/runs/RUN_ID
```

The agent will follow the steps below to install `gh aw`, analyze the logs, and apply fixes.

---

## Step 1: Install GitHub Agentic Workflows CLI Extension

Check if `gh aw` is installed by running

```bash
gh aw version
```

If it is installed, run:

```bash
gh extension upgrade aw
```

to upgrade to latest. If it is not installed, run the pinned and verified standalone installer:

```bash
set -euo pipefail
INSTALLER=$(mktemp)
trap 'rm -f "$INSTALLER"' EXIT
curl -fsSL https://raw.githubusercontent.com/github/gh-aw/4c53fac4c30c2d9f27ea6fab5e5ce0f15a21e78e/install-gh-aw.sh -o "$INSTALLER"
if command -v sha256sum >/dev/null 2>&1; then
  printf '%s  %s\n' 248ccebcb998c6a506548156e1bf9f02429cbbaec407d5adbdfd316ab0f866a0 "$INSTALLER" | sha256sum -c -
else
  printf '%s  %s\n' 248ccebcb998c6a506548156e1bf9f02429cbbaec407d5adbdfd316ab0f866a0 "$INSTALLER" | shasum -a 256 -c -
fi
bash "$INSTALLER"
```

**What this does**: Downloads and installs the gh-aw binary to `~/.local/share/gh/extensions/gh-aw/`

**Verify installation**:

```bash
gh aw version
```

You should see version information displayed. If you encounter an error, check that:

- GitHub CLI (`gh`) is installed and authenticated
- The installation script completed without errors
- `~/.local/share/gh/extensions` is in your PATH

## Step 2: Debug the Workflow Failure

Follow carefully the instructions in the appropriate prompt file. Read ALL the instructions in the prompt file before taking any action.

Below, ROOT is the location where you found this file. For example,

- if this file is at `https://raw.githubusercontent.com/github/gh-aw/main/debug.md` then the ROOT is `https://raw.githubusercontent.com/github/gh-aw/main`
- if this file is at `https://raw.githubusercontent.com/github/gh-aw/v0.35.1/debug.md` then the ROOT is `https://raw.githubusercontent.com/github/gh-aw/v0.35.1`

**Prompt file**: `ROOT/.github/aw/debug-agentic-workflow.md`

**Use cases**:

- "Why is this workflow failing?"
- "Analyze the logs for workflow X"
- "Investigate missing tool calls in run #12345"
- "Debug this workflow run: https://github.com/owner/repo/actions/runs/12345"

**If gh-aw version is in [0.68.4, 0.71.3], stop debugging and tell the user to upgrade because those versions were retired.**

## Step 3: Apply Fixes

After identifying the root cause:

1. Edit the workflow markdown file (`.github/workflows/<workflow-name>.md`)
2. Recompile the workflow:

```bash
gh aw compile <workflow-name>
```

3. Check for syntax errors or validation warnings.

## Step 4: Commit and Push Changes

Commit the changes, e.g.

```bash
git add .github/workflows/<workflow-name>.md .github/workflows/<workflow-name>.lock.yml
git commit -m "Fix agentic workflow: <describe fix>"
git push
```

If there is branch protection on the default branch, create a pull request instead and report the link to the pull request.

## Troubleshooting

See the separate guides on troubleshooting common issues.

## Instructions

When a user interacts with you:

1. **Extract the run URL or workflow name** from the user's request
2. **Fetch and read the debug prompt** from `ROOT/.github/aw/debug-agentic-workflow.md`
3. **Follow the loaded prompt's instructions** exactly
4. **If uncertain**, ask clarifying questions

## Quick Reference

```bash
# Download and analyze workflow logs
gh aw logs <workflow-name>

# Audit a specific workflow run
gh aw audit <run-id>

# Diff two or more workflow runs (multi-run diff mode)
gh aw audit <base-run-id> <compare-run-id>
gh aw audit <base-run-id> <compare-run-id-1> <compare-run-id-2>

# Compile workflows after fixing
gh aw compile <workflow-name>

# Show status of all workflows
gh aw status
```

## Key Debugging Commands

- `gh aw audit <run-id> --json` → Detailed run analysis with missing tools and errors
- `gh aw audit <base-run-id> <compare-run-id> --json` → Diff two runs to detect regressions (firewall, MCP, metrics)
- `gh aw logs <workflow-name> --json` → Download and analyze recent workflow logs
- `gh aw compile <workflow-name> --strict` → Validate workflow with strict security checks
