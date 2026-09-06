package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// This file owns the markdown workflow add pipeline: destination validation,
// dependency fetching, frontmatter transformations, writing, and compilation.

func reportAddWorkflowStart(workflowSpec *WorkflowSpec, sourceContent []byte, opts AddOptions) {
	addLog.Printf("Adding workflow: name=%s, content_size=%d bytes", workflowSpec.WorkflowName, len(sourceContent))
	if !opts.Verbose {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Adding workflow: "+workflowSpec.String()))
	if opts.Force {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Force flag enabled: will overwrite existing files"))
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Using pre-fetched workflow content (%d bytes)", len(sourceContent))))
}

func validateWorkflowDestination(githubWorkflowsDir, workflowName, sourceRepo string, opts AddOptions) (bool, error) {
	existingFile := filepath.Join(githubWorkflowsDir, workflowName+".md")
	if !fileutil.FileExists(existingFile) || opts.Force {
		return false, nil
	}
	if sourceRepo != "" {
		existingSourceRepo := readSourceRepoFromFile(existingFile)
		if existingSourceRepo == sourceRepo {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Workflow from same source already exists, skipping: "+existingFile))
			return true, nil
		}
	}
	if opts.FromWildcard {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Workflow '%s' already exists in .github/workflows/. Skipping.", workflowName)))
		return true, nil
	}
	return false, fmt.Errorf("workflow '%s' already exists in .github/workflows/. Use a different name with -n flag, remove the existing workflow first, or use --force to overwrite", workflowName)
}

func compileAddedWorkflow(ctx context.Context, destFile string, workflowSpec *WorkflowSpec, githubWorkflowsDir string, tracker *FileTracker, opts AddOptions) {
	// For remote workflows: now that the main workflow and all its imports are on disk,
	// parse the fully merged safe-outputs configuration to discover any dispatch or
	// call-workflow workers that originate from imported shared workflows (not visible
	// in the raw frontmatter).
	if !isLocalWorkflowPath(workflowSpec.WorkflowPath) {
		fetchAndSaveDispatchWorkflowsFromParsedFile(ctx, destFile, workflowSpec, githubWorkflowsDir, opts.Verbose, opts.Force, tracker)
		fetchAndSaveCallWorkflowsFromParsedFile(ctx, destFile, workflowSpec, githubWorkflowsDir, opts.Verbose, opts.Force, tracker)
	}
	// Compile any dispatch-workflow .md dependencies that were just fetched and lack a
	// .lock.yml. The dispatch-workflow validator requires every .md dispatch target to be
	// compiled before the main workflow can be validated. With --force, always recompile
	// to pick up freshly overwritten worker files.
	compileDispatchWorkflowDependenciesWithActionRef(ctx, destFile, opts.Verbose, opts.Quiet, opts.EngineOverride, opts.GhAwRef, opts.Force, tracker)
	// Compile any call-workflow .md worker dependencies that were just fetched and lack a
	// .lock.yml. Errors are propagated: a missing worker .lock.yml would leave the
	// orchestrator referencing a non-existent file. With --force, always recompile to
	// pick up freshly overwritten worker files.
	if err := compileCallWorkflowDependenciesWithActionRef(ctx, destFile, opts.Verbose, opts.Quiet, opts.EngineOverride, opts.GhAwRef, opts.Force, tracker); err != nil {
		printCompilationError(err, opts.Quiet)
		return
	}
	// Compile the workflow
	if tracker != nil {
		if err := compileWorkflowWithTrackingAndActionRef(ctx, destFile, opts.Verbose, opts.Quiet, opts.EngineOverride, opts.GhAwRef, tracker); err != nil {
			printCompilationError(err, opts.Quiet)
		}
		return
	}
	if err := compileWorkflowWithActionRef(ctx, destFile, opts.Verbose, opts.Quiet, opts.EngineOverride, opts.GhAwRef); err != nil {
		printCompilationError(err, opts.Quiet)
	}
}

func validateWorkflowSecurity(resolved *ResolvedWorkflow, opts AddOptions) error {
	if !opts.DisableSecurityScanner {
		if findings := workflow.ScanMarkdownSecurity(string(resolved.Content)); len(findings) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Security scan failed for workflow"))
			fmt.Fprintln(os.Stderr, workflow.FormatSecurityFindings(findings, resolved.Spec.WorkflowPath))
			return fmt.Errorf("workflow '%s' failed security scan: %d issue(s) detected", resolved.Spec.WorkflowPath, len(findings))
		}
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Security scan passed"))
		}
	} else if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Security scanning disabled"))
	}
	return nil
}

