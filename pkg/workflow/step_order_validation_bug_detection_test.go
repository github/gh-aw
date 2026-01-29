//go:build !integration

package workflow

import (
	"testing"
)

// TestStepOrderingValidationCatchesBugs verifies that the validation system
// would catch a compiler bug where uploads happen before secret redaction
func TestStepOrderingValidationCatchesBugs(t *testing.T) {
	// Simulate what would happen if there was a compiler bug
	tracker := NewStepOrderTracker()
	tracker.MarkAgentExecutionComplete()

	// Bug scenario: Upload happens BEFORE secret redaction
	tracker.RecordArtifactUpload("Upload logs", []string{"/tmp/gh-aw/logs.txt"})

	// Then secret redaction is added
	tracker.RecordSecretRedaction("Redact secrets")

	// More uploads after
	tracker.RecordArtifactUpload("Upload more logs", []string{"/tmp/gh-aw/more.log"})

	// Validation should catch the bug
	err := tracker.ValidateStepOrdering()
	if err == nil {
		t.Fatal("Expected validation to catch the bug (upload before redaction), but got no error")
	}

	if !contains(err.Error(), "must happen before artifact uploads") {
		t.Errorf("Expected error about upload before redaction, got: %v", err)
	}
}

// TestStepOrderingValidationCatchesUnscannablePaths verifies that the validation
// system would catch uploads of files that won't be scanned by secret redaction
func TestStepOrderingValidationCatchesUnscannablePaths(t *testing.T) {
	tracker := NewStepOrderTracker()
	tracker.MarkAgentExecutionComplete()

	// Add secret redaction first (correct order)
	tracker.RecordSecretRedaction("Redact secrets")

	// But upload a file with wrong extension that won't be scanned
	tracker.RecordArtifactUpload("Upload binary", []string{"/tmp/gh-aw/data.bin"})

	// Validation should catch the unscannable path
	err := tracker.ValidateStepOrdering()
	if err == nil {
		t.Fatal("Expected validation to catch unscannable path, but got no error")
	}

	if !contains(err.Error(), "not covered by secret redaction") {
		t.Errorf("Expected error about unscannable path, got: %v", err)
	}
}

// TestStepOrderingValidationCatchesMissingRedaction verifies that the validation
// system would catch the case where uploads exist but no redaction step
func TestStepOrderingValidationCatchesMissingRedaction(t *testing.T) {
	tracker := NewStepOrderTracker()
	tracker.MarkAgentExecutionComplete()

	// Upload without any secret redaction step
	tracker.RecordArtifactUpload("Upload logs", []string{"/tmp/gh-aw/logs.txt"})

	// Validation should catch the missing redaction
	err := tracker.ValidateStepOrdering()
	if err == nil {
		t.Fatal("Expected validation to catch missing redaction, but got no error")
	}

	if !contains(err.Error(), "no secret redaction step was added") {
		t.Errorf("Expected error about missing redaction, got: %v", err)
	}
}
