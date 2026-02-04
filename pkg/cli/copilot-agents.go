package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotAgentsLog = logger.New("cli:copilot_agents")

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

// ensureCopilotInstructions ensures that .github/aw/github-agentic-workflows.md exists
func ensureCopilotInstructions(verbose bool, skipInstructions bool) error {
	copilotAgentsLog.Print("Checking Copilot instructions file")

	if skipInstructions {
		copilotAgentsLog.Print("Skipping instructions check: instructions disabled")
		return nil
	}

	// First, clean up the old file location if it exists
	if err := cleanupOldCopilotInstructions(verbose); err != nil {
		return err
	}

	gitRoot, err := findGitRoot()
	if err != nil {
		return err // Not in a git repository, skip
	}

	targetPath := filepath.Join(gitRoot, ".github", "aw", "github-agentic-workflows.md")

	// Check if the file exists
	if _, err := os.Stat(targetPath); err == nil {
		copilotAgentsLog.Printf("Copilot instructions file exists: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Copilot instructions file exists: %s", targetPath)))
		}
		return nil
	}

	// File doesn't exist - this is expected in external repositories
	copilotAgentsLog.Printf("Copilot instructions file not found: %s (expected in gh-aw repository)", targetPath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Copilot instructions file not found: %s", targetPath)))
	}
	return nil
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

// ensureAgenticWorkflowsDispatcher ensures that .github/agents/agentic-workflows.agent.md exists
func ensureAgenticWorkflowsDispatcher(verbose bool, skipInstructions bool) error {
	copilotAgentsLog.Print("Checking agentic workflows dispatcher agent")

	if skipInstructions {
		copilotAgentsLog.Print("Skipping agent check: instructions disabled")
		return nil
	}

	gitRoot, err := findGitRoot()
	if err != nil {
		return err // Not in a git repository, skip
	}

	targetPath := filepath.Join(gitRoot, ".github", "agents", "agentic-workflows.agent.md")

	// Check if the file exists
	if _, err := os.Stat(targetPath); err == nil {
		copilotAgentsLog.Printf("Dispatcher agent file exists: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Dispatcher agent file exists: %s", targetPath)))
		}
		return nil
	}

	// File doesn't exist - this is expected in external repositories
	copilotAgentsLog.Printf("Dispatcher agent file not found: %s (expected in gh-aw repository)", targetPath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Dispatcher agent file not found: %s", targetPath)))
	}
	return nil
}

// ensureCreateWorkflowPrompt ensures that .github/aw/create-agentic-workflow.md exists
func ensureCreateWorkflowPrompt(verbose bool, skipInstructions bool) error {
	return ensurePromptFileExists(".github/aw/create-agentic-workflow.md", "create workflow prompt", verbose, skipInstructions)
}

// ensureUpdateWorkflowPrompt ensures that .github/aw/update-agentic-workflow.md exists
func ensureUpdateWorkflowPrompt(verbose bool, skipInstructions bool) error {
	return ensurePromptFileExists(".github/aw/update-agentic-workflow.md", "update workflow prompt", verbose, skipInstructions)
}

// ensureCreateSharedAgenticWorkflowPrompt ensures that .github/aw/create-shared-agentic-workflow.md exists
func ensureCreateSharedAgenticWorkflowPrompt(verbose bool, skipInstructions bool) error {
	return ensurePromptFileExists(".github/aw/create-shared-agentic-workflow.md", "create shared workflow prompt", verbose, skipInstructions)
}

// ensureDebugWorkflowPrompt ensures that .github/aw/debug-agentic-workflow.md exists
func ensureDebugWorkflowPrompt(verbose bool, skipInstructions bool) error {
	return ensurePromptFileExists(".github/aw/debug-agentic-workflow.md", "debug workflow prompt", verbose, skipInstructions)
}

// ensureUpgradeAgenticWorkflowsPrompt ensures that .github/aw/upgrade-agentic-workflows.md exists
func ensureUpgradeAgenticWorkflowsPrompt(verbose bool, skipInstructions bool) error {
	return ensurePromptFileExists(".github/aw/upgrade-agentic-workflows.md", "upgrade workflows prompt", verbose, skipInstructions)
}

// ensureSerenaTool ensures that .github/aw/serena-tool.md exists
func ensureSerenaTool(verbose bool, skipInstructions bool) error {
	return ensurePromptFileExists(".github/aw/serena-tool.md", "Serena tool documentation", verbose, skipInstructions)
}

// ensurePromptFileExists checks if a prompt file exists
func ensurePromptFileExists(relativePath, fileType string, verbose bool, skipInstructions bool) error {
	copilotAgentsLog.Printf("Checking %s file: %s", fileType, relativePath)

	if skipInstructions {
		copilotAgentsLog.Print("Skipping file check: instructions disabled")
		return nil
	}

	gitRoot, err := findGitRoot()
	if err != nil {
		return err // Not in a git repository, skip
	}

	targetPath := filepath.Join(gitRoot, relativePath)

	// Check if the file exists
	if _, err := os.Stat(targetPath); err == nil {
		copilotAgentsLog.Printf("%s file exists: %s", fileType, targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("%s file exists: %s", fileType, targetPath)))
		}
		return nil
	}

	// File doesn't exist - this is expected in external repositories
	copilotAgentsLog.Printf("%s file not found: %s (expected in gh-aw repository)", fileType, targetPath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("%s file not found: %s", fileType, targetPath)))
	}
	return nil
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

// deleteOldTemplateFiles deletes old template files that are no longer bundled in the binary
func deleteOldTemplateFiles(verbose bool) error {
	gitRoot, err := findGitRoot()
	if err != nil {
		return nil // Not in a git repository, skip
	}

	// Template files that were removed from pkg/cli/templates/
	templateFiles := []string{
		"agentic-workflows.agent.md",
		"create-agentic-workflow.md",
		"create-shared-agentic-workflow.md",
		"debug-agentic-workflow.md",
		"github-agentic-workflows.md",
		"serena-tool.md",
		"update-agentic-workflow.md",
		"upgrade-agentic-workflows.md",
	}

	templatesDir := filepath.Join(gitRoot, "pkg", "cli", "templates")
	
	// Check if templates directory exists
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		// Directory doesn't exist, nothing to clean up
		return nil
	}

	removedCount := 0
	for _, file := range templateFiles {
		path := filepath.Join(templatesDir, file)
		if _, err := os.Stat(path); err == nil {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("failed to remove old template file %s: %w", file, err)
			}
			removedCount++
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Removed old template file: %s", path)))
			}
		}
	}

	// If directory is now empty, remove it
	if removedCount > 0 {
		entries, err := os.ReadDir(templatesDir)
		if err == nil && len(entries) == 0 {
			if err := os.Remove(templatesDir); err != nil {
				return fmt.Errorf("failed to remove empty templates directory: %w", err)
			}
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Removed empty templates directory: %s", templatesDir)))
			}
		}
	}

	return nil
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
