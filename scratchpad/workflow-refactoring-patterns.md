# Workflow Refactoring Patterns

This document describes the patterns and practices used for refactoring large agentic workflows into smaller modules that each cover one concern.

## Overview

The workflow complexity reduction initiative addresses workflows that have grown beyond 600 lines, making them difficult to maintain, debug, and test. This document captures the refactoring patterns used to modularize these workflows.

## Refactoring Principles

### 1. Extract Common Functionality

Move reusable components to `.github/workflows/shared/` directory:
- **Data collection modules**: Pre-fetch and prepare data (e.g., `copilot-session-data-fetch.md`)
- **Analysis strategies**: Reusable analytical patterns (e.g., `session-analysis-strategies.md`)
- **Visualization modules**: Chart generation and data visualization (e.g., `session-analysis-charts.md`)
- **Utility modules**: Common utilities like `reporting.md`, `python-dataviz.md`, `trends.md`

### 2. Split by Concern

Separate workflows into distinct phases:
- **Data collection**: Fetch and prepare input data
- **Analysis**: Process and analyze data
- **Visualization**: Generate charts and visualizations
- **Reporting**: Create discussions, issues, or PRs

### 3. Use Imports for Composition

Compose workflows from shared modules using `imports:`:

```yaml
imports:
  - shared/copilot-session-data-fetch.md
  - shared/session-analysis-charts.md
  - shared/session-analysis-strategies.md
  - shared/reporting.md
```

## Size Guidelines

- **Target**: 400-500 lines maximum per workflow
- **Typical**: 200-300 lines for most workflows
- **Hard limit**: 600 lines (refactor above this)

## Refactoring Pattern Examples

### Example 1: Session Analysis Workflow

**Before** (748 lines):
```markdown
---
imports:
  - shared/copilot-session-data-fetch.md
  - shared/reporting.md
  - shared/trends.md
---

# Copilot Agent Session Analysis

[... 748 lines of mixed concerns: chart generation, analysis strategies, reporting templates ...]
```

**After** (403 lines):

**Main workflow** (`copilot-session-insights.md`):
```markdown
---
imports:
  - shared/copilot-session-data-fetch.md
  - shared/session-analysis-charts.md
  - shared/session-analysis-strategies.md
  - shared/reporting.md
---

# Copilot Agent Session Analysis

## Mission
[High-level mission and context]

## Task Overview
[Reference shared modules for implementation details]
```

**Extracted modules**:
- `shared/session-analysis-charts.md` (117 lines): Chart generation patterns and requirements
- `shared/session-analysis-strategies.md` (201 lines): Analysis strategies and patterns

### Example 2: CI Optimization Workflow

**Before** (725 lines):
```markdown
---
[Long steps section with data download, build, test setup]
---

# CI Optimization Coach

[... 725 lines of mixed concerns: data collection, test coverage analysis, optimization strategies ...]
```

**After** (280 lines):

**Main workflow** (`ci-coach.md`):
```markdown
---
imports:
  - shared/ci-data-analysis.md
  - shared/ci-optimization-strategies.md
  - shared/reporting.md
---

# CI Optimization Coach

## Analysis Framework
[Reference shared modules for strategies]
```

**Extracted modules**:
- `shared/ci-data-analysis.md` (154 lines): Data collection, build, and test execution
- `shared/ci-optimization-strategies.md` (186 lines): Optimization analysis patterns

## Shared Module Structure

### Data Collection Modules

Pattern for modules that fetch and prepare data:

````markdown
---
# Module name and description
#
# Usage:
#   imports:
#     - shared/module-name.md
#
# This import provides:
# - List of capabilities

imports:
  - shared/dependency.md  # If needed

tools:
  cache-memory: true
  bash: ["*"]

steps:
  - name: Fetch data
    run: |
      # Data collection logic
---

# Module Documentation

Available data:
- Location 1: Description
- Location 2: Description

Usage examples:
```bash
# How to use the collected data
```
````

### Analysis Strategies Modules

Pattern for modules that define analytical approaches:

```markdown
---
# Module name and description
#
# Usage:
#   imports:
#     - shared/module-name.md
---

# Strategy Name

## Standard Strategies

### Strategy 1: Name
- Description
- When to use
- Expected output

### Strategy 2: Name
- Description
- When to use
- Expected output

## Advanced Strategies

[More complex or experimental strategies]
```

