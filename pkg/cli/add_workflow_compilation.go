package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var addWorkflowCompilationLog = logger.New("cli:add_workflow_compilation")

// updateWorkflowTitle updates the H1 title in workflow content by appending a number.
// This is used when creating multiple numbered copies of a workflow.
func updateWorkflowTitle(content string, number int) string {
	// Find and update the first H1 header
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			// Extract the title part and add number
			title := strings.TrimSpace(line[2:])
			lines[i] = fmt.Sprintf("# %s %d", title, number)
			break
		}
	}
	return strings.Join(lines, "\n")
}

// compileWorkflow compiles a workflow file without refreshing stop time.
// This is a convenience wrapper around compileWorkflowWithRefresh.
func compileWorkflow(filePath string, verbose bool, quiet bool, engineOverride string) error {
	return compileWorkflowWithRefresh(filePath, verbose, quiet, engineOverride, false)
}

// compileWorkflowWithRefresh compiles a workflow file with optional stop time refresh.
// This function handles the compilation process and ensures .gitattributes is updated.
func compileWorkflowWithRefresh(filePath string, verbose bool, quiet bool, engineOverride string, refreshStopTime bool) error {
	addWorkflowCompilationLog.Printf("Compiling workflow: file=%s, refresh_stop_time=%v, engine=%s", filePath, refreshStopTime, engineOverride)

	// Create compiler with auto-detected version and action mode
	compiler := workflow.NewCompiler(
		workflow.WithVerbose(verbose),
		workflow.WithEngineOverride(engineOverride),
	)

	compiler.SetRefreshStopTime(refreshStopTime)
	compiler.SetQuiet(quiet)
	if err := CompileWorkflowWithValidation(compiler, filePath, verbose, false, false, false, false, false); err != nil {
		addWorkflowCompilationLog.Printf("Compilation failed: %v", err)
		return err
	}

	addWorkflowCompilationLog.Print("Compilation completed successfully")

	// Ensure .gitattributes marks .lock.yml files as generated
	if err := ensureGitAttributes(); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
		}
	}

	// Note: Instructions are only written when explicitly requested via the compile command flag
	// This helper function is used in contexts where instructions should not be automatically written

	return nil
}

// compileWorkflowWithTracking compiles a workflow and tracks generated files.
// This is a convenience wrapper around compileWorkflowWithTrackingAndRefresh.
func compileWorkflowWithTracking(filePath string, verbose bool, quiet bool, engineOverride string, tracker *FileTracker) error {
	return compileWorkflowWithTrackingAndRefresh(filePath, verbose, quiet, engineOverride, tracker, false)
}

// compileWorkflowWithTrackingAndRefresh compiles a workflow, tracks generated files, and optionally refreshes stop time.
// This function ensures that the file tracker records all files created or modified during compilation.
func compileWorkflowWithTrackingAndRefresh(filePath string, verbose bool, quiet bool, engineOverride string, tracker *FileTracker, refreshStopTime bool) error {
	addWorkflowCompilationLog.Printf("Compiling workflow with tracking: file=%s, refresh_stop_time=%v", filePath, refreshStopTime)

	// Generate the expected lock file path
	lockFile := stringutil.MarkdownToLockFile(filePath)

	// Check if lock file exists before compilation
	lockFileExists := false
	if _, err := os.Stat(lockFile); err == nil {
		lockFileExists = true
	}

	addWorkflowCompilationLog.Printf("Lock file %s exists: %v", lockFile, lockFileExists)

	// Check if .gitattributes exists before ensuring it
	gitRoot, err := findGitRoot()
	if err != nil {
		return err
	}
	gitAttributesPath := filepath.Join(gitRoot, ".gitattributes")
	gitAttributesExists := false
	if _, err := os.Stat(gitAttributesPath); err == nil {
		gitAttributesExists = true
	}

	// Track the lock file before compilation
	if lockFileExists {
		tracker.TrackModified(lockFile)
	} else {
		tracker.TrackCreated(lockFile)
	}

	// Track .gitattributes file before modification
	if gitAttributesExists {
		tracker.TrackModified(gitAttributesPath)
	} else {
		tracker.TrackCreated(gitAttributesPath)
	}

	// Create compiler with auto-detected version and action mode
	compiler := workflow.NewCompiler(
		workflow.WithVerbose(verbose),
		workflow.WithEngineOverride(engineOverride),
	)
	compiler.SetFileTracker(tracker)
	compiler.SetRefreshStopTime(refreshStopTime)
	compiler.SetQuiet(quiet)
	if err := CompileWorkflowWithValidation(compiler, filePath, verbose, false, false, false, false, false); err != nil {
		return err
	}

	// Ensure .gitattributes marks .lock.yml files as generated
	if err := ensureGitAttributes(); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
		}
	}

	return nil
}

// addSourceToWorkflow adds the source field to the workflow's frontmatter.
// This function preserves the existing frontmatter formatting while adding the source field.
func addSourceToWorkflow(content, source string) (string, error) {
	// Use shared frontmatter logic that preserves formatting
	return addFieldToFrontmatter(content, "source", source)
}
