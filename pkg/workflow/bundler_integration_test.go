//go:build integration

package workflow

import (
	"testing"
)

// TestBundlerIntegration tests the integration of bundler with embedded scripts
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundlerIntegration(t *testing.T) {
	t.Skip("Bundler integration tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundlerCaching tests that bundling is cached and only happens once
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundlerCaching(t *testing.T) {
	t.Skip("Bundler caching tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundlerConcurrency tests that the bundler works correctly under concurrent access
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundlerConcurrency(t *testing.T) {
	t.Skip("Bundler concurrency tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundledScriptsContainHelperFunctions verifies that helper functions are properly bundled
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundledScriptsContainHelperFunctions(t *testing.T) {
	t.Skip("Bundled scripts helper function tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundledScriptsDoNotContainExports verifies that exports are removed from bundled scripts
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundledScriptsDoNotContainExports(t *testing.T) {
	t.Skip("Bundled scripts exports tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundledScriptsHaveCorrectStructure verifies the structure of bundled scripts
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundledScriptsHaveCorrectStructure(t *testing.T) {
	t.Skip("Bundled scripts structure tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestSourceFilesAreSmaller verifies that source files are smaller than bundled scripts
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestSourceFilesAreSmaller(t *testing.T) {
	t.Skip("Source file size comparison tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestGetJavaScriptSources verifies that GetJavaScriptSources returns all embedded sources
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestGetJavaScriptSources(t *testing.T) {
	t.Skip("JavaScript sources tests skipped - scripts now use require() pattern to load external files at runtime")
}
