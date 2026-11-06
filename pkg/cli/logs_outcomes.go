package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/github"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/spf13/cobra"
)

// OutcomesData records live outcome observations for one selected workflow run.
type OutcomesData struct {
	RunID     int64           `json:"run_id"`
	Workflow  string          `json:"workflow,omitempty"`
	Items     []OutcomeReport `json:"items"`
	Summary   OutcomeSummary  `json:"summary"`
	EvalError string          `json:"eval_error,omitempty"`
}

func addLogsOutcomeFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("outcomes", false, "Evaluate current GitHub outcomes of selected runs' safe outputs (additional API requests)")
	cmd.Flags().String("outcomes-dir", "", "Write outcome JSONL observations to this directory (requires --outcomes)")
}

func logsOutcomeArtifactSets(sets []string, outcomes bool) []string {
	if !outcomes || len(sets) == 0 || slices.Contains(sets, "all") || slices.Contains(sets, string(ArtifactSetOutcomes)) {
		return sets
	}
	return append(slices.Clone(sets), string(ArtifactSetOutcomes))
}

func collectLogsOutcomes(ctx context.Context, runs []ProcessedRun, dir string, verbose bool) ([]OutcomesData, error) {
	ctx = withOutcomeAPICache(ctx)
	if dir == "" {
		dir = lookupEnv("GH_AW_OUTCOMES_DIR")
	}
	mapping := github.LoadObjectiveMapping()
	reports := make([]OutcomesData, 0, len(runs))
	for _, run := range runs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report, err := collectRunOutcomes(ctx, run, mapping, verbose)
		if err != nil {
			return nil, fmt.Errorf("evaluate outcomes for run %d: %w", run.Run.DatabaseID, err)
		}
		if dir != "" && report.EvalError == "" {
			if err := writeOutcomeJSONL(dir, report.RunID, report.Items); err != nil {
				return nil, err
			}
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func collectRunOutcomes(ctx context.Context, run ProcessedRun, mapping *github.ObjectiveMapping, verbose bool) (OutcomesData, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Evaluating outcomes for run %d", run.Run.DatabaseID)))
	}
	report := OutcomesData{
		RunID: run.Run.DatabaseID, Workflow: run.Run.WorkflowName,
		Items: []OutcomeReport{},
	}
	items, found, err := readLogsOutcomeItems(run)
	if err != nil {
		return report, err
	}
	if !found {
		report.EvalError = "safe-output manifest unavailable; outcome coverage is unknown"
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Run %d: %s", report.RunID, report.EvalError)))
		return report, nil
	}
	repository := logsOutcomeRepository(run.Run)
	trustedHost := outcomeAPIHostForRepo(repository)
	if runURL, err := parser.ParseRunURLExtended(run.Run.URL); err == nil && runURL.Host != "" {
		trustedHost = runURL.Host
	}
	report.Items = evaluateOutcomes(ctx, items, repository, mapping, trustedHost)
	if err := ctx.Err(); err != nil {
		return report, err
	}
	report.Summary = ComputeOutcomeSummary(report.Items, mapping)
	if report.Summary.Accepted > 0 {
		aic, err := loadOutcomeAIC(run.Run.LogsPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to compute outcome AI Credits; cost per accepted outcome omitted: %v", err)))
		} else {
			report.Summary.addAIC(aic)
		}
	}
	return report, nil
}

func logsOutcomeRepository(run WorkflowRun) string {
	runURL, err := parser.ParseRunURLExtended(run.URL)
	if err != nil || runURL.Host == "" || runURL.Host == "github.com" {
		return run.Repository
	}
	repository, host := repoutil.NormalizeRepoForAPI(run.Repository)
	if repository == "" || host != "" {
		return run.Repository
	}
	return path.Join(runURL.Host, repository)
}

func readLogsOutcomeItems(run ProcessedRun) ([]CreatedItemReport, bool, error) {
	if run.Run.LogsPath == "" {
		return nil, false, nil
	}
	artifacts, err := logsOutcomeEvidenceDirs(run.Run.LogsPath, []string{"safe-outputs-items"})
	if err != nil {
		return nil, false, err
	}
	// Immutable invocation artifacts are distinct even when their contents match.
	// Flat and nested run-root copies are mirrors, not additional invocations.
	dirs := append(slices.Clone(artifacts), run.Run.LogsPath)
	seen := make(map[[sha256.Size]byte]struct{})
	seenDirs := make(map[string]struct{})
	var combined []CreatedItemReport
	found := false
	for index, dir := range dirs {
		if _, duplicate := seenDirs[dir]; duplicate {
			continue
		}
		seenDirs[dir] = struct{}{}
		path := filepath.Join(dir, safeOutputItemsManifestFilename)
		items, err := readLogsOutcomeManifest(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, fmt.Errorf("read safe-output manifest %s: %w", path, err)
		}
		found = true
		encoded, err := json.Marshal(items)
		if err != nil {
			return nil, false, err
		}
		digest := sha256.Sum256(encoded)
		if _, mirrored := seen[digest]; index >= len(artifacts) && mirrored {
			continue
		}
		seen[digest] = struct{}{}
		evidenceDir, err := logsOutcomeEnrichmentDir(dir, run.Run.LogsPath)
		if err != nil {
			return nil, false, err
		}
		if evidenceDir != "" {
			items = enrichItemsFromAgentOutput(items, evidenceDir, run.Run.Repository)
		}
		combined = append(combined, items...)
	}
	return combined, found, nil
}

func readLogsOutcomeManifest(path string) (items []CreatedItemReport, returnErr error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close safe-output manifest %s: %w", path, err))
		}
	}()
	return decodeLogsOutcomeItems(file)
}

