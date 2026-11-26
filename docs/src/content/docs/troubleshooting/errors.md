---
title: Error Reference
description: Comprehensive reference of error messages in GitHub Agentic Workflows, including schema validation, compilation, and runtime errors with solutions.
sidebar:
  order: 100
---

This reference documents common error messages, organized by when they occur during the workflow lifecycle.

> [!TIP]
> When you mistype a frontmatter field, the compiler suggests a correction via fuzzy matching. Look for "Did you mean" hints in the output (e.g., `permisions` → `permissions`).

## Schema Validation Errors

Detected during compilation when frontmatter does not conform to the JSON schema.

| Error | Cause | Fix |
|-------|-------|-----|
| `frontmatter not properly closed` | Missing closing `---` delimiter | Enclose frontmatter between two `---` lines |
| `failed to parse frontmatter: ...` | Invalid YAML syntax | Check indentation (spaces, not tabs), colons followed by spaces, quoted special characters |
| `timeout-minutes must be an integer` | Wrong value type | Use the documented type — e.g., `timeout-minutes: 10`, not `"10"` |
| `Unknown property: ...` | Misspelled field name | Apply the "Did you mean" suggestion; see [Frontmatter Reference](/gh-aw/reference/frontmatter/) |
| `imports field must be an array of strings` | Wrong syntax for `imports:` | Use list form: `- shared/tools.md` |
| `multiple agent files found in imports: ...` | More than one agent file imported | Import only one file from `.github/agents/` per workflow |

## Compilation Errors

Raised when converting the `.md` workflow to its `.lock.yml`.

| Error | Cause | Fix |
|-------|-------|-----|
| `workflow file not found: ...` | Path is wrong or missing | Verify the file exists under `.github/workflows/`; run `gh aw compile` to compile all |
| `failed to resolve import '...'` | Import path or permissions | Confirm the file exists relative to repo root and is readable |
| `invalid workflowspec: must be owner/repo/path[@ref]` | Wrong remote import format | Use `owner/repo/path[@ref]` (e.g., `github/gh-aw/.github/workflows/shared/example.md@main`) |
| `section 'name' not found` | Referenced section missing | Internal processing issue — verify the section exists; report if persistent |

## Runtime Errors

Raised when the compiled workflow executes in GitHub Actions.

### Time Delta Errors

The `stop-after` and similar fields accept relative deltas (`+24h`, `+3d`, `+1d12h30m`) and absolute dates (`2025-12-31`, `December 31, 2025`).

| Error | Fix |
|-------|-----|
| `invalid time delta format: ...` | Use supported units: `h` (minimum), `d`, `w`, `mo` |
| `minute unit 'm' is not allowed for stop-after` | Convert minutes to hours, rounding up (e.g., `+2h` instead of `+90m`) |
| `time delta too large: ...` | Stay within: 12 months, 52 weeks, 365 days, 8760 hours |
| `duplicate unit '[unit]' in time delta` | Combine values for the same unit (e.g., `+3d` instead of `+1d2d`) |
| `unable to parse date-time: ...` | Use a supported format like `2025-12-31 23:59:59`, `December 31, 2025`, or `12/31/2025` |

### Other Runtime Errors

| Error | Fix |
|-------|-----|
| `jq not found in PATH` | Install `jq` — Ubuntu/Debian: `sudo apt-get install jq`; macOS: `brew install jq` |
| `authentication required` | Run `gh auth login`, or ensure `GITHUB_TOKEN` is available in Actions |

## Engine-Specific Errors

| Error | Fix |
|-------|-----|
| `manual-approval value must be a string` | Use a string: `manual-approval: "Approve deployment to production"` |
| `invalid frontmatter key 'triggers:'` | Use `on:` instead of `triggers:` to match standard GitHub Actions syntax — see [Triggers](/gh-aw/reference/triggers/) |
| `invalid on: section format` | Follow [GitHub Actions syntax](/gh-aw/reference/triggers/) (e.g., `on: push`, `on: { push: { branches: [main] } }`) |

## File Processing Errors

| Error | Fix |
|-------|-----|
| `failed to read file ...` | Verify the file exists, is readable, and the disk is not full |
| `failed to create .github/workflows directory` | Check filesystem permissions and disk space |
| `workflow file '...' already exists. Use --force to overwrite` | Re-run with `--force` (e.g., `gh aw init my-workflow --force`) |

## MCP Configuration Errors

