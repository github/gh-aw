# Safe Output Environment Variables Reference

This document describes environment variable requirements for each safe output job type in GitHub Agentic Workflows.

## Overview

Safe output jobs use environment variables to pass configuration from workflow frontmatter to the generated GitHub Actions jobs. Understanding these variables helps with debugging and troubleshooting configuration issues.

## Common Environment Variables

These environment variables are present in all safe output jobs:

| Variable | Description | Required | Example |
|----------|-------------|----------|---------|
| `GH_AW_AGENT_OUTPUT` | Path to agent output file containing safe output requests | Yes | `/opt/gh-aw/safeoutputs/outputs.jsonl` |
| `GH_AW_WORKFLOW_NAME` | Workflow name for attribution in footers and messages | Yes | `"Issue Triage"` |
| `GH_AW_WORKFLOW_SOURCE` | Source location in format `owner/repo/path@ref` | No | `"owner/repo/workflows/triage.md@main"` |
| `GH_AW_WORKFLOW_SOURCE_URL` | GitHub URL to workflow source file | No | Auto-generated from source |
| `GH_AW_TRACKER_ID` | Tracker ID for linking related workflows | No | `"issue-123"` |
| `GH_AW_ENGINE_ID` | AI engine identifier (copilot, claude, codex, custom) | No | `"copilot"` |
| `GH_AW_ENGINE_VERSION` | AI engine version | No | `"1.0.0"` |
| `GH_AW_ENGINE_MODEL` | AI engine model name | No | `"gpt-4"` |
| `GH_AW_SAFE_OUTPUTS_STAGED` | Preview mode flag - when true, operations are previewed but not executed | No | `"true"` |
| `GH_AW_TARGET_REPO_SLUG` | Target repository for cross-repo operations | No | `"owner/target-repo"` |
| `GH_AW_SAFE_OUTPUT_MESSAGES` | Custom message templates in JSON format | No | JSON string |
| `GITHUB_TOKEN` | GitHub API authentication token | Yes | From secrets or default `GITHUB_TOKEN` |

## Job-Specific Environment Variables

Each safe output type has additional environment variables specific to its configuration:

### Create Issue (`create-issue:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_ISSUE_TITLE_PREFIX` | Prefix for issue titles | `title-prefix` configured | `"[ai] "` |
| `GH_AW_ISSUE_LABELS` | Comma-separated labels to add | `labels` configured | `"automation,ai"` |
| `GH_AW_ISSUE_ALLOWED_LABELS` | Comma-separated allowed labels | `allowed-labels` configured | `"bug,enhancement"` |
| `GH_AW_ISSUE_EXPIRES` | Days until auto-close | `expires` configured | `"7"` |
| `GH_AW_ASSIGN_COPILOT` | Assign copilot agent to created issues | `assignees: copilot` configured | `"true"` |
| `GH_AW_TEMPORARY_ID_MAP` | Map of temporary IDs to real issue numbers | Multiple issues with references | From previous job output |

### Create Pull Request (`create-pull-request:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_WORKFLOW_ID` | Workflow identifier for branch naming | Always | `"agent"` (job name) |
| `GH_AW_BASE_BRANCH` | Base branch for PR | Always | `${{ github.ref_name }}` |
| `GH_AW_PR_TITLE_PREFIX` | Prefix for PR titles | `title-prefix` configured | `"[agent] "` |
| `GH_AW_PR_LABELS` | Comma-separated labels to add | `labels` configured | `"automation"` |
| `GH_AW_PR_ALLOWED_LABELS` | Comma-separated allowed labels | `allowed-labels` configured | `"automation,bug"` |
| `GH_AW_PR_DRAFT` | Create as draft PR | Always (default: `true`) | `"true"` or `"false"` |
| `GH_AW_PR_IF_NO_CHANGES` | Behavior when no changes detected | Always (default: `warn`) | `"warn"`, `"error"`, or `"ignore"` |
| `GH_AW_PR_ALLOW_EMPTY` | Allow PRs with no changes | Always (default: `false`) | `"true"` or `"false"` |
| `GH_AW_MAX_PATCH_SIZE` | Maximum patch size in KB | Always (default: `1024`) | `1024` |
| `GH_AW_PR_EXPIRES` | Days until auto-close (same-repo only) | `expires` configured | `"14"` |
| `GH_AW_COMMENT_ID` | Comment ID from activation job | Command-triggered workflow | From activation job output |
| `GH_AW_COMMENT_REPO` | Repository containing comment | Command-triggered workflow | From activation job output |

