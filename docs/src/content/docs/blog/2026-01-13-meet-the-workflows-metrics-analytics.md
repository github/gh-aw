---
title: "Meet the Workflows: Metrics & Analytics"
description: "A curated tour of metrics and analytics workflows that turn data into insights"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T06:00:00
sidebar:
  label: "Metrics & Analytics"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-quality-hygiene/
  label: "Fault Investigation Workflows"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-operations-release/
  label: "Operations & Release Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

Excellent journey! Now it's time to plunge into the *observatory* - the nerve center of [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-quality-hygiene/), we explored quality and hygiene workflows - the vigilant caretakers that investigate failed CI runs, detect schema drift, and catch breaking changes before users do. These workflows maintain codebase health by spotting problems before they escalate.

When you're running dozens of AI agents, how do you know if they're actually working well? How do you spot performance issues, cost problems, or quality degradation? That's where metrics and analytics workflows come in - they're the agents that monitor other agents. The aim is to turn raw activity data into actionable insights.

## Metrics & Analytics Workflows

Let's take a look at these three workflows:

- **[Metrics Collector](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/metrics-collector.md?plain=1)** - Tracks daily performance across the entire agent ecosystem
- **[Portfolio Analyst](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/portfolio-analyst.md?plain=1)** - Identifies cost reduction opportunities
- **[Audit Workflows](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/audit-workflows.md?plain=1)** - A meta-agent that audits all the other agents' runs

The Metrics Collector has created **41 daily metrics discussions** tracking performance across the agent ecosystem - for example, [#6986](https://github.com/github/gh-aw/discussions/6986) with the daily code metrics report. It became our central nervous system, gathering performance data that feeds into higher-level orchestrators.

Portfolio Analyst has created **7 portfolio analysis discussions** identifying cost reduction opportunities and token optimization patterns - for example, [#6499](https://github.com/github/gh-aw/discussions/6499) with a weekly portfolio analysis. The workflow has identified workflows that were costing us money unnecessarily (turns out some agents were way too chatty with their LLM calls).

Audit Workflows is our most prolific discussion-creating agent with **93 audit report discussions** and **9 issues**, acting as a meta-agent that analyzes logs, costs, errors, and success patterns across all other workflow runs. Four of its issues led to PRs by downstream agents.

Observability isn't optional when you're running dozens of AI agents - it's the difference between a well-oiled machine and an expensive black box.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Metrics Collector:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/metrics-collector.md
```

**Portfolio Analyst:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/portfolio-analyst.md
```

**Audit Workflows:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/audit-workflows.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

You can also [create your own workflows](/gh-aw/setup/creating-workflows).

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Operations & Release Workflows

Now that we can measure and optimize our agent ecosystem, let's talk about the moment of truth: actually shipping software to users.

Continue reading: [Operations & Release Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-operations-release/)

---

*This is part 9 of a 19-part series exploring the workflows in Peli's Agent Factory.*
