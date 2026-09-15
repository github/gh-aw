//go:build !integration

package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeFirewallDiff_NewDomains(t *testing.T) {
	t.Parallel()
	run1 := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{TotalRequests: 5, AllowedRequests: 5},
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443": {Allowed: 5, Blocked: 0},
		},
	}
	run2 := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{TotalRequests: 20, AllowedRequests: 17, BlockedRequests: 3},
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":        {Allowed: 5, Blocked: 0},
			"registry.npmjs.org:443":    {Allowed: 15, Blocked: 0},
			"telemetry.example.com:443": {Allowed: 0, Blocked: 2},
		},
	}

	diff := computeFirewallDiff(100, 200, run1, run2)

	assert.Equal(t, int64(100), diff.Run1ID, "Run1ID should match")
	assert.Equal(t, int64(200), diff.Run2ID, "Run2ID should match")
	assert.Len(t, diff.NewDomains, 2, "Should have 2 new domains")
	assert.Empty(t, diff.RemovedDomains, "Should have no removed domains")
	assert.Empty(t, diff.StatusChanges, "Should have no status changes")

	// Check new domains are sorted
	assert.Equal(t, "registry.npmjs.org:443", diff.NewDomains[0].Domain, "First new domain should be registry.npmjs.org")
	assert.Equal(t, "new", diff.NewDomains[0].Status, "Status should be 'new'")
	assert.Equal(t, "allowed", diff.NewDomains[0].Run2Status, "Registry should be allowed")
	assert.False(t, diff.NewDomains[0].IsAnomaly, "Allowed new domain should not be anomaly")

	assert.Equal(t, "telemetry.example.com:443", diff.NewDomains[1].Domain, "Second new domain should be telemetry.example.com")
	assert.Equal(t, "denied", diff.NewDomains[1].Run2Status, "Telemetry should be denied")
	assert.True(t, diff.NewDomains[1].IsAnomaly, "New denied domain should be anomaly")
	assert.Equal(t, "new denied domain", diff.NewDomains[1].AnomalyNote, "Anomaly note should explain the issue")

	// Check summary
	assert.Equal(t, 2, diff.Summary.NewDomainCount, "Summary should show 2 new domains")
	assert.True(t, diff.Summary.HasAnomalies, "Should have anomalies")
	assert.Equal(t, 1, diff.Summary.AnomalyCount, "Should have 1 anomaly")
}

func TestComputeFirewallDiff_RemovedDomains(t *testing.T) {
	t.Parallel()
	run1 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":       {Allowed: 5, Blocked: 0},
			"old-api.internal.com:443": {Allowed: 8, Blocked: 0},
		},
	}
	run2 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443": {Allowed: 5, Blocked: 0},
		},
	}

	diff := computeFirewallDiff(100, 200, run1, run2)

	assert.Len(t, diff.RemovedDomains, 1, "Should have 1 removed domain")
	assert.Equal(t, "old-api.internal.com:443", diff.RemovedDomains[0].Domain, "Removed domain should be old-api.internal.com")
	assert.Equal(t, "removed", diff.RemovedDomains[0].Status, "Status should be 'removed'")
	assert.Equal(t, "allowed", diff.RemovedDomains[0].Run1Status, "Domain was allowed in run 1")
	assert.Equal(t, 8, diff.RemovedDomains[0].Run1Allowed, "Domain had 8 allowed requests")
	assert.Equal(t, 1, diff.Summary.RemovedDomainCount, "Summary should show 1 removed domain")
	// An allowed-only removed domain must NOT be an anomaly.
	assert.False(t, diff.RemovedDomains[0].IsAnomaly, "Allowed removed domain should not be an anomaly")
	assert.False(t, diff.Summary.HasAnomalies, "No anomalies expected for an allowed removed domain")
}

// TestComputeFirewallDiff_RemovedDeniedDomain verifies that a domain which was denied in
// the base run but is absent from the comparison run is flagged as an anomaly.  This
// covers the false-red scenario where awmg-mcpg:8080 is blocked in the failed run but
// is simply absent (no traffic) in the green run — the block should still be surfaced.
func TestComputeFirewallDiff_RemovedDeniedDomain(t *testing.T) {
	t.Parallel()
	run1 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443": {Allowed: 10, Blocked: 0},
			"awmg-mcpg:8080":     {Allowed: 0, Blocked: 1},
		},
	}
	run2 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443": {Allowed: 10, Blocked: 0},
		},
	}

	diff := computeFirewallDiff(100, 200, run1, run2)

	assert.Len(t, diff.RemovedDomains, 1, "Should have 1 removed domain")
	entry := diff.RemovedDomains[0]
	assert.Equal(t, "awmg-mcpg:8080", entry.Domain)
	assert.Equal(t, "removed", entry.Status)
	assert.Equal(t, "denied", entry.Run1Status, "Domain was denied in run 1")
	assert.Equal(t, 1, entry.Run1Blocked, "Domain had 1 blocked request")
	assert.True(t, entry.IsAnomaly, "Denied removed domain should be an anomaly")
	assert.NotEmpty(t, entry.AnomalyNote, "Anomaly note should be set")
	assert.True(t, diff.Summary.HasAnomalies, "Summary should report anomalies")
	assert.Equal(t, 1, diff.Summary.AnomalyCount, "Should have 1 anomaly")
}

func TestComputeFirewallDiff_StatusChanges(t *testing.T) {
	t.Parallel()
	run1 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"staging.api.com:443":    {Allowed: 10, Blocked: 0},
			"legacy.service.com:443": {Allowed: 0, Blocked: 5},
		},
	}
	run2 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"staging.api.com:443":    {Allowed: 0, Blocked: 3},
			"legacy.service.com:443": {Allowed: 7, Blocked: 0},
		},
	}

	diff := computeFirewallDiff(100, 200, run1, run2)

	assert.Len(t, diff.StatusChanges, 2, "Should have 2 status changes")

	// legacy.service.com: denied → allowed (anomaly: previously denied, now allowed)
	legacyEntry := findDiffEntry(diff.StatusChanges, "legacy.service.com:443")
	require.NotNil(t, legacyEntry, "Should find legacy.service.com in status changes")
	assert.Equal(t, "denied", legacyEntry.Run1Status, "Was denied in run 1")
	assert.Equal(t, "allowed", legacyEntry.Run2Status, "Now allowed in run 2")
	assert.True(t, legacyEntry.IsAnomaly, "Should be flagged as anomaly")
	assert.Equal(t, "previously denied, now allowed", legacyEntry.AnomalyNote, "Anomaly note should explain the flip")

	// staging.api.com: allowed → denied (anomaly)
	stagingEntry := findDiffEntry(diff.StatusChanges, "staging.api.com:443")
	require.NotNil(t, stagingEntry, "Should find staging.api.com in status changes")
	assert.Equal(t, "allowed", stagingEntry.Run1Status, "Was allowed in run 1")
	assert.Equal(t, "denied", stagingEntry.Run2Status, "Now denied in run 2")
	assert.True(t, stagingEntry.IsAnomaly, "Should be flagged as anomaly")

	assert.Equal(t, 2, diff.Summary.StatusChangeCount, "Summary should show 2 status changes")
	assert.True(t, diff.Summary.HasAnomalies, "Should have anomalies")
}

func TestComputeFirewallDiff_VolumeChanges(t *testing.T) {
	t.Parallel()
	run1 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":  {Allowed: 23, Blocked: 0},
			"cdn.example.com:443": {Allowed: 50, Blocked: 0},
		},
	}
	run2 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":  {Allowed: 89, Blocked: 0},
			"cdn.example.com:443": {Allowed: 55, Blocked: 0},
		},
	}

	diff := computeFirewallDiff(100, 200, run1, run2)

	// api.github.com: 23 → 89 = +287% (over 100% threshold)
	assert.Len(t, diff.VolumeChanges, 1, "Should have 1 volume change (api.github.com, not cdn)")
	assert.Equal(t, "api.github.com:443", diff.VolumeChanges[0].Domain, "Volume change should be for api.github.com")
	assert.Equal(t, "+287%", diff.VolumeChanges[0].VolumeChange, "Volume change should be +287%")

	// cdn.example.com: 50 → 55 = +10% (under threshold, not flagged)
	assert.Equal(t, 1, diff.Summary.VolumeChangeCount, "Summary should show 1 volume change")
	assert.False(t, diff.Summary.HasAnomalies, "Volume changes alone should not create anomalies")
}

