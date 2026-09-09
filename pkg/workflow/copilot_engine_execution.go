// This file provides Copilot engine execution logic.
//
// This file contains the GetExecutionSteps function which generates the complete
// GitHub Actions workflow for executing GitHub Copilot CLI. This is the largest
// and most complex function in the Copilot engine, handling:
//
//   - Copilot CLI argument construction based on sandbox mode (AWF, SRT, or standard)
//   - Tool permission configuration (--allow-tool flags)
//   - MCP server configuration and environment setup
//   - Sandbox wrapping (AWF or SRT)
//   - Environment variable handling for model selection and secrets
//   - Log file configuration and output collection
//
// The execution strategy varies significantly based on sandbox mode:
//   - Standard mode: Direct copilot CLI execution
//   - AWF mode: Wrapped with awf binary for network firewalling
//   - SRT mode: Wrapped with Sandbox Runtime for process isolation
//
// This function is intentionally kept in a separate file due to its size (~430 lines)
// and complexity. Future refactoring may split it further if needed.

package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var copilotExecLog = logger.New("workflow:copilot_engine_execution")

const customEngineCommandScriptPath = "/tmp/gh-aw/engine-command.sh"

// copilotExecutionStepName is the display name of the generated Copilot CLI execution step.
const copilotExecutionStepName = "Execute GitHub Copilot CLI"

// copilotSettingsPath is the shell expression that resolves to the Copilot CLI settings
// file at runtime. The Copilot CLI resolves its config directory as ~/.copilot, which is
// /home/runner/.copilot on standard GitHub-hosted runners (HOME=/home/runner) but may
// differ on self-hosted or containerized runners. HOME is a standard POSIX environment
// variable inherited from the runner's parent process and passed through to shell steps;
// other generators (copilot_mcp.go, mcp_setup_generator.go) rely on it the same way.
const copilotSettingsPath = "$HOME/.copilot/settings.json"

// copilotSettingsDefaultContent is the default JSON content written to the Copilot CLI
// settings file when no additional settings are configured.
const copilotSettingsDefaultContent = `{"builtInAgents":{"rubberDuck":false}}`

type copilotSettings struct {
	BuiltInAgents map[string]bool            `json:"builtInAgents"`
	LSPServers    map[string]LSPServerConfig `json:"lspServers,omitempty"`
}

func buildCopilotSettingsContent(workflowData *WorkflowData) string {
	settings := copilotSettings{
		BuiltInAgents: map[string]bool{"rubberDuck": false},
	}
	if workflowData != nil {
		manager := NewLSPManager(workflowData.LSP)
		settings.LSPServers = manager.CopilotLSPServers()
	}
	settingsBytes, err := json.Marshal(settings)
	if err != nil {
		return copilotSettingsDefaultContent
	}
	return string(settingsBytes)
}

// buildCopilotSettingsSetup returns shell commands that write the Copilot CLI settings
// file before the agent runs, disabling the rubber-duck sub-agent.
func buildCopilotSettingsSetup(settingsContent string, fixOwnershipForCustomCommand bool) string {
	if settingsContent == "" {
		settingsContent = copilotSettingsDefaultContent
	}
	setup := "mkdir -p \"$HOME/.copilot\"\n"
	if fixOwnershipForCustomCommand {
		setup += "sudo chown -R \"$(id -u):$(id -g)\" \"$HOME/.copilot\"\n"
	}
	return setup + fmt.Sprintf("printf '%%s' %s > \"%s\"\n",
		shellEscapeArg(settingsContent), copilotSettingsPath)
}

// buildCopilotSettingsCleanupAndExitCodeTrap adds Copilot settings cleanup to the
// shared agent execution exit-code trap.
//
// The body is single-quoted so $HOME in copilotSettingsPath is expanded at trap-fire
// time (matching buildCopilotSettingsCleanupTrap behavior).
func buildCopilotSettingsCleanupAndExitCodeTrap() string {
	return buildAgentExecutionExitCodeTrapWithCleanup(fmt.Sprintf(`rm -f "%s"`, copilotSettingsPath))
}

// buildCopilotMCPConfigExport returns shell commands that export Copilot-CLI-specific
// env vars whose values depend on the runtime $HOME (which may not be /home/runner on
// self-hosted or containerized runners). GitHub Actions does not shell-expand env:
// values, so these must be exported from the run script rather than the YAML env: block.
//
// Always exports:
//   - XDG_CONFIG_HOME=$HOME (Copilot CLI resolves its config dir from this)
//
// Exported only when the workflow has MCP servers:
//   - GH_AW_MCP_CONFIG=$HOME/.copilot/mcp-config.json
func buildCopilotMCPConfigExport(workflowData *WorkflowData) string {
	var b strings.Builder
	b.WriteString("export XDG_CONFIG_HOME=\"$HOME\"\n")
	if HasMCPServers(workflowData) {
		b.WriteString("export GH_AW_MCP_CONFIG=\"$HOME/.copilot/mcp-config.json\"\n")
	}
	return b.String()
}

