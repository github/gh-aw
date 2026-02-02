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

func TestAccessLogUploadConditional(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name        string
		tools       map[string]any
		mcpServers  map[string]any
		expectSteps bool
	}{
		{
			name: "no tools - no access log steps",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
			},
			expectSteps: false,
		},
		{
			name: "mcp server with container but no network permissions - no access log steps",
			mcpServers: map[string]any{
				"simple": map[string]any{
					"container": "simple/tool",
					"allowed":   []any{"test"},
				},
			},
			expectSteps: false,
		},
		{
			name: "mcp server with container - no access log steps (proxy removed)",
			mcpServers: map[string]any{
				"fetch": map[string]any{
					"container": "mcp/fetch",
					"allowed":   []any{"fetch"},
				},
			},
			expectSteps: false, // Changed from true - per-tool proxy removed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder

			// Combine tools and mcpServers for testing
			testTools := tt.tools
			if testTools == nil {
				testTools = make(map[string]any)
			}
			if tt.mcpServers != nil {
				// Add mcp servers to tools map for the test
				for name, config := range tt.mcpServers {
					testTools[name] = config
				}
			}

			// Test generateExtractAccessLogs
			compiler.generateExtractAccessLogs(&yaml, testTools)
			extractContent := yaml.String()

			// Test generateUploadAccessLogs
			yaml.Reset()
			compiler.generateUploadAccessLogs(&yaml, testTools)
			uploadContent := yaml.String()

			hasExtractStep := strings.Contains(extractContent, "name: Extract squid access logs")
			hasUploadStep := strings.Contains(uploadContent, "name: Upload squid access logs")

			if tt.expectSteps {
				if !hasExtractStep {
					t.Errorf("Expected extract step to be generated but it wasn't")
				}
				if !hasUploadStep {
					t.Errorf("Expected upload step to be generated but it wasn't")
				}
			} else {
				if hasExtractStep {
					t.Errorf("Expected no extract step but one was generated")
				}
				if hasUploadStep {
					t.Errorf("Expected no upload step but one was generated")
				}
			}
		})
	}
}

// TestPullRequestForksArrayFilter tests the pull_request forks: []string filter functionality with glob support
func TestPostStepsIndentationFix(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "post-steps-indentation-test")

	// Test case with various post-steps configurations
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
post-steps:
  - name: First Post Step
    run: echo "first"
  - name: Second Post Step
    uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f
    with:
      name: test-artifact
      path: test-file.txt
      retention-days: 7
  - name: Third Post Step
    if: success()
    run: |
      echo "multiline"
      echo "script"
engine: claude
strict: false
---

# Test Post Steps Indentation

