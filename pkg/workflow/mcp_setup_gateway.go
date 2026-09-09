package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

const mcpGatewayCustomEnvNamesVar = "GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES"
const mcpGatewayCustomEnvTransportPrefix = "GH_AW_MCP_GATEWAY_ENV_"
const mcpGatewayReservedEnvPrefix = "GH_AW_MCP_GATEWAY_"
const mcpGatewayCustomEnvMarker = "__GH_AW_MCP_GATEWAY_CUSTOM_ENV__"
const mcpGatewayConfiguredAgentIDVar = "GH_AW_MCP_GATEWAY_CONFIGURED_AGENT_ID"

var optionalPRHeadEnvVars = []string{
	"GH_AW_PR_HEAD_BASE_BRANCH",
	"GH_AW_PR_HEAD_BASE_SHA",
	"GH_AW_PR_HEAD_BASE_REPO",
	"GH_AW_PR_HEAD_BASE_PR_NUMBER",
	"GH_AW_PR_HEAD_BASE_REF",
	"GH_AW_PR_HEAD_REPO",
}

func generateMCPGatewaySetup(yaml *strings.Builder, tools map[string]any, mcpTools []string, engine CodingAgentEngine, workflowData *WorkflowData, hasAgenticWorkflows bool, safeOutputsInputEnvVars map[string]string) error { //nolint:largefunc // Existing setup generation preserves emitted step ordering.
	// If the engine provides an MCP config-adapter script (e.g. Goose), write it to disk
	// before starting the gateway so that start_mcp_gateway.cjs can execute it once the
	// gateway has produced its output configuration.
	if adapterProvider, ok := engine.(MCPConfigAdapterProvider); ok {
		if adapterStep := adapterProvider.GetMCPConfigAdapterWriteStep(); len(adapterStep) > 0 {
			for _, line := range adapterStep {
				yaml.WriteString(line)
				yaml.WriteByte('\n')
			}
		}
	}
	yaml.WriteString("      - name: Start MCP Gateway\n")
	yaml.WriteString("        id: start-mcp-gateway\n")
	ensureDefaultMCPGatewayConfig(workflowData)
	gatewayConfig := workflowData.SandboxConfig.MCP
	mcpEnvVars := collectMCPEnvironmentVariables(tools, mcpTools, workflowData, hasAgenticWorkflows)
	if auth, ok := jiraAuthConfig(tools); ok && auth["type"] == jiraAPITokenAuth {
		mcpEnvVars[jiraBasicAuthEnvVar] = ""
	}
	stepEnvVars := maps.Clone(mcpEnvVars)
	maps.Copy(stepEnvVars, jiraAPIAuthStepEnv(tools))
	customGatewayEnvNames := sanitizedGatewayEnvNames(gatewayConfig.Env)
	writeMCPGatewayStepEnvWithCustomGatewayEnvNames(yaml, stepEnvVars, safeOutputsInputEnvVars, gatewayConfig.Env, customGatewayEnvNames, gatewayConfig.AgentID)
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          set -eo pipefail\n")
	writeJiraAPIAuthPreparation(yaml, tools)
	yaml.WriteString("          mkdir -p \"${RUNNER_TEMP}/gh-aw/mcp-config\"\n")
	yaml.WriteString("          if [ -n \"${GITHUB_EVENT_PATH:-}\" ] && [ -r \"${GITHUB_EVENT_PATH}\" ]; then\n")
	yaml.WriteString("            GH_AW_SAFEOUTPUTS_EVENT_PATH=\"${RUNNER_TEMP}/gh-aw/safeoutputs/github_event.json\"\n")
	yaml.WriteString("            cp \"${GITHUB_EVENT_PATH}\" \"${GH_AW_SAFEOUTPUTS_EVENT_PATH}\"\n")
	yaml.WriteString("            export GITHUB_EVENT_PATH=\"${GH_AW_SAFEOUTPUTS_EVENT_PATH}\"\n")
	yaml.WriteString("          fi\n")
	if slices.Contains(mcpTools, "playwright") {
		yaml.WriteString("          mkdir -p /tmp/gh-aw/mcp-logs/playwright\n")
		yaml.WriteString("          chmod 777 /tmp/gh-aw/mcp-logs/playwright\n")
	}
	port, domain, payloadDir, payloadPathPrefix, payloadSizeThreshold := resolveMCPGatewayValues(workflowData, gatewayConfig)
	githubToolRaw, hasGitHub := tools["github"]
	githubTool, _ := githubToolRaw.(map[string]any)
	if err := writeMCPGatewayExports(yaml, writeMCPGatewayExportsOptions{
		engine:               engine,
		workflowData:         workflowData,
		gatewayConfig:        gatewayConfig,
		tools:                tools,
		hasGitHub:            hasGitHub,
		githubTool:           githubTool,
		port:                 port,
		domain:               domain,
		payloadDir:           payloadDir,
		payloadPathPrefix:    payloadPathPrefix,
		payloadSizeThreshold: payloadSizeThreshold,
	}); err != nil {
		return err
	}
	containerCmd := buildMCPGatewayContainerCommand(buildMCPGatewayContainerCommandOptions{
		engine:                  engine,
		workflowData:            workflowData,
		gatewayConfig:           gatewayConfig,
		mcpEnvVars:              mcpEnvVars,
		payloadDir:              payloadDir,
		payloadPathPrefix:       payloadPathPrefix,
		hasGitHub:               hasGitHub,
		githubTool:              githubTool,
		tools:                   tools,
		safeOutputsInputEnvVars: safeOutputsInputEnvVars,
		customGatewayEnvNames:   customGatewayEnvNames,
	})
	yaml.WriteString("          MCP_GATEWAY_UID=$(id -u 2>/dev/null || echo '0')\n")
	yaml.WriteString("          MCP_GATEWAY_GID=$(id -g 2>/dev/null || echo '0')\n")
	// Resolve Docker socket path and GID using the dedicated shell script.
	// The script handles override variables (GH_AW_DOCKER_SOCK_PATH, GH_AW_DOCKER_SOCK_GID),
	// DOCKER_HOST parsing, stat -Lc symlink following, and numeric validation.
	// See actions/setup/sh/resolve_docker_socket_gid.sh for implementation details.
	yaml.WriteString("          source \"${RUNNER_TEMP}/gh-aw/actions/resolve_docker_socket_gid.sh\"\n")
	cmdWithExpandableVars := buildDockerCommandWithExpandableVars(containerCmd)
	yaml.WriteString("          export MCP_GATEWAY_DOCKER_COMMAND=" + cmdWithExpandableVars + "\n")
	yaml.WriteString("          \n")
	return engine.RenderMCPConfig(yaml, tools, mcpTools, workflowData)
}

