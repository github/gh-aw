package cli

import (
	"encoding/json"
	"os/exec"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const experimentsBranchPrefix = "experiments/"
const evalsBranchPrefix = constants.EvalsBranchPrefix + "/"

type ExperimentState struct {
	Counts map[string]map[string]int `json:"counts"` // experiment name → variant → count
	Runs   []ExperimentRunRecord     `json:"runs,omitempty"`
}

// ExperimentRunRecord represents a single workflow run in the JSONL ledger.
type ExperimentRunRecord struct {
	RunID          string                    `json:"run_id"`
	Timestamp      string                    `json:"timestamp"`
	Assignments    map[string]string         `json:"assignments"`
	BaselineCounts map[string]map[string]int `json:"baseline_counts,omitempty"`
}

// ExperimentVariantStats holds counts for all variants of one named A/B experiment.
type ExperimentVariantStats struct {
	Name     string         `json:"name"`
	Variants map[string]int `json:"variants"` // variant → count
	Total    int            `json:"total"`
}

// ExperimentInfo represents a single experiment workflow for list output.
// Unrelated to ExperimentData, which holds per-run variant assignments in audit reports.
type ExperimentInfo struct {
	WorkflowID  string `json:"workflow_id" console:"header:Workflow"`
	Branch      string `json:"branch" console:"header:Branch"`
	Experiments int    `json:"experiments" console:"header:Experiments"`
	TotalRuns   int    `json:"total_runs" console:"header:Total Runs"`
	LastRun     string `json:"last_run" console:"header:Last Run"`
}

// ExperimentDetails represents detailed information about a specific experiment workflow.
type ExperimentDetails struct {
	WorkflowID  string                   `json:"workflow_id"`
	Branch      string                   `json:"branch"`
	TotalRuns   int                      `json:"total_runs"`
	Experiments []ExperimentVariantStats `json:"experiments"`
	RecentRuns  []ExperimentRunRecord    `json:"recent_runs,omitempty"`
	Runs        []ExperimentRunRecord    `json:"-"`
	// Analyses holds the statistical analysis for each named experiment.
	// Populated by RunExperimentsAnalyze; absent in list output.
	Analyses []ExperimentAnalysis `json:"analyses,omitempty"`
}

func experimentStateFilenames() []string {
	return []string{"state.jsonl", "state.json"}
}

// readLocalExperimentState reads experiment state from a local git ref (e.g. "origin/experiments/foo").
// Returns an empty state when the file is absent or cannot be parsed.
func readLocalExperimentState(ref string) *ExperimentState {
	for _, fileName := range experimentStateFilenames() {
		objectArg, err := buildSafeGitShowObjectArg(ref, fileName)
		if err != nil {
			experimentsLog.Printf("Skipping unsafe git show argument (ref=%q file=%q): %v", ref, fileName, err)
			continue
		}
		cmd := exec.Command("git", "show", objectArg)
		out, err := cmd.Output()
		if err == nil {
			return parseExperimentState(out)
		}
	}
	return emptyExperimentState()
}

// readRemoteExperimentState reads state.jsonl or state.json from a remote experiment branch.
// Returns an empty state when neither file can be read or parsed.
func readRemoteExperimentState(repoOverride, branchName string) *ExperimentState {
	for _, fileName := range experimentStateFilenames() {
		decoded, err := readRemoteRepoBranchFile(repoOverride, branchName, fileName, "")
		if err == nil {
			return parseExperimentState(decoded)
		}
	}
	return emptyExperimentState()
}

func appendExperimentRun(state *ExperimentState, run ExperimentRunRecord) {
	if state.Counts == nil {
		state.Counts = map[string]map[string]int{}
	}
	for name, variants := range run.BaselineCounts {
		if state.Counts[name] == nil {
			state.Counts[name] = map[string]int{}
		}
		for variant, count := range variants {
			state.Counts[name][variant] += count
		}
	}
	for name, variant := range run.Assignments {
		if state.Counts[name] == nil {
			state.Counts[name] = map[string]int{}
		}
		// BaselineCounts captures totals before this run, so the assigned
		// variant is intentionally added as the current run.
		state.Counts[name][variant]++
	}
	state.Runs = append(state.Runs, run)
}

func parseExperimentStateJSONL(data []byte) *ExperimentState {
	state := emptyExperimentState()
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var snapshot ExperimentState
		if err := json.Unmarshal([]byte(line), &snapshot); err == nil && snapshot.Counts != nil {
			// A snapshot is a cumulative checkpoint, so it discards preceding run records.
			state = &snapshot
			continue
		}

		var run ExperimentRunRecord
		if err := json.Unmarshal([]byte(line), &run); err != nil || run.RunID == "" || run.Timestamp == "" || len(run.Assignments) == 0 {
			experimentsLog.Printf("parseExperimentStateJSONL: skipping unrecognized line")
			continue
		}
		appendExperimentRun(state, run)
	}
	return state
}

