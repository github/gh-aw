//go:build !integration

package workflow

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestThreatDetectionIsolation(t *testing.T) {
	compiler := NewCompiler()

	// Create a temporary directory for the test workflow
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-isolation.md")

	workflowContent := `---
on: push
safe-outputs:
  create-issue:
features:
  gh-aw-detection: false
tools:
  github:
    allowed: ["*"]
---
Test workflow`

	// Write the workflow file
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled output
	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	yamlStr := string(result)

	// Detection is now a separate detection job - agent job should NOT contain inline detection steps
	agentSection := extractJobSection(yamlStr, "agent")
	if agentSection == "" {
		t.Fatal("Agent job not found in compiled workflow")
	}

	// Test 1: Detection job should exist as a separate job
	detectionSection := extractJobSection(yamlStr, "detection")
	if detectionSection == "" {
		t.Error("Detection job should exist as a separate job")
	}
	if !strings.Contains(detectionSection, "detection_guard") {
		t.Error("Detection job should contain detection_guard step")
	}
	if !strings.Contains(detectionSection, "detection_conclusion") {
		t.Error("Detection job should contain detection_conclusion step")
	}
	clearSessionState := strings.Index(detectionSection, "Clear inherited Copilot session state")
	detectionExecution := strings.Index(detectionSection, "id: detection_agentic_execution")
	if clearSessionState == -1 ||
		!strings.Contains(detectionSection, "rm -rf /tmp/gh-aw/sandbox/agent/logs/copilot-session-state") ||
		detectionExecution == -1 || clearSessionState > detectionExecution {
		t.Error("Detection job should clear inherited agent checkpoints before detection")
	}
	uploadStepStart := strings.Index(detectionSection, "      - name: Upload threat detection log\n")
	if uploadStepStart == -1 {
		t.Error("Inline detection path must upload the threat detection artifact")
	} else {
		uploadStep := detectionSection[uploadStepStart:]
		if uploadStepEnd := strings.Index(uploadStep[1:], "\n      - name: "); uploadStepEnd != -1 {
			uploadStep = uploadStep[:uploadStepEnd+1]
		}
		if !strings.Contains(uploadStep, "            "+constants.TmpGhAwDir+"/threat-detection/detection_usage.json\n") ||
			!strings.Contains(uploadStep, "            "+constants.TmpGhAwDir+"/threat-detection/detection_usage.jsonl\n") {
			t.Error("Inline detection path must upload detection AI Credits accounting")
		}
	}

	// Test 2: Detection engine step should use limited tools (no --allow-all-tools)
	// The detection copilot invocation uses only shell tools for analysis
	if !strings.Contains(detectionSection, "parse_threat_detection_results.cjs") {
		t.Error("Detection job should contain parse_threat_detection_results.cjs for detection")
	}

	// Test 3: Main agent job should still have --allow-tool or --allow-all-tools for the main agent execution
	if !strings.Contains(agentSection, "--allow-tool") && !strings.Contains(agentSection, "--allow-all-tools") {
		t.Error("Main agent job should have --allow-tool or --allow-all-tools arguments")
	}

	// Test 4: Main agent job should have MCP setup
	if !strings.Contains(agentSection, "Start MCP Gateway") {
		t.Error("Main agent job should have MCP setup step")
	}

	// Test 5: A separate detection job should exist
	if !strings.Contains(yamlStr, "  detection:") {
		t.Error("Separate detection job should exist")
	}
}

