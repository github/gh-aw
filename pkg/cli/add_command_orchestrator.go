package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
)

// This file coordinates resolved add requests, file tracking, and resource dispatch.

// AddWorkflowsResult contains the result of adding workflows
type AddWorkflowsResult struct {
	// PRNumber is the PR number if a PR was created, or 0 if no PR was created
	PRNumber int
	// PRURL is the URL of the created PR, or empty if no PR was created
	PRURL string
	// HasWorkflowDispatch is true if any of the added workflows has a workflow_dispatch trigger
	HasWorkflowDispatch bool
}

// AddWorkflows adds one or more workflows from components to .github/workflows
// with optional repository installation and PR creation.
// Returns AddWorkflowsResult containing PR number (if created) and other metadata.
func AddWorkflows(ctx context.Context, workflows []string, opts AddOptions) (*AddWorkflowsResult, error) {
	addLog.Printf("Resolving %d workflow reference(s) before add", len(workflows))
	// Resolve workflows first - fetches content directly from GitHub
	resolved, err := ResolveWorkflows(ctx, workflows, opts.Verbose)
	if err != nil {
		return nil, err
	}

	return AddResolvedWorkflows(ctx, workflows, resolved, opts)
}

// AddResolvedWorkflows adds workflows using pre-resolved workflow data.
// This allows callers to resolve workflows early (e.g., to show descriptions) and then add them later.
// The opts.Quiet parameter suppresses detailed output (useful for interactive mode where output is already shown).
func AddResolvedWorkflows(ctx context.Context, workflowStrings []string, resolved *ResolvedWorkflows, opts AddOptions) (*AddWorkflowsResult, error) {
	addLog.Printf("Adding workflows: count=%d, engineOverride=%s, createPR=%v, noGitattributes=%v, opts.WorkflowDir=%s, noStopAfter=%v, stopAfter=%s", len(workflowStrings), opts.EngineOverride, opts.CreatePR, opts.NoGitattributes, opts.WorkflowDir, opts.NoStopAfter, opts.StopAfter)

	result := &AddWorkflowsResult{}

	for _, warning := range resolved.Warnings {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warning))
	}

	// If creating a PR, check prerequisites
	if opts.CreatePR {
		// Check if GitHub CLI is available
		if !isGHCLIAvailable() {
			return nil, errors.New("GitHub CLI (gh) is not available. Expected gh to be installed and on PATH before using --create-pull-request. Example: brew install gh")
		}

		// Check if we're in a git repository
		if !isGitRepo() {
			return nil, errors.New("not in a git repository - PR creation requires a git repository")
		}

		// Check no other changes are present
		if opts.addWizard == nil || !opts.addWizard.workingTreePrevalidated {
			if err := checkCleanWorkingDirectoryIgnoring(opts.Verbose, opts.wizardInitializedPaths()); err != nil {
				return nil, fmt.Errorf("working directory is not clean: %w", err)
			}
		}
	}

	// Set workflow_dispatch result
	result.HasWorkflowDispatch = resolved.HasWorkflowDispatch

	// Set FromWildcard flag based on resolved workflows
	opts.FromWildcard = resolved.HasWildcard

	// Handle PR creation workflow
	if opts.CreatePR {
		addLog.Print("Creating workflow with PR")
		prNumber, prURL, err := addWorkflowsWithPR(ctx, resolved.Workflows, opts)
		if err != nil {
			return nil, err
		}
		result.PRNumber = prNumber
		result.PRURL = prURL
		return result, nil
	}

	// Handle normal workflow addition - pass resolved workflows with content
	addLog.Print("Adding workflows normally without PR")
	return result, addWorkflows(ctx, resolved.Workflows, opts)
}

// addWorkflows handles workflow addition using pre-fetched content
func addWorkflows(ctx context.Context, workflows []*ResolvedWorkflow, opts AddOptions) error {
	addLog.Printf("Adding %d workflow(s) to repository", len(workflows))
	// Create file tracker for all operations
	tracker := NewFileTracker()
	return addWorkflowsWithTracking(ctx, workflows, tracker, opts)
}

func prepareGitAttributesTracking(tracker *FileTracker) (path string, existed bool) {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		addLog.Printf("Skipping .gitattributes tracking setup: failed to find git root: %v", err)
		return "", false
	}

	path = filepath.Join(gitRoot, ".gitattributes")
	existed = fileutil.FileExists(path)
	if tracker != nil && existed {
		tracker.TrackModified(path)
	}

	return path, existed
}

func trackGitAttributesIfCreated(tracker *FileTracker, path string, existed bool, updated bool) {
	if tracker == nil || path == "" || existed || !updated {
		return
	}
	tracker.TrackCreated(path)
}

