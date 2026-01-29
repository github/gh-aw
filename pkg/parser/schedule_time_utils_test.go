//go:build !integration

package parser

import "testing"

func TestMapWeekday(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sunday", "0"},
		{"Sunday", "0"},
		{"sun", "0"},
		{"monday", "1"},
		{"Monday", "1"},
		{"mon", "1"},
		{"tuesday", "2"},
		{"tue", "2"},
		{"wednesday", "3"},
		{"wed", "3"},
		{"thursday", "4"},
		{"thu", "4"},
		{"friday", "5"},
		{"fri", "5"},
		{"saturday", "6"},
		{"sat", "6"},
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapWeekday(tt.input)
			if result != tt.expected {
				t.Errorf("mapWeekday(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		input        string
		expectedMin  string
		expectedHour string
	}{
		{"midnight", "0", "0"},
		{"noon", "0", "12"},
		{"00:00", "0", "0"},
		{"12:00", "0", "12"},
		{"06:30", "30", "6"},
		{"23:59", "59", "23"},
		{"09:15", "15", "9"},
		// AM/PM formats
		{"1am", "0", "1"},
		{"3pm", "0", "15"},
		{"12am", "0", "0"},  // midnight
		{"12pm", "0", "12"}, // noon
		{"11pm", "0", "23"},
		{"6am", "0", "6"},
		{"8 am", "0", "8"},
		{"9am", "0", "9"},
		{"5pm", "0", "17"},
		{"10pm", "0", "22"},
		// Invalid formats fall back to defaults
		{"invalid", "0", "0"},
		{"25:00", "0", "0"},
		{"12:60", "0", "0"},
		{"12", "0", "0"},
		{"13pm", "0", "0"}, // invalid hour for 12-hour format
		{"0am", "0", "0"},  // invalid hour for 12-hour format
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			min, hour := parseTime(tt.input)
			if min != tt.expectedMin || hour != tt.expectedHour {
				t.Errorf("parseTime(%q) = (%q, %q), want (%q, %q)",
					tt.input, min, hour, tt.expectedMin, tt.expectedHour)
			}
		})
	}
}

func TestParseTimeToMinutes(t *testing.T) {
	tests := []struct {
		hourStr   string
		minuteStr string
		expected  int
	}{
		{"0", "0", 0},
		{"12", "0", 720},
		{"23", "59", 1439},
		{"6", "30", 390},
		{"18", "45", 1125},
	}

	for _, tt := range tests {
		t.Run(tt.hourStr+":"+tt.minuteStr, func(t *testing.T) {
			result := parseTimeToMinutes(tt.hourStr, tt.minuteStr)
			if result != tt.expected {
				t.Errorf("parseTimeToMinutes(%q, %q) = %d, want %d",
					tt.hourStr, tt.minuteStr, result, tt.expected)
			}
		})
	}
}

func TestParseUTCOffset(t *testing.T) {
	tests := []struct {
		input    string
		expected int // offset in minutes
	}{
		{"utc+0", 0},
		{"utc+9", 540},     // 9 hours * 60
		{"utc-5", -300},    // -5 hours * 60
		{"utc+5:30", 330},  // 5 hours 30 mins
		{"utc-8:00", -480}, // -8 hours
		{"utc", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseUTCOffset(tt.input)
			if result != tt.expected {
				t.Errorf("parseUTCOffset(%q) = %d, want %d",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseHourMinute(t *testing.T) {
	tests := []struct {
		input         string
		hour          int
		minute        int
		shouldSucceed bool
	}{
		{"12:30", 12, 30, true},
		{"9:15", 9, 15, true},
		{"23:59", 23, 59, true},
		{"0:0", 0, 0, true},
		{"12", 12, 0, true},
		{"9", 9, 0, true},
		{"invalid", 0, 0, false},
		{"12:60:30", 0, 0, false}, // too many parts
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hour, minute, ok := parseHourMinute(tt.input)
			if ok != tt.shouldSucceed {
				t.Errorf("parseHourMinute(%q) success = %v, want %v",
					tt.input, ok, tt.shouldSucceed)
			}
			if tt.shouldSucceed && (hour != tt.hour || minute != tt.minute) {
				t.Errorf("parseHourMinute(%q) = (%d, %d), want (%d, %d)",
					tt.input, hour, minute, tt.hour, tt.minute)
			}
		})
	}
}

func TestIsAMPMToken(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"am", true},
		{"AM", true},
		{"pm", true},
		{"PM", true},
		{"Am", true},
		{"Pm", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isAMPMToken(tt.input)
			if result != tt.expected {
				t.Errorf("isAMPMToken(%q) = %v, want %v",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeTimezoneAbbreviation(t *testing.T) {
	tests := []struct {
		input           string
		expected        string
		shouldNormalize bool
	}{
		{"pst", "utc-8", true},
		{"PST", "utc-8", true},
		{"pt", "utc-8", true},
		{"pdt", "utc-7", true},
		{"est", "utc-5", true},
		{"edt", "utc-4", true},
		{"invalid", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := normalizeTimezoneAbbreviation(tt.input)
			if ok != tt.shouldNormalize {
				t.Errorf("normalizeTimezoneAbbreviation(%q) normalized = %v, want %v",
					tt.input, ok, tt.shouldNormalize)
			}
			if tt.shouldNormalize && result != tt.expected {
				t.Errorf("normalizeTimezoneAbbreviation(%q) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeTimeTokens(t *testing.T) {
	tests := []struct {
		name     string
		tokens   []string
		expected string
	}{
		{
			name:     "single time token",
			tokens:   []string{"12:30"},
			expected: "12:30",
		},
		{
			name:     "time with AM",
			tokens:   []string{"9", "am"},
			expected: "9 am",
		},
		{
			name:     "time with PM",
			tokens:   []string{"3", "pm"},
			expected: "3 pm",
		},
		{
			name:     "time with UTC offset",
			tokens:   []string{"14:00", "utc+9"},
			expected: "14:00 utc+9",
		},
		{
			name:     "time with AM and UTC",
			tokens:   []string{"9", "am", "utc-5"},
			expected: "9 am utc-5",
		},
		{
			name:     "time with timezone abbreviation",
			tokens:   []string{"10:00", "pst"},
			expected: "10:00 utc-8",
		},
		{
			name:     "empty tokens",
			tokens:   []string{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeTimeTokens(tt.tokens)
			if result != tt.expected {
				t.Errorf("normalizeTimeTokens(%v) = %q, want %q",
					tt.tokens, result, tt.expected)
			}
		})
	}
}
