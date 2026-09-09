// This file provides main orchestration logic for workflow compilation.
//
// This file contains the primary compilation orchestration functions that coordinate
// the compilation of specific files or all files in a directory.
//
// # Organization Rationale
//
// These orchestration functions are grouped here because they:
//   - Coordinate the overall compilation process
//   - Handle both specific file and directory-wide compilation
//   - Integrate all compilation phases (processing, validation, linting, post-processing)
//   - Keep the main CompileWorkflows function small and focused
//
// # Key Functions
//
// Compilation Orchestration:
//   - compileSpecificFiles() - Compile a list of specific workflow files
//   - compileAllFilesInDirectory() - Compile all workflows in a directory
//
// These functions handle the complete compilation pipeline for their respective scenarios.

package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileOrchestrationLog = logger.New("cli:compile_pipeline")

// Batch tool entry points are exposed as package-level function variables so
// tests can override them to verify the batch pipeline invokes every enabled
// scanner in order, without short-circuiting, and without depending on the
// underlying external tool binaries/Docker images being available.
var (
	runBatchActionlintOnFiles                 = RunActionlintOnFiles
	runBatchZizmorOnFiles                     = RunZizmorOnFiles
	runBatchPoutineOnDirectory                = RunPoutineOnDirectory
	runBatchRunnerGuardOnDirectory            = RunRunnerGuardOnDirectory
	runBatchSyftOnLockFiles                   = RunSyftOnLockFiles
	runBatchGrypeOnLockFiles                  = RunGrypeOnLockFiles
	runBatchGrantOnLockFiles                  = RunGrantOnLockFiles
	runBatchYamllintOnFiles                   = RunYamllintOnFiles
	runBatchShellcheckOnLockFilesAndResources = RunShellcheckOnLockFilesAndResources
)

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
	var lockFilesForActionlint []string
	var lockFilesForZizmor []string
	var lockFilesForDirTools []string // lock files for directory-based tools (poutine, runner-guard)
	var lockFilesForSyft []string     // lock files for syft container image SBOM scanning
	var lockFilesForGrype []string    // lock files for grype container image vulnerability scanning
	var lockFilesForGrant []string    // lock files for grant container image license scanning
	var strictGrantErr error
	var lockFilesForYamllint []string   // lock files for yamllint YAML linter
	var lockFilesForShellcheck []string // lock files for shellcheck run step linting
	var shellcheckResources []workflow.ShellScriptResource
	var compiledLockFiles []string // every lock file actually emitted, regardless of which lint tools are enabled

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
			continue
		}
		compileOrchestrationLog.Printf("Resolved to: %s", resolvedFile)

		// Update result with resolved file name
		result.Workflow = filepath.Base(resolvedFile)

		// Compile regular workflow file (disable per-file security tools)
		fileResult := compileWorkflowFile(
			ctx, compiler, resolvedFile, compileWorkflowFileOptions{
				verbose:    config.Verbose,
				jsonOutput: config.JSONOutput,
				noEmit:     config.NoEmit,
				strict:     config.Strict,
				validate:   shouldValidate,
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
					if config.Actionlint {
						lockFilesForActionlint = append(lockFilesForActionlint, fileResult.lockFile)
					}
					if config.Zizmor {
						lockFilesForZizmor = append(lockFilesForZizmor, fileResult.lockFile)
					}
					if config.Poutine || config.RunnerGuard {
						lockFilesForDirTools = append(lockFilesForDirTools, fileResult.lockFile)
					}
					if config.Grype {
						lockFilesForGrype = append(lockFilesForGrype, fileResult.lockFile)
					}
					if config.Grant {
						lockFilesForGrant = append(lockFilesForGrant, fileResult.lockFile)
					}
					if config.Syft {
						lockFilesForSyft = append(lockFilesForSyft, fileResult.lockFile)
					}
					if config.Yamllint {
						lockFilesForYamllint = append(lockFilesForYamllint, fileResult.lockFile)
					}
					if config.shellcheckEnabled() {
						lockFilesForShellcheck = append(lockFilesForShellcheck, fileResult.lockFile)
						shellcheckResources = append(shellcheckResources, fileResult.workflowData.ShellScriptResources()...)
					}
				}
			}
		}

		*validationResults = append(*validationResults, fileResult.validationResult)
	}

	strictGrantErr, batchToolErr := runBatchExternalTools(ctx, config, batchToolsOptions{
		lockFilesForActionlint: lockFilesForActionlint,
		lockFilesForZizmor:     lockFilesForZizmor,
		lockFilesForDirTools:   lockFilesForDirTools,
		lockFilesForSyft:       lockFilesForSyft,
		lockFilesForGrype:      lockFilesForGrype,
		lockFilesForGrant:      lockFilesForGrant,
		lockFilesForYamllint:   lockFilesForYamllint,
		lockFilesForShellcheck: lockFilesForShellcheck,
		shellcheckResources:    shellcheckResources,
	}, stats, validationResults)

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
	if err := runPostProcessing(ctx, compiler, workflowDataList, compiledLockFiles, config, compiledCount); err != nil {
		return workflowDataList, err
	}

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

	return workflowDataList, nil
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
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
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
	var lockFilesForActionlint []string
	var lockFilesForZizmor []string
	var lockFilesForDirTools []string // lock files for directory-based tools (poutine, runner-guard)
	var lockFilesForSyft []string     // lock files for syft container image SBOM scanning
	var lockFilesForGrype []string    // lock files for grype container image vulnerability scanning
	var lockFilesForGrant []string    // lock files for grant container image license scanning
	var strictGrantErr error
	var lockFilesForYamllint []string   // lock files for yamllint YAML linter
	var lockFilesForShellcheck []string // lock files for shellcheck run step linting
	var shellcheckResources []workflow.ShellScriptResource
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
				verbose:    config.Verbose,
				jsonOutput: config.JSONOutput,
				noEmit:     config.NoEmit,
				strict:     config.Strict,
				validate:   shouldValidate,
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
					if config.Actionlint {
						lockFilesForActionlint = append(lockFilesForActionlint, fileResult.lockFile)
					}
					if config.Zizmor {
						lockFilesForZizmor = append(lockFilesForZizmor, fileResult.lockFile)
					}
					if config.Poutine || config.RunnerGuard {
						lockFilesForDirTools = append(lockFilesForDirTools, fileResult.lockFile)
					}
					if config.Syft {
						lockFilesForSyft = append(lockFilesForSyft, fileResult.lockFile)
					}
					if config.Grype {
						lockFilesForGrype = append(lockFilesForGrype, fileResult.lockFile)
					}
					if config.Grant {
						lockFilesForGrant = append(lockFilesForGrant, fileResult.lockFile)
					}
					if config.Yamllint {
						lockFilesForYamllint = append(lockFilesForYamllint, fileResult.lockFile)
					}
					if config.shellcheckEnabled() {
						lockFilesForShellcheck = append(lockFilesForShellcheck, fileResult.lockFile)
						shellcheckResources = append(shellcheckResources, fileResult.workflowData.ShellScriptResources()...)
					}
				}
			}
		}

		*validationResults = append(*validationResults, fileResult.validationResult)
	}

	strictGrantErr, batchToolErr := runBatchExternalTools(ctx, config, batchToolsOptions{
		workflowDir:            workflowsDir,
		lockFilesForActionlint: lockFilesForActionlint,
		lockFilesForZizmor:     lockFilesForZizmor,
		lockFilesForDirTools:   lockFilesForDirTools,
		lockFilesForSyft:       lockFilesForSyft,
		lockFilesForGrype:      lockFilesForGrype,
		lockFilesForGrant:      lockFilesForGrant,
		lockFilesForYamllint:   lockFilesForYamllint,
		lockFilesForShellcheck: lockFilesForShellcheck,
		shellcheckResources:    shellcheckResources,
	}, stats, validationResults)

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
	if err := runPostProcessingForDirectory(ctx, compiler, workflowDataList, config, workflowsDir, gitRoot, successCount, errorCount); err != nil {
		return workflowDataList, err
	}

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

	return workflowDataList, nil
}

