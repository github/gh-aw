//go:build !integration

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// TestWatchAndCompileWorkflows tests the watchAndCompileWorkflows function
// This covers pkg/cli/commands.go:644
func TestWatchAndCompileWorkflows(t *testing.T) {
	t.Run("watch function requires git repository", func(t *testing.T) {
		// Create a temporary directory without git
		tempDir := testutil.TempDir(t, "test-*")
		oldDir, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(oldDir)

		compiler := workflow.NewCompiler()

		err := watchAndCompileWorkflows("", compiler, false)
		if err == nil {
			t.Error("watchAndCompileWorkflows should require git repository")
		}

		if !strings.Contains(err.Error(), "watch mode requires being in a git repository") {
			t.Errorf("Expected git repository error, got: %v", err)
		}
	})

	t.Run("watch function requires workflows directory", func(t *testing.T) {
		// Create a git repository without workflows directory
		tempDir := testutil.TempDir(t, "test-*")
		oldDir, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(oldDir)

		// Initialize git repository
		initErr := initTestGitRepo(tempDir)
		if initErr != nil {
			t.Fatalf("Failed to init git repo: %v", initErr)
		}

		compiler := workflow.NewCompiler()

		err := watchAndCompileWorkflows("", compiler, false)
		if err == nil {
			t.Error("watchAndCompileWorkflows should require .github/workflows directory")
		}

		if !strings.Contains(err.Error(), ".github/workflows directory does not exist") {
			t.Errorf("Expected workflows directory error, got: %v", err)
		}
	})

	t.Run("watch function checks specific file exists", func(t *testing.T) {
		// Create a git repository with workflows directory
		tempDir := testutil.TempDir(t, "test-*")
		oldDir, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(oldDir)

		// Initialize git repository and workflows directory
		initErr := initTestGitRepo(tempDir)
		if initErr != nil {
			t.Fatalf("Failed to init git repo: %v", initErr)
		}
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		compiler := workflow.NewCompiler()

		err := watchAndCompileWorkflows("nonexistent.md", compiler, false)
		if err == nil {
			t.Error("watchAndCompileWorkflows should error for nonexistent specific file")
		}

		if !strings.Contains(err.Error(), "specified markdown file does not exist") {
			t.Errorf("Expected file not found error, got: %v", err)
		}
	})

	t.Run("watch function setup with valid directory", func(t *testing.T) {
		// Create a git repository with workflows directory
		tempDir := testutil.TempDir(t, "test-*")
		oldDir, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(oldDir)

		// Initialize git repository and workflows directory
		initErr := initTestGitRepo(tempDir)
		if initErr != nil {
			t.Fatalf("Failed to init git repo: %v", initErr)
		}
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create a test workflow file
		testFile := filepath.Join(workflowsDir, "test.md")
		os.WriteFile(testFile, []byte("# Test Workflow\n\nTest content"), 0644)

		compiler := &workflow.Compiler{}

		// Test that function can be set up (we'll use a context to cancel quickly)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Run in a goroutine so we can control it with context
		done := make(chan error, 1)
		go func() {
			done <- watchAndCompileWorkflows("test.md", compiler, true)
		}()

		select {
		case watchErr := <-done:
			// If it returns an error quickly, check that it's not a setup error
			if watchErr != nil && !strings.Contains(watchErr.Error(), "context") && !strings.Contains(watchErr.Error(), "interrupt") {
				t.Errorf("Unexpected error in watch setup: %v", watchErr)
			}
		case <-ctx.Done():
			// This is expected - the function should be running and waiting for file changes
			// The timeout means the setup worked and it's watching
		}
	})
}

