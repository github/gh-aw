//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
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
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with unclosed bracket.`,
			expectedErrorLine:   10, // Error detected at 'engine: claude' line (line 10 in file, line 9 in YAML content after opening ---)
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
  issues: read
  pull-requests: read
invalid: yaml: syntax
  more: bad
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with bad mapping.`,
			expectedErrorLine:   7, // Line 7 in file (line 6 in YAML content after opening ---)
			expectedErrorColumn: 10,
			expectedMessagePart: "unexpected ':'",
			description:         "invalid mapping context should be detected",
		},
		{
			name: "bad_indentation",
			content: `---
on: push
permissions:
contents: read
  issues: read
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with bad indentation.`,
			expectedErrorLine:   4, // Line 4 in file (line 3 in YAML content after opening ---)
			expectedErrorColumn: 11,
			expectedMessagePart: "unexpected ':'",
			description:         "bad indentation should be detected",
		},
		{
			name: "unclosed_quote",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: ["list_issues]
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with unclosed quote.`,
			expectedErrorLine:   9, // Line 9 in file (line 8 in YAML content after opening ---)
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
  issues: read
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with duplicate keys.`,
			expectedErrorLine:   7, // Line 7 in file (line 6 in YAML content - second permissions:)
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
  issues: read
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with missing colon.`,
			expectedErrorLine:   3, // Line 3 in file (line 2 in YAML content - permissions without colon)
			expectedErrorColumn: 1,
			expectedMessagePart: "missing ':' after key",
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
			expectedErrorLine:   5, // Line 5 in file (line 4 in YAML content - the allowed line)
			expectedErrorColumn: 29,
			expectedMessagePart: "',' or ']' must be specified",
			description:         "missing comma in array should be detected",
		},
		{
			name: "github_tool_scalar_with_nested_key",
			content: `---
on: push
tools:
  github: "invalid-string"
    toolsets: [default]
engine: claude
strict: false
---

# Test Workflow

Invalid YAML with scalar github tool config that has nested keys.`,
			expectedErrorLine:   4, // highlight the invalid github scalar value line, not the nested key line
			expectedErrorColumn: 11,
			expectedMessagePart: "tools.github tool config must be a mapping (object), not a scalar value",
			description:         "invalid github tool scalar should point to the scalar line with actionable message",
		},
		{
			name:                "mixed_tabs_and_spaces",
			content:             "---\non: push\npermissions:\n  contents: read\n\tissues: write\nengine: claude\n---\n\n# Test Workflow\n\nInvalid YAML with mixed tabs and spaces.",
			expectedErrorLine:   5, // Line 5 in file (line 4 in YAML content - the line with tab)
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
			expectedMessagePart: "expected integer or string, got number", // Synthesized from oneOf type conflicts
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
			expectedErrorLine:   7, // Line 7 in file (line 6 in YAML content - claude: [)
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
strict: false
---

# Test Workflow

Invalid YAML with unclosed flow mapping.`,
			expectedErrorLine:   4, // Line 4 in file (line 3 in YAML content - engine: claude where error is detected)
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
			testFile := filepath.Join(tmpDir, tt.name+".md")
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
			expectedLineCol: "[3:10]", // Line 3 in file (line 2 in YAML content)
			expectedInError: []string{"unexpected ':'"},
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
			expectedLineCol: "[6:1]", // Line 6 in file (second tools: key)
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
			expectedLineCol: "[3:1]", // Line 3 in file (permissions without colon)
			expectedInError: []string{"missing ':' after key", "permissions"},
			expectPointer:   true,
			description:     "missing colon shows formatted output",
		},
		{
			name: "error_on_line_2_included_in_snippet",
			content: `---
private
engine: copilot
on: push
---

# Test

Test content.`,
			expectedLineCol: "[2:1]", // Line 2 in file (first frontmatter line, missing colon)
			expectedInError: []string{"missing ':' after key", "private"},
			expectPointer:   true,
			description:     "error on line 2 (first frontmatter line) must appear in the code snippet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.name+".md")
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

			// Check for VSCode-compatible format on the first line: filename:line:column: error: message
			// This should NOT include a duplicate [line:col] line
			vscodeFormatPattern := tt.expectedLineCol[1:len(tt.expectedLineCol)-1] + ": error:"
			if !strings.Contains(errorStr, vscodeFormatPattern) {
				t.Errorf("%s: error should contain VSCode format '%s', got: %s", tt.description, vscodeFormatPattern, errorStr)
			}

			// The error should NOT contain the duplicate [line:col] line (we only want source context)
			// Check that we don't have the standalone [line:col] message line
			lines := strings.Split(errorStr, "\n")
			for i, line := range lines {
				// Skip the first line (VSCode format)
				if i == 0 {
					continue
				}
				// Check if any subsequent line is just [line:col] message without source context markers
				if strings.HasPrefix(strings.TrimSpace(line), "[") && !strings.Contains(line, "|") && !strings.Contains(line, "already defined at") {
					t.Errorf("%s: error should not contain duplicate [line:col] message line (line %d: %q), got: %s", tt.description, i+1, line, errorStr)
				}
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

			// Check that it's a YAML parsing error with VSCode-compatible format
			// Format: filename:line:column: error: message
			if !strings.Contains(errorStr, ": error: ") {
				t.Errorf("%s: error should have VSCode-compatible format (filename:line:column: error: message), got: %s", tt.description, errorStr)
			}
		})
	}
}

