package workflow

import (
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

func hasExpressionMarker(source string) bool { return strings.Contains(source, "${{") }

func containsExpression(source string) bool {
	for _, match := range ExpressionPattern.FindAllStringSubmatch(source, -1) {
		if match[1] != "" {
			return true
		}
	}
	return false
}

func isExpression(source string) bool {
	spans, err := actionslanguageservice.ExtractExpressions(source)
	return err == nil && len(spans) == 1 && spans[0].Start == 0 && spans[0].End == len(source)
}

var (
	ExpressionPattern       = actionslanguageservice.Pattern{}
	ExpressionPatternDotAll = ExpressionPattern
	InlineExpressionPattern = ExpressionPattern

	NeedsStepsPattern = actionslanguageservice.ReferencePattern(true, true, func(path []string) []string {
		if len(path) >= 2 && (path[0] == "needs" || path[0] == "steps") && namedAccessPath(path) {
			return []string{}
		}
		return nil
	})
	InputsPattern                   = contextAccessPattern("github.event.inputs", 1, 1, false)
	WorkflowCallInputsPattern       = contextAccessPattern("inputs", 1, 1, false)
	AWInputsPattern                 = contextAccessPattern("github.aw.inputs", 1, 1, false)
	AWImportInputsPattern           = contextAccessPattern("github.aw.import-inputs", 1, 2, false)
	EnvPattern                      = contextAccessPattern("env", 1, 1, false)
	AWInputsExpressionPattern       = contextAccessPattern("github.aw.inputs", 1, 1, true)
	AWImportInputsExpressionPattern = contextAccessPattern("github.aw.import-inputs", 1, 2, true)

	SecretExpressionPattern = actionslanguageservice.Pattern{Filter: func(_ string, expression *actionslanguageservice.Expression) []string {
		if expression.Kind == "logical" && expression.Token.Kind == "||" {
			expression = expression.Child(0)
		}
		path, ok := actionslanguageservice.Reference(expression)
		if !ok || len(path) != 2 || path[0] != "secrets" || !secretIdentifier(path[1]) || path[1] != strings.ToUpper(path[1]) {
			return nil
		}
		return []string{path[1]}
	}}
	SecretsExpressionPattern = actionslanguageservice.Pattern{Whole: true, Filter: func(_ string, expression *actionslanguageservice.Expression) []string {
		if secretsOnlyExpression(expression) {
			return []string{}
		}
		return nil
	}}
	UnsafeContextPattern = actionslanguageservice.Pattern{Filter: func(_ string, expression *actionslanguageservice.Expression) []string {
		for _, path := range actionslanguageservice.References(expression) {
			if (len(path) >= 2 && path[0] == "github" && path[1] == "event") ||
				(len(path) >= 4 && path[0] == "steps" && path[2] == "outputs") ||
				(len(path) >= 1 && path[0] == "inputs") {
				return []string{}
			}
		}
		return nil
	}}
	ComparisonExtractionPattern = actionslanguageservice.Pattern{Unwrapped: true, Filter: func(source string, expression *actionslanguageservice.Expression) []string {
		var captures []string
		actionslanguageservice.Walk(expression, func(node *actionslanguageservice.Expression) bool {
			if captures != nil {
				return false
			}
			if node.Kind == "binary" {
				if _, ok := actionslanguageservice.Reference(node.Child(0)); ok {
					left := node.Child(0)
					captures = []string{source[left.Start:left.End]}
				}
			}
			return captures == nil
		})
		return captures
	}}
	OrPattern = actionslanguageservice.Pattern{Unwrapped: true, Filter: func(source string, expression *actionslanguageservice.Expression) []string {
		if expression.Kind != "logical" || expression.Token.Kind != "||" {
			return nil
		}
		left, right := expression.Child(0), expression.Child(1)
		return []string{source[left.Start:left.End], source[right.Start:expression.End]}
	}}
	StringLiteralPattern = literalPattern("string")
	NumberLiteralPattern = literalPattern("number")

	// These patterns parse gh-aw's own conditional tags and numeric range syntax,
	// not Actions expressions. Nested interpolation handling lives in the scanner.
	TemplateIfPattern     = regexp.MustCompile(`\{\{#if\s+((?:\$\{\{[^\}]*\}\}|[^\}\{]|\{[^\{])*)\s*\}\}`)
	TemplateElseIfPattern = regexp.MustCompile(`\{\{#?else[-_]?if\s+((?:\$\{\{[^\}]*\}\}|[^\}\{]|\{[^\{])*)\s*\}\}`)
	RangePattern          = regexp.MustCompile(`^\d+-\d+$`)
)

func contextAccessPattern(prefix string, min, max int, wrapped bool) actionslanguageservice.Pattern {
	expected := strings.Split(prefix, ".")
	return actionslanguageservice.ReferencePattern(!wrapped, !wrapped, func(path []string) []string {
		if len(path) < len(expected)+min || len(path) > len(expected)+max || !namedAccessPath(path) {
			return nil
		}
		for i, part := range expected {
			if path[i] != part {
				return nil
			}
		}
		return []string{strings.Join(path[len(expected):], ".")}
	})
}

func namedAccessPath(path []string) bool {
	for _, part := range path {
		if part == "" {
			return false
		}
		for _, ch := range part {
			valid := ch == '_' || ch == '-' || ch >= 'a' && ch <= 'z' ||
				ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
			if !valid {
				return false
			}
		}
	}
	return true
}

func secretsOnlyExpression(expression *actionslanguageservice.Expression) bool {
	if expression.Kind == "group" {
		return secretsOnlyExpression(expression.Child(0))
	}
	if expression.Kind == "logical" && expression.Token.Kind == "||" {
		for _, child := range expression.Children {
			if !secretsOnlyExpression(child) {
				return false
			}
		}
		return true
	}
	path, ok := actionslanguageservice.Reference(expression)
	return ok && len(path) == 2 && path[0] == "secrets" && secretIdentifier(path[1])
}

func secretIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, ch := range name {
		valid := ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' ||
			i > 0 && ch >= '0' && ch <= '9'
		if !valid {
			return false
		}
	}
	return true
}

func literalPattern(kind string) actionslanguageservice.Pattern {
	return actionslanguageservice.Pattern{Whole: true, Unwrapped: true, Filter: func(_ string, expression *actionslanguageservice.Expression) []string {
		if expression.Kind == "literal" && expression.Token.Kind == kind {
			return []string{}
		}
		return nil
	}}
}
