//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
)

func TestCacheSupport(t *testing.T) {
	// Test cache support in workflow compilation
	tests := []struct {
		name              string
		frontmatter       string
		expectedInLock    []string
		notExpectedInLock []string
	}{
		{
			name: "single cache configuration",
			frontmatter: `---
name: Test Cache Workflow
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
cache:
  key: node-modules-${{ hashFiles('package-lock.json') }}
  path: node_modules
  restore-keys: |
    node-modules-
tools:
  github:
    allowed: [get_file_contents]
---`,
			expectedInLock: []string{
				"# Cache configuration from frontmatter was processed and added to the main job steps",
				"# Cache configuration from frontmatter processed below",
				"- name: Cache",
				"uses: actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: node-modules-${{ hashFiles('package-lock.json') }}",
				"path: node_modules",
				"restore-keys: node-modules-",
			},
			notExpectedInLock: []string{
				// Match standalone "cache:" field (at line start) to avoid matching "package-manager-cache:"
				"\n  cache:",
				"\ncache:",
			},
		},
		{
			name: "multiple cache configurations",
			frontmatter: `---
name: Test Multi Cache Workflow
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
cache:
  - key: node-modules-${{ hashFiles('package-lock.json') }}
    path: node_modules
    restore-keys: |
      node-modules-
  - key: build-cache-${{ github.sha }}
    path: 
      - dist
      - .cache
    restore-keys:
      - build-cache-
    fail-on-cache-miss: false
tools:
  github:
    allowed: [get_file_contents]
---`,
			expectedInLock: []string{
				"# Cache configuration from frontmatter was processed and added to the main job steps",
				"# Cache configuration from frontmatter processed below",
				"- name: Cache (node-modules-${{ hashFiles('package-lock.json') }})",
				"- name: Cache (build-cache-${{ github.sha }})",
				"uses: actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: node-modules-${{ hashFiles('package-lock.json') }}",
				"key: build-cache-${{ github.sha }}",
				"path: node_modules",
				"path: |",
				"dist",
				".cache",
				"fail-on-cache-miss: false",
			},
			notExpectedInLock: []string{
				// Match standalone "cache:" field (at line start) to avoid matching "package-manager-cache:"
				"\n  cache:",
				"\ncache:",
			},
		},
		{
			name: "cache with all optional parameters",
			frontmatter: `---
name: Test Full Cache Workflow
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
cache:
  key: full-cache-${{ github.sha }}
  path: dist
  restore-keys:
    - cache-v1-
    - cache-
  upload-chunk-size: 32000000
  fail-on-cache-miss: true
  lookup-only: false
tools:
  github:
    allowed: [get_file_contents]
---`,
			expectedInLock: []string{
				"# Cache configuration from frontmatter processed below",
				"- name: Cache",
				"uses: actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: full-cache-${{ github.sha }}",
				"path: dist",
				"restore-keys: |",
				"cache-v1-",
				"cache-",
				"upload-chunk-size: 32000000",
				"fail-on-cache-miss: true",
				"lookup-only: false",
			},
			notExpectedInLock: []string{
				// Match standalone "cache:" field (at line start) to avoid matching "package-manager-cache:"
				"\n  cache:",
				"\ncache:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for test files
			tmpDir := testutil.TempDir(t, "test-*")

			// Create test workflow file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			testContent := tt.frontmatter + "\n\n# Test Cache Workflow\n\nThis is a test workflow.\n"
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			compiler := NewCompilerWithVersion("v1.0.0")
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Unexpected error compiling workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContent := string(content)

			// Check that expected strings are present
			for _, expected := range tt.expectedInLock {
				if !strings.Contains(lockContent, expected) {
					t.Errorf("Expected lock file to contain '%s' but it didn't.\nContent:\n%s", expected, lockContent)
				}
			}

			// Check that unexpected strings are NOT present in non-comment lines
			// (frontmatter is embedded as comments, so we need to exclude comment lines)
			for _, notExpected := range tt.notExpectedInLock {
				if containsInNonCommentLines(lockContent, notExpected) {
					t.Errorf("Lock file should NOT contain '%s' in non-comment lines but it did.\nContent:\n%s", notExpected, lockContent)
				}
			}
		})
	}
}

