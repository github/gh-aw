# ADR-76903: Serialize Workflow Names as Go-Quoted YAML Scalars

**Date**: 2026-11-11
**Status**: Draft
**Deciders**: [TODO: verify] AI-generated draft for PR #76903 (PR author `app/copilot-swe-agent`, triggering actor `pelikhan`)

---

### Context

The compiler writes the workflow `name` from frontmatter into generated lock files inside a hand-built double-quoted YAML scalar (`name: "%s"`). The default `run-name` is built with the same manual quoting, and `GH_AW_INFO_WORKFLOW_NAME` carries the same value into job metadata. A valid name containing quotation marks, backslashes, or line breaks therefore produced invalid YAML or changed the meaning of the generated mapping (issue #76901). Lock files for ordinary names must stay unchanged, and name content must never alter the surrounding top-level keys.

### Decision

We will emit the workflow `name`, the default `run-name`, and `GH_AW_INFO_WORKFLOW_NAME` with Go's `%q` verb, which produces a double-quoted YAML scalar, instead of manual `"%s"` quoting. This matches the existing `writeYAMLEnv` and `formatYAMLEnv` helpers, and it escapes quotes, backslashes, line breaks, and control characters. The primary driver is correctness and injection safety. Explicit `run-name` values are not changed.

### Alternatives Considered

#### Alternative 1: Reuse or extend `escapeForYAMLDoubleQuoted`

The repository already has `escapeForYAMLDoubleQuoted` in `pkg/workflow/expression_parser.go`. It is the closest option, and the issue suggested a shared YAML-aware helper. It escapes only `\`, `"`, LF, CR, and tab, so other control characters would pass through raw. Extending it would duplicate the escape table that `%q` already provides. This was a close call, and the team should confirm the choice during review.

#### Alternative 2: Serialize through a general YAML marshaller

A YAML library could encode each value. The marshaller chooses plain, single-quoted, or block style based on content, so ordinary names may no longer match the current `name: "..."` form. [TODO: verify] This was rejected to avoid lock-file churn across every compiled workflow.

#### Alternative 3: Reject names that contain quotes or line breaks

Compilation could fail for unsafe characters. This was rejected because the issue expects valid frontmatter strings to round-trip. Rejecting titles such as `Example "quoted" workflow` would block legitimate names. The PR's tests still check field-specific errors for non-scalar `name` and `run-name` values.

### Consequences

#### Positive
- Names with quotes, backslashes, line breaks, Unicode, or YAML-significant punctuation are preserved as a single scalar in `name`, `run-name`, and `GH_AW_INFO_WORKFLOW_NAME`.
- The three emitters use the same `%q` convention as `writeYAMLEnv` and `formatYAMLEnv`, so these values share one escaping approach.
- Regression tests parse the generated lock files and check decoded values and the top-level key set, which guards against regressions.
- Ordinary ASCII names produce the same output as before (`name: "Example workflow"`).

#### Negative
- Lock files for names with backslashes, control characters, or non-printable Unicode change textually on recompilation. Names with backslashes now decode to the literal characters the author wrote.
- `%q` depends on Go's escape syntax being accepted by YAML double-quoted scalars. This holds for the tested classes of input, but it remains an assumption. The same caveat is recorded in ADR-71803.
- Only the three emitters in this PR change. Other manual `"%s"` emitters remain, for example the concurrency group in `pkg/workflow/concurrency.go` and `GH_AW_INFO_ENGINE_ID` in `pkg/workflow/compiler_yaml_step_lifecycle.go`. A follow-up audit is needed.

#### Neutral
- Explicit `run-name` handling is unchanged.
- The code change is three single-line format-string edits. The test change adds 130 table-driven lines to `pkg/workflow/yaml_encoding_test.go`.
- Related ADRs: ADR-71803 uses the same `%q` approach for compiler-generated literals, and ADR-37706 quotes env scalars that contain `: ` through post-hoc YAML text patching.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