const nodePathSetupCommand = `GH_AW_NPM_GLOBAL_ROOT="$(npm root -g 2>/dev/null || true)"; if [ -n "$GH_AW_NPM_GLOBAL_ROOT" ]; then export NODE_PATH="${GH_AW_NPM_GLOBAL_ROOT}${NODE_PATH:+:${NODE_PATH}}"; fi`
const nodeRuntimeResolutionCommand = `GH_AW_NODE_EXEC="${GH_AW_NODE_BIN:-}"; if [ -z "$GH_AW_NODE_EXEC" ] || [ ! -x "$GH_AW_NODE_EXEC" ]; then GH_AW_NODE_EXEC="$(command -v node 2>/dev/null || true)"; fi; if [ -z "$GH_AW_NODE_EXEC" ]; then echo "node runtime missing on this runner — check runtimes.node in workflow YAML" >&2; exit 127; fi; ` + nodePathSetupCommand + `; "$GH_AW_NODE_EXEC"`
const nodePathSetupCommandForCopilotSDK = `GH_AW_WORKSPACE_NODE_MODULES="${GITHUB_WORKSPACE:-$PWD}/node_modules"; if [ -d "$GH_AW_WORKSPACE_NODE_MODULES" ]; then export NODE_PATH="${GH_AW_WORKSPACE_NODE_MODULES}${NODE_PATH:+:${NODE_PATH}}"; fi; ` + nodePathSetupCommand
const nodeRuntimeResolutionCommandForCopilotSDK = `GH_AW_NODE_EXEC="${GH_AW_NODE_BIN:-}"; if [ -z "$GH_AW_NODE_EXEC" ] || [ ! -x "$GH_AW_NODE_EXEC" ]; then GH_AW_NODE_EXEC="$(command -v node 2>/dev/null || true)"; fi; if [ -z "$GH_AW_NODE_EXEC" ]; then echo "node runtime missing on this runner — check runtimes.node in workflow YAML" >&2; exit 127; fi; ` + nodePathSetupCommandForCopilotSDK + `; "$GH_AW_NODE_EXEC"`
const copilotBinaryPathSetup = `GH_AW_COPILOT_SRC="$(command -v copilot 2>/dev/null || true)"
if [ -z "$GH_AW_COPILOT_SRC" ] || [ ! -x "$GH_AW_COPILOT_SRC" ]; then
  echo "GitHub Copilot CLI executable not found on PATH after installation" >&2
  exit 127
fi
GH_AW_COPILOT_BIN="${RUNNER_TEMP}/gh-aw/bin/copilot"
mkdir -p "${RUNNER_TEMP}/gh-aw/bin"
if [ "$GH_AW_COPILOT_SRC" != "$GH_AW_COPILOT_BIN" ]; then
  cp "$GH_AW_COPILOT_SRC" "$GH_AW_COPILOT_BIN"
fi
chmod 755 "$GH_AW_COPILOT_BIN"
`
const copilotSDKPythonPathExpression = "${{ github.workspace }}/.gh-aw/copilot-sdk/python"

// copilotSDKDriverExecArgs returns the runtime command and driver path argument for the
// given SDK driver filename.
//
// For language scripts with a recognized extension, the runtime command is the appropriate
// language executor and driverArg is the driver filename (which the caller will prefix with
// SetupActionDestinationShell). For bare command names (no extension), the driver is treated
// as an arbitrary executable in PATH: runtimeCmd is the command itself and driverArg is empty.
//
//   - .js/.cjs/.mjs → ("$GH_AW_NODE_EXEC",  "driver.cjs")
//   - .py           → ("python3",             "driver.py")
//   - .ts/.mts      → ("$GH_AW_NODE_EXEC",   "driver.ts")
//   - .rb           → ("ruby",                "driver.rb")
//   - (no ext)      → ("my-driver",           "")
func copilotSDKDriverExecArgs(driverName string) (runtimeCmd, driverArg string) {
	ext := strings.ToLower(filepath.Ext(driverName))
	switch ext {
	case ".js", ".cjs", ".mjs":
		return `"$GH_AW_NODE_EXEC"`, driverName
	case ".py":
		return "python3", driverName
	case ".ts", ".mts":
		// Node 24 runs TypeScript natively; use the same node executor as .js drivers.
		return `"$GH_AW_NODE_EXEC"`, driverName
	case ".rb":
		return "ruby", driverName
	default:
		// No extension — arbitrary command in PATH; use name directly as command.
		return driverName, ""
	}
}

// copilotSDKRuntimeID returns the runtime ID used by Copilot SDK driver execution.
// It returns one of: python, typescript, ruby, or node (default/fallback).
// engine.command takes precedence; otherwise runtime is inferred from engine.driver extension.
func copilotSDKRuntimeID(workflowData *WorkflowData) string {
	if workflowData == nil || workflowData.EngineConfig == nil {
		return "node"
	}
	if runtimeID := copilotSDKInlineDriverRuntimeID(workflowData); runtimeID != "" {
		return runtimeID
	}
	command := workflowData.EngineConfig.Command
	if command != "" {
		return detectRuntimeFromCopilotCommand(command)
	}
	ext := strings.ToLower(filepath.Ext(workflowData.EngineConfig.Driver))
	switch ext {
	case ".py":
		return "python"
	case ".ts", ".mts":
		return "typescript"
	case ".rb":
		return "ruby"
	default:
		return "node"
	}
}

// GetExecutionSteps returns the GitHub Actions steps for executing GitHub Copilot CLI
func (e *CopilotEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	copilotExecLog.Printf("Generating execution steps for Copilot: workflow=%s, firewall=%v", workflowData.Name, isFirewallEnabled(workflowData))

	sandboxEnabled := isFirewallEnabled(workflowData)
	llmProvider := e.ResolveLLMProvider(workflowData)
	isBYOKMode := isCopilotBYOKMode(workflowData, sandboxEnabled)
	modelConfigured := workflowData.Model != ""
	copilotArgs, copilotToolArgs := e.buildCopilotArgs(workflowData)
	mkdirCommands := buildCopilotMkdirCommands(copilotArgs)
	modelEnvVar := getCopilotModelEnvVar(workflowData)
	timeoutValue := getCopilotTimeoutValue(workflowData)
	commandName, customCommandScriptSetup := e.resolveCopilotCommand(workflowData, sandboxEnabled)
	execPrefix := e.buildCopilotExecPrefix(workflowData, commandName)
	command, copilotSDKServerArgsJSON := e.buildCopilotCommand(
		workflowData, copilotArgs, execPrefix, customCommandScriptSetup, logFile, mkdirCommands, isBYOKMode,
	)
	copilotSDKToolConfigJSON := buildCopilotSDKToolConfigJSON(workflowData, copilotToolArgs)
	env := e.buildCopilotStepEnv(
		workflowData,
		llmProvider,
		modelEnvVar,
		timeoutValue,
		copilotStepEnvFlags{
			byokMode:        isBYOKMode,
			sandboxEnabled:  sandboxEnabled,
			modelConfigured: modelConfigured,
		},
		copilotSDKServerArgsJSON,
		copilotSDKToolConfigJSON,
	)

	return []GitHubActionStep{e.buildCopilotExecutionStep(workflowData, command, env, timeoutValue)}
}

