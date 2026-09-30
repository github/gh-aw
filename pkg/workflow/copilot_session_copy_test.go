//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopilotSessionFileCopyStep(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	// Get the firewall logs collection step (which now includes session file copy)
	steps := engine.GetFirewallLogsCollectionStep(workflowData)

	// Should have at least one step (session file copy)
	if len(steps) == 0 {
		t.Fatal("Expected at least one step for session file copy")
	}

	// Check that the step contains session file copy logic
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Verify step name
	if !strings.Contains(stepContent, "Copy Copilot session state files to logs") {
		t.Error("Expected step name to contain 'Copy Copilot session state files to logs'")
	}

	// Verify if: always() condition
	if !strings.Contains(stepContent, "if: always()") {
		t.Error("Expected step to have 'if: always()' condition")
	}

	// Verify continue-on-error
	if !strings.Contains(stepContent, "continue-on-error: true") {
		t.Error("Expected step to have 'continue-on-error: true'")
	}

	// Verify it delegates to the external shell script
	if !strings.Contains(stepContent, "copy_copilot_session_state.sh") {
		t.Error("Expected step to invoke copy_copilot_session_state.sh")
	}

	// Verify it uses the RUNNER_TEMP-based actions path
	if !strings.Contains(stepContent, "${RUNNER_TEMP}/gh-aw/actions/") {
		t.Error("Expected step to reference script via ${RUNNER_TEMP}/gh-aw/actions/")
	}
}

func TestCopilotSessionCopyScriptPrefersAWFManagedDirectory(t *testing.T) {
	awfDir := t.TempDir()
	legacyDir := t.TempDir()
	logsDir := t.TempDir()
	writeSessionStateFile(t, awfDir, "session/events.jsonl", "awf events")
	writeSessionStateFile(t, awfDir, "session/checkpoints/001.md", "awf checkpoint")
	writeSessionStateFile(t, legacyDir, "session/events.jsonl", "legacy events")
	writeSessionStateFile(t, legacyDir, "session/legacy-only.txt", "legacy only")

	runCopilotSessionCopyScript(t, awfDir, legacyDir, logsDir)

	assertSessionStateFile(t, logsDir, "session/events.jsonl", "awf events")
	assertSessionStateFile(t, logsDir, "session/checkpoints/001.md", "awf checkpoint")
	assertSessionStateFileMissing(t, logsDir, "session/legacy-only.txt")
}

func TestCopilotSessionCopyScriptFallsBackToLegacyDirectory(t *testing.T) {
	awfDir := t.TempDir()
	legacyDir := t.TempDir()
	logsDir := t.TempDir()
	writeSessionStateFile(t, legacyDir, "session/events.jsonl", "legacy events")
	writeSessionStateFile(t, legacyDir, "session/files/nested.txt", "legacy file")

	runCopilotSessionCopyScript(t, awfDir, legacyDir, logsDir)

	assertSessionStateFile(t, logsDir, "session/events.jsonl", "legacy events")
	assertSessionStateFile(t, logsDir, "session/files/nested.txt", "legacy file")
}

func TestCopilotSessionCopyScriptSkipsEmptyDirectories(t *testing.T) {
	awfDir := t.TempDir()
	legacyDir := t.TempDir()
	logsParent := t.TempDir()
	logsDir := filepath.Join(logsParent, "copilot-session-state")

	runCopilotSessionCopyScript(t, awfDir, legacyDir, logsDir)

	if _, err := os.Stat(logsDir); !os.IsNotExist(err) {
		t.Errorf("Expected no logs directory for empty sources, stat error = %v", err)
	}
}

func runCopilotSessionCopyScript(t *testing.T, awfDir, legacyDir, logsDir string) {
	t.Helper()

	scriptPath, err := filepath.Abs("../../actions/setup/sh/copy_copilot_session_state.sh")
	if err != nil {
		t.Fatalf("Failed to resolve Copilot session copy script path: %v", err)
	}
	cmd := exec.Command("bash", scriptPath)
	overrides := map[string]string{
		"GH_AW_COPILOT_SESSION_STATE_DIR":        awfDir,
		"GH_AW_COPILOT_LEGACY_SESSION_STATE_DIR": legacyDir,
		"GH_AW_COPILOT_SESSION_LOGS_DIR":         logsDir,
		"HOME":                                   t.TempDir(),
	}
	for _, variable := range os.Environ() {
		key, _, _ := strings.Cut(variable, "=")
		if _, overridden := overrides[key]; !overridden {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	for key, value := range overrides {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Copilot session copy script failed: %v\n%s", err, output)
	}
}

func writeSessionStateFile(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("Failed to create session state directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write session state file: %v", err)
	}
}

func assertSessionStateFile(t *testing.T, dir, name, want string) {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("Failed to read copied session state file %q: %v", name, err)
	}
	if string(content) != want {
		t.Errorf("Copied session state file %q = %q, want %q", name, content, want)
	}
}

func assertSessionStateFileMissing(t *testing.T, dir, name string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Errorf("Expected session state file %q to be missing, stat error = %v", name, err)
	}
}
