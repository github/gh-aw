---
title: 'Example: Automated Repository Maintenance'
description: Use Repo Assist as an example of a recurring GitHub Agentic Workflow for repository maintenance.
sidebar:
  order: 20
---

Repository maintenance is continuous work: triaging issues, reviewing pull requests, keeping tests and dependencies healthy, and deciding what should happen next. [Repo Assist](https://github.com/githubnext/agentics/blob/main/docs/repo-assist.md) packages these activities as a recurring GitHub Agentic Workflow. It is a useful example of how an agentic workflow can inspect the current state of a repository, choose useful work, and propose changes while leaving maintainers in control.

Install the complete workflow with `gh aw add-wizard githubnext/agentics/repo-assist`. Read the [official Repo Assist documentation](https://github.com/githubnext/agentics/blob/main/docs/repo-assist.md) for its source, configuration, task-selection behavior, and current usage guidance.

:::note[Real-world impact]
A [study of 15 open-source repositories](https://github.com/githubnext/repo-assist-impact/blob/main/report.md) found this approach achieved a **9× median increase** in issue closure and PR merge velocity, reducing open issue counts in every repository. Projects that were largely dormant became actively maintained, with several reaching near-complete backlog clearance. Results hold across languages and project types.
:::

## How Repo Assist Works

On each scheduled run, Repo Assist reads live repository data and selects three tasks using weights that change with the backlog. A repository with many unlabelled issues receives more labelling and investigation work. As the backlog clears, the workflow spends more time on engineering investments, testing, performance, documentation, and forward-looking improvements.

Across runs, Repo Assist can label and investigate issues and pull requests, implement focused fixes as draft pull requests, improve dependencies, CI, code, performance, tests, and documentation, maintain its own pull requests by addressing CI failures and merge conflicts, nudge authors of stale pull requests, and continue work that advances the repository's goals.

The workflow uses repository memory to cover the backlog systematically and avoid repeating work. It also keeps a rolling monthly activity issue so maintainers can review its actions and suggested next steps in one place. The complete task list, selection weights, and behavioral guidelines are documented in the [Repo Assist documentation](https://github.com/githubnext/agentics/blob/main/docs/repo-assist.md).

## What Maintainers Still Own

Repo Assist supports repository maintenance; it does not replace maintainership. Maintainers still set project direction, define contribution and coding policies, review proposed changes, decide what to merge or release, and respond where human context or judgment is required. Add repository-specific instructions to `AGENTS.md` so coding runs follow the project's build, test, style, and contribution conventions.

The workflow favors small changes, avoids breaking public APIs, and discusses new dependencies before adding them, but those are operating guidelines rather than a guarantee that every suggestion is correct. Review its comments and draft pull requests as contributor work, and use the monthly activity issue to decide whether its schedule and permissions match the repository's review capacity.

Repo Assist also does not guarantee that pull request CI starts automatically. Workflows created with the repository's `GITHUB_TOKEN` may need separate CI-trigger configuration, and public repositories should consider abuse risk before enabling automatic CI for agent-created pull requests.

## Install and Use Repo Assist

Add the complete Repo Assist workflow with the interactive setup:

```bash
gh aw add-wizard githubnext/agentics/repo-assist
```

Commit the generated workflow to the default branch to enable its schedule. Once installed, the normal mode of operation is to let Repo Assist run regularly and review the activity it produces. You can also start a run immediately:

```bash
gh aw run repo-assist
```

Maintainers can also invoke it from an issue or pull request comment that starts with `/repo-assist`, followed by a specific instruction. For example:

```text
/repo-assist investigate this bug and suggest a fix
```

An on-demand invocation follows the instruction instead of selecting scheduled tasks. It currently must appear at the start of an issue or pull request comment, does not run from a code review comment, and does not add a link to the resulting run.

## What This Workflow Demonstrates

Repo Assist combines scheduled and command-driven triggers, deterministic preprocessing, adaptive task selection, repository memory, GitHub tools, and safe outputs in one reusable workflow. Safe outputs constrain the GitHub mutations the agent can request, while integrity filtering controls which repository content enters its context. Those controls define the workflow's boundaries; they do not remove the need to review its configuration and output.

Use the generated Repo Assist workflow as a working example for repository-specific maintenance automation. Inspect its permissions, tools, safe outputs, network access, schedule, and prompts, then narrow or adapt them to the repository's policies and maintainer capacity.

## Learn More

- [Repo Assist documentation](https://github.com/githubnext/agentics/blob/main/docs/repo-assist.md)
- [Safe Outputs](/gh-aw/reference/safe-outputs/)
- [Integrity Filtering](/gh-aw/reference/integrity/)
- [Triggering CI from Agent-Created Pull Requests](/gh-aw/reference/triggering-ci/)
- [Rate Limiting Controls](/gh-aw/reference/rate-limiting-controls/)
- [Debugging Workflows](/gh-aw/troubleshooting/debugging/)
