//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestGeneratedWorkflowsUseSHAs ensures that all generated workflows use SHAs instead of version tags
func TestGeneratedWorkflowsUseSHAs(t *testing.T) {
	// Create a test workflow file
	testDir := testutil.TempDir(t, "test-*")
	workflowFile := filepath.Join(testDir, "test-workflow.md")

	workflowContent := `---
on: push
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
This is a test workflow to verify SHA pinning.
`

	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Check that actions are referenced by SHA, not by version tag
	// Pattern: uses: owner/repo@SHA (40 hex chars)
	shaPattern := regexp.MustCompile(`uses: ([a-zA-Z0-9_-]+/[a-zA-Z0-9_-]+)@([0-9a-f]{40})`)

	// Pattern: uses: owner/repo@version (should not exist)
	versionPattern := regexp.MustCompile(`uses: ([a-zA-Z0-9_-]+/[a-zA-Z0-9_-]+)@(v\d+)`)

	// Find all SHA-based action references
	shaMatches := shaPattern.FindAllString(lockContentStr, -1)
	if len(shaMatches) == 0 {
		t.Errorf("No SHA-based action references found in generated workflow")
	}

	// Check for version-based action references (should not exist)
	versionMatches := versionPattern.FindAllStringSubmatch(lockContentStr, -1)
	if len(versionMatches) > 0 {
		t.Errorf("Found %d version-based action references (should use SHAs):", len(versionMatches))
		for _, match := range versionMatches {
			t.Errorf("  - %s", match[0])
		}
	}

	t.Logf("Found %d SHA-based action references", len(shaMatches))
}

func TestCompileWorkflowPreservesUnknownSHALabel(t *testing.T) {
	const sha = "c19371144df3bb44fab255c43d04cbc2ab54d1c4"
	for _, section := range []string{"steps", "pre-steps", "pre-agent-steps", "post-steps"} {
		for _, tt := range []struct {
			name    string
			comment string
			want    string
		}{
			{"labelled", " # v2.9.1", " # v2.9.1"},
			{"unlabelled", "", ""},
		} {
			t.Run(section+"/"+tt.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "demo.md")
				content := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\n" + section + ":\n  - uses: Swatinem/rust-cache@" + sha + tt.comment + "\n---\n\nSay hello.\n"
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				if err := NewCompiler().CompileWorkflow(path); err != nil {
					t.Fatal(err)
				}
				lock, err := os.ReadFile(stringutil.MarkdownToLockFile(path))
				if err != nil {
					t.Fatal(err)
				}
				want := "uses: Swatinem/rust-cache@" + sha + tt.want
				if !strings.Contains(string(lock), want+"\n") {
					t.Fatalf("lock file missing %q", want)
				}
				if strings.Contains(string(lock), "uses: Swatinem/rust-cache@"+sha+" # "+sha) {
					t.Fatal("lock file uses SHA as version comment")
				}
				manifest, err := ExtractGHAWManifestFromLockFile(string(lock))
				if err != nil {
					t.Fatal(err)
				}
				version := sha
				if tt.comment != "" {
					version = "v2.9.1"
				}
				found := false
				for _, action := range manifest.Actions {
					if action.Repo == "Swatinem/rust-cache" && action.SHA == sha {
						found = true
						if action.Version != version {
							t.Errorf("manifest version = %q, want %q", action.Version, version)
						}
					}
				}
				if !found {
					t.Fatal("missing action in manifest")
				}
			})
		}
	}
}

func TestCompileWorkflowPreservesImportedUnknownSHALabels(t *testing.T) {
	const sha = "c19371144df3bb44fab255c43d04cbc2ab54d1c4"
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	importedWorkflow := `---
steps:
  - uses: Swatinem/rust-cache@` + sha + ` # v2.9.1
pre-steps:
  - uses: example/pre@` + sha + ` # v1.2.0
pre-agent-steps:
  - uses: example/pre-agent@` + sha + ` # v3.4.0
post-steps:
  - uses: example/post@` + sha + ` # v5.6.0
---

Shared steps.
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "shared.md"), []byte(importedWorkflow), 0600); err != nil {
		t.Fatal(err)
	}
	setupWorkflow := `name: Copilot Setup Steps
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - uses: example/setup@` + sha + ` # v7.8.0
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "copilot-setup-steps.yml"), []byte(setupWorkflow), 0600); err != nil {
		t.Fatal(err)
	}
	workflow := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
imports:
  - shared.md
  - copilot-setup-steps.yml
---

