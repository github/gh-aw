# ADR-79903: Owner-Private MCP Logs Report Cache

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: Peli de Halleux (PR author, @pelikhan); [TODO: verify other reviewers]

---

### Context

The `logs` MCP tool summarizes workflow run data and returns a `file_path` so that agents read a bounded JSON report from disk rather than receiving the full summary inline. Before this change, reports were written to a fixed, world-readable directory (`/tmp/gh-aw/logs-cache`, directories `0755`, files `0644`), so any local user could read workflow data that the reports can contain. The built-in containerized MCP server writes to the `/tmp/gh-aw` bind mount that the agent shares, and it runs as the runner's UID, so any protection must still let the same-UID agent open the returned file. The old code already refused symlinks, but it still depended on a fixed, well-known path in a shared temporary directory.

### Decision

We will write each new report into a per-server-process directory created with `MkdirTemp` and mode `0700`, and create each report as an exclusive `0600` file through `os.Root`. Unsafe ancestors, non-private directories or files, and changed directories fail closed, and the legacy cache is neither read nor migrated. The primary driver is confidentiality of workflow log data; the `file_path` response and both continuation cursor formats stay unchanged, so no callers change. The containerized `agenticworkflows` backend receives `GH_AW_MCP_LOGS_REPORT_DIR=/tmp/gh-aw` so that its reports land on the shared mount, and non-Unix builds fail closed rather than relying on POSIX mode bits.

### Alternatives Considered

#### Alternative 1: Keep the fixed shared cache and tighten its permissions

Keep `/tmp/gh-aw/logs-cache` and change its modes to `0700` and `0600`. This is the smallest diff and would stop other users from reading new reports. It was rejected because the fixed name in a shared temporary directory remains predictable, so safety would rest on path checks alone, and reports already written with `0644` would still need migration or deletion. A random per-process directory avoids both problems. This was not a close call.

#### Alternative 2: Return the report inline in the MCP response

Return the summary JSON in the tool result and drop the filesystem handoff. This removes the file trust boundary entirely. It was rejected because the `file_path` handoff exists to keep tool responses small, and inline summaries would bring that size pressure back. [TODO: verify the response-size budget that motivated the `file_path` design.]

#### Alternative 3: Store reports in a per-user directory outside `/tmp`

Use the home directory or `XDG_RUNTIME_DIR`. This avoids shared temporary storage. It was rejected because the containerized backend has a private `/tmp` and an unwritable `/root` for the runner, and the agent only sees the `/tmp/gh-aw` mount. A home or runtime path would therefore not be readable by the agent.

### Consequences

#### Positive
- New reports are readable only by their owner (`0600` in a `0700` directory), which closes the cross-user disclosure path.
- Symlinks, non-private directories or files, and replaced directories produce an error instead of a silent fallback. Report creation uses `O_EXCL` through `os.Root`.
- The caller contract is unchanged: `file_path`, report bytes, and both cursor formats are identical, so no workflow callers need code changes.

#### Negative
- Reports under the legacy `/tmp/gh-aw/logs-cache/` are neither read nor migrated. Existing `file_path` references stop resolving, and affected reports must be regenerated.
- Deduplication is now per server process. Identical reports produced by different server processes get different directories and different `file_path` values.
- Non-Unix platforms, including Windows, fail to store reports, so the `logs` tool returns an error for the file handoff. [TODO: verify whether this tool is supported on Windows.]
- The shared-mount path requires the agent and the backend container to run as the same UID. A mismatch leaves the report unreadable by the agent.
- A new environment-variable contract (`GH_AW_MCP_LOGS_REPORT_DIR`) skips symlink resolution for the explicit shared path, which is extra surface to maintain.

#### Neutral
- The `ci-front-door-researcher`, `daily-cli-tools-tester`, and `daily-rendering-scripts-verifier` workflows, their lock files, and `.github/aw/agentic-workflows-mcp.md` now describe the new location. Lock file changes are limited to generated hashes.
- The server does not remove per-process report directories. They persist until the OS clears temporary storage, as the legacy cache did.
- The gateway sets `GH_AW_MCP_LOGS_REPORT_DIR` only for the `agenticworkflows` backend, not for `safeoutputs`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
