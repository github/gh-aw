package workflow

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var mcpCLIMountLog = logger.New("workflow:mcp_cli_mount")

// mcp_cli_mount.go generates a workflow step that mounts MCP servers as local CLI tools
// and produces the prompt section that informs the agent about these tools.
//
// After the MCP gateway is started, this step runs mount_mcp_as_cli.cjs via
// actions/github-script which:
//   - Reads the CLI manifest saved by start_mcp_gateway.cjs
//   - Queries each server for its tools/list via JSON-RPC
//   - Writes a standalone CLI wrapper script for each server to ${RUNNER_TEMP}/gh-aw/mcp-cli/bin/
//   - Locks the bin directory (chmod 555) so the agent cannot modify the scripts
//   - Adds the directory to PATH via core.addPath()

// internalMCPServerNames lists the MCP servers that are internal infrastructure and
// should not be exposed as user-facing CLI tools.
var internalMCPServerNames = map[string]bool{
	constants.GitHubMCPServerID.String(): true, // GitHub MCP server is handled differently and should not be CLI-mounted
}

// appendCustomMCPServerIfEligible returns a new slice with toolName appended if
// toolValue is a map-shaped MCP server configuration, the tool is not in
// internalMCPServerNames, and the tool is not already present in servers.
// Returns the original slice otherwise (no mutation).
// It is the shared implementation used by both the cli-proxy collection loop and
// the Copilot augmentation sweep, ensuring a single source of truth for "is this
// tool a custom MCP server?" logic.
func appendCustomMCPServerIfEligible(servers []string, toolName string, toolValue any) []string {
	if internalMCPServerNames[toolName] {
		return servers
	}
	mcpConfig, ok := toolValue.(map[string]any)
	if !ok {
		return servers
	}
	if hasMcp, _ := hasMCPConfig(mcpConfig); hasMcp && !slices.Contains(servers, toolName) {
		servers = append(servers, toolName)
	}
	return servers
}

