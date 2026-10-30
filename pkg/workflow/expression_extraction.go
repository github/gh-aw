package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/importinpututil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

var expressionExtractionLog = logger.New("workflow:expression_extraction")

type ExpressionMapping struct {
	Original  string
	EnvVar    string
	Content   string
	IsLiteral bool
}

func (m *ExpressionMapping) environmentValue() string {
	if m.IsLiteral {
		return fmt.Sprintf("%q", m.Content)
	}
	return fmt.Sprintf("${{ %s }}", m.Content)
}

type ExpressionExtractor struct {
	mappings map[string]*ExpressionMapping
}

func NewExpressionExtractor() *ExpressionExtractor {
	return &ExpressionExtractor{mappings: make(map[string]*ExpressionMapping)}
}

type contentTransformer func(string) string

var defaultContentTransformers = []contentTransformer{
	transformActivationOutputs,
	transformExperimentsExpression,
	transformAwContextExpression,
	transformGatewayEventsExpression,
}

func applyContentTransformers(content string, transformers []contentTransformer) string {
	for _, transform := range transformers {
		if transformed := transform(content); transformed != content {
			expressionExtractionLog.Printf("Transformed expression: %s -> %s", content, transformed)
			content = transformed
		}
	}
	return content
}

func (e *ExpressionExtractor) addSubExpressionMappings(content string) {
	if simpleIdentifierRegex.MatchString(content) {
		return
	}
	for _, subExpression := range extractTerminalSubExpressions(content) {
		original := "${{ " + subExpression + " }}"
		if _, exists := e.mappings[original]; !exists {
			e.mappings[original] = &ExpressionMapping{
				Original: original, EnvVar: e.generateEnvVarName(subExpression), Content: subExpression,
			}
		}
	}
}

func (e *ExpressionExtractor) processMatch(original, rawContent string) error {
	originalContent := strings.TrimSpace(rawContent)
	content := applyContentTransformers(originalContent, defaultContentTransformers)
	tree, err := actionslanguageservice.ParseSyntax(content)
	if err != nil {
		return fmt.Errorf("invalid expression %s: %w", original, err)
	}
	if tree == nil {
		return fmt.Errorf("empty expression %s", original)
	}
	if _, exists := e.mappings[original]; exists {
		return nil
	}
	if content != originalContent && strings.HasPrefix(content, "steps.sanitized.outputs.") {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("Deprecated expression ${{ %s }}: use ${{ %s }} instead.", originalContent, content)))
	}
	envVar := e.generateEnvVarName(content)
	if name, ok := regexSubmatchAt(experimentNameRegex.FindStringSubmatch(originalContent), 1); ok {
		envVar = ExperimentEnvVarName(name)
	}
	e.mappings[original] = &ExpressionMapping{Original: original, EnvVar: envVar, Content: content}
	e.addSubExpressionMappings(content)
	return nil
}

func (e *ExpressionExtractor) ExtractExpressions(markdown string) ([]*ExpressionMapping, error) {
	spans, err := actionslanguageservice.ExtractExpressions(markdown)
	if err != nil {
		return nil, err
	}
	for _, span := range spans {
		if err := e.processMatch(markdown[span.Start:span.End], span.Content); err != nil {
			return nil, err
		}
	}
	var result []*ExpressionMapping
	for _, mapping := range e.mappings {
		result = append(result, mapping)
	}
	slices.SortFunc(result, func(a, b *ExpressionMapping) int {
		return strings.Compare(a.Original, b.Original)
	})
	return result, nil
}

func transformActivationOutputs(source string) string {
	return actionslanguageservice.RewriteReferences(source, func(path []string) (string, bool) {
		if len(path) == 4 && path[0] == "needs" && path[1] == "activation" && path[2] == "outputs" &&
			slices.Contains([]string{"text", "title", "body"}, path[3]) {
			return "steps.sanitized.outputs." + path[3], true
		}
		return "", false
	})
}

var experimentNameRegex = contextAccessPattern("experiments", 1, 1, false)

func ExperimentEnvVarName(experimentName string) string {
	return "GH_AW_EXPERIMENTS_" + normalizeJobNameForEnvVar(experimentName)
}

// Experiments have a gh-aw-specific comparison shorthand with JSON strings and
// JavaScript equality spellings. Normalize that extension before Actions parsing.
func normalizeExperimentComparison(source string) string {
	if !strings.HasPrefix(source, "experiments.") {
		return source
	}
	var result strings.Builder
	inString := false
	for i := 0; i < len(source); {
		if source[i] == '\'' {
			inString = !inString
		}
		if !inString && source[i] == '"' {
			quoted, err := strconv.QuotedPrefix(source[i:])
			if err != nil {
				return source
			}
			value, err := strconv.Unquote(quoted)
			if err != nil {
				return source
			}
			result.WriteString("'" + strings.ReplaceAll(value, "'", "''") + "'")
			i += len(quoted)
			continue
		}
		if !inString && (strings.HasPrefix(source[i:], "===") || strings.HasPrefix(source[i:], "!==")) {
			result.WriteString(source[i : i+2])
			i += 3
			continue
		}
		result.WriteByte(source[i])
		i++
	}
	return result.String()
}

