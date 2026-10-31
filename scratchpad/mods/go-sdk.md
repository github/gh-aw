# Module: github.com/modelcontextprotocol/go-sdk

## Overview

**go-sdk** is the official Go SDK for the Model Context Protocol (MCP). It provides
server-side primitives (`mcp.NewServer`, `mcp.AddTool`), client-side primitives
(`mcp.NewClient`), and a set of transports (stdio, in-memory, command, and
streamable HTTP).

**Key Characteristics:**

- Official MCP project, maintained jointly by the MCP community and Google
- Type-safe tool registration with automatic JSON-Schema generation (via `google/jsonschema-go`)
- Multiple transports: `StdioTransport`, `CommandTransport`, `InMemoryTransports`, streamable HTTP
- Protocol-revision negotiation with backward compatibility for older peers

## Version Used

Current version in `go.mod`: **v1.7.0** (latest release at time of review)

## Usage in gh-aw

### Files Using This Module

33 files, primarily:

- `pkg/cli/mcp_server.go` — server construction, capabilities, middleware wiring
- `pkg/cli/mcp_server_command.go` — stdio transport entry point
- `pkg/cli/mcp_server_http.go` — streamable HTTP handler and loopback binding
- `pkg/cli/mcp_tools_readonly.go`, `pkg/cli/mcp_tools_privileged.go`, `pkg/cli/mcp_tools_management.go` — tool registration
- `pkg/cli/mcp_argument_validation.go` — receiving middleware for schema-error rewriting
- `pkg/cli/mcp_inspect_mcp.go` — client-side usage for `gh aw mcp inspect`
- `pkg/parser/mcp.go` — MCP config parsing
- plus the corresponding `_test.go` files

### Key Roles

1. **Server** (`mcp_server.go`): builds the `gh aw` MCP server with a static tool set
   (status, compile, logs, audit, audit-diff, checks, mcp-inspect, add, update, fix).
   `Tools.ListChanged` is `false` because the tool set never changes.
2. **Transports**: `mcp.StdioTransport` for CLI mode and `mcp.NewStreamableHTTPHandler`
   for `--http` mode, bound to `127.0.0.1` with a 2 hour `SessionTimeout`.
3. **Client**: `mcp.NewClient` with `CommandTransport` / `StreamableClientTransport` /
   `InMemoryTransports` for the `mcp inspect` subcommand and end-to-end tests.
4. **Middleware**: `AddReceivingMiddleware` rewrites raw `additionalProperties` schema
   errors into "did you mean?" suggestions.
5. **Progress reporting**: `notifyProgress()` uses `req.Params.GetProgressToken()` and
   `Session.NotifyProgress` for long-running tools, no-oping when no token is supplied.
6. **Tool annotations**: `ReadOnlyHint` / `IdempotentHint` / `DestructiveHint` /
   `OpenWorldHint` are set consistently on every tool.

## Research Summary

**Repository:** https://github.com/modelcontextprotocol/go-sdk

### Recent Updates (v1.7.0, protocol revision `2026-07-28`)

- **Stateless / sessionless mode** (SEP-2575) — the `initialize` handshake is replaced by
  per-request `_meta` plus a `server/discover` RPC.
- **Multi-round-trip requests** (MRTR, SEP-2322) — sampling / elicitation / roots
  server-initiated calls are replaced by `InputRequiredResult` + retry.
- **`subscriptions/listen`** — a unified stream replaces the four separate
  `*/list_changed` notifications.
- **Cacheable list results** (`ttlMs` / `cacheScope`, SEP-2549).
- **HTTP header standardization** (`Mcp-Method`, `x-mcp-header` passthrough, SEP-2243).
- **Deprecation of roots, sampling, and logging** in the new revision — gh-aw uses none
  of these, so there is nothing to migrate.
- Backward compatible: the SDK negotiates down to `2025-11-25` for stateful peers, which
  is what gh-aw's HTTP server does today.
- Seven `MCPGODEBUG` escape-hatch flags exist for spec-compliance behavior changes and are
  removed in v1.9.0. None are needed by gh-aw, because its usage already matches the new
  defaults (for example bare `bool` `ReadOnlyHint` / `IdempotentHint` rather than `*bool`,
  which avoids needing `hintomitempty=1`).

### Best Practices Already Observed in gh-aw

- `mcp_server.go` documents that schema caching is automatic in go-sdk v1.3.0+.
- `mcp_tools_privileged.go` explains why the `timeout` parameter deliberately has **no**
  static schema default: the SDK would fill it in before the handler runs, short-circuiting
  the per-request computed default.
- `mcp_server_http.go` binds to loopback only as defense in depth alongside the SDK's own
  Host-header check, noting that the SDK dropped default cross-origin protection in v1.6.0.

## Improvement Opportunities

### Quick Wins

None required — the dependency is pinned to the latest release and usage patterns already
reflect v1.7.0-era defaults rather than legacy `MCPGODEBUG` opt-outs.

### Feature Opportunities

1. **Stateless HTTP mode** (`StreamableHTTPOptions.Stateless = true`) would opt the HTTP
   server into the `2026-07-28` protocol revision. Given the deliberate 2 hour
   `SessionTimeout` (session affinity for long-running `logs` / `audit` calls), staying
   stateful is retained for now because switching would break that session affinity.
2. **Custom JSON-RPC method registration** (new in v1.7.0) — no current need, but available
   if gh-aw ever wants a lightweight non-tool RPC endpoint.
3. **`x-mcp-header` passthrough** for `logs` / `audit` tool params — only relevant if the
   HTTP server is ever placed behind a reverse proxy; the server is loopback-only today.

### Convention Alignment

- Annotation types match the v1.7.0 defaults (bare `bool` hints)
- Deliberate avoidance of premature schema defaults
- In-code comments tracking SDK version-specific behavior

## Recommendations

1. No action needed this cycle — the module is current and usage follows the patterns shown in the SDK documentation.
2. Keep the `Stateless` HTTP mode decision on the radar if the MCP ecosystem drops support
   for stateful sessions on newer protocol revisions.
3. Continue the practice of commenting *why* an SDK default is or is not used (for example
   the `timeout` schema default) — it has already prevented the timeout default from silently overriding a handler-computed value.

## References

- **Package Documentation:** https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
- **Repository:** https://github.com/modelcontextprotocol/go-sdk
- **Releases:** https://github.com/modelcontextprotocol/go-sdk/releases
- **MCP Specification:** https://modelcontextprotocol.io/specification

---

**Last Reviewed:** 2026-08-18
**Module Version:** v1.7.0

---

*This summary was generated based on Go Fan analysis methodology. For the latest information, always check the upstream repository.*