// buildCopilotArgs builds the Copilot CLI argument list based on workflow configuration.
func (e *CopilotEngine) buildCopilotArgs(workflowData *WorkflowData) ([]string, []string) {
	sandboxEnabled := isFirewallEnabled(workflowData)
	isDetectionJob := isDetectionRun(workflowData)
	copilotArgs := e.buildCopilotBaseArgs(sandboxEnabled)

	// Disable Copilot CLI built-in MCP servers unless a workflow opts into
	// web-fetch. The CLI exposes web_fetch through its built-in tool schema, so
	// disabling built-ins would leave --allow-tool web_fetch with no callable tool.
	if !copilotNeedsBuiltinMCPs(workflowData) {
		copilotArgs = append(copilotArgs, "--disable-builtin-mcps")
	}
	// Add --no-ask-user to enable fully autonomous runs (suppresses interactive prompts).
	// Emitted for both agent and detection jobs when the Copilot CLI version supports it
	// (v1.0.19+). Latest and unspecified versions always include the flag.
	if copilotSupportsNoAskUser(workflowData.EngineConfig) {
		copilotExecLog.Print("Adding --no-ask-user for fully autonomous run")
		copilotArgs = append(copilotArgs, "--no-ask-user")
	}

	// Add --agent flag if specified via engine.agent
	// Note: Agent imports (.github/agents/*.md) still work for importing markdown content,
	// but they do NOT automatically set the --agent flag. Only engine.agent controls the flag.
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Agent != "" {
		agentIdentifier := workflowData.EngineConfig.Agent
		copilotExecLog.Printf("Using agent from engine.agent: %s", agentIdentifier)
		copilotArgs = append(copilotArgs, "--agent", agentIdentifier)
	}
	// Add --autopilot and --max-autopilot-continues when max-continuations > 1
	// Never apply autopilot flags to detection jobs; they are only meaningful for the agent run.
	if !isDetectionJob && workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxContinuations > 1 {
		maxCont := workflowData.EngineConfig.MaxContinuations
		copilotExecLog.Printf("Enabling autopilot mode with max-autopilot-continues=%d", maxCont)
		copilotArgs = append(copilotArgs, "--autopilot", "--max-autopilot-continues", strconv.Itoa(maxCont))
	}
	toolArgs := e.computeCopilotToolArguments(workflowData.Tools, workflowData.SafeOutputs, workflowData.MCPScripts, workflowData)
	return e.buildCopilotFeatureArgs(workflowData, copilotArgs, toolArgs), toolArgs
}

func copilotNeedsBuiltinMCPs(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.Tools == nil || isCopilotSDKMode(workflowData) {
		return false
	}
	return isCopilotToolValueEnabled(workflowData.Tools, "web-fetch")
}

func (e *CopilotEngine) buildCopilotBaseArgs(sandboxEnabled bool) []string {
	if sandboxEnabled {
		// Simplified args for sandbox mode (AWF)
		copilotExecLog.Print("Added workspace directory to --add-dir")
		copilotExecLog.Print("Using firewall mode with simplified arguments")
		// Note: --add-dir "${GITHUB_WORKSPACE}" is appended raw after shellJoinArgs below
		// to allow shell variable expansion (cannot go through shellEscapeArg).
		return []string{"--add-dir", constants.TmpGhAwDirSlash, "--log-level", "all", "--log-dir", logsFolder}
	}
	// Original args for non-sandbox mode
	copilotExecLog.Print("Using standard mode with full arguments")
	return []string{"--add-dir", "/tmp/", "--add-dir", constants.TmpGhAwDirSlash, "--add-dir", constants.TmpGhAwAgentDir, "--log-level", "all", "--log-dir", logsFolder}
}

func (e *CopilotEngine) buildCopilotFeatureArgs(workflowData *WorkflowData, copilotArgs []string, toolArgs []string) []string {
	// Add tool permission arguments based on configuration
	if len(toolArgs) > 0 {
		copilotExecLog.Printf("Adding %d tool permission arguments", len(toolArgs))
	}
	copilotArgs = append(copilotArgs, toolArgs...)
	// Add --add-dir for each configured memory backend.
	if workflowData.CacheMemoryConfig != nil {
		for _, cache := range workflowData.CacheMemoryConfig.Caches {
			cacheDir := cacheMemoryDirFor(cache.ID) + "/"
			copilotArgs = append(copilotArgs, "--add-dir", cacheDir)
		}
	}
	// Drive-memory is exposed through /tmp symlinks outside the repository workspace.
	if workflowData.DriveMemoryConfig != nil {
		for _, drive := range workflowData.DriveMemoryConfig.Drives {
			copilotArgs = append(copilotArgs, "--add-dir", driveMemoryDirFor(drive.ID)+"/")
		}
	}
	// Add --allow-all-paths when edit tool is enabled to allow write on all paths
	// See: https://github.com/github/copilot-cli/issues/67#issuecomment-3411256174
	if isCopilotEditToolEnabled(workflowData.Tools, workflowData) {
		copilotArgs = append(copilotArgs, "--allow-all-paths")
	}
	// Add --no-custom-instructions when bare mode is enabled to suppress automatic
	// loading of custom instructions from .github/AGENTS.md and user-level configs.
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Bare {
		copilotExecLog.Print("Bare mode enabled: adding --no-custom-instructions")
		copilotArgs = append(copilotArgs, "--no-custom-instructions")
	}
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Args) > 0 {
		copilotArgs = append(copilotArgs, workflowData.EngineConfig.Args...)
	}
	return copilotArgs
}

func buildCopilotMkdirCommands(copilotArgs []string) string {
	// Extract all --add-dir paths and generate mkdir commands
	addDirPaths := extractAddDirPaths(copilotArgs)
	// Also ensure the log directory exists
	addDirPaths = append(addDirPaths, logsFolder)
	var mkdirCommands strings.Builder
	for _, dir := range addDirPaths {
		fmt.Fprintf(&mkdirCommands, "mkdir -p %s\n", dir)
	}
	return mkdirCommands.String()
}

