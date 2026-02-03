// This file provides generic map and type conversion utilities.
//
// This file contains low-level helper functions for working with map[string]any
// structures and type conversions. These utilities are used throughout the workflow
// compilation process to safely parse and manipulate configuration data.
//
// # Organization Rationale
//
// These functions are grouped in a helper file because they:
//   - Provide generic, reusable utilities (used by 10+ files)
//   - Have no specific domain focus (work with any map/type data)
//   - Are small, stable functions (< 50 lines each)
//   - Follow clear, single-purpose patterns
//
// This follows the helper file conventions documented in skills/developer/SKILL.md.
//
// # Key Functions
//
// Type Conversion:
//   - parseIntValue() - Safely parse numeric types to int with truncation warnings
//
// Map Operations:
//   - filterMapKeys() - Create new map excluding specified keys
//
// These utilities handle common type conversion and map manipulation patterns that
// occur frequently during YAML-to-struct parsing and configuration processing.

package workflow

import "github.com/github/gh-aw/pkg/logger"

var mapHelpersLog = logger.New("workflow:map_helpers")

// parseIntValue safely parses various numeric types to int
// This is a common utility used across multiple parsing functions
func parseIntValue(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case uint64:
		// Check for overflow before converting uint64 to int
		const maxInt = int(^uint(0) >> 1)
		if v > uint64(maxInt) {
			mapHelpersLog.Printf("uint64 value %d exceeds max int value, returning 0", v)
			return 0, false
		}
		return int(v), true
	case float64:
		intVal := int(v)
		// Warn if truncation occurs (value has fractional part)
		if v != float64(intVal) {
			mapHelpersLog.Printf("Float value %.2f truncated to integer %d", v, intVal)
		}
		return intVal, true
	default:
		return 0, false
	}
}

// filterMapKeys creates a new map excluding the specified keys
func filterMapKeys(original map[string]any, excludeKeys ...string) map[string]any {
	excludeSet := make(map[string]bool)
	for _, key := range excludeKeys {
		excludeSet[key] = true
	}

	result := make(map[string]any)
	for key, value := range original {
		if !excludeSet[key] {
			result[key] = value
		}
	}
	return result
}
