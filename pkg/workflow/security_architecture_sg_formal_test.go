//go:build !integration

// Package workflow — security architecture SG formal tests.
//
// This file encodes the formal specification predicates (SG-01 through SG-07)
// plus eight supporting invariants for the gh-aw 7-layer security architecture
// defined in specs/security-architecture-spec-summary.md.
//
// Each predicate maps to exactly one Go test function as specified in the
// Behavioral Coverage Map in the spec summary:
//
//	SG01_InputSanitization          → TestFormalSG01_InputSanitizationInvariant
//	SG02_AgentReadOnly              → TestFormalSG02_AgentJobHasNoWritePermissions
//	SG03_NetworkAllowlist           → TestFormalSG03_NetworkAllowlistEnforcement
//	SG04_LeastPrivilege             → TestFormalSG04_LeastPrivilegeBasePermissions
//	SG05_SandboxIsolation           → TestFormalSG05_SandboxIsolationPresence
//	SG06_Auditability               → TestFormalSG06_ThreatDetectionAuditArtifact
//	SG07_FailSecure                 → TestFormalSG07_FailSecureOnSecurityError
//	T-CS-001 SchemaValidation       → TestFormalCS001_SchemaValidationRejectsUnknownField
//	T-CS-002 ExpressionSafety       → TestFormalCS002_ExpressionSafetyRejectsUnauthorizedExpression
//	BasicConformance                → TestFormalBasicConformance_AllFourControls
//	ThreatDetectionOrDefault        → TestFormalThreatDetection_EnabledByDefault
//	ThreatDetectionOrDefault        → TestFormalThreatDetection_ExplicitDisable
//	IsContinueOnError               → TestFormalThreatDetection_ContinueOnErrorDefault
//	PM11_PreActivationMembership    → TestFormalPM11_PreActivationContainsMembershipStep
//	StagedHandlerNoWritePerms       → TestFormalStaged_HandlerRequiresNoWritePerms
//	IDTokenRequirement              → TestFormalIDToken_OIDCVaultActionsRequireWriteScope
//	T-RS-003 WorkflowRunRepoCheck   → TestFormalRS003_WorkflowRunRepositoryValidation
//	T-RS-004 RuntimeRoleValidation  → TestFormalRS004_RuntimeRoleValidation
//	T-RS-005 RuntimeTokenValidation → TestFormalRS005_RuntimeTokenValidation
//	T-RS-006 AWFNetworkEnforcement  → TestFormalRS006_AWFNetworkPolicyValidation
//	T-RS-007 MCPNetworkEnforcement  → TestFormalRS007_MCPNetworkPolicyValidation
//	T-RS-008 OutputValidation       → TestFormalRS008_OutputTargetValidation
//	getPushFallbackAsPullRequest     → TestFormalPushFallback_DefaultsToTrue
//	JobTopologyOrder                → TestFormalJobTopology_PipelineOrderEnforced
//
// All tests call production code directly without stubs; no mocking is used.
// See specs/security-architecture-spec-summary.md §"Formal Model" for the
// TLA+ state-machine invariants these tests encode.
package workflow

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// formalEmptyPermissionsYAML is the minimal YAML fragment passed to
// WorkflowData.Permissions when a test needs a WorkflowData value without
// any meaningful top-level permissions block.  Using a shared constant avoids
// duplicating the same literal string across multiple test functions.
const formalEmptyPermissionsYAML = "permissions: {}"

// TestFormalSG01_InputSanitizationInvariant (SG01_InputSanitization)
//
// SG-01: Untrusted input must not be directly interpolated into GitHub Actions
// expressions without sanitization.
//
// Invariant (TLA+):
//
//	SG01 ≜ ∀ step ∈ WorkflowSteps :
//	  step.run ≠ nil ∧ ContainsExpression(step.run) ⟹ step.env ≠ nil
//
// sanitizeRunStepExpressions must move every ${{ … }} token from the run:
// field to the step's env: block.
func TestFormalSG01_InputSanitizationInvariant(t *testing.T) {
	unsafeStep := map[string]any{
		"run": "echo ${{ github.event.issue.title }}",
	}
	sanitized, descriptions, changed := sanitizeRunStepExpressions(unsafeStep)

	require.True(t, changed, "SG-01: run: step with ${{ }} expression must be sanitized")
	assert.NotEmpty(t, descriptions, "SG-01: at least one substitution description must be emitted")

	runVal, ok := sanitized["run"].(string)
	require.True(t, ok, "SG-01: sanitized run: field must remain a string")
	assert.NotContains(t, runVal, "${{",
		"SG-01: sanitized run: must not contain raw expression tokens — SG-01 invariant violated")

	_, hasEnv := sanitized["env"]
	assert.True(t, hasEnv,
		"SG-01: sanitized step must carry env: block containing the extracted expression")

	// A run: step that contains no expression must not be modified.
	cleanStep := map[string]any{"run": "echo hello"}
	_, _, cleanChanged := sanitizeRunStepExpressions(cleanStep)
	assert.False(t, cleanChanged, "SG-01: expression-free run: step must not be altered")
}