// parseExperimentState unmarshals raw JSON or JSONL into an ExperimentState.
// Returns an empty state when parsing fails or the data is invalid.
func parseExperimentState(data []byte) *ExperimentState {
	var state ExperimentState
	if err := json.Unmarshal(data, &state); err == nil && state.Counts != nil {
		return &state
	}
	return parseExperimentStateJSONL(data)
}

// emptyExperimentState returns a zero-value ExperimentState with an initialised Counts map.
func emptyExperimentState() *ExperimentState {
	return &ExperimentState{Counts: map[string]map[string]int{}}
}

// experimentInfoFromState builds an ExperimentInfo summary from experiment state.
func experimentInfoFromState(workflowID, branchName string, state *ExperimentState) ExperimentInfo {
	return ExperimentInfo{
		WorkflowID:  workflowID,
		Branch:      branchName,
		Experiments: len(state.Counts),
		TotalRuns:   experimentTotalRuns(state),
		LastRun:     experimentLastRun(state),
	}
}

// experimentDetailsFromState builds ExperimentDetails from experiment state.
func experimentDetailsFromState(workflowID, branchName string, state *ExperimentState) *ExperimentDetails {
	experiments := make([]ExperimentVariantStats, 0, len(state.Counts))
	for name, variants := range state.Counts {
		total := 0
		for _, c := range variants {
			total += c
		}
		experiments = append(experiments, ExperimentVariantStats{
			Name:     name,
			Variants: variants,
			Total:    total,
		})
	}
	slices.SortFunc(experiments, func(a, b ExperimentVariantStats) int {
		return strings.Compare(a.Name, b.Name)
	})

	recentRuns := state.Runs
	const maxRecentRuns = 10
	if len(recentRuns) > maxRecentRuns {
		recentRuns = recentRuns[len(recentRuns)-maxRecentRuns:]
	}

	return &ExperimentDetails{
		WorkflowID:  workflowID,
		Branch:      branchName,
		TotalRuns:   experimentTotalRuns(state),
		Experiments: experiments,
		RecentRuns:  recentRuns,
		Runs:        state.Runs,
	}
}

// experimentTotalRuns returns the total number of runs recorded in the state.
// Prefers the runs array length when non-empty; falls back to summing all variant counts.
func experimentTotalRuns(state *ExperimentState) int {
	if len(state.Runs) > 0 {
		return len(state.Runs)
	}
	total := 0
	for _, variants := range state.Counts {
		for _, c := range variants {
			total += c
		}
	}
	return total
}

// experimentLastRun returns the date (YYYY-MM-DD) of the most recent run, or "" if unknown.
func experimentLastRun(state *ExperimentState) string {
	if len(state.Runs) == 0 {
		return ""
	}
	ts := state.Runs[len(state.Runs)-1].Timestamp
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// extractExperimentName extracts the workflow ID from a branch ref.
//
//	"origin/experiments/my-workflow" → "my-workflow"
//	"experiments/my-workflow"        → "my-workflow"
//	"experiments/"                   → "" (bare prefix, rejected by callers)
func extractExperimentName(ref string) string {
	ref = strings.TrimPrefix(ref, "origin/")
	if !strings.HasPrefix(ref, experimentsBranchPrefix) {
		return ""
	}
	// An empty result here (bare "experiments/" ref) is acceptable: callers
	// guard against empty workflow IDs with `if workflowID == ""` checks.
	return strings.TrimPrefix(ref, experimentsBranchPrefix)
}