// TestEngineValidationErrorHasFileLocation verifies that invalid engine errors include
// the file:line:column: prefix pointing to the "engine:" field in the source file.
func TestEngineValidationErrorHasFileLocation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-location-test")

	tests := []struct {
		name              string
		content           string
		expectedErrorLine int
		description       string
	}{
		{
			name: "invalid_engine_line3",
			content: `---
on: push
engine: openai_gpt
---

# Test Workflow

Content.`,
			expectedErrorLine: 3, // "engine: openai_gpt" is on line 3 in the file
			description:       "invalid engine on line 3 should produce error at line 3",
		},
		{
			name: "invalid_engine_with_other_fields",
			content: `---
on: push
permissions:
  contents: read
engine: badengine_xyz
strict: false
---

# Test Workflow

Content.`,
			expectedErrorLine: 5, // "engine: badengine_xyz" is on line 5 in the file
			description:       "invalid engine after permissions block should produce error at correct line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.name+".md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)
			if err == nil {
				t.Errorf("%s: expected compilation to fail for invalid engine", tt.description)
				return
			}

			errorStr := err.Error()

			// Verify error contains filename:line:col: format pointing to the engine field
			expectedPattern := fmt.Sprintf(".md:%d:1:", tt.expectedErrorLine)
			if !strings.Contains(errorStr, expectedPattern) {
				t.Errorf("%s: error should contain '%s' pointing to the engine: field, got: %s",
					tt.description, expectedPattern, errorStr)
			}

			// Verify the error contains the invalid engine name
			if !strings.Contains(errorStr, "invalid engine:") {
				t.Errorf("%s: error should contain 'invalid engine:', got: %s", tt.description, errorStr)
			}

			// Verify the error type indicator is present
			if !strings.Contains(errorStr, "error:") {
				t.Errorf("%s: error should contain 'error:' type indicator, got: %s", tt.description, errorStr)
			}
		})
	}
}

func TestEngineTypeValidationErrorUsesSingleSourceLocationAndSnippet(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-type-error-test")

	// Integer engine values bypass validateEngineBeforeSchema, so this verifies the
	// schema-validation path still reports one authoritative location plus snippet.
	content := `---
on: push
name: test
engine: 123
---

# Test
`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("expected compilation to fail")
	}

	errorStr := err.Error()
	if !strings.Contains(errorStr, testFile+":4:8: error:") {
		t.Errorf("error should point to engine value location in header, got: %s", errorStr)
	}
	if strings.Contains(errorStr, "(line ") || strings.Contains(errorStr, ", col ") {
		t.Errorf("single schema failure should not repeat a second line/col location in the message body, got: %s", errorStr)
	}
	if !strings.Contains(errorStr, "4 | engine: 123") {
		t.Errorf("error should include the engine source line snippet, got: %s", errorStr)
	}
}

// TestInvalidEngineReportedBeforeImportErrors verifies that an invalid engine: value
// is reported immediately, even when imports also fail. Previously the import error
// would shadow the engine typo.
func TestInvalidEngineReportedBeforeImportErrors(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-before-import-test")

	content := `---
engine: copiilot
imports:
  - shared/skip-if-issue-open.md
on: push
---

# Test

Content.`
	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("expected compilation to fail")
	}

	errorStr := err.Error()

	// Should report the invalid engine, not the missing import
	if !strings.Contains(errorStr, "invalid engine") {
		t.Errorf("error should contain 'invalid engine' (reported before import errors), got: %s", errorStr)
	}

	// Should include a "Did you mean: copilot?" suggestion
	if !strings.Contains(errorStr, "Did you mean: copilot?") {
		t.Errorf("error should include the exact suggestion line, got: %s", errorStr)
	}

	// Should preserve filename:line:col formatting for editor navigation.
	if !strings.Contains(errorStr, testFile) {
		t.Errorf("error should include the source file path, got: %s", errorStr)
	}
	if !regexp.MustCompile(regexp.QuoteMeta(testFile) + `:\d+:\d+: error:`).MatchString(errorStr) {
		t.Errorf("error should have filename:line:col format, got: %s", errorStr)
	}

	// Should NOT report the missing import (engine error is primary)
	if strings.Contains(errorStr, "import file not found") {
		t.Errorf("import error should be suppressed when engine is invalid, got: %s", errorStr)
	}
}

