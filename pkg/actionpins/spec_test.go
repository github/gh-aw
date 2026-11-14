//go:build !integration

// Package actionpins_test contains black-box tests derived from README.md.
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
)

const specResolvedSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type specSHAResolver struct {
	sha     string
	err     error
	ctx     context.Context
	repo    string
	version string
}

var _ actionpins.SHAResolver = (*specSHAResolver)(nil)

func (r *specSHAResolver) ResolveSHA(ctx context.Context, repo, version string) (string, error) {
	r.ctx, r.repo, r.version = ctx, repo, version
	return r.sha, r.err
}

// TestSpec_PublicAPI_FormatPinnedActionReference checks Public API / Functions.
// Malformed and empty arguments have no documented formatting contract.
func TestSpec_PublicAPI_FormatPinnedActionReference(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		repo    string
		version string
	}{
		{repo: "actions/checkout", version: "v5"},
		{repo: "example/action", version: "v1.2.3"},
	} {
		t.Run(tt.repo, func(t *testing.T) {
			t.Parallel()
			got := actionpins.FormatPinnedActionReference(tt.repo, specResolvedSHA, tt.version)
			assert.Equal(t, tt.repo+"@"+specResolvedSHA+" # "+tt.version, got,
				"pinned references should use the documented repo@sha # version format")
		})
	}
}

// TestSpec_PublicAPI_FormatCacheKey checks Public API / Functions.
func TestSpec_PublicAPI_FormatCacheKey(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v5", "v1.2.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "actions/checkout@"+version,
				actionpins.FormatCacheKey("actions/checkout", version),
				"cache keys should use the documented repo@version format")
		})
	}
}

// TestSpec_PublicAPI_ExtractReference checks Public API / Functions using
// complete uses references, whose repository and version are specified.
func TestSpec_PublicAPI_ExtractReference(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		repo    string
		version string
	}{
		{name: "documented tag", repo: "actions/checkout", version: "v5"},
		{name: "pinned SHA", repo: "example/action", version: specResolvedSHA},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			uses := tt.repo + "@" + tt.version
			assert.Equal(t, tt.repo, actionpins.ExtractRepo(uses),
				"ExtractRepo should return the repository from a uses reference")
			assert.Equal(t, tt.version, actionpins.ExtractVersion(uses),
				"ExtractVersion should return the ref from a uses reference")
		})
	}
}

// TestSpec_PublicAPI_EmbeddedPins checks Public API / Functions and the
// documented actions/checkout usage example without pinning a data revision.
func TestSpec_PublicAPI_EmbeddedPins(t *testing.T) {
	t.Parallel()
	const repo = "actions/checkout"
	pins := actionpins.GetActionPinsByRepo(repo)
	require.NotEmpty(t, pins, "the documented example repository should have embedded pins")
	latest, ok := actionpins.GetLatestActionPinByRepo(repo)
	require.True(t, ok, "the documented example repository should have a latest pin")
	assert.Equal(t, pins[0], latest, "the latest pin should lead the version-descending list")
	for _, pin := range pins {
		assert.Equal(t, repo, pin.Repo, "repository lookup should return pins for the requested repository")
		assert.NotEmpty(t, pin.SHA, "embedded pins should identify a commit")
	}
}

// TestSpec_PublicAPI_ResolveActionPin checks embedded-only lookup from Overview
// and the documented safe nil-context behavior using a publicly advertised pin.
// SPEC_MISMATCH: The README's fixed strict actions/checkout@v5 example returned
// an empty reference without an error during enforcement. The example is not
// treated as conformant by the general embedded-pin checks below.
func TestSpec_PublicAPI_ResolveActionPin(t *testing.T) {
	t.Parallel()
	pin, ok := actionpins.GetLatestActionPinByRepo("actions/checkout")
	require.True(t, ok, "the documented repository should advertise an embedded pin")
	for _, tt := range []struct {
		name string
		ctx  *actionpins.PinContext
	}{
		{name: "strict embedded lookup", ctx: &actionpins.PinContext{StrictMode: true}},
		{name: "nil context"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := actionpins.ResolveActionPin(pin.Repo, pin.Version, tt.ctx)
			require.NoError(t, err, "an advertised embedded pin should resolve")
			assert.Equal(t, pin.Repo+"@"+pin.SHA+" # "+pin.Version, got,
				"embedded resolution should retain the advertised SHA and version label")
		})
	}
}

