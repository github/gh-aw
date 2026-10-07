//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleBatchToolErrorPreservesFatalFindingInNonStrictMode(t *testing.T) {
	t.Parallel()
	fatal := &fatalFindingError{err: errors.New("high severity finding")}

	err := handleBatchToolError("zizmor", fatal, false, false)
	if err == nil {
		t.Fatal("expected fatalFindingError to propagate even in non-strict mode, got nil")
	}
	var got *fatalFindingError
	if !errors.As(err, &got) {
		t.Fatalf("expected wrapped fatalFindingError, got %v", err)
	}
}

func TestHandleBatchToolErrorSuppressesRegularErrorInNonStrictMode(t *testing.T) {
	t.Parallel()
	err := handleBatchToolError("zizmor", errors.New("plain warning"), false, false)
	if err != nil {
		t.Fatalf("expected non-strict mode to suppress plain errors, got %v", err)
	}
}

func TestHandleBatchToolErrorPropagatesInStrictMode(t *testing.T) {
	t.Parallel()
	err := handleBatchToolError("zizmor", errors.New("plain warning"), true, false)
	if err == nil {
		t.Fatal("expected strict mode to propagate errors, got nil")
	}
}

// TestRunBatchExternalToolsExecutesSequentialToolsWithoutEarlyAborting verifies the
// regression this PR fixes: when an early scanner (actionlint) returns an error, every
// other enabled scanner still runs to completion, in pipeline order, and the first
// error is preserved rather than being lost or causing the pipeline to abort early.
func TestRunBatchExternalToolsExecutesSequentialToolsWithoutEarlyAborting(t *testing.T) {
	// Not t.Parallel(): this test overrides shared package-level function variables.

	var calls []string
	fakeActionlintErr := errors.New("fake actionlint finding")
	failAll := false
	scannerResult := func(tool string) error {
		calls = append(calls, tool)
		if failAll {
			return fmt.Errorf("fake %s finding", tool)
		}
		return nil
	}

	origActionlint := runBatchActionlintOnFiles
	origZizmor := runBatchZizmorOnFiles
	origPoutine := runBatchPoutineOnDirectory
	origRunnerGuard := runBatchRunnerGuardOnDirectory
	origSyft := runBatchSyftOnLockFiles
	origGrype := runBatchGrypeOnLockFiles
	origGrant := runBatchGrantOnLockFiles
	origYamllint := runBatchYamllintOnFiles
	origShellcheck := runBatchShellcheckOnLockFilesAndResources
	t.Cleanup(func() {
		runBatchActionlintOnFiles = origActionlint
		runBatchZizmorOnFiles = origZizmor
		runBatchPoutineOnDirectory = origPoutine
		runBatchRunnerGuardOnDirectory = origRunnerGuard
		runBatchSyftOnLockFiles = origSyft
		runBatchGrypeOnLockFiles = origGrype
		runBatchGrantOnLockFiles = origGrant
		runBatchYamllintOnFiles = origYamllint
		runBatchShellcheckOnLockFilesAndResources = origShellcheck
	})

	// The first scanner in pipeline order (actionlint) reports an error. Every
	// later scanner records its invocation and returns nil so we can assert
	// they all still ran, in order, after the failure.
	runBatchActionlintOnFiles = func(_ context.Context, _ []string, _ bool, _ bool) error {
		calls = append(calls, "actionlint")
		if failAll {
			return errors.Join(fakeActionlintErr, errors.New("second actionlint finding"))
		}
		return fakeActionlintErr
	}
	runBatchZizmorOnFiles = func(_ []string, _ bool, _ bool) error {
		return scannerResult("zizmor")
	}
	runBatchPoutineOnDirectory = func(_ string, _ bool, _ bool) error {
		return scannerResult("poutine")
	}
	runBatchRunnerGuardOnDirectory = func(_ string, _ bool, _ bool) error {
		return scannerResult("runner-guard")
	}
	runBatchSyftOnLockFiles = func(_ []string, _ bool, _ bool) error {
		return scannerResult("syft")
	}
	runBatchGrypeOnLockFiles = func(_ []string, _ bool, _ bool) error {
		return scannerResult("grype")
	}
	runBatchGrantOnLockFiles = func(_ []string, _ bool, _ bool) error {
		return scannerResult("grant")
	}
	runBatchYamllintOnFiles = func(_ []string, _ bool, _ bool) error {
		return scannerResult("yamllint")
	}
	runBatchShellcheckOnLockFilesAndResources = func(_ context.Context, _ []string, _ []workflow.ShellScriptResource, _ bool, _ bool) error {
		return scannerResult("shellcheck")
	}

	ctx := context.Background()
	config := CompileConfig{
		Actionlint:  true,
		Zizmor:      true,
		Poutine:     true,
		RunnerGuard: true,
		Syft:        true,
		Grype:       true,
		Grant:       true,
		Yamllint:    true,
		Shellcheck:  true,
		Strict:      true,
	}

	opts := batchToolsOptions{
		workflowDir:            ".",
		lockFilesForActionlint: []string{"a.lock.yml"},
		lockFilesForZizmor:     []string{"a.lock.yml"},
		lockFilesForDirTools:   []string{"a.lock.yml"},
		lockFilesForSyft:       []string{"a.lock.yml"},
		lockFilesForGrype:      []string{"a.lock.yml"},
		lockFilesForGrant:      []string{"a.lock.yml"},
		lockFilesForYamllint:   []string{"a.lock.yml"},
		lockFilesForShellcheck: []string{"a.lock.yml"},
	}

	stats := &CompilationStats{}
	var validationResults []ValidationResult

	strictGrantErr, batchToolErr := runBatchExternalTools(ctx, config, opts, stats, &validationResults)

	if strictGrantErr != nil {
		t.Fatalf("expected no strictGrantErr, got %v", strictGrantErr)
	}
	if !errors.Is(batchToolErr, fakeActionlintErr) {
		t.Fatalf("expected batchToolErr to preserve the first (actionlint) error, got %v", batchToolErr)
	}

	wantOrder := []string{"actionlint", "zizmor", "poutine", "runner-guard", "syft", "grype", "grant", "yamllint", "shellcheck"}
	if len(calls) != len(wantOrder) {
		t.Fatalf("expected all %d scanners to run despite the early actionlint error, got %d calls: %v", len(wantOrder), len(calls), calls)
	}
	for i, want := range wantOrder {
		if calls[i] != want {
			t.Fatalf("expected scanner invocation order %v, got %v (mismatch at index %d: want %q, got %q)", wantOrder, calls, i, want, calls[i])
		}
	}

	calls = nil
	failAll = true
	strictGrantErr, batchToolErr = runBatchExternalTools(ctx, config, opts, stats, &validationResults)
	require.ErrorContains(t, strictGrantErr, "fake grant finding")
	require.ErrorIs(t, batchToolErr, fakeActionlintErr)
	assert.Equal(t, wantOrder, calls)
	require.Len(t, validationResults, 1, "ordinary compilation retains its existing Grant result only")
	assert.Empty(t, validationResults[0].Scope)
	assert.Equal(t, "grant", validationResults[0].Workflow)
	assert.Equal(t, 1, stats.Errors)

	calls = nil
	config.DryRun = true
	config.JSONOutput = true
	stats = &CompilationStats{Total: 2, Succeeded: 2}
	validationResults = []ValidationResult{
		{Workflow: "a.md", Valid: true},
		{Workflow: "b.md", Valid: true},
	}
	strictGrantErr, batchToolErr = runBatchExternalTools(ctx, config, opts, stats, &validationResults)
	require.ErrorContains(t, strictGrantErr, "fake grant finding")
	require.ErrorIs(t, batchToolErr, fakeActionlintErr, "regular first-error behavior must remain compatible")
	assert.Equal(t, wantOrder, calls)
	err := enforceDevelopmentDiagnostics(config, workflow.NewCompiler(), stats, &validationResults, strictGrantErr, batchToolErr)
	require.Error(t, err)
	require.Len(t, validationResults, 11, "two workflows and one batch result per scanner")
	assert.True(t, validationResults[0].Valid)
	assert.True(t, validationResults[1].Valid)
	assert.Empty(t, validationResults[0].Errors)
	assert.Empty(t, validationResults[1].Errors)
	assert.Equal(t, 2, stats.Succeeded)
	assert.Equal(t, 10, stats.Errors, "all findings, including the joined actionlint error, must be counted exactly once")
	output, err := formatValidationOutput(validationResults)
	require.NoError(t, err)
	var decoded []ValidationResult
	require.NoError(t, json.Unmarshal([]byte(output), &decoded))
	for _, tool := range wantOrder {
		index := slices.IndexFunc(decoded, func(result ValidationResult) bool { return result.Workflow == tool })
		require.NotEqual(t, -1, index, "missing scanner: %s", tool)
		result := decoded[index]
		assert.Equal(t, "batch", result.Scope)
		assert.False(t, result.Valid)
		require.NotEmpty(t, result.Errors)
		assert.Equal(t, tool+"_error", result.Errors[0].Type)
		message := "fake " + tool + " finding"
		if tool == "poutine" || tool == "runner-guard" {
			message = tool + " failed: " + message
		}
		assert.Equal(t, message, result.Errors[0].Message)
	}
	require.Len(t, decoded[2].Errors, 2)
	assert.Equal(t, "second actionlint finding", decoded[2].Errors[1].Message)
}
