# ADR-65263: Export Unified Session Downloads

**Date**: 2026-10-03
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

This pull request adds a new `gh aw sessions download <run-id-or-url>` command so operators can export workflow sessions without generating a full audit report. Recent runs may already publish `usage/aw_session.jsonl`, but older runs often only contain agent artifacts, raw engine logs, or legacy metadata layouts. The implementation must preserve published JSONL byte-for-byte when it already exists, support historical artifact names and nested layouts, and provide both machine-readable JSONL and privacy-preserving Markdown output. The command also needs to work outside a source checkout, so the runtime parser sources used by Actions and audit summaries must be packaged for CLI reuse.

### Decision

We will expose session export as a dedicated `gh aw sessions download` command that first prefers the published `usage/aw_session.jsonl` artifact and falls back to reconstructing the unified session from downloaded agent evidence when the published file is absent. We will reuse the same JavaScript session parsers and renderers already used by audit and workflow summaries by embedding their runtime sources in Go and executing them through a shared CLI adapter. We will validate unified session headers and records before returning output, support both `jsonl` and privacy-preserving `markdown` formats, and only reconstruct data from artifacts that are actually present instead of inventing missing evidence.

### Alternatives Considered

#### Alternative 1: Download only published `aw_session.jsonl`

This would have been the simplest implementation because the CLI could just fetch the usage artifact and stream it to stdout. It was not chosen because the PR explicitly targets historical runs that predate unified session publication and therefore need reconstruction from agent artifacts, engine logs, and legacy metadata layouts.

#### Alternative 2: Build a sessions-specific parser pipeline separate from audit

A dedicated parser stack could have been tailored only for `sessions download` and avoided packaging the current audit parser sources for reuse outside a source checkout. It was not chosen because the PR intentionally shares the existing engine parsers, Markdown renderer, and validation path with audit and workflow summaries so session exports stay consistent across commands and do not fork parsing logic.

### Consequences

#### Positive
- Users can export a unified session directly from a workflow run without generating a full audit report.
- Historical runs remain usable because the command can reconstruct the current unified format from older artifact names, raw engine logs, and legacy directory layouts.
- Session parsing and Markdown rendering stay consistent with audit and workflow summaries because the same runtime sources are reused.

#### Negative
- The CLI now depends on packaging and executing embedded Node.js parser sources, which increases implementation complexity and introduces a runtime requirement for reconstruction and Markdown rendering.
- Session download logic must handle multiple artifact naming conventions, metadata sources, and flattening behaviors, which increases maintenance cost.
- Reconstruction can only report evidence that exists in downloaded artifacts, so older runs may still produce incomplete sessions when key artifacts are missing.

#### Neutral
- Published `aw_session.jsonl` remains the preferred source of truth and is returned byte-for-byte when present.
- Markdown output is intentionally privacy-preserving and bounded, while JSONL remains the complete machine-readable format.
- The command is introduced as experimental, so its interface and output details can evolve as session export needs become clearer.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
