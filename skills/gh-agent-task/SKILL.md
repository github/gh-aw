---
name: gh-agent-task
description: GitHub CLI Agent Task Extension
---


# GitHub CLI Agent Task Extension

The `gh agent-task` CLI extension enables creating GitHub Copilot agent tasks through the command line. An agent task is a specialized GitHub issue that triggers GitHub Copilot to perform automated code changes based on natural language instructions.

**Repository**: https://github.com/github/agent-task (internal GitHub extension)

## Overview

Agent tasks are GitHub issues that:
- Contain natural language instructions for code changes
- Trigger GitHub Copilot to autonomously execute the task
- Create pull requests with the implemented changes
- Provide a workflow for reviewing and merging automated code modifications

## Installation

Install via GitHub CLI:

```bash
gh extension install github/agent-task
```

**Note**: This extension requires authentication with a Personal Access Token (PAT) that has appropriate permissions for creating issues and pull requests.

## Core Commands

### Create Agent Task

Create a new agent task from a description:

```bash
# Create task with inline description
gh agent-task create "Fix the bug in authentication flow"

# Create task from file
gh agent-task create --from-file task-description.md

# Specify base branch
gh agent-task create --base develop "Implement new feature"

# Create in different repository
gh agent-task create --repo owner/repo "Update documentation"
```

**Command Parameters:**
- **Description** (positional): Natural language description of the task
- **`--from-file <path>`**: Read task description from file
- **`--base <branch>`**: Base branch for the pull request (default: repository default branch)
- **`--repo <owner/repo>`**: Target repository (default: current repository)

**Output Format:**
The command outputs the URL of the created agent task:
```
https://github.com/owner/repo/issues/123
```

### List Agent Tasks

List agent tasks in a repository:

```bash
# List all agent tasks
gh agent-task list

# List with filters
gh agent-task list --state open
gh agent-task list --state closed
gh agent-task list --state all
```

### View Agent Task

View details of a specific agent task:

```bash
# View by number
gh agent-task view 123

# View by URL
gh agent-task view https://github.com/owner/repo/issues/123
```

### Update Agent Task

Update an existing agent task:

```bash
# Update description
gh agent-task update 123 "Updated task description"

# Update from file
gh agent-task update 123 --from-file updated-description.md
```

## Task Description Format

Agent task descriptions should be clear, specific natural language instructions:

**Good Example:**
```markdown
# Refactor User Authentication

Refactor the user authentication flow in `src/auth/login.js` to:
1. Use async/await instead of callbacks
2. Add proper error handling with specific error messages
3. Add input validation for email format
4. Update tests to cover the new implementation

Maintain backward compatibility with the existing API.
```

**Poor Example:**
```markdown
Fix auth
```

**Best Practices:**
- Be specific about what needs to change
- Reference file paths when relevant
- Include acceptance criteria
- Specify any constraints or requirements
- Mention testing expectations

## Integration with GitHub Agentic Workflows

The `gh agent-task` extension is used by the `create-agent-task` safe output feature in GitHub Agentic Workflows (gh-aw).

### Safe Output Configuration

```yaml
safe-outputs:
  create-agent-task:
    base: main                       # Base branch for agent task PR
    target-repo: "owner/target-repo" # Cross-repository task creation
```

### Workflow Example

```yaml
on:
  issues:
    types: [labeled]
permissions:
  contents: read
  actions: read
engine: claude
safe-outputs:
  create-agent-task:
    base: main

# Code Task Delegator

When an issue is labeled with "code-task", analyze the requirements and create a GitHub Copilot agent task with detailed instructions for implementing the requested changes.
```

### Implementation Details

The safe output processor:
1. Reads agent output from the workflow execution
2. Extracts `create_agent_task` items from the structured output
3. Writes task descriptions to temporary files
4. Executes `gh agent-task create --from-file <file> --base <branch>`
5. Captures the created task URL and number
6. Reports results in job summary

**Environment Variables:**
- `GITHUB_AW_AGENT_TASK_BASE`: Base branch for the pull request
- `GITHUB_AW_TARGET_REPO`: Target repository for cross-repo task creation
- `GITHUB_AW_SAFE_OUTPUTS_STAGED`: Preview mode flag

## Authentication Requirements

Agent task creation requires elevated permissions beyond the default `GITHUB_TOKEN`:

**Required Permissions:**
- `contents: write` - To create branches and commits
- `issues: write` - To create the agent task issue
- `pull-requests: write` - To create pull requests

**Token Precedence:**
1. `COPILOT_GITHUB_TOKEN` - Dedicated Copilot operations token (recommended)
2. `GH_AW_GITHUB_TOKEN` - General override token (legacy)
3. Custom token via `github-token` configuration field

**Note**: The default `GITHUB_TOKEN` is **not** supported as it lacks required permissions. The `COPILOT_CLI_TOKEN` and `GH_AW_COPILOT_TOKEN` secrets are no longer supported as of v0.26+.

