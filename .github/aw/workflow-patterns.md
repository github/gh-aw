---
description: Shared design patterns for command workflows, monitoring workflows, scheduled one-item-at-a-time (All You Can Eat) workflows, large-repository workflows, database migration reviews, and cross-repository operations.
---

# Workflow Patterns

## Command Workflows

### Prefer `slash_command` when

- the action is conversational
- the user may pass arguments in the comment body
- the workflow should work across issues, pull requests, and discussions

### Prefer `label_command` when

- the action is one-shot and argument-free
- discoverability in the GitHub UI matters
- the workflow fits a label-driven process

### Combine both when

- the action is common enough to justify both invocation styles
- you want UI discoverability plus comment-based flexibility

See also: [triggers.md](triggers.md)

## Monitoring Workflows

### Use `workflow_run` when

- monitoring another GitHub Actions workflow in the **same repository**
- reacting to workflow completion/conclusion

Incident-triage pattern:

- trigger: `on.workflow_run` for the named deployment/CI workflow
- permissions: include `actions: read`; main job read-only
- reads: failed job logs/artifacts via GitHub tools
- output: `create-issue` with impact/root cause; `noop` when no action needed

Compact `workflow_run` examples:

- **Deploy workflow failure triage**: trigger on `workflow_run` for `Deploy`, read failed jobs/logs/artifacts, create one incident issue, `noop` when rerun succeeds.
- **CI regression watcher**: trigger on `workflow_run` for `CI`, compare current failure against recent runs, create issue only for new regressions, `noop` for known flakes.

Incident duplicate-suppression pattern:

- derive a stable incident key from the monitored workflow and failure signal (for example `<workflow-name>#<head-sha>#<failed-job-name>`)
- search open issues by title-prefix/label/key before creating a new issue
- create via `safe-outputs.create-issue` only when no matching open incident exists
- use `noop` for duplicates and include the matching issue number in the explanation

### Use `deployment_status` when

- monitoring an external deployment service reporting back to GitHub

Rule of thumb:

- `workflow_run` → GitHub Actions outcomes in this repo
- `deployment_status` → external platform outcomes via Deployments API

Do triage, evidence collection, and summary in one agent job — the single-job limits (no multi-job fan-out/fan-in, no cross-workflow waits or chaining) from [workflow-constraints.md](workflow-constraints.md) apply.

See also: [deployment-status.md](deployment-status.md)

## High-Volume Triage and Escalation Pattern

For workflows receiving many similar events (issues, PR comments, CI failures, security alerts, dependency events):

- start with a cheap triage/classification pass
- detect known/duplicate/stale/low-value cases first
- emit `noop` or a safe output when triage is confident
- escalate to the main agent only when uncertain or genuinely new/high-value

Decision flow:

```text
IF cheap triage is confident (known/duplicate/stale/low-value) THEN
  emit safe output or noop
ELSE
  escalate to the main agent
END IF
```

Use with pull-context workflows: fetch targeted evidence on demand instead of pushing raw logs into the initial prompt.

## All You Can Eat Pattern

Nickname for a scheduled workflow that keeps at most one *unconsumed* output alive at a time. The workflow wakes up frequently (typically every 30 minutes), but activation is skipped while the previous output from that workflow is still open. As soon as the user consumes the previous output (closes the issue, merges or closes the pull request), the next scheduled run produces the next item — content is served one plate at a time, on demand, and runs proceed sequentially.

Use when:

- the work is an open-ended backlog (improvement ideas, maintenance chores, research notes), not an event-driven reaction
- each output needs human consumption, so producing another before the previous one is closed only creates noise
- low latency matters: the next item should appear within one schedule tick after the user clears the last one

Avoid when outputs are independent and reviewable in parallel, or when the schedule is a periodic report tied to a window — use the recurring digest defaults in [report.md](report.md) instead.

### Shape

```yaml
on:
  schedule: every 30 minutes
  skip-if-match: 'is:issue is:open "gh-aw-workflow-id: my-workflow" in:body'
permissions: read-all
safe-outputs:
  create-issue:
    title-prefix: "[all-you-can-eat] "
    max: 1
    expires: 7
```

Rules:

