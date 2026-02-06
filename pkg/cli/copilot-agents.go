package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotAgentsLog = logger.New("cli:copilot_agents")

// ensureAgenticWorkflowsDispatcher ensures that .github/agents/agentic-workflows.agent.md contains the dispatcher agent
func ensureAgenticWorkflowsDispatcher(verbose bool, skipInstructions bool) error {
	copilotAgentsLog.Print("Ensuring agentic workflows dispatcher agent")

	if skipInstructions {
		copilotAgentsLog.Print("Skipping agent creation: instructions disabled")
		return nil
	}

	gitRoot, err := findGitRoot()
	if err != nil {
		return err // Not in a git repository, skip
	}

	targetDir := filepath.Join(gitRoot, ".github", "agents")
	targetPath := filepath.Join(targetDir, "agentic-workflows.agent.md")

	// Ensure the target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create .github/agents directory: %w", err)
	}

	// Download the agent file from GitHub
	agentContent, err := downloadAgentFileFromGitHub(verbose)
	if err != nil {
		copilotAgentsLog.Printf("Failed to download agent file from GitHub: %v", err)
		return fmt.Errorf("failed to download agent file from GitHub: %w", err)
	}

	// Check if the file already exists and matches the downloaded content
	existingContent := ""
	if content, err := os.ReadFile(targetPath); err == nil {
		existingContent = string(content)
	}

	// Check if content matches the downloaded template
	expectedContent := strings.TrimSpace(agentContent)
	if strings.TrimSpace(existingContent) == expectedContent {
		copilotAgentsLog.Printf("Dispatcher agent is up-to-date: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Dispatcher agent is up-to-date: %s", targetPath)))
		}
		return nil
	}

	// Write the file with restrictive permissions (0600) to follow security best practices
	// Agent files may contain sensitive configuration
	if err := os.WriteFile(targetPath, []byte(agentContent), 0600); err != nil {
		copilotAgentsLog.Printf("Failed to write dispatcher agent: %s, error: %v", targetPath, err)
		return fmt.Errorf("failed to write dispatcher agent: %w", err)
	}

	if existingContent == "" {
		copilotAgentsLog.Printf("Created dispatcher agent: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Created dispatcher agent: %s", targetPath)))
		}
	} else {
		copilotAgentsLog.Printf("Updated dispatcher agent: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Updated dispatcher agent: %s", targetPath)))
		}
	}

	return nil
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

	// All template files that were previously bundled
	// Now that we download the agent file on demand, all files should be removed
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

	// If any files were removed, try to remove the directory if it's now empty
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
