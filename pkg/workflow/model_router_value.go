// This file implements router model values: `model: router` and
// `router/economy`, `router/balanced`, `router/robust`.
//
// A router value chooses task-level model routing for the main agent the same way
// `engine.model-routing` with `goal: cost` does. After model precedence is resolved
// (engine.model over top-level model, the workflow's own model over an imported one),
// a router value is normalized into the existing CopilotModelRoutingConfig and the
// main-agent model is cleared, so validation, the AWF config and the lock output
// reuse the explicit-routing path.
//
// `router` and `router/*` are reserved: they are not model aliases, cannot be
// defined in `models:`, and cannot appear in model lists or in models for other
// jobs (sub-agents, threat detection, evals, enclave agents).

package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var modelRouterValueLog = logger.New("workflow:model_router_value")

// routerModelGoal is the routing goal every router value uses.
const routerModelGoal = "cost"

// routerModelValues maps each router value to its model-routing mode, in documentation order.
var routerModelValues = []struct {
	value string
	mode  string
}{
	{"router", "auto"},
	{"router/economy", "economy"},
	{"router/balanced", "balanced"},
	{"router/robust", "robust"},
}

// validRouterModelValues returns the router values for error messages.
func validRouterModelValues() string {
	values := make([]string, 0, len(routerModelValues))
	for _, entry := range routerModelValues {
		values = append(values, entry.value)
	}
	return strings.Join(values, ", ")
}

// isReservedRouterModelName reports whether model uses the reserved router
// namespace (`router` or `router/...`, in any case, with or without parameters).
// Expressions are resolved at run time and are never reserved at compile time.
func isReservedRouterModelName(model string) bool {
	if containsExpression(model) {
		return false
	}
	base, _, _ := strings.Cut(strings.TrimSpace(model), "?")
	base = strings.ToLower(base)
	return base == "router" || strings.HasPrefix(base, "router/")
}

// routerModelMode returns the model-routing mode for a router value, or an error
// naming the problem for reserved names that are not router values.
func routerModelMode(field, model string) (string, error) {
	if base, params, hasParams := strings.Cut(model, "?"); hasParams {
		return "", NewValidationError(field, model,
			fmt.Sprintf("router values do not take parameters (got %q)", "?"+params),
			fmt.Sprintf("The router chooses the model and effort. Use %s, or pin a model to set the effort, for example model: gpt-6.1-sol?effort=high.", base))
	}
	for _, entry := range routerModelValues {
		if model == entry.value {
			return entry.mode, nil
		}
	}
	return "", NewValidationError(field, model, "unknown router value",
		"Use one of: "+validRouterModelValues()+".")
}

// applyRouterModelValue normalizes a main-agent router value into the engine's
// model-routing configuration. It returns the main-agent model to use, which is
// empty when model is a router value.
func applyRouterModelValue(engineConfig *EngineConfig, model string) (string, error) {
	if !isReservedRouterModelName(model) {
		return model, nil
	}
	mode, err := routerModelMode("model", model)
	if err != nil {
		return "", err
	}
	if engineConfig == nil {
		return "", NewValidationError("model", model, "model routing requires an engine configuration", "Set engine.id to copilot, claude, codex, or pi.")
	}
	if engineConfig.ModelRouting != nil {
		return "", NewValidationError("model", model,
			"a router value and engine.model-routing both choose model routing",
			fmt.Sprintf("Use one of them: keep model: %s and remove engine.model-routing (including any imported engine.model-routing), or pin a model.", model))
	}
	if engineConfig.ModelRoutingDisabled {
		return "", NewValidationError("model", model,
			"a router value cannot be combined with engine.model-routing: false",
			"Remove engine.model-routing: false (including any imported one) to route, or pin a model to opt out of routing.")
	}
	if len(engineConfig.FallbackModels) > 0 {
		return "", NewValidationError("model", model,
			"a router value cannot be combined with engine.fallback-models",
			"Remove engine.fallback-models, or pin a model and keep fixed fallback models.")
	}
	engineConfig.ModelRouting = &CopilotModelRoutingConfig{Goal: routerModelGoal, Mode: mode, requested: model}
	modelRouterValueLog.Printf("Normalized model %q to model routing (goal=%s, mode=%s)", model, routerModelGoal, mode)
	return "", nil
}

// validateRouterModelValuePlacement rejects router values outside the main
// agent's model: in model lists, in model aliases, and in the models of
// sub-agents, threat detection, evals and enclave agents.
func validateRouterModelValuePlacement(data *WorkflowData) error {
	if data == nil {
		return nil
	}
	otherJob := func(field, model string) error {
		return NewValidationError(field, model, "only the main agent can be routed",
			"Router values ("+validRouterModelValues()+") are accepted only in the main agent's model (top-level model or engine.model). Pin a model here.")
	}
	modelList := func(field, model string) error {
		return NewValidationError(field, model, "router values are not models",
			"This list takes models, not router values ("+validRouterModelValues()+"). To route the main agent, set model: router (or router/economy, router/balanced, router/robust).")
	}
	if data.EngineConfig != nil {
		for _, model := range data.EngineConfig.FallbackModels {
			if isReservedRouterModelName(model) {
				return modelList("engine.fallback-models", model)
			}
		}
		if routing := data.EngineConfig.ModelRouting; routing != nil {
			for _, model := range routing.AllowedModels {
				if isReservedRouterModelName(model) {
					return modelList("engine.model-routing.allowed-models", model)
				}
			}
		}
	}
	for _, model := range data.ModelPolicyAllowed {
		if isReservedRouterModelName(model) {
			return modelList("models.allowed", model)
		}
	}
	for _, model := range data.ModelPolicyBlocked {
		if isReservedRouterModelName(model) {
			return modelList("models.blocked", model)
		}
	}
	keys := make([]string, 0, len(data.ModelMappings))
	for key := range data.ModelMappings {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if strings.EqualFold(key, "router") {
			return NewValidationError("models."+key, key, "router is reserved and cannot be defined as a model alias",
				"Rename the alias. To route the main agent, set model: router (or router/economy, router/balanced, router/robust).")
		}
		for _, entry := range data.ModelMappings[key] {
			if isReservedRouterModelName(entry) {
				return modelList("models."+displayKey(key), entry)
			}
		}
	}
	for _, agent := range data.SubAgentModels {
		if isReservedRouterModelName(agent.Model) {
			return otherJob(fmt.Sprintf("sub-agent %q model", agent.Name), agent.Model)
		}
	}
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && isReservedRouterModelName(data.SafeOutputs.ThreatDetection.Model) {
		return otherJob("safe-outputs.threat-detection.model", data.SafeOutputs.ThreatDetection.Model)
	}
	if data.Evals != nil && isReservedRouterModelName(data.Evals.Model) {
		return otherJob("evals.model", data.Evals.Model)
	}
	for i, enclave := range data.Enclaves {
		if enclave != nil && enclave.Agent != nil && isReservedRouterModelName(enclave.Agent.Model) {
			return otherJob(fmt.Sprintf("enclaves[%d].agent.model", i), enclave.Agent.Model)
		}
	}
	return nil
}
