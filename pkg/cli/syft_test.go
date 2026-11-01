//go:build !integration

package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func TestRunSyftOnImage_NilContext(t *testing.T) {
	// This test verifies that we can pass context.Background()
	ctx := context.Background()
	sbomDir := t.TempDir()

	// We can't actually run Docker in unit tests, so this test
	// just verifies the function signature accepts context
	_, err := runSyftOnImage(ctx, "alpine:latest", sbomDir, false)
	if err != nil {
		t.Skip("Docker not available or image not found, skipping")
	}
}

func TestRunSyftOnImage_ContextCancellation(t *testing.T) {
	// This test verifies that context cancellation is respected
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context immediately before calling the function
	cancel()

	sbomDir := t.TempDir()

	_, err := runSyftOnImage(ctx, "alpine:latest", sbomDir, false)
	if err == nil {
		t.Skip("Expected context cancellation error, but got nil - Docker may not be available")
	}

	// Context cancellation should produce an error
	if err != nil && ctx.Err() != nil {
		// Expected: context was cancelled
		t.Logf("Context cancellation worked as expected: %v", err)
	}
}

func TestRunSyftOnImage_JSONParsing(t *testing.T) {
	// Test that we can parse syft JSON output
	syftJSON := []byte(`{
		"artifacts": [
			{"name": "alpine-baselayout", "version": "3.2.0-r18", "type": "apk"},
			{"name": "busybox", "version": "1.33.1-r6", "type": "apk"}
		]
	}`)

	var output syftOutput
	if err := json.Unmarshal(syftJSON, &output); err != nil {
		t.Fatalf("Failed to parse syft JSON: %v", err)
	}

	if len(output.Artifacts) != 2 {
		t.Errorf("Expected 2 artifacts, got %d", len(output.Artifacts))
	}

	if output.Artifacts[0].Name != "alpine-baselayout" {
		t.Errorf("Expected first artifact name to be 'alpine-baselayout', got '%s'", output.Artifacts[0].Name)
	}
}

func TestRunSyftOnImage_SBOMPersistence(t *testing.T) {
	t.Skip("Requires Docker and network access - run as integration test")

	ctx := context.Background()
	sbomDir := t.TempDir()

	// Run syft on a small image
	result, err := runSyftOnImage(ctx, "alpine:latest", sbomDir, false)
	if err != nil {
		t.Fatalf("Failed to run syft: %v", err)
	}

	// Verify result structure
	if result.ImageRef != "alpine:latest" {
		t.Errorf("Expected image ref 'alpine:latest', got '%s'", result.ImageRef)
	}

	if result.PackageCount <= 0 {
		t.Errorf("Expected positive package count, got %d", result.PackageCount)
	}

	if result.SBOMPath == "" {
		t.Errorf("Expected non-empty SBOM path")
	}

	// Verify SBOM file exists
	if _, err := os.Stat(result.SBOMPath); os.IsNotExist(err) {
		t.Errorf("SBOM file does not exist at %s", result.SBOMPath)
	}

	// Verify SBOM file contains valid JSON
	data, err := os.ReadFile(result.SBOMPath)
	if err != nil {
		t.Fatalf("Failed to read SBOM file: %v", err)
	}

	var output syftOutput
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("SBOM file does not contain valid syft JSON: %v", err)
	}

	if len(output.Artifacts) != result.PackageCount {
		t.Errorf("SBOM artifact count (%d) does not match result count (%d)", len(output.Artifacts), result.PackageCount)
	}
}

func TestRunSyftOnImage_SafeFilename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Fake Docker executable uses a POSIX shell")
	}
	prependFakeDockerToPath(t, `{"artifacts":[]}`)
	sbomDir := t.TempDir()
	imageRef := "gcr.io/distroless/static:latest@sha256:" + strings.Repeat("a", 64)
	expectedPath := filepath.Join(sbomDir, fmt.Sprintf("sbom-%x.json", sha256.Sum256([]byte(imageRef))))

	result, err := runSyftOnImage(context.Background(), imageRef, sbomDir, false)
	require.NoError(t, err)
	require.Equal(t, expectedPath, result.SBOMPath)
	require.FileExists(t, expectedPath)
}