// TestDetectionStepSummaryOverride verifies that the inline detection execution step
// overrides GITHUB_STEP_SUMMARY to ThreatDetectionStepSummaryPath at the step level,
// and that a dedicated echo step is emitted afterwards to allow the runner to mask secrets.
// This prevents the AWF chroot entrypoint from failing when it tries to write to the
// real runner file-commands path (which is not accessible inside the chroot).
func TestDetectionStepSummaryOverride(t *testing.T) {
	compiler := NewCompiler()

	tmpDir := testutil.TempDir(t, "test-detection-step-summary-*")
	workflowPath := filepath.Join(tmpDir, "test-detection-step-summary.md")

	workflowContent := `---
on: push
safe-outputs:
  create-issue:
  threat-detection:
    continue-on-error: true
features:
  gh-aw-detection: false
tools:
  github:
    allowed: ["*"]
---
Test workflow`

	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	yamlStr := string(result)
	detectionSection := extractJobSection(yamlStr, "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled workflow")
	}

	// Test 1: The detection execution step must override GITHUB_STEP_SUMMARY to the
	// detection-specific path (not the agent step summary path).
	if !strings.Contains(detectionSection, "GITHUB_STEP_SUMMARY: "+constants.ThreatDetectionStepSummaryPath) {
		t.Errorf("Detection execution step should set GITHUB_STEP_SUMMARY to %q", constants.ThreatDetectionStepSummaryPath)
	}
	if strings.Contains(detectionSection, "touch "+AgentStepSummaryPath) {
		t.Errorf("Detection execution step must not create unused agent step summary %q", AgentStepSummaryPath)
	}
	if strings.Contains(detectionSection, AgentStepSummaryPath) {
		t.Errorf("Detection execution step must not reference unused agent step summary %q", AgentStepSummaryPath)
	}

	// Test 2: A dedicated echo step for the detection step summary must be present.
	if !strings.Contains(detectionSection, "Echo detection step summary") {
		t.Error("Detection job should contain an echo step for the detection step summary")
	}

	// Test 4: The agent job must still use the original agent step summary path.
	agentSection := extractJobSection(yamlStr, "agent")
	if agentSection == "" {
		t.Fatal("Agent job not found in compiled workflow")
	}
	if !strings.Contains(agentSection, "GITHUB_STEP_SUMMARY: "+AgentStepSummaryPath) {
		t.Errorf("Agent job should still use AgentStepSummaryPath %q for GITHUB_STEP_SUMMARY", AgentStepSummaryPath)
	}
}

