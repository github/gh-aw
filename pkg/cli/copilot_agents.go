package cli

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotAgentsLog = logger.New("cli:copilot_agents")

const agenticWorkflowsSkillFileListPlaceholder = "{{AW_FILE_LIST}}"
const agenticWorkflowsOTELSkillParagraph = "When the task involves OTEL, OTLP, traces, observability backends, or telemetry-driven analysis, also read and follow `skills/otel-queries/SKILL.md` after loading the matching workflow prompt or skill."
const ghAWMarkdownFilesAPIURL = "https://api.github.com/repos/github/gh-aw/contents/.github/aw?ref=main"
const agenticWorkflowsSkillDirDescription = ".github/skills/agentic-workflows directory"
const agenticWorkflowsAgentDirDescription = ".github/agents directory"

//go:embed data/agentic_workflows_agent.md
var agenticWorkflowsAgentTemplate string

//go:embed data/agentic_workflows_skill.md
var agenticWorkflowsSkillTemplate string

//go:embed data/agentic_workflows_fallback_aw_files.json
var agenticWorkflowsFallbackAWFiles string

var listAgenticWorkflowsMarkdownFiles = fetchAgenticWorkflowsMarkdownFiles

// ensureAgenticWorkflowsDispatcher ensures that .github/skills/agentic-workflows/SKILL.md
// exists and contains the routing instructions loaded by the Agentic Workflows agent.
func ensureAgenticWorkflowsDispatcher(verbose bool, skipInstructions bool, write bool) error {
	copilotAgentsLog.Print("Ensuring agentic workflows dispatcher skill")

	if skipInstructions {
		copilotAgentsLog.Print("Skipping skill creation: instructions disabled")
		return nil
	}

	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return err // Not in a git repository, skip
	}

	targetDir := filepath.Join(gitRoot, ".github", "skills", "agentic-workflows")
	targetPath := filepath.Join(targetDir, "SKILL.md")

	skillContent, err := buildAgenticWorkflowsSkillContent()
	if err != nil {
		copilotAgentsLog.Printf("Failed to build dispatcher skill: %v", err)
		return fmt.Errorf("failed to build dispatcher skill: %w", err)
	}

	existingContent, fileExists, err := readExistingRepositoryInstructionFile(targetPath, "dispatcher skill")
	if err != nil {
		return err
	}

	// Check if content matches the downloaded template
	expectedContent := strings.TrimSpace(skillContent)
	if strings.TrimSpace(existingContent) == expectedContent {
		copilotAgentsLog.Printf("Dispatcher skill is up-to-date: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Dispatcher skill is up-to-date: "+targetPath))
		}
		return nil
	}

	if !write {
		action := "update"
		if !fileExists {
			action = "create"
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Would %s dispatcher skill: %s", action, targetPath)))
		return nil
	}

	if err := writeGeneratedRepositoryInstructionFile(targetPath, []byte(skillContent), write, agenticWorkflowsSkillDirDescription, "dispatcher skill"); err != nil {
		copilotAgentsLog.Printf("Failed to write dispatcher skill: %s, error: %v", targetPath, err)
		return fmt.Errorf("failed to write dispatcher skill: %w", err)
	}

	if !fileExists {
		copilotAgentsLog.Printf("Created dispatcher skill: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created dispatcher skill: "+targetPath))
		}
	} else {
		copilotAgentsLog.Printf("Updated dispatcher skill: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated dispatcher skill: "+targetPath))
		}
	}

	return nil
}

// ensureAgenticWorkflowsAgent ensures that .github/agents/agentic-workflows.md contains the custom agent.
func ensureAgenticWorkflowsAgent(verbose bool, write bool) error {
	copilotAgentsLog.Print("Ensuring agentic workflows custom agent")

	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return err
	}

	targetDir := filepath.Join(gitRoot, ".github", "agents")
	targetPath := filepath.Join(targetDir, "agentic-workflows.md")

	existingContent, fileExists, err := readExistingRepositoryInstructionFile(targetPath, "Agentic Workflows custom agent")
	if err != nil {
		return err
	}

	agenticWorkflowsAgentContent, err := buildAgenticWorkflowsAgentContent(gitRoot)
	if err != nil {
		return err
	}

	expectedContent := strings.TrimSpace(agenticWorkflowsAgentContent)
	if strings.TrimSpace(existingContent) == expectedContent {
		copilotAgentsLog.Printf("Agentic Workflows custom agent is up-to-date: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Agentic Workflows custom agent is up-to-date: "+targetPath))
		}
		return nil
	}

	if !write {
		action := "update"
		if !fileExists {
			action = "create"
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Would %s Agentic Workflows custom agent: %s", action, targetPath)))
		return nil
	}

	if err := writeGeneratedRepositoryInstructionFile(targetPath, []byte(agenticWorkflowsAgentContent), write, agenticWorkflowsAgentDirDescription, "Agentic Workflows custom agent"); err != nil {
		return fmt.Errorf("failed to write Agentic Workflows custom agent: %w", err)
	}

	if !fileExists {
		copilotAgentsLog.Printf("Created Agentic Workflows custom agent: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created Agentic Workflows custom agent: "+targetPath))
		}
	} else {
		copilotAgentsLog.Printf("Updated Agentic Workflows custom agent: %s", targetPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated Agentic Workflows custom agent: "+targetPath))
		}
	}

	return nil
}

func writeGeneratedRepositoryInstructionFile(targetPath string, content []byte, write bool, parentDirDescription string, artifactDescription string) error {
	if !write {
		return fmt.Errorf("internal error: refusing to write %s without --write: %s", artifactDescription, targetPath)
	}

	if err := fileutil.EnsureParentDir(targetPath, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create %s: %w", parentDirDescription, err)
	}

	// Repository instruction files are committed artifacts, so keep them world-readable.
	if err := os.WriteFile(targetPath, content, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write %s: %w", artifactDescription, err)
	}

	return nil
}

