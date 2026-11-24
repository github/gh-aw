//go:build !integration

package workflow

import (
	"reflect"
	"testing"
)

func TestParseClaudeArrayRecords(t *testing.T) {
	first := `[{"type":"first","id":"a"}]`
	second := `[{"type":"second"}]`
	want := []map[string]any{{"type": "first", "id": "a"}, {"type": "second"}}
	for name, log := range map[string]string{
		"array records":  " \t" + first + "\r\n\n[]\n" + second + "\n",
		"adjacent lines": first + "\n" + second,
		"single array":   "[\n{\"type\":\"first\",\"id\":\"a\"},\n{\"type\":\"second\"}\n]",
		"mixed recovery": first + "\n[bad]\nnot JSON\n" + `{"type":"second"}`,
	} {
		t.Run(name, func(t *testing.T) {
			got := NewClaudeEngine().parseClaudeLogEntries(log, false)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}

func TestParseClaudeJSONArrayBuffer(t *testing.T) {
	tests := []struct {
		name string
		buf  string
		want []map[string]any
	}{
		{
			name: "valid json array of objects",
			buf:  `[{"type":"a"},{"type":"b"}]`,
			want: []map[string]any{{"type": "a"}, {"type": "b"}},
		},
		{
			name: "empty json array",
			buf:  `[]`,
			want: []map[string]any{},
		},
		{
			name: "whitespace padded valid array",
			buf:  "  [{\"k\":\"v\"}]  ",
			want: []map[string]any{{"k": "v"}},
		},
		{
			name: "surrounded by extra text extracts bracketed json",
			buf:  `garbage prefix [{"type":"a"}] trailing junk`,
			want: []map[string]any{{"type": "a"}},
		},
		{
			name: "no brackets at all returns nil",
			buf:  "no arrays here",
			want: nil,
		},
		{
			name: "only opening bracket returns nil",
			buf:  "[unterminated",
			want: nil,
		},
		{
			name: "only closing bracket returns nil",
			buf:  "unterminated]",
			want: nil,
		},
		{
			name: "close before open returns nil",
			buf:  "] some content [",
			want: nil,
		},
		{
			name: "bracketed content is invalid json returns nil",
			buf:  "[not valid json]",
			want: nil,
		},
		{
			name: "empty string returns nil",
			buf:  "",
			want: nil,
		},
		{
			name: "single object array with nested value",
			buf:  `[{"a":{"b":1}}]`,
			want: []map[string]any{{"a": map[string]any{"b": float64(1)}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseClaudeJSONArrayBuffer(tt.buf)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseClaudeJSONArrayBuffer(%q) = %#v, want %#v", tt.buf, got, tt.want)
			}
		})
	}
}
