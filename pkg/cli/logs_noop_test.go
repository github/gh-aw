//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/constants"
)

// TestExtractNoopsFromRun tests extracting noop messages from safe output artifact files
func TestExtractNoopsFromRun(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := testutil.TempDir(t, "test-*")

	testRun := WorkflowRun{
		DatabaseID:   67890,
		WorkflowName: "Integration Test",
	}

	tests := []struct {
		name              string
		safeOutputContent string
		expected          int
		expectMessage     string
	}{
		{
			name: "single_noop_in_safe_output",
			safeOutputContent: `{
				"items": [
					{
						"type": "noop",
						"message": "This is a test noop message",
						"timestamp": "2024-01-01T12:00:00Z"
					}
				],
				"errors": []
			}`,
			expected:      1,
			expectMessage: "This is a test noop message",
		},
		{
			name: "multiple_noops_in_safe_output",
			safeOutputContent: `{
				"items": [
					{
						"type": "noop",
						"message": "First noop message",
						"timestamp": "2024-01-01T10:00:00Z"
					},
					{
						"type": "noop",
						"message": "Second noop message",
						"timestamp": "2024-01-01T10:01:00Z"
					},
					{
						"type": "create-issue",
						"title": "Test Issue",
						"body": "This should be ignored"
					}
				],
				"errors": []
			}`,
			expected:      2,
			expectMessage: "First noop message",
		},
		{
			name: "no_noops_in_safe_output",
			safeOutputContent: `{
				"items": [
					{
						"type": "create-issue",
						"title": "Test Issue",
						"body": "No noops here"
					}
				],
				"errors": []
			}`,
			expected: 0,
		},
		{
			name: "empty_safe_output",
			safeOutputContent: `{
				"items": [],
				"errors": []
			}`,
			expected: 0,
		},
		{
			name: "malformed_json",
			safeOutputContent: `{
				"items": [
					{
						"type": "noop"
						"message": "broken"
					}
				]
			}`,
			expected: 0, // Should handle gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the safe output artifact file
			safeOutputFile := filepath.Join(tmpDir, constants.AgentOutputArtifactName)
			err := os.WriteFile(safeOutputFile, []byte(tt.safeOutputContent), 0644)
			if err != nil {
				t.Fatalf("Failed to create test safe output file: %v", err)
			}

			// Extract noops
			noops, err := extractNoopsFromRun(tmpDir, testRun, false)
			if err != nil {
				t.Fatalf("Error extracting noops: %v", err)
			}

			if len(noops) != tt.expected {
				t.Errorf("Expected %d noops, got %d", tt.expected, len(noops))
				return
			}

			if tt.expected > 0 && len(noops) > 0 {
				noop := noops[0]
				if noop.Message != tt.expectMessage {
					t.Errorf("Expected message '%s', got '%s'", tt.expectMessage, noop.Message)
				}

				// Check that run information was populated
				if noop.WorkflowName != testRun.WorkflowName {
					t.Errorf("Expected workflow name '%s', got '%s'", testRun.WorkflowName, noop.WorkflowName)
				}

				if noop.RunID != testRun.DatabaseID {
					t.Errorf("Expected run ID %d, got %d", testRun.DatabaseID, noop.RunID)
				}
			}

			// Clean up for next test
			os.Remove(safeOutputFile)
		})
	}
}

// TestExtractNoopsFlattenedStructure tests extracting noops from the new flattened artifact structure
// where agent_output.json is at root after artifact flattening
func TestExtractNoopsFlattenedStructure(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-flattened-noop")
	err := os.MkdirAll(runDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create agent_output.json at root (new flattened structure)
	agentOutputContent := `{
  "items": [
    {
      "type": "noop",
      "message": "Test noop message from flattened structure",
      "timestamp": "2025-01-05T00:00:00.000Z"
    },
    {
      "type": "noop",
      "message": "Second noop message",
      "timestamp": "2025-01-05T00:01:00.000Z"
    }
  ],
  "errors": []
}`

	// Use the actual filename: agent_output.json (with underscore and .json extension)
	agentOutputPath := filepath.Join(runDir, "agent_output.json")
	err = os.WriteFile(agentOutputPath, []byte(agentOutputContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write agent_output.json: %v", err)
	}

	// Create test run
	testRun := WorkflowRun{
		DatabaseID:   88888,
		WorkflowName: "Flattened Noop Test",
	}

	// Extract noops - should find the file at root
	noops, err := extractNoopsFromRun(runDir, testRun, false)
	if err != nil {
		t.Fatalf("Error extracting noops from flattened structure: %v", err)
	}

	// Verify results
	if len(noops) != 2 {
		t.Errorf("Expected 2 noops from flattened structure, got %d", len(noops))
		return
	}

	expectedMessages := []string{
		"Test noop message from flattened structure",
		"Second noop message",
	}

	for i, noop := range noops {
		if noop.Message != expectedMessages[i] {
			t.Errorf("Expected message '%s', got '%s'", expectedMessages[i], noop.Message)
		}

		if noop.WorkflowName != testRun.WorkflowName {
			t.Errorf("Expected workflow name '%s', got '%s'", testRun.WorkflowName, noop.WorkflowName)
		}

		if noop.RunID != testRun.DatabaseID {
			t.Errorf("Expected run ID %d, got %d", testRun.DatabaseID, noop.RunID)
		}
	}
}
