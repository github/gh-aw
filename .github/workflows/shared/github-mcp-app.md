---
#tools:
#  github:
#    app:
#      app-id: ${{ vars.APP_ID }}
#      private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

<!--
# Shared GitHub MCP App Configuration

This shared workflow provides repository-level GitHub App configuration for the GitHub MCP server.

## Configuration Variables

This shared workflow expects:
- **Repository Variable**: `APP_ID` - The GitHub App ID for MCP server authentication
- **Repository Secret**: `APP_PRIVATE_KEY` - The GitHub App private key for MCP server authentication

## Usage

Import this configuration in your workflows to enable GitHub App authentication for the GitHub MCP server:

```yaml
imports:
  - shared/github-mcp-app.md
```

The configuration will be automatically merged into your workflow's tools.github section.

## Benefits

- **Enhanced Permissions**: GitHub App tokens can have more granular permissions than PATs
- **Automatic Token Management**: Token is automatically minted at workflow start and invalidated at workflow end
- **Security**: Token is scoped to specific repositories and permissions
- **Centralized Configuration**: Single source of truth for GitHub MCP app credentials
- **Easy Updates**: Change credentials in one place
- **Consistent Usage**: All workflows use the same configuration pattern
- **Repository-Scoped**: Uses repository-specific variables and secrets

## How It Works

When this shared workflow is imported:
1. A GitHub App installation access token is minted at workflow start
2. The token is automatically configured with permissions matching the agent job's `permissions` field
3. The token is used for all GitHub MCP server operations
4. The token is automatically invalidated at workflow end (even on failure)

## Token Precedence

When a GitHub App is configured, it takes highest precedence:
1. GitHub App token (this configuration) - **Highest priority**
2. Custom `github-token` setting
3. Default token fallback chain

## Example

```yaml
---
permissions:
  contents: read
  issues: write
  pull-requests: read
imports:
  - shared/github-mcp-app.md
tools:
  github:
    toolsets: [repos, issues, pull_requests]
---

# Your Workflow

Your workflow content here...
```

The GitHub App token will be minted with `permission-contents: read`, `permission-issues: write`, and `permission-pull-requests: read`.
-->
