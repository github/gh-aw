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

- **[CI Doctor](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/ci-doctor.md?plain=1)** - Investigates failed workflows and opens diagnostic issues - **9 merged PRs out of 13 proposed (69% merge rate)**  
- **[Schema Consistency Checker](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/schema-consistency-checker.md?plain=1)** - Detects when schemas, code, and docs drift apart - **55 analysis discussions** created  
- **[Breaking Change Checker](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/breaking-change-checker.md?plain=1)** - Watches for changes that might break things for users - creates alert issues  

The CI Doctor (also known as "CI Failure Doctor") was one of our most important workflows. Instead of drowning in CI failure notifications, we now get *timely*, *investigated* failures with actual diagnostic insights. The agent doesn't just tell us something broke - it analyzes logs, identifies patterns, searches for similar past issues, and even suggests fixes - even before the human has read the failure notification. CI Failure Doctor has contributed **9 merged PRs out of 13 proposed (69% merge rate)**, including fixes like [adding Go module download pre-flight checks](https://github.com/github/gh-aw/pull/13740) and [adding retry logic to prevent proxy 403 failures](https://github.com/github/gh-aw/pull/13155). We learned that agents excel at the tedious investigation work that humans find draining.

The Schema Consistency Checker has created **55 analysis discussions** examining schema drift between JSON schemas, Go structs, and documentation - for example, [#7020](https://github.com/github/gh-aw/discussions/7020) analyzing conditional logic consistency across the codebase. It caught drift that would have taken us days to notice manually.

Breaking Change Checker is a newer workflow that monitors for backward-incompatible changes and creates alert issues (e.g., [#14113](https://github.com/github/gh-aw/issues/14113) flagging CLI version updates) before they reach production.

These "hygiene" workflows became our first line of defense, catching issues before they reached users.

The CI Doctor has inspired a growing range of similar workflows inside GitHub, where agents proactively do depth investigations of site incidents and failures. This is the future of operational excellence: AI agents kicking in immediately to do depth investigation, for faster organizational response.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**CI Doctor:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/ci-doctor.md
```

**Schema Consistency Checker:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/schema-consistency-checker.md
```

**Breaking Change Checker:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/breaking-change-checker.md
```

Then edit and remix the workflow specifications to meet your needs, regenerate the lock file using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

You can also [create your own workflows](/gh-aw/setup/creating-workflows).

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Metrics & Analytics Workflows

Next up, we look at workflows which help us understand if the agent collection as a whole is working well That's where metrics and analytics workflows come in.

Continue reading: [Metrics & Analytics Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-metrics-analytics/)

---

*This is part 8 of a 19-part series exploring the workflows in Peli's Agent Factory.*
