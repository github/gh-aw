//go:build !integration

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPMHashDocumentsAndMissingFiles(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".github", "workflows")
	manifestPath := filepath.Join(base, "apm.yml")
	lockPath := filepath.Join(base, "apm.lock.yaml")
	files := map[string]string{manifestPath: "name: project\nversion: 1.0.0\n", lockPath: "lockfile_version: '1'\ndependencies: []\n"}
	reader := func(path string) ([]byte, error) {
		content, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("missing %s", path)
		}

		return []byte(content), nil
	}
	for _, spelling := range []string{"apm: apm.yml", `apm: "apm.yml" # comment`, "apm: 'apm.yml'", `"apm": apm.yml`} {
		assert.Equal(t, "apm.yml", ExtractAPMManifestPathText(spelling))
		first, err := ComputeFrontmatterHashFromParsedContent(spelling, "", map[string]any{"apm": "apm.yml"}, base, nil, reader)
		require.NoError(t, err)
		files[lockPath] += "# branch head changed\n"
		second, err := ComputeFrontmatterHashFromParsedContent(spelling, "", map[string]any{"apm": "apm.yml"}, base, nil, reader)
		require.NoError(t, err)
		assert.NotEqual(t, first, second)
	}
	delete(files, lockPath)
	_, err := ComputeFrontmatterHashFromParsedContent("apm: apm.yml", "", nil, base, nil, reader)
	require.ErrorContains(t, err, "cannot hash APM document")
	assert.Empty(t, ExtractAPMManifestPathText("engine: copilot"))
	files[lockPath] = strings.Repeat("x", maxFrontmatterHashInputBytes)
	_, err = ComputeFrontmatterHashFromParsedContent("apm: apm.yml", "", nil, base, nil, reader)
	require.ErrorContains(t, err, "hash input exceeds")
}

func TestAPMHashGoJavaScriptParity(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0755))
	workflow := filepath.Join(dir, "review.md")
	require.NoError(t, os.WriteFile(workflow, []byte("---\nengine: copilot\napm: ../../apm.yml\n---\nReview code.\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "apm.yml"), []byte("name: review\nversion: 1.0.0\n"), 0644))
	lockPath := filepath.Join(root, "apm.lock.yaml")
	for _, content := range []string{"lockfile_version: '1'\ndependencies: []\n", "lockfile_version: '1'\ndependencies: []\n# branch changed\n"} {
		require.NoError(t, os.WriteFile(lockPath, []byte(content), 0644))
		goHash, err := ComputeFrontmatterHashFromFile(workflow, nil)
		require.NoError(t, err)
		jsHash, err := computeHashViaNode(workflow)
		require.NoError(t, err)
		assert.Equal(t, goHash, jsHash)
	}
}