func TestInvalidEngineReportedBeforeSchemaErrors(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-before-schema-test")

	content := `---
on: push
engine: copiilot
bogus-field: true
---

# Test
`
	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("expected compilation to fail")
	}

	errorStr := err.Error()
	if !strings.Contains(errorStr, "invalid engine: copiilot") {
		t.Errorf("error should prioritize the invalid engine typo, got: %s", errorStr)
	}
	if !strings.Contains(errorStr, "Did you mean: copilot?") {
		t.Errorf("error should include the closest engine suggestion, got: %s", errorStr)
	}
	if strings.Contains(errorStr, "Unknown property: bogus-field") {
		t.Errorf("schema error should not shadow the invalid engine typo, got: %s", errorStr)
	}
	if !strings.Contains(errorStr, testFile+":3:1: error:") {
		t.Errorf("error should point to the engine field location, got: %s", errorStr)
	}
}

// TestImportNotFoundHint verifies that a tailored hint is shown when an import cannot be resolved.
func TestImportNotFoundHint(t *testing.T) {
	tmpDir := testutil.TempDir(t, "import-hint-test")

	content := `---
engine: copilot
imports:
  - shared/missing-file.md
on: push
---

# Test

Content.`
	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("expected compilation to fail")
	}

	errorStr := err.Error()

	// Should show the import error
	if !strings.Contains(errorStr, "import file not found") {
		t.Errorf("error should contain 'import file not found', got: %s", errorStr)
	}

	// Should include a helpful hint pointing to the import path
	if !strings.Contains(errorStr, "hint:") {
		t.Errorf("error should contain a hint, got: %s", errorStr)
	}
	if !strings.Contains(errorStr, "shared/missing-file.md") {
		t.Errorf("hint should mention the import path 'shared/missing-file.md', got: %s", errorStr)
	}
}

// TestCommentOutProcessedFieldsInOnSection tests the commentOutProcessedFieldsInOnSection function directly

// TestAddCustomStepsAsIsBasic tests adding custom steps as-is
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

