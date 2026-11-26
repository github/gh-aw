//go:build !integration

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentLine returns a ## agent: `name` start marker line for use in test fixtures.
func agentLine(name string) string {
	return fmt.Sprintf("## agent: `%s`", name)
}

func TestExtractInlineSubAgents_NoSeparators(t *testing.T) {
	markdown := "# Hello\n\nThis is a workflow."
	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "no separators should not produce an error")
	assert.Equal(t, markdown, mainMarkdown, "markdown should be unchanged when no separators present")
	assert.Nil(t, agents, "agents should be nil when no separators found")
}

func TestExtractInlineSubAgents_EmptyMarkdown(t *testing.T) {
	mainMarkdown, agents, err := ExtractInlineSubAgents("")

	require.NoError(t, err, "empty markdown should not produce an error")
	assert.Empty(t, mainMarkdown, "empty markdown should return empty main")
	assert.Nil(t, agents, "agents should be nil for empty markdown")
}

func TestExtractInlineSubAgents_SingleAgent(t *testing.T) {
	markdown := strings.Join([]string{
		"# Main workflow",
		"",
		"Handle the issue.",
		"",
		agentLine("planner"),
		"---",
		"engine: copilot",
		"---",
		"You are a planning assistant.",
	}, "\n")

	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "single sub-agent should parse without error")
	assert.Equal(t, "# Main workflow\n\nHandle the issue.", mainMarkdown, "main markdown should exclude agent section")
	require.Len(t, agents, 1, "should extract one sub-agent")
	assert.Equal(t, "planner", agents[0].Name, "agent name should be 'planner'")
	assert.Equal(t, "---\nengine: copilot\n---\nYou are a planning assistant.", agents[0].Content, "agent content should be trimmed")
}

func TestExtractInlineSubAgents_DeadCodeRemover(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "dead-code-remover.md"))
	require.NoError(t, err)
	workflow, err := ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	mainMarkdown, agents, err := ExtractInlineSubAgents(workflow.Markdown)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "discover-candidates", agents[0].Name)
	agent, err := ExtractFrontmatterFromContent(agents[0].Content)
	require.NoError(t, err)
	assert.Equal(t, "copilot/gpt-6-luna", agent.Frontmatter["model"])
	for _, expected := range []string{
		"containsInNonCommentLines", "indexInNonCommentLines", "extractJobSection",
		`{"skip":false,"candidates":[{"function":"Name","file":"pkg/workflow/compiler.go","reason":"no callers"}]}`,
		`{"skip":true,"candidates":[]}`, "./internal/tools/...",
	} {
		assert.Contains(t, agent.Markdown, expected)
	}
	assert.Contains(t, mainMarkdown, "Run the `discover-candidates` sub-agent first.")
	assert.Contains(t, mainMarkdown, "## Phase 3: Select a Batch")
	assert.Contains(t, mainMarkdown, "## Phase 8: Create Pull Request")
	assert.Contains(t, mainMarkdown, "Read at most 60 lines per call.")
	assert.Contains(t, mainMarkdown, "`safeoutputs create_pull_request '<json>'` via bash")
	assert.NotContains(t, agents[0].Content, "## Phase 3")
}

func TestExtractInlineSubAgents_MultipleAgents(t *testing.T) {
	markdown := strings.Join([]string{
		"# Main workflow",
		"",
		"Main prompt.",
		"",
		agentLine("planner"),
		"---",
		"engine: copilot",
		"---",
		"You are a planner.",
		"",
		agentLine("executor"),
		"---",
		"engine: copilot",
		"---",
		"You are an executor.",
	}, "\n")

	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "multiple sub-agents should parse without error")
	assert.Equal(t, "# Main workflow\n\nMain prompt.", mainMarkdown, "main markdown should exclude agent sections")
	require.Len(t, agents, 2, "should extract two sub-agents")

	assert.Equal(t, "planner", agents[0].Name, "first agent name should be 'planner'")
	assert.Contains(t, agents[0].Content, "You are a planner.", "first agent content should contain prompt")

	assert.Equal(t, "executor", agents[1].Name, "second agent name should be 'executor'")
	assert.Contains(t, agents[1].Content, "You are an executor.", "second agent content should contain prompt")
}

func TestExtractInlineSubAgents_AgentAtStartOfFile(t *testing.T) {
	markdown := agentLine("only-agent") + "\n---\nengine: copilot\n---\nAgent prompt."

	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "agent at start of file should parse without error")
	assert.Empty(t, mainMarkdown, "main markdown should be empty when agent is first")
	require.Len(t, agents, 1, "should extract one sub-agent")
	assert.Equal(t, "only-agent", agents[0].Name, "agent name should be 'only-agent'")
}

