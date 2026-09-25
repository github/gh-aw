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
	if _, hasLegacyDynamic := raw["dynamic"]; hasLegacyDynamic {
		return DynamicCheckoutConfig{}, true, errors.New("dynamic checkout uses checkout.repos; checkout.dynamic is not supported")
	}
	expression, hasExpression := raw["repos"].(string)
	if !hasExpression {
		if _, hasRepos := raw["repos"]; hasRepos {
			return DynamicCheckoutConfig{}, true, errors.New("dynamic checkout repos must be a string containing a GitHub Actions expression")
		}
		return DynamicCheckoutConfig{}, false, nil
	}
	if !isExpression(expression) {
		return DynamicCheckoutConfig{}, true, errors.New("dynamic checkout repos must be a GitHub Actions expression")
	}
	allowed, ok := raw["allowed-repos"]
	if !ok {
		return DynamicCheckoutConfig{}, true, errors.New("dynamic checkout requires allowed-repos")
	}
	for key := range raw {
		if key != "repos" && key != "allowed-repos" {
			return DynamicCheckoutConfig{}, true, errors.New("dynamic checkout only supports repos and allowed-repos fields")
		}
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
