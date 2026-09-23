//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubFetchJobStatusesForProcessedRun(t *testing.T, fn func(context.Context, int64, bool) (int, error)) {
	t.Helper()
	previous := fetchJobStatusesForProcessedRun
	fetchJobStatusesForProcessedRun = fn
	t.Cleanup(func() {
		fetchJobStatusesForProcessedRun = previous
	})
}

// makeDownloadResult creates a DownloadResult pointing at a temporary directory
// that optionally contains an aw_info.json file.
func makeDownloadResult(t *testing.T, awInfoJSON string) DownloadResult {
	t.Helper()
	tmpDir := t.TempDir()
	if awInfoJSON != "" {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "aw_info.json"), []byte(awInfoJSON), 0644))
	}
	return DownloadResult{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{DatabaseID: 42},
	},
		LogsPath: tmpDir,
	}
}

// TestApplyRunFilters_NoFilters verifies that an empty runFilterOpts always
// passes every run through (returns false = do not skip).
func TestApplyRunFilters_NoFilters(t *testing.T) {
	result := makeDownloadResult(t, `{"engine_id":"claude"}`)
	skip := applyRunFilters(context.Background(), result, runFilterOpts{}, false)
	assert.False(t, skip, "no filters should never skip a run")
}

// TestApplyRunFilters_Engine exercises both the matching and non-matching engine cases.
func TestApplyRunFilters_Engine(t *testing.T) {
	tests := []struct {
		name         string
		awInfo       string
		filterEngine string
		wantSkip     bool
	}{
		{
			name:         "matching engine passes",
			awInfo:       `{"engine_id":"claude"}`,
			filterEngine: "claude",
			wantSkip:     false,
		},
		{
			name:         "non-matching engine skipped",
			awInfo:       `{"engine_id":"copilot"}`,
			filterEngine: "claude",
			wantSkip:     true,
		},
		{
			name:         "missing aw_info skipped",
			awInfo:       "", // no file
			filterEngine: "claude",
			wantSkip:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeDownloadResult(t, tt.awInfo)
			skip := applyRunFilters(context.Background(), result, runFilterOpts{engine: tt.filterEngine}, false)
			assert.Equal(t, tt.wantSkip, skip)
		})
	}
}

// TestApplyRunFilters_Runtime exercises both the matching and non-matching runtime cases.
func TestApplyRunFilters_Runtime(t *testing.T) {
	tests := []struct {
		name          string
		awInfo        string
		filterRuntime string
		wantSkip      bool
	}{
		{
			name:          "matching runtime passes",
			awInfo:        `{"agent_runtime":"cloud-hypervisor"}`,
			filterRuntime: "cloud-hypervisor",
			wantSkip:      false,
		},
		{
			name:          "non-matching runtime skipped",
			awInfo:        `{"agent_runtime":"docker"}`,
			filterRuntime: "cloud-hypervisor",
			wantSkip:      true,
		},
		{
			name:          "missing aw_info skipped",
			awInfo:        "", // no file
			filterRuntime: "cloud-hypervisor",
			wantSkip:      true,
		},
		{
			name:          "empty agent_runtime is skipped",
			awInfo:        `{"agent_runtime":""}`,
			filterRuntime: "cloud-hypervisor",
			wantSkip:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeDownloadResult(t, tt.awInfo)
			skip := applyRunFilters(context.Background(), result, runFilterOpts{runtime: tt.filterRuntime}, false)
			assert.Equal(t, tt.wantSkip, skip)
		})
	}
}

// TestApplyRunFilters_NoStaged verifies that --exclude-staged skips staged runs.
func TestApplyRunFilters_NoStaged(t *testing.T) {
	tests := []struct {
		name     string
		awInfo   string
		wantSkip bool
	}{
		{
			name:     "staged run is skipped",
			awInfo:   `{"staged":true}`,
			wantSkip: true,
		},
		{
			name:     "non-staged run passes",
			awInfo:   `{"staged":false}`,
			wantSkip: false,
		},
		{
			name:     "missing staged field treated as false",
			awInfo:   `{}`,
			wantSkip: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeDownloadResult(t, tt.awInfo)
			skip := applyRunFilters(context.Background(), result, runFilterOpts{noStaged: true}, false)
			assert.Equal(t, tt.wantSkip, skip)
		})
	}
}

