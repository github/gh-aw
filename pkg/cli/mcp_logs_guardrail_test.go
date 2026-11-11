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

	"github.com/github/gh-aw/pkg/constants"
)

func TestBuildLogsFileResponse_WritesFile(t *testing.T) {
	// buildLogsFileResponse always writes to a file and returns file_path
	output := `{"summary": {"total_runs": 1}, "runs": []}`

	cacheDir := filepath.Join(t.TempDir(), "cache")
	result := buildLogsFileResponseInDir(output, cacheDir)

	// Verify the result is valid JSON
	var response MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Response should be valid JSON: %v", err)
	}

	// Verify message is set
	if response.Message == "" {
		t.Error("Response should have a message")
	}

	// Verify file_path is set
	if response.FilePath == "" {
		t.Error("Response should have a file_path")
	}

	// Verify the file is in the cache directory (not the artifact directory)
	if filepath.Dir(response.FilePath) != cacheDir {
		t.Errorf("File should be in cache dir %q, got %q", cacheDir, response.FilePath)
	}

	// Verify the file was actually created and contains the output
	data, err := os.ReadFile(response.FilePath)
	if err != nil {
		t.Fatalf("File should exist at file_path: %v", err)
	}
	if string(data) != output {
		t.Errorf("File content should match input: got %q, want %q", string(data), output)
	}

	// Cleanup
	_ = os.Remove(response.FilePath)
}

func TestBuildLogsFileResponse_SetsPrivatePermissions(t *testing.T) {
	output := `{"summary":{"total_runs":1}}`
	cacheDir := filepath.Join(t.TempDir(), "cache")
	result := buildLogsFileResponseInDir(output, cacheDir)

	var response MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Response should be valid JSON: %v", err)
	}
	if response.FilePath == "" {
		t.Fatal("Response should include file_path")
	}

	cacheInfo, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("Cache directory should exist: %v", err)
	}
	cachePerm := cacheInfo.Mode().Perm()
	assert.Equal(t, constants.DirPermOwnerOnly, cachePerm, "only the owner's UID may traverse or list cached reports")

	fileInfo, err := os.Stat(response.FilePath)
	if err != nil {
		t.Fatalf("Cache file should exist: %v", err)
	}
	filePerm := fileInfo.Mode().Perm()
	assert.Equal(t, constants.FilePermSensitive, filePerm, "group and other users must not read reports")
	data, err := os.ReadFile(response.FilePath)
	require.NoError(t, err, "same-UID agents must retain access")
	assert.Equal(t, output, string(data))

	_ = os.Remove(response.FilePath)
}

func TestBuildLogsFileResponse_RestrictsLegacyCache(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		name := "new report"
		if reuse {
			name = "reused report"
		}
		t.Run(name, func(t *testing.T) {
			cacheDir := filepath.Join(t.TempDir(), "cache")
			output := `{"summary":{"total_runs":1}}`
			var original MCPLogsGuardrailResponse
			require.NoError(t, json.Unmarshal([]byte(buildLogsFileResponseInDir(output, cacheDir)), &original))
			require.NotEmpty(t, original.FilePath)
			require.NoError(t, os.Chmod(original.FilePath, constants.FilePermPublic))
			require.NoError(t, os.Chmod(cacheDir, constants.DirPermPublic))

			if !reuse {
				output = `{"summary":{"total_runs":2}}`
			}
			var response MCPLogsGuardrailResponse
			require.NoError(t, json.Unmarshal([]byte(buildLogsFileResponseInDir(output, cacheDir)), &response))
			require.NotEmpty(t, response.FilePath)

			cacheInfo, err := os.Stat(cacheDir)
			require.NoError(t, err)
			assert.Equal(t, constants.DirPermOwnerOnly, cacheInfo.Mode().Perm(), "deny other UIDs access to every legacy report, not only the requested report")
			fileInfo, err := os.Stat(response.FilePath)
			require.NoError(t, err)
			assert.Equal(t, constants.FilePermSensitive, fileInfo.Mode().Perm())
			data, err := os.ReadFile(response.FilePath)
			require.NoError(t, err)
			assert.Equal(t, output, string(data))
			data, err = os.ReadFile(original.FilePath)
			require.NoError(t, err, "same-UID readers must retain access to legacy reports")
			assert.JSONEq(t, `{"summary":{"total_runs":1}}`, string(data))
		})
	}
}