func getCopilotModelEnvVar(workflowData *WorkflowData) string {
	if workflowRunPhase(workflowData) == runPhaseEvals {
		return constants.EnvVarModelEvalsCopilot
	}
	isDetectionJob := isDetectionRun(workflowData)
	if isDetectionJob {
		return constants.EnvVarModelDetectionCopilot
	}
	return constants.EnvVarModelAgentCopilot
}

func getCopilotTimeoutValue(workflowData *WorkflowData) string {
	return resolveStepTimeoutValue(workflowData)
}

func (e *CopilotEngine) resolveCopilotCommand(workflowData *WorkflowData, sandboxEnabled bool) (string, string) {
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		copilotExecLog.Printf("Using serialized custom command script: %s", customEngineCommandScriptPath)
		return customEngineCommandScriptPath, buildEngineCommandScriptSetup(workflowData.EngineConfig.Command)
	}
	if sandboxEnabled {
		// Every AWF runtime receives RUNNER_TEMP/gh-aw as a read-only mount. Standard,
		// gVisor, and docker-sbx runs stage the activated binary in the execution step;
		// ARC/DinD stages it during installation so the remote daemon can see it.
		return `"` + constants.GhAwRootDirShell + `/bin/copilot"`, ""
	}
	// Non-sandbox mode: use standard copilot command
	return "copilot", ""
}

func (e *CopilotEngine) buildCopilotExecPrefix(workflowData *WorkflowData, commandName string) string {
	// Build the command - model is always passed via COPILOT_MODEL env var (see env block below).
	// The --add-dir "${GITHUB_WORKSPACE}" arg is appended raw (not through shellJoinArgs)
	// because it contains a shell variable reference that must expand at runtime.
	//
	// When a harness script is provided (GetHarnessScriptName), wrap the copilot invocation with
	// `node <harness> <commandName> <args>` to enable retry logic for transient CAPIError 400 errors.
	//
	// Resolve node dynamically at runtime:
	// - Prefer GH_AW_NODE_BIN when set and executable.
	// - Fall back to `command -v node` if GH_AW_NODE_BIN points to a non-mounted toolcache path.
	// This prevents agent startup failures when host toolcache paths are not present in the AWF container.
	harnessScriptName := e.GetHarnessScriptName()
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.HarnessScript != "" {
		harnessScriptName = workflowData.EngineConfig.HarnessScript
	}
	if harnessScriptName == "" {
		return commandName
	}
	harnessScriptPath := fmt.Sprintf(`"%s/%s"`, SetupActionDestinationShell, harnessScriptName)
	runtimeResolutionCommand := nodeRuntimeResolutionCommand
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.CopilotSDK {
		runtimeResolutionCommand = nodeRuntimeResolutionCommandForCopilotSDK
		return e.buildCopilotSDKExecPrefix(workflowData, commandName, harnessScriptPath, runtimeResolutionCommand)
	}
	return fmt.Sprintf(`%s %s %s`, runtimeResolutionCommand, harnessScriptPath, commandName)
}

func (e *CopilotEngine) buildCopilotSDKExecPrefix(workflowData *WorkflowData, commandName, harnessScriptPath, runtimeResolutionCommand string) string {
	sdkDriverScriptName := "copilot_sdk_driver.cjs"
	customSDKDriverConfigured := workflowData.EngineConfig != nil && workflowData.EngineConfig.Driver != ""
	if customSDKDriverConfigured {
		sdkDriverScriptName = workflowData.EngineConfig.Driver
	}
	// Driver mode: the harness receives the driver runtime command and the driver path (or just
	// the arbitrary command) as its argv, then calls runProcess(command, args) on the driver.
	// For language scripts (.js/.cjs/.mjs, .py, .ts/.mts, .rb), the runtime command is determined
	// by extension; bare command names (no extension) are treated as arbitrary executables in PATH.
	driverRuntimeCmd, driverArg := copilotSDKDriverExecArgs(sdkDriverScriptName)
	if driverArg == "" {
		if customSDKDriverConfigured && strings.Contains(sdkDriverScriptName, "/") {
			driverRuntimeCmd = `"${GITHUB_WORKSPACE}/` + sdkDriverScriptName + `"`
		}
		return fmt.Sprintf(`%s %s %s %s`, runtimeResolutionCommand, harnessScriptPath, driverRuntimeCmd, commandName)
	}
	driverPath := fmt.Sprintf(`"%s/%s"`, SetupActionDestinationShell, sdkDriverScriptName)
	if customSDKDriverConfigured {
		// Custom driver: sdkDriverScriptName is a validated workspace-relative path.
		// Validation ensures no shell metacharacters, quotes, or path traversal,
		// so it is safe to embed directly in the double-quoted shell argument.
		driverPath = `"${GITHUB_WORKSPACE}/` + sdkDriverScriptName + `"`
	}
	// Language script: harness runs <runtime> <setup-action-dir>/<harness> <runtime> <driver-path> <copilot-binary>
	return fmt.Sprintf(`%s %s %s %s %s`, runtimeResolutionCommand, harnessScriptPath, driverRuntimeCmd, driverPath, commandName)
}

func (e *CopilotEngine) buildCopilotCommand(workflowData *WorkflowData, copilotArgs []string, execPrefix, customCommandScriptSetup, logFile, mkdirCommands string, isBYOKMode bool) (string, string) {
	copilotCommand, copilotSDKServerArgsJSON := e.buildCopilotBaseCommand(workflowData, copilotArgs, execPrefix)
	if isFirewallEnabled(workflowData) {
		return e.buildCopilotFirewallCommand(workflowData, copilotCommand, customCommandScriptSetup, isBYOKMode, logFile), copilotSDKServerArgsJSON
	}
	return e.buildCopilotDirectCommand(workflowData, copilotCommand, customCommandScriptSetup, mkdirCommands, logFile), copilotSDKServerArgsJSON
}

