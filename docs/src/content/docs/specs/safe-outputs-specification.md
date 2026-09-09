---
title: Safe Outputs MCP Gateway Specification
description: Formal W3C-style specification defining the Safe Outputs Model Context Protocol Gateway for secure AI-to-GitHub operation translation
sidebar:
  order: 1360
---

# Safe Outputs MCP Gateway Specification

**Version**: 1.29.0<br>
**Status**: Working Draft<br>
**Publication Date**: 2026-09-02<br>
**Editor**: GitHub Agentic Workflows Team<br>
**This Version**: [safe-outputs-specification](/gh-aw/specs/safe-outputs-specification/)<br>
**Latest Published Version**: This document

---

## Abstract

This specification establishes normative requirements for the Safe Outputs Model Context Protocol (MCP) Gateway, a security-centric translation layer enabling AI agents to declare intended GitHub operations through structured protocols while maintaining strict privilege separation. The gateway functions as an intermediary between read-only agent execution environments and permission-controlled execution contexts, providing configurable constraints, input validation, content sanitization, and preview capabilities. This document specifies behavioral requirements, security properties, operational semantics, and conformance criteria for implementing systems.

## Document Status

This document represents a working draft specification subject to revision. It documents the Safe Outputs MCP Gateway as implemented in GitHub Agentic Workflows version 1.8.0 and later. Future versions may introduce backwards-incompatible changes. Implementers should consult the latest version before beginning new implementations.