// TestSpec_Types_PinContext checks Public API / PinContext fields: Ctx is
// propagated into dynamic resolution and defaults to context.Background().
func TestSpec_Types_PinContext(t *testing.T) {
	t.Parallel()
	provided, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tt := range []struct {
		name string
		ctx  context.Context
		want context.Context
	}{
		{name: "nil defaults to background", want: context.Background()},
		{name: "caller cancellation is propagated", ctx: provided, want: provided},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolver := &specSHAResolver{sha: specResolvedSHA}
			got, err := actionpins.ResolveActionPin("example/action", "v1", &actionpins.PinContext{
				Ctx: tt.ctx, Resolver: resolver, Warnings: make(map[string]bool),
			})
			require.NoError(t, err, "a successful dynamic resolver should produce a pinned reference")
			assert.Equal(t, tt.want, resolver.ctx, "dynamic resolution should receive the documented context")
			assert.Equal(t, "example/action", resolver.repo, "the resolver should receive the requested repository")
			assert.Equal(t, "v1", resolver.version, "the resolver should receive the requested ref")
			assert.Equal(t, "example/action@"+specResolvedSHA+" # v1", got,
				"dynamic resolution should use the returned SHA")
		})
	}
}

// TestSpec_PublicAPI_ResolveActionPin_EnforcePinned checks Public API /
// PinContext fields: AllowActionRefs downgrades unresolved enforcement failures.
// SPEC_AMBIGUITY: StrictMode's unresolved return value and exact diagnostics
// are not specified; this test asserts only the documented enforcement rule.
func TestSpec_PublicAPI_ResolveActionPin_EnforcePinned(t *testing.T) {
	t.Parallel()
	for _, allow := range []bool{false, true} {
		name := "enforced"
		if allow {
			name = "allowed refs"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := actionpins.ResolveActionPin("example/unlisted-spec-action", "v1", &actionpins.PinContext{
				EnforcePinned: true, AllowActionRefs: allow, Warnings: make(map[string]bool),
			})
			if allow {
				require.NoError(t, err, "AllowActionRefs should downgrade an unresolved pin failure")
			} else {
				require.Error(t, err, "EnforcePinned should reject an unresolved pin")
			}
		})
	}
}

// TestSpec_PublicAPI_ResolveActionPin_SkipHardcodedFallback checks the
// documented version-to-SHA fallback switch after dynamic resolution failure.
func TestSpec_PublicAPI_ResolveActionPin_SkipHardcodedFallback(t *testing.T) {
	t.Parallel()
	pin, ok := actionpins.GetLatestActionPinByRepo("actions/checkout")
	require.True(t, ok, "the documented repository should have an embedded pin")
	for _, skip := range []bool{false, true} {
		name := "fallback enabled"
		if skip {
			name = "fallback skipped"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := actionpins.ResolveActionPin(pin.Repo, pin.Version, &actionpins.PinContext{
				Resolver:              &specSHAResolver{err: errors.New("spec resolver failure")},
				EnforcePinned:         true,
				SkipHardcodedFallback: skip,
				Warnings:              make(map[string]bool),
			})
			if skip {
				require.Error(t, err, "skipping fallback should leave the dynamic failure unresolved")
				return
			}
			require.NoError(t, err, "enabled embedded fallback should satisfy the known ref")
			assert.Equal(t, pin.Repo+"@"+pin.SHA+" # "+pin.Version, got,
				"fallback should use the embedded pin")
		})
	}
}

