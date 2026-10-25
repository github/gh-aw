package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritePriorManifestFile(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
	cache := map[string]*workflow.GHAWManifest{"example.lock.yml": nil}

	path, err := WritePriorManifestFile(cache)
	require.NoError(t, err)
	assert.Equal(t, dir, filepath.Dir(path))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"example.lock.yml":null}`, string(content))
}

func TestWritePriorManifestFileCreateFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}

	path, err := WritePriorManifestFile(nil)
	require.Error(t, err)
	assert.Empty(t, path)
	assert.ErrorContains(t, err, "create manifest cache file")
}