func writeMCPGatewayStepEnvWithCustomGatewayEnvNames(yaml *strings.Builder, mcpEnvVars map[string]string, safeOutputsInputEnvVars map[string]string, gatewayEnvVars map[string]string, customEnvVarNames []string, configuredAgentID string) {
	if len(mcpEnvVars) == 0 && len(safeOutputsInputEnvVars) == 0 && len(customEnvVarNames) == 0 && configuredAgentID == "" {
		return
	}
	yaml.WriteString("        env:\n")
	overriddenNames := make(map[string]struct{}, len(customEnvVarNames))
	for _, name := range customEnvVarNames {
		overriddenNames[name] = struct{}{}
	}
	// Write MCP env vars first (sorted)
	envVarNames := sliceutil.MapKeys(mcpEnvVars)
	sort.Strings(envVarNames)
	for _, envVarName := range envVarNames {
		if setutil.Contains(overriddenNames, envVarName) {
			continue
		}
		if isReservedMCPGatewayEnvVar(envVarName) {
			continue
		}
		fmt.Fprintf(yaml, "          %s: %s\n", envVarName, mcpEnvVars[envVarName])
	}
	// Write safe-outputs input env vars (sorted); these must also be present in the
	// runner step environment so the docker -e flag can forward them to the container.
	inputVarNames := sliceutil.SortedKeys(safeOutputsInputEnvVars)
	for _, envVarName := range inputVarNames {
		if setutil.Contains(overriddenNames, envVarName) {
			continue
		}
		if isReservedMCPGatewayEnvVar(envVarName) {
			continue
		}
		fmt.Fprintf(yaml, "          %s: %s\n", envVarName, safeOutputsInputEnvVars[envVarName])
	}
	// Use compiler-controlled transport names so special environment variables such
	// as BASH_ENV cannot affect the host shell before the run script starts.
	if len(customEnvVarNames) > 0 {
		customEnvNamesJSON, err := json.Marshal(customEnvVarNames)
		if err != nil {
			// Build-time invariant: customEnvVarNames is a []string, which json.Marshal
			// always serialises successfully; this branch is unreachable in practice.
			panic(fmt.Sprintf("BUG: failed to marshal MCP gateway environment variable names: %v", err))
		}
		yaml.WriteString(formatYAMLEnv("          ", mcpGatewayCustomEnvNamesVar, string(customEnvNamesJSON)))
		for i, envVarName := range customEnvVarNames {
			yaml.WriteString(formatYAMLEnv("          ", mcpGatewayCustomEnvTransportName(i), gatewayEnvVars[envVarName]))
		}
	}
	if configuredAgentID != "" {
		yaml.WriteString(formatYAMLEnv("          ", mcpGatewayConfiguredAgentIDVar, configuredAgentID))
	}
}

// sanitizedGatewayEnvNames returns the sorted subset of user-supplied gateway
// environment variable names that are safe to emit.
//
// Names are already constrained by the frontmatter schema and by
// validateSandboxConfig, but this is the last boundary before a name is handed
// to the runtime launcher (which turns it into a `docker run -e NAME=value`
// argument). Re-checking here guarantees that a name can never carry shell or
// Docker argument metacharacters, even if an earlier validation path is
// bypassed. Values need no such filtering: they are transported through
// compiler-controlled GH_AW_MCP_GATEWAY_ENV_<n> variables and never appear in
// the generated shell script or in the Docker command string.
func sanitizedGatewayEnvNames(gatewayEnvVars map[string]string) []string {
	names := make([]string, 0, len(gatewayEnvVars))
	for _, name := range sliceutil.SortedKeys(gatewayEnvVars) {
		if isReservedMCPGatewayEnvVar(name) {
			mcpSetupGeneratorLog.Printf("Skipping reserved MCP gateway environment variable name: %s", name)
			continue
		}
		if !mcpGatewayEnvNamePattern.MatchString(name) {
			mcpSetupGeneratorLog.Printf("Skipping invalid MCP gateway environment variable name: %s", name)
			continue
		}
		names = append(names, name)
	}
	return names
}

func mcpGatewayCustomEnvTransportName(index int) string {
	return fmt.Sprintf("%s%d", mcpGatewayCustomEnvTransportPrefix, index)
}

func isReservedMCPGatewayEnvVar(name string) bool {
	return strings.HasPrefix(name, mcpGatewayReservedEnvPrefix)
}

func resolveMCPGatewayValues(workflowData *WorkflowData, gatewayConfig *MCPGatewayRuntimeConfig) (int, string, string, string, int) {
	port := gatewayConfig.Port
	if port == 0 {
		port = int(DefaultMCPGatewayPort)
	}
	domain := gatewayConfig.Domain
	if domain == "" {
		if workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled {
			domain = "localhost"
		} else if isDockerSbxRuntime(workflowData) {
			// Docker sbx microVMs reach host-published services via host.docker.internal
			// (the Docker bridge gateway). Use this as the MCP gateway domain so that the
			// CLI wrapper scripts generated inside the microVM point to the correct host.
			domain = "host.docker.internal"
		} else if isAWFNetworkIsolationEnabled(workflowData) {
			domain = "awmg-mcpg"
		} else {
			domain = "host.docker.internal"
		}
	}
	payloadDir := gatewayConfig.PayloadDir
	if payloadDir == "" {
		payloadDir = constants.DefaultMCPGatewayPayloadDir
	}
	payloadSizeThreshold := gatewayConfig.PayloadSizeThreshold
	if payloadSizeThreshold == 0 {
		payloadSizeThreshold = constants.DefaultMCPGatewayPayloadSizeThreshold
	}
	return port, domain, payloadDir, gatewayConfig.PayloadPathPrefix, payloadSizeThreshold
}

// writeMCPGatewayExportsOptions holds configuration for writeMCPGatewayExports.
type writeMCPGatewayExportsOptions struct {
	engine               CodingAgentEngine
	workflowData         *WorkflowData
	gatewayConfig        *MCPGatewayRuntimeConfig
	tools                map[string]any
	hasGitHub            bool
	githubTool           map[string]any
	port                 int
	domain               string
	payloadDir           string
	payloadPathPrefix    string
	payloadSizeThreshold int
}

