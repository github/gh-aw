package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/spf13/cobra"
)

type operationalValueConfig struct {
	Start         string
	End           string
	Ref           string
	LogsDir       string
	ValueFunction string
	Repo          string
}

var valueWorkflowID = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

const operationalValueHelp = `Report delivery and adoption for any workflow using the same native lifecycle
records and rules as logs --adoption. Existing records are read from
logs-dir/lifecycle.jsonl; collect or refresh them with logs --adoption first.

Value is separate and workflow-specific. Without a value-function definition,
delivery/adoption remain available and value is explicitly unavailable.
Definitions are loaded from --value-function or
.github/aw/value/WORKFLOW.json. A restricted, synchronous JavaScript
function measures explicitly selected Git snapshot files, without network,
process, environment, clock, random, or direct filesystem access.
The slides' schema-feature-coverage example has a built-in value function.

One shared pipeline handles weekly/daily sampling, native adoption joins,
isolated merge windows, and full-window file survival. Attribution currently
requires exact human-adopted PR merges; other output types retain their native
delivery/adoption reports without invented value.

Historical measurements require a full checkout and an up-to-date main ref.
Dates select 12:00 UTC snapshots. No Git fetch, workflow dispatch, lifecycle
mutation, or run-scoped grader replay is performed.`

// NewValueCommand creates the longitudinal value report command.
func NewValueCommand() *cobra.Command {
	config := operationalValueConfig{}
	var jsonOutput bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "value <workflow>",
		Short: "Report measured longitudinal operational value",
		Long:  operationalValueHelp,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` value weekly-research --repo owner/repo --json
  ` + string(constants.CLIExtensionPrefix) + ` value docs-updater --value-function value.json --json
  ` + string(constants.CLIExtensionPrefix) + ` value schema-feature-coverage --start 2026-03-22 --end 2026-11-17`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeout <= 0 {
				return errors.New("--timeout must be greater than zero")
			}
			now := time.Now().UTC()
			start, end, err := operationalValueDates(config, now)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			definition, err := resolveValueWorkflow(args[0], config.Repo) //nolint:uncheckedsliceindex // Cobra ExactArgs validates one argument.
			if err != nil {
				return err
			}
			report, measurement, err := collectOperationalValueReport(ctx, config, definition, start, end, now)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(report)
			}
			return renderOperationalValueReport(cmd.OutOrStdout(), measurement, report)
		},
	}
	cmd.Flags().StringVar(&config.Start, "start", "", "First snapshot date (YYYY-MM-DD, 12:00 UTC; default 30 days before end)")
	cmd.Flags().StringVar(&config.End, "end", "", "Final snapshot date (YYYY-MM-DD, 12:00 UTC; default yesterday)")
	cmd.Flags().StringVar(&config.Ref, "ref", "origin/main", "Local main-history ref to replay (no fetch)")
	cmd.Flags().StringVar(&config.LogsDir, "logs-dir", defaultLogsOutputDir, "Directory containing native lifecycle.jsonl adoption records")
	cmd.Flags().StringVar(&config.ValueFunction, "value-function", "", "Workflow-specific JSON definition containing selected paths and a restricted JavaScript value function")
	cmd.Flags().StringVarP(&config.Repo, "repo", "r", "", "Repository in [HOST/]OWNER/REPO format (defaults to the current repository)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Maximum report collection duration")
	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output the report as JSON")
	cmd.SetOut(os.Stdout)
	return cmd
}

func collectOperationalValueReport(ctx context.Context, config operationalValueConfig, definition valueMeasurementDefinition, start, end, now time.Time) (operationalValueReport, valueMeasurement, error) {
	report := operationalValueReport{}
	records, err := loadLifecycleRecords(filepath.Join(config.LogsDir, "lifecycle.jsonl"))
	if err != nil {
		return report, nil, err
	}
	root, rootErr := gitutil.FindGitRoot()
	if rootErr != nil && !errors.Is(rootErr, gitutil.ErrNotGitRepository) {
		return report, nil, rootErr
	}
	measurement, cleanup, err := selectValueMeasurement(ctx, definition, root, config.ValueFunction)
	if err != nil {
		return report, nil, err
	}
	defer cleanup()
	report = newOperationalValueReport(definition, start, end, now, records)
	if measurement == nil {
		return report, nil, nil
	}
	if rootErr != nil {
		return report, nil, fmt.Errorf("value-function history requires a local checkout: %w", rootErr)
	}
	git := valueGit{ctx: ctx, dir: root}
	tip, err := git.prepare(measurement.definition(), config.Ref, end)
	if err != nil {
		return report, nil, err
	}
	outputs, err := collectValueOutputs(ctx, definition)
	if err != nil {
		return report, nil, err
	}
	report, err = buildOperationalValueReport(git, measurement, tip, start, end, now, outputs, records)
	return report, measurement, err
}

func resolveValueWorkflow(arg, repository string) (valueMeasurementDefinition, error) {
	workflow := normalizeLogsWorkflowID(arg)
	if !valueWorkflowID.MatchString(workflow) {
		return valueMeasurementDefinition{}, errors.New("workflow must be a workflow ID or workflow file path")
	}
	if repository == "" {
		var err error
		repository, err = GetCurrentRepoSlug()
		if err != nil {
			return valueMeasurementDefinition{}, err
		}
	}
	repo, _ := repoutil.NormalizeRepoForAPI(repository)
	host := outcomeAPIHostForRepo(repository)
	parsed, err := url.Parse("https://" + host)
	if err != nil || parsed.Host != host || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || !localRepoSlugPattern.MatchString(repo) {
		return valueMeasurementDefinition{}, errors.New("--repo must be in [HOST/]OWNER/REPO format")
	}
	for part := range strings.SplitSeq(repo, "/") {
		if part == "." || part == ".." {
			return valueMeasurementDefinition{}, errors.New("--repo must not contain traversal components")
		}
	}
	return valueMeasurementDefinition{
		Workflow: workflow, WorkflowPath: logsWorkflowsPathSegment + workflow + ".lock.yml", Repository: repo, Host: host,
	}, nil
}

