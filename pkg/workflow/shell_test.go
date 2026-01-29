//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestShellEscapeArg(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple argument without special characters",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "argument with parentheses",
			input:    "shell(git add:*)",
			expected: "'shell(git add:*)'",
		},
		{
			name:     "argument with brackets",
			input:    "pattern[abc]",
			expected: "'pattern[abc]'",
		},
		{
			name:     "argument with spaces",
			input:    "hello world",
			expected: "'hello world'",
		},
		{
			name:     "argument with single quote",
			input:    "don't",
			expected: "'don'\\''t'",
		},
		{
			name:     "argument with asterisk",
			input:    "*.txt",
			expected: "'*.txt'",
		},
		{
			name:     "argument with dollar sign",
			input:    "$HOME",
			expected: "'$HOME'",
		},
		{
			name:     "simple flag",
			input:    "--allow-tool",
			expected: "--allow-tool",
		},
		{
			name:     "already double-quoted argument should not be escaped",
			input:    "\"$INSTRUCTION\"",
			expected: "\"$INSTRUCTION\"",
		},
		{
			name:     "already single-quoted argument should not be escaped",
			input:    "'hello world'",
			expected: "'hello world'",
		},
		{
			name:     "partial double quote should be escaped",
			input:    "hello\"world",
			expected: "'hello\"world'",
		},
		{
			name:     "empty double quotes should not be escaped",
			input:    "\"\"",
			expected: "\"\"",
		},
		{
			name:     "backslash-b sequence should be preserved",
			input:    "grep -r '\\bany\\b' pkg",
			expected: "'grep -r '\\''\\bany\\b'\\'' pkg'",
		},
		{
			name:     "backslash-n sequence should be preserved",
			input:    "echo '\\n'",
			expected: "'echo '\\''\\n'\\'''",
		},
		{
			name:     "backslash-t sequence should be preserved",
			input:    "echo '\\t'",
			expected: "'echo '\\''\\t'\\'''",
		},
		{
			name:     "literal backslash should be preserved",
			input:    "path\\to\\file",
			expected: "'path\\to\\file'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shellEscapeArg(tt.input)
			if result != tt.expected {
				t.Errorf("shellEscapeArg(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShellJoinArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{
			name:     "simple arguments",
			input:    []string{"git", "status"},
			expected: "git status",
		},
		{
			name:     "arguments with special characters",
			input:    []string{"--allow-tool", "shell(git add:*)", "--allow-tool", "shell(git commit:*)"},
			expected: "--allow-tool 'shell(git add:*)' --allow-tool 'shell(git commit:*)'",
		},
		{
			name:     "mixed arguments",
			input:    []string{"copilot", "--add-dir", "/tmp/gh-aw/", "--allow-tool", "shell(*.txt)"},
			expected: "copilot --add-dir /tmp/gh-aw/ --allow-tool 'shell(*.txt)'",
		},
		{
			name:     "prompt with pre-quoted instruction should not be escaped",
			input:    []string{"copilot", "--add-dir", "/tmp/gh-aw/", "--prompt", "\"$INSTRUCTION\""},
			expected: "copilot --add-dir /tmp/gh-aw/ --prompt \"$INSTRUCTION\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shellJoinArgs(tt.input)
			if result != tt.expected {
				t.Errorf("shellJoinArgs(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShellEscapeCommandString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple command without special characters",
			input:    "echo hello",
			expected: "\"echo hello\"",
		},
		{
			name:     "command with single-quoted arguments",
			input:    "npx --allow-tool 'shell(cat)' --allow-tool 'shell(ls)'",
			expected: "\"npx --allow-tool 'shell(cat)' --allow-tool 'shell(ls)'\"",
		},
		{
			name:     "command with double quotes",
			input:    "echo \"hello world\"",
			expected: "\"echo \\\"hello world\\\"\"",
		},
		{
			name:     "command with dollar sign (command substitution)",
			input:    "echo $(date)",
			expected: "\"echo \\$(date)\"",
		},
		{
			name:     "command with backticks",
			input:    "echo `date`",
			expected: "\"echo \\`date\\`\"",
		},
		{
			name:     "command with backslashes",
			input:    "echo \\n\\t",
			expected: "\"echo \\\\n\\\\t\"",
		},
		{
			name:     "complex copilot command",
			input:    "npx -y @github/copilot@0.0.351 --allow-tool 'github(list_workflows)' --prompt \"$(cat /tmp/prompt.txt)\"",
			expected: "\"npx -y @github/copilot@0.0.351 --allow-tool 'github(list_workflows)' --prompt \\\"\\$(cat /tmp/prompt.txt)\\\"\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shellEscapeCommandString(tt.input)
			if result != tt.expected {
				t.Errorf("shellEscapeCommandString(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildDockerCommandWithExpandableVars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple command without GITHUB_WORKSPACE",
			input:    "docker run hello",
			expected: "'docker run hello'",
		},
		{
			name:     "command with single GITHUB_WORKSPACE",
			input:    "docker run -v ${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}",
			expected: "'docker run -v '\"${GITHUB_WORKSPACE}\"':'\"${GITHUB_WORKSPACE}\"''",
		},
		{
			name:     "command with GITHUB_WORKSPACE at the start",
			input:    "${GITHUB_WORKSPACE}/file",
			expected: "''\"${GITHUB_WORKSPACE}\"'/file'",
		},
		{
			name:     "command with GITHUB_WORKSPACE at the end",
			input:    "path/to/${GITHUB_WORKSPACE}",
			expected: "'path/to/'\"${GITHUB_WORKSPACE}\"''",
		},
		{
			name:     "command with multiple GITHUB_WORKSPACE references",
			input:    "${GITHUB_WORKSPACE}/src:${GITHUB_WORKSPACE}/dst",
			expected: "''\"${GITHUB_WORKSPACE}\"'/src:'\"${GITHUB_WORKSPACE}\"'/dst'",
		},
		{
			name:     "command with GITHUB_WORKSPACE and single quote",
			input:    "it's in ${GITHUB_WORKSPACE}",
			expected: "'it'\\''s in '\"${GITHUB_WORKSPACE}\"''",
		},
		{
			name:     "complex docker command",
			input:    "docker run -v ${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw image",
			expected: "'docker run -v '\"${GITHUB_WORKSPACE}\"':'\"${GITHUB_WORKSPACE}\"':rw image'",
		},
		{
			name:     "command with spaces and no GITHUB_WORKSPACE",
			input:    "docker run hello world",
			expected: "'docker run hello world'",
		},
		{
			name:     "empty command",
			input:    "",
			expected: "",
		},
		{
			name:     "injection attempt in GITHUB_WORKSPACE context",
			input:    "${GITHUB_WORKSPACE}; rm -rf /",
			expected: "''\"${GITHUB_WORKSPACE}\"'; rm -rf /'",
		},
		{
			name:     "multiple variables mixed with GITHUB_WORKSPACE",
			input:    "${GITHUB_WORKSPACE}/src ${OTHER_VAR}/dst",
			expected: "''\"${GITHUB_WORKSPACE}\"'/src ${OTHER_VAR}/dst'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildDockerCommandWithExpandableVars(tt.input)
			if result != tt.expected {
				t.Errorf("buildDockerCommandWithExpandableVars(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildDockerCommandWithExpandableVars_PreservesVariableExpansion(t *testing.T) {
	// Test that ${GITHUB_WORKSPACE} is properly preserved for shell expansion
	input := "docker run -v ${GITHUB_WORKSPACE}:/workspace"
	result := buildDockerCommandWithExpandableVars(input)

	// The result should preserve GITHUB_WORKSPACE variable for expansion
	if !strings.Contains(result, "${GITHUB_WORKSPACE}") {
		t.Errorf("Result should preserve GITHUB_WORKSPACE variable for expansion, got: %q", result)
	}

	// GITHUB_WORKSPACE should be in double quotes for safe expansion
	if !strings.Contains(result, "\"${GITHUB_WORKSPACE}\"") {
		t.Errorf("GITHUB_WORKSPACE should be in double quotes for safe expansion, got: %q", result)
	}
}

func TestBuildDockerCommandWithExpandableVars_UnbracedVariable(t *testing.T) {
	// Test that $GITHUB_WORKSPACE (without braces) is handled
	// The current implementation only handles ${GITHUB_WORKSPACE} (with braces)
	// and treats $GITHUB_WORKSPACE as a regular shell character that gets quoted
	input := "docker run -v $GITHUB_WORKSPACE:/workspace"
	result := buildDockerCommandWithExpandableVars(input)

	// Document current behavior: unbraced $GITHUB_WORKSPACE is quoted normally
	expected := "'docker run -v $GITHUB_WORKSPACE:/workspace'"
	if result != expected {
		t.Errorf("Unbraced $GITHUB_WORKSPACE should be quoted normally (not preserved for expansion), got %q, expected %q", result, expected)
	}
}
