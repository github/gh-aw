# ADR-71894: Share a Single Whitespace-Tolerant Decoder for GitHub Contents Payloads

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: [TODO: verify] Not formally recorded; pending maintainer review of PR #71894.

---

### Context

Several `pkg/cli` readers fetch files through the GitHub Contents API, which returns file bodies as base64 wrapped across multiple lines. Three readers decoded those payloads independently: the branch file reader (`branch_file_reader.go`), the experiment config loader (`experiments_fetch.go`), and the workflow downloader (`download_workflow.go`). Their whitespace handling differed. The downloader only removed `\n` after trimming, so CRLF line endings, tabs, or Unicode whitespace inside the payload made decoding fail. The other two readers removed all whitespace with `strings.Fields`. Because the same input could decode in one reader and fail in another, behavior was harder to predict and the duplicated code could drift further (issue #71714).

### Decision

We will decode GitHub Contents base64 payloads with one shared helper, `decodeBase64FileContent`. It removes all Unicode whitespace (`strings.Join(strings.Fields(raw), "")`) and then calls `base64.StdEncoding`. Every reader calls this helper instead of keeping its own decoding logic. Each caller keeps its own error wrapping, logging, branch selection, and path validation. A malformed payload returns an error and no partial content. Each reader's tests and the decoder's own tests pin this behavior.

### Alternatives Considered

#### Alternative 1: Keep per-reader decoding, but make the whitespace handling the same

Each reader could keep its own two-line decode and copy the same `strings.Fields` normalization. This changes the least code, but the logic would still be duplicated and the readers could drift apart again. That drift is what caused this inconsistency. The helper already existed in `download_workflow.go`, so reusing it cost little.

#### Alternative 2: Strip only the GitHub line-wrapping characters (LF/CRLF)

The decoder could remove only `\r` and `\n`, matching the documented GitHub API wrapping and rejecting any other whitespace. That would be stricter, but the branch and experiment readers already accepted arbitrary whitespace through `strings.Fields`. This option would make those callers stricter than they are today. Removing all whitespace keeps their current behavior and fixes the downloader.

#### Alternative 3: Use a more lenient decoder (e.g. accepting unpadded or partially valid input)

A decoder could accept truncated or unpadded base64, or return the bytes decoded before an error. Some malformed responses would then still produce content, but corrupted workflow or experiment files could be parsed silently. The decision rejects malformed input outright and returns no partial content.

### Consequences

#### Positive
- All three Contents API readers handle the same payload the same way, including CRLF and Unicode whitespace.
- The workflow downloader now decodes CRLF-wrapped or otherwise whitespace-padded payloads that it used to reject.
- Normalization and decoding live in one place, so future fixes apply to every reader.
- New regression tests in each reader and in the decoder cover wrapped, malformed, truncated, empty, and whitespace-only input.

#### Negative
- `branch_file_reader.go` and `experiments_fetch.go` now depend on a helper defined in `download_workflow.go`, which mixes concerns within the file layout. [TODO: verify whether the helper should move to a dedicated file]
- The decoder now also accepts whitespace that GitHub never emits, such as NBSP or em space. Odd input is tolerated instead of reported.
- Error messages from the branch and experiment readers now wrap the helper's `failed to decode file content` text, which changes their error strings slightly.

#### Neutral
- Empty or whitespace-only payloads decode to empty content without an error.
- Tests now pass Contents responses through environment variables to fake `gh` scripts, which changes how fixtures are set up.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
