---
name: copilot-cli
description: GitHub Copilot CLI Integration
---


# GitHub Copilot CLI Integration

This file integrates with the GitHub Copilot CLI (`@github/copilot`) for agentic workflow execution. The Copilot CLI provides natural language processing capabilities and MCP (Model Context Protocol) server support.

## GitHub Copilot CLI Overview

The GitHub Copilot CLI is an experimental AI-powered command-line interface that can:
- Execute natural language prompts via `copilot --prompt "your instruction"`
- Support MCP servers for tool integration
- Generate code, documentation, and provide explanations
- Work with file directories and project contexts
- Integrate with GitHub API and repositories

## Installation and Setup

The easiest way to run the Copilot CLI is through the built-in `gh copilot` command:

```bash
gh copilot
```

The `gh copilot` command is built into the GitHub CLI and automatically downloads and manages the Copilot CLI installation. Just install the [GitHub CLI](https://cli.github.com/) and you're ready to go! If the Copilot CLI is not already installed, it will be downloaded to `~/.local/share/gh/copilot`.

**Alternative: Manual npm installation** (for advanced users):

```bash
npm install -g @github/copilot
```

**Environment Variables:**
- `GITHUB_TOKEN` or `COPILOT_GITHUB_TOKEN`: GitHub token for authentication
- `XDG_CONFIG_HOME`: Configuration directory (defaults to `/tmp/gh-aw/.copilot/`)
- `XDG_STATE_HOME`: State/cache directory (defaults to `/tmp/gh-aw/.copilot/`)

**Note**: The `COPILOT_CLI_TOKEN` environment variable is no longer supported as of v0.26+. Use `COPILOT_GITHUB_TOKEN` instead.

## Core Command Structure

### Basic Usage
```bash
copilot --prompt "your natural language instruction"
```

### Advanced Options
```bash
copilot --add-dir /path/to/project \
        --log-level debug \
        --log-dir /tmp/gh-aw/logs \
        --model gpt-5 \
        --prompt "instruction"
```

**Key Parameters:**
- `--add-dir`: Add directory context to the prompt
- `--log-level`: Set logging verbosity (debug, info, warn, error)
- `--log-dir`: Directory for log output
- `--model`: Specify AI model (if supported)
- `--prompt`: Natural language instruction (required to avoid interactive mode)

## MCP Server Configuration

Copilot CLI supports MCP servers via JSON configuration at `/tmp/gh-aw/.copilot/mcp-config.json`:

```json
{
  "mcpServers": {
    "github": {
      "type": "local",
      "command": "npx",
      "args": ["@github/github-mcp-server"]
    },
    "playwright": {
      "type": "local", 
      "command": "npx",
      "args": ["@playwright/mcp@latest", "--allowed-hosts", "example.com"]
    },
    "custom-server": {
      "type": "local",
      "command": "python",
      "args": ["-m", "my_server"],
      "env": {
        "API_KEY": "secret"
      }
    }
  }
}
```

**Server Types:**
- `local`: Local command execution (equivalent to `stdio` in other MCP configs)
- `http`: HTTP-based MCP server
- Built-in servers like GitHub are automatically available

## Log Parsing and Output

### Expected Log Format
Copilot CLI logs contain:
- Command execution traces
- Tool call information
- Code blocks with language annotations (```language)
- Error and warning messages
- Suggestions and responses

### Log Parsing Patterns
When parsing logs in `parse_copilot_log.cjs`:
- Look for command patterns: `copilot -p`, `github copilot`
- Extract code blocks between ``` markers
- Capture responses with `Suggestion:` or `Response:` prefixes
- Identify errors with `error:` and warnings with `warning:`
- Filter out timestamps and shell prompts

## Error Handling

### Common Error Patterns
- Authentication failures: Missing or invalid `GITHUB_TOKEN`
- MCP server connection issues
- Tool execution timeouts
- Invalid prompt formatting
- Directory permission issues

### Best Practices
- Always use `--prompt` parameter to avoid interactive blocking
- Set appropriate timeouts for long-running operations
- Validate MCP server configurations before execution
- Handle authentication errors gracefully
- Log detailed error information for debugging

## Integration with GitHub Agentic Workflows

### Engine Configuration
```yaml
engine: copilot
# or
engine:
  id: copilot
  version: latest
  model: gpt-5  # defaults to claude-sonnet-4 if not specified
```

### Tool Integration
- GitHub tools are built-in (don't add to MCP config)
- Playwright uses npx launcher instead of Docker
- Safe outputs use dedicated MCP server
- Custom tools require proper MCP server configuration

### Authentication
- Use `COPILOT_GITHUB_TOKEN` secret for GitHub token
- GitHub Actions default token is incompatible with Copilot CLI
- Must use Personal Access Token (PAT)
- Ensure token has appropriate permissions for repository access
- Token is passed via environment variables to CLI

**Note**: The `COPILOT_CLI_TOKEN` secret name is no longer supported as of v0.26+.

## Development Guidelines

### When Working with Copilot Engine Code
- Follow MCP server configuration patterns in `copilot_engine.go`
- Use "local" type instead of "stdio" for MCP servers
- Handle built-in tools (like GitHub) by skipping MCP configuration
- Ensure proper environment variable setup
- Test with various tool combinations

### Log Parser Development
- Parse both structured and unstructured log output
- Handle multi-line code blocks correctly
- Extract meaningful error and warning information
- Generate proper markdown for step summaries
- Account for CLI-specific output formats

### Testing Considerations
- Mock CLI responses for unit tests
- Test MCP configuration generation
- Validate log parsing with various output formats
- Ensure timeout handling works correctly
- Test authentication scenarios

## Command Examples

### Basic Code Generation
```bash
copilot --prompt "Generate a Python function to calculate fibonacci numbers"
```

### File Analysis
```bash
copilot --add-dir /project --prompt "Analyze the code structure and suggest improvements"
```

### GitHub Integration
```bash
copilot --add-dir /repo --prompt "Create an issue summarizing the recent changes"
```

### With Logging
```bash
copilot --add-dir /tmp/gh-aw \
        --log-level debug \
        --log-dir /tmp/gh-aw/logs \
        --prompt "Review the code and suggest optimizations"
```

## Security Considerations

- Validate all prompts before execution to prevent injection
- Restrict directory access using `--add-dir` carefully
- Ensure MCP servers are from trusted sources
- Log sensitive operations for audit trails
- Use least-privilege tokens for authentication
- Sanitize log output before displaying to users

## Troubleshooting

### CLI Not Found
When running locally, use the built-in `gh copilot` command which handles installation automatically:
```bash
gh copilot
```

If you installed manually via npm and need to troubleshoot:
- Verify npm global installation: `npm list -g @github/copilot`
- Check PATH includes npm global bin directory
- Try reinstalling: `npm uninstall -g @github/copilot && npm install -g @github/copilot`

To remove the gh copilot managed installation:
```bash
gh copilot --remove
```

### Authentication Issues
- **GitHub Actions Token Incompatibility**: The default `GITHUB_TOKEN` does NOT work with Copilot CLI
- Verify you're using a Personal Access Token in `COPILOT_GITHUB_TOKEN` secret
- Verify the token is associated with a Copilot-enabled GitHub account
- For GitHub Enterprise, contact admin for Copilot CLI token access

### MCP Server Issues
- Validate JSON configuration syntax
- Check server command availability
- Verify network connectivity for HTTP servers
- Review server logs for connection errors

### Performance Issues
- Reduce directory scope with targeted `--add-dir`
- Lower log level to reduce I/O overhead
- Set appropriate timeouts for operations
- Monitor token usage and rate limits
