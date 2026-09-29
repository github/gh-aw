//go:build !integration

// Package actionpins_test contains black-box specification tests for action pin resolution.
package actionpins_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/actionpins"
	"github.com/github/gh-aw/pkg/constants"
)

type testContextKey string

const testContextPropagationKey testContextKey = "actionpins.resolve.ctx"

const testResolvedSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// testSHAResolver is a fake SHAResolver used in tests.
type testSHAResolver struct {
	sha          string
	err          error
	capturedCtx  context.Context
	capturedRepo string
	capturedRef  string
}

// ResolveSHA captures call arguments and returns the configured sha/err pair.
func (r *testSHAResolver) ResolveSHA(ctx context.Context, repo, version string) (string, error) {
	r.capturedCtx = ctx
	r.capturedRepo = repo
	r.capturedRef = version
	return r.sha, r.err
}

// TestSpec_PublicAPI_FormatPinnedActionReference validates the documented format "repo@sha # version".
func TestSpec_PublicAPI_FormatPinnedActionReference(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		repo      string
		sha       string
		version   string
		expected  string
		wantPanic string
	}{
		{
			name:     "formats standard reference",
			repo:     "actions/checkout",
			sha:      "abc123",
			version:  "v4",
			expected: "actions/checkout@abc123 # v4",
		},
		{
			name:     "formats reference with full 40-char sha",
			repo:     "actions/setup-go",
			sha:      "cdabf2d4679a00bef48b5a7c69a9b8d0b4f6e3c9",
			version:  "v5",
			expected: "actions/setup-go@cdabf2d4679a00bef48b5a7c69a9b8d0b4f6e3c9 # v5",
		},
		{
			name:     "formats reference with empty version comment",
			repo:     "actions/checkout",
			sha:      "abc123",
			version:  "",
			expected: "actions/checkout@abc123 # ",
		},
		{
			name:      "panics for empty sha",
			repo:      "actions/checkout",
			sha:       "",
			version:   "v4",
			wantPanic: "FormatPinnedActionReference called with empty SHA for repo=actions/checkout version=v4 — this would produce invalid workflow YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.wantPanic != "" {
				assert.Empty(t, tt.expected, "test case %q: wantPanic and expected are mutually exclusive", tt.name)
				require.PanicsWithValue(t, tt.wantPanic, func() {
					actionpins.FormatPinnedActionReference(tt.repo, tt.sha, tt.version)
				})
				return
			}

			result := actionpins.FormatPinnedActionReference(tt.repo, tt.sha, tt.version)
			assert.Equal(t, tt.expected, result, "FormatPinnedActionReference(%q, %q, %q) should match spec format", tt.repo, tt.sha, tt.version)
		})
	}
}

// TestSpec_PublicAPI_FormatCacheKey validates the documented format "repo@version".
func TestSpec_PublicAPI_FormatCacheKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		repo     string
		version  string
		expected string
	}{
		{
			name:     "formats cache key as repo@version",
			repo:     "actions/checkout",
			version:  "v4",
			expected: "actions/checkout@v4",
		},
		{
			name:     "formats cache key with full semver",
			repo:     "actions/setup-node",
			version:  "v3.0.0",
			expected: "actions/setup-node@v3.0.0",
		},
		{
			name:     "formats cache key with empty repo",
			repo:     "",
			version:  "v4",
			expected: "@v4",
		},
		{
			name:     "formats cache key with empty version",
			repo:     "actions/checkout",
			version:  "",
			expected: "actions/checkout@",
		},
		{
			name:     "formats cache key with empty repo and version",
			repo:     "",
			version:  "",
			expected: "@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := actionpins.FormatCacheKey(tt.repo, tt.version)
			assert.Equal(t, tt.expected, result, "FormatCacheKey(%q, %q) should match spec format", tt.repo, tt.version)
		})
	}
}

// TestSpec_PublicAPI_ExtractRepo validates extracting the repository from a uses reference.
func TestSpec_PublicAPI_ExtractRepo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		uses     string
		expected string
	}{
		{
			name:     "extracts repo from tag reference",
			uses:     "actions/checkout@v4",
			expected: "actions/checkout",
		},
		{
			name:     "extracts repo from sha reference",
			uses:     "actions/setup-go@cdabf2d4679a00bef48b5a7c69a9b8d0b4f6e3c9",
			expected: "actions/setup-go",
		},
		{
			name:     "no @ separator returns full string",
			uses:     "actions/checkout",
			expected: "actions/checkout",
		},
		{
			name:     "empty string returns empty string",
			uses:     "",
			expected: "",
		},
		{
			name:     "leading @ returns empty repo",
			uses:     "@v4",
			expected: "",
		},
		{
			name:     "multiple @ separators returns part before first @",
			uses:     "actions/checkout@v4@extra",
			expected: "actions/checkout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := actionpins.ExtractRepo(tt.uses)
			assert.Equal(t, tt.expected, result, "ExtractRepo(%q) should return repo part", tt.uses)
		})
	}
}

// TestSpec_PublicAPI_ExtractVersion validates extracting the version from a uses reference.
func TestSpec_PublicAPI_ExtractVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		uses     string
		expected string
	}{
		{
			name:     "extracts tag version",
			uses:     "actions/checkout@v4",
			expected: "v4",
		},
		{
			name:     "extracts sha version",
			uses:     "actions/setup-go@abc123def456",
			expected: "abc123def456",
		},
		{
			name:     "no @ separator returns empty string",
			uses:     "actions/checkout",
			expected: "",
		},
		{
			name:     "empty string returns empty string",
			uses:     "",
			expected: "",
		},
		{
			name:     "leading @ returns version part after @",
			uses:     "@v4",
			expected: "v4",
		},
		{
			name:     "multiple @ separators returns everything after first @",
			uses:     "actions/checkout@v4@extra",
			expected: "v4@extra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := actionpins.ExtractVersion(tt.uses)
			assert.Equal(t, tt.expected, result, "ExtractVersion(%q) should return version part", tt.uses)
		})
	}
}

