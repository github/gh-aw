//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContainerPinCRUD verifies the Get/Set/Delete lifecycle for container pins.
func TestContainerPinCRUD(t *testing.T) {
	cache := NewActionCache(t.TempDir())

	// Initially no pin.
	_, ok := cache.GetContainerPin("node:lts-alpine")
	assert.False(t, ok, "no pin expected before Set")

	// Set a pin.
	cache.SetContainerPin("node:lts-alpine", "sha256:abc123", "node:lts-alpine@sha256:abc123")
	pin, ok := cache.GetContainerPin("node:lts-alpine")
	require.True(t, ok, "pin should exist after Set")
	assert.Equal(t, "node:lts-alpine", pin.Image, "Image field")
	assert.Equal(t, "sha256:abc123", pin.Digest, "Digest field")
	assert.Equal(t, "node:lts-alpine@sha256:abc123", pin.PinnedImage, "PinnedImage field")

	// Overwrite with updated pin.
	cache.SetContainerPin("node:lts-alpine", "sha256:updated", "node:lts-alpine@sha256:updated")
	pin, ok = cache.GetContainerPin("node:lts-alpine")
	require.True(t, ok, "pin should exist after update")
	assert.Equal(t, "sha256:updated", pin.Digest, "updated Digest")

	// Delete the pin.
	cache.DeleteContainerPin("node:lts-alpine")
	_, ok = cache.GetContainerPin("node:lts-alpine")
	assert.False(t, ok, "pin should be gone after Delete")

	// Deleting a non-existent pin is a no-op.
	assert.NotPanics(t, func() { cache.DeleteContainerPin("nonexistent:latest") }, "delete non-existent should not panic")
}

// TestContainerPinSaveLoad verifies that container pins survive a JSON round-trip.
func TestContainerPinSaveLoad(t *testing.T) {
	tmpDir := testutil.TempDir(t, "container-pin-*")

	cache := NewActionCache(tmpDir)
	cache.Set("actions/checkout", "v5", "sha1")
	cache.SetContainerPin("node:lts-alpine", "sha256:abc123", "node:lts-alpine@sha256:abc123")
	cache.SetContainerPin("alpine:latest", "sha256:def456", "alpine:latest@sha256:def456")

	require.NoError(t, cache.Save(), "Save should succeed")

	// Reload from disk.
	cache2 := NewActionCache(tmpDir)
	require.NoError(t, cache2.Load(), "Load should succeed")

	// Action entry should be preserved.
	sha, ok := cache2.Get("actions/checkout", "v5")
	require.True(t, ok, "action entry should be loaded")
	assert.Equal(t, "sha1", sha, "SHA should match")

	// Container pins should be preserved.
	pin, ok := cache2.GetContainerPin("node:lts-alpine")
	require.True(t, ok, "node pin should be loaded")
	assert.Equal(t, "sha256:abc123", pin.Digest, "node digest")
	assert.Equal(t, "node:lts-alpine@sha256:abc123", pin.PinnedImage, "node pinned image")

	pin, ok = cache2.GetContainerPin("alpine:latest")
	require.True(t, ok, "alpine pin should be loaded")
	assert.Equal(t, "sha256:def456", pin.Digest, "alpine digest")
}

// TestContainerPinBackwardCompatibility verifies that loading an existing
// actions-lock.json without a container_pins section returns an empty map
// (no error, no panic).
func TestContainerPinBackwardCompatibility(t *testing.T) {
	tmpDir := testutil.TempDir(t, "container-compat-*")

	// Write a legacy actions-lock.json (no container_pins field).
	legacyJSON := `{
  "entries": {
    "actions/checkout@v5": {
      "repo": "actions/checkout",
      "version": "v5",
      "sha": "abc123"
    }
  }
}
`
	jsonPath := filepath.Join(tmpDir, ".github", "aw", CacheFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(jsonPath), 0755))
	require.NoError(t, os.WriteFile(jsonPath, []byte(legacyJSON), 0644))

	cache := NewActionCache(tmpDir)
	require.NoError(t, cache.Load(), "Load should succeed for legacy file")

	// ContainerPins should be an initialized (empty) map, not nil.
	assert.NotNil(t, cache.ContainerPins, "ContainerPins should not be nil after Load")
	assert.Empty(t, cache.ContainerPins, "ContainerPins should be empty for legacy file")

	// Action entries should still be present.
	sha, ok := cache.Get("actions/checkout", "v5")
	require.True(t, ok, "action entry should be loaded from legacy file")
	assert.Equal(t, "abc123", sha, "SHA should match")
}

// TestContainerPinMarshalSortedOutput verifies that container pins are written
// in sorted order and that the JSON is valid.
func TestContainerPinMarshalSortedOutput(t *testing.T) {
	tmpDir := testutil.TempDir(t, "container-marshal-*")
	cache := NewActionCache(tmpDir)
	cache.Set("actions/checkout", "v5", "sha1")
	cache.SetContainerPin("z-image:latest", "sha256:zzz", "z-image:latest@sha256:zzz")
	cache.SetContainerPin("m-image:v2", "sha256:mmm", "m-image:v2@sha256:mmm")
	cache.SetContainerPin("a-image:latest", "sha256:aaa", "a-image:latest@sha256:aaa")

	require.NoError(t, cache.Save())

	content, err := os.ReadFile(filepath.Join(tmpDir, ".github", "aw", CacheFileName))
	require.NoError(t, err)

	contentStr := string(content)

	// Verify that entries appear in alphabetical order by checking their positions
	containers := []string{
		"a-image:latest",
		"m-image:v2",
		"z-image:latest",
	}

	lastPos := -1
	for _, container := range containers {
		pos := indexOf(contentStr, `"`+container+`"`)
		if pos == -1 {
			t.Errorf("Container %s not found in cache file", container)
			continue
		}
		if pos < lastPos {
			t.Errorf("Container %s appears before previous container (not sorted)", container)
		}
		lastPos = pos
	}

	// Verify containers section is present
	assert.Contains(t, contentStr, `"containers"`, "containers section present")

	// Reload and verify round-trip.
	cache2 := NewActionCache(tmpDir)
	require.NoError(t, cache2.Load())
	pin, ok := cache2.GetContainerPin("a-image:latest")
	require.True(t, ok)
	assert.Equal(t, "sha256:aaa", pin.Digest)
	pin, ok = cache2.GetContainerPin("m-image:v2")
	require.True(t, ok)
	assert.Equal(t, "sha256:mmm", pin.Digest)
	pin, ok = cache2.GetContainerPin("z-image:latest")
	require.True(t, ok)
	assert.Equal(t, "sha256:zzz", pin.Digest)
}

