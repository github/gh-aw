# ADR-72951: Deduplicate Compiler Tips by Message per Compiler Instance

**Date**: 2026-10-29
**Status**: Proposed
**Deciders**: Unknown (not recorded in PR #72951)

---

### Context

`gh aw compile` emits informational tips (for example, the `permissions.copilot-requests: write` tip for eligible Copilot workflows) to stderr. Before this change, the `Compiler` tracked whether the Copilot requests tip had already been shown with a map keyed by markdown file path (`copilotRequestsTipShown`). That prevented repeat tips for a single workflow, but when one compiler processed many eligible workflows outside batch mode, the same tip appeared once for every workflow, making output harder to read. The compiler already keeps per-instance state, such as warning caches and batch feature-usage counts; batch mode collects the Copilot tip for a single summary notice instead of inline output.

### Decision

The compiler will track tips routed through `(*Compiler).emitTipOnce(filePath, message)` by **exact message text** for the lifetime of a `Compiler` instance, replacing the path-keyed `copilotRequestsTipShown` map with `tipsShown map[string]bool`. This keeps deduplication at the emission point, reuses the existing per-instance state model, and provides a helper that is not tied to a single tip; currently, only the Copilot requests tip uses it. Each distinct message prints at most once per instance, a new compiler can print it again, and quiet mode returns without printing or marking the message as shown. In batch mode, the Copilot tip bypasses this helper and sets `copilotTipNeeded`, even when quiet mode is enabled, for the existing single summary notice.

### Alternatives Considered

#### Alternative 1: Keep path-keyed tracking and deduplicate at the CLI layer

The CLI could collect tips from each workflow compile and deduplicate them before printing, centralizing presentation policy. Keeping path-keyed tracking would still leave tip state in the compiler, however, and the compiler currently writes inline tips directly to stderr. Compared with the helper, this approach needs a new message channel or output interception and would require other compiler callers to implement the same policy.

#### Alternative 2: Use a dedicated boolean flag only for the Copilot requests tip

A single `copilotRequestsTipShown bool` would fix the reported duplication with less state and is sufficient for the current single caller. It would require another flag and emission guard for each additional one-time tip. The message-keyed map trades a small amount of storage for a reusable emission path.

#### Alternative 3: Process-global (package-level) deduplication

A package-level set would deduplicate tips across all compiler instances in the process, which could reduce repetition for callers creating multiple compilers. It would also suppress tips for otherwise independent compiler instances and introduce shared mutable state and test-order dependencies. Per-instance tracking preserves the fresh-compiler behavior covered by this PR's tests.

### Consequences

#### Positive
- The Copilot requests inline tip appears at most once per compiler instance across eligible workflows, reducing stderr noise.
- `emitTipOnce` gives future tips one reusable deduplication path that respects quiet mode.
- Tips stay separate from warning counts, so `ResetWarningCount` between workflows does not show a tip again.

#### Negative
- Deduplication depends on exact message text. If a tip includes workflow-specific text, each variant counts as a different tip and prints separately.
- When the tip is deduplicated, only the first workflow's path appears with it. Users no longer see every workflow that triggered the tip.
- The tip map remains mutable per-instance state without synchronization; the helper does not support concurrent use of the same compiler.

#### Neutral
- In batch mode, the Copilot tip still goes into `copilotTipNeeded` for the single summary notice. That collection now also runs when quiet mode is on in batch mode (`!c.quiet || c.batchMode`); the CLI continues to suppress summary notices for JSON or verbose output.
- Other informational diagnostics, such as the `id-token: write` notice, do not use the helper and are unchanged.
- The [glossary entry for `copilot-requests: none`](../src/content/docs/reference/glossary.md#copilot-org-billing-opt-out-copilot-requests-none) describes the user-facing once-per-run behavior. [Helper tests](../../pkg/workflow/compiler_error_formatter_test.go), [permission-validator tests](../../pkg/workflow/compiler_validators_test.go), and [integration tests](../../pkg/workflow/copilot_requests_tip_test.go) cover message identity, warning resets, quiet mode, fresh compilers, and batch collection.

---

This record documents the implementation in [PR #72951](https://github.com/github/gh-aw/pull/72951). Proposed status does not assert maintainer approval; the alternatives above evaluate tradeoffs rather than claim a recorded historical deliberation.
