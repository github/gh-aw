//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpec_PublicAPI_ExtractFrontmatterFromContent validates the documented
// behavior of ExtractFrontmatterFromContent as described in the package README.md.
//
// Specification: Extracts YAML frontmatter between --- delimiters from markdown.
// The markdown body that follows the frontmatter serves as the AI agent's prompt text.
func TestSpec_PublicAPI_ExtractFrontmatterFromContent(t *testing.T) {
	t.Run("extracts YAML frontmatter between --- delimiters", func(t *testing.T) {
		content := "---\non: push\n---\n# My Workflow\nSome prompt text."
		result, err := ExtractFrontmatterFromContent(content)
		require.NoError(t, err,
			"ExtractFrontmatterFromContent should not error on valid frontmatter")
		require.NotNil(t, result,
			"ExtractFrontmatterFromContent should return non-nil result")
		assert.NotNil(t, result.Frontmatter["on"],
			"result.Frontmatter should contain the 'on' key from YAML")
	})

	t.Run("markdown body follows frontmatter block", func(t *testing.T) {
		content := "---\non: push\n---\n# My Workflow\nPrompt text here."
		result, err := ExtractFrontmatterFromContent(content)
		require.NoError(t, err,
			"ExtractFrontmatterFromContent should not error on valid content")
		assert.Contains(t, result.Markdown, "Prompt text here",
			"result.Markdown should contain the body text after frontmatter")
	})

	t.Run("content without frontmatter delimiter returns empty frontmatter", func(t *testing.T) {
		content := "# Just markdown\nNo frontmatter here."
		result, err := ExtractFrontmatterFromContent(content)
		require.NoError(t, err,
			"ExtractFrontmatterFromContent should not error on content without frontmatter")
		assert.Empty(t, result.Frontmatter,
			"result.Frontmatter should be empty when no --- delimiter is present")
	})
}

// TestSpec_PublicAPI_ExtractMarkdownSection validates the documented behavior
// of ExtractMarkdownSection as described in the package README.md.
//
// Specification: Extracts a named ## section from markdown.
func TestSpec_PublicAPI_ExtractMarkdownSection(t *testing.T) {
	t.Run("extracts the named section content", func(t *testing.T) {
		content := "# Title\n\n## Tools\n\nSome tool config.\n\n## Other\n\nOther stuff."
		section, err := ExtractMarkdownSection(content, "Tools")
		require.NoError(t, err,
			"ExtractMarkdownSection should not error when section exists")
		assert.Contains(t, section, "Some tool config",
			"extracted section should contain the body following the matched heading")
	})

	t.Run("returns error when section is not found", func(t *testing.T) {
		content := "# Title\n\n## Tools\n\nSome tool config."
		_, err := ExtractMarkdownSection(content, "Missing")
		assert.Error(t, err,
			"ExtractMarkdownSection should return error when the requested section is absent")
	})

	t.Run("stops at next same-or-higher level heading", func(t *testing.T) {
		content := "## Tools\n\nTool A.\n\n## Engine\n\nEngine X."
		section, err := ExtractMarkdownSection(content, "Tools")
		require.NoError(t, err,
			"ExtractMarkdownSection should not error when section exists")
		assert.Contains(t, section, "Tool A",
			"extracted section should contain text from inside the matched section")
		assert.NotContains(t, section, "Engine X",
			"extracted section should not bleed into the next ## section")
	})
}

// TestSpec_PublicAPI_IsWorkflowSpec validates the documented behavior of
// IsWorkflowSpec as described in the package README.md.
//
// Specification: Returns whether a path is a workflow specification markdown file
// (i.e. of the form owner/repo/path[@ref]).
func TestSpec_PublicAPI_IsWorkflowSpec(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "owner/repo/path is recognised as workflowspec",
			input:    "github/gh-aw/workflows/build.md",
			expected: true,
		},
		{
			name:     "owner/repo/path@ref is recognised as workflowspec",
			input:    "github/gh-aw/workflows/build.md@main",
			expected: true,
		},
		{
			name:     "local relative path is not a workflowspec",
			input:    ".github/workflows/build.md",
			expected: false,
		},
		{
			name:     "shared/ prefixed path is not a workflowspec",
			input:    "shared/base.md",
			expected: false,
		},
		{
			name:     "single-segment path is not a workflowspec",
			input:    "workflow.md",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWorkflowSpec(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsWorkflowSpec(%q) should match documented workflowspec form", tt.input)
		})
	}
}

