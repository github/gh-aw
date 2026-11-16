package cli

import (
	"fmt"
	"io"
	"strings"
)

func renderEpisodeAdoptions(output io.Writer, data EpisodeAdoptionsData) error {
	var lines []string
	for _, episode := range data.Episodes {
		lines = append(lines, fmt.Sprintf("\nEpisode %s (selected runs: %v)", episode.EpisodeID, episode.RunIDs))
		lines = append(lines, fmt.Sprintf("  Observed adoptions: %d accepted, %d rejected, %d lifecycle, %d pending, %d not applicable, %d unknown, %d errors (%d total)",
			episode.Summary.Accepted, episode.Summary.Rejected, episode.Summary.Lifecycle, episode.Summary.Pending,
			episode.Summary.NotApplicable, episode.Summary.Unknown, episode.Summary.Errors, episode.Summary.Total))
		lines = append(lines, fmt.Sprintf("  Adoption evidence: %s (%d/%d selected runs)", episode.AdoptionCoverage, len(episode.AdoptionRunIDs), len(episode.RunIDs)))
		if episode.ObservedAIC == nil {
			lines = append(lines, "  Observed AIC: unavailable")
		} else {
			lines = append(lines, fmt.Sprintf("  Observed AIC: %.3f (%s cost coverage, %d/%d selected runs)",
				*episode.ObservedAIC, episode.CostCoverage, len(episode.CostRunIDs), len(episode.RunIDs)))
		}
		if episode.CostPerAcceptedAdoption == nil {
			lines = append(lines, "  AIC per accepted adoption: unavailable")
		} else {
			lines = append(lines, fmt.Sprintf("  AIC per accepted adoption (selected runs): %.3f", *episode.CostPerAcceptedAdoption))
		}
		for _, qualification := range episode.Qualifications {
			lines = append(lines, "  Note: "+qualification)
		}
	}
	if len(data.UnmatchedAdoptionRunIDs) > 0 {
		lines = append(lines, fmt.Sprintf("\nAdoption source runs outside selected membership (not counted): %v", data.UnmatchedAdoptionRunIDs))
	}
	if _, err := fmt.Fprintln(output, strings.Join(lines, "\n")); err != nil {
		return fmt.Errorf("failed to write episode adoptions: %w", err)
	}
	return nil
}
