//go:build !integration

package semverutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpec_PublicAPI_EnsureVPrefix validates the documented behavior of
// EnsureVPrefix as described in the semverutil README.md specification.
func TestSpec_PublicAPI_EnsureVPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "adds v prefix when missing",
			input:    "1.2.3",
			expected: "v1.2.3",
		},
		{
			name:     "does not duplicate v prefix when already present",
			input:    "v1.2.3",
			expected: "v1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EnsureVPrefix(tt.input)
			assert.Equal(t, tt.expected, result, "EnsureVPrefix(%q) mismatch", tt.input)
		})
	}
}

// TestSpec_PublicAPI_IsActionVersionTag validates the documented behavior of
// IsActionVersionTag as described in the semverutil README.md specification.
func TestSpec_PublicAPI_IsActionVersionTag(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "major-only form is accepted",
			input:    "v4",
			expected: true,
		},
		{
			name:     "major.minor form is accepted",
			input:    "v4.1",
			expected: true,
		},
		{
			name:     "major.minor.patch form is accepted",
			input:    "v4.1.0",
			expected: true,
		},
		{
			name:     "prerelease suffix is not accepted",
			input:    "v4.1.0-rc",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsActionVersionTag(tt.input)
			assert.Equal(t, tt.expected, result, "IsActionVersionTag(%q) mismatch", tt.input)
		})
	}
}

// TestSpec_PublicAPI_IsValid validates the documented behavior of IsValid as
// described in the semverutil README.md specification.
func TestSpec_PublicAPI_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "bare version without v prefix is valid",
			input:    "1.2.3",
			expected: true,
		},
		{
			name:     "version with v prefix and prerelease is valid",
			input:    "v1.2.3-beta",
			expected: true,
		},
		{
			name:     "non-semver string is invalid",
			input:    "not-a-ver",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValid(tt.input)
			assert.Equal(t, tt.expected, result, "IsValid(%q) mismatch", tt.input)
		})
	}
}

// TestSpec_PublicAPI_ParseVersion validates the documented behavior of
// ParseVersion as described in the semverutil README.md specification.
func TestSpec_PublicAPI_ParseVersion(t *testing.T) {
	t.Run("parses version into structured components", func(t *testing.T) {
		ver := ParseVersion("v1.2.3")
		require.NotNil(t, ver, "ParseVersion should return non-nil for valid semver")
		assert.Equal(t, 1, ver.Major, "Major component mismatch")
		assert.Equal(t, 2, ver.Minor, "Minor component mismatch")
		assert.Equal(t, 3, ver.Patch, "Patch component mismatch")
	})

	t.Run("returns nil for invalid version string", func(t *testing.T) {
		ver := ParseVersion("not-a-version")
		assert.Nil(t, ver, "ParseVersion should return nil for invalid semver")
	})

	t.Run("Pre field is prerelease identifier without leading hyphen", func(t *testing.T) {
		ver := ParseVersion("v1.2.3-beta.1")
		require.NotNil(t, ver, "ParseVersion should return non-nil for valid prerelease semver")
		assert.Equal(t, "beta.1", ver.Pre, "Pre field should contain prerelease identifier without leading hyphen")
	})

	t.Run("Raw field is original version string without leading v prefix", func(t *testing.T) {
		ver := ParseVersion("v1.2.3")
		require.NotNil(t, ver, "ParseVersion should return non-nil for valid semver")
		assert.Equal(t, "1.2.3", ver.Raw, "Raw field should contain version string without leading v prefix")
	})
}

// TestSpec_Types_SemanticVersion_IsPreciseVersion validates the documented
// behavior of SemanticVersion.IsPreciseVersion as described in the semverutil README.md.
func TestSpec_Types_SemanticVersion_IsPreciseVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{
			name:     "major.minor.patch is precise",
			version:  "v6.0.0",
			expected: true,
		},
		{
			name:     "major-only is not precise",
			version:  "v6",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver := ParseVersion(tt.version)
			require.NotNil(t, ver, "ParseVersion(%q) should not return nil", tt.version)
			result := ver.IsPreciseVersion()
			assert.Equal(t, tt.expected, result, "IsPreciseVersion() mismatch for %q", tt.version)
		})
	}
}

