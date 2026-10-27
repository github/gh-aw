//go:build !integration

package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFrontmatterHashInterpolationVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/frontmatter_hash_interpolation_vectors.json")
	require.NoError(t, err)
	var vectors []struct {
		Name     string
		Markdown string
		Relevant []string
		All      []string
	}
	require.NoError(t, json.Unmarshal(data, &vectors))
	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			require.Equal(t, vector.Relevant, append([]string{}, extractRelevantTemplateExpressions(vector.Markdown)...))
			require.Equal(t, vector.All, append([]string{}, extractAllTemplateExpressions(vector.Markdown)...))
			root := t.TempDir()
			workflowDir := filepath.Join(root, ".github", "workflows")
			promptDir := filepath.Join(root, ".github", "prompts")
			require.NoError(t, os.MkdirAll(workflowDir, 0755))
			require.NoError(t, os.MkdirAll(promptDir, 0755))
			workflowPath := filepath.Join(workflowDir, "vector.md")
			promptPath := filepath.Join(promptDir, "vector.md")
			require.NoError(t, os.WriteFile(promptPath, []byte(vector.Markdown), 0644))
			for _, runtimeImport := range []bool{false, true} {
				body, expressions := vector.Markdown, vector.Relevant
				if runtimeImport {
					body, expressions = "{{#runtime-import prompts/vector.md}}", vector.All
				}
				require.NoError(t, os.WriteFile(workflowPath, []byte("---\nengine: copilot\n---\n"+body), 0644))
				canonical := map[string]any{"frontmatter-text": "engine: copilot"}
				if len(expressions) > 0 {
					canonical["template-expressions"] = expressions
				}
				expectedJSON, err := json.Marshal(canonical)
				require.NoError(t, err)
				digest := sha256.Sum256(expectedJSON)
				expected := hex.EncodeToString(digest[:])
				goHash, err := ComputeFrontmatterHashFromFile(workflowPath, NewImportCache(workflowDir))
				require.NoError(t, err)
				require.Equal(t, expected, goHash, "runtime import: %v", runtimeImport)
				jsHash, err := computeHashViaNode(workflowPath)
				require.NoError(t, err, "JavaScript hash parity is required")
				require.Equal(t, expected, jsHash, "runtime import: %v", runtimeImport)
			}
		})
	}
}
