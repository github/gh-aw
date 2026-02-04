//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnfenceMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic markdown fence with backticks",
			input:    "```markdown\nThis is the content\n```",
			expected: "This is the content",
		},
		{
			name:     "markdown fence with md language tag",
			input:    "```md\nThis is the content\n```",
			expected: "This is the content",
		},
		{
			name:     "markdown fence with tildes",
			input:    "~~~markdown\nThis is the content\n~~~",
			expected: "This is the content",
		},
		{
			name:     "markdown fence with md and tildes",
			input:    "~~~md\nThis is the content\n~~~",
			expected: "This is the content",
		},
		{
			name:     "markdown fence with no language tag",
			input:    "```\nThis is the content\n```",
			expected: "This is the content",
		},
		{
			name:     "markdown fence with multiline content",
			input:    "```markdown\nLine 1\nLine 2\nLine 3\n```",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "markdown fence with nested code blocks",
			input:    "```markdown\nHere is some code:\n```javascript\nconsole.log(\"hello\");\n```\n```",
			expected: "Here is some code:\n```javascript\nconsole.log(\"hello\");\n```",
		},
		{
			name:     "markdown fence with leading and trailing whitespace",
			input:    "   ```markdown\nContent here\n```   ",
			expected: "Content here",
		},
		{
			name:     "markdown fence case insensitive",
			input:    "```MARKDOWN\nContent\n```",
			expected: "Content",
		},
		{
			name:     "markdown fence with MD uppercase",
			input:    "```MD\nContent\n```",
			expected: "Content",
		},
		{
			name:     "not a markdown fence - different language",
			input:    "```javascript\nconsole.log(\"test\");\n```",
			expected: "```javascript\nconsole.log(\"test\");\n```",
		},
		{
			name:     "not fenced - no closing fence",
			input:    "```markdown\nThis has no closing fence",
			expected: "```markdown\nThis has no closing fence",
		},
		{
			name:     "not fenced - mismatched fence types",
			input:    "```markdown\nContent\n~~~",
			expected: "```markdown\nContent\n~~~",
		},
		{
			name:     "not fenced - content before opening fence",
			input:    "Some text before\n```markdown\nContent\n```",
			expected: "Some text before\n```markdown\nContent\n```",
		},
		{
			name:     "not fenced - content after closing fence",
			input:    "```markdown\nContent\n```\nSome text after",
			expected: "```markdown\nContent\n```\nSome text after",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   \n\t\t\t\n\t\t\t",
			expected: "   \n\t\t\t\n\t\t\t",
		},
		{
			name:     "single line",
			input:    "```markdown",
			expected: "```markdown",
		},
		{
			name:     "markdown fence with empty content",
			input:    "```markdown\n```",
			expected: "",
		},
		{
			name:     "markdown fence with only whitespace content",
			input:    "```markdown\n   \n```",
			expected: "",
		},
		{
			name:     "markdown fence with complex nested structures",
			input:    "```markdown\n# Heading\n\nSome text with **bold** and *italic*.\n\n```python\ndef hello():\n    print(\"world\")\n```\n\nMore text here.\n```",
			expected: "# Heading\n\nSome text with **bold** and *italic*.\n\n```python\ndef hello():\n    print(\"world\")\n```\n\nMore text here.",
		},
		{
			name:     "markdown fence with special characters",
			input:    "```markdown\nContent with ${{ github.actor }} and @mentions\n```",
			expected: "Content with ${{ github.actor }} and @mentions",
		},
		{
			name:     "longer backtick fence",
			input:    "````markdown\nContent\n````",
			expected: "Content",
		},
		{
			name:     "longer tilde fence",
			input:    "~~~~markdown\nContent\n~~~~",
			expected: "Content",
		},
		{
			name:     "markdown fence with extra spaces in language tag",
			input:    "```  markdown  \nContent\n```",
			expected: "Content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UnfenceMarkdown(tt.input)
			assert.Equal(t, tt.expected, result, "Unfenced content should match expected")
		})
	}
}

