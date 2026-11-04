# workflowcontract Package

> Test-only contracts that keep selected workflow sources and compiled workflows aligned.

## Overview

The `workflowcontract` package contains Go tests, not runtime functionality. Its tests read workflow Markdown and compiled lock files from the repository and assert that selected prompt wording, configuration, and compiled behavior continue to agree.

These checks cover workflow details that are easy to regress when editing prompts or configuration independently of their tests. A contract failure indicates that either the workflow changed intentionally and its assertion needs review, or the workflow no longer satisfies an established repository requirement.

## Contracts

| Test | Contract |
|------|----------|
| `TestCommitChangesAnalyzerExperimentContract` | Checks the commit-changes analyzer's experiment assignment, read-only permissions, variant/evaluation configuration, guardrails, pinned analysis range, and prompt protocol. |
| `TestDailyCLIToolsTesterCoverageContract` | Preserves required coverage and incomplete-reporting language while rejecting obsolete or misleading instructions. |
| `TestDailyPerformanceSummaryUsesStableWindowMetrics` | Keeps daily performance reporting based on stable lifecycle-window metrics and expected query instructions. |
| `TestGitHubQueryScriptsUseFileBackedPagination` | Requires paginated GitHub query results to be merged through files rather than large command arguments. |
| `TestRoutedSmokeDetectionChecksMatchCompiledRouting` | Verifies detection assertions use the same allowed models and detection model as the compiled workflow routing. |

## Running the Tests

Run the package contracts from the repository root:

```bash
go test ./pkg/workflowcontract
```

The tests depend on workflow source and compiled lock files in this checkout; they do not call GitHub APIs or execute the workflows.

## Dependencies

Tests use the Go standard library, `go.yaml.in/yaml/v3` for workflow frontmatter, and `github.com/goccy/go-yaml` for compiled workflow YAML.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