| Error | Fix |
|-------|-----|
| `failed to parse existing mcp.json: ...` | Validate JSON (`cat .github/mcp.json \| jq .`) or delete to regenerate |
| `failed to marshal mcp.json: ...` | Internal error — report with reproduction steps |
| `http MCP tool '...' missing required 'url' field` | Add `url:` to the HTTP MCP server configuration |
| `unable to determine MCP type for tool '...'` | Specify at least one of `type`, `url`, `command`, or `container` |
| `tool '...' mcp configuration cannot specify both 'container' and 'command'` | Use either `container:` or `command:`, not both |
| `tool '...' mcp configuration with type 'http' cannot use 'container' field` | Remove `container:` from HTTP MCP servers (only valid for stdio) |

## Strict Mode Errors

Strict mode is the default. To opt out, use `gh aw compile` without `--strict` and avoid `strict: false` in frontmatter — see [Strict Mode](/gh-aw/reference/frontmatter/#strict-mode-strict).

| Error | Fix |
|-------|-----|
| `'network' configuration is required` | Add `network: defaults`, explicit allowed domains, or `network: {}` to deny all |
| `write permission 'contents: write' is not allowed` | Use [safe outputs](/gh-aw/reference/safe-outputs/) (e.g., `create-issue`, `create-pull-request`) instead of write permissions |
| `wildcard '*' is not allowed in network.allowed domains` | Use specific domains, wildcard patterns (`*.cdn.example.com`), or ecosystem identifiers (`python`, `node`) |
| `custom MCP server '...' with container must have network configuration` | Add `network:` with allowed domains to containerized MCP servers |
| `engine does not support firewall` | Use an engine with firewall support (e.g., `copilot`), or remove `--strict` |
| `This workflow is running on a public repository but was not compiled with strict mode.` | Recompile with `gh aw compile --strict` |

## Safe Output & Workflow Errors

| Error | Fix |
|-------|-----|
| `cannot use 'command' with 'issues' in the same workflow` | Remove the conflicting event trigger — `command:` auto-handles these events. Use `events:` inside the command to restrict scope |
| `workflow uses safe-outputs.create-issue but repository ... does not have issues enabled` | Enable the feature in Settings → General → Features, or use a different safe output |
| `job name cannot be empty` | Internal error — report with your workflow file |

## Toolset Configuration

### Tool Not Found After Migrating to Toolsets

The tool may be in a different toolset, or you chose a narrower one. Check [GitHub Toolsets](/gh-aw/reference/github-tools/), run `gh aw mcp inspect <workflow>` to list available tools, then add the required toolset.

### Invalid Toolset Name

`invalid toolset: '...' is not a valid toolset` — valid names: `context`, `repos`, `issues`, `pull_requests`, `users`, `actions`, `code_security`, `discussions`, `labels`, `notifications`, `orgs`, `projects`, `gists`, `search`, `dependabot`, `experiments`, `secret_protection`, `security_advisories`, `stargazers`, `default`, `all`.

### Toolsets and Allowed Conflict

When both `toolsets:` and `allowed:` are specified, `allowed:` restricts tools to only those listed within the enabled toolsets. Prefer using only `toolsets:`:

```yaml wrap
# Recommended
tools:
  github:
    toolsets: [issues]

# Advanced: restrict within toolset
tools:
  github:
    toolsets: [issues]
    allowed: [create_issue]
```

### GitHub MCP Server Read-Only Enforcement

`GitHub MCP server read-only mode cannot be disabled` — the GitHub MCP server is always read-only. Remove `read-only: false` (or set it to `true`). Use [safe outputs](/gh-aw/reference/safe-outputs/) for write operations.

## Troubleshooting Tips

- Use `--verbose` for detailed error information
- Validate YAML syntax and file paths
- Consult the [Frontmatter Reference](/gh-aw/reference/frontmatter-full/)
- Compile frequently to catch errors early; use `--strict` to surface security issues
- Add features incrementally

## Getting Help

If your error isn't listed:

1. Re-run with `gh aw compile --verbose`
2. Search this page (Ctrl+F / Cmd+F) for keywords from the error
3. Use an agent with the [debug.md prompt](https://raw.githubusercontent.com/github/gh-aw/main/debug.md) to investigate failing runs
4. Review [workflow patterns](/gh-aw/patterns/issue-ops/), [Common Issues](/gh-aw/troubleshooting/common-issues/), and [Troubleshooting auth errors](/gh-aw/reference/auth/#troubleshooting-auth-errors)
5. [Report the issue on GitHub](https://github.com/github/gh-aw/issues)