func (e *CopilotEngine) buildCopilotBaseCommand(workflowData *WorkflowData, copilotArgs []string, execPrefix string) (string, string) {
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.CopilotSDK {
		return e.buildCopilotSDKCommand(workflowData, execPrefix, copilotArgs)
	}
	// On ARC/DinD, /tmp/gh-aw is not daemon-visible; prompts are copied to ${RUNNER_TEMP}/gh-aw/
	promptFilePath := constants.AwPromptsFile
	if isArcDindTopology(workflowData) {
		promptFilePath = constants.AwPromptsFileShell
	}
	if isFirewallEnabled(workflowData) {
		// Sandbox mode: add workspace dir and pass prompt file path directly
		return fmt.Sprintf(`%s %s --add-dir "${GITHUB_WORKSPACE}" --prompt-file %s`, execPrefix, shellJoinArgs(copilotArgs), promptFilePath), ""
	}
	// Non-sandbox mode: pass prompt file path directly
	return fmt.Sprintf(`%s %s --prompt-file %s`, execPrefix, shellJoinArgs(copilotArgs), promptFilePath), ""
}

func (e *CopilotEngine) buildCopilotSDKCommand(workflowData *WorkflowData, execPrefix string, copilotArgs []string) (string, string) {
	// SDK driver mode: configuration is passed via environment variables so that
	// copilot_sdk_driver.cjs is a self-contained program started by the harness like any other command.
	// GH_AW_COPILOT_SDK_SERVER_ARGS carries the JSON-encoded CLI argument list for the headless
	// Copilot CLI sidecar, and the driver appends --add-dir $GITHUB_WORKSPACE automatically.
	serverArgsPrefix := []string{"--headless", "--no-auto-update"}
	if isCloudHypervisorRuntime(workflowData) {
		serverArgsPrefix = append(serverArgsPrefix, "--host", "0.0.0.0")
	}
	serverArgsPrefix = append(serverArgsPrefix, "--port", strconv.Itoa(constants.DefaultCopilotSDKPort))
	serverArgs := append(serverArgsPrefix, copilotArgs...)
	serverArgsJSON, err := json.Marshal(serverArgs)
	if err != nil {
		// This should never happen with a plain string slice, but fall back to an
		// empty array so the run is not blocked.
		copilotExecLog.Printf("warning: failed to marshal SDK server args: %v; falling back to empty array", err)
		serverArgsJSON = []byte(`[]`)
	}
	return execPrefix, string(serverArgsJSON)
}

func (e *CopilotEngine) buildCopilotFirewallCommand(workflowData *WorkflowData, copilotCommand, customCommandScriptSetup string, isBYOKMode bool, logFile string) string {
	allowedDomains := e.buildCopilotAllowedDomains(workflowData)
	// AWF v0.15.0+ uses chroot mode by default, and sudo's secure_path may strip PATH additions.
	// Prepend GetNpmBinPathSetup() and the MCP CLI bin path so the container can resolve node and CLI proxies.
	engineCommand := fmt.Sprintf("%s && %s", GetNpmBinPathSetup(), copilotCommand)
	if mcpCLIPath := GetMCPCLIPathSetup(workflowData); mcpCLIPath != "" {
		engineCommand = fmt.Sprintf("%s && %s", mcpCLIPath, engineCommand)
	}
	// Build the list of core secret var names to hide from the agent shell tools.
	// In BYOK mode COPILOT_GITHUB_TOKEN is not injected into the step env at all,
	// so there is nothing to exclude.
	copilotCoreSecrets := []string{}
	if !isBYOKMode {
		copilotCoreSecrets = []string{"COPILOT_GITHUB_TOKEN"}
	}
	return BuildAWFCommand(AWFCommandConfig{EngineName: "copilot", EngineCommand: engineCommand, LogFile: logFile, WorkflowData: workflowData, UsesTTY: false, AllowedDomains: allowedDomains, ResolveMaxAICreditsFromEnv: true, PathSetup: e.buildCopilotAWFPathSetup(workflowData, customCommandScriptSetup), ExcludeEnvVarNames: ComputeAWFExcludeEnvVarNames(workflowData, copilotCoreSecrets)})
}

func (e *CopilotEngine) buildCopilotAllowedDomains(workflowData *WorkflowData) string {
	// For detection runs use the minimal detection domain list; normal agent runs use the full domain set.
	if workflowData.IsDetectionRun {
		allowedDomains := GetThreatDetectionAllowedDomains(workflowData.NetworkPermissions)
		for _, copilotTarget := range GetCopilotAllowlistTargets(workflowData) {
			allowedDomains = mergeAPITargetDomains(allowedDomains, copilotTarget)
		}
		return allowedDomains
	}
	allowedDomains := workflowData.CachedAllowedDomainsStr
	if !workflowData.CachedAllowedDomainsComputed {
		allowedDomains = GetAllowedDomainsForEngine(constants.CopilotEngine, workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)
	}
	for _, copilotTarget := range GetCopilotAllowlistTargets(workflowData) {
		allowedDomains = mergeAPITargetDomains(allowedDomains, copilotTarget)
	}
	return allowedDomains
}

func (e *CopilotEngine) buildCopilotAWFPathSetup(workflowData *WorkflowData, customCommandScriptSetup string) string {
	stepSummaryPath := copilotStepSummaryPath(workflowData)
	pathSetup := "touch " + stepSummaryPath + "\n" +
		"GH_AW_NODE_BIN=$(command -v node 2>/dev/null || true)\n" +
		"export GH_AW_NODE_BIN\n" +
		"export COPILOT_API_KEY=\"$" + constants.CopilotBYOKDummyAPIKeyEnvVar + "\""
	usesInstalledCopilotBinary := workflowData.EngineConfig == nil || workflowData.EngineConfig.Command == ""
	if usesInstalledCopilotBinary && !isArcDindTopology(workflowData) {
		pathSetup = copilotBinaryPathSetup + "\n" + pathSetup
	}
	if customCommandScriptSetup != "" {
		pathSetup = customCommandScriptSetup + "\n" + pathSetup
	}
	homeExport := ""
	if isArcDindTopology(workflowData) {
		homeExport = fmt.Sprintf("export HOME=%s\n", awfArcDindHomePathExpr)
	}
	// Write the Copilot settings file before AWF starts. The file is created on the host and mounted
	// into the container, where the Copilot CLI reads it to disable the rubber-duck sub-agent.
	return homeExport + buildCopilotSettingsCleanupAndExitCodeTrap() + buildCopilotSettingsSetup(buildCopilotSettingsContent(workflowData), customCommandScriptSetup != "") + buildCopilotMCPConfigExport(workflowData) + pathSetup
}

