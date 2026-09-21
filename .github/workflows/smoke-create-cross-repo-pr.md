---
private: true
emoji: "🧪"
name: Smoke Create Cross-Repo PR
description: Smoke test validating cross-repo pull request creation in github/gh-aw-side-repo
on:
  schedule: every 2 days
  slash_command:
    name: smoke-create-cross-repo-pr
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke-create-cross-repo-pr"]
  status-comment: true

permissions:
  contents: read
  pull-requests: read
  issues: read

  copilot-requests: write
network:
  allowed:
    - defaults
    - github

checkout:
  - repository: github/gh-aw-side-repo
    github-token: ${{ secrets.GH_AW_SIDE_REPO_PAT }}

tools:
  edit:
  bash:
    - "*"
  github:
    toolsets: [default]
    github-token: ${{ secrets.GH_AW_SIDE_REPO_PAT }}

safe-outputs:
  allowed-domains: [default-safe-outputs]
  create-pull-request:
    target-repo: "github/gh-aw-side-repo"
    github-token: ${{ secrets.GH_AW_SIDE_REPO_PAT }}
    title-prefix: "[smoke] "
    labels: [smoke-test]
    draft: true
    expires: 1d
    if-no-changes: "error"
    fallback-as-issue: false
  create-issue:
    expires: 2h
    close-older-issues: true
    labels: [automation, testing]
  add-comment:
    hide-older-comments: true
    max: 2
  messages:
    footer: "> 🔬 *Cross-repo smoke test by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🔬 [{workflow_name}]({run_url}) is testing cross-repo PR creation in github/gh-aw-side-repo..."
    run-success: "✅ [{workflow_name}]({run_url}) successfully created a cross-repo PR in github/gh-aw-side-repo!"
    run-failure: "❌ [{workflow_name}]({run_url}) failed to create a cross-repo PR: {status}"

timeout-minutes: 10
imports:
  - shared/otlp.md
  - shared/smoke-test-brevity.md
  - shared/reporting.md
features:
  gh-aw-detection: false
---

# Smoke Test: Cross-Repo Pull Request Creation

The workspace is checked out from `githubnext/gh-aw-side-repo` (a private side repository used for smoke testing).

## Test Requirements

### 1. Create a Smoke Test File on a new branch

Create the file `smoke-tests/smoke-${{ github.run_id }}.txt` with the following content (create the directory if it doesn't exist using bash: `mkdir -p smoke-tests`):

```
Smoke test run: ${{ github.run_id }}
Repository: ${{ github.repository }}
Timestamp: <current UTC timestamp from bash: date -u +"%Y-%m-%dT%H:%M:%SZ">
Status: cross-repo PR creation smoke test

1.  Tell me, O Muse, of that ingenious hero who travelled far and wide
```

## Output

1. **Create an issue** in `${{ github.repository }}` with:
   - Use a temporary ID (e.g. `aw_smoke1`) for the issue so you can reference it later
   - Title: "Smoke Test: Copilot - Cross-repo create PR ${{ github.run_id }}"
   - Body should include:
     - Test results (✅ or ❌ for each test)
     - Overall status: PASS or FAIL
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp
     - Pull request author and assignees

2. **Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
   - PR titles only (no descriptions)
   - Overall status: IN PROGRESS

3. Use the `create_pull_request` safe-output tool to create a PR in `githubnext/gh-aw-side-repo` with the added file plus:
- **Title**: "Smoke test: cross-repo PR creation (${{ github.run_id }})"
- **Body**: "Automated smoke test PR created by `${{ github.repository }}` run [${{ github.run_id }}](${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}). This PR may be safely closed."

4. **Add a comment to the issue** from step 1 `githubnext/gh-aw-side-repo` reporting whether the PR update succeeded or failed, and if it succeeded, include the line that was added to the PR.

5. **Only if the PR update succeeded**: Add a comment to the triggering pull request in `${{ github.repository }}` (omit the `item_number` parameter to auto-target the triggering PR) with:
   - "Smoke Test: Copilot - Cross-repo create PR ${{ github.run_id }}"
   - The line that was added to the cross-repo PR
   - Overall status: SUCCESS
   - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}