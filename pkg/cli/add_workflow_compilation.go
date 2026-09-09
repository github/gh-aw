package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var addWorkflowCompilationLog = logger.New("cli:add_workflow_compilation")

// compileWorkflow compiles a workflow file without refreshing stop time.
// This is a convenience wrapper around compileWorkflowWithActionRef.
func compileWorkflow(ctx context.Context, filePath string, verbose bool, quiet bool, engineOverride string) error {
	return compileWorkflowWithActionRef(ctx, filePath, verbose, quiet, engineOverride, "")
}

func compileWorkflowWithActionRef(ctx context.Context, filePath string, verbose bool, quiet bool, engineOverride, actionRef string) error {
	return compileWorkflowWithRefreshAndActionRef(ctx, filePath, verbose, quiet, engineOverride, actionRef, false, false)
}

func compileWorkflowWithRefreshAndActionRef(ctx context.Context, filePath string, verbose bool, quiet bool, engineOverride, actionRef string, refreshStopTime bool, approve bool) error {
	addWorkflowCompilationLog.Printf("Compiling workflow: file=%s, refresh_stop_time=%v, engine=%s, approve=%v", filePath, refreshStopTime, engineOverride, approve)

	// Create compiler with auto-detected version and action mode
	compiler := workflow.NewCompiler(
		workflow.WithVerbose(verbose),
		workflow.WithEngineOverride(engineOverride),
	)
	applyAddActionRef(compiler, actionRef)

	compiler.SetRefreshStopTime(refreshStopTime)
	compiler.SetApprove(approve)
	compiler.SetQuiet(quiet)
	if err := CompileWorkflowWithValidation(ctx, compiler, filePath, CompileValidationOptions{Verbose: verbose}); err != nil {
		addWorkflowCompilationLog.Printf("Compilation failed: %v", err)
		return err
	}

	addWorkflowCompilationLog.Print("Compilation completed successfully")

	// Ensure .gitattributes marks .lock.yml files as generated
	if _, err := ensureGitAttributes(); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
		}
	}

	// Note: Instructions are only written when explicitly requested via the compile command flag
	// This helper function is used in contexts where instructions should not be automatically written

	return nil
}

func compileWorkflowWithTrackingAndActionRef(ctx context.Context, filePath string, verbose bool, quiet bool, engineOverride, actionRef string, tracker *FileTracker) error {
	return compileWorkflowWithTrackingAndRefreshAndActionRef(ctx, filePath, verbose, quiet, engineOverride, actionRef, tracker, false)
}

// compileWorkflowWithTrackingAndRefresh compiles a workflow, tracks generated files, and optionally refreshes stop time.
// This function ensures that the file tracker records all files created or modified during compilation.
func compileWorkflowWithTrackingAndRefreshAndActionRef(ctx context.Context, filePath string, verbose bool, quiet bool, engineOverride, actionRef string, tracker *FileTracker, refreshStopTime bool) error {
	addWorkflowCompilationLog.Printf("Compiling workflow with tracking: file=%s, refresh_stop_time=%v", filePath, refreshStopTime)

	// Generate the expected lock file path
	lockFile := stringutil.MarkdownToLockFile(filePath)

	// Check if lock file exists before compilation
	lockFileExists := fileutil.FileExists(lockFile)

	addWorkflowCompilationLog.Printf("Lock file %s exists: %v", lockFile, lockFileExists)

	// Check if .gitattributes exists before compilation so we know whether to
	// use TrackCreated or TrackModified if ensureGitAttributes modifies it later.
	gitRoot, gitRootErr := gitutil.FindGitRoot()
	gitAttributesPath := ""
	gitAttributesExisted := false
	if gitRootErr == nil {
		gitAttributesPath = filepath.Join(gitRoot, ".gitattributes")
		gitAttributesExisted = fileutil.FileExists(gitAttributesPath)
	}

	// Track the lock file before compilation
	if lockFileExists {
		tracker.TrackModified(lockFile)
	} else {
		tracker.TrackCreated(lockFile)
	}

	// Create compiler with auto-detected version and action mode
	compiler := workflow.NewCompiler(
		workflow.WithVerbose(verbose),
		workflow.WithEngineOverride(engineOverride),
	)
	applyAddActionRef(compiler, actionRef)
	compiler.SetFileTracker(tracker)
	compiler.SetRefreshStopTime(refreshStopTime)
	compiler.SetQuiet(quiet)
	if err := CompileWorkflowWithValidation(ctx, compiler, filePath, CompileValidationOptions{Verbose: verbose}); err != nil {
		return err
	}

	// Ensure .gitattributes marks .lock.yml files as generated; only track it if it was actually
	// modified. Errors here are non-fatal — gitattributes update failure does not prevent the
	// compiled workflow from being usable.
	if updated, err := ensureGitAttributes(); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
		}
	} else if updated && gitRootErr == nil {
		if gitAttributesExisted {
			tracker.TrackModified(gitAttributesPath)
		} else {
			tracker.TrackCreated(gitAttributesPath)
		}
	}

	return nil
}

