//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func healthAuditFixture(t *testing.T, count int) json.RawMessage {
	t.Helper()
	updated := time.Date(2026, 11, 8, 8, 0, 0, 0, time.UTC)
	targets := make([]FailureInvestigationTarget, count)
	for i := range targets {
		targets[i] = FailureInvestigationTarget{FailureClusterTarget: FailureClusterTarget{
			RunID: int64(i + 1), Repository: "example/repo",
			URL:     "https://github.com/example/repo/actions/runs/" + strconv.Itoa(i+1),
			Attempt: "1", UpdatedAt: updated,
		}, ClusterIDs: []string{"cluster"}}
	}
	raw, err := json.Marshal(map[string]any{
		"collection": map[string]any{"partial": false, "host": "github.com",
			"failure_listing_complete": true, "failure_pass_complete": true},
		"failure_investigation": &FailureInvestigation{SelectedRuns: targets},
		"failure_clusters":      []FailureCluster{{ID: "cluster"}},
	})
	require.NoError(t, err)
	return raw
}

func fakeHealthAudit(ctx context.Context, id int64, opts AuditOptions) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, opts.report(AuditData{Overview: OverviewData{
		RunID: id, RunAttempt: 1, UpdatedAt: time.Date(2026, 11, 8, 8, 0, 0, 0, time.UTC),
		URL: "https://github.com/example/repo/actions/runs/" + strconv.FormatInt(id, 10),
	}})
}

func TestAuditHealthTargets(t *testing.T) {
	t.Parallel()
	opts := auditCommandOptions{healthLimit: 3, noBaseline: true, repoFlag: "example/repo"}
	report, err := auditHealthTargets(context.Background(), healthAuditFixture(t, 5), opts, fakeHealthAudit)
	require.NoError(t, err)
	assert.True(t, report.Complete, "complete describes the requested bounded audit batch")
	assert.False(t, report.Partial)
	assert.Len(t, report.Audits, 3)
	assert.Equal(t, 2, report.RemainingTargets)
	assert.Equal(t, int64(1), report.Audits[0].Target.RunID)
	assert.Equal(t, 1, report.Audits[0].Report.Overview.RunAttempt)
}

