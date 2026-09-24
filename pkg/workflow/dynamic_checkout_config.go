package workflow

import (
	"errors"
	"strings"
)

type DynamicCheckoutConfig struct {
	Expression   string
	AllowedRepos []string
}

func parseDynamicCheckoutConfig(value any) (DynamicCheckoutConfig, bool, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return DynamicCheckoutConfig{}, false, nil
	}
	expression, hasExpression := raw["dynamic"].(string)
	if !hasExpression || !isExpression(expression) {
		return DynamicCheckoutConfig{}, false, nil
	}
	allowed, ok := raw["allowed-repos"]
	if !ok {
		return DynamicCheckoutConfig{}, true, errors.New("dynamic checkout requires allowed-repos")
	}
	if len(raw) != 2 {
		return DynamicCheckoutConfig{}, true, errors.New("dynamic checkout only supports dynamic and allowed-repos fields")
	}
	allowedRepos, err := parseStringArrayOrExpression(allowed)
	if err != nil || len(allowedRepos) == 0 {
		return DynamicCheckoutConfig{}, true, errors.New("dynamic checkout allowed-repos must be a non-empty array or GitHub Actions expression")
	}
	return DynamicCheckoutConfig{Expression: strings.TrimSpace(expression), AllowedRepos: allowedRepos}, true, nil
}

func parseStringArrayOrExpression(value any) ([]string, error) {
	if expression, ok := value.(string); ok && isExpression(expression) {
		return []string{strings.TrimSpace(expression)}, nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil, errors.New("expected array")
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		repository, ok := item.(string)
		if !ok || !strings.Contains(repository, "/") {
			return nil, errors.New("expected repository names")
		}
		result = append(result, repository)
	}
	return result, nil
}

func dynamicCheckoutExpressions(checkouts []DynamicCheckoutConfig) []string {
	expressions := make([]string, 0, len(checkouts))
	for _, checkout := range checkouts {
		expressions = append(expressions, checkout.Expression)
	}
	return expressions
}
