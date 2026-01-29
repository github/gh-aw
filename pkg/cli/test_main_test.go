//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain provides setup and teardown for unit tests in the cli package
// Note: Integration tests have their own TestMain in compile_integration_test.go
func TestMain(m *testing.M) {
	// Get current working directory before tests run
	wd, err := os.Getwd()
	if err != nil {
		panic("Failed to get current working directory: " + err.Error())
	}

	// Run all tests
	code := m.Run()

	// Clean up any action cache files created during tests
	// Tests may create .github/aw/actions-lock.json in the pkg/cli directory
	actionCacheDir := filepath.Join(wd, ".github")
	if _, err := os.Stat(actionCacheDir); err == nil {
		_ = os.RemoveAll(actionCacheDir)
	}

	os.Exit(code)
}
