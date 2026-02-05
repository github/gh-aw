---
safe-inputs:
  gh-aw-logs:
    description: "Download and analyze GitHub Actions workflow logs. This tool builds the gh-aw binary and executes the 'gh-aw logs' command to download workflow run logs."
    inputs:
      engine:
        type: string
        description: "Filter logs by engine (copilot, claude, codex, etc.)"
        required: false
      start-date:
        type: string
        description: "Start date for log download (e.g., -30d for last 30 days, -7d for last 7 days, 2024-01-01)"
        required: false
      output-dir:
        type: string
        description: "Output directory for downloaded logs (default: /tmp/gh-aw/workflow-logs)"
        required: false
      workflow:
        type: string
        description: "Filter by workflow name"
        required: false
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      
      # Build the gh-aw binary
      echo "Building gh-aw binary..."
      cd "$GITHUB_WORKSPACE"
      make build
      
      # Prepare arguments
      ARGS="logs"
      
      if [[ -n "$INPUT_ENGINE" ]]; then
        ARGS="$ARGS --engine $INPUT_ENGINE"
      fi
      
      if [[ -n "$INPUT_START_DATE" ]]; then
        ARGS="$ARGS --start-date $INPUT_START_DATE"
      fi
      
      if [[ -n "$INPUT_OUTPUT_DIR" ]]; then
        ARGS="$ARGS -o $INPUT_OUTPUT_DIR"
      else
        ARGS="$ARGS -o /tmp/gh-aw/workflow-logs"
      fi
      
      if [[ -n "$INPUT_WORKFLOW" ]]; then
        ARGS="$ARGS --workflow $INPUT_WORKFLOW"
      fi
      
      # Execute gh-aw logs command
      echo "Executing: ./gh-aw $ARGS"
      ./gh-aw $ARGS
      
      # Verify logs were downloaded
      OUTPUT_DIR="${INPUT_OUTPUT_DIR:-/tmp/gh-aw/workflow-logs}"
      echo "Downloaded workflow logs:"
      find "$OUTPUT_DIR" -maxdepth 1 -ls 2>/dev/null || echo "No logs found in $OUTPUT_DIR"

  gh-aw-audit:
    description: "Audit a specific GitHub Actions workflow run. This tool builds the gh-aw binary and executes the 'gh-aw audit' command."
    inputs:
      run-id:
        type: string
        description: "GitHub Actions workflow run ID to audit"
        required: true
      output-dir:
        type: string
        description: "Output directory for audit results (default: /tmp/gh-aw/audit)"
        required: false
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      
      # Build the gh-aw binary
      echo "Building gh-aw binary..."
      cd "$GITHUB_WORKSPACE"
      make build
      
      # Prepare arguments
      ARGS="audit $INPUT_RUN_ID"
      
      if [[ -n "$INPUT_OUTPUT_DIR" ]]; then
        ARGS="$ARGS -o $INPUT_OUTPUT_DIR"
      else
        ARGS="$ARGS -o /tmp/gh-aw/audit"
      fi
      
      # Execute gh-aw audit command
      echo "Executing: ./gh-aw $ARGS"
      ./gh-aw $ARGS
---

## gh-aw CLI Safe Input Tools

This shared workflow provides safe-input tools for executing gh-aw CLI commands within workflows. The tools automatically build the gh-aw binary before execution.

### Available Tools

1. **gh-aw-logs** - Download and analyze GitHub Actions workflow logs
2. **gh-aw-audit** - Audit a specific workflow run

### Usage

Import this shared workflow to get access to gh-aw CLI tools:

```yaml
imports:
  - shared/gh-aw-cli.md
```

### Tool Parameters

#### gh-aw-logs

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| engine | string | No | Filter logs by engine (copilot, claude, codex) |
| start-date | string | No | Start date for logs (e.g., -30d, -7d, 2024-01-01) |
| output-dir | string | No | Output directory (default: /tmp/gh-aw/workflow-logs) |
| workflow | string | No | Filter by workflow name |

**Example usage:**

```yaml
tools:
  agentic-workflows:

imports:
  - shared/gh-aw-cli.md

steps:
  - name: Download workflow logs
    run: |
      # This will fail - use safe-input tool instead
      # ./gh-aw logs --engine copilot --start-date -30d
```

Instead, use the tool via the agentic-workflows MCP server:

```
Use the gh-aw-logs tool with:
- engine: "copilot"
- start-date: "-30d"
- output-dir: "/tmp/gh-aw/workflow-logs"
```

#### gh-aw-audit

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| run-id | string | Yes | Workflow run ID to audit |
| output-dir | string | No | Output directory (default: /tmp/gh-aw/audit) |

**Example usage:**

```
Use the gh-aw-audit tool with:
- run-id: "123456789"
- output-dir: "/tmp/gh-aw/audit"
```

### Important Notes

- These tools automatically build the gh-aw binary using `make build`
- The binary is built from the checked-out repository code
- `GITHUB_TOKEN` is automatically provided for authentication
- Tools run within the workflow's context with appropriate permissions

### Why Use Safe Inputs?

Direct execution of `./gh-aw` commands in workflow steps fails because:
1. The binary is not built by default
2. Building requires Go toolchain setup
3. Safe-input tools handle the build process automatically
4. Provides consistent execution environment

### Migration Example

**Before (fails):**
```yaml
steps:
  - name: Download workflow logs
    run: |
      ./gh-aw logs --engine copilot --start-date -30d -o /tmp/gh-aw/workflow-logs
```

**After (works):**
```yaml
imports:
  - shared/gh-aw-cli.md

# In the agent prompt:
# Use the gh-aw-logs tool with engine: "copilot", start-date: "-30d"
```