func TestComputeFirewallDiff_BothNil(t *testing.T) {
	t.Parallel()
	diff := computeFirewallDiff(100, 200, nil, nil)

	assert.Empty(t, diff.NewDomains, "Should have no new domains")
	assert.Empty(t, diff.RemovedDomains, "Should have no removed domains")
	assert.Empty(t, diff.StatusChanges, "Should have no status changes")
	assert.Empty(t, diff.VolumeChanges, "Should have no volume changes")
	assert.False(t, diff.Summary.HasAnomalies, "Should have no anomalies")
}

func TestComputeFirewallDiff_Run1Nil(t *testing.T) {
	t.Parallel()
	run2 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443": {Allowed: 5, Blocked: 0},
		},
	}

	diff := computeFirewallDiff(100, 200, nil, run2)

	assert.Len(t, diff.NewDomains, 1, "All run2 domains should be new")
	assert.Equal(t, "api.github.com:443", diff.NewDomains[0].Domain, "New domain should be api.github.com")
}

func TestComputeFirewallDiff_Run2Nil(t *testing.T) {
	t.Parallel()
	run1 := &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443": {Allowed: 5, Blocked: 0},
		},
	}

	diff := computeFirewallDiff(100, 200, run1, nil)

	assert.Len(t, diff.RemovedDomains, 1, "All run1 domains should be removed")
	assert.Equal(t, "api.github.com:443", diff.RemovedDomains[0].Domain, "Removed domain should be api.github.com")
}

func TestComputeFirewallDiff_NoChanges(t *testing.T) {
	t.Parallel()
	stats := map[string]DomainRequestStats{
		"api.github.com:443": {Allowed: 5, Blocked: 0},
	}
	run1 := &FirewallAnalysis{RequestsByDomain: stats}
	run2 := &FirewallAnalysis{RequestsByDomain: stats}

	diff := computeFirewallDiff(100, 200, run1, run2)

	assert.Empty(t, diff.NewDomains, "Should have no new domains")
	assert.Empty(t, diff.RemovedDomains, "Should have no removed domains")
	assert.Empty(t, diff.StatusChanges, "Should have no status changes")
	assert.Empty(t, diff.VolumeChanges, "Should have no volume changes")
}

func TestComputeFirewallDiff_CompleteScenario(t *testing.T) {
	t.Parallel()
	run1 := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{TotalRequests: 46, AllowedRequests: 38, BlockedRequests: 8},
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":       {Allowed: 23, Blocked: 0},
			"old-api.internal.com:443": {Allowed: 8, Blocked: 0},
			"staging.api.com:443":      {Allowed: 7, Blocked: 0},
			"blocked.example.com:443":  {Allowed: 0, Blocked: 8},
		},
	}
	run2 := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{TotalRequests: 108, AllowedRequests: 106, BlockedRequests: 2},
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":        {Allowed: 89, Blocked: 0},
			"registry.npmjs.org:443":    {Allowed: 15, Blocked: 0},
			"telemetry.example.com:443": {Allowed: 0, Blocked: 2},
			"staging.api.com:443":       {Allowed: 0, Blocked: 0}, // no requests (edge case)
			"blocked.example.com:443":   {Allowed: 0, Blocked: 0}, // no longer any requests (edge case)
		},
	}

	diff := computeFirewallDiff(12345, 12346, run1, run2)

	// Verify new domains
	assert.Len(t, diff.NewDomains, 2, "Should have 2 new domains")

	// Verify removed domains
	assert.Len(t, diff.RemovedDomains, 1, "Should have 1 removed domain (old-api.internal.com)")

	// api.github.com: 23 → 89 = +287%
	assert.GreaterOrEqual(t, len(diff.VolumeChanges), 1, "Should have at least 1 volume change")
}

func TestDomainStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		stats    DomainRequestStats
		expected string
	}{
		{name: "allowed only", stats: DomainRequestStats{Allowed: 5, Blocked: 0}, expected: "allowed"},
		{name: "denied only", stats: DomainRequestStats{Allowed: 0, Blocked: 3}, expected: "denied"},
		{name: "mixed", stats: DomainRequestStats{Allowed: 2, Blocked: 1}, expected: "mixed"},
		{name: "zero requests", stats: DomainRequestStats{Allowed: 0, Blocked: 0}, expected: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyFirewallDomainStatus(tt.stats)
			assert.Equal(t, tt.expected, result, "Domain status should match")
		})
	}
}

func TestFormatVolumeChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		total1   int
		total2   int
		expected string
	}{
		{name: "increase 287%", total1: 23, total2: 89, expected: "+287%"},
		{name: "decrease 50%", total1: 100, total2: 50, expected: "-50%"},
		{name: "double", total1: 10, total2: 20, expected: "+100%"},
		{name: "from zero", total1: 0, total2: 10, expected: "+∞"},
		{name: "no change", total1: 10, total2: 10, expected: "+0%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatVolumeChange(tt.total1, tt.total2)
			assert.Equal(t, tt.expected, result, "Volume change format should match")
		})
	}
}

func TestFirewallDiffJSONSerialization(t *testing.T) {
	t.Parallel()
	diff := computeFirewallDiff(100, 200, &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443": {Allowed: 5, Blocked: 0},
		},
	}, &FirewallAnalysis{
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":  {Allowed: 5, Blocked: 0},
			"new.example.com:443": {Allowed: 3, Blocked: 0},
		},
	})

	data, err := json.MarshalIndent(diff, "", "  ")
	require.NoError(t, err, "Should serialize diff to JSON")

	var parsed FirewallDiff
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err, "Should deserialize diff from JSON")

	assert.Equal(t, int64(100), parsed.Run1ID, "Run1ID should survive serialization")
	assert.Equal(t, int64(200), parsed.Run2ID, "Run2ID should survive serialization")
	assert.Len(t, parsed.NewDomains, 1, "Should have 1 new domain after deserialization")
	assert.Equal(t, "new.example.com:443", parsed.NewDomains[0].Domain, "New domain should match")
}

func TestStatusEmoji(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "✅", firewallStatusEmoji("allowed"), "Allowed should show checkmark")
	assert.Equal(t, "❌", firewallStatusEmoji("denied"), "Denied should show X")
	assert.Equal(t, "⚠️", firewallStatusEmoji("mixed"), "Mixed should show warning")
	assert.Equal(t, "❓", firewallStatusEmoji("unknown"), "Unknown should show question mark")
	assert.Equal(t, "❓", firewallStatusEmoji(""), "Empty should show question mark")
}

func TestIsEmptyDiff(t *testing.T) {
	t.Parallel()
	emptyDiff := &FirewallDiff{}
	assert.True(t, isEmptyFirewallDiff(emptyDiff), "Empty diff should be detected")

	nonEmptyDiff := &FirewallDiff{
		NewDomains: []DomainDiffEntry{{Domain: "test.com"}},
	}
	assert.False(t, isEmptyFirewallDiff(nonEmptyDiff), "Non-empty diff should not be detected as empty")
}

// findDiffEntry is a test helper to find a domain in a list of diff entries
func findDiffEntry(entries []DomainDiffEntry, domain string) *DomainDiffEntry {
	for i := range entries {
		if entries[i].Domain == domain {
			return &entries[i]
		}
	}
	return nil
}

// findMCPToolDiffEntry is a test helper to find a tool entry by server and tool name
func findMCPToolDiffEntry(entries []MCPToolDiffEntry, serverName, toolName string) *MCPToolDiffEntry {
	for i := range entries {
		if entries[i].ServerName == serverName && entries[i].ToolName == toolName {
			return &entries[i]
		}
	}
	return nil
}

