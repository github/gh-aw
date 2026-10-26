//go:build !integration

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollectImagesFromLockFiles verifies that container image tags are correctly
// extracted from download_docker_images.sh invocations in lock files.
func TestCollectImagesFromLockFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		lockFileContent string
		expectedImages  []string
	}{
		{
			name: "single image in lock file",
			lockFileContent: `name: test
jobs:
  setup:
    steps:
      - name: Download container images
        run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" node:lts-alpine
`,
			expectedImages: []string{"node:lts-alpine"},
		},
		{
			name: "multiple images in lock file",
			lockFileContent: `name: test
jobs:
  setup:
    steps:
      - name: Download container images
        run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" ghcr.io/github/gh-aw-mcpg:v0.2.17 ghcr.io/github/github-mcp-server:v0.32.0 node:lts-alpine
`,
			expectedImages: []string{
				"ghcr.io/github/gh-aw-mcpg:v0.2.17",
				"ghcr.io/github/github-mcp-server:v0.32.0",
				"node:lts-alpine",
			},
		},
		{
			name: "digest pinned images normalized to base ref",
			lockFileContent: `name: test
jobs:
  setup:
    steps:
      - name: Download container images
        run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" ghcr.io/github/github-mcp-server:v0.32.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa node:lts-alpine@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`,
			expectedImages: []string{
				"ghcr.io/github/github-mcp-server:v0.32.0",
				"node:lts-alpine",
			},
		},
		{
			name: "no docker images in lock file",
			lockFileContent: `name: test
jobs:
  build:
    steps:
      - uses: actions/checkout@v5
`,
			expectedImages: []string{},
		},
		{
			name: "images deduplicated across multiple lock files",
			// This test sets up two lock files with overlapping images.
			// The collect function should deduplicate.
			lockFileContent: `name: first
jobs:
  setup:
    steps:
      - run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" node:lts-alpine
`,
			// Second lock file added via helper below.
			expectedImages: []string{"node:lts-alpine"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
			require.NoError(t, os.MkdirAll(workflowsDir, 0755))

			// Write the primary lock file.
			lockPath := filepath.Join(workflowsDir, "test.lock.yml")
			require.NoError(t, os.WriteFile(lockPath, []byte(tt.lockFileContent), 0644))

			// For the deduplication test, write a second lock file with the same image.
			if tt.name == "images deduplicated across multiple lock files" {
				second := `name: second
jobs:
  setup:
    steps:
      - run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" node:lts-alpine
`
				require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "second.lock.yml"), []byte(second), 0644))
			}

			images, err := collectImagesFromLockFiles(workflowsDir)
			require.NoError(t, err, "collectImagesFromLockFiles should not error")
			assert.Equal(t, tt.expectedImages, images, "collected images")
		})
	}
}

// TestCollectImagesFromLockFiles_MissingDir verifies that a non-existent workflow
// directory returns nil without error.
func TestCollectImagesFromLockFiles_MissingDir(t *testing.T) {
	t.Parallel()

	images, err := collectImagesFromLockFiles("/nonexistent/path/to/workflows")
	require.NoError(t, err, "missing dir should not return error")
	assert.Nil(t, images, "missing dir should return nil images")
}

// TestBuildxDigestPattern verifies that the regex correctly extracts the top-level
// "Digest:" line from docker buildx imagetools inspect text output.
func TestBuildxDigestPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		output         string
		expectedDigest string
		shouldMatch    bool
	}{
		{
			name: "standard OCI index output",
			output: `Name:      docker.io/mcp/brave-search:latest
MediaType: application/vnd.oci.image.index.v1+json
Digest:    sha256:f58a5c22c1196ec7bd1ca586ce216f2334fc298550ddcf652c0e8adb6d256d78

Manifests:
  Name:        docker.io/mcp/brave-search:latest@sha256:ae3b30d079370f67495d75085ffb73a11efcf9f9b23b919ffcb990ed2c076cfe
  MediaType:   application/vnd.oci.image.manifest.v1+json
  Platform:    linux/amd64
`,
			expectedDigest: "sha256:f58a5c22c1196ec7bd1ca586ce216f2334fc298550ddcf652c0e8adb6d256d78",
			shouldMatch:    true,
		},
		{
			name: "single-platform image",
			output: `Name:      ghcr.io/github/github-mcp-server:v0.32.0
MediaType: application/vnd.oci.image.manifest.v1+json
Digest:    sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1
`,
			expectedDigest: "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1",
			shouldMatch:    true,
		},
		{
			name: "picks top-level Digest not manifest sub-digest",
			output: `Name:      node:lts-alpine
Digest:    sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

Manifests:
  Name:        node:lts-alpine@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`,
			expectedDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			shouldMatch:    true,
		},
		{
			name:        "no digest in output",
			output:      "Name:      unknown\nMediaType: unknown\n",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := buildxDigestPattern.FindSubmatch([]byte(tt.output))
			if tt.shouldMatch {
				require.NotNil(t, matches, "expected pattern to match")
				assert.Equal(t, tt.expectedDigest, string(matches[1]), "extracted digest")
			} else {
				assert.Nil(t, matches, "expected pattern not to match")
			}
		})
	}
}

