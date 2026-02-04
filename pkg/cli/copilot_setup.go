package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
)

var copilotSetupLog = logger.New("cli:copilot_setup")

// generateCopilotSetupStepsYAML generates the copilot-setup-steps.yml content based on action mode
func generateCopilotSetupStepsYAML(actionMode workflow.ActionMode, version string) string {
	// Determine the action reference - use version tag in release mode, @main in dev mode
	actionRef := "@main"
	if actionMode.IsRelease() && version != "" && version != "dev" {
		actionRef = "@" + version
	}

	if actionMode.IsRelease() {
		// Use the actions/setup-cli action in release mode
		return fmt.Sprintf(`name: "Copilot Setup Steps"

# This workflow configures the environment for GitHub Copilot Agent with gh-aw MCP server
on:
  workflow_dispatch:
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml

jobs:
  # The job MUST be called 'copilot-setup-steps' to be recognized by GitHub Copilot Agent
  copilot-setup-steps:
    runs-on: ubuntu-latest

    # Set minimal permissions for setup steps
    # Copilot Agent receives its own token with appropriate permissions
    permissions:
      contents: read

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4
      - name: Install gh-aw extension
        uses: github/gh-aw/actions/setup-cli%s
        with:
          version: %s
`, actionRef, version)
	}

	// Default (dev/script mode): use curl to download install script
	return `name: "Copilot Setup Steps"

# This workflow configures the environment for GitHub Copilot Agent with gh-aw MCP server
on:
  workflow_dispatch:
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml

jobs:
  # The job MUST be called 'copilot-setup-steps' to be recognized by GitHub Copilot Agent
  copilot-setup-steps:
    runs-on: ubuntu-latest

    # Set minimal permissions for setup steps
    # Copilot Agent receives its own token with appropriate permissions
    permissions:
      contents: read

    steps:
      - name: Install gh-aw extension
        run: |
          curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash
`
}

const copilotSetupStepsYAML = `name: "Copilot Setup Steps"

# This workflow configures the environment for GitHub Copilot Agent with gh-aw MCP server
on:
  workflow_dispatch:
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml

jobs:
  # The job MUST be called 'copilot-setup-steps' to be recognized by GitHub Copilot Agent
  copilot-setup-steps:
    runs-on: ubuntu-latest

    # Set minimal permissions for setup steps
    # Copilot Agent receives its own token with appropriate permissions
    permissions:
      contents: read

    steps:
      - name: Install gh-aw extension
        run: |
          curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash
`

// CopilotWorkflowStep represents a GitHub Actions workflow step for Copilot setup scaffolding
type CopilotWorkflowStep struct {
	Name string         `yaml:"name,omitempty"`
	Uses string         `yaml:"uses,omitempty"`
	Run  string         `yaml:"run,omitempty"`
	With map[string]any `yaml:"with,omitempty"`
	Env  map[string]any `yaml:"env,omitempty"`
}

// WorkflowJob represents a GitHub Actions workflow job
type WorkflowJob struct {
	RunsOn      any                   `yaml:"runs-on,omitempty"`
	Permissions map[string]any        `yaml:"permissions,omitempty"`
	Steps       []CopilotWorkflowStep `yaml:"steps,omitempty"`
}

// Workflow represents a GitHub Actions workflow file
type Workflow struct {
	Name string                 `yaml:"name,omitempty"`
	On   any                    `yaml:"on,omitempty"`
	Jobs map[string]WorkflowJob `yaml:"jobs,omitempty"`
}

// ensureCopilotSetupSteps creates or updates .github/workflows/copilot-setup-steps.yml
func ensureCopilotSetupSteps(verbose bool, actionMode workflow.ActionMode, version string) error {
	return ensureCopilotSetupStepsWithUpgrade(verbose, actionMode, version, false)
}

// upgradeCopilotSetupSteps upgrades the version in existing copilot-setup-steps.yml
func upgradeCopilotSetupSteps(verbose bool, actionMode workflow.ActionMode, version string) error {
	return ensureCopilotSetupStepsWithUpgrade(verbose, actionMode, version, true)
}