func TestComputeMCPToolsDiff_NewTools(t *testing.T) {
	t.Parallel()
	run1 := &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 5}, ErrorCount: 0},
		},
	}
	run2 := &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 5}, ErrorCount: 0},
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "create_issue", CallCount: 3}, ErrorCount: 0},
			{ServerName: "playwright", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "screenshot", CallCount: 2}, ErrorCount: 1},
		},
	}

	diff := computeMCPToolsDiff(run1, run2)

	assert.Len(t, diff.NewTools, 2, "Should have 2 new tools")
	assert.Empty(t, diff.RemovedTools, "Should have no removed tools")
	assert.Empty(t, diff.ChangedTools, "Should have no changed tools")

	createIssue := findMCPToolDiffEntry(diff.NewTools, "github", "create_issue")
	require.NotNil(t, createIssue, "Should find create_issue in new tools")
	assert.Equal(t, "new", createIssue.Status, "Status should be 'new'")
	assert.Equal(t, 3, createIssue.Run2CallCount, "Call count should be 3")
	assert.False(t, createIssue.IsAnomaly, "No-error new tool should not be anomaly")

	screenshot := findMCPToolDiffEntry(diff.NewTools, "playwright", "screenshot")
	require.NotNil(t, screenshot, "Should find screenshot in new tools")
	assert.True(t, screenshot.IsAnomaly, "New tool with errors should be anomaly")
	assert.Equal(t, "new tool with errors", screenshot.AnomalyNote, "Anomaly note should explain errors")
	assert.Equal(t, 1, screenshot.Run2ErrorCount, "Error count should be 1")

	assert.Equal(t, 2, diff.Summary.NewToolCount, "Summary should show 2 new tools")
	assert.True(t, diff.Summary.HasAnomalies, "Should have anomalies")
	assert.Equal(t, 1, diff.Summary.AnomalyCount, "Should have 1 anomaly")
}

func TestComputeMCPToolsDiff_RemovedTools(t *testing.T) {
	t.Parallel()
	run1 := &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 10}, ErrorCount: 0},
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "search_repos", CallCount: 4}, ErrorCount: 0},
		},
	}
	run2 := &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 8}, ErrorCount: 0},
		},
	}

	diff := computeMCPToolsDiff(run1, run2)

	assert.Len(t, diff.RemovedTools, 1, "Should have 1 removed tool")
	assert.Equal(t, "search_repos", diff.RemovedTools[0].ToolName, "Removed tool should be search_repos")
	assert.Equal(t, "removed", diff.RemovedTools[0].Status, "Status should be 'removed'")
	assert.Equal(t, 4, diff.RemovedTools[0].Run1CallCount, "Should preserve run1 call count")
	assert.Equal(t, 1, diff.Summary.RemovedToolCount, "Summary should show 1 removed tool")
}

func TestComputeMCPToolsDiff_ChangedTools(t *testing.T) {
	t.Parallel()
	run1 := &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 5}, ErrorCount: 0},
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "create_pr", CallCount: 2}, ErrorCount: 1},
		},
	}
	run2 := &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 10}, ErrorCount: 0},
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "create_pr", CallCount: 2}, ErrorCount: 3},
		},
	}

	diff := computeMCPToolsDiff(run1, run2)

	assert.Len(t, diff.ChangedTools, 2, "Should have 2 changed tools")

	issueRead := findMCPToolDiffEntry(diff.ChangedTools, "github", "issue_read")
	require.NotNil(t, issueRead, "Should find issue_read in changed tools")
	assert.Equal(t, "changed", issueRead.Status, "Status should be 'changed'")
	assert.Equal(t, 5, issueRead.Run1CallCount, "Run1 call count should be 5")
	assert.Equal(t, 10, issueRead.Run2CallCount, "Run2 call count should be 10")
	assert.Equal(t, "+5", issueRead.CallCountChange, "Call count change should be +5")
	assert.False(t, issueRead.IsAnomaly, "No error increase should not be anomaly")

	createPR := findMCPToolDiffEntry(diff.ChangedTools, "github", "create_pr")
	require.NotNil(t, createPR, "Should find create_pr in changed tools")
	assert.True(t, createPR.IsAnomaly, "Increased error count should be anomaly")
	assert.Equal(t, "error count increased", createPR.AnomalyNote, "Anomaly note should explain error increase")
	assert.Equal(t, 1, createPR.Run1ErrorCount, "Run1 error count should be 1")
	assert.Equal(t, 3, createPR.Run2ErrorCount, "Run2 error count should be 3")

	assert.Equal(t, 2, diff.Summary.ChangedToolCount, "Summary should show 2 changed tools")
	assert.True(t, diff.Summary.HasAnomalies, "Should have anomalies")
	assert.Equal(t, 1, diff.Summary.AnomalyCount, "Should have 1 anomaly")
}

func TestComputeMCPToolsDiff_BothNil(t *testing.T) {
	t.Parallel()
	diff := computeMCPToolsDiff(nil, nil)

	assert.Empty(t, diff.NewTools, "Should have no new tools")
	assert.Empty(t, diff.RemovedTools, "Should have no removed tools")
	assert.Empty(t, diff.ChangedTools, "Should have no changed tools")
	assert.False(t, diff.Summary.HasAnomalies, "Should have no anomalies")
}

func TestComputeMCPToolsDiff_NoChanges(t *testing.T) {
	t.Parallel()
	toolSummary := []MCPToolSummary{
		{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 5}, ErrorCount: 0},
	}
	run1 := &MCPToolUsageData{Summary: toolSummary}
	run2 := &MCPToolUsageData{Summary: toolSummary}

	diff := computeMCPToolsDiff(run1, run2)

	assert.Empty(t, diff.NewTools, "Should have no new tools")
	assert.Empty(t, diff.RemovedTools, "Should have no removed tools")
	assert.Empty(t, diff.ChangedTools, "Should have no changed tools")
}

func TestComputeMCPToolsDiff_SortedOutput(t *testing.T) {
	t.Parallel()
	run1 := &MCPToolUsageData{}
	run2 := &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{ServerName: "z-server", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "tool", CallCount: 1}},
			{ServerName: "a-server", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "tool", CallCount: 1}},
			{ServerName: "m-server", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "tool", CallCount: 1}},
		},
	}

	diff := computeMCPToolsDiff(run1, run2)

	require.Len(t, diff.NewTools, 3, "Should have 3 new tools")
	assert.Equal(t, "a-server", diff.NewTools[0].ServerName, "First tool should be a-server (sorted)")
	assert.Equal(t, "m-server", diff.NewTools[1].ServerName, "Second tool should be m-server (sorted)")
	assert.Equal(t, "z-server", diff.NewTools[2].ServerName, "Third tool should be z-server (sorted)")
}

func TestComputeRunMetricsDiff_WithData(t *testing.T) {
	t.Parallel()
	summary1 := &RunSummary{
		RunID: 100,
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				TokenUsage: 5000,
				Duration:   10 * time.Minute,
				Turns:      8,
			},
		},
	}
	summary2 := &RunSummary{
		RunID: 200,
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				TokenUsage: 7500,
				Duration:   15 * time.Minute,
				Turns:      12,
			},
		},
	}

	diff := computeRunMetricsDiff(summary1, summary2)

	require.NotNil(t, diff, "Should produce metrics diff when data is available")
	assert.Equal(t, 5000, diff.Run1TokenUsage, "Run1 token usage should be 5000")
	assert.Equal(t, 7500, diff.Run2TokenUsage, "Run2 token usage should be 7500")
	assert.Equal(t, "+50%", diff.TokenUsageChange, "Token usage should increase by 50%")

	assert.Equal(t, "10m0s", diff.Run1Duration, "Run1 duration should be 10m0s")
	assert.Equal(t, "15m0s", diff.Run2Duration, "Run2 duration should be 15m0s")
	assert.Equal(t, "+5m0s", diff.DurationChange, "Duration should increase by 5m0s")

	assert.Equal(t, 8, diff.Run1Turns, "Run1 turns should be 8")
	assert.Equal(t, 12, diff.Run2Turns, "Run2 turns should be 12")
	assert.Equal(t, 4, diff.TurnsChange, "Turns change should be +4")
}

func TestComputeRunMetricsDiff_NegativeChange(t *testing.T) {
	t.Parallel()
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{
			TokenUsage: 8000,
			Duration:   20 * time.Minute,
			Turns:      15,
		},
	},
	}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{
			TokenUsage: 4000,
			Duration:   12 * time.Minute,
			Turns:      10,
		},
	},
	}

	diff := computeRunMetricsDiff(summary1, summary2)

	require.NotNil(t, diff, "Should produce metrics diff")
	assert.Equal(t, "-50%", diff.TokenUsageChange, "Token usage should decrease by 50%")
	assert.Equal(t, "-8m0s", diff.DurationChange, "Duration should decrease by 8m0s")
	assert.Equal(t, -5, diff.TurnsChange, "Turns change should be -5")
}

