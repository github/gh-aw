//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPromptRenderConfig struct {
	Items []struct {
		ContentEnvVar   string `json:"content_env"`
		File            string `json:"file"`
		ConditionEnvVar string `json:"condition_env"`
	} `json:"items"`
}

func promptRenderPlan(t *testing.T, output string) string {
	t.Helper()

	const configPrefix = "GH_AW_PROMPT_CONFIG: "
	configStart := strings.Index(output, configPrefix)
	require.NotEqual(t, -1, configStart, "Prompt render config should be present")
	configLine := output[configStart+len(configPrefix):]
	configLine, _, _ = strings.Cut(configLine, "\n")
	configJSON, err := strconv.Unquote(configLine)
	require.NoError(t, err)

	var config testPromptRenderConfig
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))

	var plan strings.Builder
	for _, item := range config.Items {
		if item.File != "" {
			plan.WriteString(item.File)
			plan.WriteByte('\n')
			continue
		}
		prefix := item.ContentEnvVar + ": "
		valueStart := strings.Index(output, prefix)
		require.NotEqual(t, -1, valueStart, "Prompt content variable %s should be present", item.ContentEnvVar)
		valueLine := output[valueStart+len(prefix):]
		valueLine, _, _ = strings.Cut(valueLine, "\n")
		value, unquoteErr := strconv.Unquote(valueLine)
		require.NoError(t, unquoteErr)
		plan.WriteString(value)
	}
	return plan.String()
}

func TestGenerateUnifiedPromptCreationStep_TreatsUserContentAsData(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{ParsedTools: NewTools(map[string]any{})}
	payloads := []string{
		"$(touch /tmp/command-substitution)",
		"`touch /tmp/backtick`",
		"${PATH}; rm -rf /",
		"${{ github.event.issue.title }}",
		"\"; process.exit(1); //",
		"'; require('child_process').execSync('false'); //",
		"line one\nGH_AW_PROMPT_deadbeefdeadbeef_EOF\ntouch /tmp/heredoc\nline four",
		"workflow command ::set-output name=x::value",
		"yaml\n        run: injected\n      - uses: attacker/action@main",
		"Unicode: 雪man ☃️ café",
	}

	var output strings.Builder
	compiler.generateUnifiedPromptCreationStep(&output, nil, payloads, nil, data)
	generated := output.String()

	assert.NotRegexp(t, `(?m)^        run:`, generated, "Prompt creation must not emit a shell step")
	assert.NotContains(t, generated, "cat <<", "Prompt creation must not emit heredocs")
	assert.NotContains(t, generated, "create_prompt_first.sh")
	assert.Contains(t, generated, "create_prompt.cjs")
	assert.Contains(t, generated, "await main(core)")

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				Env  map[string]string `yaml:"env"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("jobs:\n  test:\n    steps:\n"+generated), &workflow))
	require.Len(t, workflow.Jobs["test"].Steps, 1)
	step := workflow.Jobs["test"].Steps[0]
	assert.Contains(t, step.Uses, "actions/github-script@")

	script := step.With["script"]
	assert.NotContains(t, script, "${{", "GitHub expressions must not enter JavaScript source")
	assert.Equal(t, "${{ runner.temp }}/gh-aw/actions", step.Env["GH_AW_ACTIONS_DIR"])
	assert.Equal(t, "${{ runner.temp }}/gh-aw/aw-prompts/prompt.txt", step.Env["GH_AW_PROMPT"])
	for _, payload := range payloads {
		assert.NotContains(t, script, payload, "User-controlled content must not enter JavaScript source")
	}

	var config struct {
		Items []struct {
			ContentEnv string `json:"content_env"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_PROMPT_CONFIG"]), &config))
	require.Len(t, config.Items, len(payloads))
	for i, item := range config.Items {
		assert.Equal(t, payloads[i]+"\n", step.Env[item.ContentEnv])
	}
}

func TestGenerateUnifiedPromptCreationStep_SplitsSystemAndUserItems(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{ParsedTools: NewTools(map[string]any{})}
	var output strings.Builder
	compiler.generateUnifiedPromptCreationStep(&output, []PromptSection{{Content: "System instructions"}}, []string{"User instructions"}, nil, data)

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("jobs:\n  test:\n    steps:\n"+output.String()), &workflow))
	var config struct {
		Items []struct {
			ContentEnv string `json:"content_env"`
		} `json:"items"`
		SystemItemCount int `json:"system_item_count"`
	}
	env := workflow.Jobs["test"].Steps[0].Env
	require.NoError(t, json.Unmarshal([]byte(env["GH_AW_PROMPT_CONFIG"]), &config))
	require.Len(t, config.Items, 4)
	assert.Equal(t, 3, config.SystemItemCount)
	assert.Equal(t, "<system>\nSystem instructions\n</system>\n", env[config.Items[0].ContentEnv]+env[config.Items[1].ContentEnv]+env[config.Items[2].ContentEnv])
	assert.Equal(t, "User instructions\n", env[config.Items[3].ContentEnv])
}

