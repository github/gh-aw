---
title: "Welcome to Peli's Agent Factory"
description: "It's basically a candy shop chocolate factory of agentic workflows."
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-12
featured: true
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows/
  label: Meet the Workflows
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

Welcome, welcome, WELCOME to Peli's Agent Factory!

Imagine a software repository where AI agents work alongside your team - not replacing developers, but handling the repetitive, time-consuming tasks that slow down collaboration and forward progress.

Peli's Agent Factory is our exploration of what happens when you take the design philosophy of **"let's create a new automated agentic workflow for that"** as the answer to almost every opportunity that arises! What happens when you **max out on automated agentic workflows** - when you make and use dozens of specialized, automated AI agentic workflows and use them in practice.

Software development is changing rapidly. This is our attempt to understand how automated agentic AI can make software teams more efficient, collaborative, and more enjoyable.

It's basically a candy shop chocolate factory of agentic workflows. And we'd like to share it with you.

Let's explore together!

## What Is Peli's Agent Factory?

Peli's factory is a collection of [**automated agentic workflows**](https://gh.io/gh-aw) we use in practice. We have built and operated **over 100 automated agentic workflows** within the [`github/gh-aw`](https://github.com/github/gh-aw) repository. These were used mostly in the context of the [`github/gh-aw`](https://github.com/github/gh-aw) project itself, but some have also been applied at scale in GitHub internal repositories. These weren't hypothetical demos - they were working agents that:

- [Triage incoming issues](/gh-aw/blog/2026-01-13-meet-the-workflows/)
- [Diagnose CI failures](/gh-aw/blog/2026-01-13-meet-the-workflows-quality-hygiene/)
- [Maintain documentation](/gh-aw/blog/2026-01-13-meet-the-workflows-documentation/)
- [Improve test coverage](/gh-aw/blog/2026-01-13-meet-the-workflows-testing-validation/)
- [Monitor security compliance](/gh-aw/blog/2026-01-13-meet-the-workflows-security-compliance/)
- [Optimize workflow efficiency](/gh-aw/blog/2026-01-13-meet-the-workflows-metrics-analytics/)
- [Execute multi-day projects](/gh-aw/blog/2026-01-13-meet-the-workflows-multi-phase/)
- Even [write poetry to boost team morale](/gh-aw/blog/2026-01-13-meet-the-workflows-creative-culture/)

Some workflows are ["read-only analysts"](/gh-aw/blog/2026-01-13-meet-the-workflows-metrics-analytics/). Others [proactively propose changes through pull requests](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-simplicity/). Some are [meta-agents that monitor and improve the health of other workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-metrics-analytics/).

We know we're taking things to an extreme here. Most repositories won't need dozens of agentic workflows. No one can read all these outputs (except, of course, another workflow). But by pushing the boundaries, we learned valuable lessons about what works, what doesn't, and how to design safe, effective agentic workflows that teams can trust and use.

## Why Build a Factory?

When we started exploring agentic workflows, we faced a fundamental question: **What should repository-level automated agentic workflows actually do?**

Rather than trying to build one "perfect" agent, we took a broad, heterogeneous approach:

1. **Embrace diversity** - Create many specialized workflows as we identified opportunities
2. **Use them continuously** - Run them in real development workflows
3. **Observe what works** - Find which patterns work and which fail
4. **Share the knowledge** - Catalog the structures that make agents safe and effective

The factory becomes both an experiment and a reference collection - a living library of patterns that others can study, adapt, and remix. Each workflow is written in natural language using Markdown, then converted into secure [GitHub Actions](https://github.com/features/actions) that run with carefully scoped permissions with guardrails. Everything is observable, auditable, and remixable.

## Meet the Workflows

In our first series, [Meet the Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows/), we'll take you on a tour of the most interesting agents in the factory. Each article is bite-sized. If you'd like to skip ahead, here's the full list of articles in the series:

1. [Meet a Simple Triage Workflow](/gh-aw/blog/2026-01-13-meet-the-workflows/)
2. [Introducing Continuous Simplicity](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-simplicity/)
3. [Introducing Continuous Refactoring](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-refactoring/)
4. [Introducing Continuous Style](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-style/)
5. [Introducing Continuous Improvement](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-improvement/)
6. [Introducing Continuous Documentation](/gh-aw/blog/2026-01-13-meet-the-workflows-documentation/)

After that we have a cornucopia of specialized workflow categories for you to dip into:

- [Meet the Issue & PR Management Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-issue-management/)
- [Meet the Fault Investigation Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-quality-hygiene/)
- [Meet the Metrics & Analytics Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-metrics-analytics/)
- [Meet the Operations & Release Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-operations-release/)
- [Meet the Security-related Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-security-compliance/)
- [Meet the Teamwork & Culture Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-creative-culture/)
- [Meet the Interactive & ChatOps Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-interactive-chatops/)
- [Meet the Testing & Validation Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-testing-validation/)
- [Meet the Tool & Infrastructure Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-tool-infrastructure/)
- [Introducing Multi-Phase Improver Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-multi-phase/)
- [Meet the Organization & Cross-Repo Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-organization/)
- [Go Deep with Advanced Analytics & ML Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-advanced-analytics/)
- [Go Deep with Project Coordination Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows-campaigns/)

Every post comes with instructions about how to add the workflow to your own repository, or cusomtize and remix it to create your own variant.

## What We're Learning

Running this many agents in production is a learning experience! We've watched agents succeed spectacularly and fail in instructive ways. Over the next few weeks, we'll also be sharing what we've learned through a series of detailed articles. We'll be looking at the design and operational patterns we've discovered, security lessons, and practical guides for building your own workflows.

To give a taste, some key lessons are emerging:

- **Repository-level automation is powerful** - Agents embedded in the development workflow can have outsized impact
- **Specialization reveals possibilities** - Focused agents allowed us to find more useful applications of automation than a single monolithic coding agent
- **Guardrails enable innovation** - Strict constraints actually make it easier to experiment safely
- **Meta-agents are valuable** - Agents that watch other agents become incredibly valuable
- **Cost-quality tradeoffs are real** - Longer analyses aren't always better

We'll dive deeper into these lessons in upcoming articles.

## Try It Yourself

Want to start with automated agentic workflows on GitHub? See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/).

## Learn More

- **[Meet the Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows/)** - The 19-part tour of the workflows
- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Credits

**Peli's Agent Factory** is by GitHub Next, Microsoft Research and collaborators, including Peli de Halleux, Don Syme, Mara Kiefer, Edward Aftandilian, Russell Horton, Jiaxiao Zhou. This is part of GitHub Next's exploration of [Continuous AI](https://githubnext.com/projects/continuous-ai) - making AI-enriched automation as routine as CI/CD.

## Factory Status

[Current Factory Status](/gh-aw/agent-factory-status)