func TestComputeRunMetricsDiff_BothNil(t *testing.T) {
	t.Parallel()
	diff := computeRunMetricsDiff(nil, nil)
	assert.Nil(t, diff, "Should return nil when both summaries are nil")
}

func TestComputeRunMetricsDiff_AllZero(t *testing.T) {
	t.Parallel()
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{Run: WorkflowRun{}}}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{Run: WorkflowRun{}}}

	diff := computeRunMetricsDiff(summary1, summary2)
	assert.Nil(t, diff, "Should return nil when all metrics are zero")
}

func TestComputeAuditDiff_CombinesAllSections(t *testing.T) {
	t.Parallel()
	summary1 := &RunSummary{
		RunID: 100,
		RunAnalysis: RunAnalysis{
			FirewallAnalysis: &FirewallAnalysis{
				RequestsByDomain: map[string]DomainRequestStats{
					"api.github.com:443": {Allowed: 5, Blocked: 0},
				},
			},
			MCPToolUsage: &MCPToolUsageData{
				Summary: []MCPToolSummary{
					{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 3}, ErrorCount: 0},
				},
			},
			Run: WorkflowRun{TokenUsage: 2000, Turns: 5},
		},
	}
	summary2 := &RunSummary{
		RunID: 200,
		RunAnalysis: RunAnalysis{
			FirewallAnalysis: &FirewallAnalysis{
				RequestsByDomain: map[string]DomainRequestStats{
					"api.github.com:443":  {Allowed: 5, Blocked: 0},
					"new.example.com:443": {Allowed: 3, Blocked: 0},
				},
			},
			MCPToolUsage: &MCPToolUsageData{
				Summary: []MCPToolSummary{
					{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 7}, ErrorCount: 0},
					{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "create_issue", CallCount: 2}, ErrorCount: 0},
				},
			},
			Run: WorkflowRun{TokenUsage: 3000, Turns: 8},
		},
	}

	diff := computeAuditDiff(100, 200, summary1, summary2)

	assert.Equal(t, int64(100), diff.Run1ID, "Run1ID should match")
	assert.Equal(t, int64(200), diff.Run2ID, "Run2ID should match")

	require.NotNil(t, diff.FirewallDiff, "Should have firewall diff")
	assert.Len(t, diff.FirewallDiff.NewDomains, 1, "Should have 1 new domain")

	require.NotNil(t, diff.MCPToolsDiff, "Should have MCP tools diff")
	assert.Len(t, diff.MCPToolsDiff.NewTools, 1, "Should have 1 new tool")
	assert.Len(t, diff.MCPToolsDiff.ChangedTools, 1, "Should have 1 changed tool")

	require.NotNil(t, diff.RunMetricsDiff, "Should have run metrics diff")
	assert.Equal(t, 2000, diff.RunMetricsDiff.Run1TokenUsage, "Run1 token usage should match")
	assert.Equal(t, 3000, diff.RunMetricsDiff.Run2TokenUsage, "Run2 token usage should match")
}

func TestComputeAuditDiff_NilSummaries(t *testing.T) {
	t.Parallel()
	diff := computeAuditDiff(100, 200, nil, nil)

	assert.Equal(t, int64(100), diff.Run1ID, "Run1ID should be set even with nil summaries")
	assert.NotNil(t, diff.FirewallDiff, "FirewallDiff should be non-nil (empty)")
	assert.Nil(t, diff.MCPToolsDiff, "MCPToolsDiff should be nil when no MCP data")
	assert.Nil(t, diff.RunMetricsDiff, "RunMetricsDiff should be nil when no metrics data")
	assert.True(t, isEmptyAuditDiff(diff), "Diff with nil summaries should be empty")
}

func TestAuditDiffJSONSerialization(t *testing.T) {
	t.Parallel()
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{
		FirewallAnalysis: &FirewallAnalysis{
			RequestsByDomain: map[string]DomainRequestStats{
				"api.github.com:443": {Allowed: 5},
			},
		},
		MCPToolUsage: &MCPToolUsageData{
			Summary: []MCPToolSummary{
				{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 3}},
			},
		},
		Run: WorkflowRun{TokenUsage: 1000, Turns: 4},
	},
	}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{
		FirewallAnalysis: &FirewallAnalysis{
			RequestsByDomain: map[string]DomainRequestStats{
				"api.github.com:443":  {Allowed: 5},
				"new.example.com:443": {Allowed: 2},
			},
		},
		MCPToolUsage: &MCPToolUsageData{
			Summary: []MCPToolSummary{
				{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 5}},
			},
		},
		Run: WorkflowRun{TokenUsage: 1500, Turns: 6},
	},
	}

	diff := computeAuditDiff(100, 200, summary1, summary2)

	data, err := json.MarshalIndent(diff, "", "  ")
	require.NoError(t, err, "Should serialize AuditDiff to JSON")

	var parsed AuditDiff
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err, "Should deserialize AuditDiff from JSON")

	assert.Equal(t, int64(100), parsed.Run1ID, "Run1ID should survive serialization")
	assert.Equal(t, int64(200), parsed.Run2ID, "Run2ID should survive serialization")
	require.NotNil(t, parsed.FirewallDiff, "FirewallDiff should survive serialization")
	assert.Len(t, parsed.FirewallDiff.NewDomains, 1, "New domains should survive serialization")
	require.NotNil(t, parsed.MCPToolsDiff, "MCPToolsDiff should survive serialization")
	require.NotNil(t, parsed.RunMetricsDiff, "RunMetricsDiff should survive serialization")
	assert.Equal(t, 1000, parsed.RunMetricsDiff.Run1TokenUsage, "Token usage should survive serialization")
}

func TestFormatCountChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		count1   int
		count2   int
		expected string
	}{
		{name: "increase", count1: 3, count2: 8, expected: "+5"},
		{name: "decrease", count1: 10, count2: 3, expected: "-7"},
		{name: "no change", count1: 5, count2: 5, expected: "+0"},
		{name: "from zero", count1: 0, count2: 4, expected: "+4"},
		{name: "to zero", count1: 6, count2: 0, expected: "-6"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCountChange(tt.count1, tt.count2)
			assert.Equal(t, tt.expected, result, "Count change format should match")
		})
	}
}

func TestIsEmptyMCPToolsDiff(t *testing.T) {
	t.Parallel()
	assert.True(t, isEmptyMCPToolsDiff(&MCPToolsDiff{}), "Empty MCPToolsDiff should be detected")
	assert.False(t, isEmptyMCPToolsDiff(&MCPToolsDiff{
		NewTools: []MCPToolDiffEntry{{ToolName: "test"}},
	}), "Non-empty MCPToolsDiff should not be detected as empty")
}

func TestIsEmptyAuditDiff(t *testing.T) {
	t.Parallel()
	assert.True(t, isEmptyAuditDiff(&AuditDiff{}), "Empty AuditDiff should be detected")
	assert.True(t, isEmptyAuditDiff(&AuditDiff{
		FirewallDiff: &FirewallDiff{},
		MCPToolsDiff: &MCPToolsDiff{},
	}), "AuditDiff with empty sub-diffs should be detected as empty")
	assert.False(t, isEmptyAuditDiff(&AuditDiff{
		MCPToolsDiff: &MCPToolsDiff{
			NewTools: []MCPToolDiffEntry{{ToolName: "test"}},
		},
	}), "AuditDiff with MCP changes should not be empty")
	assert.False(t, isEmptyAuditDiff(&AuditDiff{
		RunMetricsDiff: &RunMetricsDiff{Run1TokenUsage: 100},
	}), "AuditDiff with metrics diff should not be empty")
}

func TestComputeTokenUsageDiff_BothNil(t *testing.T) {
	t.Parallel()
	diff := computeTokenUsageDiff(nil, nil)
	assert.Nil(t, diff, "Should return nil when both summaries are nil")
}

