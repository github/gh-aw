package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func hasCloudHypervisorEnclaves(workflowData *WorkflowData) bool {
	if workflowData == nil {
		return false
	}
	for _, enclave := range workflowData.Enclaves {
		if enclave != nil && enclave.Runtime == string(AgentRuntimeCloudHypervisor) {
			return true
		}
	}
	return false
}

func validateCloudHypervisorEnclavePrimaryRuntime(workflowData *WorkflowData) error {
	if !hasCloudHypervisorEnclaves(workflowData) {
		return nil
	}
	agent := getAgentConfig(workflowData)
	if agent == nil {
		return nil
	}
	switch agent.Runtime {
	case AgentRuntimeDockerSudoIptables:
		return errors.New("enclaves cloud-hypervisor runtime is incompatible with sandbox.agent.runtime: docker-sudo-iptables; use sandbox.agent.runtime: docker")
	case "sbx", AgentRuntimeCloudHypervisor, "nvx":
		return fmt.Errorf("enclaves cloud-hypervisor runtime is incompatible with sandbox.agent.runtime: %s; use a Docker primary agent runtime", agent.Runtime)
	default:
		return nil
	}
}

func validateCloudHypervisorEnclaves(workflowData *WorkflowData) error {
	if !hasCloudHypervisorEnclaves(workflowData) {
		return nil
	}
	if isArcDindTopology(workflowData) {
		return errors.New("enclaves cloud-hypervisor runtime is incompatible with runner.topology: arc-dind; use GitHub-hosted Ubuntu x86_64 KVM runners")
	}
	if isCliProxyNeeded(workflowData) {
		return errors.New("enclaves cloud-hypervisor runtime is incompatible with tools.github.mode: gh-proxy and integrity-reactions; remove those settings")
	}
	args := customAWFArgs(workflowData)
	if hasEnabledAWFArg(args, "--enable-dind") {
		return errors.New("enclaves cloud-hypervisor runtime is incompatible with --enable-dind; remove DinD")
	}
	for index, arg := range args {
		if arg == "--docker-host-path-prefix" || strings.HasPrefix(arg, "--docker-host-path-prefix=") {
			return errors.New("enclaves cloud-hypervisor runtime is incompatible with --docker-host-path-prefix; remove the Docker host path prefix")
		}
		name, runtime, _ := strings.Cut(arg, "=")
		if name == "--container-runtime" {
			if nextIndex := index + 1; arg == name && nextIndex >= 0 && nextIndex < len(args) {
				runtime = args[nextIndex]
			}
			switch runtime {
			case "sbx", "cloud-hypervisor", "nvx":
				return fmt.Errorf("enclaves cloud-hypervisor runtime is incompatible with --container-runtime %s; use a Docker primary agent runtime", runtime)
			}
		}
	}
	for i, enclave := range workflowData.Enclaves {
		if enclave.Runtime != string(AgentRuntimeCloudHypervisor) {
			return errors.New("enclaves cloud-hypervisor runtime cannot be mixed with Docker or default enclave runtimes; set runtime: cloud-hypervisor on every enclave entry")
		}
		if enclave.Image != "" {
			return fmt.Errorf("enclaves[%d].image is incompatible with runtime: cloud-hypervisor; remove the image override to use release-attested enclave artifacts", i)
		}
		if enclave.Dynamic != nil {
			return fmt.Errorf("enclaves[%d].dynamic is incompatible with runtime: cloud-hypervisor; use static repos", i)
		}
		if enclave.Agent != nil {
			if strings.TrimSpace(enclave.Agent.Model) == "" {
				return fmt.Errorf("enclaves[%d].agent.model is required for runtime: cloud-hypervisor", i)
			}
			if enclave.Agent.Engine != "" && enclave.Agent.Engine != "copilot" {
				return fmt.Errorf("enclaves[%d].agent.engine %q is not implemented for runtime: cloud-hypervisor; use copilot", i, enclave.Agent.Engine)
			}
			if enclave.Agent.GitHub != nil || enclaveGitHubToolsConfig(enclave) != nil {
				return fmt.Errorf("enclaves[%d] cloud-hypervisor static GitHub tools require a scoped executor bearer handoff that is not yet supported; remove agent.github and agent.tools.github", i)
			}
			if hasEnabledAWFArg(args, "--no-enable-api-proxy") {
				return fmt.Errorf("enclaves[%d] cloud-hypervisor agent runtime requires the API proxy; remove --no-enable-api-proxy", i)
			}
			if !cloudHypervisorEnclaveCopilotRouteConfigured(workflowData) {
				return fmt.Errorf("enclaves[%d] cloud-hypervisor agent runtime requires a configured Copilot API proxy provider route (COPILOT_GITHUB_TOKEN or COPILOT_PROVIDER_API_KEY); use a GitHub-backed primary engine or configure Copilot BYOK credentials", i)
			}
		}
	}
	return validateCloudHypervisorEnclaveVersion(getFirewallConfig(workflowData))
}

func validateCloudHypervisorEnclaveVersion(firewallConfig *FirewallConfig) error {
	if !awfVersionAtLeast(firewallConfig, constants.AWFEnclaveCloudHypervisorMinVersion) {
		effectiveVersion := string(constants.DefaultFirewallVersion)
		if firewallConfig != nil && firewallConfig.Version != "" {
			effectiveVersion = firewallConfig.Version
		}
		return fmt.Errorf("enclaves cloud-hypervisor runtime requires AWF %s or newer, but the effective version is %s; set sandbox.agent.version to %s or newer", constants.AWFEnclaveCloudHypervisorMinVersion, effectiveVersion, constants.AWFEnclaveCloudHypervisorMinVersion)
	}
	return nil
}

func cloudHypervisorEnclaveCopilotRouteConfigured(workflowData *WorkflowData) bool {
	env := make(map[string]string)
	engineID := ResolveEngineID(workflowData)
	if engineID == "" || engineID == "copilot" {
		engine := NewCopilotEngine()
		env = engine.buildCopilotBaseStepEnv(workflowData, engine.ResolveLLMProvider(workflowData), "", isCopilotBYOKMode(workflowData, true), hasCopilotRequestsWritePermission(workflowData))
	} else if engine, err := GetGlobalEngineRegistry().GetEngine(engineID); err == nil {
		if resolver, ok := engine.(InferenceProviderResolver); ok && resolver.ResolveLLMProvider(workflowData) == LLMProviderGitHub {
			env["COPILOT_GITHUB_TOKEN"] = llmProviderSecretExpression(LLMProviderGitHub, workflowData)
		}
	}
	applyEngineAndAgentEnv(env, workflowData, enclavesLog)
	applyMCPScriptsSecretEnv(env, workflowData)
	if engineID == "" {
		engineID = "copilot"
	}
	engine, err := GetGlobalEngineRegistry().GetEngine(engineID)
	if err != nil {
		return false
	}
	env = FilterEnvForSecrets(env, engine.GetRequiredSecretNames(workflowData))
	// Provider routes are configured by credentials in the emitted step environment,
	// not by apiProxy.providers (pricing overlays) or a target hostname alone.
	return strings.TrimSpace(env["COPILOT_GITHUB_TOKEN"]) != "" || strings.TrimSpace(env["COPILOT_PROVIDER_API_KEY"]) != ""
}
