package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2/tree"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/workflow"
)

var statusLog = logger.New("cli:status_command")

// WorkflowStatus represents the status of a single workflow for JSON output.
// It embeds WorkflowListItem so that both list and status commands share the
// same source of truth for the common workflow metadata fields.
type WorkflowStatus struct {
	WorkflowListItem
	Status        string   `json:"status" console:"header:state"`
	TimeRemaining string   `json:"time_remaining" console:"header:remaining"`
	Dependencies  []string `json:"dependencies,omitempty" console:"-"`
	RunID         int64    `json:"run_id,omitempty" console:"header:run id,omitempty"`
	RunStatus     string   `json:"run_status,omitempty" console:"header:status,omitempty"`
	RunConclusion string   `json:"run_conclusion,omitempty" console:"header:conclusion,omitempty"`
}

// GetWorkflowStatuses retrieves workflow status information and returns it as a slice.
// This function is designed for programmatic access (e.g., from MCP server).
// For CLI usage, use StatusWorkflows which handles output formatting.
func GetWorkflowStatuses(ctx context.Context, pattern string, ref string, labelFilter string, repoOverride string) ([]WorkflowStatus, error) {
	statusLog.Printf("Getting workflow statuses: pattern=%s, ref=%s, labelFilter=%s, repo=%s", pattern, ref, labelFilter, repoOverride)

	// Get GitHub workflows data
	statusLog.Print("Fetching GitHub workflow status")
	githubWorkflows, err := fetchGitHubWorkflows(ctx, repoOverride, false)
	if err != nil {
		statusLog.Printf("Failed to fetch GitHub workflows: %v", err)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		githubWorkflows = make(map[string]*GitHubWorkflow)
	} else {
		statusLog.Printf("Successfully fetched %d GitHub workflows", len(githubWorkflows))
	}

	// Fetch latest workflow runs for ref if specified
	var latestRunsByWorkflow map[string]*WorkflowRun
	if ref != "" {
		latestRunsByWorkflow, err = fetchLatestRunsByRef(ctx, ref, repoOverride, false)
		if err != nil {
			statusLog.Printf("Failed to fetch workflow runs for ref %s: %v", ref, err)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			latestRunsByWorkflow = make(map[string]*WorkflowRun)
		} else {
			statusLog.Printf("Successfully fetched %d workflow runs for ref %s", len(latestRunsByWorkflow), ref)
		}
	}

	// When --repo is specified, build statuses from GitHub API data only.
	// Local markdown files are not available for a remote repository, so
	// local-only fields (EngineID, Compiled, TimeRemaining, Labels, On) are
	// omitted from the results.
	if repoOverride != "" {
		// Label metadata is not exposed by the GitHub Actions workflow API, so
		// filtering by label is not supported when --repo is specified.
		if labelFilter != "" {
			return nil, errors.New("--label filter is not supported with --repo: label information is not available from the GitHub Actions API")
		}
		return buildRemoteWorkflowStatuses(pattern, githubWorkflows, latestRunsByWorkflow), nil
	}

	// Local path: discover markdown workflow files from the local filesystem.
	mdFiles, err := getMarkdownWorkflowFiles("")
	if err != nil {
		statusLog.Printf("Failed to get markdown workflow files: %v", err)
		return nil, fmt.Errorf("failed to get markdown workflow files: %w", err)
	}

	statusLog.Printf("Found %d markdown workflow files", len(mdFiles))
	if len(mdFiles) == 0 {
		return []WorkflowStatus{}, nil
	}

	// Build status list
	var statuses []WorkflowStatus
	for _, file := range mdFiles {
		base := filepath.Base(file)
		name := strings.TrimSuffix(base, ".md")

		// Skip if pattern specified and doesn't match
		if pattern != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(pattern)) {
			continue
		}

		// Extract engine ID from workflow file
		agent := extractEngineIDFromFile(file)

		// Check if compiled (.lock.yml file is in .github/workflows)
		lockFile := stringutil.MarkdownToLockFile(file)
		compiled := "N/A"
		timeRemaining := "N/A"

		if fileutil.FileExists(lockFile) {
			// Check if up to date using hash comparison
			compiled = isCompiledUpToDate(file, lockFile)

			// Extract stop-time from lock file
			if stopTime := workflow.ExtractStopTimeFromLockFile(lockFile); stopTime != "" {
				timeRemaining = calculateTimeRemaining(stopTime)
			}
		}

		// Get GitHub workflow status
		status := "Unknown"
		if workflow, exists := githubWorkflows[name]; exists {
			if workflow.State == "disabled_manually" {
				status = "disabled"
			} else {
				status = workflow.State
			}
		}

		// Extract "on" field and labels from frontmatter
		var onField any
		var labels []string
		var dependencies []string
		if content, err := os.ReadFile(file); err == nil {
			contentStr := string(content)
			if result, err := parser.ExtractFrontmatterFromContent(contentStr); err == nil {
				if result.Frontmatter != nil {
					onField = result.Frontmatter["on"]
					dependencies = extractWorkflowDependencies(contentStr, result.Frontmatter)
					// Extract labels field if present
					if labelsField, ok := result.Frontmatter["labels"]; ok {
						if labelsArray, ok := labelsField.([]any); ok {
							for _, label := range labelsArray {
								if labelStr, ok := label.(string); ok {
									labels = append(labels, labelStr)
								}
							}
						}
					}
				}
			}
		}

		// Skip if label filter specified and workflow doesn't have the label
		if labelFilter != "" {
			hasLabel := false
			for _, label := range labels {
				if strings.EqualFold(label, labelFilter) {
					hasLabel = true
					break
				}
			}
			if !hasLabel {
				continue
			}
		}

		// Get run status for ref if available
		var runID int64
		var runStatus, runConclusion string
		if latestRunsByWorkflow != nil {
			if run, exists := latestRunsByWorkflow[name]; exists {
				runID = run.DatabaseID
				runStatus = run.Status
				runConclusion = run.Conclusion
			}
		}

		// Build status object
		statuses = append(statuses, WorkflowStatus{
			WorkflowListItem: WorkflowListItem{
				Workflow: name,
				EngineID: agent,
				Compiled: compiled,
				Labels:   labels,
				On:       onField,
			},
			Status:        status,
			TimeRemaining: timeRemaining,
			Dependencies:  dependencies,
			RunID:         runID,
			RunStatus:     runStatus,
			RunConclusion: runConclusion,
		})
	}

	return statuses, nil
}

