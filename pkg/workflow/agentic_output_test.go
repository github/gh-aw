//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestAgenticOutputCollection(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "agentic-output-test")

	// Test case with agentic output collection for Claude engine
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
safe-outputs:
  add-labels:
    allowed: ["bug", "enhancement"]
---

# Test Agentic Output Collection

This workflow tests the agentic output collection functionality.
`

	testFile := filepath.Join(tmpDir, "test-agentic-output.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with agentic output: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-agentic-output.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify GH_AW_SAFE_OUTPUTS is set via the "Set runtime paths" step (not job-level env,
	// because runner context is unavailable in job-level env: blocks).
	if !strings.Contains(lockContent, `echo "GH_AW_SAFE_OUTPUTS=${RUNNER_TEMP}/gh-aw/safeoutputs/outputs.jsonl"`) {
		t.Error("Expected GH_AW_SAFE_OUTPUTS to be set via 'Set runtime paths' step using $GITHUB_OUTPUT")
	}
	if !strings.Contains(lockContent, `echo "RUNNER_TOOL_CACHE=${GH_AW_RUNNER_TOOL_CACHE}" >> "$GITHUB_ENV"`) {
		t.Error("Expected RUNNER_TOOL_CACHE to be exported via 'Set runtime paths' for later MCP gateway and agent steps")
	}
	if !strings.Contains(lockContent, "GH_AW_RUNNER_TOOL_CACHE: ${{ runner.tool_cache }}") {
		t.Error("Expected runner.tool_cache to be passed through the step environment")
	}
	runStart := strings.Index(lockContent, "        run: |\n")
	if runStart < 0 {
		t.Fatal("Expected 'Set runtime paths' step to contain a run script")
	}
	runEnd := strings.Index(lockContent[runStart:], "\n      - ")
	if runEnd < 0 {
		t.Fatal("Expected generated run script to be followed by another step")
	}
	runScript := lockContent[runStart : runStart+runEnd]
	if strings.Contains(runScript, "${{ runner.tool_cache }}") {
		t.Error("runner.tool_cache must not be interpolated directly in the shell script")
	}

	if !strings.Contains(lockContent, "- name: Ingest agent output") {
		t.Error("Expected 'Ingest agent output' step to be in generated workflow")
	}

	// Upload Safe Outputs and Upload sanitized agent output are now merged into the
	// unified 'agent' artifact — individual upload steps no longer exist.
	if strings.Contains(lockContent, "- name: Upload Safe Outputs\n") {
		t.Error("Upload Safe Outputs should be removed (merged into unified agent artifact)")
	}

	if strings.Contains(lockContent, "- name: Upload sanitized agent output") {
		t.Error("Upload sanitized agent output should be removed (merged into unified agent artifact)")
	}

	// Instead, verify that the Copy Safe Outputs step is present
	if !strings.Contains(lockContent, "- name: Copy Safe Outputs") {
		t.Error("Expected 'Copy Safe Outputs' step to be in generated workflow")
	}

	// Verify job output declaration for GH_AW_SAFE_OUTPUTS
	if !strings.Contains(lockContent, "output: ${{ steps.collect_output.outputs.output }}") {
		t.Error("Expected job output declaration for 'output'")
	}

	// Verify has_patch output is declared
	if !strings.Contains(lockContent, "has_patch: ${{ steps.collect_output.outputs.has_patch }}") {
		t.Error("Expected job output declaration for 'has_patch'")
	}

	// Verify GH_AW_SAFE_OUTPUTS is passed to Claude (via step output, not GITHUB_ENV)
	if !strings.Contains(lockContent, "GH_AW_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS environment variable to be passed to engine via step output")
	}

	// Verify GH_AW_SAFE_OUTPUTS is also present in the "Parse agent logs for step summary" step
	// so the log parser can detect safe-output entries and downgrade a "no structured log entries"
	// failure to a warning when the agent demonstrably completed (e.g. emitted a noop).
	parseAgentLogsIdx := strings.Index(lockContent, "- name: Parse agent logs for step summary")
	if parseAgentLogsIdx < 0 {
		t.Error("Expected 'Parse agent logs for step summary' step to be in generated workflow")
	} else {
		// Find the next step boundary after the parse step
		nextStepIdx := strings.Index(lockContent[parseAgentLogsIdx+1:], "      - name: ")
		var parseAgentLogsSection string
		if nextStepIdx >= 0 {
			parseAgentLogsSection = lockContent[parseAgentLogsIdx : parseAgentLogsIdx+1+nextStepIdx]
		} else {
			parseAgentLogsSection = lockContent[parseAgentLogsIdx:]
		}
		if !strings.Contains(parseAgentLogsSection, "GH_AW_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}") {
			t.Errorf("Expected GH_AW_SAFE_OUTPUTS to be set in 'Parse agent logs for step summary' step env, got:\n%s", parseAgentLogsSection)
		}
	}

	// NOTE: Safe outputs instructions are now provided via the MCP server tool discovery,
	// so we no longer inject output instructions into the prompt directly.

	// Verify Claude engine no longer has upload steps (Claude CLI no longer produces output.txt)
	if strings.Contains(lockContent, "- name: Upload engine output files") {
		t.Error("Claude workflow should NOT have 'Upload engine output files' step (Claude CLI no longer produces output.txt)")
	}

	if strings.Contains(lockContent, "name: agent_outputs") {
		t.Error("Claude workflow should NOT reference 'agent_outputs' artifact (Claude CLI no longer produces output.txt)")
	}

	// Verify the unified agent artifact is used (not separate safe-output / agent-output artifacts)
	if strings.Contains(lockContent, "name: safe-output\n") {
		t.Errorf("Safe outputs should now be in the unified agent artifact, not a separate '%s' artifact", constants.SafeOutputArtifactName)
	}
	if !strings.Contains(lockContent, "name: agent\n") {
		t.Error("Expected unified 'agent' artifact name in generated workflow")
	}

	t.Log("Claude workflow correctly uses unified agent artifact for safe outputs")
}

func TestCodexEngineWithOutputSteps(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "codex-no-output-test")

	// Test case with Codex engine (should have GH_AW_SAFE_OUTPUTS but no engine output collection)
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: codex
strict: false
safe-outputs:
  add-labels:
    allowed: ["bug", "enhancement"]
---

# Test Codex No Engine Output Collection

This workflow tests that Codex engine gets GH_AW_SAFE_OUTPUTS but not engine output collection.
`

	testFile := filepath.Join(tmpDir, "test-codex-no-output.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with Codex: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-codex-no-output.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify that Codex workflow DOES have GH_AW_SAFE_OUTPUTS functionality via "Set runtime paths" step
	// (not job-level env, because runner context is unavailable in job-level env: blocks).
	if !strings.Contains(lockContent, `echo "GH_AW_SAFE_OUTPUTS=${RUNNER_TEMP}/gh-aw/safeoutputs/outputs.jsonl"`) {
		t.Error("Codex workflow should set GH_AW_SAFE_OUTPUTS via 'Set runtime paths' step (GH_AW_SAFE_OUTPUTS functionality)")
	}

	if !strings.Contains(lockContent, "- name: Ingest agent output") {
		t.Error("Codex workflow should have 'Ingest agent output' step (GH_AW_SAFE_OUTPUTS functionality)")
	}

	// Upload Safe Outputs and Upload sanitized agent output are now merged into the
	// unified 'agent' artifact — individual upload steps no longer exist.
	if strings.Contains(lockContent, "- name: Upload Safe Outputs\n") {
		t.Error("Upload Safe Outputs should be removed (merged into unified agent artifact)")
	}

	if strings.Contains(lockContent, "- name: Upload sanitized agent output") {
		t.Error("Upload sanitized agent output should be removed (merged into unified agent artifact)")
	}

	// The Copy Safe Outputs step should be present instead
	if !strings.Contains(lockContent, "- name: Copy Safe Outputs") {
		t.Error("Codex workflow should have 'Copy Safe Outputs' step (GH_AW_SAFE_OUTPUTS functionality)")
	}

	if !strings.Contains(lockContent, "GH_AW_SAFE_OUTPUTS") {
		t.Error("Codex workflow should reference GH_AW_SAFE_OUTPUTS environment variable")
	}

	// Safe outputs are now in the unified agent artifact, not a separate safe-output artifact
	if strings.Contains(lockContent, "name: safe-output\n") {
		t.Errorf("Safe outputs should now be in the unified agent artifact, not a separate '%s' artifact", constants.SafeOutputArtifactName)
	}
	if !strings.Contains(lockContent, "name: agent\n") {
		t.Error("Codex workflow should reference unified 'agent' artifact")
	}

	// Verify that job outputs section includes output for GH_AW_SAFE_OUTPUTS
	if !strings.Contains(lockContent, "output: ${{ steps.collect_output.outputs.output }}") {
		t.Error("Codex workflow should have job output declaration for 'output' (GH_AW_SAFE_OUTPUTS)")
	}

	// Verify has_patch output is declared
	if !strings.Contains(lockContent, "has_patch: ${{ steps.collect_output.outputs.has_patch }}") {
		t.Error("Codex workflow should have job output declaration for 'has_patch'")
	}

	// Verify that Codex engine output files are now included in the unified agent artifact
	// (no separate agent_outputs artifact upload step)
	if strings.Contains(lockContent, "- name: Upload engine output files") {
		t.Error("Codex workflow should NOT have separate 'Upload engine output files' step (merged into unified agent artifact)")
	}

	if strings.Contains(lockContent, "name: agent_outputs") {
		t.Error("Codex workflow should NOT reference 'agent_outputs' artifact (merged into unified agent artifact)")
	}

	// Engine output paths should appear in the unified artifact paths
	if !strings.Contains(lockContent, "name: agent\n") {
		t.Error("Codex workflow should reference unified 'agent' artifact containing engine output files")
	}

	// Verify that the Codex execution step is still present
	if !strings.Contains(lockContent, "- name: Execute Codex CLI") {
		t.Error("Expected 'Execute Codex CLI' step to be in generated workflow")
	}

	t.Log("Codex workflow correctly uses unified agent artifact for safe outputs and engine output files")
}

