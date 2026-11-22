# ADR-79903: Owner-Private MCP Logs Report Cache

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: Peli de Halleux (PR author, @pelikhan)

---

### Context

The `logs` MCP tool summarizes workflow run data and returns a `file_path` so that agents read a bounded JSON report from disk rather than receiving the full summary inline. Before this change, reports were written to a fixed, world-readable directory (`/tmp/gh-aw/logs-cache`, directories `0755`, files `0644`), so any local user could read workflow data that the reports can contain. The built-in containerized MCP server writes to the `/tmp/gh-aw` bind mount that the agent shares, and it runs as the runner's UID, so any protection must still let the same-UID agent open the returned file. The old code already refused symlinks, but it still depended on a fixed, well-known path in a shared temporary directory.

### Decision

On Linux and macOS, we write reports to a per-server-process directory created with `MkdirTemp` and mode `0700`, and create each report as an exclusive `0600` file through `os.Root`. Unsafe ancestors, non-private files or directories, and replaced cache directories fail closed. On macOS, extended ACLs or failure to verify their absence also cause an error; Linux POSIX ACL grants are constrained by the mode's group class. The legacy cache is neither read nor migrated. The response fields and both continuation cursor formats stay unchanged. The containerized `agenticworkflows` backend receives `GH_AW_MCP_LOGS_REPORT_DIR=/tmp/gh-aw` so its reports land on the shared mount.

### Alternatives Considered

#### Alternative 1: Keep the fixed shared cache and tighten its permissions

Keep `/tmp/gh-aw/logs-cache` and change its modes to `0700` and `0600`. This is the smallest diff, but it would reuse a predictable path and require a policy for pre-existing entries. A fresh per-process directory avoids trusting any pre-existing cache or touching legacy reports.

#### Alternative 2: Return the report inline in the MCP response

Return the summary JSON in the tool result and drop the filesystem handoff. This removes the file trust boundary, but it would put the full report in the response instead of letting the agent read and filter only the fields it needs. The current `logs` handler writes the full report to disk regardless of size; this decision does not depend on a particular token limit.

#### Alternative 3: Store reports in a per-user directory outside `/tmp`

Use the home directory or `XDG_RUNTIME_DIR`. This avoids shared temporary storage. It was rejected because the containerized backend has a private `/tmp` and an unwritable `/root` for the runner, and the agent only sees the `/tmp/gh-aw` mount. A home or runtime path would therefore not be readable by the agent.

### Consequences

#### Positive
- New reports are readable only by their owner (`0600` in a `0700` directory), which closes the cross-user disclosure path.
- Symlinks, non-private directories or files, and replaced directories produce an error instead of a silent fallback. Report creation uses `O_EXCL` through `os.Root`.
- The caller contract is unchanged: `file_path`, report bytes, and both cursor formats are identical, so no workflow callers need code changes.

#### Negative
- The server neither reads nor migrates reports under the legacy `/tmp/gh-aw/logs-cache/`. Generate a new report to use the private cache; this change does not retroactively protect legacy files.
- Deduplication is per cache directory and verifies the existing file's SHA-256 digest before returning its path. If the directory disappears while its ancestors remain safe, the next write creates a fresh private directory.
- On platforms other than Linux and macOS, including Windows, the `logs` tool returns a file-handoff error rather than using storage whose permissions it cannot verify.
- The shared-mount path requires the agent and the backend container to run as the same UID. A mismatch leaves the report unreadable by the agent.
- `GH_AW_MCP_LOGS_REPORT_DIR` adds a gateway-to-server configuration contract. The server validates an explicitly configured path as supplied, rejecting symlink components instead of following them.

#### Neutral
- The `ci-front-door-researcher`, `daily-cli-tools-tester`, and `daily-rendering-scripts-verifier` workflows and `.github/aw/agentic-workflows-mcp.md` now describe the new location. Workflow locks are regenerated from their Markdown sources.
- The server keeps reports available while it is running. On graceful shutdown it removes only the files it created and its own cache directory, without enumerating other directories. Forced termination or unsafe metadata can leave reports for host cleanup.
- The gateway sets `GH_AW_MCP_LOGS_REPORT_DIR` only for the `agenticworkflows` backend, including when a container user is explicitly configured. It does not change the explicit identity or its `HOME` settings.