func writeMCPGatewayExports(yaml *strings.Builder, opts writeMCPGatewayExportsOptions) error { //nolint:largefunc // Existing export generation keeps related runtime variables together.
	engine := opts.engine
	workflowData := opts.workflowData
	gatewayConfig := opts.gatewayConfig
	tools := opts.tools
	hasGitHub := opts.hasGitHub
	githubTool := opts.githubTool
	port := opts.port
	domain := opts.domain
	payloadDir := opts.payloadDir
	payloadPathPrefix := opts.payloadPathPrefix
	payloadSizeThreshold := opts.payloadSizeThreshold
	yaml.WriteString("          \n")
	yaml.WriteString("          # Export gateway environment variables for MCP config and gateway script\n")
	yaml.WriteString("          export MCP_GATEWAY_PORT=\"" + strconv.Itoa(port) + "\"\n")
	yaml.WriteString("          export MCP_GATEWAY_DOMAIN=\"" + domain + "\"\n")
	// MCP_GATEWAY_HOST_DOMAIN is the domain used by host-side clients (e.g. Gemini CLI).
	// When MCP_GATEWAY_DOMAIN is host.docker.internal (only reachable from containers),
	// or when network isolation is active (gateway on bridge; host reaches it via the
	// published 127.0.0.1 port), use localhost instead; otherwise inherit the domain.
	// Exception: for microVM runtimes, the CLI wrappers run INSIDE the microVM, so they must
	// also use host.docker.internal (not localhost) to reach the published gateway port.
	// Exception: for Gemini under network isolation, use the topology hostname (awmg-mcpg)
	// instead of localhost. The Gemini CLI honors HTTP_PROXY but ignores NO_PROXY, so
	// localhost:8080 would be tunneled through the squid egress proxy and denied. The
	// awmg-mcpg topology hostname is already in the firewall allowlist.
	hostDomain := domain
	if isDockerSbxRuntime(workflowData) {
		hostDomain = "host.docker.internal"
	} else if engine.GetID() == "gemini" && isAWFNetworkIsolationEnabled(workflowData) {
		// domain is "awmg-mcpg" when network isolation is active; preserve it.
		hostDomain = domain
	} else if domain == "host.docker.internal" || isAWFNetworkIsolationEnabled(workflowData) {
		hostDomain = "localhost"
	}
	yaml.WriteString("          export MCP_GATEWAY_HOST_DOMAIN=\"" + hostDomain + "\"\n")
	if gatewayConfig.AgentID == "" {
		yaml.WriteString("          MCP_GATEWAY_AGENT_ID=$(openssl rand -base64 45 | tr -d '/+=')\n")
		yaml.WriteString("          echo \"::add-mask::${MCP_GATEWAY_AGENT_ID}\"\n")
		yaml.WriteString("          export MCP_GATEWAY_AGENT_ID\n")
	} else {
		yaml.WriteString("          export MCP_GATEWAY_AGENT_ID=\"${" + mcpGatewayConfiguredAgentIDVar + "}\"\n")
		yaml.WriteString("          echo \"::add-mask::${MCP_GATEWAY_AGENT_ID}\"\n")
	}
	yaml.WriteString("          export MCP_GATEWAY_PAYLOAD_DIR=\"" + payloadDir + "\"\n")
	yaml.WriteString("          mkdir -p \"${MCP_GATEWAY_PAYLOAD_DIR}\"\n")
	if payloadPathPrefix != "" {
		yaml.WriteString("          export MCP_GATEWAY_PAYLOAD_PATH_PREFIX=\"" + payloadPathPrefix + "\"\n")
	}
	yaml.WriteString("          export MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD=\"" + strconv.Itoa(payloadSizeThreshold) + "\"\n")
	// Allow read-write access to the host paths our built-in MCP servers mount
	// (workspace, safe-outputs runtime dir, temp dir); see buildMCPGatewayAllowedMountRoots.
	yaml.WriteString("          export MCP_GATEWAY_ALLOWED_MOUNT_ROOTS=\"" + buildMCPGatewayAllowedMountRoots(tools, gatewayConfig) + "\"\n")
	// These values are populated only for pull request checkouts, but the safe-outputs
	// server config always references them. Export empty defaults so Docker forwards
	// defined values and strict gateway config expansion also works for other triggers.
	for _, envVar := range optionalPRHeadEnvVars {
		fmt.Fprintf(yaml, "          export %s=\"${%s:-}\"\n", envVar, envVar)
	}
	if enclavesEnabled(workflowData) {
		yaml.WriteString("          AWF_ENCLAVE_MCP_CAPABILITY=$(openssl rand -hex 32)\n")
		yaml.WriteString("          echo \"::add-mask::${AWF_ENCLAVE_MCP_CAPABILITY}\"\n")
		yaml.WriteString("          export AWF_ENCLAVE_MCP_CAPABILITY\n")
		yaml.WriteString("          export AWF_ENCLAVE_MCP_GATEWAY_IDENTITY=\"gh-aw-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}-${GITHUB_JOB}\"\n")
		yaml.WriteString("          export AWF_ENCLAVE_MCP_GATEWAY_CONTAINER=\"awmg-mcpg\"\n")
		yaml.WriteString("          export AWF_ENCLAVE_MCP_GATEWAY_ENDPOINT=\"http://localhost:${MCP_GATEWAY_PORT}/mcp/awf-enclave\"\n")
		yaml.WriteString("          export AWF_ENCLAVE_MCP_READINESS_TIMEOUT_MS=\"120000\"\n")
		if enclaveDynamicRepositoryPolicyEnabled(workflowData) {
			yaml.WriteString("          AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY=$(openssl rand -hex 32)\n")
			yaml.WriteString("          echo \"::add-mask::${AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY}\"\n")
			yaml.WriteString("          export AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY\n")
			// mcpg's github-repository-delegation-v1 controller is bootstrapped as one
			// atomic configuration: envelope, control key, state path, generation, and a
			// control listener bound separately from the executor-facing data plane.
			yaml.WriteString("          export MCP_GATEWAY_DELEGATION_CONTROL_KEY=\"${AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY}\"\n")
			yaml.WriteString("          mkdir -p \"" + enclaveDelegationStateDir + "\"\n")
			yaml.WriteString("          chmod 700 \"" + enclaveDelegationStateDir + "\"\n")
			yaml.WriteString("          export MCP_GATEWAY_DELEGATION_STATE_PATH=\"" + enclaveDelegationStateDir + "/state.json\"\n")
			yaml.WriteString("          export MCP_GATEWAY_DELEGATION_GENERATION=\"" + enclaveDelegationGeneration + "\"\n")
			// The in-container listener must bind to a bridge-reachable address in
			// bridge (network-isolation) mode: Docker's -p publish NATs the host port
			// to the container's bridge IP, which cannot reach a listener bound to the
			// container's own 127.0.0.1. In --network host mode the container shares
			// the host's network namespace, so 127.0.0.1 works directly and is kept
			// tightest-scoped. The host-side publication (see buildMCPGatewayContainerCommand)
			// stays on 127.0.0.1 either way, so the control port is never reachable from
			// outside the host regardless of the in-container bind address. Note this
			// says nothing about container-to-container reachability: in bridge mode a
			// peer co-attached to a network mcpg joins can dial this 0.0.0.0 listener on
			// the container IP directly. The capability is what guards the control API;
			// see the -p comment in buildMCPGatewayContainerCommand and github/gh-aw#59268.
			controlListenHost := "127.0.0.1"
			if isAWFNetworkIsolationEnabled(workflowData) {
				controlListenHost = "0.0.0.0"
			}
			yaml.WriteString("          export MCP_GATEWAY_DELEGATION_CONTROL_LISTEN=\"" + controlListenHost + ":" + strconv.Itoa(port+enclaveDelegationControlPortOffset) + "\"\n")
			if dynamicEnclave := enclaveDynamicRepositoryPolicyConfig(workflowData); dynamicEnclave != nil {
				expiryScript, err := buildDynamicEnclaveExpiryScript(workflowData, dynamicEnclave)
				if err != nil {
					return err
				}
				yaml.WriteString(expiryScript)
				envelopeJSON, err := json.Marshal(buildMCPGatewayDelegationEnvelope(dynamicEnclave))
				if err != nil {
					return fmt.Errorf("failed to marshal MCP gateway delegation envelope: %w", err)
				}
				yaml.WriteString("          export MCP_GATEWAY_DELEGATION_ENVELOPE=" + shellEscapeArgWithVarsPreserved(string(envelopeJSON), enclaveDelegationExpiresAtEnv, "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT") + "\n")
			}
			// AWF-only private handoff: the control endpoint is never published to the
			// primary-agent network, the enclave executor network, or the general MCP
			// route, and is kept out of the primary agent's environment via --exclude-env.
			// The pinned mcpg controller serves control operations under the fixed
			// enclaveDelegationControlAPIBasePath, not a path keyed by controller name.
			fmt.Fprintf(yaml, "          export AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_ENDPOINT=\"http://127.0.0.1:%d%s/%s\"\n", port+enclaveDelegationControlPortOffset, enclaveDelegationControlAPIBasePath, enclaveDynamicController)
		}
		if enclaveGitHubIssuesEnabled(workflowData) {
			yaml.WriteString("          AWF_ENCLAVE_GITHUB_MCP_AGENT_ID=$(openssl rand -base64 45 | tr -d '/+=')\n")
			yaml.WriteString("          echo \"::add-mask::${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}\"\n")
			yaml.WriteString("          export AWF_ENCLAVE_GITHUB_MCP_AGENT_ID\n")
		}
		yaml.WriteString("          # The eager checker runs inside start_mcp_gateway.cjs in this step.\n")
		fmt.Fprintf(yaml, "          export %s=%q\n", enclaveMCPDeferredServersEnv, enclaveMCPServerName)
		// Masked values may be suppressed as GitHub Actions step outputs. Enclave host setup
		// therefore carries the gateway agent ID through the same GITHUB_ENV channel as its other
		// AWF-only handoffs; --exclude-env keeps it out of the primary agent.
		yaml.WriteString("          {\n")
		yaml.WriteString("            printf '%s=%s\\n' MCP_GATEWAY_AGENT_ID \"$MCP_GATEWAY_AGENT_ID\"\n")
		yaml.WriteString("            printf '%s=%s\\n' AWF_ENCLAVE_MCP_CAPABILITY \"$AWF_ENCLAVE_MCP_CAPABILITY\"\n")
		yaml.WriteString("            printf '%s=%s\\n' AWF_ENCLAVE_MCP_GATEWAY_IDENTITY \"$AWF_ENCLAVE_MCP_GATEWAY_IDENTITY\"\n")
		yaml.WriteString("            printf '%s=%s\\n' AWF_ENCLAVE_MCP_GATEWAY_CONTAINER \"$AWF_ENCLAVE_MCP_GATEWAY_CONTAINER\"\n")
		yaml.WriteString("            printf '%s=%s\\n' AWF_ENCLAVE_MCP_GATEWAY_ENDPOINT \"$AWF_ENCLAVE_MCP_GATEWAY_ENDPOINT\"\n")
		yaml.WriteString("            printf '%s=%s\\n' AWF_ENCLAVE_MCP_READINESS_TIMEOUT_MS \"$AWF_ENCLAVE_MCP_READINESS_TIMEOUT_MS\"\n")
		if enclaveDynamicRepositoryPolicyEnabled(workflowData) {
			yaml.WriteString("            printf '%s=%s\\n' AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY \"$AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY\"\n")
			yaml.WriteString("            printf '%s=%s\\n' AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_ENDPOINT \"$AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_ENDPOINT\"\n")
		}
		if enclaveGitHubIssuesEnabled(workflowData) {
			yaml.WriteString("            printf '%s=%s\\n' AWF_ENCLAVE_GITHUB_MCP_AGENT_ID \"$AWF_ENCLAVE_GITHUB_MCP_AGENT_ID\"\n")
		}
		yaml.WriteString("          } >> \"$GITHUB_ENV\"\n")
	}
	yaml.WriteString("          export DEBUG=\"*\"\n")
	yaml.WriteString("          \n")
	yaml.WriteString("          export GH_AW_ENGINE=\"" + engine.GetID() + "\"\n")
	if adapterProvider, ok := engine.(MCPConfigAdapterProvider); ok {
		if adapterFilename := adapterProvider.GetMCPConfigAdapterFilename(); adapterFilename != "" {
			yaml.WriteString("          export GH_AW_MCP_CONFIG_ADAPTER=\"" + adapterFilename + "\"\n")
		}
	}
	if cliServers := getMCPCLIExcludeFromAgentConfig(workflowData); len(cliServers) > 0 {
		cliServersJSON, err := json.Marshal(cliServers)
		if err == nil {
			escapedCLIServersJSON := shellEscapeArg(string(cliServersJSON))
			yaml.WriteString("          export GH_AW_MCP_CLI_SERVERS=" + escapedCLIServersJSON + "\n")
		}
	}
	if hasGitHub && getGitHubType(githubTool) == GitHubMCPModeRemote && engine.GetID() == "copilot" {
		yaml.WriteString("          export GITHUB_PERSONAL_ACCESS_TOKEN=\"$GITHUB_MCP_SERVER_TOKEN\"\n")
	}
	return nil
}