// TestSpec_PublicAPI_GetActionPinsByRepo validates GetActionPinsByRepo for known and unknown repos.
func TestSpec_PublicAPI_GetActionPinsByRepo(t *testing.T) {
	t.Parallel()
	t.Run("returns no pins for unknown repository", func(t *testing.T) {
		pins := actionpins.GetActionPinsByRepo("does-not-exist/unknown-action-xyzzy")
		assert.Empty(t, pins, "should return empty result for unknown repo")
	})

	t.Run("returns pins for a known repository when embedded data is loaded", func(t *testing.T) {
		known := "actions/checkout"
		pins := actionpins.GetActionPinsByRepo(known)
		assert.NotEmpty(t, pins, "should return pins for a known repo from embedded data")
	})
}

// TestSpec_PublicAPI_GetLatestActionPinByRepo validates GetLatestActionPinByRepo returns the latest pin.
func TestSpec_PublicAPI_GetLatestActionPinByRepo(t *testing.T) {
	t.Parallel()
	t.Run("returns false for unknown repository", func(t *testing.T) {
		_, ok := actionpins.GetLatestActionPinByRepo("does-not-exist/unknown-action-xyzzy")
		assert.False(t, ok, "should return false for unknown repo")
	})

	t.Run("returns a pin for a known repository", func(t *testing.T) {
		known := "actions/checkout"
		pin, ok := actionpins.GetLatestActionPinByRepo(known)
		require.True(t, ok, "should return true for a known repo")
		assert.Equal(t, known, pin.Repo, "returned pin should belong to the queried repo")
	})
}

// TestSpec_PublicAPI_ResolveActionPin validates resolution behavior.
// Spec: "fallback behavior controlled by PinContext.StrictMode"
func TestSpec_PublicAPI_ResolveActionPin(t *testing.T) {
	t.Parallel()
	t.Run("strict mode returns empty string and no error when pin is not found", func(t *testing.T) {
		ctx := &actionpins.PinContext{StrictMode: true, Warnings: make(map[string]bool)}
		result, err := actionpins.ResolveActionPin("does-not-exist/unknown-action-xyzzy", "v1", ctx)
		require.NoError(t, err, "strict mode still returns no error for unknown pin")
		assert.Empty(t, result, "strict mode should return empty reference for unknown pin")
	})
}

// TestSpec_PublicAPI_ResolveActionPin_NilContext validates nil context fallback to embedded pins.
func TestSpec_PublicAPI_ResolveActionPin_NilContext(t *testing.T) {
	t.Parallel()
	latestPin, ok := actionpins.GetLatestActionPinByRepo("actions/checkout")
	require.True(t, ok, "expected embedded pins for actions/checkout")

	result, err := actionpins.ResolveActionPin("actions/checkout", latestPin.Version, nil)
	require.NoError(t, err, "nil ctx should still resolve from embedded pins")
	assert.Equal(t,
		actionpins.FormatPinnedActionReference("actions/checkout", latestPin.SHA, latestPin.Version),
		result,
		"nil ctx should resolve from embedded pins with correct SHA and format")
}

// TestSpec_PublicAPI_ResolveActionPin_UnknownFullSHAReturnsBareReference validates that
// an unknown full SHA does not acquire a SHA-as-version comment.
func TestSpec_PublicAPI_ResolveActionPin_UnknownFullSHAReturnsBareReference(t *testing.T) {
	t.Parallel()
	unknownSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	result, err := actionpins.ResolveActionPin("actions/checkout", unknownSHA, nil)
	require.NoError(t, err)
	assert.Equal(t,
		"actions/checkout@"+unknownSHA,
		result,
		"unknown SHA should be returned as repo@sha")
}

// TestSpec_PublicAPI_ResolveActionPin_EnforcePinned validates unresolved pin handling in enforce mode.
func TestSpec_PublicAPI_ResolveActionPin_EnforcePinned(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		resolver         actionpins.SHAResolver
		allowActionRefs  bool
		wantErr          bool
		wantErrContains  string
		wantResultSHA    string
		wantFailureType  actionpins.ResolutionErrorType
		wantFailureCount int
		wantWarningKey   bool
	}{
		{
			name:             "returns error when EnforcePinned=true and pin is unresolved",
			wantErr:          true,
			wantErrContains:  "unable to pin action",
			wantFailureType:  actionpins.ResolutionErrorTypePinNotFound,
			wantFailureCount: 1,
		},
		{
			name:             "AllowActionRefs downgrades to warning with no error",
			allowActionRefs:  true,
			wantFailureType:  actionpins.ResolutionErrorTypePinNotFound,
			wantFailureCount: 1,
			wantWarningKey:   true,
		},
		{
			name:             "dynamic resolver fails with EnforcePinned=true returns error",
			resolver:         &testSHAResolver{err: errors.New("network error")},
			wantErr:          true,
			wantErrContains:  "unable to pin action",
			wantFailureType:  actionpins.ResolutionErrorTypeDynamicResolutionFailed,
			wantFailureCount: 1,
		},
		{
			name:          "resolver succeeds with EnforcePinned=true returns pinned reference",
			resolver:      &testSHAResolver{sha: testResolvedSHA},
			wantResultSHA: testResolvedSHA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var failures []actionpins.ResolutionFailure
			ctx := &actionpins.PinContext{
				Resolver:        tt.resolver,
				EnforcePinned:   true,
				AllowActionRefs: tt.allowActionRefs,
				Warnings:        make(map[string]bool),
				RecordResolutionFailure: func(f actionpins.ResolutionFailure) {
					failures = append(failures, f)
				},
			}

			result, err := actionpins.ResolveActionPin("does-not-exist/x", "v1", ctx)
			if tt.wantErr {
				require.Error(t, err, "enforce mode should return an error for this scenario")
				require.ErrorContains(t, err, tt.wantErrContains)
				assert.Empty(t, result, "erroring enforce mode should not return a pinned reference")
			} else {
				require.NoError(t, err, "non-error scenario should not return an error")
				if tt.wantResultSHA != "" {
					assert.Equal(t,
						actionpins.FormatPinnedActionReference("does-not-exist/x", tt.wantResultSHA, "v1"),
						result,
						"successful resolution should return the exact pinned reference format")
				} else {
					assert.Empty(t, result, "downgraded unresolved result should remain empty")
				}
			}

			require.Len(t, failures, tt.wantFailureCount, "resolution failures should be audited consistently")
			if tt.wantFailureCount > 0 {
				assert.Equal(t, tt.wantFailureType, failures[0].ErrorType,
					"resolution failure type should match the expected classification")
			}
			if tt.wantWarningKey {
				assert.True(t, ctx.Warnings[actionpins.FormatCacheKey("does-not-exist/x", "v1")],
					"warning downgrade should record the dedup key")
			}
		})
	}
}

