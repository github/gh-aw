package workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var stepOrderLog = logger.New("workflow:step_order_validation")

// StepType represents the type of step being generated
type StepType int

const (
	StepTypeSecretRedaction StepType = iota
	StepTypeArtifactUpload
	StepTypeOther
)

// StepRecord tracks a step that was generated during compilation
type StepRecord struct {
	Type        StepType
	Name        string
	Order       int      // Order in which this step was added
	UploadPaths []string // For artifact upload steps, the paths being uploaded
}

// StepOrderTracker tracks the order of steps generated during compilation
type StepOrderTracker struct {
	steps                []StepRecord
	nextOrder            int
	secretRedactionAdded bool
	secretRedactionOrder int
	afterAgentExecution  bool // Track whether we're after agent execution step
}

// NewStepOrderTracker creates a new step order tracker
func NewStepOrderTracker() *StepOrderTracker {
	return &StepOrderTracker{
		nextOrder: 0,
	}
}

// MarkAgentExecutionComplete marks that we've passed the agent execution step
// Validation only applies to steps after this point
func (t *StepOrderTracker) MarkAgentExecutionComplete() {
	t.afterAgentExecution = true
}

// RecordSecretRedaction records that a secret redaction step was added
func (t *StepOrderTracker) RecordSecretRedaction(stepName string) {
	if !t.afterAgentExecution {
		// Only track steps after agent execution
		return
	}

	t.steps = append(t.steps, StepRecord{
		Type:  StepTypeSecretRedaction,
		Name:  stepName,
		Order: t.nextOrder,
	})
	t.secretRedactionAdded = true
	t.secretRedactionOrder = t.nextOrder
	t.nextOrder++
}

// RecordArtifactUpload records that an artifact upload step was added
func (t *StepOrderTracker) RecordArtifactUpload(stepName string, uploadPaths []string) {
	if !t.afterAgentExecution {
		// Only track steps after agent execution
		return
	}

	t.steps = append(t.steps, StepRecord{
		Type:        StepTypeArtifactUpload,
		Name:        stepName,
		Order:       t.nextOrder,
		UploadPaths: uploadPaths,
	})
	t.nextOrder++
}

// ValidateStepOrdering validates that secret redaction happens before artifact uploads
// and that all uploaded paths are covered by secret redaction
func (t *StepOrderTracker) ValidateStepOrdering() error {
	stepOrderLog.Printf("Validating step ordering: %d total steps recorded", len(t.steps))
	// If we haven't reached agent execution yet, no validation needed
	if !t.afterAgentExecution {
		stepOrderLog.Print("Validation skipped: not yet after agent execution")
		return nil
	}

	// Find all artifact uploads
	var artifactUploads []StepRecord
	for _, step := range t.steps {
		if step.Type == StepTypeArtifactUpload {
			artifactUploads = append(artifactUploads, step)
		}
	}

	stepOrderLog.Printf("Found %d artifact upload steps", len(artifactUploads))

	// If no artifact uploads, no validation needed
	if len(artifactUploads) == 0 {
		stepOrderLog.Print("Validation passed: no artifact uploads to validate")
		return nil
	}

	// If there are artifact uploads but no secret redaction, that's a bug
	if !t.secretRedactionAdded {
		stepOrderLog.Print("Validation failed: artifact uploads found but no secret redaction step")
		return NewOperationError(
			"compile",
			"workflow steps",
			"artifact uploads without secret redaction",
			fmt.Errorf("artifact uploads found but no secret redaction step was added"),
			"This is a critical security issue - a compiler bug. Please report this issue to the gh-aw maintainers with your workflow file:\nhttps://github.com/github/gh-aw/issues/new",
		)
	}

	stepOrderLog.Printf("Secret redaction step found at order %d", t.secretRedactionOrder)

	// Check that secret redaction comes before all artifact uploads
	var uploadsBeforeRedaction []string
	for _, upload := range artifactUploads {
		if upload.Order < t.secretRedactionOrder {
			uploadsBeforeRedaction = append(uploadsBeforeRedaction, upload.Name)
		}
	}

	if len(uploadsBeforeRedaction) > 0 {
		return NewOperationError(
			"compile",
			"workflow steps",
			"incorrect step ordering",
			fmt.Errorf("found %d upload(s) before secret redaction: %s", len(uploadsBeforeRedaction), strings.Join(uploadsBeforeRedaction, ", ")),
			"This is a compiler bug - secret redaction must happen before artifact uploads. Please report this issue to the gh-aw maintainers with your workflow file:\nhttps://github.com/github/gh-aw/issues/new",
		)
	}

	// Check that all uploaded paths are covered by secret redaction
	// Secret redaction scans all files in /tmp/gh-aw/ with extensions .txt, .json, .log
	unscannable := t.findUnscannablePaths(artifactUploads)
	if len(unscannable) > 0 {
		return NewOperationError(
			"compile",
			"workflow steps",
			"artifact paths not covered by secret redaction",
			fmt.Errorf("paths not covered: %s", strings.Join(unscannable, ", ")),
			"This is a compiler bug - all artifact uploads must be covered by secret redaction. Please report this issue to the gh-aw maintainers with your workflow file:\nhttps://github.com/github/gh-aw/issues/new",
		)
	}

	return nil
}

// findUnscannablePaths finds paths that would be uploaded but not scanned by secret redaction
func (t *StepOrderTracker) findUnscannablePaths(artifactUploads []StepRecord) []string {
	var unscannable []string

	for _, upload := range artifactUploads {
		for _, path := range upload.UploadPaths {
			// Check if this path would be scanned by secret redaction
			// Secret redaction only scans:
			// 1. Files under /tmp/gh-aw/
			// 2. With extensions .txt, .json, .log
			if !isPathScannedBySecretRedaction(path) {
				unscannable = append(unscannable, path)
			}
		}
	}

	return unscannable
}

// isPathScannedBySecretRedaction checks if a path would be scanned by the secret redaction step
func isPathScannedBySecretRedaction(path string) bool {
	// Paths must be under /tmp/gh-aw/ or /opt/gh-aw/ to be scanned
	// Accept both literal paths and environment variable references
	if !strings.HasPrefix(path, "/tmp/gh-aw/") && !strings.HasPrefix(path, "/opt/gh-aw/") {
		// Check if it's an environment variable that might resolve to /tmp/gh-aw/ or /opt/gh-aw/
		// For now, we'll allow ${{ env.* }} patterns through as we can't resolve them at compile time
		// Assume environment variables that might contain /tmp/gh-aw or /opt/gh-aw paths are safe
		// This is a conservative assumption - in practice these are controlled by the compiler
		return strings.Contains(path, "${{ env.")
	}

	// Path must have one of the scanned extensions: .txt, .json, .log, .jsonl
	ext := filepath.Ext(path)
	scannedExtensions := []string{".txt", ".json", ".log", ".jsonl"}
	for _, scannedExt := range scannedExtensions {
		if ext == scannedExt {
			return true
		}
	}

	// If path is a directory (ends with /), we assume it contains scannable files
	return strings.HasSuffix(path, "/")
}