func resolveWorkflowTargetDir(opts AddOptions) (gitRoot, githubWorkflowsDir string, err error) {
	gitRoot, err = gitutil.FindGitRoot()
	if err != nil {
		return "", "", fmt.Errorf("add workflow requires being in a git repository: %w", err)
	}
	if opts.WorkflowDir != "" {
		if filepath.IsAbs(opts.WorkflowDir) {
			return "", "", fmt.Errorf("workflow directory is absolute: %s. Expected a relative path from the repository root. Example: --dir .github/workflows", opts.WorkflowDir)
		}
		githubWorkflowsDir = filepath.Join(gitRoot, filepath.Clean(opts.WorkflowDir))
	} else {
		githubWorkflowsDir = filepath.Join(gitRoot, constants.GetWorkflowDir())
	}
	if err := os.MkdirAll(githubWorkflowsDir, constants.DirPermPublic); err != nil {
		return "", "", fmt.Errorf("failed to create workflow directory %s: %w", githubWorkflowsDir, err)
	}
	return gitRoot, githubWorkflowsDir, nil
}

func fetchWorkflowDependencies(ctx context.Context, workflowSpec *WorkflowSpec, sourceInfo *FetchedWorkflow, sourceContent []byte, githubWorkflowsDir string, tracker *FileTracker, opts AddOptions) error {
	// For remote workflows, fetch and save all dependencies (includes, imports, dispatch workflows, resources)
	if workflowSpec.RawURL != "" {
		// Generic URL imports carry no GitHub repo context; dependency fetching is skipped.
		return nil
	}
	if !isLocalWorkflowPath(workflowSpec.WorkflowPath) {
		return fetchAllRemoteDependencies(ctx, string(sourceContent), workflowSpec, githubWorkflowsDir, opts.Verbose, opts.Force, tracker)
	}
	if sourceInfo == nil || !sourceInfo.IsLocal {
		return nil
	}

	// For local workflows, collect and copy include dependencies from local paths
	// The source directory is derived from the workflow's path
	sourceDir := filepath.Dir(workflowSpec.WorkflowPath)
	includeDeps, err := collectLocalIncludeDependencies(string(sourceContent), sourceDir, opts.Verbose)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to collect include dependencies: %v", err)))
	}
	if err := copyIncludeDependenciesFromPackageWithForce(includeDeps, githubWorkflowsDir, opts.Verbose, opts.Force, tracker); err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to copy include dependencies: %v", err)))
	}
	return nil
}

// When the fetch used a fallback path (e.g. .github/workflows/my-workflow.md instead
// of the short-form my-workflow.md), SourcePath holds the actual repo-root-relative
// path. Propagate it to workflowSpec so all downstream processing (source field,
// include/import resolution) uses the canonical path.
func resolvedWorkflowSpec(workflowSpec *WorkflowSpec, sourceInfo *FetchedWorkflow) *WorkflowSpec {
	if sourceInfo == nil || sourceInfo.IsLocal || sourceInfo.SourcePath == "" || sourceInfo.SourcePath == workflowSpec.WorkflowPath {
		return workflowSpec
	}
	specCopy := *workflowSpec
	specCopy.WorkflowPath = sourceInfo.SourcePath
	return &specCopy
}

func processWorkflowContentModifications(content string, workflowSpec *WorkflowSpec, sourceInfo *FetchedWorkflow, githubWorkflowsDir string, opts AddOptions) (string, error) {
	content, err := applyEngineAndPermissionModifications(content, opts)
	if err != nil {
		return content, err
	}
	content, err = applyLocalSkillRefRewriting(content, sourceInfo, opts)
	if err != nil {
		return content, err
	}
	content, err = applySourceAndIncludeModifications(content, workflowSpec, sourceInfo, githubWorkflowsDir, opts)
	if err != nil {
		return content, err
	}
	content, err = applyStopAfterModifications(content, opts)
	if err != nil {
		return content, err
	}
	if opts.AppendText != "" {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + opts.AppendText
	}
	return content, nil
}

