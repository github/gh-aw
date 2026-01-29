//go:build !integration

package workflow

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTopLevelSafeJobsRejected verifies that top-level "safe-jobs" field is rejected
func TestTopLevelSafeJobsRejected(t *testing.T) {
	c := NewCompiler()

	// Create a test workflow markdown with top-level safe-jobs
	markdown := `---
on:
  workflow_dispatch:
engine: copilot
safe-jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
---
Test workflow with top-level safe-jobs (should be rejected)
`

	// Write to a temporary file
	tmpFile := "/tmp/test-top-level-safe-jobs-validation.md"
	err := os.WriteFile(tmpFile, []byte(markdown), 0644)
	assert.NoError(t, err, "Should write test file")
	defer os.Remove(tmpFile)

	// Try to compile - should fail with clear error message
	_, err = c.ParseWorkflowFile(tmpFile)
	assert.Error(t, err, "Should reject top-level safe-jobs")
	assert.Contains(t, err.Error(), "top-level 'safe-jobs' field is not supported", "Error should mention top-level safe-jobs not supported")
	assert.Contains(t, err.Error(), "safe-outputs.jobs", "Error should suggest using safe-outputs.jobs")
}

// TestSafeOutputsJobsAccepted verifies that safe-outputs.jobs is accepted
func TestSafeOutputsJobsAccepted(t *testing.T) {
	c := NewCompiler()

	// Create a test workflow markdown with safe-outputs.jobs
	markdown := `---
on:
  workflow_dispatch:
engine: copilot
safe-outputs:
  jobs:
    test:
      runs-on: ubuntu-latest
      steps:
        - run: echo "test"
---
Test workflow with safe-outputs.jobs (should be accepted)
`

	// Write to a temporary file
	tmpFile := "/tmp/test-safe-outputs-jobs-validation.md"
	err := os.WriteFile(tmpFile, []byte(markdown), 0644)
	assert.NoError(t, err, "Should write test file")
	defer os.Remove(tmpFile)

	// Try to compile - should succeed (or fail for other reasons, but not safe-jobs)
	_, err = c.ParseWorkflowFile(tmpFile)
	// If there's an error, it shouldn't be about safe-jobs
	if err != nil {
		assert.NotContains(t, err.Error(), "safe-jobs", "Should not mention safe-jobs if using safe-outputs.jobs")
	}
}

// TestSharedWorkflowSafeJobsRejected verifies that shared workflows also reject top-level safe-jobs
func TestSharedWorkflowSafeJobsRejected(t *testing.T) {
	c := NewCompiler()

	// Create a test shared workflow markdown with top-level safe-jobs (no 'on' field)
	markdown := `---
engine: copilot
safe-jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
---
Shared workflow with top-level safe-jobs (should be rejected)
`

	// Write to a temporary file
	tmpFile := "/tmp/test-shared-safe-jobs-validation.md"
	err := os.WriteFile(tmpFile, []byte(markdown), 0644)
	assert.NoError(t, err, "Should write test file")
	defer os.Remove(tmpFile)

	// Try to compile - should fail with clear error message
	_, err = c.ParseWorkflowFile(tmpFile)
	assert.Error(t, err, "Should reject top-level safe-jobs in shared workflows")
	assert.Contains(t, err.Error(), "top-level 'safe-jobs' field is not supported", "Error should mention top-level safe-jobs not supported")
}
