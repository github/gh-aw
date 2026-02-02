// This file provides command-line interface functionality for gh-aw.
// This file (logs_metrics.go) contains functions for extracting and analyzing
// metrics from workflow execution logs.
//
// Key responsibilities:
//   - Extracting token usage, cost, and turn metrics from logs
//   - Identifying missing tools requested by AI agents
//   - Detecting MCP (Model Context Protocol) server failures
//   - Aggregating metrics across multiple log files
//   - Processing structured output from agent execution

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/cli/fileutil"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsMetricsLog = logger.New("cli:logs_metrics")

// Shared utilities are now in workflow package
// extractJSONMetrics is available as an alias
var extractJSONMetrics = workflow.ExtractJSONMetrics

// extractLogMetrics extracts metrics from downloaded log files
// workflowPath is optional and can be provided to help detect GitHub Copilot agent runs
func extractLogMetrics(logDir string, verbose bool, workflowPath ...string) (LogMetrics, error) {
	logsMetricsLog.Printf("Extracting log metrics from: %s", logDir)
	var metrics LogMetrics
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Beginning metric extraction in %s", logDir)))
	}

	// First check if this is a GitHub Copilot agent run (not Copilot CLI)
	var detector *CopilotAgentDetector
	if len(workflowPath) > 0 && workflowPath[0] != "" {
		detector = NewCopilotAgentDetectorWithPath(logDir, verbose, workflowPath[0])
	} else {
		detector = NewCopilotAgentDetector(logDir, verbose)
	}
	isGitHubCopilotAgent := detector.IsGitHubCopilotAgent()
	logsMetricsLog.Printf("GitHub Copilot agent detected: %v", isGitHubCopilotAgent)

	if isGitHubCopilotAgent && verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Detected GitHub Copilot agent run, using specialized parser"))
	}

	// First check for aw_info.json to determine the engine
	var detectedEngine workflow.CodingAgentEngine
	infoFilePath := filepath.Join(logDir, "aw_info.json")
	logsMetricsLog.Printf("Checking for aw_info.json at: %s", infoFilePath)
	if _, err := os.Stat(infoFilePath); err == nil {
		logsMetricsLog.Print("Found aw_info.json, extracting engine")
		// aw_info.json exists, try to extract engine information
		if engine := extractEngineFromAwInfo(infoFilePath, verbose); engine != nil {
			detectedEngine = engine
			logsMetricsLog.Printf("Detected engine: %s", engine.GetID())
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Detected engine from aw_info.json: %s", engine.GetID())))
			}
		} else {
			logsMetricsLog.Print("Failed to extract engine from aw_info.json")
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("aw_info.json exists but failed to extract engine"))
			}
		}
	} else {
		logsMetricsLog.Printf("No aw_info.json found at %s: %v", infoFilePath, err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No aw_info.json found at %s", infoFilePath)))
		}
	}

	// Check for safe_output.jsonl artifact file
	awOutputPath := filepath.Join(logDir, "safe_output.jsonl")
	if _, err := os.Stat(awOutputPath); err == nil {
		if verbose {
			// Report that the agentic output file was found
			fileInfo, statErr := os.Stat(awOutputPath)
			if statErr == nil {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found agentic output file: safe_output.jsonl (%s)", console.FormatFileSize(fileInfo.Size()))))
			}
		}
	}

	// Check for aw.patch artifact file
	awPatchPath := filepath.Join(logDir, "aw.patch")
	if _, err := os.Stat(awPatchPath); err == nil {
		if verbose {
			// Report that the git patch file was found
			fileInfo, statErr := os.Stat(awPatchPath)
			if statErr == nil {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found git patch file: aw.patch (%s)", console.FormatFileSize(fileInfo.Size()))))
			}
		}
	}

	// Check for agent_output.json artifact (some workflows may store this under a nested directory)
	agentOutputPath, agentOutputFound := findAgentOutputFile(logDir)
	if agentOutputFound {
		if verbose {
			fileInfo, statErr := os.Stat(agentOutputPath)
			if statErr == nil {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found agent output file: %s (%s)", filepath.Base(agentOutputPath), console.FormatFileSize(fileInfo.Size()))))
			}
		}
		// If the file is not already in the logDir root, copy it for convenience
		if filepath.Dir(agentOutputPath) != logDir {
			rootCopy := filepath.Join(logDir, constants.AgentOutputArtifactName)
			if _, err := os.Stat(rootCopy); errors.Is(err, os.ErrNotExist) {
				if copyErr := fileutil.CopyFile(agentOutputPath, rootCopy); copyErr == nil && verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Copied agent_output.json to run root for easy access"))
				}
			}
		}
	}

	// Walk through all files in the log directory
	err := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Process log files - exclude output artifacts like aw_output.txt and agent_output.json
		fileName := strings.ToLower(info.Name())
		if (strings.HasSuffix(fileName, ".log") ||
			(strings.HasSuffix(fileName, ".txt") && strings.Contains(fileName, "log"))) &&
			!strings.Contains(fileName, "aw_output") &&
			fileName != constants.AgentOutputFilename {

			fileMetrics, err := parseLogFileWithEngine(path, detectedEngine, isGitHubCopilotAgent, verbose)
			if err != nil && verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse log file %s: %v", path, err)))
				return nil // Continue processing other files
			}

			// Aggregate metrics
			metrics.TokenUsage += fileMetrics.TokenUsage
			metrics.EstimatedCost += fileMetrics.EstimatedCost
			if fileMetrics.Turns > metrics.Turns {
				// For turns, take the maximum rather than summing, since turns represent
				// the total conversation turns for the entire workflow run
				metrics.Turns = fileMetrics.Turns
			}

			// Aggregate tool sequences and tool calls
			metrics.ToolSequences = append(metrics.ToolSequences, fileMetrics.ToolSequences...)
			metrics.ToolCalls = append(metrics.ToolCalls, fileMetrics.ToolCalls...)
		}

		return nil
	})

	// Try to parse gateway.jsonl if it exists
	gatewayMetrics, gatewayErr := parseGatewayLogs(logDir, verbose)
	if gatewayErr == nil && gatewayMetrics != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Successfully parsed gateway.jsonl"))
		}
		// We've successfully parsed gateway metrics, but we don't add them to the main metrics
		// structure since they're tracked separately and displayed in their own table
		logsMetricsLog.Printf("Parsed gateway.jsonl: %d servers, %d requests",
			len(gatewayMetrics.Servers), gatewayMetrics.TotalRequests)
	} else if gatewayErr != nil && !strings.Contains(gatewayErr.Error(), "not found") {
		// Only log if it's an error other than "not found"
		logsMetricsLog.Printf("Failed to parse gateway.jsonl: %v", gatewayErr)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse gateway.jsonl: %v", gatewayErr)))
		}
	}

	if logsMetricsLog.Enabled() {
		logsMetricsLog.Printf("Metrics extraction completed: tokens=%d, cost=%.4f, turns=%d",
			metrics.TokenUsage, metrics.EstimatedCost, metrics.Turns)
	}
	return metrics, err
}

