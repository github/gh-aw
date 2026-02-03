package parser

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var schemaSuggestionsLog = logger.New("parser:schema_suggestions")

// Constants for suggestion limits and field generation
const (
	maxClosestMatches = 3  // Maximum number of closest matches to find
	maxSuggestions    = 5  // Maximum number of suggestions to show
	maxAcceptedFields = 10 // Maximum number of accepted fields to display
	maxExampleFields  = 3  // Maximum number of fields to include in example JSON
)

// generateSchemaBasedSuggestions generates helpful suggestions based on the schema and error type
func generateSchemaBasedSuggestions(schemaJSON, errorMessage, jsonPath string) string {
	schemaSuggestionsLog.Printf("Generating schema suggestions: path=%s, schema_size=%d bytes", jsonPath, len(schemaJSON))
	// Parse the schema to extract information for suggestions
	var schemaDoc any
	if err := json.Unmarshal([]byte(schemaJSON), &schemaDoc); err != nil {
		schemaSuggestionsLog.Printf("Failed to parse schema JSON: %v", err)
		return "" // Can't parse schema, no suggestions
	}

	// Check if this is an additional properties error
	if strings.Contains(strings.ToLower(errorMessage), "additional propert") && strings.Contains(strings.ToLower(errorMessage), "not allowed") {
		schemaSuggestionsLog.Print("Detected additional properties error")
		invalidProps := extractAdditionalPropertyNames(errorMessage)
		acceptedFields := extractAcceptedFieldsFromSchema(schemaDoc, jsonPath)

		if len(acceptedFields) > 0 {
			schemaSuggestionsLog.Printf("Found %d accepted fields for invalid properties %v", len(acceptedFields), invalidProps)
			return generateFieldSuggestions(invalidProps, acceptedFields)
		}
	}

	// Check if this is a type error
	if strings.Contains(strings.ToLower(errorMessage), "got ") && strings.Contains(strings.ToLower(errorMessage), "want ") {
		schemaSuggestionsLog.Print("Detected type mismatch error")
		example := generateExampleJSONForPath(schemaDoc, jsonPath)
		if example != "" {
			schemaSuggestionsLog.Printf("Generated example JSON: length=%d bytes", len(example))
			return fmt.Sprintf("Expected format: %s", example)
		}
	}

	schemaSuggestionsLog.Print("No suggestions generated for error")
	return ""
}

// extractAcceptedFieldsFromSchema extracts the list of accepted fields from a schema at a given JSON path
func extractAcceptedFieldsFromSchema(schemaDoc any, jsonPath string) []string {
	schemaMap, ok := schemaDoc.(map[string]any)
	if !ok {
		return nil
	}

	// Navigate to the schema section for the given path
	targetSchema := navigateToSchemaPath(schemaMap, jsonPath)
	if targetSchema == nil {
		return nil
	}

	// Extract properties from the target schema
	if properties, ok := targetSchema["properties"].(map[string]any); ok {
		var fields []string
		for fieldName := range properties {
			fields = append(fields, fieldName)
		}
		sort.Strings(fields) // Sort for consistent output
		return fields
	}

	return nil
}

// navigateToSchemaPath navigates to the appropriate schema section for a given JSON path
func navigateToSchemaPath(schema map[string]any, jsonPath string) map[string]any {
	if jsonPath == "" {
		schemaSuggestionsLog.Print("Navigating to root schema path")
		return schema // Root level
	}

	// Parse the JSON path and navigate through the schema
	schemaSuggestionsLog.Printf("Navigating schema path: %s", jsonPath)
	pathSegments := parseJSONPath(jsonPath)
	current := schema

	for _, segment := range pathSegments {
		switch segment.Type {
		case "key":
			// Navigate to properties -> key
			if properties, ok := current["properties"].(map[string]any); ok {
				if keySchema, ok := properties[segment.Value].(map[string]any); ok {
					current = resolveSchemaWithOneOf(keySchema)
				} else {
					return nil // Path not found in schema
				}
			} else {
				return nil // No properties in current schema
			}
		case "index":
			// For array indices, navigate to items schema
			if items, ok := current["items"].(map[string]any); ok {
				current = items
			} else {
				return nil // No items schema for array
			}
		}
	}

	return current
}

