//go:build !integration

package actionpins

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingResolver records how many times ResolveSHA is called, for use in tests
// that verify whether dynamic resolution is invoked.
type countingResolver struct {
	called int
}

func (r *countingResolver) ResolveSHA(_ context.Context, _, _ string) (string, error) {
	r.called++
	return "", nil
}

type fixedResolver struct {
	sha    string
	called int
}

func TestResolveActionPin_AlreadyPinnedSourceVersion(t *testing.T) {
	const sha = "c19371144df3bb44fab255c43d04cbc2ab54d1c4"
	const repo = "Swatinem/rust-cache"
	for _, tt := range []struct {
		name  string
		label string
		want  string
	}{
		{"unlabelled", "", repo + "@" + sha},
		{"source label", "v2.9.1", repo + "@" + sha + " # v2.9.1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &PinContext{SourceVersions: map[string]string{repo + "@" + sha: tt.label}}
			got, err := ResolveActionPin(repo, sha, ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	pin := GetActionPinsByRepo("actions/checkout")[0]
	got, err := ResolveActionPin(pin.Repo, pin.SHA, &PinContext{
		SourceVersions: map[string]string{pin.Repo + "@" + pin.SHA: "v0.0.0"},
	})
	require.NoError(t, err)
	assert.Equal(t, pin.Repo+"@"+pin.SHA+" # "+pin.Version, got)
}

func (r *fixedResolver) ResolveSHA(_ context.Context, _, _ string) (string, error) {
	r.called++
	return r.sha, nil
}

func TestResolveActionPin_GHESArtifactCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		repo string
		want string
	}{
		{
			repo: "actions/upload-artifact",
			want: "actions/upload-artifact@c6a366c94c3e0affe28c06c8df20a878f24da3cf # v3.2.2",
		},
		{
			repo: "actions/download-artifact",
			want: "actions/download-artifact@a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59 # v3.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			t.Parallel()
			resolver := &countingResolver{}
			got, err := ResolveActionPin(tt.repo, "latest", &PinContext{
				GHES:     true,
				Resolver: resolver,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Zero(t, resolver.called, "GHES compatibility pins should not require dynamic resolution")
		})
	}
}

func TestResolveActionPin_GHESMappingTakesPrecedence(t *testing.T) {
	t.Parallel()

	resolver := &fixedResolver{sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	got, err := ResolveActionPin("actions/upload-artifact", "v7", &PinContext{
		GHES:     true,
		Resolver: resolver,
		Mappings: map[string]string{
			"actions/upload-artifact@v7": "enterprise/upload-artifact@v3",
		},
	})

	require.NoError(t, err)
	assert.Contains(t, got, "enterprise/upload-artifact@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	assert.NotContains(t, got, "c6a366c94c3e0affe28c06c8df20a878f24da3cf")
	assert.Equal(t, 1, resolver.called, "mapped enterprise action should use normal resolution")
}

func TestBuildByRepoIndex_GroupsByRepoAndSortsDescending(t *testing.T) {
	t.Parallel()
	pins := []ActionPin{
		{Repo: "actions/checkout", Version: "v4.0.0", SHA: "sha-v4"},
		{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-v5"},
		{Repo: "actions/setup-go", Version: "v5.1.0", SHA: "sha-go-v5-1"},
		{Repo: "actions/setup-go", Version: "v5.0.0", SHA: "sha-go-v5-0"},
	}

	byRepo := buildByRepoIndex(pins)

	require.Len(t, byRepo["actions/checkout"], 2, "Expected checkout pins to be grouped")
	require.Equal(t, "v5.0.0", byRepo["actions/checkout"][0].Version, "Expected v5.0.0 as newest checkout pin")
	require.Equal(t, "v4.0.0", byRepo["actions/checkout"][1].Version, "Expected v4.0.0 as second checkout pin")

	require.Len(t, byRepo["actions/setup-go"], 2, "Expected setup-go pins to be grouped")
	require.Equal(t, "v5.1.0", byRepo["actions/setup-go"][0].Version, "Expected v5.1.0 as newest setup-go pin")
	require.Equal(t, "v5.0.0", byRepo["actions/setup-go"][1].Version, "Expected v5.0.0 as second setup-go pin")
}

func TestCountPinKeyMismatches_ReturnsOnlyVersionMismatches(t *testing.T) {
	t.Parallel()
	t.Run("returns zero for empty entries", func(t *testing.T) {
		assert.Zero(t, countPinKeyMismatches(map[string]ActionPin{}), "Expected empty input to produce zero mismatches")
	})

	t.Run("returns zero when all key versions match", func(t *testing.T) {
		entries := map[string]ActionPin{
			"actions/checkout@v5": {Repo: "actions/checkout", Version: "v5", SHA: "sha-1"},
			"actions/setup-go@v4": {Repo: "actions/setup-go", Version: "v4", SHA: "sha-2"},
		}
		assert.Zero(t, countPinKeyMismatches(entries), "Expected zero mismatches when key versions match pin versions")
	})

	t.Run("ignores invalid keys without version suffix", func(t *testing.T) {
		entries := map[string]ActionPin{
			"invalid-key": {Repo: "actions/cache", Version: "v4", SHA: "sha-3"},
		}
		assert.Zero(t, countPinKeyMismatches(entries), "Expected invalid keys without @version to be ignored")
	})

	t.Run("counts only true key-version mismatches", func(t *testing.T) {
		entries := map[string]ActionPin{
			"actions/checkout@v5": {Repo: "actions/checkout", Version: "v5", SHA: "sha-1"},
			"actions/setup-go@v5": {Repo: "actions/setup-go", Version: "v4", SHA: "sha-2"},
			"invalid-key":         {Repo: "actions/cache", Version: "v4", SHA: "sha-3"},
		}
		assert.Equal(t, 1, countPinKeyMismatches(entries), "Expected only one key/version mismatch to be counted")
	})
}

func TestCollectEntriesWithEmptySHA_ReturnsOnlyEmptySHAEntries(t *testing.T) {
	t.Parallel()
	t.Run("returns empty slice for empty entries", func(t *testing.T) {
		assert.Empty(t, collectEntriesWithEmptySHA(map[string]ActionPin{}), "Expected empty input to produce empty result")
	})

	t.Run("returns empty slice when all entries have non-empty SHAs", func(t *testing.T) {
		entries := map[string]ActionPin{
			"actions/checkout@v5": {Repo: "actions/checkout", Version: "v5", SHA: "abc123"},
			"actions/setup-go@v4": {Repo: "actions/setup-go", Version: "v4", SHA: "def456"},
		}
		assert.Empty(t, collectEntriesWithEmptySHA(entries), "Expected empty result when all SHAs are populated")
	})

	t.Run("returns key of entry with empty SHA", func(t *testing.T) {
		entries := map[string]ActionPin{
			"actions/checkout@v5":      {Repo: "actions/checkout", Version: "v5", SHA: "abc123"},
			"ruby/setup-ruby@v1.319.0": {Repo: "ruby/setup-ruby", Version: "v1.319.0", SHA: ""},
		}
		assert.Equal(t, []string{"ruby/setup-ruby@v1.319.0"}, collectEntriesWithEmptySHA(entries), "Expected the empty-SHA entry key to be returned")
	})

	t.Run("returns sorted keys of multiple entries with empty SHA", func(t *testing.T) {
		entries := map[string]ActionPin{
			"actions/checkout@v5":      {Repo: "actions/checkout", Version: "v5", SHA: "abc123"},
			"ruby/setup-ruby@v1.319.0": {Repo: "ruby/setup-ruby", Version: "v1.319.0", SHA: ""},
			"owner/repo@v2":            {Repo: "owner/repo", Version: "v2", SHA: ""},
		}
		assert.Equal(t, []string{"owner/repo@v2", "ruby/setup-ruby@v1.319.0"}, collectEntriesWithEmptySHA(entries), "Expected sorted keys of empty-SHA entries")
	})
}

func TestLoadActionPinsData_PanicsWhenEntrySHAIsEmpty(t *testing.T) {
	t.Parallel()
	fixture := []byte(`{
		"entries": {
			"ruby/setup-ruby@v1.319.0": {
				"repo": "ruby/setup-ruby",
				"version": "v1.319.0",
				"sha": ""
			}
		}
	}`)

	assert.PanicsWithValue(t,
		"action_pins.json has 1 entries with empty SHA [ruby/setup-ruby@v1.319.0] — these would produce invalid workflow YAML (e.g. 'owner/repo@ # version'); remove or fix these entries before releasing",
		func() {
			loadActionPinsData(fixture)
		}, "Expected loadActionPinsData to panic with specific message when embedded pin data contains an empty SHA")
}

func TestLoadActionPinsData_LoadsContainerPins(t *testing.T) {
	t.Parallel()
	fixture := []byte(`{
		"entries": {
			"actions/checkout@v5": {
				"repo": "actions/checkout",
				"version": "v5",
				"sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
		},
		"containers": {
			"node:lts-alpine": {
				"image": "node:lts-alpine",
				"digest": "sha256:deadbeef",
				"pinned_image": "node:lts-alpine@sha256:deadbeef"
			}
		}
	}`)

	data := loadActionPinsData(fixture)

	require.Contains(t, data.Containers, "node:lts-alpine", "Expected container pin key to be loaded")
	assert.Equal(t, "node:lts-alpine", data.Containers["node:lts-alpine"].Image)
	assert.Equal(t, "sha256:deadbeef", data.Containers["node:lts-alpine"].Digest)
	assert.Equal(t, "node:lts-alpine@sha256:deadbeef", data.Containers["node:lts-alpine"].PinnedImage)
}

func TestEmbeddedContainerPins_DoNotIncludeVulnerableAstGrepImage(t *testing.T) {
	t.Parallel()

	_, ok := GetContainerPin("mcp/ast-grep:latest")

	assert.False(t, ok, "mcp/ast-grep:latest should not be embedded as a pinned container image")
}

func TestEmbeddedContainerPins_DoNotIncludeVulnerableAWFAPIProxy02744(t *testing.T) {
	t.Parallel()

	_, ok := GetContainerPin("ghcr.io/github/gh-aw-firewall/api-proxy:0.27.44")

	assert.False(t, ok, "ghcr.io/github/gh-aw-firewall/api-proxy:0.27.44 should not be embedded as a pinned container image")
}

func TestEmbeddedContainerPins_DoNotIncludeVulnerableAWFCliProxy02744(t *testing.T) {
	t.Parallel()

	_, ok := GetContainerPin("ghcr.io/github/gh-aw-firewall/cli-proxy:0.27.44")

	assert.False(t, ok, "ghcr.io/github/gh-aw-firewall/cli-proxy:0.27.44 should not be embedded as a pinned container image")
}

func TestFormatPinnedActionReference_PanicsWhenSHAIsEmpty(t *testing.T) {
	t.Parallel()
	assert.PanicsWithValue(t,
		"FormatPinnedActionReference called with empty SHA for repo=ruby/setup-ruby version=v1.319.0 — this would produce invalid workflow YAML",
		func() {
			FormatPinnedActionReference("ruby/setup-ruby", "", "v1.319.0")
		}, "Expected FormatPinnedActionReference to panic with specific message when SHA is empty")
}

func TestPinContextEmitOnce_InitializesAndDeduplicates(t *testing.T) {
	// Not parallel: CaptureStderr swaps the global os.Stderr and is not safe
	// to run concurrently with other tests that write to stderr.

	ctx := &PinContext{}

	stderrOutput := testutil.CaptureStderr(t, func() {
		ctx.emitOnce("new", "first", func(message string) string {
			return "formatted: " + message
		})
		ctx.emitOnce("new", "second", func(message string) string {
			return "formatted: " + message
		})
	})

	assert.Equal(t, map[string]bool{"new": true}, ctx.Warnings)
	assert.Equal(t, "formatted: first\n", stderrOutput)
}

func TestFormatPinnedActionWithResolution_ConsistentVersionComment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		repo            string
		sha             string
		sourceVersion   string
		resolvedVersion string
		expected        string
	}{
		{
			name:            "shows only source version when resolvedVersion is empty",
			repo:            "actions/checkout",
			sha:             "abc123",
			sourceVersion:   "v4",
			resolvedVersion: "",
			expected:        "actions/checkout@abc123 # v4",
		},
		{
			name:            "shows only version when source equals resolved",
			repo:            "actions/checkout",
			sha:             "abc123",
			sourceVersion:   "v4.1.2",
			resolvedVersion: "v4.1.2",
			expected:        "actions/checkout@abc123 # v4.1.2",
		},
		{
			name:            "shows both versions when source differs from resolved",
			repo:            "actions/checkout",
			sha:             "abc123",
			sourceVersion:   "v4",
			resolvedVersion: "v4.1.2",
			expected:        "actions/checkout@abc123 # v4.1.2 (source v4)",
		},
		{
			name:            "shows resolved version without source suffix when sourceVersion is empty",
			repo:            "actions/checkout",
			sha:             "abc123",
			sourceVersion:   "",
			resolvedVersion: "v5.2.0",
			expected:        "actions/checkout@abc123 # v5.2.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatPinnedActionWithResolution(tt.repo, tt.sha, tt.sourceVersion, tt.resolvedVersion)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindCompatiblePin_SemverFallback(t *testing.T) {
	t.Parallel()
	pins := []ActionPin{
		{Repo: "actions/checkout", Version: "v5.2.0", SHA: "sha-v5-2"},
		{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-v5-0"},
		{Repo: "actions/checkout", Version: "v4.9.9", SHA: "sha-v4-9"},
	}

	tests := []struct {
		name          string
		version       string
		wantFound     bool
		wantVersion   string
		availablePins []ActionPin
	}{
		{
			name:          "exact-major",
			version:       "v5",
			wantFound:     true,
			wantVersion:   "v5.2.0",
			availablePins: pins,
		},
		{
			// major-compatible-match: requesting v5.0.0 from a list that contains only v5.0.0 and
			// v4.9.9 returns v5.0.0 — the function uses major-compatible matching, not exact-version
			// matching. The only v5.x present happens to be v5.0.0, so the result looks exact.
			name:        "major-compatible-match",
			version:     "v5.0.0",
			wantFound:   true,
			wantVersion: "v5.0.0",
			availablePins: []ActionPin{
				{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-v5-0"},
				{Repo: "actions/checkout", Version: "v4.9.9", SHA: "sha-v4-9"},
			},
		},
		{
			// first-compatible-not-exact: requesting v5.0.0 from [v5.2.0, v5.0.0] returns v5.2.0
			// because findCompatiblePin returns the first major-compatible match, not the exact one.
			// This is a consequence of the pre-sorted (descending) slice contract; see
			// returns-first-compatible-not-highest for the complementary case with an unsorted list.
			name:          "first-compatible-not-exact",
			version:       "v5.0.0",
			wantFound:     true,
			wantVersion:   "v5.2.0",
			availablePins: pins,
		},
		{
			// returns-first-compatible-not-highest: list order determines the result.
			// With an unsorted list [v5.0.0, v5.2.0], requesting v5 returns v5.0.0 (the first
			// major-compatible entry), not v5.2.0. Callers (e.g. resolveNonStrictHardcodedPin) must
			// supply a pre-sorted (descending) slice via GetActionPinsByRepo to get the highest pin.
			name:        "returns-first-compatible-not-highest",
			version:     "v5",
			wantFound:   true,
			wantVersion: "v5.0.0",
			availablePins: []ActionPin{
				{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-low"},
				{Repo: "actions/checkout", Version: "v5.2.0", SHA: "sha-high"},
			},
		},
		{
			// minor-version-constraint: IsCompatible performs major-only comparison, so
			// requesting v5.1 from [v5.2.0, v5.0.0] returns v5.2.0 (same major, first match).
			name:        "minor-version-constraint",
			version:     "v5.1",
			wantFound:   true,
			wantVersion: "v5.2.0",
			availablePins: []ActionPin{
				{Repo: "actions/checkout", Version: "v5.2.0", SHA: "sha-a"},
				{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-b"},
			},
		},
		{
			name:          "no-match",
			version:       "v6",
			wantFound:     false,
			availablePins: pins,
		},
		{
			name:          "empty-version",
			version:       "",
			wantFound:     false,
			availablePins: pins,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pin, found := findCompatiblePin(tt.availablePins, tt.version)
			require.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, tt.wantVersion, pin.Version)
			} else {
				assert.Equal(t, ActionPin{}, pin, "expected zero-value ActionPin when not found")
			}
		})
	}
}

func TestFindVersionBySHA_ReturnsVersionForKnownSHA(t *testing.T) {
	t.Parallel()
	t.Run("returns version for a known SHA in embedded data", func(t *testing.T) {
		pins := GetActionPinsByRepo("actions/checkout")
		require.NotEmpty(t, pins, "prerequisite: embedded pins must exist for actions/checkout")

		knownPin := pins[0]
		version := findVersionBySHA("actions/checkout", knownPin.SHA)
		assert.Equal(t, knownPin.Version, version, "should return the version for a known SHA")
	})

	tests := []struct {
		name string
		repo string
		sha  string
		want string
	}{
		{
			name: "returns empty string for unknown SHA",
			repo: "actions/checkout",
			sha:  "0000000000000000000000000000000000000000",
			want: "",
		},
		{
			name: "returns empty string for unknown repo",
			repo: "does-not-exist/unknown",
			sha:  "abc123",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := findVersionBySHA(tt.repo, tt.sha)
			assert.Equal(t, tt.want, version, "should return empty string when version is not found")
		})
	}
}

func TestGetActionPinsByRepo_ReturnsCopy(t *testing.T) {
	t.Parallel()

	const repo = "actions-ecosystem/action-add-labels"
	original := GetActionPinsByRepo(repo)
	require.NotEmpty(t, original, "prerequisite: embedded pins must exist for "+repo)

	pins := GetActionPinsByRepo(repo)
	require.NotEmpty(t, pins, "prerequisite: embedded pins must exist for "+repo)

	pinIndex := -1
	var inputKey string
	var input *ActionYAMLInput
	for i := range pins {
		for name, candidate := range pins[i].Inputs {
			if candidate != nil {
				pinIndex = i
				inputKey = name
				input = candidate
				break
			}
		}
		if input != nil {
			break
		}
	}
	require.NotNil(t, input, "prerequisite: embedded pins must include an action input")

	input.Description = "mutated description"
	delete(pins[pinIndex].Inputs, inputKey)
	pins[0] = ActionPin{Repo: "mutated/repo", Version: "mutated", SHA: "mutated"}

	fresh := GetActionPinsByRepo(repo)
	require.NotEmpty(t, fresh, "prerequisite: embedded pins must remain available")
	assert.Equal(t, original[0], fresh[0], "mutating a returned slice must not change the cached pins")
	assert.Equal(t, original[pinIndex], fresh[pinIndex], "mutating nested inputs must not change cached pins")
}

func TestGetLatestActionPinByRepo_ReturnsCopy(t *testing.T) {
	t.Parallel()

	const repo = "actions-ecosystem/action-add-labels"
	original, ok := GetLatestActionPinByRepo(repo)
	require.True(t, ok, "prerequisite: embedded pins must exist for "+repo)

	pin, ok := GetLatestActionPinByRepo(repo)
	require.True(t, ok, "prerequisite: embedded pins must exist for "+repo)
	require.NotEmpty(t, pin.Inputs, "prerequisite: embedded pins must include action inputs")

	var inputKey string
	var input *ActionYAMLInput
	for name, candidate := range pin.Inputs {
		if candidate != nil {
			inputKey = name
			input = candidate
			break
		}
	}
	require.NotNil(t, input, "prerequisite: embedded pins must include a non-nil action input")

	input.Description = "mutated description"
	delete(pin.Inputs, inputKey)

	fresh, ok := GetLatestActionPinByRepo(repo)
	require.True(t, ok, "prerequisite: embedded pins must remain available")
	assert.Equal(t, original, fresh, "mutating a returned pin's inputs must not change the cached pin")
}

func TestGetLatestActionPinReference_ReturnsFormattedReferenceOrEmpty(t *testing.T) {
	t.Parallel()
	t.Run("returns formatted reference for known repo", func(t *testing.T) {
		t.Parallel()
		pins := GetActionPinsByRepo("actions/checkout")
		require.NotEmpty(t, pins, "prerequisite: embedded pins must exist for actions/checkout")

		result := getLatestActionPinReference("actions/checkout")
		assert.Equal(t, FormatPinnedActionReference("actions/checkout", pins[0].SHA, pins[0].Version), result)
	})

	t.Run("returns empty string for unknown repo", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, getLatestActionPinReference("does-not-exist/unknown"))
	})
}

func TestGetContainerPin_ReturnsPinnedImage(t *testing.T) {
	t.Parallel()
	pin, ok := GetContainerPin("node:lts-alpine")
	require.True(t, ok, "Expected embedded container pin for node:lts-alpine")
	assert.Equal(t, "node:lts-alpine", pin.Image, "Expected image name to match key")
	assert.NotEmpty(t, pin.Digest, "Expected digest to be populated")
	assert.Contains(t, pin.PinnedImage, "@sha256:", "Expected pinned image to include digest")
}

func TestGetContainerPin_MCPGatewayVersionsArePinned(t *testing.T) {
	t.Parallel()
	var mcpgImages []string
	for image := range getCachedActionPins().containers {
		if strings.HasPrefix(image, "ghcr.io/github/gh-aw-mcpg:") {
			mcpgImages = append(mcpgImages, image)
		}
	}
	require.NotEmpty(t, mcpgImages, "Expected at least one embedded MCP Gateway container pin")
	slices.Sort(mcpgImages)

	for _, image := range mcpgImages {
		t.Run(image, func(t *testing.T) {
			t.Parallel()
			pin, ok := GetContainerPin(image)
			require.True(t, ok, "Expected embedded container pin for %s", image)
			assert.Equal(t, image, pin.Image, "Expected image name to match key")
			assert.NotEmpty(t, pin.Digest, "Expected digest to be populated for %s", image)
			assert.Equal(t, image+"@"+pin.Digest, pin.PinnedImage, "Expected pinned image to include digest for %s", image)
		})
	}
}

func TestGetContainerPin_DefaultMCPImagesArePinned(t *testing.T) {
	t.Parallel()
	images := []string{
		constants.DefaultMCPGatewayContainer + ":" + string(constants.DefaultMCPGatewayVersion),
		"ghcr.io/github/github-mcp-server:" + string(constants.DefaultGitHubMCPServerVersion),
	}

	for _, image := range images {
		t.Run(image, func(t *testing.T) {
			t.Parallel()
			pin, ok := GetContainerPin(image)
			require.True(t, ok, "Expected embedded container pin for %s", image)
			assert.Equal(t, image, pin.Image, "Expected image name to match key")
			assert.NotEmpty(t, pin.Digest, "Expected digest to be populated for %s", image)
			assert.Equal(t, image+"@"+pin.Digest, pin.PinnedImage, "Expected pinned image to include digest for %s", image)
		})
	}
}

func TestGetCachedActionPins_InitializesCache(t *testing.T) {
	t.Parallel()
	cache := getCachedActionPins()

	require.NotNil(t, cache, "Expected cache accessor to return initialized data")
	assert.NotEmpty(t, cache.pins, "Expected cached action pins")
	assert.NotNil(t, cache.byRepo, "Expected cached action pins by repository")
	assert.NotNil(t, cache.containers, "Expected cached container pins")
	assert.Same(t, cache, getCachedActionPins(), "Expected repeated cache access to return the same cache")
}

func TestResolveActionPinDynamically_SkipsForSHAInput(t *testing.T) {
	t.Parallel()
	resolver := &countingResolver{}
	ctx := &PinContext{Resolver: resolver}

	result, ok := resolveActionPinDynamically(
		"actions/checkout",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		true,
		ctx,
	)

	assert.False(t, ok, "Expected no dynamic resolution for SHA input")
	assert.Empty(t, result, "Expected empty result when dynamic resolution is skipped")
	assert.Zero(t, resolver.called, "Expected resolver not to be called for SHA input")
}

func TestLogDynamicResolutionSkipped_NoResolverBranch(t *testing.T) {
	t.Parallel()
	assert.NotPanics(t, func() {
		logDynamicResolutionSkipped(false, false)
	}, "Expected no-resolver branch to be safe")
}

func TestRecordPinResolutionFailure_NilSafety(t *testing.T) {
	t.Parallel()
	t.Run("nil context is safe", func(t *testing.T) {
		assert.NotPanics(t, func() {
			recordPinResolutionFailure(nil, "actions/checkout", "v4", ResolutionErrorTypePinNotFound)
		})
	})

	t.Run("nil callback is safe", func(t *testing.T) {
		ctx := &PinContext{}
		assert.NotPanics(t, func() {
			recordPinResolutionFailure(ctx, "actions/checkout", "v4", ResolutionErrorTypePinNotFound)
		})
	})

	t.Run("records failure when callback is configured", func(t *testing.T) {
		var got []ResolutionFailure
		ctx := &PinContext{
			RecordResolutionFailure: func(f ResolutionFailure) {
				got = append(got, f)
			},
		}

		recordPinResolutionFailure(ctx, "actions/checkout", "v4", ResolutionErrorTypeDynamicResolutionFailed)

		require.Len(t, got, 1, "Expected one resolution failure record")
		assert.Equal(t, ResolutionFailure{
			Repo:      "actions/checkout",
			Ref:       "v4",
			ErrorType: ResolutionErrorTypeDynamicResolutionFailed,
		}, got[0])
	})
}

func TestResolveActionPinFromHardcodedPins_StrictModeNoFallback(t *testing.T) {
	t.Parallel()
	t.Run("strict mode does not fall back when no exact match", func(t *testing.T) {
		t.Parallel()
		ctx := &PinContext{StrictMode: true, Warnings: make(map[string]bool)}

		result, ok := resolveActionPinFromHardcodedPins("actions/checkout", "v999", false, ctx)

		assert.False(t, ok, "Expected strict mode not to fall back to any other hardcoded version")
		assert.Empty(t, result, "Expected no pinned result in strict mode without exact match")
	})

	t.Run("strict mode resolves exact version match", func(t *testing.T) {
		t.Parallel()
		latestPin, ok := GetLatestActionPinByRepo("actions/checkout")
		require.True(t, ok, "prerequisite: embedded pin must exist for actions/checkout")

		ctx := &PinContext{StrictMode: true, Warnings: make(map[string]bool)}

		result, resolved := resolveActionPinFromHardcodedPins("actions/checkout", latestPin.Version, false, ctx)

		require.True(t, resolved, "Expected exact version match to resolve in strict mode")
		assert.Contains(t, result, latestPin.SHA, "Expected result to include matched SHA")
		assert.Contains(t, result, latestPin.Version, "Expected result to include matched version")
	})
}

// --- resolveExactHardcodedPin ---

func TestResolveExactHardcodedPin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		pins         []ActionPin
		repo         string
		version      string
		isSHA        bool
		wantOK       bool
		wantContains []string
	}{
		{
			name:         "SHA input matches by SHA field",
			pins:         []ActionPin{{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-v5"}},
			repo:         "actions/checkout",
			version:      "sha-v5",
			isSHA:        true,
			wantOK:       true,
			wantContains: []string{"sha-v5"},
		},
		{
			name:         "version input matches by Version field",
			pins:         []ActionPin{{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-v5"}},
			repo:         "actions/checkout",
			version:      "v5.0.0",
			isSHA:        false,
			wantOK:       true,
			wantContains: []string{"sha-v5", "v5.0.0"},
		},
		{
			name:    "no match returns false",
			pins:    []ActionPin{{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-v5"}},
			repo:    "actions/checkout",
			version: "v4.0.0",
			isSHA:   false,
			wantOK:  false,
		},
		{
			// Verify that the version-match loop runs before the SHA-match loop:
			// both pin[0] (Version=="sha-token") and pin[1] (SHA=="sha-token") would
			// satisfy a match, but their outputs differ. With isSHA=true the SHA loop
			// is eligible, yet the version loop must win and return pin[0]'s SHA.
			name: "version-match path takes precedence over SHA path when isAlreadySHA=true",
			pins: []ActionPin{
				{Repo: "actions/checkout", Version: "sha-token", SHA: "real-sha-version-match"},
				{Repo: "actions/checkout", Version: "v4.0.0", SHA: "sha-token"},
			},
			repo:         "actions/checkout",
			version:      "sha-token",
			isSHA:        true,
			wantOK:       true,
			wantContains: []string{"real-sha-version-match"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, ok := resolveExactHardcodedPin(tt.repo, tt.version, tt.isSHA, tt.pins)
			assert.Equal(t, tt.wantOK, ok, "ok should match expected")
			if tt.wantOK {
				for _, want := range tt.wantContains {
					assert.Contains(t, result, want, "result should contain %q", want)
				}
			} else {
				assert.Empty(t, result, "Expected empty result when no exact match is found")
			}
		})
	}
}

// --- resolveNonStrictHardcodedPin ---

func TestResolveNonStrictHardcodedPin_SelectsHighestCompatible(t *testing.T) {
	pins := []ActionPin{
		{Repo: "actions/checkout", Version: "v5.2.0", SHA: "sha-v5-2"},
		{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-v5-0"},
		{Repo: "actions/checkout", Version: "v4.9.9", SHA: "sha-v4-9"},
	}

	t.Run("uses semver-compatible pin when available", func(t *testing.T) {
		ctx := &PinContext{Warnings: make(map[string]bool)}
		var result string

		stderrOutput := testutil.CaptureStderr(t, func() {
			result = resolveNonStrictHardcodedPin("actions/checkout", "v5", pins, ctx)
		})

		assert.Contains(t, result, "sha-v5-2", "Expected highest semver-compatible pin to be selected")
		assert.True(t, ctx.Warnings["actions/checkout@v5"], "Expected warning cache key to be recorded")
		// Assert on the stable unstyled message fragment to avoid ANSI-code fragility.
		assert.Contains(t, stderrOutput, "using hardcoded pin for actions/checkout@v5.2.0", "Expected warning to be emitted for compatible fallback")
	})

	t.Run("deduplicates warning on compatible path", func(t *testing.T) {
		ctx := &PinContext{Warnings: make(map[string]bool)}

		stderrOutput := testutil.CaptureStderr(t, func() {
			resolveNonStrictHardcodedPin("actions/checkout", "v5", pins, ctx)
			resolveNonStrictHardcodedPin("actions/checkout", "v5", pins, ctx)
		})

		assert.Equal(t, 1, strings.Count(stderrOutput, "using hardcoded pin for actions/checkout@v5.2.0"),
			"Expected warning emitted exactly once for repeated compatible resolution")
		require.Len(t, ctx.Warnings, 1, "Expected exactly one warning key after deduplication")
	})
}

func TestResolveNonStrictHardcodedPin_FallsBackToHighestWhenNoCompatible(t *testing.T) {
	pins := []ActionPin{
		{Repo: "actions/checkout", Version: "v5.2.0", SHA: "sha-v5-2"},
		{Repo: "actions/checkout", Version: "v5.0.0", SHA: "sha-v5-0"},
		{Repo: "actions/checkout", Version: "v4.9.9", SHA: "sha-v4-9"},
	}

	ctx := &PinContext{Warnings: make(map[string]bool)}
	var first, second string

	stderrOutput := testutil.CaptureStderr(t, func() {
		first = resolveNonStrictHardcodedPin("actions/checkout", "v9", pins, ctx)
		second = resolveNonStrictHardcodedPin("actions/checkout", "v9", pins, ctx)
	})

	assert.Contains(t, first, "sha-v5-2", "Expected fallback to highest available pin when no compatible version exists")
	assert.Contains(t, second, "sha-v5-2", "Expected consistent fallback result on repeated calls")
	assert.True(t, ctx.Warnings["actions/checkout@v9"], "Expected warning cache key to be recorded")
	assert.Len(t, ctx.Warnings, 1, "Expected warning deduplication for repeated resolution attempts")
	// Assert on the stable unstyled message fragment to avoid ANSI-code fragility.
	assert.Equal(t, 1, strings.Count(stderrOutput, "using hardcoded pin for actions/checkout@v5.2.0"), "Expected warning to be emitted exactly once for repeated fallback resolution")
}

func TestResolveActionPinFromHardcodedPins_SkipHardcodedFallback(t *testing.T) {
	t.Parallel()
	t.Run("returns false immediately when SkipHardcodedFallback is set and version is a tag", func(t *testing.T) {
		t.Parallel()
		ctx := &PinContext{SkipHardcodedFallback: true, Warnings: make(map[string]bool)}

		// actions/checkout has hardcoded pins, but SkipHardcodedFallback should prevent version→SHA lookup
		result, ok := resolveActionPinFromHardcodedPins("actions/checkout", "v4", false, ctx)

		assert.False(t, ok, "Expected SkipHardcodedFallback to prevent version→SHA hardcoded pin lookup")
		assert.Empty(t, result, "Expected no pinned result when SkipHardcodedFallback is set for version tag")
	})

	t.Run("allows SHA→version lookup even when SkipHardcodedFallback is set", func(t *testing.T) {
		t.Parallel()
		// This is the regression test for the non-deterministic pinning bug.
		// When a workflow already pins an action with a SHA (e.g. @9c091bb... # v7.0.0)
		// and SkipHardcodedFallback is true (e.g. because GH_HOST is a non-github.com host),
		// the SHA→version lookup must still succeed to preserve the human-readable version comment.
		// Without the fix, the fallback would emit FormatPinnedActionReference(repo, sha, sha),
		// producing "# 9c091bb..." instead of "# v7.0.0".
		latestPin, ok := GetLatestActionPinByRepo("actions/checkout")
		require.True(t, ok, "expected embedded pin for actions/checkout")

		ctx := &PinContext{SkipHardcodedFallback: true, Warnings: make(map[string]bool)}

		result, found := resolveActionPinFromHardcodedPins("actions/checkout", latestPin.SHA, true, ctx)

		require.True(t, found, "Expected SHA→version lookup to succeed even with SkipHardcodedFallback=true")
		assert.Equal(t, FormatPinnedActionReference("actions/checkout", latestPin.SHA, latestPin.Version), result,
			"Expected version comment to use tag, not SHA")
	})

	t.Run("allows hardcoded pins when SkipHardcodedFallback is not set", func(t *testing.T) {
		t.Parallel()
		ctx := &PinContext{SkipHardcodedFallback: false, Warnings: make(map[string]bool)}

		// actions/checkout has hardcoded pins and should resolve
		result, ok := resolveActionPinFromHardcodedPins("actions/checkout", "v4", false, ctx)

		require.True(t, ok, "Expected hardcoded pins to be consulted when SkipHardcodedFallback is false")
		require.NotEmpty(t, result, "Expected a pinned result when SkipHardcodedFallback is not set")
	})
}

func TestApplyActionPinMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                    string
		actionRepo              string
		version                 string
		mappings                map[string]string
		repeat                  int
		wantRepo                string
		wantVersion             string
		wantMappingNotification bool
		wantMapNotificationKeys int
	}{
		{
			name:                    "no mapping",
			actionRepo:              "actions/checkout",
			version:                 "v4",
			wantRepo:                "actions/checkout",
			wantVersion:             "v4",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
		{
			name:       "applies exact mapping",
			actionRepo: "actions/checkout",
			version:    "v4",
			mappings: map[string]string{
				"actions/checkout@v4": "acme-corp/checkout@v4",
			},
			wantRepo:                "acme-corp/checkout",
			wantVersion:             "v4",
			wantMappingNotification: true,
			wantMapNotificationKeys: 1,
		},
		{
			name:       "does not match different version",
			actionRepo: "actions/checkout",
			version:    "v5",
			mappings: map[string]string{
				"actions/checkout@v4": "acme-corp/checkout@v4",
			},
			wantRepo:                "actions/checkout",
			wantVersion:             "v5",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
		{
			name:       "deduplicates notification for repeated mapping",
			actionRepo: "actions/checkout",
			version:    "v4",
			mappings: map[string]string{
				"actions/checkout@v4": "acme-corp/checkout@v4",
			},
			repeat:                  2,
			wantRepo:                "acme-corp/checkout",
			wantVersion:             "v4",
			wantMappingNotification: true,
			wantMapNotificationKeys: 1,
		},
		{
			name:       "skips invalid mapping value",
			actionRepo: "actions/checkout",
			version:    "v4",
			mappings: map[string]string{
				"actions/checkout@v4": "no-at-sign",
			},
			wantRepo:                "actions/checkout",
			wantVersion:             "v4",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
		{
			name:       "skips mapping target without ref separator",
			actionRepo: "actions/checkout",
			version:    "v4",
			mappings: map[string]string{
				"actions/checkout@v4": "acme-corp/checkout",
			},
			wantRepo:                "actions/checkout",
			wantVersion:             "v4",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := &PinContext{
				Warnings: make(map[string]bool),
				Mappings: tt.mappings,
			}
			repeat := max(tt.repeat, 1)

			var gotRepo, gotVersion string
			for range repeat {
				gotRepo, gotVersion = applyActionPinMapping(tt.actionRepo, tt.version, ctx)
			}

			assert.Equal(t, tt.wantRepo, gotRepo, "repo should match expected mapping outcome")
			assert.Equal(t, tt.wantVersion, gotVersion, "version should match expected mapping outcome")

			notifyKey := "map:" + FormatCacheKey(tt.actionRepo, tt.version)
			assert.Equal(t, tt.wantMappingNotification, ctx.Warnings[notifyKey], "mapping notification flag should match expected state")

			mapNotifications := 0
			for k := range ctx.Warnings {
				if strings.HasPrefix(k, "map:") {
					mapNotifications++
				}
			}
			assert.Equal(t, tt.wantMapNotificationKeys, mapNotifications, "unexpected number of mapping notification keys")
		})
	}
}

// TestApplyContainerPinMapping verifies the redirect, miss, invalid-value, and
// deduplication behaviour of ApplyContainerPinMapping.
func TestApplyContainerPinMapping(t *testing.T) {
	t.Parallel()
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name                    string
		image                   string
		mappings                map[string]string
		repeat                  int    // call count for deduplication tests (0 → 1)
		nilCtx                  bool   // when true, pass a nil *PinContext to trigger nil-safety path
		wantImage               string // expected return value
		wantMappingNotification bool   // should Warnings contain the notify key?
		wantMapNotificationKeys int    // total number of "container-map:" keys in Warnings
	}{
		{
			name:                    "miss - no mapping for image",
			image:                   "ghcr.io/owner/image:latest",
			mappings:                map[string]string{"other.registry.io/image:latest": "registry.acme.com/image:latest@sha256:" + digest},
			wantImage:               "ghcr.io/owner/image:latest",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
		{
			name:                    "hit - mapping applied",
			image:                   "ghcr.io/github/gh-aw-firewall:0.27.22",
			mappings:                map[string]string{"ghcr.io/github/gh-aw-firewall:0.27.22": "registry.acme.com/gh-aw-firewall:0.27.22@sha256:" + digest},
			wantImage:               "registry.acme.com/gh-aw-firewall:0.27.22@sha256:" + digest,
			wantMappingNotification: true,
			wantMapNotificationKeys: 1,
		},
		{
			name:                    "digest-pinned replacement value",
			image:                   "node:lts-alpine",
			mappings:                map[string]string{"node:lts-alpine": "registry.acme.com/node:lts-alpine@sha256:" + digest},
			wantImage:               "registry.acme.com/node:lts-alpine@sha256:" + digest,
			wantMappingNotification: true,
			wantMapNotificationKeys: 1,
		},
		{
			name:                    "empty value - mapping skipped",
			image:                   "ghcr.io/owner/image:v1",
			mappings:                map[string]string{"ghcr.io/owner/image:v1": ""},
			wantImage:               "ghcr.io/owner/image:v1",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
		{
			name:                    "tag-only value - mapping skipped",
			image:                   "ghcr.io/owner/image:v1",
			mappings:                map[string]string{"ghcr.io/owner/image:v1": "registry.acme.com/image:v1"},
			wantImage:               "ghcr.io/owner/image:v1",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
		{
			name:                    "short digest - mapping skipped",
			image:                   "ghcr.io/owner/image:v1",
			mappings:                map[string]string{"ghcr.io/owner/image:v1": "registry.acme.com/image:v1@sha256:abc123"},
			wantImage:               "ghcr.io/owner/image:v1",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
		{
			name:                    "nil mappings - image returned unchanged",
			image:                   "ghcr.io/owner/image:v1",
			mappings:                nil,
			wantImage:               "ghcr.io/owner/image:v1",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
		{
			name:                    "nil context - image returned unchanged",
			image:                   "ghcr.io/owner/image:v1",
			mappings:                nil,
			nilCtx:                  true,
			wantImage:               "ghcr.io/owner/image:v1",
			wantMappingNotification: false,
			wantMapNotificationKeys: 0,
		},
		{
			name:                    "deduplication - notification emitted only once",
			image:                   "ghcr.io/github/gh-aw-firewall:0.27.22",
			mappings:                map[string]string{"ghcr.io/github/gh-aw-firewall:0.27.22": "registry.acme.com/gh-aw-firewall:0.27.22@sha256:" + digest},
			repeat:                  3,
			wantImage:               "registry.acme.com/gh-aw-firewall:0.27.22@sha256:" + digest,
			wantMappingNotification: true,
			wantMapNotificationKeys: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var ctx *PinContext
			if tt.nilCtx {
				ctx = nil
			} else {
				ctx = &PinContext{
					Warnings:          make(map[string]bool),
					ContainerMappings: tt.mappings,
				}
			}

			repeat := max(tt.repeat, 1)
			var got string
			for range repeat {
				got = ApplyContainerPinMapping(tt.image, ctx)
			}

			assert.Equal(t, tt.wantImage, got, "mapped image should match expected")

			if ctx != nil {
				notifyKey := "container-map:" + tt.image
				assert.Equal(t, tt.wantMappingNotification, ctx.Warnings[notifyKey],
					"mapping notification flag in Warnings should match")

				mapNotifications := 0
				for k := range ctx.Warnings {
					if strings.HasPrefix(k, "container-map:") {
						mapNotifications++
					}
				}

				assert.Equal(t, tt.wantMapNotificationKeys, mapNotifications,
					"number of container-map: warning keys should match")
			}
		})
	}
}

func TestApplyContainerPinMapping_DeduplicatesInvalidWarnings(t *testing.T) {
	// Not parallel: CaptureStderr swaps the global os.Stderr and is not safe
	// to run concurrently with other tests that write to stderr.

	ctx := &PinContext{
		ContainerMappings: map[string]string{
			"ghcr.io/owner/image:latest": "registry.acme.com/image:latest",
		},
	}

	stderrOutput := testutil.CaptureStderr(t, func() {
		ApplyContainerPinMapping("ghcr.io/owner/image:latest", ctx)
		ApplyContainerPinMapping("ghcr.io/owner/image:latest", ctx)
	})

	assert.Equal(t, 1, strings.Count(stderrOutput, "invalid replacement value"))
	assert.True(t, ctx.Warnings["container-invalid:ghcr.io/owner/image:latest"])
}