// TestSpec_PublicAPI_ResolveActionPin_SkipHardcodedFallback validates that setting
// PinContext.SkipHardcodedFallback=true blocks version→SHA fallback against the
// embedded hardcoded pins while still allowing SHA→version comment labeling.
func TestSpec_PublicAPI_ResolveActionPin_SkipHardcodedFallback(t *testing.T) {
	t.Parallel()
	t.Run("known action with SkipHardcodedFallback=true returns empty result", func(t *testing.T) {
		// actions/checkout has entries in the embedded pins.
		// With SkipHardcodedFallback=true and no dynamic resolver, resolution should
		// fall through without producing a pinned reference.
		ctx := &actionpins.PinContext{
			SkipHardcodedFallback: true,
			Warnings:              make(map[string]bool),
		}
		result, err := actionpins.ResolveActionPin("actions/checkout", "v4", ctx)
		require.NoError(t, err, "SkipHardcodedFallback should not cause an error")
		assert.Empty(t, result, "SkipHardcodedFallback=true should prevent hardcoded pin lookup")
	})

	t.Run("known action with SkipHardcodedFallback=false resolves normally", func(t *testing.T) {
		ctx := &actionpins.PinContext{
			SkipHardcodedFallback: false,
			Warnings:              make(map[string]bool),
		}
		result, err := actionpins.ResolveActionPin("actions/checkout", "v4", ctx)
		require.NoError(t, err, "hardcoded pin lookup should succeed when SkipHardcodedFallback is false")
		require.NotEmpty(t, result, "SkipHardcodedFallback=false should allow hardcoded pin lookup")
		assert.Contains(t, result, "actions/checkout@", "result should reference actions/checkout")
	})

	t.Run("SHA-pinned action with SkipHardcodedFallback=true still produces version comment", func(t *testing.T) {
		// Regression test for non-deterministic pin comments bug.
		//
		// When a workflow already uses a SHA-pinned action reference (e.g.
		// actions/checkout@9c091bb... # v7.0.0) and SkipHardcodedFallback=true
		// is set (triggered when GH_HOST points to a non-github.com host), the
		// SHA→version lookup must still succeed so that the human-readable version
		// tag is preserved in the comment.
		//
		// Before the fix, the hardcoded-pin lookup was skipped entirely when
		// SkipHardcodedFallback=true, causing the fallback to emit
		// FormatPinnedActionReference(repo, sha, sha) which produces "# <sha>"
		// instead of "# v7.0.0", making the lock files non-deterministic.
		latestPin, ok := actionpins.GetLatestActionPinByRepo("actions/checkout")
		require.True(t, ok, "expected embedded pin for actions/checkout")

		ctx := &actionpins.PinContext{
			SkipHardcodedFallback: true,
			Warnings:              make(map[string]bool),
		}
		result, err := actionpins.ResolveActionPin("actions/checkout", latestPin.SHA, ctx)
		require.NoError(t, err, "SHA resolution should not return an error")
		expected := actionpins.FormatPinnedActionReference("actions/checkout", latestPin.SHA, latestPin.Version)
		assert.Equal(t, expected, result, "SHA-pinned action should use version tag as comment, not the SHA itself")
		assert.Contains(t, result, "# "+latestPin.Version, "version comment must use the human-readable tag, not the SHA")
	})
}

// TestSpec_PublicAPI_ResolveLatestActionPin validates latest-version resolution behavior.
func TestSpec_PublicAPI_ResolveLatestActionPin(t *testing.T) {
	t.Parallel()
	t.Run("returns latest pinned reference for known repository", func(t *testing.T) {
		known := "actions/checkout"
		latestPin, ok := actionpins.GetLatestActionPinByRepo(known)
		require.True(t, ok, "expected latest pin for known repository")

		result := actionpins.ResolveLatestActionPin(known, nil)
		expected := actionpins.FormatPinnedActionReference(known, latestPin.SHA, latestPin.Version)
		assert.Equal(t, expected, result, "should resolve latest pinned reference")
	})

	t.Run("returns empty string for unknown repository", func(t *testing.T) {
		result := actionpins.ResolveLatestActionPin("does-not-exist/x", nil)
		assert.Empty(t, result, "unknown repo should return empty pin")
	})
}