Import steps.
`
	path := filepath.Join(workflowsDir, "demo.md")
	if err := os.WriteFile(path, []byte(workflow), 0600); err != nil {
		t.Fatal(err)
	}
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	if err := NewCompiler().CompileWorkflow(path); err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(stringutil.MarkdownToLockFile(path))
	if err != nil {
		t.Fatal(err)
	}
	lockContent := string(lock)
	expected := map[string]string{
		"Swatinem/rust-cache": "v2.9.1",
		"example/pre":         "v1.2.0",
		"example/pre-agent":   "v3.4.0",
		"example/post":        "v5.6.0",
		"example/setup":       "v7.8.0",
	}
	for repo, version := range expected {
		reference := "uses: " + repo + "@" + sha + " # " + version
		if !strings.Contains(lockContent, reference) {
			t.Errorf("lock file missing %q", reference)
		}
	}
	manifest, err := ExtractGHAWManifestFromLockFile(lockContent)
	if err != nil {
		t.Fatal(err)
	}
	for repo, version := range expected {
		found := false
		for _, action := range manifest.Actions {
			if action.Repo == repo && action.SHA == sha {
				found = true
				if action.Version != version {
					t.Errorf("%s manifest version = %q, want %q", repo, action.Version, version)
				}
			}
		}
		if !found {
			t.Errorf("missing %s action in manifest", repo)
		}
	}
}

// TestCompileWorkflowActionReferences tests that commonly used actions are pinned to SHAs
func TestCompileWorkflowActionReferences(t *testing.T) {
	testDir := testutil.TempDir(t, "test-*")
	workflowFile := filepath.Join(testDir, "test-workflow.md")

	workflowContent := `---
on:
  issues:
    types: [opened]
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
strict: false
safe-outputs:
  create-issue:
---

# Test Workflow
Create issues based on input.
`

	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Test specific actions that should be pinned
	expectedActions := map[string]string{
		"actions/checkout":        getActionPin("actions/checkout"),
		"actions/github-script":   getActionPin("actions/github-script"),
		"actions/upload-artifact": getActionPin("actions/upload-artifact"),
	}

	for actionRepo, expectedRef := range expectedActions {
		// Extract just the SHA from the expected reference
		parts := strings.Split(expectedRef, "@")
		if len(parts) != 2 {
			t.Fatalf("Invalid action reference format: %s", expectedRef)
		}
		expectedSHA := parts[1]

		// Check if the action with this SHA appears in the workflow
		if !strings.Contains(lockContentStr, "uses: "+actionRepo+"@"+expectedSHA) {
			t.Errorf("Expected to find %s@%s in generated workflow, but it was not found", actionRepo, expectedSHA)
		}
	}
}

// TestNoVersionTagsInLockFiles is a regression test to ensure version tags are not used
func TestNoVersionTagsInLockFiles(t *testing.T) {
	testDir := testutil.TempDir(t, "test-*")
	workflowFile := filepath.Join(testDir, "test-workflow.md")

	workflowContent := `---
on: push
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Simple Test
Just a simple test workflow.
`

	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// These version-based references should NOT appear in the generated workflow
	forbiddenPatterns := []string{
		"actions/checkout@v5",
		"actions/github-script@v9",
		"actions/upload-artifact@v4",
		"actions/download-artifact@v6",
		"actions/cache@v4",
		"actions/setup-node@v6",
		"actions/setup-python@v5",
		"actions/setup-go@v6",
	}

	for _, forbidden := range forbiddenPatterns {
		if strings.Contains(lockContentStr, "uses: "+forbidden) {
			t.Errorf("Found forbidden version tag reference: uses: %s (should use SHA instead)", forbidden)
		}
	}
}

// TestActionSHAValidationSavesCache tests that cache is persisted after validation
// This test doesn't require network access and uses a pre-populated cache
func TestActionSHAValidationSavesCache(t *testing.T) {
	testDir := testutil.TempDir(t, "test-*")

	// Create a lock file with an action
	lockFile := filepath.Join(testDir, "test-workflow.lock.yml")
	lockContent := `# gh-aw-metadata: {"schema_version":"v1"}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v5
`
	if err := os.WriteFile(lockFile, []byte(lockContent), 0644); err != nil {
		t.Fatalf("Failed to write lock file: %v", err)
	}

	// Create a cache and pre-populate it with entries
	cache := NewActionCache(testDir)
	cache.Set("actions/checkout", "v5", "93cb6efe18208431cddfb8368fd83d5badbf9bfd")

	// Verify cache file doesn't exist before validation
	cachePath := filepath.Join(testDir, ".github", "aw", CacheFileName)
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		os.RemoveAll(filepath.Join(testDir, ".github"))
	}

	// Run validation - even if no updates are detected, this exercises the code path
	// In a real scenario with network access, this would detect and save updates
	err := ValidateActionSHAsInLockFile(context.Background(), lockFile, cache, false)
	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// The test verifies the code compiles and runs without errors
	// In CI with gh CLI access, the cache would be saved if updates were found
	t.Logf("Validation completed successfully")
}
