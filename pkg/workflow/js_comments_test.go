//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestRemoveJavaScriptComments tests the removeJavaScriptComments function extensively
func TestRemoveJavaScriptComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic cases
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no comments",
			input:    "const x = 1;",
			expected: "const x = 1;",
		},
		{
			name:     "single line comment only",
			input:    "// comment",
			expected: "",
		},
		{
			name:     "multiple single line comments",
			input:    "// comment 1\n// comment 2\n// comment 3",
			expected: "\n\n",
		},
		{
			name:     "code with trailing comment",
			input:    "const x = 1; // comment",
			expected: "const x = 1; ",
		},
		{
			name:     "code with leading comment",
			input:    "// comment\nconst x = 1;",
			expected: "\nconst x = 1;",
		},

		// Block comments
		{
			name:     "simple block comment",
			input:    "/* comment */",
			expected: "",
		},
		{
			name:     "multiline block comment",
			input:    "/* line 1\n   line 2\n   line 3 */",
			expected: "\n\n", // Block comments preserve line structure
		},
		{
			name:     "code before and after block comment",
			input:    "const x = 1; /* comment */ const y = 2;",
			expected: "const x = 1;  const y = 2;",
		},
		{
			name:     "nested block comment markers",
			input:    "/* outer /* inner */ still in comment */const x = 1;",
			expected: " still in comment */const x = 1;", // Block comments don't nest in JavaScript
		},
		{
			name:     "block comment spanning multiple lines with code after",
			input:    "/* comment\n   line 2 */\nconst x = 1;",
			expected: "\n\nconst x = 1;", // Preserves line structure
		},
		{
			name:     "multiple block comments",
			input:    "/* c1 */ const x = 1; /* c2 */ const y = 2; /* c3 */",
			expected: " const x = 1;  const y = 2; ",
		},

		// JSDoc comments
		{
			name:     "JSDoc single line",
			input:    "/** @param {string} x */",
			expected: "",
		},
		{
			name:     "JSDoc multiline",
			input:    "/**\n * @param {string} x\n * @returns {number}\n */\nfunction test() {}",
			expected: "\n\n\n\nfunction test() {}", // Preserves line structure
		},
		{
			name:     "JSDoc with description",
			input:    "/**\n * Function description\n * @param {string} name - The name\n */",
			expected: "\n\n\n", // Preserves line structure
		},

		// TypeScript directives
		{
			name:     "ts-check directive",
			input:    "// @ts-check\nconst x = 1;",
			expected: "\nconst x = 1;",
		},
		{
			name:     "ts-ignore directive",
			input:    "// @ts-ignore\nconst x: any = 1;",
			expected: "\nconst x: any = 1;",
		},
		{
			name:     "triple slash reference",
			input:    "/// <reference types=\"node\" />\nconst x = 1;",
			expected: "\nconst x = 1;",
		},
		{
			name:     "multiple TypeScript directives",
			input:    "// @ts-check\n/// <reference types=\"@actions/github-script\" />\n\nconst x = 1;",
			expected: "\n\n\nconst x = 1;",
		},

		// Comments in strings
		{
			name:     "single line comment in double quotes",
			input:    "const url = \"https://example.com//path\";",
			expected: "const url = \"https://example.com//path\";",
		},
		{
			name:     "single line comment in single quotes",
			input:    "const msg = 'This // is not a comment';",
			expected: "const msg = 'This // is not a comment';",
		},
		{
			name:     "block comment in double quotes",
			input:    "const msg = \"This /* is not */ a comment\";",
			expected: "const msg = \"This /* is not */ a comment\";",
		},
		{
			name:     "block comment in single quotes",
			input:    "const msg = 'This /* is not */ a comment';",
			expected: "const msg = 'This /* is not */ a comment';",
		},
		{
			name:     "comment markers in template literal",
			input:    "const template = `// this ${x} /* stays */ here`;",
			expected: "const template = `// this ${x} /* stays */ here`;",
		},
		{
			name:     "escaped quotes with comments",
			input:    "const msg = \"Quote: \\\" // not a comment\";",
			expected: "const msg = \"Quote: \\\" // not a comment\";",
		},
		{
			name:     "string with real comment after",
			input:    "const url = \"http://example.com\"; // real comment",
			expected: "const url = \"http://example.com\"; ",
		},

		// Comments in regex
		{
			name:     "regex with slashes",
			input:    "const regex = /\\/\\//;",
			expected: "const regex = /\\/\\//;",
		},
		{
			name:     "regex with comment after",
			input:    "const regex = /test/g; // comment",
			expected: "const regex = /test/g; ",
		},
		{
			name:     "complex regex pattern",
			input:    "const regex = /foo\\/bar\\/baz/gi;",
			expected: "const regex = /foo\\/bar\\/baz/gi;",
		},
		{
			name:     "regex with escaped slashes and comment",
			input:    "const regex = /\\/\\/test\\/\\//; // matches //test//",
			expected: "const regex = /\\/\\/test\\/\\//; ",
		},
		{
			name:     "division operator not regex",
			input:    "const result = x / y; // division",
			expected: "const result = x / y; ",
		},
		{
			name:     "regex after return",
			input:    "return /test/;",
			expected: "return /test/;",
		},
		{
			name:     "regex in assignment",
			input:    "const r = /pattern/;",
			expected: "const r = /pattern/;",
		},
		{
			name:     "regex with character class",
			input:    "const r = /[a-z]/gi;",
			expected: "const r = /[a-z]/gi;",
		},

		// Edge cases with escaped characters
		{
			name:     "escaped backslash in string",
			input:    "const path = \"C:\\\\path\\\\to\\\\file\";",
			expected: "const path = \"C:\\\\path\\\\to\\\\file\";",
		},
		{
			name:     "escaped quote in string",
			input:    "const quote = \"He said \\\"hello\\\"\";",
			expected: "const quote = \"He said \\\"hello\\\"\";",
		},
		{
			name:     "escaped newline in string",
			input:    "const msg = \"Line 1\\nLine 2\";",
			expected: "const msg = \"Line 1\\nLine 2\";",
		},

		// Mixed scenarios
		{
			name:     "code with multiple comment types",
			input:    "// Line comment\n/* Block comment */\nconst x = 1; // trailing\n/** JSDoc */\nconst y = 2;",
			expected: "\n\nconst x = 1; \n\nconst y = 2;",
		},
		{
			name:     "real world example with imports",
			input:    "// Import statements\nconst fs = require('fs');\nconst path = require('path'); // path module\n/* Utility function */\nfunction test() { return true; }",
			expected: "\nconst fs = require('fs');\nconst path = require('path'); \n\nfunction test() { return true; }",
		},
		{
			name:     "function with inline comments",
			input:    "function calc(a, b) {\n  // Add numbers\n  return a + b; // result\n}",
			expected: "function calc(a, b) {\n  \n  return a + b; \n}",
		},
		{
			name:     "object literal with comments",
			input:    "const obj = {\n  // Property\n  key: 'value', // inline\n  /* Another */\n  key2: 'value2'\n};",
			expected: "const obj = {\n  \n  key: 'value', \n  \n  key2: 'value2'\n};",
		},
		{
			name:     "array with comments",
			input:    "const arr = [\n  1, // first\n  2, // second\n  /* skip */ 3\n];",
			expected: "const arr = [\n  1, \n  2, \n   3\n];",
		},

		// Whitespace preservation
		{
			name:     "preserve indentation",
			input:    "  // comment\n  const x = 1;",
			expected: "  \n  const x = 1;",
		},
		{
			name:     "preserve spacing in code",
			input:    "const x = 1;    const y = 2;",
			expected: "const x = 1;    const y = 2;",
		},

		// Unicode and special characters
		{
			name:     "unicode in strings",
			input:    "const emoji = \"😀 // not a comment\";",
			expected: "const emoji = \"😀 // not a comment\";",
		},
		{
			name:     "unicode in comment",
			input:    "// Comment with emoji 😀\nconst x = 1;",
			expected: "\nconst x = 1;",
		},

		// Empty lines and whitespace
		{
			name:     "empty lines preserved",
			input:    "const x = 1;\n\n\nconst y = 2;",
			expected: "const x = 1;\n\n\nconst y = 2;",
		},
		{
			name:     "comment on empty line removed",
			input:    "const x = 1;\n\n// comment\n\nconst y = 2;",
			expected: "const x = 1;\n\n\n\nconst y = 2;",
		},
		{
			name:     "whitespace only lines",
			input:    "const x = 1;\n   \n\t\nconst y = 2;",
			expected: "const x = 1;\n   \n\t\nconst y = 2;",
		},

		// Comment at start/end of file
		{
			name:     "comment at start",
			input:    "// Start comment\nconst x = 1;",
			expected: "\nconst x = 1;",
		},
		{
			name:     "comment at end",
			input:    "const x = 1;\n// End comment",
			expected: "const x = 1;\n",
		},
		{
			name:     "comment at both ends",
			input:    "// Start\nconst x = 1;\n// End",
			expected: "\nconst x = 1;\n",
		},

		// Consecutive comments
		{
			name:     "consecutive line comments",
			input:    "// Comment 1\n// Comment 2\n// Comment 3",
			expected: "\n\n",
		},
		{
			name:     "consecutive block comments",
			input:    "/* C1 *//* C2 *//* C3 */",
			expected: "",
		},
		{
			name:     "alternating comments and code",
			input:    "// C1\nconst a = 1;\n// C2\nconst b = 2;\n// C3\nconst c = 3;",
			expected: "\nconst a = 1;\n\nconst b = 2;\n\nconst c = 3;",
		},

		// Tricky cases
		{
			name:     "division after number",
			input:    "const x = 10 / 2;",
			expected: "const x = 10 / 2;",
		},
		{
			name:     "comment-like in URL",
			input:    "const url = 'http://example.com/path';",
			expected: "const url = 'http://example.com/path';",
		},
		{
			name:     "comment-like in file path",
			input:    "const path = 'C://Windows//System32';",
			expected: "const path = 'C://Windows//System32';",
		},
		{
			name:     "asterisk in string not comment",
			input:    "const str = 'This * is not a comment';",
			expected: "const str = 'This * is not a comment';",
		},
		{
			name:     "slash and asterisk separate",
			input:    "const x = '/' + '*' + 'combined';",
			expected: "const x = '/' + '*' + 'combined';",
		},

		// Block comment edge cases
		{
			name:     "unclosed block comment",
			input:    "const x = 1; /* unclosed comment",
			expected: "const x = 1; ",
		},
		{
			name:     "block comment with asterisks inside",
			input:    "/* comment * with * asterisks */const x = 1;",
			expected: "const x = 1;",
		},
		{
			name:     "block comment end marker in string",
			input:    "const str = \"This */ is not comment end\"; /* comment */",
			expected: "const str = \"This */ is not comment end\"; ",
		},
		{
			name:     "slash star in separate strings",
			input:    "const a = '/'; const b = '*'; // comment",
			expected: "const a = '/'; const b = '*'; ",
		},

		// Real-world patterns from .cjs files
		{
			name:     "GitHub Actions script pattern",
			input:    "// @ts-check\n/// <reference types=\"@actions/github-script\" />\n\nasync function main() {\n  core.info('test');\n}",
			expected: "\n\n\nasync function main() {\n  core.info('test');\n}",
		},
		{
			name:     "JSDoc with function",
			input:    "/**\n * Process data\n * @param {Object} data - The data\n * @returns {boolean}\n */\nfunction process(data) {\n  return true;\n}",
			expected: "\n\n\n\n\nfunction process(data) {\n  return true;\n}", // Preserves line structure
		},
		{
			name:     "module pattern with comments",
			input:    "// Module exports\nmodule.exports = {\n  // Properties\n  prop1: value1, // inline\n  /* Another property */\n  prop2: value2\n};",
			expected: "\nmodule.exports = {\n  \n  prop1: value1, \n  \n  prop2: value2\n};",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeJavaScriptComments(tt.input)
			if result != tt.expected {
				t.Errorf("removeJavaScriptComments() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// TestRemoveJavaScriptCommentsFromLine tests line-level comment removal
func TestRemoveJavaScriptCommentsFromLine(t *testing.T) {
	tests := []struct {
		name           string
		line           string
		inBlockComment bool
		expected       string
		expectBlock    bool
	}{
		{
			name:           "simple line no comment",
			line:           "const x = 1;",
			inBlockComment: false,
			expected:       "const x = 1;",
			expectBlock:    false,
		},
		{
			name:           "line with trailing comment",
			line:           "const x = 1; // comment",
			inBlockComment: false,
			expected:       "const x = 1; ",
			expectBlock:    false,
		},
		{
			name:           "line starting in block comment",
			line:           "still in block */ after block",
			inBlockComment: true,
			expected:       " after block",
			expectBlock:    false,
		},
		{
			name:           "line starting block comment",
			line:           "before /* start block",
			inBlockComment: false,
			expected:       "before ",
			expectBlock:    true,
		},
		{
			name:           "line with complete block comment",
			line:           "before /* block */ after",
			inBlockComment: false,
			expected:       "before  after",
			expectBlock:    false,
		},
		{
			name:           "entire line is comment",
			line:           "// entire line",
			inBlockComment: false,
			expected:       "",
			expectBlock:    false,
		},
		{
			name:           "line in middle of block comment",
			line:           "this is inside block comment",
			inBlockComment: true,
			expected:       "",
			expectBlock:    true,
		},
		{
			name:           "comment in string preserved",
			line:           "const s = \"// not a comment\";",
			inBlockComment: false,
			expected:       "const s = \"// not a comment\";",
			expectBlock:    false,
		},
		{
			name:           "regex with slashes",
			line:           "const r = /test\\/path/;",
			inBlockComment: false,
			expected:       "const r = /test\\/path/;",
			expectBlock:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inBlock := tt.inBlockComment
			result := removeJavaScriptCommentsFromLine(tt.line, &inBlock)
			if result != tt.expected {
				t.Errorf("removeJavaScriptCommentsFromLine() = %q, expected %q", result, tt.expected)
			}
			if inBlock != tt.expectBlock {
				t.Errorf("inBlockComment after = %v, expected %v", inBlock, tt.expectBlock)
			}
		})
	}
}

// TestIsInsideStringLiteral tests string literal detection
func TestIsInsideStringLiteral(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "not in string",
			text:     "const x = ",
			expected: false,
		},
		{
			name:     "inside double quotes",
			text:     "const x = \"hello",
			expected: true,
		},
		{
			name:     "inside single quotes",
			text:     "const x = 'hello",
			expected: true,
		},
		{
			name:     "inside backticks",
			text:     "const x = `hello",
			expected: true,
		},
		{
			name:     "closed double quotes",
			text:     "const x = \"hello\"; y = ",
			expected: false,
		},
		{
			name:     "escaped quote in string",
			text:     "const x = \"hello \\\"world",
			expected: true,
		},
		{
			name:     "escaped backslash before quote",
			text:     "const x = \"hello\\\\\" y = ",
			expected: false,
		},
		{
			name:     "nested different quotes",
			text:     "const x = \"hello 'world",
			expected: true,
		},
		{
			name:     "empty string",
			text:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInsideStringLiteral(tt.text)
			if result != tt.expected {
				t.Errorf("isInsideStringLiteral(%q) = %v, expected %v", tt.text, result, tt.expected)
			}
		})
	}
}

