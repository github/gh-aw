package workflow

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const awfRoutingConversationFile = "/tmp/gh-aw/routing-conversation.json"
const dockerSocketVarPath = "/var/run/docker.sock"
const dockerSocketRunPath = "/run/docker.sock"

var defaultModelRoutingImages = map[string]string{
	"agent":    "ghcr.io/github/gh-aw-firewall/agent:0.28.29@sha256:edcf17ae63dd74366bc911a74678b9e264d66ac51c48ec156c80e2619892ebbb",
	"apiProxy": "ghcr.io/github/gh-aw-firewall/api-proxy:0.28.29@sha256:5cc683af8156b39c15bd2370615a85775a8b179bed9f49c490a3068d667dfa2b",
	"router":   "ghcr.io/githubnext/gh-aw-router:latest@sha256:d1612d0eaec3fa8f14c38bbd0a6a0682732fc9f83b7fec94219d3e757a048270",
	"squid":    "ghcr.io/github/gh-aw-firewall/squid:0.28.29@sha256:1d5e169c4df14e87fc826261b94cf4ddaf2f08aca2a5b88701100ec193968193",
}

func configuredModelRouting(data *WorkflowData) *ModelRoutingConfig {
	if data == nil || data.EngineConfig == nil || data.EngineConfig.ModelRouting == nil {
		return nil
	}
	return data.EngineConfig.ModelRouting
}

func qualifyRoutedModels(models []string) []string {
	result := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if suffix, found := strings.CutPrefix(model, "copilot/"); found {
			model = "github-copilot/" + suffix
		} else if !strings.Contains(model, "/") {
			model = "github-copilot/" + model
		}
		result = append(result, model)
	}
	return result
}

func validateEngineModelRouting(data *WorkflowData) error {
	routing := configuredModelRouting(data)
	if routing == nil {
		return nil
	}
	if data.EngineConfig.ID != string(constants.CopilotEngine) {
		return NewValidationError("engine.model-routing", data.EngineConfig.ID, "model routing is only supported by the Copilot engine", "Set engine.id to copilot or remove engine.model-routing.")
	}
	if !isFirewallEnabled(data) {
		return NewValidationError("engine.model-routing", "", "model routing requires the AWF agent sandbox", "Enable sandbox.agent or remove engine.model-routing.")
	}
	fw := getFirewallConfig(data)
	version := string(constants.DefaultFirewallVersion)
	if fw != nil && fw.Version != "" {
		version = fw.Version
	}
	if !versionAtLeast(version, string(constants.DefaultFirewallVersion), string(constants.AWFModelRoutingMinVersion)) {
		return NewValidationError("engine.model-routing", version, fmt.Sprintf("model routing requires AWF %s or newer", constants.AWFModelRoutingMinVersion), fmt.Sprintf("Set sandbox.agent.version to %s or newer, or remove engine.model-routing.", constants.AWFModelRoutingMinVersion))
	}
	if !slices.Contains([]string{"cost", "cost-speed"}, routing.Goal) {
		return NewValidationError("engine.model-routing.goal", routing.Goal, "unsupported routing goal", "Use cost or cost-speed.")
	}
	if !slices.Contains([]string{"economy", "balanced", "robust", "auto"}, routing.Mode) {
		return NewValidationError("engine.model-routing.mode", routing.Mode, "unsupported routing mode", "Use economy, balanced, robust, or auto.")
	}
	models := qualifyRoutedModels(routing.AllowedModels)
	if len(models) == 0 {
		return NewValidationError("engine.model-routing.allowed-models", "", "at least one allowed model is required", "Add one or more Copilot model IDs to engine.model-routing.allowed-models.")
	}
	for _, model := range models {
		if !strings.HasPrefix(model, "github-copilot/") || strings.TrimPrefix(model, "github-copilot/") == "" {
			return NewValidationError("engine.model-routing.allowed-models", model, "model routing only supports Copilot models", "Use unqualified model IDs or the github-copilot provider prefix.")
		}
	}
	if isArcDindTopology(data) ||
		isCloudHypervisorRuntime(data) ||
		hasEnabledAWFArg(customAWFArgs(data), "--keep-containers") ||
		hasDockerSocketExposure(data) {
		return NewValidationError("engine.model-routing", "", "model routing is incompatible with this AWF runtime configuration", "Use the default Linux runc AWF runtime without --keep-containers, DinD, or Docker-socket mounts.")
	}
	return nil
}

func hasDockerSocketExposure(data *WorkflowData) bool {
	if data != nil && data.SandboxConfig != nil && data.SandboxConfig.Agent != nil {
		if slices.ContainsFunc(data.SandboxConfig.Agent.Mounts, containsDockerSocketPath) {
			return true
		}
	}
	return slices.ContainsFunc(customAWFArgs(data), containsDockerSocketPath)
}

func containsDockerSocketPath(value string) bool {
	for part := range strings.SplitSeq(value, ":") {
		if part == dockerSocketVarPath || part == dockerSocketRunPath {
			return true
		}
	}
	return false
}

func copyDefaultModelRoutingImages() map[string]string {
	images := make(map[string]string, len(defaultModelRoutingImages))
	maps.Copy(images, defaultModelRoutingImages)
	return images
}
