// This file implements the implicit cost/balanced model-routing default.
//
// Eligible main-agent workflows that choose neither a model nor a routing
// configuration, and whose inference runs on GitHub Copilot, are routed with
// goal cost and mode balanced. The default is conservative: any explicit model or
// routing choice, the `engine.model-routing: false` opt-out, or a configuration
// whose routing eligibility is uncertain keeps the previous non-routed behavior
// without new errors, warnings, or notices.
//
// For engines with Actions model variables (copilot, claude, codex), the
// activation job decides before AWF starts whether the implicit default applies:
// a nonempty vars.GH_AW_MODEL_AGENT_<ENGINE> or vars.GH_AW_DEFAULT_MODEL_<ENGINE>
// selects the existing non-routed execution path. Explicit routing is unchanged.

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var modelRoutingDefaultLog = logger.New("workflow:model_routing_default")

const (
	defaultModelRoutingGoal = "cost"
	defaultModelRoutingMode = "balanced"

	// modelRoutingDecisionStepID is the activation step that resolves whether the
	// implicit routing default applies for this run.
	modelRoutingDecisionStepID = "resolve-model-routing"
	// modelRoutingDecisionOutput is the activation step and job output name.
	modelRoutingDecisionOutput = "model_routing"
)

// modelRoutingActiveInActivation is the expression, valid in the activation job,
// that is true when routing applies to this run.
const modelRoutingActiveInActivation = "steps." + modelRoutingDecisionStepID + ".outputs." + modelRoutingDecisionOutput + " == 'true'"

// modelRoutingInactiveInActivation is the activation-job expression that is true
// when the activation decision disabled routing for this run.
const modelRoutingInactiveInActivation = "steps." + modelRoutingDecisionStepID + ".outputs." + modelRoutingDecisionOutput + " != 'true'"

// modelRoutingInactiveDownstream is the downstream-job counterpart of
// modelRoutingInactiveInActivation.
const modelRoutingInactiveDownstream = "needs.activation.outputs." + modelRoutingDecisionOutput + " != 'true'"

// modelRoutingActiveDownstream is the expression, valid in jobs that need the
// activation job, that is true when routing applies to this run.
const modelRoutingActiveDownstream = "needs.activation.outputs." + modelRoutingDecisionOutput + " == 'true'"

// isModelRoutingImplicit reports whether routing comes from the compiler default
// rather than an authored or imported engine.model-routing block.
func isModelRoutingImplicit(data *WorkflowData) bool {
	return isModelRoutingEnabled(data) && data.EngineConfig.ModelRouting.implicit
}

// modelRoutingRuntimeVariables returns the Actions variables that select a fixed
// main-agent model for engines whose legacy non-routed path reads them.
func modelRoutingRuntimeVariables(engineID string) (agentVar, defaultVar string, ok bool) {
	switch strings.ToLower(engineID) {
	case string(constants.CopilotEngine):
		return constants.EnvVarModelAgentCopilot, compilerenv.DefaultModelCopilot, true
	case string(constants.ClaudeEngine):
		return constants.EnvVarModelAgentClaude, compilerenv.DefaultModelClaude, true
	case string(constants.CodexEngine):
		return constants.EnvVarModelAgentCodex, compilerenv.DefaultModelCodex, true
	default:
		return "", "", false
	}
}

// isModelRoutingRuntimeGuarded reports whether the implicit routing default is
// decided at run time by the activation job from the engine's model variables.
func isModelRoutingRuntimeGuarded(data *WorkflowData) bool {
	if !isModelRoutingImplicit(data) {
		return false
	}
	_, _, ok := modelRoutingRuntimeVariables(data.EngineConfig.ID)
	return ok
}

// isModelRoutingUnconditional reports whether routing applies to every run, so
// compile-time emissions can omit the non-routed model settings.
func isModelRoutingUnconditional(data *WorkflowData) bool {
	return isModelRoutingEnabled(data) && !isModelRoutingRuntimeGuarded(data)
}

// withoutModelRouting returns a shallow copy of data whose engine config has no
// routing, describing the non-routed execution path of a runtime-guarded workflow.
func withoutModelRouting(data *WorkflowData) *WorkflowData {
	if !isModelRoutingEnabled(data) {
		return data
	}
	copied := *data
	engineConfig := *data.EngineConfig
	engineConfig.ModelRouting = nil
	copied.EngineConfig = &engineConfig
	return &copied
}