// buildMCPGatewayContainerCommandOptions holds configuration for buildMCPGatewayContainerCommand.
type buildMCPGatewayContainerCommandOptions struct {
	engine                  CodingAgentEngine
	workflowData            *WorkflowData
	gatewayConfig           *MCPGatewayRuntimeConfig
	mcpEnvVars              map[string]string
	payloadDir              string
	payloadPathPrefix       string
	hasGitHub               bool
	githubTool              map[string]any
	tools                   map[string]any
	safeOutputsInputEnvVars map[string]string
	customGatewayEnvNames   []string
}

func buildMCPGatewayContainerCommand(opts buildMCPGatewayContainerCommandOptions) string { //nolint:largefunc // Existing docker command assembly keeps flag ordering stable.
	engine := opts.engine
	workflowData := opts.workflowData
	gatewayConfig := opts.gatewayConfig
	mcpEnvVars := opts.mcpEnvVars
	payloadDir := opts.payloadDir
	payloadPathPrefix := opts.payloadPathPrefix
	hasGitHub := opts.hasGitHub
	githubTool := opts.githubTool
	tools := opts.tools
	safeOutputsInputEnvVars := opts.safeOutputsInputEnvVars
	customGatewayEnvNames := opts.customGatewayEnvNames
	containerImage := gatewayConfig.Container
	if gatewayConfig.Version != "" {
		containerImage += ":" + gatewayConfig.Version
	} else {
		containerImage += ":" + effectiveMCPGatewayVersion(workflowData)
	}
	// Apply container_pins mapping from aw.json so the runtime docker run command
	// targets the redirected registry (e.g. an internal mirror) rather than the
	// default public registry.
	containerImage = applyContainerPinMappingFromData(containerImage, workflowData)
	var containerCmd strings.Builder
	// Pre-size the builder to avoid reallocations. The base flags from
	// appendMCPGatewayBaseEnvFlags alone write ~2KB of -e flags; allocating
	// 2048 bytes upfront covers the common case without overcommitting.
	containerCmd.Grow(2048)
	containerCmd.WriteString("docker run -i --rm")
	if isAWFNetworkIsolationEnabled(workflowData) {
		containerCmd.WriteString(" --network bridge")
		if isDockerSbxRuntime(workflowData) {
			// Docker sbx microVMs: publish to 0.0.0.0 so the guest can reach the gateway via
			// host.docker.internal (the Docker bridge gateway, 172.17.0.1).
			containerCmd.WriteString(" -p 0.0.0.0:${MCP_GATEWAY_PORT}:${MCP_GATEWAY_PORT}")
		} else {
			// Publish the gateway port to the host so host-side clients (e.g. Gemini CLI)
			// can reach the gateway at localhost:${MCP_GATEWAY_PORT}.
			containerCmd.WriteString(" -p 127.0.0.1:${MCP_GATEWAY_PORT}:${MCP_GATEWAY_PORT}")
		}
		if enclaveDynamicRepositoryPolicyEnabled(workflowData) {
			// Unlike the data plane above, this -p flag always publishes to the host's
			// 127.0.0.1, never 0.0.0.0, so the control port is not reachable from
			// outside the runner and AWF reaches it over host loopback.
			//
			// This publication scope is deliberately NOT relied on as a
			// container-to-container isolation boundary, and must not be described as
			// one. The in-container bind address (the right-hand side of
			// MCP_GATEWAY_DELEGATION_CONTROL_LISTEN, set in writeMCPGatewayExports) is
			// 0.0.0.0 in bridge mode because Docker NATs a published port to the
			// container's bridge IP and a container-local 127.0.0.1 bind would be
			// unreachable. A peer co-attached to any network mcpg joins can therefore
			// address the container IP and this port directly, without traversing the
			// published host port -- co-attachment, not publication scope, determines
			// container-to-container reachability.
			//
			// The enforced control on this listener is authentication, by design (see
			// github/gh-aw#59268): every request must present the 256-bit AWF-only
			// capability minted in writeMCPGatewayExports, which is never placed in any
			// primary-agent, enclave-executor, or model-sidecar environment or mount.
			// mcpg verifies it in constant time against a stored SHA-256 digest, before
			// any control routing, on a handler kept separate from the executor-facing
			// data plane, so a valid executor bearer cannot reach control operations.
			containerCmd.WriteString(" -p 127.0.0.1:${MCP_GATEWAY_DELEGATION_CONTROL_LISTEN#*:}:${MCP_GATEWAY_DELEGATION_CONTROL_LISTEN#*:}")
		}
	} else {
		containerCmd.WriteString(" --network host")
	}
	containerCmd.WriteString(" --name awmg-mcpg")
	if enclavesEnabled(workflowData) {
		containerCmd.WriteString(" --label " + enclaveMCPGatewayRunLabel + "=${AWF_ENCLAVE_MCP_GATEWAY_IDENTITY}")
	}
	if !isAWFNetworkIsolationEnabled(workflowData) {
		containerCmd.WriteString(" --add-host host.docker.internal:127.0.0.1")
	} else if shouldRewriteLocalhostToDocker(workflowData) {
		// In bridge (network-isolation) mode the container's loopback differs from the
		// host's, so host.docker.internal:127.0.0.1 would not resolve to the host.
		// Use host-gateway (Docker 20.10+) instead so the gateway container can reach
		// any host-side server (mcp-scripts HTTP server, custom HTTP MCP tools with
		// localhost URLs) that is running directly on the runner host.
		containerCmd.WriteString(" --add-host host.docker.internal:host-gateway")
	}
	containerCmd.WriteString(" --user ${MCP_GATEWAY_UID}:${MCP_GATEWAY_GID}")
	containerCmd.WriteString(" --group-add ${DOCKER_SOCK_GID}")
	containerCmd.WriteString(" -v ${DOCKER_SOCK_PATH}:/var/run/docker.sock")
	appendMCPGatewayBaseEnvFlags(&containerCmd, payloadPathPrefix)
	appendMCPGatewayConditionalEnvFlags(&containerCmd, workflowData, engine, hasGitHub, githubTool, tools)
	appendMCPGatewaySafeOutputsInputEnvFlags(&containerCmd, safeOutputsInputEnvVars)
	appendMCPGatewayCustomAndHTTPEnvFlagsWithCustomGatewayEnvNames(&containerCmd, workflowData, customGatewayEnvNames, mcpEnvVars, hasGitHub, githubTool, tools, engine)
	if payloadDir != "" {
		containerCmd.WriteString(" -v " + payloadDir + ":" + payloadDir + ":rw")
	}
	if enclaveDynamicRepositoryPolicyEnabled(workflowData) {
		// Protected, persistent mount for mcpg delegation controller state so that it
		// survives an in-run restart; restricted to owner-only permissions on the host.
		containerCmd.WriteString(" -v " + enclaveDelegationStateDir + ":" + enclaveDelegationStateDir + ":rw")
	}
	for _, mount := range gatewayConfig.Mounts {
		containerCmd.WriteString(" -v " + mount)
	}
	if gatewayConfig.Entrypoint != "" {
		containerCmd.WriteString(" --entrypoint " + shellEscapeArg(gatewayConfig.Entrypoint))
	}
	containerCmd.WriteString(" " + containerImage)
	for _, arg := range gatewayConfig.EntrypointArgs {
		containerCmd.WriteString(" " + shellEscapeArg(arg))
	}
	for _, arg := range gatewayConfig.Args {
		containerCmd.WriteString(" " + shellEscapeArg(arg))
	}
	return containerCmd.String()
}