func TestGenerateYAMLWithEnvironmentValueContainingColonSpace(t *testing.T) {
	tmpDir := testutil.TempDir(t, "yaml-env-colon-space-test")

	frontmatter := `---
name: Test Workflow
on: push
permissions:
  contents: read
engine:
  id: copilot
  env:
    ANTHROPIC_CUSTOM_HEADERS: "x-aw-gw-github-repo: ${{ github.repository }}"
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

	if !strings.Contains(yamlStr, `ANTHROPIC_CUSTOM_HEADERS: "x-aw-gw-github-repo: ${{ github.repository }}"`) {
		t.Fatalf("Expected quoted env value in generated YAML, got:\n%s", yamlStr)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("Generated lock file should be valid YAML: %v\nYAML:\n%s", err, yamlStr)
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

// TestRuntimeImportPathGitHubIO tests that repositories ending with .github.io
// generate correct runtime-import paths without duplicating the .github.io suffix
func TestRuntimeImportPathGitHubIO(t *testing.T) {
	tests := []struct {
		name        string
		repoName    string // simulated repo name in path
		expected    string
		description string
	}{
		{
			name:        "github_pages_repo",
			repoName:    "testuser.github.io",
			expected:    "{{#runtime-import .github/workflows/translate-to-ptbr.md}}",
			description: "GitHub Pages repo should not duplicate .github.io in runtime-import path",
		},
		{
			name:        "another_github_pages_repo",
			repoName:    "anotheruser.github.io",
			expected:    "{{#runtime-import .github/workflows/test.md}}",
			description: "Another GitHub Pages repo should work correctly",
		},
		{
			name:        "normal_repo",
			repoName:    "myrepo",
			expected:    "{{#runtime-import .github/workflows/workflow.md}}",
			description: "Normal repo without .github.io should work as before",
		},
		{
			name:        "repo_named_dot_github",
			repoName:    ".github",
			expected:    "{{#runtime-import .github/workflows/test.md}}",
			description: "Repo named '.github' should not duplicate .github in runtime-import path",
		},
		{
			name:        "repo_with_github_in_name",
			repoName:    "my-github-project",
			expected:    "{{#runtime-import .github/workflows/test.md}}",
			description: "Repo with 'github' in name should only match .github directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory that simulates the repo structure
			// with repo name in the path
			tmpBase := testutil.TempDir(t, "runtime-import-path-test")
			tmpDir := filepath.Join(tmpBase, tt.repoName)

			// Create .github/workflows directory
			workflowDir := filepath.Join(tmpDir, ".github", "workflows")
			if err := os.MkdirAll(workflowDir, 0755); err != nil {
				t.Fatalf("Failed to create workflow directory: %v", err)
			}

			// Determine workflow filename from expected path
			expectedParts := strings.Split(tt.expected, " ")
			if len(expectedParts) < 2 {
				t.Fatalf("Invalid expected format: %s", tt.expected)
			}
			workflowFilePath := strings.TrimSuffix(expectedParts[1], "}}")
			workflowBasename := filepath.Base(workflowFilePath)

			// Create a simple workflow file
			workflowPath := filepath.Join(workflowDir, workflowBasename)
			workflowContent := `---
on: push
engine: copilot
---

# Test Workflow

This is a test workflow.`

			if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("%s: Compilation failed: %v", tt.description, err)
			}

			// Calculate lock file path
			lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"

			// Read the generated lock file
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContentStr := string(lockContent)

			// Check that the runtime-import path is correct
			if !strings.Contains(lockContentStr, tt.expected) {
				t.Errorf("%s: Expected to find '%s' in lock file", tt.description, tt.expected)

				// Find what runtime-import was actually generated
				lines := strings.SplitSeq(lockContentStr, "\n")
				for line := range lines {
					if strings.Contains(line, "{{#runtime-import") {
						t.Logf("Found runtime-import: %s", strings.TrimSpace(line))
					}
				}
			}

			// Also verify that .github.io is NOT duplicated in the path
			if strings.Contains(lockContentStr, ".github.io/.github/workflows") {
				t.Errorf("%s: Found incorrect path with duplicated .github.io prefix", tt.description)
			}
		})
	}
}

// TestRuntimeImportPathRelative tests that relative markdown paths (e.g. ".github/workflows/test.md")
// produce the same runtime-import path as absolute paths.  This simulates the difference between
// `gh aw upgrade` (relative CWD-based paths) and `gh aw compile` (absolute git-root-based paths).
func TestRuntimeImportPathRelative(t *testing.T) {
	// Create a temporary directory that acts as the repo root.
	repoRoot := testutil.TempDir(t, "runtime-import-relative-test")

	// Create .github/workflows directory
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("Failed to create workflow directory: %v", err)
	}

	// Write a simple workflow
	workflowContent := `---
on: push
engine: copilot
---

# Test Workflow

This is a test workflow.`

	workflowPath := filepath.Join(workflowDir, "my-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Change CWD to repo root so that relative paths resolve correctly
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(origWD); err != nil {
			t.Logf("Warning: failed to restore working directory: %v", err)
		}
	}()
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Failed to change to repo root: %v", err)
	}

	// Compile using a relative path (simulates gh aw upgrade behaviour)
	relPath := filepath.Join(".github", "workflows", "my-workflow.md")
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(relPath); err != nil {
		t.Fatalf("Compilation failed with relative path: %v", err)
	}

	lockContent, err := os.ReadFile(strings.TrimSuffix(relPath, ".md") + ".lock.yml")
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	// The runtime-import path must start with ".github/" regardless of whether the
	// caller passed a relative or absolute markdown path.
	const expected = "{{#runtime-import .github/workflows/my-workflow.md}}"
	if !strings.Contains(string(lockContent), expected) {
		t.Errorf("Expected to find %q in lock file but it was not present.\nLock file content excerpt:\n%s",
			expected, extractRuntimeImportLines(string(lockContent)))
	}
}

// extractRuntimeImportLines returns all runtime-import lines from a string for diagnostic output.
func extractRuntimeImportLines(content string) string {
	var lines []string
	for line := range strings.SplitSeq(content, "\n") {
		if strings.Contains(line, "runtime-import") {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	if len(lines) == 0 {
		return "(no runtime-import lines found)"
	}
	return strings.Join(lines, "\n")
}

func TestLockMetadataVersionInReleaseBuilds(t *testing.T) {
	// Save and restore original values
	originalIsRelease := isReleaseBuild
	originalVersion := compilerVersion
	defer func() {
		isReleaseBuild = originalIsRelease
		compilerVersion = originalVersion
	}()

	tmpDir := testutil.TempDir(t, "lock-metadata-version")

	// Test both dev and release modes
	tests := []struct {
		name          string
		isRelease     bool
		version       string
		actionTag     string
		expectVersion bool
	}{
		{
			name:          "dev build should not include version",
			isRelease:     false,
			version:       "dev",
			actionTag:     "",
			expectVersion: false,
		},
		{
			name:          "release build should include version",
			isRelease:     true,
			version:       "v0.1.2",
			actionTag:     "",
			expectVersion: true,
		},
		{
			name:          "action-tag compile should include current ref",
			isRelease:     false,
			version:       "401bd13",
			actionTag:     "v9.9.9",
			expectVersion: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set version and release flag
			SetIsRelease(tt.isRelease)
			SetVersion(tt.version)

			// Create a simple workflow
			workflowContent := `---
engine: copilot
on: issues
---
# Test Workflow

Test prompt.
`
			workflowPath := filepath.Join(tmpDir, tt.name+".md")
			if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			compiler.SetActionTag(tt.actionTag)
			err := compiler.CompileWorkflow(workflowPath)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the lock file
			lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContentStr := string(lockContent)

			// Extract metadata line
			metadataLine := ""
			lines := strings.SplitSeq(lockContentStr, "\n")
			for line := range lines {
				if strings.Contains(line, "gh-aw-metadata:") {
					metadataLine = line
					break
				}
			}

			if metadataLine == "" {
				t.Fatal("Could not find gh-aw-metadata in lock file")
			}

			// Check if version is present
			hasVersion := strings.Contains(metadataLine, `"compiler_version"`)

			if tt.expectVersion && !hasVersion {
				t.Errorf("Expected version to be included in metadata for release build, but it was not found.\nMetadata: %s", metadataLine)
			}

			if !tt.expectVersion && hasVersion {
				t.Errorf("Expected version to NOT be included in metadata for dev build, but it was found.\nMetadata: %s", metadataLine)
			}

			// If version is expected, verify it matches
			if tt.expectVersion && hasVersion {
				expectedVersionStr := fmt.Sprintf(`"compiler_version":"%s"`, tt.version)
				if !strings.Contains(metadataLine, expectedVersionStr) {
					t.Errorf("Expected version '%s' to be in metadata, but got:\n%s", tt.version, metadataLine)
				}
			}
		})
	}
}

func TestCompileWorkflowMetadataIncludesDocs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "lock-metadata-docs")
	workflowPath := filepath.Join(tmpDir, "docs.md")
	workflowContent := `---
engine: copilot
metadata:
  docs: https://docs.example.com/automation/repository-health
on: issues
---
# Test Workflow

Test prompt.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	if err := NewCompiler().CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockContent, err := os.ReadFile(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	metadata, _, err := ExtractMetadataFromLockFile(string(lockContent))
	if err != nil {
		t.Fatalf("Failed to extract lock metadata: %v", err)
	}
	if metadata == nil {
		t.Fatal("Expected lock metadata")
	}
	if metadata.Docs != "https://docs.example.com/automation/repository-health" {
		t.Errorf("Docs = %q, want documentation URL", metadata.Docs)
	}
}

func TestCompileWorkflowMetadataDocsImportPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name         string
		mainMetadata string
		want         string
	}{
		{name: "first import fallback", want: "https://docs.example.com/first"},
		{name: "main workflow wins", mainMetadata: "metadata:\n  docs: https://docs.example.com/main\n", want: "https://docs.example.com/main"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "lock-metadata-docs-import")
			for _, imported := range []struct {
				name string
				docs string
			}{
				{name: "first.md", docs: "https://docs.example.com/first"},
				{name: "second.md", docs: "https://docs.example.com/second"},
			} {
				content := fmt.Sprintf("---\nmetadata:\n  docs: %s\n---\n\nImported prompt.\n", imported.docs)
				if err := os.WriteFile(filepath.Join(tmpDir, imported.name), []byte(content), 0o644); err != nil {
					t.Fatalf("Failed to write imported workflow: %v", err)
				}
			}

			workflowPath := filepath.Join(tmpDir, "main.md")
			workflowContent := fmt.Sprintf(`---
engine: copilot
imports:
  - first.md
  - second.md
%son: issues
---
# Test Workflow

Test prompt.
`, tt.mainMetadata)
			if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}
			if err := NewCompiler().CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			lockContent, err := os.ReadFile(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			metadata, _, err := ExtractMetadataFromLockFile(string(lockContent))
			if err != nil {
				t.Fatalf("Failed to extract lock metadata: %v", err)
			}
			if metadata == nil {
				t.Fatal("Expected lock metadata")
			}
			if metadata.Docs != tt.want {
				t.Fatalf("Docs = %q, want %q", metadata.Docs, tt.want)
			}
		})
	}
}

