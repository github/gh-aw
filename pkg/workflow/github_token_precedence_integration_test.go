//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestTopLevelGitHubTokenPrecedence(t *testing.T) {
	tmpDir := testutil.TempDir(t, "github-token-precedence-test")

	t.Run("top-level github-token used when no safe-outputs token", func(t *testing.T) {
		testContent := `---
name: Test Top-Level GitHub Token
on:
  issues:
    types: [opened]
engine: claude
github-token: ${{ secrets.TOPLEVEL_PAT }}
tools:
  github:
    mode: remote
    allowed: [list_issues]
---

# Test Top-Level GitHub Token

Test that top-level github-token is used in engine configuration.
`

		testFile := filepath.Join(tmpDir, "test-toplevel-token.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		outputFile := filepath.Join(tmpDir, "test-toplevel-token.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)

		// Verify that the top-level token is used in the GitHub MCP config
		// The token should be set in the env block to prevent template injection
		if !strings.Contains(yamlContent, "GITHUB_MCP_SERVER_TOKEN: ${{ secrets.TOPLEVEL_PAT }}") {
			t.Error("Expected top-level github-token to be used in GITHUB_MCP_SERVER_TOKEN env var")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}

		// Verify that the Authorization header uses the env variable
		if !strings.Contains(yamlContent, "Bearer $GITHUB_MCP_SERVER_TOKEN") {
			t.Error("Expected Authorization header to use GITHUB_MCP_SERVER_TOKEN env var")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}
	})

	t.Run("safe-outputs github-token overrides top-level", func(t *testing.T) {
		testContent := `---
name: Test Safe-Outputs Override
on:
  issues:
    types: [opened]
engine: claude
github-token: ${{ secrets.TOPLEVEL_PAT }}
safe-outputs:
  github-token: ${{ secrets.SAFE_OUTPUTS_PAT }}
  create-issue:
---

# Test Safe-Outputs Override

Test that safe-outputs github-token overrides top-level.
`

		testFile := filepath.Join(tmpDir, "test-safe-outputs-override.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		outputFile := filepath.Join(tmpDir, "test-safe-outputs-override.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)
		// Strip the comment header to check only the actual YAML content
		yamlContentNoComments := testutil.StripYAMLCommentHeader(yamlContent)

		// Verify that safe-outputs token is used in the safe_outputs job
		if !strings.Contains(yamlContentNoComments, "github-token: ${{ secrets.SAFE_OUTPUTS_PAT }}") {
			t.Error("Expected safe-outputs github-token to be used in safe_outputs job")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}

		// Verify that top-level token is NOT used in safe-outputs job
		if strings.Contains(yamlContentNoComments, "github-token: ${{ secrets.TOPLEVEL_PAT }}") {
			t.Error("Top-level github-token should not be used when safe-outputs token is present")
		}
	})

	t.Run("safe-outputs token overrides top-level", func(t *testing.T) {
		testContent := `---
name: Test Safe Outputs Override
on:
  issues:
    types: [opened]
engine: claude
github-token: ${{ secrets.TOPLEVEL_PAT }}
safe-outputs:
  github-token: ${{ secrets.SAFE_OUTPUTS_PAT }}
  create-issue:
    title-prefix: "[AUTO] "
---

# Test Safe Outputs Override

Test that safe-outputs github-token overrides top-level token.
`

		testFile := filepath.Join(tmpDir, "test-individual-override.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		outputFile := filepath.Join(tmpDir, "test-individual-override.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)

		// Verify that safe-outputs token is used in the safe_outputs job
		if !strings.Contains(yamlContent, "github-token: ${{ secrets.SAFE_OUTPUTS_PAT }}") {
			t.Error("Expected safe-outputs github-token to be used in safe_outputs job")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}

		// Verify top-level token is not used in safe_outputs job
		// (it may appear in other jobs, but not in safe_outputs)
		lines := strings.Split(yamlContent, "\n")
		inSafeOutputsJob := false
		for i, line := range lines {
			if strings.Contains(line, "safe_outputs:") && !strings.Contains(line, "#") {
				inSafeOutputsJob = true
				continue
			}
			// Check if we've moved to a new top-level job
			if inSafeOutputsJob && len(line) > 0 && !strings.HasPrefix(line, " ") && strings.Contains(line, ":") {
				inSafeOutputsJob = false
			}
			if inSafeOutputsJob && strings.Contains(line, "github-token: ${{ secrets.TOPLEVEL_PAT }}") {
				t.Errorf("Top-level token should not be used in safe_outputs job (found at line %d)", i+1)
			}
		}
	})

	t.Run("top-level token used in codex engine", func(t *testing.T) {
		testContent := `---
name: Test Codex Engine Token
on:
  workflow_dispatch:
engine: codex
github-token: ${{ secrets.TOPLEVEL_PAT }}
tools:
  github:
    allowed: [list_issues]
---

# Test Codex Engine Token

Test that top-level github-token is used in Codex engine.
`

		testFile := filepath.Join(tmpDir, "test-codex-token.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		outputFile := filepath.Join(tmpDir, "test-codex-token.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)

		// Verify that the top-level token is used in GITHUB_MCP_SERVER_TOKEN env var
		// (Codex now uses GITHUB_MCP_SERVER_TOKEN, same as Copilot, for consistency)
		if !strings.Contains(yamlContent, "GITHUB_MCP_SERVER_TOKEN: ${{ secrets.TOPLEVEL_PAT }}") {
			t.Error("Expected top-level github-token to be used in GITHUB_MCP_SERVER_TOKEN env var for Codex")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}

		// Verify that the MCP gateway config uses GITHUB_PERSONAL_ACCESS_TOKEN with the env var reference
		// The JSON config passes the token to the GitHub MCP server container
		if !strings.Contains(yamlContent, `"GITHUB_PERSONAL_ACCESS_TOKEN": "$GITHUB_MCP_SERVER_TOKEN"`) {
			t.Error("Expected MCP gateway config to pass GITHUB_PERSONAL_ACCESS_TOKEN from GITHUB_MCP_SERVER_TOKEN env var")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}
	})

	t.Run("top-level token used in copilot engine", func(t *testing.T) {
		testContent := `---
name: Test Copilot Engine Token
on:
  workflow_dispatch:
engine: copilot
github-token: ${{ secrets.TOPLEVEL_PAT }}
tools:
  github:
    allowed: [list_issues]
---

# Test Copilot Engine Token

Test that top-level github-token is used in Copilot engine.
`

		testFile := filepath.Join(tmpDir, "test-copilot-token.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		outputFile := filepath.Join(tmpDir, "test-copilot-token.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)

		// Verify that the top-level token is used in GITHUB_MCP_SERVER_TOKEN env var
		if !strings.Contains(yamlContent, "GITHUB_MCP_SERVER_TOKEN: ${{ secrets.TOPLEVEL_PAT }}") {
			t.Error("Expected top-level github-token to be used in GITHUB_MCP_SERVER_TOKEN env var for Copilot")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}
	})

	t.Run("default fallback includes GH_AW_GITHUB_MCP_SERVER_TOKEN", func(t *testing.T) {
		testContent := `---
name: Test Default Token Fallback
on:
  workflow_dispatch:
engine: copilot
tools:
  github:
    allowed: [list_issues]
---

# Test Default Token Fallback

Test that default fallback includes GH_AW_GITHUB_MCP_SERVER_TOKEN.
`

		testFile := filepath.Join(tmpDir, "test-default-fallback.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Fatalf("Unexpected error compiling workflow: %v", err)
		}

		outputFile := filepath.Join(tmpDir, "test-default-fallback.lock.yml")
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatal(err)
		}

		yamlContent := string(content)

		// Verify that the default fallback includes GH_AW_GITHUB_MCP_SERVER_TOKEN first
		expectedFallback := "GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
		if !strings.Contains(yamlContent, expectedFallback) {
			t.Error("Expected default fallback to include GH_AW_GITHUB_MCP_SERVER_TOKEN as the first secret")
			t.Logf("Generated YAML:\n%s", yamlContent)
		}
	})
}