// TestSpec_InlineSubAgents_GetEngineSubAgentExt validates the documented
// behavior of GetEngineSubAgentExt as described in the package README.md.
//
// Specification: Returns the file extension for sub-agent files for a given
// engine (.md for claude/codex/gemini, .agent.md otherwise).
func TestSpec_InlineSubAgents_GetEngineSubAgentExt(t *testing.T) {
	tests := []struct {
		name     string
		engineID string
		expected string
	}{
		{
			name:     "claude engine uses .md",
			engineID: "claude",
			expected: ".md",
		},
		{
			name:     "codex engine uses .md",
			engineID: "codex",
			expected: ".md",
		},
		{
			name:     "gemini engine uses .md",
			engineID: "gemini",
			expected: ".md",
		},
		{
			name:     "unknown engine uses .agent.md",
			engineID: "copilot",
			expected: ".agent.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEngineSubAgentExt(tt.engineID)
			assert.Equal(t, tt.expected, result,
				"GetEngineSubAgentExt(%q) should return documented sub-agent file extension", tt.engineID)
		})
	}
}

// TestSpec_PublicAPI_ExtractMarkdownContent validates the documented behavior
// of ExtractMarkdownContent as described in the package README.md.
//
// Specification: Returns the markdown body (everything after frontmatter).
func TestSpec_PublicAPI_ExtractMarkdownContent(t *testing.T) {
	t.Run("returns body after frontmatter block", func(t *testing.T) {
		content := "---\non: push\n---\n# Agent Prompt\nDo the thing."
		body, err := ExtractMarkdownContent(content)
		require.NoError(t, err,
			"ExtractMarkdownContent should not error on valid content")
		assert.Contains(t, body, "Do the thing",
			"ExtractMarkdownContent should return text after the frontmatter block")
	})

	t.Run("content without frontmatter returns full content as body", func(t *testing.T) {
		content := "# Just markdown\nNo frontmatter."
		body, err := ExtractMarkdownContent(content)
		require.NoError(t, err,
			"ExtractMarkdownContent should not error on content without frontmatter")
		assert.Contains(t, body, "No frontmatter",
			"ExtractMarkdownContent should return content as-is when no frontmatter present")
	})
}

// TestSpec_ScheduleDetection_IsCronExpression validates the documented behavior
// of IsCronExpression as described in the package README.md.
//
// Specification: Detects whether a string is already a cron expression.
func TestSpec_ScheduleDetection_IsCronExpression(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "standard 5-field cron returns true",
			input:    "0 9 * * *",
			expected: true,
		},
		{
			name:     "every-5-minutes cron returns true",
			input:    "*/5 * * * *",
			expected: true,
		},
		{
			name:     "natural language schedule returns false",
			input:    "every day at 9am",
			expected: false,
		},
		{
			name:     "empty string returns false",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCronExpression(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsCronExpression(%q) should detect cron format correctly", tt.input)
		})
	}
}

// TestSpec_ScheduleDetection_IsDailyCron validates the documented behavior of
// IsDailyCron as described in the package README.md.
//
// Specification: Detects whether a cron expression runs daily.
func TestSpec_ScheduleDetection_IsDailyCron(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "daily cron at 9am returns true",
			input:    "0 9 * * *",
			expected: true,
		},
		{
			name:     "weekly cron returns false",
			input:    "0 9 * * 1",
			expected: false,
		},
		{
			name:     "hourly cron returns false",
			input:    "0 * * * *",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDailyCron(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsDailyCron(%q) should detect daily cron correctly", tt.input)
		})
	}
}

// TestSpec_ScheduleDetection_IsHourlyCron validates the documented behavior of
// IsHourlyCron as described in the package README.md.
//
// Specification: Detects whether a cron expression runs hourly.
func TestSpec_ScheduleDetection_IsHourlyCron(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			// The implementation requires the hour field to be an interval
			// pattern (*/N) rather than plain *. "0 */1 * * *" runs hourly.
			name:     "hourly cron with interval pattern returns true",
			input:    "0 */1 * * *",
			expected: true,
		},
		{
			name:     "daily cron returns false",
			input:    "0 9 * * *",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsHourlyCron(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsHourlyCron(%q) should detect hourly cron correctly", tt.input)
		})
	}
}

