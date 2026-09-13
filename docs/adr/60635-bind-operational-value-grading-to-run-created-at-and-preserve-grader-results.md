# ADR-60635: Bind Operational-Value Grading to Run Created-At and Preserve Grader Results

**Date**: 2026-09-13
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

PR #60635 fixes two boundaries in gh-aw operational-value grading and reporting. The PR description and diff show that the grader could receive `run.createdAt: null` and still issue a grading request, while `gh aw logs --artifacts graders` republished grader results through a lossy serialization path that dropped fields such as `source`, `implementation`, `observation`, `diagnostics`, and baseline data, and could rewrite the metric unit from `ratio` to `count`. The implementation spans the JS Actions runtime, the grader execution path, the usage summary artifact, and the Go audit-report reader. The architectural question is how gh-aw should treat required run metadata and grader-result transport when operational-value results are consumed across jobs and CLI surfaces.

### Decision

We will require an authoritative workflow run creation timestamp for operational-value grading and normalize it before use, with fallback acquisition from Actions run metadata and an explicit grader error when the timestamp cannot be obtained. We will also preserve the full grader result contract end-to-end by embedding `grader_results.json` in the usage summary artifact and by keeping `source`, `implementation`, `observation`, `diagnostics`, baseline fields, and the grader-declared `unit` intact when `gh aw logs` serializes results. We decided this because downstream consumers must be able to distinguish missing evidence from transport loss, and operational-value requests must never be built from missing or malformed run metadata.

### Alternatives Considered

#### Alternative 1: Keep best-effort grading when `run.createdAt` is missing

The existing path effectively allowed the grader loop to proceed when created-at acquisition failed, leaving the grader to operate on an invalid or incomplete request. This was considered because it minimizes coordination between activation, grading, and fallback metadata lookup. It was not chosen because the PR evidence shows that this behavior silently degrades correctness and makes acquisition failures indistinguishable from a legitimate unavailable grade.

#### Alternative 2: Preserve only the current summarized grader fields in `gh aw logs`

Another option was to keep the current compact serialization that exposes status and a small subset of result fields. It was considered because a summarized contract is simpler to maintain across artifacts and schemas. It was not chosen because the diff and PR rationale show that important operational-value semantics are lost in transit, including provenance, diagnostics, baseline comparisons, and even the declared unit, which breaks downstream interpretation.

#### Alternative 3: Require only the standalone grader artifact and not the usage summary fallback

The CLI could continue reading only `grader_results.json` from the grader artifact and fail or omit data when that file is unavailable. It was considered because it avoids duplicating grader data into the usage summary. It was not chosen because the PR explicitly adds an embedded `graders` section in `activity/summary.json` so compact usage artifacts remain sufficient for downstream reporting.

### Consequences

#### Positive
- Operational-value graders now fail explicitly when run creation time cannot be resolved, preventing invalid grading requests.
- Run creation timestamps are normalized to the strict UTC format the grader contract expects, reducing cross-boundary timestamp drift.
- `gh aw logs` and cached JSONL records preserve the complete grader-result contract, including diagnostics, provenance-bearing observation data, and baseline fields.
- The usage summary artifact becomes a reliable fallback source for grader results when the standalone grader artifact is missing.

#### Negative
- The implementation adds shared acquisition and retry logic across multiple JS entry points, increasing runtime and test complexity.
- Embedding full grader results in the usage summary enlarges artifact payloads and schema surface area.
- Consumers that previously tolerated lossy grader serialization may need to handle richer objects and explicit error states.

#### Neutral
- The change introduces a dedicated shared JS module for run-created-at acquisition and normalization.
- Go audit-report parsing now maps freer-form payloads with object fields instead of relying on narrower raw-message handling for all nested result data.
- Documentation and schemas are updated to make created-at acquisition and grader-result preservation normative parts of the contract.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