func TestEngineOutputFileDeclarations(t *testing.T) {
	// Test Claude engine declares claudeDebugLogFile for debug log collection
	claudeEngine := NewClaudeEngine()
	claudeOutputFiles := claudeEngine.GetDeclaredOutputFiles()

	if len(claudeOutputFiles) != 1 || claudeOutputFiles[0] != claudeDebugLogFile {
		t.Errorf("Claude engine should declare [%s], got: %v", claudeDebugLogFile, claudeOutputFiles)
	}

	// Test Codex engine declares output files for log collection
	codexEngine := NewCodexEngine()
	codexOutputFiles := codexEngine.GetDeclaredOutputFiles()

	if len(codexOutputFiles) == 0 {
		t.Errorf("Codex engine should declare output files for log collection, got: %v", codexOutputFiles)
	}

	if len(codexOutputFiles) > 0 && codexOutputFiles[0] != "/tmp/gh-aw/mcp-config/logs/" {
		t.Errorf("Codex engine should declare /tmp/gh-aw/mcp-config/logs/, got: %v", codexOutputFiles[0])
	}

	// Test Gemini engine declares output files for error log collection
	geminiEngine := NewGeminiEngine()
	geminiOutputFiles := geminiEngine.GetDeclaredOutputFiles()

	if len(geminiOutputFiles) == 0 {
		t.Error("Gemini engine should declare output files for error log collection")
	}

	if len(geminiOutputFiles) > 0 && geminiOutputFiles[0] != "/tmp/gh-aw/gemini-client-error-*.json" {
		t.Errorf("Gemini engine should declare /tmp/gh-aw/gemini-client-error-*.json, got: %v", geminiOutputFiles[0])
	}

	t.Logf("Claude engine declares: %v", claudeOutputFiles)
	t.Logf("Codex engine declares: %v", codexOutputFiles)
	t.Logf("Gemini engine declares: %v", geminiOutputFiles)
}

