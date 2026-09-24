---
title: GitHub Agentic Workflows Security Architecture Specification
description: Formal W3C-style specification for the security architecture and guarantees of GitHub Agentic Workflows
sidebar:
  order: 1000
---

# GitHub Agentic Workflows Security Architecture Specification

**Version**: 1.0.1  
**Status**: Candidate Recommendation  
**Latest Version**: https://github.com/github/gh-aw/blob/main/specs/security-architecture-spec.md  
**Editors**: GitHub Next (GitHub, Inc.)

---

## Abstract

This specification defines the security architecture, guarantees, and implementation requirements for GitHub Agentic Workflows (gh-aw), a system for compiling natural language workflow definitions into secure, sandboxed GitHub Actions workflows. The specification establishes formal conformance requirements for implementations seeking to replicate this security model in other CI/CD environments.

The security architecture employs defense-in-depth principles including input sanitization, output isolation, network segmentation, permission minimization, and threat detection. All security controls are implemented at compilation time and enforced at runtime through generated GitHub Actions workflows.

## Status of This Document

This is a Candidate Recommendation specification and represents the current state of the GitHub Agentic Workflows security architecture as implemented in version 1.0.0. This specification is subject to updates based on security research, community feedback, and operational experience. Future versions may introduce additional security controls or refine existing requirements.