// TestSpec_PublicAPI_ResolveLatestActionPin_FallbackOnEnforceError validates that
// ResolveLatestActionPin falls back to the embedded latest-pin reference when ResolveActionPin
// errors, and returns empty when no embedded fallback exists.
func TestSpec_PublicAPI_ResolveLatestActionPin_FallbackOnEnforceError(t *testing.T) {
	t.Parallel()
	t.Run("known repo falls back to embedded latest pin after enforce error", func(t *testing.T) {
		known := "actions/checkout"
		latestPin, ok := actionpins.GetLatestActionPinByRepo(known)
		require.True(t, ok, "expected embedded pins for known repository")

		ctx := &actionpins.PinContext{
			Resolver:              &testSHAResolver{err: errors.New("network error")},
			EnforcePinned:         true,
			SkipHardcodedFallback: true,
			Warnings:              make(map[string]bool),
		}
		result := actionpins.ResolveLatestActionPin(known, ctx)
		assert.Equal(t,
			actionpins.FormatPinnedActionReference(known, latestPin.SHA, latestPin.Version),
			result,
			"known repo should fall back to the embedded latest pin when ResolveActionPin errors")
	})

	t.Run("unknown repo returns empty when no embedded fallback exists", func(t *testing.T) {
		var failures []actionpins.ResolutionFailure
		ctx := &actionpins.PinContext{
			EnforcePinned: true,
			Warnings:      make(map[string]bool),
			RecordResolutionFailure: func(f actionpins.ResolutionFailure) {
				failures = append(failures, f)
			},
		}

		result := actionpins.ResolveLatestActionPin("does-not-exist/x", ctx)
		assert.Empty(t, result, "unknown repo should return empty when no embedded fallback exists")
		assert.Empty(t, failures, "unknown repo without embedded pins should not invoke resolution failure callback")
	})
}

// TestSpec_Types_PinContext validates the documented PinContext type fields.
func TestSpec_Types_PinContext(t *testing.T) {
	t.Parallel()
	t.Run("strict mode disables non-exact fallback", func(t *testing.T) {
		ctx := &actionpins.PinContext{StrictMode: true, Warnings: make(map[string]bool)}
		result, err := actionpins.ResolveActionPin("actions/checkout", "v999", ctx)
		require.NoError(t, err)
		assert.Empty(t, result, "strict mode must not fall back to a non-exact version")
	})

	t.Run("nil resolver enables embedded-only lookup", func(t *testing.T) {
		latestPin, ok := actionpins.GetLatestActionPinByRepo("actions/checkout")
		require.True(t, ok, "expected embedded pins for actions/checkout")

		ctx := &actionpins.PinContext{Warnings: make(map[string]bool)}
		result, err := actionpins.ResolveActionPin("actions/checkout", latestPin.Version, ctx)
		require.NoError(t, err)
		assert.Equal(t,
			actionpins.FormatPinnedActionReference("actions/checkout", latestPin.SHA, latestPin.Version),
			result,
			"nil Resolver should use embedded pins")
	})
}

// TestSpec_DesignDecision_FormatConsistency validates that FormatPinnedActionReference and FormatCacheKey
// produce outputs consistent with the spec: cacheKey = "repo@version", ref = "repo@sha # version".
func TestSpec_DesignDecision_FormatConsistency(t *testing.T) {
	t.Parallel()
	repo := "actions/checkout"
	version := "v4"
	sha := "deadbeef"

	cacheKey := actionpins.FormatCacheKey(repo, version)
	reference := actionpins.FormatPinnedActionReference(repo, sha, version)

	assert.Truef(t, strings.HasPrefix(cacheKey, repo+"@"), "cache key should be repo@version, got %q", cacheKey)
	assert.Truef(t, strings.HasPrefix(reference, repo+"@"), "reference should start with repo@sha, got %q", reference)
	assert.Containsf(t, cacheKey, version, "cache key should contain version %q", version)
	assert.Containsf(t, reference, sha, "reference should contain sha %q", sha)
	assert.Containsf(t, reference, version, "reference should contain version comment %q", version)
}

// TestSpec_Types_ActionPinsData validates the documented ActionPinsData container type.
// Spec: ActionPinsData is a JSON container used to load embedded pin entries.
func TestSpec_Types_ActionPinsData(t *testing.T) {
	t.Parallel()
	data := actionpins.ActionPinsData{
		Entries: map[string]actionpins.ActionPin{
			"actions/checkout@v5": {Repo: "actions/checkout", Version: "v5", SHA: "abc123"},
		},
		Containers: map[string]actionpins.ContainerPin{
			"ghcr.io/example/image:latest": {
				Image:       "ghcr.io/example/image:latest",
				Digest:      "sha256:def456",
				PinnedImage: "ghcr.io/example/image@sha256:def456",
			},
		},
	}
	assert.Len(t, data.Entries, 1, "ActionPinsData.Entries should hold pin entries")
	entry := data.Entries["actions/checkout@v5"]
	assert.Equal(t, "actions/checkout", entry.Repo, "entry Repo should match")
	assert.Len(t, data.Containers, 1, "ActionPinsData.Containers should hold container pins")
}

// TestSpec_PublicAPI_ResolveActionPin_EmbeddedMatch validates embedded-only pin resolution returns
// a formatted reference for a known repository. Spec: "Embedded-only lookup from bundled pin data"
func TestSpec_PublicAPI_ResolveActionPin_EmbeddedMatch(t *testing.T) {
	t.Parallel()
	t.Run("returns formatted reference for known embedded pin", func(t *testing.T) {
		known := "actions/checkout"
		latestPin, ok := actionpins.GetLatestActionPinByRepo(known)
		require.True(t, ok, "prerequisite: known repo must be in embedded data")

		ctx := &actionpins.PinContext{StrictMode: false, Warnings: make(map[string]bool)}
		result, err := actionpins.ResolveActionPin(known, latestPin.Version, ctx)
		require.NoError(t, err, "embedded-only ResolveActionPin should not error for known pin")
		require.NotEmpty(t, result, "should return non-empty pinned reference for known embedded pin")
		assert.Contains(t, result, latestPin.SHA, "resolved reference should contain the pin SHA")
	})
}

