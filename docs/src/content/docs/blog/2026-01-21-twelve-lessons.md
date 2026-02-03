---
title: "12 Lessons from Peli's Agent Factory"
description: "Key insights about what works, what doesn't, and how to design effective agent ecosystems"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-21
draft: true
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows/
  label: Meet the Workflows
next:
  link: /gh-aw/blog/2026-01-24-design-patterns/
  label: 12 Design Patterns
---

[Previous: Meet the Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows/)

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*How delightful to see you again* at Peli's Agent Factory! Come, come - let me show you what we've discovered in the *lesson chamber*!

In our [previous article](/gh-aw/blog/2026-01-13-meet-the-workflows/), we introduced you to the workflows themselves. Now let's talk about what we've learned.

Running our collection of automated agentic workflows in practice is... quite the experience. We've watched agents succeed spectacularly, fail in interesting ways, and surprise us constantly. Along the way, we've learned some hard-won lessons about what makes agent ecosystems actually work.

Here's what we've figured out so far.

## The 12 Key Lessons

### Diversity Beats Perfection

No single agent can do everything - and that's perfectly fine. A collection of focused agents, each doing one thing well, works better than trying to build a single universal assistant. Instead of spending months perfecting a "super agent," we started shipping specialized agents quickly.

### Guardrails Enable Innovation