// TestSpec_PublicAPI_ResolveActionPin_SHALabel checks Public API /
// PinContext fields: skipping fallback preserves SHA-to-version labeling.
func TestSpec_PublicAPI_ResolveActionPin_SHALabel(t *testing.T) {
	t.Parallel()
	pin, ok := actionpins.GetLatestActionPinByRepo("actions/checkout")
	require.True(t, ok, "the documented repository should have an embedded pin")
	got, err := actionpins.ResolveActionPin(pin.Repo, pin.SHA, &actionpins.PinContext{
		SkipHardcodedFallback: true, Warnings: make(map[string]bool),
	})
	require.NoError(t, err, "a known SHA should remain resolvable when version fallback is skipped")
	assert.Equal(t, pin.Repo+"@"+pin.SHA+" # "+pin.Version, got,
		"SHA-to-version labeling should survive SkipHardcodedFallback")
}

// TestSpec_PublicAPI_ResolveLatestActionPin checks Public API / Functions:
// latest resolution prefers the dynamic resolver when supplied.
func TestSpec_PublicAPI_ResolveLatestActionPin(t *testing.T) {
	t.Parallel()
	pin, ok := actionpins.GetLatestActionPinByRepo("actions/checkout")
	require.True(t, ok, "the documented repository should have an embedded pin")
	for _, dynamic := range []bool{false, true} {
		name, sha := "embedded", pin.SHA
		var ctx *actionpins.PinContext
		resolver := &specSHAResolver{sha: specResolvedSHA}
		if dynamic {
			name, sha = "dynamic", specResolvedSHA
			ctx = &actionpins.PinContext{Resolver: resolver, Warnings: make(map[string]bool)}
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := actionpins.ResolveLatestActionPin(pin.Repo, ctx)
			assert.Equal(t, pin.Repo+"@"+sha+" # "+pin.Version, got,
				"latest resolution should use the preferred pin source")
			if dynamic {
				assert.Equal(t, pin.Version, resolver.version, "dynamic resolution should request the latest known version")
			}
		})
	}
}

// TestSpec_Constants_ResolutionErrorType checks Auditing Resolution Failures.
func TestSpec_Constants_ResolutionErrorType(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "dynamic_resolution_failed", string(actionpins.ResolutionErrorTypeDynamicResolutionFailed),
		"dynamic failures should retain their documented audit value")
	assert.Equal(t, "pin_not_found", string(actionpins.ResolutionErrorTypePinNotFound),
		"missing pins should retain their documented audit value")
}

// TestSpec_PublicAPI_RecordResolutionFailure checks the documented auditing
// example and classification of a failed dynamic lookup.
func TestSpec_PublicAPI_RecordResolutionFailure(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		resolver actionpins.SHAResolver
		want     actionpins.ResolutionErrorType
	}{
		{name: "missing embedded pin", want: actionpins.ResolutionErrorTypePinNotFound},
		{name: "failed dynamic lookup", resolver: &specSHAResolver{err: errors.New("spec resolver failure")},
			want: actionpins.ResolutionErrorTypeDynamicResolutionFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var failures []actionpins.ResolutionFailure
			_, _ = actionpins.ResolveActionPin("unknown/action", "v1", &actionpins.PinContext{
				Resolver: tt.resolver, Warnings: make(map[string]bool),
				RecordResolutionFailure: func(f actionpins.ResolutionFailure) {
					failures = append(failures, f)
				},
			})
			require.NotEmpty(t, failures, "unresolved pinning should be recorded for auditing")
			assert.Contains(t, failures, actionpins.ResolutionFailure{
				Repo: "unknown/action", Ref: "v1", ErrorType: tt.want,
			}, "the audit event should identify the unresolved reference and cause")
		})
	}
}