func TestCompileWorkflowMetadataIncludesEngineVersionsAndRunnerIdentifier(t *testing.T) {
	tmpDir := testutil.TempDir(t, "lock-metadata-engine-versions")

	workflowContent := `---
engine:
  id: copilot
  copilot-sdk: true
runs-on:
  - self-hosted
  - linux
on: issues
---
# Test Workflow

Test prompt.
`
	workflowPath := filepath.Join(tmpDir, "metadata-engine-versions.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	var metadataLine string
	var manifestLine string
	for line := range strings.SplitSeq(string(lockContent), "\n") {
		if trimmed, ok := strings.CutPrefix(line, "# gh-aw-metadata: "); ok {
			metadataLine = trimmed
		}
		if trimmed, ok := strings.CutPrefix(line, "# gh-aw-manifest: "); ok {
			manifestLine = trimmed
		}
	}
	if metadataLine == "" {
		t.Fatal("Could not find gh-aw-metadata in lock file")
	}
	if strings.Contains(metadataLine, "\n") {
		t.Fatal("Expected gh-aw-metadata payload to remain a single-line JSON string")
	}

	var metadata LockMetadata
	if err := json.Unmarshal([]byte(metadataLine), &metadata); err != nil {
		t.Fatalf("Failed to parse lock metadata JSON: %v", err)
	}

	if got := metadata.EngineVersions["copilot"]; got == "" {
		t.Fatal("Expected copilot version in metadata engine_versions")
	}
	if got := metadata.EngineVersions["copilot-sdk"]; got == "" {
		t.Fatal("Expected copilot-sdk version in metadata engine_versions when copilot-sdk is enabled")
	}
	if metadata.EngineBaseURLCustomized {
		t.Fatal("Expected engine_base_url_customized=false for default copilot configuration")
	}
	if metadata.AgentImageRunner != `["self-hosted","linux"]` {
		t.Fatalf("Expected serialized array runner identifier, got: %q", metadata.AgentImageRunner)
	}

	if manifestLine == "" {
		t.Fatal("Could not find gh-aw-manifest in lock file")
	}
	if strings.Contains(manifestLine, "\n") {
		t.Fatal("Expected gh-aw-manifest payload to remain a single-line JSON string")
	}
	var manifest map[string]any
	if err := json.Unmarshal([]byte(manifestLine), &manifest); err != nil {
		t.Fatalf("Failed to parse gh-aw-manifest JSON: %v", err)
	}
	if _, exists := manifest["engine_versions"]; exists {
		t.Fatal("gh-aw-manifest must not duplicate engine_versions metadata")
	}
	if _, exists := manifest["agent_image_runner"]; exists {
		t.Fatal("gh-aw-manifest must not duplicate agent_image_runner metadata")
	}
}

func TestGenerateWorkflowHeaderIncludesEffectiveDetectionMetadata(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI:           "pi",
		EngineConfig: &EngineConfig{ID: "pi"},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	var yaml strings.Builder
	if err := compiler.generateWorkflowHeader(&yaml, data, "frontmatter-hash", "body-hash", nil, nil); err != nil {
		t.Fatalf("Failed to generate workflow header: %v", err)
	}

	metadata, legacy, err := ExtractMetadataFromLockFile(yaml.String())
	if err != nil {
		t.Fatalf("Failed to extract lock metadata: %v", err)
	}
	if metadata == nil || legacy {
		t.Fatal("Expected structured lock metadata")
	}
	if metadata.DetectionAgentID != "copilot" {
		t.Fatalf("Expected normalized detection engine copilot, got %q", metadata.DetectionAgentID)
	}
	if metadata.DetectionAgentModel != "detection" {
		t.Fatalf("Expected effective detection model alias, got %q", metadata.DetectionAgentModel)
	}
	if metadata.EngineVersions["copilot"] == "" {
		t.Fatal("Expected normalized detection engine version in metadata")
	}
}

func TestGenerateWorkflowHeaderOmitsDisabledDetectionEngineMetadata(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI:           "copilot",
		EngineConfig: &EngineConfig{ID: "copilot"},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EngineDisabled: true,
				Steps:          []any{map[string]any{"run": "echo custom detection"}},
			},
		},
	}

	var yaml strings.Builder
	if err := compiler.generateWorkflowHeader(&yaml, data, "frontmatter-hash", "body-hash", nil, nil); err != nil {
		t.Fatalf("Failed to generate workflow header: %v", err)
	}

	metadata, _, err := ExtractMetadataFromLockFile(yaml.String())
	if err != nil {
		t.Fatalf("Failed to extract lock metadata: %v", err)
	}
	if metadata.DetectionAgentID != "" || metadata.DetectionAgentModel != "" {
		t.Fatalf("Expected disabled detection engine metadata to be omitted, got engine=%q model=%q", metadata.DetectionAgentID, metadata.DetectionAgentModel)
	}
}

