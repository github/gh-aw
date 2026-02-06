package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var workflowsLog = logger.New("cli:workflows")

// getPackagesDir returns the global packages directory path
func getPackagesDir() (string, error) {
	// Use global directory under user's home
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	return filepath.Join(homeDir, ".aw", "packages"), nil
}

func getWorkflowsDir() string {
	return ".github/workflows"
}

// readWorkflowFile reads a workflow file from either filesystem
func readWorkflowFile(filePath string, workflowsDir string) ([]byte, string, error) {
	// Using local filesystem
	var fullPath string

	// Check if filePath is already an absolute path
	if filepath.IsAbs(filePath) {
		fullPath = filePath
	} else {
		// Join relative path with workflowsDir
		fullPath = filepath.Join(workflowsDir, filePath)
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read workflow file %s: %w", fullPath, err)
	}
	return content, fullPath, nil
}

// GitHubWorkflow represents a workflow from GitHub API
type GitHubWorkflow struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// fetchGitHubWorkflows fetches workflow information from GitHub
func fetchGitHubWorkflows(repoOverride string, verbose bool) (map[string]*GitHubWorkflow, error) {
	workflowsLog.Printf("Fetching GitHub workflows: repoOverride=%s", repoOverride)

	// Start spinner for network operation (only if not in verbose mode)
	spinner := console.NewSpinner("Fetching GitHub workflow status...")
	if !verbose {
		spinner.Start()
	}

	args := []string{"workflow", "list", "--all", "--json", "id,name,path,state"}
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
			workflowsLog.Printf("gh workflow list command failed with exit code %d. Command: gh %v", exitCode, args)
			workflowsLog.Printf("stderr output: %s", stderr)

			return nil, fmt.Errorf("failed to execute gh workflow list command (exit code %d): %w. stderr: %s", exitCode, err, stderr)
		}

		// If not an ExitError, log what we can
		workflowsLog.Printf("gh workflow list command failed with error (not ExitError): %v. Command: gh %v", err, args)
		return nil, fmt.Errorf("failed to execute gh workflow list command: %w", err)
	}

	// Check if output is empty
	if len(output) == 0 {
		if !verbose {
			spinner.Stop()
		}
		return nil, fmt.Errorf("gh workflow list returned empty output - check if repository has workflows and gh CLI is authenticated")
	}

	// Validate JSON before unmarshaling
	if !json.Valid(output) {
		if !verbose {
			spinner.Stop()
		}
		return nil, fmt.Errorf("gh workflow list returned invalid JSON - this may be due to network issues or authentication problems")
	}

	var workflows []GitHubWorkflow
	if err := json.Unmarshal(output, &workflows); err != nil {
		if !verbose {
			spinner.Stop()
		}
		return nil, fmt.Errorf("failed to parse workflow data: %w", err)
	}

	workflowMap := make(map[string]*GitHubWorkflow)
	for i, workflow := range workflows {
		name := extractWorkflowNameFromPath(workflow.Path)
		workflowMap[name] = &workflows[i]
	}

	// Count user workflows (those with .md files) vs internal workflows
	mdFiles, _ := getMarkdownWorkflowFiles("")
	mdWorkflowNames := make(map[string]bool)
	for _, file := range mdFiles {
		name := extractWorkflowNameFromPath(file)
		mdWorkflowNames[name] = true
	}

	var userWorkflowCount, internalWorkflowCount int
	for name := range workflowMap {
		if mdWorkflowNames[name] {
			userWorkflowCount++
		} else {
			internalWorkflowCount++
		}
	}

	// Stop spinner with success message showing user and internal workflow counts
	if !verbose {
		if internalWorkflowCount > 0 {
			spinner.StopWithMessage(fmt.Sprintf("✓ Fetched %d public and %d internal workflows", userWorkflowCount, internalWorkflowCount))
		} else {
			spinner.StopWithMessage(fmt.Sprintf("✓ Fetched %d workflows", userWorkflowCount))
		}
	}

	workflowsLog.Printf("Fetched %d GitHub workflows (%d user, %d internal)", len(workflowMap), userWorkflowCount, internalWorkflowCount)
	return workflowMap, nil
}