// TestCanStartRegexLiteral tests regex literal detection
func TestCanStartRegexLiteral(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "after equals",
			text:     "const x = ",
			expected: true,
		},
		{
			name:     "after return",
			text:     "return ",
			expected: false, // Needs the keyword at the end of text
		},
		{
			name:     "after opening paren",
			text:     "test(",
			expected: true,
		},
		{
			name:     "after comma",
			text:     "arr = [1, ",
			expected: true,
		},
		{
			name:     "after opening bracket",
			text:     "arr = [",
			expected: true,
		},
		{
			name:     "after colon",
			text:     "obj = { key: ",
			expected: true,
		},
		{
			name:     "after identifier (division)",
			text:     "x ",
			expected: false,
		},
		{
			name:     "after number (division)",
			text:     "10 ",
			expected: false,
		},
		{
			name:     "at start of line",
			text:     "",
			expected: true,
		},
		{
			name:     "after if keyword",
			text:     "if ",
			expected: false, // Needs the keyword at the end of text
		},
		{
			name:     "after while keyword",
			text:     "while ",
			expected: false, // Needs the keyword at the end of text
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canStartRegexLiteral(tt.text)
			if result != tt.expected {
				t.Errorf("canStartRegexLiteral(%q) = %v, expected %v", tt.text, result, tt.expected)
			}
		})
	}
}

