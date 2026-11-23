//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
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
	// Compiler tests must not depend on live release metadata or the current
	// age of default engine versions.
	workflow.SetEngineReleaseTimeLookupForTesting(func(string, string) (time.Time, error) {
		return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), nil
	})

	goleak.VerifyTestMain(m, goleak.Cleanup(func(_ int) {
		// Clean up any action cache files created during tests
		// Tests may create .github/aw/actions-lock.json in the pkg/cli directory
		actionCacheDir := filepath.Join(wd, ".github")
		if _, err := os.Stat(actionCacheDir); err == nil {
			_ = os.RemoveAll(actionCacheDir)
		}
	}))
}
