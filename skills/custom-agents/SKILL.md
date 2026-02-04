---
name: custom-agents
description: GitHub Custom Agent File Format
Brief description of agent's purpose
>                      # Multi-line description of agent's purpose
Repository-wide coding standards
Frontend development guidelines
Agent specializing in creating and improving README files
Specialized agent for writing comprehensive test suites
Agent specialized in performing code reviews
---


# GitHub Custom Agent File Format

This document describes the GitHub custom agent file format used by GitHub Copilot to provide repository-specific or task-specific instructions to AI coding agents.

## Overview

GitHub Copilot supports custom agent instructions through Markdown files with YAML frontmatter. These files allow you to define specialized agent behaviors, tool access, and custom workflows that tailor Copilot's behavior to your repository's needs.

## File Locations

Custom agent files can be placed in several locations, each serving a different scope:

### 1. Repository-wide Instructions
- **File**: `.github/copilot-instructions.md`
- **Scope**: Applies to all code generation in the repository
- **Use case**: General coding standards, security requirements, testing practices

### 2. Path-specific Instructions
- **Directory**: `.github/instructions/`
- **Pattern**: `*.instructions.md` (e.g., `frontend.instructions.md`, `backend.instructions.md`)
- **Scope**: Can target specific directories or file patterns using `applyTo` in frontmatter
- **Use case**: Framework-specific guidelines, component-specific rules

### 3. Custom Agent Profiles
- **Directory**: `.github/agents/` or `.github/copilot/instructions/`
- **Pattern**: `AGENTS.md`, `*.md` (e.g., `readme-creator.md`, `test-writer.md`)
- **Scope**: Defines specialized agents with specific capabilities and instructions
- **Use case**: Task-specific agents (documentation, testing, refactoring)

### 4. agentic workflow integration
- **Location**: Imported via `imports` field in workflow frontmatter
- **Pattern**: Any markdown files under `.github/agents/` directory
- **Scope**: Custom agent for specific agentic workflow execution
- **Use case**: Workflow-specific agent configuration
- **Important**: Only one agent file is allowed per workflow

## File Format

### Basic Structure

```markdown
# YAML frontmatter (configuration)
name: agent-name
description: Brief description of agent's purpose

# Markdown body (instructions)

Your natural language instructions for the agent go here.
```

### Complete YAML Frontmatter Schema

```yaml
# Required fields
name: agent-identifier              # Unique identifier for the agent

# Optional descriptive fields
description: >                      # Multi-line description of agent's purpose
  Agent specializing in specific tasks

# Optional instruction fields
prompt: |                          # Freeform instructions (alternative to markdown body)
  Your instructions here

# Optional tool configuration
tools:                             # List of allowed tools for this agent
  - createFile
  - editFiles
  - codeSearch
  - search

# Optional path targeting (for .instructions.md files)
applyTo:                          # Glob patterns for targeted files/directories
  - "src/frontend/**"
  - "**/*.tsx"

# Optional MCP server configuration (enterprise/org only)
mcp-server:                       # External MCP server configuration
  url: https://my-mcp-server.com
  api-key: ${{ secrets.MCPSERVER_API_KEY }}

# Optional settings
settings:                         # Custom runtime or connection settings
  key: value
```

## Field Descriptions

### Core Fields

#### name (string, required for agent profiles)
- Unique identifier for the custom agent
- Used to reference the agent in workflows or assignments
- Convention: lowercase with hyphens (e.g., `readme-creator`, `test-writer`)

#### description (string, optional)
- Human-friendly description of the agent's focus and behavior
- Helps users understand what the agent specializes in
- Can be multi-line using YAML's `>` or `|` syntax

#### prompt (string, optional)
- Alternative to using the markdown body for instructions
- Contains freeform natural language instructions
- Use YAML's `|` (literal) or `>` (folded) for multi-line content
- If both `prompt` and markdown body exist, they are typically combined

### Tool Configuration

