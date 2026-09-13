//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateManifestResourceDestinationSharedWorkflowScripts(t *testing.T) {
	t.Parallel()

	for _, destination := range []string{
		".github/workflows/shared/runtime.mjs",
		".github/workflows/shared/lib/runtime.cjs",
		".github/workflows/shared/runtime.MJS",
	} {
		t.Run(destination, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, validateManifestResourceDestination(destination))
			assert.True(t, isPackageResourceDestination(destination))
		})
	}

	for _, destination := range []string{
		".github/workflows/shared/runtime.js",
		".github/workflows/shared/runtime.json",
		".github/workflows/shared/runtime.md",
		".github/workflows/shared/",
		".github/workflows/shared-adjacent/runtime.mjs",
	} {
		t.Run(destination, func(t *testing.T) {
			t.Parallel()
			require.Error(t, validateManifestResourceDestination(destination))
			assert.False(t, isPackageResourceDestination(destination))
		})
	}

	_, err := parseManifestResourceMapping(map[string]any{
		"source":      "runtime.mjs",
		"destination": ".github/workflows/shared/../runtime.mjs",
	}, "aw.yml")
	require.Error(t, err)
}
