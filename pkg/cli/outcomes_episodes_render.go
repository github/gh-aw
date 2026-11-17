package cli

import (
	"fmt"
	"io"
	"strings"
)

func renderEpisodeOutcomes(output io.Writer, data EpisodeOutcomesData) error {
	var lines []string
	for _, episode := range data.Episodes {
		lines = append(lines, fmt.Sprintf("\nEpisode %s (selected runs: %v)", episode.EpisodeID, episode.RunIDs))
		lines = append(lines, fmt.Sprintf("  Observed adoption: %d accepted, %d rejected, %d not applicable, %d lifecycle, %d pending, %d unknown, %d errors (%d total)",
			episode.Summary.Accepted, episode.Summary.Rejected, episode.Summary.NotApplicable, episode.Summary.Lifecycle, episode.Summary.Pending,
			episode.Summary.Unknown, episode.Summary.Errors, episode.Summary.Total))
		lines = append(lines, fmt.Sprintf("  Adoption evidence: %s (%d/%d selected runs)", episode.OutcomeCoverage, len(episode.OutcomeRunIDs), len(episode.RunIDs)))
		if episode.ObservedAIC == nil {
			lines = append(lines, "  Observed AIC: unavailable")
		} else {
			lines = append(lines, fmt.Sprintf("  Observed AIC: %.3f (%s cost coverage, %d/%d selected runs)",
				*episode.ObservedAIC, episode.CostCoverage, len(episode.CostRunIDs), len(episode.RunIDs)))
		}
		if episode.CostPerAcceptedOutcome == nil {
			lines = append(lines, "  AIC per adopted output: unavailable")
		} else {
			lines = append(lines, fmt.Sprintf("  AIC per adopted output (selected runs): %.3f", *episode.CostPerAcceptedOutcome))
		}
		for _, qualification := range episode.Qualifications {
			lines = append(lines, "  Note: "+qualification)
		}
	}
	if len(data.UnmatchedOutcomeRunIDs) > 0 {
		lines = append(lines, fmt.Sprintf("\nAdoption source runs outside selected membership (not counted): %v", data.UnmatchedOutcomeRunIDs))
	}
	if _, err := fmt.Fprintln(output, strings.Join(lines, "\n")); err != nil {
		return fmt.Errorf("failed to write episode adoption: %w", err)
	}
	return nil
}
