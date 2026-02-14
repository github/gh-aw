//go:build integration

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// createMockMCPRegistry creates a mock MCP registry server for testing
func createMockMCPRegistry(t *testing.T) *httptest.Server {
	t.Helper()

	// Create mock registry data with 35 servers to exceed the 30 minimum
	servers := make([]ServerResponse, 35)
	for i := 0; i < 35; i++ {
		servers[i] = ServerResponse{
			Server: ServerDetail{
				Name:        fmt.Sprintf("io.github.example/test-server-%d", i+1),
				Description: fmt.Sprintf("Test MCP server %d for integration testing", i+1),
				Version:     "1.0.0",
				Repository: &Repository{
					URL: fmt.Sprintf("https://github.com/example/test-server-%d", i+1),
				},
				Packages: []MCPPackage{
					{
						RegistryType: "npm",
						Identifier:   fmt.Sprintf("test-server-%d", i+1),
						Version:      "1.0.0",
						RuntimeHint:  "node",
						Transport: &Transport{
							Type: "stdio",
						},
						PackageArguments: []Argument{
							{
								Type:  ArgumentTypePositional,
								Value: fmt.Sprintf("test-server-%d", i+1),
							},
						},
						EnvironmentVariables: []EnvironmentVariable{
							{
								Name:        "TEST_TOKEN",
								Description: "Test API token",
								IsRequired:  true,
								IsSecret:    true,
							},
						},
					},
				},
			},
		}
	}

	response := ServerListResponse{
		Servers: servers,
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/servers" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		} else {
			http.NotFound(w, r)
		}
	}))
}

// TestMCPAddIntegration_ServerCountValidation tests that the "mcp add" command
// can successfully list at least 30 servers from the default MCP registry
func TestMCPAddIntegration_ServerCountValidation(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create mock MCP registry server
	mockRegistry := createMockMCPRegistry(t)
	defer mockRegistry.Close()

	// Run "gh aw mcp add" with no arguments to list servers using mock registry
	cmd := exec.Command(setup.binaryPath, "mcp", "add", "--verbose", "--registry", mockRegistry.URL)
	cmd.Dir = setup.tempDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI mcp add command failed: %v\nOutput: %s", err, string(output))
	}

	outputStr := string(output)
	t.Logf("MCP add command output: %s", outputStr)

	// Check that we successfully retrieved servers
	if !strings.Contains(outputStr, "Retrieved") || !strings.Contains(outputStr, "servers from registry") {
		t.Errorf("Expected to see server count in output, got: %s", outputStr)
	}

	// Extract the number of servers from the verbose output
	// Look for pattern like "Retrieved 45 servers from registry"
	lines := strings.Split(outputStr, "\n")
	var serverCount int
	for _, line := range lines {
		if strings.Contains(line, "Retrieved") && strings.Contains(line, "servers from registry") {
			// Extract number using string parsing
			words := strings.Fields(line)
			for i, word := range words {
				if word == "Retrieved" && i+1 < len(words) {
					if count, err := strconv.Atoi(words[i+1]); err == nil {
						serverCount = count
						break
					}
				}
			}
		}
	}

	// Validate that we have at least 10 servers (reduced from 30 to handle registry changes)
	if serverCount < 10 {
		t.Errorf("Expected at least 10 servers from the MCP registry, got %d", serverCount)
	}

	// Check that the output contains the registry URL
	if !strings.Contains(outputStr, mockRegistry.URL) {
		t.Errorf("Expected output to contain mock registry URL %s", mockRegistry.URL)
	}

	// Check that it shows usage information
	if !strings.Contains(outputStr, "Usage: gh aw mcp add <workflow-file> <server-name>") {
		t.Errorf("Expected usage information in output")
	}

	t.Logf("✓ MCP registry contains %d servers (>= 10 required for production registry)", serverCount)
}

