//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scannerJSONTestRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".scanner-json-")
	require.NoError(t, err)
	dir, err = filepath.Abs(dir)
	require.NoError(t, err)
	require.NoError(t, initTestGitRepo(dir))
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWD))
		require.NoError(t, os.RemoveAll(dir))
	})
	// ShellCheck creates script files using os.TempDir.
	t.Setenv("TMPDIR", dir)
	return dir
}

func installScannerJSONFakes(t *testing.T, dir string) {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	for name, script := range map[string]string{
		"actionlint": `#!/bin/sh
if [ "$1" = "--version" ]; then printf '1.7.12\n'; exit 0; fi
if [ "$SCANNER_TEST_MODE" = "tooling" ]; then printf 'broken tool\n' >&2; exit 2; fi
if [ "$SCANNER_TEST_MODE" = "malformed" ]; then printf 'not JSON\n'; exit 1; fi
if [ "$SCANNER_TEST_MODE" = "malformed-success" ]; then printf 'not JSON\n'; exit 0; fi
if [ "$SCANNER_TEST_MODE" = "clean" ]; then printf '[]\n'; exit 0; fi
printf '['
sep=''
for arg do
  case "$arg" in
    *.lock.yml)
      printf '%s{"message":"%s","kind":"%s","filepath":"%s","line":1,"column":3}' \
        "$sep" "$SCANNER_TEST_MESSAGE" "$SCANNER_TEST_KIND" "$arg"
      sep=','
      ;;
  esac
done
printf ']\n'
exit 1
`,
		"shellcheck": `#!/bin/sh
if [ "$SCANNER_TEST_MODE" = "tooling" ]; then printf 'broken tool\n' >&2; exit 2; fi
if [ "$SCANNER_TEST_MODE" = "clean" ]; then exit 0; fi
for arg do script="$arg"; done
printf '%s:1:3: %s: %s [SC2086]\n' "$script" "$SCANNER_TEST_LEVEL" "$SCANNER_TEST_MESSAGE"
exit 1
`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755))
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	version, stats := actionlintVersion, actionlintStats
	actionlintVersion, actionlintStats = "", nil
	t.Cleanup(func() { actionlintVersion, actionlintStats = version, stats })
}

func captureScannerCompileJSON(t *testing.T, dir string, config CompileConfig) ([]ValidationResult, error) {
	t.Helper()
	outputPath := filepath.Join(dir, "compile.json")
	output, err := os.Create(outputPath)
	require.NoError(t, err)
	oldStdout := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = oldStdout }()
	_, compileErr := CompileWorkflows(context.Background(), config)
	require.NoError(t, output.Close())
	os.Stdout = oldStdout
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	var results []ValidationResult
	require.NoError(t, json.Unmarshal(data, &results), string(data))
	return results, compileErr
}

