---
title: AgenticImport
description: AI-assisted workflow migration pattern for one-time adaptations between repositories
sidebar:
  order: 3
---

AgenticImport uses AI agents to migrate workflows between repositories with automatic adaptation. Unlike `gh aw add` or shared imports, it creates independent, customized copies without version tracking.

## When to Use

Use AgenticImport for one-time migrations requiring adaptation:

- **Repository forking** - Adapt workflows for different projects
- **Context-specific changes** - Substantial modifications for new environments
- **Cross-organization** - Copy workflows where shared imports aren't practical
- **Experimentation** - Fork workflows as automation starting points

**Don't use** for synchronized updates—use [Packaging & Distribution](/gh-aw/guides/packaging-imports/) with `gh aw add` instead.

## How It Works

The `create-agentic-agent` migrates workflows through these steps:

1. **Analyze source** - Read and understand the source workflow's purpose and logic
2. **Identify dependencies** - Detect repository-specific configuration, secrets, tools, and permissions
3. **Adapt configuration** - Modify frontmatter, imports, and instructions for target repository
4. **Create workflow** - Generate or update the workflow file in the target repository
5. **Validate** - Compile and suggest fixes for any issues

## Example Migration

The `release.md` workflow migration from `github/gh-aw` to `github/gh-aw-mcpg`:

```yaml wrap title="Prompt for create-agentic-agent"
Migrate the release.md workflow from github/gh-aw to this repository.

The workflow should:
- Keep the same release automation logic
- Adapt permissions and jobs for our repository structure
- Update any repository-specific references
- Ensure all imports and tools are compatible
```

The agent automatically handles repository names, paths, permissions, and tool configurations.

## Setup

Initialize workflow creation agents:

```bash
gh aw init
```

## AgenticImport vs Traditional Import

**Use AgenticImport for:**
- Complex adaptations (repository-specific config, tool changes)
- One-time migration without ongoing synchronization
- AI-assisted analysis and validation

**Use `gh aw add` for:**
- Synchronized updates across repositories
- Version management with semantic tags
- Minimal customization

**Use manual copy for:**
- Simple workflows without complex dependencies

## Advanced Patterns

### Merging Workflows

```markdown
Merge issue-triage from org/repo1 and label-management from org/repo2 
into a unified workflow handling both responsibilities.
```

### Splitting Workflows

```markdown
Split multi-responsibility workflow.md into:
- Issue triage (issue-triage.md)
- PR review (pr-review.md)
- Security scanning (security-scan.md)

Extract shared configuration into common imports.
```

### Cross-Organization

```markdown
Migrate ci-doctor from public-org/workflows to our private enterprise 
repository, adapting for internal MCP servers and security policies.
```

## Real-World Example

The `release.md` migration from `github/gh-aw` to `github/gh-aw-mcpg` preserved release automation logic while adapting permissions, repository references, and structure for the target environment.

## Best Practices

**Before Migration:**
- Review source workflow purpose and dependencies
- Identify required secrets, tools, and permissions
- Understand shared imports
- Plan necessary adaptations

**During Migration:**
- Provide target repository context
- Specify security policies and constraints
- Use clear, specific instructions
- Validate incrementally with `gh aw compile`

**After Migration:**
- Test in safe environment ([TrialOps](/gh-aw/guides/trialops/))
- Review and document changes
- Remove `source:` frontmatter field

## Authoring with AI

In GitHub Copilot Chat, you can use the unified **agentic-workflows** agent to help create, debug, and update workflows. Simply specify your intent:

```sh wrap
# Create a new workflow from an imported action
/agent agentic-workflows create migrate my-action to agentic workflow

# Debug compilation issues
/agent agentic-workflows debug why is my workflow not compiling?
```

The agent automatically routes your request to the appropriate specialized prompt based on your intent (create/debug/update/upgrade).

## Limitations

- **Point-in-time copies** - Not for continuous synchronization (use `gh aw add`)
- **Requires context** - Agent needs clear target repository instructions
- **Manual validation** - Review and test before production
- **Iteration needed** - Complex workflows may need refinement

## Related

- [Packaging & Distribution](/gh-aw/guides/packaging-imports/) - Shared workflows with version management
- [Custom Agents](/gh-aw/reference/custom-agents/) - Specialized workflow authoring
- [TrialOps](/gh-aw/guides/trialops/) - Safe workflow testing
- [Workflow Structure](/gh-aw/reference/workflow-structure/) - Format reference
