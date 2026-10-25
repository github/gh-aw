//go:build !integration

package typeutil

import (
	"reflect"
	"testing"
)

func TestParseBool(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    map[string]any
		want bool
	}{
		{name: "nil"},
		{name: "absent", m: map[string]any{}},
		{name: "true", m: map[string]any{"enabled": true}, want: true},
		{name: "false", m: map[string]any{"enabled": false}},
		{name: "wrong type", m: map[string]any{"enabled": "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseBool(tc.m, "enabled"); got != tc.want {
				t.Errorf("ParseBool() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLookupMap(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		key      string
		expected map[string]any
		ok       bool
	}{
		{
			name: "existing map value",
			input: map[string]any{
				"tool": map[string]any{"name": "Bash"},
			},
			key:      "tool",
			expected: map[string]any{"name": "Bash"},
			ok:       true,
		},
		{
			name:     "missing key",
			input:    map[string]any{},
			key:      "tool",
			expected: nil,
			ok:       false,
		},
		{
			name: "wrong type",
			input: map[string]any{
				"tool": "not-a-map",
			},
			key:      "tool",
			expected: nil,
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := LookupMap(tt.input, tt.key)
			if ok != tt.ok {
				t.Errorf("LookupMap(%v, %q) ok = %v, want %v", tt.input, tt.key, ok, tt.ok)
			}
			if ok && !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("LookupMap(%v, %q) = %v, want %v", tt.input, tt.key, result, tt.expected)
			}
		})
	}
}

func TestLookupString(t *testing.T) {
	nested := map[string]any{
		"type": "tool_use",
		"input": map[string]any{
			"command": "echo hello",
		},
	}

	if value, ok := LookupString(nested, "type"); !ok || value != "tool_use" {
		t.Errorf("LookupString(type) = (%q, %v), want (%q, true)", value, ok, "tool_use")
	}

	if _, ok := LookupString(nested, "missing"); ok {
		t.Error("LookupString should return ok=false for missing key")
	}
}

func TestLookupStringPath(t *testing.T) {
	nested := map[string]any{
		"type": "tool_use",
		"input": map[string]any{
			"command": "echo hello",
		},
	}

	if value, ok := LookupStringPath(nested, "input", "command"); !ok || value != "echo hello" {
		t.Errorf("LookupStringPath(input, command) = (%q, %v), want (%q, true)", value, ok, "echo hello")
	}

	if _, ok := LookupStringPath(nested, "input", "missing"); ok {
		t.Error("LookupStringPath should return ok=false for missing path segment")
	}

	if _, ok := LookupStringPath(nested); ok {
		t.Error("LookupStringPath should return ok=false for empty path")
	}
}