func TestCompileJSONOptionalScannerFindings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test scanners use POSIX shell scripts")
	}
	dir := scannerJSONTestRepo(t)
	installScannerJSONFakes(t, dir)
	var files []string
	for _, name := range []string{"first", "second"} {
		file := filepath.Join(dir, name+".md")
		content := fmt.Sprintf(`---
name: %s
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  noop:
---
Say Done.
`, name)
		require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
		files = append(files, file)
	}

	for _, tool := range []string{"actionlint", "shellcheck"} {
		for _, count := range []int{1, 2} {
			for _, strict := range []bool{false, true} {
				for _, mode := range []string{"findings", "low", "tooling", "malformed", "malformed-success", "clean"} {
					if tool == "shellcheck" && (mode == "malformed" || mode == "malformed-success") {
						continue
					}
					t.Run(fmt.Sprintf("%s/%d/strict=%t/%s", tool, count, strict, mode), func(t *testing.T) {
						t.Setenv("SCANNER_TEST_MODE", mode)
						message, kind, level := "quote this variable", "expression", "warning"
						severity := scanfindings.SeverityMedium
						if tool == "actionlint" {
							severity = scanfindings.SeverityHigh
						}
						if mode == "low" {
							if tool == "actionlint" {
								message, kind = "shellcheck reported issue in this script: SC2086:info:1:3: quote this variable", "shellcheck"
								severity = scanfindings.SeverityLow
							} else {
								level, severity = "note", scanfindings.SeverityInfo
							}
						}
						t.Setenv("SCANNER_TEST_MESSAGE", message)
						t.Setenv("SCANNER_TEST_KIND", kind)
						t.Setenv("SCANNER_TEST_LEVEL", level)
						results, compileErr := captureScannerCompileJSON(t, dir, CompileConfig{
							MarkdownFiles: files[:count], JSONOutput: true, Strict: strict,
							Actionlint: tool == "actionlint", Shellcheck: tool == "shellcheck",
						})
						assert.Equal(t, strict && mode != "clean", compileErr != nil)
						for _, result := range results[:count] {
							assert.True(t, result.Valid, "batch findings must not invalidate unrelated workflow results")
							assert.Empty(t, result.Errors)
						}
						if mode == "clean" {
							require.Len(t, results, count)
							return
						}
						require.Len(t, results, count+1)
						result := results[count]
						assert.Equal(t, "batch", result.Scope)
						assert.Equal(t, tool, result.Workflow)
						assert.Equal(t, !strict, result.Valid)
						issues := result.Warnings
						if strict {
							assert.Empty(t, result.Warnings)
							issues = result.Errors
						} else {
							assert.Empty(t, result.Errors)
						}
						require.NotEmpty(t, issues)
						if mode == "tooling" || mode == "malformed" || mode == "malformed-success" {
							for _, issue := range issues {
								assert.Equal(t, tool+"_error", issue.Type)
								assert.Equal(t, scanfindings.SeverityUnknown, issue.Severity)
								assert.Empty(t, issue.RuleID, "tool failures are not lint findings")
								assert.Zero(t, issue.Line)
							}
							return
						}
						reportedFiles := make(map[string]bool)
						for _, issue := range issues {
							assert.Equal(t, tool+"_finding", issue.Type)
							assert.Equal(t, severity, issue.Severity)
							assert.Equal(t, message, issue.Message)
							if tool == "actionlint" {
								assert.Equal(t, kind, issue.RuleID)
								assert.Equal(t, 1, issue.Line)
								assert.Equal(t, 3, issue.Column)
							} else {
								assert.Equal(t, "SC2086", issue.RuleID)
								assert.Zero(t, issue.Line, "script coordinates must not masquerade as YAML coordinates")
								assert.Zero(t, issue.Column)
								require.NotNil(t, issue.Script)
								assert.Equal(t, 1, issue.Script.Line)
								assert.Equal(t, 3, issue.Script.Column)
								assert.NotEmpty(t, issue.Script.JobID)
								assert.Positive(t, issue.Script.StepIndex)
							}
							reportedFiles[filepath.Base(issue.File)] = true
						}
						for _, file := range files[:count] {
							assert.True(t, reportedFiles[strings.TrimSuffix(filepath.Base(file), ".md")+".lock.yml"])
						}
					})
				}
			}
		}
	}
}

func TestOptionalScannerPublicRunnersRemainNonStrict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test scanners use POSIX shell scripts")
	}
	dir := scannerJSONTestRepo(t)
	installScannerJSONFakes(t, dir)
	t.Setenv("SCANNER_TEST_MODE", "low")
	t.Setenv("SCANNER_TEST_MESSAGE", "shellcheck reported issue in this script: SC2086:info:1:3: quote this")
	t.Setenv("SCANNER_TEST_KIND", "shellcheck")
	t.Setenv("SCANNER_TEST_LEVEL", "note")
	lock := filepath.Join(dir, "test.lock.yml")
	require.NoError(t, os.WriteFile(lock, []byte("jobs:\n  test:\n    steps:\n      - run: echo hello\n"), 0o600))
	require.NoError(t, runActionlintOnFiles(context.Background(), []string{lock}, false, false))
	require.NoError(t, RunActionlintOnFiles(context.Background(), []string{lock}, false, false))
	require.NoError(t, runShellcheckOnLockFilesAndResources(context.Background(), []string{lock}, nil, false, false))
	require.NoError(t, RunShellcheckOnLockFilesAndResources(context.Background(), []string{lock}, nil, false, false))
}