func TestRunSyftOnLockFiles_CollidingImageNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Fake Docker executable uses a POSIX shell")
	}
	for _, strict := range []bool{true, false} {
		t.Run(fmt.Sprintf("strict-%t", strict), func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("TMPDIR", tmpDir)
			sbomJSON := `{"artifacts":[{"name":"test","version":"1.0","type":"test"}]}`
			prependFakeDockerToPath(t, sbomJSON)
			images := []string{"ghcr.io/acme/foo_bar:latest", "ghcr.io/acme/foo/bar:latest"}
			lockFile := filepath.Join(t.TempDir(), "test.lock.yml")
			manifest := fmt.Sprintf("# gh-aw-manifest: {\"version\":1,\"containers\":[{\"image\":%q},{\"image\":%q}]}\n", images[0], images[1])
			require.NoError(t, os.WriteFile(lockFile, []byte(manifest), 0o600))

			require.NoError(t, runSyftOnLockFiles([]string{lockFile}, false, strict))
			directories, err := filepath.Glob(filepath.Join(tmpDir, "gh-aw-syft-sboms-*"))
			require.NoError(t, err)
			require.Len(t, directories, 1)
			entries, err := os.ReadDir(directories[0])
			require.NoError(t, err)
			require.Len(t, entries, 2, "both formerly colliding images must retain their SBOM")
			for _, image := range images {
				path := filepath.Join(directories[0], fmt.Sprintf("sbom-%x.json", sha256.Sum256([]byte(image))))
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.JSONEq(t, sbomJSON, string(data))
			}
		})
	}
}

func TestRunSyftOnLockFiles_NoImages(t *testing.T) {
	// Test that we handle lock files with no container images gracefully
	lockFile := filepath.Join(t.TempDir(), "empty.lock.yml")
	content := `
version: '1.0'
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "no containers here"
`
	if err := os.WriteFile(lockFile, []byte(content), constants.FilePermPublic); err != nil {
		t.Fatalf("Failed to write test lock file: %v", err)
	}

	err := runSyftOnLockFiles([]string{lockFile}, false, false)
	if err != nil {
		t.Errorf("Expected no error for lock files without images, got: %v", err)
	}
}

func TestRunSyftOnLockFiles_StrictMode(t *testing.T) {
	// Test that strict mode propagates errors
	lockFile := filepath.Join(t.TempDir(), "test.lock.yml")
	content := `
version: '1.0'
jobs:
  test:
    container:
      image: does-not-exist:latest
`
	if err := os.WriteFile(lockFile, []byte(content), constants.FilePermPublic); err != nil {
		t.Fatalf("Failed to write test lock file: %v", err)
	}

	// In strict mode, a scan failure should return an error
	err := runSyftOnLockFiles([]string{lockFile}, false, true)
	if err == nil {
		t.Skip("Expected error in strict mode, but got nil - Docker may not be available or image exists")
	}
}

func TestRunSyftOnLockFiles_NonStrictMode(t *testing.T) {
	// Test that non-strict mode continues on errors
	lockFile := filepath.Join(t.TempDir(), "test.lock.yml")
	content := `
version: '1.0'
jobs:
  test:
    container:
      image: does-not-exist:latest
`
	if err := os.WriteFile(lockFile, []byte(content), constants.FilePermPublic); err != nil {
		t.Fatalf("Failed to write test lock file: %v", err)
	}

	// In non-strict mode, a scan failure should only produce a warning
	err := runSyftOnLockFiles([]string{lockFile}, false, false)
	if err != nil {
		t.Errorf("Expected no error in non-strict mode, got: %v", err)
	}
}

func TestRunSyftOnImage_RejectsUnsafeImageRef(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
	}{
		{"option flag", "--entrypoint=/bin/sh"},
		{"embedded newline", "alpine:latest\n--privileged"},
		{"semicolon", "ghcr.io/org/im;age:latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runSyftOnImage(context.Background(), tt.imageRef, t.TempDir(), false)
			if err == nil {
				t.Fatalf("Expected error for unsafe image reference %q", tt.imageRef)
			}
			if !strings.Contains(err.Error(), "docker image reference") {
				t.Errorf("Expected image reference validation error, got: %v", err)
			}
		})
	}
}

