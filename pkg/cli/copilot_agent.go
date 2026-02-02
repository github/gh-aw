package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var copilotAgentLog = logger.New("cli:copilot_agent")

// CopilotAgentDetector contains heuristics to detect if a workflow run was executed by GitHub Copilot agent
type CopilotAgentDetector struct {
	runDir       string
	verbose      bool
	workflowPath string // Optional: workflow file path from GitHub API (e.g., .github/workflows/copilot-swe-agent.yml)
}

// NewCopilotAgentDetector creates a new detector for GitHub Copilot agent runs
func NewCopilotAgentDetector(runDir string, verbose bool) *CopilotAgentDetector {
	return &CopilotAgentDetector{
		runDir:  runDir,
		verbose: verbose,
	}
}

// NewCopilotAgentDetectorWithPath creates a detector with workflow path hint
func NewCopilotAgentDetectorWithPath(runDir string, verbose bool, workflowPath string) *CopilotAgentDetector {
	return &CopilotAgentDetector{
		runDir:       runDir,
		verbose:      verbose,
		workflowPath: workflowPath,
	}
}

// IsGitHubCopilotAgent uses heuristics to determine if this run was executed by GitHub Copilot agent
// (not the Copilot CLI engine or agentic workflows)
func (d *CopilotAgentDetector) IsGitHubCopilotAgent() bool {
	copilotAgentLog.Printf("Detecting if run is GitHub Copilot agent: %s", d.runDir)

	// If aw_info.json exists, this is an agentic workflow, NOT a GitHub Copilot agent run
	awInfoPath := filepath.Join(d.runDir, "aw_info.json")
	if _, err := os.Stat(awInfoPath); err == nil {
		copilotAgentLog.Print("Found aw_info.json - this is an agentic workflow, not a GitHub Copilot agent")
		return false
	}

	// Heuristic 1: Check workflow path if provided (most reliable hint from GitHub API)
	if d.hasAgentWorkflowPath() {
		copilotAgentLog.Print("Detected copilot-swe-agent in workflow path")
		return true
	}

	// Heuristic 2: Check for agent-specific log patterns
	if d.hasAgentLogPatterns() {
		copilotAgentLog.Print("Detected agent log patterns")
		return true
	}

	// Heuristic 3: Check for agent-specific artifacts
	if d.hasAgentArtifacts() {
		copilotAgentLog.Print("Detected agent artifacts")
		return true
	}

	copilotAgentLog.Print("No GitHub Copilot agent indicators found")
	return false
}

// hasAgentWorkflowPath checks if the workflow path indicates a Copilot agent run
// The workflow ID is always "copilot-swe-agent" for GitHub Copilot agent runs
func (d *CopilotAgentDetector) hasAgentWorkflowPath() bool {
	if d.workflowPath == "" {
		return false
	}

	// Extract the workflow filename from the path
	// E.g., .github/workflows/copilot-swe-agent.yml -> copilot-swe-agent
	filename := filepath.Base(d.workflowPath)
	workflowID := strings.TrimSuffix(filename, filepath.Ext(filename))

	// GitHub Copilot agent runs always use "copilot-swe-agent" as the workflow ID
	if workflowID == "copilot-swe-agent" {
		if d.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
				fmt.Sprintf("Detected GitHub Copilot agent from workflow path: %s (ID: %s)", d.workflowPath, workflowID)))
		}
		return true
	}

	return false
}