// ExtractLogMetricsFromRun extracts log metrics from a processed run's log directory
func ExtractLogMetricsFromRun(processedRun ProcessedRun) workflow.LogMetrics {
	// Use the LogsPath from the WorkflowRun to get metrics
	if processedRun.Run.LogsPath == "" {
		return workflow.LogMetrics{}
	}

	// Extract metrics from the log directory
	metrics, err := extractLogMetrics(processedRun.Run.LogsPath, false)
	if err != nil {
		return workflow.LogMetrics{}
	}

	return metrics
}

// extractMissingToolsFromRun extracts missing tool reports from a workflow run's artifacts
func extractMissingToolsFromRun(runDir string, run WorkflowRun, verbose bool) ([]MissingToolReport, error) {
	logsMetricsLog.Printf("Extracting missing tools from run: %d", run.DatabaseID)
	var missingTools []MissingToolReport

	// Look for the safe output artifact file that contains structured JSON with items array
	// This file is created by the collect_ndjson_output.cjs script during workflow execution
	// After artifact refactoring, the file is flattened to agent_output.json at root
	agentOutputJSONPath := filepath.Join(runDir, constants.AgentOutputFilename)

	// Support both new flattened form (agent_output.json) and old forms for backward compatibility:
	// 1. New: agent_output.json at root (after flattening)
	// 2. Old: agent-output directory with nested agent-output file
	// 3. Fallback: search recursively
	var resolvedAgentOutputFile string
	if stat, err := os.Stat(agentOutputJSONPath); err == nil && !stat.IsDir() {
		// New flattened structure: agent_output.json at root
		resolvedAgentOutputFile = agentOutputJSONPath
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %s at root: %s", constants.AgentOutputFilename, agentOutputJSONPath)))
		}
	} else {
		// Try old structure: agent-output directory
		agentOutputPath := filepath.Join(runDir, constants.AgentOutputArtifactName)
		if stat, err := os.Stat(agentOutputPath); err == nil {
			if stat.IsDir() {
				// Directory form – look for nested file
				nested := filepath.Join(agentOutputPath, constants.AgentOutputArtifactName)
				if _, nestedErr := os.Stat(nested); nestedErr == nil {
					resolvedAgentOutputFile = nested
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("agent_output.json is a directory; using nested file %s", nested)))
					}
				} else if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("agent_output.json directory present but nested file missing: %v", nestedErr)))
				}
			} else {
				// Regular file
				resolvedAgentOutputFile = agentOutputPath
			}
		} else {
			// Not present at root – search recursively (depth-first) for a file named agent_output.json
			if found, ok := findAgentOutputFile(runDir); ok {
				resolvedAgentOutputFile = found
				if verbose && found != agentOutputPath {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found agent_output.json at %s", found)))
				}
			}
		}
	}

	if resolvedAgentOutputFile != "" {
		// Sanitize the path to prevent path traversal attacks
		cleanPath := filepath.Clean(resolvedAgentOutputFile)

		// Read the safe output artifact file
		content, readErr := os.ReadFile(cleanPath)
		if readErr != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read safe output file %s: %v", cleanPath, readErr)))
			}
			return missingTools, nil // Continue processing without this file
		}

		// Parse the structured JSON output from the collect script
		var safeOutput struct {
			Items  []json.RawMessage `json:"items"`
			Errors []string          `json:"errors,omitempty"`
		}

		if err := json.Unmarshal(content, &safeOutput); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse safe output JSON from %s: %v", cleanPath, err)))
			}
			return missingTools, nil // Continue processing without this file
		}

		// Extract missing-tool entries from the items array
		for _, itemRaw := range safeOutput.Items {
			var item struct {
				Type         string `json:"type"`
				Tool         string `json:"tool,omitempty"`
				Reason       string `json:"reason,omitempty"`
				Alternatives string `json:"alternatives,omitempty"`
				Timestamp    string `json:"timestamp,omitempty"`
			}

			if err := json.Unmarshal(itemRaw, &item); err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse item from safe output: %v", err)))
				}
				continue // Skip malformed items
			}

			// Check if this is a missing-tool entry
			if item.Type == "missing_tool" {
				missingTool := MissingToolReport{
					Tool:         item.Tool,
					Reason:       item.Reason,
					Alternatives: item.Alternatives,
					Timestamp:    item.Timestamp,
					WorkflowName: run.WorkflowName,
					RunID:        run.DatabaseID,
				}
				missingTools = append(missingTools, missingTool)

				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found missing_tool entry: %s (%s)", item.Tool, item.Reason)))
				}
			}
		}

		if verbose && len(missingTools) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d missing tool reports in safe output artifact for run %d", len(missingTools), run.DatabaseID)))
		}
		logsMetricsLog.Printf("Found %d missing tool reports", len(missingTools))
	} else {
		logsMetricsLog.Print("No safe output artifact found")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No safe output artifact found at %s for run %d", agentOutputJSONPath, run.DatabaseID)))
		}
	}

	return missingTools, nil
}

