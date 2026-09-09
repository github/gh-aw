//go:build !integration

package workflow

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
)

func extractWorkflowStepByName(t *testing.T, workflowYAML, stepName string) string {
	t.Helper()

	marker := "- name: " + stepName
	nameIdx := strings.Index(workflowYAML, marker)
	if nameIdx == -1 {
		t.Fatalf("Expected %q step in generated workflow", stepName)
	}

	lineStart := nameIdx
	for lineStart > 0 && workflowYAML[lineStart-1] != '\n' {
		lineStart--
	}
	indent := workflowYAML[lineStart:nameIdx]
	stepSection := workflowYAML[lineStart:]
	nextStepMarker := "\n" + indent + "- name:"
	if next := strings.Index(stepSection[1:], nextStepMarker); next != -1 {
		stepSection = stepSection[:next+1]
	}
	return stepSection
}

func TestArtifactDownloadSingleArtifactUsesExactName(t *testing.T) {
	steps := strings.Join(buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName: "detection",
		DownloadPath: "/tmp/gh-aw/threat-detection/",
		StepName:     "Download detection artifact",
		StepID:       "download-detection-artifact",
	}, getActionPin), "")

	assert.Contains(t, steps, "name: detection", "single-artifact downloads should use exact names to avoid ambiguous multi-match merges")
	assert.NotContains(t, steps, "pattern: detection", "single-artifact downloads should not use pattern matching")
	assert.NotContains(t, steps, "merge-multiple: true", "single-artifact downloads should not merge multiple matches")
}

func TestArtifactDownloadFallbackUsesPatternWhenSupported(t *testing.T) {
	steps := strings.Join(buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName:     "agent",
		FallbackArtifact: "agent-output-fallback",
		DownloadPath:     "/tmp/gh-aw/",
		StepName:         "Download agent output artifact",
	}, getActionPin), "")

	assert.Contains(t, steps, `pattern: "{agent,agent-output-fallback}"`, "fallback downloads should intentionally match both candidate artifact names")
	assert.Contains(t, steps, "merge-multiple: true", "fallback downloads should merge both candidate artifact contents")
	assert.NotContains(t, steps, "name: agent\n", "fallback downloads should not use exact-name downloads when pattern matching is supported")
}

func TestArtifactDownloadFallbackUsesNameWithDownloadArtifactV3(t *testing.T) {
	steps := strings.Join(buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName:     "agent",
		FallbackArtifact: "agent-output-fallback",
		DownloadPath:     "/tmp/gh-aw/",
		StepName:         "Download agent output artifact",
	}, func(string) string {
		return "actions/download-artifact@a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59 # v3.1.0"
	}), "")

	assert.Contains(t, steps, "name: agent\n", "download-artifact v3 only supports exact name downloads")
	assert.NotContains(t, steps, "pattern:", "download-artifact v3 should not receive unsupported pattern input")
	assert.NotContains(t, steps, "merge-multiple:", "download-artifact v3 should not receive unsupported merge-multiple input")
}

func TestDownloadArtifactInputLinesUsePatternOnlyWhenSupported(t *testing.T) {
	v4Lines := strings.Join(downloadArtifactInputLines("evals", "actions/download-artifact@abc123 # v4.3.0"), "")
	assert.Contains(t, v4Lines, "pattern: evals")
	assert.Contains(t, v4Lines, "merge-multiple: true")
	assert.NotContains(t, v4Lines, "name: evals")

	v3Lines := strings.Join(downloadArtifactInputLines("evals", "actions/download-artifact@abc123 # v3.1.0"), "")
	assert.Contains(t, v3Lines, "name: evals")
	assert.NotContains(t, v3Lines, "pattern: evals")
	assert.NotContains(t, v3Lines, "merge-multiple: true")
}