// TestGenerateUnifiedPromptCreationStep_OrderingBuiltinFirst tests that built-in prompts
// are prepended (written first) before user prompt content
func TestGenerateUnifiedPromptCreationStep_OrderingBuiltinFirst(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	// Create data with multiple built-in sections
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"playwright": true,
		}),
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
	}

	// Collect built-in sections
	builtinSections := compiler.collectPromptSections(data)

	// Create a simple user prompt
	userPromptChunks := []string{"# User Prompt\n\nThis is the user's task."}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()
	plan := promptRenderPlan(t, output)

	// Find positions of different prompt sections in the output
	tempFolderPos := strings.Index(plan, "temp_folder_prompt.md")
	playwrightPos := strings.Index(plan, "playwright_prompt.md")
	safeOutputsPos := strings.Index(plan, "safe_outputs_prompt.md")
	userPromptPos := strings.Index(plan, "# User Prompt")

	// Verify all sections are present
	require.NotEqual(t, -1, tempFolderPos, "Temp folder prompt should be present")
	require.NotEqual(t, -1, playwrightPos, "Playwright prompt should be present")
	require.NotEqual(t, -1, safeOutputsPos, "Safe outputs prompt should be present")
	require.NotEqual(t, -1, userPromptPos, "User prompt should be present")

	// Verify ordering: built-in prompts come before user prompt
	assert.Less(t, tempFolderPos, userPromptPos, "Temp folder prompt should come before user prompt")
	assert.Less(t, playwrightPos, userPromptPos, "Playwright prompt should come before user prompt")
	assert.Less(t, safeOutputsPos, userPromptPos, "Safe outputs prompt should come before user prompt")
}

// TestGenerateUnifiedPromptCreationStep_SubstitutionWithBuiltinExpressions tests that
// expressions in built-in prompts (like GitHub context) are properly extracted and substituted
func TestGenerateUnifiedPromptCreationStep_SubstitutionWithBuiltinExpressions(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	// Create data with GitHub tool enabled (which includes GitHub context prompt with expressions)
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"github": true,
		}),
	}

	// Collect built-in sections (should include GitHub context with expressions)
	builtinSections := compiler.collectPromptSections(data)

	// Create a simple user prompt
	userPromptChunks := []string{"# User Prompt"}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()

	// Verify environment variables from GitHub context prompt are declared
	assert.Contains(t, output, "GH_AW_GITHUB_REPOSITORY:", "Should have GH_AW_GITHUB_REPOSITORY env var")
	assert.Contains(t, output, "${{ github.repository }}", "Should have github.repository expression")

	// Verify environment variables section comes before run section
	envPos := strings.Index(output, "env:")
	withPos := strings.Index(output, "with:")
	assert.Less(t, envPos, withPos, "env section should come before action inputs")
}

// TestGenerateUnifiedPromptCreationStep_SubstitutionWithUserExpressions tests that
// expressions in user prompt are properly handled alongside built-in prompt expressions
func TestGenerateUnifiedPromptCreationStep_SubstitutionWithUserExpressions(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	// Create data with a built-in section
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	// Collect built-in sections (minimal - just temp folder)
	builtinSections := compiler.collectPromptSections(data)

	// Create user prompt with expressions
	userMarkdown := "Repository: ${{ github.repository }}\nActor: ${{ github.actor }}"

	// Extract expressions from user prompt
	extractor := NewExpressionExtractor()
	expressionMappings, err := extractor.ExtractExpressions(userMarkdown)
	require.NoError(t, err)
	require.Len(t, expressionMappings, 2, "Should extract 2 expressions from user prompt")

	// Replace expressions with placeholders
	userPromptWithPlaceholders := extractor.ReplaceExpressionsWithEnvVars(userMarkdown)
	userPromptChunks := []string{userPromptWithPlaceholders}

	var yaml strings.Builder
	allExpressionMappings := compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, expressionMappings, data)

	output := yaml.String()

	// Verify environment variables from user expressions are declared
	assert.Contains(t, output, "GH_AW_GITHUB_REPOSITORY:", "Should have GH_AW_GITHUB_REPOSITORY env var")
	assert.Contains(t, output, "GH_AW_GITHUB_ACTOR:", "Should have GH_AW_GITHUB_ACTOR env var")
	assert.Contains(t, output, "${{ github.repository }}", "Should have github.repository expression value")
	assert.Contains(t, output, "${{ github.actor }}", "Should have github.actor expression value")

	// Generate the substitution step separately (as done in compiler_yaml.go)
	var substYaml strings.Builder
	if len(allExpressionMappings) > 0 {
		generatePlaceholderSubstitutionStep(&substYaml, allExpressionMappings, "      ", nil)
	}

	// Verify substitution step is generated
	substOutput := substYaml.String()
	assert.Contains(t, substOutput, "Substitute placeholders", "Should have placeholder substitution step")
	assert.Contains(t, substOutput, "GH_AW_PROMPT: ${{ runner.temp }}/gh-aw/aw-prompts/prompt.txt")
}

