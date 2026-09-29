//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestGenerateSafeOutputsAssetsArtifactUploadRequiresRedaction(t *testing.T) {
	var builder strings.Builder
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			UploadAssets: &UploadAssetsConfig{},
		},
	}

	generateSafeOutputsAssetsArtifactUpload(&builder, data, getActionPin)

	if !strings.Contains(builder.String(), "if: always() && steps.redact_secrets.outcome == 'success'") {
		t.Error("Expected assets artifact upload to require successful secret redaction")
	}
}

func TestParseUploadAssetConfig(t *testing.T) {
	c := &Compiler{}

	tests := []struct {
		name     string
		input    map[string]any
		expected *UploadAssetsConfig
	}{
		{
			name: "upload-asset config with custom values",
			input: map[string]any{
				"upload-asset": map[string]any{
					"branch":       "my-assets/${{ github.event.repository.name }}",
					"max-size":     5120,
					"allowed-exts": []any{".jpg", ".png", ".txt"},
					"github-token": "${{ secrets.CUSTOM_TOKEN }}",
				},
			},
			expected: &UploadAssetsConfig{
				BranchName:           "my-assets/${{ github.event.repository.name }}",
				MaxSizeKB:            5120,
				AllowedExts:          []string{".jpg", ".png", ".txt"},
				BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: "${{ secrets.CUSTOM_TOKEN }}"},
			},
		},
		{
			name: "upload-asset config with max",
			input: map[string]any{
				"upload-asset": map[string]any{
					"max": 5,
				},
			},
			expected: &UploadAssetsConfig{
				BranchName:           "assets/${{ github.workflow }}",
				MaxSizeKB:            10240,
				AllowedExts:          []string{".png", ".jpg", ".jpeg"},
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
			},
		},
		{
			name: "upload-asset config normalizes allowed-exts without dots",
			input: map[string]any{
				"upload-asset": map[string]any{
					"allowed-exts": []any{"png", " SVG ", ".jpg", "png"},
				},
			},
			expected: &UploadAssetsConfig{
				BranchName:           "assets/${{ github.workflow }}",
				MaxSizeKB:            10240,
				AllowedExts:          []string{".png", ".svg", ".jpg"},
				BaseSafeOutputConfig: BaseSafeOutputConfig{},
			},
		},
		{
			name: "upload-asset config preserves github actions expressions in allowed-exts",
			input: map[string]any{
				"upload-asset": map[string]any{
					"allowed-exts": []any{"${{ inputs.allowed_exts }}", " PNG "},
				},
			},
			expected: &UploadAssetsConfig{
				BranchName:           "assets/${{ github.workflow }}",
				MaxSizeKB:            10240,
				AllowedExts:          []string{"${{ inputs.allowed_exts }}", ".png"},
				BaseSafeOutputConfig: BaseSafeOutputConfig{},
			},
		},
		{
			name:     "no upload-asset config",
			input:    map[string]any{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.parseUploadAssetConfig(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("Expected %+v, got nil", tt.expected)
				return
			}

			if result.BranchName != tt.expected.BranchName {
				t.Errorf("BranchName: expected %s, got %s", tt.expected.BranchName, result.BranchName)
			}

			if result.MaxSizeKB != tt.expected.MaxSizeKB {
				t.Errorf("MaxSizeKB: expected %d, got %d", tt.expected.MaxSizeKB, result.MaxSizeKB)
			}

			if result.GitHubToken != tt.expected.GitHubToken {
				t.Errorf("GitHubToken: expected %s, got %s", tt.expected.GitHubToken, result.GitHubToken)
			}

			if (result.Max == nil) != (tt.expected.Max == nil) || (result.Max != nil && *result.Max != *tt.expected.Max) {
				t.Errorf("Max: expected %v, got %v", tt.expected.Max, result.Max)
			}

			if len(result.AllowedExts) != len(tt.expected.AllowedExts) {
				t.Errorf("AllowedExts length: expected %d, got %d", len(tt.expected.AllowedExts), len(result.AllowedExts))
			}
			for i := range tt.expected.AllowedExts {
				if i >= len(result.AllowedExts) {
					break
				}
				if result.AllowedExts[i] != tt.expected.AllowedExts[i] {
					t.Errorf("AllowedExts[%d]: expected %s, got %s", i, tt.expected.AllowedExts[i], result.AllowedExts[i])
				}
			}
		})
	}
}

func TestHasSafeOutputsEnabledWithUploadAsset(t *testing.T) {
	// Test that UploadAsset is properly detected
	config := &SafeOutputsConfig{
		UploadAssets: &UploadAssetsConfig{},
	}

	if !HasSafeOutputsEnabled(config) {
		t.Error("Expected UploadAsset to be detected as enabled safe output")
	}

	// Test with nil config
	if HasSafeOutputsEnabled(nil) {
		t.Error("Expected nil config to return false")
	}

	// Test with empty config
	emptyConfig := &SafeOutputsConfig{}
	if HasSafeOutputsEnabled(emptyConfig) {
		t.Error("Expected empty config to return false")
	}
}

func TestUploadAssetsJobUsesFileInput(t *testing.T) {
	// Test that the upload_assets job reads from file (via env var) not JSON payload
	c := NewCompiler()
	data := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			UploadAssets: &UploadAssetsConfig{
				BranchName:  "assets/test",
				MaxSizeKB:   10240,
				AllowedExts: []string{".png", ".jpg"},
			},
		},
	}

	job, err := c.buildUploadAssetsJob(data, "agent", false)
	if err != nil {
		t.Fatalf("Failed to build upload assets job: %v", err)
	}

	// Convert steps to string to check for expected patterns
	var stepsStrSb strings.Builder
	for _, step := range job.Steps {
		stepsStrSb.WriteString(step)
	}
	stepsStr := stepsStrSb.String()

	// Verify artifact download steps are present
	if !strings.Contains(stepsStr, "Download agent output artifact") {
		t.Error("Expected artifact download step to be present")
	}

	if !strings.Contains(stepsStr, "Setup agent output environment variable") {
		t.Error("Expected environment variable setup step to be present")
	}

	// Verify the correct environment variable is used (file path, not JSON payload)
	if !strings.Contains(stepsStr, "GH_AW_AGENT_OUTPUT: ${{ steps.setup-agent-output-env.outputs.GH_AW_AGENT_OUTPUT }}") {
		t.Error("Expected GH_AW_AGENT_OUTPUT to use step output (file path)")
	}

	// Verify it does NOT use the old pattern (JSON payload)
	if strings.Contains(stepsStr, "${{ needs.agent.outputs.output }}") {
		t.Error("Should not use needs.*.outputs.output (JSON payload) - should use file path instead")
	}

	// Verify custom environment variables are present
	if !strings.Contains(stepsStr, "GH_AW_ASSETS_BRANCH") {
		t.Error("Expected GH_AW_ASSETS_BRANCH environment variable")
	}

	if !strings.Contains(stepsStr, "GH_AW_ASSETS_MAX_SIZE_KB") {
		t.Error("Expected GH_AW_ASSETS_MAX_SIZE_KB environment variable")
	}

	if !strings.Contains(stepsStr, "GH_AW_ASSETS_ALLOWED_EXTS") {
		t.Error("Expected GH_AW_ASSETS_ALLOWED_EXTS environment variable")
	}
}