func TestCopilotStepSummaryPath(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		want         string
	}{
		{name: "agent", want: AgentStepSummaryPath},
		{name: "external detection", workflowData: &WorkflowData{IsDetectionRun: true}, want: AgentStepSummaryPath},
		{name: "inline detection", workflowData: &WorkflowData{IsDetectionRun: true, Features: map[string]any{"gh-aw-detection": false}}, want: constants.ThreatDetectionStepSummaryPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := copilotStepSummaryPath(tt.workflowData); got != tt.want {
				t.Errorf("copilotStepSummaryPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestExternalDetectorPath verifies that when features: gh-aw-detection: true is set,
// the compiler emits the external threat-detect binary path instead of the inline engine path.
func TestExternalDetectorPath(t *testing.T) {
	compiler := NewCompiler()

	tmpDir := testutil.TempDir(t, "test-external-detector-*")
	workflowPath := filepath.Join(tmpDir, "test-external-detector.md")

	workflowContent := `---
on: push
engine: copilot
safe-outputs:
  create-issue:
features:
  gh-aw-detection: true
tools:
  github:
    allowed: ["*"]
---
Test workflow`

	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	yamlStr := string(result)
	detectionSection := extractJobSection(yamlStr, "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled workflow")
	}

	// The external detector path must emit threat-detect conclude, not the .cjs module
	if strings.Contains(detectionSection, "parse_threat_detection_results.cjs") {
		t.Error("External detector path must NOT emit parse_threat_detection_results.cjs")
	}
	if !strings.Contains(detectionSection, "conclude_threat_detection.sh") {
		t.Error("External detector path must invoke conclude_threat_detection.sh for the conclude step")
	}
	if !strings.Contains(detectionSection, "GH_AW_DETECTION_CONTINUE_ON_ERROR") {
		t.Error("External detector path must pass GH_AW_DETECTION_CONTINUE_ON_ERROR to conclude_threat_detection.sh")
	}

	// The install step must reference the pinned version
	if !strings.Contains(detectionSection, "install_awf_binary.sh") {
		t.Error("External detector path must emit 'install_awf_binary.sh' install step")
	}
	if !strings.Contains(detectionSection, "install_threat_detect_binary.sh") {
		t.Error("External detector path must emit 'install_threat_detect_binary.sh' install step")
	}
	// In warn mode (continue-on-error default: true), the install step itself must be
	// continue-on-error so a transient download failure doesn't mark the detection job
	// as failure when the workflow logic already tolerates a missing binary.
	installStepBlock := extractInstallThreatDetectStepBlock(t, detectionSection)
	if !strings.Contains(installStepBlock, "continue-on-error: true") {
		t.Error("Install threat-detect binary step must set continue-on-error: true in warn mode")
	}
	if !strings.Contains(detectionSection, "install_copilot_cli.sh") {
		t.Error("External detector path must emit engine installation step for copilot")
	}
	// The install step must pass the DefaultThreatDetectVersion to the script
	if !strings.Contains(detectionSection, string(constants.DefaultThreatDetectVersion)) {
		t.Errorf("External detector path must use version %q from DefaultThreatDetectVersion", constants.DefaultThreatDetectVersion)
	}

	// The AWF execution step must use threat-detect as the command
	if !strings.Contains(detectionSection, "threat-detect --engine") {
		t.Error("External detector path must invoke 'threat-detect --engine' inside AWF")
	}

	// The AWF execution step must prepend npm PATH setup so npm-installed engine CLIs
	// (e.g. claude, codex) are found by threat-detect inside the AWF chroot.
	if !strings.Contains(detectionSection, "RUNNER_TOOL_CACHE") {
		t.Error("External detector AWF step must prepend npm PATH setup (RUNNER_TOOL_CACHE) so engine CLIs are on PATH")
	}

	// The upload step must include detection_result.json
	if !strings.Contains(detectionSection, "detection_result.json") {
		t.Error("External detector path must upload detection_result.json")
	}

	// The detection guard and detection_conclusion step must still exist (gate contract preserved)
	if !strings.Contains(detectionSection, "detection_guard") {
		t.Error("External detector path must contain detection_guard step")
	}
	if !strings.Contains(detectionSection, "detection_conclusion") {
		t.Error("External detector path must contain detection_conclusion step")
	}
	if !strings.Contains(detectionSection, "id: parse_detection_token_usage") {
		t.Error("External detector path must contain parse_detection_token_usage step so detection AIC is exported")
	}
	parseIdx := strings.Index(detectionSection, "id: parse_detection_token_usage")
	concludeIdx := strings.Index(detectionSection, "id: detection_conclusion")
	if concludeIdx == -1 || parseIdx >= concludeIdx {
		t.Error("External detector path must emit parse_detection_token_usage before detection_conclusion so detection AIC is exported")
	}

	// The rw mount for the threat-detection directory must be present
	if !strings.Contains(detectionSection, "/tmp/gh-aw/threat-detection:/tmp/gh-aw/threat-detection:rw") {
		t.Error("External detector path must include read-write mount for /tmp/gh-aw/threat-detection")
	}

	// The detector invocation must pass the artifacts directory positionally and write a structured result file.
	invocationNeedle := "threat-detect --engine "
	invocationIndex := strings.Index(detectionSection, invocationNeedle)
	if invocationIndex == -1 {
		t.Error("External detector path must invoke threat-detect with --engine")
	} else {
		invocationLineEnd := strings.Index(detectionSection[invocationIndex:], "\n")
		if invocationLineEnd == -1 {
			invocationLineEnd = len(detectionSection) - invocationIndex
		}
		invocationLine := detectionSection[invocationIndex : invocationIndex+invocationLineEnd]
		if !strings.Contains(invocationLine, " /tmp/gh-aw/threat-detection") {
			t.Error("External detector path must pass /tmp/gh-aw/threat-detection as the positional artifacts directory")
		}
	}
	if !strings.Contains(detectionSection, "--output /tmp/gh-aw/threat-detection/detection_result.json") {
		t.Error("External detector path must pass --output /tmp/gh-aw/threat-detection/detection_result.json to threat-detect")
	}

	// The external detector must NOT pass --step-summary to threat-detect: the flag was
	// removed upstream in threat-detect v0.4.5+ (github/gh-aw-threat-detection#792), which
	// no longer writes any step-summary output. Passing the removed flag would be a
	// no-op at best or a hard CLI-parse failure at worst.
	if strings.Contains(detectionSection, "--step-summary") {
		t.Error("External detector path must NOT pass --step-summary to threat-detect (flag removed upstream)")
	}

	// The step-summary file must NOT be recreated before AWF execution: threat-detect no
	// longer writes to it, so touching/removing it here would be dead code.
	if strings.Contains(detectionSection, constants.ThreatDetectionStepSummaryPath) {
		t.Errorf("External detector path must NOT reference %s (threat-detect no longer produces step-summary output)", constants.ThreatDetectionStepSummaryPath)
	}

	// The "Append detection step summary" host-side step must NOT be present: it existed
	// solely to copy the file threat-detect used to write via --step-summary into the real
	// $GITHUB_STEP_SUMMARY, and threat-detect no longer writes that file.
	if strings.Contains(detectionSection, "Append detection step summary") {
		t.Error("External detector path must NOT include 'Append detection step summary' step (threat-detect no longer produces step-summary output)")
	}

	// The step-summary file must NOT be included in the artifact upload: it is derived from
	// untrusted agent-influenced content and is already appended directly to
	// $GITHUB_STEP_SUMMARY by the "Append detection step summary" step above, so persisting
	// it as a downloadable artifact would be an unnecessary secret-exfiltration path.
	uploadStepStart := strings.Index(detectionSection, "      - name: Upload threat detection artifact\n")
	if uploadStepStart == -1 {
		t.Error("External detector path must include upload threat detection artifact step")
	} else {
		uploadStepEnd := strings.Index(detectionSection[uploadStepStart+1:], "\n      - name: ")
		uploadStep := detectionSection[uploadStepStart:]
		if uploadStepEnd != -1 {
			uploadStep = detectionSection[uploadStepStart : uploadStepStart+1+uploadStepEnd]
		}
		if !strings.Contains(uploadStep, "            "+constants.ThreatDetectionResultPath+"\n") {
			t.Errorf("External detector path must upload %s", constants.ThreatDetectionResultPath)
		}
		if !strings.Contains(uploadStep, "            "+constants.TmpGhAwDir+"/threat-detection/detection_usage.jsonl\n") {
			t.Error("External detector path must upload detection AI Credits accounting")
		}
		// The raw engine log (detection.log) and step-summary must NOT be uploaded on the
		// external detector path: both can contain content derived from the untrusted agent
		// transcript passed to the detection engine, and persisting them as a downloadable
		// artifact would be a secret-exfiltration path. Only the structured verdict is uploaded.
		if strings.Contains(uploadStep, constants.ThreatDetectionLogPath) {
			t.Errorf("External detector path must NOT include %s in upload artifact path block (secret-exfil risk)", constants.ThreatDetectionLogPath)
		}

		parseUsageStep := strings.Index(detectionSection, "      - name: Parse threat detection token usage for step summary\n")
		if parseUsageStep == -1 || uploadStepStart == -1 || parseUsageStep > uploadStepStart {
			t.Error("External detector path must parse detection usage before uploading the detection artifact")
		}
		if strings.Contains(uploadStep, constants.ThreatDetectionStepSummaryPath) {
			t.Errorf("External detector path must NOT include %s in upload artifact path block (secret-exfil risk)", constants.ThreatDetectionStepSummaryPath)
		}
	}

	// The AWF execution pipeline must preserve non-zero threat-detect exits.
	if !strings.Contains(detectionSection, "set -o pipefail") {
		t.Error("External detector AWF step must use set -o pipefail so non-zero threat-detect exits fail the step")
	}

	// The external detector run must inherit engine runtime env config (auth/model/etc).
	if !strings.Contains(detectionSection, "COPILOT_GITHUB_TOKEN:") {
		t.Error("External detector path must configure engine auth env like the agent job")
	}

}

// extractInstallThreatDetectStepBlock returns the "Install threat-detect binary" step block
// (from its "- name:" line up to, but not including, the next step's "- name:" line) within
// the given detection job section.
func extractInstallThreatDetectStepBlock(t *testing.T, detectionSection string) string {
	t.Helper()
	installStepIdx := strings.Index(detectionSection, "Install threat-detect binary")
	if installStepIdx == -1 {
		t.Fatal("Could not find 'Install threat-detect binary' step in detection section")
	}
	installStepBlock := detectionSection[installStepIdx:]
	if nextStepIdx := strings.Index(installStepBlock[1:], "\n      - name:"); nextStepIdx != -1 {
		installStepBlock = installStepBlock[:nextStepIdx+1]
	}
	return installStepBlock
}

// compileExternalDetectorWorkflow compiles a minimal gh-aw-detection workflow with the given
// threat-detection frontmatter snippet appended to safe-outputs, and returns the detection job
// section of the compiled lock file.
func compileExternalDetectorWorkflow(t *testing.T, threatDetectionYAML string) string {
	t.Helper()
	compiler := NewCompiler()

	tmpDir := testutil.TempDir(t, "test-external-detector-coe-*")
	workflowPath := filepath.Join(tmpDir, "test-external-detector-coe.md")

	workflowContent := "---\n" +
		"on: push\n" +
		"engine: copilot\n" +
		"safe-outputs:\n" +
		"  create-issue:\n" +
		threatDetectionYAML +
		"features:\n" +
		"  gh-aw-detection: true\n" +
		"tools:\n" +
		"  github:\n" +
		"    allowed: [\"*\"]\n" +
		"---\n" +
		"Test workflow"

	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	detectionSection := extractJobSection(string(result), "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled workflow")
	}
	return detectionSection
}

// TestExternalDetectorInstallStepContinueOnErrorStrictMode verifies that when
// threat-detection.continue-on-error is explicitly set to false (strict mode), the
// "Install threat-detect binary" step must NOT carry continue-on-error: so a download
// failure fails the detection job, matching the job's own strict-mode intolerance of
// detection failures.
func TestExternalDetectorInstallStepContinueOnErrorStrictMode(t *testing.T) {
	detectionSection := compileExternalDetectorWorkflow(t, "  threat-detection:\n    continue-on-error: false\n")
	installStepBlock := extractInstallThreatDetectStepBlock(t, detectionSection)
	if strings.Contains(installStepBlock, "continue-on-error") {
		t.Error("Install threat-detect binary step must NOT set continue-on-error in strict mode")
	}
}

// TestExternalDetectorInstallStepContinueOnErrorExpressionMode verifies that when
// threat-detection.continue-on-error is a runtime expression, the "Install threat-detect
// binary" step emits that exact expression rather than a literal true/false value.
func TestExternalDetectorInstallStepContinueOnErrorExpressionMode(t *testing.T) {
	detectionSection := compileExternalDetectorWorkflow(t, "  threat-detection:\n    continue-on-error: ${{ inputs.coe }}\n")
	installStepBlock := extractInstallThreatDetectStepBlock(t, detectionSection)
	if !strings.Contains(installStepBlock, "continue-on-error: ${{ inputs.coe }}") {
		t.Error("Install threat-detect binary step must emit the configured continue-on-error expression")
	}
}

// TestExternalDetectorConcludeDoesNotReceiveStepSummaryFlag verifies that
// threat-detect conclude (which runs on the host where GITHUB_STEP_SUMMARY is
// writable) is NOT given --step-summary. The flag is only needed for the sandboxed
// execution step; conclude reads $GITHUB_STEP_SUMMARY by default and that works
// correctly on the host. This asymmetry must not be "cleaned up" by adding the flag
// to conclude as well.
func TestExternalDetectorConcludeDoesNotReceiveStepSummaryFlag(t *testing.T) {
	compiler := NewCompiler()

	tmpDir := testutil.TempDir(t, "test-external-conclude-*")
	workflowPath := filepath.Join(tmpDir, "test-conclude.md")

	workflowContent := `---
on: push
engine: copilot
safe-outputs:
  create-issue:
features:
  gh-aw-detection: true
tools:
  github:
    allowed: ["*"]
---
Test workflow`

	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	yamlStr := string(result)
	detectionSection := extractJobSection(yamlStr, "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled workflow")
	}

	// Locate the conclude step shell invocation.
	concludeIdx := strings.Index(detectionSection, "conclude_threat_detection.sh")
	if concludeIdx == -1 {
		t.Fatal("conclude_threat_detection.sh not found in detection section")
	}
	concludeLineEnd := strings.Index(detectionSection[concludeIdx:], "\n")
	if concludeLineEnd == -1 {
		concludeLineEnd = len(detectionSection) - concludeIdx
	}
	concludeLine := detectionSection[concludeIdx : concludeIdx+concludeLineEnd]

	// conclude runs on the host: GITHUB_STEP_SUMMARY is writable there and the flag is not needed.
	if strings.Contains(concludeLine, "--step-summary") {
		t.Errorf("conclude_threat_detection.sh must NOT receive --step-summary (runs on host); got: %s", concludeLine)
	}
}

func TestExternalDetectorPathUsesCopilotForPiWorkflows(t *testing.T) {
	compiler := NewCompiler()

	tmpDir := testutil.TempDir(t, "test-external-detector-pi-*")
	workflowPath := filepath.Join(tmpDir, "test-external-detector-pi.md")

	workflowContent := `---
on: push
engine:
  id: pi
  model: copilot/gpt-5.4
safe-outputs:
  create-issue:
features:
  gh-aw-detection: true
tools:
  github:
    mode: gh-proxy
  cli-proxy: true
---
Test workflow`

	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	detectionSection := extractJobSection(string(result), "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled workflow")
	}

	if !strings.Contains(detectionSection, "install_copilot_cli.sh") {
		t.Error("Pi external detector path must install the Copilot engine")
	}
	if strings.Contains(detectionSection, "@earendil-works/pi-coding-agent") {
		t.Error("Pi external detector path must not install the Pi engine")
	}
	if !strings.Contains(detectionSection, "threat-detect --engine copilot") {
		t.Error("Pi external detector path must invoke threat-detect with the copilot engine")
	}
	if strings.Contains(detectionSection, "threat-detect --engine pi") {
		t.Error("Pi external detector path must not invoke threat-detect with the pi engine")
	}
	if !strings.Contains(detectionSection, "COPILOT_GITHUB_TOKEN:") {
		t.Error("Pi external detector path must inherit Copilot auth env")
	}
}

