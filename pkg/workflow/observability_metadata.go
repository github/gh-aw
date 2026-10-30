package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// resolveOTLPMetadataReferences replaces whole-value references in custom OTLP
// attribute maps with strings from the root workflow's metadata.
func resolveOTLPMetadataReferences(workflowData *WorkflowData) error {
	if workflowData == nil {
		return nil
	}
	obs := extractRawObservabilityMap(workflowData.RawFrontmatter)
	if obs == nil {
		return nil
	}
	otlp, ok := obs["otlp"].(map[string]any)
	if !ok {
		return nil
	}

	var metadata map[string]any
	if workflowData.RawFrontmatter != nil {
		if metadataValue, exists := workflowData.RawFrontmatter["metadata"]; exists {
			if metadataMap, isMap := metadataValue.(map[string]any); isMap {
				metadata = metadataMap
			}
		}
	}

	for _, field := range []string{"attributes", "resource-attributes"} {
		attributes := normalizeOTLPAttributeMap(otlp, field)
		if attributes == nil {
			continue
		}
		names := make([]string, 0, len(attributes))
		for name := range attributes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := attributes[name]
			text, ok := value.(string)
			if !ok || !containsOTLPMetadataAccess(text) {
				continue
			}
			scope := "observability.otlp." + field + "." + name
			resolved, err := resolveOTLPMetadataValue(text, metadata, scope)
			if err != nil {
				return err
			}
			attributes[name] = resolved
		}
	}
	return nil
}

func resolveOTLPMetadataValue(text string, metadata map[string]any, scope string) (string, error) {
	expression, ok := actionslanguageservice.WrappedExpression(text)
	path, reference := actionslanguageservice.Reference(expression)
	if !ok || !reference || len(path) != 4 || !referenceHasPrefix(path, []string{"github", "aw", "metadata"}) {
		return "", fmt.Errorf("%s must be a whole-value reference in the form ${{ github.aw.metadata.<key> }}", scope)
	}
	key := path[3]
	if metadata == nil {
		return "", fmt.Errorf("%s references metadata key %q, but the root workflow has no metadata map", scope, key)
	}
	metadataValue, exists := metadata[key]
	if !exists {
		return "", fmt.Errorf("%s references missing metadata key %q", scope, key)
	}
	resolved, ok := metadataValue.(string)
	if !ok {
		return "", fmt.Errorf("%s references metadata key %q, which must be a string", scope, key)
	}
	if strings.Contains(resolved, "${{") || strings.Contains(resolved, "}}") {
		return "", fmt.Errorf("%s cannot resolve metadata key %q because its value contains GitHub Actions expression delimiters", scope, key)
	}
	return resolved, nil
}

func containsOTLPMetadataAccess(value string) bool {
	for _, path := range interpolationReferences(value) {
		if referenceHasPrefix(path, []string{"github", "aw", "metadata"}) {
			return true
		}
	}
	return false
}

func normalizeOTLPAttributeMap(otlp map[string]any, field string) map[string]any {
	switch attributes := otlp[field].(type) {
	case map[string]any:
		return attributes
	case map[string]string:
		normalized := make(map[string]any, len(attributes))
		for name, value := range attributes {
			normalized[name] = value
		}
		otlp[field] = normalized
		return normalized
	default:
		return nil
	}
}
