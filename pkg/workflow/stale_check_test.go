//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStaleCheckMode(t *testing.T) {
	tests := []struct {
		name     string
		on       any
		disabled bool
		full     bool
	}{
		{name: "missing"},
		{name: "missing on section", on: "workflow_dispatch"},
		{name: "enabled", on: map[string]any{"stale-check": true}},
		{name: "disabled", on: map[string]any{"stale-check": false}, disabled: true},
		{name: "full", on: map[string]any{"stale-check": "full"}, full: true},
		{name: "other string", on: map[string]any{"stale-check": "true"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disabled, full := ParseStaleCheckMode(map[string]any{"on": tt.on})
			assert.Equal(t, tt.disabled, disabled)
			assert.Equal(t, tt.full, full)
		})
	}
}

// TestStaleCheckInActivationJob tests that the frontmatter hash check step is correctly
// added or omitted based on the on.stale-check flag.
func TestStaleCheckInActivationJob(t *testing.T) {
	baseWorkflowMD := `---
engine: copilot
on:
  issues:
    types: [opened]
---
Test workflow for stale check step.
`
	disabledWorkflowMD := `---
engine: copilot
on:
  issues:
    types: [opened]
  stale-check: false
---
Test workflow for stale check step disabled.
`
	enabledExplicitWorkflowMD := `---
engine: copilot
on:
  issues:
    types: [opened]
  stale-check: true
---
Test workflow for stale check step explicitly enabled.
`
	fullStaleCheckWorkflowMD := `---
engine: copilot
on:
  issues:
    types: [opened]
  stale-check: full
---
Test workflow for full stale check step.
`

	tests := []struct {
		name              string
		workflowMD        string
		wantStep          bool
		wantFullCheckMode bool
	}{
		{
			name:       "step present when stale-check not set (default)",
			workflowMD: baseWorkflowMD,
			wantStep:   true,
		},
		{
			name:       "step absent when stale-check: false",
			workflowMD: disabledWorkflowMD,
			wantStep:   false,
		},
		{
			name:       "step present when stale-check: true explicitly",
			workflowMD: enabledExplicitWorkflowMD,
			wantStep:   true,
		},
		{
			name:              "full mode when stale-check: full",
			workflowMD:        fullStaleCheckWorkflowMD,
			wantStep:          true,
			wantFullCheckMode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "stale-check-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.workflowMD), 0644), "Should write workflow file")

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)
			require.NoError(t, err, "Workflow should compile without errors")

			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Lock file should be readable")
			lockStr := string(lockContent)

			hasStep := strings.Contains(lockStr, "Check workflow lock file")
			if tt.wantStep {
				assert.True(t, hasStep,
					"Expected 'Check workflow lock file' step in activation job but not found")
			} else {
				assert.False(t, hasStep,
					"Expected no 'Check workflow lock file' step in activation job but it was found")
			}

			// Verify GH_AW_STALE_CHECK_FULL env var is present when full mode is enabled
			if tt.wantFullCheckMode {
				assert.Contains(t, lockStr, "GH_AW_STALE_CHECK_FULL",
					"Expected GH_AW_STALE_CHECK_FULL env var in lock file for stale-check: full")
			} else {
				assert.NotContains(t, lockStr, "GH_AW_STALE_CHECK_FULL",
					"Expected no GH_AW_STALE_CHECK_FULL env var in lock file when not full mode")
			}

			// Verify stale-check is commented out in the generated lock file when present
			if strings.Contains(tt.workflowMD, "stale-check:") {
				assert.NotContains(t, lockStr, "\n  stale-check:",
					"stale-check should be commented out in the lock file, not left as an active YAML key")
				assert.Contains(t, lockStr, "# stale-check:",
					"stale-check should appear as a comment in the lock file")
			}
		})
	}
}

