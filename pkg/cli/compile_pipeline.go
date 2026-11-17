// This file provides main orchestration logic for workflow compilation.
//
// It coordinates compilation of specific files or all workflow files in a
// directory while the pipeline phases live in focused companion files.
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
	files, err := compilePipelineFiles(ctx, compiler, config.MarkdownFiles, config, true, stats, validationResults)
	if err != nil {
		return files.workflowDataList, err
	}
	return finishSpecificCompile(ctx, compiler, config, stats, validationResults, files)
}

func compileAllFilesInDirectory(
	ctx context.Context,
	compiler *workflow.Compiler,
	config CompileConfig,
	workflowDir string,
	stats *CompilationStats,
	validationResults *[]ValidationResult,
) ([]*workflow.WorkflowData, error) {
	gitRoot, workflowsDir, mdFiles, err := findWorkflowsForDirectoryCompile(workflowDir, config.Verbose)
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
	purgeData, err := collectDirectoryPurgeData(config, workflowsDir, mdFiles)
	if err != nil {
		return nil, err
	}
	files, err := compilePipelineFiles(ctx, compiler, mdFiles, config, false, stats, validationResults)
	if err != nil {
		return files.workflowDataList, err
	}
	return finishDirectoryCompile(directoryCompileOptions{
		ctx: ctx, compiler: compiler, config: config, stats: stats, validationResults: validationResults,
		files: files, workflowsDir: workflowsDir, gitRoot: gitRoot, mdFiles: mdFiles, purgeData: purgeData,
	})
}

func findWorkflowsForDirectoryCompile(workflowDir string, verbose bool) (string, string, []string, error) {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return "", "", nil, fmt.Errorf("compile without arguments requires being in a git repository: %w", err)
	}
	compileOrchestrationLog.Printf("Found git root: %s", gitRoot)
	workflowsDir := filepath.Join(gitRoot, workflowDir)
	if _, err := os.Stat(workflowsDir); errors.Is(err, fs.ErrNotExist) {
		return "", "", nil, fmt.Errorf("the %s directory does not exist in git root (%s)", workflowDir, gitRoot)
	}
	compileOrchestrationLog.Printf("Scanning for markdown files in %s", workflowsDir)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Scanning for markdown files in "+workflowsDir))
	}
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

func collectDirectoryPurgeData(config CompileConfig, workflowsDir string, mdFiles []string) (*purgeTrackingData, error) {
	if !config.Purge {
		return nil, nil
	}
	data, err := collectPurgeData(workflowsDir, mdFiles, config.Verbose)
	if err != nil {
		return nil, fmt.Errorf("failed to collect existing files for purge: %w", err)
	}
	return data, nil
}