Something counter-intuitive we discovered is that strict constraints make it *easier* to experiment. [Safe outputs](https://github.github.com/gh-aw/reference/safe-outputs/), limited permissions, allowlisted tools - they don't slow us down. They give us the confidence to move fast because we know the blast radius of any failure.

With clear boundaries in place, we can prototype new agents without worrying about breaking production. Safe outputs prevent agents from accidentally deleting code or closing critical issues. Network allowlists ensure agents can't leak data to unauthorized services. These guardrails give us permission to innovate boldly.

### Meta-Agents Are Essential

Agents that watch other agents? Sounds meta, but they've become some of our most valuable workflows. They catch issues early and help us understand what's happening across the entire ecosystem.

Once we passed 50 workflows, tracking everything manually became impossible. Meta-agents like [Audit Workflows](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/audit-workflows.md) and [Agent Performance Analyzer](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/agent-performance-analyzer.md) give us the observability layer we desperately needed. They spot patterns across runs, identify struggling agents, and surface systemic issues we'd never catch looking at individual workflows.

### Personality Matters

Turns out, agents with distinct personalities - like the meticulous auditor, the helpful janitor, the creative poet - are way easier for teams to understand and trust.

We noticed generic names like "issue-handler" or "code-checker" created confusion. But when we gave agents personalities - like [Grumpy Reviewer](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/grumpy-reviewer.md) or [Poem Bot](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/poem-bot.md) - their purpose became immediately clear. Team members actually started developing relationships with specific agents. It's kind of adorable.

### Cost-Quality Tradeoffs Are Real

Longer, more thorough analyses cost more - but they're not always better. The [Portfolio Analyst](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/portfolio-analyst.md) helps us figure out which agents actually deliver value.

We discovered that some of our "thorough" agents were doing redundant work or generating reports nobody read. The Portfolio Analyst tracks cost-per-insight across all agents, revealing that simple, focused agents often deliver better ROI than complex ones. This led us to consolidate overlapping agents and tune prompt lengths to balance thoroughness with cost. AI isn't free, folks!

### Multi-Phase Workflows Enable Ambitious Goals

Breaking complex improvements into 3-phase workflows (research → setup → implement) lets agents tackle projects that would be way too large for a single run. Each phase builds on the last, with human feedback between phases.

Single-run agents hit walls fast - limited by token context and execution time. But multi-phase workflows like [Daily Test Improver](https://github.com/githubnext/agentics/blob/main/workflows/daily-test-improver.md) and [Daily Perf Improver](https://github.com/githubnext/agentics/blob/main/workflows/daily-perf-improver.md) can tackle ambitious projects by spreading work across multiple days. The research phase explores the problem, the setup phase prepares infrastructure, and the implementation phase executes changes. Human checkpoints between phases keep everything aligned with team goals.

### Slash Commands Create Natural User Interfaces

ChatOps-style `/command` triggers make agents feel like actual team members. Users can invoke powerful capabilities with simple comments, and role-gating ensures only authorized folks can trigger sensitive operations.

Instead of remembering complex webhook URLs or GitHub Actions syntax, team members just comment `/grumpy` on a PR for a critical review, or `/pr-fix` to fix failing tests. Role-gating prevents abuse while keeping the interface dead simple. This pattern works so well that most of our interactive agents use it now.

### Heartbeats Build Confidence

Frequent, lightweight validation tests (every 12 hours) catch regressions quickly. These "heartbeat" agents keep the infrastructure healthy without manual monitoring.

Instead of waiting for production failures, we deploy multiple [smoke test workflows](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/smoke-copilot.md) that continuously validate core functionality. When a smoke test fails, we know immediately which component broke. This proactive monitoring prevents cascading failures and gives us confidence that the ecosystem is actually stable.

### MCP Inspection Is Essential

As workflows start using multiple MCP servers, having agents that can validate and report on tool availability becomes critical. The [MCP Inspector](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/mcp-inspector.md) pattern prevents those cryptic "tool not available" failures.

Early on, we'd see agents fail with vague errors like "connection refused." The MCP Inspector proactively checks all MCP server configurations, validates network access, and generates status reports. This visibility transformed debugging from hours of detective work into reading a dashboard.

### Task Queuing Is Everywhere

The task queue pattern provided a simple way to queue and distribute work across multiple workflow runs. Breaking large projects into discrete tasks allowed incremental progress with clear state tracking, recording tasks as issues, discussions, or project cards.

Whether managing a backlog of refactoring work, coordinating security fixes, or distributing test creation tasks, the task queue pattern appeared repeatedly. By representing work as GitHub primitives (issues, project cards), we got built-in state management, persistence, and audit trails without building custom infrastructure.

### ML Analysis Reveals Hidden Patterns

Applying clustering and NLP to agent interactions revealed usage patterns that weren't obvious from individual runs. This meta-analysis helped identify opportunities for consolidation and optimization.

The [Prompt Clustering Analysis](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/prompt-clustering-analysis.md) and [Copilot PR NLP Analysis](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/copilot-pr-nlp-analysis.md) workflows discovered that many agents were asking similar questions or performing redundant analyses. This insight led to shared component libraries and consolidation opportunities we wouldn't have spotted through manual review.

## Challenges We've Encountered

Not everything has been smooth sailing. We've faced several challenges that taught us valuable lessons:

### Permission Creep

As agents gain capabilities, there's always temptation to grant broader permissions. We constantly audit and prune permissions to maintain least privilege.

The principle of least privilege requires ongoing vigilance. We've established a quarterly permission audit process where we review every agent's permissions against its actual behavior. This often reveals agents that got write access but only need read permissions, or agents requesting GitHub API scopes they never use.

### Debugging Complexity

When agents misbehave, tracing the root cause through multiple workflow runs and safe outputs can be challenging. We're still improving our logging and observability.

Distributed debugging across multiple agents - each generating their own logs and artifacts - is surprisingly difficult. We've improved this with structured logging, correlation IDs across related runs, and meta-agents that aggregate failure patterns. But there's definitely room for better tooling here.

### Repository Noise

Frequent agent runs create a lot of issues, PRs, and comments. We've had to implement archival strategies to keep the repository manageable.

With agents creating dozens of issues and PRs daily, the repository's signal-to-noise ratio can suffer. We've developed cleanup agents that archive old discussions, close stale issues, and consolidate redundant reports. Finding the right balance between transparency and clutter remains an ongoing challenge.

### Cost Management

Running many agents incurs significant costs. The Portfolio Analyst helps, but ongoing cost monitoring is essential.

Agentic AI operations at scale aren't free. We've had to build cost awareness into the factory's culture, with regular reviews of spend-per-agent and value-per-dollar metrics. Some expensive but low-value agents get deprecated, while high-value agents get budget increases. Cost visibility turns out to be as important as functionality.

### User Trust

Some team members are hesitant to engage with automated agents. Clear communication about capabilities and limitations helps build trust over time.

Trust isn't automatic - it's earned through consistent behavior and transparent communication. We've found that agents with clear "about me" descriptions, visible limitations, and predictable behavior patterns gain acceptance faster. Failed experiments that we openly discuss as learning opportunities also help build trust.

## Applying These Lessons

These lessons aren't just academic observations - they're practical insights you can use when building your own agent ecosystem:

1. **Start diverse, not perfect** - Launch multiple simple agents rather than one complex one
2. **Design with guardrails first** - Constraints enable safe experimentation
3. **Build meta-agents early** - You'll need them sooner than you think
4. **Give agents personality** - It helps with understanding and adoption
5. **Monitor costs from day one** - Cost awareness prevents nasty surprises
6. **Embrace multi-phase patterns** - Break ambitious projects into manageable phases
7. **Use ChatOps interfaces** - Slash commands are intuitive and role-gatable
8. **Implement heartbeats** - Proactive monitoring beats reactive debugging
9. **Inspect your tools** - Validate tool availability before agents need them
10. **Dispatch, don't monolith** - Route requests to specialized agents
11. **Queue your work** - Task queuing enables incremental progress
12. **Analyze at meta-level** - ML can reveal patterns humans miss

## What's Next?

These lessons emerged from observing agent behavior, but understanding *how* agents actually work requires diving into their fundamental design patterns.

In our next article, we'll explore the 12 core design patterns that define what agents do and how they behave. Stay tuned!

*More articles in this series coming soon.*

[Previous: Meet the Workflows](/gh-aw/blog/2026-01-13-meet-the-workflows/)
