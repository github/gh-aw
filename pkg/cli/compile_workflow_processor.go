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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileWorkflowProcessorLog = logger.New("cli:compile_workflow_processor")

func appendValidationErrors(dst []ValidationIssue, errorType string, err error) []ValidationIssue {
	for _, message := range workflow.ExpandErrorMessages(err) {
		dst = append(dst, ValidationIssue{
			Type:    errorType,
			Message: message,
		})
	}
	return dst
}

// compileWorkflowFileResult represents the result of compiling a single workflow file
type compileWorkflowFileResult struct {
	workflowData     *workflow.WorkflowData
	lockFile         string
	validationResult ValidationResult
	success          bool
}

// compileWorkflowFileOptions holds flags for compileWorkflowFile.
type compileWorkflowFileOptions struct {
	verbose        bool
	jsonOutput     bool
	noEmit         bool
	zizmor         bool
	poutine        bool
	strict         bool
	validate       bool
	failOnWarnings bool
}

// compileWorkflowFile compiles a single workflow file (not a campaign spec)
// Returns the workflow data, lock file path, validation result, and success status
func compileWorkflowFile(
	ctx context.Context,
	compiler *workflow.Compiler,
	resolvedFile string,
	opts compileWorkflowFileOptions,
) compileWorkflowFileResult {
	compileWorkflowProcessorLog.Printf("Processing workflow file: %s", resolvedFile)

	result := compileWorkflowFileResult{
		validationResult: ValidationResult{
			Workflow: filepath.Base(resolvedFile),
			Valid:    true,
			Errors:   []ValidationIssue{},
			Warnings: []ValidationIssue{},
		},
		success: false,
	}

	// Generate lock file name
	lockFile := stringutil.MarkdownToLockFile(resolvedFile)
	result.lockFile = lockFile

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
	// Uses SetRepositorySlugIfUnlocked so that an explicit --schedule-seed flag is never overridden.
	fileRepoSlug := getRepositorySlugFromRemoteForPath(resolvedFile)
	if fileRepoSlug != "" {
		if compiler.IsRepositorySlugLocked() {
			compileWorkflowProcessorLog.Printf("Repository slug from file remote (%s) ignored: overridden via --schedule-seed (%s)", fileRepoSlug, compiler.GetRepositorySlug())
		} else {
			compiler.SetRepositorySlugIfUnlocked(fileRepoSlug)
			compileWorkflowProcessorLog.Printf("Repository slug for file set: %s", fileRepoSlug)
		}
	}

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(resolvedFile)
	if err != nil {
		// Check if this is a shared workflow (not an error, just info)
		var sharedErr *workflow.SharedWorkflowError
		if errors.As(err, &sharedErr) {
			if !opts.jsonOutput {
				// Print info message instead of error
				fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(sharedErr.Error()))
			}
			// Mark as valid but skipped
			result.validationResult.Valid = true
			result.validationResult.Warnings = append(result.validationResult.Warnings, ValidationIssue{
				Type:    "shared_workflow",
				Message: "Skipped: Shared workflow component (missing 'on' field)",
			})
			result.success = true // Consider it successful, just skipped
			return result
		}

		// Check if this is a redirect-only workflow (not an error, just info)
		var redirectErr *workflow.RedirectOnlyWorkflowError
		if errors.As(err, &redirectErr) {
			if !opts.jsonOutput {
				// Print info message instead of error
				fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(redirectErr.Error()))
			}
			// Mark as valid but skipped
			result.validationResult.Valid = true
			result.validationResult.Warnings = append(result.validationResult.Warnings, ValidationIssue{
				Type:    "redirect_only_workflow",
				Message: "Skipped: Redirect-only workflow (missing 'on' field, has redirect)",
			})
			result.success = true // Consider it successful, just skipped
			return result
		}

		// Don't print error here - it will be displayed in the compilation summary
		// The error is stored in ValidationResult for JSON output and summary display
		result.validationResult.Valid = false
		result.validationResult.Errors = appendValidationErrors(result.validationResult.Errors, "parse_error", err)
		return result
	}
	result.workflowData = workflowData

	compileWorkflowProcessorLog.Printf("Starting compilation of %s", resolvedFile)

	// Compile the workflow
	// Per-file actionlint is always disabled here; actionlint runs in batch after all files are compiled.
	if err := CompileWorkflowDataWithValidation(ctx, compiler, workflowData, resolvedFile, CompileValidationOptions{
		Verbose:            opts.verbose && !opts.jsonOutput,
		RunZizmorPerFile:   opts.zizmor && !opts.noEmit,
		RunPoutinePerFile:  opts.poutine && !opts.noEmit,
		Strict:             opts.strict,
		ValidateActionSHAs: opts.validate && !opts.noEmit,
		FailOnWarnings:     opts.failOnWarnings,
	}); err != nil {
		// Don't print error here - it will be displayed in the compilation summary
		// The error is stored in ValidationResult for JSON output and summary display
		result.validationResult.Valid = false
		result.validationResult.Errors = appendValidationErrors(result.validationResult.Errors, "compilation_error", err)
		return result
	}

	result.success = true
	if !opts.noEmit {
		result.validationResult.CompiledFile = lockFile
	}

	// Collect labels for JSON output (used by create-labels maintenance operation)
	result.validationResult.Labels = extractSafeOutputLabels(workflowData)

	// Emit a compile-time guard-policy dry-run report in --strict mode.
	if !opts.jsonOutput {
		printGuardPolicyDryRunReport(filepath.Base(resolvedFile), workflowData, opts.strict)
	}

	compileWorkflowProcessorLog.Printf("Successfully processed workflow file: %s", resolvedFile)
	return result
}

// extractSafeOutputLabels collects all unique labels referenced by workflow configuration
// that should exist in the repository for the workflow to function correctly.
// Scans: safe-outputs labels (create-issue/create-discussion/create-pull-request/add-labels)
// and on.label_command trigger labels.
func extractSafeOutputLabels(data *workflow.WorkflowData) []string {
	if data == nil {
		return nil
	}

	seen := make(map[string]struct {
	})
	var labels []string

	addLabel := func(label string) {
		if label != "" && !setutil.Contains(seen, label) {
			seen[label] = struct {
			}{}
			labels = append(labels, label)
		}
	}

	so := data.SafeOutputs
	if so != nil {
		if so.CreateIssues != nil {
			for _, l := range so.CreateIssues.Labels {
				addLabel(l)
			}
			for _, l := range so.CreateIssues.AllowedLabels {
				addLabel(l)
			}
		}

		if so.CreateDiscussions != nil {
			for _, l := range so.CreateDiscussions.Labels {
				addLabel(l)
			}
			for _, l := range so.CreateDiscussions.AllowedLabels {
				addLabel(l)
			}
		}

		if so.CreatePullRequests != nil {
			for _, l := range so.CreatePullRequests.Labels {
				addLabel(l)
			}
			for _, l := range so.CreatePullRequests.AllowedLabels {
				addLabel(l)
			}
		}

		if so.AddLabels != nil {
			for _, l := range so.AddLabels.Allowed {
				addLabel(l)
			}
		}
	}

	for _, l := range data.LabelCommand {
		addLabel(l)
	}

	return labels
}