// TestGenerateUnifiedPromptCreationStep_MultipleUserChunks tests that multiple
// user prompt chunks are properly appended after built-in prompts
func TestGenerateUnifiedPromptCreationStep_MultipleUserChunks(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	// Create data with minimal built-in sections
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	// Collect built-in sections
	builtinSections := compiler.collectPromptSections(data)

	// Create multiple user prompt chunks
	userPromptChunks := []string{
		"# Part 1\n\nFirst chunk of user prompt.",
		"# Part 2\n\nSecond chunk of user prompt.",
		"# Part 3\n\nThird chunk of user prompt.",
	}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()
	plan := promptRenderPlan(t, output)

	assert.NotContains(t, output, "<<", "Prompt creation should not use heredocs")
	assert.NotContains(t, output, "run:", "Prompt creation should not use a shell step")
	assert.Contains(t, output, "uses: actions/github-script@", "Prompt creation should use actions/github-script")

	// Verify all user chunks are present and in order
	part1Pos := strings.Index(plan, "# Part 1")
	part2Pos := strings.Index(plan, "# Part 2")
	part3Pos := strings.Index(plan, "# Part 3")

	require.NotEqual(t, -1, part1Pos, "Part 1 should be present")
	require.NotEqual(t, -1, part2Pos, "Part 2 should be present")
	require.NotEqual(t, -1, part3Pos, "Part 3 should be present")

	assert.Less(t, part1Pos, part2Pos, "Part 1 should come before Part 2")
	assert.Less(t, part2Pos, part3Pos, "Part 2 should come before Part 3")

	// Verify built-in prompt comes before all user chunks
	tempFolderPos := strings.Index(plan, "temp_folder_prompt.md")
	require.NotEqual(t, -1, tempFolderPos, "Temp folder prompt should be present")
	assert.Less(t, tempFolderPos, part1Pos, "Built-in prompt should come before user prompt chunks")

	// Verify system tags wrap built-in prompts
	systemOpenPos := strings.Index(plan, "<system>")
	systemClosePos := strings.Index(plan, "</system>")
	require.NotEqual(t, -1, systemOpenPos, "Opening system tag should be present")
	require.NotEqual(t, -1, systemClosePos, "Closing system tag should be present")
	assert.Less(t, systemOpenPos, tempFolderPos, "System tag should open before built-in prompts")
	assert.Less(t, tempFolderPos, systemClosePos, "System tag should close after built-in prompts")
	assert.Less(t, systemClosePos, part1Pos, "System tag should close before user prompt")
}

// TestGenerateUnifiedPromptCreationStep_CombinedExpressions tests that expressions
// from both built-in prompts and user prompts are properly combined and substituted
func TestGenerateUnifiedPromptCreationStep_CombinedExpressions(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	// Create data with GitHub tool enabled (has built-in expressions)
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"github": true,
		}),
	}

	// Collect built-in sections (includes GitHub context with expressions)
	builtinSections := compiler.collectPromptSections(data)

	// Create user prompt with different expressions
	userMarkdown := "Run ID: ${{ github.run_id }}\nWorkspace: ${{ github.workspace }}"

	// Extract expressions from user prompt
	extractor := NewExpressionExtractor()
	expressionMappings, err := extractor.ExtractExpressions(userMarkdown)
	require.NoError(t, err)

	// Replace expressions with placeholders
	userPromptWithPlaceholders := extractor.ReplaceExpressionsWithEnvVars(userMarkdown)
	userPromptChunks := []string{userPromptWithPlaceholders}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, expressionMappings, data)

	output := yaml.String()

	// Verify environment variables from both built-in and user prompts are present
	// From built-in GitHub context prompt
	assert.Contains(t, output, "GH_AW_GITHUB_REPOSITORY:", "Should have built-in env var")
	assert.Contains(t, output, "GH_AW_GITHUB_ACTOR:", "Should have built-in env var")

	// From user prompt
	assert.Contains(t, output, "GH_AW_GITHUB_RUN_ID:", "Should have user prompt env var")
	assert.Contains(t, output, "GH_AW_GITHUB_WORKSPACE:", "Should have user prompt env var")

	// Verify all environment variables are sorted (after GH_AW_PROMPT)
	envSection := output[strings.Index(output, "env:"):strings.Index(output, "with:")]
	lines := strings.Split(envSection, "\n")

	var envVarNames []string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_") && !strings.Contains(line, "GH_AW_PROMPT:") && !strings.Contains(line, "GH_AW_SAFE_OUTPUTS:") && !strings.Contains(line, "GH_AW_PROMPT_") {
			// Extract variable name
			parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
			if len(parts) == 2 {
				envVarNames = append(envVarNames, parts[0])
			}
		}
	}

	// Check that variables are sorted
	for i := 1; i < len(envVarNames); i++ {
		assert.LessOrEqual(t, envVarNames[i-1], envVarNames[i],
			"Environment variables should be sorted: %s should come before or equal to %s",
			envVarNames[i-1], envVarNames[i])
	}
}

