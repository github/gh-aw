package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotCodingAgentLog = logger.New("cli:copilot_agent")

// agentLogPatterns contains patterns that indicate GitHub Copilot coding agent (not Copilot CLI)
var agentLogPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)github.*copilot.*agent`),
	regexp.MustCompile(`(?i)copilot-swe-agent`),
	regexp.MustCompile(`(?i)@github/copilot-swe-agent`),
	regexp.MustCompile(`(?i)agent.*task.*execution`),
	regexp.MustCompile(`(?i)copilot.*agent.*v\d+\.\d+`),
}

// CopilotCodingAgentDetector contains heuristics to detect if a workflow run was executed by GitHub Copilot coding agent
type CopilotCodingAgentDetector struct {
	runDir       string
	verbose      bool
	workflowPath string // Optional: workflow file path from GitHub API (e.g., .github/workflows/copilot-swe-agent.yml)
}

// NewCopilotCodingAgentDetector creates a new detector for GitHub Copilot coding agent runs
func NewCopilotCodingAgentDetector(runDir string, verbose bool) *CopilotCodingAgentDetector {
	return &CopilotCodingAgentDetector{
		runDir:  runDir,
		verbose: verbose,
	}
}

// NewCopilotCodingAgentDetectorWithPath creates a detector with workflow path hint
func NewCopilotCodingAgentDetectorWithPath(runDir string, verbose bool, workflowPath string) *CopilotCodingAgentDetector {
	return &CopilotCodingAgentDetector{
		runDir:       runDir,
		verbose:      verbose,
		workflowPath: workflowPath,
	}
}

// IsGitHubCopilotCodingAgent uses heuristics to determine if this run was executed by GitHub Copilot coding agent
// (not the Copilot CLI engine or agentic workflows)
func (d *CopilotCodingAgentDetector) IsGitHubCopilotCodingAgent() bool {
	copilotCodingAgentLog.Printf("Detecting if run is GitHub Copilot coding agent: %s", d.runDir)

	// If aw_info.json exists, this is an agentic workflow, NOT a GitHub Copilot coding agent run
	awInfoPath := filepath.Join(d.runDir, "aw_info.json")
	if fileutil.FileExists(awInfoPath) {
		copilotCodingAgentLog.Print("Found aw_info.json - this is an agentic workflow, not a GitHub Copilot coding agent")
		return false
	}

	// Heuristic 1: Check workflow path if provided (most reliable hint from GitHub API)
	if d.hasAgentWorkflowPath() {
		copilotCodingAgentLog.Print("Detected copilot-swe-agent in workflow path")
		return true
	}

	// Heuristic 2: Check for agent-specific log patterns
	if d.hasAgentLogPatterns() {
		copilotCodingAgentLog.Print("Detected agent log patterns")
		return true
	}

	// Heuristic 3: Check for agent-specific artifacts
	if d.hasAgentArtifacts() {
		copilotCodingAgentLog.Print("Detected agent artifacts")
		return true
	}

	copilotCodingAgentLog.Print("No GitHub Copilot coding agent indicators found")
	return false
}

// hasAgentWorkflowPath checks if the workflow path indicates a Copilot coding agent run
// The workflow ID is always "copilot-swe-agent" for GitHub Copilot coding agent runs
func (d *CopilotCodingAgentDetector) hasAgentWorkflowPath() bool {
	if d.workflowPath == "" {
		return false
	}

	// Extract the workflow filename from the path
	// E.g., .github/workflows/copilot-swe-agent.yml -> copilot-swe-agent
	filename := filepath.Base(d.workflowPath)
	workflowID := strings.TrimSuffix(filename, filepath.Ext(filename))

	// GitHub Copilot coding agent runs always use "copilot-swe-agent" as the workflow ID
	if workflowID == "copilot-swe-agent" {
		if d.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
				fmt.Sprintf("Detected GitHub Copilot coding agent from workflow path: %s (ID: %s)", d.workflowPath, workflowID)))
		}
		return true
	}

	return false
}

// hasAgentLogPatterns checks log files for patterns specific to GitHub Copilot coding agent
func (d *CopilotCodingAgentDetector) hasAgentLogPatterns() bool {
	found := false
	// Check log files for agent-specific patterns
	if walkErr := filepath.Walk(d.runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			copilotCodingAgentLog.Printf("walk error at %s: %v", path, err)
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}

		fileName := strings.ToLower(info.Name())
		if strings.HasSuffix(fileName, ".log") || strings.HasSuffix(fileName, ".txt") {
			// Read first 10KB of log file to check patterns
			content, err := readLogHeader(path, 10240)
			if err != nil {
				return nil
			}

			for _, pattern := range agentLogPatterns {
				if pattern.MatchString(content) {
					if d.verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
							fmt.Sprintf("Found agent pattern in %s: %s", filepath.Base(path), pattern.String())))
					}
					found = true
					return filepath.SkipAll // Stop walking, we found a match
				}
			}
		}

		return nil
	}); walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", d.runDir, walkErr)))
	}

	return found
}

// hasAgentArtifacts checks for artifacts specific to GitHub Copilot coding agent runs
func (d *CopilotCodingAgentDetector) hasAgentArtifacts() bool {
	// Check for agent-specific artifact patterns
	agentArtifacts := []string{
		"copilot-agent-output",
		"agent-session-result",
		"copilot-swe-agent-output",
	}

	for _, artifactName := range agentArtifacts {
		artifactPath := filepath.Join(d.runDir, artifactName)
		if _, err := os.Stat(artifactPath); err == nil {
			if d.verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
					"Found agent artifact: "+artifactName))
			}
			return true
		}
	}

	return false
}

// readLogHeader reads the first maxBytes from a log file
func readLogHeader(path string, maxBytes int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	buffer := make([]byte, maxBytes)
	n, err := file.Read(buffer)
	if err != nil && n == 0 {
		return "", err
	}

	return string(buffer[:n]), nil
}
