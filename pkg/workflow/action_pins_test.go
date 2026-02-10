//go:build !integration

package workflow

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestActionPinsExist verifies that all action pinning entries exist
func TestActionPinsExist(t *testing.T) {
	// Read action pins from JSON file instead of hardcoded list
	actionPins := getActionPins()

	// Verify we have at least some pins loaded
	if len(actionPins) == 0 {
		t.Fatal("No action pins loaded from JSON file")
	}

	// Verify each pin has required fields
	for _, pin := range actionPins {
		// Verify the pin has a repo
		if pin.Repo == "" {
			t.Errorf("Action pin has empty Repo field")
			continue
		}

		// Verify the pin has a valid SHA (40 character hex string)
		if !isValidSHA(pin.SHA) {
			t.Errorf("Invalid SHA for %s: %s (expected 40-character hex string)", pin.Repo, pin.SHA)
		}

		// Verify the pin has a version
		if pin.Version == "" {
			t.Errorf("Missing version for %s", pin.Repo)
		}
	}
}

// TestGetActionPinReturnsValidSHA tests that GetActionPin returns valid SHA references
func TestGetActionPinReturnsValidSHA(t *testing.T) {
	// Generate test cases dynamically from action pins JSON
	actionPins := getActionPins()

	if len(actionPins) == 0 {
		t.Fatal("No action pins loaded from JSON file")
	}

	for _, pin := range actionPins {
		t.Run(pin.Repo, func(t *testing.T) {
			result := GetActionPin(pin.Repo)

			// Check that the result contains a SHA (40-char hex after @ and before #)
			// Format is: repo@sha # version
			parts := strings.Split(result, "@")
			if len(parts) != 2 {
				t.Errorf("GetActionPin(%s) = %s, expected format repo@sha # version", pin.Repo, result)
				return
			}

			// Extract SHA (before the comment marker " # ")
			shaAndComment := parts[1]
			commentIdx := strings.Index(shaAndComment, " # ")
			if commentIdx == -1 {
				t.Errorf("GetActionPin(%s) = %s, expected comment with version tag", pin.Repo, result)
				return
			}

			sha := shaAndComment[:commentIdx]

			// All action pins should have valid SHAs
			if !isValidSHA(sha) {
				t.Errorf("GetActionPin(%s) = %s, expected SHA to be 40-char hex", pin.Repo, result)
			}
		})
	}
}

// TestGetActionPinFallback tests that GetActionPin returns empty string for unknown actions
func TestGetActionPinFallback(t *testing.T) {
	result := GetActionPin("unknown/action")
	expected := ""
	if result != expected {
		t.Errorf("GetActionPin(unknown/action) = %s, want %s (empty string)", result, expected)
	}
}

// isValidSHA checks if a string is a valid 40-character hexadecimal SHA
func isValidSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	matched, _ := regexp.MatchString("^[0-9a-f]{40}$", s)
	return matched
}

// TestExtractActionRepo tests the extractActionRepo function
func TestExtractActionRepo(t *testing.T) {
	tests := []struct {
		name     string
		uses     string
		expected string
	}{
		{
			name:     "action with version tag",
			uses:     "actions/checkout@v4",
			expected: "actions/checkout",
		},
		{
			name:     "action with SHA",
			uses:     "actions/setup-node@93cb6efe18208431cddfb8368fd83d5badbf9bfd",
			expected: "actions/setup-node",
		},
		{
			name:     "action with subpath and version",
			uses:     "github/codeql-action/upload-sarif@v3",
			expected: "github/codeql-action/upload-sarif",
		},
		{
			name:     "action without version",
			uses:     "actions/checkout",
			expected: "actions/checkout",
		},
		{
			name:     "action with branch ref",
			uses:     "actions/setup-python@main",
			expected: "actions/setup-python",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractActionRepo(tt.uses)
			if result != tt.expected {
				t.Errorf("extractActionRepo(%q) = %q, want %q", tt.uses, result, tt.expected)
			}
		})
	}
}

// TestExtractActionVersion tests the extractActionVersion function
func TestExtractActionVersion(t *testing.T) {
	tests := []struct {
		name     string
		uses     string
		expected string
	}{
		{
			name:     "action with version tag",
			uses:     "actions/checkout@v4",
			expected: "v4",
		},
		{
			name:     "action with SHA",
			uses:     "actions/setup-node@93cb6efe18208431cddfb8368fd83d5badbf9bfd",
			expected: "93cb6efe18208431cddfb8368fd83d5badbf9bfd",
		},
		{
			name:     "action with subpath and version",
			uses:     "github/codeql-action/upload-sarif@v3",
			expected: "v3",
		},
		{
			name:     "action without version",
			uses:     "actions/checkout",
			expected: "",
		},
		{
			name:     "action with branch ref",
			uses:     "actions/setup-python@main",
			expected: "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractActionVersion(tt.uses)
			if result != tt.expected {
				t.Errorf("extractActionVersion(%q) = %q, want %q", tt.uses, result, tt.expected)
			}
		})
	}
}

