// This file provides shared helper functions for AI engine implementations.
//
// This file contains utilities used across multiple AI engine files (copilot_engine.go,
// claude_engine.go, codex_engine.go, custom_engine.go) to generate common workflow
// steps and configurations.
//
// # Organization Rationale
//
// These helper functions are grouped here because they:
//   - Are used by 3+ engine implementations (shared utilities)
//   - Provide common patterns for agent installation and secret validation
//   - Have a clear domain focus (engine workflow generation)
//   - Are stable and change infrequently
//
// This follows the helper file conventions documented in skills/developer/SKILL.md.
//
// # Key Functions
//
// Secret Validation:
//   - GenerateMultiSecretValidationStep() - Validate at least one of multiple secrets
//   - BuildDefaultSecretValidationStep() - Build secret validation step for an engine
//
// Configuration:
//   - FormatStepWithCommandAndEnv() - Format a step with command and environment variables
//   - FilterEnvForSecrets() - Filter environment variables to only include allowed secrets
//
// These functions encapsulate shared logic that would otherwise be duplicated across
// engine files, maintaining DRY principles while keeping engine-specific code separate.

package workflow

import (
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var engineHelpersLog = logger.New("workflow:engine_helpers")

// agentFilePathRegex is an allowlist of safe characters for agent file paths.
// Only alphanumeric characters, dots, underscores, hyphens, forward slashes, and
// spaces are permitted. This prevents shell metacharacters (such as ", $, `, ;, |)
// from being embedded in paths that are later interpolated into shell commands.
var agentFilePathRegex = regexp.MustCompile(`^[a-zA-Z0-9._/\- ]+$`)

// EngineInstallConfig contains configuration for engine installation steps.
// This struct centralizes the configuration needed to generate the common
// installation steps shared by all engines (secret validation and npm installation).
type EngineInstallConfig struct {
	// Secrets is a list of secret names to validate (at least one must be set)
	Secrets []string
	// DocsURL is the documentation URL shown when secret validation fails
	DocsURL string
	// NpmPackage is the npm package name (e.g., "@github/copilot")
	NpmPackage string
	// Version is the default version of the npm package
	Version string
	// Name is the engine display name for secret validation messages (e.g., "Claude Code")
	Name string
	// CliName is the CLI name used for cache key prefix (e.g., "copilot")
	CliName string
	// InstallStepName is the display name for the npm install step (e.g., "Install Claude Code CLI")
	InstallStepName string
}

// getEngineEnvOverrides returns the engine.env map from workflowData, or nil if not set.
// This is used to pass user-provided env overrides to steps such as secret validation,
// so that overridden token expressions are used instead of the default "${{ secrets.KEY }}".
func getEngineEnvOverrides(workflowData *WorkflowData) map[string]string {
	if workflowData == nil || workflowData.EngineConfig == nil {
		return nil
	}
	return workflowData.EngineConfig.Env
}

// ResolveEngineID returns the workflow engine ID, preferring engine.id over the legacy AI field.
// It returns an empty string when no engine ID is available.
func ResolveEngineID(workflowData *WorkflowData) string {
	if workflowData == nil {
		return ""
	}
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.ID != "" {
		return workflowData.EngineConfig.ID
	}
	return workflowData.AI
}

// engineEnvHasNonEmptyValue reports whether the given env var key is present in
// engine.env and has a non-empty (after trimming whitespace) value.
func engineEnvHasNonEmptyValue(workflowData *WorkflowData, key string) bool {
	if workflowData == nil || workflowData.EngineConfig == nil {
		return false
	}
	value, ok := workflowData.EngineConfig.Env[key]
	return ok && strings.TrimSpace(value) != ""
}

// applyEngineCwdEnv sets the GH_AW_ENGINE_CWD environment variable in the given env map
// when engine.cwd is configured. This variable is consumed by JS harness processes (via
// process_runner.cjs) and by shell-based engine command prefixes so the engine spawns in
// the user-specified working directory rather than the default GITHUB_WORKSPACE.
func applyEngineCwdEnv(env map[string]string, workflowData *WorkflowData) {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.Cwd == "" {
		return
	}
	env["GH_AW_ENGINE_CWD"] = workflowData.EngineConfig.Cwd
}

// applyEngineVersionEnv sets the GH_AW_ENGINE_VERSION environment variable in the given
// env map when engine.version is configured. This allows scripts and install helpers to
// read the user-specified engine version at runtime without parsing frontmatter directly.
// It is also useful for traceability when auditing which version of a custom engine ran.
func applyEngineVersionEnv(env map[string]string, workflowData *WorkflowData) {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.Version == "" {
		return
	}
	env["GH_AW_ENGINE_VERSION"] = workflowData.EngineConfig.Version
}

// applyOptionalEngineToolTimeouts adds optional tool timeout environment variables.
func applyOptionalEngineToolTimeouts(env map[string]string, workflowData *WorkflowData) {
	if workflowData == nil {
		return
	}
	if workflowData.ToolsStartupTimeout != "" {
		env["GH_AW_STARTUP_TIMEOUT"] = workflowData.ToolsStartupTimeout
	}
	if workflowData.ToolsTimeout != "" {
		env["GH_AW_TOOL_TIMEOUT"] = workflowData.ToolsTimeout
	}
}

// applyEngineMaxTurnsEnv sets GH_AW_MAX_TURNS from engine.max-turns or the default expression.
func applyEngineMaxTurnsEnv(env map[string]string, workflowData *WorkflowData) {
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
		env["GH_AW_MAX_TURNS"] = workflowData.EngineConfig.MaxTurns
		return
	}
	env["GH_AW_MAX_TURNS"] = compilerenv.BuildDefaultMaxTurnsExpression()
}

