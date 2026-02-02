//go:build !integration

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/console"
)

// TestFrontmatterSyntaxErrors provides extensive test suite for frontmatter syntax errors
func TestFrontmatterSyntaxErrors(t *testing.T) {
	tests := []struct {
		name                  string
		frontmatterContent    string
		markdownContent       string
		expectError           bool
		expectedMinLine       int    // Minimum expected line number
		expectedMinColumn     int    // Minimum expected column number
		expectedErrorContains string // Substring that should be in error message
		description           string // Human-readable description of the error scenario
	}{
		{
			name: "missing_colon_in_mapping",
			frontmatterContent: `---
name: Test Workflow
on push
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This is a test workflow.`,
			expectError:           true,
			expectedMinLine:       3,
			expectedMinColumn:     1,
			expectedErrorContains: "non-map value",
			description:           "Missing colon in YAML mapping",
		},
		{
			name: "invalid_indentation",
			frontmatterContent: `---
name: Test Workflow
on:
  push:
    branches:
  - main
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has invalid indentation.`,
			expectError:           true,
			expectedMinLine:       4,
			expectedMinColumn:     1,
			expectedErrorContains: "non-map value",
			description:           "Invalid indentation in nested YAML structure",
		},
		{
			name: "duplicate_keys",
			frontmatterContent: `---
name: Test Workflow
on: push
name: Duplicate Name
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has duplicate keys.`,
			expectError:           true,
			expectedMinLine:       4,
			expectedMinColumn:     1,
			expectedErrorContains: "duplicate",
			description:           "Duplicate keys in YAML frontmatter",
		},
		{
			name: "unclosed_bracket_in_array",
			frontmatterContent: `---
name: Test Workflow
on:
  push:
    branches: [main, dev
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has unclosed brackets.`,
			expectError:           true,
			expectedMinLine:       5,
			expectedMinColumn:     1,
			expectedErrorContains: "must be specified",
			description:           "Unclosed bracket in YAML array",
		},
		{
			name: "unclosed_brace_in_object",
			frontmatterContent: `---
name: Test Workflow
on:
  push: {branches: [main], types: [opened
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has unclosed braces.`,
			expectError:           true,
			expectedMinLine:       4,
			expectedMinColumn:     1,
			expectedErrorContains: "must be specified",
			description:           "Unclosed brace in YAML object",
		},
		{
			name: "invalid_yaml_character",
			frontmatterContent: `---
name: Test Workflow
on: @invalid_character
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has invalid YAML characters.`,
			expectError:           true,
			expectedMinLine:       3,
			expectedMinColumn:     1,
			expectedErrorContains: "reserved character",
			description:           "Invalid character that cannot start YAML token",
		},
		{
			name: "malformed_string_quotes",
			frontmatterContent: `---
name: "Test Workflow
on: push
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has malformed string quotes.`,
			expectError:           true,
			expectedMinLine:       2,
			expectedMinColumn:     1,
			expectedErrorContains: "could not find end character",
			description:           "Malformed string quotes in YAML",
		},
		{
			name: "invalid_boolean_value",
			frontmatterContent: `---
name: Test Workflow
on: push
enabled: yes_please
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has invalid boolean value.`,
			expectError:           false, // This may not cause a parse error, just invalid data
			expectedMinLine:       0,
			expectedMinColumn:     0,
			expectedErrorContains: "",
			description:           "Invalid boolean value in YAML (may parse as string)",
		},
		{
			name: "missing_value_after_colon",
			frontmatterContent: `---
name: Test Workflow
on:
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has missing value after colon.`,
			expectError:           false, // This actually parses as null value
			expectedMinLine:       0,
			expectedMinColumn:     0,
			expectedErrorContains: "",
			description:           "Missing value after colon in YAML mapping (parses as null)",
		},
		{
			name: "invalid_list_structure",
			frontmatterContent: `---
name: Test Workflow
on:
  push:
    branches:
      main
      - dev
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has invalid list structure.`,
			expectError:           false, // This may actually parse successfully
			expectedMinLine:       0,
			expectedMinColumn:     0,
			expectedErrorContains: "",
			description:           "Invalid list structure mixing plain and dash syntax (may be accepted)",
		},
		{
			name: "unexpected_end_of_stream",
			frontmatterContent: `---
name: Test Workflow
on:
  push:
    branches: [
---`,
			markdownContent: `# Test Workflow
This workflow has unexpected end of stream.`,
			expectError:           true,
			expectedMinLine:       5,
			expectedMinColumn:     14,
			expectedErrorContains: "not found",
			description:           "Unexpected end of stream in YAML",
		},
		{
			name: "invalid_escape_sequence",
			frontmatterContent: `---
name: Test Workflow
description: "Invalid escape: \z"
on: push
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has invalid escape sequence.`,
			expectError:           true,
			expectedMinLine:       3,
			expectedMinColumn:     26,
			expectedErrorContains: "escape",
			description:           "Invalid escape sequence in YAML string",
		},
		{
			name: "mixed_tab_and_space_indentation",
			frontmatterContent: `---
name: Test Workflow
on:
  push:
	branches:
	  - main
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has mixed tab and space indentation.`,
			expectError:           true, // goccy actually does catch this error
			expectedMinLine:       5,
			expectedMinColumn:     1,
			expectedErrorContains: "cannot start any token",
			description:           "Mixed tab and space indentation in YAML",
		},
		{
			name: "anchor_without_alias",
			frontmatterContent: `---
name: Test Workflow
defaults: &default_settings
  timeout: 30
on: push
job1: *missing_anchor
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has anchor without alias.`,
			expectError:           true,
			expectedMinLine:       6,
			expectedMinColumn:     7,
			expectedErrorContains: "alias",
			description:           "Reference to undefined YAML anchor",
		},
		{
			name: "complex_nested_structure_error",
			frontmatterContent: `---
name: Test Workflow
on:
  push:
    branches:
      - main
    paths:
      - "src/**"
  pull_request:
    types: [opened, synchronize
    branches: [main]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has complex nested structure error.`,
			expectError:           true,
			expectedMinLine:       10,
			expectedMinColumn:     1,
			expectedErrorContains: "must be specified",
			description:           "Complex nested structure with missing closing bracket",
		},
		{
			name: "invalid_multiline_string",
			frontmatterContent: `---
name: Test Workflow
description: |
  This is a multiline
  description that has
invalid_key: value
on: push
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow has invalid multiline string.`,
			expectError:           false, // This may actually parse successfully with literal block
			expectedMinLine:       0,
			expectedMinColumn:     0,
			expectedErrorContains: "",
			description:           "Invalid multiline string structure in YAML (may be accepted)",
		},
		{
			name: "schema_validation_error_unknown_field",
			frontmatterContent: `---
name: Test Workflow
on: push
unknown_field: value
invalid_permissions: write
permissions: read-all
---`,
			markdownContent: `# Test Workflow
This workflow may have schema validation errors.`,
			expectError:           false, // This might not be a YAML syntax error but a schema error
			expectedMinLine:       0,
			expectedMinColumn:     0,
			expectedErrorContains: "",
			description:           "Schema validation error with unknown fields (may not cause parse error)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file for testing
			tempDir, err := os.MkdirTemp("", "frontmatter_syntax_test_*")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Write test file with frontmatter and markdown content
			testFile := filepath.Join(tempDir, "test.md")
			fullContent := tt.frontmatterContent + "\n\n" + tt.markdownContent
			if err := os.WriteFile(testFile, []byte(fullContent), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Attempt to parse frontmatter
			result, err := ExtractFrontmatterFromContent(fullContent)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for %s, but parsing succeeded", tt.description)
					return
				}

				// Extract error location information
				line, column, message := ExtractYAMLError(err, 2) // Frontmatter starts at line 2

				// Verify error location is reasonable
				if line > 0 && line < tt.expectedMinLine {
					t.Errorf("Expected line >= %d, got %d for %s", tt.expectedMinLine, line, tt.description)
				}

				if column > 0 && tt.expectedMinColumn > 0 && column < tt.expectedMinColumn {
					t.Errorf("Expected column >= %d, got %d for %s", tt.expectedMinColumn, column, tt.description)
				}

				// Verify error message contains expected content
				if tt.expectedErrorContains != "" && !strings.Contains(strings.ToLower(message), strings.ToLower(tt.expectedErrorContains)) {
					t.Errorf("Expected error message to contain '%s', got '%s' for %s", tt.expectedErrorContains, message, tt.description)
				}

				// Log detailed error information for debugging
				t.Logf("✓ %s: Line %d, Column %d, Error: %s", tt.description, line, column, message)

				// Verify that console error formatting works
				compilerError := console.CompilerError{
					Position: console.ErrorPosition{
						File:   "test.md",
						Line:   line,
						Column: column,
					},
					Type:    "error",
					Message: fmt.Sprintf("frontmatter parsing failed: %s", message),
				}

				formattedError := console.FormatError(compilerError)
				if formattedError == "" {
					t.Errorf("Console error formatting failed for %s", tt.description)
				}

			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.description, err)
					return
				}

				if result == nil {
					t.Errorf("Expected successful parsing result for %s", tt.description)
					return
				}

				t.Logf("✓ %s: Successfully parsed (no syntax error as expected)", tt.description)
			}
		})
	}
}