func TestCompileWorkflowMetadataMarksCopilotCustomConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "lock-metadata-copilot-custom-config")

	workflowContent := `---
engine:
  id: copilot
  api-target: api.acme.ghe.com
on: issues
---
# Test Workflow

Test prompt.
`
	workflowPath := filepath.Join(tmpDir, "metadata-copilot-custom-config.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	var metadataLine string
	for line := range strings.SplitSeq(string(lockContent), "\n") {
		if trimmed, ok := strings.CutPrefix(line, "# gh-aw-metadata: "); ok {
			metadataLine = trimmed
		}
	}
	if metadataLine == "" {
		t.Fatal("Could not find gh-aw-metadata in lock file")
	}

	var metadata LockMetadata
	if err := json.Unmarshal([]byte(metadataLine), &metadata); err != nil {
		t.Fatalf("Failed to parse lock metadata JSON: %v", err)
	}

	if !metadata.EngineBaseURLCustomized {
		t.Fatal("Expected engine_base_url_customized=true when copilot api-target is customized")
	}
}

func TestNormalizeBlankLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Empty or all-whitespace input: return a single newline to match the
		// original strings.TrimRight(…, "\n") + "\n" behaviour — the caller
		// always expects a trailing newline even when there is no real content.
		{"empty string", "", "\n"},
		{"single blank line", "\n", "\n"},
		{"all whitespace line", "   \n", "\n"},
		{"no trailing newline", "hello", "hello\n"},
		{"trailing blank lines stripped", "a\n\nb\n\n\n", "a\n\nb\n"},
		{"blank lines in middle preserved", "a\n\nb\n", "a\n\nb\n"},
		{"whitespace-only lines cleared", "a\n   \nb\n", "a\n\nb\n"},
		{"single non-blank line", "key: value\n", "key: value\n"},
		{"multiple trailing blank lines", "a\n\n\n\n", "a\n"},
		{"only whitespace lines", "   \n   \n", "\n"},
		{"structural trailing spaces trimmed", "key: value   \n", "key: value\n"},
		{"structural trailing tabs trimmed", "a:\tb\t\nb: c\n", "a:\tb\nb: c\n"},
		{"indentation preserved when trailing spaces trimmed", "  foo: bar  \n", "  foo: bar\n"},
		{"two structural blanks kept at limit", "a\n\n\nb\n", "a\n\n\nb\n"},
		{"three structural blanks capped to two", "a\n\n\n\nb\n", "a\n\n\nb\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeBlankLines(tc.input)
			if got != tc.want {
				t.Errorf("normalizeBlankLines(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeBlankLinesPreservesBlockScalarContent(t *testing.T) {
	input := strings.Join([]string{
		"name: demo   ",
		"jobs:",
		"  build:",
		"    steps:",
		"      - run: |",
		"          echo hello   ",
		"",
		"",
		"",
		"          echo world\\  ",
		"",
	}, "\n")

	output := normalizeBlankLines(input)
	if !strings.HasPrefix(output, "name: demo\n") {
		t.Fatalf("normalizeBlankLines should trim structural trailing spaces, got %q", output)
	}

	parseRun := func(content string) string {
		t.Helper()

		var doc map[string]any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatalf("yaml.Unmarshal failed: %v", err)
		}

		jobs := doc["jobs"].(map[string]any)
		build := jobs["build"].(map[string]any)
		steps := build["steps"].([]any)
		step := steps[0].(map[string]any)
		return step["run"].(string)
	}

	if got, want := parseRun(output), parseRun(input); got != want {
		t.Fatalf("block scalar content changed after normalization\nwant: %q\ngot:  %q", want, got)
	}
}

// ========================================
// Tests for yamlBlockScalarState / appendYAMLLine
// ========================================

// TestYamlBlockScalarStateUpdate verifies that the block-scalar state machine
// correctly identifies payload lines and structural lines.
func TestYamlBlockScalarStateUpdate(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		wantBS []bool // expected isBlockScalarContent for each line
	}{
		{
			name: "no block scalar",
			lines: []string{
				"- name: foo  ",
				"  run: echo hello  ",
			},
			wantBS: []bool{false, false},
		},
		{
			name: "literal block scalar payload preserved",
			lines: []string{
				"  run: |",
				"    echo hello   ",
				"    echo world\\  ",
			},
			wantBS: []bool{false, true, true},
		},
		{
			name: "blank line inside block scalar does not exit",
			lines: []string{
				"  run: |",
				"    line1   ",
				"",
				"    line2   ",
			},
			wantBS: []bool{false, true, true, true},
		},
		{
			name: "outdented line exits block scalar",
			lines: []string{
				"  run: |",
				"    content   ",
				"  other: value  ",
			},
			wantBS: []bool{false, true, false},
		},
		{
			name: "folded block scalar (>) also tracked",
			lines: []string{
				"  script: >",
				"    folded content   ",
			},
			wantBS: []bool{false, true},
		},
		{
			name: "blank line between header and content stays pending",
			lines: []string{
				"  run: |",
				"",
				"    content   ",
			},
			wantBS: []bool{false, false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var blockScalarState yamlBlockScalarState
			for i, line := range tt.lines {
				got := blockScalarState.update(line)
				if got != tt.wantBS[i] {
					t.Errorf("line %d %q: update() = %v, want %v", i, line, got, tt.wantBS[i])
				}
			}
		})
	}
}

