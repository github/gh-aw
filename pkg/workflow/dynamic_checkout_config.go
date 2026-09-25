package workflow

import (
	"errors"
	"fmt"
	"sort"
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
	if legacyDynamic, hasLegacyDynamic := raw["dynamic"]; hasLegacyDynamic && legacyDynamicLooksLikeCheckout(legacyDynamic, raw) {
		return DynamicCheckoutConfig{}, true, errors.New("checkout.dynamic is no longer supported; rename it to checkout.repos")
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
	var unsupportedFields []string
	for key := range raw {
		if key != "repos" && key != "allowed-repos" {
			unsupportedFields = append(unsupportedFields, key)
		}
	}
	if len(unsupportedFields) > 0 {
		sort.Strings(unsupportedFields)
		quotedFields := make([]string, 0, len(unsupportedFields))
		for _, field := range unsupportedFields {
			quotedFields = append(quotedFields, fmt.Sprintf("%q", field))
		}
		if len(quotedFields) == 1 {
			return DynamicCheckoutConfig{}, true, fmt.Errorf("dynamic checkout field %s is not supported; only repos and allowed-repos are allowed", quotedFields[0])
		}
		return DynamicCheckoutConfig{}, true, fmt.Errorf("dynamic checkout fields %s are not supported; only repos and allowed-repos are allowed", strings.Join(quotedFields, ", "))
	}
	allowedRepos, err := parseStringArrayOrExpression(allowed)
	if err != nil || len(allowedRepos) == 0 {
		return DynamicCheckoutConfig{}, true, errors.New("dynamic checkout allowed-repos must be a non-empty array or GitHub Actions expression")
	}

	return DynamicCheckoutConfig{Expression: strings.TrimSpace(expression), AllowedRepos: allowedRepos}, true, nil
}

func legacyDynamicLooksLikeCheckout(value any, raw map[string]any) bool {
	if _, hasAllowedRepos := raw["allowed-repos"]; hasAllowedRepos {
		return true
	}
	expression, ok := value.(string)
	return ok && isExpression(expression)
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
