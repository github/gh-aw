// Package envutil provides utilities for reading and validating environment variables.
package envutil

import (
	"fmt"
	"os"
	"strconv"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

// GetIntFromEnv is a generic helper that reads an integer value from an environment variable,
// validates it against min/max bounds, and returns a default value if invalid.
// This follows the configuration helper pattern from pkg/workflow/config_helpers.go.
//
// Parameters:
//   - envVar: The environment variable name (e.g., "GH_AW_MAX_CONCURRENT_DOWNLOADS")
//   - defaultValue: The default value to return if env var is not set or invalid
//   - minValue: Minimum allowed value (inclusive)
//   - maxValue: Maximum allowed value (inclusive)
//   - log: Optional logger for debug output
//
// Returns the parsed integer value, or defaultValue if:
//   - Environment variable is not set
//   - Value cannot be parsed as an integer
//   - Value is outside the [minValue, maxValue] range
//
// Invalid values trigger warning messages to stderr.
func GetIntFromEnv(envVar string, defaultValue, minValue, maxValue int, log *logger.Logger) int {
	envValue := os.Getenv(envVar)
	if envValue == "" {
		return defaultValue
	}

	val, err := strconv.Atoi(envValue)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("Invalid %s value '%s' (must be a number), using default %d", envVar, envValue, defaultValue),
		))
		return defaultValue
	}

	if val < minValue || val > maxValue {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("%s value %d is out of bounds (must be %d-%d), using default %d", envVar, val, minValue, maxValue, defaultValue),
		))
		return defaultValue
	}

	if log != nil {
		log.Printf("Using %s=%d", envVar, val)
	}
	return val
}