// extractNoopsFromRun extracts noop messages from a workflow run's artifacts
func extractNoopsFromRun(runDir string, run WorkflowRun, verbose bool) ([]NoopReport, error) {
	logsMetricsLog.Printf("Extracting noops from run: %d", run.DatabaseID)
	var noops []NoopReport

	// Look for the safe output artifact file that contains structured JSON with items array
	// This file is created by the collect_ndjson_output.cjs script during workflow execution
	// After artifact refactoring, the file is flattened to agent_output.json at root
	agentOutputJSONPath := filepath.Join(runDir, constants.AgentOutputFilename)

	// Support both new flattened form (agent_output.json) and old forms for backward compatibility:
	// 1. New: agent_output.json at root (after flattening)
	// 2. Old: agent-output directory with nested agent-output file
	// 3. Fallback: search recursively
	var resolvedAgentOutputFile string
	if stat, err := os.Stat(agentOutputJSONPath); err == nil && !stat.IsDir() {
		// New flattened structure: agent_output.json at root
		resolvedAgentOutputFile = agentOutputJSONPath
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %s at root: %s", constants.AgentOutputFilename, agentOutputJSONPath)))
		}
	} else {
		// Try old structure: agent-output directory
		agentOutputPath := filepath.Join(runDir, constants.AgentOutputArtifactName)
		if stat, err := os.Stat(agentOutputPath); err == nil {
			if stat.IsDir() {
				// Directory form – look for nested file
				nested := filepath.Join(agentOutputPath, constants.AgentOutputArtifactName)
				if _, nestedErr := os.Stat(nested); nestedErr == nil {
					resolvedAgentOutputFile = nested
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("agent_output.json is a directory; using nested file %s", nested)))
					}
				} else if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("agent_output.json directory present but nested file missing: %v", nestedErr)))
				}
			} else {
				// Regular file
				resolvedAgentOutputFile = agentOutputPath
			}
		} else {
			// Not present at root – search recursively (depth-first) for a file named agent_output.json
			if found, ok := findAgentOutputFile(runDir); ok {
				resolvedAgentOutputFile = found
				if verbose && found != agentOutputPath {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found agent_output.json at %s", found)))
				}
			}
		}
	}

	if resolvedAgentOutputFile != "" {
		// Sanitize the path to prevent path traversal attacks
		cleanPath := filepath.Clean(resolvedAgentOutputFile)

		// Read the safe output artifact file
		content, readErr := os.ReadFile(cleanPath)
		if readErr != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read safe output file %s: %v", cleanPath, readErr)))
			}
			return noops, nil // Continue processing without this file
		}

		// Parse the structured JSON output from the collect script
		var safeOutput struct {
			Items  []json.RawMessage `json:"items"`
			Errors []string          `json:"errors,omitempty"`
		}

		if err := json.Unmarshal(content, &safeOutput); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse safe output JSON from %s: %v", cleanPath, err)))
			}
			return noops, nil // Continue processing without this file
		}

		// Extract noop entries from the items array
		for _, itemRaw := range safeOutput.Items {
			var item struct {
				Type      string `json:"type"`
				Message   string `json:"message,omitempty"`
				Timestamp string `json:"timestamp,omitempty"`
			}

			if err := json.Unmarshal(itemRaw, &item); err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse item from safe output: %v", err)))
				}
				continue // Skip malformed items
			}

			// Check if this is a noop entry
			if item.Type == "noop" {
				noop := NoopReport{
					Message:      item.Message,
					Timestamp:    item.Timestamp,
					WorkflowName: run.WorkflowName,
					RunID:        run.DatabaseID,
				}
				noops = append(noops, noop)

				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found noop entry: %s", item.Message)))
				}
			}
		}

		if verbose && len(noops) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d noop messages in safe output artifact for run %d", len(noops), run.DatabaseID)))
		}
		logsMetricsLog.Printf("Found %d noop messages", len(noops))
	} else {
		logsMetricsLog.Print("No safe output artifact found")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No safe output artifact found at %s for run %d", agentOutputJSONPath, run.DatabaseID)))
		}
	}

	return noops, nil
}