// TestApplyRunFilters_Firewall verifies both --firewall and --no-firewall.
func TestApplyRunFilters_Firewall(t *testing.T) {
	withFirewall := `{"steps":{"firewall":"squid"}}`
	noFirewall := `{"steps":{}}`

	tests := []struct {
		name         string
		awInfo       string
		firewallOnly bool
		noFirewall   bool
		wantSkip     bool
	}{
		{
			name:         "firewallOnly: run with firewall passes",
			awInfo:       withFirewall,
			firewallOnly: true,
			wantSkip:     false,
		},
		{
			name:         "firewallOnly: run without firewall skipped",
			awInfo:       noFirewall,
			firewallOnly: true,
			wantSkip:     true,
		},
		{
			name:       "noFirewall: run without firewall passes",
			awInfo:     noFirewall,
			noFirewall: true,
			wantSkip:   false,
		},
		{
			name:       "noFirewall: run with firewall skipped",
			awInfo:     withFirewall,
			noFirewall: true,
			wantSkip:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeDownloadResult(t, tt.awInfo)
			skip := applyRunFilters(context.Background(), result, runFilterOpts{firewallOnly: tt.firewallOnly, noFirewall: tt.noFirewall}, false)
			assert.Equal(t, tt.wantSkip, skip)
		})
	}
}

// TestApplyRunFilters_SafeOutputType verifies safe-output-type filtering.
// When no agent_output.json is present, the run is skipped.
func TestApplyRunFilters_SafeOutputType(t *testing.T) {
	t.Run("no agent_output.json skips run", func(t *testing.T) {
		result := makeDownloadResult(t, "") // no aw_info.json either
		skip := applyRunFilters(context.Background(), result, runFilterOpts{safeOutputType: "create-issue"}, false)
		assert.True(t, skip)
	})

	t.Run("matching safe output type passes", func(t *testing.T) {
		tmpDir := t.TempDir()
		agentOutput := `{"items":[{"type":"create-issue"}]}`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agent_output.json"), []byte(agentOutput), 0644))
		result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: 1}}, LogsPath: tmpDir}
		skip := applyRunFilters(context.Background(), result, runFilterOpts{safeOutputType: "create-issue"}, false)
		assert.False(t, skip)
	})

	t.Run("non-matching safe output type skips run", func(t *testing.T) {
		tmpDir := t.TempDir()
		agentOutput := `{"items":[{"type":"add-comment"}]}`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agent_output.json"), []byte(agentOutput), 0644))
		result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: 2}}, LogsPath: tmpDir}
		skip := applyRunFilters(context.Background(), result, runFilterOpts{safeOutputType: "create-issue"}, false)
		assert.True(t, skip)
	})
}

func TestApplyRunFilters_FilteredIntegrity(t *testing.T) {
	t.Run("gateway log with DIFC filtered event passes", func(t *testing.T) {
		tmpDir := t.TempDir()
		gatewayLog := `{"timestamp":"2025-01-01T00:00:00Z","type":"DIFC_FILTERED","server_id":"github","tool_name":"create_issue","reason":"integrity"}` + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "gateway.jsonl"), []byte(gatewayLog), 0644))
		result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: 5}}, LogsPath: tmpDir}

		skip := applyRunFilters(context.Background(), result, runFilterOpts{filteredIntegrity: true}, false)

		assert.False(t, skip)
	})

	t.Run("missing gateway logs are skipped", func(t *testing.T) {
		result := makeDownloadResult(t, "")

		skip := applyRunFilters(context.Background(), result, runFilterOpts{filteredIntegrity: true}, false)

		assert.True(t, skip)
	})
}

