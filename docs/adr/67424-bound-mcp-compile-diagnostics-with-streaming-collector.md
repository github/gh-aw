# ADR-67424: Bound MCP Compile Diagnostics With a Streaming Head/Tail Collector

**Date**: 2026-10-10
**Status**: Proposed
**Deciders**: pelikhan

---

### Context

When `gh aw` runs as an MCP server, the `compile` tool shells out to the compiler/scanner subprocess and returns its diagnostics to the MCP client. With `DEBUG` enabled, the subprocess can emit many megabytes of stderr (observed up to 12 MiB in regression tests), and batch compiles can cover hundreds of workflows (334 lock files in this repository). Buffering that stream in full and returning it verbatim produced oversized MCP responses and, worse, let debug noise crowd out the one line that actually explains the failure. Reported in #66823 with corroborating reports in #66455, #66792, #67119, and #67350. The response must stay small enough for MCP transport while never dropping the cause-first error, multiline remediation text, or structured compiler JSON.

### Decision

Drain compile subprocess stderr through a compile-local streaming collector (`pkg/cli/mcp_compile_diagnostics.go`) instead of buffering the full debug stream. The collector keeps a bounded head and tail per line, strips ANSI, recognizes debug lines by the logger's namespace/message/elapsed format rather than a namespace allowlist, and tracks whether content was omitted. Skipping debug lines keeps the current error block open so interleaved logs cannot hide subsequent remediation. Shellcheck header/finding lines are collected separately and never replace an execution error or dependency warning.

Execution-failure messages are capped at 4 KiB and serialized fallback JSON at 48 KiB (including JSON escaping), with explicit truncation/omission markers. Structured compiler and scanner JSON is preserved even when it exceeds the fallback budget or the subprocess exits nonzero. The primary driver is bounded debug capture and fallback response size without losing the actionable error; structured stdout and accumulated scanner findings intentionally remain outside these bounds.

### Alternatives Considered

#### Alternative 1: Buffer all stderr and truncate at the end

Collect the complete stderr into memory and trim it to the byte budget just before returning. This is the simplest change and preserves ordering exactly, but it retains the unbounded memory cost of a multi-megabyte debug flood and tends to truncate from one end only — frequently cutting off the trailing cause-first error or the leading context. Rejected because it fixes the response size but not the memory profile or the loss of actionable content.

#### Alternative 2: Persist full diagnostics to a file and return a path/pointer

Write the complete stderr to a diagnostics file on disk and return only a short message plus a file reference. This preserves everything, but it introduces file lifecycle, cleanup, and permission concerns in the MCP server, and the MCP client may be on a different host with no access to that path. The PR explicitly concludes that no diagnostic-file persistence is needed. Rejected as unnecessary complexity for the failure modes observed.

#### Alternative 3: Suppress `DEBUG` output in the subprocess

Run the compile subprocess with debug logging disabled so the flood never occurs. This narrows the problem but does not bound genuinely large non-debug diagnostics (hundreds of workflows, huge single lines), and it removes debug signal that users intentionally enabled. Rejected as an incomplete mitigation.

### Consequences

#### Positive
- Debug-line capture and fallback response size are bounded (4 KiB message, 48 KiB fallback JSON), validated against a 12 MiB stderr flood. Structured stdout and accumulated scanner findings remain unbounded to preserve existing result contracts.
- The cause-first error, multiline remediation, and ANSI-normalized diagnostics survive truncation, so failures stay actionable.
- Structured compiler/scanner JSON and shellcheck findings remain available independently of the bounded failure message.

#### Negative
- Diagnostics can be lossy: middle content is replaced by a truncation marker, so reproducing a long failure may require rerunning the compile outside MCP.
- Debug/progress-line classification is heuristic (logger namespace/duration format and console prefixes such as `✗`, `Error:`, and `⚠`) and must track changes to logger formatting. New namespaces require no allowlist updates.
- Head/tail byte slicing requires explicit UTF-8 validation (`strings.ToValidUTF8`) and byte-boundary tests; careless indexing here is a recurring defect source.

#### Neutral
- Adds a new compile-local collector type in `pkg/cli` rather than a shared logging utility; other subprocess call sites are unchanged.
- Strict validation, required shellcheck checks, and the deprecated/ignored `max_tokens` contract are unchanged by this decision.
- Batch compiles that exceed the fallback budget return an explicit invalid batch result carrying the affected workflow count instead of per-workflow detail.

---

This decision remains proposed until maintainer review and merge.