// TestSpec_Types_SemanticVersion_IsNewer validates the documented behavior of
// SemanticVersion.IsNewer as described in the semverutil README.md.
func TestSpec_Types_SemanticVersion_IsNewer(t *testing.T) {
	t.Run("newer version returns true", func(t *testing.T) {
		v1 := ParseVersion("v2.0.0")
		v2 := ParseVersion("v1.9.9")
		require.NotNil(t, v1)
		require.NotNil(t, v2)
		assert.True(t, v1.IsNewer(v2), "v2.0.0 should be newer than v1.9.9")
	})

	t.Run("older version returns false", func(t *testing.T) {
		v1 := ParseVersion("v1.9.9")
		v2 := ParseVersion("v2.0.0")
		require.NotNil(t, v1)
		require.NotNil(t, v2)
		assert.False(t, v1.IsNewer(v2), "v1.9.9 should not be newer than v2.0.0")
	})
}

// TestSpec_PublicAPI_Compare validates the documented behavior of Compare as
// described in the semverutil README.md specification.
func TestSpec_PublicAPI_Compare(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected int
	}{
		{
			name:     "greater version returns 1",
			v1:       "v2.0.0",
			v2:       "v1.9.9",
			expected: 1,
		},
		{
			name:     "equal versions return 0",
			v1:       "v1.0.0",
			v2:       "v1.0.0",
			expected: 0,
		},
		{
			name:     "lesser version returns -1",
			v1:       "v0.9.0",
			v2:       "v1.0.0",
			expected: -1,
		},
		{
			name:     "bare versions without v prefix are accepted",
			v1:       "2.0.0",
			v2:       "1.9.9",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Compare(tt.v1, tt.v2)
			assert.Equal(t, tt.expected, result, "Compare(%q, %q) mismatch", tt.v1, tt.v2)
		})
	}
}

// TestSpec_PublicAPI_NormalizeGitDescribeSemver validates the documented
// behavior of NormalizeGitDescribeSemver as described in the semverutil README.md.
func TestSpec_PublicAPI_NormalizeGitDescribeSemver(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips git describe commit suffix",
			input:    "v1.2.3-14-gabcdef",
			expected: "v1.2.3",
		},
		{
			name:     "strips dirty git describe commit suffix",
			input:    "v1.2.3-14-gabcdef-dirty",
			expected: "v1.2.3",
		},
		{
			name:     "preserves ordinary prerelease",
			input:    "v1.2.3-beta.1",
			expected: "v1.2.3-beta.1",
		},
		{
			name:     "preserves canonical version",
			input:    "v1.2.3",
			expected: "v1.2.3",
		},
		{
			name:     "returns trimmed invalid input unchanged",
			input:    " not-a-version ",
			expected: "not-a-version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeGitDescribeSemver(tt.input)
			assert.Equal(t, tt.expected, result, "NormalizeGitDescribeSemver(%q) mismatch", tt.input)
		})
	}
}

// TestSpec_PublicAPI_IsMorePreciseVersion validates the documented behavior of
// IsMorePreciseVersion as described in the semverutil README.md specification.
func TestSpec_PublicAPI_IsMorePreciseVersion(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected bool
	}{
		{
			name:     "more specific version sorts ahead of less specific version",
			v1:       "v4.3.0",
			v2:       "v4",
			expected: true,
		},
		{
			name:     "less specific version does not sort ahead of more specific version",
			v1:       "v4",
			v2:       "v4.3.0",
			expected: false,
		},
		{
			name:     "equal precision and equal value returns false",
			v1:       "v4.3.0",
			v2:       "v4.3.0",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsMorePreciseVersion(tt.v1, tt.v2)
			assert.Equal(t, tt.expected, result, "IsMorePreciseVersion(%q, %q) mismatch", tt.v1, tt.v2)
		})
	}
}

// TestSpec_PublicAPI_IsCompatible validates the documented behavior of
// IsCompatible as described in the semverutil README.md specification.
func TestSpec_PublicAPI_IsCompatible(t *testing.T) {
	tests := []struct {
		name             string
		pinVersion       string
		requestedVersion string
		expected         bool
	}{
		{
			name:             "same major version with full pin is compatible",
			pinVersion:       "v5.0.0",
			requestedVersion: "v5",
			expected:         true,
		},
		{
			name:             "same major version with different minor is compatible",
			pinVersion:       "v5.1.0",
			requestedVersion: "v5.0.0",
			expected:         true,
		},
		{
			name:             "different major version is not compatible",
			pinVersion:       "v6.0.0",
			requestedVersion: "v5",
			expected:         false,
		},
		{
			name:             "both invalid versions are not compatible",
			pinVersion:       "not-a-version",
			requestedVersion: "also-not-a-version",
			expected:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCompatible(tt.pinVersion, tt.requestedVersion)
			assert.Equal(t, tt.expected, result, "IsCompatible(%q, %q) mismatch", tt.pinVersion, tt.requestedVersion)
		})
	}
}