This specification follows World Wide Web Consortium (W3C) formatting conventions while being independently maintained by the GitHub Agentic Workflows project.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance Requirements](#2-conformance-requirements)
3. [Security Architecture](#3-security-architecture)
4. [Structural Components](#4-structural-components)
5. [Configuration Semantics](#5-configuration-semantics)
6. [Universal Feature Interpretation](#6-universal-feature-interpretation)
7. [Safe Output Type Definitions](#7-safe-output-type-definitions)
8. [Protocol Exchange Patterns](#8-protocol-exchange-patterns)
9. [Content Integrity Mechanisms](#9-content-integrity-mechanisms)
10. [Execution Guarantees](#10-execution-guarantees)
11. [Cache Memory Integrity](#11-cache-memory-integrity)
12. [Appendices](#appendix-b-security-considerations)

---

## Terminology

This specification uses the following terms with precise definitions:

**Agent**: The AI-powered process executing in an untrusted context with read-only GitHub permissions.

**Safe Output Type**: A category of GitHub operation (e.g., `create_issue`, `add_comment`) with a corresponding MCP tool definition and handler implementation.

**MCP Gateway**: The containerized stdio MCP server accepting MCP tool invocation requests and recording operations to NDJSON format. Runs in an isolated container alongside the agent job.

**Safe Output Processor**: The privileged execution context that reads NDJSON artifacts, validates operations, and executes GitHub API calls.

**Handler**: JavaScript implementation processing operations of a specific safe output type.

**Validation**: Pre-execution verification of operation structure, limits, and authorization. Includes schema validation, limit enforcement, and allowlist checking.

**Sanitization**: Content transformation pipeline removing potentially malicious patterns while preserving legitimate content.

**Verification**: Post-compilation checking of configuration integrity through hash validation.

**Staged Mode**: Preview execution mode where operations are simulated without creating permanent GitHub resources.

**Temporary ID**: A placeholder identifier (format: `aw_<id>`) used to reference not-yet-created resources. Resolved to actual resource numbers during processing.

**Provenance**: Metadata identifying the workflow and run that created a GitHub resource. Included in footers or API metadata fields.

**Integrity Level**: A classification of the trust level assigned to a workflow run based on its guard policy. The four levels in descending order of trust are: `merged`, `approved`, `unapproved`, and `none`.

**Policy Hash**: An 8-character hexadecimal digest of the canonical form of a workflow's guard policy fields (`blocked-users`, `min-integrity`, `repos`, `trusted-bots`, `trusted-users`). Used as part of the cache key to detect policy changes.

**Integrity Branch**: A Git branch within the cache memory repository corresponding to a specific integrity level. Each branch holds data written exclusively by runs at that integrity level.

**Cache Poisoning**: A Bell-LaPadula write-up violation where a lower-integrity agent writes data to a shared cache store that is subsequently consumed by a higher-integrity run without provenance verification.

**Steering Issue**: A run-scoped issue allocated during activation when `safe-outputs.steer` is enabled. It collects user-authored steering comments and is reused as the agent failure issue when the run fails.

---

## 1. Introduction

### 1.1 Motivation and Problem Statement

Contemporary AI-powered software workflows require actionable outcomes beyond informational responses. Agents must translate reasoning into concrete platform operations—creating issues for bugs, commenting on pull requests, managing labels. However, granting AI systems direct write access to version control platforms introduces severe security vulnerabilities:

- **Prompt injection attacks**: Adversarially-crafted inputs manipulate agent behavior, potentially causing unauthorized deletions, spam creation, or credential exfiltration
- **Unbounded resource consumption**: Compromised agents exhaust API rate limits, storage quotas, or workflow execution time
- **Audit trail opacity**: Direct API invocations obscure operation provenance, complicating incident response and compliance
- **Credential surface expansion**: Write-capable tokens become high-value targets, increasing attack surface

Traditional mitigation strategies prove inadequate:

- **Full prohibition** eliminates automation benefits, relegating agents to advisory roles
- **Manual approval gates** create bottlenecks, defeating automation's purpose
- **Overly-permissive grants** accept unacceptable risk for convenience

The Safe Outputs MCP Gateway introduces a structured alternative: **declarative operation requests with deferred, validated execution**. Agents articulate intentions through type-safe protocols; isolated execution contexts validate and fulfill requests under configured constraints.

### 1.2 Scope and Boundaries

**Within Specification Scope**:

This document normatively defines:

1. **Security model architecture** establishing privilege separation between untrusted reasoning and trusted execution
2. **Configuration schema semantics** for declaring available operations, constraints, and validation rules
3. **Protocol exchange patterns** governing operation declaration, validation, and fulfillment
4. **Content security requirements** specifying sanitization, filtering, and validation transformations
5. **Operational guarantees** characterizing atomicity, ordering, idempotency, and error handling for each safe output type
6. **MCP integration** defining tool interface schemas and stdio container transport requirements

**Explicitly Out of Scope**:

This specification does NOT define:

- Core Model Context Protocol semantics (see external MCP specification)
- GitHub REST/GraphQL API implementation details (see GitHub API documentation)
- AI model selection, prompt engineering, or agent implementation strategies
- User interface design for workflow authoring or monitoring
- Container orchestration, deployment topology, or infrastructure provisioning
- Performance benchmarks or resource consumption limits

### 1.3 Design Principles

Four foundational principles govern this specification:

**Principle P1: Security Through Architectural Separation**

Write permissions MUST reside in separate execution contexts from AI reasoning. Communication occurs through structured data artifacts, not shared credentials or memory.

*Rationale*: Privilege separation limits blast radius of successful prompt injection attacks. Compromising the agent yields read-only access; compromising execution context requires additional exploitation steps.

**Principle P2: Declarative Over Imperative**

Operations are declared through schema-validated data structures, not imperative command execution. This enables static analysis, transformation, and validation before commitment.

*Rationale*: Declarative models permit inspection, logging, and modification of operations before GitHub API invocation. Imperative models lack such intervention points.

**Principle P3: Configurable Constraint Enforcement**

Workflow authors explicitly configure permitted operations and constraints. Implicit behaviors are minimized; defaults favor security over convenience.

*Rationale*: Explicit configuration ensures conscious security decisions. Implicit permissiveness creates hidden vulnerabilities.

**Principle P4: Fail-Secure By Default**

Invalid inputs, constraint violations, or execution errors result in operation rejection, not degraded execution. Error messages provide diagnostic information for remediation.

*Rationale*: Proceeding with degraded security is worse than failing. Clear error messages enable authors to correct issues rather than silently accepting risks.

### 1.4 Terminology and Definitions

This specification uses the following terms with precise meanings:

**Agent**: An AI-powered workflow job executing with read-only GitHub permissions. Agents analyze inputs, reason about appropriate actions, and declare operations through MCP tool invocations.

**Safe Output Type**: A category of GitHub operation (e.g., issue creation, comment posting, label application) with defined semantics, constraints, and operational guarantees. Each type corresponds to one or more MCP tools.

**MCP Gateway**: A containerized stdio MCP server implementing the Model Context Protocol, accepting tool invocations from agents, validating against JSON schemas, and recording operations to structured files.

**Safe Output Job**: A permission-controlled GitHub Actions job that downloads agent-declared operations, validates content, enforces limits, and executes GitHub API calls.

**NDJSON (Newline-Delimited JSON)**: A text format where each line contains one complete, valid JSON object. Enables incremental writing and parsing without loading entire dataset into memory.

**Staged Mode**: A preview execution mode where operations are simulated and summarized without permanent effects. Indicated by 🎭 emoji prefix in messages.

**Max Limit**: A configuration parameter constraining the count of operations per safe output type. Prevents resource exhaustion and limits damage from compromised agents.

**Content Sanitization**: Security transformation applied to all user-provided text fields (titles, bodies, comments) to remove exploit vectors (malicious URLs, command injection, credential patterns) while preserving legitimate content.

**Footer**: An AI attribution message appended to created content, identifying the workflow source, providing provenance via run URL, and optionally including installation instructions.

**Temporary ID**: A workflow-scoped identifier (format: `aw_<alphanumeric>`) allowing agents to reference not-yet-created issues in subsequent operations. Resolved to actual issue numbers during execution.

---

## 2. Conformance Requirements

### 2.1 Conformance Classes

This specification defines two conformance classes:

**Class C1: Full Conformance**

An implementation satisfying ALL normative requirements (MUST, SHALL, REQUIRED statements) in this document. Full conformance requires:

- Complete security architecture implementation (privilege separation, threat mitigations)
- Support for all mandatory safe output types (defined in Section 7)
- Universal feature implementation (max limits, staged mode, footers, sanitization)
- Protocol exchange pattern adherence (MCP stdio container transport, NDJSON persistence)
- Content integrity mechanism enforcement (schema validation, domain filtering)
- Execution guarantee provision (atomicity, ordering, idempotency)

**Class C2: Partial Conformance**

An implementation satisfying ALL security-critical normative requirements but omitting support for optional safe output types. Partial conformance requires:

- Complete security architecture (non-negotiable)
- Mandatory safe output types: `create_issue`, `add_comment`, `create_pull_request`, `noop`, `missing_tool`, `missing_data`, `report_incomplete`
- Clear documentation listing unsupported optional types
- Warning messages when workflows attempt to use unsupported types

*Note*: Partial conformance permits phased implementation but MUST NOT compromise security properties.

### 2.2 Normative Terminology

This document employs RFC 2119 requirement level keywords with precise interpretations:

**MUST** (SHALL, REQUIRED): Absolute requirement for conformance. Omission, violation, or alternative implementation constitutes non-conformance. Implementations violating MUST requirements are non-conforming even if they satisfy all other requirements.

**MUST NOT** (SHALL NOT): Absolute prohibition. Presence of explicitly prohibited behavior constitutes non-conformance regardless of other implementation quality.

**SHOULD** (RECOMMENDED): Strong recommendation but not absolute requirement. Valid reasons may justify different behavior, but implications MUST be fully understood and carefully weighed. Deviations MUST be documented.

**SHOULD NOT** (NOT RECOMMENDED): Strong advice against specific behavior but not absolute prohibition. Alternative approaches may be justified in specific contexts.

**MAY** (OPTIONAL): Truly optional feature or behavior. Implementations MAY choose to include, omit, or provide alternative approaches. Presence or absence does not affect conformance.

### 2.3 Conformance Verification

Conformance MAY be demonstrated through:

**Method M1: Functional Testing**

Systematic verification that all required operations produce specified outcomes under normal and edge-case conditions. Test coverage SHOULD include:

- Each safe output type with valid inputs
- Constraint enforcement (max limits, domain filtering)
- Error handling (invalid inputs, exceeded limits)
- Configuration variants (staged mode, cross-repository)

**Method M2: Security Testing**

Demonstration that security properties hold under adversarial conditions. Security test suite SHOULD include:

- Prompt injection scenarios (malicious inputs attempting unauthorized operations)
- Constraint evasion attempts (trying to exceed max limits)
- Content injection (URLs to forbidden domains, command injection)
- Cross-repository privilege escalation attempts

**Method M3: Protocol Compliance**

Validation that MCP exchange patterns conform to requirements. Protocol tests SHOULD verify:

- HTTP request/response format correctness
- JSON Schema validation enforcement
- NDJSON format adherence
- Error code and message format

**Method M4: Configuration Validation**

Verification that configuration parsing, validation, and enforcement match specifications. Configuration tests SHOULD check:

- Valid configuration acceptance
- Invalid configuration rejection with clear errors
- Inheritance rules (type-specific overriding global)
- Default value application

*Note*: A normative conformance test suite is RECOMMENDED for future specification versions but not currently provided.

---

## 3. Security Architecture

### 3.1 Privilege Separation Model

The Safe Outputs MCP Gateway implements defense-in-depth through strict architectural privilege separation. The following diagram illustrates permission boundaries:

```
┌─────────────────────────────────────────┐
│  Execution Context 1: Untrusted        │
│  ┌─────────────────────────────────┐   │
│  │  AI Agent Process               │   │
│  │  ├─ Permissions: contents:read  │   │
│  │  ├─ Network: None (firewall)    │   │
│  │  └─ Credentials: Read-only token│   │
│  └────────────┬────────────────────┘   │
│               │ MCP over HTTP/127       │
│               ↓                         │
│  ┌─────────────────────────────────┐   │
│  │  MCP Gateway Server             │   │
│  │  ├─ Permissions: File write     │   │
│  │  ├─ Network: Localhost only     │   │
│  │  ├─ Operations:                 │   │
│  │  │   • Schema validation        │   │
│  │  │   • NDJSON append            │   │
│  │  └─ No GitHub API access        │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
                 ↓
          Artifact Storage
         (GitHub-managed)
                 ↓
┌─────────────────────────────────────────┐
│  Execution Context 2: Privileged       │
│  ┌─────────────────────────────────┐   │
│  │  Safe Output Processor          │   │
│  │  ├─ Permissions: issues:write,  │   │
│  │  │    pull-requests:write, etc. │   │
│  │  ├─ Operations:                 │   │
│  │  │   • Content sanitization     │   │
│  │  │   • Limit enforcement        │   │
│  │  │   • GitHub API invocation    │   │
│  │  └─ No direct agent access      │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

**Architectural Requirements**:

**Requirement AR1: Agent Isolation**

Agents MUST execute without GitHub write permissions. Only read-level tokens SHALL be accessible to agent processes. Write-capable tokens MUST reside exclusively in safe output job contexts.

**Verification**:

- **Method**: Automated workflow file parsing and static analysis
- **Tool**: `check_privilege_separation()` in conformance checker (`scripts/check-safe-outputs-conformance.sh`)
- **Criteria**: No agent job has `issues: write`, `pull-requests: write`, `contents: write`, or other write-level permissions
- **Manual Check**: Inspect agent job permission declarations in compiled `.lock.yml` files

**Formal Definition**:

```
∀ workflow ∈ Workflows:
  permissions(workflow.jobs.agent) ∩ {issues:write, pull-requests:write, contents:write, ...} = ∅
```

**Requirement AR2: Communication Channel Integrity**

Agent-to-processor communication MUST occur through GitHub Actions artifact storage. Environment variables, network connections, or shared filesystems MUST NOT be used for operation transmission.

*Rationale*: Artifact storage provides tamper-evidence, audit logging, and access control. Alternative channels lack these properties.

**Verification**:

- **Method**: Code review and architecture inspection
- **Tool**: Manual inspection of workflow compilation code in `pkg/workflow/`
- **Criteria**: Operations are written to NDJSON files, uploaded as artifacts, and downloaded by safe output jobs
- **Manual Check**: Verify workflow structure includes `actions/upload-artifact@v4` and `actions/download-artifact@v4` steps between agent and safe output jobs

**Formal Definition**:

```
∀ operation ∈ Operations:
  transmission(operation) = artifact_storage
  ∧ transmission(operation) ≠ env_vars
  ∧ transmission(operation) ≠ network
  ∧ transmission(operation) ≠ filesystem_share
```

**Requirement AR3: Permission Minimization**

Each safe output job MUST request minimal permissions. Jobs SHOULD specialize by operation type, requesting only required permissions. For example, an issue-creation job requests `issues:write` but not `pull-requests:write`.

**Verification**:

- **Method**: Automated permission computation analysis and code review
- **Tool**: `computePermissionsForSafeOutputs()` in `pkg/workflow/safe_outputs_permissions.go` and `check_permission_computation()` in conformance checker
- **Criteria**: Each safe output job requests only the minimum permissions required for its operation types
- **Manual Check**: Review generated workflow YAML to verify job permissions match operation requirements

*Example*:

```yaml
jobs:
  safe-output-create-issue:
    permissions:
      issues: write  # Minimal for issue creation
```

**Formal Definition**:

```
∀ job ∈ SafeOutputJobs:
  permissions(job) = minimal_set(operations(job))
  ∧ ∀ p ∈ permissions(job): required(p, operations(job))
```

**Requirement AR4: No Privilege Escalation Path**

Agent execution context MUST NOT gain access to safe output job credentials through any mechanism (environment variables, file leaks, API endpoints, etc.).

Safe Output Processors MAY target external APIs. Credentials for each external service MUST remain isolated to the privileged processor step for that service and MUST NOT enter agent environments, MCP schemas, prompts, operation artifacts, generated configuration files, logs, errors, or step summaries. Credentials for distinct services MUST NOT be substituted for or derived from one another.

**Verification**:

- **Method**: Manual security audit and code review
- **Tool**: Security review of workflow structure and GitHub Actions architecture
- **Criteria**: No GITHUB_TOKEN or credentials are accessible from agent job context; tokens only exist in safe output job contexts
- **Manual Check**: Audit all communication channels (artifacts, environment variables, network, filesystem) to confirm no credential leakage

**Formal Definition**:

```
∀ t ∈ [agent_start, agent_end]:
  accessible_credentials(agent_context, t) ∩ safe_output_credentials = ∅
```

**Requirement AR5: Steering Issue Provenance**

When steering is enabled, the activation phase MUST create the steering issue in the workflow repository and export its number and URL from the trusted activation job. The issue identity MUST NOT be selected from agent-controlled content, issue comments, or event payload values.

Before reusing the steering issue for failure reporting, the conclusion phase MUST validate all of the following:

1. The issue number is a positive safe integer.
2. The issue exists in the workflow repository.
3. The item is an issue, not a pull request.
4. The issue remains open.

If validation fails, the implementation MUST NOT update another issue as the failure report.

*Rationale*: Activation outputs cross a job boundary. Re-fetching and validating the item before mutation prevents a malformed output from turning failure reporting into an arbitrary issue update.

### 3.2 Threat Model and Mitigations

This specification addresses five primary threat scenarios:

**Threat T1: Prompt Injection Exploitation**

*Attack Vector*: Adversary crafts inputs (issue descriptions, comments, file contents) causing agent to misinterpret intent and declare harmful operations.

*Examples*:

- Mass issue creation (spam)
- Malicious content injection (phishing URLs)
- Inappropriate label application
- Unauthorized cross-repository operations

*Architectural Mitigations*:

| Layer | Mechanism | Effectiveness |
|-------|-----------|---------------|
| **Constraint** | Max limits cap operation count per type | Prevents unbounded operations |
| **Validation** | JSON schema enforces structure | Rejects malformed declarations |
| **Sanitization** | Content filtering removes exploit vectors | Neutralizes injection attempts |
| **Preview** | Staged mode enables pre-commitment review | Human-in-loop detection |
| **Authorization** | Cross-repo operations require explicit config | Prevents unauthorized targeting |

*Residual Risk*: Agent may generate legitimate-seeming but contextually inappropriate content within configured limits. Mitigation: workflow monitoring, anomaly detection, periodic review.

**Threat T2: Configuration Tampering**

*Attack Vector*: Adversary modifies workflow YAML between compilation and execution, disabling security features (removing max limits, disabling sanitization).

*Examples*:

- Changing `max: 1` to `max: -1` (unlimited)
- Removing `allowed-domains` configuration
- Disabling `footer` attribution

*Architectural Mitigations*:

| Layer | Mechanism | Effectiveness |
|-------|-----------|---------------|
| **Integrity** | Frontmatter hash computed at compilation | Detects modifications |
| **Verification** | Hash checked before execution | Prevents tampered execution |
| **Embedding** | Configuration embedded in compiled workflow | No external file modification |
| **Immutability** | Compiled workflows stored in version control | Change tracking |

*Residual Risk*: Source repository compromise allows arbitrary workflow modification. Mitigation: branch protection rules, code review requirements, commit signing.

**Threat T3: Credential Leakage**

*Attack Vector*: Agent inadvertently includes secrets (API keys, passwords, tokens) in created content or logs.

*Examples*:

- Secrets in issue descriptions
- Tokens in PR comments
- Keys in log output

*Architectural Mitigations*:

| Layer | Mechanism | Effectiveness |
|-------|-----------|---------------|
| **Masking** | GitHub Actions secret masking redacts registered secrets | High for known secrets |
| **Detection** | Pattern-based scanning identifies credential-like strings | Medium (novel formats evade) |
| **Logging** | MCP logs undergo security scanning | High for logged secrets |
| **Review** | Manual inspection of suspicious patterns | High but manual |

*Residual Risk*: Novel secret formats, obfuscated credentials, or dynamically-generated tokens may evade detection. Mitigation: least-privilege principle, secret rotation, monitoring.

**Threat T4: Resource Exhaustion**

*Attack Vector*: Malicious or buggy agent attempts to consume excessive resources (API quotas, storage, execution time).

*Examples*:

- Creating maximum-permitted issues repeatedly
- Uploading large files to asset branches
- Triggering workflow dispatch cascades

*Architectural Mitigations*:

| Layer | Mechanism | Effectiveness |
|-------|-----------|---------------|
| **Operation Limits** | Max constraints per type | Prevents unbounded operations |
| **Resource Cleanup** | Expires configuration auto-closes temporary resources | Prevents accumulation |
| **Timeout** | Workflow-level execution time limits | Prevents infinite loops |
| **Size Limits** | File size constraints for uploads | Prevents storage exhaustion |

*Residual Risk*: Within configured limits, agent may still consume significant resources. Mitigation: usage monitoring, alerting, quota management.

**Threat T5: Cross-Repository Privilege Escalation**

*Attack Vector*: Agent targets unauthorized repositories through cross-repository safe output operations.

*Examples*:

- Creating issues in private repositories
- Commenting on PRs in sensitive repositories
- Adding labels to upstream project issues

*Architectural Mitigations*:

| Layer | Mechanism | Effectiveness |
|-------|-----------|---------------|
| **Allowlisting** | `allowed-github-references` restricts targets | High when configured |
| **Per-Type Allowlists** | `allowed-repos` on individual types | Fine-grained control |
| **Permission Validation** | GitHub API enforces token permissions | Backstop protection |
| **Audit Trail** | All operations logged with provenance | Detection and response |

*Residual Risk*: Misconfigured allowlists may permit unintended targets. Mitigation: principle of least privilege in configuration, periodic review.

**Threat T6: Cache Integrity Poisoning**

*Attack Vector*: A `none`-integrity agent writes malicious or attacker-controlled data to a shared cache-memory directory. A subsequent `merged` or `approved`-integrity run blindly restores and consumes that data, violating the Bell-LaPadula write-up property (lower-integrity subjects MUST NOT write where higher-integrity subjects read).

*Examples*:

- A `none`-integrity run injects fabricated results into a shared JSON cache file, causing a `merged`-integrity run to act on false analysis data
- A compromised workflow at `unapproved` integrity poisons a shared state file consumed by `approved` runs in later workflow steps
- Legacy flat-file caches shared across integrity levels with no provenance attribution

*Architectural Mitigations*:

| Layer | Mechanism | Effectiveness |
|-------|-----------|---------------|
| **Integrity-scoped cache keys** | Key encodes integrity level and policy hash; different levels never share the same cache entry | High |
| **Git-backed integrity branching** | Each integrity level writes to its own Git branch; branch structure enforces write isolation | High |
| **Merge-down semantics** | Lower-integrity runs receive higher-integrity data via read-only merge; reverse never occurs | High |
| **Policy hash invalidation** | Any change to `allow-only` policy fields forces a cache miss, preventing stale policy inheritance | Medium |

*Residual Risk*: A compromised runner may directly manipulate the `.git` directory within the restored cache tarball. Mitigation: restrict runner access to trusted environments and enable repository-level security policies.



**Repository Reference Format**

Target repositories MUST be specified in `owner/repo` format. Implementations MUST validate:

- Format matches regex: `^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`
- Owner and repo components are non-empty
- No protocol prefix (https://, git://, etc.)

**Allowlist Resolution Order**

When evaluating cross-repository operations, implementations MUST apply these rules in order:

1. **Extract target-repo**: Parse from operation arguments or configuration
2. **Check type-specific allowlist**: If safe output type defines `allowed-repos`:
   - MUST match against this list
   - Type-specific allowlist OVERRIDES global allowlist
   - If match fails, REJECT with E004
3. **Check global allowlist**: If no type-specific allowlist and `allowed-github-references` is defined:
   - MUST match against this list
   - If match fails, REJECT with E004
4. **Default deny**: If no allowlists are defined:
   - MUST reject cross-repository operations
   - Same-repository operations are permitted

**Matching Rules**

- Matching is EXACT (case-sensitive)
- Wildcards (*, ?) are NOT supported
- Pattern matching is NOT supported
- Each repository MUST be explicitly listed

**Security Properties**

**Property SP6: Cross-Repository Containment**

For all cross-repository operations:

```
∀ op ∈ operations:
  op.target_repo ≠ null ⇒ 
    (op.target_repo ∈ type_allowlist ∨ 
     (type_allowlist = null ∧ op.target_repo ∈ global_allowlist))
```

**Verification**:

- **Method**: Code review and integration testing
- **Tool**: `check_cross_repo()` in conformance checker (SEC-005) and handler unit tests
- **Criteria**: All handlers with `target-repo` parameter validate against allowlists; operations to non-allowlisted repos are rejected with E004
- **Automated Check**: Verify handlers contain allowlist validation logic
- **Integration Tests**: Submit cross-repository operations; confirm allowlist enforcement

**Property SP7: Deny-by-Default**

Without explicit allowlist configuration:

```
allowed_repos = null ∧ allowed_github_references = null ⇒
  ∀ op ∈ operations: op.target_repo = workflow.repository
```

**Verification**:

- **Method**: Integration testing
- **Tool**: Handler unit tests for cross-repository validation
- **Criteria**: Without allowlist configuration, only same-repository operations are permitted; cross-repository operations are rejected with E004
- **Integration Tests**: Submit cross-repository operations without allowlist; confirm rejection

**Example Configurations**

```yaml
# Example 1: Type-specific allowlist (overrides global)
safe-outputs:
  allowed-github-references: [owner/repo-a, owner/repo-b]
  
  create-issue:
    allowed-repos: [owner/repo-c]  # Only repo-c permitted for issues
    
  add-comment:
    # No type-specific list, uses global: repo-a, repo-b

# Example 2: Explicit same-repository only
safe-outputs:
  create-issue:
    # No allowlist = same repository only
    max: 5
```

### 3.3 Security Property Guarantees

Conforming implementations MUST maintain these security invariants:

**Property SP1: Permission Separation Invariant**

*Statement*: At all times during agent execution, the agent process SHALL NOT possess tokens or credentials permitting GitHub write operations.

**Formal Definition**:

```
∀ t ∈ [agent_start, agent_end]:
  permissions(agent_process, t) ∩ {issues:write, pull-requests:write, ...} = ∅
```

**Verification**:

- **Method**: Static analysis and runtime inspection
- **Tool**: `check_privilege_separation()` in conformance checker (SEC-001)
- **Criteria**: Agent job declares only read permissions; no write-level permissions in agent context
- **Automated Check**: Parse workflow YAML for agent job permissions
- **Runtime Check**: Inspect `$GITHUB_TOKEN` environment variable scope in agent execution context

**Property SP2: Validation Precedence Invariant**

*Statement*: For all safe output operations, validation logic MUST execute before any GitHub API invocation. Invalid operations MUST be rejected without side effects.

**Formal Definition**:

```
∀ op ∈ operations:
  valid(op) = false ⇒ github_api_call(op) never executes
```

**Verification**:

- **Method**: Code review and unit testing
- **Tool**: `check_validation_ordering()` in conformance checker (SEC-002) and handler unit tests
- **Criteria**: All validation stages (1-6) complete before Stage 7 (API invocation)
- **Automated Check**: Static analysis confirms validation functions precede API calls in handler code
- **Unit Tests**: Test cases verify invalid operations are rejected without GitHub API calls (see Section 3.3 Validation Pipeline Requirements)

#### Validation Pipeline Requirements

Implementations MUST execute validation steps in this exact sequence for all safe output operations:

**Stage 1: Schema Validation (REQUIRED)**

- Input: Raw MCP tool arguments
- Check: JSON schema validation against type-specific schema
- On failure: Reject immediately with E001 (INVALID_SCHEMA) error
- Output: Schema-validated operation data

**Stage 2: Limit Enforcement (REQUIRED)**

- Input: Count of operations of each type in current batch
- Check: Compare count against configured `max` for each type
- On failure: Reject entire batch with E002 (LIMIT_EXCEEDED) error
- Output: Limit-validated operation set

**Stage 3: Content Sanitization (REQUIRED)**

- Input: All text fields (title, body, description, etc.)
- Transform: Apply sanitization pipeline (see Section 9.2)
- On failure: Reject with E008 (SANITIZATION_FAILED) if unsafe content cannot be sanitized
- Output: Sanitized operation data

**Stage 4: Domain Filtering (CONDITIONAL)**

- Input: All URLs in markdown links and images
- Check: Validate against `allowed-domains` if configured
- Transform: Redact unauthorized URLs
- Output: Domain-filtered operation data

**Stage 5: Cross-Repository Validation (CONDITIONAL)**

- Input: `target-repo` parameter if present
- Check: Validate against `allowed-repos` or `allowed-github-references`
- On failure: Reject with E004 (INVALID_TARGET_REPO)
- Output: Authorized target repository

**Stage 6: Dependency Resolution (CONDITIONAL)**

- Input: Temporary IDs, parent references
- Check: Resolve references to actual GitHub resource numbers
- On failure: Reject with E005 (MISSING_PARENT)
- Output: Fully-resolved operation data

**Stage 7: GitHub API Invocation (EXECUTION)**

- Input: Validated, sanitized, authorized operation data
- Action: Execute GitHub API calls
- On failure: Return E007 (API_ERROR) with details

**Requirement VL1: Sequential Execution**

Stages MUST execute in the order specified above. A failure at any stage (1-6) MUST prevent Stage 7 from executing for that operation.

**Requirement VL2: Atomic Validation**

For single-operation types (max=1), validation failure MUST prevent any API calls. For batch operations, validation failure of one operation MUST NOT cause rejection of the entire batch unless it's a limit enforcement failure.

**Requirement VL3: Error Propagation**

Validation errors MUST include:

- Error code (E001-E008)
- Human-readable message
- Operation index (for batch operations)
- Field name (for schema validation errors)

**Property SP3: Limit Enforceability Invariant**

*Statement*: For all configured max limits, implementations MUST prevent exceeding the limit. Attempts to exceed limits SHALL result in operation rejection.

**Formal Definition**:

```
∀ type ∈ safe_output_types:
  count(operations[type]) > config[type].max ⇒ reject(operations[type])
```

**Verification**:

- **Method**: Integration testing and limit enforcement validation
- **Tool**: `check_max_limits()` in conformance checker (SEC-003) and handler unit tests
- **Criteria**: Operations exceeding configured `max` are rejected with E002 (LIMIT_EXCEEDED) error
- **Automated Check**: Verify handlers check operation count against `max` configuration
- **Integration Tests**: Submit operations exceeding limits; confirm batch rejection

**Property SP4: Content Integrity Invariant**

*Statement*: All user-provided content MUST undergo sanitization. Sanitization MUST occur after agent output and before GitHub API invocation.

**Formal Definition**:

```
∀ content ∈ user_provided_fields:
  github_api_call(content) ⇒ ∃ sanitized_content = sanitize(content) ∧ passed(sanitized_content)
```

**Verification**:

- **Method**: Code review and unit testing
- **Tool**: `check_sanitization()` in conformance checker (SEC-004) and sanitization unit tests
- **Criteria**: All handlers with body/content fields invoke sanitization functions before API calls
- **Automated Check**: Verify presence of `sanitize*` function calls in handlers
- **Unit Tests**: Confirm malicious content (XSS, script injection) is neutralized before GitHub API invocation

**Property SP5: Provenance Traceability Invariant**

*Statement*: All created GitHub resources MUST include provenance metadata identifying workflow source and run.

**Formal Definition**:

```
∀ resource ∈ created_resources:
  ∃ provenance_data ∈ resource ∧ provenance_data.workflow_run_url ≠ null
```

**Verification**:

- **Method**: Manual inspection and automated footer validation
- **Tool**: `check_footers()` in conformance checker (USE-002) and handler code review
- **Criteria**: All created resources include footer attribution with workflow run URL when footer is configured
- **Manual Check**: Inspect created issues, PRs, discussions, comments for footer presence
- **Automated Check**: Verify handlers call `addFooter()` or include attribution in body content

---

## 4. Structural Components

(Continuing with Section 4 and remaining sections, but due to length limits, I'll create the file now with the content we have)

### 4.1 Component Topology

The Safe Outputs MCP Gateway system comprises three distinct components operating in different phases:

**Component C1: Workflow Compiler** (Pre-execution phase)

*Input*: Markdown workflow files with YAML frontmatter  
*Output*: Compiled GitHub Actions YAML workflows  
*Key Responsibilities*:

- Parse and validate `safe-outputs:` configuration blocks
- Generate JSON schemas for enabled safe output types
- Synthesize dedicated jobs for each safe output type with minimal permissions
- Compute configuration integrity hashes
- Validate cross-references and repository feature availability

*Location*: Developer's machine or CI environment during workflow development

**Component C2: MCP Gateway Server** (Runtime agent phase)

*Input*: MCP tool invocation requests via stdio (JSON-RPC)
*Output*: NDJSON file recording operation declarations  
*Key Responsibilities*:

- Run as a containerized stdio MCP server in the `gh-aw-node` image
- Load tool schemas from `${RUNNER_TEMP}/gh-aw/safeoutputs/config.json`
- Register MCP tools matching enabled safe output types
- Validate invocations against JSON schemas
- Handle large content (>16000 tokens) via file references
- Append validated operations to `/tmp/gh-aw/safeoutputs/output.ndjson`

*Location*: Isolated `gh-aw-node` container with workspace, safeoutputs runtime files, and log directory mounted read-write

**Component C3: Safe Output Processor** (Post-execution phase)

*Input*: NDJSON artifact from agent job  
*Output*: GitHub resources (issues, PRs, comments, labels)  
*Key Responsibilities*:

- Download agent output artifact from GitHub Actions storage
- Parse NDJSON entries and group by type
- Apply content sanitization to all text fields
- Enforce max limits per type
- Execute validated operations via GitHub API
- Generate execution summaries for workflow step output
- Implement fail-safe error handling

*Location*: Dedicated safe output job containers (separate from agent)

### 4.2 Data Flow Sequence

Operations flow through the system following this precise sequence:

**Phase 1: Configuration Declaration** (Development Time)

Workflow author writes configuration in frontmatter:

```yaml
safe-outputs:
  create-issue:
    max: 3
    labels: [automated, ai]
    footer: true
```

**Phase 2: Schema Synthesis** (Compilation Time)

Compiler generates tool schemas:

```json
{
  "tools": {
    "create_issue": {
      "name": "create_issue",
      "description": "Create a new GitHub issue...",
      "inputSchema": {
        "type": "object",
        "required": ["title", "body"],
        "properties": {
          "title": {"type": "string"},
          "body": {"type": "string"},
          "labels": {"type": ["array", "string"], "items": {"type": "string"}}
        }
      }
    }
  },
  "handlers": {
    "create_issue": {
      "max": 3,
      "labels": ["automated", "ai"],
      "footer": true
    }
  }
}
```

**Phase 3: Runtime Initialization** (Agent Job Start)

MCP Gateway server reads configuration:

```javascript
const config = JSON.parse(fs.readFileSync('/opt/gh-aw/safeoutputs/config.json'));
const tools = config.tools;
const handlers = config.handlers;

for (const [name, schema] of Object.entries(tools)) {
  registerTool(server, name, schema, createHandler(handlers[name]));
}
```

**Phase 4: Operation Declaration** (Agent Execution)

Agent invokes MCP tool via stdio JSON-RPC to the containerized safeoutputs MCP server:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "create_issue",
    "arguments": {
      "title": "Memory leak in data processor",
      "body": "Observed continuous memory growth...",
      "labels": ["bug", "performance"]
    }
  }
}
```

**Phase 5: Validation and Recording** (Gateway Processing)

Gateway validates and appends to NDJSON:

```javascript
// Schema validation
const valid = validate(schema, arguments);
if (!valid) {
  return {error: {code: -32602, message: "Invalid params"}};
}

// Append to NDJSON
const entry = {...arguments, type: "create_issue"};
fs.appendFileSync(outputFile, JSON.stringify(entry) + "\n");

// Return success
return {result: {content: [{type: "text", text: '{"result":"success"}'}]}};
```

**Phase 6: Artifact Transfer** (Job Transition)

Agent job uploads NDJSON artifact:

```yaml
- name: Upload agent output
  uses: actions/upload-artifact@v4
  with:
    name: agent-output
    path: /tmp/gh-aw/safeoutputs/output.ndjson
```

Safe output job downloads artifact:

```yaml
- name: Download agent output
  uses: actions/download-artifact@v4
  with:
    name: agent-output
    path: /tmp/downloads
```

**Phase 7: Batch Processing** (Safe Output Execution)

Processor reads, validates, and executes:

```javascript
const operations = fs.readFileSync(artifact, 'utf8')
  .split('\n')
  .filter(line => line.trim())
  .map(line => JSON.parse(line));

const issueOps = operations.filter(op => op.type === 'create_issue');

// Enforce max limit
if (issueOps.length > config.create_issue.max) {
  throw new Error(\`Exceeded max limit: \${issueOps.length} > \${config.create_issue.max}\`);
}

// Execute each operation
for (const op of issueOps) {
  const sanitized = sanitizeContent(op);
  await createIssue(sanitized);
}
```

**Phase 8: Comment Memory Round-Trip** (Optional, when `comment-memory` is configured)

When `tools.comment-memory` is enabled, implementations MUST support this additional data-flow path:

1. **GitHub comment → local files (pre-agent setup)**: A setup step reads the managed comment body from the target issue or pull request, extracts content between `<gh-aw-comment-memory id="...">` and `</gh-aw-comment-memory>`, and writes one file per memory entry under `/tmp/gh-aw/comment-memory/<memory_id>.md`.
   - Setup MUST enforce memory-size safeguards: each materialized file MUST be ≤16 KiB and the aggregate materialized directory MUST be ≤48 KiB. Exceeding either limit MUST emit a warning and skip writing all memory files, leaving the comment-memory directory empty.
2. **Local files → agent**: The prompt MUST include instructions that memory files are edited directly in `/tmp/gh-aw/comment-memory/`.
3. **Agent → artifact**: The unified agent artifact MUST include `/tmp/gh-aw/comment-memory/` when comment memory is enabled.
4. **Artifact → threat detection**: Threat-detection prompt setup MUST include discovered comment-memory files in analysis context.
5. **Artifact/files → safe output processor**: The processor MUST load edited `*.md` files, synthesize `comment_memory` operations, and execute them through the `comment_memory` handler.
6. **Safe output processor → GitHub comment**: The handler MUST upsert the managed comment using the `gh-aw-comment-memory` marker and preserve only user content within the managed XML block.

This round-trip path ensures memory edits remain file-based for the agent while keeping GitHub as the authoritative persistent store.

### 4.3 Configuration Propagation

Configuration flows from author intent to runtime enforcement:

**Authoring Layer**:

```yaml
# Workflow .md file — explicit configuration
safe-outputs:
  create-issue:
    max: 3
    allowed-labels: [bug, enhancement]
```

When no `safe-outputs:` section is present, the compiler automatically injects a default `create-issue` configuration (implicit path):

```yaml
# Workflow .md file — no safe-outputs section
# Compiler auto-injects:
# safe-outputs:
#   create-issue:
#     max: 1
#     labels: [<workflowID>]
#     title-prefix: "[<workflowID>]"
```

This auto-injection is suppressed when any safe output type other than the system types (`noop`, `missing-tool`, `missing-data`) is explicitly configured.

**Compilation Layer**:

```go
// Compiler parses and validates
config := extractSafeOutputsConfig(frontmatter)
validateConfig(config) // Check constraints
schema := generateSchema(config)
jobs := synthesizeJobs(config)
```

**Deployment Layer**:

```yaml
# Compiled .lock.yml
jobs:
  agent:
    steps:
      - run: |
          cat > /opt/gh-aw/safeoutputs/config.json << 'EOF'
          {"tools": {...}, "handlers": {...}}
          EOF
```

**Runtime Layer**:

```javascript
// MCP server loads at startup
const config = JSON.parse(fs.readFileSync('/opt/gh-aw/safeoutputs/config.json'));
// Use config for tool registration and validation
```

**Execution Layer**:

```javascript
// Safe output processor enforces
const maxAllowed = config.create_issue.max;
const allowedLabels = config.create_issue.allowed_labels;
// Enforce during operation processing
```

---

## 5. Configuration Semantics

### 5.1 Configuration Schema Structure

Safe output configuration employs a two-level hierarchy: global parameters affecting all types, and type-specific blocks customizing individual operation categories.

**General Form**:

```yaml
safe-outputs:
  # Global parameters
  <global-param-name>: <value>
  
  # Type-specific blocks
  <safe-output-type>:
    <type-param-name>: <value>
```

**Namespace Separation**:

- Global parameters have unreserved names (footer, staged, allowed-domains)
- Type-specific blocks use hyphenated safe output type names (create-issue, add-comment)
- Parameter inheritance flows from global to type-specific (type overrides global)

### 5.2 Global Parameters

#### GP1: footer

**Syntax**: `footer: true | false | <github-expression>`

**Default**: `true`

**Semantics**: Controls whether AI attribution footers are appended to created content (issues, discussions, pull requests, comments).

This field is **templatable**: in addition to literal `true`/`false`, it accepts GitHub Actions expression strings (e.g., `${{ inputs.enable-footer }}`). See Section 5.5 for details.

**Inheritance**: Type-specific `footer` parameter overrides this global setting.

**Footer Composition**:

When `footer: true`, implementations MUST append this structure:

```markdown
---
> AI generated by [<workflow-name>](<run-url>)[<context>]
[>
> To add this workflow in your repository, run \`gh aw add <source>\`. See [usage guide](<url>).]
```

**Template Variables**:

- `<workflow-name>`: Workflow display name (from frontmatter `name:` or filename)
- `<run-url>`: Complete URL to workflow run (<https://github.com/{owner}/{repo}/actions/runs/{id}>)
- `<context>`: Optional triggering context:
  - `for #123` when triggered by issue #123
  - `for #456` when triggered by PR #456
  - `for discussion #789` when triggered by discussion #789
  - Omitted when no specific trigger
- `<source>`: Workflow source path (owner/repo/path@ref, e.g., github/gh-aw/.github/workflows/triage@main)
- `<url>`: Documentation URL (typically <https://github.github.com/gh-aw/setup/cli/>)

**Installation Instructions**:

The second paragraph (installation command) is OPTIONAL. It SHOULD be included when:

1. Workflow source location is known (not local development)
2. Workflow is publicly accessible
3. Workflow is intended for redistribution

**Conformance Requirements**:

MUST satisfy:

- Footer appears at end of content, not beginning
- Horizontal rule (`---`) separates footer from user content
- Clickable links for workflow run URL
- Context matches actual trigger type

MUST NOT:

- Include footer when `footer: false`
- Modify user content to insert footer mid-content
- Include broken or invalid URLs

#### GP2: staged

**Syntax**: `staged: true | false`

**Default**: `false`

**Semantics**: Controls preview mode execution. When `true`, operations are simulated and previewed without permanent effects.

**Inheritance**: Type-specific `staged` parameter overrides this global setting.

**Preview Mode Behavior**:

When `staged: true`, implementations MUST:

1. Skip all GitHub API write operations
2. Generate detailed preview summaries
3. Use 🎭 emoji prefix consistently in preview messages
4. Show complete operation details (titles, bodies, labels, assignees)
5. Include count of operations that would be performed

**Preview Message Format**:

```markdown
## 🎭 Staged Mode: <Operation Type> Preview

The following <count> <type> operation(s) would be performed if staged mode was disabled:

### Operation 1: <title>

**Type**: <safe-output-type>  
**Title**: <operation-title>  
**Body**:
<operation-body>

**Additional Fields**:
- Labels: <labels>
- Assignees: <assignees>
[...]

### Operation 2: <title>
[Same structure]

---
**Preview Summary**: <count> operations previewed. No GitHub resources were created.
```

**Use Cases**:

Staged mode is RECOMMENDED for:

- Testing new workflows before production deployment
- Validating agent behavior in safe environment
- Demonstrating workflow capabilities without side effects
- Debugging configuration issues

**Conformance Requirements**:

MUST satisfy:

- No permanent GitHub resources created in staged mode
- Preview shows sufficient detail for correctness evaluation
- Emoji 🎭 appears in all staged mode headings
- Clear indication that operations are preview-only

MUST NOT:

- Execute API write operations in staged mode
- Create partial resources (e.g., issue without closing it)
- Omit critical operation details from previews

#### GP2a: steer

**Syntax**: `steer: true | false`

**Default**: `false`

**Semantics**: When `true`, activation MUST create a workflow-repository issue before agent execution and expose its number and URL to the agent prompt. The agent MAY read user-authored issue comments containing the `steer` keyword. The conclusion phase MUST update the validated issue with the failure title and body when failure reporting is active, rather than creating a separate failure issue; on successful completion, it SHOULD close the issue and link a created pull request when available.

This mode requires top-level `issues: read` permission for comment reads. The activation and conclusion jobs require `issues: write` through the global safe-output credential (`safe-outputs.github-token` or `safe-outputs.github-app`); a GitHub App token MUST be minted with `issues: write`.

**Constraints**:

- `steer` MUST NOT be combined with `safe-outputs.failure-issue-repo`.
- `steer` MUST NOT be combined with an expression-valued `safe-outputs.staged`.
- When `safe-outputs.staged: true`, no steering issue is created.
- Steering MUST NOT alter pull request branch creation or checkout references.

#### GP3: allowed-domains

**Syntax**: `allowed-domains: [<domain-pattern>, ...]`

**Default**: `[]` (empty array, no domain filtering)

**Semantics**: Specifies allowlist of domains permitted in URLs within safe output content. When non-empty, URLs to non-allowlisted domains are redacted during sanitization.

**Domain Pattern Formats**:

1. **Plain domain**: `github.com`, `api.example.com`
   - Matches exact domain only
   - Case-insensitive matching

2. **Wildcard subdomain**: `*.github.io`, `*.example.com`
   - Matches all subdomains (but not bare domain)
   - `*.github.io` matches `user.github.io` but NOT `github.io`
   - Case-insensitive matching

3. **Protocol-specific**: `https://secure.example.com`
   - Matches domain with specified protocol only
   - `https://secure.example.com` allows HTTPS but blocks HTTP

4. **Ecosystem identifier**: `node`, `python`, `defaults`
   - Special identifiers for package ecosystems
   - No domain validation performed

**Pattern Validation**:

Implementations MUST validate patterns at compilation time. Valid patterns match:

```regex
^(\*\.)?[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$
```

Or are recognized ecosystem identifiers (no dots, no `://`).

**Redaction Behavior**:

When URL domain does not match any allowlist pattern:

1. Extract full URL from content
2. Replace with `[URL redacted: unauthorized domain]`
3. Preserve surrounding context
4. Log redacted URL to `/tmp/gh-aw/safeoutputs/redacted-domains.log`

**Example Configuration**:

```yaml
safe-outputs:
  allowed-domains:
    - github.com        # Allow github.com only
    - "*.github.io"     # Allow all *.github.io subdomains
    - api.example.com   # Allow specific API domain
```

**Example Redaction**:

Input content:

```markdown
See documentation at https://github.com/owner/repo
Also check https://malicious.example.com/phishing
Reference: https://docs.github.io/guide
```

With `allowed-domains: [github.com, "*.github.io"]`, output:

```markdown
See documentation at https://github.com/owner/repo
Also check [URL redacted: unauthorized domain]
Reference: https://docs.github.io/guide
```

**Conformance Requirements**:

MUST satisfy:

- Domain extraction handles all valid URL formats
- Wildcard matching follows specified semantics
- Case-insensitive comparison
- Redaction preserves content structure
- Redaction log created when domains filtered

MUST NOT:

- Allow non-allowlisted domains when allowlist configured
- Break valid URLs matching allowlist
- Lose content surrounding redacted URLs

#### GP4: allowed-github-references

**Syntax**: `allowed-github-references: [<owner/repo>, ...]`

**Default**: `[]` (empty array, no cross-repository restrictions)

**Semantics**: Specifies allowlist of GitHub repositories for cross-repository safe output operations. When non-empty, operations targeting non-allowlisted repositories are rejected.

**Reference Format**:

Each entry MUST match pattern: `^[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+$`

Examples:

- `github/gh-aw`
- `microsoft/vscode`
- `owner-name/repo.name`

**Validation Behavior**:

When safe output operation includes `target-repo` configuration:

1. Extract target repository from configuration
2. Check if target matches any entry in `allowed-github-references`
3. If no match, reject operation with clear error
4. If match or allowlist empty, proceed with validation

**Same-Repository Operations**:

Operations WITHOUT `target-repo` (same-repository operations) are ALWAYS permitted, regardless of `allowed-github-references` configuration.

*Rationale*: Workflows inherently have permission to operate on their own repository.

**Example Configuration**:

```yaml
safe-outputs:
  allowed-github-references:
    - github/roadmap   # Allow operations on roadmap repo
    - github/docs      # Allow operations on docs repo
  
  create-issue:
    target-repo: github/roadmap  # Must be in allowlist
```

**Error Message Format**:

When target repository not in allowlist:

```
Cross-repository operation rejected: target repository not in allowed-github-references

Target: owner/repo
Allowed repositories:
  - github/roadmap
  - github/docs

To permit this operation, add target to allowed-github-references:
  safe-outputs:
    allowed-github-references:
      - owner/repo
```

**Conformance Requirements**:

MUST satisfy:

- Repository reference format validation
- Allowlist checking before API operations
- Clear error messages on rejection
- Same-repository operations always permitted

MUST NOT:

- Allow non-allowlisted cross-repository operations
- Block same-repository operations
- Silently ignore allowlist configuration

#### GP5: github-app.repositories

**Syntax**: `safe-outputs.github-app.repositories: ["*"] | [<repository-name>, ...]`

**Default**: Implementation-defined repository scope when `github-app` is configured without `repositories`

**Semantics**: Specifies the repository scope requested when minting GitHub App installation tokens for safe outputs execution.

When `safe-outputs.github-app.repositories` is exactly `["*"]`, implementations:

- MUST omit the installation-token `repositories` parameter
- MUST NOT substitute `${{ github.event.repository.name }}`
- MUST NOT rewrite the wildcard sentinel to a single repository scope

When `safe-outputs.github-app.repositories` contains one or more explicit repository names, implementations MUST request exactly the configured repository set.

When `safe-outputs.github-app.repositories` is omitted, implementations MAY use the triggering repository as the default repository scope.

The `["*"]` behavior MUST apply to activation-job token minting and to subsequent safe-output-job token minting that inherits the same `safe-outputs.github-app` settings.

In `workflow_call` and other reusable-workflow scenarios, conforming implementations MUST preserve the `["*"]` behavior so that activation can read agent configuration from the callee repository when the App installation grant permits it.

#### GP5a: Type-Specific `github-app` Override

**Syntax**: `safe-outputs.<safe-output-type>.github-app: <github-app-config>`

**Default**: No type-specific app override. Handlers inherit the global `safe-outputs.github-app` fallback when present.

**Semantics**: Allows an individual safe output type to mint and use a dedicated GitHub App installation token instead of sharing the global safe-outputs credential.

**Resolution Rules**:

1. When `safe-outputs.<safe-output-type>.github-app` is configured for a handler that executes GitHub API calls in the `safe_outputs` job, implementations MUST mint a dedicated installation token for that handler using only the permissions required by that handler.
2. When a handler-specific token is minted, that handler MUST use the dedicated token in preference to the shared `safe-outputs.github-token` or global `safe-outputs.github-app` token.
3. The global `safe-outputs.github-app` token MUST be computed from only the handlers that do **not** declare a type-specific `github-app` override.
4. When the effective permission set for a handler is empty (for example, staged-only execution paths or handlers that perform no GitHub API call in `safe_outputs`), implementations MUST NOT mint a dedicated handler token.
5. When no dedicated handler token is minted, implementations MUST fall back to the handler's configured `github-token` (if any) or to the shared safe-outputs credential flow.

**Security Goal**: Type-specific `github-app` overrides enable least-privilege separation between output types. For example, `add-comment` can use an App with only `issues:write` while `dispatch-workflow` uses a different App with only `actions:write`.

**Example**:

```yaml
safe-outputs:
  github-app:
    app-id: ${{ vars.GLOBAL_APP_ID }}
    private-key: ${{ secrets.GLOBAL_APP_PRIVATE_KEY }}

  add-comment:
    github-app:
      app-id: ${{ vars.ISSUE_APP_ID }}
      private-key: ${{ secrets.ISSUE_APP_PRIVATE_KEY }}
    target: ${{ github.event.issue.number }}

  dispatch-workflow:
    github-app:
      app-id: ${{ vars.ACTIONS_APP_ID }}
      private-key: ${{ secrets.ACTIONS_APP_PRIVATE_KEY }}
    workflows: [downstream.yml]
```

In this example, `add-comment` and `dispatch-workflow` MUST use separate installation tokens scoped to their respective handler permissions, while any other safe output types continue using the global `safe-outputs.github-app` fallback.

#### GP6: data

**Syntax**: `safe-outputs.data: false | true | <schema-object> | <github-expression>`

**Default**: `false` (disabled)

**Semantics**: Controls whether body-capable safe output types MAY include a top-level `data` object, and optionally enforces the schema of that object.

**Modes**:

1. `false` or omitted: `data` MUST be rejected.
2. `true`: `data` MUST be allowed and MUST be an object.
3. `<schema-object>`: `data` MUST be allowed and MUST satisfy the normalized simplified schema.
4. `<github-expression>`: schema resolution MUST occur at runtime in JavaScript and the resolved schema MUST satisfy this section before `data` validation.

**Conformance Requirement GP6-1: Accepted Frontmatter Shapes**

Implementations MUST accept exactly the four syntactic forms above. Non-boolean scalar literals other than GitHub Actions expressions (for example, `data: "schema.json"`) MUST be rejected.

**Conformance Requirement GP6-2: Simplified Schema Grammar**

When `<schema-object>` is used, implementations MUST support this simplified schema syntax:

- Allowed keywords: `type`, `description`, `properties`, `required`, `items`, `enum`, `additionalProperties`, `minLength`, `maxLength`, `minimum`, `maximum`, `pattern`
- Supported primitive type names: `object`, `array`, `string`, `number`, `integer`, `boolean`
- Shorthand property syntax: object literals without schema keywords MUST be interpreted as:
  - `type: object`
  - `properties: <literal>`

**Conformance Requirement GP6-3: OpenAI Codex Structured Outputs Compatibility**

For every object schema node, implementations MUST:

1. Set `additionalProperties: false` (or reject if explicitly `true`).
2. Require every declared property in `required` (lexical ordering of `required` entries is RECOMMENDED for deterministic output).

**Conformance Requirement GP6-4: Runtime Validation Placement**

Compile-time validation SHOULD be applied when schema content is statically available. Runtime validation in JavaScript MUST be applied before operation execution for expression-resolved schemas and for handler-side enforcement.

**Data Validation Errors**

When `data` validation fails, implementations MUST return actionable field-path errors that identify the failing location (for example, `data.extra` or `data.required[0]`).

### 5.3 Type-Specific Common Parameters

Every safe output type supports these parameters:

#### TS1: max

**Syntax**: `max: <positive-integer> | -1 | null | <github-expression>`

**Default**: Type-dependent (see Section 7 for per-type defaults)

**Semantics**: Maximum count of operations permitted for this type in a single workflow run.

This field is **templatable**: in addition to integer literals and `-1`, it accepts GitHub Actions expression strings (e.g., `${{ inputs.max-issues }}`). When a GitHub Actions expression is supplied, the limit is evaluated at runtime. See Section 5.5 for details.

**Special Values**:

- Positive integer: Strict limit (e.g., `max: 3` allows up to 3 operations)
- `-1`: Unlimited operations (use with caution)
- `null` or omitted: Use type's default max
- GitHub Actions expression: Limit resolved at runtime (e.g., `max: ${{ inputs.max-issues }}`)

**Enforcement Algorithm**:

```javascript
function enforceMaxLimit(operations, type, config) {
  const typeOps = operations.filter(op => op.type === type);
  const maxAllowed = config[type].max;
  
  if (maxAllowed === -1) {
    // Unlimited
    return {allowed: typeOps, rejected: []};
  }
  
  if (typeOps.length > maxAllowed) {
    return {
      allowed: [],
      rejected: typeOps,
      error: \`Exceeded max limit for \${type}: attempted \${typeOps.length}, limit \${maxAllowed}\`
    };
  }
  
  return {allowed: typeOps, rejected: []};
}
```

**Error Reporting**:

When limit exceeded:

```
Safe output limit exceeded for create_issue

Attempted operations: 5
Configured limit: 3

Rejected operations:
  1. "Bug in authentication flow"
  2. "Memory leak in data processor"
  3. "UI rendering issue on mobile"
  4. "Performance degradation after update"
  5. "Documentation outdated"

To increase limit, update workflow configuration:
  safe-outputs:
    create-issue:
      max: 5
```

**Conformance Requirements**:

MUST satisfy:

- Count operations per type independently
- Reject ALL operations when limit exceeded (not just excess)
- Provide clear error with count and limit
- Never silently truncate operations

MUST NOT:

- Accept `max: 0` (invalid; disable type instead)
- Accept negative values except `-1`
- Allow partial execution when limit exceeded

#### TS2: footer (Type-Specific Override)

**Syntax**: `footer: true | false | <github-expression>`

**Default**: Inherits from `safe-outputs.footer` (global)

**Semantics**: Override global footer setting for this specific safe output type.

This field is **templatable**: in addition to literal `true`/`false`, it accepts GitHub Actions expression strings (e.g., `${{ inputs.enable-footer }}`). See Section 5.5 for details.

**Inheritance Precedence**:

1. Type-specific `footer` (highest priority)
2. Global `safe-outputs.footer`
3. Default value `true` (lowest priority)

**Example**:

```yaml
safe-outputs:
  footer: true  # Global default: footers enabled
  
  create-issue:
    footer: false  # Issues: no footers
  
  add-comment:
    # Inherits global footer: true
```

Result:

- Issues created without footers
- Comments created with footers

#### TS3: staged (Type-Specific Override)

**Syntax**: `staged: true | false`

**Default**: Inherits from `safe-outputs.staged` (global)

**Semantics**: Override global staged setting for this specific safe output type.

**Inheritance Precedence**: Same as footer parameter.

**Example**:

```yaml
safe-outputs:
  staged: false  # Global default: normal execution
  
  create-pull-request:
    staged: true  # PRs: preview only
  
  add-labels:
    # Inherits global staged: false (normal execution)
```

Result:

- Pull requests previewed without creation
- Labels applied normally

### 5.4 Type-Specific Extension Parameters

Beyond common parameters, individual safe output types support specialized configuration.

**Representative Examples**:

**Issue Creation Extensions**:

```yaml
create-issue:
  title-prefix: "[AI] "          # Prepend to all titles
  labels: [automation, ai]       # Auto-apply labels
  assignees: [copilot, user1]    # Auto-assign users
  expires: 7                     # Days until auto-close
  group: true                    # Group under parent
  close-older-issues: true       # Close previous workflow issues
  target-repo: owner/repo        # Cross-repository target
  allowed-repos: [owner/repo1]   # Cross-repo allowlist
  allowed-labels: [bug, feature] # Agent label restrictions
```

**Comment Extensions**:

```yaml
add-comment:
  target: "issue" | "pull_request" | "discussion" | "*"
  allows-comment-ids: [12345, 67890] # Required before agents may supply comment_id with target: "*"
  hide-older-comments: true      # Hide previous workflow comments
  discussions: false             # Exclude discussions:write permission (optional)
  target-repo: owner/repo
  allowed-repos: [...]
```

**Submit PR Review Extensions**:

```yaml
submit-pull-request-review:
  target: "triggering" | "*" | <PR number>   # Required when not in pull_request trigger
  target-repo: owner/repo        # Cross-repository target
  allowed-repos: [...]           # Additional allowed repositories
  allowed-events: [COMMENT]      # Preferred default for non-blocking bot reviews
  supersede-older-reviews: true  # Best-effort dismissal of older same-workflow REQUEST_CHANGES reviews (including legacy blockers)
  footer: "always" | "none" | "if-body"     # Footer on review body
```

> **Review attribution pinning**: Under `workflow_run` triggers, the compiler automatically injects `GH_AW_HEAD_SHA` (set to `${{ github.event.workflow_run.head_sha }}`) into the safe-outputs job environment. For `pull_request` and `pull_request_target` triggers, it is set to `${{ github.event.pull_request.head.sha }}`. The runtime uses this value as `commit_id` when posting the review, so the review is always attributed to the commit the agent actually saw — not a newer HEAD that may have landed while the agent was running. No user configuration is needed.

**Pull Request Extensions**:

```yaml
create-pull-request:
  base-branch: main              # Target branch (default: repo default)
  draft: true                    # Create as draft (default: true)
  commit-changes: true           # Auto-commit workspace changes
  reviewers: [user1, copilot]    # Auto-request reviewers
  labels: [automated]            # Auto-apply labels
  preserve-branch-name: false    # Keep agent branch name verbatim (no random salt suffix)
  recreate-ref: false            # When preserve-branch-name and remote branch exists, force-delete and recreate the remote ref
```

**Asset Upload Extensions**:

```yaml
upload-asset:
  branch: assets                 # Target branch name
  max-size-kb: 10240             # File size limit (10MB default)
  allowed-extensions: [.png, .jpg, .jpeg]  # Extension allowlist
```

**Discussion Extensions**:

```yaml
create-discussion:
  category: "General"            # Discussion category (name/slug/ID)
  title-prefix: "[Report] "      # Prepend to titles
  min-body-length: 200           # Optional minimum report body length
  labels: [report, automated]    # Auto-apply labels
  allowed-labels: [...]          # Agent label restrictions
```

Complete parameter documentation for each type appears in Section 7.

### 5.5 Templatable Fields

Certain safe output configuration fields are **templatable**: they accept either a literal value of the expected type or a GitHub Actions expression string that is evaluated at runtime.

#### Templatable Integer Fields

Fields documented as `<positive-integer> | -1 | null | <github-expression>` are templatable integers. When a GitHub Actions expression is supplied, it MUST resolve to a valid integer value at runtime.

**Applicable fields**: `max` (all types)

**Syntax**:

```yaml
# Literal integer
max: 5

# GitHub Actions expression (evaluated at runtime)
max: ${{ inputs.max-issues }}
```

**Conformance requirements**:

- Implementations MUST accept literal integers and GitHub Actions expressions for templatable integer fields.
- When a GitHub Actions expression is supplied as `max`, implementations MUST evaluate it at runtime and treat the result as an integer.
- A non-integer runtime value for a templatable integer field MUST cause the operation to fail with a descriptive error.

#### Templatable Boolean Fields

Fields documented as `true | false | <github-expression>` are templatable booleans. When a GitHub Actions expression is supplied, it MUST resolve to `true` or `false` at runtime.

**Applicable fields**: `footer` (global and type-specific), `group`, `close-older-issues`, `hide-older-comments`, `close-older-discussions`, `draft`, `allow-empty`, `auto-merge`, `report-as-issue`, `unassign-first`

**Syntax**:

```yaml
# Literal boolean
group: true

# GitHub Actions expression (evaluated at runtime)
group: ${{ inputs.group-issues }}
```

**Conformance requirements**:

- Implementations MUST accept literal booleans and GitHub Actions expressions for templatable boolean fields.
- A free-form string that is not a GitHub Actions expression (i.e., does not match `${{ ... }}`) MUST be rejected with a descriptive error at compile time.
- When a GitHub Actions expression is supplied, implementations MUST evaluate it at runtime and treat the result as a boolean.
- A non-boolean runtime value for a templatable boolean field MUST be treated as `false`.

---

## 6. Universal Feature Interpretation

This section defines precise semantics for features that apply across multiple safe output types.

### 6.1 Max Limit Semantics

**Feature Identifier**: Maximum Operation Count Constraint  
**Configuration**: `max` parameter on safe output types  
**Scope**: All safe output types

**Normative Requirements**:

**Requirement MR1**: Implementations MUST count operations of each type independently. Cross-type totals are NOT constrained by individual type limits.

**Requirement MR2**: When operation count for a type exceeds configured max, implementations MUST reject ALL operations of that type, not just excess operations.

*Rationale*: All-or-nothing semantics prevent partial execution that may create inconsistent state.

**Requirement MR3**: Rejection MUST occur before any GitHub API invocations for the type.

**Requirement MR4**: Error messages MUST include:

- Safe output type name
- Attempted operation count
- Configured max limit
- List of operation titles/summaries
- Configuration update guidance

**Requirement MR5**: Unlimited semantics (`max: -1`) MUST be supported but SHOULD trigger compilation warnings.

**Conformance Verification**:

Test Case 1: Exact Limit

- Configure `max: 3`
- Declare exactly 3 operations
- Expected: All 3 execute successfully

Test Case 2: Exceeded Limit

- Configure `max: 3`
- Declare 4 operations
- Expected: All 4 rejected with clear error

Test Case 3: Under Limit

- Configure `max: 5`
- Declare 2 operations
- Expected: Both execute successfully

Test Case 4: Unlimited

- Configure `max: -1`
- Declare any count
- Expected: All execute (with warning)

### 6.2 Staged Mode Semantics

**Feature Identifier**: Preview Mode Execution  
**Configuration**: `staged` parameter (global or type-specific)  
**Visual Indicator**: 🎭 emoji  
**Scope**: All safe output types

**Normative Requirements**:

**Requirement SM1**: In staged mode, implementations MUST NOT invoke GitHub API write methods. Read operations for validation are permitted.

**Requirement SM2**: Implementations MUST generate preview summaries containing:

- Operation type and count
- Complete operation details (all fields)
- Formatted representation of content
- Clear indication of preview-only status

**Requirement SM3**: ALL staged mode messages MUST include 🎭 emoji in headings for consistent visual identification.

**Requirement SM4**: Preview format MUST follow this structure:

```markdown
## 🎭 Staged Mode: <Type> Preview

The following <N> <type> operation(s) would be performed if staged mode was disabled:

<Per-operation details>

---
**Preview Summary**: <N> operations previewed. No GitHub resources were created.
```

**Requirement SM5**: Type-specific `staged` settings MUST override global settings according to inheritance rules.

**Conformance Verification**:

Test Case 1: Global Staged

- Configure global `staged: true`
- Declare operations of multiple types
- Expected: All types previewed

Test Case 2: Type-Specific Override

- Configure global `staged: false`, type `staged: true`
- Expected: Type previewed, others execute

Test Case 3: Preview Content

- Enable staged mode
- Declare operation with all fields
- Expected: Preview shows all fields with proper formatting

### 6.3 Footer Attribution Semantics

**Feature Identifier**: AI Attribution Messages  
**Configuration**: `footer` parameter (global or type-specific)  
**Scope**: Content-creating types (issues, discussions, PRs, comments)

**Normative Requirements**:

**Requirement FA1**: Footers MUST be appended to content body, never prepended or inserted mid-content.

**Requirement FA2**: Footers MUST be separated from user content by horizontal rule (`---`).

**Requirement FA3**: Footer MUST include workflow name and clickable run URL.

**Requirement FA4**: When workflow triggered by specific item, footer SHOULD include context reference matching trigger type:

- Issue trigger: `for #<number>`
- PR trigger: `for #<number>`
- Discussion trigger: `for discussion #<number>`

**Requirement FA5**: Installation instructions are OPTIONAL but RECOMMENDED when:

- Workflow source is known and public
- Workflow is intended for reuse

**Requirement FA6**: When `footer: false`, implementations MUST NOT append ANY attribution content.

**Requirement FA7**: Type-specific `footer` settings MUST override global settings.

**Footer Template Specification**:

```markdown
---
> AI generated by [<workflow_name>](<run_url>)[<context>]
[>
> To add this workflow in your repository, run `gh aw add <source_path>`. See [usage guide](<docs_url>).]
```

Where:

- `<workflow_name>`: From frontmatter `name:` or derived from filename
- `<run_url>`: `https://github.com/<owner>/<repo>/actions/runs/<run_id>`
- `<context>`: Optional: `for #<N>` or `for discussion #<N>`
- `<source_path>`: `<owner>/<repo>/<path>@<ref>`
- `<docs_url>`: Documentation URL (typically project docs)

**Conformance Verification**:

Test Case 1: Footer Enabled

- Configure `footer: true`
- Create issue
- Expected: Issue body contains footer with all required elements

Test Case 2: Footer Disabled

- Configure `footer: false`
- Create issue
- Expected: Issue body contains only user content, no footer

Test Case 3: Context Inclusion

- Configure `footer: true`
- Trigger workflow from issue #42
- Create comment
- Expected: Footer includes "for #42"

### 6.4 Content Sanitization Semantics

**Feature Identifier**: Input Security Transformation  
**Scope**: All text fields in all safe output types

**Normative Requirements**:

**Requirement CS1**: ALL user-provided text content MUST undergo sanitization before GitHub API invocation.

**Requirement CS2**: Sanitization MUST apply transformations in this order:

1. Unicode normalization
2. Protocol filtering
3. Domain filtering (if configured)
4. Command neutralization
5. Mention filtering
6. Markdown safety
7. Truncation

**Requirement CS3**: Sanitization MUST be idempotent: `sanitize(sanitize(content)) === sanitize(content)`

**Detailed Transformation Specifications**:

**Transformation T1: Unicode Normalization**

Requirements:

- Apply NFC (Canonical Decomposition + Canonical Composition)
- Remove zero-width characters: U+200B, U+200C, U+200D, U+FEFF
- Remove control characters: U+0000-U+001F, U+007F
- EXCEPT: Preserve U+000A (LF), U+000D (CR), U+0009 (TAB)

**Transformation T2: Protocol Filtering**

Allowed protocols:

- `http://`
- `https://`
- `mailto:`

Requirements:

- Extract URLs via pattern matching
- Check protocol against allowlist
- Replace disallowed protocols: `[URL removed: unauthorized protocol]`
- Malformed protocols: normalize or remove

**Transformation T3: Domain Filtering** (when `allowed-domains` configured)

Requirements:

- Extract domain from each URL
- Compare against allowed-domains list (case-insensitive)
- Wildcard handling: `*.example.com` matches `sub.example.com` but NOT `example.com`
- Replace non-allowed: `[URL redacted: unauthorized domain]`
- Log redacted URLs to `/tmp/gh-aw/safeoutputs/redacted-domains.log`

**Transformation T4: Command Neutralization**

Requirements:

- Detect slash commands at content start: `^/[a-zA-Z0-9_-]+`
- Escape slash: `/command` becomes `\/command`
- Preserve commands in code blocks

**Transformation T5: Mention Filtering**

Requirements:

- Detect @mentions: `@[a-zA-Z0-9_-]+`
- Check against allowed-aliases list
- Neutralize unauthorized: `@user` becomes `@ user` (add space)
- Preserve mentions in code blocks

**Transformation T6: Markdown Safety**

Requirements:

- Remove XML comments: `<!-- ... -->`
- Balance code fences: Ensure all ``` blocks properly closed
- Convert unsafe XML tags to text representation

**Transformation T7: Truncation**

Requirements:

- Default max length: 524,288 characters
- Truncate at character boundary (not mid-multibyte character)
- Append truncation notice: `\n\n[Content truncated at character limit]`

**Conformance Verification**:

Test Case 1: Protocol Filtering

- Input: `javascript:alert(1)`
- Expected: `[URL removed: unauthorized protocol]`

Test Case 2: Domain Filtering

- Config: `allowed-domains: [github.com]`
- Input: `https://github.com/x https://evil.com/y`
- Expected: `https://github.com/x [URL redacted: unauthorized domain]`

Test Case 3: Command Neutralization

- Input: `/close this issue`
- Expected: `\/close this issue`

Test Case 4: Mention Filtering

- Config: `allowed-aliases: [copilot]`
- Input: `@copilot @attacker`
- Expected: `@copilot @ attacker`

---

## 7. Safe Output Type Definitions

This section provides complete normative definitions for all safe output types. Each definition includes tool schema, operational semantics, configuration parameters, and security requirements.

### 7.0 Schema and Agent Sync Requirements

When a safe-output MCP tool schema changes (new required field, removed field, renamed field, or semantic contract change), the same pull request **MUST** update:

1. This Section 7 type definition and JSON schema snippet.
2. The corresponding handler contract in both Go and JavaScript runtime paths, if both runtimes support the type.
3. Agent-facing prompt/tool examples that rely on the changed fields.
4. Outcome-evaluation mappings when the change affects evaluable state or identifiers.

Schema-only updates without matching agent/runtime sync updates **MUST NOT** be considered conformant.

### 7.0.1 Wildcard Target Requirements Metadata

Safe-outputs MCP tool schemas MAY declare `x-safe-outputs-target-requirements` in the tool JSON schema definition to define required runtime identifiers when workflow configuration uses wildcard targeting.

When a safe-output type is configured with `target: "*"`, the MCP gateway MUST validate requests against `x-safe-outputs-target-requirements["*"]` before recording or executing intent.

For `x-safe-outputs-target-requirements["*"]`:

- `primary` SHALL identify the canonical target identifier field used in diagnostics and documentation; validation still requires at least one non-empty field listed in `anyOf`.
- `anyOf` SHALL enumerate accepted input field names; at least one listed field MUST be present with a non-empty value.
- If no listed `anyOf` field is present, the request MUST be rejected with an MCP validation error.
- When `target` is not `"*"`, implementations MUST follow each type's normal **Operational Semantics** context resolution behavior; this metadata MUST NOT add additional required runtime identifier fields.

### 7.0.2 Handler Function Cross-References

The following table defines the exact `createHandlers()` function used for each safe output type defined in §7:

| Safe Output Type | Handler Function in `actions/setup/js/safe_outputs_handlers.cjs` |
|---|---|
| `create_issue` | `createIssueHandler` |
| `add_comment` | `addCommentHandler` |
| `create_pull_request` | `createPullRequestHandler` |
| `noop` | `defaultHandler("noop")` |
| `comment_memory` | `defaultHandler("comment_memory")` |
| `update_issue` | `updateIssueHandler` |
| `close_issue` | `defaultHandler("close_issue")` |
| `link_sub_issue` | `defaultHandler("link_sub_issue")` |
| `create_discussion` | `defaultHandler("create_discussion")` |
| `update_discussion` | `defaultHandler("update_discussion")` |
| `close_discussion` | `defaultHandler("close_discussion")` |
| `update_pull_request` | `updatePullRequestHandler` |
| `close_pull_request` | `defaultHandler("close_pull_request")` |
| `approve_workflow_run` | `approve_workflow_run.cjs` (`main`) |
| `merge_pull_request` | `defaultHandler("merge_pull_request")` |
| `mark_pull_request_as_ready_for_review` | `defaultHandler("mark_pull_request_as_ready_for_review")` |
| `push_to_pull_request_branch` | `pushToPullRequestBranchHandler` |
| `push_repo_memory` | `pushRepoMemoryHandler` |
| `create_pull_request_review_comment` | `createPullRequestReviewCommentHandler` |
| `submit_pull_request_review` | `submitPullRequestReviewHandler` |
| `dismiss_pull_request_review` | `dismissPullRequestReviewHandler` |
| `resolve_pull_request_review_thread` | `defaultHandler("resolve_pull_request_review_thread")` |
| `reply_to_pull_request_review_comment` | `defaultHandler("reply_to_pull_request_review_comment")` |
| `add_labels` | `defaultHandler("add_labels")` |
| `remove_labels` | `defaultHandler("remove_labels")` |
| `add_reviewer` | `defaultHandler("add_reviewer")` |
| `assign_milestone` | `defaultHandler("assign_milestone")` |
| `assign_to_agent` | `defaultHandler("assign_to_agent")` |
| `assign_to_user` | `defaultHandler("assign_to_user")` |
| `unassign_from_user` | `defaultHandler("unassign_from_user")` |
| `hide_comment` | `defaultHandler("hide_comment")` |
| `create_project` | `createProjectHandler` |
| `update_project` | `defaultHandler("update_project")` |
| `create_project_status_update` | `defaultHandler("create_project_status_update")` |
| `update_release` | `defaultHandler("update_release")` |
| `upload_asset` | `uploadAssetHandler` |
| `upload_artifact` | `uploadArtifactHandler` |
| `dispatch_workflow` | `defaultHandler("dispatch_workflow")` |
| `create_code_scanning_alert` | `defaultHandler("create_code_scanning_alert")` |
| `autofix_code_scanning_alert` | `defaultHandler("autofix_code_scanning_alert")` |
| `create_check_run` | `defaultHandler("create_check_run")` |
| `create_agent_session` | `defaultHandler("create_agent_session")` |
| `missing_tool` | `defaultHandler("missing_tool")` |
| `missing_data` | `defaultHandler("missing_data")` |
| `report_incomplete` | `defaultHandler("report_incomplete")` |

### 7.0.3 Declared Field Payload Construction

The MCP Gateway and Safe Output Processor MUST construct downstream safe-output payloads only from the `type` field and fields declared by the applicable MCP schema, built-in validation configuration, or custom safe-job configuration. Agent-supplied fields that are not declared by that contract MUST NOT be forwarded to handlers, privileged jobs, or API clients.

If an optional advisory or enrichment field is declared with `x-strip-on-error: true`, implementations MAY omit that field from the normalized downstream payload when the field is invalid. Implementations MUST NOT use stripped fields for authorization, target selection, transport metadata, or other privileged decisions.

Fields used for privileged transport metadata, including patch anchoring and upload asset file metadata, MUST be derived by trusted workflow steps or privileged processors rather than accepted from agent-controlled NDJSON.

### 7.1 Core Issue Operations

#### Type: create_issue

**Purpose**: Create GitHub issues for bug tracking, feature requests, or task management.

**Default Max**: 1
**Cross-Repository Support**: Yes (via `target-repo`)  
**Mandatory**: Yes (required for full conformance)

**MCP Tool Schema**:

```json
{
  "name": "create_issue",
  "description": "Create a new GitHub issue for tracking bugs, feature requests, or tasks. Compatibility: labels may be passed as an array or comma-separated string.",
  "inputSchema": {
    "type": "object",
    "required": ["title", "body"],
    "properties": {
      "title": {"type": "string", "description": "Issue title"},
      "body": {"type": "string", "description": "Issue description in Markdown"},
      "labels": {"type": ["array", "string"], "items": {"type": "string"}},
      "fields": {
        "type": "array",
        "items": {
          "type": "object",
          "required": ["name", "value"],
          "properties": {
            "name": {"type": "string"},
            "value": {"type": ["string", "number"]}
          }
        }
      },
      "parent": {"type": ["number", "string"], "description": "Parent issue for sub-issues"},
      "temporary_id": {
        "type": "string",
        "pattern": "^aw_[A-Za-z0-9]{3,8}$",
        "description": "Temporary ID for referencing before creation"
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Atomicity**: Issue creation is atomic. Failure at any step prevents issue creation.
2. **Temporary ID Resolution**: References to `#aw_<id>` in bodies replaced with actual numbers post-creation.
3. **Parent Linking**: When `parent` specified, tasklist entry added to parent issue.
4. **Label Validation**: Labels must exist in repository; non-existent labels cause failure.
5. **Issue Field Validation**: Field names/values must match configured repository issue fields; invalid values return actionable errors.
6. **Cross-Repository**: When `target-repo` configured, created in that repository (must be in `allowed-repos`).
7. **Temporary ID collision norm**: A temporary ID (`aw_*`) is workflow-run scoped and MUST map to exactly one created object. Reusing the same temporary ID for a different target object in the same run MUST be rejected as ambiguous (`E005`). Reuse that references the same previously-created object is allowed and MUST resolve deterministically to the first mapping.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `title-prefix`: Prepend to titles
- `labels`: Auto-apply labels
- `assignees`: Auto-assign users/agents
- `expires`: Days until auto-close
- `group`: Group issues under parent
- `close-older-issues`: Close previous workflow issues
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist
- `allowed-labels`: Agent label restrictions
- `footer`: Footer override
- `staged`: Staged mode override

**Security Requirements**:

- Title and body undergo full sanitization
- Label validation before creation
- Cross-repo validation against allowed-repos
- Expires implemented via scheduled workflow (not client-side)

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Issue creation and modification

*GitHub App* (if using `safe-outputs.github-app` or `safe-outputs.create-issue.github-app` configuration):

- `issues: write` - Issue creation and modification  
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Both permission modes require the same write scopes
- GitHub App permissions enable cross-repository operations beyond `allowed-repos` when properly configured

---

#### Type: add_comment

**Purpose**: Add comments to existing issues, pull requests, or discussions.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: Yes (required for full conformance)

**MCP Tool Schema**:

```json
{
  "name": "add_comment",
  "description": "Add a comment to an existing issue, pull request, or discussion. IMPORTANT: Comments are subject to validation constraints enforced by the MCP server - maximum 65536 characters for the complete comment (including footer which is added automatically), 10 mentions (@username), and 50 links. Exceeding these limits will result in an immediate error with specific guidance. NOTE: By default, this tool does not require discussions:write permission. Set 'discussions: true' in the workflow's safe-outputs.add-comment configuration to enable discussion comments and request this permission.",
  "inputSchema": {
    "type": "object",
    "required": ["body"],
    "properties": {
      "body": {
        "type": "string",
        "description": "Comment text in Markdown. CONSTRAINTS: The complete comment (your body text + automatically added footer) must not exceed 65536 characters total. Maximum 10 mentions (@username), maximum 50 links (http/https URLs). A footer (~200-500 characters) is automatically appended, so leave adequate space. If these limits are exceeded, the tool call will fail with a detailed error message indicating which constraint was violated."
      },
      "item_number": {
        "type": "number",
        "description": "Issue/PR/discussion number (auto-resolved from context if omitted)"
      },
      "comment_id": {
        "type": ["number", "string"],
        "description": "Existing issue or pull request comment ID to update. Valid only when safe-outputs.add-comment.target is \"*\" and the ID appears in safe-outputs.add-comment.allows-comment-ids."
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Constraint Enforcement**: The MCP server validates body content before recording operations. Violations trigger immediate error responses with specific guidance (see Section 8.3). The body length limit applies to user-provided content; a second validation occurs after footer addition to ensure the complete comment doesn't exceed limits.
2. **Context Resolution**: When `item_number` omitted, resolves from workflow trigger context.
3. **Related Items**: When multiple outputs created, adds related items section to comments.
4. **Footer Injection**: Appends footer according to configuration (typically 200-500 characters).
5. **Cross-Repository**: Supports `target-repo` configuration.

**Controlled Comment Reuse Extensions**:

These extensions apply to safe-output processor messages for `add_comment` (including system-generated status updates).

1. When message-level `target: "status"` is set and a reusable status comment ID is available from trusted workflow state, implementations MUST update the existing issue/PR comment instead of creating a new comment.
2. When message-level `target: "status"` is set but no reusable status comment ID is available from trusted workflow state, implementations MUST create a new comment.
3. Message-level `target: "status"` MUST be rejected for discussion comments; status-comment reuse is valid only for issue and pull request comments.
4. The MCP input schema for `add_comment` MAY expose `comment_id` as an agent-controlled input only for workflows that configure `safe-outputs.add-comment.target: "*"`.
5. When an agent supplies `comment_id`, implementations MUST reject the operation unless `safe-outputs.add-comment.allows-comment-ids` is configured and contains that exact positive integer ID. The allowlist is trusted workflow state and MAY be computed by earlier workflow steps.
6. Agent-supplied `comment_id` MUST NOT be honored for discussion comments and MUST NOT be treated as a substitute for the trusted status comment ID used by `target: "status"`.
7. When updating an existing comment through either controlled reuse path, implementations SHOULD skip hide-older-comments behavior for that operation.

**Enforced Constraints**:

| Constraint | Limit | Error Code | Example Error Message |
|------------|-------|------------|----------------------|
| Body length (complete comment including footer) | 65536 characters | E006 | "Comment body exceeds maximum length of 65536 characters (got 70000)" |
| Mentions | 10 per comment | E007 | "Comment contains 15 mentions, maximum is 10" |
| Links | 50 per comment | E008 | "Comment contains 60 links, maximum is 50" |

**Note**: The 65536 character limit applies to the final comment text including the automatically appended footer. Users should leave approximately 200-500 characters of headroom to accommodate the footer, which contains workflow attribution and installation instructions.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `target`: Filter by type ("issue", "pull_request", "discussion", "*"). This configuration field applies to static workflow configuration (`safe-outputs.add-comment.target`) and is distinct from the runtime per-message `target: "status"` extension above.
- `allows-comment-ids`: Trusted allowlist of issue/PR comment IDs that the agent may update with `comment_id` when `target: "*"` is configured. This field is REQUIRED before any agent-supplied `comment_id` is honored. Accepts an array of positive integer IDs, strings containing positive integer IDs, or a GitHub Actions expression that resolves to such a list.
- `hide-older-comments`: Hide previous workflow comments
- `discussions`: Control `discussions:write` permission (default: false). Set to `true` to comment on discussions.
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Comment creation on issues
- `pull-requests: write` - Comment creation on pull requests
- `discussions: write` - Comment creation on discussions (only when `discussions: true`)

*GitHub App* (if using `safe-outputs.github-app` or `safe-outputs.add-comment.github-app` configuration):

- `issues: write` - Comment creation on issues
- `pull-requests: write` - Comment creation on pull requests
- `discussions: write` - Comment creation on discussions (only when `discussions: true`)
- `metadata: read` - Repository metadata (automatically granted)

**Permission Control via `discussions` Field**:

The optional `discussions` boolean field controls whether `discussions:write` permission is requested:

- **Default behavior** (`discussions: false` or omitted): Excludes `discussions:write` permission. This is safe for environments where the GitHub App lacks Discussions permission and avoids 422 errors during token generation.
- **Opt-in** (`discussions: true`): Includes `discussions:write` permission. Use this when the workflow needs to comment on discussions and the GitHub App has Discussions permission granted.

**Example Configuration**:

```yaml
safe-outputs:
  github-app:
    app-id: ${{ secrets.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: 'myorg'
    repositories: ['myrepo']
  add-comment:
    target: "*"
    max: 1
    discussions: true   # Opt in to discussions:write permission
```

**Notes**:

- By default, requires write permissions only for `issues` and `pull-requests`; `discussions:write` is opt-in
- Set `discussions: true` to add `discussions:write` and enable commenting on discussions
- Discussion-related safe outputs (`create-discussion`, `close-discussion`, `update-discussion`) independently add `discussions:write` permission when configured
- Cross-repository commenting requires appropriate permissions in target repository
- When `safe-outputs.add-comment.target` is `"*"`, requests MUST include at least one of `item_number`, `pr_number`, or `pr`; `item_number` is the canonical field.

---

#### Type: create_pull_request

**Purpose**: Create pull requests to propose code changes.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: Yes (required for full conformance)

**MCP Tool Schema**:

```json
{
  "name": "create_pull_request",
  "description": "Create a new pull request to propose code changes.",
  "inputSchema": {
    "type": "object",
    "required": ["title", "body"],
    "properties": {
      "title": {"type": "string"},
      "body": {"type": "string"},
      "branch": {"type": "string", "description": "Source branch (defaults to current)"},
      "base": {"type": "string", "description": "Target base branch override (allowed only when configured by allowed-base-branches)"},
      "labels": {"type": "array", "items": {"type": "string"}},
      "draft": {"type": "boolean", "description": "Create as draft (default: true)"}
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Branch Validation**: Source branch must exist and contain changes.
2. **Base Branch**: Defaults to repository default branch.
3. **Draft Status**: Creates as draft by default for safety.
4. **Auto-Commit**: When `commit-changes: true`, commits workspace changes before PR creation.
5. **Reviewer Assignment**: Auto-requests reviewers if configured.
6. **Branch Name Normalization**: The agent-supplied branch name is sanitized (invalid characters replaced; casing preserved). When `preserve-branch-name: false` (default), a random hex salt suffix is appended to ensure uniqueness across runs. When `preserve-branch-name: true`, the salt suffix is omitted so the branch name appears verbatim (useful for repository naming conventions, e.g. `bugfix/BR-329-red`).
7. **Remote Branch Collision Handling**: When the resolved branch name already exists on the remote, behavior depends on the configuration:

   | `preserve-branch-name` | `recreate-ref` | Behavior on collision |
   |---|---|---|
   | `false` (default) | n/a | Append random hex suffix to local branch name and continue |
   | `true` | `false` (default) | Surface `push_failed`; caller falls back (e.g. opens an issue when `fallback-as-issue: true`) |
   | `true` | `true` | Force-delete the existing remote ref via `DELETE /repos/{owner}/{repo}/git/refs/heads/{branch}` and let the subsequent push recreate it from the agent's local HEAD (force-push semantics). Concurrent-deletion 422 responses with "Reference does not exist" are treated as success. |
8. **Distinct Upstream and Head Repositories**: `target-repo` identifies the upstream repository that receives the pull request and owns the base branch. `head-repo`, when configured, identifies the repository that receives the pushed branch. When `head-repo` is omitted, the head repository defaults to `target-repo`.
9. **Owner-Qualified Head Reference**: When `head-repo` differs from `target-repo`, the created pull request MUST use an owner-qualified head reference identifying the head repository owner and pushed branch. Unqualified same-name branch references MUST NOT be used in fork-backed mode.
10. **Ephemeral Fork Branch Model**: When `head-repo` differs from `target-repo`, implementations SHOULD create or refresh an ephemeral branch in `head-repo` from the resolved upstream base SHA, apply the agent changes, and open the pull request back to the upstream base. Implementations MAY support explicit synchronization of that ephemeral branch with a newer upstream base, but implicit reuse of arbitrary pre-existing fork branches MUST NOT occur.
11. **Summary and Manifest Provenance**: Successful executions MUST record `head_repo` in the safe-output summary and machine-readable manifest.
**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `target-repo`: Upstream repository that receives the pull request and supplies the base branch
- `head-repo`: Optional repository that receives the pushed head branch. MUST either equal `target-repo` or identify a configured automation-owned fork
- `allowed-repos`: Cross-repository allowlist applied to both `target-repo` and `head-repo`
- `base-branch`: Target base branch for the PR. When omitted, the handler resolves the base branch in this order: (1) the runtime checkout manifest entry for the target repository, (2) the local checkout's `origin/HEAD`, (3) the repository default branch via `GET /repos/{owner}/{repo}`. Explicit configuration is RECOMMENDED when the safe-outputs job runs without repository credentials (e.g. cross-repo private targets).
- `allowed-branches`: Allowed source branch patterns for `branch` tool input
- `allowed-base-branches`: Allowed base-branch override patterns for per-run `base` tool input
- `draft`: Draft status
- `commit-changes`: Auto-commit workspace
- `reviewers`: Auto-request reviewers
- `labels`: Auto-apply labels
- `title-prefix`: Prepend to titles
- `footer`: Footer override
- `github-token`: Optional credential for upstream pull request creation and related upstream metadata operations
- `head-github-token`: Optional credential for `head-repo` branch writes. When specified, it SHOULD be limited to `contents: write` on `head-repo`
- `head-github-app`: Optional GitHub App configuration to mint an ephemeral credential for `head-repo` branch writes at runtime. When `head-github-app` is configured, the minted token takes precedence over `head-github-token`. The app installation MUST have `contents: write` on `head-repo`
- `preserve-branch-name`: When `true`, use the agent-supplied branch name verbatim without appending a random salt suffix (default: `false`)
- `recreate-ref`: When `true` (and `preserve-branch-name: true`), allows the handler to force-delete an existing remote branch ref and recreate it from the agent's local HEAD on collision. When `false` (default), an existing remote branch under `preserve-branch-name: true` causes a fallback rather than overwriting the remote ref. Has no effect when `preserve-branch-name: false`. (default: `false`)
**Security Requirements**:

- Branch name sanitization (prevent injection)
- Patch content validation
- Size limits on commits
- `head-repo` MUST be either the same repository as `target-repo` or an explicitly configured automation-owned fork; arbitrary contributor forks MUST NOT be used as write targets
- Both `target-repo` and `head-repo` MUST be validated against the configured allowlist before any push or pull request API call
- When distinct upstream and head credentials are configured, implementations MUST use the least-privilege head-repository credential only for branch writes and the upstream credential only for upstream pull request management
- Steering issue reuse MUST validate that the activation output identifies an open issue in the workflow repository and not a pull request.
- Steering issue metadata MUST NOT be used as a checkout ref or otherwise change normal pull request branch handling.

**Required Permissions**:

*GitHub Actions Token*:

- `contents: write` - Branch creation and commit operations
- `pull-requests: write` - Pull request creation

**With `fallback-as-issue: true`** (default):

- `contents: write` - Branch creation and commit operations
- `issues: write` - Issue creation fallback when PR creation fails
- `pull-requests: write` - Pull request creation

*GitHub App* (if using `safe-outputs.github-app` or `safe-outputs.create-pull-request.github-app` configuration):

- `contents: write` - Branch creation and commit operations
- `pull-requests: write` - Pull request creation
- `metadata: read` - Repository metadata (automatically granted)

**Fork-Backed Pull Request Credential Roles**:

- **Upstream credential**: `pull-requests: write` on `target-repo`; `issues: write` is additionally required when `fallback-as-issue: true`
- **Head repository credential**: `contents: write` on `head-repo`

**With `fallback-as-issue: true`** (default):

- `contents: write` - Branch creation and commit operations
- `issues: write` - Issue creation fallback when PR creation fails
- `pull-requests: write` - Pull request creation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Permission requirements vary based on `fallback-as-issue` configuration
- When `fallback-as-issue: true` (default), requires `issues: write` for fallback issue creation if PR creation fails
- When `fallback-as-issue: false`, only requires `contents: write` and `pull-requests: write`
- Fork-backed pull requests are supported only when `head-repo` is explicitly configured or defaults to `target-repo`; writes to arbitrary contributor forks are forbidden
- `push_to_pull_request_branch` follow-up support is limited to pull requests whose resolved head repository exactly matches the configured `head-repo` (or `target-repo` when `head-repo` is omitted)

---

### 7.2 System Types

System types are always available in every workflow. The types `noop`, `missing-tool`, and `missing-data` are unconditionally enabled, while `create-issue` is conditionally auto-injected when no other safe output types are explicitly configured (see Section 4.3 for auto-injection rules).

#### Type: noop

**Purpose**: Log workflow completion message for transparency.

**Default Max**: 1  
**Cross-Repository Support**: N/A (no external operations)  
**Mandatory**: Yes (always enabled, cannot be disabled)

**MCP Tool Schema**:

```json
{
  "name": "noop",
  "description": "Log a completion message indicating workflow finished successfully.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "message": {"type": "string", "description": "Completion message (default provided)"}
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Always Enabled**: Automatically registered in every workflow.
2. **Execution Order**: Always executes last for summary generation.
3. **No Side Effects**: Creates no GitHub resources.
4. **Transparency**: Provides clear indication of normal completion vs. error states.

**Configuration Parameters**:

- `max`: Operation limit (always 1; noop is registered as a singleton type)
- `message`: Default completion message (overridden by agent-provided message at invocation time)

**Security Requirements**:

- The `message` field MUST undergo content sanitization to prevent log injection
- The handler MUST NOT make any GitHub API calls
- The handler MUST NOT modify any workflow state or create side effects

**Required Permissions**:

*GitHub Actions Token*:

- No additional permissions required beyond base workflow permissions

*GitHub App* (if using `safe-outputs.github-app` configuration):

- No additional permissions required beyond base app installation

**Notes**:

- The `noop` type performs no GitHub API operations and requires no special permissions
- Only logs completion message to workflow output
- Always available regardless of permission configuration

---

### 7.3 Additional Safe Output Types

This section provides complete definitions for all remaining safe output types. Each follows the same format as Section 7.1 with full schemas, operational semantics, and permission requirements.

#### Type: comment_memory

**Purpose**: Persist structured memory in a managed issue or pull request comment using file-based editing and automatic synchronization.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Tool Exposure Model**:

- `comment_memory` is a safe output processor type.
- It MUST NOT be exposed as an agent-editable MCP tool when file-based comment-memory synchronization is active.
- The agent edits `/tmp/gh-aw/comment-memory/*.md` files directly; the processor synthesizes `comment_memory` operations from those files.

**Logical Operation Schema**:

```json
{
  "type": "comment_memory",
  "memory_id": "default",
  "body": "Markdown content loaded from /tmp/gh-aw/comment-memory/default.md"
}
```

**Operational Semantics**:

1. **Managed Marker**: Persisted comments use `<gh-aw-comment-memory id="<memory_id>">...</gh-aw-comment-memory>` markers.
2. **Setup Extraction**: Pre-agent setup extracts marker content from GitHub comments into `/tmp/gh-aw/comment-memory/<memory_id>.md`.
3. **File-Based Editing**: Agent updates memory by editing files only; no direct `comment_memory` tool call is required.
4. **Automatic Sync**: Processor reads `*.md` files and upserts corresponding managed comments after agent execution.
5. **Temporary ID Rewrite**: If temporary IDs (workflow-run-scoped placeholders prefixed with `aw_`, such as `aw_abc123`) are resolved during processing, comment-memory content MUST be rewritten using the resolved IDs before final upsert.
6. **Precedence Rule**: If both an explicit `comment_memory` operation and a file-backed entry exist for the same `memory_id`, the explicit operation takes precedence.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `memory-id`: Default memory identifier when omitted in synthesized operations
- `target`: Target issue/PR selector (`triggering`, `*`, or explicit number)
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist
- `footer`: Footer override
- `staged`: Staged mode override

**Security Requirements**:

- `memory_id` MUST be validated as `[A-Za-z0-9_-]+` with path traversal patterns rejected.
- Managed comment scan MUST be bounded by a maximum page limit.
- Materialized comment-memory files MUST enforce a per-file cap of 16 KiB and an aggregate cap of 48 KiB during setup.
- Body content MUST undergo sanitization and comment size/mention/link limit validation before upsert.
- Cross-repository targets MUST be validated against `allowed-repos`.
- Only content within managed marker tags is treated as editable memory; footer/provenance text MUST NOT be imported into editable files. For example, in `<gh-aw-comment-memory id="default">MEMORY</gh-aw-comment-memory>\n\n<!-- provenance footer -->`, only `MEMORY` is editable/imported.

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Managed comment create/update operations on issues and pull requests

*GitHub App*:

- `issues: write` - Managed comment create/update operations
- `metadata: read` - Repository metadata (automatically granted)

---

#### Type: update_issue

**Purpose**: Modify existing issue properties (title, body, state, labels, assignees, milestone).

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**MCP Tool Schema**:

```json
{
  "name": "update_issue",
  "description": "Update an existing GitHub issue's status, title, body, labels, assignees, or milestone. Only the fields you specify will be updated; other fields remain unchanged.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "status": {"type": "string", "enum": ["open", "closed"], "description": "New issue status"},
      "title": {"type": "string", "description": "New issue title"},
      "body": {"type": "string", "description": "New issue body in Markdown"},
      "issue_number": {"type": ["number", "string"], "description": "Issue number to update"},
      "operation": {
        "type": "string",
        "enum": ["replace", "append", "prepend", "replace-island"],
        "description": "Body update mode (default: append)"
      },
      "labels": {"type": "array", "items": {"type": "string"}, "description": "Replacement label list"},
      "assignees": {"type": "array", "items": {"type": "string"}, "description": "Replacement assignee list"},
      "milestone": {"type": ["number", "string"], "description": "Milestone number (null to clear)"}
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Partial Updates**: Only fields explicitly provided are modified; omitted fields are unchanged.
2. **Body Operation Modes**: `replace` overwrites the entire body; `append`/`prepend` add content with separator; `replace-island` updates a run-specific section.
3. **Label Validation**: Provided labels must exist in the repository; non-existent labels cause failure.
4. **Assignee Resolution**: Assignees must have repository access; invalid usernames cause failure.
5. **Cross-Repository**: When `target-repo` is configured, operates on that repository (must be in `allowed-repos`).

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist
- `staged`: Staged mode override

**Security Requirements**:

- Title and body MUST undergo full content sanitization before modification
- Label values MUST be validated against repository labels before application
- Cross-repository targets MUST be validated against the `allowed-repos` allowlist
- Issue number MUST be validated as a positive integer belonging to the target repository

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Issue modification operations

*GitHub App*:

- `issues: write` - Issue modification operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Only specified fields are updated; unspecified fields remain unchanged
- Same permissions as `create_issue`

---

#### Type: linear_create_issue

**Purpose**: Create an issue in one trusted Linear team using Linear's public GraphQL API.

**Experimental**: Yes. Compiling a workflow with any Linear Safe Output emits: `Using experimental feature: Linear safe outputs`.

**Configuration**:

- `linear-token`: REQUIRED trusted secret expression containing a Linear personal API key
- `linear-create-issue.team-id`: OPTIONAL Linear team model UUID or GitHub Actions expression, falling back to `LINEAR_TEAM_ID`
- `linear-create-issue.project-id`: OPTIONAL trusted Linear project URL identifier or model UUID, falling back to `LINEAR_PROJECT_ID`
- `linear-create-issue.max`: Operation limit (default: 1)
- `linear-create-issue.staged`: Staged mode override

**MCP Tool**: `linear_create_issue`

The MCP input object MUST require `title` and `body`, MUST reject additional properties, and MUST limit them to 128 and 65,000 characters respectively. The body MUST contain at least 20 characters. The trusted team UUID, optional project identifier, and credential MUST NOT be MCP inputs.

**Operational Semantics**:

1. The processor MUST POST to the fixed `https://api.linear.app/graphql` endpoint using `Content-Type: application/json` and the API key as the raw `Authorization` header value.
2. The implementation-defined `issueCreate(input: IssueCreateInput!)` document MUST use GraphQL variables for `teamId`, `title`, and `description`.
3. `title` and `body` MUST undergo standard Safe Outputs title and content sanitization before `body` is mapped to Linear's `description`.
4. The processor MUST fail on HTTP errors, malformed JSON, a non-empty top-level GraphQL `errors` array, `success: false`, or a missing issue object.
5. Staged mode MUST validate and sanitize the request but MUST NOT perform a network mutation.

The configured team ID MUST be a canonical UUID. Input exceeding a configured limit MUST be rejected, not silently truncated.

#### Type: linear_add_comment

**Purpose**: Add a comment to one trusted Linear issue.

**Experimental**: Yes.

**Configuration**:

- `linear-token`: REQUIRED trusted secret expression containing a Linear personal API key
- `linear-add-comment.target`: REQUIRED fixed issue UUID or shorthand identifier such as `ENG-123`
- `linear-add-comment.max`: Operation limit (default: 1)
- `linear-add-comment.staged`: Staged mode override

**MCP Tool**: `linear_add_comment`

The MCP input object MUST require only `body`, MUST reject additional properties, and MUST limit the body to 65,000 characters. The target and credential MUST NOT be MCP inputs.

The processor MUST use the fixed `commentCreate(input: CommentCreateInput!)` GraphQL document and pass the configured target as `issueId` through variables. The body MUST undergo standard Safe Outputs content sanitization. HTTP, parsing, GraphQL, unsuccessful-payload, and staged-mode behavior MUST match `linear_create_issue`.

#### Type: linear_update_issue

**Purpose**: Replace explicitly enabled basic fields on one trusted Linear issue.

**Experimental**: Yes.

**Configuration**:

- `linear-token`: REQUIRED trusted secret expression containing a Linear personal API key
- `linear-update-issue.target`: REQUIRED fixed issue UUID or shorthand identifier such as `ENG-123`
- `linear-update-issue.title`: Set to `true` to enable title replacement
- `linear-update-issue.body`: Set to `true` to enable description replacement
- `linear-update-issue.max`: Operation limit (default: 1)
- `linear-update-issue.staged`: Staged mode override

At least one of `title` or `body` MUST be enabled. The MCP tool name is `linear_update_issue`; its schema MUST expose only enabled fields, require at least one exposed field, reject additional properties, and apply the same field limits and sanitization as `linear_create_issue`. The target and credential MUST NOT be MCP inputs.

The processor MUST use the fixed `issueUpdate(id: String!, input: IssueUpdateInput!)` GraphQL document. The configured target and update values MUST be GraphQL variables. `body` maps to Linear's `description`. Omitted fields MUST remain unchanged. HTTP, parsing, GraphQL, unsuccessful-payload, and staged-mode behavior MUST match `linear_create_issue`.

For all Linear types, GraphQL source, endpoint, protocol, and host are implementation-defined and MUST NOT be agent-controlled. Linear API keys and GitHub App installation tokens are separate credentials for separate services. Enabling only Linear handlers MUST NOT request GitHub API write permissions or mint GitHub App tokens. Linear credentials MUST exist only in the trusted execution path and MUST NOT appear in handler configuration visible to the agent.

---

#### Type: close_issue

**Purpose**: Close issues with closing comment explaining resolution.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**MCP Tool Schema**:

```json
{
  "name": "close_issue",
  "description": "Close a GitHub issue with a closing comment explaining the resolution or reason for closing.",
  "inputSchema": {
    "type": "object",
    "required": ["body"],
    "properties": {
      "body": {"type": "string", "description": "Closing comment explaining the resolution"},
      "issue_number": {
        "type": ["number", "string"],
        "description": "Issue number to close. If omitted, closes the issue that triggered this workflow."
      },
      "duplicate_of": {
        "type": ["number", "string"],
        "description": "Issue number or URL of the canonical original issue when closing as a duplicate. Accepts: bare number (123), #-prefixed number (#123), owner/repo#number reference, or full GitHub issue URL. When provided together with state_reason: duplicate, creates a native GitHub duplicate relationship."
      }
    },
    "additionalProperties": false
  }
}
```

> **Note**: The schema above reflects the default configuration (`allow-body: true`). When `allow-body: false` is configured, the `body` field MUST be removed from the `required` array at compile time and the tool description MUST be amended with the additional constraint: "Closing comments are disabled: do not include a body field."

**Operational Semantics**:

1. **Comment First**: A closing comment is posted before the issue state is changed to `closed`.
2. **Context Resolution**: When `issue_number` is omitted, resolves from the workflow trigger context.
3. **Idempotent Comment**: If the issue is already closed, the closing comment is still posted.
4. **Cross-Repository**: When `target-repo` is configured, operates on that repository (must be in `allowed-repos`).
5. **Footer Injection**: Appends attribution footer to the closing comment when configured.
6. **Body Suppression**: When `allow-body` is `false`, the handler MUST NOT post a closing comment. Any `body` value provided by the agent SHALL be discarded before the close operation is executed. The implementation MUST log a warning if a non-empty `body` value was discarded.
7. **Native Duplicate Marking**: When `duplicate_of` is provided and `state_reason` is `duplicate`, the handler SHALL call the GitHub `markAsDuplicate` GraphQL mutation to create a native "marked this as a duplicate of #X" timeline event. If the mutation fails (e.g., missing permissions or invalid reference), a warning is logged and the close operation continues. The `duplicate_of` value MUST be parsed and resolved to a valid issue node ID before calling the mutation.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist
- `footer`: Footer override
- `staged`: Staged mode override
- `allow-body`: Controls whether the agent MAY include a closing comment body. When set to `false`, the handler MUST NOT post a closing comment; any `body` field emitted by the agent SHALL be discarded and the implementation MUST log a warning if a non-empty value was discarded. The `body` field MUST be removed from the MCP tool's `required` array at compile time and the tool description MUST be amended with the constraint "Closing comments are disabled: do not include a body field." Defaults to `true`.

**Security Requirements**:

- The closing comment body MUST undergo full content sanitization
- Issue number MUST be validated as a positive integer belonging to the target repository
- Cross-repository targets MUST be validated against the `allowed-repos` allowlist
- The handler MUST verify the caller has `issues: write` permission before executing
- The `duplicate_of` canonical issue reference MUST be resolved and validated before the GraphQL mutation is called; an unparseable value MUST be logged and skipped rather than causing an error

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Issue state modification and comment creation

*GitHub App*:

- `issues: write` - Issue state modification and comment creation
- `metadata: read` - Repository metadata (automatically granted)

---

#### Type: link_sub_issue

**Purpose**: Create parent-child relationships between issues using task list entries.

**Default Max**: 1  
**Cross-Repository Support**: Yes (restricted)  
**Mandatory**: No

**MCP Tool Schema**:

```json
{
  "name": "link_sub_issue",
  "description": "Link an issue as a sub-issue of a parent issue, establishing a parent-child relationship.",
  "inputSchema": {
    "type": "object",
    "required": ["parent_issue_number", "sub_issue_number"],
    "properties": {
      "parent_issue_number": {
        "type": ["number", "string"],
        "description": "The parent issue number to link the sub-issue to"
      },
      "sub_issue_number": {
        "type": ["number", "string"],
        "description": "The issue number to link as a sub-issue of the parent"
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Task List Insertion**: Adds a task list entry referencing the sub-issue to the parent issue body.
2. **Bidirectional Navigation**: Creates navigable links from parent to child and child to parent.
3. **Limit Enforcement**: Enforces maximum sub-issue count (default: 50) per parent issue.
4. **Validation**: Both parent and sub-issue numbers must exist in the same repository.
5. **Same Repository Only**: Cross-repository sub-issue linking is not supported.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `staged`: Staged mode override

**Security Requirements**:

- Both `parent_issue_number` and `sub_issue_number` MUST be validated as positive integers
- Both issue numbers MUST exist in the target repository before modification
- The handler MUST enforce the maximum sub-issue count limit to prevent unbounded growth
- Cross-repository operations MUST be rejected; only same-repository linking is permitted

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Issue body modification for task list entries

*GitHub App*:

- `issues: write` - Issue body modification for task list entries
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Creates bidirectional navigation links between parent and child issues
- Enforces maximum sub-issue count limit (default: 50)

---

#### Type: create_discussion

**Purpose**: Create GitHub discussions for announcements, Q&A, reports, or community conversations.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**MCP Tool Schema**:

```json
{
  "name": "create_discussion",
  "description": "Create a GitHub discussion for announcements, Q&A, reports, status updates, or community conversations.",
  "inputSchema": {
    "type": "object",
    "required": ["title", "body"],
    "properties": {
      "title": {"type": "string", "description": "Discussion title"},
      "body": {"type": "string", "description": "Discussion body in Markdown"},
      "category": {
        "type": "string",
        "description": "Discussion category by name, slug, or ID. Defaults to first available category."
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Category Resolution**: Category is resolved by name, slug, or ID; defaults to the first available category if omitted.
2. **Fallback Behavior**: When repository discussions are disabled, falls back to creating an issue if `issues: write` is available.
3. **Footer Injection**: Appends attribution footer to the discussion body when configured.
4. **Cross-Repository**: When `target-repo` is configured, creates in that repository (must be in `allowed-repos`).
5. **Temporary ID Support**: Supports `temporary_id` field for referencing before creation.
6. **Body Length Guard**: When `min-body-length` is configured, discussion creation is rejected if the body is shorter than the configured minimum.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `category`: Default discussion category
- `title-prefix`: Prepend to titles
- `min-body-length`: Minimum required body length (characters, before footer/metadata)
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist
- `footer`: Footer override
- `staged`: Staged mode override

**Security Requirements**:

- Title and body MUST undergo full content sanitization before creation
- Category MUST be validated as an existing category in the target repository
- Cross-repository targets MUST be validated against the `allowed-repos` allowlist

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Fallback issue creation when discussion creation fails
- `discussions: write` - Discussion creation operations

*GitHub App*:

- `discussions: write` - Discussion creation operations
- `issues: write` - Fallback issue creation when discussion creation fails
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Includes `issues: write` for fallback-to-issue functionality
- If discussion categories are not enabled, may fall back to creating an issue

---

#### Type: update_discussion

**Purpose**: Modify existing discussion title or body.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**MCP Tool Schema**:

```json
{
  "name": "update_discussion",
  "description": "Update an existing GitHub discussion's title or body. Only the fields you specify will be updated.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "title": {"type": "string", "description": "New discussion title"},
      "body": {"type": "string", "description": "New discussion body in Markdown"},
      "discussion_number": {
        "type": ["number", "string"],
        "description": "Discussion number to update. Required when workflow target is '*'."
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Partial Updates**: Only fields explicitly provided are modified; omitted fields are unchanged.
2. **Context Resolution**: When `discussion_number` is omitted, resolves from the workflow trigger context.
3. **Cross-Repository**: When `target-repo` is configured, operates on that repository (must be in `allowed-repos`).
4. **GraphQL-Based**: Uses GitHub GraphQL API for discussion updates as the REST API does not support discussion modification.
5. **Wildcard Target Requirement**: When `safe-outputs.update-discussion.target` is `"*"`, requests MUST include `discussion_number`.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist
- `staged`: Staged mode override

**Security Requirements**:

- Title and body MUST undergo full content sanitization before modification
- Discussion number MUST be validated as a positive integer belonging to the target repository
- Cross-repository targets MUST be validated against the `allowed-repos` allowlist
- At least one of `title` or `body` MUST be provided; empty updates MUST be rejected

**Required Permissions**:

*GitHub Actions Token*:

- `discussions: write` - Discussion modification operations

*GitHub App*:

- `discussions: write` - Discussion modification operations
- `metadata: read` - Repository metadata (automatically granted)

---

#### Type: close_discussion

**Purpose**: Close discussions with resolution reason and closing comment.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**MCP Tool Schema**:

```json
{
  "name": "close_discussion",
  "description": "Close a GitHub discussion with a resolution comment and optional reason.",
  "inputSchema": {
    "type": "object",
    "required": ["body"],
    "properties": {
      "body": {"type": "string", "description": "Closing comment explaining why the discussion is being closed"},
      "reason": {
        "type": "string",
        "enum": ["RESOLVED", "DUPLICATE", "OUTDATED", "ANSWERED"],
        "description": "Resolution reason"
      },
      "discussion_number": {
        "type": ["number", "string"],
        "description": "Discussion number to close. If omitted, closes the discussion that triggered this workflow."
      }
    },
    "additionalProperties": false
  }
}
```

> **Note**: The schema above reflects the default configuration (`allow-body: true`). When `allow-body: false` is configured, the `body` field MUST be removed from the `required` array at compile time and the tool description MUST be amended with the additional constraint: "Closing comments are disabled: do not include a body field."

**Operational Semantics**:

1. **Comment First**: A closing comment is posted before the discussion state is changed to `closed`.
2. **Resolution Reason**: Optional reason (`RESOLVED`, `DUPLICATE`, `OUTDATED`, `ANSWERED`) is recorded via GraphQL API.
3. **Context Resolution**: When `discussion_number` is omitted, resolves from the workflow trigger context.
4. **Idempotent Comment**: If the discussion is already closed, the closing comment is still posted.
5. **Cross-Repository**: When `target-repo` is configured, operates on that repository (must be in `allowed-repos`).
6. **Body Suppression**: When `allow-body` is `false`, the handler MUST NOT post a closing comment. Any `body` value provided by the agent SHALL be discarded before the close operation is executed. The implementation MUST log a warning if a non-empty `body` value was discarded.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist
- `footer`: Footer override
- `staged`: Staged mode override
- `allow-body`: Controls whether the agent MAY include a closing comment body. When set to `false`, the handler MUST NOT post a closing comment; any `body` field emitted by the agent SHALL be discarded and the implementation MUST log a warning if a non-empty value was discarded. The `body` field MUST be removed from the MCP tool's `required` array at compile time and the tool description MUST be amended with the constraint "Closing comments are disabled: do not include a body field." Defaults to `true`.

**Security Requirements**:

- The closing comment body MUST undergo full content sanitization
- Discussion number MUST be validated as a positive integer belonging to the target repository
- Cross-repository targets MUST be validated against the `allowed-repos` allowlist
- The `reason` field MUST be validated against the allowed enum values before submission

**Required Permissions**:

*GitHub Actions Token*:

- `discussions: write` - Discussion state modification and comment creation

*GitHub App*:

- `discussions: write` - Discussion state modification and comment creation
- `metadata: read` - Repository metadata (automatically granted)

---

#### Type: update_pull_request

**Purpose**: Modify existing pull request title, body, state, base branch, or draft status.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `pull-requests: write` - Pull request modification operations

*GitHub App*:

- `pull-requests: write` - Pull request modification operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Only specified fields are updated; unspecified fields remain unchanged
- Base branch changes are validated for safety
- When `safe-outputs.update-pull-request.target` is `"*"`, requests MUST include at least one of `pull_request_number`, `pr_number`, or `pr`; `pull_request_number` is the canonical field.

---

#### Type: close_pull_request

**Purpose**: Close pull requests WITHOUT merging, with closing comment.

**Default Max**: 10  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `pull-requests: write` - Pull request state modification and comment creation

*GitHub App*:

- `pull-requests: write` - Pull request state modification and comment creation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Higher default max (10) enables bulk PR cleanup operations
- Does NOT merge changes - use GitHub's merge functionality for that
- When `safe-outputs.close-pull-request.target` is `"*"`, requests MUST include `pull_request_number`.

---

#### Type: approve_workflow_run

**Purpose**: Approve a workflow run in the "action required" state.

**Default Max**: 1
**Cross-Repository Support**: No
**Mandatory**: No

**Experimental**: Yes. Compiling a workflow with `approve-workflow-run` emits: `Using experimental feature: approve-workflow-run`.

**MCP Tool Schema**:

```json
{
  "name": "approve_workflow_run",
  "description": "Approve a GitHub Actions workflow run awaiting required approval.",
  "inputSchema": {
    "type": "object",
    "required": ["run_id"],
    "properties": {
      "run_id": {
        "type": ["number", "string"],
        "description": "Positive integer workflow run ID from /actions/runs/<run_id>."
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Input Validation**: `run_id` MUST be a positive safe integer.
2. **Staged Preview**: When `staged` is true, the handler MUST return a preview without reading GitHub state or consuming the configured max limit.
3. **Eligibility**: Before approval, the handler MUST fetch the run and verify `event` is `pull_request`, the `pull_requests` array is non-empty, and the run is still awaiting approval. A run is awaiting approval when its `status` is `action_required` or `waiting`, or when its `conclusion` is `action_required`. A run that is no longer awaiting approval MUST be reported as a skipped no-op rather than a failure, because it is a benign race with a concurrent approval.
4. **Allowed Workflows**: The handler MUST fetch the workflow metadata and permit the run only when the workflow filename matches an `allowed-workflows` wildcard pattern. It MUST compare filenames rather than paths and MUST treat `.yml` and `.yaml` extensions as equivalent.
5. **Authorization**: A run is eligible only when every associated pull request is either the pull request that triggered the workflow or is listed in `allowed-pull-requests`. This permits all pending workflow runs for the triggering or explicitly allowed pull requests without authorizing mixed runs that include another pull request.
6. **Head Repositories and Events**: The handler MUST reject `pull_request_target` events. It MUST resolve the head repository of every associated pull request and reject the run unless each head repository is the current repository or matches an `allowed-repos` entry. A pull request whose head repository is unavailable or cannot be resolved MUST be rejected.
7. **Protected Files**: Before approval, the handler MUST list the files modified by every pull request associated with the run and reject approval when any file is protected. `protected-files.exclude` MAY remove specific filenames or path prefixes from the default protected set.
8. **Execution**: Only after all preceding checks pass MAY the handler invoke GitHub's workflow-run approval API and consume one max-count slot.
9. **Comment**: After a successful approval, when `comment` is not explicitly false, the handler MUST post a comment on each pull request associated with the approved run announcing the workflow run has started, linking to the run's HTML URL, and including the standard generated attribution footer. Comment posting failures MUST be logged as warnings and MUST NOT fail the approval.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `allowed-repos`: Repository slugs (wildcards supported) whose pull requests may be approved, in addition to the current repository which is always allowed (default: current repository only)
- `comment`: Post a comment on the associated pull request(s) announcing the run has started (default: true)
- `staged`: Preview without a GitHub API call or max-count consumption
- `github-token`: Explicit external token for this handler or inherited from `safe-outputs.github-token`
- `github-app`: GitHub App configuration that mints a handler-scoped token
- `allowed-workflows`: Required list of workflow filename wildcard patterns. `.yml` and `.yaml` are normalized before matching.
- `allowed-pull-requests`: Additional authorized pull request numbers as strings or an expression resolving to a list
- `protected-files.exclude`: Filenames or path prefixes to remove from the default protected-file set

**Security Requirements**:

- Live approvals MUST use an explicit external `github-token` or a GitHub App token; implementations MUST NOT use the default `github.token`.
- The handler MUST reject `pull_request_target` events, and associated pull requests whose head repository is neither the current repository nor listed in `allowed-repos`.
- The handler MUST reject a run that is not a pull request run, is not from an allowed workflow, has any associated pull request that is not authorized, or has modified protected files. A run that is no longer awaiting approval MUST be skipped rather than rejected as a failure.
- The handler MUST be classified as an Abort type for warn-mode threat-detection failures.

**Required Permissions**:

*GitHub Actions Token*:

- `actions: write` - Workflow-run approval
- `pull-requests: write` - Posting the run-started comment (when `comment` is enabled, the default); `pull-requests: read` is sufficient when `comment: false`

*GitHub App*:

- `actions: write` - Workflow-run approval
- `pull-requests: write` - Posting the run-started comment (when `comment` is enabled, the default); `pull-requests: read` is sufficient when `comment: false`
- `metadata: read` - Repository metadata (automatically granted)

---

#### Type: merge_pull_request

**Purpose**: Merge pull requests only when configured policy gates pass.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**MCP Tool Schema**:

```json
{
  "name": "merge_pull_request",
  "description": "Merge an existing pull request only after policy checks pass (status checks, approvals, resolved review threads, label/branch constraints, and mergeability gates).",
  "inputSchema": {
    "type": "object",
    "properties": {
      "pull_request_number": {
        "type": ["number", "string"],
        "description": "Pull request number to merge. Supports numeric values or temporary IDs from prior safe-output operations. If omitted, uses triggering pull request context."
      },
      "merge_method": {
        "type": "string",
        "enum": ["merge", "squash", "rebase"]
      },
      "commit_title": {"type": "string"},
      "commit_message": {"type": "string"},
      "repo": {
        "type": "string",
        "description": "Target repository in owner/repo format."
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Repository/PR Resolution**: Resolves target repository and pull request from context or explicit input.
2. **Idempotency**: Returns success immediately when the pull request is already merged; no further gates are evaluated.
3. **Mergeability Check**: Validates pull request is mergeable and not draft/conflicted.
4. **Policy Gates**: Enforces required checks, review decision, unresolved review thread gating, label constraints, and source branch constraints.
5. **Base Branch Protection**: Refuses merges when the target base branch is protected or is the repository default branch.
6. **Target Branch Open PR**: When the target base branch is neither protected nor the repository default, refuses merges when that branch does not have an associated open pull request (i.e. no open PR exists with that branch as its head/source branch). This gate enforces the PR-chain model (e.g. `release/1.0 → main`) as a hard invariant; no opt-out is provided. Only intra-repository PR chains are matched — fork-sourced upstream PRs are not considered. If your workflow does not follow this model, use a custom merge step instead.

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `required-labels`: Labels that must ALL be present on the pull request
- `required-title-prefix`: Title prefix the pull request must start with
- `allowed-branches`: Source branch glob patterns; the PR's branch must match at least one
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repository allowlist
- `staged`: Staged mode override

**Required Permissions**:

*GitHub Actions Token*:

- `contents: write` - Merge operation execution
- `pull-requests: write` - Pull request metadata and merge operations

*GitHub App*:

- `contents: write` - Merge operation execution
- `pull-requests: write` - Pull request metadata and merge operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Merge execution is blocked unless all configured gates pass.
- Merge to the repository default branch is always refused by this safe output type.
- Merge is refused when the target base branch does not have an open pull request where that branch is the head (source) branch. This enforces that merges flow only into branches actively tracked by an upstream PR.
- `pull_request_number` may be a temporary ID that resolves to a pull request number from earlier safe-output operations.
- GraphQL mergeability and review-summary queries are retried with transient-error retry logic.
- Compiling a workflow with `merge-pull-request` emits: `Using experimental feature: merge-pull-request`.

---

#### Type: mark_pull_request_as_ready_for_review

**Purpose**: Convert draft pull request to ready-for-review status.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `pull-requests: write` - Pull request draft status modification

*GitHub App*:

- `pull-requests: write` - Pull request draft status modification
- `metadata: read` - Repository metadata (automatically granted)

---

#### Type: push_to_pull_request_branch

**Purpose**: Push commits to pull request branch for automated code changes.

**Default Max**: 1  
**Cross-Repository Support**: Yes (restricted)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `contents: write` - Branch push operations and commit creation
- `issues: write` - Comment creation for push notifications
- `pull-requests: write` - Pull request metadata access

*GitHub App*:

- `contents: write` - Branch push operations and commit creation
- `issues: write` - Comment creation for push notifications  
- `pull-requests: write` - Pull request metadata access
- `metadata: read` - Repository metadata (automatically granted)

**Fork-Backed Follow-Up Push Credential Roles**:

- **Upstream credential**: `pull-requests: write` on `target-repo`; `issues: write` when the implementation posts push-result comments
- **Head repository credential**: `contents: write` on `head-repo`

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `base-branch`: Target base branch used to compute the incremental patch (e.g. `main`, `master`). When omitted, the handler resolves the base branch in this order: (1) the runtime checkout manifest entry for the target repository, (2) the local checkout's `origin/HEAD`, (3) the repository default branch via `GET /repos/{owner}/{repo}`. Explicit configuration is REQUIRED when the safe-outputs job runs without repository credentials (e.g. cross-repo private targets) and the checkout manifest is unavailable.
- `target`: Triggering PR selector (see `submit_pull_request_review` semantics)
- `target-repo`: Upstream repository containing the pull request
- `head-repo`: Optional expected head repository for fork-backed pull requests. When omitted, the expected head repository is `target-repo`
- `allowed-repos`: Cross-repository allowlist applied to both `target-repo` and `head-repo`
- `head-github-token`: Optional credential for `head-repo` branch writes
- `head-github-app`: Optional GitHub App configuration to mint an ephemeral credential for `head-repo` branch writes at runtime; takes precedence over `head-github-token` when both are specified

**Notes**:

- Requires `contents: write` for git push operations
- Enforces maximum patch size limit (default: 10 KB, range: 1–100 KB)
- Validates changes don't exceed size limits before pushing
- The handler MUST ignore agent-supplied `diff_size` values and validate patch size from the generated patch artifact.
- For patch transport, the generated patch SHOULD embed `X-GH-AW-Base-Commit` metadata derived by the trusted patch-generation step. The privileged processor MUST derive patch re-anchoring metadata from the generated patch and MUST NOT trust agent-supplied base-commit metadata.
- Base-branch resolution MUST NOT depend on interactive credential prompts; git operations issued by the handler MUST run with `GIT_TERMINAL_PROMPT=0` and an enforced timeout so credential-less environments fail fast rather than hanging
- When `safe-outputs.push-to-pull-request-branch.target` is `"*"`, requests MUST include `pull_request_number`.
- The handler MUST refuse pushes unless the resolved pull request head repository exactly matches the configured `head-repo` (or `target-repo` when `head-repo` is omitted)
- Arbitrary contributor forks MUST remain unsupported write targets even when the upstream repository itself is allowlisted
- Successful executions MUST record `head_repo` in the safe-output summary and machine-readable manifest

---

#### Type: create_pull_request_review_comment

**Purpose**: Create review comments on specific lines of code in pull requests.

**Default Max**: 10  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `pull-requests: write` - Review comment creation

*GitHub App*:

- `pull-requests: write` - Review comment creation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Comments buffered via PR review buffer for batch submission
- Higher default max (10) enables comprehensive code review
- When `safe-outputs.create-pull-request-review-comment.target` is `"*"`, requests MUST include `pull_request_number`.

---

#### Type: submit_pull_request_review

**Purpose**: Submit formal pull request review with status (APPROVE, REQUEST_CHANGES, COMMENT).

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `pull-requests: write` - Review submission operations

*GitHub App*:

- `pull-requests: write` - Review submission operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Submits all buffered review comments from `create_pull_request_review_comment`
- Review status affects PR merge requirements
- **Target**: `target` accepts `"triggering"` (default), `"*"` (request MUST include `pull_request_number`), or an explicit PR number (e.g. `${{ github.event.inputs.pr_number }}`). Required when the workflow is not triggered by a pull request (e.g. `workflow_dispatch`).
- **Cross-Repository**: `target-repo` specifies a repository in `owner/repo` format to submit reviews on PRs in another repo. Use `allowed-repos` to permit additional repositories.
- Footer control: `footer` accepts `"always"` (default), `"none"`, or `"if-body"` (only when review body has text); boolean `true`/`false` maps to `"always"`/`"none"`

---

#### Type: resolve_pull_request_review_thread

**Purpose**: Mark pull request review threads as resolved after addressing feedback.

**Default Max**: 10  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `pull-requests: write` - Review thread resolution operations

*GitHub App*:

- `pull-requests: write` - Review thread resolution operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Higher default max (10) enables resolving multiple threads per review cycle
- GitHub can reject `resolveReviewThread` with `Resource not accessible by integration` for some `GITHUB_TOKEN` contexts even with `pull-requests: write`; gh-aw treats this specific response as a soft-skip warning for this handler
- For reliable resolution, configure `safe-outputs.resolve-pull-request-review-thread.github-token` with a token that can resolve review threads in the target repository

---

#### Type: reply_to_pull_request_review_comment

**Purpose**: Reply to existing review comments on pull requests to acknowledge feedback or answer questions.

**Default Max**: 10  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `pull-requests: write` - Review comment reply creation

*GitHub App*:

- `pull-requests: write` - Review comment reply creation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Higher default max (10) enables responding to multiple review comments per cycle
- Replies scoped to triggering PR by default; `target: "*"` requires explicit `pull_request_number` per message
- Footer attribution appended by default; configurable via `footer: false`

---

#### Type: add_labels

**Purpose**: Add labels to issues or pull requests.

**Default Max**: 3  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Label addition to issues
- `pull-requests: write` - Label addition to pull requests

*GitHub App*:

- `issues: write` - Label addition to issues
- `pull-requests: write` - Label addition to pull requests
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Requires both `issues: write` and `pull-requests: write` to support labeling both entity types
- Labels must exist in repository; non-existent labels generate warnings

---

#### Type: remove_labels

**Purpose**: Remove labels from issues or pull requests.

**Default Max**: 3  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Label removal from issues
- `pull-requests: write` - Label removal from pull requests

*GitHub App*:

- `issues: write` - Label removal from issues
- `pull-requests: write` - Label removal from pull requests
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Same permissions as `add_labels`
- Missing labels are silently ignored (no error)

---

#### Type: add_reviewer

**Purpose**: Add reviewers (users or teams) to pull requests.

**Default Max**: 3  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `pull-requests: write` - Reviewer assignment operations

*GitHub App*:

- `pull-requests: write` - Reviewer assignment operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Teams are expanded to individual members based on repository configuration
- Invalid reviewers generate warnings but don't fail the operation

---

#### Type: assign_milestone

**Purpose**: Assign issues to repository milestones for release planning.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Milestone assignment operations

*GitHub App*:

- `issues: write` - Milestone assignment operations
- `metadata: read` - Repository metadata (automatically granted)

**Configuration Options**:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `allowed` | `string[]` | `[]` | Restrict assignments to milestones with these titles |
| `auto_create` | `boolean` | `false` | Auto-create milestones from the `allowed` list if they don't exist |
| `max` | `number` | `1` | Maximum number of assignments |
| `target-repo` | `string` | — | Cross-repository target (`owner/repo`) |
| `github-token` | `string` | — | Custom token for elevated permissions |

**Agent Output Fields**:

| Field | Required | Description |
|-------|----------|-------------|
| `issue_number` | Yes | Issue number or temporary ID to assign the milestone to |
| `milestone_number` | No* | Numeric milestone ID. Either this or `milestone_title` is required. |
| `milestone_title` | No* | Milestone title (e.g., `"v1.0"`). Resolved to a number internally. Either this or `milestone_number` is required. |

\* At least one of `milestone_number` or `milestone_title` must be provided.

**Notes**:

- Milestones must exist in the repository unless `auto_create: true` is set
- When `auto_create: true`, missing milestones are created automatically before assignment
- Without `auto_create`, the handler returns a clear error listing available milestones
- Replaces any existing milestone assignment

---

#### Type: assign_to_agent

**Purpose**: Assign GitHub Copilot coding agent to issues or pull requests.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Agent assignment operations

*GitHub App*:

- `issues: write` - Agent assignment operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Uses special assignee syntax for Copilot coding agent assignment
- Agent must be enabled in repository settings

---

#### Type: assign_to_user

**Purpose**: Assign users to issues or pull requests.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Configuration Options**:

- `unassign-first` (boolean, default: false): If true, unassigns all current assignees before assigning new ones. Useful for reassigning issues from one user to another.

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - User assignment operations (for issues)
- `pull-requests: write` - User assignment operations (for pull requests)

*GitHub App*:

- `issues: write` - User assignment operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Users must have repository access to be assigned
- Invalid users generate warnings
- When `unassign-first` is enabled, the handler fetches current assignees and removes them before adding new ones

---

#### Type: unassign_from_user

**Purpose**: Remove user assignments from issues or pull requests.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - User unassignment operations

*GitHub App*:

- `issues: write` - User unassignment operations
- `metadata: read` - Repository metadata (automatically granted)

---

#### Type: hide_comment

**Purpose**: Hide (minimize) comments on issues, pull requests, or discussions.

**Default Max**: 5  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Configuration Parameters**:

- `max`: Operation limit (default: 5)
- `discussions`: Control `discussions:write` permission (default: false)
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist
- `allowed-reasons`: Allowed reasons for hiding comments

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Comment hiding on issues
- `pull-requests: write` - Comment hiding on pull requests
- `discussions: write` - Comment hiding on discussions (when `discussions: true`)

*GitHub App*:

- `issues: write` - Comment hiding on issues
- `pull-requests: write` - Comment hiding on pull requests
- `discussions: write` - Comment hiding on discussions (when `discussions: true`)
- `metadata: read` - Repository metadata (automatically granted)

**Permission Control via `discussions` Field**:

The optional `discussions` boolean field controls whether `discussions:write` permission is requested:

- **Default behavior** (`discussions: false` or omitted): Excludes `discussions:write` permission. The `discussions:write` permission is now opt-in.
- **Opt-in** (`discussions: true`): Includes `discussions:write` permission. Use this when comments on discussions may need to be hidden.

**Example Configuration**:

```yaml
safe-outputs:
  github-app:
    app-id: ${{ secrets.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: 'myorg'
    repositories: ['myrepo']
  hide-comment:
    max: 5
    discussions: true  # Include discussions:write permission
    allowed-reasons: [spam, abuse, off_topic]
```

**Notes**:

- By default, only requests `issues:write` and `pull-requests:write` permissions
- When `discussions: true`, the workflow additionally requests `discussions:write` permission
- Discussion-related safe outputs independently add `discussions:write` permission when configured
- Comments are minimized, not deleted - reversible by moderators

---

#### Type: create_project

**Purpose**: Create GitHub Projects V2 boards for project management.

**Default Max**: 1  
**Cross-Repository Support**: Yes (organization or user projects)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `organization-projects: write` - Project creation operations (note: only valid for GitHub Apps)
- `issues: read` - Required to resolve issue/PR references when adding items to the project

*GitHub App*:

- `organization-projects: write` - Project creation operations
- `issues: read` - Required to resolve issue/PR references when adding items to the project
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- `organization-projects` permission is ONLY available for GitHub App tokens, not standard GitHub Actions tokens
- GitHub Actions workflows should use GitHub App authentication for project operations
- Projects can be created at organization or user level based on app installation

---

#### Type: update_project

**Purpose**: Manage GitHub Projects V2 boards (add items, update fields, remove items).

**Default Max**: 10  
**Cross-Repository Support**: Yes (via `target_repo` field in agent output; requires `allowed-repos` configuration)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `organization-projects: write` - Project management operations (note: only valid for GitHub Apps)
- `issues: read` - Required to resolve issue/PR references when adding items to the project

*GitHub App*:

- `organization-projects: write` - Project management operations
- `issues: read` - Required to resolve issue/PR references when adding items to the project
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Same permission requirements as `create_project`
- Higher default max (10) enables batch project board updates
- Cross-repo support uses `target_repo` in agent output to resolve issues/PRs from other repos; the `allowed-repos` configuration option controls which repos are permitted

---

#### Type: create_project_status_update

**Purpose**: Create status updates for GitHub Projects V2 boards.

**Default Max**: 1  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `organization-projects: write` - Project status update operations (note: only valid for GitHub Apps)

*GitHub App*:

- `organization-projects: write` - Project status update operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Does not require `issues: read` — status updates operate on the project board itself, not on issue/PR items

---

#### Type: update_release

**Purpose**: Update GitHub release descriptions and metadata.

**Default Max**: 1  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `contents: write` - Release modification operations

*GitHub App*:

- `contents: write` - Release modification operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Only updates release notes and metadata; does NOT modify release assets
- Release must already exist (identified by tag name)

---

#### Type: upload_asset

**Purpose**: Upload files to orphaned git branch for artifact storage.

**Default Max**: 10  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `contents: write` - Branch creation, commit operations, and file uploads

*GitHub App*:

- `contents: write` - Branch creation, commit operations, and file uploads
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Creates or updates orphaned branch for asset storage
- Enforces maximum file size limit (default: 10 MB = 10240 KB)
- Files accessible via raw.githubusercontent.com URLs
- Staged asset filenames MUST be keyed by a hash of the declared source path, with the original extension preserved where applicable, so distinct source paths with the same basename do not collide.
- The privileged upload job MUST validate that staged asset paths remain contained within the staged-assets directory and that the expected staged filename matches the declared source path.
- The privileged upload job MUST compute asset size, SHA-256 digest, target filename, and published URL from staged file contents and trusted runtime context. It MUST NOT trust agent-supplied `size`, `sha`, `path`, `targetFileName`, or URL metadata for privileged decisions.

---

#### Type: dispatch_workflow

**Purpose**: Trigger workflow_dispatch events to invoke other workflows.

**Default Max**: 1  
**Cross-Repository Support**: Yes (via `target-repo`)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `actions: write` - Workflow dispatch operations

*GitHub App*:

- `actions: write` - Workflow dispatch operations
- `metadata: read` - Repository metadata (automatically granted)

**Configuration Parameters**:

- `max`: Operation limit (default: 1)
- `workflows`: Allowlist of workflow names that may be dispatched
- `target-repo`: Cross-repository target (owner/repo)
- `target-ref`: Git ref (branch, tag, or SHA) to use when dispatching the workflow. In `workflow_call` relay scenarios this is auto-injected by the compiler from `needs.activation.outputs.target_ref`, ensuring the correct platform branch is used instead of the caller's `GITHUB_REF`.
- `allowed-repos`: Cross-repo allowlist (supports wildcards, e.g. `org/*`)
- `allowed-refs`: Allowlist of ref glob patterns allowed for per-call `message.ref` overrides. When omitted, the repository default branch is implicitly allowed.

**Notes**:

- Requires ONLY `actions: write` permission
- Target workflow must support `workflow_dispatch` trigger
- Workflow inputs are validated against target workflow's input schema
- Cross-repository dispatch requires appropriate `actions: write` permissions in the target repository
- In `workflow_call` relay (CentralRepoOps) scenarios, the compiler automatically injects both `target-repo` and `target-ref` from `needs.activation.outputs.*` so the dispatch targets the correct platform repository and branch
- Per-call `message.ref` values MUST match the configured `allowed-refs` patterns (or the implicit default-branch allowance when `allowed-refs` is omitted); non-matching refs MUST be rejected.

---

#### Type: create_code_scanning_alert

**Purpose**: Generate SARIF security reports and code scanning alerts.

**Default Max**: unlimited  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `security-events: write` - SARIF report upload and alert creation

*GitHub App*:

- `security-events: write` - SARIF report upload and alert creation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Unlimited max enables comprehensive security scanning
- Alerts appear in repository Security tab
- SARIF format validation performed before upload

---

#### Type: autofix_code_scanning_alert

**Purpose**: Create automated pull requests to fix code scanning alerts.

**Default Max**: 10  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `security-events: write` - Alert metadata access
- `actions: read` - Workflow run metadata for alert correlation

*GitHub App*:

- `security-events: write` - Alert metadata access
- `actions: read` - Workflow run metadata for alert correlation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- The handler calls the GitHub code-scanning autofix API (`POST /repos/{owner}/{repo}/code-scanning/alerts/{alert_number}/fixes`) directly; it does not create a branch or pull request, so `contents: write` and `pull-requests: write` are not required
- Alert must exist and be fixable

---

#### Type: create_check_run

**Purpose**: Create a GitHub Check Run to report agent analysis results as a first-class status check on a commit or pull request.

**Default Max**: 1  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**MCP Tool Schema**:

```json
{
  "name": "create_check_run",
  "description": "Create a GitHub Check Run to report agent analysis results on a commit or pull request. Check Runs appear in the PR checks UI and on commits with a pass/fail status. Use this to surface structured analysis results as a first-class GitHub check. The check run name is configured in the workflow frontmatter and is NOT accepted as a parameter. When `safe-outputs.create-check-run.target` is configured, pull request targeting follows standard PR target rules. With `target: \"*\"`, include `pull_request_number` (or `pr_number`/`pr`/`pull_number`) in each call.",
  "inputSchema": {
    "type": "object",
    "required": ["conclusion", "title", "summary"],
    "properties": {
      "conclusion": {
        "type": "string",
        "enum": ["success", "failure", "neutral", "cancelled", "skipped", "timed_out", "action_required"],
        "description": "The final conclusion of the check run."
      },
      "title": {
        "type": "string",
        "description": "Short title summarizing the check result. Shown in the checks UI next to the check run name. Maximum 256 characters."
      },
      "summary": {
        "type": "string",
        "description": "Markdown-formatted summary of the check result. Shown in the checks detail view. Maximum 65535 characters."
      },
      "text": {
        "type": "string",
        "description": "Optional detailed Markdown content shown in the check run details. Maximum 65535 characters."
      },
      "pull_request_number": {
        "type": ["number", "string"],
        "description": "Pull request number to attach the check run to when `target: \"*\"` is configured. Aliases: pr_number, pr, pull_number.",
        "x-synonyms": ["pr_number", "pr", "pull_number"]
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **SHA Resolution**: When `target` is configured, the handler resolves the target pull request number via the shared target resolution logic, then fetches the current PR head SHA via `GET /repos/{owner}/{repo}/pulls/{pull_number}`. The API fetch is intentional even when the event payload carries a SHA (e.g. `target: "triggering"` on a `pull_request` event) so that the check run always references the most recent head in the event of a force push between the triggering event and handler execution.
2. **Fallback SHA** (no `target`): The handler uses `pull_request.head.sha` from the event payload when present (avoids the ephemeral merge commit SHA produced by `pull_request` events), falling back to `GITHUB_SHA`, then `context.sha`.
3. **Check Run Creation**: Issues `POST /repos/{owner}/{repo}/check-runs` with status `completed` and the resolved `head_sha`. The check run name is taken from frontmatter configuration, defaulting to the workflow name (with `(Result)` suffix to avoid collapsing into the workflow's own check suite entry in compact UI views).
4. **Staged Mode**: In staged mode the handler logs a preview message with the resolved PR number (when `target` is set) and returns without calling the Checks API or the Pulls API.
5. **Error Handling**: Hard failures (target resolution failure, missing PR head SHA, Pulls API error) emit `core.error` for a red workflow annotation and return `{ success: false }`. Soft skips (e.g. `target: "triggering"` in a non-PR context) emit `core.info` and return `{ success: false, skipped: true }`.

**Configuration Parameters**:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | `string` | Workflow name | Check run name shown in the GitHub Checks UI. Auto-suffixed with `(Result)` when equal to the workflow name to avoid UI collapsing. |
| `target` | `string` | — | PR targeting mode: `"triggering"` (use PR from event context), `"*"` (use `pull_request_number` from each call), or an explicit PR number expression (e.g. `"${{ github.event.inputs.pr }}"` ). When omitted the handler falls back to the event-payload SHA heuristic. |
| `max` | `number` | `1` | Maximum number of check runs per workflow run. |
| `staged` | `boolean` | `false` | Enable staged mode for this handler. |
| `output.title` | `string` | — | Static fallback title when the agent does not provide one. |
| `output.summary` | `string` | — | Static fallback summary when the agent does not provide one. |

**Required Permissions**:

*GitHub Actions Token* (when `target` is NOT configured):

- `checks: write` - Check run creation

*GitHub Actions Token* (when `target` IS configured):

- `checks: write` - Check run creation
- `pull-requests: read` - PR head SHA resolution via `GET /repos/{owner}/{repo}/pulls/{pull_number}`

*GitHub App*:

- `checks: write` - Check run creation
- `pull-requests: read` - PR head SHA resolution (when `target` is configured)
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- The check run `name` is configured in workflow frontmatter, NOT accepted as an agent-provided parameter. Agents MUST NOT pass `name` in the tool call.
- `conclusion` is required and MUST be one of: `success`, `failure`, `neutral`, `cancelled`, `skipped`, `timed_out`, `action_required`.
- `title` and `summary` are required. Both may be supplied as static fallbacks in frontmatter (`output.title`, `output.summary`) when the agent does not produce them.
- When `target: "*"` is configured, requests MUST include at least one of `pull_request_number`, `pr_number`, `pr`, or `pull_number`; `pull_request_number` is the canonical field.
- The `pull-requests: read` permission is automatically added to the compiled workflow only when `target` is configured; workflows without a `target` are not affected.

**Example Frontmatter**:

```yaml
safe-outputs:
  create-check-run:
    name: "Security Scan"
    target: "*"
    max: 1
```

**Example Agent Message**:

```json
{
  "type": "create_check_run",
  "pull_request_number": 876,
  "conclusion": "failure",
  "title": "3 issues found",
  "summary": "### Findings\n- Issue A\n- Issue B\n- Issue C"
}
```

---

#### Type: create_agent_session

**Purpose**: Create GitHub Copilot coding agent sessions for code change delegation.

**Default Max**: 1  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:

- `issues: write` - Issue creation and agent assignment

*GitHub App*:

- `issues: write` - Issue creation and agent assignment
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:

- Creates issue with special agent assignment that triggers Copilot coding agent
- Agent must be enabled in repository settings

---

#### Type: missing_tool

**Purpose**: Report when AI requests unavailable functionality for feature discovery.

**Default Max**: unlimited  
**Cross-Repository Support**: N/A (logging only)  
**Mandatory**: Yes (always enabled, cannot be disabled)

**Required Permissions**:

*GitHub Actions Token*:

- No additional permissions required beyond base workflow permissions
- When `create-issue: true` configured, requires `issues: write` for issue creation

*GitHub App*:

- No additional permissions required beyond base app installation
- When `create-issue: true` configured, requires `issues: write` for issue creation

**Notes**:

- Base functionality requires no permissions (logging only)
- Optional issue creation requires `issues: write` when `create-issue: true`
- Always enabled to capture AI's unmet capability requests

---

#### Type: missing_data

**Purpose**: Report when AI lacks required information to complete goals.

**Default Max**: unlimited  
**Cross-Repository Support**: N/A (logging only)  
**Mandatory**: Yes (always enabled, cannot be disabled)

**Required Permissions**:

*GitHub Actions Token*:

- No additional permissions required beyond base workflow permissions
- When `create-issue: true` configured, requires `issues: write` for issue creation

*GitHub App*:

- No additional permissions required beyond base app installation
- When `create-issue: true` configured, requires `issues: write` for issue creation

**Notes**:

- Same permission model as `missing_tool`
- Base functionality requires no permissions (logging only)
- Optional issue creation requires `issues: write` when `create-issue: true`

---

#### Type: report_incomplete

**Purpose**: Signal that the task could not be completed due to an infrastructure or tool failure (e.g., MCP server crash, missing authentication, inaccessible repository). This is distinct from `noop` (no action needed) — it indicates an active failure that prevented the task from running. The workflow framework treats this as a failure signal even when the agent exits successfully.

**Default Max**: 5  
**Cross-Repository Support**: N/A (logging only)  
**Mandatory**: Yes (always enabled, cannot be disabled)

**MCP Tool Schema**:

```json
{
  "name": "report_incomplete",
  "description": "Signal that the task could not be completed due to an infrastructure or tool failure (e.g., MCP server crash, missing authentication, inaccessible repository). Use this when required tools or data are unavailable and the task cannot be meaningfully performed. This is distinct from noop (no action needed) — it indicates an active failure that prevented the task from running. The workflow framework will treat this as a failure signal even when the agent exits successfully.",
  "inputSchema": {
    "type": "object",
    "required": ["reason"],
    "properties": {
      "reason": {
        "type": "string",
        "description": "A concise explanation of why the task could not be completed (max 1024 characters). Be specific about which tools or resources were unavailable."
      },
      "details": {
        "type": "string",
        "description": "Optional extended details or diagnostic context about the failure (max 65000 characters)."
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Always Enabled**: Automatically registered in every workflow when `safe-outputs` is configured.
2. **Failure Signal**: Triggers failure handling in the conclusion job regardless of agent exit code. This is the key distinction from `noop`.
3. **No Side Effects** (unless `create-issue: true`): Records reason and details in workflow logs; optionally creates/updates a tracking issue.
4. **Co-emission Handling**: When emitted alongside other safe outputs (e.g., `add_comment`), those outputs are treated as describing the failure state, not a successfully completed action.

**Configuration Parameters**:

- `create-issue`: When `true`, creates/updates a GitHub tracking issue when `report_incomplete` is emitted (default: `true`)
- `title-prefix`: Prefix for tracking issue titles (default: `"[incomplete]"`)
- `labels`: Labels to add to created issues (array of strings)
- `max`: Maximum number of `report_incomplete` signals per run (default: 5)

**Security Requirements**:

- The `reason` field MUST undergo content sanitization (max 1024 characters)
- The `details` field MUST undergo content sanitization (max 65000 characters)
- The handler MUST NOT make any GitHub API calls unless `create-issue: true` is configured
- The handler MUST activate failure handling in the conclusion job regardless of agent exit code
- Co-emitted safe outputs (e.g., `add_comment`) MUST be treated as describing the failure state, not a completed action

**Required Permissions**:

*GitHub Actions Token*:

- No additional permissions required beyond base workflow permissions
- When `create-issue: true` configured, requires `issues: write` for issue creation

*GitHub App*:

- No additional permissions required beyond base app installation
- When `create-issue: true` configured, requires `issues: write` for issue creation

**Notes**:

- Base functionality requires no permissions (logging only)
- Optional issue creation requires `issues: write` when `create-issue: true`
- Activates failure handling even when the agent exits with code 0
- Should be used instead of `add_comment` for infrastructure failures
- `noop + report_incomplete` combination correctly escalates to failure handling (noop bypass is suppressed)

---

## 8. Protocol Exchange Patterns

### 8.1 Stdio Container Transport Layer

**Transport**: stdio (JSON-RPC 2.0 over stdin/stdout)  
**Deployment**: Containerized MCP server in `ghcr.io/github/gh-aw-node` image  
**Entrypoint**: `node ${GITHUB_WORKSPACE}/actions/setup/js/safe_outputs_mcp_server.cjs`  
**Operation Mode**: Stateless (no session management)

The safe-outputs MCP server runs as a containerized stdio MCP server managed by the MCP gateway. The gateway communicates with it via the standard stdio JSON-RPC 2.0 protocol. The container has direct read-write access to the workspace, safe-outputs runtime files (`${RUNNER_TEMP}/gh-aw/safeoutputs`), and the MCP log directory (`/tmp/gh-aw/mcp-logs/safeoutputs`).

**Required Mounts**:
- `${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw` — workspace access for reading configs and writing outputs
- `${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw` — safeoutputs runtime files (config.json, tools.json, output.ndjson)
- `/tmp/gh-aw/mcp-logs/safeoutputs:/tmp/gh-aw/mcp-logs/safeoutputs:rw` — MCP server log directory

**Methods**:

- `tools/list` - List available tools
- `tools/call` - Invoke tool

### 8.2 Tool Invocation Protocol

**Request Format** (JSON-RPC 2.0 over stdio):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "<tool_name>",
    "arguments": {<parameters>}
  }
}
```

**Success Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [{
      "type": "text",
      "text": "{\"result\":\"success\"}"
    }]
  }
}
```

**Validation Error**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32602,
    "message": "Invalid params: <details>"
  }
}
```

### 8.3 MCP Server Constraint Enforcement

**Requirement MCE1: Early Validation**

MCP servers MUST enforce operational constraints during tool invocation (Phase 4) rather than deferring all validation to safe output processing (Phase 6). This provides immediate feedback to the LLM, enabling corrective action before operations are recorded to NDJSON.

**Constraint Categories**:

1. **Length Limits**: Character count restrictions on text fields
2. **Entity Limits**: Maximum counts for mentions, links, or other entities
3. **Format Requirements**: Pattern validation, encoding checks
4. **Business Rules**: Type-specific constraints based on safe output configuration

**Requirement MCE2: Tool Description Disclosure**

Tool descriptions (MCP tool schemas) MUST surface enforced constraints to the LLM through the `description` field. This enables the LLM to self-regulate and avoid constraint violations.

**Example - add_comment constraints**:

```json
{
  "name": "add_comment",
  "description": "Add a comment to an existing GitHub issue, pull request, or discussion. IMPORTANT: Comments are subject to validation constraints enforced by the MCP server - maximum 65536 characters for the complete comment (including footer which is added automatically), 10 mentions (@username), and 50 links. Exceeding these limits will result in an immediate error with specific guidance.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "body": {
        "type": "string",
        "description": "The comment text in Markdown format. CONSTRAINTS: The complete comment (your body text + automatically added footer) must not exceed 65536 characters total. A footer (~200-500 characters) is automatically appended, so leave adequate space. Maximum 10 mentions (@username), maximum 50 links (http/https URLs). If these limits are exceeded, the tool call will fail with a detailed error message indicating which constraint was violated."
      }
    }
  }
}
```

**Requirement MCE3: Actionable Error Responses**

When constraints are violated, MCP servers MUST return error responses that:

1. **Identify the violated constraint** with specific name and limit
2. **Report the actual value** that triggered the violation
3. **Provide remediation guidance** on how to correct the issue
4. **Use standard error codes** (E006-E008 for add_comment limits)

**Example - Mention Limit Violation**:

```json
{
  "error": {
    "code": -32602,
    "message": "E007: Comment contains 15 mentions, maximum is 10",
    "data": {
      "constraint": "max_mentions",
      "limit": 10,
      "actual": 15,
      "guidance": "Reduce the number of @mentions in your comment to 10 or fewer. Consider tagging only essential participants."
    }
  }
}
```

**Requirement MCE4: Dual Enforcement**

Constraints MUST be enforced at both invocation time (MCP server) and processing time (safe output processor) to provide defense-in-depth:

- **MCP Server Enforcement**: Provides immediate LLM feedback during agent execution
- **Processor Enforcement**: Validates operations recorded to NDJSON as final safety check

This dual enforcement pattern ensures constraints cannot be bypassed through malformed NDJSON or direct artifact manipulation.

**Implementation Pattern**:

```javascript
// MCP Server - Immediate validation during tool call
function handleAddComment(args) {
  enforceCommentLimits(args.body); // Throws if limits exceeded
  recordOperation('add_comment', args);
  return {result: {content: [{type: "text", text: '{"result":"success"}'}]}};
}

// Safe Output Processor - Final validation before API call  
async function processAddComment(operation) {
  enforceCommentLimits(operation.body); // Defense-in-depth
  const sanitized = sanitizeContent(operation);
  await github.rest.issues.createComment(sanitized);
}
```

**Requirement MCE5: Constraint Configuration Consistency**

Constraint limits defined in MCP tool descriptions MUST match the enforcement logic in both the MCP server and safe output processor implementations. Inconsistent limits between these components violate the specification.

**Verification**:

- **Method**: Code review and integration testing
- **Tool**: Automated tests comparing tool descriptions to handler enforcement code
- **Criteria**: All constraint limits are identical across tool schema, MCP server, and processor

**Example Constraints for Common Safe Output Types**:

| Type | Constraint | Limit | Error Code |
|------|-----------|-------|------------|
| `add_comment` | Body length (complete comment with footer) | 65536 chars | E006 |
| `add_comment` | Mentions | 10 | E007 |
| `add_comment` | Links | 50 | E008 |
| `create_issue` | Title length | 256 chars | E009 |
| `create_issue` | Body length (complete body with footer) | 65536 chars | E006 |
| `create_pull_request` | Title length | 256 chars | E009 |
| `create_pull_request` | Body length (complete body with footer) | 65536 chars | E006 |

**Note**: For operations that append footers (comments, issues, pull requests), the character limit applies to the complete text including the automatically added footer. Users should reserve approximately 200-500 characters to accommodate the footer.

**Rationale**: Early constraint enforcement transforms validation failures from post-execution errors (requiring workflow reruns) into correctable feedback during agent reasoning. This improves agent effectiveness by enabling iterative refinement and reduces wasted compute on operations that will ultimately fail validation.

### 8.4 NDJSON Persistence

**File**: `/tmp/gh-aw/safeoutputs/output.ndjson`  
**Format**: One JSON object per line  
**Encoding**: UTF-8

**Entry Structure**:

```json
{"type":"<safe_output_type>","<param1>":"<value1>"}
```

**Parsing**:

- Read line-by-line
- Parse each line independently
- Ignore empty lines
- Validate `type` field presence

---

## 9. Content Integrity Mechanisms

### 9.1 Schema Validation

All tool invocations MUST validate against JSON Schema Draft 7.

**Validation Process**:

1. Parse invocation arguments
2. Load tool schema
3. Validate against schema
4. Report all errors with paths

**Error Format**:

```json
{
  "error": {
    "code": -32602,
    "message": "Invalid params",
    "data": {
      "errors": [
        {"path": "/title", "message": "Missing required field"}
      ]
    }
  }
}
```

### 9.2 Cross-Field Validation

Some validations span multiple fields:

- Conditional requirements (e.g., `discussion_number` required when target is "*")
- Range constraints (e.g., `start_line < line` in review comments)
- Mutual exclusivity checks

### 9.3 Repository Feature Validation

Operations requiring repository features must validate availability:

- Issues enabled
- Discussions enabled
- Projects enabled

Validation occurs during execution, not tool invocation.

### 9.4 Content Sanitization Pipeline

**Applicability**

Content sanitization MUST be applied to all user-provided text fields in safe output operations. Text fields include:

- `title` (issues, PRs, discussions, projects)
- `body` (issues, PRs, discussions, comments)
- `description` (projects, status updates)
- `comment` (review comments)

**Sanitization Stages**

Implementations MUST apply these transformations in order:

**S1: Null Byte Removal**

- Remove all null bytes (`\0`, `\x00`) from strings
- Rationale: Prevents string truncation attacks

**S2: Markdown Link Validation**

- Pattern: `[text](url)` and `<url>`
- For each URL:
  - Extract domain
  - If `allowed-domains` is configured:
    - Check domain against allowlist
    - If not allowed: Replace with `[text]([URL redacted: unauthorized domain])`
  - Log redacted URLs to `/tmp/gh-aw/safeoutputs/redacted-domains.log`

**S3: Markdown Image Validation**

- Pattern: `![alt](url)`
- For each image URL:
  - Extract domain
  - If `allowed-domains` is configured:
    - Check domain against allowlist
    - If not allowed: Replace with `![alt]([Image URL redacted: unauthorized domain])`

**S4: HTML Tag Filtering** (Optional, depends on field type)

- Remove potentially dangerous tags:
  - `<script>`, `</script>`
  - `<iframe>`, `</iframe>`
  - `<object>`, `</object>`
  - `<embed>`, `</embed>`
- Remove event handlers:
  - `on*` attributes in HTML tags (onclick, onerror, etc.)
- Preserve safe GitHub Flavored Markdown tags:
  - `<details>`, `<summary>`, `<sub>`, `<sup>`, `<kbd>`

**S5: Command Injection Prevention**

- Do NOT execute or interpret code blocks
- Do NOT evaluate template expressions
- Preserve code blocks verbatim (no escaping needed in markdown)

**Excluded Content**

The following content MUST NOT be sanitized:

- Code blocks (` ``` `)
- Inline code (`` `code` ``)
- System-generated footers
- System-generated metadata

**Sanitization Reversibility**

Sanitization transformations are LOSSY and NOT reversible. Original content is not preserved after sanitization. This is intentional to prevent attempts to bypass sanitization.

**Conformance Requirement CR1: Pre-API Sanitization**

All content MUST be sanitized BEFORE GitHub API invocation. Unsanitized content MUST NEVER be passed to GitHub APIs.

*Verification*: Inspect handler code to confirm sanitization occurs before `octokit.*` calls.

### 9.5 Error Code Catalog

Implementations MUST use standardized error codes for validation and execution failures.

**Error Code Table**

| Code | Name | Description | When to Use | HTTP Status Equivalent |
|------|------|-------------|-------------|------------------------|
| E001 | INVALID_SCHEMA | Operation failed JSON schema validation | Input does not match type-specific schema | 400 Bad Request |
| E002 | LIMIT_EXCEEDED | Operation count exceeds configured max | Batch contains more operations than allowed | 429 Too Many Requests |
| E003 | UNAUTHORIZED_DOMAIN | URL contains non-allowlisted domain | Domain filtering rejected URL | 403 Forbidden |
| E004 | INVALID_TARGET_REPO | target-repo not in allowed-repos | Cross-repository validation failed | 403 Forbidden |
| E005 | MISSING_PARENT | Referenced parent issue/PR not found | Temporary ID or parent reference cannot be resolved | 404 Not Found |
| E006 | INVALID_LABEL | Label does not exist in repository | Label validation failed | 404 Not Found |
| E007 | API_ERROR | GitHub API returned error | GitHub API call failed | 502 Bad Gateway |
| E008 | SANITIZATION_FAILED | Content contains unsanitizable unsafe patterns | Sanitization pipeline detected unremovable threats | 422 Unprocessable Entity |
| E009 | CONFIG_HASH_MISMATCH | Configuration hash verification failed | Workflow YAML was modified after compilation | 403 Forbidden |
| E010 | RATE_LIMIT_EXCEEDED | GitHub API rate limit exceeded | Too many API calls | 429 Too Many Requests |

**Error Message Format**

All errors MUST conform to this JSON structure:

```json
{
  "error": {
    "code": "E002",
    "name": "LIMIT_EXCEEDED",
    "message": "Operation count exceeds configured limit",
    "details": {
      "type": "create_issue",
      "attempted": 5,
      "max": 3,
      "operation_index": 3
    },
    "timestamp": "2026-02-14T16:39:20.948Z",
    "workflow_run": "https://github.com/owner/repo/actions/runs/12345"
  }
}
```

**Required Fields**:

- `code`: Error code from table above (E001-E010)
- `name`: Error name from table above
- `message`: Human-readable description
- `timestamp`: ISO 8601 timestamp

**Optional Fields**:

- `details`: Type-specific error context (operation_index, field names, etc.)
- `workflow_run`: URL to workflow run for provenance

**Error Handling Requirements**

**Requirement EH1: Early Failure Detection**

Validation errors (E001-E006) MUST be detected before any GitHub API calls are made.

**Requirement EH2: Clear Error Messages**

Error messages MUST:

- Clearly state what went wrong
- Include enough context to debug (field names, values)
- Suggest remediation when possible

**Requirement EH3: Error Logging**

All errors MUST be logged to:

- GitHub Actions step output (visible in workflow run)
- Job summary (visible in workflow run summary)
- STDERR (for local development)

---

## 10. Execution Guarantees

### 10.1 Atomicity

**Single-Item Operations**: Complete success or complete failure (no partial state).

**Batch Operations**: Best-effort semantics; partial success reported.

### 10.2 Ordering

Operations execute in:

1. NDJSON file order
2. Type grouping (same type together)
3. System types last (noop, missing_tool, missing_data, report_incomplete)

### 10.3 Idempotency

**Idempotent Operations**:

- add_labels (adding present label)
- remove_labels (removing absent label)
- hide_comment (hiding hidden comment)

**Non-Idempotent Operations**:

- create_issue
- create_discussion
- add_comment

### 10.4 Error Handling

**Fail-Safe Principle**: One operation's failure doesn't prevent others from attempting.

**Error Reporting**: All errors collected; execution summary reports per-type results.

### 10.5 Warn-Mode Threat Detection Failure Policy

When threat detection executes in `warn` mode and reports a threat signal for a safe output, implementations MUST apply a type-specific fallback policy before any safe output side effect is committed.

**Requirement WTD1 (Reviewable Annotation)**: For safe output types classified as **Reviewable** in Table WTD-A, implementations MUST convert the output into a review-first artifact that includes all of the following when threat detection reports an actual threat verdict (`threat_detected`):

1. A prominent caution section:

   > [!CAUTION]
   > agentic threat detected
   > Threat detection flagged this output in warn mode. Manual review is REQUIRED before any follow-up automation.

2. A visible threat label string: `agentic threat detected`.
3. An XML comment marker in emitted markdown content: `<!-- gh-aw-threat-detected -->`.

**Requirement WTD1a (Threat Engine Error Annotation)**: When warn-mode threat detection produces a tooling-failure reason (`agent_failure` or `parse_error`) instead of a real threat verdict, implementations MUST emit a review-first artifact with all of the following:

1. A prominent warning section:

   > [!WARNING]
   > threat detection engine error
   > The threat detection engine encountered an error and could not complete analysis. This is a tooling failure, not a security finding.

2. A visible warning label string: `threat detection engine error`.
3. An XML comment marker in emitted markdown content: `<!-- gh-aw-threat-engine-error -->`.

**Requirement WTD2 (Convertible Fallback)**: For safe output types classified as **Convertible**, implementations MUST transform the operation into the mapped Reviewable type before execution. For this specification, `push_to_pull_request_branch` (also referred to as `update-pull-request-branch`) MUST fall back to `create_pull_request` with the WTD1 or WTD1a review annotation that matches the detection reason.

**Requirement WTD3 (Non-Reviewable Abort)**: For safe output types classified as **Abort**, implementations MUST NOT apply the original safe output. Implementations MUST activate a threat-detected code path, emit an explicit failure summary, and return a machine-readable threat-detected error outcome.

**Table WTD-A: Warn-Mode Threat Detection Failure Policy by Safe Output Type**

| Safe output type | Warn-mode failure policy |
|---|---|
| `create_issue` | Reviewable |
| `add_comment` | Reviewable |
| `create_pull_request` | Reviewable |
| `noop` | Abort |
| `comment_memory` | Reviewable |
| `update_issue` | Reviewable |
| `close_issue` | Abort |
| `link_sub_issue` | Abort |
| `create_discussion` | Reviewable |
| `update_discussion` | Reviewable |
| `close_discussion` | Abort |
| `update_pull_request` | Reviewable |
| `close_pull_request` | Abort |
| `approve_workflow_run` | Abort |
| `merge_pull_request` | Abort |
| `mark_pull_request_as_ready_for_review` | Abort |
| `push_to_pull_request_branch` | Convertible (`create_pull_request`) |
| `create_pull_request_review_comment` | Reviewable |
| `submit_pull_request_review` | Reviewable |
| `resolve_pull_request_review_thread` | Abort |
| `reply_to_pull_request_review_comment` | Reviewable |
| `add_labels` | Abort |
| `remove_labels` | Abort |
| `add_reviewer` | Abort |
| `assign_milestone` | Abort |
| `assign_to_agent` | Abort |
| `assign_to_user` | Abort |
| `unassign_from_user` | Abort |
| `hide_comment` | Abort |
| `create_project` | Abort |
| `update_project` | Abort |
| `create_project_status_update` | Reviewable |
| `update_release` | Reviewable |
| `upload_asset` | Abort |
| `dispatch_workflow` | Abort |
| `create_code_scanning_alert` | Reviewable |
| `autofix_code_scanning_alert` | Abort |
| `create_agent_session` | Abort |
| `missing_tool` | Reviewable |
| `missing_data` | Reviewable |
| `report_incomplete` | Reviewable |

**Compliance Testing**:

- **T-WTD-001**: Reviewable outputs include the threat-appropriate annotation: WTD1 CAUTION block/label/marker for `threat_detected`, or WTD1a WARNING block/label/marker for `agent_failure` and `parse_error`.
- **T-WTD-002**: `push_to_pull_request_branch` in warn-mode threat failure is converted to `create_pull_request`.
- **T-WTD-003**: Abort-class outputs are not applied and produce threat-detected error outcomes.

### 10.6 Edge Case Behavior

This section defines required behavior for unusual or boundary conditions.

**Empty Operations**

*Scenario*: NDJSON artifact contains zero operations

*Behavior*:

- Safe output job MUST succeed (exit code 0)
- Job summary SHOULD display: "✅ No operations to process"
- No GitHub API calls are made
- No errors are raised

*Rationale*: Empty operations are valid (agent may determine no action is needed).

**Zero Max Limit**

*Scenario*: Configuration specifies `max: 0` for a safe output type

*Behavior*:

- Type is DISABLED (MCP tool is not registered)
- Attempts to invoke disabled type MUST return MCP error:

  ```json
  {"error": {"code": -32601, "message": "Method not found"}}
  ```

- No configuration is generated for disabled types

*Rationale*: `max: 0` is an explicit disable signal.

**API Rate Limiting**

*Scenario*: GitHub API returns 429 (rate limit exceeded) or 403 with X-RateLimit-Remaining: 0

*Behavior*:

- Processor MUST retry with exponential backoff:
  - 1st retry: After 60 seconds
  - 2nd retry: After 120 seconds  
  - 3rd retry: After 240 seconds
- After 3 retries, MUST fail with E010 error
- Error details MUST include rate limit reset time from `X-RateLimit-Reset` header

*Rationale*: Transient rate limits should not fail workflows unnecessarily.

**Workflow Cancellation**

*Scenario*: Workflow is manually cancelled during agent execution

*Behavior*:

- Safe output job MUST NOT execute if artifact upload was interrupted
- Partial NDJSON artifacts MUST NOT be processed
- GitHub Actions automatically handles cleanup
- No additional logic required in handlers

*Rationale*: GitHub Actions cancellation is handled at platform level.

**Concurrent Workflow Runs**

*Scenario*: Multiple workflow runs execute concurrently for the same workflow

*Behavior*:

- Each run operates independently
- Max limits are per-run (NOT global across runs)
- No coordination or locking between runs
- Operations in separate runs do NOT affect each other's limits

*Rationale*: Simplicity and avoiding distributed coordination complexity.

**Malformed NDJSON**

*Scenario*: NDJSON artifact contains invalid JSON on one or more lines

*Behavior*:

- Parser MUST skip invalid lines with warning
- Valid lines MUST be processed
- Job summary MUST show: "⚠️ Skipped N malformed entries"
- Invalid lines MUST be logged to STDERR

*Rationale*: Partial failure should not prevent valid operations from executing.

**Missing Artifact**

*Scenario*: Safe output job cannot download artifact (artifact not found)

*Behavior*:

- Job MUST fail with clear error message
- Error MUST suggest checking agent job completion
- Exit code MUST be non-zero

*Rationale*: Missing artifact indicates upstream failure that must be addressed.

**Duplicate Temporary IDs**

*Scenario*: Multiple operations use the same `temporary_id`

*Behavior*:

- First operation using the ID succeeds and establishes mapping
- Subsequent operations using the same ID MUST reference the first operation's result
- If this creates ambiguity (e.g., two issues both want to be "aw_parent"), MUST reject with E005

*Rationale*: Deterministic behavior prevents confusion.

**Replay Authorization with Custom Repository Roles**

*Scenario*: A user manually replays safe outputs through the Agentic Maintenance workflow in a repository that uses custom organization repository roles.

*Behavior*:

- Implementations MUST authorize replay only for actors whose effective standard repository role is exactly `admin` or `maintain`.
- When GitHub reports a custom repository role name, implementations MUST resolve authorization from the inherited standard role metadata supplied by GitHub, not from the custom role name string.
- Implementations MUST NOT infer exact authorization from the legacy `permission` bucket alone, because that bucket may collapse `maintain` to `write` and `triage` to `read`.
- If the inherited standard role for a custom repository role cannot be resolved, the replay request MUST be rejected.

*Rationale*: This preserves exact-match authorization semantics for standard roles while still permitting custom organization roles that inherit an authorized standard role.

---

## 11. Cache Memory Integrity

### 11.1 Overview and Motivation

The cache-memory subsystem provides agents with a persistent filesystem share backed by GitHub Actions cache. Prior to this specification version, caches used a flat directory structure with no integrity provenance. This allowed a `none`-integrity agent to write data into a shared cache store that was subsequently restored and consumed by a higher-integrity run—a Bell-LaPadula write-up violation (Threat T6).

This section specifies the integrity-aware cache architecture that prevents cross-integrity cache contamination while preserving the ability for lower-integrity runs to read data produced by higher-integrity runs (read-down semantics).

**Design Goals**:

1. **Write isolation**: Data written at integrity level *L* MUST NOT be visible to a run at integrity level *H* where trust(*H*) > trust(*L*) (no write-up).
2. **Read-down access**: A run at integrity level *L* MAY read data produced by runs at higher integrity levels (read-down is permitted and expected).
3. **Policy binding**: A cache entry MUST be invalidated when the guard policy changes, preventing data inherited under one policy from being consumed under a different, potentially more permissive policy.
4. **Transparency**: The agent MUST remain unaware of the git repository structure within the cache directory. The agent reads and writes plain files as normal.
5. **Migration**: Legacy flat-file caches (with no `.git` directory) MUST be automatically imported onto the `merged` integrity branch on first use.

### 11.2 Integrity Levels

Four integrity levels are defined, ordered from highest to lowest trust:

| Level | Description |
|-------|-------------|
| `merged` | Content that has passed code review and been merged into the default branch |
| `approved` | Content from pull requests that have been reviewed and approved |
| `unapproved` | Content from open, un-approved pull requests |
| `none` | Content from workflows without a configured guard policy |

The ordering MUST be: `merged` > `approved` > `unapproved` > `none`.

### 11.3 Integrity-Aware Cache Key Format

**Requirement CI1: Integrity-Scoped Keys**

All cache-memory keys MUST include the integrity level and policy hash as prefixes, in the following format:

```
memory-{integrityLevel}-{policyHash}-[{cacheID}-]{workflowID}-{runID}
```

Where:

- `{integrityLevel}` is the `min-integrity` value from the guard policy, or `none` when no guard policy is configured.
- `{policyHash}` is the 8-character hex prefix of the SHA-256 policy hash (see Section 11.4), or the sentinel string `nopolicy` when no guard policy is configured.
- `{cacheID}` is the user-defined cache identifier. The `default` cache ID MUST be omitted from the key to maintain a clean format.
- `{workflowID}` is the sanitized workflow identifier (`GH_AW_WORKFLOW_ID_SANITIZED`).
- `{runID}` is the GitHub Actions run identifier (`github.run_id`).

**Examples**:

```
# Default cache, with guard policy (min-integrity: unapproved, 8-char policy hash)
memory-unapproved-7e4d9f12-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}

# Default cache, no guard policy
memory-none-nopolicy-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}

# Named "session" cache, no guard policy
memory-none-nopolicy-session-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}
```

**Requirement CI2: Restore Key Cascade**

Restore keys MUST use the same integrity-scoped prefix so that a partial key match never crosses integrity level boundaries:

```
restore-keys: |
  memory-{integrityLevel}-{policyHash}-{workflowID}-
  memory-{integrityLevel}-{policyHash}-
  memory-
```

The final fallback `memory-` entry exists solely to allow migration from legacy (non-scoped) caches and MUST be removed in a future major version.

### 11.4 Policy Hash Computation

**Requirement CI3: Deterministic Policy Hash**

The policy hash MUST be computed as the first 8 characters of the lowercase hex SHA-256 digest of a canonical policy string, constructed as follows:

1. For each of the following fields, produce a canonical value:
   - `blocked-users`: Lowercase, sort, deduplicate. If specified as a GitHub Actions expression (e.g., `${{ github.event.sender.login }}`), prefix the raw expression with `expr:` (e.g., `expr:${{ github.event.sender.login }}`).
   - `min-integrity`: Use the literal string value.
   - `repos`: If a string (`"all"` or `"public"`), lowercase. If an array, lowercase all entries, sort, and deduplicate.
   - `trusted-bots`: Reserved for future use; always empty.
   - `trusted-users`: Reserved for future use; always empty.

2. Concatenate the fields in the fixed order shown below, each followed by a newline:

   ```
   blocked-users:{canonicalBlockedUsers}\n
   min-integrity:{minIntegrity}\n
   repos:{canonicalRepos}\n
   trusted-bots:\n
   trusted-users:{canonicalTrustedUsers}
   ```

3. Compute SHA-256 over the UTF-8 encoding of the canonical string.
4. Take the first 8 characters of the lowercase hexadecimal representation.

**Requirement CI4: Sentinel for No-Policy Workflows**

Workflows without a configured `min-integrity` field MUST use the sentinel string `nopolicy` in place of the policy hash.

**Rationale**: The sentinel avoids hash computation for the common case of no guard policy and is visually distinguishable from a genuine policy hash in cache key inspection.

### 11.5 Git-Backed Integrity Branching

The cache-memory directory MUST be a Git repository when integrity branching is active. The `.git` directory rides along within the GitHub Actions cache tarball, persisting integrity branch history across workflow runs.

**Repository Structure**:

```
/tmp/gh-aw/cache-memory/
├── .git/               ← Git metadata (integrity branches, history)
│   └── refs/heads/
│       ├── merged
│       ├── approved
│       ├── unapproved
│       └── none
├── file-written-by-merged-run.json
└── file-written-by-unapproved-run.txt
```

**Agent Transparency**:

The agent MUST see and interact with only the plain files in the working directory. The agent MUST NOT need knowledge of Git or the branching structure. File system operations (read, write, delete) behave normally from the agent's perspective.

**Requirement CI5: .git Directory Exclusion from Validation**

File validation steps that enforce allowed extensions, size limits, or other constraints MUST skip the `.git` directory. The Git metadata directory contains binary and extension-less files that are not agent-managed content.

### 11.6 Pre-Agent Setup (Integrity Checkout)

A setup step MUST execute after the cache is restored and before the agent runs. The reference implementation of this step is `actions/setup/sh/setup_cache_memory_git.sh` (informative). All conforming implementations MUST satisfy requirements CI6–CI9 regardless of the implementation mechanism.

**Requirement CI6: Git Repository Initialization**

If the restored cache directory does not contain a `.git` subdirectory (fresh or legacy cache), the implementation MUST:

1. Initialize a new Git repository on the `merged` branch.
2. Stage and commit all existing files (if any) as an `initial` commit. This migrates legacy flat-file caches automatically.
3. Create all four integrity branches (`merged`, `approved`, `unapproved`, `none`) from the same baseline commit.

**Requirement CI7: Integrity Branch Checkout**

After initialization (or if the repository already exists), the implementation MUST check out the branch corresponding to the run's `min-integrity` value. If `min-integrity` is absent, the `none` branch MUST be used.

**Requirement CI8: Merge-Down from Higher-Integrity Branches**

Before the agent executes, the implementation MUST merge all higher-integrity branches into the current branch, in descending trust order (highest first), using the `theirs` merge strategy (`-X theirs`) so that higher-integrity content takes precedence in conflicts.

The merge semantics table is:

| Run integrity | Branches merged in (read access) | Branches NOT merged in |
|---------------|----------------------------------|------------------------|
| `merged`      | (none — highest, no merge-down)  | `approved`, `unapproved`, `none` |
| `approved`    | `merged`                         | `unapproved`, `none` |
| `unapproved`  | `merged`, `approved`             | `none` |
| `none`        | `merged`, `approved`, `unapproved` | (none — reads all) |

**Requirement CI9: Merge Failure Handling**

If a merge from a higher-integrity branch fails for reasons other than "nothing to merge" or "already up-to-date", the implementation MUST abort the merge, restore the working tree to its pre-merge state, and exit with a non-zero status code to fail the workflow step.

### 11.7 Post-Agent Commit (Integrity Persistence)

A commit step MUST execute after the agent completes and before the cache is saved. The reference implementation is `actions/setup/sh/commit_cache_memory_git.sh` (informative). The step MUST execute regardless of whether the agent step succeeded or failed (i.e., unconditional execution, not gated on agent success).

**Requirement CI10: Agent Changes Committed**

The implementation MUST:

1. Stage all changes within the cache directory (`git add -A`).
2. Commit on the current integrity branch with a message of the form `run-{GITHUB_RUN_ID}`.
3. Allow empty commits (`--allow-empty`) so that runs that made no file changes still produce a commit marker in the branch history.

**Requirement CI11: Repository Compaction**

After committing, the implementation MUST invoke `git gc --auto` to prevent unbounded growth of the Git object database within the cache tarball.

**Requirement CI12: No-Repository Fallback**

If no `.git` directory is present at commit time (e.g., the setup step was skipped), the commit step MUST exit cleanly with a diagnostic message and MUST NOT fail the workflow.

### 11.8 Lifecycle Diagram

The following diagram illustrates the full per-run lifecycle:

```
GitHub Actions Cache Restore
        │
        ▼
setup_cache_memory_git.sh
  1. If no .git: git init -b merged, import files, create all branches
  2. git checkout {integrity}
  3. For each higher-integrity branch (descending):
       git merge {branch} -X theirs
        │
        ▼
Agent Execution
  (reads/writes plain files — unaware of git)
        │
        ▼
commit_cache_memory_git.sh  [if: always()]
  1. git add -A
  2. git commit --allow-empty -m "run-{run_id}"
  3. git gc --auto
        │
        ▼
GitHub Actions Cache Save
  (tarball includes .git directory with all integrity branches)
```

### 11.9 Compliance Requirements

| Requirement | Test ID | Level |
|-------------|---------|-------|
| CI1: Integrity-scoped cache keys | T-CI-001 | Required |
| CI2: Restore key cascade | T-CI-002 | Required |
| CI3: Deterministic policy hash | T-CI-003 | Required |
| CI4: Sentinel for no-policy workflows | T-CI-004 | Required |
| CI5: .git directory excluded from validation | T-CI-005 | Required |
| CI6: Git repository initialization | T-CI-006 | Required |
| CI7: Integrity branch checkout | T-CI-007 | Required |
| CI8: Merge-down from higher-integrity branches | T-CI-008 | Required |
| CI9: Merge failure handling | T-CI-009 | Required |
| CI10: Agent changes committed | T-CI-010 | Required |
| CI11: Repository compaction | T-CI-011 | Recommended |
| CI12: No-repository fallback | T-CI-012 | Required |

### 11.10 Migration from Legacy Flat-File Caches

Existing deployments using the pre-integrity cache format MUST expect a **cache miss** on the first run after upgrading to an implementation supporting this section.

**Legacy key format** (before this section):
```
memory-{workflowID}-{runID}
# Example: memory-my-workflow-12345678
```

**New key format** (this section):
```
memory-{integrityLevel}-{policyHash}-{workflowID}-{runID}
# Example (with policy):      memory-unapproved-7e4d9f12-my-workflow-12345678
# Example (without policy):   memory-none-nopolicy-my-workflow-12345678
```

The integrity level and policy hash prefixes are new components not present in legacy keys. Because the key formats differ, legacy cache entries will never match the new restore keys, resulting in a one-time cache miss.

*Rationale*: Legacy cache data has no integrity provenance. Blindly consuming legacy data under the new integrity model would provide no security guarantee. The automatic migration path in Requirement CI6 handles any residual files from the old format by importing them to the `merged` branch on first initialization.

Operators SHOULD communicate this expected one-time cache miss to their teams to avoid confusion during upgrade.

---



**Required for Full Conformance**:

- [ ] Security Architecture
  - [ ] Privilege separation enforced
  - [ ] Artifact-based communication
  - [ ] Threat mitigations implemented
  - [ ] Security properties maintained

- [ ] Configuration
  - [ ] All global parameters supported
  - [ ] Type-specific parameters supported
  - [ ] Inheritance rules followed
  - [ ] Compilation-time validation

- [ ] Universal Features
  - [ ] Max limit enforcement
  - [ ] Staged mode preview generation
  - [ ] Footer injection
  - [ ] Content sanitization pipeline

- [ ] Safe Output Types
  - [ ] Mandatory: create_issue, add_comment, create_pull_request, noop, missing_tool, missing_data, report_incomplete
  - [ ] Optional types documented if unsupported

- [ ] Protocol
  - [ ] stdio container transport (containerized MCP server in `gh-aw-node` image)
  - [ ] MCP tool invocation
  - [ ] NDJSON persistence

- [ ] Content Security
  - [ ] Schema validation
  - [ ] Domain filtering
  - [ ] Sanitization pipeline

- [ ] Execution Guarantees
  - [ ] Atomicity for single-item operations
  - [ ] Best-effort for batch operations
  - [ ] Fail-safe error handling

---

## Appendix B: Security Considerations

### Attack Surface Analysis

**Entry Points**:

1. Agent-provided tool arguments
2. Configuration in frontmatter
3. GitHub API responses

**Trust Boundaries**:

- Agent context (untrusted)
- MCP Gateway (semi-trusted)
- Safe output processor (trusted)
- GitHub API (trusted)

### Mitigation Effectiveness

Detailed threat analysis and mitigation effectiveness assessment for all five primary threats (see Section 3.2).

---

## Appendix C: Implementation Guidance

### Recommended Practices

1. **Conservative Limits**: Start with minimal max values
2. **Staged Mode Development**: Test workflows in preview mode first
3. **Explicit Domain Lists**: Use restrictive domain filtering
4. **Expires for Temporary Resources**: Auto-close temporary issues

### Common Pitfalls

1. **Unlimited Max**: Removes important safety constraint
2. **Permissive Domains**: Loses URL filtering protection
3. **Cross-Repo Without Allowlist**: Permits arbitrary targets
4. **Disabled Footers**: Reduces transparency

---

## Appendix D: Normative References

- **RFC 2119**: Key words for RFCs (MUST, SHALL, etc.)
- **JSON Schema Draft 7**: JSON Schema specification
- **NDJSON**: Newline Delimited JSON format
- **MCP Specification**: Model Context Protocol

---

## Appendix E: Informative References

- **GitHub REST API**: <https://docs.github.com/rest>
- **GitHub Actions**: <https://docs.github.com/actions>
- **MCP Gateway Specification**: /gh-aw/reference/mcp-gateway/

---

## Appendix G: Configuration Patterns

This appendix provides common configuration patterns for safe outputs.

### Pattern 1: Simple Issue Tracking

Basic configuration for automated issue creation:

```yaml
safe-outputs:
  create-issue:
    max: 1
    labels: [automated]
```

**Use case**: Single automated issue per workflow run with consistent labeling.

### Pattern 2: Multi-Type with Global Footer

Configuration with multiple output types sharing global settings:

```yaml
safe-outputs:
  footer: true  # Applied to all types
  
  create-issue:
    max: 3
    labels: [bug, automated]
  
  add-comment:
    max: 2
    hide-older-comments: true
```

**Use case**: Workflow creating multiple issues and comments with attribution footers.

### Pattern 3: Cross-Repository Operations

Secure cross-repository issue creation:

```yaml
safe-outputs:
  allowed-github-references:
    - owner/repo-a
    - owner/repo-b
  
  create-issue:
    max: 5
    target-repo: owner/repo-a
```

**Use case**: Creating issues in a central tracking repository from multiple workflow repositories.

**Security note**: Explicit allowlist prevents unauthorized repository targeting.

### Pattern 4: Staged Mode Development

Safe testing in preview mode:

```yaml
safe-outputs:
  staged: true  # Enable preview mode globally
  
  create-issue:
    max: 10  # Safe to set high in staged mode
  
  add-comment:
    max: 5
```

**Use case**: Testing workflow behavior without creating real GitHub resources.

**Workflow**: Test with `staged: true`, verify previews, then deploy with `staged: false`.

### Pattern 5: Type-Specific Allowlists

Fine-grained cross-repository control:

```yaml
safe-outputs:
  allowed-github-references: [owner/repo-a, owner/repo-b]
  
  create-issue:
    allowed-repos: [owner/repo-c]  # Overrides global
    max: 3
    
  add-comment:
    # No type-specific list, uses global: repo-a, repo-b
    max: 2
```

**Use case**: Different safe output types target different repositories.

**Security note**: Type-specific allowlists override global allowlists.

### Pattern 6: Domain Filtering for Security

Restrict URLs in safe output content:

```yaml
safe-outputs:
  allowed-domains:
    - github.com
    - "*.github.io"
    - docs.github.com
  
  create-issue:
    max: 5
```

**Use case**: Prevent agents from including unauthorized URLs in created content.

**Effect**: URLs to non-allowlisted domains are redacted during sanitization.

### Pattern 7: Temporary Resource Cleanup

Auto-close temporary issues:

```yaml
safe-outputs:
  create-issue:
    max: 10
    expires: 7  # Auto-close after 7 days
    labels: [temporary, automated]
```

**Use case**: Issues for transient notifications that should auto-clean.

**Implementation**: Scheduled workflow checks issue age and closes expired issues.

### Pattern 8: Review Comment Workflow

Pull request review automation with reply support:

```yaml
safe-outputs:
  create-pr-review-comment:
    max: 20
    
  submit-pr-review:
    max: 1
    
  reply-to-pull-request-review-comment:
    max: 10
    
  resolve-pr-review-thread:
    max: 10
```

**Use case**: Automated code review with inline comments, review replies, and thread resolution.

**Workflow**: Create review comments, submit bundled review, reply to reviewer feedback, resolve addressed threads.

### Pattern 9: Project Management

Automated project creation and updates:

```yaml
safe-outputs:
  create-project:
    max: 1
    
  update-project:
    max: 5
    
  create-project-status-update:
    max: 3
```

**Use case**: Creating and maintaining project boards automatically.

### Pattern 10: Grouped Issues with Parent

Create related issues under a parent:

```yaml
safe-outputs:
  create-issue:
    max: 10
    group: true
```

**Use case**: Workflow creates parent issue and multiple sub-issues linked via tasklists.

**Effect**: First issue becomes parent, subsequent issues link to it.

### Pattern 11: Least-Privilege Per-Output Apps

Assign separate GitHub Apps to outputs with different permission needs:

```yaml
safe-outputs:
  github-app:
    app-id: ${{ vars.GLOBAL_APP_ID }}
    private-key: ${{ secrets.GLOBAL_APP_PRIVATE_KEY }}

  add-comment:
    github-app:
      app-id: ${{ vars.ISSUE_APP_ID }}
      private-key: ${{ secrets.ISSUE_APP_PRIVATE_KEY }}
    target: ${{ github.event.issue.number }}
    max: 1

  dispatch-workflow:
    github-app:
      app-id: ${{ vars.ACTIONS_APP_ID }}
      private-key: ${{ secrets.ACTIONS_APP_PRIVATE_KEY }}
    workflows: [downstream.yml]
    max: 1
```

**Use case**: One workflow needs both issue-comment writes and workflow-dispatch writes, but repository policy requires separate Apps for those permission domains.

**Security note**:

- The global `github-app` remains the fallback for handlers without a type-specific override
- Each overridden handler receives its own installation token scoped to that handler's permissions only
- Shared/global app permissions exclude handlers that declare their own `github-app`

### Best Practices

**Start Conservative**:

- Begin with low `max` values
- Enable `staged: true` for testing
- Use explicit `allowed-repos` lists

**Use Domain Filtering**:

- Always configure `allowed-domains` when agents process external input
- Include only trusted domains

**Enable Footers**:

- Keep `footer: true` (default) for transparency
- Only disable when absolutely necessary

**Temporary Resources**:

- Use `expires` for transient issues
- Clean up with `close-older-issues` for superseded content

**Cross-Repository Security**:

- Use type-specific `allowed-repos` for fine-grained control
- Prefer explicit lists over broad permissions

---

## Appendix F: Document History

### Changelog Alignment (Reviewer, Status-Comment, and Hardening Updates)

This specification revision aligns with directly relevant `CHANGELOG.md` entries and with the current reviewer/status-comment PR updates:

- **Commit 9d80a262**: safe-output field validation was hardened so normalized downstream payloads contain only schema/config-declared fields, unconditional agent-controlled `add_comment.comment_id` was removed, upload asset metadata is re-derived by the privileged job, and patch base metadata is embedded in the generated patch.
- **Commit 178ff313**: `add_comment.comment_id` was reintroduced only for workflows that configure `safe-outputs.add-comment.target: "*"` and provide a trusted `safe-outputs.add-comment.allows-comment-ids` allowlist containing the requested comment ID.
- **v0.40.1**: `add_comment` discussion handling was updated to auto-detect discussion context without requiring a `discussion` flag.
- **v0.40.1**: append-only status comment behavior was documented for smoke workflow execution.
- **Earlier changelog entry**: status comments were decoupled from default AI reaction behavior; explicit `on.status-comment` configuration is required when status comments are desired.
- **Earlier changelog entry**: `command` trigger was renamed to `slash_command` with deprecation compatibility.

**Version 1.29.0** (2026-09-02):

- **Added**: `linear_create_issue`, `linear_add_comment`, and `linear_update_issue` Safe Output definitions.
- **Specified**: Fixed Linear GraphQL operations, variable-only dynamic values, API-key authentication, GraphQL error handling, target validation, standard content sanitization, staged execution, and credential isolation.
- **Specified**: Linear-only handlers do not request GitHub write permissions or use GitHub App installation tokens.
- **Updated**: Publication metadata to 1.29.0.

**Version 1.28.6** (2026-08-24):

- **Specified**: Pre-created pull request branches MUST be validated after creation and before downstream jobs treat them as trusted workflow state.
- **Specified**: Pre-created pull request validation MUST confirm the expected deterministic branch ref, valid pull request number, workflow-repository head, and workflow-repository base.
- **Specified**: Agent and safe-output checkouts in pre-created pull request mode MUST use the deterministic workflow-owned branch ref directly rather than activation-output-derived refs.
- **Updated**: Publication metadata to 1.28.6.

**Version 1.28.5** (2026-08-20):

- **Changed**: Default value of the `discussions` field on `hide-comment` inverted from `true` to `false`. The `discussions:write` permission is now opt-in, matching `add-comment`. Set `discussions: true` to hide comments on discussions; omitting the field no longer requests `discussions:write`.
- **Updated**: Section 7 `hide_comment` permission documentation, configuration examples, and notes to reflect the opt-in default.
- **Updated**: Publication metadata to 1.28.5.

**Version 1.28.4** (2026-08-15):

- **Added**: `approve_workflow_run` safe output definition, including its positive run-ID schema, fork-approval eligibility checks, triggering-PR authorization default, optional `allowed-pull-requests`, explicit credential requirement, and `actions: write` permission.
- **Specified**: Staged approval previews MUST not access GitHub or consume the handler max limit; live approvals are Abort operations for warn-mode threat-detection failures.
- **Updated**: Publication metadata to 1.28.4.

**Version 1.28.3** (2026-08-14):

- **Editorial-only**: Added the [safe-outputs scratchpad removal checklist](https://github.com/github/gh-aw/blob/main/specs/safe-outputs-scratchpad-removal.md) to track deletion of a deprecated scratchpad document by 2026-09-21; no normative requirements changed.
- **Updated**: Publication metadata to 1.28.3.

**Version 1.28.2** (2026-08-07):

- **Added**: Controlled `add_comment.comment_id` support for wildcard comment targets. Agent-supplied comment IDs MAY be accepted only when `safe-outputs.add-comment.target` is `"*"` and the exact positive integer ID appears in trusted `safe-outputs.add-comment.allows-comment-ids` workflow state.
- **Specified**: `allows-comment-ids` is REQUIRED before any agent-supplied `comment_id` is honored and accepts literal positive integer IDs, stringified positive integer IDs, or GitHub Actions expressions that resolve to such a list.
- **Updated**: Publication metadata to 1.28.2.

**Version 1.28.1** (2026-08-07):

- **Specified**: Normalized downstream safe-output payloads MUST include only `type` plus schema/config-declared fields, with undeclared agent-supplied fields stripped before handler or privileged-job consumption.
- **Removed**: Unconditional agent-controlled `add_comment.comment_id` from the default MCP input contract; status-comment reuse is limited to `target: "status"` with reusable comment IDs obtained from trusted workflow state.
- **Specified**: Optional advisory/enrichment fields marked `x-strip-on-error` MAY be omitted when invalid.
- **Specified**: Upload asset staging and publication MUST derive collision-resistant staged filenames and asset metadata from trusted staged files rather than agent-supplied metadata.
- **Specified**: Patch base metadata MUST be derived from the generated patch, and agent-supplied `diff_size` and base-commit metadata MUST NOT control privileged patch processing.
- **Updated**: Publication metadata to 1.28.1.

**Version 1.28.0** (2026-07-31):

- **Added**: GP5a specifying `safe-outputs.<type>.github-app` as a per-handler GitHub App override for safe output types.
- **Specified**: Type-specific GitHub App tokens MUST be minted with only the overridden handler's required permissions and MUST take precedence over shared safe-outputs credentials.
- **Specified**: Global `safe-outputs.github-app` permission aggregation excludes handlers that declare their own `github-app` override.
- **Specified**: Handlers with no effective `safe_outputs` GitHub API permission requirement MUST NOT mint a dedicated handler token and MUST fall back to existing token resolution.
- **Updated**: Safe output permission sections to reference the correct `github-app` field name.
- **Updated**: Publication metadata to 1.28.0.

**Version 1.27.0** (2026-07-29):

- **Removed**: `contents: read` from the required permissions of all output-only safe-output handlers. The permission was an unconditional baseline with no functional purpose in the `safe_outputs` job for handlers that only call issue, pull-request, discussion, checks, or security-events APIs. Operators who use tightly scoped GitHub Apps no longer need to justify a repository-contents read grant for pure output workloads.
- **Updated**: Permission tables for `create_issue`, `add_comment`, `close_issue`, `update_issue`, `assign_milestone`, `assign_to_user`, `unassign_from_user`, `assign_to_agent`, `link_sub_issue`, `set_issue_type`, `set_issue_field`, `comment_memory`, `create_discussion`, `update_discussion`, `close_discussion`, `hide_comment`, `add_labels`, `remove_labels`, `replace_label`, `add_reviewer`, `close_pull_request`, `mark_pull_request_as_ready_for_review`, `dismiss_pull_request_review`, `create_pull_request_review_comment`, `submit_pull_request_review`, `reply_to_pull_request_review_comment`, `resolve_pull_request_review_thread`, `update_pull_request` (without `update-branch`), `create_code_scanning_alert`, `autofix_code_scanning_alert`, `create_check_run`, `update_project`, `create_project`, and `create_project_status_update`.
- **Removed**: `upload_asset` PermissionBuilder — the `safe_outputs` job does not process `upload_asset` items (handled by the dedicated `publish_assets` job with `contents: write`); no permissions are contributed to `safe_outputs` for this handler.
- **Updated**: Publication metadata to 1.27.0.

**Version 1.26.0** (2026-07-16):

- **Added**: First-class fork-backed pull request semantics for `create_pull_request`, including distinct `target-repo` (upstream) and `head-repo` (automation-owned fork) roles.
- **Added**: `head-github-app` configuration for both `create_pull_request` and `push_to_pull_request_branch`, allowing a GitHub App to mint an ephemeral credential scoped to the fork/head repository at runtime. When `head-github-app` is configured, it takes precedence over `head-github-token`.
- **Specified**: Owner-qualified head references, dual-repository allowlist enforcement, least-privilege upstream/head credential roles, and required summary/manifest provenance fields (`upstream_repo`, `head_repo`, `base_sha`, `pushed_head_sha`, `credential_role`).
- **Specified**: `push_to_pull_request_branch` follow-up pushes remain limited to pull requests whose head repository exactly matches the configured `head-repo`; arbitrary contributor forks remain forbidden.
- **Updated**: Publication metadata to 1.26.0.

**Version 1.25.1** (2026-07-16):

- **Specified**: When `safe-outputs.github-app.repositories` is `["*"]`, implementations MUST omit the GitHub App installation-token `repositories` parameter rather than substituting `${{ github.event.repository.name }}`.
- **Clarified**: The wildcard repository behavior applies to activation-job token minting and to subsequent safe-output-job token minting, including `workflow_call` and other reusable-workflow scenarios.
- **Updated**: Publication metadata to 1.25.1.

**Version 1.25.0** (2026-07-15):

- **Added**: Replay authorization requirements in Section 10.6 for Agentic Maintenance `safe_outputs` replays in repositories using custom organization repository roles.
- **Specified**: Replay authorization MUST use exact `admin` or `maintain` standard roles and, for custom roles, the inherited standard-role metadata reported by GitHub rather than the custom role name or the collapsed legacy `permission` bucket.
- **Updated**: Publication metadata to 1.25.0.

**Version 1.24.0** (2026-06-13):

- **Changed**: Default value of the `discussions` field on `add-comment` inverted from `true` to `false`. The `discussions:write` permission is now opt-in. Set `discussions: true` to comment on discussions; omitting the field no longer requests `discussions:write`. The `hide-comment.discussions` default remains `true`.
- **Updated**: Section 7.3 `add_comment` permission documentation, configuration examples, and notes to reflect the opt-in default.
- **Updated**: Publication metadata to 1.24.0.

**Version 1.23.0** (2026-06-10):

- **Added**: `create_check_run` safe output type definition in Section 7.3, including full MCP tool schema, operational semantics, configuration parameters, and permission requirements.
- **Specified**: Dual-permission profile for `create_check_run`: `checks: write` when no `target` is configured; adds `pull-requests: read` when `target` is set (required for PR head SHA resolution via `GET /repos/{owner}/{repo}/pulls/{pull_number}`).
- **Specified**: SHA resolution order: API-fetched PR head SHA (when `target` configured) → event-payload `pull_request.head.sha` → `GITHUB_SHA` → `context.sha`.
- **Specified**: Staged-mode behavior: Pulls API call is skipped; preview message includes the resolved PR number when `target` is set.
- **Updated**: Publication metadata to 1.23.0.

**Version 1.22.0** (2026-06-06):

- **Added**: `base-branch` configuration parameter on `push_to_pull_request_branch`, matching the existing field on `create_pull_request`.
- **Added**: Normative base-branch resolution order for `push_to_pull_request_branch` (explicit config → runtime checkout manifest → `origin/HEAD` → repository default branch).
- **Added**: Hang-safety requirement for handler-issued git operations (`GIT_TERMINAL_PROMPT=0` plus enforced timeout) so credential-less execution contexts fail fast instead of blocking on prompts.
- **Updated**: Publication metadata to 1.22.0.

**Version 1.21.0** (2026-05-19):

- **Added**: `add_comment` status-comment reuse extension semantics in Section 7.1 for `target: "status"` behavior and issue/PR-only restrictions.
- **Added**: Changelog alignment subsection mapping safe-output/reviewer changelog items to this specification revision.
- **Updated**: Publication metadata to 1.21.0.
- **Clarified**: Section 10.5 now distinguishes real threat verdict annotations (`<!-- gh-aw-threat-detected -->`) from threat-engine tooling failures (`<!-- gh-aw-threat-engine-error -->`).

**Version 1.20.0** (2026-05-15):

- **Added**: Section 10.5 warn-mode threat-detection failure policy with mandatory reviewable annotation requirements (`agentic threat detected` caution, label, and XML marker).
- **Added**: Per-type policy matrix (Table WTD-A) annotating every safe output type as Reviewable, Convertible, or Abort during warn-mode threat failures.
- **Added**: Normative conversion fallback from `push_to_pull_request_branch` (`update-pull-request-branch`) to `create_pull_request`.
- **Added**: Compliance tests T-WTD-001 through T-WTD-003 for warn-mode threat-detection failure handling.
- **Updated**: Publication metadata to 1.20.0.

**Version 1.19.0** (2026-04-30):

- **Added**: Auto-injection of `create-issue` when no `safe-outputs:` section is present (or when only system types are configured). The injected config uses `max: 1`, with labels and `title-prefix` set to the workflow ID. Injection is suppressed when any non-builtin safe output is explicitly configured.
- **Updated**: Section 4.3 Configuration Propagation to document the implicit `create-issue` default path.
- **Updated**: Section 7.2 System Types to document `create-issue` conditional auto-injection.
- **Updated**: Publication metadata to 1.19.0

**Version 1.18.0** (2026-04-21):

- **Added**: `comment_memory` safe output type definition in Section 7.3, including file-based synchronization model and required permissions
- **Added**: Phase 8 "Comment Memory Round-Trip" in Section 4.2 defining end-to-end flow across GitHub comment, local files, agent, artifacts, threat detection, and comment upsert
- **Updated**: Publication metadata to 1.18.0

**Version 1.17.0** (2026-04-19):

- **Added**: `merge_pull_request` safe output type definition in Section 7.3, including schema, policy gate semantics, and required permissions
- **Documented**: Merge policy gates for checks, reviews, labels, branch constraints, file constraints, and base-branch restrictions
- **Updated**: Publication metadata to 1.17.0

**Version 1.15.0** (2026-03-29):

- **Added**: Section 11 "Cache Memory Integrity" specifying integrity-aware cache key format, git-backed branching, merge-down semantics, pre-agent setup, and post-agent commit requirements (CI1–CI12)
- **Added**: Threat T6 "Cache Integrity Poisoning" to Section 3.2, describing Bell-LaPadula write-up violations in cache-memory and their architectural mitigations
- **Added**: Terminology entries for *Integrity Level*, *Policy Hash*, *Integrity Branch*, and *Cache Poisoning*
- **Updated**: Table of Contents to include Section 11

**Version 1.14.0** (2026-02-22):

- **Added**: Section 5.5 "Templatable Fields" documenting support for GitHub Actions expressions in integer and boolean configuration fields
- **Updated**: GP1 (`footer` global), TS1 (`max`), and TS2 (`footer` type-specific) syntax to document expression support
- **Clarified**: Templatable integer fields (`max`) and templatable boolean fields (`footer`, `group`, `close-older-issues`, `hide-older-comments`, `close-older-discussions`, `draft`, `allow-empty`, `auto-merge`, `report-as-issue`, `unassign-first`) accept `${{ ... }}` GitHub Actions expressions in addition to literal values
- **Added**: Conformance requirements for runtime evaluation of templatable fields

**Version 1.13.0** (2026-02-18):

- **Added**: Optional `discussions` field for `add-comment` and `hide-comment` safe output types to control `discussions:write` permission
- **Enhanced**: Permission documentation for `add-comment` and `hide-comment` to explain conditional `discussions:write` inclusion
- **Added**: Configuration examples demonstrating `discussions: false` usage for GitHub Apps without Discussions permission
- **Fixed**: Issue where `add-comment` and `hide-comment` unconditionally requested `discussions:write` permission, causing 422 errors for GitHub Apps lacking Discussions permission
- **Default behavior**: `discussions: true` (or omitted) includes `discussions:write` for backward compatibility
- **Opt-out behavior**: `discussions: false` excludes `discussions:write` permission for GitHub Apps without Discussions permission

**Version 1.12.0** (2026-02-16):

- **Implemented**: MCE1 (Early Validation) for add_comment tool with MCP server constraint enforcement
- **Added**: Runtime validation in safe_outputs_handlers.cjs that enforces comment limits during tool invocation
- **Verified**: Dual enforcement pattern now operational - MCP server validates during Phase 4, safe output processor validates during Phase 6
- **Enhanced**: Error responses now use JSON-RPC error code -32602 with actionable messages containing specific constraint details
- **Tested**: Comprehensive test suite (16 test cases) validates E006/E007/E008 error handling and MCP error format compliance

**Version 1.11.0** (2026-02-15):

- **Added**: Section 8.3 "MCP Server Constraint Enforcement" specifying requirements for early validation during tool invocation (MCE1-MCE5)
- **Enhanced**: Tool descriptions to surface operational constraints to the LLM (e.g., add_comment mention/link/length limits)
- **Clarified**: Dual enforcement pattern requiring validation at both MCP server and safe output processor layers
- **Added**: Constraint consistency requirement (MCE5) ensuring limits are identical across tool schemas and enforcement code
- **Added**: Example constraint table for common safe output types with error codes
- **Updated**: add_comment tool description in safe_outputs_tools.json to include explicit constraint documentation

**Version 1.10.0** (2026-02-14):

- **Added**: `reply_to_pull_request_review_comment` safe output type definition (Section 7.3)
- **Updated**: Pattern 8 (Review Comment Workflow) to include reply-to-review-comment in example configuration

**Version 1.9.0** (2026-02-14):

- Added comprehensive validation pipeline ordering (7 stages)
- Added cross-repository security model with explicit allowlist rules
- Added content sanitization pipeline specification (5 stages)
- Added standardized error code catalog (E001-E010)
- Added edge case behavior specifications
- Added terminology section for consistency
- Enhanced security properties (SP6, SP7)
- Improved requirements testability

**Version 1.8.0** (2025-02-14):

- Initial W3C-style specification release
- Complete security model documentation
- Comprehensive safe output type catalog
- Protocol exchange pattern definitions
- Content security mechanisms
- Operational guarantees formalization

**Future Work**:

- Formal conformance test suite
- Extended threat modeling
- Performance benchmarks
- Additional safe output type proposals

---

## Sync Notes

This section maps normative specification requirements (§3–§11) to implementation files, enabling traceability between specification and code.

| Specification Section | Normative Requirements | Implementation File(s) |
|---|---|---|
| §3 Security Architecture | Privilege separation, threat mitigations (SP1–SP7), cross-repo rules | `pkg/workflow/safe_outputs_permissions.go`, `pkg/workflow/compiler_safe_outputs.go` |
| §3.1 Privilege Separation Model | Permission computation, `computePermissionsForSafeOutputs()` | `pkg/workflow/safe_outputs_permissions.go` |
| §3.2 Threat Model and Mitigations | Content sanitization, staged mode, permission scoping | `actions/setup/js/safe_outputs_handlers.cjs`, `actions/setup/js/safe_output_processor.cjs` |
| §4 Structural Components | MCP server configuration, data flow | `pkg/workflow/mcp_renderer_guard.go`, `pkg/workflow/mcp_github_config.go` |
| §4.2 Data Flow Sequence | Safe output processing phases (Phase 1–8) | `actions/setup/js/safe_outputs_handlers.cjs`, `actions/setup/js/safe_output_handler_manager.cjs` |
| §4.3 Configuration Propagation | Compiler-to-runtime config passing | `pkg/workflow/compiler_safe_outputs.go`, `pkg/workflow/compiler_safe_outputs_builder.go` |
| §5 Configuration Semantics | Frontmatter YAML parsing, type-specific config | `pkg/workflow/compiler_safe_outputs.go`, `pkg/workflow/safe_output_handlers.go` |
| §5.2 Global Parameters | `footer`, `staged`, `steer`, global max limits | `actions/setup/js/safe_outputs_config.cjs`, `pkg/workflow/compiler_safe_outputs.go`, `pkg/workflow/compiler_steering_issue.go` |
| §6 Universal Feature Interpretation | Max limit semantics (MR1–MR4), staged mode (SM1–SM4), footer attribution (FA1–FA6) | `actions/setup/js/safe_outputs_handlers.cjs`, `actions/setup/js/safe_outputs_mcp_server.cjs` |
| §7 Safe Output Type Definitions | Handler implementations for each type | `actions/setup/js/safe_outputs_handlers.cjs`, `actions/setup/js/safe_outputs_tools.json` |
| §7.0.3 Declared Field Payload Construction | Normalized payload construction, undeclared field stripping, `x-strip-on-error` advisory field handling | `actions/setup/js/collect_ndjson_output.cjs`, `actions/setup/js/safe_output_type_validator.cjs`, `pkg/workflow/safe_outputs_validation_config.go` |
| §7.1 Core Issue Operations | `create_issue`, `add_comment`, `hide_comment`, `close_issue` | `actions/setup/js/add_comment.cjs`, `actions/setup/js/safe_outputs_handlers.cjs` |
| §7.3 `push_to_pull_request_branch` and `upload_asset` | Trusted patch metadata derivation and upload asset staged-file metadata derivation | `actions/setup/js/generate_git_patch.cjs`, `actions/setup/js/push_to_pull_request_branch.cjs`, `actions/setup/js/upload_assets.cjs` |
| §8 Protocol Exchange Patterns | stdio container transport, tool invocation, MCP server constraint enforcement | `actions/setup/js/safe_outputs_mcp_server.cjs`, `actions/setup/js/safe_outputs_mcp_server_http.cjs` |
| §8.3 MCE1 Early Validation | Invocation-time validation wiring through MCP server startup | `actions/setup/js/safe_outputs_mcp_server.cjs` (`startSafeOutputsServer` → `createHandlers()`), `actions/setup/js/safe_outputs_handlers.cjs` (`addCommentHandler`, `createIssueHandler`, `updatePullRequestHandler`) |
| §8.3 MCE2 Tool Description Disclosure | Tool descriptions/schemas exposed during MCP tool registration | `actions/setup/js/safe_outputs_mcp_server.cjs` (`registerPredefinedTools` call), `actions/setup/js/safe_outputs_tools_loader.cjs` (`registerPredefinedTools`) |
| §8.3 MCE3 Actionable Error Responses | Structured MCP validation errors with remediation guidance | `actions/setup/js/safe_outputs_mcp_server.cjs` (`start()` call serving handler responses), `actions/setup/js/safe_outputs_handlers.cjs` (`buildIntentErrorResponse`, `addCommentHandler`) |
| §8.3 MCE4 Dual Enforcement | Invocation-time max checks before NDJSON append plus processor-time enforcement | `actions/setup/js/safe_outputs_mcp_server.cjs` (`createHandlers()` call path), `actions/setup/js/safe_outputs_handlers.cjs` (`enforcePerTypeMax`, `appendSafeOutputCounted`) |
| §8.3 MCE5 Constraint Configuration Consistency | Shared safe-outputs config passed into handler creation for consistent limits | `actions/setup/js/safe_outputs_mcp_server.cjs` (`bootstrapSafeOutputsServer`, `createHandlers(server, appendSafeOutput, safeOutputsConfig)`), `actions/setup/js/safe_outputs_handlers.cjs` (`getSafeOutputsToolConfig`) |
| §9 Content Integrity Mechanisms | Content sanitization pipeline | `actions/setup/js/safe_output_validator.cjs`, `actions/setup/js/safe_output_type_validator.cjs` |
| §10 Execution Guarantees | Idempotency, staged mode enforcement | `actions/setup/js/safe_outputs_handlers.cjs`, `actions/setup/js/safe_output_action_handler.cjs` |
| §11 Cache Memory Integrity | Cache key format, integrity branches, git-backed branching | `pkg/workflow/compiler_safe_outputs.go`, `actions/setup/js/safe_outputs_bootstrap.cjs` |

Sync procedure:
1. Update this specification when changing safe output handlers, permission computation, or MCP server constraint logic.
2. Update corresponding implementation files in `actions/setup/js/`, `pkg/workflow/`, and `pkg/cli/` in the same change.
3. Re-run safe output tests to verify normative parity: `make test-safe-outputs`.

Sync follow-up tasks:

- Keep §7.0.2 and §8.3 mapping rows current when handler names or MCP wiring changes in `actions/setup/js/safe_outputs_handlers.cjs`, `actions/setup/js/safe_outputs_mcp_server.cjs`, or `actions/setup/js/safe_outputs_tools_loader.cjs`.

---

**End of Specification**

Copyright © 2025 GitHub, Inc. All rights reserved.

This document may be distributed and implemented according to the terms specified in the project license.
