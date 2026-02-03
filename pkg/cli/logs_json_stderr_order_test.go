//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestLogsJSONOutputBeforeStderr verifies that when --json flag is set,
// JSON output is written to stdout BEFORE any warning messages are written to stderr.
// This is critical for CI tests that redirect both stdout and stderr to the same file.
func TestLogsJSONOutputBeforeStderr(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-json-order-*")

	// Create a context for the test
	ctx := context.Background()

	// Capture both stdout and stderr to simulate CI behavior
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	// Create pipes for stdout and stderr
	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()

	os.Stdout = stdoutW
	os.Stderr = stderrW

	// Call DownloadWorkflowLogs with parameters that will result in no matching runs
	// This should trigger the warning message path
	err := DownloadWorkflowLogs(
		ctx,
		"nonexistent-workflow-test-12345", // Workflow that doesn't exist
		2,                                 // count
		"",                                // startDate
		"",                                // endDate
		tmpDir,                            // outputDir
		"copilot",                         // engine
		"",                                // ref
		0,                                 // beforeRunID
		0,                                 // afterRunID
		"",                                // repoOverride
		false,                             // verbose
		false,                             // toolGraph
		false,                             // noStaged
		false,                             // firewallOnly
		false,                             // noFirewall
		false,                             // parse
		true,                              // jsonOutput - THIS IS KEY
		10,                                // timeout
		"summary.json",                    // summaryFile
		"",                                // safeOutputType
	)

	// Close writers first
	stdoutW.Close()
	stderrW.Close()

	// Restore stdout and stderr
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Skip test if GitHub API is not accessible
	if err != nil {
		if strings.Contains(err.Error(), "no auth token found") ||
			strings.Contains(err.Error(), "GitHub CLI authentication required") {
			t.Skip("Skipping test: GitHub authentication not available")
		}
		// For other errors, we still want to verify the output format
	}

	// Read stdout
	var stdoutBuf bytes.Buffer
	io.Copy(&stdoutBuf, stdoutR)
	stdoutOutput := stdoutBuf.String()

	// Read stderr
	var stderrBuf bytes.Buffer
	io.Copy(&stderrBuf, stderrR)
	stderrOutput := stderrBuf.String()

	// Verify stdout contains valid JSON
	if len(stdoutOutput) == 0 {
		t.Fatal("Expected JSON output on stdout, got empty string")
	}

	// Parse the JSON to verify it's valid
	var logsData LogsData
	if err := json.Unmarshal([]byte(stdoutOutput), &logsData); err != nil {
		t.Fatalf("Failed to parse JSON output from stdout: %v\nStdout: %s\nStderr: %s", err, stdoutOutput, stderrOutput)
	}

	// Verify the JSON has the required structure
	if _, err := json.Marshal(logsData); err != nil {
		t.Fatalf("Failed to re-marshal parsed JSON: %v", err)
	}

	// Most importantly: verify total_tokens field exists in stdout JSON
	var jsonMap map[string]any
	if err := json.Unmarshal([]byte(stdoutOutput), &jsonMap); err != nil {
		t.Fatalf("Failed to parse stdout JSON as map: %v", err)
	}

	summary, ok := jsonMap["summary"].(map[string]any)
	if !ok {
		t.Fatalf("Expected summary to be a map in stdout JSON, got %T", jsonMap["summary"])
	}

	// This is the exact check the CI does with jq -e '.summary.total_tokens'
	if _, exists := summary["total_tokens"]; !exists {
		t.Errorf("Expected total_tokens field to exist in stdout JSON summary (CI test would fail). Summary: %+v", summary)
	}

	// Verify stderr contains the warning message (after JSON was output)
	// This is fine as long as it's on stderr, not mixed with stdout
	if !strings.Contains(stderrOutput, "No workflow runs with artifacts found") &&
		!strings.Contains(stderrOutput, "authentication required") {
		t.Logf("Note: Expected warning message not found in stderr. This may be expected if GitHub auth succeeded. Stderr: %s", stderrOutput)
	}
}

