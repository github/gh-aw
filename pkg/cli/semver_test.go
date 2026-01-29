//go:build !integration

package cli

import (
	"testing"
)

func TestIsSemanticVersionTag(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want bool
	}{
		{"version with v prefix", "v1.0.0", true},
		{"version without v prefix", "1.0.0", true},
		{"version with two parts", "v1.0", true},
		{"version with one part", "v1", true},
		{"version with prerelease", "v1.0.0-beta", true},
		{"version with build metadata", "v1.0.0+20230101", true},
		{"branch name", "main", false},
		{"branch name with dash", "feature-branch", false},
		{"commit sha", "abc123def456", false},
		{"random string", "hello-world", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSemanticVersionTag(tt.ref)
			if got != tt.want {
				t.Errorf("isSemanticVersionTag(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMajor int
		wantMinor int
		wantPatch int
		wantPre   string
		wantNil   bool
	}{
		{
			name:      "full version with v",
			input:     "v1.2.3",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 3,
			wantPre:   "",
			wantNil:   false,
		},
		{
			name:      "full version without v",
			input:     "1.2.3",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 3,
			wantPre:   "",
			wantNil:   false,
		},
		{
			name:      "version with prerelease",
			input:     "v1.2.3-beta.1",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 3,
			wantPre:   "beta.1",
			wantNil:   false,
		},
		{
			name:      "two-part version",
			input:     "v1.2",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 0,
			wantPre:   "",
			wantNil:   false,
		},
		{
			name:      "one-part version",
			input:     "v1",
			wantMajor: 1,
			wantMinor: 0,
			wantPatch: 0,
			wantPre:   "",
			wantNil:   false,
		},
		{
			name:    "invalid version",
			input:   "not-a-version",
			wantNil: true,
		},
		{
			name:    "branch name",
			input:   "main",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseVersion(tt.input)

			if tt.wantNil {
				if got != nil {
					t.Errorf("parseVersion(%q) = %+v, want nil", tt.input, got)
				}
				return
			}

			if got == nil {
				t.Errorf("parseVersion(%q) = nil, want non-nil", tt.input)
				return
			}

			if got.major != tt.wantMajor {
				t.Errorf("parseVersion(%q).major = %d, want %d", tt.input, got.major, tt.wantMajor)
			}
			if got.minor != tt.wantMinor {
				t.Errorf("parseVersion(%q).minor = %d, want %d", tt.input, got.minor, tt.wantMinor)
			}
			if got.patch != tt.wantPatch {
				t.Errorf("parseVersion(%q).patch = %d, want %d", tt.input, got.patch, tt.wantPatch)
			}
			if got.pre != tt.wantPre {
				t.Errorf("parseVersion(%q).pre = %q, want %q", tt.input, got.pre, tt.wantPre)
			}
		})
	}
}

func TestVersionIsNewer(t *testing.T) {
	tests := []struct {
		name    string
		version string
		other   string
		want    bool
	}{
		{"newer major", "v2.0.0", "v1.0.0", true},
		{"older major", "v1.0.0", "v2.0.0", false},
		{"newer minor", "v1.2.0", "v1.1.0", true},
		{"older minor", "v1.1.0", "v1.2.0", false},
		{"newer patch", "v1.0.2", "v1.0.1", true},
		{"older patch", "v1.0.1", "v1.0.2", false},
		{"same version", "v1.0.0", "v1.0.0", false},
		{"release vs prerelease", "v1.0.0", "v1.0.0-beta", true},
		{"prerelease vs release", "v1.0.0-beta", "v1.0.0", false},
		{"same with prerelease", "v1.0.0-beta", "v1.0.0-beta", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := parseVersion(tt.version)
			other := parseVersion(tt.other)

			if v == nil || other == nil {
				t.Fatalf("failed to parse versions: %q or %q", tt.version, tt.other)
			}

			got := v.isNewer(other)
			if got != tt.want {
				t.Errorf("(%q).isNewer(%q) = %v, want %v", tt.version, tt.other, got, tt.want)
			}
		})
	}
}
