# Internal linter helpers

The packages in this directory are implementation helpers for `pkg/linters`. They are under Go's `internal` import boundary and are not supported public APIs. Their shared responsibilities are documented here; analyzer-specific behavior and the registry are documented in [`pkg/linters/README.md`](../README.md).

| Package | Responsibility |
|---------|----------------|
| `analyzerutil` | Creates analyzers with standard dependencies and provides shared AST traversal/index access. |
| `analyzerutil/analyzertest` | Runs independent diagnostic-only analyzers against shared test fixtures. |
| `astutil` | AST, type-information, source-location, and import-edit helpers used by analyzers. |
| `coverage` | Coverage-profile-aware gating for performance-sensitive linter findings. |
| `filecheck` | Indexes generated and test files and determines which files analyzers should skip. |
| `nolint` | Indexes `nolint` directives and checks whether a directive applies at a source position. |
| `resourcetracker` | Shared state machine for resource acquisitions that require deferred cleanup. |