### Add Comment (`add-comment:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_COMMENT_TARGET` | Target for comments: `triggering`, `*`, or number | `target` configured | `"*"` or `"123"` |
| `GH_AW_HIDE_OLDER_COMMENTS` | Hide older comments from same workflow | `hide-older-comments: true` | `"true"` |
| `GH_AW_ALLOWED_REASONS` | Allowed hide reasons as JSON array | `allowed-reasons` configured | `["outdated","resolved"]` |
| `GH_AW_CREATED_ISSUE_URL` | URL of created issue | Depends on `create-issue` job | From create-issue job output |
| `GH_AW_CREATED_ISSUE_NUMBER` | Number of created issue | Depends on `create-issue` job | From create-issue job output |
| `GH_AW_CREATED_DISCUSSION_URL` | URL of created discussion | Depends on `create-discussion` job | From create-discussion job output |
| `GH_AW_CREATED_DISCUSSION_NUMBER` | Number of created discussion | Depends on `create-discussion` job | From create-discussion job output |
| `GH_AW_CREATED_PULL_REQUEST_URL` | URL of created PR | Depends on `create-pull-request` job | From create-pull-request job output |
| `GH_AW_CREATED_PULL_REQUEST_NUMBER` | Number of created PR | Depends on `create-pull-request` job | From create-pull-request job output |
| `GH_AW_TEMPORARY_ID_MAP` | Map of temporary IDs to real numbers | Issues with temporary IDs created | From create-issue job output |

### Create Discussion (`create-discussion:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_DISCUSSION_TITLE_PREFIX` | Prefix for discussion titles | `title-prefix` configured | `"[ai] "` |
| `GH_AW_DISCUSSION_CATEGORY` | Category slug, name, or ID | `category` configured | `"general"` or `"DIC_kwDOABCD123"` |
| `GH_AW_CLOSE_OLDER_DISCUSSIONS` | Close older discussions from same workflow | Multiple discussions with closing | `"true"` |
| `GH_AW_DISCUSSION_EXPIRES` | Days until auto-close | `expires` configured | `"3"` |
| `GH_AW_TEMPORARY_ID_MAP` | Map of temporary IDs to real issue numbers | Issues created with references | From create-issue job output |

### Close Issue (`close-issue:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_ISSUE_REQUIRED_LABELS` | Required labels (any match) | `required-labels` configured | `"automated,bot"` |
| `GH_AW_ISSUE_REQUIRED_TITLE_PREFIX` | Required title prefix | `required-title-prefix` configured | `"[bot]"` |

### Close Pull Request (`close-pull-request:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_PR_REQUIRED_LABELS` | Required labels (any match) | `required-labels` configured | `"automated,stale"` |
| `GH_AW_PR_REQUIRED_TITLE_PREFIX` | Required title prefix | `required-title-prefix` configured | `"[bot]"` |

### Close Discussion (`close-discussion:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_DISCUSSION_REQUIRED_CATEGORY` | Required category name | `required-category` configured | `"Ideas"` |
| `GH_AW_DISCUSSION_REQUIRED_LABELS` | Required labels (any match) | `required-labels` configured | `"resolved"` |
| `GH_AW_DISCUSSION_REQUIRED_TITLE_PREFIX` | Required title prefix | `required-title-prefix` configured | `"[ai]"` |

### PR Review Comments (`create-pull-request-review-comment:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_PR_REVIEW_COMMENT_SIDE` | Side of diff: LEFT or RIGHT | Always (default: `RIGHT`) | `"RIGHT"` |
| `GH_AW_PR_REVIEW_COMMENT_TARGET` | Target PR: triggering, *, or number | `target` configured | `"*"` |

