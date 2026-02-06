---
title: "Meet the Workflows: Continuous Simplicity"
description: "Agents that detect complexity and propose simpler solutions"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T02:00:00
sidebar:
  label: "Continuous Simplicity"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows/
  label: "Meet a Simple Triage Workflow"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-continuous-refactoring/
  label: "Meet the Workflows: Continuous Refactoring"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

Ah, what marvelous timing! Come, come, let me show you the *next wonders* in [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows/), we explored how a simple triage workflow helps us stay on top of incoming activity - automatically labeling issues and reducing cognitive load.

Now let's meet the agents that work quietly in the background to keep code simple and clean. These workflows embody a powerful principle: **code quality is not a destination, it's a continuous practice**. While developers race ahead implementing features and fixing bugs, autonomous cleanup agents trail behind, constantly sweeping, polishing, and simplifying. Let's meet the agents that hunt for complexity.

## Continuous Simplicity

The next two agents represent different aspects of  code simplicity: detecting *overcomplicated code* and *duplicated logic*:

- **[Automatic Code Simplifier](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/code-simplifier.md?plain=1)** - Analyzes recently modified code and creates PRs with simplifications  
- **[Duplicate Code Detector](https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/duplicate-code-detector.md?plain=1)** - Uses Serena's semantic analysis to identify duplicate code patterns  

The **Automatic Code Simplifier** runs daily, analyzing recently modified code for opportunities to simplify without changing functionality. It looks at what changed in the last few commits and asks: "Could this be clearer? Could it be shorter? Could it be more idiomatic?"

This workflow is particularly valuable after rapid development sessions. When you're racing to implement a feature or fix a bug, code often becomes more complex than necessary. Variables get temporary names, logic becomes nested, error handling gets verbose. The workflow tirelessly cleans up after these development sessions, creating PRs that preserve functionality while improving clarity, consistency, and maintainability.

The kinds of simplifications it proposes range from extracting repeated logic into helper functions to converting nested if-statements to early returns. It spots opportunities to simplify boolean expressions, use standard library functions instead of custom implementations, and consolidate similar error handling patterns.

An example PR from our own use of this workflow is [Simplify validation config code for clarity](https://github.com/github/gh-aw/pull/13118).

The **Duplicate Code Detector** uses traditional, road-tested semantic code analysis in conjunction with agentic reasoning to find duplicate patterns. It understands code *meaning* rather than just textual similarity, catching patterns where:

- The same logic appears with different variable names
- Similar functions exist across different files
- Repeated patterns could be extracted into utilities
- Structure is duplicated even if implementation differs

What makes this workflow special is its use of semantic analysis through [Serena](https://oraios.github.io/serena/) - a powerful coding agent toolkit capable of turning an LLM into a fully-featured agent that works directly on your codebase. When we use Serena, we understand code at the compiler-resolved level, not just syntax.

The workflow focuses on recent changes in the latest commits, intelligently filtering out test files, workflows, and non-code files. It creates issues only for significant duplication: patterns spanning more than 10 lines or appearing in 3 or more locations. It performs a multi-phase analysis. It starts by setting up Serena's semantic environment for the repository, then finds changed `.go` and `.cjs` files while excluding tests and workflows. Using `get_symbols_overview` and `find_symbol`, it understands structure, identifies similar function signatures and logic blocks, and compares symbol overviews across files for deeper similarities. It creates issues with the `[duplicate-code]` prefix and limits itself to 3 issues per run, preventing overwhelm. Issues include specific file references, code snippets, and refactoring suggestions.

In our use of an early version of Duplicate Code Detector over 2 months, the agent raised **82 of 105 PRs were merged**, demonstrating the practical value of its suggestions.

## Continuous AI for Simplicity - A New Paradigm

Together, these workflows point towards **an emerging shift in how we maintain code quality**. Instead of periodic "cleanup sprints" or waiting for code reviews to catch complexity, we have agents that clean up after us and continuously monitor and propose improvements. This is especially valuable in AI-assisted development. When developers use AI to write code faster, these cleanup agents ensure speed doesn't sacrifice simplicity. They understand the same patterns that humans recognize but apply them consistently across the entire codebase, every day.

The workflows never take a day off, never get tired, and never let technical debt accumulate. They embody the principle that *good enough* can always become *better*, and that incremental improvements compound over time.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Automatic Code Simplifier:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/code-simplifier.md
```

**Duplicate Code Detector:**

```bash
gh aw add https://github.com/github/gh-aw/blob/v0.42.4/.github/workflows/duplicate-code-detector.md
```

Then edit and remix the workflow specifications to meet your needs, recompile using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

## Next Up: Continuous Refactoring

Simplification is just the beginning. Beyond removing complexity, we can use agents to continuously improve code in many more ways. Our next posts explore this topic.

Continue reading: [Continuous Refactoring →](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-refactoring/)

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

---

*This is part 2 of a 19-part series exploring the workflows in Peli's Agent Factory.*
