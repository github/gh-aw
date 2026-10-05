# Architecture Diagram

> Last updated: 2026-10-05 · Source: [Architecture diagram issue](https://github.com/github/gh-aw/issues?q=is%3Aissue+%22Architecture+diagram+%E2%80%94+2026-10-05%22)

## Overview

This diagram shows the package structure and dependencies of the `gh-aw` Go codebase. Arrows point from an importing package to an imported package.

```text
┌──────────────────────────────────────────────────────────────────────────────────────────────┐
│ ENTRY POINTS                                                                                 │
│ cmd/gh-aw ─▶ cli     cmd/gh-aw-wasm ─▶ workflow, parser     cmd/linters ─▶ linters           │
│ cmd/gh-aw-security-model (standalone security conformance runner)                            │
├──────────────────────────────────────────────────────────────────────────────────────────────┤
│ CORE PACKAGES                                                                                │
│ cli (commands) ─▶ workflow (compiler) ─▶ parser (frontmatter) ─▶ console (terminal UI)       │
│ cli ─▶ actionpins (pinning), agentdrain (logs), intent (policy), workqueue (queue)           │
│ cli ─▶ github (labels), githubapi (API options), modelsdev (model IDs), scanfindings (scans) │
│ workflow ─▶ actionpins, scanfindings, console       parser ─▶ console                        │
│ parser/workflow ─▶ importinpututil (import inputs)     cmd/linters ─▶ linters                │
├──────────────────────────────────────────────────────────────────────────────────────────────┤
│ UTILITIES — arrows point from importer to imported package                                   │
│ cli/workflow/parser ─▶ fileutil, gitutil, logger, stringutil, types, constants               │
│ parser/workflow ─▶ jsonutil, setutil, sliceutil, syncutil, typeutil                          │
│ workflow ─▶ repoutil, semverutil, validationerror, ctxutil, tty                              │
│ cli ─▶ envutil, errorutil, stats, timeutil                                                   │
│ console ─▶ colorwriter, logger, stringutil, styles, tty                                      │
│ logger ─▶ colorwriter, styles, timeutil     tests: testutil, workflowcontract                │
└──────────────────────────────────────────────────────────────────────────────────────────────┘
```

## Package Reference

| Package | Layer | Description |
|---------|-------|-------------|
| `cmd/gh-aw` | Entry | Main gh aw CLI binary |
| `cmd/gh-aw-wasm` | Entry | WebAssembly compile target |
| `cmd/gh-aw-security-model` | Entry | Standalone workflow security conformance runner |
| `cmd/linters` | Entry | Custom Go linter binary |
| `actionpins` | Core | GitHub Actions reference and SHA pin resolution |
| `agentdrain` | Core | Agent log mining and drain pipeline |
| `cli` | Core | CLI command implementations |
| `console` | Core | Terminal UI rendering and formatting |
| `github` | Core | GitHub label to objective mapping |
| `githubapi` | Core | go-gh API client options |
| `intent` | Core | Intent and governance policy resolution |
| `linters` | Core | Custom Go analysis linters |
| `modelsdev` | Core | Model provider and ID normalization |
| `parser` | Core | Workflow markdown and YAML frontmatter parsing |
| `scanfindings` | Core | Shared scan finding data model |
| `workflow` | Core | GitHub Actions workflow compilation and generation |
| `workqueue` | Core | Work queue transaction and selection model |
| `colorwriter` | Utility | Color aware writer for terminal output |
| `constants` | Utility | Shared constants and defaults |
| `ctxutil` | Utility | Context handling helpers |
| `envutil` | Utility | Environment variable reading and validation |
| `errorutil` | Utility | Error classification helpers |
| `fileutil` | Utility | File path and operation helpers |
| `gitutil` | Utility | Git repository helpers |
| `importinpututil` | Utility | Workflow import input helpers |
| `jsonutil` | Utility | JSON handling helpers |
| `logger` | Utility | Namespace based debug logging |
| `repoutil` | Utility | GitHub repository slug and URL helpers |
| `semverutil` | Utility | Semantic version helpers |
| `setutil` | Utility | Set helpers |
| `sliceutil` | Utility | Slice helpers |
| `stats` | Utility | Numerical statistics helpers |
| `stringutil` | Utility | String helpers |
| `styles` | Utility | Terminal style definitions |
| `syncutil` | Utility | Concurrency helpers |
| `testutil` | Utility | Shared test helpers |
| `timeutil` | Utility | Time helpers |
| `tty` | Utility | Terminal detection |
| `types` | Utility | Shared data types |
| `typeutil` | Utility | Untyped value conversion helpers |
| `validationerror` | Utility | Shared validation error payloads |
| `workflowcontract` | Utility | Workflow contract tests only |