func TestComputeTokenUsageDiff_WithData(t *testing.T) {
	t.Parallel()
	tu1 := &TokenUsageSummary{
		TotalInputTokens:      10000,
		TotalOutputTokens:     2000,
		TotalCacheReadTokens:  5000,
		TotalCacheWriteTokens: 1000,
		TotalAIC:              0.8,
		TotalRequests:         10,
		CacheEfficiency:       0.333,
	}
	tu2 := &TokenUsageSummary{
		TotalInputTokens:      15000,
		TotalOutputTokens:     3000,
		TotalCacheReadTokens:  7000,
		TotalCacheWriteTokens: 800,
		TotalAIC:              1.4,
		TotalRequests:         14,
		CacheEfficiency:       0.318,
	}

	diff := computeTokenUsageDiff(tu1, tu2)

	require.NotNil(t, diff, "Should produce token usage diff when data is available")
	assert.Equal(t, 10000, diff.Run1InputTokens, "Run1 input tokens should be 10000")
	assert.Equal(t, 15000, diff.Run2InputTokens, "Run2 input tokens should be 15000")
	assert.Equal(t, "+50%", diff.InputTokensChange, "Input tokens should increase by 50%")

	assert.Equal(t, 2000, diff.Run1OutputTokens, "Run1 output tokens should be 2000")
	assert.Equal(t, 3000, diff.Run2OutputTokens, "Run2 output tokens should be 3000")
	assert.Equal(t, "+50%", diff.OutputTokensChange, "Output tokens should increase by 50%")

	assert.Equal(t, 5000, diff.Run1CacheReadTokens, "Run1 cache read tokens should be 5000")
	assert.Equal(t, 7000, diff.Run2CacheReadTokens, "Run2 cache read tokens should be 7000")
	assert.Equal(t, "+40%", diff.CacheReadTokensChange, "Cache read tokens should increase by 40%")

	assert.Equal(t, 1000, diff.Run1CacheWriteTokens, "Run1 cache write tokens should be 1000")
	assert.Equal(t, 800, diff.Run2CacheWriteTokens, "Run2 cache write tokens should be 800")
	assert.Equal(t, "-20%", diff.CacheWriteTokensChange, "Cache write tokens should decrease by 20%")

	assert.InDelta(t, 0.8, diff.Run1AIC, 1e-9, "Run1 AI Credits should be 0.8")
	assert.InDelta(t, 1.4, diff.Run2AIC, 1e-9, "Run2 AI Credits should be 1.4")
	assert.Equal(t, "+0.600", diff.AICChange, "AI Credits delta should be +0.600")

	assert.Equal(t, 10, diff.Run1TotalRequests, "Run1 requests should be 10")
	assert.Equal(t, 14, diff.Run2TotalRequests, "Run2 requests should be 14")
	assert.Equal(t, "+4", diff.RequestsDelta, "Requests delta should be +4")

	assert.InDelta(t, 0.333, diff.Run1CacheEfficiency, 0.001, "Run1 cache efficiency should match")
	assert.InDelta(t, 0.318, diff.Run2CacheEfficiency, 0.001, "Run2 cache efficiency should match")
	assert.Equal(t, "-1.5pp", diff.CacheEfficiencyChange, "Cache efficiency change should be -1.5pp")
}

func TestComputeTokenUsageDiff_Run1Nil(t *testing.T) {
	t.Parallel()
	tu2 := &TokenUsageSummary{
		TotalInputTokens:  5000,
		TotalOutputTokens: 1000,
		TotalRequests:     5,
	}

	diff := computeTokenUsageDiff(nil, tu2)

	require.NotNil(t, diff, "Should produce diff when run2 has data")
	assert.Equal(t, 0, diff.Run1InputTokens, "Run1 input tokens should be 0 when nil")
	assert.Equal(t, 5000, diff.Run2InputTokens, "Run2 input tokens should be 5000")
	assert.Equal(t, "+∞", diff.InputTokensChange, "Input change should be +∞ from zero")
}

func TestComputeTokenUsageDiff_Run2Nil(t *testing.T) {
	t.Parallel()
	tu1 := &TokenUsageSummary{
		TotalInputTokens:  5000,
		TotalOutputTokens: 1000,
	}

	diff := computeTokenUsageDiff(tu1, nil)

	require.NotNil(t, diff, "Should produce diff when run1 has data")
	assert.Equal(t, 5000, diff.Run1InputTokens, "Run1 input tokens should be 5000")
	assert.Equal(t, 0, diff.Run2InputTokens, "Run2 input tokens should be 0 when nil")
	assert.Equal(t, "-100%", diff.InputTokensChange, "Input change should be -100%")
}

func TestComputeRunMetricsDiff_WithTokenUsageDetails(t *testing.T) {
	t.Parallel()
	summary1 := &RunSummary{
		RunID: 100,
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{Duration: 5 * time.Minute, Turns: 4},
			TokenUsage: &TokenUsageSummary{
				TotalInputTokens:  8000,
				TotalOutputTokens: 1500,
				TotalAIC:          0.6,
				TotalRequests:     8,
				CacheEfficiency:   0.25,
			},
		},
	}
	summary2 := &RunSummary{
		RunID: 200,
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{Duration: 7 * time.Minute, Turns: 6},
			TokenUsage: &TokenUsageSummary{
				TotalInputTokens:  12000,
				TotalOutputTokens: 2000,
				TotalAIC:          0.9,
				TotalRequests:     11,
				CacheEfficiency:   0.30,
			},
		},
	}

	diff := computeRunMetricsDiff(summary1, summary2)

	require.NotNil(t, diff, "Should produce metrics diff")
	require.NotNil(t, diff.TokenUsageDetails, "Should populate TokenUsageDetails from RunSummary.TokenUsage")

	assert.Equal(t, 8000, diff.TokenUsageDetails.Run1InputTokens, "Run1 input tokens should be 8000")
	assert.Equal(t, 12000, diff.TokenUsageDetails.Run2InputTokens, "Run2 input tokens should be 12000")
	assert.Equal(t, "+50%", diff.TokenUsageDetails.InputTokensChange, "Input tokens change should be +50%")

	assert.InDelta(t, 0.6, diff.TokenUsageDetails.Run1AIC, 1e-9, "Run1 AI Credits should be 0.6")
	assert.InDelta(t, 0.9, diff.TokenUsageDetails.Run2AIC, 1e-9, "Run2 AI Credits should be 0.9")
	assert.Equal(t, "+0.300", diff.TokenUsageDetails.AICChange, "AI Credits delta should be +0.300")
}

func TestComputeRunMetricsDiff_TokenUsageDetailsAloneNotNil(t *testing.T) {
	t.Parallel()
	// Verify that detailed token usage data alone (without Run.TokenUsage set)
	// still produces a non-nil RunMetricsDiff
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{},
		TokenUsage: &TokenUsageSummary{
			TotalInputTokens: 5000,
			TotalRequests:    5,
		},
	},
	}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{},
		TokenUsage: &TokenUsageSummary{
			TotalInputTokens: 8000,
			TotalRequests:    7,
		},
	},
	}

	diff := computeRunMetricsDiff(summary1, summary2)

	require.NotNil(t, diff, "Should produce non-nil diff when only TokenUsage data is present")
	require.NotNil(t, diff.TokenUsageDetails, "Should have TokenUsageDetails")
	assert.Equal(t, 5000, diff.TokenUsageDetails.Run1InputTokens, "Run1 input tokens should be 5000")
	assert.Equal(t, 8000, diff.TokenUsageDetails.Run2InputTokens, "Run2 input tokens should be 8000")
}

