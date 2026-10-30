package workflow

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

// Activation cannot read its own job outputs through needs. Preserve placeholder
// names while binding prompt environment values to their local producers.
func rewriteActivationPromptOutputs(content string, outputs map[string]string) (string, error) {
	bindings := make(map[string]string, len(outputs))
	for name, value := range outputs {
		if strings.HasPrefix(strings.TrimSpace(value), "${{") {
			bindings[name] = "(" + stripExpressionWrapper(value) + ")"
			continue
		}
		var literal string
		if err := yaml.Unmarshal([]byte(value), &literal); err != nil {
			return "", fmt.Errorf("activation prompt requires a string or expression output binding for %s; correct the compiler output declaration", name)
		}
		bindings[name] = BuildStringLiteral(literal).Render()
	}
	return rewriteExpressionReferences(content, activationOutputReferencePrefix, func(name string) (string, bool) {
		value, declared := bindings[name]
		return value, declared
	}), nil
}
