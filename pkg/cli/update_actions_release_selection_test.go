//go:build !integration

package cli

import (
	"context"
	"strings"
	"testing"
)

func TestActionReleaseSelectionMatchesAPIAndGit(t *testing.T) {
	for _, tc := range []struct {
		name, current, want, wantError string
		releases                       []string
		allowMajor                     bool
	}{
		{name: "same major", current: "v1", releases: []string{"v2", "v1.3.0-beta.1", "v1.2.0"}, want: "v1.2.0"},
		{name: "new major", current: "v1", releases: []string{"v1.2.0", "v2", "v3.0.0-beta.1"}, allowMajor: true, want: "v2"},
		{name: "major tag preference", current: "v8", releases: []string{"v8.0.0", "v8"}, want: "v8"},
		{name: "unknown current", current: "unknown", releases: []string{"v1", "v2.1.0"}, want: "v2.1.0"},
		{name: "no compatible release", current: "v1", releases: []string{"v2"}, wantError: "no compatible release found"},
		{name: "no stable release", current: "v1", releases: []string{"bad", "v2.0.0-beta.1"}, wantError: "no valid semantic version releases found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := newTestActionUpdateDeps()
			deps.runGHReleasesAPI = func(context.Context, string) ([]byte, error) {
				return []byte(strings.Join(tc.releases, "\n")), nil
			}
			shas := make(map[string]string)
			for _, tag := range tc.releases {
				shas[tag] = "sha-for-" + tag
			}
			deps.getActionSHAForTag = func(_ context.Context, _, tag string) (string, error) {
				return shas[tag], nil
			}
			apiVersion, apiSHA, apiErr := getLatestActionReleaseWithDeps(context.Background(), deps, "actions/example", tc.current, tc.allowMajor, false)
			gitVersion, gitSHA, gitErr := selectActionGitRelease(tc.releases, shas, tc.current, tc.allowMajor, false)
			if tc.wantError != "" {
				if apiErr == nil || gitErr == nil || apiErr.Error() != tc.wantError || gitErr.Error() != tc.wantError {
					t.Fatalf("API error = %v, git error = %v, want %q", apiErr, gitErr, tc.wantError)
				}
				return
			}
			if apiErr != nil || gitErr != nil {
				t.Fatalf("API error = %v, git error = %v", apiErr, gitErr)
			}
			if apiVersion != tc.want || gitVersion != tc.want || apiSHA != shas[tc.want] || gitSHA != apiSHA {
				t.Fatalf("API = (%q, %q), git = (%q, %q), want tag %q", apiVersion, apiSHA, gitVersion, gitSHA, tc.want)
			}
		})
	}
}
