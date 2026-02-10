---
title: "Meet the Workflows: Project Coordination"
description: "A curated tour of workflows that coordinate multi-agent projects"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T16:00:00
sidebar:
  label: "Campaigns & Projects"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-advanced-analytics/
  label: "Advanced Analytics & ML Workflows"
# next:
#   link: /gh-aw/blog/2026-01-21-twelve-lessons/
#   label: 12 Lessons
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

My dear friends, we've arrived at the *grand finale* - the most spectacular room of all in [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)!

We've journeyed through 18 categories of workflows - from triage bots to code quality improvers, from security guards to creative poets, culminating in [advanced analytics](/gh-aw/blog/2026-01-13-meet-the-workflows-advanced-analytics/) that use machine learning to understand agent behavior patterns. Each workflow handles its individual task admirably.

But here's the ultimate challenge: how do you coordinate *multiple* agents working toward a shared goal? How do you break down a large initiative like "migrate all workflows to a new engine" into trackable sub-tasks that different agents can tackle? How do you monitor progress, alert on delays, and ensure the whole is greater than the sum of its parts? This final post explores planning, task-decomposition and project coordination workflows - the orchestration layer that proves AI agents can handle not just individual tasks, but entire structured projects requiring careful coordination and progress tracking.

## Planning & Project Coordination Workflows

These agents coordinate multi-agent plans and projects:

- **[Plan Command](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/plan.md?plain=1)** - Breaks down issues into actionable sub-tasks via `/plan` command - **514 merged PRs out of 761 proposed (67% merge rate)**
- **[Discussion Task Miner](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/discussion-task-miner.md?plain=1)** - Extracts actionable tasks from discussion threads - **60 merged PRs out of 105 proposed (57% merge rate)**

Plan Command has contributed **514 merged PRs out of 761 proposed (67% merge rate)**, providing on-demand task decomposition that breaks complex issues into actionable sub-tasks. This is the **highest-volume workflow by attribution** in the entire factory. Developers can comment `/plan` on any issue to get an AI-generated breakdown into actionable sub-issues that agents can work on. A verified example causal chain: [Discussion #7631](https://github.com/github/gh-aw/discussions/7631) → [Issue #8058](https://github.com/github/gh-aw/issues/8058) → [PR #8110](https://github.com/github/gh-aw/pull/8110).

Discussion Task Miner has contributed **60 merged PRs out of 105 proposed (57% merge rate)**, continuously scanning discussions to extract actionable tasks that might otherwise be lost. The workflow demonstrates perfect causal chain attribution: when it creates an issue from a discussion, and Copilot Coding Assistant later fixes that issue, the resulting PR is correctly attributed to Discussion Task Miner. A verified example: [Discussion #13934](https://github.com/github/gh-aw/discussions/13934) → [Issue #14084](https://github.com/github/gh-aw/issues/14084) → [PR #14129](https://github.com/github/gh-aw/pull/14129). Recent merged examples include [fixing firewall SSL-bump field extraction](https://github.com/github/gh-aw/pull/13920) and [adding security rationale to permissions documentation](https://github.com/github/gh-aw/pull/13918).

We learned that individual agents are great at focused tasks, but orchestrating multiple agents toward a shared goal requires careful architecture. Project coordination isn't just about breaking down work - it's about discovering work (Task Miner), planning work (Plan Command), and tracking work (Workflow Health Manager).

These workflows implement patterns like epic issues, progress tracking, and deadline management. They prove that AI agents can handle not just individual tasks, but entire projects when given proper coordination infrastructure.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Plan Command:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/plan.md
```

**Discussion Task Miner:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/discussion-task-miner.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

You can also [create your own workflows](/gh-aw/setup/creating-workflows).

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

---

## What We've Learned

Throughout this 19-part journey, we've explored workflows spanning from simple triage bots to sophisticated multi-phase improvers, from security guards to creative poets, from individual task automation to organization-wide orchestration.

The key insight? **AI agents are most powerful when they're specialized, well-coordinated, and designed for their specific context.** No single agent does everything - instead, we have an ecosystem where each agent excels at its particular job, and they work together through careful orchestration.

We've learned that observability is essential, that incremental progress beats heroic efforts, that security needs careful boundaries, and that even "fun" workflows can drive meaningful engagement. We've discovered that AI agents can maintain documentation, manage campaigns, analyze their own behavior, and continuously improve codebases - when given the right architecture and guardrails.

As you build your own agentic workflows, remember: start small, measure everything, iterate based on real usage, and don't be afraid to experiment. The workflows we've shown you evolved through experimentation and real-world use. Yours will too.

*This is part 19 (final) of a 19-part series exploring the workflows in Peli's Agent Factory.*
