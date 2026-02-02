//go:build !integration

package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestIsUnderWorkflowsDirectory(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "file under .github/workflows",
			filePath: "/some/path/.github/workflows/test.md",
			expected: true,
		},
		{
			name:     "file under .github/workflows subdirectory",
			filePath: "/some/path/.github/workflows/shared/helper.md",
			expected: false, // Files in subdirectories are not top-level workflow files
		},
		{
			name:     "file outside .github/workflows",
			filePath: "/some/path/docs/instructions.md",
			expected: false,
		},
		{
			name:     "file in .github but not workflows",
			filePath: "/some/path/.github/ISSUE_TEMPLATE/bug.md",
			expected: false,
		},
		{
			name:     "relative path under workflows",
			filePath: ".github/workflows/test.md",
			expected: true,
		},
		{
			name:     "relative path outside workflows",
			filePath: "docs/readme.md",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isUnderWorkflowsDirectory(tt.filePath)
			if result != tt.expected {
				t.Errorf("isUnderWorkflowsDirectory(%q) = %v, want %v", tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestIsCustomAgentFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "file under .github/agents with .md extension",
			filePath: "/some/path/.github/agents/test-agent.md",
			expected: true,
		},
		{
			name:     "file under .github/agents with .agent.md extension",
			filePath: "/some/path/.github/agents/feature-flag-remover.agent.md",
			expected: true,
		},
		{
			name:     "file under .github/agents subdirectory",
			filePath: "/some/path/.github/agents/subdir/helper.md",
			expected: true, // Still an agent file even in subdirectory
		},
		{
			name:     "file outside .github/agents",
			filePath: "/some/path/docs/instructions.md",
			expected: false,
		},
		{
			name:     "file in .github/workflows",
			filePath: "/some/path/.github/workflows/test.md",
			expected: false,
		},
		{
			name:     "file in .github but not agents",
			filePath: "/some/path/.github/ISSUE_TEMPLATE/bug.md",
			expected: false,
		},
		{
			name:     "relative path under agents",
			filePath: ".github/agents/test-agent.md",
			expected: true,
		},
		{
			name:     "file under agents but not markdown",
			filePath: ".github/agents/config.json",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCustomAgentFile(tt.filePath)
			if result != tt.expected {
				t.Errorf("isCustomAgentFile(%q) = %v, want %v", tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestResolveIncludePath(t *testing.T) {
	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "test_resolve")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create regular test file in temp dir
	regularFile := filepath.Join(tempDir, "regular.md")
	if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to write regular file: %v", err)
	}

	tests := []struct {
		name     string
		filePath string
		baseDir  string
		expected string
		wantErr  bool
	}{
		{
			name:     "regular relative path",
			filePath: "regular.md",
			baseDir:  tempDir,
			expected: regularFile,
		},
		{
			name:     "regular file not found",
			filePath: "nonexistent.md",
			baseDir:  tempDir,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveIncludePath(tt.filePath, tt.baseDir, nil)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveIncludePath() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ResolveIncludePath() error = %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("ResolveIncludePath() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExtractWorkflowNameFromMarkdown(t *testing.T) {
	// Create temporary test files
	tempDir, err := os.MkdirTemp("", "test-extract-name-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name        string
		content     string
		expected    string
		expectError bool
	}{
		{
			name: "file with H1 header",
			content: `---
name: Test Workflow
---

# Daily QA Report

This is a test workflow.`,
			expected:    "Daily QA Report",
			expectError: false,
		},
		{
			name: "file without H1 header",
			content: `---
name: Test Workflow
---

This is content without H1 header.
## This is H2`,
			expected:    "Test Extract Name", // Should generate from filename
			expectError: false,
		},
		{
			name: "file with multiple H1 headers",
			content: `---
name: Test Workflow
---

# First Header

Some content.

# Second Header

Should use first H1.`,
			expected:    "First Header",
			expectError: false,
		},
		{
			name: "file with only frontmatter",
			content: `---
name: Test Workflow
description: A test
---`,
			expected:    "Test Extract Name", // Should generate from filename
			expectError: false,
		},
		{
			name: "file with H1 and extra spaces",
			content: `---
name: Test
---

#   Spaced Header   

Content here.`,
			expected:    "Spaced Header",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			fileName := "test-extract-name.md"
			filePath := filepath.Join(tempDir, fileName)

			err := os.WriteFile(filePath, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			got, err := ExtractWorkflowNameFromMarkdown(filePath)

			if tt.expectError {
				if err == nil {
					t.Errorf("ExtractWorkflowNameFromMarkdown(%q) expected error, but got none", filePath)
				}
				return
			}

			if err != nil {
				t.Errorf("ExtractWorkflowNameFromMarkdown(%q) unexpected error: %v", filePath, err)
				return
			}

			if got != tt.expected {
				t.Errorf("ExtractWorkflowNameFromMarkdown(%q) = %q, want %q", filePath, got, tt.expected)
			}
		})
	}

	// Test nonexistent file
	t.Run("nonexistent file", func(t *testing.T) {
		_, err := ExtractWorkflowNameFromMarkdown("/nonexistent/file.md")
		if err == nil {
			t.Error("ExtractWorkflowNameFromMarkdown with nonexistent file should return error")
		}
	})
}

// Test ExtractMarkdown function
func TestExtractMarkdown(t *testing.T) {
	// Create temporary test files
	tempDir, err := os.MkdirTemp("", "test-extract-markdown-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name        string
		content     string
		expected    string
		expectError bool
	}{
		{
			name: "file with frontmatter",
			content: `---
name: Test Workflow
description: A test workflow
---

# Test Content

This is the markdown content.`,
			expected:    "# Test Content\n\nThis is the markdown content.",
			expectError: false,
		},
		{
			name: "file without frontmatter",
			content: `# Pure Markdown

This is just markdown content without frontmatter.`,
			expected:    "# Pure Markdown\n\nThis is just markdown content without frontmatter.",
			expectError: false,
		},
		{
			name:        "empty file",
			content:     ``,
			expected:    "",
			expectError: false,
		},
		{
			name: "file with only frontmatter",
			content: `---
name: Test
---`,
			expected:    "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			fileName := "test-extract-markdown.md"
			filePath := filepath.Join(tempDir, fileName)

			err := os.WriteFile(filePath, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			got, err := ExtractMarkdown(filePath)

			if tt.expectError {
				if err == nil {
					t.Errorf("ExtractMarkdown(%q) expected error, but got none", filePath)
				}
				return
			}

			if err != nil {
				t.Errorf("ExtractMarkdown(%q) unexpected error: %v", filePath, err)
				return
			}

			if got != tt.expected {
				t.Errorf("ExtractMarkdown(%q) = %q, want %q", filePath, got, tt.expected)
			}
		})
	}

	// Test nonexistent file
	t.Run("nonexistent file", func(t *testing.T) {
		_, err := ExtractMarkdown("/nonexistent/file.md")
		if err == nil {
			t.Error("ExtractMarkdown with nonexistent file should return error")
		}
	})
}

// Test mergeToolsFromJSON function
func TestStripANSI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "plain text without ANSI",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "simple CSI color sequence",
			input:    "\x1b[31mRed Text\x1b[0m",
			expected: "Red Text",
		},
		{
			name:     "multiple CSI sequences",
			input:    "\x1b[1m\x1b[31mBold Red\x1b[0m\x1b[32mGreen\x1b[0m",
			expected: "Bold RedGreen",
		},
		{
			name:     "CSI cursor movement",
			input:    "Line 1\x1b[2;1HLine 2",
			expected: "Line 1Line 2",
		},
		{
			name:     "CSI erase sequences",
			input:    "Text\x1b[2JCleared\x1b[K",
			expected: "TextCleared",
		},
		{
			name:     "OSC sequence with BEL terminator",
			input:    "\x1b]0;Window Title\x07Content",
			expected: "Content",
		},
		{
			name:     "OSC sequence with ST terminator",
			input:    "\x1b]2;Terminal Title\x1b\\More content",
			expected: "More content",
		},
		{
			name:     "character set selection G0",
			input:    "\x1b(0Hello\x1b(B",
			expected: "Hello",
		},
		{
			name:     "character set selection G1",
			input:    "\x1b)0World\x1b)B",
			expected: "World",
		},
		{
			name:     "keypad mode sequences",
			input:    "\x1b=Keypad\x1b>Normal",
			expected: "KeypadNormal",
		},
		{
			name:     "reset sequence",
			input:    "Before\x1bcAfter",
			expected: "BeforeAfter",
		},
		{
			name:     "save and restore cursor",
			input:    "Start\x1b7Middle\x1b8End",
			expected: "StartMiddleEnd",
		},
		{
			name:     "index and reverse index",
			input:    "Text\x1bDDown\x1bMUp",
			expected: "TextDownUp",
		},
		{
			name:     "next line and horizontal tab set",
			input:    "Line\x1bENext\x1bHTab",
			expected: "LineNextTab",
		},
		{
			name:     "complex CSI with parameters",
			input:    "\x1b[38;5;196mBright Red\x1b[48;5;21mBlue BG\x1b[0m",
			expected: "Bright RedBlue BG",
		},
		{
			name:     "CSI with semicolon parameters",
			input:    "\x1b[1;31;42mBold red on green\x1b[0m",
			expected: "Bold red on green",
		},
		{
			name:     "malformed escape at end",
			input:    "Text\x1b",
			expected: "Text",
		},
		{
			name:     "malformed CSI at end",
			input:    "Text\x1b[31",
			expected: "Text",
		},
		{
			name:     "malformed OSC at end",
			input:    "Text\x1b]0;Title",
			expected: "Text",
		},
		{
			name:     "escape followed by invalid character",
			input:    "Text\x1bXInvalid",
			expected: "TextInvalid",
		},
		{
			name:     "consecutive escapes",
			input:    "\x1b[31m\x1b[1m\x1b[4mText\x1b[0m",
			expected: "Text",
		},
		{
			name:     "mixed content with newlines",
			input:    "Line 1\n\x1b[31mRed Line 2\x1b[0m\nLine 3",
			expected: "Line 1\nRed Line 2\nLine 3",
		},
		{
			name:     "common terminal output",
			input:    "\x1b[?25l\x1b[2J\x1b[H\x1b[32m✓\x1b[0m Success",
			expected: "✓ Success",
		},
		{
			name:     "git diff style colors",
			input:    "\x1b[32m+Added line\x1b[0m\n\x1b[31m-Removed line\x1b[0m",
			expected: "+Added line\n-Removed line",
		},
		{
			name:     "unicode content with ANSI",
			input:    "\x1b[33m🎉 Success! 测试\x1b[0m",
			expected: "🎉 Success! 测试",
		},
		{
			name:     "very long CSI sequence",
			input:    "\x1b[1;2;3;4;5;6;7;8;9;10;11;12;13;14;15mLong params\x1b[0m",
			expected: "Long params",
		},
		{
			name:     "CSI with question mark private parameter",
			input:    "\x1b[?25hCursor visible\x1b[?25l",
			expected: "Cursor visible",
		},
		{
			name:     "CSI with greater than private parameter",
			input:    "\x1b[>0cDevice attributes\x1b[>1c",
			expected: "Device attributes",
		},
		{
			name:     "all final CSI characters test",
			input:    "\x1b[@\x1b[A\x1b[B\x1b[C\x1b[D\x1b[E\x1b[F\x1b[G\x1b[H\x1b[I\x1b[J\x1b[K\x1b[L\x1b[M\x1b[N\x1b[O\x1b[P\x1b[Q\x1b[R\x1b[S\x1b[T\x1b[U\x1b[V\x1b[W\x1b[X\x1b[Y\x1b[Z\x1b[[\x1b[\\\x1b[]\x1b[^\x1b[_\x1b[`\x1b[a\x1b[b\x1b[c\x1b[d\x1b[e\x1b[f\x1b[g\x1b[h\x1b[i\x1b[j\x1b[k\x1b[l\x1b[m\x1b[n\x1b[o\x1b[p\x1b[q\x1b[r\x1b[s\x1b[t\x1b[u\x1b[v\x1b[w\x1b[x\x1b[y\x1b[z\x1b[{\x1b[|\x1b[}\x1b[~Text",
			expected: "Text",
		},
		{
			name:     "CSI with invalid final character",
			input:    "Before\x1b[31Text after",
			expected: "Beforeext after",
		},
		{
			name:     "real world lipgloss output",
			input:    "\x1b[1;38;2;80;250;123m✓\x1b[0;38;2;248;248;242m Success message\x1b[0m",
			expected: "✓ Success message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripANSI(tt.input)
			if result != tt.expected {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Test isCSIParameterChar function
func TestIsCSIParameterChar(t *testing.T) {
	tests := []struct {
		name     string
		char     byte
		expected bool
	}{
		// Valid parameter characters (0x30-0x3F, 0-?)
		{name: "0 (0x30)", char: '0', expected: true},
		{name: "9 (0x39)", char: '9', expected: true},
		{name: "; (0x3B)", char: ';', expected: true},
		{name: "? (0x3F)", char: '?', expected: true},

		// Valid intermediate characters (0x20-0x2F, space-/)
		{name: "space (0x20)", char: ' ', expected: true},
		{name: "! (0x21)", char: '!', expected: true},
		{name: "/ (0x2F)", char: '/', expected: true},

		// Invalid characters (below 0x20)
		{name: "tab (0x09)", char: '\t', expected: false},
		{name: "newline (0x0A)", char: '\n', expected: false},
		{name: "null (0x00)", char: 0x00, expected: false},

		// Invalid characters (above 0x3F)
		{name: "@ (0x40)", char: '@', expected: false},
		{name: "A (0x41)", char: 'A', expected: false},
		{name: "m (0x6D)", char: 'm', expected: false},
		{name: "~ (0x7E)", char: '~', expected: false},
		{name: "DEL (0x7F)", char: 0x7F, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCSIParameterChar(tt.char)
			if result != tt.expected {
				t.Errorf("isCSIParameterChar(%q/0x%02X) = %v, want %v", tt.char, tt.char, result, tt.expected)
			}
		})
	}
}

// Test isFinalCSIChar function
func TestIsFinalCSIChar(t *testing.T) {
	tests := []struct {
		name     string
		char     byte
		expected bool
	}{
		// Valid final characters (0x40-0x7E, @-~)
		{name: "@ (0x40)", char: '@', expected: true},
		{name: "A (0x41)", char: 'A', expected: true},
		{name: "Z (0x5A)", char: 'Z', expected: true},
		{name: "a (0x61)", char: 'a', expected: true},
		{name: "m (0x6D)", char: 'm', expected: true}, // Common color final char
		{name: "~ (0x7E)", char: '~', expected: true},

		// Invalid characters (below 0x40)
		{name: "space (0x20)", char: ' ', expected: false},
		{name: "0 (0x30)", char: '0', expected: false},
		{name: "9 (0x39)", char: '9', expected: false},
		{name: "; (0x3B)", char: ';', expected: false},
		{name: "? (0x3F)", char: '?', expected: false},

		// Invalid characters (above 0x7E)
		{name: "DEL (0x7F)", char: 0x7F, expected: false},
		{name: "high byte (0x80)", char: 0x80, expected: false},
		{name: "high byte (0xFF)", char: 0xFF, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFinalCSIChar(tt.char)
			if result != tt.expected {
				t.Errorf("isFinalCSIChar(%q/0x%02X) = %v, want %v", tt.char, tt.char, result, tt.expected)
			}
		})
	}
}

// Benchmark StripANSI function for performance
func BenchmarkStripANSI(b *testing.B) {
	testCases := []struct {
		name  string
		input string
	}{
		{
			name:  "plain text",
			input: "This is plain text without any ANSI codes",
		},
		{
			name:  "simple color",
			input: "\x1b[31mRed text\x1b[0m",
		},
		{
			name:  "complex formatting",
			input: "\x1b[1;38;2;255;0;0m\x1b[48;2;0;255;0mComplex formatting\x1b[0m",
		},
		{
			name:  "mixed content",
			input: "Normal \x1b[31mred\x1b[0m normal \x1b[32mgreen\x1b[0m normal \x1b[34mblue\x1b[0m text",
		},
		{
			name:  "long text with ANSI",
			input: strings.Repeat("\x1b[31mRed \x1b[32mGreen \x1b[34mBlue\x1b[0m ", 100),
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				StripANSI(tc.input)
			}
		})
	}
}

func TestIsWorkflowSpec(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "valid workflowspec",
			path: "owner/repo/path/to/file.md",
			want: true,
		},
		{
			name: "workflowspec with ref",
			path: "owner/repo/workflows/file.md@main",
			want: true,
		},
		{
			name: "workflowspec with section",
			path: "owner/repo/workflows/file.md#section",
			want: true,
		},
		{
			name: "workflowspec with ref and section",
			path: "owner/repo/workflows/file.md@sha123#section",
			want: true,
		},
		{
			name: "local path with .github",
			path: ".github/workflows/file.md",
			want: false,
		},
		{
			name: "relative local path",
			path: "../shared/file.md",
			want: false,
		},
		{
			name: "absolute path",
			path: "/tmp/gh-aw/gh-aw/file.md",
			want: false,
		},
		{
			name: "too few parts",
			path: "owner/repo",
			want: false,
		},
		{
			name: "local path starting with dot",
			path: "./file.md",
			want: false,
		},
		{
			name: "shared path with 2 parts",
			path: "shared/file.md",
			want: false,
		},
		{
			name: "shared path with 3 parts (mcp subdirectory)",
			path: "shared/mcp/gh-aw.md",
			want: false,
		},
		{
			name: "shared path with ref",
			path: "shared/mcp/tavily.md@main",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWorkflowSpec(tt.path)
			if got != tt.want {
				t.Errorf("isWorkflowSpec(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestProcessImportsFromFrontmatter(t *testing.T) {
	// Create temp directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a test include file
	includeFile := filepath.Join(tempDir, "include.md")
	includeContent := `---
tools:
  bash:
    allowed:
      - ls
      - cat
---
# Include Content
This is an included file.`
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatalf("Failed to write include file: %v", err)
	}

	tests := []struct {
		name          string
		frontmatter   map[string]any
		wantToolsJSON bool
		wantEngines   bool
		wantErr       bool
	}{
		{
			name: "no imports field",
			frontmatter: map[string]any{
				"on": "push",
			},
			wantToolsJSON: false,
			wantEngines:   false,
			wantErr:       false,
		},
		{
			name: "empty imports array",
			frontmatter: map[string]any{
				"on":      "push",
				"imports": []string{},
			},
			wantToolsJSON: false,
			wantEngines:   false,
			wantErr:       false,
		},
		{
			name: "valid imports",
			frontmatter: map[string]any{
				"on":      "push",
				"imports": []string{"include.md"},
			},
			wantToolsJSON: true,
			wantEngines:   false,
			wantErr:       false,
		},
		{
			name: "invalid imports type",
			frontmatter: map[string]any{
				"on":      "push",
				"imports": "not-an-array",
			},
			wantToolsJSON: false,
			wantEngines:   false,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools, engines, err := ProcessImportsFromFrontmatter(tt.frontmatter, tempDir)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ProcessImportsFromFrontmatter() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ProcessImportsFromFrontmatter() unexpected error: %v", err)
				return
			}

			if tt.wantToolsJSON {
				if tools == "" {
					t.Errorf("ProcessImportsFromFrontmatter() expected tools JSON but got empty string")
				}
				// Verify it's valid JSON
				var toolsMap map[string]any
				if err := json.Unmarshal([]byte(tools), &toolsMap); err != nil {
					t.Errorf("ProcessImportsFromFrontmatter() tools not valid JSON: %v", err)
				}
			} else {
				if tools != "" {
					t.Errorf("ProcessImportsFromFrontmatter() expected no tools but got: %s", tools)
				}
			}

			if tt.wantEngines {
				if len(engines) == 0 {
					t.Errorf("ProcessImportsFromFrontmatter() expected engines but got none")
				}
			} else {
				if len(engines) != 0 {
					t.Errorf("ProcessImportsFromFrontmatter() expected no engines but got: %v", engines)
				}
			}
		})
	}
}

// TestProcessIncludedFileWithNameAndDescription verifies that name and description fields
// do not generate warnings when processing included files outside .github/workflows/
