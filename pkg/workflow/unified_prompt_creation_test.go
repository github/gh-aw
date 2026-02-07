//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	// Find positions of different prompt sections in the output
	tempFolderPos := strings.Index(output, "temp_folder_prompt.md")
	playwrightPos := strings.Index(output, "playwright_prompt.md")
	safeOutputsPos := strings.Index(output, "<safe-outputs>")
	userPromptPos := strings.Index(output, "# User Prompt")

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
	runPos := strings.Index(output, "run: |")
	assert.Less(t, envPos, runPos, "env section should come before run section")
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
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, expressionMappings, data)

	output := yaml.String()

	// Verify environment variables from user expressions are declared
	assert.Contains(t, output, "GH_AW_GITHUB_REPOSITORY:", "Should have GH_AW_GITHUB_REPOSITORY env var")
	assert.Contains(t, output, "GH_AW_GITHUB_ACTOR:", "Should have GH_AW_GITHUB_ACTOR env var")
	assert.Contains(t, output, "${{ github.repository }}", "Should have github.repository expression value")
	assert.Contains(t, output, "${{ github.actor }}", "Should have github.actor expression value")

	// Verify substitution step is generated
	assert.Contains(t, output, "Substitute placeholders", "Should have placeholder substitution step")
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

	// Count PROMPT_EOF markers
	// With system tags:
	// - 2 for opening <system> tag
	// - 2 for closing </system> tag
	// - 2 per user chunk
	eofCount := strings.Count(output, "PROMPT_EOF")
	expectedEOFCount := 4 + (len(userPromptChunks) * 2) // 4 for system tags, 2 per user chunk
	assert.Equal(t, expectedEOFCount, eofCount, "Should have correct number of PROMPT_EOF markers")

	// Verify all user chunks are present and in order
	part1Pos := strings.Index(output, "# Part 1")
	part2Pos := strings.Index(output, "# Part 2")
	part3Pos := strings.Index(output, "# Part 3")

	require.NotEqual(t, -1, part1Pos, "Part 1 should be present")
	require.NotEqual(t, -1, part2Pos, "Part 2 should be present")
	require.NotEqual(t, -1, part3Pos, "Part 3 should be present")

	assert.Less(t, part1Pos, part2Pos, "Part 1 should come before Part 2")
	assert.Less(t, part2Pos, part3Pos, "Part 2 should come before Part 3")

	// Verify built-in prompt comes before all user chunks
	tempFolderPos := strings.Index(output, "temp_folder_prompt.md")
	require.NotEqual(t, -1, tempFolderPos, "Temp folder prompt should be present")
	assert.Less(t, tempFolderPos, part1Pos, "Built-in prompt should come before user prompt chunks")

	// Verify system tags wrap built-in prompts
	systemOpenPos := strings.Index(output, "<system>")
	systemClosePos := strings.Index(output, "</system>")
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
	envSection := output[strings.Index(output, "env:"):strings.Index(output, "run: |")]
	lines := strings.Split(envSection, "\n")

	var envVarNames []string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_") && !strings.Contains(line, "GH_AW_PROMPT:") && !strings.Contains(line, "GH_AW_SAFE_OUTPUTS:") {
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
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, expressionMappings, data)

	output := yaml.String()

	// Verify there's only the unified step and substitution step (not old separate steps)
	stepNameCount := strings.Count(output, "- name:")
	assert.Equal(t, 2, stepNameCount, "Should have exactly 2 steps: Create prompt and Substitute placeholders")

	// Verify the old append step name is not present
	assert.NotContains(t, output, "Append context instructions to prompt",
		"Should not have old 'Append context instructions' step")
	assert.NotContains(t, output, "Append prompt (part",
		"Should not have old 'Append prompt (part N)' steps")
}

