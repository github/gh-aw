//go:build !integration

package cli

import (
	"strings"
	"testing"
)

func TestRenderSingleAuditDiffPrettySectionOrder(t *testing.T) {
	diff := &AuditDiff{
		Run1ID: 1, Run2ID: 2,
		FirewallDiff: &FirewallDiff{
			NewDomains:     []DomainDiffEntry{{Domain: "new.example", Run2Allowed: 1}},
			RemovedDomains: []DomainDiffEntry{{Domain: "removed.example", Run1Blocked: 2}},
			StatusChanges:  []DomainDiffEntry{{Domain: "status.example", Run1Status: "allowed", Run2Status: "denied"}},
			VolumeChanges:  []DomainDiffEntry{{Domain: "volume.example", Run1Allowed: 1, Run2Allowed: 4, VolumeChange: "+300%"}},
			Summary:        FirewallDiffSummary{AnomalyCount: 2},
		},
		MCPToolsDiff: &MCPToolsDiff{
			NewTools:     []MCPToolDiffEntry{{ServerName: "server", ToolName: "new-tool", Run2CallCount: 1}},
			RemovedTools: []MCPToolDiffEntry{{ServerName: "server", ToolName: "old-tool", Run1CallCount: 2}},
			ChangedTools: []MCPToolDiffEntry{{ServerName: "server", ToolName: "changed-tool", Run1CallCount: 1, Run2CallCount: 2}},
			Summary:      MCPToolsDiffSummary{NewToolCount: 1, RemovedToolCount: 1, ChangedToolCount: 1},
		},
		RunMetricsDiff: &RunMetricsDiff{
			Run1TokenUsage: 1, Run2TokenUsage: 2,
			TokenUsageDetails: &TokenUsageDiff{Run1InputTokens: 1, Run2OutputTokens: 2},
		},
	}
	stdout, stderr := captureOutput(t, func() error {
		renderSingleAuditDiffPretty(diff)
		return nil
	})
	if stdout != "" {
		t.Fatalf("pretty output wrote to stdout: %q", stdout)
	}
	position := -1
	for _, section := range []string{"Changes:", "2 anomalies detected", "Firewall Changes", "New Domains (1)", "Removed Domains (1)", "Status Changes (1)", "Volume Changes", "MCP Tool Changes", "New Tools (1)", "Removed Tools (1)", "Changed Tools (1)", "Run Metrics", "Token Usage Details"} {
		next := strings.Index(stderr, section)
		if next <= position {
			t.Fatalf("missing or out-of-order section %q in %q", section, stderr)
		}
		position = next
	}
}

func TestRenderSingleAuditDiffPrettyEmpty(t *testing.T) {
	stdout, stderr := captureOutput(t, func() error {
		renderSingleAuditDiffPretty(&AuditDiff{Run1ID: 1, Run2ID: 2})
		return nil
	})
	if stdout != "" || !strings.Contains(stderr, "No behavioral changes detected") || strings.Contains(stderr, "Firewall Changes") {
		t.Fatalf("unexpected empty diff output: stdout=%q, stderr=%q", stdout, stderr)
	}
}
