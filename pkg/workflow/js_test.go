//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestFormatJavaScriptForYAML(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		expected []string
	}{
		{
			name:     "empty string",
			script:   "",
			expected: []string{},
		},
		{
			name:   "single line without empty lines",
			script: "console.log('hello');",
			expected: []string{
				"            console.log('hello');\n",
			},
		},
		{
			name:   "multiple lines without empty lines",
			script: "const x = 1;\nconsole.log(x);",
			expected: []string{
				"            const x = 1;\n",
				"            console.log(x);\n",
			},
		},
		{
			name:   "script with empty lines should skip them",
			script: "const x = 1;\n\nconsole.log(x);\n\nreturn x;",
			expected: []string{
				"            const x = 1;\n",
				"            console.log(x);\n",
				"            return x;\n",
			},
		},
		{
			name:   "script with only whitespace lines should skip them",
			script: "const x = 1;\n   \n\t\nconsole.log(x);",
			expected: []string{
				"            const x = 1;\n",
				"            console.log(x);\n",
			},
		},
		{
			name:   "script with leading and trailing empty lines",
			script: "\n\nconst x = 1;\nconsole.log(x);\n\n",
			expected: []string{
				"            const x = 1;\n",
				"            console.log(x);\n",
			},
		},
		{
			name:   "script with indented code",
			script: "if (true) {\n  console.log('indented');\n}",
			expected: []string{
				"            if (true) {\n",
				"              console.log('indented');\n",
				"            }\n",
			},
		},
		{
			name:   "complex script with mixed content",
			script: "// Comment\nconst github = require('@actions/github');\n\nconst token = process.env.GITHUB_TOKEN;\n\n// Another comment\nif (token) {\n  console.log('Token found');\n}\n",
			expected: []string{
				"            const github = require('@actions/github');\n",
				"            const token = process.env.GITHUB_TOKEN;\n",
				"            if (token) {\n",
				"              console.log('Token found');\n",
				"            }\n",
			},
		},
		{
			name:     "script with only single-line comments should produce empty result",
			script:   "// First comment\n// Second comment\n//Third comment",
			expected: []string{},
		},
		{
			name:   "script with block comments",
			script: "/* Block comment */\nconst x = 1;\n/* Another\n   multiline\n   comment */\nreturn x;",
			expected: []string{
				"            const x = 1;\n",
				"            return x;\n",
			},
		},
		{
			name:   "script with comments inside strings should preserve them",
			script: "const url = \"https://example.com// not a comment\";\nconst msg = 'This /* is not */ a comment';\n// This is a real comment\nconst template = `// this ${variable} /* should */ stay`;",
			expected: []string{
				"            const url = \"https://example.com// not a comment\";\n",
				"            const msg = 'This /* is not */ a comment';\n",
				"            const template = `// this ${variable} /* should */ stay`;\n",
			},
		},
		{
			name:   "script with mixed comments and strings",
			script: "// Initial comment\nconst str = \"This // looks like comment\";\n/* Block comment */\nif (str.includes('//')) {\n  // Line comment\n  console.log('Found //');\n}\n/* Final comment */",
			expected: []string{
				"            const str = \"This // looks like comment\";\n",
				"            if (str.includes('//')) {\n",
				"              console.log('Found //');\n",
				"            }\n",
			},
		},
		{
			name:   "script with regular expressions should preserve them",
			script: "const regex = /\\/\\// // This should be removed but regex should stay\nconst value = 42;",
			expected: []string{
				"            const regex = /\\/\\// \n",
				"            const value = 42;\n",
			},
		},
		{
			name:   "script with complex regular expressions and comments",
			script: "const regex1 = /foo\\/bar/g; // comment after regex\nconst regex2 = /test\\/\\/path/i; // another comment\nconst problematic = /end\\/\\/with\\/slashes/; // comment\n// This is a comment\nconst simpleRegex = /simple/;\nif (text.match(/pattern\\/with\\/slashes/)) {\n  console.log('matched');\n}",
			expected: []string{
				"            const regex1 = /foo\\/bar/g; \n",
				"            const regex2 = /test\\/\\/path/i; \n",
				"            const problematic = /end\\/\\/with\\/slashes/; \n",
				"            const simpleRegex = /simple/;\n",
				"            if (text.match(/pattern\\/with\\/slashes/)) {\n",
				"              console.log('matched');\n",
				"            }\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatJavaScriptForYAML(tt.script)

			if len(result) != len(tt.expected) {
				t.Errorf("FormatJavaScriptForYAML() returned %d lines, expected %d", len(result), len(tt.expected))
				t.Errorf("Got: %v", result)
				t.Errorf("Expected: %v", tt.expected)
				return
			}

			for i, line := range result {
				if line != tt.expected[i] {
					t.Errorf("FormatJavaScriptForYAML() line %d = %q, expected %q", i, line, tt.expected[i])
				}
			}
		})
	}
}