### Update Issue (`update-issue:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_UPDATE_TARGET` | Target issue: triggering, *, or number | `target` configured | `"*"` |
| `GH_AW_UPDATE_TITLE` | Allow title updates | `title:` field present | `"true"` |
| `GH_AW_UPDATE_BODY` | Allow body updates | `body:` field present | `"true"` |
| `GH_AW_UPDATE_LABELS` | Allow label updates | `labels:` field present | `"true"` |

### Update Discussion (`update-discussion:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_UPDATE_TARGET` | Target discussion: triggering, *, or number | `target` configured | `"*"` |
| `GH_AW_DISCUSSION_ALLOWED_LABELS` | Allowed label changes | `allowed-labels` configured | `"feedback,resolved"` |

### Push to PR Branch (`push-to-pull-request-branch:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_PUSH_TARGET` | Target PR: triggering, *, or number | Always (default: `triggering`) | `"*"` |
| `GH_AW_PUSH_IF_NO_CHANGES` | Behavior when no changes | Always (default: `warn`) | `"warn"`, `"error"`, or `"ignore"` |
| `GH_AW_PR_TITLE_PREFIX` | Required title prefix | `title-prefix` configured | `"[bot] "` |
| `GH_AW_PR_LABELS` | Required labels (all must match) | `labels` configured | `"automated"` |
| `GH_AW_COMMIT_TITLE_SUFFIX` | Suffix for commit titles | `commit-title-suffix` configured | `" [skip ci]"` |
| `GH_AW_MAX_PATCH_SIZE` | Maximum patch size in KB | Always (default: `1024`) | `1024` |
| `GH_AW_COMMENT_ID` | Comment ID from activation job | Command-triggered workflow | From activation job output |
| `GH_AW_COMMENT_REPO` | Repository containing comment | Command-triggered workflow | From activation job output |

### Upload Assets (`upload-asset:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_ASSETS_BRANCH` | Target branch name | Always (default: `assets/${{ github.workflow }}`) | `"assets/screenshots"` |
| `GH_AW_ASSETS_MAX_SIZE_KB` | Maximum file size in KB | Always (default: `10240`) | `10240` |
| `GH_AW_ASSETS_ALLOWED_EXTS` | Comma-separated allowed extensions | Always (default: `.png,.jpg,.jpeg`) | `".png,.jpg,.svg"` |

### Add Labels (`add-labels:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_LABELS_ALLOWED` | Comma-separated allowed labels | `allowed` configured | `"bug,enhancement"` |
| `GH_AW_LABELS_MAX_COUNT` | Maximum labels to add | Always (default: `3`) | `3` |
| `GH_AW_LABELS_TARGET` | Target: triggering, *, or number | `target` configured | `"*"` |

### Add Reviewer (`add-reviewer:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_REVIEWERS_ALLOWED` | Comma-separated allowed reviewers | `reviewers` configured | `"user1,copilot"` |

### Assign Milestone (`assign-milestone:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_MILESTONES_ALLOWED` | Comma-separated allowed milestone titles | `allowed` configured | `"v1.0,v2.0"` |

### Create Agent Task (`create-agent-session:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_AGENT_TASK_BASE` | Base branch for pull request | `base` configured or `${{ github.ref_name }}` fallback | `"main"` or `"develop"` |
| `GH_AW_TARGET_REPO` | Target repository for cross-repo tasks | `target-repo` configured | `"owner/repo"` |

### Assign to Agent (`assign-to-agent:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_DEFAULT_AGENT` | Default agent name | `name` configured | `"copilot"` |

### Assign to User (`assign-to-user:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_USERS_ALLOWED` | Comma-separated allowed usernames | `allowed` configured | `"user1,user2"` |

### Link Sub-Issue (`link-sub-issue:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_PARENT_REQUIRED_LABELS` | Parent must have any of these labels | `parent-required-labels` configured | `"epic"` |
| `GH_AW_PARENT_TITLE_PREFIX` | Parent must match this prefix | `parent-title-prefix` configured | `"[Epic]"` |
| `GH_AW_SUB_REQUIRED_LABELS` | Sub-issue must have any of these labels | `sub-required-labels` configured | `"task"` |
| `GH_AW_SUB_TITLE_PREFIX` | Sub-issue must match this prefix | `sub-title-prefix` configured | `"[Task]"` |
| `GH_AW_TEMPORARY_ID_MAP` | Map of temporary IDs to real issue numbers | Issues created with references | From create-issue job output |

