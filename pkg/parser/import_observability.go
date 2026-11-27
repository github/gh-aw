// Package parser provides functions for parsing and processing workflow markdown files.
// import_observability.go implements OTLP endpoint extraction and merging for imported
// workflow observability configurations.
package parser

import (
	"encoding/json"
	"maps"

	"github.com/github/gh-aw/pkg/setutil"
)

// observabilityImportEndpoint is an endpoint entry used during import merging.
// Headers are kept as any (original format: string or map) so that the workflow
// package can later normalise both supported forms correctly.
type observabilityImportEndpoint struct {
	URL     string `json:"url"`
	Headers any    `json:"headers,omitempty"`
}

// extractOTLPEndpointsFromObsMap reads the `otlp.endpoint` field from a raw
// observability map and returns all endpoint entries as observabilityImportEndpoints.
// Supports string, object, and array forms of the endpoint field.
// Top-level `headers` is only applied to the backward-compat string endpoint form.
func extractOTLPEndpointsFromObsMap(obs map[string]any) []observabilityImportEndpoint {
	otlpAny, ok := obs["otlp"]
	if !ok {
		return nil
	}
	otlpMap, ok := otlpAny.(map[string]any)
	if !ok {
		return nil
	}

	endpointRaw := otlpMap["endpoint"]
	headersRaw := otlpMap["headers"] // only applies to the backward-compat string form

	var result []observabilityImportEndpoint
	switch ep := endpointRaw.(type) {
	case string:
		if ep != "" {
			entry := observabilityImportEndpoint{URL: ep}
			if headersRaw != nil {
				entry.Headers = headersRaw
			}
			result = append(result, entry)
		}
	case map[string]any:
		url, ok := ep["url"].(string)
		if ok && url != "" {
			entry := observabilityImportEndpoint{URL: url}
			if h, hasH := ep["headers"]; hasH {
				entry.Headers = h
			}
			result = append(result, entry)
		}
	case []any:
		for _, item := range ep {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			url, ok := itemMap["url"].(string)
			if !ok || url == "" {
				continue
			}
			entry := observabilityImportEndpoint{URL: url}
			if h, hasH := itemMap["headers"]; hasH {
				entry.Headers = h
			}
			result = append(result, entry)
		}
	}
	return result
}

// mergeObservabilityConfigs takes a slice of observability config JSON strings (one per
// import), extracts all OTLP endpoint entries from each (supporting string, object, and
// array forms), deduplicates by URL (first occurrence wins), and returns a single merged
// observability JSON string with all endpoints expressed as an array. Custom OTLP
// span and resource attributes are also merged across imports (first occurrence wins
// per key).
// Returns "" when no valid endpoints or attributes are found.
func mergeObservabilityConfigs(configs []string) string {
	seen := make(map[string]struct{})
	var allEndpoints []observabilityImportEndpoint
	mergedAttrs := make(map[string]string)
	mergedResourceAttrs := make(map[string]string)
	var mergedGitHubApp map[string]any

	for i, cfgJSON := range configs {
		if cfgJSON == "" {
			continue
		}
		var obs map[string]any
		if err := json.Unmarshal([]byte(cfgJSON), &obs); err != nil {
			parserLog.Printf("Failed to unmarshal observability config from import %d during merge: %v", i, err)
			continue
		}
		for _, e := range extractOTLPEndpointsFromObsMap(obs) {
			if !setutil.Contains(seen, e.URL) {
				seen[e.URL] = struct{}{}
				allEndpoints = append(allEndpoints, e)
			}
		}
		mergeOTLPStringMapFirstWins(mergedAttrs, extractOTLPAttributesFromObsMap(obs))
		mergeOTLPStringMapFirstWins(mergedResourceAttrs, extractOTLPResourceAttributesFromObsMap(obs))
		if mergedGitHubApp == nil {
			mergedGitHubApp = extractOTLPGitHubAppFromObsMap(obs)
		}
	}

	if len(allEndpoints) == 0 && len(mergedAttrs) == 0 && len(mergedResourceAttrs) == 0 && mergedGitHubApp == nil {
		return ""
	}

	// Produce a merged config with the endpoint field as an array so that the
	// workflow package's collectAllOTLPEndpoints handles it uniformly.  Include
	// any merged custom attributes so the orchestrator can propagate them.
	otlpMap := map[string]any{}
	if len(allEndpoints) > 0 {
		otlpMap["endpoint"] = allEndpoints
	}
	if len(mergedAttrs) > 0 {
		otlpMap["attributes"] = mergedAttrs
	}
	if len(mergedResourceAttrs) > 0 {
		otlpMap["resource-attributes"] = mergedResourceAttrs
	}
	if mergedGitHubApp != nil {
		otlpMap["github-app"] = mergedGitHubApp
	}
	merged := map[string]any{"otlp": otlpMap}
	b, err := json.Marshal(merged)
	if err != nil {
		parserLog.Printf("Failed to marshal %d merged OTLP endpoints: %v", len(allEndpoints), err)
		return ""
	}
	return string(b)
}

func mergeOTLPStringMapFirstWins(destination, source map[string]string) {
	for key, value := range source {
		if _, exists := destination[key]; !exists {
			destination[key] = value
		}
	}
}

func extractOTLPGitHubAppFromObsMap(obs map[string]any) map[string]any {
	if obs == nil {
		return nil
	}
	otlpAny, ok := obs["otlp"]
	if !ok {
		return nil
	}
	otlpMap, ok := otlpAny.(map[string]any)
	if !ok {
		return nil
	}
	githubAppAny, ok := otlpMap["github-app"]
	if !ok {
		return nil
	}
	githubAppMap, ok := githubAppAny.(map[string]any)
	if !ok {
		return nil
	}
	copyMap := make(map[string]any, len(githubAppMap))
	maps.Copy(copyMap, githubAppMap)
	return copyMap
}

// extractOTLPAttributesFromObsMap reads the custom OTLP attributes map from a
// raw observability section (as parsed from an import's frontmatter).  Only
// string values are accepted; non-string values are silently ignored.
// Returns nil when the field is absent or empty.
//
// Note: this intentionally duplicates the logic of
// workflow.extractOTLPCustomAttributesFromObsMap.  The parser package must not
// import the workflow package (circular-dependency risk), so the helper lives
// here as a local copy.  Both implementations must stay in sync.
func extractOTLPAttributesFromObsMap(obs map[string]any) map[string]string {
	return extractOTLPStringMapFromObsMap(obs, "attributes")
}

func extractOTLPResourceAttributesFromObsMap(obs map[string]any) map[string]string {
	return extractOTLPStringMapFromObsMap(obs, "resource-attributes")
}

func extractOTLPStringMapFromObsMap(obs map[string]any, fieldName string) map[string]string {
	if obs == nil {
		return nil
	}
	otlpAny, ok := obs["otlp"]
	if !ok {
		return nil
	}
	otlpMap, ok := otlpAny.(map[string]any)
	if !ok {
		return nil
	}
	attrsAny, ok := otlpMap[fieldName]
	if !ok {
		return nil
	}
	attrsMap, ok := attrsAny.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(attrsMap))
	for k, v := range attrsMap {
		if s, ok := v.(string); ok && k != "" {
			result[k] = s
		}
	}
	return result
}
