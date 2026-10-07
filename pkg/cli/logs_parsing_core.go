// This file provides command-line interface functionality for gh-aw.
// This file (logs_parsing_core.go) contains core log parsing functions
// for extracting engine configuration from workflow logs.
//
// Key responsibilities:
//   - Parsing aw_info.json to extract engine configuration
//   - Registering the errWalkStop sentinel used by walk-based helpers in this package

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsParsingCoreLog = logger.New("cli:logs_parsing_core")

// errWalkStop is a sentinel returned from filepath.Walk callbacks to stop traversal early.
// It is shared across all walk-based file-search functions in this package.
var errWalkStop = errors.New("stop")

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
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("aw_info.json is a directory, trying nested file: "+nestedPath))
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
		if localEngine, localErr := loadLocalLogParserEngine(info.EngineID); localErr == nil && localEngine != nil {
			return localEngine
		} else if localErr != nil {
			logsParsingCoreLog.Printf("Failed to load local engine definition: %v", localErr)
		}
		logsParsingCoreLog.Printf("Unknown engine: %s", info.EngineID)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Unknown engine in aw_info.json: "+info.EngineID))
		}
		return nil
	}

	logsParsingCoreLog.Printf("Successfully extracted engine: %s", engine.GetID())
	return engine
}