// TestFormalSG02_AgentJobHasNoWritePermissions (SG02_AgentReadOnly)
//
// SG-02: AI agents must have no direct write access.
//
// Invariant (F*):
//
//	val SG02 : ∀ (scope : PermissionScope) → scope ∉ {id-token, metadata} →
//	  validateDangerousPermissions (permissions {scope = write}) = Error
//
// validateDangerousPermissions must reject every write-capable scope on the
// agent job (id-token and metadata are excluded per their special semantics).
func TestFormalSG02_AgentJobHasNoWritePermissions(t *testing.T) {
	for _, scope := range GetAllPermissionScopes() {
		if scope == PermissionIdToken || scope == PermissionMetadata {
			continue
		}
		t.Run(string(scope), func(t *testing.T) {
			perms := NewPermissions()
			perms.Set(scope, PermissionWrite)
			err := validateDangerousPermissions(&WorkflowData{Permissions: formalEmptyPermissionsYAML}, perms)
			require.Error(t, err,
				"SG-02: agent job scope %s:write must be rejected by validateDangerousPermissions", scope)
			require.ErrorContains(t, err, "write permissions",
				"SG-02: error message must identify the write-permission violation")
		})
	}
}

// TestFormalSG03_NetworkAllowlistEnforcement (SG03_NetworkAllowlist)
//
// SG-03: Network access must be restricted to explicitly allowed domains.
// Blocked domains always take precedence over the allowed list.
//
// Invariant (TLA+):
//
//	SG03 ≜ ∀ domain ∈ NetworkPermissions.Blocked :
//	  domain ∈ GetBlockedDomains(NetworkPermissions)
//	  ∧ ¬(domain ∈ NetworkPermissions.Allowed ⟹ domain ∉ GetBlockedDomains)
//
// A domain listed in blocked must appear in GetBlockedDomains regardless of
// whether it also appears in the allowed list.
func TestFormalSG03_NetworkAllowlistEnforcement(t *testing.T) {
	// A domain that appears in both allowed and blocked lists must be blocked.
	net := &NetworkPermissions{
		Allowed: []string{"github.com", "evil.example.com"},
		Blocked: []string{"evil.example.com"},
	}
	blocked := GetBlockedDomains(net)
	assert.Contains(t, blocked, "evil.example.com",
		"SG-03: blocked domain must appear in GetBlockedDomains even when also in allowed list — precedence invariant violated")

	// Validate that the allowed-domain validator does not strip the domain from the block list.
	compiler := NewCompiler()
	err := compiler.validateNetworkAllowedDomains(net)
	require.NoError(t, err,
		"SG-03: allowed-domain validation must not fail when a blocked domain is also listed as allowed")

	// An empty blocked list must yield no blocked domains.
	emptyNet := &NetworkPermissions{Allowed: []string{"github.com"}}
	assert.Empty(t, GetBlockedDomains(emptyNet),
		"SG-03: network with no blocked domains must return an empty blocked-domain list")

	// Nil network must not produce blocked domains.
	assert.Empty(t, GetBlockedDomains(nil),
		"SG-03: nil network permissions must return an empty blocked-domain list")
}

// TestFormalSG04_LeastPrivilegeBasePermissions (SG04_LeastPrivilege)
//
// SG-04: Permissions must follow the principle of least privilege.
//
// Invariant (F*):
//
//	val SG04 : NewPermissions () = {} ∧
//	  validateDangerousPermissions (all-read) = Ok
//
// A freshly-created Permissions object must not contain any write grants, and
// an all-read configuration must pass validation.
func TestFormalSG04_LeastPrivilegeBasePermissions(t *testing.T) {
	// Default (empty) permissions must not contain any write grants.
	perms := NewPermissions()
	err := validateDangerousPermissions(&WorkflowData{Permissions: formalEmptyPermissionsYAML}, perms)
	require.NoError(t, err,
		"SG-04: default empty permissions must contain no write grants (least-privilege baseline)")

	// An all-read permission set must also be accepted.
	readAll := NewPermissions()
	for _, scope := range GetAllPermissionScopes() {
		if scope == PermissionIdToken {
			continue // id-token is write-or-absent, never read
		}
		readAll.Set(scope, PermissionRead)
	}
	err = validateDangerousPermissions(&WorkflowData{Permissions: formalEmptyPermissionsYAML}, readAll)
	require.NoError(t, err,
		"SG-04: an all-read permission set must be accepted for the agent job")
}

