package workflow

import (
	"fmt"
	"slices"
	"strings"
)

// expandSubagentModelPatterns shares the declared experiment alternatives between
// audit metadata and request admission. These are possible models, not proof that
// the selected variant was honored.
func expandSubagentModelPatterns(request string, data *WorkflowData, provider string) []string {
	model, _, _ := strings.Cut(request, "?")
	if !isExpression(model) {
		return expandModelPatterns(request, data.ModelMappings, provider)
	}
	value := stripExpressionWrapper(model)
	var patterns []string
	for _, prefix := range []string{"experiments.", activationOutputsPrefix, pickExperimentOutputsPrefix} {
		if name, ok := strings.CutPrefix(value, prefix); ok {
			for _, candidate := range data.Experiments[name] {
				for _, pattern := range expandModelPatterns(candidate, data.ModelMappings, provider) {
					if !slices.Contains(patterns, pattern) {
						patterns = append(patterns, pattern)
					}
				}
			}
		}
	}
	return patterns
}

func validateExperimentalSubagentModels(data *WorkflowData) error {
	for _, agent := range data.SubAgentModels {
		model, _, _ := strings.Cut(agent.Model, "?")
		for _, match := range experimentsFieldReferenceRegex.FindAllStringSubmatch(agent.Model, -1) {
			name, ok := regexSubmatchAt(match, 1)
			if !ok || RewriteExperimentsReferenceForDownstreamJobs(agent.Model, map[string][]string{name: nil}) == agent.Model {
				continue
			}
			if stripExpressionWrapper(model) != "experiments."+name {
				return fmt.Errorf("sub-agent %q model uses a compound experiment expression; use ${{ experiments.%s }} and put the complete model or alias in each experiment variant", agent.Name, name)
			}
			if _, declared := data.Experiments[name]; !declared {
				return fmt.Errorf("sub-agent %q model references undeclared experiment %q; declare it in experiments", agent.Name, name)
			}
		}
	}
	return nil
}
