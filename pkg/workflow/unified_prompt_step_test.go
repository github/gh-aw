//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			result := stringutil.NormalizeLeadingWhitespace(tt.input)
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

func TestCollectPromptSections_DisableXPIA(t *testing.T) {
	// Test that XPIA section is excluded when disable-xpia-prompt feature flag is set
	compiler := &Compiler{
		trialMode:            false,
		trialLogicalRepoSlug: "",
	}

	// Test with feature flag disabled (default - XPIA should be included)
	t.Run("XPIA included by default", func(t *testing.T) {
		data := &WorkflowData{
			ParsedTools: NewTools(map[string]any{}),
			Features:    nil,
		}

		sections := compiler.collectPromptSections(data)
		require.NotEmpty(t, sections, "Should collect sections")

		// Check that XPIA section is included
		hasXPIA := false
		for _, section := range sections {
			if section.IsFile && section.Content == xpiaPromptFile {
				hasXPIA = true
				break
			}
		}
		assert.True(t, hasXPIA, "XPIA section should be included by default")
	})

	// Test with feature flag enabled (XPIA should be excluded)
	t.Run("XPIA excluded when feature flag enabled", func(t *testing.T) {
		data := &WorkflowData{
			ParsedTools: NewTools(map[string]any{}),
			Features: map[string]any{
				"disable-xpia-prompt": true,
			},
		}

		sections := compiler.collectPromptSections(data)
		require.NotEmpty(t, sections, "Should still collect other sections")

		// Check that XPIA section is NOT included
		hasXPIA := false
		for _, section := range sections {
			if section.IsFile && section.Content == xpiaPromptFile {
				hasXPIA = true
				break
			}
		}
		assert.False(t, hasXPIA, "XPIA section should be excluded when feature flag is enabled")
	})

	// Test with feature flag explicitly disabled (XPIA should be included)
	t.Run("XPIA included when feature flag explicitly disabled", func(t *testing.T) {
		data := &WorkflowData{
			ParsedTools: NewTools(map[string]any{}),
			Features: map[string]any{
				"disable-xpia-prompt": false,
			},
		}

		sections := compiler.collectPromptSections(data)
		require.NotEmpty(t, sections, "Should collect sections")

		// Check that XPIA section is included
		hasXPIA := false
		for _, section := range sections {
			if section.IsFile && section.Content == xpiaPromptFile {
				hasXPIA = true
				break
			}
		}
		assert.True(t, hasXPIA, "XPIA section should be included when feature flag is explicitly false")
	})
}

// TestCollectPromptSections_GitHubMCPAndSafeOutputsConsistency is a regression test that
// ensures the generated prompt never assigns "all GitHub operations" to safeoutputs when
// the GitHub MCP server is also mounted, and that GitHub MCP read guidance is always present.
func TestCollectPromptSections_GitHubMCPAndSafeOutputsConsistency(t *testing.T) {
	t.Run("both GitHub MCP and safe-outputs enabled", func(t *testing.T) {
		compiler := &Compiler{}

		data := &WorkflowData{
			ParsedTools: NewTools(map[string]any{"github": true}),
			SafeOutputs: &SafeOutputsConfig{
				MissingData: &MissingDataConfig{},
				NoOp:        &NoOpConfig{},
			},
		}

		sections := compiler.collectPromptSections(data)
		require.NotEmpty(t, sections, "Should collect sections")

		// No inline section should claim safeoutputs handles "all GitHub operations"
		for _, section := range sections {
			if !section.IsFile {
				assert.NotContains(t, section.Content, "all GitHub operations",
					"Prompt must not claim safeoutputs handles all GitHub operations when GitHub MCP is mounted")
			}
		}

		// The with-safeoutputs variant of the GitHub MCP guidance file must be selected
		var githubMCPSection *PromptSection
		for i := range sections {
			if sections[i].IsFile && strings.Contains(sections[i].Content, "github_mcp_tools") {
				githubMCPSection = &sections[i]
				break
			}
		}
		require.NotNil(t, githubMCPSection, "Should include github_mcp_tools file when GitHub MCP is enabled")
		assert.Equal(t, githubMCPToolsWithSafeOutputsPromptFile, githubMCPSection.Content,
			"Should use the with-safeoutputs variant when both GitHub MCP and safe-outputs are enabled")
	})

	t.Run("only GitHub MCP enabled (no safe-outputs)", func(t *testing.T) {
		compiler := &Compiler{}

		data := &WorkflowData{
			ParsedTools: NewTools(map[string]any{"github": true}),
			SafeOutputs: nil,
		}

		sections := compiler.collectPromptSections(data)
		require.NotEmpty(t, sections, "Should collect sections")

		// The base GitHub MCP guidance file must be selected (without safeoutputs)
		var githubMCPSection *PromptSection
		for i := range sections {
			if sections[i].IsFile && strings.Contains(sections[i].Content, "github_mcp_tools") {
				githubMCPSection = &sections[i]
				break
			}
		}
		require.NotNil(t, githubMCPSection, "Should include github_mcp_tools file even without safe-outputs")
		assert.Equal(t, githubMCPToolsPromptFile, githubMCPSection.Content,
			"Should use the base variant when only GitHub MCP is enabled")
	})

	t.Run("no GitHub MCP tool", func(t *testing.T) {
		compiler := &Compiler{}

		data := &WorkflowData{
			ParsedTools: NewTools(map[string]any{}),
			SafeOutputs: &SafeOutputsConfig{
				MissingData: &MissingDataConfig{},
				NoOp:        &NoOpConfig{},
			},
		}

		sections := compiler.collectPromptSections(data)

		// Without GitHub MCP there should be no github_mcp_tools file section
		for _, section := range sections {
			if section.IsFile {
				assert.NotContains(t, section.Content, "github_mcp_tools",
					"Should not include GitHub MCP guidance when GitHub tool is not enabled")
			}
		}
	})
}