// TestFormalSG05_SandboxIsolationPresence (SG05_SandboxIsolation)
//
// SG-05: Agent processes must execute in isolated sandbox environments.
//
// Invariant (TLA+):
//
//	SG05 ≜ isSandboxEnabled(config, net) ⟺
//	  (config.Agent.Type = AWF ∧ ¬config.Agent.Disabled) ∨ net.Firewall.Enabled
//
// isSandboxEnabled must return true for approved sandbox configurations and
// false when the sandbox is explicitly disabled or absent.
func TestFormalSG05_SandboxIsolationPresence(t *testing.T) {
	// An explicit AWF sandbox must be enabled.
	awfCfg := &SandboxConfig{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF}}
	assert.True(t, isSandboxEnabled(awfCfg, nil),
		"SG-05: AWF sandbox must be enabled when agent.type=awf")

	// An explicitly disabled sandbox must not be enabled.
	disabledCfg := &SandboxConfig{Agent: &AgentSandboxConfig{Disabled: true}}
	assert.False(t, isSandboxEnabled(disabledCfg, nil),
		"SG-05: sandbox must not be enabled when agent.disabled=true")

	// A firewall-enabled network must auto-enable the AWF sandbox.
	firewallNet := &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}
	assert.True(t, isSandboxEnabled(nil, firewallNet),
		"SG-05: AWF firewall must auto-enable the sandbox")

	// Nil sandbox and nil network must not be treated as sandbox-enabled.
	assert.False(t, isSandboxEnabled(nil, nil),
		"SG-05: absence of sandbox configuration must not be treated as sandbox-enabled")
}

// TestFormalSG06_ThreatDetectionAuditArtifact (SG06_Auditability)
//
// SG-06: All actions must produce auditable artifacts.
// Threat detection produces an auditable detection job when enabled.
//
// Invariant (TLA+):
//
//	SG06 ≜ ThreatDetection ≠ nil ⟹ ∃ job ∈ CompiledJobs : job.name = "detection"
//
// When safe-outputs are configured (threat detection is enabled by default),
// the compiled workflow must include a detection job as the audit artifact.
func TestFormalSG06_ThreatDetectionAuditArtifact(t *testing.T) {
	md := `---
name: sg06-audit-test
on: push
engine: copilot
permissions:
  contents: read
safe-outputs:
  create-issue:
---

# Mission

SG-06 audit artifact test: verify the detection job is compiled when threat detection is active.
`
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(md), 0600))

	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowFile(mdPath)
	require.NoError(t, err, "SG-06: workflow with safe-outputs must parse without error")

	yamlOut, err := compiler.CompileToYAML(wd, mdPath)
	require.NoError(t, err, "SG-06: workflow with default threat detection must compile successfully")
	require.NotEmpty(t, yamlOut, "SG-06: compiled YAML must not be empty")

	// The compiled YAML must include a detection job as the auditable artifact.
	assert.Contains(t, yamlOut, string(constants.DetectionJobName)+":",
		"SG-06: compiled workflow must contain a %q job — threat detection audit artifact is missing",
		constants.DetectionJobName)
}

// TestFormalSG07_FailSecureOnSecurityError (SG07_FailSecure)
//
// SG-07: Security violations must prevent workflow execution rather than
// allowing degraded operation. CompileToYAML must return ("", error) when a
// security violation is detected.
//
// Invariant (F*):
//
//	val SG07 : ∀ (wd : WorkflowData) →
//	  DangerousPermissions wd ⟹ CompileToYAML wd = ("", Error)
func TestFormalSG07_FailSecureOnSecurityError(t *testing.T) {
	md := `---
name: sg07-fail-secure-test
on: push
engine: copilot
strict: false
permissions:
  contents: write
---

# Mission

SG-07: verify that a write-permission violation blocks lock-file emission.
`
	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowString(md, "workflow.md")
	require.NoError(t, err,
		"SG-07: ParseWorkflowString must succeed before the compilation-time security check runs")

	yamlOut, err := compiler.CompileToYAML(wd, "workflow.md")
	require.Error(t, err,
		"SG-07: CompileToYAML must return an error when a write-permission violation is present")
	assert.Empty(t, yamlOut,
		"SG-07: CompileToYAML must return empty YAML — no lock-file may be emitted on security violation")
	require.ErrorContains(t, err, "write permissions",
		"SG-07: error must identify the write-permission violation")
}

