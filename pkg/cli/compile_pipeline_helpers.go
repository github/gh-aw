package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workflow"
)

type compilePipelineFileOutcome struct {
	filePath      string
	result        compileWorkflowFileResult
	resolutionErr error
}

type compilePipelineFilesResult struct {
	workflowDataList                []*workflow.WorkflowData
	compiledLockFiles               []string
	workflowValidationResultIndexes []int
	batchTools                      batchToolsOptions
	compiledCount                   int
	successCount                    int
	errorCount                      int
}

func compilePipelineFiles(
	ctx context.Context,
	compiler *workflow.Compiler,
	files []string,
	config CompileConfig,
	targeted bool,
	stats *CompilationStats,
	validationResults *[]ValidationResult,
) (*compilePipelineFilesResult, error) {
	result := &compilePipelineFilesResult{}
	shouldValidate := compilePipelineShouldValidate(config)
	for _, file := range files {
		if err := cancelCompileBetweenFiles(ctx); err != nil {
			return result, err
		}
		stats.Total++
		outcome := compilePipelineFile(ctx, compiler, file, config, shouldValidate, targeted)
		if outcome.resolutionErr != nil {
			recordWorkflowResolutionFailure(outcome, stats, result, validationResults, config)
			continue
		}
		if targeted && len(*validationResults) == 0 {
			appendMCPCachedModelInventoryWarning(config, &outcome.result.validationResult)
		}
		recordCompilePipelineFile(outcome, targeted, config, stats, result)
		if !targeted && outcome.result.success && outcome.result.workflowData != nil {
			result.workflowValidationResultIndexes = append(result.workflowValidationResultIndexes, len(*validationResults))
		}
		*validationResults = append(*validationResults, outcome.result.validationResult)
		writeMCPCompileProgress(config.MCPProgressFile, *validationResults)
	}
	return result, nil
}

func cancelCompileBetweenFiles(ctx context.Context) error {
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Operation cancelled"))
		return ctx.Err()
	default:
		return nil
	}
}

func compilePipelineShouldValidate(config CompileConfig) bool {
	shouldValidate := config.Validate || config.ForceRefreshActionPins
	if config.ForceRefreshActionPins && !config.Validate {
		compileOrchestrationLog.Print("Automatically enabling action SHA validation due to --force-refresh-action-pins")
	}
	return shouldValidate
}

func compilePipelineFile(
	ctx context.Context,
	compiler *workflow.Compiler,
	file string,
	config CompileConfig,
	shouldValidate bool,
	targeted bool,
) compilePipelineFileOutcome {
	filePath := file
	if targeted {
		compileOrchestrationLog.Printf("Resolving workflow file: %s", file)
		resolvedFile, err := resolveWorkflowFile(file, config.Verbose)
		if err != nil {
			return compilePipelineFileOutcome{
				filePath: file,
				result: compileWorkflowFileResult{validationResult: ValidationResult{
					Workflow: file, Valid: false, Errors: []ValidationIssue{},
					Warnings: []ValidationIssue{},
				}},
				resolutionErr: err,
			}
		}
		filePath = resolvedFile
		compileOrchestrationLog.Printf("Resolved to: %s", resolvedFile)
	}

	result := compileWorkflowFile(
		ctx, compiler, filePath, compileWorkflowFileOptions{
			verbose:        config.Verbose,
			jsonOutput:     config.JSONOutput,
			noEmit:         config.NoEmit,
			strict:         config.Strict,
			validate:       shouldValidate,
			failOnWarnings: config.DryRun,
		},
	)
	if targeted {
		result.validationResult.Workflow = filepath.Base(filePath)
	}
	return compilePipelineFileOutcome{filePath: filePath, result: result}
}

func recordWorkflowResolutionFailure(
	outcome compilePipelineFileOutcome,
	stats *CompilationStats,
	result *compilePipelineFilesResult,
	validationResults *[]ValidationResult,
	config CompileConfig,
) {
	result.errorCount++
	stats.Errors++
	trackWorkflowFailure(stats, outcome.filePath, 1, []string{outcome.resolutionErr.Error()})
	validationResult := outcome.result.validationResult
	validationResult.Errors = append(validationResult.Errors, ValidationIssue{
		Type: "resolution_error", Message: outcome.resolutionErr.Error(),
	})
	*validationResults = append(*validationResults, validationResult)
	writeMCPCompileProgress(config.MCPProgressFile, *validationResults)
}

