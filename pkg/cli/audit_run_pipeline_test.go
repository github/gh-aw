//go:build !integration

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuditRunConfig(t *testing.T) {
	t.Parallel()
	t.Run("populates config from options", func(t *testing.T) {
		t.Parallel()
		cfg, err := newAuditRunConfig(42, AuditOptions{
			Owner:      "octo",
			Repo:       "repo",
			Hostname:   "github.example.com",
			OutputDir:  "logs",
			Verbose:    true,
			Parse:      true,
			JSONOutput: true,
			JobID:      7,
			StepNumber: 3,
			EvalsOnly:  true,
		})
		require.NoError(t, err)
		assert.Equal(t, int64(42), cfg.runID)
		assert.Equal(t, "octo", cfg.owner)
		assert.Equal(t, "repo", cfg.repo)
		assert.Equal(t, "github.example.com", cfg.hostname)
		assert.True(t, filepath.IsAbs(cfg.outputDir))
		assert.True(t, strings.HasSuffix(cfg.outputDir, filepath.Join("logs", "run-42")))
		assert.True(t, cfg.verbose)
		assert.True(t, cfg.parse)
		assert.True(t, cfg.jsonOutput)
		assert.Equal(t, int64(7), cfg.jobID)
		assert.Equal(t, 3, cfg.stepNumber)
		assert.True(t, cfg.evalsOnly)
		assert.True(t, cfg.evalsArtifactRequested)
	})

	t.Run("rejects invalid artifact sets", func(t *testing.T) {
		t.Parallel()
		_, err := newAuditRunConfig(1, AuditOptions{ArtifactSets: []string{"not-an-artifact-set"}})
		require.Error(t, err)
	})
}

func TestResolveAuditOutputDir(t *testing.T) {
	t.Parallel()
	dir := resolveAuditOutputDir("logs", 99)
	assert.True(t, filepath.IsAbs(dir))
	assert.Equal(t, "run-99", filepath.Base(dir))
}