Test post-steps indentation fix.
`

	testFile := filepath.Join(tmpDir, "test-post-steps-indentation.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-post-steps-indentation.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify all post-steps are present
	if !strings.Contains(lockContent, "- name: First Post Step") {
		t.Error("Expected post-step 'First Post Step' to be in generated workflow")
	}
	if !strings.Contains(lockContent, "- name: Second Post Step") {
		t.Error("Expected post-step 'Second Post Step' to be in generated workflow")
	}
	// Note: "Third Post Step" has an 'if' condition, so it appears as "name: Third Post Step" not "- name:"
	if !strings.Contains(lockContent, "name: Third Post Step") {
		t.Error("Expected post-step 'Third Post Step' to be in generated workflow")
	}

	// Verify indentation is correct (6 spaces for list items, 8 for properties)
	// Only check non-comment lines (frontmatter is embedded as comments)
	lines := strings.Split(lockContent, "\n")
	for i, line := range lines {
		// Skip comment lines
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, "- name: First Post Step") {
			// Check that this line has exactly 6 leading spaces
			if !strings.HasPrefix(line, "      - name: First Post Step") {
				t.Errorf("Line %d: Expected 6 spaces before '- name: First Post Step', got: %q", i+1, line)
			}
			// Check the next non-comment line (run:) has 8 spaces
			for j := i + 1; j < len(lines); j++ {
				nextTrimmed := strings.TrimLeft(lines[j], " \t")
				if strings.HasPrefix(nextTrimmed, "#") {
					continue
				}
				nextLine := lines[j]
				if strings.Contains(nextLine, "run:") && !strings.HasPrefix(nextLine, "        run:") {
					t.Errorf("Line %d: Expected 8 spaces before 'run:', got: %q", j+1, nextLine)
				}
				break
			}
		}
		if strings.Contains(line, "- name: Second Post Step") {
			// Check that this line has exactly 6 leading spaces
			if !strings.HasPrefix(line, "      - name: Second Post Step") {
				t.Errorf("Line %d: Expected 6 spaces before '- name: Second Post Step', got: %q", i+1, line)
			}
			// Check subsequent non-comment lines have correct indentation
			checkIdx := 0
			for j := i + 1; j < len(lines) && checkIdx < 2; j++ {
				nextTrimmed := strings.TrimLeft(lines[j], " \t")
				if strings.HasPrefix(nextTrimmed, "#") {
					continue
				}
				if checkIdx == 0 && strings.Contains(lines[j], "uses:") {
					if !strings.HasPrefix(lines[j], "        uses:") {
						t.Errorf("Line %d: Expected 8 spaces before 'uses:', got: %q", j+1, lines[j])
					}
					checkIdx++
				} else if checkIdx == 1 && strings.Contains(lines[j], "with:") {
					if !strings.HasPrefix(lines[j], "        with:") {
						t.Errorf("Line %d: Expected 8 spaces before 'with:', got: %q", j+1, lines[j])
					}
					checkIdx++
				}
			}
		}
	}

	t.Log("Post-steps indentation verified successfully")
}

func TestPromptUploadArtifact(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "prompt-upload-test")

	// Create a test markdown file with basic frontmatter
	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
---

# Test Prompt Upload

This workflow should generate a unified artifact upload step that includes the prompt.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockYAML := string(lockContent)

	// Verify that the unified artifact upload step is present
	if !strings.Contains(lockYAML, "- name: Upload agent artifacts") {
		t.Error("Expected 'Upload agent artifacts' step to be in generated workflow")
	}

	// Verify the upload step uses the correct action
	if !strings.Contains(lockYAML, "uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f") {
		t.Error("Expected 'actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f' action to be used")
	}

	// Verify the unified artifact name
	if !strings.Contains(lockYAML, "name: agent-artifacts") {
		t.Error("Expected artifact name to be 'agent-artifacts'")
	}

	// Verify the prompt path is included in the unified upload
	if !strings.Contains(lockYAML, "/tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Error("Expected prompt path '/tmp/gh-aw/aw-prompts/prompt.txt' to be in unified upload")
	}

	// Verify the upload step has the if-no-files-found configuration set to ignore
	if !strings.Contains(lockYAML, "if-no-files-found: ignore") {
		t.Error("Expected 'if-no-files-found: ignore' in upload step")
	}

	// Verify the upload step runs always (with if: always())
	uploadStepIndex := strings.Index(lockYAML, "- name: Upload agent artifacts")
	if uploadStepIndex == -1 {
		t.Fatal("Upload agent artifacts step not found")
	}

	// Check for "if: always()" in the section after the upload step name
	afterUploadStep := lockYAML[uploadStepIndex:]
	nextStepIndex := strings.Index(afterUploadStep[20:], "- name:")
	if nextStepIndex == -1 {
		nextStepIndex = len(afterUploadStep) - 20
	}
	uploadStepSection := afterUploadStep[:20+nextStepIndex]

	if !strings.Contains(uploadStepSection, "if: always()") {
		t.Error("Expected 'if: always()' in upload agent artifacts step")
	}

	// Verify continue-on-error is set
	if !strings.Contains(uploadStepSection, "continue-on-error: true") {
		t.Error("Expected 'continue-on-error: true' in upload agent artifacts step")
	}

	t.Log("Unified artifact upload step verified successfully (includes prompt)")
}
