package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"
)

func writeOutcomeJSONL(dir string, runID int64, reports []OutcomeReport) error {
	if err := os.MkdirAll(dir, constants.DirPermSensitive); err != nil {
		return fmt.Errorf("create adoption directory: %w", err)
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	for _, report := range reports {
		report.OutcomeEvaluation = normalizeOutcomeEvaluation(report)
		if err := encoder.Encode(EpisodeOutcomeObservation{
			RunID: runID, RunItemCount: len(reports), OutcomeReport: report,
		}); err != nil {
			return fmt.Errorf("encode adoption observation: %w", err)
		}
	}
	filePath := filepath.Join(dir, fmt.Sprintf("adoption-%d.jsonl", runID))
	if err := writeSessionFileAtomically(filePath, buffer.Bytes()); err != nil {
		return fmt.Errorf("write adoption observations: %w", err)
	}
	return nil
}