### Setting Up Authentication

Store your Personal Access Token in repository secrets:

```bash
# In your repository settings, add secret:
# Name: COPILOT_GITHUB_TOKEN (recommended)
# Value: ghp_YourPersonalAccessToken
```

:::note[Backward Compatibility]
Legacy token name `GH_AW_GITHUB_TOKEN` is still supported for backward compatibility. The `GH_AW_COPILOT_TOKEN` token is no longer supported as of v0.26+.
:::

## Error Handling

### Authentication Errors

```
Error: failed to create agent task
authentication required
```

**Solution**: Configure `COPILOT_GITHUB_TOKEN` or legacy `GH_AW_GITHUB_TOKEN` with a PAT.

### Permission Errors

```
Error: 403 Forbidden
Resource not accessible by integration
```

**Solution**: Ensure the token has `contents: write`, `issues: write`, and `pull-requests: write` permissions.

### Repository Not Found

```
Error: repository not found
```

**Solution**: Verify the target repository exists and the token has access to it.

## Testing in Staged Mode

When `safe-outputs.staged: true`, agent tasks are previewed without creation:

```yaml
safe-outputs:
  staged: true
  create-agent-task:
```

**Staged Output:**
```markdown
## 🎭 Staged Mode: Create Agent Tasks Preview

The following agent tasks would be created if staged mode was disabled:

### Task 1

**Description:**
Refactor authentication to use async/await pattern

**Base Branch:** main

**Target Repository:** owner/repo
```

## Common Patterns

### Issue-Triggered Agent Tasks

```yaml
on:
  issues:
    types: [labeled]
engine: claude
safe-outputs:
  create-agent-task:

When issue is labeled with "needs-implementation", create an agent task with implementation instructions.
```

### Scheduled Code Improvements

```yaml
on:
  schedule:
    - cron: "0 9 * * 1"  # Monday 9AM
engine: copilot
safe-outputs:
  create-agent-task:
    base: develop

Analyze codebase for improvement opportunities and create agent tasks for top 3 improvements.
```

### Cross-Repository Task Delegation

```yaml
on: workflow_dispatch
engine: claude
safe-outputs:
  create-agent-task:
    target-repo: "organization/backend-repo"
    base: main

Create agent task in backend repository to implement the API changes described in this issue.
```

## Best Practices

### Task Description Guidelines

1. **Be Specific**: Include file paths, function names, and exact requirements
2. **Include Context**: Explain why the change is needed
3. **Define Success**: Specify acceptance criteria or expected outcomes
4. **Mention Tests**: Request test coverage for changes
5. **Set Constraints**: Note any compatibility requirements or limitations

### Security Considerations

1. **Token Security**: Store PATs as secrets, never commit to repository
2. **Permission Scope**: Use minimum required permissions on tokens
3. **Repository Access**: Validate target repository before task creation
4. **Review Process**: Establish review workflow for agent-generated code

### Operational Guidelines

1. **Monitor Usage**: Track agent task creation and completion rates
2. **Review Output**: Always review agent-generated pull requests
3. **Iterate**: Refine task descriptions based on agent performance
4. **Document**: Maintain patterns for common task types

## Troubleshooting

### Task Creation Fails Silently

**Symptom**: No error but no task created

**Check**:
1. Verify `COPILOT_GITHUB_TOKEN` is set in repository secrets
2. Confirm token has required permissions
3. Check job logs for error messages
4. Verify target repository exists and is accessible

### Agent Task Not Triggering Copilot

**Symptom**: Task created but no automated PR

**Possible Causes**:
1. GitHub Copilot not enabled for repository
2. Task description unclear or ambiguous
3. Repository settings blocking automated PRs
4. Copilot service issues

**Solution**: Check repository Copilot settings and refine task description.

### Cross-Repository Tasks Fail

**Symptom**: Error when creating tasks in different repository

**Check**:
1. Token has access to target repository
2. Target repository exists and is spelled correctly
3. Token has required permissions in target repository

## Output Structure

When used via safe outputs, the create-agent-task job provides outputs:

```yaml
outputs:
  task_number: "123"
  task_url: "https://github.com/owner/repo/issues/123"
```

**Usage in Dependent Jobs:**
```yaml
jobs:
  follow_up:
    needs: create_agent_task
    steps:
      - name: Notify team
        run: |
          echo "Agent task created: ${{ needs.create_agent_task.outputs.task_url }}"
```

## References

- [GitHub Copilot Documentation](https://docs.github.com/en/copilot)
- [GitHub CLI Extensions](https://docs.github.com/en/github-cli/github-cli/using-github-cli-extensions)
- [Safe Outputs Documentation](https://github.github.com/gh-aw/reference/safe-outputs/)
- [Personal Access Tokens](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/creating-a-personal-access-token)
