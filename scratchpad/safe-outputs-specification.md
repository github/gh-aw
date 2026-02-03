---
title: Safe Outputs System Specification
description: Formal specification for the safe outputs system in GitHub Agentic Workflows
sidebar:
  order: 100
---

# Safe Outputs System Specification

**Version**: 1.0.0  
**Status**: Recommendation  
**Latest Version**: https://github.github.com/gh-aw/scratchpad/safe-outputs-specification/  
**Editors**: GitHub Next Team

---

## Abstract

This specification defines the Safe Outputs System for GitHub Agentic Workflows, a security-oriented architecture that enables AI agents to request write operations to GitHub resources without possessing write permissions. The system enforces separation of concerns through a multi-layered architecture comprising frontmatter configuration, Model Context Protocol (MCP) servers, validation guardrails, and execution handlers. This specification establishes conformance requirements for implementations, security controls, and operational patterns for both generic system tools and GitHub-specific operations.

## Status of This Document

This is a Recommendation specification and represents the current stable implementation of the Safe Outputs System in GitHub Agentic Workflows. This specification is actively maintained and may receive updates to reflect new features, security enhancements, or clarifications. Breaking changes will result in a new major version.

This specification is governed by the GitHub Next team and follows semantic versioning principles for version management.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Architecture](#3-architecture)
4. [Security Model](#4-security-model)
5. [Builtin System Tools](#5-builtin-system-tools)
6. [GitHub Operations](#6-github-operations)
7. [Compliance Testing](#7-compliance-testing)
8. [Appendices](#appendices)
9. [References](#references)
10. [Change Log](#change-log)

---

## 1. Introduction

### 1.1 Purpose

The Safe Outputs System addresses the security challenge of enabling AI agents to perform write operations on GitHub resources while maintaining strict security boundaries. Traditional approaches grant agents write permissions, creating risks from prompt injection, unintended actions, or malicious payloads. This specification defines a system that separates AI reasoning (read-only) from write operations (permission-controlled), providing defense-in-depth security.

### 1.2 Scope

This specification covers:

- Architectural layers and their interactions
- Security model and permission boundaries
- Configuration schema and validation
- MCP server protocol integration
- Builtin system tools (missing-tool, missing-data, noop)
- GitHub-specific operation handlers
- Validation guardrails and limits
- Error handling and recovery patterns
- Conformance requirements and testing

This specification does NOT cover:

- General GitHub Actions syntax and semantics
- AI engine implementation details
- Network transport security (covered separately)
- General MCP protocol specification (see [MCP Specification](https://modelcontextprotocol.io/))
- Workflow trigger mechanisms
- GitHub API implementation details

### 1.3 Design Goals

The Safe Outputs System is designed to achieve:

1. **Security Through Separation**: AI agents operate with read-only access; write operations execute in isolated jobs
2. **Least Privilege**: Each operation receives only the minimum permissions required
3. **Defense Against Injection**: Structured output format prevents command injection and template attacks
4. **Auditability**: All operations are logged, traceable, and reviewable
5. **Controlled Limits**: Configurable per-operation limits prevent resource exhaustion
6. **Extensibility**: Plugin architecture supports custom operations via safe-jobs
7. **Transparency**: Staged mode enables preview without execution
8. **Fail-Safe Defaults**: Conservative defaults protect against misconfiguration

---

## 2. Conformance

### 2.1 Conformance Classes

This specification defines three conformance classes:

#### 2.1.1 Basic Conformance

A **Basic Conforming Implementation** MUST:
- Implement the four-layer architecture (frontmatter, MCP, guardrails, application)
- Support all three builtin system tools (missing-tool, missing-data, noop)
- Enforce read-only execution for AI agent jobs
- Validate all structured output against defined schemas
- Implement permission isolation between agent and execution jobs
- Support staged mode for preview operations
- Generate NDJSON output in the specified format

#### 2.1.2 Standard Conformance

A **Standard Conforming Implementation** MUST satisfy Basic Conformance and:
- Implement at least 10 GitHub operation types
- Support cross-repository operations where specified
- Implement all validation guardrails defined in Section 4
- Support temporary ID resolution for issue references
- Implement message templating and attribution footers
- Support auto-expiration for time-limited resources
- Handle error recovery patterns for failed operations

#### 2.1.3 Complete Conformance

A **Complete Conforming Implementation** MUST satisfy Standard Conformance and:
- Implement all GitHub operation types defined in Section 6
- Support custom safe-jobs via MCP tool registration
- Implement GitHub Projects v2 operations
- Support Code Scanning SARIF report generation
- Implement all optional features (grouping, hiding, etc.)
- Pass all compliance tests defined in Section 7

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.3 Compliance Levels

Implementations are classified into three levels based on completeness:

- **Level 1: Basic** - Core architecture and builtin tools only
- **Level 2: Standard** - Common GitHub operations and guardrails
- **Level 3: Complete** - Full feature set including advanced operations

---

## 3. Architecture

### 3.1 Overview

The Safe Outputs System implements a four-layer architecture that separates concerns and enforces security boundaries:

```text
┌─────────────────────────────────────────────────────────┐
│ Layer 1: Frontmatter Configuration                      │
│ - Workflow author declares safe-outputs: in YAML        │
│ - Defines limits, permissions, and constraints          │
│ - Compiled into GitHub Actions workflow                 │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ Layer 2: MCP Server (Agent Interface)                   │
│ - Exposes tools to AI agent via MCP protocol            │
│ - Accepts structured requests as JSON                   │
│ - No write permissions, only output collection          │
│ - Tools registered per configuration                    │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ Layer 3: Validation Guardrails                          │
│ - Schema validation of structured output                │
│ - Max count enforcement per operation type              │
│ - Label sanitization and content filtering              │
│ - Cross-repository permission validation                │
│ - Target validation (triggering, *, number)             │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ Layer 4: Execution Handlers (GitHub Operations)         │
│ - Separate GitHub Actions jobs with write permissions   │
│ - Execute validated operations via GitHub API           │
│ - Apply message templating and attribution              │
│ - Handle errors with fallback strategies                │
└─────────────────────────────────────────────────────────┘
```

### 3.2 Layer 1: Frontmatter Configuration

#### 3.2.1 Configuration Schema

Implementations MUST support the following frontmatter structure:

```yaml
safe-outputs:
  # Builtin system tools
  missing-tool: {}
  missing-data: {}
  noop: {}
  
  # GitHub operations (see Section 6)
  create-issue: {}
  add-comment: {}
  create-pull-request: {}
  # ... (additional operations)
  
  # Custom operations
  jobs:
    custom-operation:
      description: "Custom handler"
      steps: [...]
```

#### 3.2.2 Configuration Processing

Implementations MUST:
- Parse frontmatter configuration during workflow compilation
- Validate configuration against JSON schemas
- Apply default values for omitted optional fields
- Reject invalid configurations with descriptive errors
- Pass configuration to execution handlers
- Register only enabled tools with the MCP server

### 3.3 Layer 2: MCP Server

#### 3.3.1 Server Initialization

The MCP server MUST:
- Load configuration from designated location
- Register tools based on enabled safe-outputs
- Expose tools via JSON-RPC 2.0 over stdio
- Log operations to designated log directory
- Validate tool requests against schemas

#### 3.3.2 Tool Registration

Each safe output type MUST be registered as an MCP tool with:
- **Name**: Normalized tool name (hyphens to underscores)
- **Description**: Human-readable purpose
- **Input Schema**: JSON Schema for request validation
- **Handler**: Function to process validated requests

Example tool registration:
```javascript
{
  "name": "create_issue",
  "description": "Create a GitHub issue",
  "inputSchema": {
    "type": "object",
    "properties": {
      "title": { "type": "string" },
      "body": { "type": "string" }
    },
    "required": ["title", "body"]
  }
}
```

#### 3.3.3 Request Processing

For each tool invocation, the MCP server MUST:
1. Validate request against input schema
2. Apply operation-specific guardrails
3. Append validated request to NDJSON output file
4. Return success response to agent
5. NOT execute any GitHub API operations directly

#### 3.3.4 Output Format

The MCP server MUST write requests to NDJSON file with format:
```json
{"type": "create-issue", "title": "Bug report", "body": "Description"}
{"type": "add-comment", "target": "123", "body": "Comment text"}
```

Each line MUST be valid JSON with at minimum a `type` field.

### 3.4 Layer 3: Validation Guardrails

#### 3.4.1 Schema Validation

Implementations MUST validate all structured output against JSON schemas covering:
- Required field presence
- Type correctness
- Format constraints (e.g., string length, number ranges)
- Enumerated value restrictions
- Cross-field dependencies

#### 3.4.2 Max Count Enforcement

For each safe output type, implementations MUST:
- Enforce configured `max` limits (default: 1 for most types)
- Track operation count during execution
- Reject operations exceeding limit with descriptive error
- Allow unlimited operations when `max: 0` is explicitly configured

#### 3.4.3 Content Sanitization

Implementations MUST sanitize:
- **Labels**: Remove `@` mentions, control characters, limit to 64 chars
- **Titles**: Trim whitespace, reject empty strings
- **Bodies**: No modification except template variable replacement
- **Paths**: Validate against directory traversal attacks
- **URLs**: Validate format for external references

#### 3.4.4 Target Validation

For operations supporting `target` field, implementations MUST validate:
- `"triggering"`: Requires workflow triggered by issue/PR/discussion event
- `"*"`: Accepts any valid target
- Numeric value: Validates against repository's issue/PR/discussion numbers
- Temporary IDs: Resolves to actual numbers before execution

#### 3.4.5 Cross-Repository Validation

For cross-repository operations, implementations MUST:
- Validate `target-repo` format: `owner/repo`
- Verify token has access to target repository
- Reject operations when token lacks required permissions
- Apply same validation rules as same-repository operations

### 3.5 Layer 4: Execution Handlers

#### 3.5.1 Job Isolation

Execution handlers MUST run in separate GitHub Actions jobs that:
- Depend on the agent job completing successfully
- Execute only if safe output of that type was requested
- Have isolated permissions (no access to agent environment)
- Use dedicated GitHub tokens with minimal required scope

#### 3.5.2 Permission Model

Each execution job MUST declare minimum required permissions:
- `contents: read` for all jobs (access to repository)
- Additional permissions per operation type:
  - `issues: write` for issue operations
  - `pull-requests: write` for PR operations
  - `discussions: write` for discussion operations
  - Custom tokens via secrets for GitHub Projects

#### 3.5.3 Execution Flow

Handlers MUST follow this execution pattern:
1. Load NDJSON output from agent job
2. Parse and validate each operation request
3. Apply operation-specific business logic
4. Execute GitHub API operations in order
5. Generate attribution footers and messages
6. Handle errors with fallback strategies
7. Output summary to GitHub Actions step summary

#### 3.5.4 Error Handling

Implementations MUST handle errors with:
- **Retry Logic**: Transient failures (rate limits, network errors)
- **Fallback Strategies**: Alternative actions on primary failure
- **Descriptive Messages**: Clear error descriptions for debugging
- **Graceful Degradation**: Continue processing remaining operations when possible

---

## 4. Security Model

### 4.1 Threat Model

The Safe Outputs System is designed to defend against:

1. **Prompt Injection**: Malicious prompts attempting to manipulate agent output
2. **Privilege Escalation**: Attempts to gain write permissions within agent context
3. **Resource Exhaustion**: Unbounded operation creation depleting repository resources
4. **Data Exfiltration**: Using output channels to leak sensitive information
5. **Cross-Repository Attacks**: Unauthorized operations in external repositories

### 4.2 Security Boundaries

#### 4.2.1 Permission Isolation

The system enforces these permission boundaries:

| Component | Permissions | Rationale |
|-----------|-------------|-----------|
| Agent Job | `contents: read` only | Prevents write operations during AI reasoning |
| MCP Server | No GitHub API access | Output collection only, no execution |
| Execution Jobs | Minimal write per type | Least privilege for specific operation |

#### 4.2.2 Read-Only Agent Enforcement

Implementations MUST ensure agent jobs:
- Cannot write to repository
- Cannot create or modify GitHub resources
- Cannot access write-scoped tokens
- Cannot bypass MCP server validation

#### 4.2.3 Structured Output Validation

All agent output MUST be:
- Validated against strict JSON schemas
- Sanitized for injection attacks
- Limited by max count restrictions
- Reviewable before execution (staged mode)

### 4.3 Security Controls

#### 4.3.1 Input Validation

Implementations MUST validate:
- All string fields for injection patterns
- All numeric fields for range limits
- All array fields for length limits
- All object fields for required properties

#### 4.3.2 Label Sanitization

Label sanitization MUST:
- Remove all `@` characters to prevent mentions
- Remove control characters (0x00-0x1F, 0x7F-0x9F)
- Trim whitespace
- Limit to 64 characters
- Reject empty labels after sanitization

#### 4.3.3 Rate Limiting

Implementations SHOULD implement:
- Per-workflow operation limits
- Per-repository aggregate limits
- Exponential backoff for rate limit errors
- Clear error messages when limits exceeded

#### 4.3.4 Audit Logging

All operations MUST be logged with:
- Workflow run URL
- Operation type
- Target resource
- Result (success/failure)
- Timestamp
- Triggering user or event

### 4.4 Staged Mode

#### 4.4.1 Preview Operations

Staged mode (`staged: true`) MUST:
- Parse and validate all operations
- Display preview in GitHub Actions step summary
- NOT execute any GitHub API operations
- Include full operation details in preview
- Use 🎭 emoji to indicate staged operations

#### 4.4.2 Preview Format

Staged previews MUST include:
- Operation type header
- All operation parameters
- Generated message content
- Attribution footers
- Patch previews for code changes (truncated at 500 lines)

---

## 5. Builtin System Tools

Builtin system tools provide essential system functions independent of GitHub operations. These tools MUST be available in all conforming implementations.

### 5.1 Missing Tool (`missing-tool`)

#### 5.1.1 Purpose

Reports when the AI agent requires functionality not available in the current tool configuration. Enables workflows to self-document missing capabilities and optionally create tracking issues.

#### 5.1.2 Configuration

```yaml
safe-outputs:
  missing-tool:
    max: 0                           # 0 = unlimited (default)
    create-issue: true               # Create GitHub issue (default: true)
    title-prefix: "[missing tool] "  # Issue title prefix
    labels: [enhancement, tools]     # Labels for created issues
```

#### 5.1.3 Request Schema

```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Name of the missing tool"
    },
    "description": {
      "type": "string",
      "description": "Why this tool is needed"
    },
    "use_case": {
      "type": "string",
      "description": "Specific use case or task requiring this tool"
    }
  },
  "required": ["name", "description"]
}
```

#### 5.1.4 Behavior

The missing-tool handler MUST:
1. Validate request against schema
2. Check if max limit is exceeded (if configured)
3. If `create-issue: true`:
   - Create GitHub issue with title: `{title-prefix}{tool.name}`
   - Include description and use case in issue body
   - Add configured labels
   - Add workflow attribution footer
4. Log missing tool to GitHub Actions step summary
5. Output `tools_reported` and `total_count` for tracking

#### 5.1.5 Conformance Requirements

- **T-MST-001**: MUST validate `name` and `description` are non-empty strings
- **T-MST-002**: MUST respect `max` limit when configured (non-zero)
- **T-MST-003**: MUST create issue when `create-issue: true`
- **T-MST-004**: MUST apply title prefix to issue title
- **T-MST-005**: MUST sanitize labels according to Section 4.3.2

### 5.2 Missing Data (`missing-data`)

#### 5.2.1 Purpose

Reports when the AI agent requires information not available in the current context to achieve its goal. Distinguishes between missing tools (functionality) and missing data (information).

#### 5.2.2 Configuration

```yaml
safe-outputs:
  missing-data:
    max: 0                           # 0 = unlimited (default)
    create-issue: true               # Create GitHub issue (default: true)
    title-prefix: "[missing data] "  # Issue title prefix
    labels: [documentation, data]    # Labels for created issues
```

#### 5.2.3 Request Schema

```json
{
  "type": "object",
  "properties": {
    "data_type": {
      "type": "string",
      "description": "Type of data needed (e.g., 'API documentation', 'database schema')"
    },
    "reason": {
      "type": "string",
      "description": "Why this data is required"
    },
    "context": {
      "type": "string",
      "description": "Additional context about the missing information"
    }
  },
  "required": ["data_type", "reason"]
}
```

#### 5.2.4 Behavior

The missing-data handler MUST:
1. Validate request against schema
2. Check if max limit is exceeded (if configured)
3. If `create-issue: true`:
   - Create GitHub issue with title: `{title-prefix}{data_type}`
   - Include reason and context in issue body
   - Add configured labels
   - Add workflow attribution footer
4. Log missing data to GitHub Actions step summary
5. Output `data_reported` and `total_count` for tracking

#### 5.2.5 Conformance Requirements

- **T-MSD-001**: MUST validate `data_type` and `reason` are non-empty strings
- **T-MSD-002**: MUST respect `max` limit when configured (non-zero)
- **T-MSD-003**: MUST create issue when `create-issue: true`
- **T-MSD-004**: MUST differentiate from missing-tool in issue content
- **T-MSD-005**: MUST apply title prefix to issue title

### 5.3 No-Op (`noop`)

#### 5.3.1 Purpose

Provides a transparent way for agents to signal completion without taking any action. Useful for workflows that analyze but don't modify, or when agent determines no action is necessary.

#### 5.3.2 Configuration

```yaml
safe-outputs:
  noop:
    max: 1  # Maximum noop messages (default: 1)
```

#### 5.3.3 Request Schema

```json
{
  "type": "object",
  "properties": {
    "message": {
      "type": "string",
      "description": "Explanation of why no action was taken"
    },
    "reason": {
      "type": "string",
      "enum": ["no_action_needed", "analysis_only", "insufficient_data", "other"],
      "description": "Categorized reason for no-op"
    }
  },
  "required": ["message"]
}
```

#### 5.3.4 Behavior

The noop handler MUST:
1. Validate request against schema
2. Check if max limit is exceeded
3. Log message to GitHub Actions step summary with 📝 icon
4. Include reason category if provided
5. NOT create any GitHub resources
6. Return success without side effects

#### 5.3.5 Conformance Requirements

- **T-NOP-001**: MUST validate `message` is non-empty string
- **T-NOP-002**: MUST respect `max: 1` limit
- **T-NOP-003**: MUST NOT create any GitHub resources
- **T-NOP-004**: MUST log to step summary for transparency
- **T-NOP-005**: MUST accept optional `reason` enum value

### 5.4 Builtin Tools Summary

| Tool | Purpose | Creates GitHub Resources | Default Max | Auto-Enabled |
|------|---------|-------------------------|-------------|--------------|
| `missing-tool` | Report missing functionality | Optional (issues) | Unlimited | Yes when any safe-outputs configured |
| `missing-data` | Report missing information | Optional (issues) | Unlimited | Yes when any safe-outputs configured |
| `noop` | Signal completion without action | Never | 1 | Always |

---

## 6. GitHub Operations

GitHub operations enable AI agents to interact with GitHub resources. These operations are separated from builtin tools because they are specific to GitHub's platform and require specialized permissions and handling.

### 6.1 Operation Categories

#### 6.1.1 Issues & Discussions

Operations for creating and managing issues and discussions:

- `create-issue` - Create new GitHub issues
- `update-issue` - Modify existing issue title, body, or status
- `close-issue` - Close issues with optional comment
- `link-sub-issue` - Create parent-child issue relationships
- `create-discussion` - Create repository discussions
- `update-discussion` - Modify discussion title, body, or labels
- `close-discussion` - Close discussions with resolution

#### 6.1.2 Pull Requests

Operations for pull request management:

- `create-pull-request` - Create PRs with code changes
- `update-pull-request` - Modify PR title or body
- `close-pull-request` - Close PRs without merging
- `create-pull-request-review-comment` - Add line-specific review comments
- `push-to-pull-request-branch` - Push changes to PR branch

#### 6.1.3 Labels, Assignments & Reviews

Operations for metadata and collaboration:

- `add-comment` - Post comments on issues, PRs, or discussions
- `hide-comment` - Minimize comments in GitHub UI
- `add-labels` - Add labels to issues or PRs
- `add-reviewer` - Request PR reviews from users
- `assign-milestone` - Assign issues to milestones
- `assign-to-agent` - Assign Copilot agents
- `assign-to-user` - Assign users to issues

#### 6.1.4 Projects, Releases & Assets

Operations for project management and releases:

- `create-project` - Create GitHub Projects boards
- `update-project` - Modify project items and fields
- `create-project-status-update` - Post project status updates
- `update-release` - Modify release descriptions
- `upload-asset` - Upload files to orphaned branches

#### 6.1.5 Security & Agent Tasks

Operations for security and agent orchestration:

- `create-code-scanning-alert` - Generate SARIF security reports
- `create-agent-session` - Create Copilot agent sessions

### 6.2 Common Patterns

#### 6.2.1 Cross-Repository Support

Operations supporting cross-repository actions MUST:
- Accept `target-repo: "owner/repo"` configuration
- Validate token has access to target repository
- Use target repository for all GitHub API calls
- Apply same validation rules as same-repository operations

Exceptions (same-repository only):
- `push-to-pull-request-branch` - Requires repository write access
- `upload-asset` - Creates orphaned branches
- `create-code-scanning-alert` - Security scanning scope
- `update-project` - Project board modifications

#### 6.2.2 Target Specification

Many operations support `target` field for specifying the affected resource:

```yaml
target: "triggering"  # Default: uses resource that triggered workflow
target: "*"           # Wildcard: agent specifies target in request
target: 123           # Specific: operates on issue/PR/discussion #123
```

Implementations MUST:
- Validate `target: "triggering"` requires appropriate event type
- Validate `target: "*"` includes target in agent request
- Validate numeric target exists in repository
- Reject invalid targets with clear error messages

#### 6.2.3 Attribution Footers

All GitHub content created by safe outputs MUST include attribution:

```markdown
> AI generated by [WorkflowName](run_url)
```

With optional context for triggering resource:
```markdown
> AI generated by [WorkflowName](run_url) for #123
```

Implementations MUST:
- Append footer to all created content (issues, comments, PRs, discussions)
- Include workflow name from configuration
- Include workflow run URL for traceability
- Include triggering resource reference when available

#### 6.2.4 Title Prefixes

Many operations support `title-prefix` for consistent labeling:

```yaml
title-prefix: "[ai] "  # Prepended to all titles
```

Implementations MUST:
- Prepend prefix to all titles generated by that operation
- NOT add prefix if title already starts with it (idempotency)
- Allow empty string to disable prefix

#### 6.2.5 Label Management

Operations creating GitHub resources MAY apply labels:

```yaml
labels: [automation, ai-generated]      # Always applied
allowed-labels: [bug, enhancement]       # Restrict agent choices
```

Implementations MUST:
- Apply configured labels to all created resources
- Filter agent-requested labels against `allowed-labels` if configured
- Sanitize all labels according to Section 4.3.2
- Handle non-existent labels gracefully (auto-create or skip based on GitHub API)

#### 6.2.6 Expiration

Some operations support auto-expiration:

```yaml
expires: 7      # Days (integer)
expires: "2h"   # Relative format: 2h, 7d, 2w, 1m, 1y
```

Implementations MUST:
- Parse expiration format correctly
- Generate maintenance workflow for expiration checking
- Schedule maintenance at appropriate frequency
- Close expired resources with attribution comment

### 6.3 Operation Specifications

Due to the extensive nature of GitHub operations (20+ operation types), detailed per-operation specifications are provided in separate appendices:

- **Appendix A**: Issues & Discussions Operations
- **Appendix B**: Pull Request Operations
- **Appendix C**: Labels, Assignments & Reviews Operations
- **Appendix D**: Projects, Releases & Assets Operations
- **Appendix E**: Security & Agent Task Operations

Each appendix includes:
- Configuration schema
- Request schema
- Behavior specification
- Error handling requirements
- Conformance test identifiers
- Examples

### 6.4 GitHub Operations Summary Table

| Operation | Max Default | Cross-Repo | Permissions Required | Staged Support |
|-----------|-------------|------------|---------------------|----------------|
| `create-issue` | 1 | ✅ | `issues: write` | ✅ |
| `update-issue` | 1 | ✅ | `issues: write` | ✅ |
| `close-issue` | 1 | ✅ | `issues: write` | ✅ |
| `link-sub-issue` | 1 | ✅ | `issues: write` | ✅ |
| `create-discussion` | 1 | ✅ | `discussions: write` | ✅ |
| `update-discussion` | 1 | ✅ | `discussions: write` | ✅ |
| `close-discussion` | 1 | ✅ | `discussions: write` | ✅ |
| `add-comment` | 1 | ✅ | `issues/pull-requests: write` | ✅ |
| `hide-comment` | 5 | ✅ | `issues/pull-requests: write` | ✅ |
| `create-pull-request` | 1 | ✅ | `contents: write`, `pull-requests: write` | ✅ |
| `update-pull-request` | 1 | ✅ | `pull-requests: write` | ✅ |
| `close-pull-request` | 10 | ✅ | `pull-requests: write` | ✅ |
| `create-pull-request-review-comment` | 10 | ✅ | `pull-requests: write` | ✅ |
| `push-to-pull-request-branch` | 1 | ❌ | `contents: write` | ✅ |
| `add-labels` | 3 | ✅ | `issues: write` | ✅ |
| `add-reviewer` | 3 | ✅ | `pull-requests: write` | ✅ |
| `assign-milestone` | 1 | ✅ | `issues: write` | ✅ |
| `assign-to-agent` | 1 | ✅ | Custom token | ✅ |
| `assign-to-user` | 1 | ✅ | `issues: write` | ✅ |
| `create-project` | 1 | ✅ | PAT with `project` scope | ✅ |
| `update-project` | 10 | ❌ | PAT with `project` scope | ✅ |
| `create-project-status-update` | Unlimited | ✅ | PAT with `project` scope | ✅ |
| `update-release` | 1 | ✅ | `contents: write` | ✅ |
| `upload-asset` | 10 | ❌ | `contents: write` | ✅ |
| `create-code-scanning-alert` | Unlimited | ❌ | `security-events: write` | ❌ |
| `create-agent-session` | 1 | ✅ | Custom token | ✅ |

---

## 7. Compliance Testing

### 7.1 Test Suite Requirements

Conforming implementations MUST include automated tests covering:

#### 7.1.1 Architecture Tests
- **T-ARC-001**: Verify four-layer architecture separation
- **T-ARC-002**: Verify read-only agent job permissions
- **T-ARC-003**: Verify isolated execution job permissions
- **T-ARC-004**: Verify configuration propagation to execution handlers

#### 7.1.2 Security Tests
- **T-SEC-001**: Verify label sanitization removes @ mentions
- **T-SEC-002**: Verify max count enforcement prevents resource exhaustion
- **T-SEC-003**: Verify staged mode prevents execution
- **T-SEC-004**: Verify cross-repository permission validation
- **T-SEC-005**: Verify injection prevention in all string fields

#### 7.1.3 Builtin Tool Tests
- **T-MST-001** through **T-MST-005**: Missing Tool conformance
- **T-MSD-001** through **T-MSD-005**: Missing Data conformance
- **T-NOP-001** through **T-NOP-005**: No-Op conformance

#### 7.1.4 GitHub Operation Tests
- Per-operation schema validation tests
- Per-operation max count enforcement tests
- Cross-repository operation tests
- Error handling and recovery tests
- Attribution footer generation tests

### 7.2 Compliance Checklist

| Requirement | Test ID | Level | Status |
|-------------|---------|-------|--------|
| Four-layer architecture | T-ARC-001 | 1 | Required |
| Read-only agent | T-ARC-002 | 1 | Required |
| Isolated execution | T-ARC-003 | 1 | Required |
| Environment variables | T-ARC-004 | 1 | Required |
| Label sanitization | T-SEC-001 | 1 | Required |
| Max count enforcement | T-SEC-002 | 1 | Required |
| Staged mode | T-SEC-003 | 1 | Required |
| Cross-repo validation | T-SEC-004 | 2 | Required |
| Injection prevention | T-SEC-005 | 1 | Required |
| Missing Tool | T-MST-* | 1 | Required |
| Missing Data | T-MSD-* | 1 | Required |
| No-Op | T-NOP-* | 1 | Required |
| GitHub Operations (10+) | Various | 2 | Required |
| GitHub Operations (All) | Various | 3 | Optional |

### 7.3 Test Execution Procedures

Implementations SHOULD provide:
1. **Unit Tests**: Per-component validation in isolation
2. **Integration Tests**: End-to-end workflow compilation and execution
3. **Security Tests**: Fuzzing and injection attack simulations
4. **Compatibility Tests**: Multiple GitHub repository configurations

---

## Appendices

### Appendix A: Examples

#### A.1 Basic Missing Tool Report

**Configuration:**
```yaml
safe-outputs:
  missing-tool:
```

**Agent Request:**
```json
{
  "type": "missing-tool",
  "name": "database-query",
  "description": "Need ability to query PostgreSQL database",
  "use_case": "Fetch user statistics for report generation"
}
```

**Result:**
- Issue created: `[missing tool] database-query`
- Body includes description and use case
- Attribution footer added
- Logged to step summary

#### A.2 Cross-Repository Issue Creation

**Configuration:**
```yaml
safe-outputs:
  create-issue:
    target-repo: "octocat/downstream"
    labels: [upstream-request]
```

**Agent Request:**
```json
{
  "type": "create-issue",
  "title": "Feature request from upstream",
  "body": "Please add API endpoint for /users/stats"
}
```

**Result:**
- Issue created in `octocat/downstream` repository
- Label `upstream-request` applied
- Attribution footer references source workflow

#### A.3 Staged Mode Preview

**Configuration:**
```yaml
safe-outputs:
  create-issue:
    staged: true
```

**Result:**
- No issues created
- Preview shown in step summary:
  ```
  ## 🎭 Staged Mode: Create Issues Preview
  
  ### Issue 1
  **Title:** Bug in authentication
  **Body:** Users unable to log in with OAuth
  **Labels:** bug, authentication
  ```

### Appendix B: Error Codes

| Code | Message | Resolution |
|------|---------|------------|
| `E-VAL-001` | Invalid request schema | Check request against JSON schema |
| `E-VAL-002` | Max count exceeded | Reduce number of operations or increase limit |
| `E-VAL-003` | Invalid target specification | Use "triggering", "*", or valid number |
| `E-SEC-001` | Label sanitization failed | Remove invalid characters from labels |
| `E-SEC-002` | Cross-repo permission denied | Verify token has access to target repo |
| `E-API-001` | GitHub API rate limit | Wait and retry with exponential backoff |
| `E-API-002` | Resource not found | Verify target issue/PR/discussion exists |

### Appendix C: Security Considerations

#### C.1 Threat Prevention

The Safe Outputs System prevents:
- **Command Injection**: Structured JSON prevents shell command injection
- **Template Injection**: No template evaluation in agent context
- **Path Traversal**: File paths validated and sanitized
- **XSS**: Label sanitization removes script content
- **CSRF**: GitHub API token prevents cross-site attacks

#### C.2 Security Best Practices

Implementations SHOULD:
- Use minimal-scope tokens for each operation type
- Rotate tokens regularly
- Log all operations for audit trail
- Monitor for unusual operation patterns
- Implement rate limiting per workflow
- Review staged mode previews before disabling

#### C.3 Known Limitations

The system does NOT protect against:
- **Social Engineering**: Malicious users manipulating workflow authors
- **Repository Owner Actions**: Owners can bypass restrictions
- **Transitive Attacks**: Compromised dependencies in execution environment

---

## References

### Normative References

- **[RFC 2119]** S. Bradner. "Key words for use in RFCs to Indicate Requirement Levels". RFC 2119, March 1997. https://www.ietf.org/rfc/rfc2119.txt

- **[JSON-SCHEMA]** "JSON Schema: A Media Type for Describing JSON Documents". https://json-schema.org/

- **[MCP]** "Model Context Protocol Specification". https://modelcontextprotocol.io/

- **[NDJSON]** "Newline Delimited JSON". http://ndjson.org/

### Informative References

- **[GITHUB-ACTIONS]** "GitHub Actions Documentation". https://docs.github.com/actions

- **[GITHUB-API]** "GitHub REST API Documentation". https://docs.github.com/rest

- **[YAML]** "YAML Ain't Markup Language (YAML™) Version 1.2". https://yaml.org/spec/1.2/spec.html

- **[SAFE-OUTPUT-MESSAGES]** "Safe Output Messages Design System". ../safe-output-messages.md

- **[SAFE-OUTPUT-ENV]** "Safe Output Environment Variables Reference". ../safe-output-environment-variables.md

---

## Change Log

### Version 1.0.0 (Recommendation)

**Initial Release** - 2026-01-20

- Defined four-layer architecture (frontmatter, MCP, guardrails, application)
- Specified three conformance levels (Basic, Standard, Complete)
- Documented security model and threat prevention
- Specified builtin system tools (missing-tool, missing-data, noop)
- Outlined GitHub operations (full specifications in appendices)
- Defined compliance testing requirements
- Established RFC 2119 conformance keywords

**Design Decisions**:
- Separated builtin tools from GitHub operations for clarity and portability
- Used NDJSON for structured output to support streaming and line-based processing
- Enforced read-only agent permissions to prevent prompt injection attacks
- Provided staged mode for safe testing and preview
- Applied conservative defaults (max: 1) to prevent accidental resource exhaustion

---

*Copyright © 2026 GitHub. All rights reserved.*
