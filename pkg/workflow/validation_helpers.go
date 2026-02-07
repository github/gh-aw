// This file provides validation helper functions for agentic workflow compilation.
//
// This file contains reusable validation helpers for common validation patterns
// such as integer range validation, string validation, and list membership checks.
// These utilities are used across multiple workflow configuration validation functions.
//
// # Available Helper Functions
//
//   - validateIntRange() - Validates that an integer value is within a specified range
//   - ValidateRequired() - Validates that a required field is not empty
//   - ValidateMaxLength() - Validates that a field does not exceed maximum length
//   - ValidateMinLength() - Validates that a field meets minimum length requirement
//   - ValidateInList() - Validates that a value is in an allowed list
//   - ValidatePositiveInt() - Validates that a value is a positive integer
//   - ValidateNonNegativeInt() - Validates that a value is a non-negative integer
//   - fileExists() - Checks if a file exists at the given path
//   - isEmptyOrNil() - Checks if a value is empty, nil, or zero (Phase 2)
//   - getMapFieldAsString() - Safely extracts a string field from a map[string]any (Phase 2)
//   - getMapFieldAsMap() - Safely extracts a nested map from a map[string]any (Phase 2)
//   - getMapFieldAsBool() - Safely extracts a boolean field from a map[string]any (Phase 2)
//   - getMapFieldAsInt() - Safely extracts an integer field from a map[string]any (Phase 2)
//   - dirExists() - Checks if a directory exists at the given path (Phase 2)
//
// # Design Rationale
//
// These helpers consolidate 76+ duplicate validation patterns identified in the
// semantic function clustering analysis. By extracting common patterns, we:
//   - Reduce code duplication across 32 validation files
//   - Provide consistent validation behavior
//   - Make validation code more maintainable and testable
//   - Reduce cognitive overhead when writing new validators
//
// For the validation architecture overview, see validation.go.

package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var validationHelpersLog = logger.New("workflow:validation_helpers")

// validateIntRange validates that a value is within the specified inclusive range [min, max].
// It returns an error if the value is outside the range, with a descriptive message
// including the field name and the actual value.
//
// Parameters:
//   - value: The integer value to validate
//   - min: The minimum allowed value (inclusive)
//   - max: The maximum allowed value (inclusive)
//   - fieldName: A human-readable name for the field being validated (used in error messages)
//
// Returns:
//   - nil if the value is within range
//   - error with a descriptive message if the value is outside the range
//
// Example:
//
//	err := validateIntRange(port, 1, 65535, "port")
//	if err != nil {
//	    return err
//	}
func validateIntRange(value, min, max int, fieldName string) error {
	if value < min || value > max {
		return fmt.Errorf("%s must be between %d and %d, got %d",
			fieldName, min, max, value)
	}
	return nil
}

// ValidateRequired validates that a required field is not empty
func ValidateRequired(field, value string) error {
	if strings.TrimSpace(value) == "" {
		validationHelpersLog.Printf("Required field validation failed: field=%s", field)
		return NewValidationError(
			field,
			value,
			"field is required and cannot be empty",
			fmt.Sprintf("Provide a non-empty value for '%s'", field),
		)
	}
	return nil
}

// ValidateMaxLength validates that a field does not exceed maximum length
func ValidateMaxLength(field, value string, maxLength int) error {
	if len(value) > maxLength {
		return NewValidationError(
			field,
			value,
			fmt.Sprintf("field exceeds maximum length of %d characters (actual: %d)", maxLength, len(value)),
			fmt.Sprintf("Shorten '%s' to %d characters or less", field, maxLength),
		)
	}
	return nil
}

// ValidateMinLength validates that a field meets minimum length requirement
func ValidateMinLength(field, value string, minLength int) error {
	if len(value) < minLength {
		return NewValidationError(
			field,
			value,
			fmt.Sprintf("field is shorter than minimum length of %d characters (actual: %d)", minLength, len(value)),
			fmt.Sprintf("Ensure '%s' is at least %d characters long", field, minLength),
		)
	}
	return nil
}

// ValidateInList validates that a value is in an allowed list
func ValidateInList(field, value string, allowedValues []string) error {
	for _, allowed := range allowedValues {
		if value == allowed {
			return nil
		}
	}

	validationHelpersLog.Printf("List validation failed: field=%s, value=%s not in allowed list", field, value)
	return NewValidationError(
		field,
		value,
		fmt.Sprintf("value is not in allowed list: %v", allowedValues),
		fmt.Sprintf("Choose one of the allowed values for '%s': %s", field, strings.Join(allowedValues, ", ")),
	)
}

