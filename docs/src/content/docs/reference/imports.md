---
title: Imports
description: Learn how to modularize and reuse workflow components across multiple workflows using the imports field in frontmatter for better organization and maintainability.
sidebar:
  order: 325
---

Using imports in frontmatter or markdown allows you to modularize and reuse workflow components across multiple workflows.

## Syntax

Imports can be specified either in frontmatter or in markdown. In frontmatter the `imports:` field is used:

```aw wrap
---
on: issues
engine: copilot
imports:
  - shared/common-tools.md
  - shared/mcp/tavily.md
---

# Your Workflow

Workflow instructions here...
```

In markdown, use the special `{{#import ...}}` directive:

```aw wrap
---
...
---

# Your Workflow

Workflow instructions here...

{{#import shared/common-tools.md}}
```

## Shared Workflow Components

Workflows without an `on` field are shared workflow components. These files are validated but not compiled into GitHub Actions - they're meant to be imported by other workflows. The compiler skips them with an informative message, allowing you to organize reusable components without generating unnecessary lock files.

## Path Formats

Import paths support local files (`shared/file.md`, `../file.md`), remote repositories (`owner/repo/file.md@v1.0.0`), and section references (`file.md#SectionName`). Optional imports use `{{#import? file.md}}` syntax in markdown.

Paths are resolved relative to the importing file, with support for nested imports and circular import protection.

## Remote Repository Imports

Import shared components from external repositories using the `owner/repo/path@ref` format:

```aw wrap
---
on: issues
engine: copilot
imports:
  - acme-org/shared-workflows/mcp/tavily.md@v1.0.0
  - acme-org/shared-workflows/tools/github-setup.md@main
---

# Issue Triage Workflow

Analyze incoming issues using imported tools and configurations.
```

Version references support semantic tags (`@v1.0.0`), branch names (`@main`, `@develop`), or commit SHAs for immutable references. See [Packaging & Distribution](/gh-aw/guides/packaging-imports/) for installation and update workflows.

## Import Cache

Remote imports are cached in `.github/aw/imports/` to enable offline compilation. First compilation downloads and caches the import by commit SHA; subsequent compilations use the cached file. The cache is git-tracked with `.gitattributes` configured for conflict-free merges. Local imports are never cached.

## Agent Files

Import custom agent files to customize AI engine behavior. Agent files are markdown documents with specialized instructions that modify how the AI interprets and executes workflows. Agent files can be imported from local `.github/agents/` directories or from external repositories.

### Local Agent Imports

Import agent files from your repository's `.github/agents/` directory:

```yaml wrap
---
on: pull_request
engine: copilot
imports:
  - .github/agents/code-reviewer.md
---
```

### Remote Agent Imports

Import agent files from external repositories using the `owner/repo/path@ref` format:

```yaml wrap
---
on: pull_request
engine: copilot
imports:
  - githubnext/shared-agents/.github/agents/security-reviewer.md@v1.0.0
---

# PR Security Review

Analyze pull requests for security vulnerabilities using the shared security reviewer agent.
```

Remote agent imports support the same versioning as other imports:
- Semantic tags: `@v1.0.0`, `@v2.1.3`
- Branch names: `@main`, `@develop`
- Commit SHAs: `@abc123def456` (immutable references)

### Constraints

- **One agent per workflow**: Only one agent file can be imported per workflow (local or remote)
- **Agent path detection**: Files in `.github/agents/` directories are automatically recognized as agent files
- **Caching**: Remote agents are cached in `.github/aw/imports/` by commit SHA, enabling offline compilation

## Frontmatter Merging

Imported files can define specific frontmatter fields that merge with the main workflow's configuration. The merge behavior varies by field type and follows specific precedence rules detailed below.

### Allowed Import Fields

Shared workflow files (without `on:` field) can define:
- `tools:` - Tool configurations (bash, web-fetch, github, mcp-*, etc.)
- `mcp-servers:` - Model Context Protocol server configurations
- `services:` - Docker services for workflow execution
- `safe-outputs:` - Safe output handlers and configuration
- `safe-inputs:` - Safe input configurations
- `network:` - Network permission specifications
- `permissions:` - GitHub Actions permissions (validated, not merged)
- `runtimes:` - Runtime version overrides (node, python, go, etc.)
- `secret-masking:` - Secret masking steps

