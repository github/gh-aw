//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deliveredFixtureRun(t *testing.T, manifest string) ProcessedRun {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "run-42")
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(manifest), 0o600))
	return ProcessedRun{Run: WorkflowRun{
		DatabaseID: 42, Attempt: 1, WorkflowPath: ".github/workflows/test.lock.yml",
		Repository: "acme/repo", LogsPath: dir,
	}}
}

func TestLifecycleRetainedEvidenceDoesNotCrossAttempts(t *testing.T) {
	run := deliveredFixtureRun(t, `{"type":"add_comment","status":"success","timestamp":"2026-11-01T00:00:00Z"}`)
	_, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(run.Run.LogsPath, safeOutputItemsManifestFilename)))
	run.Run.Attempt++
	report, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	assert.Empty(t, report.Delivery.Items)
	assert.Equal(t, "unavailable", report.Delivery.Coverage)
}

func TestLifecycleSessionMirrorsAndCollectionWarnings(t *testing.T) {
	manifest := `{"type":"add_comment","status":"success","timestamp":"2026-11-01T00:00:00Z"}`
	run := deliveredFixtureRun(t, manifest)
	first, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	require.Len(t, first.Records, 1)
	require.NoError(t, os.Remove(filepath.Join(run.Run.LogsPath, safeOutputItemsManifestFilename)))
	session := unifiedDetectionTestHeader +
		`{"type":"workflow.info","provenance":{"component":"workflow"},"data":{"runId":42,"runAttempt":1,"repository":"acme/repo","serverUrl":"https://github.com"}}` + "\n" +
		`{"type":"safe_output.result","provenance":{"component":"safe_output","phase":"safe_outputs","path":"safe-output-items.jsonl"},"data":` + manifest + "}\n"
	path := filepath.Join(run.Run.LogsPath, "aw_session.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(session), 0o600))
	replay, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	require.Len(t, replay.Records, 1)
	assert.Equal(t, first.Records[0].ID, replay.Records[0].ID)
	require.NoError(t, os.WriteFile(path, []byte(session+`{"type":"session.collection_warning","data":{"path":"safe-output-items.jsonl","code":"invalid_json"}}`+"\n"), 0o600))
	partial, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	assert.Len(t, partial.Delivery.Items, 1)
	assert.Equal(t, "partial", partial.Delivery.Coverage)
	require.NoError(t, os.WriteFile(path, []byte(session+unifiedDetectionTestHeader), 0o600))
	_, err = collectRunLifecycle(t.Context(), run, nil, false, false)
	require.ErrorContains(t, err, "repeated format headers")
}

func TestLifecycleCacheRejectsTruncationAndHonorsLockCancellation(t *testing.T) {
	run := deliveredFixtureRun(t, `{"type":"add_comment","status":"success","timestamp":"2026-11-01T00:00:00Z"}`)
	_, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	path := lifecycleStorePath(run.Run.LogsPath)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(content, []byte(`{"schema_version":`)...), 0o600))
	_, err = loadLifecycleCache(run.Run.LogsPath)
	require.ErrorContains(t, err, "decode retained lifecycle evidence")
	unlock, err := lockLifecycleCache(t.Context(), run.Run.LogsPath)
	require.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = lockLifecycleCache(ctx, run.Run.LogsPath)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLifecycleLogsBudgetUsesTargetHostAndStopsBeforeRequest(t *testing.T) {
	calls := installOutcomeAPIFixture(t, "exit 1\n")
	original := fetchRateLimitForHostFunc
	t.Cleanup(func() { fetchRateLimitForHostFunc = original })
	fetchRateLimitForHostFunc = func(_ context.Context, host string) (rateLimitResource, error) {
		assert.Equal(t, "ghe.example.com", host)
		return rateLimitResource{Limit: 5000, Used: 100, Reset: time.Now().Add(time.Hour).Unix()}, nil
	}
	ctx, cancel := lifecycleLogsContext(t.Context(), 1, 1, 100)
	defer cancelLogsDownload(cancel)
	networkCtx, ok := ctx.Value(lifecycleNetworkContextKey{}).(context.Context)
	require.True(t, ok)
	deadline, ok := networkCtx.Deadline()
	require.True(t, ok)
	assert.WithinDuration(t, time.Now().Add(time.Second), deadline, 100*time.Millisecond)
	_, err := requestLifecycleAPI(ctx, "ghe.example.com/acme/repo", []string{"api", "repos/acme/repo/issues/42"})
	require.ErrorIs(t, err, errLogsAPIRateLimitReached)
	_, err = os.Stat(calls)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestLifecycleDeliveryDoesNotQueryDisposition(t *testing.T) {
	calls := installOutcomeAPIFixture(t, "exit 1\n")
	var rows []string
	for _, status := range []string{"success", "failed", "skipped", "staged", "deferred", "cancelled"} {
		rows = append(rows, `{"type":"create_issue","status":"`+status+`","number":42,"timestamp":"2026-11-01T00:00:00Z"}`)
	}
	run := deliveredFixtureRun(t, strings.Join(rows, "\n"))
	report, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	assert.Empty(t, report.Items)
	require.Len(t, report.Delivery.Items, 1)
	assert.Equal(t, "complete", report.Delivery.Coverage)
	_, err = os.Stat(calls)
	require.ErrorIs(t, err, os.ErrNotExist)
	retained, err := loadLifecycleCache(run.Run.LogsPath)
	require.NoError(t, err)
	assert.Len(t, retained, 1)
	info, err := os.Stat(lifecycleStorePath(run.Run.LogsPath))
	require.NoError(t, err)
	assert.Zero(t, info.Mode().Perm()&0o077)
}

func TestLifecycleProposalRequiresDecisionEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, decision, state, coverage, kind string
	}{
		{"human", `"closed_at":"2026-11-02T00:00:00Z","closed_by":{"login":"alice","type":"User"}`, "accepted", "available", "explicit_human"},
		{"bot", `"closed_at":"2026-11-02T00:00:00Z","closed_by":{"login":"stale[bot]","type":"Bot"}`, "accepted", "available", "automation"},
		{"bot without login", `"closed_at":"2026-11-02T00:00:00Z","closed_by":{"type":"Bot"}`, "unknown", "available", ""},
		{"missing clock", `"closed_by":{"login":"alice","type":"User"}`, "unknown", "available", ""},
		{"publisher", `"closed_at":"2026-11-02T00:00:00Z","closed_by":{"login":"publisher","type":"User"}`, "unknown", "available", ""},
		{"missing actor", `"closed_at":"2026-11-02T00:00:00Z"`, "unknown", "available", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installOutcomeAPIFixture(t, `case "$2" in
repos/acme/repo/issues/42) printf '%s' '{"html_url":"https://github.com/acme/repo/issues/42","state":"closed","state_reason":"completed","user":{"login":"publisher"},`+tc.decision+`}' ;;
repos/acme/repo/collaborators/alice/permission) printf '%s' '{"permission":"write"}' ;;
*) exit 1 ;;
esac`)
			run := deliveredFixtureRun(t, `{"type":"create_issue","number":42,"status":"success","timestamp":"2026-11-01T00:00:00Z"}`)
			report, err := collectRunOutcomes(t.Context(), run, nil, false)
			require.NoError(t, err)
			require.Len(t, report.Items, 1)
			assert.Equal(t, tc.state, report.Items[0].Adoption.State)
			assert.Equal(t, tc.coverage, report.Items[0].Adoption.Coverage)
			assert.Equal(t, tc.kind, report.Items[0].Adoption.AcceptanceKind)
			assert.Equal(t, tc.kind == "explicit_human", report.Items[0].Adoption.HumanVerified)
		})
	}
}

