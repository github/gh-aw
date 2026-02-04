// This file provides workflow file processing functions for compilation.
//
// This file contains functions that process individual workflow files.
//
// # Organization Rationale
//
// These workflow processing functions are grouped here because they:
//   - Handle per-file processing logic
//   - Process workflow files with compilation and validation
//   - Have a clear domain focus (workflow file processing)
//   - Keep the main orchestrator focused on batch operations
//
// # Key Functions
//
// Workflow Processing:
//   - processWorkflowFile() - Process a single workflow markdown file
//   - collectLockFilesForLinting() - Collect lock files for batch linting
//
// These functions abstract per-file processing, allowing the main compile
// orchestrator to focus on coordination while these handle file processing.

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileWorkflowProcessorLog = logger.New("cli:compile_workflow_processor")

// compileWorkflowFileResult represents the result of compiling a single workflow file
type compileWorkflowFileResult struct {
	workflowData     *workflow.WorkflowData
	lockFile         string
	validationResult ValidationResult
	success          bool
}

// compileWorkflowFile compiles a single workflow file (not a campaign spec)
// Returns the workflow data, lock file path, validation result, and success status
func compileWorkflowFile(
	compiler *workflow.Compiler,
	resolvedFile string,
	verbose bool,
	jsonOutput bool,
	noEmit bool,
	zizmor bool,
	poutine bool,
	actionlint bool,
	strict bool,
	validate bool,
) compileWorkflowFileResult {
	compileWorkflowProcessorLog.Printf("Processing workflow file: %s", resolvedFile)

	result := compileWorkflowFileResult{
		validationResult: ValidationResult{
			Workflow: filepath.Base(resolvedFile),
			Valid:    true,
			Errors:   []CompileValidationError{},
			Warnings: []CompileValidationError{},
		},
		success: false,
	}

	// Generate lock file name
	lockFile := stringutil.MarkdownToLockFile(resolvedFile)
	result.lockFile = lockFile
	if !noEmit {
		result.validationResult.CompiledFile = lockFile
	}

	// Parse workflow file to get data
	compileWorkflowProcessorLog.Printf("Parsing workflow file: %s", resolvedFile)

	// Set workflow identifier for schedule scattering (use repository-relative path for stability)
	relPath, err := getRepositoryRelativePath(resolvedFile)
	if err != nil {
		compileWorkflowProcessorLog.Printf("Warning: failed to get repository-relative path for %s: %v", resolvedFile, err)
		// Fallback to basename if we can't get relative path
		relPath = filepath.Base(resolvedFile)
	}
	compiler.SetWorkflowIdentifier(relPath)

	// Set repository slug for this specific file (may differ from CWD's repo)
	fileRepoSlug := getRepositorySlugFromRemoteForPath(resolvedFile)
	if fileRepoSlug != "" {
		compiler.SetRepositorySlug(fileRepoSlug)
		compileWorkflowProcessorLog.Printf("Repository slug for file set: %s", fileRepoSlug)
	}

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(resolvedFile)
	if err != nil {
		// Check if this is a shared workflow (not an error, just info)
		if sharedErr, ok := err.(*workflow.SharedWorkflowError); ok {
			if !jsonOutput {
				// Print info message instead of error
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(sharedErr.Error()))
			}
			// Mark as valid but skipped
			result.validationResult.Valid = true
			result.validationResult.Warnings = append(result.validationResult.Warnings, CompileValidationError{
				Type:    "shared_workflow",
				Message: "Skipped: Shared workflow component (missing 'on' field)",
			})
			result.success = true // Consider it successful, just skipped
			return result
		}

		// Don't print error here - it will be displayed in the compilation summary
		// The error is stored in ValidationResult for JSON output and summary display
		result.validationResult.Valid = false
		result.validationResult.Errors = append(result.validationResult.Errors, CompileValidationError{
			Type:    "parse_error",
			Message: err.Error(),
		})
		return result
	}
	result.workflowData = workflowData

	compileWorkflowProcessorLog.Printf("Starting compilation of %s", resolvedFile)

	// Compile the workflow
	// Disable per-file actionlint run (false instead of actionlint && !noEmit) - we'll batch them
	if err := CompileWorkflowDataWithValidation(compiler, workflowData, resolvedFile, verbose && !jsonOutput, zizmor && !noEmit, poutine && !noEmit, false, strict, validate && !noEmit); err != nil {
		// Don't print error here - it will be displayed in the compilation summary
		// The error is stored in ValidationResult for JSON output and summary display
		result.validationResult.Valid = false
		result.validationResult.Errors = append(result.validationResult.Errors, CompileValidationError{
			Type:    "compilation_error",
			Message: err.Error(),
		})
		return result
	}

	result.success = true
	compileWorkflowProcessorLog.Printf("Successfully processed workflow file: %s", resolvedFile)
	return result
}