**Publication Date**: January 29, 2026  
**Governance**: This specification is maintained by GitHub Next and governed by GitHub's security and research processes.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Security Architecture Overview](#3-security-architecture-overview)
4. [Input Sanitization Layer](#4-input-sanitization-layer)
5. [Output Isolation Layer](#5-output-isolation-layer)
6. [Network Isolation Layer](#6-network-isolation-layer)
7. [Permission Management Layer](#7-permission-management-layer)
8. [Sandbox Isolation Layer](#8-sandbox-isolation-layer)
9. [Threat Detection Layer](#9-threat-detection-layer)
10. [Compilation-Time Security Checks](#10-compilation-time-security-checks)
11. [Runtime Security Enforcement](#11-runtime-security-enforcement)
12. [Compliance Testing](#12-compliance-testing)
13. [Appendices](#appendices)
14. [References](#references)
15. [Change Log](#change-log)

---

## 1. Introduction

### 1.1 Purpose

This specification formalizes the security architecture implemented in GitHub Agentic Workflows, enabling:

1. **Replication**: Organizations can implement equivalent security controls in other CI/CD platforms
2. **Verification**: Security teams can audit implementations against formal requirements
3. **Compliance**: Systems can demonstrate conformance to defined security standards
4. **Evolution**: The specification provides a foundation for future security enhancements

### 1.2 Scope

This specification covers:

- Multi-layered security architecture and defense-in-depth strategies
- Input sanitization mechanisms for untrusted content
- Output isolation patterns separating read from write operations
- Network segmentation and domain allowlisting
- Permission management and least-privilege enforcement
- Sandbox isolation for agent processes and MCP servers
- Threat detection and validation procedures
- Compilation-time security validation
- Runtime security enforcement mechanisms

This specification does NOT cover:

- AI model security or prompt engineering best practices
- Application-level security for workflows (responsibility of workflow authors)
- GitHub Actions platform security (covered by GitHub documentation)
- Operational security procedures for managing workflows
- Incident response or security monitoring procedures

### 1.3 Design Goals

The security architecture is designed to achieve:

1. **Defense in Depth**: Multiple independent security layers
2. **Least Privilege**: Minimal permissions required for each operation
3. **Explicit Over Implicit**: Security controls are declarative and reviewable
4. **Fail Secure**: Security failures prevent execution rather than allowing degraded operation
5. **Auditability**: All security-relevant actions produce visible artifacts
6. **Composability**: Security controls can be combined and extended
7. **Transparency**: Generated workflows are human-readable and reviewable

---

## 2. Conformance

### 2.1 Conformance Classes

This specification defines three conformance classes:

#### 2.1.1 Basic Conformance

A **basic conforming implementation** MUST implement:

- Input sanitization layer (Section 4)
- Output isolation layer (Section 5)
- Permission management layer (Section 7)
- Compilation-time security checks (Section 10)

Basic conformance provides core security protections suitable for low-risk workflows.

#### 2.1.2 Standard Conformance

A **standard conforming implementation** MUST implement all basic conformance requirements plus:

- Network isolation layer (Section 6)
- Sandbox isolation layer (Section 8)
- Runtime security enforcement (Section 11)

Standard conformance is RECOMMENDED for production workflows handling sensitive data.

#### 2.1.3 Complete Conformance

A **complete conforming implementation** MUST implement all standard conformance requirements plus:

- Threat detection layer (Section 9)
- All optional security enhancements marked as RECOMMENDED

Complete conformance provides maximum security for high-risk workflows.

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.3 Compliance Levels

Implementations are evaluated at three compliance levels:

| Level | Description | Conformance Class |
|-------|-------------|-------------------|
| **Level 1** | Core security controls | Basic Conformance |
| **Level 2** | Production-ready security | Standard Conformance |
| **Level 3** | Maximum security hardening | Complete Conformance |

---

## 3. Security Architecture Overview

### 3.1 Multi-Layered Architecture

The security architecture employs seven independent security layers that provide defense-in-depth:

```text
┌─────────────────────────────────────────────────────┐
│              Workflow Definition (.md)               │
└─────────────────┬───────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────┐
│      Layer 0: Compilation-Time Validation            │
│  - Schema validation                                 │
│  - Expression safety checks                          │
│  - Permission validation                             │
│  - Network configuration validation                  │
└─────────────────┬───────────────────────────────────┘
                  │ Generates
                  ▼
┌─────────────────────────────────────────────────────┐
│         Compiled Workflow (.lock.yml)                │
└─────────────────┬───────────────────────────────────┘
                  │ Executed by GitHub Actions
                  ▼
        ┌─────────────────────┐
        │  Runtime Execution   │
        └─────────┬───────────┘
                  │
    ┌─────────────┼─────────────┐
    │             │             │
    ▼             ▼             ▼
┌────────┐  ┌─────────┐  ┌──────────┐
│Layer 1 │  │Layer 2  │  │Layer 3   │
│Input   │  │Output   │  │Network   │
│Sanit.  │  │Isolat.  │  │Isolat.   │
└────────┘  └─────────┘  └──────────┘
    │             │             │
    ▼             ▼             ▼
┌────────┐  ┌─────────┐  ┌──────────┐
│Layer 4 │  │Layer 5  │  │Layer 6   │
│Permiss.│  │Sandbox  │  │Threat    │
│Mgmt.   │  │Isolat.  │  │Detect.   │
└────────┘  └─────────┘  └──────────┘
```

### 3.2 Security Guarantees

A conforming implementation MUST provide the following security guarantees:

**SG-01**: Untrusted input SHALL NOT be directly interpolated into GitHub Actions expressions without sanitization.

> **Note**: This guarantee specifically protects against template injection in GitHub Actions expressions (e.g., `${{ github.event.issue.title }}`). It does not prevent AI agents from accessing untrusted data at runtime through MCP tools (e.g., GitHub MCP returning issue titles or PR bodies). Runtime access to untrusted data via tools is subject to prompt injection risks, which are mitigated through:
> - Threat detection layer (Section 9) analyzing agent behavior
> - Output isolation layer (Section 5) preventing direct write access
> - Network isolation layer (Section 6) restricting data exfiltration
> - Safe outputs validation (Section 5.4) checking output integrity

**SG-02**: AI agents SHALL NOT have direct write access to repository resources (issues, pull requests, discussions).

**SG-03**: Network access SHALL be restricted to explicitly allowed domains unless explicitly configured otherwise.

**SG-04**: Permissions SHALL follow the principle of least privilege with read-only defaults.

**SG-05**: AI agent processes SHALL execute in isolated sandbox environments.

**SG-06**: All security-relevant actions SHALL produce auditable artifacts (workflow logs, comments, pull requests).

**SG-07**: Security violations SHALL prevent workflow execution rather than allowing degraded operation.

### 3.3 Threat Model

The security architecture protects against:

1. **Template Injection**: Malicious input manipulating GitHub Actions expressions
2. **Prompt Injection**: Adversarial inputs manipulating AI agent behavior
   - **Via GitHub event context**: Mitigated through input sanitization layer (Section 4)
   - **Via MCP tool responses**: Mitigated through threat detection (Section 9), output isolation (Section 5), and network filtering (Section 6)
3. **Data Exfiltration**: Unauthorized data transmission to external services
4. **Privilege Escalation**: Unauthorized elevation of permissions or capabilities
5. **Supply Chain Attacks**: Compromised dependencies or actions
6. **Resource Exhaustion**: Denial of service through infinite loops or resource consumption

**Threat Model Clarification**: While the input sanitization layer prevents template injection through GitHub event context, AI agents may still access untrusted data at runtime through MCP tools. For example, the GitHub MCP server can return issue titles, PR bodies, and comments that were not pre-sanitized by the activation job. This is an accepted architectural tradeoff that enables dynamic workflows while relying on:
- **Threat detection**: Analyzes agent output for malicious behavior before execution
- **Output isolation**: Prevents agents from directly writing to repository resources
- **Network isolation**: Restricts data exfiltration to allowed domains only
- **Safe outputs validation**: Validates all output before execution

### 3.4 Security Principles

**SP-01 (Defense in Depth)**: Multiple independent security layers SHALL provide overlapping protections.

**SP-02 (Least Privilege)**: Components SHALL operate with the minimum permissions required.

**SP-03 (Fail Secure)**: Security failures SHALL prevent execution rather than degrading protections.

**SP-04 (Explicit Configuration)**: Security controls SHALL be declared explicitly in workflow definitions.

**SP-05 (Auditability)**: Security-relevant actions SHALL produce visible, persistent artifacts.

**SP-06 (Separation of Concerns)**: Read operations SHALL be separated from write operations.

---

## 4. Input Sanitization Layer

### 4.1 Overview

The input sanitization layer protects against template injection and prompt injection by processing all untrusted input through a sanitization pipeline before delivery to AI agents.

**Key Sanitization Features**:
- **Markdown Safety**: Neutralizes @mentions, bot command patterns, and GitHub references
- **URL Filtering**: Validates and redacts URLs against network allowlists, enforcing HTTPS-only policies
- **HTML/XML Tag Filtering**: Converts tags to safe HTML entities to prevent injection attacks
- **ANSI Escape Code Removal**: Strips terminal color codes and control characters
- **Content Limits**: Enforces size and line count restrictions with truncation
- **Protocol Sanitization**: Removes unsafe URL protocols (javascript:, data:, file:)

### 4.2 Sanitization Requirements

**IS-01**: A conforming implementation MUST sanitize all content from untrusted sources including:
- Issue titles and bodies
- Pull request titles and bodies
- Issue comments and PR review comments
- Commit messages
- User-provided input fields

**IS-02**: Sanitized content MUST be made available through a dedicated output variable (e.g., `steps.sanitized.outputs.text`).

**IS-03**: Workflows MUST NOT use raw GitHub event context (e.g., `github.event.issue.title`) directly in AI agent prompts.

### 4.3 Sanitization Procedures

#### 4.3.1 Mention Neutralization

**IS-04**: The implementation MUST neutralize @mentions by wrapping them in backticks:
- Input: `@username`
- Output: `` `@username` ``

This prevents unintended notifications and bot trigger abuse.

#### 4.3.2 Bot Trigger Protection

**IS-05**: The implementation MUST neutralize bot command patterns:
- Patterns like `fixes #123`, `/command`, `closes #456`
- MUST be wrapped in backticks: `` `fixes #123` ``

#### 4.3.3 HTML/XML Tag Filtering

**IS-06**: The implementation MUST convert HTML/XML tags to safe HTML entities:
- Open tags: `<tag>` → `&lt;tag&gt;`
- Close tags: `</tag>` → `&lt;/tag&gt;`
- Self-closing tags: `<tag/>` → `&lt;tag/&gt;`
- Attribute tags: `<tag attr="value">` → `&lt;tag attr="value"&gt;`

**IS-06a**: HTML entity conversion prevents:
- XSS (Cross-Site Scripting) attacks via embedded JavaScript
- HTML injection that could manipulate rendered content
- XML External Entity (XXE) attacks in parsers
- Malformed markup that could break sanitization

**IS-06b**: XML comment removal MUST:
- Remove `<!-- comment -->` style comments
- Prevent comment-based obfuscation of malicious content
- Execute before tag conversion to ensure complete sanitization

#### 4.3.4 URL Filtering and Validation

**IS-07**: The implementation MUST validate and filter URLs:
- Only HTTPS URLs SHOULD be allowed (HTTP URLs MAY be permitted for specific allowlisted domains)
- URLs MUST match the network allowlist configuration
- Non-compliant URLs MUST be replaced with `(redacted)`
- Unsafe URL protocols MUST be removed: `javascript:`, `data:`, `file:`, `vbscript:`

**IS-07a**: URL domain validation MUST:
- Extract hostname from URLs
- Compare against allowed domains from network configuration
- Support wildcard matching (e.g., `*.github.com` matches `api.github.com`)
- Log redacted domains for security audit

**IS-07b**: Protocol sanitization MUST:
- Strip `javascript:` pseudo-protocol to prevent XSS
- Strip `data:` protocol to prevent data exfiltration
- Strip `file:` protocol to prevent local file access
- Preserve `https:` and allowlisted `http:` protocols only

#### 4.3.5 Content Limits

**IS-08**: The implementation MUST enforce content size limits:
- Maximum total size: 0.5 MB (524,288 bytes)
- Maximum line count: 65,536 lines
- Content exceeding limits MUST be truncated with a truncation notice

#### 4.3.6 Control Character Removal

**IS-09**: The implementation MUST remove control characters:
- ASCII control characters (0-31) except newline (10), tab (9), and carriage return (13)
- ANSI escape sequences used for terminal colors and formatting (e.g., `\x1b[31m`, `\x1b[0m`)
- Delete character (ASCII 127)

**IS-09a**: ANSI escape code removal prevents:
- Terminal injection attacks
- Obfuscation of malicious content via hidden characters
- Breaking of text processing pipelines
- Confusion in logs and audit trails

### 4.4 Sanitization Pipeline

**IS-10**: The sanitization pipeline MUST execute in the following order:

1. Extract raw content from GitHub event context
2. Remove ANSI escape sequences and control characters (IS-09)
3. Apply mention neutralization (IS-04)
4. Apply bot trigger protection (IS-05)
5. Remove XML comments
6. Convert HTML/XML tags to entities (IS-06)
7. Sanitize URL protocols (remove unsafe protocols)
8. Validate and filter URLs against allowlist (IS-07)
9. Apply content size limits and truncation (IS-08)
10. Store result in sanitized output variable

### 4.5 Bypass Prevention

**IS-11**: The implementation MUST prevent sanitization bypass:
- Workflows SHALL NOT access raw event context after sanitization is available
- The compiler MUST validate that AI prompts use sanitized variables
- Workflows using raw context MUST fail compilation with a clear error

---

## 5. Output Isolation Layer

### 5.1 Overview

The output isolation layer enforces separation between AI agent operations (read-only) and GitHub API operations (write access). This prevents AI agents from directly modifying repository resources while enabling validated automation.

### 5.2 Job Architecture

**OI-01**: A conforming implementation MUST separate workflow execution into distinct job types:

1. **Activation Job**: Performs sanitization and produces `steps.sanitized.outputs.text`
2. **Agent Job**: Executes AI agent with read-only permissions
3. **Safe Output Jobs**: Perform validated GitHub API operations with write permissions

**OI-02**: Agent jobs MUST NOT have write permissions to repository resources:
- `contents: read` (NOT `contents: write`)
- `issues: read` (NOT `issues: write`)
- `pull-requests: read` (NOT `pull-requests: write`)

### 5.3 Safe Output Configuration

**OI-03**: Write operations MUST be declared explicitly in workflow frontmatter using the `safe-outputs` configuration block.

**OI-04**: The implementation MUST support the following safe output types:
- `create-issue`: Create new issues
- `add-comment`: Add comments to issues/PRs
- `create-pull-request`: Create pull requests
- `create-discussion`: Create repository discussions
- `close-issue`: Close issues
- `close-pull-request`: Close pull requests
- `close-discussion`: Close discussions
- `mark-pull-request-as-ready-for-review`: Mark draft PRs as ready

**OI-05**: The implementation MAY support additional safe output types as extensions.

### 5.4 Output Validation

**OI-06**: Safe output jobs MUST validate agent output before execution:
- Output format MUST conform to expected JSON schema
- Required fields MUST be present
- Field values MUST pass type validation
- String lengths MUST be within configured limits

**OI-07**: Invalid output MUST cause workflow failure with a descriptive error message.

### 5.5 Token Management

**OI-08**: Safe output jobs MUST support configurable GitHub tokens:
- Individual safe output token: `safe-outputs.<type>.github-token`
- Global safe outputs token: `safe-outputs.github-token`
- Workflow-level token: `github-token`
- Default token: `${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}`

**OI-09**: Token resolution MUST follow precedence order (highest to lowest):
1. Individual safe output token
2. Global safe outputs token
3. Workflow-level token
4. Default token

**OI-10**: Tokens MUST be GitHub Actions expressions referencing secrets or job outputs (e.g., `${{ secrets.TOKEN_NAME }}` or `${{ needs.auth.outputs.token }}`). Plaintext tokens MUST cause compilation failure.

### 5.6 Output Isolation Guarantees

**OI-11**: The implementation MUST guarantee:
- AI agents cannot directly execute GitHub API write operations
- All write operations are validated before execution
- Write operations execute in separate jobs with isolated permissions
- Failed validations prevent write operations from executing

---

## 6. Network Isolation Layer

### 6.1 Overview

The network isolation layer restricts network access for AI agents and MCP servers using domain allowlists and ecosystem identifiers.

### 6.2 Network Configuration

**NI-01**: A conforming implementation MUST support network access control through a `network` configuration field.

**NI-02**: The implementation MUST support three network access modes:

1. **Default Allowlist** (`network: defaults`): Basic infrastructure only
2. **Custom Allowlist** (`network: { allowed: [...] }`): Explicit domain list
3. **No Access** (`network: {}`): All network access denied

**NI-03**: If `network` is not specified, the implementation SHOULD default to `network: defaults`.

### 6.3 Ecosystem Identifiers

**NI-04**: The implementation MUST support ecosystem identifiers that expand to domain lists:

| Identifier | Purpose |
|------------|---------|
| `defaults` | Basic infrastructure (certificates, JSON schema, Ubuntu packages) |
| `github` | GitHub domains (github.com, githubusercontent.com, etc.) |
| `containers` | Container registries (Docker Hub, GHCR, Quay) |
| `python` | Python package ecosystem (PyPI, anaconda) |
| `node` | Node.js package ecosystem (npm, yarn) |
| `go` | Go package ecosystem |
| `java` | Java package ecosystem (Maven Central) |
| `dotnet` | .NET package ecosystem (NuGet) |
| `ruby` | Ruby package ecosystem (RubyGems) |
| `rust` | Rust package ecosystem (crates.io) |

**NI-05**: The implementation MAY support additional ecosystem identifiers as extensions.

### 6.4 Domain Allowlisting

**NI-06**: Individual domains in the `allowed` list MUST support:
- Exact domain matching (e.g., `api.example.com`)
- Automatic subdomain matching (e.g., `example.com` allows `api.example.com`)
- Wildcard patterns (e.g., `*.cdn.example.com`)

**NI-07**: The implementation SHOULD support protocol-specific filtering:
- `https://domain.com`: HTTPS-only access
- `http://domain.com`: HTTP-only access
- `domain.com`: Both HTTP and HTTPS

**NI-08**: The implementation MUST reject invalid protocols with compilation errors:
- Invalid: `ftp://`, `ws://`, `wss://`, etc.
- Error message MUST indicate valid protocols: `http://` and `https://`

### 6.5 Domain Blocking

**NI-09**: The implementation MUST support a `blocked` field for denying specific domains:

```yaml
network:
  allowed:
    - defaults
    - python
  blocked:
    - "tracker.example.com"
```

**NI-10**: Blocked domains MUST take precedence over allowed domains.

**NI-11**: The blocked list MUST support both individual domains and ecosystem identifiers.

### 6.6 Enforcement Mechanisms

**NI-12**: For AI engines, the implementation MUST enforce network restrictions using one of:
- Process-level firewall (e.g., AWF for Copilot)
- Container-level network policies
- Proxy-based filtering
- Engine-native network controls (e.g., Claude)

**NI-13**: For MCP servers in containers, the implementation MUST enforce network restrictions using:
- Container-level egress filtering (iptables, network policies)
- Per-container proxy configuration (e.g., Squid)
- MCP server network isolation

### 6.7 Content Sanitization Integration

**NI-14**: The network allowlist MUST also apply to content sanitization:
- URIs from non-allowed domains MUST be replaced with `(redacted)`
- GitHub domains SHOULD be allowed by default

---

## 7. Permission Management Layer

### 7.1 Overview

The permission management layer enforces least-privilege access control for workflow jobs using GitHub Actions permissions.

### 7.2 Permission Defaults

**PM-01**: A conforming implementation MUST set read-only permissions as the default:

```yaml
permissions:
  contents: read
  actions: read
```

**PM-02**: Unspecified permissions MUST default to `none` (GitHub Actions default behavior).

### 7.3 Permission Configuration

**PM-03**: The implementation MUST support configuring permissions in workflow frontmatter:

```yaml
permissions:
  contents: read
  issues: read
  pull-requests: read
```

**PM-04**: The implementation MUST support both permission shorthand and detailed configuration:

**Shorthand**: Top-level `permissions` field

**Detailed**: Job-level `permissions` within `jobs` configuration

### 7.4 Strict Mode

**PM-05**: The implementation MUST support a `strict` mode that enforces security restrictions:

**PM-06**: When `strict: true` is enabled, the implementation MUST:
- Block write permissions (`contents: write`, `issues: write`, `pull-requests: write`)
- Require use of safe outputs for write operations
- Apply secure network defaults unless explicitly configured
- Refuse wildcard (`*`) in network domains
- Require network configuration for custom MCP containers
- Enforce action pinning to commit SHAs
- Refuse deprecated frontmatter fields

**PM-07**: Violations of strict mode requirements MUST cause compilation failure with descriptive error messages.

### 7.5 Fork Protection

**PM-08**: For `pull_request` triggers, the implementation MUST:
- Block forks by default
- Generate repository ID comparison: `github.event.pull_request.head.repo.id == github.repository_id`
- Support explicit fork allowlisting via `forks` configuration

**PM-09**: For `workflow_run` triggers, the implementation MUST inject safety conditions:

```yaml
if: >
  (github.event.workflow_run.repository.id == github.repository_id) &&
  (!github.event.workflow_run.repository.fork)
```

### 7.6 Role-Based Access Control

**PM-10**: The implementation MUST support role-based execution restrictions:

```yaml
roles: [admin, maintainer, write]  # Default
roles: [admin, maintainer]         # Stricter
roles: all                         # Least restrictive
```

**PM-11**: Role checks MUST be performed at runtime using membership validation.

The separate `pre_activation` job defined in Section 7.6.1 is the normative runtime mechanism that satisfies **PM-11**. Role validation MUST complete in `pre_activation` before the `activation` job begins. Implementations MUST NOT replace this gate with a later best-effort check in `activation`, `agent`, or `safe_outputs`.

**PM-12**: Failed role checks MUST cancel workflow execution with a warning message.

#### 7.6.1 Pre-Activation Pattern

Role-based access control is implemented via a dedicated `pre_activation` job that runs before the activation job. This ensures that only authorized users can trigger the full workflow execution chain.

**PM-10a**: A conforming implementation MUST implement role checks in a separate `pre_activation` job that precedes the `activation` job:

```yaml
jobs:
  pre_activation:
    runs-on: ubuntu-slim
    permissions:
      contents: read
    outputs:
      activated: ${{ steps.check_membership.outputs.is_team_member == 'true' }}
    steps:
      - name: Check team membership for workflow
        id: check_membership
        uses: actions/github-script@<SHA>  # Pin to a specific commit SHA in production
        env:
          GH_AW_REQUIRED_ROLES: admin,maintainer,write
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { main } = require('/opt/gh-aw/actions/check_membership.cjs');
            await main();

  activation:
    needs: pre_activation
    if: needs.pre_activation.outputs.activated == 'true'
    # ... remainder of activation job
```

**PM-10b**: The `pre_activation` job MUST output an `activated` boolean that the `activation` job uses as an execution gate via its `if:` condition.

**PM-10c**: The `pre_activation` job MUST run with read-only permissions (`contents: read`). It MUST NOT have write permissions of any kind.

**PM-10d**: The `GH_AW_REQUIRED_ROLES` environment variable MUST be set from the workflow's `roles` frontmatter field. The default value MUST be `admin,maintainer,write` when no `roles` field is specified.

This pattern separates the membership check from the input sanitization that occurs in the `activation` job, enabling independent auditing of each security gate.

### 7.7 Token Validation

**PM-13**: The implementation MUST validate that `github-token` fields contain GitHub Actions expressions referencing secrets or job outputs.

**PM-14**: Plaintext tokens or environment variables MUST cause compilation failure.

**PM-15**: Valid token formats:
- `${{ secrets.TOKEN_NAME }}`
- `${{ secrets.ORG_TOKEN || secrets.FALLBACK_TOKEN }}`
- `${{ needs.auth.outputs.token }}`

---

## 8. Sandbox Isolation Layer

### 8.1 Overview

The sandbox isolation layer provides process-level and container-level isolation for AI agents and MCP servers.

### 8.2 Coding Agent Sandbox

**SI-01**: A conforming implementation MUST execute AI agents in isolated environments.

**SI-02**: The implementation MUST support agent sandbox types:
- **AWF (Agent Workflow Firewall)**: Container-based with network firewall (default)
- **Sandbox Runtime (SRT)**: Alternative container runtime (experimental)

**SI-03**: If no sandbox type is specified, the implementation MUST default to AWF.

### 8.3 AWF Sandbox Requirements

**SI-04**: The AWF sandbox MUST provide:
- Container-based process isolation
- Network egress control via iptables and domain-based allowlisting
- Chroot-based filesystem transparency (all host binaries accessible, no explicit mounts)
- Hidden Docker socket for security
- Automatic environment variable inheritance via `--env-all`
- Capability drop post-setup (CAP_NET_ADMIN, CAP_SYS_CHROOT)

**SI-05**: AWF chroot mode MUST enforce this filesystem access model:

| Path Type | Mode | Examples |
|-----------|------|----------|
| User paths | Read-write | `$HOME`, `$GITHUB_WORKSPACE`, `/tmp` |
| System paths | Read-only | `/usr`, `/opt`, `/bin`, `/lib` |
| Docker socket | Hidden | `/var/run/docker.sock` |

### 8.4 MCP Server Sandbox

**SI-08**: The implementation MUST execute MCP servers in isolated containers.

**SI-09**: MCP server containers MUST:
- Run with non-root UIDs
- Have dropped capabilities
- Use seccomp/AppArmor profiles
- Deny privilege escalation
- Have egress network filtering

**SI-10**: MCP server containers SHOULD:
- Be pinned to digests (e.g., `image@sha256:...`)
- Be scanned for vulnerabilities
- Have SBOMs tracked

### 8.5 Environment Variable Inheritance

**SI-06**: AWF MUST pass all environment variables via `--env-all` and implement PATH inheritance:
- Host `PATH` captured as `AWF_HOST_PATH` and restored inside container
- `GOROOT` explicitly captured after `actions/setup-go` (Go's trimmed binaries require it)
- Undefined variables silently ignored

### 8.6 Sandbox Guarantees

**SI-07**: The sandbox isolation layer MUST guarantee:
- Read-only system paths, read-write user paths only (`$HOME`, `$GITHUB_WORKSPACE`, `/tmp`)
- No Docker socket access
- Network access only to allowed domains (iptables enforcement independent of chroot)
- MCP servers in isolated containers with independent network allowlists
- Defense in depth: filesystem visibility (chroot) and network isolation (iptables) remain separate layers

---

## 9. Threat Detection Layer

### 9.1 Overview

The threat detection layer analyzes AI agent output for security threats before safe output jobs execute.

> Note: Repository-specific GitHub-tool access-control policies such as `trusted-users` are complementary runtime controls, but they are specified in the companion GitHub MCP access-control documents (`scratchpad/github-mcp-access-control-specification.md` and `scratchpad/guard-policies-specification.md`), not in the threat-detection requirements below.

### 9.2 Threat Detection Requirements

**TD-01**: A conforming implementation with complete conformance MUST provide automated threat detection.

**TD-02**: Threat detection MUST be automatically enabled when `safe-outputs` is configured.

**TD-03**: The implementation MUST support disabling threat detection via `threat-detection: false`.

### 9.3 Detection Categories

**TD-04**: The implementation MUST detect the following threat categories:

1. **Prompt Injection**: Malicious instructions manipulating AI behavior
2. **Secret Leaks**: Exposed API keys, tokens, passwords, credentials
3. **Malicious Patches**: Code changes introducing vulnerabilities or backdoors

**TD-05**: The implementation MAY support additional threat categories as extensions.

### 9.4 Detection Methods

**TD-06**: The implementation MUST support AI-powered threat detection using configured AI engines.

**TD-07**: The implementation SHOULD support custom detection steps for specialized scanning:

```yaml
threat-detection:
  enabled: true
  steps:
    - name: Run TruffleHog
      uses: trufflesecurity/trufflehog@main
```

### 9.5 Detection Output

**TD-08**: Threat detection MUST produce structured JSON output:

```json
{
  "prompt_injection": false,
  "secret_leak": false,
  "malicious_patch": false,
  "reasons": []
}
```

**TD-09**: If any threat is detected (`true`), the workflow MUST fail and safe outputs MUST NOT execute.

**TD-10**: The `reasons` array SHOULD contain human-readable explanations for detected threats.

### 9.6 Custom Prompts

**TD-11**: The implementation MUST support custom detection prompts:

```yaml
safe-outputs:
  threat-detection:
    prompt: "Focus on SQL injection vulnerabilities"
```

**TD-12**: Custom prompts MUST be appended to default detection instructions, not replace them.

Custom prompt text is the `prompt` field under `safe-outputs.threat-detection`. The compiler parses it in `pkg/workflow/threat_detection_config.go` and passes it to the detection job as additional instructions after the baseline threat-detection prompt, so custom prompts can narrow focus but cannot remove the default detection categories.

### 9.7 Engine Configuration

**TD-13**: The implementation MUST support overriding the AI engine for threat detection:

```yaml
safe-outputs:
  threat-detection:
    engine: "copilot"  # String format
```

**TD-14**: The implementation MUST support full engine configuration objects:

```yaml
safe-outputs:
  threat-detection:
    engine:
      id: copilot
      model: gpt-4
      harness:
        max-retries: 1
```

**TD-15**: The implementation MUST support disabling AI-powered detection:

```yaml
safe-outputs:
  threat-detection:
    engine: false
    steps:  # Custom steps only
      - name: Static Analysis
        run: ./scan.sh
```

The current parser accepts the following `safe-outputs.threat-detection` option set: `enabled` (boolean or expression; `false` disables the detection job), `prompt`, `steps`, `post-steps`, `engine-timeout`, `max-turns`, `retries`, `max-ai-credits`, `runs-on`, `continue-on-error` (boolean or expression; default `true`), and `engine`. The `engine` field may be a string engine ID, `false` to skip AI-powered detection while retaining custom steps, or an engine configuration object parsed with the same `EngineConfig` rules as the primary workflow engine. Detection engine configuration inherits the primary engine when no detection-specific engine is provided; detection-specific engine fields take precedence, and `max-ai-credits` is resolved independently from the main agent budget.

---

## 10. Compilation-Time Security Checks

### 10.1 Overview

Compilation-time security checks validate workflow definitions before generating executable workflows, preventing security misconfigurations from reaching runtime.

### 10.2 Schema Validation

**CS-01**: The implementation MUST validate workflow frontmatter against JSON schemas.

**CS-02**: Schema validation MUST check:
- Required fields are present
- Field types match specifications
- Field values are within allowed ranges
- Enum values are from allowed sets

**CS-03**: Schema validation failures MUST cause compilation failure with descriptive error messages indicating:
- Field path (e.g., `network.allowed[0]`)
- Validation failure reason
- Expected format or value range

### 10.3 Expression Safety

**CS-04**: The implementation MUST validate that GitHub Actions expressions do not use untrusted input:

**Unsafe patterns** (MUST reject):
- `${{ github.event.issue.title }}`
- `${{ github.event.comment.body }}`
- `${{ github.head_ref }}` (can be controlled by PR authors)

**Safe patterns** (MUST allow):
- `${{ github.actor }}`
- `${{ github.repository }}`
- `${{ github.run_id }}`
- `${{ steps.sanitized.outputs.text }}` (sanitized)

**CS-05**: Expression safety violations MUST cause compilation failure with suggestions for safe alternatives.

### 10.4 Permission Validation

**CS-06**: In strict mode, the implementation MUST reject write permissions:
- `contents: write`
- `issues: write`
- `pull-requests: write`
- `discussions: write`

**CS-07**: Permission validation MUST suggest using `safe-outputs` for write operations.

### 10.5 Network Validation

**CS-08**: The implementation MUST validate network configuration:
- Ecosystem identifiers are recognized
- Domain patterns are valid (no invalid wildcards)
- Protocol prefixes are valid (`http://`, `https://`, or none)
- Wildcard patterns (`*`) are not used in strict mode

**CS-09**: Network validation failures MUST cause compilation failure with suggestions for correction.

### 10.6 Action Pinning

**CS-10**: In strict mode, the implementation MUST enforce action pinning to commit SHAs:
- Actions MUST use format `owner/repo@SHA` (40-character hex)
- Tag references (e.g., `@v4`) MUST be rejected
- Branch references (e.g., `@main`) MUST be rejected

**CS-11**: Unpinned actions MUST cause compilation failure with suggestions to use SHA pins.

### 10.7 Deprecated Features

**CS-12**: In strict mode, the implementation MUST reject deprecated frontmatter fields.

**CS-13**: Deprecation errors MUST indicate:
- Deprecated field name
- Replacement field or pattern
- Migration instructions

### 10.8 Compile-Time vs Runtime Validation Tradeoffs

The security model uses both compile-time and runtime validation because each layer covers different failure modes:

1. Compile-time checks provide deterministic early rejection for static misconfiguration (schema, expressions, permissions, network declarations, and action pinning) before execution cost is incurred.
2. Runtime checks provide enforcement against context-dependent threats that cannot be fully proven statically (actor identity, repository origin, token materialization, network path activation, and runtime output integrity).
3. Controls that can be reliably expressed statically SHOULD be enforced at compile time first to reduce runtime attack surface and operational variance.
4. Controls that depend on event payloads, credentials, or mutable platform state MUST be enforced at runtime even when a compile-time approximation exists.
5. Security-critical controls MAY be enforced in both phases when defense-in-depth materially reduces bypass risk.

---

## 11. Runtime Security Enforcement

### 11.1 Overview

Runtime security enforcement ensures security controls remain active during workflow execution.

### 11.2 Timestamp Validation

**RS-01**: The implementation MUST validate that compiled workflows are up-to-date with source definitions.

**RS-02**: Timestamp validation MUST compare:
- Source `.md` file modification time
- Compiled `.lock.yml` file modification time

**RS-03**: If source is newer than compiled workflow, the workflow MUST fail with a recompilation message.

### 11.3 Repository Validation

**RS-04**: For `pull_request` triggers, the implementation MUST validate repository ownership at runtime:

```yaml
if: github.event.pull_request.head.repo.id == github.repository_id
```

**RS-05**: For `workflow_run` triggers, the implementation MUST validate:
- Trigger scoping: non-`workflow_run` triggers in the same workflow MUST short-circuit before any `github.event.workflow_run.*` field access.
- Payload presence: `github.event.workflow_run` and `github.event.workflow_run.repository` MUST be present before repository fields are evaluated; a missing payload MUST fail closed for `workflow_run`.
- Repository ID match: `github.event.workflow_run.repository.id == github.repository_id`
- Not from fork: `!github.event.workflow_run.repository.fork`

**RS-05a**: For `workflow_dispatch` triggers where the `aw_context` input encodes a pull request context (`item_type == "pull_request"`), the implementation MUST enforce all of the following before executing a PR checkout:

1. **Repository scope**: If `aw_context.repo` is present, the implementation MUST compare it against the current repository identity (`context.repo.owner/context.repo.repo`). A mismatch MUST cause checkout to be skipped with a warning; cross-repository PR checkout is NOT supported.
2. **Actor trust**: The triggering actor MUST satisfy `assertTrustedCheckoutRuntime()`, which enforces two properties with different scopes:
   - **Fork-runtime rejection** (`workflow_dispatch` PR replay ONLY): the runtime repository MUST NOT be a fork. This check MUST NOT be evaluated for other PR-capable triggers (`pull_request`, `pull_request_target`, `pull_request_review`, `pull_request_review_comment`, `issue_comment`), because a structurally forked base repository is a legitimate topology for those triggers and rejecting it produces a false positive (see the risk matrix below). If the `workflow_dispatch` event payload does not carry `repository` data, the implementation MUST fail closed (reject checkout) rather than treat unverifiable fork status as trusted.
   - **Permission floor** (ALL PR checkout paths): the actor MUST hold write-or-higher repository permission. For centralized command or label dispatches where the runtime actor is `github-actions[bot]`, the implementation MUST validate the originating `aw_context.actor`; a missing originating actor MUST fail closed. Bot/app actors MAY use the platform-set `sender.type == "Bot"` signal only as an identity signal; it MUST NOT bypass the repository permission check. If a bot/app actor's repository permission cannot be verified, checkout MUST fail closed.
3. **Parse resilience**: Malformed `aw_context` JSON MUST be caught; the implementation MUST emit a warning and skip checkout rather than propagating the parse error.
4. **Ref isolation**: The PR head MUST be fetched exclusively via `refs/pull/N/head` from the current repository's origin, using array-based execution (no shell interpolation).

The implementation MUST NOT perform checkout when `aw_context.item_number` is absent or falsy.

**RS-05a Risk Matrix**: A conforming implementation's fork-runtime rejection MUST produce the following outcomes across the full trigger × topology combination space:

| PR-capable trigger | Runtime repository is a fork | Result |
| --- | --- | --- |
| `pull_request` (same-repo head) | yes/no | Allowed after actor trust check; direct branch fetch. |
| `pull_request` (fork head) | yes/no | Allowed after actor trust check; fetches `refs/pull/N/head`. |
| `pull_request_target`, `pull_request_review`, `pull_request_review_comment`, `issue_comment` on a PR | yes/no | Allowed after actor trust check; fetches `refs/pull/N/head` from the base context. |
| `workflow_dispatch` with valid PR `aw_context` | no | Allowed after actor trust check; fetches `refs/pull/N/head`. |
| `workflow_dispatch` with valid PR `aw_context` | yes | Rejected before actor/API/git operations. |
| `workflow_dispatch` with valid PR `aw_context` and no `repository` payload | unverifiable | Rejected (fail closed); the implementation MUST NOT infer non-fork status from absent data. |
| Any trigger without PR context, or a malformed/non-PR/cross-repository `aw_context` | any | Checkout is skipped before the fork-runtime check is reached. |

Only the fork-runtime rejection row is scope-limited to `workflow_dispatch`; the permission floor and ref-isolation properties apply uniformly across every row.

**RS-05a and the `on.fork` frontmatter field**: The `fork` GitHub Actions event (`on: {fork: null}`, exposed as `on: repository forked` in the DSL) is unrelated to RS-05a and does not interact with it. That event fires in the base/upstream repository when someone forks it, carries no `pull_request` payload, and is not `workflow_dispatch`, so `pullRequest` is never resolved in `checkout_pr_branch.cjs` and `assertTrustedCheckoutRuntime()` is never invoked for it — checkout is skipped before the fork-runtime check is reached (last row of the risk matrix above), regardless of `payload.repository.fork`. This is also distinct from the `pull_request`/`pull_request_target` `forks:` allowlist field (RS-04-adjacent), which gates workflow *activation* for inbound PRs from a forked head repository and is evaluated in the compiled `if:` condition, not in `assertTrustedCheckoutRuntime()`.

### 11.4 Role Validation

**RS-06**: The implementation MUST validate user roles at workflow start.

**RS-07**: Role validation MUST use GitHub API membership checks.

**RS-08**: Failed role checks MUST cancel execution with a descriptive message.

### 11.5 Token Validation

**RS-09**: Safe output jobs MUST validate token format before use.

**RS-10**: The implementation MUST validate that tokens are secret expressions, not plaintext.

**RS-11**: Invalid tokens MUST cause job failure with a security warning.

### 11.6 Network Enforcement

**RS-12**: For AWF sandbox, network restrictions MUST be enforced via:
- Container networking configuration
- iptables egress rules
- Domain allowlist passed to firewall

**RS-13**: For MCP servers, network restrictions MUST be enforced via:
- Per-container Squid proxy
- iptables REDIRECT rules
- Container network isolation

### 11.7 Output Validation

**RS-14**: Safe output jobs MUST validate agent output structure:
- JSON parsing success
- Schema conformance
- Required fields present
- Field types correct

**RS-15**: Invalid output MUST cause job failure with validation error details.

### 11.8 Concurrency Control

**RS-16**: The implementation MUST configure automatic concurrency control to prevent race conditions and resource conflicts.

**RS-17**: Concurrency control MUST use GitHub Actions' native `concurrency` field with:
- Group identifier that uniquely identifies the workflow context
- Optional `cancel-in-progress` flag for workflow cancellation behavior

**RS-18**: The implementation SHOULD use dynamic group identifiers that include:
- Workflow name or identifier
- Context-specific identifiers (issue number, PR number, or ref)

**RS-19**: Example concurrency configurations:

```yaml
# For pull request workflows
concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
  cancel-in-progress: true

# For issue-based workflows
concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number }}"
```

**RS-20**: Concurrency control provides operational security by:
- Preventing multiple concurrent runs on the same resource
- Avoiding race conditions in shared state
- Reducing resource exhaustion risks
- Ensuring sequential processing when required

**RS-21**: The `cancel-in-progress` flag SHOULD be set to `true` for workflows where:
- Latest run supersedes previous runs (e.g., PR review workflows)
- Multiple runs on the same context would cause conflicts
- Resource conservation is important

**RS-22**: The `cancel-in-progress` flag SHOULD be set to `false` or omitted for workflows where:
- All runs must complete (e.g., audit workflows)
- Cancellation could leave inconsistent state
- Sequential queueing is preferred over cancellation

### 11.9 Runtime Enforcement Operations Sequence

A conforming implementation MUST execute runtime controls in this order:

1. **Concurrency gate setup**: Apply `concurrency` grouping and cancellation policy before executing job logic (RS-16 through RS-22).
2. **Freshness gate**: Validate source-vs-compiled timestamps and fail fast on stale lock files (RS-01 through RS-03).
3. **Repository trust gate**: Validate repository identity and fork constraints for the active trigger (RS-04, RS-05, and RS-05a). If `lockdown: true` is active, it takes absolute precedence over guard-policy fields such as `allowed-repos` and `min-integrity`; those fields MUST NOT be evaluated to widen access beyond the triggering repository.
4. **Actor authorization gate**: Validate role membership and required privileges before any mutation-capable step (RS-06 through RS-08).
5. **Credential gate**: Validate token shape and expression-only sourcing before token use (RS-09 through RS-11).
6. **Network boundary gate**: Activate sandbox/proxy/iptables controls before running agent or MCP execution paths (RS-12 and RS-13).
7. **Threat detection gate**: Run configured detection steps and the detection engine after the agent job and before safe-output dispatch. A strict-mode detection failure (`continue-on-error: false`) MUST block safe outputs; warn mode (`continue-on-error: true`, the default) MUST surface the finding without treating transient detector failures as access grants.
8. **Output integrity gate**: Validate agent output schema and required fields before dispatching safe outputs (RS-14 and RS-15).
9. **Termination and audit**: Fail closed on any gate violation and emit a deterministic security failure reason for auditability.

---

## 12. Compliance Testing

### 12.1 Test Suite Requirements

A conforming implementation MUST provide a compliance test suite covering all MUST and SHALL requirements.

### 12.2 Test Categories

#### 12.2.1 Input Sanitization Tests

- **T-IS-001**: Verify @mention neutralization
- **T-IS-002**: Verify bot trigger protection
- **T-IS-003**: Verify XML/HTML tag conversion
- **T-IS-004**: Verify URI validation and redaction
- **T-IS-005**: Verify content size limits enforcement
- **T-IS-006**: Verify control character removal
- **T-IS-007**: Verify sanitization pipeline ordering
- **T-IS-008**: Verify bypass prevention

#### 12.2.2 Output Isolation Tests

- **T-OI-001**: Verify job architecture separation
- **T-OI-002**: Verify agent job read-only permissions
- **T-OI-003**: Verify safe output type support
- **T-OI-004**: Verify output validation rules
- **T-OI-005**: Verify token precedence order
- **T-OI-006**: Verify token secret expression validation
- **T-OI-007**: Verify write operation isolation

#### 12.2.3 Network Isolation Tests

- **T-NI-001**: Verify network mode support (defaults, custom, none)
- **T-NI-002**: Verify ecosystem identifier expansion
- **T-NI-003**: Verify domain allowlist matching (exact, subdomain, wildcard)
- **T-NI-004**: Verify protocol-specific filtering (HTTP, HTTPS)
- **T-NI-005**: Verify invalid protocol rejection
- **T-NI-006**: Verify blocked domain precedence
- **T-NI-007**: Verify AWF firewall enforcement
- **T-NI-008**: Verify MCP server network isolation
- **T-NI-009**: Verify content sanitization integration

#### 12.2.4 Permission Management Tests

- **T-PM-001**: Verify read-only permission defaults
- **T-PM-002**: Verify permission configuration support
- **T-PM-003**: Verify strict mode enforcement
- **T-PM-004**: Verify fork protection for pull_request
- **T-PM-005**: Verify repository validation for workflow_run
- **T-PM-006**: Verify role-based access control
- **T-PM-007**: Verify token validation

#### 12.2.5 Sandbox Isolation Tests

- **T-SI-001**: Verify agent sandbox type support (AWF, SRT)
- **T-SI-002**: Verify AWF chroot filesystem visibility and access model
- **T-SI-003**: Verify Docker socket hidden from chroot
- **T-SI-004**: Verify `--env-all` and `AWF_HOST_PATH` mechanism
- **T-SI-005**: Verify GOROOT capture for Go runtime
- **T-SI-006**: Verify MCP server container isolation
- **T-SI-007**: Verify network isolation independent of chroot

#### 12.2.6 Threat Detection Tests

- **T-TD-001**: Verify automatic threat detection enablement
- **T-TD-002**: Verify prompt injection detection
- **T-TD-003**: Verify secret leak detection
- **T-TD-004**: Verify malicious patch detection
- **T-TD-005**: Verify custom prompt support
- **T-TD-006**: Verify engine configuration override
- **T-TD-007**: Verify workflow failure on threat detection

#### 12.2.7 Compilation-Time Security Tests

- **T-CS-001**: Verify schema validation enforcement
- **T-CS-002**: Verify expression safety checks
- **T-CS-003**: Verify permission validation in strict mode
- **T-CS-004**: Verify network configuration validation
- **T-CS-005**: Verify action pinning enforcement
- **T-CS-006**: Verify deprecated feature rejection
- **T-SG07-001**: Verify fail-secure behavior on schema validation error — when `gh-aw compile` processes a workflow containing a schema validation error (e.g., an unrecognized top-level key, a field with a value that violates the JSON Schema type or enum constraint), compilation **MUST** fail with a non-zero exit code and a diagnostic message identifying the invalid field and the violated schema constraint. The output lock file **MUST NOT** be written or updated when compilation fails due to a schema validation error. **Assertion**: `gh-aw compile` exits non-zero; no `.lock.yml` file is created or modified; stderr contains the schema violation description. **Expected result**: compilation failure; no output artifact produced.
- **T-SG07-002**: Verify fail-secure behavior on MUST-level requirement violation — when `gh-aw compile` processes a workflow that violates a MUST-level security requirement (e.g., declaring `permissions: contents: write` without a `safe-outputs` configuration, triggering CTR-001), compilation **MUST** fail with a non-zero exit code and a diagnostic message identifying the violated requirement by its rule identifier (e.g., `CTR-001`) and providing actionable remediation guidance. The output lock file **MUST NOT** be written or updated when compilation fails due to a MUST-level requirement violation. **Assertion**: `gh-aw compile` exits non-zero; no `.lock.yml` file is created or modified; stderr contains the rule identifier and remediation guidance. **Expected result**: compilation failure; no output artifact produced.

#### 12.2.8 Runtime Security Tests

- **T-RS-001**: Verify timestamp validation
- **T-RS-002**: Verify repository validation for pull_request
- **T-RS-003**: Verify repository validation for workflow_run
- **T-RS-004**: Verify role validation
- **T-RS-005**: Verify token validation
- **T-RS-006**: Verify AWF network enforcement
- **T-RS-007**: Verify MCP network enforcement
- **T-RS-008**: Verify output validation
- **T-RS-009**: Verify concurrency control configuration
- **T-RS-010**: Verify cancel-in-progress behavior
- **T-RS-011**: Verify dynamic group identifier generation

#### 12.2.9 Companion GitHub MCP Access-Control Tests

GitHub-tool-specific runtime access-control behaviors are tracked in the companion specifications `scratchpad/github-mcp-access-control-specification.md` and `scratchpad/guard-policies-specification.md`.

- **T-GH-047 to T-GH-060** cover blocked-user enforcement, label-based promotion, minimum-integrity ordering, and related guard-policy runtime decisions for GitHub MCP integrations.
- These companion test IDs are defined in `scratchpad/github-mcp-access-control-specification.md` Section 11.1.8 (Blocked-User Tests) and Section 11.1.9 (Integrity Level Tests).
- Dedicated `trusted-users` runtime-enforcement test IDs are not yet mirrored in this top-level suite; maintainers SHOULD consult the companion GitHub MCP access-control specification and implementation tests until they are promoted here.

### 12.3 Compliance Checklist

| Requirement | Test ID | Level | Status |
|-------------|---------|-------|--------|
| Input Sanitization | T-IS-001 to T-IS-008 | 1 | Required |
| Output Isolation | T-OI-001 to T-OI-007 | 1 | Required |
| Permission Management | T-PM-001 to T-PM-007 | 1 | Required |
| Compilation-Time Checks | T-CS-001 to T-CS-006 | 1 | Required |
| Fail-Secure (SG-07) | T-SG07-001, T-SG07-002 | 1 | Required |
| Network Isolation | T-NI-001 to T-NI-009 | 2 | Required |
| Sandbox Isolation | T-SI-001 to T-SI-007 | 2 | Required |
| Runtime Enforcement | T-RS-001 to T-RS-011 | 2 | Required |
| Threat Detection | T-TD-001 to T-TD-007 | 3 | Optional |

### 12.4 Test Execution Procedures

**TP-01**: Compliance tests MUST be automated and executable in CI/CD environments.

**TP-02**: Test results MUST indicate:
- Pass/Fail status for each test
- Conformance level achieved
- Failed requirements with diagnostic information

**TP-03**: Implementations SHOULD provide a compliance report generator that produces:
- Summary of conformance level
- Detailed test results
- Failed requirement explanations
- Remediation suggestions

---

## Appendices

### Appendix A: Security Architecture Diagram

The following diagram illustrates the complete security architecture with all layers and their interactions:

```text
┌─────────────────────────────────────────────────────────────┐
│                   Workflow Definition (.md)                  │
│  - Declarative security controls                             │
│  - Network allowlists                                        │
│  - Permission specifications                                 │
│  - Safe output declarations                                  │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│              Compilation Layer (gh-aw compile)               │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ Compilation-Time Security Checks                     │    │
│  │  - Schema validation                                 │    │
│  │  - Expression safety analysis                        │    │
│  │  - Permission validation                             │    │
│  │  - Network configuration validation                  │    │
│  │  - Action pinning verification                       │    │
│  └─────────────────────────────────────────────────────┘    │
└──────────────────────┬──────────────────────────────────────┘
                       │ Generates
                       ▼
┌─────────────────────────────────────────────────────────────┐
│              Compiled Workflow (.lock.yml)                   │
│  - Fully expanded YAML                                       │
│  - Embedded security controls                                │
│  - Generated validation steps                                │
└──────────────────────┬──────────────────────────────────────┘
                       │ Deployed to
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                   GitHub Actions Runtime                     │
└──────────────────────┬──────────────────────────────────────┘
                       │
        ┌──────────────┴──────────────┐
        ▼                             ▼
┌────────────────────┐      ┌────────────────────┐
│  Activation Job    │      │   Agent Job        │
│  ─────────────     │      │   ─────────        │
│  Permissions:      │      │   Permissions:     │
│   contents: read   │      │    contents: read  │
│                    │      │    actions: read   │
│  Layer 1:          │      │                    │
│  Input Sanitization│      │  Layer 5:          │
│  ─────────────     │      │  Sandbox Isolation │
│  - @mention neutral│      │  ─────────────     │
│  - Bot trigger     │      │  ┌──────────────┐  │
│  - XML/HTML safe   │      │  │ AWF Container│  │
│  - URI validation  │      │  │  - Network   │  │
│  - Size limits     │      │  │    firewall  │  │
│  - Ctrl char strip │      │  │  - Domain    │  │
│                    │      │  │    allowlist │  │
│  Outputs:          │      │  │  - Process   │  │
│   text: sanitized  │      │  │    isolation │  │
└─────────┬──────────┘      │  └──────────────┘  │
          │                 │                    │
          │                 │  Layer 3:          │
          └────────────────►│  Network Isolation │
                            │  ─────────────     │
                            │  - Allowed domains │
                            │  - Ecosystem IDs   │
                            │  - Protocol filter │
                            │  - Blocked domains │
                            │                    │
                            │  Layer 4:          │
                            │  Permission Mgmt   │
                            │  ─────────────     │
                            │  - Least privilege │
                            │  - Read-only mode  │
                            │  - Role validation │
                            │                    │
                            │  AI Agent Output:  │
                            │   - JSON artifacts │
                            │   - Git patches    │
                            └─────────┬──────────┘
                                      │
                        ┌─────────────┴─────────────┐
                        ▼                           ▼
              ┌────────────────────┐      ┌────────────────────┐
              │ Threat Detection   │      │ Safe Output Jobs   │
              │ ─────────────      │      │ ─────────────      │
              │ Layer 6:           │      │ Layer 2:           │
              │  - Prompt inject   │      │  - Write perms     │
              │  - Secret leak     │      │  - Output valid    │
              │  - Malicious patch │      │  - Token mgmt      │
              │                    │      │  - GitHub API      │
              │ If threats found:  │      │                    │
              │  → FAIL workflow   │      │ Types:             │
              │                    │      │  - create-issue    │
              │ If clean:          │      │  - add-comment     │
              │  → Continue ───────┼─────►│  - create-pr       │
              └────────────────────┘      │  - create-discuss  │
                                          │  - close-issue     │
                                          │  - close-pr        │
                                          └────────────────────┘

Legend:
┌─────┐
│ Box │  = Component or layer
└─────┘
  →    = Data flow direction
  │    = Dependency or sequence
```

#### Job Dependency Graph

The following diagram shows the concrete job execution order within a compiled gh-aw workflow, as generated in `.lock.yml` files:

```text
pre_activation (role check — membership validation)
      │
      │  if: activated == 'true'
      ▼
activation (timestamp check, input sanitization → outputs.text)
      │
      │  if: pre_activation.outputs.activated == 'true'
      ▼
agent (AI execution — read-only permissions, AWF sandbox)
      │
      ├──────────────────────┐
      │                      │
      ▼                      ▼
detection               [other jobs]
(threat analysis —      (optional parallel
 no permissions)         post-agent jobs)
      │
      │  if: detection.outputs.success == 'true'
      ▼
safe_outputs (validated write operations — write permissions)
      │
      ▼
conclusion (cleanup, summary — optional)
```

**Key properties of this dependency chain**:

| Job | Depends On | Purpose | Permissions |
|-----|------------|---------|-------------|
| `pre_activation` | — | Role-based access control | `contents: read` |
| `activation` | `pre_activation` | Timestamp validation, input sanitization | `contents: read` |
| `agent` | `activation` | AI agent execution | `contents: read`, `actions: read` |
| `detection` | `agent` | Threat analysis of agent output | `{}` (none) |
| `safe_outputs` | `agent`, `detection` (conditional: `success == 'true'`) | Validated write to GitHub API | `contents: read`, `issues: write`, etc. |
| `conclusion` | `safe_outputs` | Cleanup and execution summary | `contents: read` |

The `detection` job acts as a security gate: `safe_outputs` only runs when `needs.detection.outputs.success == 'true'`.

> Note: The optional `conclusion` job shown above is non-normative. Implementations MAY add cleanup or reporting jobs after `safe_outputs`, but conformance does not require a `conclusion` job and such jobs MUST NOT weaken the required isolation or permission boundaries.

### Appendix B: Sanitization Examples

#### Example 1: @Mention Neutralization

**Input**:
```markdown
@octocat please review this PR
```

**Output**:
```markdown
`@octocat` please review this PR
```

#### Example 2: Bot Trigger Protection

**Input**:
```markdown
This fixes #123 and closes #456
```

**Output**:
```markdown
This `fixes #123` and `closes #456`
```

#### Example 3: XML/HTML Tag Conversion

**Input**:
```html
<script>alert('xss')</script>
<img src="x" onerror="alert('xss')"/>
```

**Output**:
```text
&lt;script&gt;alert('xss')&lt;/script&gt;
&lt;img src="x" onerror="alert('xss')"/&gt;
```

#### Example 4: URL Filtering and Validation

**Input** (with network allowlist: `defaults`, `github.com`):
```text
Check https://github.com/example/repo
Visit https://malicious.example.com
Download javascript:alert('xss')
See data:text/html,<script>alert('xss')</script>
```

**Output**:
```text
Check https://github.com/example/repo
Visit (redacted)
Download (redacted)
See (redacted)
```

**Explanation**:
- `github.com` URL preserved (in allowlist)
- `malicious.example.com` redacted (not in allowlist)
- `javascript:` protocol stripped and URL redacted (unsafe protocol)
- `data:` protocol stripped and URL redacted (unsafe protocol)

#### Example 5: ANSI Escape Code Removal

**Input**:
```text
\x1b[31mError: failed\x1b[0m
\x1b[1;32mSuccess!\x1b[0m
```

**Output**:
```text
Error: failed
Success!
```

**Explanation**: Terminal color codes (`\x1b[...m`) removed to prevent terminal injection.

#### Example 6: Combined Threat Sanitization

**Input** (markdown with multiple threats):
```markdown
Hey @admin, please run this: <script>alert(document.cookie)</script>
Check out https://phishing.example.com/steal?data=\x1b[31msecret\x1b[0m
This fixes #1 and /deploy to prod
```

**Output** (with allowlist: `defaults`, `github.com`):
```markdown
Hey `@admin`, please run this: &lt;script&gt;alert(document.cookie)&lt;/script&gt;
Check out (redacted)
This `fixes #1` and `/deploy` to prod
```

**Protections Applied**:
1. @mention neutralized (prevents notification spam)
2. HTML `<script>` tag converted to entities (prevents XSS)
3. Non-allowlisted URL redacted (prevents phishing)
4. ANSI escape codes removed (prevents terminal injection)
5. Bot triggers neutralized (prevents unintended commands)

### Appendix C: Network Configuration Examples

#### Example 1: Default Infrastructure Only

```yaml
network: defaults
```

**Allowed domains**: Certificates, JSON schema, Ubuntu packages, Microsoft sources.

#### Example 2: Selective Ecosystem Access

```yaml
network:
  allowed:
    - defaults
    - github
    - python
    - node
```

**Allowed domains**: Defaults + GitHub + PyPI + npm/yarn.

#### Example 3: Custom Domain with Protocol Filtering

```yaml
network:
  allowed:
    - defaults
    - "https://api.example.com"
    - "http://legacy-internal.corp"
```

**Allowed**: HTTPS to api.example.com, HTTP to legacy-internal.corp.

#### Example 4: Domain Blocking

```yaml
network:
  allowed:
    - defaults
    - python
  blocked:
    - "tracker.pypi.org"
    - "analytics.python.org"
```

**Result**: Python ecosystem minus tracking domains.

### Appendix D: Safe Output Configuration Examples

#### Example 1: Basic Issue Creation

```yaml
safe-outputs:
  create-issue:
```

**Generates**: Job that creates issues from agent JSON output.

#### Example 2: Multiple Output Types with Custom Tokens

```yaml
github-token: ${{ secrets.WORKFLOW_TOKEN }}

safe-outputs:
  github-token: ${{ secrets.SAFE_OUTPUT_TOKEN }}
  
  create-issue:
    github-token: ${{ secrets.ISSUE_TOKEN }}
  
  create-pull-request:
    github-token: ${{ secrets.PR_TOKEN }}
```

**Token resolution**:
- create-issue: Uses `ISSUE_TOKEN`
- create-pull-request: Uses `PR_TOKEN`

#### Example 3: With Threat Detection

```yaml
safe-outputs:
  create-pull-request:
  threat-detection:
    enabled: true
    prompt: "Focus on SQL injection vulnerabilities"
    steps:
      - name: Run TruffleHog
        uses: trufflesecurity/trufflehog@main
```

**Behavior**: AI detection + TruffleHog scan before PR creation.

In compiled workflows, this pattern yields a dedicated `detection` job that serves as the runtime threat-detection layer between the `agent` job and any `safe_outputs` write operations.

#### Example 4: Concurrency Control

```yaml
# Pull request workflow with cancel-in-progress
on:
  pull_request:
    types: [opened, synchronize]

concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
  cancel-in-progress: true

safe-outputs:
  add-comment:
```

**Behavior**: Newer runs cancel older runs for the same PR, ensuring only the latest code is analyzed.

```yaml
# Issue workflow with sequential processing
on:
  issues:
    types: [opened, labeled]

concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number }}"
  # cancel-in-progress: false (omitted = queue runs sequentially)

safe-outputs:
  create-issue:
```

**Behavior**: All workflow runs complete in order, preventing incomplete operations.

#### Example 5: Pre-Activation, Detection, and Conclusion Job Structure

The following excerpt (abridged from a compiled `.lock.yml`) shows the three implementation-detail jobs referenced in the Minor Discrepancies section of `specs/security-architecture-spec-validation.md`: `pre_activation` (role-based access control ahead of `activation`), `detection` (runtime manifestation of the Threat Detection Layer, Section 9), and `conclusion` (cleanup/summary reporting job that always runs).

```yaml
jobs:
  pre_activation:
    if: >
      (github.event_name != 'pull_request' || github.event.pull_request.head.repo.id == github.repository_id)
    runs-on: ubuntu-slim
    permissions:
      contents: read
    outputs:
      activated: ${{ steps.check_membership.outputs.is_team_member == 'true' }}
    steps:
      - name: Check team membership for workflow
        id: check_membership
        uses: actions/github-script@<pinned-sha> # vX.Y.Z
        env:
          GH_AW_REQUIRED_ROLES: "admin,maintainer,write"

  activation:
    needs: [pre_activation]
    # ... timestamp/lockdown validation (Section 11.1) ...

  agent:
    needs: [activation]
    permissions:
      contents: read
    # ... agentic execution (read-only) ...

  detection:
    needs: [activation, agent]
    if: always() && needs.agent.result != 'skipped'
    runs-on: ubuntu-latest
    permissions:
      contents: read
    outputs:
      detection_conclusion: ${{ steps.detection_conclusion.outputs.conclusion }}
      detection_success: ${{ steps.detection_conclusion.outputs.success }}
    # ... AI threat-detection scan of agent output (Section 9.1) ...

  safe_outputs:
    needs: [activation, agent, detection]
    if: (!cancelled()) && needs.agent.result != 'skipped' && needs.detection.result == 'success'
    permissions:
      issues: write
      pull-requests: write
    # ... write operations gated on successful detection ...

  conclusion:
    needs: [activation, agent, detection, safe_outputs]
    if: always() && (needs.agent.result != 'skipped' || needs.activation.outputs.lockdown_check_failed == 'true')
    permissions:
      actions: read
      issues: write
      pull-requests: write
    # ... run summary, cleanup, and failure reporting ...
```

**Behavior**: `pre_activation` gates `activation` on role membership; `detection` runs after `agent` and gates `safe_outputs` on a successful threat-detection conclusion; `conclusion` always runs last (subject to `always()`) to report status regardless of upstream success or failure.

### Appendix E: Concurrency Control Examples

#### Example 1: Pull Request Workflow with Cancellation

```yaml
name: PR Review Agent
on:
  pull_request:
    types: [opened, synchronize, ready_for_review]

concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
  cancel-in-progress: true

safe-outputs:
  add-comment:
```

**Group pattern**: `gh-aw-PR Review Agent-123` (where 123 is PR number)

**Behavior**: 
- When a new commit is pushed to PR #123, any in-progress run for that PR is cancelled
- Only the latest code version is analyzed, avoiding outdated feedback
- Resource-efficient for rapidly changing PRs

#### Example 2: Issue Workflow with Sequential Processing

```yaml
name: Issue Triage
on:
  issues:
    types: [opened, labeled]

concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number }}"
  # cancel-in-progress: false (default)

safe-outputs:
  create-issue:
  add-comment:
```

**Group pattern**: `gh-aw-Issue Triage-456` (where 456 is issue number)

**Behavior**:
- Multiple runs on the same issue queue sequentially
- Each run completes before the next starts
- Ensures all operations complete without cancellation
- Prevents race conditions in issue state

#### Example 3: Scheduled Workflow without Concurrency Conflicts

```yaml
name: Daily Audit
on:
  schedule:
    - cron: "0 9 * * *"
  workflow_dispatch:

concurrency:
  group: "gh-aw-${{ github.workflow }}"
  # No PR/issue number = single group for all runs

safe-outputs:
  create-issue:
```

**Group pattern**: `gh-aw-Daily Audit` (same for all runs)

**Behavior**:
- Only one run of this workflow can execute at a time
- Manual triggers queue behind scheduled runs
- Prevents overlapping audit operations

#### Example 4: Repository-Wide Lock

```yaml
name: Database Migration
on:
  workflow_dispatch:

concurrency:
  group: "gh-aw-database-migration-${{ github.repository }}"
  cancel-in-progress: false

safe-outputs:
  create-pull-request:
```

**Group pattern**: `gh-aw-database-migration-owner/repo`

**Behavior**:
- Repository-wide exclusive lock
- Runs queue sequentially across the entire repository
- Critical for operations that must not overlap

### Appendix F: Strict Mode Violations

#### Violation 1: Write Permissions

```yaml
strict: true
permissions:
  contents: write  # ❌ Blocked in strict mode
```

**Error**: `strict mode does not allow write permissions, use safe-outputs instead`

#### Violation 2: Unpinned Actions

```yaml
strict: true
jobs:
  test:
    steps:
      - uses: actions/checkout@v6  # ❌ Blocked in strict mode
```

**Error**: `strict mode requires actions to be pinned to commit SHA, not tag`

#### Violation 3: Wildcard Network

```yaml
strict: true
network:
  allowed:
    - "*"  # ❌ Blocked in strict mode
```

**Error**: `strict mode does not allow wildcard (*) in network domains`

### Appendix G: Lock File Validation Checklist

Use this checklist to verify that a compiled `.lock.yml` workflow file meets all security requirements.

#### G.1 Action Pinning

- [ ] All `uses:` statements reference actions with 40-character SHA commits (not tags or branch names)
- [ ] Each pinned SHA is accompanied by a version comment (e.g., `# v6`)
- [ ] No `uses:` statement references a mutable tag (e.g., `@v6`, `@main`, `@latest`)

#### G.2 Permission Separation

- [ ] Top-level `permissions: {}` sets all permissions to `none` by default
- [ ] `agent` job has only read-only permissions (`contents: read`, `actions: read`, `pull-requests: read`)
- [ ] `agent` job does NOT have write permissions of any kind
- [ ] `safe_outputs` job has write permissions only for the specific operations it performs
- [ ] `activation` job has only `contents: read`
- [ ] `pre_activation` job has only `contents: read`

#### G.3 Fork Protection

- [ ] `activation` job `if:` condition includes repository ID check for `pull_request` events:  
  `github.event.pull_request.head.repo.id == github.repository_id`
- [ ] `pre_activation` job `if:` condition (if present) includes the same fork protection

#### G.4 Input Sanitization

- [ ] `activation` job runs the timestamp validation step (`check_workflow_timestamp_api.cjs`)
- [ ] `activation` job runs the sanitize content step (`sanitize_content_core.cjs`)
- [ ] Agent prompts use `steps.sanitized.outputs.text` (sanitized), not raw `github.event.*` context

#### G.5 Threat Detection

- [ ] A `detection` job exists between the `agent` job and `safe_outputs`
- [ ] `detection` job has `permissions: {}` (no permissions)
- [ ] `safe_outputs` job has `if:` condition that checks `needs.detection.outputs.success == 'true'`

#### G.6 Role-Based Access Control

- [ ] A `pre_activation` job exists with a membership check step (`check_membership.cjs`)
- [ ] `activation` job has `needs: pre_activation` and `if: needs.pre_activation.outputs.activated == 'true'`
- [ ] `GH_AW_REQUIRED_ROLES` environment variable is set appropriately

#### G.7 AWF Sandbox (Copilot Engine)

- [ ] Agent job installs the AWF binary (`install_awf_binary.sh`)
- [ ] Agent job runs the Copilot CLI within the AWF container

#### G.8 Concurrency Control

- [ ] `concurrency.group` uses a dynamic identifier including the workflow name and event context (PR number, issue number, or ref)
- [ ] `cancel-in-progress` is set appropriately for the workflow type:
  - PR workflows: `true` (latest cancels older)
  - Issue/audit workflows: `false` or omitted (sequential)

#### G.9 Runtime Validation

- [ ] Activation job checks workflow timestamp to ensure `.lock.yml` is up-to-date
- [ ] Conclusion job (if present) is documented and performs only cleanup/summary operations

---

#### G.10 Formal Test Coverage Audit

The following table maps each Appendix G checklist category against the formal tests in
`pkg/workflow/security_architecture_sg_formal_test.go`. Each cell may list zero, one, or
more test function names. Cells marked **covered** have at least one dedicated test;
cells marked **gap** lack direct formal test coverage; cells marked **partial** have
related coverage that does not fully satisfy the checklist item.

Path check revalidated on 2026-08-25: every named test below resolves in
`pkg/workflow/security_architecture_sg_formal_test.go`.

| Checklist Category | Formal Test(s) | Coverage |
|---|---|---|
| **G.1** Action pinning | _(none)_ | **gap** — no formal test verifies that compiled `uses:` lines are SHA-pinned; relies on actionlint/poutine in CI |
| **G.2** Permission separation — agent job | `TestFormalSG02_AgentJobHasNoWritePermissions`, `TestFormalSG04_LeastPrivilegeBasePermissions` | covered |
| **G.2** Permission separation — safe_outputs job | `TestFormalStaged_HandlerRequiresNoWritePerms` | **partial** — verifies that a globally staged handler does not accumulate write grants via `ComputePermissionsForSafeOutputs`, but does not inspect the compiled `safe_outputs` job itself or verify that its `permissions:` block is limited to the configured operations |
| **G.3** Fork protection | `TestFormalBasicConformance_AllFourControls` | **partial** — verifies `safe_outputs` job presence, permission management, and compilation-time checks; does not inspect the activation job's `if:` expression or assert a repository-ID fork guard |
| **G.4** Input sanitization | `TestFormalSG01_InputSanitizationInvariant` | **partial** — verifies that a generic `run:` step with a `${{ }}` expression is rewritten and an `env:` block is injected; does not assert either required activation step or that compiled prompts consume `steps.sanitized.outputs.text` |
| **G.5** Threat detection | `TestFormalSG06_ThreatDetectionAuditArtifact`, `TestFormalThreatDetection_EnabledByDefault`, `TestFormalThreatDetection_ExplicitDisable` | **partial** — detection-job presence and configuration defaults are covered; `permissions: {}` on the detection job and the `needs.detection.outputs.success` gate on `safe_outputs` are not directly asserted |
| **G.6** RBAC — job topology (PM-10b) | `TestFormalJobTopology_PipelineOrderEnforced` (verifies `pre_activation → activation` needs dependency) | covered |
| **G.6** RBAC — membership check step (PM-11) | `TestFormalPM11_PreActivationContainsMembershipStep` | covered |
| **G.7** AWF sandbox | `TestFormalSG05_SandboxIsolationPresence` | **partial** — verifies `isSandboxEnabled()` return value for configuration combinations (AWF type, disabled flag, firewall); does not compile a workflow or assert `install_awf_binary.sh` step presence or execution inside the AWF container |
| **G.8** Concurrency control | _(none)_ | **gap** — no formal test verifies `concurrency.group` is set or that `cancel-in-progress` matches workflow type |
| **G.9** Runtime validation — timestamp check | _(none)_ | **gap** — `TestFormalSG07_FailSecureOnSecurityError` tests write-permission rejection at compile time and is unrelated to the G.9 checklist items; neither the activation timestamp step nor conclusion-job behavior is asserted by any test in this file |

**Summary of coverage gaps (as of 2026-08-25)**:

- **G.1 Action pinning**: Formal test gap. CI tooling (actionlint, poutine) provides runtime coverage; consider adding a dedicated test that parses compiled lock file `uses:` patterns.
- **G.2 Permission separation — safe_outputs job**: Partial coverage. `TestFormalStaged_HandlerRequiresNoWritePerms` does not inspect the compiled `safe_outputs` job's own `permissions:` block. A dedicated test compiling a safe-outputs workflow and asserting its permission scope would close this gap.
- **G.3 Fork protection**: Partial coverage. `TestFormalBasicConformance_AllFourControls` does not inspect the activation job's `if:` expression or verify a repository-ID fork guard. A test asserting the exact `if:` condition on the compiled activation job is needed.
- **G.4 Input sanitization**: Partial coverage. `TestFormalSG01_InputSanitizationInvariant` covers expression rewriting but not the full three-item checklist (activation step presence; `steps.sanitized.outputs.text` consumption in compiled prompts).
- **G.5 Threat detection**: Partial coverage. Detection-job presence and defaults are covered. Tests asserting `permissions: {}` on the detection job and the `needs.detection.outputs.success` gate on `safe_outputs` are missing.
- **G.7 AWF sandbox**: Partial coverage. Configuration-logic checks are covered; lock-file-level assertions (`install_awf_binary.sh`, AWF container execution) are missing.
- **G.8 Concurrency control**: Formal test gap. Concurrency group format is validated only by manual review. Consider a test that compiles a PR-trigger workflow and asserts `concurrency.group` contains the PR number expression.
- **G.9 Runtime validation — timestamp check**: Formal test gap. No formal test verifies the activation timestamp step or conclusion-job behavior.

---

### Appendix H: Security Best Practices

#### BP-01: Always Use Sanitized Context

**Don't**:
```yaml
prompt: |
  Analyze this issue: ${{ github.event.issue.title }}
```

**Do**:
```yaml
prompt: |
  Analyze this issue: ${{ steps.sanitized.outputs.text }}
```

#### BP-02: Enable Strict Mode for Production

**Don't**:
```yaml
permissions:
  contents: write
  issues: write
```

**Do**:
```yaml
strict: true
permissions:
  contents: read
  
safe-outputs:
  create-issue:
  add-comment:
```

#### BP-03: Use Specific Domain Allowlists

**Don't**:
```yaml
network:
  allowed:
    - "*"  # Too broad
```

**Do**:
```yaml
network:
  allowed:
    - defaults
    - github
    - python
    - "api.internal.example.com"
```

#### BP-04: Pin Actions to SHAs

**Don't**:
```yaml
- uses: actions/checkout@v6
- uses: actions/setup-node@main
```

**Do**:
```yaml
- uses: actions/checkout@8e8c483db84b4bee98b60c0593521ed34d9990e8 # v6
- uses: actions/setup-node@1e60f620b9541d16bece96c5465dc8ee9832be0b # v4.0.3
```

#### BP-05: Enable Threat Detection

**Don't**:
```yaml
safe-outputs:
  create-pull-request:
  threat-detection: false  # Risky
```

**Do**:
```yaml
safe-outputs:
  create-pull-request:
  threat-detection:
    enabled: true
    prompt: "Focus on security vulnerabilities specific to this codebase"
```

#### BP-06: Use Role-Based Access Control

**Don't**:
```yaml
roles: all  # Allows any user in public repos
```

**Do**:
```yaml
roles: [admin, maintainer]  # Restrict to trusted roles
```

---

## References

### Normative References

- **[RFC 2119]** S. Bradner. "Key words for use in RFCs to Indicate Requirement Levels." March 1997. https://www.ietf.org/rfc/rfc2119.txt

- **[JSON-SCHEMA]** "JSON Schema: A Media Type for Describing JSON Documents." https://json-schema.org/

- **[YAML-1.2]** Oren Ben-Kiki, Clark Evans, Ingy döt Net. "YAML Ain't Markup Language (YAML) Version 1.2." https://yaml.org/spec/1.2/spec.html

- **[GHA-SYNTAX]** "Workflow syntax for GitHub Actions." GitHub Documentation. https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions

- **[GHA-SECURITY]** "Security hardening for GitHub Actions." GitHub Documentation. https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions

- **[GHAW-GUARD-POLICIES]** "Guard Policies Integration Specification." GitHub Agentic Workflows, version 0.1.0 draft. https://github.com/github/gh-aw/blob/main/scratchpad/guard-policies-specification.md

- **[GHAW-GITHUB-ACCESS]** "GitHub MCP Server Access Control Specification." GitHub Agentic Workflows, version 1.1.0 draft. https://github.com/github/gh-aw/blob/main/scratchpad/github-mcp-access-control-specification.md

### Informative References

- **[MCP-SPEC]** "Model Context Protocol Specification." Anthropic. https://modelcontextprotocol.io/specification

- **[MCP-SECURITY]** "Security Best Practices for MCP." June 2025. https://modelcontextprotocol.io/specification/2025-06-18/basic/security_best_practices

- **[OWASP-TOP10]** "OWASP Top Ten." OWASP Foundation. https://owasp.org/www-project-top-ten/

- **[CWE]** "Common Weakness Enumeration." MITRE Corporation. https://cwe.mitre.org/

- **[ACTIONLINT]** "actionlint: Static checker for GitHub Actions workflow files." https://github.com/rhysd/actionlint

- **[ZIZMOR]** "zizmor: Security scanner for GitHub Actions." https://github.com/woodruffw/zizmor

- **[GHAW-DOCS]** "GitHub Agentic Workflows Documentation." https://github.com/github/gh-aw

- **[GHAW-ARCH]** "GitHub Agentic Workflows Architecture." https://github.com/github/gh-aw/blob/main/docs/src/content/docs/introduction/architecture.mdx

---

## Change Log

### Version 1.0.1 (Editorial Update, addendum: September 24, 2026)

**Scoped RS-05a fork-runtime rejection to `workflow_dispatch` PR replays**:
- The fork-runtime rejection in `assertTrustedCheckoutRuntime()` now applies only
  to `workflow_dispatch` PR replays; `pull_request`, `pull_request_target`,
  `pull_request_review`, `pull_request_review_comment`, and `issue_comment`
  events are unaffected by a structurally forked base repository. The
  permission-floor and ref-isolation properties remain unchanged for all paths.
- Adds the RS-05a Risk Matrix documenting expected outcomes across the full
  trigger × fork-topology combination space, based on the systematic risk
  analysis in https://github.com/github/gh-aw/pull/63011#issuecomment-5804282930.
- Closes a fail-open gap: `workflow_dispatch` PR checkout is now rejected when
  the event payload carries no `repository` data, since fork status cannot be
  verified in that case.
- Backed by additional unit test coverage in
  `actions/setup/js/checkout_pr_branch.test.cjs` for the missing-`repository`
  case and for forked-runtime checkout across every non-`workflow_dispatch`
  PR-capable trigger.

### Version 1.0.1 (Editorial Update)

**Published**: August 1, 2026

**Added RS-05a** — `workflow_dispatch` + `aw_context` PR checkout validation:
- Documents the four-property security contract for the new `workflow_dispatch`
  event path in `checkout_pr_branch.cjs`: repository-scope check, actor trust,
  parse resilience, and ref isolation.
- Updates Section 11.9 (Runtime Enforcement Operations Sequence) to reference RS-05a
  alongside RS-04 and RS-05 in the repository trust gate.
- Backed by 9 unit test cases in `actions/setup/js/checkout_pr_branch.test.cjs`
  (cross-repo mismatch, invalid JSON, missing `item_number`, successful checkout, etc.).

### Version 1.0.0 (Candidate Recommendation)

**Published**: January 29, 2026  
**Status**: Candidate Recommendation

**Initial Release**:
- Formalized security architecture with seven independent layers
- Defined three conformance classes (Basic, Standard, Complete)
- Specified input sanitization requirements and procedures
- Specified output isolation patterns and guarantees
- Specified network isolation controls and enforcement
- Specified permission management and least-privilege requirements
- Specified sandbox isolation for agents and MCP servers
- Specified threat detection layer and validation procedures
- Specified compilation-time security checks
- Specified runtime security enforcement
- Defined compliance testing requirements and test categories
- Included appendices with examples and best practices

**Key Design Decisions**:
- Defense-in-depth approach with independent, composable layers
- Explicit over implicit security controls
- Fail-secure behavior on security violations
- Separation of concerns between read and write operations
- Declarative security configuration in workflow definitions

**Target Implementations**:
- GitHub Agentic Workflows (reference implementation)
- Other CI/CD platforms (GitLab CI, Jenkins, CircleCI, etc.)
- Agentic workflow systems in enterprise environments

---

*Copyright © 2026 GitHub, Inc. All rights reserved.*

---

## Sync Notes

This section cross-references the normative compliance validation document and defines the revalidation cadence for this specification.

### Validation Document

The compliance validation pass for this specification is documented in:

- **`specs/security-architecture-spec-validation.md`** — records the results of the most recent full validation pass against each MUST/SHALL requirement, including test IDs, pass/fail status, and any open findings. All conforming implementations **SHOULD** consult this document to understand the current validation state before making security architecture changes.

### Revalidation Trigger

A full revalidation pass **MUST** be triggered and the results in `specs/security-architecture-spec-validation.md` **MUST** be updated whenever:

1. This specification is incremented to a new minor version (e.g., `1.0.x` → `1.1.0` or higher).
2. A new MUST-level requirement is added or an existing MUST-level requirement is modified or removed in any section.
3. A security incident or CVE is attributed to a control defined in this specification.
4. The reference implementation (GitHub Agentic Workflows) ships a change that affects a control described in Sections 4–11.

The revalidation cadence **SHOULD** also include a review of `specs/security-architecture-spec-validation.md` on every minor version bump, even in the absence of the above triggers, to ensure the validation summary table remains current.

### Last Revalidation

The most recent full validation pass against this specification was completed on **2026-07-15**. The results are recorded in `specs/security-architecture-spec-validation.md`. Any changes to MUST-level requirements after this date require a new validation pass per the triggers above.

Sync check on **2026-08-25** found no new minor-version bump, reported security incident, or reference-implementation change requiring a full revalidation since the 2026-07-15 pass. This maintenance pass synchronized §9/§11 prose to the existing `pkg/workflow/threat_detection_config.go` parser and `pkg/workflow/threat_detection_inline_engine.go` runtime behavior; because it clarifies MUST-level runtime sequencing text, a targeted §9/§11 revalidation is scheduled with the Security Architecture maintainers by **2026-09-01**. The known Appendix G.10 partial-coverage gaps remain tracked there.

*This specification is provided under the MIT License.*