---
title: "Meet the Workflows: Testing & Validation"
description: "A curated tour of testing workflows that keep everything running smoothly"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T11:00:00
sidebar:
  label: "Testing & Validation"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-interactive-chatops/
  label: "Interactive & ChatOps Workflows"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-tool-infrastructure/
  label: "Tool & Infrastructure Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Right this way!* Let's continue our grand tour of [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)! Into the *verification chamber* where nothing escapes scrutiny!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-interactive-chatops/), we explored ChatOps workflows - agents that respond to slash commands and GitHub reactions, providing on-demand assistance with full context.

But making code *better* is only half the battle. We also need to ensure it keeps *working*. As we refactor, optimize, and evolve our codebase, how do we know we haven't broken something? How do we catch regressions before users do? That's where testing and validation workflows come in - the skeptical guardians that continuously verify our systems still function as expected. We learned that AI infrastructure needs constant health checks, because what worked yesterday might silently fail today. These workflows embody **trust but verify**.

## Testing & Validation Workflows

These agents keep everything running smoothly through continuous testing:

### Code Quality & Test Validation

- **[Daily Testify Uber Super Expert](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-testify-uber-super-expert.md?plain=1)** - Analyzes test files daily and suggests testify-based improvements - **19 issues created**, **13 led to merged PRs (100% causal chain merge rate)**
- **[Daily Test Improver](https://github.com/githubnext/agentics/blob/main/workflows/daily-test-improver.md?plain=1)** - Identifies coverage gaps and implements new tests incrementally
- **[Daily Compiler Quality Check](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-compiler-quality.md?plain=1)** - Analyzes compiler code to ensure it meets quality standards

### User Experience & Integration Testing

- **[Daily Multi-Device Docs Tester](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-multi-device-docs-tester.md?plain=1)** - Tests documentation across devices with Playwright - **2 merged PRs out of 2 proposed (100% merge rate)**
- **[CLI Consistency Checker](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/cli-consistency-checker.md?plain=1)** - Inspects the CLI for inconsistencies, typos, and documentation gaps - **80 merged PRs out of 102 proposed (78% merge rate)**

### CI/CD Optimization

- **[CI Coach](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/ci-coach.md?plain=1)** - Analyzes CI pipelines and suggests optimizations - **9 merged PRs out of 9 proposed (100% merge rate)**
- **[Workflow Health Manager](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/workflow-health-manager.md?plain=1)** - Meta-orchestrator monitoring health of all agentic workflows - **40 issues created**, **5 direct PRs + 14 causal chain PRs merged**

The Daily Testify Expert has created **19 issues** analyzing test quality, and **13 of those issues led to merged PRs** by downstream agents - a perfect 100% causal chain merge rate. For example, [issue #13701](https://github.com/github/gh-aw/issues/13701) led to [#13722](https://github.com/github/gh-aw/pull/13722) modernizing console render tests with testify assertions. The Daily Test Improver works alongside it to identify coverage gaps and implement new tests.

The Multi-Device Docs Tester uses Playwright to test our documentation on different screen sizes - it has created **2 PRs (both merged)**, including [adding --network host to Playwright Docker containers](https://github.com/github/gh-aw/pull/7158). It found mobile rendering issues we never would have caught manually. The CLI Consistency Checker has contributed **80 merged PRs out of 102 proposed (78% merge rate)**, maintaining consistency in CLI interface and documentation. Recent examples include [removing undocumented CLI commands](https://github.com/github/gh-aw/pull/12762) and [fixing upgrade command documentation](https://github.com/github/gh-aw/pull/11559).

CI Optimization Coach has contributed **9 merged PRs out of 9 proposed (100% merge rate)**, optimizing CI pipelines for speed and efficiency with perfect execution. Examples include [removing unnecessary test dependencies](https://github.com/github/gh-aw/pull/13925) and [fixing duplicate test execution](https://github.com/github/gh-aw/pull/8176).

The Workflow Health Manager has created **40 issues** monitoring the health of all other workflows, with **25 of those issues leading to 34 PRs** (14 merged) by downstream agents - plus **5 direct PRs merged**. For example, [issue #14105](https://github.com/github/gh-aw/issues/14105) about a missing runtime file led to [#14127](https://github.com/github/gh-aw/pull/14127) fixing the workflow configuration.

These workflows embody the principle: **trust but verify**. Just because it worked yesterday doesn't mean it works today.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Daily Testify Uber Super Expert:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-testify-uber-super-expert.md
```

**Daily Test Improver:**

```bash
gh aw add-wizard githubnext/agentics/daily-test-improver
```

**Daily Compiler Quality Check:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-compiler-quality.md
```

**Daily Multi-Device Docs Tester:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-multi-device-docs-tester.md
```

**CLI Consistency Checker:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/cli-consistency-checker.md
```

**CI Coach:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/ci-coach.md
```

**Workflow Health Manager:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/workflow-health-manager.md
```

Then edit and remix the workflow specifications to meet your needs, regenerate the lock file using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

You can also [create your own workflows](/gh-aw/setup/creating-workflows).

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Monitoring the Monitors

But what about the infrastructure itself? Who watches the watchers? Time to go meta.

Continue reading: [Tool & Infrastructure Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-tool-infrastructure/)

---

*This is part 14 of a 19-part series exploring the workflows in Peli's Agent Factory.*
