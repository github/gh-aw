//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPServer_LogsGuardrail tests the output size guardrail on the logs tool
func TestMCPServer_LogsGuardrail(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	// Start the MCP server as a subprocess
	serverCmd := exec.Command(binaryPath, "mcp-server")
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	t.Run("small output passes through normally", func(t *testing.T) {
		// This test is informational - it documents expected behavior
		// In a real environment with workflows, calling logs with count=1 and jq filter
		// should produce output small enough to pass through without triggering guardrail
		t.Skip("Skipping real logs call test - requires repository with workflows")
	})

	t.Run("guardrail provides schema and suggestions", func(t *testing.T) {
		// We can't easily trigger the guardrail in a real scenario without
		// having a large amount of logs, so this test documents the expected
		// behavior and structure of the guardrail response

		// Test that checkLogsOutputSize produces the expected structure
		// Default limit is 12000 tokens = 48000 characters
		// Use 50000 to safely exceed the limit
		largeOutput := strings.Repeat("x", 50000)
		guardrailJSON, triggered := checkLogsOutputSize(largeOutput, 0)

		if !triggered {
			t.Fatal("Guardrail should be triggered for large output")
		}

		// Parse the guardrail response
		var guardrail MCPLogsGuardrailResponse
		if err := json.Unmarshal([]byte(guardrailJSON), &guardrail); err != nil {
			t.Fatalf("Guardrail response should be valid JSON: %v", err)
		}

		// Verify guardrail has all expected components
		if guardrail.Message == "" {
			t.Error("Guardrail should have a message")
		}

		if !strings.Contains(guardrail.Message, "exceeds the limit") {
			t.Error("Message should explain the issue")
		}

		if !strings.Contains(guardrail.Message, "tokens") {
			t.Error("Message should mention tokens")
		}

		if guardrail.Schema.Type != "object" {
			t.Error("Schema should be object type")
		}

		if len(guardrail.Schema.Fields) == 0 {
			t.Error("Schema should have fields")
		}

		// Verify some expected fields are in the schema
		expectedFields := []string{"summary", "runs", "tool_usage", "errors_and_warnings"}
		for _, field := range expectedFields {
			if _, ok := guardrail.Schema.Fields[field]; !ok {
				t.Errorf("Schema should include field '%s'", field)
			}
		}
	})
}
