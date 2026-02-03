//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArtifactManagerIntegrationWithCompiler tests that the artifact manager
// is properly integrated into the compiler and resets between compilations
func TestArtifactManagerIntegrationWithCompiler(t *testing.T) {
	tmpDir := testutil.TempDir(t, "artifact-manager-integration-*")

	// Create a simple workflow
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Test Artifact Manager Integration

This test verifies that the artifact manager is integrated into the compiler.
`

	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Create compiler
	compiler := NewCompiler()

	// Verify artifact manager is initialized
	artifactManager := compiler.GetArtifactManager()
	require.NotNil(t, artifactManager, "Artifact manager should be initialized")

	// First compilation
	err = compiler.CompileWorkflow(workflowFile)
	require.NoError(t, err)

	// Artifact manager should be reset (empty) after first compilation
	assert.Empty(t, artifactManager.GetAllArtifacts(), "Artifact manager should be reset between compilations")

	// Manually add some test data to artifact manager
	artifactManager.SetCurrentJob("test-job")
	err = artifactManager.RecordUpload(&ArtifactUpload{
		Name:    "test-artifact",
		Paths:   []string{"/tmp/test.txt"},
		JobName: "test-job",
	})
	require.NoError(t, err)

	// Second compilation should reset the artifact manager
	err = compiler.CompileWorkflow(workflowFile)
	require.NoError(t, err)

	// Verify artifact manager was reset
	assert.Empty(t, artifactManager.GetAllArtifacts(), "Artifact manager should be reset after second compilation")
}

// TestArtifactManagerAccessDuringCompilation demonstrates how the artifact
// manager can be accessed and used during workflow compilation
func TestArtifactManagerAccessDuringCompilation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "artifact-manager-access-*")

	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
engine: copilot
---

# Test Artifact Manager Access

This workflow has safe outputs configured.
`

	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()

	// Compile the workflow
	err = compiler.CompileWorkflow(workflowFile)
	require.NoError(t, err)

	// Access the artifact manager after compilation
	artifactManager := compiler.GetArtifactManager()
	require.NotNil(t, artifactManager)

	// The manager should be available but empty (since we didn't track anything yet)
	// In future integration, the compiler would populate this during job generation
	assert.NotNil(t, artifactManager, "Artifact manager should be accessible after compilation")
}

// TestArtifactManagerWithMultipleWorkflows tests that the artifact manager
// properly resets between multiple workflow compilations
func TestArtifactManagerWithMultipleWorkflows(t *testing.T) {
	tmpDir := testutil.TempDir(t, "artifact-manager-multi-*")

	// Create multiple workflow files
	workflows := []struct {
		name    string
		content string
	}{
		{
			name: "workflow1.md",
			content: `---
on: push
permissions:
  contents: read
engine: copilot
---

# Workflow 1
Test workflow 1.
`,
		},
		{
			name: "workflow2.md",
			content: `---
on: pull_request
permissions:
  contents: read
engine: copilot
---

# Workflow 2
Test workflow 2.
`,
		},
		{
			name: "workflow3.md",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Workflow 3
Test workflow 3.
`,
		},
	}

	compiler := NewCompiler()
	artifactManager := compiler.GetArtifactManager()

	for i, wf := range workflows {
		workflowFile := filepath.Join(tmpDir, wf.name)
		err := os.WriteFile(workflowFile, []byte(wf.content), 0644)
		require.NoError(t, err)

		// Add some test data before compilation
		artifactManager.SetCurrentJob("test-job")
		err = artifactManager.RecordUpload(&ArtifactUpload{
			Name:    "artifact-" + wf.name,
			Paths:   []string{"/tmp/file.txt"},
			JobName: "test-job",
		})
		require.NoError(t, err)

		// Compile workflow
		err = compiler.CompileWorkflow(workflowFile)
		require.NoError(t, err, "Workflow %d should compile successfully", i+1)

		// Verify artifact manager was reset
		assert.Empty(t, artifactManager.GetAllArtifacts(),
			"Artifact manager should be reset after compiling workflow %d", i+1)

		// Verify lock file was created
		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		_, err = os.Stat(lockFile)
		assert.NoError(t, err, "Lock file should exist for workflow %d", i+1)
	}
}

// TestArtifactManagerLazyInitialization tests that the artifact manager
// is lazily initialized if not present
func TestArtifactManagerLazyInitialization(t *testing.T) {
	tmpDir := testutil.TempDir(t, "artifact-manager-lazy-*")

	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Test Lazy Init

Test lazy initialization.
`

	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Create compiler without initializing artifact manager
	compiler := &Compiler{
		verbose:          false,
		version:          "test",
		skipValidation:   true,
		actionMode:       ActionModeDev,
		jobManager:       NewJobManager(),
		engineRegistry:   GetGlobalEngineRegistry(),
		stepOrderTracker: NewStepOrderTracker(),
		// artifactManager intentionally not initialized
	}

	// GetArtifactManager should lazy-initialize
	artifactManager := compiler.GetArtifactManager()
	assert.NotNil(t, artifactManager, "GetArtifactManager should lazy-initialize")

	// Second call should return same instance
	artifactManager2 := compiler.GetArtifactManager()
	assert.Same(t, artifactManager, artifactManager2, "Should return same instance")
}

// TestArtifactManagerValidationExample demonstrates how validation could work
// This is a conceptual test showing how the artifact manager could validate
// artifact dependencies in a workflow
func TestArtifactManagerValidationExample(t *testing.T) {
	// Create a compiler with artifact manager
	compiler := NewCompiler()
	artifactManager := compiler.GetArtifactManager()

	// Simulate job 1 uploading an artifact
	artifactManager.SetCurrentJob("build")
	err := artifactManager.RecordUpload(&ArtifactUpload{
		Name:    "build-artifact",
		Paths:   []string{"/dist/app"},
		JobName: "build",
	})
	require.NoError(t, err)

	// Simulate job 2 downloading the artifact
	artifactManager.SetCurrentJob("test")
	err = artifactManager.RecordDownload(&ArtifactDownload{
		Name:      "build-artifact",
		Path:      "/tmp/build",
		JobName:   "test",
		DependsOn: []string{"build"},
	})
	require.NoError(t, err)

	// Validate all downloads
	errors := artifactManager.ValidateAllDownloads()
	assert.Empty(t, errors, "All downloads should be valid")

	// Simulate a job trying to download a non-existent artifact
	artifactManager.SetCurrentJob("deploy")
	err = artifactManager.RecordDownload(&ArtifactDownload{
		Name:      "nonexistent-artifact",
		Path:      "/tmp/deploy",
		JobName:   "deploy",
		DependsOn: []string{"build"},
	})
	require.NoError(t, err)

	// Validation should catch the missing artifact
	errors = artifactManager.ValidateAllDownloads()
	assert.Len(t, errors, 1, "Should detect missing artifact")
	assert.Contains(t, errors[0].Error(), "nonexistent-artifact")
	assert.Contains(t, errors[0].Error(), "not found")
}
