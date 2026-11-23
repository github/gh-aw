---
private: true
emoji: "🚨"
description: Monitors deployment failures and automatically creates deduplicated incident issues with root cause analysis.
on:
  deployment_status:
    state: [error, failure]
  skip-if-match: "is:issue is:open label:incident label:deployment-failure"
permissions:
  contents: read
  actions: read
  deployments: read
engine:
  id: claude
  model-provider: github
imports:
  - shared/mcp-pagination.md
  - shared/reporting.md
  - shared/otlp.md
features:
  gh-aw-detection: true
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [repos, actions]
safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[Incident] "
    labels: [incident, deployment-failure, "backlog:run-alerts"]
    close-older-issues: true
  noop:
timeout-minutes: 10

evals:
  - id: incident-or-noop
    question: Did the agent either create an incident issue or call noop?
  - id: root-cause-included
    question: Does the agent output include a root cause analysis or a reason for the noop decision?
  - id: dedup-respected
    question: Does the agent output show that it checked for an existing open incident issue before creating a new one?
---

### Deployment Incident Monitor

A deployment to **${{ github.event.deployment.environment }}** has failed with state `${{ github.event.deployment_status.state }}`.

#### Your Task

Perform a root cause analysis of this deployment failure and create a focused incident issue.

#### Deployment Context

- **Environment**: ${{ github.event.deployment.environment }}
- **Status**: ${{ github.event.deployment_status.state }}
- **Repository**: ${{ github.repository }}

#### Investigation Steps

1. **Check for an existing open incident issue**: Look for open issues with both `incident` and `deployment-failure` labels. If one already exists for this environment and recent timeframe, call `noop` with a brief explanation.

2. **Gather context** using `gh` CLI:
   - Look up recent workflow runs: `gh run list --repo $REPO --limit 10 --json databaseId,conclusion,name,headSha,createdAt`
   - Download job logs for failed runs: `gh run view <run_id> --log-failed`
   - Review recent commits: `gh api repos/$REPO/commits?per_page=10`
   - Check for related CI failures preceding the deployment

3. **Create an incident issue** if no duplicate exists. The issue should include:
   - **Environment** and the deployment failure state
   - **Summary** of likely root cause based on available evidence
   - **Evidence**: relevant log excerpts, failing steps, or recent commits linked to the failure
   - **Suggested remediation** steps for the on-call team
   - A link to the failing deployment for quick access

#### Output Guidelines

- Use `noop` if a duplicate open incident issue already exists.
- Keep the issue concise and actionable — focus on what the on-call engineer needs to know immediately.
- Do not create speculative issues; only create one when there is concrete evidence of a failure.
- In created incident issues, use `###` or `####` section headings only (never `#` or `##`).
- Wrap long evidence/log excerpts in `<details>` blocks with clear `<summary>` labels to keep issues scannable.