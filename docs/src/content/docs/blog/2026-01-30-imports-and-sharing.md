---
title: "Imports & Sharing: Peli's Secret Weapon"
description: "How modular, reusable components enabled scaling our agent collection"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-30
draft: true
prev:
  link: /gh-aw/blog/2026-01-27-operational-patterns/
  label: 9 Operational Patterns
next:
  link: /gh-aw/blog/2026-02-02-security-lessons/
  label: Security Lessons
---

[Previous Article](/gh-aw/blog/2026-01-27-operational-patterns/)

---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Come with me, and you'll see* another installment in our Peli's Agent Factory series! We've already toured the [workflows](/gh-aw/blog/2026-01-13-meet-the-workflows/), learned our [lessons](/gh-aw/blog/2026-01-21-twelve-lessons/), discovered the [secret recipes](/gh-aw/blog/2026-01-24-design-patterns/), and explored [operational patterns](/gh-aw/blog/2026-01-27-operational-patterns/). Today, I shall reveal the *everlasting gobstopper* - the secret weapon that made scaling possible: imports!

Here's the truth: tending dozens of agents would be completely unsustainable without reuse. One of the most powerful features that let us scale Peli's Agent Factory is the **imports system** - a mechanism for sharing and reusing workflow components across the entire factory.

Instead of duplicating configuration, tool setup, and instructions in every single workflow, we created a library of shared components that agents can import on-demand. This isn't just about being DRY (though that's nice) - it's carefully designed to support modularization, sharing, installation, pinning, and versioning of single-file portions of agentic workflows.

Let's dive in!

## The Power of Imports

Imports provide several game-changing benefits that transformed how we develop and maintain the factory:

### 🔄 DRY Principle for Agentic Workflows

When we improve report formatting or update an MCP server configuration, the change automatically propagates to all workflows that import it. No need to update 46 workflows individually. That's huge!

For example, when we enhanced the [`reporting.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/reporting.md) component with better formatting guidelines, all 46 workflows that imported it immediately benefited. One change, 46 workflows improved. Magic.

### 🧩 Composable Workflow Capabilities

Workflows can mix and match capabilities by importing different shared components - like combining data visualization, trending analysis, and web search in a single import list.

A typical analytical workflow might import:

- `reporting.md` for report formatting
- `python-dataviz.md` for visualization capabilities
- `jqschema.md` for JSON processing
- `mcp/tavily.md` for web search

Each import adds a specific capability, and workflows compose exactly what they need. It's like LEGO blocks for agents!

### 🎯 Separation of Concerns

Tools configuration, network permissions, data fetching logic, and agent instructions can be maintained independently by different experts, then composed together.

This allows specialization:

- Infrastructure team manages MCP server configurations
- Security team maintains network policies
- Data team builds visualization components
- Agent authors focus on prompts and logic

Everyone works on what they know best, and it all comes together seamlessly.

### ⚡ Rapid Experimentation

Creating a new workflow often means writing just the agent-specific prompt and importing 3-5 shared components. We can prototype new agents in minutes instead of hours.

Example minimal workflow:

```markdown
---
description: Analyze code patterns
imports:
  - shared/reporting.md
  - shared/mcp/serena.md
  - shared/jqschema.md
---

Analyze the codebase for common patterns...
```

That's it! Three imports give you reporting, code analysis, and JSON processing. You just focus on writing the prompt.

## The Import Library

The factory organized shared components into two main directories:

### Core Capabilities: `.github/workflows/shared/`

35+ components providing fundamental capabilities:

#### Most Popular Shared Components

**[`reporting.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/reporting.md?plain=1)** (46 imports)

- Report formatting guidelines
- Workflow run references
- Footer standards
- Consistent structure

**[`jqschema.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/jqschema.md?plain=1)** (17 imports)

- JSON querying utilities
- Schema validation
- Data transformation patterns

**[`python-dataviz.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/python-dataviz.md?plain=1)** (7 imports)

- Python environment with NumPy, Pandas, Matplotlib, Seaborn
- Data visualization templates
- Chart generation utilities

**[`trending-charts-simple.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/trending-charts-simple.md?plain=1)** (6 imports)

- Quick setup for trend visualizations
- Time-series analysis
- Comparison charts

**[`gh.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/gh.md?plain=1)** (4 imports)

- Safe-input wrapper for GitHub CLI
- Authentication handling
- Common gh commands

**[`copilot-pr-data-fetch.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/copilot-pr-data-fetch.md?plain=1)** (4 imports)

- Fetch GitHub Copilot PR data
- Cache management
- Data preprocessing

#### Specialized Components

**Data Analysis**

- [`charts-with-trending.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/charts-with-trending.md) - Comprehensive trending with cache-memory
- [`ci-data-analysis.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/ci-data-analysis.md) - CI workflow analysis
- [`session-analysis-charts.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/session-analysis-charts.md) - Copilot session visualization

**Prompting & Output**

- [`keep-it-short.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/keep-it-short.md) - Guidance for concise responses
- [`safe-output-app.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/safe-output-app.md) - Safe output patterns

### MCP Server Configurations: `.github/workflows/shared/mcp/`

20+ MCP server configurations for specialized capabilities:

#### Most Used MCP Servers

**[`gh-aw.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/gh-aw.md?plain=1)** (12 imports)

- GitHub Agentic Workflows MCP server
- `logs` command for workflow debugging
- Workflow metadata access

**[`tavily.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/tavily.md?plain=1)** (5 imports)

- Web search via Tavily API
- Research capabilities
- Current information access

**[`markitdown.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/markitdown.md?plain=1)** (3 imports)

- Document conversion (PDF, Office, images to Markdown)
- Content extraction
- Multimedia analysis

**[`ast-grep.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/ast-grep.md?plain=1)** (2 imports)

