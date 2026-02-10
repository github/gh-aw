package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

// UpdateWorkflows updates workflows from their source repositories
func UpdateWorkflows(workflowNames []string, allowMajor, force, verbose bool, engineOverride string, workflowsDir string, noStopAfter bool, stopAfter string, merge bool) error {
	updateLog.Printf("Scanning for workflows with source field: dir=%s, filter=%v, merge=%v", workflowsDir, workflowNames, merge)

	// Use provided workflows directory or default
	if workflowsDir == "" {
		workflowsDir = getWorkflowsDir()
	}

	// Find all workflows with source field
	workflows, err := findWorkflowsWithSource(workflowsDir, workflowNames, verbose)
	if err != nil {
		return err
	}

	updateLog.Printf("Found %d workflows with source field", len(workflows))

	if len(workflows) == 0 {
		if len(workflowNames) > 0 {
			return fmt.Errorf("no workflows found matching the specified names with source field")
		}
		return fmt.Errorf("no workflows found with source field")
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d workflow(s) to update", len(workflows))))

	// Track update results
	var successfulUpdates []string
	var failedUpdates []updateFailure

	// Update each workflow
	for _, wf := range workflows {
		updateLog.Printf("Updating workflow: %s (source: %s)", wf.Name, wf.SourceSpec)
		if err := updateWorkflow(wf, allowMajor, force, verbose, engineOverride, noStopAfter, stopAfter, merge); err != nil {
			updateLog.Printf("Failed to update workflow %s: %v", wf.Name, err)
			failedUpdates = append(failedUpdates, updateFailure{
				Name:  wf.Name,
				Error: err.Error(),
			})
			continue
		}
		updateLog.Printf("Successfully updated workflow: %s", wf.Name)
		successfulUpdates = append(successfulUpdates, wf.Name)
	}

	// Show summary
	showUpdateSummary(successfulUpdates, failedUpdates)

	if len(successfulUpdates) == 0 {
		return fmt.Errorf("no workflows were successfully updated")
	}

	return nil
}

// findWorkflowsWithSource finds all workflows that have a source field
func findWorkflowsWithSource(workflowsDir string, filterNames []string, verbose bool) ([]*workflowWithSource, error) {
	updateLog.Printf("Finding workflows with source field in %s", workflowsDir)
	var workflows []*workflowWithSource

	// Read all .md files in workflows directory
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflows directory: %w", err)
	}
	updateLog.Printf("Found %d entries in workflows directory", len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		// Skip .lock.yml files
		if strings.HasSuffix(entry.Name(), ".lock.yml") {
			continue
		}

		workflowPath := filepath.Join(workflowsDir, entry.Name())
		workflowName := normalizeWorkflowID(entry.Name())

		// Filter by name if specified
		if len(filterNames) > 0 {
			matched := false
			for _, filterName := range filterNames {
				// Normalize filter name to handle both "workflow" and "workflow.md" formats
				filterName = normalizeWorkflowID(filterName)
				if workflowName == filterName {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		// Read the workflow file and extract source field
		content, err := os.ReadFile(workflowPath)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read %s: %v", workflowPath, err)))
			}
			continue
		}

		// Parse frontmatter
		result, err := parser.ExtractFrontmatterFromContent(string(content))
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse frontmatter in %s: %v", workflowPath, err)))
			}
			continue
		}

		// Check for source field
		sourceRaw, ok := result.Frontmatter["source"]
		if !ok {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Skipping %s: no source field", workflowName)))
			}
			continue
		}

		source, ok := sourceRaw.(string)
		if !ok || source == "" {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: invalid source field", workflowName)))
			}
			continue
		}

		workflows = append(workflows, &workflowWithSource{
			Name:       workflowName,
			Path:       workflowPath,
			SourceSpec: strings.TrimSpace(source),
		})
	}

	return workflows, nil
}

// resolveLatestRef resolves the latest ref for a workflow source
func resolveLatestRef(repo, currentRef string, allowMajor, verbose bool) (string, error) {
	updateLog.Printf("Resolving latest ref: repo=%s, currentRef=%s, allowMajor=%v", repo, currentRef, allowMajor)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Resolving latest ref for %s (current: %s)", repo, currentRef)))
	}

	// Check if current ref is a tag (looks like a semantic version)
	if isSemanticVersionTag(currentRef) {
		updateLog.Print("Current ref is semantic version tag, resolving latest release")
		return resolveLatestRelease(repo, currentRef, allowMajor, verbose)
	}

	// Check if current ref is a commit SHA (40-character hex string)
	if IsCommitSHA(currentRef) {
		updateLog.Printf("Current ref is a commit SHA: %s, returning as-is", currentRef)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Ref %s is a commit SHA, already pinned to specific commit", currentRef)))
		}
		// Commit SHAs are already pinned to a specific commit, no need to resolve
		return currentRef, nil
	}

	// Otherwise, treat as branch and get latest commit
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Treating %s as branch, getting latest commit", currentRef)))
	}

	// Get the latest commit SHA for the branch
	output, err := workflow.RunGH("Fetching branch info...", "api", fmt.Sprintf("/repos/%s/branches/%s", repo, currentRef), "--jq", ".commit.sha")
	if err != nil {
		return "", fmt.Errorf("failed to get latest commit for branch %s: %w", currentRef, err)
	}

	latestSHA := strings.TrimSpace(string(output))
	updateLog.Printf("Latest commit for branch %s: %s", currentRef, latestSHA)

	// For branches, we return the branch name, not the SHA
	// The source spec will remain as branch@branchname
	return currentRef, nil
}

