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

// TestTemplateExpressionWrappingIntegration verifies end-to-end compilation
// with template expressions that should be wrapped
func TestTemplateExpressionWrappingIntegration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "template-expression-integration")

	// Real-world example workflow with template conditionals
	testContent := `---
on:
  issues:
    types: [opened, edited]
  pull_request:
    types: [opened, edited]
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Issue and PR Analyzer

Analyze the issue or pull request and provide insights.

{{#if github.event.issue.number}}
## Issue Analysis

You are analyzing issue #${{ github.event.issue.number }} in repository ${{ github.repository }}.

The issue was created by ${{ github.actor }}.
{{/if}}

{{#if github.event.pull_request.number}}
## Pull Request Analysis

You are analyzing PR #${{ github.event.pull_request.number }} in repository ${{ github.repository }}.

The PR was created by ${{ github.actor }}.
{{/if}}

{{#if needs.activation.outputs.text}}
## Content

${{ needs.activation.outputs.text }}
{{/if}}

## Instructions

1. Review the content above
2. Provide actionable feedback
3. Create a summary comment
`

	testFile := filepath.Join(tmpDir, "analyzer.md")
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

	// Verify that interpolation and template rendering are present
	if !strings.Contains(compiledStr, "- name: Interpolate variables and render templates") {
		t.Error("Compiled workflow should contain interpolation and template rendering step")
	}

	// Verify GitHub expressions are properly replaced with placeholders in template conditionals
	// The GitHub context section (built-in) should have placeholders
	// User markdown content is loaded via runtime-import and processed at runtime
	expectedPlaceholderExpressions := []string{
		"{{#if __GH_AW_GITHUB_EVENT_ISSUE_NUMBER__ }}",
		"{{#if __GH_AW_GITHUB_EVENT_PULL_REQUEST_NUMBER__ }}",
	}

	for _, expectedExpr := range expectedPlaceholderExpressions {
		if !strings.Contains(compiledStr, expectedExpr) {
			t.Errorf("Compiled workflow should contain placeholder expression in GitHub context: %s", expectedExpr)
		}
	}

	// Verify that the main workflow content is loaded via runtime-import
	// Template conditionals in the user's markdown (like needs.activation.outputs.text)
	// are processed at runtime by the JavaScript runtime_import helper
	if !strings.Contains(compiledStr, "{{#runtime-import") {
		t.Error("Compiled workflow should contain runtime-import macro for main workflow content")
	}

	// Verify that expressions OUTSIDE template conditionals are NOT double-wrapped
	// These should remain as ${{ github.event.issue.number }} (not wrapped again)
	if strings.Contains(compiledStr, "${{ ${{ github.event.issue.number }}") {
		t.Error("Expressions outside template conditionals should not be double-wrapped")
	}

	// Verify that GitHub expressions in content have been replaced with environment variable references
	// in the heredoc, but they can still appear in the comment header
	delimiter := GenerateHeredocDelimiter("PROMPT")
	heredocStart := strings.Index(compiledStr, "cat << '"+delimiter+"' > \"$GH_AW_PROMPT\"")
	if heredocStart == -1 {
		t.Error("Could not find prompt heredoc section")
	} else {
		heredocEnd := strings.Index(compiledStr[heredocStart:], "\n          "+delimiter+"\n")
		if heredocEnd == -1 {
			t.Error("Could not find end of prompt heredoc")
		} else {
			heredocContent := compiledStr[heredocStart : heredocStart+heredocEnd]
			// Verify original expressions are NOT in the heredoc content
			if strings.Contains(heredocContent, "issue #${{ github.event.issue.number }}") {
				t.Error("GitHub expressions in heredoc content should be replaced with environment variable references for security")
			}
		}
	}

	// Verify that environment variables are defined for the expressions
	// Simple expressions like github.repository generate pretty names like GH_AW_GITHUB_REPOSITORY
	if !strings.Contains(compiledStr, "GH_AW_GITHUB_") {
		t.Error("Environment variables should be defined for GitHub expressions")
	}
}

// TestTemplateExpressionAlreadyWrapped verifies that already-wrapped expressions
// are not double-wrapped
func TestTemplateExpressionAlreadyWrapped(t *testing.T) {
	tmpDir := testutil.TempDir(t, "template-already-wrapped")

	// Workflow with pre-wrapped expressions
	testContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
---

# Test Already Wrapped

{{#if ${{ github.event.issue.number }} }}
This expression is already wrapped.
{{/if}}

{{#if github.actor}}
This expression needs wrapping.
{{/if}}
`

	testFile := filepath.Join(tmpDir, "already-wrapped.md")
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

	// After compilation, GitHub expressions are extracted and replaced with placeholders
	// for security. The original ${{ ... }} expressions are not preserved in the compiled output.
	// Instead, we verify that:
	// 1. Placeholders are created for the expressions
	// 2. Placeholders are not double-wrapped
	// 3. Both expressions result in placeholder-based conditionals

	// Verify that placeholder conditionals exist (not the original expressions)
	if !strings.Contains(compiledStr, "{{#if __GH_AW_GITHUB_EVENT_ISSUE_NUMBER__ }}") {
		t.Error("Expected placeholder conditional for github.event.issue.number")
	}

	if !strings.Contains(compiledStr, "{{#if __GH_AW_GITHUB_ACTOR__ }}") {
		t.Error("Expected placeholder conditional for github.actor")
	}

	// Verify that expressions are not double-wrapped with ${{ ${{ ... }}
	if strings.Contains(compiledStr, "${{ ${{") {
		t.Error("Expressions should not be double-wrapped")
	}

	// Verify that the original ${{ }} syntax doesn't appear in conditionals
	// (it should have been extracted and replaced with placeholders)
	if strings.Contains(compiledStr, "{{#if ${{ github.event.issue.number }}") {
		t.Error("Original GitHub expression should have been extracted and replaced with placeholder")
	}
}

// TestTemplateWithMixedExpressionsAndLiterals verifies correct handling
// of template conditionals with both GitHub expressions and literal values
func TestTemplateWithMixedExpressionsAndLiterals(t *testing.T) {
	tmpDir := testutil.TempDir(t, "template-mixed")

	testContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
---

# Mixed Template Test

{{#if github.event.issue.number}}
GitHub expression - will be wrapped.
{{/if}}

{{#if true}}
Literal true - will also be wrapped.
{{/if}}

{{#if false}}
Literal false - will also be wrapped.
{{/if}}

{{#if some_variable}}
Unknown variable - will also be wrapped.
{{/if}}

{{#if steps.my_step.outputs.value}}
Steps expression - will be wrapped.
{{/if}}
`

	testFile := filepath.Join(tmpDir, "mixed.md")
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

	// Verify GitHub expressions in the GitHub context section are replaced with placeholders
	// (These are in the built-in context, not the user's markdown)
	if !strings.Contains(compiledStr, "{{#if __GH_AW_GITHUB_EVENT_ISSUE_NUMBER__ }}") {
		t.Error("GitHub context should contain placeholder for github.event.issue.number")
	}

	// Verify that the main workflow content is loaded via runtime-import
	// Template conditionals in the user's markdown (like steps, true/false literals, etc.)
	// are processed at runtime by the JavaScript runtime_import helper
	if !strings.Contains(compiledStr, "{{#runtime-import") {
		t.Error("Compiled workflow should contain runtime-import macro for main workflow content")
	}

	// Make sure we didn't create invalid double-wrapping
	if strings.Contains(compiledStr, "${{ ${{") {
		t.Error("Should not double-wrap expressions")
	}
}
