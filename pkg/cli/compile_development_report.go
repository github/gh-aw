package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

const dryRunScopeMessage = "Dry-run is compile-only: compiler-managed GitHub mutations and OTLP telemetry configuration are disabled; OTEL_* and GH_AW_OTLP_* environment variables are removed. Custom scripts/jobs, agent shell commands, external MCP servers, and custom credentials remain unverified. Compilation does not authorize execution or approve emitted files."

// DryRunCompileReport describes the compilation gate, not execution authorization.
type DryRunCompileReport struct {
	Gate                    string                        `json:"gate"`
	CompileOnly             bool                          `json:"compile_only"`
	ExecutionAuthorized     bool                          `json:"execution_authorized"`
	RequiredFlags           []string                      `json:"required_flags"`
	Scanners                map[string]DryRunScannerCheck `json:"scanners"`
	ValidateImagesRequired  bool                          `json:"validate_images_required"`
	ModelInventoryAvailable bool                          `json:"model_inventory_available"`
	UnverifiedEffects       []string                      `json:"unverified_effects"`
}

// DryRunScannerCheck records an invocation against emitted inputs.
type DryRunScannerCheck struct {
	Requested bool   `json:"requested"`
	Status    string `json:"status"`
}

func newDryRunCompileReport(config CompileConfig) *DryRunCompileReport {
	report := &DryRunCompileReport{
		Gate: "failed", CompileOnly: true, RequiredFlags: DryRunRequiredBoolFlags(),
		Scanners:               make(map[string]DryRunScannerCheck),
		ValidateImagesRequired: config.ValidateImages,
		UnverifiedEffects: []string{
			"custom scripts/jobs", "agent shell commands", "external MCP servers", "custom credentials",
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
	valid := stats.Errors == 0 && stats.Warnings == 0 && stats.Succeeded > 0
	for _, result := range *results {
		valid = valid && result.Valid && len(result.Errors) == 0 && len(result.Warnings) == 0
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
		statuses = append(statuses, name+"="+report.Scanners[name].Status)
	}
	message := fmt.Sprintf("Dry-run compilation gate: %s. Scanner coverage: %s. Unverified: %s.",
		report.Gate, strings.Join(statuses, ", "), strings.Join(report.UnverifiedEffects, ", "))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(message))
}
