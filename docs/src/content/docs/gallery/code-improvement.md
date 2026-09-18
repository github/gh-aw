---
title: Automated code improvement
description: Use Code Simplifier as a gh-aw example that proposes focused, behavior-preserving code improvements automatically.
---

Automated code improvement with GitHub Agentic Workflows means reviewing recent production-code changes for unnecessary complexity and proposing a small, behavior-preserving simplification. This example is a portable adaptation of the [Code Simplifier workflow](https://github.com/githubnext/agentics/blob/main/workflows/code-simplifier.md).

```aw wrap title=".github/workflows/code-simplifier.md"
---
on:
  schedule: daily

permissions:
  contents: read
  issues: read
  pull-requests: read

safe-outputs:
  create-pull-request:
    title-prefix: "[code-simplifier] "
    draft: true
---

# Code Simplifier

Review production code changed in the last 24 hours. Look for one clear opportunity to remove needless branching, duplication, dead local abstractions, or confusing naming while preserving behavior and public interfaces exactly.

Make only a focused change with a measurable readability or maintainability benefit. Run the repository's formatter, tests, linter, and build. Open a draft pull request describing why the result is simpler and which checks passed. Do nothing when no worthwhile simplification exists.
```

The workflow opens a draft pull request rather than changing the default branch directly. Keep project-specific build and test commands in `AGENTS.md` so the agent can validate its proposal using the repository's normal contribution process.

## Learn More

- [Code Simplifier source workflow](https://github.com/githubnext/agentics/blob/main/workflows/code-simplifier.md)
- [Safe outputs for pull requests](/gh-aw/reference/safe-outputs-pull-requests/)
- [Creating agentic workflows](/gh-aw/setup/creating-workflows/)