func TestAccessLogUploadConditional(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name        string
		tools       map[string]any
		mcpServers  map[string]any
		expectSteps bool
	}{
		{
			name: "no tools - no access log steps",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
			},
			expectSteps: false,
		},
		{
			name: "mcp server with container but no network permissions - no access log steps",
			mcpServers: map[string]any{
				"simple": map[string]any{
					"container": "simple/tool",
					"allowed":   []any{"test"},
				},
			},
			expectSteps: false,
		},
		{
			name: "mcp server with container - no access log steps (proxy removed)",
			mcpServers: map[string]any{
				"fetch": map[string]any{
					"container": "mcp/fetch",
					"allowed":   []any{"fetch"},
				},
			},
			expectSteps: false, // Changed from true - per-tool proxy removed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder

			// Combine tools and mcpServers for testing
			testTools := tt.tools
			if testTools == nil {
				testTools = make(map[string]any)
			}
			if tt.mcpServers != nil {
				// Add mcp servers to tools map for the test
				maps.Copy(testTools, tt.mcpServers)
			}

			// Test generateExtractAccessLogs
			compiler.generateExtractAccessLogs(&yaml, testTools)
			extractContent := yaml.String()

			// Test generateUploadAccessLogs
			yaml.Reset()
			compiler.generateUploadAccessLogs(&yaml, testTools)
			uploadContent := yaml.String()

			hasExtractStep := strings.Contains(extractContent, "name: Extract squid access logs")
			hasUploadStep := strings.Contains(uploadContent, "name: Upload squid access logs")

			if tt.expectSteps {
				if !hasExtractStep {
					t.Errorf("Expected extract step to be generated but it wasn't")
				}
				if !hasUploadStep {
					t.Errorf("Expected upload step to be generated but it wasn't")
				}
			} else {
				if hasExtractStep {
					t.Errorf("Expected no extract step but one was generated")
				}
				if hasUploadStep {
					t.Errorf("Expected no upload step but one was generated")
				}
			}
		})
	}
}

// TestPullRequestForksArrayFilter tests the pull_request forks: []string filter functionality with glob support
func TestPostStepsIndentationFix(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "post-steps-indentation-test")

	// Test case with various post-steps configurations
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
post-steps:
  - name: First Post Step
    run: echo "first"
  - name: Second Post Step
    uses: actions/upload-artifact@v4 # SHA will be pinned
    with:
      name: test-artifact
      path: test-file.txt
      retention-days: 7
  - name: Third Post Step
    if: success()
    run: |
      echo "multiline"
      echo "script"
engine: claude
strict: false
---

# Test Post Steps Indentation