// TestApplyActionPinToStep tests the ApplyActionPinToStep function
func TestApplyActionPinToStep(t *testing.T) {
	tests := []struct {
		name         string
		stepMap      map[string]any
		expectPinned bool
		expectedUses string
	}{
		{
			name: "step with pinned action (checkout)",
			stepMap: map[string]any{
				"name": "Checkout code",
				"uses": "actions/checkout@v5",
			},
			expectPinned: true,
			expectedUses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v5",
		},
		{
			name: "step with pinned action (setup-node)",
			stepMap: map[string]any{
				"name": "Setup Node",
				"uses": "actions/setup-node@v6",
				"with": map[string]any{
					"node-version": "20",
				},
			},
			expectPinned: true,
			expectedUses: "actions/setup-node@6044e13b5dc448c55e2357c09f80417699197238 # v6",
		},
		{
			name: "step with unpinned action",
			stepMap: map[string]any{
				"name": "Custom action",
				"uses": "my-org/my-action@v1",
			},
			expectPinned: false,
			expectedUses: "my-org/my-action@v1",
		},
		{
			name: "step without uses field",
			stepMap: map[string]any{
				"name": "Run command",
				"run":  "echo hello",
			},
			expectPinned: false,
			expectedUses: "",
		},
		{
			name: "step with already pinned SHA",
			stepMap: map[string]any{
				"name": "Checkout",
				"uses": "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd",
			},
			expectPinned: true,
			expectedUses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v5.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal WorkflowData for testing
			data := &WorkflowData{}

			// Convert to typed step
			typedStep, err := MapToStep(tt.stepMap)
			if err != nil {
				t.Fatalf("Failed to convert step to typed step: %v", err)
			}

			// Apply action pinning using typed version
			pinnedStep := ApplyActionPinToTypedStep(typedStep, data)
			if pinnedStep == nil {
				t.Fatal("ApplyActionPinToTypedStep returned nil")
			}

			// Convert back to map for comparison
			result := pinnedStep.ToMap()

			// Check if uses field exists in result
			if uses, hasUses := result["uses"]; hasUses {
				usesStr, ok := uses.(string)
				if !ok {
					t.Errorf("ApplyActionPinToTypedStep returned non-string uses field")
					return
				}

				if usesStr != tt.expectedUses {
					t.Errorf("ApplyActionPinToTypedStep uses = %q, want %q", usesStr, tt.expectedUses)
				}

				// Verify other fields are preserved (check length and keys)
				if len(result) != len(tt.stepMap) {
					t.Errorf("ApplyActionPinToTypedStep changed number of fields: got %d, want %d", len(result), len(tt.stepMap))
				}
				for k := range tt.stepMap {
					if _, exists := result[k]; !exists {
						t.Errorf("ApplyActionPinToTypedStep lost field %q", k)
					}
				}
			} else if tt.expectedUses != "" {
				t.Errorf("ApplyActionPinToTypedStep removed uses field when it should be %q", tt.expectedUses)
			}
		})
	}
}

// TestGetActionPinsSorting tests that getActionPins returns sorted action pins
func TestGetActionPinsSorting(t *testing.T) {
	pins := getActionPins()

	// Verify we got all the pins (38 as of February 2026)
	if len(pins) != 38 {
		t.Errorf("getActionPins() returned %d pins, expected 38", len(pins))
	}

	// Verify they are sorted by version (descending) then by repository name (ascending)
	for i := 0; i < len(pins)-1; i++ {
		if pins[i].Version < pins[i+1].Version {
			t.Errorf("Pins not sorted correctly by version: %s (v%s) should come before %s (v%s)",
				pins[i].Repo, pins[i].Version, pins[i+1].Repo, pins[i+1].Version)
		} else if pins[i].Version == pins[i+1].Version && pins[i].Repo > pins[i+1].Repo {
			t.Errorf("Pins not sorted correctly by repo name within same version: %s should come before %s",
				pins[i].Repo, pins[i+1].Repo)
		}
	}

	// Verify all pins have the required fields
	for _, pin := range pins {
		if pin.Repo == "" {
			t.Error("Found pin with empty Repo field")
		}
		if pin.Version == "" {
			t.Errorf("Pin %s has empty Version field", pin.Repo)
		}
		if !isValidSHA(pin.SHA) {
			t.Errorf("Pin %s has invalid SHA: %s", pin.Repo, pin.SHA)
		}
	}
}

// TestGetActionPinByRepo tests the GetActionPinByRepo function
func TestGetActionPinByRepo(t *testing.T) {
	tests := []struct {
		repo         string
		expectExists bool
		expectRepo   string
		expectVer    string
	}{
		{
			repo:         "actions/checkout",
			expectExists: true,
			expectRepo:   "actions/checkout",
			expectVer:    "v6.0.2",
		},
		{
			repo:         "actions/setup-node",
			expectExists: true,
			expectRepo:   "actions/setup-node",
			expectVer:    "v6.2.0",
		},
		{
			repo:         "unknown/action",
			expectExists: false,
		},
		{
			repo:         "",
			expectExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			pin, exists := GetActionPinByRepo(tt.repo)

			if exists != tt.expectExists {
				t.Errorf("GetActionPinByRepo(%s) exists = %v, want %v", tt.repo, exists, tt.expectExists)
			}

			if tt.expectExists {
				if pin.Repo != tt.expectRepo {
					t.Errorf("GetActionPinByRepo(%s) repo = %s, want %s", tt.repo, pin.Repo, tt.expectRepo)
				}
				if pin.Version != tt.expectVer {
					t.Errorf("GetActionPinByRepo(%s) version = %s, want %s", tt.repo, pin.Version, tt.expectVer)
				}
				if !isValidSHA(pin.SHA) {
					t.Errorf("GetActionPinByRepo(%s) has invalid SHA: %s", tt.repo, pin.SHA)
				}
			}
		})
	}
}

