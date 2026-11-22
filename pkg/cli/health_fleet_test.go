//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthFleetResumesLargeInventoryAndVerification(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(1999 * time.Second)
	sha := strings.Repeat("a", 40)
	source := strings.Repeat("b", 40)
	lock := strings.Repeat("c", 40)
	var registry []GitHubWorkflow
	var runs []workflowRunAPIResponse
	for i := range 369 {
		registry = append(registry, GitHubWorkflow{ID: int64(i + 1), Name: fmt.Sprintf("Workflow %03d", i), Path: fmt.Sprintf(".github/workflows/test-%03d.lock.yml", i), State: "active"})
	}
	for i := range 1200 {
		run := workflowRunAPIResponse{ID: int64(i + 1), Path: registry[i%369].Path, Status: "completed",
			Conclusion: "success", Event: "schedule", HeadBranch: "main", HeadSHA: sha, RunAttempt: 1,
			CreatedAt: start.Add(time.Duration(i) * time.Second)}
		if i == 0 {
			run.Conclusion = "failure"
		}
		runs = append(runs, run)
	}
	oldGH, oldRate := healthRunGH, fetchRateLimitForHostFunc
	t.Cleanup(func() { healthRunGH, fetchRateLimitForHostFunc = oldGH, oldRate })
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{Remaining: 5000}, nil
	}
	calls := map[string]int{}
	block := ""
	healthRunGH = func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		endpoint := args[3]
		if block != "" && strings.Contains(endpoint, block) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		calls[endpoint]++
		u, err := url.Parse(endpoint)
		require.NoError(t, err)
		page, _ := strconv.Atoi(u.Query().Get("page"))
		switch {
		case u.Path == "repos/org/repo":
			return []byte(`{"full_name":"org/repo","default_branch":"main"}`), nil
		case strings.Contains(u.Path, "/git/ref/heads/"):
			return []byte(sha), nil
		case strings.HasSuffix(u.Path, "/actions/workflows"):
			return json.Marshal(map[string]any{"workflows": registry[(page-1)*100 : min(page*100, len(registry))]})
		case strings.HasSuffix(u.Path, "/actions/runs"):
			from, to, _ := strings.Cut(u.Query().Get("created"), "..")
			fromTime, err := time.Parse(time.RFC3339, from)
			require.NoError(t, err)
			toTime, err := time.Parse(time.RFC3339, to)
			require.NoError(t, err)
			match := []workflowRunAPIResponse{}
			for _, run := range slices.Backward(runs) {
				if !run.CreatedAt.Before(fromTime) && !run.CreatedAt.After(toTime) {
					match = append(match, run)
				}
			}
			return json.Marshal(map[string]any{"total_count": min(1000, len(match)), "workflow_runs": match[(page-1)*100 : min(page*100, len(match))]})
		case strings.HasSuffix(u.Path, "/contents/.github/workflows"):
			require.Equal(t, sha, u.Query().Get("ref"))
			var files []map[string]string
			for _, workflow := range registry {
				files = append(files, map[string]string{"path": workflow.Path, "sha": lock, "type": "file"},
					map[string]string{"path": strings.TrimSuffix(workflow.Path, ".lock.yml") + ".md", "sha": source, "type": "file"})
			}
			return json.Marshal(files)
		case strings.Contains(u.Path, "/git/blobs/"):
			return []byte(testHealthLockHeader), nil
		case strings.HasSuffix(u.Path, "/jobs"):
			return []byte(`{"total_count":1,"jobs":[{"name":"agent","conclusion":"failure","steps":[{"name":"Start MCP Gateway","conclusion":"failure"}]}]}`), nil
		default:
			t.Fatalf("unexpected endpoint: %s", endpoint)
			return nil, nil
		}
	}
	config := HealthConfig{Full: true, Threshold: 80, RepoOverride: "org/repo", Checkpoint: filepath.Join(t.TempDir(), "health.json")}
	output := captureAuditGroupStdout(t, func() {
		cmd := NewHealthCommand()
		cmd.SetArgs([]string{"--repo", "org/repo", "--json", "--start-date", start.Format(time.RFC3339), "--end-date", end.Format(time.RFC3339)})
		require.NoError(t, cmd.Execute())
	})
	var compact healthFleetReport
	require.NoError(t, json.Unmarshal([]byte(output), &compact))
	require.Equal(t, 1, compact.SchemaVersion)
	require.False(t, compact.Coverage.Partial)
	require.Nil(t, compact.Details)
	clear(calls)
	run := func(blockEndpoint string) healthFleetReport {
		t.Helper()
		block = blockEndpoint
		state, err := loadHealthFleetCheckpoint(config, "github.com", "org/repo", start, end)
		require.NoError(t, err)
		ctx := context.Background()
		if block != "" {
			deadline, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
			defer cancel()
			ctx = deadline
		}
		cfg := config
		cfg.collection = newHealthCollection(ctx, cfg, start, end)
		cfg.collection.repoOverride = config.RepoOverride
		cfg.collection.fleetResponses = state.Responses
		err = collectHealthFleet(state, cfg)
		if block == "" {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, context.DeadlineExceeded)
		}
		if config.Checkpoint != "" {
			require.NoError(t, saveHealthFleetCheckpoint(config.Checkpoint, state))
		}
		return makeHealthFleetReport(state, cfg)
	}

	inventoryPartial := run("page=2")
	require.False(t, inventoryPartial.Coverage.RegistryComplete)
	require.Equal(t, 100, inventoryPartial.TotalWorkflows)
	require.Equal(t, "resume", inventoryPartial.NextAction.Type)

	first := run("/git/blobs/")
	require.True(t, first.Coverage.InventoryComplete)
	require.False(t, first.Coverage.VerificationDone)
	require.Equal(t, "time_budget", first.Coverage.Reason)
	require.Equal(t, "resume", first.NextAction.Type)
	require.Equal(t, 1200, first.HistoricalRuns)
	require.Equal(t, map[string]int{"failure": 1, "success": 1199}, first.Outcomes)
	require.Equal(t, 369, first.StatusCounts["not_collected"])
	require.Empty(t, first.Clusters)

	second := run("/jobs?")
	require.True(t, second.Coverage.Partial)
	require.Equal(t, 369, second.StatusCounts["not_collected"])
	resumed := run("")
	require.True(t, resumed.Coverage.VerificationDone)
	require.False(t, resumed.Coverage.Partial)
	require.Equal(t, first.Scope, resumed.Scope)
	require.Equal(t, 1, resumed.StatusCounts["failures_observed"])
	require.NotEmpty(t, resumed.Clusters)
	require.Equal(t, []healthFleetRunRef{{RunID: 1, WorkflowPath: registry[0].Path}}, resumed.Details.FailureRepresentatives)
	require.Equal(t, "audit", resumed.NextAction.Type)
	for endpoint, count := range calls {
		require.Equal(t, 1, count, "completed API response was fetched again: %s", endpoint)
	}
	config.Checkpoint = ""
	require.Equal(t, resumed, run(""))
}

const testHealthLockHeader = `# gh-aw-metadata: {"frontmatter_hash":"front","body_hash":"body","compiler_version":"test"}`
