//go:build !integration

package parser

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMain sets up the test environment with the correct compiler version
func TestMain(m *testing.M) {
	// Get the current git version (matching what the build system uses)
	version := getGitVersion()
	if version != "" {
		SetCompilerVersion(version)
		// Log the version being used for debugging
		os.Stderr.WriteString("Test: Using compiler version: " + version + "\n")
	} else {
		os.Stderr.WriteString("Test: WARNING - Could not determine git version, using default\n")
	}

	// Run tests
	code := m.Run()
	os.Exit(code)
}

// getGitVersion gets the git version using the same logic as the build system
func getGitVersion() string {
	// Try to run: git describe --always --dirty
	cmd := exec.Command("git", "describe", "--always", "--dirty")
	output, err := cmd.Output()
	if err != nil {
		// Fall back to "dev" if git command fails
		return "dev"
	}

	version := strings.TrimSpace(string(output))

	// Strip the -dirty suffix for test consistency
	// The lock files are compiled with a clean version, but tests may run
	// with uncommitted changes (the lock files themselves)
	version = strings.TrimSuffix(version, "-dirty")

	return version
}