func TestAuditHealthInputFailsClosed(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`null`, `{}`, `{"collection":{"host":"github.com"}}`,
		`{"collection":{"partial":true,"host":"github.com"}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			called := false
			report, err := auditHealthTargets(context.Background(), []byte(raw),
				auditCommandOptions{healthLimit: 5}, func(context.Context, int64, AuditOptions) (bool, error) {
					called = true
					return false, nil
				})
			require.Error(t, err)
			assert.True(t, report.Partial)
			assert.False(t, report.Complete)
			assert.False(t, called)
		})
	}
}

func TestAuditHealthTargetsValidateScopeAndDuplicates(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*FailureInvestigation){
		func(plan *FailureInvestigation) {
			plan.SelectedRuns[0].URL = "https://github.com/other/repo/actions/runs/1"
		},
		func(plan *FailureInvestigation) { plan.SelectedRuns[0].Attempt = "" },
		func(plan *FailureInvestigation) { plan.SelectedRuns[0].Attempt = "not-a-number" },
		func(plan *FailureInvestigation) { plan.SelectedRuns[0].ClusterIDs = []string{"missing"} },
		func(plan *FailureInvestigation) { plan.SelectedRuns = append(plan.SelectedRuns, plan.SelectedRuns[0]) },
	} {
		var data map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(healthAuditFixture(t, 1), &data))
		var plan FailureInvestigation
		require.NoError(t, json.Unmarshal(data["failure_investigation"], &plan))
		mutate(&plan)
		data["failure_investigation"], _ = json.Marshal(plan)
		raw, err := json.Marshal(data)
		require.NoError(t, err)
		_, err = resolveHealthAuditTargets(raw, auditCommandOptions{})
		require.Error(t, err)
	}
	_, err := resolveHealthAuditTargets(healthAuditFixture(t, 1), auditCommandOptions{repoFlag: "other/repo"})
	require.Error(t, err)
}

func TestAuditHealthRejectsIncompletePlansBeforeAuditing(t *testing.T) {
	t.Parallel()
	for _, change := range []func(map[string]json.RawMessage){
		func(data map[string]json.RawMessage) { delete(data, "failure_investigation") },
		func(data map[string]json.RawMessage) {
			data["collection"] = json.RawMessage(`{"partial":false,"host":"github.com","failure_listing_complete":false,"failure_pass_complete":true}`)
		},
		func(data map[string]json.RawMessage) {
			data["failure_clusters"] = json.RawMessage(`[{"id":"cluster","intentional_failure":true}]`)
		},
		func(data map[string]json.RawMessage) {
			data["failure_clusters"] = json.RawMessage(`[{"id":"cluster"},{"id":"cluster"}]`)
		},
	} {
		var data map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(healthAuditFixture(t, 1), &data))
		change(data)
		raw, err := json.Marshal(data)
		require.NoError(t, err)
		called := false
		report, err := auditHealthTargets(context.Background(), raw, auditCommandOptions{healthLimit: 5},
			func(context.Context, int64, AuditOptions) (bool, error) {
				called = true
				return false, nil
			})
		require.Error(t, err)
		assert.False(t, called)
		assert.True(t, report.Partial)
	}
}

func TestAuditHealthPreservesPartialProgress(t *testing.T) {
	t.Parallel()
	report, err := auditHealthTargets(context.Background(), healthAuditFixture(t, 2),
		auditCommandOptions{healthLimit: 5}, func(ctx context.Context, id int64, opts AuditOptions) (bool, error) {
			if id == 2 {
				return false, context.DeadlineExceeded
			}
			return fakeHealthAudit(ctx, id, opts)
		})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Len(t, report.Audits, 1)
	assert.True(t, report.Partial)
	assert.False(t, report.Complete)
	assert.Equal(t, 1, report.RemainingTargets)
}

func TestAuditHealthRejectsChangedOrMissingReports(t *testing.T) {
	t.Parallel()
	for _, audit := range []func(context.Context, int64, AuditOptions) (bool, error){
		func(_ context.Context, id int64, opts AuditOptions) (bool, error) {
			return false, opts.report(AuditData{Overview: OverviewData{RunID: id, RunAttempt: 2}})
		},
		func(context.Context, int64, AuditOptions) (bool, error) { return false, nil },
		func(context.Context, int64, AuditOptions) (bool, error) { return true, nil },
	} {
		_, err := auditHealthTargets(context.Background(), healthAuditFixture(t, 1),
			auditCommandOptions{healthLimit: 5}, audit)
		require.Error(t, err)
	}
}

func TestAuditHealthStdinChainEmptyReport(t *testing.T) {
	t.Parallel()
	cmd := NewAuditCommand()
	cmd.SetIn(bytes.NewReader(healthAuditFixture(t, 0)))
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--health", "--stdin", "--json"})
	require.NoError(t, cmd.Execute())
	var report HealthAuditReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	assert.True(t, report.Complete)
	assert.Empty(t, report.Audits)
	assert.JSONEq(t, string(healthAuditFixture(t, 0)), string(report.Health))
}

func TestAuditHealthJSONUsesStdoutWithRootHelpRouting(t *testing.T) {
	t.Parallel()
	cmd := NewAuditCommand()
	cmd.SetOut(os.Stderr)
	assert.Same(t, os.Stdout, healthAuditJSONWriter(cmd))
	var output bytes.Buffer
	cmd.SetOut(&output)
	assert.Same(t, &output, healthAuditJSONWriter(cmd))
}

func TestAuditHealthFlagValidation(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--health-limit", "2"}, {"--start-date", "-24h"},
		{"--health", "--health-limit", "6"}, {"--health", "--health-timeout", "-1s"},
		{"--health", "--group"}, {"--health", "--search", "error"},
		{"--health", "--stdin", "workflow"}, {"--health", "--stdin", "--start-date", "-24h"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			cmd := NewAuditCommand()
			cmd.SetArgs(args)
			cmd.SetIn(strings.NewReader("{}"))
			require.Error(t, cmd.Execute())
		})
	}
}

func TestReadHealthAuditInput(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "42\n", "{} {}", strings.Repeat(" ", maximumHealthAuditInputBytes+1)} {
		_, err := readHealthAuditInput(strings.NewReader(raw))
		require.Error(t, err)
	}
	_, err := readHealthAuditInput(healthAuditErrorReader{})
	require.Error(t, err)
}

type healthAuditErrorReader struct{}

func (healthAuditErrorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestAuditHealthReportCallback(t *testing.T) {
	t.Parallel()
	called := false
	opts := AuditOptions{report: func(data AuditData) error {
		called = true
		assert.Equal(t, int64(42), data.Overview.RunID)
		return nil
	}}
	require.NoError(t, renderPreparedAuditReport(context.Background(), AuditData{Overview: OverviewData{RunID: 42}}, opts))
	assert.True(t, called)
	cfg, err := newAuditRunConfig(42, opts)
	require.NoError(t, err)
	assert.NotNil(t, cfg.auditOptions().report)
}
