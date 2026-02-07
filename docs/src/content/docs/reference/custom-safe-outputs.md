---
title: Custom Safe Outputs
description: How to create custom safe outputs for third-party integrations using custom jobs and MCP servers.
sidebar:
  order: 5
---

Custom safe outputs extend GitHub Agentic Workflows beyond built-in GitHub operations. While built-in safe outputs handle GitHub issues, PRs, and discussions, custom safe outputs let you integrate with third-party services like Notion, Slack, databases, or any external API.

## When to Use Custom Safe Outputs

Use custom safe outputs when you need to:

- Send data to external services (Slack, Discord, Notion, Jira)
- Trigger deployments or CI/CD pipelines
- Update databases or external storage
- Call custom APIs that require authentication
- Perform any write operation that built-in safe outputs don't cover

## Quick Start

Here's a minimal custom safe output that sends a Slack message:

```yaml wrap title=".github/workflows/shared/slack-notify.md"
---
safe-outputs:
  jobs:
    slack-notify:
      description: "Send a message to Slack"
      runs-on: ubuntu-latest
      output: "Message sent to Slack!"
      inputs:
        message:
          description: "The message to send"
          required: true
          type: string
      steps:
        - name: Send Slack message
          env:
            SLACK_WEBHOOK: "${{ secrets.SLACK_WEBHOOK }}"
          run: |
            if [ -f "$GH_AW_AGENT_OUTPUT" ]; then
              MESSAGE=$(cat "$GH_AW_AGENT_OUTPUT" | jq -r '.items[] | select(.type == "slack_notify") | .message')
              # Use jq to safely escape JSON content
              PAYLOAD=$(jq -n --arg text "$MESSAGE" '{text: $text}')
              curl -X POST "$SLACK_WEBHOOK" \
                -H 'Content-Type: application/json' \
                -d "$PAYLOAD"
            else
              echo "No agent output found"
              exit 1
            fi
---
```

Use it in a workflow:

```aw wrap title=".github/workflows/issue-notifier.md"
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
imports:
  - shared/slack-notify.md
---

# Issue Notifier

A new issue was opened: "${{ needs.activation.outputs.text }}"

Summarize the issue and use the slack-notify tool to send a notification.
```

The agent can now call `slack-notify` with a message, and the custom job executes with access to the `SLACK_WEBHOOK` secret.

## Architecture

Custom safe outputs separate read and write operations: agents use read-only Model Context Protocol (MCP) servers with `allowed:` tool lists, while custom jobs handle write operations with secret access after agent completion.

```text
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Agent (AI)    │────▶│  MCP Server     │────▶│  External API   │
│                 │     │  (read-only)    │     │  (GET requests) │
└─────────────────┘     └─────────────────┘     └─────────────────┘
        │
        │ calls safe-job tool
        ▼
┌─────────────────┐     ┌─────────────────┐
│  Custom Job     │────▶│  External API   │
│  (with secrets) │     │  (POST/PUT)     │
└─────────────────┘     └─────────────────┘
```

## Creating a Custom Safe Output

### Step 1: Define the MCP Server (Read-Only)

Create a shared configuration with read-only MCP tools:

```yaml wrap
---
mcp-servers:
  notion:
    container: "mcp/notion"
    env:
      NOTION_TOKEN: "${{ secrets.NOTION_TOKEN }}"
    allowed:
      - "search_pages"
      - "get_page"
      - "get_database"
      - "query_database"
---
```

Use `container:` for Docker servers or `command:`/`args:` for npx. List only read-only tools in `allowed`.

### Step 2: Define the Custom Job (Write Operations)

Add a custom job under `safe-outputs.jobs` for write operations:

```yaml wrap
---
mcp-servers:
  notion:
    container: "mcp/notion"
    env:
      NOTION_TOKEN: "${{ secrets.NOTION_TOKEN }}"
    allowed:
      - "search_pages"
      - "get_page"
      - "get_database"
      - "query_database"

safe-outputs:
  jobs:
    notion-add-comment:
      description: "Add a comment to a Notion page"
      runs-on: ubuntu-latest
      output: "Comment added to Notion successfully!"
      permissions:
        contents: read
      inputs:
        page_id:
          description: "The Notion page ID to add a comment to"
          required: true
          type: string
        comment:
          description: "The comment text to add"
          required: true
          type: string
      steps:
        - name: Add comment to Notion page
          uses: actions/github-script@v8
          env:
            NOTION_TOKEN: "${{ secrets.NOTION_TOKEN }}"
          with:
            script: |
              const fs = require('fs');
              const notionToken = process.env.NOTION_TOKEN;
              const outputFile = process.env.GH_AW_AGENT_OUTPUT;
              
              if (!notionToken) {
                core.setFailed('NOTION_TOKEN secret is not configured');
                return;
              }
              
              if (!outputFile) {
                core.info('No GH_AW_AGENT_OUTPUT environment variable found');
                return;
              }
              
              // Read and parse agent output
              const fileContent = fs.readFileSync(outputFile, 'utf8');
              const agentOutput = JSON.parse(fileContent);
              
              // Filter for notion-add-comment items (job name with dashes → underscores)
              const items = agentOutput.items.filter(item => item.type === 'notion_add_comment');
              
              for (const item of items) {
                const pageId = item.page_id;
                const comment = item.comment;
                
                core.info(`Adding comment to Notion page: ${pageId}`);
                
                try {
                  const response = await fetch('https://api.notion.com/v1/comments', {
                    method: 'POST',
                    headers: {
                      'Authorization': `Bearer ${notionToken}`,
                      'Notion-Version': '2022-06-28',
                      'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({
                      parent: { page_id: pageId },
                      rich_text: [{
                        type: 'text',
                        text: { content: comment }
                      }]
                    })
                  });
                  
                  if (!response.ok) {
                    const errorData = await response.text();
                    core.setFailed(`Notion API error (${response.status}): ${errorData}`);
                    return;
                  }
                  
                  const data = await response.json();
                  core.info('Comment added successfully');
                  core.info(`Comment ID: ${data.id}`);
                } catch (error) {
                  core.setFailed(`Failed to add comment: ${error.message}`);
                  return;
                }
              }
---
```

