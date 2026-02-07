---
# This shared component depends on jqschema.md being imported first.
#
# NOTE: Due to BFS import ordering, transitive imports are not guaranteed to have their
# steps executed before the parent import's steps. To ensure correct execution order,
# import jqschema.md directly in your workflow BEFORE importing this file:
#
#   imports:
#     - shared/jqschema.md  # Must come first
#     - shared/copilot-session-data-fetch.md
#
imports:
  - shared/jqschema.md

tools:
  cache-memory:
    key: copilot-session-data
  bash:
    - "gh api *"
    - "gh agent-task *"
    - "jq *"
    - "/tmp/gh-aw/jqschema.sh"
    - "mkdir *"
    - "date *"
    - "cp *"
    - "unzip *"
    - "find *"
    - "rm *"
    - "cat *"

steps:
  - name: Fetch Copilot session data
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      # Create output directories
      mkdir -p /tmp/gh-aw/session-data
      mkdir -p /tmp/gh-aw/session-data/logs
      mkdir -p /tmp/gh-aw/cache-memory
      
      # Get today's date for cache identification
      TODAY=$(date '+%Y-%m-%d')
      CACHE_DIR="/tmp/gh-aw/cache-memory"
      
      # Check if cached data exists from today
      if [ -f "$CACHE_DIR/copilot-sessions-${TODAY}.json" ] && [ -s "$CACHE_DIR/copilot-sessions-${TODAY}.json" ]; then
        echo "✓ Found cached session data from ${TODAY}"
        cp "$CACHE_DIR/copilot-sessions-${TODAY}.json" /tmp/gh-aw/session-data/sessions-list.json
        
        # Regenerate schema if missing
        if [ ! -f "$CACHE_DIR/copilot-sessions-${TODAY}-schema.json" ]; then
          /tmp/gh-aw/jqschema.sh < /tmp/gh-aw/session-data/sessions-list.json > "$CACHE_DIR/copilot-sessions-${TODAY}-schema.json"
        fi
        cp "$CACHE_DIR/copilot-sessions-${TODAY}-schema.json" /tmp/gh-aw/session-data/sessions-schema.json
        
        # Restore cached log files if they exist
        if [ -d "$CACHE_DIR/session-logs-${TODAY}" ]; then
          echo "✓ Found cached session logs from ${TODAY}"
          cp -r "$CACHE_DIR/session-logs-${TODAY}"/* /tmp/gh-aw/session-data/logs/ 2>/dev/null || true
          echo "Restored $(find /tmp/gh-aw/session-data/logs -type f | wc -l) session log files from cache"
        fi
        
        echo "Using cached data from ${TODAY}"
        echo "Total sessions in cache: $(jq 'length' /tmp/gh-aw/session-data/sessions-list.json)"
      else
        echo "⬇ Downloading fresh session data..."
        
        # Calculate date 30 days ago
        DATE_30_DAYS_AGO=$(date -d '30 days ago' '+%Y-%m-%d' 2>/dev/null || date -v-30d '+%Y-%m-%d')

        # Search for workflow runs from copilot/* branches
        # This fetches GitHub Copilot agent task runs by searching for workflow runs on copilot/* branches
        echo "Fetching Copilot agent workflow runs from the last 30 days..."
        
        # Get workflow runs from copilot/* branches
        gh api "repos/${{ github.repository }}/actions/runs" \
          --paginate \
          --jq ".workflow_runs[] | select(.head_branch | startswith(\"copilot/\")) | select(.created_at >= \"${DATE_30_DAYS_AGO}\") | {id, name, head_branch, created_at, updated_at, status, conclusion, html_url}" \
          | jq -s '.[0:50]' \
          > /tmp/gh-aw/session-data/sessions-list.json

        # Generate schema for reference
        /tmp/gh-aw/jqschema.sh < /tmp/gh-aw/session-data/sessions-list.json > /tmp/gh-aw/session-data/sessions-schema.json

        # Download conversation logs using gh agent-task command (limit to first 50)
        SESSION_COUNT=$(jq 'length' /tmp/gh-aw/session-data/sessions-list.json)
        echo "Downloading conversation logs for $SESSION_COUNT sessions..."
        
        # Use gh agent-task to fetch session logs with conversation transcripts
        # Extract session numbers from head_branch (format: copilot/issue-123 or copilot/task-456)
        # The number is the issue/task/PR number that the gh agent-task command uses
        jq -r '.[].head_branch' /tmp/gh-aw/session-data/sessions-list.json | while read -r branch; do
          if [ -n "$branch" ]; then
            # Extract number from branch name (e.g., copilot/issue-123 -> 123)
            # This is the session identifier used by gh agent-task
            session_number=$(echo "$branch" | sed 's/copilot\///' | sed 's/[^0-9]//g')
            
            if [ -n "$session_number" ]; then
              echo "Downloading conversation log for session #$session_number (branch: $branch)"
              
              # Use gh agent-task view --log to get conversation transcript
              # This contains the agent's internal monologue, tool calls, and reasoning
              gh agent-task view --repo "${{ github.repository }}" "$session_number" --log \
                > "/tmp/gh-aw/session-data/logs/${session_number}-conversation.txt" 2>&1 || {
                echo "Warning: Could not fetch conversation log for session #$session_number"
                # If gh agent-task fails, fall back to downloading GitHub Actions logs
                # This ensures we have some data even if agent-task command is unavailable
                run_id=$(jq -r ".[] | select(.head_branch == \"$branch\") | .id" /tmp/gh-aw/session-data/sessions-list.json)
                if [ -n "$run_id" ]; then
                  echo "Falling back to GitHub Actions logs for run ID: $run_id"
                  gh api "repos/${{ github.repository }}/actions/runs/${run_id}/logs" \
                    > "/tmp/gh-aw/session-data/logs/${session_number}-actions.zip" 2>&1 || true
                  
                  if [ -f "/tmp/gh-aw/session-data/logs/${session_number}-actions.zip" ] && [ -s "/tmp/gh-aw/session-data/logs/${session_number}-actions.zip" ]; then
                    unzip -q "/tmp/gh-aw/session-data/logs/${session_number}-actions.zip" -d "/tmp/gh-aw/session-data/logs/${session_number}/" 2>/dev/null || true
                    rm "/tmp/gh-aw/session-data/logs/${session_number}-actions.zip"
                  fi
                fi
              }
            fi
          fi
        done
        
        LOG_COUNT=$(find /tmp/gh-aw/session-data/logs/ -type f -name "*-conversation.txt" | wc -l)
        echo "Conversation logs downloaded: $LOG_COUNT session logs"
        
        FALLBACK_COUNT=$(find /tmp/gh-aw/session-data/logs/ -type d -mindepth 1 | wc -l)
        if [ "$FALLBACK_COUNT" -gt 0 ]; then
          echo "Fallback GitHub Actions logs: $FALLBACK_COUNT sessions"
        fi

        # Store in cache with today's date
        cp /tmp/gh-aw/session-data/sessions-list.json "$CACHE_DIR/copilot-sessions-${TODAY}.json"
        cp /tmp/gh-aw/session-data/sessions-schema.json "$CACHE_DIR/copilot-sessions-${TODAY}-schema.json"
        
        # Cache the log files
        mkdir -p "$CACHE_DIR/session-logs-${TODAY}"
        cp -r /tmp/gh-aw/session-data/logs/* "$CACHE_DIR/session-logs-${TODAY}/" 2>/dev/null || true

        echo "✓ Session data saved to cache: copilot-sessions-${TODAY}.json"
        echo "Total sessions found: $(jq 'length' /tmp/gh-aw/session-data/sessions-list.json)"
      fi
      
      # Always ensure data is available at expected locations for backward compatibility
      echo "Session data available at: /tmp/gh-aw/session-data/sessions-list.json"
      echo "Schema available at: /tmp/gh-aw/session-data/sessions-schema.json"
      echo "Logs available at: /tmp/gh-aw/session-data/logs/"
      
      # Set outputs for downstream use
      echo "sessions_count=$(jq 'length' /tmp/gh-aw/session-data/sessions-list.json)" >> "$GITHUB_OUTPUT"
---

<!--
## Copilot Session Data Fetch

This shared component fetches GitHub Copilot agent session data by analyzing workflow runs from `copilot/*` branches, with intelligent caching to avoid redundant API calls.

### What It Does

1. Creates output directories at `/tmp/gh-aw/session-data/` and `/tmp/gh-aw/cache-memory/`
2. Checks for cached session data from today's date in cache-memory
3. If cache exists (from earlier workflow runs today):
   - Uses cached data instead of making API calls
   - Copies data from cache to working directory
   - Restores cached log files if available
4. If cache doesn't exist:
   - Calculates the date 30 days ago (cross-platform compatible)
   - Fetches all workflow runs from branches starting with `copilot/` using GitHub API
   - **Downloads conversation logs** using `gh agent-task view --log` for up to 50 most recent sessions
   - Falls back to GitHub Actions logs if agent-task command fails
   - Saves data to cache with date-based filename (e.g., `copilot-sessions-2024-11-22.json`)
   - Copies data to working directory for use
5. Generates a schema of the data structure

### What's New: Conversation Transcript Access

**This module now fetches actual agent conversation logs** instead of just infrastructure logs:
- Uses `gh agent-task view --log` to access agent session logs
- Logs include agent's internal monologue, reasoning, and tool usage
- Enables true behavioral pattern analysis and prompt quality assessment
- Falls back to GitHub Actions logs if agent-task command is unavailable

### Caching Strategy

- **Cache Key**: `copilot-session-data` for workflow-level sharing
- **Cache Files**: Stored with today's date in the filename (e.g., `copilot-sessions-2024-11-22.json`)
- **Cache Location**: `/tmp/gh-aw/cache-memory/`
- **Cache Benefits**: 
  - Multiple workflows running on the same day share the same session data
  - Reduces GitHub API rate limit usage
  - Faster workflow execution after first fetch of the day
  - Includes conversation transcript cache

### Output Files

- **`/tmp/gh-aw/session-data/sessions-list.json`**: Full session data including run ID, name, branch, timestamps, status, conclusion, and URL
- **`/tmp/gh-aw/session-data/sessions-schema.json`**: JSON schema showing the structure of the session data
- **`/tmp/gh-aw/session-data/logs/`**: Directory containing session conversation logs
  - **`{session_number}-conversation.txt`**: Agent conversation transcript with internal monologue and tool usage (primary)
  - **`{session_number}/`**: GitHub Actions infrastructure logs (fallback only)
- **`/tmp/gh-aw/cache-memory/copilot-sessions-YYYY-MM-DD.json`**: Cached session data with date
- **`/tmp/gh-aw/cache-memory/copilot-sessions-YYYY-MM-DD-schema.json`**: Cached schema with date
- **`/tmp/gh-aw/cache-memory/session-logs-YYYY-MM-DD/`**: Cached log files with date

### Usage

Import this component in your workflow:

```yaml
imports:
  - shared/copilot-session-data-fetch.md
```

**Note**: This component automatically imports `jqschema.md` as a dependency. The compiler handles the transitive closure of imports, ensuring all required utilities are set up in the correct order.

Then access the pre-fetched data in your workflow prompt:

```bash
# Get sessions from the last 24 hours
TODAY="$(date -d '24 hours ago' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -v-24H '+%Y-%m-%dT%H:%M:%SZ')"
jq --arg today "$TODAY" '[.[] | select(.created_at >= $today)]' /tmp/gh-aw/session-data/sessions-list.json

# Count total sessions
jq 'length' /tmp/gh-aw/session-data/sessions-list.json

# Get session numbers for conversation logs
jq -r '.[].head_branch' /tmp/gh-aw/session-data/sessions-list.json | sed 's/copilot\///' | sed 's/[^0-9]//g'

# List conversation log files
find /tmp/gh-aw/session-data/logs -type f -name "*-conversation.txt"

# Read a specific conversation log (session number 123)
cat /tmp/gh-aw/session-data/logs/123-conversation.txt
```

### Requirements

- Automatically imports `jqschema.md` for schema generation (via transitive import closure)
- Uses GitHub Actions API to fetch workflow runs from `copilot/*` branches
- **Uses `gh agent-task view --log` to fetch conversation transcripts** (requires gh CLI v2.80.0+)
- Cross-platform date calculation (works on both GNU and BSD date commands)
- Cache-memory tool is automatically configured for data persistence
- Falls back to GitHub Actions infrastructure logs if `gh agent-task` is unavailable

### Why Branch-Based Search?

GitHub Copilot creates branches with the `copilot/` prefix, making branch-based workflow run search a reliable way to identify Copilot agent sessions.

### Conversation Log Access

This module now provides access to **actual agent conversation transcripts** via the `gh agent-task view --log` command:

**What's in the conversation logs:**
- Agent's internal monologue and reasoning
- Tool calls and their results
- Step-by-step problem-solving approach
- Code changes and validations
- Error handling and recovery attempts

**Benefits for analysis:**
- True behavioral pattern analysis (not just infrastructure metrics)
- Prompt quality assessment based on actual responses
- Success factor identification from agent reasoning
- Failure signal detection from error patterns
- Tool usage effectiveness analysis
- **Broader Access**: Works in all GitHub environments, not just Enterprise with Copilot

### Cache Behavior

The cache is date-based, meaning:
- All workflows running on the same day share cached data
- Cache refreshes automatically the next day
- First workflow of the day fetches fresh data and populates the cache
- Subsequent workflows use the cached data for faster execution
-->