// TestCollectPromptSections_CliProxy tests that when GitHub gh-proxy mode is
// enabled, the cli-proxy prompt is used instead of the GitHub MCP tools prompt,
// and that the GitHub MCP server guidance is never injected.
func TestCollectPromptSections_CliProxy(t *testing.T) {
	t.Run("cli-proxy enabled without safe-outputs uses cli_proxy_prompt", func(t *testing.T) {
		compiler := &Compiler{}

		data := &WorkflowData{
			Tools:       map[string]any{"github": map[string]any{"mode": "gh-proxy"}},
			ParsedTools: NewTools(map[string]any{"github": true}),
			SafeOutputs: nil,
		}

		sections := compiler.collectPromptSections(data)
		require.NotEmpty(t, sections, "Should collect sections")

		// Should include cli-proxy prompt
		var cliProxySection *PromptSection
		for i := range sections {
			if sections[i].IsFile && sections[i].Content == cliProxyPromptFile {
				cliProxySection = &sections[i]
				break
			}
		}
		require.NotNil(t, cliProxySection, "Should include cli_proxy_prompt.md when cli-proxy is enabled")

		// Should NOT include GitHub MCP tools prompt
		for _, section := range sections {
			assert.NotEqual(t, githubMCPToolsPromptFile, section.Content,
				"Should not include github_mcp_tools_prompt.md when cli-proxy is enabled")
			assert.NotEqual(t, githubMCPToolsWithSafeOutputsPromptFile, section.Content,
				"Should not include github_mcp_tools_with_safeoutputs_prompt.md when cli-proxy is enabled")
		}
	})

	t.Run("tools.github.mode gh-proxy uses cli_proxy_prompt without legacy feature flag", func(t *testing.T) {
		compiler := &Compiler{}

		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"mode": "gh-proxy"},
			},
			ParsedTools: NewTools(map[string]any{"github": map[string]any{"mode": "gh-proxy"}}),
			SafeOutputs: nil,
		}

		sections := compiler.collectPromptSections(data)
		require.NotEmpty(t, sections, "Should collect sections")

		var cliProxySection *PromptSection
		for i := range sections {
			if sections[i].IsFile && sections[i].Content == cliProxyPromptFile {
				cliProxySection = &sections[i]
				break
			}
		}
		require.NotNil(t, cliProxySection, "Should include cli_proxy_prompt.md when tools.github.mode is gh-proxy")
	})

	t.Run("cli-proxy enabled with safe-outputs uses cli_proxy_with_safeoutputs_prompt", func(t *testing.T) {
		compiler := &Compiler{}

		data := &WorkflowData{
			Tools:       map[string]any{"github": map[string]any{"mode": "gh-proxy"}},
			ParsedTools: NewTools(map[string]any{"github": true}),
			SafeOutputs: &SafeOutputsConfig{
				MissingData: &MissingDataConfig{},
				NoOp:        &NoOpConfig{},
			},
		}

		sections := compiler.collectPromptSections(data)
		require.NotEmpty(t, sections, "Should collect sections")

		// Should include the with-safeoutputs cli-proxy prompt
		var cliProxySection *PromptSection
		for i := range sections {
			if sections[i].IsFile && sections[i].Content == cliProxyWithSafeOutputsPromptFile {
				cliProxySection = &sections[i]
				break
			}
		}
		require.NotNil(t, cliProxySection, "Should include cli_proxy_with_safeoutputs_prompt.md when cli-proxy and safe-outputs are both enabled")

		// Should NOT include GitHub MCP tools prompt
		for _, section := range sections {
			assert.NotEqual(t, githubMCPToolsPromptFile, section.Content,
				"Should not include github_mcp_tools_prompt.md when cli-proxy is enabled")
			assert.NotEqual(t, githubMCPToolsWithSafeOutputsPromptFile, section.Content,
				"Should not include github_mcp_tools_with_safeoutputs_prompt.md when cli-proxy is enabled")
		}
	})

	t.Run("legacy features flag does not enable cli-proxy prompt", func(t *testing.T) {
		compiler := &Compiler{}

		data := &WorkflowData{
			ParsedTools: NewTools(map[string]any{}),
			Features:    map[string]any{"cli-proxy": true},
			SafeOutputs: nil,
		}

		sections := compiler.collectPromptSections(data)

		for i := range sections {
			if sections[i].IsFile && sections[i].Content == cliProxyPromptFile {
				t.Fatal("legacy features flag must not add cli_proxy_prompt.md")
			}
		}
	})

	t.Run("cli-proxy disabled uses GitHub MCP tools prompt when github tool is enabled", func(t *testing.T) {
		compiler := &Compiler{}

		data := &WorkflowData{
			ParsedTools: NewTools(map[string]any{"github": true}),
			Features:    nil,
			SafeOutputs: nil,
		}

		sections := compiler.collectPromptSections(data)

		// Should include the standard GitHub MCP tools prompt
		var githubMCPSection *PromptSection
		for i := range sections {
			if sections[i].IsFile && strings.Contains(sections[i].Content, "github_mcp_tools") {
				githubMCPSection = &sections[i]
				break
			}
		}
		require.NotNil(t, githubMCPSection, "Should include github_mcp_tools file when cli-proxy is not enabled")
		assert.Equal(t, githubMCPToolsPromptFile, githubMCPSection.Content,
			"Should use the standard github_mcp_tools_prompt when cli-proxy is not enabled")

		// Should NOT include cli-proxy prompt
		for _, section := range sections {
			assert.NotEqual(t, cliProxyPromptFile, section.Content,
				"Should not include cli_proxy_prompt.md when cli-proxy is not enabled")
			assert.NotEqual(t, cliProxyWithSafeOutputsPromptFile, section.Content,
				"Should not include cli_proxy_with_safeoutputs_prompt.md when cli-proxy is not enabled")
		}
	})
}