Test post-steps indentation fix.
`

	testFile := filepath.Join(tmpDir, "test-post-steps-indentation.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-post-steps-indentation.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify all post-steps are present
	if !strings.Contains(lockContent, "- name: First Post Step") {
		t.Error("Expected post-step 'First Post Step' to be in generated workflow")
	}
	if !strings.Contains(lockContent, "- name: Second Post Step") {
		t.Error("Expected post-step 'Second Post Step' to be in generated workflow")
	}
	// Note: "Third Post Step" has an 'if' condition, so it appears as "name: Third Post Step" not "- name:"
	if !strings.Contains(lockContent, "name: Third Post Step") {
		t.Error("Expected post-step 'Third Post Step' to be in generated workflow")
	}

	// Verify indentation is correct (6 spaces for list items, 8 for properties)
	// Only check non-comment lines (frontmatter is embedded as comments)
	lines := strings.Split(lockContent, "\n")
	for i, line := range lines {
		// Skip comment lines
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, "- name: First Post Step") {
			// Check that this line has exactly 6 leading spaces
			if !strings.HasPrefix(line, "      - name: First Post Step") {
				t.Errorf("Line %d: Expected 6 spaces before '- name: First Post Step', got: %q", i+1, line)
			}
			// Check the next non-comment line (run:) has 8 spaces
			for j := i + 1; j < len(lines); j++ {
				nextTrimmed := strings.TrimLeft(lines[j], " \t")
				if strings.HasPrefix(nextTrimmed, "#") {
					continue
				}
				nextLine := lines[j]
				if strings.Contains(nextLine, "run:") && !strings.HasPrefix(nextLine, "        run:") {
					t.Errorf("Line %d: Expected 8 spaces before 'run:', got: %q", j+1, nextLine)
				}
				break
			}
		}
		if strings.Contains(line, "- name: Second Post Step") {
			// Check that this line has exactly 6 leading spaces
			if !strings.HasPrefix(line, "      - name: Second Post Step") {
				t.Errorf("Line %d: Expected 6 spaces before '- name: Second Post Step', got: %q", i+1, line)
			}
			// Check subsequent non-comment lines have correct indentation
			checkIdx := 0
			for j := i + 1; j < len(lines) && checkIdx < 2; j++ {
				nextTrimmed := strings.TrimLeft(lines[j], " \t")
				if strings.HasPrefix(nextTrimmed, "#") {
					continue
				}
				if checkIdx == 0 && strings.Contains(lines[j], "uses:") {
					if !strings.HasPrefix(lines[j], "        uses:") {
						t.Errorf("Line %d: Expected 8 spaces before 'uses:', got: %q", j+1, lines[j])
					}
					checkIdx++
				} else if checkIdx == 1 && strings.Contains(lines[j], "with:") {
					if !strings.HasPrefix(lines[j], "        with:") {
						t.Errorf("Line %d: Expected 8 spaces before 'with:', got: %q", j+1, lines[j])
					}
					checkIdx++
				}
			}
		}
	}

	t.Log("Post-steps indentation verified successfully")
}

func TestPromptUploadArtifact(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "prompt-upload-test")

	// Create a test markdown file with basic frontmatter
	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
---

# Test Prompt Upload

This workflow should generate a unified artifact upload step that includes the prompt.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockYAML := string(lockContent)

	// Verify that the unified artifact upload step is present
	if !strings.Contains(lockYAML, "- name: Upload agent artifacts") {
		t.Error("Expected 'Upload agent artifacts' step to be in generated workflow")
	}

	// Verify the upload step uses the correct action
	if !strings.Contains(lockYAML, "uses: actions/upload-artifact@") { // SHA varies
		t.Error("Expected 'actions/upload-artifact' action to be used")
	}

	// Verify the unified artifact name
	if !strings.Contains(lockYAML, "name: agent\n") {
		t.Error("Expected artifact name to be 'agent'")
	}

	// Verify the prompt path is included in the unified upload
	if !strings.Contains(lockYAML, "/tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Error("Expected prompt path '/tmp/gh-aw/aw-prompts/prompt.txt' to be in unified upload")
	}

	// Verify the upload step has the if-no-files-found configuration set to ignore
	if !strings.Contains(lockYAML, "if-no-files-found: ignore") {
		t.Error("Expected 'if-no-files-found: ignore' in upload step")
	}

	// Verify the upload step runs always (with if: always())
	uploadStepIndex := strings.Index(lockYAML, "- name: Upload agent artifacts")
	if uploadStepIndex == -1 {
		t.Fatal("Upload agent artifacts step not found")
	}

	// Check for "if: always()" in the section after the upload step name
	afterUploadStep := lockYAML[uploadStepIndex:]
	nextStepIndex := strings.Index(afterUploadStep[20:], "- name:")
	if nextStepIndex == -1 {
		nextStepIndex = len(afterUploadStep) - 20
	}
	uploadStepSection := afterUploadStep[:20+nextStepIndex]

	if !strings.Contains(uploadStepSection, "if: always()") {
		t.Error("Expected 'if: always()' in upload agent artifacts step")
	}

	// Verify continue-on-error is set
	if !strings.Contains(uploadStepSection, "continue-on-error: true") {
		t.Error("Expected 'continue-on-error: true' in upload agent artifacts step")
	}

	t.Log("Unified artifact upload step verified successfully (includes prompt)")
}

func TestAmbientFoldersIncludedInActivationArtifact(t *testing.T) {
	tmpDir := testutil.TempDir(t, "ambient-folders-artifact-test")
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}
	sharedContent := `---
