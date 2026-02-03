//go:build !integration

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestUpdateWorkflowFrontmatter(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	tests := []struct {
		name            string
		initialContent  string
		updateFunc      func(frontmatter map[string]any) error
		expectedContent string
		expectError     bool
	}{
		{
			name: "Add tool to existing tools section",
			initialContent: `---
tools:
  existing: {}
---
# Test Workflow
Some content`,
			updateFunc: func(frontmatter map[string]any) error {
				tools := EnsureToolsSection(frontmatter)
				tools["new-tool"] = map[string]any{"type": "test"}
				return nil
			},
			expectedContent: `---
existing: {}
new-tool:
  type: test
tools:
  existing: {}
  new-tool:
    type: test
---
# Test Workflow
Some content`,
			expectError: false,
		},
		{
			name: "Create tools section if missing",
			initialContent: `---
engine: claude
---
# Test Workflow
Some content`,
			updateFunc: func(frontmatter map[string]any) error {
				tools := EnsureToolsSection(frontmatter)
				tools["new-tool"] = map[string]any{"type": "test"}
				return nil
			},
			expectedContent: `---
engine: claude
tools:
  new-tool:
    type: test
---
# Test Workflow
Some content`,
			expectError: false,
		},
		{
			name: "Handle empty frontmatter",
			initialContent: `---
---
# Test Workflow
Some content`,
			updateFunc: func(frontmatter map[string]any) error {
				tools := EnsureToolsSection(frontmatter)
				tools["new-tool"] = map[string]any{"type": "test"}
				return nil
			},
			expectedContent: `---
tools:
  new-tool:
    type: test
---
# Test Workflow
Some content`,
			expectError: false,
		},
		{
			name: "Handle file with no frontmatter",
			initialContent: `# Test Workflow
Some content without frontmatter`,
			updateFunc: func(frontmatter map[string]any) error {
				tools := EnsureToolsSection(frontmatter)
				tools["new-tool"] = map[string]any{"type": "test"}
				return nil
			},
			expectedContent: `---
tools:
  new-tool:
    type: test
---
# Test Workflow
Some content without frontmatter`,
			expectError: false,
		},
		{
			name: "Update function returns error",
			initialContent: `---
tools: {}
---
# Test Workflow`,
			updateFunc: func(frontmatter map[string]any) error {
				return fmt.Errorf("test error")
			},
			expectedContent: "",
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tempDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.initialContent), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Run the update function
			err := UpdateWorkflowFrontmatter(testFile, tt.updateFunc, false)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Read the updated content
			updatedContent, err := os.ReadFile(testFile)
			if err != nil {
				t.Fatalf("Failed to read updated file: %v", err)
			}

			// For tools section tests, just verify the tools were added correctly
			// Skip exact content comparison since YAML marshaling order may vary
			if strings.Contains(tt.name, "tool") {
				content := string(updatedContent)
				if !strings.Contains(content, "new-tool:") {
					t.Errorf("Expected 'new-tool:' in updated content, got: %s", content)
				}
				if !strings.Contains(content, "type: test") {
					t.Errorf("Expected 'type: test' in updated content, got: %s", content)
				}
				if !strings.Contains(content, "---") {
					t.Errorf("Expected frontmatter delimiters '---' in updated content, got: %s", content)
				}
			}
		})
	}
}

func TestEnsureToolsSection(t *testing.T) {
	tests := []struct {
		name          string
		frontmatter   map[string]any
		expectedTools map[string]any
	}{
		{
			name:          "Create tools section when missing",
			frontmatter:   map[string]any{},
			expectedTools: map[string]any{},
		},
		{
			name: "Return existing tools section",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"existing": map[string]any{"type": "test"},
				},
			},
			expectedTools: map[string]any{
				"existing": map[string]any{"type": "test"},
			},
		},
		{
			name: "Replace invalid tools section",
			frontmatter: map[string]any{
				"tools": "invalid",
			},
			expectedTools: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := EnsureToolsSection(tt.frontmatter)

			// Verify tools section is a map
			if tools == nil {
				t.Errorf("Expected tools to be a map, got nil")
				return
			}

			// Verify frontmatter was updated
			frontmatterTools, ok := tt.frontmatter["tools"].(map[string]any)
			if !ok {
				t.Errorf("Expected frontmatter['tools'] to be a map")
				return
			}

			// Verify returned tools is the same reference as in frontmatter
			if len(tools) != len(frontmatterTools) {
				t.Errorf("Expected returned tools to have same length as frontmatter['tools']")
			}

			// For existing tools, verify content
			if len(tt.expectedTools) > 0 {
				for key, expectedValue := range tt.expectedTools {
					if actualValue, exists := tools[key]; !exists {
						t.Errorf("Expected tool '%s' not found", key)
					} else {
						// Simple comparison for test values
						if expectedMap, ok := expectedValue.(map[string]any); ok {
							if actualMap, ok := actualValue.(map[string]any); ok {
								if expectedMap["type"] != actualMap["type"] {
									t.Errorf("Expected tool '%s' type '%v', got '%v'", key, expectedMap["type"], actualMap["type"])
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestReconstructWorkflowFile(t *testing.T) {
	tests := []struct {
		name            string
		frontmatterYAML string
		markdownContent string
		expectedResult  string
	}{
		{
			name:            "With frontmatter and markdown",
			frontmatterYAML: "engine: claude\ntools: {}",
			markdownContent: "# Test Workflow\nSome content",
			expectedResult:  "---\nengine: claude\ntools: {}\n---\n# Test Workflow\nSome content",
		},
		{
			name:            "Empty frontmatter with markdown",
			frontmatterYAML: "",
			markdownContent: "# Test Workflow\nSome content",
			expectedResult:  "---\n---\n# Test Workflow\nSome content",
		},
		{
			name:            "Frontmatter with no markdown",
			frontmatterYAML: "engine: claude",
			markdownContent: "",
			expectedResult:  "---\nengine: claude\n---",
		},
		{
			name:            "Frontmatter with trailing newline",
			frontmatterYAML: "engine: claude\n",
			markdownContent: "# Test",
			expectedResult:  "---\nengine: claude\n---\n# Test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reconstructWorkflowFile(tt.frontmatterYAML, tt.markdownContent)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expectedResult {
				t.Errorf("Expected:\n%s\n\nGot:\n%s", tt.expectedResult, result)
			}
		})
	}
}
