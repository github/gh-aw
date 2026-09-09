---
title: MCP Gateway Specification
description: Formal specification for the Model Context Protocol (MCP) Gateway implementation following W3C conventions
sidebar:
  order: 1350
---

# MCP Gateway Specification

**Version**: 1.17.0
**Status**: Draft Specification  
**Latest Version**: [mcp-gateway](/gh-aw/reference/mcp-gateway/)  
**JSON Schema**: [mcp-gateway-config.schema.json](/gh-aw/schemas/mcp-gateway-config.schema.json)  
**Editor**: GitHub Agentic Workflows Team

---

## Abstract

This specification defines the Model Context Protocol (MCP) Gateway, a transparent proxy service that enables unified HTTP access to multiple MCP servers. The gateway supports containerized MCP servers, HTTP-based MCP servers, and custom server types. The gateway provides protocol translation, server isolation, authentication, health monitoring, and extensibility for specialized server implementations.

## Status of This Document

This section describes the status of this document at the time of publication. This is a draft specification and may be updated, replaced, or made obsolete by other documents at any time.

This document is governed by the GitHub Agentic Workflows project specifications process.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Architecture](#3-architecture)
4. [Configuration](#4-configuration)
5. [Protocol Behavior](#5-protocol-behavior)
6. [Server Isolation](#6-server-isolation)
7. [Authentication](#7-authentication)
8. [Health Monitoring](#8-health-monitoring)
9. [Error Handling](#9-error-handling)
10. [Guard Policy](#10-guard-policy)
11. [Compliance Testing](#11-compliance-testing)

---

## 1. Introduction

### 1.1 Purpose

The MCP Gateway serves as a protocol translation layer between MCP clients expecting HTTP-based communication and MCP servers running in containers or accessible via HTTP. It enables:

- **Protocol Translation**: Converting between containerized stdio servers and HTTP transports
- **Unified Access**: Single HTTP endpoint for multiple MCP servers
- **Server Isolation**: Enforcing boundaries between server instances through containerization
- **Authentication**: Token-based access control
- **Health Monitoring**: Service availability endpoints

The gateway requires that stdio-based MCP servers MUST be containerized. Direct command execution (stdio+command without containerization) is NOT supported because it cannot provide the necessary isolation and portability guarantees.

### 1.2 Scope

This specification covers:

- Gateway configuration format and semantics
- Protocol translation behavior
- Server lifecycle management
- Authentication mechanisms
- Health monitoring interfaces
- Error handling requirements

This specification does NOT cover:

- Model Context Protocol (MCP) core protocol semantics
- Individual MCP server implementations
- Client-side MCP implementations
- User interfaces or interactive features (e.g., elicitation)

### 1.3 Design Goals

The gateway MUST be designed for:

- **Headless Operation**: No user interaction required during runtime
- **Fail-Fast Behavior**: Immediate failure with diagnostic information
- **Forward Compatibility**: Graceful rejection of unknown configuration features
- **Security**: Isolation between servers and secure credential handling

---

## 2. Conformance

### 2.1 Conformance Classes

A **conforming MCP Gateway implementation** is one that satisfies all MUST, REQUIRED, and SHALL requirements in this specification.

A **partially conforming MCP Gateway implementation** is one that satisfies all MUST requirements but MAY lack support for optional features marked with SHOULD or MAY.

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.3 Compliance Levels

Implementations MUST support:

- **Level 1 (Required)**: Basic proxy functionality, stdio transport, configuration parsing
- **Level 2 (Standard)**: HTTP transport, authentication, health endpoints
- **Level 3 (Complete)**: All optional features including variable expressions, timeout configuration

---

## 3. Architecture

### 3.1 Gateway Model

```
┌─────────────────────────────────────────────────────────┐
│                      MCP Client                         │
│                    (HTTP Transport)                     │
└──────────────────────┬──────────────────────────────────┘
                       │ HTTP/JSON-RPC
                       ▼
┌─────────────────────────────────────────────────────────┐
│                    MCP Gateway                          │
│  ┌───────────────────────────────────────────────────┐  │
│  │  Authentication & Authorization Layer             │  │
│  └───────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────┐  │
│  │  Protocol Translation Layer                       │  │
│  └───────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────┐  │
│  │  Server Isolation & Lifecycle Management          │  │
│  └───────────────────────────────────────────────────┘  │
└──────┬──────────────┬──────────────┬───────────────────┘
       │              │              │
       │ stdio        │ HTTP         │ stdio
       ▼              ▼              ▼
  ┌─────────┐   ┌─────────┐   ┌─────────┐
  │ MCP     │   │ MCP     │   │ MCP     │
  │ Server  │   │ Server  │   │ Server  │
  │ 1       │   │ 2       │   │ N       │
  └─────────┘   └─────────┘   └─────────┘
```

### 3.2 Transport Support

The gateway MUST support the following transport mechanisms:

- **stdio (containerized)**: MCP servers running in containers with standard input/output based communication
- **HTTP**: Direct HTTP-based MCP servers

The gateway MUST translate all upstream transports to HTTP for client communication.

#### 3.2.1 Containerization Requirement

Stdio-based MCP servers MUST be containerized. The gateway SHALL NOT support direct command execution without containerization (stdio+command) because:

1. Containerization provides necessary process isolation and security boundaries
2. Containers enable reproducible environments across different deployment contexts
3. Container images provide versioning and dependency management
4. Containerization ensures portability and consistent behavior

Direct command execution of stdio servers (e.g., `command: "node server.js"` without a container) is explicitly NOT SUPPORTED by this specification.

### 3.3 Operational Model

The gateway operates in a headless mode:

1. Configuration is provided via **stdin** (JSON format)
2. Secrets are provided via **environment variables**
3. Startup output is written to **stdout** (rewritten configuration)
4. Error messages are written to **stdout** as error payloads
5. HTTP server accepts client requests on configured port

---

## 4. Configuration

### 4.1 Configuration Format

The gateway MUST accept configuration via stdin in JSON format conforming to the MCP configuration file schema.

**JSON Schema**: [mcp-gateway-config.schema.json](/gh-aw/schemas/mcp-gateway-config.schema.json)

#### 4.1.1 Configuration Structure

```json
{
  "mcpServers": {
    "server-name": {
      "container": "string",
      "entrypoint": "string",
      "entrypointArgs": ["string"],
      "mounts": ["source:dest:mode"],
      "env": {
        "VAR_NAME": "value"
      },
      "type": "stdio" | "http",
      "url": "string",
      "tools": ["*"] | ["tool1", "tool2"],
      "headers": {
        "Authorization": "Bearer ${TOKEN}"
      }
    }
  },
  "gateway": {
    "port": 8080,
    "agentId": "string",
    "domain": "string",
    "startupTimeout": 30,
    "toolTimeout": 60
  },
  "customSchemas": {
    "custom-type": "https://example.com/schema.json"
  }
}
```

#### 4.1.2 Server Configuration Fields

Each server configuration MUST support:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `container` | string | Conditional* | Container image for the MCP server (required for stdio servers) |
| `entrypoint` | string | No | Optional entrypoint override for container (equivalent to `docker run --entrypoint`) |
| `entrypointArgs` | array[string] | No | Arguments passed to container entrypoint (container only) |
| `mounts` | array[string] | No | Volume mounts for containerized stdio servers (format: "host:container:mode" where mode is "ro" (read-only) or "rw" (read-write)). Applies to stdio servers only. See Section 4.1.5 for details. |
| `env` | object | No | Environment variables for the server process |
| `type` | string | No | Transport type: "stdio" or "http" (default: "stdio") |
| `url` | string | Conditional** | HTTP endpoint URL for HTTP servers |
| `registry` | string | No | URI to the installation location when MCP is installed from a registry. This is an informational field used for documentation and tooling discovery. Applies to both stdio and HTTP servers. Example: `"https://api.mcp.github.com/v0/servers/microsoft/markitdown"` |
| `tools` | array[string] | No | Tool filter for the MCP server. Use `["*"]` to allow all tools (default), or specify a list of tool names to allow. This field is passed through to agent configurations and applies to both stdio and http servers. |
| `headers` | object | No | HTTP headers to include in requests (HTTP servers only). Commonly used for authentication to external HTTP servers. Values may contain variable expressions. |
| `auth` | object | No | Upstream authentication configuration for HTTP servers. See [Section 7.6](#76-upstream-authentication-oidc). |

*Required for stdio servers (containerized execution)  
**Required for HTTP servers

**Note**: The `command` field is NOT supported. Stdio servers MUST use the `container` field to specify a containerized MCP server. Direct command execution is not supported by this specification.

#### 4.1.3 Gateway Configuration Fields

The `gateway` section is required and configures gateway-specific behavior:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `port` | integer | Yes | HTTP server port |
| `domain` | string | Yes | Gateway domain (localhost or host.docker.internal) |
| `agentId` | string | Conditional* | Single agent/session identifier used for authentication. Mutually exclusive with `agentIds`. See Section 4.1.3.9. |
| `agentIds` | array[string] | Conditional* | List of agent/session identifiers (at least one, each non-empty), used for authentication. Mutually exclusive with `agentId`. See Section 4.1.3.9. |
| `startupTimeout` | integer | No | Server startup timeout in seconds (default: 30) |
| `toolTimeout` | integer | No | Tool invocation timeout in seconds (default: 60) |
| `payloadDir` | string | No | Directory path for storing large payload JSON files for authenticated clients |
| `payloadPathPrefix` | string | No | Path prefix to remap payload paths for agent containers (e.g., /workspace/payloads) |
| `payloadSizeThreshold` | integer | No | Size threshold in bytes for storing payloads to disk (default: 524288 = 512KB) |
| `trustedBots` | array[string] | No | Additional GitHub bot identity strings (e.g., `github-actions[bot]`) passed to the gateway and merged with its built-in trusted identity list. This field is additive — it extends the internal list but cannot remove built-in entries. |
| `keepaliveInterval` | integer | No | Keepalive ping interval in seconds for HTTP MCP backends. Prevents session expiry during long-running tasks. Use `-1` to disable, `0` or unset for gateway default (1500s = 25 min), or a positive integer for a custom interval. |
| `sessionTimeout` | string | No | Session timeout for MCP gateway sessions as a Go duration string (e.g. `"30m"`, `"4h"`, `"24h"`). Empty or omitted uses the gateway default (6h). Must be at least 5m when set by the workflow compiler (no upper bound; infrastructure operators may override via `MCP_GATEWAY_SESSION_TIMEOUT` env var). |
| `opentelemetry` | object | No | OpenTelemetry configuration for emitting distributed tracing events for MCP calls. See Section 4.1.3.7 for details. |
| `forcePublicRepos` | boolean | No | When `true` (default), forces the allow-only policy to `repos="public"` at runtime if the gateway detects it is running in a public repository. When `false`, disables this override — set by the compiler when `private-to-public-flows: allow` is declared in workflow frontmatter. See Section 4.1.3.8 for details. |
| `sinkVisibilityExemptServers` | array[string] | No | List of server IDs exempt from the default `sink-visibility="public"` enforcement. Use `["*"]` to exempt all servers. Set by the compiler when `private-to-public-flows` lists specific server IDs in workflow frontmatter. See Section 10.9 for details. |

*Exactly one of `agentId` or `agentIds` MUST be specified; the two fields are mutually exclusive. See Section 4.1.3.9.

#### 4.1.3.1 Payload Directory Path Validation

When the optional `payloadDir` field is provided in the gateway configuration, it specifies a directory path where the gateway stores large payload JSON files for authenticated clients. This enables efficient handling of large response payloads by offloading them to the filesystem.

**Path Requirements**:

If `payloadDir` is specified, the following requirements apply:

1. The path MUST be an absolute path (full pathname)
2. On Unix-like systems (Linux, macOS), absolute paths MUST start with `/`
3. On Windows systems, absolute paths MUST start with a drive letter followed by `:` and `\` (e.g., `C:\`, `D:\`)
4. The path MUST NOT be an empty string
5. The path SHOULD be writable by the gateway process
6. The path SHOULD exist or be creatable by the gateway process

**Validation Examples**:

Valid absolute paths:

```
Unix/Linux/macOS:
- "/var/lib/mcp-gateway/payloads"
- "/tmp/gateway-payloads"
- "/opt/mcp/data/payloads"

Windows:
- "C:\Program Files\MCP Gateway\payloads"
- "D:\gateway\payloads"
- "C:\temp\payloads"
```

Invalid paths (MUST be rejected):

```
Relative paths:
- "payloads" (no leading / or drive letter)
- "./payloads" (relative to current directory)
- "../data/payloads" (relative path with parent reference)
- "data/payloads" (relative path)

Empty or malformed:
- "" (empty string)
- " " (whitespace only)
```

**Security Considerations**:

- Gateway implementations MUST ensure proper isolation between different clients' payload files
- The gateway SHOULD use appropriate file permissions to prevent unauthorized access
- The gateway SHOULD implement cleanup mechanisms for old payload files
- The gateway SHOULD validate that the path does not escape intended directory boundaries through symbolic links or other mechanisms

**Compliance Test**: T-CFG-005 - Payload Directory Path Validation

#### 4.1.3.2 Payload Path Prefix for Agent Containers

When the optional `payloadPathPrefix` field is provided in the gateway configuration, it specifies a path prefix used to remap payload file paths returned to clients. This enables agents running in containers to access payload files via mounted volumes.

**How it works**:

1. Gateway saves payload to actual filesystem: `/tmp/jq-payloads/session123/query456/payload.json`
2. Gateway returns remapped path to client: `/workspace/payloads/session123/query456/payload.json`
3. Agent container mounts volume: `-v /tmp/jq-payloads:/workspace/payloads`
4. Agent can now access the file at the returned path ✅

**Configuration Example**:

```toml
[gateway]
payload_dir = "/tmp/jq-payloads"
payload_path_prefix = "/workspace/payloads"
port = 8080
domain = "localhost"
agent_id = "secret"
```

**Use Cases**:
- Agents running in containers with different filesystem layouts
- Docker-in-Docker scenarios where host paths need remapping
- Environments with controlled volume mounts for security

**Requirements**:
- If specified, the path prefix SHOULD match a mounted volume in the agent container
- The gateway MUST use this prefix when returning `payloadPath` to clients
- The gateway MUST still save files to the actual filesystem path (`payloadDir`)

#### 4.1.3.3 Payload Size Threshold

The `payloadSizeThreshold` field (default: 524288 bytes = 512KB) controls when response payloads are stored to disk versus returned inline.

**Behavior**:
- Payloads **smaller than or equal** to threshold: Returned inline in the response
- Payloads **larger than** threshold: Stored to disk, metadata returned with `payloadPath`

**Default Value**: 524288 bytes (512KB)

**Rationale**: The 512KB default accommodates typical MCP tool responses including GitHub API queries (list_commits, list_issues, etc.) without triggering disk storage. This prevents agent looping issues when payloadPath is not accessible in agent containers.

**Configuration Example**:

```toml
[gateway]
payload_size_threshold = 1048576  # 1MB - minimize disk storage
# OR
payload_size_threshold = 262144   # 256KB - more aggressive disk storage
```

**Configuration Methods**:
- CLI flag: `--payload-size-threshold <bytes>`
- Environment variable: `MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD=<bytes>`
- TOML config file: `payload_size_threshold = <bytes>` in `[gateway]` section
- Default if not specified: 524288 bytes (512KB)

**Requirements**:
- Threshold MUST be a positive integer representing bytes
- Gateway MUST compare actual payload size against threshold before deciding storage method
- Threshold MAY be adjusted based on deployment needs (memory vs disk I/O trade-offs)

#### 4.1.3.4 Trusted Bot Identity Configuration

The optional `trustedBots` field in the gateway configuration passes an additional list of GitHub bot identity strings to the gateway. The gateway merges this list with its own built-in trusted identity list to form the effective set of identities it recognizes.

> **Important**: `trustedBots` is **additive**. The gateway maintains its own internal list of trusted bot identities. The `trustedBots` field extends that internal list with additional identities; it cannot remove or override the gateway's built-in trusted identities.

**Configuration Example**:

```json
{
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "${MCP_GATEWAY_AGENT_ID}",
    "trustedBots": [
      "github-actions[bot]",
      "copilot-swe-agent[bot]"
    ]
  }
}
```

**Frontmatter Example** (workflow author):

```yaml
sandbox:
  mcp:
    trusted-bots:
      - github-actions[bot]
      - copilot-swe-agent[bot]
```

**Requirements**:
- `trustedBots` MUST be a non-empty array of strings when present
- Each entry MUST be a non-empty string (typically a GitHub username such as `github-actions[bot]`)
- `trustedBots` entries are **merged** with the gateway's internal built-in trusted identities to form the effective list passed to the gateway
- `trustedBots` MUST NOT be used to remove or override entries in the gateway's internal built-in trusted identity list

**Compliance Test**: T-AUTH-006 - Trusted Bot Identity Configuration

#### 4.1.3.5 Keepalive Interval Configuration

The optional `keepaliveInterval` field in the gateway configuration controls how often the gateway sends periodic keepalive pings to HTTP MCP backends. This prevents idle session expiry during long-running agent tasks.

| Value | Behavior |
|-------|----------|
| Unset / `0` | Gateway default: 1500 seconds (25 minutes) |
| `> 0` | Custom keepalive interval in seconds |
| `-1` | Disable keepalive pings entirely |

**Configuration example (JSON)**:

```json
{
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "${MCP_GATEWAY_AGENT_ID}",
    "keepaliveInterval": 300
  }
}
```

**Workflow frontmatter** (via `sandbox.mcp.keepalive-interval`):

```yaml
sandbox:
  mcp:
    keepalive-interval: 300   # 5-minute keepalive for backends with short idle timeouts
```

**Compliance rules**:

- `keepaliveInterval` MUST be an integer when present
- A value of `0` is treated as unset by the gateway (silently defaults to 1500 seconds)
- A value of `-1` disables keepalive pings entirely
- Any positive integer sets the keepalive interval in seconds

#### 4.1.3.6 Session Timeout Configuration

The optional `sessionTimeout` field in the gateway configuration controls how long stateful MCP sessions survive in both unified and routed modes.

| Value | Behavior |
|-------|----------|
| Unset / `""` | Gateway default: 6 hours (or `MCP_GATEWAY_SESSION_TIMEOUT` env var) |
| Go duration string | Custom session lifetime (e.g. `"30m"`, `"4h"`, `"6h"`, `"12h"`) |

**Precedence**: `sessionTimeout` in the stdin config JSON **>** `MCP_GATEWAY_SESSION_TIMEOUT` environment variable **>** gateway built-in default (6h).

**Configuration example (JSON)**:

```json
{
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "${MCP_GATEWAY_AGENT_ID}",
    "sessionTimeout": "4h"
  }
}
```

**Workflow frontmatter** (via `engine.mcp.session-timeout`):

```yaml
engine:
  id: copilot
  mcp:
    session-timeout: 4h   # 4-hour sessions for long-running migrations
```

**Compliance rules**:

- `sessionTimeout` MUST be a valid Go duration string when present (e.g. `"30m"`, `"4h"`)
- The workflow compiler enforces a minimum of `5m` for author-specified values (no upper bound)
- Infrastructure operators may set `MCP_GATEWAY_SESSION_TIMEOUT` on the gateway container to override the default for all workflows; a per-workflow `sessionTimeout` in the stdin config takes precedence
- When unset, the gateway uses its built-in default (6h)

#### 4.1.3.7 OpenTelemetry Configuration

The optional `opentelemetry` object in the gateway configuration enables the gateway to emit distributed tracing events for MCP calls using the [OpenTelemetry](https://opentelemetry.io/) standard. When configured, the gateway creates spans for each MCP tool invocation and exports them to the designated collector endpoint.

**Configuration Fields**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `endpoint` | string | Yes (when `opentelemetry` is present) | OTLP/HTTP endpoint URL for the OpenTelemetry collector (e.g., `https://collector.example.com:4318/v1/traces` or `http://127.0.0.1:4318/v1/traces`). MUST use HTTP or HTTPS. Supports variable expressions. |
| `traceId` | string | No | Parent trace ID for context propagation. When set, the gateway attaches all emitted spans as children of this trace, enabling correlation with an existing distributed trace. MUST be a 32-character lowercase hex string (128-bit W3C trace ID format). Supports variable expressions. |
| `spanId` | string | No | Parent span ID for context propagation. When set together with `traceId`, the gateway sets this span as the direct parent of its root span. MUST be a 16-character lowercase hex string (64-bit W3C span ID format). Ignored when `traceId` is not set. Supports variable expressions. |
| `serviceName` | string | No | Logical service name reported in the `service.name` resource attribute of all emitted spans. Identifies the gateway in the tracing backend. Defaults to `"mcp-gateway"` when not specified. |

> [!NOTE]
> Authentication headers (e.g., `Authorization: Bearer <token>`) for the OTLP collector MUST be provided via the `OTEL_EXPORTER_OTLP_HEADERS` environment variable, not through the JSON config. This follows the standard [OTel SDK environment variable convention](https://opentelemetry.io/docs/specs/otel/protocol/exporter/#configuration-options) and keeps credentials out of the stdin config pipe. The format is a comma-separated list of `key=value` pairs (e.g., `Authorization=Bearer my-token,X-Custom=value`) as defined by the [OpenTelemetry OTLP exporter specification](https://opentelemetry.io/docs/specs/otel/protocol/exporter/#configuration-options). When gh-aw compiles a workflow with `observability.otlp.headers`, the value is automatically forwarded to the gateway container via `-e OTEL_EXPORTER_OTLP_HEADERS`.

**Configuration Example**:

```json
{
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "${MCP_GATEWAY_AGENT_ID}",
    "opentelemetry": {
      "endpoint": "https://collector.example.com:4318/v1/traces",
      "serviceName": "my-mcp-gateway"
    }
  }
}
```

**Tracing Behavior**:

When `opentelemetry` is configured, the gateway MUST:

1. Create a root span for the gateway process lifetime with `service.name` set to the configured `serviceName`
2. Create a child span for each MCP tool invocation with the following attributes:
   - `mcp.server`: the server name as configured in `mcpServers`
   - `mcp.method`: the JSON-RPC method name (e.g., `tools/call`)
   - `mcp.tool`: the tool name when the method is `tools/call`
   - `http.status_code`: the HTTP status code of the proxied response
3. Record span start and end timestamps accurately
4. Export completed spans to the configured `endpoint` using the OTLP/HTTP protocol
5. Apply any headers from `OTEL_EXPORTER_OTLP_HEADERS` (when set) to every export request
6. Propagate W3C `traceparent` context when `traceId` and `spanId` are provided

When `traceId` is supplied, the gateway MUST construct a valid W3C `traceparent` header and use it as the parent context for the root span. The trace flags field SHOULD be set to `01` (sampled) when the gateway has no upstream sampling decision available; implementations MAY propagate upstream sampling flags when they are available. When only `traceId` is supplied without `spanId`, the gateway MUST generate a random `spanId` for the `traceparent` header.

**Failure Handling**:

The gateway MUST NOT fail to start if the OpenTelemetry collector endpoint is unreachable. Export failures SHOULD be logged as warnings and MUST NOT affect MCP request processing. The gateway SHOULD implement an exponential back-off retry strategy for failed exports.

**Requirements**:

- `endpoint` MUST be present when the `opentelemetry` object is configured
- `endpoint` MUST be an HTTP or HTTPS URL
- `traceId`, when provided, MUST be a 32-character lowercase hex string
- `spanId`, when provided, MUST be a 16-character lowercase hex string
- `spanId` SHOULD only be set when `traceId` is also set; if `spanId` is provided without `traceId` the gateway SHOULD log a warning and ignore `spanId`
- Export failures MUST NOT propagate errors to MCP clients
- Authentication headers MUST be provided via `OTEL_EXPORTER_OTLP_HEADERS` env var; the `headers` field is no longer accepted in the JSON config

**Compliance Test**: T-OTEL-001 through T-OTEL-010 (Section 11.1.10)

#### 4.1.3.8 `forcePublicRepos` Configuration

When `forcePublicRepos` is `true` (the default), the gateway overrides the compiled allow-only policy to `repos="public"` at startup if it detects the workflow is running in a public repository. This prevents agents from accumulating private-data secrecy tags in the first place — even when the compiled config grants broader access.

**Applies to both modes**: This runtime check applies to both the MCP gateway (unified/routed mode, via `gateway.forcePublicRepos` config) and the CLI proxy (`awmg proxy`, via the `--force-public-repos` flag, which defaults from `MCP_GATEWAY_FORCE_PUBLIC_REPOS`). In proxy mode, the override modifies the `--policy` JSON before passing it to the WASM guard.

**Detection mechanism**: The gateway reads `GITHUB_REPOSITORY` and calls `GET /repos/{owner}/{repo}` at startup to determine repository visibility. Proxy mode uses the same check only when the launcher forwards `GITHUB_REPOSITORY` and a GitHub token into the proxy process/container. If the repository is public and `forcePublicRepos` is `true`, the override is applied — subject to those inputs and a successful API response (see Precedence rules below).

**Scope of the override**: Only the allow-only policy is affected. Guard policies, tool permissions, and other configuration are unchanged.

**Precedence rules**:
- Configured `repos="public"` (explicit in the allow-only policy) is always preserved — the gateway never relaxes an explicit public constraint.
- Runtime detection of a public repo with `forcePublicRepos: true` **adds** the public restriction; it does not relax any existing constraint.
- `forcePublicRepos: false` disables the runtime override entirely — the compiled allow-only policy is used as-is.
- This override is **skipped** when `GITHUB_REPOSITORY` or the GitHub token is unavailable.
- API errors during visibility detection result in a non-fatal warning; the gateway/proxy falls back to the compiled policy.

**Opt-out**: Workflow authors who intentionally allow private→public data flows set `private-to-public-flows: allow` in frontmatter (Section 10.9). The compiler translates this to `gateway.forcePublicRepos: false` in the generated gateway JSON stdin config. For proxy mode, launchers should pass `--force-public-repos=false` when they need equivalent opt-out behavior.

**Environment variable override**: `MCP_GATEWAY_FORCE_PUBLIC_REPOS=false` disables the override without requiring a config change. The `--force-public-repos` flag defaults to the value of this environment variable (defaulting to `true` when unset).

**Configuration Example**:

```json
{
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "${MCP_GATEWAY_AGENT_ID}",
    "forcePublicRepos": false
  }
}
```

**Compliance Test**: T-WS-004 (Section 11.1.12)

#### 4.1.3.9 Agent Identifier Configuration (`agentId` / `agentIds`)

The gateway configuration MUST select exactly one of the following mutually exclusive forms to configure the agent/session identifier(s) used for authentication and DIFC session-state keying:

- `agentId` (string): a single agent/session identifier.
- `agentIds` (array of strings): one or more agent/session identifiers, enabling a primary agent and enclave sessions to share one gateway instance while retaining independent DIFC state and policies.

**Requirements**:

- A gateway configuration MUST specify exactly one of `agentId` or `agentIds`. Specifying both, or neither, MUST be rejected as invalid configuration.
- `agentId`, when present, MUST be a non-empty string.
- `agentIds`, when present, MUST be an array containing at least one item, and every item MUST be a non-empty string.
- `agentIds` enables associating multiple agent/session identifiers with one gateway instance in configuration. Runtime authentication and per-identifier DIFC session-state keying are out of scope for this section and are tracked as follow-up work (see Section 7.2).

**Configuration Example (singular)**:

```json
{
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "primary-agent"
  }
}
```

**Configuration Example (plural)**:

```json
{
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentIds": ["primary-agent", "enclave-agent"]
  }
}
```

**mcpg TOML spelling**:

```toml
[gateway]
port = 8080
domain = "localhost"
agent_id = "primary-agent"
# OR
agent_ids = ["primary-agent", "enclave-agent"]
```

**Compatibility**: Implementations MAY continue to accept the legacy `api_key`/`apiKey` spelling internally for backward compatibility with existing deployments, but `apiKey` MUST NOT appear in generated or published configuration; `agentId` or `agentIds` MUST be used instead. Runtime mapping from individual identifiers to allow-only policies is out of scope for this section and is tracked as follow-up work.

#### 4.1.3a Top-Level Configuration Fields

The following fields MAY be specified at the top level of the configuration:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `customSchemas` | object | No | Map of custom server type names to JSON Schema URLs for validation. See Section 4.1.4 for details. |

#### 4.1.4 Custom Server Types

The gateway MAY support custom server types beyond the standard "stdio" and "http" types. Custom server types enable extensibility for specialized MCP server implementations with additional configuration requirements.

**Registration Mechanism**:

Custom server types MUST be registered in the `customSchemas` field at the top level of the configuration, which maps type names to JSON Schema URLs:

```json
{
  "mcpServers": {
    "my-custom-server": {
      "type": "safeinputs"
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "secret"
  },
  "customSchemas": {
    "safeinputs": "https://docs.github.com/gh-aw/schemas/mcp-scripts-config.schema.json"
  }
}
```

**Validation Behavior**:

When a server configuration includes a `type` field with a value not in `["stdio", "http"]`:

1. The gateway MUST check if the type is registered in `customSchemas`
2. If registered with an HTTPS URL, the gateway MUST fetch and apply the corresponding JSON Schema for validation
3. If registered with an empty string, the gateway MUST skip schema validation for that type
4. If not registered, the gateway MUST reject the configuration with an error indicating the unknown type
5. Custom schemas MUST be valid JSON Schema Draft 7 or later
6. Custom schemas MAY extend base server configuration fields

**Example with Custom Type**:

```json
{
  "mcpServers": {
    "my-custom-server": {
      "type": "safeinputs",
      "tools": {
        "greet": {
          "description": "Greet user",
          "script": "return { message: 'Hello!' };"
        }
      }
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "secret"
  },
  "customSchemas": {
    "safeinputs": "https://docs.github.com/gh-aw/schemas/mcp-scripts-config.schema.json"
  }
}
```

**Requirements**:

- Custom types MUST NOT conflict with reserved types ("stdio", "http")
- Custom schema URLs MUST be HTTPS URLs only (for security reasons)
- Custom schema URLs MAY be empty strings to skip validation
- Implementations SHOULD cache fetched schemas for performance
- Schema fetch failures MUST result in configuration validation errors
- Custom server configurations MUST validate against their registered schemas when a schema URL is provided

#### 4.1.5 Volume Mounts for Stdio Servers

Stdio (containerized) MCP servers MAY specify volume mounts to provide access to host filesystem paths. Volume mounts enable servers to read configuration files, access data directories, or write output files while maintaining container isolation.

**Mount Format**:

Volume mounts MUST use the format:

```
"host:container:mode"
```

Where:
- **host**: Absolute path on the host filesystem
- **container**: Absolute path inside the container
- **mode**: Access mode, either "ro" (read-only) or "rw" (read-write)

**Configuration Example**:

```json
{
  "mcpServers": {
    "data-processor": {
      "container": "ghcr.io/example/data-mcp:latest",
      "type": "stdio",
      "mounts": [
        "/var/data/input:/app/input:ro",
        "/var/data/output:/app/output:rw",
        "/etc/config/app.json:/app/config.json:ro"
      ]
    }
  }
}
```

**Requirements**:

- The `mounts` field MUST only be specified for stdio servers (servers with `type: "stdio"` or servers without an explicit type, since stdio is the default)
- Each mount string MUST conform to the "host:container:mode" format
- The host path MUST be an absolute path
- The container path MUST be an absolute path
- The mode MUST be either "ro" (read-only) or "rw" (read-write)
- The gateway MUST validate mount format during configuration parsing
- Invalid mount formats MUST result in configuration validation errors

**Security Considerations**:

- Read-only mounts ("ro") SHOULD be preferred when the server only needs to read data
- Read-write mounts ("rw") SHOULD be limited to specific directories required for output
- Implementations SHOULD document any restrictions on host paths (e.g., disallowing system directories)
- Volume mounts provide access to host filesystem while maintaining container process isolation

**Use Cases**:

1. **Configuration Files**: Mount read-only configuration files into containers
   ```json
   "mounts": ["/etc/app/config.yaml:/app/config.yaml:ro"]
   ```

2. **Data Directories**: Provide access to large datasets without copying into containers
   ```json
   "mounts": ["/var/data/corpus:/data:ro"]
   ```

3. **Output Directories**: Allow containers to write results to host filesystem
   ```json
   "mounts": ["/var/output:/results:rw"]
   ```

4. **Shared Cache**: Share cache directories between container and host
   ```json
   "mounts": ["/tmp/cache:/app/cache:rw"]
   ```

### 4.2 Variable Expression Rendering

#### 4.2.1 Syntax

Configuration values MAY contain variable expressions using the syntax:

```
"${VARIABLE_NAME}"
```

#### 4.2.2 Resolution Behavior

The gateway MUST:

1. Detect variable expressions in configuration values
2. Replace expressions with values from process environment variables
3. FAIL IMMEDIATELY if a referenced variable is not defined
4. Log the undefined variable name to stdout as an error payload
5. Exit with non-zero status code

#### 4.2.3 Example

Configuration:

```json
{
  "mcpServers": {
    "github": {
      "container": "ghcr.io/github/github-mcp-server:latest",
      "env": {
        "GITHUB_TOKEN": "${GITHUB_PERSONAL_ACCESS_TOKEN}"
      }
    }
  }
}
```

If `GITHUB_PERSONAL_ACCESS_TOKEN` is not set in the environment:

```
Error: undefined environment variable referenced: GITHUB_PERSONAL_ACCESS_TOKEN
Required by: mcpServers.github.env.GITHUB_TOKEN
```

### 4.3 Configuration Validation

#### 4.3.1 Unknown Features

The gateway MUST reject configurations containing unrecognized fields at the top level with an error message indicating:

- The unrecognized field name
- The location in the configuration
- A suggestion to check the specification version

#### 4.3.2 Schema Validation

The gateway MUST validate:

- Required fields are present
- Field types match expected types
- Value constraints are satisfied (e.g., port ranges)
- Mutually exclusive fields are not both present

#### 4.3.3 Fail-Fast Requirements

If configuration is invalid, the gateway MUST:

1. Write a detailed error message to stdout as an error payload including:
   - The specific validation error
   - The location in the configuration (JSON path)
   - Suggested corrective action
2. Exit with status code 1
3. NOT start the HTTP server
4. NOT initialize any MCP servers

---

## 5. Protocol Behavior

For complete details on the Model Context Protocol, see the [Model Context Protocol Specification](https://spec.modelcontextprotocol.io/).

### 5.1 HTTP Server Interface

#### 5.1.1 Endpoint Structure

The gateway MUST expose the following HTTP endpoints:

```
POST /mcp/{server-name}
GET  /health
POST /close
```

#### 5.1.2 RPC Endpoint Behavior

**Request Format**:

```http
POST /mcp/{server-name} HTTP/1.1
Content-Type: application/json
Authorization: <agentId>

{
  "jsonrpc": "2.0",
  "method": "string",
  "params": {},
  "id": "string|number"
}
```

**Note**: The format of the `Authorization` header is implementation-dependent. Consult your gateway implementation's documentation for the expected format.

**Response Format**:

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "result": {},
  "id": "string|number"
}
```

**Error Response**:

```http
HTTP/1.1 500 Internal Server Error
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "error": {
    "code": -32603,
    "message": "Internal error",
    "data": {}
  },
  "id": "string|number"
}
```

#### 5.1.3 Close Endpoint Behavior

The gateway MUST provide a `/close` endpoint for graceful shutdown and resource cleanup.

**Request Format**:

```http
POST /close HTTP/1.1
Authorization: <agentId>
```

**Note**: The format of the `Authorization` header is implementation-dependent. Consult your gateway implementation's documentation for the expected format.

**Success Response**:

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "status": "closed",
  "message": "Gateway shutdown initiated",
  "serversTerminated": 3
}
```

**Already Closed Response**:

```http
HTTP/1.1 410 Gone
Content-Type: application/json

{
  "error": "Gateway has already been closed"
}
```

**Behavior Requirements**:

The gateway MUST perform the following actions when the `/close` endpoint is called:

1. **Stop Accepting New Requests**: Immediately reject any new RPC requests to `/mcp/{server-name}` endpoints with HTTP 503 Service Unavailable
2. **Complete In-Flight Requests**: Allow currently processing requests to complete (with a reasonable timeout, e.g., 30 seconds)
3. **Terminate All Containers**: Stop all running MCP server containers:
   - Send SIGTERM to each container process
   - Wait up to 10 seconds for graceful shutdown
   - Send SIGKILL if container does not stop within timeout
   - Log termination status for each server
4. **Release Resources**:
   - Close all file descriptors and network sockets
   - Clean up temporary files and logs
   - Release volume mounts
   - Free allocated memory
5. **Return Response**: Send success response before process exits
6. **Exit Process**: Exit the gateway process with status code 0

**Idempotency**:

The `/close` endpoint MUST be idempotent:
- First call: Initiates shutdown and returns HTTP 200
- Subsequent calls: Returns HTTP 410 Gone indicating gateway is already closed

**Authentication**:

The `/close` endpoint MUST require authentication when `gateway.agentId` or `gateway.agentIds` is configured. Requests without valid authentication MUST be rejected with HTTP 401 Unauthorized.

#### 5.1.4 Request Routing

The gateway MUST:

1. Extract server name from URL path
2. Validate server exists in configuration
3. Route request to appropriate backend server
4. Translate protocols if necessary (stdio ↔ HTTP)
5. Return response to client

### 5.2 Protocol Translation

#### 5.2.1 Stdio (Containerized) to HTTP

For containerized stdio-based servers, the gateway MUST:

1. Start the container on first request (lazy initialization)
2. Write JSON-RPC request to container's stdin
3. Read JSON-RPC response from container's stdout
4. Return HTTP response to client
5. Maintain container for subsequent requests
6. Buffer partial responses until complete JSON is received

The gateway SHALL NOT support non-containerized command execution. All stdio servers MUST be containerized.

#### 5.2.2 HTTP to HTTP

For HTTP-based servers, the gateway MUST:

1. Forward the JSON-RPC request to the server's URL
2. Apply any configured headers or authentication
3. Return the server's response to the client
4. Handle HTTP-level errors appropriately

**Connection Failure Handling**:

When a connection to an HTTP-based MCP server fails, the gateway MUST either:

1. **Pass through the error**: Return an appropriate error response to the client indicating the server is unavailable (e.g., HTTP 503 Service Unavailable or JSON-RPC error -32001 "Server unavailable")
2. **Handle with fallback**: Implement a fallback mechanism (e.g., retry logic, alternative server, cached response) and return a result to the client

The gateway MUST NOT silently ignore connection failures. All connection failures MUST result in either an error response to the client or successful fallback handling.

#### 5.2.3 Tool Signature Preservation

The gateway SHOULD NOT modify:

- Tool names
- Tool parameters
- Tool return values
- Method signatures

This ensures transparent proxying without name mangling or schema transformation.

### 5.3 Timeout Handling

#### 5.3.1 Startup Timeout

The gateway SHOULD enforce `startupTimeout` for server initialization:

1. Start timer when server container is launched
2. Wait for server ready signal (stdio) or successful health check (HTTP)
3. If timeout expires, kill server container and return error
4. Log timeout error with server name and elapsed time

#### 5.3.2 Tool Timeout

The gateway SHOULD enforce `toolTimeout` for individual tool invocations:

1. Start timer when RPC request is sent to server
2. Wait for complete response
3. If timeout expires, return timeout error to client
4. Log timeout with server name, method, and elapsed time

### 5.4 Stdout Configuration Output

After successful initialization, the gateway MUST:

1. Write a complete MCP server configuration to stdout
2. Include gateway connection details for each configured MCP server:
   - `type`: MUST be set to "http"
   - `url`: MUST be the gateway URL in format "http://{domain}:{port}/mcp/{server-name}"
   - `headers`: SHOULD include authorization headers required to connect to the gateway
     - `Authorization`: Contains the authentication credentials in an implementation-dependent format
   - `tools`: MAY be included to specify tool filters from the original configuration
   
   Example output configuration:
   ```json
   {
     "mcpServers": {
       "server-name": {
         "type": "http",
         "url": "http://{domain}:{port}/mcp/server-name",
         "headers": {
           "Authorization": "{agentId}"
         },
         "tools": ["*"]
       }
     }
   }
   ```
   
   The `headers` object SHOULD be present in each server configuration when authentication is required. The gateway is responsible for generating and including appropriate authentication credentials. The specific format of authentication headers is implementation-dependent.
   
   The `tools` field MAY be included in the output configuration to preserve tool filtering from the input configuration. When present, it specifies which tools are allowed for the server (`["*"]` for all tools, or a list of specific tool names).

3. Write configuration as a single JSON document
4. Flush stdout buffer
5. Continue serving requests

This allows clients to dynamically discover gateway endpoints and authentication credentials.

---

## 6. Server Isolation

### 6.1 Container Isolation

For stdio servers, the gateway MUST:

1. Launch each server in a separate container
2. Maintain isolated stdin/stdout/stderr streams
3. Prevent cross-container communication
4. Terminate containers on gateway shutdown (via `/close` endpoint or process termination)
5. Apply volume mounts as configured in the server's `mounts` field (Section 4.1.5)

All stdio-based MCP servers MUST be containerized to ensure:

- **Process Isolation**: Each container provides a separate process namespace
- **Resource Isolation**: Containers enforce CPU, memory, and filesystem boundaries
- **Network Isolation**: Containers provide isolated network namespaces
- **Security Boundaries**: Container runtimes enforce security policies and capabilities
- **Filesystem Isolation**: Container filesystems are isolated, with controlled access to host paths via volume mounts

The gateway SHALL NOT support non-containerized process execution for stdio servers.

**Volume Mount Isolation**:

When volume mounts are configured (Section 4.1.5):

- The gateway MUST mount the specified host paths into the container at the configured container paths
- The gateway MUST enforce the specified access mode (read-only "ro" or read-write "rw")
- Each container's mounts MUST be independent; mounts configured for one server MUST NOT affect other servers
- Volume mounts provide controlled access to host filesystem while maintaining container process isolation
- The gateway MUST validate mount paths and modes before container startup

### 6.2 Resource Isolation

The gateway MUST ensure:

- Each server has isolated environment variables within its container
- File descriptors are not shared between containers
- Network sockets are not shared (for HTTP servers)
- Container failures do not affect other containers

### 6.3 Security Boundaries

The gateway MUST NOT:

- Allow servers to access each other's configuration
- Share authentication credentials between servers
- Expose server implementation details to clients
- Allow cross-server tool invocations

---

## 7. Authentication

### 7.1 Authorization Header Format

The MCP Gateway uses a simple agent identifier authentication scheme. When `gateway.agentId` or `gateway.agentIds` is configured:

- The `Authorization` header contains an agent/session identifier value (the configured `agentId`, or one of the configured `agentIds` entries)
- Implementations MAY use different formats (e.g., direct value or a scheme-prefixed value)
- The specific format is implementation-dependent

> [!WARNING]
> The gateway agent identifier should not be treated as a secure lock against code already running inside the agent container. A sufficiently capable agent may extract it from in-memory process state or other runtime channels. Treat this identifier as leaked by design and rely on container isolation, network controls, and staged permission boundaries for defense in depth.

**Example formats**:

```http
Authorization: my-secret-agent-id-12345
```

or

```http
Authorization: Token my-other-agent-id-67890
```

This authentication scheme provides flexibility for different implementation requirements.

### 7.2 Agent Identifier Authentication

When `gateway.agentId` or `gateway.agentIds` is configured, the gateway MUST:

1. Require `Authorization` header on all RPC requests to `/mcp/{server-name}` and `/close` endpoints
   - The specific format of the Authorization header is implementation-dependent
   - Implementations SHOULD document their expected format
2. Reject requests with missing or invalid tokens (HTTP 401)
3. Reject requests with malformed Authorization headers (HTTP 400)
4. NOT log agent identifiers in plaintext

> [!NOTE]
> This section covers configuration parsing only. Runtime authentication and routing of individual `agentIds` entries to independent DIFC session state is out of scope for this specification and is tracked as follow-up implementation work.

### 7.3 Temporary Agent Identifiers

A gateway configuration MUST always specify exactly one of `agentId` or `agentIds` (see Section 4.1.3.9); the gateway MUST NOT silently generate an identifier when neither is provided. Callers that need a short-lived identity (for example, an ephemeral session) SHOULD generate their own temporary identifier and supply it as `agentId` before starting the gateway.

### 7.4 Authentication Exemptions

The following endpoints MUST NOT require authentication:

- `/health`

### 7.5 Trusted Bot Identity Configuration

The `gateway.trustedBots` field allows workflow authors to pass additional trusted bot identity strings to the gateway via the generated gateway configuration file. The gateway merges these entries with its own built-in trusted identity list.

`gateway.trustedBots` is **additive** — it extends the gateway's built-in list but cannot remove entries from it.

Workflow authors set this via the `sandbox.mcp.trusted-bots` frontmatter field; the compiler translates it into the `trustedBots` array in the generated `gateway` section of the MCP config file.

---

### 7.6 Upstream Authentication (OIDC)

HTTP MCP servers MAY configure upstream authentication using the `auth` field. When present, the gateway dynamically acquires tokens and injects them as `Authorization: Bearer` headers on every outgoing request to the server.

#### 7.6.1 GitHub Actions OIDC

When `auth.type` is `"github-oidc"`, the gateway acquires short-lived JWTs from the GitHub Actions OIDC endpoint. This requires the workflow to have `permissions: { id-token: write }`.

**Configuration**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Must be `"github-oidc"` |
| `audience` | string | No | The intended audience (`aud` claim) for the OIDC token. Defaults to the server `url` if omitted. |

**Environment Variables** (set automatically by GitHub Actions):

| Variable | Description |
|----------|-------------|
| `ACTIONS_ID_TOKEN_REQUEST_URL` | OIDC token endpoint URL |
| `ACTIONS_ID_TOKEN_REQUEST_TOKEN` | Bearer token for authenticating to the OIDC endpoint |

**Behavior**:

1. On startup, the gateway checks for `ACTIONS_ID_TOKEN_REQUEST_URL`. If set, an OIDC provider is initialized.
2. If a server has `auth.type: "github-oidc"` but the OIDC env vars are missing, the gateway MUST log an error at startup and MUST return an error when the server is first accessed.
3. Tokens are cached per audience and refreshed proactively before expiry (60-second margin).
4. The OIDC `Authorization: Bearer` header overwrites any static `Authorization` header from the `headers` field. Other static headers pass through.
5. The gateway does NOT verify JWT signatures — it acts as a token acquirer/forwarder. The downstream MCP server is the relying party and MUST validate the token.

**Example** (JSON stdin format):

```json
{
  "mcpServers": {
    "my-mcp-server": {
      "type": "http",
      "url": "https://my-server.example.com/mcp",
      "auth": {
        "type": "github-oidc",
        "audience": "https://my-server.example.com"
      }
    }
  }
}
```

**Example with audience defaulting to URL**:

```json
{
  "mcpServers": {
    "my-mcp-server": {
      "type": "http",
      "url": "https://my-server.example.com/mcp",
      "auth": {
        "type": "github-oidc"
      }
    }
  }
}
```

In this case, the audience defaults to `"https://my-server.example.com/mcp"`.

**Frontmatter Example** (workflow author):

```yaml
tools:
  mcp-servers:
    my-mcp-server:
      type: http
      url: "https://my-server.example.com/mcp"
      auth:
        type: github-oidc
        audience: "https://my-server.example.com"
```

#### 7.6.2 Interaction with Static Headers

When both `headers` and `auth` are configured:

- Static headers from `headers` are applied first
- The OIDC token overwrites the `Authorization` header
- All other static headers (e.g., `X-Custom-Header`) pass through unchanged

This allows combining OIDC auth with non-auth headers:

```json
{
  "type": "http",
  "url": "https://my-server.example.com/mcp",
  "headers": {
    "X-Custom-Header": "custom-value"
  },
  "auth": {
    "type": "github-oidc"
  }
}
```

#### 7.6.3 Validation Rules

- `auth` is only valid on HTTP servers (`type: "http"`). Stdio servers with `auth` MUST be rejected with a validation error.
- `auth.type` is required when `auth` is present. Empty type MUST be rejected.
- Unsupported `auth.type` values MUST be rejected with a descriptive error.

---

## 8. Health Monitoring

### 8.1 Health Endpoints

#### 8.1.1 General Health (`/health`)

```http
GET /health HTTP/1.1
```

**Response Format**:

```json
{
  "status": "healthy" | "unhealthy",
  "specVersion": "string",
  "gatewayVersion": "string",
  "servers": {
    "server-name": {
      "status": "running" | "stopped" | "error",
      "uptime": 12345
    }
  }
}
```

**Response Fields**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `status` | string | Yes | Overall gateway health status: "healthy" or "unhealthy" |
| `specVersion` | string | Yes | MCP Gateway Specification version (e.g., "1.3.0") |
| `gatewayVersion` | string | Yes | Gateway implementation version (e.g., "0.1.0") |
| `servers` | object | Yes | Map of server names to their health status |
| `servers[name].status` | string | Yes | Server status: "running", "stopped", or "error" |
| `servers[name].uptime` | integer | No | Server uptime in seconds |

**Requirements**:

The gateway MUST include the following version information in the `/health` endpoint response:

1. **`specVersion`**: The version of this MCP Gateway Specification that the implementation conforms to. This field MUST use semantic versioning (MAJOR.MINOR.PATCH format).
2. **`gatewayVersion`**: The version of the gateway implementation itself. This field MUST use semantic versioning and represents the specific build or release version of the gateway software.

These version fields enable clients to:
- Verify specification compatibility
- Detect implementation versions for debugging
- Track deployment versions across environments
- Ensure feature availability based on specification version

### 8.2 Health Check Behavior

The gateway SHOULD:

1. Periodically check server health (every 30 seconds)
2. Restart failed containerized stdio servers automatically
3. Mark HTTP servers unhealthy if unreachable
4. Include health status in `/health` response
5. Update readiness based on critical server status

> [!TIP]
> To inspect MCP server health for a specific workflow run at runtime, use `gh aw audit <run-id>`. The **MCP Server Health** section of the audit report shows connection failures, timeout errors, tool call counts, and error rates per server — providing a post-run view of gateway behavior. For recurring MCP failures, pass two run IDs to `gh aw audit` (e.g. `gh aw audit <base-id> <compare-id>`) to compare MCP tool usage between runs and identify regressions. See [Audit Commands](/gh-aw/reference/audit/).

---

## 9. Error Handling

### 9.1 Startup Failures

If any configured server fails to start, the gateway MUST:

1. Write detailed error to stdout as an error payload including:
   - Server name
   - Container image or URL attempted
   - Error message from server container
   - Environment variable status
   - Stdout/stderr from failed container
2. Exit with status code 1
3. NOT start the HTTP server

### 9.2 Runtime Errors

For runtime errors, the gateway MUST:

1. Log errors to stdout as error payloads with:
   - Timestamp
   - Server name
   - Request ID
   - Error details
2. Return JSON-RPC error response to client
3. Continue serving other requests
4. Attempt to restart failed containerized stdio servers

### 9.3 Error Response Format

JSON-RPC errors MUST follow this structure:

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32000,
    "message": "Server error",
    "data": {
      "server": "server-name",
      "detail": "Specific error information"
    }
  },
  "id": "request-id"
}
```

Error codes:

- `-32700`: Parse error
- `-32600`: Invalid request
- `-32601`: Method not found
- `-32603`: Internal error
- `-32000` to `-32099`: Server errors

### 9.4 Graceful Degradation

The gateway SHOULD:

1. Continue serving healthy servers when others fail
2. Return specific errors for unavailable servers
3. Attempt automatic recovery for transient failures
4. Provide clear client feedback about server status

---

## 10. Guard Policy

### 10.1 Overview

The guard policy controls which GitHub content the gateway exposes to the agent based on **integrity** — a trust level derived from the content author's association with the repository and the content's merge status. Guard policy configuration is specific to the GitHub MCP server and is passed to the gateway alongside the standard server configuration.

This section specifies the guard policy fields supported by the gateway, their semantics, and the algorithm used to compute effective integrity for each item. For user-facing configuration documentation, see the [GitHub Integrity Filtering Reference](/gh-aw/reference/integrity/).

### 10.2 Integrity Levels

The gateway recognizes the following integrity levels, ordered from highest to lowest:

```text
merged > approved > unapproved > none > blocked
```

| Level | Meaning |
|-------|---------|
| `merged` | Pull requests merged into the target branch; commits reachable from the default branch |
| `approved` | Content from `OWNER`, `MEMBER`, or `COLLABORATOR`; non-fork PRs on public repos; all items in private repos; recognized platform bots; users in `trusted-users` |
| `unapproved` | Content from `CONTRIBUTOR` or `FIRST_TIME_CONTRIBUTOR` |
| `none` | All other content, including `FIRST_TIMER` and users with no association |
| `blocked` | Content from users in `blocked-users` — always denied, cannot be promoted |

`blocked` is not a configurable `min-integrity` value. It is assigned automatically to items from users in `blocked-users` and is always denied regardless of the configured threshold.

### 10.3 Guard Policy Fields

Guard policy fields are passed to the gateway as part of the GitHub MCP server configuration under a dedicated guard policy object. The following fields are supported:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `min-integrity` | string | Conditional | `approved` (public repos) | Minimum integrity threshold: `merged`, `approved`, `unapproved`, or `none`. Required when any other guard policy field is used. |
| `allowed-repos` | string or array | No | `"all"` | Repository scope: `"all"`, `"public"`, or array of patterns (e.g., `["myorg/*"]`) |
| `blocked-users` | array or expression | No | `[]` | GitHub usernames unconditionally denied, regardless of any other policy |
| `trusted-users` | array or expression | No | `[]` | GitHub usernames elevated to `approved` integrity regardless of their author association |
| `approval-labels` | array or expression | No | `[]` | GitHub label names that promote items to `approved` integrity |
| `refusal-labels` | array or expression | No | `[]` | GitHub label names that downgrade items to `none` integrity, overriding any promotion |

Compiler-emitted gateway shape for the built-in GitHub server:

```json
{
  "mcpServers": {
    "github": {
      "guard-policies": {
        "allow-only": {
          "repos": "all",
          "min-integrity": "approved",
          "blocked-users": "${{ steps.parse-guard-vars.outputs.blocked_users }}",
          "trusted-users": "${{ steps.parse-guard-vars.outputs.trusted_users }}",
          "approval-labels": "${{ steps.parse-guard-vars.outputs.approval_labels }}"
        }
      }
    }
  }
}
```

:::caution
When `tools.github.lockdown: true` is set, guard-policy fields (`allowed-repos`, `min-integrity`, `blocked-users`, `trusted-users`, `approval-labels`) are ignored at runtime. Lockdown takes precedence and the compiler emits a warning for this combination.
:::

### 10.4 Effective Integrity Computation

The gateway MUST compute each item's effective integrity in the following order:

1. **Base integrity**: Derived from GitHub metadata (author association, merge status, repository visibility).
2. **`blocked-users` check**: If the content author is listed in `blocked-users`, effective integrity → `blocked` (unconditionally denied; skip remaining steps).
3. **`refusal-labels` check**: If the item carries any label present in `refusal-labels`, effective integrity → `none` (overrides any promotion from steps 4–5).
4. **`trusted-users` check**: If the content author is listed in `trusted-users`, effective integrity → `max(base, approved)`.
5. **`approval-labels` check**: If the item carries any label present in `approval-labels`, effective integrity → `max(base, approved)`.
6. **Default**: effective integrity → base.

The `min-integrity` threshold check is applied after step 6. Items whose effective integrity is below the threshold MUST be removed before the response is returned to the agent.

**Key constraints**:

- `blocked-users` MUST take precedence over all other policy fields. Blocked items MUST be denied even if they carry an `approval-labels` label or the author is in `trusted-users`.
- `refusal-labels` MUST override promotion from `trusted-users` and `approval-labels`. An item bearing a refusal label and an approval label simultaneously MUST have effective integrity `none`.
- `trusted-users` and `approval-labels` are **promotion only** — they MUST NOT lower integrity. `max(base, approved)` ensures existing higher integrity (`merged`) is preserved.
- `refusal-labels` is **demotion only** — it sets integrity to `none` and MUST NOT affect `blocked` items.

### 10.5 `approval-labels` Field

`approval-labels` lists GitHub labels that promote matching items to `approved` integrity. When an item has one of these labels, its effective integrity becomes `max(base, approved)`, so `merged` items stay `merged`. `blocked-users` still takes precedence, and `refusal-labels` still wins if both kinds of label are present.

**Use case**: Human-review gate workflows where a trusted reviewer adds a label to signal that an external contribution is safe for the agent.

**Configuration Example**:

```yaml
tools:
  github:
    min-integrity: approved
    approval-labels:
      - "human-reviewed"
      - "safe-for-agent"
```

**Compliance Test**: T-GP-003 — Approval label promotion

### 10.6 `refusal-labels` Field

`refusal-labels` is the inverse of `approval-labels`: any matching label downgrades an item to `none`, regardless of author association or promotion from `trusted-users` or `approval-labels`. It overrides both promotion paths, while `blocked-users` still takes precedence and remains `blocked`.

**Use case**: Suppressing specific items from the agent — for example, issues flagged for security review or pull requests pending a manual compliance check — even when the workflow's `min-integrity` would otherwise allow them.

**Configuration Example**:

```yaml
tools:
  github:
    min-integrity: approved
    refusal-labels:
      - "needs-security-review"
      - "do-not-automate"
```

**Combined Example** (approval and refusal labels together):

```yaml
tools:
  github:
    min-integrity: approved
    approval-labels:
      - "human-reviewed"
    refusal-labels:
      - "needs-security-review"
```

In this configuration, `human-reviewed` promotes items to `approved` unless `needs-security-review` is also present, in which case the item is downgraded to `none`.

**Requirements**:

- The gateway MUST apply `refusal-labels` checks before `trusted-users` and `approval-labels` checks in the effective integrity computation.
- The gateway MUST set effective integrity to `none` for any item bearing a label present in `refusal-labels`.
- The `refusal-labels` value MUST support both a literal array of strings and a GitHub Actions expression resolving to a comma- or newline-separated list.
- The gateway MUST treat an empty `refusal-labels` list as a no-op.

**Compliance Tests**: T-GP-004 through T-GP-008

### 10.7 Centralized Management via GitHub Variables

Each guard policy list field (`blocked-users`, `trusted-users`, `approval-labels`, `refusal-labels`) MAY be extended centrally using GitHub repository or organization variables. The runtime MUST union the per-workflow values with the corresponding variable at runtime.

| Workflow field | GitHub variable |
|----------------|----------------|
| `blocked-users` | `GH_AW_GITHUB_BLOCKED_USERS` |
| `trusted-users` | `GH_AW_GITHUB_TRUSTED_USERS` |
| `approval-labels` | `GH_AW_GITHUB_APPROVAL_LABELS` |
| `refusal-labels` | `GH_AW_GITHUB_REFUSAL_LABELS` |

Variables are split on commas and newlines, trimmed of whitespace, and deduplicated. The union of workflow-declared values and variable values forms the effective list used at runtime.

### 10.8 Write-Sink Guard Policy (`sink-visibility`)

The write-sink guard policy controls whether an agent may write to the safe-outputs sink based on **DIFC secrecy** — a data-flow integrity property that prevents private-origin data from leaking into public repositories. This addresses the GitLost vulnerability class, where an agent accumulates private-data secrecy tags by reading private repositories and then writes that tainted content to a public output.

#### 10.8.1 Overview

The write-sink guard policy applies at the **output side**: it is checked immediately before the safe-outputs MCP server accepts a write operation. Unlike the integrity guard policy (Section 10), which filters *inputs* to the agent, the write-sink guard filters *outputs* from the agent.

**Key assumption**: The safe-outputs target is always the workflow's own repository (`GITHUB_REPOSITORY`). Under this assumption, "sink visibility" and "workflow repository visibility" are the same value. The runtime determines sink visibility by reading `GITHUB_REPOSITORY` and calling `GET /repos/{owner}/{repo}` at startup.

#### 10.8.2 `sink-visibility` Field

The `write-sink` object inside `guard-policies` for a safe-outputs (or equivalent) MCP server accepts the following fields:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `accept` | array[string] | Yes | — | Secrecy tag patterns that are permitted to write to this sink. Use `["*"]` to accept all secrecy levels. |
| `sink-visibility` | string | No | (omitted) | Declares the visibility of the safe-outputs target repository. Values: `"public"`, `"private"`, `"internal"`. When `"public"`, any agent with non-empty secrecy is blocked. |

**Values**:

| Value | Enforcement |
|-------|-------------|
| `"public"` | Target is a public repo; agents with **non-empty secrecy** are BLOCKED. Resource secrecy is unconditionally set to empty (`{}`), overriding `accept` patterns. |
| `"private"` | Target is a private repo; standard `accept`-pattern matching applies. |
| `"internal"` | Target is an org-internal repo; semantically equivalent to `"private"` — standard `accept`-pattern matching applies. |
| (omitted) | Backward-compatible default; standard `accept`-pattern matching applies. |

**Enforcement model**: Only `"public"` alters DIFC enforcement. Both `"private"` and `"internal"` are semantically equivalent to each other and to the omitted case. The `"internal"` value exists for configuration fidelity (matching GitHub's three-tier visibility model) rather than distinct enforcement behavior. Implementations MUST treat any non-`"public"` value or an omitted `sink-visibility` as "use accept patterns normally."

#### 10.8.3 Interaction with `accept`

When `sink-visibility` is `"public"`, the `accept` array is ignored for enforcement: resource secrecy is set to empty (`{}`), so the DIFC write check (`agentSecrecy ⊆ resourceSecrecy`) fails for any agent with non-empty secrecy. `accept` remains syntactically required but has no runtime effect. When `sink-visibility` is `"private"`, `"internal"`, or omitted, `accept` alone determines resource secrecy and write eligibility.

**Precedence**: `sink-visibility: "public"` is a hard override that trumps `accept`.

#### 10.8.4 Runtime Verification (Defense-in-Depth)

The gateway performs a runtime visibility check at startup as defense-in-depth:

1. Reads `GITHUB_REPOSITORY` and calls `GET /repos/{owner}/{repo}` to verify actual visibility.
2. If the repo is public but `sink-visibility` is set to `"private"` or `"internal"` → overrides `sink-visibility` to `"public"` with a warning.
3. **Skipped** when `sink-visibility` is omitted (preserves backward compatibility by skipping this runtime verification step; defaulting behavior is defined in Sections 10.8.6 and 10.8.7).
4. **Skipped** when `GITHUB_REPOSITORY` or the GitHub token is unavailable.
5. Falls back to the configured value on API errors (non-fatal); this is logged as a warning. When `sink-visibility` is omitted, fallback means enforcement remains skipped.
6. Never relaxes: a configured `"public"` stays `"public"` even if the repo is actually private.

#### 10.8.5 Compiler Behavior

The gh-aw compiler automatically sets `sink-visibility` as a runtime expression in the generated gateway config when the GitHub tool is configured with safe-outputs:

```
"sink-visibility": ${{ toJSON(steps.determine-automatic-lockdown.outputs.visibility) }}
```

This runtime expression resolves to the actual repository visibility at workflow execution time (as determined by the automatic lockdown detection step). Workflow authors do not need to set `sink-visibility` manually — it is derived from the repository context automatically.

**Configuration Example** (resolved runtime value — `"public"` shown as an example after the expression is evaluated):

```json
{
  "mcpServers": {
    "safe-outputs": {
      "container": "ghcr.io/github/safe-outputs:latest",
      "guard-policies": {
        "write-sink": {
          "accept": ["*"],
          "sink-visibility": "public"
        }
      }
    }
  }
}
```

**Compliance Tests**: T-WS-001 through T-WS-003 (Section 11.1.12)

#### 10.8.6 Default Sink Visibility (Security-by-Default)

Non-safe-outputs write-sink servers default to `sink-visibility="public"` when no explicit value is configured. This assumes external sinks such as Playwright, Slack, or third-party services release data publicly unless proven otherwise.

The default applies only when the server is not `safe-outputs` (or legacy `safeoutputs`), the server is not listed in `gateway.sinkVisibilityExemptServers`, that exemption list does not contain `"*"`, and `forcePublicRepos` is not `false`.

**Rationale**: Treating external sinks as public by default helps prevent accidental data exfiltration through channels outside the workflow author's control.

#### 10.8.7 Safe-Outputs Runtime Safety Net

As a defense-in-depth measure, the gateway forces `sink-visibility="public"` for `safe-outputs` (or legacy `safeoutputs`) when no `sink-visibility` is configured, `GITHUB_REPOSITORY` and a GitHub token are available, and the GitHub API confirms the workflow repository is public. This keeps the gateway self-defending when the compiler omits the setting for legacy workflows or misconfiguration.

**Note**: This safety net does NOT fire when `sink-visibility` is explicitly set to `"private"` or `"internal"`; Section 10.8.4 handles that case.

### 10.9 Cross-Visibility Opt-Out (`private-to-public-flows`)

Workflow authors who intentionally allow private→public data flows can opt out of cross-visibility protections by declaring `private-to-public-flows` under `tools.github` in workflow frontmatter. It accepts either `allow` for a blanket opt-out that disables both `forcePublicRepos` and sink-visibility enforcement, or a list of server IDs to exempt only those servers from default sink-visibility enforcement.

#### 10.9.1 Workflow Frontmatter

**Blanket opt-out:**
```yaml
---
tools:
  github:
    private-to-public-flows: allow
---
```

**Targeted opt-out (list form):**
```yaml
---
tools:
  github:
    private-to-public-flows:
      - github
      - my-custom-server
mcp-servers:
  my-custom-server:
    type: http
    url: "http://localhost:9000/mcp"
---
```

#### 10.9.2 Constraints

Blanket `allow` is incompatible with `guards_mode: strict`, so the compiler MUST reject that combination at compile time. The list form remains compatible with `strict` because it relaxes only the default sink-visibility for named servers while leaving other protections in place. In list form, every server ID MUST map to an actual MCP server declared in the workflow's `tools` or `mcp-servers` list; unknown IDs MUST be rejected at compile time. With non-strict mode (`filter` or `propagate`), the blanket form disables both the forced `repos="public"` override (Section 4.1.3.8) and the `sink-visibility` runtime-verification override (Section 10.8.4).

#### 10.9.3 Compiler Responsibilities

When the compiler encounters `private-to-public-flows`, it MUST validate the chosen form, emit the matching gateway configuration, and record the opt-out in the audit trail.

For blanket `allow`, the compiler MUST reject `guards_mode: strict`, set `gateway.forcePublicRepos: false` in the generated JSON stdin config, and skip setting `sink-visibility` in write-sink guard policies even if the target repo is public at compile time.

For list form, the compiler MUST verify that every listed server ID exists in the workflow's declared `tools` or `mcp-servers` list, set `gateway.sinkVisibilityExemptServers` to that list in the generated JSON stdin config, and leave `forcePublicRepos` unchanged.

#### 10.9.4 Interaction Matrix

| `guards_mode` | `private-to-public-flows` | Forced `repos="public"` | Default `sink-visibility` enforced | Strict-mode compatible |
|---|---|---|---|---|
| `strict` | `allow` | ❌ **Rejected** (compile error) | — | ❌ |
| `strict` | `[servers...]` | ✅ Yes | ✅ Yes (except listed servers) | ✅ |
| `strict` | *(omitted)* | ✅ Yes | ✅ Yes | ✅ |
| `filter` / `propagate` | `allow` | ❌ Disabled | ❌ Disabled | N/A |
| `filter` / `propagate` | `[servers...]` | ✅ Yes | ✅ Yes (except listed servers) | N/A |
| `filter` / `propagate` | *(omitted)* | ✅ Yes | ✅ Yes | N/A |

#### 10.9.5 Security Rationale

This opt-out exists for workflows that legitimately need to read from private repos and post summaries to public issue trackers, aggregate private data into public dashboards, or cross-post between private and public repos.

The strict-mode incompatibility ensures organizations requiring maximum security cannot accidentally enable this escape hatch.

**Compliance Test**: T-WS-005 (Section 11.1.12)

---

## 11. Compliance Testing

### 11.1 Test Suite Requirements

A conforming implementation MUST pass the following test categories:

#### 11.1.1 Configuration Tests

- **T-CFG-001**: Valid stdio server configuration
- **T-CFG-002**: Valid HTTP server configuration
- **T-CFG-003**: Variable expression resolution
- **T-CFG-004**: Undefined variable error detection
- **T-CFG-005**: Payload directory path validation (absolute paths)
- **T-CFG-006**: Unknown field rejection
- **T-CFG-007**: Missing required field detection
- **T-CFG-008**: Invalid type detection
- **T-CFG-009**: Port range validation
- **T-CFG-010**: Valid custom server type with registered schema
- **T-CFG-011**: Reject custom type without schema registration
- **T-CFG-012**: Validate custom configuration against registered schema
- **T-CFG-013**: Reject custom type conflicting with reserved types (stdio/http)
- **T-CFG-014**: Custom schema URL fetch and cache
- **T-CFG-015**: Valid volume mount format (host:container:mode)
- **T-CFG-016**: Reject invalid mount format (missing components)
- **T-CFG-017**: Reject invalid mount mode (not "ro" or "rw")
- **T-CFG-018**: Multiple mounts for single stdio server
- **T-CFG-019**: Reject mounts for HTTP servers (stdio only)

#### 11.1.2 Protocol Translation Tests

- **T-PTL-001**: Stdio request/response cycle
- **T-PTL-002**: HTTP passthrough
- **T-PTL-003**: Tool signature preservation
- **T-PTL-004**: Concurrent request handling
- **T-PTL-005**: Large payload handling
- **T-PTL-006**: Partial response buffering
- **T-PTL-007**: HTTP connection failure error response
- **T-PTL-008**: HTTP connection failure is not silently ignored

#### 11.1.3 Isolation Tests

- **T-ISO-001**: Container isolation verification
- **T-ISO-002**: Environment isolation verification
- **T-ISO-003**: Credential isolation verification
- **T-ISO-004**: Cross-container communication prevention
- **T-ISO-005**: Container failure isolation
- **T-ISO-006**: Volume mount isolation (mounts do not affect other containers)
- **T-ISO-007**: Volume mount access mode enforcement (ro vs rw)
- **T-ISO-008**: Volume mount path independence between containers

#### 11.1.4 Authentication Tests

- **T-AUTH-001**: Valid token acceptance
- **T-AUTH-002**: Invalid token rejection
- **T-AUTH-003**: Missing token rejection
- **T-AUTH-004**: Health endpoint exemption
- **T-AUTH-005**: Token rotation support
- **T-AUTH-006**: Trusted bot identity configuration — `trustedBots` entries are present in the generated gateway config and merged with the gateway's built-in list

#### 11.1.5 Timeout Tests

- **T-TMO-001**: Startup timeout enforcement
- **T-TMO-002**: Tool timeout enforcement
- **T-TMO-003**: Timeout error messaging
- **T-TMO-004**: Partial response timeout
- **T-TMO-005**: Concurrent timeout handling

#### 11.1.6 Health Monitoring Tests

- **T-HLT-001**: Health endpoint availability
- **T-HLT-002**: Liveness probe accuracy
- **T-HLT-003**: Readiness probe accuracy
- **T-HLT-004**: Server status reporting
- **T-HLT-005**: Automatic restart behavior
- **T-HLT-006**: Health response includes specVersion field
- **T-HLT-007**: Health response includes gatewayVersion field
- **T-HLT-008**: specVersion uses semantic versioning format
- **T-HLT-009**: gatewayVersion uses semantic versioning format

#### 11.1.7 Configuration Output Tests

- **T-OUT-001**: Gateway outputs valid JSON configuration to stdout
- **T-OUT-002**: Output configuration includes all configured servers
- **T-OUT-003**: Each server configuration has "type": "http"
- **T-OUT-004**: Each server configuration has correct "url" format
- **T-OUT-005**: Each server configuration includes "headers" object when authentication is required
- **T-OUT-006**: Authorization header is present when authentication is configured
- **T-OUT-007**: Output configuration is complete before health endpoint becomes available

#### 11.1.8 Error Handling Tests

- **T-ERR-001**: Startup failure reporting
- **T-ERR-002**: Runtime error handling
- **T-ERR-003**: Invalid request handling
- **T-ERR-004**: Server crash recovery
- **T-ERR-005**: Error message quality

#### 11.1.9 Gateway Lifecycle Tests

- **T-LIFE-001**: Close endpoint authentication
- **T-LIFE-002**: Close endpoint success response
- **T-LIFE-003**: Close endpoint idempotency (returns 410 on subsequent calls)
- **T-LIFE-004**: Container termination on close
- **T-LIFE-005**: Resource cleanup on close
- **T-LIFE-006**: In-flight request handling during shutdown
- **T-LIFE-007**: New requests rejected after close initiated

#### 11.1.10 OpenTelemetry Tests

- **T-OTEL-001**: Gateway starts successfully when `opentelemetry` is omitted
- **T-OTEL-002**: Gateway starts successfully when `opentelemetry` is configured with a valid endpoint
- **T-OTEL-003**: Reject `opentelemetry` configuration with missing `endpoint` field
- **T-OTEL-004**: Reject `opentelemetry` configuration with a non-HTTP(S) endpoint
- **T-OTEL-005**: Span emitted for each MCP tool invocation with required attributes (`mcp.server`, `mcp.method`, `mcp.tool`, `http.status_code`)
- **T-OTEL-006**: When `OTEL_EXPORTER_OTLP_HEADERS` env var is set, headers are sent with every OTLP export request
- **T-OTEL-007**: W3C `traceparent` context propagated when both `traceId` and `spanId` are configured
- **T-OTEL-008**: Gateway generates random `spanId` in `traceparent` when only `traceId` is provided
- **T-OTEL-009**: Export failure does not affect MCP request processing or gateway availability
- **T-OTEL-010**: `serviceName` is reflected in `service.name` resource attribute of emitted spans

#### 11.1.11 Guard Policy Tests

- **T-GP-001**: Items from `blocked-users` are denied regardless of `min-integrity` setting
- **T-GP-002**: Items from `trusted-users` receive effective integrity `approved` when base is lower
- **T-GP-003**: Items bearing an `approval-labels` label receive effective integrity `approved` when base is lower
- **T-GP-004**: Items bearing a `refusal-labels` label receive effective integrity `none`
- **T-GP-005**: `refusal-labels` overrides `approval-labels` — an item with both a refusal label and an approval label has effective integrity `none`
- **T-GP-006**: `refusal-labels` overrides `trusted-users` — an item from a trusted user with a refusal label has effective integrity `none`
- **T-GP-007**: `blocked-users` takes precedence over `refusal-labels` — blocked items remain `blocked`
- **T-GP-008**: Empty `refusal-labels` list results in no items being downgraded
- **T-GP-009**: `refusal-labels` accepts a GitHub Actions expression (comma- or newline-separated list)
- **T-GP-010**: `min-integrity: none` allows items at `none` integrity through; items downgraded by `refusal-labels` to `none` are visible when `min-integrity: none`

#### 11.1.12 Write-Sink Guard Policy Tests

- **T-WS-001**: Agent with non-empty secrecy is blocked when `sink-visibility` is `"public"` — DIFC write check fails regardless of `accept` patterns
- **T-WS-002**: Agent with empty secrecy is allowed when `sink-visibility` is `"public"` — DIFC write check passes
- **T-WS-003**: `accept` patterns are used normally when `sink-visibility` is `"private"`, `"internal"`, or omitted
- **T-WS-004**: Gateway overrides allow-only policy to `repos="public"` at startup when repository is public and `forcePublicRepos` is `true`
- **T-WS-005**: `forcePublicRepos: false` disables the runtime override — compiled allow-only policy is used as-is
- **T-WS-006**: Runtime verification overrides `sink-visibility` to `"public"` when configured value is `"private"` or `"internal"` but actual repo is public
- **T-WS-007**: Runtime verification is skipped when `sink-visibility` is omitted (backward compatibility)
- **T-WS-008**: Configured `"public"` is never relaxed by runtime verification even if API returns private visibility

### 11.2 Compliance Checklist

| Requirement | Test ID | Level | Status |
|-------------|---------|-------|--------|
| Configuration parsing | T-CFG-* | 1 | Required |
| Variable expressions | T-CFG-003, T-CFG-004 | 3 | Optional |
| Stdio transport | T-PTL-001 | 1 | Required |
| HTTP transport | T-PTL-002 | 2 | Standard |
| Authentication | T-AUTH-* | 2 | Standard |
| Timeout handling | T-TMO-* | 3 | Optional |
| Health monitoring | T-HLT-* | 2 | Standard |
| Server isolation | T-ISO-* | 1 | Required |
| Configuration output | T-OUT-* | 1 | Required |
| Error handling | T-ERR-* | 1 | Required |
| Gateway lifecycle | T-LIFE-* | 2 | Standard |
| OpenTelemetry | T-OTEL-* | 3 | Optional |
| Guard policy | T-GP-* | 2 | Standard |
| Write-sink guard policy | T-WS-* | 2 | Standard |

### 11.3 Test Execution

Implementations SHOULD provide:

1. Automated test runner
2. Test result reporting in standard format (e.g., TAP, JUnit)
3. Test fixtures for common scenarios
4. Performance benchmarks
5. Conformance report generation

---

## Appendices

### Appendix A: Example Configurations

#### A.1 Basic Containerized Stdio Server

```json
{
  "mcpServers": {
    "example": {
      "container": "ghcr.io/example/mcp-server:latest",
      "entrypointArgs": ["--verbose"],
      "env": {
        "API_KEY": "${MY_API_KEY}"
      }
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "gateway-secret-token"
  }
}
```

#### A.2 Server with Volume Mounts and Custom Entrypoint

```json
{
  "mcpServers": {
    "data-server": {
      "container": "ghcr.io/example/data-mcp:latest",
      "entrypoint": "/custom/entrypoint.sh",
      "entrypointArgs": ["--config", "/app/config.json"],
      "mounts": [
        "/host/data:/container/data:ro",
        "/host/config:/container/config:rw"
      ],
      "type": "stdio"
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "gateway-secret-token"
  }
}
```

#### A.3 Mixed Transport Configuration

```json
{
  "mcpServers": {
    "local-server": {
      "container": "ghcr.io/example/python-mcp:latest",
      "entrypointArgs": ["--config", "/app/config.json"],
      "type": "stdio",
      "tools": ["read_file", "write_file", "list_directory"]
    },
    "remote-server": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${API_TOKEN}"
      },
      "tools": ["*"]
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "startupTimeout": 60,
    "toolTimeout": 120
  }
}
```

#### A.4 GitHub MCP Server (Containerized)

```json
{
  "mcpServers": {
    "github": {
      "container": "ghcr.io/github/github-mcp-server:latest",
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost"
  }
}
```

#### A.5 Servers with Registry Field

The `registry` field documents the MCP server's installation location in an MCP registry. This is useful for tooling discovery and version management.

```json
{
  "mcpServers": {
    "markitdown": {
      "registry": "https://api.mcp.github.com/v0/servers/microsoft/markitdown",
      "container": "node:lts-alpine",
      "entrypointArgs": ["npx", "-y", "@microsoft/markitdown"],
      "type": "stdio"
    },
    "filesystem": {
      "registry": "https://api.mcp.github.com/v0/servers/modelcontextprotocol/filesystem",
      "container": "node:lts-alpine",
      "entrypointArgs": ["npx", "-y", "@modelcontextprotocol/server-filesystem"],
      "type": "stdio"
    },
    "custom-api": {
      "registry": "https://registry.example.com/servers/custom-api/v1",
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${API_TOKEN}"
      }
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "gateway-secret-token"
  }
}
```

**Notes**:
- The `registry` field is informational and does not affect server execution
- It can be used with both stdio (containerized) and HTTP servers
- Registry-aware tooling can use this field for discovery and version management
- The field complements other configuration fields like `container`, `entrypointArgs`, or `url`

#### A.6 Gateway with OpenTelemetry Tracing

The following example configures the gateway to export traces to an OTLP collector. Authentication headers (e.g., `Authorization: Bearer <token>`) are provided via the `OTEL_EXPORTER_OTLP_HEADERS` environment variable, not in the JSON config.

```json
{
  "mcpServers": {
    "github": {
      "container": "ghcr.io/github/github-mcp-server:latest",
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "${MCP_GATEWAY_AGENT_ID}",
    "opentelemetry": {
      "endpoint": "https://collector.example.com:4318/v1/traces",
      "serviceName": "my-workflow-gateway"
    }
  }
}
```

Set `OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer ${OTEL_TOKEN}` as an environment variable on the gateway container to authenticate with the collector.

#### A.7 Gateway with OpenTelemetry and Parent Trace Context

The following example propagates an existing distributed trace into the gateway, linking all MCP spans as children of a parent trace originating in another system:

```json
{
  "mcpServers": {
    "github": {
      "container": "ghcr.io/github/github-mcp-server:latest",
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentId": "${MCP_GATEWAY_AGENT_ID}",
    "opentelemetry": {
      "endpoint": "https://collector.example.com:4318/v1/traces",
      "traceId": "${PARENT_TRACE_ID}",
      "spanId": "${PARENT_SPAN_ID}",
      "serviceName": "my-workflow-gateway"
    }
  }
}
```

`PARENT_TRACE_ID` and `PARENT_SPAN_ID` are 32-character and 16-character lowercase hex strings respectively, typically injected as environment variables by the orchestrating system.

#### A.8 Gateway with Multiple Agent Identifiers (`agentIds`)

The following example configures a gateway shared by a primary agent and an enclave agent, each with its own identifier and independently keyed DIFC session state:

```json
{
  "mcpServers": {
    "github": {
      "container": "ghcr.io/github/github-mcp-server:latest",
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  },
  "gateway": {
    "port": 8080,
    "domain": "localhost",
    "agentIds": ["primary-agent", "enclave-agent"]
  }
}
```

### Appendix B: Gateway Lifecycle Examples

#### B.1 Closing the Gateway

**Request**:

```http
POST /close HTTP/1.1
Host: localhost:8080
Authorization: gateway-secret-token
```

**Note**: Consult your gateway implementation's documentation for the expected authorization header format.

**Success Response**:

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "status": "closed",
  "message": "Gateway shutdown initiated",
  "serversTerminated": 3
}
```

**Example Shutdown Sequence**:

1. Client calls `POST /close`
2. Gateway stops accepting new RPC requests
3. Gateway waits for in-flight requests to complete (max 30s)
4. Gateway terminates containers:
   - `github` container: SIGTERM sent, stopped after 2s
   - `slack` container: SIGTERM sent, stopped after 1s
   - `data-server` container: SIGTERM sent, SIGKILL after 10s timeout
5. Gateway cleans up resources
6. Gateway returns success response
7. Gateway process exits with code 0

**Idempotent Behavior**:

```http
POST /close HTTP/1.1
Host: localhost:8080
Authorization: gateway-secret-token
```

**Note**: Consult your gateway implementation's documentation for the expected authorization header format.

```http
HTTP/1.1 410 Gone
Content-Type: application/json

{
  "error": "Gateway has already been closed"
}
```

### Appendix C: Error Code Reference

| Code | Name | Description |
|------|------|-------------|
| -32700 | Parse error | Invalid JSON received |
| -32600 | Invalid request | Invalid JSON-RPC request |
| -32601 | Method not found | Method does not exist |
| -32602 | Invalid params | Invalid method parameters |
| -32603 | Internal error | Internal JSON-RPC error |
| -32000 | Server error | Generic server error |
| -32001 | Server unavailable | Server not responding |
| -32002 | Server timeout | Server response timeout |
| -32003 | Authentication failed | Invalid or missing credentials |

### Appendix D: Security Considerations

#### D.1 Credential Handling

- Agent identifiers (`agentId`/`agentIds`) MUST NOT be logged
- Environment variables MUST be isolated per server
- Secrets SHOULD be cleared from memory after use

#### D.2 Network Security

- Gateway SHOULD support TLS/HTTPS
- Server URLs SHOULD be validated
- Cross-origin requests SHOULD be restricted
- Rate limiting SHOULD be implemented

#### D.3 Container Security

- Server containers SHOULD run with minimal privileges
- Resource limits SHOULD be enforced (CPU, memory, file descriptors)
- Temporary files SHOULD be cleaned up
- Container monitoring SHOULD detect anomalies
- Container images SHOULD be signed and verified
- Containers SHOULD use read-only root filesystems where possible

#### D.4 Shutdown Security

- The `/close` endpoint MUST require authentication to prevent unauthorized shutdown
- Gateway SHOULD log all shutdown attempts (both successful and failed) for audit purposes
- In-flight requests SHOULD have a reasonable timeout to prevent denial-of-service during shutdown
- Container termination SHOULD use SIGTERM first to allow graceful cleanup before SIGKILL

---

## References

### Normative References

- **[RFC 2119]** Key words for use in RFCs to Indicate Requirement Levels
- **[JSON-RPC 2.0]** JSON-RPC 2.0 Specification
- **[MCP]** Model Context Protocol Specification
- **[W3C-TraceContext]** W3C Trace Context — Trace Context Level 2 (https://www.w3.org/TR/trace-context/)
- **[OTLP]** OpenTelemetry Protocol Specification — OTLP/HTTP Transport (https://opentelemetry.io/docs/specs/otlp/#otlphttp)

### Informative References

- **[MCP-Config]** MCP Configuration Format
- **[HTTP/1.1]** Hypertext Transfer Protocol -- HTTP/1.1
- **[gh-aw-audit]** [Audit Commands Reference](/gh-aw/reference/audit/) — Runtime MCP server health, guard policy analysis, and cross-run debugging
- **[OpenTelemetry]** OpenTelemetry — Observability framework (https://opentelemetry.io/)

---

## Change Log

### Version 1.17.0 (Draft)

- **BREAKING**: Removed `gateway.apiKey` from the specification and published schemas (Sections 4.1.1, 4.1.3, 7.1, 7.2)
  - The field's terminology was misleading: it represented agent/session identity, not a separately modeled API credential
  - Replaced by `gateway.agentId` (singular) and `gateway.agentIds` (plural), which are mutually exclusive
- **Added**: `gateway.agentId` — a single agent/session identifier, equivalent in behavior to the former `apiKey` field (Section 4.1.3.9)
- **Added**: `gateway.agentIds` — an array of agent/session identifiers (at least one, each a non-empty string), enabling a primary agent and enclave sessions to share one gateway instance while retaining independent DIFC state and policies (Section 4.1.3.9)
- **Added**: Section 4.1.3.9 — Agent Identifier Configuration (`agentId` / `agentIds`), documenting the mutually exclusive singular/plural forms and the corresponding mcpg TOML spelling `agent_id` / `agent_ids`
- **Changed**: A gateway configuration MUST select exactly one of `agentId` or `agentIds` (Sections 4.1.3, 4.1.3.9)
- **Updated**: JSON Schema — `gatewayConfig` definition now requires `port`, `domain`, and exactly one of `agentId`/`agentIds`; `apiKey` removed from both canonical schema copies
- **Note**: Implementations MAY continue to accept the legacy `api_key`/`apiKey` spelling internally for backward compatibility, but it is no longer part of the normative or published configuration contract

### Version 1.16.0 (Draft)

- **Changed**: `opentelemetry.endpoint` now accepts HTTP or HTTPS OTLP/HTTP collector URLs (Section 4.1.3.7)
  - Enables runner-local OTLP collectors such as `http://127.0.0.1:4318` for gateway self-telemetry
  - **Updated**: T-OTEL-004 — now rejects non-HTTP(S) endpoints instead of all non-HTTPS endpoints
  - **Updated**: JSON Schema — `opentelemetryConfig.endpoint` pattern now accepts `http://` and `https://` URLs in both canonical schema copies

### Version 1.15.0 (Draft)

- **Added**: Section 4.1.3.8 — `forcePublicRepos` Configuration
  - New optional boolean gateway config field (default: `true`) that forces the allow-only policy to `repos="public"` at runtime when the gateway detects it is running in a public repository
  - Prevents agents from accumulating private-data secrecy tags by restricting repository access at the input side
  - Set to `false` by the compiler when `private-to-public-flows: allow` is declared in workflow frontmatter
  - Can also be overridden via `MCP_GATEWAY_FORCE_PUBLIC_REPOS=false` environment variable
- **Added**: `forcePublicRepos` field to the gateway configuration fields table (Section 4.1.3)
- **Added**: Section 10.8 — Write-Sink Guard Policy (`sink-visibility`)
  - Formal specification of the `write-sink` guard policy for DIFC-based output filtering
  - Section 10.8.1 — Overview: describes the write-sink guard as output-side filtering complementary to input-side integrity filtering
  - Section 10.8.2 — `sink-visibility` field reference: values `"public"`, `"private"`, `"internal"`, and omitted; enforcement model per value
  - Section 10.8.3 — Interaction with `accept`: `"public"` unconditionally overrides accept patterns; non-public values delegate to accept patterns
  - Section 10.8.4 — Runtime Verification (defense-in-depth): gateway reads `GITHUB_REPOSITORY` at startup and overrides misconfigured `sink-visibility` when the actual repo is public; skipped when `sink-visibility` is omitted
  - Section 10.8.5 — Compiler Behavior: documents the runtime expression emitted for `sink-visibility` using `determine-automatic-lockdown` step output
- **Added**: Section 10.9 — Cross-Visibility Opt-Out (`private-to-public-flows: allow`)
  - Workflow frontmatter field that disables both `forcePublicRepos` override and `sink-visibility` runtime verification
  - Section 10.9.2 — Constraints: incompatible with `guards_mode: strict` (compile error); only allowed with `filter`/`propagate`
  - Section 10.9.3 — Compiler Responsibilities: validate, emit `forcePublicRepos: false`, skip `sink-visibility` override, and log opt-out
  - Section 10.9.4 — Interaction Matrix: defines enforced/disabled combinations per `guards_mode` and `private-to-public-flows` setting
  - Section 10.9.5 — Security Rationale: documents legitimate use cases for cross-visibility flows
- **Added**: Compliance test category 11.1.12 — Write-Sink Guard Policy Tests (T-WS-001 through T-WS-008)
- **Updated**: Compliance Checklist (Section 11.2) — added Write-Sink Guard Policy row (T-WS-*, Level 2, Standard)
- **Updated**: JSON Schema — added typed `write-sink` property to `guard-policies` in both `stdioServerConfig` and `httpServerConfig`; added `forcePublicRepos` property to `gatewayConfig`

### Version 1.14.0 (Draft)

- **Added**: Section 10 — Guard Policy
  - Formal specification of the guard policy mechanism for integrity-based content filtering on the GitHub MCP server
  - Section 10.1 — Overview: describes the purpose and relationship to the integrity filtering reference
  - Section 10.2 — Integrity Levels: defines `merged`, `approved`, `unapproved`, `none`, and `blocked` levels
  - Section 10.3 — Guard Policy Fields: field reference table covering `min-integrity`, `allowed-repos`, `blocked-users`, `trusted-users`, `approval-labels`, and `refusal-labels`
  - Section 10.4 — Effective Integrity Computation: normative 6-step algorithm with precedence rules
  - Section 10.5 — `approval-labels` Field: semantics, constraints, and configuration example
  - Section 10.6 — `refusal-labels` Field: new field; semantics, constraints, combined example, and requirements
  - Section 10.7 — Centralized Management via GitHub Variables: `GH_AW_GITHUB_REFUSAL_LABELS` variable added
- **Added**: `refusal-labels` guard policy field (Section 10.6)
  - The inverse of `approval-labels`: items bearing any listed label have effective integrity downgraded to `none`
  - Overrides `trusted-users` and `approval-labels` promotion; `blocked-users` still takes precedence
  - Accepts a literal array or a GitHub Actions expression (comma- or newline-separated list)
  - Empty list is a no-op
- **Added**: Compliance test category 11.1.11 — Guard Policy Tests (T-GP-001 through T-GP-010)
  - T-GP-004 through T-GP-008 specifically cover `refusal-labels` behavior
- **Updated**: Compliance Checklist (Section 11.2) — added Guard Policy row (T-GP-*, Level 2, Standard)
- **Renumbered**: Former Section 10 (Compliance Testing) is now Section 11; all subsection references updated accordingly
- **Added**: `GH_AW_GITHUB_REFUSAL_LABELS` to the centralized management variables table (Section 10.7)

### Version 1.14.0 (Draft)

- **Breaking**: `headers` field removed from `opentelemetry` configuration in JSON schema and gateway config spec (Section 4.1.3.7)
  - Authentication headers MUST now be provided via the `OTEL_EXPORTER_OTLP_HEADERS` environment variable (standard OTel convention)
  - gh-aw automatically forwards `OTEL_EXPORTER_OTLP_HEADERS` to the mcpg container when `observability.otlp` is configured
  - This keeps credentials out of the stdin JSON config pipe and follows the [OTel SDK environment variable spec](https://opentelemetry.io/docs/specs/otel/protocol/exporter/#configuration-options)
- **Updated**: T-OTEL-006 — now verifies `OTEL_EXPORTER_OTLP_HEADERS` env var is read and applied (instead of `headers` JSON field)
- **Updated**: JSON Schema — removed `headers` property from `opentelemetryConfig` definition
- **Updated**: Appendix A.6 and A.7 examples to remove `headers` from JSON config and document env var usage

### Version 1.13.0 (Draft)

- **Breaking**: `headers` field in `opentelemetry` configuration is now exclusively a string; object form is no longer supported (Section 4.1.3.6)
  - The value MUST be a raw string provided by the user and passed through without parsing
  - Removed Section 4.1.3.6.1 (Headers String Format) — no parsing rules apply; the string is used as-is
- **Removed**: Compliance tests T-OTEL-011 and T-OTEL-012 (string-form parsing); replaced T-OTEL-006 with a string pass-through test
- **Updated**: JSON Schema `opentelemetryConfig.headers` changed from `oneOf [object, string]` to `string` only
- **Updated**: Appendix A.6 and A.7 examples to use string form for `headers`

### Version 1.12.0 (Draft)

- **Changed**: `headers` field in `opentelemetry` configuration now accepts two forms (Section 4.1.3.6)
  - **Object form** (existing): keys are header names, values are header values — fully backward-compatible
  - **String form** (new): a multi-line string where each line uses `name=value` syntax, providing a concise alternative for simple header lists
- **Added**: Section 4.1.3.6.1 — Headers String Format
  - Parsing rules for `name=value` lines: split on first `=`, trim whitespace, skip lines without `=` or with empty name, last occurrence wins for duplicate names
  - Equivalence example showing string form vs object form
  - Notes on case sensitivity and duplicate name handling
- **Added**: Compliance tests T-OTEL-011 and T-OTEL-012 for string-form header parsing
- **Updated**: JSON Schema `opentelemetryConfig.headers` changed from a plain `object` type to `oneOf [object, string]`

### Version 1.11.0 (Draft)

- **Added**: `opentelemetry` field to gateway configuration (Section 4.1.3, 4.1.3.6)
  - Optional object for configuring OpenTelemetry distributed tracing of MCP calls
  - `endpoint` (required when present): HTTPS OTLP/HTTP collector endpoint URL
  - `headers`: Additional HTTP headers sent with every export request (e.g., `Authorization`)
  - `traceId`: Parent trace ID (32-char lowercase hex) for W3C trace context propagation
  - `spanId`: Parent span ID (16-char lowercase hex) for W3C trace context propagation
  - `serviceName`: Logical service name in the `service.name` resource attribute (default: `"mcp-gateway"`)
- **Added**: Section 4.1.3.6 — OpenTelemetry Configuration
  - Full field reference table with types, requirements, and descriptions
  - Tracing behavior requirements (span attributes, W3C `traceparent` construction, export protocol)
  - Failure handling requirements (export failures MUST NOT affect MCP processing)
- **Added**: Compliance test category 10.1.10 — OpenTelemetry Tests (T-OTEL-001 through T-OTEL-010)
- **Added**: OpenTelemetry example configurations (Appendix A.6, A.7)
- **Added**: Normative references for W3C Trace Context and OTLP
- **Updated**: JSON Schema with `opentelemetry` property and `opentelemetryConfig` definition in `gatewayConfig`

### Version 1.11.0 (Draft)

- **Added**: `sessionTimeout` field to gateway configuration (Section 4.1.3, 4.1.3.6)
  - Optional Go duration string for controlling MCP session lifetime (e.g. `"4h"`, `"30m"`)
  - Precedence: stdin config `sessionTimeout` > `MCP_GATEWAY_SESSION_TIMEOUT` env var > gateway default (6h)
  - Workflow authors configure via `engine.mcp.session-timeout` in frontmatter; the compiler validates (min 5m, no upper bound) and emits it into the gateway config JSON
- **Added**: Section 4.1.3.6 — Session Timeout Configuration
- **Renumbered**: Former Section 4.1.3.6 (OpenTelemetry) to 4.1.3.7
- **Updated**: JSON Schema with `sessionTimeout` property in `gatewayConfig` definition

### Version 1.10.0 (Draft)

- **Added**: `keepaliveInterval` field to gateway configuration (Section 4.1.3, 4.1.3.5)
  - Optional integer (seconds) for controlling keepalive ping frequency to HTTP MCP backends
  - Prevents session expiry during long-running agent tasks
  - Workflow authors configure via `sandbox.mcp.keepalive-interval` in frontmatter; the compiler translates it into the gateway config
- **Added**: Section 4.1.3.5 — Keepalive Interval Configuration
- **Updated**: JSON Schema with `keepaliveInterval` property in `gatewayConfig` definition

### Version 1.9.0 (Draft)

- **Added**: `trustedBots` field to gateway configuration (Section 4.1.3, 4.1.3.4)
  - Optional array of GitHub bot identity strings passed to the gateway via the generated config
  - Merged with the gateway's built-in trusted identity list (additive — cannot remove built-in entries)
  - Workflow authors configure via `sandbox.mcp.trusted-bots` in frontmatter; the compiler translates it into the gateway config
- **Added**: Section 7.5 — Trusted Bot Identity Configuration
  - Describes `trustedBots` as a config-passing mechanism from frontmatter to gateway config
- **Added**: Compliance test for trusted bot identity configuration (Section 10.1.4)
  - T-AUTH-006: Trusted bot identity configuration
- **Updated**: JSON Schema with `trustedBots` property in `gatewayConfig` definition

### Version 1.8.0 (Draft)

- **Added**: `payloadDir` field to gateway configuration (Section 4.1.3)
  - Optional directory path where the gateway places large payload JSON files for authenticated clients
  - Enables efficient handling of large response payloads by offloading them to the filesystem
  - Gateway implementations MUST ensure proper isolation between clients' payload files when this feature is used
  - Payload files are accessible to clients authenticated with the corresponding API key
- **Added**: Path validation requirements for `payloadDir` field (Section 4.1.3.1)
  - `payloadDir` MUST be an absolute path if provided
  - Unix-like systems: paths MUST start with `/`
  - Windows systems: paths MUST start with a drive letter followed by `:\`
  - Relative paths, empty strings, and malformed paths MUST be rejected
  - JSON schema pattern validation added: `^(/|[A-Za-z]:\\\\)`
  - Compliance test T-CFG-005 for payload directory path validation

### Version 1.7.0 (Draft)

- **Added**: Comprehensive volume mount documentation for stdio servers (Section 4.1.5)
  - Detailed specification of mount format: "host:container:mode" where mode is "ro" (read-only) or "rw" (read-write)
  - Requirements for mount path validation and format enforcement
  - Security considerations for read-only vs read-write mounts
  - Common use cases: configuration files, data directories, output directories, shared caches
- **Updated**: Server configuration field documentation (Section 4.1.2)
  - Clarified that `mounts` field applies to stdio (containerized) servers only
  - Updated mount format description to use "host:container:mode" terminology for clarity
  - Added cross-reference to Section 4.1.5 for detailed mount documentation
- **Updated**: Container isolation documentation (Section 6.1)
  - Added requirement to apply volume mounts as configured
  - Added "Filesystem Isolation" to isolation guarantees
  - Added "Volume Mount Isolation" subsection explaining mount behavior and independence between containers
  - Clarified that mounts provide controlled host filesystem access while maintaining process isolation
- **Added**: Compliance tests for volume mounts
  - T-CFG-014: Valid volume mount format (host:container:mode)
  - T-CFG-015: Reject invalid mount format (missing components)
  - T-CFG-016: Reject invalid mount mode (not "ro" or "rw")
  - T-CFG-017: Multiple mounts for single stdio server
  - T-CFG-018: Reject mounts for HTTP servers (stdio only)
  - T-ISO-006: Volume mount isolation (mounts do not affect other containers)
  - T-ISO-007: Volume mount access mode enforcement (ro vs rw)
  - T-ISO-008: Volume mount path independence between containers

### Version 1.6.0 (Draft)

- **Added**: Custom server type support (Section 4.1.4)
  - Gateway MAY support custom server types beyond "stdio" and "http"
  - Custom types registered in top-level `customSchemas` field mapping type names to JSON Schema URLs
  - Custom server configurations validated against registered schemas
  - Enables extensibility for specialized MCP server implementations (e.g., MCP Scripts)
- **Added**: `customSchemas` field to top-level configuration (Section 4.1.3a)
  - Maps custom type names to HTTPS URLs for JSON Schema validation
  - Supports empty string to skip validation for a custom type
  - HTTPS-only for security (no file:// URLs)
  - Moved from gateway configuration to top level for cleaner structure
- **Updated**: JSON Schema with custom server type support
  - Added `customServerConfig` definition for extensible server types
  - Added `customSchemas` property at top level with HTTPS-only validation
  - Added example configuration demonstrating custom type usage
- **Added**: Compliance tests for custom server types
  - T-CFG-009: Valid custom server type with registered schema
  - T-CFG-010: Reject custom type without schema registration
  - T-CFG-011: Validate custom configuration against registered schema
  - T-CFG-012: Reject custom type conflicting with reserved types (stdio/http)
  - T-CFG-013: Custom schema URL fetch and cache

### Version 1.5.0 (Draft)

- **Added**: Documentation for `tools` field support for HTTP servers (Section 4.1.2)
  - Clarified that the `tools` field applies to both stdio and HTTP server configurations
  - Tool filtering allows `["*"]` for all tools or a list of specific tool names
  - Updated configuration structure example to include `tools` and `headers` fields (Section 4.1.1)
- **Added**: Example configurations demonstrating `tools` field usage (Appendix A.3)
  - Shows stdio server with specific tool allowlist
  - Shows HTTP server with all tools allowed (`["*"]`)
- **Updated**: Stdout configuration output documentation (Section 5.4)
  - Added guidance that `tools` field MAY be included in output to preserve tool filtering
  - Updated example to show tools field in gateway output configuration

### Version 1.4.0 (Draft)

- **Changed**: Relaxed authorization header format requirements (Section 7.1, 7.2, 5.4)
  - Authorization header format is now implementation-dependent rather than strictly prescribed
  - Removed requirement to NOT use Bearer authentication scheme
  - Updated examples to show multiple possible formats
  - Modified stdout configuration output requirements from MUST to SHOULD for headers object
- **Added**: Connection failure handling requirements (Section 5.2.2)
  - Gateway MUST NOT silently ignore connection failures to HTTP-based MCP servers
  - Gateway MUST either pass through errors or handle with fallback mechanisms
  - Added protocol translation compliance tests (T-PTL-007, T-PTL-008)
- **Updated**: Configuration output compliance tests (Section 10.1.7)
  - Modified T-OUT-005 and T-OUT-006 to reflect relaxed authentication requirements
  - Tests now verify presence of authentication headers when configured, not specific format

### Version 1.3.0 (Draft)

- **Added**: Health endpoint version information requirements (Section 8.1.1)
  - `/health` endpoint MUST include `specVersion` field with MCP Gateway Specification version
  - `/health` endpoint MUST include `gatewayVersion` field with gateway implementation version
  - Both version fields MUST use semantic versioning format (MAJOR.MINOR.PATCH)
- **Added**: Health monitoring compliance tests for version fields (T-HLT-006 through T-HLT-009)
- **Improved**: Health endpoint documentation with detailed field descriptions and requirements

### Version 1.2.0 (Draft)

- **BREAKING**: Clarified stdout configuration output requirements (Section 5.4)
  - Gateway MUST include `headers` object in output configuration for each server
  - `Authorization` header MUST be present with API key value
  - Made explicit that authorization headers are required for client connectivity
- Added configuration output compliance tests (T-OUT-001 through T-OUT-007)
- Updated compliance checklist to include configuration output as Level 1 (Required)

### Version 1.1.0 (Draft)

- Added `/close` endpoint for graceful gateway shutdown
- Added gateway lifecycle compliance tests (T-LIFE-*)
- Added resource cleanup requirements
- Added shutdown security considerations
- Added gateway lifecycle examples in Appendix B

### Version 1.0.0 (Draft)

- Initial specification release
- Configuration format definition
- Protocol behavior specification
- Compliance test framework

---

*Copyright © 2026 GitHub, Inc. All rights reserved.*
