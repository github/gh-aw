# ADR-51783: Pure-Function Test Suites with Purity Assertions

**Date**: 2026-08-10
**Status**: Draft
**Deciders**: PureLock automation (pelikhan)

---

### Context

PureLock automated analysis identified two pure Go functions in `pkg/cli` with 0% test coverage: `removeUnsafeEngineEnvKeys` (a YAML-frontmatter line-based state machine that strips unsafe `engine.env:` keys) and `migrateMessagesEffectiveTokensSuffixToAICreditsSuffix` (a single-pass rewriter that migrates `{effective_tokens_suffix}` placeholders to `{ai_credits_suffix}` within `safe-outputs.messages:` blocks). Both functions are non-trivial: they implement multi-state YAML parsers that track block nesting, handle scalar and block-scalar values, skip blank lines and comments, and exit cleanly when they cross block boundaries. The absence of any test coverage made regressions undetectable by CI.

### Decision

We will test pure functions using a **two-layer test pattern**: a primary table-driven subtest suite that covers every meaningful branch of the state machine using YAML-line fixtures, and a dedicated purity test that asserts no input slice is mutated and that repeated invocations with identical inputs return identical results. This approach was applied to both `removeUnsafeEngineEnvKeys` and `migrateMessagesEffectiveTokensSuffixToAICreditsSuffix`.

### Alternatives Considered

#### Alternative 1: Integration tests via the codemod command infrastructure

Exercise the functions indirectly by constructing real workflow YAML files and invoking the top-level codemod command. This would provide realistic end-to-end coverage but requires filesystem setup, command plumbing, and expensive test infrastructure. It cannot easily enumerate every internal state-machine branch in isolation, and the signal-to-noise ratio for pinpointing which branch a failure exercises is low.

#### Alternative 2: Fuzzing with `go test -fuzz`

Use Go's native fuzzer to discover edge cases automatically. The PR body explicitly evaluated and rejected this: the line-oriented state machines achieve full branch coverage with a carefully chosen set of table fixtures, and the exhaustive fixture set provides clearer failure messages than a corpus-based fuzzer. Fuzzing would be redundant once full branch coverage is confirmed.

### Consequences

#### Positive
- Coverage jumps from 0% to 93.8% (`removeUnsafeEngineEnvKeys`) and 100% (`migrateMessagesEffectiveTokensSuffixToAICreditsSuffix`), providing a CI safety net for regression.
- The purity test acts as a machine-enforced contract: any future change that introduces input mutation or non-determinism will fail a test immediately.
- Table-driven fixtures are self-documenting — each subtest name describes a distinct state-machine scenario, making the expected behavior readable without consulting the implementation.

#### Negative
- Tests are coupled to the line-based implementation strategy. If the parser is replaced with a proper YAML library, all fixture tests will require significant rewriting.
- The two-layer pattern (behavioral tests + purity test) adds boilerplate per function; this overhead scales with the number of pure functions targeted.

#### Neutral
- These tests reside in the `cli` package (same package as the functions under test), giving them access to unexported symbols without an additional export file.
- PureLock identified the coverage gap; this ADR codifies the testing pattern that PureLock-driven PRs should follow for pure functions.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
