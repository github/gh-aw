package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var statusLog = logger.New("cli:status_command")

// WorkflowStatus represents the status of a single workflow for JSON output
type WorkflowStatus struct {
	Workflow      string   `json:"workflow" console:"header:Workflow"`
	EngineID      string   `json:"engine_id" console:"header:Engine"`
	Compiled      string   `json:"compiled" console:"header:Compiled"`
	Status        string   `json:"status" console:"header:Status"`
	TimeRemaining string   `json:"time_remaining" console:"header:Time Remaining"`
	Labels        []string `json:"labels,omitempty" console:"header:Labels,omitempty"`
	On            any      `json:"on,omitempty" console:"-"`
	RunStatus     string   `json:"run_status,omitempty" console:"header:Run Status,omitempty"`
	RunConclusion string   `json:"run_conclusion,omitempty" console:"header:Run Conclusion,omitempty"`
}

// GetWorkflowStatuses retrieves workflow status information and returns it as a slice.
// This function is designed for programmatic access (e.g., from MCP server).
// For CLI usage, use StatusWorkflows which handles output formatting.
func GetWorkflowStatuses(pattern string, ref string, labelFilter string, repoOverride string) ([]WorkflowStatus, error) {
	statusLog.Printf("Getting workflow statuses: pattern=%s, ref=%s, labelFilter=%s, repo=%s", pattern, ref, labelFilter, repoOverride)

	mdFiles, err := getMarkdownWorkflowFiles("")
	if err != nil {
		statusLog.Printf("Failed to get markdown workflow files: %v", err)
		return nil, fmt.Errorf("failed to get markdown workflow files: %w", err)
	}

	statusLog.Printf("Found %d markdown workflow files", len(mdFiles))
	if len(mdFiles) == 0 {
		return []WorkflowStatus{}, nil
	}

	// Get GitHub workflows data
	statusLog.Print("Fetching GitHub workflow status")
	githubWorkflows, err := fetchGitHubWorkflows(repoOverride, false)
	if err != nil {
		statusLog.Printf("Failed to fetch GitHub workflows: %v", err)
		githubWorkflows = make(map[string]*GitHubWorkflow)
	} else {
		statusLog.Printf("Successfully fetched %d GitHub workflows", len(githubWorkflows))
	}

	// Fetch latest workflow runs for ref if specified
	var latestRunsByWorkflow map[string]*WorkflowRun
	if ref != "" {
		latestRunsByWorkflow, err = fetchLatestRunsByRef(ref, repoOverride, false)
		if err != nil {
			statusLog.Printf("Failed to fetch workflow runs for ref %s: %v", ref, err)
			latestRunsByWorkflow = make(map[string]*WorkflowRun)
		} else {
			statusLog.Printf("Successfully fetched %d workflow runs for ref %s", len(latestRunsByWorkflow), ref)
		}
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

		if _, err := os.Stat(lockFile); err == nil {
			// Check if up to date
			mdStat, _ := os.Stat(file)
			lockStat, _ := os.Stat(lockFile)
			if mdStat.ModTime().After(lockStat.ModTime()) {
				compiled = "No"
			} else {
				compiled = "Yes"
			}

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
		if content, err := os.ReadFile(file); err == nil {
			if result, err := parser.ExtractFrontmatterFromContent(string(content)); err == nil {
				if result.Frontmatter != nil {
					onField = result.Frontmatter["on"]
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
		var runStatus, runConclusion string
		if latestRunsByWorkflow != nil {
			if run, exists := latestRunsByWorkflow[name]; exists {
				runStatus = run.Status
				runConclusion = run.Conclusion
			}
		}

		// Build status object
		statuses = append(statuses, WorkflowStatus{
			Workflow:      name,
			EngineID:      agent,
			Compiled:      compiled,
			Status:        status,
			TimeRemaining: timeRemaining,
			Labels:        labels,
			On:            onField,
			RunStatus:     runStatus,
			RunConclusion: runConclusion,
		})
	}

	return statuses, nil
}

func StatusWorkflows(pattern string, verbose bool, jsonOutput bool, ref string, labelFilter string, repoOverride string) error {
	statusLog.Printf("Checking workflow status: pattern=%s, jsonOutput=%v, ref=%s, labelFilter=%s, repo=%s", pattern, jsonOutput, ref, labelFilter, repoOverride)
	if verbose && !jsonOutput {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking status of workflow files"))
		if pattern != "" {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Filtering by pattern: %s", pattern)))
		}
	}

	// Verbose logging for network operations
	if verbose && !jsonOutput {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fetching GitHub workflow status..."))
	}

	// Get workflow statuses
	statuses, err := GetWorkflowStatuses(pattern, ref, labelFilter, repoOverride)
	if err != nil {
		statusLog.Printf("Failed to get workflow statuses: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return nil
	}

	// Additional verbose output after successful fetch
	if verbose && !jsonOutput && len(statuses) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully fetched status for %d workflows", len(statuses))))
	}

	// Handle output
	if jsonOutput {
		// Output JSON
		jsonBytes, err := json.MarshalIndent(statuses, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(jsonBytes))
		return nil
	}

	// Handle empty result for text output
	if len(statuses) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflow files found."))
		return nil
	}

	// Render the table using struct-based rendering
	fmt.Print(console.RenderStruct(statuses))

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

// StatusWorkflows shows status of workflows
// getMarkdownWorkflowFiles finds all markdown files in .github/workflows directory
func getMarkdownWorkflowFiles(workflowDir string) ([]string, error) {
	// Use provided directory or default
	workflowsDir := workflowDir
	if workflowsDir == "" {
		workflowsDir = getWorkflowsDir()
	}

	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("no %s directory found", workflowsDir)
	}

	// Find all markdown files in workflow directory
	mdFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	if err != nil {
		return nil, fmt.Errorf("failed to find workflow files: %w", err)
	}

	// Filter out README.md files
	mdFiles = filterWorkflowFiles(mdFiles)

	return mdFiles, nil
}

// Helper functions

func extractWorkflowNameFromFile(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	// Extract markdown content (excluding frontmatter)
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return "", err
	}

	// Look for first H1 header
	scanner := bufio.NewScanner(strings.NewReader(result.Markdown))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:]), nil
		}
	}

	// No H1 header found, generate default name from filename
	baseName := filepath.Base(filePath)
	baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	baseName = strings.ReplaceAll(baseName, "-", " ")

	// Capitalize first letter of each word
	words := strings.Fields(baseName)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}

	return strings.Join(words, " "), nil
}

