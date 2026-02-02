---
title: "12 Design Patterns from Peli's Agent Factory"
description: "Fundamental behavioral patterns for successful agentic workflows"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-24
draft: true
prev:
  link: /gh-aw/blog/2026-01-21-twelve-lessons/
  label: 12 Lessons
next:
  link: /gh-aw/blog/2026-01-27-operational-patterns/
  label: 9 Operational Patterns
---

[Previous Article](/gh-aw/blog/2026-01-21-twelve-lessons/)

---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*My dear friends!* What a scrumptious third helping in the Peli's Agent Factory series! You've sampled the [workflows](/gh-aw/blog/2026-01-13-meet-the-workflows/) and savored the [lessons we've learned](/gh-aw/blog/2026-01-21-twelve-lessons/) - now prepare yourselves for the *secret recipes* - the fundamental design patterns that emerged from running our collection!

After building our collection of agents in Peli's Agent Factory, we started noticing patterns. Not the kind we planned upfront - these emerged organically from solving real problems. Now we've identified 12 fundamental design patterns that capture what successful agentic workflows actually do.

Think of these patterns as architectural blueprints for agents. Every workflow in the factory fits into at least one pattern, and many combine several. Understanding these patterns will help you design effective agents faster, without reinventing the wheel.

Let's dive in!

## Pattern 1: The Read-Only Analyst

**Observe, analyze, and report - without changing anything**

These agents gather data, perform analysis, and publish insights through discussions or assets. They have zero write permissions to code. This makes them incredibly safe to run continuously at any frequency.

Use these when:

- Building confidence in agent behavior (great for getting started!)
- Establishing baselines before automation
- Generating reports and metrics
- Deep research and investigation

Here are some examples:

- [`audit-workflows`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/audit-workflows.md) - Meta-agent that audits all other agents' runs
- [`portfolio-analyst`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/portfolio-analyst.md) - Spots cost optimization opportunities
- [`session-insights`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/copilot-session-insights.md) - Analyzes Copilot usage patterns
- [`org-health-report`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/org-health-report.md) - Organization-wide health metrics
- [`scout`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/scout.md), [`archie`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/archie.md) - Deep research agents

Some key characteristics are:

- `permissions: contents: read` only (that's it!)
- Output via discussions, issues, or artifact uploads
- Can run on any schedule without risk
- Builds trust through transparency
- Often creates visualizations and charts

---

## Pattern 2: The ChatOps Responder

**On-demand assistance via slash commands**

These agentic workflows areactivated by `/command` mentions in issues or PRs. Role-gated for security. They respond with analysis, visualizations, or actions.

Use these when:

- Interactive code reviews
- On-demand optimizations
- User-initiated research
- Specialized assistance requiring authorization

Here are some examples:

- [`q`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/q.md) - Workflow optimizer (type `/q` and it investigates!)
- [`grumpy-reviewer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/grumpy-reviewer.md) - Critical code review with personality
- [`poem-bot`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/poem-bot.md) - Creative verse generation (because why not?)
- [`mergefest`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/mergefest.md) - Branch merging automation
- [`pr-fix`](https://github.com/githubnext/agentics/blob/main/workflows/pr-fix.md) - Fixes failing CI checks on demand

Some key characteristics are:

- Triggered by `/command` in issue/PR comments
- Often includes role-gating for security
- Provides immediate feedback
- Uses cache-memory to avoid duplicate work
- Clear personality and purpose

---

## Pattern 3: The Continuous Janitor

**Automated cleanup and maintenance**

These agentic workflows propose incremental improvements through PRs. Run on schedules (daily/weekly). Create scoped changes with descriptive labels and commit messages. Always require human review before merging.

Use these when:

- Keeping dependencies up to date
- Maintaining documentation sync
- Formatting and style consistency
- Small refactorings and cleanups
- File organization improvements

Here are some examples:

- [`daily-workflow-updater`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/daily-workflow-updater.md) - Keeps actions and dependencies current
- [`glossary-maintainer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/glossary-maintainer.md) - Syncs glossary with codebase
- [`daily-file-diet`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/daily-file-diet.md) - Refactors oversized files
- [`hourly-ci-cleaner`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/hourly-ci-cleaner.md) - Repairs CI issues

Some key characteristics are:

- Runs on fixed schedules
- Creates PRs for human review (no auto-merge!)
- Makes small, focused changes
- Uses descriptive labels and commits
- Often includes "if no changes" guards

---

## Pattern 4: The Quality Guardian

**Continuous validation and compliance enforcement**

These agentic workflows validate system integrity through testing, scanning, and compliance checks. Run frequently (hourly/daily) to catch regressions early.

Use these when:

- Smoke testing infrastructure
- Security scanning
- Accessibility validation
- Schema consistency checks
- Infrastructure health monitoring

Here are some examples:

- Smoke tests for [`copilot`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/smoke-copilot.md), [`claude`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/smoke-claude.md), [`codex`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/smoke-codex.md)
- [`schema-consistency-checker`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/schema-consistency-checker.md)
- [`breaking-change-checker`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/breaking-change-checker.md)
- [`firewall`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/firewall.md), [`mcp-inspector`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/mcp-inspector.md)
- [`daily-accessibility-review`](https://github.com/githubnext/agentics/blob/main/workflows/daily-accessibility-review.md)

Some key characteristics are:

- Frequent execution (hourly to daily)
- Clear pass/fail criteria
- Creates issues when validation fails
- Minimal false positives
- Fast execution (heartbeat pattern)

---

## Pattern 5: The Issue & PR Manager

**Intelligent workflow automation for issues and pull requests**

These agentic workflows triage, link, label, close, and coordinate issues and PRs. React to events or run on schedules.

Use these when:

- Automating issue triage
- Linking related issues
- Managing sub-issues
- Coordinating merges
- Optimizing issue templates

Here are some examples:

- [`issue-triage-agent`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/issue-triage-agent.md) - Auto-labels and categorizes
- [`issue-arborist`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/issue-arborist.md) - Links related issues
- [`mergefest`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/mergefest.md) - Merge coordination
- [`sub-issue-closer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/sub-issue-closer.md) - Closes completed sub-issues

Some key characteristics are:

- Event-driven (issue/PR triggers)
- Uses safe outputs for modifications
- Often includes intelligent classification
- Maintains issue relationships
- Respects user intent and context

---

## Pattern 6: The Multi-Phase Improver

**Progressive work across multiple days with human checkpoints**

These agentic workflows tackle complex improvements too large for single runs. Three phases: (1) Research and create plan discussion, (2) Infer/setup build infrastructure, (3) Implement changes via PR. Check state each run to determine current phase.

Use these when:

- Large refactoring projects
- Test coverage improvements
- Performance optimization
- Backlog reduction initiatives
- Quality improvement programs

Here are some examples:

- [`daily-backlog-burner`](https://github.com/githubnext/agentics/blob/main/workflows/daily-backlog-burner.md) - Systematic backlog reduction
- [`daily-perf-improver`](https://github.com/githubnext/agentics/blob/main/workflows/daily-perf-improver.md) - Performance optimization
- [`daily-test-improver`](https://github.com/githubnext/agentics/blob/main/workflows/daily-test-improver.md) - Test coverage enhancement
- [`daily-qa`](https://github.com/githubnext/agentics/blob/main/workflows/daily-qa.md) - Continuous quality assurance

Some key characteristics are:

- Multi-day operation
- Three distinct phases with checkpoints
- Uses repo-memory for state persistence
- Human approval between phases
- Creates comprehensive documentation

---

## Pattern 7: The Code Intelligence Agent

**Semantic analysis and pattern detection**

Agents using specialized code analysis tools (Serena, ast-grep) to detect patterns, duplicates, anti-patterns, and refactoring opportunities.

Use these when:

- Finding duplicate code
- Detecting anti-patterns
- Identifying refactoring opportunities
- Analyzing code style consistency
- Type system improvements

Here are some examples:

- [`duplicate-code-detector`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/duplicate-code-detector.md) - Finds code duplicates
- [`semantic-function-refactor`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/semantic-function-refactor.md) - Refactoring opportunities
- [`terminal-stylist`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/terminal-stylist.md) - Console output analysis
- [`go-pattern-detector`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/go-pattern-detector.md) - Go-specific patterns
- [`typist`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/typist.md) - Type analysis

Some key characteristics are:

- Uses specialized analysis tools (MCP servers)
- Language-aware or cross-language
- Creates detailed issues with code locations
- Often proposes concrete fixes
- Integrates with IDE workflows

---

## Pattern 8: The Content & Documentation Agent

**Maintain knowledge artifacts synchronized with code**

These agentic workflows keep documentation, glossaries, slide decks, blog posts, and other content fresh by monitoring codebase changes and updating corresponding docs.

Use these when:

- Keeping docs synchronized
- Maintaining glossaries
- Updating slide decks
- Analyzing multimedia content
- Generating documentation

Here are some examples:

- [`glossary-maintainer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/glossary-maintainer.md) - Glossary synchronization
- [`technical-doc-writer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/technical-doc-writer.md) - Technical documentation
- [`slide-deck-maintainer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/slide-deck-maintainer.md) - Presentation maintenance
- [`ubuntu-image-analyzer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/ubuntu-image-analyzer.md) - Environment documentation

Some key characteristics are:

- Monitors code changes
- Creates documentation PRs
- Uses document analysis tools (markitdown)
- Maintains consistency
- Often includes visualization

---

## Pattern 9: The Meta-Agent Optimizer

**Monitor and optimize other agents**

These agentic workflows analyze the agent ecosystem itself. Download workflow logs, classify failures, detect missing tools, track performance metrics, identify cost optimization opportunities.

Use these when:

- Managing agentic ecosystems at scale
- Cost optimization
- Performance monitoring
- Failure pattern detection
- Tool availability validation

Here are some examples:

- [`audit-workflows`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/audit-workflows.md) - Comprehensive workflow auditing
- [`agent-performance-analyzer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/agent-performance-analyzer.md) - Agent quality metrics
- [`portfolio-analyst`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/portfolio-analyst.md) - Cost optimization
- [`workflow-health-manager`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/workflow-health-manager.md) - Health monitoring

Some key characteristics are:

- Accesses workflow run data
- Analyzes logs and metrics
- Identifies systemic issues
- Provides actionable recommendations
- Essential for scale

---

## Pattern 10: The Meta-Agent Orchestrator

**Orchestrate multi-step workflows via state machines**

These agentic workflows coordinate complex workflows through task queue patterns. Track state across runs (open/in-progress/completed).

Use these when:

- Task management
- Multi-step coordination
- Workflow generation
- Development monitoring
- Task distribution

Here are some examples:

- [`workflow-generator`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/workflow-generator.md) - Generates new workflows
- [`dev-hawk`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/dev-hawk.md) - Development monitoring

Some key characteristics are:

- Manages state across runs
- Uses GitHub primitives (issues, projects)
- Coordinates multiple agents
- Implements workflow patterns
- Often dispatcher-based

---

## Pattern 11: The ML & Analytics Agent

**Advanced insights through machine learning and NLP**

These agentic workflows apply clustering, NLP, statistical analysis, or ML techniques to extract patterns from historical data. Generate visualizations and trend reports.

Use these when:

- Pattern discovery in large datasets
- NLP on conversations
- Clustering similar items
- Trend analysis
- Longitudinal studies

Here are some examples:

- [`copilot-session-insights`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/copilot-session-insights.md) - Session usage analysis
- [`copilot-pr-nlp-analysis`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/copilot-pr-nlp-analysis.md) - NLP on PR conversations
- [`prompt-clustering`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/prompt-clustering-analysis.md) - Clusters and categorizes prompts

Some key characteristics are:

- Uses ML/statistical techniques
- Requires historical data
- Often uses repo-memory
- Generates visualizations
- Discovers non-obvious patterns

---

## Pattern 12: The Security & Moderation Agent

**Protect repositories from threats and enforce policies**

These agentic workflows guard repositories through vulnerability scanning, secret detection, spam filtering, malicious code analysis, and compliance enforcement.

Use these when:

- Security vulnerability scanning
- Secret detection
- Spam and abuse prevention
- Compliance enforcement
- Security fix generation

Here are some examples:

- [`security-compliance`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/security-compliance.md) - Vulnerability campaigns
- [`firewall`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/firewall.md) - Network security testing
- [`daily-secrets-analysis`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/daily-secrets-analysis.md) - Secret scanning
- [`ai-moderator`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/ai-moderator.md) - Comment spam filtering
- [`security-fix-pr`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/security-fix-pr.md) - Automated security fixes

Some key characteristics are:

- Security-focused permissions
- High accuracy requirements
- Often regulatory-driven
- Creates actionable alerts
- May include auto-remediation

---

## Combining Patterns

Here's where it gets fun: many successful workflows combine multiple patterns. For example:

- **Read-Only Analyst + ML Analytics** - Analyze historical data and generate insights
- **ChatOps Responder + Multi-Phase Improver** - User triggers a multi-day improvement project
- **Quality Guardian + Security Agent** - Validate both quality and security continuously
- **Meta-Agent Optimizer + Meta-Agent Orchestrator** - Monitor and coordinate the ecosystem

## Choosing the Right Pattern

When designing a new agent, ask yourself:

1. **Does it modify anything?** → If no, start with Read-Only Analyst (safest!)
2. **Is it user-triggered?** → Consider ChatOps Responder
3. **Should it run automatically?** → Choose between Janitor (PRs) or Guardian (validation)
4. **Is it managing other agents?** → Use Meta-Agent Optimizer or Orchestrator
5. **Does it need multiple phases?** → Use Multi-Phase Improver
6. **Is it security-related?** → Apply Security & Moderation pattern

## What's Next?

These design patterns describe *what* agents do behaviorally. But *how* they operate within GitHub's ecosystem - that requires understanding operational patterns.

In our next article, we'll explore 9 operational patterns for running agents effectively on GitHub. These are the strategies that make agents work in practice!

*More articles in this series coming soon.*

[Previous Article](/gh-aw/blog/2026-01-21-twelve-lessons/)
