---
title: "Authoring New Workflows in Peli's Agent Factory"
description: "A practical guide to creating effective agentic workflows"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-02-08
draft: true
prev:
  link: /gh-aw/blog/2026-02-05-how-workflows-work/
  label: How Workflows Work
---

[Previous Article](/gh-aw/blog/2026-02-05-how-workflows-work/)

---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Excellent!* You've arrived at the *invention room* - our practical guide in the Peli's Agent Factory series! Now that you've glimpsed [the magnificent machinery under the hood](/gh-aw/blog/2026-02-05-how-workflows-work/), it's time to don your inventor's cap and start creating your own confections!

Ready to build your own agentic workflows? Awesome! Let's make it happen.

Now that you understand how workflows operate under the hood, we'll walk through the practical side of creating your own. This guide shares patterns, tips, and best practices from creating our collection of automated agentic workflows in practice. Don't worry - we'll start simple and build up from there.

Let's dive in!

## The Authoring Process

Creating an effective agentic workflow breaks down into five straightforward stages:

```text
1. Define Purpose → 2. Choose Pattern → 3. Write Prompt → 4. Configure → 5. Test & Iterate
```

Let's walk through each stage with real examples from the factory.

## Stage 1: Define Purpose

Before writing any code, get crystal clear on what you're building. Answer these questions:

### What Problem Are You Solving?

Be specific! Instead of "improve code quality," try:

- "Detect duplicate code blocks over 50 lines"
- "Identify functions without test coverage"
- "Find outdated API usage patterns"

The more specific you are, the better your agent will perform.

### Who Is the Audience?

Different audiences need different approaches:

**Team members**: Conversational tone, explain recommendations  
**Maintainers**: Technical detail, include reproduction steps  
**External contributors**: Welcoming tone, provide context

Know who you're talking to!

### What Should the Output Be?

Define the artifact the agent will create:

- Issue with findings and recommendations
- PR with automated fixes
- Discussion with metrics and visualizations
- Comment with analysis results

Be clear about what success looks like.

### How Often Should It Run?

Consider the appropriate cadence:

- **Continuous** (every commit): Quality checks, smoke tests
- **Hourly**: CI monitoring, security scanning
- **Daily**: Metrics, health checks, cleanup
- **Weekly**: Deep analysis, trend reports
- **On-demand**: Reviews, investigations, fixes

Match the frequency to the need.

## Stage 2: Choose Your Pattern

Select from the [12 design patterns](03-design-patterns.md) and [9 operational patterns](04-operational-patterns.md):

### Quick Pattern Selection Guide

**For observability without changes:**
→ Read-Only Analyst + DailyOps

**For interactive assistance:**
→ ChatOps Responder + Role-gated

**For automated maintenance:**
→ Continuous Janitor + Human-in-the-Loop

**For quality enforcement:**
→ Quality Guardian + Network Restricted

**For complex improvements:**
→ Multi-Phase Improver + TaskOps

**For ecosystem management:**
→ Meta-Agent Optimizer + Meta-Agent Orchestrator

## Stage 3: Write an Effective Prompt

The prompt is the heart of your workflow. Great prompts are:

### Clear and Specific

❌ **Poor**: "Analyze the code"

✅ **Good**: "Analyze the codebase for functions longer than 100 lines. For each long function, identify:

1. Primary responsibility
2. Potential split points
3. Suggested refactoring approach"

### Structured

Use headings, lists, and examples:

```markdown
## Task

Identify test coverage gaps in the repository.

## Process

1. **Analyze test files** in `test/` directory
2. **Map test coverage** to source files in `src/`
3. **Identify gaps** where source files lack corresponding tests
4. **Prioritize** based on file complexity and importance

## Output

Create an issue titled "Test Coverage Gaps - [Date]" with:

- Summary of overall coverage
- List of untested files (highest priority first)
- Suggested test cases for top 3 gaps
- Link to coverage report

## Example

    ### Untested Files
    
    1. **src/payment.js** (Critical - handles transactions)
       - Test cases needed:
         - Success path with valid payment
         - Failure path with invalid card
         - Edge case: $0.00 transaction
```

### Contextual

Provide relevant background:

```markdown
## Context

This repository uses Jest for unit tests and Playwright for integration tests.
Tests are co-located with source files as `*.test.js`.

Our coverage goal is 80% for critical paths (payment, auth, user management).

## Conventions

- One test file per source file
- Test names follow pattern: "should [expected behavior] when [condition]"
- Use mocks from `test/mocks/` directory
```

### Personality-Driven

Give your agent character:

**The Meticulous Auditor**: "Review every configuration file with extreme attention to detail. Note even minor inconsistencies."

**The Helpful Janitor**: "Propose small, incremental improvements. Make the codebase a little better each day."

