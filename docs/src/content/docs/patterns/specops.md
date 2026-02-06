---
title: SpecOps
description: Maintain and propagate W3C-style specifications using agentic workflows
---

SpecOps is a pattern for maintaining formal specifications using agentic workflows. It leverages the [`w3c-specification-writer` agent](https://github.com/github/gh-aw/blob/main/.github/agents/w3c-specification-writer.agent.md) to create W3C-style specifications with RFC 2119 keywords (MUST, SHALL, SHOULD, MAY) and automatically propagates changes to consuming implementations.

Use SpecOps when you need to:
- Maintain formal technical specifications
- Keep specifications synchronized across multiple repositories
- Ensure implementations stay compliant with specification updates

## How SpecOps Works

SpecOps coordinates specification updates across agents and repositories:

1. **Update specification** - Trigger a workflow with the `w3c-specification-writer` agent to modify the specification document with RFC 2119 keywords, version numbers, and change log entries.

2. **Review changes** - Review and approve the specification changes in the pull request.

3. **Propagate automatically** - When merged to main, agentic workflows detect the updates and analyze impact on consuming repositories.

4. **Update implementations** - Coding agents create pull requests to update implementations in consuming repositories (like [gh-aw-mcpg](https://github.com/github/gh-aw-mcpg)) to comply with new requirements.

5. **Verify compliance** - Test generation workflows update compliance test suites to verify implementations satisfy the new specification requirements.

This automated workflow ensures specifications remain the single source of truth while keeping all implementations synchronized and compliant.

## Update Specifications

Create a workflow to update specifications using the [`w3c-specification-writer` agent](https://github.com/github/gh-aw/blob/main/.github/agents/w3c-specification-writer.agent.md):

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

tools:
  edit:
  bash:
---

# Specification Update Workflow

Update the MCP Gateway specification using the w3c-specification-writer agent.

**Change Request**: ${{ inputs.change_description }}

## Your Task

1. Review the current specification at `docs/src/content/docs/reference/mcp-gateway.md`

2. Apply the requested changes following W3C conventions:
   - Use RFC 2119 keywords (MUST, SHALL, SHOULD, MAY)
   - Update version number (major/minor/patch)
   - Add entry to Change Log section
   - Update Status of This Document if needed

3. Ensure changes maintain clear conformance requirements, testable specifications, and complete examples

4. Create a pull request with the updated specification
```

The agent applies RFC 2119 keywords, updates semantic versioning, and creates a pull request.

## Propagate Changes

After specification updates merge, automatically propagate changes to consuming repositories:

```yaml
---
name: Propagate Spec Changes
on:
  push:
    branches:
      - main
    paths:
      - 'docs/src/content/docs/reference/mcp-gateway.md'

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

The MCP Gateway specification has been updated. Propagate changes to consuming repositories.

## Consuming Repositories

- **gh-aw-mcpg**: Update implementation compliance, schemas, and tests
- **gh-aw**: Update MCP gateway validation and documentation

## Your Task

1. Read the latest specification version and change log
2. Identify breaking changes and new requirements
3. For each consuming repository:
   - Update implementation to match spec
   - Run tests to verify compliance
   - Create pull request with changes
4. Create tracking issue linking all PRs
```

This workflow automatically updates consuming repositories like [gh-aw-mcpg](https://github.com/github/gh-aw-mcpg) to maintain compliance.

## Specification Structure

W3C-style specifications follow a formal structure:

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

See the [`w3c-specification-writer` agent](https://github.com/github/gh-aw/blob/main/.github/agents/w3c-specification-writer.agent.md) for complete specification template and guidelines.

## Semantic Versioning

- **Major (X.0.0)** - Breaking changes
- **Minor (0.Y.0)** - New features, backward-compatible
- **Patch (0.0.Z)** - Bug fixes, clarifications

## Best Practices

**Write Testable Requirements** - Create specifications with clear pass/fail criteria.

**Follow Version Discipline** - Use semantic versioning strictly for breaking changes.

**Track Consumers** - Maintain a list of repositories implementing the specification.

**Automate Compliance** - Run automated tests to verify conformance regularly.

## Example: MCP Gateway

The [MCP Gateway Specification](/gh-aw/reference/mcp-gateway/) demonstrates SpecOps:
- **Specification**: Formal W3C document with RFC 2119 keywords
- **Implementation**: [gh-aw-mcpg](https://github.com/github/gh-aw-mcpg) repository
- **Maintenance**: Automated pattern extraction via `layout-spec-maintainer` workflow

## Related Patterns

- **[MultiRepoOps](/gh-aw/patterns/multirepoops/)** - Cross-repository coordination

## References

- [W3C Specification Writer Agent](https://github.com/github/gh-aw/blob/main/.github/agents/w3c-specification-writer.agent.md) - Custom agent for writing W3C-style specifications
- [MCP Gateway Specification](/gh-aw/reference/mcp-gateway/) - Example specification
- [RFC 2119: Requirement Level Keywords](https://www.ietf.org/rfc/rfc2119.txt) - Keywords for technical specifications
