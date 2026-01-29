//go:build integration

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestVersionIsSetDuringBuild verifies that when built with proper ldflags,
// the version is set to the actual version, not "dev"
func TestVersionIsSetDuringBuild(t *testing.T) {
	t.Run("version variable can be overridden at build time", func(t *testing.T) {

		// Build a test binary with a specific version
		testVersion := "v0.0.0-test"

		// Build the binary with version set via ldflags
		cmd := exec.Command("go", "build",
			"-ldflags", "-X main.version="+testVersion,
			"-o", "/tmp/gh-aw-test-version",
			".")
		cmd.Dir = "."

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to build test binary: %v\nOutput: %s", err, output)
		}

		// Run the test binary to check its version
		versionCmd := exec.Command("/tmp/gh-aw-test-version", "version")
		versionOutput, err := versionCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to run version command: %v", err)
		}

		outputStr := string(versionOutput)
		if !strings.Contains(outputStr, testVersion) {
			t.Errorf("Version output should contain '%s', got: %s", testVersion, outputStr)
		}

		if strings.Contains(outputStr, "dev") && !strings.Contains(testVersion, "dev") {
			t.Errorf("Version output should not contain 'dev' when built with custom version, got: %s", outputStr)
		}
	})

	t.Run("default version is dev", func(t *testing.T) {
		// The in-memory version variable should default to "dev"
		if version != "dev" {
			t.Logf("Note: version is '%s', expected 'dev' (this is okay if running in a release build)", version)
		}

		// This test documents that the default hardcoded value is "dev"
		// and it can be overridden at build time using ldflags
	})
}

// TestBuildReleaseScriptExists verifies the custom build script exists and is executable
func TestBuildReleaseScriptExists(t *testing.T) {
	t.Run("build script exists", func(t *testing.T) {
		cmd := exec.Command("test", "-f", "../../scripts/build-release.sh")
		if err := cmd.Run(); err != nil {
			t.Error("scripts/build-release.sh should exist")
		}
	})

	t.Run("build script is executable", func(t *testing.T) {
		cmd := exec.Command("test", "-x", "../../scripts/build-release.sh")
		if err := cmd.Run(); err != nil {
			t.Error("scripts/build-release.sh should be executable")
		}
	})

	t.Run("build script requires version argument", func(t *testing.T) {
		cmd := exec.Command("../../scripts/build-release.sh")
		err := cmd.Run()
		if err == nil {
			t.Error("build script should fail without version argument")
		}
	})
}