// TestGenerateUnifiedPromptCreationStep_NoAppendSteps tests that the old
// "Append context instructions" step is not generated
func TestGenerateUnifiedPromptCreationStep_NoAppendSteps(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"playwright": true,
			"github":     true,
		}),
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
	}

	builtinSections := compiler.collectPromptSections(data)

	// Create user prompt with expressions to ensure substitution step is generated
	userMarkdown := "Run ID: ${{ github.run_id }}"
	extractor := NewExpressionExtractor()
	expressionMappings, _ := extractor.ExtractExpressions(userMarkdown)
	userPromptWithPlaceholders := extractor.ReplaceExpressionsWithEnvVars(userMarkdown)
	userPromptChunks := []string{userPromptWithPlaceholders}

	var yaml strings.Builder
	_ = compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, expressionMappings, data)

	output := yaml.String()

	// Verify there's only the unified step (not old separate steps)
	// The substitution step is now generated separately in compiler_yaml.go
	stepNameCount := strings.Count(output, "- name:")
	assert.Equal(t, 1, stepNameCount, "Should have exactly 1 step: Create prompt")

	// Verify the old append step name is not present
	assert.NotContains(t, output, "Append context instructions to prompt",
		"Should not have old 'Append context instructions' step")
	assert.NotContains(t, output, "Append prompt (part",
		"Should not have old 'Append prompt (part N)' steps")
}

// TestGenerateUnifiedPromptCreationStep_UsesJavaScriptRenderer verifies that
// prompt content is rendered by a static JavaScript action with no shell syntax.
func TestGenerateUnifiedPromptCreationStep_UsesJavaScriptRenderer(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	builtinSections := compiler.collectPromptSections(data)
	userPromptChunks := []string{"# User Prompt"}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()

	assert.Contains(t, output, "uses: actions/github-script@")
	assert.Contains(t, output, "create_prompt.cjs")
	assert.Contains(t, output, "GH_AW_PROMPT_CONFIG:")
	assert.NotContains(t, output, "run:")
	assert.NotContains(t, output, "cat ")
	assert.NotContains(t, output, "<<")
	assert.NotContains(t, output, "create_prompt_first.sh")
}

// TestGenerateUnifiedPromptCreationStep_SystemTags tests that built-in prompts
// are wrapped in <system> XML tags
func TestGenerateUnifiedPromptCreationStep_SystemTags(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	// Create data with multiple built-in sections
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"playwright": true,
		}),
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
	}

	// Collect built-in sections
	builtinSections := compiler.collectPromptSections(data)

	// Create user prompt
	userPromptChunks := []string{"# User Task\n\nThis is the user's task."}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()
	plan := promptRenderPlan(t, output)

	// Verify system tags are present
	assert.Contains(t, output, "<system>", "Should have opening system tag")
	assert.Contains(t, output, "</system>", "Should have closing system tag")

	// Verify system tags wrap built-in content
	systemOpenPos := strings.Index(plan, "<system>")
	systemClosePos := strings.Index(plan, "</system>")

	// Find positions of built-in content
	tempFolderPos := strings.Index(plan, "temp_folder_prompt.md")
	playwrightPos := strings.Index(plan, "playwright_prompt.md")
	safeOutputsPos := strings.Index(plan, "safe_outputs_prompt.md")

	// Find position of user content
	userTaskPos := strings.Index(plan, "# User Task")

	// Verify ordering: <system> -> built-in content -> </system> -> user content
	require.NotEqual(t, -1, systemOpenPos, "Opening system tag should be present")
	require.NotEqual(t, -1, systemClosePos, "Closing system tag should be present")
	require.NotEqual(t, -1, tempFolderPos, "Temp folder should be present")
	require.NotEqual(t, -1, userTaskPos, "User task should be present")

	assert.Less(t, systemOpenPos, tempFolderPos, "System tag should open before temp folder")
	assert.Less(t, tempFolderPos, playwrightPos, "Temp folder should come before playwright")
	assert.Less(t, playwrightPos, safeOutputsPos, "Playwright should come before safe outputs")
	assert.Less(t, safeOutputsPos, systemClosePos, "Safe outputs should come before system close tag")
	assert.Less(t, systemClosePos, userTaskPos, "System tag should close before user content")
}

// TestGenerateUnifiedPromptCreationStep_EmptyUserPrompt tests handling of empty user prompt
func TestGenerateUnifiedPromptCreationStep_EmptyUserPrompt(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	builtinSections := compiler.collectPromptSections(data)
	userPromptChunks := []string{} // Empty user prompt

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()
	plan := promptRenderPlan(t, output)

	// Verify built-in sections are still present
	assert.Contains(t, plan, "temp_folder_prompt.md", "Should have temp folder prompt")
	assert.Contains(t, plan, "<system>", "Should have system tag even with empty user prompt")
	assert.Contains(t, plan, "</system>", "Should close system tag even with empty user prompt")

	// Verify the step was created
	assert.Contains(t, output, "- name: Create prompt with built-in context")
}

