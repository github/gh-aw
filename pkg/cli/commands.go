package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var commandsLog = logger.New("cli:commands")

// Package-level version information
var (
	version = "dev"
)

func init() {
	// Set the default version in the workflow package
	// This allows workflow.NewCompiler() to auto-detect the version
	workflow.SetDefaultVersion(version)
}

// SetVersionInfo sets the version information for the CLI and workflow package
func SetVersionInfo(v string) {
	version = v
	workflow.SetDefaultVersion(v) // Keep workflow package in sync
}

// GetVersion returns the current version
func GetVersion() string {
	return version
}

// downloadAgentFileFromGitHub downloads the agentic-workflows.agent.md file from GitHub
func downloadAgentFileFromGitHub(verbose bool) (string, error) {
	commandsLog.Print("Downloading agentic-workflows.agent.md from GitHub")

	// Determine the ref to use (tag for releases, main for dev builds)
	ref := "main"
	currentVersion := GetVersion()

	// If version looks like a release tag (starts with v and contains dots), use it
	isRelease := strings.HasPrefix(currentVersion, "v") && strings.Contains(currentVersion, ".") && !strings.Contains(currentVersion, "dirty")
	if isRelease {
		ref = currentVersion
		commandsLog.Printf("Using release tag: %s", ref)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using release version: %s", ref)))
		}
	} else {
		commandsLog.Print("Using main branch for dev build")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Using main branch (dev build)"))
		}
	}

	// Construct the raw GitHub URL
	url := fmt.Sprintf("https://raw.githubusercontent.com/github/gh-aw/%s/.github/agents/agentic-workflows.agent.md", ref)
	commandsLog.Printf("Downloading from URL: %s", url)

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Download the file
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download agent file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download agent file: HTTP %d", resp.StatusCode)
	}

	// Read the content
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read agent file content: %w", err)
	}

	contentStr := string(content)

	// Patch URLs to match the current version/ref
	patchedContent := patchAgentFileURLs(contentStr, ref)
	if patchedContent != contentStr && verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Patched URLs to use ref: %s", ref)))
	}

	commandsLog.Printf("Successfully downloaded agent file (%d bytes)", len(patchedContent))
	return patchedContent, nil
}

// patchAgentFileURLs patches URLs in the agent file to use the correct ref
func patchAgentFileURLs(content, ref string) string {
	// Pattern 1: Convert local paths to GitHub URLs
	// `.github/aw/file.md` -> `https://github.com/github/gh-aw/blob/{ref}/.github/aw/file.md`
	content = strings.ReplaceAll(content, "`.github/aw/", fmt.Sprintf("`https://github.com/github/gh-aw/blob/%s/.github/aw/", ref))

	// Pattern 2: Update existing GitHub URLs to use the correct ref
	// https://github.com/github/gh-aw/blob/main/ -> https://github.com/github/gh-aw/blob/{ref}/
	if ref != "main" {
		content = strings.ReplaceAll(content, "/blob/main/", fmt.Sprintf("/blob/%s/", ref))
	}

	return content
}

func isGHCLIAvailable() bool {
	cmd := exec.Command("gh", "--version")
	return cmd.Run() == nil
}

// normalizeWorkflowID extracts the workflow ID from a workflow identifier.
// It handles both workflow IDs ("my-workflow") and full paths (".github/workflows/my-workflow.md").
// Returns the workflow ID without .md extension.
func normalizeWorkflowID(workflowIDOrPath string) string {
	// Get the base filename if it's a path
	basename := filepath.Base(workflowIDOrPath)

	// Remove .md extension if present
	return strings.TrimSuffix(basename, ".md")
}

// resolveWorkflowFile resolves a file or workflow name to an actual file path
// Note: This function only looks for local workflows, not packages
func resolveWorkflowFile(fileOrWorkflowName string, verbose bool) (string, error) {
	return resolveWorkflowFileInDir(fileOrWorkflowName, verbose, "")
}

func resolveWorkflowFileInDir(fileOrWorkflowName string, verbose bool, workflowDir string) (string, error) {
	// First, try to use it as a direct file path
	if _, err := os.Stat(fileOrWorkflowName); err == nil {
		commandsLog.Printf("Found workflow file at path: %s", fileOrWorkflowName)
		console.LogVerbose(verbose, fmt.Sprintf("Found workflow file at path: %s", fileOrWorkflowName))
		// Return absolute path
		absPath, err := filepath.Abs(fileOrWorkflowName)
		if err != nil {
			return fileOrWorkflowName, nil // fallback to original path
		}
		return absPath, nil
	}

	// If it's not a direct file path, try to resolve it as a workflow name
	commandsLog.Printf("File not found at %s, trying to resolve as workflow name", fileOrWorkflowName)

	// Add .md extension if not present
	workflowPath := fileOrWorkflowName
	if !strings.HasSuffix(workflowPath, ".md") {
		workflowPath += ".md"
	}

	commandsLog.Printf("Looking for workflow file: %s", workflowPath)

	// Use provided directory or default
	workflowsDir := workflowDir
	if workflowsDir == "" {
		workflowsDir = getWorkflowsDir()
	}

	// Try to find the workflow in local sources only (not packages)
	_, path, err := readWorkflowFile(workflowPath, workflowsDir)
	if err != nil {
		suggestions := []string{
			fmt.Sprintf("Run '%s status' to see all available workflows", string(constants.CLIExtensionPrefix)),
			fmt.Sprintf("Create a new workflow with '%s new %s'", string(constants.CLIExtensionPrefix), fileOrWorkflowName),
			"Check for typos in the workflow name",
		}

		// Add fuzzy match suggestions
		similarNames := suggestWorkflowNames(fileOrWorkflowName)
		if len(similarNames) > 0 {
			suggestions = append([]string{fmt.Sprintf("Did you mean: %s?", strings.Join(similarNames, ", "))}, suggestions...)
		}

		return "", errors.New(console.FormatErrorWithSuggestions(
			fmt.Sprintf("workflow '%s' not found in local .github/workflows", fileOrWorkflowName),
			suggestions,
		))
	}

	commandsLog.Print("Found workflow in local .github/workflows")

	// Return absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path, nil // fallback to original path
	}
	return absPath, nil
}

