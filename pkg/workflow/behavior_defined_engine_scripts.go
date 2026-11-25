package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func (e *BehaviorDefinedEngine) RenderMCPConfig(sb *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error {
	// The rendered config is piped to start_mcp_gateway.cjs, which is what actually
	// launches the MCP gateway container. It must therefore be emitted even when the
	// engine declares no MCP config path (e.g. engines with `mcp: false` that consume
	// MCP-backed tools through cli-proxy): skipping it leaves the gateway container
	// unstarted while AWF still attempts to attach it to the internal network.
	configPath := constants.ShellMcpServersJsonPath
	if behavior := e.behavior(); behavior != nil && behavior.MCP != nil && behavior.MCP.ConfigPath != "" {
		configPath = behavior.MCP.ConfigPath
	}
	return renderDefaultJSONMCPConfig(sb, tools, mcpTools, workflowData, configPath)
}

// harnessScriptHeredocDelimiter is the shell heredoc delimiter used when writing
// the harness script to disk. It is intentionally long and project-specific so that
// it is extremely unlikely to appear at the start of a line in any JavaScript harness.
const harnessScriptHeredocDelimiter = "GHAW_HARNESS_SCRIPT_3c7b9f1a_EOF"

// mcpConfigAdapterHeredocDelimiter is the shell heredoc delimiter used when writing
// the MCP config-adapter script to disk. It is intentionally long and project-specific
// so that it is extremely unlikely to appear at the start of a line in any JavaScript.
const mcpConfigAdapterHeredocDelimiter = "GHAW_MCP_CONFIG_ADAPTER_SCRIPT_7e1a4d2c_EOF"

// harnessScriptFilename returns the filename (not path) for the engine's harness script.
func (e *BehaviorDefinedEngine) harnessScriptFilename() string {
	return e.GetID() + "_harness.cjs"
}

// mcpConfigAdapterFilename returns the filename (not path) for the engine's MCP
// config-adapter script.
func (e *BehaviorDefinedEngine) mcpConfigAdapterFilename() string {
	return e.GetID() + "_mcp_config_adapter.cjs"
}

// buildScriptWriteStep generates a GitHub Actions step that writes script content to
// ${RUNNER_TEMP}/gh-aw/actions/<filename> via a bash heredoc using delimiter. Returns
// nil and logs a warning if script contains the delimiter, which would break the
// generated shell command.
func (e *BehaviorDefinedEngine) buildScriptWriteStep(stepName, filename, script, delimiter string) GitHubActionStep {
	if script == "" {
		return nil
	}
	// Safety check: if the script contains the heredoc delimiter at the start
	// of any line, the heredoc would be terminated prematurely. Detect this at
	// compile time and log a clear error rather than generating a broken step.
	if strings.Contains(script, "\n"+delimiter) || strings.HasPrefix(script, delimiter) {
		behaviorDefinedEngineLog.Printf(
			"WARNING: engine %q script %q contains heredoc delimiter %q; write step skipped",
			e.GetID(), filename, delimiter,
		)
		return nil
	}
	command := fmt.Sprintf(
		"mkdir -p \"%[1]s\"\ncat <<'%[4]s' > \"%[1]s/%[2]s\"\n%[3]s\n%[4]s\nchmod 755 \"%[1]s/%[2]s\"", //nolint:generatedyamlheredoc // Legacy engine script rendering remains to be migrated.
		SetupActionDestinationShell,
		filename,
		script,
		delimiter,
	)
	stepLines := []string{"      - name: " + stepName}
	stepLines = FormatStepWithCommandAndEnv(stepLines, command, nil)
	return GitHubActionStep(stepLines)
}

// buildHarnessWriteStep generates a GitHub Actions step that writes the behavior-defined
// engine's harness-script content to ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_harness.cjs
// so it can be executed as a Node.js harness during the engine execution step.
// Returns nil and logs a warning if the harness script contains the heredoc delimiter,
// which would break the generated shell command.
func (e *BehaviorDefinedEngine) buildHarnessWriteStep() GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil || behavior.HarnessScript == "" {
		return nil
	}
	return e.buildScriptWriteStep(
		"Write "+e.GetDisplayName()+" harness script",
		e.harnessScriptFilename(),
		behavior.HarnessScript,
		harnessScriptHeredocDelimiter,
	)
}

// GetMCPConfigAdapterWriteStep generates a GitHub Actions step that writes the
// behavior-defined engine's mcp.config-adapter script content to
// ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_mcp_config_adapter.cjs so that
// start_mcp_gateway.cjs can execute it in place of a built-in per-engine converter.
// Returns nil (satisfying the MCPConfigAdapterProvider interface as a no-op) if the
// engine's behaviors do not declare a config-adapter script.
func (e *BehaviorDefinedEngine) GetMCPConfigAdapterWriteStep() GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil || behavior.MCP == nil || behavior.MCP.ConfigAdapter == "" {
		return nil
	}
	return e.buildScriptWriteStep(
		"Write "+e.GetDisplayName()+" MCP config adapter script",
		e.mcpConfigAdapterFilename(),
		behavior.MCP.ConfigAdapter,
		mcpConfigAdapterHeredocDelimiter,
	)
}

