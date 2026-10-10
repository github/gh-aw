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
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsMetricsLog = logger.New("cli:logs_metrics")

// Shared utilities are now in workflow package
// extractJSONMetrics is available as an alias
var extractJSONMetrics = workflow.ExtractJSONMetrics

func buildReportProvenance(run WorkflowRun, timestamp, experimentName, variant string) ReportProvenance {
	return ReportProvenance{
		Timestamp:      timestamp,
		WorkflowName:   run.WorkflowName,
		RunID:          run.DatabaseID,
		ExperimentName: experimentName,
		Variant:        variant,
	}
}

// extractLogMetrics extracts metrics from downloaded log files
// workflowPath is optional and can be provided to help detect GitHub Copilot coding agent runs
func extractLogMetrics(logDir string, verbose bool, workflowPath ...string) (LogMetrics, error) {
	logsMetricsLog.Printf("Extracting log metrics from: %s", logDir)
	var metrics LogMetrics
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Beginning metric extraction in "+logDir))
	}

	// First check if this is a GitHub Copilot coding agent run (not Copilot CLI)
	var detector *CopilotCodingAgentDetector
	if len(workflowPath) > 0 && workflowPath[0] != "" {
		detector = NewCopilotCodingAgentDetectorWithPath(logDir, verbose, workflowPath[0])
	} else {
		detector = NewCopilotCodingAgentDetector(logDir, verbose)
	}
	isGitHubCopilotCodingAgent := detector.IsGitHubCopilotCodingAgent()
	logsMetricsLog.Printf("GitHub Copilot coding agent detected: %v", isGitHubCopilotCodingAgent)

	if isGitHubCopilotCodingAgent && verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Detected GitHub Copilot coding agent run, using specialized parser"))
	}

	// First check for aw_info.json to determine the engine
	var detectedEngine workflow.CodingAgentEngine
	infoFilePath := filepath.Join(logDir, "aw_info.json")
	logsMetricsLog.Printf("Checking for aw_info.json at: %s", infoFilePath)
	if fileutil.FileExists(infoFilePath) {
		logsMetricsLog.Print("Found aw_info.json, extracting engine")
		// aw_info.json exists, try to extract engine information
		if engine := extractEngineFromAwInfo(infoFilePath, verbose); engine != nil {
			detectedEngine = engine
			logsMetricsLog.Printf("Detected engine: %s", engine.GetID())
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Detected engine from aw_info.json: "+engine.GetID()))
			}
		} else {
			logsMetricsLog.Print("Failed to extract engine from aw_info.json")
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("aw_info.json exists but failed to extract engine"))
			}
		}
	} else {
		if _, statErr := os.Stat(infoFilePath); statErr != nil {
			logsMetricsLog.Printf("No aw_info.json found at %s: %v", infoFilePath, statErr)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No aw_info.json found at %s: %v", infoFilePath, statErr)))
			}
		} else {
			// Path exists but is not a regular file (for example, a directory).
			logsMetricsLog.Printf("No aw_info.json file found at %s", infoFilePath)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No aw_info.json file found at "+infoFilePath))
			}
		}
	}

	// Check for safe_output.jsonl artifact file
	awOutputPath := filepath.Join(logDir, "safe_output.jsonl")
	if fileutil.FileExists(awOutputPath) {
		if verbose {
			// Report that the agentic output file was found
			fileInfo, statErr := os.Stat(awOutputPath)
			if statErr == nil {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found agentic output file: safe_output.jsonl (%s)", console.FormatFileSize(fileInfo.Size()))))
			}
		}
	}

	// Check for aw-*.patch and aw-*.bundle artifact files (branch-named patches/bundles)
	if dirEntries, err := os.ReadDir(logDir); err == nil {
		for _, entry := range dirEntries {
			name := entry.Name()
			isPatch, _ := filepath.Match("aw-*.patch", name)
			isBundle, _ := filepath.Match("aw-*.bundle", name)
			if isPatch || isBundle {
				if verbose {
					filePath := filepath.Join(logDir, name)
					if fileInfo, statErr := os.Stat(filePath); statErr == nil {
						fileType := "git patch"
						if isBundle {
							fileType = "git bundle"
						}
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %s file: %s (%s)", fileType, name, console.FormatFileSize(fileInfo.Size()))))
					}
				}
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
			rootCopy := filepath.Join(logDir, constants.AgentOutputArtifactName.String())
			if _, err := os.Stat(rootCopy); errors.Is(err, os.ErrNotExist) {
				if copyErr := fileutil.CopyFile(agentOutputPath, rootCopy); copyErr == nil && verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Copied agent_output.json to run root for easy access"))
				}
			}
		}
	}

	// Try events.jsonl first – it provides a precise, structured event list from the Copilot CLI
	// session state and is the most reliable source for tool calls, turns, and usage metrics.
	// Fall back to walking .log files if events.jsonl is not present or cannot be parsed.
	var walkErr error
	eventsJSONLParsed := false
	if eventsJSONLPath := findEventsJSONLFile(logDir); eventsJSONLPath != "" {
		if verbose {
			fileInfo, statErr := os.Stat(eventsJSONLPath)
			if statErr == nil {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found events.jsonl (%s), using as primary metrics source", console.FormatFileSize(fileInfo.Size()))))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found events.jsonl, using as primary metrics source"))
			}
		}
		eventsMetrics, eventsErr := parseEventsJSONLMetrics(eventsJSONLPath, verbose)
		if eventsErr == nil {
			metrics = eventsMetrics
			eventsJSONLParsed = true
			logsMetricsLog.Printf("events.jsonl parsed: turns=%d tokens=%d toolCalls=%d",
				metrics.Turns, metrics.TokenUsage, len(metrics.ToolCalls))
		} else {
			logsMetricsLog.Printf("Failed to parse events.jsonl, falling back to log files: %v", eventsErr)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse events.jsonl: %v", eventsErr)))
			}
		}
	}

	// Walk through all .log files when events.jsonl was not available or failed to parse
	if !eventsJSONLParsed {
		walkErr = filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Skip directories. workflow-logs/ is explicitly excluded because it contains
			// GitHub Actions runner captures of each job/step's stdout rather than the agent
			// artifact data. Parsing those files would double-count token usage and turns;
			// the same agent session output appears in both the agent artifact
			// (e.g. agent-stdio.log) and the workflow run logs (workflow-logs/).
			if info.IsDir() {
				if info.Name() == "workflow-logs" {
					logsMetricsLog.Printf("Skipping workflow-logs directory (GHA runner logs, not agent metrics): %s", path)
					return filepath.SkipDir
				}
				return nil
			}

			// Process log files - exclude output artifacts like aw_output.txt and agent_output.json
			fileName := strings.ToLower(info.Name())
			if (strings.HasSuffix(fileName, ".log") ||
				(strings.HasSuffix(fileName, ".txt") && strings.Contains(fileName, "log"))) &&
				!strings.Contains(fileName, "aw_output") &&
				fileName != constants.AgentOutputFilename.String() {

				fileMetrics, err := parseLogFileWithEngine(path, detectedEngine, isGitHubCopilotCodingAgent, verbose)
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
	}

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
	} else if gatewayErr != nil && !errorutil.IsNotFoundError(gatewayErr) {
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
	return metrics, walkErr
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

// extractMissingToolsFromRun extracts missing tool reports from a workflow run's artifacts.
// experimentName and variant are the pre-resolved experiment assignment for this run; pass empty
// strings when no experiment context is available.
func extractMissingToolsFromRun(runDir string, run WorkflowRun, verbose bool, experimentName, variant string) ([]MissingToolReport, error) {
	logsMetricsLog.Printf("Extracting missing tools from run: %d", run.DatabaseID)
	var missingTools []MissingToolReport

	// Look for the safe output artifact file that contains structured JSON with items array
	// This file is created by the collect_ndjson_output.cjs script during workflow execution
	// After artifact refactoring, the file is flattened to agent_output.json at root
	agentOutputJSONPath := filepath.Join(runDir, constants.AgentOutputFilename.String())

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
		agentOutputPath := filepath.Join(runDir, constants.AgentOutputArtifactName.String())
		if stat, err := os.Stat(agentOutputPath); err == nil {
			if stat.IsDir() {
				// Directory form – look for nested file
				nested := filepath.Join(agentOutputPath, constants.AgentOutputArtifactName.String())
				if fileutil.FileExists(nested) {
					resolvedAgentOutputFile = nested
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage("agent_output.json is a directory; using nested file "+nested))
					}
				} else if verbose {
					if _, nestedErr := os.Stat(nested); nestedErr != nil {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
							fmt.Sprintf("agent_output.json directory present but nested file missing: %v", nestedErr)))
					} else {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
							"agent_output.json directory present but nested path is not a file"))
					}
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
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found agent_output.json at "+found))
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
					Tool:             item.Tool,
					Reason:           item.Reason,
					Alternatives:     item.Alternatives,
					ReportProvenance: buildReportProvenance(run, item.Timestamp, experimentName, variant),
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

