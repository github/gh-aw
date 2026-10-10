//go:build !integration

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/console"
)

// TestMissingToolSummaryDisplayFields verifies that Display fields are used by console rendering
func TestMissingToolSummaryDisplayFields(t *testing.T) {
	t.Parallel()
	// Create a MissingToolSummary with populated Display fields
	summaries := []MissingToolSummary{
		{
			Tool: "test-tool",
			AggregatedSummaryBase: AggregatedSummaryBase{
				Count:              5,
				Workflows:          []string{"workflow1", "workflow2", "workflow3"},
				WorkflowsDisplay:   "workflow1, workflow2, workflow3", // This should be rendered
				FirstReason:        "Tool not found in MCP server",
				FirstReasonDisplay: "Tool not found in MCP server", // This should be rendered
				RunIDs:             []int64{1, 2, 3},
			},
		},
	}

	// Render using console.RenderStructStdout
	output := console.RenderStructStdout(summaries)

	// Verify that Display fields are included in output
	if !strings.Contains(output, "workflow1, workflow2, workflow3") {
		t.Errorf("WorkflowsDisplay field not found in console output:\n%s", output)
	}

	if !strings.Contains(output, "Tool not found in MCP server") {
		t.Errorf("FirstReasonDisplay field not found in console output:\n%s", output)
	}

	// Verify headers are present
	if !strings.Contains(output, "Tool") {
		t.Errorf("Tool header not found in console output")
	}
	if !strings.Contains(output, "Occurrences") {
		t.Errorf("Occurrences header not found in console output")
	}
	if !strings.Contains(output, "Workflows") {
		t.Errorf("Workflows header not found in console output")
	}
	if !strings.Contains(output, "First Reason") {
		t.Errorf("First Reason header not found in console output")
	}
}

// TestMCPFailureSummaryDisplayFields verifies that MCP display fields retain their specific tags.
func TestMCPFailureSummaryDisplayFields(t *testing.T) {
	t.Parallel()
	// Create a MCPFailureSummary with populated Display field
	summaries := []MCPFailureSummary{
		{
			ServerName: "github-mcp-server",
			AggregatedSummaryBase: AggregatedSummaryBase{
				Count:            3,
				Workflows:        []string{"workflow-a", "workflow-b"},
				WorkflowsDisplay: "workflow-a, workflow-b", // This should be rendered
				RunIDs:           []int64{1, 2, 3},
			},
		},
	}

	output := console.RenderStructStdout(mcpFailureSummaryDisplays(summaries))

	// Verify that Display field is included in output
	if !strings.Contains(output, "workflow-a, workflow-b") {
		t.Errorf("WorkflowsDisplay field not found in console output:\n%s", output)
	}

	// Verify headers are present
	if !strings.Contains(output, "Server") {
		t.Errorf("Server header not found in console output")
	}
	if !strings.Contains(output, "Failures") {
		t.Errorf("Failures header not found in console output")
	}
	if !strings.Contains(output, "Workflows") {
		t.Errorf("Workflows header not found in console output")
	}
}

// TestMCPFailureSummaryJSONFields verifies that embedding AggregatedSummaryBase keeps
// the shared JSON fields flattened at the MCP failure summary level.
func TestMCPFailureSummaryJSONFields(t *testing.T) {
	t.Parallel()
	summary := MCPFailureSummary{
		ServerName: "github-mcp-server",
		AggregatedSummaryBase: AggregatedSummaryBase{
			Count:       3,
			Workflows:   []string{"workflow-a", "workflow-b"},
			FirstReason: "not part of the MCP failure schema",
			RunIDs:      []int64{1, 2, 3},
		},
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	output := string(data)
	for _, expected := range []string{
		`"server_name":"github-mcp-server"`,
		`"count":3`,
		`"workflows":["workflow-a","workflow-b"]`,
		`"run_ids":[1,2,3]`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected %s in JSON output: %s", expected, output)
		}
	}
	if strings.Contains(output, "first_reason") {
		t.Errorf("first_reason must not be added to the MCP failure JSON schema: %s", output)
	}
}
