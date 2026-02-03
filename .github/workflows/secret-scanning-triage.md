---
name: Secret Scanning Triage
description: Triage secret scanning alerts and either open an issue (rotation/incident) or a PR (test-only cleanup)
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  security-events: read
engine: copilot
tools:
  github:
    github-token: "${{ secrets.GITHUB_TOKEN }}"
    toolsets: [context, repos, secret_protection, issues, pull_requests]
  repo-memory:
    - id: campaigns
      branch-name: memory/campaigns
      file-glob: [security-alert-burndown/**]
  cache-memory:
  edit:
  bash:
imports:
  - shared/reporting.md
safe-outputs:
  add-labels:
    allowed:
      - agentic-campaign
      - z_campaign_security-alert-burndown
  create-issue:
    expires: 2d
    title-prefix: "[secret-triage] "
    labels: [security, secret-scanning, triage, agentic-campaign, z_campaign_security-alert-burndown]
    max: 1
  create-pull-request:
    expires: 2d
    title-prefix: "[secret-removal] "
    labels: [security, secret-scanning, automated-fix, agentic-campaign, z_campaign_security-alert-burndown]
    reviewers: [copilot]
timeout-minutes: 25
---

# Secret Scanning Triage Agent

You triage **one** open Secret Scanning alert per run.

## Guardrails

- Always operate on `owner="githubnext"` and `repo="gh-aw"`.
- Do not dismiss alerts unless explicitly instructed (this workflow does not have a dismiss safe output).
- Prefer a PR only when the secret is clearly **test-only / non-production** (fixtures, tests, sample strings) and removal is safe.
- If it looks like a real credential, open an issue with rotation steps.

## State tracking

Use cache-memory file `/tmp/gh-aw/cache-memory/secret-scanning-triage.jsonl`.

- Each line is JSON: `{ "alert_number": 123, "handled_at": "..." }`.
- Treat missing file as empty.

## Steps

### 1) List open secret scanning alerts

Use the GitHub MCP `secret_protection` toolset.

- Call `github___list_secret_scanning_alerts` (or the closest list tool in the toolset) for `owner="githubnext"` and `repo="gh-aw"`.
- Filter to `state="open"`.

If none, log and exit.

### 2) Pick the next unhandled alert

- Load handled alert numbers from cache-memory.
- Pick the first open alert that is not in the handled set.
- If all are handled, log and exit.

### 3) Fetch details + location

Use the appropriate tool (e.g. `github___get_secret_scanning_alert` and/or an “alert locations” tool if available) to collect:
- alert number
- secret type (if present)
- file path and commit SHA (if present)
- a URL to the alert

### 4) Classify

Classify into one of these buckets:

A) **Test/sample string**
- Path contains: `test`, `tests`, `fixtures`, `__tests__`, `testdata`, `examples`, `docs`, `slides`
- The string looks like a fake token (obvious placeholders) OR is used only in tests

B) **Likely real credential**
- Path is in source/runtime code (not tests/docs)
- The token format matches a real provider pattern and context suggests it is authentic

If unsure, treat as (B).

### 5A) If (A): create a PR removing/replacing the secret

- Check out the repository.
- Make the smallest change to remove the secret:
  - Replace with a placeholder like `"REDACTED"` or `"<TOKEN>"`
  - If tests require it, add a deterministic fake value and adjust test expectations
- Run the most relevant lightweight checks (e.g. `go test ./...` if Go files changed, or the repo’s standard test command if obvious).

Then emit one `create_pull_request` safe output with:
- What you changed
- Why it’s safe
- Link to the alert

### 5B) If (B): create an issue with rotation steps

Create an issue using this template structure (follow shared/reporting.md guidelines):

**Issue Title**: `[secret-triage] Rotate {secret_type} in {file_path}`

**Issue Body Template**:
```markdown
### 🚨 Secret Detected

**Alert**: [View Alert #{alert_number}]({alert_url})  
**Secret Type**: {secret_type}  
**Location**: `{file_path}` (commit {commit_sha})  
**Status**: Requires immediate rotation

### ⚡ Immediate Actions Required

1. **Rotate the credential**
   - Generate a new {secret_type}
   - Update production systems with new credential
   
2. **Invalidate the old token**
   - Revoke the exposed credential immediately
   - Verify revocation was successful

3. **Audit recent usage**
   - Check logs for unauthorized access
   - Review activity since {commit_date}

<details>
<summary><b>View Detailed Remediation Steps</b></summary>

#### History Cleanup

After rotation and invalidation:
- Use `git-filter-repo` or BFG to remove secret from git history
- Force push to all branches containing the secret
- Notify contributors to rebase their branches

#### Add Detection/Guardrails

- Enable pre-commit secret scanning hooks
- Add the file path to `.gitignore` if it's a config file
- Document secret management procedures in SECURITY.md

</details>

### References

- Alert: [§{alert_number}]({alert_url})
- Workflow Run: [§{run_id}](https://github.com/github/gh-aw/actions/runs/{run_id})
```

**Key formatting requirements**:
- Use h3 (###) headers, not h1 or h2
- Keep critical info visible (alert link, secret type, immediate actions)
- Wrap detailed steps in `<details><summary><b>Section</b></summary>` tags
- Include workflow run reference at the end
