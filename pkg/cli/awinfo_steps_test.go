//go:build !integration

package cli

import (
	"encoding/json"
	"testing"
)

func TestAwInfoStepsFieldParsing(t *testing.T) {
	tests := []struct {
		name          string
		jsonContent   string
		expectedSteps AwInfoSteps
		description   string
	}{
		{
			name: "firewall enabled with squid",
			jsonContent: `{
				"engine_id": "copilot",
				"engine_name": "Copilot",
				"model": "gpt-4",
				"version": "1.0",
				"workflow_name": "test-workflow",
				"staged": false,
				"steps": {
					"firewall": "squid"
				},
				"created_at": "2025-01-27T15:00:00Z"
			}`,
			expectedSteps: AwInfoSteps{
				Firewall: "squid",
			},
			description: "Should parse steps.firewall as 'squid' when firewall is enabled",
		},
		{
			name: "firewall disabled (empty string)",
			jsonContent: `{
				"engine_id": "copilot",
				"engine_name": "Copilot",
				"model": "gpt-4",
				"version": "1.0",
				"workflow_name": "test-workflow",
				"staged": false,
				"steps": {
					"firewall": ""
				},
				"created_at": "2025-01-27T15:00:00Z"
			}`,
			expectedSteps: AwInfoSteps{
				Firewall: "",
			},
			description: "Should parse steps.firewall as empty string when firewall is disabled",
		},
		{
			name: "no steps field (backward compatibility)",
			jsonContent: `{
				"engine_id": "claude",
				"engine_name": "Claude",
				"model": "claude-3-sonnet",
				"version": "20240620",
				"workflow_name": "test-workflow",
				"staged": false,
				"created_at": "2025-01-27T15:00:00Z"
			}`,
			expectedSteps: AwInfoSteps{
				Firewall: "",
			},
			description: "Should handle missing steps field (backward compatibility)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var info AwInfo
			err := json.Unmarshal([]byte(tt.jsonContent), &info)
			if err != nil {
				t.Fatalf("Failed to unmarshal JSON: %v", err)
			}

			if info.Steps.Firewall != tt.expectedSteps.Firewall {
				t.Errorf("%s\nExpected firewall: '%s', got: '%s'",
					tt.description, tt.expectedSteps.Firewall, info.Steps.Firewall)
			}

			t.Logf("✓ %s", tt.description)
		})
	}
}

func TestAwInfoStepsMarshaling(t *testing.T) {
	info := AwInfo{
		EngineID:     "copilot",
		EngineName:   "Copilot",
		Model:        "gpt-4",
		Version:      "1.0",
		WorkflowName: "test-workflow",
		Staged:       false,
		Steps: AwInfoSteps{
			Firewall: "squid",
		},
		CreatedAt: "2025-01-27T15:00:00Z",
	}

	jsonData, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Failed to marshal AwInfo: %v", err)
	}

	// Verify that the JSON contains the steps field
	var result map[string]any
	err = json.Unmarshal(jsonData, &result)
	if err != nil {
		t.Fatalf("Failed to unmarshal marshaled JSON: %v", err)
	}

	steps, ok := result["steps"].(map[string]any)
	if !ok {
		t.Fatal("Expected 'steps' field in marshaled JSON")
	}

	firewall, ok := steps["firewall"].(string)
	if !ok {
		t.Fatal("Expected 'firewall' field in steps object")
	}

	if firewall != "squid" {
		t.Errorf("Expected firewall to be 'squid', got: '%s'", firewall)
	}

	t.Log("✓ AwInfo marshals correctly with steps field")
}
