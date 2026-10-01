//go:build !integration

package workflow

import "testing"

func TestParsePositiveIntValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		parse    func(any) int
		raw      any
		expected int
	}{
		{name: "max-runs int", parse: parseMaxRunsValue, raw: 3, expected: 3},
		{name: "max-runs string", parse: parseMaxRunsValue, raw: "5", expected: 5},
		{name: "max-runs invalid", parse: parseMaxRunsValue, raw: "oops", expected: 0},
		{name: "max-runs zero", parse: parseMaxRunsValue, raw: 0, expected: 0},
		{name: "max-runs negative", parse: parseMaxRunsValue, raw: -1, expected: 0},
		{name: "max-runs nil", parse: parseMaxRunsValue, raw: nil, expected: 0},
		{name: "max-turn-cache-misses int", parse: parseMaxTurnCacheMissesValue, raw: 2, expected: 2},
		{name: "max-turn-cache-misses string", parse: parseMaxTurnCacheMissesValue, raw: "7", expected: 7},
		{name: "max-turn-cache-misses invalid", parse: parseMaxTurnCacheMissesValue, raw: "bad", expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.parse(tt.raw); got != tt.expected {
				t.Fatalf("got %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestParseIntOrExpressionValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		parse    func(any) string
		raw      any
		expected string
	}{
		{name: "max-turns rejects zero", parse: parseMaxTurnsValue, raw: 0, expected: ""},
		{name: "max-turns accepts expression", parse: parseMaxTurnsValue, raw: " ${{ inputs.max_turns }} ", expected: "${{ inputs.max_turns }}"},
		{name: "max-turns trims whitespace", parse: parseMaxTurnsValue, raw: " 3 ", expected: "3"},
		{name: "non-negative accepts zero", parse: parseHarnessMaxRetriesValue, raw: 0, expected: "0"},
		{name: "non-negative accepts expression", parse: parseHarnessMaxRetriesValue, raw: "${{ inputs.max_retries }}", expected: "${{ inputs.max_retries }}"},
		{name: "non-negative trims whitespace", parse: parseHarnessMaxRetriesValue, raw: " 2 ", expected: "2"},
		{name: "max-tool-denials rejects zero", parse: parseMaxToolDenialsValue, raw: "0", expected: ""},
		{name: "max-tool-denials accepts expression", parse: parseMaxToolDenialsValue, raw: "${{ inputs.max_tool_denials }}", expected: "${{ inputs.max_tool_denials }}"},
		{name: "max-tool-calls rejects zero", parse: parseMaxToolCallsValue, raw: "0", expected: ""},
		{name: "max-tool-calls accepts expression", parse: parseMaxToolCallsValue, raw: "${{ inputs.max_tool_calls }}", expected: "${{ inputs.max_tool_calls }}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.parse(tt.raw); got != tt.expected {
				t.Fatalf("got %q, want %q", got, tt.expected)
			}
		})
	}
}