func appendMCPGatewayBaseEnvFlags(containerCmd *strings.Builder, payloadPathPrefix string) {
	containerCmd.WriteString(" -e MCP_GATEWAY_PORT")
	containerCmd.WriteString(" -e MCP_GATEWAY_DOMAIN")
	containerCmd.WriteString(" -e MCP_GATEWAY_AGENT_ID")
	containerCmd.WriteString(" -e MCP_GATEWAY_PAYLOAD_DIR")
	if payloadPathPrefix != "" {
		containerCmd.WriteString(" -e MCP_GATEWAY_PAYLOAD_PATH_PREFIX")
	}
	containerCmd.WriteString(" -e MCP_GATEWAY_PAYLOAD_SIZE_THRESHOLD")
	// Override DOCKER_HOST inside the gateway to match the fixed mount destination,
	// regardless of what the runner's DOCKER_HOST was (custom path, tcp://, etc.).
	containerCmd.WriteString(" -e DOCKER_HOST=unix:///var/run/docker.sock")
	containerCmd.WriteString(" -e DEBUG")
	containerCmd.WriteString(" -e MCP_GATEWAY_LOG_DIR")
	containerCmd.WriteString(" -e GH_AW_MCP_LOG_DIR")
	containerCmd.WriteString(" -e GH_AW_SAFE_OUTPUTS")
	containerCmd.WriteString(" -e GH_AW_SAFE_OUTPUTS_CONFIG_PATH")
	containerCmd.WriteString(" -e GH_AW_SAFE_OUTPUTS_TOOLS_PATH")
	for _, envVar := range optionalPRHeadEnvVars {
		containerCmd.WriteString(" -e " + envVar)
	}
	containerCmd.WriteString(" -e " + compilerenv.PolicyAllowCreatePullRequest)
	containerCmd.WriteString(" -e GH_AW_ASSETS_BRANCH")
	containerCmd.WriteString(" -e GH_AW_ASSETS_MAX_SIZE_KB")
	containerCmd.WriteString(" -e GH_AW_ASSETS_ALLOWED_EXTS")
	containerCmd.WriteString(" -e DEFAULT_BRANCH")
	containerCmd.WriteString(" -e GITHUB_MCP_SERVER_TOKEN")
	containerCmd.WriteString(" -e GITHUB_MCP_GUARD_MIN_INTEGRITY")
	containerCmd.WriteString(" -e GITHUB_MCP_GUARD_REPOS")
	containerCmd.WriteString(" -e " + sinkVisibilityEnvVar)
	containerCmd.WriteString(" -e GITHUB_REPOSITORY")
	containerCmd.WriteString(" -e GITHUB_SERVER_URL")
	containerCmd.WriteString(" -e GITHUB_SHA")
	containerCmd.WriteString(" -e GITHUB_WORKSPACE")
	containerCmd.WriteString(" -e GITHUB_TOKEN")
	containerCmd.WriteString(" -e GITHUB_RUN_ID")
	containerCmd.WriteString(" -e GITHUB_RUN_NUMBER")
	containerCmd.WriteString(" -e GITHUB_RUN_ATTEMPT")
	containerCmd.WriteString(" -e GITHUB_JOB")
	containerCmd.WriteString(" -e GITHUB_ACTION")
	containerCmd.WriteString(" -e GITHUB_EVENT_NAME")
	containerCmd.WriteString(" -e GITHUB_EVENT_PATH")
	containerCmd.WriteString(" -e GITHUB_ACTOR")
	containerCmd.WriteString(" -e GITHUB_ACTOR_ID")
	containerCmd.WriteString(" -e GITHUB_TRIGGERING_ACTOR")
	containerCmd.WriteString(" -e GITHUB_WORKFLOW")
	containerCmd.WriteString(" -e GITHUB_WORKFLOW_REF")
	containerCmd.WriteString(" -e GITHUB_WORKFLOW_SHA")
	containerCmd.WriteString(" -e GITHUB_REF")
	containerCmd.WriteString(" -e GITHUB_REF_NAME")
	containerCmd.WriteString(" -e GITHUB_REF_TYPE")
	containerCmd.WriteString(" -e GITHUB_HEAD_REF")
	containerCmd.WriteString(" -e GITHUB_BASE_REF")
	containerCmd.WriteString(" -e RUNNER_TEMP")
	containerCmd.WriteString(" -e RUNNER_TOOL_CACHE")
	containerCmd.WriteString(" -e MCP_GATEWAY_ALLOWED_MOUNT_ROOTS")
}

