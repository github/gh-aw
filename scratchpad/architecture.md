# Architecture Diagram

> Last updated: 2026-11-09 · Source: [Architecture Diagram issue](https://github.com/github/gh-aw/issues?q=is%3Aissue%20%22Architecture%20Diagram%20%E2%80%94%202026-11-09%22)

## Overview

This diagram shows the package structure and dependencies of the `gh-aw` codebase. Arrows point from an importing package to an imported package.

```text
┌──────────────────────────────────────────────────────────────────────────────────────────────┐
│ ENTRY POINTS                                                                                 │
│ cmd/gh-aw ─▶ cli, workflow, parser, console, constants    cmd/gh-aw-wasm ─▶ workflow, parser │
│ cmd/linters ─▶ linters                               cmd/gh-aw-security-model (standalone)   │
├──────────────────────────────────────────────────────────────────────────────────────────────┤
│ CORE PACKAGES                                                                                │
│ cli (commands) ─▶ workflow (compiler) ─▶ parser (Markdown/YAML) ─▶ console (terminal UI)     │
│ cli ─▶ workqueue (queue delivery), agentdrain (session mining), atif (trajectory export)     │
│ cli/workflow ─▶ actionpins (SHA pinning)          workflow ─▶ workqueue, console             │
│ cli ─▶ intent (policy), github, githubapi, modelsdev, scanfindings; cmd/linters ─▶ linters   │
│ workqueue ─▶ parser, console, githubapi          parser ─▶ console, githubapi                │
├──────────────────────────────────────────────────────────────────────────────────────────────┤
│ UTILITIES                                                                                    │
│ cli/workflow/parser ─▶ constants, fileutil, gitutil, logger, stringutil                      │
│ workflow ─▶ setutil, sliceutil, typeutil, validationerror; cli ─▶ envutil, errorutil, stats  │
│ console ─▶ colorwriter, logger, styles, tty; logger ─▶ colorwriter, styles, timeutil         │
│ actionpins ─▶ gitutil, semverutil; agentdrain ─▶ logger, setutil, sliceutil                  │
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
| `agentdrain` | Core | Agent session log mining |
| `atif` | Core | Session JSONL to ATIF trajectory conversion |
| `cli` | Core | Command implementations |
| `console` | Core | Terminal UI rendering |
| `github` | Core | GitHub label and objective mapping |
| `githubapi` | Core | GitHub API client options and requests |
| `intent` | Core | Intent policy resolution |
| `linters` | Core | Custom Go analyzers |
| `modelsdev` | Core | Model catalog and provider normalization |
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
