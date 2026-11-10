//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGenerateInterpolationAndTemplateStep_DeduplicatesEnvVars tests that duplicate
// expression mappings (same EnvVar) are only emitted once in the env section,
// preventing runtime errors when import and main workflow reference the same variable.
func TestGenerateInterpolationAndTemplateStep_DeduplicatesEnvVars(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{
		MarkdownContent: "hello",
		ParsedTools:     NewTools(map[string]any{}),
	}

	// Simulate the same variable referenced in both imported content and main workflow
	expressionMappings := []*ExpressionMapping{
		{EnvVar: "GH_AW_VARS_MY_VAR", Content: "vars.MY_VAR"},
		{EnvVar: "GH_AW_VARS_MY_VAR", Content: "vars.MY_VAR"}, // duplicate
		{EnvVar: "GH_AW_VARS_OTHER_VAR", Content: "vars.OTHER_VAR"},
	}

	var yaml strings.Builder
	compiler.generateInterpolationAndTemplateStep(&yaml, expressionMappings, data, false)

	result := yaml.String()

	// MY_VAR should appear exactly once
	count := strings.Count(result, `GH_AW_VARS_MY_VAR: "${{ vars.MY_VAR }}"`)
	assert.Equal(t, 1, count, "duplicate env var GH_AW_VARS_MY_VAR should appear exactly once")

	// OTHER_VAR should be present
	assert.Contains(t, result, `GH_AW_VARS_OTHER_VAR: "${{ vars.OTHER_VAR }}"`, "OTHER_VAR should be present")
}

// TestGenerateInterpolationAndTemplateStep_SkipPath tests that the step is not generated
// when there are no expression mappings, no template patterns, no GitHub context tool, and
// no runtime-import macros (i.e. inline/Wasm compilation mode where no self-import is emitted).
func TestGenerateInterpolationAndTemplateStep_SkipPath(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{
		MarkdownContent: "hello world",
		ParsedTools:     NewTools(map[string]any{}),
	}

	var yaml strings.Builder
	compiler.generateInterpolationAndTemplateStep(&yaml, nil, data, false)

	assert.Empty(t, yaml.String(), "step YAML should be empty when MarkdownContent has no expressions or template patterns")
}

// TestGenerateInterpolationAndTemplateStep_GeneratePath tests that the step is generated
// when the markdown content contains a template pattern.
func TestGenerateInterpolationAndTemplateStep_GeneratePath(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{
		MarkdownContent: "{{#if github.event.issue.number}}do something{{/if}}",
		ParsedTools:     NewTools(map[string]any{}),
	}

	var yaml strings.Builder
	compiler.generateInterpolationAndTemplateStep(&yaml, nil, data, false)

	result := yaml.String()
	assert.Contains(t, result, "Interpolate variables and render templates", "step name should be present when template patterns exist")
	assert.Contains(t, result, "GH_AW_PROMPT:", "prompt env var should be set when step is generated")
	assert.Contains(t, result, "interpolate_prompt.cjs", "interpolate_prompt script should be referenced in the step")
	assert.Contains(t, result, "setupGlobals", "setupGlobals helper should be called to initialise GitHub Actions objects")
}

// TestGenerateInterpolationAndTemplateStep_WithInlineSubAgent ensures inline sub-agent
// workflows still run interpolate_prompt.cjs even when github context templates are absent.
// ParsedTools uses an empty map (no github key) so hasGitHubTool returns false,
// confirming that the inline sub-agent marker alone is sufficient to trigger the step.
func TestGenerateInterpolationAndTemplateStep_WithInlineSubAgent(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{
		MarkdownContent: "Main prompt\n\n## agent: `planner`\nDo planning.",
		ParsedTools:     NewTools(map[string]any{}),
	}

	var yaml strings.Builder
	compiler.generateInterpolationAndTemplateStep(&yaml, nil, data, false)

	result := yaml.String()
	assert.Contains(t, result, "Interpolate variables and render templates", "step should be present when inline sub-agents are defined")
	assert.Contains(t, result, "interpolate_prompt.cjs", "interpolate_prompt script should run to extract inline sub-agents")
}

func TestGenerateInterpolationAndTemplateStep_WithPreservedDefinitions(t *testing.T) {
	for _, kind := range []string{"agent", "skill"} {
		t.Run(kind, func(t *testing.T) {
			compiler := &Compiler{}
			data := &WorkflowData{
				MarkdownContent: "Main prompt",
				PromptMarkdown:  "Main prompt\n\n## " + kind + ": `helper`\nHelp.",
				ParsedTools:     NewTools(map[string]any{}),
			}
			var yaml strings.Builder
			compiler.generateInterpolationAndTemplateStep(&yaml, nil, data, false)
			assert.Contains(t, yaml.String(), "interpolate_prompt.cjs", "a preserved definition alone must trigger extraction")
		})
	}
}

// TestGenerateInterpolationAndTemplateStep_WithRuntimeImport ensures the interpolation step
// is emitted whenever the compiled prompt contains a {{#runtime-import}} macro, even when
// the markdown body has no expressions, no {{#if}} patterns, and github is disabled.
// This is the scenario from the bug report: github: false + no expressions = skipped step,
// but the compiler always emits a self-import macro that requires interpolate_prompt.cjs.
func TestGenerateInterpolationAndTemplateStep_WithRuntimeImport(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{
		// Plain body with no expressions, no {{#if}}, no github tool — previously caused the step
		// to be skipped even though the compiled prompt always contains a runtime-import macro.
		MarkdownContent: "Do some work.",
		ParsedTools:     NewTools(map[string]any{}),
	}

	var yaml strings.Builder
	compiler.generateInterpolationAndTemplateStep(&yaml, nil, data, true)

	result := yaml.String()
	assert.Contains(t, result, "Interpolate variables and render templates",
		"step must be emitted when runtime-import macros are present so they are resolved at runtime")
	assert.Contains(t, result, "interpolate_prompt.cjs",
		"interpolate_prompt script must be referenced to resolve {{#runtime-import}} macros")
}
