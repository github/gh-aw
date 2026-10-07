package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func nativeAWFFallbackModels(data *WorkflowData) []string {
	if data == nil || data.EngineConfig == nil {
		return nil
	}
	var models []string
	for _, model := range data.EngineConfig.FallbackModels {
		if _, name, qualified := strings.Cut(model, "/"); qualified && fallbackModelProvider(model, "") != "" {
			model = name
		}
		models = append(models, model)
	}
	return models
}

func validateAWFFallbackModels(data *WorkflowData) error {
	if !isFirewallEnabled(data) {
		return errors.New("engine.fallback-models requires AWF request-level recovery; enable sandbox.agent")
	}
	if !awfVersionAtLeast(getFirewallConfig(data), constants.AWFFallbackModelsMinVersion) {
		return fmt.Errorf("engine.fallback-models requires AWF %s or newer; update sandbox.agent.version", constants.AWFFallbackModelsMinVersion)
	}
	image := getSandboxAgentImages(data)[awfImageRoleAPIProxy]
	if image == "" {
		image = defaultAWFImageForRole(awfImageRoleAPIProxy, getAWFImageTag(getFirewallConfig(data)))
	}
	image = resolveContainerImage(image, data)
	tag, found := imageReferenceTag(image)
	if !found || !versionAtLeast(tag, "", string(constants.AWFFallbackModelsMinVersion)) {
		return fmt.Errorf("engine.fallback-models requires an API-proxy image tagged %s or newer, got %q; update sandbox.agent.images.api-proxy", constants.AWFFallbackModelsMinVersion, image)
	}
	providers := fallbackModelProviders(data)
	if len(providers) == 0 {
		return fmt.Errorf("engine.fallback-models requires a known AWF inference provider for engine %q", data.EngineConfig.ID)
	}
	if len(providers) > 1 {
		return errors.New("engine.fallback-models currently requires models on the same provider; AWF cross-provider recovery is tracked in https://github.com/github/gh-aw-firewall/issues/9548")
	}
	for _, model := range data.EngineConfig.FallbackModels {
		if _, alias := data.ModelMappings[model]; alias {
			return fmt.Errorf("engine.fallback-models requires concrete model identifiers, got alias %q; replace it with a model ID supported by the configured provider", model)
		}
	}
	return nil
}
