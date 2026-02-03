---
# Structured Logging for Agent Sessions
# 
# This shared component ensures all agent workflows produce analyzable logs
# that can be collected and analyzed by session analysis workflows.
#
# Usage:
#   imports:
#     - shared/structured-logging.md
#
# Then call these functions in your workflow prompt:
#   - log_session_start(): At the beginning of agent execution
#   - log_session_step(step_name, details): For each major step
#   - log_session_end(status, summary): At the end of agent execution
#   - log_tool_call(tool_name, success, details): After each tool call
#   - log_error(error_type, message): When errors occur

tools:
  bash:
    - "echo *"
    - "date *"
    - "cat /tmp/gh-aw/session.log"
    - "mkdir -p /tmp/gh-aw"

steps:
  - name: Initialize structured logging
    run: |
      #!/bin/bash
      set -e
      
      # Create logging directory
      mkdir -p /tmp/gh-aw
      
      # Initialize session log file
      SESSION_LOG="/tmp/gh-aw/session.log"
      
      # Create log file with session metadata
      cat > "$SESSION_LOG" << 'EOF'
      ========================================
      AGENT SESSION LOG
      ========================================
      Workflow: ${{ github.workflow }}
      Run ID: ${{ github.run_id }}
      Run Number: ${{ github.run_number }}
      Repository: ${{ github.repository }}
      Actor: ${{ github.actor }}
      Event: ${{ github.event_name }}
      Started: $(date -u '+%Y-%m-%d %H:%M:%S UTC')
      ========================================
      EOF
      
      echo "✓ Structured logging initialized at $SESSION_LOG"
      
      # Create logging helper functions script
      cat > /tmp/gh-aw/log-helpers.sh << 'HELPERS_EOF'
      #!/bin/bash
      # Structured logging helper functions
      
      SESSION_LOG="/tmp/gh-aw/session.log"
      
      log_session_start() {
        local agent_name="${1:-Unknown Agent}"
        local task="${2:-No task specified}"
        echo "" >> "$SESSION_LOG"
        echo "========================================" >> "$SESSION_LOG"
        echo "SESSION START" >> "$SESSION_LOG"
        echo "========================================" >> "$SESSION_LOG"
        echo "Agent: $agent_name" >> "$SESSION_LOG"
        echo "Task: $task" >> "$SESSION_LOG"
        echo "Timestamp: $(date -u '+%Y-%m-%d %H:%M:%S UTC')" >> "$SESSION_LOG"
        echo "========================================" >> "$SESSION_LOG"
        echo "" >> "$SESSION_LOG"
        echo "::notice::Session started - $agent_name"
      }
      
      log_session_step() {
        local step_name="${1:-Unnamed Step}"
        local details="${2:-No details}"
        echo "" >> "$SESSION_LOG"
        echo "--- STEP: $step_name ---" >> "$SESSION_LOG"
        echo "Time: $(date -u '+%Y-%m-%d %H:%M:%S UTC')" >> "$SESSION_LOG"
        echo "Details: $details" >> "$SESSION_LOG"
        echo "" >> "$SESSION_LOG"
        echo "::notice::Step completed - $step_name"
      }
      
      log_session_end() {
        local status="${1:-unknown}"
        local summary="${2:-No summary provided}"
        echo "" >> "$SESSION_LOG"
        echo "========================================" >> "$SESSION_LOG"
        echo "SESSION END" >> "$SESSION_LOG"
        echo "========================================" >> "$SESSION_LOG"
        echo "Status: $status" >> "$SESSION_LOG"
        echo "Summary: $summary" >> "$SESSION_LOG"
        echo "Ended: $(date -u '+%Y-%m-%d %H:%M:%S UTC')" >> "$SESSION_LOG"
        echo "========================================" >> "$SESSION_LOG"
        
        if [ "$status" = "success" ]; then
          echo "::notice::Session completed successfully - $summary"
        else
          echo "::warning::Session ended with status: $status - $summary"
        fi
        
        # Output log summary to GitHub Actions
        echo "" >> "$SESSION_LOG"
        echo "--- SESSION SUMMARY ---" >> "$SESSION_LOG"
        cat "$SESSION_LOG" >> "$SESSION_LOG"
      }
      
      log_tool_call() {
        local tool_name="${1:-unknown_tool}"
        local success="${2:-unknown}"
        local details="${3:-No details}"
        echo "" >> "$SESSION_LOG"
        echo "TOOL CALL: $tool_name" >> "$SESSION_LOG"
        echo "  Success: $success" >> "$SESSION_LOG"
        echo "  Details: $details" >> "$SESSION_LOG"
        echo "  Time: $(date -u '+%Y-%m-%d %H:%M:%S UTC')" >> "$SESSION_LOG"
        
        if [ "$success" = "true" ] || [ "$success" = "yes" ]; then
          echo "::notice::Tool call succeeded - $tool_name"
        else
          echo "::warning::Tool call failed - $tool_name: $details"
        fi
      }
      
      log_error() {
        local error_type="${1:-Unknown Error}"
        local message="${2:-No message}"
        echo "" >> "$SESSION_LOG"
        echo "ERROR: $error_type" >> "$SESSION_LOG"
        echo "  Message: $message" >> "$SESSION_LOG"
        echo "  Time: $(date -u '+%Y-%m-%d %H:%M:%S UTC')" >> "$SESSION_LOG"
        echo "::error::$error_type - $message"
      }
      
      log_context_update() {
        local context_type="${1:-general}"
        local update="${2:-No update}"
        echo "" >> "$SESSION_LOG"
        echo "CONTEXT UPDATE: $context_type" >> "$SESSION_LOG"
        echo "  Update: $update" >> "$SESSION_LOG"
        echo "  Time: $(date -u '+%Y-%m-%d %H:%M:%S UTC')" >> "$SESSION_LOG"
      }
      
      # Export functions for use in agent scripts
      export -f log_session_start
      export -f log_session_step
      export -f log_session_end
      export -f log_tool_call
      export -f log_error
      export -f log_context_update
      HELPERS_EOF
      
      chmod +x /tmp/gh-aw/log-helpers.sh
      echo "✓ Log helper functions created at /tmp/gh-aw/log-helpers.sh"
      
      # Source the helpers so they're available
      source /tmp/gh-aw/log-helpers.sh