// TestSpec_ScheduleDetection_IsWeeklyCron validates the documented behavior of
// IsWeeklyCron as described in the package README.md.
//
// Specification: Detects whether a cron expression runs weekly.
func TestSpec_ScheduleDetection_IsWeeklyCron(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "weekly on Monday returns true",
			input:    "0 9 * * 1",
			expected: true,
		},
		{
			name:     "daily cron returns false",
			input:    "0 9 * * *",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWeeklyCron(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsWeeklyCron(%q) should detect weekly cron correctly", tt.input)
		})
	}
}

// TestSpec_PublicAPI_IsValidGitHubIdentifier validates the documented behavior
// of IsValidGitHubIdentifier as described in the package README.md.
//
// Specification: Validates a GitHub username/org identifier.
func TestSpec_PublicAPI_IsValidGitHubIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "simple lowercase name is valid",
			input:    "myrepo",
			expected: true,
		},
		{
			name:     "name with hyphens is valid",
			input:    "my-repo",
			expected: true,
		},
		{
			name:     "name with digits is valid",
			input:    "repo123",
			expected: true,
		},
		{
			name:     "empty string is invalid",
			input:    "",
			expected: false,
		},
		{
			name:     "name with slash is invalid",
			input:    "owner/repo",
			expected: false,
		},
		{
			name:     "owner name over 39 chars is invalid",
			input:    "this-owner-name-is-longer-than-thirty-nine",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidGitHubIdentifier(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsValidGitHubIdentifier(%q) should match documented behavior", tt.input)
		})
	}
}

// TestSpec_PublicAPI_IsValidGitHubRepositoryName validates the documented behavior
// of IsValidGitHubRepositoryName as described in the package README.md.
//
// Specification: Validates a GitHub repository name.
func TestSpec_PublicAPI_IsValidGitHubRepositoryName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "repo name over owner length and within 100 chars is valid",
			input:    "this-repository-name-is-significantly-longer-than-thirty-nine",
			expected: true,
		},
		{
			name:     "repo name over 100 chars is invalid",
			input:    "this-repository-name-is-way-too-long-because-it-exceeds-one-hundred-characters-when-you-keep-adding-more",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidGitHubRepositoryName(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsValidGitHubRepositoryName(%q) should match documented behavior", tt.input)
		})
	}
}

// TestSpec_PublicAPI_IsMCPType validates the documented behavior of IsMCPType
// as described in the package README.md.
//
// Specification: Validates an MCP transport type string.
// ValidMCPTypes contains "stdio", "http", "local".
func TestSpec_PublicAPI_IsMCPType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "stdio is a valid MCP type",
			input:    "stdio",
			expected: true,
		},
		{
			name:     "http is a valid MCP type",
			input:    "http",
			expected: true,
		},
		{
			name:     "local is a valid MCP type",
			input:    "local",
			expected: true,
		},
		{
			name:     "unknown type is invalid",
			input:    "grpc",
			expected: false,
		},
		{
			name:     "empty string is invalid",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsMCPType(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsMCPType(%q) should validate against documented MCP transport types", tt.input)
		})
	}
}

// TestSpec_Constants_ValidMCPTypes validates the documented ValidMCPTypes
// variable values as described in the package README.md.
//
// Specification: ValidMCPTypes contains "stdio", "http", "local".
func TestSpec_Constants_ValidMCPTypes(t *testing.T) {
	assert.Contains(t, ValidMCPTypes, "stdio",
		"ValidMCPTypes should contain 'stdio' per specification")
	assert.Contains(t, ValidMCPTypes, "http",
		"ValidMCPTypes should contain 'http' per specification")
	assert.Contains(t, ValidMCPTypes, "local",
		"ValidMCPTypes should contain 'local' per specification")
	assert.Len(t, ValidMCPTypes, 3,
		"ValidMCPTypes should contain exactly the 3 documented types")
}