// extractMissingDataFromRun extracts missing data reports from a workflow run's artifacts
func extractMissingDataFromRun(runDir string, run WorkflowRun, verbose bool) ([]MissingDataReport, error) {
	logsMetricsLog.Printf("Extracting missing data from run: %d", run.DatabaseID)
	var missingData []MissingDataReport

	// Look for the safe output artifact file that contains structured JSON with items array
	// This file is created by the collect_ndjson_output.cjs script during workflow execution
	// After artifact refactoring, the file is flattened to agent_output.json at root
	agentOutputJSONPath := filepath.Join(runDir, constants.AgentOutputFilename)

	// Support both new flattened form (agent_output.json) and old forms for backward compatibility:
	// 1. New: agent_output.json at root (after flattening)
	// 2. Old: agent-output directory with nested agent-output file
	// 3. Fallback: search recursively
	var resolvedAgentOutputFile string
	if stat, err := os.Stat(agentOutputJSONPath); err == nil && !stat.IsDir() {
		// New flattened structure: agent_output.json at root
		resolvedAgentOutputFile = agentOutputJSONPath
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %s at root: %s", constants.AgentOutputFilename, agentOutputJSONPath)))
		}
	} else {
		// Try old structure: agent-output directory
		agentOutputPath := filepath.Join(runDir, constants.AgentOutputArtifactName)
		if stat, err := os.Stat(agentOutputPath); err == nil {
			if stat.IsDir() {
				// Directory form – look for nested file
				nested := filepath.Join(agentOutputPath, constants.AgentOutputArtifactName)
				if _, nestedErr := os.Stat(nested); nestedErr == nil {
					resolvedAgentOutputFile = nested
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("agent_output.json is a directory; using nested file %s", nested)))
					}
				} else if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("agent_output.json directory present but nested file missing: %v", nestedErr)))
				}
			} else {
				// Regular file
				resolvedAgentOutputFile = agentOutputPath
			}
		} else {
			// Not present at root – search recursively (depth-first) for a file named agent_output.json
			if found, ok := findAgentOutputFile(runDir); ok {
				resolvedAgentOutputFile = found
				if verbose && found != agentOutputPath {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found agent_output.json at %s", found)))
				}
			}
		}
	}

	if resolvedAgentOutputFile != "" {
		// Sanitize the path to prevent path traversal attacks
		cleanPath := filepath.Clean(resolvedAgentOutputFile)

		// Read the safe output artifact file
		content, readErr := os.ReadFile(cleanPath)
		if readErr != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read safe output file %s: %v", cleanPath, readErr)))
			}
			return missingData, nil // Continue processing without this file
		}

		// Parse the structured JSON output from the collect script
		var safeOutput struct {
			Items  []json.RawMessage `json:"items"`
			Errors []string          `json:"errors,omitempty"`
		}

		if err := json.Unmarshal(content, &safeOutput); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse safe output JSON from %s: %v", cleanPath, err)))
			}
			return missingData, nil // Continue processing without this file
		}

		// Extract missing_data entries from the items array
		for _, itemRaw := range safeOutput.Items {
			var item struct {
				Type         string `json:"type"`
				DataType     string `json:"data_type,omitempty"`
				Reason       string `json:"reason,omitempty"`
				Context      string `json:"context,omitempty"`
				Alternatives string `json:"alternatives,omitempty"`
				Timestamp    string `json:"timestamp,omitempty"`
			}

			if err := json.Unmarshal(itemRaw, &item); err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse item from safe output: %v", err)))
				}
				continue // Skip malformed items
			}

			// Check if this is a missing_data entry
			if item.Type == "missing_data" {
				missingDataItem := MissingDataReport{
					DataType:     item.DataType,
					Reason:       item.Reason,
					Context:      item.Context,
					Alternatives: item.Alternatives,
					Timestamp:    item.Timestamp,
					WorkflowName: run.WorkflowName,
					RunID:        run.DatabaseID,
				}
				missingData = append(missingData, missingDataItem)

				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found missing_data entry: %s (%s)", item.DataType, item.Reason)))
				}
			}
		}

		if verbose && len(missingData) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d missing data reports in safe output artifact for run %d", len(missingData), run.DatabaseID)))
		}
		logsMetricsLog.Printf("Found %d missing data reports", len(missingData))
	} else {
		logsMetricsLog.Print("No safe output artifact found")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No safe output artifact found at %s for run %d", agentOutputJSONPath, run.DatabaseID)))
		}
	}

	return missingData, nil
}