// TestGenerateUnifiedPromptCreationStep_NoBuiltinSections tests handling when there are no built-in sections
func TestGenerateUnifiedPromptCreationStep_NoBuiltinSections(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	builtinSections := []PromptSection{} // No built-in sections
	userPromptChunks := []string{"# User Task"}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()

	// Verify user prompt is still written
	assert.Contains(t, output, "# User Task", "Should have user task even without built-in sections")

	// System tags should not be present when there are no built-in sections
	assert.NotContains(t, output, "<system>", "Should not have system tag without built-in sections")
	assert.NotContains(t, output, "</system>", "Should not have closing system tag without built-in sections")
}

// TestGenerateUnifiedPromptCreationStep_TrialMode tests that trial mode note is included in built-in prompts
func TestGenerateUnifiedPromptCreationStep_TrialMode(t *testing.T) {
	compiler := &Compiler{
		trialMode:            true,
		trialLogicalRepoSlug: "test-org/test-repo",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	builtinSections := compiler.collectPromptSections(data)
	userPromptChunks := []string{"# User Task"}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()
	plan := promptRenderPlan(t, output)

	// Verify trial mode content is present
	assert.Contains(t, output, "test-org/test-repo", "Should contain trial repo slug")

	// Verify it's within system tags
	systemOpenPos := strings.Index(plan, "<system>")
	systemClosePos := strings.Index(plan, "</system>")
	trialModePos := strings.Index(plan, "test-org/test-repo")

	require.NotEqual(t, -1, systemOpenPos, "Should have opening system tag")
	require.NotEqual(t, -1, systemClosePos, "Should have closing system tag")
	require.NotEqual(t, -1, trialModePos, "Should have trial mode content")

	assert.Less(t, systemOpenPos, trialModePos, "Trial mode should be after system tag opens")
	assert.Less(t, trialModePos, systemClosePos, "Trial mode should be before system tag closes")
}

// TestGenerateUnifiedPromptCreationStep_CacheAndRepoMemory tests cache and repo memory prompts
func TestGenerateUnifiedPromptCreationStep_CacheAndRepoMemory(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{ID: "default"},
			},
		},
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{ID: "default", BranchName: "memory"},
			},
		},
	}

	builtinSections := compiler.collectPromptSections(data)
	userPromptChunks := []string{"# User Task"}

	var yaml strings.Builder
	allExpressionMappings := compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()
	plan := promptRenderPlan(t, output)

	// Verify cache template file reference
	assert.Contains(t, output, "cache_memory_prompt.md", "Should reference cache template file")
	assert.Contains(t, output, "repo_memory_prompt.md", "Should reference repo memory template file")

	// Generate the substitution step separately to verify cache dir is in substitutions
	var substYaml strings.Builder
	if len(allExpressionMappings) > 0 {
		generatePlaceholderSubstitutionStep(&substYaml, allExpressionMappings, "      ", nil)
	}
	substOutput := substYaml.String()
	assert.Contains(t, substOutput, "GH_AW_CACHE_DIR: process.env.GH_AW_CACHE_DIR", "Should have cache dir in substitution")
	assert.Contains(t, substOutput, "GH_AW_MEMORY_DIR: process.env.GH_AW_MEMORY_DIR", "Should have memory dir in substitution")

	// Verify ordering within system tags
	systemOpenPos := strings.Index(plan, "<system>")
	cachePos := strings.Index(plan, "cache_memory_prompt.md")
	repoPos := strings.Index(plan, "repo_memory_prompt.md")
	systemClosePos := strings.Index(plan, "</system>")
	userPos := strings.Index(plan, "# User Task")

	assert.Less(t, systemOpenPos, cachePos, "Cache should be after system tag opens")
	assert.Less(t, cachePos, repoPos, "Cache should come before repo memory")
	assert.Less(t, repoPos, systemClosePos, "Repo memory should be before system tag closes")
	assert.Less(t, systemClosePos, userPos, "User task should be after system tag closes")
}

// TestGenerateUnifiedPromptCreationStep_PRContextConditional tests that PR context uses data-only conditions
func TestGenerateUnifiedPromptCreationStep_PRContextConditional(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
		On:          "issue_comment",
		Permissions: "contents: read",
	}

	builtinSections := compiler.collectPromptSections(data)
	userPromptChunks := []string{"# User Task"}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()
	plan := promptRenderPlan(t, output)

	// Verify PR context is included with conditional
	assert.Contains(t, output, "pr_context_prompt.md", "Should have PR context prompt file reference")
	assert.Contains(t, output, `\"condition_env\":\"GH_AW_INCLUDE_PR_CONTEXT\"`, "Should reference a condition environment variable")
	assert.Contains(t, output, "GH_AW_INCLUDE_PR_CONTEXT: ${{", "Should evaluate the condition as environment data")
	assert.NotContains(t, output, "if [", "Should not use shell conditions")

	// Verify it's within system tags
	systemOpenPos := strings.Index(plan, "<system>")
	systemClosePos := strings.Index(plan, "</system>")
	prContextPos := strings.Index(plan, "pr_context_prompt.md")
	userPos := strings.Index(plan, "# User Task")

	require.NotEqual(t, -1, prContextPos, "PR context should be present")
	assert.Less(t, systemOpenPos, prContextPos, "PR context should be after system tag opens")
	assert.Less(t, prContextPos, systemClosePos, "PR context should be before system tag closes")
	assert.Less(t, systemClosePos, userPos, "User task should be after system tag closes")
}

