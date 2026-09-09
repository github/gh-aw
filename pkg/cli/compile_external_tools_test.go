//go:build !integration

package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
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
		return fakeActionlintErr
	}
	runBatchZizmorOnFiles = func(_ []string, _ bool, _ bool) error {
		calls = append(calls, "zizmor")
		return nil
	}
	runBatchPoutineOnDirectory = func(_ string, _ bool, _ bool) error {
		calls = append(calls, "poutine")
		return nil
	}
	runBatchRunnerGuardOnDirectory = func(_ string, _ bool, _ bool) error {
		calls = append(calls, "runner-guard")
		return nil
	}
	runBatchSyftOnLockFiles = func(_ []string, _ bool, _ bool) error {
		calls = append(calls, "syft")
		return nil
	}
	runBatchGrypeOnLockFiles = func(_ []string, _ bool, _ bool) error {
		calls = append(calls, "grype")
		return nil
	}
	runBatchGrantOnLockFiles = func(_ []string, _ bool, _ bool) error {
		calls = append(calls, "grant")
		return nil
	}
	runBatchYamllintOnFiles = func(_ []string, _ bool, _ bool) error {
		calls = append(calls, "yamllint")
		return nil
	}
	runBatchShellcheckOnLockFilesAndResources = func(_ context.Context, _ []string, _ []workflow.ShellScriptResource, _ bool, _ bool) error {
		calls = append(calls, "shellcheck")
		return nil
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
		workflowDir:            t.TempDir(),
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
}
