package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var trialSupportLog = logger.New("cli:trial_support")

// TrialArtifacts represents all artifacts downloaded from a workflow run
type TrialArtifacts struct {
	SafeOutputs map[string]any `json:"safe_outputs"`
	//AgentStdioLogs      []string               `json:"agent_stdio_logs,omitempty"`
	AgenticRunInfo      map[string]any `json:"agentic_run_info,omitempty"`
	AdditionalArtifacts map[string]any `json:"additional_artifacts,omitempty"`
}

// downloadAllArtifacts downloads and parses all available artifacts from a workflow run
func downloadAllArtifacts(hostRepoSlug, runID string, verbose bool) (*TrialArtifacts, error) {
	trialSupportLog.Printf("Downloading artifacts: repo=%s, runID=%s", hostRepoSlug, runID)
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
	walkErr := filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
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
		case strings.HasSuffix(path, constants.AgentOutputFilename.String()):
			// Parse safe outputs
			trialSupportLog.Printf("Processing safe outputs artifact: %s", relPath)
			if safeOutputs := parseJSONArtifact(path, verbose); safeOutputs != nil {
				artifacts.SafeOutputs = safeOutputs
			}

		case strings.HasSuffix(path, "aw_info.json"):
			// Parse agentic run information
			trialSupportLog.Printf("Processing agentic run info artifact: %s", relPath)
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

	if walkErr != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Error walking artifact directory: %v", walkErr)))
		}
	}

	return artifacts, nil
}

// parseJSONArtifact parses a JSON artifact file and returns the parsed content
func parseJSONArtifact(filePath string, verbose bool) map[string]any {
	trialSupportLog.Printf("Parsing JSON artifact: %s", filePath)
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
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Parsed JSON artifact: "+filepath.Base(filePath)))
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