// TestSpec_PublicAPI_ParseImportDirective validates the documented behavior of
// ParseImportDirective as described in the package README.md.
//
// Specification: Parses a single @import or @include line.
func TestSpec_PublicAPI_ParseImportDirective(t *testing.T) {
	t.Run("@import directive is parsed correctly", func(t *testing.T) {
		line := "@import shared/base.md"
		result := ParseImportDirective(line)
		require.NotNil(t, result,
			"ParseImportDirective should return non-nil for valid @import line")
		assert.Equal(t, "shared/base.md", result.Path,
			"ParseImportDirective should extract the path from @import directive")
	})

	t.Run("@include directive is parsed correctly", func(t *testing.T) {
		line := "@include shared/tools.md"
		result := ParseImportDirective(line)
		require.NotNil(t, result,
			"ParseImportDirective should return non-nil for valid @include line")
		assert.Equal(t, "shared/tools.md", result.Path,
			"ParseImportDirective should extract the path from @include directive")
	})

	t.Run("non-directive line returns nil", func(t *testing.T) {
		line := "# Just a heading"
		result := ParseImportDirective(line)
		assert.Nil(t, result,
			"ParseImportDirective should return nil for non-directive lines")
	})
}

// TestSpec_PublicAPI_NewImportCache validates the documented behavior of
// NewImportCache as described in the package README.md.
//
// Specification: Creates a new import cache rooted at the repository.
// ImportCache is designed for use within a single goroutine per compilation run.
func TestSpec_PublicAPI_NewImportCache(t *testing.T) {
	t.Run("creates non-nil cache for given repo root", func(t *testing.T) {
		cache := NewImportCache("/path/to/repo")
		assert.NotNil(t, cache,
			"NewImportCache should return a non-nil ImportCache")
	})

	t.Run("creates separate cache instances", func(t *testing.T) {
		cache1 := NewImportCache("/repo/a")
		cache2 := NewImportCache("/repo/b")
		assert.NotSame(t, cache1, cache2,
			"NewImportCache should create separate cache instances for concurrent compilations")
	})
}

// TestSpec_PublicAPI_ParseSchedule validates the documented behavior of ParseSchedule
// as described in the package README.md.
//
// Specification: Parses natural-language or cron schedule to a cron expression.
// Example: parser.ParseSchedule("every day at 9am") → cron = "0 9 * * *"
func TestSpec_PublicAPI_ParseSchedule(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedCron string
		wantErr      bool
	}{
		{
			name:         "documented example: every day at 9am",
			input:        "every day at 9am",
			expectedCron: "0 9 * * *",
		},
		{
			name:         "already a cron expression is returned as-is",
			input:        "0 9 * * *",
			expectedCron: "0 9 * * *",
		},
		{
			name:    "empty input returns error",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cron, _, err := ParseSchedule(tt.input)
			if tt.wantErr {
				assert.Error(t, err, "ParseSchedule(%q) should return error", tt.input)
				return
			}
			require.NoError(t, err, "ParseSchedule(%q) unexpected error", tt.input)
			assert.Equal(t, tt.expectedCron, cron,
				"ParseSchedule(%q) should return documented cron expression", tt.input)
		})
	}
}

// TestSpec_ScheduleDetection_IsFuzzyCron validates the documented behavior of
// IsFuzzyCron as described in the package README.md.
//
// Specification: Detects whether a cron is a fuzzy wildcard.
func TestSpec_ScheduleDetection_IsFuzzyCron(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "FUZZY-prefixed value returns true",
			input:    "FUZZY:daily",
			expected: true,
		},
		{
			name:     "standard cron expression returns false",
			input:    "0 9 * * *",
			expected: false,
		},
		{
			name:     "empty string returns false",
			input:    "",
			expected: false,
		},
		{
			name:     "natural language schedule returns false",
			input:    "every day at 9am",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsFuzzyCron(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsFuzzyCron(%q) should detect fuzzy wildcard correctly", tt.input)
		})
	}
}

