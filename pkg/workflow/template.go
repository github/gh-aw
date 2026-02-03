package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var templateLog = logger.New("workflow:template")

// wrapExpressionsInTemplateConditionals transforms template conditionals by wrapping
// expressions in ${{ }}. For example:
// {{#if github.event.issue.number}} becomes {{#if ${{ github.event.issue.number }} }}
func wrapExpressionsInTemplateConditionals(markdown string) string {
	// Pattern to match {{#if expression}} where expression is not already wrapped in ${{ }}
	// This regex captures the entire {{#if ...}} block and handles nested }} within ${{ }} expressions
	// The pattern (?:\$\{\{[^\}]*\}\}|[^\}])* matches either:
	//   - A complete ${{ ... }} expression (where ... has no })
	//   - OR any character that's not }
	// This allows matching ${{ github.event.issue.number }} as a unit while stopping at the
	// closing }} of the conditional
	re := regexp.MustCompile(`\{\{#if\s+((?:\$\{\{[^\}]*\}\}|[^\}])*)\s*\}\}`)

	templateLog.Print("Wrapping expressions in template conditionals")

	result := re.ReplaceAllStringFunc(markdown, func(match string) string {
		// Extract the expression part (everything between "{{#if " and "}}")
		submatches := re.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		expr := strings.TrimSpace(submatches[1])

		// Check if expression is empty (after trimming)
		// Empty expressions are treated as false and wrapped as such
		if expr == "" {
			templateLog.Print("Empty expression detected, wrapping as false")
			return "{{#if ${{ false }} }}"
		}

		// Check if expression is already wrapped in ${{ ... }}
		// Look for the pattern starting with "${{"
		if strings.HasPrefix(expr, "${{") {
			templateLog.Print("Expression already wrapped, skipping")
			return match // Already wrapped, return as-is
		}

		// Check if expression is an environment variable reference (starts with ${)
		// These don't need ${{ }} wrapping as they're already evaluated
		if strings.HasPrefix(expr, "${") {
			templateLog.Print("Environment variable reference detected, skipping wrap")
			return match // Environment variable reference, return as-is
		}

		// Check if expression is a placeholder reference (starts with __)
		// These are substituted with sed and don't need ${{ }} wrapping
		if strings.HasPrefix(expr, "__") {
			templateLog.Print("Placeholder reference detected, skipping wrap")
			return match // Placeholder reference, return as-is
		}

		// Always wrap expressions that don't start with ${{ or ${ or __
		templateLog.Printf("Wrapping expression: %s", expr)
		return "{{#if ${{ " + expr + " }} }}"
	})

	return result
}

// generateInterpolationAndTemplateStep generates a step that interpolates GitHub expression variables
// and renders template conditionals in the prompt file.
// This combines both variable interpolation and template filtering into a single step.
//
// Parameters:
//   - yaml: The string builder to write the YAML to
//   - expressionMappings: Array of ExpressionMapping containing the mappings between placeholders and GitHub expressions
//   - data: WorkflowData containing markdown content and parsed tools
//
// The generated step:
//   - Uses actions/github-script action
//   - Sets GH_AW_PROMPT environment variable to the prompt file path
//   - Sets GH_AW_EXPR_* environment variables with the actual GitHub expressions (${{ ... }})
//   - Runs interpolate_prompt.cjs script to replace placeholders and render template conditionals
func (c *Compiler) generateInterpolationAndTemplateStep(yaml *strings.Builder, expressionMappings []*ExpressionMapping, data *WorkflowData) {
	// Check if we need interpolation
	hasExpressions := len(expressionMappings) > 0

	// Check if we need template rendering
	hasTemplatePattern := strings.Contains(data.MarkdownContent, "{{#if ")
	hasGitHubContext := hasGitHubTool(data.ParsedTools)
	hasTemplates := hasTemplatePattern || hasGitHubContext

	// Skip if neither interpolation nor template rendering is needed
	if !hasExpressions && !hasTemplates {
		templateLog.Print("No interpolation or template rendering needed, skipping step generation")
		return
	}

	templateLog.Printf("Generating interpolation and template step: expressions=%d, hasPattern=%v, hasGitHubContext=%v",
		len(expressionMappings), hasTemplatePattern, hasGitHubContext)

	yaml.WriteString("      - name: Interpolate variables and render templates\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/github-script"))
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt\n")

	// Add environment variables for extracted expressions
	for _, mapping := range expressionMappings {
		// Write the environment variable with the original GitHub expression
		fmt.Fprintf(yaml, "          %s: ${{ %s }}\n", mapping.EnvVar, mapping.Content)
	}

	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Load interpolate_prompt script from external file
	// Use setup_globals helper to store GitHub Actions objects in global scope
	yaml.WriteString("            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io);\n")
	yaml.WriteString("            const { main } = require('/opt/gh-aw/actions/interpolate_prompt.cjs');\n")
	yaml.WriteString("            await main();\n")
}
