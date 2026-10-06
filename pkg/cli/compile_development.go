package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow"
)

func applyDevelopmentCompileMode(config CompileConfig) CompileConfig {
	if !config.Dev {
		return config
	}
	config.Strict = true
	config.Staged = true
	config.Validate = true
	config.ValidateImages = true
	config.Actionlint = true
	config.Zizmor = true
	config.Poutine = true
	config.RunnerGuard = true
	config.Syft = true
	config.Grype = true
	config.Grant = true
	config.Yamllint = true
	config.Shellcheck = true
	config.Models = true
	return config
}

func validateDevelopmentCompileMode(config CompileConfig) error {
	if !config.Dev {
		return nil
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
			return fmt.Errorf("--dev cannot be combined with %s: development testing requires emitted lock files, complete checks, and an unchanged safe-update baseline", option.name)
		}
	}
	return nil
}

func enforceDevelopmentDiagnostics(config CompileConfig, compiler *workflow.Compiler, stats *CompilationStats, results *[]ValidationResult, scanErrors ...error) error {
	if !config.Dev {
		return nil
	}
	diagnostics := append([]string{}, compiler.GetSafeUpdateWarnings()...)
	diagnostics = append(diagnostics, compiler.GetScheduleWarnings()...)
	if stats.Warnings > 0 || compiler.GetWarningCount() > 0 {
		diagnostics = append(diagnostics, "compiler reported warnings; review the complete compiler diagnostics")
	}
	for _, result := range *results {
		for _, warning := range result.Warnings {
			diagnostics = append(diagnostics, warning.Message)
		}
	}
	for _, err := range scanErrors {
		if err != nil {
			diagnostics = append(diagnostics, err.Error())
		}
	}
	if len(diagnostics) == 0 {
		return nil
	}

	err := errors.New("development testing checks failed; resolve all warnings and scanner failures before uploading or dispatching a live test:\n" + strings.Join(diagnostics, "\n"))
	entries := *results
	for i := range entries {
		result := &entries[i]
		if !result.Valid {
			continue
		}
		result.Valid = false
		result.Errors = append(result.Errors, ValidationIssue{
			Type:    "development_validation",
			Message: err.Error(),
		})
		stats.Errors++
		if stats.Succeeded > 0 {
			stats.Succeeded--
		}
		trackWorkflowFailure(stats, result.Workflow, 1, []string{err.Error()})
	}
	return err
}
