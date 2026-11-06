# Security Architecture Specification - Summary

**Document**: `security-architecture-spec.md`  
**Architecture baseline**: 1.0.0
**Current specification revision**: 1.1.9 / November 5, 2026
**Status**: Candidate Recommendation  
**Date**: November 5, 2026
**Last full validation**: v1.0.0 / 2026-07-15
**Current targeted lock check**: 2026-11-03 (partial; see [validation report](security-architecture-spec-validation.md))

## Overview

The GitHub Agentic Workflows Security Architecture Specification is a formal W3C-style document that defines the security architecture, guarantees, and implementation requirements for gh-aw. This specification enables organizations to replicate the security model in other CI/CD environments.

### Appendix G Test Mapping (v1.1.9 / 2026-11-05)

The full specification's v1.1.9 revision updates Appendix G.10 test-path mappings
and clarifies partial-coverage limits. It was published November 5, 2026; this
summary's current specification revision and date match that header and change
log.

### Floating-Head Opt-In (v1.1.8 / 2026-10-28)

`checkout.pull-request-floating-head: true` explicitly selects the current API PR head instead of the triggering revision; it is not revision approval. The default remains event revision binding. The opt-in retains SHA fetch and verification, fetch-time head checks, authorization, and checkout-disable policies. Formal revalidation remains pending.

### RS-05a Update (v1.1.7 / 2026-10-27)