// TestSpec_PublicAPI_ExtractWorkflowNameFromMarkdownBody validates the documented
// behavior of ExtractWorkflowNameFromMarkdownBody as described in the package README.md.
//
// Specification: Derives the workflow name from the first # heading in the markdown body.
func TestSpec_PublicAPI_ExtractWorkflowNameFromMarkdownBody(t *testing.T) {
	t.Run("extracts name from first H1 heading", func(t *testing.T) {
		body := "# My Workflow\n\nSome prompt text."
		name, err := ExtractWorkflowNameFromMarkdownBody(body, "my-workflow.md")
		require.NoError(t, err,
			"ExtractWorkflowNameFromMarkdownBody should not error on content with H1")
		assert.NotEmpty(t, name,
			"ExtractWorkflowNameFromMarkdownBody should return a non-empty name from H1 heading")
	})

	t.Run("falls back to virtual path when no H1 heading", func(t *testing.T) {
		body := "Just some text without a heading."
		name, err := ExtractWorkflowNameFromMarkdownBody(body, "my-workflow.md")
		require.NoError(t, err,
			"ExtractWorkflowNameFromMarkdownBody should not error when no H1 is present")
		assert.NotEmpty(t, name,
			"ExtractWorkflowNameFromMarkdownBody should return a non-empty fallback name")
	})
}

// TestSpec_PublicAPI_NewFormattedParserError validates the documented behavior of
// NewFormattedParserError as described in the package README.md.
//
// Specification: Creates a pre-formatted parser error.
func TestSpec_PublicAPI_NewFormattedParserError(t *testing.T) {
	t.Run("returns non-nil FormattedParserError implementing error", func(t *testing.T) {
		msg := "error: bad import at line 5"
		err := NewFormattedParserError(msg)
		require.NotNil(t, err, "NewFormattedParserError should return a non-nil error")
		var _ error = err
		assert.Equal(t, msg, err.Error(),
			"NewFormattedParserError.Error() should return the formatted message")
	})

	t.Run("empty message is preserved", func(t *testing.T) {
		err := NewFormattedParserError("")
		require.NotNil(t, err, "NewFormattedParserError should return non-nil for empty message")
		assert.Empty(t, err.Error(),
			"NewFormattedParserError should preserve empty message")
	})
}

// TestSpec_PublicAPI_EnsureToolsSection validates the documented behavior of
// EnsureToolsSection as described in the package README.md.
//
// Specification: Ensures `tools` exists and is a map in frontmatter.
func TestSpec_PublicAPI_EnsureToolsSection(t *testing.T) {
	t.Run("creates tools map when absent from frontmatter", func(t *testing.T) {
		fm := map[string]any{}
		tools := EnsureToolsSection(fm)
		require.NotNil(t, tools,
			"EnsureToolsSection should return a non-nil tools map")
		_, ok := fm["tools"]
		assert.True(t, ok, "EnsureToolsSection should set 'tools' key in frontmatter")
	})

	t.Run("returns existing tools map when already present", func(t *testing.T) {
		existing := map[string]any{"gh": map[string]any{}}
		fm := map[string]any{"tools": existing}
		tools := EnsureToolsSection(fm)
		require.NotNil(t, tools,
			"EnsureToolsSection should return the existing tools map")
		assert.Equal(t, existing, tools,
			"EnsureToolsSection should return the same map that was already present")
	})
}