func copilotStepSummaryPath(workflowData *WorkflowData) string {
	if workflowData != nil && workflowData.IsDetectionRun &&
		!isFeatureEnabled(constants.GHAWDetectionFeatureFlag, workflowData) {
		return constants.ThreatDetectionStepSummaryPath
	}
	return AgentStepSummaryPath
}

func (e *CopilotEngine) buildCopilotDirectCommand(workflowData *WorkflowData, copilotCommand, customCommandScriptSetup, mkdirCommands, logFile string) string {
	// Run copilot command without AWF wrapper.
	// Prepend a touch command to create the agent step summary file before copilot runs.
	preCommandSetup := mkdirCommands
	if customCommandScriptSetup != "" {
		preCommandSetup = customCommandScriptSetup + "\n" + preCommandSetup
	}
	// Write the Copilot settings file before the agent runs to disable the rubber-duck sub-agent.
	preCommandSetup = buildCopilotSettingsCleanupAndExitCodeTrap() + buildCopilotSettingsSetup(buildCopilotSettingsContent(workflowData), customCommandScriptSetup != "") + buildCopilotMCPConfigExport(workflowData) + preCommandSetup
	return fmt.Sprintf(`set -o pipefail
printf '%%s' "$(date +%%s%%3N)" > %s
touch %s
(umask 177 && touch %s)
%s%s 2>&1 | tee %s`, AgentCLIStartMsPath, copilotStepSummaryPath(workflowData), logFile, preCommandSetup, copilotCommand, logFile)
}

type copilotStepEnvFlags struct {
	byokMode        bool
	sandboxEnabled  bool
	modelConfigured bool
}

func (e *CopilotEngine) buildCopilotStepEnv(
	workflowData *WorkflowData,
	llmProvider LLMProvider,
	modelEnvVar string,
	timeoutValue string,
	flags copilotStepEnvFlags,
	copilotSDKServerArgsJSON string,
	copilotSDKToolConfigJSON string,
) map[string]string {
	useCopilotRequests := hasCopilotRequestsWritePermission(workflowData)
	env := e.buildCopilotBaseStepEnv(workflowData, llmProvider, timeoutValue, flags.byokMode, useCopilotRequests)
	e.addCopilotWorkflowStepEnv(env, workflowData, flags.sandboxEnabled)
	e.addCopilotGitHubToolEnv(env, workflowData)
	e.addCopilotModelEnv(env, workflowData, flags.modelConfigured, modelEnvVar)
	e.addCopilotFinalStepEnv(env, workflowData)
	e.addCopilotSandboxEnv(env, flags.sandboxEnabled)
	e.addCopilotSDKStepEnv(env, workflowData, copilotSDKServerArgsJSON, copilotSDKToolConfigJSON)
	return env
}

func (e *CopilotEngine) buildCopilotBaseStepEnv(workflowData *WorkflowData, llmProvider LLMProvider, timeoutValue string, isBYOKMode, useCopilotRequests bool) map[string]string {
	env := map[string]string{"COPILOT_AGENT_RUNNER_TYPE": "STANDALONE", "GITHUB_STEP_SUMMARY": copilotStepSummaryPath(workflowData), "GITHUB_HEAD_REF": "${{ github.head_ref }}", "GITHUB_REF_NAME": "${{ github.ref_name }}", "GITHUB_WORKSPACE": "${{ github.workspace }}", "RUNNER_TEMP": "${{ runner.temp }}", "GH_AW_TIMEOUT_MINUTES": timeoutValue, "GITHUB_SERVER_URL": "${{ github.server_url }}", "GITHUB_API_URL": "${{ github.api_url }}", "GH_AW_LLM_PROVIDER": string(llmProvider)}
	// Auto-configure Copilot BYOK routing when engine.model-provider selects a non-GitHub provider.
	// Explicit engine.env values still win later via maps.Copy.
	if llmProvider != LLMProviderGitHub && isFirewallEnabled(workflowData) {
		env[constants.CopilotProviderBaseURL] = llmProviderGatewayBaseURL(llmProvider)
		env[constants.CopilotProviderAPIKey] = llmProviderSecretExpression(llmProvider, workflowData)
	}
	// Inject the GitHub token only when not in BYOK mode. The engine.env merge that
	// happens later (maps.Copy(env, workflowData.EngineConfig.Env)) can still override
	// or nullify this if the user explicitly sets COPILOT_GITHUB_TOKEN in engine.env.
	if isBYOKMode {
		copilotExecLog.Print("Skipping COPILOT_GITHUB_TOKEN injection: BYOK mode active (COPILOT_PROVIDER_BASE_URL is set)")
	} else {
		env["COPILOT_GITHUB_TOKEN"] = e.buildCopilotGitHubTokenExpression(useCopilotRequests)
	}
	injectWorkflowCallNetworkAllowedEnv(env, workflowData)
	// When permissions.copilot-requests is write, set S2STOKENS=true to allow the Copilot CLI
	// to accept GitHub App installation tokens (ghs_*) such as ${{ github.token }}.
	if useCopilotRequests {
		env["S2STOKENS"] = "true"
	}
	return env
}

func (e *CopilotEngine) buildCopilotGitHubTokenExpression(useCopilotRequests bool) string {
	// COPILOT_GITHUB_TOKEN injection: the token is only needed for GitHub's own Copilot backend.
	// When not in BYOK mode, use the GitHub Actions token when permissions.copilot-requests is write,
	// otherwise use the COPILOT_GITHUB_TOKEN secret.
	// #nosec G101 -- These are NOT hardcoded credentials. They are GitHub Actions expression templates
	// that the runtime replaces with actual values. The strings "${{ secrets.COPILOT_GITHUB_TOKEN }}"
	// and "${{ github.token }}" are placeholders, not actual credentials.
	if useCopilotRequests {
		copilotExecLog.Print("Using GitHub Actions token as COPILOT_GITHUB_TOKEN (permissions.copilot-requests=write)")
		return "${{ github.token }}"
	}
	return "${{ secrets.COPILOT_GITHUB_TOKEN }}"
}

