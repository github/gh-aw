package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var compilerYamlStepLifecycleLog = logger.New("workflow:compiler_yaml:steps")

func (c *Compiler) generatePreSteps(yaml *strings.Builder, data *WorkflowData) {
	writeStepsSection(yaml, data.PreSteps)
}

func (c *Compiler) generatePostSteps(yaml *strings.Builder, data *WorkflowData) {
	writeStepsSection(yaml, data.PostSteps)
}

func (c *Compiler) generatePreAgentSteps(yaml *strings.Builder, data *WorkflowData) {
	writeStepsSection(yaml, data.PreAgentSteps)
}

// writeStepsSection writes a steps section (pre-steps, pre-agent-steps, or post-steps) to the YAML builder,
// stripping the header line and normalising indentation to match the agent job step format:
// top-level items get 6-space indent (      - name:) and nested properties get 8-space indent (        run:).
func writeStepsSection(yaml *strings.Builder, stepsYAML string) {
	if stepsYAML == "" {
		return
	}
	// Inject zizmor ignore annotations before uses: lines from unverified creators.
	// YAML comments are stripped during parse/marshal, so they must be re-injected here
	// before the indentation-normalization pass below rewrites each line.
	stepsYAML = injectZizmorUnverifiedCreatorAnnotations(stepsYAML)
	lines := strings.Split(stepsYAML, "\n")
	var blockScalarState yamlBlockScalarState
	for _, line := range lines[1:] { // skip the "pre-steps:" / "pre-agent-steps:" / "post-steps:" header line
		// Update block-scalar state using the original source line (before the
		// 2-space strip below) so that indent-based entry/exit detection is correct.
		isBS := blockScalarState.update(line)
		if strings.TrimSpace(line) == "" {
			yaml.WriteString("\n")
			continue
		}
		// Normalise indentation: nested properties (2+ leading spaces) get an
		// 8-space indent; top-level step items get a 6-space indent.
		var prefix, content string
		if strings.HasPrefix(line, "  ") {
			prefix = "        "
			content = line[2:]
		} else {
			prefix = "      "
			content = line
		}
		appendYAMLLine(yaml, prefix, content, isBS)
	}
}

