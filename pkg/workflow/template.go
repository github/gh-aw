package workflow

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
)

var templateLog = logger.New("workflow:template")
var inlineSubAgentPattern = regexp.MustCompile("(?m)^##[ \t]+agent:[ \t]+`[a-z][a-z0-9_-]*`[ \t]*$")

// wrapExpressionsInTemplateConditionals transforms template conditionals by wrapping
// expressions in ${{ }}. For example:
// {{#if github.event.issue.number}} becomes {{#if ${{ github.event.issue.number }} }}
// {{#elseif github.actor}} becomes {{#elseif ${{ github.actor }} }}
func wrapExpressionsInTemplateConditionals(markdown string) string {
	templateLog.Print("Wrapping expressions in template conditionals")

	// wrapTagExpr applies the wrapping logic to a single extracted expression and
	// returns the full reconstructed tag. re must be the same regex that produced
	// match (so FindStringSubmatch reliably extracts capture group 1 = the expression).
	// prefix is the canonical opening tag text without the expression
	// (e.g. "{{#if " or "{{#elseif "), used to rebuild the output tag.
	wrapTagExpr := func(re *regexp.Regexp, match, prefix string) string {
		submatches := re.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		expr := strings.TrimSpace(submatches[1])

		// Empty expressions are treated as false and wrapped as such
		if expr == "" {
			templateLog.Print("Empty expression detected, wrapping as false")
			return prefix + "${{ false }} }}"
		}

		// Already wrapped in ${{ ... }} — return as-is
		if strings.HasPrefix(expr, "${{") {
			templateLog.Print("Expression already wrapped, skipping")
			return match
		}

		// Environment variable reference (starts with ${) — already evaluated
		if strings.HasPrefix(expr, "${") {
			templateLog.Print("Environment variable reference detected, skipping wrap")
			return match
		}

		// Placeholder reference (starts with __) — substituted at runtime
		if strings.HasPrefix(expr, "__") {
			templateLog.Print("Placeholder reference detected, skipping wrap")
			return match
		}

		templateLog.Printf("Wrapping expression: %s", expr)
		return prefix + "${{ " + expr + " }} }}"
	}

	// Process {{#if ...}} tags
	result := TemplateIfPattern.ReplaceAllStringFunc(markdown, func(match string) string {
		return wrapTagExpr(TemplateIfPattern, match, "{{#if ")
	})

	// Process all elseif variant tags — normalise to canonical {{#elseif ...}} form after wrapping
	result = TemplateElseIfPattern.ReplaceAllStringFunc(result, func(match string) string {
		submatches := TemplateElseIfPattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}
		wrapped := wrapTagExpr(TemplateElseIfPattern, match, "{{#elseif ")
		// If the original tag used a non-canonical form (else-if / else_if / without #),
		// the prefix replacement above already normalises it to {{#elseif ...}}.
		return wrapped
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
//   - hasRuntimeImports: True when any prompt chunk contains a {{#runtime-import}} macro that must
//     be resolved by interpolate_prompt.cjs at runtime. Pass true whenever the compiled prompt
//     includes such macros (always the case in non-inline compilation mode).
//
// The generated step:
//   - Uses actions/github-script action
//   - Sets GH_AW_PROMPT environment variable to the prompt file path
//   - Sets GH_AW_EXPR_* environment variables with the actual GitHub expressions (${{ ... }})
//   - Runs interpolate_prompt.cjs script to replace placeholders and render template conditionals
func (c *Compiler) generateInterpolationAndTemplateStep(yaml *strings.Builder, expressionMappings []*ExpressionMapping, data *WorkflowData, hasRuntimeImports bool) {
	// Check if we need interpolation
	hasExpressions := len(expressionMappings) > 0

	// Check if we need template rendering
	hasTemplatePattern := strings.Contains(data.MarkdownContent, "{{#if ")
	hasGitHubContext := hasGitHubTool(data.ParsedTools)
	hasInlineSubAgents := inlineSubAgentPattern.MatchString(data.MarkdownContent)
	// hasRuntimeImports is true when the compiled prompt contains {{#runtime-import}} macros that
	// must be resolved by interpolate_prompt.cjs. The compiler always emits a self-import macro in
	// non-inline mode, so this is true even when the markdown source contains no explicit macros.
	hasTemplates := hasTemplatePattern || hasGitHubContext || hasInlineSubAgents || hasRuntimeImports

	// Skip if neither interpolation nor template rendering is needed
	if !hasExpressions && !hasTemplates {
		templateLog.Print("No interpolation or template rendering needed, skipping step generation")
		return
	}

	templateLog.Printf("Generating interpolation and template step: expressions=%d, hasPattern=%v, hasGitHubContext=%v, hasInlineSubAgents=%v, hasRuntimeImports=%v",
		len(expressionMappings), hasTemplatePattern, hasGitHubContext, hasInlineSubAgents, hasRuntimeImports)

	yaml.WriteString("      - name: Interpolate variables and render templates\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        env:\n")
	fmt.Fprintf(yaml, "          GH_AW_PROMPT: %s\n", constants.AwPromptsFileExpr)
	if data.EngineConfig != nil && data.EngineConfig.ID != "" {
		fmt.Fprintf(yaml, "          GH_AW_ENGINE_ID: \"%s\"\n", data.EngineConfig.ID)
	}
	engineID := resolveActivationEngineID(data)
	baseDir := engineConfigBaseDirForRegistry(c.engineRegistry, engineID)
	yaml.WriteString(formatYAMLEnv("          ", "GH_AW_SUB_AGENT_DIR", path.Join(baseDir, "agents")))
	yaml.WriteString(formatYAMLEnv("          ", "GH_AW_SUB_AGENT_EXT", parser.GetEngineSubAgentExt(engineID)))
	yaml.WriteString(formatYAMLEnv("          ", "GH_AW_SKILL_DIR", path.Join(baseDir, "skills")))
	yaml.WriteString(formatYAMLEnv("          ", "GH_AW_SKILL_EXT", engineInlineSkillExt(engineID)))

	// Add environment variables for extracted expressions (deduplicated by EnvVar)
	seen := make(map[string]struct{})
	for _, mapping := range expressionMappings {
		if setutil.Contains(seen, mapping.EnvVar) {
			continue
		}
		seen[mapping.EnvVar] = struct{}{}

		// Write the environment variable with the original GitHub expression
		fmt.Fprintf(yaml, "          %s: ${{ %s }}\n", mapping.EnvVar, mapping.Content)
	}

	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Load interpolate_prompt script from external file
	// Use setup_globals helper to store GitHub Actions objects in global scope
	yaml.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/interpolate_prompt.cjs');\n")
	yaml.WriteString("            await main();\n")
}