// ensureCopilotSetupStepsWithUpgrade creates .github/workflows/copilot-setup-steps.yml
// If the file already exists, it renders console instructions instead of editing
// When upgradeVersion is true and called from upgrade command, this is a special case
func ensureCopilotSetupStepsWithUpgrade(verbose bool, actionMode workflow.ActionMode, version string, upgradeVersion bool) error {
	copilotSetupLog.Printf("Creating copilot-setup-steps.yml with action mode: %s, version: %s, upgradeVersion: %v", actionMode, version, upgradeVersion)

	// Create .github/workflows directory if it doesn't exist
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		return fmt.Errorf("failed to create workflows directory: %w", err)
	}
	copilotSetupLog.Printf("Ensured directory exists: %s", workflowsDir)

	// Write copilot-setup-steps.yml
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")

	// Check if file already exists
	if _, err := os.Stat(setupStepsPath); err == nil {
		copilotSetupLog.Printf("File already exists: %s", setupStepsPath)

		// Read existing file to check if extension install step exists
		content, err := os.ReadFile(setupStepsPath)
		if err != nil {
			return fmt.Errorf("failed to read existing copilot-setup-steps.yml: %w", err)
		}

		// Check if the extension install step is already present (check for both modes)
		contentStr := string(content)
		hasLegacyInstall := strings.Contains(contentStr, "install-gh-aw.sh") ||
			(strings.Contains(contentStr, "Install gh-aw extension") && strings.Contains(contentStr, "curl -fsSL"))
		hasActionInstall := strings.Contains(contentStr, "actions/setup-cli")

		// If we have an install step and upgradeVersion is true, this is from upgrade command
		// In this case, we still update the file for backward compatibility
		if (hasLegacyInstall || hasActionInstall) && upgradeVersion {
			copilotSetupLog.Print("Extension install step exists, attempting version upgrade (upgrade command)")

			// Parse existing workflow
			var workflow Workflow
			if err := yaml.Unmarshal(content, &workflow); err != nil {
				return fmt.Errorf("failed to parse existing copilot-setup-steps.yml: %w", err)
			}

			// Upgrade the version in existing steps
			upgraded, err := upgradeSetupCliVersion(&workflow, actionMode, version)
			if err != nil {
				return fmt.Errorf("failed to upgrade setup-cli version: %w", err)
			}

			if !upgraded {
				copilotSetupLog.Print("No version upgrade needed")
				if verbose {
					fmt.Fprintf(os.Stderr, "No version upgrade needed for %s\n", setupStepsPath)
				}
				return nil
			}

			// Marshal back to YAML
			updatedContent, err := yaml.Marshal(&workflow)
			if err != nil {
				return fmt.Errorf("failed to marshal updated workflow: %w", err)
			}

			if err := os.WriteFile(setupStepsPath, updatedContent, 0600); err != nil {
				return fmt.Errorf("failed to update copilot-setup-steps.yml: %w", err)
			}
			copilotSetupLog.Printf("Upgraded version in file: %s", setupStepsPath)

			if verbose {
				fmt.Fprintf(os.Stderr, "Updated %s with new version %s\n", setupStepsPath, version)
			}
			return nil
		}

		// File exists - render instructions instead of editing
		if hasLegacyInstall || hasActionInstall {
			copilotSetupLog.Print("Extension install step already exists, file is up to date")
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping %s (already has gh-aw extension install step)\n", setupStepsPath)
			}
			return nil
		}

		// File exists but needs update - render instructions
		copilotSetupLog.Print("File exists without install step, rendering update instructions instead of editing")
		renderCopilotSetupUpdateInstructions(setupStepsPath, actionMode, version)
		return nil
	}

	// File doesn't exist - create it
	if err := os.WriteFile(setupStepsPath, []byte(generateCopilotSetupStepsYAML(actionMode, version)), 0600); err != nil {
		return fmt.Errorf("failed to write copilot-setup-steps.yml: %w", err)
	}
	copilotSetupLog.Printf("Created file: %s", setupStepsPath)

	return nil
}

// renderCopilotSetupUpdateInstructions renders console instructions for updating copilot-setup-steps.yml
func renderCopilotSetupUpdateInstructions(filePath string, actionMode workflow.ActionMode, version string) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s %s\n",
		"ℹ",
		fmt.Sprintf("Existing file detected: %s", filePath))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "To enable GitHub Copilot Agent integration, please add the following steps")
	fmt.Fprintln(os.Stderr, "to the 'copilot-setup-steps' job in your .github/workflows/copilot-setup-steps.yml file:")
	fmt.Fprintln(os.Stderr)

	// Determine the action reference
	actionRef := "@main"
	if actionMode.IsRelease() && version != "" && version != "dev" {
		actionRef = "@" + version
	}

	if actionMode.IsRelease() {
		fmt.Fprintln(os.Stderr, "      - name: Checkout repository")
		fmt.Fprintln(os.Stderr, "        uses: actions/checkout@v4")
		fmt.Fprintf(os.Stderr, "      - name: Install gh-aw extension\n")
		fmt.Fprintf(os.Stderr, "        uses: github/gh-aw/actions/setup-cli%s\n", actionRef)
		fmt.Fprintln(os.Stderr, "        with:")
		fmt.Fprintf(os.Stderr, "          version: %s\n", version)
	} else {
		fmt.Fprintln(os.Stderr, "      - name: Install gh-aw extension")
		fmt.Fprintln(os.Stderr, "        run: |")
		fmt.Fprintln(os.Stderr, "          curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash")
	}
	fmt.Fprintln(os.Stderr)
}