// TestGenerateUnifiedPromptCreationStep_AllToolsCombined tests with all tools enabled
func TestGenerateUnifiedPromptCreationStep_AllToolsCombined(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"playwright": true,
			"github":     true,
		}),
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{{ID: "default"}},
		},
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{{ID: "default", BranchName: "memory"}},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
		On:          "issue_comment",
		Permissions: "contents: read",
	}

	builtinSections := compiler.collectPromptSections(data)
	userPromptChunks := []string{"# User Task"}

	var yaml strings.Builder
	allExpressionMappings := compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()
	plan := promptRenderPlan(t, output)

	// Verify all sections are present
	assert.Contains(t, output, "temp_folder_prompt.md", "Should have temp folder")
	assert.Contains(t, output, "playwright_prompt.md", "Should have playwright")
	assert.Contains(t, output, "cache_memory_prompt.md", "Should have cache memory template")
	assert.Contains(t, output, "repo_memory_prompt.md", "Should have repo memory template file")
	assert.Contains(t, output, "safe_outputs_prompt.md", "Should have safe outputs file reference")
	assert.Contains(t, output, "<github-context>", "Should have GitHub context")
	assert.Contains(t, output, "pr_context_prompt.md", "Should have PR context")

	// Generate the substitution step separately to verify cache dir is in substitutions
	var substYaml strings.Builder
	if len(allExpressionMappings) > 0 {
		generatePlaceholderSubstitutionStep(&substYaml, allExpressionMappings, "      ", nil)
	}
	substOutput := substYaml.String()
	assert.Contains(t, substOutput, "GH_AW_CACHE_DIR: process.env.GH_AW_CACHE_DIR", "Should have cache dir in substitution")

	// Verify all are within system tags and before user prompt
	systemOpenPos := strings.Index(plan, "<system>")
	systemClosePos := strings.Index(plan, "</system>")
	userPos := strings.Index(plan, "# User Task")

	require.NotEqual(t, -1, systemOpenPos, "Should have opening system tag")
	require.NotEqual(t, -1, systemClosePos, "Should have closing system tag")
	assert.Less(t, systemClosePos, userPos, "All built-in sections should be before user task")
}

// TestGenerateUnifiedPromptCreationStep_EnvironmentVariableSorting tests that env vars are sorted
func TestGenerateUnifiedPromptCreationStep_EnvironmentVariableSorting(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"github": true,
		}),
	}

	builtinSections := compiler.collectPromptSections(data)

	// Create user prompt with multiple expressions
	userMarkdown := "Workspace: ${{ github.workspace }}\nActor: ${{ github.actor }}\nRepo: ${{ github.repository }}"
	extractor := NewExpressionExtractor()
	expressionMappings, _ := extractor.ExtractExpressions(userMarkdown)
	userPromptWithPlaceholders := extractor.ReplaceExpressionsWithEnvVars(userMarkdown)
	userPromptChunks := []string{userPromptWithPlaceholders}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, expressionMappings, data)

	output := yaml.String()

	// Extract env var names
	envSection := output[strings.Index(output, "env:"):strings.Index(output, "with:")]
	lines := strings.Split(envSection, "\n")

	var envVarNames []string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_") && !strings.Contains(line, "GH_AW_PROMPT:") && !strings.Contains(line, "GH_AW_SAFE_OUTPUTS:") && !strings.Contains(line, "GH_AW_PROMPT_") {
			parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
			if len(parts) == 2 {
				envVarNames = append(envVarNames, parts[0])
			}
		}
	}

	// Verify we have multiple env vars
	require.Greater(t, len(envVarNames), 3, "Should have multiple environment variables")

	// Verify they are sorted
	for i := 1; i < len(envVarNames); i++ {
		assert.LessOrEqual(t, envVarNames[i-1], envVarNames[i],
			"Environment variables should be sorted: %s should come before %s",
			envVarNames[i-1], envVarNames[i])
	}
}

// TestGenerateUnifiedPromptCreationStep_LargeUserPromptChunking tests handling of very large user prompts
func TestGenerateUnifiedPromptCreationStep_LargeUserPromptChunking(t *testing.T) {
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	builtinSections := compiler.collectPromptSections(data)

	// Create many chunks to simulate large prompt
	userPromptChunks := make([]string, 10)
	for i := range 10 {
		userPromptChunks[i] = fmt.Sprintf("# Section %d\n\nContent for section %d.", i+1, i+1)
	}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()
	plan := promptRenderPlan(t, output)

	// Verify all chunks are present
	for i := range 10 {
		assert.Contains(t, output, fmt.Sprintf("# Section %d", i+1),
			"Should contain section %d", i+1)
	}

	// Verify chunks are in order
	positions := make([]int, 10)
	for i := range 10 {
		positions[i] = strings.Index(plan, fmt.Sprintf("# Section %d", i+1))
		require.NotEqual(t, -1, positions[i], "Section %d should be present", i+1)
	}

	for i := 1; i < 10; i++ {
		assert.Less(t, positions[i-1], positions[i],
			"Section %d should come before Section %d", i, i+1)
	}

	// Verify all chunks come after system tag closes
	systemClosePos := strings.Index(plan, "</system>")
	for i := range 10 {
		assert.Less(t, systemClosePos, positions[i],
			"Section %d should come after system tag closes", i+1)
	}
}