#### tools (array of strings, optional)
- List of tools the agent is allowed to use
- If omitted or set to `["*"]`, agent has access to all available tools
- Tool names are **case-insensitive**
- Supports both GitHub's standard tool aliases and legacy naming conventions

**GitHub Standard Tool Aliases:**

GitHub Copilot defines a standardized set of tool aliases for custom agents:

- **`read`** - Access and read contents of files or code
- **`edit`** - Make changes in code files, apply edits or refactoring
- **`search`** - Search codebase for keywords, references, or patterns
- **`pr`** - Create, manage, or update pull requests
- **`issue`** - Create, manage, or update issues

**Legacy Tool Names:**

For backward compatibility, these legacy tool names are still supported:

- `createFile` - Create new files (use `edit` instead)
- `editFiles` - Modify existing files (use `edit` instead)
- `deleteFiles` - Remove files (use `edit` instead)
- `codeSearch` - Semantic code search (use `search` instead)
- `runCommand` - Execute shell commands
- `getFile` - Read file contents (use `read` instead)
- `listFiles` - List directory contents (use `read` instead)

**MCP Server Tool Prefixes:**

When using Model Context Protocol (MCP) servers, you can specify tools with server prefixes:
- Single tool: `my-mcp-server/tool-name`
- All tools from a server: `my-mcp-server/*`

**Examples:**

```yaml
# Using standard tool aliases
tools:
  - read
  - edit
  - search

# Enable all tools with wildcard
tools: ["*"]

# Using legacy names (still supported)
tools:
  - editFiles
  - createFile
  - search

# Mixed standard and MCP server tools
tools:
  - read
  - edit
  - github-mcp/create_issue
  - custom-mcp/*

# Empty list disables all tools
tools: []
```

### Path Targeting

#### applyTo (array of strings, optional)
- Only used in `.instructions.md` files
- Specifies glob patterns for files/directories these instructions apply to
- Supports wildcards: `*` (any characters), `**` (any directories)
- Multiple patterns can be specified

**Example:**
```yaml
applyTo:
  - "src/frontend/**/*.tsx"
  - "src/frontend/**/*.ts"
  - "components/**"
```

### Enterprise Features

#### mcp-server (object, optional)
- Configuration for external MCP (Model Context Protocol) servers
- Typically used in enterprise or organization settings
- Allows integration with custom tools and services

**Fields:**
- `url` (string): MCP server endpoint
- `api-key` (string): Authentication key (use GitHub secrets)

**Example:**
```yaml
mcp-server:
  url: https://internal-tools.company.com/mcp
  api-key: ${{ secrets.INTERNAL_MCP_KEY }}
```

#### settings (object, optional)
- Custom runtime or connection settings
- Key-value pairs for agent-specific configuration
- Format and available keys depend on the agent implementation

## Usage Patterns

### Pattern 1: Repository-wide Standards

**File:** `.github/copilot-instructions.md`

```markdown
description: Repository-wide coding standards

# Coding Standards

## Style Guide
- Use single quotes in JavaScript/TypeScript
- Follow ESLint configuration in `.eslintrc.json`
- Maximum line length: 100 characters

## Security
- Always set `httpOnly` and `secure` flags for cookies
- Validate all user input
- Use parameterized queries for database access

## Testing
- All new code must include Jest tests
- Aim for >80% code coverage
- Test edge cases and error conditions
```

### Pattern 2: Path-specific Instructions

**File:** `.github/instructions/frontend.instructions.md`

```markdown
description: Frontend development guidelines
applyTo:
  - "src/frontend/**"
  - "components/**"

# Frontend Development Guidelines

## Component Structure
- Use React functional components with hooks
- Prefer composition over inheritance
- Keep components small and focused (< 150 lines)

## Styling
- Use CSS Modules for component styles
- Follow BEM naming convention
- Use Tailwind utility classes where appropriate

## State Management
- Use React Context for global state
- Keep local state in components when possible
- Use reducers for complex state logic
```

