package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

// TrialSecretTracker tracks which secrets were added during a trial for cleanup
type TrialSecretTracker struct {
	RepoSlug     string          `json:"repo_slug"`
	AddedSecrets map[string]bool `json:"added_secrets"` // secrets that were successfully added by trial
}

// NewTrialSecretTracker creates a new secret tracker for a repository
func NewTrialSecretTracker(repoSlug string) *TrialSecretTracker {
	return &TrialSecretTracker{
		RepoSlug:     repoSlug,
		AddedSecrets: make(map[string]bool),
	}
}

// determineAndAddEngineSecret determines the required engine secret and adds it to the repository
func determineAndAddEngineSecret(engineConfig *workflow.EngineConfig, hostRepoSlug string, tracker *TrialSecretTracker, engineOverride string, verbose bool) error {
	trialLog.Printf("Determining engine secret for repo: %s", hostRepoSlug)
	var engineType string

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Determining required engine secret for workflow"))
	}

	// Use engine override if provided
	if engineOverride != "" {
		engineType = engineOverride
		trialLog.Printf("Using engine override: %s", engineType)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using engine override: %s", engineType)))
	} else {
		// Check if engine is specified in the EngineConfig
		if engineConfig != nil && engineConfig.ID != "" {
			engineType = engineConfig.ID
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Found engine in EngineConfig: %s", engineType)))
			}
		}
	}

	// Default to copilot if no engine specified
	if engineType == "" {
		engineType = "copilot"
		trialLog.Print("No engine specified, defaulting to copilot")
	}

	// Set the appropriate secret based on engine type
	switch engineType {
	case "claude":
		// Claude supports both CLAUDE_CODE_OAUTH_TOKEN and ANTHROPIC_API_KEY
		// Try to set both if available, fail only if neither is set
		var hasSecret bool

		// Try CLAUDE_CODE_OAUTH_TOKEN first
		if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Setting CLAUDE_CODE_OAUTH_TOKEN secret for Claude engine"))
			}
			if err := addEngineSecret("CLAUDE_CODE_OAUTH_TOKEN", hostRepoSlug, tracker, verbose); err == nil {
				hasSecret = true
			} else if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Failed to set CLAUDE_CODE_OAUTH_TOKEN: "+err.Error()))
			}
		}

		// Try ANTHROPIC_API_KEY
		if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("ANTHROPIC_KEY") != "" {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Setting ANTHROPIC_API_KEY secret for Claude engine"))
			}
			if err := addEngineSecret("ANTHROPIC_API_KEY", hostRepoSlug, tracker, verbose); err == nil {
				hasSecret = true
			} else if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Failed to set ANTHROPIC_API_KEY: "+err.Error()))
			}
		}

		if !hasSecret {
			return fmt.Errorf("neither CLAUDE_CODE_OAUTH_TOKEN nor ANTHROPIC_API_KEY environment variable is set")
		}
		return nil
	case "codex", "openai":
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Setting OPENAI_API_KEY secret for OpenAI engine"))
		}
		return addEngineSecret("OPENAI_API_KEY", hostRepoSlug, tracker, verbose)
	case "copilot":
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Setting COPILOT_GITHUB_TOKEN secret for Copilot engine"))
		}
		return addEngineSecret("COPILOT_GITHUB_TOKEN", hostRepoSlug, tracker, verbose)
	default:
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Unknown engine type '%s', defaulting to Copilot", engineType)))
		}
		return addEngineSecret("COPILOT_GITHUB_TOKEN", hostRepoSlug, tracker, verbose)
	}
}