// TestUpdateContainerPins_PinnedLockFilesPreserveContainerPins verifies that when
// lock files already contain digest-pinned image references (image:tag@sha256:...),
// the existing container pins in actions-lock.json are NOT pruned.  This is the
// regression test for the bug where gh aw update wiped out all container pins
// because collectImagesFromLockFiles returned digest-suffixed keys that did not
// match the base-tag keys used in the container pins map.
//
// The test calls UpdateContainerPins end-to-end: because all images in the lock
// file are already digest-pinned, Docker is never invoked.
func TestUpdateContainerPins_PinnedLockFilesPreserveContainerPins(t *testing.T) {
	tmpDir := t.TempDir()

	// actions-lock.json has an existing container pin using the base image tag as key.
	actionsLockDir := filepath.Join(tmpDir, ".github", "aw")
	require.NoError(t, os.MkdirAll(actionsLockDir, 0755))
	actionsLockContent := `{
  "entries": {},
  "containers": {
    "ghcr.io/github/gh-aw-firewall/agent:0.27.9": {
      "image": "ghcr.io/github/gh-aw-firewall/agent:0.27.9",
      "digest": "sha256:13f522853a688bfe24b04adbbe40b68101e8ef4b6fe0b636068527141bf1c269",
      "pinned_image": "ghcr.io/github/gh-aw-firewall/agent:0.27.9@sha256:13f522853a688bfe24b04adbbe40b68101e8ef4b6fe0b636068527141bf1c269"
    }
  }
}
`
	actionsLockPath := filepath.Join(actionsLockDir, "actions-lock.json")
	require.NoError(t, os.WriteFile(actionsLockPath, []byte(actionsLockContent), 0644))

	// The compiled lock file already embeds the digest-pinned reference (image:tag@sha256:...).
	// This is the real-world case after a prior successful gh aw update run.
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	lockFileContent := `name: test
jobs:
  setup:
    steps:
      - name: Download container images
        run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" ghcr.io/github/gh-aw-firewall/agent:0.27.9@sha256:13f522853a688bfe24b04adbbe40b68101e8ef4b6fe0b636068527141bf1c269
`
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "my-workflow.lock.yml"), []byte(lockFileContent), 0644))

	// UpdateContainerPins uses "." as the repo root for the action cache, so we
	// chdir into tmpDir before calling it and restore the original directory after.
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir) //nolint:errcheck
	require.NoError(t, os.Chdir(tmpDir))

	// Call UpdateContainerPins end-to-end. Because the lock file image is already
	// digest-pinned (@sha256:...), Docker is never invoked. The function should
	// prune zero pins (the bug caused it to prune all of them) and return false
	// (no new pins were added).
	added, err := UpdateContainerPins(context.Background(), workflowsDir, false)
	require.NoError(t, err)
	assert.False(t, added, "no new pins should be added when all images are already pinned")

	// Reload the cache from disk and confirm the original pin is still present.
	cache := workflow.NewActionCache(tmpDir)
	require.NoError(t, cache.Load())

	pin, ok := cache.GetContainerPin("ghcr.io/github/gh-aw-firewall/agent:0.27.9")
	require.True(t, ok, "container pin should still be present after UpdateContainerPins")
	assert.Equal(t, "sha256:13f522853a688bfe24b04adbbe40b68101e8ef4b6fe0b636068527141bf1c269", pin.Digest)

	// Verify the on-disk lock file is unchanged.
	data, err := os.ReadFile(actionsLockPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "ghcr.io/github/gh-aw-firewall/agent:0.27.9", "container pin should still be in actions-lock.json")
}

