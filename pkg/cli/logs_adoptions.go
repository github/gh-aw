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

// AdoptionsData records live adoption observations for one selected workflow run.
type AdoptionsData struct {
	RunID     int64            `json:"run_id"`
	Workflow  string           `json:"workflow,omitempty"`
	Items     []AdoptionReport `json:"items"`
	Summary   AdoptionSummary  `json:"summary"`
	EvalError string           `json:"eval_error,omitempty"`
}

func addLogsAdoptionFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("adoptions", false, "Evaluate current GitHub adoptions of selected runs' safe outputs (additional API requests)")
	cmd.Flags().String("adoptions-dir", "", "Write adoption JSONL observations to this directory (requires --adoptions)")
}

func logsAdoptionArtifactSets(sets []string, adoptions bool) []string {
	if !adoptions || len(sets) == 0 || slices.Contains(sets, "all") || slices.Contains(sets, string(ArtifactSetAdoptions)) {
		return sets
	}
	return append(slices.Clone(sets), string(ArtifactSetAdoptions))
}

func collectLogsAdoptions(ctx context.Context, runs []ProcessedRun, dir string, verbose bool) ([]AdoptionsData, error) {
	ctx = withAdoptionAPICache(ctx)
	if dir == "" {
		dir = lookupEnv("GH_AW_ADOPTIONS_DIR")
	}
	mapping := github.LoadObjectiveMapping()
	reports := make([]AdoptionsData, 0, len(runs))
	for _, run := range runs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report, err := collectRunAdoptions(ctx, run, mapping, verbose)
		if err != nil {
			return nil, fmt.Errorf("evaluate adoptions for run %d: %w", run.Run.DatabaseID, err)
		}
		if dir != "" && report.EvalError == "" {
			if err := writeAdoptionJSONL(dir, report.RunID, report.Items); err != nil {
				return nil, err
			}
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func collectRunAdoptions(ctx context.Context, run ProcessedRun, mapping *github.ObjectiveMapping, verbose bool) (AdoptionsData, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Evaluating adoptions for run %d", run.Run.DatabaseID)))
	}
	report := AdoptionsData{
		RunID: run.Run.DatabaseID, Workflow: run.Run.WorkflowName,
		Items: []AdoptionReport{},
	}
	items, found, err := readLogsAdoptionItems(run)
	if err != nil {
		return report, err
	}
	if !found {
		report.EvalError = "safe-output manifest unavailable; adoption coverage is unknown"
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Run %d: %s", report.RunID, report.EvalError)))
		return report, nil
	}
	repository := logsAdoptionRepository(run.Run)
	trustedHost := adoptionAPIHostForRepo(repository)
	if runURL, err := parser.ParseRunURLExtended(run.Run.URL); err == nil && runURL.Host != "" {
		trustedHost = runURL.Host
	}
	report.Items = evaluateAdoptions(ctx, items, repository, mapping, trustedHost)
	if err := ctx.Err(); err != nil {
		return report, err
	}
	report.Summary = ComputeAdoptionSummary(report.Items, mapping)
	if report.Summary.Accepted > 0 {
		aic, err := loadAdoptionAIC(run.Run.LogsPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to compute adoption AI Credits; cost per accepted adoption omitted: %v", err)))
		} else {
			report.Summary.addAIC(aic)
		}
	}
	return report, nil
}

func logsAdoptionRepository(run WorkflowRun) string {
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

func readLogsAdoptionItems(run ProcessedRun) ([]CreatedItemReport, bool, error) {
	if run.Run.LogsPath == "" {
		return nil, false, nil
	}
	artifacts, err := logsAdoptionEvidenceDirs(run.Run.LogsPath, []string{"safe-outputs-items"})
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
		items, err := readLogsAdoptionManifest(path)
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
		evidenceDir, err := logsAdoptionEnrichmentDir(dir, run.Run.LogsPath)
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

func readLogsAdoptionManifest(path string) (items []CreatedItemReport, returnErr error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close safe-output manifest %s: %w", path, err))
		}
	}()
	return decodeLogsAdoptionItems(file)
}

func collectEpisodeAdoptions(ctx context.Context, data *LogsData, adoptionsDir string, verbose bool) error {
	runs := make([]ProcessedRun, 0, len(data.Runs))
	for _, run := range data.Runs {
		runs = append(runs, ProcessedRun{Run: WorkflowRun{
			DatabaseID: run.RunID, WorkflowName: run.WorkflowName, URL: run.URL,
			Repository: run.Repository, LogsPath: run.LogsPath,
		}})
	}
	reports, err := collectLogsAdoptions(ctx, runs, adoptionsDir, verbose)
	if err != nil {
		return err
	}
	return attachLogsAdoptions(data, reports)
}

func decodeLogsAdoptionItems(reader io.Reader) ([]CreatedItemReport, error) {
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

func attachLogsAdoptions(data *LogsData, reports []AdoptionsData) error {
	data.Adoptions = reports
	var items []AdoptionReport
	for _, report := range reports {
		items = append(items, report.Items...)
	}
	summary := ComputeAdoptionSummary(items, nil)
	data.Summary.AdoptionTotal = summary.Total
	data.Summary.AdoptionAccepted = summary.Accepted
	data.Summary.AdoptionRejected = summary.Rejected
	data.Summary.AdoptionPending = summary.Pending
	data.Summary.AdoptionLifecycle = summary.Lifecycle
	data.Summary.AdoptionNotApplicable = summary.NotApplicable
	data.Summary.AdoptionUnknown = summary.Unknown
	data.Summary.AdoptionAcceptanceRate = summary.AcceptanceRate
	data.Summary.AdoptionWasteRate = summary.WasteRate
	data.Summary.AdoptionZeroTouchRate = summary.ZeroTouchRate
	if reports == nil || len(data.Episodes) == 0 {
		return nil
	}
	var observations []EpisodeAdoptionObservation
	costs := make(map[int64]episodeAdoptionCost)
	for _, report := range reports {
		for _, item := range report.Items {
			observations = append(observations, EpisodeAdoptionObservation{RunID: report.RunID, RunItemCount: len(report.Items), AdoptionReport: item})
		}
		for _, run := range data.Runs {
			if run.RunID != report.RunID {
				continue
			}
			usage, err := analyzeTokenUsageAICOnly(run.LogsPath, false)
			if err != nil {
				costs[run.RunID] = episodeAdoptionCost{warnings: []string{fmt.Sprintf("cost is unavailable: %v", err)}}
			} else if usage != nil {
				cost := episodeAdoptionCost{warnings: usage.Warnings}
				if usage.AICFound {
					cost.aic = &usage.TotalAIC
				}
				costs[run.RunID] = cost
			}
		}
	}
	logs := episodeAdoptionsLogs{Runs: data.Runs, Adoptions: reports}
	for _, episode := range data.Episodes {
		coverage := episode.Coverage
		logs.Episodes = append(logs.Episodes, episodeAdoptionMembership{
			EpisodeData: episode,
			Coverage: &EpisodeAdoptionMembershipCoverage{
				Scope: coverage.Scope, Status: coverage.Status,
				Reasons: coverage.Reasons, CollectionTruncated: coverage.CollectionTruncated,
			},
		})
	}
	joined, err := buildEpisodeAdoptions(logs, observations, costs)
	if err != nil {
		return fmt.Errorf("aggregate episode adoptions: %w", err)
	}
	data.EpisodeAdoptions = &joined
	return nil
}

func renderLogsAdoptions(w io.Writer, data LogsData) error {
	for _, run := range data.Adoptions {
		if _, err := fmt.Fprintf(w, "\nAdoptions for %s (run %d): %d accepted, %d rejected, %d lifecycle, %d pending, %d not applicable, %d unknown, %d errors (%d total)\n",
			run.Workflow, run.RunID, run.Summary.Accepted, run.Summary.Rejected, run.Summary.Lifecycle, run.Summary.Pending, run.Summary.NotApplicable, run.Summary.Unknown, run.Summary.Errors, run.Summary.Total); err != nil {
			return err
		}
		if run.EvalError != "" {
			if _, err := fmt.Fprintln(w, "  Missing evidence: "+run.EvalError); err != nil {
				return err
			}
		}
		for _, item := range run.Items {
			if _, err := fmt.Fprintf(w, "  %s #%d: %s (%s)\n", item.Type, item.ObjectNumber, item.AdoptionStatus, item.Detail); err != nil {
				return err
			}
		}
	}
	if data.EpisodeAdoptions != nil {
		return renderEpisodeAdoptions(w, *data.EpisodeAdoptions)
	}
	return nil
}