func TestLifecycleProposalRecordsAutomationResolution(t *testing.T) {
	for _, tc := range []struct {
		name, output, pull, issue, state, signal, actor string
	}{
		{
			name: "merged pull request", output: "create_pull_request",
			pull:  `{"html_url":"https://github.com/acme/repo/pull/42","state":"closed","merged":true,"merged_at":"2026-11-02T00:00:00Z","merged_by":{"login":"github-actions[bot]","type":"Bot"},"user":{"login":"github-actions[bot]"}}`,
			state: "accepted", signal: "merged", actor: "github-actions[bot]",
		},
		{
			name: "closed pull request", output: "create_pull_request",
			pull:  `{"html_url":"https://github.com/acme/repo/pull/42","state":"closed","merged":false,"closed_at":"2026-11-02T00:00:00Z","user":{"login":"github-actions[bot]"}}`,
			issue: `{"closed_by":{"login":"stale[bot]","type":"Bot"}}`,
			state: "rejected", signal: "closed_without_merge", actor: "stale[bot]",
		},
		{
			name: "issue closed as not planned", output: "create_issue",
			issue: `{"html_url":"https://github.com/acme/repo/issues/42","state":"closed","state_reason":"not_planned","closed_at":"2026-11-02T00:00:00Z","closed_by":{"login":"stale[bot]","type":"Bot"}}`,
			state: "rejected", signal: "closed_not_planned", actor: "stale[bot]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installOutcomeAPIFixture(t, `case "$2" in
repos/acme/repo/pulls/42) printf '%s' '`+tc.pull+`' ;;
repos/acme/repo/issues/42) printf '%s' '`+tc.issue+`' ;;
*) exit 1 ;;
esac`)
			run := deliveredFixtureRun(t, `{"type":"`+tc.output+`","number":42,"status":"success","timestamp":"2026-11-01T00:00:00Z"}`)
			report, err := collectRunOutcomes(t.Context(), run, nil, false)
			require.NoError(t, err)
			require.Len(t, report.Items, 1)
			adoption := report.Items[0].Adoption
			require.NotNil(t, adoption)
			assert.Equal(t, tc.state, adoption.State)
			assert.Equal(t, tc.signal, adoption.Signal)
			assert.Equal(t, "automation", adoption.AcceptanceKind)
			assert.Equal(t, lifecycle.Actor{Login: tc.actor, Type: "Bot"}, adoption.Actor)
			assert.False(t, adoption.HumanVerified)
			assert.Equal(t, tc.state == "accepted", report.Summary.AcceptedAutomation == 1)
			assert.Equal(t, tc.state == "rejected", report.Summary.Rejected == 1)
			assert.Zero(t, report.Summary.AcceptedHuman)
			assert.Zero(t, report.Summary.Lifecycle)
			retained, err := loadLifecycleCache(run.Run.LogsPath)
			require.NoError(t, err)
			require.Len(t, retained, 1)
			for _, record := range retained {
				assert.True(t, adoption.Equal(record.Adoption), "retained adoption must keep the automation actor and resolution")
			}
		})
	}
}

func TestLifecycleReviewPublicationIsNotRejectedByParentClosure(t *testing.T) {
	for _, tc := range []struct{ reviewState, adoption, survival string }{
		{"CHANGES_REQUESTED", "accepted", "unknown"},
		{"DISMISSED", "rejected", "reversed"},
	} {
		t.Run(tc.reviewState, func(t *testing.T) {
			installOutcomeAPIFixture(t, `case "$2" in