// guardModelExpressionForImplicitRouting wraps a ${{ }} model expression so it is
// empty when the activation job selected implicit routing for this run.
func guardModelExpressionForImplicitRouting(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "${{") || !strings.HasSuffix(trimmed, "}}") || strings.Count(trimmed, "${{") != 1 {
		return value
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "${{"), "}}"))
	// The non-routed model expression always resolves to a nonempty value when the
	// activation job disables routing, because that requires a nonempty variable.
	return fmt.Sprintf("${{ %s && (%s) || '' }}", modelRoutingInactiveDownstream, inner)
}

// guardImplicitRoutingModelEnv wraps the named non-routed model env entries so
// they are populated only when the activation job disabled implicit routing.
func guardImplicitRoutingModelEnv(env map[string]string, data *WorkflowData, keys ...string) {
	if !isModelRoutingRuntimeGuarded(data) {
		return
	}
	for _, key := range keys {
		if value, ok := env[key]; ok {
			env[key] = guardModelExpressionForImplicitRouting(value)
		}
	}
}

// modelRoutingRuntimeFlag returns the GH_AW_MODEL_ROUTING value for the agent job.
func modelRoutingRuntimeFlag(data *WorkflowData) string {
	if isModelRoutingRuntimeGuarded(data) {
		return "${{ " + modelRoutingActiveDownstream + " && '1' || '0' }}"
	}
	return "1"
}

// generateModelRoutingDecisionStep emits the activation step that decides, before
// AWF starts, whether the implicit routing default applies. A nonempty phase-specific
// agent model variable wins over the engine default model variable; either one
// selects the legacy non-routed execution path.
func generateModelRoutingDecisionStep(data *WorkflowData) string {
	if !isModelRoutingRuntimeGuarded(data) {
		return ""
	}
	agentVar, defaultVar, _ := modelRoutingRuntimeVariables(data.EngineConfig.ID)
	var b strings.Builder
	b.WriteString("      - name: Resolve model routing\n")
	fmt.Fprintf(&b, "        id: %s\n", modelRoutingDecisionStepID)
	b.WriteString("        env:\n")
	fmt.Fprintf(&b, "          GH_AW_MODEL_AGENT_VAR: ${{ vars.%s }}\n", agentVar)
	fmt.Fprintf(&b, "          GH_AW_DEFAULT_MODEL_VAR: ${{ vars.%s }}\n", defaultVar)
	b.WriteString("        run: |\n")
	fmt.Fprintf(&b, "          if [ -n \"${GH_AW_MODEL_AGENT_VAR}\" ]; then\n")
	fmt.Fprintf(&b, "            echo \"Model routing disabled: vars.%s selects the model\"\n", agentVar)
	fmt.Fprintf(&b, "            echo \"%s=false\" >> \"$GITHUB_OUTPUT\"\n", modelRoutingDecisionOutput)
	fmt.Fprintf(&b, "          elif [ -n \"${GH_AW_DEFAULT_MODEL_VAR}\" ]; then\n")
	fmt.Fprintf(&b, "            echo \"Model routing disabled: vars.%s selects the model\"\n", defaultVar)
	fmt.Fprintf(&b, "            echo \"%s=false\" >> \"$GITHUB_OUTPUT\"\n", modelRoutingDecisionOutput)
	b.WriteString("          else\n")
	fmt.Fprintf(&b, "            echo \"Model routing enabled: goal %s, mode %s\"\n", data.EngineConfig.ModelRouting.Goal, data.EngineConfig.ModelRouting.Mode)
	fmt.Fprintf(&b, "            echo \"%s=true\" >> \"$GITHUB_OUTPUT\"\n", modelRoutingDecisionOutput)
	b.WriteString("          fi\n")
	return b.String()
}