// hasAgentLogPatterns checks log files for patterns specific to GitHub Copilot agent
func (d *CopilotAgentDetector) hasAgentLogPatterns() bool {
	// Patterns that indicate GitHub Copilot agent (not Copilot CLI)
	agentPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)github.*copilot.*agent`),
		regexp.MustCompile(`(?i)copilot-swe-agent`),
		regexp.MustCompile(`(?i)@github/copilot-swe-agent`),
		regexp.MustCompile(`(?i)agent.*task.*execution`),
		regexp.MustCompile(`(?i)copilot.*agent.*v\d+\.\d+`),
	}

	found := false
	// Check log files for agent-specific patterns
	_ = filepath.Walk(d.runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}

		fileName := strings.ToLower(info.Name())
		if strings.HasSuffix(fileName, ".log") || strings.HasSuffix(fileName, ".txt") {
			// Read first 10KB of log file to check patterns
			content, err := readLogHeader(path, 10240)
			if err != nil {
				return nil
			}

			for _, pattern := range agentPatterns {
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
	})

	return found
}

// hasAgentArtifacts checks for artifacts specific to GitHub Copilot agent runs
func (d *CopilotAgentDetector) hasAgentArtifacts() bool {
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
					fmt.Sprintf("Found agent artifact: %s", artifactName)))
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

// ParseCopilotAgentLogMetrics extracts metrics from GitHub Copilot agent logs
// This is different from Copilot CLI logs and requires specialized parsing
func ParseCopilotAgentLogMetrics(logContent string, verbose bool) workflow.LogMetrics {
	copilotAgentLog.Printf("Parsing GitHub Copilot agent log metrics: %d bytes", len(logContent))

	var metrics workflow.LogMetrics
	var maxTokenUsage int

	lines := strings.Split(logContent, "\n")
	toolCallMap := make(map[string]*workflow.ToolCallInfo)
	var currentSequence []string
	turns := 0

	// GitHub Copilot agent log patterns
	// These patterns are designed to match the specific log format of the agent
	turnPattern := regexp.MustCompile(`(?i)task.*iteration|agent.*turn|step.*\d+`)
	toolCallPattern := regexp.MustCompile(`(?i)tool.*call|executing.*tool|calling.*(\w+)`)

	for _, line := range lines {
		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Count turns based on agent iteration patterns
		if turnPattern.MatchString(line) {
			turns++
			// Start of a new turn, save previous sequence if any
			if len(currentSequence) > 0 {
				metrics.ToolSequences = append(metrics.ToolSequences, currentSequence)
				currentSequence = []string{}
			}
		}

		// Extract tool calls from agent logs
		if matches := toolCallPattern.FindStringSubmatch(line); len(matches) > 1 {
			toolName := extractToolName(line)
			if toolName != "" {
				// Track tool call
				if _, exists := toolCallMap[toolName]; !exists {
					toolCallMap[toolName] = &workflow.ToolCallInfo{
						Name:      toolName,
						CallCount: 0,
					}
				}
				toolCallMap[toolName].CallCount++

				// Add to current sequence
				currentSequence = append(currentSequence, toolName)

				if verbose {
					copilotAgentLog.Printf("Found tool call: %s", toolName)
				}
			}
		}

		// Try to extract token usage from JSON format if available
		jsonMetrics := workflow.ExtractJSONMetrics(line, verbose)
		if jsonMetrics.TokenUsage > 0 || jsonMetrics.EstimatedCost > 0 {
			if jsonMetrics.TokenUsage > maxTokenUsage {
				maxTokenUsage = jsonMetrics.TokenUsage
			}
			if jsonMetrics.EstimatedCost > 0 {
				metrics.EstimatedCost += jsonMetrics.EstimatedCost
			}
		}
	}

	// Add final sequence if any
	if len(currentSequence) > 0 {
		metrics.ToolSequences = append(metrics.ToolSequences, currentSequence)
	}

	// Convert tool call map to slice
	for _, toolInfo := range toolCallMap {
		metrics.ToolCalls = append(metrics.ToolCalls, *toolInfo)
	}

	metrics.TokenUsage = maxTokenUsage
	metrics.Turns = turns

	copilotAgentLog.Printf("Parsed metrics: tokens=%d, cost=$%.4f, turns=%d",
		metrics.TokenUsage, metrics.EstimatedCost, metrics.Turns)

	return metrics
}

// extractToolName extracts a tool name from a log line
func extractToolName(line string) string {
	// Try to extract tool name from various patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)tool[:\s]+([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)calling[:\s]+([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)executing[:\s]+([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)using[:\s]+tool[:\s]+([a-zA-Z0-9_-]+)`),
	}

	for _, pattern := range patterns {
		if matches := pattern.FindStringSubmatch(line); len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
	}

	return ""
}
