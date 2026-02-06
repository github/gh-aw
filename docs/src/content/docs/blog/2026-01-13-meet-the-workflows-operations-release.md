---
title: "Meet the Workflows: Operations & Release"
description: "A curated tour of operations and release workflows that ship software"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T07:00:00
sidebar:
  label: "Operations & Release"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-metrics-analytics/
  label: "Metrics & Analytics Workflows"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-security-compliance/
  label: "Security-related Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

Ah! Right this way to our next chamber in [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)! The chamber where our AI agents enhance the magical moment of *shipping software*.

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-metrics-analytics/), we explored metrics and analytics workflows - the agents that monitor other agents, turning raw activity data into actionable insights.

## Operations & Release Workflows

The agents that help us actually ship software:

- **[Release](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/release.md?plain=1)** - Orchestrates builds, tests, and release note generation
- **[Changeset](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/changeset.md?plain=1)** - Manages version bumps and changelog entries for releases
- **[Daily Workflow Updater](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-workflow-updater.md?plain=1)** - Keeps actions and dependencies current (because dependency updates never stop)  

Shipping software is stressful enough without worrying about whether you formatted your release notes correctly.

The Release workflow handles the entire orchestration - building, testing, generating coherent release notes from commits, and publishing. What's interesting here is the **reliability** requirement: these workflows can't afford to be creative or experimental. They need to be deterministic, well-tested, and boring (in a good way).

The Changeset workflow automates the tedious task of version bumps and changelog entries. It analyzes commits since the last release, determines the appropriate version bump (major, minor, patch), and updates the changelog accordingly.

The Daily Workflow Updater taught us that maintenance is a perfect use case for agents - it's repetitive, necessary, and nobody enjoys doing it manually. These workflows handle the toil so we can focus on the interesting problems.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Release:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/release.md
```

**Changeset:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/changeset.md
```

**Daily Workflow Updater:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-workflow-updater.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Security-related Workflows

After all this focus on shipping, we need to talk about the guardrails: how do we ensure these powerful agents operate safely?

Continue reading: [Security-related Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-security-compliance/)

---

*This is part 10 of a 19-part series exploring the workflows in Peli's Agent Factory.*
