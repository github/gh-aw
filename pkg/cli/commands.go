package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var commandsLog = logger.New("cli:commands")

// CreateWorkflowMarkdownFile creates a new workflow markdown file with template content
func CreateWorkflowMarkdownFile(workflowName string, verbose bool, force bool, engine string) error {
	commandsLog.Printf("Creating new workflow: name=%s, force=%v, engine=%s", workflowName, force, engine)

	// Normalize the workflow name by removing .md extension if present
	// This ensures consistent behavior whether user provides "my-workflow" or "my-workflow.md"
	workflowName = strings.TrimSuffix(workflowName, ".md")
	commandsLog.Printf("Normalized workflow name: %s", workflowName)

	console.LogVerbose(verbose, "Creating new workflow: "+workflowName)

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

	if err := os.MkdirAll(githubWorkflowsDir, constants.DirPermPublic); err != nil {
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
	if fileutil.FileExists(destFile) && !force {
		commandsLog.Printf("Workflow file already exists and force=false: %s", destFile)
		return fmt.Errorf("workflow file '%s' already exists. Use --force to overwrite", destFile)
	}

	// Create the template content
	template := createWorkflowTemplate(workflowName, engine)

	// Write the template to file with restrictive permissions (owner-only)
	if err := os.WriteFile(destFile, []byte(template), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write workflow file '%s': %w", destFile, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created new workflow: "+destFile))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Edit the file to customize your workflow, then run '%s compile' to generate the GitHub Actions workflow", string(constants.CLIExtensionPrefix))))

	return nil
}

// createWorkflowTemplate generates a concise workflow template with essential options
func createWorkflowTemplate(workflowName string, engine string) string {
	engineLine := ""
	if engine != "" {
		engineLine = "\n# AI engine to use for this workflow\nengine: " + engine + "\n"
	}
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
# Write operations (creating issues, PRs, comments, etc.) are handled
# automatically by the safe-outputs job with its own scoped permissions.
permissions:
  contents: read
  issues: read
  pull-requests: read
` + engineLine + `
# Tools - GitHub API access via toolsets (context, repos, issues, pull_requests)
# tools:
#   github:
#     toolsets: [default]

# Network access
network: defaults

` + buildSafeOutputsSection() + `
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

// buildSafeOutputsSection generates the safe-outputs section of the workflow template.
// It uses the JSON schema to derive the valid safe output type names, ensuring the
// template is always consistent with what the schema accepts.
func buildSafeOutputsSection() string {
	keys, err := parser.GetSafeOutputTypeKeys()
	if err != nil {
		commandsLog.Printf("Failed to get safe output type keys from schema: %v", err)
		// Fallback to a minimal static section
		return "# Outputs - what APIs and tools can the AI use?\nsafe-outputs:\n  create-issue:  # Creates issues (default max: 1)\n"
	}

	var sb strings.Builder
	sb.WriteString("# Outputs - what APIs and tools can the AI use?\n")
	sb.WriteString("safe-outputs:\n")
	sb.WriteString("  create-issue:          # Creates issues (default max: 1)\n")
	sb.WriteString("    max: 5               # Optional: specify maximum number\n")
	for _, key := range keys {
		if key == "create-issue" {
			continue
		}
		sb.WriteString("  # " + key + ":\n")
	}
	return sb.String()
}