// TestSpec_DynamicResolution_VersionCommentConsistency validates that when dynamic resolution
// succeeds and the returned SHA matches an embedded pin, the version comment includes both
// the resolved version and the source version — consistent with the embedded-fallback path.
func TestSpec_DynamicResolution_VersionCommentConsistency(t *testing.T) {
	t.Parallel()
	known := "actions/checkout"
	latestPin, ok := actionpins.GetLatestActionPinByRepo(known)
	require.True(t, ok, "prerequisite: known repo must be in embedded data")

	t.Run("shows resolved version and source version when SHA matches embedded pin", func(t *testing.T) {
		// Simulate dynamic resolution returning the same SHA as the embedded pin,
		// but requested with a shorter version tag (e.g. "v4" instead of "v4.1.2").
		sourceVersion := "v4"
		ctx := &actionpins.PinContext{
			Resolver: &testSHAResolver{sha: latestPin.SHA},
			Warnings: make(map[string]bool),
		}
		result, err := actionpins.ResolveActionPin(known, sourceVersion, ctx)
		require.NoError(t, err)
		assert.Contains(t, result, latestPin.SHA, "result should contain the resolved SHA")
		assert.Contains(t, result, latestPin.Version, "result should contain the resolved version")
		assert.Contains(t, result, sourceVersion, "result should contain the source version")
	})

	t.Run("shows only source version when SHA is not in embedded pins", func(t *testing.T) {
		unknownSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		sourceVersion := "v4"
		ctx := &actionpins.PinContext{
			Resolver: &testSHAResolver{sha: unknownSHA},
			Warnings: make(map[string]bool),
		}
		result, err := actionpins.ResolveActionPin(known, sourceVersion, ctx)
		require.NoError(t, err)
		assert.Contains(t, result, unknownSHA, "result should contain the resolved SHA")
		assert.Contains(t, result, sourceVersion, "result should contain the source version")
	})

	t.Run("skips version comment when version is already a SHA", func(t *testing.T) {
		ctx := &actionpins.PinContext{Warnings: make(map[string]bool)}
		result, err := actionpins.ResolveActionPin(known, latestPin.SHA, ctx)
		require.NoError(t, err)
		assert.Contains(t, result, latestPin.SHA, "result should contain the SHA")
		// Resolver is not called for SHA inputs; only version comment content matters
	})
}

// TestSpec_PublicAPI_GetContainerPin validates the documented GetContainerPin function.
// Spec: "Returns a pinned container image by its original image reference"
func TestSpec_PublicAPI_GetContainerPin(t *testing.T) {
	t.Parallel()
	t.Run("returns false for unknown container image", func(t *testing.T) {
		_, ok := actionpins.GetContainerPin("does-not-exist/unknown-image:latest")
		assert.False(t, ok, "should return false for unknown container image")
	})

	t.Run("returns pinned container for known image", func(t *testing.T) {
		knownImage := constants.DefaultMCPGatewayContainer + ":" + string(constants.DefaultMCPGatewayVersion)
		pin, ok := actionpins.GetContainerPin(knownImage)
		require.True(t, ok, "should return true for a known container image")
		assert.Equal(t, knownImage, pin.Image, "ContainerPin.Image should match the queried image")
		require.NotEmpty(t, pin.Digest, "ContainerPin.Digest should be non-empty for a known image")
		assert.True(t, strings.HasPrefix(pin.Digest, "sha256:"), "ContainerPin.Digest should use sha256: format, got %q", pin.Digest)
		assert.Equal(t, knownImage+"@"+pin.Digest, pin.PinnedImage, "PinnedImage should equal image@digest")
	})
}

// TestSpec_Constants_ResolutionErrorType validates the documented ResolutionErrorType constant values.
// Spec table: ResolutionErrorTypeDynamicResolutionFailed="dynamic_resolution_failed",
// ResolutionErrorTypePinNotFound="pin_not_found".
func TestSpec_Constants_ResolutionErrorType(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "dynamic_resolution_failed", string(actionpins.ResolutionErrorTypeDynamicResolutionFailed),
		"ResolutionErrorTypeDynamicResolutionFailed should equal the documented value")
	assert.Equal(t, "pin_not_found", string(actionpins.ResolutionErrorTypePinNotFound),
		"ResolutionErrorTypePinNotFound should equal the documented value")
}

// TestSpec_PublicAPI_RecordResolutionFailure validates the documented auditing behavior:
// PinContext.RecordResolutionFailure collects ResolutionFailure events for unresolved pins,
// classified according to whether a resolver was present.
// Spec section "Auditing Resolution Failures".
func TestSpec_PublicAPI_RecordResolutionFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		repo          string
		version       string
		resolver      actionpins.SHAResolver
		wantErrorType actionpins.ResolutionErrorType
	}{
		{
			name:          "no resolver classifies failure as pin_not_found",
			repo:          "does-not-exist/unknown-action-xyzzy",
			version:       "v1",
			wantErrorType: actionpins.ResolutionErrorTypePinNotFound,
		},
		{
			name:          "failing resolver classifies failure as dynamic_resolution_failed",
			repo:          "does-not-exist/x",
			version:       "v1",
			resolver:      &testSHAResolver{err: errors.New("network error")},
			wantErrorType: actionpins.ResolutionErrorTypeDynamicResolutionFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var failures []actionpins.ResolutionFailure
			ctx := &actionpins.PinContext{
				Resolver: tt.resolver,
				Warnings: make(map[string]bool),
				RecordResolutionFailure: func(f actionpins.ResolutionFailure) {
					failures = append(failures, f)
				},
			}

			_, err := actionpins.ResolveActionPin(tt.repo, tt.version, ctx)
			require.NoError(t, err, "ResolveActionPin should not error for unresolved pins in non-enforce mode")

			require.Len(t, failures, 1, "RecordResolutionFailure should be invoked once for an unresolved pin")
			assert.Equal(t, tt.wantErrorType, failures[0].ErrorType,
				"failure should be classified with the expected error type")
			assert.Equal(t, tt.repo, failures[0].Repo, "recorded failure should carry the queried repo")
			assert.Equal(t, tt.version, failures[0].Ref, "recorded failure should carry the queried ref")
		})
	}
}

