package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// expandSubagentModelPatterns shares the declared experiment alternatives between
// audit metadata and request admission. These are possible models, not proof that
// the selected variant was honored.
func expandSubagentModelPatterns(request string, data *WorkflowData, provider string) []string {
	return expandSubagentModelPatternsWith(request, data, provider, func(model string) string { return model })
}

// expandSubagentModelPatternsWith expands like expandSubagentModelPatterns after applying
// normalize to the literal request or to each experiment candidate.
func expandSubagentModelPatternsWith(request string, data *WorkflowData, provider string, normalize func(string) string) []string {
	model, _, _ := strings.Cut(request, "?")
	if !isExpression(model) {
		return expandModelPatterns(normalize(request), data.ModelMappings, provider)
	}
	expression, _ := actionslanguageservice.WrappedExpression(model)
	path, _ := actionslanguageservice.Reference(expression)
	var patterns []string
	for _, prefix := range [][]string{experimentReferencePrefix, activationOutputReferencePrefix, {"steps", "pick-experiment", "outputs"}} {
		if len(path) == len(prefix)+1 && referenceHasPrefix(path, prefix) {
			name := path[len(prefix)]
			for _, candidate := range data.Experiments[name] {
				for _, pattern := range expandModelPatterns(normalize(candidate), data.ModelMappings, provider) {
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
		for _, path := range interpolationReferences(agent.Model) {
			if len(path) != 2 || !referenceHasPrefix(path, experimentReferencePrefix) {
				continue
			}
			name := path[1]
			if RewriteExperimentsReferenceForDownstreamJobs(agent.Model, map[string][]string{name: nil}) == agent.Model {
				continue
			}
			expression, _ := actionslanguageservice.WrappedExpression(model)
			modelPath, _ := actionslanguageservice.Reference(expression)
			if len(modelPath) != 2 || !referenceHasPrefix(modelPath, experimentReferencePrefix) || modelPath[1] != name {
				return fmt.Errorf("sub-agent %q model uses a compound experiment expression; use ${{ experiments.%s }} and put the complete model or alias in each experiment variant", agent.Name, name)
			}
			if _, declared := data.Experiments[name]; !declared {
				return fmt.Errorf("sub-agent %q model references undeclared experiment %q; declare it in experiments", agent.Name, name)
			}
		}
	}
	return nil
}
