package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type RetainedAdoptionData struct {
	ObservedAt time.Time          `json:"observed_at" console:"header:Observed"`
	Delivered  int                `json:"delivered" console:"header:Retained deliveries,format:number"`
	Refreshed  int                `json:"refreshed" console:"header:Refreshed,format:number"`
	Deferred   int                `json:"deferred" console:"header:Deferred,format:number"`
	Coverage   string             `json:"coverage" console:"header:Coverage"`
	Records    []lifecycle.Record `json:"records" console:"-"`
}

func runRetainedAdoptionCommand(cmd *cobra.Command, args []string) error {
	if len(args) != 0 || !getBoolFlag(cmd, "adoption") || getStringFlag(cmd, "repo") == "" {
		return errors.New("--retained requires --adoption and --repo, without positional workflow arguments")
	}
	allowed := []string{"retained", "adoption", "repo", "output", "count", "last", "timeout", "timeout-seconds", "max-github-api-rate-limit", "json", "verbose"}
	var invalid string
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if cmd.InheritedFlags().Lookup(flag.Name) != nil {
			return
		}
		if !slices.Contains(allowed, flag.Name) {
			invalid = flag.Name
		}
	})
	if invalid != "" {
		return fmt.Errorf("--retained cannot be combined with --%s", invalid)
	}
	opts, err := loadCommonLogsOptions(cmd)
	if err != nil {
		return err
	}
	if opts.Count <= 0 || opts.Count > maxLifecycleCacheItems {
		return errors.New("--retained requires --count between 1 and 20000")
	}
	ctx, cancel := lifecycleLogsContext(cmd.Context(), opts.TimeoutMinutes, opts.TimeoutSeconds, opts.MaxGitHubAPIRateLimit)
	defer cancelLogsDownload(cancel)
	data, err := refreshRetainedAdoption(ctx, opts.OutputDir, opts.RepoOverride, opts.Count)
	if err != nil {
		return err
	}
	if opts.JSONOutput {
		return renderLogsJSON(LogsData{
			RetainedAdoption: &data, Runs: []RunData{}, LogsLocation: opts.OutputDir,
			Summary: LogsSummary{AICCoverage: "unavailable"},
		}, opts.Verbose)
	}
	_, err = fmt.Fprint(os.Stdout, console.RenderStructStdout(data))
	return err
}

func refreshRetainedAdoption(ctx context.Context, dir, repository string, limit int) (RetainedAdoptionData, error) {
	data := RetainedAdoptionData{ObservedAt: time.Now().UTC(), Coverage: "complete", Records: []lifecycle.Record{}}
	repo, host := repoutil.NormalizeRepoForAPI(repository)
	if host == "" {
		host = "github.com"
	}
	if strings.Count(repo, "/") != 1 || strings.HasPrefix(repo, "/") || strings.HasSuffix(repo, "/") {
		return data, errors.New("retained adoption requires a repository in [HOST/]OWNER/REPO format")
	}
	unlock, err := lockLifecycleStore(ctx, dir)
	if err != nil {
		return data, err
	}
	defer unlock()
	store := filepath.Join(dir, "lifecycle.jsonl")
	if _, err := os.Lstat(store); err != nil {
		return data, fmt.Errorf("read retained delivery ledger: %w; restore lifecycle.jsonl or collect logs --delivery first", err)
	}
	records, err := loadLifecycleRecords(store)
	if err != nil {
		return data, err
	}
	due := retainedLifecycleDue(records, repo, host, data.ObservedAt)
	ctx = withOutcomeAPICache(ctx)
	for _, record := range due {
		if err := ctx.Err(); err != nil {
			return data, err
		}
		if network, ok := ctx.Value(lifecycleNetworkContextKey{}).(context.Context); ok && network.Err() != nil {
			break
		}
		if data.Refreshed >= limit {
			break
		}
		run := WorkflowRun{DatabaseID: record.RunID, Attempt: record.RunAttempt, WorkflowPath: record.Workflow, Repository: path.Join(host, repo)}
		report, err := observeLifecycle(ctx, run, retainedLifecycleItem(record), record)
		if err != nil {
			return data, err
		}
		records[record.ID] = *report.Lifecycle
		data.Refreshed++
	}
	if err := writeLifecycleRecords(store, records); err != nil {
		return data, err
	}
	cutoff := data.ObservedAt.Add(-365 * 24 * time.Hour)
	for _, record := range records {
		if !strings.EqualFold(record.Repository, repo) || !strings.EqualFold(record.Host, host) || record.Delivery.At.Before(cutoff) {
			continue
		}
		data.Records = append(data.Records, record)
		if record.Adoption.Coverage != "available" && record.Adoption.State != "not_applicable" || record.Survival.Error != "" {
			data.Coverage = "partial"
		}
	}
	slices.SortFunc(data.Records, func(a, b lifecycle.Record) int { return strings.Compare(a.ID, b.ID) })
	data.Delivered, data.Deferred = len(data.Records), len(due)-data.Refreshed
	if data.Deferred > 0 {
		data.Coverage = "partial"
	}
	return data, nil
}