// addEngineSecret adds an engine-specific secret to the repository with tracking
func addEngineSecret(secretName, hostRepoSlug string, tracker *TrialSecretTracker, verbose bool) error {
	// Check if secret already exists by trying to list secrets
	listOutput, listErr := workflow.RunGHCombined("Checking secrets...", "secret", "list", "--repo", hostRepoSlug)
	secretExists := listErr == nil && strings.Contains(string(listOutput), secretName)

	// Skip if secret already exists
	if secretExists {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Secret %s already exists, skipping", secretName)))
		}
		return nil
	}

	// First try to get the secret from environment variables
	secretValue := os.Getenv(secretName)
	if secretValue == "" {
		// Try common alternative environment variable names
		switch secretName {
		case "ANTHROPIC_API_KEY":
			// Try alternative name ANTHROPIC_KEY
			secretValue = os.Getenv("ANTHROPIC_KEY")
		case "CLAUDE_CODE_OAUTH_TOKEN":
			// No alternative names for CLAUDE_CODE_OAUTH_TOKEN
			// Already checked by os.Getenv(secretName) above
		case "OPENAI_API_KEY":
			secretValue = os.Getenv("OPENAI_KEY")
		case "COPILOT_GITHUB_TOKEN":
			// Use the proper GitHub token helper that handles both env vars and gh CLI
			var err error
			secretValue, err = parser.GetGitHubToken()
			if err != nil {
				return fmt.Errorf("failed to get GitHub token for COPILOT_GITHUB_TOKEN: %w", err)
			}
		}
	}

	if secretValue == "" {
		return fmt.Errorf("environment variable %s not found. Please set it before running the trial", secretName)
	}

	// Use the repository slug directly (should already be in user/repo format)
	repoSlug := hostRepoSlug

	// Add the secret to the repository
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Running: gh secret set %s --repo %s --body <redacted>", secretName, repoSlug)))
	}

	if output, err := workflow.RunGHCombined("Adding secret...", "secret", "set", secretName, "--repo", repoSlug, "--body", secretValue); err != nil {
		return fmt.Errorf("failed to add %s secret: %w\nOutput: %s", secretName, err, string(output))
	}

	// Mark as successfully added (only if tracker is provided)
	if tracker != nil {
		tracker.AddedSecrets[secretName] = true
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully added %s secret", secretName)))
	}

	return nil
}

// addGitHubTokenSecret adds the GitHub token as a repository secret
func addGitHubTokenSecret(repoSlug string, tracker *TrialSecretTracker, verbose bool) error {
	secretName := "GH_AW_GITHUB_TOKEN"
	trialLog.Printf("Adding GitHub token secret to repo: %s", repoSlug)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Adding GitHub token as repository secret"))
	}

	// Check if secret already exists by trying to list secrets
	listOutput, listErr := workflow.RunGHCombined("Checking secrets...", "secret", "list", "--repo", repoSlug)
	secretExists := listErr == nil && strings.Contains(string(listOutput), secretName)

	// Skip if secret already exists
	if secretExists {
		trialLog.Printf("Secret %s already exists, skipping", secretName)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Secret %s already exists, skipping", secretName)))
		}
		return nil
	}

	// Get the current auth token using the proper helper
	token, err := parser.GetGitHubToken()
	if err != nil {
		return fmt.Errorf("failed to get GitHub auth token: %w", err)
	}

	// Add the token as a repository secret
	output, err := workflow.RunGHCombined("Adding secret...", "secret", "set", secretName, "--repo", repoSlug, "--body", token)

	if err != nil {
		return fmt.Errorf("failed to set repository secret: %w (output: %s)", err, string(output))
	}

	// Mark as successfully added (only if tracker is provided)
	if tracker != nil {
		tracker.AddedSecrets[secretName] = true
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Added %s secret to host repository", secretName)))
	}

	return nil
}

// cleanupTrialSecrets cleans up secrets that were added during the trial
func cleanupTrialSecrets(repoSlug string, tracker *TrialSecretTracker, verbose bool) error {
	// Skip cleanup if no tracker was provided (secrets were not pushed)
	if tracker == nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No secrets to clean up (secret pushing was disabled)"))
		}
		return nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Cleaning up API key secrets from host repository"))
	}

	secretsDeleted := 0
	// Only delete secrets that were actually added by this trial command
	for secretName := range tracker.AddedSecrets {
		if output, err := workflow.RunGHCombined("Deleting secret...", "secret", "delete", secretName, "--repo", repoSlug); err != nil {
			// It's okay if the secret doesn't exist, just log in verbose mode
			if verbose && !strings.Contains(string(output), "Not Found") {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Could not delete secret %s: %s", secretName, string(output))))
			}
		} else {
			secretsDeleted++
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Deleted secret: %s", secretName)))
			}
		}
	}

	if verbose {
		if secretsDeleted > 0 {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("API key secrets cleaned up from host repository (%d deleted)", secretsDeleted)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("No secrets needed cleanup (none were added by trial)"))
		}
	}

	return nil
}

