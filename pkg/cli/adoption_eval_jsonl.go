package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"
)

// writeAdoptionJSONL writes adoption reports as JSONL to the given directory.
// Each line is a JSON object suitable for OTLP span conversion or downstream processing.
func writeAdoptionJSONL(dir string, runID int64, reports []AdoptionReport) (err error) {
	if err := os.MkdirAll(dir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("create adoptions directory: %w", err)
	}

	filePath := filepath.Join(dir, fmt.Sprintf("adoptions-%d.jsonl", runID))
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create adoptions file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close adoptions file: %w", closeErr)
		}
	}()

	for _, r := range reports {
		line, err := json.Marshal(adoptionJSONLEntry(runID, len(reports), r))
		if err != nil {
			return fmt.Errorf("encode adoption observation: %w", err)
		}
		if _, err := f.Write(line); err != nil {
			return fmt.Errorf("write adoption observation: %w", err)
		}
		if _, err := f.WriteString("\n"); err != nil {
			return fmt.Errorf("write adoption observation: %w", err)
		}
	}

	adoptionEvalLog.Printf("Wrote %d adoption entries to %s", len(reports), filePath)
	return nil
}

func adoptionJSONLEntry(runID int64, runItemCount int, report AdoptionReport) map[string]any {
	eval := normalizeAdoptionEvaluation(report)
	entry := map[string]any{
		"run_id": runID, "item_id": report.ItemID, "run_item_count": runItemCount,
		"objective_value": report.ObjectiveValue, "objective_labels": report.ObjectiveLabels,
		"type": report.Type, "adoption_status": eval.AdoptionStatus,
		"evidence_strength": eval.EvidenceStrength, "signal": eval.Signal, "source": eval.Source,
		"rule_version": eval.RuleVersion, "effect_class": eval.EffectClass,
		"fallback_to_delivery": eval.FallbackToDelivery, "attribution_eligible": eval.AttributionEligible,
		"survival_required": eval.SurvivalRequired, "adoption_at": eval.AdoptionAt,
		"acceptance_kind": eval.AcceptanceKind, "detail": report.Detail,
		"object_url": report.ObjectURL, "object_number": report.ObjectNumber, "repo": report.Repo,
		"time_to_adoption_hours": report.TimeToAdoptionHours,
		"human_comments":         report.HumanComments, "human_edits": report.HumanEdits,
		"human_reviews": report.HumanReviews, "zero_touch": report.ZeroTouch,
		"zero_touch_evidence_complete": report.ZeroTouchEvidenceComplete,
		"created_at":                   report.CreatedAt, "checked_at": report.CheckedAt,
	}
	if report.Delivery != nil {
		entry["delivery"] = report.Delivery
	}
	return entry
}
