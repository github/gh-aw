---
title: MCP Scripts
description: Define custom MCP tools inline as JavaScript or shell scripts with secret access, providing lightweight tool creation without external dependencies.
sidebar:
  order: 750
---

The [`mcp-scripts:`](/gh-aw/reference/glossary/#mcp-scripts) element allows you to define custom [MCP](/gh-aw/reference/glossary/#mcp-model-context-protocol) (Model Context Protocol) tools directly in your workflow [frontmatter](/gh-aw/reference/glossary/#frontmatter) using JavaScript, shell scripts, or Python. These tools are generated at runtime and run as an HTTP MCP server **on the GitHub Actions runner, outside the agent container**. The agent reaches the server via `host.docker.internal`, keeping tool execution isolated from the AI sandbox while still providing controlled secret access.

> [!WARNING]
> **MCP Scripts run outside the agent sandbox and must only implement READ-ONLY operations.**
>
> Because MCP Scripts execute directly on the GitHub Actions runner host — not inside the isolated agent container — they can access the runner's file system, network, and environment without the safety constraints of the sandbox. Implementing **write operations** (creating files, pushing commits, calling mutating APIs, etc.) in MCP Scripts bypasses the audit trail and approval gates that protect your repository.
>
> - ✅ **Use MCP Scripts for**: reading data, querying APIs, fetching information, performing calculations.
> - ❌ **Do not use MCP Scripts for**: writing files, creating issues/PRs, modifying repository content, or any other mutating action.
>
> For write operations, use [Safe Output Jobs](/gh-aw/reference/safe-outputs/) or [Custom Safe Output Jobs](/gh-aw/reference/custom-safe-outputs/) instead. These run in a controlled, auditable step that is separate from the AI agent's execution.

## Quick Start

```yaml wrap
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

The agent can now call `greet-user` with a `name` parameter.

## HTTP Transport Limits

Generated workflows create a per-run API key and configure the MCP gateway to send it verbatim in the `Authorization` header. The HTTP server requires `GH_AW_MCP_SCRIPTS_API_KEY` at startup and rejects missing or incorrect request credentials with HTTP 401. `GET /health` and `OPTIONS` preflight requests remain unauthenticated.

Request bodies are limited to 1 MiB, including chunked uploads. Oversized requests return HTTP 413; malformed JSON returns HTTP 400. Headers and body uploads each have a 30-second timeout. The upload deadline does not limit tool execution time.

## Tool Definition

Each mcp-script tool requires a unique name and configuration:

```yaml wrap
mcp-scripts:
  tool-name:
    description: "What the tool does"  # Required
    inputs:                            # Optional parameters
      param1:
        type: string
        required: true
        description: "Parameter description"
      param2:
        type: number
        default: 10
    script: |                          # JavaScript implementation
      // Your code here
    env:                               # Environment variables
      API_KEY: "${{ secrets.API_KEY }}"
    timeout: 120                       # Optional: timeout in seconds (default: 60)
```

Each tool requires `description:` and exactly one of `script:`, `run:`, `py:`, or `go:`.

## JavaScript Tools (`script:`)

JavaScript tools wrap your `script:` in `async function execute(inputs)` with inputs destructured. Access secrets via `process.env`:

```yaml wrap
mcp-scripts:
  fetch-data:
    description: "Fetch data from API"
    inputs:
      endpoint:
        type: string
        required: true
    script: |
      const apiKey = process.env.API_KEY;
      const response = await fetch(`https://api.example.com/${endpoint}`, {
        headers: { Authorization: `Bearer ${apiKey}` }
      });
      return await response.json();
    env:
      API_KEY: "${{ secrets.API_KEY }}"
```

## Shell Tools (`run:`)

Shell scripts execute in bash with inputs as environment variables (e.g., `repo` → `INPUT_REPO`):

```yaml wrap
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
```

**Shared gh CLI Tool**: Import `shared/gh.md` for a reusable gh tool that accepts any CLI command via args parameter.

## Python Tools (`py:`)

Python tools execute using `python3` with inputs available as a dictionary. Access inputs via `inputs.get('name')`, secrets via `os.environ`, and return results by printing JSON to stdout:

```yaml wrap
mcp-scripts:
  analyze-data:
    description: "Analyze data with Python"
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
```

Python 3.10+ is available with standard library modules. For third-party packages, use the `dependencies:` field with exact version pins (for example, `requests==2.32.3`) so gh-aw installs them before first tool invocation.

## Go Tools (`go:`)

Go tools execute using `go run` with inputs provided as a `map[string]any` parsed from stdin. Standard library imports (`encoding/json`, `fmt`, `io`, `os`) are automatically included:

```yaml wrap
mcp-scripts:
  calculate:
    description: "Perform calculations with Go"
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
```

Your Go code receives `inputs map[string]any` from stdin and should output JSON to stdout. The code is wrapped in a `package main` with a `main()` function that handles input parsing.

Access secrets via `os.Getenv("VAR_NAME")` (see [Environment Variables](#environment-variables-env) for the `env:` field).

## Input Parameters

Define typed parameters with validation:

```yaml wrap
mcp-scripts:
  example-tool:
    description: "Example with all input options"
    inputs:
      required-param:
        type: string
        required: true
        description: "This parameter is required"
      optional-param:
        type: number
        default: 42
        description: "This has a default value"
      args:
        type: array
        items:
          type: string
        description: "An array of argument strings"
      choice-param:
        type: string
        enum: ["option1", "option2", "option3"]
        description: "Limited to specific values"
```

Array parameters accept an optional `items: { type: string }` declaration to
validate every element. Item types support `string`, `number`, `boolean`, `array`,
and `object`; only the item `type` is supported. Omitting `items` leaves element
types unrestricted. `items` requires the parameter's `type` to be `array`.

## Timeout Configuration

Set execution timeout with `timeout:` field (default: 60 seconds):

```yaml wrap
mcp-scripts:
  slow-processing:
    description: "Process large dataset"
    timeout: 300  # 5 minutes (default: 60)
    py: |
      import json
      import time
      time.sleep(120)
      print(json.dumps({"status": "complete"}))
```

Enforced for shell (`run:`) and Python (`py:`) tools. JavaScript (`script:`) tools run in-process without timeout enforcement.

## Environment Variables (`env:`)

Pass secrets and configuration via `env:` (available in JavaScript via `process.env`, shell via `$VAR_NAME`):

```yaml wrap
mcp-scripts:
  secure-tool:
    description: "Tool with multiple secrets"
    script: |
      const { API_KEY, API_SECRET } = process.env;
      // Use secrets...
    env:
      API_KEY: "${{ secrets.SERVICE_API_KEY }}"
      API_SECRET: "${{ secrets.SERVICE_API_SECRET }}"
```

Secrets using `${{ secrets.* }}` are masked in logs.

## Large Output Handling

When output exceeds 500 characters, it's saved to a file. The agent receives the file path, size, and JSON schema preview (if applicable).

## Importing MCP Scripts

Import tools from shared workflows using `imports:`. Local tool definitions override imported ones on name conflicts:

```yaml wrap
imports:
  - shared/github-tools.md
```

## Complete Example

```yaml wrap
---
on: workflow_dispatch
engine: copilot
imports:
  - shared/pr-data-mcp-script.md
mcp-scripts:
  analyze-text:
    description: "Analyze text and return statistics"
    inputs:
      text:
        type: string
        required: true
    script: |
      const words = text.split(/\s+/).filter(w => w.length > 0);
      return {
        word_count: words.length,
        char_count: text.length,
        avg_word_length: (text.length / words.length).toFixed(2)
      };
safe-outputs:
  create-discussion:
    category: "General"
---

Analyze provided text using the `analyze-text` tool and create a discussion with results.
```

## Security Considerations

MCP Scripts tools run on the GitHub Actions **runner host** — outside the agent container — so they can access the runner's file system and environment but are isolated from the AI's own execution environment. Tools also provide secret isolation (only specified env vars are forwarded), process isolation (separate execution), and output sanitization (large outputs saved to files). Only predefined tools are available to agents.

MCP script arguments are withheld from server logs, step summaries, and recent-tool-call diagnostics. Output payloads and error details are also withheld from server logs because they may echo arguments. Handlers receive the original inputs, and tool responses remain unchanged. Do not return secrets to the agent.

## Troubleshooting

- **Tool Not Found**: The tool name the agent calls must exactly match the key under `mcp-scripts:` in frontmatter (case-sensitive). Check the agent's tool list in the run logs against your `mcp-scripts:` block.
- **Script Errors**: Check the tool's error response for details. The `MCP Scripts` step logs tool names and execution status, but redacts payloads and error details that may contain secrets.
- **Secret Not Available**: Confirm the secret referenced in `env:` (e.g., `${{ secrets.API_KEY }}`) is defined at the repository or organization level under **Settings > Secrets and variables > Actions**, and that its name matches exactly.
- **Large Output**: When output exceeds 500 characters, the agent receives a file path instead of inline content (see [Large Output Handling](#large-output-handling)); read that file if the agent's response looks truncated.

## Learn More

- [MCP Scripts Specification](/gh-aw/specs/mcp-scripts-specification/) - Formal W3C-style specification
- [Tools](/gh-aw/reference/tools/) - Other tool configuration options
- [Imports](/gh-aw/reference/imports/) - Importing shared workflows
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Automated post-workflow actions
- [MCPs](/gh-aw/guides/mcps/) - External MCP server integration
- [Custom Safe Output Jobs](/gh-aw/reference/custom-safe-outputs/) - Post-workflow custom jobs
