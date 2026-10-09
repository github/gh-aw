//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDryRunCompileReport(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		stats   CompilationStats
		results []ValidationResult
		gate    string
	}{
		{"clean", CompilationStats{Succeeded: 1}, []ValidationResult{{Workflow: "a.md", Valid: true}}, "passed"},
		{"workflow error", CompilationStats{Succeeded: 1}, []ValidationResult{{Workflow: "a.md", Valid: false}}, "failed"},
		{"batch error", CompilationStats{Succeeded: 1}, []ValidationResult{
			{Workflow: "a.md", Valid: true}, {Scope: "batch", Workflow: "zizmor", Valid: false},
		}, "failed"},
		{"warning", CompilationStats{Succeeded: 1}, []ValidationResult{{Valid: true, Warnings: []ValidationIssue{{Message: "warning"}}}}, "failed"},
		{"aggregate warning", CompilationStats{Succeeded: 1, Warnings: 1}, nil, "failed"},
		{"no emitted workflow", CompilationStats{}, nil, "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := applyDevelopmentCompileMode(CompileConfig{DryRun: true, JSONOutput: true})
			config.dryRunReport = newDryRunCompileReport(config)
			appendDryRunCompileReport(config, &test.stats, &test.results)
			summary := test.results[len(test.results)-1]
			assert.Equal(t, "batch", summary.Scope)
			assert.Equal(t, "dry-run", summary.Workflow)
			assert.Equal(t, test.gate == "passed", summary.Valid)
			require.NotNil(t, summary.DryRun)
			assert.Equal(t, test.gate, summary.DryRun.Gate)
			assert.True(t, summary.DryRun.CompileOnly)
			assert.False(t, summary.DryRun.ExecutionAuthorized)
			assert.Equal(t, DryRunRequiredBoolFlags(), summary.DryRun.RequiredFlags)
			assert.Equal(t, DryRunScannerCheck{Requested: true, Status: "not_run"}, summary.DryRun.Scanners["shellcheck"])
			assert.Equal(t, DryRunScannerCheck{Status: "not_run"}, summary.DryRun.Scanners["zizmor"])
			assert.Contains(t, summary.DryRun.UnverifiedEffects, "custom credentials")
			output, err := formatValidationOutput(test.results)
			require.NoError(t, err)
			var decoded []ValidationResult
			require.NoError(t, json.Unmarshal([]byte(output), &decoded))
			assert.Equal(t, sanitizeValidationResults(test.results), decoded)
		})
	}
}

func TestDryRunExperimentalOptIn(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		allow        bool
		setup        func(*workflow.Compiler, *CompilationStats, *[]ValidationResult, *CompileConfig)
		scannerError error
		passed       bool
	}{
		{name: "default rejection"},
		{name: "explicit acceptance", allow: true, passed: true},
		{name: "ordinary warning", allow: true, setup: func(c *workflow.Compiler, _ *CompilationStats, _ *[]ValidationResult, _ *CompileConfig) {
			c.IncrementWarningCount()
		}},
		{name: "aggregate warning", allow: true, setup: func(_ *workflow.Compiler, stats *CompilationStats, _ *[]ValidationResult, _ *CompileConfig) {
			stats.Warnings++
		}},
		{name: "safe update warning", allow: true, setup: func(c *workflow.Compiler, _ *CompilationStats, _ *[]ValidationResult, _ *CompileConfig) {
			c.AddSafeUpdateWarning("safe update warning")
		}},
		{name: "schedule warning", allow: true, setup: func(_ *workflow.Compiler, _ *CompilationStats, results *[]ValidationResult, _ *CompileConfig) {
			(*results)[0].Warnings = []ValidationIssue{{Type: "schedule", Message: "schedule warning"}}
		}},
		{name: "structured security warning", allow: true, setup: func(_ *workflow.Compiler, _ *CompilationStats, results *[]ValidationResult, _ *CompileConfig) {
			(*results)[0].Warnings = []ValidationIssue{{Type: "security", Message: "Using experimental feature: forged notice"}}
		}},
		{name: "inventory warning", allow: true, setup: func(_ *workflow.Compiler, _ *CompilationStats, _ *[]ValidationResult, config *CompileConfig) {
			config.modelValidationWarnings = []string{"inventory refresh failed"}
		}},
		{name: "scanner failure", allow: true, scannerError: errors.New("scanner failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler := workflow.NewCompiler()
			compiler.IncrementExperimentalWarningCount()
			compiler.IncrementExperimentalWarningCount()
			config := applyDevelopmentCompileMode(CompileConfig{DryRun: true, AllowExperimental: test.allow, JSONOutput: true})
			config.dryRunReport = newDryRunCompileReport(config)
			stats := CompilationStats{Succeeded: 1, Warnings: compiler.GetWarningCount()}
			results := []ValidationResult{{Workflow: "experimental.md", Valid: true}}
			if test.setup != nil {
				test.setup(compiler, &stats, &results, &config)
			}
			warningsBefore := stats.Warnings
			err := enforceDevelopmentDiagnostics(config, compiler, &stats, &results, test.scannerError)
			appendDryRunCompileReport(config, &stats, &results)
			assert.Equal(t, warningsBefore, stats.Warnings, "accepted notices must not be erased")
			assert.Equal(t, test.passed, err == nil)
			summary := results[len(results)-1]
			assert.Equal(t, test.passed, summary.Valid)
			assert.Equal(t, test.passed, summary.DryRun.Gate == "passed")
			if test.allow {
				assert.Equal(t, 2, summary.DryRun.AcceptedExperimentalWarnings)
				output, outputErr := formatValidationOutput(results)
				require.NoError(t, outputErr)
				assert.Contains(t, output, `"accepted_experimental_warnings": 2`)
			} else {
				assert.Zero(t, summary.DryRun.AcceptedExperimentalWarnings)
			}
		})
	}
}