// buildRemoteWorkflowStatuses constructs workflow statuses from GitHub API data when
// --repo is specified. Local-only fields (EngineID, Compiled, TimeRemaining, Labels,
// On) are not available for remote repositories and are omitted from results.
func buildRemoteWorkflowStatuses(pattern string, githubWorkflows map[string]*GitHubWorkflow, latestRunsByWorkflow map[string]*WorkflowRun) []WorkflowStatus {
	statuses := make([]WorkflowStatus, 0, len(githubWorkflows))
	for name, wf := range githubWorkflows {
		// Skip if pattern specified and doesn't match
		if pattern != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(pattern)) {
			continue
		}

		status := wf.State
		if wf.State == "disabled_manually" {
			status = "disabled"
		}

		var runID int64
		var runStatus, runConclusion string
		if latestRunsByWorkflow != nil {
			if run, exists := latestRunsByWorkflow[name]; exists {
				runID = run.DatabaseID
				runStatus = run.Status
				runConclusion = run.Conclusion
			}
		}

		statuses = append(statuses, WorkflowStatus{
			// Remote workflow status only includes the workflow name here; the
			// GitHub Actions API response does not provide list metadata fields.
			WorkflowListItem: WorkflowListItem{
				Workflow: name,
			},
			Status:        status,
			RunID:         runID,
			RunStatus:     runStatus,
			RunConclusion: runConclusion,
		})
	}
	return statuses
}

func StatusWorkflows(ctx context.Context, pattern string, verbose bool, jsonOutput bool, ref string, labelFilter string, repoOverride string) error {
	statusLog.Printf("Checking workflow status: pattern=%s, jsonOutput=%v, ref=%s, labelFilter=%s, repo=%s", pattern, jsonOutput, ref, labelFilter, repoOverride)
	if verbose && !jsonOutput {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking status of workflow files"))
		if pattern != "" {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Filtering by pattern: "+pattern))
		}
	}

	// Verbose logging for network operations
	if verbose && !jsonOutput {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fetching GitHub workflow status..."))
	}

	// Get workflow statuses
	statuses, err := GetWorkflowStatuses(ctx, pattern, ref, labelFilter, repoOverride)
	if err != nil {
		statusLog.Printf("Failed to get workflow statuses: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return err
	}

	// Additional verbose output after successful fetch
	if verbose && !jsonOutput && len(statuses) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully fetched status for %d workflows", len(statuses))))
	}

	// Handle output
	if jsonOutput {
		// Output JSON
		jsonBytes, err := marshalIndentJSONOrWrap(statuses, "workflow statuses")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(jsonBytes))
		return nil
	}

	// Handle empty result for text output
	if len(statuses) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflow files found."))
		return nil
	}

	// Render the table using struct-based rendering
	fmt.Fprint(os.Stdout, console.RenderStruct(statuses))
	if verbose {
		if dependenciesTree := renderWorkflowDependencyTree(statuses); dependenciesTree != "" {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, dependenciesTree)
		}
	}

	return nil
}