All jobs require `description` and `inputs`. Use `output` for success messages and `actions/github-script@v8` for API calls with `core.setFailed()` error handling.

### Step 3: Use in Workflow

Import the configuration:

```aw wrap
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  actions: read

imports:
  - shared/mcp/notion.md
---

# Issue Summary to Notion

Analyze the issue: "${{ needs.activation.outputs.text }}"

Search for the GitHub Issues page in Notion using the read-only Notion tools, then add a summary comment using the notion-add-comment safe-job.
```

The agent uses read-only tools to query, then calls the safe-job which executes with write permissions after completion.

## Safe Job Reference

### Job Properties

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `description` | string | Yes | Tool description shown to the agent |
| `runs-on` | string | Yes | GitHub Actions runner (e.g., `ubuntu-latest`) |
| `inputs` | object | Yes | Tool parameters (see [Input Types](#input-types)) |
| `steps` | array | Yes | GitHub Actions steps to execute |
| `output` | string | No | Success message returned to the agent |
| `permissions` | object | No | GitHub token permissions for the job |
| `env` | object | No | Environment variables for all steps |
| `if` | string | No | Conditional execution expression |
| `timeout-minutes` | number | No | Maximum job duration (default: 360) |

### Input Types

All jobs must define `inputs`:

| Type | Description |
|------|-------------|
| `string` | Text input |
| `boolean` | True/false (as strings: `"true"` or `"false"`) |
| `choice` | Selection from predefined options |

```yaml wrap
inputs:
  message:
    description: "Message content"
    required: true
    type: string
  notify:
    description: "Send notification"
    required: false
    type: boolean
    default: "true"
  environment:
    description: "Target environment"
    required: true
    type: choice
    options: ["staging", "production"]
```

### Environment Variables

Custom safe-output jobs have access to these environment variables:

| Variable | Description |
|----------|-------------|
| `GH_AW_AGENT_OUTPUT` | Path to JSON file containing the agent's output data |
| `GH_AW_SAFE_OUTPUTS_STAGED` | Set to `"true"` when running in staged/preview mode |

### Accessing Agent Output

Custom safe-output jobs receive the agent's data through the `GH_AW_AGENT_OUTPUT` environment variable, which contains a path to a JSON file. This file has the structure:

```json
{
  "items": [
    {
      "type": "job_name_with_underscores",
      "field1": "value1",
      "field2": "value2"
    }
  ]
}
```

The `type` field matches your job name with dashes converted to underscores (e.g., job `webhook-notify` → type `webhook_notify`).

#### Bash Example

```yaml
steps:
  - name: Process output
    run: |
      if [ -f "$GH_AW_AGENT_OUTPUT" ]; then
        # Extract specific field from matching items
        MESSAGE=$(cat "$GH_AW_AGENT_OUTPUT" | jq -r '.items[] | select(.type == "my_job") | .message')
        echo "Message: $MESSAGE"
      else
        echo "No agent output found"
        exit 1
      fi
```

#### JavaScript Example

```yaml
steps:
  - name: Process output
    uses: actions/github-script@v8
    with:
      script: |
        const fs = require('fs');
        const outputFile = process.env.GH_AW_AGENT_OUTPUT;
        
        if (!outputFile) {
          core.info('No GH_AW_AGENT_OUTPUT environment variable found');
          return;
        }
        
        // Read and parse the JSON file
        const fileContent = fs.readFileSync(outputFile, 'utf8');
        const agentOutput = JSON.parse(fileContent);
        
        // Filter for items matching this job (job-name → job_name)
        const items = agentOutput.items.filter(item => item.type === 'my_job');
        
        // Process each item
        for (const item of items) {
          const message = item.message;
          core.info(`Processing: ${message}`);
          // Your logic here
        }
```

### Understanding `inputs:`

The `inputs:` field in your job definition serves **two purposes**:

1. **Tool Discovery**: Defines the MCP tool schema that the AI agent sees
2. **Validation**: Describes what fields the agent should provide in its output

The agent uses the `inputs:` schema to understand what parameters to include when calling your custom job. The actual values are written to the `GH_AW_AGENT_OUTPUT` JSON file, which your job must read and parse.

## Importing Custom Jobs

Define jobs in shared files under `.github/workflows/shared/` and import them:

```aw wrap
---
on: issues
permissions:
  contents: read
imports:
  - shared/slack-notify.md
  - shared/jira-integration.md
---

# Issue Handler

Handle the issue and notify via Slack and Jira.
```

Jobs with duplicate names cause compilation errors - rename to resolve conflicts.

## Best Practices

### Error Handling

Use `core.setFailed()` for errors and validate required inputs:

```javascript
if (!process.env.API_KEY) {
  core.setFailed('API_KEY secret is not configured');
  return;
}

try {
  const response = await fetch(url);
  if (!response.ok) {
    core.setFailed(`API error (${response.status}): ${await response.text()}`);
    return;
  }
  core.info('Operation completed successfully');
} catch (error) {
  core.setFailed(`Request failed: ${error.message}`);
}
```

### Security

Store secrets in GitHub Secrets and pass via environment variables. Limit job permissions to minimum required and validate all inputs.

### Staged Mode Support

Check `GH_AW_SAFE_OUTPUTS_STAGED` to preview operations without executing:

```javascript
if (process.env.GH_AW_SAFE_OUTPUTS_STAGED === 'true') {
  core.info('🎭 Staged mode: would send notification');
  await core.summary.addRaw('## Preview\nWould send: ' + process.env.MESSAGE).write();
  return;
}
// Actually send the notification
```

## Common Mistakes

### ❌ Using `${{ inputs.* }}` Syntax

**This does NOT work:**

```yaml
steps:
  - name: Wrong approach
    env:
      MESSAGE: "${{ inputs.message }}"  # ❌ This will be empty!
    run: |
      echo "$MESSAGE"  # Empty - inputs.* is not available
```

**✅ Correct approach:**

```yaml
steps:
  - name: Correct approach
    run: |
      if [ -f "$GH_AW_AGENT_OUTPUT" ]; then
        MESSAGE=$(cat "$GH_AW_AGENT_OUTPUT" | jq -r '.items[] | select(.type == "my_job") | .message')
        echo "$MESSAGE"
      fi
```

### ❌ Wrong Job Type Name

**This does NOT work:**

```yaml
safe-outputs:
  jobs:
    notify-user:  # Job name with dashes
      # ...
      steps:
        - run: |
            # Wrong: using dashes in type filter
            jq '.items[] | select(.type == "notify-user")'  # ❌ Won't match!
```

**Why it fails**: Job names with dashes are converted to underscores in the `type` field.

**✅ Correct approach:**

```yaml
safe-outputs:
  jobs:
    notify-user:  # Job name with dashes
      # ...
      steps:
        - run: |
            # Correct: use underscores in type filter
            jq '.items[] | select(.type == "notify_user")'  # ✅ Matches!
```

### ❌ Not Checking for Agent Output

**This does NOT work:**

```yaml
steps:
  - run: |
      # ❌ Assumes file exists without checking
      MESSAGE=$(cat "$GH_AW_AGENT_OUTPUT" | jq -r '.items[0].message')
```

**Why it fails**: If the agent doesn't call your tool, the file might not exist or be empty, causing errors.

**✅ Correct approach:**

```yaml
steps:
  - run: |
      if [ -f "$GH_AW_AGENT_OUTPUT" ]; then
        ITEMS=$(cat "$GH_AW_AGENT_OUTPUT" | jq '.items[] | select(.type == "my_job")')
        if [ -z "$ITEMS" ]; then
          echo "No items found for this job"
          exit 0
        fi
        # Process items...
      else
        echo "No agent output file found"
        exit 1
      fi
```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Job not appearing as tool | Ensure `inputs` and `description` are defined; verify import path; run `gh aw compile` |
| Secrets not available | Check secret exists in repository settings and name matches exactly (case-sensitive) |
| Job fails silently | Add `core.info()` logging and ensure `core.setFailed()` is called on errors |
| Agent calls wrong tool | Make `description` specific and unique; explicitly mention job name in prompt |

## Related Documentation

- [Deterministic & Agentic Patterns](/gh-aw/guides/deterministic-agentic-patterns/) - Mixing computation and AI reasoning
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Built-in safe output types
- [MCPs](/gh-aw/guides/mcps/) - Model Context Protocol setup
- [Frontmatter](/gh-aw/reference/frontmatter/) - All configuration options
- [Imports](/gh-aw/reference/imports/) - Sharing workflow configurations
