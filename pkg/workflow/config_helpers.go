// This file provides helper functions for parsing safe output configurations.
//
// This file contains parsing utilities for extracting and validating configuration
// values from safe output config maps. These helpers are used across safe output
// processors to parse common configuration patterns.
//
// # Organization Rationale
//
// These parse functions are grouped in a helper file because they:
//   - Share a common purpose (safe output config parsing)
//   - Are used by multiple safe output modules (3+ callers)
//   - Provide stable, reusable parsing patterns
//   - Have clear domain focus (configuration extraction)
//
// This follows the helper file conventions documented in the developer instructions.
// See skills/developer/SKILL.md#helper-file-conventions for details.
//
// # Key Functions
//
// Configuration Array Parsing:
//   - ParseStringArrayFromConfig() - Generic string array extraction
//   - parseLabelsFromConfig() - Extract labels array
//   - parseParticipantsFromConfig() - Extract participants array
//   - parseAllowedLabelsFromConfig() - Extract allowed labels array
//
// Configuration String Parsing:
//   - extractStringFromMap() - Generic string extraction
//   - parseTitlePrefixFromConfig() - Extract title prefix
//   - parseTargetRepoFromConfig() - Extract target repository
//   - parseTargetRepoWithValidation() - Extract and validate target repo
//
// Configuration Integer Parsing:
//   - parseExpiresFromConfig() - Extract expiration time
//   - parseRelativeTimeSpec() - Parse relative time specifications

package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var configHelpersLog = logger.New("workflow:config_helpers")

// ParseStringArrayFromConfig is a generic helper that extracts and validates a string array from a map
// Returns a slice of strings, or nil if not present or invalid
// If log is provided, it will log the extracted values for debugging
func ParseStringArrayFromConfig(m map[string]any, key string, log *logger.Logger) []string {
	if value, exists := m[key]; exists {
		if log != nil {
			log.Printf("Parsing %s from config", key)
		}
		if arrayValue, ok := value.([]any); ok {
			var strings []string
			for _, item := range arrayValue {
				if strVal, ok := item.(string); ok {
					strings = append(strings, strVal)
				}
			}
			// Return the slice even if empty (to distinguish from not provided)
			if strings == nil {
				if log != nil {
					log.Printf("No valid %s strings found, returning empty array", key)
				}
				return []string{}
			}
			if log != nil {
				log.Printf("Parsed %d %s from config", len(strings), key)
			}
			return strings
		}
	}
	return nil
}

// parseLabelsFromConfig extracts and validates labels from a config map
// Returns a slice of label strings, or nil if labels is not present or invalid
func parseLabelsFromConfig(configMap map[string]any) []string {
	return ParseStringArrayFromConfig(configMap, "labels", configHelpersLog)
}

// extractStringFromMap is a generic helper that extracts and validates a string value from a map
// Returns the string value, or empty string if not present or invalid
// If log is provided, it will log the extracted value for debugging
func extractStringFromMap(m map[string]any, key string, log *logger.Logger) string {
	if value, exists := m[key]; exists {
		if valueStr, ok := value.(string); ok {
			if log != nil {
				log.Printf("Parsed %s from config: %s", key, valueStr)
			}
			return valueStr
		}
	}
	return ""
}

// parseTitlePrefixFromConfig extracts and validates title-prefix from a config map
// Returns the title prefix string, or empty string if not present or invalid
func parseTitlePrefixFromConfig(configMap map[string]any) string {
	return extractStringFromMap(configMap, "title-prefix", configHelpersLog)
}

// parseTargetRepoFromConfig extracts the target-repo value from a config map.
// Returns the target repository slug as a string, or empty string if not present or invalid.
// This function does not perform any special handling or validation for wildcard values ("*");
// callers are responsible for validating the returned value as needed.
func parseTargetRepoFromConfig(configMap map[string]any) string {
	return extractStringFromMap(configMap, "target-repo", configHelpersLog)
}

// parseTargetRepoWithValidation extracts the target-repo value from a config map and validates it.
// Returns the target repository slug as a string, or empty string if not present or invalid.
// Returns an error (indicated by the second return value being true) if the value is "*" (wildcard),
// which is not allowed for safe output target repositories.
func parseTargetRepoWithValidation(configMap map[string]any) (string, bool) {
	targetRepoSlug := parseTargetRepoFromConfig(configMap)
	// Validate that target-repo is not "*" - only definite strings are allowed
	if targetRepoSlug == "*" {
		configHelpersLog.Print("Invalid target-repo: wildcard '*' is not allowed")
		return "", true // Return true to indicate validation error
	}
	return targetRepoSlug, false
}

// validateTargetRepoSlug validates that a target-repo slug is not a wildcard.
// Returns true if the value is invalid (i.e., equals "*").
// This helper is used when the target-repo has already been parsed into a struct field.
func validateTargetRepoSlug(targetRepoSlug string, log *logger.Logger) bool {
	if targetRepoSlug == "*" {
		if log != nil {
			log.Print("Invalid target-repo: wildcard '*' is not allowed")
		}
		return true // Return true to indicate validation error
	}
	return false
}