func recordCompilePipelineFile(
	outcome compilePipelineFileOutcome,
	targeted bool,
	config CompileConfig,
	stats *CompilationStats,
	result *compilePipelineFilesResult,
) {
	if !outcome.result.success {
		recordWorkflowCompilationFailure(outcome, stats, result)
		return
	}
	result.compiledCount++
	result.successCount++
	stats.Succeeded++
	if outcome.result.workflowData != nil {
		result.workflowDataList = append(result.workflowDataList, outcome.result.workflowData)
	}
	collectCompileBatchInputs(outcome, targeted, config, result)
}

func recordWorkflowCompilationFailure(
	outcome compilePipelineFileOutcome,
	stats *CompilationStats,
	result *compilePipelineFilesResult,
) {
	var messages []string
	for _, issue := range outcome.result.validationResult.Errors {
		messages = append(messages, issue.Message)
	}
	if len(messages) == 0 {
		messages = []string{fallbackCompilationErrorMessage}
	}
	result.errorCount++
	stats.Errors += len(messages)
	trackWorkflowFailure(stats, outcome.filePath, len(messages), messages)
}

func collectCompileBatchInputs(
	outcome compilePipelineFileOutcome,
	targeted bool,
	config CompileConfig,
	result *compilePipelineFilesResult,
) {
	if config.NoEmit || outcome.result.lockFile == "" {
		return
	}
	if _, err := os.Stat(outcome.result.lockFile); err != nil {
		return
	}
	lockFile := outcome.result.lockFile
	if targeted {
		result.compiledLockFiles = append(result.compiledLockFiles, lockFile)
	}
	if config.Actionlint {
		result.batchTools.lockFilesForActionlint = append(result.batchTools.lockFilesForActionlint, lockFile)
	}
	if config.Zizmor {
		result.batchTools.lockFilesForZizmor = append(result.batchTools.lockFilesForZizmor, lockFile)
	}
	if config.Poutine || config.RunnerGuard {
		result.batchTools.lockFilesForDirTools = append(result.batchTools.lockFilesForDirTools, lockFile)
	}
	if config.Syft {
		result.batchTools.lockFilesForSyft = append(result.batchTools.lockFilesForSyft, lockFile)
	}
	if config.Grype {
		result.batchTools.lockFilesForGrype = append(result.batchTools.lockFilesForGrype, lockFile)
	}
	if config.Grant {
		result.batchTools.lockFilesForGrant = append(result.batchTools.lockFilesForGrant, lockFile)
	}
	if config.Yamllint {
		result.batchTools.lockFilesForYamllint = append(result.batchTools.lockFilesForYamllint, lockFile)
	}
	if config.shellcheckEnabled() {
		result.batchTools.lockFilesForShellcheck = append(result.batchTools.lockFilesForShellcheck, lockFile)
		result.batchTools.shellcheckResources = append(result.batchTools.shellcheckResources, outcome.result.workflowData.ShellScriptResources()...)
	}
}

func runCompilePipelineBatchTools(
	ctx context.Context,
	config CompileConfig,
	workflowDir string,
	files *compilePipelineFilesResult,
	stats *CompilationStats,
	validationResults *[]ValidationResult,
) (error, error) {
	files.batchTools.workflowDir = workflowDir
	return runBatchExternalTools(ctx, config, files.batchTools, stats, validationResults)
}

