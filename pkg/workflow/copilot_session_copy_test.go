//go:build !integration

package workflow

import (
	"os"
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

func TestCopilotSessionCopyScriptUsesAWFManagedDirectory(t *testing.T) {
	content, err := os.ReadFile("../../actions/setup/sh/copy_copilot_session_state.sh")
	if err != nil {
		t.Fatalf("Failed to read Copilot session copy script: %v", err)
	}
	script := string(content)

	if !strings.Contains(script, `AWF_SESSION_STATE_DIR="/tmp/gh-aw/sandbox/agent/session-state"`) {
		t.Error("Expected script to read from the AWF-managed session-state directory")
	}
	if !strings.Contains(script, `LEGACY_SESSION_STATE_DIR="$HOME/.copilot/session-state"`) {
		t.Error("Expected script to retain the legacy host HOME fallback")
	}
	if !strings.Contains(script, `LOGS_DIR="/tmp/gh-aw/sandbox/agent/logs/copilot-session-state"`) {
		t.Error("Expected script to copy session state into the parser's log directory")
	}
}