// ValidatePositiveInt validates that a value is a positive integer
func ValidatePositiveInt(field string, value int) error {
	if value <= 0 {
		return NewValidationError(
			field,
			fmt.Sprintf("%d", value),
			"value must be a positive integer",
			fmt.Sprintf("Provide a positive integer value for '%s'", field),
		)
	}
	return nil
}

// ValidateNonNegativeInt validates that a value is a non-negative integer
func ValidateNonNegativeInt(field string, value int) error {
	if value < 0 {
		return NewValidationError(
			field,
			fmt.Sprintf("%d", value),
			"value must be a non-negative integer",
			fmt.Sprintf("Provide a non-negative integer value for '%s'", field),
		)
	}
	return nil
}

// fileExists checks if a file exists at the given path.
// Returns true if the file exists and is accessible, false otherwise.
//
// This helper consolidates common file existence checking patterns.
//
// Example:
//
//	if !fileExists(agentPath) {
//	    return NewValidationError("agent.file", agentPath, "file does not exist", "...")
//	}
func fileExists(path string) bool {
	if path == "" {
		validationHelpersLog.Print("File existence check failed: empty path")
		return false
	}

	info, err := os.Stat(path)
	if err != nil {
		validationHelpersLog.Printf("File existence check failed: path=%s, error=%v", path, err)
		return false
	}

	return !info.IsDir()
}

// isEmptyOrNil evaluates whether a value represents an empty or absent state.
// This consolidates various emptiness checks across the codebase into a single
// reusable function. The function handles multiple value types with appropriate
// emptiness semantics for each.
//
// Returns true when encountering:
//   - nil values (representing absence)
//   - strings that are empty or contain only whitespace
//   - numeric types equal to zero
//   - boolean false
//   - collections (slices, maps) with no elements
//
// Usage pattern:
//
//	if isEmptyOrNil(configValue) {
//	    return NewValidationError("fieldName", "", "required field missing", "provide a value")
//	}
func isEmptyOrNil(candidate any) bool {
	// Handle nil case first
	if candidate == nil {
		return true
	}

	// Type-specific emptiness checks using reflection-free approach
	switch typedValue := candidate.(type) {
	case string:
		// String is empty if blank after trimming whitespace
		return len(strings.TrimSpace(typedValue)) == 0
	case int:
		return typedValue == 0
	case int8:
		return typedValue == 0
	case int16:
		return typedValue == 0
	case int32:
		return typedValue == 0
	case int64:
		return typedValue == 0
	case uint:
		return typedValue == 0
	case uint8:
		return typedValue == 0
	case uint16:
		return typedValue == 0
	case uint32:
		return typedValue == 0
	case uint64:
		return typedValue == 0
	case float32:
		return typedValue == 0.0
	case float64:
		return typedValue == 0.0
	case bool:
		// false represents empty boolean state
		return !typedValue
	case []any:
		return len(typedValue) == 0
	case map[string]any:
		return len(typedValue) == 0
	}

	// Non-nil values of unrecognized types are considered non-empty
	return false
}

// getMapFieldAsString retrieves a string value from a configuration map with safe type handling.
// This function wraps the common pattern of extracting string fields from map[string]any structures
// that result from YAML parsing, providing consistent error behavior and logging.
//
// The function returns the fallback value in these scenarios:
//   - Source map is nil
//   - Requested key doesn't exist in map
//   - Value at key is not a string type
//
// Parameters:
//   - source: The configuration map to query
//   - fieldKey: The key to look up in the map
//   - fallback: Value returned when extraction fails
//
// Example usage:
//
//	titleValue := getMapFieldAsString(frontmatter, "title", "")
//	if titleValue == "" {
//	    return NewValidationError("title", "", "title required", "provide a title")
//	}
func getMapFieldAsString(source map[string]any, fieldKey string, fallback string) string {
	// Early return for nil map
	if source == nil {
		return fallback
	}

	// Attempt to retrieve value
	retrievedValue, keyFound := source[fieldKey]
	if !keyFound {
		return fallback
	}

	// Verify type before returning
	stringValue, isString := retrievedValue.(string)
	if !isString {
		validationHelpersLog.Printf("Type mismatch for key %q: expected string, found %T", fieldKey, retrievedValue)
		return fallback
	}

	return stringValue
}