func TestBuildLogsFileResponse_RejectsCacheSymlinks(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "report"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			cacheDir := filepath.Join(root, "cache")
			target := filepath.Join(root, "target")
			output := `{"summary":{"total_runs":1}}`
			var targetMode os.FileMode
			if directory {
				targetMode = constants.DirPermPublic
				require.NoError(t, os.Mkdir(target, targetMode))
				require.NoError(t, os.Symlink(target, cacheDir))
			} else {
				targetMode = constants.FilePermPublic
				require.NoError(t, os.WriteFile(target, []byte("unrelated"), targetMode))
				var response MCPLogsGuardrailResponse
				require.NoError(t, json.Unmarshal([]byte(buildLogsFileResponseInDir(output, cacheDir)), &response))
				require.NotEmpty(t, response.FilePath)
				require.NoError(t, os.Remove(response.FilePath))
				require.NoError(t, os.Symlink(target, response.FilePath))
			}
			require.NoError(t, os.Chmod(target, targetMode))

			var response MCPLogsGuardrailResponse
			require.NoError(t, json.Unmarshal([]byte(buildLogsFileResponseInDir(output, cacheDir)), &response))
			assert.Empty(t, response.FilePath)
			assert.Contains(t, response.Message, "is a symlink; refusing to use it")
			info, err := os.Stat(target)
			require.NoError(t, err)
			assert.Equal(t, targetMode, info.Mode().Perm(), "migration must not chmod a symlink target")
		})
	}
}

func TestBuildLogsFileResponse_ContentDeduplication(t *testing.T) {
	// Same content should yield the same file path (content-addressed)
	output := `{"summary": {"total_runs": 5}, "runs": []}`

	cacheDir := t.TempDir()
	result1 := buildLogsFileResponseInDir(output, cacheDir)
	result2 := buildLogsFileResponseInDir(output, cacheDir)

	var r1, r2 MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result1), &r1); err != nil {
		t.Fatalf("First response should be valid JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(result2), &r2); err != nil {
		t.Fatalf("Second response should be valid JSON: %v", err)
	}

	if r1.FilePath != r2.FilePath {
		t.Errorf("Identical content should produce the same file path: got %q and %q", r1.FilePath, r2.FilePath)
	}

	// Verify the file exists only once (not duplicated)
	if _, err := os.Stat(r1.FilePath); os.IsNotExist(err) {
		t.Errorf("Cached file should exist at %q", r1.FilePath)
	}

	// Cleanup
	_ = os.Remove(r1.FilePath)
}

func TestBuildLogsFileResponse_LargeOutput(t *testing.T) {
	// buildLogsFileResponse should always write to file regardless of output size
	largeOutput := strings.Repeat("x", 50000)

	result := buildLogsFileResponseInDir(largeOutput, t.TempDir())

	var response MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Response should be valid JSON: %v", err)
	}

	if response.FilePath == "" {
		t.Error("Large output should also produce a file_path")
	}

	// Verify the file contains the large output
	data, err := os.ReadFile(response.FilePath)
	if err != nil {
		t.Fatalf("File should exist at file_path: %v", err)
	}
	if len(data) != len(largeOutput) {
		t.Errorf("File size mismatch: got %d, want %d", len(data), len(largeOutput))
	}

	// Cleanup
	_ = os.Remove(response.FilePath)
}

