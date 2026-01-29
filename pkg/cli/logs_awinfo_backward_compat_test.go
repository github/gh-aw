//go:build !integration

package cli

import (
	"encoding/json"
	"testing"
)

// TestAwInfoBackwardCompatibility verifies that AwInfo can parse both old and new field names
func TestAwInfoBackwardCompatibility(t *testing.T) {
	tests := []struct {
		name                    string
		jsonData                string
		expectedFirewallVersion string
		expectedCLIVersion      string
		description             string
	}{
		{
			name: "new field name awf_version",
			jsonData: `{
				"engine_id": "copilot",
				"engine_name": "GitHub Copilot",
				"cli_version": "1.0.0",
				"awf_version": "v0.7.0",
				"workflow_name": "test"
			}`,
			expectedFirewallVersion: "v0.7.0",
			expectedCLIVersion:      "1.0.0",
			description:             "Should parse new awf_version field",
		},
		{
			name: "old field name firewall_version",
			jsonData: `{
				"engine_id": "copilot",
				"engine_name": "GitHub Copilot",
				"firewall_version": "v0.6.0",
				"workflow_name": "test"
			}`,
			expectedFirewallVersion: "v0.6.0",
			expectedCLIVersion:      "",
			description:             "Should parse old firewall_version field for backward compatibility",
		},
		{
			name: "both field names present - prefer new",
			jsonData: `{
				"engine_id": "copilot",
				"engine_name": "GitHub Copilot",
				"cli_version": "1.0.0",
				"awf_version": "v0.7.0",
				"firewall_version": "v0.6.0",
				"workflow_name": "test"
			}`,
			expectedFirewallVersion: "v0.7.0",
			expectedCLIVersion:      "1.0.0",
			description:             "Should prefer awf_version when both are present",
		},
		{
			name: "no firewall version fields",
			jsonData: `{
				"engine_id": "copilot",
				"engine_name": "GitHub Copilot",
				"cli_version": "1.0.0",
				"workflow_name": "test"
			}`,
			expectedFirewallVersion: "",
			expectedCLIVersion:      "1.0.0",
			description:             "Should handle missing firewall version gracefully",
		},
		{
			name: "empty firewall version",
			jsonData: `{
				"engine_id": "copilot",
				"engine_name": "GitHub Copilot",
				"cli_version": "1.0.0",
				"awf_version": "",
				"workflow_name": "test"
			}`,
			expectedFirewallVersion: "",
			expectedCLIVersion:      "1.0.0",
			description:             "Should handle empty awf_version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var info AwInfo
			err := json.Unmarshal([]byte(tt.jsonData), &info)
			if err != nil {
				t.Fatalf("Failed to unmarshal JSON: %v", err)
			}

			// Test GetFirewallVersion method
			actualFirewallVersion := info.GetFirewallVersion()
			if actualFirewallVersion != tt.expectedFirewallVersion {
				t.Errorf("%s: GetFirewallVersion() = %q, want %q", tt.description, actualFirewallVersion, tt.expectedFirewallVersion)
			}

			// Test CLIVersion field
			if info.CLIVersion != tt.expectedCLIVersion {
				t.Errorf("%s: CLIVersion = %q, want %q", tt.description, info.CLIVersion, tt.expectedCLIVersion)
			}

			// Verify that the fields were parsed correctly
			if tt.jsonData != "" {
				if info.EngineID != "copilot" {
					t.Errorf("EngineID = %q, want %q", info.EngineID, "copilot")
				}
			}
		})
	}
}

// TestAwInfoMarshaling verifies that AwInfo can be marshaled correctly
func TestAwInfoMarshaling(t *testing.T) {
	tests := []struct {
		name             string
		info             AwInfo
		shouldContainNew bool // Should contain awf_version
		shouldContainOld bool // Should contain firewall_version
		description      string
	}{
		{
			name: "with new field",
			info: AwInfo{
				EngineID:   "copilot",
				EngineName: "GitHub Copilot",
				CLIVersion: "1.0.0",
				AwfVersion: "v0.7.0",
			},
			shouldContainNew: true,
			shouldContainOld: false,
			description:      "Should marshal with awf_version when set",
		},
		{
			name: "with old field",
			info: AwInfo{
				EngineID:        "copilot",
				EngineName:      "GitHub Copilot",
				FirewallVersion: "v0.6.0",
			},
			shouldContainNew: false,
			shouldContainOld: true,
			description:      "Should marshal with firewall_version when set",
		},
		{
			name: "with both fields",
			info: AwInfo{
				EngineID:        "copilot",
				EngineName:      "GitHub Copilot",
				CLIVersion:      "1.0.0",
				AwfVersion:      "v0.7.0",
				FirewallVersion: "v0.6.0",
			},
			shouldContainNew: true,
			shouldContainOld: true,
			description:      "Should marshal both fields when both are set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.info)
			if err != nil {
				t.Fatalf("Failed to marshal AwInfo: %v", err)
			}

			jsonStr := string(data)

			if tt.shouldContainNew {
				// Check for awf_version in JSON
				var temp map[string]interface{}
				json.Unmarshal(data, &temp)
				if _, exists := temp["awf_version"]; !exists {
					t.Errorf("%s: JSON should contain awf_version field, got: %s", tt.description, jsonStr)
				}
			}

			if tt.shouldContainOld {
				// Check for firewall_version in JSON
				var temp map[string]interface{}
				json.Unmarshal(data, &temp)
				if _, exists := temp["firewall_version"]; !exists {
					t.Errorf("%s: JSON should contain firewall_version field, got: %s", tt.description, jsonStr)
				}
			}
		})
	}
}
