//go:build !integration

package console

import "testing"

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
		result := FormatFileSize(tt.size)
		if result != tt.expected {
			t.Errorf("FormatFileSize(%d) = %q, expected %q", tt.size, result, tt.expected)
		}
	}
}