ambient-folders:
  - .squad
jobs:
  activation:
    pre-steps:
      - name: Create ambient folder
        run: |
          mkdir -p .squad
          echo team > .squad/team.md
---

# Shared ambient folder setup
`
	if err := os.WriteFile(filepath.Join(sharedDir, "ambient.md"), []byte(sharedContent), 0644); err != nil {
		t.Fatal(err)
	}

	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
imports:
  - shared/ambient.md
---

# Test Ambient Folders
`
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockYAML := string(lockContent)

	for _, want := range []string{
		"Stage ambient folders for activation artifact",
		"GH_AW_AMBIENT_FOLDERS: \".squad\"",
		"/tmp/gh-aw/ambient-folders",
		"Restore ambient folders from activation artifact",
		".squad",
	} {
		if !strings.Contains(lockYAML, want) {
			t.Fatalf("Expected compiled workflow to contain %q, got:\n%s", want, lockYAML)
		}
	}
}

func TestAmbientFoldersRestoredAfterCustomCheckout(t *testing.T) {
	tmpDir := testutil.TempDir(t, "ambient-folders-custom-checkout-test")
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}
	sharedContent := `---
ambient-folders:
  - .squad
jobs:
  activation:
    pre-steps:
      - name: Create ambient folder
        run: |
          mkdir -p .squad
          echo team > .squad/team.md
---

# Shared ambient folder setup
`
	if err := os.WriteFile(filepath.Join(sharedDir, "ambient.md"), []byte(sharedContent), 0644); err != nil {
		t.Fatal(err)
	}

	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
imports:
  - shared/ambient.md
steps:
  - name: Custom checkout
    uses: actions/checkout@v4
---

