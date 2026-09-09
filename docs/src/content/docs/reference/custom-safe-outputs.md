---
title: Custom Safe Outputs
description: How to create custom safe outputs for third-party integrations using custom jobs and MCP servers.
sidebar:
  order: 5
---

Custom safe outputs extend built-in GitHub operations to integrate with third-party services — Slack, Discord, Notion, databases, or any external API requiring authentication. Use them for write operations that built-in safe outputs do not already cover.

> [!NOTE]
> Jira Cloud now has built-in safe outputs for issue creation, issue updates, comments, and label additions. Use the built-in Jira handlers when those operations are sufficient, and reach for custom safe outputs only when you need unsupported Jira mutations or another external system.

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

A new issue was opened: "${{ steps.sanitized.outputs.text }}"

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

### Step 1: Define the Shared Configuration

In a shared file, define the read-only MCP server and the custom job together:

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

Use `container:` for Docker servers or `command:`/`args:` for npx. List only read-only tools in `allowed`. All jobs require `description` and `inputs`. Use `output` for success messages and `actions/github-script@v8` for API calls with `core.setFailed()` error handling.

### Step 2: Use in Workflow

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

Analyze the issue: "${{ steps.sanitized.outputs.text }}"

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
| `needs` | string or array | No | Jobs that must complete before this job runs (see [Job Ordering](#job-ordering-needs)) |
| `permissions` | object | No | GitHub token permissions for the job |
| `env` | object | No | Environment variables for all steps |
| `if` | string | No | Conditional execution expression |
| `timeout-minutes` | number | No | Maximum job duration (GitHub Actions default: 360) |

### Job Ordering (`needs:`)

Use `needs:` to sequence a custom safe-output job relative to other jobs in the compiled workflow. Unlike manually patching `needs:` in the lock file (which gets overwritten on every recompile), `needs:` declared in the frontmatter persists across recompiles.

```yaml wrap
safe-outputs:
  create-issue: {}
  jobs:
    post-process:
      needs: safe_outputs        # runs after the consolidated safe-outputs job
      steps:
        - run: echo "post-processing"
```

The compiler validates each `needs:` entry at compile time and fails with a clear error if the target does not exist. Target names with dashes are automatically normalized to underscores (e.g., `safe-outputs` → `safe_outputs`).

Valid `needs:` targets for custom safe-jobs:

| Target | Available when |
|--------|---------------|
| `agent` | Always |
| `safe_outputs` | At least one builtin handler, script, action, or user step is configured |
| `detection` | Threat detection is enabled |
| `upload_assets` | `upload-asset` is configured |
| `unlock` | `lock-for-agent` is enabled |
| `<job-name>` | That job exists in `safe-outputs.jobs` |

Self-dependencies and cycles between custom jobs are also caught at compile time.

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

#### Example

```yaml
steps:
  - name: Process output
    run: |
      if [ -f "$GH_AW_AGENT_OUTPUT" ]; then
        MESSAGE=$(cat "$GH_AW_AGENT_OUTPUT" | jq -r '.items[] | select(.type == "my_job") | .message')
        echo "Message: $MESSAGE"
      else
        echo "No agent output found"
        exit 1
      fi
```

The `inputs:` schema serves as both the MCP tool definition visible to the agent and validation for the output fields written to `GH_AW_AGENT_OUTPUT`.

> [!IMPORTANT]
> A custom job runs **once per workflow run**, not once per tool call. If the agent calls the tool multiple times, every call is collected into the `.items[]` array of the single `GH_AW_AGENT_OUTPUT` file. Your job must iterate over `.items[]` (for example with `jq -c '.items[] | select(.type == "my_job")'`) to process all calls. Reading only `.items[0]` silently drops every call after the first — the run still succeeds, so the data loss is easy to miss.
>
> Because inline [scripts](#inline-script-handlers-safe-outputsscripts) cannot access repository secrets, any per-item handler that needs a secret (for example to call an external API with a token) must be a job — and therefore must loop over `.items[]` itself.

## Inline Script Handlers (`safe-outputs.scripts`)

Use `safe-outputs.scripts` to define lightweight inline JavaScript handlers that execute inside the consolidated safe-outputs job handler loop. Unlike `jobs` (which run as a single separate GitHub Actions job per workflow run, processing all of that tool's calls together), scripts run in-process alongside the built-in safe-output handlers — once per tool call, with no extra job allocation or startup overhead.

**When to use scripts vs jobs:**

| | Scripts | Jobs |
|---|---|---|
| Execution | In-process, in the consolidated safe-outputs job | Separate GitHub Actions job |
| Invocation | Once per tool call (each call gets its own `item`) | Once per workflow run (job must loop over `.items[]`) |
| Startup | Fast (no job scheduling) | Slower (one job per run) |
| Secrets | Not directly available — use for lightweight logic | Full access to repository secrets |
| Use case | Lightweight processing, logging, notifications without secrets | External API calls requiring secrets |

### Defining a Script

Under `safe-outputs.scripts`, define each handler with a `description`, `inputs`, and `script` body:

```yaml wrap title=".github/workflows/my-workflow.md"
---
safe-outputs:
  scripts:
    post-slack-message:
      description: Post a message to a Slack channel
      inputs:
        channel:
          description: Slack channel name
          required: true
          type: string
        message:
          description: Message text
          required: true
          type: string
      script: |
        const targetChannel = item.channel || "#general";
        const text = item.message || "(no message)";
        core.info(`Posting to ${targetChannel}: ${text}`);
        return { success: true, channel: targetChannel };
---
```

The agent calls `post_slack_message` (dashes normalized to underscores) and the script runs synchronously in the handler loop.

### Script Body Context

Write only the handler body — the compiler wraps it automatically. Inside the body you have access to:

| Variable | Description |
|----------|-------------|
| `item` | Runtime message object with field values matching your `inputs` schema |
| `core` | `@actions/core` for logging (`core.info()`, `core.warning()`, `core.error()`) |
| `resolvedTemporaryIds` | Map of temporary object IDs resolved at runtime |

Each input declared in `inputs` is also destructured into a local variable. For example, an `inputs.channel` entry is available as `item.channel`.

```javascript
// Example: access inputs via item
const channel = item.channel;
const message = item.message;
core.info(`Sending to ${channel}: ${message}`);
return { sent: true };
```

> [!NOTE]
> Script names with dashes are normalized to underscores when registered as MCP tools (e.g., `post-slack-message` becomes `post_slack_message`). The normalized name is what the agent uses to call the tool.

### Script Reference

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `description` | string | Yes | Tool description shown to the agent |
| `inputs` | object | Yes | Tool parameters (same schema as custom jobs) |
| `script` | string | Yes | JavaScript handler body |

Scripts support the same `inputs` types as custom jobs: `string`, `boolean`, and `number`.

## GitHub Action Wrappers (`safe-outputs.actions`)

Use `safe-outputs.actions` to mount any public GitHub Action as a once-callable MCP tool. At compile time, `gh aw compile` fetches the action's `action.yml` to resolve its inputs and pins the action reference to a specific SHA. The agent can call the tool once per workflow run; the action executes inside the consolidated safe-outputs job.

**When to use actions vs scripts vs jobs:**

| | Actions | Scripts | Jobs |
|---|---|---|---|
| Execution | In the consolidated safe-outputs job, as a step | In-process, in the consolidated safe-outputs job | Separate GitHub Actions job |
| Reuse | Any public GitHub Action | Custom inline JavaScript | Custom inline YAML job |
| Secrets | Full access via `env:` | Not directly available | Full access to repository secrets |
| Use case | Reuse existing marketplace actions | Lightweight logic | Complex multi-step workflows |

### Defining an Action

Under `safe-outputs.actions`, define each action with a `uses` field (matching GitHub Actions `uses` syntax) and an optional `description` override:

```yaml wrap title=".github/workflows/my-workflow.md"
---
safe-outputs:
  actions:
    add-smoked-label:
      uses: actions-ecosystem/action-add-labels@v1
      description: Add the 'smoked' label to the current pull request
      env:
        GITHUB_TOKEN: ${{ github.token }}
---
```

The agent calls `add_smoked_label` (dashes normalized to underscores). The action's declared inputs become the tool's parameters — values are passed as step inputs at runtime.

### Action Reference

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `uses` | string | Yes | Action reference (`owner/repo@ref` or `./path/to/local-action`) |
| `description` | string | No | Tool description shown to the agent (overrides the action's own description) |
| `env` | object | No | Additional environment variables injected into the action step |

> [!NOTE]
> Action names with dashes are normalized to underscores when registered as MCP tools (e.g., `add-smoked-label` becomes `add_smoked_label`). The normalized name is what the agent uses to call the tool.

> [!TIP]
> Action references are pinned to a SHA at compile time for reproducibility. Run `gh aw compile` again to update pinned SHAs after an upstream action release.

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

## Error Handling

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

## Security

Store secrets in GitHub Secrets and pass via environment variables. Limit job permissions to minimum required and validate all inputs.

## Staged Mode Support

When `GH_AW_SAFE_OUTPUTS_STAGED === 'true'`, skip the real operation and display a preview using `core.summary`. See [Staged Mode](/gh-aw/reference/staged-mode/#staged-mode-for-custom-safe-output-jobs) for a complete example.

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Job or script not appearing as tool | Ensure `inputs` and `description` are defined; verify import path; run `gh aw compile` |
| Secrets not available | Check secret exists in repository settings and name matches exactly (case-sensitive) |
| Job fails silently | Add `core.info()` logging and ensure `core.setFailed()` is called on errors |
| Agent calls wrong tool | Make `description` specific and unique; explicitly mention job name in prompt |

## Learn More

- [DeterministicOps](/gh-aw/patterns/deterministic-ops/) - Mixing computation and AI reasoning
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Built-in safe output types
- [MCPs](/gh-aw/guides/mcps/) - Model Context Protocol setup
- [Frontmatter](/gh-aw/reference/frontmatter/) - All configuration options
- [Imports](/gh-aw/reference/imports/) - Sharing workflow configurations
