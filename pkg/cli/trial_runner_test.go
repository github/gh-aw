//go:build !integration

package cli

import "testing"

func TestResolveTrialRepositoryMode(t *testing.T) {
	tests := []struct {
		name    string
		repos   TrialRepoContext
		logical string
		clone   string
		version string
		direct  bool
	}{
		{"clone with version", TrialRepoContext{CloneRepo: "owner/source@branch"}, "", "owner/source", "branch", false},
		{"logical repository", TrialRepoContext{LogicalRepo: "owner/logical@branch"}, "owner/logical", "", "", false},
		{"direct host", TrialRepoContext{HostRepo: "owner/host"}, "", "", "", true},
		{"clone takes precedence", TrialRepoContext{CloneRepo: "owner/source", LogicalRepo: "owner/logical", HostRepo: "owner/host"}, "", "owner/source", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logical, clone, version, direct, err := resolveTrialRepositoryMode(TrialOptions{Repos: tt.repos})
			if err != nil {
				t.Fatal(err)
			}
			if logical != tt.logical || clone != tt.clone || version != tt.version || direct != tt.direct {
				t.Errorf("mode = (%q, %q, %q, %v), want (%q, %q, %q, %v)", logical, clone, version, direct, tt.logical, tt.clone, tt.version, tt.direct)
			}
		})
	}
}

func TestResolveTrialRepositoryModeInvalidSpec(t *testing.T) {
	for _, repos := range []TrialRepoContext{{CloneRepo: "invalid"}, {LogicalRepo: "invalid"}} {
		if _, _, _, _, err := resolveTrialRepositoryMode(TrialOptions{Repos: repos}); err == nil {
			t.Errorf("expected an error for %+v", repos)
		}
	}
}
