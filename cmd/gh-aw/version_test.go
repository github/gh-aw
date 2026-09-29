//go:build integration

package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestVersionIsSetDuringBuild verifies that when built with proper ldflags,
// the version is set to the actual version, not "dev"
func TestVersionIsSetDuringBuild(t *testing.T) {
	t.Parallel()
	t.Run("version variable can be overridden at build time", func(t *testing.T) {
		t.Parallel()

		// Build a test binary with a specific version
		testVersion := "v0.0.0-test"

		binaryName := "gh-aw-test-version"
		if runtime.GOOS == "windows" {
			binaryName += ".exe"
		}
		binaryPath := filepath.Join(t.TempDir(), binaryName)

		// Build the binary with version set via ldflags
		cmd := exec.Command("go", "build",
			"-ldflags", "-X main.version="+testVersion,
			"-o", binaryPath,
			".")
		cmd.Dir = "."

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to build test binary: %v\nOutput: %s", err, output)
		}

		for _, args := range [][]string{{"version"}, {"--version"}} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				var stderr bytes.Buffer
				versionCmd := exec.Command(binaryPath, args...)
				versionCmd.Stderr = &stderr
				versionOutput, err := versionCmd.Output()
				if err != nil {
					t.Fatalf("Failed to run version command: %v\nStderr: %s", err, stderr.String())
				}

				outputStr := string(versionOutput)
				if !strings.Contains(outputStr, testVersion) {
					t.Errorf("Version output should contain '%s', got: %s", testVersion, outputStr)
				}

				if strings.Contains(outputStr, "dev") && !strings.Contains(testVersion, "dev") {
					t.Errorf("Version output should not contain 'dev' when built with custom version, got: %s", outputStr)
				}

				if stderr.Len() != 0 {
					t.Errorf("Version output should not be written to stderr, got: %s", stderr.String())
				}
			})
		}
	})

	t.Run("default version is dev", func(t *testing.T) {
		t.Parallel()
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
	t.Parallel()
	t.Run("build script exists", func(t *testing.T) {
		t.Parallel()
		cmd := exec.Command("test", "-f", "../../scripts/build-release.sh")
		if err := cmd.Run(); err != nil {
			t.Error("scripts/build-release.sh should exist")
		}
	})

	t.Run("build script is executable", func(t *testing.T) {
		t.Parallel()
		cmd := exec.Command("test", "-x", "../../scripts/build-release.sh")
		if err := cmd.Run(); err != nil {
			t.Error("scripts/build-release.sh should be executable")
		}
	})

	t.Run("build script requires version argument", func(t *testing.T) {
		t.Parallel()
		cmd := exec.Command("../../scripts/build-release.sh")
		err := cmd.Run()
		if err == nil {
			t.Error("build script should fail without version argument")
		}
	})
}
