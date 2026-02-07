// Package stringutil provides utility functions for working with strings.
package stringutil

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Truncate truncates a string to a maximum length, adding "..." if truncated.
// If maxLen is 3 or less, the string is truncated without "...".
//
// This is a general-purpose utility for truncating any string to a configurable
// length. For domain-specific workflow command identifiers with newline handling,
// see workflow.ShortenCommand instead.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// NormalizeWhitespace normalizes trailing whitespace and newlines to reduce spurious conflicts.
// It trims trailing whitespace from each line and ensures exactly one trailing newline.
func NormalizeWhitespace(content string) string {
	// Split into lines and trim trailing whitespace from each line
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}

	// Join back and ensure exactly one trailing newline if content is not empty
	normalized := strings.Join(lines, "\n")
	normalized = strings.TrimRight(normalized, "\n")
	if len(normalized) > 0 {
		normalized += "\n"
	}

	return normalized
}

// ParseVersionValue converts version values of various types to strings.
// Supports string, int, int64, uint64, and float64 types.
// Returns empty string for unsupported types.
func ParseVersionValue(version any) string {
	switch v := version.(type) {
	case string:
		return v
	case int, int64, uint64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%g", v)
	default:
		return ""
	}
}

// IsPositiveInteger checks if a string is a positive integer.
// Returns true for strings like "1", "123", "999" but false for:
//   - Zero ("0")
//   - Negative numbers ("-5")
//   - Numbers with leading zeros ("007")
//   - Floating point numbers ("3.14")
//   - Non-numeric strings ("abc")
//   - Empty strings ("")
func IsPositiveInteger(s string) bool {
	// Must not be empty
	if s == "" {
		return false
	}

	// Must not have leading zeros (except "0" itself, but that's not positive)
	if len(s) > 1 && s[0] == '0' {
		return false
	}

	// Must be numeric and > 0
	num, err := strconv.ParseInt(s, 10, 64)
	return err == nil && num > 0
}

// ansiEscapePattern matches ANSI escape sequences
// Pattern matches: ESC [ <optional params> <command letter>
// Examples: \x1b[0m, \x1b[31m, \x1b[1;32m
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSIEscapeCodes removes ANSI escape sequences from a string.
// This prevents terminal color codes and other control sequences from
// being accidentally included in generated files (e.g., YAML workflows).
//
// Common ANSI escape sequences that are removed:
//   - Color codes: \x1b[31m (red), \x1b[0m (reset)
//   - Text formatting: \x1b[1m (bold), \x1b[4m (underline)
//   - Cursor control: \x1b[2J (clear screen)
//
// Example:
//
//	input := "Hello \x1b[31mWorld\x1b[0m"  // "Hello [red]World[reset]"
//	output := StripANSIEscapeCodes(input)  // "Hello World"
//
// This function is particularly important for:
//   - Workflow descriptions copied from terminal output
//   - Comments in generated YAML files
//   - Any text that should be plain ASCII
func StripANSIEscapeCodes(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

// LevenshteinDistance calculates the Levenshtein distance between two strings.
// This is the minimum number of single-character edits (insertions, deletions,
// or substitutions) required to change one string into the other.
//
// The distance is useful for:
//   - Detecting typos in user input
//   - Finding similar strings for "did you mean" suggestions
//   - Fuzzy string matching
//
// Example:
//
//	LevenshteinDistance("copilot", "copiilot")   // Returns: 1 (one insertion)
//	LevenshteinDistance("claude", "claue")       // Returns: 1 (one deletion)
//	LevenshteinDistance("codex", "codec")        // Returns: 1 (one substitution)
//	LevenshteinDistance("abc", "xyz")            // Returns: 3 (three substitutions)
func LevenshteinDistance(s1, s2 string) int {
	len1, len2 := len(s1), len(s2)

	// Handle empty strings
	if len1 == 0 {
		return len2
	}
	if len2 == 0 {
		return len1
	}

	// Create a matrix to store intermediate distances
	// Using a 2D slice for clarity
	matrix := make([][]int, len1+1)
	for i := range matrix {
		matrix[i] = make([]int, len2+1)
	}

	// Initialize first row and column
	for i := 0; i <= len1; i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len2; j++ {
		matrix[0][j] = j
	}

	// Fill in the rest of the matrix
	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}

			// Minimum of:
			// - deletion: matrix[i-1][j] + 1
			// - insertion: matrix[i][j-1] + 1
			// - substitution: matrix[i-1][j-1] + cost
			deletion := matrix[i-1][j] + 1
			insertion := matrix[i][j-1] + 1
			substitution := matrix[i-1][j-1] + cost

			matrix[i][j] = min(deletion, min(insertion, substitution))
		}
	}

	return matrix[len1][len2]
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// FindClosestMatch finds the closest matching string from a list of valid options.
// It uses Levenshtein distance to determine similarity.
//
// Returns:
//   - The closest matching string if one is found within a reasonable distance
//   - Empty string if no good match is found
//
// Matching criteria:
//   - Distance must be <= 2 (max 2 character edits)
//   - Distance must be <= 40% of the longer string length
//   - If multiple matches have the same distance, returns the first one
//
// This function is useful for providing "did you mean" suggestions when
// users make typos in command-line arguments or configuration values.
//
// Example:
//
//	validEngines := []string{"copilot", "claude", "codex", "custom"}
//	FindClosestMatch("copiilot", validEngines)  // Returns: "copilot"
//	FindClosestMatch("claud", validEngines)     // Returns: "claude"
//	FindClosestMatch("xyz", validEngines)       // Returns: "" (no close match)
func FindClosestMatch(input string, validOptions []string) string {
	if len(validOptions) == 0 {
		return ""
	}

	minDistance := -1
	closestMatch := ""

	for _, option := range validOptions {
		distance := LevenshteinDistance(input, option)

		// Calculate maximum allowed distance (40% of longer string)
		maxLen := len(input)
		if len(option) > maxLen {
			maxLen = len(option)
		}
		maxAllowedDistance := (maxLen * 2) / 5 // 40% threshold

		// Only consider if distance is small enough
		if distance > 2 || distance > maxAllowedDistance {
			continue
		}

		// Update closest match if this is better
		if minDistance == -1 || distance < minDistance {
			minDistance = distance
			closestMatch = option
		}
	}

	return closestMatch
}