// applyEngineHarnessRetryEnv injects GH_AW_HARNESS_* environment variables from
// the engine frontmatter harness policy fields (engine.harness.max-retries, etc.).
// Only fields that are explicitly set are injected; absent fields let the harness
// fall back to its built-in defaults. Must be called before applyEngineAndAgentEnv
// so that explicit engine.env overrides take precedence.
func applyEngineHarnessRetryEnv(env map[string]string, workflowData *WorkflowData) {
	if workflowData == nil || workflowData.EngineConfig == nil {
		return
	}

	cfg := workflowData.EngineConfig
	if cfg.HarnessMaxRetries != "" {
		env["GH_AW_HARNESS_MAX_RETRIES"] = cfg.HarnessMaxRetries
	}
	if cfg.HarnessInitialDelayMs != "" {
		env["GH_AW_HARNESS_INITIAL_DELAY_MS"] = cfg.HarnessInitialDelayMs
	}
	if cfg.HarnessBackoffMultiplier != "" {
		env["GH_AW_HARNESS_BACKOFF_MULTIPLIER"] = cfg.HarnessBackoffMultiplier
	}
	if cfg.HarnessMaxDelayMs != "" {
		env["GH_AW_HARNESS_MAX_DELAY_MS"] = cfg.HarnessMaxDelayMs
	}
	if cfg.HarnessWatchdogTimeoutMs != "" {
		env["GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS"] = cfg.HarnessWatchdogTimeoutMs
	}
}

// buildShellHarnessCommand runs a shell command through the shared process runner.
// This gives engines with shell pipeline execution the same stall diagnostics as
// the dedicated JavaScript harnesses.
func buildShellHarnessCommand(engineName, command string) string {
	return fmt.Sprintf(
		`%s %s/shell_harness.cjs %s %s`,
		nodeRuntimeResolutionCommand,
		SetupActionDestinationShell,
		shellEscapeArg(engineName),
		shellEscapeArg(command),
	)
}

// applyEngineAndAgentEnv merges custom environment variables from engine and agent configs.
func applyEngineAndAgentEnv(env map[string]string, workflowData *WorkflowData, log *logger.Logger) {
	if workflowData == nil {
		return
	}
	applyPlaywrightBrowserEnv(env, workflowData)
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
		maps.Copy(env, workflowData.EngineConfig.Env)
	}
	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil && len(agentConfig.Env) > 0 {
		maps.Copy(env, agentConfig.Env)
		if log != nil {
			log.Printf("Added %d custom env vars from agent config", len(agentConfig.Env))
		}
	}

}

func applyPlaywrightBrowserEnv(env map[string]string, workflowData *WorkflowData) {
	if isPlaywrightCLIMode(workflowData.Tools) {
		env["PLAYWRIGHT_BROWSERS_PATH"] = playwrightBrowsersPath
	}
}

// applyMCPScriptsSecretEnv appends mcp-scripts secrets unless already present.
func applyMCPScriptsSecretEnv(env map[string]string, workflowData *WorkflowData) {
	if workflowData == nil {
		return
	}
	if !IsMCPScriptsEnabled(workflowData.MCPScripts) {
		return
	}
	mcpScriptsSecrets := collectMCPScriptsSecrets(workflowData.MCPScripts)
	for varName, secretExpr := range mcpScriptsSecrets {
		if _, exists := env[varName]; !exists {
			env[varName] = secretExpr
		}
	}
}