// compileDepsOptions holds shared compilation options for dependency compilation helpers.
type compileDepsOptions struct {
	verbose, quiet  bool
	engineOverride  string
	actionRef       string
	force           bool
	propagateErrors bool
	tracker         *FileTracker
}

func compileDispatchWorkflowDependenciesWithActionRef(ctx context.Context, workflowFile string, verbose, quiet bool, engineOverride, actionRef string, force bool, tracker *FileTracker) {
	compileSafeOutputsWorkflowDependencies(ctx, workflowFile, "dispatch-workflow dependency", dispatchWorkflowNamesForCompilation, compileDepsOptions{
		verbose: verbose, quiet: quiet, engineOverride: engineOverride, actionRef: actionRef, force: force, propagateErrors: false, tracker: tracker,
	})
}

// compileCallWorkflowDependenciesWithActionRef compiles any call-workflow .md worker dependencies of
// workflowFile that are present locally but lack a corresponding .lock.yml. This must be
// called before compiling the main workflow, because the call-workflow validator requires
// every referenced .md worker to have an up-to-date .lock.yml.
//
// Unlike dispatch-workflow dependencies, call-workflow compilation failures are propagated:
// the dynamic tool-generation path maps every worker .md to a .lock.yml reference, so a
// worker whose lock cannot be produced would leave the orchestrator referencing a file that
// does not exist.
func compileCallWorkflowDependenciesWithActionRef(ctx context.Context, workflowFile string, verbose, quiet bool, engineOverride, actionRef string, force bool, tracker *FileTracker) error {
	return compileSafeOutputsWorkflowDependencies(ctx, workflowFile, "call-workflow worker", callWorkflowNamesForCompilation, compileDepsOptions{
		verbose: verbose, quiet: quiet, engineOverride: engineOverride, actionRef: actionRef, force: force, propagateErrors: true, tracker: tracker,
	})
}

// compileSafeOutputsWorkflowDependencies is the shared implementation for compiling
// local .md worker/dependency files referenced by a workflow. namesFunc extracts the
// list of referenced workflow names from workflowFile; label is used in log/console
// messages to identify the dependency type (e.g. "dispatch-workflow dependency").
// When opts.propagateErrors is true the first compilation failure is returned to the
// caller instead of being logged and swallowed.
// When opts.force is true, dependencies are recompiled even when a .lock.yml already
// exists (needed after --force re-fetches a stale worker .md).
func compileSafeOutputsWorkflowDependencies(ctx context.Context, workflowFile, label string, namesFunc func(string) []string, opts compileDepsOptions) error {
	workflowNames := namesFunc(workflowFile)
	if len(workflowNames) == 0 {
		return nil
	}

	workflowsDir := filepath.Dir(workflowFile)

	for _, name := range workflowNames {
		mdPath := filepath.Join(workflowsDir, name+".md")
		lockPath := stringutil.MarkdownToLockFile(mdPath)

		// Skip if the .md is not present locally.
		if _, mdErr := os.Stat(mdPath); mdErr != nil {
			continue
		}
		// Skip recompilation when a lock already exists, unless --force was specified.
		if !opts.force && fileutil.FileExists(lockPath) {
			continue
		}

		addWorkflowCompilationLog.Printf("Compiling %s: %s", label, mdPath)
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Compiling %s: %s", label, mdPath)))
		}

		var compileErr error
		if opts.tracker != nil {
			compileErr = compileWorkflowWithTrackingAndActionRef(ctx, mdPath, opts.verbose, opts.quiet, opts.engineOverride, opts.actionRef, opts.tracker)
		} else {
			compileErr = compileWorkflowWithActionRef(ctx, mdPath, opts.verbose, opts.quiet, opts.engineOverride, opts.actionRef)
		}
		if compileErr != nil {
			if opts.propagateErrors {
				return fmt.Errorf("failed to compile %s %s: %w", label, mdPath, compileErr)
			}
			// Best-effort: log and continue so the main workflow can still give a clear error.
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to compile %s %s: %v", label, mdPath, compileErr)))
			}
		}
	}
	return nil
}

