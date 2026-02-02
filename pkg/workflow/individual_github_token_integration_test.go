//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestIndividualGitHubTokenIntegration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "individual-github-token-test")

	t.Run("create-issue uses safe-outputs global github-token", func(t *testing.T) {
		testContent := `---
name: Test Global GitHub Token for Issues
on:
  issues:
    types: [opened]
engine: claude
safe-outputs:
  github-token: ${{ secrets.GLOBAL_PAT }}
  create-issue:
    title-prefix: "[AUTO] "
---

# Test Global GitHub Token for Issues

This workflow tests that create-issue uses the safe-outputs global github-token.
`

		testFile := filepath.Join(tmpDir, "test-issue-token.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()

		// Compile the workflow
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		// Read the generated YAML
		outputFile := filepath.Join(tmpDir, "test-issue-token.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)

		// Verify that the safe_outputs job exists
		if !strings.Contains(yamlContent, "safe_outputs:") {
			t.Error("Expected safe_outputs job to be generated")
		}

		// Verify that the global token is used for create_issue
		if !strings.Contains(yamlContent, "github-token: ${{ secrets.GLOBAL_PAT }}") {
			t.Error("Expected safe_outputs job to use the global GitHub token")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}
	})

	t.Run("create-pull-request uses safe-outputs global github-token", func(t *testing.T) {
		testContent := `---
name: Test GitHub Token for PRs
on:
  issues:
    types: [opened]
engine: claude
safe-outputs:
  github-token: ${{ secrets.GLOBAL_PAT }}
  create-pull-request:
    draft: true
  create-issue:
    title-prefix: "[AUTO] "
---

# Test GitHub Token for PRs

This workflow tests that create-pull-request uses the safe-outputs global github-token.
`

		testFile := filepath.Join(tmpDir, "test-pr-fallback.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()

		// Compile the workflow
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		// Read the generated YAML
		outputFile := filepath.Join(tmpDir, "test-pr-fallback.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)

		// Verify that both jobs exist and use correct tokens
		if !strings.Contains(yamlContent, "safe_outputs:") {
			t.Error("Expected safe_outputs job to be generated")
		}

		// Verify that the global token is used
		if !strings.Contains(yamlContent, "github-token: ${{ secrets.GLOBAL_PAT }}") {
			t.Error("Expected safe_outputs job to use global GitHub token")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}
	})

	t.Run("add-labels uses safe-outputs global github-token", func(t *testing.T) {
		testContent := `---
name: Test Global GitHub Token for Labels
on:
  issues:
    types: [opened]
engine: claude
safe-outputs:
  github-token: ${{ secrets.GLOBAL_PAT }}
  add-labels:
    allowed: [bug, feature, enhancement]
---

# Test Global GitHub Token for Labels

This workflow tests that add-labels uses the safe-outputs global github-token.
`

		testFile := filepath.Join(tmpDir, "test-labels-token.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()

		// Compile the workflow
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		// Read the generated YAML
		outputFile := filepath.Join(tmpDir, "test-labels-token.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)

		// Verify that the safe_outputs job is generated
		if !strings.Contains(yamlContent, "safe_outputs:") {
			t.Error("Expected safe_outputs job to be generated")
		}

		// Verify the github token is used
		if !strings.Contains(yamlContent, "github-token: ${{ secrets.GLOBAL_PAT }}") {
			t.Error("Expected safe_outputs job to use the global GitHub token")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}
	})

	t.Run("backward compatibility - global github-token still works", func(t *testing.T) {
		testContent := `---
name: Test Backward Compatibility
on:
  issues:
    types: [opened]
engine: claude
safe-outputs:
  github-token: ${{ secrets.LEGACY_PAT }}
  create-issue:
    title-prefix: "[AUTO] "
    # No individual github-token, should use global
---

# Test Backward Compatibility

This workflow tests that the global github-token still works when no individual tokens are specified.
`

		testFile := filepath.Join(tmpDir, "test-backward-compatibility.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()

		// Compile the workflow
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		// Read the generated YAML
		outputFile := filepath.Join(tmpDir, "test-backward-compatibility.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)

		// Verify that the safe_outputs job uses the global token
		if !strings.Contains(yamlContent, "safe_outputs:") {
			t.Error("Expected safe_outputs job to be generated")
		}

		if !strings.Contains(yamlContent, "github-token: ${{ secrets.LEGACY_PAT }}") {
			t.Error("Expected safe_outputs job to use the global GitHub token for backward compatibility")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}
	})
}
