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
//   - validateNoPreExpandedExperimentPlaceholders() - Validates that pre-expanded __GH_AW_EXPERIMENTS_*__ placeholders are not used in template conditions
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

	// preExpandedExperimentPattern matches the internal __GH_AW_EXPERIMENTS_*__ placeholder form
	// that is produced by the runtime and must never be written manually in workflow markdown.
	// Authors should use the experiments.<name> form (e.g. experiments.prompt_style == "detailed").
	preExpandedExperimentPattern = regexp.MustCompile(`__GH_AW_EXPERIMENTS_[A-Z0-9_]+__`)

	// experimentDoubleQuotePattern matches experiments.<name> comparison expressions that use
	// double-quoted string literals (e.g. experiments.mode == "value").  GitHub Actions
	// expression syntax only supports single-quoted string literals, so double quotes must be
	// replaced with single quotes before the expression reaches the lock file.
	experimentDoubleQuotePattern = regexp.MustCompile(`experiments\.[a-zA-Z_][a-zA-Z0-9_]*\s*(?:!==?|===?)\s*"[^"]*"`)

	// templateSeparatorPattern matches template block separator tags used by the markdown
	// renderer (render_template.cjs, template_branch.cjs). We warn when these separators
	// appear mid-line because inline placement is fragile and can lead to hard-to-debug
	// rendering behavior.
	//
	// Recognised forms (mirroring the renderer's grammar):
	//   {{#if <expr>}}              — opening tag
	//   {{#elseif <expr>}}  (and variants: #else-if, #else_if, elseif, else-if, else_if)
	//   {{#else}}                   — else branch (canonical; {{else}} without # is NOT rendered)
	//   {{#endif}}                  — primary closing tag
	//   {{/if}}                     — alternate closing tag
	//
	// The expression group reuses the same nested-expression grammar as TemplateIfPattern and
	// TemplateElseIfPattern so that conditions containing ${{ ... }} sub-expressions are
	// matched correctly (plain [^}]+ would stop at the inner brace).
	templateSeparatorPattern = regexp.MustCompile(`\{\{(?:#if\s+(?:\$\{\{[^\}]*\}\}|[^\}\{]|\{[^\{])*\s*|#?else[-_]?if\s+(?:\$\{\{[^\}]*\}\}|[^\}\{]|\{[^\{])*\s*|#else\s*|#endif\s*|/if\s*)\}\}`)
)