- **One open item at a time.** Cap the safe output with `max: 1`. The string form of `skip-if-match` implies a threshold of `max: 1`, so any single open match skips activation; no extra field is needed.
- **Match on stable identity.** Prefer the hidden `gh-aw-workflow-id: <workflow-file-name-without-.md>` marker (`in:body`) over a title prefix, because humans rename titles; see [Footer Control](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/footers.md). A title match (`in:title "[all-you-can-eat] "`) works when the workflow also sets `title-prefix:`. The query is auto-scoped to the current repository.
- **Pull requests use the same shape** with `is:pr is:open` and `create-pull-request`; drafts count as open. Allow a small queue depth by raising the threshold (`skip-if-match: { query: ..., max: 3 }`) when the user can consume several items in parallel.
- **Do not starve.** If the user never closes the item, the workflow never runs again. Set `expires:` on the safe output (or an equivalent auto-close) so an abandoned item eventually unblocks the schedule.
- **Skipped runs are cheap.** The check runs in the `pre_activation` job, so the agent never starts and no tokens are spent on a skipped tick.
- **Keep concurrency on.** `skip-if-match` is evaluated before activation and cannot cancel a run already in flight; default workflow concurrency prevents two runs from producing at once.

### Learn from the closed output

Because the run only activates after the previous item was closed, the closing action is the feedback signal. Instruct the agent to:

1. search the most recently closed items from this workflow (same marker or title prefix), newest first and bounded — for example the last three
2. read the close reason (`completed` vs `not planned`), the closing comment, labels, and any review feedback
3. treat "not planned" or a rejecting comment as a negative signal and do not re-propose the same idea; treat completed/merged as a positive signal for similar work
4. persist a compact accept/reject list across runs with `cache-memory`, or `repo-memory` when losing the list would cause repeat proposals (see [memory-stateful-patterns.md](memory-stateful-patterns.md))
5. emit `noop` when nothing clears the quality bar left by past rejections

See also: [triggers.md](triggers.md), [safe-outputs-content.md](safe-outputs-content.md), [maintainer.md](maintainer.md)

## Large-Repository Improvement Pattern

For recurring maintenance in large repos:

- use `cache-memory`
- process one package/module/directory per run
- store last-processed item; round-robin
- prefer small focused PRs over wide sweeps

See also: [memory.md](memory.md)

## Step Authoring Guidance

When writing `steps:`, `pre-steps:`, and `post-steps:`, choose the implementation type in this order of preference:

### 1. Preferred: `actions/github-script`

Use `actions/github-script` for GitHub API interactions and general scripting. The workflow compiler handles action pinning automatically; specify a recent major version tag (`@v7`) without a SHA.

- Provides typed access to the GitHub REST API via `github.rest.*`
- Exposes `context`, `core`, `github`, `io`, and `exec` helpers
- Eliminates shell injection risks for untrusted input
- Example:

  ```yaml
  steps:
    - name: Fetch issue data
      uses: actions/github-script@v7
      with:
        script: |
          const issue = await github.rest.issues.get({
            owner: context.repo.owner,
            repo: context.repo.repo,
            issue_number: context.issue.number,
          });
          core.setOutput('title', issue.data.title);
  ```

### 2. Shell scripts

Use `run:` steps when `actions/github-script` is not suitable. To prevent shell injection, never interpolate untrusted values directly into the script body. Any value that originates from user input — including `github.event.issue.title`, `github.event.issue.body`, `github.event.comment.body`, `github.event.pull_request.title`, `github.event.pull_request.body`, and `github.head_ref` — must be passed through environment variables:

```yaml
# ❌ Unsafe: direct expression interpolation into the shell script
- name: Unsafe comment
  run: gh issue comment ${{ github.event.issue.number }} --body "${{ github.event.issue.title }}"

# ✅ Safe: pass untrusted values through env vars and reference them as $VAR_NAME
- name: Safe comment
  env:
    ISSUE_NUMBER: ${{ github.event.issue.number }}
    TITLE: ${{ github.event.issue.title }}
  run: gh issue comment "$ISSUE_NUMBER" --body "$TITLE"
```

### 3. Python (last resort)

Use Python only when the task genuinely requires data science or numeric libraries (for example `pandas`, `numpy`, `matplotlib`). Prefer `actions/github-script` or a shell step for everything else.

## Pre-Step Data Fetching Pattern

Use deterministic `steps:` when the workflow needs large external data before the agent runs.

Rules:

- write prepared files to `/tmp/gh-aw/agent/`
- trim large outputs before handing to the agent
- set `GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}` on every `gh` step
- add `permissions: actions: read` for downloading workflow logs/artifacts
- use `jq` to reduce JSON payload size

