// This file provides template structure validation for agentic workflows.
//
// # Template Validation
//
// This file validates template conditionals and their interaction with other workflow features.
// It ensures that import directives and template regions don't conflict.
//
// # Validation Functions
//
//   - validateNoIncludesInTemplateRegions() - Validates that imports are not inside template blocks
//
// # Validation Pattern: Structure Validation
//
// Template validation uses structure checking:
//   - Parses template conditional blocks ({{#if...}}{{/if}})
//   - Checks for import directives within template regions
//   - Prevents import processing conflicts with template rendering
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates template structure or syntax
//   - It checks template conditional blocks
//   - It validates template-related features
//   - It ensures template compatibility with other features
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var templateValidationLog = logger.New("workflow:template_validation")

// Pre-compiled regexes for performance (avoid recompilation in hot paths)
var (
	// templateRegionPattern matches template conditional blocks with their content
	// Uses (?s) for dotall mode, .*? (non-greedy) with \s* to handle expressions with or without trailing spaces
	templateRegionPattern = regexp.MustCompile(`(?s)\{\{#if\s+.*?\s*\}\}(.*?)\{\{/if\}\}`)
)

// validateNoIncludesInTemplateRegions checks that import directives
// are not used inside template conditional blocks ({{#if...}}{{/if}})
func validateNoIncludesInTemplateRegions(markdown string) error {
	templateValidationLog.Print("Validating that imports are not inside template regions")

	// Use pre-compiled regex from package level for performance
	matches := templateRegionPattern.FindAllStringSubmatch(markdown, -1)
	templateValidationLog.Printf("Found %d template regions to validate", len(matches))

	// Collect all validation errors
	var errs []error

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		// Check the content inside the template region (capture group 1)
		regionContent := match[1]

		// Check for import directives in this region
		lines := strings.Split(regionContent, "\n")
		for lineNum, line := range lines {
			// Trim leading/trailing whitespace before checking
			trimmedLine := strings.TrimSpace(line)
			directive := parser.ParseImportDirective(trimmedLine)
			if directive != nil {
				importErr := fmt.Errorf("import directives cannot be used inside template regions ({{#if...}}{{/if}}): found '%s' at line %d within template block", directive.Original, lineNum+1)
				errs = append(errs, importErr)
			}
		}
	}

	// Return aggregated errors
	if len(errs) > 0 {
		templateValidationLog.Printf("Found %d template validation errors", len(errs))
		return errors.Join(errs...)
	}

	return nil
}
