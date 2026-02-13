---
title: "Meet the Workflows: Issue & PR Management"
description: "A curated tour of workflows that enhance GitHub collaboration"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T04:00:00
sidebar:
  label: "Issue & PR Management"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-documentation/
  label: "Meeth the Workflows: Continuous Documentation"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-quality-hygiene/
  label: "Fault Investigation Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Ah!* Let's discuss the art of managing issues and pull requests at [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)! A most delicious topic indeed!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-documentation/), we explored documentation and content workflows - agents that maintain glossaries, technical docs, slide decks, and blog content. We learned how we took a heterogeneous approach to documentation agents - some workflows generate content, others maintain it, and still others validate it.

Now let's talk about the daily rituals of software development: managing issues and pull requests. GitHub provides excellent primitives for collaboration, but there's ceremony involved - linking related issues, merging main into PR branches, assigning work, closing completed sub-issues, optimizing templates. These are small papercuts individually, but they can add up to significant friction.

## Issue & PR Management Workflows

These agents enhance issue and pull request workflows:

- **[Issue Arborist](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/issue-arborist.md?plain=1)** - Links related issues as sub-issues - **77 discussion reports** and **18 parent issues** created  
- **[Issue Monster](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/issue-monster.md?plain=1)** - Assigns issues to Copilot agents one at a time - **task dispatcher** for the whole system
- **[Mergefest](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/mergefest.md?plain=1)** - Automatically merges main branch into PR branches - **orchestrator workflow**
- **[Sub Issue Closer](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/sub-issue-closer.md?plain=1)** - Closes completed sub-issues automatically - **orchestrator workflow**
- **[Issue Template Optimizer](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/issue-template-optimizer.md?plain=1)** - Improves issue templates based on usage - **2 merged PRs out of 2 proposed (100% merge rate)**

The Issue Arborist is an **organizational workflow** that has created **77 discussion reports** (titled "[Issue Arborist] Issue Arborist Report") and **18 parent issues** to group related sub-issues. It keeps the issue tracker organized by automatically linking related issues, building a dependency tree we'd never maintain manually. For example, [#12037](https://github.com/github/gh-aw/issues/12037) grouped engine documentation updates.

The Issue Monster is the **task dispatcher** - it assigns issues to Copilot agents one at a time. It doesn't create PRs itself, but enables every other agent's work by feeding them tasks. This prevents the chaos of parallel work on the same codebase.

Mergefest is an **orchestrator workflow** that automatically merges main into PR branches, keeping long-lived PRs up to date without manual intervention. It eliminates the "please merge main" dance.

Sub Issue Closer automatically closes completed sub-issues when their parent issue is resolved, keeping the issue tracker clean.

Issue Template Optimizer is a recent addition - so far it has created **2 PRs (both merged)**, improving issue templates based on usage patterns. It analyzes which fields in our templates actually get filled out and suggests improvements.

Issue and PR management workflows don't replace GitHub's features; they enhance them, removing ceremony and making collaboration feel smoother.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Issue Arborist:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/issue-arborist.md
```

**Issue Monster:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/issue-monster.md
```

**Mergefest:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/mergefest.md
```

**Sub Issue Closer:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/sub-issue-closer.md
```

**Issue Template Optimizer:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/issue-template-optimizer.md
```

Then edit and remix the workflow specifications to meet your needs, regenerate the lock file using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

You can also [create your own workflows](/gh-aw/setup/creating-workflows).

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Fault Investigation Workflows

Next up we look at agents that maintain codebase health - spotting problems before they escalate.

Continue reading: [Fault Investigation Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-quality-hygiene/)

---

*This is part 7 of a 19-part series exploring the workflows in Peli's Agent Factory.*