// extractNoopsFromRun extracts noop messages from a workflow run's artifacts.
// experimentName and variant are the pre-resolved experiment assignment for this run; pass empty
// strings when no experiment context is available.
func extractNoopsFromRun(runDir string, run WorkflowRun, verbose bool, experimentName, variant string) ([]NoopReport, error) {
	logsMetricsLog.Printf("Extracting noops from run: %d", run.DatabaseID)
	var noops []NoopReport

	// Look for the safe output artifact file that contains structured JSON with items array
	// This file is created by the collect_ndjson_output.cjs script during workflow execution
	// After artifact refactoring, the file is flattened to agent_output.json at root
	agentOutputJSONPath := filepath.Join(runDir, constants.AgentOutputFilename.String())

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
		agentOutputPath := filepath.Join(runDir, constants.AgentOutputArtifactName.String())
		if stat, err := os.Stat(agentOutputPath); err == nil {
			if stat.IsDir() {
				// Directory form – look for nested file
				nested := filepath.Join(agentOutputPath, constants.AgentOutputArtifactName.String())
				if fileutil.FileExists(nested) {
					resolvedAgentOutputFile = nested
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage("agent_output.json is a directory; using nested file "+nested))
					}
				} else if verbose {
					if _, nestedErr := os.Stat(nested); nestedErr != nil {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
							fmt.Sprintf("agent_output.json directory present but nested file missing: %v", nestedErr)))
					} else {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
							"agent_output.json directory present but nested path is not a file"))
					}
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
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found agent_output.json at "+found))
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
					Message:          item.Message,
					ReportProvenance: buildReportProvenance(run, item.Timestamp, experimentName, variant),
				}
				noops = append(noops, noop)

				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found noop entry: "+item.Message))
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

