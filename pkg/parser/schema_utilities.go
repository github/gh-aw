package parser

import (
	"github.com/github/gh-aw/pkg/constants"
)

// filterIgnoredFields removes ignored fields from frontmatter without warnings
// NOTE: This function is kept for backward compatibility but currently does nothing
// as all previously ignored fields (description, applyTo) are now validated by the schema
func filterIgnoredFields(frontmatter map[string]any) map[string]any {
	if frontmatter == nil {
		return nil
	}

	// Check if there are any ignored fields configured
	if len(constants.IgnoredFrontmatterFields) == 0 {
		// No fields to filter, return as-is
		return frontmatter
	}

	// Create a copy of the frontmatter map without ignored fields
	filtered := make(map[string]any)
	for key, value := range frontmatter {
		// Skip ignored fields
		ignored := false
		for _, ignoredField := range constants.IgnoredFrontmatterFields {
			if key == ignoredField {
				ignored = true
				break
			}
		}
		if !ignored {
			filtered[key] = value
		}
	}

	return filtered
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// removeDuplicates removes duplicate strings from a slice
func removeDuplicates(strings []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, str := range strings {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}
