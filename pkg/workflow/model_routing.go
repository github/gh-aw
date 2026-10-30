package workflow

import (
	"encoding/json"
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

func applyModelRoutingEnv(env map[string]string, data *WorkflowData) {
	if isModelRoutingEnabled(data) {
		env["GH_AW_MODEL_ROUTING"] = "1"
	}
}

func validModelRoutingCandidate(model string) bool {
	if !containsExpression(model) {
		return routingModelNamePattern.MatchString(model)
	}
	withoutExpressions := ExpressionPattern.ReplaceAllString(model, "x")
	return routingModelNamePattern.MatchString(withoutExpressions)
}

// subAgentModelPolicyWarnings reports declared sub-agent models that AWF will reject
// because models.allowed or models.blocked excludes every model they resolve to.
func subAgentModelPolicyWarnings(data *WorkflowData, allowed, blocked []string) []string {
	if len(allowed) == 0 && len(blocked) == 0 {
		return nil
	}
	var warnings []string
	for _, agent := range data.SubAgentModels {
		if !subAgentModelAllowedByPolicy(agent.Model, data, allowed, blocked) {
			warnings = append(warnings, fmt.Sprintf("sub-agent %q model %q is outside models.allowed or models.blocked (or resolves to no models); AWF will reject its requests", agent.Name, agent.Model))
		}
	}
	return warnings
}

func subAgentModelAllowedByPolicy(request string, data *WorkflowData, allowed, blocked []string) bool {
	namespace := copilotRoutingNamespace
	if data != nil && data.EngineConfig != nil {
		namespace = modelRoutingNamespace(data, data.EngineConfig.ID)
	}
	for _, pattern := range expandSubagentModelPatterns(request, data, namespace) {
		if len(allowed) > 0 && strings.ContainsAny(pattern, "*[") {
			for _, rule := range allowed {
				if strings.ContainsAny(rule, "*[") {
					continue
				}
				qualified := rule
				if !strings.Contains(rule, "/") {
					qualified = qualifiedModelID(namespace, rule)
				}
				if matched, _ := path.Match(pattern, qualified); matched && !matchesModelPolicy(rule, qualified, blocked) {
					return true
				}
			}
		}
		model := strings.TrimPrefix(pattern, namespace+"/")
		if len(allowed) > 0 && !matchesModelPolicy(model, pattern, allowed) {
			continue
		}
		if matchesModelPolicy(model, pattern, blocked) {
			continue
		}
		return true
	}
	return false
}

func expandModelPatterns(request string, aliases map[string][]string, defaultProvider string) []string {
	var patterns []string
	walkModelPatterns(request, aliases, defaultProvider, func(pattern, _ string) {
		patterns = append(patterns, pattern)
	})
	return patterns
}

// walkModelPatterns visits each provider-qualified pattern that request expands to,
// together with the alias whose entry produced it ("" for a literal request; "" is
// also a valid alias name, the default policy).
func walkModelPatterns(request string, aliases map[string][]string, defaultProvider string, visit func(pattern, alias string)) {
	var expand func(string, string, bool, map[string]bool)
	expand = func(model, source string, fromAlias bool, visited map[string]bool) {
		model, _, _ = strings.Cut(model, "?")
		if entries, ok := aliases[model]; ok {
			if visited[model] {
				return
			}
			visited[model] = true
			for _, entry := range entries {
				expand(entry, model, true, visited)
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
			visit(model, source)
		}
	}
	expand(request, "", false, make(map[string]bool))
}

func (c *Compiler) warnRoutedSubAgentModels(data *WorkflowData) {
	if !isModelRoutingEnabled(data) {
		return
	}
	c.warnRoutedModelOverrides(data)
	allowed, blocked := resolveModelPolicyForAWFConfig(data)
	if groups, err := resolveModelRoutingCandidateGroups(data, data.EngineConfig.ID); err == nil && len(groups) > 0 {
		_, excluded, err := intersectModelRoutingPolicy(groups, allowed, blocked, !apiProxySupportsRoutingCandidateModels(data))
		if err == nil && len(excluded) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(
				"engine.model-routing.allowed-models entries excluded by models.allowed or models.blocked will not be selected: "+strings.Join(excluded, ", ")))
			c.IncrementWarningCount()
		}
	}
	if len(data.SubAgentModels) == 0 {
		return
	}
	namespace := modelRoutingNamespace(data, data.EngineConfig.ID)
	for _, agent := range data.SubAgentModels {
		for _, model := range expandSubagentModelPatterns(agent.Model, data, namespace) {
			if !modelRoutingEngineSupportsModel(data.EngineConfig.ID, model) {
				warning := fmt.Sprintf("sub-agent %q model %q may be incompatible with the %s engine's model-routing API", agent.Name, agent.Model, data.EngineConfig.ID)
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(warning))
				c.IncrementWarningCount()
				break
			}
		}
	}
	firewall := getFirewallConfig(data)
	if !awfVersionAtLeast(firewall, constants.AWFRoutingCandidateModelsMinVersion) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(
			fmt.Sprintf("sub-agent models are limited to engine.model-routing.allowed-models with AWF %s; use AWF %s or newer to apply models.allowed and models.blocked instead",
				getAWFImageTag(firewall), constants.AWFRoutingCandidateModelsMinVersion)))
		c.IncrementWarningCount()
		return
	}
	if !apiProxySupportsRoutingCandidateModels(data) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(
			"sub-agent model routing requires an AWF apiProxy image with routing.candidateModels support (v0.28.33+); the configured image is older or its version is unknown. Pin a compatible image in sandbox.agent.images."))
		c.IncrementWarningCount()
	}
	for _, warning := range subAgentModelPolicyWarnings(data, allowed, blocked) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(warning))
		c.IncrementWarningCount()
	}
}

