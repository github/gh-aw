package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// Resolve the shared budget after admission, before issuing any model requests.
func (c *Compiler) buildDecisionJob(data *WorkflowData, activation *Job) (*Job, error) {
	env := map[string]string{}
	applyDefaultMaxAICreditsEnvToMap(env, data)
	delete(env, "GH_AW_ADMISSION_RESULT")
	delete(env, "GH_AW_ADMISSION_MAX_AI_CREDITS")
	delete(env, "GH_AW_DECISION_REMAINING_AI_CREDITS")
	if data.EngineConfig != nil && data.EngineConfig.MaxAICredits != 0 {
		env[awfMaxAICreditsVarName] = strconv.FormatInt(data.EngineConfig.MaxAICredits, 10)
	}
	if admissionBudgetOutputsEnabled(data) {
		env["GH_AW_ADMISSION_RESULT"] = "${{ needs.admission.outputs.result }}"
		env["GH_AW_ADMISSION_MAX_AI_CREDITS"] = "${{ needs.admission.outputs.max-ai-credits }}"
	}
	steps := c.generateCheckoutActionsFolder(data)
	setupRef := c.resolveActionReference("./actions/setup", data)
	if setupRef == "" {
		return nil, errors.New("decision setup action reference could not be resolved")
	}
	steps = append(steps, c.generateSetupStepForJob("decision", &WorkflowData{Name: data.Name}, setupRef, SetupActionDestination, false, "", "", "")...)
	steps = c.appendDecisionStep(data, steps, "${{ needs.pre_activation.outputs.decision_eligible == 'true' }}")
	permissions := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead})
	if hasCopilotRequestsWritePermission(data) {
		permissions.Set(PermissionCopilotRequests, PermissionWrite)
	}
	return &Job{
		Name: "decision", If: activation.If,
		RunsOn:         c.formatFrameworkJobRunsOn(data),
		Permissions:    permissions.RenderToYAML(),
		Environment:    c.indentYAMLLines(resolveSafeOutputsEnvironment(data), "    "),
		TimeoutMinutes: 2, Env: env, Steps: steps,
		Needs: slices.Clone(activation.Needs),
		Outputs: map[string]string{
			"decision_result":      "${{ steps.decision_gate.outputs.decision_result }}",
			"decision_status":      "${{ steps.decision_gate.outputs.decision_status }}",
			"remaining-ai-credits": "${{ steps.decision_gate.outputs.remaining-ai-credits }}",
		},
	}, nil
}

func (c *Compiler) connectActivationDecision(activation *Job) {
	activation.Needs = append(activation.Needs, "decision")
	condition := "needs.decision.result == 'success' && needs.decision.outputs.decision_result != 'skip' && needs.decision.outputs.remaining-ai-credits != '' && needs.decision.outputs.remaining-ai-credits != '0'"
	activation.If = c.combineJobIfConditions(condition, activation.If)
	activation.Outputs["decision-remaining-ai-credits"] = "${{ needs.decision.outputs.remaining-ai-credits }}"
}

func decisionQuestionInputs(data *WorkflowData) ([]string, map[string]string) {
	outputs := map[string]string{}
	questions := slices.Clone(data.Decision.Questions)
	for i, question := range questions {
		expressions, _ := actionslanguageservice.ExtractExpressions(question)
		for _, expression := range slices.Backward(expressions) {
			rewritten := actionslanguageservice.RewriteReferences(expression.Content, func(ref []string) (string, bool) {
				if len(ref) < 3 || !strings.EqualFold(decisionReferencePart(ref, 0), "steps") || slices.Contains(ref, "*") {
					return "", false
				}
				var value strings.Builder
				value.WriteString("steps")
				for _, part := range ref[1:] {
					value.WriteString("['")
					value.WriteString(strings.ReplaceAll(part, "'", "''"))
					value.WriteString("']")
				}
				name := fmt.Sprintf("decision-input-%d", len(outputs))
				outputs[name] = "${{ " + value.String() + " }}"
				return "needs.pre_activation.outputs." + name, true
			})
			question = question[:expression.Start] + "${{" + rewritten + "}}" + question[expression.End:]
		}
		questions[i] = question
	}
	return questions, outputs
}