func TestInlineDetectionUsesCopilotForPiWorkflows(t *testing.T) {
	compiler := NewCompiler()

	tmpDir := testutil.TempDir(t, "test-inline-detector-pi-*")
	workflowPath := filepath.Join(tmpDir, "test-inline-detector-pi.md")

	workflowContent := `---
on: push
engine:
  id: pi
  model: copilot/gpt-5.4
safe-outputs:
  create-issue:
tools:
  github:
    mode: gh-proxy
  cli-proxy: true
---
Test workflow`

	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	detectionSection := extractJobSection(string(result), "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled workflow")
	}

	if !strings.Contains(detectionSection, "install_copilot_cli.sh") {
		t.Error("Pi inline detection path must install the Copilot engine")
	}
	if strings.Contains(detectionSection, "@earendil-works/pi-coding-agent") {
		t.Error("Pi inline detection path must not install the Pi engine")
	}
	if !strings.Contains(detectionSection, "COPILOT_GITHUB_TOKEN:") {
		t.Error("Pi inline detection path must inherit Copilot auth env")
	}
}

func TestExternalDetectorPathPreparesCodexConfig(t *testing.T) {
	compiler := NewCompiler()

	tmpDir := testutil.TempDir(t, "test-external-detector-codex-*")
	workflowPath := filepath.Join(tmpDir, "test-external-detector-codex.md")

	workflowContent := `---
on: push
engine:
  id: codex
  model-provider: github
model: auto
permissions:
  copilot-requests: write
safe-outputs:
  create-issue:
features:
  gh-aw-detection: true
tools:
  github:
    mode: gh-proxy
---
Test workflow`

	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	detectionSection := extractJobSection(string(result), "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled workflow")
	}

	if !strings.Contains(detectionSection, "Prepare Codex config for threat-detect") {
		t.Error("Codex external detector path must prepare Codex config files before execution")
	}
	if !strings.Contains(detectionSection, string(constants.ShellMcpServersJsonPath)) {
		t.Error("Codex external detector path must create an empty mcp-servers.json for Codex")
	}
	if !strings.Contains(detectionSection, string(constants.TmpMcpConfigDir)+"/config.toml") {
		t.Error("Codex external detector path must create a writable CODEX_HOME config.toml")
	}
	if !strings.Contains(detectionSection, "model_provider = \"openai-proxy\"") {
		t.Error("Codex external detector path must route Codex through the AWF OpenAI proxy")
	}
	if !strings.Contains(detectionSection, "api_base = \"http://") {
		t.Error("Codex external detector path must set api_base in config.toml")
	}
	if !strings.Contains(detectionSection, "wss_base = \"ws://") {
		t.Error("Codex external detector path must set wss_base in config.toml")
	}
	if !strings.Contains(detectionSection, "supports_websockets = false") {
		t.Error("Codex external detector path must disable websocket startup for the proxy config")
	}
	if !strings.Contains(detectionSection, "env_key = \"CODEX_API_KEY\"") {
		t.Error("Codex external detector path must use the Codex BYOK API key")
	}
	if !strings.Contains(detectionSection, "wire_api = \"responses\"") {
		t.Error("Codex external detector path must use the Responses API")
	}
	if !strings.Contains(detectionSection, "requires_openai_auth = false") {
		t.Error("Codex external detector path must use BYOK authentication")
	}
	expectedCopilotBaseURL := "http://" + net.JoinHostPort(constants.AWFAPIProxyContainerIP, strconv.Itoa(constants.CopilotLLMGatewayPort))
	if !strings.Contains(detectionSection, `base_url = "`+expectedCopilotBaseURL+`"`) {
		t.Errorf("Codex external detector path must use the reflected Copilot provider endpoint %q", expectedCopilotBaseURL)
	}
	if !strings.Contains(detectionSection, `export CODEX_API_KEY="$`+constants.CopilotBYOKDummyAPIKeyEnvVar+`" &&`) {
		t.Error("Codex external detector path must activate the BYOK proxy route")
	}
	if !strings.Contains(detectionSection, "--exclude-env COPILOT_GITHUB_TOKEN") {
		t.Error("Codex external detector path must keep the GitHub token out of the sandbox")
	}
}

