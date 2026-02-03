//go:build integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
)

func TestCustomStepsIndentation(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "steps-indentation-test")

	tests := []struct {
		name        string
		stepsYAML   string
		description string
	}{
		{
			name: "standard_2_space_indentation",
			stepsYAML: `steps:
  - name: Checkout code
    uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd
  - name: Set up Go
    uses: actions/setup-go@4dc6199c7b1a012772edbd06daecab0f50c9053c
    with:
      go-version-file: go.mod
      cache: true`,
			description: "Standard 2-space indentation should be preserved with 6-space base offset",
		},
		{
			name: "odd_3_space_indentation",
			stepsYAML: `steps:
   - name: Odd indent
     uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd
     with:
       param: value`,
			description: "3-space indentation should be normalized to standard format",
		},
		{
			name: "deep_nesting",
			stepsYAML: `steps:
  - name: Deep nesting
    uses: actions/complex@v1
    with:
      config:
        database:
          host: localhost
          settings:
            timeout: 30`,
			description: "Deep nesting should maintain relative indentation with 6-space base offset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test workflow with the given steps YAML
			testContent := fmt.Sprintf(`---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
%s
engine: claude
strict: false
---

# Test Steps Indentation

%s
`, tt.stepsYAML, tt.description)

			testFile := filepath.Join(tmpDir, fmt.Sprintf("test-%s.md", tt.name))
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			compiler := NewCompiler()

			// Compile the workflow
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Unexpected error compiling workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := filepath.Join(tmpDir, fmt.Sprintf("test-%s.lock.yml", tt.name))
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read generated lock file: %v", err)
			}

			lockContent := string(content)

			// Verify the YAML is valid by parsing it
			var yamlData map[string]any
			if err := yaml.Unmarshal(content, &yamlData); err != nil {
				t.Errorf("Generated YAML is not valid: %v\nContent:\n%s", err, lockContent)
			}

			// Check that custom steps are present and properly indented
			if !strings.Contains(lockContent, "      - name:") {
				t.Errorf("Expected to find properly indented step items (6 spaces) in generated content")
			}

			// Verify step properties have proper indentation (8+ spaces for uses, with, etc.)
			// Only check non-comment lines (frontmatter is embedded as comments)
			lines := strings.Split(lockContent, "\n")
			foundCustomSteps := false
			for i, line := range lines {
				// Skip comment lines
				trimmed := strings.TrimLeft(line, " \t")
				if strings.HasPrefix(trimmed, "#") {
					continue
				}
				// Look for custom step content (not generated workflow infrastructure)
				if strings.Contains(line, "Checkout code") || strings.Contains(line, "Set up Go") ||
					strings.Contains(line, "Odd indent") || strings.Contains(line, "Deep nesting") {
					foundCustomSteps = true
				}

				// Check indentation for lines containing step properties within custom steps section
				if foundCustomSteps && (strings.Contains(line, "uses: actions/") || strings.Contains(line, "with:")) {
					if !strings.HasPrefix(line, "        ") {
						t.Errorf("Step property at line %d should have 8+ spaces indentation: '%s'", i+1, line)
					}
				}
			}

			if !foundCustomSteps {
				t.Error("Expected to find custom steps content in generated workflow")
			}
		})
	}
}

func TestCustomStepsEdgeCases(t *testing.T) {
	tmpDir := testutil.TempDir(t, "steps-edge-cases-test")

	tests := []struct {
		name        string
		stepsYAML   string
		expectError bool
		description string
	}{
		{
			name:        "no_custom_steps",
			stepsYAML:   `# No steps section defined`,
			expectError: false,
			description: "Should use default checkout step when no custom steps defined",
		},
		{
			name:        "empty_steps",
			stepsYAML:   `steps: []`,
			expectError: false,
			description: "Empty steps array should be handled gracefully",
		},
		{
			name:        "steps_with_only_whitespace",
			stepsYAML:   `# No steps defined`,
			expectError: false,
			description: "No steps section should use default steps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := fmt.Sprintf(`---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
%s
engine: claude
strict: false
---

# Test Edge Cases

%s
`, tt.stepsYAML, tt.description)

			testFile := filepath.Join(tmpDir, fmt.Sprintf("test-%s.md", tt.name))
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for test '%s', got nil", tt.name)
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for test '%s': %v", tt.name, err)
			}

			if !tt.expectError {
				// Verify lock file was created and is valid YAML
				lockFile := filepath.Join(tmpDir, fmt.Sprintf("test-%s.lock.yml", tt.name))
				content, err := os.ReadFile(lockFile)
				if err != nil {
					t.Fatalf("Failed to read generated lock file: %v", err)
				}

				var yamlData map[string]any
				if err := yaml.Unmarshal(content, &yamlData); err != nil {
					t.Errorf("Generated YAML is not valid: %v", err)
				}

				// For no custom steps, should contain default checkout
				if tt.name == "no_custom_steps" {
					lockContent := string(content)
					if !strings.Contains(lockContent, "- name: Checkout repository") {
						t.Error("Expected default checkout step when no custom steps defined")
					}
				}
			}
		})
	}
}
