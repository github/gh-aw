//go:build !integration

package stringutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripANSI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "plain text unchanged",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "basic color code",
			input: "\x1b[31mred text\x1b[0m",
			want:  "red text",
		},
		{
			name:  "bold color code",
			input: "\x1b[1;32mBold Green\x1b[0m",
			want:  "Bold Green",
		},
		{
			name:  "reset code only",
			input: "text\x1b[mmore",
			want:  "textmore",
		},
		{
			name:  "CSI private parameter",
			input: "\x1b[?25l",
			want:  "",
		},
		{
			name:  "CSI intermediate character",
			input: "\x1b[1 q",
			want:  "",
		},
		{
			name:  "incomplete CSI parameters at end",
			input: "text\x1b[31",
			want:  "text",
		},
		{
			name:  "invalid CSI character ends sequence",
			input: "text\x1b[31\x01tail",
			want:  "text\x01tail",
		},
		{
			name:  "invalid two-character escape preserves following byte",
			input: "before\x1b\x01after",
			want:  "before\x01after",
		},
		{
			name:  "multiple ANSI sequences",
			input: "\x1b[31mdoes important\x1b[0m things\x1b[m",
			want:  "does important things",
		},
		{
			name:  "description with embedded ANSI",
			input: "This workflow \x1b[31mdoes important\x1b[0m things\x1b[m",
			want:  "This workflow does important things",
		},
		{
			name:  "bold ANSI in description",
			input: "Workflow with \x1b[1mANSI\x1b[0m codes",
			want:  "Workflow with ANSI codes",
		},
		{
			name:  "file path with ANSI color",
			input: "path/to/\x1b[32mfile1.md\x1b[0m",
			want:  "path/to/file1.md",
		},
		{
			name:  "stop-time with ANSI codes",
			input: "2026-12-31\x1b[31mT23:59:59Z\x1b[0m",
			want:  "2026-12-31T23:59:59Z",
		},
		{
			name:  "environment name with ANSI bold",
			input: "production-\x1b[1menv\x1b[0m",
			want:  "production-env",
		},
		{
			name:  "OSC sequence with BEL terminator",
			input: "before\x1b]0;title\x07after",
			want:  "beforeafter",
		},
		{
			name:  "OSC sequence with ST terminator",
			input: "before\x1b]0;title\x1b\\after",
			want:  "beforeafter",
		},
		{
			name:  "unterminated OSC sequence",
			input: "before\x1b]0;title",
			want:  "before",
		},
		{
			name:  "OSC ignores non-ST escape",
			input: "before\x1b]title\x1bXmore\x07after",
			want:  "beforeafter",
		},
		{
			name:  "G0 character set selection",
			input: "before\x1b(Bafter",
			want:  "beforeafter",
		},
		{
			name:  "incomplete character set selection",
			input: "before\x1b(",
			want:  "before",
		},
		{
			name:  "G1 character set selection",
			input: "before\x1b)0after",
			want:  "beforeafter",
		},
		{
			name:  "application keypad mode",
			input: "before\x1b=after",
			want:  "beforeafter",
		},
		{
			name:  "normal keypad mode",
			input: "before\x1b>after",
			want:  "beforeafter",
		},
		{
			name:  "reset sequence",
			input: "before\x1bcafter",
			want:  "beforeafter",
		},
		{
			name:  "cursor save (two-char sequence)",
			input: "before\x1b7after",
			want:  "beforeafter",
		},
		{
			name:  "cursor restore (two-char sequence)",
			input: "before\x1b8after",
			want:  "beforeafter",
		},
		{
			name:  "ESC at end of string",
			input: "text\x1b",
			want:  "text",
		},
		{
			name:  "incomplete CSI introducer at end",
			input: "text\x1b[",
			want:  "text",
		},
		{
			name:  "multiline text with ANSI codes",
			input: "Line 1 with \x1b[32mgreen\x1b[0m text\nLine 2 with \x1b[31mred\x1b[0m text",
			want:  "Line 1 with green text\nLine 2 with red text",
		},
		{
			name:  "256-color foreground",
			input: "\x1b[38;5;196mred256\x1b[0m",
			want:  "red256",
		},
		{
			name:  "256-color background",
			input: "\x1b[48;5;21mblue256bg\x1b[0m",
			want:  "blue256bg",
		},
		{
			name:  "hyperlink OSC sequence",
			input: "\x1b]8;;https://example.com\x07click\x1b]8;;\x07",
			want:  "click",
		},
		{
			name:  "no ESC but brackets preserved",
			input: "array[0] and [key]",
			want:  "array[0] and [key]",
		},
		{
			name:  "source path with ANSI",
			input: "\x1b[33mowner/repo\x1b[0m@v1.2.3",
			want:  "owner/repo@v1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripANSI(tt.input)
			assert.Equal(t, tt.want, got, "StripANSI(%q)", tt.input)
			assert.NotContains(t, got, "\x1b", "StripANSI(%q) left an ESC byte", tt.input)
		})
	}
}

func TestIsFinalCSIChar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		char byte
		want bool
	}{
		{name: "before final range", char: 0x3F, want: false},
		{name: "first final character", char: 0x40, want: true},
		{name: "last final character", char: 0x7E, want: true},
		{name: "after final range", char: 0x7F, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isFinalCSIChar(tt.char))
		})
	}
}

func TestIsCSIParameterChar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		char byte
		want bool
	}{
		{name: "below parameter range", char: 0x1F, want: false},
		{name: "first intermediate character", char: 0x20, want: true},
		{name: "last intermediate character", char: 0x2F, want: true},
		{name: "first parameter character", char: 0x30, want: true},
		{name: "after parameter range", char: 0x40, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isCSIParameterChar(tt.char))
		})
	}
}

func FuzzStripANSI(f *testing.F) {
	f.Add("")
	f.Add("plain text")
	f.Add("\x1b[31mcolored\x1b[0m")
	f.Add("\x1b]0;title\x07")
	f.Add("incomplete\x1b[")
	f.Fuzz(func(t *testing.T, input string) {
		got := StripANSI(input)
		assert.NotContains(t, got, "\x1b", "StripANSI(%q) left an ESC byte", input)
		assert.Equal(t, got, StripANSI(got), "StripANSI is not idempotent for %q", input)
	})
}
