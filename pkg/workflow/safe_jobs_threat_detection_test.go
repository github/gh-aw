//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSafeOutputsJobsEnableThreatDetectionByDefault verifies that when safe-outputs.jobs
// is configured, threat detection is automatically enabled even if not mentioned in frontmatter
func TestSafeOutputsJobsEnableThreatDetectionByDefault(t *testing.T) {
	c := NewCompiler()

	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"jobs": map[string]any{
				"my-custom-job": map[string]any{
					"steps": []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	safeOutputsConfig := c.extractSafeOutputsConfig(frontmatter)

	if safeOutputsConfig == nil {
		t.Fatal("Expected safe-outputs config to be extracted, got nil")
	}

	// Verify that Jobs are parsed
	if len(safeOutputsConfig.Jobs) != 1 {
		t.Fatalf("Expected 1 job in safe-outputs, got %d", len(safeOutputsConfig.Jobs))
	}

	// Verify that threat detection is enabled by default
	// A non-nil ThreatDetection means it's enabled; nil means disabled
	if safeOutputsConfig.ThreatDetection == nil {
		t.Error("Expected threat detection to be enabled by default when safe-outputs.jobs is configured")
	}
}

// TestSafeOutputsJobsRespectExplicitThreatDetectionFalse verifies that when
// threat-detection is explicitly set to false, it respects that setting
func TestSafeOutputsJobsRespectExplicitThreatDetectionFalse(t *testing.T) {
	c := NewCompiler()

	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"threat-detection": false,
			"jobs": map[string]any{
				"my-custom-job": map[string]any{
					"steps": []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	safeOutputsConfig := c.extractSafeOutputsConfig(frontmatter)

	if safeOutputsConfig == nil {
		t.Fatal("Expected safe-outputs config to be extracted, got nil")
	}

	// Verify that threat detection respects explicit false
	// When explicitly disabled, ThreatDetection should be nil
	if safeOutputsConfig.ThreatDetection != nil {
		t.Error("Expected threat detection to be disabled (nil) when explicitly set to false")
	}
}

// TestSafeOutputsJobsRespectExplicitThreatDetectionTrue verifies that when
// threat-detection is explicitly set to true, it respects that setting
func TestSafeOutputsJobsRespectExplicitThreatDetectionTrue(t *testing.T) {
	c := NewCompiler()

	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"threat-detection": true,
			"jobs": map[string]any{
				"my-custom-job": map[string]any{
					"steps": []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	safeOutputsConfig := c.extractSafeOutputsConfig(frontmatter)

	if safeOutputsConfig == nil {
		t.Fatal("Expected safe-outputs config to be extracted, got nil")
	}

	// Verify that threat detection respects explicit true
	// When explicitly enabled, ThreatDetection should be non-nil
	if safeOutputsConfig.ThreatDetection == nil {
		t.Error("Expected threat detection to be enabled (non-nil) when explicitly set to true")
	}
}

// TestSafeOutputsJobsDependOnDetectionJob verifies that custom safe-output jobs
// depend on both the agent job and the detection job when threat detection is enabled
func TestSafeOutputsJobsDependOnDetectionJob(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				// Non-nil ThreatDetection means enabled
			},
			Jobs: map[string]*SafeJobConfig{
				"my-custom-job": {
					Steps: []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	// Build safe jobs with threat detection enabled
	_, err := c.buildSafeJobs(workflowData, true)
	if err != nil {
		t.Fatalf("Unexpected error building safe jobs: %v", err)
	}

	jobs := c.jobManager.GetAllJobs()
	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job to be created, got %d", len(jobs))
	}

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	// Detection is a separate job, so safe-jobs depend on both "agent" and "detection"
	hasAgentDep := false
	hasDetectionDep := false
	for _, dep := range job.Needs {
		if dep == "agent" {
			hasAgentDep = true
		}
		if dep == "detection" {
			hasDetectionDep = true
		}
	}

	if !hasAgentDep {
		t.Error("Expected job to depend on 'agent' job")
	}

	if !hasDetectionDep {
		t.Error("Expected job to depend on 'detection' job (detection is now a separate job)")
	}
}

// TestSafeOutputsJobsDoNotDependOnDetectionWhenDisabled verifies that custom safe-output jobs
// do NOT depend on the detection job when threat detection is disabled
func TestSafeOutputsJobsDoNotDependOnDetectionWhenDisabled(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: nil, // nil means disabled
			Jobs: map[string]*SafeJobConfig{
				"my-custom-job": {
					Steps: []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	// Build safe jobs with threat detection disabled
	_, err := c.buildSafeJobs(workflowData, false)
	if err != nil {
		t.Fatalf("Unexpected error building safe jobs: %v", err)
	}

	jobs := c.jobManager.GetAllJobs()
	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job to be created, got %d", len(jobs))
	}

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	// Verify the job depends on 'agent' but NOT 'detection'
	hasAgentDep := false
	hasDetectionDep := false
	for _, dep := range job.Needs {
		if dep == "agent" {
			hasAgentDep = true
		}
		if dep == "detection" {
			hasDetectionDep = true
		}
	}

	if !hasAgentDep {
		t.Error("Expected job to depend on 'agent' job")
	}

	if hasDetectionDep {
		t.Error("Expected job NOT to depend on 'detection' job when threat detection is disabled")
	}
}

// TestHasSafeOutputsEnabledWithJobs verifies that HasSafeOutputsEnabled returns true
// when only safe-outputs.jobs is configured (no other safe-outputs)
func TestHasSafeOutputsEnabledWithJobs(t *testing.T) {
	config := &SafeOutputsConfig{
		Jobs: map[string]*SafeJobConfig{
			"my-job": {},
		},
	}

	if !HasSafeOutputsEnabled(config) {
		t.Error("Expected HasSafeOutputsEnabled to return true when safe-outputs.jobs is configured")
	}
}

// TestHasSafeOutputsEnabledWithoutJobs verifies that HasSafeOutputsEnabled returns false
// when safe-outputs exists but has no enabled features
func TestHasSafeOutputsEnabledWithoutJobs(t *testing.T) {
	config := &SafeOutputsConfig{
		Jobs: map[string]*SafeJobConfig{},
	}

	if HasSafeOutputsEnabled(config) {
		t.Error("Expected HasSafeOutputsEnabled to return false when safe-outputs.jobs is empty")
	}
}

// TestSafeJobsWithThreatDetectionConfigObject verifies that threat detection
// configuration object is properly handled
func TestSafeJobsWithThreatDetectionConfigObject(t *testing.T) {
	c := NewCompiler()

	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"threat-detection": map[string]any{
				"enabled": true,
				"prompt":  "Additional security instructions",
			},
			"jobs": map[string]any{
				"my-custom-job": map[string]any{
					"steps": []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	safeOutputsConfig := c.extractSafeOutputsConfig(frontmatter)

	if safeOutputsConfig == nil {
		t.Fatal("Expected safe-outputs config to be extracted, got nil")
	}

	// Verify that threat detection is enabled
	// Non-nil ThreatDetection means enabled
	if safeOutputsConfig.ThreatDetection == nil {
		t.Error("Expected threat detection to be enabled (non-nil)")
	}

	// Verify custom prompt is preserved
	if safeOutputsConfig.ThreatDetection.Prompt != "Additional security instructions" {
		t.Errorf("Expected custom prompt to be preserved, got %q", safeOutputsConfig.ThreatDetection.Prompt)
	}
}

// TestSafeJobsIntegrationWithWorkflowCompilation is an integration test that verifies
// the entire workflow compilation process with safe-output jobs and threat detection
func TestSafeJobsIntegrationWithWorkflowCompilation(t *testing.T) {
	c := NewCompiler()

	markdown := `---
on: issues
safe-outputs:
  jobs:
    my-custom-job:
      steps:
        - run: echo "test"
---

# Test Workflow
Test workflow content
`

	// Create temporary test file
	tmpDir := testutil.TempDir(t, "test-*")
	testFile := tmpDir + "/test-safe-jobs.md"
	if err := os.WriteFile(testFile, []byte(markdown), 0o644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Compile the workflow
	err := c.CompileWorkflow(testFile)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := tmpDir + "/test-safe-jobs.lock.yml"
	workflow, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	workflowStr := string(workflow)

	// Verify detection is a separate job (not inline in agent job)
	if !strings.Contains(workflowStr, "  detection:") {
		t.Error("Expected compiled workflow to contain separate 'detection:' job")
	}

	// Verify detection steps exist in detection job (not agent job)
	detectionSection := extractJobSection(workflowStr, "detection")
	if detectionSection == "" {
		t.Error("Expected compiled workflow to contain 'detection:' job")
	}
	if !strings.Contains(detectionSection, "detection_guard") {
		t.Error("Expected detection job to contain detection_guard step")
	}

	// Verify custom safe job is created
	if !strings.Contains(workflowStr, "my_custom_job:") {
		t.Error("Expected compiled workflow to contain 'my_custom_job:' job")
	}

	// Verify custom job depends on detection job (threat detection enabled by default)
	if !strings.Contains(workflowStr, "- detection") {
		t.Error("Expected custom safe job to depend on detection job")
	}
}

func TestSafeJobsThreatDetectionArtifactPathsUseSlashes(t *testing.T) {
	c := NewCompiler()
	markdown := `---
name: Reproduce detector artifact redaction failure
on:
  workflow_dispatch:
permissions:
  copilot-requests: write
engine:
  id: copilot
  model: gpt-5.6-luna
safe-outputs:
  threat-detection:
    engine:
      id: copilot
      model: detection
  jobs:
    publish:
      description: Publish a value after detection succeeds.
      runs-on: ubuntu-latest
      permissions:
        contents: read
      inputs:
        value:
          type: string
          required: true
      steps:
        - run: echo "$VALUE"
          env:
            VALUE: ${{ inputs.value }}
---

Return a short response.
`

	tmpDir := testutil.TempDir(t, "test-*")
	testFile := filepath.Join(tmpDir, "test-safe-jobs-threat-detection.md")
	require.NoError(t, os.WriteFile(testFile, []byte(markdown), 0o644))
	require.NoError(t, c.CompileWorkflow(testFile))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err)
	uploadSection := extractWorkflowStepByName(t, string(lockContent), "Upload agent output fallback artifact")
	assert.Contains(t, uploadSection, "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl")
	assert.Contains(t, uploadSection, "/tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl")
	assert.NotContains(t, uploadSection, `\tmp\gh-aw`)
}

// TestSafeJobsExpressionEnvNeverWrittenToOutput verifies that job-level env vars containing
// GitHub Actions expressions are never written to $GITHUB_OUTPUT in the compiled lock file.
// This is a security guardrail: tokens like ${{ github.token }} must never be stored in outputs.
func TestSafeJobsExpressionEnvNeverWrittenToOutput(t *testing.T) {
	c := NewCompiler()

	markdown := `---
on: issues
safe-outputs:
  jobs:
    publish:
      env:
        GH_TOKEN: ${{ github.token }}
        STATIC: literal-value
      steps:
        - name: Do work
          run: echo "working"
---

# Test Workflow
Test workflow content
`

	tmpDir := testutil.TempDir(t, "test-*")
	testFile := tmpDir + "/test-safe-jobs-secret.md"
	if err := os.WriteFile(testFile, []byte(markdown), 0o644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := tmpDir + "/test-safe-jobs-secret.lock.yml"
	workflowBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	workflowStr := string(workflowBytes)

	// No env var at all may be written to $GITHUB_OUTPUT by safe-jobs.
	for line := range strings.SplitSeq(workflowStr, "\n") {
		if strings.Contains(line, "GH_TOKEN") && strings.Contains(line, "GITHUB_OUTPUT") {
			t.Errorf("GH_TOKEN must never be written to GITHUB_OUTPUT (secret leak): %s", strings.TrimSpace(line))
		}
		if strings.Contains(line, "STATIC") && strings.Contains(line, "GITHUB_OUTPUT") {
			t.Errorf("STATIC env var must not be written to GITHUB_OUTPUT: %s", strings.TrimSpace(line))
		}
	}

	// The token must be injected into the step env: block, not through step outputs.
	if !strings.Contains(workflowStr, "GH_TOKEN: ${{ github.token }}") {
		t.Error("Expected GH_TOKEN expression to be injected into step env: block in compiled output")
	}

	// Literal value must also be injected directly into the step env: block.
	if !strings.Contains(workflowStr, "STATIC: literal-value") {
		t.Error("Expected literal env var to be injected directly into step env: block in compiled output")
	}
}

func TestIsThreatDetectionExplicitlyDisabledInConfigs(t *testing.T) {
	tests := []struct {
		name     string
		configs  []string
		expected bool
	}{
		{
			name:     "empty configs",
			configs:  []string{},
			expected: false,
		},
		{
			name:     "empty JSON objects",
			configs:  []string{"{}", ""},
			expected: false,
		},
		{
			name:     "config without threat-detection key",
			configs:  []string{`{"create-issue": {"max": 1}}`},
			expected: false,
		},
		{
			name:     "config with threat-detection false",
			configs:  []string{`{"create-issue": {"max": 1}, "threat-detection": false}`},
			expected: true,
		},
		{
			name:     "config with threat-detection true",
			configs:  []string{`{"create-issue": {"max": 1}, "threat-detection": true}`},
			expected: false,
		},
		{
			name:     "config with threat-detection as object",
			configs:  []string{`{"create-issue": {"max": 1}, "threat-detection": {"prompt": "check for injection"}}`},
			expected: false,
		},
		{
			name:     "config with threat-detection object and enabled: false",
			configs:  []string{`{"create-issue": {"max": 1}, "threat-detection": {"enabled": false}}`},
			expected: true,
		},
		{
			name:     "config with threat-detection object and enabled: true",
			configs:  []string{`{"create-issue": {"max": 1}, "threat-detection": {"enabled": true}}`},
			expected: false,
		},
		{
			name:     "multiple configs, one has false",
			configs:  []string{`{"create-issue": {"max": 1}}`, `{"create-discussion": {"max": 1}, "threat-detection": false}`},
			expected: true,
		},
		{
			name:     "multiple configs, none disabled",
			configs:  []string{`{"create-issue": {"max": 1}}`, `{"create-discussion": {"max": 1}}`},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isThreatDetectionExplicitlyDisabledInConfigs(tt.configs)
			if result != tt.expected {
				t.Errorf("isThreatDetectionExplicitlyDisabledInConfigs() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestDefaultThreatDetectionAppliedWhenSafeOutputsFromImportsOnly verifies that when
// safe-outputs configuration comes entirely from imports (no safe-outputs: in main frontmatter),
// threat detection is enabled by default — ensuring the detection gate is wired for
// MCP-driven safe-output writes in native-card-style workflows.
func TestDefaultThreatDetectionAppliedWhenSafeOutputsFromImportsOnly(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Shared workflow provides safe-outputs with no threat-detection key (default should apply)
	sharedWorkflow := `---
safe-outputs:
  create-issue:
    max: 1
    labels: [test]
---

# Shared Safe Outputs
`
	sharedFile := filepath.Join(workflowsDir, "shared-safe-outputs.md")
	if err := os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Main workflow has NO safe-outputs: section — safe-outputs comes entirely from import
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-safe-outputs.md
---

# Native-Card-Style Workflow

This workflow uses safe-outputs via MCP tool calls (no explicit safe-outputs: frontmatter).
`
	mainFile := filepath.Join(workflowsDir, "native-card.md")
	if err := os.WriteFile(mainFile, []byte(mainWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write main file: %v", err)
	}

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile(mainFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected SafeOutputs to be non-nil after importing shared safe-outputs config")
	}

	// Core assertion: threat detection must be enabled by default when safe-outputs
	// comes entirely from imports and no config explicitly disabled it.
	if workflowData.SafeOutputs.ThreatDetection == nil {
		t.Error("Expected ThreatDetection to be enabled (non-nil) by default when safe-outputs comes from imports only")
	}
}

// TestDefaultThreatDetectionNotAppliedWhenImportedConfigExplicitlyDisables verifies that
// when an imported config explicitly sets threat-detection: false, the default is NOT applied.
func TestDefaultThreatDetectionNotAppliedWhenImportedConfigExplicitlyDisables(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Shared workflow explicitly disables threat detection
	sharedWorkflow := `---
safe-outputs:
  create-issue:
    max: 1
  threat-detection: false
---

# Shared Safe Outputs (detection disabled)
`
	sharedFile := filepath.Join(workflowsDir, "shared-no-detection.md")
	if err := os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Main workflow has NO safe-outputs: section
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-no-detection.md
---

# Workflow with detection explicitly disabled in import
`
	mainFile := filepath.Join(workflowsDir, "no-detection.md")
	if err := os.WriteFile(mainFile, []byte(mainWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write main file: %v", err)
	}

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile(mainFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected SafeOutputs to be non-nil after importing shared safe-outputs config")
	}

	// When imported config explicitly disabled detection, the default should NOT be applied.
	if workflowData.SafeOutputs.ThreatDetection != nil {
		t.Error("Expected ThreatDetection to be nil (disabled) when imported config explicitly sets threat-detection: false")
	}
}

// TestImportedSafeOutputsCompiledWithDetectionJob verifies that the compiled workflow
// for a native-card-style workflow (safe-outputs from imports only) contains a detection job
// and that safe_outputs depends on both agent and detection.
func TestImportedSafeOutputsCompiledWithDetectionJob(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := testutil.TempDir(t, "native-card-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Shared workflow provides safe-outputs with no threat-detection key
	sharedWorkflow := `---
safe-outputs:
  create-issue:
    max: 1
---

# Shared Safe Outputs
`
	sharedFile := filepath.Join(workflowsDir, "shared.md")
	if err := os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Main workflow has NO safe-outputs: section
	mainWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
imports:
  - ./shared.md
---

# Native-Card-Style Workflow

Test that safe_outputs depends on detection when safe-outputs comes from imports.
`
	mainFile := filepath.Join(workflowsDir, "main.md")
	if err := os.WriteFile(mainFile, []byte(mainWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write main file: %v", err)
	}

	// Compile the workflow
	if err := compiler.CompileWorkflow(mainFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled lock file
	lockFile := filepath.Join(workflowsDir, "main.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	workflowStr := string(content)

	// Verify that a detection job was generated
	if !strings.Contains(workflowStr, "  detection:") {
		t.Error("Expected compiled workflow to contain 'detection:' job when safe-outputs comes from imports")
	}

	// Verify that safe_outputs depends on both agent and detection
	safeOutputsSection := extractJobSection(workflowStr, "safe_outputs")
	if safeOutputsSection == "" {
		t.Fatal("Expected compiled workflow to contain 'safe_outputs:' job")
	}
	if !strings.Contains(safeOutputsSection, "- detection") {
		t.Error("Expected safe_outputs job to depend on 'detection' job when safe-outputs comes from imports")
	}
	if !strings.Contains(safeOutputsSection, "detection.result == 'success'") {
		t.Error("Expected safe_outputs job to gate on detection.result == 'success'")
	}
}

// TestDefaultThreatDetectionNotAppliedWhenImportedConfigObjectFormDisables verifies that
// when an imported config disables detection via the object form (threat-detection: { enabled: false }),
// the default is NOT applied — mirroring parseThreatDetectionConfig's object-form support.
func TestDefaultThreatDetectionNotAppliedWhenImportedConfigObjectFormDisables(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Shared workflow disables threat detection using the object form
	sharedWorkflow := `---
safe-outputs:
  create-issue:
    max: 1
  threat-detection:
    enabled: false
---

# Shared Safe Outputs (detection disabled via object form)
`
	sharedFile := filepath.Join(workflowsDir, "shared-no-detection-obj.md")
	if err := os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Main workflow has NO safe-outputs: section
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-no-detection-obj.md
---

# Workflow with detection disabled via object form in import
`
	mainFile := filepath.Join(workflowsDir, "no-detection-obj.md")
	if err := os.WriteFile(mainFile, []byte(mainWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write main file: %v", err)
	}

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile(mainFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected SafeOutputs to be non-nil after importing shared safe-outputs config")
	}

	// When imported config uses { enabled: false }, the default should NOT be applied.
	if workflowData.SafeOutputs.ThreatDetection != nil {
		t.Error("Expected ThreatDetection to be nil (disabled) when imported config uses threat-detection: { enabled: false }")
	}
}

// TestParseThreatDetectionConfigExpression verifies that expression strings are accepted
// for threat-detection (top-level) and stored in EnabledExpr.
func TestParseThreatDetectionConfigExpression(t *testing.T) {
	c := NewCompiler()

	tests := []struct {
		name            string
		frontmatter     map[string]any
		wantNil         bool
		wantEnabledExpr string
		wantConditional bool
		wantHasRunnable bool
	}{
		{
			name: "literal true",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": true,
				},
			},
			wantNil:         false,
			wantEnabledExpr: "",
			wantConditional: false,
			wantHasRunnable: true,
		},
		{
			name: "literal false",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": false,
				},
			},
			wantNil: true,
		},
		{
			name: "expression string",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": "${{ inputs.enable-threat-detection }}",
				},
			},
			wantNil:         false,
			wantEnabledExpr: "${{ inputs.enable-threat-detection }}",
			wantConditional: true,
			wantHasRunnable: true,
		},
		{
			name: "object with literal enabled true",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": map[string]any{
						"enabled": true,
					},
				},
			},
			wantNil:         false,
			wantEnabledExpr: "",
			wantConditional: false,
			wantHasRunnable: true,
		},
		{
			name: "object with literal enabled false",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": map[string]any{
						"enabled": false,
					},
				},
			},
			wantNil: true,
		},
		{
			name: "object with expression enabled",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": map[string]any{
						"enabled": "${{ inputs.enable-threat-detection }}",
					},
				},
			},
			wantNil:         false,
			wantEnabledExpr: "${{ inputs.enable-threat-detection }}",
			wantConditional: true,
			wantHasRunnable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := c.extractSafeOutputsConfig(tt.frontmatter)
			if tt.wantNil {
				if config != nil && config.ThreatDetection != nil {
					t.Errorf("expected ThreatDetection nil (disabled), got non-nil")
				}
				return
			}
			if config == nil || config.ThreatDetection == nil {
				t.Fatal("expected non-nil ThreatDetection config")
			}
			td := config.ThreatDetection
			if tt.wantEnabledExpr == "" {
				if td.EnabledExpr != nil {
					t.Errorf("expected EnabledExpr nil, got %q", *td.EnabledExpr)
				}
			} else {
				if td.EnabledExpr == nil {
					t.Fatalf("expected EnabledExpr %q, got nil", tt.wantEnabledExpr)
				}
				if *td.EnabledExpr != tt.wantEnabledExpr {
					t.Errorf("expected EnabledExpr %q, got %q", tt.wantEnabledExpr, *td.EnabledExpr)
				}
			}
			if got := td.IsConditional(); got != tt.wantConditional {
				t.Errorf("IsConditional() = %v, want %v", got, tt.wantConditional)
			}
			if got := td.HasRunnableDetection(); got != tt.wantHasRunnable {
				t.Errorf("HasRunnableDetection() = %v, want %v", got, tt.wantHasRunnable)
			}
		})
	}
}

// TestParseThreatDetectionContinueOnErrorExpression verifies that expression strings
// are accepted for continue-on-error and stored in ContinueOnErrorExpr.
func TestParseThreatDetectionContinueOnErrorExpression(t *testing.T) {
	c := NewCompiler()

	tests := []struct {
		name           string
		coeValue       any
		wantCOELiteral *bool
		wantCOEExpr    string
	}{
		{
			name:           "literal true",
			coeValue:       true,
			wantCOELiteral: boolPtr(true),
			wantCOEExpr:    "",
		},
		{
			name:           "literal false",
			coeValue:       false,
			wantCOELiteral: boolPtr(false),
			wantCOEExpr:    "",
		},
		{
			name:        "expression string",
			coeValue:    "${{ inputs.detection-coe }}",
			wantCOEExpr: "${{ inputs.detection-coe }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": map[string]any{
						"continue-on-error": tt.coeValue,
					},
				},
			}
			config := c.extractSafeOutputsConfig(frontmatter)
			if config == nil || config.ThreatDetection == nil {
				t.Fatal("expected non-nil ThreatDetection")
			}
			td := config.ThreatDetection

			if tt.wantCOELiteral != nil {
				if td.ContinueOnError == nil {
					t.Fatalf("expected ContinueOnError %v, got nil", *tt.wantCOELiteral)
				}
				if *td.ContinueOnError != *tt.wantCOELiteral {
					t.Errorf("ContinueOnError = %v, want %v", *td.ContinueOnError, *tt.wantCOELiteral)
				}
			} else {
				if td.ContinueOnError != nil {
					t.Errorf("expected ContinueOnError nil, got %v", *td.ContinueOnError)
				}
			}

			if tt.wantCOEExpr == "" {
				if td.ContinueOnErrorExpr != nil {
					t.Errorf("expected ContinueOnErrorExpr nil, got %q", *td.ContinueOnErrorExpr)
				}
			} else {
				if td.ContinueOnErrorExpr == nil {
					t.Fatalf("expected ContinueOnErrorExpr %q, got nil", tt.wantCOEExpr)
				}
				if *td.ContinueOnErrorExpr != tt.wantCOEExpr {
					t.Errorf("ContinueOnErrorExpr = %q, want %q", *td.ContinueOnErrorExpr, tt.wantCOEExpr)
				}
			}
		})
	}
}

