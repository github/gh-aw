//go:build !integration

package parser

import (
	"testing"
)

func TestQuoteCronExpressions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Simple unquoted cron expression",
			input: `on:
  schedule:
    - cron: 0 14 * * 1-5`,
			expected: `on:
  schedule:
    - cron: "0 14 * * 1-5"`,
		},
		{
			name: "Already quoted cron expression",
			input: `on:
  schedule:
    - cron: "0 14 * * 1-5"`,
			expected: `on:
  schedule:
    - cron: "0 14 * * 1-5"`,
		},
		{
			name: "Single quoted cron expression",
			input: `on:
  schedule:
    - cron: '0 14 * * 1-5'`,
			expected: `on:
  schedule:
    - cron: '0 14 * * 1-5'`,
		},
		{
			name: "Multiple unquoted cron expressions",
			input: `on:
  schedule:
    - cron: 0 14 * * 1-5
    - cron: 0 9 * * *`,
			expected: `on:
  schedule:
    - cron: "0 14 * * 1-5"
    - cron: "0 9 * * *"`,
		},
		{
			name: "Cron with trailing comment",
			input: `on:
  schedule:
    - cron: 0 14 * * 1-5 # Weekdays at 2pm`,
			expected: `on:
  schedule:
    - cron: "0 14 * * 1-5" # Weekdays at 2pm`,
		},
		{
			name: "Cron without dash prefix",
			input: `on:
  schedule:
    cron: 0 14 * * 1-5`,
			expected: `on:
  schedule:
    cron: "0 14 * * 1-5"`,
		},
		{
			name: "Every minute cron (starts with asterisk - not matched)",
			input: `on:
  schedule:
    - cron: * * * * *`,
			expected: `on:
  schedule:
    - cron: * * * * *`,
		},
		{
			name: "Every 5 minutes cron (starts with asterisk - not matched)",
			input: `on:
  schedule:
    - cron: */5 * * * *`,
			expected: `on:
  schedule:
    - cron: */5 * * * *`,
		},
		{
			name: "Midnight daily cron",
			input: `on:
  schedule:
    - cron: 0 0 * * *`,
			expected: `on:
  schedule:
    - cron: "0 0 * * *"`,
		},
		{
			name: "Mixed quoted and unquoted",
			input: `on:
  schedule:
    - cron: "0 9 * * *"
    - cron: 0 14 * * 1-5
    - cron: '0 18 * * *'`,
			expected: `on:
  schedule:
    - cron: "0 9 * * *"
    - cron: "0 14 * * 1-5"
    - cron: '0 18 * * *'`,
		},
		{
			name:     "Empty input",
			input:    "",
			expected: "",
		},
		{
			name: "No cron expressions",
			input: `on:
  push:
    branches: [main]`,
			expected: `on:
  push:
    branches: [main]`,
		},
		{
			name: "Cron with extra whitespace",
			input: `on:
  schedule:
    -   cron:   0 14 * * 1-5  `,
			expected: `on:
  schedule:
    -   cron:   "0 14 * * 1-5"`,
		},
		{
			name: "Multiple schedules in one workflow",
			input: `on:
  schedule:
    - cron: 0 9 * * 1
    - cron: 0 14 * * 1-5
    - cron: 0 18 * * 5`,
			expected: `on:
  schedule:
    - cron: "0 9 * * 1"
    - cron: "0 14 * * 1-5"
    - cron: "0 18 * * 5"`,
		},
		{
			name: "Cron expression with slashes and commas",
			input: `on:
  schedule:
    - cron: 0,30 */2 1,15 * *`,
			expected: `on:
  schedule:
    - cron: "0,30 */2 1,15 * *"`,
		},
		{
			name: "Cron with comment and extra spaces",
			input: `on:
  schedule:
    - cron: 0 9 * * *   # Morning run`,
			expected: `on:
  schedule:
    - cron: "0 9 * * *" # Morning run`,
		},
		{
			name:     "Only cron line in input",
			input:    `    - cron: 0 14 * * 1-5`,
			expected: `    - cron: "0 14 * * 1-5"`,
		},
		{
			name:     "Cron without list dash and with indent",
			input:    `      cron: 0 14 * * 1-5`,
			expected: `      cron: "0 14 * * 1-5"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := QuoteCronExpressions(tt.input)
			if result != tt.expected {
				t.Errorf("\nInput:\n%s\n\nExpected:\n%s\n\nGot:\n%s", tt.input, tt.expected, result)
			}
		})
	}
}