// TestSpec_ThreadSafety_ConcurrentGetActionPinsByRepo validates that concurrent calls to GetActionPinsByRepo
// are safe after initialization (sync.Once guarantee from the spec).
func TestSpec_ThreadSafety_ConcurrentGetActionPinsByRepo(t *testing.T) {
	t.Parallel()
	const goroutines = 10
	const repo = "actions/checkout"
	results := make([][]actionpins.ActionPin, goroutines)
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = actionpins.GetActionPinsByRepo(repo)
		}(i)
	}

	wg.Wait()

	require.NotEmpty(t, results[0], "baseline goroutine 0 should return pins")
	for i := 1; i < goroutines; i++ {
		assert.NotEmpty(t, results[i], "concurrent GetActionPinsByRepo should return pins for known repo")
		assert.Lenf(t, results[i], len(results[0]),
			"concurrent GetActionPinsByRepo should return same number of pins (goroutine %d vs 0)", i)
	}
}

// TestSpec_PublicAPI_ResolveActionPin_DynamicHappyPath validates that a dynamic resolver that
// successfully returns a SHA produces a correctly formatted pinned reference.
func TestSpec_PublicAPI_ResolveActionPin_DynamicHappyPath(t *testing.T) {
	t.Parallel()
	known := "actions/checkout"
	latestPin, ok := actionpins.GetLatestActionPinByRepo(known)
	require.True(t, ok, "prerequisite: known repo must be in embedded data")

	// Resolver returns the SHA of the latest embedded pin so that the resolved
	// version comment can be verified end-to-end.
	ctx := &actionpins.PinContext{
		Resolver: &testSHAResolver{sha: latestPin.SHA},
		Warnings: make(map[string]bool),
	}
	result, err := actionpins.ResolveActionPin(known, "v4", ctx)
	require.NoError(t, err, "dynamic resolver success should not return an error")
	require.NotEmpty(t, result, "should return a non-empty pinned reference when resolver succeeds")
	assert.Contains(t, result, latestPin.SHA, "result should contain the SHA returned by the resolver")
	assert.True(t, strings.HasPrefix(result, known+"@"),
		"result should start with repo@sha in the documented format")
}

// TestSpec_DynamicResolution_EmptySHAFallsThrough validates that a resolver returning an empty
// SHA with a nil error falls through to the hardcoded pin lookup rather than producing a result.
func TestSpec_DynamicResolution_EmptySHAFallsThrough(t *testing.T) {
	t.Parallel()
	t.Run("empty SHA with nil error falls through to hardcoded pins for known repo", func(t *testing.T) {
		known := "actions/checkout"
		latestPin, ok := actionpins.GetLatestActionPinByRepo(known)
		require.True(t, ok, "prerequisite: known repo must be in embedded data")

		// Resolver returns ("", nil) — empty SHA is treated as a non-result and falls through.
		ctx := &actionpins.PinContext{
			Resolver: &testSHAResolver{sha: "", err: nil},
			Warnings: make(map[string]bool),
		}
		result, err := actionpins.ResolveActionPin(known, latestPin.Version, ctx)
		require.NoError(t, err)
		require.NotEmpty(t, result, "empty-SHA resolver should fall through to hardcoded pins")
		assert.Equal(t,
			actionpins.FormatPinnedActionReference(known, latestPin.SHA, latestPin.Version),
			result,
			"result should match the exact hardcoded pin format")
	})

	t.Run("empty SHA with nil error falls through and produces empty for unknown repo", func(t *testing.T) {
		// Resolver returns ("", nil) — empty SHA is treated as a non-result and falls through.
		// The auditing contract requires a ResolutionFailure to be recorded for the unresolved pin.
		var failures []actionpins.ResolutionFailure
		ctx := &actionpins.PinContext{
			Resolver: &testSHAResolver{sha: "", err: nil},
			Warnings: make(map[string]bool),
			RecordResolutionFailure: func(f actionpins.ResolutionFailure) {
				failures = append(failures, f)
			},
		}
		result, err := actionpins.ResolveActionPin("does-not-exist/x", "v1", ctx)
		require.NoError(t, err)
		assert.Empty(t, result, "empty-SHA resolver on unknown repo should produce empty result")
		require.Len(t, failures, 1, "unresolved pin should be recorded as a resolution failure")
		assert.Equal(t, actionpins.ResolutionErrorTypeDynamicResolutionFailed, failures[0].ErrorType,
			"failure should be classified as dynamic resolution failed")
	})
}