func TestExtractInlineSubAgents_AgentWithoutFrontmatter(t *testing.T) {
	markdown := "Main workflow.\n\n" + agentLine("simple") + "\nJust a prompt, no frontmatter."

	_, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "agent without frontmatter should parse without error")
	require.Len(t, agents, 1, "should extract one sub-agent")
	assert.Equal(t, "simple", agents[0].Name, "agent name should be 'simple'")
	assert.Equal(t, "Just a prompt, no frontmatter.", agents[0].Content, "agent content should be the prompt")
}

func TestExtractInlineSubAgents_PreservesFrontmatterExactly(t *testing.T) {
	markdown := strings.Join([]string{
		"# Main workflow",
		"",
		"Delegate to the helper.",
		"",
		agentLine("helper"),
		"---",
		"description: Returns a short answer",
		"tools:",
		"  github:",
		"    toolsets: [issues]",
		"model: claude-haiku-4.5",
		"engine: copilot",
		"---",
		`Output "Hello from the sub-agent!".`,
	}, "\n")

	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "sub-agent frontmatter should parse without error")
	assert.Equal(t, "# Main workflow\n\nDelegate to the helper.", mainMarkdown, "main markdown should exclude agent section")
	require.Len(t, agents, 1, "should extract one sub-agent")
	assert.Equal(t, "helper", agents[0].Name, "agent name should be 'helper'")
	assert.Equal(t, strings.Join([]string{
		"---",
		"description: Returns a short answer",
		"tools:",
		"  github:",
		"    toolsets: [issues]",
		"model: claude-haiku-4.5",
		"engine: copilot",
		"---",
		`Output "Hello from the sub-agent!".`,
	}, "\n"), agents[0].Content, "agent content should preserve the frontmatter exactly")
}

func TestExtractInlineSubAgents_SeparatorWithTrailingWhitespace(t *testing.T) {
	// Trailing whitespace after the closing backtick should be tolerated
	markdown := "Main.\n\n" + agentLine("padded") + "   \nAgent content."

	_, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "separator with trailing whitespace should be recognized")
	require.Len(t, agents, 1, "should extract one sub-agent")
	assert.Equal(t, "padded", agents[0].Name, "agent name should be 'padded'")
}

func TestExtractInlineSubAgents_InvalidNameNotRecognized(t *testing.T) {
	tests := []struct {
		name      string
		separator string
	}{
		{
			name:      "name starts with digit",
			separator: "## agent: `1agent`",
		},
		{
			name:      "name contains spaces",
			separator: "## agent: `my agent`",
		},
		{
			name:      "name contains slash",
			separator: "## agent: `my/agent`",
		},
		{
			name:      "missing name",
			separator: "## agent:",
		},
		{
			name:      "name not in backticks",
			separator: "## agent: myagent",
		},
		{
			name:      "name uppercase",
			separator: "## agent: `MyAgent`",
		},
		{
			name:      "wrong heading level (H1)",
			separator: "# agent: `myagent`",
		},
		{
			name:      "wrong heading level (H3)",
			separator: "### agent: `myagent`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			markdown := "Main content.\n\n" + tt.separator + "\nAgent content."
			mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

			require.NoError(t, err, "invalid separator should be treated as regular text")
			assert.Equal(t, markdown, mainMarkdown, "invalid separator should not consume main markdown")
			assert.Nil(t, agents, "invalid separator should not produce agents")
		})
	}
}

func TestExtractInlineSubAgents_DuplicateNameError(t *testing.T) {
	markdown := "Main.\n\n" + agentLine("planner") + "\nContent 1.\n\n" + agentLine("planner") + "\nContent 2."

	_, _, err := ExtractInlineSubAgents(markdown)

	require.Error(t, err, "duplicate agent name should produce an error")
	require.ErrorContains(t, err, "Validation failed for field 'sub-agents'")
	require.ErrorContains(t, err, "Value: planner")
	require.ErrorContains(t, err, "Reason: duplicate name already defined")
	require.ErrorContains(t, err, "Suggestion: Rename one of the duplicate sub-agents or remove the extra `planner` definition.")
	require.ErrorContains(t, err, "planner", "error should include the duplicate name")
}

func TestExtractInlineSubAgents_NameVariants(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
	}{
		{"with hyphens", "my-agent"},
		{"with underscores", "my_agent"},
		{"with digits", "agent1"},
		{"single letter", "a"},
		{"mixed pattern", "planner-v2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			markdown := "Main.\n\n" + agentLine(tt.agentName) + "\nContent."
			_, agents, err := ExtractInlineSubAgents(markdown)

			require.NoError(t, err, "valid agent name %q should parse without error", tt.agentName)
			require.Len(t, agents, 1, "should extract one sub-agent")
			assert.Equal(t, tt.agentName, agents[0].Name, "agent name should match")
		})
	}
}

