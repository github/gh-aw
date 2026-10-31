//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsHarborAdditionalGraderOptions(t *testing.T) {
	workflow := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\n---\nTest.\n")
	cmd := NewLogsCommand()
	require.NoError(t, cmd.ParseFlags([]string{"--harbor", "--additional-grader", workflow + ":loops"}))
	opts, err := loadCommonLogsOptions(cmd)
	require.NoError(t, err)
	require.Equal(t, []string{workflow + ":loops"}, opts.AdditionalGraders)
	stdin, err := loadStdinLogsOptions(cmd)
	require.NoError(t, err)
	require.Equal(t, opts.AdditionalGraders, stdin.AdditionalGraders)
	require.False(t, opts.GradersOnly, "additional graders must not filter runs by original scores")

	for _, args := range [][]string{
		{"--additional-grader", workflow + ":loops"},
		{"--harbor", "--additional-grader", workflow + ":unknown"},
		{"--harbor", "--additional-grader", "invalid"},
	} {
		cmd := NewLogsCommand()
		require.NoError(t, cmd.ParseFlags(args))
		_, err := loadCommonLogsOptions(cmd)
		require.Error(t, err)
	}
}

func TestLogsHarborRegradingPreservesEvidence(t *testing.T) {
	regradeTestRequireNode(t)
	workflow := writeGraderRunWorkflow(t, `---
on: workflow_dispatch
graders:
  recorded-count:
    script: return session.filter(e => e.type === "assistant.message").length;
  absent:
    script: return null;
  broken:
    script: throw new Error("expected failure");
---
Test.
`)
	root := t.TempDir()
	source := filepath.Join(root, "run-123")
	session := sessionTestHeader + sessionTestAgentEvent
	const original = `{"version":2,"results":[{"id":"recorded-count","status":"fail","value":-2,"implementation":{"version":"original"}}]}`
	writeSessionTestFile(t, source, "usage/aw_session.jsonl", session)
	writeSessionTestFile(t, source, "graders/grader_results.json", original)
	runs := []ProcessedRun{harborTestRun(source)}
	missing := harborTestRun(filepath.Join(root, "run-124"))
	missing.Run.DatabaseID = 124
	missing.Run.Attempt = 2
	missing.Run.Conclusion = "failure"
	runs = append(runs, missing)
	selectors := []string{workflow + ":recorded-count", workflow + ":absent", workflow + ":broken"}

	output := filepath.Join(root, "export")
	require.NoError(t, exportLogsHarbor(t.Context(), output, runs, selectors))
	require.Nil(t, runs[0].Regrading, "ordinary report records must remain unchanged")
	firstRecords := make(map[string][]byte)
	for _, name := range harborTestTrials(t, output) {
		dir := filepath.Join(output, harborExportDirectory, name)
		metadata := harborTestJSON(t, filepath.Join(dir, "gh-aw", "metadata.json"))
		require.Equal(t, "gh-aw/regrading.json", metadata["regrading"])
		raw, err := os.ReadFile(filepath.Join(dir, "gh-aw", "regrading.json"))
		require.NoError(t, err)
		firstRecords[name] = raw
		result := harborTestJSON(t, filepath.Join(dir, "result.json"))
		run := metadata["run"].(map[string]any)
		if run["databaseId"] == float64(124) {
			require.Equal(t, "failure", run["conclusion"])
			require.InDelta(t, 2, run["attempt"], 0)
			require.Nil(t, result["verifier_result"])
			continue
		}
		rewards := result["verifier_result"].(map[string]any)["rewards"]
		require.Equal(t, map[string]any{"gh-aw.recorded-count": float64(-2), "gh-aw.regraded.recorded-count": float64(1)}, rewards)
		snapshot, err := os.ReadFile(filepath.Join(dir, "gh-aw", "evidence", "graders", "grader_results.json"))
		require.NoError(t, err)
		require.Equal(t, original, string(snapshot)) //nolint:testifylint // Preserve exact historical evidence bytes, not just JSON equivalence.
		var record struct {
			Results []struct {
				ID             string          `json:"id"`
				Status         string          `json:"status"`
				Implementation json.RawMessage `json:"implementation"`
			} `json:"results"`
		}
		require.NoError(t, json.Unmarshal(raw, &record))
		assert.NotContains(t, string(raw), root)
		assert.NotContains(t, string(raw), workflow)
		statuses := map[string]string{}
		for _, r := range record.Results {
			statuses[r.ID] = r.Status
			require.NotEmpty(t, r.Implementation)
		}
		require.Equal(t, map[string]string{"recorded-count": "pass", "absent": "unavailable", "broken": "error"}, statuses)
	}
	require.NoError(t, exportLogsHarbor(t.Context(), output, runs, selectors))
	for name, originalRecord := range firstRecords {
		raw, err := os.ReadFile(filepath.Join(output, harborExportDirectory, name, "gh-aw", "regrading.json"))
		require.NoError(t, err)
		require.JSONEq(t, string(originalRecord), string(raw), "regrading must be deterministic")
	}
	raw, err := os.ReadFile(filepath.Join(source, "usage", "aw_session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, session, string(raw))
}

func TestHarborSanitizesRegradingPathsAndErrors(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "run")
	record := &RegradedRun{
		SourceDirectory: source,
		Run:             WorkflowRun{LogsPath: source},
		SessionPath:     filepath.Join(source, "usage", "aw_session.jsonl"),
		SessionError:    "cannot read " + filepath.Join(source, "private.json"),
		Definitions:     []RegradeDefinition{{ID: "loops", Workflow: filepath.Join(root, ".github/workflows/test.md"), Selector: filepath.Join(root, ".github/workflows/test.md") + ":loops"}},
		Results:         []json.RawMessage{json.RawMessage(`{"id":"loops","status":"error","error":"failed at /tmp/secret.js"}`)},
		OriginalEvidence: []RegradeOriginalEvidence{{
			Path: filepath.Join(source, "graders", "grader_results.json"),
			Data: json.RawMessage(`{"results":[{"id":"loops","status":"error","error":"failed at /tmp/recorded.js"}]}`),
		}},
	}
	safe := harborSanitizeRegradedRun(record)
	encoded, err := json.Marshal(safe)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), root)
	assert.NotContains(t, string(encoded), "/tmp/")
	assert.Equal(t, "gh-aw/evidence", safe.SourceDirectory)
	assert.Equal(t, "gh-aw/evidence", safe.Run.LogsPath)
	assert.Equal(t, "gh-aw/evidence/usage/aw_session.jsonl", safe.SessionPath)
	assert.Equal(t, "loops", safe.Definitions[0].Selector)
	assert.Empty(t, safe.Definitions[0].Workflow)
	assert.Equal(t, "gh-aw/evidence/graders/grader_results.json", safe.OriginalEvidence[0].Path)
	assert.Contains(t, safe.SessionError, "[local path]")
	assert.NotContains(t, string(record.Results[0]), "[local path]", "sanitization must not mutate source records")
}