func (e *CopilotEngine) addCopilotWorkflowStepEnv(env map[string]string, workflowData *WorkflowData, sandboxEnabled bool) {
	// In sandbox (AWF) mode, set git identity environment variables so the first git commit succeeds.
	if sandboxEnabled {
		maps.Copy(env, getGitIdentityEnvVars())
	}
	// Always add GH_AW_PROMPT for agentic workflows
	env["GH_AW_PROMPT"] = constants.AwPromptsFile
	// Tag the step as a GitHub AW agentic execution for discoverability by agents
	env["GITHUB_AW"] = "true"
	env["GH_AW_PHASE"] = workflowRunPhase(workflowData)
	if IsRelease() {
		env["GH_AW_VERSION"] = GetVersion()
	} else {
		env["GH_AW_VERSION"] = "dev"
	}
	applyDefaultMaxAICreditsEnvToMap(env, workflowData)
	applySafeOutputEnvToMap(env, workflowData)
	applyTraceContextEnvToMap(env)
	applyOptionalEngineToolTimeouts(env, workflowData)
	applyEngineMaxTurnsEnv(env, workflowData)
	applyEngineHarnessRetryEnv(env, workflowData)
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.CopilotSDK {
		env[constants.EnvVarMaxToolDenials] = strconv.Itoa(constants.DefaultMaxToolDenials)
		if workflowData.EngineConfig.MaxToolDenials != "" {
			env[constants.EnvVarMaxToolDenials] = workflowData.EngineConfig.MaxToolDenials
		}
	}
}

func (e *CopilotEngine) addCopilotGitHubToolEnv(env map[string]string, workflowData *WorkflowData) {
	if !hasGitHubTool(workflowData.ParsedTools) {
		return
	}
	githubToolConfig, _ := workflowData.Tools["github"].(map[string]any)
	customGitHubToken := getGitHubToken(githubToolConfig)
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.GitHub != nil && workflowData.ParsedTools.GitHub.GitHubApp != nil {
		tokenExpression := "${{ steps.github-mcp-app-token.outputs.token }}"
		if workflowData.ParsedTools.GitHub.GitHubApp.shouldIgnoreMissingKey() {
			tokenExpression = combineTokenExpressions(tokenExpression, getEffectiveGitHubToken(customGitHubToken))
		}
		env["GITHUB_MCP_SERVER_TOKEN"] = tokenExpression
		return
	}
	env["GITHUB_MCP_SERVER_TOKEN"] = getEffectiveGitHubToken(customGitHubToken)
}

func (e *CopilotEngine) addCopilotModelEnv(env map[string]string, workflowData *WorkflowData, modelConfigured bool, modelEnvVar string) {
	// Set the model environment variable.
	// The model is always passed via the native COPILOT_MODEL env var, which the Copilot CLI reads directly.
	// When model is not configured, map the GitHub org variable to COPILOT_MODEL so users can set a default.
	if modelConfigured {
		if containsExpression(workflowData.Model) {
			env[constants.EnvVarModelFallback] = compilerenv.BuildModelOverrideExpression(modelEnvVar, compilerenv.DefaultModelCopilot, constants.CopilotBYOKDefaultModel)
		}
		copilotExecLog.Printf("Setting %s env var for model: %s", constants.CopilotCLIModelEnvVar, workflowData.Model)
		env[constants.CopilotCLIModelEnvVar] = workflowData.Model
		return
	}
	env[constants.CopilotCLIModelEnvVar] = compilerenv.BuildModelOverrideExpression(modelEnvVar, compilerenv.DefaultModelCopilot, constants.CopilotBYOKDefaultModel)
}

func (e *CopilotEngine) addCopilotFinalStepEnv(env map[string]string, workflowData *WorkflowData) {
	// Inject GH_AW_ENGINE_CWD when engine.cwd is configured.
	applyEngineCwdEnv(env, workflowData)
	applyEngineAndAgentEnv(env, workflowData, copilotExecLog)
	// Always inject the Copilot integration ID for agentic workflows after all env merges
	// so user-supplied env does not override this value.
	env[constants.CopilotCLIIntegrationIDEnvVar] = constants.CopilotCLIIntegrationIDValue
	// Add HTTP MCP header secrets to env for passthrough
	for varName, secretExpr := range collectHTTPMCPHeaderSecrets(workflowData.Tools) {
		if _, exists := env[varName]; !exists {
			env[varName] = secretExpr
		}
	}
	applyMCPScriptsSecretEnv(env, workflowData)
}

func (e *CopilotEngine) addCopilotSandboxEnv(env map[string]string, sandboxEnabled bool) {
	// Inject the dummy BYOK sentinel and AWF_REFLECT_ENABLED only when the AWF sandbox is active.
	// COPILOT_API_KEY itself is exported in PathSetup via shell variable expansion to avoid false positives.
	if sandboxEnabled {
		env[constants.CopilotBYOKDummyAPIKeyEnvVar] = constants.CopilotBYOKDummyAPIKey
		env["AWF_REFLECT_ENABLED"] = "1"
	}
}

func (e *CopilotEngine) addCopilotSDKStepEnv(env map[string]string, workflowData *WorkflowData, copilotSDKServerArgsJSON string, copilotSDKToolConfigJSON string) {
	// When copilot-sdk: true, provide the SDK URI that the harness uses to start a separate headless server.
	if workflowData.EngineConfig == nil || !workflowData.EngineConfig.CopilotSDK {
		return
	}
	env[constants.CopilotSDKURIEnvVar] = fmt.Sprintf("http://127.0.0.1:%d", constants.DefaultCopilotSDKPort)
	copilotExecLog.Printf("copilot-sdk enabled: set %s=%s", constants.CopilotSDKURIEnvVar, env[constants.CopilotSDKURIEnvVar])
	env[constants.CopilotSDKDriverEnvVar] = "1"
	env[constants.CopilotSDKServerArgsEnvVar] = copilotSDKServerArgsJSON
	env[constants.CopilotSDKToolConfigEnvVar] = copilotSDKToolConfigJSON
	copilotExecLog.Printf("copilot-sdk driver mode: set %s, %s and %s", constants.CopilotSDKDriverEnvVar, constants.CopilotSDKServerArgsEnvVar, constants.CopilotSDKToolConfigEnvVar)
	if currentPythonPath, exists := env["PYTHONPATH"]; copilotSDKRuntimeID(workflowData) == "python" && (!exists || currentPythonPath == "") {
		env["PYTHONPATH"] = copilotSDKPythonPathExpression
	}
}

