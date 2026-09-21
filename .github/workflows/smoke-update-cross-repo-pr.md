---
private: true
emoji: "🧪"
name: Smoke Update Cross-Repo PR
description: Smoke test validating cross-repo pull request updates in github/gh-aw-side-repo by adding lines from Homer's Odyssey to the README

on:
  schedule: every 2 days
  slash_command:
    name: smoke-update-cross-repo-pr
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke-update-cross-repo-pr"]
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
    fetch: ["main", "refs/pulls/open/*"]      # fetch all open PR refs after checkout
    fetch-depth: 0               # fetch full history to ensure we can see all commits and PR details

tools:
  edit:
  cache-memory: true
  bash:
    - "*"
  github:
    toolsets: [default]
    github-token: ${{ secrets.GH_AW_SIDE_REPO_PAT }}

safe-outputs:
  allowed-domains: [default-safe-outputs]
  create-issue:
    expires: 2h
    close-older-issues: true
    labels: [automation, testing]
  add-comment:
    hide-older-comments: true
    max: 2
  push-to-pull-request-branch:
    target-repo: "github/gh-aw-side-repo"
    github-token: ${{ secrets.GH_AW_SIDE_REPO_PAT }}
    if-no-changes: "error"
    target: "1" # PR #1
  messages:
    footer: "> 📜 *Cross-repo PR update smoke test by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "📜 [{workflow_name}]({run_url}) is adding the next Odyssey line to github/gh-aw-side-repo PR #1..."
    run-success: "✅ [{workflow_name}]({run_url}) successfully updated the cross-repo PR with a new Odyssey line!"
    run-failure: "❌ [{workflow_name}]({run_url}) failed to update the cross-repo PR: {status}"

timeout-minutes: 10
imports:
  - shared/otlp.md
  - shared/smoke-test-brevity.md
  - shared/reporting.md
features:
  gh-aw-detection: false
---

# Smoke Test: Cross-Repo Pull Request Update

The workspace is checked out from `githubnext/gh-aw-side-repo`. You will update PR #1 in that repo by appending the next sequential line from Homer's Odyssey to the README. Determine the next line by inspecting the Odyssey lines already in the README and choosing the line that immediately follows them, avoiding duplicates.

## Test Steps

Mark this step ✅ if the checkout succeeds, ❌ otherwise.

## Output

1. **Create an issue** in `${{ github.repository }}` with:
   - Use a temporary ID (e.g. `aw_smoke1`) for the issue so you can reference it later
   - Title: "Smoke Test: Copilot - Cross-repo update PR ${{ github.run_id }}"
   - Body should include:
     - Test results (✅ or ❌ for each test)
     - Overall status: PASS or FAIL
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp
     - Pull request author and assignees

2. **Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
   - PR titles only (no descriptions)
   - Overall status: IN PROGRESS

3. Use 'push_to_pull_request_branch' to update the PR https://github.com/githubnext/gh-aw-side-repo/pull/1 by appending the next sequential line of Homer's Odyssey to the README (not always the opening line).

4. **Add a comment to the issue** from step 1 `githubnext/gh-aw-side-repo` reporting whether the PR update succeeded or failed, and if it succeeded, include the line that was added to the PR.

5. **Only if the PR update succeeded**: Add a comment to the triggering pull request in `${{ github.repository }}` (omit the `item_number` parameter to auto-target the triggering PR) with:
   - "Smoke Test: Copilot - Cross-repo update PR ${{ github.run_id }}"
   - The line that was added to the cross-repo PR
   - Overall status: SUCCESS
   - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}