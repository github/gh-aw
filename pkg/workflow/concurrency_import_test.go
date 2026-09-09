//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrencyJobDiscriminatorImport(t *testing.T) {
	tempDir := t.TempDir()
	sharedPath := filepath.Join(tempDir, "shared.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte(`---
concurrency:
  job-discriminator: ${{ inputs.shared_id }}
---

Shared concurrency configuration.
`), 0o644))

	tests := []struct {
		name                  string
		mainConcurrency       string
		expectedDiscriminator string
	}{
		{
			name:                  "uses imported discriminator",
			expectedDiscriminator: "${{ inputs.shared_id }}",
		},
		{
			name: "main workflow wins",
			mainConcurrency: `concurrency:
  job-discriminator: ${{ inputs.main_id }}
`,
			expectedDiscriminator: "${{ inputs.main_id }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mainPath := filepath.Join(tempDir, tt.name+".md")
			content := "---\non: workflow_dispatch\nimports:\n  - shared.md\n" + tt.mainConcurrency + "---\n\nMain workflow.\n"
			require.NoError(t, os.WriteFile(mainPath, []byte(content), 0o644))

			data, err := NewCompiler().ParseWorkflowFile(mainPath)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedDiscriminator, data.ConcurrencyJobDiscriminator)
		})
	}
}

func TestConcurrencyGroupImport(t *testing.T) {
	tempDir := t.TempDir()
	sharedPath := filepath.Join(tempDir, "shared.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte(`---
concurrency:
  group: ${{ github.workflow }}-${{ inputs.shared_id }}
---

Shared concurrency configuration.
`), 0o644))

	tests := []struct {
		name            string
		mainConcurrency string
		expectedGroup   string
	}{
		{
			name:          "uses imported group",
			expectedGroup: "${{ github.workflow }}-${{ inputs.shared_id }}",
		},
		{
			name: "main workflow wins",
			mainConcurrency: `concurrency:
  group: ${{ github.workflow }}-${{ inputs.main_id }}
`,
			expectedGroup: "${{ github.workflow }}-${{ inputs.main_id }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mainPath := filepath.Join(tempDir, tt.name+".md")
			content := "---\non: workflow_dispatch\nimports:\n  - shared.md\n" + tt.mainConcurrency + "---\n\nMain workflow.\n"
			require.NoError(t, os.WriteFile(mainPath, []byte(content), 0o644))

			data, err := NewCompiler().ParseWorkflowFile(mainPath)
			require.NoError(t, err)
			assert.Contains(t, data.Concurrency, "group")
			assert.Contains(t, data.Concurrency, tt.expectedGroup)
		})
	}
}

func TestSharedWorkflowConcurrencyValidationInSubdirectory(t *testing.T) {
	tempDir := t.TempDir()
	sharedDir := filepath.Join(tempDir, ".github", "workflows", "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0o755))
	sharedPath := filepath.Join(sharedDir, "invalid.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte(`---
concurrency:
  cancel-in-progress: true
---

Unsupported shared concurrency.
`), 0o644))

	mainPath := filepath.Join(tempDir, "main.md")
	content := "---\non: workflow_dispatch\nimports:\n  - .github/workflows/shared/invalid.md\n---\n\nMain workflow.\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(content), 0o644))

	_, err := NewCompiler().ParseWorkflowFile(mainPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported key: cancel-in-progress")
}
