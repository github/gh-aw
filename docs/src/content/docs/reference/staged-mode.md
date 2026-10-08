---
title: Staged Mode
description: Preview built-in safe output operations without applying their writes; custom jobs and other workflow effects require separate controls.
sidebar:
  order: 820
---

Staged mode lets you run a workflow and preview the built-in [safe outputs](/gh-aw/reference/safe-outputs/) it would create — issues, comments, pull requests, and more — without applying those writes. Staged built-in operations are replaced with a detailed 🎭 preview in the GitHub Actions step summary.

Use it to review proposed safe outputs before enabling their production writes.
Staging is not a sandbox: custom jobs, scripts, external MCP servers, custom
credentials, memory/cache persistence, and diagnostic exports require separate
controls. [Compile dry-run](/gh-aw/reference/compilation-process/#development-testing-mode)
disables additional compiler-managed mutations but does not sandbox custom code.

## Enabling Staged Mode

Add `staged: true` to the `safe-outputs:` block in your workflow frontmatter:

```aw wrap
---
on: issues

safe-outputs:
  staged: true
  create-issue:
    title-prefix: "[ai] "
    labels: [automation]
---

# Issue Analyzer

Analyze the issue and suggest follow-up tasks.
```

With this configuration the workflow runs fully — the AI completes its analysis — but no issues are created. Instead, the Actions run summary shows a preview of what would have been created.

## Scoping Staged Mode per Output Type

Use `staged: true` on a specific type to preview only that output type while letting others execute normally:

```aw wrap
---
safe-outputs:
  staged: false         # default: execute normally
  create-pull-request:
    staged: true        # PRs: preview only
  add-comment:          # comments: execute normally
---
```

A built-in output is staged when either the global setting or its type-level
setting is true. With global staging false or unset, type-level staging lets you
preview one output type while keeping others active. Type-level `staged: false`
cannot disable global staging, including staging forced by compile `--staged`
or `--dry-run`.

## What the Preview Looks Like

When staged mode is active, the step summary contains a structured preview for each output type. The 🎭 emoji marks every heading so previews are easy to spot:

```markdown
## 🎭 Staged Mode: Issue Creation Preview

The following 2 issue(s) would be performed if staged mode was disabled:

### Operation 1: Add caching layer to database queries

**Type**: create-issue
**Title**: Add caching layer to database queries
**Body**:
Performance profiling shows repeated queries to the users table …

**Additional Fields**:
- Labels: performance, database
- Assignees: octocat

### Operation 2: Update connection pool settings

…

---
**Preview Summary**: 2 operations previewed. No GitHub resources were created.
```

The preview includes every populated field — title, body, labels, assignees, and similar metadata — so you can review the full output before enabling it.

## Supported Output Types

Staged mode is supported by all built-in safe output types:

| Output type | What the preview shows |
|---|---|
| [`create-issue`](/gh-aw/reference/safe-outputs/#issue-creation-create-issue) | Title, body, labels, assignees |
| [`update-issue`](/gh-aw/reference/safe-outputs/#issue-updates-update-issue) | Target issue, updated fields |
| [`close-issue`](/gh-aw/reference/safe-outputs/#close-issue-close-issue) | Target issue, closing comment |
| [`add-comment`](/gh-aw/reference/safe-outputs/#comment-creation-add-comment) | Target issue/PR/discussion, comment body |
| [`add-labels`](/gh-aw/reference/safe-outputs/#add-labels-add-labels) | Target item, labels to add |
| [`remove-labels`](/gh-aw/reference/safe-outputs/#remove-labels-remove-labels) | Target item, labels to remove |
| [`create-discussion`](/gh-aw/reference/safe-outputs/#discussion-creation-create-discussion) | Title, body, category |
| [`update-discussion`](/gh-aw/reference/safe-outputs/#discussion-updates-update-discussion) | Target discussion, updated fields |
| [`close-discussion`](/gh-aw/reference/safe-outputs/#close-discussion-close-discussion) | Target discussion, closing comment |
| [`create-pull-request`](/gh-aw/reference/safe-outputs-pull-requests/#pull-request-creation-create-pull-request) | Title, body, branch, diff |
| [`update-pull-request`](/gh-aw/reference/safe-outputs/#pull-request-updates-update-pull-request) | Target PR, updated fields |
| [`close-pull-request`](/gh-aw/reference/safe-outputs/#close-pull-request-close-pull-request) | Target PR |
| [`create-pull-request-review-comment`](/gh-aw/reference/safe-outputs/#pr-review-comments-create-pull-request-review-comment) | File, line, comment body |
| [`push-to-pull-request-branch`](/gh-aw/reference/safe-outputs-pull-requests/#push-to-pr-branch-push-to-pull-request-branch) | Branch, patch summary |
| [`create-project`](/gh-aw/reference/safe-outputs/#project-creation-create-project) | Project title, description |
| [`update-project`](/gh-aw/reference/safe-outputs/#project-board-updates-update-project) | Target project, project items and fields to update |
| [`create-project-status-update`](/gh-aw/reference/safe-outputs/#project-status-updates-create-project-status-update) | Status, body |
| [`update-release`](/gh-aw/reference/safe-outputs/#release-updates-update-release) | Target release, updated body |
| [`upload-asset`](/gh-aw/reference/safe-outputs/#asset-uploads-upload-asset) | File names and sizes |
| [`dispatch-workflow`](/gh-aw/reference/safe-outputs/#workflow-dispatch-dispatch-workflow) | Target workflow, inputs |
| [`assign-to-agent`](/gh-aw/reference/safe-outputs/#assign-to-agent-assign-to-agent) | Target issue/PR |
| [`assign-to-user`](/gh-aw/reference/safe-outputs/#assign-to-user-assign-to-user) | Target item, user |
| [`create-agent-session`](/gh-aw/reference/copilot-cloud-agent/#create-agent-session) | Session details |

[Custom safe output jobs](/gh-aw/reference/custom-safe-outputs/) receive the `GH_AW_SAFE_OUTPUTS_STAGED` environment variable set to `"true"` when staged mode is active, allowing you to implement your own preview behavior.
They are not automatically skipped: their code must honor the staging signal,
and their credentials and external effects still require review.

## Staged Mode for Custom Safe Output Jobs

Custom jobs check `GH_AW_SAFE_OUTPUTS_STAGED` to skip the real operation and display a preview instead:

```javascript
if (process.env.GH_AW_SAFE_OUTPUTS_STAGED === 'true') {
  core.info('🎭 Staged mode: would send Slack notification');
  await core.summary
    .addHeading('🎭 Staged Mode: Slack Notification Preview', 2)
    .addRaw(`**Would send**: ${process.env.MESSAGE}`)
    .write();
  return;
}

// Production path — actually send the notification
await sendSlackMessage(process.env.MESSAGE);
```

See [Custom Safe Outputs — Staged Mode Support](/gh-aw/reference/custom-safe-outputs/#staged-mode-support) for a complete example.

## Customizing Preview Messages

Override the default preview heading and description using the `messages:` block:

```aw wrap
---
safe-outputs:
  staged: true
  messages:
    staged-title: "🎭 Preview: {operation}"
    staged-description: "The following {operation} would occur if staged mode was disabled:"
  create-issue:
---
```

The `{operation}` placeholder is replaced with the safe output operation name (for example, `issue creation`).

## Recommended Workflow

A common adoption pattern is:

1. Enable `staged: true` and trigger the workflow on a real event.
2. Review the 🎭 preview in the Actions run summary.
3. Adjust the prompt or configuration.
4. Repeat until the output looks correct.
5. Disable staged mode to start creating real GitHub resources.

> [!TIP]
> Keep staged mode enabled while iterating on prompt changes, and re-enable it for a single output type when introducing a new safe output.

## Learn More

See [Safe Outputs](/gh-aw/reference/safe-outputs/) for built-in output types, [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/) for custom jobs with staged mode support, [Frontmatter (Full)](/gh-aw/reference/frontmatter-full/) for the full configuration reference, and [Threat Detection](/gh-aw/reference/threat-detection/) for security scanning of safe output content.
