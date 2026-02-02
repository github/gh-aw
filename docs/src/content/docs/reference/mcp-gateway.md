---
title: MCP Gateway Specification
description: Formal specification for the Model Context Protocol (MCP) Gateway implementation following W3C conventions
sidebar:
  order: 1350
---

# MCP Gateway Specification

**Version**: 1.8.0  
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
10. [Compliance Testing](#10-compliance-testing)

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
    "apiKey": "string",
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

*Required for stdio servers (containerized execution)  
**Required for HTTP servers

**Note**: The `command` field is NOT supported. Stdio servers MUST use the `container` field to specify a containerized MCP server. Direct command execution is not supported by this specification.

#### 4.1.3 Gateway Configuration Fields

The `gateway` section is required and configures gateway-specific behavior:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `port` | integer | Yes | HTTP server port |
| `domain` | string | Yes | Gateway domain (localhost or host.docker.internal) |
| `apiKey` | string | Yes | API key for authentication |
| `startupTimeout` | integer | No | Server startup timeout in seconds (default: 30) |
| `toolTimeout` | integer | No | Tool invocation timeout in seconds (default: 60) |
| `payloadDir` | string | No | Directory path for storing large payload JSON files for authenticated clients |

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
    "apiKey": "secret"
  },
  "customSchemas": {
    "safeinputs": "https://docs.github.com/gh-aw/schemas/safe-inputs-config.schema.json"
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
    "apiKey": "secret"
  },
  "customSchemas": {
    "safeinputs": "https://docs.github.com/gh-aw/schemas/safe-inputs-config.schema.json"
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
Authorization: <apiKey>

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
Authorization: <apiKey>
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

The `/close` endpoint MUST require authentication when `gateway.apiKey` is configured. Requests without valid authentication MUST be rejected with HTTP 401 Unauthorized.

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
           "Authorization": "{apiKey}"
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

The MCP Gateway uses a simple API key authentication scheme. When `gateway.apiKey` is configured:

- The `Authorization` header contains the API key value
- Implementations MAY use different formats (e.g., direct value or Bearer scheme)
- The specific format is implementation-dependent

**Example formats**:

```http
Authorization: my-secret-api-key-12345
```

or

```http
Authorization: Bearer my-secret-api-key-12345
```

This authentication scheme provides flexibility for different implementation requirements.

### 7.2 API Key Authentication

When `gateway.apiKey` is configured, the gateway MUST:

1. Require `Authorization` header on all RPC requests to `/mcp/{server-name}` and `/close` endpoints
   - The specific format of the Authorization header is implementation-dependent
   - Implementations SHOULD document their expected format
2. Reject requests with missing or invalid tokens (HTTP 401)
3. Reject requests with malformed Authorization headers (HTTP 400)
4. NOT log API keys in plaintext

### 7.3 Optimal Temporary API Key

The gateway SHOULD support temporary API keys:

1. Generate a random API key on startup if not provided
2. Include key in stdout configuration output

### 7.4 Authentication Exemptions

The following endpoints MUST NOT require authentication:

- `/health`

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

## 10. Compliance Testing

### 10.1 Test Suite Requirements

A conforming implementation MUST pass the following test categories:

#### 10.1.1 Configuration Tests

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

#### 10.1.2 Protocol Translation Tests

- **T-PTL-001**: Stdio request/response cycle
- **T-PTL-002**: HTTP passthrough
- **T-PTL-003**: Tool signature preservation
- **T-PTL-004**: Concurrent request handling
- **T-PTL-005**: Large payload handling
- **T-PTL-006**: Partial response buffering
- **T-PTL-007**: HTTP connection failure error response
- **T-PTL-008**: HTTP connection failure is not silently ignored

#### 10.1.3 Isolation Tests

- **T-ISO-001**: Container isolation verification
- **T-ISO-002**: Environment isolation verification
- **T-ISO-003**: Credential isolation verification
- **T-ISO-004**: Cross-container communication prevention
- **T-ISO-005**: Container failure isolation
- **T-ISO-006**: Volume mount isolation (mounts do not affect other containers)
- **T-ISO-007**: Volume mount access mode enforcement (ro vs rw)
- **T-ISO-008**: Volume mount path independence between containers

#### 10.1.4 Authentication Tests

- **T-AUTH-001**: Valid token acceptance
- **T-AUTH-002**: Invalid token rejection
- **T-AUTH-003**: Missing token rejection
- **T-AUTH-004**: Health endpoint exemption
- **T-AUTH-005**: Token rotation support

#### 10.1.5 Timeout Tests

- **T-TMO-001**: Startup timeout enforcement
- **T-TMO-002**: Tool timeout enforcement
- **T-TMO-003**: Timeout error messaging
- **T-TMO-004**: Partial response timeout
- **T-TMO-005**: Concurrent timeout handling

#### 10.1.6 Health Monitoring Tests

- **T-HLT-001**: Health endpoint availability
- **T-HLT-002**: Liveness probe accuracy
- **T-HLT-003**: Readiness probe accuracy
- **T-HLT-004**: Server status reporting
- **T-HLT-005**: Automatic restart behavior
- **T-HLT-006**: Health response includes specVersion field
- **T-HLT-007**: Health response includes gatewayVersion field
- **T-HLT-008**: specVersion uses semantic versioning format
- **T-HLT-009**: gatewayVersion uses semantic versioning format

#### 10.1.7 Configuration Output Tests

- **T-OUT-001**: Gateway outputs valid JSON configuration to stdout
- **T-OUT-002**: Output configuration includes all configured servers
- **T-OUT-003**: Each server configuration has "type": "http"
- **T-OUT-004**: Each server configuration has correct "url" format
- **T-OUT-005**: Each server configuration includes "headers" object when authentication is required
- **T-OUT-006**: Authorization header is present when authentication is configured
- **T-OUT-007**: Output configuration is complete before health endpoint becomes available

#### 10.1.8 Error Handling Tests

- **T-ERR-001**: Startup failure reporting
- **T-ERR-002**: Runtime error handling
- **T-ERR-003**: Invalid request handling
- **T-ERR-004**: Server crash recovery
- **T-ERR-005**: Error message quality

#### 10.1.9 Gateway Lifecycle Tests

- **T-LIFE-001**: Close endpoint authentication
- **T-LIFE-002**: Close endpoint success response
- **T-LIFE-003**: Close endpoint idempotency (returns 410 on subsequent calls)
- **T-LIFE-004**: Container termination on close
- **T-LIFE-005**: Resource cleanup on close
- **T-LIFE-006**: In-flight request handling during shutdown
- **T-LIFE-007**: New requests rejected after close initiated

### 10.2 Compliance Checklist

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

### 10.3 Test Execution

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
    "apiKey": "gateway-secret-token"
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
    "apiKey": "gateway-secret-token"
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
    "apiKey": "gateway-secret-token"
  }
}
```

**Notes**:
- The `registry` field is informational and does not affect server execution
- It can be used with both stdio (containerized) and HTTP servers
- Registry-aware tooling can use this field for discovery and version management
- The field complements other configuration fields like `container`, `entrypointArgs`, or `url`

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

- API keys MUST NOT be logged
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

### Informative References

- **[MCP-Config]** MCP Configuration Format
- **[HTTP/1.1]** Hypertext Transfer Protocol -- HTTP/1.1

---

## Change Log

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
  - Enables extensibility for specialized MCP server implementations (e.g., Safe Inputs)
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
