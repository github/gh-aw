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

// TestManifestIncludePathRelativeToRepoRoot verifies that included files in sibling
// .github/ subdirectories (e.g. .github/shared/ when the workflow is in .github/workflows/)
// are recorded with a repo-root-relative path instead of an absolute path.
func TestManifestIncludePathRelativeToRepoRoot(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-sibling-test")

	// Create .github/workflows/ and .github/shared/ structure
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(tmpDir, ".github", "shared")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create an include file in .github/shared/ (sibling of .github/workflows/)
	editorialFile := filepath.Join(sharedDir, "editorial.md")
	editorialContent := `## Writing Style

Write in a newspaper editorial tone.`
	if err := os.WriteFile(editorialFile, []byte(editorialContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create workflow that includes the file via .github/-prefixed path
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Test Workflow

{{#import: .github/shared/editorial.md}}

Handle the issue.`

	compiler := NewCompiler()
	testFile := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// The Includes section should show .github/shared/editorial.md (relative to repo root),
	// NOT an absolute path like /tmp/.../.../.github/shared/editorial.md
	expectedLine := "#     - .github/shared/editorial.md"
	if !strings.Contains(lockContent, expectedLine) {
		t.Errorf("Expected relative include path %q in lock file, but not found.\nLock file content excerpt:\n%s",
			expectedLine, extractLockFileHeader(lockContent))
	}

	// Verify no absolute path appears in the Includes section
	for line := range strings.SplitSeq(lockContent, "\n") {
		if strings.HasPrefix(line, "#     - /") {
			t.Errorf("Found absolute path in lock file Includes section: %q", line)
		}
	}
}

// extractLockFileHeader returns the first 50 lines of a lock file for test diagnostics.
func extractLockFileHeader(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 50 {
		lines = lines[:50]
	}
	return strings.Join(lines, "\n")
}

// TestBodyLevelRuntimeImportPromotedToMacro verifies that a body-level {{#runtime-import}} directive
// in the workflow markdown generates an explicit {{#runtime-import}} macro in the compiled lock-file prompt,
// making the imported content visible without having to chase the workflow file at runtime.
func TestBodyLevelRuntimeImportPromotedToMacro(t *testing.T) {
	tmpDir := testutil.TempDir(t, "body-import-test")

	// Create .github/workflows/ and .github/shared/ structure
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(tmpDir, ".github", "shared")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create the shared editorial file
	editorialFile := filepath.Join(sharedDir, "editorial.md")
	if err := os.WriteFile(editorialFile, []byte("## Writing Style\n\nNewspaper editorial tone.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Workflow uses {{#runtime-import}} directly (preferred form)
	workflowContent := `---
on:
  schedule:
    - cron: "0 9 * * *"
permissions:
  contents: read
  issues: read
engine: copilot
safe-outputs:
  create-issue: {}
---

{{#runtime-import .github/shared/editorial.md}}

# Daily Report

Generate the daily report.`

	compiler := NewCompiler()
	testFile := filepath.Join(workflowsDir, "daily-report.md")
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := promptRenderPlan(t, string(content))

	// Fragment imports retain their order before the compiler-selected author source.
	expectedEditorialMacro := "{{#runtime-import .github/shared/editorial.md}}"
	if !strings.Contains(lockContent, expectedEditorialMacro) {
		t.Errorf("Expected %q in compiled lock file prompt, but not found.\nContent excerpt:\n%s",
			expectedEditorialMacro, extractLockFileHeader(lockContent))
	}

	// The main workflow file must still be imported after the editorial import
	expectedMainMacro := "{{#author-prompt .github/workflows/daily-report.md}}"
	if !strings.Contains(lockContent, expectedMainMacro) {
		t.Errorf("Expected %q in compiled lock file prompt, but not found", expectedMainMacro)
	}

	// editorial macro must come before the main workflow macro
	editorialIdx := strings.Index(lockContent, expectedEditorialMacro)
	mainIdx := strings.Index(lockContent, expectedMainMacro)
	if editorialIdx >= mainIdx {
		t.Errorf("Expected editorial import (%d) to appear before main workflow import (%d)", editorialIdx, mainIdx)
	}
}
