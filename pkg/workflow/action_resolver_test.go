//go:build !integration

package workflow

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestExtractBaseRepo(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		expected string
	}{
		{
			name:     "simple repo",
			repo:     "actions/checkout",
			expected: "actions/checkout",
		},
		{
			name:     "repo with subpath",
			repo:     "github/codeql-action/upload-sarif",
			expected: "github/codeql-action",
		},
		{
			name:     "repo with multiple subpaths",
			repo:     "owner/repo/sub/path",
			expected: "owner/repo",
		},
		{
			name:     "single part repo",
			repo:     "myrepo",
			expected: "myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gitutil.ExtractBaseRepo(tt.repo)
			if result != tt.expected {
				t.Errorf("gitutil.ExtractBaseRepo(%q) = %q, want %q", tt.repo, result, tt.expected)
			}
		})
	}
}

func TestActionResolverCache(t *testing.T) {
	// Create a cache and resolver
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)
	resolver := NewActionResolver(cache)

	// Manually add an entry to the cache
	cache.Set("actions/checkout", "v5", "test-sha-123")

	// Resolve should return cached value without making API call
	sha, err := resolver.ResolveSHA(context.Background(), "actions/checkout", "v5")
	if err != nil {
		t.Errorf("Expected no error for cached entry, got: %v", err)
	}
	if sha != "test-sha-123" {
		t.Errorf("Expected SHA 'test-sha-123', got '%s'", sha)
	}
}

func TestActionResolverFailedResolutionCache(t *testing.T) {
	// Create a cache and resolver
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)
	resolver := NewActionResolver(cache)

	// Attempt to resolve a non-existent action
	// This will fail since we don't have a valid GitHub API connection in tests
	repo := "nonexistent/action"
	version := "v999.999.999"

	// First attempt should try to resolve
	_, err1 := resolver.ResolveSHA(context.Background(), repo, version)
	if err1 == nil {
		t.Error("Expected error for non-existent action on first attempt")
	}

	// Verify the failed resolution was tracked
	cacheKey := formatActionCacheKey(repo, version)
	if _, ok := resolver.failedResolutions[cacheKey]; !ok {
		t.Errorf("Expected failed resolution to be tracked for %s", cacheKey)
	}
	if _, ok := resolver.GetUsedCacheKeys()[cacheKey]; !ok {
		t.Errorf("Expected used cache keys to track attempted resolution for %s", cacheKey)
	}

	// Second attempt should be skipped and return error immediately
	_, err2 := resolver.ResolveSHA(context.Background(), repo, version)
	if err2 == nil {
		t.Error("Expected error for non-existent action on second attempt")
	}

	// Verify the error message indicates it was skipped
	expectedErrMsg := "previously failed to resolve"
	if !strings.Contains(err2.Error(), expectedErrMsg) {
		t.Errorf("Expected error message to contain %q, got: %v", expectedErrMsg, err2)
	}
	if _, ok := resolver.GetUsedCacheKeys()[cacheKey]; !ok {
		t.Errorf("Expected used cache keys to retain attempted resolution key %s", cacheKey)
	}
}

// Note: Testing the actual GitHub API resolution requires network access
// and is tested in integration tests or with network-dependent test tags

// TestLookupEmbeddedActionPin exercises the semver matching logic inside
// lookupEmbeddedActionPin directly — independently of the cache and network
// layers. It covers the three reachable code paths: precise-version match,
// range (major/minor) match, and no match.
func TestLookupEmbeddedActionPin(t *testing.T) {
	latestCheckoutPin, ok := getLatestActionPinByRepo("actions/checkout")
	if !ok || latestCheckoutPin.Version == "" {
		t.Fatal("expected latest embedded pin for actions/checkout")
	}

	major := strings.Split(strings.TrimPrefix(latestCheckoutPin.Version, "v"), ".")[0]
	if major == "" {
		t.Fatalf("failed to derive major version from %q", latestCheckoutPin.Version)
	}
	checkoutMajorTag := "v" + major

	t.Run("precise version returns SHA", func(t *testing.T) {
		sha, found := lookupEmbeddedActionPin("actions/checkout", latestCheckoutPin.Version)
		if !found {
			t.Fatalf("expected embedded pin hit for actions/checkout@%s, got not-found", latestCheckoutPin.Version)
		}
		if sha == "" {
			t.Fatalf("expected non-empty SHA for actions/checkout@%s", latestCheckoutPin.Version)
		}
	})

	t.Run("semver range returns SHA for compatible pin", func(t *testing.T) {
		sha, found := lookupEmbeddedActionPin("actions/checkout", checkoutMajorTag)
		if !found {
			t.Fatalf("expected embedded pin hit for actions/checkout@%s, got not-found", checkoutMajorTag)
		}
		if sha == "" {
			t.Fatalf("expected non-empty SHA for actions/checkout@%s", checkoutMajorTag)
		}
	})

	t.Run("unknown repo returns not-found", func(t *testing.T) {
		_, found := lookupEmbeddedActionPin("nonexistent/action", "v1")
		if found {
			t.Error("expected not-found for nonexistent/action@v1")
		}
	})

	t.Run("known repo but incompatible version returns not-found", func(t *testing.T) {
		// actions/checkout has no v1.x.x in the embedded pin set.
		_, found := lookupEmbeddedActionPin("actions/checkout", "v1")
		if found {
			t.Error("expected not-found for actions/checkout@v1 (no such pin)")
		}
	})
}

