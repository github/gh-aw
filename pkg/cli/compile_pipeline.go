// This file provides main orchestration logic for workflow compilation.
//
// The orchestration functions coordinate compilation of specific files or all
// files in a directory and integrate the compilation phases.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileOrchestrationLog = logger.New("cli:compile_pipeline")

const fallbackCompilationErrorMessage = "compilation failed (no detailed error message available)"

// compileSpecificFiles compiles a specific list of workflow files
func compileSpecificFiles( //nolint:largefunc // Orchestrates the full targeted compile pipeline.
	ctx context.Context,
	compiler *workflow.Compiler,
	config CompileConfig,
	stats *CompilationStats,
	validationResults *[]ValidationResult,
) ([]*workflow.WorkflowData, error) {
	compileOrchestrationLog.Printf("Compiling %d specific workflow files", len(config.MarkdownFiles))
	if err := prepareDryRunScannerBaseline(ctx, &config, config.MarkdownFiles, "", compiler); err != nil {
		return nil, err
	}

	batchMode := !config.Verbose && len(config.MarkdownFiles) > 1
	compiler.SetBatchMode(batchMode)
	compiler.SetQuiet(batchMode)

	// Enable validation automatically when force-refresh-action-pins is used
	// to verify all resolved action SHAs are valid
	shouldValidate := config.Validate || config.ForceRefreshActionPins
	if config.ForceRefreshActionPins && !config.Validate {
		compileOrchestrationLog.Print("Automatically enabling action SHA validation due to --force-refresh-action-pins")
	}

	var workflowDataList []*workflow.WorkflowData
	var compiledCount int
	var errorCount int
	var batchOpts batchToolsOptions
	var compiledLockFiles []string

	// Compile each specified file
	for _, markdownFile := range config.MarkdownFiles {
		// Respect context cancellation between files (e.g. Ctrl+C)
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Operation cancelled"))
			return workflowDataList, ctx.Err()
		default:
		}

		stats.Total++

		// Initialize validation result
		result := ValidationResult{
			Workflow: markdownFile,
			Valid:    true,
			Errors:   []ValidationIssue{},
			Warnings: []ValidationIssue{},
		}

		// Resolve workflow ID or file path to actual file path
		compileOrchestrationLog.Printf("Resolving workflow file: %s", markdownFile)
		resolvedFile, err := resolveWorkflowFile(markdownFile, config.Verbose)
		if err != nil {
			// Don't print error here - it will be displayed in the compilation summary
			// The error is stored in ValidationResult for JSON output and returned for main to display
			errorCount++
			stats.Errors++
			trackWorkflowFailure(stats, markdownFile, 1, []string{err.Error()})
			result.Valid = false
			result.Errors = append(result.Errors, ValidationIssue{
				Type:    "resolution_error",
				Message: err.Error(),
			})
			*validationResults = append(*validationResults, result)
			writeMCPCompileProgress(config.MCPProgressFile, *validationResults)
			continue
		}
		compileOrchestrationLog.Printf("Resolved to: %s", resolvedFile)

		// Update result with resolved file name
		result.Workflow = filepath.Base(resolvedFile)

		// Compile regular workflow file (disable per-file security tools)
		fileResult := compileWorkflowFile(
			ctx, compiler, resolvedFile, compileWorkflowFileOptions{
				verbose:        config.Verbose,
				jsonOutput:     config.JSONOutput,
				noEmit:         config.NoEmit,
				strict:         config.Strict,
				validate:       shouldValidate,
				failOnWarnings: config.DryRun,
				// zizmor, poutine, actionlint disabled per-file (batched instead)
			},
		)

		if !fileResult.success {
			// Collect error messages from validation result for display in summary
			var errMsgs []string
			for _, verr := range fileResult.validationResult.Errors {
				errMsgs = append(errMsgs, verr.Message)
			}
			if len(errMsgs) == 0 {
				errMsgs = []string{fallbackCompilationErrorMessage}
			}
			errorCount++
			stats.Errors += len(errMsgs)
			trackWorkflowFailure(stats, resolvedFile, len(errMsgs), errMsgs)
		} else {
			compiledCount++
			stats.Succeeded++
			if fileResult.workflowData != nil {
				workflowDataList = append(workflowDataList, fileResult.workflowData)
			}

			// Collect lock files for batch security tools
			if !config.NoEmit && fileResult.lockFile != "" {
				if _, err := os.Stat(fileResult.lockFile); err == nil {
					compiledLockFiles = append(compiledLockFiles, fileResult.lockFile)
					collectBatchToolInputs(config, fileResult, &batchOpts)
				}
			}
		}

		if len(*validationResults) == 0 {
			appendMCPCachedModelInventoryWarning(config, &fileResult.validationResult)
		}
		*validationResults = append(*validationResults, fileResult.validationResult)
		writeMCPCompileProgress(config.MCPProgressFile, *validationResults)
	}

	if config.DryRun {
		batchOpts.workflowDir = config.dryRunWorkflowDir
	}
	strictGrantErr, batchToolErr := runBatchExternalTools(ctx, config, batchOpts, stats, validationResults)

	if strictGrantErr != nil || batchToolErr != nil {
		errorCount++
	}

	// Get warning count from compiler
	stats.Warnings = compiler.GetWarningCount()

	// Aggregate and display batch-mode notices (experimental features, Copilot tip)
	displayBatchCompilationNotices(compiler, config)

	// Display schedule warnings
	displayScheduleWarnings(compiler, config.JSONOutput)

	// Display safe update warnings (emitted as prompts for the calling agent)
	displaySafeUpdateWarnings(compiler, config.JSONOutput)

	// Post-processing
	if !config.NoEmit {
		if err := runPostProcessing(ctx, compiler, workflowDataList, compiledLockFiles, config, compiledCount); err != nil {
			return workflowDataList, err
		}
	}

	developmentErr := enforceDevelopmentDiagnostics(config, compiler, stats, validationResults, strictGrantErr, batchToolErr)

	// Output results
	if err := outputResults(stats, validationResults, config); err != nil {
		return workflowDataList, err
	}

	// Return error if any compilations failed
	// Don't return the detailed error message here since it's already printed in the summary
	// Returning a simple error prevents duplication in the output
	if errorCount > 0 {
		if strictGrantErr != nil {
			return workflowDataList, strictGrantErr
		}
		if batchToolErr != nil {
			return workflowDataList, batchToolErr
		}
		return workflowDataList, errors.New("compilation failed")
	}

	return workflowDataList, developmentErr
}