Agent files (`.github/agents/*.md`) can additionally define:
- `name` - Agent name
- `description` - Agent description

Other fields in imported files generate warnings and are ignored.

### Merge Algorithm Overview

The compiler processes imports using **breadth-first search (BFS) traversal**. Direct imports are processed first, then their nested imports, preventing circular dependencies and ensuring deterministic ordering. Configurations accumulate during traversal and merge into the main workflow using field-specific rules.

### Field-Specific Merge Semantics

#### Tools (`tools:`)

Deep merge with array concatenation. New tool keys are added, duplicate keys trigger deep merge. `allowed` arrays concatenate and deduplicate. MCP tools detect conflicts except for `allowed` arrays.

```aw wrap
# main.md tools.bash.allowed: [write]
# import tools.bash.allowed: [read, list]
# Result: [read, list, write]
```

#### MCP Servers (`mcp-servers:`)

Imported servers override main workflow servers with the same name. Main workflow servers not defined in imports are kept. Multiple imports defining the same server use first-wins ordering.

#### Network Permissions (`network:`)

Union of `allowed` domains, deduplicated and sorted alphabetically. Network `mode` and `firewall` from main workflow take precedence.

#### Permissions (`permissions:`)

Validation only - imported permissions are not merged. Main workflow must explicitly declare all imported permissions with sufficient levels (`write` >= `read` >= `none`). Missing or insufficient permissions fail compilation.

#### Safe Outputs (`safe-outputs:`)

Each safe-output type can be defined once across all imports. Main workflow definitions override imported definitions for the same type. Multiple imports defining the same type fail compilation. Meta fields use first-wins merging (main > imports).

#### Runtimes (`runtimes:`)

Main workflow runtime versions override imported versions. Imported runtimes are used if not specified in main workflow.

#### Services (`services:`)

Service names must be unique across main and imports. Duplicate service names fail compilation. All services are available to workflow jobs.

#### Steps (`steps:`)

Imported steps are prepended to main workflow steps (imported first, then main). Action pinning applies to all steps. Steps from multiple imports concatenate in import order.

#### Jobs (`jobs:`)

The `jobs:` field in imported files is not merged. Custom jobs can only be defined in the main workflow's frontmatter. Use `safe-outputs.jobs` for importable job definitions.

#### Safe Output Jobs (`safe-outputs.jobs`)

Safe-job names must be unique across main workflow and all imports. Duplicate job names fail compilation. Job execution order is determined by `needs:` dependencies.

### Import Processing Order

Imports are processed in breadth-first order: direct imports first, then nested imports. Earlier imports in the main workflow's list take precedence. Circular imports are detected and prevented, ensuring deterministic results.

### Error Handling

**Circular imports**: Detected and prevented during compilation.

**Missing files**: Optional imports use `{{#import? file.md}}` to handle missing files gracefully. Required imports fail compilation if missing.

**Conflicts**: Multiple imports defining the same safe-output type fail compilation. Resolution: Define in main workflow (overrides imports) or remove from one import.

**Permission validation**: Insufficient permissions produce detailed error messages with suggested fixes.

### Performance Considerations

Remote imports are cached by commit SHA in `.github/aw/imports/`. Keep import chains shallow, use shared workflows for reusable configurations, and consolidate related imports. Every compilation records imports in the lock file manifest for dependency tracking.

## Best Practices

**Layer configurations by scope**: Create base configurations with core tools, then extend with specialized imports. Use nested imports to build layered configurations.

**Declare permissions explicitly**: Main workflows must explicitly declare all imported permissions - they are not automatically inherited.

**Use semantic versioning**: Reference stable versions (`@v2.1.0`) in production, use branch names (`@main`) in development.

**Flatten import chains**: Avoid deeply nested imports. Use direct imports to multiple shared files instead of chaining imports through multiple levels.

## Related Documentation

- [Packaging and Updating](/gh-aw/guides/packaging-imports/) - Complete guide to managing workflow imports
- [Frontmatter](/gh-aw/reference/frontmatter/) - Configuration options reference
- [MCPs](/gh-aw/guides/mcps/) - Model Context Protocol setup
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Safe output configuration details
- [Network Configuration](/gh-aw/reference/network/) - Network permission management
