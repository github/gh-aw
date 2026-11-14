// This file implements router values chosen at run time through the main agent's
// model expression, for example `model: ${{ inputs.level || 'router/balanced' }}`.
//
// When the main agent's model is a single Actions expression on a workflow where
// the implicit routing default could otherwise apply, the compiler emits both the
// routed and the non-routed execution paths, as it does for the implicit default.
// The activation job evaluates the expression once and publishes the result: a
// router value (router, router/economy, router/balanced, router/robust) turns
// routing on with that mode; any other value keeps the non-routed path. Later jobs
// read the published value instead of evaluating the expression again, so an
// expression over pre-activation outputs resolves the same way in every job.
// Router values in the engine's model variables are not accepted here, because
// threat detection and evals share those variables.

package workflow

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

const (
	// modelRoutingModeOutput is the activation step and job output carrying the
	// mode chosen at run time. Like modelRoutingDecisionOutput, the hyphen keeps
	// it outside the experiment-name namespace.
	modelRoutingModeOutput = "model-routing-mode"
	// modelSelectorOutput is the activation step and job output carrying the
	// resolved value of the main agent's model expression.
	modelSelectorOutput = "model-selector"
	// modelRoutingModeEnvVar carries the mode into the agent job's AWF config setup.
	modelRoutingModeEnvVar = "GH_AW_MODEL_ROUTING_MODE"
)

// These reference the mode and the resolved model expression from the activation
// job and from jobs that need it.
const (
	modelRoutingModeInActivation = "steps." + modelRoutingDecisionStepID + ".outputs." + modelRoutingModeOutput
	modelRoutingModeDownstream   = "needs.activation.outputs." + modelRoutingModeOutput
	modelSelectorInActivation    = "steps." + modelRoutingDecisionStepID + ".outputs." + modelSelectorOutput
	modelSelectorDownstream      = "needs.activation.outputs." + modelSelectorOutput
)

// The routed AWF config is compiled with the auto mode; dynamic routing replaces
// that compiled objective with one that reads the mode at run time.
const (
	awfCompiledDynamicRoutingObjective = `"objective":{"goal":"` + routerModelGoal + `","mode":"` + defaultModelRoutingMode + `"}`
	awfRuntimeDynamicRoutingObjective  = `"objective":{"goal":"` + routerModelGoal + `","mode":"${` + modelRoutingModeEnvVar + `}"}`
)

// isModelRoutingDynamic reports whether the activation job chooses routing and
// its mode from the main agent's model expression.
func isModelRoutingDynamic(data *WorkflowData) bool {
	return isModelRoutingEnabled(data) && data.EngineConfig.ModelRouting.selector != ""
}

// applyDynamicModelRouting lets an eligible main-agent model expression resolve
// to a router value at run time. Ineligible workflows keep today's behavior, in
// which a router value from an expression stops the run before the agent starts.
func (c *Compiler) applyDynamicModelRouting(ctx *workflowBuildContext) {
	if ctx == nil || ctx.workflowData == nil || !containsExpression(ctx.workflowData.Model) {
		return
	}
	data := ctx.workflowData
	if reason := dynamicModelRoutingIneligibility(data); reason != "" {
		modelRoutingDefaultLog.Printf("Dynamic model routing not applied to %s: %s", filepath.Base(ctx.cleanPath), reason)
		return
	}
	data.EngineConfig.ModelRouting = &CopilotModelRoutingConfig{Goal: routerModelGoal, Mode: defaultModelRoutingMode, selector: data.Model}
	if reason := defaultModelRoutingValidationFailure(c, data); reason != "" {
		data.EngineConfig.ModelRouting = nil
		modelRoutingDefaultLog.Printf("Dynamic model routing not applied to %s: %s", filepath.Base(ctx.cleanPath), reason)
		return
	}
	// Jobs after activation read the value the activation job resolved.
	data.Model = "${{ " + modelSelectorDownstream + " }}"
	modelRoutingDefaultLog.Printf("Applied dynamic model routing to %s for engine %s", filepath.Base(ctx.cleanPath), data.EngineConfig.ID)
}

// activationModelExpression returns the main agent's model expression as the
// activation job evaluates it, since a job cannot read its own outputs via needs.
func activationModelExpression(data *WorkflowData) string {
	if isModelRoutingDynamic(data) {
		return "${{ " + modelSelectorInActivation + " }}"
	}
	return RewriteActivationOutputsToLocalStepOutputs(data.Model, data.Experiments)
}

// dynamicModelRoutingIneligibility applies the implicit default's eligibility
// rules, apart from the configured model, and requires an engine whose runtime
// model variables the activation job can read and a model that is exactly one
// expression, such as ${{ inputs.level }} but not ${{ inputs.model }}?effort=high,
// which the activation job resolves to the same value later jobs would.
func dynamicModelRoutingIneligibility(data *WorkflowData) string {
	if reason := modelRoutingRuntimeIneligibility(data); reason != "" {
		return reason
	}
	if _, _, ok := modelRoutingRuntimeVariables(data.EngineConfig.ID); !ok {
		return "the engine has no runtime model variables"
	}
	if expression, ok := actionslanguageservice.WrappedExpression(data.Model); !ok || !activationResolvesModelExpression(expression, data.Experiments) {
		return "the model is not one expression the activation job can resolve"
	}
	return ""
}

// runWideGitHubProperties are github context properties with the same value in
// every job of a run.
var runWideGitHubProperties = []string{
	"actor", "actor_id", "api_url", "base_ref", "event", "event_name", "graphql_url", "head_ref", "ref", "ref_name",
	"ref_protected", "ref_type", "repository", "repository_id", "repository_owner", "repository_owner_id", "repositoryurl",
	"retention_days", "run_attempt", "run_id", "run_number", "server_url", "sha", "triggering_actor", "workflow",
	"workflow_ref", "workflow_sha",
}

