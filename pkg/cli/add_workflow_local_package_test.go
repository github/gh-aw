package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalRepositoryPackageManifestFromFile(t *testing.T) {
	packageDir := t.TempDir()
	manifestPath := filepath.Join(packageDir, repositoryPackageManifestFileName)
	require.NoError(t, os.WriteFile(manifestPath, []byte("name: Local Package\n"), 0o644))
	readmePath := filepath.Join(packageDir, "README.md")
	require.NoError(t, os.WriteFile(readmePath, []byte("# Package\n"), 0o644))

	gotManifest, gotPackageDir, err := localRepositoryPackageManifest(manifestPath)
	require.NoError(t, err)
	assert.Equal(t, manifestPath, gotManifest)
	assert.Equal(t, packageDir, gotPackageDir)

	gotManifest, gotPackageDir, err = localRepositoryPackageManifest(readmePath)
	require.NoError(t, err)
	assert.Empty(t, gotManifest)
	assert.Empty(t, gotPackageDir)
}
