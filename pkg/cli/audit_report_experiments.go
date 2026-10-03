// This file provides command-line interface functionality for gh-aw.
// This file (audit_report_experiments.go) parses the experiment artifact uploaded by the
// activation job and exposes the A/B experiment assignment data for display in the
// audit and logs commands.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var experimentDataLog = logger.New("cli:audit_report_experiments")

// ExperimentData represents the A/B experiment assignments for a single workflow run.
// Unrelated to ExperimentInfo, which summarizes an experiment workflow for list output.
type ExperimentData struct {
	// Assignments maps each experiment name to the variant selected for this run.
	// e.g. {"caveman": "yes", "style": "concise"}
	Assignments map[string]string `json:"assignments"`

	// CumulativeCounts maps each experiment name to a per-variant invocation counter.
	// e.g. {"caveman": {"yes": 3, "no": 2}}
	CumulativeCounts map[string]map[string]int `json:"cumulative_counts,omitempty"`
}

// experimentStateDirCandidates returns the directories that may contain experiment state
// files: the run root (flattened artifact), the experiment artifact directory, and the
// experiment/ subdirectory of the compact usage artifact staged by the conclusion job.
// Prefixed artifact directories ("{prefix}-experiment" for regular workflows and
// "{hash}-usage" for workflow_call runs) are also included.
func experimentStateDirCandidates(logsPath string) []string {
	experimentDir := constants.ExperimentArtifactName.String()
	usageDir := constants.UsageArtifactName.String()
	candidates := []string{
		logsPath,
		filepath.Join(logsPath, experimentDir),
		filepath.Join(logsPath, usageDir, experimentDir),
	}
	entries, err := os.ReadDir(logsPath)
	if err != nil {
		return candidates
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "-"+experimentDir) {
			candidates = append(candidates, filepath.Join(logsPath, name))
		}
		if strings.HasSuffix(name, "-"+usageDir) {
			candidates = append(candidates, filepath.Join(logsPath, name, experimentDir))
		}
	}
	return candidates
}

// findExperimentStatePath returns the first existing experiment state path. The JSONL run
// ledger is preferred over the legacy JSON state in every candidate directory.
func findExperimentStatePath(logsPath string) string {
	if logsPath == "" {
		return ""
	}
	dirs := experimentStateDirCandidates(logsPath)
	for _, filename := range []string{"state.jsonl", "state.json"} {
		for _, dir := range dirs {
			p := filepath.Join(dir, filename)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p
			}
		}
	}
	return ""
}

// extractExperimentData reads experiment state from the experiment artifact directory under
// logsPath and returns a populated ExperimentData or nil when no experiment artifact
// is present.
//
// When the state file contains a non-empty run ledger (written by pick_experiment.cjs
// v2+), the assignments of the most recent run record are returned directly.
// For legacy state files that only contain "counts" (no "runs" field), the selected
// variant is inferred by the max-count heuristic: the variant with the highest cumulative
// count is assumed to have been selected last (ties broken by sorted variant order).
//
// When no experiment state file is found, the function falls back to reading the usage
// activity summary (summary.json written by the conclusion job), which also includes
// experiment assignments since v2+.
func extractExperimentData(logsPath string) *ExperimentData {
	if logsPath == "" {
		return nil
	}

	experimentDataLog.Printf("Extracting experiment data from: %s", logsPath)

	statePath := findExperimentStatePath(logsPath)
	if statePath != "" {
		experimentDataLog.Printf("Reading experiment state from: %s", statePath)
		raw, err := os.ReadFile(statePath)
		if err == nil {
			if data := experimentDataFromState(parseExperimentState(raw)); data != nil {
				return data
			}
		}
	}

	// Fall back to the usage activity summary (written by the conclusion job).
	// This is available when the experiment artifact was not downloaded separately,
	// and the conclusion job was run with pick_experiment.cjs v2+ (JSONL ledger).
	usageSummary, err := loadUsageActivitySummary(logsPath)
	if err == nil && usageSummary != nil && usageSummary.Experiments != nil {
		if len(usageSummary.Experiments.Assignments) > 0 {
			experimentDataLog.Printf("Loaded experiment assignments from usage activity summary (%d experiment(s))", len(usageSummary.Experiments.Assignments))
			return &ExperimentData{
				Assignments: usageSummary.Experiments.Assignments,
			}
		}
	}

	experimentDataLog.Print("No experiment data found")
	return nil
}

