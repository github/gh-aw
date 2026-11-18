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

func TestCopilotSessionCopyScriptUsesOnlyAWFManagedDirectory(t *testing.T) {
	awfDir := t.TempDir()
	homeDir := t.TempDir()
	legacyDir := filepath.Join(homeDir, ".copilot", "session-state")
	logsDir := t.TempDir()
	writeSessionStateFile(t, awfDir, "session/events.jsonl", "awf events")
	writeSessionStateFile(t, awfDir, "session/checkpoints/001.md", "awf checkpoint")
	writeSessionStateFile(t, legacyDir, "session/events.jsonl", "legacy events")
	writeSessionStateFile(t, legacyDir, "session/legacy-only.txt", "legacy only")

	runCopilotSessionCopyScript(t, awfDir, homeDir, logsDir)

	assertSessionStateFile(t, logsDir, "session/events.jsonl", "awf events")
	assertSessionStateFile(t, logsDir, "session/checkpoints/001.md", "awf checkpoint")
	assertSessionStateFileMissing(t, logsDir, "session/legacy-only.txt")
}

func TestCopilotSessionCopyScriptDoesNotFallBackToLegacyDirectory(t *testing.T) {
	for _, name := range []string{"empty", "missing", "non-redactable"} {
		t.Run(name, func(t *testing.T) {
			awfDir := filepath.Join(t.TempDir(), "session-state")
			if name != "missing" {
				if err := os.MkdirAll(awfDir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if name == "non-redactable" {
				writeSessionStateFile(t, awfDir, "session/session.db", "awf database")
			}
			homeDir := t.TempDir()
			legacyDir := filepath.Join(homeDir, ".copilot", "session-state")
			logsDir := filepath.Join(t.TempDir(), "copilot-session-state")
			writeSessionStateFile(t, legacyDir, "historical-session/events.jsonl", "legacy events")
			writeSessionStateFile(t, legacyDir, "another-session/files/nested.txt", "legacy file")

			output := runCopilotSessionCopyScript(t, awfDir, homeDir, logsDir)

			if !strings.Contains(output, "::warning::No Copilot session state files found in "+awfDir) {
				t.Errorf("Expected warning for unusable job session directory, got: %s", output)
			}
			if strings.Contains(output, legacyDir) {
				t.Errorf("Expected no reference to legacy session directory, got: %s", output)
			}
			if _, err := os.Stat(logsDir); !os.IsNotExist(err) {
				t.Errorf("Expected no logs directory for unusable job session directory, stat error = %v", err)
			}
			assertSessionStateFile(t, legacyDir, "historical-session/events.jsonl", "legacy events")
			assertSessionStateFile(t, legacyDir, "another-session/files/nested.txt", "legacy file")
		})
	}
}

func TestCopilotSessionCopyScriptCopiesOnlyRedactableFiles(t *testing.T) {
	awfDir := t.TempDir()
	homeDir := t.TempDir()
	logsDir := t.TempDir()
	writeSessionStateFile(t, awfDir, "session/events.jsonl", "awf events")
	writeSessionStateFile(t, awfDir, "session/checkpoints/001.md", "awf checkpoint")
	writeSessionStateFile(t, awfDir, "session/session.db", "sensitive database")
	writeSessionStateFile(t, awfDir, "session/extensionless", "sensitive extensionless file")
	writeSessionStateFile(t, awfDir, "session/settings.yaml", "unsupported extension")

	runCopilotSessionCopyScript(t, awfDir, homeDir, logsDir)

	assertSessionStateFile(t, logsDir, "session/events.jsonl", "awf events")
	assertSessionStateFile(t, logsDir, "session/checkpoints/001.md", "awf checkpoint")
	assertSessionStateFileMissing(t, logsDir, "session/session.db")
	assertSessionStateFileMissing(t, logsDir, "session/extensionless")
	assertSessionStateFileMissing(t, logsDir, "session/settings.yaml")
}

func TestCopilotSessionCopyScriptSkipsEmptyDirectories(t *testing.T) {
	awfDir := t.TempDir()
	homeDir := t.TempDir()
	logsParent := t.TempDir()
	logsDir := filepath.Join(logsParent, "copilot-session-state")

	runCopilotSessionCopyScript(t, awfDir, homeDir, logsDir)

	if _, err := os.Stat(logsDir); !os.IsNotExist(err) {
		t.Errorf("Expected no logs directory for empty sources, stat error = %v", err)
	}
}

func TestCopilotSessionCopyScriptCopiesOnlyDurableDirectModeSession(t *testing.T) {
	const sessionID = "00000000-0000-4000-8000-000000000001"
	awfDir := filepath.Join(t.TempDir(), "session-state")
	homeDir := t.TempDir()
	logsDir := filepath.Join(t.TempDir(), "copilot-session-state")
	writeSessionStateFile(t, homeDir, ".copilot/session-state/"+sessionID+"/events.jsonl", "current events")
	writeSessionStateFile(t, homeDir, ".copilot/session-state/historical/events.jsonl", "historical events")

	runCopilotSessionCopyScriptWithSessionID(t, awfDir, homeDir, logsDir, sessionID)

	assertSessionStateFile(t, logsDir, sessionID+"/events.jsonl", "current events")
	assertSessionStateFileMissing(t, logsDir, "historical/events.jsonl")
}

func TestCopilotSessionCopyScriptIgnoresStaleDirectModeBaseline(t *testing.T) {
	const sessionID = "00000000-0000-4000-8000-000000000001"
	awfDir := filepath.Join(t.TempDir(), "session-state")
	homeDir := t.TempDir()
	logsDir := filepath.Join(t.TempDir(), "copilot-session-state")
	writeSessionStateFile(t, homeDir, ".copilot/session-state/"+sessionID+"/events.jsonl", "historical events")

	output := runCopilotSessionCopyScriptWithSessionID(t, awfDir, homeDir, logsDir, sessionID, "older-run")

	if !strings.Contains(output, "::warning::No Copilot session state files found in "+awfDir) {
		t.Errorf("Expected warning for stale durable session baseline, got: %s", output)
	}
	if _, err := os.Stat(logsDir); !os.IsNotExist(err) {
		t.Errorf("Expected stale session not to be copied, stat error = %v", err)
	}
	assertSessionStateFile(t, homeDir, ".copilot/session-state/"+sessionID+"/events.jsonl", "historical events")
}

func runCopilotSessionCopyScript(t *testing.T, awfDir, homeDir, logsDir string) string {
	return runCopilotSessionCopyScriptWithSessionID(t, awfDir, homeDir, logsDir, "")
}

func runCopilotSessionCopyScriptWithSessionID(t *testing.T, awfDir, homeDir, logsDir, sessionID string, baselineRunIDs ...string) string {
	t.Helper()

	scriptPath, err := filepath.Abs("../../actions/setup/sh/copy_copilot_session_state.sh")
	if err != nil {
		t.Fatalf("Failed to resolve Copilot session copy script path: %v", err)
	}
	sessionIDFile := filepath.Join(t.TempDir(), "capture-offset.json")
	if sessionID != "" {
		baselineRunID := "current-run"
		if len(baselineRunIDs) > 0 {
			baselineRunID = baselineRunIDs[0]
		}
		if err := os.WriteFile(sessionIDFile, []byte(`{"id":"`+sessionID+`","bytes":0,"runId":"`+baselineRunID+`","runAttempt":"1"}`), 0o600); err != nil {
			t.Fatalf("Failed to write durable session capture baseline: %v", err)
		}
	}
	cmd := exec.Command("bash", scriptPath)
	overrides := map[string]string{
		"GH_AW_COPILOT_SESSION_STATE_DIR": awfDir,
		"GH_AW_COPILOT_SESSION_ID_FILE":   sessionIDFile,
		"GH_AW_COPILOT_SESSION_LOGS_DIR":  logsDir,
		"GITHUB_RUN_ID":                   "current-run",
		"GITHUB_RUN_ATTEMPT":              "1",
		"HOME":                            homeDir,
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
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Copilot session copy script failed: %v\n%s", err, output)
	}
	return string(output)
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