// TestIsConditionalDetection verifies the IsConditionalDetection helper.
func TestIsConditionalDetection(t *testing.T) {
	expr := "${{ inputs.flag }}"
	tests := []struct {
		name string
		so   *SafeOutputsConfig
		want bool
	}{
		{"nil SafeOutputsConfig", nil, false},
		{"nil ThreatDetection", &SafeOutputsConfig{}, false},
		{"literal bool config", &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}}, false},
		{"expression config", &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{EnabledExpr: &expr}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsConditionalDetection(tt.so); got != tt.want {
				t.Errorf("IsConditionalDetection() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDetectionJobConditionWithExpression verifies that when threat-detection uses an expression,
// the compiled detection job's if: condition includes the expression.
func TestDetectionJobConditionWithExpression(t *testing.T) {
	c := NewCompiler()

	expr := "${{ inputs.enable-threat-detection }}"
	data := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EnabledExpr: &expr,
			},
		},
	}

	job, err := c.buildDetectionJob(data)
	if err != nil {
		t.Fatalf("buildDetectionJob returned error: %v", err)
	}
	if job == nil {
		t.Fatal("expected non-nil detection job")
	}

	// The if: condition must include the raw expression (without ${{ }})
	if !strings.Contains(job.If, "inputs.enable-threat-detection") {
		t.Errorf("detection job if: %q does not contain 'inputs.enable-threat-detection'", job.If)
	}
}