func (c *Compiler) generateCreateAwInfo(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine) {
	// Engine ID (prefer EngineConfig.ID, fallback to AI field for backwards compatibility)
	engineID := engine.GetID()
	if data.EngineConfig != nil && data.EngineConfig.ID != "" {
		engineID = data.EngineConfig.ID
	} else if data.AI != "" {
		engineID = data.AI
	}

	// Model - explicit config or runtime env var via vars context
	modelConfigured := data.Model != ""
	var modelEnvVar string
	if !modelConfigured {
		switch engineID {
		case "copilot":
			modelEnvVar = constants.EnvVarModelAgentCopilot
		case "claude":
			modelEnvVar = constants.EnvVarModelAgentClaude
		case "codex":
			modelEnvVar = constants.EnvVarModelAgentCodex
		case "custom":
			modelEnvVar = constants.EnvVarModelAgentCustom
		default:
			modelEnvVar = constants.EnvVarModelAgentCustom
		}
	}

	// Agent version - use the actual installation version (includes defaults)
	agentVersion := getInstallationVersion(data, engine, c.engineRegistry)

	// Version: prefer explicit engine config version, fall back to the installation version
	// so the run details always show the version being used rather than "(none)".
	version := agentVersion
	if data.EngineConfig != nil && data.EngineConfig.Version != "" {
		version = data.EngineConfig.Version
	}

	// Staged value from safe-outputs configuration
	stagedValue := "false"
	if data.SafeOutputs != nil && data.SafeOutputs.Staged != nil {
		stagedValue = data.SafeOutputs.Staged.String()
	}

	// Network configuration
	var allowedDomains []string
	firewallEnabled := false
	firewallVersion := ""
	if data.NetworkPermissions != nil {
		allowedDomains = data.NetworkPermissions.Allowed
	}
	if firewallConfig := getFirewallConfig(data); firewallConfig != nil {
		firewallEnabled = firewallConfig.Enabled
		firewallVersion = firewallConfig.Version
		if firewallEnabled && firewallVersion == "" {
			firewallVersion = string(constants.DefaultFirewallVersion)
		}
	}

	// Allowed domains as JSON array string
	domainsJSON := "[]"
	if len(allowedDomains) > 0 {
		b, _ := json.Marshal(allowedDomains) //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
		domainsJSON = string(b)
	}

	// MCP Gateway version
	mcpGatewayVersion := ""
	if data.SandboxConfig != nil && data.SandboxConfig.MCP != nil {
		mcpGatewayVersion = effectiveMCPGatewayVersion(data)
	} else if enclaveDynamicRepositoryPolicyEnabled(data) {
		mcpGatewayVersion = effectiveMCPGatewayVersion(data)
	}

	// Firewall type
	firewallType := ""
	if isFirewallEnabled(data) {
		firewallType = "squid"
	}

	// Sandbox agent runtime (e.g., "gvisor", "docker-sbx", "cloud-hypervisor"), stored in aw_info.json
	// for observability and used by the logs/audit --runtime filter.
	agentRuntime := ""
	if data.SandboxConfig != nil && data.SandboxConfig.Agent != nil {
		agentRuntime = string(data.SandboxConfig.Agent.Runtime)
	}

	compilerYamlStepLifecycleLog.Printf("Generating aw_info step: engine=%s, modelConfigured=%t, version=%s, firewallEnabled=%t, staged=%s, agentRuntime=%s", engineID, modelConfigured, version, firewallEnabled, stagedValue, agentRuntime)

	yaml.WriteString("      - name: Generate agentic run info\n")
	yaml.WriteString("        id: generate_aw_info\n")
	yaml.WriteString("        env:\n")
	fmt.Fprintf(yaml, "          GH_AW_INFO_ENGINE_ID: \"%s\"\n", engineID)
	fmt.Fprintf(yaml, "          GH_AW_INFO_ENGINE_NAME: \"%s\"\n", engine.GetDisplayName())
	if modelConfigured {
		fmt.Fprintf(yaml, "          GH_AW_INFO_MODEL: \"%s\"\n", data.Model)
	} else {
		// Use the engine's default model as fallback when neither explicit model nor
		// model variable is configured, so the run details show "agent" rather than "(none)".
		defaultModel := getDefaultAgentModel(engineID)
		defaultModelOverrideVar := getDefaultModelOverrideVar(engineID)
		if defaultModel != "" && defaultModelOverrideVar != "" {
			fmt.Fprintf(yaml, "          GH_AW_INFO_MODEL: %s\n", compilerenv.BuildModelOverrideExpression(modelEnvVar, defaultModelOverrideVar, defaultModel))
		} else if defaultModel != "" {
			fmt.Fprintf(yaml, "          GH_AW_INFO_MODEL: ${{ vars.%s || '%s' }}\n", modelEnvVar, defaultModel)
		} else if defaultModelOverrideVar != "" {
			fmt.Fprintf(yaml, "          GH_AW_INFO_MODEL: %s\n", compilerenv.BuildModelOverrideExpressionEmptyFallback(modelEnvVar, defaultModelOverrideVar))
		} else {
			fmt.Fprintf(yaml, "          GH_AW_INFO_MODEL: ${{ vars.%s || '' }}\n", modelEnvVar)
		}
	}
	fmt.Fprintf(yaml, "          GH_AW_INFO_VERSION: \"%s\"\n", version)
	fmt.Fprintf(yaml, "          GH_AW_INFO_AGENT_VERSION: \"%s\"\n", agentVersion)
	// CLI version only for released builds
	if IsReleasedVersion(c.version) {
		fmt.Fprintf(yaml, "          GH_AW_INFO_CLI_VERSION: \"%s\"\n", c.version)
	}
	fmt.Fprintf(yaml, "          GH_AW_INFO_WORKFLOW_NAME: \"%s\"\n", data.Name)
	fmt.Fprintf(yaml, "          GH_AW_INFO_EXPERIMENTAL: \"%t\"\n", engine.IsExperimental())
	fmt.Fprintf(yaml, "          GH_AW_INFO_SUPPORTS_TOOLS_ALLOWLIST: \"%t\"\n", engine.GetCapabilities().ToolsAllowlist)
	fmt.Fprintf(yaml, "          GH_AW_INFO_STAGED: \"%s\"\n", stagedValue)
	fmt.Fprintf(yaml, "          GH_AW_INFO_ALLOWED_DOMAINS: '%s'\n", domainsJSON)
	fmt.Fprintf(yaml, "          GH_AW_INFO_FIREWALL_ENABLED: \"%t\"\n", firewallEnabled)
	fmt.Fprintf(yaml, "          GH_AW_INFO_AWF_VERSION: \"%s\"\n", firewallVersion)
	fmt.Fprintf(yaml, "          GH_AW_INFO_AWMG_VERSION: \"%s\"\n", mcpGatewayVersion)
	fmt.Fprintf(yaml, "          GH_AW_INFO_FIREWALL_TYPE: \"%s\"\n", firewallType)
	fmt.Fprintf(yaml, "          GH_AW_INFO_AGENT_RUNTIME: \"%s\"\n", agentRuntime)
	if operationalValueGraderEnabled(data) {
		yaml.WriteString("          GH_AW_INFO_FETCH_RUN_CREATED_AT: \"true\"\n")
	}
	// Only emit the cache-memory flag when at least one cache is configured.
	if data.CacheMemoryConfig != nil && len(data.CacheMemoryConfig.Caches) > 0 {
		yaml.WriteString("          GH_AW_INFO_CACHE_MEMORY: \"true\"\n")
	}
	if data.Source != "" {
		fmt.Fprintf(yaml, "          GH_AW_INFO_FRONTMATTER_SOURCE: %q\n", data.Source)
		// Body-modified defaults to false at compile time; update flows may override this
		// signal when source/body drift is detected before execution.
		yaml.WriteString("          GH_AW_INFO_BODY_MODIFIED: \"false\"\n")
	}
	if data.FrontmatterEmoji != "" {
		fmt.Fprintf(yaml, "          GH_AW_INFO_FRONTMATTER_EMOJI: %q\n", data.FrontmatterEmoji)
	}
	// Always include strict mode flag for lockdown validation.
	// validateLockdownRequirements uses this to enforce strict: true for public repositories.
	// Use effectiveStrictMode to infer strictness from the source (frontmatter), not just the CLI flag.
	fmt.Fprintf(yaml, "          GH_AW_COMPILED_STRICT: \"%t\"\n", c.effectiveStrictMode(data.RawFrontmatter))
	// When a workflow_call trigger is present, pass the target_repo resolved by the
	// resolve-host-repo step so it can be stored in aw_info.json for observability.
	if hasWorkflowCallTrigger(data.On) && !data.InlinedImports {
		yaml.WriteString("          GH_AW_INFO_TARGET_REPO: ${{ steps.resolve-host-repo.outputs.target_repo }}\n")
	}
	// Include lockdown validation env vars when lockdown is explicitly enabled.
	// validateLockdownRequirements is called from generate_aw_info.cjs and uses these vars.
	githubTool, hasGitHub := data.Tools["github"]
	if hasGitHub && githubTool != false {
		toolConfig, _ := githubTool.(map[string]any)
		if hasGitHubLockdownExplicitlySet(toolConfig) && getGitHubLockdown(toolConfig) {
			yaml.WriteString("          GITHUB_MCP_LOCKDOWN_EXPLICIT: \"true\"\n")
			yaml.WriteString("          GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}\n")
			yaml.WriteString("          GH_AW_GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}\n")
			if customToken := getGitHubToken(toolConfig); customToken != "" {
				fmt.Fprintf(yaml, "          CUSTOM_GITHUB_TOKEN: %s\n", customToken)
			}
		}
	}
	// Embed the `models` overlay from frontmatter so the activation job can merge it with
	// the built-in models.json and write the combined catalog to /tmp/gh-aw/models.json.
	if len(data.ModelCosts) > 0 {
		if modelCostsJSON, err := json.Marshal(data.ModelCosts); err == nil {
			// Escape single quotes for YAML single-quoted scalar safety
			escapedModelCostsJSON := strings.ReplaceAll(string(modelCostsJSON), "'", "''")
			fmt.Fprintf(yaml, "          GH_AW_INFO_MODEL_COSTS: '%s'\n", escapedModelCostsJSON)
		}
	}
	if runtimeFeatures := runtimeVisibleFeatures(data.Features); len(runtimeFeatures) > 0 {
		if featuresJSON, err := json.Marshal(runtimeFeatures); err == nil {
			// Escape single quotes for YAML single-quoted scalar safety
			escapedFeaturesJSON := strings.ReplaceAll(string(featuresJSON), "'", "''")
			fmt.Fprintf(yaml, "          GH_AW_INFO_FEATURES: '%s'\n", escapedFeaturesJSON)
		}
	}
	if len(data.Skills) > 0 {
		if skillsJSON, err := json.Marshal(data.Skills); err == nil {
			escapedSkillsJSON := strings.ReplaceAll(string(skillsJSON), "'", "''")
			fmt.Fprintf(yaml, "          GH_AW_INFO_SKILLS: '%s'\n", escapedSkillsJSON)
		} else {
			compilerYamlStepLifecycleLog.Printf("Failed to marshal skills for GH_AW_INFO_SKILLS, engine will not receive skill list: %v", err)
		}
	}
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/generate_aw_info.cjs');\n")
	yaml.WriteString("            await main(core, context);\n")
}