// getMCPCLIServerNames returns the sorted list of MCP server names that will be
// mounted as CLI tools.
//
// Infrastructure servers (safeoutputs, mcpscripts) are always CLI-mounted when
// configured, regardless of whether cli-proxy is enabled. This ensures that
// workflows using engine.command can call safeoutputs/mcpscripts as shell
// commands inside the AWF chroot without needing cli-proxy: true.
//
// User-facing MCP servers (playwright, custom, etc.) are only mounted as CLIs
// when tools.cli-proxy is true.
//
// Returns nil when no servers are eligible for mounting.
// The GitHub MCP server is excluded (handled differently).
func getMCPCLIServerNames(data *WorkflowData) []string { //nolint:largefunc // Existing CLI proxy selection handles all supported tool sources.
	if data == nil {
		return nil
	}

	var servers []string

	// User-facing MCP servers require cli-proxy: true.
	if data.ParsedTools != nil && data.ParsedTools.CLIProxy {
		mcpCLIMountLog.Print("cli-proxy enabled, collecting user-facing CLI server names")

		// Collect user-facing standard MCP tools from the raw Tools map
		for toolName, toolValue := range data.Tools {
			if toolValue == false {
				continue
			}
			// Only include tools that have MCP servers (skip bash, web-fetch, web-search, edit, cache-memory, etc.)
			// Note: "github" is excluded — it is handled differently and should not be CLI-mounted.
			switch toolName {
			case "playwright":
				// In CLI mode, playwright is installed as @playwright/cli via npm and is NOT
				// an MCP server, so it must not appear in the CLI-mounted servers list.
				if !isPlaywrightCLIMode(data.Tools) {
					servers = append(servers, toolName)
				}
			case "qmd":
				servers = append(servers, toolName)
			case "agentic-workflows":
				// The gateway and manifest use "agenticworkflows" (no hyphen) as the server ID.
				// Using the gateway ID here ensures GH_AW_MCP_CLI_SERVERS matches the manifest entries.
				servers = append(servers, constants.AgenticWorkflowsMCPServerID.String())
			default:
				// Include custom MCP servers (not in the internal list)
				servers = appendCustomMCPServerIfEligible(servers, toolName, toolValue)
			}
		}

		// Also check ParsedTools.Custom for custom MCP servers
		for name := range data.ParsedTools.Custom {
			if !internalMCPServerNames[name] && !slices.Contains(servers, name) {
				servers = append(servers, name)
			}
		}
	}

	// Infrastructure servers (safeoutputs, mcpscripts) are always CLI-mounted when
	// configured. This allows workflows with engine.command to call safeoutputs or
	// mcpscripts as shell commands inside the AWF/Copilot chroot without requiring
	// cli-proxy: true. The PATH setup in GetMCPCLIPathSetup ensures the bin directory
	// is reachable inside the sandbox.
	if HasSafeOutputsEnabled(data.SafeOutputs) && !slices.Contains(servers, constants.SafeOutputsMCPServerID.String()) {
		servers = append(servers, constants.SafeOutputsMCPServerID.String())
	}
	if IsMCPScriptsEnabled(data.MCPScripts) && !slices.Contains(servers, constants.MCPScriptsMCPServerID.String()) {
		servers = append(servers, constants.MCPScriptsMCPServerID.String())
	}

	// Copilot normally runs with --disable-builtin-mcps. When at least one CLI
	// mount trigger is active (safeoutputs/mcpscripts or cli-proxy), the mount
	// script discovers all MCP servers from the gateway manifest (including
	// GitHub and custom servers such as azure-devops) and exposes wrappers on
	// PATH. Reflect that runtime reality in the generated CLI server list so
	// agents can call these wrappers deterministically instead of guessing
	// command names.
	//
	// Use cli-proxy as part of the activation condition because the initial
	// collection deliberately excludes GitHub: a workflow with cli-proxy: true
	// and only a GitHub MCP tool would have len(servers)==0 at this point,
	// causing the block to be skipped and `github` to never be advertised.
	isCLIMountActive := len(servers) > 0 || (data.ParsedTools != nil && data.ParsedTools.CLIProxy)
	if isCLIMountActive && data.EngineConfig != nil && data.EngineConfig.ID == string(constants.CopilotEngine) {
		if hasGitHubTool(data.ParsedTools) && !isGitHubCLIModeEnabled(data) && !slices.Contains(servers, constants.GitHubMCPServerID.String()) {
			servers = append(servers, constants.GitHubMCPServerID.String())
		}

		for toolName, toolValue := range data.Tools {
			servers = appendCustomMCPServerIfEligible(servers, toolName, toolValue)
		}
	}

	if len(servers) == 0 {
		mcpCLIMountLog.Print("No MCP CLI servers configured")
		return nil
	}

	sort.Strings(servers)
	mcpCLIMountLog.Printf("MCP CLI servers selected: %v", servers)
	return servers
}

func buildCLIWorkflowDataForMounts(workflowData *WorkflowData, tools map[string]any, safeOutputs *SafeOutputsConfig, mcpScripts *MCPScriptsConfig) *WorkflowData {
	if workflowData == nil {
		workflowData = &WorkflowData{}
	}

	copied := *workflowData
	if copied.Tools == nil {
		copied.Tools = tools
	}
	if copied.SafeOutputs == nil {
		copied.SafeOutputs = safeOutputs
	}
	if copied.MCPScripts == nil {
		copied.MCPScripts = mcpScripts
	}
	if copied.ParsedTools == nil && copied.Tools != nil {
		copied.ParsedTools = NewTools(copied.Tools)
	}

	return &copied
}

// hasBashRestrictedAllowlist reports true when tools contains an explicit, finite bash
// command allowlist (not a wildcard). Used to decide whether implicit commands must be
// injected for auto-allowed tools such as playwright-cli.
func hasBashRestrictedAllowlist(tools map[string]any) bool {
	if tools == nil {
		return false
	}
	bashConfig, hasBash := tools["bash"]
	if !hasBash {
		return false
	}
	bashCommands, ok := bashConfig.([]any)
	if !ok || len(bashCommands) == 0 {
		return false
	}
	for _, cmd := range bashCommands {
		if cmdStr, ok := cmd.(string); ok && (cmdStr == "*" || cmdStr == ":*") {
			return false
		}
	}
	return true
}