type batchToolsOptions struct {
	workflowDir            string
	lockFilesForActionlint []string
	lockFilesForZizmor     []string
	lockFilesForDirTools   []string
	lockFilesForSyft       []string
	lockFilesForGrype      []string
	lockFilesForGrant      []string
	lockFilesForYamllint   []string
	lockFilesForShellcheck []string
	shellcheckResources    []workflow.ShellScriptResource
}

// runBatchExternalTools executes all enabled batch analysis tools sequentially without short-circuiting
// when individual tools report findings or errors.
func runBatchLinters(ctx context.Context, config CompileConfig, opts batchToolsOptions) error {
	var firstErr error

	if config.Actionlint && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := runBatchActionlintOnFiles(ctx, opts.lockFilesForActionlint, config.Verbose && !config.JSONOutput, config.Strict); err != nil {
			if config.Strict && firstErr == nil {
				firstErr = err
			}
		}
	}

	if config.Zizmor && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := runBatchZizmorOnFiles(opts.lockFilesForZizmor, config.Verbose && !config.JSONOutput, config.Strict); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func runBatchDirScanners(ctx context.Context, config CompileConfig, opts batchToolsOptions) error {
	var firstErr error

	if config.Poutine && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return err
		}
		workflowDir := opts.workflowDir
		if workflowDir == "" && len(opts.lockFilesForDirTools) > 0 {
			workflowDir = filepath.Dir(opts.lockFilesForDirTools[0])
		}
		if err := runBatchDirectoryTool("poutine", workflowDir, config.Verbose && !config.JSONOutput, config.Strict, runBatchPoutineOnDirectory); err != nil {
			if config.Strict && firstErr == nil {
				firstErr = err
			}
		}
	}

	if config.RunnerGuard && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return err
		}
		workflowDir := opts.workflowDir
		if workflowDir == "" && len(opts.lockFilesForDirTools) > 0 {
			workflowDir = filepath.Dir(opts.lockFilesForDirTools[0])
		}
		if err := runBatchDirectoryTool("runner-guard", workflowDir, config.Verbose && !config.JSONOutput, config.Strict, runBatchRunnerGuardOnDirectory); err != nil {
			if config.Strict && firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func runBatchContainerScanners(
	ctx context.Context,
	config CompileConfig,
	opts batchToolsOptions,
	stats *CompilationStats,
	validationResults *[]ValidationResult,
) (strictGrantErr error, containerErr error) {
	if config.Syft && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := runBatchSyftOnLockFiles(opts.lockFilesForSyft, config.Verbose && !config.JSONOutput, config.Strict); err != nil {
			if config.Strict && containerErr == nil {
				containerErr = err
			}
		}
	}

	if config.Grype && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := runBatchGrypeOnLockFiles(opts.lockFilesForGrype, config.Verbose && !config.JSONOutput, config.Strict); err != nil {
			if config.Strict && containerErr == nil {
				containerErr = err
			}
		}
	}

	if config.Grant && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := runBatchGrantOnLockFiles(opts.lockFilesForGrant, config.Verbose && !config.JSONOutput, config.Strict); err != nil {
			if config.Strict {
				stats.Errors++
				*validationResults = append(*validationResults, ValidationResult{
					Workflow: "grant",
					Valid:    false,
					Errors: []ValidationIssue{{
						Type:    "grant_error",
						Message: err.Error(),
					}},
				})
				if strictGrantErr == nil {
					strictGrantErr = err
				}
			}
		}
	}

	return strictGrantErr, containerErr
}

