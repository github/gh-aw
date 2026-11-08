package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// Handled token rejections leave successful steps, so audit the status artifact too.
func generateEvalsAuthFindings(run WorkflowRun) []AuditFinding {
	if run.LogsPath == "" || run.DatabaseID == 0 || run.Attempt == 0 {
		return nil
	}
	statusName := constants.EvalsStatusFilename.String()
	candidates := []string{filepath.Join(run.LogsPath, statusName)}
	entries, err := os.ReadDir(run.LogsPath)
	if err != nil {
		auditEngineStepsLog.Printf("Cannot inspect evals status artifacts: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if evalsArtifactNamePattern.MatchString(name) {
			candidates = append(candidates, filepath.Join(run.LogsPath, name, statusName))
		} else if evalsArtifactNamePattern.MatchString(strings.TrimSuffix(name, "usage")+"evals") && strings.HasSuffix(name, "usage") {
			candidates = append(candidates, filepath.Join(run.LogsPath, name, "evals", statusName))
		}
	}
	for _, path := range candidates {
		data, err := readFileLimited(path, 64*1024)
		if err != nil {
			continue
		}
		var marker struct {
			Status     string `json:"status"`
			ReasonCode string `json:"reason_code"`
			RunID      string `json:"run_id"`
			RunAttempt string `json:"run_attempt"`
		}
		if json.Unmarshal(data, &marker) != nil || marker.Status != "not_evaluated" || marker.ReasonCode != "auth_unsupported" ||
			marker.RunID != strconv.FormatInt(run.DatabaseID, 10) || marker.RunAttempt != strconv.Itoa(run.Attempt) {
			continue
		}
		return []AuditFinding{{
			Code:        AuditFindingEvalsAuthUnsupported,
			Category:    "error",
			Severity:    "high",
			Title:       "Evals Authentication Unsupported",
			Description: "Evals was not evaluated (auth_unsupported): the Decisions endpoint rejected a personal access token. Use a credential supported by the endpoint, configure an OpenAI-backed evals.model, or remove evals. Changing the Copilot model will not fix authentication.",
			Impact:      "The evals job can report success without judging; no answers were recorded",
		}}
	}
	return nil
}
