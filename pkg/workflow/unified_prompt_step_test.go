//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateUnifiedPromptStep_AllSections(t *testing.T) {
	// Test that all prompt sections are included when all features are enabled
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
			Caches: []CacheMemoryEntry{
				{ID: "default"},
			},
		},
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{ID: "default", BranchName: "memory"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
		Permissions: "contents: read",
		On:          "issue_comment",
	}

	var yaml strings.Builder
	compiler.generateUnifiedPromptStep(&yaml, data)

	output := yaml.String()

	// Verify single step is created with correct name
	assert.Contains(t, output, "- name: Create prompt with built-in context")

	// Verify all sections are included
	assert.Contains(t, output, "temp_folder_prompt.md", "Should include temp folder instructions")
	assert.Contains(t, output, "playwright_prompt.md", "Should include playwright instructions")
	assert.Contains(t, output, "cache_memory_prompt.md", "Should include cache memory template file")
	assert.Contains(t, output, "Repo Memory Available", "Should include repo memory instructions")
	assert.Contains(t, output, "<safe-outputs>", "Should include safe outputs instructions")
	assert.Contains(t, output, "<github-context>", "Should include GitHub context")

	// Verify cache env vars are NOT in the prompt creation step
	promptStepStart := strings.Index(output, "- name: Create prompt with built-in context")
	promptStepEnd := strings.Index(output, "- name:")
	if promptStepEnd <= promptStepStart {
		promptStepEnd = len(output)
	}
	promptStep := output[promptStepStart:promptStepEnd]
	assert.NotContains(t, promptStep, "GH_AW_CACHE_DIR:", "Cache env vars should not be in prompt creation step")

	// Verify environment variables are declared at the top
	lines := strings.Split(output, "\n")
	envSectionStarted := false
	runSectionStarted := false
	for _, line := range lines {
		if strings.Contains(line, "env:") {
			envSectionStarted = true
		}
		if strings.Contains(line, "run: |") {
			runSectionStarted = true
		}
		// Check that environment variable declarations (key: ${{ ... }}) are in env section
		// Skip lines that are just references to the variables (like __GH_AW_GITHUB_ACTOR__)
		if strings.Contains(line, ": ${{") && runSectionStarted {
			t.Errorf("Found environment variable declaration after run section started: %s", line)
		}
	}
	assert.True(t, envSectionStarted, "Should have env section")
	assert.True(t, runSectionStarted, "Should have run section")
}

func TestGenerateUnifiedPromptStep_MinimalSections(t *testing.T) {
	// Test that only temp folder is included when no other features are enabled
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools:       NewTools(map[string]any{}),
		CacheMemoryConfig: nil,
		RepoMemoryConfig:  nil,
		SafeOutputs:       nil,
		Permissions:       "",
		On:                "push",
	}

	var yaml strings.Builder
	compiler.generateUnifiedPromptStep(&yaml, data)

	output := yaml.String()

	// Verify single step is created
	assert.Contains(t, output, "- name: Create prompt with built-in context")

	// Verify only temp folder is included
	assert.Contains(t, output, "temp_folder_prompt.md", "Should include temp folder instructions")

	// Verify other sections are NOT included
	assert.NotContains(t, output, "playwright_prompt.md", "Should not include playwright without tool")
	assert.NotContains(t, output, "cache_memory_prompt.md", "Should not include cache memory template without config")
	assert.NotContains(t, output, "Repo Memory Available", "Should not include repo memory without config")
	assert.NotContains(t, output, "<safe-outputs>", "Should not include safe outputs without config")
	assert.NotContains(t, output, "<github-context>", "Should not include GitHub context without tool")
}

func TestGenerateUnifiedPromptStep_TrialMode(t *testing.T) {
	// Test that trial mode note is included
	compiler := &Compiler{
		trialMode:            true,
		trialLogicalRepoSlug: "owner/repo",
	}

	data := &WorkflowData{
		ParsedTools:       NewTools(map[string]any{}),
		CacheMemoryConfig: nil,
		RepoMemoryConfig:  nil,
		SafeOutputs:       nil,
		Permissions:       "",
		On:                "push",
	}

	var yaml strings.Builder
	compiler.generateUnifiedPromptStep(&yaml, data)

	output := yaml.String()

	// Verify trial mode note is included
	assert.Contains(t, output, "## Note")
	assert.Contains(t, output, "owner/repo")
}

func TestGenerateUnifiedPromptStep_PRContext(t *testing.T) {
	// Test that PR context is included with proper condition
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools:       NewTools(map[string]any{}),
		CacheMemoryConfig: nil,
		RepoMemoryConfig:  nil,
		SafeOutputs:       nil,
		Permissions:       "contents: read",
		On:                "issue_comment",
	}

	var yaml strings.Builder
	compiler.generateUnifiedPromptStep(&yaml, data)

	output := yaml.String()

	// Verify PR context is included with condition
	assert.Contains(t, output, "pr_context_prompt.md", "Should include PR context file")
	assert.Contains(t, output, "if [", "Should have shell conditional for PR context")
	assert.Contains(t, output, "GITHUB_EVENT_NAME", "Should check event name")
}

