package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"
)

// writeOutcomeJSONL writes outcome reports as JSONL to the given directory.
// Each line is a JSON object suitable for OTLP span conversion or downstream processing.
func writeOutcomeJSONL(dir string, runID int64, reports []OutcomeReport) {
	if err := os.MkdirAll(dir, constants.DirPermPublic); err != nil {
		outcomeEvalLog.Printf("Failed to create outcomes dir %s: %v", dir, err)
		return
	}

	filePath := filepath.Join(dir, fmt.Sprintf("outcomes-%d.jsonl", runID))
	f, err := os.Create(filePath)
	if err != nil {
		outcomeEvalLog.Printf("Failed to create outcomes file %s: %v", filePath, err)
		return
	}
	defer f.Close()

	for _, r := range reports {
		eval := normalizeOutcomeEvaluation(r)
		entry := map[string]any{
			"run_id":                runID,
			"type":                  r.Type,
			"outcome_status":        eval.OutcomeStatus,
			"evidence_strength":     eval.EvidenceStrength,
			"signal":                eval.Signal,
			"detail":                r.Detail,
			"object_url":            r.ObjectURL,
			"object_number":         r.ObjectNumber,
			"repo":                  r.Repo,
			"time_to_outcome_hours": r.TimeToOutcomeHours,
			"human_comments":        r.HumanComments,
			"human_edits":           r.HumanEdits,
			"human_reviews":         r.HumanReviews,
			"zero_touch":            r.ZeroTouch,
			"created_at":            r.CreatedAt,
			"checked_at":            r.CheckedAt,
		}
		line, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if _, err := f.Write(line); err != nil {
			outcomeEvalLog.Printf("Failed to write outcome entry to %s: %v", filePath, err)
			return
		}
		if _, err := f.WriteString("\n"); err != nil {
			outcomeEvalLog.Printf("Failed to write newline to %s: %v", filePath, err)
			return
		}
	}

	outcomeEvalLog.Printf("Wrote %d outcome entries to %s", len(reports), filePath)
}