func TestWriteJavaScriptToYAML(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		expected string
	}{
		{
			name:     "empty string",
			script:   "",
			expected: "",
		},
		{
			name:     "single line without empty lines",
			script:   "console.log('hello');",
			expected: "            console.log('hello');\n",
		},
		{
			name:     "multiple lines without empty lines",
			script:   "const x = 1;\nconsole.log(x);",
			expected: "            const x = 1;\n            console.log(x);\n",
		},
		{
			name:     "script with empty lines should skip them",
			script:   "const x = 1;\n\nconsole.log(x);\n\nreturn x;",
			expected: "            const x = 1;\n            console.log(x);\n            return x;\n",
		},
		{
			name:     "script with only whitespace lines should skip them",
			script:   "const x = 1;\n   \n\t\nconsole.log(x);",
			expected: "            const x = 1;\n            console.log(x);\n",
		},
		{
			name:     "script with leading and trailing empty lines",
			script:   "\n\nconst x = 1;\nconsole.log(x);\n\n",
			expected: "            const x = 1;\n            console.log(x);\n",
		},
		{
			name:     "script with indented code",
			script:   "if (true) {\n  console.log('indented');\n}",
			expected: "            if (true) {\n              console.log('indented');\n            }\n",
		},
		{
			name:     "complex script with mixed content",
			script:   "// Comment\nconst github = require('@actions/github');\n\nconst token = process.env.GITHUB_TOKEN;\n\n// Another comment\nif (token) {\n  console.log('Token found');\n}\n",
			expected: "            const github = require('@actions/github');\n            const token = process.env.GITHUB_TOKEN;\n            if (token) {\n              console.log('Token found');\n            }\n",
		},
		{
			name:     "script with only single-line comments should produce empty result",
			script:   "// First comment\n// Second comment\n//Third comment",
			expected: "",
		},
		{
			name:     "script with block comments",
			script:   "/* Block comment */\nconst x = 1;\n/* Another\n   multiline\n   comment */\nreturn x;",
			expected: "            const x = 1;\n            return x;\n",
		},
		{
			name:     "script with comments inside strings should preserve them",
			script:   "const url = \"https://example.com// not a comment\";\nconst msg = 'This /* is not */ a comment';\n// This is a real comment\nconst template = `// this ${variable} /* should */ stay`;",
			expected: "            const url = \"https://example.com// not a comment\";\n            const msg = 'This /* is not */ a comment';\n            const template = `// this ${variable} /* should */ stay`;\n",
		},
		{
			name:     "script with mixed comments and strings",
			script:   "// Initial comment\nconst str = \"This // looks like comment\";\n/* Block comment */\nif (str.includes('//')) {\n  // Line comment\n  console.log('Found //');\n}\n/* Final comment */",
			expected: "            const str = \"This // looks like comment\";\n            if (str.includes('//')) {\n              console.log('Found //');\n            }\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			WriteJavaScriptToYAML(&yaml, tt.script)
			result := yaml.String()

			if result != tt.expected {
				t.Errorf("WriteJavaScriptToYAML() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestFormatJavaScriptForYAMLProducesValidIndentation(t *testing.T) {
	script := "const x = 1;\nif (x > 0) {\n  console.log('positive');\n}"
	result := FormatJavaScriptForYAML(script)

	// Check that all lines start with proper indentation (12 spaces)
	for i, line := range result {
		if !strings.HasPrefix(line, "            ") {
			t.Errorf("Line %d does not start with proper indentation: %q", i, line)
		}
		if !strings.HasSuffix(line, "\n") {
			t.Errorf("Line %d does not end with newline: %q", i, line)
		}
	}
}

func TestWriteJavaScriptToYAMLProducesValidIndentation(t *testing.T) {
	script := "const x = 1;\nif (x > 0) {\n  console.log('positive');\n}"
	var yaml strings.Builder
	WriteJavaScriptToYAML(&yaml, script)
	result := yaml.String()

	lines := strings.Split(result, "\n")
	// Remove last empty line from split
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Check that all lines start with proper indentation (12 spaces)
	for i, line := range lines {
		if !strings.HasPrefix(line, "            ") {
			t.Errorf("Line %d does not start with proper indentation: %q", i, line)
		}
	}
}

func TestJavaScriptFormattingConsistency(t *testing.T) {
	// Test that both functions produce equivalent output
	testScript := "const x = 1;\n\nconsole.log(x);\n\nreturn x;"

	// Test FormatJavaScriptForYAML
	formattedLines := FormatJavaScriptForYAML(testScript)
	formattedResult := strings.Join(formattedLines, "")

	// Test WriteJavaScriptToYAML
	var yaml strings.Builder
	WriteJavaScriptToYAML(&yaml, testScript)
	writeResult := yaml.String()

	if formattedResult != writeResult {
		t.Errorf("FormatJavaScriptForYAML and WriteJavaScriptToYAML produce different results")
		t.Errorf("FormatJavaScriptForYAML: %q", formattedResult)
		t.Errorf("WriteJavaScriptToYAML: %q", writeResult)
	}
}

func BenchmarkFormatJavaScriptForYAML(b *testing.B) {
	script := `const github = require('@actions/github');
const core = require('@actions/core');

const token = process.env.GITHUB_TOKEN;
const context = github.context;

if (!token) {
  core.setFailed('GITHUB_TOKEN is required');
  return;
}

const octokit = github.getOctokit(token);

// Create a pull request
const result = await octokit.rest.pulls.create({
  owner: context.repo.owner,
  repo: context.repo.repo,
  title: 'Automated PR',
  head: 'feature-branch',
  base: 'main',
  body: 'This is an automated pull request'
});

console.log('PR created:', result.data.html_url);`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FormatJavaScriptForYAML(script)
	}
}

func BenchmarkWriteJavaScriptToYAML(b *testing.B) {
	script := `const github = require('@actions/github');
const core = require('@actions/core');

const token = process.env.GITHUB_TOKEN;
const context = github.context;

if (!token) {
  core.setFailed('GITHUB_TOKEN is required');
  return;
}

const octokit = github.getOctokit(token);

// Create a pull request
const result = await octokit.rest.pulls.create({
  owner: context.repo.owner,
  repo: context.repo.repo,
  title: 'Automated PR',
  head: 'feature-branch',
  base: 'main',
  body: 'This is an automated pull request'
});

console.log('PR created:', result.data.html_url);`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var yaml strings.Builder
		WriteJavaScriptToYAML(&yaml, script)
	}
}