### Visualization Modules

Pattern for modules that generate charts and visualizations:

```markdown
---
# Module name and description
#
# Usage:
#   imports:
#     - shared/module-name.md

imports:
  - shared/python-dataviz.md  # For Python-based charts
---

# Chart Generation

## Chart 1: Name
- Description
- Data requirements
- Output location
- Implementation pattern

## Chart 2: Name
- Description
- Data requirements
- Output location
- Implementation pattern
```

## Refactoring Checklist

When refactoring a large workflow:

- [ ] Identify distinct concerns in the workflow
- [ ] Extract data collection steps to shared module
- [ ] Extract analysis strategies to shared module
- [ ] Extract visualization logic to shared module (if applicable)
- [ ] Update main workflow to use imports
- [ ] Verify workflow compiles successfully
- [ ] Check that line count is < 500 (recommended range 200-400)
- [ ] Test workflow functionality
- [ ] Document extracted modules with usage examples

## Benefits

### Maintainability
- Each module covers one concern, which narrows what a reader must follow
- Changes to shared logic propagate to all workflows
- Separation of concerns between modules

### Testability
- Smaller modules reduce test scope
- Shared modules can be tested independently
- Smaller review scope for each change

### Reusability
- Shared modules serve multiple workflows
- Common patterns defined once
- New workflows can import existing modules

### Debugging
- Module boundaries isolate failure scope
- Error messages include specific module context
- Issues are localized to a single concern

## Common Patterns

### Pattern: Data Fetch + Analysis + Visualization

```text
Main Workflow (300 lines)
├── Import: data-fetch.md (150 lines)
├── Import: analysis-strategies.md (200 lines)
├── Import: visualization.md (120 lines)
└── Import: reporting.md (15 lines)
```

### Pattern: Build + Analyze + Propose Changes

```text
Main Workflow (280 lines)
├── Import: build-and-test.md (180 lines)
├── Import: optimization-strategies.md (190 lines)
└── Import: reporting.md (15 lines)
```

## Anti-Patterns to Avoid

❌ **Don't over-extract**: Keep related logic together. Not every 50-line section needs to be a separate module.
   - **Bad example**: Extracting a 30-line section just because it's slightly different
   - **Good example**: Extracting a 150-line section that's used by 3+ workflows

❌ **Don't create circular dependencies**: Shared modules should not import each other in circular ways.
   - **Bad example**: Module A imports Module B, which imports Module C, which imports Module A
   - **Good example**: Linear dependency chain: Main → Module A → Module B

❌ **Don't duplicate shared logic**: If two modules need the same setup, extract it to a common base module.
   - **Bad example**: Both `data-analysis-a.md` and `data-analysis-b.md` have identical data fetch code
   - **Good example**: Extract common data fetch to `data-fetch.md`, both modules import it

❌ **Don't make modules too generic**: Each module should cover one topic rather than act as a catch-all utility.
   - **Bad example**: `shared/utilities.md` with 500 lines of unrelated functions
   - **Good example**: `shared/python-dataviz.md` focused on data visualization setup

## Success Metrics

A successful refactoring achieves:
- ✅ Main workflow < 500 lines (recommended range 200-400)
- ✅ No more than 3 distinct concerns per workflow
- ✅ Reusable shared modules with a stated purpose
- ✅ Workflow compiles without errors
- ✅ Functionality preserved (verified by testing)

## References

- **Refactored Workflows**:
  - `copilot-session-insights.md`: 748 → 403 lines (46% reduction)
  - `ci-coach.md`: 725 → 280 lines (61% reduction)

- **Created Shared Modules**:
  - `shared/session-analysis-charts.md`
  - `shared/session-analysis-strategies.md`
  - `shared/ci-data-analysis.md`
  - `shared/ci-optimization-strategies.md`
  - `shared/token-cost-analysis.md`

## Future Work

Additional workflows identified for refactoring:
- `daily-copilot-token-report.md` (680 lines)
- `prompt-clustering-analysis.md` (639 lines)
- `developer-docs-consolidator.md` (623 lines)

These workflows can follow the same patterns established in this document.