func TestHarborRegradedRewardCollision(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "gh-aw"), 0o700))
	record := &RegradedRun{Results: []json.RawMessage{json.RawMessage(`{"id":"loops","status":"pass","value":1}`)}}
	_, _, err := harborRegradedRewards(dir, record, map[string]any{},
		map[string]json.Number{"gh-aw.regraded.loops": "99"}, map[string]any{"metrics": map[string]any{}})
	require.ErrorContains(t, err, "conflicts with recorded reward")
	record.Results = append(record.Results, json.RawMessage(`{"id":"loops","status":"unavailable","value":null}`))
	_, _, err = harborRegradedRewards(dir, record, map[string]any{}, nil, nil)
	require.ErrorContains(t, err, "duplicate additional grader reward identity")
}

func TestHarborSanitizeSensitiveText(t *testing.T) {
	githubToken := "ghp_" + strings.Repeat("a", 36)
	for _, tt := range []struct {
		name, input, want string
	}{
		{"classic PAT", "tool echoed " + githubToken, "tool echoed [REDACTED]"},
		{"installation token", "ghs_" + strings.Repeat("b", 36), "[REDACTED]"},
		{"extended installation token", "ghs_" + strings.Repeat("b", 36) + "." + strings.Repeat("a_-", 20) + "x", "[REDACTED]"},
		{"OAuth token", "gho_" + strings.Repeat("b", 36), "[REDACTED]"},
		{"user token", "ghu_" + strings.Repeat("b", 36), "[REDACTED]"},
		{"refresh token", "ghr_" + strings.Repeat("b", 36), "[REDACTED]"},
		{"fine grained PAT", "github_pat_" + strings.Repeat("a", 82), "[REDACTED]"},
		{"OpenAI key", "sk-" + strings.Repeat("b", 48), "[REDACTED]"},
		{"long OpenAI project key", "sk-proj-" + strings.Repeat("b_", 80), "[REDACTED]"},
		{"Anthropic key", "sk-ant-api03-" + strings.Repeat("b_", 48), "[REDACTED]"},
		{"Google API key", "AIzaSy" + strings.Repeat("a", 33), "[REDACTED]"},
		{"AWS access key", "AKIA" + strings.Repeat("A", 16), "[REDACTED]"},
		{"AWS temporary access key", "ASIA" + strings.Repeat("A", 16), "[REDACTED]"},
		{"Linear key", "lin_api_" + strings.Repeat("a", 40), "[REDACTED]"},
		{"Slack token", "xoxb-" + strings.Repeat("1", 12) + "-" + strings.Repeat("2", 12) + "-" + strings.Repeat("a", 24), "[REDACTED]"},
		{"JWT", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijklmnopqrstuvwx", "[REDACTED]"},
		{"bearer header", "Authorization: Bearer opaque.token-value+/==; status=401", "Authorization: Bearer [REDACTED]; status=401"},
		{"basic header", "Authorization: Basic dXNlcjpwYXNzd29yZA==", "Authorization: Basic [REDACTED]"},
		{"lowercase bearer", "authorization: bearer abcdefghijklmnop", "authorization: bearer [REDACTED]"},
		{"token assignment", "token=abcdefghijklmnop failed", "token=[REDACTED] failed"},
		{"API key header", "X-Api-Key: abcdefghijklmnop\nstatus: 401", "X-Api-Key: [REDACTED]\nstatus: 401"},
		{"environment assignment", "GITHUB_TOKEN=abcdefghijklmnop; retry", "GITHUB_TOKEN=[REDACTED]; retry"},
		{"JSON in diagnostic", `{"api_key":"abcdefghijklmnop","status":401}`, `{"api_key":[REDACTED],"status":401}`},
		{"quoted password", `password='long password with spaces' failed`, `password=[REDACTED] failed`},
		{"client secret", `client_secret: "abcdefghijklmnop"`, `client_secret: [REDACTED]`},
		{"URL query assignment", "https://example.com/?access_token=abcdefghijklmnop&status=401", "https://example.com/?access_token=[REDACTED]&status=401"},
		{"multiple credentials and path", "failed at /Users/private/grader.js token=abcdefghijklmnop api_key: 1234567890abcdef", "failed at [local path] token=[REDACTED] api_key: [REDACTED]"},
		{"windows path", `failed at C:\private\grader.js Bearer abcdefghijklmnop`, "failed at [local path] Bearer [REDACTED]"},
		{"empty", "", ""},
		{"harmless diagnostic", "expected failure: status=401, retry after 10 seconds", "expected failure: status=401, retry after 10 seconds"},
		{"missing credential", "token=missing api_key: unavailable password: unset", "token=missing api_key: unavailable password: unset"},
		{"token counts", "token_count=1234567890123456 tokens=1234567890123456", "token_count=1234567890123456 tokens=1234567890123456"},
		{"nonsensitive long assignment", "request_id=abcdefghijklmnop", "request_id=abcdefghijklmnop"},
		{"token name only", "GITHUB_TOKEN was not configured", "GITHUB_TOKEN was not configured"},
		{"URL diagnostic", "request failed at https://example.com/grader: 401", "request failed at https://example.com/grader: 401"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, harborSanitizeSensitiveText(tt.input))
			require.Equal(t, tt.want, harborSanitizeSensitiveText(tt.want), "sanitization must be idempotent")
		})
	}
}

