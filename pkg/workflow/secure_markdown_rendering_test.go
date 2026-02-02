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

func TestSecureMarkdownRendering_Integration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "secure-markdown-test")

	// Simple workflow with GitHub expressions
	testContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
---

# Test Workflow

Repository: ${{ github.repository }}
Actor: ${{ github.actor }}
Run ID: ${{ github.run_id }}
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

	// Debug: print the compiled YAML section we care about
	lines := strings.Split(compiledStr, "\n")
	inPromptStep := false
	for i, line := range lines {
		if strings.Contains(line, "name: Create prompt") {
			inPromptStep = true
		}
		if inPromptStep {
			t.Logf("Line %d: %s", i, line)
			if i > 0 && strings.Contains(lines[i-1], "PROMPT_EOF") && strings.Contains(line, "name:") && !strings.Contains(line, "Create prompt") {
				break
			}
		}
	}

	// Verify that environment variables are defined for GitHub expressions
	// Simple expressions like github.repository generate pretty names like GH_AW_GITHUB_REPOSITORY
	if !strings.Contains(compiledStr, "GH_AW_GITHUB_") {
		t.Error("Compiled workflow should contain GH_AW_* environment variables")
	}

	// Verify the original expressions have been replaced in the prompt heredoc content
	// Find the heredoc section by looking for the "cat " line
	heredocStart := strings.Index(compiledStr, "cat << 'PROMPT_EOF' > \"$GH_AW_PROMPT\"")
	if heredocStart == -1 {
		t.Error("Could not find prompt heredoc section")
	} else {
		// Find the end of the heredoc
		heredocEnd := strings.Index(compiledStr[heredocStart:], "\n          PROMPT_EOF\n")
		if heredocEnd == -1 {
			t.Error("Could not find end of prompt heredoc")
		} else {
			heredocContent := compiledStr[heredocStart : heredocStart+heredocEnd]
			// Verify original expressions are NOT in the heredoc content
			if strings.Contains(heredocContent, "Repository: ${{ github.repository }}") {
				t.Error("Original GitHub expressions should be replaced with environment variable references in the prompt heredoc")
			}
		}
	}

	// Verify that placeholder references ARE in the heredoc content
	if !strings.Contains(compiledStr, "__GH_AW_GITHUB_") {
		t.Error("Placeholder references should be in the prompt content")
	}

	// Verify environment variables are set with GitHub expressions
	if !strings.Contains(compiledStr, "GH_AW_GITHUB_REPOSITORY") || !strings.Contains(compiledStr, ": ${{ github.repository }}") {
		t.Error("Environment variables should be set to GitHub expression values")
	}
}