// extractMissingDataFromRun extracts missing data reports from a workflow run's artifacts.
// experimentName and variant are the pre-resolved experiment assignment for this run; pass empty
// strings when no experiment context is available.
func extractMissingDataFromRun(runDir string, run WorkflowRun, verbose bool, experimentName, variant string) ([]MissingDataReport, error) {
	logsMetricsLog.Printf("Extracting missing data from run: %d", run.DatabaseID)
	var missingData []MissingDataReport

	// Look for the safe output artifact file that contains structured JSON with items array
	// This file is created by the collect_ndjson_output.cjs script during workflow execution
	// After artifact refactoring, the file is flattened to agent_output.json at root
	agentOutputJSONPath := filepath.Join(runDir, constants.AgentOutputFilename.String())

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
		agentOutputPath := filepath.Join(runDir, constants.AgentOutputArtifactName.String())
		if stat, err := os.Stat(agentOutputPath); err == nil {
			if stat.IsDir() {
				// Directory form – look for nested file
				nested := filepath.Join(agentOutputPath, constants.AgentOutputArtifactName.String())
				if fileutil.FileExists(nested) {
					resolvedAgentOutputFile = nested
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage("agent_output.json is a directory; using nested file "+nested))
					}
				} else if verbose {
					if _, nestedErr := os.Stat(nested); nestedErr != nil {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
							fmt.Sprintf("agent_output.json directory present but nested file missing: %v", nestedErr)))
					} else {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
							"agent_output.json directory present but nested path is not a file"))
					}
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
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found agent_output.json at "+found))
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
					DataType:         item.DataType,
					Reason:           item.Reason,
					Context:          item.Context,
					Alternatives:     item.Alternatives,
					ReportProvenance: buildReportProvenance(run, item.Timestamp, experimentName, variant),
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

