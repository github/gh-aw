//go:build !integration

package cli

import (
	"testing"
)

func TestIsPreciseVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{
			name:     "major only - not precise",
			version:  "v6",
			expected: false,
		},
		{
			name:     "major.minor - not precise",
			version:  "v6.0",
			expected: false,
		},
		{
			name:     "major.minor.patch - precise",
			version:  "v6.0.0",
			expected: true,
		},
		{
			name:     "major.minor.patch non-zero - precise",
			version:  "v6.0.1",
			expected: true,
		},
		{
			name:     "full version - precise",
			version:  "v6.1.2",
			expected: true,
		},
		{
			name:     "without v prefix - precise",
			version:  "6.0.0",
			expected: true,
		},
		{
			name:     "single digit major - not precise",
			version:  "v1",
			expected: false,
		},
		{
			name:     "three component version - precise",
			version:  "v1.2.3",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := parseVersion(tt.version)
			if v == nil {
				t.Fatalf("Failed to parse version: %s", tt.version)
			}

			result := v.isPreciseVersion()
			if result != tt.expected {
				t.Errorf("isPreciseVersion() for %q = %v, want %v", tt.version, result, tt.expected)
			}
		})
	}
}

func TestPreciseVersionPreference(t *testing.T) {
	// Test that when comparing equal versions, major-only versions are preferred
	// This follows GitHub Actions convention of using major version tags (e.g., v8 instead of v8.0.0)
	v6 := parseVersion("v6")
	v600 := parseVersion("v6.0.0")

	if v6 == nil || v600 == nil {
		t.Fatal("Failed to parse versions")
	}

	// They should parse to the same major.minor.patch
	if v6.major != v600.major || v6.minor != v600.minor || v6.patch != v600.patch {
		t.Errorf("v6 and v6.0.0 should parse to same major.minor.patch, got v6=%+v, v600=%+v", v6, v600)
	}

	// v6.0.0 should be precise, v6 should not
	if !v600.isPreciseVersion() {
		t.Error("v6.0.0 should be precise")
	}

	if v6.isPreciseVersion() {
		t.Error("v6 should not be precise")
	}

	// Neither should be considered "newer" than the other
	if v6.isNewer(v600) {
		t.Error("v6 should not be newer than v6.0.0")
	}

	if v600.isNewer(v6) {
		t.Error("v6.0.0 should not be newer than v6")
	}
}
