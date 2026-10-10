# ADR-67423: Enforce Destination-Aware Console Formatters with a Dedicated Analyzer

**Date**: 2026-10-10
**Status**: Draft
**Deciders**: pelikhan (PR author), gh-aw maintainers

---

### Context

`pkg/console` exposes two families of formatters: stdout-aware variants (`FormatError`, `FormatInfoMessage`, `RenderTable`, …) and stderr-aware `*Stderr` variants. Across `pkg/cli`, `pkg/workflow`, and `pkg/parser`, many call sites wrote stdout-aware formatter output directly to `os.Stderr` via `fmt.Fprint*`, so styling and stream-detection decisions (color degradation, TTY checks) were computed for the wrong destination. Related findings also showed non-TTY tables ignoring any width budget and duplicated literal semantic markers (for example a hand-written `Error:` prefix in front of an already-prefixed formatter). Previous partial fixes (#41773, #47108, #42945, #50850, #64336) added helpers and migrated some call sites, but nothing prevented regressions. Issue #67143 (source #66976) required that formatter stream usage be verified by a regression test and guarded by a lint rule.

### Decision

We will make console stream selection an explicitly enforced contract: every direct write of console-formatted text to stderr must use a destination-aware `*Stderr` formatter, and a new custom static analyzer, `pkg/linters/consolestderr`, blocks CI when a stdout-aware formatter is written directly to `os.Stderr`.

The analyzer carries a fixed stdout→stderr replacement table and emits suggested fixes, so the ~1,600 selector substitutions across 241 files are mechanical and reproducible rather than hand-audited. To complete the contract, we add the missing stderr renderers (prompt, verbose, table, reflected struct), propagate the destination through nested tables and error-chain styling, and exempt already-stderr-aware helpers such as `FormatErrorMessage`. Separately, human-facing experiment tables gain an **opt-in** width budget (`TableConfig.MaxWidth`, `DefaultTableWidth = 80`) with a lossless labeled-row fallback when columns cannot fit; JSON, version, completion, compact-log and WASM renderers keep their existing unbounded/tab-separated contracts.

### Alternatives Considered

#### Alternative 1: Runtime stream assertion instead of static analysis

Make the formatters themselves detect their destination at runtime (for example by threading an `io.Writer` into every formatter and deriving styling from it), so a "wrong stream" call is impossible by construction. This was attractive because it would remove the duplicated `*Stderr` API surface entirely. It was not chosen because it would change the signature of nearly every console formatter and every call site simultaneously, breaking the string-returning API that callers compose into larger messages, and it would give no compile-time or CI signal for the remaining `fmt.Fprint*` patterns.

#### Alternative 2: Convention plus a grep/regex CI check

Document the stdout/stderr rule and enforce it with a `grep`-based CI script over `fmt.Fprint*(os.Stderr, …)` call sites. This is far cheaper to build than a `go/analysis` analyzer. It was rejected because the incorrect pattern appears in nested, aliased-import, and multi-argument forms that a regex cannot classify without false positives, and because a text-level check cannot produce the suggested fixes that make a 241-file migration tractable.

#### Alternative 3: Universal table width budget

Apply the 80-column budget to all table rendering rather than making it opt-in. Rejected because JSON, version, completion and compact-log output are machine- or pipe-consumed; wrapping them would silently change existing output contracts. The budget is therefore limited to human experiment tables.

### Consequences

#### Positive
- Stream/styling decisions now match the actual destination, so color degradation and TTY detection behave correctly for stderr output.
- The `consolestderr` analyzer is part of the blocking production CI analyzer set, so the fixed pattern cannot silently regress; suggested fixes make future migrations mechanical.
- Human experiment tables respect a width budget without data loss, via the labeled-row fallback.
- Redundant literal semantic markers at diagnostic call sites were removed, so messages no longer double-prefix.

#### Negative
- The `pkg/console` API surface grows a parallel `*Stderr` variant for each formatter, which must be kept in sync; the replacement table in the analyzer is a third place to update when a formatter is added.
- The change touches 241 files mechanically, making the diff hard to review line-by-line and likely to conflict with in-flight branches.
- The analyzer deliberately does not perform dataflow analysis, so writes through intermediate writer/formatter aliases are not covered — the guarantee is "no direct mismatches", not "no mismatches".
- Enabling `consolestderr` in the blocking set surfaces pre-existing legacy/advisory findings in the mechanically touched files, which remain unaddressed in this PR.

#### Neutral
- WASM builds keep tab-separated tables; width budgeting applies only to native human output.
- The ADR covers the stream contract and the opt-in width budget together because both derive from the same console-rendering root cause in #67143.
- `TableConfig.MaxWidth` defaults to `0` (unbounded), so existing callers are unaffected until they opt in.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
