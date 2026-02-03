// This file provides command-line interface functionality for gh-aw.
// This file (logs_parsing_core.go) contains core log parsing functions
// for locating and extracting engine configuration from workflow logs.
//
// Key responsibilities:
//   - Parsing aw_info.json to extract engine configuration
//   - Locating agent log files and output artifacts
//   - Supporting multiple artifact layouts (before/after flattening)

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

var logsParsingCoreLog = logger.New("cli:logs_parsing_core")

// parseAwInfo reads and parses aw_info.json file, returning the parsed data
// Handles cases where aw_info.json is a file or a directory containing the actual file
func parseAwInfo(infoFilePath string, verbose bool) (*AwInfo, error) {
	// Sanitize the path to prevent path traversal attacks
	cleanPath := filepath.Clean(infoFilePath)
	logsParsingCoreLog.Printf("Parsing aw_info.json from: %s", cleanPath)
	var data []byte
	var err error

	// Check if the path exists and determine if it's a file or directory
	stat, statErr := os.Stat(cleanPath)
	if statErr != nil {
		logsParsingCoreLog.Printf("Failed to stat aw_info.json: %v", statErr)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to stat aw_info.json: %v", statErr)))
		}
		return nil, statErr
	}

	if stat.IsDir() {
		// It's a directory - look for nested aw_info.json
		nestedPath := filepath.Join(cleanPath, "aw_info.json")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("aw_info.json is a directory, trying nested file: %s", nestedPath)))
		}
		data, err = os.ReadFile(nestedPath)
	} else {
		// It's a regular file
		data, err = os.ReadFile(cleanPath)
	}

	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read aw_info.json: %v", err)))
		}
		return nil, err
	}

	var info AwInfo
	if err := json.Unmarshal(data, &info); err != nil {
		logsParsingCoreLog.Printf("Failed to unmarshal aw_info.json: %v", err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse aw_info.json: %v", err)))
		}
		return nil, err
	}

	logsParsingCoreLog.Printf("Successfully parsed aw_info.json with engine_id: %s", info.EngineID)
	return &info, nil
}

// extractEngineFromAwInfo reads aw_info.json and returns the appropriate engine
// Handles cases where aw_info.json is a file or a directory containing the actual file
func extractEngineFromAwInfo(infoFilePath string, verbose bool) workflow.CodingAgentEngine {
	logsParsingCoreLog.Printf("Extracting engine from aw_info.json: %s", infoFilePath)
	info, err := parseAwInfo(infoFilePath, verbose)
	if err != nil {
		return nil
	}

	if info.EngineID == "" {
		logsParsingCoreLog.Print("No engine_id found in aw_info.json")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No engine_id found in aw_info.json"))
		}
		return nil
	}

	registry := workflow.GetGlobalEngineRegistry()
	engine, err := registry.GetEngine(info.EngineID)
	if err != nil {
		logsParsingCoreLog.Printf("Unknown engine: %s", info.EngineID)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Unknown engine in aw_info.json: %s", info.EngineID)))
		}
		return nil
	}

	logsParsingCoreLog.Printf("Successfully extracted engine: %s", engine.GetID())
	return engine
}

// findAgentOutputFile searches for a file named agent_output.json within the logDir tree.
// Returns the first path found (depth-first) and a boolean indicating success.
func findAgentOutputFile(logDir string) (string, bool) {
	var foundPath string
	_ = filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() && strings.EqualFold(info.Name(), constants.AgentOutputArtifactName) {
			foundPath = path
			return errors.New("stop") // sentinel to stop walking early
		}
		return nil
	})
	if foundPath == "" {
		return "", false
	}
	return foundPath, true
}