// TestFormalCS001_SchemaValidationRejectsUnknownField (T-CS-001)
//
// T-CS-001: Verify schema validation enforcement.
func TestFormalCS001_SchemaValidationRejectsUnknownField(t *testing.T) {
	md := `---
name: cs001-schema-validation
on: push
engine: copilot
permissions:
  contents: read
unknown-top-level-field: true
---

# Mission

T-CS-001 schema validation enforcement.
`
	compiler := NewCompiler(WithNoEmit(true))
	_, err := compiler.ParseWorkflowString(md, "workflow.md")
	require.Error(t, err, "T-CS-001: unknown frontmatter fields must fail schema validation")
	require.ErrorContains(t, err, "Unknown property",
		"T-CS-001: error message must identify schema-level unknown property validation")
}

// TestFormalCS002_ExpressionSafetyRejectsUnauthorizedExpression (T-CS-002)
//
// T-CS-002: Verify expression safety checks.
func TestFormalCS002_ExpressionSafetyRejectsUnauthorizedExpression(t *testing.T) {
	md := `---
name: cs002-expression-safety
on: push
engine: copilot
permissions:
  contents: read
---

# Mission

This markdown intentionally uses an unauthorized expression:
${{ github.event.issue.body }}
`
	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowString(md, "workflow.md")
	require.NoError(t, err, "T-CS-002: schema parsing should succeed before expression safety validation")

	yamlOut, err := compiler.CompileToYAML(wd, "workflow.md")
	require.Error(t, err, "T-CS-002: unauthorized expressions must fail compilation")
	assert.Empty(t, yamlOut, "T-CS-002: expression safety failure must block lock-file emission")
	require.ErrorContains(t, err, "unauthorized expressions found",
		"T-CS-002: error must report unauthorized expression detection")
}

// TestFormalBasicConformance_AllFourControls (BasicConformance)
//
// Basic Conformance (Level 1) requires all four core security controls to be
// present and exercisable:
//
//  1. Input sanitization  — sanitizeRunStepExpressions rewrites run: expressions.
//  2. Output isolation    — safe_outputs job appears in compiled workflows.
//  3. Permission mgmt     — validateDangerousPermissions rejects write grants.
//  4. Compilation checks  — CompileToYAML errors block lock-file emission.
//
// Invariant (TLA+):
//
//	BasicConformance ≜
//	  SG01_InputSanitization ∧ OutputIsolation ∧ SG04_LeastPrivilege ∧ SG07_FailSecure
func TestFormalBasicConformance_AllFourControls(t *testing.T) {
	// Control 1: Input sanitization.
	step := map[string]any{"run": "echo ${{ github.actor }}"}
	sanitized, _, changed := sanitizeRunStepExpressions(step)
	assert.True(t, changed,
		"BasicConformance[1]: input sanitization control must rewrite run: expressions")
	_, hasEnv := sanitized["env"]
	assert.True(t, hasEnv,
		"BasicConformance[1]: sanitized step must carry an env: block")

	// Control 2: Output isolation — safe_outputs job present in compiled workflow.
	md := `---
name: basic-conformance-test
on: push
engine: copilot
permissions:
  contents: read
safe-outputs:
  create-issue:
---

# Mission

Basic conformance: verify output isolation (safe_outputs job).
`
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(md), 0600))

	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowFile(mdPath)
	require.NoError(t, err)

	yamlOut, err := compiler.CompileToYAML(wd, mdPath)
	require.NoError(t, err, "BasicConformance[2]: safe-outputs workflow must compile without error")
	require.NotEmpty(t, yamlOut)

	assert.Contains(t, yamlOut, string(constants.SafeOutputsJobName)+":",
		"BasicConformance[2]: compiled workflow must contain a %q job (output isolation)",
		constants.SafeOutputsJobName)

	// Control 3: Permission management — write grants rejected.
	perms := NewPermissions()
	perms.Set(PermissionContents, PermissionWrite)
	err = validateDangerousPermissions(&WorkflowData{Permissions: formalEmptyPermissionsYAML}, perms)
	require.Error(t, err,
		"BasicConformance[3]: permission management control must reject write grants")

	// Control 4: Compilation-time checks — dangerous config blocks lock-file emission.
	mdDangerous := `---
name: basic-conformance-dangerous
on: push
engine: copilot
strict: false
permissions:
  issues: write
---

# Mission

Basic conformance: compilation-time check.
`
	compiler2 := NewCompiler(WithNoEmit(true))
	wd2, err := compiler2.ParseWorkflowString(mdDangerous, "workflow.md")
	require.NoError(t, err)
	yamlDangerous, err := compiler2.CompileToYAML(wd2, "workflow.md")
	require.Error(t, err,
		"BasicConformance[4]: compilation-time check must reject dangerous permissions before emitting lock-file")
	assert.Empty(t, yamlDangerous,
		"BasicConformance[4]: no YAML must be emitted when compilation-time check fails")
}