// TestFrontmatterParsingWithRealGoccyErrors tests frontmatter parsing with actual goccy/go-yaml errors
func TestFrontmatterParsingWithRealGoccyErrors(t *testing.T) {
	tests := []struct {
		name                  string
		yamlContent           string
		expectPreciseLocation bool
		description           string
	}{
		{
			name: "real_mapping_error",
			yamlContent: `name: Test
on: push
invalid syntax here
permissions: read`,
			expectPreciseLocation: true,
			description:           "Real mapping syntax error that goccy should catch with precise location",
		},
		{
			name: "real_indentation_error",
			yamlContent: `name: Test
on:
  push:
    branches:
  invalid_indent: here
permissions: read`,
			expectPreciseLocation: false, // This may actually parse successfully
			description:           "Real indentation error that may not cause parse error",
		},
		{
			name: "real_array_error",
			yamlContent: `name: Test
on:
  push:
    branches: [main, dev, feature/test
permissions: read`,
			expectPreciseLocation: true,
			description:           "Real array syntax error that goccy should catch with precise location",
		},
		{
			name: "real_string_error",
			yamlContent: `name: "Unterminated string
on: push
permissions: read`,
			expectPreciseLocation: true,
			description:           "Real string syntax error that goccy should catch with precise location",
		},
		{
			name: "real_complex_structure_error",
			yamlContent: `name: Test
on:
  workflow_dispatch:
    inputs:
      version:
        description: 'Version to deploy'
        required: true
        default: 'latest'
        type: string
      environment:
        description: 'Environment'
        required: true
        default: 'staging'
        type: choice
        options: [staging, production
jobs:
  deploy:
    runs-on: ubuntu-latest`,
			expectPreciseLocation: true,
			description:           "Real complex structure error that goccy should catch with precise location",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create full frontmatter content
			fullContent := "---\n" + tt.yamlContent + "\n---\n\n# Test\nContent here."

			// Attempt to parse frontmatter
			_, err := ExtractFrontmatterFromContent(fullContent)

			if err == nil {
				if tt.expectPreciseLocation {
					t.Errorf("Expected parsing to fail for %s", tt.description)
					return
				} else {
					t.Logf("✓ %s: Parsed successfully (may not be an error)", tt.description)
					return
				}
			}

			// Extract error location using our goccy parser
			line, column, message := ExtractYAMLError(err, 2) // Frontmatter starts at line 2

			t.Logf("Goccy Error for %s:", tt.description)
			t.Logf("  Original Error: %s", err.Error())
			t.Logf("  Parsed Location: Line %d, Column %d", line, column)
			t.Logf("  Parsed Message: %s", message)

			if tt.expectPreciseLocation {
				// Verify we got a reasonable line and column
				if line < 2 { // Should be at least at frontmatter start
					t.Errorf("Expected line >= 2, got %d for %s", line, tt.description)
				}

				if column <= 0 {
					t.Errorf("Expected column > 0, got %d for %s", column, tt.description)
				}

				if message == "" {
					t.Errorf("Expected non-empty message for %s", tt.description)
				}

				// Verify that we're getting goccy's native format, not fallback parsing
				if strings.Contains(err.Error(), "[") && strings.Contains(err.Error(), "]") {
					t.Logf("✓ Using goccy native [line:column] format for %s", tt.description)
				} else {
					t.Logf("ℹ Using fallback string parsing for %s", tt.description)
				}
			}
		})
	}
}

