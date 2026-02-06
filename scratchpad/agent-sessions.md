# Agent Sessions Terminology Migration Plan

## Overview

This document outlines the plan to migrate from the old terminology "agent session" to the new terminology "agent session" throughout the GitHub Agentic Workflows codebase.

**Status**: Planning Phase  
**Target**: Complete terminology update to "agent session"  
**Scope**: Code, documentation, schemas, tests, and codemods

---

## Background

The terminology for starting an agent is changing from "New Agent Task" to "New Agent Session". This requires a coordinated update across:

1. **Code**: Go source files with types, variables, functions
2. **JavaScript**: CommonJS action files
3. **Schema**: JSON schema definitions
4. **Documentation**: Markdown files in docs/ and scratchpad/
5. **Skills**: Skill documentation
6. **Codemods**: Migration scripts for user workflows
7. **Tests**: Test files and fixtures

---

## Current State Analysis

### Files Containing "agent session" (case-insensitive)

**Go Files (Primary Code)**:
- `pkg/workflow/create_agent_task.go` - Core implementation
- `pkg/workflow/create_agent_task_test.go` - Unit tests
- `pkg/workflow/create_agent_task_integration_test.go` - Integration tests
- `pkg/workflow/safe_outputs_config.go` - Configuration parsing
- `pkg/workflow/safe_outputs_config_helpers_reflection.go` - Reflection helpers
- `pkg/workflow/safe_outputs_config_generation.go` - Code generation
- `pkg/workflow/compiler_safe_outputs_job.go` - Job compilation
- `pkg/workflow/compiler_safe_outputs_specialized.go` - Specialized compilation
- `pkg/workflow/tool_description_enhancer.go` - Tool descriptions
- `pkg/workflow/github_token.go` - Token handling
- `pkg/workflow/safe_outputs_tools_test.go` - Tool tests
- `pkg/workflow/safe_outputs_steps.go` - Step generation
- `pkg/workflow/safe_outputs_test.go` - Safe outputs tests
- `pkg/workflow/safe_outputs_integration_test.go` - Integration tests
- `pkg/workflow/imports.go` - Import handling
- `pkg/workflow/compiler_types.go` - Type definitions
- `pkg/workflow/checkout_persist_credentials_test.go` - Test references
- `pkg/cli/copilot_agent.go` - Agent detection
- `pkg/cli/copilot_agent_test.go` - Agent detection tests
- `pkg/cli/interactive.go` - Interactive mode
- `pkg/cli/commands.go` - CLI commands
- `pkg/cli/tokens_bootstrap.go` - Token setup

**JavaScript Files**:
- `actions/setup/js/create_agent_task.cjs` - Action implementation
- `actions/setup/js/create_agent_task.test.cjs` - Action tests
- `actions/setup/js/safe_outputs_tools.json` - Tool definitions
- `pkg/workflow/js/safe_outputs_tools.json` - Tool definitions (copy)

**Schema Files**:
- `pkg/parser/schemas/main_workflow_schema.json` - Main schema definition

**Documentation Files**:
- `skills/gh-agent-task/SKILL.md` - Skill documentation
- `docs/src/content/docs/reference/safe-outputs.md` - Safe outputs reference
- `docs/src/content/docs/reference/frontmatter-full.md` - Frontmatter reference
- `docs/src/content/docs/reference/tokens.md` - Token reference
- `docs/src/content/docs/patterns/multirepoops.md` - Multi-repo guide
- `docs/src/content/docs/examples/multi-repo.md` - Multi-repo example
- `pkg/cli/templates/github-agentic-workflows.md` - Template
- `AGENTS.md` - Main agents documentation
- `CHANGELOG.md` - Changelog references
- `install.md` - Installation guide

**Specs Files**:
- `scratchpad/code-organization.md` - Code organization patterns
- `scratchpad/layout.md` - Layout documentation
- `scratchpad/safe-output-environment-variables.md` - Environment variables
- `scratchpad/security_review.md` - Security review
- `scratchpad/template-injection-prevention.md` - Security patterns

**Test Workflows**:
- `pkg/cli/workflows/test-copilot-create-agent-session.md` - Test workflow

### Key Terminology Patterns