// GetMCPConfigAdapterFilename returns the filename (not path) of the engine's MCP
// config-adapter script, or an empty string if no config-adapter is declared.
func (e *BehaviorDefinedEngine) GetMCPConfigAdapterFilename() string {
	behavior := e.behavior()
	if behavior == nil || behavior.MCP == nil || behavior.MCP.ConfigAdapter == "" {
		return ""
	}
	return e.mcpConfigAdapterFilename()
}

// logParserHeredocDelimiter is the shell heredoc delimiter used when writing
// the log-parser script to disk.
const logParserHeredocDelimiter = "GHAW_LOG_PARSER_SCRIPT_5d2e8b3f_EOF"

// logParserScriptFilename returns the filename (not path) for the engine's log-parser script.
func (e *BehaviorDefinedEngine) logParserScriptFilename() string {
	return e.GetID() + "_log_parser"
}

// buildWrappedLogParserScript wraps the user-supplied log-parser source with the
// createEngineLogParser bootstrap and returns the complete script content.
// Returns "" if no log-parser is defined or if the wrapped script contains the
// heredoc delimiter (which would break the generated shell heredoc).
func (e *BehaviorDefinedEngine) buildWrappedLogParserScript() string {
	behavior := e.behavior()
	if behavior == nil || behavior.LogParser == "" {
		return ""
	}
	// Wrap the user-provided parse function with the createEngineLogParser
	// bootstrap so the script conforms to the contract expected by
	// log_parser_bootstrap.cjs (runLogParser). The author only needs to define
	// a parseLog(logContent) function; the wrapper handles file I/O and exports.
	wrapped := fmt.Sprintf(`// @ts-check
// Auto-generated log parser for behavior-defined engine %q.
const { createEngineLogParser } = require("./log_parser_shared.cjs");

// --- begin engine-provided parse function ---
%s
// --- end engine-provided parse function ---

const main = createEngineLogParser({
  parserName: %q,
  parseFunction: parseLog,
  supportsDirectories: false,
});

if (typeof module !== "undefined" && module.exports) {
  module.exports = { main, parseLog };
}`,
		e.GetDisplayName(),
		behavior.LogParser,
		e.GetDisplayName(),
	)
	// Safety check: if the wrapped script contains the heredoc delimiter at the
	// start of any line, the heredoc would be terminated prematurely. Detect
	// this at compile time so GetLogParserScriptId() and buildLogParserWriteStep()
	// are always consistent.
	if strings.Contains(wrapped, "\n"+logParserHeredocDelimiter) || strings.HasPrefix(wrapped, logParserHeredocDelimiter) {
		behaviorDefinedEngineLog.Printf(
			"WARNING: engine %q log-parser script contains heredoc delimiter %q; log-parser step suppressed",
			e.GetID(), logParserHeredocDelimiter,
		)
		return ""
	}
	return wrapped
}

// GetLogParserScriptId returns the log-parser script ID for this engine.
// Returns "" when no log-parser is defined, or when the wrapped script would
// contain the heredoc delimiter (which prevents the write step from running).
// This is always consistent with buildLogParserWriteStep: the ID is non-empty
// only when the write step will actually emit the script file.
func (e *BehaviorDefinedEngine) GetLogParserScriptId() string {
	if e.buildWrappedLogParserScript() == "" {
		return ""
	}
	return e.logParserScriptFilename()
}

// GetLogParserScriptSource returns the generated parser source for local parsing.
func (e *BehaviorDefinedEngine) GetLogParserScriptSource() string {
	return e.buildWrappedLogParserScript()
}

// buildLogParserWriteStep generates a GitHub Actions step that writes the
// behavior-defined engine's log-parser script to
// ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_log_parser.cjs so the post-agent
// log-parsing step can require() it.
//
// The raw log-parser JavaScript from the behavior definition is wrapped with a
// createEngineLogParser call from log_parser_shared.cjs so that the author only
// needs to define a parseLog(logContent) function returning
// {markdown, logEntries, mcpFailures, maxTurnsHit}.
//
// The step runs with "if: always()" so the script is written even if an earlier
// step in the job fails, ensuring the log-parsing step (which also runs always)
// can always require() the file.
func (e *BehaviorDefinedEngine) buildLogParserWriteStep() GitHubActionStep {
	script := e.buildWrappedLogParserScript()
	if script == "" {
		return nil
	}
	stepLines := []string{
		"      - name: Write " + e.GetDisplayName() + " log parser script",
		"        if: always()",
	}
	command := fmt.Sprintf(
		"mkdir -p \"%[1]s\"\ncat <<'%[4]s' > \"%[1]s/%[2]s\"\n%[3]s\n%[4]s\nchmod 755 \"%[1]s/%[2]s\"", //nolint:generatedyamlheredoc // Legacy log-parser rendering remains to be migrated.
		SetupActionDestinationShell,
		e.logParserScriptFilename()+".cjs",
		script,
		logParserHeredocDelimiter,
	)
	stepLines = FormatStepWithCommandAndEnv(stepLines, command, nil)
	return GitHubActionStep(stepLines)
}