func getMountedCLIServerNamesIfBashRestricted(workflowData *WorkflowData, tools map[string]any, safeOutputs *SafeOutputsConfig, mcpScripts *MCPScriptsConfig) []string {
	if !hasBashRestrictedAllowlist(tools) {
		return nil
	}
	return getMCPCLIServerNames(buildCLIWorkflowDataForMounts(workflowData, tools, safeOutputs, mcpScripts))
}

func withMountedCLIShellCommandsInRestrictedBash(workflowData *WorkflowData) map[string]any { //nolint:largefunc // Existing shell wrapper logic preserves command rewriting order.
	if workflowData == nil {
		return nil
	}
	if workflowData.Tools == nil {
		return workflowData.Tools
	}

	hasRestrictedBash := hasBashRestrictedAllowlist(workflowData.Tools)
	servers := getMountedCLIServerNamesIfBashRestricted(workflowData, workflowData.Tools, workflowData.SafeOutputs, workflowData.MCPScripts)
	needsPlaywrightCLI := isPlaywrightCLIMode(workflowData.Tools) && hasRestrictedBash
	needsGitHubCLI := isGitHubCLIModeEnabled(workflowData) && hasRestrictedBash

	if len(servers) == 0 && !needsPlaywrightCLI && !needsGitHubCLI {
		return workflowData.Tools
	}

	bashCommands, ok := workflowData.Tools["bash"].([]any)
	if !ok || len(bashCommands) == 0 {
		return workflowData.Tools
	}

	copiedTools := make(map[string]any, len(workflowData.Tools))
	// A shallow copy is sufficient because we only replace the top-level "bash"
	// value with a newly allocated slice and do not mutate nested map/slice values.
	maps.Copy(copiedTools, workflowData.Tools)

	augmentedBash := append([]any(nil), bashCommands...)
	for _, server := range servers {
		command := server + ":*"
		exists := false
		for _, allowed := range augmentedBash {
			if allowedStr, ok := allowed.(string); ok && allowedStr == command {
				exists = true
				break
			}
		}
		if !exists {
			augmentedBash = append(augmentedBash, command)
		}
	}

	// When playwright is configured in CLI mode, playwright-cli must be executable.
	// Automatically add it to the restricted bash allowlist so the agent can invoke it.
	// This injection only applies when bash is restricted (explicit allowlist); when bash
	// is unrestricted (nil or wildcard), playwright-cli is already accessible without
	// explicit allowlisting.
	if needsPlaywrightCLI {
		const playwrightCLICommand = "playwright-cli:*"
		if !slices.ContainsFunc(augmentedBash, func(v any) bool {
			s, ok := v.(string)
			return ok && s == playwrightCLICommand
		}) {
			augmentedBash = append(augmentedBash, playwrightCLICommand)
		}
	}

	// When GitHub CLI mode is enabled (tools.github.mode: gh-proxy), GitHub access
	// happens through the pre-authenticated gh binary. Ensure restricted bash
	// allowlists can invoke it.
	if needsGitHubCLI {
		const ghCLICommand = "gh:*"
		if !slices.ContainsFunc(augmentedBash, func(v any) bool {
			s, ok := v.(string)
			return ok && s == ghCLICommand
		}) {
			augmentedBash = append(augmentedBash, ghCLICommand)
		}
	}

	copiedTools["bash"] = augmentedBash
	return copiedTools
}

// getMCPCLIExcludeFromAgentConfig returns the sorted list of MCP server names that
// should be excluded from the agent's MCP config (because they are CLI-only).
//
// Only excludes servers when tools.cli-proxy is explicitly enabled. Infrastructure
// servers (safeoutputs, mcpscripts) are CLI-mounted even without cli-proxy (so the
// agent can call them as shell commands), but they remain in the agent's MCP config
// unless cli-proxy is explicitly enabled. This preserves existing agent behaviour
// for workflows that use safeoutputs via MCP rather than via the CLI wrapper.
func getMCPCLIExcludeFromAgentConfig(data *WorkflowData) []string {
	if data == nil || data.ParsedTools == nil || !data.ParsedTools.CLIProxy {
		return nil
	}
	return getMCPCLIServerNames(data)
}