func TestDryRunScannerReportingKeepsFindingsAndGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test scanners use POSIX shell scripts")
	}
	dir := scannerJSONTestRepo(t)
	installScannerJSONFakes(t, dir)
	lock := filepath.Join(dir, "test.lock.yml")
	require.NoError(t, os.WriteFile(lock, []byte("jobs:\n  test:\n    steps:\n      - run: echo hello\n"), 0o600))
	for _, tool := range []string{"actionlint", "shellcheck"} {
		for _, mode := range []string{"findings", "low", "tooling", "malformed-success"} {
			t.Run(tool+"/"+mode, func(t *testing.T) {
				if tool == "shellcheck" && mode == "malformed-success" {
					t.Skip("ShellCheck does not emit JSON")
				}
				t.Setenv("SCANNER_TEST_MODE", mode)
				message, kind, level := "quote this variable", "expression", "warning"
				if mode == "low" {
					message, kind, level = "shellcheck reported issue in this script: SC2086:info:1:3: quote this", "shellcheck", "note"
				}
				t.Setenv("SCANNER_TEST_MESSAGE", message)
				t.Setenv("SCANNER_TEST_KIND", kind)
				t.Setenv("SCANNER_TEST_LEVEL", level)
				config := CompileConfig{
					DryRun: true, Strict: true, JSONOutput: true,
					Actionlint: tool == "actionlint", Shellcheck: tool == "shellcheck",
					dryRunBaseline: &dryRunScannerBaseline{results: map[string]error{}},
				}
				config.dryRunReport = newDryRunCompileReport(config)
				var results []ValidationResult
				stats := &CompilationStats{}
				_, err := runBatchExternalTools(context.Background(), config, batchToolsOptions{
					lockFilesForActionlint: []string{lock}, lockFilesForShellcheck: []string{lock},
				}, stats, &results)
				assert.Equal(t, mode != "low", err != nil)
				output, outputErr := formatValidationOutput(results)
				require.NoError(t, outputErr)
				require.NoError(t, json.Unmarshal([]byte(output), &results))
				require.Len(t, results, 1)
				assert.Equal(t, mode == "low", results[0].Valid)
				if mode == "low" {
					require.Empty(t, results[0].Errors)
					require.Len(t, results[0].Warnings, 1)
					assert.Equal(t, message, results[0].Warnings[0].Message)
					assert.Equal(t, "passed", config.dryRunReport.Scanners[tool].Status)
					assert.Zero(t, stats.Errors)
				} else {
					require.NotEmpty(t, results[0].Errors)
					assert.Equal(t, "failed", config.dryRunReport.Scanners[tool].Status)
					assert.Positive(t, stats.Errors)
				}
				if tool == "shellcheck" && mode != "tooling" {
					issues := results[0].Errors
					if mode == "low" {
						issues = results[0].Warnings
					}
					require.NotNil(t, issues[0].Script, "dry-run filtering must preserve snippet location metadata")
					assert.Equal(t, "test", issues[0].Script.JobID)
					assert.Equal(t, 1, issues[0].Script.StepIndex)
					assert.Equal(t, 1, issues[0].Script.Line)
					assert.Equal(t, 3, issues[0].Script.Column)
					assert.Zero(t, issues[0].Line)
				}
			})
		}
	}
}

func TestScannerReportingPreservesMixedFailures(t *testing.T) {
	step := runStepInfo{Script: "echo hello", LockFile: "test.lock.yml"}
	finding := withShellcheckFindings(errors.New("findings"), step, "script:1:3: warning: quote this [SC2086]")
	dockerFailure := fmt.Errorf("Docker ShellCheck invocation failed: %w", errors.New("exit status 125: container stderr"))
	var reported error
	ctx := context.WithValue(context.Background(), compileScannerReporterKey{}, func(_ string, err error) { reported = err })
	err := reportShellcheckResults(ctx, []runStepInfo{step, step}, []shellcheckResult{
		{err: finding}, {err: dockerFailure},
	}, true)
	require.Error(t, err)
	issues := scannerValidationIssues("shellcheck", reported)
	require.Len(t, issues, 2)
	assert.Equal(t, "SC2086", issues[0].RuleID)
	assert.Equal(t, "shellcheck_error", issues[1].Type)
	assert.Contains(t, issues[1].Message, "Docker ShellCheck invocation failed")
	assert.Contains(t, issues[1].Message, "container stderr")
	require.Len(t, scannerValidationIssues("shellcheck", err), 2)
}