// GenerateMultiSecretValidationStep creates a GitHub Actions step that validates at least one
// of multiple secrets is available.
// secretNames: slice of secret names to validate (e.g., []string{"CODEX_API_KEY", "OPENAI_API_KEY"})
// engineName: the display name of the engine (e.g., "Codex")
// docsURL: URL to the documentation page for setting up the secret
// envOverrides: optional map of env var key to expression override (from engine.env); when set,
// the overridden expression is used instead of the default "${{ secrets.KEY }}" so the
// validation step checks the user-provided secret reference rather than the default one.
func GenerateMultiSecretValidationStep(secretNames []string, engineName, docsURL string, envOverrides map[string]string) GitHubActionStep {
	if len(secretNames) == 0 {
		// This is a programming error - engine configurations should always provide secrets
		// Log the error and return empty step to avoid breaking compilation
		engineHelpersLog.Printf("ERROR: GenerateMultiSecretValidationStep called with empty secretNames for engine %s", engineName)
		return GitHubActionStep{}
	}

	// Build the step name
	stepName := fmt.Sprintf("      - name: Validate %s secret", strings.Join(secretNames, " or "))

	// Build the command to call the validation script
	// The script expects: SECRET_NAME1 [SECRET_NAME2 ...] ENGINE_NAME DOCS_URL
	// Use shellJoinArgs to properly escape multi-word engine names and special characters
	scriptArgs := append(secretNames, engineName, docsURL)
	scriptArgsStr := shellJoinArgs(scriptArgs)

	stepLines := []string{
		stepName,
		"        id: validate-secret",
		"        run: bash \"${RUNNER_TEMP}/gh-aw/actions/validate_multi_secret.sh\" " + scriptArgsStr,
		"        env:",
	}

	// Add env section with all secrets. When engine.env provides an override for a key,
	// use that expression (e.g. "${{ secrets.MY_ORG_TOKEN }}") so the validation step
	// validates the user-supplied secret instead of the default one.
	for _, secretName := range secretNames {
		expr := fmt.Sprintf("${{ secrets.%s }}", secretName)
		if envOverrides != nil {
			if override, ok := envOverrides[secretName]; ok {
				expr = override
			}
		}
		stepLines = appendEnvVarLine(stepLines, secretName, expr)
	}

	return GitHubActionStep(stepLines)
}

// EngineSecretValidationConfig describes how an engine validates its required
// authentication secrets.
type EngineSecretValidationConfig struct {
	SecretNames []string
	EngineName  string
	DocsURL     string
	Skip        func(*WorkflowData) bool
}

// BuildEngineSecretValidationStep applies an engine-specific skip policy and
// delegates rendering to BuildDefaultSecretValidationStep.
func BuildEngineSecretValidationStep(workflowData *WorkflowData, config EngineSecretValidationConfig) GitHubActionStep {
	if config.Skip != nil && config.Skip(workflowData) {
		return GitHubActionStep{}
	}
	if len(config.SecretNames) == 0 {
		return GitHubActionStep{}
	}
	return BuildDefaultSecretValidationStep(workflowData, config.SecretNames, config.EngineName, config.DocsURL)
}

// BuildDefaultSecretValidationStep returns a secret validation step for the given engine
// configuration, or an empty step when a custom command is specified. This consolidates
// the common guard+delegate pattern shared across all engine GetSecretValidationStep
// implementations.
//
// Parameters:
//   - workflowData: The workflow data (checked for custom command)
//   - secrets: The secret names to validate (e.g., []string{"ANTHROPIC_API_KEY"})
//   - name: The engine display name used in the step (e.g., "Claude Code")
//   - docsURL: The documentation URL shown when validation fails
//
// Returns:
//   - GitHubActionStep: The validation step, or an empty step if a custom command is set
func BuildDefaultSecretValidationStep(workflowData *WorkflowData, secrets []string, name, docsURL string) GitHubActionStep {
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		engineHelpersLog.Printf("Skipping secret validation step: custom command specified (%s)", workflowData.EngineConfig.Command)
		return GitHubActionStep{}
	}
	if workflowData != nil && strings.TrimSpace(workflowData.Environment) != "" {
		engineHelpersLog.Print("Skipping secret validation step: top-level environment is configured")
		return GitHubActionStep{}
	}
	return GenerateMultiSecretValidationStep(secrets, name, docsURL, getEngineEnvOverrides(workflowData))
}