This summary retains its historical validation baseline above. Current [RS-05a requirements](security-architecture-spec.md#113-repository-validation) bind PR checkout to a validated immutable SHA from `origin` and verify actual `HEAD` before success or patch-baseline export. Ordinary PR events retain their recorded revision; label/review/revision-bound dispatch paths reject detected head changes. Malformed or cross-repository PR dispatch context fails explicitly. Issue-comment commands and unbound manual dispatch select the current head, not an approved revision; timestamps cannot prove which revision was reviewed. Actor trust, credential constraints, and independent safe-output lifecycle checks remain unchanged. The [validation mapping](security-architecture-spec-validation.md#7b-pr-checkout-authorization-and-revision-isolation-section-113---rs-05a) records runtime evidence, not a new formal freshness proof or live GitHub fetch test; targeted formal revalidation remains pending.

## Key Highlights

### Conformance Classes

1. **Basic Conformance** (Level 1): Core security controls
   - Input sanitization
   - Output isolation
   - Permission management
   - Compilation-time checks

2. **Standard Conformance** (Level 2): Production-ready security
   - Basic + Network isolation
   - Basic + Sandbox isolation
   - Basic + Runtime enforcement

3. **Complete Conformance** (Level 3): Maximum security
   - Standard + Threat detection
   - Standard + All recommended enhancements

### Security Architecture Layers

The specification defines a **7-layer defense-in-depth architecture**:

0. **Compilation-Time Validation** - Schema, expressions, permissions
1. **Input Sanitization Layer** - @mentions, bot triggers, XML/HTML, URIs
2. **Output Isolation Layer** - Separate read/write operations
3. **Network Isolation Layer** - Domain allowlisting, ecosystem IDs
4. **Permission Management Layer** - Least privilege, role-based access
5. **Sandbox Isolation Layer** - AWF/SRT containers, MCP isolation
6. **Threat Detection Layer** - Prompt injection, secret leaks, malicious patches

### Core Security Guarantees

The specification defines **7 security guarantees (SG-01 to SG-07)**:

- **SG-01**: Untrusted input not directly interpolated into GitHub Actions expressions without sanitization
- **SG-02**: AI agents have no direct write access
- **SG-03**: Network access restricted to allowlists
- **SG-04**: Least-privilege permissions by default
- **SG-05**: Agent processes in isolated sandboxes
- **SG-06**: All actions produce auditable artifacts
- **SG-07**: Security failures prevent execution (fail-secure)

**Note on SG-01**: This guarantee protects against template injection in GitHub Actions expressions. It does not prevent AI agents from accessing untrusted data at runtime through tools like GitHub MCP (which can return issue titles, PR bodies, etc.). Such data is subject to AI prompt injection risks, which are addressed through threat detection (Layer 6) and safe outputs isolation (Layer 2).

### Formal Model

The seven security guarantees are encoded as a state-machine with invariants using TLA+, F* pre/post contracts, and Z3/SMT-LIB arithmetic bounds.

**State space** (`WorkflowState`):

```
WorkflowState ≜ [
  steps         : Seq(Step),
  permissions   : PermissionScope → PermissionValue,
  network       : NetworkPermissions,
  sandboxConfig : SandboxConfig ∪ {nil},
  threatDetect  : ThreatDetectionConfig ∪ {nil},
  emitAllowed   : Bool
]
```

**TLA+ invariants** (one per security guarantee):

```tla
SG01_InputSanitization ≜
  ∀ step ∈ WorkflowState.steps :
    step.run ≠ nil ∧ ContainsExpression(step.run) ⟹ step.env ≠ nil

SG02_AgentReadOnly ≜
  ∀ scope ∈ AllPermissionScopes \ {id-token, metadata} :
    WorkflowState.permissions[scope] ≠ write

SG03_NetworkAllowlist ≜
  ∀ domain ∈ WorkflowState.network.blocked :
    domain ∈ GetBlockedDomains(WorkflowState.network)

SG04_LeastPrivilege ≜
  ∀ scope ∈ DefaultPermissions :
    WorkflowState.permissions[scope] = read

SG05_SandboxIsolation ≜
  isSandboxEnabled(sandboxConfig, network) ⟺
    (sandboxConfig ≠ nil ∧ sandboxConfig.Agent ≠ nil ∧
      sandboxConfig.Agent.Type = AWF ∧ ¬sandboxConfig.Agent.Disabled) ∨
    (network ≠ nil ∧ network.Firewall ≠ nil ∧ network.Firewall.Enabled)

SG06_Auditability ≜
  WorkflowState.threatDetect ≠ nil ⟹
    ∃ job ∈ CompiledJobs : job.name = "detection"

SG07_FailSecure ≜
  DangerousPermissions(WorkflowState) ⟹
    CompileToYAML(WorkflowState) = ("", Error) ∧
    WorkflowState.emitAllowed = false
```

**F* pre/post contracts** (selected):

```fstar
val parseThreatDetectionConfig :
  outputMap:map string any →
  Tot (option ThreatDetectionConfig)
  (requires True)
  (ensures fun td →
    (not (Map.mem "threat-detection" outputMap) ⟹ Some? td) ∧
    (Map.sel outputMap "threat-detection" = false ⟹ td = None))

val IsContinueOnError :
  td:ThreatDetectionConfig →
  Tot bool
  (requires True)
  (ensures fun b → td.ContinueOnError = None ⟹ b = true)
```

**Z3/SMT-LIB bounds** (conformance level ordering):

```smt2
(declare-const basic    Int)
(declare-const standard Int)
(declare-const complete Int)
(assert (= basic    1))
(assert (= standard 2))
(assert (= complete 3))
(assert (>= complete standard))
(assert (>= standard basic))
(check-sat) ; sat — ordering is consistent
```

**Job pipeline topology** (Appendix A canonical order):

```
pre_activation → activation → agent → detection → safe_outputs → conclusion?
```

This ordering is enforced as an invariant:

```tla
JobTopologyOrder ≜
  LET canonical == << "pre_activation", "activation", "agent",
                      "detection", "safe_outputs", "conclusion" >>
  IN ∀ i ∈ 1..(Len(canonical)-1) :
       LET a == canonical[i]
           b == canonical[i+1]
       IN (a ∈ CompiledJobNames ∧ b ∈ CompiledJobNames) ⟹
            IndexOf(a, CompiledJobNames) < IndexOf(b, CompiledJobNames)
```

### Behavioral Coverage Map

| Predicate / Invariant | Test Function | Description |
|---|---|---|
| `SG01_InputSanitization` | `TestFormalSG01_InputSanitizationInvariant` | Verifies untrusted input is not interpolated without sanitization |
| `SG02_AgentReadOnly` | `TestFormalSG02_AgentJobHasNoWritePermissions` | Agent jobs must carry zero write permission scopes |
| `SG03_NetworkAllowlist` | `TestFormalSG03_NetworkAllowlistEnforcement` | Blocked domains always take precedence over allowed list |
| `SG04_LeastPrivilege` | `TestFormalSG04_LeastPrivilegeBasePermissions` | Base activation permissions default to read-only scopes |
| `SG05_SandboxIsolation` | `TestFormalSG05_SandboxIsolationPresence` | Agent job sandbox container configuration is present |
| `SG06_Auditability` | `TestFormalSG06_ThreatDetectionAuditArtifact` | Threat detection produces auditable output when enabled |
| `SG07_FailSecure` | `TestFormalSG07_FailSecureOnSecurityError` | Compiler does not emit output when security validation fails |
| `T-CS-001 SchemaValidation` | `TestFormalCS001_SchemaValidationRejectsUnknownField` | Unknown frontmatter keys fail schema validation before compilation |
| `T-CS-002 ExpressionSafety` | `TestFormalCS002_ExpressionSafetyRejectsUnauthorizedExpression` | Unauthorized GitHub Actions expressions are rejected at compile-time |
| `BasicConformance` | `TestFormalBasicConformance_AllFourControls` | All four basic conformance controls are present in a compiled workflow |
| `ThreatDetectionOrDefault` | `TestFormalThreatDetection_EnabledByDefault` | Threat detection auto-injected when safe-outputs configured without explicit disable |
| `ThreatDetectionOrDefault` | `TestFormalThreatDetection_ExplicitDisable` | Threat detection returns nil when explicitly set to false |
| `IsContinueOnError` | `TestFormalThreatDetection_ContinueOnErrorDefault` | IsContinueOnError defaults to true when field is nil |
| `PM11_PreActivationMembership` | `TestFormalPM11_PreActivationContainsMembershipStep` | `pre_activation` job contains the runtime `check_membership` gate when RBAC is enabled |
| `StagedHandlerNoWritePerms` | `TestFormalStaged_HandlerRequiresNoWritePerms` | Staged safe-output handlers do not require write permissions |
| `IDTokenRequirement` | `TestFormalIDToken_OIDCVaultActionsRequireWriteScope` | OIDC vault actions trigger id-token:write requirement |
| `T-RS-003 WorkflowRunRepoCheck` | `TestFormalRS003_WorkflowRunRepositoryValidation` | `workflow_run` triggers include repository-id safety checks |
| `T-RS-004 RuntimeRoleValidation` | `TestFormalRS004_RuntimeRoleValidation` | Role-gated workflows enforce runtime membership checks |
| `T-RS-005 RuntimeTokenValidation` | `TestFormalRS005_RuntimeTokenValidation` | Runtime role/token checks use `GITHUB_TOKEN` and exclude custom tokens |
| `T-RS-006 AWFNetworkEnforcement` | `TestFormalRS006_AWFNetworkPolicyValidation` | Strict network validation rejects unrestricted wildcard access |
| `T-RS-007 MCPNetworkEnforcement` | `TestFormalRS007_MCPNetworkPolicyValidation` | Containerized MCP servers require explicit top-level network policy |
| `T-RS-008 OutputValidation` | `TestFormalRS008_OutputTargetValidation` | Invalid safe-output targets fail validation |
| `getPushFallbackAsPullRequest` | `TestFormalPushFallback_DefaultsToTrue` | Push fallback-as-pull-request defaults to true when config is nil |
| `JobTopologyOrder` | `TestFormalJobTopology_PipelineOrderEnforced` | Compiled job pipeline preserves pre_activation→activation→agent→detection→safe_outputs order |

### Generated Test Suite

The 24 test functions above are implemented in
`pkg/workflow/security_architecture_sg_formal_test.go` using the Go
`testify` library. All tests carry the `//go:build !integration` tag so they
run in the default unit-test suite without any special flags.

Each test function:

- maps to exactly one predicate/invariant in the coverage map above;
- calls production code directly (no stubs, no mocking);
- uses `assert`/`require` calls whose failure messages quote the SG identifier
  and the violated invariant clause;
- is independently runnable with `go test -run <TestFunctionName>`.

Run the full suite:

```sh
go test ./pkg/workflow/ -run 'TestFormalSG|TestFormalCS|TestFormalRS|TestFormalBasicConformance|TestFormalThreatDetection|TestFormalPM11|TestFormalStaged|TestFormalIDToken|TestFormalPushFallback|TestFormalJobTopology' -v
```

### Formal Requirements

The following links point to the normative requirement entries in the full
specification. Identifier ranges include every numbered requirement and suffix
variant shown in that section.

| Formal requirement | Entry in `security-architecture-spec.md` |
|---|---|
| SG-01–SG-07 | [§3.2 Security Guarantees](security-architecture-spec.md#32-security-guarantees) |
| IS-01–IS-03 | [§4.2 Sanitization Requirements](security-architecture-spec.md#42-sanitization-requirements) |
| IS-04 | [§4.3.1 Mention Neutralization](security-architecture-spec.md#431-mention-neutralization) |
| IS-05 | [§4.3.2 Bot Trigger Protection](security-architecture-spec.md#432-bot-trigger-protection) |
| IS-06, IS-06a–IS-06b | [§4.3.3 HTML/XML Tag Filtering](security-architecture-spec.md#433-htmlxml-tag-filtering) |
| IS-07, IS-07a–IS-07b | [§4.3.4 URL Filtering and Validation](security-architecture-spec.md#434-url-filtering-and-validation) |
| IS-08 | [§4.3.5 Content Limits](security-architecture-spec.md#435-content-limits) |
| IS-09, IS-09a | [§4.3.6 Control Character Removal](security-architecture-spec.md#436-control-character-removal) |
| IS-10 | [§4.4 Sanitization Pipeline](security-architecture-spec.md#44-sanitization-pipeline) |
| IS-11 | [§4.5 Bypass Prevention](security-architecture-spec.md#45-bypass-prevention) |
| OI-01–OI-02 | [§5.2 Job Architecture](security-architecture-spec.md#52-job-architecture) |
| OI-03–OI-05 | [§5.3 Safe Output Configuration](security-architecture-spec.md#53-safe-output-configuration) |
| OI-06–OI-07 | [§5.4 Output Validation](security-architecture-spec.md#54-output-validation) |
| OI-08–OI-10 | [§5.5 Token Management](security-architecture-spec.md#55-token-management) |
| OI-11 | [§5.6 Output Isolation Guarantees](security-architecture-spec.md#56-output-isolation-guarantees) |
| NI-01–NI-03 | [§6.2 Network Configuration](security-architecture-spec.md#62-network-configuration) |
| NI-04–NI-05 | [§6.3 Ecosystem Identifiers](security-architecture-spec.md#63-ecosystem-identifiers) |
| NI-06–NI-08 | [§6.4 Domain Allowlisting](security-architecture-spec.md#64-domain-allowlisting) |
| NI-09–NI-11 | [§6.5 Domain Blocking](security-architecture-spec.md#65-domain-blocking) |
| NI-12–NI-13 | [§6.6 Enforcement Mechanisms](security-architecture-spec.md#66-enforcement-mechanisms) |
| NI-14 | [§6.7 Content Sanitization Integration](security-architecture-spec.md#67-content-sanitization-integration) |
| PM-01–PM-02 | [§7.2 Permission Defaults](security-architecture-spec.md#72-permission-defaults) |
| PM-03–PM-04 | [§7.3 Permission Configuration](security-architecture-spec.md#73-permission-configuration) |
| PM-05–PM-07 | [§7.4 Strict Mode](security-architecture-spec.md#74-strict-mode) |
| PM-08–PM-09 | [§7.5 Fork Protection](security-architecture-spec.md#75-fork-protection) |
| PM-10–PM-12, PM-10a, PM-10b, PM-10c, PM-10d | [§7.6 Role-Based Access Control](security-architecture-spec.md#76-role-based-access-control) |
| PM-13–PM-15 | [§7.7 Token Validation](security-architecture-spec.md#77-token-validation) |
| SI-01–SI-03 | [§8.2 Coding Agent Sandbox](security-architecture-spec.md#82-coding-agent-sandbox) |
| SI-04–SI-05 | [§8.3 AWF Sandbox Requirements](security-architecture-spec.md#83-awf-sandbox-requirements) |
| SI-08–SI-10 | [§8.4 MCP Server Sandbox](security-architecture-spec.md#84-mcp-server-sandbox) |
| SI-06–SI-07 | [§8.5 Environment Variable Inheritance](security-architecture-spec.md#85-environment-variable-inheritance) and [§8.6 Sandbox Guarantees](security-architecture-spec.md#86-sandbox-guarantees) |
| TD-01–TD-03 | [§9.2 Threat Detection Requirements](security-architecture-spec.md#92-threat-detection-requirements) |
| TD-04–TD-05 | [§9.3 Detection Categories](security-architecture-spec.md#93-detection-categories) |
| TD-06–TD-07 | [§9.4 Detection Methods](security-architecture-spec.md#94-detection-methods) |
| TD-08–TD-10 | [§9.5 Detection Output](security-architecture-spec.md#95-detection-output) |
| TD-11–TD-12 | [§9.6 Custom Prompts](security-architecture-spec.md#96-custom-prompts) |
| TD-13–TD-15 | [§9.7 Engine Configuration](security-architecture-spec.md#97-engine-configuration) |
| CS-01–CS-03 | [§10.2 Schema Validation](security-architecture-spec.md#102-schema-validation) |
| CS-04–CS-05 | [§10.3 Expression Safety](security-architecture-spec.md#103-expression-safety) |
| CS-06–CS-07 | [§10.4 Permission Validation](security-architecture-spec.md#104-permission-validation) |
| CS-08–CS-09 | [§10.5 Network Validation](security-architecture-spec.md#105-network-validation) |
| CS-10–CS-11 | [§10.6 Action Pinning](security-architecture-spec.md#106-action-pinning) |
| CS-12–CS-13 | [§10.7 Deprecated Features](security-architecture-spec.md#107-deprecated-features) |
| RS-01–RS-03 | [§11.2 Timestamp Validation](security-architecture-spec.md#112-timestamp-validation) |
| RS-04–RS-05a | [§11.3 Repository Validation](security-architecture-spec.md#113-repository-validation) |
| RS-06–RS-08 | [§11.4 Role Validation](security-architecture-spec.md#114-role-validation) |
| RS-09–RS-11 | [§11.5 Token Validation](security-architecture-spec.md#115-token-validation) |
| RS-12–RS-13 | [§11.6 Network Enforcement](security-architecture-spec.md#116-network-enforcement) |
| RS-14–RS-15 | [§11.7 Output Validation](security-architecture-spec.md#117-output-validation) |
| RS-16–RS-22 | [§11.8 Concurrency Control](security-architecture-spec.md#118-concurrency-control) |

### Test Categories

1. **Input Sanitization Tests** (T-IS-001 to T-IS-008)
2. **Output Isolation Tests** (T-OI-001 to T-OI-007)
3. **Network Isolation Tests** (T-NI-001 to T-NI-009)
4. **Permission Management Tests** (T-PM-001 to T-PM-007)
5. **Sandbox Isolation Tests** (T-SI-001 to T-SI-007)
6. **Threat Detection Tests** (T-TD-001 to T-TD-007)
7. **Compilation-Time Security Tests** (T-CS-001 to T-CS-006)
8. **Runtime Security Tests** (T-RS-001 to T-RS-008)

## Document Structure

| Section | Content | Requirements |
|---------|---------|--------------|
| 1. Introduction | Purpose, scope, design goals | - |
| 2. Conformance | Classes, notation, compliance levels | 3 classes |
| 3. Architecture | Multi-layer overview, guarantees, threat model | 7 guarantees, 6 principles |
| 4. Input Sanitization | Sanitization procedures, bypass prevention | 11 requirements |
| 5. Output Isolation | Job architecture, validation, token management | 11 requirements |
| 6. Network Isolation | Configuration, allowlists, enforcement | 14 requirements |
| 7. Permission Management | Defaults, strict mode, role-based access | 15 requirements |
| 8. Sandbox Isolation | Agent sandbox, MCP isolation, guarantees | 13 requirements |
| 9. Threat Detection | Categories, methods, output format | 15 requirements |
| 10. Compilation Security | Schema, expressions, permissions, actions | 13 requirements |
| 11. Runtime Security | Timestamp, repository, role, token validation | 15 requirements |
| 12. Compliance Testing | Test suite, categories, procedures | 70+ tests |
| Appendices A-H | Diagrams, examples, best practices | 8 appendices |

## Appendices

### Appendix A: Security Architecture Diagram
Complete visual representation of the security architecture with all layers, plus a concrete job dependency graph showing `pre_activation → activation → agent → detection → safe_outputs → conclusion`.

### Appendix B: Sanitization Examples
Real-world examples of @mention, bot trigger, XML/HTML, and URI sanitization.

### Appendix C: Network Configuration Examples
Sample configurations for default, selective, protocol-specific, and blocked domains.

### Appendix D: Safe Output Configuration Examples
Examples of basic, multi-output, and threat-detection-enabled configurations.

### Appendix E: Concurrency Control Examples
Examples of concurrency configuration patterns for PR workflows, issue workflows, scheduled workflows, and repository-wide locks.

### Appendix F: Strict Mode Violations
Common violations and error messages for write permissions, unpinned actions, and wildcards.

### Appendix G: Lock File Validation Checklist
Step-by-step checklist for verifying that a compiled `.lock.yml` file meets all security requirements, covering action pinning, permission separation, fork protection, input sanitization, threat detection, role-based access control, AWF sandbox, concurrency control, and runtime validation.

### Appendix H: Security Best Practices
Six key best practices with "Don't" and "Do" examples.

## 8. REASONS Canvas

### Requirements

The normative requirements remain in [`security-architecture-spec.md`](security-architecture-spec.md), Sections 4–12. This summary describes their architecture and evidence; it does not replace those requirements.

### Entities

The architecture's principal entities are the compiled workflow jobs (`pre_activation`, `admission`, `activation`, `agent`, `detection`, `evals`, `push_evals_state`, `safe_outputs`, `conclusion`, `conclusion_admission`), the sanitized input, structured agent output, and the permission scopes granted to each job.

### Approach

Workflows are compiled to GitHub Actions with least-privilege job separation: the agent job runs read-only, threat detection gates safe outputs, and validated write operations occur in dedicated jobs. Enforcement mechanisms are listed under Safeguards.

### Structure

The full specification's Sections 4–12 define the security layers, and the [validation report](security-architecture-spec-validation.md#current-compiled-job-structure-2026-11-03) maps them to the compiled job structure.

### Evidence

The [validation report](security-architecture-spec-validation.md) records the historical full validation and the current targeted lock check. The 2026-11-03 check confirms the ten-job topology in `.github/workflows/security-review.lock.yml`; it is not a full revalidation.

### Assumptions

Compiled lock files are the evidence for generated job structure and permissions. One representative workflow does not establish that every workflow has identical job scopes or trigger conditions.

### Safeguards

| Practice | Enforcement | Mechanism |
|---|---|---|
| BP-01 Sanitized context | Compile-time | Compiler rewrites `${{ github.event.* }}` expressions in `prompt:` to `${{ steps.sanitized.outputs.text }}` during compilation; unsanitized expressions never reach the generated `.lock.yml` |
| BP-02 Strict mode for production | Compile-time | `strict: true` causes the compiler to reject workflows with write permissions on the `agent` job or missing `safe-outputs:`; violations fail `gh aw compile` |
| BP-03 Specific domain allowlists | Runtime | The AWF network proxy/firewall enforces the configured `network.allowed` domain list against outbound requests during workflow execution; a wildcard (`"*"`) is accepted at compile time but only its effect is observed at runtime |
| BP-04 Pin actions to SHAs | Compile-time (advisory) / CI-time (enforced) | The compiler itself does not reject unpinned `uses:` references; SHA-pinning is enforced by CI tooling (`actionlint`, `poutine`, `zizmor`) run against compiled `.lock.yml` files, not by the compiler at `gh aw compile` time (see Appendix G.1 coverage gap) |
| BP-05 Enable threat detection | Compile-time (job generation) / Runtime (detection execution) | The compiler generates a `detection` job when `threat-detection.enabled` is not explicitly `false`; runtime `safe_outputs` execution is gated on successful detection-job completion. The representative lock does not establish one universal gate expression for every workflow. |
| BP-06 Role-based access control | Runtime | `roles:` configures the `GH_AW_REQUIRED_ROLES` environment variable consumed by the `pre_activation` job's `check_membership.cjs` step, which queries GitHub's API for the triggering actor's role at workflow run time; the compiler does not verify roles ahead of time |

### Operations

The representative workflow orders its jobs as `pre_activation` → `admission` → `activation` → `agent` → `detection` → `evals` → `push_evals_state`, with `safe_outputs` also following `detection`, and `conclusion` → `conclusion_admission` terminal. The conclusion job is terminal reporting/cleanup, not a required security layer. See the current job table in the [validation report](security-architecture-spec-validation.md#current-compiled-job-structure-2026-11-03).

### Norms

The full specification defines normative MUST/SHOULD requirements. Summary status statements distinguish historical full-validation results from targeted current evidence; a job's presence alone does not establish its runtime guarantees.

### Summary

This canvas covers requirements, entities, approach, structure, operations, norms, and safeguards, with evidence, assumptions, and this summary as additional subsections. The latest targeted check leaves PM-08 fork protection UNVERIFIED for the representative lock and does not assign a new overall validation grade.

## Target Audience

- **Security Engineers**: Audit and verify security controls
- **Platform Engineers**: Implement equivalent systems in other CI/CD platforms
- **Compliance Teams**: Assess conformance to security standards
- **Workflow Authors**: Understand security guarantees and limitations
- **Research Teams**: Build upon or extend the security architecture

## References

### Normative References
- RFC 2119 (Requirement keywords)
- JSON Schema
- YAML 1.2
- GitHub Actions Syntax
- GitHub Actions Security

### Informative References
- MCP Specification
- MCP Security Best Practices
- OWASP Top 10
- CWE (Common Weakness Enumeration)
- [actionlint](https://github.com/rhysd/actionlint) (workflow checker)
- [zizmor](https://github.com/zizmorcore/zizmor) and its maintainers and contributors ([audit definitions](https://docs.zizmor.sh/audits/), [MIT License](https://github.com/zizmorcore/zizmor/blob/main/LICENSE)); upstream security research and remediation guidance informing selected independent gh-aw AST checks, not a claim of parity or endorsement
- GitHub Agentic Workflows documentation

## Implementation Status

This summary covers the **1.0.0 architecture baseline** and its reference implementation. The full validation grade is historical; the current targeted check is limited to the evidence stated in the validation report.

- **Reference Implementation**: GitHub Agentic Workflows (Go-based)
- **Compiled Format**: GitHub Actions YAML (`.lock.yml` files)
- **Runtime**: GitHub Actions with AWF/SRT sandboxes
- **Language**: Go with embedded JavaScript/shell scripts

### Implementation Files

Key implementation files referenced in the specification:

- `pkg/workflow/safe_outputs_parser.go` - Safe-output frontmatter parsing
- `pkg/workflow/safe_outputs_config_base.go` - Shared safe-output configuration
- `pkg/workflow/safe_outputs_config_types.go` - Safe-output type configuration
- `pkg/workflow/engine.go` - Network permissions
- `pkg/workflow/compiler_safe_outputs.go` - Safe output compilation
- `pkg/workflow/safe_jobs.go` - Threat detection
- `pkg/workflow/compiler_types.go` - Core types

### Spec-to-Lock Sync (v1.0.0)

The 1.0.0 architecture baseline was last fully validated on **2026-07-15**, matching the historical validation marker in `specs/security-architecture-spec-validation.md`.

The historical validation established the following minimum `.lock.yml` behaviors:

- Activation, agent, detection, and safe output jobs remain separated in compiled workflows
- Agent jobs retain read-only permissions while write permissions remain isolated to safe output jobs
- Runtime repository/fork validation, timestamp validation, and action pinning remain present in compiled output
- Concurrency controls and threat-detection job placement are validated against current lock-file generation

The **2026-11-03 targeted check** confirms the ten-job structure in `.github/workflows/security-review.lock.yml`, but also records workflow-specific write permissions and an UNVERIFIED PM-08 claim. It does not update the historical full-validation grade.

## Next Steps

### For Security Review
1. Read the full specification: `security-architecture-spec.md`
2. Review the security guarantees (Section 3.2)
3. Examine the formal requirements (Sections 4-11)
4. Assess compliance testing requirements (Section 12)

### For Implementation
1. Determine target conformance class (Basic/Standard/Complete)
2. Review implementation requirements for chosen class
3. Study the reference implementation in gh-aw
4. Implement compliance tests (Section 12)
5. Generate conformance report

### For Integration
1. Understand the compilation model (Section 10)
2. Map security layers to target CI/CD platform
3. Implement equivalent sandbox mechanisms
4. Adapt network isolation to platform capabilities
5. Validate against compliance tests

### Spec Maintenance Tasks

| Task | Status | Notes |
|------|--------|-------|
| Add job dependency diagram to Appendix A | ✅ Done (2026-05-10) | Added to `security-architecture-spec.md` Appendix A |
| Add lock file validation checklist as Appendix G | ✅ Done (2026-05-10) | Added to `security-architecture-spec.md` as Appendix G; old Appendix G renamed to Appendix H |
| Document the pre_activation pattern in Section 7.6 | ✅ Done (2026-05-10) | Added Section 7.6.1 "Pre-Activation Pattern" with normative requirements PM-10a through PM-10d |
| Rerun validation report after Appendix A update | ✅ Done (2026-05-15) | Revalidated against `specs/security-architecture-spec-validation.md`; grade remains pass with job architecture, sanitization, permissions, and threat-detection mappings verified |
| Update summary to reflect v1.0.2 CTR-012 work | ✅ Done (2026-05-10) | Appendix count updated; security architecture remains at version 1.0.0 |
| Audit "Next Steps" for stale v1.0.0 tasks | ✅ Done (2026-05-10) | This table replaces the stale untracked list |
| Add spec-to-lock sync note for security summary consumers | ✅ Done (2026-06-25) | Added "Spec-to-Lock Sync (v1.0.0)" section mapping summary version to validated `.lock.yml` behaviors |
| Track pre_activation note from validation doc | ✅ Done (2026-07-06) | Added an explicit PM-11 note tying runtime membership validation to the separate `pre_activation` job in `specs/security-architecture-spec.md` |
| Track detection job naming note from validation doc | ✅ Done (2026-07-06) | Appendix D now names the `detection` job explicitly as the runtime threat-detection layer |
| Track conclusion job note from validation doc | ✅ Done (2026-07-06) | Documented the optional `conclusion` job as non-normative cleanup/reporting guidance |
| Audit trusted-users runtime enforcement coverage | ✅ Done (2026-07-06) | Sections 8-9 now document runtime `trusted-users` enforcement scope directly in this spec summary (membership checks gate privileged runtime access) |
| Add formal model and test suite for SG-01 through SG-07 | ✅ Done (2026-07-09) | Added "Formal Model" (TLA+/F*/Z3 invariants), "Behavioral Coverage Map" (15 predicates), and "Generated Test Suite" sections; 15 tests in `pkg/workflow/security_architecture_sg_formal_test.go` |
| Sync PM-11 formal coverage into behavioral coverage map | ✅ Done (2026-07-15) | Added `TestFormalPM11_PreActivationContainsMembershipStep` to the behavioral coverage map and generated suite notes; formal suite now tracks 16 tests in `pkg/workflow/security_architecture_sg_formal_test.go` |
| Sync §12 CS/RS coverage update from daily SPDD queue | ✅ Done (2026-07-28) | Added formal coverage map/test-suite entries for T-CS-001/002 and T-RS-003..008; synced to `specs/security-architecture-spec-validation.md` §12 matrix |
| Add RS-05a for workflow_dispatch + aw_context PR checkout | ✅ Done (2026-08-01) | Security threat review cleared; added RS-05a to §11.3 (repository scope, actor trust, parse resilience, ref isolation); validation §7a added with 9-test unit evidence; §12 matrix updated; spec bumped to v1.0.1 |
| Recheck compiled ten-job structure | ✅ Targeted check (2026-11-03) | `.github/workflows/security-review.lock.yml` confirms the ten jobs; full revalidation remains pending and PM-08 is UNVERIFIED in the validation report |

## Change Log

### 2026-11-03

- Added the seven REASONS headings with explicit safeguards, operational ordering, and normative-status qualifications.
- Reconciled the implementation and sync status with the historical full validation and the current targeted lock check.

### 2026-11-06

- Synced the current revision marker and added links from every formal requirement group to its entry in the full specification.

## Versioning

The specification follows **semantic versioning**:

- **Major**: Breaking changes, incompatible modifications
- **Minor**: New features, backward-compatible additions
- **Patch**: Bug fixes, clarifications, editorial changes

Historical summary revision: **1.0.1** (Candidate Recommendation). The architecture baseline is **1.0.0**; the later RS-05a updates, including v1.1.7, are recorded above.
The current full specification revision is **1.1.9**, published **November 5, 2026**.

## Feedback

For questions, feedback, or errata:

- **Repository**: https://github.com/github/gh-aw
- **Issues**: https://github.com/github/gh-aw/issues
- **Discussions**: https://github.com/github/gh-aw/discussions

## License

Copyright © 2026 GitHub, Inc.  
This specification is provided under the MIT License.
