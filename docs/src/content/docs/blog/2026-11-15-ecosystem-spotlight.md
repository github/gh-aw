---
title: "Ecosystem Spotlight: githubnext/evergreen"
description: "Evergreen keeps opted-in pull requests mergeable with a deterministic preflight gate, a lease label and verified safe outputs."
authors:
  - copilot
date: 2026-11-15
metadata:
  seoDescription: "How githubnext/evergreen keeps pull requests green with a deterministic preflight, a lease label and verified safe outputs."
  linkedPostText: "Ecosystem Spotlight: a PR gardener built on deterministic gates"
---

[githubnext/evergreen](https://github.com/githubnext/evergreen) is a GitHub Agentic Workflow that keeps selected pull requests green and mergeable. It fixes CI failures and keeps PR branches current with their base. It is aimed at teams that want an agent to repair pull requests they have explicitly opted in, without giving that agent the power to merge or to write to the default branch.

## Why it's awesome

**The trigger is deliberately not `pull_request`.** The workflow runs on a 15-minute schedule and on `workflow_dispatch`. The comment at the top of [workflows/evergreen.md](https://github.com/githubnext/evergreen/blob/main/workflows/evergreen.md) explains that generated pre-activation runs can trip confused-deputy guards for bot-authored or indirect PR events. Teams that need immediate handling are told to use a separate deterministic controller that never checks out or runs PR code.

**A deterministic gate runs before any agent.** A `preflight` job confirms that the PR is open, carries the opt-in `evergreen` label and the `evergreen_active` lease, and still has the head SHA that was selected. A pre-agent step then fetches `refs/pull/<n>/head` and refuses to continue if the fetched SHA differs from the expected one. A stale checkout fails closed.

**A lease prevents two runs on one PR.** The workflow creates the `evergreen_active` label, and it refuses to dispatch the agent if the label cannot be added. A separate cleanup job removes the lease. Runs are queued with `cancel-in-progress: false`, so a second run waits rather than cancelling the first.

**Write rules are operating rules.** [safe-output-policy.md](https://github.com/githubnext/evergreen/blob/main/workflows/shared/evergreen/safe-output-policy.md) requires the agent to reload GitHub state after every safe output and to report the operation as blocked if the expected change is not visible. It also forbids direct merges, base-branch writes and branch updates that merge or rebase the base. The workflow enforces the last rule too: a `git` wrapper placed on the agent's path exits with status 64 for `merge` and `rebase`.

**Quota and stop states are explicit.** [quota-policy.md](https://github.com/githubnext/evergreen/blob/main/workflows/shared/evergreen/quota-policy.md) counts quota per PR, continuing across runs for as long as the label stays. When it runs out, the agent stops, asks for the opt-in label to be removed, adds an exhausted label and leaves one terse comment. Every run ends in one of five named stop states, such as `awaiting-controller-recheck` or `quota-exhausted`.

**Fail-closed defaults.** The workflow ships `REQUIRED_CHECKS_JSON` as `'[]'`, and a comment labels it a fail-closed placeholder that installers must replace by naming their gates or choosing all-observed mode.

## How it works

1. The cron wakes the `preflight` job, which selects an eligible PR and evaluates the gates.
2. For an eligible PR, the job adds the lease label and dispatches the agent. The agent job checks out the verified head and runs the Copilot engine with `network: defaults`.
3. The agent runs `evergreen-diagnose` before it edits anything, then `evergreen-repair` if diagnosis says repair is needed. Both skills are pinned to a commit SHA and installed at runtime, so the prompt does not carry their full text.
4. Writes are safe outputs: comments, labels, PR-branch pushes limited to PRs with the `evergreen` label, and at most one review. Each write is verified after the fact, and the run finishes by removing the lease.

## Try it

Evergreen is installed by an agent, following [INSTALL.md](https://github.com/githubnext/evergreen/blob/main/INSTALL.md). The installer reads the target repository's guidance and its last 50 closed PRs, proposes a mergeability policy for you to confirm, copies `workflows/evergreen.md` and `workflows/shared/evergreen/` into `.github/workflows/`, runs `gh aw compile` and opens an installation PR. After that, apply the `evergreen` label to any PR you want it to work on.

Before you adopt it, check a few things:

- The `preflight` job has `contents`, `issues` and `pull-requests: write` permissions, so review its gate logic whenever you change it.
- The required-checks list ships empty, so you must name your gates before the agent can clear anything.
- Public or fork PRs should require trusted approval before any code runs, as the [README](https://github.com/githubnext/evergreen/blob/main/README.md) says.
- It is early-stage: 13 stars, and its latest commit is from late July 2026.

Even if you never install it, Evergreen is a useful reference. Its gate, lease and verification patterns apply to any agent that writes to pull requests.

Explore [githubnext/evergreen](https://github.com/githubnext/evergreen), and learn more about building your own workflows at [github/gh-aw](https://github.com/github/gh-aw).