func applyEngineAndPermissionModifications(content string, opts AddOptions) (string, error) {
	// Handle engine override - add/update the engine field in frontmatter before source so
	// the engine declaration appears above the source field in the final file.
	// The default engine is omitted to avoid unnecessary noise and prevent conflicts during
	// later workflow updates.
	if opts.EngineOverride != "" && opts.EngineOverride != string(constants.DefaultEngine) {
		updatedContent, err := addEngineToWorkflow(content, opts.EngineOverride)
		if err != nil {
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to set engine field: %v", err)))
			}
		} else {
			content = updatedContent
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Set engine field to: "+opts.EngineOverride))
			}
		}
	}

	// Inject permissions.copilot-requests: write when the user chose org-billing auth.
	// Only inject for Copilot workflows — guard against the flag being inadvertently set
	// when multiple workflows with different engines are processed in the same batch.
	if opts.AddCopilotRequestsPermission && isCopilotWorkflowContent(content) {
		updatedContent, err := addCopilotRequestsPermissionToContent(content)
		if err != nil {
			// Always warn: user explicitly chose copilot-requests auth; a silent failure
			// means the deployed workflow will lack the required permission.
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to add copilot-requests permission: %v", err)))
		} else {
			content = updatedContent
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Added permissions.copilot-requests: write to workflow"))
			}
		}
	}
	if opts.AddCopilotRequestsNonePermission && isCopilotWorkflowContent(content) {
		updatedContent, err := addCopilotRequestsNonePermissionToContent(content)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to disable copilot-requests permission: %v", err)))
		} else {
			content = updatedContent
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Added permissions.copilot-requests: none to workflow"))
			}
		}
	}
	return content, nil
}

func applySourceAndIncludeModifications(content string, workflowSpec *WorkflowSpec, sourceInfo *FetchedWorkflow, githubWorkflowsDir string, opts AddOptions) (string, error) {
	// Add source field to frontmatter
	commitSHA := ""
	if sourceInfo != nil {
		commitSHA = sourceInfo.CommitSHA
	}
	sourceString := buildSourceStringWithCommitSHA(workflowSpec, commitSHA)
	if sourceString != "" {
		updatedContent, err := addSourceToWorkflow(content, sourceString)
		if err != nil {
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to add source field: %v", err)))
			}
		} else {
			content = updatedContent
		}
	}

	// Note: frontmatter 'imports:' are intentionally kept as relative paths here.
	// fetchAndSaveRemoteFrontmatterImports already downloaded those files locally, so
	// the compiler can resolve them from disk without any GitHub API calls.

	// Process @include directives and replace with workflowspec.
	// For local workflows, use the workflow's directory as the package source path.
	// Pass githubWorkflowsDir as localWorkflowDir so that any body-level import
	// whose target already exists locally is preserved as a local reference rather
	// than being rewritten to a cross-repo workflowspec.
	if workflowSpec.RepoSlug == "" || workflowSpec.WorkflowPath == "" {
		return content, nil
	}
	includeSourceDir := ""
	if sourceInfo != nil && sourceInfo.IsLocal {
		includeSourceDir = filepath.Dir(workflowSpec.WorkflowPath)
	}
	processedContent, err := processIncludesWithWorkflowSpec(content, workflowSpec, commitSHA, includeSourceDir, githubWorkflowsDir, opts.Verbose)
	if err != nil {
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to process includes: %v", err)))
		}
		return content, nil
	}
	return processedContent, nil
}

func applyStopAfterModifications(content string, opts AddOptions) (string, error) {
	// Handle stop-after field modifications
	if opts.NoStopAfter {
		cleanedContent, err := RemoveFieldFromOnTrigger(content, "stop-after")
		if err != nil {
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove stop-after field: %v", err)))
			}
			return content, nil
		}
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Removed stop-after field from workflow"))
		}
		return cleanedContent, nil
	}
	if opts.StopAfter == "" {
		return content, nil
	}

	updatedContent, err := SetFieldInOnTrigger(content, "stop-after", opts.StopAfter)
	if err != nil {
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to set stop-after field: %v", err)))
		}
		return content, nil
	}
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Set stop-after field to: "+opts.StopAfter))
	}
	return updatedContent, nil
}

func trackAndWriteWorkflowFile(destFile string, content string, fileExists bool, opts AddOptions, tracker *FileTracker) error {
	if tracker != nil {
		if fileExists {
			tracker.TrackModified(destFile)
		} else {
			tracker.TrackCreated(destFile)
		}
	}
	if err := os.WriteFile(destFile, []byte(content), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write destination file '%s': %w", destFile, err)
	}

	// Read back the just-written file to ensure downstream processing (including
	// frontmatter hash computation) uses the exact bytes on disk and avoids parity drift.
	writtenContent, err := os.ReadFile(destFile)
	if err != nil {
		return fmt.Errorf("failed to read back destination file '%s': %w", destFile, err)
	}
	if !opts.Quiet {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Added workflow: "+filepath.Base(destFile)))
		if opts.Verbose {
			if description := ExtractWorkflowDescription(string(writtenContent)); description != "" {
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(description))
				fmt.Fprintln(os.Stderr, "")
			}
		}
	}
	return nil
}