func TestUnfenceMarkdownPreservesNonWrappedContent(t *testing.T) {
	// Test that normal markdown content is not modified
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "normal markdown with headers",
			input: "# Title\n\nSome content here.\n\n## Subtitle\n\nMore content.",
		},
		{
			name:  "markdown with multiple code blocks",
			input: "Some text\n\n```javascript\ncode1();\n```\n\nMore text\n\n```python\ncode2()\n```",
		},
		{
			name:  "markdown with inline code",
			input: "Use `code` for inline code snippets.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UnfenceMarkdown(tt.input)
			assert.Equal(t, tt.input, result, "Non-wrapped content should remain unchanged")
		})
	}
}

func TestUnfenceMarkdownFenceLengthMatching(t *testing.T) {
	// Test that fence lengths must match (closing must be >= opening)
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "4 backticks opening, 4 backticks closing",
			input:    "````markdown\nContent\n````",
			expected: "Content",
		},
		{
			name:     "4 backticks opening, 5 backticks closing",
			input:    "````markdown\nContent\n`````",
			expected: "Content",
		},
		{
			name:     "5 backticks opening, 5 backticks closing",
			input:    "`````markdown\nContent\n`````",
			expected: "Content",
		},
		{
			name:     "3 backticks opening, 4 backticks closing",
			input:    "```markdown\nContent\n````",
			expected: "Content",
		},
		{
			name:     "4 backticks opening, 3 backticks closing - should not unfence",
			input:    "````markdown\nContent\n```",
			expected: "````markdown\nContent\n```",
		},
		{
			name:     "10 backticks opening, 10 backticks closing",
			input:    "``````````markdown\nContent\n``````````",
			expected: "Content",
		},
		{
			name:     "4 tildes opening, 4 tildes closing",
			input:    "~~~~markdown\nContent\n~~~~",
			expected: "Content",
		},
		{
			name:     "5 tildes opening, 6 tildes closing",
			input:    "~~~~~markdown\nContent\n~~~~~~",
			expected: "Content",
		},
		{
			name:     "4 tildes opening, 3 tildes closing - should not unfence",
			input:    "~~~~markdown\nContent\n~~~",
			expected: "~~~~markdown\nContent\n~~~",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UnfenceMarkdown(tt.input)
			assert.Equal(t, tt.expected, result, "Fence length matching should work correctly")
		})
	}
}

func TestUnfenceMarkdownRealWorldExamples(t *testing.T) {
	// Test real-world examples that might come from agents
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "agent response with issue update",
			input:    "```markdown\n# Issue Analysis\n\nI've reviewed the code and found the following:\n\n- Bug in line 42\n- Missing validation\n```",
			expected: "# Issue Analysis\n\nI've reviewed the code and found the following:\n\n- Bug in line 42\n- Missing validation",
		},
		{
			name:     "agent response with code examples",
			input:    "```markdown\nHere's the fix:\n\n```go\nfunc Fix() {\n    // Fixed code\n}\n```\n\nThis should resolve the issue.\n```",
			expected: "Here's the fix:\n\n```go\nfunc Fix() {\n    // Fixed code\n}\n```\n\nThis should resolve the issue.",
		},
		{
			name:     "agent response with multiple sections",
			input:    "```md\n## Summary\n\nCompleted the task.\n\n## Changes\n\n- Updated file A\n- Fixed bug in B\n\n## Testing\n\nAll tests pass.\n```",
			expected: "## Summary\n\nCompleted the task.\n\n## Changes\n\n- Updated file A\n- Fixed bug in B\n\n## Testing\n\nAll tests pass.",
		},
		{
			name:     "plain markdown without fence - no change",
			input:    "## Summary\n\nTask completed successfully.",
			expected: "## Summary\n\nTask completed successfully.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UnfenceMarkdown(tt.input)
			assert.Equal(t, tt.expected, result, "Real-world examples should unfence correctly")
		})
	}
}