### Pattern 3: Custom Agent Profile

**File:** `.github/agents/readme-creator.md`

```markdown
name: readme-creator
description: Agent specializing in creating and improving README files
tools:
  - read
  - edit
  - search

# README Creator Agent

You are a documentation specialist focused on creating clear, comprehensive README files.

## Responsibilities
- Create well-structured README.md files for projects
- Include all standard sections: Overview, Installation, Usage, Contributing
- Generate accurate code examples
- Ensure documentation is up-to-date with codebase

## Style Guidelines
- Use clear, concise language
- Include code examples with syntax highlighting
- Add badges for build status, coverage, version
- Organize with logical heading hierarchy
- Include a table of contents for long READMEs

## Quality Standards
- Verify all code examples are accurate
- Test installation instructions
- Ensure links are valid and working
- Check for proper Markdown formatting
```

### Pattern 4: Test Writer Agent

**File:** `.github/agents/test-writer.md`

```markdown
name: test-writer
description: Specialized agent for writing comprehensive test suites
tools:
  - read
  - edit
  - search

# Test Writer Agent

You specialize in creating comprehensive, well-structured test suites.

## Testing Framework
- Use Jest for JavaScript/TypeScript
- Follow AAA pattern: Arrange, Act, Assert
- Use descriptive test names: "should [expected behavior] when [condition]"

## Test Coverage
- Write unit tests for all public functions
- Create integration tests for API endpoints
- Add edge case tests (null, undefined, empty, boundary values)
- Test error conditions and exception handling

## Test Organization
- Group related tests with `describe` blocks
- Use `beforeEach` and `afterEach` for setup/teardown
- Keep tests independent and isolated
- Mock external dependencies

## Best Practices
- One assertion per test when possible
- Use test data builders for complex objects
- Avoid test interdependence
- Keep tests fast (< 1 second each)
```

### Pattern 5: Agentic Workflow Integration

**File:** `.github/workflows/code-review.md`

```markdown
on:
  pull_request:
    types: [opened, synchronize]
permissions:
  contents: read
  pull-requests: write
engine:
  id: copilot
  custom-agent: .github/agents/code-reviewer.md

# Automated Code Review

Review the pull request changes and provide constructive feedback.
```

**File:** `.github/agents/code-reviewer.md`

```markdown
name: code-reviewer
description: Agent specialized in performing code reviews
tools:
  - read
  - search

# Code Review Agent

You are an experienced code reviewer focused on code quality, security, and best practices.

## Review Checklist
- Code follows repository style guidelines
- Proper error handling is implemented
- Security best practices are followed
- Tests are included for new functionality
- Documentation is updated where needed
- No unnecessary complexity

## Feedback Style
- Be constructive and specific
- Explain the reasoning behind suggestions
- Prioritize issues (critical, important, minor)
- Acknowledge good patterns and improvements
- Provide code examples for suggestions
```

## Integration with gh-aw

The gh-aw (GitHub Agentic Workflows) tool supports custom agent files through the `imports` field in workflow frontmatter. Any markdown files under the `.github/agents/` directory are treated as custom agent files when imported.

### Configuration

```markdown
on: issues
engine:
  id: copilot
imports:
  - .github/agents/my-agent.md

# My Workflow

Instructions for the workflow...
```

### Supported Engines

Custom agent files are supported by the following engines:

1. **Copilot** - Uses `--agent <path>` flag to load custom agent file
2. **Claude** - Prepends agent file content to the workflow prompt
3. **Codex** - Prepends agent file content to the workflow prompt

### File Path Resolution

- Agent files are imported via the `imports` field
- Must be markdown files located under `.github/agents/` directory
- Only one agent file is allowed per workflow
- File is validated during workflow compilation
- Checkout step is automatically added if agent file is imported

### Example Workflow with Custom Agent