func (c *Compiler) warnRoutedModelOverrides(data *WorkflowData) {
	if !isModelRoutingEnabled(data) {
		return
	}
	var warnings []string
	if data.Model != "" {
		warnings = append(warnings, "model")
	}
	if data.EngineConfig != nil {
		for key := range data.EngineConfig.Env {
			if isModelRoutingOverrideEnvKey(key) {
				warnings = append(warnings, "engine.env."+key)
			}
		}
		if data.EngineConfig.ID == "pi" && data.EngineConfig.Config != "" {
			var config struct {
				Settings struct {
					DefaultThinkingLevel string `json:"defaultThinkingLevel"`
				} `json:"settings"`
			}
			if json.Unmarshal([]byte(data.EngineConfig.Config), &config) == nil && config.Settings.DefaultThinkingLevel != "" {
				warnings = append(warnings, "engine.config.settings.defaultThinkingLevel")
			}
		}
	}
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(
		"model-routing selection overrides fixed model or effort settings: "+strings.Join(warnings, ", ")))
	c.IncrementWarningCount()
}

func isModelRoutingOverrideEnvKey(key string) bool {
	upperKey := strings.ToUpper(key)
	return strings.EqualFold(key, "MODEL") ||
		strings.HasSuffix(upperKey, "_MODEL") ||
		strings.HasSuffix(upperKey, "_MODEL_ID") ||
		strings.HasSuffix(upperKey, "_MODEL_NAME") ||
		strings.HasPrefix(upperKey, "GH_AW_MODEL_AGENT_") ||
		strings.HasPrefix(upperKey, "GH_AW_MODEL_DETECTION_") ||
		strings.HasPrefix(upperKey, "GH_AW_MODEL_EVALS_") ||
		strings.HasPrefix(upperKey, "GH_AW_DEFAULT_MODEL_")
}

func modelRoutingEngineSupportsModel(engineName, candidate string) bool {
	if containsExpression(candidate) {
		return true
	}
	_, model, qualified := strings.Cut(candidate, "/")
	if qualified {
		candidate = model
	}
	candidate = strings.TrimPrefix(candidate, "copilot/")
	engineName = strings.ToLower(engineName)
	switch engineName {
	case "copilot", "pi":
		return true
	case "claude", "codex":
		return strings.HasPrefix(strings.ToLower(candidate), modelRoutingEngineFamilyPrefixes[engineName])
	default:
		return false
	}
}