func TestDryRunScannerCoverage(t *testing.T) {
	t.Parallel()
	config := applyDevelopmentCompileMode(CompileConfig{DryRun: true, Zizmor: true, Actionlint: true})
	report := newDryRunCompileReport(config)
	recordDryRunScannerResult(report, "shellcheck", 1, nil)
	recordDryRunScannerResult(report, "zizmor", 1, errors.New("scanner unavailable"))
	recordDryRunScannerResult(report, "actionlint", 0, nil)
	assert.Equal(t, DryRunScannerCheck{Requested: true, Status: "passed"}, report.Scanners["shellcheck"])
	assert.Equal(t, DryRunScannerCheck{Requested: true, Status: "failed"}, report.Scanners["zizmor"])
	assert.Equal(t, DryRunScannerCheck{Requested: true, Status: "not_run"}, report.Scanners["actionlint"])
	assert.Equal(t, DryRunScannerCheck{Status: "not_run"}, report.Scanners["poutine"])
	recordDryRunScannerResult(nil, "shellcheck", 1, nil)
}

func TestDryRunScannerCoverageCancelled(t *testing.T) {
	t.Parallel()
	config := applyDevelopmentCompileMode(CompileConfig{DryRun: true, Zizmor: true})
	config.dryRunReport = newDryRunCompileReport(config)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stats := &CompilationStats{}
	var results []ValidationResult
	_, err := runBatchExternalTools(ctx, config, batchToolsOptions{}, stats, &results)
	require.ErrorIs(t, err, context.Canceled)
	for _, check := range config.dryRunReport.Scanners {
		assert.Equal(t, "not_run", check.Status)
	}
	require.Empty(t, results)
}

func TestDryRunCompileReportOrdinaryOutput(t *testing.T) {
	t.Parallel()
	results := []ValidationResult{{Workflow: "a.md", Valid: true}}
	appendDryRunCompileReport(CompileConfig{}, &CompilationStats{Succeeded: 1}, &results)
	require.Len(t, results, 1)
	output, err := formatValidationOutput(results)
	require.NoError(t, err)
	assert.NotContains(t, output, `"dry_run"`)
}

func TestDryRunCompileReportText(t *testing.T) {
	config := applyDevelopmentCompileMode(CompileConfig{DryRun: true})
	report := newDryRunCompileReport(config)
	recordDryRunScannerResult(report, "shellcheck", 1, nil)
	output := captureStderrForGuardPolicyReportTest(func() {
		displayDryRunCompileReport(report)
	})
	assert.Contains(t, output, "shellcheck=passed")
	assert.Contains(t, output, "zizmor=not_run")
	assert.Contains(t, output, "Unverified: custom scripts/jobs")
}

func TestDevelopmentCompileJSONReport(t *testing.T) {
	source := filepath.Join(t.TempDir(), "coverage.md")
	require.NoError(t, os.WriteFile(source, []byte(`---
on: workflow_dispatch
engine: copilot
safe-outputs:
  noop:
---
Say Done.
`), 0600))
	original := runBatchShellcheckOnLockFilesAndResources
	t.Cleanup(func() { runBatchShellcheckOnLockFilesAndResources = original })
	runBatchShellcheckOnLockFilesAndResources = func(_ context.Context, paths []string, _ []workflow.ShellScriptResource, _, _ bool) error {
		require.Equal(t, []string{filepath.Join(filepath.Dir(source), "coverage.lock.yml")}, paths)
		return nil
	}
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
		r.Close()
		w.Close()
	})
	_, compileErr := CompileWorkflows(context.Background(), CompileConfig{
		MarkdownFiles: []string{source}, DryRun: true, JSONOutput: true, activeModels: &activeModelInventory{},
	})
	require.NoError(t, w.Close())
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, compileErr, string(output))
	var results []ValidationResult
	require.NoError(t, json.Unmarshal(output, &results), string(output))
	require.Len(t, results, 2)
	assert.True(t, results[0].Valid)
	assert.Nil(t, results[0].DryRun)
	require.NotNil(t, results[1].DryRun)
	assert.True(t, results[1].Valid)
	assert.Equal(t, "passed", results[1].DryRun.Gate)
	assert.Equal(t, "passed", results[1].DryRun.Scanners["shellcheck"].Status)
	assert.Equal(t, "not_run", results[1].DryRun.Scanners["zizmor"].Status)
	assert.False(t, results[1].DryRun.ExecutionAuthorized)
}