// parseParticipantsFromConfig extracts and validates participants (assignees/reviewers) from a config map.
// Supports both string (single participant) and array (multiple participants) formats.
// Returns a slice of participant usernames, or nil if not present or invalid.
// The participantKey parameter specifies which key to look for (e.g., "assignees" or "reviewers").
func parseParticipantsFromConfig(configMap map[string]any, participantKey string) []string {
	if participants, exists := configMap[participantKey]; exists {
		configHelpersLog.Printf("Parsing %s from config", participantKey)

		// Handle single string format
		if participantStr, ok := participants.(string); ok {
			configHelpersLog.Printf("Parsed single %s: %s", participantKey, participantStr)
			return []string{participantStr}
		}

		// Handle array format
		if participantsArray, ok := participants.([]any); ok {
			var participantStrings []string
			for _, participant := range participantsArray {
				if participantStr, ok := participant.(string); ok {
					participantStrings = append(participantStrings, participantStr)
				}
			}
			// Return the slice even if empty (to distinguish from not provided)
			if participantStrings == nil {
				configHelpersLog.Printf("No valid %s strings found, returning empty array", participantKey)
				return []string{}
			}
			configHelpersLog.Printf("Parsed %d %s from config", len(participantStrings), participantKey)
			return participantStrings
		}
	}
	return nil
}

// parseAllowedLabelsFromConfig extracts and validates allowed-labels from a config map.
// Returns a slice of label strings, or nil if not present or invalid.
func parseAllowedLabelsFromConfig(configMap map[string]any) []string {
	return ParseStringArrayFromConfig(configMap, "allowed-labels", configHelpersLog)
}

// NOTE: parseExpiresFromConfig and parseRelativeTimeSpec have been moved to time_delta.go
// to consolidate all time parsing logic in a single location. These functions are used
// for parsing expiration configurations in safe output jobs.

// ParseIntFromConfig is a generic helper that extracts and validates an integer value from a map.
// Supports int, int64, float64, and uint64 types.
// Returns the integer value, or 0 if not present or invalid.
// If log is provided, it will log the extracted value for debugging.
// Note: For uint64 values, returns 0 if the value would overflow int.
func ParseIntFromConfig(m map[string]any, key string, log *logger.Logger) int {
	if value, exists := m[key]; exists {
		if log != nil {
			log.Printf("Parsing %s from config", key)
		}
		// Use parseIntValue for the actual type conversion
		if result, ok := parseIntValue(value); ok {
			if log != nil {
				log.Printf("Parsed %s from config: %d", key, result)
			}
			return result
		}
	}
	return 0
}

// ParseBoolFromConfig is a generic helper that extracts and validates a boolean value from a map.
// Returns the boolean value, or false if not present or invalid.
// If log is provided, it will log the extracted value for debugging.
func ParseBoolFromConfig(m map[string]any, key string, log *logger.Logger) bool {
	if value, exists := m[key]; exists {
		if log != nil {
			log.Printf("Parsing %s from config", key)
		}
		if boolValue, ok := value.(bool); ok {
			if log != nil {
				log.Printf("Parsed %s from config: %t", key, boolValue)
			}
			return boolValue
		}
	}
	return false
}

// unmarshalConfig unmarshals a config value from a map into a typed struct using YAML.
// This provides type-safe parsing by leveraging YAML struct tags on config types.
// Returns an error if the config key doesn't exist, the value can't be marshaled, or unmarshaling fails.
//
// Example usage:
//
//	var config CreateIssuesConfig
//	if err := unmarshalConfig(outputMap, "create-issue", &config, log); err != nil {
//	    return nil, err
//	}
//
// This function:
// 1. Extracts the config value from the map using the provided key
// 2. Marshals it to YAML bytes (preserving structure)
// 3. Unmarshals the YAML into the typed struct (using struct tags for field mapping)
// 4. Validates that all fields are properly typed
func unmarshalConfig(m map[string]any, key string, target any, log *logger.Logger) error {
	configData, exists := m[key]
	if !exists {
		return fmt.Errorf("config key %q not found", key)
	}

	// Handle nil config gracefully - unmarshal empty map
	if configData == nil {
		configData = map[string]any{}
	}

	if log != nil {
		log.Printf("Unmarshaling config for key %q into typed struct", key)
	}

	// Marshal the config data back to YAML bytes
	yamlBytes, err := yaml.Marshal(configData)
	if err != nil {
		return fmt.Errorf("failed to marshal config for %q: %w", key, err)
	}

	// Unmarshal into the typed struct
	if err := yaml.Unmarshal(yamlBytes, target); err != nil {
		return fmt.Errorf("failed to unmarshal config for %q: %w", key, err)
	}

	if log != nil {
		log.Printf("Successfully unmarshaled config for key %q", key)
	}

	return nil
}
