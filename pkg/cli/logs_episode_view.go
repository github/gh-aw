package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

func validateEpisodeView(view string) error {
	if view != "summary" && view != "hops" {
		return fmt.Errorf("invalid --view %q: use summary or hops", view)
	}
	return nil
}

func renderEpisodeView(out io.Writer, data LogsData, view string, jsonOutput bool) error {
	if err := validateEpisodeView(view); err != nil {
		return err
	}
	if jsonOutput {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	}
	if retrieval := data.EpisodeRetrieval; retrieval != nil && retrieval.Cached {
		status := "no GitHub requests"
		if data.Outcomes != nil {
			status = "membership reused; adoption checked live"
		}
		if _, err := fmt.Fprintf(out, "Cached episode snapshot from %s; %s. Use --refresh to collect again.\n", retrieval.CollectedAt.Format(time.RFC3339), status); err != nil {
			return fmt.Errorf("write episode cache status: %w", err)
		}
	}
	if view == "hops" {
		return renderEpisodeHops(out, data)
	}
	return renderEpisodeRetrieval(out, data)
}

func renderEpisodeHops(out io.Writer, data LogsData) error {
	var text []byte
	for _, episode := range data.Episodes {
		view := episode.Explanation
		if view == nil {
			view = explainEpisode(episode, data.Runs, data.Edges)
		}
		text = fmt.Appendf(text, "Episode %s\nCoverage: %s (%s); complete membership is not established.\n", episode.EpisodeID, episode.Coverage.Status, episode.Coverage.Scope)
		for _, hop := range view.Hops {
			label := hop.HopID
			if label == "" {
				label = "(hop ID not recorded)"
			}
			text = fmt.Appendf(text, "  Hop %s: run %d, %s, workflow %s", label, hop.RunID, hop.Repository, hop.Workflow)
			if hop.SharedRun {
				text = append(text, " [shared run]"...)
			}
			text = append(text, '\n')
			for _, gap := range hop.MissingEvidence {
				text = fmt.Appendf(text, "    Missing evidence: %s\n", gap)
			}
		}
		for _, edge := range view.Edges {
			text = fmt.Appendf(text, "  Hop edge %s -> %s: %s", edge.SourceHopID, edge.TargetHopID, edge.Status)
			if edge.Reason != "" {
				text = fmt.Appendf(text, " (%s)", edge.Reason)
			}
			text = append(text, '\n')
		}
		for _, edge := range view.RunEdges {
			text = fmt.Appendf(text, "  Recorded run edge %d -> %d: %s\n", edge.SourceRunID, edge.TargetRunID, edge.EdgeType)
		}
		for _, gap := range append(append([]string{}, episode.Coverage.Reasons...), view.MissingEvidence...) {
			text = fmt.Appendf(text, "  Missing evidence: %s\n", gap)
		}
		text = append(text, "Run-level accounting (observed once per run; not allocated to hops):\n"...)
		for _, cost := range view.RunCosts {
			text = fmt.Appendf(text, "  Run %d: %d tokens, %.6g AI credits\n", cost.RunID, cost.Tokens, cost.AIC)
		}
		text = append(text, '\n')
	}
	if _, err := io.Copy(out, bytes.NewReader(text)); err != nil {
		return fmt.Errorf("write episode explanation: %w", err)
	}
	return nil
}

func renderLogsEpisodeOutcomes(out io.Writer, data LogsData, episodes bool) error {
	if episodes {
		if len(data.Episodes) == 0 {
			if _, err := fmt.Fprintln(out, "\nNo recorded episodes in the selected runs; episode membership is unavailable."); err != nil {
				return fmt.Errorf("write episode coverage: %w", err)
			}
		} else if err := renderEpisodeHops(out, data); err != nil {
			return err
		}
	}
	return renderLogsOutcomes(out, data)
}