// TestRemoveJavaScriptCommentsEdgeCases tests additional edge cases
func TestRemoveJavaScriptCommentsEdgeCases(t *testing.T) {
	t.Run("very long line with comment", func(t *testing.T) {
		longLine := strings.Repeat("x", 10000) + " // comment"
		result := removeJavaScriptComments(longLine)
		expected := strings.Repeat("x", 10000) + " "
		if result != expected {
			t.Errorf("Failed to handle long line")
		}
	})

	t.Run("many consecutive comments", func(t *testing.T) {
		input := strings.Repeat("// comment\n", 100)
		result := removeJavaScriptComments(input)
		// Each comment line leaves an empty line (100 lines, each becoming empty)
		// The function doesn't trim trailing newlines from repeated inputs
		expected := strings.Repeat("\n", 100)
		if len(result) != len(expected) {
			t.Errorf("Failed to handle many consecutive comments: got %d chars, expected %d", len(result), len(expected))
		}
	})

	t.Run("deeply nested strings and comments", func(t *testing.T) {
		input := `const a = "/*"; const b = "*/"; // comment`
		result := removeJavaScriptComments(input)
		expected := `const a = "/*"; const b = "*/"; `
		if result != expected {
			t.Errorf("removeJavaScriptComments() = %q, expected %q", result, expected)
		}
	})

	t.Run("multiple block comments on same line", func(t *testing.T) {
		input := "/* c1 */ a /* c2 */ b /* c3 */"
		result := removeJavaScriptComments(input)
		expected := " a  b "
		if result != expected {
			t.Errorf("removeJavaScriptComments() = %q, expected %q", result, expected)
		}
	})

	t.Run("block comment across many lines", func(t *testing.T) {
		input := "start /* comment\nline 2\nline 3\nline 4\nline 5 */ end"
		result := removeJavaScriptComments(input)
		// Block comments preserve line structure
		expected := "start \n\n\n\n end"
		if result != expected {
			t.Errorf("removeJavaScriptComments() = %q, expected %q", result, expected)
		}
	})
}

// BenchmarkRemoveJavaScriptComments benchmarks the comment removal function
func BenchmarkRemoveJavaScriptComments(b *testing.B) {
	testCases := []struct {
		name  string
		input string
	}{
		{
			name:  "simple code no comments",
			input: "const x = 1;\nconst y = 2;\nconst z = 3;",
		},
		{
			name:  "code with comments",
			input: "// Comment\nconst x = 1; // inline\n/* block */ const y = 2;",
		},
		{
			name: "real world script",
			input: `// @ts-check
/// <reference types="@actions/github-script" />

async function main() {
  const { eventName } = context;
  
  // skip check for safe events
  const safeEvents = ["workflow_dispatch", "schedule"];
  if (safeEvents.includes(eventName)) {
    core.info('Event does not require validation');
    return;
  }
  
  /* Process the event */
  const actor = context.actor;
  const { owner, repo } = context.repo;
  
  // Return result
  return { actor, owner, repo };
}`,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				removeJavaScriptComments(tc.input)
			}
		})
	}
}
