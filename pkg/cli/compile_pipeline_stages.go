package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workflow"
)

type compilePipelineStage struct {
	compiler          *workflow.Compiler
	config            CompileConfig
	stats             *CompilationStats
	results           *[]ValidationResult
	workflows         []*workflow.WorkflowData
	resultIndexes     []int
	compiledLockFiles []string
	batch             batchToolsOptions
	successes         int
	failures          int
}

func (stage *compilePipelineStage) compile(ctx context.Context, files []string, resolve bool) error {
	validate := stage.config.Validate || stage.config.ForceRefreshActionPins
	if stage.config.ForceRefreshActionPins && !stage.config.Validate {
		compileOrchestrationLog.Print("Automatically enabling action SHA validation due to --force-refresh-action-pins")
	}

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Operation cancelled"))
			return err
		}
		stage.stats.Total++
		if resolve {
			compileOrchestrationLog.Printf("Resolving workflow file: %s", file)
			resolved, err := resolveWorkflowFile(file, stage.config.Verbose)
			if err != nil {
				stage.failures++
				stage.stats.Errors++
				trackWorkflowFailure(stage.stats, file, 1, []string{err.Error()})
				*stage.results = append(*stage.results, ValidationResult{
					Workflow: file, Valid: false,
					Errors:   []ValidationIssue{{Type: "resolution_error", Message: err.Error()}},
					Warnings: []ValidationIssue{},
				})
				writeMCPCompileProgress(stage.config.MCPProgressFile, *stage.results)
				continue
			}
			file = resolved
			compileOrchestrationLog.Printf("Resolved to: %s", file)
		}

		result := compileWorkflowFile(ctx, stage.compiler, file, compileWorkflowFileOptions{
			verbose: stage.config.Verbose, jsonOutput: stage.config.JSONOutput,
			noEmit: stage.config.NoEmit, strict: stage.config.Strict,
			validate: validate, failOnWarnings: stage.config.DryRun,
		})
		stage.recordFile(file, result)
		if resolve && len(*stage.results) == 0 {
			appendMCPCachedModelInventoryWarning(stage.config, &result.validationResult)
		}
		*stage.results = append(*stage.results, result.validationResult)
		writeMCPCompileProgress(stage.config.MCPProgressFile, *stage.results)
	}
	return nil
}

func (stage *compilePipelineStage) recordFile(file string, result compileWorkflowFileResult) {
	if !result.success {
		var messages []string
		for _, issue := range result.validationResult.Errors {
			messages = append(messages, issue.Message)
		}
		if len(messages) == 0 {
			messages = []string{fallbackCompilationErrorMessage}
		}
		stage.failures++
		stage.stats.Errors += len(messages)
		trackWorkflowFailure(stage.stats, file, len(messages), messages)
		return
	}

	stage.successes++
	stage.stats.Succeeded++
	if result.workflowData != nil {
		stage.workflows = append(stage.workflows, result.workflowData)
		stage.resultIndexes = append(stage.resultIndexes, len(*stage.results))
	}
	if stage.config.NoEmit || result.lockFile == "" {
		return
	}
	if _, err := os.Stat(result.lockFile); err != nil {
		return
	}
	stage.compiledLockFiles = append(stage.compiledLockFiles, result.lockFile)
	if stage.config.Actionlint {
		stage.batch.lockFilesForActionlint = append(stage.batch.lockFilesForActionlint, result.lockFile)
	}
	if stage.config.Zizmor {
		stage.batch.lockFilesForZizmor = append(stage.batch.lockFilesForZizmor, result.lockFile)
	}
	if stage.config.Poutine || stage.config.RunnerGuard {
		stage.batch.lockFilesForDirTools = append(stage.batch.lockFilesForDirTools, result.lockFile)
	}
	if stage.config.Syft {
		stage.batch.lockFilesForSyft = append(stage.batch.lockFilesForSyft, result.lockFile)
	}
	if stage.config.Grype {
		stage.batch.lockFilesForGrype = append(stage.batch.lockFilesForGrype, result.lockFile)
	}
	if stage.config.Grant {
		stage.batch.lockFilesForGrant = append(stage.batch.lockFilesForGrant, result.lockFile)
	}
	if stage.config.Yamllint {
		stage.batch.lockFilesForYamllint = append(stage.batch.lockFilesForYamllint, result.lockFile)
	}
	if stage.config.shellcheckEnabled() {
		stage.batch.lockFilesForShellcheck = append(stage.batch.lockFilesForShellcheck, result.lockFile)
		stage.batch.shellcheckResources = append(stage.batch.shellcheckResources, result.workflowData.ShellScriptResources()...)
	}
}