// TestUpdateContainerPins_PrunesStaleEntries verifies that UpdateContainerPins
// removes container pin entries from actions-lock.json that are no longer
// referenced in the compiled lock files (e.g. a superseded image version).
//
// Note: gh-aw-firewall images are intentionally exempt from this pruning (see
// TestUpdateContainerPins_KeepsStaleFirewallEntries below and gh-aw#51248), so
// this test uses a generic non-firewall image family to exercise the normal
// pruning path.
func TestUpdateContainerPins_PrunesStaleEntries(t *testing.T) {
	t.Parallel()

	// Create a temp directory acting as the repo root.
	tmpDir := t.TempDir()

	// Write an actions-lock.json with a stale container pin and a live one.
	// The live pin (v2) should be kept; the stale one (v1) should be pruned.
	actionsLockDir := filepath.Join(tmpDir, ".github", "aw")
	require.NoError(t, os.MkdirAll(actionsLockDir, 0755))
	actionsLockContent := `{
  "entries": {},
  "containers": {
    "ghcr.io/example/tool:v1": {
      "image": "ghcr.io/example/tool:v1",
      "digest": "sha256:olddigest0000000000000000000000000000000000000000000000000000000",
      "pinned_image": "ghcr.io/example/tool:v1@sha256:olddigest0000000000000000000000000000000000000000000000000000000"
    },
    "ghcr.io/example/tool:v2": {
      "image": "ghcr.io/example/tool:v2",
      "digest": "sha256:newdigest0000000000000000000000000000000000000000000000000000000",
      "pinned_image": "ghcr.io/example/tool:v2@sha256:newdigest0000000000000000000000000000000000000000000000000000000"
    }
  }
}
`
	actionsLockPath := filepath.Join(actionsLockDir, "actions-lock.json")
	require.NoError(t, os.WriteFile(actionsLockPath, []byte(actionsLockContent), 0644))

	// Write a lock file referencing the NEW version (v2), not the old one.
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	lockFileContent := `name: test
jobs:
  setup:
    steps:
      - name: Download container images
        run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" ghcr.io/example/tool:v2
`
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "my-workflow.lock.yml"), []byte(lockFileContent), 0644))

	// collectImagesFromLockFiles should find the new version only.
	images, err := collectImagesFromLockFiles(workflowsDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"ghcr.io/example/tool:v2"}, images)

	// Load the cache and exercise PruneStaleContainerPins directly (Docker is not
	// available in unit tests so we can't call the full UpdateContainerPins function).
	cache := workflow.NewActionCache(tmpDir)
	require.NoError(t, cache.Load())

	imageSet := map[string]struct{}{"ghcr.io/example/tool:v2": {}}
	pruned := cache.PruneStaleContainerPins(imageSet)
	assert.Equal(t, 1, pruned, "stale v1 entry should be pruned")

	_, ok := cache.GetContainerPin("ghcr.io/example/tool:v1")
	assert.False(t, ok, "old-version pin should not be in cache after prune")

	pin, ok := cache.GetContainerPin("ghcr.io/example/tool:v2")
	require.True(t, ok, "current-version pin should still be in cache")
	assert.Equal(t, "sha256:newdigest0000000000000000000000000000000000000000000000000000000", pin.Digest)

	// Save and verify the stale entry is gone from disk.
	require.NoError(t, cache.Save())

	data, err := os.ReadFile(actionsLockPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "\"ghcr.io/example/tool:v1\"", "stale version should not appear in saved file")
	assert.Contains(t, string(data), "ghcr.io/example/tool:v2", "current version should be in saved file")
}

