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

- **[Daily Team Status](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-team-status.md?plain=1)** - Shares team mood and status updates - **22 issues**, **17 discussions** (plus 2 causal chain PRs!)  
- **[Daily News](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-news.md?plain=1)** - Curates relevant news for the team - **45 news digest discussions**
- **[Poem Bot](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/poem-bot.md?plain=1)** - Responds to `/poem-bot` commands with creative verses (yes, really)  
- **[Weekly Issue Summary](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/weekly-issue-summary.md?plain=1)** - Creates digestible summaries complete with charts and trends - **5 weekly analysis discussions**
- **[Daily Repo Chronicle](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-repo-chronicle.md?plain=1)** - Narrates the day's activity like a storyteller - **6 chronicle discussions**

The Poem Bot started as a whimsy in our Copilot for PRs project in 2022. Someone said "wouldn't it be funny if we had an agent that writes poems about our code?" and then we built it. Poem Bot responds to `/poem-bot` commands with creative verses about code, adding a touch of whimsy to the development workflow. We learned that AI agents don't have to be all business - they can build culture and create moments of joy.

Daily News has created **45 news digest discussions** curating relevant developments for the team - for example, [#6932](https://github.com/github/gh-aw/discussions/6932) with the daily status roundup. It shares links, adds commentary and connects them to our work.

Daily Team Status has created **22 issues** and **17 discussions** sharing daily team status updates - for example, [#6930](https://github.com/github/gh-aw/discussions/6930) with the daily team status report. Two of its issues even led to merged PRs by downstream agents, showing that even "soft" workflows can drive concrete improvements.

Weekly Issue Summary has created **5 weekly analysis discussions** with digestible summaries, charts, and trends - for example, [#5844](https://github.com/github/gh-aw/discussions/5844) analyzing the week of December 1-8, 2025.

Daily Repo Chronicle has created **6 chronicle discussions** narrating the repository's activity like a storyteller - for example, [#6750](https://github.com/github/gh-aw/discussions/6750) chronicling a development surge with 42 active PRs.

A theme here is the **reduction of cognitive load**. Having agents summarize and narrate daily activity means we don't have to mentally parse long lists of issues or PRs. Instead, we get digestible stories that highlight what's important. This frees up mental bandwidth for actual work.

Another theme is that **tone** can help make things more enjoyable. The Daily Repo Chronicle started writing summaries in a narrative, almost journalistic style. The outputs from AI agents don't have to be robotic - they can have personality while still being informative.

These communication workflows help build team cohesion and remind us that work can be delightful.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Daily Team Status:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-team-status.md
```

**Daily News:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-news.md
```

**Poem Bot:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/poem-bot.md
```

**Weekly Issue Summary:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/weekly-issue-summary.md
```

**Daily Repo Chronicle:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-repo-chronicle.md
```

Then edit and remix the workflow specifications to meet your needs, regenerate the lock file using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

You can also [create your own workflows](/gh-aw/setup/creating-workflows).

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Summon an Agent on Demand

Scheduled workflows are great, but sometimes you need help *right now*. Enter ChatOps and interactive workflows.

Continue reading: [Interactive & ChatOps Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-interactive-chatops/)

---

*This is part 12 of a 19-part series exploring the workflows in Peli's Agent Factory.*