// TestUnifiedPromptCreation_EndToEndIntegration tests full workflow compilation
func TestUnifiedPromptCreation_EndToEndIntegration(t *testing.T) {
	// Create a simple test workflow
	testWorkflow := `---
on: push
engine: claude
tools:
  playwright:
  github:
  cache-memory:
  repo-memory:
    branch-name: memory
safe-outputs:
  create-issue:
---

# Test Workflow

This is a test workflow to verify prompt generation.
Repository: ${{ github.repository }}
Actor: ${{ github.actor }}`

	// Write to temp file
	tmpDir := t.TempDir()
	workflowFile := tmpDir + "/test.md"
	err := os.WriteFile(workflowFile, []byte(testWorkflow), 0644)
	require.NoError(t, err, "Should write test workflow file")

	// Compile workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowFile)
	require.NoError(t, err, "Should compile workflow successfully")

	// Read generated lock file
	lockFile := strings.Replace(workflowFile, ".md", ".lock.yml", 1)
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Should read lock file")

	lockStr := string(lockContent)
	plan := promptRenderPlan(t, lockStr)

	// Verify system tags are present
	assert.Contains(t, lockStr, "<system>", "Lock file should contain opening system tag")
	assert.Contains(t, lockStr, "</system>", "Lock file should contain closing system tag")

	// Verify built-in prompts are within system tags
	systemOpenPos := strings.Index(plan, "<system>")
	systemClosePos := strings.Index(plan, "</system>")
	tempFolderPos := strings.Index(plan, "temp_folder_prompt.md")
	playwrightPos := strings.Index(plan, "playwright_prompt.md")

	assert.Less(t, systemOpenPos, tempFolderPos, "Built-in prompts should be after system tag opens")
	assert.Less(t, tempFolderPos, systemClosePos, "Built-in prompts should be before system tag closes")
	assert.Less(t, playwrightPos, systemClosePos, "Playwright should be before system tag closes")

	// Verify user prompt is after system tags
	// With runtime-import, the actual content is in the original workflow file
	// The lock file should contain the runtime-import macro after system tags
	runtimeImportPos := strings.Index(plan, "{{#runtime-import")
	assert.Greater(t, runtimeImportPos, -1, "Should contain runtime-import macro")
	assert.Less(t, systemClosePos, runtimeImportPos, "Runtime-import macro should come after system tag closes")

	// Verify expressions are handled
	assert.Contains(t, lockStr, "GH_AW_GITHUB_REPOSITORY:", "Should have repository env var")
	assert.Contains(t, lockStr, "GH_AW_GITHUB_ACTOR:", "Should have actor env var")
}

// TestUnifiedPromptCreation_MinimalWorkflow tests compilation of minimal workflow
func TestUnifiedPromptCreation_MinimalWorkflow(t *testing.T) {
	testWorkflow := `---
on: push
engine: claude
---

# Simple Task

Do something simple.`

	tmpDir := t.TempDir()
	workflowFile := tmpDir + "/minimal.md"
	err := os.WriteFile(workflowFile, []byte(testWorkflow), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowFile)
	require.NoError(t, err, "Should compile minimal workflow")

	lockFile := strings.Replace(workflowFile, ".md", ".lock.yml", 1)
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	lockStr := string(lockContent)
	plan := promptRenderPlan(t, lockStr)

	// Even minimal workflow should have system tags
	assert.Contains(t, lockStr, "<system>", "Minimal workflow should have system tags")
	assert.Contains(t, lockStr, "</system>", "Minimal workflow should have closing system tag")

	// Should have at least temp folder
	assert.Contains(t, lockStr, "temp_folder_prompt.md", "Should have temp folder prompt")

	// User prompt should be after system tags
	// With runtime-import, the actual content is in the original workflow file
	// The lock file should contain the runtime-import macro after system tags
	systemClosePos := strings.Index(plan, "</system>")
	runtimeImportPos := strings.Index(plan, "{{#runtime-import")
	assert.Greater(t, runtimeImportPos, -1, "Should contain runtime-import macro")
	assert.Less(t, systemClosePos, runtimeImportPos, "Runtime-import macro should be after system tags")
}

