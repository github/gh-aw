//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCJSFilesNoActionsRequires verifies that production .cjs files in actions/setup/js
// do not use require() statements with "actions/" paths or "@actions/*" npm packages.
//
// When these .cjs files are deployed to GitHub Actions runners, they are copied
// to /opt/gh-aw/actions/ as a flat directory structure. Any require() statements
// that reference "actions/..." paths or "@actions/*" npm packages would fail because:
// 1. There's no parent "actions/" directory in the runtime environment
// 2. All files are in the same flat directory
// 3. The @actions/* npm packages are not installed in the runtime environment
//
// Note: Test files (*.test.cjs, test-*.cjs) are excluded from this validation as they
// are not deployed to GitHub Actions runners and may use @actions/* packages for testing.
//
// Valid requires:
//   - require("./file.cjs") - relative paths within the same directory
//   - require("fs") - built-in Node.js modules
//   - require("path") - other built-in Node.js modules
//
// Invalid requires:
//   - require("actions/setup/js/file.cjs") - absolute path to actions directory
//   - require("../../actions/setup/js/file.cjs") - relative path up to actions directory
//   - require("@actions/core") - npm packages from @actions/* are not available at runtime
//   - require("@actions/github") - npm packages from @actions/* are not available at runtime
func TestCJSFilesNoActionsRequires(t *testing.T) {
	// Find the repository root
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}

	cjsDir := filepath.Join(repoRoot, "actions", "setup", "js")

	// Get all .cjs files (excluding test files)
	entries, err := os.ReadDir(cjsDir)
	if err != nil {
		t.Fatalf("Failed to read directory %s: %v", cjsDir, err)
	}

	var cjsFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Include .cjs files but exclude test files
		// Test files (*.test.cjs, test-*.cjs) are not deployed to GitHub Actions runners
		// and may use @actions/* packages for testing purposes
		if strings.HasSuffix(name, ".cjs") &&
			!strings.HasSuffix(name, ".test.cjs") &&
			!strings.HasPrefix(name, "test-") {
			cjsFiles = append(cjsFiles, name)
		}
	}

	if len(cjsFiles) == 0 {
		t.Skip("No .cjs files found to test")
	}

	t.Logf("Checking %d .cjs files for invalid require statements", len(cjsFiles))

	// Pattern to match require statements with "actions/" paths
	// Matches: require("actions/...") or require('actions/...')
	actionsRequirePattern := regexp.MustCompile(`require\s*\(\s*["']actions/[^"']+["']\s*\)`)

	// Pattern to match relative paths going up to actions directory
	// Matches: require("../../actions/...") or similar patterns
	relativeActionsPattern := regexp.MustCompile(`require\s*\(\s*["']\.\./.+/actions/[^"']+["']\s*\)`)

	// Pattern to match require statements with @actions/* npm packages
	// Matches: require("@actions/core"), require('@actions/github'), etc.
	npmActionsPattern := regexp.MustCompile(`require\s*\(\s*["']@actions/[^"']+["']\s*\)`)

	var failedFiles []string
	var violations []string

	// Exception: safe_output_unified_handler_manager.cjs is allowed to require @actions/github
	// because the package is installed at runtime via setup.sh when safe-output-projects flag is enabled
	allowedNpmActionsRequires := map[string][]string{
		"safe_output_unified_handler_manager.cjs": {"@actions/github"},
	}

	for _, filename := range cjsFiles {
		filepath := filepath.Join(cjsDir, filename)
		content, err := os.ReadFile(filepath)
		if err != nil {
			t.Errorf("Failed to read %s: %v", filename, err)
			continue
		}

		code := string(content)

		// Check for "actions/" absolute path requires
		actionsMatches := actionsRequirePattern.FindAllString(code, -1)
		if len(actionsMatches) > 0 {
			for _, match := range actionsMatches {
				violation := filename + ": " + match
				violations = append(violations, violation)
				t.Errorf("Invalid require in %s: %s", filename, match)
			}
			if !sliceContainsString(failedFiles, filename) {
				failedFiles = append(failedFiles, filename)
			}
		}

		// Check for relative paths going up to actions directory
		relativeMatches := relativeActionsPattern.FindAllString(code, -1)
		if len(relativeMatches) > 0 {
			for _, match := range relativeMatches {
				violation := filename + ": " + match
				violations = append(violations, violation)
				t.Errorf("Invalid require in %s: %s", filename, match)
			}
			if !sliceContainsString(failedFiles, filename) {
				failedFiles = append(failedFiles, filename)
			}
		}

		// Check for @actions/* npm package requires (with exceptions)
		npmMatches := npmActionsPattern.FindAllString(code, -1)
		if len(npmMatches) > 0 {
			for _, match := range npmMatches {
				// Check if this file/package combination is allowed
				isAllowed := false
				if allowedPackages, ok := allowedNpmActionsRequires[filename]; ok {
					for _, allowedPkg := range allowedPackages {
						if strings.Contains(match, allowedPkg) {
							isAllowed = true
							t.Logf("Allowed @actions/* require in %s: %s (package installed at runtime)", filename, match)
							break
						}
					}
				}

				if !isAllowed {
					violation := filename + ": " + match
					violations = append(violations, violation)
					t.Errorf("Invalid require in %s: %s", filename, match)
					if !sliceContainsString(failedFiles, filename) {
						failedFiles = append(failedFiles, filename)
					}
				}
			}
		}
	}

	if len(failedFiles) > 0 {
		t.Errorf("\nFound %d file(s) with invalid require statements:", len(failedFiles))
		for _, file := range failedFiles {
			t.Errorf("  - %s", file)
		}
		t.Error("\nInvalid require patterns detected:")
		for _, violation := range violations {
			t.Errorf("  - %s", violation)
		}
		t.Error("\nWhen .cjs files are deployed to GitHub Actions runners, they are copied")
		t.Error("to /opt/gh-aw/actions/ as a flat directory. Any require() statements that")
		t.Error("reference 'actions/...' paths or '@actions/*' npm packages will fail at runtime")
		t.Error("because:")
		t.Error("  1. The parent 'actions/' directory structure doesn't exist")
		t.Error("  2. The @actions/* npm packages are not installed in the runtime environment")
		t.Error("\nUse relative requires instead:")
		t.Error("  ✓ require('./file.cjs')     - relative path in same directory")
		t.Error("  ✓ require('fs')             - built-in Node.js module")
		t.Error("  ✓ require('path')           - built-in Node.js module")
		t.Error("\nDo not use:")
		t.Error("  ✗ require('actions/setup/js/file.cjs')     - absolute path to actions directory")
		t.Error("  ✗ require('../../actions/setup/js/file.cjs') - relative path up to actions directory")
		t.Error("  ✗ require('@actions/core')                 - @actions/* npm packages not available at runtime")
		t.Error("  ✗ require('@actions/github')               - @actions/* npm packages not available at runtime")
	} else {
		t.Logf("✓ All %d .cjs files use valid require statements", len(cjsFiles))
	}
}

// sliceContainsString checks if a string slice contains a specific string
func sliceContainsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}