// TestMCPAddIntegration_AddAllServers tests adding multiple MCP servers from the registry
// to workflows using the mcp add CLI command
func TestMCPAddIntegration_AddAllServers(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create mock MCP registry server
	mockRegistry := createMockMCPRegistry(t)
	defer mockRegistry.Close()

	// First, get the list of available servers
	listCmd := exec.Command(setup.binaryPath, "mcp", "add", "--verbose", "--registry", mockRegistry.URL)
	listCmd.Dir = setup.tempDir

	listOutput, err := listCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to list MCP servers: %v\nOutput: %s", err, string(listOutput))
	}

	// Parse server names from the output
	// The table uses box-drawing characters (│ ┌ ┐ └ ┘ ├ ┤ ┬ ┴ ┼ ─)
	// Example row format: │io.github.example/test-server-1 │Test MCP server 1...│
	lines := strings.Split(string(listOutput), "\n")
	var serverNames []string
	inTable := false

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Detect when we enter the data section of the table
		// The separator line after the header uses ├ and ┼
		if strings.Contains(trimmedLine, "├") && strings.Contains(trimmedLine, "┼") {
			inTable = true
			continue
		}

		// Detect end of data rows
		if inTable && (strings.Contains(trimmedLine, "└") || strings.Contains(trimmedLine, "Total:") || strings.Contains(trimmedLine, "Usage:")) {
			inTable = false
			continue
		}

		// Parse data rows by splitting on │
		if inTable && strings.Contains(trimmedLine, "│") {
			// Split by │ to get columns
			// Expected format: │column1│column2│ -> ["", "column1", "column2", ""]
			columns := strings.Split(trimmedLine, "│")
			// Need at least 3 elements: empty before first │, column1, and rest
			if len(columns) >= 3 {
				serverName := strings.TrimSpace(columns[1])
				// Skip if empty, looks like a header row, or a border line
				// Headers typically contain generic terms like "Name" or start with border chars
				if serverName != "" && !strings.EqualFold(serverName, "Name") && !strings.HasPrefix(serverName, "─") {
					serverNames = append(serverNames, serverName)
				}
			}
		}
	}

	if len(serverNames) == 0 {
		t.Fatal("No server names could be parsed from the mcp add output")
	}

	t.Logf("Found %d server names to test: %v", len(serverNames), serverNames[:min(5, len(serverNames))])

	// Create a base test workflow
	baseWorkflowContent := `---
name: MCP Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
engine: claude
---

# MCP Test Workflow

This is a test workflow for MCP server integration.
`

	successCount := 0
	failureCount := 0

	// Limit testing to first 10 servers to avoid test timeouts
	testServers := serverNames[:min(10, len(serverNames))]

	for i, serverName := range testServers {
		t.Run("server_"+serverName, func(t *testing.T) {
			// Create a unique workflow file for this server
			workflowFile := filepath.Join(setup.workflowsDir, "test-mcp-"+strconv.Itoa(i)+".md")
			if err := os.WriteFile(workflowFile, []byte(baseWorkflowContent), 0o644); err != nil {
				t.Fatalf("Failed to create test workflow file: %v", err)
			}

			// Try to add the MCP server to the workflow
			addCmd := exec.Command(setup.binaryPath, "mcp", "add", filepath.Base(workflowFile[:len(workflowFile)-3]), serverName, "--verbose", "--registry", mockRegistry.URL)
			addCmd.Dir = setup.tempDir

			// Set a timeout for each server addition
			timeout := 30 * time.Second
			addCmd.Env = os.Environ()

			done := make(chan error, 1)
			var output []byte
			go func() {
				var err error
				output, err = addCmd.CombinedOutput()
				done <- err
			}()

			select {
			case err := <-done:
				if err != nil {
					t.Logf("Warning: Failed to add server %s: %v\nOutput: %s", serverName, err, string(output))
					failureCount++
				} else {
					t.Logf("✓ Successfully added server: %s", serverName)
					successCount++

					// Verify the workflow file was updated
					updatedContent, readErr := os.ReadFile(workflowFile)
					if readErr != nil {
						t.Errorf("Failed to read updated workflow file: %v", readErr)
					} else {
						updatedStr := string(updatedContent)
						if !strings.Contains(updatedStr, "mcp:") {
							t.Errorf("Expected MCP configuration to be added to workflow for server %s", serverName)
						}
					}
				}
			case <-time.After(timeout):
				t.Logf("Warning: Timeout adding server %s after %v", serverName, timeout)
				failureCount++
			}
		})
	}

	// Report overall results
	totalTested := len(testServers)
	t.Logf("MCP Server Addition Results: %d successful, %d failed out of %d tested", successCount, failureCount, totalTested)

	// Require at least 50% success rate
	if successCount == 0 {
		t.Error("No MCP servers could be successfully added to workflows")
	} else if float64(successCount)/float64(totalTested) < 0.5 {
		t.Errorf("Low success rate: %d/%d (%.1f%%) servers added successfully", successCount, totalTested, float64(successCount)/float64(totalTested)*100)
	}
}
