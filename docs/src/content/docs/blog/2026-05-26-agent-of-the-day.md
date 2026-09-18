---
title: "Agent of the Day – May 26, 2026"
description: "Copilot Agent PR Analysis: a daily workflow that monitors GitHub Copilot coding agent performance across pull requests"
authors:
  - copilot
date: 2026-05-26
metadata:
  seoDescription: "Daily GitHub Actions workflow analyzing Copilot coding agent PR performance. Tracks merge rates, review times, and patterns with Claude Opus 4.7 for ~$1.53/run."
  linkedPostText: "How we track Copilot agent PR performance with a daily analysis workflow"
---

> [!NOTE]

Every morning someone at GitHub opens their laptop and wonders: how well did the coding agents do yesterday? Did they ship? Did they stall? Did they create more work than they saved? These questions used to require manual spelunking through dashboards, cross-referencing merged PRs with author names, and guessing at patterns from vibes alone.

Not anymore.

## 🤖 Agent of the Day: Copilot Agent PR Analysis

The Copilot Agent PR Analysis workflow runs daily at 6pm UTC with a single mandate: understand how GitHub's own coding agents are performing in the wild. It watches `copilot-swe-agent`-authored pull requests, tracks their lifecycle from open to merge (or close), and surfaces patterns that would otherwise vanish into the noise of a busy repository.

[Run 26415065259](https://github.com/github/gh-aw/actions/runs/26415065259) on May 25th tells the story. Six minutes. Nineteen agent turns. Nearly a million tokens processed. And at the end, a GitHub Discussion summarizing everything the agents accomplished in the last 24 hours—merge rates, review turnaround, file change distributions, the works.

![Workflow activity chart](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

What makes this run interesting isn't just the output—it's the mechanics underneath. The workflow starts by reading pre-fetched PR data from `/tmp/gh-aw/agent/pr-data/copilot-prs.json`, a file populated by an earlier step that batches GitHub API calls. This matters because API rate limits are a real constraint when you're analyzing dozens of PRs daily. By front-loading the data fetch, the Claude Opus 4.7 model can focus on *analysis* rather than pagination logistics.

From there, the agent orchestrates across 16 different tool types. `github-list_pull_requests` and `github-search_pull_requests` pull in the raw data. `github-get_file_contents` adds context when the agent needs to understand what a PR actually changed. `push_repo_memory` persists metrics for trend analysis—because spotting a single bad day matters less than spotting a three-week decline. And `create_discussion` posts the findings where the team can actually see them.

The token economics tell their own story. Of the 947,148 tokens consumed, over 3 million effective tokens came from cache reads—a 63% hit rate. That's not an accident. The workflow's prompt structure and tool imports are designed to maximize cache reuse across runs. At $1.53 per execution, this is the kind of analysis that would cost ten times more if you rebuilt context from scratch each day.

Nineteen turns might sound like a lot, but the average inter-turn time of 19.8 seconds reveals something important: this agent is *thinking*, not thrashing. It's making deliberate tool calls, waiting for responses, incorporating results, and planning next steps. The turn count reflects adaptive planning—the kind of reasoning that adjusts when it finds fewer PRs than expected or more activity in an unexpected repository corner.

[PR #34947](https://github.com/github/gh-aw/pull/34947), merged just one day after this run, shows the feedback loop in action. Titled "Normalize `copilot-session-insights` discussion output hierarchy and disclosure," it refined how the analysis gets presented—making the daily summaries easier to scan and the trend data more accessible. The workflow's own output informed improvements to the workflow itself.

This is what continuous observability looks like for AI systems. Traditional software gets monitored with APM tools, error rates, and latency percentiles. But when your "software" is an autonomous agent making judgment calls about code, you need a different kind of visibility. You need to know: are the agents getting better at writing tests? Are they over-indexing on certain file types? Are their PRs sitting in review limbo, or are humans accepting them quickly?

The Copilot Agent PR Analysis workflow answers these questions daily, automatically, without anyone remembering to ask.

---

**Curious about building workflows that watch your workflows?** Explore the full gh-aw project at [github/gh-aw](https://github.com/github/gh-aw)—where agentic automation meets operational insight.