func TestCollectPromptSections_PRCommentPushToPRBranchGuidance(t *testing.T) {
	compiler := &Compiler{}

	t.Run("includes guidance when PR comment triggers and push-to-pr-branch is configured", func(t *testing.T) {
		data := &WorkflowData{
			On:          "issue_comment",
			Permissions: "contents: read",
			SafeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
		}

		sections := compiler.collectPromptSections(data)

		var guidanceSection *PromptSection
		for i := range sections {
			if sections[i].IsFile && sections[i].Content == prContextPushToPRBranchGuidanceFile {
				guidanceSection = &sections[i]
				break
			}
		}

		require.NotNil(t, guidanceSection, "Should include push-to-PR-branch guidance for PR comment workflows")
		assert.Equal(t, "GH_AW_INCLUDE_PR_CONTEXT", guidanceSection.ConditionEnvVar, "Guidance should be conditionally injected for PR-comment events")
		assert.Contains(t, guidanceSection.EnvVars["GH_AW_INCLUDE_PR_CONTEXT"], "github.event_name")
	})

	t.Run("includes guidance when pull_request_review_comment triggers and push-to-pr-branch is configured", func(t *testing.T) {
		data := &WorkflowData{
			On:          "pull_request_review_comment",
			Permissions: "contents: read",
			SafeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
		}

		sections := compiler.collectPromptSections(data)

		var guidanceSection *PromptSection
		for i := range sections {
			if sections[i].IsFile && sections[i].Content == prContextPushToPRBranchGuidanceFile {
				guidanceSection = &sections[i]
				break
			}
		}

		require.NotNil(t, guidanceSection, "Should include push-to-PR-branch guidance for pull_request_review_comment workflows")
		assert.Equal(t, "GH_AW_INCLUDE_PR_CONTEXT", guidanceSection.ConditionEnvVar, "Guidance should be conditionally injected for PR-comment events")
	})

	t.Run("does not include guidance when push-to-pr-branch is not configured", func(t *testing.T) {
		data := &WorkflowData{
			On:          "issue_comment",
			Permissions: "contents: read",
			SafeOutputs: &SafeOutputsConfig{},
		}

		sections := compiler.collectPromptSections(data)

		for _, section := range sections {
			assert.NotEqual(t, prContextPushToPRBranchGuidanceFile, section.Content,
				"Should not include push-to-PR-branch guidance unless tool is configured")
		}
	})

	t.Run("does not include guidance when SafeOutputs is nil", func(t *testing.T) {
		data := &WorkflowData{
			On:          "issue_comment",
			Permissions: "contents: read",
			SafeOutputs: nil,
		}

		sections := compiler.collectPromptSections(data)

		for _, section := range sections {
			assert.NotEqual(t, prContextPushToPRBranchGuidanceFile, section.Content,
				"Should not include push-to-PR-branch guidance when SafeOutputs is nil")
		}
	})
}

