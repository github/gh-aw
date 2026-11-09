//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalRepositoryPackageManifest(t *testing.T) {
	packageDir := t.TempDir()
	manifestPath := filepath.Join(packageDir, repositoryPackageManifestFileName)
	require.NoError(t, os.WriteFile(manifestPath, []byte("name: test\n"), 0600))

	for _, source := range []string{packageDir, manifestPath} {
		resolvedManifest, resolvedDir, err := localRepositoryPackageManifest(source)
		require.NoError(t, err)
		require.Equal(t, manifestPath, resolvedManifest)
		require.Equal(t, packageDir, resolvedDir)
	}

	otherFile := filepath.Join(packageDir, "README.md")
	require.NoError(t, os.WriteFile(otherFile, []byte("test"), 0600))
	resolvedManifest, resolvedDir, err := localRepositoryPackageManifest(otherFile)
	require.NoError(t, err)
	require.Empty(t, resolvedManifest)
	require.Empty(t, resolvedDir)
}