// extractMCPFailuresFromRun extracts MCP server failure reports from a workflow run's logs.
// experimentName and variant are the pre-resolved experiment assignment for this run; pass empty
// strings when no experiment context is available.
func extractMCPFailuresFromRun(runDir string, run WorkflowRun, verbose bool, experimentName, variant string) ([]MCPFailureReport, error) {
	logsMetricsLog.Printf("Extracting MCP failures from run: %d", run.DatabaseID)
	var mcpFailures []MCPFailureReport

	// Look for agent output logs that contain the system init entry with MCP server status
	// This information is available in the raw log files, typically with names containing "log"
	walkErr := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
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

			failures, parseErr := extractMCPFailuresFromLogFile(path, run, verbose, experimentName, variant)
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

	if walkErr != nil {
		return mcpFailures, fmt.Errorf("error walking run directory: %w", walkErr)
	}

	if verbose && len(mcpFailures) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d MCP server failures for run %d", len(mcpFailures), run.DatabaseID)))
	}
	logsMetricsLog.Printf("Found %d MCP failures", len(mcpFailures))

	return mcpFailures, nil
}

// extractMCPFailuresFromLogFile parses a single log file for MCP server failures
func extractMCPFailuresFromLogFile(logPath string, run WorkflowRun, verbose bool, experimentName, variant string) ([]MCPFailureReport, error) {
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
			processMCPFailureEntry(entry, run, verbose, experimentName, variant, &mcpFailures)
		}
	} else {
		// Fallback: Try to parse as JSON lines (Claude logs are typically NDJSON format)
		lines := strings.SplitSeq(logContent, "\n")
		for line := range lines {
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
			processMCPFailureEntry(entry, run, verbose, experimentName, variant, &mcpFailures)
		}
	}

	return mcpFailures, nil
}

func processMCPFailureEntry(entry map[string]any, run WorkflowRun, verbose bool, experimentName, variant string, mcpFailures *[]MCPFailureReport) {
	entryType, ok := typeutil.LookupString(entry, "type")
	if !ok || entryType != "system" {
		return
	}

	subtype, ok := typeutil.LookupString(entry, "subtype")
	if !ok || subtype != "init" {
		return
	}

	mcpServers, ok := entry["mcp_servers"].([]any)
	if !ok {
		return
	}

	timestamp, _ := typeutil.LookupString(entry, "timestamp") // Optional field, ignore if missing.

	for _, serverInterface := range mcpServers {
		server, ok := serverInterface.(map[string]any)
		if !ok {
			continue
		}

		serverName, hasName := typeutil.LookupString(server, "name")
		status, hasStatus := typeutil.LookupString(server, "status")
		if !hasName || !hasStatus || status != "failed" {
			continue
		}

		failure := MCPFailureReport{
			ServerName:       serverName,
			Status:           status,
			ReportProvenance: buildReportProvenance(run, timestamp, experimentName, variant),
		}

		*mcpFailures = append(*mcpFailures, failure)

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Found MCP server failure: %s (status: %s)", serverName, status)))
		}
	}
}

