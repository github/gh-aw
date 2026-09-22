package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RequireGhAwBinary locates the built gh-aw binary used by integration tests
// and skips the calling test if it cannot be found.
//
// The binary is resolved relative to the repository root, which is located by
// walking up from the current working directory in search of go.mod. This
// avoids the brittleness of hardcoding a relative path (such as
// "../../gh-aw"), which breaks if a test file moves to a different package
// depth, and honors runtime.GOOS to append ".exe" on Windows.
//
// Returns the absolute path to the binary. If the repository root cannot be
// located or the binary does not exist, the test is skipped with a message
// instructing the caller to run `make build` first.
func RequireGhAwBinary(t *testing.T) string {
	t.Helper()

	binaryName := "gh-aw"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("Skipping test: unable to locate repository root: %v", err)
	}

	binaryPath := filepath.Join(repoRoot, binaryName)
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	return binaryPath
}

// findRepoRoot walks up from the current working directory looking for
// go.mod, which marks the root of the gh-aw module.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s upward", dir)
		}
		dir = parent
	}
}
