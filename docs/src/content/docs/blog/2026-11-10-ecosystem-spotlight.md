---
title: "Ecosystem Spotlight: elastic/oblt-aw"
description: "A framework that distributes thin agentic workflow clients across a fleet of repositories and gates every run behind a checkbox dashboard."
authors:
  - copilot
date: 2026-11-10
metadata:
  seoDescription: "How elastic/oblt-aw distributes thin gh-aw clients, gates runs with an issue-based dashboard, and promotes pinned releases across many repositories."
  linkedPostText: "Ecosystem Spotlight: fleet-scale agentic workflows with oblt-aw"
---

Wiring an agentic workflow by hand into every repository works for one repository and falls apart at fifty. [elastic/oblt-aw](https://github.com/elastic/oblt-aw) is an opinionated framework for GitHub Agentic Workflows that addresses this directly: shared, reusable routes live in one repository, small "client" workflows are installed automatically into consumer repositories, and a control plane decides whether each agent is allowed to run. It is aimed at platform and developer-productivity teams that need to roll out, tune and switch off agents across many repositories without chasing each one.

## Why it's awesome

**Thin clients, central logic.** Consumers hold only narrow-trigger files such as `trigger-obs-aw-pull-request.yml`, each of which calls an event-scoped orchestrator in the framework repository. The [client template doc](https://github.com/elastic/oblt-aw/blob/main/docs/workflows/obs-aw-client-template.md) explains the split-trigger design: only the routes that match the GitHub event run, which keeps pull requests quiet. Improving an agent means changing it once in `oblt-aw`.

**A checkbox dashboard as the on/off switch.** Each repository gets a single pinned issue, `[oblt-aw] Control Plane Dashboard`, with one task-list entry per workflow. The [get-enabled-workflows](https://github.com/elastic/oblt-aw/blob/main/docs/workflows/get-enabled-workflows.md) workflow reads it with only `contents: read` and `issues: read`, and the rules are fail-closed: no open dashboard issue means no gated workflow proceeds. Repository owners get a self-service control with no config file to edit. The [dashboard guide](https://github.com/elastic/oblt-aw/blob/main/docs/operations/control-plane-dashboard.md) describes the flow.

**A shared prelude, enforced by CI.** [aw-prelude](https://github.com/elastic/oblt-aw/blob/main/docs/workflows/aw-prelude.md) runs once per event family. It reads the dashboard, optionally loads bot allow lists for pull request and issue authors, and emits a `proceed-by-workflow` map. A validation script (`validate_aw_workflow_prelude.py`) checks that every route declares the `shared-proceed` input, so a new workflow cannot quietly skip the gate. A matching check covers [aw-resolve-agentic-assets](https://github.com/elastic/oblt-aw/blob/main/docs/workflows/aw-resolve-agentic-assets.md), which merges platform instructions with a consumer's optional `apm.yml` so repositories can tailor an agent without forking it.

**Multi-organization ownership.** Per-organization configuration lives under `config/<org-key>/`, and checkboxes use compound ids like `org:workflow-id`. The [multi-org design](https://github.com/elastic/oblt-aw/blob/main/docs/architecture/multi-org-agentic-workflows.md) lets several teams share one catalog while a repository still sees one dashboard grouped by owner. A [maturity model](https://github.com/elastic/oblt-aw/blob/main/docs/operations/workflow-maturity.md) (stable, early adoption, experimental) labels each workflow so adopters know what they are opting into.

**A release train with rollback.** The [release model](https://github.com/elastic/oblt-aw/blob/main/docs/operations/release-model.md) separates `development` repositories, which follow `@main` and act as live canaries, from `production` repositories, which are pinned to a moving major tag. Promotion is a manual dispatch that runs the end-to-end suite first, then pushes an immutable audit tag and updates `config/release-pointers.json`. Rollback retargets the tag to the previous commit. The docs also record fail-closed rules: distribution will not write a pin for a tag that is missing on origin.

## How it works

1. A repository is listed in its organization's `active-repositories.json`.
2. The [distribution workflow](https://github.com/elastic/oblt-aw/blob/main/docs/operations/distribute-client-workflow.md) opens pull requests that install the client templates, substituting the right pin for the repository's `pin-class`. If two organizations would install different files at the same path, it stops with an explicit conflict.
3. A sync job creates or updates the dashboard issue.
4. When an event fires, the client calls its orchestrator, the prelude evaluates the gates, and enabled routes run an agent, resolving instructions first through the assets workflow.

Reusable agents that are called across repositories have one extra requirement. Elastic's sibling project [docs-actions](https://github.com/elastic/docs-actions/blob/main/agentic-workflows/DEVELOPING.md) documents that cross-repository `workflow_call` use depends on `inlined-imports: true`, so the compiled lock file is self-contained. That detail is easy to miss and worth copying if you publish reusable agents.

## Try it

`oblt-aw` is tailored to Elastic's organizations, so treat it as a reference architecture rather than a drop-in package. The patterns transfer well:

- Keep consumer workflows tiny and call a pinned reusable workflow centrally.
- Use an issue with checkboxes as a fail-closed, auditable enablement switch.
- Add a CI check that every agent route honors the gate.
- Separate canary repositories (`@main`) from production (`@v0`), and rehearse rollback.

For a smaller example of the same idea, [elastic/docs-actions](https://github.com/elastic/docs-actions) publishes reusable documentation agents with `example.yml` caller templates and a table of the safe outputs each workflow may use.

## Learn more

Explore [elastic/oblt-aw](https://github.com/elastic/oblt-aw), starting with its README and architecture overview, and see what a governed fleet of agents looks like. To build your own, start with [GitHub Agentic Workflows](https://github.com/github/gh-aw).
