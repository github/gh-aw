//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/stringutil"

	"github.com/githubnext/gh-aw/pkg/testutil"
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

	// Read the compiled workflow (lock file)
	lockFile := stringutil.MarkdownToLockFile(testFile)
	compiledYAML, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	compiledStr := string(compiledYAML)

	// Verify the interpolation and template rendering step is present in lock file
	if !strings.Contains(compiledStr, "- name: Interpolate variables and render templates") {
		t.Error("Compiled workflow should contain interpolation and template rendering step")
	}

	if !strings.Contains(compiledStr, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
		t.Error("Interpolation and template rendering step should use github-script action")
	}

	// Read the body file which now contains the markdown content
	bodyFile := strings.TrimSuffix(testFile, ".md") + ".body.md"
	bodyContent, err := os.ReadFile(bodyFile)
	if err != nil {
		t.Fatalf("Failed to read body file: %v", err)
	}

	bodyStr := string(bodyContent)

	// Verify that GitHub expressions are replaced with placeholders in the body file
	// Note: With runtime-import, expressions in the body file are processed at runtime,
	// so we should check for the original expressions or runtime-import macro in lock file
	if !strings.Contains(compiledStr, "{{#runtime-import") {
		t.Error("Compiled workflow should contain runtime-import macro")
	}

	// Verify the body file contains the template conditionals
	if !strings.Contains(bodyStr, "{{#if github.event.issue.number}}") {
		t.Error("Body file should contain conditional for github.event.issue.number expression")
	}

	if !strings.Contains(bodyStr, "{{#if github.actor}}") {
		t.Error("Body file should contain conditional for github.actor expression")
	}

	if !strings.Contains(bodyStr, "{{#if true}}") {
		t.Error("Body file should contain conditional for literal true")
	}

	if !strings.Contains(bodyStr, "{{#if false}}") {
		t.Error("Body file should contain conditional for literal false")
	}

	// Verify the setupGlobals helper is used in lock file
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