func TestShellcheckEarlyFailuresAreReported(t *testing.T) {
	for _, tc := range []struct {
		name          string
		available     bool
		lockContent   string
		wantInMessage string
	}{
		{
			name: "shellcheck and docker unavailable", lockContent: "jobs:\n  test:\n    steps:\n      - run: echo hello\n",
			wantInMessage: "shellcheck binary not found and Docker is unavailable",
		},
		{
			name: "lock file extraction failure", available: true, lockContent: "jobs: [",
			wantInMessage: "could not parse test.lock.yml",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("test scanners use POSIX shell scripts")
			}
			dir := scannerJSONTestRepo(t)
			lock := filepath.Join(dir, "test.lock.yml")
			require.NoError(t, os.WriteFile(lock, []byte(tc.lockContent), 0o600))
			if tc.available {
				installScannerJSONFakes(t, dir)
				t.Setenv("SCANNER_TEST_MODE", "clean")
			} else {
				t.Setenv("PATH", t.TempDir())
			}

			var reported error
			ctx := context.WithValue(context.Background(), compileScannerReporterKey{}, func(_ string, err error) {
				reported = err
			})
			require.NoError(t, RunShellcheckOnLockFilesAndResources(ctx, []string{lock}, nil, false, false),
				"non-strict behavior must remain non-fatal")
			require.Error(t, reported)
			issues := scannerValidationIssues("shellcheck", reported)
			require.Len(t, issues, 1)
			assert.Contains(t, issues[0].Message, tc.wantInMessage)
		})
	}
}

func TestDryRunScannerFiltersAdvisoryFromMixedFailure(t *testing.T) {
	t.Parallel()
	step := runStepInfo{Script: "echo hello", LockFile: "test.lock.yml"}
	advisory := withShellcheckFindings(errors.New("findings"), step, "script:1:3: note: informational [SC1000]")
	dockerFailure := fmt.Errorf("Docker ShellCheck invocation failed: %w", errors.New("exit status 125"))
	rawErr := errors.Join(advisory, dockerFailure)
	baseline := &dryRunScannerBaseline{results: map[string]error{}}
	_, gateErr := baseline.filter("shellcheck", rawErr)
	require.Error(t, gateErr)

	stats := &CompilationStats{}
	var results []ValidationResult
	appendDryRunScannerResult("shellcheck", rawErr, gateErr, stats, &results)
	require.Len(t, results, 1)
	result := results[0]
	assert.False(t, result.Valid)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "shellcheck_error", result.Errors[0].Type)
	assert.Contains(t, result.Errors[0].Message, "Docker ShellCheck invocation failed")
	assert.NotContains(t, result.Errors[0].Message, "informational")
	require.Len(t, result.Warnings, 1)
	assert.Equal(t, "shellcheck_finding", result.Warnings[0].Type)
	assert.Equal(t, scanfindings.SeverityInfo, result.Warnings[0].Severity)
	assert.Equal(t, 1, stats.Errors)
}

func TestScannerValidationIssueSanitizationPreservesFields(t *testing.T) {
	issue := ValidationIssue{
		Type: "shellcheck_finding", Message: "secrets.PRIVATE_TOKEN must not be exposed",
		RuleID: "SC2086", File: "workflow.lock.yml", Line: 9, Column: 17, Severity: scanfindings.SeverityMedium,
		Script: &ValidationScriptLocation{
			JobID: "agent", StepID: "check", StepName: "Check secrets.PRIVATE_TOKEN",
			Resource: "mcp-scripts.PRIVATE_TOKEN", Line: 2, Column: 6,
		},
	}
	originalMessage, originalScript := issue.Message, *issue.Script
	input := []ValidationResult{{Errors: []ValidationIssue{issue}, Warnings: []ValidationIssue{issue}}}
	result := sanitizeValidationResults(input)[0]
	require.Len(t, result.Warnings, 1)
	require.Len(t, result.Errors, 1)
	for _, sanitized := range []ValidationIssue{result.Warnings[0], result.Errors[0]} {
		assert.NotContains(t, sanitized.Message, "PRIVATE_TOKEN")
		require.NotNil(t, sanitized.Script)
		assert.NotSame(t, issue.Script, sanitized.Script)
		assert.NotContains(t, sanitized.Script.StepName, "PRIVATE_TOKEN")
		assert.NotContains(t, sanitized.Script.Resource, "PRIVATE_TOKEN")
		expected := issue
		script := originalScript
		expected.Message = sanitized.Message
		script.StepName, script.Resource = sanitized.Script.StepName, sanitized.Script.Resource
		expected.Script = &script
		assert.Equal(t, expected, sanitized)
	}
	assert.Equal(t, originalMessage, issue.Message)
	assert.Equal(t, originalScript, *issue.Script, "sanitization must not mutate nested metadata in the input")
	output, err := formatValidationOutput(input)
	require.NoError(t, err)
	assert.NotContains(t, output, "PRIVATE_TOKEN")
	finding := issue.ToFinding(issue.Severity)
	assert.Equal(t, issue.RuleID, finding.RuleID)
	assert.Equal(t, issue.Column, finding.Column)
	assert.Equal(t, issue.Severity, finding.Severity)
}