// buildMCPGatewayAllowedMountRoots computes the value of the gateway's
// MCP_GATEWAY_ALLOWED_MOUNT_ROOTS environment variable. Since gh-aw-mcpg
// enforces a trusted host-path mount policy before launching containerized
// backend MCP servers (safe-outputs, agentic-workflows, custom mcp-servers, ...),
// and defaults to read-only access for $GITHUB_WORKSPACE and the working
// directory, we must explicitly allow read-write access to the host paths our
// built-in MCP servers mount (workspace, safe-outputs runtime dir, temp dir)
// plus every other host mount surface the compiler supports: custom gateway
// mounts (sandbox.mcp.mounts), per-server "mounts" arrays (mcp-servers.<name>.mounts,
// tools.github.mounts, ...), and "-v"/"--volume" flags embedded in a server's
// "args"/"entrypointArgs". Once this variable is set, mcpg treats it as the
// complete policy, so any host mount the compiler configures for a backend MCP
// server must be reflected here or that server fails to register with a
// "mount policy violation" error.
func buildMCPGatewayAllowedMountRoots(tools map[string]any, gatewayConfig *MCPGatewayRuntimeConfig) string {
	rootModes := make(map[string]string)
	addRoot := func(path, mode string) {
		if path == "" {
			return
		}
		if existing, ok := rootModes[path]; ok && (existing == "rw" || mode == "ro") {
			return
		}
		rootModes[path] = mode
	}
	addMount := func(mount string) {
		source, mode, ok := parseMCPGatewayAllowlistMount(mount)
		if !ok {
			return
		}
		addRoot(source, mode)
	}

	// Built-in MCP servers (safe-outputs, agentic-workflows) mount these paths;
	// see mcp_renderer_builtin.go and constants.Default*Mount. Only the
	// safe-outputs runtime subdirectory needs read-write access; the rest of the
	// gh-aw runtime tree (generated action scripts, etc.) stays read-only so
	// backend MCP containers cannot tamper with it.
	addRoot("${GITHUB_WORKSPACE}", "rw")
	addRoot(constants.GhAwRootDirShell, "ro")
	addRoot(constants.GhAwRootDirShell+"/safeoutputs", "rw")
	addRoot("/tmp", "rw")
	addRoot(constants.GhCLIPath, "ro")

	if gatewayConfig != nil {
		for _, mount := range gatewayConfig.Mounts {
			addMount(mount)
		}
	}

	for _, mount := range collectMCPServerConfiguredMounts(tools) {
		addMount(mount)
	}

	roots := sliceutil.SortedKeys(rootModes)
	entries := make([]string, 0, len(roots))
	for _, root := range roots {
		entries = append(entries, root+":"+rootModes[root])
	}
	return strings.Join(entries, ",")
}