```markdown
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine:
  id: copilot
imports:
  - .github/agents/issue-triager.md
tools:
  github:
    allowed:
      - add_labels_to_issue
      - create_issue_comment

# Issue Triage Workflow

Analyze the issue and categorize it appropriately.
```

## Best Practices

### 1. Keep Instructions Focused
- Each agent should have a clear, specific purpose
- Avoid mixing unrelated concerns in a single agent
- Create multiple specialized agents rather than one general-purpose agent

### 2. Be Explicit and Specific
- Provide concrete examples of expected behavior
- Define clear success criteria
- Specify edge cases and error handling

### 3. Use Appropriate Scoping
- Repository-wide instructions for universal standards
- Path-specific instructions for framework or directory-specific rules
- Custom agents for task-specific workflows

### 4. Test Agent Behavior
- Verify agent follows instructions correctly
- Test with various input scenarios
- Iterate based on actual agent performance

### 5. Maintain and Update
- Keep instructions current with codebase changes
- Review and refine based on agent performance
- Remove outdated or conflicting instructions

### 6. Security Considerations
- Limit tool access to what's necessary
- Be cautious with file deletion permissions
- Use secrets for sensitive configuration
- Review agent actions regularly

## Common Patterns

### Documentation Agent
```yaml
name: documentation-specialist
description: Creates and maintains technical documentation
tools: [read, edit, search]
```

### Refactoring Agent
```yaml
name: code-refactorer
description: Improves code quality and structure
tools: [read, edit, search]
```

### Security Auditor
```yaml
name: security-auditor
description: Reviews code for security vulnerabilities
tools: [read, search]
```

### Migration Assistant
```yaml
name: migration-helper
description: Assists with framework or library migrations
tools: [read, edit, search]
```

### Issue Management Agent
```yaml
name: issue-manager
description: Manages GitHub issues and project tracking
tools: [read, issue]
```

### Pull Request Assistant
```yaml
name: pr-assistant
description: Assists with pull request creation and management
tools: [read, edit, pr]
```

## Troubleshooting

### Agent Not Following Instructions
- Make instructions more explicit and specific
- Provide concrete examples
- Break down complex instructions into steps
- Ensure instructions don't conflict

### Tool Access Issues
- Verify tools are listed in `tools` array
- Check agent has necessary permissions
- Ensure tools are available in the environment

### Path Targeting Not Working
- Verify glob patterns are correct
- Check file paths match patterns
- Ensure `applyTo` is only used in `.instructions.md` files

### Custom Agent File Not Found
- Verify agent file is imported in the `imports` field
- Ensure file exists and is committed under `.github/agents/` directory
- Confirm agent file path is correct in imports list
- Remember: only one agent file is allowed per workflow

## References

- [GitHub Copilot Custom Agents Configuration](https://docs.github.com/en/copilot/reference/copilot-custom-agents-configuration) - Official reference for custom agent configuration including tool aliases
- [GitHub Copilot Custom Instructions Documentation](https://docs.github.com/en/copilot/how-tos/configure-custom-instructions/add-repository-instructions)
- [About Custom Agents](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-custom-agents)
- [GitHub Blog: Custom Instructions Support](https://github.blog/changelog/2025-07-23-github-copilot-coding-agent-now-supports-instructions-md-custom-instructions/)
- [GitHub Blog: AGENTS.md Support](https://github.blog/changelog/2025-08-28-copilot-coding-agent-now-supports-agents-md-custom-instructions/)

## Examples in This Repository

The gh-aw repository uses custom agent files for performance engineering guides:

- `.github/copilot/instructions/ci-performance.md` - CI/CD optimization
- `.github/copilot/instructions/workflow-performance.md` - Workflow efficiency
- `.github/copilot/instructions/build-performance.md` - Build optimization
- `.github/copilot/instructions/cli-performance.md` - CLI performance

These files provide specialized guidance for performance engineering tasks and demonstrate the custom agent file format in practice.