// TestGenerateUnifiedPromptCreationStep_TrailingWhitespace verifies that trailing
// whitespace is stripped from every user-prompt content line.
func TestGenerateUnifiedPromptCreationStep_TrailingWhitespace(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}
	builtinSections := compiler.collectPromptSections(data)

	// Chunk lines with assorted trailing whitespace
	userPromptChunks := []string{"line one   \nline two\t\nline three  \t  "}

	var sb strings.Builder
	compiler.generateUnifiedPromptCreationStep(&sb, builtinSections, userPromptChunks, nil, data)
	output := sb.String()

	assert.NotContains(t, output, "line one   ", "trailing spaces should be stripped")
	assert.NotContains(t, output, "line two\t", "trailing tab should be stripped")
	assert.NotContains(t, output, "line three  \t  ", "mixed trailing whitespace should be stripped")
	assert.Contains(t, output, `line one\nline two\nline three\n`, "normalized content should be stored in a data environment variable")
}

// TestGenerateUnifiedPromptCreationStep_BlankRunCapWithinChunk verifies that a run of
// more than maxConsecutiveBlankLines blank lines inside a single chunk is capped.
func TestGenerateUnifiedPromptCreationStep_BlankRunCapWithinChunk(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}
	builtinSections := compiler.collectPromptSections(data)

	// Four consecutive blank lines between content lines – should be collapsed to two.
	userPromptChunks := []string{"# Start\n\n\n\n\n# End"}

	var sb strings.Builder
	compiler.generateUnifiedPromptCreationStep(&sb, builtinSections, userPromptChunks, nil, data)
	output := sb.String()

	assert.Contains(t, output, "# Start", "start marker should be present")
	assert.Contains(t, output, "# End", "end marker should be present")

	// At most maxConsecutiveBlankLines (2) blank lines should appear between them.
	startIdx := strings.Index(output, "# Start")
	endIdx := strings.Index(output, "# End")
	require.Less(t, startIdx, endIdx)
	between := output[startIdx:endIdx]
	// Three or more consecutive newlines would indicate a blank run of >2.
	assert.NotContains(t, between, "\n\n\n\n", "blank run should be capped at 2")
}