// TestParseTagRefTSV verifies that ParseTagRefTSV correctly parses the tab-separated
// output produced by the GitHub API jq expression `[.object.sha, .object.type] | @tsv`.
// This is the core parsing step used when resolving action tags to SHAs; it must
// distinguish lightweight tags (type "commit") from annotated tags (type "tag") so
// that annotated tags can be peeled to their underlying commit SHA.
func TestParseTagRefTSV(t *testing.T) {
	const (
		commitSHA    = "ea222e359276c0702a5f5203547ff9d88d0ddd76"
		tagObjectSHA = "2fe53acc038ba01c3bbdc767d4b25df31ca5bdfc"
	)

	tests := []struct {
		name        string
		input       string
		wantSHA     string
		wantType    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "lightweight tag returns commit type",
			input:    commitSHA + "\tcommit\n",
			wantSHA:  commitSHA,
			wantType: "commit",
		},
		{
			name:     "annotated tag returns tag type",
			input:    tagObjectSHA + "\ttag\n",
			wantSHA:  tagObjectSHA,
			wantType: "tag",
		},
		{
			name:     "input without trailing newline",
			input:    commitSHA + "\tcommit",
			wantSHA:  commitSHA,
			wantType: "commit",
		},
		{
			name:     "uppercase SHA is accepted",
			input:    "EA222E359276C0702A5F5203547FF9D88D0DDD76\tcommit",
			wantSHA:  "EA222E359276C0702A5F5203547FF9D88D0DDD76",
			wantType: "commit",
		},
		{
			name:        "empty input is rejected",
			input:       "",
			wantErr:     true,
			errContains: "unexpected format",
		},
		{
			name:        "missing tab separator is rejected",
			input:       commitSHA,
			wantErr:     true,
			errContains: "unexpected format",
		},
		{
			name:        "empty type field is rejected",
			input:       commitSHA + "\t",
			wantErr:     true,
			errContains: "unexpected format",
		},
		{
			name:        "short SHA is rejected",
			input:       "abc123\tcommit",
			wantErr:     true,
			errContains: "invalid SHA format",
		},
		{
			name:        "non-hex SHA is rejected",
			input:       "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz\tcommit",
			wantErr:     true,
			errContains: "invalid SHA format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sha, objType, err := ParseTagRefTSV(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseTagRefTSV(%q): expected error, got nil", tt.input)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ParseTagRefTSV(%q): error = %q, want it to contain %q", tt.input, err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseTagRefTSV(%q): unexpected error: %v", tt.input, err)
				return
			}
			if sha != tt.wantSHA {
				t.Errorf("ParseTagRefTSV(%q): sha = %q, want %q", tt.input, sha, tt.wantSHA)
			}
			if objType != tt.wantType {
				t.Errorf("ParseTagRefTSV(%q): type = %q, want %q", tt.input, objType, tt.wantType)
			}
		})
	}
}

// TestActionResolverUsedCacheKeysOnCacheHit verifies that GetUsedCacheKeys tracks
// cache hits — i.e. keys that were already in the cache and returned by ResolveSHA.
func TestActionResolverUsedCacheKeysOnCacheHit(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)
	resolver := NewActionResolver(cache)

	// Pre-populate the cache with two entries.
	cache.Set("owner/action-a", "v1", "sha_a")
	cache.Set("owner/action-b", "v2", "sha_b")

	// Resolve only action-a — it should appear in UsedCacheKeys.
	sha, err := resolver.ResolveSHA(context.Background(), "owner/action-a", "v1")
	if err != nil {
		t.Fatalf("Expected no error for cached entry, got: %v", err)
	}

	if sha != "sha_a" {
		t.Errorf("Expected sha_a, got %q", sha)
	}

	usedKeys := resolver.GetUsedCacheKeys()
	if _, ok := usedKeys["owner/action-a@v1"]; !ok {
		t.Error("Expected owner/action-a@v1 to be in used cache keys after a cache hit")
	}
	if _, ok := usedKeys["owner/action-b@v2"]; ok {
		t.Error("Expected owner/action-b@v2 to be absent from used cache keys (never resolved)")
	}
}

