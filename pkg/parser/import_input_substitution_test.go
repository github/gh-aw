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
			name:     "missing import-input falls back to string literal",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || 'fallback-literal' }}`,
			inputs:   map[string]any{},
			expected: "VALUE: fallback-literal",
		},
		{
			name:     "empty import-input falls back to string literal",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || 'fallback-literal' }}`,
			inputs:   map[string]any{"campaign": ""},
			expected: "VALUE: fallback-literal",
		},
		{
			name:     "truthy import-input wins over string literal",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || 'fallback-literal' }}`,
			inputs:   map[string]any{"campaign": "eslint-rules"},
			expected: "VALUE: eslint-rules",
		},
		{
			name:     "false literal falls through to string literal",
			content:  `VALUE: ${{ github.aw.import-inputs.enabled || false || 'fallback-literal' }}`,
			inputs:   map[string]any{"enabled": false},
			expected: "VALUE: fallback-literal",
		},
		{
			name:     "numeric literal fallback resolves",
			content:  `VALUE: ${{ github.aw.import-inputs.count || 5 }}`,
			inputs:   map[string]any{"count": 0},
			expected: "VALUE: 5",
		},
		{
			name:     "escaped quote string literal fallback resolves",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || 'fallback''literal' }}`,
			inputs:   map[string]any{},
			expected: "VALUE: fallback'literal",
		},
		{
			name:     "legacy input participates in fallback chains",
			content:  `VALUE: ${{ github.aw.inputs.campaign || github.aw.import-inputs.package || 'fallback-literal' }}`,
			inputs:   map[string]any{"package": "repo-assist"},
			expected: "VALUE: repo-assist",
		},
		{
			name:     "unsupported runtime operand leaves fallback expression untouched",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || github.event.inputs.campaign }}`,
			inputs:   map[string]any{},
			expected: `VALUE: ${{ github.aw.import-inputs.campaign || github.event.inputs.campaign }}`,
			contains: true,
		},
		{
			name:     "unformattable truthy input leaves fallback expression untouched",
			content:  `VALUE: ${{ github.aw.import-inputs.config || 'fallback-literal' }}`,
			inputs:   map[string]any{"config": map[string]any{"bad": func() {}}},
			expected: `VALUE: ${{ github.aw.import-inputs.config || 'fallback-literal' }}`,
			contains: true,
		},
		{
			name:     "unrelated fallback expression remains untouched",
			content:  `VALUE: ${{ github.event.inputs.campaign || 'fallback-literal' }}`,
			inputs:   map[string]any{},
			expected: `VALUE: ${{ github.event.inputs.campaign || 'fallback-literal' }}`,
			contains: true,
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
		{
			name:     "negative integer literal fallback resolves",
			content:  `VALUE: ${{ github.aw.import-inputs.count || -5 }}`,
			inputs:   map[string]any{"count": 0},
			expected: "VALUE: -5",
		},
		{
			name:     "negative float literal fallback resolves",
			content:  `VALUE: ${{ github.aw.import-inputs.count || -1.5 }}`,
			inputs:   map[string]any{"count": 0},
			expected: "VALUE: -1.5",
		},
		{
			name:     "null literal fallback folds to empty string",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || null }}`,
			inputs:   map[string]any{},
			expected: "VALUE: ",
		},
		{
			name:     "truthy import-input wins over null literal",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || null }}`,
			inputs:   map[string]any{"campaign": "eslint-rules"},
			expected: "VALUE: eslint-rules",
		},
		{
			name:     "array import-input value is JSON serialized",
			content:  `VALUE: ${{ github.aw.import-inputs.tags || 'fallback-literal' }}`,
			inputs:   map[string]any{"tags": []any{"a", "b"}},
			expected: `VALUE: ["a","b"]`,
		},
		{
			name:     "object import-input value is JSON serialized",
			content:  `VALUE: ${{ github.aw.import-inputs.config || 'fallback-literal' }}`,
			inputs:   map[string]any{"config": map[string]any{"k": "v"}},
			expected: `VALUE: {"k":"v"}`,
		},
		{
			name:     "empty array import-input is truthy and wins",
			content:  `VALUE: ${{ github.aw.import-inputs.tags || 'fallback-literal' }}`,
			inputs:   map[string]any{"tags": []any{}},
			expected: "VALUE: []",
		},
		{
			name:     "extra whitespace around operands is tolerated",
			content:  `VALUE: ${{   github.aw.import-inputs.campaign   ||   'fallback-literal'   }}`,
			inputs:   map[string]any{},
			expected: "VALUE: fallback-literal",
		},
		{
			name:     "hyphenated input key resolves in fallback chain",
			content:  `VALUE: ${{ github.aw.import-inputs.my-input || 'fallback-literal' }}`,
			inputs:   map[string]any{"my-input": "hyphen-value"},
			expected: "VALUE: hyphen-value",
		},
		{
			name:     "multiple independent fallback expressions in same content both resolve",
			content:  `A: ${{ github.aw.import-inputs.campaign || 'a-default' }}` + "\n" + `B: ${{ github.aw.import-inputs.package || 'b-default' }}`,
			inputs:   map[string]any{"campaign": "eslint-rules"},
			expected: "A: eslint-rules\nB: b-default",
		},
		{
			name:     "multiline expression is left untouched",
			content:  "VALUE: ${{ github.aw.import-inputs.campaign ||\n'fallback-literal' }}",
			inputs:   map[string]any{},
			expected: "VALUE: ${{ github.aw.import-inputs.campaign ||\n'fallback-literal' }}",
			contains: true,
		},
		{
			name:     "literal-only fallback without input reference is left untouched",
			content:  `VALUE: ${{ 'a' || 'b' }}`,
			inputs:   map[string]any{},
			expected: `VALUE: ${{ 'a' || 'b' }}`,
			contains: true,
		},
		{
			name:     "mixed literal and import-input fallback resolves to literal when input falsy",
			content:  `VALUE: ${{ 'literal-first' || github.aw.import-inputs.campaign }}`,
			inputs:   map[string]any{"campaign": "eslint-rules"},
			expected: "VALUE: literal-first",
		},
		{
			name:     "import-input fallback to literal containing pipe-like text in quotes",
			content:  `VALUE: ${{ github.aw.import-inputs.campaign || 'a||b' }}`,
			inputs:   map[string]any{},
			expected: "VALUE: a||b",
		},
		{
			name:     "boolean true literal fallback wins over falsy import-input",
			content:  `VALUE: ${{ github.aw.import-inputs.enabled || true }}`,
			inputs:   map[string]any{"enabled": false},
			expected: "VALUE: true",
		},
		{
			name:     "four-way fallback chain resolves to first truthy in middle",
			content:  `VALUE: ${{ github.aw.import-inputs.a || github.aw.import-inputs.b || github.aw.import-inputs.c || github.aw.import-inputs.d }}`,
			inputs:   map[string]any{"a": "", "b": "middle", "c": "unused", "d": "unused2"},
			expected: "VALUE: middle",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := substituteImportInputsInContent(tc.content, tc.inputs)
			if result != tc.expected {
				t.Errorf("substituteImportInputsInContent() = %q, want %q", result, tc.expected)
			}
			if strings.Contains(result, "github.aw.import-inputs") && !tc.contains && tc.name != "no inputs leaves single expression untouched" {
				t.Errorf("result still contains unresolved import-inputs expression: %q", result)
			}
		})
	}
}

func TestSplitFallbackOperands(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "two simple operands",
			input:    "github.aw.import-inputs.campaign || 'fallback'",
			expected: []string{"github.aw.import-inputs.campaign ", " 'fallback'"},
		},
		{
			name:     "three operands",
			input:    "a || b || c",
			expected: []string{"a ", " b ", " c"},
		},
		{
			name:     "pipe characters inside single-quoted string are not split",
			input:    "github.aw.import-inputs.campaign || 'a||b'",
			expected: []string{"github.aw.import-inputs.campaign ", " 'a||b'"},
		},
		{
			name:     "escaped single quote inside string is not treated as string terminator",
			input:    "github.aw.import-inputs.campaign || 'it''s here'",
			expected: []string{"github.aw.import-inputs.campaign ", " 'it''s here'"},
		},
		{
			name:     "no fallback operator returns single operand",
			input:    "github.aw.import-inputs.campaign",
			expected: []string{"github.aw.import-inputs.campaign"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitFallbackOperands(tc.input)
			if len(got) != len(tc.expected) {
				t.Fatalf("splitFallbackOperands(%q) = %#v, want %#v", tc.input, got, tc.expected)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("splitFallbackOperands(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.expected[i])
				}
			}
		})
	}
}

func TestResolveFallbackOperand(t *testing.T) {
	inputs := map[string]any{
		"campaign": "eslint-rules",
		"empty":    "",
		"nested":   map[string]any{"name": "nested-value"},
	}

	tests := []struct {
		name             string
		operand          string
		wantOK           bool
		wantValue        any
		wantFormatted    string
		wantInputRef     bool
		checkValue       bool
		checkFormattedEq bool
	}{
		{name: "import-inputs reference resolves", operand: "github.aw.import-inputs.campaign", wantOK: true, wantValue: "eslint-rules", wantFormatted: "eslint-rules", wantInputRef: true, checkValue: true, checkFormattedEq: true},
		{name: "legacy inputs reference resolves", operand: "github.aw.inputs.campaign", wantOK: true, wantValue: "eslint-rules", wantFormatted: "eslint-rules", wantInputRef: true, checkValue: true, checkFormattedEq: true},
		{name: "missing input reference resolves ok with empty formatted", operand: "github.aw.import-inputs.missing", wantOK: true, wantInputRef: true, checkFormattedEq: true, wantFormatted: ""},
		{name: "nested subkey resolves", operand: "github.aw.import-inputs.nested.name", wantOK: true, wantValue: "nested-value", wantFormatted: "nested-value", wantInputRef: true, checkValue: true, checkFormattedEq: true},
		{name: "true literal resolves", operand: "true", wantOK: true, wantValue: true, wantFormatted: "true", checkValue: true, checkFormattedEq: true},
		{name: "false literal resolves", operand: "false", wantOK: true, wantValue: false, wantFormatted: "false", checkValue: true, checkFormattedEq: true},
		{name: "null literal resolves to ok with nil value", operand: "null", wantOK: true, checkFormattedEq: true, wantFormatted: ""},
		{name: "quoted string literal resolves", operand: "'hello'", wantOK: true, wantValue: "hello", wantFormatted: "hello", checkValue: true, checkFormattedEq: true},
		{name: "integer literal resolves", operand: "42", wantOK: true, wantValue: int64(42), wantFormatted: "42", checkValue: true, checkFormattedEq: true},
		{name: "negative integer literal resolves", operand: "-42", wantOK: true, wantValue: int64(-42), wantFormatted: "-42", checkValue: true, checkFormattedEq: true},
		{name: "float literal resolves", operand: "1.5", wantOK: true, wantValue: 1.5, wantFormatted: "1.5", checkValue: true, checkFormattedEq: true},
		{name: "unsupported bare identifier does not resolve", operand: "github.event.inputs.campaign", wantOK: false},
		{name: "unquoted bare word does not resolve", operand: "notaliteral", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveFallbackOperand(tc.operand, inputs)
			if got.ok != tc.wantOK {
				t.Fatalf("resolveFallbackOperand(%q).ok = %v, want %v", tc.operand, got.ok, tc.wantOK)
			}
			if !got.ok {
				return
			}
			if got.isInputReference != tc.wantInputRef {
				t.Errorf("resolveFallbackOperand(%q).isInputReference = %v, want %v", tc.operand, got.isInputReference, tc.wantInputRef)
			}
			if tc.checkValue && got.value != tc.wantValue {
				t.Errorf("resolveFallbackOperand(%q).value = %#v, want %#v", tc.operand, got.value, tc.wantValue)
			}
			if tc.checkFormattedEq && got.formatted != tc.wantFormatted {
				t.Errorf("resolveFallbackOperand(%q).formatted = %q, want %q", tc.operand, got.formatted, tc.wantFormatted)
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
