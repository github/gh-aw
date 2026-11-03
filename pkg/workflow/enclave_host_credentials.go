package workflow

import (
	"errors"

	"github.com/github/gh-aw/pkg/constants"
)

func validateHostGitHubTokenConfig(data *WorkflowData) error {
	agent := getAgentConfig(data)
	if agent == nil || agent.HostGitHubToken == "" {
		return nil
	}

	if err := validateSecretsExpression(agent.HostGitHubToken); err != nil {
		return errors.New("sandbox.agent.host-github-token must be a secret expression, for example ${{ secrets.HOST_STAGING_TOKEN }}")
	}
	if !enclavesEnabled(data) || !isAWFNetworkIsolationEnabled(data) {
		return errors.New("sandbox.agent.host-github-token requires enclaves and an enabled network-isolated AWF agent sandbox")
	}
	if !resolveSandboxRuntimeProfile(agent).Rootless {
		return errors.New("sandbox.agent.host-github-token requires a supported rootless AWF agent runtime")
	}
	if !awfVersionAtLeast(getFirewallConfig(data), constants.AWFDockerEnclaveSetupMinVersion) {
		return errors.New("sandbox.agent.host-github-token requires AWF v0.29.5 or newer; set sandbox.agent.version to a supported release")
	}
	return nil
}

func prependHostGitHubTokenCheck(command string, data *WorkflowData) string {
	if agent := getAgentConfig(data); agent != nil && agent.HostGitHubToken != "" &&
		!data.IsDetectionRun && !data.IsEvalsRun {
		return `if [ -z "${GH_TOKEN:-}" ]; then echo "sandbox.agent.host-github-token resolved to an empty credential" >&2; exit 1; fi` + "\n" + command
	}
	return command
}