// extractMCPFailuresFromRun extracts MCP server failure reports from a workflow run's logs
func extractMCPFailuresFromRun(runDir string, run WorkflowRun, verbose bool) ([]MCPFailureReport, error) {
	logsMetricsLog.Printf("Extracting MCP failures from run: %d", run.DatabaseID)
	var mcpFailures []MCPFailureReport

	// Look for agent output logs that contain the system init entry with MCP server status
	// This information is available in the raw log files, typically with names containing "log"
	err := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Process log files - exclude output artifacts
		fileName := strings.ToLower(info.Name())
		if (strings.HasSuffix(fileName, ".log") ||
			(strings.HasSuffix(fileName, ".txt") && strings.Contains(fileName, "log"))) &&
			!strings.Contains(fileName, "aw_output") &&
			!strings.Contains(fileName, "agent_output") &&
			!strings.Contains(fileName, "access") {

			failures, parseErr := extractMCPFailuresFromLogFile(path, run, verbose)
			if parseErr != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse MCP failures from %s: %v", filepath.Base(path), parseErr)))
				}
				return nil // Continue processing other files
			}
			mcpFailures = append(mcpFailures, failures...)
		}

		return nil
	})

	if err != nil {
		return mcpFailures, fmt.Errorf("error walking run directory: %w", err)
	}

	if verbose && len(mcpFailures) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d MCP server failures for run %d", len(mcpFailures), run.DatabaseID)))
	}
	logsMetricsLog.Printf("Found %d MCP failures", len(mcpFailures))

	return mcpFailures, nil
}