// declaresModelOrRoutingChoice reports whether the main workflow or its resolved
// imports choose a model or routing: a top-level or engine model (including
// aliases, auto, expressions, and empty values), engine.model-routing (including
// false), fallback models, or a recognized engine.env model override.
func declaresModelOrRoutingChoice(frontmatter map[string]any, model string, engineSpecs []string, mergedEngineModel string) bool {
	if model != "" || mergedEngineModel != "" {
		return true
	}
	if _, ok := frontmatter["model"]; ok {
		return true
	}
	if engineObj, ok := frontmatter["engine"].(map[string]any); ok && engineObjectDeclaresModelChoice(engineObj) {
		return true
	}
	for _, spec := range engineSpecs {
		var engine any
		if err := json.Unmarshal([]byte(spec), &engine); err != nil {
			return true
		}
		if engineObj, ok := engine.(map[string]any); ok && engineObjectDeclaresModelChoice(engineObj) {
			return true
		}
	}
	return false
}

func engineObjectDeclaresModelChoice(engineObj map[string]any) bool {
	for _, key := range []string{"model", "model-routing", "fallback-models"} {
		if _, ok := engineObj[key]; ok {
			return true
		}
	}
	if env, ok := engineObj["env"].(map[string]any); ok {
		for key := range env {
			if isModelRoutingOverrideEnvKey(key) {
				return true
			}
		}
	}
	return false
}

// applyDefaultModelRouting applies the implicit cost/balanced routing default to
// an eligible workflow. Ineligible or uncertain workflows are left unchanged.
func (c *Compiler) applyDefaultModelRouting(ctx *workflowBuildContext) {
	if ctx == nil || ctx.workflowData == nil || ctx.engineSetup == nil || ctx.engineSetup.modelChoiceDeclared {
		return
	}
	data := ctx.workflowData
	if reason := defaultModelRoutingIneligibility(data); reason != "" {
		modelRoutingDefaultLog.Printf("Implicit model routing not applied to %s: %s", filepath.Base(ctx.cleanPath), reason)
		return
	}
	data.EngineConfig.ModelRouting = &CopilotModelRoutingConfig{Goal: defaultModelRoutingGoal, Mode: defaultModelRoutingMode, implicit: true}
	if reason := defaultModelRoutingValidationFailure(c, data); reason != "" {
		data.EngineConfig.ModelRouting = nil
		modelRoutingDefaultLog.Printf("Implicit model routing not applied to %s: %s", filepath.Base(ctx.cleanPath), reason)
		return
	}
	modelRoutingDefaultLog.Printf("Applied implicit model routing (goal=%s, mode=%s) to %s for engine %s", defaultModelRoutingGoal, defaultModelRoutingMode, filepath.Base(ctx.cleanPath), data.EngineConfig.ID)
	if c.batchMode {
		// Batch compilation prints one aggregated informational notice instead of
		// one line per workflow.
		c.defaultRoutingCount++
		return
	}
	if c.quiet {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(defaultModelRoutingNotice(filepath.Base(ctx.cleanPath), data.EngineConfig.ID)))
}

// DefaultModelRoutingBatchNotice is the aggregated informational notice for
// workflows that received the implicit model-routing default in batch mode.
func DefaultModelRoutingBatchNotice(count int) string {
	return fmt.Sprintf("%d workflow(s) set no model or engine.model-routing, so they use model routing with goal %s and mode %s on GitHub Copilot inference. "+
		"Set engine.model-routing: false to opt out, or choose a model. A nonempty vars.GH_AW_MODEL_AGENT_<ENGINE> or vars.GH_AW_DEFAULT_MODEL_<ENGINE> bypasses routing at run time. "+
		"See https://github.github.com/gh-aw/reference/model-routing/", count, defaultModelRoutingGoal, defaultModelRoutingMode)
}

func defaultModelRoutingNotice(workflowFile, engineID string) string {
	message := fmt.Sprintf("%s: no model or engine.model-routing is set, so the %s engine uses model routing with goal %s and mode %s on GitHub Copilot inference. Set engine.model-routing: false to opt out, or choose a model.",
		workflowFile, engineID, defaultModelRoutingGoal, defaultModelRoutingMode)
	if agentVar, defaultVar, ok := modelRoutingRuntimeVariables(engineID); ok {
		message += fmt.Sprintf(" A nonempty vars.%s or vars.%s bypasses routing at run time.", agentVar, defaultVar)
	}
	return message + " See https://github.github.com/gh-aw/reference/model-routing/"
}