// generateMCPCLIMountStep generates the "Mount MCP servers as CLIs" workflow step.
// This step runs after the MCP gateway is started and creates executable CLI wrapper
// scripts for each MCP server in a read-only directory on $PATH.
func (c *Compiler) generateMCPCLIMountStep(yaml *strings.Builder, data *WorkflowData) {
	servers := getMCPCLIServerNames(data)
	if len(servers) == 0 {
		return
	}
	mcpCLIMountLog.Printf("Generating MCP CLI mount step for %d servers: %v", len(servers), servers)

	yaml.WriteString("      - name: Mount MCP servers as CLIs\n")
	yaml.WriteString("        id: mount-mcp-clis\n")
	yaml.WriteString("        continue-on-error: true\n")
	yaml.WriteString("        env:\n")
	if !enclavesEnabled(data) {
		yaml.WriteString("          MCP_GATEWAY_AGENT_ID: ${{ steps.start-mcp-gateway.outputs.gateway-agent-id }}\n")
	}
	yaml.WriteString("          MCP_GATEWAY_DOMAIN: ${{ steps.start-mcp-gateway.outputs.gateway-domain }}\n")
	yaml.WriteString("          MCP_GATEWAY_PORT: ${{ steps.start-mcp-gateway.outputs.gateway-port }}\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getActionPin("actions/github-script"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io);\n")
	yaml.WriteString("            const { main } = require('" + SetupActionDestination + "/mount_mcp_as_cli.cjs');\n")
	yaml.WriteString("            await main();\n")
}

// GetMCPCLIPathSetup returns a shell command that adds the MCP CLI bin directory
// to PATH inside the AWF container. This ensures CLI-mounted MCP servers are
// accessible as shell commands even though sudo's secure_path may strip the
// core.addPath() additions from $GITHUB_PATH.
//
// Returns an empty string if no MCP CLIs are configured, so callers can safely
// chain it with && without introducing empty commands.
func GetMCPCLIPathSetup(data *WorkflowData) string {
	if getMCPCLIServerNames(data) == nil {
		return ""
	}
	return `export PATH="${RUNNER_TEMP}/gh-aw/mcp-cli/bin:$PATH"`
}

// buildMCPCLIPromptSection returns a PromptSection describing the CLI tools available
// to the agent, or nil if there are no servers to mount.
// The prompt is loaded from actions/setup/md/mcp_cli_tools_prompt.md at runtime,
// with the __GH_AW_MCP_CLI_SERVERS_LIST__ placeholder substituted by the substitution step.
//
// The server list is computed at compile time from the workflow configuration.
// Each entry uses the `--help` convention so agents can discover tool signatures at runtime.
//
// The section is omitted when shell execution is fully disabled (tools.bash: false or
// tools.bash: []): the agent has no way to invoke the CLI wrappers, so advertising them
// would steer the model towards an unusable tool path (for example telling it to call the
// safeoutputs CLI from bash when only the safeoutputs MCP tools are reachable).
func buildMCPCLIPromptSection(data *WorkflowData) *PromptSection {
	if data != nil && data.BashDisabled {
		mcpCLIMountLog.Print("Skipping MCP CLI tools prompt section: bash is fully disabled")
		return nil
	}

	servers := getMCPCLIServerNames(data)
	if len(servers) == 0 {
		return nil
	}

	// Build a static list of CLI server entries from the compile-time known server names.
	// Using step outputs (e.g. steps.mount-mcp-clis.outputs.mcp-cli-servers-list) here
	// would reference a step from the agent job in the activation job's env block, which
	// is out of scope and triggers actionlint errors.
	lines := make([]string, len(servers))
	for i, server := range servers {
		lines[i] = fmt.Sprintf("- `%s` — run `%s --help` to see available tools", server, server)
	}

	promptFile := mcpCLIToolsPromptFile
	if slices.Contains(servers, constants.SafeOutputsMCPServerID.String()) {
		promptFile = mcpCLIToolsWithSafeOutputsPromptFile
	}

	return &PromptSection{
		Content: promptFile,
		IsFile:  true,
		EnvVars: map[string]string{
			"GH_AW_MCP_CLI_SERVERS_LIST": strings.Join(lines, "\n"),
		},
	}
}