func TestScannerReportingPreservesNonStrictFatalFindings(t *testing.T) {
	t.Parallel()
	stats := &CompilationStats{}
	var results []ValidationResult
	appendCompileScannerResult("zizmor", &fatalFindingError{err: errors.New("high severity finding")}, false, stats, &results)
	require.Len(t, results, 1)
	assert.False(t, results[0].Valid)
	require.Len(t, results[0].Errors, 1)
	assert.Empty(t, results[0].Warnings)
	assert.Equal(t, 1, stats.Errors)
}

func TestBatchScannerReportingPreservesNonStrictFatalFindings(t *testing.T) {
	original := runBatchZizmorOnFiles
	t.Cleanup(func() { runBatchZizmorOnFiles = original })
	fatal := &fatalFindingError{err: errors.New("high severity finding")}
	runBatchZizmorOnFiles = func(_ []string, verbose, strict bool) error {
		return handleBatchToolError("zizmor", withScannerFindings(fatal, []scanfindings.Finding{{
			RuleID: "dangerous-triggers", Severity: scanfindings.SeverityHigh,
			Message: "high severity finding", File: "test.lock.yml", Line: 5, Column: 3,
		}}), strict, verbose)
	}
	stats := &CompilationStats{}
	var results []ValidationResult
	_, err := runBatchExternalTools(context.Background(), CompileConfig{Zizmor: true, JSONOutput: true}, batchToolsOptions{
		lockFilesForZizmor: []string{"test.lock.yml"},
	}, stats, &results)
	require.ErrorIs(t, err, fatal)
	output, err := formatValidationOutput(results)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	require.Len(t, results, 1)
	assert.False(t, results[0].Valid)
	require.Len(t, results[0].Errors, 1)
	assert.Empty(t, results[0].Warnings)
	assert.Equal(t, "dangerous-triggers", results[0].Errors[0].RuleID)
	assert.Equal(t, scanfindings.SeverityHigh, results[0].Errors[0].Severity)
	assert.Equal(t, 1, stats.Errors)
}

func TestShellcheckReportingDisambiguatesScriptLocations(t *testing.T) {
	t.Parallel()
	steps := extractRunStepsFromJob(map[string]any{"steps": []any{
		map[string]any{"uses": "actions/checkout@v4"},
		map[string]any{"id": "first", "name": "Check", "run": "echo $value"},
		map[string]any{"id": "second", "name": "Check", "run": "echo $value"},
	}}, "bash", "test.lock.yml")
	require.Len(t, steps, 2)
	for index, step := range steps {
		step.JobID = "agent"
		err := withShellcheckFindings(errors.New("findings"), step, "script:1:6: warning: quote this [SC2086]")
		issues := scannerValidationIssues("shellcheck", err)
		require.Len(t, issues, 1)
		assert.Equal(t, "test.lock.yml", issues[0].File)
		assert.Zero(t, issues[0].Line)
		assert.Zero(t, issues[0].Column)
		require.NotNil(t, issues[0].Script)
		assert.Equal(t, &ValidationScriptLocation{
			JobID: "agent", StepID: step.ID, StepName: "Check", StepIndex: index + 2, Line: 1, Column: 6,
		}, issues[0].Script)
	}
	resource := runStepInfo{Source: "test-workflow", Name: "mcp-scripts.inspect", Script: "echo '${{ github.repository }}' $value"}
	err := withShellcheckFindings(errors.New("findings"), resource, "script:1:6: warning: quote this [SC2086]")
	issues := scannerValidationIssues("shellcheck", err)
	require.Len(t, issues, 1)
	assert.Empty(t, issues[0].File, "workflow identifiers are not file paths")
	assert.Zero(t, issues[0].Line)
	require.NotNil(t, issues[0].Script)
	assert.Equal(t, &ValidationScriptLocation{
		Source: "test-workflow", Resource: "mcp-scripts.inspect", Line: 1, Column: 6, Sanitized: true,
	}, issues[0].Script)
}

func TestDryRunLowScannerWarningsDoNotMaskCompilerWarnings(t *testing.T) {
	t.Parallel()
	compiler := workflow.NewCompiler()
	compiler.IncrementWarningCount()
	results := []ValidationResult{{
		Scope: "batch", Workflow: "shellcheck", Valid: true,
		Warnings: []ValidationIssue{{Message: "advice", Severity: scanfindings.SeverityInfo}},
	}}
	err := enforceDevelopmentDiagnostics(CompileConfig{DryRun: true}, compiler, &CompilationStats{}, &results)
	require.ErrorContains(t, err, "compiler reported warnings")
}