func TestStaleCheckFrontmatterHashParityForPinnedAndUnpinnedSource(t *testing.T) {
	tests := []struct {
		name       string
		sourceLine string
	}{
		{
			name:       "pinned source sha",
			sourceLine: "source: github/gh-aw/.github/workflows/test.md@0123456789abcdef0123456789abcdef01234567",
		},
		{
			name:       "unpinned source ref",
			sourceLine: "source: github/gh-aw/.github/workflows/test.md@main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "stale-check-hash-parity")
			workflowPath := filepath.Join(tmpDir, "hash-parity.md")
			workflowMD := `---
engine: copilot
on:
  workflow_dispatch:
` + tt.sourceLine + `
---
Hash parity regression coverage.
`

			require.NoError(t, os.WriteFile(workflowPath, []byte(workflowMD), 0644), "Should write workflow file")

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(workflowPath)
			require.NoError(t, err, "Workflow should compile without errors")

			lockPath := stringutil.MarkdownToLockFile(workflowPath)
			lockContent, err := os.ReadFile(lockPath)
			require.NoError(t, err, "Lock file should be readable")

			metadata, _, err := ExtractMetadataFromLockFile(string(lockContent))
			require.NoError(t, err, "Lock metadata should be parseable")
			require.NotNil(t, metadata, "Lock metadata should exist")

			currentHash, err := parser.ComputeFrontmatterHashFromFile(workflowPath, parser.NewImportCache(tmpDir))
			require.NoError(t, err, "Frontmatter hash should be recomputable from workflow markdown")

			assert.Equal(t, currentHash, metadata.FrontmatterHash,
				"Frontmatter hash in lock metadata should match markdown source hash")
		})
	}
}

// TestBodyHashStoredInLockMetadata verifies that the body hash is computed and stored in the
// lock metadata when a workflow is compiled.
func TestBodyHashStoredInLockMetadata(t *testing.T) {
	workflowMD := `---
engine: copilot
on:
  issues:
    types: [opened]
---
This is the body of the workflow. It contains the agent instructions.

## Steps

1. Do something
2. Do something else
`

	tmpDir := testutil.TempDir(t, "body-hash-test")
	workflowPath := filepath.Join(tmpDir, "body-hash.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowMD), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Workflow should compile without errors")

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err, "Lock file should be readable")

	metadata, _, err := ExtractMetadataFromLockFile(string(lockContent))
	require.NoError(t, err, "Lock metadata should be parseable")
	require.NotNil(t, metadata, "Lock metadata should exist")

	assert.Equal(t, LockSchemaV4, metadata.SchemaVersion, "Should use v4 schema with body hash")
	assert.NotEmpty(t, metadata.BodyHash, "Body hash should be stored in lock metadata")
	assert.Len(t, metadata.BodyHash, 64, "Body hash should be a 64-character hex string")

	// Verify the body hash is recomputable
	currentBodyHash, err := parser.ComputeBodyHashFromFile(workflowPath)
	require.NoError(t, err, "Body hash should be recomputable from workflow file")

	assert.Equal(t, currentBodyHash, metadata.BodyHash,
		"Body hash in lock metadata should match recomputed hash")
}

// TestBodyHashChangesWhenBodyChanges verifies that modifying the body text produces a different hash.
func TestBodyHashChangesWhenBodyChanges(t *testing.T) {
	originalMD := `---
engine: copilot
on:
  issues:
    types: [opened]
---
Original body content.
`
	modifiedMD := `---
engine: copilot
on:
  issues:
    types: [opened]
---
Modified body content with different instructions.
`

	tmpDir := testutil.TempDir(t, "body-hash-change-test")
	workflowPath := filepath.Join(tmpDir, "test.md")

	// Compile original
	require.NoError(t, os.WriteFile(workflowPath, []byte(originalMD), 0644))
	originalHash, err := parser.ComputeBodyHashFromFile(workflowPath)
	require.NoError(t, err)

	// Compile modified
	require.NoError(t, os.WriteFile(workflowPath, []byte(modifiedMD), 0644))
	modifiedHash, err := parser.ComputeBodyHashFromFile(workflowPath)
	require.NoError(t, err)

	assert.NotEqual(t, originalHash, modifiedHash,
		"Body hash should change when body content changes")
}

// TestBodyHashStableWhenFrontmatterChanges verifies the body hash does not change when only
// the frontmatter changes (and the body stays the same).
func TestBodyHashStableWhenFrontmatterChanges(t *testing.T) {
	body := "\nThis is the body content that should stay the same.\n"
	originalMD := "---\nengine: copilot\non:\n  issues:\n    types: [opened]\n---" + body
	modifiedFrontmatterMD := "---\nengine: copilot\non:\n  issues:\n    types: [opened, edited]\n---" + body

	tmpDir := testutil.TempDir(t, "body-hash-stable-test")
	workflowPath := filepath.Join(tmpDir, "test.md")

	require.NoError(t, os.WriteFile(workflowPath, []byte(originalMD), 0644))
	originalHash, err := parser.ComputeBodyHashFromFile(workflowPath)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(workflowPath, []byte(modifiedFrontmatterMD), 0644))
	modifiedHash, err := parser.ComputeBodyHashFromFile(workflowPath)
	require.NoError(t, err)

	assert.Equal(t, originalHash, modifiedHash,
		"Body hash should not change when only frontmatter changes")
}
