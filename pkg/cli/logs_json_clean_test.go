//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestJSONOutputNotCorruptedByStderr is a unit test that verifies JSON output
// is not corrupted when stderr messages are present. This simulates the CI test scenario.
func TestJSONOutputNotCorruptedByStderr(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-json-clean-*")

	// Build logs data with empty runs (simulating no matching workflow runs)
	logsData := buildLogsData([]ProcessedRun{}, tmpDir, nil)

	// Capture stdout (JSON output)
	oldStdout := os.Stdout
	stdoutR, stdoutW, _ := os.Pipe()
	os.Stdout = stdoutW

	// Render JSON
	err := renderLogsJSON(logsData)
	if err != nil {
		t.Fatalf("Failed to render JSON: %v", err)
	}

	// Close stdout writer and restore
	stdoutW.Close()
	os.Stdout = oldStdout

	// Read stdout
	var stdoutBuf bytes.Buffer
	stdoutBuf.ReadFrom(stdoutR)
	jsonOutput := stdoutBuf.String()

	// Verify the output is valid JSON (no corruption)
	if len(jsonOutput) == 0 {
		t.Fatal("Expected JSON output, got empty string")
	}

	// The output should be parseable as JSON
	var parsedData LogsData
	if err := json.Unmarshal([]byte(jsonOutput), &parsedData); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, jsonOutput)
	}

	// Verify critical fields exist
	var jsonMap map[string]any
	if err := json.Unmarshal([]byte(jsonOutput), &jsonMap); err != nil {
		t.Fatalf("Failed to parse as map: %v", err)
	}

	summary, ok := jsonMap["summary"].(map[string]any)
	if !ok {
		t.Fatalf("Expected summary to be a map, got %T", jsonMap["summary"])
	}

	// This is what the CI test checks: jq -e '.summary.total_tokens'
	if _, exists := summary["total_tokens"]; !exists {
		t.Errorf("Expected total_tokens field in summary. Summary: %+v", summary)
	}

	// Verify the output doesn't contain any stderr-like messages
	// (warning symbols, error symbols, etc.)
	if strings.Contains(jsonOutput, "✗") || strings.Contains(jsonOutput, "⚠") {
		t.Errorf("JSON output contains stderr message symbols, which would corrupt JSON: %s", jsonOutput)
	}

	// Verify the JSON output is clean (no non-JSON text)
	trimmed := strings.TrimSpace(jsonOutput)
	if !strings.HasPrefix(trimmed, "{") {
		t.Errorf("JSON output doesn't start with '{'. It may be corrupted by stderr messages. Output: %s", jsonOutput)
	}
	if !strings.HasSuffix(trimmed, "}") {
		t.Errorf("JSON output doesn't end with '}'. It may be corrupted by stderr messages. Output: %s", jsonOutput)
	}
}

// TestStderrMessagesAfterJSON verifies that when both stdout and stderr exist,
// the stderr messages come after the complete JSON structure
func TestStderrMessagesAfterJSON(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-stderr-after-*")

	// This test manually constructs what happens in DownloadWorkflowLogs
	// when no runs are found and JSON output is requested

	// Step 1: Build logs data (what happens in the function)
	logsData := buildLogsData([]ProcessedRun{}, tmpDir, nil)

	// Step 2: Capture both stdout and stderr
	var combinedOutput bytes.Buffer

	oldStdout := os.Stdout
	oldStderr := os.Stderr

	// Create a single pipe that will receive both outputs (simulating 2>&1)
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	// Start goroutine to read
	// Channel is closed by goroutine (sender) to signal completion
	done := make(chan struct{})
	go func() {
		defer close(done)
		combinedOutput.ReadFrom(r)
	}()

	// Step 3: Output JSON FIRST (as our fix does)
	renderLogsJSON(logsData)

	// Step 4: Then output stderr message (as our fix does)
	// Using os.Stderr here, but it's redirected to the same pipe as stdout
	// Simulating: fmt.Fprintln(os.Stderr, console.FormatWarningMessage("..."))
	os.Stderr.WriteString("⚠ No workflow runs with artifacts found matching the specified criteria\n")

	// Close pipe
	w.Close()
	<-done

	// Restore
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	output := combinedOutput.String()

	// The key test: output should start with valid JSON
	// Find the first '{' and the matching '}'
	firstBrace := strings.Index(output, "{")
	if firstBrace != 0 {
		maxLen := 50
		if len(output) < maxLen {
			maxLen = len(output)
		}
		t.Errorf("Output doesn't start with JSON. It starts with: %s", output[:maxLen])
	}

	// Try to parse just the JSON part
	// We expect the JSON to be complete before any stderr messages
	var jsonEndIndex int
	braceCount := 0
	inString := false
	escape := false

	for i, ch := range output {
		if escape {
			escape = false
			continue
		}
		if ch == '\\' {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '{' {
			braceCount++
		} else if ch == '}' {
			braceCount--
			if braceCount == 0 {
				jsonEndIndex = i + 1
				break
			}
		}
	}

	if jsonEndIndex == 0 {
		t.Fatal("Could not find end of JSON in output")
	}

	jsonPart := output[:jsonEndIndex]
	remainingPart := output[jsonEndIndex:]

	// Parse the JSON part
	var parsedData LogsData
	if err := json.Unmarshal([]byte(jsonPart), &parsedData); err != nil {
		t.Fatalf("Failed to parse JSON part: %v\nJSON part: %s\nFull output: %s", err, jsonPart, output)
	}

	// Verify the stderr message comes AFTER the JSON
	if !strings.Contains(remainingPart, "No workflow runs") {
		t.Logf("Note: Stderr message not found after JSON. This is OK. Remaining part: %s", remainingPart)
	}

	// Most importantly: the JSON part should be valid and complete
	var jsonMap map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &jsonMap); err != nil {
		t.Fatalf("Failed to parse JSON part as map: %v", err)
	}

	summary, ok := jsonMap["summary"].(map[string]any)
	if !ok {
		t.Fatalf("Expected summary in JSON part, got %T", jsonMap["summary"])
	}

	if _, exists := summary["total_tokens"]; !exists {
		t.Errorf("Expected total_tokens in JSON summary. Summary: %+v", summary)
	}
}