**Configuration Keys** (in YAML/JSON):
- `create-agent-session` → `create-agent-session`
- `GITHUB_AW_AGENT_TASK_BASE` → `GITHUB_AW_AGENT_SESSION_BASE`

**Go Type Names**:
- `CreateAgentSessionConfig` → `CreateAgentSessionConfig`
- `CreateAgentSessions` → `CreateAgentSessions`
- `parseAgentTaskConfig` → `parseAgentSessionConfig`
- `buildCreateOutputAgentTaskJob` → `buildCreateOutputAgentSessionJob`
- `createAgentTaskLog` → `createAgentSessionLog`

**JavaScript Variables**:
- `createAgentTaskItems` → `createAgentSessionItems`
- `create_agent_task` → `create_agent_session`
- `task_number` → `session_number`
- `task_url` → `session_url`

**File Names**:
- `create_agent_task.go` → `create_agent_session.go`
- `create_agent_task_test.go` → `create_agent_session_test.go`
- `create_agent_task.cjs` → `create_agent_session.cjs`
- `create_agent_task.test.cjs` → `create_agent_session_test.cjs`
- `test-copilot-create-agent-session.md` → `test-copilot-create-agent-session.md`

**CLI Commands**:
- `gh agent-task create` → Keeping this (external CLI tool, not part of gh-aw)

**User-Facing Terms**:
- "Agent Task Creation" → "Agent Session Creation"
- "Create Agent Task" → "Create Agent Session"
- "agent session" → "agent session"

---

## Migration Strategy

### Phase 1: Schema and Codemod (Breaking Change)

Since the configuration key `create-agent-session` is changing to `create-agent-session`, this is a **breaking change** that requires:

1. **Update JSON Schema**
   - Add `create-agent-session` to schema
   - Mark `create-agent-session` as deprecated (if possible)
   - Update descriptions and examples

2. **Create Codemod**
   - Add new codemod to `pkg/cli/fix_codemods.go`
   - Codemod ID: `agent-task-to-agent-session-migration`
   - Transform: `create-agent-session` → `create-agent-session`
   - Preserve formatting, comments, and nested properties

3. **Update Compiler**
   - Support both old and new keys during transition
   - Parse `create-agent-session` first
   - Fall back to `create-agent-session` with deprecation warning
   - Plan full removal in future version

### Phase 2: Go Code Migration

1. **Rename Core Files**
   ```
   pkg/workflow/create_agent_task.go → create_agent_session.go
   pkg/workflow/create_agent_task_test.go → create_agent_session_test.go
   pkg/workflow/create_agent_task_integration_test.go → create_agent_session_integration_test.go
   ```

2. **Update Type Names**
   - `CreateAgentSessionConfig` → `CreateAgentSessionConfig`
   - Update all references in dependent files

3. **Update Function Names**
   - `parseAgentTaskConfig` → `parseAgentSessionConfig`
   - `buildCreateOutputAgentTaskJob` → `buildCreateOutputAgentSessionJob`

4. **Update Variable Names**
   - `agentTaskConfig` → `agentSessionConfig`
   - `CreateAgentSessions` → `CreateAgentSessions`

5. **Update Logger Names**
   - `createAgentTaskLog` → `createAgentSessionLog`
   - `workflow:create_agent_task` → `workflow:create_agent_session`

6. **Update Environment Variables**
   - `GITHUB_AW_AGENT_TASK_BASE` → `GITHUB_AW_AGENT_SESSION_BASE`

### Phase 3: JavaScript Migration

1. **Rename Files**
   ```
   actions/setup/js/create_agent_task.cjs → create_agent_session.cjs
   actions/setup/js/create_agent_task.test.cjs → create_agent_session.test.cjs
   ```

2. **Update Variable Names**
   - `createAgentTaskItems` → `createAgentSessionItems`
   - `create_agent_task` → `create_agent_session`

3. **Update Output Names**
   - `task_number` → `session_number`
   - `task_url` → `session_url`

4. **Update safe_outputs_tools.json**
   - Update tool type from `create_agent_task` to `create_agent_session`
   - Update descriptions and examples

### Phase 4: Documentation Migration

1. **Update Skills**
   - Rename `skills/gh-agent-task/` to `skills/gh-agent-session/`
   - Update SKILL.md content throughout