// addWorkflows handles workflow addition using pre-fetched content
func addWorkflowsWithTracking(ctx context.Context, workflows []*ResolvedWorkflow, tracker *FileTracker, opts AddOptions) error {
	addLog.Printf("Adding %d workflow(s) with tracking: force=%v, disableSecurityScanner=%v", len(workflows), opts.Force, opts.DisableSecurityScanner)
	// Ensure .gitattributes is configured unless flag is set
	if !opts.NoGitattributes {
		gitAttributesPath, gitAttributesExisted := prepareGitAttributesTracking(tracker)

		addLog.Print("Configuring .gitattributes")
		if updated, err := ensureGitAttributes(); err != nil {
			addLog.Printf("Failed to configure .gitattributes: %v", err)
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
			}
			// Don't fail the entire operation if gitattributes update fails
		} else if updated {
			trackGitAttributesIfCreated(tracker, gitAttributesPath, gitAttributesExisted, updated)
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Configured .gitattributes"))
			}
		}
	}

	if !opts.Quiet && len(workflows) > 1 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Adding %d workflow(s)...", len(workflows))))
	}

	// Add each workflow using pre-fetched content
	for i, resolved := range workflows {
		if !opts.Quiet && len(workflows) > 1 {
			fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("Adding workflow %d/%d: %s", i+1, len(workflows), resolved.Spec.WorkflowName)))
		}

		if err := addWorkflowWithTracking(ctx, resolved, tracker, opts); err != nil {
			if tracker != nil {
				if rollbackErr := tracker.RollbackAllFiles(opts.Verbose); rollbackErr != nil {
					return fmt.Errorf("failed to add workflow '%s' (rollback also failed): %w", resolved.Spec.String(), errors.Join(err, rollbackErr))
				}
			}
			return fmt.Errorf("failed to add workflow '%s': %w", resolved.Spec.String(), err)
		}
	}

	if err := writePackageOwnershipRecords(workflows, tracker, opts); err != nil {
		if tracker != nil {
			if rollbackErr := tracker.RollbackAllFiles(opts.Verbose); rollbackErr != nil {
				return fmt.Errorf("failed to write package ownership records (rollback also failed): %w", errors.Join(err, rollbackErr))
			}
		}
		return err
	}

	if !opts.Quiet && len(workflows) > 1 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully added all %d workflows", len(workflows))))
	}

	return nil
}

// addWorkflowWithTracking adds a workflow using pre-fetched content with file tracking
func addWorkflowWithTracking(ctx context.Context, resolved *ResolvedWorkflow, tracker *FileTracker, opts AddOptions) error {
	workflowSpec := resolved.Spec
	sourceContent := resolved.Content
	sourceInfo := resolved.SourceInfo

	addLog.Printf("Processing workflow %q (source_size=%d bytes)", workflowSpec.WorkflowName, len(sourceContent))
	reportAddWorkflowStart(workflowSpec, sourceContent, opts)
	if err := validateWorkflowSecurity(resolved, opts); err != nil {
		return err
	}
	gitRoot, githubWorkflowsDir, err := resolveWorkflowTargetDir(opts)
	if err != nil {
		return err
	}

	// For package manifest entries WorkflowName is derived from the entry's install
	// destination, so mapped entries install under their declared destination name.
	workflowName := workflowSpec.WorkflowName
	if opts.Name != "" {
		workflowName = opts.Name
	}
	if handled, err := addNonWorkflowResourceWithTracking(resolved, tracker, opts, gitRoot, githubWorkflowsDir, workflowName); handled || err != nil {
		return err
	}
	sourceRepo := ""
	if sourceInfo != nil && !sourceInfo.IsLocal {
		sourceRepo = workflowSpec.RepoSlug
	}
	skip, err := validateWorkflowDestination(githubWorkflowsDir, workflowName, sourceRepo, opts)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}
	if err := fetchWorkflowDependencies(ctx, workflowSpec, sourceInfo, sourceContent, githubWorkflowsDir, tracker, opts); err != nil {
		return err
	}

	destFile := filepath.Join(githubWorkflowsDir, workflowName+".md")
	fileExists := fileutil.FileExists(destFile)
	if fileExists && !opts.showInteractiveProgress() {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Overwriting existing file: "+destFile))
	}
	stopProgress := startAddInteractiveProgress(opts, "Preparing workflow files...")
	defer stopProgress()
	workflowSpec = resolvedWorkflowSpec(workflowSpec, sourceInfo)
	content, err := processWorkflowContentModifications(string(sourceContent), workflowSpec, sourceInfo, githubWorkflowsDir, opts)
	if err != nil {
		return err
	}
	if err := trackAndWriteWorkflowFile(destFile, content, fileExists, opts, tracker); err != nil {
		return err
	}
	compileAddedWorkflow(ctx, destFile, workflowSpec, githubWorkflowsDir, tracker, opts)
	return nil
}

func startAddInteractiveProgress(opts AddOptions, message string) func() {
	if !opts.showInteractiveProgress() {
		return func() {}
	}
	spinner := console.NewSpinner(message)
	spinner.Start()
	return spinner.Stop

}
