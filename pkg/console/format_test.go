//go:build !integration

package console

import "testing"

func TestScaleBinaryBytesBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		size  int64
		units []string
		value float64
		unit  string
	}{
		{name: "empty units", size: 1024, value: 1024, unit: "B"},
		{name: "negative size", size: -1, units: []string{"KB"}, value: -1, unit: "B"},
		{name: "below first unit", size: 1023, units: []string{"KB"}, value: 1023, unit: "B"},
		{name: "first unit", size: 1024, units: []string{"KB"}, value: 1, unit: "KB"},
		{name: "capped unit", size: 1 << 20, units: []string{"KB"}, value: 1024, unit: "KB"},
		{name: "last unit", size: 1 << 20, units: []string{"KB", "MB"}, value: 1, unit: "MB"},
		{name: "maximum size", size: 1<<63 - 1, units: []string{"KB", "MB", "GB", "TB", "PB", "EB", "ZB"}, value: float64(1<<63-1) / float64(1<<60), unit: "EB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, unit := scaleBinaryBytes(tc.size, tc.units)
			if value != tc.value || unit != tc.unit {
				t.Errorf("scaleBinaryBytes() = (%v, %q), want (%v, %q)", value, unit, tc.value, tc.unit)
			}
		})
	}
}

func TestFormatFileSize(t *testing.T) {
	t.Parallel()
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