func TestHarborSanitizeErrorJSONPreservesOtherFields(t *testing.T) {
	raw := json.RawMessage(`{"message":"Bearer abcdefghijklmnop","error":123,"value":9007199254740993,"details":[{"error":"token=abcdefghijklmnop","message":"expected failure","data":"token=abcdefghijklmnop"}],"prompt":"Bearer abcdefghijklmnop"}`)
	safe := harborSanitizeErrorJSON(raw)
	require.JSONEq(t, `{"message":"Bearer [REDACTED]","error":123,"value":9007199254740993,"details":[{"error":"token=[REDACTED]","message":"expected failure","data":"token=abcdefghijklmnop"}],"prompt":"Bearer abcdefghijklmnop"}`, string(safe))
	require.Contains(t, string(safe), `"value":9007199254740993`, "large numeric scores must remain exact")
	require.Contains(t, string(raw), `"message":"Bearer abcdefghijklmnop"`, "source JSON must remain unchanged")
}

func TestLogsHarborRegradingExportRedactsCredentials(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "run")
	writeSessionTestFile(t, source, "usage/aw_session.jsonl", sessionTestHeader+sessionTestAgentEvent)
	record := &RegradedRun{
		SourceDirectory: source,
		SessionError:    "Authorization: Bearer abcdefghijklmnop",
		OriginalGraders: json.RawMessage(`{"error":"api_key: abcdefghijklmnop"}`),
		Results:         []json.RawMessage{json.RawMessage(`{"id":"test","status":"error","error":"token=abcdefghijklmnop","details":[{"message":"sk-proj-abcdefghijklmnopqrstuvwxyz1234567890"}]}`)},
		OriginalEvidence: []RegradeOriginalEvidence{{
			Error: "password=abcdefghijklmnop",
			Data:  json.RawMessage(`{"message":"client_secret=abcdefghijklmnop"}`),
		}},
	}
	run := harborTestRun(source)
	run.Regrading = record
	output := filepath.Join(root, "export")
	require.NoError(t, logsExportHarborJob(t.Context(), output, []ProcessedRun{run}))
	trials := harborTestTrials(t, output)
	require.Len(t, trials, 1)
	raw, err := os.ReadFile(filepath.Join(output, harborExportDirectory, trials[0], "gh-aw", "regrading.json"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "abcdefghijklmnop")
	require.NotContains(t, string(raw), "sk-proj-")
	require.Contains(t, string(raw), "Bearer [REDACTED]")
	var safe RegradedRun
	require.NoError(t, json.Unmarshal(raw, &safe))
	require.JSONEq(t, `{"error":"api_key: [REDACTED]"}`, string(safe.OriginalGraders))
	require.JSONEq(t, `{"id":"test","status":"error","error":"token=[REDACTED]","details":[{"message":"[REDACTED]"}]}`, string(safe.Results[0]))
	require.Equal(t, "password=[REDACTED]", safe.OriginalEvidence[0].Error)
	require.JSONEq(t, `{"message":"client_secret=[REDACTED]"}`, string(safe.OriginalEvidence[0].Data))
	require.Equal(t, "Authorization: Bearer abcdefghijklmnop", record.SessionError)
	require.Contains(t, string(record.Results[0]), "token=abcdefghijklmnop", "original records must not be mutated")
}