// activationResolvesModelExpression reports whether the expression evaluates in
// the activation job as it would in later jobs. It may read inputs, vars,
// run-wide github properties, pre-activation outputs, and experiment variants.
// Anything else, such as step or other job outputs, github.job, or hashFiles,
// which reads the job's workspace, keeps the expression in the agent job without routing.
func activationResolvesModelExpression(expression *actionslanguageservice.Expression, experiments map[string][]string) bool {
	resolves := true
	actionslanguageservice.Walk(expression, func(node *actionslanguageservice.Expression) bool {
		if node.Kind == "call" && strings.EqualFold(node.Token.Text, "hashFiles") {
			resolves = false
		}
		path, ok := actionslanguageservice.Reference(node)
		if !ok {
			return resolves
		}
		resolves = resolves && activationResolvesReference(strings.Join(path, "."), experiments)
		return false
	})
	return resolves
}

func activationResolvesReference(reference string, experiments map[string][]string) bool {
	// Experiment references were rewritten to needs.activation.outputs.<name>.
	if n := len(activationOutputsPrefix); len(reference) > n && strings.EqualFold(reference[:n], activationOutputsPrefix) {
		_, declared := experiments[reference[n:]]
		return declared
	}
	context, rest, _ := strings.Cut(strings.ToLower(reference), ".")
	property, _, _ := strings.Cut(rest, ".")
	switch context {
	case "inputs", "vars":
		return true
	case "github":
		return slices.Contains(runWideGitHubProperties, property)
	case "needs":
		return strings.HasPrefix(rest+".", string(constants.PreActivationJobName)+".outputs.")
	}
	return false
}

// generateDynamicModelRoutingDecisionStep emits the activation step that resolves
// the main agent's model expression and exports its value, whether routing applies,
// and the mode. An empty or non-router value keeps the non-routed path, where the
// engine's model variables still apply as before.
func generateDynamicModelRoutingDecisionStep(data *WorkflowData) string {
	// The activation job reads experiment variants from the pick-experiment step.
	selector := strings.TrimSpace(RewriteActivationOutputsToLocalStepOutputs(data.EngineConfig.ModelRouting.selector, data.Experiments))
	return fmt.Sprintf(`      - name: Resolve model routing
        id: %s
        env:
          GH_AW_MODEL_SELECTOR: "%s"
          GH_AW_MODEL_ROUTING_GOAL: %s
        run: bash "${RUNNER_TEMP}/gh-aw/actions/resolve_model_routing.sh"
`, modelRoutingDecisionStepID, selector, data.EngineConfig.ModelRouting.Goal)
}

// dynamicRoutingAwInfoModelConfig returns the activation-job expression for the
// aw_info model configuration: the routed configuration for the mode chosen at
// run time, or the non-routed configuration when routing does not apply.
func dynamicRoutingAwInfoModelConfig(data *WorkflowData) string {
	plain, _ := buildAwInfoModelConfig(withoutModelRouting(data))
	branches := make([]string, 0, len(routerModelValues)+1)
	for _, entry := range routerModelValues {
		routed, _ := buildAwInfoModelConfig(withDynamicRoutingSelection(data, entry.value, entry.mode))
		branches = append(branches, fmt.Sprintf("%s == '%s' && '%s'", modelRoutingModeInActivation, entry.mode, strings.ReplaceAll(routed, "'", "''")))
	}
	branches = append(branches, "'"+strings.ReplaceAll(plain, "'", "''")+"'")
	return "${{ " + strings.Join(branches, " || ") + " }}"
}

// withDynamicRoutingSelection returns a shallow copy of data whose routing records
// the router value and mode chosen at run time.
func withDynamicRoutingSelection(data *WorkflowData, value, mode string) *WorkflowData {
	copied := *data
	engineConfig := *data.EngineConfig
	routing := *data.EngineConfig.ModelRouting
	routing.Mode = mode
	routing.requested = value
	engineConfig.ModelRouting = &routing
	copied.EngineConfig = &engineConfig
	return &copied
}

// injectDynamicModelRoutingMode makes the routed AWF config read its mode from
// GH_AW_MODEL_ROUTING_MODE, which the agent job sets from the activation decision.
func injectDynamicModelRoutingMode(routedJSON string) string {
	return strings.Replace(routedJSON, awfCompiledDynamicRoutingObjective, awfRuntimeDynamicRoutingObjective, 1)
}

// dynamicModelRoutingModeCheck rejects a mode outside the router's modes before
// the routed AWF config is written.
func dynamicModelRoutingModeCheck() string {
	return fmt.Sprintf(`case "${%s:-}" in
  auto|economy|balanced|robust) ;;
  *) echo "ERR_CONFIG: unsupported model-routing mode from the activation job" >&2; exit 1 ;;
esac`, modelRoutingModeEnvVar)
}

// guardDynamicRoutingDetectionModel keeps threat detection off router values: when
// the activation job routed the main agent, detection uses its default model
// instead of the main agent's model expression.
func guardDynamicRoutingDetectionModel(inherited, detectionDefault string) string {
	return fmt.Sprintf("${{ %s && (%s) || (%s) }}", modelRoutingActiveDownstream, expressionOperand(detectionDefault), expressionOperand(inherited))
}

// expressionOperand returns value as an Actions expression operand: the body of
// a single ${{ }} expression, or a quoted string literal.
func expressionOperand(value string) string {
	trimmed := strings.TrimSpace(value)
	if _, ok := actionslanguageservice.WrappedExpression(trimmed); ok {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "${{"), "}}"))
	}
	return "'" + strings.ReplaceAll(trimmed, "'", "''") + "'"
}