// TestFrontmatterErrorContextExtraction tests that we extract good context for error reporting
func TestFrontmatterErrorContextExtraction(t *testing.T) {
	content := `---
name: Test Workflow
on:
  push:
    branches: [main, dev
  pull_request:
    types: [opened]
permissions: read-all
jobs:
  test:
    runs-on: ubuntu-latest
---

# Test Workflow

This is a test workflow with a syntax error in the frontmatter.
The error is on line 5 where there's an unclosed bracket.`

	result, err := ExtractFrontmatterFromContent(content)

	if err == nil {
		t.Fatal("Expected parsing to fail due to syntax error")
	}

	// Extract error information
	line, column, message := ExtractYAMLError(err, 2)

	if line <= 2 {
		t.Errorf("Expected error line > 2, got %d", line)
	}

	if column <= 0 {
		t.Errorf("Expected error column > 0, got %d", column)
	}

	// Verify we have frontmatter lines for context
	if result != nil && len(result.FrontmatterLines) > 0 {
		t.Logf("✓ Frontmatter context available with %d lines", len(result.FrontmatterLines))
	} else {
		t.Log("ℹ No frontmatter context available (expected for parse errors)")
	}

	// Create console error format
	compilerError := console.CompilerError{
		Position: console.ErrorPosition{
			File:   "test.md",
			Line:   line,
			Column: column,
		},
		Type:    "error",
		Message: fmt.Sprintf("frontmatter parsing failed: %s", message),
	}

	// Test that error formatting works
	formattedError := console.FormatError(compilerError)
	if formattedError == "" {
		t.Error("Error formatting failed")
	} else {
		t.Logf("✓ Formatted error:\n%s", formattedError)
	}

	t.Logf("Error details: Line %d, Column %d, Message: %s", line, column, message)
}