// TestFormalThreatDetection_EnabledByDefault (ThreatDetectionOrDefault — enabled)
//
// Invariant: when safe-outputs are configured and threat-detection is not
// explicitly mentioned, the compiler auto-injects a ThreatDetectionConfig
// (the returned value must be non-nil).
//
//	ThreatDetectionOrDefault(outputMap) ≜
//	  "threat-detection" ∉ outputMap.keys ⟹ parseThreatDetectionConfig(outputMap) ≠ nil
func TestFormalThreatDetection_EnabledByDefault(t *testing.T) {
	c := NewCompiler()

	// A safe-outputs map with no threat-detection key must produce a non-nil config.
	outputMap := map[string]any{
		"create-issue": map[string]any{},
	}
	td := c.parseThreatDetectionConfig(outputMap)
	require.NotNil(t, td,
		"ThreatDetectionOrDefault: parseThreatDetectionConfig must return non-nil when threat-detection key is absent")
}

// TestFormalThreatDetection_ExplicitDisable (ThreatDetectionOrDefault — disabled)
//
// Invariant: when threat-detection is explicitly set to false, the returned
// config must be nil (detection is disabled).
//
//	ThreatDetectionOrDefault({"threat-detection": false}) = nil
func TestFormalThreatDetection_ExplicitDisable(t *testing.T) {
	c := NewCompiler()

	outputMap := map[string]any{
		"threat-detection": false,
		"create-issue":     map[string]any{},
	}
	td := c.parseThreatDetectionConfig(outputMap)
	assert.Nil(t, td,
		"ThreatDetectionOrDefault: parseThreatDetectionConfig must return nil when threat-detection is explicitly false")
}

// TestFormalThreatDetection_ContinueOnErrorDefault (IsContinueOnError)
//
// Invariant: IsContinueOnError must default to true when the field is nil
// (detection failures produce warnings, not errors, by default).
//
//	IsContinueOnError({ContinueOnError: nil}) = true
func TestFormalThreatDetection_ContinueOnErrorDefault(t *testing.T) {
	// Nil ContinueOnError field must default to true.
	td := &ThreatDetectionConfig{}
	assert.True(t, td.IsContinueOnError(),
		"IsContinueOnError: must return true when ContinueOnError field is nil (default: continue)")

	// An explicitly-true value must also return true.
	trueVal := true
	tdExplicitTrue := &ThreatDetectionConfig{ContinueOnError: &trueVal}
	assert.True(t, tdExplicitTrue.IsContinueOnError(),
		"IsContinueOnError: must return true when ContinueOnError is explicitly set to true")

	// An explicitly-false value must return false (blocking mode).
	falseVal := false
	tdExplicitFalse := &ThreatDetectionConfig{ContinueOnError: &falseVal}
	assert.False(t, tdExplicitFalse.IsContinueOnError(),
		"IsContinueOnError: must return false when ContinueOnError is explicitly set to false (blocking mode)")
}

// TestFormalPM11_PreActivationContainsMembershipStep (PM11_PreActivationMembership)
//
// PM-11: Role checks MUST be performed at runtime using membership validation in
// the pre_activation job before activation may proceed.
func TestFormalPM11_PreActivationContainsMembershipStep(t *testing.T) {
	md := `---
name: pm11-membership-test
on:
  pull_request:
    types: [opened]
  roles:
    - write
engine: copilot
permissions:
  contents: read
---

# Mission

PM-11: verify the compiled pre_activation job contains the membership-check step.
`
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(md), 0600))

	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowFile(mdPath)
	require.NoError(t, err, "PM-11: workflow with role-based access control must parse successfully")

	yamlOut, err := compiler.CompileToYAML(wd, mdPath)
	require.NoError(t, err, "PM-11: workflow with role-based access control must compile successfully")
	require.NotEmpty(t, yamlOut, "PM-11: compiled YAML must not be empty")

	preActivationSection := extractJobSection(yamlOut, string(constants.PreActivationJobName))
	require.NotEmpty(t, preActivationSection,
		"PM-11: compiled workflow must contain the pre_activation job")
	assert.Contains(t, preActivationSection, "id: "+string(constants.CheckMembershipStepID),
		"PM-11: pre_activation job must contain the check_membership step ID")
	assert.Contains(t, preActivationSection, "check_membership.cjs",
		"PM-11: pre_activation job must invoke check_membership.cjs for runtime membership validation")
	assert.Contains(t, preActivationSection, `GH_AW_REQUIRED_ROLES: "write"`,
		"PM-11: pre_activation job must pass the configured role set to the membership check step")
}