// TestApplyActionPinToTypedStep tests the ApplyActionPinToTypedStep function with typed steps
func TestApplyActionPinToTypedStep(t *testing.T) {
	tests := []struct {
		name         string
		step         *WorkflowStep
		expectPinned bool
		expectedUses string
	}{
		{
			name: "step with pinned action (checkout)",
			step: &WorkflowStep{
				Name: "Checkout code",
				Uses: "actions/checkout@v5",
			},
			expectPinned: true,
			expectedUses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v5",
		},
		{
			name: "step with pinned action (setup-node)",
			step: &WorkflowStep{
				Name: "Setup Node",
				Uses: "actions/setup-node@v6",
				With: map[string]any{
					"node-version": "20",
				},
			},
			expectPinned: true,
			expectedUses: "actions/setup-node@6044e13b5dc448c55e2357c09f80417699197238 # v6",
		},
		{
			name: "step with unpinned action",
			step: &WorkflowStep{
				Name: "Custom action",
				Uses: "my-org/my-action@v1",
			},
			expectPinned: false,
			expectedUses: "my-org/my-action@v1",
		},
		{
			name: "step without uses field",
			step: &WorkflowStep{
				Name: "Run command",
				Run:  "echo hello",
			},
			expectPinned: false,
			expectedUses: "",
		},
		{
			name:         "nil step",
			step:         nil,
			expectPinned: false,
			expectedUses: "",
		},
		{
			name: "step preserves other fields",
			step: &WorkflowStep{
				Name: "Complex step",
				ID:   "test-id",
				Uses: "actions/checkout@v5",
				With: map[string]any{
					"fetch-depth": "0",
				},
				Env: map[string]string{
					"TEST": "value",
				},
			},
			expectPinned: true,
			expectedUses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test WorkflowData
			data := &WorkflowData{}

			result := ApplyActionPinToTypedStep(tt.step, data)

			if tt.step == nil {
				if result != nil {
					t.Errorf("ApplyActionPinToTypedStep(nil) = %v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("ApplyActionPinToTypedStep() returned nil")
			}

			// Check uses field
			if result.Uses != tt.expectedUses {
				t.Errorf("ApplyActionPinToTypedStep() uses = %q, want %q", result.Uses, tt.expectedUses)
			}

			// Verify other fields are preserved
			if result.Name != tt.step.Name {
				t.Errorf("ApplyActionPinToTypedStep() changed name from %q to %q", tt.step.Name, result.Name)
			}
			if result.ID != tt.step.ID {
				t.Errorf("ApplyActionPinToTypedStep() changed id from %q to %q", tt.step.ID, result.ID)
			}
			if result.Run != tt.step.Run {
				t.Errorf("ApplyActionPinToTypedStep() changed run from %q to %q", tt.step.Run, result.Run)
			}

			// Verify original step is not modified
			if tt.expectPinned && tt.step.Uses == result.Uses {
				// For pinned actions, the uses should be different
				// But this doesn't apply if the step is already pinned or doesn't have uses
				if tt.step.Uses != "" && !isValidSHA(extractActionVersion(tt.step.Uses)) {
					// Original uses is not a SHA, so it should be different from pinned result
					if tt.step.Uses == result.Uses {
						t.Errorf("ApplyActionPinToTypedStep() did not create a copy, original uses still %q", tt.step.Uses)
					}
				}
			}
		})
	}
}

// TestApplyActionPinToTypedStep_Immutability verifies that the original step is not modified
func TestApplyActionPinToTypedStep_Immutability(t *testing.T) {
	originalStep := &WorkflowStep{
		Name: "Test step",
		Uses: "actions/checkout@v5",
		With: map[string]any{
			"fetch-depth": "0",
		},
	}

	// Keep a copy of the original uses value
	originalUses := originalStep.Uses

	data := &WorkflowData{}
	result := ApplyActionPinToTypedStep(originalStep, data)

	// Verify the original step was not modified
	if originalStep.Uses != originalUses {
		t.Errorf("ApplyActionPinToTypedStep() modified original step uses: %q -> %q", originalUses, originalStep.Uses)
	}

	// Verify the result is different
	if result.Uses == originalUses {
		t.Errorf("ApplyActionPinToTypedStep() did not pin the action")
	}

	// Verify modifying result doesn't affect original
	result.Name = "Modified name"
	if originalStep.Name == "Modified name" {
		t.Errorf("ApplyActionPinToTypedStep() did not return an independent copy")
	}
}

// TestGetActionPinSemverPreference verifies that when multiple versions exist for the same repo,
// the latest version by semver is returned
func TestGetActionPinSemverPreference(t *testing.T) {
	tests := []struct {
		name            string
		repo            string
		expectedVersion string
	}{
		{
			name:            "setup-go prefers v6.2.0 over v6",
			repo:            "actions/setup-go",
			expectedVersion: "v6.2.0",
		},
		{
			name:            "setup-node prefers v6.2.0 over v6",
			repo:            "actions/setup-node",
			expectedVersion: "v6.2.0",
		},
		{
			name:            "upload-artifact prefers v6.0.0 over v5 and v4",
			repo:            "actions/upload-artifact",
			expectedVersion: "v6.0.0",
		},
		{
			name:            "setup-python prefers v5.6.0 over v5",
			repo:            "actions/setup-python",
			expectedVersion: "v5.6.0",
		},
		{
			name:            "cache prefers v4.3.0 over v4",
			repo:            "actions/cache",
			expectedVersion: "v4.3.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test GetActionPin
			result := GetActionPin(tt.repo)
			if !strings.Contains(result, "# "+tt.expectedVersion) {
				t.Errorf("GetActionPin(%s) = %s, expected version %s", tt.repo, result, tt.expectedVersion)
			}

			// Test GetActionPinByRepo
			pin, exists := GetActionPinByRepo(tt.repo)
			if !exists {
				t.Fatalf("GetActionPinByRepo(%s) returned false, expected true", tt.repo)
			}
			if pin.Version != tt.expectedVersion {
				t.Errorf("GetActionPinByRepo(%s).Version = %s, expected %s", tt.repo, pin.Version, tt.expectedVersion)
			}
		})
	}
}

