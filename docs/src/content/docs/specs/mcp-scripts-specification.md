---
title: MCP Scripts Specification
description: Formal specification for MCP Scripts custom MCP tools following W3C conventions
sidebar:
  order: 1360
---

# MCP Scripts Specification

**Version**: 1.1.0  
**Status**: Draft Specification  
**Latest Version**: [mcp-scripts-specification](/gh-aw/specs/mcp-scripts-specification/)  
**JSON Schema**: [mcp-scripts-config.schema.json](/gh-aw/schemas/mcp-scripts-config.schema.json)  
**Editor**: GitHub Agentic Workflows Team

---

## Abstract

This specification defines MCP Scripts, an extension to the MCP Gateway that enables inline definition of custom MCP tools directly in workflow frontmatter using JavaScript, shell scripts, Python, or Go. MCP Scripts provides ephemeral, containerized tool execution with controlled secret access through a standardized MCP tools interface. Tool execution is stateless and session-independent, providing process isolation and security boundaries for custom functionality.

## Status of This Document

This section describes the status of this document at the time of publication. This is a draft specification and may be updated, replaced, or made obsolete by other documents at any time.

This document is governed by the GitHub Agentic Workflows project specifications process.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Architecture](#3-architecture)
4. [Configuration Format](#4-configuration-format)
5. [Tool Execution](#5-tool-execution)
6. [Language Support](#6-language-support)
7. [Security Model](#7-security-model)
8. [Large Output Handling](#8-large-output-handling)
9. [Integration with MCP Gateway](#9-integration-with-mcp-gateway)
10. [Safeguards](#10-safeguards)
11. [Norms](#11-norms)
12. [Compliance Testing](#12-compliance-testing)
13. [Sync Notes](#13-sync-notes)

---

## 1. Introduction

### 1.1 Purpose

MCP Scripts enables developers to define custom MCP tools inline in workflow frontmatter without requiring external MCP server implementations. It solves the following problems:

- **Rapid Tool Development**: Define tools directly in workflow without creating separate services
- **Secret Isolation**: Provide controlled access to secrets through explicit environment variable mapping
- **Language Flexibility**: Support multiple implementation languages (JavaScript, Shell, Python, Go)
- **Process Isolation**: Execute tools in containerized environments with security boundaries
- **Ephemeral Execution**: Stateless tool invocations without session management overhead

### 1.2 Scope

This specification covers:

- MCP Scripts configuration format in workflow frontmatter
- Tool definition structure and validation rules
- Supported implementation languages and their execution models
- Secret access and environment variable handling
- Tool input/output schemas and validation
- Large output handling mechanisms
- Integration with MCP Gateway infrastructure

This specification does NOT cover:

- MCP Gateway core protocol (see [MCP Gateway Specification](/gh-aw/reference/mcp-gateway/))
- MCP protocol semantics (see [Model Context Protocol Specification](https://spec.modelcontextprotocol.io/))
- External MCP server implementations
- Agent client implementations
- UI or interactive features

### 1.3 Design Goals

MCP Scripts is designed for:

- **Developer Convenience**: Minimal configuration overhead for common tool patterns
- **Security by Default**: Explicit secret access, process isolation, output sanitization
- **Stateless Execution**: No session management, each invocation is independent
- **Language Agnostic**: Support multiple implementation languages with consistent behavior
- **Gateway Integration**: Seamless integration with MCP Gateway as configuration extension

### 1.4 Relationship to MCP Gateway

MCP Scripts is an **extension** to the MCP Gateway Specification. The MCP Gateway allows additional fields in its configuration format, and MCP Scripts leverages this extensibility to provide inline tool definitions. MCP Scripts configurations are processed during workflow compilation and translated into MCP server configurations that are gatewayed by the MCP Gateway infrastructure.

---

## 2. Conformance

### 2.1 Conformance Classes

A **conforming MCP Scripts implementation** is one that satisfies all MUST, REQUIRED, and SHALL requirements in this specification.

A **partially conforming MCP Scripts implementation** is one that satisfies all MUST requirements for JavaScript tools but MAY lack support for Shell, Python, or Go implementations.

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.3 Compliance Levels

Implementations MUST support:

- **Level 1 (Required)**: JavaScript tools, basic input validation, HTTP transport
- **Level 2 (Standard)**: Shell and Python tools, timeout handling, secret isolation
- **Level 3 (Complete)**: Go tools, large output handling, all optional features

---

## 3. Architecture

### 3.1 System Architecture

```
┌─────────────────────────────────────────────────────────┐
│                 Workflow Frontmatter                    │
│                  (mcp-scripts:)                         │
└──────────────────────┬──────────────────────────────────┘
                       │ Compilation
                       ▼
┌─────────────────────────────────────────────────────────┐
│              MCP Scripts Server                     │
│  ┌───────────────────────────────────────────────────┐  │
│  │  Tool Registry & Configuration Loader             │  │
│  └───────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────┐  │
│  │  HTTP MCP Server (JSON-RPC over HTTP)            │  │
│  └───────────────────────────────────────────────────┘  │
└──────┬──────────────┬──────────────┬───────────────────┘
       │              │              │
       │ JavaScript   │ Shell        │ Python/Go
       ▼              ▼              ▼
  ┌─────────┐   ┌─────────┐   ┌─────────┐
  │ In-     │   │ Docker  │   │ Docker  │
  │ Process │   │ Container│   │ Container│
  │         │   │         │   │         │
  └─────────┘   └─────────┘   └─────────┘
```

### 3.2 Execution Model

MCP Scripts operates with the following execution model:

1. **Compilation Phase**: Workflow frontmatter is parsed and validated
2. **Server Startup**: MCP Scripts server starts with tool configurations
3. **Tool Registration**: Each tool is registered with the MCP server
4. **Invocation**: Agent invokes tool via MCP protocol (HTTP transport)
5. **Execution**: Tool handler executes in appropriate runtime environment
6. **Response**: Result is returned via JSON-RPC response
7. **Cleanup**: Ephemeral resources are cleaned up after invocation

### 3.2.1 Operations Ordering (Normative Cross-Link)

Implementations MUST preserve the invocation operation ordering defined in §5.1.1. Architecture
changes in this section MUST NOT weaken or reorder the §5.1.1 sequence.

### 3.3 Transport Model

MCP Scripts MUST use HTTP transport for MCP communication. The transport architecture is:

- **Client → Gateway**: HTTP with JSON-RPC payloads
- **Gateway → MCP Scripts Server**: HTTP with JSON-RPC payloads
- **MCP Scripts Server**: HTTP server on configurable port (default: 3000)
- **Authentication**: API key-based authentication via Authorization header

Stdio transport is NOT supported for MCP Scripts.

---

## 4. Configuration Format

### 4.1 Frontmatter Structure

MCP Scripts configuration MUST be defined in the `mcp-scripts:` section of workflow frontmatter:

```yaml
mcp-scripts:
  tool-name:
    description: "Tool description"
    inputs:
      param-name:
        type: string
        required: true
        description: "Parameter description"
        default: "default-value"
    script: |
      // JavaScript implementation
    env:
      SECRET_NAME: "${{ secrets.SECRET_NAME }}"
    timeout: 30
```

**JSON Schema**: [mcp-scripts-config.schema.json](/gh-aw/schemas/mcp-scripts-config.schema.json)

### 4.2 Tool Configuration Fields

Each tool configuration MUST contain:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | Yes | Human-readable tool description shown to agents |

Each tool configuration MAY contain:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `inputs` | object | No | Input parameter definitions (JSON Schema format) |
| `script` | string | Conditional* | JavaScript (CommonJS) implementation |
| `run` | string | Conditional* | Shell script implementation |
| `py` | string | Conditional* | Python script implementation |
| `go` | string | Conditional* | Go code implementation |
| `env` | object | No | Environment variables (typically secrets) |
| `timeout` | integer | No | Execution timeout in seconds (default: 30, applies to run/py/go only) |
| `dependencies` | array[string] | No | Package dependencies to install in execution environment (runtime-specific) |

*Exactly ONE of `script`, `run`, `py`, or `go` MUST be provided per tool.

### 4.3 Dependencies

The `dependencies` field allows specification of runtime dependencies that MUST be installed before tool execution. The package manager is inferred from the implementation language:

- **JavaScript (`script:`)**: Dependencies installed via `npm install`
- **Shell (`run:`)**: Dependencies installed via appropriate package manager (apt, yum, etc.)
- **Python (`py:`)**: Dependencies installed via `pip install`
- **Go (`go:`)**: Dependencies installed via `go get`

**Example**:

```yaml
mcp-scripts:
  analyze-json:
    description: "Analyze JSON with jq"
    inputs:
      json:
        type: string
        required: true
    run: |
      echo "$INPUT_JSON" | jq '.data | length'
    dependencies:
      - jq=1.6-2.1
    timeout: 30
```

**Python Dependencies Example**:

```yaml
mcp-scripts:
  fetch-url:
    description: "Fetch URL with requests library"
    inputs:
      url:
        type: string
        required: true
    py: |
      import requests
      import json
      response = requests.get(inputs.get('url'))
      print(json.dumps({"status": response.status_code, "content_length": len(response.text)}))
    dependencies:
      - requests==2.32.3
    timeout: 60
```

**Requirements**:
- Implementations MUST install dependencies before first tool invocation
- Dependencies MUST be pinned to exact release versions (floating references such as `latest`, bare names, or version ranges are not allowed)
- Dependencies SHOULD be cached for subsequent invocations
- Dependency installation failures MUST result in tool execution errors
- Package names MUST be valid for the target package manager
- Implementations MAY enforce security policies on allowed packages
- Deterministic dependency failures (for example, package not found, unsupported platform, invalid
  version specifier, or permission denied) MUST fail fast without retries
- Transient dependency failures (for example, network timeout or temporary registry unavailability)
  MAY be retried with bounded backoff (maximum 2 retries after the initial attempt)
- If transient retries are exhausted, execution MUST fail with a terminal tool error and MUST NOT
  continue to user code

### 4.4 Input Parameter Schema

Input parameters follow JSON Schema conventions:

```yaml
inputs:
  param-name:
    type: string|number|boolean|array|object
    description: "Parameter description"
    required: true|false
    default: value
    enum: [value1, value2, ...]
```

**Supported Types**:
- `string` - Text values
- `number` - Numeric values (integer or float)
- `boolean` - True/false values
- `array` - List of values
- `object` - Structured data

**Validation Options**:
- `required: true` - Parameter must be provided by agent
- `default: value` - Default value if not provided
- `enum: [...]` - Restrict to specific values
- `description: "..."` - Help text for agent tool selection

Array parameters MAY declare `items: { type: string }` to constrain every
element's type. The item schema supports only `type`, using the supported types
listed above, and requires an explicit parameter `type: array`. When declared,
implementations MUST publish `items` in the tool's input schema and reject
elements of a different type before invoking the handler. Without `items`,
array element types remain unrestricted.

### 4.5 Environment Variables

Environment variables provide secret access to tools:

```yaml
env:
  API_KEY: "${{ secrets.SERVICE_API_KEY }}"
  DATABASE_URL: "${{ secrets.DATABASE_URL }}"
```

**Requirements**:
- Environment variable values MAY contain GitHub Actions secret expressions (`${{ secrets.NAME }}`)
- Secret expressions MUST be resolved during compilation
- Secrets MUST be masked in logs
- Only explicitly declared secret/configuration bindings and the ordinary runtime variables listed in [Environment Variables](/gh-aw/reference/mcp-scripts/#environment-variables-env) are available to tool processes

### 4.6 Timeout Configuration

Timeout applies to Shell (`run:`), Python (`py:`), and Go (`go:`) tools:

```yaml
timeout: 120  # 2 minutes
```

**Behavior**:
- Default timeout: 60 seconds
- Minimum timeout: 1 second
- Maximum timeout: Implementation-dependent (SHOULD be at least 600 seconds)
- Timeout enforcement: Process MUST be terminated with SIGTERM, then SIGKILL after grace period
- JavaScript tools (`script:`) execute in-process and do NOT have timeout enforcement

### 4.7 Validation Requirements

Implementations MUST validate:

1. **Required Fields**: `description` field is present and non-empty
2. **Mutually Exclusive Implementations**: Exactly one of `script`, `run`, `py`, `go` is provided
3. **Input Schema**: Input definitions follow JSON Schema conventions
4. **Timeout Range**: Timeout value is positive integer (minimum 1 second)
5. **Environment Variables**: Environment variable names are valid identifiers (uppercase alphanumeric with underscores)
6. **Tool Names**: Tool names match pattern `^[a-zA-Z][a-zA-Z0-9_-]*$`
7. **Dependencies**: Dependency names are valid for target package manager
8. **Per-string length limits**: Each string-typed input parameter MUST enforce a maximum accepted
   length of at least 10KB and MUST reject oversized values before tool execution (SM-IS-01).

Implementations SHOULD validate:

1. **Script Syntax**: Syntax errors in implementation code (language-specific)
2. **Input Types**: Input parameter types are supported JSON Schema types
3. **Reserved Names**: Tool names do not conflict with built-in MCP methods
4. **Description Length**: Tool descriptions are clear and concise (recommended 10-200 characters)
5. **Timeout Reasonableness**: Timeout values are reasonable for tool purpose (warn if >600 seconds)

**Sync note (2026-05-28)**: SM-IS-01 enforcement is now implemented across all MCP transport paths. `validateStringInputLengths`
has been added to `actions/setup/js/schema-validation.cjs` and is called by both the HTTP server
in `actions/setup/js/mcp_scripts_mcp_server_http.cjs` and the shared stdio/core server in
`actions/setup/js/mcp_server_core.cjs` immediately after required-field validation.
Any string-typed input parameter whose UTF-8 byte length exceeds `MAX_STRING_INPUT_BYTES` (10 KB =
10,240 bytes) causes the tool invocation to be rejected with a descriptive error before the handler
is called. Implementations MUST NOT silently truncate oversized inputs. Verified by unit tests in
`actions/setup/js/schema-validation.test.cjs`. Closes
[#35257](https://github.com/github/gh-aw/issues/35257).

---

## 5. Tool Execution

### 5.1 Invocation Flow

1. Agent sends JSON-RPC request to MCP Scripts server
2. Server validates request format and authentication
3. Server validates tool inputs against schema
4. Server dispatches to appropriate language handler
5. Handler executes tool implementation
6. Handler captures output and errors
7. Server returns JSON-RPC response to agent

### 5.1.1 Operations Ordering

A conforming implementation MUST preserve the following operation order for each tool invocation
attempt:

1. Authenticate the request and resolve the target tool name before executing any user-defined code.
2. Apply input validation and default-value expansion before runtime startup or dependency
   installation.
3. Complete any required dependency installation or runtime bootstrap before invoking the tool body.
4. Execute the tool body exactly once for the current attempt.
5. Sanitize stdout-derived results before classifying success, generating previews, or writing
   oversized payloads to disk.
6. Apply the large-output transformation in §8 only after the sanitized success payload has been
   fully materialized for the current attempt.
7. Classify failures and set `data.recoverable` before cleanup, then clean up ephemeral resources
   before the server returns the final JSON-RPC response.

Implementations MUST NOT reorder these steps in a way that allows unsanitized output to bypass
§7.4 (Output Sanitization) or allows retry classification to observe partially cleaned-up state from
a different attempt.

### 5.2 Input Validation

Implementations MUST:

1. Validate all required parameters are provided
2. Reject requests with missing required parameters (JSON-RPC error -32602)
3. Apply default values for optional parameters
4. Validate enum constraints if specified
5. Coerce types where possible (e.g., string to number)

### 5.3 Error Handling

Implementations MUST return JSON-RPC errors for:

- **Missing Tool** (-32601): Tool name not found in registry
- **Invalid Parameters** (-32602): Required parameter missing or invalid type
- **Execution Error** (-32603): Tool execution failed (syntax error, runtime error, timeout)
- **Internal Error** (-32603): Server-side error during processing

Error responses MUST include:
- Standard JSON-RPC error structure
- Human-readable error message
- Error details in `data` field (stack trace, line numbers, etc.)
- A `data.recoverable` boolean indicating whether a retry MAY succeed (§5.7)

The `data.recoverable` field MUST conform to the following requirements:

1. The field **MUST** be present and **MUST** be a JSON boolean (`true` or `false`) for all
   execution errors (`-32603`).
2. `recoverable: true` **MUST** only be used for transient failures where the same invocation
   MAY succeed on a subsequent attempt (e.g., timeout, temporary runtime startup failure).
3. `recoverable: false` **MUST** be used for permanent failures where retry would not change
   the result (e.g., invalid script syntax, unsupported runtime dependency, deterministic
   input-validation failure detected during execution).
4. Implementations **MUST NOT** infer retryability solely from the JSON-RPC code; clients
   **MUST** use `data.recoverable` as the authoritative retryability signal in conjunction with
   the retry policy in §5.7.

### 5.4 Execution Isolation

Each tool invocation MUST be isolated:

- **Process Isolation**: Shell/Python/Go tools execute in separate containers
- **Environment Isolation**: Only the tool's declared bindings and the ordinary runtime allowlist are inherited
- **Filesystem Isolation**: Tools have access only to their execution environment
- **Network Isolation**: Tools inherit network permissions from workflow configuration

JavaScript tools execute in-process but MUST have:
- Isolated module scope
- No access to server internals
- Limited execution time (via V8 isolates or similar)

### 5.5 Output Capture

Implementations MUST:

1. Capture stdout from tool execution
2. Parse JSON output if possible
3. Return output in JSON-RPC result field
4. Handle large outputs per Section 8 (Large Output Handling)

For Shell/Python/Go tools:
- Stdout contains the tool result (MUST be valid JSON)
- Stderr is logged but not returned to agent
- Exit code 0 indicates success
- Non-zero exit code indicates failure

For JavaScript tools:
- Return value is the tool result
- Thrown errors indicate failure
- Async functions are awaited

### 5.6 Runtime Timeout Requirements

Each runtime handler (`script`, `run`, `py`, and `go`) **MUST** enforce a configurable execution timeout and **MUST** terminate tool execution when the timeout is reached.

Implementations **SHOULD** default this timeout to 30 seconds or less unless the workflow author explicitly configures a different value.

When a timeout occurs, the server **MUST** return a JSON-RPC execution error (`-32603`) that explicitly identifies timeout termination.

### 5.7 Retry Policy

Retry behavior is caller-controlled and uses the `data.recoverable` signal from §5.3.
In this section, **retry budget** means the maximum number of total attempts (initial attempt
plus retries) permitted for a single invocation.

1. MCP Scripts servers **MUST NOT** automatically retry failed tool invocations.
2. A caller **MUST** treat `data.recoverable: false` from §5.3 as terminal for that invocation
   and **MUST NOT** retry unless operator policy explicitly overrides this requirement.
3. A caller **MAY** retry when `data.recoverable: true` from §5.3. When retrying, callers
   **SHOULD** use exponential backoff with jitter:
   - Initial delay: 250 ms (or higher)
   - Backoff multiplier: 2x
   - Maximum delay: 5 s
4. The default retry budget for recoverable failures **SHOULD NOT** exceed 3 attempts total
   (initial attempt + up to 2 retries) unless workflow-specific reliability requirements justify
   a higher budget.
5. Because tool invocations may be non-idempotent, callers **MUST** treat retry safety as a
   caller responsibility and **SHOULD** apply idempotency safeguards before retrying
   state-changing tools. Callers SHOULD use one of the following techniques:
   - **Idempotency key**: include a unique, stable identifier (e.g., a UUID derived from the
     original request) in the tool input so the tool or downstream service can detect and
     deduplicate re-submissions.
   - **Side-effect check**: before retrying, query the external system to confirm the
     prior attempt did not produce the intended effect (e.g., verify the resource does not
     already exist before attempting creation again).
   Example (idempotency key pattern):
   ```json
   {
     "tool": "create-github-issue",
     "input": {
       "title": "Deploy failed",
       "idempotency_key": "deploy-fail-2026-05-26-abc123"
     }
   }
   ```
6. Each retry **MUST** begin from a fresh invocation attempt: callers and servers **MUST NOT** reuse
   partially emitted stdout, partially written large-output files, or partially initialized runtime
   state from a previous failed attempt as the result for the retry.
7. When a recoverable attempt fails after producing side effects outside the tool process (for
   example, creating a remote resource before timing out), callers **SHOULD** perform explicit
   side-effect checks or compensating cleanup before retrying.
8. Once the retry budget is exhausted, the caller **MUST** surface the final failure as terminal and
   **SHOULD** include the total attempts made when reporting the error to operators.

---

## 6. Language Support

### 6.1 JavaScript Tools (`script:`)

#### 6.1.1 Execution Environment

JavaScript tools MUST:
- Execute in Node.js environment
- Use CommonJS module format
- Be wrapped in async function with destructured inputs
- Have access to `process.env` for secrets
- Have access to GitHub Actions global objects (`github`, `context`, `core`, `io`, `exec`, `glob`, `artifact`)

#### 6.1.2 Available Global Objects

JavaScript tools have access to standard GitHub Actions JavaScript libraries without explicit import:

- **`github`**: GitHub API client from `@actions/github`
- **`context`**: Workflow context information from `@actions/github`
- **`core`**: Actions core utilities from `@actions/core`
- **`io`**: File I/O utilities from `@actions/io`
- **`exec`**: Command execution utilities from `@actions/exec`
- **`glob`**: File pattern matching from `@actions/glob`
- **`artifact`**: Artifact management from `@actions/artifact`

**Example using global objects**:

```yaml
mcp-scripts:
  create-issue:
    description: "Create a GitHub issue"
    inputs:
      title:
        type: string
        required: true
      body:
        type: string
        required: true
    script: |
      const octokit = github.getOctokit(process.env.GITHUB_TOKEN);
      const { data } = await octokit.rest.issues.create({
        owner: context.repo.owner,
        repo: context.repo.repo,
        title,
        body
      });
      return { number: data.number, url: data.html_url };
    env:
      GITHUB_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
```

**Requirements**:
- Global objects MUST be available without `require()` statements
- Tools MAY use these globals alongside user code
- Implementations MUST provide same version of libraries as GitHub Actions runtime
- No restrictions on where tools execute (in-process or containerized)
- Tool code MUST NOT invoke workflow-control side effects via global objects (`core.setFailed()`,
  `core.setOutput()`, workflow summary writes, or equivalent run-level mutators)
- Tool failures MUST be expressed by returning structured error payloads or by throwing exceptions
  handled as tool-level failures, not by mutating workflow/job status directly

#### 6.1.3 Code Wrapping

Implementation code is wrapped as:

```javascript
async function execute(inputs) {
  const { param1, param2 } = inputs;
  // User code here
}
```

#### 6.1.4 Example

```yaml
mcp-scripts:
  greet-user:
    description: "Greet a user by name"
    inputs:
      name:
        type: string
        required: true
    script: |
      return { message: `Hello, ${name}!` };
```

### 6.2 Shell Tools (`run:`)

#### 6.2.1 Execution Environment

Shell tools MUST:
- Execute in bash shell
- Run in containerized environment (Docker)
- Have inputs as environment variables with `INPUT_` prefix
- Output valid JSON to stdout

#### 6.2.2 Input Mapping

Input parameters are mapped to environment variables:
- Parameter `repo` becomes `$INPUT_REPO`
- Parameter `state` becomes `$INPUT_STATE`
- Naming convention: `INPUT_${UPPERCASE_PARAM_NAME}`

#### 6.2.3 Example

```yaml
mcp-scripts:
  list-prs:
    description: "List pull requests"
    inputs:
      repo:
        type: string
        required: true
      state:
        type: string
        default: "open"
    run: |
      gh pr list --repo "$INPUT_REPO" --state "$INPUT_STATE" --json number,title
    env:
      GH_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
    timeout: 30
```

### 6.3 Python Tools (`py:`)

#### 6.3.1 Execution Environment

Python tools MUST:
- Execute using Python 3.10+ interpreter
- Run in containerized environment (Docker)
- Have access to standard library modules
- Receive inputs as dictionary variable `inputs`
- Output valid JSON to stdout

#### 6.3.2 Input Access

Input parameters are available via `inputs` dictionary:
- `inputs.get('param_name')` - Access parameter value
- `inputs.get('param_name', default)` - Access with default
- Parameters use original names (not uppercased)

#### 6.3.3 Example

```yaml
mcp-scripts:
  analyze-data:
    description: "Analyze numeric data"
    inputs:
      numbers:
        type: string
        description: "Comma-separated numbers"
        required: true
    py: |
      import json
      
      numbers_str = inputs.get('numbers', '')
      numbers = [float(x.strip()) for x in numbers_str.split(',') if x.strip()]
      
      result = {
          "count": len(numbers),
          "sum": sum(numbers),
          "average": sum(numbers) / len(numbers) if numbers else 0
      }
      
      print(json.dumps(result))
    timeout: 60
```

### 6.4 Go Tools (`go:`)

#### 6.4.1 Execution Environment

Go tools MUST:
- Execute using `go run` command
- Run in containerized environment (Docker)
- Have access to standard library imports
- Receive inputs as `map[string]any` from stdin
- Output valid JSON to stdout

#### 6.4.2 Code Wrapping

Implementation code is wrapped in:

```go
package main

import (
    "encoding/json"
    "fmt"
    "io"
    "os"
)

func main() {
    // Parse inputs from stdin
    var inputs map[string]any
    decoder := json.NewDecoder(os.Stdin)
    if err := decoder.Decode(&inputs); err != nil {
        fmt.Fprintf(os.Stderr, "Error parsing inputs: %v\n", err)
        os.Exit(1)
    }
    
    // User code here
}
```

#### 6.4.3 Available Imports

The following imports are automatically included:
- `encoding/json` - JSON encoding/decoding
- `fmt` - Formatted I/O
- `io` - I/O primitives
- `os` - Operating system functionality

Additional imports MAY be added by the user in their code.

#### 6.4.4 Module and Dependency Requirements

**go.mod requirement**: Go tools that use external (non-standard-library) packages MUST supply a
`go.mod` file. The `go.mod` content MUST be provided either:

1. **Inline** in the `go:` script body, as a separate section delimited by `// go.mod` comments
   (implementation-defined syntax); or
2. **Via the `dependencies` field**, where each entry is a `module@version` string (e.g.,
   `github.com/some/pkg@v1.2.3`) that the runtime will use to generate a minimal `go.mod`.

Go tools that use only standard library packages MAY omit the `go.mod` declaration; the runtime
MUST generate a minimal `go.mod` with `go 1.21` (the minimum supported version) in that case.

**Minimum supported Go version**: Implementations MUST support Go **1.21** or later. The Go
toolchain version used in the containerized environment SHOULD be declared in the tool's
`dependencies` or documented in the workflow's README. Tools relying on language features
introduced after 1.21 MUST specify the required version in their `go.mod` `go` directive.

**R-GO-001**: If a `go.mod` is provided and specifies a minimum Go version, the runtime MUST use
a toolchain that satisfies that requirement and MUST fail with a descriptive error if no
conforming toolchain is available.

**R-GO-002**: The runtime MUST NOT cache `go.mod` state across tool invocations. Each invocation
MUST start from a clean module cache to ensure reproducible dependency resolution.

#### 6.4.5 Example

```yaml
mcp-scripts:
  calculate:
    description: "Perform calculations"
    inputs:
      a:
        type: number
        required: true
      b:
        type: number
        required: true
    go: |
      a := inputs["a"].(float64)
      b := inputs["b"].(float64)
      result := map[string]any{
          "sum": a + b,
          "product": a * b,
      }
      json.NewEncoder(os.Stdout).Encode(result)
    timeout: 30
```

---

## 7. Security Model

### 7.1 Secret Isolation

Implementations MUST:

1. **Explicit Access**: Only secret/configuration bindings declared in the tool's `env:` are available, alongside the ordinary runtime allowlist
2. **Secret Masking**: Secrets referenced via `${{ secrets.NAME }}` are masked in logs
3. **No Global Access**: Tools cannot access workflow secrets not explicitly declared
4. **Environment Isolation**: Each tool has isolated environment variable namespace

The compiler MUST assign distinct server-side transport references to each `(tool name, variable name)` pair. Identical variable names with different secret expressions MUST remain distinct through startup and invocation. JavaScript, shell, Python, and Go handlers MUST construct fresh child environments from the ordinary runtime allowlist and only their own resolved bindings; they MUST NOT inherit server authentication keys, sibling transport references, or arbitrary job credentials. Missing transport references MUST produce an explicit error. Dependency installers MUST receive only the ordinary runtime allowlist, not tool secrets. Existing workflows require recompilation to generate these transport references.

This environment boundary does not provide a filesystem or network sandbox. The current handlers execute trusted code in separate processes on the runner host.

### 7.2 Process Isolation

Implementations MUST provide:

1. **Containerization**: Shell, Python, and Go tools execute in Docker containers
2. **Process Boundaries**: Each invocation is a separate process
3. **Resource Limits**: Containers enforce CPU, memory, and filesystem limits
4. **Network Restrictions**: Network access controlled by workflow configuration

JavaScript tools MUST provide:

1. **Module Isolation**: Tools execute in isolated module scope
2. **Limited Execution**: Use V8 isolates or similar for CPU/memory limits
3. **No Server Access**: Tools cannot access server internals or other tools

**SM-JS-01**: JavaScript tools MUST execute in a sandboxed V8 context with restricted global scope. Implementations MUST NOT expose Node.js global objects (e.g., `process`, `require`, `__dirname`) to tool scripts unless explicitly permitted by the tool configuration.

### 7.3 Input Sanitization

Implementations MUST:

1. Validate input types against schema before execution
2. Reject inputs that do not conform to schema
3. Prevent code injection via input validation
4. Apply length limits to string inputs (MUST enforce a maximum input string length of at least 10KB)

**SM-IS-01**: Implementations MUST enforce a maximum input string length of at least 10KB for each string-typed input parameter. Inputs exceeding the configured maximum MUST be rejected with a validation error before the tool script is invoked. Implementations MUST NOT silently truncate oversized inputs. The limit is enforced via `validateStringInputLengths` in `actions/setup/js/schema-validation.cjs` (`MAX_STRING_INPUT_BYTES` = 10,240 bytes). **Status: Implemented 2026-05-28.**

### 7.4 Output Sanitization

Implementations MUST:

1. Parse and validate JSON output from tools
2. Reject non-JSON output from Shell/Python/Go tools
3. Apply size limits to output (see Section 8)
4. Remove or mask any accidental secret exposure in output

**SM-01**: Implementations MUST sanitize tool stdout before forwarding the result to the MCP
client. Any string value in the tool's output that matches a **registered secret pattern** MUST be
redacted and replaced with the string `"[REDACTED]"` prior to serialization. This requirement
applies to all output fields, including nested JSON objects and arrays.

A *registered secret pattern* is any secret value that the GitHub Actions runner has registered
for masking (i.e., any value sourced from `${{ secrets.NAME }}` references declared in the
workflow's `env:` block, as described in §7.1). Implementations MUST obtain the list of active
secret values from the runner's masking registry (GitHub Actions `::add-mask::` mechanism) to
determine which patterns to redact. See §7.1 for the secret isolation model that governs which
secrets are in scope.

**SM-02**: The secret pattern matching used for output sanitization MUST be consistent with the
masking logic applied to runner logs. Implementations MUST reuse or delegate to the sanitization
helper functions provided in `actions/setup/js/` (specifically the output-redaction utilities in
`actions/setup/js/mcp_scripts_mcp_server_http.cjs` and related helpers) rather than implementing
independent redaction logic.

**SM-03**: Implementations MUST NOT forward raw tool stdout to the MCP client without first
passing it through the output sanitization pipeline. This applies regardless of whether the tool
declares secrets in its `env:` field; residual secret values that appear in stdout through indirect
means (e.g., subprocess output, error messages) MUST also be redacted.

### 7.5 Timeout Enforcement

Implementations MUST:

1. Enforce timeout for Shell/Python/Go tools
2. Terminate processes that exceed timeout
3. Send SIGTERM, wait for grace period (5 seconds), then SIGKILL
4. Return timeout error to agent
5. Clean up container resources after timeout

### 7.6 Norms

This subsection establishes normative security norms for secret lifecycle management and
secret scope that are not fully captured by §7.1–§7.5.

#### 7.6.1 Secret Rotation Norms

- **SN-ROT-01**: Workflows that use long-lived secrets (e.g., API tokens, service account
  credentials) SHOULD define an explicit rotation policy. The rotation period SHOULD be
  documented in the workflow's frontmatter `description` field or an adjacent README.
- **SN-ROT-02**: Implementations SHOULD NOT cache secret values across tool invocations beyond
  the lifetime of a single workflow run. Any in-process cache of a resolved secret MUST be
  cleared at the end of the run.
- **SN-ROT-03**: When a secret rotation event is detected (e.g., a new version of a GitHub
  secret is deployed), implementations MUST NOT reuse a previously resolved secret value that
  was cached before the rotation. Callers SHOULD re-resolve secrets at the start of each
  workflow run.

#### 7.6.2 Secret Scope Norms

- **SN-SCOPE-01**: Secrets declared in the workflow `env:` block MUST be scoped to the
  minimum required access level. Workflows SHOULD NOT declare secrets with broader permissions
  than necessary for the tools they invoke.
- **SN-SCOPE-02**: Implementations MUST NOT propagate a tool's declared secrets to child
  processes or containers that are not part of that tool's execution scope (as defined in §7.1
  and §7.2).
- **SN-SCOPE-03**: When a workflow declares the same secret for multiple tools, implementations
  MUST ensure each tool receives only its own environment variable binding and MUST NOT expose
  one tool's secrets to another tool's execution context.
- **SN-SCOPE-04**: Secrets MUST NOT be included in tool result objects, log lines, or any
  output forwarded to the MCP client. See §7.4 (SM-01 through SM-03) for the output
  sanitization requirements that enforce this norm at runtime.

---

## 8. Large Output Handling

### 8.1 Size Threshold

When tool output exceeds 500 characters, implementations MUST:

1. Save complete output to a file
2. Generate unique filename in accessible location
3. Return metadata response instead of full content

### 8.2 Metadata Response Format

```json
{
  "content": {
    "type": "file",
    "path": "/tmp/tool-output-abc123.json",
    "size": 15234,
    "message": "Output too large (15234 bytes). Saved to file."
  },
  "preview": {
    "schema": {
      "type": "array",
      "items": { "type": "object" }
    },
    "first_item": { ... },
    "item_count": 42
  }
}
```

**Required Fields**:
- `content.type`: MUST be "file"
- `content.path`: File path accessible to agent
- `content.size`: File size in bytes
- `content.message`: Human-readable explanation

**Optional Fields**:
- `preview.schema`: JSON schema of content
- `preview.first_item`: First item in array/list
- `preview.item_count`: Number of items in collection

### 8.2.1 Response Structure Norms

- The large-output response **MUST** preserve the original tool result envelope and replace only the
  oversized content payload with the `content` metadata object shown above.
- The `content` object **MUST NOT** embed the full original payload inline once the file indirection
  path is chosen.
- `preview` is OPTIONAL, but when present it **MUST** summarize sanitized content from the same
  attempt that produced `content.path`; implementations **MUST NOT** mix preview data from a prior
  failed or retried attempt.
- For collection-shaped outputs, `preview.first_item` and `preview.item_count` SHOULD describe the
  collection shape without requiring the client to open the file immediately. For non-collection
  outputs, implementations MAY omit these fields and return only `preview.schema`.

### 8.3 File Access

Implementations MUST:

1. Store output files in location accessible to agent
2. Use unique, non-predictable filenames
3. Clean up files after workflow completion
4. Enforce file size limits (SHOULD be at least 10MB)

---

## 9. Integration with MCP Gateway

### 9.1 Configuration Extension

MCP Scripts extends the MCP Gateway configuration format. During workflow compilation:

1. MCP Scripts tools are compiled into MCP server configuration
2. Configuration is passed to MCP Gateway as additional server
3. Gateway routes requests to MCP Scripts server
4. MCP Scripts server handles tool execution

### 9.2 Gateway Communication

MCP Scripts server MUST:

1. Expose HTTP endpoint for MCP communication
2. Accept JSON-RPC requests from gateway
3. Require authentication via Authorization header
4. Return JSON-RPC responses to gateway

### 9.3 Configuration Generation

At compilation time, MCP Scripts generates:

```json
{
  "mcpServers": {
    "safeinputs": {
      "type": "http",
      "url": "http://localhost:3000",
      "headers": {
        "Authorization": "generated-api-key"
      }
    }
  }
}
```

This configuration is merged with other MCP servers and passed to gateway.

### 9.4 Server Lifecycle

MCP Scripts server:

1. **Startup**: Server starts during workflow initialization
2. **Tool Registration**: All tools are registered at startup
3. **Runtime**: Server accepts requests throughout workflow execution
4. **Shutdown**: Server terminates when workflow completes
5. **Cleanup**: All ephemeral resources are cleaned up

---

## 10. Safeguards

This section elevates the MCP Scripts threat model and mitigation requirements for direct normative
reference.

### Threat Model

- **S-MCP-001**: Secret leakage vectors include tool stdout/stderr, JSON responses, dependency
  installer logs, and exception stack traces.
- **S-MCP-002**: Container escape attempts can originate from shell/python/go tools via privilege
  escalation paths such as host mounts, kernel interfaces, or unrestricted network egress.
- **S-MCP-003**: Cross-tool contamination risk exists when one invocation attempts to read another
  tool's environment bindings or temporary artifacts.

### Required Mitigations

- **S-MCP-004**: Implementations MUST enforce secret isolation, output sanitization, execution
  isolation, and network isolation defaults exactly as described in §7.1–§7.4 and Appendix D.
- **S-MCP-005**: Dependency installation MUST fail closed when package integrity cannot be
  established.

### Residual Risk

Logic-level metadata exfiltration and zero-day container runtime vulnerabilities remain residual
risks. Operators SHOULD pair this specification with least-privilege repository controls and
continuous runtime patching.

---

## 11. Norms

This section elevates the security norms from §7.6 into top-level normative requirements.

### Secret Rotation Norms

- **N-SN-ROT-01**: Workflows that use long-lived secrets SHOULD define an explicit rotation policy.
- **N-SN-ROT-02**: Implementations SHOULD NOT cache secret values across tool invocations beyond a
  single workflow run, and any in-process cache MUST be cleared at run end.
- **N-SN-ROT-03**: When secret rotation is detected, implementations MUST NOT reuse stale cached
  secret values from before the rotation event.

### Secret Scope Norms

- **N-SN-SCOPE-01**: Secrets declared in workflow `env:` MUST be scoped to the minimum required
  access level.
- **N-SN-SCOPE-02**: Implementations MUST NOT propagate a tool's declared secrets to child
  processes or containers outside that tool's execution scope.
- **N-SN-SCOPE-03**: When the same secret is declared for multiple tools, each tool MUST receive
  only its own environment-variable binding and MUST NOT gain access to sibling tool bindings.
- **N-SN-SCOPE-04**: Secrets MUST NOT be included in tool result objects, logs, or output returned
  to MCP clients.

---

## 12. Compliance Testing

### 12.1 Test Suite Requirements

A conforming implementation MUST pass the following test categories:

#### 12.1.1 Configuration Tests

- **T-CFG-001**: Valid tool with JavaScript implementation
- **T-CFG-002**: Valid tool with Shell implementation
- **T-CFG-003**: Valid tool with Python implementation
- **T-CFG-004**: Valid tool with Go implementation
- **T-CFG-005**: Tool with all input parameter types
- **T-CFG-006**: Tool with environment variables
- **T-CFG-007**: Tool with custom timeout
- **T-CFG-008**: Reject tool without description
- **T-CFG-009**: Reject tool with multiple implementations
- **T-CFG-010**: Reject tool with invalid timeout

#### 12.1.2 Input Validation Tests

- **T-VAL-001**: Required parameter validation
- **T-VAL-002**: Optional parameter with default
- **T-VAL-003**: Enum constraint validation
- **T-VAL-004**: Type coercion (string to number)
- **T-VAL-005**: Invalid type rejection
- **T-VAL-006**: Missing required parameter error

#### 12.1.3 Execution Tests

- **T-EXE-001**: JavaScript tool successful execution
- **T-EXE-002**: Shell tool successful execution
- **T-EXE-003**: Python tool successful execution
- **T-EXE-004**: Go tool successful execution
- **T-EXE-005**: Tool with secret access
- **T-EXE-006**: Tool timeout enforcement
- **T-EXE-007**: Tool execution error handling
- **T-EXE-008**: Tool with JSON output parsing

#### 12.1.4 Security Tests

- **T-SEC-001**: Secret isolation verification
- **T-SEC-002**: Environment variable isolation
- **T-SEC-003**: Process isolation (Shell/Python/Go)
- **T-SEC-004**: Input sanitization
- **T-SEC-005**: Output sanitization
- **T-SEC-006**: Secret masking in logs
- **T-SEC-007**: Dependency installation security
- **T-SEC-008**: GitHub Actions global objects access control
- **T-MCP-050**: Go sandbox network isolation (no unrestricted outbound access without explicit
  `network.allowed` entries)
- **T-MCP-051**: Secret-scope isolation for SN-SCOPE-01 through SN-SCOPE-04 (per-tool binding only,
  no cross-tool secret leakage, no secret values in returned output)

#### 12.1.5 Large Output Tests

- **T-OUT-001**: Output under 500 characters (direct return)
- **T-OUT-002**: Output over 500 characters (file save)
- **T-OUT-003**: Metadata response format
- **T-OUT-004**: File accessibility to agent
- **T-OUT-005**: JSON schema preview generation

#### 12.1.6 Dependencies Tests

- **T-DEP-001**: npm dependency installation for JavaScript tools
- **T-DEP-002**: pip dependency installation for Python tools
- **T-DEP-003**: go get dependency installation for Go tools
- **T-DEP-004**: apt/yum dependency installation for shell tools
- **T-DEP-005**: Dependency caching behavior
- **T-DEP-006**: Dependency installation failure handling

#### 12.1.7 Integration Tests

- **T-INT-001**: MCP Gateway configuration generation
- **T-INT-002**: HTTP MCP server startup
- **T-INT-003**: Authentication with gateway
- **T-INT-004**: JSON-RPC request handling
- **T-INT-005**: Error response format

#### 12.1.8 Negative Tests

- **T-MS-NEG-001**: Tool definition with missing `script` (or `run`, `py`, `go`) field — implementation MUST reject the configuration at compile time with an error identifying the missing implementation field. The error MUST reference the tool name and the required field names.
- **T-MS-NEG-002**: Tool input schema referencing an undefined type (e.g., `type: "uuid"`) — implementation MUST reject the schema at validation time with an error indicating the unsupported type. The error MUST include the tool name, parameter name, and the invalid type value.

### 12.2 Compliance Checklist

| Requirement | Test ID | Level | Status |
|-------------|---------|-------|--------|
| JavaScript tools | T-CFG-001, T-EXE-001 | 1 | Required |
| Shell tools | T-CFG-002, T-EXE-002 | 2 | Standard |
| Python tools | T-CFG-003, T-EXE-003 | 2 | Standard |
| Go tools | T-CFG-004, T-EXE-004 | 3 | Complete |
| Input validation | T-VAL-* | 1 | Required |
| Secret isolation | T-SEC-001, T-SEC-002 | 1 | Required |
| Process isolation | T-SEC-003 | 2 | Standard |
| Go sandbox network isolation | T-MCP-050 | 3 | Complete |
| Secret scope norms (SN-SCOPE-01–04) | T-MCP-051 | 2 | Standard |
| Timeout handling | T-EXE-006 | 2 | Standard |
| Large output handling | T-OUT-* | 3 | Complete |
| Dependencies support | T-DEP-* | 2 | Standard |
| GitHub Actions globals | T-SEC-008 | 1 | Required |
| MCP Gateway integration | T-INT-* | 1 | Required |
| Missing implementation field rejection | T-MS-NEG-001 | 1 | Required |
| Invalid input schema type rejection | T-MS-NEG-002 | 1 | Required |

### 12.3 Test Execution

Implementations SHOULD provide:

1. Automated test runner for compliance suite
2. Test result reporting in standard format
3. Test fixtures for common scenarios
4. Integration test environment setup
5. Conformance report generation

---

## Appendices

### Appendix A: Complete Examples

#### A.1 JavaScript Tool with Secrets

```yaml
mcp-scripts:
  fetch-api-data:
    description: "Fetch data from external API"
    inputs:
      endpoint:
        type: string
        required: true
        description: "API endpoint path"
      method:
        type: string
        default: "GET"
        enum: ["GET", "POST", "PUT", "DELETE"]
    script: |
      const apiKey = process.env.API_KEY;
      const baseUrl = process.env.API_BASE_URL;
      
      const response = await fetch(`${baseUrl}/${endpoint}`, {
        method,
        headers: {
          'Authorization': `Bearer ${apiKey}`,
          'Content-Type': 'application/json'
        }
      });
      
      if (!response.ok) {
        throw new Error(`API request failed: ${response.status}`);
      }
      
      return await response.json();
    env:
      API_KEY: "${{ secrets.SERVICE_API_KEY }}"
      API_BASE_URL: "https://api.example.com"
```

#### A.2 Shell Tool with GitHub CLI

```yaml
mcp-scripts:
  list-issues:
    description: "List GitHub issues using gh CLI"
    inputs:
      repo:
        type: string
        required: true
        description: "Repository in owner/name format"
      state:
        type: string
        default: "open"
        enum: ["open", "closed", "all"]
      limit:
        type: number
        default: 30
        description: "Maximum number of issues to return"
    run: |
      #!/bin/bash
      set -euo pipefail
      
      gh issue list \
        --repo "$INPUT_REPO" \
        --state "$INPUT_STATE" \
        --limit "$INPUT_LIMIT" \
        --json number,title,state,createdAt,author
    env:
      GH_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
    timeout: 60
```

#### A.3 Python Tool with Data Processing

```yaml
mcp-scripts:
  process-metrics:
    description: "Process and aggregate metric data"
    inputs:
      data:
        type: string
        required: true
        description: "JSON array of metric objects"
      group_by:
        type: string
        default: "category"
        description: "Field to group by"
    py: |
      import json
      from collections import defaultdict
      
      # Parse input data
      data_str = inputs.get('data', '[]')
      data = json.loads(data_str)
      group_by = inputs.get('group_by', 'category')
      
      # Group and aggregate
      groups = defaultdict(list)
      for item in data:
        key = item.get(group_by, 'unknown')
        groups[key].append(item)
      
      # Calculate statistics
      result = {}
      for key, items in groups.items():
        values = [item.get('value', 0) for item in items]
        result[key] = {
          'count': len(items),
          'sum': sum(values),
          'avg': sum(values) / len(values) if values else 0,
          'min': min(values) if values else 0,
          'max': max(values) if values else 0
        }
      
      print(json.dumps(result))
    timeout: 120
```

#### A.4 Go Tool with HTTP Request

```yaml
mcp-scripts:
  health-check:
    description: "Check health of multiple endpoints"
    inputs:
      urls:
        type: string
        required: true
        description: "Comma-separated list of URLs"
      timeout_seconds:
        type: number
        default: 10
        description: "Timeout for each request"
    go: |
      import (
        "context"
        "net/http"
        "strings"
        "time"
      )
      
      // Parse URLs
      urlsStr := inputs["urls"].(string)
      urls := strings.Split(urlsStr, ",")
      timeoutSecs := int(inputs["timeout_seconds"].(float64))
      
      // Check each URL
      results := make(map[string]any)
      client := &http.Client{
        Timeout: time.Duration(timeoutSecs) * time.Second,
      }
      
      for _, url := range urls {
        url = strings.TrimSpace(url)
        start := time.Now()
        
        resp, err := client.Get(url)
        duration := time.Since(start).Milliseconds()
        
        if err != nil {
          results[url] = map[string]any{
            "status": "error",
            "error": err.Error(),
            "duration_ms": duration,
          }
          continue
        }
        
        results[url] = map[string]any{
          "status": "success",
          "status_code": resp.StatusCode,
          "duration_ms": duration,
        }
        resp.Body.Close()
      }
      
      json.NewEncoder(os.Stdout).Encode(results)
    timeout: 60
```

#### A.5 Complete Tool with Dependencies and GitHub Integration

```yaml
mcp-scripts:
  analyze-pr-complexity:
    description: "Analyze pull request complexity and provide metrics"
    inputs:
      pr_number:
        type: number
        required: true
        description: "Pull request number to analyze"
      include_files:
        type: boolean
        default: true
        description: "Include per-file analysis"
    script: |
      const octokit = github.getOctokit(process.env.GITHUB_TOKEN);
      const { owner, repo } = context.repo;
      
      // Fetch PR data
      const { data: pr } = await octokit.rest.pulls.get({
        owner,
        repo,
        pull_number: pr_number
      });
      
      // Fetch PR files
      const { data: files } = await octokit.rest.pulls.listFiles({
        owner,
        repo,
        pull_number: pr_number
      });
      
      // Calculate complexity metrics
      const metrics = {
        pr_number: pr_number,
        title: pr.title,
        author: pr.user.login,
        files_changed: files.length,
        total_additions: files.reduce((sum, f) => sum + f.additions, 0),
        total_deletions: files.reduce((sum, f) => sum + f.deletions, 0),
        total_changes: files.reduce((sum, f) => sum + f.changes, 0),
        complexity_score: 0
      };
      
      // Calculate complexity score (simple heuristic)
      metrics.complexity_score = 
        (metrics.files_changed * 2) + 
        (metrics.total_changes / 10);
      
      // Add per-file analysis if requested
      if (include_files) {
        metrics.file_analysis = files.map(f => ({
          filename: f.filename,
          status: f.status,
          additions: f.additions,
          deletions: f.deletions,
          changes: f.changes
        }));
      }
      
      return metrics;
    env:
      GITHUB_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
    timeout: 120
```

### Appendix B: Error Response Examples

#### B.1 Missing Required Parameter

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32602,
    "message": "Invalid params",
    "data": {
      "missing": ["name"],
      "provided": ["age"],
      "schema": {
        "name": { "type": "string", "required": true },
        "age": { "type": "number", "required": false }
      }
    }
  },
  "id": "req-123"
}
```

#### B.2 Tool Execution Timeout

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32603,
    "message": "Internal error",
    "data": {
      "error": "Tool execution timeout",
      "timeout_seconds": 60,
      "tool": "process-data"
    }
  },
  "id": "req-456"
}
```

#### B.3 Invalid Tool Output

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32603,
    "message": "Internal error",
    "data": {
      "error": "Tool output is not valid JSON",
      "stdout": "Error: Cannot parse data\n",
      "stderr": "SyntaxError at line 42"
    }
  },
  "id": "req-789"
}
```

#### B.4 Dependency Installation Failure

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32603,
    "message": "Internal error",
    "data": {
      "error": "Dependency installation failed",
      "dependency": "requests",
      "package_manager": "pip",
      "stderr": "Could not find a version that satisfies the requirement requests"
    }
  },
  "id": "req-101"
}
```

### Appendix C: Security Considerations

#### C.1 Secret Management

- Secrets MUST be explicitly declared in `env:` section
- Secrets SHOULD use GitHub Actions secret syntax (`${{ secrets.NAME }}`)
- Secrets MUST be masked in all logs and output
- Tools SHOULD NOT log or return secrets in output
- Secret rotation SHOULD be handled at workflow level

#### C.2 Input Validation

- All input parameters MUST be validated against schema
- String inputs SHOULD have length limits
- Numeric inputs SHOULD have range limits
- Input sanitization MUST prevent code injection
- Malicious input MUST NOT compromise host system

#### C.3 Container Security

- Shell/Python/Go tools SHOULD run in minimal containers
- Containers SHOULD have read-only root filesystem where possible
- Resource limits SHOULD be enforced (CPU, memory, disk)
- Network access SHOULD be restricted by default
- Container images SHOULD be verified and signed

#### C.4 Output Security

- Large outputs SHOULD be saved to temporary files
- Temporary files SHOULD be cleaned up after workflow
- Output SHOULD NOT contain secrets or sensitive data
- Output size SHOULD be limited to prevent DoS
- File paths SHOULD be non-predictable

#### C.5 Dependency Security

- Implementations SHOULD validate package names against known malicious packages
- Dependency sources SHOULD be from trusted registries (npm, PyPI, Go modules)
- Implementations MAY enforce allowlists for permitted packages
- Dependency versions SHOULD be pinned when possible
- Security advisories SHOULD be checked for known vulnerabilities

#### C.6 GitHub Actions Integration Security

- Global objects MUST be provided in sandboxed environment
- Token access MUST be controlled through explicit env declarations
- API rate limits SHOULD be enforced to prevent abuse
- Actions permissions SHOULD follow least privilege principle
- Audit logging SHOULD track all GitHub API operations

---

### Appendix D: Safeguards

#### D.1 Threat Model

Primary threat classes for MCP Scripts deployments:

1. **Secret leakage vectors**: tool stdout/stderr, JSON responses, dependency installer logs, and
   exception stack traces may expose secret values.
2. **Container escape scenarios**: shell/python/go tools may attempt privilege escalation through
   host mounts, kernel interfaces, or unrestricted network egress.
3. **Cross-tool contamination**: one tool invocation attempting to read another tool's environment
   or temporary output artifacts.

#### D.2 Required Mitigations

- **Secret isolation**: only explicitly declared `env` keys are injected; undeclared secrets MUST be
  inaccessible.
- **Output sanitization**: all tool output MUST pass through redaction before client return (see
  §7.4 SM-01..SM-03).
- **Execution isolation**: shell/python/go tools MUST execute in isolated containers/processes with
  bounded resources and no host workspace mount by default.
- **Network isolation**: outbound access from containerized tools MUST be denied by default and MUST
  be granted only for explicitly allowed domains.
- **Dependency controls**: dependency installation MUST fail closed when package integrity cannot be
  established.

#### D.3 Residual Risk

Residual risk remains for logic-level exfiltration via intentionally returned non-secret metadata
and for zero-day container runtime vulnerabilities. Operators SHOULD pair this specification with
repository-level least privilege and continuous runtime patching.

---

## 13. Sync Notes

This section maps each normative section of the MCP Scripts Specification to the Go source
files in `pkg/workflow/` that implement it. This mapping is maintained to assist contributors
in verifying that specification changes are reflected in the implementation and vice versa.

After any change to this specification, run `make recompile` to regenerate compiled lock files,
and run `go test ./pkg/workflow/...` to verify conformance.

| Section | Title | Primary Source File(s) |
|---------|-------|------------------------|
| §3 | Architecture | `pkg/workflow/mcp_scripts_parser.go` (type definitions, `MCPScriptsConfig`, `MCPScriptToolConfig`), `pkg/workflow/mcp_scripts_renderer.go` (gateway config rendering) |
| §4 | Configuration Format | `pkg/workflow/mcp_scripts_parser.go` (`parseMCPScriptsMap`, `parseMCPScriptToolConfig`), `pkg/parser/` (frontmatter YAML parsing) |
| §5 | Tool Execution | `pkg/workflow/mcp_scripts_generator.go` (`GenerateMCPScriptJavaScriptToolScript`, `GenerateMCPScriptShellToolScript`, `GenerateMCPScriptPythonToolScript`), `actions/setup/js/` (runtime JS execution harness) |
| §6 | Language Support | `pkg/workflow/mcp_scripts_generator.go` (per-language script generation), `actions/setup/sh/` (shell harness), `pkg/workflow/mcp_scripts_parser.go` (`parseTimeoutString`) |
| §7 | Security Model | `pkg/workflow/mcp_scripts_renderer.go` (`collectMCPScriptsSecrets`, `renderMCPScriptsMCPConfigWithOptions`), `pkg/workflow/mcp_scripts_parser.go` (env/secret field parsing) |
| §8 | Large Output Handling | `pkg/workflow/mcp_scripts_generator.go` (output truncation logic), `actions/setup/js/mcp_scripts_mcp_server_http.cjs` (HTTP transport output streaming) |
| §9 | Integration with MCP Gateway | `pkg/workflow/mcp_scripts_renderer.go` (`renderMCPScriptsMCPConfigWithOptions`), `pkg/workflow/mcp_scripts_generator.go` (`GenerateMCPScriptsMCPServerScript`, `GenerateMCPScriptsToolsConfig`) |
| §10 | Safeguards | `pkg/workflow/mcp_scripts_renderer.go`, `pkg/workflow/mcp_scripts_generator.go`, `actions/setup/js/mcp_scripts_mcp_server_http.cjs` |
| §11 | Norms | `pkg/workflow/mcp_scripts_renderer.go`, `pkg/workflow/mcp_scripts_parser.go` |

Sync follow-up tasks:

- Add/maintain compliance coverage for **T-MCP-051** in
  `pkg/workflow/mcp_scripts_renderer_test.go` for SN-SCOPE-01 through SN-SCOPE-04 secret-scope
  behavior.

### Security Marker Sync Map

| Marker | Implementation File(s) | Enforcement Path |
|---|---|---|
| SM-JS-01 | `pkg/workflow/mcp_scripts_generator.go`, `actions/setup/js/mcp_server_core.cjs` | `GenerateMCPScriptJavaScriptToolScript` emits per-tool JS handlers; `loadToolHandlers` in `mcp_server_core.cjs` executes handlers in isolated subprocesses |
| SM-IS-01 | `actions/setup/js/mcp_scripts_mcp_server_http.cjs`, `actions/setup/js/mcp_server_core.cjs`, `actions/setup/js/schema-validation.cjs` | `validateStringInputLengths` called after `validateRequiredFields` in both the HTTP server (`createMCPServer`) and the stdio/core server (`handleMessage`); rejects any string-typed input parameter whose UTF-8 byte length exceeds `MAX_STRING_INPUT_BYTES` (10,240 bytes). **Status: Implemented 2026-05-28.** |
| SM-03 | `actions/setup/js/mcp_server_core.cjs`, `actions/setup/js/mcp_scripts_mcp_server_http.cjs` | Tool-call response path serializes handler output before returning MCP `content`; raw passthrough handling is centralized in server transport/handler pipeline |

### Implementation Notes

- **§3 — Architecture**: The `MCPScriptsConfig` and `MCPScriptToolConfig` structs in
  `mcp_scripts_parser.go` define the in-memory representation of the configuration format.
  The `mcp_scripts_renderer.go` file translates this representation into the MCP Gateway
  JSON configuration at compile time.

- **§4 — Configuration Format**: Frontmatter YAML is parsed by `parseMCPScriptsMap` in
  `mcp_scripts_parser.go`. The `dependencies` field (added in v1.1.0) is handled in
  `parseMCPScriptToolConfig` and propagated to `tools.json` by
  `GenerateMCPScriptsToolsConfig` in `mcp_scripts_generator.go`. Runtime dependency
  installation is performed before first invocation in
  `actions/setup/js/mcp_dependencies_manager.cjs` via `loadToolHandlers` in
  `actions/setup/js/mcp_server_core.cjs`. JSON Schema validation is provided by
  `pkg/parser/schemas/mcp-scripts-config.schema.json`.

- **§5 — Tool Execution**: Each language's tool script is generated by a dedicated function
  in `mcp_scripts_generator.go`. The JavaScript HTTP transport server entry point is
  generated by `GenerateMCPScriptsMCPServerScript` and executed at runtime via
  `actions/setup/js/mcp_scripts_mcp_server_http.cjs`.

- **§6 — Language Support**: The `implementation` field is mapped to a language constant in
  `mcp_scripts_parser.go`. Go tool support requires a separate container image; see
  `MCPScriptsMode` constants for transport mode selection.

- **§7 — Security Model**: Secret environment variable injection is performed by
  `collectMCPScriptsSecrets` in `mcp_scripts_renderer.go`, using the same per-tool
  transport names as `GenerateMCPScriptsToolsConfig`. `loadToolHandlers` passes each
  tool's references to its language handler; `createToolEnvironment` in
  `mcp_handler_process.cjs` resolves only those references and the runtime allowlist.
  The rendered gateway config includes a `guard` block to restrict tool access to declared inputs only.

- **§8 — Large Output Handling**: Output truncation thresholds are defined as constants in
  `mcp_scripts_generator.go`. The HTTP transport implementation in
  `mcp_scripts_mcp_server_http.cjs` enforces the same limits at runtime.

- **§9 — MCP Gateway Integration**: The gateway config JSON written to
  `/tmp/gh-aw/mcp-config/mcp-servers.json` at runtime is generated by
  `renderMCPScriptsMCPConfigWithOptions`. The `includeCopilotFields` parameter controls
  whether Copilot-specific gateway fields are emitted.

### Go Sandbox Constraints for `go` Language Tools

When `implementation: go` is specified, the tool executes inside a containerized Go
sandbox with the following constraints:

- **Network access**: No outbound network calls are permitted from within the Go tool sandbox
  unless the workflow's `network.allowed` list explicitly includes the target host. The
  sandbox is air-gapped by default.
- **Filesystem access**: The tool has read-write access to a temporary working directory
  (`/tmp/tool-workspace`). It cannot access the runner's workspace (`/home/runner/work`)
  or any GitHub Actions environment files.
- **Binary execution**: `go run` is used for single-file tools. Multi-file tools require a
  `go.mod` to be included in the script body or supplied via `dependencies`.
- **Timeout enforcement**: The `timeout` field (default 30 seconds) is enforced via
  `context.WithTimeout` wrapping the subprocess. Exceeding the timeout causes the tool to
  return an MCP error response with code `-32001`.
- **Secret access**: Secrets are injected as environment variables only if declared in the
  tool's `env:` field, alongside ordinary runtime variables. Go runs in a separate
  process on the runner host, not a credential-isolated filesystem sandbox.

### Norms Audit (2026-06-01)

The following is an audit of normative coverage for tool execution ordering, retry semantics,
and error propagation — three areas that implementations commonly need explicit guidance on.

#### Tool Execution Ordering

**Status: Explicitly specified — no gap.**

Section 5.1 specifies stateless, session-independent invocation. Each tool call is
independent with no defined ordering dependency across calls. The MCP protocol layer handles
queueing. No additional ordering norm is required.

#### Retry Semantics

**Status: Specified in §5.7 — no gap.**

Section §5.7 now defines normative retry semantics, including caller-controlled retry,
retry budgets, backoff strategy guidance, and idempotency responsibilities.

#### Error Propagation

**Status: Specified in §5.3 and §5.7 — no gap.**

Section §5.3 now defines a required `data.recoverable` boolean with normative semantics,
and §5.7 defines how callers MUST/SHOULD interpret that signal for retries.

_All open action items from the 2026-05-08 audit have been resolved. No outstanding gaps remain._

---

## References

### Normative References

- **[RFC 2119]** Key words for use in RFCs to Indicate Requirement Levels
- **[JSON-RPC 2.0]** JSON-RPC 2.0 Specification
- **[MCP]** Model Context Protocol Specification
- **[JSON Schema]** JSON Schema Specification (Draft 7)

### Informative References

- **[MCP Gateway Specification]** GitHub Agentic Workflows MCP Gateway Specification
- **[GitHub Actions]** GitHub Actions Workflow Syntax
- **[Docker]** Docker Container Runtime

---

## Change Log

### Version 1.1.0 (Draft)

- **Added**: Dependencies support (Section 4.3)
  - `dependencies` field for specifying runtime package dependencies
  - Package manager inferred from implementation language (npm, pip, go get, apt/yum)
  - Dependencies installed before tool execution
  - Examples added for Python requests and shell jq dependencies
- **Added**: GitHub Actions global objects for JavaScript tools (Section 6.1.2)
  - Global `github`, `context`, `core`, `io`, `exec`, `glob`, `artifact` objects
  - Available without explicit `require()` statements
  - Added side-effect constraint: tools MUST NOT call workflow control mutators (for example, `core.setFailed()`)
  - No restrictions on execution location (in-process or containerized)
  - Example demonstrating GitHub API usage via global objects
- **Clarified**: `dependencies` installation failure semantics in Section 4.3 (fail-fast for deterministic failures, bounded retry for transient failures)
- **Added**: Appendix D safeguards threat model covering secret leakage vectors, container escape scenarios, and residual risk
- **Added**: Compliance test ID `T-MCP-050` for Go sandbox network isolation
- **Updated**: Section numbering to accommodate new sections

### Version 1.0.0 (Draft)

- Initial specification release
- Configuration format definition
- Language support (JavaScript, Shell, Python, Go)
- Security model specification
- Large output handling
- MCP Gateway integration
- Compliance test framework

---

*Copyright © 2026 GitHub, Inc. All rights reserved.*