// validateNoIncludesInTemplateRegions checks that import directives
// are not used inside template conditional blocks ({{#if...}}{{/if}})
func validateNoIncludesInTemplateRegions(markdown string) error {
	templateValidationLog.Print("Validating that imports are not inside template regions")

	// Fast path: skip expensive regex if the markdown contains no {{#if blocks.
	// templateRegionPattern exclusively matches {{#if ... }}...{{/if}} constructs;
	// other handlebars-style tags (e.g. {{#unless}}) are not used in AWF workflows.
	if !strings.Contains(markdown, "{{#if") {
		return nil
	}

	// Use pre-compiled regex from package level for performance
	matches := (conditionalPattern{templateRegionPattern}).FindAllStringSubmatch(markdown, -1)
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

// validateNoPreExpandedExperimentPlaceholders checks that authors have not written the
// internal __GH_AW_EXPERIMENTS_*__ placeholder form directly in template conditions.
// This form is produced at runtime by the interpolation step and must never appear in
// workflow markdown source.  The correct form is experiments.<name> (optionally with a
// comparison, e.g. experiments.prompt_style == "detailed").
func validateNoPreExpandedExperimentPlaceholders(markdown string) error {
	templateValidationLog.Print("Validating that pre-expanded experiment placeholders are not used in template conditions")

	// Fast path: skip expensive regex if the markdown contains no template conditions.
	if !strings.Contains(markdown, "{{#") {
		return nil
	}

	// Collect conditions from both {{#if ...}} and all elseif variants
	ifConditions := (conditionalPattern{TemplateIfPattern}).FindAllStringSubmatch(markdown, -1)
	elseifConditions := (conditionalPattern{TemplateElseIfPattern}).FindAllStringSubmatch(markdown, -1)
	allConditions := append(ifConditions, elseifConditions...)
	templateValidationLog.Printf("Found %d template condition(s) to validate", len(allConditions))

	var errs []error
	for _, m := range allConditions {
		if len(m) < 2 {
			continue
		}
		condition := m[1]
		if preExpandedExperimentPattern.MatchString(condition) {
			errs = append(errs, fmt.Errorf(
				"pre-expanded experiment placeholder %q found in template condition %q: use experiments.<name> instead (e.g. experiments.prompt_style == \"detailed\")",
				preExpandedExperimentPattern.FindString(condition), condition,
			))
		}
	}

	if len(errs) > 0 {
		templateValidationLog.Printf("Found %d pre-expanded placeholder error(s)", len(errs))
		return errors.Join(errs...)
	}

	return nil
}

// detectDoubleQuotedExperimentComparisons scans template conditions for experiment comparison
// expressions that use double-quoted string literals (e.g. experiments.mode == "value").
// GitHub Actions expression syntax only supports single-quoted string literals, so double
// quotes must be replaced with single quotes (e.g. experiments.mode == 'value').
//
// The compiler converts double quotes to single quotes automatically, but callers should
// surface these findings as warnings so authors are prompted to fix the source.
//
// Returns one message per occurrence found, or nil if none.
func detectDoubleQuotedExperimentComparisons(markdown string) []string {
	templateValidationLog.Print("Checking for double-quoted experiment comparison expressions")

	// Fast path: skip expensive regex if the markdown contains no template conditions.
	if !strings.Contains(markdown, "{{#") {
		return nil
	}

	ifConditions := (conditionalPattern{TemplateIfPattern}).FindAllStringSubmatch(markdown, -1)
	elseifConditions := (conditionalPattern{TemplateElseIfPattern}).FindAllStringSubmatch(markdown, -1)
	allConditions := append(ifConditions, elseifConditions...)

	var warnings []string
	for _, m := range allConditions {
		if len(m) < 2 {
			continue
		}
		condition := strings.TrimSpace(m[1])
		if match := experimentDoubleQuotePattern.FindString(condition); match != "" {
			warnings = append(warnings, fmt.Sprintf(
				"experiment comparison expression uses double quotes: %q — "+
					"GitHub Actions expressions require single quotes; use single quotes instead "+
					"(e.g. experiments.name == 'value')",
				match,
			))
		}
	}

	templateValidationLog.Printf("Found %d double-quoted experiment comparison(s)", len(warnings))
	return warnings
}

// detectMidlineTemplateSeparators scans template block separator tags and returns warnings
// for lines where separators are embedded alongside other text.
//
// Example (warn): "4. {{#if cond}}A{{else}}B{{/if}}"
// Example (ok):   "{{#if cond}}" on its own line.
func detectMidlineTemplateSeparators(markdown string) []string {
	templateValidationLog.Print("Checking for mid-line template separators")

	if !strings.Contains(markdown, "{{") {
		return nil
	}

	lines := strings.Split(markdown, "\n")
	var warnings []string

	for i, line := range lines {
		matches := (conditionalPattern{templateSeparatorPattern}).FindAllStringIndex(line, -1)
		if len(matches) == 0 {
			continue
		}

		for _, match := range matches {
			separator := line[match[0]:match[1]]
			before := strings.TrimSpace(line[:match[0]])
			after := strings.TrimSpace(line[match[1]:])
			if before == "" && after == "" {
				continue
			}
			warnings = append(warnings, fmt.Sprintf(
				"template separator appears mid-line at line %d: %q — place template separators on their own lines",
				i+1, separator,
			))
		}
	}

	templateValidationLog.Printf("Found %d mid-line template separator warning(s)", len(warnings))
	return warnings
}
