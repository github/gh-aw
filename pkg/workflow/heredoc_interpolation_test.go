//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestHeredocInterpolation verifies that PROMPT_EOF heredoc delimiter is quoted
// to prevent bash variable interpolation. Variables are interpolated using github-script instead.
func TestHeredocInterpolation(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "heredoc-interpolation-test")

	// Workflow with markdown content containing GitHub expressions
	// These should be extracted and replaced with ${GH_AW_...} references
	// Simple expressions like github.repository generate pretty names like GH_AW_GITHUB_REPOSITORY
	testContent := `---
on: issues
permissions:
  contents: read
engine: copilot
---

# Test Workflow with Expressions

Repository: ${{ github.repository }}
Actor: ${{ github.actor }}
`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	lockFile := stringutil.MarkdownToLockFile(testFile)
	compiledYAML, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	compiledStr := string(compiledYAML)

	// Verify that heredoc delimiters ARE quoted (should be 'PROMPT_EOF' not PROMPT_EOF)
	// This prevents shell variable interpolation
	if !strings.Contains(compiledStr, "<< 'PROMPT_EOF'") {
		t.Error("PROMPT_EOF delimiter should be quoted to prevent shell variable interpolation")

		// Show the problematic lines
		lines := strings.Split(compiledStr, "\n")
		for i, line := range lines {
			if strings.Contains(line, "<< PROMPT_EOF") && !strings.Contains(line, "'PROMPT_EOF'") {
				t.Logf("Line %d with unquoted delimiter: %s", i, line)
			}
		}
	}

	// Verify that the prompt content contains ${GH_AW_...} references
	// These will be interpolated by the github-script step, not by bash
	// Simple expressions like github.repository generate pretty names like GH_AW_GITHUB_REPOSITORY
	if !strings.Contains(compiledStr, "${GH_AW_") {
		t.Error("Prompt content should contain ${GH_AW_...} references for JavaScript interpolation")
	}

	// Verify the original expressions have been replaced in the prompt heredoc content
	// Find the heredoc section by looking for the "cat " line and the PROMPT_EOF delimiter
	heredocStart := strings.Index(compiledStr, "cat << 'PROMPT_EOF' > \"$GH_AW_PROMPT\"")
	if heredocStart == -1 {
		t.Error("Could not find prompt heredoc section")
	} else {
		// Find the end of the heredoc (PROMPT_EOF on its own line)
		heredocEnd := strings.Index(compiledStr[heredocStart:], "\n          PROMPT_EOF\n")
		if heredocEnd == -1 {
			t.Error("Could not find end of prompt heredoc")
		} else {
			heredocContent := compiledStr[heredocStart : heredocStart+heredocEnd]
			// Verify original expressions are NOT in the heredoc content
			if strings.Contains(heredocContent, "Repository: ${{ github.repository }}") {
				t.Error("Original GitHub expressions should be replaced with ${GH_AW_...} references in prompt heredoc")
			}
		}
	}

	// Verify that the interpolation and template rendering step exists
	if !strings.Contains(compiledStr, "- name: Interpolate variables and render templates") {
		t.Error("Compiled workflow should contain interpolation and template rendering step")
	}

	// Verify that the step uses github-script
	if !strings.Contains(compiledStr, "uses: actions/github-script@") {
		t.Error("Interpolation and template rendering step should use actions/github-script")
	}

	// Verify environment variables are defined in the step
	// Simple expressions like github.repository generate pretty names like GH_AW_GITHUB_REPOSITORY
	if !strings.Contains(compiledStr, "GH_AW_GITHUB_") {
		t.Error("Interpolation and template rendering step should contain GH_AW_* environment variables")
	}
}

// TestHeredocInterpolationMainPrompt tests that the main prompt content uses quoted delimiter
func TestHeredocInterpolationMainPrompt(t *testing.T) {
	tmpDir := testutil.TempDir(t, "heredoc-main-test")

	testContent := `---
on: issues
permissions:
  contents: read
engine: copilot
---

# Test Workflow

Repository: ${{ github.repository }}
Actor: ${{ github.actor }}
`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	compiledYAML, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	compiledStr := string(compiledYAML)

	// All heredoc delimiters should be quoted to prevent shell expansion
	quotedCount := strings.Count(compiledStr, "<< 'PROMPT_EOF'")
	if quotedCount == 0 {
		t.Error("Expected quoted PROMPT_EOF delimiters to prevent shell variable interpolation")
	}

	// Verify interpolation and template rendering step exists
	if !strings.Contains(compiledStr, "- name: Interpolate variables and render templates") {
		t.Error("Expected interpolation and template rendering step for JavaScript-based variable interpolation")
	}
}
