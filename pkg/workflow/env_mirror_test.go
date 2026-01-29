//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMirroredEnvArgs(t *testing.T) {
	args := GetMirroredEnvArgs()

	// Should return pairs of --env and KEY=${KEY} format
	require.NotEmpty(t, args, "Should return environment variable arguments")
	require.Equal(t, 0, len(args)%2, "Arguments should come in pairs (--env, KEY=${KEY})")

	// Verify the structure of arguments
	for i := 0; i < len(args); i += 2 {
		assert.Equal(t, "--env", args[i], "Even indices should be --env flag")
		assert.NotEmpty(t, args[i+1], "Odd indices should be environment variable assignments")
		// Verify the "KEY=${KEY}" format with outer double quotes
		assert.True(t, len(args[i+1]) >= 2 && args[i+1][0] == '"' && args[i+1][len(args[i+1])-1] == '"',
			"Should be wrapped in double quotes for shell expansion, got: %s", args[i+1])
		assert.Contains(t, args[i+1], "=", "Should contain = for KEY=VALUE format")
		assert.Contains(t, args[i+1], "=${", "Should contain =${ for shell expansion")
		assert.Contains(t, args[i+1], "}", "Should contain } for shell expansion")
	}
}

func TestGetMirroredEnvArgs_ContainsExpectedVariables(t *testing.T) {
	args := GetMirroredEnvArgs()

	// Convert to a set for easy lookup (extract variable name from "KEY=${KEY}" format)
	varSet := make(map[string]bool)
	for i := 1; i < len(args); i += 2 {
		// Extract the variable name from "KEY=${KEY}" format
		envAssignment := args[i]
		// Skip the leading quote and get the part before the '='
		if len(envAssignment) > 1 && envAssignment[0] == '"' {
			for j := 1; j < len(envAssignment); j++ {
				if envAssignment[j] == '=' {
					varSet[envAssignment[1:j]] = true
					break
				}
			}
		}
	}

	// Test that critical environment variables are included
	expectedVars := []string{
		"JAVA_HOME",
		"JAVA_HOME_17_X64",
		"ANDROID_HOME",
		"CHROMEWEBDRIVER",
		"GECKOWEBDRIVER",
		"CONDA",
		"VCPKG_INSTALLATION_ROOT",
		"GOPATH",
	}

	for _, expected := range expectedVars {
		assert.True(t, varSet[expected], "Should include %s in mirrored environment variables", expected)
	}
}

func TestGetMirroredEnvArgs_IsSorted(t *testing.T) {
	args := GetMirroredEnvArgs()

	// Extract just the variable names from "KEY=${KEY}" format (odd indices)
	var varNames []string
	for i := 1; i < len(args); i += 2 {
		envAssignment := args[i]
		// Skip the leading quote and get the part before the '='
		if len(envAssignment) > 1 && envAssignment[0] == '"' {
			for j := 1; j < len(envAssignment); j++ {
				if envAssignment[j] == '=' {
					varNames = append(varNames, envAssignment[1:j])
					break
				}
			}
		}
	}

	// Verify they are sorted
	for i := 1; i < len(varNames); i++ {
		assert.LessOrEqual(t, varNames[i-1], varNames[i],
			"Environment variables should be sorted, but %s comes after %s",
			varNames[i-1], varNames[i])
	}
}

func TestGetMirroredEnvVarsList(t *testing.T) {
	vars := GetMirroredEnvVarsList()

	require.NotEmpty(t, vars, "Should return a list of environment variables")

	// Verify the list contains expected variables
	varSet := make(map[string]bool)
	for _, v := range vars {
		varSet[v] = true
	}

	assert.True(t, varSet["JAVA_HOME"], "Should include JAVA_HOME")
	assert.True(t, varSet["ANDROID_HOME"], "Should include ANDROID_HOME")
	assert.True(t, varSet["CHROMEWEBDRIVER"], "Should include CHROMEWEBDRIVER")
}

func TestGetMirroredEnvVarsList_IsSorted(t *testing.T) {
	vars := GetMirroredEnvVarsList()

	// Verify they are sorted
	for i := 1; i < len(vars); i++ {
		assert.LessOrEqual(t, vars[i-1], vars[i],
			"Environment variables should be sorted, but %s comes after %s",
			vars[i-1], vars[i])
	}
}

func TestMirroredEnvVars_NoDuplicates(t *testing.T) {
	vars := GetMirroredEnvVarsList()

	seen := make(map[string]bool)
	for _, v := range vars {
		assert.False(t, seen[v], "Duplicate environment variable found: %s", v)
		seen[v] = true
	}
}

func TestMirroredEnvVars_IncludesJavaVersions(t *testing.T) {
	vars := GetMirroredEnvVarsList()

	varSet := make(map[string]bool)
	for _, v := range vars {
		varSet[v] = true
	}

	// Java versions commonly available on GitHub Actions runners
	javaVersions := []string{
		"JAVA_HOME_8_X64",
		"JAVA_HOME_11_X64",
		"JAVA_HOME_17_X64",
		"JAVA_HOME_21_X64",
	}

	for _, javaVar := range javaVersions {
		assert.True(t, varSet[javaVar], "Should include %s for Java version support", javaVar)
	}
}

func TestMirroredEnvVars_IncludesAndroidVars(t *testing.T) {
	vars := GetMirroredEnvVarsList()

	varSet := make(map[string]bool)
	for _, v := range vars {
		varSet[v] = true
	}

	// Android environment variables from the runner
	androidVars := []string{
		"ANDROID_HOME",
		"ANDROID_SDK_ROOT",
		"ANDROID_NDK",
		"ANDROID_NDK_HOME",
	}

	for _, androidVar := range androidVars {
		assert.True(t, varSet[androidVar], "Should include %s for Android development support", androidVar)
	}
}

func TestMirroredEnvVars_IncludesBrowserVars(t *testing.T) {
	vars := GetMirroredEnvVarsList()

	varSet := make(map[string]bool)
	for _, v := range vars {
		varSet[v] = true
	}

	// Browser/WebDriver environment variables from the runner
	browserVars := []string{
		"CHROMEWEBDRIVER",
		"EDGEWEBDRIVER",
		"GECKOWEBDRIVER",
		"SELENIUM_JAR_PATH",
	}

	for _, browserVar := range browserVars {
		assert.True(t, varSet[browserVar], "Should include %s for browser automation support", browserVar)
	}
}

func TestGetMirroredEnvArgs_CorrectFormat(t *testing.T) {
	args := GetMirroredEnvArgs()

	// Find ANDROID_HOME in the args and verify its format
	found := false
	for i := 0; i < len(args); i += 2 {
		if args[i] == "--env" && i+1 < len(args) {
			// Check for the specific format: "KEY=${KEY}" with outer double quotes
			if args[i+1] == "\"ANDROID_HOME=${ANDROID_HOME}\"" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "Should include \"ANDROID_HOME=${ANDROID_HOME}\" in correct format with outer double quotes")

	// Also verify JAVA_HOME format
	foundJava := false
	for i := 0; i < len(args); i += 2 {
		if args[i] == "--env" && i+1 < len(args) {
			if args[i+1] == "\"JAVA_HOME=${JAVA_HOME}\"" {
				foundJava = true
				break
			}
		}
	}
	assert.True(t, foundJava, "Should include \"JAVA_HOME=${JAVA_HOME}\" in correct format with outer double quotes")
}
