# ADR-64175: Preserve version labels on SHA-pinned workflow actions

**Date**: 2026-09-29
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This PR changes how `gh-aw` compiles workflow actions that are already pinned to a full SHA, especially when the source markdown includes an inline version label comment such as `# v2.9.1`. The current behavior can rewrite an unknown SHA-pinned action as `repo@sha # sha`, which introduces a misleading comment and causes downstream audit tooling like zizmor to report a `ref-version-mismatch`. The diff also threads source-version comment data from workflow frontmatter into action pin resolution and adds tests for compiled lock files and manifest output. The decision is how `gh-aw` should preserve or emit labels for already SHA-pinned action references when no embedded pin-table version is available.

### Decision

We will preserve author-provided inline version labels for SHA-pinned workflow actions and otherwise emit a bare `repo@sha` reference when the SHA is not known in the embedded pin table. We will carry those inline labels from workflow frontmatter into workflow compilation and action pin resolution so lock files and manifests can distinguish between authoritative source labels and unlabeled SHA pins. We chose this because it prevents `gh-aw` from fabricating a SHA-as-version comment while still preserving meaningful human-provided version context when it exists.

### Alternatives Considered

#### Alternative 1: Keep formatting unknown SHA pins as `repo@sha # sha`

This was the pre-change behavior and was a realistic option because it requires no new data flow through workflow compilation. It was not chosen because the PR description and tests show that this fabricated comment is misleading and triggers zizmor `ref-version-mismatch` findings.

#### Alternative 2: Always strip comments from SHA-pinned actions

This was a realistic simplification because the compiler could emit `repo@sha` for every already-pinned action regardless of source comments. It was not chosen because the PR explicitly preserves meaningful inline labels like `# v2.9.1`, and the tests verify that source labels should survive compilation when provided by the author.

### Consequences

#### Positive
- Compiled lock files no longer invent SHA-as-version comments for unknown pinned actions.
- Author-supplied inline version labels remain visible in compiled workflow output and manifest data.
- Security and audit tooling receive action references that better reflect the original workflow intent.

#### Negative
- Workflow compilation now depends on comment extraction from frontmatter YAML, which adds parsing complexity.
- Action pinning behavior spans more components because workflow building, pin context propagation, and action resolution must stay consistent.
- Additional regression tests are needed to cover labeled and unlabeled SHA-pinned cases.

#### Neutral
- Manifest output may now report either the preserved label or the raw SHA, depending on whether the source workflow included an inline version comment.
- Existing embedded pin-table labels remain authoritative over source comments when a hardcoded pin entry is known.
- This change affects already SHA-pinned actions rather than the general version-to-SHA pinning flow.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
