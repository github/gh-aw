# ADR-66938: Expose a Minimal Read-Only Work-Queue Subset Through the MCP Server

**Date**: 2026-10-08
**Status**: Accepted
**Deciders**: pelikhan (PR #66938 author)

---

### Context

AI clients connected to the `gh aw mcp-server` need to observe the Git-backed work queue (Work items, Claims, dependencies, delivery barriers) to reason about in-flight agentic work, but giving them queue mutation capability (publish, claim, dispatch, cancel) would let an agent change shared coordination state. The existing work-queue functionality is implemented as CLI subcommands with JSON output, filtering, pagination, and GitHub API credential handling already in place. The MCP server previously had no work-queue visibility. Any new tool must fit the repository's MCP conventions: a generated JSON schema, an allowlisted argument registry (`mcpToolParams`), registration in `mcp_server.go`, and inclusion in generated `.github/mcp.json` configuration.

### Decision

Expose exactly one MCP tool, `work-queue`, that supports only two read-only operations: `state` (bounded, metadata-only Work/Claim forest with filters and pagination) and `inspect` (details for exactly one `work_id` or `claim_id`). The tool is a thin wrapper that executes the existing CLI directly, without a shell (`work-queue <operation> --json --storage=git`), rather than reimplementing queue access. It is annotated `ReadOnlyHint`/`IdempotentHint`, requires an explicit `repo` (with `branch` defaulting to `work-queue`), and rejects any argument that does not belong to the selected operation. Invalid selectors and state filters are rejected as invalid MCP parameters before execution; actual execution failures remain internal errors. All other read-only operations (`replay`, `stats`, `explain`, `trace`, `evidence`) and every mutating operation remain CLI-only. The primary drivers are capability minimization (no queue mutation path reachable through this tool) and bounded metadata responses for MCP clients.

### Alternatives Considered

#### Alternative 1: One MCP tool per work-queue operation

Register separate `work-queue-state`, `work-queue-inspect`, (and later `work-queue-stats`, etc.) tools. This gives each operation a precise, self-documenting schema and avoids the "option belongs to another operation" validation logic entirely. Rejected because it multiplies the tool list that every client must load and would grow the MCP tool surface linearly as work-queue operations are added; a single dispatch tool with an `operation` enum keeps discovery cheap.

#### Alternative 2: Expose all read-only work-queue operations

Ship `state`, `inspect`, `replay`, `stats`, `explain`, `trace`, and `evidence` through MCP since none of them mutate state. The initial implementation exposed this set, but the author requested only the essentials. `state` already supplies counts, and `inspect` supplies item details. Full replay can expose payloads and produce large output; the other operations add specialized diagnostics beyond this initial need. The narrower subset can be widened later; it cannot easily be narrowed once clients depend on it.

#### Alternative 3: Call the work-queue Go packages directly instead of shelling out to the CLI

Invoke the internal queue/GitHub APIs in-process for lower latency and no subprocess cost. Not selected because the CLI already owns JSON shaping, filtering, pagination defaults, storage selection, and credential handling. Reusing its command path follows the server's existing subprocess pattern and avoids a second presentation path. This change makes no latency guarantee; a future optimization can extract shared presentation helpers if subprocess overhead becomes material.

### Consequences

#### Positive
- Agents gain queue observability with no reachable queue mutation path through this tool; it is annotated read-only and idempotent.
- Output formatting, filtering, pagination, and Git credentials are reused from the CLI, so MCP and CLI results stay consistent by construction.
- Argument allowlisting per operation (`offset >= 0`, `limit` in 1..256, flag-attached values) prevents option smuggling into the executed command line.
- State output is bounded by both row pagination (`limit` default 80, max 256) and the CLI's 64 KiB view budget. Inspect detail text uses the same view budget; neither operation exposes work payloads or receipt bodies.

#### Negative
- Each tool call spawns a `gh aw work-queue` subprocess, adding process-startup latency compared with an in-process call.
- The single-tool-with-`operation`-enum shape means the schema advertises fields that are invalid for the selected operation, so some misuse is only caught at call time rather than by the client's schema validation.
- Clients that want `stats`, `trace`, `explain`, `replay`, or `evidence` must still fall back to the CLI, creating an uneven capability surface.
- Adding `work-queue` to the default generated tool list changes `.github/mcp.json` output and required updating several existing tests.

#### Neutral
- Actor validation applied to the log and audit tools does not gate this tool; access is governed by the server's existing Git credentials, so private-repo reads require read access.
- `--storage=git` is pinned by the MCP wrapper, so the tool does not follow any alternative storage backend the CLI may support.
- The exposed subset is additive and can be widened in a later ADR if client demand appears.

---

*Finalized for PR #66938.*
