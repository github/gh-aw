---
title: Agent Imports
description: Learn how to import and reuse specialized AI agents from external repositories to enhance your workflows with expert-crafted instructions and behavior.
sidebar:
  order: 7
---

GitHub Agentic Workflows supports importing agent files from external repositories, enabling you to reuse expert-crafted AI instructions and specialized behavior across teams and projects.

## Why Import Agents?

Agent files provide specialized AI instructions and behavior that can be shared and versioned like code. Importing agents from external repositories enables:

- **Reuse expertise**: Leverage specialized agents created by experts
- **Consistency**: Ensure all teams use the same agent behavior
- **Versioning**: Pin to stable agent versions with semantic tags
- **Updates**: Update agent once, affects all workflows using it
- **Modularity**: Mix and match agents with workflow components

## Basic Example

### Importing a Code Review Agent

Import a specialized code review agent from an external repository:

```yaml wrap title=".github/workflows/pr-review.md"
---
on: pull_request
engine: copilot
imports:
  - acme-org/shared-agents/.github/agents/code-reviewer.md@v1.0.0
permissions:
  contents: read
  pull-requests: write
---

# Automated Code Review

Review this pull request for:
- Code quality and best practices
- Security vulnerabilities
- Performance issues
```

The agent file in `acme-org/shared-agents/.github/agents/code-reviewer.md` contains specialized instructions:

```markdown title="acme-org/shared-agents/.github/agents/code-reviewer.md"
---
name: Expert Code Reviewer
description: Specialized agent for comprehensive code review
tools:
  github:
    toolsets: [pull_requests, repos]
---

# Code Review Instructions

You are an expert code reviewer with deep knowledge of:
- Security best practices (OWASP Top 10)
- Performance optimization patterns
- Code maintainability and readability

When reviewing code:
1. Identify security vulnerabilities first
2. Check for performance issues
3. Ensure code follows team conventions
4. Suggest specific improvements with examples
```

## Versioning Agents

Use semantic versioning to control agent updates:

```yaml wrap
imports:
  # Production - pin to specific version
  - acme-org/ai-agents/.github/agents/security-auditor.md@v2.0.0
  
  # Development - use latest
  - acme-org/ai-agents/.github/agents/performance.md@main
  
  # Immutable - pin to commit SHA
  - acme-org/ai-agents/.github/agents/custom.md@abc123def
```

## Agent Collections

Organizations can create libraries of specialized agents:

```text
acme-org/ai-agents/
└── .github/
    └── agents/
        ├── code-reviewer.md         # General code review
        ├── security-auditor.md      # Security-focused analysis
        ├── performance-analyst.md   # Performance optimization
        ├── accessibility-checker.md # WCAG compliance
        └── documentation-writer.md  # Technical documentation
```

Teams import agents based on workflow needs:

```yaml wrap title="Security-focused PR review"
---
on: pull_request
engine: copilot
imports:
  - acme-org/ai-agents/.github/agents/security-auditor.md@v2.0.0
  - acme-org/ai-agents/.github/agents/code-reviewer.md@v1.5.0
---

# Security Review

Perform comprehensive security review of this pull request.
```

```yaml wrap title="Accessibility-focused PR review"
---
on: pull_request
engine: copilot
imports:
  - acme-org/ai-agents/.github/agents/accessibility-checker.md@v1.0.0
---

# Accessibility Review

Check this pull request for WCAG 2.1 compliance issues.
```

## Combining Agents with Other Imports

Mix agent imports with tool configurations and shared components:

```yaml wrap
---
on: pull_request
engine: copilot
imports:
  # Import specialized agent
  - acme-org/ai-agents/.github/agents/security-auditor.md@v2.0.0
  
  # Import tool configurations
  - acme-org/workflow-library/shared/tools/github-standard.md@v1.0.0
  
  # Import MCP servers
  - acme-org/workflow-library/shared/mcp/database.md@v1.0.0
  
  # Import security policies
  - acme-org/workflow-library/shared/config/security-policies.md@v1.0.0
permissions:
  contents: read
  pull-requests: write
safe-outputs:
  create-pull-request-review-comment:
    max: 10
---

# Comprehensive Security Review

Perform detailed security analysis using specialized agent and tools.
```

## Caching and Offline Compilation

Remote agent imports are cached in `.github/aw/imports/` by commit SHA:

- **First compilation**: Downloads and caches the agent file
- **Subsequent compilations**: Uses cached file (works offline)
- **Cache sharing**: Same commit SHA across different refs shares cache
- **Version updates**: New versions download fresh agent files

## Best Practices

### For Agent Authors

When creating shareable agents:

1. **Use semantic versioning**: Tag releases with `v1.0.0`, `v1.1.0`, etc.
2. **Document capabilities**: Include clear frontmatter describing the agent
3. **Test thoroughly**: Validate agent behavior across different scenarios
4. **Maintain backwards compatibility**: Avoid breaking changes within major versions
5. **Provide examples**: Include usage examples in the repository

### For Agent Users

When importing agents:

1. **Pin production to versions**: Use `@v1.0.0` for stable production workflows
2. **Test updates first**: Try new versions in non-production workflows
3. **Use branches for development**: Import from `@main` during active development
4. **Review agent code**: Understand what the agent does before importing
5. **Monitor updates**: Subscribe to releases for agent repositories

## Constraints

- **One agent per workflow**: Only one agent file can be imported per workflow
- **Agent path detection**: Files in `.github/agents/` are automatically recognized
- **Local or remote**: Can import from local `.github/agents/` or remote repositories
- **Same format**: Remote and local agents use identical file format

## Related Documentation

- [Custom Agents Reference](/gh-aw/reference/copilot-custom-agents/) - Agent file format and requirements
- [Imports Reference](/gh-aw/reference/imports/) - Complete import system documentation
- [Packaging & Distribution](/gh-aw/guides/packaging-imports/) - Managing workflow imports
- [Frontmatter](/gh-aw/reference/frontmatter/) - Configuration options reference
