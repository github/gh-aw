# ADR-78157: Add JSON Decoder Trailing-Data Linter

**Date**: 2026-11-16
**Status**: Draft
**Deciders**: Unknown (draft generated from PR #78157; author to confirm)

---

### Context

gh-aw enforces project conventions through a suite of custom `golang.org/x/tools/go/analysis` analyzers under `pkg/linters/`, each registered in `pkg/linters/registry.go`. A `json.Decoder.Decode` call reads one JSON value and returns without error even when more data follows, so a decoder built on an in-memory reader (`strings.NewReader` or `bytes.NewReader`) silently accepts trailing content unless the caller issues a second `Decode` and checks for `io.EOF`. The PR body reports a code-pattern scan that found many in-memory decoder call sites with only a first `Decode`, and the existing `jsonunmarshalbytesconversion` guidance already asks for the same follow-up EOF check. The check must stay off generic `io.Reader` and HTTP body decoders, where a single read is often correct, and must honor generated-file skipping and `//nolint` like sibling analyzers.

### Decision

We will add a standalone analyzer, `jsondecodertrailingdataunchecked` (`pkg/linters/jsondecodertrailingdataunchecked`), that reports the first `Decode` on a `json.Decoder` created from an inline `strings.NewReader` or `bytes.NewReader` call when the same function does not follow it with a `Decode` whose error is compared against `io.EOF`. The primary driver is rejecting trailing JSON data in whole-document inputs, and scoping the rule to in-memory readers keeps false positives low. The analyzer is registered in `allAnalyzers`, documented in `README.md` and `doc.go`, and listed in `notYetEnforced` in `doc_sync_test.go`, so it is not enforced in CI until existing findings are reviewed.

### Alternatives Considered

#### Alternative 1: Migrate whole-document inputs to `json.Unmarshal`

`json.Unmarshal` already rejects trailing data, so moving single-document call sites to it would remove the defect without a new analyzer. It was not chosen as the sole remedy because the migration is manual and nothing would stop new `NewDecoder` call sites from regressing. This was a close call: call-site migration is still a reasonable follow-up for inputs that are always a single document, and `json.Unmarshal` is not suitable where the input is a true stream. [TODO: verify how many flagged sites are single-document inputs.]

#### Alternative 2: Flag every single `Decode` on any reader

A broader analyzer would also cover `io.Reader` sources and HTTP response bodies. It was rejected because those readers are often consumed incrementally or carry several values, so the false-positive rate would be high. The PR excludes them explicitly through the `strings.NewReader` and `bytes.NewReader` source check.

#### Alternative 3: Rely on code review and documentation

Documenting the EOF-check pattern without a mechanical guard is the cheapest option. It was rejected because the scan already found many unguarded call sites, and documentation alone does not prevent regressions.

### Consequences

#### Positive
- In-memory JSON decoders that silently ignore trailing data become visible to the linter suite.
- The analyzer uses the shared helpers (`analyzerutil`, `filecheck`, `nolint`), so generated-file skipping and `//nolint:jsondecodertrailingdataunchecked` behave like sibling analyzers.
- Scoping to `strings.NewReader` and `bytes.NewReader` keeps the rule off generic and HTTP readers.
- Registration, `README.md`, `doc.go`, and `spec_test.go` are checked by existing sync tests, which limits documentation drift.

#### Negative
- The analyzer is not enforced in CI at introduction (`notYetEnforced`), so existing findings do not block merges until an enforcement-readiness audit is complete. [TODO: verify the current finding count across the repository.]
- The analyzer counts call sites, not executions. A single `Decode` inside a loop (for example, one guarded by `dec.More()`) is reported even when the input is intentionally a stream, so such code needs `//nolint` or restructuring.
- Detection is local to one function body. A decoder passed to a helper, or an EOF check that lives in another function, is not recognized.
- Only an inline `json.NewDecoder(strings.NewReader(...))` or `json.NewDecoder(bytes.NewReader(...))` is tracked. A decoder built from a reader held in a variable is missed.
- One more analyzer pass runs on every lint invocation, a small increase in lint time.

#### Neutral
- Each new analyzer requires updates in four places (registry, `README.md`, `doc.go`, `spec_test.go`), plus a `notYetEnforced` entry in `doc_sync_test.go`, the same as its siblings.
- The `jsonunmarshalbytesconversion` documentation already points to the same EOF-check pattern, so the guidance across the two analyzers is aligned.
- Test coverage uses `analysistest` with `testdata` covering flagged, accepted (`io.EOF` comparison and `errors.Is`), excluded, and `//nolint`-suppressed cases.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