func collectEpisodeOutcomes(ctx context.Context, data *LogsData, outcomesDir string, verbose bool) error {
	runs := make([]ProcessedRun, 0, len(data.Runs))
	for _, run := range data.Runs {
		runs = append(runs, ProcessedRun{Run: WorkflowRun{
			DatabaseID: run.RunID, WorkflowName: run.WorkflowName, URL: run.URL,
			Repository: run.Repository, LogsPath: run.LogsPath,
		}})
	}
	reports, err := collectLogsOutcomes(ctx, runs, outcomesDir, verbose)
	if err != nil {
		return err
	}
	return attachLogsOutcomes(data, reports)
}

func decodeLogsOutcomeItems(reader io.Reader) ([]CreatedItemReport, error) {
	items := []CreatedItemReport{}
	decoder := json.NewDecoder(reader)
	for {
		var item CreatedItemReport
		if err := decoder.Decode(&item); err != nil {
			if errors.Is(err, io.EOF) {
				return items, nil
			}
			return nil, err
		}
		if item.Type == "" {
			return nil, errors.New("safe-output manifest entry has no type")
		}
		items = append(items, item)
	}
}

func attachLogsOutcomes(data *LogsData, reports []OutcomesData) error {
	data.Outcomes = reports
	if reports == nil || len(data.Episodes) == 0 {
		return nil
	}
	var observations []EpisodeOutcomeObservation
	costs := make(map[int64]episodeOutcomeCost)
	for _, report := range reports {
		for _, item := range report.Items {
			observations = append(observations, EpisodeOutcomeObservation{RunID: report.RunID, RunItemCount: len(report.Items), OutcomeReport: item})
		}
		for _, run := range data.Runs {
			if run.RunID != report.RunID {
				continue
			}
			usage, err := analyzeTokenUsageAICOnly(run.LogsPath, false)
			if err != nil {
				costs[run.RunID] = episodeOutcomeCost{warnings: []string{fmt.Sprintf("cost is unavailable: %v", err)}}
			} else if usage != nil {
				cost := episodeOutcomeCost{warnings: usage.Warnings}
				if usage.AICFound {
					cost.aic = &usage.TotalAIC
				}
				costs[run.RunID] = cost
			}
		}
	}
	logs := episodeOutcomesLogs{Runs: data.Runs, Outcomes: reports}
	for _, episode := range data.Episodes {
		coverage := episode.Coverage
		logs.Episodes = append(logs.Episodes, episodeOutcomeMembership{
			EpisodeData: episode,
			Coverage: &EpisodeOutcomeMembershipCoverage{
				Scope: coverage.Scope, Status: coverage.Status,
				Reasons: coverage.Reasons, CollectionTruncated: coverage.CollectionTruncated,
			},
		})
	}
	joined, err := buildEpisodeOutcomes(logs, observations, costs)
	if err != nil {
		return fmt.Errorf("aggregate episode outcomes: %w", err)
	}
	data.EpisodeOutcomes = &joined
	return nil
}

func renderLogsOutcomes(w io.Writer, data LogsData) error {
	for _, run := range data.Outcomes {
		if _, err := fmt.Fprintf(w, "\nOutcomes for %s (run %d): %d accepted, %d rejected, %d ignored, %d lifecycle, %d pending, %d unknown, %d errors (%d total)\n",
			run.Workflow, run.RunID, run.Summary.Accepted, run.Summary.Rejected, run.Summary.Ignored, run.Summary.Lifecycle, run.Summary.Pending, run.Summary.Unknown, run.Summary.Errors, run.Summary.Total); err != nil {
			return err
		}
		if run.EvalError != "" {
			if _, err := fmt.Fprintln(w, "  Missing evidence: "+run.EvalError); err != nil {
				return err
			}
		}
		for _, item := range run.Items {
			if _, err := fmt.Fprintf(w, "  %s #%d: %s (%s)\n", item.Type, item.ObjectNumber, item.OutcomeStatus, item.Detail); err != nil {
				return err
			}
		}
	}
	if data.EpisodeOutcomes != nil {
		return renderEpisodeOutcomes(w, *data.EpisodeOutcomes)
	}
	return nil
}
