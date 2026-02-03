---
title: Safe Inputs
description: Define custom MCP tools inline as JavaScript or shell scripts with secret access, providing lightweight tool creation without external dependencies.
sidebar:
  order: 750
---

> [!CAUTION]
> Experimental Feature
> Safe Inputs is an experimental feature. The API and behavior may change in future releases.

The [`safe-inputs:`](/gh-aw/reference/glossary/#safe-inputs) (validated user input tools) element allows you to define custom [MCP](/gh-aw/reference/glossary/#mcp-model-context-protocol) (Model Context Protocol) tools directly in your workflow [frontmatter](/gh-aw/reference/glossary/#frontmatter) using JavaScript, shell scripts, or Python. These tools are generated at runtime and mounted as an MCP server, giving your agent access to custom functionality with controlled secret access.

## Quick Start

```yaml wrap
safe-inputs:
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

## Tool Definition

Each safe-input tool requires a unique name and configuration:

```yaml wrap
safe-inputs:
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

### Required Fields

- **`description:`** - Human-readable description of what the tool does. This is shown to the agent for tool selection.

### Optional Fields

- **`timeout:`** - Maximum execution time in seconds (default: 60). The tool will be terminated if it exceeds this duration. Applies to shell (`run:`) and Python (`py:`) tools.

### Implementation Options

Choose one implementation method:

- **`script:`** - JavaScript (CommonJS) code
- **`run:`** - Shell script
- **`py:`** - Python script (Python 3.1x)
- **`go:`** - Go (Golang) code

You can only use one of `script:`, `run:`, `py:`, or `go:` per tool.

## JavaScript Tools (`script:`)

JavaScript tools are automatically wrapped in an async function with destructured inputs:

```yaml wrap
safe-inputs:
  calculate-sum:
    description: "Add two numbers"
    inputs:
      a:
        type: number
        required: true
      b:
        type: number
        required: true
    script: |
      const result = a + b;
      return { sum: result };
```

Your script is wrapped as `async function execute(inputs)` with inputs destructured. Access secrets via `process.env`:

```yaml wrap
safe-inputs:
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
safe-inputs:
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
safe-inputs:
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

Python 3.10+ is available with standard library modules. Install additional packages inline using pip if needed.

## Go Tools (`go:`)

Go tools execute using `go run` with inputs provided as a `map[string]any` parsed from stdin. Standard library imports (`encoding/json`, `fmt`, `io`, `os`) are automatically included:

```yaml wrap
safe-inputs:
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

**Available by default:**
- `encoding/json` - JSON encoding/decoding
- `fmt` - Formatted I/O
- `io` - I/O primitives
- `os` - Operating system functionality

Access environment variables (including secrets) using `os.Getenv()`:

```yaml wrap
safe-inputs:
  api-call:
    description: "Call an API with Go"
    inputs:
      endpoint:
        type: string
        required: true
    go: |
      apiKey := os.Getenv("API_KEY")
      endpoint := inputs["endpoint"].(string)

      // Make your API call here
      result := map[string]any{
          "endpoint": endpoint,
          "authenticated": apiKey != "",
      }
      json.NewEncoder(os.Stdout).Encode(result)
    env:
      API_KEY: "${{ secrets.API_KEY }}"
```

## Input Parameters

Define typed parameters with validation:

```yaml wrap
safe-inputs:
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
      choice-param:
        type: string
        enum: ["option1", "option2", "option3"]
        description: "Limited to specific values"
```

### Supported Types

- `string` - Text values
- `number` - Numeric values
- `boolean` - True/false values
- `array` - List of values
- `object` - Structured data

### Validation Options

- `required: true` - Parameter must be provided
- `default: value` - Default if not provided
- `enum: [...]` - Restrict to specific values
- `description: "..."` - Help text for the agent

## Timeout Configuration

Set execution timeout with `timeout:` field (default: 60 seconds):

```yaml wrap
safe-inputs:
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
safe-inputs:
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

## Importing Safe Inputs

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
  - shared/pr-data-safe-input.md
safe-inputs:
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

Tools provide secret isolation (only specified env vars), process isolation (separate execution), and output sanitization (large outputs saved to files). Only predefined tools are available to agents.

## Comparison with Other Options

| Feature | Safe Inputs | Custom MCP Servers | Bash Tool |
|---------|-------------|-------------------|-----------|
| Setup | Inline in frontmatter | External service | Simple commands |
| Languages | JavaScript, Shell, Python | Any language | Shell only |
| Secret Access | Controlled via `env:` | Full access | Workflow env |
| Isolation | Process-level | Service-level | None |

## Troubleshooting

- **Tool Not Found**: Verify tool name matches exactly
- **Script Errors**: Check workflow logs for syntax errors
- **Secret Not Available**: Confirm secret name in repository/org settings
- **Large Output**: Agent reads file path from response

## Related Documentation

- [Safe Inputs Specification](/gh-aw/reference/safe-inputs-specification/) - Formal W3C-style specification
- [Tools](/gh-aw/reference/tools/) - Other tool configuration options
- [Imports](/gh-aw/reference/imports/) - Importing shared workflows
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Automated post-workflow actions
- [MCPs](/gh-aw/guides/mcps/) - External MCP server integration
- [Custom Safe Output Jobs](/gh-aw/reference/custom-safe-outputs/) - Post-workflow custom jobs