// resolveSchemaWithOneOf resolves a schema that may contain oneOf, choosing the object variant for suggestions
func resolveSchemaWithOneOf(schema map[string]any) map[string]any {
	// Check if this schema has oneOf
	if oneOf, ok := schema["oneOf"].([]any); ok {
		// Look for the first object type in oneOf that has properties
		for _, variant := range oneOf {
			if variantMap, ok := variant.(map[string]any); ok {
				if schemaType, ok := variantMap["type"].(string); ok && schemaType == "object" {
					if _, hasProperties := variantMap["properties"]; hasProperties {
						return variantMap
					}
				}
			}
		}
		// If no object with properties found, return the first variant
		if len(oneOf) > 0 {
			if firstVariant, ok := oneOf[0].(map[string]any); ok {
				return firstVariant
			}
		}
	}

	return schema
}

// generateFieldSuggestions creates a helpful suggestion message for invalid field names
func generateFieldSuggestions(invalidProps, acceptedFields []string) string {
	if len(acceptedFields) == 0 || len(invalidProps) == 0 {
		return ""
	}

	var suggestion strings.Builder

	// Find closest matches using Levenshtein distance
	var suggestions []string
	for _, invalidProp := range invalidProps {
		closest := FindClosestMatches(invalidProp, acceptedFields, maxClosestMatches)
		suggestions = append(suggestions, closest...)
	}

	// Remove duplicates
	uniqueSuggestions := removeDuplicates(suggestions)

	// Generate appropriate message based on suggestions found
	if len(uniqueSuggestions) > 0 {
		if len(invalidProps) == 1 && len(uniqueSuggestions) == 1 {
			// Single typo, single suggestion
			suggestion.WriteString("Did you mean '")
			suggestion.WriteString(uniqueSuggestions[0])
			suggestion.WriteString("'?")
		} else {
			// Multiple typos or multiple suggestions
			suggestion.WriteString("Did you mean: ")
			if len(uniqueSuggestions) <= maxSuggestions {
				suggestion.WriteString(strings.Join(uniqueSuggestions, ", "))
			} else {
				suggestion.WriteString(strings.Join(uniqueSuggestions[:maxSuggestions], ", "))
				suggestion.WriteString(", ...")
			}
		}
	} else {
		// No close matches found - show all valid fields
		suggestion.WriteString("Valid fields are: ")
		if len(acceptedFields) <= maxAcceptedFields {
			suggestion.WriteString(strings.Join(acceptedFields, ", "))
		} else {
			suggestion.WriteString(strings.Join(acceptedFields[:maxAcceptedFields], ", "))
			suggestion.WriteString(", ...")
		}
	}

	return suggestion.String()
}

// FindClosestMatches finds the closest matching strings using Levenshtein distance.
// It returns up to maxResults matches that have a Levenshtein distance of 3 or less.
// Results are sorted by distance (closest first), then alphabetically for ties.
func FindClosestMatches(target string, candidates []string, maxResults int) []string {
	schemaSuggestionsLog.Printf("Finding closest matches for '%s' from %d candidates", target, len(candidates))
	type match struct {
		value    string
		distance int
	}

	const maxDistance = 3 // Maximum acceptable Levenshtein distance

	var matches []match
	targetLower := strings.ToLower(target)

	for _, candidate := range candidates {
		candidateLower := strings.ToLower(candidate)

		// Skip exact matches
		if targetLower == candidateLower {
			continue
		}

		distance := LevenshteinDistance(targetLower, candidateLower)

		// Only include if distance is within acceptable range
		if distance <= maxDistance {
			matches = append(matches, match{value: candidate, distance: distance})
		}
	}

	// Sort by distance (lower is better), then alphabetically for ties
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].distance != matches[j].distance {
			return matches[i].distance < matches[j].distance
		}
		return matches[i].value < matches[j].value
	})

	// Return top matches
	var results []string
	for i := 0; i < len(matches) && i < maxResults; i++ {
		results = append(results, matches[i].value)
	}

	schemaSuggestionsLog.Printf("Found %d closest matches (from %d total matches within max distance)", len(results), len(matches))
	return results
}

