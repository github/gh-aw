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
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/gitutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileOrchestrationLog = logger.New("cli:compile_pipeline")

const fallbackCompilationErrorMessage = "compilation failed (no detailed error message available)"

// compileSpecificFiles compiles a specific list of workflow files
func compileSpecificFiles(
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

	stage := compilePipelineStage{compiler: compiler, config: config, stats: stats, results: validationResults}
	if err := stage.compile(ctx, config.MarkdownFiles, true); err != nil {
		return stage.workflows, err
	}
	if config.DryRun {
		stage.batch.workflowDir = config.dryRunWorkflowDir
	}
	strictGrantErr, batchToolErr := runBatchExternalTools(ctx, config, stage.batch, stats, validationResults)
	return stage.finishSpecific(ctx, strictGrantErr, batchToolErr)
}

func (stage *compilePipelineStage) finishSpecific(ctx context.Context, strictGrantErr, batchToolErr error) ([]*workflow.WorkflowData, error) {
	compiler, config, stats, validationResults := stage.compiler, stage.config, stage.stats, stage.results
	workflowDataList := stage.workflows

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
		if err := runPostProcessing(ctx, compiler, workflowDataList, stage.compiledLockFiles, config, stage.successes); err != nil {
			return workflowDataList, err
		}
	}

	developmentErr := enforceDevelopmentDiagnostics(config, compiler, stats, validationResults, strictGrantErr, batchToolErr)

	// Output results
	if err := outputResults(stats, validationResults, config); err != nil {
		return workflowDataList, err
	}

	return workflowDataList, compilationResultError(stage.failures, strictGrantErr, batchToolErr, developmentErr)
}

// compileAllFilesInDirectory compiles all workflow files in a directory
func compileAllFilesInDirectory(
	ctx context.Context,
	compiler *workflow.Compiler,
	config CompileConfig,
	workflowDir string,
	stats *CompilationStats,
	validationResults *[]ValidationResult,
) ([]*workflow.WorkflowData, error) {
	gitRoot, workflowsDir, mdFiles, err := findDirectoryWorkflows(workflowDir, config.Verbose)
	if err != nil {
		return nil, err
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

	var purgeData *purgeTrackingData
	if config.Purge {
		purgeData, err = collectPurgeData(workflowsDir, mdFiles, config.Verbose)
		if err != nil {
			return nil, fmt.Errorf("failed to collect existing files for purge: %w", err)
		}
	}

	stage := compilePipelineStage{compiler: compiler, config: config, stats: stats, results: validationResults}
	if err := stage.compile(ctx, mdFiles, false); err != nil {
		return stage.workflows, err
	}
	stage.batch.workflowDir = workflowsDir
	if config.DryRun {
		stage.batch.workflowDir = firstNonEmpty(config.dryRunWorkflowDir, workflowsDir)
	}
	strictGrantErr, batchToolErr := runBatchExternalTools(ctx, config, stage.batch, stats, validationResults)
	return stage.finishDirectory(ctx, gitRoot, workflowsDir, mdFiles, purgeData, strictGrantErr, batchToolErr)
}

func findDirectoryWorkflows(workflowDir string, verbose bool) (string, string, []string, error) {
	// Find git root for consistent behavior
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return "", "", nil, fmt.Errorf("compile without arguments requires being in a git repository: %w", err)
	}
	compileOrchestrationLog.Printf("Found git root: %s", gitRoot)

	// Compile all markdown files in the specified workflow directory
	workflowsDir := filepath.Join(gitRoot, workflowDir)
	if _, err := os.Stat(workflowsDir); errors.Is(err, fs.ErrNotExist) {
		return "", "", nil, fmt.Errorf("the %s directory does not exist in git root (%s)", workflowDir, gitRoot)
	}

	compileOrchestrationLog.Printf("Scanning for markdown files in %s", workflowsDir)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Scanning for markdown files in "+workflowsDir))
	}

	// Find and filter markdown files (shared helper keeps logic in one place)
	mdFiles, err := getMarkdownWorkflowFiles(workflowsDir)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to find markdown files: %w", err)
	}
	mdFiles, err = filterMarkdownFilesWithFrontmatter(mdFiles)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to filter markdown files: %w", err)
	}

	if len(mdFiles) == 0 {
		return "", "", nil, fmt.Errorf("no workflow markdown files found in %s (workflow files must start with a frontmatter opener on the first line)", workflowsDir)
	}
	return gitRoot, workflowsDir, mdFiles, nil
}

func (stage *compilePipelineStage) finishDirectory(ctx context.Context, gitRoot, workflowsDir string, mdFiles []string, purgeData *purgeTrackingData, strictGrantErr, batchToolErr error) ([]*workflow.WorkflowData, error) {
	compiler, config, stats, validationResults := stage.compiler, stage.config, stage.stats, stage.results
	workflowDataList, successCount, errorCount := stage.workflows, stage.successes, stage.failures
	if strictGrantErr != nil || batchToolErr != nil {
		errorCount++
	}

	if err := stage.reportDirectoryNotices(); err != nil {
		return workflowDataList, err
	}

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

	return workflowDataList, compilationResultError(stage.failures, strictGrantErr, batchToolErr, developmentErr)
}

func compilationResultError(failures int, strictGrantErr, batchToolErr, developmentErr error) error {
	if strictGrantErr != nil {
		return strictGrantErr
	}
	if batchToolErr != nil {
		return batchToolErr
	}
	if failures > 0 {
		return errors.New("compilation failed")
	}
	return developmentErr
}