# Test Ambient Folders With Custom Checkout
`
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockYAML := string(lockContent)
	checkoutIndex := strings.Index(lockYAML, "name: Custom checkout")
	if checkoutIndex == -1 {
		t.Fatalf("Expected custom checkout step in compiled workflow, got:\n%s", lockYAML)
	}
	restoreIndex := strings.LastIndex(lockYAML, "Restore ambient folders from activation artifact")
	if restoreIndex == -1 {
		t.Fatalf("Expected ambient restore step in compiled workflow, got:\n%s", lockYAML)
	}
	if restoreIndex < checkoutIndex {
		t.Fatalf("Expected final ambient restore after custom checkout; checkout index %d, restore index %d", checkoutIndex, restoreIndex)
	}
}

func TestPatchIncludedInArtifactWhenThreatDetectionEnabled(t *testing.T) {
	// When push-to-pull-request-branch is staged, usesPatchesAndCheckouts() returns false.
	// But if threat detection is enabled, the detection job needs patches for security
	// analysis, so aw-*.patch must still be included in the agent artifact.
	tmpDir := testutil.TempDir(t, "patch-artifact-test")

	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  push-to-pull-request-branch:
    staged: true
---

# Test Patch Upload

Push some changes.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockYAML := string(lockContent)

	// Find the Upload agent artifacts step and verify aw-*.patch is in its paths
	uploadIdx := strings.Index(lockYAML, "- name: Upload agent artifacts")
	if uploadIdx == -1 {
		t.Fatal("Upload agent artifacts step not found")
	}

	// Extract the section up to the next step
	afterUpload := lockYAML[uploadIdx:]
	nextStep := strings.Index(afterUpload[30:], "- name:")
	if nextStep == -1 {
		nextStep = len(afterUpload) - 30
	}
	uploadSection := afterUpload[:30+nextStep]

	if !strings.Contains(uploadSection, "/tmp/gh-aw/aw-*.patch") {
		t.Error("Expected '/tmp/gh-aw/aw-*.patch' in unified artifact upload when threat detection is enabled with staged push-to-pull-request-branch")
	}
	if !strings.Contains(uploadSection, "/tmp/gh-aw/aw-*.bundle") {
		t.Error("Expected '/tmp/gh-aw/aw-*.bundle' in unified artifact upload when threat detection is enabled with staged push-to-pull-request-branch")
	}
}

// TestAgentOutputFallbackArtifact verifies that safe-output processing does not depend on the
// large "agent" artifact upload succeeding: a small dedicated artifact carries the agent output,
// and downstream jobs match both artifacts when downloading. See gh-aw#53099, where a timed-out
// upload of the agent artifact silently dropped every safe output.
func TestAgentOutputFallbackArtifact(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-output-fallback-test")

	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
graders:
  custom:
    script: return 1
safe-outputs:
  create-issue:
---

# Test Agent Output Fallback

Body.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockYAML := string(lockContent)

	uploadSection := extractWorkflowStepByName(t, lockYAML, "Upload agent output fallback artifact")
	for _, expected := range []string{
		"name: agent-output-fallback\n",
		"/tmp/gh-aw/agent_output.json",
		"/tmp/gh-aw/safeoutputs.jsonl",
		"/tmp/gh-aw/agent/graders/grader_manifest.json",
		"/tmp/gh-aw/agent/graders/grader_results.json",
		"if-no-files-found: ignore",
		"continue-on-error: true",
	} {
		if !strings.Contains(uploadSection, expected) {
			t.Errorf("Expected %q in fallback upload step, got:\n%s", expected, uploadSection)
		}
	}
	if strings.Contains(uploadSection, constants.OperationalValueEvaluatorFilename.String()) {
		t.Errorf("Fallback upload must not include an operational-value evaluator for inline-only graders, got:\n%s", uploadSection)
	}

	// The fallback artifact must be uploaded before the (large, failure-prone) agent artifact.
	uploadIdx := strings.Index(lockYAML, "- name: Upload agent output fallback artifact")
	agentUploadIdx := strings.Index(lockYAML, "- name: Upload agent artifacts")
	if agentUploadIdx == -1 {
		t.Fatal("Upload agent artifacts step not found")
	}
	if uploadIdx > agentUploadIdx {
		t.Error("Expected the agent output fallback upload to come before the unified agent artifact upload")
	}

	// Downstream jobs must match both artifacts so the agent output is still found when the
	// unified agent artifact upload failed.
	if !strings.Contains(lockYAML, `pattern: "{agent,agent-output-fallback}"`) {
		t.Error("Expected agent output download to match both the agent and fallback artifacts")
	}
	if !strings.Contains(lockYAML, "merge-multiple: true") {
		t.Error("Expected 'merge-multiple: true' so both artifacts extract into the same directory")
	}
}

func TestAgentArtifactExcludesUserGeneratedDirectory(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-artifact-paths-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
---

# Test Agent Artifact Paths

Body.
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	uploadSection := extractWorkflowStepByName(t, string(lockContent), "Upload agent artifacts")

	for _, expected := range []string{
		constants.TmpMcpLogsDir,
		constants.TmpSandboxAgentLogsDir,
		constants.AgentStdioLogPath,
		constants.AWFProxyLogsDir.String() + "/",
		constants.AWFAuditDir.String() + "/",
	} {
		assert.Contains(t, uploadSection, expected)
	}
	assert.NotContains(t, uploadSection, "\n            "+constants.TmpGhAwAgentDir+"\n")
}

func TestAgentOutputFallbackArtifact_NoSafeOutputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-output-fallback-no-safe-outputs-test")

	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
---

# Test No Agent Output Fallback

Body.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockYAML := string(lockContent)

	if strings.Contains(lockYAML, "Upload agent output fallback artifact") {
		t.Error("Expected no fallback artifact upload when safe-outputs is not declared")
	}
}