// LevenshteinDistance computes the Levenshtein distance between two strings.
// This is the minimum number of single-character edits (insertions, deletions, or substitutions)
// required to change one string into the other.
func LevenshteinDistance(a, b string) int {
	aLen := len(a)
	bLen := len(b)

	// Early exit for empty strings
	if aLen == 0 {
		return bLen
	}
	if bLen == 0 {
		return aLen
	}

	// Create a 2D matrix for dynamic programming
	// We only need the previous row, so we can optimize space
	previousRow := make([]int, bLen+1)
	currentRow := make([]int, bLen+1)

	// Initialize the first row (distance from empty string)
	for i := 0; i <= bLen; i++ {
		previousRow[i] = i
	}

	// Calculate distances for each character in string a
	for i := 1; i <= aLen; i++ {
		currentRow[0] = i // Distance from empty string

		for j := 1; j <= bLen; j++ {
			// Cost of substitution (0 if characters match, 1 otherwise)
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}

			// Minimum of:
			// - Deletion: previousRow[j] + 1
			// - Insertion: currentRow[j-1] + 1
			// - Substitution: previousRow[j-1] + cost
			deletion := previousRow[j] + 1
			insertion := currentRow[j-1] + 1
			substitution := previousRow[j-1] + cost

			currentRow[j] = min(deletion, min(insertion, substitution))
		}

		// Swap rows for next iteration
		previousRow, currentRow = currentRow, previousRow
	}

	return previousRow[bLen]
}

// generateExampleJSONForPath generates an example JSON object for a specific schema path
func generateExampleJSONForPath(schemaDoc any, jsonPath string) string {
	schemaMap, ok := schemaDoc.(map[string]any)
	if !ok {
		return ""
	}

	// Navigate to the target schema
	targetSchema := navigateToSchemaPath(schemaMap, jsonPath)
	if targetSchema == nil {
		return ""
	}

	// Generate example based on schema type
	example := generateExampleFromSchema(targetSchema)
	if example == nil {
		return ""
	}

	// Convert to JSON string
	exampleJSON, err := json.Marshal(example)
	if err != nil {
		return ""
	}

	return string(exampleJSON)
}

// generateExampleFromSchema generates an example value based on a JSON schema
func generateExampleFromSchema(schema map[string]any) any {
	schemaType, ok := schema["type"].(string)
	if !ok {
		// Try to infer from other properties
		if _, hasProperties := schema["properties"]; hasProperties {
			schemaType = "object"
		} else if _, hasItems := schema["items"]; hasItems {
			schemaType = "array"
		} else {
			return nil
		}
	}

	switch schemaType {
	case "string":
		if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
			if str, ok := enum[0].(string); ok {
				return str
			}
		}
		return "string"
	case "number", "integer":
		return 42
	case "boolean":
		return true
	case "array":
		if items, ok := schema["items"].(map[string]any); ok {
			itemExample := generateExampleFromSchema(items)
			if itemExample != nil {
				return []any{itemExample}
			}
		}
		return []any{}
	case "object":
		result := make(map[string]any)
		if properties, ok := schema["properties"].(map[string]any); ok {
			// Add required properties first
			requiredFields := make(map[string]bool)
			if required, ok := schema["required"].([]any); ok {
				for _, field := range required {
					if fieldName, ok := field.(string); ok {
						requiredFields[fieldName] = true
					}
				}
			}

			// Add a few example properties (prioritize required ones)
			count := 0

			// First, add required fields
			for propName, propSchema := range properties {
				if requiredFields[propName] && count < maxExampleFields {
					if propSchemaMap, ok := propSchema.(map[string]any); ok {
						result[propName] = generateExampleFromSchema(propSchemaMap)
						count++
					}
				}
			}

			// Then add some optional fields if we have room
			for propName, propSchema := range properties {
				if !requiredFields[propName] && count < maxExampleFields {
					if propSchemaMap, ok := propSchema.(map[string]any); ok {
						result[propName] = generateExampleFromSchema(propSchemaMap)
						count++
					}
				}
			}
		}
		return result
	}

	return nil
}