// TestCompileAllWorkflowFiles tests the compileAllWorkflowFiles function
// This covers pkg/cli/commands.go:790
func TestCompileAllWorkflowFiles(t *testing.T) {
	t.Run("compile all with no markdown files", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		compiler := &workflow.Compiler{}

		stats, err := compileAllWorkflowFiles(compiler, workflowsDir, true)
		if err != nil {
			t.Errorf("compileAllWorkflowFiles should handle empty directory: %v", err)
		}
		if stats.Total != 0 {
			t.Errorf("Expected 0 total files, got %d", stats.Total)
		}
	})

	t.Run("compile all with markdown files", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create test markdown files
		testFiles := []string{"test1.md", "test2.md", "test3.md"}
		for _, file := range testFiles {
			filePath := filepath.Join(workflowsDir, file)
			content := fmt.Sprintf("---\non: push\nengine: claude\n---\n# %s\n\nTest workflow content", strings.TrimSuffix(file, ".md"))
			os.WriteFile(filePath, []byte(content), 0644)
		}

		// Create a basic compiler
		compiler := workflow.NewCompiler()

		stats, err := compileAllWorkflowFiles(compiler, workflowsDir, true)
		if err != nil {
			t.Errorf("compileAllWorkflowFiles failed: %v", err)
		}

		// Check stats
		if stats.Total != len(testFiles) {
			t.Errorf("Expected %d total files, got %d", len(testFiles), stats.Total)
		}

		// Check that lock files were created
		for _, file := range testFiles {
			lockFile := filepath.Join(workflowsDir, stringutil.MarkdownToLockFile(file))
			if _, statErr := os.Stat(lockFile); os.IsNotExist(statErr) {
				t.Errorf("Expected lock file %s to be created", lockFile)
			}
		}
	})

	t.Run("compile all handles glob error", func(t *testing.T) {
		// Use a malformed glob pattern that will cause filepath.Glob to error
		invalidDir := "/tmp/gh-aw/[invalid"

		compiler := &workflow.Compiler{}

		_, err := compileAllWorkflowFiles(compiler, invalidDir, false)
		if err == nil {
			t.Error("compileAllWorkflowFiles should handle glob errors")
		}

		if !strings.Contains(err.Error(), "failed to find markdown files") {
			t.Errorf("Expected glob error, got: %v", err)
		}
	})

	t.Run("compile all with compilation errors", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create an invalid markdown file (malformed YAML)
		invalidFile := filepath.Join(workflowsDir, "invalid.md")
		invalidContent := "---\nmalformed: yaml: content:\n  - missing\n    proper: structure\n---\n# Invalid\n\nThis should fail"
		os.WriteFile(invalidFile, []byte(invalidContent), 0644)

		compiler := workflow.NewCompiler()

		// This should not return an error (it prints errors but continues)
		stats, err := compileAllWorkflowFiles(compiler, workflowsDir, false)
		if err != nil {
			t.Errorf("compileAllWorkflowFiles should handle compilation errors gracefully: %v", err)
		}
		if stats.Errors == 0 {
			t.Error("Expected at least 1 compilation error")
		}
	})

	t.Run("compile all verbose mode", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create a valid test file
		testFile := filepath.Join(workflowsDir, "verbose-test.md")
		content := "---\non: push\nengine: claude\n---\n# Verbose Test\n\nTest content for verbose mode"
		os.WriteFile(testFile, []byte(content), 0644)

		compiler := workflow.NewCompiler()

		// Test verbose mode (should not error)
		stats, err := compileAllWorkflowFiles(compiler, workflowsDir, true)
		if err != nil {
			t.Errorf("compileAllWorkflowFiles verbose mode failed: %v", err)
		}
		if stats.Total != 1 {
			t.Errorf("Expected 1 total file, got %d", stats.Total)
		}
	})
}