func TestActionResolverGetUsedCacheKeysReturnsCopy(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)
	resolver := NewActionResolver(cache)
	cache.Set("owner/action-a", "v1", "sha_a")

	if _, err := resolver.ResolveSHA(context.Background(), "owner/action-a", "v1"); err != nil {
		t.Fatalf("Expected no error resolving cache hit: %v", err)
	}

	usedKeys := resolver.GetUsedCacheKeys()
	delete(usedKeys, "owner/action-a@v1")

	if _, ok := resolver.GetUsedCacheKeys()["owner/action-a@v1"]; !ok {
		t.Error("Expected resolver used cache keys to be immutable via returned map")
	}
}

func TestActionResolverMarksCompilerGeneratedDockerActionsAsUsed(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)
	resolver := NewActionResolver(cache)

	cache.Set("docker/build-push-action", "v7.4.0", "build_push_sha")
	cache.Set("docker/setup-buildx-action", "v4.4.0", "setup_buildx_sha")

	resolver.MarkCompilerGeneratedActionsAsUsed()

	usedKeys := resolver.GetUsedCacheKeys()
	for _, key := range []string{
		"docker/build-push-action@v7.4.0",
		"docker/setup-buildx-action@v4.4.0",
	} {
		if _, ok := usedKeys[key]; !ok {
			t.Errorf("Expected compiler-generated Docker action %s to be marked used", key)
		}
	}
}

// TestForceGHHostEnvWithPresetCmdEnv verifies the non-nil cmd.Env branch of
// ForceGHHostEnv: a stale GH_HOST in a pre-populated cmd.Env is replaced,
// other env entries are preserved, and there is exactly one GH_HOST entry.
func TestForceGHHostEnvWithPresetCmdEnv(t *testing.T) {
	cmd := ExecGHContext(context.Background(), "api", "/test")
	cmd.Env = []string{"GH_HOST=stale.ghe.com", "OTHER=value"}

	ForceGHHostEnv(cmd, "github.com")

	var ghHostEntries []string
	preservedOther := false
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "GH_HOST=") {
			ghHostEntries = append(ghHostEntries, e)
		}
		if e == "OTHER=value" {
			preservedOther = true
		}
	}
	if len(ghHostEntries) != 1 {
		t.Errorf("expected exactly one GH_HOST entry, got: %v", ghHostEntries)
	} else if ghHostEntries[0] != "GH_HOST=github.com" {
		t.Errorf("expected GH_HOST=github.com, got %q", ghHostEntries[0])
	}
	if !preservedOther {
		t.Error("expected OTHER=value to be preserved in cmd.Env")
	}
}

// GH_HOST=github.com on the command environment regardless of the process-level
// GH_HOST setting, including when GH_HOST is unset, set to a GHE host, or already
// set to github.com.
func TestForceGHHostEnvSetsGitHubCom(t *testing.T) {
	tests := []struct {
		name    string
		ghHost  string
		unsetIt bool
	}{
		{
			name:    "GH_HOST unset",
			unsetIt: true,
		},
		{
			name:   "GH_HOST set to GHE host",
			ghHost: "myorg.ghe.com",
		},
		{
			name:   "GH_HOST already set to github.com",
			ghHost: "github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unsetIt {
				// Truly unset GH_HOST and restore the original value (or re-unset)
				// after the subtest so the env does not leak to subsequent tests.
				original, wasSet := os.LookupEnv("GH_HOST")
				if err := os.Unsetenv("GH_HOST"); err != nil {
					t.Fatalf("failed to unset GH_HOST: %v", err)
				}
				t.Cleanup(func() {
					if wasSet {
						os.Setenv("GH_HOST", original) //nolint:errcheck
					} else {
						os.Unsetenv("GH_HOST") //nolint:errcheck
					}
				})
			} else {
				t.Setenv("GH_HOST", tt.ghHost)
			}

			cmd := ExecGHContext(context.Background(), "api", "/repos/actions/checkout/git/ref/tags/v4", "--jq", "[.object.sha, .object.type] | @tsv")
			ForceGHHostEnv(cmd, "github.com")

			// The command env must contain exactly GH_HOST=github.com and not
			// any other GH_HOST value.
			if cmd.Env == nil {
				t.Fatal("expected cmd.Env to be set after ForceGHHostEnv, got nil")
			}

			found := slices.ContainsFunc(cmd.Env, func(e string) bool {
				return e == "GH_HOST=github.com"
			})
			if !found {
				t.Errorf("expected GH_HOST=github.com in cmd.Env, got: %v", cmd.Env)
			}

			// Verify that no other GH_HOST value is present (i.e. GHE host is not inherited).
			for _, e := range cmd.Env {
				if strings.HasPrefix(e, "GH_HOST=") && e != "GH_HOST=github.com" {
					t.Errorf("unexpected GH_HOST entry in cmd.Env: %q", e)
				}
			}
		})
	}
}