// parseMCPGatewayAllowlistMount parses a "source:dest[:mode]" mount string (Docker
// bind-mount / MCP Gateway mount syntax) and returns the host source path and the
// effective access mode. Docker treats a mount without an explicit mode as
// read-write, so mode-less mounts must not be downgraded to "ro" here.
//
// Mount sources may use a backslash-escaped "\${VAR}" form (e.g. in imported
// partials, to survive GitHub Actions expression interpolation during import
// merging); the gateway/runtime unescapes this to "${VAR}" before substitution,
// so we normalize it here too, or the allowlist would gain a spurious entry
// that never matches the real mounted path.
func parseMCPGatewayAllowlistMount(mount string) (string, string, bool) {
	mount = strings.ReplaceAll(mount, `\$`, "$")
	parts := strings.SplitN(mount, ":", 3)
	if len(parts) < 2 || parts[0] == "" {
		return "", "", false
	}
	mode := "rw"
	if len(parts) == 3 && parts[2] == "ro" {
		mode = "ro"
	}
	return parts[0], mode, true
}

// collectMCPServerConfiguredMounts scans every configured MCP server (built-in
// and custom, e.g. tools.github, tools.playwright, mcp-servers.<name>) for host
// mount sources the compiler will pass into that server's container, so they
// can be reflected in the gateway's trusted mount allowlist. This covers
// explicit "mounts" arrays as well as "-v"/"--volume" flags embedded in
// "args"/"entrypointArgs" for containerized servers.
func collectMCPServerConfiguredMounts(tools map[string]any) []string {
	var mounts []string
	for _, toolValue := range tools {
		toolConfig, ok := toolValue.(map[string]any)
		if !ok {
			continue
		}
		mounts = append(mounts, extractMCPMountsField(toolConfig["mounts"])...)
		mounts = append(mounts, extractMCPVolumeArgMounts(toolConfig["args"])...)
		mounts = append(mounts, extractMCPVolumeArgMounts(toolConfig["entrypointArgs"])...)
	}
	return mounts
}

// extractMCPMountsField normalizes a raw "mounts" field value (as produced by
// YAML/JSON frontmatter parsing) into a slice of "source:dest[:mode]" strings.
func extractMCPMountsField(mountsRaw any) []string {
	switch v := mountsRaw.(type) {
	case []any:
		mounts := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				mounts = append(mounts, str)
			}
		}
		return mounts
	case []string:
		return v
	default:
		return nil
	}
}

// extractMCPVolumeArgMounts scans a raw "args"/"entrypointArgs" field value for
// Docker "-v"/"--volume" flags (both "-v host:dest:mode" and
// "--volume=host:dest:mode" forms) and returns the mount specs they declare.
func extractMCPVolumeArgMounts(argsRaw any) []string {
	var args []string
	switch v := argsRaw.(type) {
	case []any:
		args = make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				args = append(args, str)
			}
		}
	case []string:
		args = v
	default:
		return nil
	}

	var mounts []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-v" || arg == "--volume":
			if i+1 < len(args) {
				mounts = append(mounts, args[i+1])
				i++
			}
		case strings.HasPrefix(arg, "--volume="):
			mounts = append(mounts, strings.TrimPrefix(arg, "--volume="))
		case strings.HasPrefix(arg, "-v="):
			mounts = append(mounts, strings.TrimPrefix(arg, "-v="))
		}
	}
	return mounts
}

