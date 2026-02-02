//go:build !integration

package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// TestAuditReportIncludesInputSizes verifies that the audit report includes input sizes for MCP tools
func TestAuditReportIncludesInputSizes(t *testing.T) {
	// Create test data with input and output sizes
	run := WorkflowRun{
		DatabaseID:   999888,
		WorkflowName: "MCP Tool Test",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    time.Now(),
		Event:        "push",
		HeadBranch:   "main",
		URL:          "https://github.com/test/repo/actions/runs/999888",
		LogsPath:     "/tmp/test-logs",
	}

	metrics := LogMetrics{
		ToolCalls: []workflow.ToolCallInfo{
			{
				Name:          "github_search_issues",
				CallCount:     3,
				MaxInputSize:  512,  // Input size in tokens
				MaxOutputSize: 2048, // Output size in tokens
				MaxDuration:   2 * time.Second,
			},
			{
				Name:          "bash_ls",
				CallCount:     1,
				MaxInputSize:  64,
				MaxOutputSize: 256,
				MaxDuration:   500 * time.Millisecond,
			},
		},
	}

	processedRun := ProcessedRun{
		Run: run,
	}

	// Generate markdown report with empty downloaded files
	downloadedFiles := []FileInfo{}
	report := generateAuditReport(processedRun, metrics, downloadedFiles)

	// Verify the report contains the MCP Tool Usage section
	if !strings.Contains(report, "## MCP Tool Usage") {
		t.Error("Report should contain MCP Tool Usage section")
	}

	// Verify the table header includes "Max Input" column
	if !strings.Contains(report, "Max Input") {
		t.Error("Report should contain 'Max Input' column header")
	}

	// Verify input sizes are displayed
	if !strings.Contains(report, "512") {
		t.Error("Report should contain input size 512")
	}
	if !strings.Contains(report, "64") {
		t.Error("Report should contain input size 64")
	}

	// Verify output sizes are still displayed
	if !strings.Contains(report, "2.05k") && !strings.Contains(report, "2.0k") && !strings.Contains(report, "2048") {
		t.Error("Report should contain output size 2.05k, 2.0k or 2048")
	}

	// Verify durations are displayed
	if !strings.Contains(report, "2.0s") && !strings.Contains(report, "2s") {
		t.Error("Report should contain duration 2.0s or 2s")
	}

	// Verify the table has the correct number of columns (5: Tool, Calls, Max Input, Max Output, Max Duration)
	headerLine := ""
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, "Max Input") {
			headerLine = line
			break
		}
	}
	if headerLine != "" {
		// Count pipes to verify we have the right number of columns
		pipeCount := strings.Count(headerLine, "|")
		if pipeCount != 6 { // 6 pipes for 5 columns (includes leading and trailing)
			t.Errorf("Expected 6 pipes in header line, got %d. Line: %s", pipeCount, headerLine)
		}
	}
}

// TestAuditDataJSONIncludesInputSizes verifies that JSON output includes input sizes
func TestAuditDataJSONIncludesInputSizes(t *testing.T) {
	run := WorkflowRun{
		DatabaseID:   888999,
		WorkflowName: "JSON Test",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    time.Now(),
		Event:        "push",
		HeadBranch:   "main",
		URL:          "https://github.com/test/repo/actions/runs/888999",
		LogsPath:     testutil.TempDir(t, "test-*"),
	}

	metrics := LogMetrics{
		ToolCalls: []workflow.ToolCallInfo{
			{
				Name:          "github_issue_read",
				CallCount:     2,
				MaxInputSize:  256,
				MaxOutputSize: 1024,
				MaxDuration:   1 * time.Second,
			},
		},
	}

	processedRun := ProcessedRun{
		Run: run,
	}

	// Build audit data
	auditData := buildAuditData(processedRun, metrics, nil)

	// Verify tool usage data includes input sizes
	if len(auditData.ToolUsage) == 0 {
		t.Fatal("Expected tool usage data, got none")
	}

	toolUsage := auditData.ToolUsage[0]
	if toolUsage.MaxInputSize != 256 {
		t.Errorf("Expected MaxInputSize 256, got %d", toolUsage.MaxInputSize)
	}
	if toolUsage.MaxOutputSize != 1024 {
		t.Errorf("Expected MaxOutputSize 1024, got %d", toolUsage.MaxOutputSize)
	}

	// Verify JSON serialization includes input sizes
	jsonData, err := json.Marshal(auditData)
	if err != nil {
		t.Fatalf("Failed to marshal audit data: %v", err)
	}

	jsonStr := string(jsonData)
	if !strings.Contains(jsonStr, "max_input_size") {
		t.Error("JSON should contain max_input_size field")
	}
	if !strings.Contains(jsonStr, "\"max_input_size\":256") {
		t.Error("JSON should contain max_input_size value of 256")
	}
}

// TestToolUsageInfoStructure verifies the ToolUsageInfo structure has correct fields
func TestToolUsageInfoStructure(t *testing.T) {
	toolInfo := ToolUsageInfo{
		Name:          "test_tool",
		CallCount:     5,
		MaxInputSize:  128,
		MaxOutputSize: 512,
		MaxDuration:   "1s",
	}

	// Verify all fields are accessible
	if toolInfo.Name != "test_tool" {
		t.Error("Name field should be accessible")
	}
	if toolInfo.CallCount != 5 {
		t.Error("CallCount field should be accessible")
	}
	if toolInfo.MaxInputSize != 128 {
		t.Error("MaxInputSize field should be accessible")
	}
	if toolInfo.MaxOutputSize != 512 {
		t.Error("MaxOutputSize field should be accessible")
	}
	if toolInfo.MaxDuration != "1s" {
		t.Error("MaxDuration field should be accessible")
	}

	// Verify JSON tags are correct
	jsonData, err := json.Marshal(toolInfo)
	if err != nil {
		t.Fatalf("Failed to marshal tool info: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal tool info: %v", err)
	}

	if _, exists := parsed["max_input_size"]; !exists {
		t.Error("JSON should have max_input_size field")
	}
	if _, exists := parsed["max_output_size"]; !exists {
		t.Error("JSON should have max_output_size field")
	}
}
