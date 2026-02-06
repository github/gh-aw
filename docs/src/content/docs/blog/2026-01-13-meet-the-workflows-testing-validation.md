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

- **[Daily Testify Uber Super Expert](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-testify-uber-super-expert.md?plain=1)** - Analyzes test files daily and suggests testify-based improvements
- **[Daily Test Improver](https://github.com/githubnext/agentics/blob/main/workflows/daily-test-improver.md?plain=1)** - Identifies coverage gaps and implements new tests incrementally
- **[Daily Compiler Quality Check](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-compiler-quality.md?plain=1)** - Analyzes compiler code to ensure it meets quality standards

### User Experience & Integration Testing

- **[Daily Multi-Device Docs Tester](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-multi-device-docs-tester.md?plain=1)** - Tests documentation across devices with Playwright (mobile matters!)
- **[CLI Consistency Checker](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/cli-consistency-checker.md?plain=1)** - Inspects the CLI for inconsistencies, typos, and documentation gaps

### CI/CD Optimization

- **[CI Coach](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/ci-coach.md?plain=1)** - Analyzes CI pipelines and suggests optimizations
- **[Workflow Health Manager](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/workflow-health-manager.md?plain=1)** - Meta-orchestrator monitoring health of all agentic workflows

The Daily Testify Expert and Daily Test Improver work together to continuously improve our test suite - one analyzes existing tests for quality improvements, the other identifies coverage gaps and implements new tests. The Compiler Quality Check and Breaking Change Checker maintain code quality and API stability.

The Multi-Device Docs Tester uses Playwright to test our documentation on different screen sizes - it found mobile rendering issues we never would have caught manually. The CLI Consistency Checker helps maintain developer experience by catching UX inconsistencies.

The CI Coach suggests pipeline optimizations to keep builds fast, while the Workflow Health Manager watches all these watchers, ensuring the testing infrastructure itself stays healthy.

These workflows embody the principle: **trust but verify**. Just because it worked yesterday doesn't mean it works today.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Daily Testify Uber Super Expert:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-testify-uber-super-expert.md
```

**Daily Test Improver:**

```bash
gh aw add githubnext/agentics/workflows/daily-test-improver.md
```

**Daily Compiler Quality Check:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-compiler-quality.md
```

**Daily Multi-Device Docs Tester:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-multi-device-docs-tester.md
```

**CLI Consistency Checker:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/cli-consistency-checker.md
```

**CI Coach:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/ci-coach.md
```

**Workflow Health Manager:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/workflow-health-manager.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Monitoring the Monitors

But what about the infrastructure itself? Who watches the watchers? Time to go meta.

Continue reading: [Tool & Infrastructure Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-tool-infrastructure/)

---

*This is part 14 of a 19-part series exploring the workflows in Peli's Agent Factory.*