func selectValueMeasurement(ctx context.Context, definition valueMeasurementDefinition, root, path string) (valueMeasurement, func(), error) {
	explicit := path != ""
	if path == "" && root != "" {
		path = filepath.Join(root, ".github", "aw", "value", definition.Workflow+".json")
	}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return loadScriptValueMeasurement(ctx, definition, path)
		} else if explicit || !errors.Is(err, os.ErrNotExist) {
			return nil, func() {}, fmt.Errorf("read value-function definition: %w", err)
		}
	}
	builtin := schemaCoverageMeasurement{}
	info := builtin.definition()
	if definition.Host == info.Host && definition.Repository == info.Repository && definition.Workflow == info.Workflow {
		return builtin, func() {}, nil
	}
	return nil, func() {}, nil
}

func operationalValueDates(config operationalValueConfig, now time.Time) (time.Time, time.Time, error) {
	if config.End == "" {
		config.End = now.UTC().AddDate(0, 0, -1).Format(time.DateOnly)
	}
	end, endErr := time.Parse(time.DateOnly, config.End)
	if config.Start == "" && endErr == nil {
		config.Start = end.AddDate(0, 0, -30).Format(time.DateOnly)
	}
	start, startErr := time.Parse(time.DateOnly, config.Start)
	start, end = start.Add(12*time.Hour), end.Add(12*time.Hour)
	if startErr != nil || endErr != nil || end.Before(start) || end.After(now) {
		return time.Time{}, time.Time{}, errors.New("--start and --end must be YYYY-MM-DD dates in order with the final 12:00 UTC snapshot in the past")
	}
	if end.Sub(start) > 2*365*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("value supports at most two years of weekly snapshots")
	}
	return start, end, nil
}

func renderOperationalValueReport(out io.Writer, measurement valueMeasurement, report operationalValueReport) error {
	fields := "unavailable"
	if report.AttributedValue != nil {
		fields = fmt.Sprintf("%g %s (verified subtotal)", *report.AttributedValue, report.Measurement.Unit)
	}
	outcome, name, delivered, adopted, linked, merged := "unavailable", "not configured", "unavailable", "unavailable", "not collected", "not collected"
	if measurement != nil {
		if len(report.Weekly) == 0 {
			return errors.New("configured value report has no historical snapshots")
		}
		name = report.Measurement.Name
		outcome = measurement.format(report.Weekly[0]) + " -> " + measurement.format(report.Weekly[len(report.Weekly)-1]) //nolint:uncheckedsliceindex // Weekly is nonempty above.
		linked, merged = strconv.Itoa(len(report.Outputs)), strconv.Itoa(len(report.Windows))
	}
	if report.Lifecycle.Delivered != nil {
		delivered = fmt.Sprintf("%d (retained subset)", *report.Lifecycle.Delivered)
		adopted = fmt.Sprintf("%d human, %d delivery fallback", report.Lifecycle.Adoption.AcceptedHuman, report.Lifecycle.Adoption.AcceptedFallback)
	}
	summary := struct {
		Workflow    string `console:"header:Workflow"`
		Period      string `console:"header:Period (UTC noon)"`
		Measurement string `console:"header:Measurement"`
		Coverage    string `console:"header:Outcome (all authors)"`
		Delivered   string `console:"header:Native deliveries"`
		Adopted     string `console:"header:Native adoption"`
		Outputs     string `console:"header:Producer-linked pull requests"`
		Merged      string `console:"header:Merged (adoption proxy)"`
		Attributed  string `console:"header:Attributed value"`
		Attribution string `console:"header:Attribution coverage"`
	}{
		report.Measurement.Workflow, report.Start.Format(time.DateOnly) + " to " + report.End.Format(time.DateOnly),
		name, outcome, delivered, adopted, linked, merged, fields, report.AttributionCoverage,
	}
	if _, err := fmt.Fprint(out, console.RenderStructStdout(summary)); err != nil {
		return err
	}
	if report.Reason != "" {
		_, err := fmt.Fprintf(out, "Value unavailable: %s. Declare a value function to enable historical measurement.\n", report.Reason)
		return err
	}
	return renderOperationalValueWindows(out, report)
}

func renderOperationalValueWindows(out io.Writer, report operationalValueReport) error {
	type windowRow struct {
		PR     int    `console:"header:Pull request"`
		Merge  string `console:"header:Merge (UTC)"`
		Change string `console:"header:Merge change"`
		Status string `console:"header:Attribution"`
		Reason string `console:"header:Evidence"`
	}
	rows := make([]windowRow, 0, len(report.Windows))
	for _, window := range report.Windows {
		change := "unavailable"
		if window.Change.Value != nil {
			change = fmt.Sprintf("%g %s", *window.Change.Value, report.Measurement.Unit)
		}
		rows = append(rows, windowRow{window.PR, window.MergedAt.Format(time.RFC3339), change, window.Status, window.Reason})
	}
	if _, err := fmt.Fprint(out, console.RenderStructStdout(rows)); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "Retrospective measurement, not proof of saved time or exclusive causality. PR search is not complete native delivery history. Use --json for weekly and daily samples.")
	return err
}
