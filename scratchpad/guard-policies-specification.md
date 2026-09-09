---
title: Guard Policies Integration Specification
description: Formal specification for the guard policies framework in the MCP Gateway
version: 0.1.0
status: Draft
sidebar:
  order: 1450
---

# Guard Policies Integration Proposal

## Executive Summary

This document proposes an extensible guard policies framework for the MCP Gateway, starting with GitHub-specific policies. Guard policies enable fine-grained access control at the MCP gateway level, restricting which repositories and operations AI agents can access through MCP servers.

**Version**: 0.1.0  
**Status**: Draft  
**Date**: 2026-06-21

## Requirements Notation

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and **OPTIONAL** in this document are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174).

## Problem Statement

The user requested support for guard policies in the MCP gateway configuration, with the following requirements:

1. Support GitHub-specific guard policies with flat frontmatter syntax:
   - `allowed-repos` (scope): Repository access patterns
   - `min-integrity` (minintegrity): Minimum min-integrity level required

2. Design an extensible system that can support future MCP servers (Jira, WorkIQ) with different policy schemas

3. Expose these parameters through workflow frontmatter fields

## Approach

### 1. Type Hierarchy

```
GitHubToolConfig (GitHub-specific)
  ├── Repos: GitHubReposScope (string or []any)
  └── MinIntegrity: GitHubIntegrityLevel (enum)

MCPServerConfig (general)
  └── GuardPolicies: map[string]any (extensible for all servers)
```

### 2. GitHub Guard Policy Schema

Based on the provided JSON schema, the implementation supports:

#### Repos Scope

- `"all"` - All repositories accessible by the token
- `"public"` - Public repositories only
- Array of patterns:
  - `"owner/repo"` - Exact repository match
  - `"owner/*"` - All repositories under owner
  - `"owner/prefix*"` - Repositories with name prefix under owner

**Integrity Levels:**

Integrity levels are based on the combination of the `author_association` field associated with GitHub objects and whether an object is reachable from the main branch:

- `"merged"` - Objects reachable from the main branch (highest integrity, regardless of authorship)
- `"approved"` - Objects with `author_association` of `OWNER`, `MEMBER`, or `COLLABORATOR`
- `"unapproved"` - Objects with `author_association` of `CONTRIBUTOR` or `FIRST_TIME_CONTRIBUTOR`
- `"none"` - Objects with `author_association` of `FIRST_TIMER` or `NONE` (lowest integrity)

### 3. Frontmatter Syntax

#### Minimal Example

```yaml
tools:
  github:
    mode: remote
    toolsets: [default]
    allowed-repos: "all"
    min-integrity: unapproved
```

#### With Repository Patterns

```yaml
tools:
  github:
    mode: remote
    toolsets: [default]
    allowed-repos:
      - "myorg/*"
      - "partner/shared-repo"
      - "docs/api-*"
    min-integrity: approved
```

#### Public Repositories Only

```yaml
tools:
  github:
    allowed-repos: "public"
    min-integrity: none
```

> **Note**: The field was originally named `repos` and renamed to `allowed-repos` in PR #22331. The old name is retained as a deprecated alias; run `gh aw fix` to migrate automatically.

