package cli

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var addWorkflowPRLog = logger.New("cli:add_workflow_pr")

const (
	ghAwDocumentationURL = "https://github.github.com/gh-aw/"
	ghAwRepositoryURL    = "https://github.com/github/gh-aw"
)

// invalidBranchCharsPattern matches characters not allowed in git branch names
var invalidBranchCharsPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// consecutiveHyphensPattern matches two or more consecutive hyphens
var consecutiveHyphensPattern = regexp.MustCompile(`-{2,}`)

// sanitizeBranchName sanitizes a string for use in a git branch name.
// Git branch names cannot contain:
// - spaces, ~, ^, :, \, ?, *, [, @{
// - consecutive dots (..)
// - leading/trailing dots or slashes
// - control characters
func sanitizeBranchName(name string) string {
	// Use base name only (no directory path)
	name = normalizeWorkflowID(name)

	// Replace problematic characters with hyphens
	// This regex matches any character that's not alphanumeric, hyphen, or underscore
	name = invalidBranchCharsPattern.ReplaceAllString(name, "-")

	// Remove consecutive hyphens
	name = consecutiveHyphensPattern.ReplaceAllString(name, "-")

	// Trim leading/trailing hyphens
	name = strings.Trim(name, "-")

	// Ensure non-empty (fallback to "workflow")
	if name == "" {
		name = "workflow"
	}

	return name
}