// Removed duplicate code - now everything goes through GetWorkflowStatuses

// calculateTimeRemaining calculates and formats the time remaining until stop-time
func calculateTimeRemaining(stopTimeStr string) string {
	if stopTimeStr == "" {
		return "N/A"
	}

	// Parse the stop time in local timezone
	stopTime, err := time.ParseInLocation("2006-01-02 15:04:05", stopTimeStr, time.Local)
	if err != nil {
		return "Invalid"
	}

	now := time.Now()
	remaining := stopTime.Sub(now)

	// If already past the stop time
	if remaining <= 0 {
		return "Expired"
	}

	// Format the remaining time in a human-readable way
	days := int(remaining.Hours() / 24)
	hours := int(remaining.Hours()) % 24
	minutes := int(remaining.Minutes()) % 60

	if days > 0 {
		if days == 1 {
			return fmt.Sprintf("%dd %dh", days, hours)
		}
		return fmt.Sprintf("%dd %dh", days, hours)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	} else {
		return "< 1m"
	}
}

// isCompiledUpToDate checks if a workflow's lock file is up to date with the current source
// using hash-based comparison. Falls back to "Yes" when no hash is available (legacy lock files).
func isCompiledUpToDate(workflowPath, lockFilePath string) string {
	return isCompiledUpToDateWithCache(workflowPath, lockFilePath, parser.NewImportCache(""))
}

func extractWorkflowDependencies(content string, frontmatter map[string]any) []string {
	unique := make(map[string]struct{})
	addDependency := func(dependency string) {
		normalized := normalizeWorkflowDependency(dependency)
		if normalized != "" {
			unique[normalized] = struct{}{}
		}
	}

	if frontmatter != nil {
		addDependenciesFromImports(frontmatter["imports"], addDependency)
	}

	includes, err := findIncludesInContent(content)
	if err == nil {
		for _, include := range includes {
			addDependency(include)
		}
	}

	if len(unique) == 0 {
		return nil
	}

	dependencies := make([]string, 0, len(unique))
	for dependency := range unique {
		dependencies = append(dependencies, dependency)
	}
	slices.Sort(dependencies)
	return dependencies
}

// addDependenciesFromImports collects dependency paths from supported imports formats:
// string, []string, []any (string or object with "path"/"uses"), and object form
// map[string]any with nested "aw" entries. The recursive call handles imports.aw
// structures that mirror the parser's object-form import syntax.
func addDependenciesFromImports(imports any, addDependency func(string)) {
	switch value := imports.(type) {
	case string:
		addDependency(value)
	case []string:
		for _, item := range value {
			addDependency(item)
		}
	case []any:
		for _, item := range value {
			switch importItem := item.(type) {
			case string:
				addDependency(importItem)
			case map[string]any:
				if uses, ok := importItem["uses"].(string); ok {
					addDependency(uses)
				} else if path, ok := importItem["path"].(string); ok {
					addDependency(path)
				}
			}
		}
	case map[string]any:
		// Object form: imports: { aw: [...] }
		if aw, ok := value["aw"]; ok {
			addDependenciesFromImports(aw, addDependency)
			return
		}
		// Allow direct single-object form for resilience.
		if uses, ok := value["uses"].(string); ok {
			addDependency(uses)
		} else if path, ok := value["path"].(string); ok {
			addDependency(path)
		}
	}
}

func normalizeWorkflowDependency(dependency string) string {
	dependency = strings.TrimSpace(dependency)
	if dependency == "" {
		return ""
	}
	if path, _, ok := strings.Cut(dependency, "#"); ok {
		return strings.TrimSpace(path)
	}
	return dependency
}

func renderWorkflowDependencyTree(statuses []WorkflowStatus) string {
	root := tree.Root("Workflow Dependencies")
	hasDependencies := false

	for _, status := range statuses {
		if len(status.Dependencies) == 0 {
			continue
		}
		hasDependencies = true

		workflowNode := tree.Root(status.Workflow)
		for _, dependency := range status.Dependencies {
			workflowNode.Child(dependency)
		}
		root.Child(workflowNode)
	}

	if !hasDependencies {
		return ""
	}

	return root.
		Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(styles.TreeEnumerator).
		ItemStyle(styles.TreeNode).
		String()
}