func TestBuildLogsFileResponse_ResponseStructure(t *testing.T) {
	output := `{"summary": {"total_runs": 2}}`

	result := buildLogsFileResponseInDir(output, t.TempDir())

	var response MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Should return valid JSON: %v", err)
	}

	if response.Message == "" {
		t.Error("JSON should have message field")
	}

	if response.FilePath == "" {
		t.Error("JSON should have file_path field")
	}

	// Cleanup
	_ = os.Remove(response.FilePath)
}

func TestBuildLogsFileResponse_MarksPartialResults(t *testing.T) {
	output := `{"summary":{"total_runs":2},"continuation":{"message":"Timeout reached. Use these parameters to continue fetching more logs.","before_run_id":42,"count":100}}`

	result := buildLogsFileResponseInDir(output, t.TempDir())

	var response MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Response should be valid JSON: %v", err)
	}

	if !response.Partial {
		t.Error("Response should be marked partial when a continuation is present")
	}
	if response.Continuation == nil {
		t.Fatal("Response should include the continuation cursor")
	}
	if response.Continuation.BeforeRunID != 42 {
		t.Errorf("Continuation before_run_id mismatch: got %d, want 42", response.Continuation.BeforeRunID)
	}
	if !strings.Contains(response.Message, "PARTIAL RESULTS") {
		t.Errorf("Message should indicate partial results, got %q", response.Message)
	}

	// Cleanup
	_ = os.Remove(response.FilePath)
}

func TestBuildLogsFileResponse_CompleteResultsNotPartial(t *testing.T) {
	output := `{"summary":{"total_runs":2},"runs":[]}`

	result := buildLogsFileResponseInDir(output, t.TempDir())

	var response MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Response should be valid JSON: %v", err)
	}

	if response.Partial {
		t.Error("Response should not be marked partial without a continuation")
	}
	if response.Continuation != nil {
		t.Errorf("Response should not include a continuation, got %+v", response.Continuation)
	}

	// Cleanup
	_ = os.Remove(response.FilePath)
}