// findAgentLogFile searches for agent logs within the logDir.
// It uses engine.GetLogFileForParsing() to determine which log file to use:
//   - If GetLogFileForParsing() returns a non-empty value that doesn't point to agent-stdio.log,
//     look for files in the "agent_output" artifact directory (before flattening)
//     or in the flattened location (after flattening)
//   - Otherwise, look for the "agent-stdio.log" artifact file
//
// Returns the first path found and a boolean indicating success.
func findAgentLogFile(logDir string, engine workflow.CodingAgentEngine) (string, bool) {
	// Use GetLogFileForParsing to determine which log file to use
	logFileForParsing := engine.GetLogFileForParsing()

	// If the engine specifies a log file that isn't the default agent-stdio.log,
	// look in the agent_output artifact directory or flattened location
	if logFileForParsing != "" && logFileForParsing != defaultAgentStdioLogPath {
		// Check for agent_output directory (artifact, before flattening)
		agentOutputDir := filepath.Join(logDir, "agent_output")
		if fileutil.DirExists(agentOutputDir) {
			// Find the first file in this directory
			var foundFile string
			_ = filepath.Walk(agentOutputDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info == nil {
					return nil
				}
				if !info.IsDir() && foundFile == "" {
					foundFile = path
					return errors.New("stop") // sentinel to stop walking early
				}
				return nil
			})
			if foundFile != "" {
				return foundFile, true
			}
		}

		// Check for flattened location (after flattening)
		// The engine's log path is absolute (e.g., /tmp/gh-aw/sandbox/agent/logs/)
		// After flattening, it's at logDir/sandbox/agent/logs/
		// Strip /tmp/gh-aw/ prefix to get the relative path
		const tmpGhAwPrefix = "/tmp/gh-aw/"
		if strings.HasPrefix(logFileForParsing, tmpGhAwPrefix) {
			relPath := strings.TrimPrefix(logFileForParsing, tmpGhAwPrefix)
			flattenedDir := filepath.Join(logDir, relPath)
			logsParsingCoreLog.Printf("Checking flattened location for logs: %s", flattenedDir)
			if fileutil.DirExists(flattenedDir) {
				// Find the first .log file in this directory
				var foundFile string
				_ = filepath.Walk(flattenedDir, func(path string, info os.FileInfo, err error) error {
					if err != nil || info == nil {
						return nil
					}
					if !info.IsDir() && strings.HasSuffix(info.Name(), ".log") && foundFile == "" {
						foundFile = path
						logsParsingCoreLog.Printf("Found session log file: %s", path)
						return errors.New("stop") // sentinel to stop walking early
					}
					return nil
				})
				if foundFile != "" {
					return foundFile, true
				}
			}
		}

		// Fallback: search recursively in logDir for session*.log or process*.log files
		// This handles cases where the artifact structure is different than expected
		// Note: Copilot changed from session-*.log to process-*.log naming convention
		logsParsingCoreLog.Printf("Searching recursively in %s for session*.log or process*.log files", logDir)
		var foundFile string
		_ = filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			// Look for session*.log or process*.log files
			fileName := info.Name()
			if !info.IsDir() && (strings.HasPrefix(fileName, "session") || strings.HasPrefix(fileName, "process")) && strings.HasSuffix(fileName, ".log") && foundFile == "" {
				foundFile = path
				logsParsingCoreLog.Printf("Found Copilot log file via recursive search: %s", path)
				return errors.New("stop") // sentinel to stop walking early
			}
			return nil
		})
		if foundFile != "" {
			return foundFile, true
		}
	}

	// Default to agent-stdio.log
	agentStdioLog := filepath.Join(logDir, "agent-stdio.log")
	if fileutil.FileExists(agentStdioLog) {
		return agentStdioLog, true
	}

	// Also check for nested agent-stdio.log in case it's in a subdirectory
	var foundPath string
	_ = filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() && info.Name() == "agent-stdio.log" {
			foundPath = path
			return errors.New("stop") // sentinel to stop walking early
		}
		return nil
	})
	if foundPath != "" {
		return foundPath, true
	}

	return "", false
}