// extractSkillActivationsFromRun extracts skill invocation records from a workflow run.
// It applies to all skills in the workflow: Phase 1 reads explicit skill_invocation entries
// from agent_output.json; Phase 2 always runs to supplement with any additional skills
// detected in raw agent log files that were not already reported in Phase 1.
// agent_output entries take precedence when the same skill name appears in both sources.
// experimentName and variant are the pre-resolved experiment assignment for this run; pass
// empty strings when no experiment context is available.
func extractSkillActivationsFromRun(runDir string, run WorkflowRun, verbose bool, experimentName, variant string) ([]SkillActivation, error) {
	logsMetricsLog.Printf("Extracting skill activations from run: %d", run.DatabaseID)

	// Phase 1 – look for explicit skill_invocation items in agent_output.json.
	agentOutputActivations, err := extractSkillActivationsFromAgentOutput(runDir, run, verbose, experimentName, variant)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("Failed to read skill activations from agent output for run %d: %v", run.DatabaseID, err),
		))
	}

	// Phase 2 – scan raw agent log files for engine-specific patterns.
	// Always runs so that skills not covered by agent_output.json are also captured.
	// Skills already found in Phase 1 are skipped to avoid duplicates.
	logActivations, logErr := extractSkillActivationsFromLogFiles(runDir, run, verbose, experimentName, variant)
	if logErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("Failed to parse skill activations from logs for run %d: %v", run.DatabaseID, logErr),
		))
	}

	// Merge: agent_output entries take precedence; add log-parsed entries for skills
	// not already present in the agent_output results.
	seen := make(map[string]struct{}, len(agentOutputActivations))
	activations := make([]SkillActivation, 0, len(agentOutputActivations)+len(logActivations))
	for _, act := range agentOutputActivations {
		seen[act.Name] = struct{}{}
		activations = append(activations, act)
	}
	for _, act := range logActivations {
		if _, already := seen[act.Name]; !already {
			activations = append(activations, act)
		}
	}

	if verbose && len(activations) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
			fmt.Sprintf("Found %d skill activation(s) for run %d", len(activations), run.DatabaseID),
		))
	}
	logsMetricsLog.Printf("Found %d skill activation(s)", len(activations))
	return activations, nil
}

// extractSkillActivationsFromAgentOutput reads agent_output.json and returns any
// items whose type is "skill_invocation".
func extractSkillActivationsFromAgentOutput(runDir string, run WorkflowRun, verbose bool, experimentName, variant string) ([]SkillActivation, error) {
	var activations []SkillActivation

	resolvedFile := resolveAgentOutputFile(runDir, verbose)
	if resolvedFile == "" {
		return activations, nil
	}

	cleanPath := filepath.Clean(resolvedFile)
	content, readErr := os.ReadFile(cleanPath)
	if readErr != nil {
		return activations, readErr
	}

	var safeOutput struct {
		Items  []json.RawMessage `json:"items"`
		Errors []string          `json:"errors,omitempty"`
	}
	if err := json.Unmarshal(content, &safeOutput); err != nil {
		return activations, err
	}

	for _, itemRaw := range safeOutput.Items {
		var item struct {
			Type      string `json:"type"`
			Name      string `json:"name,omitempty"`
			Status    string `json:"status,omitempty"`
			Timestamp string `json:"timestamp,omitempty"`
		}
		if err := json.Unmarshal(itemRaw, &item); err != nil {
			continue
		}
		if item.Type != "skill_invocation" {
			continue
		}
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		status := item.Status
		if status == "" {
			status = "invoked"
		}
		act := SkillActivation{
			Name:             item.Name,
			Status:           status,
			Source:           "agent_output",
			ReportProvenance: buildReportProvenance(run, item.Timestamp, experimentName, variant),
		}
		activations = append(activations, act)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
				fmt.Sprintf("Found skill_invocation entry: %s (status: %s)", item.Name, status),
			))
		}
	}

	logsMetricsLog.Printf("Found %d skill activation(s) in agent output", len(activations))
	return activations, nil
}

// resolveAgentOutputFile returns the path to agent_output.json in runDir, trying
// the new flattened layout first and then the legacy nested layout.
// Returns an empty string when no file is found.
func resolveAgentOutputFile(runDir string, verbose bool) string {
	agentOutputJSONPath := filepath.Join(runDir, constants.AgentOutputFilename.String())
	if stat, err := os.Stat(agentOutputJSONPath); err == nil && !stat.IsDir() {
		return agentOutputJSONPath
	}

	// Legacy: agent-output directory
	agentOutputPath := filepath.Join(runDir, constants.AgentOutputArtifactName.String())
	if stat, err := os.Stat(agentOutputPath); err == nil {
		if stat.IsDir() {
			nested := filepath.Join(agentOutputPath, constants.AgentOutputArtifactName.String())
			if fileutil.FileExists(nested) {
				return nested
			}
			return ""
		}
		return agentOutputPath
	}

	// Recursive fallback
	if found, ok := findAgentOutputFile(runDir); ok {
		return found
	}
	return ""
}

