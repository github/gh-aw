package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var unavailableTopLevelEnvContextPattern = regexp.MustCompile(
	`(?i)(?:^|[^A-Za-z0-9_.-])(env|job|jobs|matrix|needs|runner|steps|strategy)(?:[^A-Za-z0-9_-]|$)`,
)

// validateTopLevelEnvExpressions rejects expression contexts that GitHub Actions
// only makes available within jobs or reusable workflow outputs.
func validateTopLevelEnvExpressions(env map[string]any) error {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		value := env[name]
		stringValue, ok := value.(string)
		if !ok {
			continue
		}

		unavailableContexts := findUnavailableContextsInExpression(stringValue)
		if len(unavailableContexts) == 0 {
			continue
		}
		return buildTopLevelEnvError(name, stringValue, unavailableContexts)
	}
	return nil
}

func findUnavailableContextsInExpression(stringValue string) []string {
	var unavailableContexts []string
	for _, rawExpr := range extractExpressionsQuoteAware(stringValue) {
		expression := maskQuotedExpressionLiterals(rawExpr)
		for _, contextMatch := range unavailableTopLevelEnvContextPattern.FindAllStringSubmatch(expression, -1) {
			if len(contextMatch) >= 2 {
				ctxName := strings.ToLower(contextMatch[1])
				if !slices.Contains(unavailableContexts, ctxName) {
					unavailableContexts = append(unavailableContexts, ctxName)
				}
			}
		}
	}
	slices.Sort(unavailableContexts)
	return unavailableContexts
}

func buildTopLevelEnvError(name, stringValue string, unavailableContexts []string) error {
	hasJobs := slices.Contains(unavailableContexts, "jobs")
	hasNonJobs := false
	for _, ctx := range unavailableContexts {
		if ctx != "jobs" {
			hasNonJobs = true
			break
		}
	}

	var reason, remediation string
	switch {
	case hasJobs && !hasNonJobs:
		reason = "top-level env expression references the 'jobs' context, which is only available in reusable workflow outputs"
		remediation = "The 'jobs' context is not available in environment variables. Access job outputs using 'needs.<job_id>.outputs' in dependent jobs, or define reusable workflow outputs under 'on.workflow_call.outputs'."
	case hasJobs && hasNonJobs:
		reason = "top-level env expression references unavailable context(s): " + strings.Join(unavailableContexts, ", ")
		remediation = "The 'jobs' context is only available in reusable workflow outputs (use 'needs' in dependent jobs). Move variables referencing other contexts to a job or step env block."
	default:
		reason = "top-level env expression references context(s) unavailable outside jobs: " + strings.Join(unavailableContexts, ", ")
		remediation = fmt.Sprintf("Move this environment variable to a job or step env block. Example:\njobs:\n  agent:\n    env:\n      %s: %s", name, stringValue)
	}

	return NewValidationError(
		"env."+name,
		stringValue,
		reason,
		remediation,
	)
}

func extractExpressionsQuoteAware(input string) []string {
	var expressions []string
	idx := 0
	for idx < len(input) {
		start := strings.Index(input[idx:], "${{")
		if start == -1 {
			break
		}
		exprStart := idx + start + 2
		i := exprStart
		var inQuote byte
		foundEnd := false
		for i < len(input) {
			ch := input[i]
			if inQuote != 0 {
				if ch == inQuote {
					if i+1 < len(input) && input[i+1] == inQuote {
						i += 2
						continue
					}
					inQuote = 0
				} else if ch == '\\' && inQuote != '\'' {
					i += 2
					continue
				}
				i++
				continue
			}

			if ch == '\'' || ch == '"' || ch == '`' {
				inQuote = ch
				i++
				continue
			}

			if ch == '}' && i+1 < len(input) && input[i+1] == '}' {
				expressions = append(expressions, input[exprStart:i])
				idx = i + 2
				foundEnd = true
				break
			}
			i++
		}
		if !foundEnd {
			break
		}
	}
	return expressions
}
