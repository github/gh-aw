package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcessImportsRetainsResolvedMetadataDocuments(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	sharedDir := filepath.Join(root, ".github", "aw")
	require.NoError(t, os.MkdirAll(workflowDir, 0o755))
	require.NoError(t, os.MkdirAll(sharedDir, 0o755))
	parent := "---\nimports:\n  - ./models.md\n---\nParent body\n"
	models := "---\nmodels:\n  custom: [openai/model]\n---\nModels body\n"
	parentPath := filepath.Join(sharedDir, "parent.md")
	modelPath := filepath.Join(sharedDir, "models.md")
	require.NoError(t, os.WriteFile(parentPath, []byte(parent), 0o644))
	require.NoError(t, os.WriteFile(modelPath, []byte(models), 0o644))
	result, err := ProcessImportsFromFrontmatterWithSource(map[string]any{"imports": []any{".github/aw/parent.md"}}, workflowDir, nil, filepath.Join(workflowDir, "main.md"), "")
	require.NoError(t, err)
	require.Equal(t, parent, result.MetadataDocuments[parentPath])
	require.Equal(t, models, result.MetadataDocuments[modelPath])
}
