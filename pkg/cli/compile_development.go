package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow"
)

// DryRunRequiredBoolFlags lists checks that development compilation cannot disable.
func DryRunRequiredBoolFlags() []string {
	return []string{
		"strict", "staged", "validate", "shellcheck", "models",
	}
}

// ValidateDevelopmentCompileFlags rejects explicit opt-outs while allowing omitted defaults.
func ValidateDevelopmentCompileFlags(dev bool, flags map[string]bool) error {
	if dev {
		for _, name := range DryRunRequiredBoolFlags() {
			if enabled, supplied := flags[name]; supplied && !enabled {
				return fmt.Errorf("--dry-run cannot be combined with --%s=false: development testing requires this check; remove --%s=false or omit --dry-run", name, name)
			}
		}
	}
	return nil
}

func applyDevelopmentCompileMode(config CompileConfig) CompileConfig {
	if !config.DryRun {
		return config
	}
	config.Strict = true
	config.Staged = true
	config.Validate = true
	config.Shellcheck = true
	config.Models = true
	return config
}

func validateDevelopmentCompileMode(config CompileConfig) error {
	if !config.DryRun {
		return nil
	}
	if err := ValidateDevelopmentCompileFlags(config.DryRun, config.ExplicitBoolFlags); err != nil {
		return err
	}
	for _, option := range []struct {
		enabled bool
		name    string
	}{
		{config.NoEmit, "--no-emit"},
		{config.Watch, "--watch"},
		{config.Approve, "--approve"},
		{config.AllowActionRefs, "--allow-action-refs"},
	} {
		if option.enabled {
			return fmt.Errorf("--dry-run cannot be combined with %s: development testing requires emitted lock files, complete checks, and an unchanged safe-update baseline", option.name)
		}
	}
	return nil
}

func enforceDevelopmentDiagnostics(config CompileConfig, compiler *workflow.Compiler, stats *CompilationStats, results *[]ValidationResult, scanErrors ...error) error {
	if !config.DryRun {
		return nil
	}
	appendDevelopmentCompilerDiagnostics(compiler, stats, results)
	for _, err := range scanErrors {
		for _, issue := range appendValidationErrors(nil, "scanner_error", err) {
			if !hasDevelopmentBatchDiagnostic(*results, issue.Message) {
				appendDevelopmentBatchDiagnostics("scanners", []ValidationIssue{issue}, stats, results)
			}
		}
	}

	var diagnostics []string
	for i := range *results {
		result := &(*results)[i]
		if result.Scope == "batch" {
			for _, issue := range result.Errors {
				diagnostics = append(diagnostics, issue.Message)
			}
			continue
		}
		for _, warning := range result.Warnings {
			diagnostics = append(diagnostics, warning.Message)
		}
		enforceDevelopmentWorkflowWarnings(result, stats)
	}
	if len(diagnostics) == 0 {
		return nil
	}
	return errors.New("development testing checks failed; resolve all warnings and scanner failures before uploading or dispatching a live test:\n" + strings.Join(diagnostics, "\n"))
}

func appendDevelopmentCompilerDiagnostics(compiler *workflow.Compiler, stats *CompilationStats, results *[]ValidationResult) {
	var compilerDiagnostics []ValidationIssue
	for _, message := range compiler.GetSafeUpdateWarnings() {
		compilerDiagnostics = append(compilerDiagnostics, ValidationIssue{Type: "safe_update_warning", Message: message})
	}
	for _, message := range compiler.GetScheduleWarnings() {
		compilerDiagnostics = append(compilerDiagnostics, ValidationIssue{Type: "schedule_warning", Message: message})
	}
	if stats.Warnings > 0 || compiler.GetWarningCount() > 0 {
		compilerDiagnostics = append(compilerDiagnostics, ValidationIssue{
			Type: "compiler_warning", Message: "compiler reported warnings; review the complete compiler diagnostics",
		})
	}
	if len(compilerDiagnostics) > 0 {
		appendDevelopmentBatchDiagnostics("compiler", compilerDiagnostics, stats, results)
	}
}

func hasDevelopmentBatchDiagnostic(results []ValidationResult, message string) bool {
	for _, result := range results {
		if result.Scope == "batch" {
			for _, issue := range result.Errors {
				if issue.Message == message {
					return true
				}
			}
		}
	}
	return false
}

func enforceDevelopmentWorkflowWarnings(result *ValidationResult, stats *CompilationStats) {
	if !result.Valid || len(result.Warnings) == 0 {
		return
	}
	result.Valid = false
	var messages []string
	for _, warning := range result.Warnings {
		result.Errors = append(result.Errors, ValidationIssue{
			Type: "development_validation", Message: warning.Message, Line: warning.Line, File: warning.File,
		})
		messages = append(messages, warning.Message)
	}
	stats.Errors += len(messages)
	if stats.Succeeded > 0 {
		stats.Succeeded--
	}
	trackWorkflowFailure(stats, result.Workflow, len(messages), messages)
}

func appendDevelopmentBatchDiagnostics(source string, issues []ValidationIssue, stats *CompilationStats, results *[]ValidationResult) {
	*results = append(*results, ValidationResult{
		Scope: "batch", Workflow: source, Valid: false, Errors: issues, Warnings: []ValidationIssue{},
	})
	stats.Errors += len(issues)
}