- Structural code search and analysis
- Pattern matching
- Refactoring support

**[`brave.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/brave.md?plain=1)** (2 imports)

- Alternative web search via Brave API
- Privacy-focused search
- Diverse search results

#### Infrastructure & Analysis

**Development Tools**

- [`jupyter.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/jupyter.md) - Jupyter notebook environment with Docker services
- [`skillz.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/skillz.md) - Dynamic skill loading from `.github/skills/`
- [`serena.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/serena.md) - Semantic code analysis

**Knowledge & Search**

- [`context7.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/context7.md) - Context-aware search
- [`deepwiki.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/deepwiki.md) - Wikipedia deep search
- [`microsoft-docs.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/microsoft-docs.md) - Microsoft documentation
- [`arxiv.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/arxiv.md) - Academic paper research

**External Integrations**

- [`slack.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/slack.md) - Slack workspace integration
- [`notion.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/notion.md) - Notion workspace integration
- [`sentry.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/sentry.md) - Error tracking
- [`datadog.md`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/shared/mcp/datadog.md) - Observability platform

## Import Statistics

The factory's extensive use of imports demonstrates their value:

- **84 workflows** (65% of factory) use the imports feature
- **46 workflows** import `reporting.md` (most popular component)
- **17 workflows** import `jqschema.md` (JSON utilities)
- **12 workflows** import `mcp/gh-aw.md` (meta-analysis server)
- **35+ shared components** in `.github/workflows/shared/`
- **20+ MCP server configs** in `.github/workflows/shared/mcp/`
- **Average 2-3 imports** per workflow (some have 8+!)

## How Imports Work

### Basic Import Syntax

```markdown
---
description: My workflow
imports:
  - shared/reporting.md
  - shared/mcp/tavily.md
---

Your workflow prompt here...
```

### What Gets Imported

When a workflow imports a shared component, several things are merged:

1. **Frontmatter** - Tools, permissions, network settings
2. **Instructions** - Prompt guidance and context
3. **MCP Servers** - Tool configurations
4. **Safe Outputs** - Output templates

### Import Resolution

Imports are resolved at compile time:

1. Parse workflow frontmatter
2. Load each imported file
3. Merge configurations (workflow overrides imports)
4. Compile to final YAML

### Versioning & Pinning

Imports can be pinned to specific commits:

```markdown
imports:
  - shared/reporting.md@abc123
  - shared/mcp/tavily.md@v1.2.0
```

This ensures stability for production workflows while allowing experimentation with latest versions.

## Best Practices for Imports

### Creating Shared Components

**Do:**

- ✅ Make components focused and single-purpose
- ✅ Document configuration options
- ✅ Version significant changes
- ✅ Test with multiple importers
- ✅ Provide examples

**Don't:**

- ❌ Create monolithic "kitchen sink" components
- ❌ Break existing importers without versioning
- ❌ Duplicate functionality across components
- ❌ Hard-code repository-specific values
- ❌ Forget to update documentation

### Using Imports Effectively

**Do:**

- ✅ Import only what you need
- ✅ Override imported settings when necessary
- ✅ Pin critical production workflows
- ✅ Document why each import is needed
- ✅ Test after updating imports

**Don't:**

- ❌ Import conflicting components
- ❌ Override without understanding impact
- ❌ Use unpinned imports in production
- ❌ Cargo-cult import lists
- ❌ Forget to recompile after changes

## Evolution of Shared Components

The shared component library evolved organically:

### Phase 1: Duplication (Workflows 1-10)

Early workflows duplicated configuration. Copy-paste was fastest for initial prototypes.

### Phase 2: Extraction (Workflows 11-30)

As patterns emerged, we extracted common configurations into shared files. First components: `reporting.md` and `python-dataviz.md`.

### Phase 3: Ecosystem (Workflows 31-80)

Component library grew to cover most common needs. New workflows primarily composed existing components.

### Phase 4: Specialization (Later Workflows)

Highly specialized components emerged for specific domains (Copilot analysis, security scanning, etc.).

## Impact on Velocity

The imports system dramatically accelerated development:

| Metric | Without Imports | With Imports |
| ------ | --------------- | ------------ |
| Time to create workflow | 2-4 hours | 15-30 minutes |
| Lines of configuration | 100-200 | 20-40 |
| Maintenance burden | High | Low |
| Consistency | Manual | Automatic |
| Reuse rate | ~0% | ~65% |

## Common Import Patterns

### The Analyst Stack

```markdown
imports:
  - shared/reporting.md
  - shared/jqschema.md
  - shared/python-dataviz.md
```

For read-only analysis workflows with visualization.

### The Researcher Stack

```markdown
imports:
  - shared/reporting.md
  - shared/mcp/tavily.md
  - shared/mcp/arxiv.md
```

For research-heavy workflows needing web search and academic papers.

### The Code Intelligence Stack

```markdown
imports:
  - shared/reporting.md
  - shared/mcp/serena.md
  - shared/mcp/ast-grep.md
```

For semantic code analysis and refactoring.

### The Meta-Agent Stack

```markdown
imports:
  - shared/reporting.md
  - shared/mcp/gh-aw.md
  - shared/charts-with-trending.md
```

For workflows that analyze other workflows.

## What's Next?

The imports system enabled rapid scaling, but even the best components need proper security foundations. All the reusability in the world doesn't help if agents can accidentally cause harm.

In our next article, we'll explore the security lessons learned from operating our collection of automated agentic workflows with access to real repositories.

_More articles in this series coming soon._

[Previous Article](/gh-aw/blog/2026-01-27-operational-patterns/)
