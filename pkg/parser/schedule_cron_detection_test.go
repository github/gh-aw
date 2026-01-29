//go:build !integration

package parser

import "testing"

func TestIsCronExpression(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0 0 * * *", true},
		{"*/15 * * * *", true},
		{"0 14 * * 1-5", true},
		{"30 6 * * 1", true},
		{"0 12 25 12 *", true},
		{"daily", false},
		{"weekly on monday", false},
		{"every 10 minutes", false},
		{"0 0 * *", false},         // Too few fields
		{"0 0 * * * *", false},     // Too many fields
		{"0 0 * * * extra", false}, // Extra tokens
		{"invalid cron expression", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsCronExpression(tt.input)
			if result != tt.expected {
				t.Errorf("IsCronExpression(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsDailyCron(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0 0 * * *", true},
		{"30 14 * * *", true},
		{"0 9 * * *", true},
		{"*/15 * * * *", false}, // interval
		{"0 0 1 * *", false},    // monthly
		{"0 0 * * 1", false},    // weekly
		{"0 14 * * 1-5", false}, // weekdays only
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsDailyCron(tt.input)
			if result != tt.expected {
				t.Errorf("IsDailyCron(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsHourlyCron(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0 */1 * * *", true},
		{"30 */2 * * *", true},
		{"15 */6 * * *", true},
		{"0 0 * * *", false},    // daily, not hourly interval
		{"*/30 * * * *", false}, // minute interval, not hourly
		{"0 0 1 * *", false},    // monthly
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsHourlyCron(tt.input)
			if result != tt.expected {
				t.Errorf("IsHourlyCron(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsWeeklyCron(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0 0 * * 1", true},
		{"30 14 * * 5", true},
		{"0 9 * * 0", true},
		{"0 17 * * 6", true},
		{"*/15 * * * *", false}, // interval
		{"0 0 1 * *", false},    // monthly
		{"0 0 * * *", false},    // daily
		{"0 14 * * 1-5", false}, // weekdays range (not simple weekly)
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsWeeklyCron(tt.input)
			if result != tt.expected {
				t.Errorf("IsWeeklyCron(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsFuzzyCron(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"FUZZY:DAILY * * *", true},
		{"FUZZY:HOURLY * * *", true},
		{"FUZZY:WEEKLY * * *", true},
		{"0 0 * * *", false},
		{"daily", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsFuzzyCron(tt.input)
			if result != tt.expected {
				t.Errorf("IsFuzzyCron(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
