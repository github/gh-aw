package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileOrchestratorLog = logger.New("cli:compile_orchestrator")
var compileUpdateContainerPins = updateContainerPins

// CompileWorkflows compiles workflows based on the provided configuration
func CompileWorkflows(ctx context.Context, config CompileConfig) ([]*workflow.WorkflowData, error) { //nolint:largefunc // Existing compilation lifecycle remains centralized.
	config = applyWorkspaceStrictMode(config)
	compileOrchestratorLog.Printf("Starting workflow compilation: files=%d, validate=%v, watch=%v, noEmit=%v",
		len(config.MarkdownFiles), config.Validate, config.Watch, config.NoEmit)

	// Check context cancellation at the start
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Operation cancelled"))
		return nil, ctx.Err()
	default:
	}

	if config.Watch && IsRunningInCI() {
		return nil, errors.New("watch mode cannot be used in CI or Copilot coding agent environments")
	}

	if os.Getenv("GH_HOST") == "" { //nolint:osgetenvlibrary
		if detectedHost := getHostFromOriginRemote(); detectedHost != "github.com" && detectedHost != "" {
			compileOrchestratorLog.Printf("Auto-detected GHES host from git remote: %s", detectedHost)
			workflow.SetDefaultGHHost(detectedHost)
		} else if detectedHost == "github.com" {
			workflow.SetDefaultGHHost("")
		}
	}

	// Validate configuration
	if err := validateCompileConfig(config); err != nil {
		return nil, err
	}

	// Validate action mode if specified
	if err := validateActionModeConfig(config.ActionMode); err != nil {
		return nil, err
	}

	// When --validate is set, run a pre-gate integrity check on actions-lock.json
	// before compiling. This catches malformed or inconsistent pins early so the
	// user sees the problem before any lock files are rewritten.
	// Structural-only validation is used (no live API calls) to keep compile fast
	// and avoid false positives from floating tags.
	if config.Validate {
		compileOrchestratorLog.Print("Running actions-lock.json SHA integrity pre-gate check")
		if err := validateUpdateSHAEntriesStructural(ctx, "."); err != nil {
			return nil, fmt.Errorf("actions-lock.json integrity check failed (pre-compile): %w", err)
		}
	}

	// Initialize actionlint statistics if actionlint is enabled
	if config.Actionlint && !config.NoEmit {
		initActionlintStats()
	}

	// Warn or error when shellcheck is requested but not installed.
	// Skip this check when --no-emit is set: no lock files are written so shellcheck
	// is never invoked. When the binary is absent, Docker is used as a fallback (lazy
	// — only when there are scripts to lint). Only warn/error when neither is available.
	if config.shellcheckEnabled() && !config.NoEmit && !isShellcheckAvailable() {
		if !IsDockerAvailable(ctx) {
			if config.Strict {
				return nil, errors.New("shellcheck not available: binary not found in PATH and Docker is not running; install shellcheck or start Docker to enable run step linting")
			} else {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("shellcheck binary not found in PATH and Docker is not running; run step linting will be skipped"))
			}
		}
	}

	// Track compilation statistics
	stats := &CompilationStats{}

	// Track validation results for JSON output
	var validationResults []ValidationResult

	// Set up workflow directory (using default if not specified)
	workflowDir := config.WorkflowDir
	if workflowDir == "" {
		workflowDir = constants.GetWorkflowDir()
		compileOrchestratorLog.Printf("Using default workflow directory: %s", workflowDir)
	} else {
		workflowDir = filepath.Clean(workflowDir)
		compileOrchestratorLog.Printf("Using custom workflow directory: %s", workflowDir)
	}

	// Preprocess args: expand directory paths and GitHub URLs to constituent workflow files
	if len(config.MarkdownFiles) > 0 {
		expandedFiles, err := resolveCompileArgs(config.MarkdownFiles, config.Verbose)
		if err != nil {
			return nil, err
		}
		config.MarkdownFiles = expandedFiles
	}

	// Create and configure compiler
	if err := maybeForceRefreshContainerPins(ctx, config, workflowDir); err != nil {
		return nil, err
	}

	compiler := createAndConfigureCompiler(config)
	compiler.SetContext(ctx)

	if err := validateRepositoryManifestForCompilation(config, stats, &validationResults); err != nil {
		if config.JSONOutput {
			if outputErr := outputResults(stats, &validationResults, config); outputErr != nil {
				return nil, outputErr
			}
		}
		return nil, err
	}

	// Handle watch mode (early return)
	if config.Watch {
		// Watch mode: watch for file changes and recompile automatically
		// For watch mode, we only support a single file for now
		var markdownFile string
		if len(config.MarkdownFiles) > 0 {
			if len(config.MarkdownFiles) > 1 {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Watch mode only supports a single file, using the first one"))
			}
			// Resolve the workflow file to get the full path
			resolvedFile, err := resolveWorkflowFile(config.MarkdownFiles[0], config.Verbose)
			if err != nil {
				// Return error directly without wrapping - it already contains formatted message with suggestions
				return nil, err
			}
			markdownFile = resolvedFile
		}
		return nil, watchAndCompileWorkflows(ctx, markdownFile, compiler, config.Verbose)
	}

	// Compile specific files or all files in directory
	if len(config.MarkdownFiles) > 0 {
		// Compile specific workflow files
		return compileSpecificFiles(ctx, compiler, config, stats, &validationResults)
	}

	// Compile all workflow files in directory
	return compileAllFilesInDirectory(ctx, compiler, config, workflowDir, stats, &validationResults)
}

func applyWorkspaceStrictMode(config CompileConfig) CompileConfig {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		gitRoot = ""
	}
	repoConfig, err := workflow.LoadRepoConfig(gitRoot)
	if err == nil && repoConfig.Strict {
		config.Strict = true
	}
	return config
}

func maybeForceRefreshContainerPins(ctx context.Context, config CompileConfig, workflowDir string) error {
	if !config.ForceRefreshContainerPins {
		return nil
	}
	if config.NoEmit {
		if config.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping --force-refresh-container-pins because --no-emit is set"))
		}
		return nil
	}

	compileOrchestratorLog.Print("Refreshing container image digest pins before compilation")
	if _, err := compileUpdateContainerPins(ctx, defaultContainerPinUpdateDeps(), workflowDir, config.Verbose, containerPinUpdateOptions{
		refreshExisting:     true,
		failOnResolveErrors: true,
	}); err != nil {
		return fmt.Errorf("failed to refresh container pins: %w", err)
	}
	return nil
}