// TrialArtifacts represents all artifacts downloaded from a workflow run
type TrialArtifacts struct {
	SafeOutputs map[string]any `json:"safe_outputs"`
	//AgentStdioLogs      []string               `json:"agent_stdio_logs,omitempty"`
	AgenticRunInfo      map[string]any `json:"agentic_run_info,omitempty"`
	AdditionalArtifacts map[string]any `json:"additional_artifacts,omitempty"`
}

// downloadAllArtifacts downloads and parses all available artifacts from a workflow run
func downloadAllArtifacts(hostRepoSlug, runID string, verbose bool) (*TrialArtifacts, error) {
	// Use the repository slug directly (should already be in user/repo format)
	repoSlug := hostRepoSlug

	// Create temp directory for artifact download
	tempDir, err := os.MkdirTemp("", "trial-artifacts-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Download all artifacts for this run
	output, err := workflow.RunGHCombined("Downloading artifacts...", "run", "download", runID, "--repo", repoSlug, "--dir", tempDir)
	if err != nil {
		// If no artifacts exist, that's okay - some workflows don't generate artifacts
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No artifacts found for run %s: %s", runID, string(output))))
		}
		return &TrialArtifacts{}, nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Downloaded all artifacts for run %s to %s", runID, tempDir)))
	}

	artifacts := &TrialArtifacts{
		AdditionalArtifacts: make(map[string]any),
	}

	// Walk through all downloaded artifacts
	err = filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Get relative path from temp directory
		relPath, err := filepath.Rel(tempDir, path)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to get relative path for %s: %v", path, err)))
			}
			return nil
		}

		// Handle specific artifact types
		switch {
		case strings.HasSuffix(path, constants.AgentOutputFilename):
			// Parse safe outputs
			if safeOutputs := parseJSONArtifact(path, verbose); safeOutputs != nil {
				artifacts.SafeOutputs = safeOutputs
			}

		case strings.HasSuffix(path, "aw_info.json"):
			// Parse agentic run information
			if runInfo := parseJSONArtifact(path, verbose); runInfo != nil {
				artifacts.AgenticRunInfo = runInfo
			}

		// case strings.Contains(relPath, "agent") && strings.HasSuffix(path, ".log"):
		// 	// Collect agent stdio logs
		// 	if logContent := readTextArtifact(path, verbose); logContent != "" {
		// 		artifacts.AgentStdioLogs = append(artifacts.AgentStdioLogs, logContent)
		// 	}

		case strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".jsonl") || strings.HasSuffix(path, ".log") || strings.HasSuffix(path, ".txt"):
			// Handle other artifacts
			if strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".jsonl") {
				if content := parseJSONArtifact(path, verbose); content != nil {
					artifacts.AdditionalArtifacts[relPath] = content
				}
			} else {
				if content := readTextArtifact(path, verbose); content != "" {
					artifacts.AdditionalArtifacts[relPath] = content
				}
			}
		}

		return nil
	})

	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Error walking artifact directory: %v", err)))
		}
	}

	return artifacts, nil
}

// parseJSONArtifact parses a JSON artifact file and returns the parsed content
func parseJSONArtifact(filePath string, verbose bool) map[string]any {
	content, err := os.ReadFile(filePath)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read JSON artifact %s: %v", filePath, err)))
		}
		return nil
	}

	var parsed map[string]any
	if err := json.Unmarshal(content, &parsed); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse JSON artifact %s: %v", filePath, err)))
		}
		return nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Parsed JSON artifact: %s", filepath.Base(filePath))))
	}

	return parsed
}

// readTextArtifact reads a text artifact file and returns its content
func readTextArtifact(filePath string, verbose bool) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read text artifact %s: %v", filePath, err)))
		}
		return ""
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Read text artifact: %s (%d bytes)", filepath.Base(filePath), len(content))))
	}

	return string(content)
}