func TestBuildLogsFileResponse_CollectionCoverage(t *testing.T) {
	tests := []struct {
		name          string
		output        string
		partial       bool
		status        string
		reasons       []string
		continuations []WorkflowContinuation
		message       string
	}{
		{
			name: "per-target continuations without a singular cursor",
			output: `{"runs":[],"continuations":[
				{"repository":"github/gh-aw","workflow_name":"audit","message":"Count limit reached.","count":100,"end_date":"2026-10-18T16:49:00Z","before_run_id":42},
				{"repository":"example/other","workflow_name":"monitor","message":"Timeout reached.","start_date":"2026-10-12","before_run_id":17}
			]}`,
			partial: true,
			continuations: []WorkflowContinuation{
				{"github/gh-aw", ContinuationData{WorkflowName: "audit", Message: "Count limit reached.", Count: 100, EndDate: "2026-10-18T16:49:00Z", BeforeRunID: 42}},
				{"example/other", ContinuationData{WorkflowName: "monitor", Message: "Timeout reached.", StartDate: "2026-10-12", BeforeRunID: 17}},
			},
			message: "every per-target entry in continuations",
		},
		{
			name:    "bounded retrieval stopped without episode rows",
			output:  `{"runs":[],"episode_retrieval":{"stop_reasons":["timeout","run_limit"],"issues":[{"reason":"discovery_failed","message":"diagnostic only in file"}]}}`,
			partial: true,
			status:  "partial",
			reasons: []string{"retrieval_discovery_failed", "retrieval_run_limit", "retrieval_timeout"},
			message: "no resumable cursor is available",
		},
		{
			name:    "retrieval issues alone preserve unavailable evidence",
			output:  `{"runs":[{"intentional_failure":true},{"conclusion":"failure"}],"episode_retrieval":{"issues":[{"reason":"recorded_run_unavailable","message":"diagnostic only in file"}]}}`,
			partial: true,
			status:  "partial",
			reasons: []string{"retrieval_recorded_run_unavailable"},
			message: "no resumable cursor is available",
		},
		{
			name:    "evidence gaps are not resumable collection",
			output:  `{"episodes":[{"coverage":{"scope":"selected_runs","status":"partial","reasons":["missing_recorded_parent"]}}]}`,
			partial: true,
			status:  "partial",
			reasons: []string{"missing_recorded_parent"},
			message: "episode collection or evidence has known gaps",
		},
		{
			name:    "coverage reasons are stable and deduplicated",
			output:  `{"episode_retrieval":{"stop_reasons":["timeout"]},"episodes":[{"coverage":{"status":"partial","reasons":["retrieval_timeout","missing_recorded_root"]}},{"coverage":{"status":"unknown","reasons":["missing_recorded_root"]}}]}`,
			partial: true,
			status:  "partial",
			reasons: []string{"missing_recorded_root", "retrieval_timeout"},
			message: "Episode membership coverage is partial",
		},
		{
			name:    "truncation flag preserves partial even without status",
			output:  `{"episodes":[{"coverage":{"collection_truncated":true}}]}`,
			partial: true,
			status:  "partial",
			message: "PARTIAL RESULTS",
		},
		{
			name:    "bounded retrieval without gaps remains unknown",
			output:  `{"episode_retrieval":{"stop_reasons":[]},"episodes":[{"coverage":{"scope":"selected_runs","status":"unknown"}}]}`,
			status:  "unknown",
			message: "Episode membership coverage is unknown",
		},
		{
			name:    "selected episodes without retrieval remain unknown",
			output:  `{"episodes":[{"coverage":{"scope":"selected_runs","status":"unknown"}}]}`,
			status:  "unknown",
			message: "do not establish complete episode membership",
		},
		{
			name:    "empty discovery carries no episode coverage claim",
			output:  `{"runs":[],"episodes":[],"continuations":[]}`,
			message: "collected report",
		},
		{
			name:    "truncated report is explicitly unknown",
			output:  `{"episodes":[`,
			status:  "unknown",
			reasons: []string{"invalid_report_json"},
			message: "Collection state is unknown",
		},
		{
			name:    "non JSON report is explicitly unknown",
			output:  "diagnostic only in file",
			status:  "unknown",
			reasons: []string{"invalid_report_json"},
			message: "Collection state is unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response MCPLogsGuardrailResponse
			require.NoError(t, json.Unmarshal([]byte(buildLogsFileResponseInDir(tt.output, t.TempDir())), &response))
			require.NotEmpty(t, response.FilePath)
			t.Cleanup(func() { assert.NoError(t, os.Remove(response.FilePath)) })
			assert.Equal(t, tt.partial, response.Partial)
			assert.Equal(t, tt.status, response.CoverageStatus)
			assert.Equal(t, tt.reasons, response.CoverageReasons)
			assert.Equal(t, tt.continuations, response.Continuations)
			assert.Nil(t, response.Continuation)
			assert.Contains(t, response.Message, tt.message)
			assert.NotContains(t, response.Message, "read the full data")
			assert.NotContains(t, response.Message, "diagnostic only in file")
			data, err := os.ReadFile(response.FilePath)
			require.NoError(t, err)
			assert.Equal(t, tt.output, string(data), "retain the original report and intentional-failure semantics unchanged")
		})
	}
}

func TestExtractLogsCollectionState_PreservesBothCursorShapes(t *testing.T) {
	output := `{"continuation":{"message":"Count limit reached.","before_run_id":42},"continuations":[{"repository":"github/gh-aw","message":"Timeout reached.","before_run_id":17}],"runs":[{"intentional_failure":true}]}`
	response := extractLogsCollectionState(output)
	require.NotNil(t, response.Continuation)
	assert.Equal(t, int64(42), response.Continuation.BeforeRunID)
	require.Len(t, response.Continuations, 1)
	assert.Equal(t, int64(17), response.Continuations[0].BeforeRunID)
	assert.True(t, response.Partial)
	assert.Empty(t, response.CoverageStatus)
}

