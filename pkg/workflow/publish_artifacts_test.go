//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/setutil"
)

func TestParseUploadArtifactConfig(t *testing.T) {
	c := &Compiler{}

	tests := []struct {
		name     string
		input    map[string]any
		expected *UploadArtifactConfig
		isNil    bool
	}{
		{
			name:  "no upload-artifact key",
			input: map[string]any{},
			isNil: true,
		},
		{
			name:  "upload-artifact explicitly false",
			input: map[string]any{"upload-artifact": false},
			isNil: true,
		},
		{
			name:  "upload-artifact true uses defaults",
			input: map[string]any{"upload-artifact": true},
			expected: &UploadArtifactConfig{
				MaxUploads:   defaultArtifactMaxUploads,
				MaxSizeBytes: defaultArtifactMaxSizeBytes,
			},
		},
		{
			name: "upload-artifact with retention-days and skip-archive",
			input: map[string]any{
				"upload-artifact": map[string]any{
					"max-uploads":    3,
					"retention-days": 30,
					"skip-archive":   true,
					"max-size-bytes": 52428800,
					"github-token":   "${{ secrets.MY_TOKEN }}",
				},
			},
			expected: &UploadArtifactConfig{
				MaxUploads:           3,
				RetentionDays:        strPtr("30"),
				SkipArchive:          strPtr("true"),
				MaxSizeBytes:         52428800,
				BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: "${{ secrets.MY_TOKEN }}"},
			},
		},
		{
			name: "upload-artifact with templated retention-days and skip-archive",
			input: map[string]any{
				"upload-artifact": map[string]any{
					"retention-days": "${{ inputs.retention }}",
					"skip-archive":   "${{ inputs.skip }}",
				},
			},
			expected: &UploadArtifactConfig{
				MaxUploads:    defaultArtifactMaxUploads,
				MaxSizeBytes:  defaultArtifactMaxSizeBytes,
				RetentionDays: strPtr("${{ inputs.retention }}"),
				SkipArchive:   strPtr("${{ inputs.skip }}"),
			},
		},
		{
			name: "upload-artifact with allowed-paths",
			input: map[string]any{
				"upload-artifact": map[string]any{
					"allowed-paths": []any{"dist/**", "reports/**"},
				},
			},
			expected: &UploadArtifactConfig{
				MaxUploads:   defaultArtifactMaxUploads,
				MaxSizeBytes: defaultArtifactMaxSizeBytes,
				AllowedPaths: []string{"dist/**", "reports/**"},
			},
		},
		{
			name: "upload-artifact with filters",
			input: map[string]any{
				"upload-artifact": map[string]any{
					"filters": map[string]any{
						"include": []any{"reports/**/*.json"},
						"exclude": []any{"**/*.env", "**/*.pem"},
					},
				},
			},
			expected: &UploadArtifactConfig{
				MaxUploads:   defaultArtifactMaxUploads,
				MaxSizeBytes: defaultArtifactMaxSizeBytes,
				Filters: &ArtifactFiltersConfig{
					Include: []string{"reports/**/*.json"},
					Exclude: []string{"**/*.env", "**/*.pem"},
				},
			},
		},
		{
			name: "upload-artifact with defaults if-no-files",
			input: map[string]any{
				"upload-artifact": map[string]any{
					"defaults": map[string]any{
						"if-no-files": "ignore",
					},
				},
			},
			expected: &UploadArtifactConfig{
				MaxUploads:   defaultArtifactMaxUploads,
				MaxSizeBytes: defaultArtifactMaxSizeBytes,
				Defaults: &ArtifactDefaultsConfig{
					IfNoFiles: "ignore",
				},
			},
		},
		{
			name: "upload-artifact with max field",
			input: map[string]any{
				"upload-artifact": map[string]any{
					"max": 5,
				},
			},
			expected: &UploadArtifactConfig{
				MaxUploads:           defaultArtifactMaxUploads,
				MaxSizeBytes:         defaultArtifactMaxSizeBytes,
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.parseUploadArtifactConfig(tt.input)

			if tt.isNil {
				assert.Nil(t, result, "expected nil result")
				return
			}

			require.NotNil(t, result, "expected non-nil result")
			assert.Equal(t, tt.expected.MaxUploads, result.MaxUploads, "MaxUploads mismatch")
			assert.Equal(t, tt.expected.MaxSizeBytes, result.MaxSizeBytes, "MaxSizeBytes mismatch")
			assert.Equal(t, tt.expected.AllowedPaths, result.AllowedPaths, "AllowedPaths mismatch")
			assert.Equal(t, tt.expected.GitHubToken, result.GitHubToken, "GitHubToken mismatch")

			if tt.expected.RetentionDays == nil {
				assert.Nil(t, result.RetentionDays, "RetentionDays should be nil")
			} else {
				require.NotNil(t, result.RetentionDays, "RetentionDays should not be nil")
				assert.Equal(t, *tt.expected.RetentionDays, *result.RetentionDays, "RetentionDays value mismatch")
			}

			if tt.expected.SkipArchive == nil {
				assert.Nil(t, result.SkipArchive, "SkipArchive should be nil")
			} else {
				require.NotNil(t, result.SkipArchive, "SkipArchive should not be nil")
				assert.Equal(t, *tt.expected.SkipArchive, *result.SkipArchive, "SkipArchive value mismatch")
			}

			if tt.expected.Max == nil {
				assert.Nil(t, result.Max, "Max should be nil")
			} else {
				require.NotNil(t, result.Max, "Max should not be nil")
				assert.Equal(t, *tt.expected.Max, *result.Max, "Max value mismatch")
			}

			if tt.expected.Filters == nil {
				assert.Nil(t, result.Filters, "Filters should be nil")
			} else {
				require.NotNil(t, result.Filters, "Filters should not be nil")
				assert.Equal(t, tt.expected.Filters.Include, result.Filters.Include, "Filters.Include mismatch")
				assert.Equal(t, tt.expected.Filters.Exclude, result.Filters.Exclude, "Filters.Exclude mismatch")
			}

			if tt.expected.Defaults == nil {
				assert.Nil(t, result.Defaults, "Defaults should be nil")
			} else {
				require.NotNil(t, result.Defaults, "Defaults should not be nil")
				assert.Equal(t, tt.expected.Defaults.IfNoFiles, result.Defaults.IfNoFiles, "Defaults.IfNoFiles mismatch")
			}
		})
	}
}