// TestGetActionPinWithData_SemverPreference tests that GetActionPinWithData
// resolves actions using the exact version tag specified, and only falls back
// to compatible versions when the exact tag doesn't exist in hardcoded pins
func TestGetActionPinWithData_SemverPreference(t *testing.T) {
	tests := []struct {
		name           string
		repo           string
		requestedVer   string
		expectedVer    string
		strictMode     bool
		shouldFallback bool // Whether we expect to fall back to highest version
	}{
		{
			name:           "exact match for setup-go v6.2.0",
			repo:           "actions/setup-go",
			requestedVer:   "v6.2.0",
			expectedVer:    "v6.2.0",
			strictMode:     false,
			shouldFallback: false,
		},
		{
			name:           "exact match for setup-go v6.2.0 from hardcoded pins",
			repo:           "actions/setup-go",
			requestedVer:   "v6.2.0",
			expectedVer:    "v6.2.0", // Should match exactly v6.2.0
			strictMode:     false,
			shouldFallback: false,
		},
		{
			name:           "fallback to highest semver-compatible version for upload-artifact when requesting v4",
			repo:           "actions/upload-artifact",
			requestedVer:   "v4",
			expectedVer:    "v4", // Comment shows requested version, not the pin's v4.6.2
			strictMode:     false,
			shouldFallback: true,
			// Note: When requesting v4 without dynamic resolution, the system uses v4.6.2's SHA
			// (the highest v4.x.x version from hardcoded pins), but shows v4 in the comment
			// to preserve the user's intent.
		},
		{
			name:           "fallback to highest semver-compatible version for upload-artifact when requesting v5",
			repo:           "actions/upload-artifact",
			requestedVer:   "v5",
			expectedVer:    "v5", // Comment shows requested version, not the pin's v5.0.0
			strictMode:     false,
			shouldFallback: true,
		},
		{
			name:           "exact match for upload-artifact v4",
			repo:           "actions/upload-artifact",
			requestedVer:   "v4.6.2",
			expectedVer:    "v4.6.2",
			strictMode:     false,
			shouldFallback: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				StrictMode: tt.strictMode,
			}

			result, err := GetActionPinWithData(tt.repo, tt.requestedVer, data)

			if err != nil {
				t.Fatalf("GetActionPinWithData(%s, %s) returned error: %v", tt.repo, tt.requestedVer, err)
			}

			if result == "" {
				t.Fatalf("GetActionPinWithData(%s, %s) returned empty string", tt.repo, tt.requestedVer)
			}

			// Check that the result contains the expected version in the comment
			if !strings.Contains(result, "# "+tt.expectedVer) {
				t.Errorf("GetActionPinWithData(%s, %s) = %s, expected version %s in comment",
					tt.repo, tt.requestedVer, result, tt.expectedVer)
			}

			// Verify the result format is correct (repo@sha # version)
			if !strings.Contains(result, "@") || !strings.Contains(result, " # ") {
				t.Errorf("GetActionPinWithData(%s, %s) = %s, expected format 'repo@sha # version'",
					tt.repo, tt.requestedVer, result)
			}
		})
	}
}

// TestGetActionPinWithData_AlreadySHA tests that no warnings are issued when
// the version is already a full 40-character SHA
func TestGetActionPinWithData_AlreadySHA(t *testing.T) {
	tests := []struct {
		name        string
		repo        string
		sha         string
		expectError bool
	}{
		{
			name: "actions/checkout with full SHA",
			repo: "actions/checkout",
			sha:  "93cb6efe18208431cddfb9bfd000000000000000", // 40-char SHA
		},
		{
			name: "actions/setup-node with full SHA",
			repo: "actions/setup-node",
			sha:  "395ad3262231945c25e8478fd5baf05154b1d79f", // 40-char SHA from the issue
		},
		{
			name: "different action with full SHA",
			repo: "actions/upload-artifact",
			sha:  "1234567890abcdef1234567890abcdef12345678", // 40-char SHA
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				StrictMode: false,
			}

			// Capture stderr to verify no warnings are issued
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			result, err := GetActionPinWithData(tt.repo, tt.sha, data)

			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)
			stderr := buf.String()

			// Should not error for full SHAs
			if err != nil {
				t.Errorf("GetActionPinWithData() unexpected error = %v", err)
				return
			}

			// Should return the SHA as-is
			if result == "" {
				t.Errorf("GetActionPinWithData() returned empty result")
				return
			}

			// Result should contain the original SHA
			if !strings.Contains(result, tt.sha) {
				t.Errorf("GetActionPinWithData() = %s, expected to contain SHA %s", result, tt.sha)
			}

			// IMPORTANT: Should NOT emit any warnings for actions already pinned to SHAs
			if strings.Contains(stderr, "⚠") || strings.Contains(stderr, "Unable to resolve") {
				t.Errorf("Expected NO warnings for action already pinned to SHA, but got: %s", stderr)
			}

			// Log the resolution for debugging
			t.Logf("Resolution: %s@%s → %s", tt.repo, tt.sha, result)
			if stderr != "" {
				t.Logf("Stderr (should be empty): %s", strings.TrimSpace(stderr))
			}
		})
	}
}

func TestSortPinsByVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    []ActionPin
		expected []ActionPin
	}{
		{
			name: "versions in ascending order",
			input: []ActionPin{
				{Repo: "actions/checkout", Version: "v1", SHA: "abc1"},
				{Repo: "actions/checkout", Version: "v2", SHA: "abc2"},
				{Repo: "actions/checkout", Version: "v3", SHA: "abc3"},
			},
			expected: []ActionPin{
				{Repo: "actions/checkout", Version: "v3", SHA: "abc3"},
				{Repo: "actions/checkout", Version: "v2", SHA: "abc2"},
				{Repo: "actions/checkout", Version: "v1", SHA: "abc1"},
			},
		},
		{
			name: "versions already in descending order",
			input: []ActionPin{
				{Repo: "actions/checkout", Version: "v3", SHA: "abc3"},
				{Repo: "actions/checkout", Version: "v2", SHA: "abc2"},
				{Repo: "actions/checkout", Version: "v1", SHA: "abc1"},
			},
			expected: []ActionPin{
				{Repo: "actions/checkout", Version: "v3", SHA: "abc3"},
				{Repo: "actions/checkout", Version: "v2", SHA: "abc2"},
				{Repo: "actions/checkout", Version: "v1", SHA: "abc1"},
			},
		},
		{
			name: "mixed version order with patch versions",
			input: []ActionPin{
				{Repo: "actions/checkout", Version: "v2.1.0", SHA: "abc210"},
				{Repo: "actions/checkout", Version: "v3.0.0", SHA: "abc300"},
				{Repo: "actions/checkout", Version: "v2.0.1", SHA: "abc201"},
				{Repo: "actions/checkout", Version: "v1.0.0", SHA: "abc100"},
			},
			expected: []ActionPin{
				{Repo: "actions/checkout", Version: "v3.0.0", SHA: "abc300"},
				{Repo: "actions/checkout", Version: "v2.1.0", SHA: "abc210"},
				{Repo: "actions/checkout", Version: "v2.0.1", SHA: "abc201"},
				{Repo: "actions/checkout", Version: "v1.0.0", SHA: "abc100"},
			},
		},
		{
			name:     "empty slice",
			input:    []ActionPin{},
			expected: []ActionPin{},
		},
		{
			name: "single element",
			input: []ActionPin{
				{Repo: "actions/checkout", Version: "v1", SHA: "abc1"},
			},
			expected: []ActionPin{
				{Repo: "actions/checkout", Version: "v1", SHA: "abc1"},
			},
		},
		{
			name: "versions without v prefix",
			input: []ActionPin{
				{Repo: "actions/checkout", Version: "1.0.0", SHA: "abc100"},
				{Repo: "actions/checkout", Version: "2.0.0", SHA: "abc200"},
				{Repo: "actions/checkout", Version: "1.5.0", SHA: "abc150"},
			},
			expected: []ActionPin{
				{Repo: "actions/checkout", Version: "2.0.0", SHA: "abc200"},
				{Repo: "actions/checkout", Version: "1.5.0", SHA: "abc150"},
				{Repo: "actions/checkout", Version: "1.0.0", SHA: "abc100"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// sortPinsByVersion now returns a new sorted slice (immutable operation)
			result := sortPinsByVersion(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("sortPinsByVersion() length = %d, want %d", len(result), len(tt.expected))
				return
			}

			for i := range result {
				if result[i].Repo != tt.expected[i].Repo ||
					result[i].Version != tt.expected[i].Version ||
					result[i].SHA != tt.expected[i].SHA {
					t.Errorf("sortPinsByVersion() at index %d = %+v, want %+v",
						i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestApplyActionPinsToTypedSteps(t *testing.T) {
	// Create a minimal WorkflowData for testing
	data := &WorkflowData{
		StrictMode: false,
	}

	tests := []struct {
		name  string
		steps []*WorkflowStep
		want  []*WorkflowStep
	}{
		{
			name:  "nil steps",
			steps: nil,
			want:  nil,
		},
		{
			name:  "empty steps",
			steps: []*WorkflowStep{},
			want:  []*WorkflowStep{},
		},
		{
			name: "step with uses - should be pinned",
			steps: []*WorkflowStep{
				{
					Name: "Checkout",
					Uses: "actions/checkout@v4",
				},
			},
			want: []*WorkflowStep{
				{
					Name: "Checkout",
					// SHA will be pinned by the function, we just check structure
					Uses: "actions/checkout@",
				},
			},
		},
		{
			name: "step with run - should not change",
			steps: []*WorkflowStep{
				{
					Name: "Run tests",
					Run:  "npm test",
				},
			},
			want: []*WorkflowStep{
				{
					Name: "Run tests",
					Run:  "npm test",
				},
			},
		},
		{
			name: "mixed steps",
			steps: []*WorkflowStep{
				{
					Name: "Checkout",
					Uses: "actions/checkout@v4",
				},
				{
					Name: "Run tests",
					Run:  "npm test",
				},
				{
					Name: "Setup Node",
					Uses: "actions/setup-node@v4",
				},
			},
			want: []*WorkflowStep{
				{
					Name: "Checkout",
					Uses: "actions/checkout@",
				},
				{
					Name: "Run tests",
					Run:  "npm test",
				},
				{
					Name: "Setup Node",
					Uses: "actions/setup-node@",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyActionPinsToTypedSteps(tt.steps, data)

			if len(got) != len(tt.want) {
				t.Errorf("ApplyActionPinsToTypedSteps() returned %d steps, want %d", len(got), len(tt.want))
				return
			}

			for i := range got {
				if got[i] == nil && tt.want[i] == nil {
					continue
				}
				if got[i] == nil || tt.want[i] == nil {
					t.Errorf("ApplyActionPinsToTypedSteps() step %d: got nil=%v, want nil=%v",
						i, got[i] == nil, tt.want[i] == nil)
					continue
				}

				// Check basic fields
				if got[i].Name != tt.want[i].Name {
					t.Errorf("ApplyActionPinsToTypedSteps() step %d name = %s, want %s",
						i, got[i].Name, tt.want[i].Name)
				}
				if got[i].Run != tt.want[i].Run {
					t.Errorf("ApplyActionPinsToTypedSteps() step %d run = %s, want %s",
						i, got[i].Run, tt.want[i].Run)
				}

				// For uses steps, check that pinning occurred (contains @ symbol and SHA)
				if tt.want[i].Uses != "" {
					if !strings.Contains(got[i].Uses, "@") {
						t.Errorf("ApplyActionPinsToTypedSteps() step %d uses = %s, expected to contain @",
							i, got[i].Uses)
					}
					// If the original step had a known action, verify it was pinned
					if strings.HasPrefix(tt.want[i].Uses, "actions/checkout@") ||
						strings.HasPrefix(tt.want[i].Uses, "actions/setup-node@") {
						// Verify the result has a SHA (40 hex chars after @)
						parts := strings.Split(got[i].Uses, "@")
						if len(parts) == 2 {
							shaAndComment := parts[1]
							commentIdx := strings.Index(shaAndComment, " # ")
							if commentIdx != -1 {
								sha := shaAndComment[:commentIdx]
								if len(sha) != 40 {
									t.Errorf("ApplyActionPinsToTypedSteps() step %d uses SHA length = %d, want 40",
										i, len(sha))
								}
							}
						}
					}
				}
			}
		})
	}
}

// TestActionPinsCaching verifies that action pins are cached and not re-parsed
func TestActionPinsCaching(t *testing.T) {
	// Reset the cache by creating a new sync.Once
	// Note: In production, this is handled automatically by sync.Once

	// First call - should load and cache
	pins1 := getActionPins()
	if len(pins1) == 0 {
		t.Fatal("No action pins loaded on first call")
	}

	// Second call - should return cached data (same slice reference)
	pins2 := getActionPins()
	if len(pins2) == 0 {
		t.Fatal("No action pins loaded on second call")
	}

	// Verify both calls return the same data
	if len(pins1) != len(pins2) {
		t.Errorf("Cache returned different number of pins: first=%d, second=%d", len(pins1), len(pins2))
	}

	// Verify the data is identical by checking a few pins
	for i := 0; i < len(pins1) && i < 3; i++ {
		if pins1[i].Repo != pins2[i].Repo {
			t.Errorf("Pin %d repo mismatch: first=%s, second=%s", i, pins1[i].Repo, pins2[i].Repo)
		}
		if pins1[i].Version != pins2[i].Version {
			t.Errorf("Pin %d version mismatch: first=%s, second=%s", i, pins1[i].Version, pins2[i].Version)
		}
		if pins1[i].SHA != pins2[i].SHA {
			t.Errorf("Pin %d SHA mismatch: first=%s, second=%s", i, pins1[i].SHA, pins2[i].SHA)
		}
	}
}

// TestGetActionPinWithData_V5ExactMatch verifies that v5.0.0 resolves to its exact SHA
func TestGetActionPinWithData_V5ExactMatch(t *testing.T) {
	data := &WorkflowData{
		StrictMode: false,
	}

	result, err := GetActionPinWithData("actions/upload-artifact", "v5.0.0", data)

	if err != nil {
		t.Fatalf("GetActionPinWithData returned error: %v", err)
	}

	if result == "" {
		t.Fatalf("GetActionPinWithData returned empty string")
	}

	t.Logf("Result: %s", result)

	// Should match v5.0.0 exactly, not fall back to v6.0.0
	if !strings.Contains(result, "# v5.0.0") {
		t.Errorf("Expected v5.0.0 in result, got: %s", result)
	}

	// Check the SHA matches v5.0.0
	expectedSHA := "330a01c490aca151604b8cf639adc76d48f6c5d4"
	if !strings.Contains(result, expectedSHA) {
		t.Errorf("Expected SHA %s in result, got: %s", expectedSHA, result)
	}
}

// TestGetActionPinWithData_ExactVersionResolution verifies that when resolving
// a version tag like "v4", the system returns exactly "v4" in the comment,
// not a more precise version like "v4.6.2". This ensures we respect the
// user's specified version tag precisely.
func TestGetActionPinWithData_ExactVersionResolution(t *testing.T) {
	tests := []struct {
		name            string
		repo            string
		requestedVer    string
		expectedComment string
	}{
		{
			name:            "v4 resolves to exactly v4, not v4.6.2",
			repo:            "actions/upload-artifact",
			requestedVer:    "v4",
			expectedComment: "# v4",
		},
		{
			name:            "v5 resolves to exactly v5, not v5.0.0",
			repo:            "actions/upload-artifact",
			requestedVer:    "v5",
			expectedComment: "# v5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for cache
			tmpDir := testutil.TempDir(t, "test-*")
			cache := NewActionCache(tmpDir)
			resolver := NewActionResolver(cache)

			// Simulate that we have a cached resolution for the precise version
			// This mimics what would happen if another workflow referenced "v4.6.2"
			// but we request "v4" - we should get back "v4" not "v4.6.2"
			if tt.repo == "actions/upload-artifact" && tt.requestedVer == "v4" {
				// Pre-populate cache with a more precise version to test that
				// we don't inappropriately use it
				cache.Set(tt.repo, "v4.6.2", "c5eb11a343de00d7472c5a5c6598bc1f1fd51144")
				// Now add the exact version we're requesting
				cache.Set(tt.repo, "v4", "c5eb11a343de00d7472c5a5c6598bc1f1fd51144")
			} else if tt.repo == "actions/upload-artifact" && tt.requestedVer == "v5" {
				// Pre-populate cache with a more precise version
				cache.Set(tt.repo, "v5.0.0", "330a01c490aca151604b8cf639adc76d48f6c5d4")
				// Now add the exact version we're requesting
				cache.Set(tt.repo, "v5", "330a01c490aca151604b8cf639adc76d48f6c5d4")
			}

			data := &WorkflowData{
				StrictMode:     false,
				ActionResolver: resolver,
				ActionCache:    cache,
			}

			result, err := GetActionPinWithData(tt.repo, tt.requestedVer, data)

			if err != nil {
				t.Fatalf("GetActionPinWithData returned error: %v", err)
			}

			if result == "" {
				t.Fatalf("GetActionPinWithData returned empty string")
			}

			t.Logf("Result: %s", result)

			// Verify we get exactly the version we requested in the comment
			if !strings.Contains(result, tt.expectedComment) {
				t.Errorf("Expected %q in result, got: %s", tt.expectedComment, result)
			}

			// Ensure we DON'T get a more precise version in the comment
			if tt.requestedVer == "v4" && strings.Contains(result, "# v4.6.2") {
				t.Errorf("Should not have replaced v4 with v4.6.2, got: %s", result)
			}
			if tt.requestedVer == "v5" && strings.Contains(result, "# v5.0.0") {
				t.Errorf("Should not have replaced v5 with v5.0.0, got: %s", result)
			}
		})
	}
}

// TestFallbackVersionUsesRequestedVersionInComment tests that when falling back to
// a semver-compatible version, the comment uses the requested version, not the pin's version.
// For example, if user requests v8 and we fall back to v8.0.0, the comment should say v8.
func TestFallbackVersionUsesRequestedVersionInComment(t *testing.T) {
	tests := []struct {
		name            string
		repo            string
		requestedVer    string
		expectedComment string
	}{
		{
			name:            "v8 falls back to v8.0.0 but comment shows v8",
			repo:            "actions/github-script",
			requestedVer:    "v8",
			expectedComment: "# v8",
		},
		{
			name:            "v7 falls back to v7.0.1 but comment shows v7",
			repo:            "actions/github-script",
			requestedVer:    "v7",
			expectedComment: "# v7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				StrictMode: false,
			}

			result, err := GetActionPinWithData(tt.repo, tt.requestedVer, data)
			if err != nil {
				t.Fatalf("GetActionPinWithData(%s, %s) returned error: %v", tt.repo, tt.requestedVer, err)
			}

			if !strings.Contains(result, tt.expectedComment) {
				t.Errorf("GetActionPinWithData(%s, %s) = %s, expected comment to contain %s",
					tt.repo, tt.requestedVer, result, tt.expectedComment)
			}

			// Also verify it doesn't contain the pin's version
			if tt.requestedVer == "v8" && strings.Contains(result, "# v8.0.0") {
				t.Errorf("GetActionPinWithData(%s, %s) = %s, should use requested version v8 in comment, not v8.0.0",
					tt.repo, tt.requestedVer, result)
			}
			if tt.requestedVer == "v7" && strings.Contains(result, "# v7.0.1") {
				t.Errorf("GetActionPinWithData(%s, %s) = %s, should use requested version v7 in comment, not v7.0.1",
					tt.repo, tt.requestedVer, result)
			}
		})
	}
}

// TestActionPinWarningDeduplication tests that repeated calls to GetActionPinWithData
// for the same action@version only emit the warning once, not multiple times
func TestActionPinWarningDeduplication(t *testing.T) {
	tests := []struct {
		name          string
		repo          string
		version       string
		callCount     int
		expectedWarns int // How many warnings should be emitted
	}{
		{
			name:          "unknown action called 3 times - warn once",
			repo:          "unknown/action",
			version:       "v1.0.0",
			callCount:     3,
			expectedWarns: 1,
		},
		{
			name:          "unknown action called 6 times - warn once",
			repo:          "github/gh-aw/actions/setup",
			version:       "v0.37.0",
			callCount:     6,
			expectedWarns: 1,
		},
		{
			name:          "different versions warn separately",
			repo:          "unknown/action",
			version:       "v1.0.0",
			callCount:     1,
			expectedWarns: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a shared WorkflowData with the warning cache
			data := &WorkflowData{
				StrictMode:        false,
				ActionPinWarnings: make(map[string]bool),
			}

			// Capture stderr to count warnings
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			// Call GetActionPinWithData multiple times
			for i := 0; i < tt.callCount; i++ {
				_, _ = GetActionPinWithData(tt.repo, tt.version, data)
			}

			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)
			stderr := buf.String()

			// Count the number of warnings in stderr
			warningCount := strings.Count(stderr, "⚠")

			if warningCount != tt.expectedWarns {
				t.Errorf("Expected %d warning(s), but got %d\nStderr:\n%s",
					tt.expectedWarns, warningCount, stderr)
			}

			// Verify the cache was populated
			cacheKey := tt.repo + "@" + tt.version
			if !data.ActionPinWarnings[cacheKey] {
				t.Errorf("Expected cache to be populated for %s, but it wasn't", cacheKey)
			}
		})
	}
}

// TestActionPinWarningDeduplicationAcrossDifferentVersions tests that warnings
// for different versions of the same action are NOT deduplicated (each version warns once)
func TestActionPinWarningDeduplicationAcrossDifferentVersions(t *testing.T) {
	// Create a shared WorkflowData with the warning cache
	data := &WorkflowData{
		StrictMode:        false,
		ActionPinWarnings: make(map[string]bool),
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Call with v1.0.0 twice
	_, _ = GetActionPinWithData("unknown/action", "v1.0.0", data)
	_, _ = GetActionPinWithData("unknown/action", "v1.0.0", data)

	// Call with v2.0.0 twice
	_, _ = GetActionPinWithData("unknown/action", "v2.0.0", data)
	_, _ = GetActionPinWithData("unknown/action", "v2.0.0", data)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderr := buf.String()

	// Should have exactly 2 warnings: one for v1.0.0, one for v2.0.0
	warningCount := strings.Count(stderr, "⚠")
	if warningCount != 2 {
		t.Errorf("Expected 2 warnings (one per version), but got %d\nStderr:\n%s",
			warningCount, stderr)
	}

	// Verify both cache keys are populated
	if !data.ActionPinWarnings["unknown/action@v1.0.0"] {
		t.Errorf("Cache should contain key for v1.0.0")
	}
	if !data.ActionPinWarnings["unknown/action@v2.0.0"] {
		t.Errorf("Cache should contain key for v2.0.0")
	}
}

// TestFormatActionReference tests the formatActionReference helper function
func TestFormatActionReference(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		sha      string
		version  string
		expected string
	}{
		{
			name:     "standard action reference",
			repo:     "actions/checkout",
			sha:      "abc1234567890123456789012345678901234567",
			version:  "v4.1.0",
			expected: "actions/checkout@abc1234567890123456789012345678901234567 # v4.1.0",
		},
		{
			name:     "action with simple version",
			repo:     "actions/setup-node",
			sha:      "def9876543210987654321098765432109876543",
			version:  "v20",
			expected: "actions/setup-node@def9876543210987654321098765432109876543 # v20",
		},
		{
			name:     "action with short repo name",
			repo:     "x/y",
			sha:      "1234567890123456789012345678901234567890",
			version:  "v1",
			expected: "x/y@1234567890123456789012345678901234567890 # v1",
		},
		{
			name:     "action with nested repo path",
			repo:     "github/codeql-action/upload-sarif",
			sha:      "abcdef1234567890abcdef1234567890abcdef12",
			version:  "v3.27.9",
			expected: "github/codeql-action/upload-sarif@abcdef1234567890abcdef1234567890abcdef12 # v3.27.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatActionReference(tt.repo, tt.sha, tt.version)
			if result != tt.expected {
				t.Errorf("formatActionReference(%q, %q, %q) = %q, want %q",
					tt.repo, tt.sha, tt.version, result, tt.expected)
			}
		})
	}
}

// TestFormatActionCacheKey tests the formatActionCacheKey helper function
func TestFormatActionCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		version  string
		expected string
	}{
		{
			name:     "standard cache key",
			repo:     "actions/checkout",
			version:  "v4",
			expected: "actions/checkout@v4",
		},
		{
			name:     "cache key with precise version",
			repo:     "actions/setup-node",
			version:  "v20.10.0",
			expected: "actions/setup-node@v20.10.0",
		},
		{
			name:     "cache key with simple version",
			repo:     "x/y",
			version:  "v1",
			expected: "x/y@v1",
		},
		{
			name:     "cache key with SHA",
			repo:     "actions/upload-artifact",
			version:  "abc1234567890123456789012345678901234567",
			expected: "actions/upload-artifact@abc1234567890123456789012345678901234567",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatActionCacheKey(tt.repo, tt.version)
			if result != tt.expected {
				t.Errorf("formatActionCacheKey(%q, %q) = %q, want %q",
					tt.repo, tt.version, result, tt.expected)
			}
		})
	}
}

