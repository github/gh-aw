---
title: "Meet the Workflows: Organization & Cross-Repo"
description: "A curated tour of workflows that operate at organization scale"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T14:00:00
sidebar:
  label: "Organization & Cross-Repo"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-multi-phase/
  label: "Multi-Phase Improver Workflows"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-advanced-analytics/
  label: "Advanced Analytics & ML Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

Let's zoom out at [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-multi-phase/), we explored multi-phase improver workflows - our most ambitious agents that tackle big projects over multiple days, maintaining state and making incremental progress. These workflows proved that AI agents can handle complex, long-running initiatives when given the right architecture.

But all that sophisticated functionality has focused on a single repository. What happens when you zoom out to organization scale? What insights emerge when you analyze dozens or hundreds of repositories together? What looks perfectly normal in one repo might be a red flag across an organization. Organization and cross-repo workflows operate at enterprise scale, requiring careful permission management, thoughtful rate limiting, and different analytical lenses. Let's explore workflows that see the forest, not just the trees.

## Organization & Cross-Repo Workflows

These agents work at organization scale, across multiple repositories:

- **[Org Health Report](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/org-health-report.md?plain=1)** - Organization-wide repository health metrics  
- **[Stale Repo Identifier](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/stale-repo-identifier.md?plain=1)** - Identifies inactive repositories  
- **[Ubuntu Image Analyzer](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/ubuntu-image-analyzer.md?plain=1)** - Documents GitHub Actions runner environments  

Scaling agents across an entire organization changes the game. The Org Health Report analyzes dozens of repositories at once, identifying patterns and outliers ("these three repos have no tests, these five haven't been updated in months"). The Stale Repo Identifier helps with organizational hygiene - finding abandoned projects that should be archived or transferred. We learned that **cross-repo insights are different** - what looks fine in one repository might be an outlier across the organization. These workflows require careful permission management (reading across repos needs organization-level tokens) and thoughtful rate limiting (you can hit API limits fast when analyzing 50+ repos). The Ubuntu Image Analyzer is wonderfully meta - it documents the very environment that runs our agents.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Org Health Report:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/org-health-report.md
```

**Stale Repo Identifier:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/stale-repo-identifier.md
```

**Ubuntu Image Analyzer:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/ubuntu-image-analyzer.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Advanced Analytics & ML Workflows

Cross-repo insights reveal patterns, but we wanted to go even deeper - using machine learning to understand agent behavior.

Continue reading: [Advanced Analytics & ML Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-advanced-analytics/)

---

*This is part 17 of a 19-part series exploring the workflows in Peli's Agent Factory.*
