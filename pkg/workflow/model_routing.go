package workflow

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const modelRoutingConversationFile = "/tmp/gh-aw/routing-conversation.json"

var routingModelNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type CopilotModelRoutingConfig struct {
	Goal          string
	Mode          string
	AllowedModels []string
}

func isModelRoutingEnabled(data *WorkflowData) bool {
	return data != nil && data.EngineConfig != nil && data.EngineConfig.ModelRouting != nil
}

func resolveModelRoutingAllowedModels(routing *CopilotModelRoutingConfig) ([]string, error) {
	if routing == nil {
		return nil, nil
	}
	if len(routing.AllowedModels) == 0 {
		return nil, errors.New("engine.model-routing.allowed-models must contain at least one Copilot model")
	}

	models := make([]string, 0, len(routing.AllowedModels))
	seen := make(map[string]struct{}, len(routing.AllowedModels))
	for _, model := range routing.AllowedModels {
		model = strings.TrimSpace(model)
		model = strings.TrimPrefix(model, "github-copilot/")
		if !routingModelNamePattern.MatchString(model) {
			return nil, fmt.Errorf("engine.model-routing.allowed-models contains invalid Copilot model %q", model)
		}
		qualified := "github-copilot/" + model
		if _, ok := seen[qualified]; ok {
			continue
		}
		seen[qualified] = struct{}{}
		models = append(models, qualified)
	}
	return models, nil
}

func intersectModelRoutingPolicy(candidates, allowed, blocked []string) ([]string, error) {
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		model := strings.TrimPrefix(candidate, "github-copilot/")
		if len(allowed) > 0 && !matchesModelPolicy(model, candidate, allowed) {
			continue
		}
		if matchesModelPolicy(model, candidate, blocked) {
			continue
		}
		result = append(result, candidate)
	}
	if len(result) == 0 {
		return nil, errors.New("all engine.model-routing.allowed-models are excluded by models.allowed or models.blocked policy")
	}
	return result, nil
}

func matchesModelPolicy(model, qualifiedModel string, rules []string) bool {
	for _, rule := range rules {
		if rule == model || rule == qualifiedModel {
			return true
		}
		if matched, _ := path.Match(rule, model); matched {
			return true
		}
		if matched, _ := path.Match(rule, qualifiedModel); matched {
			return true
		}
	}
	return false
}

func validateModelRouting(workflowData *WorkflowData, engineName string) error {
	if !isModelRoutingEnabled(workflowData) {
		return nil
	}
	routing := workflowData.EngineConfig.ModelRouting
	if !strings.EqualFold(engineName, "copilot") {
		return NewValidationError("engine.model-routing", engineName, "task-level model routing is supported only by the Copilot engine", "Set engine.id to copilot or remove engine.model-routing.")
	}
	if !isFirewallEnabled(workflowData) {
		return NewValidationError("engine.model-routing", "", "task-level model routing requires the AWF firewall", "Enable the AWF firewall for this Copilot workflow.")
	}
	if routing.Goal != "cost" && routing.Goal != "cost-speed" {
		return NewValidationError("engine.model-routing.goal", routing.Goal, "unsupported model-routing goal", "Use cost or cost-speed.")
	}
	if routing.Mode != "economy" && routing.Mode != "balanced" && routing.Mode != "robust" && routing.Mode != "auto" {
		return NewValidationError("engine.model-routing.mode", routing.Mode, "unsupported model-routing mode", "Use economy, balanced, robust, or auto.")
	}
	candidates, err := resolveModelRoutingAllowedModels(routing)
	if err != nil {
		return NewValidationError("engine.model-routing.allowed-models", strings.Join(routing.AllowedModels, ", "), "invalid model-routing candidates", err.Error())
	}
	allowed, blocked := resolveModelPolicyForAWFConfig(workflowData)
	if _, err := intersectModelRoutingPolicy(candidates, allowed, blocked); err != nil {
		return NewValidationError("engine.model-routing.allowed-models", strings.Join(routing.AllowedModels, ", "), "model-routing candidates violate model policy", err.Error())
	}
	firewallConfig := getFirewallConfig(workflowData)
	version := getAWFImageTag(firewallConfig)
	if !versionAtLeast(version, string(constants.DefaultFirewallVersion), string(constants.AWFModelRoutingMinVersion)) {
		return NewValidationError("engine.model-routing", version, fmt.Sprintf("task-level model routing requires AWF %s or newer", constants.AWFModelRoutingMinVersion), fmt.Sprintf("The effective AWF version is %s. Set sandbox.agent.version or firewall.version to %s or newer.", version, constants.AWFModelRoutingMinVersion))
	}
	return nil
}

func generateModelRoutingConversationStep(yaml *strings.Builder, data *WorkflowData) {
	if !isModelRoutingEnabled(data) {
		return
	}
	yaml.WriteString("      - name: Prepare model-routing conversation\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_ROUTING_PROMPT: " + constants.AwPromptsFileExpr + "\n")
	yaml.WriteString("          GH_AW_ROUTING_CONVERSATION_FILE: " + modelRoutingConversationFile + "\n")
	yaml.WriteString("        run: node \"${RUNNER_TEMP}/gh-aw/actions/prepare_model_routing_conversation.cjs\"\n")
}
