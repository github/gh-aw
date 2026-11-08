package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

const dryRunScopeMessage = "Dry-run is compile-only: compiler-managed GitHub mutations, authored custom jobs, daily credit accounting, and OTLP telemetry configuration are disabled; OTEL_* and GH_AW_OTLP_* environment variables are removed. Authored steps and post-steps are omitted from emitted locks, including their code and credentials; separate dispatch does not execute them. Pre-steps, pre-agent-steps, on.steps, custom MCP servers, custom safe-output scripts, Bash graders, and authored OIDC permissions are rejected. Per-run credit limits are retained or derived from configured daily limits. Manual dispatch is enabled with maintainer/admin authorization. Agent shell commands and custom credentials remain unverified. Compilation does not authorize execution or approve emitted files."

// DryRunCompileReport describes the compilation gate, not execution authorization.
type DryRunCompileReport struct {
	Gate                         string                        `json:"gate"`
	CompileOnly                  bool                          `json:"compile_only"`
	ExecutionAuthorized          bool                          `json:"execution_authorized"`
	RequiredFlags                []string                      `json:"required_flags"`
	Scanners                     map[string]DryRunScannerCheck `json:"scanners"`
	ValidateImagesRequired       bool                          `json:"validate_images_required"`
	ModelInventoryAvailable      bool                          `json:"model_inventory_available"`
	UnverifiedEffects            []string                      `json:"unverified_effects"`
	DisabledEffects              []string                      `json:"disabled_effects"`
	AcceptedExperimentalWarnings int                           `json:"accepted_experimental_warnings,omitempty"`
	IgnoredLowSeverityWarnings   int                           `json:"ignored_low_severity_warnings,omitempty"`
	WarningBaseline              string                        `json:"warning_baseline"`
	MinimumSeverity              string                        `json:"minimum_severity"`
	IgnoredScannerRules          map[string][]string           `json:"ignored_scanner_rules"`
}

// DryRunScannerCheck records an invocation against emitted inputs.
type DryRunScannerCheck struct {
	Requested    bool                       `json:"requested"`
	Status       string                     `json:"status"`
	Differential *DryRunScannerDifferential `json:"differential,omitempty"`
}

func newDryRunCompileReport(config CompileConfig) *DryRunCompileReport {
	report := &DryRunCompileReport{
		Gate: "failed", CompileOnly: true, RequiredFlags: DryRunRequiredBoolFlags(),
		WarningBaseline: "pre_compile_lock_files", MinimumSeverity: "medium",
		IgnoredScannerRules:    map[string][]string{"zizmor": {"secrets-outside-env"}},
		Scanners:               make(map[string]DryRunScannerCheck),
		ValidateImagesRequired: config.ValidateImages,
		DisabledEffects:        []string{"steps", "post-steps"},
		UnverifiedEffects: []string{
			"agent shell commands", "custom engine execution", "custom credentials", "compiler-managed OIDC integrations",
			"hosted approvals/OIDC/runners", "live model availability", "diagnostic artifacts/summaries",
		},
	}
	for name, requested := range map[string]bool{
		"shellcheck": config.Shellcheck, "actionlint": config.Actionlint, "zizmor": config.Zizmor,
		"poutine": config.Poutine, "runner-guard": config.RunnerGuard, "syft": config.Syft,
		"grype": config.Grype, "grant": config.Grant, "yamllint": config.Yamllint,
	} {
		report.Scanners[name] = DryRunScannerCheck{Requested: requested, Status: "not_run"}
	}
	return report
}

func recordDryRunScannerResult(report *DryRunCompileReport, tool string, inputs int, err error) {
	if report == nil {
		return
	}
	check := report.Scanners[tool]
	if err != nil {
		check.Status = "failed"
	} else if inputs > 0 {
		check.Status = "passed"
	}
	report.Scanners[tool] = check
}

func appendDryRunCompileReport(config CompileConfig, stats *CompilationStats, results *[]ValidationResult) {
	if !config.DryRun {
		return
	}
	report := config.dryRunReport
	if report == nil {
		report = newDryRunCompileReport(config)
	}
	report.ModelInventoryAvailable = config.activeModels != nil
	valid := stats.Errors == 0 && stats.Warnings <= report.IgnoredLowSeverityWarnings && stats.Succeeded > 0
	for _, result := range *results {
		valid = valid && result.Valid && len(result.Errors) == 0
		for _, warning := range result.Warnings {
			valid = valid && isLowSeverityWarning(warning)
		}
	}
	if valid {
		report.Gate = "passed"
	} else {
		report.Gate = "failed"
	}
	*results = append(*results, ValidationResult{
		Scope: "batch", Workflow: "dry-run", Valid: valid,
		Errors: []ValidationIssue{}, Warnings: []ValidationIssue{}, DryRun: report,
	})
	if !config.JSONOutput {
		displayDryRunCompileReport(report)
	}
}

func displayDryRunCompileReport(report *DryRunCompileReport) {
	names := make([]string, 0, len(report.Scanners))
	for name := range report.Scanners {
		names = append(names, name)
	}
	slices.Sort(names)
	statuses := make([]string, 0, len(names))
	for _, name := range names {
		status := name + "=" + report.Scanners[name].Status
		if diff := report.Scanners[name].Differential; diff != nil {
			status = fmt.Sprintf("%s(added=%d, existing=%d, ignored=%d, baseline=%d)", status, diff.Added, diff.Existing, diff.Ignored, diff.Baseline)
		}
		statuses = append(statuses, status)
	}
	message := fmt.Sprintf("Dry-run compilation gate: %s. Scanner coverage: %s. Unverified: %s.",
		report.Gate, strings.Join(statuses, ", "), strings.Join(report.UnverifiedEffects, ", "))
	if report.AcceptedExperimentalWarnings > 0 {
		message += fmt.Sprintf(" Accepted %d experimental-feature notice(s) via --allow-experimental.", report.AcceptedExperimentalWarnings)
	}
	message += " Existing and added medium-or-higher or unclassified scanner findings fail; low-severity findings and zizmor secrets-outside-env are excluded from the gate."
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(message))
}