func runBatchScriptLinters(ctx context.Context, config CompileConfig, opts batchToolsOptions) error {
	var firstErr error

	if config.Yamllint && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := runBatchYamllintOnFiles(opts.lockFilesForYamllint, config.Verbose && !config.JSONOutput, config.Strict); err != nil {
			if config.Strict && firstErr == nil {
				firstErr = err
			}
		}
	}

	if config.shellcheckEnabled() && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := runBatchShellcheckOnLockFilesAndResources(ctx, opts.lockFilesForShellcheck, opts.shellcheckResources, config.Verbose && !config.JSONOutput, config.Strict); err != nil {
			if config.Strict && firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func runBatchExternalTools(
	ctx context.Context,
	config CompileConfig,
	opts batchToolsOptions,
	stats *CompilationStats,
	validationResults *[]ValidationResult,
) (strictGrantErr error, batchToolErr error) {
	if err := runBatchLinters(ctx, config, opts); err != nil && batchToolErr == nil {
		batchToolErr = err
	}

	if err := runBatchDirScanners(ctx, config, opts); err != nil && batchToolErr == nil {
		batchToolErr = err
	}

	sGrantErr, containerErr := runBatchContainerScanners(ctx, config, opts, stats, validationResults)
	if sGrantErr != nil && strictGrantErr == nil {
		strictGrantErr = sGrantErr
	}
	if containerErr != nil && batchToolErr == nil {
		batchToolErr = containerErr
	}

	if err := runBatchScriptLinters(ctx, config, opts); err != nil && batchToolErr == nil {
		batchToolErr = err
	}

	return strictGrantErr, batchToolErr
}

func appendDuplicateWorkflowNameWarnings(workflowDataList []*workflow.WorkflowData, validationResultIndexes []int, validationResults *[]ValidationResult) ([]ValidationIssue, error) {
	workflowIDsByName := make(map[string][]string)
	for _, workflowData := range workflowDataList {
		if workflowData != nil && workflowData.Name != "" {
			workflowIDsByName[workflowData.Name] = append(workflowIDsByName[workflowData.Name], workflowData.WorkflowID)
		}
	}

	warningsByName := make(map[string]ValidationIssue)
	for name, workflowIDs := range workflowIDsByName {
		if len(workflowIDs) > 1 {
			slices.Sort(workflowIDs)
			warningsByName[name] = ValidationIssue{
				Type:    "duplicate_workflow_name",
				Message: fmt.Sprintf("Duplicate workflow name %q in %s; GitHub displays them as the same agentic workflow", name, strings.Join(workflowIDs, ", ")),
			}
		}
	}

	var warnings []ValidationIssue
	reportedNames := make(map[string]struct{})
	for workflowIndex, workflowData := range workflowDataList {
		if workflowData == nil || workflowData.Name == "" {
			continue
		}

		warning, duplicate := warningsByName[workflowData.Name]
		if !duplicate {
			continue
		}

		if _, reported := reportedNames[workflowData.Name]; !reported {
			warnings = append(warnings, warning)
			reportedNames[workflowData.Name] = struct{}{}
		}

		if workflowIndex >= len(validationResultIndexes) {
			return nil, fmt.Errorf("missing validation result index for workflow %q", workflowData.WorkflowID)
		}
		resultIndex := validationResultIndexes[workflowIndex]
		if resultIndex < 0 || resultIndex >= len(*validationResults) {
			return nil, fmt.Errorf("validation result index %d for workflow %q is out of range", resultIndex, workflowData.WorkflowID)
		}
		(*validationResults)[resultIndex].Warnings = append((*validationResults)[resultIndex].Warnings, warning)
	}

	return warnings, nil
}

func displayBatchCompilationNotices(compiler *workflow.Compiler, config CompileConfig) {
	if config.JSONOutput || config.Verbose {
		return
	}

	featureUsage := compiler.GetExperimentalFeatureUsage()
	if len(featureUsage) > 0 {
		type featureCount struct {
			name  string
			count int
		}
		features := make([]featureCount, 0, len(featureUsage))
		for message, count := range featureUsage {
			features = append(features, featureCount{
				name:  strings.TrimPrefix(message, "Using experimental feature: "),
				count: count,
			})
		}
		slices.SortFunc(features, func(a, b featureCount) int {
			if a.count != b.count {
				return cmp.Compare(b.count, a.count)
			}
			return cmp.Compare(a.name, b.name)
		})

		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Experimental features in use:"))
		for _, feature := range features {
			fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("%s: %s", feature.name, formatWorkflowCount(feature.count))))
		}
	}

	if compiler.CopilotRequestsTipNeeded() {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(
			"Copilot token-based inference may be available: add permissions.copilot-requests: write. "+
				"To suppress this tip when using a PAT, set permissions.copilot-requests: none. "+
				"See https://github.github.com/gh-aw/reference/billing/",
		))
	}
}

