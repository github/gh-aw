//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJSweepWorkflowConfiguration validates that the jsweep workflow is properly configured
// to process a single JavaScript file with TypeScript validation and prettier formatting.
func TestJSweepWorkflowConfiguration(t *testing.T) {
	// Read the jsweep.md file
	jsweepPath := filepath.Join("..", "..", ".github", "workflows", "jsweep.md")
	content, err := os.ReadFile(jsweepPath)
	if err != nil {
		t.Fatalf("Failed to read jsweep.md: %v", err)
	}

	mdContent := string(content)

	// Test 1: Verify the workflow processes one file, not three
	t.Run("ProcessesSingleFile", func(t *testing.T) {
		if !strings.Contains(mdContent, "one .cjs file per day") {
			t.Error("jsweep workflow should process one .cjs file per day")
		}
		if strings.Contains(mdContent, "three .cjs files per day") {
			t.Error("jsweep workflow should not process three files")
		}
		// Check for "one file" in either Priority 1 or Priority 2
		if !strings.Contains(mdContent, "one file") {
			t.Error("jsweep workflow should pick one file")
		}
		if strings.Contains(mdContent, "Pick the **three files**") {
			t.Error("jsweep workflow should not pick three files")
		}
	})

	// Test 2: Verify TypeScript validation is configured
	t.Run("TypeScriptValidation", func(t *testing.T) {
		if !strings.Contains(mdContent, "npm run typecheck") {
			t.Error("jsweep workflow should include TypeScript validation with 'npm run typecheck'")
		}
		if !strings.Contains(mdContent, "verify no type errors") {
			t.Error("jsweep workflow should verify no type errors")
		}
		if !strings.Contains(mdContent, "type safety") {
			t.Error("jsweep workflow should mention type safety")
		}
	})

	// Test 3: Verify prettier formatting is configured
	t.Run("PrettierFormatting", func(t *testing.T) {
		if !strings.Contains(mdContent, "npm run format:cjs") {
			t.Error("jsweep workflow should include prettier formatting with 'npm run format:cjs'")
		}
		if !strings.Contains(mdContent, "ensure consistent formatting") {
			t.Error("jsweep workflow should ensure consistent formatting")
		}
		if !strings.Contains(mdContent, "prettier") {
			t.Error("jsweep workflow should mention prettier")
		}
	})

	// Test 4: Verify the PR title format is correct for single file
	t.Run("PRTitleFormat", func(t *testing.T) {
		if !strings.Contains(mdContent, "Title: `[jsweep] Clean <filename>`") {
			t.Error("jsweep workflow should have PR title format for single file: [jsweep] Clean <filename>")
		}
		if strings.Contains(mdContent, "Clean <file1>, <file2>, <file3>") {
			t.Error("jsweep workflow should not have PR title format for three files")
		}
	})

	// Test 5: Verify the workflow runs tests
	t.Run("RunsTests", func(t *testing.T) {
		if !strings.Contains(mdContent, "npm run test:js") {
			t.Error("jsweep workflow should run JavaScript tests with 'npm run test:js'")
		}
		if !strings.Contains(mdContent, "verify all tests pass") {
			t.Error("jsweep workflow should verify all tests pass")
		}
	})

	// Test 6: Verify testing requirements
	t.Run("TestingRequirements", func(t *testing.T) {
		if !strings.Contains(mdContent, "Testing is NOT optional") {
			t.Error("jsweep workflow should specify that testing is not optional")
		}
		if !strings.Contains(mdContent, "the file must have comprehensive test coverage") {
			t.Error("jsweep workflow should require comprehensive test coverage for the file")
		}
		if strings.Contains(mdContent, "every file must have comprehensive test coverage") {
			t.Error("jsweep workflow should refer to 'the file' (singular) not 'every file'")
		}
	})

	// Test 7: Verify the workflow description
	t.Run("WorkflowDescription", func(t *testing.T) {
		if !strings.Contains(mdContent, "description: Daily JavaScript unbloater that cleans one .cjs file per day") {
			t.Error("jsweep workflow description should specify 'one .cjs file per day'")
		}
		if strings.Contains(mdContent, "description: Daily JavaScript unbloater that cleans three .cjs files per day") {
			t.Error("jsweep workflow description should not specify 'three .cjs files per day'")
		}
	})

	// Test 8: Verify the workflow prioritizes files with @ts-nocheck
	t.Run("PrioritizesTsNocheck", func(t *testing.T) {
		if !strings.Contains(mdContent, "Priority 1") {
			t.Error("jsweep workflow should have Priority 1 for file selection")
		}
		if !strings.Contains(mdContent, "@ts-nocheck") {
			t.Error("jsweep workflow should mention @ts-nocheck")
		}
		if !strings.Contains(mdContent, "these need type checking enabled") {
			t.Error("jsweep workflow should explain why @ts-nocheck files are prioritized")
		}
	})

	// Test 9: Verify the workflow has instructions to remove @ts-nocheck
	t.Run("RemovesTsNocheck", func(t *testing.T) {
		if !strings.Contains(mdContent, "Remove `@ts-nocheck`") {
			t.Error("jsweep workflow should have instructions to remove @ts-nocheck")
		}
		if !strings.Contains(mdContent, "Replace it with `@ts-check`") {
			t.Error("jsweep workflow should instruct replacing @ts-nocheck with @ts-check")
		}
		if !strings.Contains(mdContent, "Fix type errors") {
			t.Error("jsweep workflow should mention fixing type errors")
		}
	})

	// Test 10: Verify the workflow has a valid lock file
	t.Run("HasValidLockFile", func(t *testing.T) {
		lockPath := filepath.Join("..", "..", ".github", "workflows", "jsweep.lock.yml")
		_, err := os.Stat(lockPath)
		if err != nil {
			t.Errorf("jsweep.lock.yml should exist and be accessible: %v", err)
		}
	})
}

