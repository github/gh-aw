# Architecture Diagram

> Last updated: 2026-10-26 · Source: [Architecture diagram issue](https://github.com/github/gh-aw/issues?q=is%3Aissue%20%22Architecture%20Diagram%20%E2%80%94%202026-10-26%22)

## Overview

This diagram shows the package structure and dependencies of the `gh-aw` codebase. Arrows point from an importing package to an imported package.

```text
┌──────────────────────────────────────────────────────────────────────────────────────────────┐
│ ENTRY POINTS                                                                                 │
│ cmd/gh-aw ─▶ cli, workflow, parser, console    cmd/gh-aw-wasm ─▶ workflow, parser            │
│ cmd/linters ─▶ linters                         cmd/gh-aw-security-model (standalone)         │
├──────────────────────────────────────────────────────────────────────────────────────────────┤
│ CORE PACKAGES                                                                                │
│ cli (commands) ─▶ workflow (compiler) ─▶ parser (frontmatter) ─▶ console (terminal UI)       │
│ cli ─▶ actionpins, agentdrain, intent, github, githubapi, modelsdev, scanfindings, workqueue │
│ workflow ─▶ actionpins, scanfindings, workqueue, console      parser ─▶ githubapi            │
│ workqueue ─▶ parser, githubapi, console         actionpins ─▶ console                        │
├──────────────────────────────────────────────────────────────────────────────────────────────┤
│ UTILITIES (arrows point from importer to imported package)                                   │
│ cli/workflow/parser ─▶ fileutil, gitutil, logger, stringutil, types, constants               │
│ parser/workflow ─▶ importinpututil, jsonutil, setutil, sliceutil, syncutil, typeutil         │
│ workflow ─▶ repoutil, semverutil, validationerror, ctxutil, tty                              │
│ cli ─▶ envutil, errorutil, stats, timeutil     console ─▶ colorwriter, styles, tty           │
│ console ─▶ logger, stringutil                 logger ─▶ colorwriter, styles, timeutil        │
└──────────────────────────────────────────────────────────────────────────────────────────────┘
```

## Package Reference

| Package | Layer | Description |
|---|---|---|
| `cmd/gh-aw` | Entry | Main CLI binary |
| `cmd/gh-aw-wasm` | Entry | WebAssembly compiler target |
| `cmd/gh-aw-security-model` | Entry | Security conformance runner |
| `cmd/linters` | Entry | Custom linter binary |
| `actionpins` | Core | Action reference and SHA pin resolution |
| `agentdrain` | Core | Agent log mining |
| `cli` | Core | Command implementations |
| `console` | Core | Terminal UI rendering |
| `github` | Core | GitHub label and objective mapping |
| `githubapi` | Core | GitHub API client options |
| `intent` | Core | Intent policy resolution |
| `linters` | Core | Custom Go analyzers |
| `modelsdev` | Core | Model provider normalization |
| `parser` | Core | Markdown and frontmatter parsing |
| `scanfindings` | Core | Scan finding data model |
| `workflow` | Core | Workflow compilation and generation |
| `workqueue` | Core | Work queue selection and delivery |
| `colorwriter` | Utility | Color aware writer |
| `constants` | Utility | Shared constants |
| `ctxutil` | Utility | Context helpers |
| `envutil` | Utility | Environment validation |
| `errorutil` | Utility | Error classification |
| `fileutil` | Utility | File helpers |
| `gitutil` | Utility | Git repository helpers |
| `importinpututil` | Utility | Workflow import inputs |
| `jsonutil` | Utility | JSON helpers |
| `logger` | Utility | Namespace debug logging |
| `repoutil` | Utility | Repository URLs and slugs |
| `semverutil` | Utility | Semantic version helpers |
| `setutil` | Utility | Set helpers |
| `sliceutil` | Utility | Slice helpers |
| `stats` | Utility | Numerical statistics |
| `stringutil` | Utility | String helpers |
| `styles` | Utility | Terminal styles |
| `syncutil` | Utility | Concurrency helpers |
| `testutil` | Utility | Shared test helpers |
| `timeutil` | Utility | Time helpers |
| `tty` | Utility | Terminal detection |
| `types` | Utility | Shared types |
| `typeutil` | Utility | Untyped value conversion |
| `validationerror` | Utility | Validation error payloads |