// TestMapToStepWithActionPinning tests the integration of MapToStep and ApplyActionPinToTypedStep
// This verifies the migration pattern used in compiler_jobs.go, safe_jobs.go, and custom_engine.go
func TestMapToStepWithActionPinning(t *testing.T) {
	tests := []struct {
		name         string
		stepMap      map[string]any
		wantErr      bool
		expectedUses string
	}{
		{
			name: "valid step with action - should pin",
			stepMap: map[string]any{
				"name": "Checkout",
				"uses": "actions/checkout@v5",
			},
			wantErr:      false,
			expectedUses: "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v5",
		},
		{
			name: "valid step with run - should not pin",
			stepMap: map[string]any{
				"name": "Run command",
				"run":  "echo hello",
			},
			wantErr:      false,
			expectedUses: "",
		},
		{
			name: "step with complex fields",
			stepMap: map[string]any{
				"name": "Setup Node",
				"uses": "actions/setup-node@v6",
				"with": map[string]any{
					"node-version": "20",
					"cache":        "npm",
				},
				"env": map[string]string{
					"CI": "true",
				},
			},
			wantErr:      false,
			expectedUses: "actions/setup-node@6044e13b5dc448c55e2357c09f80417699197238 # v6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{}

			// Convert to typed step (as done in migration)
			typedStep, err := MapToStep(tt.stepMap)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MapToStep() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			// Apply action pinning using typed version
			pinnedStep := ApplyActionPinToTypedStep(typedStep, data)
			if pinnedStep == nil {
				t.Fatal("ApplyActionPinToTypedStep returned nil")
			}

			// Verify the result
			if tt.expectedUses != "" {
				if pinnedStep.Uses != tt.expectedUses {
					t.Errorf("pinnedStep.Uses = %q, want %q", pinnedStep.Uses, tt.expectedUses)
				}
			}

			// Verify step can be converted back to map
			resultMap := pinnedStep.ToMap()
			if resultMap == nil {
				t.Fatal("ToMap() returned nil")
			}

			// Verify essential fields are preserved
			if name, ok := tt.stepMap["name"].(string); ok {
				if resultMap["name"] != name {
					t.Errorf("ToMap() name = %v, want %v", resultMap["name"], name)
				}
			}
		})
	}
}