// extractWorkflowNameFromPath extracts workflow name from path
func extractWorkflowNameFromPath(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	return strings.TrimSuffix(name, ".lock")
}

// getWorkflowStatus gets the status of a single workflow by name
func getWorkflowStatus(workflowIdOrName string, repoOverride string, verbose bool) (*GitHubWorkflow, error) {
	workflowsLog.Printf("Getting workflow status: workflow=%s", workflowIdOrName)

	// Extract workflow name for lookup
	filename := strings.TrimSuffix(filepath.Base(workflowIdOrName), ".md")

	// Get all GitHub workflows
	githubWorkflows, err := fetchGitHubWorkflows(repoOverride, verbose)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub workflows: %w", err)
	}

	// Find the workflow
	if workflow, exists := githubWorkflows[filename]; exists {
		return workflow, nil
	}

	suggestions := []string{
		fmt.Sprintf("Run '%s status' to see all available workflows", string(constants.CLIExtensionPrefix)),
		"Check if the workflow has been compiled and pushed to GitHub",
		"Verify the workflow name matches the compiled .lock.yml file",
	}
	return nil, errors.New(console.FormatErrorWithSuggestions(
		fmt.Sprintf("workflow '%s' not found on GitHub", workflowIdOrName),
		suggestions,
	))
}

// restoreWorkflowState restores a workflow to disabled state if it was previously disabled
func restoreWorkflowState(workflowIdOrName string, workflowID int64, repoOverride string, verbose bool) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Restoring workflow '%s' to disabled state...", workflowIdOrName)))
	}

	args := []string{"workflow", "disable", strconv.FormatInt(workflowID, 10)}
	if repoOverride != "" {
		args = append(args, "--repo", repoOverride)
	}
	cmd := workflow.ExecGH(args...)
	if err := cmd.Run(); err != nil {
		// Extract detailed error information including exit code and stderr
		var exitCode int
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			stderr = string(exitErr.Stderr)
			workflowsLog.Printf("gh workflow disable command failed with exit code %d. Command: gh %v", exitCode, args)
			workflowsLog.Printf("stderr output: %s", stderr)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to restore workflow '%s' to disabled state (exit code %d): %v. stderr: %s", workflowIdOrName, exitCode, err, stderr)))
		} else {
			workflowsLog.Printf("gh workflow disable command failed with error (not ExitError): %v. Command: gh %v", err, args)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to restore workflow '%s' to disabled state: %v", workflowIdOrName, err)))
		}
	} else {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Restored workflow to disabled state: %s", workflowIdOrName)))
	}
}

// getAvailableWorkflowNames returns a list of available workflow names (without .md extension)
func getAvailableWorkflowNames() []string {
	mdFiles, err := getMarkdownWorkflowFiles("")
	if err != nil {
		return nil
	}

	return sliceutil.Map(mdFiles, func(file string) string {
		return strings.TrimSuffix(filepath.Base(file), ".md")
	})
}

// suggestWorkflowNames returns up to 3 similar workflow names using fuzzy matching
// The target can be a workflow name or filename (with or without .md extension)
func suggestWorkflowNames(target string) []string {
	availableNames := getAvailableWorkflowNames()
	if len(availableNames) == 0 {
		return nil
	}

	// Normalize target: strip .md extension and get basename if it's a path
	normalizedTarget := strings.TrimSuffix(filepath.Base(target), ".md")

	// Use the existing FindClosestMatches function from parser package
	return parser.FindClosestMatches(normalizedTarget, availableNames, 3)
}

// isWorkflowFile returns true if the file should be treated as a workflow file.
// README.md files are excluded as they are documentation, not workflows.
func isWorkflowFile(filename string) bool {
	base := strings.ToLower(filepath.Base(filename))
	return base != "readme.md"
}

// filterWorkflowFiles filters out non-workflow files from a list of markdown files.
func filterWorkflowFiles(files []string) []string {
	return sliceutil.Filter(files, isWorkflowFile)
}
