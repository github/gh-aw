//go:build !integration

// Package workflow - security architecture formal model tests.
//
// This file encodes the formal specification predicates (P1–P10) for the
// gh-aw 7-layer security architecture defined in
// specs/security-architecture-spec-summary.md.
//
// Each predicate is mapped to a Go test function:
//
//	P1  InputNotDirectlyInterpolated  → TestFormal_P1_InputSanitizationRequired
//	P2  NoDirectAgentWrite            → TestFormal_P2_AgentHasNoWritePermissions
//	P3  NetworkRestricted             → TestFormal_P3_NetworkDomainAllowlist
//	P4  LeastPrivilege                → TestFormal_P4_DefaultPermissionsMinimal
//	P5  AgentSandboxed                → TestFormal_P5_AgentMustRunInSandbox
//	P6  FailSecure                    → TestFormal_P6_SecurityFailureHaltsExecution
//	P7  Monotonicity                  → TestFormal_P7_ConformanceLevelMonotonicity
//	P8  JobOrder                      → TestFormal_P8_JobDependencyChainOrder
//	P9  CompileValidates              → TestFormal_P9_CompilationValidatesBeforeEmit
//	P10 TokenIsolation                → TestFormal_P10_WriteTokenIsolatedToSafeOutput
//
// Most predicates exercise production Go code directly via the public compiler
// API (ParseWorkflowString, ParseWorkflowFile, CompileToYAML) or internal
// validators.  P7 and P8 encode spec invariants that have no single production
// call site; they use file-local formal helpers instead.
package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// formalConformanceLevel is a typed integer representing a spec conformance class.
// Basic=1, Standard=2, Complete=3 (spec Section 2).
type formalConformanceLevel int

const (
	formalConformanceLevelBasic    formalConformanceLevel = 1
	formalConformanceLevelStandard formalConformanceLevel = 2
	formalConformanceLevelComplete formalConformanceLevel = 3
)

// formalConformanceMonotonicity checks the spec invariant:
// Complete >= Standard >= Basic.
func formalConformanceMonotonicity(basic, standard, complete formalConformanceLevel) bool {
	return complete >= standard && standard >= basic
}

// formalJobOrderValid checks that every pair of consecutive canonical job names
// appears in the correct order within the supplied slice.
// It only enforces ordering for names that are both present in the slice.
func formalJobOrderValid(order []string) bool {
	canonical := []string{
		string(constants.PreActivationJobName),
		string(constants.ActivationJobName),
		string(constants.AgentJobName),
		string(constants.DetectionJobName),
		string(constants.SafeOutputsJobName),
		string(constants.ConclusionJobName),
	}
	idx := make(map[string]int, len(order))
	for i, name := range order {
		idx[name] = i
	}
	for i := 1; i < len(canonical); i++ {
		a, okA := idx[canonical[i-1]]
		b, okB := idx[canonical[i]]
		if okA && okB && a >= b {
			return false
		}
	}
	return true
}