func retainedLifecycleDue(records map[string]lifecycle.Record, repository, host string, now time.Time) []lifecycle.Record {
	var due []lifecycle.Record
	for _, record := range records {
		if !strings.EqualFold(record.Repository, repository) || !strings.EqualFold(record.Host, host) ||
			record.Delivery.At.Before(now.Add(-365*24*time.Hour)) {
			continue
		}
		rule, registered := lifecycle.RuleFor(record.Type)
		if !registered || !rule.ValueEligible || record.Type != "create_issue" && record.Type != "create_pull_request" && !rule.FallbackToDelivery {
			continue
		}
		if last := retainedLifecycleCheckedAt(record); !last.IsZero() && now.Sub(last) < 24*time.Hour {
			continue
		}
		due = append(due, record)
	}
	slices.SortFunc(due, func(a, b lifecycle.Record) int {
		if comparison := retainedLifecycleCheckedAt(a).Compare(retainedLifecycleCheckedAt(b)); comparison != 0 {
			return comparison
		}
		return strings.Compare(a.ID, b.ID)
	})
	return due
}

func retainedLifecycleCheckedAt(record lifecycle.Record) time.Time {
	if record.Survival.ObservedAt.After(record.Adoption.ObservedAt) {
		return record.Survival.ObservedAt
	}
	if record.Adoption.Source == "github-api" || record.Adoption.Error != "" {
		return record.Adoption.ObservedAt
	}
	return record.Survival.ObservedAt
}

func retainedLifecycleItem(record lifecycle.Record) CreatedItemReport {
	item := CreatedItemReport{
		Type: record.Type, URL: record.TargetURL, Repo: path.Join(record.Host, record.TargetRepository),
		Timestamp: record.Delivery.At.Format(time.RFC3339Nano), EvidenceID: record.EvidenceID,
		InvocationID: record.InvocationID, EvidenceSource: record.Delivery.Source, Status: "success",
	}
	if execution := record.Execution; execution != nil {
		item.Number, item.Metadata = execution.Number, execution.Metadata
		item.LabelsBefore, item.LabelsAdded = execution.LabelsBefore, execution.LabelsAdded
		item.BeforeState, item.AfterState = execution.BeforeState, execution.AfterState
	}
	return item
}

func retainedLifecycleExecution(item CreatedItemReport) *lifecycle.Execution {
	execution := &lifecycle.Execution{
		Number: resolveItemNumber(item), LabelsBefore: slices.Clone(item.LabelsBefore),
		LabelsAdded: slices.Clone(item.LabelsAdded),
	}
	if len(execution.LabelsAdded) == 0 {
		for _, label := range item.Labels {
			execution.LabelsAdded = append(execution.LabelsAdded, label.Name)
		}
	}
	execution.Metadata = retainedLifecycleFields(item.Metadata, []string{
		"review_id", "milestone_number", "requested_reviewers", "requested_team_reviewers",
		"pushed_commit_shas", "commit_sha", "run_id",
	})
	fields := mutableTrackedFields(item.Type)
	if item.Type == "add_labels" {
		fields = []string{"labels"}
	}
	execution.BeforeState = retainedLifecycleFields(item.BeforeState, fields)
	execution.AfterState = retainedLifecycleFields(item.AfterState, fields)
	return execution
}

func retainedLifecycleFields(values map[string]any, fields []string) map[string]any {
	var retained map[string]any
	for _, field := range fields {
		if value, ok := values[field]; ok {
			if retained == nil {
				retained = make(map[string]any)
			}
			retained[field] = value
		}
	}
	return retained
}
