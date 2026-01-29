//go:build !integration

package workflow

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.9.9", "2.0.0", -1},
		{"3.11", "3.9", 1},
		{"3.9", "3.11", -1},
		{"24", "20", 1},
		{"20", "24", -1},
	}

	for _, tt := range tests {
		t.Run(tt.v1+"_vs_"+tt.v2, func(t *testing.T) {
			result := compareVersions(tt.v1, tt.v2)
			if result != tt.expected {
				t.Errorf("compareVersions(%s, %s) = %d, expected %d", tt.v1, tt.v2, result, tt.expected)
			}
		})
	}
}

func TestExtractMajorVersion(t *testing.T) {
	tests := []struct {
		version  string
		expected int
	}{
		{"v5.0.0", 5},
		{"v6", 6},
		{"5.1.0", 5},
		{"v4.6.2", 4},
		{"v10.2.3", 10},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			result := extractMajorVersion(tt.version)
			if result != tt.expected {
				t.Errorf("extractMajorVersion(%q) = %d, want %d", tt.version, result, tt.expected)
			}
		})
	}
}

func TestIsSemverCompatible(t *testing.T) {
	tests := []struct {
		pinVersion       string
		requestedVersion string
		expected         bool
	}{
		{"v5.0.0", "v5", true},
		{"v5.1.0", "v5.0.0", true},
		{"v6.0.0", "v5", false},
		{"v4.6.2", "v4", true},
		{"v4.6.2", "v5", false},
		{"v10.2.3", "v10", true},
	}

	for _, tt := range tests {
		t.Run(tt.pinVersion+"_"+tt.requestedVersion, func(t *testing.T) {
			result := isSemverCompatible(tt.pinVersion, tt.requestedVersion)
			if result != tt.expected {
				t.Errorf("isSemverCompatible(%q, %q) = %v, want %v",
					tt.pinVersion, tt.requestedVersion, result, tt.expected)
			}
		})
	}
}