func TestExtractLogsCollectionState_InvalidReport(t *testing.T) {
	for _, output := range []string{"", "diagnostic only in file", `{"continuation":`, "null", "[]", `"report"`, "42"} {
		t.Run(output, func(t *testing.T) {
			response := extractLogsCollectionState(output)
			assert.Equal(t, "unknown", response.CoverageStatus)
			assert.Equal(t, []string{"invalid_report_json"}, response.CoverageReasons)
			assert.False(t, response.Partial, "unknown collection state is not known partial evidence")
			assert.Nil(t, response.Continuation)
			assert.Empty(t, response.Continuations)
			assert.Contains(t, response.Message, "Collection state could not be fully decoded")
			assert.NotContains(t, response.Message, "diagnostic only in file")
		})
	}
}

func TestExtractLogsCollectionState_MalformedSectionIndependence(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		status  string
		partial bool
		reasons []string
		single  bool
		multi   bool
	}{
		{
			name:    "malformed coverage preserves single cursor",
			output:  `{"continuation":{"before_run_id":42},"episodes":[{"coverage":{"status":17}}]}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
			single:  true,
		},
		{
			name:    "malformed coverage preserves per target cursors",
			output:  `{"continuations":[{"repository":"github/gh-aw","before_run_id":17}],"episodes":[{"coverage":{"reasons":17}}]}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
			multi:   true,
		},
		{
			name:    "malformed coverage and retrieval preserve both cursor shapes",
			output:  `{"continuation":{"before_run_id":42},"continuations":[{"repository":"github/gh-aw","before_run_id":17}],"episodes":"invalid","episode_retrieval":"invalid"}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_episode_coverage", "invalid_episode_retrieval"},
			single:  true,
			multi:   true,
		},
		{
			name:    "malformed single cursor preserves per target cursors",
			output:  `{"continuation":{"before_run_id":"invalid"},"continuations":[{"repository":"github/gh-aw","before_run_id":17}]}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_continuation"},
			multi:   true,
		},
		{
			name:    "malformed per target cursors preserve single cursor",
			output:  `{"continuation":{"before_run_id":42},"continuations":"invalid"}`,
			status:  "unknown",
			partial: true,
			reasons: []string{"invalid_continuations"},
			single:  true,
		},
		{
			name:    "malformed coverage alone is unknown not partial",
			output:  `{"episodes":[{"coverage":17}]}`,
			status:  "unknown",
			reasons: []string{"invalid_episode_coverage"},
		},
		{
			name:    "malformed retrieval alone is unknown not partial",
			output:  `{"episode_retrieval":{"stop_reasons":17}}`,
			status:  "unknown",
			reasons: []string{"invalid_episode_retrieval"},
		},
		{
			name:    "known partial row overrides later malformed row",
			output:  `{"episodes":[{"coverage":{"status":"partial","reasons":["missing_recorded_parent"]}},{"coverage":17}]}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage", "missing_recorded_parent"},
		},
		{
			name:    "known partial row overrides earlier malformed row",
			output:  `{"episodes":[{"coverage":17},{"coverage":{"collection_truncated":true}}]}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
		},
		{
			name:    "valid status survives malformed reasons in same coverage",
			output:  `{"episodes":[{"coverage":{"status":"partial","reasons":17}}]}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
		},
		{
			name:    "valid truncation survives malformed status in same coverage",
			output:  `{"episodes":[{"coverage":{"status":17,"collection_truncated":true}}]}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage"},
		},
		{
			name:    "retrieval stop overrides malformed coverage",
			output:  `{"episodes":[{"coverage":17}],"episode_retrieval":{"stop_reasons":["timeout"]}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_coverage", "retrieval_timeout"},
		},
		{
			name:    "coverage partial overrides malformed retrieval",
			output:  `{"episodes":[{"coverage":{"status":"partial"}}],"episode_retrieval":17}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_retrieval"},
		},
		{
			name:    "retrieval issue survives malformed stop reasons",
			output:  `{"episode_retrieval":{"stop_reasons":17,"issues":[{"reason":"discovery_failed","message":"diagnostic only in file"}]}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_retrieval", "retrieval_discovery_failed"},
		},
		{
			name:    "retrieval stop survives malformed issues",
			output:  `{"episode_retrieval":{"stop_reasons":["timeout"],"issues":17}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_retrieval", "retrieval_timeout"},
		},
		{
			name:    "valid retrieval reasons survive invalid array entries",
			output:  `{"episode_retrieval":{"stop_reasons":[17,"timeout"],"issues":[17,{"reason":"discovery_failed"}]}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"invalid_episode_retrieval", "retrieval_discovery_failed", "retrieval_timeout"},
		},
		{
			name:    "unrelated retrieval metadata does not erase stop reasons",
			output:  `{"episode_retrieval":{"start_date":"future format","stop_reasons":["timeout"]}}`,
			status:  "partial",
			partial: true,
			reasons: []string{"retrieval_timeout"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := extractLogsCollectionState(tt.output)
			assert.Equal(t, tt.status, response.CoverageStatus)
			assert.Equal(t, tt.partial, response.Partial)
			assert.Equal(t, tt.reasons, response.CoverageReasons)
			if tt.single {
				require.NotNil(t, response.Continuation)
				assert.Equal(t, int64(42), response.Continuation.BeforeRunID)
			} else {
				assert.Nil(t, response.Continuation)
			}
			if tt.multi {
				require.Len(t, response.Continuations, 1)
				assert.Equal(t, "github/gh-aw", response.Continuations[0].Repository)
				assert.Equal(t, int64(17), response.Continuations[0].BeforeRunID)
			} else {
				assert.Empty(t, response.Continuations)
			}
			if len(tt.reasons) > 0 && strings.HasPrefix(tt.reasons[0], "invalid_") {
				assert.Contains(t, response.Message, "Collection state could not be fully decoded")
			}
			assert.NotContains(t, response.Message, "diagnostic only in file")
		})
	}
}

func TestBuildLogsFileResponse_SurfacesStaleDataWarning(t *testing.T) {
	output := `{"summary":{"total_runs":1},"runs":[],"stale_warning":"No start_date/end_date was specified, and the most recent run in this result is 11 days old."}`

	result := buildLogsFileResponseInDir(output, t.TempDir())

	var response MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Response should be valid JSON: %v", err)
	}

	if !strings.Contains(response.Message, "WARNING:") {
		t.Errorf("Message should surface the embedded warning, got %q", response.Message)
	}
	if !strings.Contains(response.Message, "No start_date/end_date was specified") {
		t.Errorf("Message should include the stale-data warning text, got %q", response.Message)
	}

	// Cleanup
	_ = os.Remove(response.FilePath)
}

func TestBuildLogsFileResponse_DoesNotWarnOnOrdinaryMessage(t *testing.T) {
	// A non-stale "message" field (e.g. the usage-only artifact hint) must not
	// be relabeled as a WARNING; only the dedicated "stale_warning" field should
	// trigger the WARNING prefix.
	output := `{"summary":{"total_runs":1},"runs":[],"message":"Only the usage artifact was downloaded."}`

	result := buildLogsFileResponseInDir(output, t.TempDir())

	var response MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Response should be valid JSON: %v", err)
	}

	if strings.Contains(response.Message, "WARNING:") {
		t.Errorf("Message should not contain WARNING for a non-stale message, got %q", response.Message)
	}

	// Cleanup
	_ = os.Remove(response.FilePath)
}