// NewWorkflow creates a new workflow markdown file with template content
func NewWorkflow(workflowName string, verbose bool, force bool) error {
	commandsLog.Printf("Creating new workflow: name=%s, force=%v", workflowName, force)

	// Normalize the workflow name by removing .md extension if present
	// This ensures consistent behavior whether user provides "my-workflow" or "my-workflow.md"
	workflowName = strings.TrimSuffix(workflowName, ".md")
	commandsLog.Printf("Normalized workflow name: %s", workflowName)

	console.LogVerbose(verbose, fmt.Sprintf("Creating new workflow: %s", workflowName))

	// Get current working directory for .github/workflows
	workingDir, err := os.Getwd()
	if err != nil {
		commandsLog.Printf("Failed to get working directory: %v", err)
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Create .github/workflows directory if it doesn't exist
	githubWorkflowsDir := filepath.Join(workingDir, constants.GetWorkflowDir())
	commandsLog.Printf("Creating workflows directory: %s", githubWorkflowsDir)

	// Validate the directory path
	githubWorkflowsDir, err = fileutil.ValidateAbsolutePath(githubWorkflowsDir)
	if err != nil {
		commandsLog.Printf("Invalid workflows directory path: %v", err)
		return fmt.Errorf("invalid workflows directory path: %w", err)
	}

	if err := os.MkdirAll(githubWorkflowsDir, 0755); err != nil {
		commandsLog.Printf("Failed to create workflows directory: %v", err)
		return fmt.Errorf("failed to create .github/workflows directory: %w", err)
	}

	// Construct the destination file path
	destFile := filepath.Join(githubWorkflowsDir, workflowName+".md")
	commandsLog.Printf("Destination file: %s", destFile)

	// Validate the destination file path
	destFile, err = fileutil.ValidateAbsolutePath(destFile)
	if err != nil {
		commandsLog.Printf("Invalid destination file path: %v", err)
		return fmt.Errorf("invalid destination file path: %w", err)
	}

	// Check if destination file already exists
	if _, err := os.Stat(destFile); err == nil && !force {
		commandsLog.Printf("Workflow file already exists and force=false: %s", destFile)
		return fmt.Errorf("workflow file '%s' already exists. Use --force to overwrite", destFile)
	}

	// Create the template content
	template := createWorkflowTemplate(workflowName)

	// Write the template to file with restrictive permissions (owner-only)
	if err := os.WriteFile(destFile, []byte(template), 0600); err != nil {
		return fmt.Errorf("failed to write workflow file '%s': %w", destFile, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Created new workflow: %s", destFile)))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Edit the file to customize your workflow, then run '%s compile' to generate the GitHub Actions workflow", string(constants.CLIExtensionPrefix))))

	return nil
}

// createWorkflowTemplate generates a concise workflow template with essential options
func createWorkflowTemplate(workflowName string) string {
	return `---
# Trigger - when should this workflow run?
on:
  workflow_dispatch:  # Manual trigger

# Alternative triggers (uncomment to use):
# on:
#   issues:
#     types: [opened, reopened]
#   pull_request:
#     types: [opened, synchronize]
#   schedule: daily  # Fuzzy daily schedule (scattered execution time)
#   # schedule: weekly on monday  # Fuzzy weekly schedule

# Permissions - what can this workflow access?
permissions:
  contents: read
  issues: write
  pull-requests: write

# Outputs - what APIs and tools can the AI use?
safe-outputs:
  create-issue:          # Creates issues (default max: 1)
    max: 5               # Optional: specify maximum number
  # create-agent-session:   # Creates GitHub Copilot agent sessions (max: 1)
  # create-pull-request: # Creates exactly one pull request
  # add-comment:   # Adds comments (default max: 1)
  #   max: 2             # Optional: specify maximum number
  # add-labels:

---

# ` + workflowName + `

Describe what you want the AI to do when this workflow runs.

## Instructions

Replace this section with specific instructions for the AI. For example:

1. Read the issue description and comments
2. Analyze the request and gather relevant information
3. Provide a helpful response or take appropriate action

Be clear and specific about what the AI should accomplish.

## Notes

- Run ` + "`" + string(constants.CLIExtensionPrefix) + " compile`" + ` to generate the GitHub Actions workflow
- See https://github.github.com/gh-aw/ for complete configuration options and tools documentation
`
}
