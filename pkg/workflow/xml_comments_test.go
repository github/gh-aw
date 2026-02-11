//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestRemoveXMLComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No XML comments",
			input:    "This is regular markdown content",
			expected: "This is regular markdown content",
		},
		{
			name:     "Single line XML comment",
			input:    "Before <!-- this is a comment --> after",
			expected: "Before  after",
		},
		{
			name:     "XML comment at start of line",
			input:    "<!-- comment at start --> content",
			expected: " content",
		},
		{
			name:     "XML comment at end of line",
			input:    "content <!-- comment at end -->",
			expected: "content ",
		},
		{
			name:     "Entire line is XML comment",
			input:    "<!-- entire line comment -->",
			expected: "",
		},
		{
			name:     "Multiple XML comments on same line",
			input:    "<!-- first --> middle <!-- second --> end",
			expected: " middle  end",
		},
		{
			name: "Multiline XML comment",
			input: `Before comment
<!-- this is a
multiline comment
that spans multiple lines -->
After comment`,
			expected: `Before comment

After comment`,
		},
		{
			name: "Multiple separate XML comments",
			input: `First line
<!-- comment 1 -->
Middle line
<!-- comment 2 -->
Last line`,
			expected: `First line

Middle line

Last line`,
		},
		{
			name:     "XML comment with special characters",
			input:    "Text <!-- comment with & < > special chars --> more text",
			expected: "Text  more text",
		},
		{
			name:     "Nested-like XML comment (not actually nested)",
			input:    "<!-- outer <!-- inner --> -->",
			expected: " -->",
		},
		{
			name: "XML comment in code block should be preserved",
			input: `Regular text
` + "```" + `
<!-- this comment is in code -->
` + "```" + `
<!-- this comment should be removed -->
More text`,
			expected: `Regular text
` + "```" + `
<!-- this comment is in code -->
` + "```" + `

More text`,
		},
		{
			name: "XML comment in code block with 4 backticks should be preserved",
			input: `Regular text
` + "````" + `python
<!-- this comment is in code -->
` + "````" + `
<!-- this comment should be removed -->
More text`,
			expected: `Regular text
` + "````" + `python
<!-- this comment is in code -->
` + "````" + `

More text`,
		},
		{
			name: "XML comment in code block with tildes should be preserved",
			input: `Regular text
~~~bash
<!-- this comment is in code -->
~~~
<!-- this comment should be removed -->
More text`,
			expected: `Regular text
~~~bash
<!-- this comment is in code -->
~~~

More text`,
		},
		{
			name: "XML comment in code block with 5 tildes should be preserved",
			input: `Regular text
~~~~~
<!-- this comment is in code -->
~~~~~
<!-- this comment should be removed -->
More text`,
			expected: `Regular text
~~~~~
<!-- this comment is in code -->
~~~~~

More text`,
		},
		{
			name:     "Empty XML comment",
			input:    "Before <!---->  after",
			expected: "Before   after",
		},
		{
			name:     "XML comment with only whitespace",
			input:    "Before <!--   --> after",
			expected: "Before  after",
		},
		{
			name: "Mixed code block markers should not interfere",
			input: `Regular text
` + "````python" + `
some code
` + "~~~" + `
this is still in the same python block, not a new tilde block
` + "````" + `
<!-- this comment should be removed because we're outside code blocks -->
More text`,
			expected: `Regular text
` + "````python" + `
some code
` + "~~~" + `
this is still in the same python block, not a new tilde block
` + "````" + `

More text`,
		},
		{
			name: "Different marker types should not close each other",
			input: `Text before
` + "~~~bash" + `
code in tilde block
` + "```" + `
this is still in the tilde block, backticks don't close it
` + "~~~" + `
<!-- this comment should be removed -->
Final text`,
			expected: `Text before
` + "~~~bash" + `
code in tilde block
` + "```" + `
this is still in the tilde block, backticks don't close it
` + "~~~" + `

Final text`,
		},
		{
			name: "Nested same-type markers with proper count matching",
			input: `Content
` + "```" + `
code block
` + "```" + `
<!-- this comment should be removed -->
End`,
			expected: `Content
` + "```" + `
code block
` + "```" + `

End`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeXMLComments(tt.input)
			if result != tt.expected {
				t.Errorf("removeXMLComments() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGeneratePromptRemovesXMLComments(t *testing.T) {
	compiler := NewCompiler()

	// Note: With the hybrid runtime-import approach, workflows without imports use runtime-import
	// which means generatePrompt emits a runtime-import macro, not inline content
	// XML comments are removed at runtime by runtime_import.cjs
	data := &WorkflowData{
		MarkdownContent: `# Workflow Title

This is some content.
<!-- This comment should be removed from the prompt -->
More content here.

<!-- Another comment
that spans multiple lines
should also be removed -->

Final content.`,
		ImportedFiles: []string{}, // No imports, so will use runtime-import
		ImportInputs:  nil,
	}

	var yaml strings.Builder
	compiler.generatePrompt(&yaml, data)

	output := yaml.String()

	// With runtime-import (no imports), the output should contain the runtime-import macro
	// XML comments will be removed at runtime by runtime_import.cjs
	if !strings.Contains(output, "{{#runtime-import") {
		t.Error("Expected runtime-import macro in prompt generation output for workflow without imports")
	}
}

func TestSplitContentIntoChunks(t *testing.T) {
	// Test short content - should result in single chunk
	shortContent := "# Short content\n\nThis is a brief workflow description."
	chunks := splitContentIntoChunks(shortContent)
	if len(chunks) != 1 {
		t.Errorf("Short content should result in 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != shortContent {
		t.Error("Short content should be unchanged in single chunk")
	}

	// Test content that exceeds the limit - should result in multiple chunks
	longLine := "This is a very long line of content that will be repeated many times to exceed the character limit."
	longContent := strings.Repeat(longLine+"\n", 400)
	chunks = splitContentIntoChunks(longContent)

	if len(chunks) <= 1 {
		t.Errorf("Long content should result in multiple chunks, got %d", len(chunks))
	}

	// Verify that each chunk stays within the size limit
	const maxChunkSize = 20900
	for i, chunk := range chunks {
		lines := strings.Split(chunk, "\n")
		estimatedSize := 0
		for _, line := range lines {
			estimatedSize += 10 + len(line) + 1 // 10 spaces indentation + line + newline
		}
		if estimatedSize > maxChunkSize {
			t.Errorf("Chunk %d exceeds size limit: %d > %d", i, estimatedSize, maxChunkSize)
		}
	}

	// Verify that joining chunks recreates original content (minus potential trailing newline)
	rejoined := strings.Join(chunks, "\n")
	if strings.TrimSuffix(rejoined, "\n") != strings.TrimSuffix(longContent, "\n") {
		t.Error("Joined chunks should recreate original content")
	}
}

func TestCompileWorkflowWithChunking(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "chunking-test")

	compiler := NewCompiler()

	// Test that normal-sized content compiles successfully with single step
	normalContent := `---
on:
  issues:
    types: [opened]
permissions:
  issues: write
tools:
  github:
    toolsets: [issues]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Normal Workflow

This is a normal-sized workflow that should compile successfully.`

	normalFile := filepath.Join(tmpDir, "normal-workflow.md")
	if err := os.WriteFile(normalFile, []byte(normalContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := compiler.CompileWorkflow(normalFile); err != nil {
		t.Errorf("Normal workflow should compile successfully, got error: %v", err)
	}

	// Test that oversized content now compiles successfully with chunking
	// Note: Add imports to trigger inlining (runtime-import is only used for workflows without imports)
	longContent := "---\n" +
		"on:\n" +
		"  issues:\n" +
		"    types: [opened]\n" +
		"permissions:\n" +
		"  issues: write\n" +
		"features:\n" +
		"  dangerous-permissions-write: true\n" +
		"strict: false\n" +
		"tools:\n" +
		"  github:\n" +
		"    toolsets: [issues]\n" +
		"engine: claude\n" +
		"imports:\n" +
		"  - shared/dummy.md\n" +
		"---\n\n" +
		"# Very Long Workflow\n\n" +
		strings.Repeat("This is a very long line that will be repeated many times to test the chunking functionality in GitHub Actions prompt generation.\n", 400)

	// Create shared directory and dummy import file
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.Mkdir(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}
	dummyFile := filepath.Join(sharedDir, "dummy.md")
	if err := os.WriteFile(dummyFile, []byte("# Dummy\n\nDummy content.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	longFile := filepath.Join(tmpDir, "long-workflow.md")
	if err := os.WriteFile(longFile, []byte(longContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := compiler.CompileWorkflow(longFile); err != nil {
		t.Errorf("Long workflow should now compile successfully with chunking, got error: %v", err)
	}

	// Read the lock file for the normal workflow as a baseline.
	normalLockFile := filepath.Join(tmpDir, "normal-workflow.lock.yml")
	normalLockContent, err := os.ReadFile(normalLockFile)
	if err != nil {
		t.Fatalf("Failed to read generated normal lock file: %v", err)
	}

	// Verify that the generated lock file contains multiple prompt steps
	lockFile := filepath.Join(tmpDir, "long-workflow.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockString := string(lockContent)
	normalLockString := string(normalLockContent)

	// Unified prompt generation should always include the consolidated prompt creation step.
	if !strings.Contains(lockString, "Create prompt with built-in context") {
		t.Error("Expected 'Create prompt with built-in context' step in generated workflow")
	}

	// Normal workflow (no imports) should use runtime-import
	if !strings.Contains(normalLockString, "{{#runtime-import") {
		t.Error("Normal workflow without imports should use runtime-import")
	}

	// Long workflow (with imports) should be inlined and chunked
	// Chunking is implemented by emitting more heredoc blocks for large content,
	// not by generating old "Append prompt (part N)" steps.
	delimiter := GenerateHeredocDelimiter("PROMPT")
	normalHeredocCount := strings.Count(normalLockString, "cat << '"+delimiter+"'")
	longHeredocCount := strings.Count(lockString, "cat << '"+delimiter+"'")
	if longHeredocCount <= normalHeredocCount {
		t.Errorf("Expected long workflow with imports to have more heredoc blocks than normal (normal=%d, long=%d)", normalHeredocCount, longHeredocCount)
	}
}

// TestRemoveXMLCommentsCodeBlocksInComments tests the specific bug fix:
// code blocks within XML comments should be removed, not preserved
func TestRemoveXMLCommentsCodeBlocksInComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Code block with backticks inside XML comment should be removed",
			input: `Before comment
<!--
Documentation about usage:

` + "```yaml" + `
---
key: value
---
` + "```" + `

More documentation
-->
After comment`,
			expected: `Before comment

After comment`,
		},
		{
			name: "Code block with tildes inside XML comment should be removed",
			input: `Content
<!--
Example:
~~~bash
command --flag
~~~
-->
More content`,
			expected: `Content

More content`,
		},
		{
			name: "Multiple code blocks inside XML comment should be removed",
			input: `Text
<!--
First example:
` + "```python" + `
print("hello")
` + "```" + `

Second example:
` + "```javascript" + `
console.log("world");
` + "```" + `
-->
End`,
			expected: `Text

End`,
		},
		{
			name: "Code block starting before XML comment and continuing after should be split correctly",
			input: `Start
` + "```" + `
code before comment
<!--
commented code
-->
code after comment
` + "```" + `
End`,
			expected: `Start
` + "```" + `
code before comment
<!--
commented code
-->
code after comment
` + "```" + `
End`,
		},
		{
			name: "XML comment inside code block with nested code block markers should preserve",
			input: `Text
` + "```markdown" + `
This is markdown content

` + "```yaml" + `
nested: block
` + "```" + `

<!-- This comment is in the markdown code block -->
` + "```" + `
End`,
			expected: `Text
` + "```markdown" + `
This is markdown content

` + "```yaml" + `
nested: block
` + "```" + `

<!-- This comment is in the markdown code block -->
` + "```" + `
End`,
		},
		{
			name: "Real-world case: shared workflow import documentation",
			input: `<!--
This shared configuration sets up a custom engine.

**Usage:**
Include this file in your workflow using frontmatter imports:

` + "```yaml" + `
---
imports:
  - shared/example.md
---
` + "```" + `

**Requirements:**
- The workflow requires certain dependencies
-->

Actual workflow content here.`,
			expected: `

Actual workflow content here.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeXMLComments(tt.input)
			if result != tt.expected {
				t.Errorf("removeXMLComments() mismatch\nGot:\n%q\n\nWant:\n%q", result, tt.expected)
			}
		})
	}
}

// TestRemoveXMLCommentsEdgeCases tests various edge cases and boundary conditions
func TestRemoveXMLCommentsEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Only whitespace",
			input:    "   \n\t\n   ",
			expected: "   \n\t\n   ",
		},
		{
			name:     "Only XML comment opening without closing",
			input:    "<!-- unclosed comment",
			expected: "",
		},
		{
			name:     "Only XML comment closing without opening",
			input:    "closing without opening -->",
			expected: "closing without opening -->",
		},
		{
			name:     "Comment markers in text are processed as comments",
			input:    `Text with "<!--" in quotes and "-->" also in quotes`,
			expected: `Text with "" also in quotes`,
		},
		{
			name: "XML comment spanning entire content",
			input: `<!-- Everything is commented
Line 2
Line 3
Last line -->`,
			expected: "",
		},
		{
			name: "Multiple consecutive XML comments",
			input: `Text
<!-- Comment 1 -->
<!-- Comment 2 -->
<!-- Comment 3 -->
More text`,
			expected: `Text



More text`,
		},
		{
			name: "XML comment with code block markers but incomplete blocks",
			input: `Before
<!--
` + "```" + `
This code block is never closed
and the comment ends
-->
After`,
			expected: `Before

After`,
		},
		{
			name: "Very long XML comment",
			input: `Start
<!--
` + strings.Repeat("Very long line of commented content.\n", 100) + `
-->
End`,
			expected: `Start

End`,
		},
		{
			name:     "XML comment with unusual but valid markers",
			input:    `Text <!-- comment with  extra  spaces   --> more`,
			expected: `Text  more`,
		},
		{
			name: "Code block with language specifier and XML comment inside",
			input: `Normal
` + "```python" + `
def function():
    <!-- This is in code -->
    pass
` + "```" + `
End`,
			expected: `Normal
` + "```python" + `
def function():
    <!-- This is in code -->
    pass
` + "```" + `
End`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeXMLComments(tt.input)
			if result != tt.expected {
				t.Errorf("removeXMLComments() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractCodeBlockMarker tests the code block marker extraction function
func TestExtractCodeBlockMarker(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedMarker   string
		expectedLanguage string
	}{
		{
			name:             "Three backticks",
			input:            "```",
			expectedMarker:   "```",
			expectedLanguage: "",
		},
		{
			name:             "Three backticks with language",
			input:            "```python",
			expectedMarker:   "```",
			expectedLanguage: "python",
		},
		{
			name:             "Four backticks",
			input:            "````",
			expectedMarker:   "````",
			expectedLanguage: "",
		},
		{
			name:             "Four backticks with language",
			input:            "````javascript",
			expectedMarker:   "````",
			expectedLanguage: "javascript",
		},
		{
			name:             "Three tildes",
			input:            "~~~",
			expectedMarker:   "~~~",
			expectedLanguage: "",
		},
		{
			name:             "Three tildes with language",
			input:            "~~~bash",
			expectedMarker:   "~~~",
			expectedLanguage: "bash",
		},
		{
			name:             "Five tildes",
			input:            "~~~~~",
			expectedMarker:   "~~~~~",
			expectedLanguage: "",
		},
		{
			name:             "Two backticks (invalid)",
			input:            "``",
			expectedMarker:   "",
			expectedLanguage: "",
		},
		{
			name:             "No marker",
			input:            "regular text",
			expectedMarker:   "",
			expectedLanguage: "",
		},
		{
			name:             "Empty string",
			input:            "",
			expectedMarker:   "",
			expectedLanguage: "",
		},
		{
			name:             "Backticks with whitespace before language",
			input:            "```  python  ",
			expectedMarker:   "```",
			expectedLanguage: "python",
		},
		{
			name:             "Many backticks",
			input:            "``````````",
			expectedMarker:   "``````````",
			expectedLanguage: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			marker, lang := extractCodeBlockMarker(tt.input)
			if marker != tt.expectedMarker {
				t.Errorf("extractCodeBlockMarker(%q) marker = %q, want %q", tt.input, marker, tt.expectedMarker)
			}
			if lang != tt.expectedLanguage {
				t.Errorf("extractCodeBlockMarker(%q) language = %q, want %q", tt.input, lang, tt.expectedLanguage)
			}
		})
	}
}

// TestIsValidCodeBlockMarker tests the validation of code block markers
func TestIsValidCodeBlockMarker(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"Three backticks", "```", true},
		{"Four backticks", "````", true},
		{"Three tildes", "~~~", true},
		{"Five tildes", "~~~~~", true},
		{"Backticks with language", "```python", true},
		{"Tildes with language", "~~~bash", true},
		{"Two backticks", "``", false},
		{"One backtick", "`", false},
		{"Two tildes", "~~", false},
		{"Regular text", "text", false},
		{"Empty string", "", false},
		{"Mixed markers", "``~", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidCodeBlockMarker(tt.input)
			if result != tt.expected {
				t.Errorf("isValidCodeBlockMarker(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestIsMatchingCodeBlockMarker tests the matching of closing markers
func TestIsMatchingCodeBlockMarker(t *testing.T) {
	tests := []struct {
		name        string
		closing     string
		opening     string
		shouldMatch bool
	}{
		{"Same backticks", "```", "```", true},
		{"More closing backticks", "````", "```", true},
		{"Fewer closing backticks", "```", "````", false},
		{"Same tildes", "~~~", "~~~", true},
		{"More closing tildes", "~~~~~", "~~~", true},
		{"Fewer closing tildes", "~~~", "~~~~~", false},
		{"Backticks vs tildes", "```", "~~~", false},
		{"Tildes vs backticks", "~~~", "```", false},
		{"Empty opening", "```", "", false},
		{"Empty closing", "", "```", false},
		{"Both empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMatchingCodeBlockMarker(tt.closing, tt.opening)
			if result != tt.shouldMatch {
				t.Errorf("isMatchingCodeBlockMarker(%q, %q) = %v, want %v",
					tt.closing, tt.opening, result, tt.shouldMatch)
			}
		})
	}
}

// TestRemoveXMLCommentsFromLine tests the single-line processing function
func TestRemoveXMLCommentsFromLine(t *testing.T) {
	tests := []struct {
		name           string
		line           string
		inComment      bool
		expectedResult string
		expectedWasIn  bool
		expectedIsIn   bool
	}{
		{
			name:           "No comment, not in comment",
			line:           "Regular text",
			inComment:      false,
			expectedResult: "Regular text",
			expectedWasIn:  false,
			expectedIsIn:   false,
		},
		{
			name:           "Complete comment on line",
			line:           "Before <!-- comment --> after",
			inComment:      false,
			expectedResult: "Before  after",
			expectedWasIn:  false,
			expectedIsIn:   false,
		},
		{
			name:           "Start multiline comment",
			line:           "Text before <!-- comment starts",
			inComment:      false,
			expectedResult: "Text before ",
			expectedWasIn:  false,
			expectedIsIn:   true,
		},
		{
			name:           "Inside multiline comment",
			line:           "This line is entirely in comment",
			inComment:      true,
			expectedResult: "",
			expectedWasIn:  true,
			expectedIsIn:   true,
		},
		{
			name:           "End multiline comment",
			line:           "comment ends --> after comment",
			inComment:      true,
			expectedResult: " after comment",
			expectedWasIn:  true,
			expectedIsIn:   false,
		},
		{
			name:           "Multiple comments on same line",
			line:           "<!-- c1 --> text <!-- c2 --> more",
			inComment:      false,
			expectedResult: " text  more",
			expectedWasIn:  false,
			expectedIsIn:   false,
		},
		{
			name:           "Empty comment",
			line:           "Text <!---> more",
			inComment:      false,
			expectedResult: "Text  more",
			expectedWasIn:  false,
			expectedIsIn:   false,
		},
		{
			name:           "Comment with no spaces",
			line:           "a<!--b-->c",
			inComment:      false,
			expectedResult: "ac",
			expectedWasIn:  false,
			expectedIsIn:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, wasIn, isIn := removeXMLCommentsFromLine(tt.line, tt.inComment)
			if result != tt.expectedResult {
				t.Errorf("removeXMLCommentsFromLine(%q, %v) result = %q, want %q",
					tt.line, tt.inComment, result, tt.expectedResult)
			}
			if wasIn != tt.expectedWasIn {
				t.Errorf("removeXMLCommentsFromLine(%q, %v) wasInComment = %v, want %v",
					tt.line, tt.inComment, wasIn, tt.expectedWasIn)
			}
			if isIn != tt.expectedIsIn {
				t.Errorf("removeXMLCommentsFromLine(%q, %v) isInComment = %v, want %v",
					tt.line, tt.inComment, isIn, tt.expectedIsIn)
			}
		})
	}
}

// TestRemoveXMLCommentsComplexNesting tests complex nesting scenarios
func TestRemoveXMLCommentsComplexNesting(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Code block, then comment, then code block",
			input:    "```\ncode1\n```\n<!-- comment -->\n```\ncode2\n```",
			expected: "```\ncode1\n```\n\n```\ncode2\n```",
		},
		{
			name:     "Comment, then code block, then comment",
			input:    "<!-- c1 -->\n```\ncode\n```\n<!-- c2 -->",
			expected: "\n```\ncode\n```\n",
		},
		{
			name: "Interleaved comments and code blocks",
			input: `Text
<!-- Start comment
Comment line 1
Comment line 2
End comment -->
` + "```" + `
Code block
` + "```" + `
<!-- Another comment -->
Final text`,
			expected: `Text

` + "```" + `
Code block
` + "```" + `

Final text`,
		},
		{
			name: "Code block with comment-like content (not actual comments)",
			input: `Text
` + "```html" + `
<div>
<!-- This looks like a comment but it's HTML in code -->
</div>
` + "```" + `
End`,
			expected: `Text
` + "```html" + `
<div>
<!-- This looks like a comment but it's HTML in code -->
</div>
` + "```" + `
End`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeXMLComments(tt.input)
			if result != tt.expected {
				t.Errorf("removeXMLComments() mismatch\nGot:\n%q\n\nWant:\n%q", result, tt.expected)
			}
		})
	}
}
