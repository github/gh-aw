# Architecture Diagram

> Last updated: 2026-11-23 · Source: [Workflow run](https://github.com/github/gh-aw/actions/runs/45140538836)

## Overview

This diagram shows the package structure and dependencies of the `gh-aw` codebase. Arrows point from an importing package to an imported package.

```text
┌────────────────────────────────────────────────────────────────────────────────────────────────┐
│ ENTRY POINTS                                                                                   │
│ cmd/gh-aw ─▶ cli, workflow, parser, console, constants                                         │
│ cmd/gh-aw-wasm ─▶ workflow, parser                 cmd/linters ─▶ linters                      │
│ cmd/gh-aw-security-model (standalone conformance runner)                                       │
├────────────────────────────────────────────────────────────────────────────────────────────────┤
│ CORE PACKAGES  (arrows point from importer to imported package)                                │
│ cli (commands) ─▶ workflow (compiler), parser (frontmatter), console (terminal UI)             │
│ cli ─▶ workqueue (delivery), agentdrain (logs), atif (trajectories), intent (policy)           │
│ workflow ─▶ parser, console, workqueue, actionpins (SHA pins), connectors (declarations)       │
│ workflow ─▶ workflow/{actions,aw,lint,taint,...} (compiler internals)                          │
│ parser ─▶ console, githubapi (API), workflow/actionslanguageservice                            │
│ workqueue ─▶ parser, console, githubapi;  cli ─▶ github, modelsdev, scanfindings               │
│ cli ─▶ lifecycle (safe-output rules and evidence)                                              │
│ cmd/linters ─▶ linters ─▶ linters/* (Go analyzers)                                             │
├────────────────────────────────────────────────────────────────────────────────────────────────┤
│ UTILITIES                                                                                      │
│ cli/workflow/parser ─▶ fileutil, gitutil, logger, stringutil, constants                        │
│ workflow ─▶ setutil, sliceutil, typeutil, validationerror;  parser ─▶ validationerror          │
│ console ─▶ colorwriter, logger, styles, tty;  logger ─▶ colorwriter, styles, timeutil          │
│ actionpins ─▶ gitutil, semverutil;  agentdrain ─▶ logger, setutil, sliceutil                   │
└────────────────────────────────────────────────────────────────────────────────────────────────┘
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
| `atif` | Core | Session trajectory export |
| `cli` | Core | Command implementations |
| `connectors` | Core | Connector declarations |
| `console` | Core | Terminal UI rendering |
| `github` | Core | GitHub label and objective mapping |
| `githubapi` | Core | GitHub API requests |
| `intent` | Core | Intent policy resolution |
| `lifecycle` | Core | Safe-output lifecycle rules and evidence |
| `linters` | Core | Custom Go analyzers |
| `linters/*` | Core | Individual Go analyzer packages |
| `modelsdev` | Core | Model catalog normalization |
| `parser` | Core | Markdown and frontmatter parsing |
| `scanfindings` | Core | Scan finding data model |
| `workflow` | Core | Workflow compilation and generation |
| `workflow/*` | Core | Compiler AST, lint, taint, and policy packages |
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