func readExistingRepositoryInstructionFile(targetPath string, artifactDescription string) (string, bool, error) {
	content, err := os.ReadFile(targetPath)
	if err == nil {
		return string(content), true, nil
	}
	if os.IsNotExist(err) {
		return "", false, nil
	}
	return "", false, fmt.Errorf("failed to read existing %s: %w", artifactDescription, err)
}

func buildAgenticWorkflowsAgentContent(gitRoot string) (string, error) {
	return agenticWorkflowsAgentTemplate, nil
}

func buildAgenticWorkflowsSkillContent() (string, error) {
	awFiles, err := listAgenticWorkflowsMarkdownFiles(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch .github/aw markdown file list from github/gh-aw: %v. Falling back to embedded list.", err)))
		awFiles = embeddedFallbackAWMarkdownFiles()
	}
	sort.Strings(awFiles)
	if len(awFiles) == 0 {
		return "", errors.New("no .github/aw markdown files available from remote or embedded fallback")
	}

	var fileList strings.Builder
	for _, file := range awFiles {
		fmt.Fprintf(&fileList, "- `.github/aw/%s`\n", file)
	}

	if !strings.Contains(agenticWorkflowsSkillTemplate, agenticWorkflowsSkillFileListPlaceholder) {
		return "", fmt.Errorf("agentic workflows skill template is missing %s placeholder", agenticWorkflowsSkillFileListPlaceholder)
	}

	content := strings.Replace(agenticWorkflowsSkillTemplate, agenticWorkflowsSkillFileListPlaceholder, fileList.String(), 1)
	if !strings.Contains(content, agenticWorkflowsOTELSkillParagraph) {
		content = strings.TrimRight(content, "\n") + "\n\n" + agenticWorkflowsOTELSkillParagraph + "\n"
	}
	return content, nil
}

type gitHubRepositoryContentEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func fetchAgenticWorkflowsMarkdownFiles(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ghAWMarkdownFilesAPIURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build github API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gh-aw")

	client := &http.Client{Timeout: constants.DefaultHTTPClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, fmt.Errorf("github API request timed out after %s: %w", constants.DefaultHTTPClientTimeout, err)
		}
		return nil, fmt.Errorf("github API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %s", resp.Status)
	}

	var entries []gitHubRepositoryContentEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("failed to decode github API response: %w", err)
	}

	awFiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type != "file" || !strings.HasSuffix(entry.Name, ".md") {
			continue
		}
		awFiles = append(awFiles, entry.Name)
	}

	if len(awFiles) == 0 {
		return nil, errors.New("github API returned no markdown files")
	}

	sort.Strings(awFiles)
	return awFiles, nil
}

func embeddedFallbackAWMarkdownFiles() []string {
	var awFiles []string
	if err := json.Unmarshal([]byte(agenticWorkflowsFallbackAWFiles), &awFiles); err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse embedded .github/aw fallback markdown file list: %v", err)))
		return nil
	}
	sort.Strings(awFiles)
	return awFiles
}

// cleanupOldPromptFile removes an old prompt file from .github/prompts/ if it exists
func cleanupOldPromptFile(promptFileName string, verbose bool) error {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return nil // Not in a git repository, skip
	}

	oldPath := filepath.Join(gitRoot, ".github", "prompts", promptFileName)

	// Check if the old file exists and remove it
	if fileutil.FileExists(oldPath) {
		if err := os.Remove(oldPath); err != nil {
			return fmt.Errorf("failed to remove old prompt file: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Removed old prompt file: "+oldPath))
		}
	}

	return nil
}

// deleteAgenticWorkflowDesignerSkillDir removes the legacy
// .github/skills/agentic-workflow-designer/ directory if it exists.
// The designer instructions are now bundled inside the agentic-workflows skill
// at .github/aw/designer.md (loaded on demand from
// github/gh-aw) and no longer need to be written to user repositories.
func deleteAgenticWorkflowDesignerSkillDir(verbose bool) error {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return nil // Not in a git repository, skip
	}

	designerDir := filepath.Join(gitRoot, ".github", "skills", "agentic-workflow-designer")
	if _, err := os.Stat(designerDir); os.IsNotExist(err) {
		return nil // Already removed, nothing to do
	}

	if err := os.RemoveAll(designerDir); err != nil {
		return fmt.Errorf("failed to remove legacy agentic-workflow-designer skill directory: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Removed legacy skill directory: "+designerDir))
	}
	return nil
}

// deleteSetupAgenticWorkflowsAgent deletes the setup-agentic-workflows.agent.md file if it exists
func deleteSetupAgenticWorkflowsAgent(verbose bool) error {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return nil // Not in a git repository, skip
	}

	agentPath := filepath.Join(gitRoot, ".github", "agents", "setup-agentic-workflows.agent.md")

	// Check if the file exists and remove it
	if fileutil.FileExists(agentPath) {
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
	gitRoot, err := gitutil.FindGitRoot()
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
		if fileutil.FileExists(path) {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("failed to remove old template file %s: %w", file, err)
			}
			removedCount++
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Removed old template file: "+path))
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
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Removed empty templates directory: "+templatesDir))
			}
		}
	}

	return nil
}

// deleteLegacyAgentFiles deletes legacy workflow-specific agent files from .github/agents/.
func deleteLegacyAgentFiles(verbose bool) error {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return nil // Not in a git repository, skip
	}

	// Map of subdirectory to list of files to delete
	filesToDelete := map[string][]string{
		"agents": {
			"agentic-workflows.agent.md",
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
			if fileutil.FileExists(path) {
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