### Update Project (`update-project:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_PROJECT_GITHUB_TOKEN` | PAT with Projects access (required) | Always | From secrets |

### Update Release (`update-release:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_RELEASE_OPERATION` | Operation: replace, append, or prepend | Agent specifies | `"replace"` |

### Code Scanning Alerts (`create-code-scanning-alert:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_SECURITY_REPORT_MAX` | Maximum alerts | `max` configured (0=unlimited) | `50` |
| `GH_AW_SECURITY_REPORT_DRIVER` | Tool name in SARIF | Always | Workflow name or driver field |
| `GH_AW_WORKFLOW_FILENAME` | Workflow filename for SARIF | Always | Auto-generated |

### No-Op (`noop:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_NOOP_MAX` | Maximum no-op messages | Always (default: `1`) | `1` |

### Missing Tool (`missing-tool:`)

| Variable | Description | Set When | Example |
|----------|-------------|----------|---------|
| `GH_AW_MISSING_TOOL_MAX` | Maximum missing tool reports | `max` configured (0=unlimited) | `0` |

## Activation Job Variables

When workflows are triggered by comments or reactions, the activation job provides additional context:

| Variable | Description | Example |
|----------|-------------|---------|
| `GH_AW_COMMENT_ID` | ID of triggering comment | `12345` |
| `GH_AW_COMMENT_REPO` | Repository containing comment | `owner/repo` |
| `GH_AW_REACTION` | Reaction that triggered workflow | `"eyes"` |
| `GH_AW_COMMAND` | Command that triggered workflow | `"test-bot"` |
| `GH_AW_LOCK_FOR_AGENT` | Lock issue/PR for agent processing | `"true"` |

## Safe Inputs Variables

When safe inputs are enabled, additional variables are available:

| Variable | Description | Example |
|----------|-------------|---------|
| `GH_AW_SAFE_INPUTS_PORT` | HTTP server port for safe inputs | From safe-inputs job output |
| `GH_AW_SAFE_INPUTS_API_KEY` | API key for safe inputs authentication | From safe-inputs job output |

## Custom Environment Variables

You can add custom environment variables at the safe-outputs level:

```yaml
safe-outputs:
  create-issue:
  env:
    GITHUB_TOKEN: ${{ secrets.CUSTOM_PAT }}
    DEBUG_MODE: "true"
    CUSTOM_API_KEY: ${{ secrets.CUSTOM_API_KEY }}
```text

These variables are added to all safe output jobs and take precedence over default values.

## Troubleshooting

### Common Issues

**Missing required variables**: If a safe output job fails with a missing variable error, ensure the corresponding configuration is present in frontmatter:
- `GH_AW_WORKFLOW_NAME` → workflow name in frontmatter
- `GH_AW_AGENT_OUTPUT` → automatically set, indicates missing safe-outputs configuration
- `GITHUB_TOKEN` → check token configuration and permissions

**Token permissions**: Different operations require different tokens:
- Default `GITHUB_TOKEN` → most operations
- `GH_AW_PROJECT_GITHUB_TOKEN` or custom token → GitHub Projects v2
- `COPILOT_GITHUB_TOKEN` or `GH_AW_GITHUB_TOKEN` → Copilot reviewer/task operations
- `GH_AW_AGENT_TOKEN` → Agent assignment operations

**Cross-repository operations**: Set `GH_AW_TARGET_REPO_SLUG` via `target-repo` configuration. Requires a PAT with access to target repository.

**Staged mode not working**: Ensure `GH_AW_SAFE_OUTPUTS_STAGED` is set to `"true"` via `staged: true` configuration.

### Debugging Variables

To debug environment variables, add a step to your workflow that prints them:

```yaml
safe-outputs:
  jobs:
    debug:
      description: "Debug environment variables"
      steps:
        - name: Print environment variables
          run: env | grep GH_AW_ | sort
```text

## Related Documentation

- [Safe Output Messages Design](./safe-output-messages.md) - Message patterns and formatting for safe outputs
- [Developer Instructions](../AGENTS.md) - Development guidelines including safe output implementation