// TestFrontmatterSyntaxErrorBoundaryConditions tests edge cases and boundary conditions
func TestFrontmatterSyntaxErrorBoundaryConditions(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		description string
	}{
		{
			name: "minimal_invalid_frontmatter",
			content: `---
:
---

# Content`,
			expectError: true,
			description: "Minimal invalid frontmatter with just a colon",
		},
		{
			name: "empty_frontmatter_with_error",
			content: `---
---

# Content`,
			expectError: false,
			description: "Empty frontmatter should not cause parse error",
		},
		{
			name: "very_long_line_with_error",
			content: `---
name: Test
very_long_line_with_error: ` + strings.Repeat("a", 1000) + ` invalid: syntax
permissions: read-all
---

# Content`,
			expectError: true,
			description: "Very long line with syntax error",
		},
		{
			name: "unicode_content_with_error",
			content: `---
name: "测试工作流 🚀"
description: "这是一个测试"
invalid_syntax_here
on: push
permissions: read-all
---

# 测试内容

这里是 markdown 内容。`,
			expectError: true,
			description: "Unicode content with syntax error",
		},
		{
			name: "deeply_nested_error",
			content: `---
name: Test
jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu, windows]
        node: [14, 16, 18]
        include:
          - os: ubuntu
            node: 20
            special: true
        exclude:
          - os: windows
            node: 14
            invalid syntax here
permissions: read-all
---

# Content`,
			expectError: true,
			description: "Deeply nested structure with syntax error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ExtractFrontmatterFromContent(tt.content)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for %s", tt.description)
					return
				}

				line, column, message := ExtractYAMLError(err, 2)
				t.Logf("✓ %s: Line %d, Column %d, Error: %s", tt.description, line, column, message)
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.description, err)
				} else {
					t.Logf("✓ %s: Parsed successfully as expected", tt.description)
				}
			}
		})
	}
}
