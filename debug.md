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

to upgrade to the latest non-prerelease. This can lag behind prereleases; see
[Model and engine misconfiguration](#model-and-engine-misconfiguration).
If it is not installed, run the installation script from the main branch of the gh-aw repository:

```bash
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash
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

## Model and engine misconfiguration

For AWF model/endpoint 400s, `model: auto` failures, install-step 404s after a
version pin, or questions about version fields, use the
[full checklist](.github/aw/debug-agentic-workflow.md#model-and-engine-misconfiguration):

1. **Check the compiler first.** Read `compiler_version` from the lock file's
   `gh-aw-metadata` header and `cli_version` from `aw_info.json`. Compare with
   `gh release list --repo github/gh-aw --limit 20`, including prereleases.
   `gh extension install` and `gh extension upgrade` default to the latest
   non-prerelease. To install a specific prerelease, use
   `gh extension install github/gh-aw --force --pin TAG`, then verify and recompile.
   In the October 2026 case, v0.89.21 predated wire-API inference
   ([#64177](https://github.com/github/gh-aw/pull/64177)); v0.91.7 was the reported
   newest prerelease, not a permanent latest tag.
2. **Check model/endpoint compatibility.** The harness resolves `auto` to a
   concrete model. Wire-API precedence is explicit `engine.env` override →
   catalog `wire_api` → `-utility` base-model catalog fallback → `gpt-5+` name rule
   → CLI default `/chat/completions`. `COPILOT_PROVIDER_WIRE_API=responses` uses
   `/responses`; `completions` uses `/chat/completions`. One wire API applies to the
   whole session, including sub-agents; replace an incompatible sub-agent with a
   model supporting the main session's endpoint. For `engine.model-routing`, inspect AWF's
   selected endpoint too. `Cannot translate Copilot request feature`,
   `Unsupported Responses custom tool`, `model_policy_violation`, and
   `not accessible via the … endpoint` warrant model/endpoint or model-policy
   investigation, not transient retries or prompt tuning. For
   `model_policy_violation`, check the model allowlist/denylist and rejected model;
   policy rejection alone does not establish an endpoint mismatch.
3. **Apply fixes in this order:**
   1. **Upgrade gh-aw and recompile.**
   2. **Pin a model that supports the required endpoint.**
   3. **Remove a conflicting `COPILOT_PROVIDER_WIRE_API` override.**
   4. **Use sub-agent models from the main model's family.** Verify endpoint
      compatibility; see [github/gh-aw#67460](https://github.com/github/gh-aw/issues/67460)
      and [github/copilot-cli#5103](https://github.com/github/copilot-cli/issues/5103).
   **Switching to an older model is a last resort** if these fail: leading with
   `model: gpt-4.1` trades capability for a workaround and leaves the
   underlying misconfiguration in place.
4. **Check the version field.** `engine.version` is the agent CLI version
   (Copilot CLI, 1.0.x in the customer case): **Install GitHub Copilot CLI** 404s
   point here. `sandbox.agent.version` is the AWF release in `vX.Y.Z` form and
   must match a GitHub release of `github/gh-aw-firewall`: **Install AWF binary**
   failures point here. There is no `engine.copilot.version` field. Usually remove
   the misplaced pin and recompile to use the compiled default.
5. **Read the evidence.** Inspect `[copilot-harness]` alias and
   `COPILOT_PROVIDER_WIRE_API` lines in `agent-stdio.log`; model and path per request
   in `sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl`; and `model`,
   `requested_model`, `cli_version` (gh-aw), `version` (agent CLI), and `awf_version`
   in `aw_info.json`. Use `gh aw audit RUN_ID` for the combined view. Older runs may
   omit diagnostics; do not assume routing was correct.

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