// purgeTrackingData holds data needed for purge operations
type purgeTrackingData struct {
	existingLockFiles    []string
	existingInvalidFiles []string
	expectedLockFiles    []string
}

// collectPurgeData collects existing files for purge operations
func collectPurgeData(workflowsDir string, mdFiles []string, verbose bool) (*purgeTrackingData, error) {
	return collectPurgeDataWithPatterns(workflowsDir, mdFiles, verbose, "*.lock.yml", "*.invalid.yml")
}

// collectPurgeDataWithPatterns is the testable implementation of collectPurgeData.
// lockPattern and invalidPattern are appended to workflowsDir for the glob calls.
func collectPurgeDataWithPatterns(workflowsDir string, mdFiles []string, verbose bool, lockPattern, invalidPattern string) (*purgeTrackingData, error) {
	data := &purgeTrackingData{}

	// Find all existing files
	var err error
	data.existingLockFiles, err = filepath.Glob(filepath.Join(workflowsDir, lockPattern))
	if err != nil {
		return nil, fmt.Errorf("failed to glob existing .lock.yml files in %s: %w", workflowsDir, err)
	}
	data.existingInvalidFiles, err = filepath.Glob(filepath.Join(workflowsDir, invalidPattern))
	if err != nil {
		return nil, fmt.Errorf("failed to glob existing .invalid.yml files in %s: %w", workflowsDir, err)
	}

	// Create expected files list
	for _, mdFile := range mdFiles {
		lockFile := stringutil.MarkdownToLockFile(mdFile)
		data.expectedLockFiles = append(data.expectedLockFiles, lockFile)
	}

	if verbose {
		if len(data.existingLockFiles) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Found %d existing .lock.yml files", len(data.existingLockFiles))))
		}
		if len(data.existingInvalidFiles) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Found %d existing .invalid.yml files", len(data.existingInvalidFiles))))
		}
	}

	return data, nil
}

