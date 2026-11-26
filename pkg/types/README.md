# types Package

> Shared type definitions for MCP server configuration, token weights, and workflow input parameters — used across multiple `gh-aw` packages to avoid circular imports.

## Overview

This package defines common data structures shared between the `parser` and `workflow` packages. Centralizing these types in a dedicated package allows both to reference the same definitions without creating import cycles.

The primary types are `BaseMCPServerConfig` (the foundation for all MCP server configurations), `MCPAuthConfig` (OIDC authentication for HTTP MCP servers), `TokenWeights` with `TokenClassWeights` (AI Credits cost ratio configuration used in run metadata), and `InputDefinition` (workflow dispatch input parameters following the GitHub Actions schema).

All struct fields carry both `json` and `yaml` struct tags, ensuring types can be round-tripped through both serialization formats. `BaseMCPServerConfig` is designed to be embedded rather than used directly — consumer packages (`parser`, `workflow`) add domain-specific fields and validation on top of the shared base.

## Public API

### `BaseMCPServerConfig`

The foundational configuration structure for MCP (Model Context Protocol) servers. This type is embedded by both `parser.MCPServerConfig` and `workflow.MCPServerConfig`.

MCP servers can run as:
- **stdio processes**: `Command` + `Args`, launched as a child process.
- **HTTP endpoints**: `URL` + optional `Headers` and `Auth`, reached over HTTP/HTTPS.
- **Container services**: `Container` image + optional `Mounts`, run inside a container.

```go
import "github.com/github/gh-aw/pkg/types"

// Stdio MCP server
cfg := types.BaseMCPServerConfig{
    Type:    "stdio",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
    Env: map[string]string{
        "ALLOWED_PATHS": "/workspace",
    },
}

// HTTP MCP server with OIDC auth
cfg := types.BaseMCPServerConfig{
    Type: "http",
    URL:  "https://my-mcp-server.example.com",
    Auth: &types.MCPAuthConfig{
        Type:     "github-oidc",
        Audience: "https://my-mcp-server.example.com",
    },
}
```

#### Fields

| Field | Type | Description |
|-------|------|-------------|
| `Type` | `string` | Server type: `"stdio"`, `"http"`, `"local"`, or `"remote"` |
| `Command` | `string` | Executable to launch (stdio mode) |
| `Args` | `[]string` | Arguments passed to the command |
| `Env` | `map[string]string` | Environment variables injected into the process |
| `Version` | `string` | Optional version or tag |
| `URL` | `string` | HTTP endpoint URL (HTTP mode) |
| `Headers` | `map[string]string` | Additional HTTP headers (HTTP mode) |
| `CACertFile` | `string` | Author-facing `ca-cert-file` path for per-server HTTPS CA trust |
| `Auth` | `*MCPAuthConfig` | Upstream authentication (HTTP mode only) |
| `Container` | `string` | Container image (container mode) |
| `Entrypoint` | `string` | Optional entrypoint override for the container |
| `EntrypointArgs` | `[]string` | Arguments passed to the container entrypoint |
| `Mounts` | `[]string` | Volume mounts in `"source:dest:mode"` format |

### `MCPAuthConfig`

Authentication configuration for HTTP MCP servers. When configured, the MCP gateway dynamically acquires tokens and injects them as `Authorization` headers on each outgoing request.

```go
auth := &types.MCPAuthConfig{
    Type:     "github-oidc", // Currently the only supported type
    Audience: "https://my-service.example.com",
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Type` | `string` | Auth type; currently only `"github-oidc"` is supported |
| `Audience` | `string` | OIDC token audience (`aud` claim); defaults to the server URL if omitted |
| `Provider` | `string` | Selects the federation provider for the token exchange. |
| `AzureTenantID` | `string` | Azure tenant ID used for Azure federation (`azure-tenant-id`). |
| `AzureClientID` | `string` | Azure application/client ID used for Azure federation (`azure-client-id`). |
| `AzureScope` | `string` | Scope requested from Azure during token exchange (`azure-scope`). |
| `AzureCloud` | `string` | Azure cloud environment used for federation (`azure-cloud`). |

### `TokenWeights`

Defines historical model cost metadata that may appear in prior `aw_info.json` runtime artifacts.

```go
weights := types.TokenWeights{
    Multipliers: map[string]float64{
        "gpt-4o": 2.5,
    },
    TokenClassWeights: &types.TokenClassWeights{
        Input:  1.0,
        Output: 3.0,
    },
}
```

### `TokenClassWeights`

Per-token-class weights for cost computation. Each field corresponds to one token class; a zero value means "use the default weight".

| Field | Token class |
|-------|-------------|
| `Input` | Standard input tokens |
| `CachedInput` | Cache-hit input tokens |
| `Output` | Generated output tokens |
| `Reasoning` | Internal reasoning tokens |
| `CacheWrite` | Cache-write tokens |

### `InputDefinition`

Defines an input parameter for workflows, safe-jobs, and imported workflows. The structure follows the [`workflow_dispatch` input schema](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#onworkflow_dispatchinputs) from GitHub Actions.

```go
input := types.InputDefinition{
    Description: "The environment to deploy to",
    Required:    true,
    Default:     "staging",
    Type:        "choice",
    Options:     []string{"staging", "production"},
}
```

#### Fields

| Field | Type | Description |
|-------|------|-------------|
| `Description` | `string` | Human-readable description of the input parameter |
| `Required` | `bool` | Whether the input is required; defaults to `false` |
| `Default` | `any` | Default value; can be `string`, `number`, or `boolean` |
| `Type` | `string` | Input type: `"string"`, `"choice"`, `"boolean"`, `"number"`, or `"environment"` |
| `Options` | `[]string` | Valid options for `choice` type inputs |

#### Method: `GetDefaultAsString() string`

Returns the `Default` field as a string, regardless of its underlying type. Handles `string`, `bool`, `int`, `int64`, and `float64` inputs. Integer-valued `float64` defaults (e.g. `1.0`) are formatted without a decimal point. Returns `""` when `Default` is `nil`.

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/types"

// Stdio MCP server
cfg := types.BaseMCPServerConfig{
    Type:    "stdio",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
    Env:     map[string]string{"ALLOWED_PATHS": "/workspace"},
}

// HTTP MCP server with OIDC auth
cfg := types.BaseMCPServerConfig{
    Type: "http",
    URL:  "https://my-mcp-server.example.com",
    Auth: &types.MCPAuthConfig{
        Type:     "github-oidc",
        Audience: "https://my-mcp-server.example.com",
    },
}

// Token weights for model cost tracking
weights := types.TokenWeights{
    Multipliers: map[string]float64{"gpt-4o": 2.5},
}
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — package-scoped debug logging for input-definition fallback coercion

## Design Notes

- This package keeps internal dependencies minimal and currently only imports `pkg/logger` for debug logging. This minimal dependency footprint keeps it safe to import broadly without introducing package cycles.
- All struct fields use both `json` and `yaml` struct tags so they can be round-tripped through both serialization formats.
- `BaseMCPServerConfig` is designed to be embedded — packages add domain-specific fields and validation on top of the shared base.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