func TestExtractInlineSubAgents_ContentTrimmed(t *testing.T) {
	// Content after the separator should have leading/trailing whitespace trimmed
	markdown := "Main.\n\n" + agentLine("trim-test") + "\n\n\n  Agent content here.  \n\n"

	_, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "content trimming should not produce an error")
	require.Len(t, agents, 1, "should extract one sub-agent")
	assert.Equal(t, "Agent content here.", agents[0].Content, "agent content should be trimmed")
}

func TestExtractInlineSubAgents_AgentEndsAtNextH2(t *testing.T) {
	// An agent block must end at the next H2 heading (any ##), not just ## agent:.
	markdown := strings.Join([]string{
		"# Main workflow",
		"",
		"Main prompt.",
		"",
		agentLine("planner"),
		"---",
		"engine: copilot",
		"---",
		"You are a planner.",
		"",
		"## Summary",
		"This content is outside the agent block.",
	}, "\n")

	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "H2 ending should parse without error")
	assert.Equal(t, "# Main workflow\n\nMain prompt.", mainMarkdown, "main markdown should exclude agent section")
	require.Len(t, agents, 1, "should extract one agent")
	assert.Equal(t, "planner", agents[0].Name)
	assert.Contains(t, agents[0].Content, "You are a planner.", "agent content should contain prompt")
	assert.NotContains(t, agents[0].Content, "Summary", "content after H2 must not appear in agent")
	assert.NotContains(t, agents[0].Content, "outside the agent block", "content after H2 must not appear in agent")
}

func TestExtractInlineSubAgents_PreservesImplicitBoundaryBeforeSkill(t *testing.T) {
	markdown := strings.Join([]string{
		"Main.",
		"",
		agentLine("planner"),
		"Planner prompt.",
		"",
		skillLine("reporting"),
		"Reporting prompt.",
	}, "\n")

	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "agent followed by skill should parse without error")
	require.Len(t, agents, 1)
	assert.Equal(t, "planner", agents[0].Name)
	assert.Equal(t, "Planner prompt.", agents[0].Content)
	assert.Equal(t, "Main.\n\n"+skillLine("reporting")+"\nReporting prompt.", mainMarkdown, "implicit skill boundary should be preserved for skill extraction")
}

func TestExtractInlineSubAgents_AgentEndsAtNextAgentH2(t *testing.T) {
	// A new ## agent: `name` marker (which is itself an H2) also ends the previous agent.
	markdown := strings.Join([]string{
		"Main.",
		"",
		agentLine("planner"),
		"Planner prompt.",
		"",
		agentLine("executor"),
		"Executor prompt.",
	}, "\n")

	_, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "multiple agents should parse without error")
	require.Len(t, agents, 2, "should extract two agents")
	assert.Equal(t, "planner", agents[0].Name)
	assert.Equal(t, "Planner prompt.", agents[0].Content, "planner content must stop at next agent marker")
	assert.Equal(t, "executor", agents[1].Name)
	assert.Equal(t, "Executor prompt.", agents[1].Content)
}

func TestExtractInlineSubAgents_MainMarkdownTrailingNewlinesStripped(t *testing.T) {
	markdown := "Line 1.\nLine 2.\n\n\n" + agentLine("a") + "\nContent."

	mainMarkdown, _, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "should parse without error")
	assert.Equal(t, "Line 1.\nLine 2.", mainMarkdown, "trailing newlines should be stripped from main markdown")
}

// agentEndLine returns a "## end agent: `name`" end marker line for tests.
func agentEndLine(name string) string {
	return fmt.Sprintf("## end agent: `%s`", name)
}

func TestExtractInlineSubAgents_ExplicitEndMarker_ResumesMainMarkdown(t *testing.T) {
	markdown := strings.Join([]string{
		"Before the import.",
		"",
		agentLine("planner"),
		"---",
		"engine: copilot",
		"---",
		"You are a planner.",
		agentEndLine("planner"),
		"",
		"After the import.",
	}, "\n")

	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "explicit end marker should parse without error")
	require.Len(t, agents, 1, "should extract one sub-agent")
	assert.Equal(t, "planner", agents[0].Name)
	assert.Equal(t, "---\nengine: copilot\n---\nYou are a planner.", agents[0].Content)
	assert.Equal(t, "Before the import.\n\nAfter the import.", mainMarkdown, "content after the end marker should resume as main markdown")
}