// compileAllFilesInDirectory compiles all workflow files in a directory
func compileAllFilesInDirectory( //nolint:largefunc // Orchestrates the full directory compile pipeline.
	ctx context.Context,
	compiler *workflow.Compiler,
	config CompileConfig,
	workflowDir string,
	stats *CompilationStats,
	validationResults *[]ValidationResult,
) ([]*workflow.WorkflowData, error) {
	// Find git root for consistent behavior
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return nil, fmt.Errorf("compile without arguments requires being in a git repository: %w", err)
	}
	compileOrchestrationLog.Printf("Found git root: %s", gitRoot)

	// Compile all markdown files in the specified workflow directory
	workflowsDir := filepath.Join(gitRoot, workflowDir)
	if _, err := os.Stat(workflowsDir); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("the %s directory does not exist in git root (%s)", workflowDir, gitRoot)
	}

	compileOrchestrationLog.Printf("Scanning for markdown files in %s", workflowsDir)
	if config.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Scanning for markdown files in "+workflowsDir))
	}

	// Find and filter markdown files (shared helper keeps logic in one place)
	mdFiles, err := getMarkdownWorkflowFiles(workflowsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find markdown files: %w", err)
	}
	mdFiles, err = filterMarkdownFilesWithFrontmatter(mdFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to filter markdown files: %w", err)
	}

	if len(mdFiles) == 0 {
		return nil, fmt.Errorf("no workflow markdown files found in %s (workflow files must start with a frontmatter opener on the first line)", workflowsDir)
	}
	if err := prepareDryRunScannerBaseline(ctx, &config, mdFiles, workflowsDir, compiler); err != nil {
		return nil, err
	}

	compileOrchestrationLog.Printf("Found %d markdown files to compile", len(mdFiles))
	if config.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Found %d markdown files to compile", len(mdFiles))))
	}

	batchMode := !config.Verbose && len(mdFiles) > 1
	compiler.SetBatchMode(batchMode)
	compiler.SetQuiet(batchMode)

	// Handle purge logic: collect existing files before compilation
	var purgeData *purgeTrackingData
	if config.Purge {
		purgeData, err = collectPurgeData(workflowsDir, mdFiles, config.Verbose)
		if err != nil {
			return nil, fmt.Errorf("failed to collect existing files for purge: %w", err)
		}
	}

	// Enable validation automatically when force-refresh-action-pins is used
	// to verify all resolved action SHAs are valid
	shouldValidate := config.Validate || config.ForceRefreshActionPins
	if config.ForceRefreshActionPins && !config.Validate {
		compileOrchestrationLog.Print("Automatically enabling action SHA validation due to --force-refresh-action-pins")
	}

	// Compile each file
	var workflowDataList []*workflow.WorkflowData
	var successCount int
	var errorCount int
	var batchOpts batchToolsOptions
	var workflowValidationResultIndexes []int

	for _, file := range mdFiles {
		// Respect context cancellation between files (e.g. Ctrl+C)
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Operation cancelled"))
			return workflowDataList, ctx.Err()
		default:
		}

		stats.Total++

		// Compile regular workflow file (disable per-file security tools)
		fileResult := compileWorkflowFile(
			ctx, compiler, file, compileWorkflowFileOptions{
				verbose:        config.Verbose,
				jsonOutput:     config.JSONOutput,
				noEmit:         config.NoEmit,
				strict:         config.Strict,
				validate:       shouldValidate,
				failOnWarnings: config.DryRun,
				// zizmor, poutine, actionlint disabled per-file (batched instead)
			},
		)

		if !fileResult.success {
			// Collect error messages from validation result
			var errMsgs []string
			for _, verr := range fileResult.validationResult.Errors {
				errMsgs = append(errMsgs, verr.Message)
			}
			if len(errMsgs) == 0 {
				errMsgs = []string{fallbackCompilationErrorMessage}
			}
			errorCount++
			stats.Errors += len(errMsgs)
			trackWorkflowFailure(stats, file, len(errMsgs), errMsgs)
		} else {
			successCount++
			stats.Succeeded++
			if fileResult.workflowData != nil {
				workflowDataList = append(workflowDataList, fileResult.workflowData)
				workflowValidationResultIndexes = append(workflowValidationResultIndexes, len(*validationResults))
			}

			// Collect lock files for batch security tools
			if !config.NoEmit && fileResult.lockFile != "" {
				if _, err := os.Stat(fileResult.lockFile); err == nil {
					collectBatchToolInputs(config, fileResult, &batchOpts)
				}
			}
		}

		*validationResults = append(*validationResults, fileResult.validationResult)
		writeMCPCompileProgress(config.MCPProgressFile, *validationResults)
	}

	dryRunWorkflowDir := ""
	if config.DryRun {
		dryRunWorkflowDir = config.dryRunWorkflowDir
	}
	batchOpts.workflowDir = firstNonEmpty(dryRunWorkflowDir, workflowsDir)
	strictGrantErr, batchToolErr := runBatchExternalTools(ctx, config, batchOpts, stats, validationResults)

	if strictGrantErr != nil || batchToolErr != nil {
		errorCount++
	}

	// Emit recommendation when many slash commands are present without centralized strategy.
	displayCentralizedSlashCommandRecommendation(compiler, workflowDataList, config.JSONOutput)

	duplicateNameWarnings, err := appendDuplicateWorkflowNameWarnings(workflowDataList, workflowValidationResultIndexes, validationResults)
	if err != nil {
		return workflowDataList, err
	}
	if !config.JSONOutput {
		for _, warning := range duplicateNameWarnings {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(warning.Message))
		}
	}

	// Get warning count from compiler
	stats.Warnings = compiler.GetWarningCount() + len(duplicateNameWarnings)

	displayBatchCompilationNotices(compiler, config)

	// Display schedule warnings
	displayScheduleWarnings(compiler, config.JSONOutput)

	// Display safe update warnings (emitted as prompts for the calling agent)
	displaySafeUpdateWarnings(compiler, config.JSONOutput)

	if config.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr(fmt.Sprintf("Successfully compiled %d out of %d workflow files", successCount, len(mdFiles))))
	}

	// Handle purge logic if requested
	if config.Purge && purgeData != nil {
		runPurgeOperations(workflowsDir, purgeData, config.Verbose)
	}

	// Post-processing
	if !config.NoEmit {
		if err := runPostProcessingForDirectory(ctx, compiler, workflowDataList, config, workflowsDir, gitRoot, successCount, errorCount); err != nil {
			return workflowDataList, err
		}
	}

	developmentErr := enforceDevelopmentDiagnostics(config, compiler, stats, validationResults, strictGrantErr, batchToolErr)

	// Output results.
	// Populate MarkdownFiles so that outputResults can collect per-workflow stats
	// (e.g. schedule heatmap) even when the caller did not specify explicit files.
	if config.Stats && len(config.MarkdownFiles) == 0 {
		config.MarkdownFiles = mdFiles
	}
	if err := outputResults(stats, validationResults, config); err != nil {
		return workflowDataList, err
	}

	// Return error if any compilations failed
	if errorCount > 0 {
		if strictGrantErr != nil {
			return workflowDataList, strictGrantErr
		}
		if batchToolErr != nil {
			return workflowDataList, batchToolErr
		}
		return workflowDataList, errors.New("compilation failed")
	}

	return workflowDataList, developmentErr
}