2. **Update Reference Docs**
   - `docs/src/content/docs/reference/safe-outputs.md`
   - `docs/src/content/docs/reference/frontmatter-full.md`
   - `docs/src/content/docs/reference/tokens.md`

3. **Update Guides and Examples**
   - `docs/src/content/docs/patterns/multirepoops.md`
   - `docs/src/content/docs/examples/multi-repo.md`

4. **Update Root Documentation**
   - `AGENTS.md`
   - `install.md`
   - `CHANGELOG.md`

5. **Update Specs**
   - All files in `scratchpad/` directory

### Phase 5: Test Migration

1. **Update Test Workflows**
   - Rename `test-copilot-create-agent-session.md` → `test-copilot-create-agent-session.md`
   - Update workflow content

2. **Update Test Files**
   - All `*_test.go` files with agent session references

3. **Update Test Fixtures**
   - Any test data or fixtures referencing agent sessions

### Phase 6: Backward Compatibility

During transition period:

1. **Support Both Terms**
   - Accept both `create-agent-session` and `create-agent-session` in schema
   - Parse both in compiler
   - Emit deprecation warning for old term

2. **Migration Path**
   - Users run `gh aw fix` to apply codemod
   - Codemod updates their workflows automatically

3. **Removal Timeline**
   - Deprecation: v0.X (next version)
   - Warning period: 2-3 versions
   - Removal: v0.X+3

---

## Implementation Checklist

### Schema Changes
- [ ] Update `pkg/parser/schemas/main_workflow_schema.json`
  - [ ] Add `create-agent-session` property
  - [ ] Mark `create-agent-session` as deprecated (with description note)
  - [ ] Update examples to use new terminology
  - [ ] Update `$comment` field listing operations

### Codemod Implementation
- [ ] Add codemod to `pkg/cli/fix_codemods.go`
  - [ ] Implement `getAgentTaskToAgentSessionCodemod()`
  - [ ] Add test cases in `fix_command_test.go`
  - [ ] Test on real workflow examples

### Go Code Changes
- [ ] Rename files
  - [ ] `create_agent_task.go` → `create_agent_session.go`
  - [ ] `create_agent_task_test.go` → `create_agent_session_test.go`
  - [ ] `create_agent_task_integration_test.go` → `create_agent_session_integration_test.go`
  
- [ ] Update type definitions
  - [ ] `CreateAgentSessionConfig` → `CreateAgentSessionConfig`
  - [ ] Update all struct field names in `SafeOutputsConfig`
  
- [ ] Update function names
  - [ ] `parseAgentTaskConfig` → `parseAgentSessionConfig`
  - [ ] `buildCreateOutputAgentTaskJob` → `buildCreateOutputAgentSessionJob`
  
- [ ] Update variable names
  - [ ] All local variables: `agentTaskConfig` → `agentSessionConfig`
  - [ ] All references to `CreateAgentSessions` → `CreateAgentSessions`
  
- [ ] Update logger
  - [ ] `createAgentTaskLog` → `createAgentSessionLog`
  - [ ] Logger category: `workflow:create_agent_session`
  
- [ ] Update environment variables
  - [ ] `GITHUB_AW_AGENT_TASK_BASE` → `GITHUB_AW_AGENT_SESSION_BASE`
  
- [ ] Add backward compatibility
  - [ ] Parse both `create-agent-session` and `create-agent-session`
  - [ ] Emit deprecation warning for old key
  
- [ ] Update all importing files
  - [ ] `safe_outputs_config.go`
  - [ ] `safe_outputs_config_helpers_reflection.go`
  - [ ] `safe_outputs_config_generation.go`
  - [ ] `compiler_safe_outputs_job.go`
  - [ ] `compiler_safe_outputs_specialized.go`
  - [ ] `tool_description_enhancer.go`
  - [ ] `github_token.go`
  - [ ] `safe_outputs_steps.go`
  - [ ] `imports.go`
  - [ ] `compiler_types.go`

### JavaScript Changes
- [ ] Rename files
  - [ ] `create_agent_task.cjs` → `create_agent_session.cjs`
  - [ ] `create_agent_task.test.cjs` → `create_agent_session.test.cjs`
  
