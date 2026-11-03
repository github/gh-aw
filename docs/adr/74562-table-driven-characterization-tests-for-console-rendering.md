# ADR-74562: Table-Driven Characterization Tests for Console Rendering and Number Formatting

**Date**: 2026-11-03
**Status**: Draft
**Deciders**: [TODO: verify] pelikhan, Copilot coding agent

---

### Context

`pkg/console` renders CLI output by reflecting over structs, slices, and maps (`RenderStruct`, `RenderStructStdout`, `RenderStructWithOptions`). It also formats counts for display (`FormatNumber`, `FormatTokens`). Before this change, `FormatTokens` had no direct tests. The `RenderOptions` `MaxWidth` and `Stderr` options had no direct tests either. Reflection edge cases were also untested: nil values, scalars, channels, functions, and unexported fields. The embedded-struct tests were split across several near-duplicate functions with linear assertions, so one failure could hide others. Issue #74185 asked for better test quality in this package. The PR is test-only and does not change any production code.

### Decision

We will test `pkg/console` rendering and formatting with table-driven, `t.Parallel()` subtests. Each case will be a named subtest, and we will use `require` for preconditions before indexing into results (for example, `config.Rows[0]`). These tests are characterization tests: they pin the current behavior of the formatters and renderers as the contract. That includes these edge cases:

- `FormatTokens(0)` returns `"-"`.
- Negative inputs to `FormatNumber` and `FormatTokens` are returned unabridged, for example `"-1000000"`.
- Values just below a unit boundary round up within the smaller unit, for example `FormatTokens(999_999)` returns `"1000.0K"`.
- Values of one billion or more stay in millions, for example `FormatTokens(1_000_000_000)` returns `"1000.0M"`.
- With `MaxWidth` set, no rendered line is wider than that limit (measured with `lipgloss.Width`), and wrapping keeps the full value.

The main reason is regression safety at low cost. Named, parallel subtests isolate failures. A pinned contract makes any future change to the formatting rules a deliberate, visible test change.

### Alternatives Considered

#### Alternative 1: Keep separate linear test functions per scenario

We could have kept separate functions such as `TestRenderStruct_EmbeddedStruct`, `TestRenderStruct_EmbeddedStructOmitEmptyAndPointer`, and `TestRenderSlice_SkippedEmbeddedStruct`, and added more functions for the new cases. This needs less restructuring and keeps each scenario's custom assertion messages. We did not choose it because it duplicates type declarations and assertion logic, and because a failed assertion partway through a function hides the checks that follow.

#### Alternative 2: Specify the intended formatting behavior and fix the formatters

Instead of pinning current output, we could have defined the behavior we want and changed `render.go` to match. For example, negatives could be abbreviated symmetrically (`-1.0K`), `999_999` could promote to `1.0M`, and a billion unit could be added. We did not choose this because the PR is scoped as a test-only quality improvement (issue #74185), and changing user-visible CLI output would need its own review. [TODO: verify whether the current negative and boundary outputs are intended or just tolerated.]

#### Alternative 3: Golden-file snapshot tests for rendered output

Full rendered output could be compared against checked-in golden files. That catches every visual change, but the tests become brittle when styling, borders, or terminal width handling changes. Targeted assertions such as `Contains`, column order, and line width express the actual contract more precisely.

### Consequences

#### Positive
- `FormatTokens`, the `RenderOptions` `MaxWidth` and `Stderr` options, and reflection edge cases now have direct coverage.
- Named parallel subtests report each failing case on its own and run concurrently, so failures are easier to diagnose.
- `require` preconditions turn out-of-range index panics into clear test failures.
- Consolidating the embedded-struct tests into tables removes duplicated type declarations and assertion code.

#### Negative
- Pinning current output turns behavior that may be accidental into a tested contract: unabridged negatives, `"1000.0K"` just below one million, and `"1000.0M"` for billions. Anyone who later improves the formatting must update these tests on purpose.
- The table-driven tests use generic assertion messages (for example, `assert.Contains(t, output, text)`), so failures carry less per-scenario detail than the custom messages they replace.
- Nested subtests for destination × width × data shape are harder to read than flat tests.
- `TestRenderStruct_UnexportedFields` asserts an exact output string, so any change to padding or alignment breaks it.

#### Neutral
- The test file now imports `charm.land/lipgloss/v2` to measure rendered line width. This is already a dependency of the package.
- No production code changes; runtime behavior is unchanged.
- The table-driven, parallel-subtest pattern matches existing conventions in the repository's Go tests. [TODO: verify whether to adopt it as a package-wide convention.]

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
