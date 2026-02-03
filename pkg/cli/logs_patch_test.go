//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestLogsPatchArtifactHandling(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a mock log directory structure with artifacts
	logDir := filepath.Join(tmpDir, "mock-run-123")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("Failed to create log directory: %v", err)
	}

	// Create mock artifact files
	awInfoFile := filepath.Join(logDir, "aw_info.json")
	awInfoContent := `{
		"engine": "claude",
		"workflow_name": "test-workflow",
		"run_id": 123
	}`
	if err := os.WriteFile(awInfoFile, []byte(awInfoContent), 0644); err != nil {
		t.Fatalf("Failed to write aw_info.json: %v", err)
	}

	awOutputFile := filepath.Join(logDir, "safe_output.jsonl")
	awOutputContent := "Test output from agentic execution"
	if err := os.WriteFile(awOutputFile, []byte(awOutputContent), 0644); err != nil {
		t.Fatalf("Failed to write safe_output.jsonl: %v", err)
	}

	awPatchFile := filepath.Join(logDir, "aw.patch")
	awPatchContent := `diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..9daeafb
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+test
`
	if err := os.WriteFile(awPatchFile, []byte(awPatchContent), 0644); err != nil {
		t.Fatalf("Failed to write aw.patch: %v", err)
	}

	// Test extractLogMetrics function with verbose output to capture messages
	metrics, err := extractLogMetrics(logDir, true)
	if err != nil {
		t.Fatalf("extractLogMetrics failed: %v", err)
	}

	// Verify metrics were extracted (basic validation)
	if metrics.TokenUsage < 0 {
		t.Error("Expected non-negative token usage")
	}
	if metrics.EstimatedCost < 0 {
		t.Error("Expected non-negative estimated cost")
	}

	// Test that the function doesn't crash when processing the patch file
	// The actual verbose output validation would be more complex to test
	// since it goes to stdout, but the important thing is that it doesn't error
}

func TestLogsCommandHelp(t *testing.T) {
	// Test that the logs command help includes patch information
	cmd := NewLogsCommand()
	helpText := cmd.Long

	// Verify that the help text mentions the git patch
	if !strings.Contains(helpText, "aw.patch") {
		t.Error("Expected logs command help to mention 'aw.patch' artifact")
	}

	if !strings.Contains(helpText, "Git patch of changes made during execution") {
		t.Error("Expected logs command help to describe the git patch artifact")
	}

	// Verify the help text mentions all expected artifacts
	expectedArtifacts := []string{
		"aw_info.json",
		"safe_output.jsonl",
		"aw.patch",
	}

	for _, artifact := range expectedArtifacts {
		if !strings.Contains(helpText, artifact) {
			t.Errorf("Expected logs command help to mention artifact: %s", artifact)
		}
	}
}