// isCompiledUpToDateWithCache is the same as isCompiledUpToDate but accepts a shared
// ImportCache so callers that check multiple workflows can avoid creating a new cache
// on every call.
func isCompiledUpToDateWithCache(workflowPath, lockFilePath string, cache *parser.ImportCache) string {
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		return "No"
	}

	metadata, _, err := workflow.ExtractMetadataFromLockFile(string(lockContent))
	if err != nil || metadata == nil || metadata.FrontmatterHash == "" {
		return "No"
	}

	currentHash, err := parser.ComputeFrontmatterHashFromFile(workflowPath, cache)
	if err != nil {
		statusLog.Printf("Failed to compute frontmatter hash for %s: %v", workflowPath, err)
		return "No"
	}

	if currentHash != metadata.FrontmatterHash {
		return "No"
	}
	if metadata.BodyHash != "" {
		content, err := os.ReadFile(workflowPath)
		if err != nil {
			return "No"
		}
		result, err := parser.ExtractFrontmatterFromContent(string(content))
		if err != nil {
			return "No"
		}
		_, fullStaleCheck := workflow.ParseStaleCheckMode(result.Frontmatter)
		if fullStaleCheck {
			bodyHash, err := parser.ComputeBodyHashFromFile(workflowPath)
			if err != nil || bodyHash != metadata.BodyHash {
				return "No"
			}
		}
	}
	return "Yes"
}

// fetchLatestRunsByRef fetches the latest workflow run for each workflow from a specific ref (branch or tag)
func fetchLatestRunsByRef(ctx context.Context, ref string, repoOverride string, verbose bool) (map[string]*WorkflowRun, error) {
	statusLog.Printf("Fetching latest workflow runs for ref: %s, repo: %s", ref, repoOverride)

	// Start spinner for network operation (only if not in verbose mode)
	spinner := console.NewSpinner("Fetching workflow runs for ref...")
	if !verbose {
		spinner.Start()
	}

	// Fetch workflow runs for the ref (uses --branch flag which also works for tags)
	args := []string{"run", "list", "--branch", ref, "--json", "databaseId,number,url,status,conclusion,workflowName,createdAt,headBranch", "--limit", "100"}
	if repoOverride != "" {
		args = append(args, "--repo", repoOverride)
	}
	cmd := workflow.ExecGHContext(ctx, args...)
	output, err := cmd.Output()

	if err != nil {
		// Stop spinner on error
		if !verbose {
			spinner.Stop()
		}

		// Extract detailed error information including exit code and stderr
		var exitCode int
		var stderr string
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
			stderr = string(exitErr.Stderr)
			statusLog.Printf("gh run list command failed with exit code %d. Command: gh %v", exitCode, args)
			statusLog.Printf("stderr output: %s", stderr)

			// Check for invalid field errors first (before generic error)
			// GitHub CLI returns these when JSON fields don't exist or are misspelled
			combinedMsg := err.Error() + " " + stderr
			if strings.Contains(combinedMsg, "invalid field") ||
				strings.Contains(combinedMsg, "unknown field") ||
				strings.Contains(combinedMsg, "field not found") ||
				strings.Contains(combinedMsg, "no such field") {
				return nil, fmt.Errorf("invalid field in JSON query (exit code %d): %s", exitCode, stderr)
			}

			return nil, fmt.Errorf("failed to execute gh run list command (exit code %d): %w. stderr: %s", exitCode, err, stderr)
		}

		// If not an ExitError, log what we can
		statusLog.Printf("gh run list command failed with error (not ExitError): %v. Command: gh %v", err, args)
		return nil, fmt.Errorf("failed to execute gh run list command: %w", err)
	}

	// Check if output is empty
	if len(output) == 0 {
		if !verbose {
			spinner.Stop()
		}
		return nil, errors.New("gh run list returned empty output")
	}

	// Validate JSON before unmarshaling
	if !json.Valid(output) {
		if !verbose {
			spinner.Stop()
		}
		return nil, errors.New("gh run list returned invalid JSON")
	}

	var runs []WorkflowRun
	if err := json.Unmarshal(output, &runs); err != nil {
		if !verbose {
			spinner.Stop()
		}
		return nil, fmt.Errorf("failed to parse workflow runs: %w", err)
	}

	// Stop spinner with success message
	if !verbose {
		spinner.StopWithMessage(fmt.Sprintf("✓ Fetched %d workflow runs", len(runs)))
	}

	// Build map of latest run for each workflow (first occurrence is the latest)
	latestRuns := make(map[string]*WorkflowRun)
	for i := range runs {
		run := &runs[i]
		// Extract workflow name from workflowName field
		workflowName := extractWorkflowNameFromPath(run.WorkflowName)
		// Only keep the first (latest) run for each workflow
		if _, exists := latestRuns[workflowName]; !exists {
			latestRuns[workflowName] = run
		}
	}

	statusLog.Printf("Fetched latest runs for %d workflows on ref %s", len(latestRuns), ref)
	return latestRuns, nil
}