func TestComputeAuditDiff_MultipleRuns(t *testing.T) {
	t.Parallel()
	base := &RunSummary{
		RunID: 100,
		RunAnalysis: RunAnalysis{
			FirewallAnalysis: &FirewallAnalysis{
				RequestsByDomain: map[string]DomainRequestStats{
					"api.github.com:443": {Allowed: 5, Blocked: 0},
				},
			},
			MCPToolUsage: &MCPToolUsageData{
				Summary: []MCPToolSummary{
					{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 3}, ErrorCount: 0},
				},
			},
			Run: WorkflowRun{Turns: 5},
			TokenUsage: &TokenUsageSummary{
				TotalInputTokens:  10000,
				TotalOutputTokens: 2000,
				TotalRequests:     10,
			},
		},
	}

	compare1 := &RunSummary{
		RunID: 200,
		RunAnalysis: RunAnalysis{
			FirewallAnalysis: &FirewallAnalysis{
				RequestsByDomain: map[string]DomainRequestStats{
					"api.github.com:443":   {Allowed: 5, Blocked: 0},
					"new1.example.com:443": {Allowed: 3, Blocked: 0},
				},
			},
			MCPToolUsage: &MCPToolUsageData{
				Summary: []MCPToolSummary{
					{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "issue_read", CallCount: 5}, ErrorCount: 0},
				},
			},
			Run: WorkflowRun{Turns: 7},
			TokenUsage: &TokenUsageSummary{
				TotalInputTokens:  15000,
				TotalOutputTokens: 3000,
				TotalRequests:     12,
			},
		},
	}

	compare2 := &RunSummary{
		RunID: 300,
		RunAnalysis: RunAnalysis{
			FirewallAnalysis: &FirewallAnalysis{
				RequestsByDomain: map[string]DomainRequestStats{
					"api.github.com:443":   {Allowed: 5, Blocked: 0},
					"new2.example.com:443": {Allowed: 1, Blocked: 2},
				},
			},
			Run: WorkflowRun{Turns: 4},
			TokenUsage: &TokenUsageSummary{
				TotalInputTokens:  8000,
				TotalOutputTokens: 1500,
				TotalRequests:     8,
			},
		},
	}

	// Compute two diffs from the same base
	diff1 := computeAuditDiff(base.RunID, compare1.RunID, base, compare1)
	diff2 := computeAuditDiff(base.RunID, compare2.RunID, base, compare2)

	// Diff 1: base vs compare1
	assert.Equal(t, int64(100), diff1.Run1ID, "Diff1 Run1ID should be base")
	assert.Equal(t, int64(200), diff1.Run2ID, "Diff1 Run2ID should be compare1")
	require.NotNil(t, diff1.FirewallDiff, "Diff1 should have firewall diff")
	assert.Len(t, diff1.FirewallDiff.NewDomains, 1, "Diff1 should have 1 new domain")
	assert.Equal(t, "new1.example.com:443", diff1.FirewallDiff.NewDomains[0].Domain, "Diff1 new domain should be new1")
	require.NotNil(t, diff1.RunMetricsDiff, "Diff1 should have run metrics diff")
	require.NotNil(t, diff1.RunMetricsDiff.TokenUsageDetails, "Diff1 should have token usage details")
	assert.Equal(t, "+50%", diff1.RunMetricsDiff.TokenUsageDetails.InputTokensChange, "Diff1 input tokens should increase by 50%")

	// Diff 2: base vs compare2
	assert.Equal(t, int64(100), diff2.Run1ID, "Diff2 Run1ID should be base")
	assert.Equal(t, int64(300), diff2.Run2ID, "Diff2 Run2ID should be compare2")
	require.NotNil(t, diff2.FirewallDiff, "Diff2 should have firewall diff")
	assert.Len(t, diff2.FirewallDiff.NewDomains, 1, "Diff2 should have 1 new domain")
	assert.Equal(t, "new2.example.com:443", diff2.FirewallDiff.NewDomains[0].Domain, "Diff2 new domain should be new2")
	assert.True(t, diff2.FirewallDiff.NewDomains[0].IsAnomaly, "Diff2 new domain should be anomaly (blocked)")
	require.NotNil(t, diff2.RunMetricsDiff, "Diff2 should have run metrics diff")
	require.NotNil(t, diff2.RunMetricsDiff.TokenUsageDetails, "Diff2 should have token usage details")
	assert.Equal(t, "-20%", diff2.RunMetricsDiff.TokenUsageDetails.InputTokensChange, "Diff2 input tokens should decrease by 20%")

	// The two diffs should be independent (no shared state)
	assert.NotEqual(t, diff1.Run2ID, diff2.Run2ID, "The two diffs should have different Run2IDs")
}

func TestComputeGitHubRateLimitDiff_BothNil(t *testing.T) {
	t.Parallel()
	diff := computeGitHubRateLimitDiff(nil, nil)
	assert.Nil(t, diff, "both nil should return nil")
}

func TestComputeGitHubRateLimitDiff_WithData(t *testing.T) {
	t.Parallel()
	rl1 := &GitHubRateLimitUsage{
		TotalRequestsMade: 40,
		CoreConsumed:      40,
		CoreRemaining:     4960,
		CoreLimit:         5000,
	}
	rl2 := &GitHubRateLimitUsage{
		TotalRequestsMade: 60,
		CoreConsumed:      60,
		CoreRemaining:     4940,
		CoreLimit:         5000,
	}

	diff := computeGitHubRateLimitDiff(rl1, rl2)
	require.NotNil(t, diff, "diff should not be nil")

	assert.Equal(t, 40, diff.Run1TotalAPICalls, "Run1 total API calls should be 40")
	assert.Equal(t, 60, diff.Run2TotalAPICalls, "Run2 total API calls should be 60")
	assert.Equal(t, "+50%", diff.APICallsChange, "API calls should increase by 50%")
	assert.Equal(t, 40, diff.Run1CoreConsumed, "Run1 core consumed should be 40")
	assert.Equal(t, 60, diff.Run2CoreConsumed, "Run2 core consumed should be 60")
	assert.Equal(t, "+50%", diff.CoreConsumedChange, "Core consumed should increase by 50%")
	assert.Equal(t, 4960, diff.Run1CoreRemaining, "Run1 core remaining should be 4960")
	assert.Equal(t, 4940, diff.Run2CoreRemaining, "Run2 core remaining should be 4940")
	assert.Equal(t, 5000, diff.Run1CoreLimit, "Run1 core limit should be 5000")
	assert.Equal(t, 5000, diff.Run2CoreLimit, "Run2 core limit should be 5000")
}

func TestComputeGitHubRateLimitDiff_Run1Nil(t *testing.T) {
	t.Parallel()
	rl2 := &GitHubRateLimitUsage{
		TotalRequestsMade: 30,
		CoreConsumed:      30,
		CoreRemaining:     4970,
		CoreLimit:         5000,
	}

	diff := computeGitHubRateLimitDiff(nil, rl2)
	require.NotNil(t, diff, "diff should not be nil when run2 has data")
	assert.Equal(t, 0, diff.Run1TotalAPICalls, "Run1 total API calls should be 0")
	assert.Equal(t, 30, diff.Run2TotalAPICalls, "Run2 total API calls should be 30")
}

func TestComputeRunMetricsDiff_WithRateLimitData(t *testing.T) {
	t.Parallel()
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{
			TokenUsage: 1000,
			Duration:   2 * time.Minute,
			Turns:      5,
		},
		GitHubRateLimitUsage: &GitHubRateLimitUsage{
			TotalRequestsMade: 40,
			CoreConsumed:      40,
			CoreRemaining:     4960,
			CoreLimit:         5000,
		},
	},
	}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{
			TokenUsage: 1200,
			Duration:   3 * time.Minute,
			Turns:      6,
		},
		GitHubRateLimitUsage: &GitHubRateLimitUsage{
			TotalRequestsMade: 60,
			CoreConsumed:      60,
			CoreRemaining:     4940,
			CoreLimit:         5000,
		},
	},
	}

	diff := computeRunMetricsDiff(summary1, summary2)
	require.NotNil(t, diff, "diff should not be nil")
	require.NotNil(t, diff.GitHubRateLimitDetails, "GitHubRateLimitDetails should be populated")
	assert.Equal(t, 40, diff.GitHubRateLimitDetails.Run1TotalAPICalls, "Run1 API calls should be 40")
	assert.Equal(t, 60, diff.GitHubRateLimitDetails.Run2TotalAPICalls, "Run2 API calls should be 60")
	assert.Equal(t, "+50%", diff.GitHubRateLimitDetails.APICallsChange, "API calls should increase by 50%")
}

func TestComputeRunMetricsDiff_RateLimitAloneNotNil(t *testing.T) {
	t.Parallel()
	// RunMetricsDiff should be non-nil when only rate limit data is present
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{
		GitHubRateLimitUsage: &GitHubRateLimitUsage{
			TotalRequestsMade: 10,
		},
	},
	}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{
		GitHubRateLimitUsage: &GitHubRateLimitUsage{
			TotalRequestsMade: 15,
		},
	},
	}

	diff := computeRunMetricsDiff(summary1, summary2)
	require.NotNil(t, diff, "diff should not be nil when rate limit data is present")
	require.NotNil(t, diff.GitHubRateLimitDetails, "GitHubRateLimitDetails should be populated")
}

// --- Tool calls diff tests ---

func TestComputeToolCallsDiff_BothNil(t *testing.T) {
	t.Parallel()
	result := computeToolCallsDiff(nil, nil)
	assert.Nil(t, result, "Should return nil when both metrics are nil")
}

func TestComputeToolCallsDiff_BothEmpty(t *testing.T) {
	t.Parallel()
	m1 := &LogMetrics{}
	m2 := &LogMetrics{}
	result := computeToolCallsDiff(m1, m2)
	assert.Nil(t, result, "Should return nil when both metrics have no tool calls")
}