func TestEngineOutputCleanupExcludesTmpFiles(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "engine-output-cleanup-test")

	// Create a test markdown file with Copilot engine (which declares /tmp/gh-aw/.agent/logs/ as output file)
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: copilot
strict: false
---

# Test Engine Output Cleanup

This workflow tests that /tmp/gh-aw/ files are excluded from cleanup.
`

	testFile := filepath.Join(tmpDir, "test-engine-output-cleanup.md")
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify that the upload step includes the /tmp/gh-aw/ path (artifact should still be uploaded)
	if !strings.Contains(lockStr, "/tmp/gh-aw/sandbox/agent/logs/") {
		t.Error("Expected upload artifact path to include '/tmp/gh-aw/sandbox/agent/logs/' in generated workflow")
	}

	// Verify that the cleanup step does NOT include rm commands for /tmp/gh-aw/ paths
	if strings.Contains(lockStr, "rm -fr /tmp/gh-aw/sandbox/agent/logs/") {
		t.Error("Cleanup step should NOT include 'rm -fr /tmp/gh-aw/sandbox/agent/logs/' command")
	}

	// Verify that cleanup step does NOT exist when all files are in /tmp/gh-aw/
	if strings.Contains(lockStr, "- name: Clean up engine output files") {
		t.Error("Cleanup step should NOT be present when all output files are in /tmp/gh-aw/")
	}

	t.Log("Successfully verified that /tmp/gh-aw/ files are excluded from cleanup step while still being uploaded as artifacts")
}

func TestClaudeEngineNetworkHookCleanup(t *testing.T) {
	engine := NewClaudeEngine()

	// Note: With AWF integration, we no longer generate Python hooks for network permissions.
	// Instead, AWF wraps the Claude CLI command directly. This test verifies that
	// no cleanup steps are generated since hooks are no longer used.

	t.Run("No hook cleanup with Claude engine and network permissions (AWF mode)", func(t *testing.T) {
		// Test data with Claude engine and network permissions with firewall enabled
		data := &WorkflowData{
			Name:  "test-workflow",
			Model: "claude-3-5-sonnet-20241022",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed:  []string{"example.com", "*.trusted.com"},
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/th-aw/test.log")

		// Convert all steps to string for analysis
		var allStepsStr strings.Builder
		for _, step := range steps {
			allStepsStr.WriteString(strings.Join(step, "\n"))
			allStepsStr.WriteString("\n")
		}
		result := allStepsStr.String()

		// Verify AWF is used instead of hooks
		if !strings.Contains(result, "awf") {
			t.Error("Expected AWF wrapper to be used with network permissions")
		}

		// Verify no old hook cleanup step is generated (hooks are deprecated)
		if strings.Contains(result, "- name: Clean up network proxy hook files") {
			t.Error("Expected no hook cleanup step since AWF is used instead of hooks")
		}
	})

	t.Run("No cleanup with Claude engine and defaults network permissions", func(t *testing.T) {
		// Test data with Claude engine and defaults network permissions
		// (This simulates what happens when no network section is specified - defaults to "defaults" mode)
		data := &WorkflowData{
			Name:  "test-workflow",
			Model: "claude-3-5-sonnet-20241022",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults"}, // Default network mode
			},
		}

		steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/th-aw/test.log")

		// Convert all steps to string for analysis
		var allStepsStr strings.Builder
		for _, step := range steps {
			allStepsStr.WriteString(strings.Join(step, "\n"))
			allStepsStr.WriteString("\n")
		}
		result := allStepsStr.String()

		// Verify no hook cleanup step (firewall not enabled, no AWF)
		if strings.Contains(result, "- name: Clean up network proxy hook files") {
			t.Error("Expected no hook cleanup step since AWF is used instead of hooks")
		}
	})

	t.Run("No cleanup with Claude engine but no network permissions", func(t *testing.T) {
		// Test data with Claude engine but no network permissions
		data := &WorkflowData{
			Name:  "test-workflow",
			Model: "claude-3-5-sonnet-20241022",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: nil, // No network permissions
		}

		steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/th-aw/test.log")

		// Convert all steps to string for analysis
		var allStepsStr strings.Builder
		for _, step := range steps {
			allStepsStr.WriteString(strings.Join(step, "\n"))
			allStepsStr.WriteString("\n")
		}
		result := allStepsStr.String()

		// Verify no cleanup step is generated
		if strings.Contains(result, "- name: Clean up network proxy hook files") {
			t.Error("Expected no cleanup step to be generated without network permissions")
		}
	})

	t.Run("No cleanup with empty network permissions (AWF deny-all)", func(t *testing.T) {
		// Test data with Claude engine and empty network permissions with firewall enabled
		data := &WorkflowData{
			Name:  "test-workflow",
			Model: "claude-3-5-sonnet-20241022",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed:  []string{}, // Empty allowed list (deny-all)
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/th-aw/test.log")

		// Convert all steps to string for analysis
		var allStepsStr strings.Builder
		for _, step := range steps {
			allStepsStr.WriteString(strings.Join(step, "\n"))
			allStepsStr.WriteString("\n")
		}
		result := allStepsStr.String()

		// Verify AWF is used
		if !strings.Contains(result, "awf") {
			t.Error("Expected AWF to be used even with deny-all policy")
		}

		// Verify no old hook cleanup step is generated
		if strings.Contains(result, "- name: Clean up network proxy hook files") {
			t.Error("Expected no hook cleanup step since AWF is used instead of hooks")
		}
	})
}

func TestEngineOutputCleanupWithMixedPaths(t *testing.T) {
	// Test the cleanup logic directly with mixed paths to ensure proper filtering
	var yaml strings.Builder

	// Simulate mixed output files: some in /tmp/gh-aw/, some in workspace
	mockOutputFiles := []string{
		"/tmp/gh-aw/logs/debug.log",
		"workspace-output/results.txt",
		"/tmp/gh-aw/.cache/data.json",
		"build/artifacts.zip",
	}

	// Generate the engine output collection manually to test the logic
	yaml.WriteString("      - name: Upload engine output files\n")
	yaml.WriteString("        uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          name: agent_outputs\n")
	yaml.WriteString("          path: |\n")
	for _, file := range mockOutputFiles {
		yaml.WriteString("            " + file + "\n")
	}
	yaml.WriteString("          if-no-files-found: ignore\n")

	// Add cleanup step using the same function as the actual implementation
	cleanupYaml, hasCleanup := generateCleanupStep(mockOutputFiles)
	if hasCleanup {
		yaml.WriteString(cleanupYaml)
	}

	result := yaml.String()

	// Verify that all files are included in the upload step
	if !strings.Contains(result, "/tmp/gh-aw/logs/debug.log") {
		t.Error("Expected /tmp/gh-aw/logs/debug.log to be included in upload step")
	}
	if !strings.Contains(result, "workspace-output/results.txt") {
		t.Error("Expected workspace-output/results.txt to be included in upload step")
	}
	if !strings.Contains(result, "/tmp/gh-aw/.cache/data.json") {
		t.Error("Expected /tmp/gh-aw/.cache/data.json to be included in upload step")
	}
	if !strings.Contains(result, "build/artifacts.zip") {
		t.Error("Expected build/artifacts.zip to be included in upload step")
	}

	// Verify that only workspace files are included in cleanup step
	if strings.Contains(result, "rm -fr /tmp/gh-aw/logs/debug.log") {
		t.Error("Cleanup step should NOT include 'rm -fr /tmp/gh-aw/logs/debug.log' command")
	}
	if strings.Contains(result, "rm -fr /tmp/gh-aw/.cache/data.json") {
		t.Error("Cleanup step should NOT include 'rm -fr /tmp/gh-aw/.cache/data.json' command")
	}
	if !strings.Contains(result, "rm -fr workspace-output/results.txt") {
		t.Error("Cleanup step should include 'rm -fr workspace-output/results.txt' command")
	}
	if !strings.Contains(result, "rm -fr build/artifacts.zip") {
		t.Error("Cleanup step should include 'rm -fr build/artifacts.zip' command")
	}

	t.Log("Successfully verified that mixed path cleanup properly filters /tmp/gh-aw/ files")
}

func TestGenerateCleanupStep(t *testing.T) {
	// Test the generateCleanupStep function directly to demonstrate its testability

	// Test case 1: Only /tmp/gh-aw/ files - should not generate cleanup step
	tmpOnlyFiles := []string{"/tmp/gh-aw/logs/debug.log", "/tmp/gh-aw/.cache/data.json"}
	cleanupYaml, hasCleanup := generateCleanupStep(tmpOnlyFiles)

	if hasCleanup {
		t.Error("Expected no cleanup step for /tmp/gh-aw/ only files")
	}
	if cleanupYaml != "" {
		t.Error("Expected empty cleanup YAML for /tmp/gh-aw/ only files")
	}

	// Test case 2: Only workspace files - should generate cleanup step
	workspaceOnlyFiles := []string{"output.txt", "build/artifacts.zip"}
	cleanupYaml, hasCleanup = generateCleanupStep(workspaceOnlyFiles)

	if !hasCleanup {
		t.Error("Expected cleanup step for workspace files")
	}
	if !strings.Contains(cleanupYaml, "rm -fr output.txt") {
		t.Error("Expected cleanup YAML to contain 'rm -fr output.txt'")
	}
	if !strings.Contains(cleanupYaml, "rm -fr build/artifacts.zip") {
		t.Error("Expected cleanup YAML to contain 'rm -fr build/artifacts.zip'")
	}

	// Test case 3: Mixed files - should generate cleanup step only for workspace files
	mixedFiles := []string{"/tmp/gh-aw/debug.log", "workspace/output.txt", "/tmp/gh-aw/.cache/data.json"}
	cleanupYaml, hasCleanup = generateCleanupStep(mixedFiles)

	if !hasCleanup {
		t.Error("Expected cleanup step for mixed files containing workspace files")
	}
	if strings.Contains(cleanupYaml, "rm -fr /tmp/gh-aw/debug.log") {
		t.Error("Cleanup YAML should NOT contain /tmp/gh-aw/ files")
	}
	if strings.Contains(cleanupYaml, "rm -fr /tmp/gh-aw/.cache/data.json") {
		t.Error("Cleanup YAML should NOT contain /tmp/gh-aw/ files")
	}
	if !strings.Contains(cleanupYaml, "rm -fr workspace/output.txt") {
		t.Error("Expected cleanup YAML to contain workspace files")
	}

	// Test case 4: Empty input - should not generate cleanup step
	emptyFiles := []string{}
	cleanupYaml, hasCleanup = generateCleanupStep(emptyFiles)

	if hasCleanup {
		t.Error("Expected no cleanup step for empty files list")
	}
	if cleanupYaml != "" {
		t.Error("Expected empty cleanup YAML for empty files list")
	}

	t.Log("Successfully verified generateCleanupStep function behavior in all scenarios")
}

func TestRedactedURLsLogPathIncludedInEngineOutput(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "redacted-urls-test")

	// Test that redacted URLs log path is included in Copilot engine output collection
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
tools:
  github:
    allowed: [list_issues]
engine: copilot
---

# Test Redacted URLs Log Collection

This workflow tests that the redacted URLs log file is included in artifact uploads.
`

	testFile := filepath.Join(tmpDir, "test-redacted-urls.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	// Verify that Upload engine output files step NO LONGER exists (merged into unified artifact)
	if strings.Contains(lockStr, "- name: Upload engine output files") {
		t.Error("'Upload engine output files' step should be removed (merged into unified agent artifact)")
	}

	// Verify that the engine output path is present in the unified artifact upload paths.
	// Copilot engine declares /tmp/gh-aw/sandbox/agent/logs/ as its output path.
	if !strings.Contains(lockStr, "/tmp/gh-aw/sandbox/agent/logs/") {
		t.Error("Expected Copilot engine output path '/tmp/gh-aw/sandbox/agent/logs/' to be in unified agent artifact upload paths")
	}

	// Verify that the redacted URLs log path is included in the unified artifact paths
	if !strings.Contains(lockStr, RedactedURLsLogPath) {
		t.Errorf("Expected '%s' to be included in unified agent artifact upload paths", RedactedURLsLogPath)
	}

	// Verify the unified agent artifact name is used
	if !strings.Contains(lockStr, "name: agent\n") {
		t.Error("Expected unified 'agent' artifact name in generated workflow")
	}

	t.Log("Successfully verified that redacted URLs log path is included in the unified agent artifact")
}

