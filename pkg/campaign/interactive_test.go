//go:build !integration

package campaign

import (
	"os"
	"testing"
)

func TestRunInteractiveCampaignCreation_SkipInAutomation(t *testing.T) {
	// Set GO_TEST_MODE to simulate test environment
	os.Setenv("GO_TEST_MODE", "true")
	defer os.Unsetenv("GO_TEST_MODE")

	tmpDir := t.TempDir()

	err := RunInteractiveCampaignCreation(tmpDir, false, false)
	if err == nil {
		t.Error("Expected error when running interactive mode in test environment")
	}

	expectedMsg := "interactive mode cannot be used in automated tests or CI environments"
	if !containsString(err.Error(), expectedMsg) {
		t.Errorf("Expected error containing %q, got: %v", expectedMsg, err)
	}
}

func TestFormatCampaignName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"security-q1-2025", "Security Q1 2025"},
		{"my-test-campaign", "My Test Campaign"},
		{"single", "Single"},
		{"", ""},
		{"already-capitalized", "Already Capitalized"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := formatCampaignName(tt.input)
			if result != tt.expected {
				t.Errorf("formatCampaignName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || (len(s) >= len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
