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
