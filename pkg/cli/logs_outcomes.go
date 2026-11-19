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
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/github"
	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/spf13/cobra"
)

// OutcomesData records live outcome observations for one selected workflow run.
type OutcomesData struct {
	RunID     int64              `json:"run_id"`
	Workflow  string             `json:"workflow,omitempty"`
	Items     []OutcomeReport    `json:"items"`
	Summary   OutcomeSummary     `json:"summary"`
	EvalError string             `json:"eval_error,omitempty"`
	Coverage  string             `json:"coverage"`
	Delivery  *DeliveryData      `json:"-"`
	Live      bool               `json:"-"`
	Records   []lifecycle.Record `json:"-"`
}

type DeliveredItem struct {
	ID string `json:"id"`
	lifecycle.Identity
	Delivery lifecycle.Delivery `json:"delivery"`
}

type DeliveryData struct {
	RunID         int64           `json:"run_id"`
	Workflow      string          `json:"workflow,omitempty"`
	Items         []DeliveredItem `json:"items"`
	Delivered     int             `json:"delivered"`
	Coverage      string          `json:"coverage"`
	Error         string          `json:"error,omitempty"`
	RetentionDays int             `json:"retention_days"`
}

func addLogsOutcomeFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("delivery", false, "Report verified safe-output delivery from selected runs' execution evidence")
	cmd.Flags().Bool("adoption", false, "Refresh safe-output adoption from GitHub (includes delivery; additional API requests)")
	cmd.Flags().String("adoption-dir", "", "Write adoption JSONL observations to this directory (requires --adoption)")
	cmd.Flags().Bool("retained", false, "Refresh due retained deliveries without discovering or downloading runs (requires --adoption and --repo; --count limits delivery checks)")
}

func logsOutcomeArtifactSets(sets []string, outcomes bool) []string {
	if !outcomes || len(sets) == 0 || slices.Contains(sets, "all") || slices.Contains(sets, string(ArtifactSetOutcomes)) {
		return sets
	}
	return append(slices.Clone(sets), string(ArtifactSetOutcomes))
}

