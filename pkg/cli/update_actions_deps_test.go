//go:build !integration

package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/gitutil"
)

func newTestActionUpdateDeps() actionUpdateDeps {
	return defaultActionUpdateDeps()
}

func newActionUpdateDepsWithLatestRelease(fn func(context.Context, string, string, bool, bool) (string, string, error)) actionUpdateDeps {
	deps := newTestActionUpdateDeps()
	deps.getLatestRelease = fn
	return deps
}
func TestExtractBaseRepo(t *testing.T) {
	tests := []struct {
		name       string
		actionPath string
		want       string
	}{
		{
			name:       "action without subfolder",
			actionPath: "actions/checkout",
			want:       "actions/checkout",
		},
		{
			name:       "action with one subfolder",
			actionPath: "actions/cache/restore",
			want:       "actions/cache",
		},
		{
			name:       "action with multiple subfolders",
			actionPath: "github/codeql-action/upload-sarif",
			want:       "github/codeql-action",
		},
		{
			name:       "action with deeply nested subfolders",
			actionPath: "owner/repo/path/to/action",
			want:       "owner/repo",
		},
		{
			name:       "action with only owner",
			actionPath: "owner",
			want:       "owner",
		},
		{
			name:       "empty string",
			actionPath: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitutil.ExtractBaseRepo(tt.actionPath)
			if got != tt.want {
				t.Errorf("gitutil.ExtractBaseRepo(%q) = %q, want %q", tt.actionPath, got, tt.want)
			}
		})
	}
}
func TestCachedActionUpdateDepsCachesResultsAndErrors(t *testing.T) {
	t.Parallel()
	latestCalls := 0
	releaseCalls := 0
	shaCalls := 0
	coolDownCalls := 0
	expectedErr := errors.New("rate limited")

	base := actionUpdateDeps{
		getLatestRelease: func(context.Context, string, string, bool, bool) (string, string, error) {
			latestCalls++
			return "", "", expectedErr
		},
		runGHReleasesAPI: func(context.Context, string) ([]byte, error) {
			releaseCalls++
			return nil, expectedErr
		},
		getActionSHAForTag: func(context.Context, string, string) (string, error) {
			shaCalls++
			return "", expectedErr
		},
		checkCoolDown: func(context.Context, string, string, time.Duration) coolDownCheckResult {
			coolDownCalls++
			return coolDownCheckResult{InCoolDown: true}
		},
	}

	deps := newCachedActionUpdateDeps(base)
	ctx := context.Background()

	for range 2 {
		_, _, err := deps.getLatestRelease(ctx, "actions/cache", "v4", true, false)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("getLatestRelease() error = %v, want %v", err, expectedErr)
		}
		_, err = deps.runGHReleasesAPI(ctx, "actions/cache")
		if !errors.Is(err, expectedErr) {
			t.Fatalf("runGHReleasesAPI() error = %v, want %v", err, expectedErr)
		}
		_, err = deps.getActionSHAForTag(ctx, "actions/cache", "v4")
		if !errors.Is(err, expectedErr) {
			t.Fatalf("getActionSHAForTag() error = %v, want %v", err, expectedErr)
		}
		if !deps.checkCoolDown(ctx, "actions/cache", "v4", 7*24*time.Hour).InCoolDown {
			t.Fatal("checkCoolDown() should return cached cooldown result")
		}
	}

	if latestCalls != 1 || releaseCalls != 1 || shaCalls != 1 || coolDownCalls != 1 {
		t.Fatalf("call counts = latest:%d releases:%d sha:%d cooldown:%d, want all 1",
			latestCalls, releaseCalls, shaCalls, coolDownCalls)
	}
}
func TestCachedActionUpdateDepsSharesNestedLookups(t *testing.T) {
	t.Parallel()
	releaseCalls, shaCalls := 0, 0
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	deps := newCachedActionUpdateDeps(actionUpdateDeps{
		runGHReleasesAPI: func(_ context.Context, repo string) ([]byte, error) {
			if repo != "actions/cache" {
				t.Fatalf("unexpected release repository: %s", repo)
			}
			releaseCalls++
			return []byte("v4.2.0\nv4.1.0"), nil
		},
		getActionSHAForTag: func(_ context.Context, repo, tag string) (string, error) {
			if repo != "actions/cache" || tag != "v4.2.0" {
				t.Fatalf("unexpected tag lookup: %s@%s", repo, tag)
			}
			shaCalls++
			return sha, nil
		},
	})
	for _, action := range []struct{ repo, version string }{
		{"actions/cache", "v4"},
		{"actions/cache/restore", "v4.1.0"},
		{"actions/cache/save", "v4"},
	} {
		version, gotSHA, err := deps.getLatestRelease(context.Background(), action.repo, action.version, true, false)
		if err != nil || version != "v4.2.0" || gotSHA != sha {
			t.Fatalf("unexpected resolution: %s, %s, %v", version, gotSHA, err)
		}
	}
	if releaseCalls != 1 || shaCalls != 1 {
		t.Fatalf("network calls = releases:%d sha:%d, want one each", releaseCalls, shaCalls)
	}
}

func TestIsCoreAction(t *testing.T) {
	tests := []struct {
		name string
		repo string
		want bool
	}{
		{"actions/checkout is core", "actions/checkout", true},
		{"actions/setup-go is core", "actions/setup-go", true},
		{"actions/cache/restore is core", "actions/cache/restore", true},
		{"github/codeql-action is not core", "github/codeql-action", false},
		{"docker/login-action is not core", "docker/login-action", false},
		{"super-linter/super-linter is not core", "super-linter/super-linter", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCoreAction(tt.repo)
			if got != tt.want {
				t.Errorf("isCoreAction(%q) = %v, want %v", tt.repo, got, tt.want)
			}
		})
	}
}
func TestIsGhAwNativeAction(t *testing.T) {
	tests := []struct {
		repo string
		want bool
	}{
		{"github/gh-aw-actions/setup", true},
		{"github/gh-aw/actions/setup", true},
		{"github/gh-aw/actions/setup-cli", true},
		{"actions/checkout", false},
		{"actions/setup-node", false},
		{"docker/login-action", false},
		{"github/codeql-action/upload-sarif", false},
	}
	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			got := isGhAwNativeAction(tt.repo)
			if got != tt.want {
				t.Errorf("isGhAwNativeAction(%q) = %v, want %v", tt.repo, got, tt.want)
			}
		})
	}
}