// TestSafeOutputsJobConditionWithConditionalDetection verifies that the safe_outputs job
// condition uses always() + buildDetectionPassedCondition() when detection is expression-based.
func TestSafeOutputsJobConditionWithConditionalDetection(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	expr := "${{ inputs.enable-threat-detection }}"
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EnabledExpr: &expr,
			},
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[Test] ",
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")
	if err != nil {
		t.Fatalf("buildConsolidatedSafeOutputsJob returned error: %v", err)
	}
	if job == nil {
		t.Fatal("expected non-nil safe_outputs job")
	}

	// Condition must include always() to override skip behavior when detection is skipped
	if !strings.Contains(job.If, "always()") {
		t.Errorf("safe_outputs job if: %q should contain 'always()' for conditional detection", job.If)
	}

	// Condition must accept both success and skipped detection results
	if !strings.Contains(job.If, "'skipped'") {
		t.Errorf("safe_outputs job if: %q should check for 'skipped' detection result for conditional detection", job.If)
	}
	if !strings.Contains(job.If, "'success'") {
		t.Errorf("safe_outputs job if: %q should check for 'success' detection result", job.If)
	}

	// Job must still depend on detection job
	if !slices.Contains(job.Needs, string(constants.DetectionJobName)) {
		t.Errorf("safe_outputs job Needs %v should contain detection job", job.Needs)
	}
}

