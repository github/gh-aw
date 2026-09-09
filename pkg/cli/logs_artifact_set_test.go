//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateArtifactSets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		sets      []string
		expectErr bool
	}{
		{
			name:      "empty sets is valid",
			sets:      nil,
			expectErr: false,
		},
		{
			name:      "all is valid",
			sets:      []string{"all"},
			expectErr: false,
		},
		{
			name:      "activation is valid",
			sets:      []string{"activation"},
			expectErr: false,
		},
		{
			name:      "agent is valid",
			sets:      []string{"agent"},
			expectErr: false,
		},
		{
			name:      "mcp is valid",
			sets:      []string{"mcp"},
			expectErr: false,
		},
		{
			name:      "firewall is valid",
			sets:      []string{"firewall"},
			expectErr: false,
		},
		{
			name:      "detection is valid",
			sets:      []string{"detection"},
			expectErr: false,
		},
		{
			name:      "github-api is valid",
			sets:      []string{"github-api"},
			expectErr: false,
		},
		{
			name:      "usage is valid",
			sets:      []string{"usage"},
			expectErr: false,
		},
		{
			name:      "multiple valid sets",
			sets:      []string{"agent", "mcp"},
			expectErr: false,
		},
		{
			name:      "unknown set returns error",
			sets:      []string{"unknown"},
			expectErr: true,
		},
		{
			name:      "mix of valid and unknown returns error",
			sets:      []string{"agent", "bad-set"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateArtifactSets(tt.sets)
			if tt.expectErr {
				assert.Error(t, err, "Expected an error for sets: %v", tt.sets)
			} else {
				require.NoError(t, err, "Expected no error for sets: %v", tt.sets)
			}
		})
	}
}

func TestResolveArtifactFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		sets     []string
		expected []string // nil means "no filter" (download all)
	}{
		{
			name:     "nil sets returns nil filter",
			sets:     nil,
			expected: nil,
		},
		{
			name:     "empty sets returns nil filter",
			sets:     []string{},
			expected: nil,
		},
		{
			name:     "all returns nil filter",
			sets:     []string{"all"},
			expected: nil,
		},
		{
			name:     "all with other sets returns nil filter",
			sets:     []string{"agent", "all"},
			expected: nil,
		},
		{
			name:     "activation resolves to activation artifact",
			sets:     []string{"activation"},
			expected: []string{"activation"},
		},
		{
			name:     "agent resolves to agent artifact and output fallback",
			sets:     []string{"agent"},
			expected: []string{constants.AgentArtifactName.String(), constants.AgentOutputFallbackArtifactName.String()},
		},
		{
			name:     "mcp resolves to agent artifact",
			sets:     []string{"mcp"},
			expected: []string{"agent"},
		},
		{
			name:     "firewall resolves to agent artifact",
			sets:     []string{"firewall"},
			expected: []string{"agent"},
		},
		{
			name:     "mcp and firewall both deduplicate to single agent",
			sets:     []string{"mcp", "firewall"},
			expected: []string{"agent"},
		},
		{
			name:     "detection resolves to detection artifact",
			sets:     []string{"detection"},
			expected: []string{"detection"},
		},
		{
			name:     "github-api resolves to activation and agent",
			sets:     []string{"github-api"},
			expected: []string{"activation", "agent"},
		},
		{
			name:     "usage resolves to usage artifact",
			sets:     []string{"usage"},
			expected: []string{"usage"},
		},
		{
			name:     "evals resolves to usage artifact (evals now included in usage)",
			sets:     []string{"evals"},
			expected: []string{"usage"},
		},
		{
			name:     "graders resolves to usage agent and output fallback",
			sets:     []string{"graders"},
			expected: []string{constants.UsageArtifactName.String(), constants.AgentArtifactName.String(), constants.AgentOutputFallbackArtifactName.String()},
		},
		{
			name:     "multiple sets are merged and deduplicated",
			sets:     []string{"activation", "agent"},
			expected: []string{constants.ActivationArtifactName.String(), constants.AgentArtifactName.String(), constants.AgentOutputFallbackArtifactName.String()},
		},
		{
			name:     "github-api and agent deduplicates agent",
			sets:     []string{"github-api", "agent"},
			expected: []string{constants.ActivationArtifactName.String(), constants.AgentArtifactName.String(), constants.AgentOutputFallbackArtifactName.String()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ResolveArtifactFilter(tt.sets)
			assert.Equal(t, tt.expected, result, "ResolveArtifactFilter(%v)", tt.sets)
		})
	}
}

func TestArtifactMatchesFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		artifact string
		filter   []string
		expected bool
	}{
		{
			name:     "nil filter matches everything",
			artifact: "agent",
			filter:   nil,
			expected: true,
		},
		{
			name:     "empty filter matches everything",
			artifact: "agent",
			filter:   []string{},
			expected: true,
		},
		{
			name:     "exact match",
			artifact: "agent",
			filter:   []string{"agent"},
			expected: true,
		},
		{
			name:     "no match",
			artifact: "detection",
			filter:   []string{"agent"},
			expected: false,
		},
		{
			name:     "prefixed match (workflow_call context)",
			artifact: "abc123-agent",
			filter:   []string{"agent"},
			expected: true,
		},
		{
			name:     "prefixed activation match",
			artifact: "deadbeef-activation",
			filter:   []string{"activation"},
			expected: true,
		},
		{
			name:     "prefix does not false-positive on partial names",
			artifact: "sub-agent-tools",
			filter:   []string{"agent"},
			expected: false,
		},
		{
			name:     "multi-filter any match succeeds",
			artifact: "firewall-audit-logs",
			filter:   []string{"agent", "firewall-audit-logs"},
			expected: true,
		},
		{
			name:     "firewall-audit-logs exact match",
			artifact: "firewall-audit-logs",
			filter:   []string{"firewall-audit-logs"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := artifactMatchesFilter(tt.artifact, tt.filter)
			assert.Equal(t, tt.expected, result, "artifactMatchesFilter(%q, %v)", tt.artifact, tt.filter)
		})
	}
}

func TestValidArtifactSetNames(t *testing.T) {
	t.Parallel()
	names := ValidArtifactSetNames()
	require.NotEmpty(t, names, "ValidArtifactSetNames should return non-empty slice")

	expected := []string{"all", "activation", "agent", "detection", "evals", "experiment", "firewall", "github-api", "graders", "mcp", "usage"}
	assert.ElementsMatch(t, expected, names, "ValidArtifactSetNames should contain all known sets")
}

func TestApplyEvalsArtifact(t *testing.T) {
	t.Parallel()
	t.Run("returns empty slice unchanged when artifact list is empty", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, applyEvalsArtifact(nil, true))
		assert.Empty(t, applyEvalsArtifact([]string{}, true))
	})

	t.Run("appends evals when evals requested and artifact list narrowed", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"agent", "evals"}, applyEvalsArtifact([]string{"agent"}, true))
	})

	t.Run("does not append evals when usage already present", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"usage"}, applyEvalsArtifact([]string{"usage"}, true))
	})
}

func TestApplyGradersArtifact(t *testing.T) {
	t.Parallel()
	t.Run("returns empty slice unchanged when artifact list is empty", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, applyGradersArtifact(nil, true))
		assert.Empty(t, applyGradersArtifact([]string{}, true))
	})

	t.Run("appends graders when graders requested and artifact list narrowed", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"usage", "graders"}, applyGradersArtifact([]string{"usage"}, true))
	})

	t.Run("does not append graders when already present", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"graders"}, applyGradersArtifact([]string{"graders"}, true))
	})
}

func TestIsEvalsArtifactRequested(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		evalsOnly    bool
		artifactSets []string
		expected     bool
	}{
		{
			name:         "true when --evals is set",
			evalsOnly:    true,
			artifactSets: nil,
			expected:     true,
		},
		{
			name:         "true when explicit evals artifact set is requested",
			evalsOnly:    false,
			artifactSets: []string{"evals"},
			expected:     true,
		},
		{
			name:         "false when evals are not requested",
			evalsOnly:    false,
			artifactSets: []string{"usage"},
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isEvalsArtifactRequested(tt.evalsOnly, tt.artifactSets))
		})
	}
}

func TestIsUsageOnlyArtifactFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		filter   []string
		expected bool
	}{
		{
			name:     "usage only",
			filter:   []string{"usage"},
			expected: true,
		},
		{
			name:     "usage plus another artifact",
			filter:   []string{"usage", "agent"},
			expected: false,
		},
		{
			name:     "non-usage only",
			filter:   []string{"agent"},
			expected: false,
		},
		{
			name:     "empty filter",
			filter:   nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isUsageOnlyArtifactFilter(tt.filter))
		})
	}
}

func TestShouldDownloadWorkflowRunLogs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		filter   []string
		expected bool
	}{
		{name: "all artifacts", filter: nil, expected: true},
		{name: "usage only", filter: []string{"usage"}, expected: false},
		{name: "activation and usage", filter: []string{"activation", "usage"}, expected: false},
		{name: "agent", filter: []string{"agent"}, expected: true},
		{name: "agent and usage", filter: []string{"agent", "usage"}, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, shouldDownloadWorkflowRunLogs(tt.filter))
		})
	}
}

func TestFindMissingFilterEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		filter       []string
		existingDirs []string
		expected     []string
	}{
		{
			name:         "all entries present (exact match)",
			filter:       []string{"agent", "activation"},
			existingDirs: []string{"agent", "activation"},
			expected:     nil,
		},
		{
			name:         "all entries present (prefix match)",
			filter:       []string{"agent"},
			existingDirs: []string{"abc123-agent", "activation"},
			expected:     nil,
		},
		{
			name:         "one entry missing",
			filter:       []string{"agent", "firewall-audit-logs"},
			existingDirs: []string{"agent"},
			expected:     []string{"firewall-audit-logs"},
		},
		{
			name:         "all entries missing",
			filter:       []string{"agent", "firewall-audit-logs"},
			existingDirs: []string{},
			expected:     []string{"agent", "firewall-audit-logs"},
		},
		{
			name:         "prefix match does not false-positive on substring (suffix mismatch)",
			filter:       []string{"agent"},
			existingDirs: []string{"agent-output"},
			expected:     []string{"agent"},
		},
		{
			name:         "agent fallback satisfies agent transport filter",
			filter:       []string{"agent", "agent-output-fallback"},
			existingDirs: []string{"agent-output-fallback"},
			expected:     nil,
		},
		{
			name:         "prefixed agent fallback satisfies agent transport filter",
			filter:       []string{"agent", "agent-output-fallback"},
			existingDirs: []string{"abc123-agent-output-fallback"},
			expected:     nil,
		},
		{
			name:         "agent satisfies fallback transport filter",
			filter:       []string{"agent", "agent-output-fallback"},
			existingDirs: []string{"agent"},
			expected:     nil,
		},
		{
			name:         "any-suffix directory matches filter entry (mirrors artifactMatchesFilter behavior)",
			filter:       []string{"agent"},
			existingDirs: []string{"super-agent"},
			// strings.HasSuffix("super-agent", "-agent") is true; intentional (consistent
			// with artifactMatchesFilter) — in practice only workflow_call hash-prefixed
			// directories appear in a run folder.
			expected: nil,
		},
		{
			name:         "firewall-audit-logs exact match found",
			filter:       []string{"firewall-audit-logs"},
			existingDirs: []string{"firewall-audit-logs"},
			expected:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, d := range tt.existingDirs {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, d), 0755), "failed to create test dir")
			}
			result := findMissingFilterEntries(tt.filter, dir)
			assert.Equal(t, tt.expected, result, "findMissingFilterEntries(%v, dir)", tt.filter)
		})
	}
}

func TestFindMissingFilterEntriesUsesDownloadedMarkers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, markArtifactDownloaded(dir, "activation"))
	require.NoError(t, markArtifactDownloaded(dir, "abc123-usage"))

	assert.Nil(t, findMissingFilterEntries([]string{"activation", "usage"}, dir))
	assert.Equal(t, []string{"agent"}, findMissingFilterEntries([]string{"activation", "agent"}, dir))
}

func TestFindMissingFilterEntriesAllMarkerSatisfiesFiltered(t *testing.T) {
	t.Parallel()
	// A complete-download marker (ArtifactSetAll) should satisfy every filtered
	// request even when individual artifact directories no longer exist (e.g. after
	// flattenSingleFileArtifacts removes them).
	dir := t.TempDir()
	require.NoError(t, markArtifactDownloaded(dir, string(ArtifactSetAll)))

	assert.Nil(t, findMissingFilterEntries([]string{"activation"}, dir))
	assert.Nil(t, findMissingFilterEntries([]string{"activation", "usage"}, dir))
	assert.Nil(t, findMissingFilterEntries([]string{string(ArtifactSetAll)}, dir))
}

func TestMarkArtifactDownloadedRejectsInvalidNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../activation", `..\activation`, ".", ".."} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := markArtifactDownloaded(t.TempDir(), name)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid artifact name")
		})
	}
}

// TestFindMissingFilterEntriesIncrementalScenario validates the key scenario used by
// the incremental unfiltered download: a previous filtered pass wrote per-artifact
// markers with the full API artifact name (e.g. "abc123-activation"), and the
// subsequent unfiltered pass supplies the same full names to findMissingFilterEntries
// to determine which are still missing.
func TestFindMissingFilterEntriesIncrementalScenario(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Simulate a previous filtered download that wrote markers with full API artifact names.
	require.NoError(t, markArtifactDownloaded(dir, "abc123-activation"))
	require.NoError(t, markArtifactDownloaded(dir, "abc123-usage"))

	// The unfiltered incremental check passes full API names as the filter.
	// activation and usage are found via their exact-match markers; agent is missing.
	result := findMissingFilterEntries([]string{"abc123-activation", "abc123-usage", "abc123-agent"}, dir)
	assert.Equal(t, []string{"abc123-agent"}, result)

	// After downloading agent (marker written), nothing is missing.
	require.NoError(t, markArtifactDownloaded(dir, "abc123-agent"))
	assert.Nil(t, findMissingFilterEntries([]string{"abc123-activation", "abc123-usage", "abc123-agent"}, dir))
}

// TestFindMissingFilterEntriesAllMarkerSatisfiesFullNames verifies that the
// complete-download marker satisfies a filter containing full API artifact names
// (as used by the incremental unfiltered download check).
func TestFindMissingFilterEntriesAllMarkerSatisfiesFullNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, markArtifactDownloaded(dir, string(ArtifactSetAll)))

	// Full API artifact names — all satisfied by the 'all' marker.
	assert.Nil(t, findMissingFilterEntries([]string{"abc123-activation", "abc123-usage", "abc123-agent"}, dir))
	assert.Nil(t, findMissingFilterEntries([]string{"activation", "usage", "agent"}, dir))
}