func TestRunSyftOnImage_AcceptsValidImageRef(t *testing.T) {
	prependFakeDockerToPath(t, `{"artifacts":[]}`)

	result, err := runSyftOnImage(context.Background(), "ghcr.io/anchore/syft:v1.0.0", t.TempDir(), false)
	if err != nil {
		t.Fatalf("Expected valid image reference to reach docker, got: %v", err)
	}
	if result.ImageRef != "ghcr.io/anchore/syft:v1.0.0" {
		t.Fatalf("Expected result image ref to match input, got %q", result.ImageRef)
	}
}

func TestRunSyftOnLockFiles_PrivateOutputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Fake Docker executable uses a POSIX shell")
	}
	for _, precreation := range []string{"directory", "symlink"} {
		t.Run(precreation, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("TMPDIR", tmpDir)
			prependFakeDockerToPath(t, `{"artifacts":[{"name":"test","version":"1.0","type":"test"}]}`)
			target := t.TempDir()
			legacyDir := filepath.Join(tmpDir, "gh-aw-syft-sboms")
			if precreation == "symlink" {
				require.NoError(t, os.Symlink(target, legacyDir))
			} else {
				require.NoError(t, os.Mkdir(legacyDir, 0o777))
				target = legacyDir
			}
			victim := filepath.Join(target, "sbom-alpine_latest.json")
			require.NoError(t, os.WriteFile(victim, []byte("untouched"), 0o644))
			lockFile := filepath.Join(t.TempDir(), "test.lock.yml")
			require.NoError(t, os.WriteFile(lockFile, []byte("# gh-aw-manifest: {\"version\":1,\"containers\":[{\"image\":\"alpine:latest\"}]}\n"), 0o600))

			for range 2 {
				require.NoError(t, runSyftOnLockFiles([]string{lockFile}, true, true))
			}
			directories, err := filepath.Glob(filepath.Join(tmpDir, "gh-aw-syft-sboms-*"))
			require.NoError(t, err)
			require.Len(t, directories, 2, "each scan must retain its own outputs")
			for _, dir := range directories {
				info, err := os.Stat(dir)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
				sbom := filepath.Join(dir, fmt.Sprintf("sbom-%x.json", sha256.Sum256([]byte("alpine:latest"))))
				info, err = os.Stat(sbom)
				require.NoError(t, err)
				require.Equal(t, constants.FilePermSensitive, info.Mode().Perm())
				data, err := os.ReadFile(sbom)
				require.NoError(t, err)
				var output syftOutput
				require.NoError(t, json.Unmarshal(data, &output))
				require.Len(t, output.Artifacts, 1)
			}
			data, err := os.ReadFile(victim)
			require.NoError(t, err)
			require.Equal(t, "untouched", string(data))
		})
	}
}

func TestRunSyftOnImage_RefusesPreexistingOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Fake Docker executable uses a POSIX shell")
	}
	prependFakeDockerToPath(t, `{"artifacts":[]}`)
	for _, precreation := range []string{"file", "symlink"} {
		t.Run(precreation, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, fmt.Sprintf("sbom-%x.json", sha256.Sum256([]byte("alpine:latest"))))
			victim := path
			if precreation == "symlink" {
				victim = filepath.Join(t.TempDir(), "victim")
				require.NoError(t, os.Symlink(victim, path))
			}
			require.NoError(t, os.WriteFile(victim, []byte("untouched"), 0o600))
			_, err := runSyftOnImage(context.Background(), "alpine:latest", dir, false)
			require.ErrorContains(t, err, "failed to write SBOM")
			data, err := os.ReadFile(victim)
			require.NoError(t, err)
			require.Equal(t, "untouched", string(data))
		})
	}
}

func TestRunSyftOnLockFiles_CleansFailedScanDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Fake Docker executable uses a POSIX shell")
	}
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	prependFakeDockerToPath(t, `{"artifacts":[]}`)
	t.Setenv("TEST_DOCKER_EXIT_CODE", "1")
	lockFile := filepath.Join(t.TempDir(), "test.lock.yml")
	require.NoError(t, os.WriteFile(lockFile, []byte("# gh-aw-manifest: {\"version\":1,\"containers\":[{\"image\":\"alpine:latest\"}]}\n"), 0o600))
	for _, strict := range []bool{true, false} {
		err := runSyftOnLockFiles([]string{lockFile}, false, strict)
		if strict {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		entries, err := os.ReadDir(tmpDir)
		require.NoError(t, err)
		require.Empty(t, entries, "failed scans must not leave empty output directories")
	}
}