// TestGenerateUnifiedPromptCreationStep_BlankRunCapAcrossChunks verifies that a blank
// run straddling two adjacent chunks is still capped correctly.
func TestGenerateUnifiedPromptCreationStep_BlankRunCapAcrossChunks(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}
	builtinSections := compiler.collectPromptSections(data)

	// chunk1 ends with two blank lines; chunk2 starts with two blank lines.
	// Across the boundary that would be four consecutive blanks; they should be collapsed.
	userPromptChunks := []string{
		"# Chunk1\n\n\n",
		"\n\n# Chunk2",
	}

	var sb strings.Builder
	compiler.generateUnifiedPromptCreationStep(&sb, builtinSections, userPromptChunks, nil, data)
	output := sb.String()

	startIdx := strings.Index(output, "# Chunk1")
	endIdx := strings.Index(output, "# Chunk2")
	require.Less(t, startIdx, endIdx)
	between := output[startIdx:endIdx]
	assert.NotContains(t, between, "\n\n\n\n", "blank run across chunk boundary should be capped at 2")
}

// TestGenerateUnifiedPromptCreationStep_BlankRunCapAdjacentToRuntimeImport verifies
// that blank-run tracking resets correctly around a runtime-import macro so blank lines
// on both sides of the macro are handled independently.
func TestGenerateUnifiedPromptCreationStep_BlankRunCapAdjacentToRuntimeImport(t *testing.T) {
	compiler := &Compiler{}
	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}
	builtinSections := compiler.collectPromptSections(data)

	// chunk[0] ends with a long blank run, chunk[1] is a runtime-import macro,
	// chunk[2] starts with a long blank run.
	userPromptChunks := []string{
		"# Before\n\n\n\n",
		"{{#runtime-import docs/task.md}}",
		"\n\n\n\n# After",
	}

	var sb strings.Builder
	compiler.generateUnifiedPromptCreationStep(&sb, builtinSections, userPromptChunks, nil, data)
	output := sb.String()

	// Runtime-import macro must be present verbatim.
	assert.Contains(t, output, "{{#runtime-import docs/task.md}}", "runtime-import macro must be emitted verbatim")

	// Content markers must appear.
	assert.Contains(t, output, "# Before", "before-marker should be present")
	assert.Contains(t, output, "# After", "after-marker should be present")

	// No run of four or more newlines (i.e., 3+ consecutive blank lines) anywhere.
	assert.NotContains(t, output, "\n\n\n\n", "blank run should be capped throughout the output")
}

func TestCollectPromptSections_TrialLogicalRepoGitHubContext(t *testing.T) {
	compiler := &Compiler{}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"github": true,
		}),
		Permissions:      "contents: read",
		On:               "issue_comment",
		TrialMode:        true,
		TrialLogicalRepo: "owner/target",
	}

	sections := compiler.collectPromptSections(data)

	var githubContext string
	for _, section := range sections {
		if !section.IsFile && strings.Contains(section.Content, "github-context") {
			githubContext = section.Content
			break
		}
	}
	require.NotEmpty(t, githubContext, "Should have a github-context section")

	assert.Contains(t, githubContext, "- **repository**: owner/target",
		"github-context should report the logical target repository")
	assert.NotContains(t, githubContext, "**repository**: ${{ github.repository }}",
		"github-context should not report the host repository in trial mode")
	assert.Contains(t, githubContext, "trial mode",
		"github-context should include a trial mode directive")
}

func TestCollectPromptSections_NoTrialLogicalRepoKeepsHostRepo(t *testing.T) {
	compiler := &Compiler{}

	data := &WorkflowData{
		ParsedTools: NewTools(map[string]any{
			"github": true,
		}),
		Permissions: "contents: read",
		On:          "issue_comment",
	}

	sections := compiler.collectPromptSections(data)

	var githubContext string
	for _, section := range sections {
		if !section.IsFile && strings.Contains(section.Content, "github-context") {
			githubContext = section.Content
			break
		}
	}
	require.NotEmpty(t, githubContext, "Should have a github-context section")

	assert.Contains(t, githubContext, "**repository**:",
		"github-context should report the repository via the github.repository expression")
	assert.NotContains(t, githubContext, "owner/target",
		"github-context should not include a logical target when trial mode is off")
}
