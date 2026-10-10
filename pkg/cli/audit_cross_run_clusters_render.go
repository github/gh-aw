package cli

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/timeutil"
)

// renderMarkdownClusterAnalysisToWriter outputs cluster analysis as Markdown.
func renderMarkdownClusterAnalysisToWriter(w io.Writer, ca *ClusterAnalysis) {
	if ca == nil {
		return
	}

	if len(ca.Patterns) > 0 {
		fmt.Fprintln(w, "## Cluster Patterns")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "| Severity | Kind | Title | Description |\n")
		fmt.Fprintf(w, "|----------|------|-------|-------------|\n")
		for _, p := range ca.Patterns {
			fmt.Fprintf(w, "| %s %s | %s | %s | %s |\n",
				renderSeverityIcon(p.Severity), p.Severity, p.Kind, p.Title, p.Description)
		}
		fmt.Fprintln(w)
	}

	if len(ca.Clusters) > 0 {
		fmt.Fprintln(w, "## Run Clusters")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "| Dimension | Value | Runs | Avg Tokens | Avg Turns | Avg Duration | Success Rate |\n")
		fmt.Fprintf(w, "|-----------|-------|------|------------|-----------|--------------|-------------|\n")
		for _, c := range ca.Clusters {
			durStr := "—"
			if c.Metrics.AvgDurationNs > 0 {
				durStr = timeutil.FormatDurationNs(c.Metrics.AvgDurationNs)
			}
			fmt.Fprintf(w, "| %s | %s | %d | %d | %.1f | %s | %.0f%% |\n",
				c.Dimension, c.Value, c.Count,
				c.Metrics.AvgTokens, c.Metrics.AvgTurns, durStr,
				c.Metrics.SuccessRate*100)
		}
		fmt.Fprintln(w)
	}
}

// renderPrettyClusterAnalysis outputs cluster analysis as console output.
func renderPrettyClusterAnalysis(ca *ClusterAnalysis) {
	if ca == nil {
		return
	}

	if len(ca.Patterns) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Cluster Patterns (%d detected)", len(ca.Patterns))))
		for _, p := range ca.Patterns {
			fmt.Fprintf(os.Stderr, "  %s [%s/%s] %s\n", renderSeverityIcon(p.Severity), p.Kind, p.Severity, p.Title)
			fmt.Fprintf(os.Stderr, "     %s\n", p.Description)
			if p.Evidence != "" {
				fmt.Fprintf(os.Stderr, "     evidence: %s\n", p.Evidence)
			}
		}
		fmt.Fprintln(os.Stderr)
	}

	if len(ca.Clusters) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run Clusters (%d groups)", len(ca.Clusters))))
		// Group by dimension for display
		dimOrder := clusterDimensionOrder(ca.Clusters)
		for _, dim := range dimOrder {
			fmt.Fprintf(os.Stderr, "  [%s]\n", dim)
			for _, c := range ca.Clusters {
				if c.Dimension != dim {
					continue
				}
				durStr := ""
				if c.Metrics.AvgDurationNs > 0 {
					durStr = "  dur=" + timeutil.FormatDurationNs(c.Metrics.AvgDurationNs)
				}
				fmt.Fprintf(os.Stderr, "    %-20s  runs=%d  tokens=%d  turns=%.1f%s  success=%.0f%%\n",
					c.Value, c.Count, c.Metrics.AvgTokens, c.Metrics.AvgTurns,
					durStr, c.Metrics.SuccessRate*100)
			}
		}
		fmt.Fprintln(os.Stderr)
	}
}

// clusterDimensionOrder returns unique dimensions in deterministic sorted order.
func clusterDimensionOrder(clusters []RunCluster) []string {
	seen := make(map[string]struct{})
	var dims []string
	for _, c := range clusters {
		if _, ok := seen[c.Dimension]; !ok {
			seen[c.Dimension] = struct{}{}
			dims = append(dims, c.Dimension)
		}
	}
	sort.Strings(dims)
	return dims
}