func compilePipelineFinalError(
	workflowDataList []*workflow.WorkflowData,
	errorCount int,
	strictGrantErr error,
	batchToolErr error,
	developmentErr error,
) ([]*workflow.WorkflowData, error) {
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

func finishSpecificCompile(
	ctx context.Context,
	compiler *workflow.Compiler,
	config CompileConfig,
	stats *CompilationStats,
	validationResults *[]ValidationResult,
	files *compilePipelineFilesResult,
) ([]*workflow.WorkflowData, error) {
	workflowDir := ""
	if config.DryRun {
		workflowDir = config.dryRunWorkflowDir
	}
	strictGrantErr, batchToolErr := runCompilePipelineBatchTools(ctx, config, workflowDir, files, stats, validationResults)
	if strictGrantErr != nil || batchToolErr != nil {
		files.errorCount++
	}
	displayCompileNotices(compiler, config, stats)
	if !config.NoEmit {
		if err := runPostProcessing(ctx, compiler, files.workflowDataList, files.compiledLockFiles, config, files.compiledCount); err != nil {
			return files.workflowDataList, err
		}
	}
	developmentErr := enforceDevelopmentDiagnostics(config, compiler, stats, validationResults, strictGrantErr, batchToolErr)
	if err := outputResults(stats, validationResults, config); err != nil {
		return files.workflowDataList, err
	}
	return compilePipelineFinalError(files.workflowDataList, files.errorCount, strictGrantErr, batchToolErr, developmentErr)
}

func displayCompileNotices(compiler *workflow.Compiler, config CompileConfig, stats *CompilationStats) {
	stats.Warnings = compiler.GetWarningCount()
	displayBatchCompilationNotices(compiler, config)
	displayScheduleWarnings(compiler, config.JSONOutput)
	displaySafeUpdateWarnings(compiler, config.JSONOutput)
}

func finishDirectoryCompile(
	opts directoryCompileOptions,
) ([]*workflow.WorkflowData, error) {
	ctx := opts.ctx
	compiler := opts.compiler
	config := opts.config
	stats := opts.stats
	validationResults := opts.validationResults
	files := opts.files
	workflowsDir := opts.workflowsDir
	gitRoot := opts.gitRoot
	mdFiles := opts.mdFiles
	purgeData := opts.purgeData
	dryRunWorkflowDir := ""
	if config.DryRun {
		dryRunWorkflowDir = config.dryRunWorkflowDir
	}
	strictGrantErr, batchToolErr := runCompilePipelineBatchTools(
		ctx, config, firstNonEmpty(dryRunWorkflowDir, workflowsDir), files, stats, validationResults,
	)
	if strictGrantErr != nil || batchToolErr != nil {
		files.errorCount++
	}
	displayCentralizedSlashCommandRecommendation(compiler, files.workflowDataList, config.JSONOutput)
	duplicateWarnings, err := appendDuplicateWorkflowNameWarnings(
		files.workflowDataList, files.workflowValidationResultIndexes, validationResults,
	)
	if err != nil {
		return files.workflowDataList, err
	}
	displayDuplicateWorkflowWarnings(duplicateWarnings, config.JSONOutput)
	stats.Warnings = compiler.GetWarningCount() + len(duplicateWarnings)
	displayBatchCompilationNotices(compiler, config)
	displayScheduleWarnings(compiler, config.JSONOutput)
	displaySafeUpdateWarnings(compiler, config.JSONOutput)
	displayDirectoryCompileSuccess(config, files.successCount, len(mdFiles))
	if config.Purge && purgeData != nil {
		runPurgeOperations(workflowsDir, purgeData, config.Verbose)
	}
	if !config.NoEmit {
		if err := runPostProcessingForDirectory(
			ctx, compiler, files.workflowDataList, config, workflowsDir, gitRoot, files.successCount, files.errorCount,
		); err != nil {
			return files.workflowDataList, err
		}
	}
	developmentErr := enforceDevelopmentDiagnostics(config, compiler, stats, validationResults, strictGrantErr, batchToolErr)
	populateCompileStatsFiles(&config, mdFiles)
	if err := outputResults(stats, validationResults, config); err != nil {
		return files.workflowDataList, err
	}
	return compilePipelineFinalError(files.workflowDataList, files.errorCount, strictGrantErr, batchToolErr, developmentErr)
}

type directoryCompileOptions struct {
	ctx               context.Context
	compiler          *workflow.Compiler
	config            CompileConfig
	stats             *CompilationStats
	validationResults *[]ValidationResult
	files             *compilePipelineFilesResult
	workflowsDir      string
	gitRoot           string
	mdFiles           []string
	purgeData         *purgeTrackingData
}

func displayDuplicateWorkflowWarnings(warnings []ValidationIssue, jsonOutput bool) {
	if jsonOutput {
		return
	}
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(warning.Message))
	}
}

func displayDirectoryCompileSuccess(config CompileConfig, successCount, total int) {
	if config.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr(
			fmt.Sprintf("Successfully compiled %d out of %d workflow files", successCount, total),
		))
	}
}

func populateCompileStatsFiles(config *CompileConfig, mdFiles []string) {
	if config.Stats && len(config.MarkdownFiles) == 0 {
		config.MarkdownFiles = mdFiles
	}
}