func experimentDataFromState(state *ExperimentState) *ExperimentData {
	if state == nil || len(state.Counts) == 0 {
		return nil
	}
	experimentDataLog.Printf("Found %d experiment(s) in state file", len(state.Counts))
	runs := state.Runs
	lastRunIndex := len(runs) - 1
	if lastRunIndex >= 0 && lastRunIndex < len(runs) {
		lastRun := runs[lastRunIndex]
		if len(lastRun.Assignments) > 0 {
			experimentDataLog.Printf("Using run record from run_id=%s (timestamp=%s)", lastRun.RunID, lastRun.Timestamp)
			return &ExperimentData{
				Assignments:      lastRun.Assignments,
				CumulativeCounts: state.Counts,
			}
		}
	}

	assignments := make(map[string]string, len(state.Counts))
	names := sliceutil.SortedKeys(state.Counts)
	for _, name := range names {
		variantCounts := state.Counts[name]
		selected := deriveLastSelectedVariant(variantCounts)
		assignments[name] = selected
		experimentDataLog.Printf("Experiment %q: selected variant=%q", name, selected)
	}
	return &ExperimentData{
		Assignments:      assignments,
		CumulativeCounts: state.Counts,
	}
}

// formatExperimentLabel returns a compact, human-readable label summarising the
// experiment assignments for a single run. It is used in the Overview section of
// the audit report to surface experiment context alongside the run header.
//
// Examples:
//
//	one experiment:  "style=concise"
//	two experiments: "caveman=yes, style=concise"
//	nil/empty:       ""
func formatExperimentLabel(exp *ExperimentData) string {
	if exp == nil || len(exp.Assignments) == 0 {
		return ""
	}

	names := sliceutil.SortedKeys(exp.Assignments)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+exp.Assignments[name])
	}
	return strings.Join(parts, ", ")
}

// firstExperimentAssignment returns the deterministic first experiment assignment
// from exp, sorted by experiment name. It is used when callers need a stable
// single experiment/variant pair for provenance fields.
func firstExperimentAssignment(exp *ExperimentData) (name, variant string, ok bool) {
	if exp == nil || len(exp.Assignments) == 0 {
		return "", "", false
	}
	names := sliceutil.SortedKeys(exp.Assignments)
	if len(names) > 0 {
		name = names[0]
		return name, exp.Assignments[name], true
	}
	return "", "", false
}

// experimentMatchesFilter reports whether exp satisfies the given experiment/variant
// filter pair. Rules:
//   - If experimentName is empty, every run passes (no filter active).
//   - If experimentName is set but exp is nil or lacks that experiment, the run fails.
//   - If variant is also set, the assigned variant must equal variant.
func experimentMatchesFilter(exp *ExperimentData, experimentName, variant string) bool {
	if experimentName == "" {
		return true
	}
	if exp == nil {
		return false
	}
	assigned, ok := exp.Assignments[experimentName]
	if !ok {
		return false
	}
	if variant != "" && assigned != variant {
		return false
	}
	return true
}

// formatExperimentSkipMessage returns the informational message emitted when a run
// is skipped because its experiment data does not satisfy the active filter.
func formatExperimentSkipMessage(runID int64, experimentName, variant string) string {
	if variant != "" {
		return fmt.Sprintf("Run %d skipped: experiment %q not assigned variant %q", runID, experimentName, variant)
	}
	return fmt.Sprintf("Run %d skipped: experiment %q not assigned (not found in run artifacts)", runID, experimentName)
}

// deriveLastSelectedVariant returns the variant selected on the last run based on the
// highest count. Ties are broken by sorted order.
func deriveLastSelectedVariant(variantCounts map[string]int) string {
	if len(variantCounts) == 0 {
		return ""
	}
	variants := sliceutil.SortedKeys(variantCounts)
	if len(variants) > 0 {
		selected := variants[0]
		maxCount := variantCounts[selected]
		for _, v := range variants[1:] {
			if variantCounts[v] > maxCount {
				maxCount = variantCounts[v]
				selected = v
			}
		}
		return selected
	}
	return ""
}