// collectCommonMCPSecrets returns the MCP-related secret names shared across all engines:
//   - MCP_GATEWAY_AGENT_ID (when MCP servers are present)
//   - mcp-scripts secrets (when mcp-scripts feature is enabled)
//
// Parameters:
//   - workflowData: The workflow data used to check MCP server and mcp-scripts configuration
//
// Returns:
//   - []string: Common MCP secret names (may be empty)
func collectCommonMCPSecrets(workflowData *WorkflowData) []string {
	var secrets []string

	if HasMCPServers(workflowData) {
		secrets = append(secrets, "MCP_GATEWAY_AGENT_ID")
	}

	if IsMCPScriptsEnabled(workflowData.MCPScripts) {
		mcpScriptsSecrets := collectMCPScriptsSecrets(workflowData.MCPScripts)
		for varName := range mcpScriptsSecrets {
			secrets = append(secrets, varName)
		}
	}

	return secrets
}

// RenderCustomMCPToolConfigHandler is a function type that engines must provide to render their specific MCP config
// FormatStepWithCommandAndEnv formats a GitHub Actions step with command and environment variables.
// This shared function extracts the common pattern used by Copilot and Codex engines.
//
// Parameters:
//   - stepLines: Existing step lines to append to (e.g., name, id, comments, timeout)
//   - command: The command to execute (may contain multiple lines)
//   - env: Map of environment variables to include in the step
//
// Returns:
//   - []string: Complete step lines including run command and env section
func FormatStepWithCommandAndEnv(stepLines []string, command string, env map[string]string) []string {
	engineHelpersLog.Printf("Formatting step with command and %d environment variables", len(env))
	// Add the run section
	stepLines = append(stepLines, "        run: |")

	// Split command into lines and indent them properly
	commandLines := strings.SplitSeq(command, "\n")
	for line := range commandLines {
		// Don't add indentation to empty lines
		if line == "" {
			stepLines = append(stepLines, "")
		} else {
			stepLines = append(stepLines, "          "+line)
		}
	}

	// Add environment variables
	if len(env) > 0 {
		stepLines = append(stepLines, "        env:")
		for _, key := range sliceutil.SortedKeys(env) {
			value := env[key]
			stepLines = appendEnvVarLine(stepLines, key, value)
		}
	}

	return stepLines
}

const agentExecutionExitCodePath = "/tmp/gh-aw/agent_execution_exit_code.txt"

// buildAgentExecutionExitCodeTrap returns an EXIT trap that persists an agent
// execution result for OTLP reporting and annotates non-zero exits in the log.
func buildAgentExecutionExitCodeTrap() string {
	return buildAgentExecutionExitCodeTrapWithCleanup("")
}

func buildAgentExecutionExitCodeTrapWithCleanup(cleanup string) string {
	if cleanup != "" {
		cleanup += "; "
	}
	return fmt.Sprintf(
		"trap 'gh_aw_exit_code=$?; mkdir -p /tmp/gh-aw >/dev/null 2>&1 || true; printf \"%%s\" \"$gh_aw_exit_code\" > %s || true; %sif [ \"$gh_aw_exit_code\" -ne 0 ]; then echo \"::error::Agent execution exited with code $gh_aw_exit_code\"; fi' EXIT\n",
		agentExecutionExitCodePath,
		cleanup,
	)
}

func wrapAgentExecutionCommand(command string) string {
	const pipefailSetup = "set -o pipefail\n"
	if commandWithoutPipefail, ok := strings.CutPrefix(command, pipefailSetup); ok {
		return pipefailSetup + buildAgentExecutionExitCodeTrap() + commandWithoutPipefail
	}
	return buildAgentExecutionExitCodeTrap() + command
}

