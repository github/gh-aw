package parser

import (
	"encoding/json"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var contentExtractorLog = logger.New("parser:content_extractor")

// extractToolsFromContent extracts tools and mcp-servers sections from frontmatter as JSON string
func extractToolsFromContent(content string) (string, error) {
	log.Printf("Extracting tools from content: size=%d bytes", len(content))
	result, err := ExtractFrontmatterFromContent(content)
	if err != nil {
		log.Printf("Failed to extract frontmatter: %v", err)
		return "{}", nil // Return empty object on error to match bash behavior
	}

	// Create a map to hold the merged result
	extracted := make(map[string]any)

	// Helper function to merge a field into extracted map
	mergeField := func(fieldName string) {
		if fieldValue, exists := result.Frontmatter[fieldName]; exists {
			if fieldMap, ok := fieldValue.(map[string]any); ok {
				for key, value := range fieldMap {
					extracted[key] = value
				}
			}
		}
	}

	// Extract and merge tools section (tools are stored as tool_name: tool_config)
	mergeField("tools")

	// Extract and merge mcp-servers section (mcp-servers are stored as server_name: server_config)
	mergeField("mcp-servers")

	// If nothing was extracted, return empty object
	if len(extracted) == 0 {
		log.Print("No tools or mcp-servers found in content")
		return "{}", nil
	}

	log.Printf("Extracted %d tool/server configurations", len(extracted))
	// Convert to JSON string
	extractedJSON, err := json.Marshal(extracted)
	if err != nil {
		return "{}", nil
	}

	return strings.TrimSpace(string(extractedJSON)), nil
}

// extractSafeOutputsFromContent extracts safe-outputs section from frontmatter as JSON string
func extractSafeOutputsFromContent(content string) (string, error) {
	contentExtractorLog.Print("Extracting safe-outputs from content")
	return extractFrontmatterField(content, "safe-outputs", "{}")
}

// extractSafeInputsFromContent extracts safe-inputs section from frontmatter as JSON string
func extractSafeInputsFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "safe-inputs", "{}")
}

// extractMCPServersFromContent extracts mcp-servers section from frontmatter as JSON string
func extractMCPServersFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "mcp-servers", "{}")
}

// extractStepsFromContent extracts steps section from frontmatter as YAML string
func extractStepsFromContent(content string) (string, error) {
	result, err := ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", nil // Return empty string on error
	}

	// Extract steps section
	steps, exists := result.Frontmatter["steps"]
	if !exists {
		return "", nil
	}

	// Convert to YAML string (similar to how CustomSteps are handled in compiler)
	stepsYAML, err := yaml.Marshal(steps)
	if err != nil {
		return "", nil
	}

	return strings.TrimSpace(string(stepsYAML)), nil
}

// extractEngineFromContent extracts engine section from frontmatter as JSON string
func extractEngineFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "engine", "")
}

// extractRuntimesFromContent extracts runtimes section from frontmatter as JSON string
func extractRuntimesFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "runtimes", "{}")
}

// extractServicesFromContent extracts services section from frontmatter as YAML string
func extractServicesFromContent(content string) (string, error) {
	result, err := ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", nil // Return empty string on error
	}

	// Extract services section
	services, exists := result.Frontmatter["services"]
	if !exists {
		return "", nil
	}

	// Convert to YAML string (similar to how steps are handled)
	servicesYAML, err := yaml.Marshal(services)
	if err != nil {
		return "", nil
	}

	return strings.TrimSpace(string(servicesYAML)), nil
}

// extractNetworkFromContent extracts network section from frontmatter as JSON string
func extractNetworkFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "network", "{}")
}

// ExtractPermissionsFromContent extracts permissions section from frontmatter as JSON string
func ExtractPermissionsFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "permissions", "{}")
}

// extractSecretMaskingFromContent extracts secret-masking section from frontmatter as JSON string
func extractSecretMaskingFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "secret-masking", "{}")
}

// extractBotsFromContent extracts bots section from frontmatter as JSON string
func extractBotsFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "bots", "[]")
}

// extractPluginsFromContent extracts plugins section from frontmatter as JSON string
func extractPluginsFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "plugins", "[]")
}

// extractPostStepsFromContent extracts post-steps section from frontmatter as YAML string
func extractPostStepsFromContent(content string) (string, error) {
	result, err := ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", nil // Return empty string on error
	}

	// Extract post-steps section
	postSteps, exists := result.Frontmatter["post-steps"]
	if !exists {
		return "", nil
	}

	// Convert to YAML string (similar to how steps are handled)
	postStepsYAML, err := yaml.Marshal(postSteps)
	if err != nil {
		return "", nil
	}

	return strings.TrimSpace(string(postStepsYAML)), nil
}

// extractLabelsFromContent extracts labels section from frontmatter as JSON string
func extractLabelsFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "labels", "[]")
}

// extractCacheFromContent extracts cache section from frontmatter as JSON string
func extractCacheFromContent(content string) (string, error) {
	return extractFrontmatterField(content, "cache", "{}")
}

// extractFrontmatterField extracts a specific field from frontmatter as JSON string
func extractFrontmatterField(content, fieldName, emptyValue string) (string, error) {
	contentExtractorLog.Printf("Extracting field: %s", fieldName)
	result, err := ExtractFrontmatterFromContent(content)
	if err != nil {
		contentExtractorLog.Printf("Failed to extract frontmatter for field %s: %v", fieldName, err)
		return emptyValue, nil // Return empty value on error
	}

	// Extract the requested field
	fieldValue, exists := result.Frontmatter[fieldName]
	if !exists {
		contentExtractorLog.Printf("Field %s not found in frontmatter", fieldName)
		return emptyValue, nil
	}

	// Convert to JSON string
	fieldJSON, err := json.Marshal(fieldValue)
	if err != nil {
		contentExtractorLog.Printf("Failed to marshal field %s to JSON: %v", fieldName, err)
		return emptyValue, nil
	}

	contentExtractorLog.Printf("Successfully extracted field %s: size=%d bytes", fieldName, len(fieldJSON))
	return strings.TrimSpace(string(fieldJSON)), nil
}