func TestComputeToolCallsDiff_DuplicateToolNames(t *testing.T) {
	t.Parallel()
	// When the same tool name appears multiple times (e.g. metrics aggregated from multiple log files),
	// call counts should be summed and max sizes kept.
	m1 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 3, MaxInputSize: 100, MaxOutputSize: 200},
			{Name: "bash", CallCount: 4, MaxInputSize: 150, MaxOutputSize: 180},
		},
	}
	m2 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 5, MaxInputSize: 120, MaxOutputSize: 300},
			{Name: "bash", CallCount: 2, MaxInputSize: 90, MaxOutputSize: 250},
		},
	}

	diff := computeToolCallsDiff(m1, m2)
	require.NotNil(t, diff, "Should produce diff")

	require.Len(t, diff.AllTools, 1, "Should have 1 unique tool (bash)")
	bash := diff.AllTools[0]
	assert.Equal(t, "bash", bash.Name, "Tool should be bash")
	// Run1: 3+4=7, Run2: 5+2=7 → unchanged
	assert.Equal(t, 7, bash.Run1CallCount, "Run1 call count should be sum: 3+4=7")
	assert.Equal(t, 7, bash.Run2CallCount, "Run2 call count should be sum: 5+2=7")
	assert.Equal(t, "unchanged", bash.Status, "Status should be unchanged when counts are equal")
	// Max input: run1=max(100,150)=150, run2=max(120,90)=120
	assert.Equal(t, 150, bash.Run1MaxInputSize, "Run1 max input should be max(100,150)=150")
	assert.Equal(t, 120, bash.Run2MaxInputSize, "Run2 max input should be max(120,90)=120")
	// Max output: run1=max(200,180)=200, run2=max(300,250)=300
	assert.Equal(t, 200, bash.Run1MaxOutputSize, "Run1 max output should be max(200,180)=200")
	assert.Equal(t, 300, bash.Run2MaxOutputSize, "Run2 max output should be max(300,250)=300")
}

func TestComputeToolCallsDiff_NewTools(t *testing.T) {
	t.Parallel()
	m1 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "gh", CallCount: 5},
		},
	}
	m2 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "gh", CallCount: 5},
			{Name: "bash", CallCount: 3, MaxInputSize: 200, MaxOutputSize: 500},
			{Name: "edit", CallCount: 2},
		},
	}

	diff := computeToolCallsDiff(m1, m2)
	require.NotNil(t, diff, "Should produce diff when tool calls exist")

	assert.Len(t, diff.NewTools, 2, "Should have 2 new tools (bash, edit)")
	assert.Empty(t, diff.RemovedTools, "Should have no removed tools")
	assert.Empty(t, diff.ChangedTools, "Should have no changed tools")
	assert.Len(t, diff.AllTools, 3, "AllTools should include all 3 tools")

	bashEntry := findToolCallDiffEntry(diff.NewTools, "bash")
	require.NotNil(t, bashEntry, "Should find bash in new tools")
	assert.Equal(t, "new", bashEntry.Status, "bash status should be 'new'")
	assert.Equal(t, 3, bashEntry.Run2CallCount, "bash call count should be 3")
	assert.Equal(t, 200, bashEntry.Run2MaxInputSize, "bash max input should be 200")
	assert.Equal(t, 500, bashEntry.Run2MaxOutputSize, "bash max output should be 500")

	assert.Equal(t, 2, diff.Summary.NewToolCount, "Summary should show 2 new tools")
	assert.Equal(t, 5, diff.Summary.Run1TotalCalls, "Run1 total should be 5")
	assert.Equal(t, 10, diff.Summary.Run2TotalCalls, "Run2 total should be 10 (5+3+2)")
}

func TestComputeToolCallsDiff_RemovedTools(t *testing.T) {
	t.Parallel()
	m1 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 8},
			{Name: "gh", CallCount: 4},
			{Name: "edit", CallCount: 2},
		},
	}
	m2 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 6},
		},
	}

	diff := computeToolCallsDiff(m1, m2)
	require.NotNil(t, diff, "Should produce diff")

	assert.Len(t, diff.RemovedTools, 2, "Should have 2 removed tools (gh, edit)")
	assert.Empty(t, diff.NewTools, "Should have no new tools")

	assert.Equal(t, 2, diff.Summary.RemovedToolCount, "Summary should show 2 removed tools")
	assert.Equal(t, 14, diff.Summary.Run1TotalCalls, "Run1 total should be 14")
	assert.Equal(t, 6, diff.Summary.Run2TotalCalls, "Run2 total should be 6")
}

func TestComputeToolCallsDiff_ChangedTools(t *testing.T) {
	t.Parallel()
	m1 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 5, MaxOutputSize: 300},
			{Name: "gh", CallCount: 3},
		},
	}
	m2 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 12, MaxOutputSize: 800},
			{Name: "gh", CallCount: 3},
		},
	}

	diff := computeToolCallsDiff(m1, m2)
	require.NotNil(t, diff, "Should produce diff")

	assert.Len(t, diff.ChangedTools, 1, "Should have 1 changed tool (bash)")
	assert.Empty(t, diff.NewTools, "Should have no new tools")
	assert.Empty(t, diff.RemovedTools, "Should have no removed tools")

	bashEntry := findToolCallDiffEntry(diff.ChangedTools, "bash")
	require.NotNil(t, bashEntry, "Should find bash in changed tools")
	assert.Equal(t, "changed", bashEntry.Status, "bash status should be 'changed'")
	assert.Equal(t, 5, bashEntry.Run1CallCount, "bash run1 call count should be 5")
	assert.Equal(t, 12, bashEntry.Run2CallCount, "bash run2 call count should be 12")
	assert.Equal(t, "+7", bashEntry.CallCountChange, "bash change should be +7")
	assert.Equal(t, 300, bashEntry.Run1MaxOutputSize, "run1 max output should be 300")
	assert.Equal(t, 800, bashEntry.Run2MaxOutputSize, "run2 max output should be 800")

	assert.Equal(t, 1, diff.Summary.ChangedToolCount, "Summary should show 1 changed tool")
}

func TestComputeToolCallsDiff_AllToolsContainsEverything(t *testing.T) {
	t.Parallel()
	m1 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 4},
			{Name: "gh", CallCount: 2},
		},
	}
	m2 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 8},
			{Name: "edit", CallCount: 3},
		},
	}

	diff := computeToolCallsDiff(m1, m2)
	require.NotNil(t, diff, "Should produce diff")

	// AllTools should include bash (changed), gh (removed), edit (new) - all 3 tools
	assert.Len(t, diff.AllTools, 3, "AllTools should contain all 3 unique tools")

	statuses := make(map[string]string)
	for _, e := range diff.AllTools {
		statuses[e.Name] = e.Status
	}
	assert.Equal(t, "changed", statuses["bash"], "bash should be changed")
	assert.Equal(t, "removed", statuses["gh"], "gh should be removed")
	assert.Equal(t, "new", statuses["edit"], "edit should be new")
}

func TestComputeToolCallsDiff_SortedOutput(t *testing.T) {
	t.Parallel()
	m1 := &LogMetrics{}
	m2 := &LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "z-tool", CallCount: 1},
			{Name: "a-tool", CallCount: 1},
			{Name: "m-tool", CallCount: 1},
		},
	}

	diff := computeToolCallsDiff(m1, m2)
	require.NotNil(t, diff, "Should produce diff")
	require.Len(t, diff.AllTools, 3, "Should have 3 tools in AllTools")

	// AllTools is built from sorted keys
	assert.Equal(t, "a-tool", diff.AllTools[0].Name, "First tool should be a-tool (sorted)")
	assert.Equal(t, "m-tool", diff.AllTools[1].Name, "Second tool should be m-tool (sorted)")
	assert.Equal(t, "z-tool", diff.AllTools[2].Name, "Third tool should be z-tool (sorted)")
}

// --- Bash commands diff tests ---

func TestComputeBashCommandsDiff_NoBash(t *testing.T) {
	t.Parallel()
	// computeBashCommandsDiff receives pre-filtered maps; passing no bash tools → nil
	run1Tools := map[string]ToolCallInfo{}
	run2Tools := map[string]ToolCallInfo{}
	result := computeBashCommandsDiff(run1Tools, run2Tools)
	assert.Nil(t, result, "Should return nil when no bash tools present")
}

