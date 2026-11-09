---
title: Custom Safe Outputs
description: How to create custom safe outputs for third-party integrations using custom jobs and MCP servers.
sidebar:
  order: 5
---

Custom safe outputs extend built-in GitHub operations to third-party services such as Slack, Discord, Notion, databases, or any external API that requires authentication. Use them for write operations that built-in safe outputs do not cover.

> [!NOTE]
> Jira Cloud uses the shared [Jira connector](/gh-aw/reference/connectors/#jira) for issue creation, issue updates, comments, and label additions. Use its scoped effects when sufficient; custom safe outputs require a separately reviewed credential and resource boundary.

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

The agent calls `slack-notify`, and the custom job runs with access to the `SLACK_WEBHOOK` secret.

## Architecture

Agents use read-only Model Context Protocol (MCP) servers with `allowed:` tool lists. Custom jobs perform writes with secret access after the agent completes.

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

### Recording execution outcomes

Optionally map `status` and `url` outputs from the job so Conclusion can record what happened:

```yaml
outputs:
  status: ${{ steps.publish.outputs.status }}
  url: ${{ steps.publish.outputs.url }}
```

The step with `id: publish` sets them with `$GITHUB_OUTPUT` or `core.setOutput`. `status` is `success`, `failure`, `cancelled`, or `skipped`. Use `skipped` for staged previews and intentional no-ops. Set `url` only to the absolute URI the service returns, never one the agent suggested.

Conclusion records a `safe_output.result` event in `usage/aw_session.jsonl`. Without an explicit status, the Actions job result is used. Failed, cancelled, and skipped jobs never report a URL. A successful job without a URL proves execution, not creation of an identifiable resource. Custom scripts instead return `{ success: true, url: canonicalResourceUri }` after the service confirms the operation.

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
              const items = JSON.parse(fs.readFileSync(process.env.GH_AW_AGENT_OUTPUT, 'utf8'))
                .items.filter(item => item.type === 'notion_add_comment');

              for (const item of items) {
                const response = await fetch('https://api.notion.com/v1/comments', {
                  method: 'POST',
                  headers: {
                    'Authorization': `Bearer ${process.env.NOTION_TOKEN}`,
                    'Notion-Version': '2022-06-28',
                    'Content-Type': 'application/json'
                  },
                  body: JSON.stringify({
                    parent: { page_id: item.page_id },
                    rich_text: [{ type: 'text', text: { content: item.comment } }]
                  })
                });
                if (!response.ok) {
                  core.setFailed(`Notion API error (${response.status}): ${await response.text()}`);
                  return;
                }
              }
---
```

Use `container:` for Docker servers or `command:`/`args:` for npx. List only read-only tools in `allowed`. Every job requires `description` and `inputs`; `output` sets the success message.

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

The agent queries with read-only tools, and the safe job runs with write permissions after the agent completes.

## Safe Job Reference

### Attribution and Tracking

Custom jobs must add a generated-by footer linking to the workflow run and preserve workflow provenance annotations on published issues, discussions, and comments. Direct API calls bypass built-in handlers, so without attribution, audits cannot identify the items as workflow-generated. Custom jobs receive the same `GH_AW_WORKFLOW_*`, engine, tracker, message, and detection metadata as built-in jobs. For GitHub issues, use the `createIssue` helper below instead of hand-maintained footer templates.

### Issue Creation API

`actions/setup/js/index.cjs` re-exports `setupGlobals`, `createIssue`, and `logSpan`. Run the setup action to install the runtime helpers, then load them from `${RUNNER_TEMP}/gh-aw/actions/index.cjs` in `actions/github-script`:

```aw wrap
safe-outputs:
  jobs:
    publish-finding:
      description: "Publish one validated finding"
      runs-on: ubuntu-latest
      permissions:
        issues: write
      inputs:
        body:
          description: "Finding details"
          required: true
          type: string
      steps:
        - uses: github/gh-aw/actions/setup@main
        - uses: actions/github-script@v9.0.0
          with:
            script: |
              const fs = require("node:fs");
              const actionsDir = `${process.env.RUNNER_TEMP}/gh-aw/actions`;
              const { setupGlobals, createIssue } = require(`${actionsDir}/index.cjs`);
              setupGlobals(core, github, context, exec, io, getOctokit);
              const { sanitizeContent } = require(`${actionsDir}/sanitize_content.cjs`);
              const output = JSON.parse(fs.readFileSync(process.env.GH_AW_AGENT_OUTPUT, "utf8"));
              const items = output.items.filter(item => item.type === "publish_finding");
              if (items.length !== 1 || typeof items[0].body !== "string") {
                throw new Error("Exactly one finding with a string body is required");
              }
              const result = await createIssue({
                title: "Validated workflow finding",
                body: sanitizeContent(items[0].body),
              });
              if (!result.staged) core.info(result.issue.html_url);
```

Use a release that contains this API, and pin the setup action to a trusted commit in production.

| API | Contract |
|-----|----------|
| `setupGlobals(core, github, context, exec, io, getOctokit)` | Initializes the runtime once before calling `createIssue`. |
| `createIssue(parameters)` | Accepts Octokit issue-creation parameters with a required string `body`. `owner` and `repo` default to the workflow repository. Labels are sanitized and deduplicated. Returns `{ staged: false, issue }`, or `{ staged: true, preview }` when `GH_AW_SAFE_OUTPUTS_STAGED` is `"true"`. Throws on invalid input or API failure. |
| `logSpan(toolName, attributes?, options?)` | Re-exports the [OpenTelemetry span helper](/gh-aw/reference/open-telemetry/#custom-spans-from-shared-imports). Export failures are non-fatal. |

The helper adds the generated-by footer, run and history links, and workflow, caller, engine, and tracker annotations. A body over GitHub's 65,536-character limit, including attribution, fails before creation; content is never silently truncated.

`parameters.artifacts` accepts up to 10 [issue artifacts](/gh-aw/reference/safe-outputs/#issue-artifacts) per issue, each with inline `body` up to 1 MB. The helper does not accept `path`, so the custom job must sanitize artifact names and content. Live results include `artifacts: [{ number, url }]`. If an attachment fails, the error names the already-created issue.

The helper does not perform grouping, deduplication, expiration, agent assignment, or manifest artifact upload. The custom job remains responsible for validating agent-controlled content, enforcing output counts and allowed repositories, and supplying a token with appropriate permissions.

#### Attaching a Text File as an Issue Artifact

For the built-in `create_issue` tool, use [`artifacts[].path`](/gh-aw/reference/safe-outputs/#attaching-files-by-path). Trusted custom jobs using `createIssue` must pass `body` instead. Declare the agent's file so it reaches the custom job:

```yaml
artifacts:
  - /tmp/gh-aw/agent/issue-plan.md
```

The compiler downloads declared files beside the agent output, preserving their paths relative to `/tmp/gh-aw/`. Read the file with the guarded-file helper and pass its sanitized text as `body`:

```javascript
const path = require("node:path");
const { readGuardedFile } = require(`${actionsDir}/symlink_guard.cjs`);
const source = path.join(path.dirname(process.env.GH_AW_AGENT_OUTPUT), "agent", "issue-plan.md");
const body = sanitizeContent(readGuardedFile(source, 1048576).toString("utf8"), {
  maxLength: Number.MAX_SAFE_INTEGER,
  maxLines: Number.MAX_SAFE_INTEGER,
});
const result = await createIssue({
  title: "Implement parser caching",
  body: "The implementation plan is attached.",
  artifacts: [{ type: "plan", name: "Implementation plan", body }],
});
if (!result.staged) core.info(result.issue.html_url);
```

Use a fixed declared path, not an agent-supplied one. Binary files need [path-based file uploads](/gh-aw/reference/safe-outputs/#attaching-files-by-path) instead.

### Job Properties

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `description` | string | Yes | Tool description shown to the agent |
| `runs-on` | string | Yes | GitHub Actions runner (e.g., `ubuntu-latest`) |
| `inputs` | object | Yes | Tool parameters (see [Input Types](#input-types)) |
| `steps` | array | Yes | GitHub Actions steps to execute |
| `output` | string | No | Success message returned to the agent |
| `needs` | string or array | No | Jobs that must complete first (see [Job Ordering](#job-ordering-needs)) |
| `permissions` | object | No | GitHub token permissions for the job |
| `env` | object | No | Environment variables for all steps |
| `if` | string | No | Conditional execution expression |
| `timeout-minutes` | number | No | Maximum job duration (GitHub Actions default: 360) |
| `artifacts` | array of strings | No | Agent-job filesystem paths this job depends on (see [Depending on Agent-Job Files](#depending-on-agent-job-files)) |

### Job Ordering (`needs:`)

Frontmatter `needs:` sequences a custom job relative to other jobs. Unlike lock-file edits, it persists across recompiles:

```yaml wrap
safe-outputs:
  create-issue: {}
  jobs:
    post-process:
      needs: safe_outputs        # runs after the consolidated safe-outputs job
      steps:
        - run: echo "post-processing"
```

The compiler validates each target, normalizes dashes to underscores, and rejects self-dependencies and cycles. Valid targets:

| Target | Available when |
|--------|---------------|
| `agent` | Always |
| `safe_outputs` | At least one builtin handler, script, action, or user step is configured |
| `detection` | Threat detection is enabled |
| `upload_assets` | `upload-asset` is configured |
| `unlock` | `lock-for-agent` is enabled |
| `<job-name>` | That job exists in `safe-outputs.jobs` |

### Input Types

All jobs must define `inputs`:

| Type | Description |
|------|-------------|
| `string` | Text input |
| `boolean` | True/false (as strings: `"true"` or `"false"`) |
| `choice` | Selection from predefined options |

Inputs also support `description`, `required`, and `default`. String inputs can set `max-length` (1–1,000,000 characters) to replace the default 10,240-byte limit. Use it for large structured values that must stay in the safe-output item. For files or directories, use `artifacts:` instead.

```yaml wrap
inputs:
  message:
    description: "Message content"
    required: true
    type: string
    max-length: 40000
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

| Variable | Description |
|----------|-------------|
| `GH_AW_AGENT_OUTPUT` | Path to JSON file containing the agent's output data |
| `GH_AW_SAFE_OUTPUTS_STAGED` | Set to `"true"` when running in staged/preview mode |

### Accessing Agent Output

`GH_AW_AGENT_OUTPUT` points to a JSON file shaped like this:

```json
{
  "items": [
    { "type": "job_name_with_underscores", "field1": "value1" }
  ]
}
```

The `type` is the job name with dashes converted to underscores (`webhook-notify` becomes `webhook_notify`). The `inputs:` schema is both the tool definition the agent sees and the validation for these fields.

> [!IMPORTANT]
> A custom job runs **once per workflow run**, not once per tool call. Iterate over every matching entry in `.items[]`, for example with `jq -c '.items[] | select(.type == "my_job")'`. Reading only `.items[0]` silently drops later calls. Inline [scripts](#inline-script-handlers-safe-outputsscripts) cannot access secrets, so any per-item handler that needs a secret must be a job.

### Depending on Agent-Job Files

The agent job uploads only a fixed set of compiler-managed paths. Files the agent writes elsewhere, such as under `/tmp/gh-aw/agent/`, are silently dropped unless you declare them with `artifacts:`:

```yaml wrap
safe-outputs:
  jobs:
    publish-review-bundle:
      description: "Publish a prepared directory as a workflow artifact"
      runs-on: ubuntu-latest
      artifacts:
        - /tmp/gh-aw/agent/review-bundles/
      inputs:
        source_dir:
          description: "Directory under /tmp/gh-aw/agent/ containing prepared files"
          required: true
          type: string
      steps:
        - run: echo "processing bundle"
```

Entries must be literal paths or globs under `/tmp/gh-aw/`. Expressions and control characters are rejected, and so are `..` segments, hidden files, and paths outside that root. Directory paths end with `/` and expand to recursive globs. Only files with these extensions are persisted: `.txt`, `.json`, `.log`, `.md`, `.mdx`, `.yml`, `.jsonl`, or `.patch`. Symbolic links are removed before upload.

Prefer embedding small values in the safe-output item, where later stages can inspect them. Use `artifacts:` for directories or multiple files.

If secret redaction fails, the source is removed and the step fails. Diagnostics report only a fixed stage (`read`, `redact`, `write`, `replace`, or `unlink`) and an allowlisted error code such as `EACCES` or `ENOSPC`, never messages, paths, or secret values. A failed removal is still fatal, so unsanitized content is never uploaded.

## Inline Script Handlers (`safe-outputs.scripts`)

Use `safe-outputs.scripts` for lightweight inline JavaScript. It runs in-process inside the consolidated safe-outputs job, once per tool call, with no job startup overhead. Scripts cannot access repository secrets.

### Choosing Between Scripts, Actions, and Jobs

| | Scripts | Actions | Jobs |
|---|---|---|---|
| Execution | In-process, in the consolidated safe-outputs job | Step in the consolidated safe-outputs job | Separate GitHub Actions job |
| Invocation | Once per tool call (each call gets its own `item`) | Once per workflow run | Once per workflow run (loops over `.items[]`) |
| Secrets | Not directly available | Full access via `env:` | Full access |
| Reuse | Custom inline JavaScript | Any public GitHub Action | Custom YAML job |
| Typical use | Lightweight processing, logging, or notifications | Reusing marketplace actions | External API calls that need secrets |

### Defining a Script

Under `safe-outputs.scripts`, define each handler with a `description`, `inputs`, and a `script` body:

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

Write only the handler body; the compiler wraps it. The body can use:

| Variable | Description |
|----------|-------------|
| `item` | Runtime message object with field values matching your `inputs` schema |
| `core` | `@actions/core` for logging (`core.info()`, `core.warning()`, `core.error()`) |
| `resolvedTemporaryIds` | Map of temporary object IDs resolved at runtime |

Each declared input is also available as a local variable (`inputs.channel` is `channel` as well as `item.channel`).

> [!NOTE]
> Dashes in script and action names become underscores when registered as tools. `post-slack-message` is called as `post_slack_message`.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `description` | string | Yes | Tool description shown to the agent |
| `inputs` | object | Yes | Tool parameters (same schema as custom jobs) |
| `script` | string | Yes | JavaScript handler body |

Scripts support the `string`, `boolean`, and `number` input types.

## GitHub Action Wrappers (`safe-outputs.actions`)

Use `safe-outputs.actions` to expose a public GitHub Action as a tool the agent can call once per workflow run. At compile time, `gh aw compile` fetches the action's `action.yml` to resolve its inputs and pins the reference to a SHA.

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

The agent calls `add_smoked_label`. The action's declared inputs become the tool's parameters.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `uses` | string | Yes | Action reference (`owner/repo@ref` or `./path/to/local-action`) |
| `description` | string | No | Overrides the action's own description |
| `env` | object | No | Additional environment variables for the action step |

> [!TIP]
> Run `gh aw compile` again after an upstream release to update the pinned SHA.

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

Jobs with duplicate names cause compilation errors. Rename one to resolve the conflict.

## Security

Store secrets in GitHub Secrets and pass them through environment variables. Limit job permissions to the minimum required, and validate all inputs.

## Staged Mode Support

When `GH_AW_SAFE_OUTPUTS_STAGED === 'true'`, skip the real operation and show a preview with `core.summary`. See [Staged Mode](/gh-aw/reference/staged-mode/#staged-mode-for-custom-safe-output-jobs) for an example.

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Job or script not appearing as tool | Ensure `inputs` and `description` are defined; verify import path; run `gh aw compile` |
| Secrets not available | Check the secret exists and the name matches exactly (case-sensitive) |
| Job fails silently | Add `core.info()` logging and call `core.setFailed()` on errors |
| Agent calls wrong tool | Make `description` specific and unique; name the job in the prompt |

## Learn More

- [DeterministicOps](/gh-aw/patterns/deterministic-ops/): Mixing computation and AI reasoning
- [Safe Outputs](/gh-aw/reference/safe-outputs/): Built-in safe output types
- [MCPs](/gh-aw/guides/mcps/): Model Context Protocol setup
- [Frontmatter](/gh-aw/reference/frontmatter/): All configuration options
- [Imports](/gh-aw/reference/imports/): Sharing workflow configurations
