package workflow

import (
	"errors"
	"regexp"
)

var dynamicCheckoutStepsContextPattern = regexp.MustCompile(`\bsteps\s*\.`)

// validateDynamicCheckoutContexts rejects agent-job step references because dynamic
// checkouts are also evaluated in the safe_outputs job.
func (c *Compiler) validateDynamicCheckoutContexts(workflowData *WorkflowData) error {
	for _, expression := range dynamicCheckoutExpressions(workflowData.DynamicCheckouts) {
		if dynamicCheckoutStepsContextPattern.MatchString(maskQuotedExpressionLiterals(expression)) {
			return errors.New("dynamic checkout expressions cannot reference steps.* because they are also evaluated in the safe_outputs job; use a workflow input, vars.*, github.*, or top-level env.* instead")
		}
	}
	return nil
}