// modelRoutingEngineFamilyPrefixes holds the model families that engines with a
// family-specific wire API (Claude's Messages API, Codex's Responses API) can call.
var modelRoutingEngineFamilyPrefixes = map[string]string{
	"claude": "claude-",
	"codex":  "gpt-",
}

// modelRoutingDefaultCandidates returns the router candidates used when
// engine.model-routing.allowed-models is omitted: the engine's model family for
// Claude and Codex, qualified with the routing namespace (github-copilot, or the
// engine's native provider), and none (the policy-filtered catalogue) for Copilot and pi.
func modelRoutingDefaultCandidates(engineName, namespace string) []string {
	if prefix, ok := modelRoutingEngineFamilyPrefixes[strings.ToLower(engineName)]; ok {
		return []string{qualifiedModelID(namespace, prefix+"*")}
	}
	return nil
}

// modelRoutingDefaultCandidatesPolicyError reports when models.allowed or models.blocked leaves
// no model in the Claude or Codex family that the router uses when allowed-models is omitted.
func modelRoutingDefaultCandidatesPolicyError(engineName, namespace string, allowed, blocked []string) error {
	prefix, ok := modelRoutingEngineFamilyPrefixes[strings.ToLower(engineName)]
	if !ok {
		return nil
	}
	err := fmt.Errorf("all engine.model-routing.allowed-models are excluded by models.allowed or models.blocked policy (allowed-models is omitted, so the %s engine uses its model family, %s*)", strings.ToLower(engineName), prefix)
	for _, rule := range blocked {
		if modelPolicyRuleCoversFamily(rule, prefix, namespace) {
			return err
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, rule := range allowed {
		if !modelPolicyRuleMayMatchFamily(rule, prefix, namespace) {
			continue
		}
		if !strings.ContainsAny(rule, "*?[\\") {
			if qualified, _ := qualifiedModelPolicyRule(rule, namespace); modelPolicyRulesCover(blocked, qualified) {
				continue
			}
		}
		return nil
	}
	return err
}

func splitModelPolicyRule(rule string) (string, string) {
	if i := strings.LastIndex(rule, "/"); i >= 0 {
		return rule[:i], rule[i+1:]
	}
	return "", rule
}

// modelPolicyRuleMayMatchFamily reports whether rule can match a model in namespace whose name starts with prefix.
func modelPolicyRuleMayMatchFamily(rule, prefix, namespace string) bool {
	qualified, ok := qualifiedModelPolicyRule(rule, namespace)
	if !ok {
		return false
	}
	_, model := splitModelPolicyRule(qualified)
	model = strings.ToLower(model)
	if i := strings.IndexAny(model, "*?[\\"); i >= 0 {
		head := model[:i]
		return strings.HasPrefix(prefix, head) || strings.HasPrefix(head, prefix)
	}
	return strings.HasPrefix(model, prefix)
}

// modelPolicyRuleCoversFamily reports whether rule matches every model in namespace whose name starts with prefix.
func modelPolicyRuleCoversFamily(rule, prefix, namespace string) bool {
	return modelPolicyRulesCover([]string{rule}, qualifiedModelID(namespace, prefix+"*"))
}

// modelRoutingNativeLLMProviders maps the engines that can route on their own
// provider's API to that provider. AWF routes natively only on the matching
// configured provider (apiProxy.routing.provider) and never crosses providers.
var modelRoutingNativeLLMProviders = map[string]LLMProvider{
	"claude": LLMProviderAnthropic,
	"codex":  LLMProviderOpenAI,
}

// modelRoutingNativeProvider returns the AWF routing provider (anthropic or openai)
// when engine.model-routing runs on the engine's native provider, or "" when it runs
// on GitHub Copilot (or is not supported).
func modelRoutingNativeProvider(engineName string, data *WorkflowData) string {
	engineName = strings.ToLower(engineName)
	native, ok := modelRoutingNativeLLMProviders[engineName]
	if !ok || modelRoutingProvider(engineName, data) != native {
		return ""
	}
	return string(native)
}

// modelRoutingNamespace returns the provider namespace of the router's candidates:
// the native provider for native routing, otherwise github-copilot.
func modelRoutingNamespace(data *WorkflowData, engineName string) string {
	if provider := modelRoutingNativeProvider(engineName, data); provider != "" {
		return provider
	}
	return copilotRoutingNamespace
}

func modelRoutingProvider(engineName string, data *WorkflowData) LLMProvider {
	switch strings.ToLower(engineName) {
	case "copilot":
		return resolveEngineLLMProvider(data, LLMProviderGitHub)
	case "claude":
		return resolveEngineLLMProvider(data, LLMProviderAnthropic)
	case "codex":
		return resolveEngineLLMProvider(data, LLMProviderOpenAI)
	case "pi":
		return resolveEngineLLMProvider(data, LLMProviderGitHub)
	default:
		return LLMProviderGitHub
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
	engineName = strings.ToLower(engineName)
	if engineName != "copilot" && engineName != "claude" && engineName != "codex" && engineName != "pi" {
		return NewValidationError("engine.model-routing", engineName, "task-level model routing is supported only by Copilot, Claude, Codex, and pi engines", "Set engine.id to copilot, claude, codex, or pi, or remove engine.model-routing.")
	}
	if !isFirewallEnabled(workflowData) {
		return NewValidationError("engine.model-routing", "", "task-level model routing requires the AWF firewall", "Enable the AWF firewall for this workflow.")
	}
	if err := validateModelRoutingProviderTarget(workflowData, engineName); err != nil {
		return err
	}
	if routing.Goal != "cost" && routing.Goal != "cost-speed" {
		return NewValidationError("engine.model-routing.goal", routing.Goal, "unsupported model-routing goal", "Use cost or cost-speed.")
	}
	if routing.Mode != "economy" && routing.Mode != "balanced" && routing.Mode != "robust" && routing.Mode != "auto" {
		return NewValidationError("engine.model-routing.mode", routing.Mode, "unsupported model-routing mode", "Use economy, balanced, robust, or auto.")
	}
	groups, err := resolveModelRoutingCandidateGroups(workflowData, engineName)
	if err != nil {
		return NewValidationError("engine.model-routing.allowed-models", strings.Join(routing.AllowedModels, ", "), "invalid model-routing candidates", err.Error())
	}
	for _, group := range groups {
		// Aliases are already limited to the engine's model family when they are expanded.
		if !group.Alias && !modelRoutingEngineSupportsModel(engineName, group.Name) {
			candidate := group.Name
			message := fmt.Sprintf("model %q is incompatible with the %s engine's Copilot API", candidate, engineName)
			switch engineName {
			case "claude":
				message = fmt.Sprintf("model %q is incompatible with Claude's native Messages API; allow only Claude models", candidate)
			case "codex":
				message = fmt.Sprintf("model %q is incompatible with Codex's Responses API; allow only GPT models", candidate)
			}
			return NewValidationError("engine.model-routing.allowed-models", candidate, "incompatible model-routing candidate", message)
		}
	}
	if len(groups) == 0 && !apiProxySupportsRoutingCandidateModels(workflowData) {
		return NewValidationError("engine.model-routing.allowed-models", "", "missing model-routing candidates",
			fmt.Sprintf("AWF versions before %s, or an apiProxy image without routing.candidateModels support, route only over engine.model-routing.allowed-models. List the candidate models, or use AWF %s or newer.",
				constants.AWFRoutingCandidateModelsMinVersion, constants.AWFRoutingCandidateModelsMinVersion))
	}
	allowed, blocked := resolveModelPolicyForAWFConfig(workflowData)
	if len(groups) > 0 {
		if _, _, err := intersectModelRoutingPolicy(groups, allowed, blocked, !apiProxySupportsRoutingCandidateModels(workflowData)); err != nil {
			return NewValidationError("engine.model-routing.allowed-models", strings.Join(routing.AllowedModels, ", "), "model-routing candidates violate model policy", err.Error())
		}
	} else if err := modelRoutingDefaultCandidatesPolicyError(engineName, modelRoutingNamespace(workflowData, engineName), allowed, blocked); err != nil {
		return NewValidationError("engine.model-routing.allowed-models", "", "model-routing candidates violate model policy", err.Error())
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
