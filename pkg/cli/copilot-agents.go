package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/githubnext/gh-aw/pkg/console"
	"github.com/githubnext/gh-aw/pkg/logger"
)

var copilotAgentsLog = logger.New("cli:copilot_agents")

// ensureFileMatchesTemplate ensures a file in a subdirectory matches the expected template content
func ensureFileMatchesTemplate(subdir, fileName, templateContent, fileType string, verbose bool, skipInstructions bool) error {
	copilotAgentsLog.Printf("Ensuring file matches template: subdir=%s, file=%s, type=%s", subdir, fileName, fileType)

	if skipInstructions {
		copilotAgentsLog.Print("Skipping template update: instructions disabled")
		return nil
	}

	gitRoot, err := findGitRoot()
	if err != nil {
		return err // Not in a git repository, skip
	}

	targetDir := filepath.Join(gitRoot, subdir)
	targetPath := filepath.Join(targetDir, fileName)

	// Ensure the target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", subdir, err)
	}

	// Check if the file already exists and matches the template
	existingContent := ""
	if content, err := os.ReadFile(targetPath); err == nil {
		existingContent = string(content)
	}

	// Check if content matches our expected template
	expectedContent := strings.TrimSpace(templateContent)
	if strings.TrimSpace(existingContent) == expectedContent {
		copilotAgentsLog.Printf("File is up-to-date: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("%s is up-to-date: %s", fileType, targetPath)))
		}
		return nil
	}

	// Write the file with restrictive permissions (0600) to follow security best practices
	// Agent files and instructions may contain sensitive configuration
	if err := os.WriteFile(targetPath, []byte(templateContent), 0600); err != nil {
		copilotAgentsLog.Printf("Failed to write file: %s, error: %v", targetPath, err)
		return fmt.Errorf("failed to write %s: %w", fileType, err)
	}

	if existingContent == "" {
		copilotAgentsLog.Printf("Created %s: %s", fileType, targetPath)
	} else {
		copilotAgentsLog.Printf("Updated %s: %s", fileType, targetPath)
	}

	if verbose {
		if existingContent == "" {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Created %s: %s", fileType, targetPath)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Updated %s: %s", fileType, targetPath)))
		}
	}

	return nil
}

// ensureAgentFromTemplate ensures that an agent file exists and matches the embedded template
func ensureAgentFromTemplate(agentFileName, templateContent string, verbose bool, skipInstructions bool) error {
	return ensureFileMatchesTemplate(
		filepath.Join(".github", "agents"),
		agentFileName,
		templateContent,
		"agent",
		verbose,
		skipInstructions,
	)
}

// cleanupOldPromptFile removes an old prompt file from .github/prompts/ if it exists
func cleanupOldPromptFile(promptFileName string, verbose bool) error {
	gitRoot, err := findGitRoot()
	if err != nil {
		return nil // Not in a git repository, skip
	}

	oldPath := filepath.Join(gitRoot, ".github", "prompts", promptFileName)

	// Check if the old file exists and remove it
	if _, err := os.Stat(oldPath); err == nil {
		if err := os.Remove(oldPath); err != nil {
			return fmt.Errorf("failed to remove old prompt file: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Removed old prompt file: %s", oldPath)))
		}
	}

	return nil
}

// ensureCopilotInstructions ensures that .github/aw/github-agentic-workflows.md contains the copilot instructions
func ensureCopilotInstructions(verbose bool, skipInstructions bool) error {
	// First, clean up the old file location if it exists
	if err := cleanupOldCopilotInstructions(verbose); err != nil {
		return err
	}

	return ensureFileMatchesTemplate(
		filepath.Join(".github", "aw"),
		"github-agentic-workflows.md",
		copilotInstructionsTemplate,
		"copilot instructions",
		verbose,
		skipInstructions,
	)
}

// cleanupOldCopilotInstructions removes the old instructions file from .github/instructions/
func cleanupOldCopilotInstructions(verbose bool) error {
	gitRoot, err := findGitRoot()
	if err != nil {
		return nil // Not in a git repository, skip
	}

	oldPath := filepath.Join(gitRoot, ".github", "instructions", "github-agentic-workflows.instructions.md")

	// Check if the old file exists and remove it
	if _, err := os.Stat(oldPath); err == nil {
		if err := os.Remove(oldPath); err != nil {
			return fmt.Errorf("failed to remove old instructions file: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Removed old instructions file: %s", oldPath)))
		}
	}

	return nil
}

// ensureAgenticWorkflowsDispatcher ensures that .github/agents/agentic-workflows.agent.md contains the dispatcher agent
func ensureAgenticWorkflowsDispatcher(verbose bool, skipInstructions bool) error {
	return ensureAgentFromTemplate("agentic-workflows.agent.md", agenticWorkflowsDispatcherTemplate, verbose, skipInstructions)
}