func TestEnsureAuditNotCancelled(t *testing.T) {
	t.Parallel()
	require.NoError(t, ensureAuditNotCancelled(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ensureAuditNotCancelled(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestAuditRunConfigJobOptions(t *testing.T) {
	t.Parallel()
	cfg := auditRunConfig{
		runID:      5,
		jobID:      6,
		stepNumber: 7,
		owner:      "octo",
		repo:       "repo",
		hostname:   "github.com",
		outputDir:  "/tmp/run-5",
		verbose:    true,
		jsonOutput: true,
	}
	opts := cfg.jobOptions()
	assert.Equal(t, int64(5), opts.runID)
	assert.Equal(t, int64(6), opts.jobID)
	assert.Equal(t, 7, opts.stepNumber)
	assert.Equal(t, "octo", opts.owner)
	assert.Equal(t, "repo", opts.repo)
	assert.Equal(t, "github.com", opts.hostname)
	assert.Equal(t, "/tmp/run-5", opts.outputDir)
	assert.True(t, opts.verbose)
	assert.True(t, opts.jsonOutput)
}

func TestAuditRunConfigAuditOptions(t *testing.T) {
	t.Parallel()
	cfg := auditRunConfig{
		owner:      "octo",
		repo:       "repo",
		hostname:   "github.com",
		outputDir:  "/tmp/run-5",
		verbose:    true,
		parse:      true,
		jsonOutput: true,
		evalsOnly:  true,
	}
	opts := cfg.auditOptions()
	assert.Equal(t, "octo", opts.Owner)
	assert.Equal(t, "repo", opts.Repo)
	assert.Equal(t, "github.com", opts.Hostname)
	assert.Equal(t, "/tmp/run-5", opts.OutputDir)
	assert.True(t, opts.Verbose)
	assert.True(t, opts.Parse)
	assert.True(t, opts.JSONOutput)
	assert.True(t, opts.EvalsOnly)
}

func TestCacheRecoveryError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	err := cacheRecoveryError("GitHub API access denied.", 1234, "/tmp/run-1234", sentinel)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "GitHub API access denied.")
	assert.Contains(t, msg, "1234")
	assert.Contains(t, msg, "/tmp/run-1234")
	assert.Contains(t, msg, "boom")
	require.ErrorIs(t, err, sentinel, "cacheRecoveryError must wrap its cause with %%w so errors.Is can match it")
	require.Error(t, errors.Unwrap(err), "cacheRecoveryError result must be unwrappable")
}

func TestPrepareRunForAnalysis(t *testing.T) {
	cfg := auditRunConfig{runID: 77, outputDir: "/tmp/run-77"}

	// Not parallel: this subtest uses testutil.CaptureStderr, which reassigns
	// the process-wide os.Stderr and would race with other stderr-capturing tests.
	t.Run("synthesizes metadata when using local cache without run metadata", func(t *testing.T) {
		var run WorkflowRun
		output := testutil.CaptureStderr(t, func() {
			run = prepareRunForAnalysis(WorkflowRun{}, cfg, true)
		})
		assert.Equal(t, int64(77), run.DatabaseID)
		assert.Equal(t, "Workflow Run 77", run.WorkflowName)
		assert.Equal(t, "unknown", run.Status)
		assert.Equal(t, "/tmp/run-77", run.LogsPath)
		assert.Contains(t, output, "locally cached artifacts")
	})

	t.Run("computes duration from timestamps", func(t *testing.T) {
		t.Parallel()
		started := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		run := prepareRunForAnalysis(WorkflowRun{
			DatabaseID: 77,
			StartedAt:  started,
			UpdatedAt:  started.Add(90 * time.Second),
		}, cfg, false)
		assert.Equal(t, 90*time.Second, run.Duration)
		assert.Equal(t, "/tmp/run-77", run.LogsPath)
	})

	t.Run("leaves duration unset when timestamps are missing", func(t *testing.T) {
		t.Parallel()
		run := prepareRunForAnalysis(WorkflowRun{DatabaseID: 77}, cfg, false)
		assert.Zero(t, run.Duration)
	})
}

func TestShouldSkipForEvals(t *testing.T) {
	t.Parallel()
	t.Run("no skip when evals filtering is disabled", func(t *testing.T) {
		t.Parallel()
		cfg := auditRunConfig{runID: 1, outputDir: t.TempDir()}
		assert.False(t, shouldSkipForEvals(context.Background(), cfg, WorkflowRun{}))
	})

	t.Run("skips when no evals results are present", func(t *testing.T) {
		t.Parallel()
		cfg := auditRunConfig{runID: 1, outputDir: t.TempDir(), evalsOnly: true}
		assert.True(t, shouldSkipForEvals(context.Background(), cfg, WorkflowRun{}))
	})

	t.Run("does not skip when evals are present locally", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "evals.jsonl"), []byte("{}"), 0o600))

		cfg := auditRunConfig{runID: 1, outputDir: dir, evalsOnly: true}
		assert.False(t, shouldSkipForEvals(context.Background(), cfg, WorkflowRun{}))
	})
}

func TestAnnounceAuditRun(t *testing.T) {
	t.Run("quiet mode prints nothing", func(t *testing.T) {
		output := testutil.CaptureStderr(t, func() {
			announceAuditRun(auditRunConfig{runID: 1})
		})
		assert.Empty(t, output)
	})

	t.Run("verbose run message", func(t *testing.T) {
		output := testutil.CaptureStderr(t, func() {
			announceAuditRun(auditRunConfig{runID: 1, verbose: true})
		})
		assert.Contains(t, output, "Auditing workflow run 1")
	})

	t.Run("verbose job message", func(t *testing.T) {
		output := testutil.CaptureStderr(t, func() {
			announceAuditRun(auditRunConfig{runID: 1, jobID: 2, verbose: true})
		})
		assert.Contains(t, output, "job 2")
	})

	t.Run("verbose job and step message", func(t *testing.T) {
		output := testutil.CaptureStderr(t, func() {
			announceAuditRun(auditRunConfig{runID: 1, jobID: 2, stepNumber: 3, verbose: true})
		})
		assert.Contains(t, output, "step 3")
	})

	t.Run("verbose artifact filter message", func(t *testing.T) {
		output := testutil.CaptureStderr(t, func() {
			announceAuditRun(auditRunConfig{runID: 1, verbose: true, artifactFilter: []string{"agent"}})
		})
		assert.Contains(t, output, "Artifact filter")
	})
}
