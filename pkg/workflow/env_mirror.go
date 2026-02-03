// This file provides environment variable mirroring for agent containers.
//
// This file contains logic for mirroring essential GitHub Actions runner environment
// variables into the agent container. The Ubuntu runner image provides many environment
// variables that workflows and actions depend on (e.g., JAVA_HOME, ANDROID_HOME,
// CHROMEWEBDRIVER, CONDA, etc.). This module ensures these are available inside
// the AWF (Agent Workflow Firewall) container.
//
// Environment variables are passed through using AWF's --env flag, which sets
// environment variables only if they exist on the host. This ensures graceful
// handling of missing variables.
//
// Reference: scratchpad/ubuntulatest.md section "Environment Variables"

package workflow

import (
	"sort"

	"github.com/github/gh-aw/pkg/logger"
)

var envMirrorLog = logger.New("workflow:env_mirror")

// MirroredEnvVars is the list of environment variables from the GitHub Actions
// Ubuntu runner that should be mirrored into the agent container.
//
// These are grouped by category:
// - Java JDK homes (for multiple Java versions)
// - Android SDK paths
// - Browser WebDriver paths
// - Package manager paths
// - Go workspace path
//
// Variables are only passed through if they exist on the host runner.
// Reference: scratchpad/ubuntulatest.md
var MirroredEnvVars = []string{
	// Java JDK homes (multiple versions available on Ubuntu runner)
	"JAVA_HOME",
	"JAVA_HOME_8_X64",
	"JAVA_HOME_11_X64",
	"JAVA_HOME_17_X64",
	"JAVA_HOME_21_X64",
	"JAVA_HOME_25_X64",

	// Android SDK paths
	"ANDROID_HOME",
	"ANDROID_SDK_ROOT",
	"ANDROID_NDK",
	"ANDROID_NDK_HOME",
	"ANDROID_NDK_ROOT",
	"ANDROID_NDK_LATEST_HOME",

	// Browser WebDriver paths (for Selenium/browser automation)
	"CHROMEWEBDRIVER",
	"EDGEWEBDRIVER",
	"GECKOWEBDRIVER",
	"SELENIUM_JAR_PATH",

	// Package manager paths
	"CONDA",
	"VCPKG_INSTALLATION_ROOT",

	// Go workspace path
	"GOPATH",

	// .NET environment
	"DOTNET_ROOT",

	// Python environment
	"PIPX_HOME",
	"PIPX_BIN_DIR",

	// Ruby environment
	"GEM_HOME",
	"GEM_PATH",

	// Rust environment
	"CARGO_HOME",
	"RUSTUP_HOME",

	// Homebrew (Linux)
	"HOMEBREW_PREFIX",
	"HOMEBREW_CELLAR",
	"HOMEBREW_REPOSITORY",

	// Swift
	"SWIFT_PATH",

	// Common tool homes
	"GOROOT",
	"NVM_DIR",

	// Azure environment
	"AZURE_EXTENSION_DIR",
}

// GetMirroredEnvArgs returns the AWF command-line arguments for mirroring
// environment variables from the runner into the agent container.
//
// AWF uses the --env flag to pass environment variables in KEY=VALUE format.
// The output uses shell variable expansion syntax (e.g., JAVA_HOME=${JAVA_HOME})
// so that the actual value is resolved at runtime from the host environment.
//
// Example output: ["--env", "JAVA_HOME=${JAVA_HOME}", "--env", "ANDROID_HOME=${ANDROID_HOME}", ...]
//
// This function always returns the same list of environment variables to mirror.
// Variables that don't exist on the host will expand to empty strings at runtime.
func GetMirroredEnvArgs() []string {
	envMirrorLog.Print("Generating mirrored environment variable arguments")

	// Sort for consistent output
	sortedVars := make([]string, len(MirroredEnvVars))
	copy(sortedVars, MirroredEnvVars)
	sort.Strings(sortedVars)

	var args []string
	for _, envVar := range sortedVars {
		// Use shell variable expansion syntax so the value is resolved at runtime
		// Pre-wrap in double quotes so shellEscapeArg preserves them (allowing shell expansion)
		args = append(args, "--env", "\""+envVar+"=${"+envVar+"}\"")
	}

	envMirrorLog.Printf("Generated %d environment variable mirror arguments", len(sortedVars))
	return args
}

// GetMirroredEnvVarsList returns the list of environment variables that
// are mirrored from the runner to the agent container.
//
// This is useful for documentation and debugging purposes.
func GetMirroredEnvVarsList() []string {
	result := make([]string, len(MirroredEnvVars))
	copy(result, MirroredEnvVars)
	sort.Strings(result)
	return result
}
