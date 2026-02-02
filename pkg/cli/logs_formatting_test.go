//go:build !integration

package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/console"
)

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{5, "5"},
		{42, "42"},
		{999, "999"},
		{1000, "1.00k"},
		{1200, "1.20k"},
		{1234, "1.23k"},
		{12000, "12.0k"},
		{12300, "12.3k"},
		{123000, "123k"},
		{999999, "1000k"},
		{1000000, "1.00M"},
		{1200000, "1.20M"},
		{1234567, "1.23M"},
		{12000000, "12.0M"},
		{12300000, "12.3M"},
		{123000000, "123M"},
		{999999999, "1000M"},
		{1000000000, "1.00B"},
		{1200000000, "1.20B"},
		{1234567890, "1.23B"},
		{12000000000, "12.0B"},
		{123000000000, "123B"},
	}

	for _, test := range tests {
		result := console.FormatNumber(test.input)
		if result != test.expected {
			t.Errorf("console.FormatNumber(%d) = %s, expected %s", test.input, result, test.expected)
		}
	}
}

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		size     int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},          // 1.5 * 1024
		{1048576, "1.0 MB"},       // 1024 * 1024
		{2097152, "2.0 MB"},       // 2 * 1024 * 1024
		{1073741824, "1.0 GB"},    // 1024^3
		{1099511627776, "1.0 TB"}, // 1024^4
	}

	for _, tt := range tests {
		result := console.FormatFileSize(tt.size)
		if result != tt.expected {
			t.Errorf("console.FormatFileSize(%d) = %q, expected %q", tt.size, result, tt.expected)
		}
	}
}
