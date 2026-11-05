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
func writeOutcomeJSONL(dir string, runID int64, reports []OutcomeReport) (err error) {
	if err := os.MkdirAll(dir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("create outcomes directory: %w", err)
	}

	filePath := filepath.Join(dir, fmt.Sprintf("outcomes-%d.jsonl", runID))
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create outcomes file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close outcomes file: %w", closeErr)
		}
	}()

	for _, r := range reports {
		eval := normalizeOutcomeEvaluation(r)
		entry := map[string]any{
			"run_id":                       runID,
			"item_id":                      r.ItemID,
			"run_item_count":               len(reports),
			"objective_value":              r.ObjectiveValue,
			"objective_labels":             r.ObjectiveLabels,
			"type":                         r.Type,
			"outcome_status":               eval.OutcomeStatus,
			"evidence_strength":            eval.EvidenceStrength,
			"signal":                       eval.Signal,
			"detail":                       r.Detail,
			"object_url":                   r.ObjectURL,
			"object_number":                r.ObjectNumber,
			"repo":                         r.Repo,
			"time_to_outcome_hours":        r.TimeToOutcomeHours,
			"human_comments":               r.HumanComments,
			"human_edits":                  r.HumanEdits,
			"human_reviews":                r.HumanReviews,
			"zero_touch":                   r.ZeroTouch,
			"zero_touch_evidence_complete": r.ZeroTouchEvidenceComplete,
			"created_at":                   r.CreatedAt,
			"checked_at":                   r.CheckedAt,
		}
		line, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encode outcome observation: %w", err)
		}
		if _, err := f.Write(line); err != nil {
			return fmt.Errorf("write outcome observation: %w", err)
		}
		if _, err := f.WriteString("\n"); err != nil {
			return fmt.Errorf("write outcome observation: %w", err)
		}
	}

	outcomeEvalLog.Printf("Wrote %d outcome entries to %s", len(reports), filePath)
	return nil
}
