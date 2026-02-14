---
title: Footer Control
description: Learn how to control AI-generated footers in safe output operations and customize footer messages for GitHub issues, pull requests, discussions, and releases.
sidebar:
  order: 805
---

Control whether AI-generated footers are added to created and updated GitHub items (issues, pull requests, discussions, releases). Footers provide attribution and links to workflow runs, but you may want to omit them for cleaner content or when using custom branding.

## Global Footer Control

Set `footer: false` at the safe-outputs level to hide footers for all output types:

```yaml wrap
safe-outputs:
  footer: false                      # hide footers globally
  create-issue:
    title-prefix: "[ai] "
  create-pull-request:
    title-prefix: "[ai] "
```

When `footer: false` is set:
- **Visible footer content is omitted** - No AI-generated attribution text appears in the item body
- **XML markers are preserved** - Hidden workflow-id and tracker-id markers remain for searchability
- **All safe output types affected** - Applies to create-issue, create-pull-request, create-discussion, update-issue, update-discussion, and update-release

## Per-Handler Footer Control

Override the global setting for specific output types by setting `footer` at the handler level:

```yaml wrap
safe-outputs:
  footer: false                      # global default: no footers
  create-issue:
    title-prefix: "[issue] "
    # inherits footer: false
  create-pull-request:
    title-prefix: "[pr] "
    footer: true                     # override: show footer for PRs only
```

Individual handler settings always take precedence over the global setting.

## PR Review Comment Footer Control

For PR review comments (`create-pull-request-review-comment`), the `footer` field supports additional conditional control:

```yaml wrap
safe-outputs:
  create-pull-request-review-comment:
    footer: "if-body"         # conditional footer based on review body
  submit-pull-request-review:
```

The `footer` field accepts three string values:

- `"always"` (default) - Always include footer on review comments
- `"none"` - Never include footer on review comments
- `"if-body"` - Only include footer when the review has body text

Boolean values are also supported and automatically converted:
- `true` → `"always"`
- `false` → `"none"`

This is particularly useful for clean approval reviews without body text. With `footer: "if-body"`, approval reviews appear clean without the AI-generated footer, while reviews with explanatory text still include the footer for attribution.

**Example use case - Clean approvals:**

```yaml wrap
safe-outputs:
  create-pull-request-review-comment:
    footer: "if-body"         # Show footer only when review has body
  submit-pull-request-review:
    max: 1
```

When the agent submits an approval without a body (just "APPROVE" event), no footer appears. When the agent includes explanatory comments in the review body, the footer is included.

## What's Preserved When Footer is Hidden

Even with `footer: false`, the following are still included:

1. **Workflow-id marker** - Hidden HTML comment for search and tracking:
   ```html
   <!-- gh-aw-workflow-id: WORKFLOW_NAME -->
   ```

2. **Tracker-id marker** - For issue/discussion tracking (when applicable):
   ```html
   <!-- gh-aw-tracker-id: unique-id -->
   ```

These markers enable you to search for workflow-created items using GitHub's search, even when footers are hidden.

### Searching for Workflow-Created Items

You can use the workflow-id marker to find all items created by a specific workflow on GitHub.com. The marker is always included in the body of issues, pull requests, discussions, and comments, regardless of the `footer` setting.

**Search Examples:**

Find all open issues created by the `daily-team-status` workflow:
```
repo:owner/repo is:issue is:open "gh-aw-workflow-id: daily-team-status" in:body
```

Find all pull requests created by the `security-audit` workflow:
```
repo:owner/repo is:pr "gh-aw-workflow-id: security-audit" in:body
```

Find all items (issues, PRs, discussions) from any workflow in your organization:
```
org:your-org "gh-aw-workflow-id:" in:body
```

Find comments from a specific workflow:
```
repo:owner/repo "gh-aw-workflow-id: bot-responder" in:comments
```

> [!TIP]
> **Search Tips for Workflow Markers**
>
> - Use quotes around the marker text to search for the exact phrase
> - Add `in:body` to search issue/PR descriptions, or `in:comments` for comments
> - Combine with other filters like `is:open`, `is:closed`, `created:>2024-01-01`
> - The workflow name in the marker is the workflow filename without the `.md` extension
> - Use GitHub's advanced search to refine results: [Advanced search documentation](https://docs.github.com/en/search-github/searching-on-github/searching-issues-and-pull-requests)

## Use Cases

**Clean content for public repositories:**
```yaml wrap
safe-outputs:
  footer: false
  create-issue:
    title-prefix: "[report] "
    labels: [automated]
```

**Custom branding - footers on PRs only:**
```yaml wrap
safe-outputs:
  footer: false                      # hide for issues
  create-issue:
    title-prefix: "[issue] "
  create-pull-request:
    footer: true                     # show for PRs
    title-prefix: "[pr] "
```

**Minimal documentation updates:**
```yaml wrap
safe-outputs:
  update-release:
    footer: false                    # clean release notes
    max: 1
```

## Backward Compatibility

The default value for `footer` is `true`, maintaining backward compatibility with existing workflows. To hide footers, you must explicitly set `footer: false`.

## Customizing Footer Messages

Instead of hiding footers entirely, you can customize the footer message text using the `messages.footer` template. This allows you to maintain attribution while using custom branding:

```yaml wrap
safe-outputs:
  messages:
    footer: "> 🤖 Powered by [{workflow_name}]({run_url})"
  create-issue:
    title-prefix: "[bot] "
```

The `messages.footer` template supports variables like `{workflow_name}`, `{run_url}`, `{triggering_number}`, and more. See [Custom Messages](/gh-aw/reference/safe-outputs/#custom-messages-messages) for complete documentation on message templates and available variables.

**When to use each approach:**
- **`footer: false`** - Completely hide attribution footers for cleaner content
- **`messages.footer`** - Keep attribution but customize the text and branding

## Related Documentation

- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Complete safe outputs reference
- [Custom Messages](/gh-aw/reference/safe-outputs/#custom-messages-messages) - Message templates and variables
- [Frontmatter](/gh-aw/reference/frontmatter/) - All configuration options for workflows