// resolveLatestRelease resolves the latest compatible release for a workflow source
func resolveLatestRelease(repo, currentRef string, allowMajor, verbose bool) (string, error) {
	updateLog.Printf("Resolving latest release for repo %s (current: %s, allowMajor=%v)", repo, currentRef, allowMajor)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Checking for latest release (current: %s, allow major: %v)", currentRef, allowMajor)))
	}

	// Get all releases using gh CLI
	output, err := workflow.RunGH("Fetching releases...", "api", fmt.Sprintf("/repos/%s/releases", repo), "--jq", ".[].tag_name")
	if err != nil {
		return "", fmt.Errorf("failed to fetch releases: %w", err)
	}

	releases := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(releases) == 0 || releases[0] == "" {
		return "", fmt.Errorf("no releases found")
	}

	// Parse current version
	currentVer := parseVersion(currentRef)
	if currentVer == nil {
		// If current version is not a valid semantic version, just return the latest release
		latestRelease := releases[0]
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Current version is not valid, using latest release: %s", latestRelease)))
		}
		return latestRelease, nil
	}

	// Find the latest compatible release
	var latestCompatible string
	var latestCompatibleVersion *semanticVersion

	for _, release := range releases {
		releaseVer := parseVersion(release)
		if releaseVer == nil {
			continue
		}

		// Check if compatible based on major version
		if !allowMajor && releaseVer.major != currentVer.major {
			continue
		}

		// Check if this is newer than what we have
		if latestCompatibleVersion == nil || releaseVer.isNewer(latestCompatibleVersion) {
			latestCompatible = release
			latestCompatibleVersion = releaseVer
		}
	}

	if latestCompatible == "" {
		return "", fmt.Errorf("no compatible release found")
	}

	if verbose && latestCompatible != currentRef {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found newer release: %s", latestCompatible)))
	}

	return latestCompatible, nil
}

