// This file provides utilities for compiling agentic workflows into GitHub Actions YAML.
//
// This file contains YAML marshaling utilities with deterministic field ordering,
// which is essential for GitHub Actions workflows to maintain readability and consistency.
//
// # Why Field Ordering Matters
//
// GitHub Actions workflows follow conventional field ordering that improves readability
// and aligns with GitHub's official documentation. While YAML and GitHub Actions don't
// technically require any specific field order, maintaining consistent ordering provides
// several benefits:
//
//   - Readability: Developers expect to see "name" first, followed by "on", then "jobs"
//   - Consistency: All workflows in a repository follow the same structure
//   - Diff-friendliness: Deterministic ordering makes git diffs more meaningful
//   - Maintainability: Easier to locate and update specific fields
//
// # The MapSlice Solution
//
// Go's built-in map type uses random iteration order for security reasons, which means
// marshaling a map[string]any to YAML produces non-deterministic field ordering.
// The goccy/go-yaml library provides yaml.MapSlice, which maintains insertion order:
//
//   - MapSlice is a slice of MapItem structs, each containing a Key and Value
//   - Fields appear in YAML in the exact order they're appended to the MapSlice
//   - This gives us full control over field ordering during marshaling
//
// # GitHub Actions Field Order Conventions
//
// This package follows GitHub Actions' conventional field ordering:
//
// Workflow-level fields (top to bottom):
//   - name: Workflow name (always first for easy identification)
//   - on: Trigger events (defines when the workflow runs)
//   - permissions: Workflow-level permissions
//   - env: Workflow-level environment variables
//   - defaults: Default settings for all jobs
//   - concurrency: Concurrency control settings
//   - jobs: Job definitions (the main content)
//
// Job-level fields (within each job):
//   - name: Job name
//   - runs-on: Runner specification
//   - permissions: Job-level permissions
//   - environment: Deployment environment
//   - if: Conditional execution
//   - needs: Job dependencies
//   - env: Job-level environment variables
//   - steps: Step definitions (the job's tasks)
//
// Step-level fields (within each step):
//   - name: Step name
//   - id: Step identifier
//   - if: Conditional execution
//   - uses: Action reference (for action steps)
//   - run: Command to run (for shell steps)
//   - with: Action inputs
//   - env: Step-level environment variables
//
// Event-specific and other nested fields are typically ordered alphabetically
// when no specific convention applies.
//
// # When to Use Ordered vs. Unordered Maps
//
// Use OrderMapFields/MarshalWithFieldOrder when:
//   - Creating top-level workflow structure (name, on, jobs)
//   - Defining job configurations
//   - Building step definitions
//   - Generating user-facing YAML that should be readable
//
// Use regular map marshaling when:
//   - Field order doesn't matter for readability
//   - Working with internal data structures not marshaled to YAML
//   - Performance is critical and ordering isn't needed
//
// # References
//
// GitHub Actions workflow syntax:
// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions
//
// GitHub Actions best practices:
// https://docs.github.com/en/actions/learn-github-actions/usage-limits-billing-and-administration

package workflow

