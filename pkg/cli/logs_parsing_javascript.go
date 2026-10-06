// This file provides command-line interface functionality for gh-aw.
// This file (logs_parsing_javascript.go) contains functionality for executing
// JavaScript log parsers to generate markdown summaries.
//
// Key responsibilities:
//   - Running JavaScript log parser scripts
//   - Sharing the session parsers used by workflow summaries
//   - Generating markdown log summaries

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsParsingJsLog = logger.New("cli:logs_parsing_js")

// parseAgentLog parses agent logs and generates a markdown summary
func parseAgentLog(runDir string, engine workflow.CodingAgentEngine, verbose bool) error {
	logsParsingJsLog.Printf("Parsing agent logs in: %s", runDir)
	if rendered, err := parsePersistedAgentLog(runDir); rendered || err != nil {
		return err
	}
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

	var output []byte
	var err error
	if behaviorEngine, ok := engine.(*workflow.BehaviorDefinedEngine); ok {
		if parserSource := behaviorEngine.GetLogParserScriptSource(); parserSource != "" {
			output, err = runSessionParserWithSources(
				context.Background(),
				map[string][]byte{"behavior_log_parser.cjs": []byte(parserSource)},
				"agent-markdown",
				agentLogPath,
				engine.GetID(),
				"behavior_log_parser.cjs",
			)
		} else {
			output, err = runSessionParser(context.Background(), "agent-markdown", agentLogPath, engine.GetID())
		}
	} else {
		output, err = runSessionParser(context.Background(), "agent-markdown", agentLogPath, engine.GetID())
	}
	if err != nil {
		return err
	}

	// Write the output to log.md in the run directory
	logMdPath := filepath.Join(runDir, "log.md")
	if err := os.WriteFile(logMdPath, []byte(strings.TrimSpace(string(output))), constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write log.md: %w", err)
	}

	return nil
}