// TestPruneStaleContainerPins verifies that PruneStaleContainerPins removes
// entries not present in the known-image set and preserves entries that are.
//
// Canonical AWF and router images are exempt from pruning even when no longer
// referenced by any local lock file, so historical versions retain their embedded
// digest pins.
func TestPruneStaleContainerPins(t *testing.T) {
	cache := NewActionCache(t.TempDir())

	// Populate with stale and current canonical pins, plus ordinary pins.
	cache.SetContainerPin("ghcr.io/github/gh-aw-firewall/agent:0.27.0", "sha256:old", "ghcr.io/github/gh-aw-firewall/agent:0.27.0@sha256:old")
	cache.SetContainerPin("ghcr.io/github/gh-aw-firewall/agent:0.27.2", "sha256:new", "ghcr.io/github/gh-aw-firewall/agent:0.27.2@sha256:new")
	cache.SetContainerPin(constants.DefaultRouterRegistry+":0.1.2", "sha256:router-old", constants.DefaultRouterRegistry+":0.1.2@sha256:router-old")
	cache.SetContainerPin("node:lts-alpine", "sha256:node", "node:lts-alpine@sha256:node")
	cache.SetContainerPin("stale-registry.example.com/other:v1", "sha256:stale", "stale-registry.example.com/other:v1@sha256:stale")

	// Lock files now only reference the new AWF version and the node image.
	knownImages := map[string]struct{}{
		"ghcr.io/github/gh-aw-firewall/agent:0.27.2": {},
		"node:lts-alpine": {},
	}

	pruned := cache.PruneStaleContainerPins(knownImages)
	assert.Equal(t, 1, pruned, "only the non-firewall stale pin should be pruned")

	// Canonical images are exempt from pruning, so historical versions must survive.
	pin, ok := cache.GetContainerPin("ghcr.io/github/gh-aw-firewall/agent:0.27.0")
	require.True(t, ok, "stale old-version gh-aw-firewall pin must be retained")
	assert.Equal(t, "sha256:old", pin.Digest)

	pin, ok = cache.GetContainerPin(constants.DefaultRouterRegistry + ":0.1.2")
	require.True(t, ok, "stale gh-aw-router pin must be retained")
	assert.Equal(t, "sha256:router-old", pin.Digest)

	// Non-firewall stale pin should be gone.
	_, ok = cache.GetContainerPin("stale-registry.example.com/other:v1")
	assert.False(t, ok, "stale non-firewall pin should be removed")

	// Current versions should still be present.
	pin, ok = cache.GetContainerPin("ghcr.io/github/gh-aw-firewall/agent:0.27.2")
	require.True(t, ok, "current pin should be kept")
	assert.Equal(t, "sha256:new", pin.Digest)

	pin, ok = cache.GetContainerPin("node:lts-alpine")
	require.True(t, ok, "node pin should be kept")
	assert.Equal(t, "sha256:node", pin.Digest)
}

// TestPruneStaleContainerPins_AllStale verifies that pruning with an empty known set
// removes all container pins.
func TestPruneStaleContainerPins_AllStale(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	cache.SetContainerPin("image-a:v1", "sha256:aaa", "image-a:v1@sha256:aaa")
	cache.SetContainerPin("image-b:v2", "sha256:bbb", "image-b:v2@sha256:bbb")

	pruned := cache.PruneStaleContainerPins(map[string]struct{}{})
	assert.Equal(t, 2, pruned, "all pins should be pruned when known set is empty")
	assert.Empty(t, cache.ContainerPins, "ContainerPins map should be empty after full prune")
}

// TestPruneStaleContainerPins_NoneStale verifies that pruning with a set matching all
// existing pins removes nothing.
func TestPruneStaleContainerPins_NoneStale(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	cache.SetContainerPin("node:lts-alpine", "sha256:abc", "node:lts-alpine@sha256:abc")

	// Mark cache as clean to verify dirty flag is not set when nothing changes.
	cache.dirty = false

	pruned := cache.PruneStaleContainerPins(map[string]struct{}{"node:lts-alpine": {}})
	assert.Equal(t, 0, pruned, "no pins should be pruned")
	assert.False(t, cache.dirty, "dirty flag should not be set when nothing was pruned")
}

// TestPruneStaleContainerPins_NilMap verifies that pruning a cache with a nil
// ContainerPins map returns 0 without panicking.
func TestPruneStaleContainerPins_NilMap(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	cache.ContainerPins = nil

	assert.NotPanics(t, func() {
		pruned := cache.PruneStaleContainerPins(map[string]struct{}{"any:image": {}})
		assert.Equal(t, 0, pruned, "nil map should return 0 pruned")
	})
}
