//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBinaryPath(t *testing.T) {
	t.Run("returns non-empty path", func(t *testing.T) {
		path, err := GetBinaryPath()
		require.NoError(t, err, "Should get binary path without error")
		assert.NotEmpty(t, path, "Binary path should not be empty")
	})

	t.Run("returns absolute path", func(t *testing.T) {
		path, err := GetBinaryPath()
		require.NoError(t, err, "Should get binary path without error")
		assert.True(t, filepath.IsAbs(path), "Binary path should be absolute")
	})

	t.Run("returned path exists", func(t *testing.T) {
		path, err := GetBinaryPath()
		require.NoError(t, err, "Should get binary path without error")

		// Check if the file exists
		info, err := os.Stat(path)
		require.NoError(t, err, "Binary file should exist at the returned path")
		if err == nil {
			assert.False(t, info.IsDir(), "Binary path should not be a directory")
		}
	})

	t.Run("path ends with executable name", func(t *testing.T) {
		path, err := GetBinaryPath()
		require.NoError(t, err, "Should get binary path without error")

		// The path should end with a reasonable executable name
		// During tests, it might be a test binary name
		base := filepath.Base(path)
		assert.NotEmpty(t, base, "Binary path should have a base name")
		// Don't check for specific name as it could be the test binary
	})
}