// TestSpec_VirtualFilesystem_RegisterBuiltinVirtualFile validates the documented
// behavior of RegisterBuiltinVirtualFile and BuiltinVirtualFileExists as described
// in the package README.md.
//
// Specification:
//   - RegisterBuiltinVirtualFile registers embedded virtual file content under an @builtin: path.
//   - BuiltinVirtualFileExists returns whether a built-in virtual file path has been registered.
func TestSpec_VirtualFilesystem_RegisterBuiltinVirtualFile(t *testing.T) {
	t.Run("registered path is detectable via BuiltinVirtualFileExists", func(t *testing.T) {
		path := "@builtin:spec-test-" + t.Name()
		content := []byte("spec test content")

		before := BuiltinVirtualFileExists(path)
		assert.False(t, before,
			"BuiltinVirtualFileExists should return false before registration")

		RegisterBuiltinVirtualFile(path, content)

		after := BuiltinVirtualFileExists(path)
		assert.True(t, after,
			"BuiltinVirtualFileExists should return true after RegisterBuiltinVirtualFile")
	})

	t.Run("unregistered path returns false", func(t *testing.T) {
		path := "@builtin:spec-test-never-registered-" + t.Name()
		assert.False(t, BuiltinVirtualFileExists(path),
			"BuiltinVirtualFileExists should return false for paths that were never registered")
	})

	t.Run("registered content is stable if caller mutates input slice", func(t *testing.T) {
		path := "@builtin:spec-test-content-copy-" + t.Name()
		content := []byte("immutable builtin content")

		RegisterBuiltinVirtualFile(path, content)
		content[0] = 'X'

		readContent, err := ReadFile(path)
		require.NoError(t, err, "ReadFile should resolve registered builtin path")
		assert.Equal(t, []byte("immutable builtin content"), readContent,
			"builtin virtual file content should not change when caller mutates source slice")

		readContent[0] = 'X'
		readContent, err = ReadFile(path)
		require.NoError(t, err, "ReadFile should resolve registered builtin path")
		assert.Equal(t, []byte("immutable builtin content"), readContent,
			"builtin virtual file content should not change when caller mutates read data")
	})
}

// TestSpec_PublicAPI_FindClosestMatches validates the documented behavior of
// FindClosestMatches as described in the package README.md.
//
// Specification: Finds the closest string matches (for typo suggestions).
// Returns up to maxResults matches sorted by distance.
func TestSpec_PublicAPI_FindClosestMatches(t *testing.T) {
	t.Run("returns close matches for a typo", func(t *testing.T) {
		candidates := []string{"on", "runs-on", "steps", "name", "jobs"}
		matches := FindClosestMatches("naem", candidates, 3)
		assert.NotEmpty(t, matches,
			"FindClosestMatches should return at least one match for a close typo")
		assert.LessOrEqual(t, len(matches), 3,
			"FindClosestMatches should return at most maxResults matches")
	})

	t.Run("returns empty when no candidate is close enough", func(t *testing.T) {
		candidates := []string{"alpha", "beta", "gamma"}
		matches := FindClosestMatches("xyzzy", candidates, 5)
		assert.Empty(t, matches,
			"FindClosestMatches should return empty when no candidate is within edit distance")
	})

	t.Run("respects maxResults limit", func(t *testing.T) {
		candidates := []string{"abc", "abd", "abe", "abf", "abg"}
		matches := FindClosestMatches("abx", candidates, 2)
		assert.LessOrEqual(t, len(matches), 2,
			"FindClosestMatches should return no more than maxResults results")
	})
}

// TestSpec_PublicAPI_IsSimpleSecretExpression validates the README contract
// that only a direct secrets reference is considered a simple expression.
func TestSpec_PublicAPI_IsSimpleSecretExpression(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "direct secret reference", input: "${{ secrets.API_TOKEN }}", expected: true},
		{name: "reference with an additional operator", input: "${{ secrets.API_TOKEN || 'fallback' }}", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsSimpleSecretExpression(tt.input),
				"IsSimpleSecretExpression(%q) should enforce the documented direct-reference contract", tt.input)
		})
	}
}

// TestSpec_Constants_ParserValues validates the constant values documented in
// the package README.md.
func TestSpec_Constants_ParserValues(t *testing.T) {
	assert.Equal(t, "@builtin:", BuiltinPathPrefix,
		"BuiltinPathPrefix should equal the documented prefix")
	assert.Equal(t, ".github/aw/imports", ImportCacheDir,
		"ImportCacheDir should equal the documented cache directory")
	assert.ElementsMatch(t, []string{"stdio", "http", "local"}, ValidMCPTypes,
		"ValidMCPTypes should contain exactly the documented MCP transport types")

	urlTypes := map[string]GitHubURLType{
		"blob":       URLTypeBlob,
		"issue":      URLTypeIssue,
		"pull":       URLTypePullRequest,
		"raw":        URLTypeRaw,
		"rawcontent": URLTypeRawContent,
		"run":        URLTypeRun,
		"tree":       URLTypeTree,
		"unknown":    URLTypeUnknown,
	}
	for expected, actual := range urlTypes {
		assert.Equal(t, expected, string(actual),
			"GitHubURLType constant should equal its documented value")
	}
}
