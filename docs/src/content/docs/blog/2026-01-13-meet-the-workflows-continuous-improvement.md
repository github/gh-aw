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

In our [previous posts](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-simplicity/), we've explored autonomous cleanup agents that continuously improve code: simplifying complexity, refactoring structure, and polishing style. Now we complete the picture with agents that take a *holistic view* - analyzing dependencies, type safety patterns, and overall repository quality.

## Continuous Improvement Workflows

Today's agents analyze higher-level concerns and long-term health:

- **[Go Fan](https://github.com/githubnext/gh-aw/blob/v0.37.7/.github/workflows/go-fan.md?plain=1)** - Daily Go module usage reviewer that analyzes direct dependencies  
- **[Typist](https://github.com/githubnext/gh-aw/blob/v0.37.7/.github/workflows/typist.md?plain=1)** - Analyzes Go type usage patterns to improve type safety  
- **[Functional Pragmatist](https://github.com/githubnext/gh-aw/blob/main/.github/workflows/functional-programming-enhancer.md?plain=1)** - Applies moderate functional programming techniques for clarity and safety  
- **[Repository Quality Improver](https://github.com/githubnext/gh-aw/blob/v0.37.7/.github/workflows/repository-quality-improver.md?plain=1)** - Takes a holistic view of code quality and suggests improvements  

### Go Fan: The Dependency Enthusiast 🐹

The **Go Fan** is perhaps the most uniquely characterized workflow in the factory - an "enthusiastic Go module expert" who performs daily deep-dive reviews of the project's Go dependencies. This isn't just dependency scanning - it's thoughtful analysis of **how well we're using the tools we've chosen**.

Most dependency tools focus on vulnerabilities or outdated versions. Go Fan asks deeper and more positive questions: Are we using this module's best features? Have recent updates introduced better patterns we should adopt? Could we use a more appropriate module for this use case? Are we following the module's recommended practices?

Go Fan uses an intelligent selection algorithm. It extracts direct dependencies from `go.mod`, fetches GitHub metadata for each dependency including last update time, sorts by recency to prioritize recently updated modules, uses round-robin selection to cycle through modules ensuring comprehensive coverage, and maintains persistent memory through cache-memory to track which modules were recently reviewed.

This ensures recently updated modules get reviewed first since new features might be relevant, all modules eventually get reviewed so nothing is forgotten, and reviews don't repeat unnecessarily thanks to cache tracking.

For each selected module, Go Fan researches the module's repository including recent releases and changelog entries, documentation and best practices, and example usage patterns. It analyzes the project's actual usage by using Serena to find all imports and usage, examining actual code patterns, and identifying gaps between best practices and current usage. Then it generates recommendations suggesting better usage patterns, highlighting new features worth adopting, and identifying potential issues or anti-patterns. Finally, it saves summaries under `scratchpad/mods/` and opens GitHub Discussions with findings, complete with specific code examples and recommendations.

The kinds of insights Go Fan produces are quite specific: "The Lipgloss update added adaptive color support - we're still using fixed colors in 12 places," or "Cobra now recommends using ValidArgsFunction instead of ValidArgs - we should migrate," or "We're using low-level HTTP client code - the `go-gh` module we already have provides better abstractions."

The 30-minute timeout gives Go Fan substantial time to do deep research, making each review thorough and actionable.

### Typist: The Type Safety Advocate

The **Typist** analyzes Go type usage patterns with a singular focus: improving type safety. It hunts for untyped code that should be strongly typed, and identifies duplicated type definitions that create confusion.

Typist looks for untyped usages: `interface{}` or `any` where specific types would be better, untyped constants that should have explicit types, and type assertions that could be eliminated with better design. It also hunts for duplicated type definitions - the same types defined in multiple packages, similar types with different names, and type aliases that could be unified.

Using grep patterns to find type definitions, interface{} usage, and any usage combined with Serena's semantic analysis, Typist discovers type definitions across the codebase, identifies semantic duplicates that are structurally similar, analyzes usage patterns where untyped code appears, and generates specific actionable refactoring recommendations.

Strong typing catches bugs at compile time, documents intent, and makes code easier to understand. But as codebases evolve, quick prototypes use `any` for flexibility, similar types emerge in different packages, and type information gets lost in translations.

Typist trails behind development, systematically identifying opportunities to strengthen type safety without slowing down feature development.

Typist creates discussions rather than issues because type safety improvements often involve architectural decisions that benefit from team conversation. Each discussion includes specific file references and line numbers, current problematic patterns, suggested type definitions, and migration path recommendations.

Today's hybrid languages like Go, C# and F# support both strong and dynamic typing. Seeing strong typing as arising from continuous improvement area is a particularly novel insight: rather than enforcing strict typing upfront, we can develop quickly with flexibility, then let autonomous agents like Typist trail behind, strengthening type safety over time.

### Functional Pragmatist: The Pragmatic Purist 🔄

The **Functional Pragmatist** systematically identifies opportunities to apply moderate, tasteful functional programming techniques to improve code clarity, safety, and maintainability. Unlike dogmatic functional approaches, this workflow balances pragmatism with functional purity.

The workflow focuses on seven key patterns: immutability (making data immutable where there's no existing mutation), functional initialization (using composite literals and declarative patterns), transformative operations (leveraging map/filter/reduce approaches), functional options pattern (using option functions for flexible configuration), avoiding shared mutable state (eliminating global variables), pure functions (extracting calculations from side effects), and reusable logic wrappers (creating higher-order functions for retry, logging, caching).

The enhancement process follows a structured approach. During discovery, it searches for variables that could be immutable, imperative loops that could be transformative, initialization anti-patterns, constructors that could use functional options, shared mutable state (global variables and mutexes), functions with side effects that could be pure, and repeated logic patterns that could use wrappers.

For each opportunity, it scores by safety improvement (reduces mutation risk), clarity improvement (makes code more readable), testability improvement (makes code easier to test), and risk level (lower risk gets higher priority). Using Serena for deep analysis, it understands full context, identifies dependencies and side effects, verifies no hidden mutations, and designs specific improvements.

Implementation examples include converting mutable initialization to immutable patterns (using composite literals instead of incremental building), transforming constructors to use functional options (allowing extensible APIs without breaking changes), eliminating global state through explicit parameter passing, extracting pure functions from impure code (separating calculations from I/O), and creating reusable wrappers like `Retry[T]` with exponential backoff, `WithTiming[T]` for performance logging, and `Memoize[K,V]` for caching expensive computations.

The workflow applies principles of immutability first (variables are immutable unless mutation is necessary), declarative over imperative (initialization expresses "what" not "how"), transformative over iterative (data transformations use functional patterns), explicit parameters (pass dependencies rather than using globals), pure over impure (separate calculations from side effects), and composition over complexity (build behavior from simple wrappers).

What makes this workflow particularly effective is its pragmatism. It doesn't force functional purity at the cost of clarity. Go's simple, imperative style is respected - sometimes a for-loop is clearer than a functional helper. The workflow only adds abstraction where it genuinely improves code, focusing on low-risk changes like converting `var x T; x = value` to `x := value`, using composite literals, and extracting pure helper functions.

The result is code that's safer (reduced mutation surface area), more testable (pure functions need no mocks), more maintainable (functional patterns are easier to reason about), and more extensible (functional options allow API evolution). The workflow runs on a schedule (Tuesday and Thursday mornings), systematically improving functional patterns across the entire codebase over time.

### Repository Quality Improver: The Holistic Analyst

The **Repository Quality Improver** takes the widest view of any workflow we've discussed. Rather than focusing on a specific aspect (simplicity, refactoring, styling, types), it selects a *focus area* each day and analyzes the repository from that perspective.

The workflow uses cache memory to track which areas it has recently analyzed, ensuring diverse coverage through a careful distribution: roughly 60% custom areas exploring repository-specific concerns that emerge from analysis, 30% standard categories covering fundamentals like code quality, documentation, testing, security, and performance, and 10% reuse occasionally revisiting areas for consistency. This distribution ensures novel insights from creative focus areas, systematic coverage of fundamental concerns, and periodic verification that previous improvements held.

Standard categories include code quality and static analysis, documentation completeness, testing coverage and quality, security best practices, and performance optimization. Custom areas are repository-specific: error message consistency, CLI flag naming conventions, workflow YAML generation patterns, console output formatting, and configuration file validation.

The analysis workflow loads history by checking cache for recent focus areas, selects the next area based on rotation strategy, spends 20 minutes on deep analysis from that perspective, generates discussions with actionable recommendations, and saves state by updating cache with this run's focus area.

A repository is more than the sum of its parts. Individual workflows optimize specific concerns, but quality emerges from balance. Is error handling consistent across the codebase? Do naming conventions align throughout? Are architectural patterns coherent? Does the overall structure make sense?

The Repository Quality Improver looks for these cross-cutting concerns that don't fit neatly into "simplify" or "refactor" but nonetheless impact overall quality.

## The Power of Holistic Improvement

Together, these workflows complete the autonomous improvement picture. Go Fan ensures our dependencies stay fresh and well-used, Typist systematically strengthens type safety, Functional Pragmatist applies moderate functional techniques for clarity and safety, and Repository Quality Improver maintains overall coherence.

Combined with our earlier workflows covering simplicity, refactoring, and style, we now have agents that continuously improve code at every level: the Terminal Stylist ensures beautiful output at the line level, Code Simplifier removes complexity at the function level, Semantic Function Refactor improves organization at the file level, Go Pattern Detector enforces consistency at the pattern level, Functional Pragmatist applies functional patterns for clarity and safety, Typist strengthens type safety at the type level, Go Fan optimizes dependencies at the module level, and Repository Quality Improver maintains coherence at the repository level.

This is the future of code quality: not periodic cleanup sprints, but continuous autonomous improvement across every dimension simultaneously.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://githubnext.github.io/gh-aw/setup/quick-start/), then run one of the following:

**Go Fan:**

```bash
gh aw add https://github.com/githubnext/gh-aw/blob/v0.37.7/.github/workflows/go-fan.md
```

**Typist:**

```bash
gh aw add https://github.com/githubnext/gh-aw/blob/v0.37.7/.github/workflows/typist.md
```

**Functional Pragmatist:**

```bash
gh aw add https://github.com/githubnext/gh-aw/blob/main/.github/workflows/functional-programming-enhancer.md
```

**Repository Quality Improver:**

```bash
gh aw add https://github.com/githubnext/gh-aw/blob/v0.37.7/.github/workflows/repository-quality-improver.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://githubnext.github.io/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Next Up: Continuous Documentation

Beyond code quality, we need to keep documentation accurate and up-to-date as code evolves. How do we maintain docs that stay current?

Continue reading: [Continuous Documentation Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-documentation/)

## Learn More

- **[GitHub Agentic Workflows](https://githubnext.github.io/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://githubnext.github.io/gh-aw/setup/quick-start/)** - How to write and compile workflows

---

*This is part 5 of a 19-part series exploring the workflows in Peli's Agent Factory.*
