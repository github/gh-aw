package cli

import (
	"context"
	"path/filepath"

	"github.com/github/gh-aw/pkg/workflow"
)

// Batch tool entry points are exposed as package-level function variables so
// tests can override them to verify the batch pipeline invokes every enabled
// scanner in order, without short-circuiting, and without depending on the
// underlying external tool binaries/Docker images being available.
var (
	runBatchActionlintOnFiles   = RunActionlintOnFiles
	runBatchZizmorOnFiles       = RunZizmorOnFiles
	runBatchDryRunZizmorOnFiles = func(files []string, verbose, strict bool) error {
		return runZizmorOnFilesWithOptions(files, verbose, strict, zizmorResultOptions{dryRun: true})
	}
	runBatchPoutineOnDirectory                = RunPoutineOnDirectory
	runBatchRunnerGuardOnDirectory            = RunRunnerGuardOnDirectory
	runBatchSyftOnLockFiles                   = RunSyftOnLockFiles
	runBatchGrypeOnLockFiles                  = RunGrypeOnLockFiles
	runBatchGrantOnLockFiles                  = RunGrantOnLockFiles
	runBatchYamllintOnFiles                   = RunYamllintOnFiles
	runBatchShellcheckOnLockFilesAndResources = RunShellcheckOnLockFilesAndResources
)