func applyAddActionRef(compiler *workflow.Compiler, actionRef string) {
	if actionRef == "" {
		return
	}
	compiler.SetActionMode(workflow.ActionModeRelease)
	compiler.SetActionTag(actionRef)
}

func callWorkflowNamesForCompilation(workflowFile string) []string {
	return safeOutputsWorkflowNamesForCompilation(workflowFile, "call-workflow", func(data *workflow.WorkflowData) []string {
		if data.SafeOutputs.CallWorkflow == nil {
			return nil
		}
		return data.SafeOutputs.CallWorkflow.Workflows
	})
}

// extractSafeOutputsNamesFromFrontmatter reads workflowFile and extracts workflow names from
// the named key under safe-outputs. It handles both array and map (with a "workflows" field)
// forms of the configuration.
func extractSafeOutputsNamesFromFrontmatter(workflowFile, safeOutputsKey string) []string {
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		return nil
	}

	frontmatter, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || frontmatter == nil {
		return nil
	}

	safeOutputs, ok := frontmatter.Frontmatter["safe-outputs"].(map[string]any)
	if !ok {
		return nil
	}

	configVal, exists := safeOutputs[safeOutputsKey]
	if !exists {
		return nil
	}

	var names []string
	switch config := configVal.(type) {
	case []any:
		names = appendDispatchWorkflowNames(names, config)
	case map[string]any:
		if workflows, ok := config["workflows"].([]any); ok {
			names = appendDispatchWorkflowNames(names, workflows)
		}
	}

	return dedupeDispatchWorkflowNames(names)
}

func dispatchWorkflowNamesForCompilation(workflowFile string) []string {
	return safeOutputsWorkflowNamesForCompilation(workflowFile, "dispatch-workflow", func(data *workflow.WorkflowData) []string {
		if data.SafeOutputs.DispatchWorkflow == nil {
			return nil
		}
		return data.SafeOutputs.DispatchWorkflow.Workflows
	})
}

// safeOutputsWorkflowNamesForCompilation is the shared implementation for resolving
// the list of workflow names that need pre-compilation. It first tries to parse the
// workflow file with the compiler (which merges imports), then falls back to raw
// frontmatter extraction when the file cannot be fully parsed. safeOutputsKey is
// used in the fallback log message; getWorkflows selects the relevant workflow list
// from the parsed safe-outputs data.
func safeOutputsWorkflowNamesForCompilation(workflowFile, safeOutputsKey string, getWorkflows func(*workflow.WorkflowData) []string) []string {
	compiler := workflow.NewCompiler()
	data, err := compiler.ParseWorkflowFile(workflowFile)
	if err == nil && data != nil && data.SafeOutputs != nil {
		if names := dedupeDispatchWorkflowNames(getWorkflows(data)); len(names) > 0 {
			return names
		}
	}

	if err != nil {
		var sharedErr *workflow.SharedWorkflowError
		var redirectErr *workflow.RedirectOnlyWorkflowError
		if !errors.As(err, &sharedErr) && !errors.As(err, &redirectErr) {
			addWorkflowCompilationLog.Printf("Falling back to raw %s extraction for %s after parse error: %v", safeOutputsKey, workflowFile, err)
		}
	}

	return extractSafeOutputsNamesFromFrontmatter(workflowFile, safeOutputsKey)
}

func appendDispatchWorkflowNames(names []string, raw []any) []string {
	for _, candidate := range raw {
		name, ok := candidate.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		names = append(names, trimmed)
	}
	return names
}

func dedupeDispatchWorkflowNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// This function preserves the existing frontmatter formatting while adding the source field.
func addSourceToWorkflow(content, source string) (string, error) {
	// Use shared frontmatter logic that preserves formatting
	return addFieldToFrontmatter(content, "source", source, false)
}

// addEngineToWorkflow adds or updates the engine field in the workflow's frontmatter.
// This function preserves the existing frontmatter formatting while setting the engine field.
// A trailing blank line is added after the engine declaration to visually separate it from
// the source field that follows, preventing adjacent-line merge conflicts during updates.
func addEngineToWorkflow(content, engine string) (string, error) {
	// Use shared frontmatter logic that preserves formatting; trailing blank line separates
	// the engine declaration from the source field added immediately after.
	return addFieldToFrontmatter(content, "engine", engine, true)
}
