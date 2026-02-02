//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestMaximumPatchSizeEnvironmentVariable(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "patch-size-test")

	tests := []struct {
		name                 string
		frontmatterContent   string
		expectedConfigValue  string // Changed from expectedEnvValue - now in config JSON
		shouldContainPushJob bool
		shouldContainPRJob   bool
	}{
		{
			name: "default patch size (no config)",
			frontmatterContent: `---
on: push
safe-outputs:
  push-to-pull-request-branch: null
  create-pull-request: null
---

# Test Workflow

This workflow tests default patch size configuration.`,
			expectedConfigValue:  `\"max_patch_size\":1024`, // Now in handler config JSON (escaped in YAML)
			shouldContainPushJob: true,
			shouldContainPRJob:   true,
		},
		{
			name: "custom patch size 512 KB",
			frontmatterContent: `---
on: push
safe-outputs:
  max-patch-size: 512
  push-to-pull-request-branch: null
  create-pull-request: null
---

# Test Workflow

This workflow tests custom 512KB patch size configuration.`,
			expectedConfigValue:  `\"max_patch_size\":512`, // Now in handler config JSON (escaped in YAML)
			shouldContainPushJob: true,
			shouldContainPRJob:   true,
		},
		{
			name: "custom patch size 2MB",
			frontmatterContent: `---
on: push
safe-outputs:
  max-patch-size: 2048
  create-pull-request: null
---

# Test Workflow

This workflow tests custom 2MB patch size configuration.`,
			expectedConfigValue:  `\"max_patch_size\":2048`, // Now in handler config JSON (escaped in YAML)
			shouldContainPushJob: false,
			shouldContainPRJob:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create markdown file
			mdFile := filepath.Join(tmpDir, tt.name+".md")
			err := os.WriteFile(mdFile, []byte(tt.frontmatterContent), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Compile workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(mdFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Determine expected lock file name
			lockFile := stringutil.MarkdownToLockFile(mdFile)

			// Read lock file content
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockContentStr := string(lockContent)

			// Check that the safe_outputs job is generated (consolidated mode)
			if tt.shouldContainPushJob || tt.shouldContainPRJob {
				if !strings.Contains(lockContentStr, "safe_outputs:") {
					t.Errorf("Expected safe_outputs job to be generated")
				}
				// For config JSON, check with flexible spacing (accounting for escaped quotes in YAML)
				expectedFound := strings.Contains(lockContentStr, tt.expectedConfigValue) ||
					strings.Contains(lockContentStr, strings.ReplaceAll(tt.expectedConfigValue, ":", ": "))
				if !expectedFound {
					t.Errorf("Expected '%s' to be found in handler config, got:\n%s", tt.expectedConfigValue, lockContentStr)
				}
			}

			// Cleanup
			if err := os.Remove(lockFile); err != nil {
				t.Logf("Warning: Failed to remove lock file: %v", err)
			}
		})
	}
}

func TestPatchSizeWithInvalidValues(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "patch-size-invalid-test")

	tests := []struct {
		name                   string
		frontmatterContent     string
		expectedValue          string // The value to look for (env var or config JSON)
		isHandlerManagerConfig bool   // If true, look in config JSON; if false, look for env var
	}{
		{
			name: "very small patch size should work",
			frontmatterContent: `---
on: push
safe-outputs:
  max-patch-size: 1
  push-to-pull-request-branch: null
---

# Test Workflow

This workflow tests very small patch size configuration.`,
			expectedValue:          `\"max_patch_size\":1`, // Config JSON for handler manager (escaped in YAML)
			isHandlerManagerConfig: true,
		},
		{
			name: "large valid patch size should work",
			frontmatterContent: `---
on: push
safe-outputs:
  max-patch-size: 10240
  create-pull-request: null
---

# Test Workflow

This workflow tests large valid patch size configuration.`,
			expectedValue:          `\"max_patch_size\":10240`, // Config JSON for handler manager (escaped in YAML)
			isHandlerManagerConfig: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create markdown file
			mdFile := filepath.Join(tmpDir, tt.name+".md")
			err := os.WriteFile(mdFile, []byte(tt.frontmatterContent), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Compile workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(mdFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Determine expected lock file name
			lockFile := stringutil.MarkdownToLockFile(mdFile)

			// Read lock file content
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockContentStr := string(lockContent)

			// Check that the value is in the right format (env var or config JSON)
			// For config JSON, we need to check with flexible spacing (accounting for escaped quotes in YAML)
			expectedFound := false
			if tt.isHandlerManagerConfig {
				// Check both with and without spaces after colons
				expectedFound = strings.Contains(lockContentStr, tt.expectedValue) ||
					strings.Contains(lockContentStr, strings.ReplaceAll(tt.expectedValue, ":", ": "))
			} else {
				expectedFound = strings.Contains(lockContentStr, tt.expectedValue)
			}

			if !expectedFound {
				context := "environment variable"
				if tt.isHandlerManagerConfig {
					context = "handler config JSON"
				}
				t.Errorf("Expected '%s' to be found in %s, got:\n%s", tt.expectedValue, context, lockContentStr)
			}

			// Cleanup
			if err := os.Remove(lockFile); err != nil {
				t.Logf("Warning: Failed to remove lock file: %v", err)
			}
		})
	}
}
