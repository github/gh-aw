---
title: Fork Support
description: How GitHub Agentic Workflows behaves in forked repositories and how to allow PRs from trusted forks.
sidebar:
  order: 7
---

GitHub Agentic Workflows has two distinct fork scenarios with different behaviors: **inbound pull requests from forks** and **running workflows inside a forked repository**.

## Running workflows in a fork

Agentic workflows do **not** run in forked repositories. When a workflow runs in a fork, all jobs skip automatically using the `if: ${{ !github.event.repository.fork }}` condition injected at compile time.

This means:

- Agent jobs are skipped — no AI execution occurs
- Maintenance and self-update jobs do not run
- No secrets from the upstream repository are available

This is intentional. Forks lack the secrets and context required for agentic workflows to function correctly, and there is no safe way to run agents with partial configuration.

> [!NOTE]
> To run agentic workflows in your own repository, fork the upstream repo and configure your own secrets — the workflows will then run in your copy of the repository, which is not a fork from GitHub Actions' perspective.

## Inbound pull requests from forks

When a pull request is opened from a fork to your repository, the default behavior is to **block the workflow from running** — the `pull_request` trigger includes a repository ID check that verifies the PR head branch comes from the same repository.

To allow workflows to run for PRs from trusted fork repositories, use the `forks:` field:

```aw wrap
---
on:
  pull_request:
    types: [opened, synchronize]
    forks: ["trusted-org/*"]
---
```

### Fork patterns

The `forks:` field accepts a string or a list of repository patterns:

| Pattern | Matches |
|---|---|
| `"*"` | All forks (use with caution) |
| `"owner/*"` | All forks from a specific user or organization |
| `"owner/repo"` | A specific fork repository |

```aw wrap
---
on:
  pull_request:
    types: [opened, synchronize]
    forks:
      - "trusted-org/*"
      - "partner/specific-fork"
---
```

> [!WARNING]
> Allowing all forks (`"*"`) means any user who forks your repository can trigger agent execution. Workflows triggered from fork PRs run with the permissions configured in the workflow — review those permissions carefully before allowing untrusted forks.

### Author associations

Alternatively, `forks:` accepts an object with an explicit `author-association` list. This allows fork PRs based on GitHub's `github.event.pull_request.author_association`, which describes the PR author's relationship to the base repository, not the fork owner:

```aw wrap
---
on:
  pull_request:
    forks:
      author-association: [OWNER, MEMBER, COLLABORATOR]
---
```

Same-repository PRs remain allowed. Fork PRs are allowed only when the author's association is in the list. Omit `MEMBER` if organization membership alone should not grant access. An empty list allows only same-repository PRs.

Accepted values are `OWNER`, `MEMBER`, `COLLABORATOR`, `CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`, `FIRST_TIMER`, `MANNEQUIN`, and `NONE`. Values must be uppercase. Associations such as `CONTRIBUTOR` and `NONE` do not imply trusted repository access; include only associations appropriate for the workflow's permissions.

> [!NOTE]
> This filter does not grant permissions or expose secrets. Agent authentication still requires credentials to be available to the fork PR run, for example in a private or internal repository where an administrator has enabled sending secrets to workflows from fork pull requests. Other workflow guards still apply.

## PR builds and deployment credentials

Allowing a fork PR to trigger a workflow does not make its code trusted. Run PR-head builds and tests without deployment credentials and with only the permissions they need. Do not check out or execute PR-head code in a privileged `pull_request_target` or `workflow_run` job.

Deploy production releases only after the change is merged, from a trusted commit, in a separate job that receives deployment credentials. If a pre-merge preview is required, isolate it from production and use credentials scoped to disposable preview resources. Treat artifacts from PR runs as untrusted: validate their provenance and contents, and never execute them in a credentialed job. See [cross-run artifact trust](/gh-aw/reference/artifacts/#cross-run-artifact-trust).
