---
title: SpecOps
description: Maintain and propagate W3C-style specifications using agentic workflows
---

SpecOps is a pattern for maintaining formal specifications using agentic workflows. The `w3c-specification-writer` agent creates W3C-style specifications with RFC 2119 keywords (MUST, SHALL, SHOULD, MAY) and propagates changes to consuming implementations automatically.

Use SpecOps when formal specifications need to stay synchronized across multiple repositories.

## How SpecOps Works

The SpecOps workflow coordinates specification updates across multiple agents and repositories:

1. **User initiates specification change** - The user triggers a workflow with the `w3c-specification-writer` agent, describing what needs to change in the specification.

2. **Agent updates specification** - The coding agent modifies the specification document, applying RFC 2119 keywords (MUST, SHALL, SHOULD, MAY), updating version numbers, and adding change log entries. The user reviews and approves the changes.

3. **Propagation workflows activate** - When the specification changes merge to main, agentic workflows automatically detect the updates and analyze the impact on consuming repositories.

4. **Implementation plans generated** - Coding agents create plans to update implementations in consuming repositories (like [gh-aw-mcpg](https://github.com/githubnext/gh-aw-mcpg)) to comply with new specification requirements.

5. **Compliance tests updated** - Test generation workflows update compliance test suites to verify implementations satisfy the new specification requirements, ensuring continuous conformance.

This automated workflow ensures specifications remain the single source of truth while keeping all implementations synchronized and compliant.

## Update Specifications

Create a workflow to update specifications using the w3c-specification-writer agent:

```yaml
---
name: Update MCP Gateway Spec
on:
  workflow_dispatch:
    inputs:
      change_description:
        description: 'What needs to change in the spec?'
        required: true
        type: string

engine: copilot
strict: true

safe-outputs:
  create-pull-request:
    title-prefix: "[spec] "
    labels: [documentation, specification]
    draft: false

tools:
  edit:
  bash:
---

# Specification Update Workflow

You are using the w3c-specification-writer agent to update the
MCP Gateway specification.

**Change Request**: ${{ inputs.change_description }}

## Your Task

1. Review the current specification at 
   `docs/src/content/docs/reference/mcp-gateway.md`

2. Apply the requested changes following W3C conventions:
   - Use RFC 2119 keywords (MUST, SHALL, SHOULD, MAY)
   - Update version number appropriately (major/minor/patch)
   - Add entry to Change Log section
   - Update Status of This Document if needed

3. Ensure all changes maintain:
   - Clear conformance requirements
   - Testable specifications
   - Complete examples
   - Proper cross-references

4. Create a pull request with the updated specification
```

The agent applies RFC 2119 keywords, updates semantic versioning, adds change log entries, and creates a pull request.

## Propagate Changes

After specification updates merge, propagate changes to consuming repositories:

```yaml
---
name: Propagate Spec Changes
on:
  push:
    branches:
      - main
    paths:
      - 'docs/src/content/docs/reference/mcp-gateway.md'
  workflow_dispatch:

engine: copilot
strict: true

safe-outputs:
  create-pull-request:
    title-prefix: "[spec-update] "
    labels: [dependencies, specification]

tools:
  github:
    toolsets: [repos, pull_requests]
  edit:
  bash:
---

# Specification Propagation Workflow

The MCP Gateway specification has been updated. Propagate changes
to consuming repositories.

## Consuming Repositories

- **gh-aw-mcpg**: Implementation repository
  - Update compliance with new requirements
  - Adjust configuration schemas
  - Update integration tests
  
- **gh-aw**: Main repository  
  - Update MCP gateway configuration validation
  - Adjust workflow compilation if needed
  - Update documentation links

## Your Task

1. Read the latest specification version and change log
2. Identify breaking changes and new requirements
3. For each consuming repository:
   - Clone or fetch latest code
   - Update implementation to match spec
   - Run tests to verify compliance
   - Create pull request with changes
4. Create tracking issue linking all PRs
```

This workflow updates consuming repositories (like [gh-aw-mcpg](https://github.com/githubnext/gh-aw-mcpg)) to maintain spec compliance.

## Specification Structure

W3C-style specifications include:
**Required Sections**:
- Abstract, Status, Introduction, Conformance
- Numbered technical sections with RFC 2119 keywords
- Compliance testing, References, Change log

**Example**:
```markdown
## 3. Gateway Configuration

The gateway MUST validate all configuration fields before startup.
The gateway SHOULD log validation errors with field names.
The gateway MAY cache validated configurations.
```

## Semantic Versioning

- **Major (X.0.0)** - Breaking changes
- **Minor (0.Y.0)** - New features, backward-compatible
- **Patch (0.0.Z)** - Bug fixes, clarifications

## Propagation Strategies

**Push-Based**: Specification repository triggers updates when changes merge to main.

**Pull-Based**: Consumers check for updates on schedule (e.g., weekly).

**Hybrid**: Push for breaking changes, pull for minor updates.

## Best Practices

**Testable Requirements**: Write specifications with clear pass/fail criteria.

**Version Discipline**: Follow semantic versioning strictly for breaking changes.

**Track Consumers**: Maintain list of repositories implementing the specification.

**Compliance Checks**: Run automated tests to verify conformance regularly.

## Example: MCP Gateway

The [MCP Gateway Specification](/gh-aw/reference/mcp-gateway/) demonstrates SpecOps:
- **Specification**: Formal W3C document with RFC 2119 keywords
- **Implementation**: [gh-aw-mcpg](https://github.com/githubnext/gh-aw-mcpg) repository
- **Maintenance**: Automated pattern extraction via `layout-spec-maintainer` workflow

## Related Patterns

- **[MultiRepoOps](/gh-aw/guides/multirepoops/)** - Cross-repository coordination

## References

- [W3C Specification Writer Agent](https://github.com/githubnext/gh-aw/blob/main/.github/agents/w3c-specification-writer.agent.md)
- [MCP Gateway Specification](/gh-aw/reference/mcp-gateway/)
- [RFC 2119: Requirement Level Keywords](https://www.ietf.org/rfc/rfc2119.txt)
