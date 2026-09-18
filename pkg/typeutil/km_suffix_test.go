//go:build !integration

package typeutil

import "testing"

func TestParseInt64KMSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected int64
		ok       bool
	}{
		{name: "plain integer", input: "10000000", expected: 10_000_000, ok: true},
		{name: "uppercase K suffix", input: "100000K", expected: 100_000_000, ok: true},
		{name: "lowercase m suffix", input: "100m", expected: 100_000_000, ok: true},
		{name: "whitespace trimmed", input: " 42M ", expected: 42_000_000, ok: true},
		{name: "zero invalid", input: "0", expected: 0, ok: false},
		{name: "invalid suffix", input: "10G", expected: 0, ok: false},
		{name: "invalid string", input: "abc", expected: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseInt64KMSuffix(tt.input)
			if ok != tt.ok {
				t.Fatalf("ParseInt64KMSuffix(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if got != tt.expected {
				t.Fatalf("ParseInt64KMSuffix(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeInt64KMSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
		ok       bool
	}{
		{name: "plain integer", input: "1234", expected: "1234", ok: true},
		{name: "uppercase M suffix", input: "100M", expected: "100000000", ok: true},
		{name: "lowercase k suffix", input: "250k", expected: "250000", ok: true},
		{name: "invalid string", input: "0M", expected: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeInt64KMSuffix(tt.input)
			if ok != tt.ok {
				t.Fatalf("NormalizeInt64KMSuffix(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if got != tt.expected {
				t.Fatalf("NormalizeInt64KMSuffix(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