// extractMCPFailuresFromLogFile parses a single log file for MCP server failures
func extractMCPFailuresFromLogFile(logPath string, run WorkflowRun, verbose bool) ([]MCPFailureReport, error) {
	var mcpFailures []MCPFailureReport

	content, err := os.ReadFile(logPath)
	if err != nil {
		return mcpFailures, fmt.Errorf("error reading log file: %w", err)
	}

	logContent := string(content)

	// First try to parse as JSON array
	var logEntries []map[string]any
	if err := json.Unmarshal(content, &logEntries); err == nil {
		// Successfully parsed as JSON array, process entries
		for _, entry := range logEntries {
			if entryType, ok := entry["type"].(string); ok && entryType == "system" {
				if subtype, ok := entry["subtype"].(string); ok && subtype == "init" {
					if mcpServers, ok := entry["mcp_servers"].([]any); ok {
						for _, serverInterface := range mcpServers {
							if server, ok := serverInterface.(map[string]any); ok {
								serverName, hasName := server["name"].(string)
								status, hasStatus := server["status"].(string)

								if hasName && hasStatus && status == "failed" {
									failure := MCPFailureReport{
										ServerName:   serverName,
										Status:       status,
										WorkflowName: run.WorkflowName,
										RunID:        run.DatabaseID,
									}

									// Try to extract timestamp if available
									if timestamp, hasTimestamp := entry["timestamp"].(string); hasTimestamp {
										failure.Timestamp = timestamp
									}

									mcpFailures = append(mcpFailures, failure)

									if verbose {
										fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Found MCP server failure: %s (status: %s)", serverName, status)))
									}
								}
							}
						}
					}
				}
			}
		}
	} else {
		// Fallback: Try to parse as JSON lines (Claude logs are typically NDJSON format)
		lines := strings.Split(logContent, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "{") {
				continue
			}

			// Try to parse each line as JSON
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue // Skip non-JSON lines
			}

			// Look for system init entries that contain MCP server information
			if entryType, ok := entry["type"].(string); ok && entryType == "system" {
				if subtype, ok := entry["subtype"].(string); ok && subtype == "init" {
					if mcpServers, ok := entry["mcp_servers"].([]any); ok {
						for _, serverInterface := range mcpServers {
							if server, ok := serverInterface.(map[string]any); ok {
								serverName, hasName := server["name"].(string)
								status, hasStatus := server["status"].(string)

								if hasName && hasStatus && status == "failed" {
									failure := MCPFailureReport{
										ServerName:   serverName,
										Status:       status,
										WorkflowName: run.WorkflowName,
										RunID:        run.DatabaseID,
									}

									// Try to extract timestamp if available
									if timestamp, hasTimestamp := entry["timestamp"].(string); hasTimestamp {
										failure.Timestamp = timestamp
									}

									mcpFailures = append(mcpFailures, failure)

									if verbose {
										fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Found MCP server failure: %s (status: %s)", serverName, status)))
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return mcpFailures, nil
}