// TestSpec_PublicAPI_ResolveActionPin_Mappings checks Action Pin Mappings:
// exact repo@ref matches redirect both repository and ref before resolution.
func TestSpec_PublicAPI_ResolveActionPin_Mappings(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		version string
		repo    string
		ref     string
	}{
		{name: "matching ref redirects", version: "v4", repo: "acme-corp/checkout", ref: "v2"},
		{name: "different ref is unaffected", version: "v5", repo: "actions/checkout", ref: "v5"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolver := &specSHAResolver{sha: specResolvedSHA}
			ctx := &actionpins.PinContext{
				Resolver: resolver, Warnings: make(map[string]bool),
				Mappings: map[string]string{"actions/checkout@v4": "acme-corp/checkout@v2"},
			}
			got, err := actionpins.ResolveActionPin("actions/checkout", tt.version, ctx)
			require.NoError(t, err, "a mapped dynamic lookup should resolve successfully")
			assert.Equal(t, tt.repo, resolver.repo, "only an exact mapping should redirect the repository")
			assert.Equal(t, tt.ref, resolver.version, "the resolver should receive the replacement ref")
			assert.Equal(t, tt.repo+"@"+specResolvedSHA+" # "+tt.ref, got,
				"the result should resolve against the selected repository and ref")
		})
	}
}

// TestSpec_PublicAPI_ApplyResolvedActionPinPrefix checks Public API /
// Functions: remapping an already-resolved reference retains its SHA and label.
// SPEC_AMBIGUITY: The README does not list PrefixMappings in its PinContext
// field table or specify precedence for overlapping repository mappings.
func TestSpec_PublicAPI_ApplyResolvedActionPinPrefix(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v4", "v5"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			got := actionpins.ApplyResolvedActionPinPrefix("actions/checkout",
				"actions/checkout@"+specResolvedSHA+" # "+version, &actionpins.PinContext{
					Warnings:       make(map[string]bool),
					PrefixMappings: map[string]string{"actions/checkout": "acme-corp/checkout"},
				})
			assert.Equal(t, "acme-corp/checkout@"+specResolvedSHA+" # "+version, got,
				"repository remapping should retain the resolved SHA and version label")
		})
	}
}

// TestSpec_PublicAPI_ApplyContainerPinMapping checks Container Pins:
// replacements require a valid SHA-256 digest at the end of the reference.
func TestSpec_PublicAPI_ApplyContainerPinMapping(t *testing.T) {
	t.Parallel()
	const original = "ghcr.io/example/image:v1"
	const pinned = "registry.example/image@sha256:" +
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, tt := range []struct {
		name        string
		replacement string
		want        string
	}{
		{name: "valid digest", replacement: pinned, want: pinned},
		{name: "missing digest", replacement: "registry.example/image:v1", want: original},
		{name: "short digest", replacement: "registry.example/image@sha256:abc123", want: original},
		{name: "nonhex digest", replacement: "registry.example/image@sha256:" + strings.Repeat("z", 64), want: original},
		{name: "trailing text", replacement: pinned + "-extra", want: original},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := actionpins.ApplyContainerPinMapping(original, &actionpins.PinContext{
				Warnings:          make(map[string]bool),
				ContainerMappings: map[string]string{original: tt.replacement},
			})
			assert.Equal(t, tt.want, got, "only a digest-pinned replacement should redirect the image")
		})
	}
}

// TestSpec_ThreadSafety_EmbeddedReads checks Thread Safety: embedded lookup
// results are consistent across concurrent readers without sharing PinContext.
func TestSpec_ThreadSafety_EmbeddedReads(t *testing.T) {
	t.Parallel()
	const readers = 10
	const repo = "actions/checkout"
	pins := make([][]actionpins.ActionPin, readers)
	latest := make([]actionpins.ActionPin, readers)
	found := make([]bool, readers)
	var wg sync.WaitGroup
	for i := range readers {
		wg.Go(func() {
			pins[i] = actionpins.GetActionPinsByRepo(repo)
			latest[i], found[i] = actionpins.GetLatestActionPinByRepo(repo)
		})
	}
	wg.Wait()
	require.NotEmpty(t, pins[0], "the documented repository should have embedded pins")
	for i := range readers {
		assert.True(t, found[i], "each concurrent reader should find the latest pin")
		assert.Equal(t, pins[0], pins[i], "concurrent readers should observe the same embedded pins")
		assert.Equal(t, pins[0][0], latest[i], "concurrent latest lookups should agree with the pin list")
	}
}

// SPEC_AMBIGUITY: GetContainerPin and ResolveGHESActionPin do not document
// concrete bundled images or repositories requiring GHES-compatible pins.
// Avoid deriving those fixture guarantees from implementation data.