// upgradeSetupCliVersion upgrades the version in existing actions/setup-cli steps
// Returns true if any upgrades were made, false otherwise
func upgradeSetupCliVersion(workflow *Workflow, actionMode workflow.ActionMode, version string) (bool, error) {
	copilotSetupLog.Printf("Upgrading setup-cli version to %s with action mode: %s", version, actionMode)

	// Find the copilot-setup-steps job
	job, exists := workflow.Jobs["copilot-setup-steps"]
	if !exists {
		return false, fmt.Errorf("copilot-setup-steps job not found in workflow")
	}

	upgraded := false
	actionRef := "@main"
	if actionMode.IsRelease() && version != "" && version != "dev" {
		actionRef = "@" + version
	}

	// Iterate through steps and update any actions/setup-cli steps
	for i := range job.Steps {
		step := &job.Steps[i]

		// Check if this is a setup-cli action step
		if step.Uses != "" && strings.Contains(step.Uses, "actions/setup-cli") {
			// Update the action reference
			oldUses := step.Uses
			if actionMode.IsRelease() {
				// Update to the new version tag
				newUses := fmt.Sprintf("github/gh-aw/actions/setup-cli%s", actionRef)
				step.Uses = newUses

				// Update the with.version parameter
				if step.With == nil {
					step.With = make(map[string]any)
				}
				step.With["version"] = version

				copilotSetupLog.Printf("Upgraded setup-cli action from %s to %s", oldUses, newUses)
				upgraded = true
			}
		}
	}

	if upgraded {
		workflow.Jobs["copilot-setup-steps"] = job
	}

	return upgraded, nil
}

// injectExtensionInstallStep injects the gh-aw extension install and verification steps into an existing workflow
func injectExtensionInstallStep(workflow *Workflow, actionMode workflow.ActionMode, version string) error {
	var installStep, checkoutStep CopilotWorkflowStep

	// Determine the action reference - use version tag in release mode, @main in dev mode
	actionRef := "@main"
	if actionMode.IsRelease() && version != "" && version != "dev" {
		actionRef = "@" + version
	}

	if actionMode.IsRelease() {
		// In release mode, use the actions/setup-cli action
		checkoutStep = CopilotWorkflowStep{
			Name: "Checkout repository",
			Uses: "actions/checkout@v4",
		}
		installStep = CopilotWorkflowStep{
			Name: "Install gh-aw extension",
			Uses: fmt.Sprintf("github/gh-aw/actions/setup-cli%s", actionRef),
			With: map[string]any{
				"version": version,
			},
		}
	} else {
		// In dev/script mode, use curl to download install script
		installStep = CopilotWorkflowStep{
			Name: "Install gh-aw extension",
			Run:  "curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash",
		}
	}

	// Find the copilot-setup-steps job
	job, exists := workflow.Jobs["copilot-setup-steps"]
	if !exists {
		return fmt.Errorf("copilot-setup-steps job not found in workflow")
	}

	// Insert the extension install step at the beginning
	insertPosition := 0

	// Prepare steps to insert based on mode
	var stepsToInsert []CopilotWorkflowStep
	if actionMode.IsRelease() {
		stepsToInsert = []CopilotWorkflowStep{checkoutStep, installStep}
	} else {
		stepsToInsert = []CopilotWorkflowStep{installStep}
	}

	// Insert steps at the determined position
	newSteps := make([]CopilotWorkflowStep, 0, len(job.Steps)+len(stepsToInsert))
	newSteps = append(newSteps, job.Steps[:insertPosition]...)
	newSteps = append(newSteps, stepsToInsert...)
	newSteps = append(newSteps, job.Steps[insertPosition:]...)

	job.Steps = newSteps
	workflow.Jobs["copilot-setup-steps"] = job

	return nil
}
