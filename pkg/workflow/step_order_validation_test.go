//go:build !integration

package workflow

import (
	"testing"
)

func TestStepOrderTracker_ValidateOrdering_NoSteps(t *testing.T) {
	tracker := NewStepOrderTracker()
	tracker.MarkAgentExecutionComplete()

	err := tracker.ValidateStepOrdering()
	if err != nil {
		t.Errorf("Expected no error for empty tracker, got: %v", err)
	}
}

func TestStepOrderTracker_ValidateOrdering_SecretRedactionBeforeUploads(t *testing.T) {
	tracker := NewStepOrderTracker()
	tracker.MarkAgentExecutionComplete()

	// Add secret redaction first
	tracker.RecordSecretRedaction("Redact secrets in logs")

	// Add artifact uploads after
	tracker.RecordArtifactUpload("Upload agent logs", []string{"/tmp/gh-aw/agent-stdio.log"})
	tracker.RecordArtifactUpload("Upload MCP logs", []string{"/tmp/gh-aw/mcp-logs/"})

	err := tracker.ValidateStepOrdering()
	if err != nil {
		t.Errorf("Expected no error when secret redaction comes before uploads, got: %v", err)
	}
}

func TestStepOrderTracker_ValidateOrdering_UploadBeforeSecretRedaction(t *testing.T) {
	tracker := NewStepOrderTracker()
	tracker.MarkAgentExecutionComplete()

	// Add artifact upload BEFORE secret redaction (BUG!)
	tracker.RecordArtifactUpload("Upload prompt", []string{"/tmp/gh-aw/aw-prompts/prompt.txt"})

	// Add secret redaction after
	tracker.RecordSecretRedaction("Redact secrets in logs")

	// Add more uploads after
	tracker.RecordArtifactUpload("Upload agent logs", []string{"/tmp/gh-aw/agent-stdio.log"})

	err := tracker.ValidateStepOrdering()
	if err == nil {
		t.Error("Expected error when upload comes before secret redaction, got nil")
	}
	expectedMsg := "This is a compiler bug - secret redaction must happen before artifact uploads"
	if err != nil && !contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
	}
}

func TestStepOrderTracker_ValidateOrdering_NoSecretRedactionWithUploads(t *testing.T) {
	tracker := NewStepOrderTracker()
	tracker.MarkAgentExecutionComplete()

	// Add artifact uploads WITHOUT secret redaction (BUG!)
	tracker.RecordArtifactUpload("Upload agent logs", []string{"/tmp/gh-aw/agent-stdio.log"})
	tracker.RecordArtifactUpload("Upload MCP logs", []string{"/tmp/gh-aw/mcp-logs/"})

	err := tracker.ValidateStepOrdering()
	if err == nil {
		t.Error("Expected error when uploads exist without secret redaction, got nil")
	}
	expectedMsg := "artifact uploads found but no secret redaction step was added"
	if err != nil && !contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
	}
}

func TestStepOrderTracker_ValidateOrdering_BeforeAgentExecution(t *testing.T) {
	tracker := NewStepOrderTracker()
	// Don't mark agent execution complete

	// Add steps before agent execution - these should be ignored
	tracker.RecordArtifactUpload("Upload prompt", []string{"/tmp/gh-aw/aw-prompts/prompt.txt"})

	err := tracker.ValidateStepOrdering()
	if err != nil {
		t.Errorf("Expected no error for steps before agent execution, got: %v", err)
	}
}

func TestIsPathScannedBySecretRedaction_ScannableFiles(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "JSON file in /tmp/gh-aw/",
			path:     "/tmp/gh-aw/aw_info.json",
			expected: true,
		},
		{
			name:     "TXT file in /tmp/gh-aw/",
			path:     "/tmp/gh-aw/aw-prompts/prompt.txt",
			expected: true,
		},
		{
			name:     "LOG file in /tmp/gh-aw/",
			path:     "/tmp/gh-aw/agent-stdio.log",
			expected: true,
		},
		{
			name:     "JSONL file in /opt/gh-aw/",
			path:     "/opt/gh-aw/safeoutputs/outputs.jsonl",
			expected: true,
		},
		{
			name:     "Directory in /tmp/gh-aw/",
			path:     "/tmp/gh-aw/mcp-logs/",
			expected: true,
		},
		{
			name:     "Subdirectory in /tmp/gh-aw/",
			path:     "/tmp/gh-aw/access-logs/",
			expected: true,
		},
		{
			name:     "Environment variable reference",
			path:     "${{ env.GH_AW_SAFE_OUTPUTS }}",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathScannedBySecretRedaction(tt.path)
			if result != tt.expected {
				t.Errorf("isPathScannedBySecretRedaction(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsPathScannedBySecretRedaction_UnscannableFiles(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "File outside /tmp/gh-aw/",
			path:     "/tmp/other/file.log",
			expected: false,
		},
		{
			name:     "File in workspace root",
			path:     "output.json",
			expected: false,
		},
		{
			name:     "File with wrong extension in /tmp/gh-aw/",
			path:     "/tmp/gh-aw/script.sh",
			expected: false,
		},
		{
			name:     "Binary file in /tmp/gh-aw/",
			path:     "/tmp/gh-aw/data.bin",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathScannedBySecretRedaction(tt.path)
			if result != tt.expected {
				t.Errorf("isPathScannedBySecretRedaction(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestStepOrderTracker_ValidateOrdering_UnscannablePath(t *testing.T) {
	tracker := NewStepOrderTracker()
	tracker.MarkAgentExecutionComplete()

	// Add secret redaction
	tracker.RecordSecretRedaction("Redact secrets in logs")

	// Add upload with unscannable path (BUG!)
	tracker.RecordArtifactUpload("Upload workspace file", []string{"/tmp/gh-aw/output.xml"})

	err := tracker.ValidateStepOrdering()
	if err == nil {
		t.Error("Expected error for unscannable path, got nil")
	}
	expectedMsg := "not covered by secret redaction"
	if err != nil && !contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
	}
}

func TestStepOrderTracker_ValidateOrdering_MixedScannableAndUnscannable(t *testing.T) {
	tracker := NewStepOrderTracker()
	tracker.MarkAgentExecutionComplete()

	// Add secret redaction
	tracker.RecordSecretRedaction("Redact secrets in logs")

	// Add uploads with both scannable and unscannable paths
	tracker.RecordArtifactUpload("Upload logs", []string{"/tmp/gh-aw/agent-stdio.log"})
	tracker.RecordArtifactUpload("Upload binary", []string{"/tmp/gh-aw/data.bin"})

	err := tracker.ValidateStepOrdering()
	if err == nil {
		t.Error("Expected error for unscannable path, got nil")
	}
	expectedMsg := "not covered by secret redaction"
	if err != nil && !contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
	}
}