// TestFormalStaged_HandlerRequiresNoWritePerms (StagedHandlerNoWritePerms)
//
// Invariant: staged safe-output handlers must not produce write-permission
// grants. ComputePermissionsForSafeOutputs for a fully-staged config must
// return an empty (or nil) permission set.
//
//	StagedHandlerNoWritePerms ≜
//	  isHandlerStaged(true, _) ⟹ PermissionBuilder(_) = nil
func TestFormalStaged_HandlerRequiresNoWritePerms(t *testing.T) {
	// isHandlerStaged must return true when the global staged flag is set.
	assert.True(t, isHandlerStaged(true, nil),
		"StagedHandlerNoWritePerms: globally staged handler must report staged=true")

	// A TemplatableBool set to "true" on the handler must also be staged.
	trueVal := TemplatableBool("true")
	assert.True(t, isHandlerStaged(false, &trueVal),
		"StagedHandlerNoWritePerms: per-handler staged=true must report staged=true")

	// When the global safe-outputs config is staged, ComputePermissionsForSafeOutputs
	// must not accumulate any write grants.
	trueValBool := TemplatableBool("true")
	stagedConfig := &SafeOutputsConfig{
		Staged:       &trueValBool,
		CreateIssues: &CreateIssuesConfig{},
	}
	perms := ComputePermissionsForSafeOutputs(stagedConfig)
	require.NotNil(t, perms)
	for _, scope := range GetAllPermissionScopes() {
		val, exists := perms.Get(scope)
		if exists {
			assert.NotEqual(t, PermissionWrite, val,
				"StagedHandlerNoWritePerms: staged create-issue must not grant %s:write", scope)
		}
	}
}

// TestFormalIDToken_OIDCVaultActionsRequireWriteScope (IDTokenRequirement)
//
// Invariant: steps that use a known OIDC/vault action must trigger
// id-token:write — stepsRequireIDToken returns true.
//
//	IDTokenRequirement ≜
//	  ∃ step ∈ steps : step.uses ∈ oidcVaultActions ⟹ stepsRequireIDToken(steps) = true
func TestFormalIDToken_OIDCVaultActionsRequireWriteScope(t *testing.T) {
	// Each known OIDC vault action must trigger id-token:write detection.
	oidcActions := []string{
		"aws-actions/configure-aws-credentials",
		"azure/login",
		"google-github-actions/auth",
		"hashicorp/vault-action",
		"cyberark/conjur-action",
	}

	for _, action := range oidcActions {
		t.Run(action, func(t *testing.T) {
			steps := []any{
				map[string]any{"uses": action + "@v2"},
			}
			assert.True(t, stepsRequireIDToken(steps),
				"IDTokenRequirement: OIDC vault action %q must trigger stepsRequireIDToken=true", action)
		})
	}

	// A step that uses a non-OIDC action must not trigger id-token:write.
	plainSteps := []any{
		map[string]any{"uses": "actions/checkout@abc123"},
	}
	assert.False(t, stepsRequireIDToken(plainSteps),
		"IDTokenRequirement: non-OIDC action must not trigger stepsRequireIDToken=true")

	// An empty steps list must return false.
	assert.False(t, stepsRequireIDToken(nil),
		"IDTokenRequirement: nil steps must return false")
}

// TestFormalRS003_WorkflowRunRepositoryValidation (T-RS-003)
//
// T-RS-003: Verify repository validation for workflow_run.
func TestFormalRS003_WorkflowRunRepositoryValidation(t *testing.T) {
	md := `---
name: rs003-workflow-run-repo-validation
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
    branches: [main]
engine: copilot
permissions:
  contents: read
---

# Mission

T-RS-003 repository validation for workflow_run.
`
	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowString(md, "workflow.md")
	require.NoError(t, err)

	yamlOut, err := compiler.CompileToYAML(wd, "workflow.md")
	require.NoError(t, err)
	assert.Contains(t, yamlOut, "github.event_name != 'workflow_run'",
		"T-RS-003: compiled workflow_run trigger must short-circuit safely for non-workflow_run events")
	assert.Contains(t, yamlOut, "github.event.workflow_run != null",
		"T-RS-003: compiled workflow_run trigger must guard workflow_run payload access")
	assert.Contains(t, yamlOut, "github.event.workflow_run.repository != null",
		"T-RS-003: compiled workflow_run trigger must fail closed when repository metadata is absent")
	assert.Contains(t, yamlOut, "github.event.workflow_run.repository.id == github.repository_id",
		"T-RS-003: compiled workflow_run trigger must include repository ID safety check")
	assert.Contains(t, yamlOut, "!(github.event.workflow_run.repository.fork)",
		"T-RS-003: compiled workflow_run trigger must reject forked upstream workflow runs")
}