func TestGeminiEngineOutputFilesGeneratedByCompiler(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "gemini-engine-output-test")

	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: gemini
---

# Test Gemini Engine Output Collection

This workflow tests that the Gemini engine error log wildcard is uploaded as an artifact.
`

	testFile := filepath.Join(tmpDir, "test-gemini-output.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile Gemini workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	// Verify that the compiler does NOT generate a separate Upload engine output files step
	// (engine output paths are now merged into the unified agent artifact)
	if strings.Contains(lockStr, "- name: Upload engine output files") {
		t.Error("Gemini workflow should NOT have separate 'Upload engine output files' step (merged into unified agent artifact)")
	}

	// Verify that the Gemini error log wildcard path is included in the unified artifact upload
	// under /tmp/gh-aw/ (not /tmp/) so the artifact upload LCA stays at /tmp/gh-aw/
	if !strings.Contains(lockStr, "/tmp/gh-aw/gemini-client-error-*.json") {
		t.Error("Gemini workflow should include '/tmp/gh-aw/gemini-client-error-*.json' in unified agent artifact upload")
	}
	if strings.Contains(lockStr, "path: /tmp/gemini-client-error") {
		t.Error("Gemini workflow must NOT have '/tmp/gemini-client-error' artifact path (causes wrong LCA)")
	}

	// Verify that the unified agent artifact name is used (not separate agent_outputs)
	if strings.Contains(lockStr, "name: agent_outputs") {
		t.Error("Gemini engine output should be in unified 'agent' artifact, not separate 'agent_outputs'")
	}
	if !strings.Contains(lockStr, "name: agent\n") {
		t.Error("Gemini workflow should use unified 'agent' artifact name")
	}

	t.Log("Successfully verified that Gemini engine error log wildcard is included in the unified agent artifact")
}
