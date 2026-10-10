# ADR-67423: Enforce Destination-Aware Console Formatters with a Dedicated Analyzer

**Date**: 2026-10-10
**Status**: Proposed
**Deciders**: pelikhan (PR author), gh-aw maintainers

---

### Context

`pkg/console` exposes two families of formatters: stdout-aware variants (`FormatError`, `FormatInfoMessage`, `RenderTable`, …) and stderr-aware `*Stderr` variants. Across `pkg/cli`, `pkg/workflow`, and `pkg/parser`, many call sites wrote stdout-aware formatter output directly to `os.Stderr` via `fmt.Fprint*`, so styling and stream-detection decisions (color degradation, TTY checks) were computed for the wrong destination. Related findings also showed non-TTY tables ignoring any width budget and duplicated literal semantic markers (for example a hand-written `Error:` prefix in front of an already-prefixed formatter). Previous partial fixes (#41773, #47108, #42945, #50850, #64336) added helpers and migrated some call sites, but nothing prevented regressions. Issue #67143 (source #66976) required that formatter stream usage be verified by a regression test and guarded by a lint rule.

### Decision

We will correct the behavior of the existing diagnostic formatter names rather than rename their callers: `FormatError`, message formatters, `RenderTable`, and `RenderStruct` now consult stderr. Existing `*Stderr` entry points remain compatible aliases. Intentional stdout callers use explicit `*Stdout` entry points, preserving their previous terminal/color behavior and data contracts. Formatter signatures and actual write destinations do not change.

The initial implementation mechanically renamed approximately 1,600 diagnostic calls across 241 files. On the author's direction, that migration is replaced by the API behavior change, eliminating bulk call-site churn. The `consolestderr` analyzer now checks both stdout and stderr writes, suggesting the appropriate default or explicit stdout entry point. The options-based `RenderStructWithOptions` keeps its explicit destination contract: `Stderr: true` selects stderr, and false or omitted selects stdout. Unknown option variables receive a diagnostic without an unsafe automatic edit. Nested tables and error-chain styling use the selected destination. Separately, human-facing experiment tables gain an **opt-in** width budget (`TableConfig.MaxWidth`, `DefaultTableWidth = 80`) with a lossless labeled-row fallback when columns cannot fit; JSON, version, completion, compact-log and WASM renderers keep their existing unbounded/tab-separated contracts.

### Alternatives Considered

#### Alternative 1: Rename every diagnostic call to a stderr variant

The initial implementation used the existing `*Stderr` APIs at every proven direct stderr write. Replaced on the author's direction because fixing the API default achieves the same behavior with much less churn. The smaller set of intentional stdout callers must still opt into stdout styling so redirected output remains correct.

#### Alternative 2: Runtime stream assertion instead of static analysis

Make the formatters themselves detect their destination at runtime (for example by threading an `io.Writer` into every formatter and deriving styling from it), so a "wrong stream" call is impossible by construction. This was attractive because it would remove the duplicated `*Stderr` API surface entirely. It was not chosen because it would change the signature of nearly every console formatter and every call site simultaneously, breaking the string-returning API that callers compose into larger messages, and it would give no compile-time or CI signal for the remaining `fmt.Fprint*` patterns.

#### Alternative 3: Convention plus a grep/regex CI check

Document the stdout/stderr rule and enforce it with a `grep`-based CI script over standard-stream writes. It was rejected because the incorrect pattern appears in nested, aliased-import, and multi-argument forms that a regex cannot classify reliably.

#### Alternative 4: Universal table width budget

Apply the 80-column budget to all table rendering rather than making it opt-in. Rejected because JSON, version, completion and compact-log output are machine- or pipe-consumed; wrapping them would silently change existing output contracts. The budget is therefore limited to human experiment tables.

### Consequences

#### Positive
- Stream/styling decisions now match the actual destination, so color degradation and TTY detection behave correctly for stderr output.
- The `consolestderr` analyzer is part of the blocking production CI analyzer set, so the fixed pattern cannot silently regress; suggested fixes make future migrations mechanical.
- Human experiment tables respect a width budget without data loss, via the labeled-row fallback.
- Redundant literal semantic markers at diagnostic call sites were removed, so messages no longer double-prefix.

#### Negative
- The API surface includes explicit stdout variants and compatible stderr aliases; the destination map in the analyzer must be updated when a formatter is added.
- The existing default names change styling behavior for callers that intentionally write to stdout. Repository stdout callers have been migrated to explicit variants; downstream callers need the same opt-in.
- The analyzer deliberately does not perform dataflow analysis, so writes through intermediate writer/formatter aliases are not covered — the guarantee is "no direct mismatches", not "no mismatches".
- The all-custom-analyzer local gate reports pre-existing legacy/advisory findings in mechanically touched files, plus an advisory false positive on explicitly bounds-guarded options indexing. These remain separate from the blocking production CI selection, which includes `consolestderr` and passes.

#### Neutral
- WASM builds keep tab-separated tables; width budgeting applies only to native human output.
- The ADR covers the stream contract and the opt-in width budget together because both derive from the same console-rendering root cause in #67143.
- `TableConfig.MaxWidth` defaults to `0` (unbounded), so existing callers are unaffected until they opt in.

---

The decision rationale and alternatives are complete for review. Maintainers
retain responsibility for accepting this proposed decision before merge.
