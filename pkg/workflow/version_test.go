//go:build !integration

package workflow

import (
	"testing"
)

func TestSetVersionAndGetVersion(t *testing.T) {
	// Save original version
	originalVersion := compilerVersion
	defer func() { compilerVersion = originalVersion }()

	tests := []struct {
		version string
	}{
		{"1.0.0"},
		{"dev"},
		{"v2.3.4"},
		{""},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			SetVersion(tt.version)
			if got := GetVersion(); got != tt.version {
				t.Errorf("GetVersion() = %q, want %q", got, tt.version)
			}
		})
	}
}

func TestSetIsReleaseAndIsRelease(t *testing.T) {
	// Save original isReleaseBuild
	originalIsRelease := isReleaseBuild
	defer func() { isReleaseBuild = originalIsRelease }()

	tests := []struct {
		name     string
		value    bool
		expected bool
	}{
		{"Set true", true, true},
		{"Set false", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetIsRelease(tt.value)
			if got := IsRelease(); got != tt.expected {
				t.Errorf("IsRelease() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsReleasedVersion_WithReleaseFlag(t *testing.T) {
	// Save original state
	originalIsRelease := isReleaseBuild
	defer func() { isReleaseBuild = originalIsRelease }()

	tests := []struct {
		name          string
		isRelease     bool
		version       string
		expectedValue bool
		description   string
	}{
		{
			name:          "Release flag true with valid version",
			isRelease:     true,
			version:       "1.0.0",
			expectedValue: true,
			description:   "When isRelease is true, should return true",
		},
		{
			name:          "Release flag true with dev version",
			isRelease:     true,
			version:       "dev",
			expectedValue: true,
			description:   "When isRelease is true, should return true even with dev version",
		},
		{
			name:          "Release flag true with dirty version",
			isRelease:     true,
			version:       "1.0.0-dirty",
			expectedValue: true,
			description:   "When isRelease is true, should return true even with dirty version",
		},
		{
			name:          "Release flag false with valid semver",
			isRelease:     false,
			version:       "1.0.0",
			expectedValue: false,
			description:   "When isRelease is false, should return false regardless of version format",
		},
		{
			name:          "Release flag false with dev version",
			isRelease:     false,
			version:       "dev",
			expectedValue: false,
			description:   "When isRelease is false, should return false",
		},
		{
			name:          "Release flag false with dirty version",
			isRelease:     false,
			version:       "1.0.0-dirty",
			expectedValue: false,
			description:   "When isRelease is false, should return false",
		},
		{
			name:          "Release flag false with git hash",
			isRelease:     false,
			version:       "abc123",
			expectedValue: false,
			description:   "When isRelease is false, should return false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetIsRelease(tt.isRelease)
			got := IsReleasedVersion(tt.version)
			if got != tt.expectedValue {
				t.Errorf("IsReleasedVersion(%q) with isRelease=%v = %v, want %v\n%s",
					tt.version, tt.isRelease, got, tt.expectedValue, tt.description)
			}
		})
	}
}