func (e *CopilotEngine) buildCopilotExecutionStep(workflowData *WorkflowData, command string, env map[string]string, timeoutValue string) GitHubActionStep {
	// Generate the step for Copilot CLI execution
	stepLines := []string{"      - name: " + copilotExecutionStepName, "        id: agentic_execution"}
	// Add tool arguments comment before the run section
	toolArgsComment := e.generateCopilotToolArgumentsComment(workflowData.Tools, workflowData.SafeOutputs, workflowData.MCPScripts, workflowData, "        ")
	if toolArgsComment != "" {
		commentLines := strings.Split(strings.TrimSuffix(toolArgsComment, "\n"), "\n")
		stepLines = append(stepLines, commentLines...)
	}
	stepLines = append(stepLines, "        timeout-minutes: "+timeoutValue)
	// Filter environment variables to only include allowed secrets
	// This is a security measure to prevent exposing unnecessary secrets to the AWF container
	allowedSecrets := e.GetRequiredSecretNames(workflowData)
	filteredEnv := FilterEnvForSecrets(env, allowedSecrets)
	if enclavesEnabled(workflowData) {
		filteredEnv["MCP_GATEWAY_API_KEY"] = "${{ steps.start-mcp-gateway.outputs.gateway-api-key }}"
	}
	// Inject GH_TOKEN for CLI proxy (added after filtering since it uses a special
	// fallback expression that is always allowed when cli-proxy is enabled)
	addCliProxyGHTokenToEnv(filteredEnv, workflowData)
	return GitHubActionStep(FormatStepWithCommandAndEnv(stepLines, command, filteredEnv))
}

// copilotSupportsNoAskUser returns true when the effective Copilot CLI version supports the
// --no-ask-user flag, which enables fully autonomous agentic runs by suppressing interactive prompts.
//
// The --no-ask-user flag was introduced in Copilot CLI v1.0.19. Any workflow that pins an
// explicit version older than v1.0.19 must not emit --no-ask-user or the run will fail at startup.
//
// Special cases:
//   - No version override (engineConfig is nil or has no Version): use
//     DefaultCopilotVersion. This preserves existing behavior while avoiding drift if
//     DefaultCopilotVersion is ever lowered below CopilotNoAskUserMinVersion.
//   - "latest": always returns true (latest is always a new release).
//   - Expression (e.g. "${{ inputs.engine-version }}"): returns true. The version resolves
//     at runtime so we cannot gate at compile time; the installer already handles expression
//     versions via ENGINE_VERSION env-var injection, ensuring the right binary is installed.
//   - Any semver string ≥ CopilotNoAskUserMinVersion: returns true.
//   - Any semver string < CopilotNoAskUserMinVersion: returns false.
//   - Non-semver string (e.g. a branch name): returns false (conservative).
func copilotSupportsNoAskUser(engineConfig *EngineConfig) bool {
	var versionStr string
	if engineConfig != nil && engineConfig.Version != "" {
		versionStr = engineConfig.Version
	}
	// Expression versions resolve at runtime; treat as supported so the generated execution
	// flags match the installed binary for any version >= CopilotNoAskUserMinVersion.
	if containsExpression(versionStr) {
		copilotExecLog.Printf("copilotSupportsNoAskUser: expression version %q treated as supported", versionStr)
		return true
	}
	return versionAtLeast(
		versionStr,
		string(constants.DefaultCopilotVersion),
		string(constants.CopilotNoAskUserMinVersion),
	)
}

// extractAddDirPaths extracts all directory paths from copilot args that follow --add-dir flags
func extractAddDirPaths(args []string) []string {
	var dirs []string
	for i := range len(args) - 1 {
		if args[i] == "--add-dir" {
			dirs = append(dirs, args[i+1])
		}
	}
	return dirs
}

func buildEngineCommandScriptSetup(command string) string {
	// engine.command intentionally accepts shell-form commands from trusted workflow
	// configuration authored in-repo; preserve shell semantics and forward driver args.
	scriptContent := fmt.Sprintf("#!/usr/bin/env bash\nset +o histexpand\nset -eo pipefail\n%s \"$@\"\n", command)
	heredocDelimiter := "GH_AW_ENGINE_COMMAND_EOF"
	for strings.Contains(scriptContent, heredocDelimiter) { //nolint:stringsconcatloop // trivial cold path, runs 0 times in normal operation
		heredocDelimiter += "_X"
	}

	//nolint:generatedyamlheredoc // Legacy trusted engine-command rendering remains to be migrated to the JavaScript renderer.
	return fmt.Sprintf(`mkdir -p /tmp/gh-aw
GH_AW_PREV_UMASK="$(umask)"
umask 0177
cat > %s <<'%s'
%s
%s
chmod 700 %s
umask "$GH_AW_PREV_UMASK"`, customEngineCommandScriptPath, heredocDelimiter, scriptContent, heredocDelimiter, customEngineCommandScriptPath)
}

// generateCopilotSessionFileCopyStep generates a step to copy the entire Copilot
// session-state directory from ~/.copilot/session-state/ to /tmp/gh-aw/sandbox/agent/logs/
// This ensures all session files (events.jsonl, session.db, plan.md, checkpoints, etc.)
// are in /tmp/gh-aw/ where secret redaction can scan them and they get uploaded as artifacts.
// The logic is in actions/setup/sh/copy_copilot_session_state.sh.
func generateCopilotSessionFileCopyStep() GitHubActionStep {
	var step []string

	step = append(step, "      - name: Copy Copilot session state files to logs")
	step = append(step, "        if: always()")
	step = append(step, "        continue-on-error: true")
	step = append(step, "        run: bash \"${RUNNER_TEMP}/gh-aw/actions/copy_copilot_session_state.sh\"")

	return GitHubActionStep(step)
}