// TestExternalDetectorCodexConfigUsesOpenAIProxyPort verifies that the detection
// config.toml pins Codex to an ingress that speaks the OpenAI Responses wire API.
// Pointing it at the Anthropic ingress (port 10001) makes every detection request
// fail with 403 "Credentials for Anthropic (port 10001) are not configured", so the
// engine never produces a verdict and the detection job fails with ERR_SYSTEM.
func TestExternalDetectorCodexConfigUsesOpenAIProxyPort(t *testing.T) {
	tests := []struct {
		name       string
		engineYAML string
	}{
		{
			name:       "default provider",
			engineYAML: "engine:\n  id: codex\n",
		},
		{
			name:       "anthropic provider override",
			engineYAML: "engine:\n  id: codex\n  model-provider: anthropic\n",
		},
	}

	openAIBaseURL := "http://" + net.JoinHostPort(constants.AWFAPIProxyContainerIP, strconv.Itoa(constants.CodexLLMGatewayPort))
	anthropicHostPort := net.JoinHostPort(constants.AWFAPIProxyContainerIP, strconv.Itoa(constants.ClaudeLLMGatewayPort))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			tmpDir := testutil.TempDir(t, "test-external-detector-codex-port-*")
			workflowPath := filepath.Join(tmpDir, "test-codex-port.md")

			workflowContent := `---
on: push
` + tt.engineYAML + `safe-outputs:
  create-issue:
features:
  gh-aw-detection: true
---
Test workflow`

			if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}
			if err := compiler.CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			result, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
			if err != nil {
				t.Fatalf("Failed to read compiled workflow: %v", err)
			}

			detectionSection := extractJobSection(string(result), "detection")
			if detectionSection == "" {
				t.Fatal("Detection job not found in compiled workflow")
			}

			for _, key := range []string{"base_url", "api_base"} {
				expected := key + ` = "` + openAIBaseURL + `"`
				if !strings.Contains(detectionSection, expected) {
					t.Errorf("Codex detection config must set %s to the OpenAI ingress (%s)", key, openAIBaseURL)
				}
			}
			expectedWSS := `wss_base = "ws://` + net.JoinHostPort(constants.AWFAPIProxyContainerIP, strconv.Itoa(constants.CodexLLMGatewayPort)) + `"`
			if !strings.Contains(detectionSection, expectedWSS) {
				t.Errorf("Codex detection config must set wss_base to the OpenAI ingress (%s)", expectedWSS)
			}
			if strings.Contains(detectionSection, `base_url = "http://`+anthropicHostPort+`"`) {
				t.Errorf("Codex detection config must never point at the Anthropic ingress (%s)", anthropicHostPort)
			}
			if !strings.Contains(detectionSection, `CODEX_API_KEY: ${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}`) {
				t.Error("Codex detection execution must use the OpenAI credential expression")
			}
			if strings.Contains(detectionSection, `CODEX_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}`) {
				t.Error("Codex detection execution must never use the Anthropic credential expression")
			}
		})
	}
}