import (
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var yamlLog = logger.New("workflow:yaml")

// UnquoteYAMLKey removes quotes from a YAML key at the start of a line.
//
// The YAML marshaler automatically adds quotes around YAML reserved words and keywords
// to prevent parsing ambiguity. For example, the word "on" is a YAML boolean value,
// so the marshaler outputs it as "on": to distinguish it from the boolean.
//
// However, GitHub Actions uses "on" as a top-level key for workflow triggers, and
// the Actions UI and documentation always show it unquoted. This function removes
// those quotes to match the conventional GitHub Actions syntax.
//
// The function only replaces quoted keys at the start of a line (optionally preceded
// by whitespace) to avoid incorrectly unquoting string values that happen to contain
// the same text. For example, description: "This is about on: something" would be
// left unchanged.
//
// Parameters:
//   - yamlStr: The YAML string to process
//   - key: The key to unquote (e.g., "on", "if", "true", "false")
//
// Returns:
//   - The YAML string with the specified key unquoted at line starts
//
// Example:
//
//	input := "\"on\":\n  push:\n    branches:\n      - main"
//	result := UnquoteYAMLKey(input, "on")
//	// result: "on:\n  push:\n    branches:\n      - main"
func UnquoteYAMLKey(yamlStr string, key string) string {
	yamlLog.Printf("Unquoting YAML key: %s", key)

	// Create a regex pattern that matches the quoted key at the start of a line
	// Pattern: (start of line or newline) + (optional whitespace) + quoted key + colon
	pattern := `(^|\n)([ \t]*)"` + regexp.QuoteMeta(key) + `":`

	// Replacement: keep the line start and whitespace, but remove quotes from the key
	// Need to use ReplaceAllStringFunc to properly construct the replacement
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllStringFunc(yamlStr, func(match string) string {
		// Find the submatch groups
		submatches := re.FindStringSubmatch(match)
		if len(submatches) >= 3 {
			// submatches[0] is the full match
			// submatches[1] is the line start (^ or \n)
			// submatches[2] is the whitespace
			return submatches[1] + submatches[2] + key + ":"
		}
		return match
	})
}

// MarshalWithFieldOrder marshals a map to YAML with fields in a specific order.
//
// This function ensures deterministic field ordering in the generated YAML by using
// yaml.MapSlice internally. Priority fields are emitted first in the order specified,
// then remaining fields are emitted alphabetically.
//
// This is the primary function used throughout the workflow compiler to generate
// user-facing YAML for GitHub Actions workflows. It ensures workflows follow the
// conventional GitHub Actions field ordering, making them more readable and consistent
// with GitHub's documentation.
//
// Parameters:
//   - data: A map of field names to values to marshal
//   - priorityFields: A slice of field names that should appear first, in order
//
// Returns:
//   - The marshaled YAML bytes with deterministic field ordering
//   - An error if marshaling fails
//
// The function uses the following YAML marshaling options:
//   - 2-space indentation (GitHub Actions standard)
//   - Literal block scalars for multiline strings (improves readability)
//
// Example:
//
//	data := map[string]any{
//	    "jobs": map[string]any{...},
//	    "name": "My Workflow",
//	    "on": map[string]any{...},
//	    "permissions": map[string]any{...},
//	}
//	// Generate YAML with conventional field order: name, on, permissions, jobs
//	yaml, err := MarshalWithFieldOrder(data, []string{"name", "on", "permissions", "jobs"})
//	// Result:
//	// name: My Workflow
//	// on: ...
//	// permissions: ...
//	// jobs: ...
func MarshalWithFieldOrder(data map[string]any, priorityFields []string) ([]byte, error) {
	yamlLog.Printf("Marshaling YAML with field order: %d priority fields", len(priorityFields))

	// Convert the map to an ordered MapSlice structure
	orderedData := OrderMapFields(data, priorityFields)

	// Marshal the ordered data with proper options for GitHub Actions
	return yaml.MarshalWithOptions(orderedData, DefaultMarshalOptions...)
}

// OrderMapFields converts a map to yaml.MapSlice with fields in a specific order.
//
// yaml.MapSlice is a slice of key-value pairs that preserves insertion order during
// YAML marshaling. This function creates a MapSlice from a regular Go map, ensuring
// fields appear in a deterministic order.
//
// The ordering strategy is:
//  1. Priority fields are added first, in the exact order specified
//  2. Remaining fields are added alphabetically
//
// This function is useful when you need the MapSlice structure directly (e.g., for
// nested structures or when you need to add more items to the slice). For simple
// marshaling to YAML bytes, use MarshalWithFieldOrder instead.
//
// Parameters:
//   - data: A map of field names to values to order
//   - priorityFields: Field names that should appear first, in order. If empty,
//     all fields are sorted alphabetically.
//
// Returns:
//   - A yaml.MapSlice with fields in the specified order
//
// Example use case - Ordering job step fields:
//
//	step := map[string]any{
//	    "env": map[string]string{"FOO": "bar"},
//	    "name": "Build project",
//	    "run": "make build",
//	}
//	// Order step fields: name, run, env (conventional GitHub Actions order)
//	orderedStep := OrderMapFields(step, []string{"name", "id", "if", "uses", "run", "with", "env"})
//	// orderedStep will have: name, run, env (only fields that exist)
//
// Example use case - Alphabetical ordering:
//
//	permissions := map[string]any{
//	    "issues": "write",
//	    "contents": "read",
//	    "pull-requests": "write",
//	}
//	// Order alphabetically by passing empty priority list
//	orderedPerms := OrderMapFields(permissions, []string{})
//	// orderedPerms will have: contents, issues, pull-requests
func OrderMapFields(data map[string]any, priorityFields []string) yaml.MapSlice {
	var orderedData yaml.MapSlice

	// Phase 1: Add priority fields in the specified order
	// This ensures important fields like "name", "on", "jobs" appear first
	for _, fieldName := range priorityFields {
		if value, exists := data[fieldName]; exists {
			orderedData = append(orderedData, yaml.MapItem{Key: fieldName, Value: value})
		}
	}

	// Phase 2: Collect remaining fields (those not in priority list)
	var remainingKeys []string
	for key := range data {
		// Skip if it's already been added as a priority field
		isPriority := false
		for _, priorityField := range priorityFields {
			if key == priorityField {
				isPriority = true
				break
			}
		}
		if !isPriority {
			remainingKeys = append(remainingKeys, key)
		}
	}

	// Phase 3: Sort remaining keys alphabetically
	// This ensures deterministic ordering for fields not in the priority list
	sort.Strings(remainingKeys)

	// Phase 4: Add remaining fields to the ordered map
	for _, key := range remainingKeys {
		orderedData = append(orderedData, yaml.MapItem{Key: key, Value: data[key]})
	}

	return orderedData
}

// CleanYAMLNullValues removes " null" from YAML key-value pairs where the value is null.
//
// GitHub Actions YAML treats workflow_dispatch: and workflow_dispatch: null identically,
// but the former is more concise and matches GitHub's documentation style.
// This function post-processes YAML strings to convert `: null` to `:` for better readability.
//
// The function only replaces null values at the end of lines (after a colon and optional whitespace)
// to avoid incorrectly modifying string values that contain the word "null".
//
// Parameters:
//   - yamlStr: The YAML string to process
//
// Returns:
//   - The YAML string with `: null` replaced by `:`
//
// Example:
//
//	input := "on:\n  workflow_dispatch: null\n  schedule:\n    - cron: '0 0 * * *'"
//	result := CleanYAMLNullValues(input)
//	// result: "on:\n  workflow_dispatch:\n  schedule:\n    - cron: '0 0 * * *'"
func CleanYAMLNullValues(yamlStr string) string {
	yamlLog.Print("Cleaning null values from YAML")

	// Create a regex pattern that matches `: null` at the end of a line
	// Pattern: colon + optional whitespace + "null" + optional whitespace + end of line
	pattern := regexp.MustCompile(`:\s*null\s*$`)

	// Split into lines, process each line, and rejoin
	lines := strings.Split(yamlStr, "\n")
	for i, line := range lines {
		lines[i] = pattern.ReplaceAllString(line, ":")
	}

	return strings.Join(lines, "\n")
}
