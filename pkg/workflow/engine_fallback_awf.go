package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func nativeAWFFallbackModels(data *WorkflowData) []string {
	if data == nil || data.EngineConfig == nil || len(data.EngineConfig.FallbackModels) == 0 || !isFirewallEnabled(data) {
		return nil
	}
	if !awfVersionAtLeast(getFirewallConfig(data), constants.AWFFallbackModelsMinVersion) {
		return nil
	}
	image := getSandboxAgentImages(data)[awfImageRoleAPIProxy]
	if image == "" {
		image = defaultAWFImageForRole(awfImageRoleAPIProxy, getAWFImageTag(getFirewallConfig(data)))
	}
	image = resolveContainerImage(image, data)
	tag, found := imageReferenceTag(image)
	if !found || !versionAtLeast(tag, "", string(constants.AWFFallbackModelsMinVersion)) {
		return nil
	}
	providers := fallbackModelProviders(data)
	if len(providers) != 1 {
		return nil
	}
	var models []string
	for _, model := range data.EngineConfig.FallbackModels {
		if _, alias := data.ModelMappings[model]; alias {
			return nil
		}
		if _, name, qualified := strings.Cut(model, "/"); qualified && fallbackModelProvider(model, "") != "" {
			model = name
		}
		models = append(models, model)
	}
	return models
}
