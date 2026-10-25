//go:build !integration

package cli

import (
	"testing"
)

func TestSummarizeDependencyVersions(t *testing.T) {
	deps := []DependencyInfoWithIndirect{
		{DependencyInfo: DependencyInfo{Version: "v0.9.0"}},
		{DependencyInfo: DependencyInfo{Version: "v1.2.0"}, Indirect: true},
		{DependencyInfo: DependencyInfo{Version: "v2.1.0"}},
		{DependencyInfo: DependencyInfo{Version: "v3.1.0"}, Indirect: true},
		{DependencyInfo: DependencyInfo{Version: "v4.0.0"}},
	}
	report := summarizeDependencyVersions(deps)
	if report.TotalDeps != 5 || report.DirectDeps != 3 || report.IndirectDeps != 2 {
		t.Fatalf("unexpected dependency counts: %+v", report)
	}
	if report.V0Count != 1 || report.V1PlusCount != 1 || report.V2PlusCount != 2 {
		t.Fatalf("unexpected maturity counts: %+v", report)
	}
	if report.Outdated != nil || report.Advisories != nil {
		t.Fatal("summarizing versions must not populate remote results")
	}
}

func TestDependencyReportRecommendationsEmpty(t *testing.T) {
	recommendations := dependencyReportRecommendations(&DependencyReport{}, 30)
	if recommendations == nil || len(recommendations) != 0 {
		t.Fatalf("expected non-nil empty recommendations: %v", recommendations)
	}
}