// updateWorkflow updates a single workflow from its source
func updateWorkflow(wf *workflowWithSource, allowMajor, force, verbose bool, engineOverride string, noStopAfter bool, stopAfter string, merge bool) error {
	updateLog.Printf("Updating workflow: name=%s, source=%s, force=%v, merge=%v", wf.Name, wf.SourceSpec, force, merge)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("\nUpdating workflow: %s", wf.Name)))
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Source: %s", wf.SourceSpec)))
	}

	// Parse source spec
	sourceSpec, err := parseSourceSpec(wf.SourceSpec)
	if err != nil {
		updateLog.Printf("Failed to parse source spec: %v", err)
		return fmt.Errorf("failed to parse source spec: %w", err)
	}

	// If no ref specified, use default branch
	currentRef := sourceSpec.Ref
	if currentRef == "" {
		currentRef = "main"
	}

	// Resolve latest ref
	latestRef, err := resolveLatestRef(sourceSpec.Repo, currentRef, allowMajor, verbose)
	if err != nil {
		return fmt.Errorf("failed to resolve latest ref: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Current ref: %s", currentRef)))
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Latest ref: %s", latestRef)))
	}

	// Check if update is needed
	if !force && currentRef == latestRef {
		updateLog.Printf("Workflow already at latest ref: %s, checking for local modifications", currentRef)

		// Download the source content to check if local file has been modified
		sourceContent, err := downloadWorkflowContent(sourceSpec.Repo, sourceSpec.Path, currentRef, verbose)
		if err != nil {
			// If we can't download for comparison, just show the up-to-date message
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to download source for comparison: %v", err)))
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow %s is already up to date (%s)", wf.Name, currentRef)))
			return nil
		}

		// Read current workflow content
		currentContent, err := os.ReadFile(wf.Path)
		if err != nil {
			return fmt.Errorf("failed to read current workflow: %w", err)
		}

		// Check if local file differs from source
		if hasLocalModifications(string(sourceContent), string(currentContent), wf.SourceSpec, verbose) {
			updateLog.Printf("Local modifications detected in workflow: %s", wf.Name)
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow %s is already up to date (%s)", wf.Name, currentRef)))
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("⚠️  Local copy of %s has been modified from source", wf.Name)))
			return nil
		}

		updateLog.Printf("Workflow %s is up to date with no local modifications", wf.Name)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow %s is already up to date (%s)", wf.Name, currentRef)))
		return nil
	}

	// Download the latest version
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Downloading latest version from %s/%s@%s", sourceSpec.Repo, sourceSpec.Path, latestRef)))
	}

	newContent, err := downloadWorkflowContent(sourceSpec.Repo, sourceSpec.Path, latestRef, verbose)
	if err != nil {
		return fmt.Errorf("failed to download workflow: %w", err)
	}

	var finalContent string
	var hasConflicts bool

	// Decide whether to merge or override
	if merge {
		// Merge mode: perform 3-way merge to preserve local changes
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Using merge mode to preserve local changes"))
		}

		// Download the base version (current ref from source)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Downloading base version from %s/%s@%s", sourceSpec.Repo, sourceSpec.Path, currentRef)))
		}

		baseContent, err := downloadWorkflowContent(sourceSpec.Repo, sourceSpec.Path, currentRef, verbose)
		if err != nil {
			return fmt.Errorf("failed to download base workflow: %w", err)
		}

		// Read current workflow content
		currentContent, err := os.ReadFile(wf.Path)
		if err != nil {
			return fmt.Errorf("failed to read current workflow: %w", err)
		}

		// Perform 3-way merge using git merge-file
		updateLog.Printf("Performing 3-way merge for workflow: %s", wf.Name)
		mergedContent, conflicts, err := MergeWorkflowContent(string(baseContent), string(currentContent), string(newContent), wf.SourceSpec, latestRef, verbose)
		if err != nil {
			updateLog.Printf("Merge failed for workflow %s: %v", wf.Name, err)
			return fmt.Errorf("failed to merge workflow content: %w", err)
		}

		finalContent = mergedContent
		hasConflicts = conflicts

		if hasConflicts {
			updateLog.Printf("Merge conflicts detected in workflow: %s", wf.Name)
		}
	} else {
		// Override mode (default): replace local file with new content from source
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Using override mode - local changes will be replaced"))
		}

		// Update the source field in the new content with the new ref
		newWithUpdatedSource, err := UpdateFieldInFrontmatter(string(newContent), "source", fmt.Sprintf("%s/%s@%s", sourceSpec.Repo, sourceSpec.Path, latestRef))
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update source in new content: %v", err)))
			}
			// Continue with original new content
			finalContent = string(newContent)
		} else {
			finalContent = newWithUpdatedSource
		}

		// Process @include directives if present
		workflow := &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug: sourceSpec.Repo,
				Version:  latestRef,
			},
			WorkflowPath: sourceSpec.Path,
		}

		processedContent, err := processIncludesInContent(finalContent, workflow, latestRef, verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to process includes: %v", err)))
			}
			// Continue with unprocessed content
		} else {
			finalContent = processedContent
		}
	}

	// Handle stop-after field modifications
	if noStopAfter {
		// Remove stop-after field if requested
		cleanedContent, err := RemoveFieldFromOnTrigger(finalContent, "stop-after")
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove stop-after field: %v", err)))
			}
		} else {
			finalContent = cleanedContent
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Removed stop-after field from workflow"))
			}
		}
	} else if stopAfter != "" {
		// Set custom stop-after value if provided
		updatedContent, err := SetFieldInOnTrigger(finalContent, "stop-after", stopAfter)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to set stop-after field: %v", err)))
			}
		} else {
			finalContent = updatedContent
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Set stop-after field to: %s", stopAfter)))
			}
		}
	}

	// Write updated content
	if err := os.WriteFile(wf.Path, []byte(finalContent), 0644); err != nil {
		return fmt.Errorf("failed to write updated workflow: %w", err)
	}

	if hasConflicts {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Updated %s from %s to %s with CONFLICTS - please review and resolve manually", wf.Name, currentRef, latestRef)))
		return nil // Not an error, but user needs to resolve conflicts
	}

	updateLog.Printf("Successfully updated workflow %s from %s to %s", wf.Name, currentRef, latestRef)
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Updated %s from %s to %s", wf.Name, currentRef, latestRef)))

	// Compile the updated workflow with refreshStopTime enabled
	updateLog.Printf("Compiling updated workflow: %s", wf.Name)
	if err := compileWorkflowWithRefresh(wf.Path, verbose, false, engineOverride, true); err != nil {
		updateLog.Printf("Compilation failed for workflow %s: %v", wf.Name, err)
		return fmt.Errorf("failed to compile updated workflow: %w", err)
	}

	return nil
}
