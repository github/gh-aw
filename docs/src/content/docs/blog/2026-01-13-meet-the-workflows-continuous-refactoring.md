---
title: "Meet the Workflows: Continuous Refactoring"
description: "Agents that identify structural improvements and systematically refactor code"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T02:15:00
sidebar:
  label: "Meet the Workflows: Continuous Refactoring"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-continuous-simplicity/
  label: "Meet the Workflows: Continuous Simplicity"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-continuous-style/
  label: "Meet the Workflows: Continuous Style"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

Welcome back to [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-simplicity/), we met automated agents that detect complexity and propose simpler solutions. These work tirelessly in the background, cleaning things up. Now let's explore similar agents that take a deeper structural view, extending the automation to *structural refactoring*.

## Continuous Refactoring

Our next three agents continuously analyze code structure, suggesting systematic improvements:

- **[Semantic Function Refactor](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/semantic-function-refactor.md?plain=1)** - Spots refactoring opportunities we might have missed  
- **[Large File Simplifier](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-file-diet.md?plain=1)** - Monitors file sizes and proposes splitting oversized files
- **[Go Pattern Detector](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/go-pattern-detector.md?plain=1)** - Detects common Go patterns and anti-patterns for consistency  

The **Semantic Function Refactor** workflow combines agentic AI with code analysis tools to analyze and address the structure of the entire codebase. It analyzes all Go source files in the `pkg/` directory to identify functions that might be in the wrong place.

As codebases evolve, functions sometimes end up in files where they don't quite belong. Humans struggle to notice these organizational issues because we work on one file at a time and focus on making code work rather than on where it lives.

The workflow performs comprehensive discovery by

1. algorithmically collecting all function names from non-test Go files, then
2. agentically grouping functions semantically by name and purpose.

It then identifies functions that don't fit their current file's theme as outliers, uses Serena-powered semantic code analysis to detect potential duplicates, and creates issues recommending consolidated refactoring. These issues can then be reviewed and addressed by coding agents.

The workflow follows a "one file per feature" principle: files should be named after their primary purpose, and functions within each file should align with that purpose. It closes existing open issues with the `[refactor]` prefix before creating new ones. This prevents issue accumulation and ensures recommendations stay current.

In our own use of Semantic Function Refactoring **36 out of 53 proposed PRs were merged** (67% acceptance rate). It's been impressive to see how many organizational improvements the workflow can identify that we missed, and how practical its suggestions are for improving code structure and maintainability.

An example PR from our own use of this workflow is [Move misplaced extraction functions to frontmatter_extraction.go](https://github.com/github/gh-aw/pull/7043).

### Large File Simplifier: The Size Monitor

Large files are a common code smell - they often indicate unclear boundaries, mixed responsibilities, or accumulated complexity. The **Large File Simplifier** workflow monitors file sizes daily and creates actionable issues when files grow too large.

The workflow runs on weekdays, analyzing all Go source files in the `pkg/` directory. It identifies the largest file, checks if it exceeds healthy size thresholds, and creates a detailed issue proposing how to split it into smaller, more focused files.

What makes this workflow effective is its focus and prioritization. Instead of overwhelming developers with issues about every large file, it creates at most one issue, targeting the largest offender. The workflow also skips if an open `[file-diet]` issue already exists, preventing duplicate work.

In our own use, Large File Simplifier has been remarkably successful: **62 out of 78 proposed PRs were merged** (79% acceptance rate). This demonstrates that the refactoring suggestions are practical and valuable - developers agree with the splits and can implement them efficiently.

The workflow uses Serena for semantic code analysis to understand function relationships and propose logical boundaries for splitting. It doesn't just count lines - it analyzes the code structure to suggest meaningful module boundaries that make sense.

### Go Pattern Detector: The Consistency Enforcer

The **Go Pattern Detector** uses another code analysis tool, `ast-grep`, to scan for specific code patterns and anti-patterns. This uses abstract syntax tree (AST) pattern matching to find exact structural patterns.

Currently, the workflow detects use of `json:"-"` tags in Go structs - a pattern that can indicate fields that should be private but aren't, serialization logic that could be cleaner, or potential API design issues.

The workflow runs in two phases. First, AST scanning runs on a standard GitHub Actions runner:

```bash
# Install ast-grep
cargo install ast-grep --locked

# Scan for patterns
sg --pattern 'json:"-"' --lang go .
```

If patterns are found, it triggers the second phase where the coding agent analyzes the detected patterns, reviews context around each match, determines if patterns are problematic, and creates issues with specific recommendations. This architecture is efficient: fast AST scanning uses minimal resources, expensive AI analysis only runs when needed, false positives don't consume AI budget, and the approach scales to frequent checks without cost concerns.

The workflow is designed to be extended with additional pattern checks - common anti-patterns like ignored errors or global state, project-specific conventions, performance anti-patterns, and security-sensitive patterns.

## The Power of Continuous Refactoring

These workflows demonstrate how AI agents can continuously maintain institutional knowledge about code organization. The benefits compound over time: better organization makes code easier to find, consistent patterns reduce cognitive load, reduced duplication improves maintainability, and clean structure attracts further cleanliness. They're particularly valuable in AI-assisted development, where code gets written quickly and organizational concerns can take a backseat to functionality.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Semantic Function Refactor:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/semantic-function-refactor.md
```

**Large File Simplifier:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/daily-file-diet.md
```

**Go Pattern Detector:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/go-pattern-detector.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Next Up: Continuous Style

Beyond structure and organization, there's another dimension of code quality: presentation and style. How do we maintain beautiful, consistent console output and formatting?

Continue reading: [Meet the Workflows: Continuous Style →](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-style/)

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

---

*This is part 3 of a 19-part series exploring the workflows in Peli's Agent Factory.*