---

# Structured Logging Component

This shared component provides structured logging capabilities for all agent workflows to ensure session logs are always collected and analyzable.

## Purpose

**Problem**: 56% of agent sessions produce no analyzable log content, making it impossible to:
- Diagnose issues in failed sessions
- Learn from successful patterns
- Track agent behavior over time
- Measure improvement in agent performance

**Solution**: This component ensures every agent workflow produces structured, analyzable logs by:
1. Initializing a session log file at `/tmp/gh-aw/session.log`
2. Providing helper functions for logging key events
3. Capturing session metadata automatically
4. Making logs available for downstream analysis

## Usage in Agent Workflows

### 1. Import the Component

Add to your workflow frontmatter:

```yaml
imports:
  - shared/structured-logging.md
```

### 2. Call Logging Functions

In your agent prompt, instruct the agent to call these bash functions at appropriate times:

```markdown
## Your Mission

Before starting your work, log the session start:
```bash
source /tmp/gh-aw/log-helpers.sh
log_session_start "Agent Name" "Task description"
```

As you work, log each major step:
```bash
log_session_step "Phase 1: Analysis" "Analyzing repository structure"
```

After each tool call:
```bash
log_tool_call "github_api" "true" "Successfully fetched issue data"
```

If errors occur:
```bash
log_error "API Error" "Failed to fetch PR details: 404 Not Found"
```

When complete, log the session end:
```bash
log_session_end "success" "Completed analysis and posted comment"
```
```

