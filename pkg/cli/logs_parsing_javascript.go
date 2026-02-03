// This file provides command-line interface functionality for gh-aw.
// This file (logs_parsing_js.go) contains functionality for executing
// JavaScript log parsers to generate markdown summaries.
//
// Key responsibilities:
//   - Running JavaScript log parser scripts
//   - Mocking GitHub Actions environment for parsers
//   - Generating markdown log summaries

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsParsingJsLog = logger.New("cli:logs_parsing_js")

// parseAgentLog parses agent logs and generates a markdown summary
func parseAgentLog(runDir string, engine workflow.CodingAgentEngine, verbose bool) error {
	logsParsingJsLog.Printf("Parsing agent logs in: %s", runDir)
	// Determine which parser script to use based on the engine
	if engine == nil {
		logsParsingJsLog.Print("No engine detected, skipping log parsing")
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No engine detected in %s, skipping log parsing", filepath.Base(runDir))))
		return nil
	}

	// Find the agent log file - use engine.GetLogFileForParsing() to determine location
	agentLogPath, found := findAgentLogFile(runDir, engine)
	if !found {
		logsParsingJsLog.Print("No agent log file found")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No agent logs found in %s, skipping log parsing", filepath.Base(runDir))))
		return nil
	}

	logsParsingJsLog.Printf("Found agent log file: %s", agentLogPath)

	parserScriptName := engine.GetLogParserScriptId()
	if parserScriptName == "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No log parser available for engine %s in %s, skipping", engine.GetID(), filepath.Base(runDir))))
		return nil
	}

	jsScript := workflow.GetLogParserScript(parserScriptName)
	if jsScript == "" {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to get log parser script %s", parserScriptName)))
		}
		return nil
	}

	// Read the log content
	logContent, err := os.ReadFile(agentLogPath)
	if err != nil {
		return fmt.Errorf("failed to read agent log file: %w", err)
	}

	// Create a temporary directory for running the parser
	tempDir, err := os.MkdirTemp("", "log_parser")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Write the log content to a temporary file
	logFile := filepath.Join(tempDir, "agent.log")
	if err := os.WriteFile(logFile, logContent, 0644); err != nil {
		return fmt.Errorf("failed to write log file: %w", err)
	}

	// Write the bootstrap helper to the temp directory
	bootstrapScript := workflow.GetLogParserBootstrap()
	if bootstrapScript != "" {
		bootstrapFile := filepath.Join(tempDir, "log_parser_bootstrap.cjs")
		if err := os.WriteFile(bootstrapFile, []byte(bootstrapScript), 0644); err != nil {
			return fmt.Errorf("failed to write bootstrap file: %w", err)
		}
	}

	// Write the shared helper to the temp directory
	sharedScript := workflow.GetJavaScriptSources()["log_parser_shared.cjs"]
	if sharedScript != "" {
		sharedFile := filepath.Join(tempDir, "log_parser_shared.cjs")
		if err := os.WriteFile(sharedFile, []byte(sharedScript), 0644); err != nil {
			return fmt.Errorf("failed to write shared helper file: %w", err)
		}
	}

	// Create a Node.js script that mimics the GitHub Actions environment
	nodeScript := fmt.Sprintf(`
const fs = require('fs');

// Mock @actions/core for the parser
const core = {
	summary: {
		addRaw: function(content) {
			this._content = content;
			return this;
		},
		write: function() {
			console.log(this._content);
		},
		_content: ''
	},
	setFailed: function(message) {
		console.error('FAILED:', message);
		process.exit(1);
	},
	info: function(message) {
		// Silent in CLI mode
	}
};

// Set global core for the parser scripts
global.core = core;

// Set up environment
process.env.GH_AW_AGENT_OUTPUT = '%s';

// Execute the parser script
%s
`, logFile, jsScript)

	// Write the Node.js script
	nodeFile := filepath.Join(tempDir, "parser.js")
	if err := os.WriteFile(nodeFile, []byte(nodeScript), 0644); err != nil {
		return fmt.Errorf("failed to write node script: %w", err)
	}

	// Execute the Node.js script
	cmd := exec.Command("node", "parser.js")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute parser script: %w\nOutput: %s", err, string(output))
	}

	// Write the output to log.md in the run directory
	logMdPath := filepath.Join(runDir, "log.md")
	if err := os.WriteFile(logMdPath, []byte(strings.TrimSpace(string(output))), 0644); err != nil {
		return fmt.Errorf("failed to write log.md: %w", err)
	}

	return nil
}