// formalJobSections parses a compiled GitHub Actions YAML string and returns
// the content of the named jobs as separate strings.  Jobs are YAML keys at
// the 2-space indent level inside the top-level `jobs:` block.
func formalJobSections(yamlContent string) map[string]string {
	// jobKeyIndent is the number of leading spaces that identifies a top-level
	// job key inside the `jobs:` block (e.g. "  agent:").
	const jobKeyIndent = 2

	lines := strings.Split(yamlContent, "\n")
	type boundary struct{ start, end int }

	var order []string
	bounds := map[string]boundary{}
	inJobs := false

	for i, line := range lines {
		if strings.TrimSpace(line) == "jobs:" {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		// A job key is indented by exactly jobKeyIndent spaces and contains no
		// interior spaces before the trailing colon
		// (e.g. "  agent:" not "    steps:").
		if len(line) > jobKeyIndent && line[0] == ' ' && line[1] == ' ' && line[jobKeyIndent] != ' ' {
			trimmed := strings.TrimSpace(line)
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(strings.TrimSuffix(trimmed, ":"), " ") {
				name := strings.TrimSuffix(trimmed, ":")
				order = append(order, name)
				bounds[name] = boundary{start: i}
			}
		}
	}

	// Close each section at the line before the next job key (or EOF).
	for i, name := range order {
		b := bounds[name]
		if i+1 < len(order) {
			b.end = bounds[order[i+1]].start - 1
		} else {
			b.end = len(lines) - 1
		}
		bounds[name] = b
	}

	result := make(map[string]string, len(order))
	for name, b := range bounds {
		result[name] = strings.Join(lines[b.start:b.end+1], "\n")
	}
	return result
}

// TestFormal_P1_InputSanitizationRequired (P1 InputNotDirectlyInterpolated)
//
// SG-01: Untrusted input must not be directly interpolated into GitHub Actions
// expressions without sanitization.  sanitizeRunStepExpressions must extract
// every ${{ … }} occurrence from a run: field into the step's env: block.
func TestFormal_P1_InputSanitizationRequired(t *testing.T) {
	// A run: step that contains a GitHub Actions expression must be rewritten.
	unsafeStep := map[string]any{
		"run": "echo ${{ github.event.issue.title }}",
	}
	sanitized, descriptions, changed := sanitizeRunStepExpressions(unsafeStep)
	assert.True(t, changed, "expression in run: must be extracted to env: to prevent template injection")
	assert.NotEmpty(t, descriptions, "at least one substitution description must be emitted")

	// The sanitized step must still have a string run: field (sanitization must not break
	// the step shape) and must have extracted the expression into an env: block.
	runVal, ok := sanitized["run"].(string)
	require.True(t, ok, "sanitized run: field must remain a string after sanitization")
	assert.NotContains(t, runVal, "${{", "sanitized run: field must not contain raw ${{ }} expression")
	_, hasEnv := sanitized["env"]
	assert.True(t, hasEnv, "sanitized step must carry an env: block containing the extracted expression")

	// A run: step without any expression must not be modified.
	cleanStep := map[string]any{
		"run": "echo hello",
	}
	_, _, cleanChanged := sanitizeRunStepExpressions(cleanStep)
	assert.False(t, cleanChanged, "run: step without expressions must not be modified")
}

// TestFormal_P2_AgentHasNoWritePermissions (P2 NoDirectAgentWrite)
//
// SG-02: AI agents must have no direct write access.  validateDangerousPermissions
// must reject every write-capable scope on the agent job.
func TestFormal_P2_AgentHasNoWritePermissions(t *testing.T) {
	for _, scope := range GetAllPermissionScopes() {
		// id-token is used for OIDC authentication and does not grant repo write access.
		// metadata is implicitly read-only and excluded from the rejection rule.
		if scope == PermissionIdToken || scope == PermissionMetadata {
			continue
		}
		t.Run(string(scope), func(t *testing.T) {
			perms := NewPermissions()
			perms.Set(scope, PermissionWrite)
			err := validateDangerousPermissions(&WorkflowData{Permissions: "permissions: {}"}, perms)
			require.Error(t, err, "agent job scope %s:write must be rejected", scope)
			require.ErrorContains(t, err, "write permissions")
		})
	}
}

// TestFormal_P3_NetworkDomainAllowlist (P3 NetworkRestricted)
//
// SG-03: Network access must be restricted to explicitly allowed domains.
// validateNetworkAllowedDomains must accept valid domain lists, and
// validateStrictNetwork must reject wildcard-only allowlists.
func TestFormal_P3_NetworkDomainAllowlist(t *testing.T) {
	compiler := NewCompiler()

	// An explicit allowlist of valid domains must be accepted.
	validNet := &NetworkPermissions{Allowed: []string{"github.com", "api.github.com"}}
	require.NoError(t, compiler.validateNetworkAllowedDomains(validNet),
		"explicit allowlist of valid domains must be accepted")

	// A wildcard-only allowlist must be rejected in strict mode (CTR-011).
	err := compiler.validateStrictNetwork(&NetworkPermissions{Allowed: []string{"*"}})
	require.Error(t, err, "wildcard-only allowlist must be rejected in strict mode")
	require.ErrorContains(t, err, "wildcard")

	// An empty network permission set must not cause a validation error.
	require.NoError(t, compiler.validateNetworkAllowedDomains(nil),
		"nil network permissions must not fail allowlist validation")
}

// TestFormal_P4_DefaultPermissionsMinimal (P4 LeastPrivilege)
//
// SG-04: Permissions must follow the principle of least privilege.  A freshly
// created Permissions object must grant no write access, and an all-read set
// must also be accepted.
func TestFormal_P4_DefaultPermissionsMinimal(t *testing.T) {
	// Default (empty) permissions must contain no write grants.
	perms := NewPermissions()
	err := validateDangerousPermissions(&WorkflowData{Permissions: "permissions: {}"}, perms)
	require.NoError(t, err, "default (empty) permissions must contain no write grants")

	// All-read permissions must also pass validation.
	readAllPerms := NewPermissions()
	for _, scope := range GetAllPermissionScopes() {
		// id-token is treated as write-or-absent by GitHub Actions, so skip it here.
		if scope == PermissionIdToken {
			continue
		}
		readAllPerms.Set(scope, PermissionRead)
	}
	err = validateDangerousPermissions(&WorkflowData{Permissions: "permissions: {}"}, readAllPerms)
	require.NoError(t, err, "read-only permissions must be accepted for the agent job")
}

// TestFormal_P5_AgentMustRunInSandbox (P5 AgentSandboxed)
//
// SG-05: Agent processes must execute in isolated sandbox environments.
// isSandboxEnabled must return true for approved sandbox configurations and
// false when the sandbox is explicitly disabled.
func TestFormal_P5_AgentMustRunInSandbox(t *testing.T) {
	// An explicit AWF sandbox configuration must be enabled.
	awfSandbox := &SandboxConfig{
		Agent: &AgentSandboxConfig{Type: SandboxTypeAWF},
	}
	assert.True(t, isSandboxEnabled(awfSandbox, nil),
		"AWF sandbox must be enabled when agent.type=awf")

	// An explicitly disabled sandbox must not be enabled.
	disabledSandbox := &SandboxConfig{
		Agent: &AgentSandboxConfig{Disabled: true},
	}
	assert.False(t, isSandboxEnabled(disabledSandbox, nil),
		"sandbox must not be enabled when agent.disabled=true")

	// A firewall-enabled network configuration must auto-enable the AWF sandbox.
	firewallNet := &NetworkPermissions{
		Firewall: &FirewallConfig{Enabled: true},
	}
	assert.True(t, isSandboxEnabled(nil, firewallNet),
		"AWF firewall must auto-enable the sandbox")

	// Nil sandbox and nil network must not be treated as enabled.
	assert.False(t, isSandboxEnabled(nil, nil),
		"no sandbox configuration must not be treated as sandbox-enabled")
}

// TestFormal_P6_SecurityFailureHaltsExecution (P6 FailSecure)
//
// SG-07: Security violations must prevent workflow execution rather than
// allowing degraded operation.  A write-permission violation detected during
// compilation must block lock-file emission — CompileToYAML returns ("", err).
//
// strict:false in the frontmatter bypasses strict-mode-only checks so that
// validateDangerousPermissions (the spec-mandated P6 guard) is the sole blocker.
func TestFormal_P6_SecurityFailureHaltsExecution(t *testing.T) {
	md := `---
name: fail-secure-test
on: push
engine: copilot
strict: false
permissions:
  contents: write
---

# Mission

Simulate a write-permission violation to verify that emit is blocked.
`
	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowString(md, "workflow.md")
	require.NoError(t, err, "ParseWorkflowString must succeed before the compilation-time security check runs")

	yamlOut, err := compiler.CompileToYAML(wd, "workflow.md")
	require.Error(t, err, "CompileToYAML must return an error when a write-permission violation is present (P6 FailSecure)")
	assert.Empty(t, yamlOut, "CompileToYAML must return empty YAML — the lock-file must not be emitted — when a security violation is detected")
	require.ErrorContains(t, err, "write permissions", "error must identify the permission violation")
}

// TestFormal_P7_ConformanceLevelMonotonicity (P7 Monotonicity)
//
// Spec Section 2: conformance classes must satisfy Complete >= Standard >= Basic.
func TestFormal_P7_ConformanceLevelMonotonicity(t *testing.T) {
	assert.True(t,
		formalConformanceMonotonicity(
			formalConformanceLevelBasic,
			formalConformanceLevelStandard,
			formalConformanceLevelComplete,
		),
		"Complete >= Standard >= Basic must hold")

	// A reversed assignment must violate the invariant.
	assert.False(t,
		formalConformanceMonotonicity(
			formalConformanceLevelComplete,
			formalConformanceLevelStandard,
			formalConformanceLevelBasic,
		),
		"reversed level assignment must not satisfy the monotonicity invariant")

	// The level constants themselves must satisfy the ordering numerically.
	assert.True(t,
		int(formalConformanceLevelComplete) >= int(formalConformanceLevelStandard) &&
			int(formalConformanceLevelStandard) >= int(formalConformanceLevelBasic),
		"conformance level constants must be positive integers satisfying the ordering")
}

// TestFormal_P8_JobDependencyChainOrder (P8 JobOrder)
//
// Spec Appendix A: the canonical job dependency order must be
// pre_activation → activation → agent → detection → safe_outputs → conclusion.
func TestFormal_P8_JobDependencyChainOrder(t *testing.T) {
	canonical := []string{
		string(constants.PreActivationJobName),
		string(constants.ActivationJobName),
		string(constants.AgentJobName),
		string(constants.DetectionJobName),
		string(constants.SafeOutputsJobName),
		string(constants.ConclusionJobName),
	}

	assert.True(t, formalJobOrderValid(canonical),
		"canonical job dependency order must be valid")

	// Reversing the order must violate the invariant.
	reversed := make([]string, len(canonical))
	for i, v := range canonical {
		reversed[len(canonical)-1-i] = v
	}
	assert.False(t, formalJobOrderValid(reversed),
		"reversed job order must be invalid")

	// Job name constants must match the specification values exactly.
	assert.Equal(t, "pre_activation", string(constants.PreActivationJobName))
	assert.Equal(t, "activation", string(constants.ActivationJobName))
	assert.Equal(t, "agent", string(constants.AgentJobName))
	assert.Equal(t, "detection", string(constants.DetectionJobName))
	assert.Equal(t, "safe_outputs", string(constants.SafeOutputsJobName))
	assert.Equal(t, "conclusion", string(constants.ConclusionJobName))
}

// TestFormal_P9_CompilationValidatesBeforeEmit (P9 CompileValidates)
//
// Spec Section 10: compilation-time security checks must block lock-file
// emission when the input is invalid.
//
// Two distinct code paths are exercised:
//
//	a. Dangerous permissions → CompileToYAML returns ("", err) via
//	   validateDangerousPermissions.  strict:false ensures the check comes from
//	   the non-strict validator rather than the strict-mode guard.
//
//	b. Wildcard-only network allowlist in strict mode → ParseWorkflowString
//	   itself returns an error via runStrictFrontmatterValidations, so no
//	   WorkflowData is produced and no YAML can be emitted.
func TestFormal_P9_CompilationValidatesBeforeEmit(t *testing.T) {
	// a. Dangerous permissions detected by CompileToYAML must block emit.
	mdPerms := `---
name: compile-validates-perms
on: push
engine: copilot
strict: false
permissions:
  issues: write
---

# Mission

Simulate dangerous permissions at compile time.
`
	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowString(mdPerms, "workflow.md")
	require.NoError(t, err, "ParseWorkflowString must succeed before the compilation-time check runs")

	yamlOut, err := compiler.CompileToYAML(wd, "workflow.md")
	require.Error(t, err, "dangerous permissions must be rejected at compile time (P9)")
	assert.Empty(t, yamlOut, "CompileToYAML must return empty YAML when dangerous permissions are detected — no lock-file may be emitted")

	// b. Wildcard-only network allowlist with default strict mode is rejected by
	// ParseWorkflowString (runStrictFrontmatterValidations → validateStrictNetwork),
	// so no WorkflowData is produced and CompileToYAML cannot be reached.
	mdNet := `---
name: compile-validates-network
on: push
engine: copilot
network:
  allowed: ["*"]
---

# Mission

Simulate a wildcard network violation.
`
	compiler2 := NewCompiler(WithNoEmit(true))
	_, strictErr := compiler2.ParseWorkflowString(mdNet, "workflow.md")
	require.Error(t, strictErr, "wildcard-only network allowlist must be rejected before any YAML is generated (P9)")
	require.ErrorContains(t, strictErr, "wildcard", "error must identify the wildcard violation")
}

// TestFormal_P10_WriteTokenIsolatedToSafeOutput (P10 TokenIsolation)
//
// Spec Section 5: write credentials must not be available during agent execution.
// The ingestion app key may appear in a trusted post-agent mint step, not in the
// execution environment; that token must not request write permissions by default.
func TestFormal_P10_WriteTokenIsolatedToSafeOutput(t *testing.T) {
	md := `---
name: token-isolation-test
on: push
engine: copilot
permissions:
  contents: read
safe-outputs:
  create-issue:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Mission

Token isolation test: verify the private key is unavailable during agent execution.
`
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "workflow.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(md), 0600)) //nolint:gosec // 0600 avoids world-readable files with embedded secret reference patterns

	compiler := NewCompiler(WithNoEmit(true))
	wd, err := compiler.ParseWorkflowFile(mdPath)
	require.NoError(t, err, "workflow with safe-outputs github-app must parse without error")

	yamlOut, err := compiler.CompileToYAML(wd, mdPath)
	require.NoError(t, err, "workflow must compile successfully")
	require.NotEmpty(t, yamlOut, "compiled YAML must not be empty")

	// Partition the compiled YAML into per-job sections so we can verify
	// isolation at the job boundary.
	sections := formalJobSections(yamlOut)

	agentSection, hasAgent := sections["agent"]
	require.True(t, hasAgent, "compiled YAML must contain an agent job")

	safeOutputsSection, hasSafeOutputs := sections["safe_outputs"]
	require.True(t, hasSafeOutputs, "compiled YAML must contain a safe_outputs job")

	mintStart := strings.Index(agentSection, "      - name: Generate GitHub App token for output ingestion\n")
	require.GreaterOrEqual(t, mintStart, 0)
	executionStart := strings.Index(agentSection, "        id: agentic_execution\n")
	require.GreaterOrEqual(t, executionStart, 0)
	assert.Less(t, executionStart, mintStart)
	require.Contains(t, agentSection, "      - name: Stop MCP Gateway\n")
	assert.Less(t, strings.Index(agentSection, "      - name: Stop MCP Gateway\n"), mintStart)
	assert.NotContains(t, agentSection[:mintStart], "APP_PRIVATE_KEY",
		"the ingestion app key must not be present before agent execution finishes")
	assert.Contains(t, agentSection[mintStart:], "private-key: ${{ secrets.APP_PRIVATE_KEY }}")
	assert.NotContains(t, agentSection, "permission-issues: write",
		"the ingestion token must not request the safe_outputs job's write permissions")

	// The safe_outputs job must hold the private key in its mint step inputs (with: block).
	assert.Contains(t, safeOutputsSection, "private-key: ${{ secrets.APP_PRIVATE_KEY }}",
		"safe_outputs job must hold the private key in the mint step with: block")
}