// TestSpec_PublicAPI_ResolveActionPin_NilCtxField validates that a nil PinContext.Ctx
// falls back to context.Background() instead of panicking.
func TestSpec_PublicAPI_ResolveActionPin_NilCtxField(t *testing.T) {
	t.Parallel()
	resolver := &testSHAResolver{sha: testResolvedSHA}
	ctx := &actionpins.PinContext{
		Ctx:      nil, // deliberately nil — should fall back to context.Background()
		Resolver: resolver,
		Warnings: make(map[string]bool),
	}
	require.NotPanics(t, func() {
		result, err := actionpins.ResolveActionPin("actions/checkout", "v4", ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	}, "nil PinContext.Ctx should fall back to context.Background() without panicking")
	require.NotNil(t, resolver.capturedCtx, "resolver must receive a non-nil context even when PinContext.Ctx is nil")
	assert.Equal(t, context.Background(), resolver.capturedCtx, "resolver must receive context.Background() as the documented fallback")
}

// TestSpec_PublicAPI_ResolveActionPin_UsesProvidedContext validates that PinContext.Ctx
// is forwarded to the resolver instead of being replaced with context.Background().
func TestSpec_PublicAPI_ResolveActionPin_UsesProvidedContext(t *testing.T) {
	t.Parallel()
	resolver := &testSHAResolver{sha: testResolvedSHA}
	baseCtx := context.WithValue(context.Background(), testContextPropagationKey, "context-propagation-marker")
	providedCtx, cancel := context.WithCancel(baseCtx)
	// Pre-cancel the context so the resolver can verify that ResolveActionPin forwards the
	// exact caller-provided context object, including its cancellation state, not just values.
	cancel()

	result, err := actionpins.ResolveActionPin("actions/checkout", "v4", &actionpins.PinContext{
		Ctx:      providedCtx,
		Resolver: resolver,
		Warnings: make(map[string]bool),
	})
	require.NoError(t, err)
	assert.Same(t, providedCtx, resolver.capturedCtx, "resolver should receive the exact PinContext.Ctx value")
	assert.Equal(t, context.Canceled, resolver.capturedCtx.Err(), "resolver should observe the canceled context")
	assert.Equal(t, "context-propagation-marker", resolver.capturedCtx.Value(testContextPropagationKey), "resolver should receive context values")
	assert.Equal(t, "actions/checkout", resolver.capturedRepo, "resolver should receive the original repo")
	assert.Equal(t, "v4", resolver.capturedRef, "resolver should receive the original version")
	assert.Contains(t, result, resolver.sha, "successful resolution should use the resolver-provided SHA")
}

// TestSpec_PublicAPI_ResolveLatestActionPin_NonNilContext validates that a non-nil PinContext
// is forwarded correctly to the embedded pin resolution path.
func TestSpec_PublicAPI_ResolveLatestActionPin_NonNilContext(t *testing.T) {
	t.Parallel()
	known := "actions/checkout"
	latestPin, ok := actionpins.GetLatestActionPinByRepo(known)
	require.True(t, ok, "prerequisite: known repo must be in embedded data")

	ctx := &actionpins.PinContext{Warnings: make(map[string]bool)}
	result := actionpins.ResolveLatestActionPin(known, ctx)
	expected := actionpins.FormatPinnedActionReference(known, latestPin.SHA, latestPin.Version)
	assert.Equal(t, expected, result,
		"non-nil ctx without a resolver should resolve the same reference as the nil-ctx path")
}

// TestSpec_PublicAPI_RecordResolutionFailure_WarningDedup validates that repeated resolution
// failures for the same repo@version emit the warning only once (Warnings map deduplication).
func TestSpec_PublicAPI_RecordResolutionFailure_WarningDedup(t *testing.T) {
	t.Parallel()
	var failures []actionpins.ResolutionFailure
	ctx := &actionpins.PinContext{
		Warnings: make(map[string]bool),
		RecordResolutionFailure: func(f actionpins.ResolutionFailure) {
			failures = append(failures, f)
		},
	}

	repo, version := "does-not-exist/x", "v1"
	cacheKey := actionpins.FormatCacheKey(repo, version)

	// First call: failure is recorded and warning key is set.
	_, err := actionpins.ResolveActionPin(repo, version, ctx)
	require.NoError(t, err)
	require.Len(t, failures, 1, "first call should record one resolution failure")
	assert.True(t, ctx.Warnings[cacheKey], "warning key should be set after first call")

	// Second call with the same args: failure is recorded again (auditing is not
	// deduplicated), but the warning is suppressed by the Warnings map.
	_, err = actionpins.ResolveActionPin(repo, version, ctx)
	require.NoError(t, err)
	assert.Len(t, failures, 2, "second call should append another resolution failure")
	assert.True(t, ctx.Warnings[cacheKey], "warning dedup key should remain set after second call")
}

// TestSpec_PublicAPI_ResolveActionPin_AppliesMapping validates that ctx.Mappings redirects
// action resolution to the mapped repository and version.
func TestSpec_PublicAPI_ResolveActionPin_AppliesMapping(t *testing.T) {
	t.Parallel()
	// actions/checkout is in the embedded pins; acme-corp/checkout is not.
	// After mapping, resolution should succeed using the mapped repo's pins.
	checkoutPins := actionpins.GetActionPinsByRepo("actions/checkout")
	require.NotEmpty(t, checkoutPins, "prerequisite: embedded pins for actions/checkout must exist")

	t.Run("mapping redirects to a known pinned repo", func(t *testing.T) {
		ctx := &actionpins.PinContext{
			Warnings: make(map[string]bool),
			Mappings: map[string]string{
				"actions/setup-node@v4": "actions/checkout@v4",
			},
		}
		// Request setup-node@v4 which is mapped to checkout@v4 — both exist in embedded pins.
		result, err := actionpins.ResolveActionPin("actions/setup-node", "v4", ctx)
		require.NoError(t, err)
		assert.Contains(t, result, "actions/checkout@", "result should reference the mapped repo")
	})

	t.Run("mapping notification key is recorded in warnings", func(t *testing.T) {
		ctx := &actionpins.PinContext{
			Warnings: make(map[string]bool),
			Mappings: map[string]string{
				"actions/setup-node@v4": "actions/checkout@v4",
			},
		}
		_, _ = actionpins.ResolveActionPin("actions/setup-node", "v4", ctx)
		assert.True(t, ctx.Warnings["map:actions/setup-node@v4"], "mapping notification key should be set in warnings")
	})

	t.Run("self-mapping preserves the same resolved reference", func(t *testing.T) {
		baseline, err := actionpins.ResolveActionPin("actions/checkout", "v4", &actionpins.PinContext{
			Warnings: make(map[string]bool),
		})
		require.NoError(t, err)

		ctx := &actionpins.PinContext{
			Warnings: make(map[string]bool),
			Mappings: map[string]string{
				"actions/checkout@v4": "actions/checkout@v4",
			},
		}
		result, err := actionpins.ResolveActionPin("actions/checkout", "v4", ctx)
		require.NoError(t, err)
		assert.Equal(t, baseline, result, "self-mapping should behave the same as the unmapped case")
		assert.True(t, ctx.Warnings["map:actions/checkout@v4"], "self-mapping should still record the mapping notification key")
	})

	t.Run("no mapping leaves resolution unchanged", func(t *testing.T) {
		ctx := &actionpins.PinContext{Warnings: make(map[string]bool)}

		result, err := actionpins.ResolveActionPin("actions/checkout", "v4", ctx)
		require.NoError(t, err)
		assert.Contains(t, result, "actions/checkout@", "result should reference the original repo when no mapping exists")
	})

	t.Run("invalid mapping value is skipped", func(t *testing.T) {
		baseline, err := actionpins.ResolveActionPin("actions/checkout", "v4", &actionpins.PinContext{
			Warnings: make(map[string]bool),
		})
		require.NoError(t, err)

		ctx := &actionpins.PinContext{
			Warnings: make(map[string]bool),
			Mappings: map[string]string{
				"actions/checkout@v4": "actions/checkout",
			},
		}
		result, err := actionpins.ResolveActionPin("actions/checkout", "v4", ctx)
		require.NoError(t, err, "invalid mapping should be skipped without error")
		assert.Equal(t, baseline, result, "invalid mapping should leave resolution behavior unchanged")
		assert.NotContains(t, ctx.Warnings, "map:actions/checkout@v4", "invalid mappings should not record mapping notifications")
	})
}

// TestSpec_PublicAPI_ResolveActionPin_MappingTargetUnknown validates that mapping to a repo
// with no known pins yields an empty result without panicking.
func TestSpec_PublicAPI_ResolveActionPin_MappingTargetUnknown(t *testing.T) {
	t.Parallel()
	ctx := &actionpins.PinContext{
		Warnings: make(map[string]bool),
		Mappings: map[string]string{
			"actions/checkout@v4": "does-not-exist/unknown-action-xyzzy@v1",
		},
	}

	require.NotPanics(t, func() {
		result, err := actionpins.ResolveActionPin("actions/checkout", "v4", ctx)
		require.NoError(t, err)
		assert.Empty(t, result, "mapping to unknown repo should produce unresolved empty result")
	})
}

// TestSpec_PublicAPI_ApplyContainerPinMapping validates the exported ApplyContainerPinMapping function.
// Spec: ApplyContainerPinMapping redirects container image references via ctx.ContainerMappings,
// requiring a valid @sha256:<64-hex-char> digest in the mapped value.
func TestSpec_PublicAPI_ApplyContainerPinMapping(t *testing.T) {
	t.Parallel()
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	t.Run("nil ctx - image returned unchanged", func(t *testing.T) {
		result := actionpins.ApplyContainerPinMapping("ghcr.io/owner/image:latest", nil)
		assert.Equal(t, "ghcr.io/owner/image:latest", result, "nil ctx should return image unchanged")
	})

	t.Run("no mapping - image returned unchanged", func(t *testing.T) {
		ctx := &actionpins.PinContext{Warnings: make(map[string]bool)}
		result := actionpins.ApplyContainerPinMapping("ghcr.io/owner/image:latest", ctx)
		assert.Equal(t, "ghcr.io/owner/image:latest", result, "absent mapping should return image unchanged")
	})

	t.Run("valid mapping - image redirected and notification recorded", func(t *testing.T) {
		ctx := &actionpins.PinContext{
			Warnings: make(map[string]bool),
			ContainerMappings: map[string]string{
				"ghcr.io/owner/image:latest": "registry.acme.com/image:latest@sha256:" + digest,
			},
		}
		result := actionpins.ApplyContainerPinMapping("ghcr.io/owner/image:latest", ctx)
		assert.Equal(t, "registry.acme.com/image:latest@sha256:"+digest, result,
			"valid mapping should redirect to the mapped image")
		assert.True(t, ctx.Warnings["container-map:ghcr.io/owner/image:latest"],
			"mapping notification should be recorded in warnings")
	})

	t.Run("invalid mapping - mapped value without digest returns image unchanged", func(t *testing.T) {
		ctx := &actionpins.PinContext{
			Warnings: make(map[string]bool),
			ContainerMappings: map[string]string{
				"ghcr.io/owner/image:latest": "registry.acme.com/image:latest",
			},
		}
		result := actionpins.ApplyContainerPinMapping("ghcr.io/owner/image:latest", ctx)
		assert.Equal(t, "ghcr.io/owner/image:latest", result,
			"mapping without valid @sha256: digest should be rejected and image returned unchanged")
	})

	t.Run("invalid mapping - mapped value with malformed digest returns image unchanged", func(t *testing.T) {
		ctx := &actionpins.PinContext{
			Warnings: make(map[string]bool),
			ContainerMappings: map[string]string{
				"ghcr.io/owner/image:latest": "registry.acme.com/image:latest@sha256:not-a-valid-digest",
			},
		}
		result := actionpins.ApplyContainerPinMapping("ghcr.io/owner/image:latest", ctx)
		assert.Equal(t, "ghcr.io/owner/image:latest", result,
			"mapping with malformed @sha256 digest should be rejected and image returned unchanged")
	})
}