// TestJSweepWorkflowLockFile validates that the compiled jsweep.lock.yml file
// uses runtime-import to reference the original workflow file
func TestJSweepWorkflowLockFile(t *testing.T) {
	// Read the jsweep.lock.yml file
	lockPath := filepath.Join("..", "..", ".github", "workflows", "jsweep.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read jsweep.lock.yml: %v", err)
	}

	lockStr := string(lockContent)

	// Verify the lock file uses runtime-import (jsweep has no imports)
	if !strings.Contains(lockStr, "{{#runtime-import") {
		t.Error("jsweep lock file should use runtime-import (workflow has no imports)")
	}

	if !strings.Contains(lockStr, "jsweep.md") {
		t.Error("Runtime-import should reference jsweep.md")
	}

	// For runtime-import workflows, the content is in the original .md file
	// Read the source workflow file to verify the content
	mdPath := filepath.Join("..", "..", ".github", "workflows", "jsweep.md")
	mdContent, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("Failed to read jsweep.md: %v", err)
	}

	mdStr := string(mdContent)

	// Test 1: Verify the workflow processes one file
	t.Run("CompiledProcessesSingleFile", func(t *testing.T) {
		if !strings.Contains(mdStr, "one .cjs file per day") {
			t.Error("jsweep workflow should process one .cjs file per day")
		}
		if strings.Contains(mdStr, "three .cjs files per day") {
			t.Error("jsweep workflow should not process three files")
		}
	})

	// Test 2: Verify TypeScript validation is in the workflow
	t.Run("CompiledTypeScriptValidation", func(t *testing.T) {
		if !strings.Contains(mdStr, "npm run typecheck") {
			t.Error("jsweep workflow should include TypeScript validation")
		}
	})

	// Test 3: Verify prettier formatting is in the workflow
	t.Run("CompiledPrettierFormatting", func(t *testing.T) {
		if !strings.Contains(mdStr, "npm run format:cjs") {
			t.Error("jsweep workflow should include prettier formatting")
		}
	})

	// Test 4: Verify @ts-nocheck prioritization is in the workflow
	t.Run("CompiledTsNocheckPrioritization", func(t *testing.T) {
		if !strings.Contains(mdStr, "Priority 1") {
			t.Error("jsweep workflow should prioritize files with @ts-nocheck")
		}
		if !strings.Contains(mdStr, "@ts-nocheck") {
			t.Error("jsweep workflow should mention @ts-nocheck")
		}
	})
}