func TestExtractInlineSubAgents_ExplicitEndMarker_CanContainH2Headings(t *testing.T) {
	markdown := strings.Join([]string{
		"Main.",
		"",
		agentLine("guide"),
		"## Section A",
		"Content A.",
		"## Section B",
		"Content B.",
		agentEndLine("guide"),
		"",
		"Tail.",
	}, "\n")

	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "explicit end marker with nested H2 should parse without error")
	require.Len(t, agents, 1)
	assert.Equal(t, "guide", agents[0].Name)
	assert.Equal(t, "## Section A\nContent A.\n## Section B\nContent B.", agents[0].Content)
	assert.Equal(t, "Main.\n\nTail.", mainMarkdown)
}

func TestExtractInlineSubAgents_MultipleAgentsWithMixedEndMarkers(t *testing.T) {
	markdown := strings.Join([]string{
		"Main.",
		"",
		agentLine("bounded"),
		"Bounded content.",
		agentEndLine("bounded"),
		"",
		"Interlude.",
		"",
		agentLine("unbounded"),
		"Unbounded content.",
	}, "\n")

	mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

	require.NoError(t, err, "mixed end marker usage should parse without error")
	require.Len(t, agents, 2)
	assert.Equal(t, "bounded", agents[0].Name)
	assert.Equal(t, "Bounded content.", agents[0].Content)
	assert.Equal(t, "unbounded", agents[1].Name)
	assert.Equal(t, "Unbounded content.", agents[1].Content)
	assert.Equal(t, "Main.\n\nInterlude.", mainMarkdown, "gap after the explicitly-closed agent should resume as main markdown")
}

func TestExtractInlineSubAgents_OrphanEndMarker_Error(t *testing.T) {
	markdown := strings.Join([]string{
		"Main.",
		"",
		agentLine("planner"),
		"Content.",
		agentEndLine("plannerr"), // typo: does not match "planner"
	}, "\n")

	_, _, err := ExtractInlineSubAgents(markdown)

	require.Error(t, err, "orphan end marker should produce an error")
	require.ErrorContains(t, err, "plannerr", "error should mention the orphan end marker's name")
	require.ErrorContains(t, err, "line 5", "error should mention where the orphan end marker was found")
}

func TestExtractInlineSubAgents_StandaloneEndMarker_Error(t *testing.T) {
	markdown := "Intro.\n\n" + agentEndLine("planner")

	_, _, err := ExtractInlineSubAgents(markdown)

	require.Error(t, err, "standalone end marker should produce an error")
	require.ErrorContains(t, err, "planner")
	require.ErrorContains(t, err, "line 3")
}

func TestExtractInlineSubAgents_EndMarkerForWrongAgentNotConsumed(t *testing.T) {
	markdown := strings.Join([]string{
		"Main.",
		"",
		agentLine("first"),
		"First content.",
		agentEndLine("second"),
	}, "\n")

	_, _, err := ExtractInlineSubAgents(markdown)

	require.Error(t, err, "mismatched end marker name should be an orphan error")
	require.ErrorContains(t, err, "second")
}

func TestExtractInlineSubAgents_EndMarkerNameVariantsRecognized(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
	}{
		{"with hyphens", "my-agent"},
		{"with underscores", "my_agent"},
		{"with digits", "agent1"},
		{"single letter", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			markdown := "Main.\n\n" + agentLine(tt.agentName) + "\nContent.\n" + agentEndLine(tt.agentName) + "\n\nTail."
			mainMarkdown, agents, err := ExtractInlineSubAgents(markdown)

			require.NoError(t, err, "valid end marker for %q should parse without error", tt.agentName)
			require.Len(t, agents, 1)
			assert.Equal(t, tt.agentName, agents[0].Name)
			assert.Equal(t, "Content.", agents[0].Content)
			assert.Equal(t, "Main.\n\nTail.", mainMarkdown)
		})
	}
}

func TestExtractInlineSubAgents_EndMarkerInvalidVariantsNotRecognizedAsEnd(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"missing name", "## end agent:"},
		{"name not in backticks", "## end agent: planner"},
		{"wrong heading level", "### end agent: `planner`"},
		{"missing end keyword", "## Notes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			markdown := "Main.\n\n" + agentLine("planner") + "\nContent.\n" + tt.line + "\nTail."
			_, agents, err := ExtractInlineSubAgents(markdown)

			require.NoError(t, err, "malformed end marker should not error")
			require.Len(t, agents, 1)
			assert.Equal(t, "planner", agents[0].Name)
			if strings.HasPrefix(tt.line, "##") && !strings.HasPrefix(tt.line, "###") {
				assert.Equal(t, "Content.", agents[0].Content, "content should stop at the fallback H2 boundary")
			} else {
				assert.Contains(t, agents[0].Content, "Content.", "content should include text since no H2 boundary was found")
			}
		})
	}
}
