package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

const maximumHealthAuditInputBytes = 16 * 1024 * 1024

type healthAuditInput struct {
	Collection *struct {
		Partial                *bool  `json:"partial"`
		Host                   string `json:"host"`
		FailureListingComplete *bool  `json:"failure_listing_complete"`
		FailurePassComplete    *bool  `json:"failure_pass_complete"`
	} `json:"collection"`
	FailureInvestigation *FailureInvestigation `json:"failure_investigation"`
	FailureClusters      []FailureCluster      `json:"failure_clusters"`
}

// HealthAuditReport preserves the bounded discovery evidence alongside each selected audit.
type HealthAuditReport struct {
	Health           json.RawMessage  `json:"health"`
	Complete         bool             `json:"complete"`
	Partial          bool             `json:"partial"`
	Audits           []HealthAuditRun `json:"audits"`
	RemainingTargets int              `json:"remaining_targets"`
	Error            string           `json:"error,omitempty"`
}

type HealthAuditRun struct {
	Target FailureInvestigationTarget `json:"target"`
	Report AuditData                  `json:"report"`
}

func registerAuditHealthFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("health", false, "Select runs with revision-aware health discovery; with --stdin, consume health --json without rescanning")
	cmd.Flags().Int("health-limit", defaultFailureInvestigationBudget, "Maximum selected health targets to audit (1-5)")
	cmd.Flags().Duration("health-timeout", 2*time.Minute, "Time budget for health discovery before auditing")
	cmd.Flags().String("start-date", "", "Health discovery creation-time start (default: -24h; requires --health)")
	cmd.Flags().String("end-date", "", "Health discovery creation-time end (default: now; requires --health)")
}

func validateAuditHealthOnlyFlags(cmd *cobra.Command) error {
	for _, name := range []string{"health-limit", "health-timeout", "start-date", "end-date"} {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s requires --health", name)
		}
	}
	return nil
}

func validateAuditHealthCommand(cmd *cobra.Command, args []string, opts auditCommandOptions) error {
	if opts.healthLimit < 1 || opts.healthLimit > defaultFailureInvestigationBudget {
		return errors.New("--health-limit must be between 1 and 5")
	}
	if opts.group || opts.search != "" || opts.models || opts.evalsOnly ||
		opts.experimentFilter != "" || opts.variantFilter != "" || opts.runtimeFilter != "" || opts.parse {
		return errors.New("--health cannot be combined with run filters, --group, --parse, --search, or --models")
	}
	if len(args) > 1 || (opts.stdin && len(args) != 0) {
		return errors.New("--health accepts at most one workflow, or --stdin without positional arguments")
	}
	if opts.stdin {
		for _, name := range []string{"health-timeout", "start-date", "end-date"} {
			if cmd.Flags().Changed(name) {
				return fmt.Errorf("--%s cannot rescope a health report supplied through --stdin", name)
			}
		}
	}
	if getDurationFlag(cmd, "health-timeout") < 0 {
		return errors.New("--health-timeout must not be negative")
	}
	return nil
}

func runAuditHealthCommand(cmd *cobra.Command, args []string, opts auditCommandOptions) error {
	if err := validateAuditHealthCommand(cmd, args, opts); err != nil {
		return err
	}
	var raw json.RawMessage
	var err error
	if opts.stdin {
		raw, err = readHealthAuditInput(cmd.InOrStdin())
	} else {
		raw, err = collectAuditHealth(cmd.Context(), cmd, args, opts)
	}
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		defer cancel()
	}
	report, auditErr := auditHealthTargets(ctx, raw, opts, AuditWorkflowRun)
	if opts.jsonOutput {
		if err := json.NewEncoder(healthAuditJSONWriter(cmd)).Encode(report); err != nil {
			return errors.Join(auditErr, fmt.Errorf("failed to encode health audit: %w", err))
		}
	} else {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Health audit: %d selected runs audited; %d targets remain; complete=%t\n",
			len(report.Audits), report.RemainingTargets, report.Complete); err != nil {
			return errors.Join(auditErr, fmt.Errorf("failed to write health audit summary: %w", err))
		}
		for _, run := range report.Audits {
			renderConsole(run.Report, run.Report.Overview.LogsPath)
		}
	}
	return auditErr
}

func healthAuditJSONWriter(cmd *cobra.Command) io.Writer {
	writer := cmd.OutOrStdout()
	// The root sends help to stderr; machine-readable reports belong on stdout.
	if writer == os.Stderr {
		return os.Stdout
	}
	return writer
}

func collectAuditHealth(ctx context.Context, cmd *cobra.Command, args []string, opts auditCommandOptions) (json.RawMessage, error) {
	timeout := getDurationFlag(cmd, "health-timeout")
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	config := HealthConfig{
		Days: 7, Threshold: 80, StartDate: getStringFlag(cmd, "start-date"),
		EndDate: getStringFlag(cmd, "end-date"), RepoOverride: opts.repoFlag,
		LogsDir: opts.outputDir, Timeout: timeout, Verbose: opts.verbose,
	}
	if config.StartDate == "" {
		config.StartDate = "-24h"
	}
	if len(args) > 0 {
		config.WorkflowName = args[0]
	}
	runs, config, err := collectHealthRuns(ctx, config, time.Now())
	if err != nil {
		return nil, err
	}
	return json.Marshal(buildHealthSummary(runs, config))
}