// skillInvocationPatterns holds the regular expressions used to detect skill
// invocations in raw agent log lines.  They are compiled once at package init
// to avoid repeated allocations on every log-file scan.
//
// Supported patterns:
//  1. `skill(<name>)` — Copilot coding agent explicit invocation form.
//  2. `[skills] invoked: <name>` — structured log prefix (future/custom engines).
//  3. `Skill invoked: <name>` — human-readable form (future/custom engines).
var skillInvocationPatterns = []*skillPattern{
	{
		re:   regexp.MustCompile(`(?i)\bskill\(([A-Za-z0-9][A-Za-z0-9_.-]*)\)`),
		name: "skill(name)",
	},
	{
		re:   regexp.MustCompile(`(?i)\[skills\]\s+invoked:\s+([A-Za-z0-9][A-Za-z0-9_. -]*\S)`),
		name: "[skills] invoked: name",
	},
	{
		re:   regexp.MustCompile(`(?i)\bskill\s+invoked\s*:\s+([A-Za-z0-9][A-Za-z0-9_. -]*\S)`),
		name: "skill invoked: name",
	},
}

type skillPattern struct {
	re   *regexp.Regexp
	name string
}

// extractSkillActivationsFromLogFiles walks agent log files in runDir and
// returns SkillActivation records for every unique skill name found.
func extractSkillActivationsFromLogFiles(runDir string, run WorkflowRun, verbose bool, experimentName, variant string) ([]SkillActivation, error) {
	seen := make(map[string]struct{})
	var activations []SkillActivation

	walkErr := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Only inspect agent log files; skip output artifacts.
		fileName := strings.ToLower(info.Name())
		isLogFile := strings.HasSuffix(fileName, ".log") ||
			(strings.HasSuffix(fileName, ".txt") && strings.Contains(fileName, "log"))
		isOutputArtifact := strings.Contains(fileName, "aw_output") ||
			strings.Contains(fileName, "agent_output") ||
			strings.Contains(fileName, "access")
		if !isLogFile || isOutputArtifact {
			return nil
		}

		found, parseErr := extractSkillActivationsFromLogFile(path, run, verbose, seen, experimentName, variant)
		if parseErr != nil && verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				fmt.Sprintf("Failed to parse skill activations from %s: %v", filepath.Base(path), parseErr),
			))
			return nil
		}
		activations = append(activations, found...)
		return nil
	})

	return activations, walkErr
}

// extractSkillActivationsFromLogFile parses a single agent log file and returns
// SkillActivation records for each unique skill name found.  seen is used as a
// cross-file deduplication set so that the same skill is not reported twice.
func extractSkillActivationsFromLogFile(logPath string, run WorkflowRun, verbose bool, seen map[string]struct{}, experimentName, variant string) ([]SkillActivation, error) {
	content, err := os.ReadFile(logPath)
	if err != nil {
		return nil, err
	}

	var activations []SkillActivation
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, pat := range skillInvocationPatterns {
			matches := pat.re.FindStringSubmatch(line)
			if len(matches) < 2 {
				continue
			}
			skillName := strings.TrimSpace(matches[1])
			if skillName == "" {
				continue
			}
			if _, already := seen[skillName]; already {
				continue
			}
			seen[skillName] = struct{}{}
			act := SkillActivation{
				Name:             skillName,
				Status:           "invoked",
				Source:           "log_parse",
				ReportProvenance: buildReportProvenance(run, "", experimentName, variant),
			}
			activations = append(activations, act)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
					fmt.Sprintf("Detected skill invocation from log (%s): %s", pat.name, skillName),
				))
			}
		}
	}

	return activations, nil
}