func appendMCPGatewayConditionalEnvFlags(containerCmd *strings.Builder, workflowData *WorkflowData, engine CodingAgentEngine, hasGitHub bool, githubTool map[string]any, tools map[string]any) {
	if enclavesEnabled(workflowData) {
		containerCmd.WriteString(" -e " + enclaveMCPCapabilityEnv)
	}
	if enclaveDynamicRepositoryPolicyEnabled(workflowData) {
		containerCmd.WriteString(" -e MCP_GATEWAY_DELEGATION_ENVELOPE")
		containerCmd.WriteString(" -e MCP_GATEWAY_DELEGATION_CONTROL_KEY")
		containerCmd.WriteString(" -e MCP_GATEWAY_DELEGATION_STATE_PATH")
		containerCmd.WriteString(" -e MCP_GATEWAY_DELEGATION_GENERATION")
		containerCmd.WriteString(" -e MCP_GATEWAY_DELEGATION_CONTROL_LISTEN")
	}
	if hasGitHub && getGitHubType(githubTool) == GitHubMCPModeRemote && engine.GetID() == "copilot" {
		containerCmd.WriteString(" -e GITHUB_PERSONAL_ACCESS_TOKEN")
	}
	if IsMCPScriptsEnabled(workflowData.MCPScripts) {
		containerCmd.WriteString(" -e GH_AW_MCP_SCRIPTS_PORT")
		containerCmd.WriteString(" -e GH_AW_MCP_SCRIPTS_API_KEY")
	}
	if workflowData.OTLPEndpoint != "" {
		containerCmd.WriteString(" -e GITHUB_AW_OTEL_TRACE_ID")
		containerCmd.WriteString(" -e GITHUB_AW_OTEL_PARENT_SPAN_ID")
		// Pass OTEL_EXPORTER_OTLP_HEADERS as an env var so that auth credentials
		// are not embedded in the stdin JSON config pipe. mcpg reads this env var
		// as the standard OTel mechanism for providing OTLP authentication headers.
		containerCmd.WriteString(" -e OTEL_EXPORTER_OTLP_HEADERS")
	}
	if hasGitHubOIDCAuthInTools(tools) {
		containerCmd.WriteString(" -e ACTIONS_ID_TOKEN_REQUEST_URL")
		containerCmd.WriteString(" -e ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	}
}

// appendMCPGatewaySafeOutputsInputEnvFlags adds -e flags for GH_AW_INPUT_* environment variables
// that are referenced by the safe-outputs config. These variables are written into config.json as
// ${GH_AW_INPUT_…} shell-style placeholders at compile time and must be resolvable inside the
// containerised safe-outputs MCP server at runtime.
func appendMCPGatewaySafeOutputsInputEnvFlags(containerCmd *strings.Builder, safeOutputsInputEnvVars map[string]string) {
	if len(safeOutputsInputEnvVars) == 0 {
		return
	}
	envVarNames := sliceutil.SortedKeys(safeOutputsInputEnvVars)
	for _, envVarName := range envVarNames {
		containerCmd.WriteString(" -e " + envVarName)
	}
}

func appendMCPGatewayCustomAndHTTPEnvFlagsWithCustomGatewayEnvNames(containerCmd *strings.Builder, workflowData *WorkflowData, customGatewayEnvNames []string, mcpEnvVars map[string]string, hasGitHub bool, githubTool map[string]any, tools map[string]any, engine CodingAgentEngine) {
	if len(customGatewayEnvNames) > 0 {
		containerCmd.WriteString(" " + mcpGatewayCustomEnvMarker)
	}
	if len(mcpEnvVars) == 0 {
		return
	}
	addedEnvVars := buildAddedGatewayEnvVarSet(workflowData, customGatewayEnvNames, hasGitHub, githubTool, tools, engine)
	var envVarNames []string
	for envVarName := range mcpEnvVars {
		if !setutil.Contains(addedEnvVars, envVarName) {
			envVarNames = append(envVarNames, envVarName)
		}
	}
	sort.Strings(envVarNames)
	for _, envVarName := range envVarNames {
		containerCmd.WriteString(" -e " + envVarName)
	}
	if mcpSetupGeneratorLog.Enabled() && len(envVarNames) > 0 {
		mcpSetupGeneratorLog.Printf("Added %d HTTP MCP environment variables to gateway container: %v", len(envVarNames), envVarNames)
	}
}

func buildAddedGatewayEnvVarSet(workflowData *WorkflowData, customGatewayEnvNames []string, hasGitHub bool, githubTool map[string]any, tools map[string]any, engine CodingAgentEngine) map[string]struct{} {
	addedEnvVars := make(map[string]struct{})
	standardEnvVars := []string{
		"MCP_GATEWAY_PORT", "MCP_GATEWAY_DOMAIN", "MCP_GATEWAY_AGENT_ID", "MCP_GATEWAY_PAYLOAD_DIR", "DEBUG",
		"MCP_GATEWAY_LOG_DIR", "GH_AW_MCP_LOG_DIR", "GH_AW_SAFE_OUTPUTS",
		"GH_AW_SAFE_OUTPUTS_CONFIG_PATH", "GH_AW_SAFE_OUTPUTS_TOOLS_PATH", compilerenv.PolicyAllowCreatePullRequest,
		"GH_AW_ASSETS_BRANCH", "GH_AW_ASSETS_MAX_SIZE_KB", "GH_AW_ASSETS_ALLOWED_EXTS",
		"DEFAULT_BRANCH", "GITHUB_MCP_SERVER_TOKEN", "GITHUB_MCP_GUARD_MIN_INTEGRITY", "GITHUB_MCP_GUARD_REPOS",
		sinkVisibilityEnvVar,
		"GITHUB_REPOSITORY", "GITHUB_SERVER_URL", "GITHUB_SHA", "GITHUB_WORKSPACE",
		"RUNNER_TEMP", "RUNNER_TOOL_CACHE", "MCP_GATEWAY_ALLOWED_MOUNT_ROOTS",
		"GITHUB_TOKEN", "GITHUB_RUN_ID", "GITHUB_RUN_NUMBER", "GITHUB_RUN_ATTEMPT",
		"GITHUB_JOB", "GITHUB_ACTION", "GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH",
		"GITHUB_ACTOR", "GITHUB_ACTOR_ID", "GITHUB_TRIGGERING_ACTOR",
		"GITHUB_WORKFLOW", "GITHUB_WORKFLOW_REF", "GITHUB_WORKFLOW_SHA",
		"GITHUB_REF", "GITHUB_REF_NAME", "GITHUB_REF_TYPE", "GITHUB_HEAD_REF", "GITHUB_BASE_REF",
	}
	for _, envVar := range standardEnvVars {
		addedEnvVars[envVar] = struct{}{}
	}
	for _, envVar := range optionalPRHeadEnvVars {
		addedEnvVars[envVar] = struct{}{}
	}
	if hasGitHub && getGitHubType(githubTool) == GitHubMCPModeRemote && engine.GetID() == "copilot" {
		addedEnvVars["GITHUB_PERSONAL_ACCESS_TOKEN"] = struct{}{}
	}
	if IsMCPScriptsEnabled(workflowData.MCPScripts) {
		addedEnvVars["GH_AW_MCP_SCRIPTS_PORT"] = struct{}{}
		addedEnvVars["GH_AW_MCP_SCRIPTS_API_KEY"] = struct{}{}
	}
	if workflowData.OTLPEndpoint != "" {
		addedEnvVars["GITHUB_AW_OTEL_TRACE_ID"] = struct{}{}
		addedEnvVars["GITHUB_AW_OTEL_PARENT_SPAN_ID"] = struct{}{}
		addedEnvVars["OTEL_EXPORTER_OTLP_HEADERS"] = struct{}{}
	}
	if hasGitHubOIDCAuthInTools(tools) {
		addedEnvVars["ACTIONS_ID_TOKEN_REQUEST_URL"] = struct{}{}
		addedEnvVars["ACTIONS_ID_TOKEN_REQUEST_TOKEN"] = struct{}{}
	}
	for _, envVarName := range customGatewayEnvNames {
		addedEnvVars[envVarName] = struct{}{}
	}
	return addedEnvVars
}
