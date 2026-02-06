---
title: "Meet the Workflows: Fault Investigation"
description: "A curated tour of proactive fault investigation workflows that maintain codebase health"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T05:00:00
sidebar:
  label: "Fault Investigation"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-issue-management/
  label: "Issue & PR Management Workflows"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-metrics-analytics/
  label: "Metrics & Analytics Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Ah, splendid!* Welcome back to [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)! Come, let me show you the chamber where vigilant caretakers investigate faults before they escalate!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-issue-management/), we explored issue and PR management workflows.

Now let's shift from collaboration ceremony to fault investigation.

 While issue workflows help us handle what comes in, fault investigation workflows act as vigilant caretakers - spotting problems before they escalate and keeping our codebase healthy. These are the agents that investigate failed CI runs, detect schema drift, and catch breaking changes before users do.

## Fault Investigation Workflows

These are our diligent caretakers - the agents that spot problems before they become bigger problems:

- **[CI Doctor](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/ci-doctor.md?plain=1)** - Investigates failed workflows and opens diagnostic issues (it's like having a DevOps specialist on call 24/7)  
- **[Schema Consistency Checker](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/schema-consistency-checker.md?plain=1)** - Detects when schemas, code, and docs drift apart  
- **[Breaking Change Checker](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/breaking-change-checker.md?plain=1)** - Watches for changes that might break things for users  

The CI Doctor was one of our most important workflows. Instead of drowning in CI failure notifications, we now get *timely*, *investigated* failures with actual diagnostic insights. The agent doesn't just tell us something broke - it analyzes logs, identifies patterns, searches for similar past issues, and even suggests fixes - even before the human has read the failure notification. We learned that agents excel at the tedious investigation work that humans find draining.

The Schema Consistency Checker caught drift that would have taken us days to notice manually. 

These "hygiene" workflows became our first line of defense, catching issues before they reached users.

The CI Doctor has inspired a growing range of similar workflows inside GitHub, where agents proactively do depth investigations of site incidents and failures. This is the future of operational excellence: AI agents kicking in immediately to do depth investigation, for faster organizational response.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**CI Doctor:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/ci-doctor.md
```

**Schema Consistency Checker:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/schema-consistency-checker.md
```

**Breaking Change Checker:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/breaking-change-checker.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Metrics & Analytics Workflows

Next up, we look at workflows which help us understand if the agent collection as a whole is working well That's where metrics and analytics workflows come in.

Continue reading: [Metrics & Analytics Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-metrics-analytics/)

---

*This is part 8 of a 19-part series exploring the workflows in Peli's Agent Factory.*