// TestFormalRS004_RuntimeRoleValidation (T-RS-004)
//
// T-RS-004: Verify role validation.
func TestFormalRS004_RuntimeRoleValidation(t *testing.T) {
	md := `---
name: rs004-role-validation
on:
  pull_request:
    types: [opened]
  roles:
    - maintainer
engine: copilot
permissions:
  contents: read
---

# Mission

T-RS-004 runtime role validation.
`
	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowString(md, "workflow.md")
	require.NoError(t, err)

	yamlOut, err := compiler.CompileToYAML(wd, "workflow.md")
	require.NoError(t, err)
	assert.Contains(t, yamlOut, "id: check_membership",
		"T-RS-004: role-gated workflows must include runtime membership validation")
	assert.Contains(t, yamlOut, "GH_AW_REQUIRED_ROLES:",
		"T-RS-004: runtime membership validation must include the required roles environment input")
}

// TestFormalRS005_RuntimeTokenValidation (T-RS-005)
//
// T-RS-005: Verify token validation.
func TestFormalRS005_RuntimeTokenValidation(t *testing.T) {
	md := `---
name: rs005-token-validation
on:
  pull_request:
    types: [opened]
  roles:
    - write
engine: copilot
permissions:
  contents: read
tools:
  github:
    github-token: ${{ secrets.CUSTOM_PAT }}
---

# Mission

T-RS-005 runtime token validation.
`
	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowString(md, "workflow.md")
	require.NoError(t, err)

	yamlOut, err := compiler.CompileToYAML(wd, "workflow.md")
	require.NoError(t, err)

	preActivation := extractJobSection(yamlOut, string(constants.PreActivationJobName))
	require.NotEmpty(t, preActivation)
	assert.Contains(t, preActivation, "github-token: ${{ secrets.GITHUB_TOKEN }}",
		"T-RS-005: runtime membership/token validation must use the repository GITHUB_TOKEN")
	assert.NotContains(t, preActivation, "CUSTOM_PAT",
		"T-RS-005: runtime role/token checks must not use custom GitHub tool tokens")
}

// TestFormalRS006_AWFNetworkPolicyValidation (T-RS-006)
//
// T-RS-006: Verify AWF network enforcement.
func TestFormalRS006_AWFNetworkPolicyValidation(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateStrictNetwork(&NetworkPermissions{Allowed: []string{"*"}})
	require.Error(t, err, "T-RS-006: wildcard network access must be rejected by strict network enforcement")
	require.ErrorContains(t, err, "wildcard",
		"T-RS-006: strict network enforcement must identify unrestricted wildcard access")
}

// TestFormalRS007_MCPNetworkPolicyValidation (T-RS-007)
//
// T-RS-007: Verify MCP network enforcement.
func TestFormalRS007_MCPNetworkPolicyValidation(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"mcp-servers": map[string]any{
			"custom": map[string]any{
				"type":      "stdio",
				"container": "ghcr.io/example/mcp-server:latest",
			},
		},
	}
	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	require.Error(t, err, "T-RS-007: strict mode must require top-level network rules for containerized MCP servers")
	require.ErrorContains(t, err, "top-level network configuration",
		"T-RS-007: MCP network enforcement error must explain the missing top-level network requirement")
}

// TestFormalRS008_OutputTargetValidation (T-RS-008)
//
// T-RS-008: Verify output validation.
func TestFormalRS008_OutputTargetValidation(t *testing.T) {
	config := &SafeOutputsConfig{
		CloseIssues: &CloseIssuesConfig{
			SafeOutputTargetConfig: SafeOutputTargetConfig{
				Target: "invalid-target",
			},
		},
	}
	err := validateSafeOutputsTarget(config)
	require.Error(t, err, "T-RS-008: invalid safe-output targets must fail validation")
	require.ErrorContains(t, err, "invalid target value",
		"T-RS-008: output validation must report invalid target values")
}

// TestFormalPushFallback_DefaultsToTrue (getPushFallbackAsPullRequest)
//
// Invariant: getPushFallbackAsPullRequest defaults to true when the config is nil
// or when FallbackAsPullRequest is unset.
//
//	getPushFallbackAsPullRequest(nil) = true
//	getPushFallbackAsPullRequest({FallbackAsPullRequest: nil}) = true
func TestFormalPushFallback_DefaultsToTrue(t *testing.T) {
	// Nil config must default to true.
	assert.True(t, getPushFallbackAsPullRequest(nil),
		"getPushFallbackAsPullRequest: nil config must default to true")

	// Config with nil FallbackAsPullRequest must also default to true.
	assert.True(t, getPushFallbackAsPullRequest(&PushToPullRequestBranchConfig{}),
		"getPushFallbackAsPullRequest: config with nil FallbackAsPullRequest must default to true")

	// Explicit false must be respected.
	falseVal := false
	assert.False(t, getPushFallbackAsPullRequest(&PushToPullRequestBranchConfig{FallbackAsPullRequest: &falseVal}),
		"getPushFallbackAsPullRequest: explicit false must be returned as false")

	// Explicit true must be respected.
	trueVal := true
	assert.True(t, getPushFallbackAsPullRequest(&PushToPullRequestBranchConfig{FallbackAsPullRequest: &trueVal}),
		"getPushFallbackAsPullRequest: explicit true must be returned as true")
}