Compact reporting/incident prefetch example:

```yaml
steps:
  - name: Prefetch compact failure context
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      REPO: ${{ github.repository }}
      RUN_ID: ${{ github.event.workflow_run.id }}
    run: |
      gh api "repos/$REPO/actions/runs/$RUN_ID/jobs" \
        --jq '[.jobs[] | select(.conclusion != "success") | {name, conclusion, started_at, completed_at}]' \
        > /tmp/gh-aw/agent/failed_jobs.json
```

## PR Visual Regression Pattern

For PR UI validation and screenshot diffs:

- trigger: `pull_request`
- tools: `playwright` plus `cache-memory` for baseline metadata
- permissions: read-only repo/PR access
- output: `add-comment` with pass/fail summary and artifact links
- fallback: `noop` when no UI changes detected

## Design Token / CSS Governance Pattern

For PRs touching design tokens or CSS files that require a linked design reference (Figma link, design doc URL, or ADR token):

- trigger: `pull_request` with `paths:` scoped to token/style files (for example `tokens/**`, `**/*.tokens.json`, `**/*.css`, `src/styles/**`, `design-system/**`)
- permissions: `pull-requests: read`, `contents: read`; agent job read-only
- reads: PR body and comments via `gh pr view` to locate the linked design reference; validate the link target is reachable and matches the changed components
- output: apply the classify flow from [PR Checks with Linked References](github-agentic-workflows.md) (valid → ✅ comment; incomplete → gap comment; missing → request comment, escalating to `create-issue` only for a required blocking gate with no open issue already covering the scope)
- fallback: `noop` when the `paths:` guard excludes all changed files

## QA Coverage Report Pattern

For PR QA coverage summaries (gaps, risks, suggested test focus):

- trigger: `pull_request` (optionally scoped with `paths:`)
- tools: `github` (`gh-proxy`) for changed files, PR metadata, labels, checks
- permissions: `contents: read`, `pull-requests: read`; agent job read-only
- output: `add-comment` with coverage matrix and untested/high-risk areas
- fallback: `noop` for non-testable changes (e.g. docs-only)

## PM Stakeholder Digest Pattern

For recurring product/stakeholder digests:

- trigger: fuzzy `schedule` (e.g. `weekly on mondays`)
- tools: `github` (`gh-proxy`), optional `cache-memory` for period-over-period continuity
- permissions: read-only
- output: `create-issue` by default; `create-discussion` only when requested
- prompt: audience-aware language (summary first, details second)

## Database Migration Safety Pattern

For PRs adding/modifying migration files:

- trigger: `pull_request` with `paths:` scoped to migration dirs (e.g. `db/migrate/**`, `migrations/**`, `*.sql`)
- permissions: `contents: read`, `pull-requests: read`; agent job read-only
- reads: changed migration content via GitHub tools
- output: `add-comment` flagging risky operations; `noop` when clean
- prompt: include migration best practices

## Release Automation Pattern

For workflows that build, test, publish a GitHub release, and generate release highlights:

- trigger: `workflow_dispatch` with a `release_type` input (`patch`, `minor`, `major`); restrict with `roles: [admin, maintainer]`
- structure: **Classic + Agent** hybrid — all build/test/release jobs are standard GitHub Actions jobs; the agent job runs last and only updates the release description
- classic jobs: `config` (compute semver), `build` (compile + upload artifact), `test`, `release` (create prerelease with `--generate-notes --latest=false`); output `release_id` from the release job
- agent job: depends on `release` job; pre-fetches merged PRs and changelog in `steps:`; uses `tools: cli-proxy: true`; writes highlights via `update-release` with `operation: prepend`
- safe output: agent-generated release notes and release-description changes must use `update-release`; never mutate releases directly with `gh`, the GitHub API, or a GitHub write tool; set `threat-detection: false` because release bodies can contain code snippets
- permissions: global `contents: read`; per-job `contents: write` only on jobs that push tags or create releases

See [release-workflow.md](release-workflow.md) for the full pattern, frontmatter template, job skeletons, and reference implementation pointer.

## Cross-Repository Pattern

For cross-repo reads and writes:

- enable GitHub toolsets needed for external repos
- configure PAT or GitHub App auth in `safe-outputs:` for cross-repo writes
- tell the agent to set `target-repo` explicitly
- document required token scopes in the prompt or instructions

Cross-repo workflows inherit single-job constraints from [workflow-constraints.md](workflow-constraints.md).