repos/acme/repo/pulls/42) printf '%s' '{"state":"closed","merged":false}' ;;
repos/acme/repo/pulls/42/reviews) printf '%s' '[[{"id":7,"state":"`+tc.reviewState+`","submitted_at":"2026-11-01T00:00:00Z"}]]' ;;
*) exit 1 ;;
esac`)
			run := deliveredFixtureRun(t, `{"type":"submit_pull_request_review","number":42,"status":"success","metadata":{"review_id":7},"timestamp":"2026-11-01T00:00:00Z"}`)
			report, err := collectRunOutcomes(t.Context(), run, nil, false)
			require.NoError(t, err)
			require.Len(t, report.Items, 1)
			require.NotNil(t, report.Items[0].Adoption)
			assert.Equal(t, tc.adoption, report.Items[0].Adoption.State)
			assert.Equal(t, tc.survival, report.Items[0].Survival.State)
			_, err = loadLifecycleCache(run.Run.LogsPath)
			require.NoError(t, err)
		})
	}
}

func TestLifecycleRetentionReopeningAndDeliveryReplay(t *testing.T) {
	response := filepath.Join(t.TempDir(), "issue.json")
	t.Setenv("GH_AW_LIFECYCLE_RESPONSE", response)
	installOutcomeAPIFixture(t, `case "$2" in