**The Creative Poet**: "Compose verses that capture the repository's essence and team achievements."

**The Critical Reviewer**: "Challenge assumptions. Ask hard questions. Push for excellence."

### Tool-Aware

Reference available tools explicitly:

```markdown
## Tools Available

- **Serena MCP**: Use for semantic code analysis
- **GitHub API**: Query issues, PRs, workflow runs
- **jq**: Process JSON data from API responses
- **Python + matplotlib**: Generate visualizations

## Example Usage

Use Serena to find duplicate code:
```bash
serena find-duplicates --min-lines 30 --similarity 0.8
```

## Stage 4: Configure Effectively

### Frontmatter Best Practices

#### Start Minimal, Add as Needed

```yaml
---
description: Daily test coverage report
on:
  schedule: "0 9 * * 1-5"  # Weekdays at 9am
permissions:
  contents: read  # Start with read-only
---
```

Add write permissions only when safe outputs require them.

#### Use Imports for Common Needs

```yaml
---
description: Repository health metrics
imports:
  - shared/reporting.md       # Report formatting
  - shared/python-dataviz.md  # Visualization
  - shared/jqschema.md        # JSON processing
on:
  schedule: "0 0 * * 0"  # Weekly on Sunday
---
```

#### Configure Safe Outputs with Guardrails

```yaml
safe_outputs:
  create_issue:
    title_prefix: "[Health Check]"
    labels: ["automated", "health"]
    max_items: 3         # Limit to 3 issues
    close_older: true    # Close old duplicates
    expire: "+14d"       # Auto-close after 2 weeks
    if_no_changes: skip  # Don't create if unchanged
```

#### Allowlist Tools Explicitly

```yaml
tools:
  github:
    toolsets: [repos, issues]  # Only what you need
  bash:
    commands: [git, jq, python3]  # Explicit list
```

#### Restrict Network Access

```yaml
network:
  allowed:
    - "api.github.com"      # GitHub API
    - "api.tavily.com"      # Web search
    # No wildcards in production
```

## Stage 5: Test and Iterate

### Local Testing

Before deploying, test locally:

```bash
# Validate syntax
gh aw compile workflow.md

# Check for issues
gh aw validate workflow.md

# Test compilation
gh aw compile --output test.lock.yml workflow.md
```

### Staged Rollout

#### Phase 1: Manual Trigger

Start with manual workflow dispatch:

```yaml
on:
  workflow_dispatch:
```

Run a few times manually, review outputs, iterate on prompt.

#### Phase 2: Limited Schedule

Add a schedule with time limit:

```yaml
on:
  workflow_dispatch:
  schedule: "0 9 * * 1"  # Once a week
stop-after: "+1mo"       # Auto-expire in 1 month
```

Monitor for a month, adjust based on feedback.

#### Phase 3: Production

Remove time limit, adjust schedule as needed:

```yaml
on:
  workflow_dispatch:
  schedule: "0 9 * * 1-5"  # Weekdays
```

### Iteration Checklist

After each run, review:

- [ ] Did the agent understand the task correctly?
- [ ] Were the outputs useful and actionable?
- [ ] Did it respect all guardrails (max_items, etc.)?
- [ ] Were there any security concerns?
- [ ] Did it run within expected time/cost?
- [ ] What could be improved in the next version?

## Common Patterns from the Factory

### Pattern: The Weekly Analyst

```markdown
---
description: Weekly repository health report
imports:
  - shared/reporting.md
  - shared/python-dataviz.md
on:
  schedule: "0 9 * * 0"  # Sunday morning
permissions:
  contents: read
safe_outputs:
  create_discussion:
    title: "Weekly Health Report - {date}"
    category: "Reports"
---

## Weekly Repository Health Analysis

Create a comprehensive health report:

1. **Issue Metrics**: Open, closed, average resolution time
2. **PR Metrics**: Open, merged, average review time
3. **CI Health**: Success rate, failure patterns
4. **Security**: Open vulnerabilities, compliance status
5. **Trends**: Week-over-week comparisons

Include:
- Summary dashboard
- Key insights and recommendations
- Visualizations (charts, graphs)
- Links to detailed data
```

### Pattern: The ChatOps Responder

```markdown
---
description: On-demand code review
on:
  issue_comment:
    types: [created]
permissions:
  contents: read
  pull-requests: write
safe_outputs:
  add_comment:
    prefix: "## Review Complete\n\n"
---

## Grumpy Code Reviewer

When a user comments `/grumpy` on a PR:

1. Verify user has collaborator access
2. Download PR diff
3. Perform critical code review:
   - Architecture issues
   - Performance concerns
   - Security vulnerabilities
   - Style violations
4. Post review as comment

Use a critical, direct tone. Be thorough but constructive.
Never approve - always find room for improvement.
```

### Pattern: The Multi-Phase Improver

```markdown
---
description: Systematic test improvement
imports:
  - shared/reporting.md