func collectLogsLifecycles(ctx context.Context, runs []ProcessedRun, dir string, verbose, live bool, caches ...*cachedLogsJSONLCache) ([]OutcomesData, error) {
	ctx = withOutcomeAPICache(ctx)
	if len(caches) > 0 && caches[0] != nil { //nolint:uncheckedsliceindex // The length guard protects the optional first cache.
		ctx = context.WithValue(ctx, lifecycleCacheKey{}, caches[0])
	}
	if dir == "" {
		dir = lookupEnv("GH_AW_ADOPTION_DIR")
	}
	mapping := github.LoadObjectiveMapping()
	reports := make([]OutcomesData, 0, len(runs))
	for _, run := range runs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report, err := collectRunLifecycle(ctx, run, mapping, verbose, live)
		if err != nil {
			return nil, fmt.Errorf("evaluate adoption for run %d: %w", run.Run.DatabaseID, err)
		}
		if live && dir != "" && (report.EvalError == "" || len(report.Items) > 0) {
			if err := writeOutcomeJSONL(dir, report.RunID, report.Items); err != nil {
				return nil, err
			}
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func collectRunOutcomes(ctx context.Context, run ProcessedRun, mapping *github.ObjectiveMapping, verbose bool) (OutcomesData, error) {
	return collectRunLifecycle(ctx, run, mapping, verbose, true)
}

func collectRunLifecycle(ctx context.Context, run ProcessedRun, mapping *github.ObjectiveMapping, verbose, live bool) (OutcomesData, error) { //nolint:largefunc // Keeps the cache lock, evidence merge, refresh, and atomic persistence in one transaction.
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Evaluating adoption for run %d", run.Run.DatabaseID)))
	}
	report := OutcomesData{
		RunID: run.Run.DatabaseID, Workflow: run.Run.WorkflowName,
		Items: []OutcomeReport{}, Live: live, Coverage: "complete",
		Delivery: &DeliveryData{RunID: run.Run.DatabaseID, Workflow: run.Run.WorkflowName, Items: []DeliveredItem{}, Coverage: "complete", RetentionDays: 365},
	}
	items, found, err := readLifecycleItems(ctx, run)
	if errors.Is(err, errLifecycleEvidenceIncomplete) {
		report.Coverage, report.Delivery.Coverage = "partial", "partial"
		report.EvalError, report.Delivery.Error = err.Error(), err.Error()
	} else if err != nil {
		return report, err
	}
	if run.Run.LogsPath == "" {
		report.EvalError = "safe-output artifact directory unavailable"
		report.Coverage = "unavailable"
		report.Delivery.Coverage, report.Delivery.Error = "unavailable", report.EvalError
		return report, nil
	}
	unlock, err := lockLifecycleCache(ctx, run.Run.LogsPath)
	if err != nil {
		return report, err
	}
	defer unlock()
	retained, err := loadLifecycleCache(run.Run.LogsPath)
	if err != nil {
		return report, err
	}
	if imported, ok := ctx.Value(lifecycleCacheKey{}).(*cachedLogsJSONLCache); ok {
		for _, record := range imported.lifecycles {
			retained[record.ID], err = lifecycle.Merge(retained[record.ID], record)
			if err != nil {
				return report, err
			}
		}
	}
	retainedOnly := !found
	if !found {
		sourceRepository, _ := repoutil.NormalizeRepoForAPI(logsOutcomeRepository(run.Run))
		for _, record := range retained {
			if record.RunID != run.Run.DatabaseID || record.RunAttempt != run.Run.Attempt ||
				record.Workflow != run.Run.WorkflowPath || !strings.EqualFold(record.Repository, sourceRepository) ||
				record.Host != outcomeAPIHostForRepo(logsOutcomeRepository(run.Run)) {
				continue
			}
			items = append(items, retainedLifecycleItem(record))
		}
		found = len(items) > 0
	}
	if !found {
		report.EvalError = "safe-output execution evidence unavailable; delivery and adoption coverage are unknown"
		report.Coverage = "unavailable"
		report.Delivery.Coverage, report.Delivery.Error = "unavailable", report.EvalError
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Run %d: %s", report.RunID, report.EvalError)))
		return report, nil
	}
	repository := logsOutcomeRepository(run.Run)
	trustedHost := outcomeAPIHostForRepo(repository)
	if runURL, err := parser.ParseRunURLExtended(run.Run.URL); err == nil && runURL.Host != "" {
		trustedHost = runURL.Host
	}
	for _, item := range items {
		record, err := lifecycleFromItem(run.Run, item, repository, trustedHost)
		if err != nil {
			report.Delivery.Coverage = "partial"
			report.Coverage = "partial"
			report.Delivery.Error = err.Error()
			report.EvalError = err.Error()
			continue
		}
		if retainedOnly {
			record.Execution = retained[record.ID].Execution
		}
		record, err = lifecycle.Merge(retained[record.ID], record)
		if err != nil {
			return report, err
		}
		report.Delivery.Items = append(report.Delivery.Items, DeliveredItem{ID: record.ID, Identity: record.Identity, Delivery: record.Delivery})
		if !live {
			retained[record.ID] = record
			report.Records = append(report.Records, record)
			continue
		}
		observation, err := observeLifecycle(ctx, run.Run, item, record)
		if err != nil {
			return report, err
		}
		report.Items = append(report.Items, observation)
		if observation.Adoption.Coverage != "available" {
			report.Coverage = "partial"
		}
		retained[record.ID] = *observation.Lifecycle
		report.Records = append(report.Records, *observation.Lifecycle)
	}
	report.Delivery.Delivered = len(report.Delivery.Items)
	if err := writeLifecycleCache(run.Run.LogsPath, retained); err != nil {
		return report, err
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	report.Summary = ComputeOutcomeSummary(report.Items, mapping)
	if report.Summary.Accepted > 0 {
		aic, err := loadOutcomeAIC(run.Run.LogsPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to compute adoption AI Credits; cost per accepted adoption omitted: %v", err)))
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
		occurrences := make(map[[sha256.Size]byte]int)
		for i := range items {
			items[i].EvidenceID = outcomeItemID(items[i], occurrences)
			items[i].InvocationID = nameForLifecycleInvocation(dir, run.Run.LogsPath)
		}
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

func collectEpisodeLifecycles(ctx context.Context, data *LogsData, outcomesDir string, verbose, live bool) error {
	if data.EpisodeRetrieval != nil {
		ctx = context.WithValue(ctx, lifecycleAPIBudgetKey{}, &lifecycleAPIBudget{report: data.EpisodeRetrieval})
	}
	runs := make([]ProcessedRun, 0, len(data.Runs))
	for _, run := range data.Runs {
		attempt, err := strconv.Atoi(run.RunAttempt)
		if run.RunAttempt != "" && (err != nil || attempt <= 0) {
			return fmt.Errorf("invalid recorded attempt for run %d", run.RunID)
		}
		runs = append(runs, ProcessedRun{Run: WorkflowRun{
			DatabaseID: run.RunID, WorkflowName: run.WorkflowName, URL: run.URL,
			Repository: run.Repository, LogsPath: run.LogsPath, Attempt: attempt, WorkflowPath: run.WorkflowPath,
		}})
	}
	reports, err := collectLogsLifecycles(ctx, runs, outcomesDir, verbose, live)
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
	for _, report := range reports {
		if report.Delivery != nil {
			data.Delivery = append(data.Delivery, *report.Delivery)
		}
		if report.Live || report.Delivery == nil {
			data.Outcomes = append(data.Outcomes, report)
		}
	}
	if len(data.Outcomes) == 0 || len(data.Episodes) == 0 {
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
		return fmt.Errorf("aggregate episode adoption: %w", err)
	}
	data.EpisodeOutcomes = &joined
	return nil
}

func renderLogsOutcomes(w io.Writer, data LogsData) error {
	for _, delivery := range data.Delivery {
		if _, err := fmt.Fprintf(w, "\nDelivery for %s (run %d): %d delivered; coverage: %s\n", delivery.Workflow, delivery.RunID, delivery.Delivered, delivery.Coverage); err != nil {
			return err
		}
		if delivery.Error != "" {
			if _, err := fmt.Fprintln(w, "  Missing evidence: "+delivery.Error); err != nil {
				return err
			}
		}
	}
	for _, run := range data.Outcomes {
		if _, err := fmt.Fprintf(w, "\nAdoption for %s (run %d): %d accepted, %d rejected, %d not applicable, %d lifecycle, %d pending, %d unknown, %d errors (%d total; human: %d, automation: %d, delivery fallback: %d)\n",
			run.Workflow, run.RunID, run.Summary.Accepted, run.Summary.Rejected, run.Summary.NotApplicable, run.Summary.Lifecycle, run.Summary.Pending, run.Summary.Unknown, run.Summary.Errors, run.Summary.Total, run.Summary.AcceptedHuman, run.Summary.AcceptedAutomation, run.Summary.AcceptedFallback); err != nil {
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
