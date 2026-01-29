//go:build integration

package workflow

import (
	"strings"
	"testing"
)

func TestBranchNormalizationInlinedInMainJob(t *testing.T) {
	// Test that normalization logic is inlined in upload_assets.cjs when upload-assets is configured
	compiler := NewCompiler()

	// Create test workflow data with upload-assets configured
	data := &WorkflowData{
		Name:        "Test Workflow",
		On:          "on:\n  push:\n",
		Permissions: "permissions:\n  contents: read\n",
		SafeOutputs: &SafeOutputsConfig{
			UploadAssets: &UploadAssetsConfig{
				BranchName:  "assets/${{ github.workflow }}",
				MaxSizeKB:   10240,
				AllowedExts: []string{".png", ".jpg"},
			},
		},
		AI:              "copilot",
		EngineConfig:    &EngineConfig{ID: "copilot"},
		MarkdownContent: "Test content",
	}

	// Build the main job
	job, err := compiler.buildMainJob(data, false)
	if err != nil {
		t.Fatalf("Failed to build main job: %v", err)
	}

	// Check that the job has steps
	if len(job.Steps) == 0 {
		t.Fatal("Expected job to have steps")
	}

	// Convert steps to string
	stepsContent := strings.Join(job.Steps, "\n")

	// Verify that the separate normalization step is NOT present
	if strings.Contains(stepsContent, "Normalize GH_AW_ASSETS_BRANCH") {
		t.Error("Expected separate normalization step to NOT be present (should be inlined)")
	}
}

func TestBranchNormalizationStepNotAddedWhenNoUploadAssets(t *testing.T) {
	// Test that no normalization-related content appears when upload-assets is not configured
	compiler := NewCompiler()

	// Create test workflow data WITHOUT upload-assets
	data := &WorkflowData{
		Name:        "Test Workflow",
		On:          "on:\n  push:\n",
		Permissions: "permissions:\n  contents: read\n",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
		AI:              "copilot",
		EngineConfig:    &EngineConfig{ID: "copilot"},
		MarkdownContent: "Test content",
	}

	// Build the main job
	job, err := compiler.buildMainJob(data, false)
	if err != nil {
		t.Fatalf("Failed to build main job: %v", err)
	}

	// Check that the job has steps
	if len(job.Steps) == 0 {
		t.Fatal("Expected job to have steps")
	}

	// Convert steps to string
	stepsContent := strings.Join(job.Steps, "\n")

	// Verify that the normalization step is NOT present
	if strings.Contains(stepsContent, "Normalize GH_AW_ASSETS_BRANCH") {
		t.Error("Expected normalization step to NOT be present when upload-assets is not configured")
	}
}

// TestUploadAssetsJobHasInlinedNormalization tests that upload_assets job has inlined normalization logic
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestUploadAssetsJobHasInlinedNormalization(t *testing.T) {
	t.Skip("Upload assets inlined normalization tests skipped - scripts now use require() pattern to load external files at runtime")
}