// TestUpdateContainerPins_KeepsStaleFirewallEntries verifies that
// gh-aw-firewall container pins are retained across pruning even when no
// longer referenced by any local lock file (e.g. after bumping
// constants.DefaultFirewallVersion). This prevents the recurring regression
// where a previously-shipped AWF version silently loses its embedded digest
// pin (gh-aw#38561 / #43307 / #44040 / #51248).
func TestUpdateContainerPins_KeepsStaleFirewallEntries(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	actionsLockDir := filepath.Join(tmpDir, ".github", "aw")
	require.NoError(t, os.MkdirAll(actionsLockDir, 0755))
	actionsLockContent := `{
  "entries": {},
  "containers": {
    "ghcr.io/github/gh-aw-firewall/agent:0.27.0": {
      "image": "ghcr.io/github/gh-aw-firewall/agent:0.27.0",
      "digest": "sha256:olddigest0000000000000000000000000000000000000000000000000000000",
      "pinned_image": "ghcr.io/github/gh-aw-firewall/agent:0.27.0@sha256:olddigest0000000000000000000000000000000000000000000000000000000"
    },
    "ghcr.io/github/gh-aw-firewall/agent:0.27.2": {
      "image": "ghcr.io/github/gh-aw-firewall/agent:0.27.2",
      "digest": "sha256:newdigest0000000000000000000000000000000000000000000000000000000",
      "pinned_image": "ghcr.io/github/gh-aw-firewall/agent:0.27.2@sha256:newdigest0000000000000000000000000000000000000000000000000000000"
    }
  }
}
`
	actionsLockPath := filepath.Join(actionsLockDir, "actions-lock.json")
	require.NoError(t, os.WriteFile(actionsLockPath, []byte(actionsLockContent), 0644))

	// Write a lock file referencing only the NEW AWF version (0.27.2).
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	lockFileContent := `name: test
jobs:
  setup:
    steps:
      - name: Download container images
        run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" ghcr.io/github/gh-aw-firewall/agent:0.27.2
`
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "my-workflow.lock.yml"), []byte(lockFileContent), 0644))

	cache := workflow.NewActionCache(tmpDir)
	require.NoError(t, cache.Load())

	imageSet := map[string]struct{}{"ghcr.io/github/gh-aw-firewall/agent:0.27.2": {}}
	pruned := cache.PruneStaleContainerPins(imageSet)
	assert.Equal(t, 0, pruned, "gh-aw-firewall pins must never be pruned")

	pin, ok := cache.GetContainerPin("ghcr.io/github/gh-aw-firewall/agent:0.27.0")
	require.True(t, ok, "stale AWF version pin must be retained")
	assert.Equal(t, "sha256:olddigest0000000000000000000000000000000000000000000000000000000", pin.Digest)

	pin, ok = cache.GetContainerPin("ghcr.io/github/gh-aw-firewall/agent:0.27.2")
	require.True(t, ok, "current AWF version pin should still be in cache")
	assert.Equal(t, "sha256:newdigest0000000000000000000000000000000000000000000000000000000", pin.Digest)

	require.NoError(t, cache.Save())
	data, err := os.ReadFile(actionsLockPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "0.27.0", "stale AWF version must still appear in saved file")
	assert.Contains(t, string(data), "0.27.2", "current AWF version must still appear in saved file")
}

