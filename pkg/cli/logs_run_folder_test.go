//go:build !integration

package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteWorkflowRunFolderLocation(t *testing.T) {
	runFolder := filepath.Join(t.TempDir(), "run-42")

	_, stderr := captureOutput(t, func() error {
		writeWorkflowRunFolderLocation(42, runFolder)
		return nil
	})

	assert.Contains(t, stderr, "Workflow run 42 folder: "+runFolder)
}
