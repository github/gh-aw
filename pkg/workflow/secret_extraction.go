package workflow

import (
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

var secretLog = logger.New("workflow:secret_extraction")

type SecretExpression struct {
	VarName  string
	FullExpr string
}

func visitValueExpressions(value string, visit func(string, *actionslanguageservice.Expression)) {
	spans, err := actionslanguageservice.ExtractExpressions(value)
	if err != nil {
		secretLog.Printf("Cannot scan complete expression value: %v", err)
	}
	for _, span := range spans {
		expression, err := actionslanguageservice.ParseSyntax(span.Content)
		if err != nil || expression == nil {
			secretLog.Printf("Cannot inspect expression %q: %v", span.Content, err)
			continue
		}
		visit(value[span.Start:span.End], expression)
	}
}

func ExtractSecretName(value string) string {
	var name string
	visitValueExpressions(value, func(_ string, expression *actionslanguageservice.Expression) {
		if path, ok := leadingValueReference(expression); ok && len(path) == 2 && path[0] == "secrets" && secretIdentifier(path[1]) && name == "" {
			name = path[1]
		}
	})
	return name
}

// ExtractSecretsFromValue finds actual named references, including those in
// function arguments and fallbacks, but not reference-like string literals.
func ExtractSecretsFromValue(value string) map[string]string {
	secrets := make(map[string]string)
	visitValueExpressions(value, func(full string, expression *actionslanguageservice.Expression) {
		for _, path := range actionslanguageservice.References(expression) {
			if len(path) >= 2 && strings.EqualFold(path[0], "secrets") && secretIdentifier(path[1]) {
				secrets[path[1]] = full
			}
		}
	})
	return secrets
}

func ExtractSecretsFromMap(values map[string]string) map[string]string {
	result := make(map[string]string)
	for _, value := range values {
		maps.Copy(result, ExtractSecretsFromValue(value))
	}
	return result
}

// Job output collections and individual outputs both carry upstream values that
// must be excluded from the agent container, just like secrets.
func ContainsJobOutputExpr(value string) bool {
	found := false
	visitValueExpressions(value, func(_ string, expression *actionslanguageservice.Expression) {
		actionslanguageservice.Walk(expression, func(node *actionslanguageservice.Expression) bool {
			if node.Kind != "index" || node.Child(1).Kind != "literal" {
				return true
			}
			property, ok := node.Child(1).Token.Value.(string)
			if !ok || !strings.EqualFold(property, "outputs") {
				return true
			}
			base := node.Child(0)
			for base.Kind == "group" {
				base = base.Child(0)
			}
			if base.Kind != "index" {
				return true
			}
			for base.Kind == "index" || base.Kind == "group" {
				base = base.Child(0)
			}
			if base.Kind == "context" && strings.EqualFold(base.Token.Text, "needs") {
				found = true
			}
			return !found
		})
	})
	return found
}

func leadingValueReference(expression *actionslanguageservice.Expression) ([]string, bool) {
	for expression.Kind == "logical" || expression.Kind == "binary" || expression.Kind == "group" {
		expression = expression.Child(0)
	}
	return actionslanguageservice.Reference(expression)
}

func ExtractEnvExpressionsFromValue(value string) map[string]string {
	result := make(map[string]string)
	visitValueExpressions(value, func(full string, expression *actionslanguageservice.Expression) {
		path, ok := leadingValueReference(expression)
		if ok && len(path) == 2 && path[0] == "env" && secretIdentifier(path[1]) {
			existing, exists := result[path[1]]
			if !exists || (!strings.Contains(existing, "||") && strings.Contains(full, "||")) {
				result[path[1]] = full
			}
		}
	})
	return result
}

func ExtractEnvExpressionsFromMap(values map[string]string) map[string]string {
	result := make(map[string]string)
	for _, value := range values {
		for name, expression := range ExtractEnvExpressionsFromValue(value) {
			existing, exists := result[name]
			if !exists || (!strings.Contains(existing, "||") && strings.Contains(expression, "||")) {
				result[name] = expression
			}
		}
	}
	return result
}

func ReplaceSecretsWithShellEnvVars(value string, secrets map[string]string) string {
	return replaceSecretsWithPrefixedEnvVars(value, secrets, "${")
}

func replaceSecretsWithPrefixedEnvVars(value string, secrets map[string]string, prefix string) string {
	// Shared fallback expressions can name multiple secrets. Sort so the selected
	// environment variable is deterministic.
	for _, name := range sliceutil.SortedKeys(secrets) {
		value = strings.ReplaceAll(value, secrets[name], prefix+name+"}")
	}
	return value
}

func replaceEnvExpressionsWithPrefixedEnvVars(value string, prefix string) string {
	return ExpressionPattern.ReplaceAllStringFunc(value, func(full string) string {
		expression, ok := actionslanguageservice.WrappedExpression(full)
		if !ok {
			return full
		}
		path, ok := leadingValueReference(expression)
		if ok && len(path) == 2 && path[0] == "env" && secretIdentifier(path[1]) {
			return prefix + path[1] + "}"
		}
		return full
	})
}

var gitHubContextExprPattern = contextAccessPattern("github", 1, 10, true)
var workflowInputExpressionPattern = contextAccessPattern("inputs", 1, 1, true)

var gitHubContextEnvVarMap = map[string]string{
	"workflow": "GITHUB_WORKFLOW", "run_id": "GITHUB_RUN_ID",
	"run_number": "GITHUB_RUN_NUMBER", "run_attempt": "GITHUB_RUN_ATTEMPT",
	"actor": "GITHUB_ACTOR", "repository": "GITHUB_REPOSITORY",
	"event_name": "GITHUB_EVENT_NAME", "sha": "GITHUB_SHA",
	"ref": "GITHUB_REF", "ref_name": "GITHUB_REF_NAME", "ref_type": "GITHUB_REF_TYPE",
	"head_ref": "GITHUB_HEAD_REF", "base_ref": "GITHUB_BASE_REF",
	"server_url": "GITHUB_SERVER_URL", "job": "GITHUB_JOB", "action": "GITHUB_ACTION",
	"workspace": "GITHUB_WORKSPACE", "workflow_ref": "GITHUB_WORKFLOW_REF",
	"workflow_sha": "GITHUB_WORKFLOW_SHA", "repository_owner": "GITHUB_REPOSITORY_OWNER",
	"triggering_actor": "GITHUB_TRIGGERING_ACTOR", "token": "GITHUB_TOKEN",
}

func ExtractGitHubContextExpressionsFromValue(value string) map[string]string {
	result := make(map[string]string)
	for _, match := range gitHubContextExprPattern.FindAllStringSubmatch(value, -1) {
		if name, ok := gitHubContextEnvVarMap[match[1]]; ok {
			result[name] = match[0]
		}
	}
	return result
}

func ExtractWorkflowInputExpressionsFromValue(value string) map[string]string {
	result := make(map[string]string)
	for _, match := range workflowInputExpressionPattern.FindAllStringSubmatch(value, -1) {
		result[formatInputNameAsEnvVar(match[1])] = match[0]
	}
	return result
}

func formatInputNameAsEnvVar(inputName string) string {
	normalized := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, inputName)
	return "GH_AW_INPUT_" + strings.ToUpper(normalized)
}

func ReplaceTemplateExpressionsWithEnvVars(value string) string {
	secrets := ExtractSecretsFromValue(value)
	value = replaceSecretsWithPrefixedEnvVars(value, secrets, "\\${")
	value = replaceEnvExpressionsWithPrefixedEnvVars(value, "\\${")
	return ExpressionPattern.ReplaceAllStringFunc(value, func(full string) string {
		expression, ok := actionslanguageservice.WrappedExpression(full)
		if ok {
			if path, ok := actionslanguageservice.Reference(expression); ok && len(path) == 2 && path[0] == "github" && path[1] == "workspace" {
				return "\\${GITHUB_WORKSPACE}"
			}
		}
		return full
	})
}