// runPurgeOperations runs all purge operations
func runPurgeOperations(workflowsDir string, data *purgeTrackingData, verbose bool) {
	// Errors from purge operations are logged but don't stop compilation
	_ = purgeOrphanedLockFiles(workflowsDir, data.expectedLockFiles, verbose)
	_ = purgeInvalidFiles(workflowsDir, verbose)
}

// runPostProcessing runs post-processing for specific files compilation
func runPostProcessing(
	ctx context.Context,
	compiler *workflow.Compiler,
	workflowDataList []*workflow.WorkflowData,
	compiledLockFiles []string,
	config CompileConfig,
	successCount int,
) error {
	// Get action cache
	actionCache := compiler.GetSharedActionCache()

	// Update .gitattributes (errors are non-fatal)
	_ = updateGitAttributes(successCount, actionCache, config.Verbose)

	// Generate Dependabot manifests and reconcile compiler-managed ignore entries if requested.
	if config.Dependabot && !config.NoEmit {
		if gitRoot, err := gitutil.FindGitRoot(); err == nil {
			absWorkflowDir := filepath.Join(gitRoot, config.WorkflowDir)
			if err := generateDependabotManifestsWrapper(ctx, compiler, workflowDataList, absWorkflowDir, config.ForceOverwrite, config.Strict); err != nil {
				if config.Strict {
					return err
				}
			}
			if err := compiler.ReconcileManagedDependabotIgnoresInRepo(gitRoot); err != nil {
				if config.Strict {
					return err
				}
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to reconcile compiler-managed Dependabot ignore entries: %v", err)))
			}
		}
	}

	// Reconcile the implicit action-failure expiry marker so specific-file
	// compiles agree with what a full directory compile would produce.
	// Maintenance workflow generation itself is skipped for specific-file
	// compiles because it requires parsing every workflow in the directory to
	// check for expires fields; only reconcile when using the default
	// workflow directory (custom --dir compiles and --no-emit compiles are
	// left untouched).
	if !config.NoEmit && config.WorkflowDir == "" && len(compiledLockFiles) > 0 {
		if gitRoot, err := gitutil.FindGitRoot(); err == nil {
			absWorkflowDir := getAbsoluteWorkflowDir(getWorkflowsDir(), gitRoot)
			repoConfig, err := workflow.LoadRepoConfig(gitRoot)
			if err != nil {
				repoConfig = nil
			}
			workflow.DisableDefaultActionFailureExpiryMarkersIfUnenforced(compiledLockFiles, absWorkflowDir, repoConfig)
		}
	}

	// Prune stale gh-aw-actions entries before saving
	pruneStaleActionCacheEntries(compiler, actionCache)

	// Save action cache (errors are logged but non-fatal)
	_ = saveActionCache(actionCache, config.Verbose)

	return nil
}

