//go:build !integration

package cli

import "testing"

func TestFrontmatterRefQuotedScalarBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		body  string
		rest  string
		ok    bool
	}{
		{name: "empty"},
		{name: "single opening quote", value: "'"},
		{name: "double opening quote", value: `"`},
		{name: "empty single quoted scalar", value: "''", ok: true},
		{name: "empty double quoted scalar", value: `""`, ok: true},
		{name: "single quoted scalar", value: "'repo@v1':", body: "repo@v1", rest: ":", ok: true},
		{name: "escaped single quote", value: "'it''s'", body: "it's", ok: true},
		{name: "escaped double quote", value: `"a\"b" tail`, body: `a"b`, rest: " tail", ok: true},
		{name: "trailing escape", value: `"a\`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, rest, ok := cutQuotedYAMLScalar(tt.value)
			if body != tt.body || rest != tt.rest || ok != tt.ok {
				t.Errorf("cutQuotedYAMLScalar(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.value, body, rest, ok, tt.body, tt.rest, tt.ok)
			}
		})
	}
}

func TestFrontmatterRefReplacementBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		line       string
		candidates []frontmatterRefCandidate
		applied    []bool
		want       string
		wantApply  bool
	}{
		{name: "empty"},
		{name: "no candidates", line: "repo@v1", want: "repo@v1"},
		{
			name:       "nil applied",
			line:       "repo@v1",
			candidates: []frontmatterRefCandidate{{search: "repo@v1", replacement: "repo@v2"}},
			want:       "repo@v1",
		},
		{
			name: "invalid candidates before valid match",
			line: "repo@v1 # keep",
			candidates: []frontmatterRefCandidate{
				{update: -1, search: "repo@v1", replacement: "invalid"},
				{update: 1, search: "repo@v1", replacement: "invalid"},
				{update: 2, search: "repo@v1", replacement: "invalid"},
				{update: 0, search: "repo@v1", replacement: "repo@v2"},
			},
			applied:   []bool{false},
			want:      "repo@v2 # keep",
			wantApply: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceFrontmatterRefValues(tt.line, tt.candidates, tt.applied)
			if got != tt.want {
				t.Errorf("replaceFrontmatterRefValues() = %q, want %q", got, tt.want)
			}
			for _, applied := range tt.applied {
				if applied != tt.wantApply {
					t.Errorf("applied = %v, want %v", applied, tt.wantApply)
				}
			}
		})
	}
}

func TestFrontmatterRefBackslashEscapeBounds(t *testing.T) {
	t.Parallel()
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name  string
		value string
		index int
		want  bool
	}{
		{name: "empty"},
		{name: "negative", value: `\`, index: -1},
		{name: "minimum integer", value: `\`, index: -maxInt - 1},
		{name: "maximum integer", value: `\`, index: maxInt},
		{name: "empty past end", index: 1},
		{name: "zero", value: `\`},
		{name: "past end", value: `\`, index: 2},
		{name: "equal length odd", value: `\`, index: 1, want: true},
		{name: "equal length even", value: `\\`, index: 2},
		{name: "three backslashes", value: `\\\`, index: 3, want: true},
		{name: "unicode before backslash", value: "é\\", index: 3, want: true},
		{name: "invalid UTF-8 before backslash", value: "\xff\\", index: 2, want: true},
		{name: "escaped quote", value: `\"`, index: 1, want: true},
		{name: "unescaped quote", value: `\\"`, index: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBackslashEscaped(tt.value, tt.index); got != tt.want {
				t.Errorf("isBackslashEscaped(%q, %d) = %v, want %v", tt.value, tt.index, got, tt.want)
			}
		})
	}
}
