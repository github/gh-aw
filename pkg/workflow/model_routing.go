package workflow

import (
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
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
		if !validModelRoutingCandidate(model) {
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
		if containsExpression(candidate) {
			if len(allowed) > 0 || len(blocked) > 0 {
				return nil, errors.New("GitHub Actions expressions in engine.model-routing.allowed-models cannot be checked against models.allowed or models.blocked")
			}
			result = append(result, candidate)
			continue
		}
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

func validModelRoutingCandidate(model string) bool {
	if !containsExpression(model) {
		return routingModelNamePattern.MatchString(model)
	}
	withoutExpressions := ExpressionPattern.ReplaceAllString(model, "x")
	return routingModelNamePattern.MatchString(withoutExpressions)
}

// subAgentRequestModels extends request policy without changing router candidates.
func subAgentRequestModels(data *WorkflowData, candidates, allowed, blocked []string) ([]string, []string) {
	result := append([]string(nil), candidates...)
	seen := make(map[string]struct{}, len(result))
	for _, candidate := range result {
		seen[candidate] = struct{}{}
	}
	var warnings []string
	for _, agent := range data.SubAgentModels {
		patterns := expandSubAgentModel(agent.Model, data.ModelMappings)
		admitted := false
		for _, pattern := range patterns {
			if len(allowed) > 0 && strings.ContainsAny(pattern, "*[") {
				for _, rule := range allowed {
					if strings.ContainsAny(rule, "*[") {
						continue
					}
					qualified := rule
					if !strings.Contains(rule, "/") {
						qualified = "github-copilot/" + rule
					}
					if matched, _ := path.Match(pattern, qualified); matched {
						if !matchesModelPolicy(rule, qualified, blocked) {
							admitted = true
							if _, ok := seen[qualified]; !ok {
								result = append(result, qualified)
								seen[qualified] = struct{}{}
							}
						}
					}
				}
			}
			model := strings.TrimPrefix(pattern, "github-copilot/")
			if len(allowed) > 0 && !matchesModelPolicy(model, pattern, allowed) {
				continue
			}
			if matchesModelPolicy(model, pattern, blocked) {
				continue
			}
			admitted = true
			if _, ok := seen[pattern]; !ok {
				result = append(result, pattern)
				seen[pattern] = struct{}{}
			}
		}
		if !admitted {
			warnings = append(warnings, fmt.Sprintf("sub-agent %q model %q cannot be admitted by models.allowed or models.blocked (or resolves to no models)", agent.Name, agent.Model))
		}
	}
	return result, warnings
}

func expandSubAgentModel(request string, aliases map[string][]string) []string {
	return expandModelPatterns(request, aliases, "github-copilot")
}

func expandModelPatterns(request string, aliases map[string][]string, defaultProvider string) []string {
	var patterns []string
	var expand func(string, map[string]bool, bool)
	expand = func(model string, visited map[string]bool, fromAlias bool) {
		model, _, _ = strings.Cut(model, "?")
		if entries, ok := aliases[model]; ok {
			if visited[model] {
				return
			}
			visited[model] = true
			for _, entry := range entries {
				expand(entry, visited, true)
			}
			delete(visited, model)
			return
		}
		if !fromAlias && strings.ContainsAny(model, "*[]") {
			return
		}
		if suffix, ok := strings.CutPrefix(model, "copilot/"); ok {
			model = "github-copilot/" + suffix
		} else if !strings.Contains(model, "/") {
			if !routingModelNamePattern.MatchString(model) {
				return
			}
			model = path.Join(defaultProvider, model)
		}
		if provider, name, ok := strings.Cut(model, "/"); ok && provider != "" && name != "" {
			patterns = append(patterns, model)
		}
	}
	expand(request, make(map[string]bool), false)
	return patterns
}

func (c *Compiler) warnRoutedSubAgentModels(data *WorkflowData) {
	if !isModelRoutingEnabled(data) || len(data.SubAgentModels) == 0 {
		return
	}
	firewall := getFirewallConfig(data)
	if !awfVersionAtLeast(firewall, constants.AWFRoutingCandidateModelsMinVersion) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(
			fmt.Sprintf("sub-agent models are limited to engine.model-routing.allowed-models with AWF %s; use AWF %s or newer to admit separately declared models",
				getAWFImageTag(firewall), constants.AWFRoutingCandidateModelsMinVersion)))
		c.IncrementWarningCount()
		return
	}
	if !apiProxySupportsRoutingCandidateModels(data) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(
			"sub-agent model routing requires an AWF apiProxy image with routing.candidateModels support (v0.28.33+); the configured image is older or its version is unknown. Pin a compatible image in sandbox.agent.images."))
		c.IncrementWarningCount()
	}
	candidates, err := resolveModelRoutingAllowedModels(data.EngineConfig.ModelRouting)
	if err != nil {
		return
	}
	allowed, blocked := resolveModelPolicyForAWFConfig(data)
	candidates, err = intersectModelRoutingPolicy(candidates, allowed, blocked)
	if err != nil {
		return
	}
	_, warnings := subAgentRequestModels(data, candidates, allowed, blocked)
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(warning))
		c.IncrementWarningCount()
	}
}

func apiProxySupportsRoutingCandidateModels(data *WorkflowData) bool {
	if !isModelRoutingEnabled(data) {
		return false
	}
	firewall := getFirewallConfig(data)
	if !awfVersionAtLeast(firewall, constants.AWFRoutingCandidateModelsMinVersion) {
		return false
	}
	image := getSandboxAgentImages(data)[awfImageRoleAPIProxy]
	tag, found := imageReferenceTag(image)
	return found && !strings.EqualFold(tag, "latest") &&
		versionAtLeast(tag, "", string(constants.AWFRoutingCandidateModelsMinVersion))
}

func imageReferenceTag(image string) (string, bool) {
	if digestIndex := strings.IndexByte(image, '@'); digestIndex >= 0 {
		image = image[:digestIndex]
	}
	tagIndex := strings.LastIndexByte(image, ':')
	if tagIndex <= strings.LastIndexByte(image, '/') || tagIndex == len(image)-1 {
		return "", false
	}
	return image[tagIndex+1:], true
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
	promptFilePath := constants.AwPromptsUserFile
	fallbackFilePath := constants.AwPromptsFile
	yaml.WriteString("      - name: Prepare model-routing conversation\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_ROUTING_PROMPT: " + promptFilePath + "\n")
	yaml.WriteString("          GH_AW_ROUTING_PROMPT_FALLBACK: " + fallbackFilePath + "\n")
	yaml.WriteString("          GH_AW_ROUTING_CONVERSATION_FILE: " + modelRoutingConversationFile + "\n")
	yaml.WriteString("        run: node -e \"const fs=require('node:fs'); const path=require('node:path'); const readPrompt=file=>{try{return fs.readFileSync(file,'utf8');}catch(error){if(error.code==='ENOENT')return '';throw error;}}; let prompt=readPrompt(process.env.GH_AW_ROUTING_PROMPT); if (!prompt.trim()) prompt=readPrompt(process.env.GH_AW_ROUTING_PROMPT_FALLBACK); if (!prompt.trim()) throw new Error('Rendered workflow prompt is empty; cannot route this task'); const destination=process.env.GH_AW_ROUTING_CONVERSATION_FILE; fs.mkdirSync(path.dirname(destination),{recursive:true,mode:0o700}); fs.writeFileSync(destination,JSON.stringify([{role:'user',parts:[{text:prompt}]}]),{mode:0o600});\"\n")
}
