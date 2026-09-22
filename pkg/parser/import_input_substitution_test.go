package parser

import (
	"strings"
	"testing"
)

func TestSubstituteImportInputsInContent_Fallback(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		inputs   map[string]any
		expected string
		contains bool // if true, assert expected is contained; otherwise assert exact equality
	}{
		{
			name:     "first operand truthy string wins",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}`,
			inputs:   map[string]any{"campaign": "eslint-rules", "package": "repo-assist"},
			expected: "VALUE: eslint-rules",
		},
		{
			name:     "first operand empty falls back to second",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}`,
			inputs:   map[string]any{"campaign": "", "package": "repo-assist"},
			expected: "VALUE: repo-assist",
		},
		{
			name:     "first operand missing falls back to second",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}`,
			inputs:   map[string]any{"package": "repo-assist"},
			expected: "VALUE: repo-assist",
		},
		{
			name:     "all operands missing folds to empty string",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}`,
			inputs:   map[string]any{},
			expected: "VALUE: ",
		},
		{
			name:     "all operands empty folds to empty string",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}`,
			inputs:   map[string]any{"campaign": "", "package": ""},
			expected: "VALUE: ",
		},
		{
			name:     "boolean false operand falls through",
			content:  `VALUE: ${{ github.aw.import-inputs.enabled || github.aw.import-inputs.fallback }}`,
			inputs:   map[string]any{"enabled": false, "fallback": true},
			expected: "VALUE: true",
		},
		{
			name:     "numeric zero operand falls through",
			content:  `VALUE: ${{ github.aw.import-inputs.count || github.aw.import-inputs.fallback }}`,
			inputs:   map[string]any{"count": 0, "fallback": 5},
			expected: "VALUE: 5",
		},
		{
			name:     "numeric nonzero operand wins",
			content:  `VALUE: ${{ github.aw.import-inputs.count || github.aw.import-inputs.fallback }}`,
			inputs:   map[string]any{"count": 3, "fallback": 5},
			expected: "VALUE: 3",
		},
		{
			name:     "three-way fallback chain resolves to last truthy",
			content:  `VALUE: ${{ github.aw.import-inputs.a || github.aw.import-inputs.b || github.aw.import-inputs.c }}`,
			inputs:   map[string]any{"a": "", "b": "", "c": "final"},
			expected: "VALUE: final",
		},
		{
			name:     "three-way fallback chain with all falsy folds to last operand value",
			content:  `VALUE: ${{ github.aw.import-inputs.a || github.aw.import-inputs.b || github.aw.import-inputs.c }}`,
			inputs:   map[string]any{"a": "", "b": false, "c": 0},
			expected: "VALUE: 0",
		},
		{
			name:     "nested subkey path resolution",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign.name || github.aw.import-inputs.package }}`,
			inputs:   map[string]any{"campaign": map[string]any{"name": "nested-value"}, "package": "repo-assist"},
			expected: "VALUE: nested-value",
		},
		{
			name:     "non-fallback single import-inputs expression still substitutes",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign }}`,
			inputs:   map[string]any{"campaign": "solo-value"},
			expected: "VALUE: solo-value",
		},
		{
			name:     "legacy inputs expression substitutes",
			content:  `VALUE: ${{ github.aw.inputs.campaign }}`,
			inputs:   map[string]any{"campaign": "legacy-value"},
			expected: "VALUE: legacy-value",
		},
		{
			name:     "no inputs leaves single expression untouched",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign }}`,
			inputs:   map[string]any{},
			expected: `VALUE: ${{ github.aw.import-inputs.campaign }}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := substituteImportInputsInContent(tc.content, tc.inputs)
			if result != tc.expected {
				t.Errorf("substituteImportInputsInContent() = %q, want %q", result, tc.expected)
			}
			if strings.Contains(result, "github.aw.import-inputs") && tc.name != "no inputs leaves single expression untouched" {
				t.Errorf("result still contains unresolved import-inputs expression: %q", result)
			}
		})
	}
}

func TestIsTruthyImportInput(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{name: "nil is falsy", value: nil, expected: false},
		{name: "empty string is falsy", value: "", expected: false},
		{name: "non-empty string is truthy", value: "x", expected: true},
		{name: "false bool is falsy", value: false, expected: false},
		{name: "true bool is truthy", value: true, expected: true},
		{name: "zero int is falsy", value: 0, expected: false},
		{name: "nonzero int is truthy", value: 7, expected: true},
		{name: "zero float is falsy", value: 0.0, expected: false},
		{name: "nonzero float is truthy", value: 1.5, expected: true},
		{name: "empty map is truthy", value: map[string]any{}, expected: true},
		{name: "empty slice is truthy", value: []any{}, expected: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTruthyImportInput(tc.value); got != tc.expected {
				t.Errorf("isTruthyImportInput(%#v) = %v, want %v", tc.value, got, tc.expected)
			}
		})
	}
}