// TestFormalJobTopology_PipelineOrderEnforced (JobTopologyOrder)
//
// SG-02/SG-05/SG-06: The compiled job pipeline must preserve the canonical
// dependency order: pre_activation → activation → agent → detection →
// safe_outputs (→ conclusion, when present).
//
// Invariant (TLA+):
//
//	JobTopologyOrder ≜ formalJobOrderValid(JobNames(CompiledYAML))
//
// This test compiles a real workflow with safe-outputs (which injects the
// detection job) and verifies the section order in the produced YAML.
func TestFormalJobTopology_PipelineOrderEnforced(t *testing.T) {
	md := `---
name: job-topology-test
on: push
engine: copilot
permissions:
  contents: read
safe-outputs:
  create-issue:
---

# Mission

JobTopologyOrder: verify compiled job pipeline preserves canonical order.
`
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(md), 0600))

	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowFile(mdPath)
	require.NoError(t, err, "JobTopologyOrder: workflow must parse without error")

	yamlOut, err := compiler.CompileToYAML(wd, mdPath)
	require.NoError(t, err, "JobTopologyOrder: workflow must compile without error")
	require.NotEmpty(t, yamlOut)

	// Extract per-job sections using the file-local helper defined in
	// security_architecture_formal_test.go (same package, same build tag).
	sections := formalJobSections(yamlOut)

	// Collect the order in which job sections appear.
	type jobPos struct {
		name string
		line int
	}
	var positions []jobPos
	for _, line := range []struct{ name string }{
		{string(constants.PreActivationJobName)},
		{string(constants.ActivationJobName)},
		{string(constants.AgentJobName)},
		{string(constants.DetectionJobName)},
		{string(constants.SafeOutputsJobName)},
	} {
		if _, ok := sections[line.name]; !ok {
			continue
		}
		// Find the byte-offset of the job's section header in the YAML.
		needle := line.name + ":"
		idx := strings.Index(yamlOut, "\n  "+needle)
		if idx < 0 {
			idx = strings.Index(yamlOut, needle)
		}
		// A job present in sections but absent as a header means the YAML is
		// malformed; fail immediately so ordering checks don't silently pass
		// on stale or bogus positions.
		require.GreaterOrEqual(t, idx, 0,
			"JobTopologyOrder: job %q found in sections map but its header is missing from compiled YAML", line.name)
		positions = append(positions, jobPos{name: line.name, line: idx})
	}

	// Sort by YAML byte-offset so foundOrder reflects the actual order jobs
	// appear in the compiled output, not the canonical iteration order above.
	sort.Slice(positions, func(i, j int) bool { return positions[i].line < positions[j].line })

	// Verify that every expected job is present.
	expectedJobs := []string{
		string(constants.PreActivationJobName),
		string(constants.ActivationJobName),
		string(constants.AgentJobName),
		string(constants.DetectionJobName),
		string(constants.SafeOutputsJobName),
	}
	for _, name := range expectedJobs {
		_, present := sections[name]
		assert.True(t, present,
			"JobTopologyOrder: compiled workflow must contain job %q", name)
	}

	// Build foundOrder in YAML appearance order (used for diagnostic messages).
	var foundOrder []string
	for _, p := range positions {
		foundOrder = append(foundOrder, p.name)
	}

	// Verify the canonical needs-based dependency chain.  In GitHub Actions,
	// execution order is determined by the needs: graph, not YAML section
	// position, so each job must declare its direct predecessor as a dependency.
	// Compiled YAML uses either the scalar form ("needs: <name>") or a list
	// item ("- <name>") inside a multi-value needs: block.
	type dependencyEdge struct{ job, predecessor string }
	edges := []dependencyEdge{
		{string(constants.ActivationJobName), string(constants.PreActivationJobName)},
		{string(constants.AgentJobName), string(constants.ActivationJobName)},
		{string(constants.DetectionJobName), string(constants.AgentJobName)},
		{string(constants.SafeOutputsJobName), string(constants.DetectionJobName)},
	}
	for _, e := range edges {
		sec, ok := sections[e.job]
		if !ok {
			continue // already reported as missing by the presence loop above
		}
		hasNeedsDep := strings.Contains(sec, "needs: "+e.predecessor) ||
			strings.Contains(sec, "- "+e.predecessor)
		assert.True(t, hasNeedsDep,
			"JobTopologyOrder: %q must declare %q as a direct needs dependency "+
				"to enforce the canonical pipeline order (actual YAML section order: %v)",
			e.job, e.predecessor, foundOrder)
	}
}
