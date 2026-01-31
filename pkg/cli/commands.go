package cli

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/githubnext/gh-aw/pkg/console"
	"github.com/githubnext/gh-aw/pkg/constants"
	"github.com/githubnext/gh-aw/pkg/logger"
	"github.com/githubnext/gh-aw/pkg/workflow"
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

//go:embed templates/github-agentic-workflows.md
var copilotInstructionsTemplate string

//go:embed templates/agentic-workflows.agent.md
var agenticWorkflowsDispatcherTemplate string

//go:embed templates/create-agentic-workflow.md
var createWorkflowPromptTemplate string

//go:embed templates/update-agentic-workflow.md
var updateWorkflowPromptTemplate string

//go:embed templates/create-shared-agentic-workflow.md
var createSharedAgenticWorkflowPromptTemplate string

//go:embed templates/debug-agentic-workflow.md
var debugWorkflowPromptTemplate string

//go:embed templates/upgrade-agentic-workflows.md
var upgradeAgenticWorkflowsPromptTemplate string

//go:embed templates/serena-tool.md
var serenaToolTemplate string

// SetVersionInfo sets the version information for the CLI and workflow package
func SetVersionInfo(v string) {
	version = v
	workflow.SetDefaultVersion(v) // Keep workflow package in sync
}

// GetVersion returns the current version
func GetVersion() string {
	return version
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

	if err := os.MkdirAll(githubWorkflowsDir, 0755); err != nil {
		commandsLog.Printf("Failed to create workflows directory: %v", err)
		return fmt.Errorf("failed to create .github/workflows directory: %w", err)
	}

	// Construct the destination file path
	destFile := filepath.Join(githubWorkflowsDir, workflowName+".md")
	commandsLog.Printf("Destination file: %s", destFile)

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
- See https://githubnext.github.io/gh-aw/ for complete configuration options and tools documentation
`
}