// TestSliceToStepsWithActionPinning tests the integration of SliceToSteps and ApplyActionPinsToTypedSteps
// This verifies the migration pattern used in compiler_orchestrator_workflow.go
func TestSliceToStepsWithActionPinning(t *testing.T) {
	tests := []struct {
		name      string
		steps     []any
		wantErr   bool
		wantCount int
	}{
		{
			name: "mixed steps - some with actions, some with run",
			steps: []any{
				map[string]any{
					"name": "Checkout",
					"uses": "actions/checkout@v5",
				},
				map[string]any{
					"name": "Run command",
					"run":  "echo hello",
				},
				map[string]any{
					"name": "Setup Node",
					"uses": "actions/setup-node@v6",
				},
			},
			wantErr:   false,
			wantCount: 3,
		},
		{
			name:      "empty steps slice",
			steps:     []any{},
			wantErr:   false,
			wantCount: 0,
		},
		{
			name: "all action steps",
			steps: []any{
				map[string]any{
					"name": "Checkout",
					"uses": "actions/checkout@v5",
				},
				map[string]any{
					"name": "Setup Node",
					"uses": "actions/setup-node@v6",
				},
			},
			wantErr:   false,
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{}

			// Convert to typed steps (as done in migration)
			typedSteps, err := SliceToSteps(tt.steps)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SliceToSteps() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			if len(typedSteps) != tt.wantCount {
				t.Errorf("SliceToSteps() returned %d steps, want %d", len(typedSteps), tt.wantCount)
			}

			// Apply action pinning using typed version
			pinnedSteps := ApplyActionPinsToTypedSteps(typedSteps, data)
			if len(pinnedSteps) != len(typedSteps) {
				t.Errorf("ApplyActionPinsToTypedSteps() returned %d steps, want %d", len(pinnedSteps), len(typedSteps))
			}

			// Verify steps can be converted back to slice
			resultSlice := StepsToSlice(pinnedSteps)
			if len(resultSlice) != len(pinnedSteps) {
				t.Errorf("StepsToSlice() returned %d steps, want %d", len(resultSlice), len(pinnedSteps))
			}

			// Verify action steps were pinned
			for i, step := range pinnedSteps {
				if step.Uses != "" && !step.IsUsesStep() {
					t.Errorf("Step %d: Uses field set but IsUsesStep() is false", i)
				}
				if step.Uses != "" {
					// Verify the uses field contains either @ or is a local action
					if !strings.Contains(step.Uses, "@") && !strings.Contains(step.Uses, "./") {
						t.Errorf("Step %d: Uses field %q should contain @ or be a local action", i, step.Uses)
					}
				}
			}
		})
	}
}