// TestIsThreatDetectionExplicitlyDisabledExpressionNotDisabled verifies that an expression
// string in an imported config is NOT treated as "explicitly disabled".
func TestIsThreatDetectionExplicitlyDisabledExpressionNotDisabled(t *testing.T) {
	configs := []string{
		`{"threat-detection": "${{ inputs.enable-threat-detection }}"}`,
	}
	if isThreatDetectionExplicitlyDisabledInConfigs(configs) {
		t.Error("expression string for threat-detection should not be treated as explicitly disabled")
	}
}

// TestDetectionJobWithExpressionCompilation is an integration test that compiles a full
// workflow with expression-based threat-detection and verifies the lock file output.
func TestDetectionJobWithExpressionCompilation(t *testing.T) {
	c := NewCompiler()

	markdown := `---
on:
  workflow_call:
    inputs:
      enable-threat-detection:
        type: boolean
        required: false
        default: true
safe-outputs:
  threat-detection: ${{ inputs.enable-threat-detection }}
  create-issue:
    title-prefix: "[Test] "
---

# Test Workflow
Test workflow content
`
	tmpDir := testutil.TempDir(t, "test-expr-detection-*")
	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(markdown), 0o644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	yaml := string(content)

	// Detection job must be present (always compiled for conditional detection)
	if !strings.Contains(yaml, "  detection:") {
		t.Error("compiled workflow should contain a 'detection:' job")
	}

	detectionSection := extractJobSection(yaml, "detection")
	if detectionSection == "" {
		t.Fatal("detection job section not found")
	}

	// The detection job's if: must include the caller expression
	if !strings.Contains(detectionSection, "inputs.enable-threat-detection") {
		t.Errorf("detection job should reference 'inputs.enable-threat-detection' in its condition, got:\n%s", detectionSection)
	}

	// The safe_outputs job must handle detection being skipped (always() in condition)
	safeOutputsSection := extractJobSection(yaml, "safe_outputs")
	if safeOutputsSection == "" {
		t.Fatal("safe_outputs job section not found")
	}

	if !strings.Contains(safeOutputsSection, "always()") {
		t.Errorf("safe_outputs job should use always() for conditional detection, got:\n%s", safeOutputsSection)
	}
	if !strings.Contains(safeOutputsSection, "'skipped'") {
		t.Errorf("safe_outputs condition should handle 'skipped' detection result, got:\n%s", safeOutputsSection)
	}
}
