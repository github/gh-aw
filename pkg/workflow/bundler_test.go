//go:build !integration

package workflow

import (
	"testing"
)

// TestBundleJavaScriptFromSources tests bundling JavaScript from source map
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptFromSources(t *testing.T) {
	t.Skip("JavaScript bundling tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundleJavaScriptFromSourcesWithoutRequires tests bundling without requires
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptFromSourcesWithoutRequires(t *testing.T) {
	t.Skip("JavaScript bundling without requires tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestRemoveExports tests removing exports from JavaScript
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestRemoveExports(t *testing.T) {
	t.Skip("Remove exports tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundleJavaScriptFromSourcesWithMultipleRequires tests bundling with multiple requires
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptFromSourcesWithMultipleRequires(t *testing.T) {
	t.Skip("JavaScript bundling with multiple requires tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundleJavaScriptFromSourcesWithNestedPath tests bundling with nested paths
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptFromSourcesWithNestedPath(t *testing.T) {
	t.Skip("JavaScript bundling with nested paths tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestValidateNoLocalRequires tests validation that no local requires remain
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestValidateNoLocalRequires(t *testing.T) {
	t.Skip("Validate no local requires tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundleJavaScriptValidationSuccess tests successful validation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptValidationSuccess(t *testing.T) {
	t.Skip("JavaScript bundling validation success tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundleJavaScriptValidationFailure tests validation failure handling
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptValidationFailure(t *testing.T) {
	t.Skip("JavaScript bundling validation failure tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundleJavaScriptWithNpmPackages tests bundling with npm packages
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptWithNpmPackages(t *testing.T) {
	t.Skip("JavaScript bundling with npm packages tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestRemoveExportsMultiLine tests removing multi-line exports
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestRemoveExportsMultiLine(t *testing.T) {
	t.Skip("Remove multi-line exports tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestRemoveExportsConditional tests removing conditional exports
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestRemoveExportsConditional(t *testing.T) {
	t.Skip("Remove conditional exports tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestBundleJavaScriptMergesDestructuredImports tests merging destructured imports
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptMergesDestructuredImports(t *testing.T) {
	t.Skip("JavaScript bundling destructured imports tests skipped - scripts now use require() pattern to load external files at runtime")
}