func TestApplyRunFilters_Graders(t *testing.T) {
	t.Run("missing grader results are skipped", func(t *testing.T) {
		result := makeDownloadResult(t, "")

		skip := applyRunFilters(context.Background(), result, runFilterOpts{gradersOnly: true}, false)

		assert.True(t, skip)
	})

	t.Run("grader results pass", func(t *testing.T) {
		tmpDir := t.TempDir()
		gradersDir := filepath.Join(tmpDir, "usage", "graders")
		require.NoError(t, os.MkdirAll(gradersDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(gradersDir, "grader_results.json"), []byte(`{"version":1,"results":[{"id":"quality","status":"pass","value":1,"passed":true}]}`), 0o600))
		result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: 9}}, LogsPath: tmpDir}

		skip := applyRunFilters(context.Background(), result, runFilterOpts{gradersOnly: true}, false)

		assert.False(t, skip)
	})
}

// TestBuildProcessedRun verifies that buildProcessedRun correctly populates
// the ProcessedRun fields from a DownloadResult.
func TestBuildProcessedRun(t *testing.T) {
	t.Run("basic fields are propagated", func(t *testing.T) {
		stubFetchJobStatusesForProcessedRun(t, func(context.Context, int64, bool) (int, error) { return 0, nil })
		now := time.Now()
		tmpDir := t.TempDir()
		awCtx := &AwContext{Repo: "owner/repo"}
		result := DownloadResult{RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID: 1234,
				StartedAt:  now.Add(-5 * time.Minute),
				UpdatedAt:  now,
			},
			AwContext: awCtx,
		},
			LogsPath: tmpDir,
		}

		pr := buildProcessedRun(context.Background(), result, false, false)

		assert.Equal(t, int64(1234), pr.Run.DatabaseID)
		assert.Equal(t, tmpDir, pr.Run.LogsPath)
		assert.Equal(t, awCtx, pr.AwContext)
		assert.Equal(t, 0, pr.Run.ErrorCount)
		assert.Equal(t, 0, pr.Run.WarningCount)
	})

	t.Run("duration and action minutes are computed", func(t *testing.T) {
		stubFetchJobStatusesForProcessedRun(t, func(context.Context, int64, bool) (int, error) { return 0, nil })
		base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		result := DownloadResult{RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID: 999,
				StartedAt:  base,
				UpdatedAt:  base.Add(90 * time.Second), // 1.5 minutes
			},
		},
			LogsPath: t.TempDir(),
		}

		pr := buildProcessedRun(context.Background(), result, false, false)

		assert.Equal(t, 90*time.Second, pr.Run.Duration)
		assert.InDelta(t, 2.0, pr.Run.ActionMinutes, 0.001) // ceil(1.5) = 2
	})

	t.Run("zero timestamps leave duration unset", func(t *testing.T) {
		stubFetchJobStatusesForProcessedRun(t, func(context.Context, int64, bool) (int, error) { return 0, nil })
		result := DownloadResult{RunAnalysis: RunAnalysis{
			Run: WorkflowRun{DatabaseID: 7},
		},
			LogsPath: t.TempDir(),
		}
		pr := buildProcessedRun(context.Background(), result, false, false)
		assert.Equal(t, time.Duration(0), pr.Run.Duration)
		assert.InDelta(t, 0.0, pr.Run.ActionMinutes, 0.001)
	})

	t.Run("failed job count is added via test seam", func(t *testing.T) {
		type ctxKey string
		const key ctxKey = "request-id"
		ctx := context.WithValue(context.Background(), key, "abc123")

		stubFetchJobStatusesForProcessedRun(t, func(fetchCtx context.Context, runID int64, verbose bool) (int, error) {
			assert.Equal(t, int64(88), runID)
			assert.False(t, verbose)
			assert.Equal(t, "abc123", fetchCtx.Value(key))
			return 2, nil
		})
		result := DownloadResult{RunAnalysis: RunAnalysis{
			Run: WorkflowRun{DatabaseID: 88},
		},
			LogsPath: t.TempDir(),
		}

		pr := buildProcessedRun(ctx, result, false, false)

		assert.Equal(t, 2, pr.Run.ErrorCount)
	})
}
