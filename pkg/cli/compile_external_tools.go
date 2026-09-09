// This file provides external tool runners for workflow compilation.
//
// This file contains functions that invoke external analysis tools
// (actionlint, zizmor, poutine, runner-guard, syft, grype, grant) on compiled workflow files.
// (actionlint, zizmor, poutine, runner-guard, syft, grype, grant, yamllint)
// on compiled workflow files.
//
// # Organization Rationale
//
// These external tool runner functions are grouped here because they:
//   - Invoke third-party analysis tools (not compilation logic)
//   - Operate on compiled lock files as a post-compilation step
//   - Have a clear domain focus (external tooling integration)
//   - Keep compile_batch_operations.go focused on batch file management
//
// # Key Functions
//
// External Tool Runners:
//   - RunActionlintOnFiles() - Run actionlint on multiple lock files
//   - RunZizmorOnFiles() - Run zizmor on multiple lock files
//   - RunPoutineOnDirectory() - Run poutine security scanner on a directory
//   - RunRunnerGuardOnDirectory() - Run runner-guard taint analysis on a directory
//   - RunGrantOnLockFiles() - Run grant license scanning on container images
//   - RunYamllintOnFiles() - Run yamllint YAML linter on multiple lock files

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileExternalToolsLog = logger.New("cli:compile_external_tools")

// RunActionlintOnFiles runs actionlint on multiple lock files in a single batch.
// This is more efficient than running actionlint once per file.
func RunActionlintOnFiles(ctx context.Context, lockFiles []string, verbose bool, strict bool) error {
	return runBatchLockFileTool("actionlint", lockFiles, verbose, strict, func(files []string, runVerbose bool, runStrict bool) error {
		return runActionlintOnFiles(ctx, files, runVerbose, runStrict)
	})
}

// RunZizmorOnFiles runs zizmor on multiple lock files in a single batch.
// This is more efficient than running zizmor once per file.
func RunZizmorOnFiles(lockFiles []string, verbose bool, strict bool) error {
	return runBatchLockFileTool("zizmor", lockFiles, verbose, strict, runZizmorOnFiles)
}

// RunPoutineOnDirectory runs poutine security scanner once on a directory.
// Poutine scans all workflows in a directory, so it only needs to run once.
func RunPoutineOnDirectory(workflowDir string, verbose bool, strict bool) error {
	return runPoutineOnDirectory(workflowDir, verbose, strict)
}

// RunRunnerGuardOnDirectory runs runner-guard taint analysis scanner once on a directory.
// Runner-guard scans all workflows in a directory, so it only needs to run once.
func RunRunnerGuardOnDirectory(workflowDir string, verbose bool, strict bool) error {
	return runRunnerGuardOnDirectory(workflowDir, verbose, strict)
}

// RunGrypeOnLockFiles runs the grype vulnerability scanner on container images extracted
// from the gh-aw-manifest headers in the provided lock files.
// Images are deduplicated by pinned reference, and results are cached per image.
func RunGrypeOnLockFiles(lockFiles []string, verbose bool, strict bool) error {
	return runBatchLockFileTool("grype", lockFiles, verbose, strict, runGrypeOnLockFiles)
}

// RunGrantOnLockFiles runs the grant license scanner on container images extracted
// from the gh-aw-manifest headers in the provided lock files.
func RunGrantOnLockFiles(lockFiles []string, verbose bool, strict bool) error {
	return runBatchLockFileTool("grant", lockFiles, verbose, strict, runGrantOnLockFiles)
}

// RunYamllintOnFiles runs yamllint on multiple lock files in a single batch.
// This is more efficient than running yamllint once per file.
func RunYamllintOnFiles(lockFiles []string, verbose bool, strict bool) error {
	return runBatchLockFileTool("yamllint", lockFiles, verbose, strict, runYamllintOnFiles)
}

// RunShellcheckOnLockFilesAndResources runs shellcheck on run steps extracted
// from lock files and shell script resources defined in workflow frontmatter.
func RunShellcheckOnLockFilesAndResources(ctx context.Context, lockFiles []string, resources []workflow.ShellScriptResource, verbose bool, strict bool) error {
	if len(lockFiles) == 0 && len(resources) == 0 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running shellcheck on run steps (0 lock files and 0 frontmatter resources found)"))
		compileExternalToolsLog.Printf("No shell script resources to process with shellcheck")
		return nil
	}

	compileExternalToolsLog.Printf("Running batch shellcheck on %d lock files and %d frontmatter resources", len(lockFiles), len(resources))
	return handleBatchToolError("shellcheck", runShellcheckOnLockFilesAndResources(ctx, lockFiles, resources, verbose, strict), strict, verbose)
}

// RunSyftOnLockFiles runs the syft SBOM scanner on container images extracted
// from the gh-aw-manifest headers in the provided lock files.
func RunSyftOnLockFiles(lockFiles []string, verbose bool, strict bool) error {
	return runBatchLockFileTool("syft", lockFiles, verbose, strict, runSyftOnLockFiles)
}

// runBatchLockFileTool runs a batch tool on lock files with uniform error handling.
// Even when there are zero lock files to process, an explicit stderr marker is
// emitted so downstream completeness checks (e.g. static-analysis-report.md) can
// distinguish "tool ran with zero input" from "tool was never invoked".
func runBatchLockFileTool(toolName string, lockFiles []string, verbose bool, strict bool, runner func([]string, bool, bool) error) error {
	if len(lockFiles) == 0 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf("Running %s (0 lock files found)", toolName)))
		compileExternalToolsLog.Printf("No lock files to process with %s", toolName)
		return nil
	}

	compileExternalToolsLog.Printf("Running batch %s on %d lock files", toolName, len(lockFiles))

	return handleBatchToolError(toolName, runner(lockFiles, verbose, strict), strict, verbose)
}

// runBatchDirectoryTool runs a directory-based batch tool with uniform error handling
func runBatchDirectoryTool(toolName string, workflowDir string, verbose bool, strict bool, runner func(string, bool, bool) error) error {
	compileExternalToolsLog.Printf("Running batch %s on directory: %s", toolName, workflowDir)

	return handleBatchToolError(toolName, runner(workflowDir, verbose, strict), strict, verbose)
}

// handleBatchToolError applies uniform strict/non-strict error handling for batch tool results.
// In strict mode, errors are returned wrapped. In non-strict mode, errors are logged as warnings.
func handleBatchToolError(toolName string, err error, strict, verbose bool) error {
	if err == nil {
		return nil
	}
	var fatal *fatalFindingError
	if strict || errors.As(err, &fatal) {
		return fmt.Errorf("%s failed: %w", toolName, err)
	}
	// In non-strict mode, errors are warnings
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("%s warnings: %v", toolName, err)))
	}
	return nil
}
