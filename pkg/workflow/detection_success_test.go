//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestDetectionJobHasSuccessOutput verifies that the detection job has detection success/conclusion outputs
func TestDetectionJobHasSuccessOutput(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
features:
  gh-aw-detection: false
---

# Test

Create an issue.
`

	if err := os.WriteFile(workflowPath, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	// Read the compiled YAML
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	yamlBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read compiled YAML: %v", err)
	}
	yaml := string(yamlBytes)

	// Detection is now in a separate detection job
	detectionSection := extractJobSection(yaml, "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled YAML")
	}

	// Check that detection job outputs include detection_success and detection_conclusion
	if !strings.Contains(yaml, "detection_success:") {
		t.Error("Detection job missing detection_success output")
	}
	if !strings.Contains(yaml, "detection_conclusion:") {
		t.Error("Detection job missing detection_conclusion output")
	}
	if !strings.Contains(yaml, "detection_reason:") {
		t.Error("Detection job missing detection_reason output")
	}
	if !strings.Contains(yaml, "aic:") {
		t.Error("Detection job missing aic output")
	}

	// Check that the detection conclusion step has GH_AW_DETECTION_CONTINUE_ON_ERROR env var
	if !strings.Contains(detectionSection, "GH_AW_DETECTION_CONTINUE_ON_ERROR:") {
		t.Error("Detection conclusion step missing GH_AW_DETECTION_CONTINUE_ON_ERROR env var")
	}
	if !strings.Contains(detectionSection, "DETECTION_AGENTIC_EXECUTION_OUTCOME: ${{ steps.detection_agentic_execution.outcome }}") {
		t.Error("Detection conclusion step missing DETECTION_AGENTIC_EXECUTION_OUTCOME env var")
	}

	// Check that the combined parse-and-conclude step has ID detection_conclusion
	if !strings.Contains(detectionSection, "id: detection_conclusion") {
		t.Error("Combined parse-and-conclude step missing id: detection_conclusion")
	}

	// Check that the script uses require to load the parse_threat_detection_results.cjs file
	if !strings.Contains(detectionSection, "require(path.join(actionsDir, 'parse_threat_detection_results.cjs'))") {
		t.Error("Detection conclusion step doesn't use require to load parse_threat_detection_results.cjs")
	}
	if !strings.Contains(detectionSection, "id: parse_detection_token_usage") {
		t.Error("Detection job missing parse_detection_token_usage step")
	}
	if !strings.Contains(detectionSection, "GH_AW_TOKEN_USAGE_SUMMARY_TITLE: Threat Detection Token Usage") {
		t.Error("Detection token usage step missing threat detection summary title")
	}
	if !strings.Contains(detectionSection, "require(path.join(actionsDir, 'parse_token_usage.cjs'))") {
		t.Error("Detection token usage step doesn't use require to load parse_token_usage.cjs")
	}

	// Check that setupGlobals is called
	if !strings.Contains(yaml, "setupGlobals(core, github, context, exec, io, getOctokit)") {
		t.Error("Detection conclusion step doesn't call setupGlobals")
	}

	// Check that main() is awaited
	if !strings.Contains(yaml, "await main()") {
		t.Error("Detection conclusion step doesn't await main()")
	}

	// Verify there is no separate parse_detection_results step (it is now merged into detection_conclusion)
	if strings.Contains(detectionSection, "id: parse_detection_results") {
		t.Error("Separate parse_detection_results step should no longer exist; logic is consolidated in detection_conclusion")
	}
}

// TestSafeOutputJobsCheckDetectionSuccess verifies that safe output jobs check detection success
func TestSafeOutputJobsCheckDetectionSuccess(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
  add-comment:
---

# Test

Create outputs.
`

	if err := os.WriteFile(workflowPath, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	// Read the compiled YAML
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	yamlBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read compiled YAML: %v", err)
	}
	yaml := string(yamlBytes)

	// Check that safe_outputs job has detection success check in its condition
	if !strings.Contains(yaml, "safe_outputs:") {
		t.Fatal("safe_outputs job not found")
	}

	// Detection is now in a separate detection job - check uses detection job result
	// (detection job fails with exit 1 when threats are found, so downstream jobs check job result)
	if !strings.Contains(yaml, "needs.detection.result == 'success'") {
		t.Error("Safe output jobs don't check detection result via detection job result")
	}
	if !strings.Contains(yaml, "GH_AW_AGENT_AIC: ${{ needs.agent.outputs.aic }}") {
		t.Error("Safe output jobs should receive agent AI Credits separately")
	}
	if !strings.Contains(yaml, "GH_AW_THREAT_DETECTION_AIC: ${{ needs.detection.outputs.aic }}") {
		t.Error("Safe output jobs should receive threat-detection AI Credits separately")
	}
}