// addWorkflowsWithPR handles workflow addition with PR creation using pre-resolved workflows.
func addWorkflowsWithPR(ctx context.Context, workflows []*ResolvedWorkflow, opts AddOptions) (int, string, error) {
	addWorkflowPRLog.Printf("Adding %d workflow(s) with PR creation (resolved)", len(workflows))

	// Get current branch for restoration later
	currentBranch, err := getCurrentBranch()
	if err != nil {
		addWorkflowPRLog.Printf("Failed to get current branch: %v", err)
		return 0, "", fmt.Errorf("failed to get current branch: %w", err)
	}

	addWorkflowPRLog.Printf("Current branch: %s", currentBranch)

	// Create temporary branch with random 4-digit number
	// Use sanitized workflow name to avoid invalid git ref characters
	randomNum := rand.Intn(9000) + 1000 // Generate number between 1000-9999
	sanitizedName := sanitizeBranchName(workflows[0].Spec.WorkflowPath)
	branchName := fmt.Sprintf("add-workflow-%s-%04d", sanitizedName, randomNum)

	addWorkflowPRLog.Printf("Creating temporary branch: %s", branchName)

	if err := createAndSwitchBranch(branchName, opts.Verbose); err != nil {
		return 0, "", fmt.Errorf("failed to create branch %s: %w", branchName, err)
	}

	// Create file tracker for rollback capability
	tracker := NewFileTracker()
	if opts.addWizard != nil {
		for _, initializedFile := range opts.addWizard.initializedFiles {
			if initializedFile.wasExisting {
				tracker.OriginalContent[initializedFile.path] = initializedFile.originalContent
				tracker.TrackModified(initializedFile.path)
			} else {
				tracker.TrackCreated(initializedFile.path)
			}
		}
	}

	// Ensure we switch back to original branch on exit
	defer func() {
		if switchErr := switchBranch(currentBranch, opts.Verbose); switchErr != nil && opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to switch back to branch %s: %v", currentBranch, switchErr)))
		}
	}()

	// Add workflows using the resolved workflow path
	addWorkflowPRLog.Print("Adding workflows to repository")
	prOpts := opts
	if err := addWorkflowsWithTracking(ctx, workflows, tracker, prOpts); err != nil {
		addWorkflowPRLog.Printf("Failed to add workflows: %v", err)
		return 0, "", fmt.Errorf("failed to add workflows: %w", err)
	}

	prepareSpinner := console.NewSpinner("Preparing pull request...")
	if opts.showInteractiveProgress() {
		prepareSpinner.Start()
	}
	defer prepareSpinner.Stop()

	// Stage all files before creating PR
	addWorkflowPRLog.Print("Staging workflow files")
	if err := tracker.StageAllFiles(opts.Verbose); err != nil {
		addWorkflowPRLog.Printf("Failed to stage workflow files: %v", err)
		if rollbackErr := tracker.RollbackAllFiles(opts.Verbose); rollbackErr != nil {
			addWorkflowPRLog.Printf("Failed to rollback files after staging failure: %v", rollbackErr)
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to rollback files: %v", rollbackErr)))
			}
		}
		return 0, "", fmt.Errorf("failed to stage workflow files: %w", err)
	}

	// Update .gitattributes and stage it if changed
	if err := stageGitAttributesIfChanged(); err != nil {
		addWorkflowPRLog.Printf("Failed to stage .gitattributes: %v", err)
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to stage .gitattributes: %v", err)))
		}
	}

	// Commit changes
	var commitMessage, prTitle, prBody, joinedNames string
	if len(workflows) == 1 {
		joinedNames = workflows[0].Spec.WorkflowName
		commitMessage = "Add agentic workflow " + joinedNames
		prTitle = "Add agentic workflow " + joinedNames
	} else {
		workflowNames := sliceutil.Map(workflows, func(wf *ResolvedWorkflow) string {
			return wf.Spec.WorkflowName
		})
		joinedNames = strings.Join(workflowNames, ", ")
		commitMessage = "Add agentic workflows: " + joinedNames
		prTitle = "Add agentic workflows: " + joinedNames
	}
	prBody = buildAddWorkflowPRBody(workflows, opts)

	if err := commitChanges(commitMessage, opts.Verbose); err != nil {
		addWorkflowPRLog.Printf("Failed to commit workflow files on branch %s: %v", branchName, err)
		// Don't rollback - leave the workflow files on disk for manual recovery.
		// Return a richly formatted error with clear instructions so the user can
		// commit and push manually. The top-level error handler will print this.
		return 0, "", fmt.Errorf(
			"failed to commit workflow files: %w\n\n"+
				"The workflow files have been written to disk and staged in git.\n"+
				"Please commit the files manually, then either push them to the\n"+
				"repository or create a pull request:\n\n"+
				"  git commit -m %q\n"+
				"  git push\n\n"+
				"Or to create a pull request:\n\n"+
				"  git checkout -b %s\n"+
				"  git commit -m %q\n"+
				"  git push -u origin %s\n"+
				"  gh pr create --title %q",
			err, commitMessage, branchName, commitMessage, branchName, prTitle,
		)
	}

	// Push branch
	addWorkflowPRLog.Printf("Pushing branch %s to remote", branchName)
	if opts.showInteractiveProgress() {
		prepareSpinner.UpdateMessage("Pushing pull request branch...")
	}
	if err := pushBranch(branchName, opts.Verbose); err != nil {
		addWorkflowPRLog.Printf("Failed to push branch: %v", err)
		// Treat push failure as a warning: keep the files and commit intact so the
		// user can push manually. Do NOT rollback.
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to push branch %s: %v", branchName, err)))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
			"The workflow files have been committed to local branch "+branchName+".\n"+
				"  To push the branch and create a pull request, run:\n\n"+
				"    git push -u origin "+branchName+"\n"+
				"    gh pr create --title "+fmt.Sprintf("%q", prTitle),
		))
		return 0, "", fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	// Create PR
	prepareSpinner.Stop()
	addWorkflowPRLog.Printf("Creating pull request: %s", prTitle)
	prNumber, prURL, err := createPRForRepo(ctx, branchName, prTitle, prBody, opts.RepoSlug, opts.Verbose)
	if err != nil {
		addWorkflowPRLog.Printf("Failed to create PR: %v", err)
		if rollbackErr := tracker.RollbackAllFiles(opts.Verbose); rollbackErr != nil && opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to rollback files: %v", rollbackErr)))
		}
		return 0, "", fmt.Errorf("failed to create PR: %w", err)
	}

	addWorkflowPRLog.Printf("Successfully created PR #%d: %s", prNumber, prURL)

	// Switch back to original branch
	if err := switchBranch(currentBranch, opts.Verbose); err != nil {
		return prNumber, prURL, fmt.Errorf("failed to switch back to branch %s: %w", currentBranch, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created pull request "+prURL))
	return prNumber, prURL, nil
}

func buildAddWorkflowPRBody(workflows []*ResolvedWorkflow, opts AddOptions) string {
	var body strings.Builder
	if opts.addWizard != nil {
		fmt.Fprintf(&body, "This pull request was created with [`gh aw add-wizard`](%s) from [GitHub Agentic Workflows](%s), version `%s`.\n", ghAwDocumentationURL, ghAwRepositoryURL, markdownText(GetVersion()))
	} else {
		fmt.Fprintf(&body, "This pull request was created with [`gh aw add`](%s) from [GitHub Agentic Workflows](%s), version `%s`.\n", ghAwDocumentationURL, ghAwRepositoryURL, markdownText(GetVersion()))
	}

	body.WriteString("\n## Workflows\n")
	for _, resolved := range workflows {
		fmt.Fprintf(&body, "\n### `%s`\n\n", markdownText(resolved.Spec.WorkflowName))
		if resolved.Description != "" {
			fmt.Fprintf(&body, "%s\n\n", markdownBlock(resolved.Description))
		}
		fmt.Fprintf(&body, "- **Source:** %s\n", workflowSourceMarkdown(resolved))
		fmt.Fprintf(&body, "- **Triggers:** %s\n", workflowTriggerSummary(resolved.Content))
	}

	body.WriteString("\n## Options selected\n\n")
	body.WriteString("- **Delivery:** pull request\n")
	if opts.EngineOverride != "" {
		fmt.Fprintf(&body, "- **Engine:** `%s`\n", markdownText(opts.EngineOverride))
	}
	if opts.EngineOverride == "copilot" {
		auth := "`COPILOT_GITHUB_TOKEN` repository secret"
		if opts.AddCopilotRequestsPermission {
			auth = "organization billing via `permissions.copilot-requests: write`"
		} else if opts.addWizard != nil && opts.addWizard.secretSource != "" {
			auth = fmt.Sprintf("existing `COPILOT_GITHUB_TOKEN` %s secret", opts.addWizard.secretSource)
		} else if opts.addWizard != nil && opts.addWizard.skipSecret {
			auth = "`COPILOT_GITHUB_TOKEN` setup skipped"
		}
		fmt.Fprintf(&body, "- **Authentication:** %s\n", auth)
	}
	fmt.Fprintf(&body, "- **Security scanner:** %s\n", enabledText(!opts.DisableSecurityScanner))
	fmt.Fprintf(&body, "- **Stop-after guard:** %s\n", stopAfterSummary(opts))
	fmt.Fprintf(&body, "- **Git attributes:** %s\n", enabledText(!opts.NoGitattributes))
	if opts.WorkflowDir != "" {
		fmt.Fprintf(&body, "- **Workflow directory:** `%s`\n", markdownText(opts.WorkflowDir))
	}
	if opts.GhAwRef != "" {
		fmt.Fprintf(&body, "- **GitHub Agentic Workflows action reference:** `%s`\n", markdownText(opts.GhAwRef))
	}
	if opts.Force {
		body.WriteString("- **Existing workflow files:** overwrite confirmed\n")
	}
	if opts.addWizard != nil {
		fmt.Fprintf(&body, "- **GitHub App permission and event inference:** %s\n", enabledText(!opts.addWizard.disableGitHubAppPermissionInference))
	}
	if opts.AppendText != "" {
		body.WriteString("- **Custom appended instructions:** included\n")
	}
	if opts.addWizard != nil && len(opts.addWizard.initializedFiles) > 0 {
		paths := make([]string, 0, len(opts.addWizard.initializedFiles))
		for _, file := range opts.addWizard.initializedFiles {
			paths = append(paths, file.displayPath)
		}
		fmt.Fprintf(&body, "- **Repository initialization:** %s\n", joinCodeValues(paths))
	}

	body.WriteString("\n## Review criteria\n\n")
	body.WriteString("- Confirm each workflow's source, description, and triggers match the intended automation.\n")
	body.WriteString("- Review the workflow permissions, network access, tools, and safe outputs before enabling it.\n")
	body.WriteString("- Verify the generated `.lock.yml` changes contain only the expected compiled workflow behavior.\n")
	if opts.EngineOverride == "copilot" && !opts.AddCopilotRequestsPermission {
		body.WriteString("- Confirm `COPILOT_GITHUB_TOKEN` is available to the repository; its value is not included in this pull request.\n")
	}

	body.WriteString("\n## Forward progress\n\n")
	body.WriteString("1. Review the changes against the criteria above; request or make changes in the Markdown workflow source, then recompile it with `gh aw compile`.\n")
	body.WriteString("2. Merge this pull request to install the workflow")
	if opts.addWizard != nil && opts.EngineOverride == "copilot" && !opts.AddCopilotRequestsPermission && opts.addWizard.secretSource == "" && !opts.addWizard.skipSecret {
		body.WriteString(". After merge, the add wizard will configure `COPILOT_GITHUB_TOKEN` when needed")
	}
	body.WriteString(".\n")
	body.WriteString("3. Monitor the first scheduled or manually dispatched run, then adjust the Markdown source and recompile if the workflow needs refinement.\n")

	return body.String()
}

func workflowSourceMarkdown(resolved *ResolvedWorkflow) string {
	label := markdownText(resolved.Spec.String())
	if resolved.Spec.RawURL != "" {
		return fmt.Sprintf("[%s](%s)", label, resolved.Spec.RawURL)
	}
	if resolved.SourceInfo == nil || resolved.SourceInfo.IsLocal || resolved.Spec.RepoSlug == "" {
		return "`" + label + "` (local source)"
	}
	host := resolved.Spec.Host
	if host == "" {
		host = "github.com"
	}
	ref := resolved.SourceInfo.CommitSHA
	if ref == "" {
		ref = resolved.Spec.Version
	}
	if ref == "" {
		ref = "HEAD"
	}
	sourceURL := url.URL{Scheme: "https", Host: host, Path: "/" + resolved.Spec.RepoSlug + "/blob/" + ref + "/" + resolved.Spec.WorkflowPath}
	return fmt.Sprintf("[%s](%s)", label, sourceURL.String())
}

func workflowTriggerSummary(content []byte) string {
	frontmatter, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return "not detected"
	}
	on, found := frontmatter.Frontmatter["on"]
	if !found {
		return "not declared"
	}
	if trigger, ok := on.(string); ok {
		return "`" + markdownText(trigger) + "`"
	}
	onMap, ok := on.(map[string]any)
	if !ok {
		return "declared in workflow frontmatter"
	}
	triggers := make([]string, 0, len(onMap))
	for trigger, config := range onMap {
		summary := "`" + markdownText(trigger) + "`"
		if trigger == "schedule" {
			if schedule := detectWorkflowScheduleInfo(string(content)).RawExpr; schedule != "" {
				summary += " (`" + markdownText(schedule) + "`)"
			}
		} else if configString, ok := config.(string); ok && configString != "" {
			summary += " (`" + markdownText(configString) + "`)"
		}
		triggers = append(triggers, summary)
	}
	slices.Sort(triggers)
	return strings.Join(triggers, ", ")
}

func stopAfterSummary(opts AddOptions) string {
	if opts.NoStopAfter {
		return "disabled"
	}
	if opts.StopAfter != "" {
		return "`" + markdownText(opts.StopAfter) + "`"
	}
	return "default"
}

func enabledText(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func markdownText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return strings.NewReplacer("\\", "\\\\", "`", "\\`", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;").Replace(value)
}

func markdownBlock(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func joinCodeValues(values []string) string {
	formatted := make([]string, 0, len(values))
	for _, value := range values {
		formatted = append(formatted, "`"+markdownText(value)+"`")
	}
	return strings.Join(formatted, ", ")
}