// TestGenerateUnifiedPromptCreationStep_FirstContentUsesCreate tests that
// the first content uses ">" (create/overwrite) and subsequent content uses ">>" (append)
func TestGenerateUnifiedPromptCreationStep_FirstContentUsesCreate(t *testing.T) {
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

	// Find the first cat command (should use > for create)
	firstCatPos := strings.Index(output, `cat "`)
	require.NotEqual(t, -1, firstCatPos, "Should have cat command")

	// Extract the line containing the first cat command
	firstCatLine := output[firstCatPos : firstCatPos+strings.Index(output[firstCatPos:], "\n")]

	// Verify it uses > (create mode)
	assert.Contains(t, firstCatLine, `> "$GH_AW_PROMPT"`,
		"First content should use > (create mode): %s", firstCatLine)

	// Find subsequent cat commands (should use >> for append)
	remainingOutput := output[firstCatPos+len(firstCatLine):]
	if strings.Contains(remainingOutput, `cat "`) || strings.Contains(remainingOutput, "cat << 'PROMPT_EOF'") {
		// Verify subsequent operations use >> (append mode)
		assert.Contains(t, remainingOutput, `>> "$GH_AW_PROMPT"`,
			"Subsequent content should use >> (append mode)")
	}
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

	// Verify system tags are present
	assert.Contains(t, output, "<system>", "Should have opening system tag")
	assert.Contains(t, output, "</system>", "Should have closing system tag")

	// Verify system tags wrap built-in content
	systemOpenPos := strings.Index(output, "<system>")
	systemClosePos := strings.Index(output, "</system>")

	// Find positions of built-in content
	tempFolderPos := strings.Index(output, "temp_folder_prompt.md")
	playwrightPos := strings.Index(output, "playwright_prompt.md")
	safeOutputsPos := strings.Index(output, "<safe-outputs>")

	// Find position of user content
	userTaskPos := strings.Index(output, "# User Task")

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

	// Verify built-in sections are still present
	assert.Contains(t, output, "temp_folder_prompt.md", "Should have temp folder prompt")
	assert.Contains(t, output, "<system>", "Should have system tag even with empty user prompt")
	assert.Contains(t, output, "</system>", "Should close system tag even with empty user prompt")

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

	// Verify trial mode content is present
	assert.Contains(t, output, "test-org/test-repo", "Should contain trial repo slug")

	// Verify it's within system tags
	systemOpenPos := strings.Index(output, "<system>")
	systemClosePos := strings.Index(output, "</system>")
	trialModePos := strings.Index(output, "test-org/test-repo")

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
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()

	// Verify cache template file reference and environment variables
	assert.Contains(t, output, "cache_memory_prompt.md", "Should reference cache template file")
	assert.Contains(t, output, "GH_AW_CACHE_DIR: '/tmp/gh-aw/cache-memory/'", "Should have cache dir env var")
	assert.Contains(t, output, "GH_AW_CACHE_DIR: process.env.GH_AW_CACHE_DIR", "Should have cache dir in substitution")
	assert.Contains(t, output, "Repo Memory Available", "Should have repo memory prompt")
	assert.Contains(t, output, "/tmp/gh-aw/repo-memory/", "Should reference repo memory directory")

	// Verify ordering within system tags
	systemOpenPos := strings.Index(output, "<system>")
	cachePos := strings.Index(output, "cache_memory_prompt.md")
	repoPos := strings.Index(output, "Repo Memory Available")
	systemClosePos := strings.Index(output, "</system>")
	userPos := strings.Index(output, "# User Task")

	assert.Less(t, systemOpenPos, cachePos, "Cache should be after system tag opens")
	assert.Less(t, cachePos, repoPos, "Cache should come before repo memory")
	assert.Less(t, repoPos, systemClosePos, "Repo memory should be before system tag closes")
	assert.Less(t, systemClosePos, userPos, "User task should be after system tag closes")
}

