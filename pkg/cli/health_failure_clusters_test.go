//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthFailureClustersReuseCacheAndRejectStaleEvidence(t *testing.T) {
	root := t.TempDir()
	run := WorkflowRun{DatabaseID: 123, Status: "completed", Conclusion: "failure",
		WorkflowName: "example", WorkflowPath: "example.lock.yml",
		URL: "https://github.com/org/repo/actions/runs/123", Attempt: 1,
		UpdatedAt: time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)}
	summary := &RunSummary{CLIVersion: GetVersion(), RunID: 123, RunAnalysis: RunAnalysis{
		Run: run, JobDetails: []JobInfoWithDuration{{JobInfo: JobInfo{
			Name: "agent", Conclusion: "failure",
			Steps: []JobStep{{Name: "Start MCP Gateway", Conclusion: "failure"}},
		}}},
	}}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run-123"), 0755))
	require.NoError(t, saveRunSummary(filepath.Join(root, "run-123"), summary, false))
	config := HealthConfig{RepoOverride: "org/repo", LogsDir: root, collection: &HealthCollection{
		jobs: map[healthRunAttempt]*healthJobs{{id: 123, attempt: 1}: {
			details: []JobInfoWithDuration{{Name: "partial live job", Conclusion: "failure"}},
		}},
	}}
	clusters, plan := buildHealthFailureClusters([]WorkflowRun{run}, config)
	require.Len(t, clusters, 1)
	assert.Equal(t, "failure_boundary", clusters[0].SignalRole)
	assert.Equal(t, "Start MCP Gateway", clusters[0].Signal.Step)
	assert.Equal(t, 1, plan.ObservedEvidenceRuns, "usable cached evidence survives incomplete live inventory")
	require.Len(t, plan.SelectedRuns, 1)
	assert.Equal(t, filepath.Join(root, "run-123"), plan.SelectedRuns[0].LogsPath)
	run.Attempt++
	clusters, plan = buildHealthFailureClusters([]WorkflowRun{run}, config)
	require.Len(t, clusters, 1)
	assert.Equal(t, "metadata_only", clusters[0].Evidence)
	assert.Equal(t, 1, plan.MetadataOnlyRuns)
}

func TestHealthPresenceOnlyRunsRemainMetadataOnly(t *testing.T) {
	run := WorkflowRun{DatabaseID: 42, Attempt: 1, Conclusion: "failure"}
	collection := &HealthCollection{jobPresence: map[healthRunAttempt]bool{{id: 42, attempt: 1}: true}}
	clusters, plan := buildHealthFailureClusters([]WorkflowRun{run},
		HealthConfig{RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection})
	require.Len(t, clusters, 1)
	assert.Equal(t, "metadata_only", clusters[0].Evidence)
	assert.Equal(t, 1, plan.MetadataOnlyRuns)
	assert.Zero(t, plan.ObservedEvidenceRuns)
	assert.Empty(t, collection.JobEvidence)
}

