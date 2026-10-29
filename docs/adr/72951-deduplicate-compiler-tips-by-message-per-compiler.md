# ADR-72951: Deduplicate Compiler Tips by Message per Compiler Instance

**Date**: 2026-10-29
**Status**: Draft
**Deciders**: pelikhan [TODO: verify]

---

### Context

`gh aw compile` emits informational tips (for example, the `permissions.copilot-requests: write` tip for Copilot workflows) to stderr. Before this change, the `Compiler` tracked whether the Copilot requests tip had already been shown with a map keyed by markdown file path (`copilotRequestsTipShown`). That prevented repeat tips for a single workflow, but when one compile run processed many Copilot workflows, the same tip appeared once for every workflow. That noise made the output harder to read. The compiler already keeps per-instance state, such as warning caches and batch feature-usage counts, and runs sequentially within a single invocation.

### Decision

We will track emitted tips by **message text** for the lifetime of a `Compiler` instance. To do this, we will replace the path-keyed `copilotRequestsTipShown` map with a general `tipsShown map[string]bool` and a `(*Compiler).emitTipOnce(filePath, message)` helper. Each distinct tip prints at most once per compiler instance, and a new compiler can print it again. Quiet mode still suppresses inline tips. In batch mode, the compiler records the Copilot billing tip in `copilotTipNeeded` so it appears once in the summary instead of inline. This is the simplest fix because it reuses the existing per-compiler state model and is not tied to a single tip.

### Alternatives Considered

#### Alternative 1: Keep path-keyed tracking and deduplicate at the CLI layer

The CLI could collect tips from each workflow compile and deduplicate them before printing. This would keep the `Compiler` stateless about tips across workflows. We did not choose it because the compiler writes tips directly to stderr. Moving them to the CLI would require a new message channel and would affect every caller of the compiler.

#### Alternative 2: Use a dedicated boolean flag only for the Copilot requests tip

A single `copilotRequestsTipShown bool` would fix the reported duplication with a minimal change. We did not choose it because every future tip would need its own flag. A message-keyed map plus a shared helper handles all tips with one mechanism.

#### Alternative 3: Process-global (package-level) deduplication

A package-level set would deduplicate tips across all compiler instances in the process. We did not choose it because it adds global mutable state. That makes tests order-dependent and stops a new compiler from showing the tip again, which this PR explicitly preserves.

### Consequences

#### Positive
- Each distinct tip appears once per compile run, even when compiling many workflows, which reduces stderr noise.
- `emitTipOnce` gives future tips one reusable deduplication path that respects quiet mode.
- Tips stay separate from warning counts, so `ResetWarningCount` between workflows does not show a tip again.

#### Negative
- Deduplication depends on exact message text. If a tip includes workflow-specific text, each variant counts as a different tip and prints separately.
- When the tip is deduplicated, only the first workflow's path appears with it. Users no longer see every workflow that triggered the tip.
- `Compiler` gains more mutable per-instance state, which is not goroutine-safe. This matches the existing assumption that the compiler runs sequentially.

#### Neutral
- In batch mode, the Copilot tip still goes into `copilotTipNeeded` for the single summary notice. That collection now also runs when quiet mode is on in batch mode (`!c.quiet || c.batchMode`).
- The glossary entry for `copilot-requests: none` was updated to say the tip appears once per compile run.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