type batchToolsOptions struct {
	reportResult           func(string, int, error)
	filterResult           func(string, error) error
	dryRunZizmor           bool
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

func collectBatchToolInputs(config CompileConfig, fileResult compileWorkflowFileResult, opts *batchToolsOptions) {
	lockFile := fileResult.lockFile
	if config.Actionlint {
		opts.lockFilesForActionlint = append(opts.lockFilesForActionlint, lockFile)
	}
	if config.Zizmor {
		opts.lockFilesForZizmor = append(opts.lockFilesForZizmor, lockFile)
	}
	if config.Poutine || config.RunnerGuard {
		opts.lockFilesForDirTools = append(opts.lockFilesForDirTools, lockFile)
	}
	if config.Syft {
		opts.lockFilesForSyft = append(opts.lockFilesForSyft, lockFile)
	}
	if config.Grype {
		opts.lockFilesForGrype = append(opts.lockFilesForGrype, lockFile)
	}
	if config.Grant {
		opts.lockFilesForGrant = append(opts.lockFilesForGrant, lockFile)
	}
	if config.Yamllint {
		opts.lockFilesForYamllint = append(opts.lockFilesForYamllint, lockFile)
	}
	if config.shellcheckEnabled() {
		opts.lockFilesForShellcheck = append(opts.lockFilesForShellcheck, lockFile)
		opts.shellcheckResources = append(opts.shellcheckResources, fileResult.workflowData.ShellScriptResources()...)
	}
}

func (opts batchToolsOptions) recordResult(tool string, inputs int, err error) error {
	if opts.filterResult != nil {
		err = opts.filterResult(tool, err)
	}
	if opts.reportResult != nil {
		opts.reportResult(tool, inputs, err)
	}
	return err
}

// runBatchExternalTools executes all enabled batch analysis tools sequentially without short-circuiting
// when individual tools report findings or errors.
func runBatchLinters(ctx context.Context, config CompileConfig, opts batchToolsOptions) error {
	var firstErr error

	if config.Actionlint && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := runBatchActionlintOnFiles(ctx, opts.lockFilesForActionlint, config.Verbose && !config.JSONOutput, config.Strict)
		err = opts.recordResult("actionlint", len(opts.lockFilesForActionlint), err)
		if err != nil {
			if config.Strict && firstErr == nil {
				firstErr = err
			}
		}
	}

	if config.Zizmor && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		if opts.dryRunZizmor || config.DryRun && config.dryRunBaseline != nil {
			err = runBatchDryRunZizmorOnFiles(opts.lockFilesForZizmor, config.Verbose && !config.JSONOutput, config.Strict)
		} else {
			err = runBatchZizmorOnFiles(opts.lockFilesForZizmor, config.Verbose && !config.JSONOutput, config.Strict)
		}
		err = opts.recordResult("zizmor", len(opts.lockFilesForZizmor), err)
		if err != nil {
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
		err := runBatchDirectoryTool("poutine", workflowDir, config.Verbose && !config.JSONOutput, config.Strict, runBatchPoutineOnDirectory)
		err = opts.recordResult("poutine", len(opts.lockFilesForDirTools), err)
		if err != nil {
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
		err := runBatchDirectoryTool("runner-guard", workflowDir, config.Verbose && !config.JSONOutput, config.Strict, runBatchRunnerGuardOnDirectory)
		err = opts.recordResult("runner-guard", len(opts.lockFilesForDirTools), err)
		if err != nil {
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
		err := runBatchSyftOnLockFiles(opts.lockFilesForSyft, config.Verbose && !config.JSONOutput, config.Strict)
		err = opts.recordResult("syft", len(opts.lockFilesForSyft), err)
		if err != nil {
			if config.Strict && containerErr == nil {
				containerErr = err
			}
		}
	}

	if config.Grype && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := runBatchGrypeOnLockFiles(opts.lockFilesForGrype, config.Verbose && !config.JSONOutput, config.Strict)
		err = opts.recordResult("grype", len(opts.lockFilesForGrype), err)
		if err != nil {
			if config.Strict && containerErr == nil {
				containerErr = err
			}
		}
	}

	if config.Grant && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := runBatchGrantOnLockFiles(opts.lockFilesForGrant, config.Verbose && !config.JSONOutput, config.Strict)
		err = opts.recordResult("grant", len(opts.lockFilesForGrant), err)
		if err != nil {
			if config.Strict {
				if !config.DryRun {
					stats.Errors++
					*validationResults = append(*validationResults, ValidationResult{
						Workflow: "grant",
						Valid:    false,
						Errors: []ValidationIssue{{
							Type:    "grant_error",
							Message: err.Error(),
						}},
					})
				}
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
		err := runBatchYamllintOnFiles(opts.lockFilesForYamllint, config.Verbose && !config.JSONOutput, config.Strict)
		err = opts.recordResult("yamllint", len(opts.lockFilesForYamllint), err)
		if err != nil {
			if config.Strict && firstErr == nil {
				firstErr = err
			}
		}
	}

	if config.shellcheckEnabled() && !config.NoEmit {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := runBatchShellcheckOnLockFilesAndResources(ctx, opts.lockFilesForShellcheck, opts.shellcheckResources, config.Verbose && !config.JSONOutput, config.Strict)
		err = opts.recordResult("shellcheck", len(opts.lockFilesForShellcheck)+len(opts.shellcheckResources), err)
		if err != nil {
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
	scannerResults := make(map[string]error)
	ctx = context.WithValue(ctx, compileScannerReporterKey{}, func(tool string, err error) {
		scannerResults[tool] = err
	})
	previousReport := opts.reportResult
	if config.DryRun {
		if config.dryRunBaseline != nil {
			opts.filterResult = func(tool string, err error) error {
				diff, filteredErr := config.dryRunBaseline.filter(tool, err)
				if config.dryRunReport != nil && diff != nil {
					check := config.dryRunReport.Scanners[tool]
					check.Differential = diff
					config.dryRunReport.Scanners[tool] = check
				}
				return filteredErr
			}
		}
		opts.reportResult = func(tool string, inputs int, err error) {
			recordDryRunScannerResult(config.dryRunReport, tool, inputs, err)
			appendDryRunScannerResult(tool, scannerResults[tool], err, stats, validationResults)
			if previousReport != nil {
				previousReport(tool, inputs, err)
			}
		}
	} else {
		opts.reportResult = func(tool string, inputs int, err error) {
			diagnostics, reported := scannerResults[tool]
			if !reported {
				diagnostics = err
			}
			// Grant already emits its strict batch result.
			if tool != "grant" || !config.Strict {
				appendCompileScannerResult(tool, diagnostics, config.Strict && err != nil, stats, validationResults)
			}
			if previousReport != nil {
				previousReport(tool, inputs, err)
			}
		}
	}
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