// TestDetectionRunsStepInConclusionJob verifies that when threat detection is enabled,
// the conclusion job contains a "Log detection run" step.
func TestDetectionRunsStepInConclusionJob(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
---

# Test

Create an issue.
`

	if err := os.WriteFile(workflowPath, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	// Read the compiled YAML
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	yamlBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read compiled YAML: %v", err)
	}
	yaml := string(yamlBytes)

	// Verify conclusion job exists
	conclusionSection := extractJobSection(yaml, "conclusion")
	if conclusionSection == "" {
		t.Fatal("Conclusion job not found in compiled YAML")
	}

	// Verify that "Log detection run" step is in the conclusion job
	if !strings.Contains(conclusionSection, "Log detection run") {
		t.Error("Conclusion job should contain 'Log detection run' step")
	}

	// Verify step has detection conclusion/reason env vars
	if !strings.Contains(conclusionSection, "GH_AW_DETECTION_CONCLUSION:") {
		t.Error("Detection runs step missing GH_AW_DETECTION_CONCLUSION env var")
	}
	if !strings.Contains(conclusionSection, "GH_AW_DETECTION_REASON:") {
		t.Error("Detection runs step missing GH_AW_DETECTION_REASON env var")
	}

	// Verify step has run URL
	if !strings.Contains(conclusionSection, "GH_AW_RUN_URL:") {
		t.Error("Detection runs step missing GH_AW_RUN_URL env var")
	}

	// Verify the step uses handle_detection_runs.cjs
	if !strings.Contains(conclusionSection, "handle_detection_runs.cjs") {
		t.Error("Detection runs step should use handle_detection_runs.cjs")
	}
}

func TestDetectionRunsStepOmittedWhenReportAsIssueDisabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
  threat-detection:
    report-as-issue: false
---

# Test

Create an issue.
`

	if err := os.WriteFile(workflowPath, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	// Read the compiled YAML
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	yamlBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read compiled YAML: %v", err)
	}
	yaml := string(yamlBytes)

	// Verify conclusion job exists (other conclusion steps still run)
	conclusionSection := extractJobSection(yaml, "conclusion")
	if conclusionSection == "" {
		t.Fatal("Conclusion job not found in compiled YAML")
	}

	// The detection runs tracking issue step must be omitted entirely.
	if strings.Contains(conclusionSection, "Log detection run") {
		t.Error("Conclusion job should not contain 'Log detection run' step when report-as-issue is disabled")
	}
	if strings.Contains(conclusionSection, "handle_detection_runs.cjs") {
		t.Error("Conclusion job should not reference handle_detection_runs.cjs when report-as-issue is disabled")
	}

	// Detection job itself must still be present since threat detection remains enabled.
	detectionSection := extractJobSection(yaml, string(constants.DetectionJobName))
	if detectionSection == "" {
		t.Error("Detection job should still be compiled when only report-as-issue is disabled")
	}
}

func TestNoopStepGetsThreatDetectionAICEnvVar(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  noop: {}
---

# Test

Emit noop output.
`

	if err := os.WriteFile(workflowPath, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	yamlBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read compiled YAML: %v", err)
	}
	yaml := string(yamlBytes)

	conclusionSection := extractJobSection(yaml, "conclusion")
	if conclusionSection == "" {
		t.Fatal("Conclusion job not found in compiled YAML")
	}
	if !strings.Contains(conclusionSection, "Process no-op messages") {
		t.Fatal("Conclusion job should contain 'Process no-op messages' step")
	}
	if !strings.Contains(conclusionSection, "GH_AW_THREAT_DETECTION_AIC: ${{ needs.detection.outputs.aic }}") {
		t.Error("No-op step should receive threat-detection AI Credits env var")
	}
}

func TestAgentFailureStepGetsThreatDetectionAICEnvVar(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
---

# Test

Create an issue.
`

	if err := os.WriteFile(workflowPath, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	yamlBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read compiled YAML: %v", err)
	}
	yaml := string(yamlBytes)

	conclusionSection := extractJobSection(yaml, "conclusion")
	if conclusionSection == "" {
		t.Fatal("Conclusion job not found in compiled YAML")
	}
	if !strings.Contains(conclusionSection, "handle_agent_failure") {
		t.Fatal("Conclusion job should contain handle_agent_failure step")
	}
	if !strings.Contains(conclusionSection, "GH_AW_THREAT_DETECTION_AIC: ${{ needs.detection.outputs.aic }}") {
		t.Error("Agent failure step should receive threat-detection AI Credits env var")
	}
}

func TestAgentFailureStepOmitsThreatDetectionAICEnvVarWhenDetectionDisabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
  threat-detection: false
---

# Test

