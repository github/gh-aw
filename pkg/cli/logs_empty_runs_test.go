//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestBuildLogsDataEmptyRuns tests that buildLogsData works correctly with zero runs
func TestBuildLogsDataEmptyRuns(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Build logs data with no runs
	logsData := buildLogsData([]ProcessedRun{}, tmpDir, nil)

	// Verify summary has zero values but all fields present
	if logsData.Summary.TotalRuns != 0 {
		t.Errorf("Expected TotalRuns to be 0, got %d", logsData.Summary.TotalRuns)
	}
	if logsData.Summary.TotalTokens != 0 {
		t.Errorf("Expected TotalTokens to be 0, got %d", logsData.Summary.TotalTokens)
	}
	if logsData.Summary.TotalCost != 0 {
		t.Errorf("Expected TotalCost to be 0, got %f", logsData.Summary.TotalCost)
	}

	// Verify runs array is empty
	if len(logsData.Runs) != 0 {
		t.Errorf("Expected empty runs array, got %d runs", len(logsData.Runs))
	}
}

// TestRenderLogsJSONEmptyRuns tests that JSON rendering works correctly with zero runs
func TestRenderLogsJSONEmptyRuns(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Create logs data with no runs
	logsData := buildLogsData([]ProcessedRun{}, tmpDir, nil)

	// Redirect stdout to capture JSON output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Render JSON
	err := renderLogsJSON(logsData)
	if err != nil {
		t.Fatalf("Failed to render JSON: %v", err)
	}

	// Restore stdout and read captured output
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	// Verify it's valid JSON
	var parsedData LogsData
	if err := json.Unmarshal([]byte(output), &parsedData); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify key fields exist and have correct zero values
	if parsedData.Summary.TotalRuns != 0 {
		t.Errorf("Expected TotalRuns 0, got %d", parsedData.Summary.TotalRuns)
	}
	if parsedData.Summary.TotalTokens != 0 {
		t.Errorf("Expected TotalTokens 0, got %d", parsedData.Summary.TotalTokens)
	}

	// Verify the JSON contains the total_tokens field
	// This is the key test - the field should be present even when zero
	var jsonMap map[string]any
	if err := json.Unmarshal([]byte(output), &jsonMap); err != nil {
		t.Fatalf("Failed to parse JSON as map: %v", err)
	}

	summary, ok := jsonMap["summary"].(map[string]any)
	if !ok {
		t.Fatalf("Expected summary to be a map, got %T", jsonMap["summary"])
	}

	if _, exists := summary["total_tokens"]; !exists {
		t.Errorf("Expected total_tokens field to exist in summary, but it was missing. Summary: %+v", summary)
	}
}