// TestCompileModifiedFiles tests the compileModifiedFiles function
// This covers pkg/cli/commands.go:837
func TestCompileModifiedFiles(t *testing.T) {
	t.Run("compile modified files basic functionality", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create test files with different modification times
		file1 := filepath.Join(workflowsDir, "recent.md")
		file2 := filepath.Join(workflowsDir, "old.md")

		content := "---\non: push\nengine: claude\n---\n# Test\n\nTest content"

		os.WriteFile(file1, []byte(content), 0644)
		os.WriteFile(file2, []byte(content), 0644)

		// Make file2 older
		oldTime := time.Now().Add(-2 * time.Hour)
		os.Chtimes(file2, oldTime, oldTime)

		compiler := workflow.NewCompiler()

		// Test with recent files - compileModifiedFiles takes a slice of files
		modifiedFiles := []string{file1} // Only include the recent file
		compileModifiedFiles(compiler, modifiedFiles, true)

		// Check that the recent file was compiled
		recentLock := filepath.Join(workflowsDir, "recent.lock.yml")

		if _, err := os.Stat(recentLock); os.IsNotExist(err) {
			t.Error("Recent file should have been compiled")
		}
	})

	t.Run("compile modified files with no files", func(t *testing.T) {
		compiler := workflow.NewCompiler()

		// Test with empty file list (should not error)
		emptyFiles := []string{}
		compileModifiedFiles(compiler, emptyFiles, true)
		// Should complete without error
	})

	t.Run("compile modified files with invalid files", func(t *testing.T) {
		compiler := workflow.NewCompiler()

		// Test with invalid file paths
		invalidFiles := []string{"nonexistent/path/file.md"}
		compileModifiedFiles(compiler, invalidFiles, false)
		// Should handle gracefully without panicking
	})

	t.Run("compile modified files verbose mode", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create a recent file
		recentFile := filepath.Join(workflowsDir, "recent.md")
		content := "---\non: push\nengine: claude\n---\n# Recent Test\n\nRecent content"
		os.WriteFile(recentFile, []byte(content), 0644)

		compiler := workflow.NewCompiler()

		// Test verbose mode
		modifiedFiles := []string{recentFile}
		compileModifiedFiles(compiler, modifiedFiles, true)
		// Should complete without error
	})
}

// TestHandleFileDeleted tests the handleFileDeleted function
// This covers pkg/cli/commands.go:888
func TestHandleFileDeleted(t *testing.T) {
	t.Run("handle deleted markdown file", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create a lock file that should be deleted when markdown file is removed
		lockFile := filepath.Join(workflowsDir, "deleted-workflow.lock.yml")
		lockContent := "# Generated lock file content\nname: deleted-workflow\n"
		os.WriteFile(lockFile, []byte(lockContent), 0644)

		// Simulate the markdown file path
		markdownFile := filepath.Join(workflowsDir, "deleted-workflow.md")

		handleFileDeleted(markdownFile, true)

		// Check that lock file was removed
		if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
			t.Error("Lock file should have been deleted")
		}
	})

	t.Run("handle deleted non-markdown file", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")

		// Test with a non-markdown file
		txtFile := filepath.Join(tempDir, "test.txt")

		// This should not error (no-op for non-markdown files)
		handleFileDeleted(txtFile, true)
	})

	t.Run("handle deleted file without corresponding lock", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Test deleting a markdown file that doesn't have a corresponding lock file
		markdownFile := filepath.Join(workflowsDir, "no-lock.md")

		handleFileDeleted(markdownFile, false)
	})

	t.Run("handle deleted file verbose mode", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create a lock file
		lockFile := filepath.Join(workflowsDir, "verbose-test.lock.yml")
		os.WriteFile(lockFile, []byte("name: verbose-test\n"), 0644)

		markdownFile := filepath.Join(workflowsDir, "verbose-test.md")

		// Test verbose mode
		handleFileDeleted(markdownFile, true)
	})

	t.Run("handle deleted file with permission error", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create a lock file in a read-only directory (simulate permission error)
		readOnlyDir := filepath.Join(tempDir, "readonly")
		os.MkdirAll(readOnlyDir, 0555) // read-only
		defer func() {
			if err := os.Chmod(readOnlyDir, 0755); err != nil {
				t.Errorf("Failed to restore permissions: %v", err)
			}
		}() // restore permissions for cleanup

		markdownFile := filepath.Join(readOnlyDir, "readonly-test.md")

		// This might error due to permissions, but should handle gracefully
		// The important thing is that it doesn't panic
		handleFileDeleted(markdownFile, false)
	})
}

