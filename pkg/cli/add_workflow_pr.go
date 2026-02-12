package cli

import (
	"fmt"
	"math/rand"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var addWorkflowPRLog = logger.New("cli:add_workflow_pr")

// addWorkflowsWithPR handles workflow addition with PR creation and returns the PR number and URL.
func addWorkflowsWithPR(workflows []*WorkflowSpec, opts AddOptions) (int, string, error) {
	addWorkflowPRLog.Printf("Adding %d workflow(s) with PR creation", len(workflows))

	// Get current branch for restoration later
	currentBranch, err := getCurrentBranch()
	if err != nil {
		addWorkflowPRLog.Printf("Failed to get current branch: %v", err)
		return 0, "", fmt.Errorf("failed to get current branch: %w", err)
	}

	addWorkflowPRLog.Printf("Current branch: %s", currentBranch)

	// Create temporary branch with random 4-digit number
	randomNum := rand.Intn(9000) + 1000 // Generate number between 1000-9999
	branchName := fmt.Sprintf("add-workflow-%s-%04d", strings.ReplaceAll(workflows[0].WorkflowPath, "/", "-"), randomNum)

	addWorkflowPRLog.Printf("Creating temporary branch: %s", branchName)

	if err := createAndSwitchBranch(branchName, opts.Verbose); err != nil {
		return 0, "", fmt.Errorf("failed to create branch %s: %w", branchName, err)
	}

	// Create file tracker for rollback capability
	tracker, err := NewFileTracker()
	if err != nil {
		return 0, "", fmt.Errorf("failed to create file tracker: %w", err)
	}

	// Ensure we switch back to original branch on exit
	defer func() {
		if switchErr := switchBranch(currentBranch, opts.Verbose); switchErr != nil && opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to switch back to branch %s: %v", currentBranch, switchErr)))
		}
	}()

	// Add workflows using the normal function logic
	addWorkflowPRLog.Print("Adding workflows to repository")
	// Disable security scanner for PR creation to use workflow settings
	prOpts := opts
	prOpts.DisableSecurityScanner = false
	if err := addWorkflowsNormal(workflows, prOpts); err != nil {
		addWorkflowPRLog.Printf("Failed to add workflows: %v", err)
		// Rollback on error
		if rollbackErr := tracker.RollbackAllFiles(opts.Verbose); rollbackErr != nil && opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to rollback files: %v", rollbackErr)))
		}
		return 0, "", fmt.Errorf("failed to add workflows: %w", err)
	}

	// Stage all files before creating PR
	addWorkflowPRLog.Print("Staging workflow files")
	if err := tracker.StageAllFiles(opts.Verbose); err != nil {
		if rollbackErr := tracker.RollbackAllFiles(opts.Verbose); rollbackErr != nil && opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to rollback files: %v", rollbackErr)))
		}
		return 0, "", fmt.Errorf("failed to stage workflow files: %w", err)
	}

	// Update .gitattributes and stage it if changed
	if err := stageGitAttributesIfChanged(); err != nil && opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to stage .gitattributes: %v", err)))
	}

	// Commit changes
	var commitMessage, prTitle, prBody, joinedNames string
	if len(workflows) == 1 {
		joinedNames = workflows[0].WorkflowName
		commitMessage = fmt.Sprintf("Add agentic workflow %s", joinedNames)
		prTitle = fmt.Sprintf("Add agentic workflow %s", joinedNames)
		prBody = fmt.Sprintf("Add agentic workflow %s", joinedNames)
	} else {
		// Get workflow.Workflo
		workflowNames := make([]string, len(workflows))
		for i, wf := range workflows {
			workflowNames[i] = wf.WorkflowName
		}
		joinedNames = strings.Join(workflowNames, ", ")
		commitMessage = fmt.Sprintf("Add agentic workflows: %s", joinedNames)
		prTitle = fmt.Sprintf("Add agentic workflows: %s", joinedNames)
		prBody = fmt.Sprintf("Add agentic workflows: %s", joinedNames)
	}

	if err := commitChanges(commitMessage, opts.Verbose); err != nil {
		if rollbackErr := tracker.RollbackAllFiles(opts.Verbose); rollbackErr != nil && opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to rollback files: %v", rollbackErr)))
		}
		return 0, "", fmt.Errorf("failed to commit files: %w", err)
	}

	// Push branch
	addWorkflowPRLog.Printf("Pushing branch %s to remote", branchName)
	if err := pushBranch(branchName, opts.Verbose); err != nil {
		addWorkflowPRLog.Printf("Failed to push branch: %v", err)
		if rollbackErr := tracker.RollbackAllFiles(opts.Verbose); rollbackErr != nil && opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to rollback files: %v", rollbackErr)))
		}
		return 0, "", fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	// Create PR
	addWorkflowPRLog.Printf("Creating pull request: %s", prTitle)
	prNumber, prURL, err := createPR(branchName, prTitle, prBody, opts.Verbose)
	if err != nil {
		addWorkflowPRLog.Printf("Failed to create PR: %v", err)
		if rollbackErr := tracker.RollbackAllFiles(opts.Verbose); rollbackErr != nil && opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to rollback files: %v", rollbackErr)))
		}
		return 0, "", fmt.Errorf("failed to create PR: %w", err)
	}

	addWorkflowPRLog.Printf("Successfully created PR #%d: %s", prNumber, prURL)

	// Success - no rollback needed

	// Switch back to original branch
	if err := switchBranch(currentBranch, opts.Verbose); err != nil {
		return prNumber, prURL, fmt.Errorf("failed to switch back to branch %s: %w", currentBranch, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Created pull request %s", prURL)))
	return prNumber, prURL, nil
}