// TestUnifiedPromptCreation_SafeOutputsOnly tests workflow with only safe-outputs
func TestUnifiedPromptCreation_SafeOutputsOnly(t *testing.T) {
	testWorkflow := `---
on: issue_comment
engine: claude
safe-outputs:
  create-issue:
  update-issue:
---

# Issue Manager

Manage issues based on comments.`

	tmpDir := t.TempDir()
	workflowFile := tmpDir + "/safe-outputs.md"
	err := os.WriteFile(workflowFile, []byte(testWorkflow), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowFile)
	require.NoError(t, err)

	lockFile := strings.Replace(workflowFile, ".md", ".lock.yml", 1)
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	lockStr := string(lockContent)
	plan := promptRenderPlan(t, lockStr)

	// Verify safe-outputs file reference is within system tags
	systemOpenPos := strings.Index(plan, "<system>")
	systemClosePos := strings.Index(plan, "</system>")
	safeOutputsPos := strings.Index(plan, "safe_outputs_prompt.md")

	require.NotEqual(t, -1, safeOutputsPos, "Should reference safe_outputs_prompt.md")
	assert.Less(t, systemOpenPos, safeOutputsPos, "Safe outputs should be after system tag opens")
	assert.Less(t, safeOutputsPos, systemClosePos, "Safe outputs should be before system tag closes")

	// Should mention the specific tools (per-tool instructions are still inline)
	assert.Contains(t, lockStr, "create_issue", "Should reference create_issue tool")
	assert.Contains(t, lockStr, "update_issue", "Should reference update_issue tool")
}

// TestUnifiedPromptCreation_ExpressionSubstitution tests that expressions are properly substituted
func TestUnifiedPromptCreation_ExpressionSubstitution(t *testing.T) {
	testWorkflow := `---
on: push
engine: claude
---

# Expression Test

Repository: ${{ github.repository }}
Run ID: ${{ github.run_id }}
Workspace: ${{ github.workspace }}
Actor: ${{ github.actor }}`

	tmpDir := t.TempDir()
	workflowFile := tmpDir + "/expressions.md"
	err := os.WriteFile(workflowFile, []byte(testWorkflow), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowFile)
	require.NoError(t, err)

	lockFile := strings.Replace(workflowFile, ".md", ".lock.yml", 1)
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	lockStr := string(lockContent)

	// Verify all expressions have corresponding env vars
	assert.Contains(t, lockStr, "GH_AW_GITHUB_REPOSITORY:", "Should have repository env var")
	assert.Contains(t, lockStr, "GH_AW_GITHUB_RUN_ID:", "Should have run_id env var")
	assert.Contains(t, lockStr, "GH_AW_GITHUB_WORKSPACE:", "Should have workspace env var")
	assert.Contains(t, lockStr, "GH_AW_GITHUB_ACTOR:", "Should have actor env var")

	// Verify substitution step is generated
	assert.Contains(t, lockStr, "Substitute placeholders", "Should have substitution step")
	assert.Contains(t, lockStr, "substitute_placeholders.cjs", "Should use substitution script")
}

// TestToolWithMaxBudget tests the toolWithMaxBudget helper formats tool names correctly.
func TestToolWithMaxBudget(t *testing.T) {
	one := "1"
	five := "5"
	expr := "${{ inputs.max }}"

	tests := []struct {
		name     string
		toolName string
		max      *string
		want     string
	}{
		{
			name:     "nil max returns plain name",
			toolName: "create_issue",
			max:      nil,
			want:     "create_issue",
		},
		{
			name:     "max=1 returns plain name",
			toolName: "create_issue",
			max:      &one,
			want:     "create_issue",
		},
		{
			name:     "max>1 returns annotated name",
			toolName: "create_issue",
			max:      &five,
			want:     "create_issue(max:5)",
		},
		{
			name:     "GitHub Actions expression preserved",
			toolName: "add_comment",
			max:      &expr,
			want:     "add_comment(max:${{ inputs.max }})",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolWithMaxBudget(tt.toolName, tt.max)
			assert.Equal(t, tt.want, got, "toolWithMaxBudget(%q, max) should return %q", tt.toolName, tt.want)
		})
	}
}

// TestUnifiedPromptCreation_SafeOutputsMaxBudgetInTools tests that compiled workflows
// include per-tool max annotations in the tools list when max > 1.
func TestUnifiedPromptCreation_SafeOutputsMaxBudgetInTools(t *testing.T) {
	testWorkflow := `---
on: issue_comment
engine: claude
safe-outputs:
  create-issue:
    max: 20
  add-comment:
    max: 5
  close-issue:
    max: 1
---

# Multi-Write Workflow

Process issues with multiple outputs.`

	tmpDir := t.TempDir()
	workflowFile := tmpDir + "/multi-write.md"
	err := os.WriteFile(workflowFile, []byte(testWorkflow), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowFile)
	require.NoError(t, err)

	lockFile := strings.Replace(workflowFile, ".md", ".lock.yml", 1)
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	lockStr := string(lockContent)

	// Tools with max > 1 should show budget annotation
	assert.Contains(t, lockStr, "create_issue(max:20)", "create_issue with max:20 should show budget annotation")
	assert.Contains(t, lockStr, "add_comment(max:5)", "add_comment with max:5 should show budget annotation")

	// Tool with max=1 should not show annotation (default behavior)
	assert.Contains(t, lockStr, "close_issue", "close_issue should be present")
	assert.NotContains(t, lockStr, "close_issue(max:1)", "close_issue with max:1 should not show annotation")

	// Prompt should use "at least one" not "exactly one"
	assert.NotContains(t, lockStr, "exactly one", "Prompt should not say 'exactly one'")
}