// extractEngineIDFromFile extracts the engine ID from a workflow file's frontmatter
func extractEngineIDFromFile(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "" // Return empty string if file cannot be read
	}

	// Parse frontmatter
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return "" // Return empty string if frontmatter cannot be parsed
	}

	// Use the workflow package's extractEngineConfig to handle both string and object formats
	compiler := &workflow.Compiler{}
	engineSetting, engineConfig := compiler.ExtractEngineConfig(result.Frontmatter)

	// If engine is specified, return the ID from the config
	if engineConfig != nil && engineConfig.ID != "" {
		return engineConfig.ID
	}

	// If we have an engine setting string, return it
	if engineSetting != "" {
		return engineSetting
	}

	return "copilot" // Default engine
}

// fetchLatestRunsByRef fetches the latest workflow run for each workflow from a specific ref (branch or tag)
func fetchLatestRunsByRef(ref string, repoOverride string, verbose bool) (map[string]*WorkflowRun, error) {
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
	cmd := workflow.ExecGH(args...)
	output, err := cmd.Output()
	if err != nil {
		// Stop spinner on error
		if !verbose {
			spinner.Stop()
		}

		// Extract detailed error information including exit code and stderr
		var exitCode int
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
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
		return nil, fmt.Errorf("gh run list returned empty output")
	}

	// Validate JSON before unmarshaling
	if !json.Valid(output) {
		if !verbose {
			spinner.Stop()
		}
		return nil, fmt.Errorf("gh run list returned invalid JSON")
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
