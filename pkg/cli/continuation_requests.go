package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func continuationRequestCount(requests []ContinuationRequestReport) *int {
	if requests == nil {
		return nil
	}
	count := len(requests)
	return &count
}

func backfillContinuationRequests(requests *[]ContinuationRequestReport, run WorkflowRun, runDir string, verbose bool) bool {
	if !continuationEvidenceMatchesRun(run, runDir, verbose) {
		*requests = nil
		return false
	}
	if *requests != nil {
		return false
	}
	experiment, variant, _ := firstExperimentAssignment(extractExperimentData(runDir))
	*requests, _ = extractContinuationRequestsFromRun(runDir, run, verbose, experiment, variant)
	return *requests != nil
}

func continuationEvidenceIdentityChanged(cachedRun, currentRun WorkflowRun) bool {
	if cachedRun.DatabaseID <= 0 || currentRun.DatabaseID <= 0 ||
		cachedRun.Attempt <= 0 || currentRun.Attempt <= 0 ||
		cachedRun.Repository == "" || currentRun.Repository == "" ||
		cachedRun.UpdatedAt.IsZero() || currentRun.UpdatedAt.IsZero() {
		return false
	}
	return cachedRun.DatabaseID != currentRun.DatabaseID ||
		cachedRun.Attempt != currentRun.Attempt ||
		!strings.EqualFold(cachedRun.Repository, currentRun.Repository) ||
		!cachedRun.UpdatedAt.Equal(currentRun.UpdatedAt)
}

func invalidateContinuationArtifactEvidence(runDir string) error {
	if err := invalidateAgentOutputDownloadMarkers(runDir); err != nil {
		return err
	}
	markerDir := filepath.Join(runDir, downloadedArtifactsMarkerDir)
	return filepath.WalkDir(runDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path == markerDir {
			return fs.SkipDir
		}
		if entry.IsDir() {
			if path == runDir ||
				(!artifactNameMatchesBase(entry.Name(), constants.AgentArtifactName.String()) &&
					!artifactNameMatchesBase(entry.Name(), constants.AgentOutputFallbackArtifactName.String())) {
				return nil
			}
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("failed to remove stale continuation artifact directory %s: %w", path, err)
			}
			return fs.SkipDir
		}
		if entry.Name() != constants.AgentOutputFilename.String() {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to remove stale continuation evidence %s: %w", path, err)
		}
		return nil
	})
}

func continuationEvidenceMatchesRun(run WorkflowRun, runDir string, verbose bool) bool {
	if run.DatabaseID <= 0 || run.Attempt <= 0 || run.Repository == "" || run.UpdatedAt.IsZero() {
		return false
	}
	summary, ok := loadRunSummary(runDir, verbose)
	if !ok || summary == nil || summary.RunID != run.DatabaseID ||
		summary.Run.DatabaseID != run.DatabaseID ||
		summary.Run.Attempt != run.Attempt ||
		!strings.EqualFold(summary.Run.Repository, run.Repository) ||
		!summary.Run.UpdatedAt.Equal(run.UpdatedAt) ||
		summary.ProcessedAt.IsZero() || summary.ProcessedAt.Before(summary.Run.UpdatedAt) {
		return false
	}
	return true
}

func mergeContinuationRequestsIntoAudit(auditData *AuditData, requests []ContinuationRequestReport) bool {
	if auditData == nil || requests == nil {
		return false
	}
	changed := auditData.ContinuationRequests == nil || !slices.Equal(auditData.ContinuationRequests, requests)
	auditData.ContinuationRequests = requests
	count := len(requests)
	if auditData.SessionAnalysis == nil {
		auditData.SessionAnalysis = &SessionAnalysis{}
		changed = true
	}
	if auditData.SessionAnalysis.ContinuationRequestCount == nil || *auditData.SessionAnalysis.ContinuationRequestCount != count {
		auditData.SessionAnalysis.ContinuationRequestCount = &count
		changed = true
	}
	return changed
}

func renderTSVContinuationRequestSummary(w io.Writer, summary LogsSummary) {
	if summary.TotalContinuationRequests > 0 || summary.ContinuationRequestsUnavailableRuns > 0 {
		fmt.Fprintf(w, "# continuation_requests=%d continuation_requested_runs=%d continuation_requests_unavailable_runs=%d (requests only; no automatic dispatch or resume)\n",
			summary.TotalContinuationRequests, summary.ContinuationRequestedRuns, summary.ContinuationRequestsUnavailableRuns)
	}
}

func renderContinuationRequests(w io.Writer, requests []ContinuationRequestReport) error {
	if len(requests) == 0 {
		return nil
	}
	out := &compactLogsWriter{writer: w}
	out.printf("[continuation_requests] requests=%d (intent only; no automatic dispatch or resume)\n", len(requests))
	for _, request := range requests {
		out.printf("run=%d workflow=%q message=%q timestamp=%q", request.RunID, request.WorkflowName, request.Message, request.Timestamp)
		if request.ExperimentName != "" {
			out.printf(" experiment=%q variant=%q", request.ExperimentName, request.Variant)
		}
		out.println()
	}
	return out.err
}