## Available Functions

### `log_session_start <agent_name> <task>`
Records the start of an agent session with metadata.
- **agent_name**: Name of the agent (e.g., "Q", "Scout", "Archie")
- **task**: Brief description of the task being performed

### `log_session_step <step_name> <details>`
Records completion of a major step in the workflow.
- **step_name**: Name of the step (e.g., "Phase 1: Data Collection")
- **details**: Brief description of what was done

### `log_session_end <status> <summary>`
Records the end of an agent session.
- **status**: One of: "success", "failure", "partial", "abandoned"
- **summary**: Brief summary of what was accomplished

### `log_tool_call <tool_name> <success> <details>`
Records a tool invocation.
- **tool_name**: Name of the tool used (e.g., "github_api", "serena", "playwright")
- **success**: "true" or "false" indicating if the tool call succeeded
- **details**: Brief description of what the tool did or any errors

### `log_error <error_type> <message>`
Records an error that occurred during execution.
- **error_type**: Type of error (e.g., "API Error", "Permission Denied", "Timeout")
- **message**: Detailed error message

### `log_context_update <context_type> <update>`
Records changes to agent context or state.
- **context_type**: Type of context update (e.g., "memory", "state", "configuration")
- **update**: Description of the update

## Log File Location

Logs are written to `/tmp/gh-aw/session.log` and automatically captured by GitHub Actions as part of the workflow run logs.

## Benefits

1. **Consistent Format**: All agent sessions use the same structured log format
2. **Easy Analysis**: Logs can be parsed and analyzed programmatically
3. **Debugging**: Failed sessions have detailed information for troubleshooting
4. **Learning**: Successful patterns can be identified and replicated
5. **Metrics**: Session performance can be tracked over time
6. **Minimal Overhead**: Logging adds negligible runtime cost

## Integration with Session Analysis

The `copilot-session-insights` workflow automatically collects these logs and analyzes them to:
- Calculate session success rates
- Identify behavioral patterns
- Detect common failure modes
- Generate actionable recommendations

## Example Agent Integration

Here's how to integrate structured logging into an agent workflow:

```markdown
---
name: Example Agent
imports:
  - shared/structured-logging.md
---

# Example Agent

You are an example agent that demonstrates structured logging.

## Mission

When you start, initialize logging:

```bash
source /tmp/gh-aw/log-helpers.sh
log_session_start "Example Agent" "Demonstrate structured logging"
```

For each phase of work:

```bash
# Phase 1
log_session_step "Phase 1: Setup" "Configuring environment"
# ... do work ...

# Phase 2
log_session_step "Phase 2: Execution" "Running analysis"
# ... do work ...

# Log tool calls
log_tool_call "github_api" "true" "Fetched repository data"

# Log any errors
log_error "Validation Error" "Invalid input format"
```

When complete:

```bash
log_session_end "success" "Analysis complete, report generated"
```
```

## Implementation Notes

- **Automatic Initialization**: The log file is created automatically when this component is imported
- **Helper Functions**: Bash functions are sourced and available throughout the workflow
- **GitHub Actions Integration**: Logs use `::notice::` and `::warning::` annotations for visibility
- **Timestamp Format**: All timestamps use UTC in ISO 8601 format for consistency
- **Error Visibility**: Errors are logged both to the log file and as GitHub Actions error annotations

## Troubleshooting

**No log file created?**
- Check that the `bash` tool is enabled in your workflow frontmatter
- Verify the initialization step ran successfully

**Functions not available?**
- Ensure you're sourcing the helpers: `source /tmp/gh-aw/log-helpers.sh`
- Check that the initialization step completed before trying to call functions

**Logs not appearing in analysis?**
- Verify the workflow is running on a `copilot/*` branch (required for session data fetch)
- Check that the workflow run completed (logs are only available after completion)
- Ensure the session log file has content (check `/tmp/gh-aw/session.log`)