// defaultModelRoutingIneligibility returns why the implicit default cannot be
// applied safely, or "" when the workflow is a candidate.
func defaultModelRoutingIneligibility(data *WorkflowData) string {
	config := data.EngineConfig
	switch {
	case config == nil:
		return "no engine configuration"
	case config.ModelRouting != nil:
		return "model routing is already configured"
	case config.ModelRoutingDisabled:
		return "engine.model-routing is false"
	case data.Model != "":
		return "a model is configured"
	}
	engineID := strings.ToLower(config.ID)
	if engineID != string(constants.CopilotEngine) && engineID != string(constants.ClaudeEngine) &&
		engineID != string(constants.CodexEngine) && engineID != string(constants.PiEngine) {
		return "unsupported engine"
	}
	if config.IsInlineDefinition || config.InlineProviderID != "" || config.InlineProviderAuth != nil || config.InlineProviderRequest != nil {
		return "inline engine definition"
	}
	if !isFirewallEnabled(data) {
		return "AWF firewall is disabled"
	}
	if modelRoutingProvider(engineID, data) != LLMProviderGitHub || modelRoutingEngineCredentialProvider(engineID, data) != LLMProviderGitHub {
		return "inference provider is not GitHub Copilot"
	}
	if len(config.FallbackModels) > 0 {
		return "fallback models are configured"
	}
	if config.Command != "" || config.HarnessScript != "" || config.Driver != "" || config.InlineDriver != nil {
		return "custom execution path"
	}
	if config.Agent != "" || data.AgentFile != "" || len(data.SubAgentModels) > 0 {
		return "custom agent or sub-agent models"
	}
	if config.Config != "" || config.APITarget != "" || config.Auth != nil || config.Durable != nil || config.durableError != nil {
		return "custom engine configuration"
	}
	if argsMayChooseModel(config.Args) {
		return "engine arguments may choose a model"
	}
	for key := range config.Env {
		upperKey := strings.ToUpper(key)
		if isModelRoutingOverrideEnvKey(key) || strings.Contains(upperKey, "BASE_URL") ||
			strings.Contains(upperKey, "EFFORT") || strings.Contains(upperKey, "THINKING") || strings.HasPrefix(upperKey, "COPILOT_PROVIDER_") {
			return "engine.env may choose a model or inference target"
		}
	}
	if data.TokenBrokerCopilotRequest || data.TokenBrokerEnabled || enclavesEnabled(data) {
		return "custom inference credentials or enclaves"
	}
	if isArcDindTopology(data) || isCloudHypervisorRuntime(data) || isNVXRuntime(data) {
		return "non-default sandbox runtime"
	}
	if agentConfig := getAgentConfig(data); agentConfig != nil &&
		(agentConfig.Command != "" || len(agentConfig.Images) > 0 || len(agentConfig.Targets) > 0) {
		return "custom AWF command, images, or API targets"
	}
	if len(customAWFArgs(data)) > 0 {
		return "custom AWF arguments"
	}
	return ""
}

// argsMayChooseModel conservatively detects engine arguments that may select a
// model, reasoning effort, or provider.
func argsMayChooseModel(args []string) bool {
	if codexArgsSelectModel(args) {
		return true
	}
	for _, arg := range args {
		lower := strings.ToLower(arg)
		for _, marker := range []string{"model", "effort", "reasoning", "thinking", "provider"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

// defaultModelRoutingValidationFailure reuses the explicit-routing validators on
// the tentatively routed workflow; any failure or routed warning makes the
// default ineligible.
func defaultModelRoutingValidationFailure(c *Compiler, data *WorkflowData) string {
	engineID := data.EngineConfig.ID
	if err := validateModelRouting(data, engineID); err != nil {
		return err.Error()
	}
	if err := validateSandboxAgentImages(data); err != nil {
		return err.Error()
	}
	if err := c.validatePiEngineConfig(data); err != nil {
		return err.Error()
	}
	if err := validateEngineFallbackModels(data); err != nil {
		return err.Error()
	}
	if warnings := modelRoutingCompileWarnings(data); len(warnings) > 0 {
		return strings.Join(warnings, "; ")
	}
	return ""
}
