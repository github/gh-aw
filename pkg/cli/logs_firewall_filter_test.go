//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestParseAwInfo_FirewallField verifies that the firewall field is correctly parsed from aw_info.json
func TestParseAwInfo_FirewallField(t *testing.T) {
	tests := []struct {
		name             string
		jsonContent      string
		expectedFirewall string
		description      string
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
			expectedFirewall: "squid",
			description:      "Should detect firewall enabled when steps.firewall is 'squid'",
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
			expectedFirewall: "",
			description:      "Should detect firewall disabled when steps.firewall is empty string",
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
			expectedFirewall: "",
			description:      "Should handle missing steps field (backward compatibility)",
		},
		{
			name: "steps field without firewall",
			jsonContent: `{
				"engine_id": "copilot",
				"engine_name": "Copilot",
				"model": "gpt-4",
				"version": "1.0",
				"workflow_name": "test-workflow",
				"staged": false,
				"steps": {},
				"created_at": "2025-01-27T15:00:00Z"
			}`,
			expectedFirewall: "",
			description:      "Should handle steps field without firewall subfield",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file with the JSON content
			tempDir := testutil.TempDir(t, "test-*")
			awInfoPath := filepath.Join(tempDir, "aw_info.json")

			err := os.WriteFile(awInfoPath, []byte(tt.jsonContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Parse the aw_info.json file
			info, err := parseAwInfo(awInfoPath, false)
			if err != nil {
				t.Fatalf("Failed to parse aw_info.json: %v", err)
			}

			// Check the firewall field
			if info.Steps.Firewall != tt.expectedFirewall {
				t.Errorf("%s\nExpected firewall: '%s', got: '%s'",
					tt.description, tt.expectedFirewall, info.Steps.Firewall)
			}

			t.Logf("✓ %s", tt.description)
		})
	}
}

// TestFirewallFilterLogic verifies the filtering logic for firewall parameter
func TestFirewallFilterLogic(t *testing.T) {
	tests := []struct {
		name            string
		firewallInJSON  string
		filterValue     string
		shouldBeSkipped bool
		description     string
	}{
		{
			name:            "filter=true, has firewall - should NOT skip",
			firewallInJSON:  "squid",
			filterValue:     "true",
			shouldBeSkipped: false,
			description:     "Run with firewall should pass when filter='true'",
		},
		{
			name:            "filter=true, no firewall - should skip",
			firewallInJSON:  "",
			filterValue:     "true",
			shouldBeSkipped: true,
			description:     "Run without firewall should be skipped when filter='true'",
		},
		{
			name:            "filter=false, has firewall - should skip",
			firewallInJSON:  "squid",
			filterValue:     "false",
			shouldBeSkipped: true,
			description:     "Run with firewall should be skipped when filter='false'",
		},
		{
			name:            "filter=false, no firewall - should NOT skip",
			firewallInJSON:  "",
			filterValue:     "false",
			shouldBeSkipped: false,
			description:     "Run without firewall should pass when filter='false'",
		},
		{
			name:            "filter empty, has firewall - should NOT skip",
			firewallInJSON:  "squid",
			filterValue:     "",
			shouldBeSkipped: false,
			description:     "Run with firewall should pass when no filter specified",
		},
		{
			name:            "filter empty, no firewall - should NOT skip",
			firewallInJSON:  "",
			filterValue:     "",
			shouldBeSkipped: false,
			description:     "Run without firewall should pass when no filter specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the filtering logic from DownloadWorkflowLogs
			var hasFirewall bool
			if tt.firewallInJSON != "" {
				hasFirewall = true
			}

			var shouldSkip bool
			if tt.filterValue != "" {
				filterRequiresFirewall := tt.filterValue == "true"
				if filterRequiresFirewall && !hasFirewall {
					shouldSkip = true
				}
				if !filterRequiresFirewall && hasFirewall {
					shouldSkip = true
				}
			}

			if shouldSkip != tt.shouldBeSkipped {
				t.Errorf("%s\nExpected shouldSkip=%v, got shouldSkip=%v",
					tt.description, tt.shouldBeSkipped, shouldSkip)
			}

			t.Logf("✓ %s (shouldSkip=%v)", tt.description, shouldSkip)
		})
	}
}

// TestAwInfoWithFirewallMarshaling verifies that AwInfo with firewall field marshals correctly
func TestAwInfoWithFirewallMarshaling(t *testing.T) {
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

	// Verify that the JSON contains the steps.firewall field
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

	t.Log("✓ AwInfo with firewall field marshals correctly")
}
