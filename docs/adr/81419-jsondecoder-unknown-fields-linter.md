# ADR-81419: Flag JSON Decoders That Accept Unknown Fields with a Custom Linter

**Date**: 2026-11-26
**Status**: Draft
**Deciders**: Not stated in PR evidence (PR author: gh-aw-bot)
**Related**: [PR #81419](https://github.com/github/gh-aw/pull/81419), [Issue #81418](https://github.com/github/gh-aw/issues/81418) (closing reference in PR body)

## Context

`encoding/json` decoders accept unknown object fields by default. As the PR body states, this can hide schema drift and silently ignore input that a caller expects to be validated. The gap is invisible at review time because the decode call looks correct unless the reviewer knows to check for a `DisallowUnknownFields()` call.

Forces:

- The repository already runs a related decoder analyzer, `jsondecodertrailingdataunchecked`, which checks in-memory decoders for trailing data. A new analyzer fits the same pattern and the same registry.
- Every analyzer must be registered, documented in `pkg/linters/README.md` and `pkg/linters/doc.go`, and listed in `spec_test.go`. Sync tests enforce these lists.
- New analyzers can be held back from CI enforcement. The `notYetEnforced` map in `pkg/linters/doc_sync_test.go` is the mechanism for this.
- The linked issue #81418 reports a code-pattern scan of roughly 71 decoder uses against 11 `DisallowUnknownFields` uses. This figure comes from the issue body and was not verified in this gate.

## Decision

Add a custom `go/analysis` analyzer, `jsondecoderunknownfields`, that reports each `json.NewDecoder` call whose decoder does not have `DisallowUnknownFields()` called on it before its first `Decode`. The change:

1. Identifies `NewDecoder` calls by package path, so import aliases are covered (`jsonalias` in the testdata). It tracks decoders bound to local identifiers and decoders used inline.
2. Accepts a `DisallowUnknownFields` call only when it appears before the first `Decode` and before any reassignment of the same identifier. The check is based on source position (`isStrictBeforeDecode` in `jsondecoderunknownfields.go`).
3. Skips generated files and honors `//nolint:jsondecoderunknownfields`.
4. Is registered in `pkg/linters/registry.go`, and documented in `pkg/linters/README.md`, `pkg/linters/doc.go` (now "All 105 active analyzers"), and `spec_test.go`.
5. Is recorded in the `notYetEnforced` map in `pkg/linters/doc_sync_test.go` with the reason "new JSON decoder schema-drift analyzer needs an enforcement-readiness audit before CI enables it". The analyzer is therefore not yet enforced in CI.

## Alternatives Considered

1. **Enforce the analyzer in CI immediately.** Not chosen in this PR. The `notYetEnforced` entry records that enforcement readiness has not been audited. Enforcing before the audit would make CI fail on existing decoders that have not been reviewed.
2. **Use control-flow-aware or interprocedural analysis.** Not chosen in this PR. The implementation uses source-position ordering and identifier matching instead of a control-flow graph. The PR gives no explicit rationale for this choice. The trade-off is simpler code and fewer false negatives on straight-line code, at the cost of the limitations listed under Consequences.

## Consequences

Positive:

- Decoders that silently accept unknown input fields become visible at lint time. `//nolint:jsondecoderunknownfields` gives reviewers a recorded, scoped escape hatch.
- The analyzer has `analysistest` coverage for strict decoders, default decoders, aliased imports, reassigned decoders, inline decoders, post-decode strictness, unused decoders, suppression, and generated files. It is also registered and documented alongside the existing analyzers, so the sync tests guard it.

Negative:

- The check is position-based, not control-flow-aware. A `DisallowUnknownFields()` call inside a conditional branch that precedes `Decode` satisfies the check. A decoder not bound to a local identifier (for example, a struct field or a chained call) is always reported, even when it is strict.
- Because the analyzer is listed in `notYetEnforced`, CI does not enforce it. This PR does not remediate existing decoders that accept unknown fields, so the schema-drift risk remains until the enforcement audit is complete.
