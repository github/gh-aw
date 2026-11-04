# ADR-74906: Bound ZIP Log Extraction by Total Size and Entry Count

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: Unknown (PR author: Copilot coding agent; reviewers [TODO: verify])

---

### Context

`gh aw logs` downloads workflow-run log archives and extracts them with `unzipFile` in `pkg/cli/logs_download_zip.go`. Before this change, each entry was capped at 1 GB, but the archive as a whole had no limit on total uncompressed size or entry count. A multi-entry archive could therefore exhaust disk space or CPU (CWE-409, code scanning alert 1062, tracked in #74889). ZIP central-directory metadata (`UncompressedSize64`) is supplied by the archive creator and cannot be fully trusted. The existing per-entry guard and zip-slip protections also had to keep working.

### Decision

We will enforce two archive-wide limits during ZIP log extraction: at most 10,000 entries (`maxZipEntryCount`) and at most 1 GiB of total uncompressed output (`maxZipTotalSize`). Enforcement happens in two layers. First, a pre-extraction validation pass rejects the archive based on entry count and the sum of declared sizes before any file is written. Second, a runtime byte budget is passed to each entry's `io.LimitReader`, so inaccurate size metadata cannot bypass the total limit. The limits live in a parameterized `unzipFileWithLimits` function, and `unzipFile` calls it with the default constants. This keeps the production API unchanged and lets tests use small limits.

Preflight also caps unique directories, including implicit parent directories, at 10,000 (`maxZipDirectoryCount`). This bounds inode consumption even when many empty files have deeply nested paths.

### Alternatives Considered

#### Alternative 1: Runtime byte counting only (no pre-validation pass)

Extraction could rely only on the cumulative `io.LimitReader` budget. That would be simpler, but it would partially extract an archive that its own metadata already shows is too large. Disk work would be wasted and leftover files would stay in the destination directory. The pre-validation pass rejects these archives before writing anything. The new test asserts that the destination directory stays empty.

#### Alternative 2: Pre-validation of declared metadata only

Checking only `UncompressedSize64` and `len(r.File)` up front would be cheap. But a crafted archive can understate its uncompressed sizes. That would defeat the protection this change is meant to add, so the runtime budget is still required.

#### Alternative 3: Configurable limits (flags/environment) instead of constants

Exposing the limits as user configuration would help users with very large legitimate log archives. This change does not do that. It keeps fixed constants, matching the existing `maxZipEntrySize` convention, and avoids adding CLI surface for a security guard. [TODO: verify whether any real workflow logs approach 1 GiB / 10,000 entries]

### Consequences

#### Positive
- Bounds disk and CPU use when extracting downloaded log archives, which addresses CWE-409 / alert 1062.
- Defense in depth: archives that are too large according to their own metadata are rejected before extraction, and archives with understated sizes are stopped at runtime.
- `unzipFileWithLimits` can be tested with small limits, without building very large fixtures.
- Close errors from the ZIP reader and entry readers are now returned instead of being silently dropped.

#### Negative
- Legitimate log archives over 1 GiB uncompressed or with more than 10,000 entries can no longer be extracted, and no override exists.
- The total limit (1 GiB) equals the per-entry limit, so the per-entry cap is now effectively redundant for single-entry archives. Two similar constants must be kept consistent.
- If the runtime budget is exceeded partway through, earlier entries are already written. Rejection is not atomic in that case.

#### Neutral
- `extractZipFile` keeps its signature as a wrapper. Internal helpers now return bytes written (`uint64`) along with the error.
- Runtime size-limit errors report the byte bound without attributing it to either the per-entry or archive-wide cap.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
