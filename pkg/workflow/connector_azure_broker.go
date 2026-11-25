package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
)

const connectorAzureSocketPrefix = "GH_AW_CONNECTOR_CREDENTIAL_SOCKET_"

func (config *WorkflowConnector) azureRoute(secret string) *ConnectorAzureOIDC {
	for name, azure := range config.AzureOIDC {
		if connectorAppSecret(config.Alias, name) == secret {
			return azure
		}
	}
	return nil
}

func connectorAzureDirectory(secret string) string {
	return "steps." + connectorAppStepID(secret) + ".outputs.directory"
}

func connectorAzureMount(secret string) string {
	const mountRoot = "/run/gh-aw-azure/"
	return mountRoot + secret
}

// Each broker handles one fixed credential. Only its directory (never an
// assertion, token or OIDC request pair) crosses the runner/adapter boundary.
func (c *Compiler) connectorAzureBrokerSteps(data *WorkflowData, phase string, teardown bool) []string {
	var steps []string
	for _, alias := range sliceutil.SortedKeys(data.Connectors) {
		config := data.Connectors[alias]
		plan := config.Agent
		if phase == "safe-output" {
			plan = config.SafeOutputs
		}
		if plan == nil {
			continue
		}
		for _, name := range sliceutil.SortedKeys(config.AzureOIDC) {
			secret := connectorAppSecret(alias, name)
			if !slices.Contains(plan.Secrets(), secret) {
				continue
			}
			condition, staged, enabled := connectorCredentialMintCondition(c, data, config, secret, phase)
			if !enabled {
				continue
			}
			if teardown {
				directory := connectorAzureDirectory(secret)
				steps = append(steps, fmt.Sprintf("      - name: Stop connector Azure OIDC broker %s/%s\n", alias, name),
					"        if: ${{ always() && "+directory+" != '' }}\n",
					"        env:\n", formatYAMLEnv("          ", "GH_AW_CONNECTOR_BROKER_DIRECTORY", "${{ "+directory+" }}"),
					"        run: node \"$RUNNER_TEMP/gh-aw/actions/connector_azure_oidc.cjs\" stop\n")
				continue
			}
			timeout := resolveAgentJobTimeoutValue(data)
			if phase == "safe-output" {
				timeout = strconv.Itoa(safeOutputsJobTimeoutMinutes(data))
				if override, ok := builtinJobTimeoutOverride(data, string(constants.SafeOutputsJobName)); ok {
					timeout = override
				}
			}
			azure := config.AzureOIDC[name]
			steps = append(steps, fmt.Sprintf("      - name: Start connector Azure OIDC broker %s/%s\n", alias, name),
				"        id: "+connectorAppStepID(secret)+"\n", condition, "        env:\n",
				formatYAMLEnv("          ", "GH_AW_CONNECTOR_BROKER_TIMEOUT_MINUTES", timeout),
				formatYAMLEnv("          ", "GH_AW_AZURE_TENANT_ID", azure.TenantID),
				formatYAMLEnv("          ", "GH_AW_AZURE_CLIENT_ID", azure.ClientID),
				formatYAMLEnv("          ", "GH_AW_AZURE_SCOPE", azure.Scope))
			if azure.Cloud != "" {
				steps = append(steps, formatYAMLEnv("          ", "GH_AW_AZURE_CLOUD", azure.Cloud))
			}
			if azure.Audience != "" {
				steps = append(steps, formatYAMLEnv("          ", "GH_AW_AZURE_AUDIENCE", azure.Audience))
			}
			if condition != "" {
				steps = append(steps, formatYAMLEnv("          ", "GH_AW_CONNECTOR_STAGED", staged))
			}
			steps = append(steps, "        run: node \"$RUNNER_TEMP/gh-aw/actions/connector_azure_oidc.cjs\" start\n")
		}
	}
	return steps
}

func (c *Compiler) validateConnectorAzureBoundary(data *WorkflowData) error {
	readers, writers := map[string]struct{}{}, map[string]struct{}{}
	if data.EngineConfig != nil && data.EngineConfig.Auth != nil {
		auth := data.EngineConfig.Auth
		if auth.Type == "github-oidc" && auth.Provider == "azure" {
			readers[azureClientKey(auth.AzureClientID)] = struct{}{}
		}
	}
	for _, config := range data.Connectors {
		if config.Inference != nil && config.Inference.Auth != nil {
			readers[azureClientKey(config.Inference.Auth.AzureClientID)] = struct{}{}
		}
		if config.Agent != nil {
			for _, secret := range config.Agent.Secrets() {
				if azure := config.azureRoute(secret); azure != nil {
					readers[azureClientKey(azure.ClientID)] = struct{}{}
					if c.dryRun {
						return errors.New("--dry-run cannot safely execute connector readers using Azure OIDC")
					}
					if err := validateConnectorAzureReaderHost(data); err != nil {
						return err
					}
				}
			}
		}
		if config.SafeOutputs != nil {
			for _, secret := range config.SafeOutputs.Secrets() {
				if azure := config.azureRoute(secret); azure != nil {
					if _, _, live := connectorCredentialMintCondition(c, data, config, secret, "safe-output"); !live {
						continue
					}
					writers[azureClientKey(azure.ClientID)] = struct{}{}
				}
			}
		}
	}
	for client := range writers {
		if _, shared := readers[client]; shared {
			return errors.New("connector Azure OIDC safe outputs require a separate client ID from readers and inference")
		}
	}
	return nil
}

