---
title: "Meet the Workflows: Continuous Improvement"
description: "Agents that take a holistic view of repository health"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T02:45:00
sidebar:
  label: "Continuous Improvement"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-continuous-style/
  label: "Meet the Workflows: Continuous Style"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-documentation/
  label: "Meet the Workflows: Continuous Documentation"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

Welcome back to [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)!

In our [previous posts](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-simplicity/), we've explored autonomous cleanup agents. Now we complete the picture with agents that analyze dependencies, type safety, and overall repository quality.

## Continuous Improvement Workflows

- **[Go Fan](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/go-fan.md?plain=1)** - Daily Go module usage reviewer  
- **[Typist](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/typist.md?plain=1)** - Analyzes type usage patterns for improved safety  
- **[Functional Pragmatist](https://github.com/github/gh-aw/blob/main/.github/workflows/functional-programming-enhancer.md?plain=1)** - Applies functional techniques pragmatically  
- **[Repository Quality Improver](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/repository-quality-improver.md?plain=1)** - Holistic code quality analysis  

### Go Fan: The Dependency Enthusiast 🐹

The **Go Fan** is perhaps the most uniquely characterized workflow in the factory - an "enthusiastic Go module expert" who performs daily deep-dive reviews of the project's Go dependencies. This isn't just dependency scanning - it's thoughtful analysis of **how well we're using the tools we've chosen**.

Most dependency tools focus on vulnerabilities or outdated versions. Go Fan asks deeper and more positive questions: Are we using this module's best features? Have recent updates introduced better patterns we should adopt? Could we use a more appropriate module for this use case? Are we following the module's recommended practices?

Go Fan uses an intelligent selection algorithm. It extracts direct dependencies from `go.mod`, fetches GitHub metadata for each dependency including last update time, sorts by recency to prioritize recently updated modules, uses round-robin selection to cycle through modules ensuring comprehensive coverage, and maintains persistent memory through cache-memory to track which modules were recently reviewed.

This ensures recently updated modules get reviewed first since new features might be relevant, all modules eventually get reviewed so nothing is forgotten, and reviews don't repeat unnecessarily thanks to cache tracking.

For each module, Go Fan researches the repository (releases, docs, best practices), analyzes actual usage patterns using Serena, and generates actionable recommendations. It saves summaries under `scratchpad/mods/` and opens GitHub Discussions.

The output of Go Fan is a discussion, which is then often "task mined" for actionable tasks using the [TaskOps](https://github.github.com/gh-aw/patterns/taskops/) design pattern.

Let's take a look at an example of how this works:

1. Go Fan created the [Go Module Review: actionlint](https://github.com/github/gh-aw/discussions/7472) after the `actionlint` module was updated.
2. Peli [requested the Plan agent](https://github.com/github/gh-aw/discussions/7472#discussioncomment-15342254) mine for actionable tasks.
3. This created [a parent issue](https://github.com/github/gh-aw/issues/7648) and 5 sub-tasks.
4. The subtasks were then solved by further workflow runs. An example PR is [Implement parallel multi-file actionlint execution](https://github.com/github/gh-aw/issues/7649).

### Typist: The Type Safety Advocate

The **Typist** analyzes Go type usage patterns with a singular focus: improving type safety. It hunts for untyped code that should be strongly typed, and identifies duplicated type definitions that create confusion.

Typist looks for untyped usages: `interface{}` or `any` where specific types would be better, untyped constants that should have explicit types, and type assertions that could be eliminated with better design. It also hunts for duplicated type definitions - the same types defined in multiple packages, similar types with different names, and type aliases that could be unified.

Using grep patterns and Serena's semantic analysis, it discovers type definitions, identifies semantic duplicates, analyzes untyped usage patterns, and generates refactoring recommendations.

Typist also uses the [TaskOps](https://github.github.com/gh-aw/patterns/taskops/) pattern. This means the job of Typist is not to fix code, but to analyze code and recommend possible improvements.

Let's take a look at an example of this in practice:

- Typist created the [Typist - Go Type Consistency Analysis Report](https://github.com/github/gh-aw/discussions/4082). This used grep and other tools to perform acomprehensive analysis examining 208 non-test Go files.
- The report found 477 instances of `map[string]any` usage, 36 untyped constants and 30+ uses `any` in function signatures.
- Peli request `/plan` on that issue, causing the Plan agent to do further research and create 5 issues for work to be done such as [Create unified ToolsConfig struct in tools_types.go](https://github.com/github/gh-aw/issues/4155).
- 4/5 of these issues were then solved by Copilot. For example [Add unified ToolsConfig struct to replace map[string]any pattern](https://github.com/github/gh-aw/pull/4158).

The static v. dynamic typing debate has raged for decades. Today's hybrid languages like Go, C# and F# support both strong and dynamic typing. Continuous typing improvement offers **a new and refreshing perspective on this old debate**: rather than enforcing strict typing upfront, we can develop quickly with flexibility, then let autonomous agents like Typist trail behind, strengthening type safety over time. This allows us to get the best of both worlds: rapid development without getting bogged down in type design, while still achieving strong typing and safety as the codebase matures.

### Functional Pragmatist: The Pragmatic Purist 🔄

**Functional Pragmatist** applies moderate functional programming techniques to improve code clarity and safety, balancing pragmatism with functional principles.

The workflow focuses on seven patterns: immutability, functional initialization, transformative operations (map/filter/reduce), functional options pattern, avoiding shared mutable state, pure functions, and reusable logic wrappers.

It searches for opportunities (mutable variables, imperative loops, initialization anti-patterns, global state), scores by safety/clarity/testability improvements, uses Serena for deep analysis, and implements changes like converting to composite literals, using functional options, eliminating globals, extracting pure functions, and creating reusable wrappers (Retry, WithTiming, Memoize).

The workflow is pragmatic: Go's simple style is respected, for-loops stay when clearer, and abstraction is added only where it genuinely improves code. It runs Tuesday and Thursday mornings, systematically improving patterns over time.

An example PR from our own use of this workflow is [Apply functional programming and immutability improvements](https://github.com/github/gh-aw/pull/12921).

### Repository Quality Improver: The Holistic Analyst

**Repository Quality Improver** takes the widest view, selecting a different *focus area* each day to analyze the repository from that perspective.

It uses cache memory to ensure diverse coverage: 60% custom areas (repository-specific concerns), 30% standard categories (code quality, documentation, testing, security, performance), and 10% revisits for consistency.

Standard categories cover fundamentals. Custom areas are repository-specific: error message consistency, CLI flag naming conventions, workflow YAML generation patterns, console output formatting, configuration validation.

The workflow loads recent history, selects the next area, spends 20 minutes on deep analysis, generates discussions with recommendations, and saves state. It looks for cross-cutting concerns that don't fit neatly into other categories but impact overall quality.

Example reports from our own use of this workflow are:

- [Repository Quality Improvement - CI/CD Optimization](https://github.com/github/gh-aw/discussions/6863)
- [Repository Quality Improvement Report - Performance](https://github.com/github/gh-aw/discussions/13280).

## The Power of Continuous Improvement

These workflows complete the autonomous improvement picture: Go Fan keeps dependencies fresh, Typist strengthens type safety, Functional Pragmatist applies functional techniques, and Repository Quality Improver maintains coherence.

Combined with earlier workflows, we have agents improving code at every level: line-level output (Terminal Stylist), function-level complexity (Code Simplifier), file-level organization (Semantic Function Refactor), pattern-level consistency (Go Pattern Detector), functional clarity (Functional Pragmatist), type safety (Typist), module dependencies (Go Fan), and repository coherence (Repository Quality Improver).

This is the future of code quality: not periodic cleanup sprints, but continuous autonomous improvement across every dimension simultaneously.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Go Fan:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/go-fan.md
```

**Typist:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/typist.md
```

**Functional Pragmatist:**

```bash
gh aw add https://github.com/github/gh-aw/blob/main/.github/workflows/functional-programming-enhancer.md
```

**Repository Quality Improver:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/repository-quality-improver.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Next Up: Continuous Documentation

Beyond code quality, we need to keep documentation accurate and up-to-date as code evolves. How do we maintain docs that stay current?

Continue reading: [Continuous Documentation Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-documentation/)

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

---

*This is part 5 of a 19-part series exploring the workflows in Peli's Agent Factory.*
