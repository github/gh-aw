package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// interpolationReferences ignores quoted data and plain text outside Actions expressions.
func interpolationReferences(source string) [][]string {
	var paths [][]string
	spans, _ := actionslanguageservice.ExtractExpressions(source)
	for _, span := range spans {
		expression, err := actionslanguageservice.ParseSyntax(span.Content)
		if err == nil {
			paths = append(paths, actionslanguageservice.References(expression)...)
		}
	}
	return paths
}

func referenceHasPrefix(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i, part := range prefix {
		if !strings.EqualFold(path[i], part) {
			return false
		}
	}
	return true
}

func expressionReferencePattern(whole bool, prefix []string, length, capture int) actionslanguageservice.Pattern {
	return actionslanguageservice.ReferencePattern(whole, false, func(path []string) []string {
		if len(path) != length || !referenceHasPrefix(path, prefix) {
			return nil
		}
		if capture >= 0 {
			return []string{path[capture]}
		}
		return []string{}
	})
}

func expressionReferenceAccessPattern(prefix []string) actionslanguageservice.Pattern {
	return actionslanguageservice.Pattern{Filter: func(_ string, expression *actionslanguageservice.Expression) []string {
		for _, path := range actionslanguageservice.References(expression) {
			if referenceHasPrefix(path, prefix) {
				return []string{}
			}
		}
		return nil
	}}
}

// Credential policies intentionally permit only named references and, for secrets,
// a disjunction of named secret references, not arbitrary Actions expressions.
func secretFallbackExpression(expression *actionslanguageservice.Expression) bool {
	if expression.Kind == "logical" && expression.Token.Text == "||" {
		for _, child := range expression.Children {
			if !secretFallbackExpression(child) {
				return false
			}
		}
		return true
	}
	path, ok := actionslanguageservice.Reference(expression)
	return ok && len(path) == 2 && path[0] == "secrets" && credentialIdentifier(path[1], false)
}

func credentialIdentifier(value string, allowHyphen bool) bool {
	return actionslanguageservice.ActionsIdentifier(value, allowHyphen)
}

func credentialExpressionPattern(allowNeeds, allowSteps, allowEnv bool) actionslanguageservice.Pattern {
	return actionslanguageservice.Pattern{Whole: true, Filter: func(_ string, expression *actionslanguageservice.Expression) []string {
		if secretFallbackExpression(expression) {
			return []string{}
		}
		path, ok := actionslanguageservice.Reference(expression)
		if !ok {
			return nil
		}
		if allowEnv && len(path) == 2 && path[0] == "env" && credentialIdentifier(path[1], false) {
			return []string{}
		}
		if len(path) == 4 && path[2] == "outputs" &&
			(allowNeeds && path[0] == "needs" || allowSteps && path[0] == "steps") &&
			credentialIdentifier(path[1], allowSteps && path[0] == "steps") &&
			credentialIdentifier(path[3], allowSteps && path[0] == "steps") {
			return []string{}
		}
		return nil
	}}
}
