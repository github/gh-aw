---
title: "Ecosystem Spotlight: sabbasi3-factory-lab/factory"
description: "An agentic software factory where gh-aw agents do the work and deterministic code enforces the rules."
authors:
  - copilot
date: 2026-11-06
metadata:
  seoDescription: "How sabbasi3-factory-lab/factory builds software with gh-aw agents: tiered PRs, hash-chained audit logs, spend caps, a kill switch and independent verification."
  linkedPostText: "Ecosystem Spotlight: an agentic factory built on gh-aw"
---

Most gh-aw showcases focus on a single workflow. [sabbasi3-factory-lab/factory](https://github.com/sabbasi3-factory-lab/factory) asks a bigger question: what does it take to let agents ship a whole product, with a human approving *policies* rather than individual changes? Its answer is a rulebook repository, a plain-code controller and a set of gh-aw workflows that together ported a Rust date-parsing library, [chrono-node-rs](https://github.com/sabbasi3-factory-lab/chrono-node-rs), almost entirely autonomously. It is aimed at teams exploring how far agentic automation can be trusted when the guardrails are explicit and auditable.

## Why it's awesome

**Agents work, code governs.** The [README](https://github.com/sabbasi3-factory-lab/factory/blob/main/README.md) separates roles cleanly. The *controller* is deterministic code on GitHub Actions with no AI: it picks tasks, starts agents, watches checks, merges and records everything. The *agent* implements one task per pull request through a gh-aw `implement` workflow. A *reviewer* from a different model family inspects the risky PRs. No single AI both writes and approves a change.

**Risk-tiered merging.** Every PR is assigned a tier in [policies/tiers.yaml](https://github.com/sabbasi3-factory-lab/factory/blob/main/policies/tiers.yaml): `auto` for routine work, `auto-strict` for shared or core code (requiring reviewer approval, a full regression run and automatic revert if `main` breaks), and `human` for the rules and referees themselves, such as golden tests, gates, workflows and the controller.

**Rules live apart from the code they govern.** Agents work in the product repository and cannot edit the policies or the verifier. The controller reads policy at a pinned commit, and the rulebook repo states plainly that agents can't write to it.

**Tamper-evident records.** Each controller decision is a hash-chained event on an append-only `audit` branch that only the controller App can push to. An independent [verifier](https://github.com/sabbasi3-factory-lab/factory/tree/main/verifier), running without the App key, re-checks the whole chain on a schedule and appends anchors to its own branch.

**Operational safety nets.** A [heartbeat watchdog](https://github.com/sabbasi3-factory-lab/factory/blob/main/dashboard/watchdog.mjs) opens a liveness issue if the controller or verifier goes quiet, or if scheduled workflows fail twice in a row. A `FACTORY_ENABLED` variable acts as a kill switch, and [policies/budgets.yaml](https://github.com/sabbasi3-factory-lab/factory/blob/main/policies/budgets.yaml) caps AI spend per run, per task, per workflow per day and for the whole factory per day.

## How it works

1. The controller selects the next planned task and dispatches the gh-aw `implement` workflow.
2. The agent opens one PR for that task.
3. If CI fails, the controller starts the gh-aw `repair` workflow, at most twice per task. A repeated or persistent failure goes to a person.
4. Depending on the tier, the PR is merged automatically, merged after strict gates and an independent review, or held for the owner.
5. The `outcomes` workflow runs `gh aw logs --outcomes` after every agent run and publishes the results to a branch that feeds a public dashboard.

## Results reported by the project

As of 2026-11-04, the README reports 47 of 47 planned tasks merged (39 routine, 8 core) between Oct 22 and Nov 3 with zero reverts. 45 merged without needing a repair run, the median time from start to merge was 13 minutes, and agent runs cost $40.12 in total, about $0.76 per accepted PR. These figures come from the project's own audit log and dashboard, so treat them as self-reported.

## Try it

The project is a design to learn from rather than an installable package. The most reusable ideas are easy to adapt to your own repositories:

- Classify PRs by risk and let policy, not the agent, decide how each tier merges.
- Keep policies and verification code outside the agents' write scope.
- Put budget caps and a kill switch in place before scaling up.
- Use `gh aw logs --outcomes` to track how agent runs actually performed.

The [DESIGN.md](https://github.com/sabbasi3-factory-lab/factory/blob/main/DESIGN.md) is the source of truth, and the build journal in `docs/JOURNAL.md` records decisions and lessons learned.

## Explore further

Take a look at the [factory repository](https://github.com/sabbasi3-factory-lab/factory) and its live control room, then tell us what you are building. To start your own agentic workflows, visit [github/gh-aw](https://github.com/github/gh-aw).
