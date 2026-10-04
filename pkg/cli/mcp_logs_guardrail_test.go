//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildLogsFileResponse_WritesFile(t *testing.T) {
	// buildLogsFileResponse always writes to a file and returns file_path
	output := `{"summary": {"total_runs": 1}, "runs": []}`

	result := buildLogsFileResponse(output)

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
	if !strings.HasPrefix(response.FilePath, mcpLogsCacheDir) {
		t.Errorf("File should be in cache dir %q, got %q", mcpLogsCacheDir, response.FilePath)
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

func TestBuildLogsFileResponse_SetsReadablePermissions(t *testing.T) {
	output := `{"summary":{"total_runs":1}}`
	result := buildLogsFileResponse(output)

	var response MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Response should be valid JSON: %v", err)
	}
	if response.FilePath == "" {
		t.Fatal("Response should include file_path")
	}

	cacheInfo, err := os.Stat(mcpLogsCacheDir)
	if err != nil {
		t.Fatalf("Cache directory should exist: %v", err)
	}
	cachePerm := cacheInfo.Mode().Perm()
	if cachePerm&0o005 != 0o005 {
		t.Errorf("Cache directory should be accessible to other users (expected other r-x bits set), got mode %o", cachePerm)
	}

	fileInfo, err := os.Stat(response.FilePath)
	if err != nil {
		t.Fatalf("Cache file should exist: %v", err)
	}
	filePerm := fileInfo.Mode().Perm()
	if filePerm&0o004 != 0o004 {
		t.Errorf("Cache file should be world-readable (expected other read bit set), got mode %o", filePerm)
	}

	_ = os.Remove(response.FilePath)
}

func TestMakeMCPFirstRequestArtifactsReadable(t *testing.T) {
	baseDir := t.TempDir()
	parentDir := filepath.Join(baseDir, "aw-mcp")
	outputDir := filepath.Join(parentDir, "logs")
	nestedDir := filepath.Join(outputDir, "run-1", "sandbox", "firewall", "logs", "api-proxy-logs")
	if err := os.MkdirAll(nestedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(nestedDir, "event-logs.jsonl")
	if err := os.WriteFile(filePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	unselectedDir := filepath.Join(outputDir, "run-2", "sandbox", "firewall", "logs", "api-proxy-logs")
	if err := os.MkdirAll(unselectedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	unselectedFile := filepath.Join(unselectedDir, "event-logs.jsonl")
	if err := os.WriteFile(unselectedFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(nestedDir, "private.txt")
	if err := os.WriteFile(privatePath, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := makeMCPFirstRequestArtifactsReadable(outputDir, []string{"1"}); err != nil {
		t.Fatalf("makeMCPFirstRequestArtifactsReadable() error = %v", err)
	}

	for path, want := range map[string]os.FileMode{
		parentDir:      0o755,
		outputDir:      0o755,
		nestedDir:      0o755,
		filePath:       0o644,
		privatePath:    0o600,
		unselectedFile: 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("permissions for %q = %o, want %o", path, got, want)
		}
	}
}

func TestMakeMCPFirstRequestArtifactsReadableRejectsSymlinks(t *testing.T) {
	baseDir := t.TempDir()
	outputDir := filepath.Join(baseDir, "logs")
	linkDir := filepath.Join(outputDir, "run-1", "sandbox", "firewall", "logs", "api-proxy-logs")
	if err := os.MkdirAll(linkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(baseDir, "private.jsonl")
	if err := os.WriteFile(targetPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, filepath.Join(linkDir, "event-logs.jsonl")); err != nil {
		t.Fatal(err)
	}

	if err := makeMCPFirstRequestArtifactsReadable(outputDir, []string{"1"}); err == nil {
		t.Fatal("makeMCPFirstRequestArtifactsReadable() error = nil, want symlink error")
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("target permissions = %o, want 600", got)
	}
}

func TestMCPRunIDs(t *testing.T) {
	got := mcpRunIDs([]string{
		"123",
		"https://github.com/owner/repo/actions/runs/456",
		"https://github.com/owner/repo/actions/runs/789/job/101#step:2:1",
		"https://github.com/owner/repo/actions/runs/456",
		"invalid",
	})
	want := []string{"123", "456", "789"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mcpRunIDs() = %v, want %v", got, want)
	}
}

func TestMCPLogsRequestFirstRequestArtifacts(t *testing.T) {
	for _, test := range []struct {
		name      string
		artifacts []string
		want      bool
	}{
		{name: "compact metadata", artifacts: []string{"info", "usage"}},
		{name: "agent logs", artifacts: []string{"agent"}, want: true},
		{name: "firewall logs", artifacts: []string{"firewall"}, want: true},
		{name: "all artifacts", artifacts: []string{"all"}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := mcpLogsRequestFirstRequestArtifacts(test.artifacts); got != test.want {
				t.Errorf("mcpLogsRequestFirstRequestArtifacts(%v) = %v, want %v", test.artifacts, got, test.want)
			}
		})
	}
}

func TestBuildLogsFileResponse_ContentDeduplication(t *testing.T) {
	// Same content should yield the same file path (content-addressed)
	output := `{"summary": {"total_runs": 5}, "runs": []}`

	result1 := buildLogsFileResponse(output)
	result2 := buildLogsFileResponse(output)

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

	result := buildLogsFileResponse(largeOutput)

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

	result := buildLogsFileResponse(output)

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

	result := buildLogsFileResponse(output)

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

	result := buildLogsFileResponse(output)

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

func TestBuildLogsFileResponse_SurfacesStaleDataWarning(t *testing.T) {
	output := `{"summary":{"total_runs":1},"runs":[],"stale_warning":"No start_date/end_date was specified, and the most recent run in this result is 11 days old."}`

	result := buildLogsFileResponse(output)

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

	result := buildLogsFileResponse(output)

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