on:
  schedule: "0 10 * * 1-5"  # Weekdays
permissions:
  contents: read
  issues: write
  pull-requests: write
repo-memory:
  - id: test-improver
    create-orphan: true
safe_outputs:
  create_discussion:
    category: "Plans"
  create_issue:
    labels: ["testing", "improvement"]
  create_pull_request:
    labels: ["testing", "automated"]
---

## Daily Test Improver

Systematically improve test coverage over multiple days:

### Phase 1: Research (Days 1-2)
- Analyze current test coverage
- Identify coverage gaps
- Prioritize files by importance
- Create discussion with findings

### Phase 2: Plan (Days 3-4)
- Design test strategies for top gaps
- Create issues for implementation
- Break down into manageable tasks
- Get human approval on plan

### Phase 3: Implement (Days 5+)
- Implement tests from approved issues
- Create PRs with new tests
- One PR per day for easy review
- Track progress in repo-memory

Check repo-memory/test-improver/ for current phase.
```

## Advanced Techniques

### Using Repo-Memory

Store state across runs:

```yaml
repo-memory:
  - id: daily-metrics
    max-file-size: 1MB
    max-files: 100
```

In your prompt:

```markdown
Check `/tmp/gh-aw/repo-memory/daily-metrics/` for historical data.
Compare today's metrics with last week's baseline.
Store today's results as `metrics-{date}.json`.
```

### Cache-Memory for ChatOps

Prevent duplicate work:

```markdown
Before analyzing, check cache-memory/{issue-number}.json.
If analysis exists and issue hasn't changed, skip analysis.
Store new analysis results for future reference.
```

### Conditional Execution

```markdown
## Pre-flight Checks

1. Check if CI is currently failing - if not, exit early
2. Check if diagnostic issue already exists - if so, update it
3. Check if this is a known transient failure - if so, just log it
```

## Common Mistakes to Avoid

### Mistake 1: Vague Prompts

❌ "Look at the code and tell me what's wrong"

✅ "Analyze functions in src/ for complexity. Flag any function with cyclomatic complexity > 15. For each, suggest refactoring approach."

### Mistake 2: Missing Constraints

❌ No max_items, creates 50 duplicate issues

✅ `max_items: 3`, `close_older: true`, `expire: "+7d"`

### Mistake 3: Too Many Permissions

❌ `permissions: write-all`

✅ Start with `contents: read`, add specific permissions as needed

### Mistake 4: No Testing

❌ Deploy directly to production schedule

✅ Test manually first, then limited schedule with expiration

### Mistake 5: Overly Complex

❌ One workflow that does 10 different things

✅ Multiple focused workflows, each doing one thing well

## Resources and Examples

### Example Workflows to Study

Browse the factory for inspiration:

**Simple Examples:**

- [`poem-bot`](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/poem-bot.md) - ChatOps personality
- [`daily-repo-chronicle`](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-repo-chronicle.md) - Daily summary
- [`issue-triage-agent`](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/issue-triage-agent.md) - Event-driven

**Intermediate Examples:**

- [`ci-doctor`](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/ci-doctor.md) - Diagnostic analysis
- [`glossary-maintainer`](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/glossary-maintainer.md) - Content sync
- [`terminal-stylist`](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/terminal-stylist.md) - Code analysis

**Advanced Examples:**

- [`daily-test-improver`](https://github.com/githubnext/agentics/blob/main/workflows/daily-test-improver.md) - Multi-phase
- [`agent-performance-analyzer`](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/agent-performance-analyzer.md) - Meta-agent
- [`workflow-health-manager`](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/workflow-health-manager.md) - Orchestration

### Documentation

- [Workflow Reference](https://github.github.com/gh-aw/reference/workflows/)
- [Safe Outputs Guide](https://github.github.com/gh-aw/reference/safe-outputs/)
- [Tools Reference](https://github.github.com/gh-aw/reference/tools/)
- [Examples Gallery](https://github.github.com/gh-aw/examples/)

### Community

- Share workflows in [Agentics Collection](https://github.com/githubnext/agentics)
- Get help in [GitHub Next Discord](https://gh.io/next-discord) #continuous-ai
- Browse [discussions](https://github.com/github/gh-aw/discussions)

## Your Turn

You now have everything you need to author effective agentic workflows:

1. **Start simple** - Pick one repetitive task
2. **Follow patterns** - Use proven designs
3. **Test iteratively** - Manual → Limited → Production
4. **Share learnings** - Contribute to the community

Ready to begin? The next article will walk you through getting your first workflow running.

## What's Next?

_More articles in this series coming soon._

[Previous Article](/gh-aw/blog/2026-02-05-how-workflows-work/)
