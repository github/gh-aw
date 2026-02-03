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

// TestManifestRendering tests that imported and included files are correctly rendered
// as comments in the generated lock file
func TestManifestRendering(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-test")

	// Create shared directory
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.Mkdir(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create imported tools file
	toolsFile := filepath.Join(sharedDir, "tools.md")
	toolsContent := `---
on: push
tools:
  github:
    allowed:
      - list_commits
---`
	if err := os.WriteFile(toolsFile, []byte(toolsContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create included instructions file
	instructionsFile := filepath.Join(sharedDir, "instructions.md")
	instructionsContent := `# Shared Instructions

Be helpful and concise.`
	if err := os.WriteFile(instructionsFile, []byte(instructionsContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	tests := []struct {
		name             string
		workflowContent  string
		expectedImports  []string
		expectedIncludes []string
		description      string
	}{
		{
			name: "workflow_with_imports_and_includes",
			workflowContent: `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
imports:
  - shared/tools.md
---

# Test Workflow

@include shared/instructions.md

Handle the issue.`,
			expectedImports:  []string{"shared/tools.md"},
			expectedIncludes: []string{"shared/instructions.md"},
			description:      "Should render both imports and includes in manifest",
		},
		{
			name: "workflow_with_only_imports",
			workflowContent: `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
imports:
  - shared/tools.md
---

# Test Workflow

Handle the issue.`,
			expectedImports:  []string{"shared/tools.md"},
			expectedIncludes: nil,
			description:      "Should render only imports in manifest",
		},
		{
			name: "workflow_with_only_includes",
			workflowContent: `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
---

# Test Workflow

@include shared/instructions.md

Handle the issue.`,
			expectedImports:  nil,
			expectedIncludes: []string{"shared/instructions.md"},
			description:      "Should render only includes in manifest",
		},
		{
			name: "workflow_without_imports_or_includes",
			workflowContent: `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
---

# Test Workflow

Handle the issue.`,
			expectedImports:  nil,
			expectedIncludes: nil,
			description:      "Should not render manifest section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflowContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Unexpected error compiling workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read generated lock file: %v", err)
			}

			lockContent := string(content)

			if tt.expectedImports == nil && tt.expectedIncludes == nil {
				// Verify no manifest section is present
				if strings.Contains(lockContent, "# Resolved workflow manifest:") {
					t.Errorf("%s: Expected no manifest section but found one", tt.description)
				}
			} else {
				// Verify manifest section exists
				if !strings.Contains(lockContent, "# Resolved workflow manifest:") {
					t.Errorf("%s: Expected manifest section but none found", tt.description)
				}

				// Verify imports section if expected
				if tt.expectedImports != nil {
					if !strings.Contains(lockContent, "#   Imports:") {
						t.Errorf("%s: Expected Imports section but none found", tt.description)
					}
					for _, importFile := range tt.expectedImports {
						expectedLine := "#     - " + importFile
						if !strings.Contains(lockContent, expectedLine) {
							t.Errorf("%s: Expected import line '%s' but not found", tt.description, expectedLine)
						}
					}
				}

				// Verify includes section if expected
				if tt.expectedIncludes != nil {
					if !strings.Contains(lockContent, "#   Includes:") {
						t.Errorf("%s: Expected Includes section but none found", tt.description)
					}
					for _, includeFile := range tt.expectedIncludes {
						expectedLine := "#     - " + includeFile
						if !strings.Contains(lockContent, expectedLine) {
							t.Errorf("%s: Expected include line '%s' but not found", tt.description, expectedLine)
						}
					}
				}
			}
		})
	}
}

// TestManifestIncludeOrdering tests that included files are rendered in alphabetical order
func TestManifestIncludeOrdering(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-order-test")

	// Create shared directory
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.Mkdir(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create multiple include files with names that would be out of order if not sorted
	includeFiles := []string{
		"zebra.md",
		"apple.md",
		"middle.md",
		"banana.md",
	}

	for _, filename := range includeFiles {
		content := "# " + filename + "\n\nSome content."
		filePath := filepath.Join(sharedDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create workflow that includes all files in non-alphabetical order
	workflowContent := `---
on: issues
engine: claude
---

# Test Workflow

@include shared/zebra.md
@include shared/apple.md
@include shared/middle.md
@include shared/banana.md

Handle the issue.`

	compiler := NewCompiler()
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify manifest section exists
	if !strings.Contains(lockContent, "# Resolved workflow manifest:") {
		t.Fatal("Expected manifest section but none found")
	}

	// Verify includes section exists
	if !strings.Contains(lockContent, "#   Includes:") {
		t.Fatal("Expected Includes section but none found")
	}

	// Extract the includes section and verify alphabetical order
	lines := strings.Split(lockContent, "\n")
	var includeLines []string
	inIncludesSection := false

	for _, line := range lines {
		if strings.Contains(line, "#   Includes:") {
			inIncludesSection = true
			continue
		}
		if inIncludesSection {
			if strings.HasPrefix(line, "#     - ") {
				includeLines = append(includeLines, line)
			} else if !strings.HasPrefix(line, "#") {
				// End of includes section
				break
			}
		}
	}

	// Verify we found all includes
	expectedCount := len(includeFiles)
	if len(includeLines) != expectedCount {
		t.Fatalf("Expected %d include lines, found %d", expectedCount, len(includeLines))
	}

	// Expected order is alphabetical
	expectedOrder := []string{
		"#     - shared/apple.md",
		"#     - shared/banana.md",
		"#     - shared/middle.md",
		"#     - shared/zebra.md",
	}

	for i, expected := range expectedOrder {
		if includeLines[i] != expected {
			t.Errorf("Include line %d: expected %q, got %q", i, expected, includeLines[i])
		}
	}
}