Runtime precedence is defined by the canonical [Evaluation Order](#41-evaluation-order) in Operations.

## Operations

### 4. MCP Gateway Configuration Flow

1. **Frontmatter Parsing** (`tools_parser.go`):
   - Extracts `allowed-repos` and `min-integrity` directly from GitHub tool config
   - Stores them as fields on `GitHubToolConfig`
   - Validates structure and types
   - MUST complete before guard-policy validation in the same compiler pass. The compiler orchestration invariant is `ParseWorkflowFile()` → `setupWorkflowBuildContext()` / `processToolsAndMarkdown()` (frontmatter and tool parsing) → `validateWorkflowBuildContext()` / `validateWorkflowToolConfigurations()` (validation) in `pkg/workflow/compiler_orchestrator_workflow.go`.

2. **Validation** (`tools_validation.go`):
   - Validates allowed-repos format (all/public or valid patterns)
   - Validates min-integrity level (none/unapproved/approved/merged)
   - Validates repository pattern syntax (lowercase, valid characters, wildcard placement)
   - MUST run after frontmatter parsing has populated `GitHubToolConfig` and before workflow compilation completes or emits a compiled workflow. A guard-policy validation error MUST abort the same compiler pass.

3. **Compilation**:
   - Guard policy fields (allowed-repos, min-integrity) included in compiled GitHub tool configuration
   - Passed through to MCP Gateway configuration

4. **Runtime (MCP Gateway)**:
   - Gateway receives guard policies in server configuration
   - Enforces policies on all tool invocations
   - Blocks unauthorized repository access

### 4.1 Evaluation Order

The MCP Gateway MUST evaluate access in this order:

1. `lockdown: true` takes absolute precedence and denies the invocation; `allowed-repos` and `min-integrity` MUST NOT be evaluated.
2. When lockdown is not enabled, `allowed-repos` determines whether the target repository is in scope.
3. When the repository is in scope, `min-integrity` determines whether the content meets the required integrity level. Both checks MUST pass to allow the invocation.

Lockdown is an emergency security stop and MUST NOT be weakened by guard policies; those policies narrow access in an otherwise-open tool session but never grant access revoked by lockdown.

### 5. Safe Outputs Integration

When GitHub guard policies are configured, the compiler automatically derives a linked guard-policy for the safe-outputs MCP server. This ensures that safe output operations work correctly with guard policies by creating a write-sink configuration.

**Normative Requirements for `deriveSafeOutputsGuardPolicyFromGitHub()`:**

- **MUST derive a `write-sink` guard policy** for the safe-outputs MCP server whenever a GitHub guard policy (`allowed-repos` or `min-integrity`) is present in the workflow frontmatter. The derived policy MUST be applied before the workflow is executed.
- **MUST map `allowed-repos: "all"` or `allowed-repos: "public"` to `accept: ["*"]`**, allowing all safe output operations. Implementations MUST NOT restrict the write-sink scope when the GitHub guard policy already permits all repositories.
- **MUST transform each repository pattern** in an `allowed-repos` array to a `private:`-prefixed accept entry. Owner-wildcard patterns (`owner/*`) MUST be transformed to `private:owner` (the trailing `/*` is stripped). Prefix-wildcard patterns (`owner/prefix*`) MUST be transformed to `private:owner/prefix*` (the prefix is preserved). Exact repository patterns (`owner/repo`) MUST be transformed to `private:owner/repo`.
- **MUST NOT include duplicate accept entries** in the derived `write-sink` policy. If multiple input patterns resolve to the same `private:` value, the implementation MUST deduplicate before emitting the accept list.
- **SHOULD log a debug-level message** when a guard policy is derived, identifying the source GitHub `allowed-repos` value and the resulting accept list. This assists operators in diagnosing unexpected policy behavior.
- **MUST return `nil`** (no derived policy) when no GitHub guard policy fields are present on the tool configuration. The absence of a guard policy MUST NOT be treated as an implicit `accept: ["*"]` — the decision to omit the policy is intentional and MUST be preserved.

**Derivation Rules:**

- **`allowed-repos: "all"` or `allowed-repos: "public"`**: Creates `accept: ["*"]` to allow all safe output operations
- **`allowed-repos: [patterns]`**: Each pattern is transformed and added to the accept list:
  - `"owner/*"` → `"private:owner"` (owner wildcard → strip wildcard)
  - `"owner/prefix*"` → `"private:owner/prefix*"` (prefix wildcard → keep as-is)
  - `"owner/repo"` → `"private:owner/repo"` (specific repo → keep as-is)

#### Example - Public Repositories

```yaml
tools:
  github:
    allowed-repos: "public"
    min-integrity: approved
```

Generates safeoutputs guard-policy:
```json
{
  "write-sink": {
    "accept": ["*"]
  }
}
```

#### Example - Specific Repositories

```yaml
tools:
  github:
    allowed-repos:
      - "github/*"
      - "microsoft/copilot"
    min-integrity: approved
```

Generates safeoutputs guard-policy:
```json
{
  "write-sink": {
    "accept": [
      "private:github",
      "private:microsoft/copilot"
    ]
  }
}
```

#### Implementation

- Function: `deriveSafeOutputsGuardPolicyFromGitHub()` in `pkg/workflow/mcp_github_config.go`
- Called during MCP renderer setup for safeoutputs server
- Tests: `pkg/workflow/safeoutputs_guard_policy_test.go`

### 6. Extensibility for Future Servers

The design supports future MCP servers (Jira, WorkIQ) through:

1. **Server-Specific Policy Fields:**
   ```go
   type JiraToolConfig struct {
       // ... other fields ...
       // Guard policy fields (flat syntax under jira:)
       Projects   []string `yaml:"projects,omitempty"`
       IssueTypes []string `yaml:"issue-types,omitempty"`
   }
   ```

2. **General MCPServerConfig Field:**
   ```go
   type MCPServerConfig struct {
       // ...
       GuardPolicies map[string]any `yaml:"guard-policies,omitempty"`
   }
   ```

3. **Frontmatter Configuration:**
   ```yaml
   tools:
     jira:
       mode: remote
       projects: ["PROJ-*", "SHARED"]
       issue-types: ["Bug", "Story"]
   ```

## Implementation Details

### Files Modified

1. **pkg/workflow/tools_types.go**
   - Added `GitHubIntegrityLevel` enum type
   - Added `GitHubReposScope` type alias
   - Extended `GitHubToolConfig` with flat `Repos` and `MinIntegrity` fields
   - Extended `MCPServerConfig` with `GuardPolicies` field

2. **pkg/workflow/schemas/mcp-gateway-config.schema.json**
   - Added `guard-policies` field to `stdioServerConfig`
   - Added `guard-policies` field to `httpServerConfig`
   - Set `additionalProperties: true` for server-specific schemas

3. **pkg/workflow/tools_parser.go**
   - Extended `parseGitHubTool()` to extract `allowed-repos` and `min-integrity` directly

4. **pkg/workflow/tools_validation_github.go**
   - Updated `validateGitHubGuardPolicy()` function (validates flat fields)
   - Added `validateReposScope()` function
   - Added `validateRepoPattern()` function
   - Added `isValidOwnerOrRepo()` helper function

5. **pkg/workflow/compiler_orchestrator_workflow.go**
   - Added call to `validateGitHubGuardPolicy()`

6. **pkg/workflow/compiler_string_api.go**
   - Added call to `validateGitHubGuardPolicy()`

### Validation Rules

**Repository Patterns:**
- Must be lowercase
- Format: `owner/repo`, `owner/*`, or `owner/prefix*`
- Owner and repo parts must contain only: lowercase letters, numbers, hyphens, underscores
- Wildcards only allowed at end of repo name
- Empty arrays not allowed

**Integrity Levels:**
- Must be one of: `none`, `unapproved`, `approved`, `merged`
- Case-sensitive

**Required Fields:**
- `min-integrity` is required when using GitHub guard policies
- `allowed-repos` defaults to `"all"` if not specified

## Error Messages

The implementation provides clear, actionable error messages:

```
invalid guard policy: repository pattern 'Owner/Repo' must be lowercase

invalid guard policy: repository pattern 'owner/re*po' has wildcard in the middle.
Wildcards only allowed at the end (e.g., 'prefix*')

invalid guard policy: 'github.min-integrity' must be one of: 'none', 'unapproved', 'approved', 'merged'.
Got: 'admin'
```

## Usage Examples

### Example 1: Restrict to Organization

```yaml
tools:
  github:
    mode: remote
    toolsets: [default]
    allowed-repos:
      - "myorg/*"
    min-integrity: unapproved
```

### Example 2: Multiple Organizations

```yaml
tools:
  github:
    mode: remote
    toolsets: [default]
    allowed-repos:
      - "frontend-org/*"
      - "backend-org/*"
      - "shared/infrastructure"
    min-integrity: approved
```

### Example 3: Public Repositories Only

```yaml
tools:
  github:
    mode: remote
    toolsets: [repos, issues]
    allowed-repos: "public"
    min-integrity: none
```

### Example 4: Prefix Matching

```yaml
tools:
  github:
    mode: remote
    toolsets: [default]
    allowed-repos:
      - "myorg/api-*"     # Matches api-gateway, api-service, etc.
      - "myorg/web-*"     # Matches web-frontend, web-backend, etc.
    min-integrity: approved
```

## Testing Strategy

1. **Unit Tests** (Complete):
   - `TestValidateGitHubGuardPolicy`: 14 cases covering valid/invalid repos values, invalid min-integrity, missing fields
   - `TestValidateReposScopeWithStringSlice`: 4 cases covering `[]string` and `[]any` input types
   - Tests live in `pkg/workflow/tools_validation_test.go`

2. **Integration Tests** (Complete):
   - `TestGuardPolicyYAMLCompilationIntegration`: 5 round-trip tests in `pkg/workflow/guard_policy_compilation_integration_test.go`
     - `allowed-repos: all` → `accept: ["*"]` write-sink in compiled YAML
     - `allowed-repos: public` → `accept: ["*"]` write-sink in compiled YAML
     - Single specific repo → `"private:owner/repo"` in compiled YAML
     - Owner-wildcard repo (`owner/*`) → `"private:owner"` (stripped wildcard) in compiled YAML
     - Multiple repos → multiple `"private:..."` accept entries in compiled YAML
   - These tests verify that guard policies appear in the compiled lock YAML at the correct structure

## Next Steps

1. **Write Tests**:
   - Unit tests for parsing functions
   - Unit tests for validation functions
   - Integration tests for end-to-end workflow compilation

2. **Update Documentation**:
   - Add guard policies section to MCP gateway documentation
   - Add examples to GitHub MCP server documentation
   - Update frontmatter configuration reference

### Documentation Tasks

- [x] `docs/src/content/docs/reference/mcp-gateway.md` — document how GitHub guard policies map into gateway `guard-policies` and how `lockdown: true` overrides them. **Done when** the page shows the compiled gateway shape and warns that guard-policy fields are ignored under lockdown. (PR: [#48686](https://github.com/github/gh-aw/issues/48686))
- [x] `docs/src/content/docs/reference/github-tools.md` — add frontmatter examples for `allowed-repos`, `min-integrity`, `blocked-users`, `trusted-users`, and `approval-labels`. **Done when** the page includes at least one valid multi-field example and notes the deprecated `repos` alias. (PR: [#48686](https://github.com/github/gh-aw/issues/48686))
- [x] `docs/src/content/docs/reference/frontmatter-full.md` — add schema-level reference entries for the GitHub guard-policy fields. **Done when** each field has a documented type, default/requirement note, and at least one cross-reference to the GitHub/MCP gateway docs. (PR: [#48686](https://github.com/github/gh-aw/issues/48686))

3. **Runtime Implementation** (Separate from this PR):
   - MCP Gateway enforcement of guard policies
   - Repository pattern matching logic
   - Integrity level verification
   - Access control logging

## Benefits

1. **Security**: Restrict AI agent access to specific repositories
2. **Compliance**: Enforce minimum min-integrity requirements
3. **Flexibility**: Support diverse repository patterns and wildcards
4. **Extensibility**: Supports adding policies for Jira, WorkIQ, etc.
5. **Clarity**: Clear error messages and validation
6. **Documentation**: Self-documenting through type system

## Resolved Decisions

> **Status**: All four questions below have been resolved with decision records.

1. **Should we support negative patterns (e.g., exclude certain repos)?**

   **Decision**: No, negative patterns (e.g., `!owner/repo`) are **not supported** in the initial implementation.
   *Rationale*: Negative patterns introduce ordering complexity and ambiguity when combined with wildcard rules (e.g., `"owner/*"` and `"!owner/private-repo"` create a subtraction model that is hard to reason about safely). The preferred approach is to use an explicit allowlist — specify only what is permitted rather than excluding items from a broader grant. If a workflow requires fine-grained exclusions, it SHOULD use a narrower `allowed-repos` pattern. Negative patterns may be revisited in a future version if a clear security use-case emerges.

2. **Should we support combining multiple policies (AND/OR logic)?**

   **Decision**: Policies within a single MCP server are evaluated as **AND** conjunctions. Multiple `allowed-repos` entries in an array are evaluated as **OR** (any match grants access).
   *Rationale*: AND semantics for the combination of `allowed-repos` + `min-integrity` is the only safe default — a request must satisfy both the repository scope constraint AND the integrity constraint to proceed. Within `allowed-repos`, OR semantics (any matching pattern) is the standard allowlist behavior and consistent with how `roles` and other list-valued fields work throughout the compiler. Explicit cross-policy AND/OR combinators are deferred as unnecessary complexity; the current model covers all known production use-cases.

3. **How should conflicts between lockdown and guard policies be resolved?**

   **Decision**: The canonical [Evaluation Order](#41-evaluation-order) applies. The compiler SHOULD warn operators at compilation time when `lockdown: true` and guard-policy fields are both present; this is implemented in `pkg/workflow/tools_validation_github.go`, where `validateGitHubGuardPolicy()` detects the conflict and `emitGitHubLockdownGuardPolicyWarning()` surfaces the warning. This mirrors [github-mcp-access-control-specification.md §9.5.1](github-mcp-access-control-specification.md#951-precedence-rule).

4. **Should we add a "dry-run" mode to test policies before enforcement?**

   **Decision**: Runtime dry-run enforcement mode remains **deferred** to a future release. The compile-time validation (`gh aw compile --strict`) that reports which repositories would be permitted or denied under the configured guard policy is now **implemented**: `pkg/cli/compile_guard_policy_report.go` renders a per-workflow guard-policy dry-run report (allowed-repos, min-integrity, blocked-users, trusted-users, approval-labels, and lockdown precedence) to stderr whenever `--strict` is passed to `gh aw compile` and a GitHub guard policy is configured.
   *Rationale*: A runtime dry-run mode requires MCP Gateway support for pass-through logging of policy decisions, which is out of scope for the initial implementation. Compile-time policy analysis covers the majority of the validation need (catching misconfigured patterns before deployment) at lower implementation cost. Runtime dry-run may be added when MCP Gateway observability tooling matures.

## Conclusion

This implementation covers guard policies in the MCP gateway. The design is:

- **Type-safe**: Strongly-typed structs with validation
- **Extensible**: New servers and policy types can be added without structural changes
- **Consistent syntax**: Follows existing frontmatter conventions
- **Well-validated**: Validation with clear error messages
- **Forward-compatible**: Supports future enhancements

The implementation follows established patterns in the codebase and integrates with the existing compilation and validation infrastructure.

---

## Entities

This section defines the normative data entities of the guard policies framework. Implementations MUST represent each entity with the fields, types, and constraints described below.

### Entity: `GitHubReposScope`

`GitHubReposScope` defines the repository access scope for a GitHub guard policy. It MUST be one of:

| Value | Type | Meaning |
|---|---|---|
| `"all"` | String scalar | All repositories accessible by the token; no restriction |
| `"public"` | String scalar | Public repositories only; private repositories are denied |
| Array of patterns | `[]string` | Explicit allowlist of repository patterns (see §GP-03 for pattern syntax) |

Implementations MUST reject any other type (e.g., integers, booleans, nested maps) with a descriptive compilation error.

**Deprecated alias**: The YAML field `repos` is a deprecated alias for `allowed-repos` with identical semantics. Implementations MUST accept `repos` for backwards compatibility and SHOULD emit a deprecation warning when `repos` is used. New authoring MUST use `allowed-repos`. See [Deprecation: `repos` Field](#deprecation-repos-field).

### Entity: `GitHubIntegrityLevel`

`GitHubIntegrityLevel` represents the minimum content integrity level required before an AI agent is permitted to act on a GitHub object. It MUST be one of:

| Value | Meaning |
|---|---|
| `"none"` | No integrity requirement; all objects are permitted (lowest trust) |
| `"unapproved"` | Objects from open, non-approved pull requests are permitted |
| `"approved"` | Objects from pull requests that have been reviewed and approved |
| `"merged"` | Objects reachable from the main branch (highest trust) |

The trust ordering MUST be: `merged` > `approved` > `unapproved` > `none`.

Any value outside the four literals above MUST be rejected with a compilation error.

### Entity: `GitHubToolConfig` (guard-policy fields)

`GitHubToolConfig` is the workflow-level struct that carries GitHub-specific configuration under the `tools.github` frontmatter key. The guard-policy subset of fields is:

| Field | YAML Key | Type | Required | Description |
|---|---|---|---|---|
| `AllowedRepos` | `allowed-repos` | `GitHubReposScope` | No | Repository access scope. Defaults to `"all"` when `min-integrity` is present. |
| `Repos` | `repos` | `GitHubReposScope` | No | **Deprecated** alias for `allowed-repos`. |
| `MinIntegrity` | `min-integrity` | `GitHubIntegrityLevel` | Conditionally | Required whenever `allowed-repos` is explicitly configured, including `allowed-repos: "all"`. |

Implementations MUST ensure `AllowedRepos` and `Repos` are not both set simultaneously; if both are present, implementations SHOULD error or use `AllowedRepos` and warn.

### Deprecation: `repos` Field

The YAML key `repos` under `tools.github` is **deprecated** as of guard-policy specification version 0.2.0. It was renamed to `allowed-repos` to avoid collision with the `repos` toolset name.

**Migration path**: Use `gh aw fix` to automatically migrate `repos:` to `allowed-repos:` in workflow frontmatter.

**Removal target**: The `repos` alias SHOULD be removed in a future major version of the spec; tracking is managed in issue [#44357](https://github.com/github/gh-aw/issues/44357). When the alias is removed, implementations MUST reject `repos` as an unknown field with an error message that suggests `allowed-repos`.

---

## Conformance

The key words in this section are to be interpreted as described in RFC 2119 (see [Requirements Notation](#requirements-notation) above).

A conforming implementation of the guard policies framework **MUST** satisfy all of the following normative requirements:

**GP-01**: Implementations MUST support the `allowed-repos` field on `GitHubToolConfig` and validate its value as either a string scalar (`"all"`, `"public"`, or the expression `"${{ github.repository }}"`) or a non-empty array of repository patterns. Implementations MUST reject any other string scalar or any other type with a descriptive compilation error.

**GP-02**: Implementations MUST support the `min-integrity` field on `GitHubToolConfig` and validate its value as one of the enum strings `"none"`, `"unapproved"`, `"approved"`, or `"merged"`. Any other value MUST produce a descriptive compilation error.

**GP-03**: When `allowed-repos` is set to an array, implementations MUST validate that each element is either (a) the exact expression string `"${{ github.repository }}"` (accepted as a dynamic self-repo reference) or (b) a non-empty string matching one of the allowed pattern formats: exact (`owner/repo`), owner-wildcard (`owner/*`), or prefix-wildcard (`owner/prefix*`). Uppercase letters and wildcards in non-terminal positions MUST be rejected.

**GP-04**: Implementations MUST NOT permit an empty array as the value of `allowed-repos`. An empty allowlist MUST produce a compilation error indicating that an empty array is invalid.

**GP-05**: Implementations MUST call `deriveSafeOutputsGuardPolicyFromGitHub()` during MCP renderer setup for the safe-outputs server whenever a GitHub guard policy is present in the workflow frontmatter, and MUST apply the derived `write-sink` policy to the safe-outputs server configuration before the workflow is executed.

**GP-06**: The derived safe-outputs `write-sink` policy MUST map `allowed-repos: "all"` and `allowed-repos: "public"` to `accept: ["*"]`, permitting all safe output operations.

**GP-07**: The derived safe-outputs `write-sink` policy MUST transform each repository pattern in an `allowed-repos` array: owner-wildcard patterns (`owner/*`) MUST become `"private:owner"`; prefix-wildcard patterns (`owner/prefix*`) MUST become `"private:owner/prefix*"`; exact patterns (`owner/repo`) MUST become `"private:owner/repo"`. Duplicate accept entries MUST be deduplicated.

**GP-08**: When no GitHub guard policy fields are present on the tool configuration, `deriveSafeOutputsGuardPolicyFromGitHub()` MUST return `nil`. The absence of a guard policy MUST NOT be treated as an implicit `accept: ["*"]`.

**GP-09**: Implementations SHOULD emit a debug-level log message when a guard policy is derived, identifying the source `allowed-repos` value and the resulting `accept` list.

**GP-10**: When `lockdown: true` is set in the same workflow, implementations MUST treat `lockdown` as taking absolute precedence. Guard policy fields (`allowed-repos`, `min-integrity`) MUST NOT widen access beyond the single triggering repository when lockdown is active. The compiler SHOULD emit a warning when both `lockdown: true` and guard policy fields are present.

**GP-11**: When `allowed-repos` is configured explicitly, implementations MUST require `min-integrity` to be present. This requirement applies to every explicit repository scope, including `allowed-repos: "all"`, `allowed-repos: "public"`, the expression `"${{ github.repository }}"`, and explicit pattern arrays. Implementations MUST NOT accept any explicit `allowed-repos` value without `min-integrity`.

---

## Safeguards

This section defines normative safeguards that conforming implementations MUST apply to prevent misconfiguration, privilege escalation, and silent policy-bypass in the guard policies framework.

### GP-S001: Empty Allowlist Prevention

Implementations MUST reject an empty `allowed-repos` array (`allowed-repos: []`) with a compilation error. An empty allowlist provides no access and is almost always a misconfiguration. The error message MUST identify the field and indicate that an empty array is not a valid scope value. A `MUST` sentinel such as `"all"` or `"public"` MUST be used instead.

### GP-S002: Lockdown Supremacy

When `lockdown: true` is present on the same workflow, guard policy fields (`allowed-repos`, `min-integrity`, `blocked-users`, `trusted-users`, `approval-labels`) MUST NOT be evaluated for access-widening purposes. Implementations MUST treat lockdown as taking absolute precedence and MUST NOT combine lockdown with guard policies in any way that permits access beyond the single triggering repository.

**Shared precedence rule**: `lockdown: true` takes absolute precedence over `allowed-repos`, including non-empty allowlists, and over `min-integrity`; implementations MUST ignore those guard-policy fields for authorization while lockdown is active and MUST NOT allow them to widen access beyond the triggering repository.

See also: [github-mcp-access-control-specification.md §9.5.1](github-mcp-access-control-specification.md#951-precedence-rule).

Implementations MUST emit a compilation warning when both `lockdown: true` and any guard-policy field are present simultaneously, because the combination is almost certainly a misconfiguration (the guard-policy fields become inert).

### GP-S003: Cross-Field Consistency

When `allowed-repos` is set to any explicit value, including `"all"`, `"public"`, `"${{ github.repository }}"`, or an explicit pattern array, implementations MUST require `min-integrity` to also be present. Permitting a repository scope without a minimum integrity level could allow low-integrity content to reach repositories undetected.

Implementations MUST reject the combination `{ allowed-repos: <any explicit scope>, min-integrity: (absent) }` with a compilation error that names both the missing field and the reason it is required.

### GP-S004: Legacy Field Isolation

When the deprecated `repos` field is used alongside `allowed-repos` in the same `tools.github` block, implementations MUST NOT silently merge the two values. Implementations MUST either: (a) reject the combination with an error explaining that `repos` and `allowed-repos` cannot both be set, or (b) use `allowed-repos` and emit a warning that `repos` is ignored when `allowed-repos` is present.

In no case MUST the deprecated `repos` field silently override or supplement the normative `allowed-repos` field.

### GP-S005: Absent Policy is Not Permissive

When no guard-policy fields are present on `tools.github`, the derived safe-outputs `write-sink` policy MUST be `nil`. The absence of a guard policy is not equivalent to `accept: ["*"]`. Implementations MUST NOT add a default `accept: ["*"]` when the user has not configured any guard-policy.

---

## Sync Notes

This section maps normative sections of this specification to the implementation files that realise each requirement. Use this mapping to identify which files must be reviewed or updated when specification sections change.

**Last verified**: 2026-07-03

### Guard Policy Validation

| Spec Requirement | Description | Implementation File(s) |
|---|---|---|
| GP-01 `allowed-repos` parsing | Flat `allowed-repos` field extraction and type validation | `pkg/workflow/tools_parser.go` (`parseGitHubTool`) |
| GP-01, GP-03 pattern validation | Repository pattern format validation (exact, wildcard, prefix) | `pkg/workflow/tools_validation_github.go` (`validateReposScope`, `validateRepoPattern`, `isValidOwnerOrRepo`) |
| GP-02 `min-integrity` validation | Enum value check for `none`/`unapproved`/`approved`/`merged` | `pkg/workflow/tools_validation_github.go` (`validateGitHubGuardPolicy`) |
| GP-04 empty array rejection | Empty `allowed-repos` array detection and error | `pkg/workflow/tools_validation_github.go` (`validateGitHubGuardPolicy`) |
| GP-11 cross-field consistency | Any explicit `allowed-repos` value without `min-integrity` MUST fail validation, including `allowed-repos: "all"` | `pkg/workflow/tools_validation_github.go` (`validateGitHubGuardPolicy`), `pkg/workflow/tools_validation_test.go` (`missing min-integrity field`, `allowed-repos non-all without min-integrity fails`) |
| GP-10 lockdown precedence | Lockdown + guard-policy conflict detection and warning | `pkg/workflow/tools_validation_github.go` (`validateGitHubGuardPolicy`, `emitGitHubLockdownGuardPolicyWarning`) |

### Safe-Outputs Guard Policy Derivation

| Spec Requirement | Description | Implementation File(s) |
|---|---|---|
| GP-05 through GP-08 | Deriving the safe-outputs `write-sink` policy from GitHub guard policy | `pkg/workflow/mcp_github_config.go` (`deriveSafeOutputsGuardPolicyFromGitHub`) |
| GP-06 scalar mapping | `"all"` / `"public"` → `accept: ["*"]` mapping | `pkg/workflow/mcp_github_config.go` (`deriveSafeOutputsGuardPolicyFromGitHub`) |
| GP-07 pattern transformation | Array patterns → `private:`-prefixed accept entries | `pkg/workflow/mcp_github_config.go` (`normalizeGitHubRepositoryInReposScope`) |
| GP-05 through GP-08 tests | Derivation tests including nil-return, scalar, and array cases | `pkg/workflow/safeoutputs_guard_policy_test.go` (`TestDeriveSafeOutputsGuardPolicyFromGitHub`) |

### Legacy `repos` Field Migration

The deprecated `repos` field (YAML key: `repos`) is handled alongside `allowed-repos` in:

- **`pkg/workflow/mcp_github_config.go`** — The `deriveSafeOutputsGuardPolicyFromGitHub()` function reads `"allowed-repos"` first and falls back to `"repos"` when `"allowed-repos"` is absent (lines: `repos, hasRepos := githubTool["allowed-repos"]` then `repos, hasRepos = githubTool["repos"]`).
- **`pkg/workflow/tools_types.go`** — `GitHubToolConfig` declares both `AllowedRepos` (`yaml:"allowed-repos,omitempty"`) and the deprecated `Repos` (`yaml:"repos,omitempty"`) fields.

**Migration command**: `gh aw fix` applies a codemod that replaces `repos:` with `allowed-repos:` in workflow frontmatter. The codemod is idempotent and safe to run multiple times.

**Removal tracking**: The `repos` alias is tracked for removal. When it is removed, update `pkg/workflow/tools_types.go` (delete the `Repos` field), `pkg/workflow/mcp_github_config.go` (remove the fallback lookup), and `pkg/workflow/tools_validation_github.go` (adjust any `repos`-specific validation paths). Update doc-comments in `pkg/workflow/tools_types.go` to reference this spec version after the removal.

---

## Sync Follow-ups

This section lists the files that **MUST** be reviewed and updated whenever a normative section of this specification changes. Reviewers **SHALL** confirm each target is consistent with the updated spec before merging.

### After Restructuring Approach and Operations Headers

The Approach and Operations headers organize the former Proposed Solution and MCP Gateway Configuration Flow content. Keep these headers in place when synchronizing this scratchpad document with downstream documentation or navigation.

### After Adding or Changing Normative Requirements (§Conformance)

When requirements GP-01–GP-11 (or any later additions) change, update the following:

1. **`pkg/workflow/schemas/mcp-gateway-config.schema.json`** AND **`docs/public/schemas/mcp-gateway-config.schema.json`** — These are the source and published copies of the gateway config schema (JSON Schema draft-07, using `definitions`). The `allowed-repos` and `min-integrity` fields are frontmatter keys that compile to the `guard-policies` object inside the gateway config; they are not top-level properties of `stdioServerConfig` or `httpServerConfig`. Verify that the `guard-policies` definition and its `allowed-repos`/`min-integrity` sub-fields in both copies reflect the updated GP-01 and GP-02 constraints (enum values, types, `required` constraints). Keep both copies in sync.

2. **`pkg/workflow/tools_validation_github.go`** — Update `validateGitHubGuardPolicy()`, `validateReposScope()`, and `validateRepoPattern()` to enforce the revised constraints. Any new rejection rule in GP-01–GP-11 **MUST** have a corresponding validation call and error message in this file.

3. **`pkg/workflow/mcp_github_config.go`** — Update `deriveSafeOutputsGuardPolicyFromGitHub()` to match any changes to GP-05 or GP-08 derivation rules.

4. **`pkg/workflow/tools_types.go`** — Update `GitHubToolConfig`, `GitHubReposScope`, and `GitHubIntegrityLevel` type definitions and struct tags when field names, types, or constraints change.

### After Changing the Safe-Outputs Derivation Rules (§5)

When the derivation mapping in §5 changes (e.g., new pattern transformation rules):

1. **`pkg/workflow/schemas/mcp-gateway-config.schema.json`** AND **`docs/public/schemas/mcp-gateway-config.schema.json`** — Ensure the `write-sink` accept-list field structure in both copies of the gateway config schema matches the new derivation output.

2. **`pkg/workflow/mcp_github_config.go`** — Update `deriveSafeOutputsGuardPolicyFromGitHub()` and `normalizeGitHubRepositoryInReposScope()`.

3. **`pkg/workflow/safeoutputs_guard_policy_test.go`** — Add or update test cases in `TestDeriveSafeOutputsGuardPolicyFromGitHub` to cover the new transformation rules.

### After Changing Extension-Point Semantics (§6)

When the extensibility model for future MCP servers (Jira, WorkIQ) changes:

1. **`pkg/workflow/tools_types.go`** — Update `MCPServerConfig.GuardPolicies` and any server-specific policy types.

2. **`pkg/workflow/schemas/mcp-gateway-config.schema.json`** AND **`docs/public/schemas/mcp-gateway-config.schema.json`** — Add server-specific guard-policy schema objects as new `definitions` entries (the schema is JSON Schema draft-07 and uses `definitions`, not `$defs`) and reference them from the relevant server config schemas. Update both copies.

3. Document the new policy type in this specification under a new `### Entity:` subsection in [§Entities](#entities).