- [ ] Update variable names in JavaScript
  - [ ] `createAgentTaskItems` → `createAgentSessionItems`
  - [ ] Type check: `create_agent_task` → `create_agent_session`
  
- [ ] Update output names
  - [ ] `task_number` → `session_number`
  - [ ] `task_url` → `session_url`
  
- [ ] Update comments and strings
  - [ ] User-facing messages: "agent session" → "agent session"
  - [ ] "Create Agent Task" → "Create Agent Session"
  
- [ ] Update tool definitions
  - [ ] `actions/setup/js/safe_outputs_tools.json`
  - [ ] `pkg/workflow/js/safe_outputs_tools.json`

### Documentation Changes
- [ ] Update Skills
  - [ ] Rename directory: `skills/gh-agent-task/` → `skills/gh-agent-session/`
  - [ ] Update `SKILL.md` content throughout
  
- [ ] Update Reference Documentation
  - [ ] `docs/src/content/docs/reference/safe-outputs.md`
    - [ ] Update section title: "Agent Task Creation" → "Agent Session Creation"
    - [ ] Update all text references
    - [ ] Update code examples
  - [ ] `docs/src/content/docs/reference/frontmatter-full.md`
  - [ ] `docs/src/content/docs/reference/tokens.md`
  
- [ ] Update Guides
  - [ ] `docs/src/content/docs/patterns/multirepoops.md`
  
- [ ] Update Examples
  - [ ] `docs/src/content/docs/examples/multi-repo.md`
  
- [ ] Update Root Documentation
  - [ ] `AGENTS.md` - Update skill references
  - [ ] `install.md` - Update examples if any
  - [ ] `CHANGELOG.md` - Add migration entry
  
- [ ] Update Specs
  - [ ] `scratchpad/code-organization.md`
  - [ ] `scratchpad/layout.md`
  - [ ] `scratchpad/safe-output-environment-variables.md`
  - [ ] `scratchpad/security_review.md`
  - [ ] `scratchpad/template-injection-prevention.md`
  
- [ ] Update Templates
  - [ ] `pkg/cli/templates/github-agentic-workflows.md`

### Test Changes
- [ ] Rename test workflow
  - [ ] `test-copilot-create-agent-session.md` → `test-copilot-create-agent-session.md`
  - [ ] Update workflow content
  
- [ ] Update test files
  - [ ] All references in `*_test.go` files
  - [ ] Test fixtures and expected output
  
- [ ] Update CLI tests
  - [ ] `pkg/cli/copilot_agent_test.go`
  - [ ] Other CLI test files

### Build and Validation
- [ ] Run `make fmt` - Format all code
- [ ] Run `make lint` - Validate code quality
- [ ] Run `make test-unit` - Run unit tests
- [ ] Run `make test` - Run all tests
- [ ] Run `make recompile` - Recompile workflows
- [ ] Run `make agent-finish` - Final validation
- [ ] Test codemod on real workflows
- [ ] Verify backward compatibility

---

## Notes

### External Dependencies

The `gh agent-task` CLI extension is **external** to gh-aw (repository: `github/agent-task`). We should:
- Keep using the CLI as-is (no changes needed)
- Update our documentation to clarify it's creating "agent sessions"
- Note that the CLI command name may change in the future

### Breaking Change Communication

This is a **breaking change** for users with existing workflows using `create-agent-session`. Communication plan:

1. **Changeset**: Create changeset with `BREAKING CHANGE` prefix
2. **Release Notes**: Clearly document the migration path
3. **Migration Guide**: Link to codemod usage in docs
4. **Deprecation Warning**: Emit clear warning when old key is used

### Testing Priority

Focus testing on:
1. Codemod correctness (preserves formatting, handles edge cases)
2. Backward compatibility (both old and new keys work)
3. Deprecation warning (emitted correctly)
4. Integration tests (end-to-end workflow execution)

---

## References

- Issue: Terminology change from "agent session" to "agent session"
- Related: `gh agent-task` CLI tool (external, github/agent-task repo)
- Schema: `pkg/parser/schemas/main_workflow_schema.json`
- Codemods: `pkg/cli/fix_codemods.go`

---

**Last Updated**: 2026-01-07  
**Status**: Ready for Implementation
