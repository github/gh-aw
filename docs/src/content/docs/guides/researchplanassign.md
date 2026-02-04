---
title: ResearchPlanAssign Strategy
description: Scaffold AI-powered code improvements with research agents, planning agents, and copilot execution while keeping developers in control
---

The ResearchPlanAssign strategy is a scaffolded approach to using AI agents for systematic code improvements. This strategy keeps developers in the driver's seat by providing clear decision points at each phase while leveraging AI agents to handle the heavy lifting of research, planning, and implementation.

## How ResearchPlanAssign Works

The strategy follows three distinct phases:

### Phase 1: Research

A research agent (typically scheduled daily or weekly) investigates the repository under a specific angle and generates a comprehensive report. Using advanced Model Context Protocol (MCP) tools for deep analysis (static analysis, logging data, semantic search), it examines the codebase from a specific perspective and creates a detailed discussion or issue with findings, recommendations, and supporting data. Cache memory maintains historical context to track trends over time.

### Phase 2: Plan

The developer reviews the research report to determine if worthwhile improvements were identified. If the findings merit action, the developer invokes a planner agent to convert the research into specific, actionable issues. The planner splits complex work into smaller, focused tasks optimized for copilot agent success, formatting each issue with clear objectives, file paths, acceptance criteria, and implementation guidance.

### Phase 3: Assign

The developer reviews the generated issues and decides which ones to execute. Approved issues are [assigned to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot) for automated implementation and can be executed sequentially or in parallel depending on dependencies. Copilot creates a pull request with the implementation for developer review and merging.

## When to Use ResearchPlanAssign

Use this strategy when code improvements require systematic investigation before action, work needs to be broken down for optimal AI agent execution, or when research findings may vary in priority and require developer oversight at each phase.

## Example Implementations

The following workflows demonstrate the ResearchPlanAssign pattern in practice:

### Static Analysis → Plan → Fix

**Research Phase**: [`static-analysis-report.md`](https://github.com/github/gh-aw/blob/main/.github/workflows/static-analysis-report.md)

Runs daily to scan all agentic workflows with security tools (zizmor, poutine, actionlint), creating a comprehensive security discussion with clustered findings by tool and issue type, severity assessment, fix prompts, and historical trends.

**Plan Phase**: Developer reviews the security discussion and uses the `/plan` command to convert high-priority findings into issues.

**Assign Phase**: Developer [assigns generated issues to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot) for automated fixes.

### Duplicate Code Detection → Plan → Refactor

**Research Phase**: [`duplicate-code-detector.md`](https://github.com/github/gh-aw/blob/main/.github/workflows/duplicate-code-detector.md)

Runs daily using Serena MCP for semantic code analysis to identify exact, structural, and functional duplication. Creates one issue per distinct pattern (max 3 per run) that are [assigned to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot) (via `assignees: copilot` in workflow config) since duplication fixes are typically straightforward.

**Plan Phase**: Since issues are already well-scoped, the plan phase is implicit in the research output.

**Assign Phase**: Issues are created and [assigned to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot) (via `assignees: copilot`) for automated refactoring.

### File Size Analysis → Plan → Refactor

**Research Phase**: [`daily-file-diet.md`](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-file-diet.md)

Runs weekdays to monitor file sizes, identify files exceeding healthy size thresholds (1000+ lines), and analyze file structure to identify natural split boundaries. Creates a detailed refactoring issue with a suggested approach and file organization recommendations.

**Plan Phase**: The research issue already contains a concrete refactoring plan.

**Assign Phase**: Developer reviews and [assigns to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot) or handles manually depending on complexity.

### Deep Research → Plan → Implementation

**Research Phase**: [`scout.md`](https://github.com/github/gh-aw/blob/main/.github/workflows/scout.md)

Performs deep research investigations using multiple research MCPs (Tavily, arXiv, DeepWiki) to gather information from diverse sources. Creates a structured research summary with recommendations posted as a comment on the triggering issue.

**Plan Phase**: Developer uses `/plan` command on the research comment to convert recommendations into issues.

**Assign Phase**: Developer assigns resulting issues to appropriate agents or team members.

## Best Practices

**Research Agent Design**: Schedule appropriately (daily for critical metrics, weekly for comprehensive analysis). Use cache memory to store historical data and identify trends. Focus each research agent on one specific angle or concern, ensure reports lead to concrete recommendations, and only create reports when findings exceed meaningful thresholds.

**Planning Phase**: Review carefully—not all research findings require immediate action. Prioritize high-impact issues first, right-size tasks for AI agent execution with unambiguous success criteria, and reference the parent research report for full context.

**Assignment Phase**: Consider dependencies when assigning multiple issues sequentially or in parallel. Recognize that some tasks are better suited for human developers. Always review AI-generated code before merging and refine prompts based on agent performance.

## Customization

Adapt the ResearchPlanAssign strategy by customizing the research focus (static analysis, performance metrics, documentation quality, security, code duplication, test coverage), frequency (daily, weekly, on-demand), report format (discussions vs issues), planning approach (automatic vs manual), and assignment method (pre-assign via `assignees: copilot` in workflow config, [manual assignment through GitHub UI](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot), or mixed).

## Benefits

The ResearchPlanAssign strategy provides developer control through clear decision points, systematic improvement via regular focused analysis, optimal task sizing for AI agents, historical context tracking through cache memory, and reduced overhead by automating research and execution while developers focus on decisions.

## Limitations

The three-phase approach takes longer than direct execution and requires developers to review research reports and generated issues. Research agents may flag issues that don't require action (false positives), and multiple phases require workflow coordination and clear handoffs. Research agents often need specialized MCPs (Serena, Tavily, etc.).

## Related Strategies

- **[Orchestration](/gh-aw/guides/orchestration/)**: Coordinate multiple ResearchPlanAssign cycles toward a shared goal
- **[Threat Detection](/gh-aw/reference/threat-detection/)**: Continuous monitoring without planning phase
- **[Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/)**: Create custom actions for plan phase

> [!NOTE]
> The ResearchPlanAssign strategy works best when research findings vary in relevance and priority. For issues that always require immediate action, consider using direct execution workflows instead.
