---
title: "Meet the Workflows: Interactive & ChatOps"
description: "A curated tour of interactive workflows that respond to commands"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T10:00:00
sidebar:
  label: "Interactive & ChatOps"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-creative-culture/
  label: "Teamwork & Culture Workflows"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-testing-validation/
  label: "Testing & Validation Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Onwards, onwards!* Let's keep exploring the wonders of [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)! To the *command center* where instant magic happens!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-creative-culture/), we explored creative and culture workflows - agents that bring joy, build team culture, and create moments of delight. We discovered that AI agents don't have to be all business; they can have personality while making work more enjoyable.

But sometimes you need help *right now*, at the exact moment you're stuck on a problem. You don't want to wait for a scheduled run - you want to summon an expert agent with a command. That's where interactive workflows and ChatOps come in. These agents respond to slash commands and GitHub reactions, providing on-demand assistance with full context of the current situation.

We learned that the right agent at the right moment with the right information is a valuable addition to an agent portfolio.

## Interactive & ChatOps Workflows

These agents respond to commands, providing on-demand assistance whenever you need it:

- **[Q](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/q.md?plain=1)** - Workflow optimizer that investigates performance and creates PRs  
- **[Grumpy Reviewer](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/grumpy-reviewer.md?plain=1)** - Performs critical code reviews with personality
- **[Workflow Generator](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/workflow-generator.md?plain=1)** - Creates new workflows from issue requests

Interactive workflows changed how we think about agent invocation. Instead of everything running on a schedule, these respond to slash commands and reactions - `/q` summons the workflow optimizer, a 🚀 reaction triggers analysis. Q (yes, named after the James Bond quartermaster) became our go-to troubleshooter - it investigates workflow performance issues and opens PRs with optimizations.

The Grumpy Reviewer gave us surprisingly valuable feedback with a side of sass ("This function is so nested it has its own ZIP code"). We learned that **context is king** - these agents work because they're invoked at the right moment with the right context, not because they run on a schedule.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Q:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/q.md
```

**Grumpy Reviewer:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/grumpy-reviewer.md
```

**Workflow Generator:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/workflow-generator.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Testing & Validation Workflows

While ChatOps agents respond to commands, we also need workflows that continuously verify our systems still function as expected.

Continue reading: [Testing & Validation Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-testing-validation/)

---

*This is part 13 of a 19-part series exploring the workflows in Peli's Agent Factory.*