func TestHasSafeOutputsEnabledWithUploadArtifact(t *testing.T) {
	t.Run("UploadArtifact is detected as enabled", func(t *testing.T) {
		config := &SafeOutputsConfig{
			UploadArtifact: &UploadArtifactConfig{},
		}
		assert.True(t, HasSafeOutputsEnabled(config), "UploadArtifact should be detected as enabled")
	})

	t.Run("nil SafeOutputsConfig returns false", func(t *testing.T) {
		assert.False(t, HasSafeOutputsEnabled(nil), "nil config should return false")
	})

	t.Run("empty SafeOutputsConfig returns false", func(t *testing.T) {
		assert.False(t, HasSafeOutputsEnabled(&SafeOutputsConfig{}), "empty config should return false")
	})
}

func TestComputeEnabledToolNamesIncludesUploadArtifact(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			UploadArtifact: &UploadArtifactConfig{},
		},
	}
	tools := computeEnabledToolNames(data)
	assert.True(t, setutil.Contains(tools, "upload_artifact"), "upload_artifact should be in enabled tools")
}

func TestGenerateSafeOutputsArtifactStagingUpload(t *testing.T) {
	t.Run("generates step when UploadArtifact is configured", func(t *testing.T) {
		var b strings.Builder
		data := &WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				UploadArtifact: &UploadArtifactConfig{},
			},
		}
		generateSafeOutputsArtifactStagingUpload(&b, data, getActionPin)
		result := b.String()
		assert.Contains(t, result, "safe-outputs-upload-artifacts", "should reference staging artifact name")
		assert.Contains(t, result, artifactStagingDirExpr, "should reference staging directory")
		assert.Contains(t, result, "if: always() && steps.redact_secrets.outcome == 'success'",
			"should require successful secret redaction")
	})

	t.Run("generates nothing when UploadArtifact is nil", func(t *testing.T) {
		var b strings.Builder
		data := &WorkflowData{
			SafeOutputs: &SafeOutputsConfig{UploadArtifact: nil},
		}
		generateSafeOutputsArtifactStagingUpload(&b, data, getActionPin)
		assert.Empty(t, b.String(), "should generate nothing when UploadArtifact is nil")
	})

	t.Run("generates nothing when SafeOutputs is nil", func(t *testing.T) {
		var b strings.Builder
		data := &WorkflowData{SafeOutputs: nil}
		generateSafeOutputsArtifactStagingUpload(&b, data, getActionPin)
		assert.Empty(t, b.String(), "should generate nothing when SafeOutputs is nil")
	})
}