repos/acme/repo/issues/42) cat "$GH_AW_LIFECYCLE_RESPONSE" ;;
repos/acme/repo/collaborators/alice/permission) printf '%s' '{"permission":"write"}' ;;
*) exit 1 ;;
esac`)
	run := deliveredFixtureRun(t, `{"type":"create_issue","number":42,"status":"success","timestamp":"2026-11-01T00:00:00Z"}`)
	require.NoError(t, os.WriteFile(response, []byte(`{"html_url":"https://github.com/acme/repo/issues/42","state":"closed","state_reason":"completed","closed_at":"2026-11-02T00:00:00Z","closed_by":{"login":"alice","type":"User"}}`), 0o600))
	first, err := collectRunOutcomes(t.Context(), run, nil, false)
	require.NoError(t, err)
	require.Equal(t, 1, first.Summary.AcceptedHuman)
	replay, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	require.Equal(t, first.Records[0].Adoption, replay.Records[0].Adoption)
	require.NoError(t, os.Remove(filepath.Join(run.Run.LogsPath, safeOutputItemsManifestFilename)))
	require.NoError(t, os.WriteFile(response, []byte(`{"html_url":"https://github.com/acme/repo/issues/42","state":"open"}`), 0o600))
	refreshed, err := collectRunOutcomes(t.Context(), run, nil, false)
	require.NoError(t, err)
	require.Len(t, refreshed.Items, 1)
	assert.Equal(t, first.Items[0].ItemID, refreshed.Items[0].ItemID)
	assert.Equal(t, "reopened", refreshed.Items[0].ReversalSignal)
	assert.Equal(t, "reversed", refreshed.Items[0].Survival.State)
	assert.Empty(t, refreshed.Items[0].ReversedAt, "polling time is not a reversal event clock")
}

func TestLifecycleCachedJSONLRoundTripAndConflict(t *testing.T) {
	run := deliveredFixtureRun(t, `{"type":"add_comment","status":"success","timestamp":"2026-11-01T00:00:00Z"}`)
	report, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	cachePath := filepath.Join(t.TempDir(), "logs.jsonl")
	writer := newCachedLogsJSONLWriter(cachePath)
	require.NoError(t, cacheLogsLifecycles(writer, []OutcomesData{report}))
	cache, err := loadCachedLogsJSONL(cachePath)
	require.NoError(t, err)
	require.Len(t, cache.lifecycles, 1)
	var record lifecycle.Record
	for _, item := range cache.lifecycles {
		record = item
	}
	assert.Equal(t, report.Records[0], record)
	record.Delivery.State = "requested"
	require.NoError(t, writer.AppendLifecycle(record))
	_, err = loadCachedLogsJSONL(cachePath)
	require.ErrorContains(t, err, "invalid lifecycle")
}

func TestLifecycleEpisodeBudgetCountsPagination(t *testing.T) {
	rows := make([]map[string]int, 100)
	for i := range rows {
		rows[i] = map[string]int{"id": i}
	}
	body, err := json.Marshal(rows)
	require.NoError(t, err)
	calls := installOutcomeAPIFixture(t, "printf '%s' '"+string(body)+"'\n")
	report := &EpisodeRetrieval{Host: "github.com", Repositories: []string{"acme/repo"}, MaxAPIRequests: 1}
	ctx := context.WithValue(t.Context(), lifecycleAPIBudgetKey{}, &lifecycleAPIBudget{report: report})
	_, err = ghAPIGetArray(ctx, "issues/42/events", "github.com/acme/repo")
	require.ErrorContains(t, err, errEpisodeAPILimit.Error())
	assert.Equal(t, 1, report.APIRequests)
	assert.Len(t, outcomeAPICalls(t, calls), 1)
	assert.Contains(t, report.StopReasons, "api_request_limit")
	_, err = ghAPIGet(ctx, "issues/42", "github.com/other/repo")
	require.ErrorContains(t, err, "outside the episode")
	assert.Len(t, outcomeAPICalls(t, calls), 1)
}