// TestMapToStepErrorHandling tests error handling for invalid step maps
func TestMapToStepErrorHandling(t *testing.T) {
	tests := []struct {
		name    string
		stepMap map[string]any
		wantErr bool
	}{
		{
			name:    "nil step map",
			stepMap: nil,
			wantErr: true,
		},
		{
			name:    "empty step map",
			stepMap: map[string]any{},
			wantErr: false, // Empty maps are valid, just produce empty steps
		},
		{
			name: "valid step with all fields",
			stepMap: map[string]any{
				"name":              "Test step",
				"id":                "test-id",
				"uses":              "actions/checkout@v5",
				"with":              map[string]any{"key": "value"},
				"env":               map[string]string{"VAR": "value"},
				"timeout-minutes":   30,
				"continue-on-error": true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MapToStep(tt.stepMap)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapToStep() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSliceToStepsErrorHandling tests error handling for invalid step slices
func TestSliceToStepsErrorHandling(t *testing.T) {
	tests := []struct {
		name    string
		steps   []any
		wantErr bool
	}{
		{
			name:    "nil slice",
			steps:   nil,
			wantErr: false, // nil is handled gracefully
		},
		{
			name:    "empty slice",
			steps:   []any{},
			wantErr: false,
		},
		{
			name: "slice with non-map element",
			steps: []any{
				"not a map",
			},
			wantErr: true,
		},
		{
			name: "slice with mixed valid and invalid elements",
			steps: []any{
				map[string]any{"name": "Valid step"},
				"not a map",
			},
			wantErr: true,
		},
		{
			name: "slice with all valid elements",
			steps: []any{
				map[string]any{"name": "Step 1"},
				map[string]any{"name": "Step 2"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SliceToSteps(tt.steps)
			if (err != nil) != tt.wantErr {
				t.Errorf("SliceToSteps() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