func TestDefaultPermissions(t *testing.T) {
	// Test that workflows without permissions in frontmatter get default permissions applied
	tmpDir := testutil.TempDir(t, "default-permissions-test")

	// Create a test workflow WITHOUT permissions specified in frontmatter
	testContent := `---
on:
  issues:
    types: [opened]
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
---

# Test Workflow

This workflow should get default permissions applied automatically.
`

	testFile := filepath.Join(tmpDir, "test-default-permissions.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Calculate the lock file path
	lockFile := stringutil.MarkdownToLockFile(testFile)

	// Read the generated lock file
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that default permissions are applied (contents:read for agent job in dev mode)
	// With the new behavior, workflows without explicit permissions get minimal job-level permissions
	expectedDefaultPermissions := []string{
		"contents: read", // Agent job gets contents:read in dev mode for local actions
	}

	for _, expectedPerm := range expectedDefaultPermissions {
		if !strings.Contains(lockContentStr, expectedPerm) {
			t.Errorf("Expected default permission '%s' not found in generated workflow.\nGenerated content:\n%s", expectedPerm, lockContentStr)
		}
	}

	// Verify that permissions section exists
	if !strings.Contains(lockContentStr, "permissions:") {
		t.Error("Expected 'permissions:' section not found in generated workflow")
	}

	// Parse the generated YAML to verify structure
	var workflow map[string]any
	if err := yaml.Unmarshal(lockContent, &workflow); err != nil {
		t.Fatalf("Failed to parse generated YAML: %v", err)
	}

	// Verify that jobs section exists
	jobs, exists := workflow["jobs"]
	if !exists {
		t.Fatal("Jobs section not found in parsed workflow")
	}

	jobsMap, ok := jobs.(map[string]any)
	if !ok {
		t.Fatal("Jobs section is not a map")
	}

	// Find the main job (should be the one with the workflow name converted to kebab-case)
	var mainJob map[string]any
	for jobName, job := range jobsMap {
		if jobName == "agent" { // The workflow name "Test Workflow" becomes "test-workflow"
			if jobMap, ok := job.(map[string]any); ok {
				mainJob = jobMap
				break
			}
		}
	}

	if mainJob == nil {
		t.Fatal("Main workflow job not found")
	}

	// Verify permissions section exists in the main job
	permissions, exists := mainJob["permissions"]
	if !exists {
		t.Fatal("Permissions section not found in main job")
	}

	// Verify permissions is a map with contents: read
	permissionsMap, ok := permissions.(map[string]any)
	if !ok {
		t.Fatalf("Permissions section is not a map, got type: %T", permissions)
	}
	contentsPermission, exists := permissionsMap["contents"]
	if !exists {
		t.Fatal("Contents permission not found in agent job")
	}
	if contentsPermission != "read" {
		t.Fatalf("Expected contents: read permission, got: %v", contentsPermission)
	}
}

func TestCustomPermissionsOverrideDefaults(t *testing.T) {
	// Test that custom permissions in frontmatter override default permissions
	tmpDir := testutil.TempDir(t, "custom-permissions-test")

	// Create a test workflow WITH custom permissions specified in frontmatter
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: write
  issues: write
tools:
  github:
    toolsets: [repos, issues]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow

This workflow has custom permissions that should override defaults.
`

	testFile := filepath.Join(tmpDir, "test-custom-permissions.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Calculate the lock file path
	lockFile := stringutil.MarkdownToLockFile(testFile)

	// Read the generated lock file
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	// Parse the generated YAML to verify structure
	var workflow map[string]any
	if err := yaml.Unmarshal(lockContent, &workflow); err != nil {
		t.Fatalf("Failed to parse generated YAML: %v", err)
	}

	// Verify that jobs section exists
	jobs, exists := workflow["jobs"]
	if !exists {
		t.Fatal("Jobs section not found in parsed workflow")
	}

	jobsMap, ok := jobs.(map[string]any)
	if !ok {
		t.Fatal("Jobs section is not a map")
	}

	// Find the main job (should be the one with the workflow name converted to kebab-case)
	var mainJob map[string]any
	for jobName, job := range jobsMap {
		if jobName == "agent" { // The workflow name "Test Workflow" becomes "test-workflow"
			if jobMap, ok := job.(map[string]any); ok {
				mainJob = jobMap
				break
			}
		}
	}

	if mainJob == nil {
		t.Fatal("Main workflow job not found")
	}

	// Verify permissions section exists in the main job
	permissions, exists := mainJob["permissions"]
	if !exists {
		t.Fatal("Permissions section not found in main job")
	}

	// Verify permissions is a map
	permissionsMap, ok := permissions.(map[string]any)
	if !ok {
		t.Fatal("Permissions section is not a map")
	}

	// Verify custom permissions are applied
	expectedCustomPermissions := map[string]string{
		"contents": "write",
		"issues":   "write",
	}

	for key, expectedValue := range expectedCustomPermissions {
		actualValue, exists := permissionsMap[key]
		if !exists {
			t.Errorf("Expected custom permission '%s' not found in permissions map", key)
			continue
		}
		if actualValue != expectedValue {
			t.Errorf("Expected permission '%s' to have value '%s', but got '%v'", key, expectedValue, actualValue)
		}
	}

	// Verify that default permissions that are not overridden are NOT present
	// since custom permissions completely replace defaults
	lockContentStr := string(lockContent)
	defaultOnlyPermissions := []string{
		"pull-requests: read",
		"discussions: read",
		"deployments: read",
		"actions: read",
		"checks: read",
		"statuses: read",
	}

	for _, defaultPerm := range defaultOnlyPermissions {
		if strings.Contains(lockContentStr, defaultPerm) {
			t.Errorf("Default permission '%s' should not be present when custom permissions are specified.\nGenerated content:\n%s", defaultPerm, lockContentStr)
		}
	}
}
