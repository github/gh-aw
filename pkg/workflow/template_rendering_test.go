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

func TestTemplateRenderingStep(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "template-rendering-test")

	// Test case with conditional blocks that use GitHub expressions
	testContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: claude
---

# Test Template Rendering

{{#if github.event.issue.number}}
This section should be shown if there's an issue number.
{{/if}}

{{#if github.actor}}
This section should be shown if there's an actor.
{{/if}}

{{#if true}}
This section should be kept (literal true).
{{/if}}

{{#if false}}
This section should be removed (literal false).
{{/if}}

Normal content here.
`

	testFile := filepath.Join(tmpDir, "test-template-rendering.md")
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

	// Verify the interpolation and template rendering step is present
	if !strings.Contains(compiledStr, "- name: Interpolate variables and render templates") {
		t.Error("Compiled workflow should contain interpolation and template rendering step")
	}

	if !strings.Contains(compiledStr, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
		t.Error("Interpolation and template rendering step should use github-script action")
	}

	// Verify runtime-import macro is in lock file
	if !strings.Contains(compiledStr, "{{#runtime-import") {
		t.Error("Compiled workflow should contain runtime-import macro")
	}

	// Verify the runtime-import references the test workflow file
	if !strings.Contains(compiledStr, "test-template-rendering.md") {
		t.Error("Runtime-import should reference the original workflow file")
	}

	// With runtime-import, expressions and templates are processed at runtime
	// The original workflow file (testFile) contains the template conditionals
	// Let's verify the original file has the conditionals (runtime_import.cjs will process them)
	testFileContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}
	testFileStr := string(testFileContent)

	// Verify the original file contains the template conditionals (processed at runtime)
	if !strings.Contains(testFileStr, "{{#if github.event.issue.number}}") {
		t.Error("Workflow file should contain conditional for github.event.issue.number")
	}

	if !strings.Contains(testFileStr, "{{#if github.actor}}") {
		t.Error("Workflow file should contain conditional for github.actor")
	}

	if !strings.Contains(testFileStr, "{{#if true}}") {
		t.Error("Workflow file should contain conditional for literal true")
	}

	if !strings.Contains(testFileStr, "{{#if false}}") {
		t.Error("Workflow file should contain conditional for literal false")
	}

	// Verify the setupGlobals helper is used
	if !strings.Contains(compiledStr, "const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs')") {
		t.Error("Template rendering step should use setupGlobals helper")
	}

	if !strings.Contains(compiledStr, "setupGlobals(core, github, context, exec, io)") {
		t.Error("Template rendering step should call setupGlobals function")
	}

	// Verify the interpolate_prompt script is loaded via require
	if !strings.Contains(compiledStr, "const { main } = require('/opt/gh-aw/actions/interpolate_prompt.cjs')") {
		t.Error("Template rendering step should require interpolate_prompt.cjs")
	}

	if !strings.Contains(compiledStr, "await main()") {
		t.Error("Template rendering step should call main() function")
	}
}

func TestTemplateRenderingStepSkipped(t *testing.T) {
	// NOTE: This test is now less relevant because GitHub tools are added by default,
	// which means GitHub context (with template conditionals) is always added.
	// However, we keep this test to verify that template rendering behaves correctly
	// even when the user's markdown doesn't have conditionals.

	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "template-rendering-skip-test")

	// Test case WITHOUT conditional blocks in user's markdown
	// Note: GitHub tools are added by default, so GitHub context will still be added
	testContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  edit:
  web-fetch:
engine: claude
---

# Test Without Template

Normal content without conditionals.
`

	testFile := filepath.Join(tmpDir, "test-no-template.md")
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

	// Verify the interpolation and template rendering step IS present (because GitHub tool is added by default)
	if !strings.Contains(compiledStr, "- name: Interpolate variables and render templates") {
		t.Error("Compiled workflow should contain interpolation and template rendering step because GitHub tool is added by default")
	}

	// Verify the GitHub context was added (now part of unified prompt creation step)
	if !strings.Contains(compiledStr, "- name: Create prompt with built-in context") {
		t.Error("Compiled workflow should contain unified prompt creation step (which includes GitHub context)")
	}
}

func TestTemplateRenderingStepWithGitHubTool(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "template-rendering-github-test")

	// Test case WITHOUT conditional blocks in markdown but WITH GitHub tool
	testContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: claude
---

# Test With GitHub Tool

Normal content without conditionals in markdown.
`

	testFile := filepath.Join(tmpDir, "test-github-tool.md")
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

	// Verify the interpolation and template rendering step IS present (because GitHub tool adds conditionals)
	if !strings.Contains(compiledStr, "- name: Interpolate variables and render templates") {
		t.Error("Compiled workflow should contain interpolation and template rendering step when GitHub tool is enabled")
	}

	// Verify the GitHub context was added (now part of unified prompt creation step)
	if !strings.Contains(compiledStr, "- name: Create prompt with built-in context") {
		t.Error("Compiled workflow should contain unified prompt creation step (which includes GitHub context)")
	}
}