// getMapFieldAsMap retrieves a nested map value from a configuration map with safe type handling.
// This consolidates the pattern of extracting nested configuration sections while handling
// type mismatches gracefully. Returns nil when the field cannot be extracted as a map.
//
// Parameters:
//   - source: The parent configuration map
//   - fieldKey: The key identifying the nested map
//
// Example usage:
//
//	toolsSection := getMapFieldAsMap(config, "tools")
//	if toolsSection != nil {
//	    playwrightConfig := getMapFieldAsMap(toolsSection, "playwright")
//	}
func getMapFieldAsMap(source map[string]any, fieldKey string) map[string]any {
	// Guard against nil source
	if source == nil {
		return nil
	}

	// Look up the field
	retrievedValue, keyFound := source[fieldKey]
	if !keyFound {
		return nil
	}

	// Type assert to nested map
	mapValue, isMap := retrievedValue.(map[string]any)
	if !isMap {
		validationHelpersLog.Printf("Type mismatch for key %q: expected map[string]any, found %T", fieldKey, retrievedValue)
		return nil
	}

	return mapValue
}

// getMapFieldAsBool retrieves a boolean value from a configuration map with safe type handling.
// This wraps the pattern of extracting boolean configuration flags while providing consistent
// fallback behavior when the value is missing or has an unexpected type.
//
// Parameters:
//   - source: The configuration map to query
//   - fieldKey: The key to look up
//   - fallback: Value returned when extraction fails
//
// Example usage:
//
//	sandboxEnabled := getMapFieldAsBool(config, "sandbox", false)
//	if sandboxEnabled {
//	    // Enable sandbox mode
//	}
func getMapFieldAsBool(source map[string]any, fieldKey string, fallback bool) bool {
	// Handle nil source
	if source == nil {
		return fallback
	}

	// Retrieve value from map
	retrievedValue, keyFound := source[fieldKey]
	if !keyFound {
		return fallback
	}

	// Verify boolean type
	booleanValue, isBoolean := retrievedValue.(bool)
	if !isBoolean {
		validationHelpersLog.Printf("Type mismatch for key %q: expected bool, found %T", fieldKey, retrievedValue)
		return fallback
	}

	return booleanValue
}

// getMapFieldAsInt retrieves an integer value from a configuration map with automatic numeric type conversion.
// This function handles the common pattern of extracting numeric config values that may be represented
// as various numeric types in YAML (int, int64, float64, uint64). It delegates to parseIntValue for
// the actual type conversion logic.
//
// Parameters:
//   - source: The configuration map to query
//   - fieldKey: The key to look up
//   - fallback: Value returned when extraction or conversion fails
//
// Example usage:
//
//	retentionDays := getMapFieldAsInt(config, "retention-days", 30)
//	if err := validateIntRange(retentionDays, 1, 90, "retention-days"); err != nil {
//	    return err
//	}
func getMapFieldAsInt(source map[string]any, fieldKey string, fallback int) int {
	// Guard against nil source
	if source == nil {
		return fallback
	}

	// Look up the value
	retrievedValue, keyFound := source[fieldKey]
	if !keyFound {
		return fallback
	}

	// Attempt numeric conversion using existing utility
	convertedInt, conversionOk := parseIntValue(retrievedValue)
	if !conversionOk {
		validationHelpersLog.Printf("Failed to convert key %q to int: got %T", fieldKey, retrievedValue)
		return fallback
	}

	return convertedInt
}

// dirExists verifies that a filesystem path exists and represents a directory.
// This consolidates directory existence checking patterns used throughout validation code.
// Returns false for empty paths, non-existent paths, or paths that reference files.
//
// Example usage:
//
//	if !dirExists(workflowDirectory) {
//	    return NewValidationError("directory", workflowDirectory, "directory not found", "create the directory")
//	}
func dirExists(targetPath string) bool {
	// Reject empty paths immediately
	if len(targetPath) == 0 {
		validationHelpersLog.Print("Directory check received empty path")
		return false
	}

	// Stat the path to check existence and type
	pathInfo, statError := os.Stat(targetPath)
	if statError != nil {
		validationHelpersLog.Printf("Directory check failed for %q: %v", targetPath, statError)
		return false
	}

	// Verify it's actually a directory
	isDirectory := pathInfo.IsDir()
	if !isDirectory {
		validationHelpersLog.Printf("Path %q exists but is not a directory", targetPath)
	}

	return isDirectory
}
