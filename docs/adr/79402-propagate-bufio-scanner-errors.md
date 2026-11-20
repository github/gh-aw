# ADR-79402: Propagate Bufio Scanner Errors Instead of Discarding Them

**Date**: 2026-11-20
**Status**: Draft
**Deciders**: Unknown (draft generated from PR #79402; author to confirm)

---

### Context

Several gh-aw functions read line-oriented input with `bufio.Scanner` and never checked `scanner.Err()` after the loop. When a line exceeds the scanner's default 64 KiB token limit, or a read fails, the loop stops early and the caller receives partial content with no error. The affected inputs are markdown section extraction, runtime and include imports, nested and remote includes, evals.jsonl results, and zizmor stderr. The `bufioscannererunchecked` analyzer described in ADR-60660 reports seven such loops, and the ignored-results tracker #71980 covers the wider backlog.

### Decision

We will check `scanner.Err()` after every `bufio.Scanner` loop and return the error with context, instead of returning partial results. Helpers that previously returned no error now return one, including the exported `parser.ExtractBodyLevelImportPaths`, and their callers and tests are updated to match. The primary driver is correctness: a truncated read must not silently produce an incomplete compiled workflow or incorrect experiment metrics.

### Alternatives Considered

#### Alternative 1: Log a warning and keep partial results

Logging `scanner.Err()` and continuing would keep today's success path for workflows with very long lines. It was considered because it avoids new failures for existing inputs. It was a close call. It was not chosen because a warning is easy to miss, and the partial content could still produce an incomplete workflow or wrong metrics without a failing signal.

#### Alternative 2: Raise the scanner buffer limit

Calling `scanner.Buffer` with a larger maximum, as several other gh-aw parsers already do, would make oversized lines rarer. It was considered because it keeps the current success path for most inputs. It was not chosen as the fix in this PR because it only moves the cutoff, and inputs above the new limit would still be truncated silently unless the error is checked. It remains a complementary option for specific inputs that legitimately need more room.

#### Alternative 3: Suppress the finding with `//nolint` or `_ = scanner.Err()`

Suppression would clear the lint output with a one-line change at each site. It was considered because it is the smallest diff. It was rejected because it keeps the silent-truncation behavior and only hides the report.

### Consequences

#### Positive
- Over-long lines and read failures in these inputs now return a clear, wrapped error instead of partial output.
- The seven loops reported by the pre-check are fixed, and new error-path tests use oversized tokens to lock in the behavior.
- The error contract of `parser.ExtractBodyLevelImportPaths` and the eval-record loaders is explicit in their signatures.

#### Negative
- This is a behavior change: inputs with lines longer than 64 KiB, which were previously truncated silently, now fail. Affected workflows may need restructuring or an explicit limit. [TODO: verify whether any known workflows exceed the limit.]
- Exported and internal signatures change, so any out-of-tree callers of `ExtractBodyLevelImportPaths` must be updated. [TODO: verify there are no external callers.]
- The wider backlog remains. `make golint-custom` still reports repository-wide findings, and #71980 records 727 ignored-result findings after this change.

#### Neutral
- Errors are wrapped with `%w` and name the file or section being scanned, so callers can inspect them with `errors.Is`.
- Checking `scanner.Err()` after each loop becomes the expected pattern for new code, which the analyzer in ADR-60660 enforces once it is adopted.
- The PR's validation covered `make fmt`, `go test ./pkg/parser`, and focused CLI scanner tests. The full `make golint-custom` run still exits non-zero because of existing findings.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
