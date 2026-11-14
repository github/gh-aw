---
title: "Ecosystem Spotlight: svennijhuis/agentic-workflows"
description: "A catalog of installable GitHub Agentic Workflows that builds, reviews and fixes pull requests inside hard round caps."
authors:
  - copilot
date: 2026-11-14
metadata:
  seoDescription: "How svennijhuis/agentic-workflows chains a builder, a four-axis reviewer and capped fix rounds, all installable with gh aw add."
  linkedPostText: "Ecosystem Spotlight: a capped builder and reviewer loop in agentic-workflows"
---

Most agentic workflow examples stop at one trigger and one comment. [svennijhuis/agentic-workflows](https://github.com/svennijhuis/agentic-workflows) goes further. It is a catalog of eleven installable workflows that take an issue to a reviewed pull request, with a reviewer that can send work back a limited number of times before handing it to a human. It is aimed at teams that want a working, opinionated build loop they can install with `gh aw add` and adapt, rather than designing one from scratch.

## Why it's awesome

**A hard cap on the loop.** Each workflow enforces its own limit in its `if:` condition, so the runner decides whether a run starts, not prompt text. [issue-builder.md](https://github.com/svennijhuis/agentic-workflows/blob/main/workflows/issue-builder.md) starts only for a new build or fix rounds 1 and 2. [pr-reviewer.md](https://github.com/svennijhuis/agentic-workflows/blob/main/workflows/pr-reviewer.md) runs review rounds 1 to 3 only, so the builder gets at most two send-backs. The reviewer also derives the round from its own history of `Reviewer: changes needed` reviews, so a wrong input cannot reset the count.

**Review that never merges.** The reviewer allows only `COMMENT` reviews. The workflow explains why: without a separate builder identity, builder PRs and reviews share `github-actions[bot]`, and GitHub rejects `REQUEST_CHANGES` on your own PR. A maintainer stays the merge gate. The reviewer can add `ready-to-merge` or `needs-help` labels, but it never approves, merges or pushes.

**Four axes plus a conditional security pass.** The reviewer starts `standards`, `spec`, `deslop` and `blast-radius` sub-agents, each in a fresh context and each asked for `path:line` evidence. A `security` sub-agent runs only when the diff touches a trust boundary, such as endpoints, auth, permissions, `.github/workflows/*`, dependency manifests, secrets, shell calls, outbound network or deserialisation. Each axis keeps its own heading and is not reranked, so a reader can see which axis raised a concern.

**Fail-closed defaults in shared fragments.** [shared/security.md](https://github.com/svennijhuis/agentic-workflows/blob/main/workflows/shared/security.md) closes the network except for gh-aw defaults, requires importers to name any extra host, and keeps threat detection on. [shared/defaults.md](https://github.com/svennijhuis/agentic-workflows/blob/main/workflows/shared/defaults.md) pins the engine version. Cost caps such as `max-ai-credits`, `max-daily-ai-credits`, `max-turns` and `timeout-minutes` stay in each workflow's own frontmatter, because the compiler ignores them in imports.

**Vendored skills with provenance.** The `shared/skills/` folder holds local copies of eleven skills, mostly from [mattpocock/skills](https://github.com/mattpocock/skills). The [shared README](https://github.com/svennijhuis/agentic-workflows/blob/main/workflows/shared/README.md) records the source path, commit SHA and licence for each one, along with the update procedure.

## How it works

1. [aw.yml](https://github.com/svennijhuis/agentic-workflows/blob/main/aw.yml) is the package manifest. It lists the eleven workflows under `includes:` and sets `min-version: v0.93.4`, so `gh aw add` knows what to install.
2. An issue-builder run implements an issue and opens a `builder/*` pull request labelled `builder`.
3. The builder then dispatches `pr-reviewer` for round 1. The reviewer posts a `COMMENT` review that starts with `Reviewer: ready to merge.` when clean. Otherwise it posts inline comments and dispatches the builder for a fix round.
4. If round 3 still has findings, the reviewer adds `needs-help` and posts one summary of open points for a maintainer. It starts nothing further.

## Try it

From the README, in a target repository:

```bash
gh aw add svennijhuis/agentic-workflows/workflows/issue-builder.md@<tag>
gh aw compile
```

Pin `<tag>` to a released version rather than a branch. Each workflow names the other in `dispatch-workflow`, so installing either one brings the other along. The builder also references a `CI_TRIGGER_TOKEN` secret for an extra empty commit that starts CI. It is a placeholder name, so rename it to match your setup. The [install guide](https://github.com/svennijhuis/agentic-workflows/blob/main/docs/install.md) covers tokens, secrets and labels for an organisation.

Patterns worth borrowing even without the full loop:

- Put the loop cap in the trigger `if:` so the runner enforces it.
- Make the reviewer comment-only when the author and reviewer share an identity.
- Gate expensive security review on a path-based trust-boundary check.

## Learn more

Explore [svennijhuis/agentic-workflows](https://github.com/svennijhuis/agentic-workflows), starting with its [workflows README](https://github.com/svennijhuis/agentic-workflows/blob/main/workflows/README.md) and the builder and reviewer pair. To build your own, start with [GitHub Agentic Workflows](https://github.com/github/gh-aw).
