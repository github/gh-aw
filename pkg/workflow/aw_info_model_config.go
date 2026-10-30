package workflow

import (
	"encoding/json"
	"slices"
	"strconv"
)

// awInfoModelRouting is the configured routing policy recorded in aw_info.model_config.
// It retains the compiled objective, never the router's per-run selection.
type awInfoModelRouting struct {
	Enabled bool   `json:"enabled"`
	Goal    string `json:"goal,omitempty"`
	Mode    string `json:"mode,omitempty"`
	// Provider is the native routing provider (anthropic or openai) sent to AWF as
	// apiProxy.routing.provider; it is omitted for GitHub Copilot routing.
	Provider      string   `json:"provider,omitempty"`
	AllowedModels []string `json:"allowed_models,omitempty"`
	// CandidateModels is the router's effective pool sent to AWF: allowed-models with
	// aliases expanded and narrowed by models.allowed and models.blocked.
	CandidateModels []string `json:"candidate_models,omitempty"`
}

type awInfoModelPolicy struct {
	Allowed []string `json:"allowed"`
	Blocked []string `json:"blocked"`
}

// awInfoModelConfig is the compiled model configuration published by generate_aw_info.
// The runner later adds the resolved baseline model; the collector projects it as model.config.
type awInfoModelConfig struct {
	Routing            awInfoModelRouting `json:"routing"`
	Models             *awInfoModelPolicy `json:"models,omitempty"`
	MaxAICreditsSource string             `json:"max_ai_credits_source,omitempty"`
}

// copilotEngineHarness returns the compiled Copilot harness identifier, or "" for other engines.
func copilotEngineHarness(engineID string, data *WorkflowData) string {
	if engineID != "copilot" {
		return ""
	}
	if isCopilotSDKMode(data) {
		return "copilot-sdk"
	}
	return "copilot-cli"
}

// buildAwInfoModelConfig returns the JSON model configuration and the budget value
// (a literal or a GitHub Actions expression) for the aw_info step.
func buildAwInfoModelConfig(data *WorkflowData) (string, string) {
	config := awInfoModelConfig{}
	if isModelRoutingEnabled(data) {
		routing := data.EngineConfig.ModelRouting
		config.Routing = awInfoModelRouting{
			Enabled:  true,
			Goal:     routing.Goal,
			Mode:     routing.Mode,
			Provider: modelRoutingNativeProvider(data.EngineConfig.ID, data),
		}
		if groups, err := resolveModelRoutingCandidateGroups(data, data.EngineConfig.ID); err == nil {
			allowed := make([]string, 0, len(groups))
			for _, group := range groups {
				// Expression candidates resolve at runtime; omit the list rather than record a partial one.
				if containsExpression(group.Name) {
					allowed = nil
					break
				}
				allowed = append(allowed, group.Name)
			}
			if len(allowed) > 0 {
				config.Routing.AllowedModels = allowed
			}
		}
		policyAllowed, policyBlocked := resolveModelPolicyForAWFConfig(data)
		if candidates, err := modelRoutingAWFCandidates(data, data.EngineConfig.ID, policyAllowed, policyBlocked); err == nil &&
			!slices.ContainsFunc(candidates, containsExpression) {
			config.Routing.CandidateModels = candidates
		}
	}
	budget := ""
	if isFirewallEnabled(data) {
		allowed, blocked := resolveModelPolicyForAWFConfig(data)
		// Expression rules resolve at runtime; omit the policy rather than record a partial one.
		if !slices.ContainsFunc(allowed, containsExpression) && !slices.ContainsFunc(blocked, containsExpression) {
			config.Models = &awInfoModelPolicy{Allowed: append([]string{}, allowed...), Blocked: append([]string{}, blocked...)}
		}
		switch {
		case data.EngineConfig != nil && data.EngineConfig.MaxAICredits > 0:
			budget = strconv.FormatInt(data.EngineConfig.MaxAICredits, 10)
			config.MaxAICreditsSource = "frontmatter"
		case data.EngineConfig == nil || data.EngineConfig.MaxAICredits == 0:
			env := map[string]string{}
			applyDefaultMaxAICreditsEnvToMap(env, data)
			budget = env[awfMaxAICreditsVarName]
			config.MaxAICreditsSource = "default"
			if raw, ok := data.RawFrontmatter["max-ai-credits"].(string); ok && data.DryRun && raw == budget {
				config.MaxAICreditsSource = "frontmatter"
			}
		}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		compilerYamlStepLifecycleLog.Printf("Failed to marshal aw_info model config: %v", err)
		return "", ""
	}
	return string(encoded), budget
}