func transformExperimentsExpression(source string) string {
	normalized := normalizeExperimentComparison(source)
	expression, err := actionslanguageservice.ParseSyntax(normalized)
	if err != nil || expression == nil {
		return source
	}
	operand := expression
	if expression.Kind == "binary" && (expression.Token.Kind == "==" || expression.Token.Kind == "!=") &&
		expression.Child(1).Kind == "literal" && expression.Child(1).Token.Kind == "string" {
		operand = expression.Child(0)
	}
	path, ok := actionslanguageservice.Reference(operand)
	if !ok || len(path) != 2 || path[0] != "experiments" || !namedAccessPath(path) {
		return source
	}
	return actionslanguageservice.RewriteReferences(normalized, func(path []string) (string, bool) {
		if len(path) == 2 && path[0] == "experiments" {
			return "steps.pick-experiment.outputs." + path[1], true
		}
		return "", false
	})
}

func regexSubmatchAt(matches []string, index int) (string, bool) {
	if index < 0 || index >= len(matches) {
		return "", false
	}
	return matches[index], true
}

func transformAwContextExpression(source string) string {
	return actionslanguageservice.RewriteReferences(source, func(path []string) (string, bool) {
		if slices.Equal(path, []string{"github", "aw", "context"}) {
			return "fromJSON(steps.generate_aw_info.outputs.aw_context || '{}')", true
		}
		return "", false
	})
}

func transformGatewayEventsExpression(source string) string {
	return actionslanguageservice.RewriteReferences(source, func(path []string) (string, bool) {
		if slices.Equal(path, []string{"gateway", "events"}) {
			return "fromJSON(steps.generate_aw_info.outputs.aw_context || '{}').webhook.deliveries", true
		}
		return "", false
	})
}

// The runtime's deterministic env-var convention intentionally accepts only
// simple dotted identifiers, not every property access in the Actions grammar.
var simpleIdentifierRegex = actionslanguageservice.Pattern{Whole: true, Unwrapped: true, Filter: func(source string, expression *actionslanguageservice.Expression) []string {
	path, ok := actionslanguageservice.Reference(expression)
	if !ok || source != strings.Join(path, ".") {
		return nil
	}
	for _, part := range path {
		if !secretIdentifier(part) {
			return nil
		}
	}
	return []string{}
}}

var runtimeEvalEnvVarPrefixRegex = actionslanguageservice.ReferencePattern(true, true, func(path []string) []string {
	if len(path) > 1 && slices.Contains([]string{"needs", "steps", "inputs"}, path[0]) {
		return []string{}
	}
	return nil
})

func isQualifyingSubExpression(source string) bool {
	return simpleIdentifierRegex.MatchString(source) && runtimeEvalEnvVarPrefixRegex.MatchString(source)
}

func extractTerminalSubExpressions(source string) []string {
	tree, err := actionslanguageservice.ParseSyntax(source)
	if err != nil {
		expressionExtractionLog.Printf("Cannot extract expression references: %v", err)
		return nil
	}
	seen := make(map[string]struct{})
	var result []string
	actionslanguageservice.Walk(tree, func(node *actionslanguageservice.Expression) bool {
		switch node.Kind {
		case "logical", "group", "unary":
			return true
		case "context", "index":
		default:
			// The runtime env-var evaluator recurses through logical
			// operands, not through arbitrary calls or comparisons.
			return false
		}
		if _, ok := actionslanguageservice.Reference(node); !ok {
			return false
		}
		content := source[node.Start:node.End]
		if _, exists := seen[content]; isQualifyingSubExpression(content) && !exists {
			result = append(result, content)
			seen[content] = struct{}{}
		}
		return false
	})
	return result
}

func (e *ExpressionExtractor) generateEnvVarName(content string) string {
	if simpleIdentifierRegex.MatchString(content) {
		return "GH_AW_" + strings.ToUpper(strings.ReplaceAll(content, ".", "_"))
	}
	hash := sha256.Sum256([]byte(content))
	return "GH_AW_EXPR_" + strings.ToUpper(hex.EncodeToString(hash[:])[:8])
}

func (e *ExpressionExtractor) ReplaceExpressionsWithEnvVars(markdown string) string {
	return ExpressionPattern.ReplaceAllStringFunc(markdown, func(original string) string {
		if mapping, ok := e.mappings[original]; ok {
			return "__" + mapping.EnvVar + "__"
		}
		return original
	})
}

func applyWorkflowDispatchFallbacks(mappings []*ExpressionMapping, hasItemNumber bool) {
	if !hasItemNumber {
		return
	}
	fallbacks := map[string]string{
		"github.event.pull_request.number": "github.event.pull_request.number || inputs.item_number",
		"github.event.issue.number":        "github.event.issue.number || inputs.item_number",
		"github.event.discussion.number":   "github.event.discussion.number || inputs.item_number",
	}
	for _, mapping := range mappings {
		if enhanced, ok := fallbacks[mapping.Content]; ok {
			mapping.Content = enhanced
		}
	}
}

func SubstituteImportInputs(content string, importInputs map[string]any) string {
	if len(importInputs) == 0 {
		return content
	}
	substitute := func(pattern actionslanguageservice.Pattern) func(string) string {
		return func(match string) string {
			path, ok := regexSubmatchAt(pattern.FindStringSubmatch(match), 1)
			if ok {
				if value, found := resolveImportInputByPath(importInputs, path); found {
					return marshalImportInputValue(value)
				}
			}
			return match
		}
	}
	result := AWInputsExpressionPattern.ReplaceAllStringFunc(content, substitute(AWInputsExpressionPattern))
	return AWImportInputsExpressionPattern.ReplaceAllStringFunc(result, substitute(AWImportInputsExpressionPattern))
}

func marshalImportInputValue(value any) string {
	if formatted, ok := importinpututil.FormatResolvedValue(value); ok {
		return formatted
	}
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func resolveImportInputByPath(importInputs map[string]any, path string) (any, bool) {
	return importinpututil.ResolvePathValue(importInputs, path)
}
