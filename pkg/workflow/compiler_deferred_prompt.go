package workflow

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/setutil"
)

var deferredPromptOutputPattern = regexp.MustCompile(`^needs\.([a-zA-Z_][a-zA-Z0-9_-]*)\.(?:outputs\.([a-zA-Z_][a-zA-Z0-9_-]*)|result)$`)

// Keep activation's prompt rendering independent of jobs that run between
// activation and the agent. Only direct result and declared output references are deferred.
func rewriteDeferredPromptExpressions(mappings []*ExpressionMapping, jobs map[string]any, beforeActivation []string, markdown string) ([]*ExpressionMapping, []*ExpressionMapping) {
	rewritten := make([]*ExpressionMapping, 0, len(mappings))
	var deferred []*ExpressionMapping
	seen := make(map[string]struct{})
	for _, mapping := range mappings {
		if !canDeferPromptExpression(mapping, mappings, jobs, beforeActivation, markdown) {
			rewritten = append(rewritten, mapping)
			continue
		}
		sum := sha256.Sum256([]byte(mapping.Content))
		envVar := fmt.Sprintf("GH_AW_DEFERRED_%X", sum[:8])
		replacement := *mapping
		replacement.Content = "__" + envVar + "__"
		replacement.IsLiteral = true
		rewritten = append(rewritten, &replacement)
		if _, exists := seen[envVar]; !exists {
			deferred = append(deferred, &ExpressionMapping{
				Original: mapping.Original,
				EnvVar:   envVar,
				Content:  mapping.Content,
			})
			seen[envVar] = struct{}{}
		}
	}
	slices.SortFunc(deferred, func(a, b *ExpressionMapping) int {
		return strings.Compare(a.EnvVar, b.EnvVar)
	})
	return rewritten, deferred
}

func canDeferPromptExpression(mapping *ExpressionMapping, mappings []*ExpressionMapping, jobs map[string]any, beforeActivation []string, markdown string) bool {
	parts := deferredPromptOutputPattern.FindStringSubmatch(mapping.Content)
	if len(parts) < 3 {
		return false
	}
	producer, output := parts[1], parts[2] //nolint:uncheckedsliceindex // The guard above requires all three regex captures.
	if slices.Contains(beforeActivation, producer) || isBuiltinJobName(producer) {
		return false
	}
	job, ok := jobs[producer].(map[string]any)
	if !ok || setutil.Contains(getJobsDependingOnAgent(jobs), producer) {
		return false
	}
	outputs, hasOutputs := job["outputs"].(map[string]any)
	_, declared := outputs[output]
	return (output == "" || (hasOutputs && declared)) && !deferredPromptControlsRendering(mapping, mappings, markdown)
}

// Inline chunks already contain placeholders. Keep mappings used by fallback
// expressions and conditions so filtering cannot strand those placeholders.
func filterPromptExpressionsForActivation(mappings []*ExpressionMapping, jobs map[string]any, beforeActivation []string, markdown string) []*ExpressionMapping {
	activationMappings := filterExpressionsForActivation(mappings, jobs, beforeActivation)
	for _, mapping := range mappings {
		if !slices.Contains(activationMappings, mapping) &&
			(!deferredPromptOutputPattern.MatchString(mapping.Content) || deferredPromptControlsRendering(mapping, mappings, markdown)) {
			activationMappings = append(activationMappings, mapping)
		}
	}
	return activationMappings
}

func deferredPromptControlsRendering(mapping *ExpressionMapping, mappings []*ExpressionMapping, markdown string) bool {
	for _, other := range mappings {
		if other.Content != mapping.Content && containsPromptReference(other.Content, mapping.Content) {
			return true
		}
	}
	for _, pattern := range []*regexp.Regexp{TemplateIfPattern, TemplateElseIfPattern} {
		for _, conditional := range pattern.FindAllString(markdown, -1) {
			if containsPromptReference(conditional, mapping.Content) || strings.Contains(conditional, "__"+mapping.EnvVar+"__") {
				return true
			}
		}
	}
	return false
}

func containsPromptReference(content, reference string) bool {
	isReferenceChar := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
	}
	for offset := 0; offset < len(content); {
		index := strings.Index(content[offset:], reference)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(reference)
		if (start == 0 || !isReferenceChar(content[start-1])) && (end == len(content) || !isReferenceChar(content[end])) { //nolint:uncheckedsliceindex // strings.Index bounds the match; short-circuit checks guard its adjacent bytes.
			return true
		}
		offset = end
	}
	return false
}
