---
title: Error Reference
description: Comprehensive reference of error messages in GitHub Agentic Workflows, including schema validation, compilation, and runtime errors with solutions.
sidebar:
  order: 100
---

This reference documents common error messages encountered when working with GitHub Agentic Workflows, organized by when they occur during the workflow lifecycle.

## Schema Validation Errors

Schema validation errors occur when the workflow frontmatter does not conform to the expected JSON schema. These errors are detected during the compilation process.

> [!TIP]
> Typo Detection
> When you make a typo in frontmatter field names, the compiler automatically suggests correct field names using fuzzy matching. Look for "Did you mean" suggestions in error messages to quickly identify and fix common typos like `permisions` → `permissions` or `engnie` → `engine`.

### Frontmatter Not Properly Closed

`frontmatter not properly closed`

The YAML frontmatter section lacks a closing `---` delimiter. Ensure the frontmatter is enclosed between two `---` lines:

```aw wrap
---
on: push
permissions:
  contents: read
---

# Workflow content
```

### Failed to Parse Frontmatter

`failed to parse frontmatter: [yaml error details]`

Invalid YAML syntax in the frontmatter. Check indentation (use spaces, not tabs), ensure colons are followed by spaces, quote strings with special characters, and verify array/object syntax:

```yaml wrap
# Correct indentation and spacing
on:
  issues:
    types: [opened]
```

### Invalid Field Type

`timeout-minutes must be an integer`

A field received a value of the wrong type. Use the correct type as specified in the [frontmatter reference](/gh-aw/reference/frontmatter/) (e.g., `timeout-minutes: 10` not `"10"`).

### Unknown Property

`Unknown property: permisions. Did you mean 'permissions'?`

Use the suggested field name from the error message. The compiler uses fuzzy matching to suggest corrections for common typos.

### Imports Field Must Be Array

`imports field must be an array of strings`

The `imports:` field must use array syntax:

```yaml wrap
imports:
  - shared/tools.md
  - shared/security.md
```

### Multiple Agent Files in Imports

`multiple agent files found in imports: 'file1.md' and 'file2.md'. Only one agent file is allowed per workflow`

Import only one agent file per workflow from `.github/agents/`.

## Compilation Errors

Compilation errors occur when the workflow file is being converted to a GitHub Actions YAML workflow (`.lock.yml` file).

### Workflow File Not Found

`workflow file not found: [path]`

Verify the file exists in `.github/workflows/` and the filename is correct. Use `gh aw compile` without arguments to compile all workflows in the directory.

### Failed to Resolve Import

`failed to resolve import 'path': [details]`

Ensure the imported file exists at the specified path (relative to repository root) and has read permissions.

### Invalid Workflow Specification

`invalid workflowspec: must be owner/repo/path[@ref]`

Use the correct format for remote imports: `owner/repo/path[@ref]` (e.g., `github/gh-aw/.github/workflows/shared/example.md@main`).

### Section Not Found

`section 'name' not found`

Verify the referenced section exists in the frontmatter. This typically occurs during internal processing and may indicate a bug.

## Runtime Errors

Runtime errors occur when the compiled workflow executes in GitHub Actions.

### Time Delta Errors

`invalid time delta format: +[value]. Expected format like +25h, +3d, +1w, +1mo, +1d12h30m`

Use the correct time delta syntax with supported units: `h` (hours, minimum), `d` (days), `w` (weeks), `mo` (months). Example: `stop-after: +24h`.

`minute unit 'm' is not allowed for stop-after. Minimum unit is hours 'h'. Use +[hours]h instead of +[minutes]m`

Convert minutes to hours (round up as needed). Use `+2h` instead of `+90m`.

### Time Delta Too Large

`time delta too large: [value] [unit] exceeds maximum of [max]`

Reduce the time delta or use a larger unit. Maximum values: 12 months, 52 weeks, 365 days, 8760 hours.

### Duplicate Time Unit

`duplicate unit '[unit]' in time delta: +[value]`

Combine values for the same unit (e.g., `+3d` instead of `+1d2d`).

### Unable to Parse Date-Time

`unable to parse date-time: [value]. Supported formats include: YYYY-MM-DD HH:MM:SS, MM/DD/YYYY, January 2 2006, 1st June 2025, etc`

Use a supported date format like `"2025-12-31 23:59:59"`, `"December 31, 2025"`, or `"12/31/2025"`.

### JQ Not Found

`jq not found in PATH`

Install `jq`: Ubuntu/Debian: `sudo apt-get install jq`, macOS: `brew install jq`.

### Authentication Errors

`authentication required`

Authenticate with GitHub CLI (`gh auth login`) or ensure `GITHUB_TOKEN` is available in GitHub Actions.

## Engine-Specific Errors

### Manual Approval Invalid Format

`manual-approval value must be a string`

Use a string value: `manual-approval: "Approve deployment to production"`.

### Invalid On Section Format

`invalid on: section format`

Verify the trigger configuration follows [GitHub Actions syntax](/gh-aw/reference/triggers/) (e.g., `on: push`, `on: { push: { branches: [main] } }`).

## File Processing Errors

### Failed to Read File

`failed to read file [path]: [details]`

Verify the file exists, has read permissions, and the disk is not full.

### Failed to Create Directory

`failed to create .github/workflows directory: [details]`