func azureClientKey(id string) string {
	return strings.ToLower(strings.Join(strings.Fields(id), ""))
}

func (c *Compiler) validateConnectorAzureBrokerRunners(data *WorkflowData) error {
	for _, phase := range []struct{ name, job string }{
		{"agent", string(constants.AgentJobName)},
		{"safe-output", string(constants.SafeOutputsJobName)},
	} {
		if len(c.connectorAzureBrokerSteps(data, phase.name, false)) == 0 {
			continue
		}
		job, ok := c.jobManager.GetJob(phase.job)
		if ok && strings.TrimSpace(strings.TrimPrefix(job.RunsOn, "runs-on:")) == "windows-latest" {
			return fmt.Errorf("connector Azure OIDC %s requires a non-Windows runner; Windows WSL broker lifecycle is not supported", phase.name)
		}
	}
	return nil
}

func validateConnectorAzureReaderHost(data *WorkflowData) error {
	errHost := errors.New("connector Azure OIDC readers require a built-in sandboxed engine, standard AWF docker runtime and trusted MCP gateway (no custom host access)")
	engine := data.EngineConfig
	if !slices.Contains([]string{"copilot", "claude", "codex", "gemini", "pi"}, data.AI) ||
		(engine != nil && (engine.IsInlineDefinition || engine.Command != "")) || !isFirewallEnabled(data) ||
		data.Container != "" || data.Services != "" {
		return errHost
	}
	if connectorAzureReaderHasRunnerHooks(data) {
		return errors.New("connector Azure OIDC readers cannot grant job credentials to authored runner steps")
	}
	if data.MCPScripts != nil && len(data.MCPScripts.Tools) != 0 {
		return errors.New("connector Azure OIDC readers cannot share a job with mcp-scripts")
	}
	firewall := getFirewallConfig(data)
	if !awfSupportsExcludeEnv(firewall) {
		return fmt.Errorf("connector Azure OIDC readers require AWF %s or newer to exclude Actions OIDC credentials", constants.AWFExcludeEnvMinVersion)
	}
	if firewall != nil && len(firewall.Args) > 0 {
		return errHost
	}
	if agent := getAgentConfig(data); agent != nil &&
		((agent.ID != "" && agent.ID != "awf") || (agent.Runtime != "" && agent.Runtime != AgentRuntimeDocker) ||
			agent.Command != "" || len(agent.Args)+len(agent.Env)+len(agent.Mounts)+len(agent.Images)+len(agent.AllowHostPorts) != 0) {
		return errHost
	}
	if data.SandboxConfig != nil && data.SandboxConfig.MCP != nil {
		gateway := data.SandboxConfig.MCP
		if (gateway.Container != "" && gateway.Container != constants.DefaultMCPGatewayContainer) ||
			gateway.Entrypoint != "" || len(gateway.Args)+len(gateway.EntrypointArgs)+len(gateway.Env)+len(gateway.Mounts) != 0 {
			return errHost
		}
	}
	return validateConnectorAzureReaderTools(data)
}

func validateConnectorAzureReaderTools(data *WorkflowData) error {
	for name, raw := range data.Tools {
		if config := data.Connectors[name]; config != nil && config.Agent != nil {
			adapter, err := connectorAgentAdapter(config)
			if err != nil {
				return err
			}
			expected, err := json.Marshal(adapter)
			if err != nil {
				return err
			}
			actual, err := json.Marshal(raw)
			if err != nil || !bytes.Equal(expected, actual) {
				return fmt.Errorf("mcp-servers.%s must not override a compiler-owned connector adapter", name)
			}
			continue
		}
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if mcp, _ := hasMCPConfig(tool); mcp {
			return fmt.Errorf("connector Azure OIDC reader isolation does not permit sibling custom MCP server %q", name)
		}
		for _, field := range []string{"mounts", "args", "entrypointArgs", "proxy-args"} {
			if _, ok := tool[field]; ok {
				return fmt.Errorf("connector Azure OIDC readers cannot share a job with tools.%s.%s", name, field)
			}
		}
	}
	return nil
}

func connectorAzureReaderHasRunnerHooks(data *WorkflowData) bool {
	if data.PreSteps != "" || data.PreAgentSteps != "" || data.PostSteps != "" || data.CustomSteps != "" ||
		(data.SecretMasking != nil && len(data.SecretMasking.Steps) != 0) {
		return true
	}
	if agentJob, ok := data.Jobs["agent"].(map[string]any); ok {
		_, setupSteps := agentJob["setup-steps"]
		_, preSteps := agentJob["pre-steps"]
		return setupSteps || preSteps
	}
	return false
}