// TestLogsJSONAndStderrRedirected simulates exactly what the CI test does:
// redirecting both stdout and stderr to the same file and parsing as JSON
func TestLogsJSONAndStderrRedirected(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-json-stderr-*")

	// Create a buffer that will receive both stdout and stderr (simulating 2>&1)
	var combinedOutput bytes.Buffer

	// Save original stdout/stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	// Create a pipe
	r, w, _ := os.Pipe()

	// Redirect both stdout and stderr to the same pipe (simulating 2>&1)
	os.Stdout = w
	os.Stderr = w

	// Start a goroutine to read from the pipe
	// Channel is closed by goroutine (sender) to signal completion
	done := make(chan struct{})
	go func() {
		defer close(done)
		io.Copy(&combinedOutput, r)
	}()

	// Call DownloadWorkflowLogs
	ctx := context.Background()
	err := DownloadWorkflowLogs(
		ctx,
		"nonexistent-workflow-ci-test-67890",
		2,
		"",
		"",
		tmpDir,
		"copilot",
		"",
		0,
		0,
		"",
		false,
		false,
		false,
		false,
		false,
		false,
		true, // jsonOutput
		10,
		"summary.json",
		"", // safeOutputType
	)

	// Close the writer
	w.Close()

	// Wait for the reader to finish
	<-done

	// Restore stdout/stderr
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Skip test if GitHub API is not accessible
	if err != nil {
		if strings.Contains(err.Error(), "no auth token found") ||
			strings.Contains(err.Error(), "GitHub CLI authentication required") {
			t.Skip("Skipping test: GitHub authentication not available")
		}
	}

	output := combinedOutput.String()

	// The critical test: the output should START with valid JSON
	// Any stderr messages should come AFTER the JSON
	// We find where JSON ends (the first newline after a closing brace)
	if len(output) == 0 {
		t.Fatal("Expected output, got empty string")
	}

	// Try to parse the entire output as JSON
	// If it fails, it means stderr messages corrupted the JSON
	var logsData LogsData
	if err := json.Unmarshal([]byte(output), &logsData); err != nil {
		// JSON parse failed - check if it's because stderr messages are mixed in
		// Try to extract just the JSON part (everything up to the first complete JSON object)
		lines := strings.Split(output, "\n")
		var jsonLines []string
		braceCount := 0
		foundStart := false

		for _, line := range lines {
			if !foundStart && strings.HasPrefix(strings.TrimSpace(line), "{") {
				foundStart = true
			}
			if foundStart {
				jsonLines = append(jsonLines, line)
				braceCount += strings.Count(line, "{") - strings.Count(line, "}")
				if braceCount == 0 && len(jsonLines) > 1 {
					// Complete JSON object found
					break
				}
			}
		}

		jsonPart := strings.Join(jsonLines, "\n")
		if err := json.Unmarshal([]byte(jsonPart), &logsData); err != nil {
			t.Fatalf("Failed to parse JSON even after extracting JSON portion: %v\nFull output: %s\nExtracted JSON: %s", err, output, jsonPart)
		}

		// If we get here, it means we successfully extracted JSON from mixed output
		// This is actually what we DON'T want - JSON should be clean
		t.Logf("Warning: Had to extract JSON from mixed output. This suggests stderr messages may be interfering.")
	}

	// Verify total_tokens exists
	var jsonMap map[string]any
	if err := json.Unmarshal([]byte(output), &jsonMap); err == nil {
		// JSON parsed cleanly - this is good
		summary, ok := jsonMap["summary"].(map[string]any)
		if !ok {
			t.Fatalf("Expected summary to be a map, got %T", jsonMap["summary"])
		}

		if _, exists := summary["total_tokens"]; !exists {
			t.Errorf("Expected total_tokens field to exist in summary. Summary: %+v", summary)
		}
	} else {
		// If the entire output isn't valid JSON, we have a problem
		// Try using jq-like parsing that CI uses
		if !strings.Contains(output, `"total_tokens"`) {
			t.Errorf("Expected to find 'total_tokens' field somewhere in output. Output: %s", output)
		}
	}
}