func (c *Compiler) generateOutputCollectionStep(yaml *strings.Builder, data *WorkflowData) error {
	// Copy the raw safe-output NDJSON to a /tmp/gh-aw/ path so it can be included in the
	// unified agent artifact together with all other /tmp/gh-aw/ outputs.
	yaml.WriteString("      - name: Copy Safe Outputs\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}\n")
	yaml.WriteString("        run: |\n")
	fmt.Fprintf(yaml, "          mkdir -p /tmp/gh-aw\n")
	fmt.Fprintf(yaml, "          cp \"$GH_AW_SAFE_OUTPUTS\" /tmp/gh-aw/%s 2>/dev/null || true\n", constants.SafeOutputsFilename)

	yaml.WriteString("      - name: Ingest agent output\n")
	yaml.WriteString("        id: collect_output\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))

	// Add environment variables for JSONL validation
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}\n")

	// Config is written to file, not passed as env var

	// Add allowed domains configuration for sanitization
	// Use manually configured domains if available, otherwise compute from network configuration
	var domainsStr string
	if data.SafeOutputs != nil && len(data.SafeOutputs.AllowedDomains) > 0 {
		// allowed-domains: additional domains unioned with engine/network base set; supports ecosystem identifiers
		expanded, err := c.computeExpandedAllowedDomainsForSanitization(data)
		if err != nil {
			return err
		}
		domainsStr = expanded
	} else {
		// Fall back to computing from network configuration (same as firewall)
		computed, err := c.computeAllowedDomainsForSanitization(data)
		if err != nil {
			return err
		}
		domainsStr = computed
	}
	if domainsStr != "" {
		fmt.Fprintf(yaml, "          GH_AW_ALLOWED_DOMAINS: %q\n", domainsStr)
	}
	if data.SafeOutputs != nil && data.SafeOutputs.URLs != "" {
		fmt.Fprintf(yaml, "          GH_AW_SAFE_OUTPUTS_URLS: %q\n", data.SafeOutputs.URLs)
	}

	// Add allowed GitHub references configuration for reference escaping
	if data.SafeOutputs != nil && data.SafeOutputs.AllowGitHubReferences != nil {
		refsStr := strings.Join(data.SafeOutputs.AllowGitHubReferences, ",")
		fmt.Fprintf(yaml, "          GH_AW_ALLOWED_GITHUB_REFS: %q\n", refsStr)
	}

	// Add GitHub server URL and API URL for dynamic domain extraction
	// This allows the sanitization code to permit GitHub domains that vary by deployment
	yaml.WriteString("          GITHUB_SERVER_URL: ${{ github.server_url }}\n")
	yaml.WriteString("          GITHUB_API_URL: ${{ github.api_url }}\n")

	// Add command names for command trigger prevention in safe outputs
	if len(data.Command) > 0 {
		if commandsJSON, err := json.Marshal(data.Command); err == nil {
			fmt.Fprintf(yaml, "          GH_AW_COMMANDS: %q\n", string(commandsJSON))
		}
		if data.CommandPlaceholder != "" {
			fmt.Fprintf(yaml, "          GH_AW_COMMAND_PLACEHOLDER: %q\n", data.CommandPlaceholder)
		}
	}
	if len(data.LabelCommand) > 0 {
		if labelCommandsJSON, err := json.Marshal(data.LabelCommand); err == nil {
			fmt.Fprintf(yaml, "          GH_AW_LABEL_COMMANDS: %q\n", string(labelCommandsJSON))
		}
	}

	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Load script from external file using require()
	yaml.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/collect_ndjson_output.cjs');\n")
	yaml.WriteString("            await main();\n")

	return nil
}
