---
title: "Meet the Workflows: Teamwork & Culture"
description: "A curated tour of creative and culture workflows that bring joy to work"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T09:00:00
sidebar:
  label: "Teamwork & Culture"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-security-compliance/
  label: "Security-related Workflows"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-interactive-chatops/
  label: "Interactive & ChatOps Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Oh, my dear friends!* Let's explore the *playful workshop* - the most fun corner of [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-security-compliance/), we explored security and compliance workflows - the essential guardrails that manage vulnerability campaigns, validate network security, and prevent credential exposure. These workflows let us sleep soundly knowing our agents operate within safe boundaries.

But here's the thing: work doesn't have to be all business. While we've built serious, production-critical workflows for quality, releases, and security, we also discovered something unexpected - AI agents can bring joy, build team culture, and create moments of delight. Not every workflow needs to solve a critical problem; some can simply make your day better. Let's explore the playful side of our agent factory, where we learned that personality and fun drive engagement just as powerfully as utility.

## Teamwork & Culture Workflows

These agents facilitate team communication and remind us that work can be fun:

- **[Daily Team Status](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-team-status.md?plain=1)** - Shares team mood and status updates  
- **[Daily News](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-news.md?plain=1)** - Curates relevant news for the team
- **[Poem Bot](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/poem-bot.md?plain=1)** - Responds to `/poem-bot` commands with creative verses (yes, really)  
- **[Weekly Issue Summary](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/weekly-issue-summary.md?plain=1)** - Creates digestible summaries complete with charts and trends
- **[Daily Repo Chronicle](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-repo-chronicle.md?plain=1)** - Narrates the day's activity like a storyteller - seriously, it's kind of delightful.

The Poem Bot started as a whimsy in our Copilot for PRs project in 2022. Someone said "wouldn't it be funny if we had an agent that writes poems about our code?" and then we built it. We learned that AI agents don't have to be all business - they can build culture and create moments of joy. We brought this forward to this project.

The Daily News workflow curates relevant articles, but it also adds commentary and connects them to our work.

The Weekly Issue Summary and Daily Repo Chronicle workflows turn dry data into engaging narratives, making it easier to stay informed without feeling overwhelmed.

A theme here is the **reduction of cognitive load**. Having agents summarize and narrate daily activity means we don't have to mentally parse long lists of issues or PRs. Instead, we get digestible stories that highlight what's important. This frees up mental bandwidth for actual work.

Another theme is that **tone** can help make things more enjoyable. The Daily Repo Chronicle started writing summaries in a narrative, almost journalistic style. The outputs from AI agents don't have to be robotic - they can have personality while still being informative.

These communication workflows help build team cohesion and remind us that work can be delightful.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Daily Team Status:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-team-status.md
```

**Daily News:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-news.md
```

**Poem Bot:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/poem-bot.md
```

**Weekly Issue Summary:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/weekly-issue-summary.md
```

**Daily Repo Chronicle:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-repo-chronicle.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Summon an Agent on Demand

Scheduled workflows are great, but sometimes you need help *right now*. Enter ChatOps and interactive workflows.

Continue reading: [Interactive & ChatOps Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-interactive-chatops/)

---

*This is part 12 of a 19-part series exploring the workflows in Peli's Agent Factory.*