// TestAppendYAMLLine verifies that structural lines are trimmed and block-scalar
// content is preserved verbatim.
func TestAppendYAMLLine(t *testing.T) {
	tests := []struct {
		name      string
		yamlLines []string // source lines (no prefix)
		prefix    string
		want      string
	}{
		{
			name: "structural trailing spaces are trimmed",
			yamlLines: []string{
				"- name: foo   ",
				"  key: value   ",
			},
			prefix: "      ",
			want:   "      - name: foo\n        key: value\n",
		},
		{
			name: "block scalar payload preserved verbatim",
			yamlLines: []string{
				"run: |",
				"  echo hello   ",
				"  echo world\\  ",
			},
			prefix: "      ",
			want:   "      run: |\n        echo hello   \n        echo world\\  \n",
		},
		{
			name: "blank lines always bare newlines",
			yamlLines: []string{
				"run: |",
				"  line1   ",
				"",
				"  line2   ",
			},
			prefix: "      ",
			want:   "      run: |\n        line1   \n\n        line2   \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			var blockScalarState yamlBlockScalarState
			for _, line := range tt.yamlLines {
				isBS := blockScalarState.update(line)
				appendYAMLLine(&b, tt.prefix, line, isBS)
			}
			if got := b.String(); got != tt.want {
				t.Errorf("appendYAMLLine output mismatch\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// ========================================
// Tests for writeStepsSection
// ========================================

// TestWriteStepsSection verifies that writeStepsSection trims trailing whitespace
// from structural YAML lines while preserving block-scalar payload verbatim.
func TestWriteStepsSection(t *testing.T) {
	tests := []struct {
		name      string
		stepsYAML string
		wantLines []string // substrings that must appear in the output
		wantNot   []string // substrings that must NOT appear in the output
	}{
		{
			name:      "structural trailing spaces trimmed",
			stepsYAML: "pre-steps:\n- name: My Step   \n  run: echo hi   \n",
			wantLines: []string{"- name: My Step\n", "run: echo hi\n"},
			wantNot:   []string{"My Step   ", "echo hi   "},
		},
		{
			name: "block scalar payload preserved verbatim",
			// `\\  ` in the Go string literal represents a literal backslash followed by
			// two trailing spaces in the actual content. This is the critical case: a shell
			// line ending in `\  ` (backslash + spaces) must not be trimmed because the
			// spaces prevent the backslash from acting as a line-continuation character.
			stepsYAML: "pre-steps:\n- name: Script\n  run: |\n    echo hello   \n    echo world\\  \n",
			wantLines: []string{"echo hello   ", "echo world\\  "},
		},
		{
			name:      "blank lines emitted as bare newlines",
			stepsYAML: "pre-steps:\n- name: A\n  run: echo a\n\n- name: B\n  run: echo b\n",
			wantLines: []string{"- name: A", "- name: B"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			writeStepsSection(&b, tt.stepsYAML)
			got := b.String()

			for _, want := range tt.wantLines {
				if !strings.Contains(got, want) {
					t.Errorf("expected output to contain %q\ngot: %q", want, got)
				}
			}
			for _, notWant := range tt.wantNot {
				if strings.Contains(got, notWant) {
					t.Errorf("expected output NOT to contain %q\ngot: %q", notWant, got)
				}
			}
		})
	}
}

// TestAddCustomStepsAsIsTrimsStructuralTrailingSpaces verifies that addCustomStepsAsIs
// trims trailing whitespace from structural YAML lines but preserves block-scalar payload.
func TestAddCustomStepsAsIsTrimsStructuralTrailingSpaces(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name        string
		customSteps string
		wantLines   []string // substrings that must appear
		wantNot     []string // substrings that must NOT appear
	}{
		{
			name:        "structural trailing spaces are trimmed",
			customSteps: "steps:\n- name: My Step   \n  uses: actions/checkout@v4   \n",
			wantLines:   []string{"- name: My Step\n", "uses: actions/checkout@v4\n"},
			wantNot:     []string{"My Step   ", "checkout@v4   "},
		},
		{
			name: "block scalar run content preserved verbatim",
			// `\\  ` in the Go string literal represents a literal backslash followed by
			// two trailing spaces. Trimming would change `\  ` → `\`, flipping the shell
			// backslash-newline continuation semantics — so payload must be kept verbatim.
			customSteps: "steps:\n- name: Script\n  run: |\n    echo trailing   \n    echo bs\\  \n",
			wantLines:   []string{"echo trailing   ", "echo bs\\  "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			compiler.addCustomStepsAsIs(&b, tt.customSteps)
			got := b.String()

			for _, want := range tt.wantLines {
				if !strings.Contains(got, want) {
					t.Errorf("expected output to contain %q\ngot: %q", want, got)
				}
			}
			for _, notWant := range tt.wantNot {
				if strings.Contains(got, notWant) {
					t.Errorf("expected output NOT to contain %q\ngot: %q", notWant, got)
				}
			}
		})
	}
}

// TestInterpolationStepPresentWithGitHubFalse verifies the bug fix for the scenario where
// a workflow has tools.github: false (no GitHub MCP server), no template expressions, and no
// {{#if}} blocks. Before the fix the compiler skipped the "Interpolate variables and render
// templates" step because it didn't account for the {{#runtime-import}} self-import macro that
// is always emitted in normal (non-inline) compilation mode. This caused the agent to receive
// an unresolved macro and no effective instructions.
func TestInterpolationStepPresentWithGitHubFalse(t *testing.T) {
	tmpDir := testutil.TempDir(t, "interpolation-step-github-false")
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("failed to create workflow directory: %v", err)
	}

	// Minimal workflow that previously triggered the bug:
	// - engine.id set (no GitHub tool inferred)
	// - tools.github: false (hasGitHubContext == false)
	// - no {{#if}} or ${{ }} in body (hasTemplatePattern == false, hasExpressions == false)
	workflowContent := `---
on: repository_dispatch
permissions:
  contents: read
engine:
  id: claude
tools:
  edit:
  github: false
safe-outputs:
  create-pull-request:
---

Do some important work.
`
	workflowPath := filepath.Join(workflowDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	lockPath := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockContent := string(lockBytes)

	// The compiled lock must contain a runtime-import macro (always emitted in normal mode).
	if !strings.Contains(lockContent, "{{#runtime-import") {
		t.Error("expected lock file to contain a {{#runtime-import}} macro")
	}

	// And it must contain the interpolation step to resolve that macro.
	if !strings.Contains(lockContent, "Interpolate variables and render templates") {
		t.Error("expected lock file to contain 'Interpolate variables and render templates' step, " +
			"but it was absent; the {{#runtime-import}} macro will not be resolved at runtime")
	}
	if !strings.Contains(lockContent, "interpolate_prompt.cjs") {
		t.Error("expected lock file to reference interpolate_prompt.cjs in the interpolation step")
	}
}