// runPostProcessingForDirectory runs post-processing for directory compilation
func runPostProcessingForDirectory(
	ctx context.Context,
	compiler *workflow.Compiler,
	workflowDataList []*workflow.WorkflowData,
	config CompileConfig,
	workflowsDir string,
	gitRoot string,
	successCount int,
	errorCount int,
) error {
	// Get action cache
	actionCache := compiler.GetSharedActionCache()

	// Update .gitattributes (errors are non-fatal)
	_ = updateGitAttributes(successCount, actionCache, config.Verbose)

	// Generate Dependabot manifests if requested
	if config.Dependabot && !config.NoEmit {
		absWorkflowDir := getAbsoluteWorkflowDir(workflowsDir, gitRoot)
		if err := generateDependabotManifestsWrapper(ctx, compiler, workflowDataList, absWorkflowDir, config.ForceOverwrite, config.Strict); err != nil {
			if config.Strict {
				return err
			}
		}
	}

	// Reconcile compiler-managed Dependabot ignore entries for compiler-emitted action refs.
	if config.Dependabot && !config.NoEmit {
		if err := compiler.ReconcileManagedDependabotIgnoresInRepo(gitRoot); err != nil {
			if config.Strict {
				return err
			}
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to reconcile compiler-managed Dependabot ignore entries: %v", err)))
		}
	}

	// Generate maintenance workflow if needed.
	// Skip maintenance workflow generation when using custom --dir option.
	// Keep invoking generators for empty workflowDataList so stale generated files are cleaned up.
	if !config.NoEmit && config.WorkflowDir == "" {
		absWorkflowDir := getAbsoluteWorkflowDir(workflowsDir, gitRoot)
		if err := generateMaintenanceWorkflowWrapper(ctx, compiler, workflowDataList, absWorkflowDir, gitRoot, config.Verbose, config.Strict); err != nil {
			if config.Strict {
				return err
			}
		}
		if err := generateCentralSlashCommandWorkflowWrapper(ctx, workflowDataList, absWorkflowDir, gitRoot, config.Strict); err != nil {
			if config.Strict {
				return err
			}
		}
	}

	// Prune stale gh-aw-actions entries before saving
	pruneStaleActionCacheEntries(compiler, actionCache)

	// Prune orphaned entries — entries for action versions no longer referenced
	// by any workflow in the directory (e.g. old pins left after a version bump).
	// Safe to call only after a full-directory compilation with zero compile errors.
	pruneOrphanedActionCacheEntries(compiler, actionCache, errorCount)

	// Save action cache (errors are logged but non-fatal)
	_ = saveActionCache(actionCache, config.Verbose)

	return nil
}

// outputResults outputs compilation results in the requested format
func outputResults(
	stats *CompilationStats,
	validationResults *[]ValidationResult,
	config CompileConfig,
) error {
	// Collect and display stats if requested
	if config.Stats && !config.NoEmit && !config.JSONOutput {
		var statsList []*WorkflowStats
		if len(config.MarkdownFiles) > 0 {
			statsList = collectWorkflowStatisticsWrapper(config.MarkdownFiles)
		}
		displayStatsTable(statsList)
		displayScheduleCalendar(statsList)
	}

	// Output JSON if requested
	if config.JSONOutput {
		jsonStr, err := formatValidationOutput(*validationResults)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, jsonStr)
	} else if !config.Stats {
		// Print summary for text output (skip if stats mode)
		printCompilationSummary(stats, config.ShowAllErrors)
	}

	// Display actionlint summary if enabled
	if config.Actionlint && !config.NoEmit && !config.JSONOutput {
		displayActionlintSummary()
	}

	return nil
}