func TestComputeBashCommandsDiff_GenericBash(t *testing.T) {
	t.Parallel()
	// Only bash tools are passed to computeBashCommandsDiff
	run1Tools := map[string]ToolCallInfo{
		"bash": {Name: "bash", CallCount: 5},
	}
	run2Tools := map[string]ToolCallInfo{
		"bash": {Name: "bash", CallCount: 10},
	}

	result := computeBashCommandsDiff(run1Tools, run2Tools)
	require.NotNil(t, result, "Should produce bash diff when bash tool present")

	assert.Equal(t, 5, result.Run1TotalCalls, "Run1 total bash calls should be 5")
	assert.Equal(t, 10, result.Run2TotalCalls, "Run2 total bash calls should be 10")
	assert.Equal(t, "+5", result.TotalCallsChange, "Total change should be +5")

	require.Len(t, result.Commands, 1, "Should have 1 command (bash)")
	assert.Equal(t, "bash", result.Commands[0].Name, "Command should be bash")
	assert.Equal(t, "changed", result.Commands[0].Status, "bash status should be changed")
	assert.Equal(t, "+5", result.Commands[0].CallCountChange, "bash change should be +5")
}

func TestComputeBashCommandsDiff_PerCommandTracking(t *testing.T) {
	t.Parallel()
	// Codex-style per-command bash tracking
	run1Tools := map[string]ToolCallInfo{
		"bash_git_status": {Name: "bash_git_status", CallCount: 3},
		"bash_ls_la":      {Name: "bash_ls_la", CallCount: 1},
		"bash_cat_readme": {Name: "bash_cat_readme", CallCount: 2},
	}
	run2Tools := map[string]ToolCallInfo{
		"bash_git_status": {Name: "bash_git_status", CallCount: 5},
		"bash_git_diff":   {Name: "bash_git_diff", CallCount: 4},
		"bash_cat_readme": {Name: "bash_cat_readme", CallCount: 2},
	}

	result := computeBashCommandsDiff(run1Tools, run2Tools)
	require.NotNil(t, result, "Should produce bash diff")

	assert.Equal(t, 6, result.Run1TotalCalls, "Run1 total: 3+1+2=6")
	assert.Equal(t, 11, result.Run2TotalCalls, "Run2 total: 5+4+2=11")
	assert.Equal(t, "+5", result.TotalCallsChange, "Total change should be +5")

	require.Len(t, result.Commands, 4, "Should have 4 commands (ls removed, diff added, status/cat changed/unchanged)")

	statusMap := make(map[string]string)
	for _, cmd := range result.Commands {
		statusMap[cmd.Name] = cmd.Status
	}
	assert.Equal(t, "changed", statusMap["bash_git_status"], "git_status should be changed")
	assert.Equal(t, "removed", statusMap["bash_ls_la"], "ls_la should be removed")
	assert.Equal(t, "unchanged", statusMap["bash_cat_readme"], "cat_readme should be unchanged")
	assert.Equal(t, "new", statusMap["bash_git_diff"], "git_diff should be new")
}

func TestComputeBashCommandsDiff_BashCapitalized(t *testing.T) {
	t.Parallel()
	// Claude uses "Bash" (capitalized)
	run1Tools := map[string]ToolCallInfo{
		"Bash": {Name: "Bash", CallCount: 7},
	}
	run2Tools := map[string]ToolCallInfo{
		"Bash": {Name: "Bash", CallCount: 4},
	}

	result := computeBashCommandsDiff(run1Tools, run2Tools)
	require.NotNil(t, result, "Should detect capitalized Bash tool")
	assert.Equal(t, 7, result.Run1TotalCalls, "Run1 total should be 7")
	assert.Equal(t, 4, result.Run2TotalCalls, "Run2 total should be 4")
}

// --- Tokens per turn tests ---

func TestComputeRunMetricsDiff_TokensPerTurn(t *testing.T) {
	t.Parallel()
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{
			TokenUsage: 10000,
			Turns:      5,
		},
	},
	}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{
			TokenUsage: 18000,
			Turns:      6,
		},
	},
	}

	diff := computeRunMetricsDiff(summary1, summary2)
	require.NotNil(t, diff, "Should produce metrics diff")

	// Uses engine token counts for tokens/turn.
	assert.Equal(t, 2000, diff.Run1TokensPerTurn, "Run1 tokens/turn should be 10000/5=2000")
	assert.Equal(t, 3000, diff.Run2TokensPerTurn, "Run2 tokens/turn should be 18000/6=3000")
	assert.Equal(t, "+50%", diff.TokensPerTurnChange, "Tokens/turn should increase by 50%")
}

func TestComputeRunMetricsDiff_TokensPerTurnZeroTurns(t *testing.T) {
	t.Parallel()
	// When turns = 0, tokens per turn should remain 0 (no division)
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{
			TokenUsage: 5000,
			Turns:      0,
		},
	},
	}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{
			TokenUsage: 8000,
			Turns:      4,
		},
	},
	}

	diff := computeRunMetricsDiff(summary1, summary2)
	require.NotNil(t, diff, "Should produce metrics diff")

	assert.Equal(t, 0, diff.Run1TokensPerTurn, "Run1 tokens/turn should be 0 when turns=0")
	assert.Equal(t, 2000, diff.Run2TokensPerTurn, "Run2 tokens/turn should be 8000/4=2000")
}

// --- Tool calls diff in RunMetricsDiff integration test ---

func TestComputeRunMetricsDiff_WithToolCallsDiff(t *testing.T) {
	t.Parallel()
	m1 := LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 5},
			{Name: "gh", CallCount: 3},
		},
		Turns: 4,
	}
	m2 := LogMetrics{
		ToolCalls: []ToolCallInfo{
			{Name: "bash", CallCount: 12},
			{Name: "gh", CallCount: 3},
			{Name: "edit", CallCount: 4},
		},
		Turns: 6,
	}
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{
		Run:     WorkflowRun{TokenUsage: 5000, Turns: 4},
		Metrics: m1,
	},
	}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{
		Run:     WorkflowRun{TokenUsage: 9000, Turns: 6},
		Metrics: m2,
	},
	}

	diff := computeRunMetricsDiff(summary1, summary2)
	require.NotNil(t, diff, "Should produce metrics diff")
	require.NotNil(t, diff.ToolCallsDiff, "Should include tool calls diff")

	assert.Len(t, diff.ToolCallsDiff.NewTools, 1, "Should have 1 new tool (edit)")
	assert.Len(t, diff.ToolCallsDiff.ChangedTools, 1, "Should have 1 changed tool (bash)")
	assert.Empty(t, diff.ToolCallsDiff.RemovedTools, "Should have no removed tools")

	require.NotNil(t, diff.ToolCallsDiff.BashDiff, "Should have bash diff")
	assert.Equal(t, 5, diff.ToolCallsDiff.BashDiff.Run1TotalCalls, "Bash run1 total should be 5")
	assert.Equal(t, 12, diff.ToolCallsDiff.BashDiff.Run2TotalCalls, "Bash run2 total should be 12")
}

// findToolCallDiffEntry is a test helper to find a tool entry by name
func findToolCallDiffEntry(entries []ToolCallDiffEntry, name string) *ToolCallDiffEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}

func TestComputeRunMetricsDiffIncludesWorkingSetRebuild(t *testing.T) {
	t.Parallel()
	run1Factor := 2.0
	run2Factor := 3.9
	summary1 := &RunSummary{RunAnalysis: RunAnalysis{
		WorkingSet: &WorkingSetMetrics{MeasurementState: "measured", RebuildFactor: &run1Factor},
	}}
	summary2 := &RunSummary{RunAnalysis: RunAnalysis{
		WorkingSet: &WorkingSetMetrics{MeasurementState: "measured", RebuildFactor: &run2Factor},
	}}

	diff := computeRunMetricsDiff(summary1, summary2)
	require.NotNil(t, diff)
	require.NotNil(t, diff.Run1WorkingSetRebuild)
	require.NotNil(t, diff.Run2WorkingSetRebuild)
	assert.InDelta(t, 2.0, *diff.Run1WorkingSetRebuild, 1e-12)
	assert.InDelta(t, 3.9, *diff.Run2WorkingSetRebuild, 1e-12)
	assert.Equal(t, "+95.0%", diff.WorkingSetRebuildDelta)
}