Create an issue.
`

	if err := os.WriteFile(workflowPath, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	yamlBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read compiled YAML: %v", err)
	}
	yaml := string(yamlBytes)

	conclusionSection := extractJobSection(yaml, "conclusion")
	if conclusionSection == "" {
		t.Fatal("Conclusion job not found in compiled YAML")
	}
	if !strings.Contains(conclusionSection, "handle_agent_failure") {
		t.Fatal("Conclusion job should contain handle_agent_failure step")
	}
	if strings.Contains(conclusionSection, "GH_AW_THREAT_DETECTION_AIC: ${{ needs.detection.outputs.aic }}") {
		t.Error("Agent failure step should not receive threat-detection AI Credits env var when detection is disabled")
	}
}

// TestDetectionConclusionStepContinueOnError verifies that:
//   - In warn mode (default, continue-on-error: true), the parse step has continue-on-error: true
//     so that an unexpected parse exception never fails the detection job.
//   - In strict mode (continue-on-error: false), the parse step does NOT have continue-on-error
//     so that a detection failure in strict mode correctly blocks safe_outputs.
func TestDetectionConclusionStepContinueOnError(t *testing.T) {
	tests := []struct {
		name              string
		frontmatter       string
		wantContinueOnErr bool
	}{
		{
			name: "warn mode (default) — parse step has continue-on-error: true",
			frontmatter: `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
---

# Test

Create an issue.
`,
			wantContinueOnErr: true,
		},
		{
			name: "warn mode explicit — parse step has continue-on-error: true",
			frontmatter: `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
  threat-detection:
    continue-on-error: true
---

# Test

Create an issue.
`,
			wantContinueOnErr: true,
		},
		{
			name: "strict mode — parse step does NOT have continue-on-error",
			frontmatter: `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
  threat-detection:
    continue-on-error: false
---

# Test

Create an issue.
`,
			wantContinueOnErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "test-*")
			workflowPath := filepath.Join(tmpDir, "test-workflow.md")

			if err := os.WriteFile(workflowPath, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("Failed to compile: %v", err)
			}

			lockPath := stringutil.MarkdownToLockFile(workflowPath)
			yamlBytes, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("Failed to read compiled YAML: %v", err)
			}
			yamlStr := string(yamlBytes)

			detectionSection := extractJobSection(yamlStr, "detection")
			if detectionSection == "" {
				t.Fatal("Detection job not found in compiled YAML")
			}

			// Find the parse-and-conclude step in the detection section
			parseStepIdx := strings.Index(detectionSection, "id: detection_conclusion")
			if parseStepIdx < 0 {
				t.Fatal("Parse-and-conclude step (id: detection_conclusion) not found in detection job")
			}
			// continue-on-error appears AFTER the id: line, within the next 150 chars
			stepContext := detectionSection[parseStepIdx:min(len(detectionSection), parseStepIdx+150)]

			hasContinueOnErr := strings.Contains(stepContext, "continue-on-error: true")
			if tt.wantContinueOnErr && !hasContinueOnErr {
				t.Error("Expected parse step to have continue-on-error: true in warn mode")
			}
			if !tt.wantContinueOnErr && hasContinueOnErr {
				t.Error("Expected parse step to NOT have continue-on-error: true in strict mode")
			}
		})
	}
}

// TestDetectionJobDownloadsExperimentArtifact verifies that when experiments are declared,
// the detection job includes a step to download the experiment artifact.
func TestDetectionJobDownloadsExperimentArtifact(t *testing.T) {
	tests := []struct {
		name                   string
		frontmatter            string
		wantExperimentDownload bool
	}{
		{
			name: "experiments declared — detection job downloads experiment artifact",
			frontmatter: `---
on: issues
permissions:
  contents: read
engine: copilot
experiments:
  caveman: [yes, no]
safe-outputs:
  create-issue:
---

# Test

Create an issue.
`,
			wantExperimentDownload: true,
		},
		{
			name: "no experiments — detection job does not download experiment artifact",
			frontmatter: `---
on: issues
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
---

# Test

Create an issue.
`,
			wantExperimentDownload: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "test-*")
			workflowPath := filepath.Join(tmpDir, "test-workflow.md")

			if err := os.WriteFile(workflowPath, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("Failed to compile: %v", err)
			}

			lockPath := stringutil.MarkdownToLockFile(workflowPath)
			yamlBytes, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("Failed to read compiled YAML: %v", err)
			}
			detectionSection := extractJobSection(string(yamlBytes), "detection")
			if detectionSection == "" {
				t.Fatal("Detection job not found in compiled YAML")
			}

			hasDownload := strings.Contains(detectionSection, "Download experiment artifact")
			if tt.wantExperimentDownload && !hasDownload {
				t.Error("Expected detection job to download experiment artifact when experiments are declared")
			}
			if !tt.wantExperimentDownload && hasDownload {
				t.Error("Expected detection job NOT to download experiment artifact when no experiments are declared")
			}
		})
	}
}