// TestGenerateUnifiedPromptCreationStep_PRContextConditional tests that PR context uses shell conditions
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

	// Verify PR context is included with conditional
	assert.Contains(t, output, "pr_context_prompt.md", "Should have PR context prompt file reference")
	assert.Contains(t, output, "if [", "Should have shell conditional for PR context")
	assert.Contains(t, output, "GITHUB_EVENT_NAME", "Should check event name in conditional")
	assert.Contains(t, output, "GH_AW_IS_PR_COMMENT", "Should have PR comment check env var")

	// Verify it's within system tags
	systemOpenPos := strings.Index(output, "<system>")
	systemClosePos := strings.Index(output, "</system>")
	prContextPos := strings.Index(output, "pr_context_prompt.md")
	userPos := strings.Index(output, "# User Task")

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
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()

	// Verify all sections are present
	assert.Contains(t, output, "temp_folder_prompt.md", "Should have temp folder")
	assert.Contains(t, output, "playwright_prompt.md", "Should have playwright")
	assert.Contains(t, output, "cache_memory_prompt.md", "Should have cache memory template")
	assert.Contains(t, output, "GH_AW_CACHE_DIR: '/tmp/gh-aw/cache-memory/'", "Should have cache dir env var")
	assert.Contains(t, output, "GH_AW_CACHE_DIR: process.env.GH_AW_CACHE_DIR", "Should have cache dir in substitution")
	assert.Contains(t, output, "Repo Memory Available", "Should have repo memory")
	assert.Contains(t, output, "<safe-outputs>", "Should have safe outputs")
	assert.Contains(t, output, "<github-context>", "Should have GitHub context")
	assert.Contains(t, output, "pr_context_prompt.md", "Should have PR context")

	// Verify all are within system tags and before user prompt
	systemOpenPos := strings.Index(output, "<system>")
	systemClosePos := strings.Index(output, "</system>")
	userPos := strings.Index(output, "# User Task")

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
	envSection := output[strings.Index(output, "env:"):strings.Index(output, "run: |")]
	lines := strings.Split(envSection, "\n")

	var envVarNames []string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_") && !strings.Contains(line, "GH_AW_PROMPT:") && !strings.Contains(line, "GH_AW_SAFE_OUTPUTS:") {
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
	for i := 0; i < 10; i++ {
		userPromptChunks[i] = fmt.Sprintf("# Section %d\n\nContent for section %d.", i+1, i+1)
	}

	var yaml strings.Builder
	compiler.generateUnifiedPromptCreationStep(&yaml, builtinSections, userPromptChunks, nil, data)

	output := yaml.String()

	// Verify all chunks are present
	for i := 0; i < 10; i++ {
		assert.Contains(t, output, fmt.Sprintf("# Section %d", i+1),
			"Should contain section %d", i+1)
	}

	// Verify chunks are in order
	positions := make([]int, 10)
	for i := 0; i < 10; i++ {
		positions[i] = strings.Index(output, fmt.Sprintf("# Section %d", i+1))
		require.NotEqual(t, -1, positions[i], "Section %d should be present", i+1)
	}

	for i := 1; i < 10; i++ {
		assert.Less(t, positions[i-1], positions[i],
			"Section %d should come before Section %d", i, i+1)
	}

	// Verify all chunks come after system tag closes
	systemClosePos := strings.Index(output, "</system>")
	for i := 0; i < 10; i++ {
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

	// Verify system tags are present
	assert.Contains(t, lockStr, "<system>", "Lock file should contain opening system tag")
	assert.Contains(t, lockStr, "</system>", "Lock file should contain closing system tag")

	// Verify built-in prompts are within system tags
	systemOpenPos := strings.Index(lockStr, "<system>")
	systemClosePos := strings.Index(lockStr, "</system>")
	tempFolderPos := strings.Index(lockStr, "temp_folder_prompt.md")
	playwrightPos := strings.Index(lockStr, "playwright_prompt.md")

	assert.Less(t, systemOpenPos, tempFolderPos, "Built-in prompts should be after system tag opens")
	assert.Less(t, tempFolderPos, systemClosePos, "Built-in prompts should be before system tag closes")
	assert.Less(t, playwrightPos, systemClosePos, "Playwright should be before system tag closes")

	// Verify user prompt is after system tags
	// With runtime-import, the actual content is in the original workflow file
	// The lock file should contain the runtime-import macro after system tags
	runtimeImportPos := strings.Index(lockStr, "{{#runtime-import")
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

	// Even minimal workflow should have system tags
	assert.Contains(t, lockStr, "<system>", "Minimal workflow should have system tags")
	assert.Contains(t, lockStr, "</system>", "Minimal workflow should have closing system tag")

	// Should have at least temp folder
	assert.Contains(t, lockStr, "temp_folder_prompt.md", "Should have temp folder prompt")

	// User prompt should be after system tags
	// With runtime-import, the actual content is in the original workflow file
	// The lock file should contain the runtime-import macro after system tags
	systemClosePos := strings.Index(lockStr, "</system>")
	runtimeImportPos := strings.Index(lockStr, "{{#runtime-import")
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

	// Verify safe-outputs section is within system tags
	systemOpenPos := strings.Index(lockStr, "<system>")
	systemClosePos := strings.Index(lockStr, "</system>")
	safeOutputsPos := strings.Index(lockStr, "<safe-outputs>")

	require.NotEqual(t, -1, safeOutputsPos, "Should have safe-outputs section")
	assert.Less(t, systemOpenPos, safeOutputsPos, "Safe outputs should be after system tag opens")
	assert.Less(t, safeOutputsPos, systemClosePos, "Safe outputs should be before system tag closes")

	// Should mention the specific tools
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