// TestExternalDetectorCodexConfigModelProviderAtRoot verifies that the top-level
// model_provider selector is emitted before any TOML table header ([history],
// [model_providers.*]). If it appears after [history], TOML parses it as
// history.model_provider, which Codex ignores, causing it to fall back to the
// default openai provider and bypass the AWF api-proxy sidecar (401 Unauthorized).
func TestExternalDetectorCodexConfigModelProviderAtRoot(t *testing.T) {
	config := buildExternalDetectorCodexConfig("http://172.30.0.30:10000", "ws://172.30.0.30:10000")

	// Strip any leading whitespace from each line so the config reads as plain
	// TOML regardless of the exact indentation used in the source template.
	var trimmedLines []string
	for line := range strings.SplitSeq(config, "\n") {
		trimmedLines = append(trimmedLines, strings.TrimLeft(line, " \t"))
	}
	toml := strings.Join(trimmedLines, "\n")

	expected := "model_provider = \"" + codexOpenAIProxyProviderID + "\""
	modelProviderIndex := strings.Index(toml, expected)
	if modelProviderIndex == -1 {
		t.Fatalf("Expected model_provider selector in detection config, got:\n%s", toml)
	}

	// Find the first real TOML table header by scanning line-by-line; this
	// avoids matching `[` that appears inside quoted string values or comments.
	firstTableIndex := -1
	lineStart := 0
	for line := range strings.SplitSeq(toml, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			firstTableIndex = lineStart
			break
		}
		lineStart += len(line) + 1 // +1 for the newline
	}
	if firstTableIndex == -1 {
		t.Fatalf("Expected at least one TOML table header in detection config, got:\n%s", toml)
	}
	// The top-level model_provider must precede any table header so TOML assigns
	// it to the document root rather than the most recent table.
	if modelProviderIndex > firstTableIndex {
		t.Errorf("model_provider selector must appear before any table header (e.g. [history]) so TOML assigns it to the document root, got:\n%s", toml)
	}

	// Guard against regression: the [history] table section must not contain a
	// model_provider key. Extract the [history] body line-by-line so that `[`
	// characters inside quoted values or arrays do not prematurely truncate it.
	var historyLines []string
	inHistory := false
	for line := range strings.SplitSeq(toml, "\n") {
		if line == "[history]" {
			inHistory = true
			continue
		}
		if inHistory {
			if strings.HasPrefix(strings.TrimSpace(line), "[") {
				break
			}
			historyLines = append(historyLines, line)
		}
	}
	if !inHistory {
		t.Fatalf("Expected [history] table in detection config, got:\n%s", toml)
	}
	historyBody := strings.Join(historyLines, "\n")
	if strings.Contains(historyBody, "model_provider") {
		t.Errorf("[history] table must NOT contain a model_provider key, got:\n%s", historyBody)
	}
}