func TestUpdateContainerPins_RefreshExistingPin(t *testing.T) {
	tmpDir := t.TempDir()

	actionsLockDir := filepath.Join(tmpDir, ".github", "aw")
	require.NoError(t, os.MkdirAll(actionsLockDir, 0755))
	actionsLockContent := `{
  "entries": {},
  "containers": {
    "ghcr.io/github/github-mcp-server:v0.32.0": {
      "image": "ghcr.io/github/github-mcp-server:v0.32.0",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
      "pinned_image": "ghcr.io/github/github-mcp-server:v0.32.0@sha256:1111111111111111111111111111111111111111111111111111111111111111"
    }
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(actionsLockDir, "actions-lock.json"), []byte(actionsLockContent), 0644))

	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	lockFileContent := `name: test
jobs:
  setup:
    steps:
      - name: Download container images
        run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" ghcr.io/github/github-mcp-server:v0.32.0@sha256:1111111111111111111111111111111111111111111111111111111111111111
`
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "test.lock.yml"), []byte(lockFileContent), 0644))

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir) //nolint:errcheck
	require.NoError(t, os.Chdir(tmpDir))

	deps := defaultContainerPinUpdateDeps()
	const refreshedDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	deps.fetchDigest = func(_ context.Context, image string, verbose bool) (string, error) {
		require.Equal(t, "ghcr.io/github/github-mcp-server:v0.32.0", image)
		return refreshedDigest, nil
	}

	changed, err := updateContainerPins(context.Background(), deps, workflowsDir, false, containerPinUpdateOptions{refreshExisting: true})
	require.NoError(t, err)
	assert.True(t, changed, "refreshing an existing pin should report a change")

	cache := workflow.NewActionCache(tmpDir)
	require.NoError(t, cache.Load())
	pin, ok := cache.GetContainerPin("ghcr.io/github/github-mcp-server:v0.32.0")
	require.True(t, ok)
	assert.Equal(t, refreshedDigest, pin.Digest)
	assert.Equal(t, "ghcr.io/github/github-mcp-server:v0.32.0@"+refreshedDigest, pin.PinnedImage)
}

func TestUpdateContainerPins_FailOnResolveErrorsOption(t *testing.T) {
	tmpDir := t.TempDir()

	actionsLockDir := filepath.Join(tmpDir, ".github", "aw")
	require.NoError(t, os.MkdirAll(actionsLockDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(actionsLockDir, "actions-lock.json"), []byte(`{"entries":{},"containers":{}}`), 0644))

	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	lockFileContent := `name: test
jobs:
  setup:
    steps:
      - name: Download container images
        run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" ghcr.io/github/github-mcp-server:v0.32.0
`
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "test.lock.yml"), []byte(lockFileContent), 0644))

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir) //nolint:errcheck
	require.NoError(t, os.Chdir(tmpDir))

	deps := defaultContainerPinUpdateDeps()
	deps.fetchDigest = func(_ context.Context, image string, verbose bool) (string, error) {
		return "", fmt.Errorf("resolution failed for %s", image)
	}

	t.Run("non-fatal by default", func(t *testing.T) {
		changed, err := updateContainerPins(context.Background(), deps, workflowsDir, false, containerPinUpdateOptions{})
		require.NoError(t, err)
		assert.False(t, changed)
	})

	t.Run("fatal when enabled", func(t *testing.T) {
		changed, err := updateContainerPins(context.Background(), deps, workflowsDir, false, containerPinUpdateOptions{failOnResolveErrors: true})
		require.Error(t, err)
		assert.False(t, changed)
		assert.Contains(t, err.Error(), "failed to resolve digest for 1 image(s)")
	})
}

func TestUpdateContainerPins_FailureSummary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		errors []error
		reason string
	}{
		{
			name: "repeated registry denial",
			errors: []error{
				&containerDigestResolutionError{reason: "registry access denied (HTTP 403)", details: "long buildx diagnostics"},
				&containerDigestResolutionError{reason: "registry access denied (HTTP 403)", details: "missing crane diagnostics"},
			},
			reason: "registry access denied (HTTP 403)",
		},
		{
			name: "mixed causes",
			errors: []error{
				&containerDigestResolutionError{reason: "registry access denied (HTTP 403)"},
				&containerDigestResolutionError{reason: "registry metadata lookup failed"},
			},
			reason: "registry access denied (HTTP 403); registry metadata lookup failed",
		},
		{
			name:   "unknown errors",
			errors: []error{errors.New("unexpected first failure"), errors.New("unexpected second failure")},
			reason: "registry metadata lookup failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Chdir(tmpDir)
			workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
			require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "test.lock.yml"),
				[]byte("run: bash download_docker_images.sh ghcr.io/example/agent:v1 ghcr.io/example/proxy:v1\n"), 0o644))
			cache := workflow.NewActionCache(".")
			digest := "sha256:" + strings.Repeat("a", 64)
			cache.SetContainerPin("ghcr.io/example/agent:v1", digest, "ghcr.io/example/agent:v1@"+digest)
			require.NoError(t, cache.Save())
			deps := containerPinUpdateDeps{
				fetchDigest: func(_ context.Context, image string, _ bool) (string, error) {
					if image == "ghcr.io/example/agent:v1" {
						return "", tc.errors[0]
					}
					return "", tc.errors[1]
				},
			}
			var expected string
			output := captureUpdateOrgStderr(t, func() {
				expected = console.FormatInfoMessage("Container pins could not be updated: " + tc.reason + ".")
				changed, err := updateContainerPins(context.Background(), deps, workflowsDir, false, containerPinUpdateOptions{refreshExisting: true})
				require.NoError(t, err)
				require.False(t, changed)
			})
			assert.Equal(t, strings.TrimSpace(expected), containerPinNormalOutput(output), "emit exactly one informational line without per-image diagnostics")
			cache = workflow.NewActionCache(".")
			require.NoError(t, cache.Load())
			pin, ok := cache.GetContainerPin("ghcr.io/example/agent:v1")
			require.True(t, ok, "failed refresh must preserve the existing pin")
			assert.Equal(t, digest, pin.Digest)
		})
	}
}

func TestUpdateContainerPins_Interrupted(t *testing.T) {
	for _, interruption := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, fatal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/fatal=%v", interruption, fatal), func(t *testing.T) {
				t.Chdir(t.TempDir())
				require.NoError(t, os.MkdirAll(".github/workflows", 0o755))
				require.NoError(t, os.WriteFile(".github/workflows/test.lock.yml",
					[]byte("run: bash download_docker_images.sh ghcr.io/example/agent:v1\n"), 0o644))
				deps := containerPinUpdateDeps{
					fetchDigest: func(context.Context, string, bool) (string, error) {
						return "", fmt.Errorf("interrupted lookup: %w", interruption)
					},
				}
				output := captureUpdateOrgStderr(t, func() {
					changed, err := updateContainerPins(context.Background(), deps, ".github/workflows", false,
						containerPinUpdateOptions{failOnResolveErrors: fatal})
					require.ErrorIs(t, err, interruption)
					require.False(t, changed)
				})
				assert.Empty(t, containerPinNormalOutput(output), "interruption must not emit a registry failure summary")
			})
		}
	}
}

func containerPinNormalOutput(output string) string {
	var lines []string
	for line := range strings.SplitSeq(output, "\n") {
		if !isCompileDebugLine(stringutil.StripANSI(line)) {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