func readHealthAuditInput(reader io.Reader) (json.RawMessage, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maximumHealthAuditInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read health JSON: %w", err)
	}
	if len(raw) > maximumHealthAuditInputBytes {
		return nil, errors.New("health JSON exceeds 16 MiB")
	}
	if !json.Valid(raw) || !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		return nil, errors.New("--health --stdin requires one valid health JSON report")
	}
	return raw, nil
}

func auditHealthTargets(ctx context.Context, raw json.RawMessage, opts auditCommandOptions,
	audit func(context.Context, int64, AuditOptions) (bool, error)) (HealthAuditReport, error) {
	report := HealthAuditReport{Health: raw, Audits: []HealthAuditRun{}}
	targets, err := resolveHealthAuditTargets(raw, opts)
	report.RemainingTargets = len(targets)
	if err != nil {
		report.Partial, report.Error = true, err.Error()
		return report, err
	}
	for _, target := range targets[:min(len(targets), opts.healthLimit)] {
		data, err := auditHealthTarget(ctx, target, opts, audit)
		if err != nil {
			report.Partial, report.Error = true, err.Error()
			return report, err
		}
		report.Audits = append(report.Audits, HealthAuditRun{Target: target, Report: data})
		report.RemainingTargets--
	}
	report.Complete = true
	return report, nil
}

func resolveHealthAuditTargets(raw json.RawMessage, opts auditCommandOptions) ([]FailureInvestigationTarget, error) {
	var input healthAuditInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, fmt.Errorf("invalid health report: %w", err)
	}
	if input.Collection == nil || input.Collection.Partial == nil || input.Collection.Host == "" {
		return nil, errors.New("health report is missing collection completeness or host metadata")
	}
	if *input.Collection.Partial || input.Collection.FailureListingComplete == nil ||
		!*input.Collection.FailureListingComplete || input.Collection.FailurePassComplete == nil ||
		!*input.Collection.FailurePassComplete {
		return nil, errors.New("health discovery is partial; no audits started")
	}
	if input.FailureInvestigation == nil {
		if len(input.FailureClusters) > 0 {
			return nil, errors.New("health report is missing its failure investigation plan")
		}
		return nil, nil
	}
	targets := input.FailureInvestigation.SelectedRuns
	if len(targets) > defaultFailureInvestigationBudget {
		return nil, errors.New("health report exceeds the five-target audit budget")
	}
	seen := make(map[string]struct{})
	clusters := make(map[string]FailureCluster)
	for _, cluster := range input.FailureClusters {
		if cluster.ID == "" || clusters[cluster.ID].ID != "" {
			return nil, errors.New("health report has missing or duplicate cluster identities")
		}
		clusters[cluster.ID] = cluster
	}
	for _, target := range targets {
		components, err := parser.ParseRunURLExtended(target.URL)
		attempt, attemptErr := strconv.Atoi(target.Attempt)
		if err != nil || components.Number != target.RunID || components.Host != input.Collection.Host ||
			components.JobID != 0 || path.Join(components.Owner, components.Repo) != target.Repository ||
			(opts.repoFlag != "" && opts.repoFlag != target.Repository &&
				opts.repoFlag != path.Join(input.Collection.Host, target.Repository)) ||
			attemptErr != nil || attempt < 1 || target.UpdatedAt.IsZero() || len(target.ClusterIDs) == 0 {
			return nil, fmt.Errorf("invalid or out-of-scope health target %d", target.RunID)
		}
		for _, id := range target.ClusterIDs {
			cluster, exists := clusters[id]
			if !exists || cluster.IntentionalFailure {
				return nil, fmt.Errorf("health target %d references a missing or intentional-failure cluster", target.RunID)
			}
		}
		key := path.Join(target.Repository, strconv.FormatInt(target.RunID, 10))
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate health target %d", target.RunID)
		}
		seen[key] = struct{}{}
	}
	return targets, nil
}

func auditHealthTarget(ctx context.Context, target FailureInvestigationTarget, opts auditCommandOptions,
	audit func(context.Context, int64, AuditOptions) (bool, error)) (AuditData, error) {
	components, err := parser.ParseRunURLExtended(target.URL)
	if err != nil {
		return AuditData{}, err
	}
	var data AuditData
	captured := false
	skipped, err := audit(ctx, target.RunID, AuditOptions{
		Owner: components.Owner, Repo: components.Repo, Hostname: components.Host,
		OutputDir: opts.outputDir, Verbose: opts.verbose, ArtifactSets: opts.artifacts,
		NoBaseline: opts.noBaseline,
		report: func(result AuditData) error {
			if result.Overview.RunID != target.RunID ||
				strconv.Itoa(result.Overview.RunAttempt) != target.Attempt ||
				!result.Overview.UpdatedAt.Equal(target.UpdatedAt) ||
				!strings.EqualFold(result.Overview.URL, target.URL) {
				return fmt.Errorf("health target %d changed during auditing", target.RunID)
			}
			data, captured = result, true
			return nil
		},
	})
	if err != nil {
		return AuditData{}, fmt.Errorf("audit health target %d: %w", target.RunID, err)
	}
	if skipped || !captured {
		return AuditData{}, fmt.Errorf("health target %d audit was skipped or produced no report", target.RunID)
	}
	return data, nil
}

func getDurationFlag(cmd *cobra.Command, name string) time.Duration {
	value, _ := cmd.Flags().GetDuration(name)
	return value
}