// ensureCreateWorkflowPrompt ensures that .github/aw/create-agentic-workflow.md contains the new workflow creation prompt
func ensureCreateWorkflowPrompt(verbose bool, skipInstructions bool) error {
	return ensureFileMatchesTemplate(
		filepath.Join(".github", "aw"),
		"create-agentic-workflow.md",
		createWorkflowPromptTemplate,
		"create workflow prompt",
		verbose,
		skipInstructions,
	)
}

// ensureUpdateWorkflowPrompt ensures that .github/aw/update-agentic-workflow.md contains the workflow update prompt
func ensureUpdateWorkflowPrompt(verbose bool, skipInstructions bool) error {
	return ensureFileMatchesTemplate(
		filepath.Join(".github", "aw"),
		"update-agentic-workflow.md",
		updateWorkflowPromptTemplate,
		"update workflow prompt",
		verbose,
		skipInstructions,
	)
}

// ensureCreateSharedAgenticWorkflowPrompt ensures that .github/aw/create-shared-agentic-workflow.md contains the shared workflow creation prompt
func ensureCreateSharedAgenticWorkflowPrompt(verbose bool, skipInstructions bool) error {
	return ensureFileMatchesTemplate(
		filepath.Join(".github", "aw"),
		"create-shared-agentic-workflow.md",
		createSharedAgenticWorkflowPromptTemplate,
		"create shared workflow prompt",
		verbose,
		skipInstructions,
	)
}

// ensureDebugWorkflowPrompt ensures that .github/aw/debug-agentic-workflow.md contains the debug workflow prompt
func ensureDebugWorkflowPrompt(verbose bool, skipInstructions bool) error {
	return ensureFileMatchesTemplate(
		filepath.Join(".github", "aw"),
		"debug-agentic-workflow.md",
		debugWorkflowPromptTemplate,
		"debug workflow prompt",
		verbose,
		skipInstructions,
	)
}

// ensureUpgradeAgenticWorkflowsPrompt ensures that .github/aw/upgrade-agentic-workflows.md contains the upgrade workflows prompt
func ensureUpgradeAgenticWorkflowsPrompt(verbose bool, skipInstructions bool) error {
	return ensureFileMatchesTemplate(
		filepath.Join(".github", "aw"),
		"upgrade-agentic-workflows.md",
		upgradeAgenticWorkflowsPromptTemplate,
		"upgrade workflows prompt",
		verbose,
		skipInstructions,
	)
}

// deleteSetupAgenticWorkflowsAgent deletes the setup-agentic-workflows.agent.md file if it exists
func deleteSetupAgenticWorkflowsAgent(verbose bool) error {
	gitRoot, err := findGitRoot()
	if err != nil {
		return nil // Not in a git repository, skip
	}

	agentPath := filepath.Join(gitRoot, ".github", "agents", "setup-agentic-workflows.agent.md")

	// Check if the file exists and remove it
	if _, err := os.Stat(agentPath); err == nil {
		if err := os.Remove(agentPath); err != nil {
			return fmt.Errorf("failed to remove setup-agentic-workflows agent: %w", err)
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "Removed setup-agentic-workflows agent: %s\n", agentPath)
		}
	}

	// Also clean up the old prompt file if it exists
	return cleanupOldPromptFile("setup-agentic-workflows.prompt.md", verbose)
}

// deleteOldAgentFiles deletes old .agent.md files that have been moved to .github/aw/
func deleteOldAgentFiles(verbose bool) error {
	gitRoot, err := findGitRoot()
	if err != nil {
		return nil // Not in a git repository, skip
	}

	// Map of subdirectory to list of files to delete
	filesToDelete := map[string][]string{
		"agents": {
			"create-agentic-workflow.agent.md",
			"debug-agentic-workflow.agent.md",
			"create-shared-agentic-workflow.agent.md",
			"create-shared-agentic-workflow.md",
			"create-agentic-workflow.md",
			"setup-agentic-workflows.md",
			"update-agentic-workflows.md",
			"upgrade-agentic-workflows.md",
		},
		"aw": {
			"upgrade-agentic-workflow.md", // singular form (typo/duplicate)
		},
	}

	for subdir, files := range filesToDelete {
		for _, file := range files {
			path := filepath.Join(gitRoot, ".github", subdir, file)
			if _, err := os.Stat(path); err == nil {
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("failed to remove old %s file %s: %w", subdir, file, err)
				}
				if verbose {
					fmt.Fprintf(os.Stderr, "Removed old %s file: %s\n", subdir, path)
				}
			}
		}
	}

	return nil
}