// appendEnvVarLine appends a YAML env var entry to lines.
// If the value contains embedded newlines (e.g. from a multi-line YAML block scalar
// like >- with extra-indented continuation lines), it is emitted as a YAML literal
// block scalar (|) with proper indentation. At most one trailing newline (produced
// by block scalars) is trimmed before processing; multiple intentional trailing
// newlines are preserved.
func appendEnvVarLine(lines []string, key, value string) []string {
	// Trim at most one trailing newline added by YAML | or > block scalars.
	// Using TrimSuffix (not TrimRight) to avoid stripping multiple trailing
	// newlines that may be intentional in the value.
	value = strings.TrimSuffix(value, "\n")

	if !strings.Contains(value, "\n") {
		// Single-line: emit inline with YAML-safe quoting
		return append(lines, fmt.Sprintf("          %s: %s", key, yamlStringValue(value)))
	}

	// Multi-line: emit as a literal block scalar so embedded newlines are preserved
	lines = append(lines, fmt.Sprintf("          %s: |", key))
	for line := range strings.SplitSeq(value, "\n") {
		lines = append(lines, "            "+line)
	}
	return lines
}

// yamlStringValue returns a YAML-safe representation of a string value.
// If the value starts with a YAML flow indicator ('{' or '[') or other characters
// that would cause it to be misinterpreted by YAML parsers, it wraps the value
// in single quotes. Any embedded single quotes are escaped by doubling them (' becomes ”).
func yamlStringValue(value string) string {
	if value == "" {
		return value
	}
	if quoted := quoteYAMLValueContainingColonSpace(value); quoted != value {
		return quoted
	}
	// Values starting with YAML flow indicators need quoting to be treated as strings.
	// '{' would be parsed as a YAML flow mapping, '[' as a YAML flow sequence.
	first := value[0]
	if first != '{' && first != '[' {
		return value
	}
	// Single-quote the value, escaping any embedded single quotes by doubling them.
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// FilterEnvForSecrets filters environment variables to only include allowed secrets.
// This is a security measure to ensure that only necessary secrets are passed to the execution step.
//
// An env var carrying a secret reference is kept when either:
//   - The referenced secret name (e.g. "COPILOT_GITHUB_TOKEN") is in allowedNamesAndKeys, OR
//   - The env var key itself (e.g. "COPILOT_GITHUB_TOKEN") is in allowedNamesAndKeys.
//
// The second rule allows users to override an engine's required env var with a
// differently-named secret, e.g. COPILOT_GITHUB_TOKEN: ${{ secrets.MY_ORG_TOKEN }}.
//
// Parameters:
//   - env: Map of all environment variables
//   - allowedNamesAndKeys: List of secret names and/or env var keys that are permitted
//
// Returns:
//   - map[string]string: Filtered environment variables with only allowed secrets
func FilterEnvForSecrets(env map[string]string, allowedNamesAndKeys []string) map[string]string {
	engineHelpersLog.Printf("Filtering environment variables: total=%d, allowed=%d", len(env), len(allowedNamesAndKeys))

	// Create a set for fast lookup — entries may be secret names or env var keys.
	allowedSet := make(map[string]struct {
	})
	for _, entry := range allowedNamesAndKeys {
		allowedSet[entry] = struct {
		}{}
	}

	filtered := make(map[string]string)
	secretsRemoved := 0

	for key, value := range env {
		// Check if this env var is a secret reference (starts with "${{ secrets.")
		if strings.Contains(value, "${{ secrets.") {
			// Extract the secret name from the expression
			// Format: ${{ secrets.SECRET_NAME }} or ${{ secrets.SECRET_NAME || ... }}
			secretName := ExtractSecretName(value)
			// Allow the secret if the secret name OR the env var key is in the allowed set.
			if secretName != "" && !setutil.Contains(allowedSet, secretName) && !setutil.Contains(allowedSet, key) {
				engineHelpersLog.Printf("Removing unauthorized secret from env: %s (secret: %s)", key, secretName)
				secretsRemoved++
				continue
			}
		}
		filtered[key] = value
	}

	engineHelpersLog.Printf("Filtered environment variables: kept=%d, removed=%d", len(filtered), secretsRemoved)
	return filtered
}

// normalizeBashCommand strips the trailing " *" wildcard suffix from a bash
// tool command, converting patterns like "jq *" or "gh issue list *" to their
// canonical prefix form ("jq", "gh issue list"). All agentic engines use the
// canonical form so that their respective prefix-matching semantics permit any
// invocation of the command (e.g. shell(jq), Bash(jq), run_shell_command(jq)).
//
// The full-wildcard sentinels "*" and ":*" are handled separately by each
// engine before reaching this function, so only per-command entries of the
// form "<cmd> *" arrive here.
//
// Returns the normalized command and whether normalization was applied.
func normalizeBashCommand(cmdStr string) (string, bool) {
	if after, found := strings.CutSuffix(cmdStr, " *"); found {
		return after, true
	}
	return cmdStr, false
}