func TestHealthFailureClusterOutput(t *testing.T) {
	runs := []WorkflowRun{{DatabaseID: 12, WorkflowName: "missing-cache", Conclusion: "failure"},
		{DatabaseID: 13, WorkflowName: "missing-cache", Conclusion: "success"}}
	config := HealthConfig{Days: 7, Threshold: 80, JSONOutput: true, LogsDir: t.TempDir(),
		RepoOverride: "org/repo", WorkflowName: "missing-cache"}
	{
		output := captureAuditGroupStdout(t, func() {
			require.NoError(t, displayDetailedHealth(runs, config))

		})
		var data struct {
			FailureClusters      []FailureCluster      `json:"failure_clusters"`
			FailureInvestigation *FailureInvestigation `json:"failure_investigation"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &data))
		require.Len(t, data.FailureClusters, 1)
		assert.Equal(t, 1, data.FailureInvestigation.UniqueFailedRuns)
		assert.Equal(t, 1, data.FailureInvestigation.MetadataOnlyRuns)
	}
	config.JSONOutput = false
	output := captureAuditGroupStderr(t, func() {
		require.NoError(t, displayDetailedHealth(runs, config))
	})
	assert.Contains(t, output, "[failure_clusters]")
	assert.Contains(t, output, "[failure_investigation]")
}

func TestHealthReusesCrossVersionRawJobsNotDerivedMetrics(t *testing.T) {
	run := WorkflowRun{DatabaseID: 123, Status: "completed", Conclusion: "failure",
		WorkflowName: "example", WorkflowPath: ".github/workflows/example.lock.yml",
		URL: "https://github.com/org/repo/actions/runs/123", Repository: "org/repo", Attempt: 1}
	summary := &RunSummary{CLIVersion: "v0.92.6", RunID: 123,
		Run: run, JobDetails: []JobInfoWithDuration{{
			Name: "activation", Conclusion: "failure",
			Steps: []JobStep{{Name: "Restore daily AIC scan observations", Conclusion: "failure"}},
		}},
		MCPFailures: []MCPFailureReport{{ServerName: "untrusted-derived-diagnostic"}},
	}
	summary.Run.TurnsAvailable = true
	dir := filepath.Join(t.TempDir(), "run-123")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, saveRunSummary(dir, summary, false))
	config := HealthConfig{RepoOverride: "org/repo", LogsDir: filepath.Dir(dir), collection: &HealthCollection{}}
	clusters, plan := buildHealthFailureClusters([]WorkflowRun{run}, config)
	require.Len(t, clusters, 1)
	assert.Equal(t, "Restore daily AIC scan observations", clusters[0].Signal.Step)
	assert.Empty(t, clusters[0].FailureKind, "foreign computed turn metrics must not classify this failure")
	assert.Equal(t, "version_mismatch_raw_jobs_only", config.collection.CacheEvidence[123].Status)
	assert.Equal(t, "v0.92.6", config.collection.CacheEvidence[123].ProducerVersion)
	require.Len(t, plan.SelectedRuns, 1)
	assert.Equal(t, dir, plan.SelectedRuns[0].LogsPath)
	raw, reason := readHealthCache(dir)
	require.Empty(t, reason)
	assert.Equal(t, "v0.92.6", raw.CLIVersion, "health must not rewrite producer metadata")
	_, ok := loadRunSummary(dir, false)
	assert.False(t, ok, "other consumers still require current-version derived metrics")

	run.Attempt++
	config.collection = &HealthCollection{}
	clusters, plan = buildHealthFailureClusters([]WorkflowRun{run}, config)
	require.Len(t, clusters, 1)
	assert.Equal(t, "metadata_only", clusters[0].Evidence)
	assert.Equal(t, "stale", config.collection.CacheEvidence[123].Status)
	assert.Equal(t, 1, plan.MetadataOnlyRuns)
}

func TestHealthCacheDiagnosticsAndMemoization(t *testing.T) {
	run := WorkflowRun{DatabaseID: 42}
	config := HealthConfig{LogsDir: t.TempDir(), collection: &HealthCollection{}}
	summary, dir := loadHealthRunSummary(run, config)
	assert.Nil(t, summary)
	assert.Equal(t, "missing", config.collection.CacheEvidence[42].Status)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, runSummaryFileName), []byte("{"), 0o600))
	loadHealthRunSummary(run, config)
	assert.Equal(t, "missing", config.collection.CacheEvidence[42].Status, "one cache observation per run attempt and report")
	config.collection = &HealthCollection{}
	loadHealthRunSummary(run, config)
	assert.Equal(t, "invalid", config.collection.CacheEvidence[42].Status)
	output := captureAuditGroupStderr(t, func() {
		require.NoError(t, renderHealthCollection(os.Stderr, config.collection))
	})
	assert.Contains(t, output, "[health_cache] status=invalid runs=1")
}
