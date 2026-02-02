//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompileWorkflowWithInvalidYAML(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "invalid-yaml-test")

	tests := []struct {
		name                string
		content             string
		expectedErrorLine   int
		expectedErrorColumn int
		expectedMessagePart string
		description         string
	}{
		{
			name: "unclosed_bracket_in_array",
			content: `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
tools:
  github:
    allowed: [list_issues
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow

Invalid YAML with unclosed bracket.`,
			expectedErrorLine:   9, // Error detected at 'engine: claude' line in YAML (line 9 after opening ---)
			expectedErrorColumn: 1,
			expectedMessagePart: "',' or ']' must be specified",
			description:         "unclosed bracket in array should be detected",
		},
		{
			name: "invalid_mapping_context",
			content: `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
invalid: yaml: syntax
  more: bad
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow

Invalid YAML with bad mapping.`,
			expectedErrorLine:   6, // Line 6 in YAML content (after opening ---)
			expectedErrorColumn: 10,
			expectedMessagePart: "mapping value is not allowed in this context",
			description:         "invalid mapping context should be detected",
		},
		{
			name: "bad_indentation",
			content: `---
on: push
permissions:
contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow

Invalid YAML with bad indentation.`,
			expectedErrorLine:   3, // Line 3 in YAML content
			expectedErrorColumn: 11,
			expectedMessagePart: "mapping value is not allowed in this context",
			description:         "bad indentation should be detected",
		},
		{
			name: "unclosed_quote",
			content: `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
tools:
  github:
    allowed: ["list_issues]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow

Invalid YAML with unclosed quote.`,
			expectedErrorLine:   8, // Line 8 in YAML content
			expectedErrorColumn: 15,
			expectedMessagePart: "could not find end character of double-quoted text",
			description:         "unclosed quote should be detected",
		},
		{
			name: "duplicate_keys",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
permissions:
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow

Invalid YAML with duplicate keys.`,
			expectedErrorLine:   6, // Line 6 in YAML content (second permissions:)
			expectedErrorColumn: 1,
			expectedMessagePart: "mapping key \"permissions\" already defined",
			description:         "duplicate keys should be detected",
		},
		{
			name: "invalid_boolean_value",
			content: `---
on: push
permissions:
  contents: read
  issues: yes_please
  pull-requests: read
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with non-boolean value for permissions.`,
			expectedErrorLine:   3,                                              // The permissions field is on line 3
			expectedErrorColumn: 13,                                             // After "permissions:"
			expectedMessagePart: "value must be one of 'read', 'write', 'none'", // Schema validation catches this
			description:         "invalid boolean values should trigger schema validation error",
		},
		{
			name: "missing_colon_in_mapping",
			content: `---
on: push
permissions
  contents: read
  issues: write
engine: claude
strict: false
features:
  dangerous-permissions-write: true
---

# Test Workflow

Invalid YAML with missing colon.`,
			expectedErrorLine:   2, // Line 2 in YAML content (permissions without colon)
			expectedErrorColumn: 1,
			expectedMessagePart: "unexpected key name",
			description:         "missing colon in mapping should be detected",
		},
		{
			name: "invalid_array_syntax_missing_comma",
			content: `---
on: push
tools:
  github:
    allowed: ["list_issues" "create_issue"]
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with missing comma in array.`,
			expectedErrorLine:   4, // Line 4 in YAML content (the allowed line)
			expectedErrorColumn: 29,
			expectedMessagePart: "',' or ']' must be specified",
			description:         "missing comma in array should be detected",
		},
		{
			name:                "mixed_tabs_and_spaces",
			content:             "---\non: push\npermissions:\n  contents: read\n\tissues: write\nengine: claude\n---\n\n# Test Workflow\n\nInvalid YAML with mixed tabs and spaces.",
			expectedErrorLine:   4, // Line 4 in YAML content (the line with tab)
			expectedErrorColumn: 1,
			expectedMessagePart: "found character '\t' that cannot start any token",
			description:         "mixed tabs and spaces should be detected",
		},
		{
			name: "invalid_number_format",
			content: `---
on: push
timeout-minutes: 05.5
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with invalid number format.`,
			expectedErrorLine:   3, // The timeout-minutes field is on line 3
			expectedErrorColumn: 17,
			expectedMessagePart: "got number, want integer", // Schema validation catches this
			description:         "invalid number format should trigger schema validation error",
		},
		{
			name: "invalid_nested_structure",
			content: `---
on: push
tools:
  github: {
    allowed: ["list_issues"]
  }
  claude: [
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with malformed nested structure.`,
			expectedErrorLine:   6, // Line 6 in YAML content (claude: [)
			expectedErrorColumn: 11,
			expectedMessagePart: "sequence end token ']' not found",
			description:         "invalid nested structure should be detected",
		},
		{
			name: "unclosed_flow_mapping",
			content: `---
on: push
permissions: {contents: read, issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow

Invalid YAML with unclosed flow mapping.`,
			expectedErrorLine:   3, // Line 3 in YAML content (engine: claude - where error is detected)
			expectedErrorColumn: 1,
			expectedMessagePart: "',' or '}' must be specified",
			description:         "unclosed flow mapping should be detected",
		},
		{
			name: "yaml_error_with_column_information_support",
			content: `---
on: push
message: "invalid escape sequence \x in middle"
engine: claude
strict: false
---

# Test Workflow

YAML error that demonstrates column position handling.`,
			expectedErrorLine:   3, // The message field is on line 3
			expectedErrorColumn: 1, // Schema validation error
			expectedMessagePart: "Unknown property: message",
			description:         "yaml error should be extracted with column information when available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, fmt.Sprintf("%s.md", tt.name))
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Create compiler
			compiler := NewCompiler()

			// Attempt compilation - should fail with proper error formatting
			err := compiler.CompileWorkflow(testFile)
			if err == nil {
				t.Errorf("%s: expected compilation to fail due to invalid YAML", tt.description)
				return
			}

			errorStr := err.Error()

			// Determine if this is a YAML parsing error or schema validation error
			isYAMLParsingError := strings.Contains(errorStr, "failed to parse frontmatter:")
			isSchemaValidationError := strings.Contains(errorStr, "error:") && !isYAMLParsingError

			if isYAMLParsingError {
				// For YAML parsing errors, check for yaml.FormatError() style [line:column] format
				expectedPattern := fmt.Sprintf("[%d:%d]", tt.expectedErrorLine, tt.expectedErrorColumn)
				if !strings.Contains(errorStr, expectedPattern) {
					t.Errorf("%s: error should contain yaml.FormatError [line:col] format '%s', got: %s", tt.description, expectedPattern, errorStr)
				}

				// Verify yaml.FormatError() output contains context lines with '|' markers
				// and visual pointer '>' to indicate error location
				if !strings.Contains(errorStr, "|") {
					t.Errorf("%s: error should contain context lines with '|' markers from yaml.FormatError(), got: %s", tt.description, errorStr)
				}
				if !strings.Contains(errorStr, ">") {
					t.Errorf("%s: error should contain visual pointer '>' from yaml.FormatError(), got: %s", tt.description, errorStr)
				}
			} else if isSchemaValidationError {
				// For schema validation errors, check for filename:line:column: format
				expectedPattern := fmt.Sprintf(".md:%d:%d:", tt.expectedErrorLine, tt.expectedErrorColumn)
				if !strings.Contains(errorStr, expectedPattern) {
					t.Errorf("%s: error should contain console.FormatError 'filename:line:column:' format '%s', got: %s", tt.description, expectedPattern, errorStr)
				}
			}

			// Verify error contains "error:" type indicator or "failed to parse frontmatter:"
			if !strings.Contains(errorStr, "error:") && !strings.Contains(errorStr, "failed to parse frontmatter:") {
				t.Errorf("%s: error should contain error indicator, got: %s", tt.description, errorStr)
			}

			// Verify error contains the expected YAML error message part
			if !strings.Contains(errorStr, tt.expectedMessagePart) {
				t.Errorf("%s: error should contain '%s', got: %s", tt.description, tt.expectedMessagePart, errorStr)
			}
		})
	}
}

// TestYAMLFormatErrorOutput tests that yaml.FormatError() is used for YAML parsing errors
func TestYAMLFormatErrorOutput(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-format-error-test")

	tests := []struct {
		name            string
		content         string
		expectedLineCol string
		expectedInError []string
		expectPointer   bool
		description     string
	}{
		{
			name: "simple_syntax_error",
			content: `---
on: push
invalid: yaml: syntax
engine: copilot
---

# Test

Test content.`,
			expectedLineCol: "[2:10]",
			expectedInError: []string{"mapping value is not allowed"},
			expectPointer:   true,
			description:     "simple syntax error shows formatted output",
		},
		{
			name: "duplicate_key_error",
			content: `---
on: push
tools:
  github:
    mode: remote
tools:
  playwright: {}
engine: copilot
---

# Test

Test content.`,
			expectedLineCol: "[5:1]",
			expectedInError: []string{"mapping key \"tools\" already defined"},
			expectPointer:   true,
			description:     "duplicate key error shows formatted output with both locations",
		},
		{
			name: "missing_value_colon",
			content: `---
on: push
permissions
  contents: read
engine: copilot
---

# Test

Test content.`,
			expectedLineCol: "[2:1]",
			expectedInError: []string{"unexpected key name", "permissions"},
			expectPointer:   true,
			description:     "missing colon shows formatted output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, fmt.Sprintf("%s.md", tt.name))
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)
			if err == nil {
				t.Errorf("%s: expected compilation to fail", tt.description)
				return
			}

			errorStr := err.Error()

			// Check for [line:col] format from yaml.FormatError()
			if !strings.Contains(errorStr, tt.expectedLineCol) {
				t.Errorf("%s: error should contain line:col format '%s', got: %s", tt.description, tt.expectedLineCol, errorStr)
			}

			// Check that expected strings are in the error
			for _, expected := range tt.expectedInError {
				if !strings.Contains(errorStr, expected) {
					t.Errorf("%s: error should contain '%s', got: %s", tt.description, expected, errorStr)
				}
			}

			// Check for line number markers (|) from yaml.FormatError()
			if !strings.Contains(errorStr, "|") {
				t.Errorf("%s: error should contain line number markers '|' from yaml.FormatError(), got: %s", tt.description, errorStr)
			}

			// Check for visual pointer (>)
			if tt.expectPointer && !strings.Contains(errorStr, ">") {
				t.Errorf("%s: error should contain visual pointer '>' from yaml.FormatError(), got: %s", tt.description, errorStr)
			}

			// Check that it's a YAML parsing error (not schema validation)
			if !strings.Contains(errorStr, "failed to parse frontmatter:") {
				t.Errorf("%s: error should be a frontmatter parsing error, got: %s", tt.description, errorStr)
			}
		})
	}
}

// TestCommentOutProcessedFieldsInOnSection tests the commentOutProcessedFieldsInOnSection function directly

// ========================================
// convertGoPatternToJavaScript Tests
// ========================================

// TestConvertGoPatternToJavaScript tests the convertGoPatternToJavaScript method
func TestConvertGoPatternToJavaScript(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name      string
		goPattern string
		expected  string
	}{
		{
			name:      "case insensitive flag removed",
			goPattern: "(?i)error.*pattern",
			expected:  "error.*pattern",
		},
		{
			name:      "no flag to remove",
			goPattern: "error.*pattern",
			expected:  "error.*pattern",
		},
		{
			name:      "empty pattern",
			goPattern: "",
			expected:  "",
		},
		{
			name:      "flag at start only",
			goPattern: "(?i)",
			expected:  "",
		},
		{
			name:      "complex pattern with flag",
			goPattern: "(?i)^(ERROR|WARN|FATAL):.*$",
			expected:  "^(ERROR|WARN|FATAL):.*$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.convertGoPatternToJavaScript(tt.goPattern)
			if result != tt.expected {
				t.Errorf("convertGoPatternToJavaScript(%q) = %q, want %q",
					tt.goPattern, result, tt.expected)
			}
		})
	}
}

func TestAddCustomStepsAsIsBasic(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name        string
		customSteps string
		expectedIn  []string
		expectedNot []string
	}{
		{
			name: "basic steps",
			customSteps: `steps:
  - name: Setup
    run: echo "setup"`,
			expectedIn: []string{"name: Setup", "run: echo"},
		},
		{
			name: "multiple steps",
			customSteps: `steps:
  - name: Step 1
    run: echo "1"
  - name: Step 2
    run: echo "2"`,
			expectedIn: []string{"name: Step 1", "name: Step 2"},
		},
		{
			name: "step with uses",
			customSteps: `steps:
  - name: Checkout
    uses: actions/checkout@v4`,
			expectedIn: []string{"name: Checkout", "uses: actions/checkout"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var builder strings.Builder
			compiler.addCustomStepsAsIs(&builder, tt.customSteps)
			result := builder.String()

			for _, expected := range tt.expectedIn {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected %q in result:\n%s", expected, result)
				}
			}

			for _, notExpected := range tt.expectedNot {
				if strings.Contains(result, notExpected) {
					t.Errorf("Did not expect %q in result:\n%s", notExpected, result)
				}
			}
		})
	}
}

// ========================================
// Integration Tests for generateYAML
// ========================================

// TestGenerateYAMLBasicWorkflow tests generating YAML for a basic workflow
func TestGenerateYAMLBasicWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-gen-test")

	frontmatter := `---
name: Test Workflow
on: push
permissions:
  contents: read
engine: copilot
strict: false
---

# Test Workflow

This is a test workflow.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check basic workflow structure
	expectedElements := []string{
		"name: \"Test Workflow\"",
		"on:",
		"push",
		"permissions:",
		"jobs:",
	}

	for _, expected := range expectedElements {
		if !strings.Contains(yamlStr, expected) {
			t.Errorf("Expected %q in generated YAML", expected)
		}
	}
}

// TestGenerateYAMLWithDescription tests that description is added as comment
func TestGenerateYAMLWithDescription(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-desc-test")

	frontmatter := `---
name: Test Workflow
description: This workflow does important things
on: push
permissions:
  contents: read
engine: copilot
strict: false
---

# Test Workflow

Test content.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Description should appear in comments
	if !strings.Contains(yamlStr, "# This workflow does important things") {
		t.Error("Expected description to be in comments")
	}
}

// TestGenerateYAMLAutoGeneratedDisclaimer tests that disclaimer is added
func TestGenerateYAMLAutoGeneratedDisclaimer(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-disclaimer-test")

	frontmatter := `---
name: Test Workflow
on: push
permissions:
  contents: read
engine: copilot
strict: false
---

# Test Workflow

Test content.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check for auto-generated disclaimer - the version may or may not be present
	// For dev builds: "This file was automatically generated by gh-aw. DO NOT EDIT."
	// For release builds: "This file was automatically generated by gh-aw (version). DO NOT EDIT."
	if !strings.Contains(yamlStr, "This file was automatically generated by gh-aw") ||
		!strings.Contains(yamlStr, "DO NOT EDIT") {
		t.Error("Expected auto-generated disclaimer")
	}
}

// TestGenerateYAMLWithEnvironment tests that environment is properly set
func TestGenerateYAMLWithEnvironment(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-env-test")

	frontmatter := `---
name: Test Workflow
on: push
permissions:
  contents: read
engine: copilot
strict: false
environment: production
---

# Test Workflow

Test content.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check for environment in output
	if !strings.Contains(yamlStr, "environment:") {
		t.Error("Expected environment in generated YAML")
	}
}

// TestGenerateYAMLWithConcurrency tests that concurrency is properly set
func TestGenerateYAMLWithConcurrency(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-concurrency-test")

	frontmatter := `---
name: Test Workflow
on: push
permissions:
  contents: read
engine: copilot
strict: false
concurrency:
  group: test-group
  cancel-in-progress: true
---

# Test Workflow

Test content.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check for concurrency in output
	if !strings.Contains(yamlStr, "concurrency:") {
		t.Error("Expected concurrency in generated YAML")
	}
}

// TestGenerateYAMLStripsANSIEscapeCodes tests that ANSI escape sequences are removed from YAML comments
func TestGenerateYAMLStripsANSIEscapeCodes(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-ansi-test")

	// Test with ANSI codes in description, source, and other comments
	frontmatter := `---
name: Test Workflow
description: "This workflow \x1b[31mdoes important\x1b[0m things\x1b[m"
on: push
permissions:
  contents: read
engine: copilot
strict: false
---

# Test Workflow

Test content.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Verify ANSI codes are stripped from description
	if !strings.Contains(yamlStr, "# This workflow does important things") {
		t.Error("Expected clean description without ANSI codes in comments")
	}

	// Verify no ANSI escape sequences remain in the file
	if strings.Contains(yamlStr, "\x1b[") {
		t.Error("Found ANSI escape sequences in generated YAML file")
	}

	// Verify the [m pattern (without ESC) is also not present
	// This catches cases where only the trailing part of an ANSI code remains
	if strings.Contains(yamlStr, "[31m") || strings.Contains(yamlStr, "[0m") || strings.Contains(yamlStr, "[m") {
		// Check if it's actually an ANSI code pattern (after ESC character removal)
		// We want to allow normal brackets like [something] but catch ANSI patterns
		lines := strings.Split(yamlStr, "\n")
		for i, line := range lines {
			if strings.Contains(line, "[m") || strings.Contains(line, "[0m") || strings.Contains(line, "[31m") {
				t.Errorf("Found ANSI code remnant in generated YAML at line %d: %q", i+1, line)
			}
		}
	}
}

// TestGenerateYAMLStripsANSIFromAllFields tests ANSI stripping from all workflow metadata fields
func TestGenerateYAMLStripsANSIFromAllFields(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-ansi-all-fields-test")

	// Test with ANSI codes in multiple fields: description, source, imports, stop-time, manual-approval
	frontmatter := `---
name: Test Workflow
description: "Workflow with \x1b[1mANSI\x1b[0m codes"
on: push
permissions:
  contents: read
engine: copilot
strict: false
---

# Test Workflow

Test content.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Verify description has ANSI codes stripped
	if !strings.Contains(yamlStr, "# Workflow with ANSI codes") {
		t.Error("Expected clean description without ANSI codes")
	}

	// Verify no ANSI escape sequences anywhere
	if strings.Contains(yamlStr, "\x1b[") {
		t.Error("Found ANSI escape sequences in generated YAML file")
	}
}

// TestGenerateYAMLStripsANSIFromImportedFiles tests ANSI stripping from imported file paths
func TestGenerateYAMLStripsANSIFromImportedFiles(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-ansi-imports-test")

	// Create a workflow that will have imported files
	// We'll create it manually by modifying WorkflowData
	compiler := NewCompiler()

	// Create a simple workflow file first
	frontmatter := `---
name: Test Workflow
on: push
permissions:
  contents: read
engine: copilot
strict: false
---

# Test Workflow

Test content.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("ParseWorkflowFile() error: %v", err)
	}

	// Add ANSI codes to imported/included files
	workflowData.ImportedFiles = []string{
		"path/to/\x1b[32mfile1.md\x1b[0m",
		"path/to/\x1b[31mfile2.md\x1b[m",
	}
	workflowData.IncludedFiles = []string{
		"path/to/\x1b[1minclude1.md\x1b[0m",
	}

	// Compile with the modified data
	if err := compiler.CompileWorkflowData(workflowData, testFile); err != nil {
		t.Fatalf("CompileWorkflowData() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Verify imported files have ANSI codes stripped
	if !strings.Contains(yamlStr, "path/to/file1.md") {
		t.Error("Expected clean imported file path without ANSI codes")
	}
	if !strings.Contains(yamlStr, "path/to/file2.md") {
		t.Error("Expected clean imported file path without ANSI codes")
	}
	if !strings.Contains(yamlStr, "path/to/include1.md") {
		t.Error("Expected clean included file path without ANSI codes")
	}

	// Verify no ANSI escape sequences remain
	if strings.Contains(yamlStr, "\x1b[") {
		t.Error("Found ANSI escape sequences in generated YAML file")
	}
}

// TestGenerateYAMLStripsANSIFromStopTimeAndManualApproval tests ANSI stripping from stop-time and manual-approval
func TestGenerateYAMLStripsANSIFromStopTimeAndManualApproval(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-ansi-stoptime-test")

	// Create workflow with stop-time and manual-approval containing ANSI codes
	compiler := NewCompiler()

	frontmatter := `---
name: Test Workflow
on: push
permissions:
  contents: read
engine: copilot
strict: false
---

# Test Workflow

Test content.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("ParseWorkflowFile() error: %v", err)
	}

	// Add ANSI codes to stop-time and manual-approval
	workflowData.StopTime = "2026-12-31\x1b[31mT23:59:59Z\x1b[0m"
	workflowData.ManualApproval = "production-\x1b[1menv\x1b[0m"

	// Compile with the modified data
	if err := compiler.CompileWorkflowData(workflowData, testFile); err != nil {
		t.Fatalf("CompileWorkflowData() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Verify stop-time has ANSI codes stripped
	if !strings.Contains(yamlStr, "# Effective stop-time: 2026-12-31T23:59:59Z") {
		t.Error("Expected clean stop-time without ANSI codes")
	}

	// Verify manual-approval has ANSI codes stripped
	if !strings.Contains(yamlStr, "# Manual approval required: environment 'production-env'") {
		t.Error("Expected clean manual-approval without ANSI codes")
	}

	// Verify no ANSI escape sequences remain
	if strings.Contains(yamlStr, "\x1b[") {
		t.Error("Found ANSI escape sequences in generated YAML file")
	}
}

// TestGenerateYAMLStripsANSIMultilineDescription tests ANSI stripping from multiline descriptions
func TestGenerateYAMLStripsANSIMultilineDescription(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-ansi-multiline-test")

	compiler := NewCompiler()

	// Create workflow with simple description first
	frontmatter := `---
name: Test Workflow
on: push
permissions:
  contents: read
engine: copilot
strict: false
---

# Test Workflow

Test content.`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("ParseWorkflowFile() error: %v", err)
	}

	// Set a multiline description with ANSI codes
	workflowData.Description = "Line 1 with \x1b[32mgreen\x1b[0m text\nLine 2 with \x1b[31mred\x1b[0m text\nLine 3 with \x1b[1mbold\x1b[0m text"

	// Compile with the modified data
	if err := compiler.CompileWorkflowData(workflowData, testFile); err != nil {
		t.Fatalf("CompileWorkflowData() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Verify all lines have ANSI codes stripped
	if !strings.Contains(yamlStr, "# Line 1 with green text") {
		t.Error("Expected clean line 1 without ANSI codes")
	}
	if !strings.Contains(yamlStr, "# Line 2 with red text") {
		t.Error("Expected clean line 2 without ANSI codes")
	}
	if !strings.Contains(yamlStr, "# Line 3 with bold text") {
		t.Error("Expected clean line 3 without ANSI codes")
	}

	// Verify no ANSI escape sequences remain
	if strings.Contains(yamlStr, "\x1b[") {
		t.Error("Found ANSI escape sequences in generated YAML file")
	}
}
