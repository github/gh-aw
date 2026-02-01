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

These agents gather data, perform analysis, and publish insights through discussions or assets with zero write permissions to code. Safe to run continuously at any frequency.

**Best for:** Building confidence in agent behavior, establishing baselines, generating reports, and deep research.

**Examples:** [`audit-workflows`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/audit-workflows.md), [`portfolio-analyst`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/portfolio-analyst.md), [`session-insights`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/copilot-session-insights.md), [`org-health-report`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/org-health-report.md), [`scout`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/scout.md), [`archie`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/archie.md)

**Key characteristics:** `permissions: contents: read` only, output via discussions/issues/artifacts, can run on any schedule without risk.

---

## Pattern 2: The ChatOps Responder

**On-demand assistance via slash commands**

Activated by `/command` mentions in issues or PRs. Role-gated for security. Respond with analysis, visualizations, or actions.

**Best for:** Interactive code reviews, on-demand optimizations, user-initiated research, and specialized assistance requiring authorization.

**Examples:** [`q`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/q.md), [`grumpy-reviewer`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/grumpy-reviewer.md), [`poem-bot`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/poem-bot.md), [`mergefest`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/mergefest.md), [`pr-fix`](https://github.com/githubnext/agentics/blob/main/workflows/pr-fix.md)

**Key characteristics:** Triggered by `/command` in comments, often includes role-gating, provides immediate feedback.

---

## Pattern 3: The Continuous Janitor

**Automated cleanup and maintenance**

Propose incremental improvements through PRs on schedules (daily/weekly). Create scoped changes with descriptive labels and commit messages. Always require human review before merging.

**Best for:** Dependency updates, documentation sync, formatting consistency, small refactorings, and file organization.

**Examples:** [`daily-workflow-updater`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/daily-workflow-updater.md), [`glossary-maintainer`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/glossary-maintainer.md), [`daily-file-diet`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/daily-file-diet.md), [`hourly-ci-cleaner`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/hourly-ci-cleaner.md)

**Key characteristics:** Runs on fixed schedules, creates PRs for human review (no auto-merge), makes small focused changes.

---

## Pattern 4: The Quality Guardian

**Continuous validation and compliance enforcement**

Validate system integrity through testing, scanning, and compliance checks. Run frequently (hourly/daily) to catch regressions early.

**Best for:** Smoke testing, security scanning, accessibility validation, schema consistency, and infrastructure health monitoring.

**Examples:** [`smoke-copilot`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/smoke-copilot.md), [`smoke-claude`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/smoke-claude.md), [`schema-consistency-checker`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/schema-consistency-checker.md), [`breaking-change-checker`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/breaking-change-checker.md), [`firewall`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/firewall.md), [`daily-accessibility-review`](https://github.com/githubnext/agentics/blob/main/workflows/daily-accessibility-review.md)

**Key characteristics:** Frequent execution (hourly/daily), clear pass/fail criteria, creates issues when validation fails.

---

## Pattern 5: The Issue & PR Manager

**Intelligent workflow automation for issues and pull requests**

Triage, link, label, close, and coordinate issues and PRs. React to events or run on schedules.

**Best for:** Issue triage, linking related issues, managing sub-issues, coordinating merges, and optimizing templates.

**Examples:** [`issue-triage-agent`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/issue-triage-agent.md), [`issue-arborist`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/issue-arborist.md), [`mergefest`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/mergefest.md), [`sub-issue-closer`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/sub-issue-closer.md)

**Key characteristics:** Event-driven (issue/PR triggers), uses safe outputs, includes intelligent classification.

---

## Pattern 6: The Multi-Phase Improver

**Progressive work across multiple days with human checkpoints**

Tackle complex improvements too large for single runs through three phases: (1) Research and create plan discussion, (2) Infer/setup build infrastructure, (3) Implement changes via PR. Check state each run to determine current phase.

**Best for:** Large refactorings, test coverage improvements, performance optimization, backlog reduction, and quality improvement programs.

**Examples:** [`daily-backlog-burner`](https://github.com/githubnext/agentics/blob/main/workflows/daily-backlog-burner.md), [`daily-perf-improver`](https://github.com/githubnext/agentics/blob/main/workflows/daily-perf-improver.md), [`daily-test-improver`](https://github.com/githubnext/agentics/blob/main/workflows/daily-test-improver.md), [`daily-qa`](https://github.com/githubnext/agentics/blob/main/workflows/daily-qa.md)

**Key characteristics:** Multi-day operation, three distinct phases with checkpoints, uses repo-memory for state persistence.

---

## Pattern 7: The Code Intelligence Agent

**Semantic analysis and pattern detection**

Use specialized code analysis tools (Serena, ast-grep) to detect patterns, duplicates, anti-patterns, and refactoring opportunities.

**Best for:** Finding duplicate code, detecting anti-patterns, identifying refactoring opportunities, analyzing style consistency, and type system improvements.

**Examples:** [`duplicate-code-detector`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/duplicate-code-detector.md), [`semantic-function-refactor`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/semantic-function-refactor.md), [`terminal-stylist`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/terminal-stylist.md), [`go-pattern-detector`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/go-pattern-detector.md), [`typist`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/typist.md)

**Key characteristics:** Uses specialized analysis tools (MCP servers), language-aware, creates detailed issues with code locations.

---

## Pattern 8: The Content & Documentation Agent

**Maintain knowledge artifacts synchronized with code**

Keep documentation, glossaries, slide decks, blog posts, and other content fresh by monitoring codebase changes and updating corresponding docs.

**Best for:** Keeping docs synchronized, maintaining glossaries, updating slide decks, analyzing multimedia content, and generating documentation.

**Examples:** [`glossary-maintainer`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/glossary-maintainer.md), [`technical-doc-writer`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/technical-doc-writer.md), [`slide-deck-maintainer`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/slide-deck-maintainer.md), [`ubuntu-image-analyzer`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/ubuntu-image-analyzer.md)

**Key characteristics:** Monitors code changes, creates documentation PRs, uses document analysis tools.

---

## Pattern 9: The Meta-Agent Optimizer

**Monitor and optimize other agents**

Analyze the agent ecosystem by downloading workflow logs, classifying failures, detecting missing tools, tracking performance metrics, and identifying cost optimization opportunities.

**Best for:** Managing ecosystems at scale, cost optimization, performance monitoring, failure pattern detection, and tool availability validation.

**Examples:** [`audit-workflows`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/audit-workflows.md), [`agent-performance-analyzer`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/agent-performance-analyzer.md), [`portfolio-analyst`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/portfolio-analyst.md), [`workflow-health-manager`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/workflow-health-manager.md)

**Key characteristics:** Accesses workflow run data, analyzes logs and metrics, provides actionable recommendations.

---

## Pattern 10: The Meta-Agent Orchestrator

**Orchestrate multi-step workflows via state machines**

Coordinate complex workflows through task queue patterns. Track state across runs (open/in-progress/completed).

**Best for:** Task management, multi-step coordination, workflow generation, development monitoring, and task distribution.

**Examples:** [`workflow-generator`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/workflow-generator.md), [`dev-hawk`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/dev-hawk.md)

**Key characteristics:** Manages state across runs, uses GitHub primitives (issues, projects), coordinates multiple agents.

---

## Pattern 11: The ML & Analytics Agent

**Advanced insights through machine learning and NLP**

Apply clustering, NLP, statistical analysis, or ML techniques to extract patterns from historical data. Generate visualizations and trend reports.

**Best for:** Pattern discovery in large datasets, NLP on conversations, clustering similar items, trend analysis, and longitudinal studies.

**Examples:** [`copilot-session-insights`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/copilot-session-insights.md), [`copilot-pr-nlp-analysis`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/copilot-pr-nlp-analysis.md), [`prompt-clustering`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/prompt-clustering-analysis.md)

**Key characteristics:** Uses ML/statistical techniques, requires historical data, generates visualizations.

---

## Pattern 12: The Security & Moderation Agent

**Protect repositories from threats and enforce policies**

Guard repositories through vulnerability scanning, secret detection, spam filtering, malicious code analysis, and compliance enforcement.

**Best for:** Security vulnerability scanning, secret detection, spam and abuse prevention, compliance enforcement, and security fix generation.

**Examples:** [`security-compliance`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/security-compliance.md), [`firewall`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/firewall.md), [`daily-secrets-analysis`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/daily-secrets-analysis.md), [`ai-moderator`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/ai-moderator.md), [`security-fix-pr`](https://github.com/githubnext/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/security-fix-pr.md)

**Key characteristics:** Security-focused permissions, high accuracy requirements, creates actionable alerts.

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