Check file system permissions and available disk space.

### Workflow File Already Exists

`workflow file '[path]' already exists. Use --force to overwrite`

Use `gh aw init my-workflow --force` to overwrite.

## Safe Output Errors

### Failed to Parse Existing MCP Config

`failed to parse existing mcp.json: [details]`

Fix the JSON syntax (validate with `cat .vscode/mcp.json | jq .`) or delete the file to regenerate.

### Failed to Marshal MCP Config

`failed to marshal mcp.json: [details]`

Internal error when generating the MCP configuration. Report the issue with reproduction steps.

## Top User-Facing Errors

This section documents the most common errors you may encounter when working with GitHub Agentic Workflows.

### Cannot Use Command with Event Trigger

`cannot use 'command' with 'issues' in the same workflow`

Remove the conflicting event trigger (`issues`, `issue_comment`, `pull_request`, or `pull_request_review_comment`). The `command:` configuration automatically handles these events. To restrict to specific events, use the `events:` field within the command configuration.

### Strict Mode Network Configuration Required

`strict mode: 'network' configuration is required`

Add network configuration: use `network: defaults` (recommended), specify allowed domains explicitly, or deny all network access with `network: {}`.

### Strict Mode Write Permission Not Allowed

`strict mode: write permission 'contents: write' is not allowed`

Use `safe-outputs` instead of write permissions. Configure safe outputs like `create-issue` or `create-pull-request` with appropriate options.

### Strict Mode Network Wildcard Not Allowed

`strict mode: wildcard '*' is not allowed in network.allowed domains`

Replace the standalone `*` wildcard with specific domains, wildcard patterns (e.g., `*.cdn.example.com`), or ecosystem identifiers (e.g., `python`, `node`). Alternatively, use `network: defaults`.

### HTTP MCP Tool Missing Required URL Field

`http MCP tool 'my-tool' missing required 'url' field`

Add the required `url:` field to the HTTP MCP server configuration.

### Job Name Cannot Be Empty

`job name cannot be empty`

Internal error. Report it with your workflow file.

### Unable to Determine MCP Type

`unable to determine MCP type for tool 'my-tool': missing type, url, command, or container`

Specify at least one of: `type`, `url`, `command`, or `container`.

### Tool MCP Configuration Cannot Specify Both Container and Command

`tool 'my-tool' mcp configuration cannot specify both 'container' and 'command'`

Use either `container:` OR `command:`, not both.

### HTTP MCP Configuration Cannot Use Container

`tool 'my-tool' mcp configuration with type 'http' cannot use 'container' field`

Remove the `container:` field from HTTP MCP server configurations (only valid for stdio-based servers).

### Strict Mode Custom MCP Server Requires Network Configuration

`strict mode: custom MCP server 'my-server' with container must have network configuration`

Add network configuration with allowed domains to containerized MCP servers in strict mode.

### Repository Features Not Enabled for Safe Outputs

`workflow uses safe-outputs.create-issue but repository owner/repo does not have issues enabled`

Enable the required repository feature (Settings → General → Features) or use a different safe output type.

### Engine Does Not Support Firewall

`strict mode: engine does not support firewall`

Use an engine with firewall support (e.g., `copilot`), compile without `--strict` flag, or use `network: defaults`.

## Toolsets Configuration Issues

### Tool Not Found After Migrating to Toolsets

After changing from `allowed:` to `toolsets:`, expected tools are not available. The tool may be in a different toolset than expected, or a narrower toolset was chosen.

Check the [tool-to-toolset mapping](/gh-aw/guides/mcps/#migration-from-allowed-to-toolsets), use `gh aw mcp inspect <workflow>` to see available tools, then add the required toolset.

### Invalid Toolset Name

`invalid toolset: 'action' is not a valid toolset`

Use valid toolset names: `context`, `repos`, `issues`, `pull_requests`, `users`, `actions`, `code_security`, `discussions`, `labels`, `notifications`, `orgs`, `projects`, `gists`, `search`, `dependabot`, `experiments`, `secret_protection`, `security_advisories`, `stargazers`, `default`, `all`.

### Toolsets and Allowed Conflict

Unexpected tool availability when using both `toolsets:` and `allowed:`. When both are specified, `allowed:` restricts tools to only those listed within the enabled toolsets.

For most use cases, use only `toolsets:` without `allowed:`:

```yaml wrap
# Recommended: use only toolsets
tools:
  github:
    toolsets: [issues]  # Gets all issue-related tools

# Advanced: restrict within toolset
tools:
  github:
    toolsets: [issues]
    allowed: [create_issue]  # Only create_issue from issues toolset
```

## Troubleshooting Tips

- Use `--verbose` flag for detailed error information
- Validate YAML syntax and check file paths
- Consult the [frontmatter reference](/gh-aw/reference/frontmatter-full/)
- Run `gh aw compile` frequently to catch errors early
- Use `--strict` flag to catch security issues early
- Test incrementally: add one feature at a time

## Getting Help

If you encounter an error not documented here, search this page (Ctrl+F / Cmd+F) for keywords, review workflow examples in the documentation, enable verbose mode with `gh aw compile --verbose`, or [report issues on GitHub](https://github.com/github/gh-aw/issues). See [Common Issues](/gh-aw/troubleshooting/common-issues/) for additional help.