func TestCollectPromptSections_Order(t *testing.T) {
	// Test that sections are collected in the correct order
	compiler := &Compiler{
		trialMode:            true,
		trialLogicalRepoSlug: "owner/repo",
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
		Permissions: "contents: read",
		On:          "issue_comment",
	}

	sections := compiler.collectPromptSections(data)

	// Verify we have sections
	require.NotEmpty(t, sections, "Should collect sections")

	// Verify order:
	// 1. Temp folder
	// 2. Playwright
	// 3. Trial mode note
	// 4. Cache memory
	// 5. Repo memory
	// 6. Safe outputs
	// 7. GitHub context
	// 8. PR context

	var sectionTypes []string
	for _, section := range sections {
		if section.IsFile {
			if strings.Contains(section.Content, "temp_folder") {
				sectionTypes = append(sectionTypes, "temp")
			} else if strings.Contains(section.Content, "playwright") {
				sectionTypes = append(sectionTypes, "playwright")
			} else if strings.Contains(section.Content, "pr_context") {
				sectionTypes = append(sectionTypes, "pr-context")
			}
		} else {
			if strings.Contains(section.Content, "## Note") {
				sectionTypes = append(sectionTypes, "trial")
			} else if strings.Contains(section.Content, "Cache Folder") {
				sectionTypes = append(sectionTypes, "cache")
			} else if strings.Contains(section.Content, "Repo Memory") {
				sectionTypes = append(sectionTypes, "repo")
			} else if strings.Contains(section.Content, "safe-outputs") {
				sectionTypes = append(sectionTypes, "safe-outputs")
			} else if strings.Contains(section.Content, "github-context") {
				sectionTypes = append(sectionTypes, "github")
			}
		}
	}

	// Verify expected order (not all may be present, but order should be maintained)
	expectedOrder := []string{"temp", "playwright", "trial", "cache", "repo", "safe-outputs", "github", "pr-context"}

	// Check that the sections we found appear in the expected order
	lastIndex := -1
	for _, sectionType := range sectionTypes {
		currentIndex := -1
		for i, expected := range expectedOrder {
			if expected == sectionType {
				currentIndex = i
				break
			}
		}
		assert.Greater(t, currentIndex, lastIndex, "Section %s should appear after previous section", sectionType)
		lastIndex = currentIndex
	}
}

func TestGenerateUnifiedPromptStep_NoSections(t *testing.T) {
	// This should never happen in practice, but test the edge case
	compiler := &Compiler{
		trialMode: false,
	}

	// Create minimal data that would result in at least temp folder
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	var yaml strings.Builder
	compiler.generateUnifiedPromptStep(&yaml, data)

	output := yaml.String()

	// Should still generate step with at least temp folder
	assert.Contains(t, output, "- name: Create prompt with built-in context")
	assert.Contains(t, output, "temp_folder_prompt.md")
}

func TestNormalizeLeadingWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "removes consistent leading spaces",
			input: `          Line 1
          Line 2
          Line 3`,
			expected: `Line 1
Line 2
Line 3`,
		},
		{
			name:     "handles no leading spaces",
			input:    "Line 1\nLine 2",
			expected: "Line 1\nLine 2",
		},
		{
			name: "preserves relative indentation",
			input: `          Line 1
            Indented Line 2
          Line 3`,
			expected: `Line 1
  Indented Line 2
Line 3`,
		},
		{
			name: "handles empty lines",
			input: `          Line 1

          Line 3`,
			expected: `Line 1

Line 3`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeLeadingWhitespace(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRemoveConsecutiveEmptyLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "removes consecutive empty lines",
			input: `Line 1


Line 2`,
			expected: `Line 1

Line 2`,
		},
		{
			name: "keeps single empty lines",
			input: `Line 1

Line 2

Line 3`,
			expected: `Line 1

Line 2

Line 3`,
		},
		{
			name: "handles multiple consecutive empty lines",
			input: `Line 1




Line 2`,
			expected: `Line 1

Line 2`,
		},
		{
			name:     "handles no empty lines",
			input:    "Line 1\nLine 2\nLine 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name: "handles empty lines at start",
			input: `

Line 1`,
			expected: `
Line 1`,
		},
		{
			name: "handles empty lines at end",
			input: `Line 1


`,
			expected: `Line 1
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeConsecutiveEmptyLines(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateUnifiedPromptStep_EnvVarsSorted(t *testing.T) {
	// Test that environment variables are sorted alphabetically
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"github": true,
		}),
		CacheMemoryConfig: nil,
		RepoMemoryConfig:  nil,
		SafeOutputs:       nil,
		Permissions:       "",
		On:                "push",
	}

	var yaml strings.Builder
	compiler.generateUnifiedPromptStep(&yaml, data)

	output := yaml.String()

	// Verify environment variables are present and sorted
	lines := strings.Split(output, "\n")
	envSectionStarted := false
	runSectionStarted := false
	var envVarLines []string

	for _, line := range lines {
		if strings.Contains(line, "env:") {
			envSectionStarted = true
			continue
		}
		if strings.Contains(line, "run: |") {
			runSectionStarted = true
			break
		}
		if envSectionStarted && strings.Contains(line, ": ${{") {
			// Extract just the variable name (before the colon)
			trimmed := strings.TrimSpace(line)
			colonIndex := strings.Index(trimmed, ":")
			if colonIndex > 0 {
				varName := trimmed[:colonIndex]
				envVarLines = append(envVarLines, varName)
			}
		}
	}

	assert.True(t, runSectionStarted, "Should have found run section")

	// Verify that environment variables (excluding GH_AW_PROMPT which is always first) are sorted
	// Skip the first entry which is GH_AW_PROMPT
	if len(envVarLines) > 0 {
		// Check that the remaining variables are in sorted order
		for i := 0; i < len(envVarLines)-1; i++ {
			current := envVarLines[i]
			next := envVarLines[i+1]
			if current > next {
				t.Errorf("Environment variables are not sorted: %s comes before %s", current, next)
			}
		}
	}
}
