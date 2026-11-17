//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

// TestMain provides setup and teardown for unit tests in the cli package
// Note: Integration tests have their own TestMain in compile_integration_test.go
func TestMain(m *testing.M) {
	// Get current working directory before tests run
	wd, err := os.Getwd()
	if err != nil {
		panic("Failed to get current working directory: " + err.Error())
	}
	runtimeDirectory, err := os.MkdirTemp("", "gh-aw-cli-test-runtime-")
	if err != nil {
		panic("Failed to create private test runtime: " + err.Error())
	}
	previousRuntimeDirectory, hadRuntimeDirectory := os.LookupEnv("GH_AW_RUNTIME_DIR")
	previousRuntimeID, hadRuntimeID := os.LookupEnv("GH_AW_RUNTIME_ID")
	_ = os.Setenv("GH_AW_RUNTIME_DIR", runtimeDirectory)
	_ = os.Setenv("GH_AW_RUNTIME_ID", "cli-test-runtime")

	goleak.VerifyTestMain(m, goleak.Cleanup(func(_ int) {
		if hadRuntimeDirectory {
			_ = os.Setenv("GH_AW_RUNTIME_DIR", previousRuntimeDirectory)
		} else {
			_ = os.Unsetenv("GH_AW_RUNTIME_DIR")
		}
		if hadRuntimeID {
			_ = os.Setenv("GH_AW_RUNTIME_ID", previousRuntimeID)
		} else {
			_ = os.Unsetenv("GH_AW_RUNTIME_ID")
		}
		_ = os.RemoveAll(runtimeDirectory)
		// Clean up any action cache files created during tests
		// Tests may create .github/aw/actions-lock.json in the pkg/cli directory
		actionCacheDir := filepath.Join(wd, ".github")
		if _, err := os.Stat(actionCacheDir); err == nil {
			_ = os.RemoveAll(actionCacheDir)
		}
	}))
}