// TestExternalDetectorCodexFirewallDomains verifies that the Codex external detection
// path includes the required API domains in the AWF firewall allowlist.
// Without the correct allowed domains the firewall blocks api.openai.com, chatgpt.com
// and api.github.com and the detection job exits with code 1/2 (engine_error).
func TestExternalDetectorCodexFirewallDomains(t *testing.T) {
	compiler := NewCompiler()

	tmpDir := testutil.TempDir(t, "test-external-detector-codex-fw-*")
	workflowPath := filepath.Join(tmpDir, "test-codex-fw.md")

	workflowContent := `---
on: push
engine: codex
safe-outputs:
  create-issue:
features:
  gh-aw-detection: true
tools:
  github:
    mode: gh-proxy
---
Test workflow`

	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	result, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	detectionSection := extractJobSection(string(result), "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled workflow")
	}

	allowDomainsLine := ""
	for line := range strings.SplitSeq(detectionSection, "\n") {
		if strings.Contains(line, "allowDomains") {
			allowDomainsLine = line
			break
		}
	}
	if allowDomainsLine == "" {
		t.Fatal("Detection job AWF config must include network.allowDomains")
	}

	// Assert against the specific AWF allowDomains JSON line to avoid false positives
	// from unrelated occurrences elsewhere in the rendered detection job.
	for _, domain := range []string{"api.openai.com", "chatgpt.com", "openai.com", "github.com", "api.github.com"} {
		if !strings.Contains(allowDomainsLine, domain) {
			t.Errorf("Codex external detector AWF allowDomains must include %q", domain)
		}
	}
}
