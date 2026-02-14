---
title: Safe Outputs MCP Gateway Specification
description: Formal W3C-style specification defining the Safe Outputs Model Context Protocol Gateway for secure AI-to-GitHub operation translation
sidebar:
  order: 1360
---

# Safe Outputs MCP Gateway Specification

**Version**: 1.8.0  
**Status**: Working Draft  
**Publication Date**: 2025-02-14  
**Editor**: GitHub Agentic Workflows Team  
**This Version**: [safe-outputs-specification](/gh-aw/reference/safe-outputs-specification/)  
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
11. [Appendices](#appendices)

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
6. **MCP integration** defining tool interface schemas and HTTP transport requirements

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

**MCP Gateway**: An HTTP server implementing the Model Context Protocol, accepting tool invocations from agents, validating against JSON schemas, and recording operations to structured files.

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
- Protocol exchange pattern adherence (MCP HTTP transport, NDJSON persistence)
- Content integrity mechanism enforcement (schema validation, domain filtering)
- Execution guarantee provision (atomicity, ordering, idempotency)

**Class C2: Partial Conformance**

An implementation satisfying ALL security-critical normative requirements but omitting support for optional safe output types. Partial conformance requires:

- Complete security architecture (non-negotiable)
- Mandatory safe output types: `create_issue`, `add_comment`, `create_pull_request`, `noop`
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

*Verification*: Inspect agent job permission declarations. Confirm absence of write permissions (issues:write, pull-requests:write, etc.).

**Requirement AR2: Communication Channel Integrity**

Agent-to-processor communication MUST occur through GitHub Actions artifact storage. Environment variables, network connections, or shared filesystems MUST NOT be used for operation transmission.

*Rationale*: Artifact storage provides tamper-evidence, audit logging, and access control. Alternative channels lack these properties.

**Requirement AR3: Permission Minimization**

Each safe output job MUST request minimal permissions. Jobs SHOULD specialize by operation type, requesting only required permissions. For example, an issue-creation job requests `issues:write` but not `pull-requests:write`.

*Example*:
```yaml
jobs:
  safe-output-create-issue:
    permissions:
      contents: read
      issues: write  # Minimal for issue creation
```

**Requirement AR4: No Privilege Escalation Path**

Agent execution context MUST NOT gain access to safe output job credentials through any mechanism (environment variables, file leaks, API endpoints, etc.).

*Verification*: Audit all communication channels between contexts. Confirm no token exposure.

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

### 3.3 Security Property Guarantees

Conforming implementations MUST maintain these security invariants:

**Property SP1: Permission Separation Invariant**

*Statement*: At all times during agent execution, the agent process SHALL NOT possess tokens or credentials permitting GitHub write operations.

*Formal Definition*:
```
∀ t ∈ [agent_start, agent_end]:
  permissions(agent_process, t) ∩ {issues:write, pull-requests:write, ...} = ∅
```

*Verification*: Static analysis of workflow YAML permission declarations. Runtime inspection of environment variables.

**Property SP2: Validation Precedence Invariant**

*Statement*: For all safe output operations, validation logic MUST execute before any GitHub API invocation. Invalid operations MUST be rejected without side effects.

*Formal Definition*:
```
∀ op ∈ operations:
  valid(op) = false ⇒ github_api_call(op) never executes
```

*Verification*: Code review of safe output processor. Unit tests confirming validation rejects before API calls.

**Property SP3: Limit Enforceability Invariant**

*Statement*: For all configured max limits, implementations MUST prevent exceeding the limit. Attempts to exceed limits SHALL result in operation rejection.

*Formal Definition*:
```
∀ type ∈ safe_output_types:
  count(operations[type]) > config[type].max ⇒ reject(operations[type])
```

*Verification*: Integration tests with operations exceeding limits. Confirm rejection occurs.

**Property SP4: Content Integrity Invariant**

*Statement*: All user-provided content MUST undergo sanitization. Sanitization MUST occur after agent output and before GitHub API invocation.

*Formal Definition*:
```
∀ content ∈ user_provided_fields:
  github_api_call(content) ⇒ ∃ sanitized_content = sanitize(content) ∧ passed(sanitized_content)
```

*Verification*: Unit tests for sanitization function. Integration tests confirming unsanitized content never reaches API.

**Property SP5: Provenance Traceability Invariant**

*Statement*: All created GitHub resources MUST include provenance metadata identifying workflow source and run.

*Formal Definition*:
```
∀ resource ∈ created_resources:
  ∃ provenance_data ∈ resource ∧ provenance_data.workflow_run_url ≠ null
```

*Verification*: Inspect created resources. Confirm presence of footer or metadata fields.

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

*Input*: MCP tool invocation requests over HTTP
*Output*: NDJSON file recording operation declarations  
*Key Responsibilities*:
- Start HTTP server on port 3001 (configurable)
- Load tool schemas from `/opt/gh-aw/safeoutputs/config.json`
- Register MCP tools matching enabled safe output types
- Validate invocations against JSON schemas
- Handle large content (>16000 tokens) via file references
- Append validated operations to `/tmp/gh-aw/safeoutputs/output.ndjson`

*Location*: Agent job container (same context as AI agent process)

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
          "labels": {"type": "array", "items": {"type": "string"}}
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

Agent invokes MCP tool:
```http
POST http://127.0.0.1:3001/tools/call
Content-Type: application/json

{
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

### 4.3 Configuration Propagation

Configuration flows from author intent to runtime enforcement:

**Authoring Layer**:
```yaml
# Workflow .md file
safe-outputs:
  create-issue:
    max: 3
    allowed-labels: [bug, enhancement]
```

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

**Syntax**: `footer: true | false`

**Default**: `true`

**Semantics**: Controls whether AI attribution footers are appended to created content (issues, discussions, pull requests, comments).

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
- `<run-url>`: Complete URL to workflow run (https://github.com/{owner}/{repo}/actions/runs/{id})
- `<context>`: Optional triggering context:
  - ` for #123` when triggered by issue #123
  - ` for #456` when triggered by PR #456
  - ` for discussion #789` when triggered by discussion #789
  - Omitted when no specific trigger
- `<source>`: Workflow source path (owner/repo/path@ref, e.g., github/gh-aw/.github/workflows/triage@main)
- `<url>`: Documentation URL (typically https://github.github.com/gh-aw/setup/cli/)

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

### 5.3 Type-Specific Common Parameters

Every safe output type supports these parameters:

#### TS1: max

**Syntax**: `max: <positive-integer> | -1 | null`

**Default**: Type-dependent (see Section 7 for per-type defaults)

**Semantics**: Maximum count of operations permitted for this type in a single workflow run.

**Special Values**:
- Positive integer: Strict limit (e.g., `max: 3` allows up to 3 operations)
- `-1`: Unlimited operations (use with caution)
- `null` or omitted: Use type's default max

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

**Syntax**: `footer: true | false`

**Default**: Inherits from `safe-outputs.footer` (global)

**Semantics**: Override global footer setting for this specific safe output type.

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
  hide-older-comments: true      # Hide previous workflow comments
  target-repo: owner/repo
  allowed-repos: [...]
```

**Pull Request Extensions**:
```yaml
create-pull-request:
  base-branch: main              # Target branch (default: repo default)
  draft: true                    # Create as draft (default: true)
  commit-changes: true           # Auto-commit workspace changes
  reviewers: [user1, copilot]    # Auto-request reviewers
  labels: [automated]            # Auto-apply labels
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
  labels: [report, automated]    # Auto-apply labels
  allowed-labels: [...]          # Agent label restrictions
```

Complete parameter documentation for each type appears in Section 7.

---

(Due to response length constraints, I'll complete the file with remaining sections)


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
- `<context>`: Optional: ` for #<N>` or ` for discussion #<N>`
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
  "description": "Create a new GitHub issue for tracking bugs, feature requests, or tasks.",
  "inputSchema": {
    "type": "object",
    "required": ["title", "body"],
    "properties": {
      "title": {"type": "string", "description": "Issue title"},
      "body": {"type": "string", "description": "Issue description in Markdown"},
      "labels": {"type": "array", "items": {"type": "string"}},
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
5. **Cross-Repository**: When `target-repo` configured, created in that repository (must be in `allowed-repos`).

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
- `contents: read` - Repository metadata and file access
- `issues: write` - Issue creation and modification

*GitHub App* (if using `safe-outputs.app` configuration):
- `issues: write` - Issue creation and modification  
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Both permission modes require the same write scopes
- GitHub App permissions enable cross-repository operations beyond `allowed-repos` when properly configured
- The `contents: read` permission is always included for repository context access

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
  "description": "Add a comment to an existing issue, pull request, or discussion.",
  "inputSchema": {
    "type": "object",
    "required": ["body"],
    "properties": {
      "body": {"type": "string", "description": "Comment text in Markdown"},
      "item_number": {
        "type": "number",
        "description": "Issue/PR/discussion number (auto-resolved from context if omitted)"
      }
    },
    "additionalProperties": false
  }
}
```

**Operational Semantics**:

1. **Context Resolution**: When `item_number` omitted, resolves from workflow trigger context.
2. **Related Items**: When multiple outputs created, adds related items section to comments.
3. **Footer Injection**: Appends footer according to configuration.
4. **Cross-Repository**: Supports `target-repo` configuration.

**Configuration Parameters**:
- `max`: Operation limit (default: 1)
- `target`: Filter by type ("issue", "pull_request", "discussion", "*")
- `hide-older-comments`: Hide previous workflow comments
- `target-repo`: Cross-repository target
- `allowed-repos`: Cross-repo allowlist

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and file access
- `issues: write` - Comment creation on issues
- `pull-requests: write` - Comment creation on pull requests
- `discussions: write` - Comment creation on discussions

*GitHub App* (if using `safe-outputs.app` configuration):
- `issues: write` - Comment creation on issues
- `pull-requests: write` - Comment creation on pull requests
- `discussions: write` - Comment creation on discussions
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Requires write permissions for all three entity types (issues, PRs, discussions) since comments can be added to any type
- Cross-repository commenting requires appropriate permissions in target repository
- The `contents: read` permission is always included for repository context access

---

#### Type: create_pull_request

**Purpose**: Create pull requests to propose code changes.

**Default Max**: 1  
**Cross-Repository Support**: No (same-repository only)  
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

**Configuration Parameters**:
- `max`: Operation limit (default: 1)
- `base-branch`: Target branch
- `draft`: Draft status
- `commit-changes`: Auto-commit workspace
- `reviewers`: Auto-request reviewers
- `labels`: Auto-apply labels
- `title-prefix`: Prepend to titles
- `footer`: Footer override

**Security Requirements**:
- Branch name sanitization (prevent injection)
- Patch content validation
- Size limits on commits

**Required Permissions**:

*GitHub Actions Token*:
- `contents: write` - Branch creation and commit operations
- `pull-requests: write` - Pull request creation

**With `fallback-as-issue: true`** (default):
- `contents: write` - Branch creation and commit operations
- `issues: write` - Issue creation fallback when PR creation fails
- `pull-requests: write` - Pull request creation

*GitHub App* (if using `safe-outputs.app` configuration):
- `contents: write` - Branch creation and commit operations
- `pull-requests: write` - Pull request creation
- `metadata: read` - Repository metadata (automatically granted)

**With `fallback-as-issue: true`** (default):
- `contents: write` - Branch creation and commit operations
- `issues: write` - Issue creation fallback when PR creation fails
- `pull-requests: write` - Pull request creation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Permission requirements vary based on `fallback-as-issue` configuration
- When `fallback-as-issue: true` (default), requires `issues: write` for fallback issue creation if PR creation fails
- When `fallback-as-issue: false`, only requires `contents: write` and `pull-requests: write`
- Cross-repository pull requests are not supported - operations are limited to same repository

---

### 7.2 System Types

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

**Required Permissions**:

*GitHub Actions Token*:
- No additional permissions required beyond base workflow permissions

*GitHub App* (if using `safe-outputs.app` configuration):
- No additional permissions required beyond base app installation

**Notes**:
- The `noop` type performs no GitHub API operations and requires no special permissions
- Only logs completion message to workflow output
- Always available regardless of permission configuration

---

### 7.3 Additional Safe Output Types

This section provides complete definitions for all remaining safe output types. Each follows the same format as Section 7.1 with full schemas, operational semantics, and permission requirements.

#### Type: update_issue

**Purpose**: Modify existing issue properties (title, body, state, labels, assignees, milestone).

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `issues: write` - Issue modification operations

*GitHub App*:
- `issues: write` - Issue modification operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Only specified fields are updated; unspecified fields remain unchanged
- Same permissions as `create_issue`

---

#### Type: close_issue

**Purpose**: Close issues with closing comment explaining resolution.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `issues: write` - Issue state modification and comment creation

*GitHub App*:
- `issues: write` - Issue state modification and comment creation
- `metadata: read` - Repository metadata (automatically granted)

---

#### Type: link_sub_issue

**Purpose**: Create parent-child relationships between issues using task list entries.

**Default Max**: 1  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
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

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
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

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
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

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
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
- `contents: read` - Repository metadata and context
- `pull-requests: write` - Pull request modification operations

*GitHub App*:
- `pull-requests: write` - Pull request modification operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Only specified fields are updated; unspecified fields remain unchanged
- Base branch changes are validated for safety

---

#### Type: close_pull_request

**Purpose**: Close pull requests WITHOUT merging, with closing comment.

**Default Max**: 10  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `pull-requests: write` - Pull request state modification and comment creation

*GitHub App*:
- `pull-requests: write` - Pull request state modification and comment creation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Higher default max (10) enables bulk PR cleanup operations
- Does NOT merge changes - use GitHub's merge functionality for that

---

#### Type: mark_pull_request_as_ready_for_review

**Purpose**: Convert draft pull request to ready-for-review status.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `pull-requests: write` - Pull request draft status modification

*GitHub App*:
- `pull-requests: write` - Pull request draft status modification
- `metadata: read` - Repository metadata (automatically granted)

---

#### Type: push_to_pull_request_branch

**Purpose**: Push commits to pull request branch for automated code changes.

**Default Max**: 1  
**Cross-Repository Support**: No (same repository only)  
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

**Notes**:
- Requires `contents: write` for git push operations
- Enforces maximum patch size limit (default: 1024 KB)
- Validates changes don't exceed size limits before pushing

---

#### Type: create_pull_request_review_comment

**Purpose**: Create review comments on specific lines of code in pull requests.

**Default Max**: 10  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and diff access
- `pull-requests: write` - Review comment creation

*GitHub App*:
- `pull-requests: write` - Review comment creation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Comments buffered via PR review buffer for batch submission
- Higher default max (10) enables comprehensive code review

---

#### Type: submit_pull_request_review

**Purpose**: Submit formal pull request review with status (APPROVE, REQUEST_CHANGES, COMMENT).

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `pull-requests: write` - Review submission operations

*GitHub App*:
- `pull-requests: write` - Review submission operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Submits all buffered review comments from `create_pull_request_review_comment`
- Review status affects PR merge requirements

---

#### Type: resolve_pull_request_review_thread

**Purpose**: Mark pull request review threads as resolved after addressing feedback.

**Default Max**: 10  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `pull-requests: write` - Review thread resolution operations

*GitHub App*:
- `pull-requests: write` - Review thread resolution operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Higher default max (10) enables resolving multiple threads per review cycle

---

#### Type: add_labels

**Purpose**: Add labels to issues or pull requests.

**Default Max**: 3  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
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
- `contents: read` - Repository metadata and context
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
- `contents: read` - Repository metadata and context
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
- `contents: read` - Repository metadata and context
- `issues: write` - Milestone assignment operations

*GitHub App*:
- `issues: write` - Milestone assignment operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Milestone must exist in repository
- Replaces any existing milestone assignment

---

#### Type: assign_to_agent

**Purpose**: Assign GitHub Copilot agents to issues or pull requests.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `issues: write` - Agent assignment operations

*GitHub App*:
- `issues: write` - Agent assignment operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Uses special assignee syntax for Copilot agent assignment
- Agent must be enabled in repository settings

---

#### Type: assign_to_user

**Purpose**: Assign users to issues or pull requests.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `issues: write` - User assignment operations (for issues)
- `pull-requests: write` - User assignment operations (for pull requests)

*GitHub App*:
- `issues: write` - User assignment operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Users must have repository access to be assigned
- Invalid users generate warnings

---

#### Type: unassign_from_user

**Purpose**: Remove user assignments from issues or pull requests.

**Default Max**: 1  
**Cross-Repository Support**: Yes  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
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

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `issues: write` - Comment hiding on issues
- `pull-requests: write` - Comment hiding on pull requests
- `discussions: write` - Comment hiding on discussions

*GitHub App*:
- `issues: write` - Comment hiding on issues
- `pull-requests: write` - Comment hiding on pull requests
- `discussions: write` - Comment hiding on discussions
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Requires all three write permissions to support hiding comments across all entity types
- Comments are minimized, not deleted - reversible by moderators

---

#### Type: create_project

**Purpose**: Create GitHub Projects V2 boards for project management.

**Default Max**: 1  
**Cross-Repository Support**: Yes (organization or user projects)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `organization-projects: write` - Project creation operations (note: only valid for GitHub Apps)

*GitHub App*:
- `organization-projects: write` - Project creation operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- `organization-projects` permission is ONLY available for GitHub App tokens, not standard GitHub Actions tokens
- GitHub Actions workflows should use GitHub App authentication for project operations
- Projects can be created at organization or user level based on app installation

---

#### Type: update_project

**Purpose**: Manage GitHub Projects V2 boards (add items, update fields, remove items).

**Default Max**: 10  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `organization-projects: write` - Project management operations (note: only valid for GitHub Apps)

*GitHub App*:
- `organization-projects: write` - Project management operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Same permission requirements as `create_project`
- Higher default max (10) enables batch project board updates

---

#### Type: create_project_status_update

**Purpose**: Create status updates for GitHub Projects V2 boards.

**Default Max**: 1  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
- `organization-projects: write` - Project status update operations (note: only valid for GitHub Apps)

*GitHub App*:
- `organization-projects: write` - Project status update operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Same permission requirements as `create_project` and `update_project`

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

---

#### Type: dispatch_workflow

**Purpose**: Trigger workflow_dispatch events to invoke other workflows.

**Default Max**: 3  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `actions: write` - Workflow dispatch operations

*GitHub App*:
- `actions: write` - Workflow dispatch operations
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Requires ONLY `actions: write` permission (no `contents: read` needed)
- Target workflow must support `workflow_dispatch` trigger
- Workflow inputs are validated against target workflow's input schema

---

#### Type: create_code_scanning_alert

**Purpose**: Generate SARIF security reports and code scanning alerts.

**Default Max**: unlimited  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
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
- `contents: read` - Repository metadata and context
- `security-events: write` - Alert metadata access
- `actions: read` - Workflow run metadata for alert correlation

*GitHub App*:
- `security-events: write` - Alert metadata access
- `contents: write` - Pull request branch creation
- `pull-requests: write` - Pull request creation
- `actions: read` - Workflow run metadata for alert correlation
- `metadata: read` - Repository metadata (automatically granted)

**Notes**:
- Most complex permission set - requires security-events, contents, pull-requests, and actions scopes
- Creates pull request with proposed fix referencing the alert
- Alert must exist and be fixable

---

#### Type: create_agent_session

**Purpose**: Create GitHub Copilot agent sessions for code change delegation.

**Default Max**: 1  
**Cross-Repository Support**: No (same repository only)  
**Mandatory**: No

**Required Permissions**:

*GitHub Actions Token*:
- `contents: read` - Repository metadata and context
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

## 8. Protocol Exchange Patterns

### 8.1 HTTP Transport Layer

**Protocol**: HTTP/1.1  
**Bind Address**: 127.0.0.1 (localhost only)  
**Port**: From `GH_AW_SAFE_OUTPUTS_PORT` (default: 3001)  
**Operation Mode**: Stateless (no session management)

**Endpoints**:
- `POST /tools/list` - List available tools
- `POST /tools/call` - Invoke tool

### 8.2 Tool Invocation Protocol

**Request Format**:
```http
POST /tools/call HTTP/1.1
Host: 127.0.0.1:3001
Content-Type: application/json

{
  "method": "tools/call",
  "params": {
    "name": "<tool_name>",
    "arguments": {<parameters>}
  }
}
```

**Success Response**:
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "result": {
    "content": [{
      "type": "text",
      "text": "{\"result\":\"success\"}"
    }]
  }
}
```

**Validation Error**:
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "error": {
    "code": -32602,
    "message": "Invalid params: <details>"
  }
}
```

### 8.3 NDJSON Persistence

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

---

## 10. Execution Guarantees

### 10.1 Atomicity

**Single-Item Operations**: Complete success or complete failure (no partial state).

**Batch Operations**: Best-effort semantics; partial success reported.

### 10.2 Ordering

Operations execute in:
1. NDJSON file order
2. Type grouping (same type together)
3. System types last (noop)

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

---

## Appendix A: Conformance Checklist

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
  - [ ] Mandatory: create_issue, add_comment, create_pull_request, noop
  - [ ] Optional types documented if unsupported

- [ ] Protocol
  - [ ] HTTP transport
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

- **GitHub REST API**: https://docs.github.com/rest
- **GitHub Actions**: https://docs.github.com/actions
- **MCP Gateway Specification**: /gh-aw/reference/mcp-gateway/

---

## Appendix F: Document History

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

**End of Specification**

Copyright © 2025 GitHub, Inc. All rights reserved.

This document may be distributed and implemented according to the terms specified in the project license.