// TestCompileSingleFile tests the compileSingleFile helper function
func TestCompileSingleFile(t *testing.T) {
	t.Run("compile single file successfully", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create a valid workflow file
		filePath := filepath.Join(workflowsDir, "test.md")
		content := "---\non: push\nengine: claude\n---\n# Test\n\nTest workflow content"
		os.WriteFile(filePath, []byte(content), 0644)

		compiler := workflow.NewCompiler()
		stats := &CompilationStats{}

		// Compile without checking existence
		result := compileSingleFile(compiler, filePath, stats, false, false)

		if !result {
			t.Error("Expected compilation to be attempted")
		}

		if stats.Total != 1 {
			t.Errorf("Expected Total to be 1, got %d", stats.Total)
		}

		if stats.Errors != 0 {
			t.Errorf("Expected no errors, got %d", stats.Errors)
		}

		// Check that lock file was created
		lockFile := filepath.Join(workflowsDir, "test.lock.yml")
		if _, err := os.Stat(lockFile); os.IsNotExist(err) {
			t.Error("Expected lock file to be created")
		}
	})

	t.Run("compile single file with error", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create an invalid workflow file
		filePath := filepath.Join(workflowsDir, "invalid.md")
		content := "---\nmalformed: yaml: content:\n  - missing\n    proper: structure\n---\n# Invalid\n"
		os.WriteFile(filePath, []byte(content), 0644)

		compiler := workflow.NewCompiler()
		stats := &CompilationStats{}

		// Compile without checking existence
		result := compileSingleFile(compiler, filePath, stats, false, false)

		if !result {
			t.Error("Expected compilation to be attempted")
		}

		if stats.Total != 1 {
			t.Errorf("Expected Total to be 1, got %d", stats.Total)
		}

		if stats.Errors != 1 {
			t.Errorf("Expected 1 error, got %d", stats.Errors)
		}

		if len(stats.FailedWorkflows) != 1 {
			t.Errorf("Expected 1 failed workflow, got %d", len(stats.FailedWorkflows))
		}

		if stats.FailedWorkflows[0] != "invalid.md" {
			t.Errorf("Expected failed workflow to be 'invalid.md', got '%s'", stats.FailedWorkflows[0])
		}
	})

	t.Run("compile single file with checkExists true and file exists", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create a valid workflow file
		filePath := filepath.Join(workflowsDir, "test.md")
		content := "---\non: push\nengine: claude\n---\n# Test\n\nTest workflow content"
		os.WriteFile(filePath, []byte(content), 0644)

		compiler := workflow.NewCompiler()
		stats := &CompilationStats{}

		// Compile with existence check
		result := compileSingleFile(compiler, filePath, stats, false, true)

		if !result {
			t.Error("Expected compilation to be attempted")
		}

		if stats.Total != 1 {
			t.Errorf("Expected Total to be 1, got %d", stats.Total)
		}
	})

	t.Run("compile single file with checkExists true and file does not exist", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Use a non-existent file path
		filePath := filepath.Join(workflowsDir, "nonexistent.md")

		compiler := workflow.NewCompiler()
		stats := &CompilationStats{}

		// Compile with existence check - should skip
		result := compileSingleFile(compiler, filePath, stats, false, true)

		if result {
			t.Error("Expected compilation to be skipped for non-existent file")
		}

		if stats.Total != 0 {
			t.Errorf("Expected Total to be 0 (not attempted), got %d", stats.Total)
		}
	})

	t.Run("compile single file verbose mode", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Create a valid workflow file
		filePath := filepath.Join(workflowsDir, "verbose-test.md")
		content := "---\non: push\nengine: claude\n---\n# Verbose Test\n\nTest workflow content"
		os.WriteFile(filePath, []byte(content), 0644)

		compiler := workflow.NewCompiler()
		stats := &CompilationStats{}

		// Compile in verbose mode
		result := compileSingleFile(compiler, filePath, stats, true, false)

		if !result {
			t.Error("Expected compilation to be attempted")
		}

		if stats.Total != 1 {
			t.Errorf("Expected Total to be 1, got %d", stats.Total)
		}

		if stats.Errors != 0 {
			t.Errorf("Expected no errors, got %d", stats.Errors)
		}
	})
}
