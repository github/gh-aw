---
title: "Meet the Workflows: Tool & Infrastructure"
description: "A curated tour of infrastructure workflows that monitor the agentic systems"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T12:00:00
sidebar:
  label: "Tool & Infrastructure"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-testing-validation/
  label: "Testing & Validation Workflows"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-multi-phase/
  label: "Multi-Phase Improver Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Delighted to have you back* on our journey through [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)! Now, prepare yourself for something *quite peculiar* - the room where we watch the watchers!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-testing-validation/), we explored testing and validation workflows that continuously verify our systems function correctly - running smoke tests, checking documentation across devices, and catching regressions before users notice them. We learned that trust must be verified.

But here's a question that kept us up at night: what if the *infrastructure itself* fails? What if MCP servers are misconfigured, tools become unavailable, or agents can't access the capabilities they need? Testing the *application* is one thing; monitoring the *platform* that runs AI agents is another beast entirely. Tool and infrastructure workflows provide meta-monitoring - they watch the watchers, validate configurations, and ensure the invisible plumbing stays functional. Welcome to the layer where we monitor agents monitoring agents monitoring code. Yes, it gets very meta.

## Tool & Infrastructure Workflows

These agents monitor and analyze the agentic infrastructure itself:

- **[MCP Inspector](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/mcp-inspector.md?plain=1)** - Validates Model Context Protocol configurations  
- **[GitHub MCP Tools Report](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/github-mcp-tools-report.md?plain=1)** - Analyzes available MCP tools  
- **[Agent Performance Analyzer](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/agent-performance-analyzer.md?plain=1)** - Meta-orchestrator for agent quality  

Infrastructure for AI agents is different from traditional infrastructure - you need to validate that tools are available, properly configured, and actually working. The MCP Inspector checks Model Context Protocol server configurations because a misconfigured MCP server means an agent can't access the tools it needs. The Agent Performance Analyzer is a meta-orchestrator that monitors all our other agents - looking for performance degradation, cost spikes, and quality issues. We learned that **layered observability** is crucial: you need monitoring at the infrastructure level (are servers up?), the tool level (can agents access what they need?), and the agent level (are they performing well?).

These workflows provide visibility into the invisible.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**MCP Inspector:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/mcp-inspector.md
```

**GitHub MCP Tools Report:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/github-mcp-tools-report.md
```

**Agent Performance Analyzer:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/agent-performance-analyzer.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Multi-Phase Improver Workflows

Most workflows we've seen are stateless - they run, complete, and disappear. But what if agents could maintain memory across days?

Continue reading: [Multi-Phase Improver Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-multi-phase/)

---

*This is part 15 of a 19-part series exploring the workflows in Peli's Agent Factory.*
