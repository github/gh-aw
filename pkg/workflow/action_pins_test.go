//go:build !integration

package workflow

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/testutil"
)

const setupNodeV6ExpectedUsesPlaceholder = "__setup_node_v6__"
const checkoutV6ExpectedUsesPlaceholder = "__checkout_v6__"
const checkoutSHAExpectedUsesPlaceholder = "__checkout_sha__"
const checkoutLatestExpectedUsesPlaceholder = "__checkout_latest__"

func expectedPinnedUses(t *testing.T, repo, version string) string {
	t.Helper()

	result, err := getActionPinWithData(repo, version, &WorkflowData{})
	if err != nil {
		t.Fatalf("getActionPinWithData(%s, %s) returned error: %v", repo, version, err)
	}
	if result == "" {
		t.Fatalf("getActionPinWithData(%s, %s) returned empty result", repo, version)
	}
	return result
}

func latestPinnedUsesForRepo(t *testing.T, repo string) string {
	t.Helper()

	result := getCachedActionPin(repo, &WorkflowData{})
	if result == "" {
		t.Fatalf("getCachedActionPin(%s) returned empty", repo)
	}
	return result
}

func latestActionVersionForRepo(t *testing.T, repo string) string {
	t.Helper()

	pin, ok := getLatestActionPinByRepo(repo)
	if !ok || pin.Version == "" {
		t.Fatalf("getLatestActionPinByRepo(%s) returned no latest pin", repo)
	}
	return pin.Version
}

func captureStderrOutput(t *testing.T, fn func()) string {
	t.Helper()

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		_ = w.Close()
		os.Stderr = oldStderr
	})

	fn()

	_ = w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

// TestGetActionPinFallback tests that getActionPin returns empty string for unknown actions
func TestGetActionPinFallback(t *testing.T) {
	result := getActionPin("unknown/action")
	expected := ""
	if result != expected {
		t.Errorf("getActionPin(unknown/action) = %s, want %s (empty string)", result, expected)
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
				"uses": "actions/checkout@v6",
			},
			expectPinned: true,
			expectedUses: checkoutV6ExpectedUsesPlaceholder,
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
			expectedUses: setupNodeV6ExpectedUsesPlaceholder,
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
			name: "step with unversioned action",
			stepMap: map[string]any{
				"name": "Checkout",
				"uses": "actions/checkout",
			},
			expectPinned: true,
			expectedUses: checkoutLatestExpectedUsesPlaceholder,
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
				"uses": "actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd",
			},
			expectPinned: true,
			expectedUses: checkoutSHAExpectedUsesPlaceholder,
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
			pinnedStep, pinErr := applyActionPinToTypedStep(typedStep, data)
			if pinErr != nil {
				t.Fatalf("applyActionPinToTypedStep() returned error: %v", pinErr)
			}
			if pinnedStep == nil {
				t.Fatal("applyActionPinToTypedStep returned nil")
			}

			// Convert back to map for comparison
			result := pinnedStep.ToMap()

			// Check if uses field exists in result
			if uses, hasUses := result["uses"]; hasUses {
				usesStr, ok := uses.(string)
				if !ok {
					t.Errorf("applyActionPinToTypedStep returned non-string uses field")
					return
				}

				expectedUses := tt.expectedUses
				if expectedUses == setupNodeV6ExpectedUsesPlaceholder {
					expectedUses = expectedPinnedUses(t, "actions/setup-node", "v6")
				}
				if expectedUses == checkoutV6ExpectedUsesPlaceholder {
					expectedUses = expectedPinnedUses(t, "actions/checkout", "v6")
				}
				if expectedUses == checkoutSHAExpectedUsesPlaceholder {
					expectedUses = expectedPinnedUses(t, "actions/checkout", "de0fac2e4500dabe0009e67214ff5f5447ce83dd")
				}
				if expectedUses == checkoutLatestExpectedUsesPlaceholder {
					expectedUses = latestPinnedUsesForRepo(t, "actions/checkout")
				}
				if usesStr != expectedUses {
					t.Errorf("applyActionPinToTypedStep uses = %q, want %q", usesStr, expectedUses)
				}

				// Verify other fields are preserved (check length and keys)
				if len(result) != len(tt.stepMap) {
					t.Errorf("applyActionPinToTypedStep changed number of fields: got %d, want %d", len(result), len(tt.stepMap))
				}
				for k := range tt.stepMap {
					if _, exists := result[k]; !exists {
						t.Errorf("applyActionPinToTypedStep lost field %q", k)
					}
				}
			} else if tt.expectedUses != "" {
				t.Errorf("applyActionPinToTypedStep removed uses field when it should be %q", tt.expectedUses)
			}
		})
	}
}

// TestGetLatestActionPinByRepo tests the getLatestActionPinByRepo function
func TestGetLatestActionPinByRepo(t *testing.T) {
	tests := []struct {
		repo                string
		expectExists        bool
		expectRepo          string
		expectVersion       string
		expectVersionPrefix string
	}{
		{
			repo:                "actions/checkout",
			expectExists:        true,
			expectRepo:          "actions/checkout",
			expectVersionPrefix: "v",
		},
		{
			repo:                "actions/setup-node",
			expectExists:        true,
			expectRepo:          "actions/setup-node",
			expectVersionPrefix: "v",
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
			if tt.expectVersion != "" && tt.expectVersionPrefix != "" {
				t.Fatalf("invalid test case: expectVersion and expectVersionPrefix are mutually exclusive")
			}

			pin, exists := getLatestActionPinByRepo(tt.repo)

			if exists != tt.expectExists {
				t.Errorf("getLatestActionPinByRepo(%s) exists = %v, want %v", tt.repo, exists, tt.expectExists)
			}

			if tt.expectExists {
				if pin.Repo != tt.expectRepo {
					t.Errorf("getLatestActionPinByRepo(%s) repo = %s, want %s", tt.repo, pin.Repo, tt.expectRepo)
				}
				if tt.expectVersion != "" && pin.Version != tt.expectVersion {
					t.Errorf("getLatestActionPinByRepo(%s) version = %s, want %s", tt.repo, pin.Version, tt.expectVersion)
				}
				if tt.expectVersionPrefix != "" && !strings.HasPrefix(pin.Version, tt.expectVersionPrefix) {
					t.Errorf("getLatestActionPinByRepo(%s) version = %s, want prefix %s", tt.repo, pin.Version, tt.expectVersionPrefix)
				}
				if !isValidSHA(pin.SHA) {
					t.Errorf("getLatestActionPinByRepo(%s) has invalid SHA: %s", tt.repo, pin.SHA)
				}
			}
		})
	}
}

// TestApplyActionPinToTypedStep tests the applyActionPinToTypedStep function with typed steps
func TestApplyActionPinToTypedStep(t *testing.T) {
	tests := []struct {
		name         string
		step         *WorkflowStep
		expectPinned bool
		expectedUses string
		wantErr      bool
	}{
		{
			name: "step with pinned action (checkout)",
			step: &WorkflowStep{
				Name: "Checkout code",
				Uses: "actions/checkout@v6",
			},
			expectPinned: true,
			expectedUses: checkoutV6ExpectedUsesPlaceholder,
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
			expectedUses: setupNodeV6ExpectedUsesPlaceholder,
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
			name: "step with unversioned action in embedded pins",
			step: &WorkflowStep{
				Name: "Checkout",
				Uses: "actions/checkout",
			},
			expectPinned: true,
			expectedUses: checkoutLatestExpectedUsesPlaceholder,
		},
		{
			name: "step with unversioned action not in pins returns error",
			step: &WorkflowStep{
				Name: "Custom action",
				Uses: "my-org/my-unpinned-action",
			},
			wantErr: true,
		},
		{
			name: "local action ref (./) is passed through unchanged",
			step: &WorkflowStep{
				Name: "Local action",
				Uses: "./.github/actions/my-setup",
			},
			expectPinned: false,
			expectedUses: "./.github/actions/my-setup",
		},
		{
			name: "local action ref (../) returns error",
			step: &WorkflowStep{
				Name: "Parent local action",
				Uses: "../other-repo/.github/actions/shared",
			},
			wantErr: true,
		},
		{
			name: "docker image ref is passed through unchanged",
			step: &WorkflowStep{
				Name: "Docker action",
				Uses: "docker://alpine:3.20",
			},
			expectPinned: false,
			expectedUses: "docker://alpine:3.20",
		},
		{
			name: "docker image ref with digest is passed through unchanged",
			step: &WorkflowStep{
				Name: "Docker action with digest",
				Uses: "docker://alpine@sha256:abc123",
			},
			expectPinned: false,
			expectedUses: "docker://alpine@sha256:abc123",
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
				Uses: "actions/checkout@v6",
				With: map[string]any{
					"fetch-depth": "0",
				},
				Env: map[string]string{
					"TEST": "value",
				},
			},
			expectPinned: true,
			expectedUses: checkoutV6ExpectedUsesPlaceholder,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test WorkflowData
			data := &WorkflowData{}

			result, err := applyActionPinToTypedStep(tt.step, data)

			if tt.step == nil {
				if err != nil {
					t.Errorf("applyActionPinToTypedStep(nil) returned error: %v", err)
				}
				if result != nil {
					t.Errorf("applyActionPinToTypedStep(nil) = %v, want nil", result)
				}
				return
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("applyActionPinToTypedStep() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("applyActionPinToTypedStep() returned error: %v", err)
			}

			if result == nil {
				t.Fatalf("applyActionPinToTypedStep() returned nil")
			}

			// Check uses field
			expectedUses := tt.expectedUses
			if expectedUses == setupNodeV6ExpectedUsesPlaceholder {
				expectedUses = expectedPinnedUses(t, "actions/setup-node", "v6")
			}
			if expectedUses == checkoutV6ExpectedUsesPlaceholder {
				expectedUses = expectedPinnedUses(t, "actions/checkout", "v6")
			}
			if expectedUses == checkoutLatestExpectedUsesPlaceholder {
				expectedUses = latestPinnedUsesForRepo(t, "actions/checkout")
			}
			if result.Uses != expectedUses {
				t.Errorf("applyActionPinToTypedStep() uses = %q, want %q", result.Uses, expectedUses)
			}

			// Verify other fields are preserved
			if result.Name != tt.step.Name {
				t.Errorf("applyActionPinToTypedStep() changed name from %q to %q", tt.step.Name, result.Name)
			}
			if result.ID != tt.step.ID {
				t.Errorf("applyActionPinToTypedStep() changed id from %q to %q", tt.step.ID, result.ID)
			}
			if result.Run != tt.step.Run {
				t.Errorf("applyActionPinToTypedStep() changed run from %q to %q", tt.step.Run, result.Run)
			}

			// Verify original step is not modified
			if tt.expectPinned && tt.step.Uses == result.Uses {
				// For pinned actions, the uses should be different
				// But this doesn't apply if the step is already pinned or doesn't have uses
				if tt.step.Uses != "" && !isValidSHA(extractActionVersion(tt.step.Uses)) {
					// Original uses is not a SHA, so it should be different from pinned result
					if tt.step.Uses == result.Uses {
						t.Errorf("applyActionPinToTypedStep() did not create a copy, original uses still %q", tt.step.Uses)
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
	result, err := applyActionPinToTypedStep(originalStep, data)
	if err != nil {
		t.Fatalf("applyActionPinToTypedStep() returned error: %v", err)
	}

	// Verify the original step was not modified
	if originalStep.Uses != originalUses {
		t.Errorf("applyActionPinToTypedStep() modified original step uses: %q -> %q", originalUses, originalStep.Uses)
	}

	// Verify the result is different
	if result.Uses == originalUses {
		t.Errorf("applyActionPinToTypedStep() did not pin the action")
	}

	// Verify modifying result doesn't affect original
	result.Name = "Modified name"
	if originalStep.Name == "Modified name" {
		t.Errorf("applyActionPinToTypedStep() did not return an independent copy")
	}
}

// TestGetActionPinWithData_SemverPreference tests that getActionPinWithData
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
			name:           "fallback for setup-go v6.2.0 resolves to latest pinned version",
			repo:           "actions/setup-go",
			requestedVer:   "v6.2.0",
			strictMode:     false,
			shouldFallback: true,
		},
		{
			name:           "fallback for setup-go v6.2.0 from hardcoded pins resolves to latest pinned version",
			repo:           "actions/setup-go",
			requestedVer:   "v6.2.0",
			strictMode:     false,
			shouldFallback: true,
		},
		{
			name:           "fallback to highest semver-compatible version for upload-artifact when requesting v4",
			repo:           "actions/upload-artifact",
			requestedVer:   "v4",
			expectedVer:    "v7.0.1",
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
			expectedVer:    "v7.0.1",
			strictMode:     false,
			shouldFallback: true,
		},
		{
			name:           "fallback for upload-artifact v4.6.2 resolves to latest pinned version",
			repo:           "actions/upload-artifact",
			requestedVer:   "v4.6.2",
			strictMode:     false,
			shouldFallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.shouldFallback && tt.expectedVer == "" {
				t.Fatalf("invalid test case %q: expectedVer must be set when shouldFallback is false", tt.name)
			}

			data := &WorkflowData{
				StrictMode: tt.strictMode,
			}

			result, err := getActionPinWithData(tt.repo, tt.requestedVer, data)

			if err != nil {
				t.Fatalf("getActionPinWithData(%s, %s) returned error: %v", tt.repo, tt.requestedVer, err)
			}

			if result == "" {
				t.Fatalf("getActionPinWithData(%s, %s) returned empty string", tt.repo, tt.requestedVer)
			}

			expectedVersion := tt.expectedVer
			if tt.shouldFallback {
				latestPin, ok := getLatestActionPinByRepo(tt.repo)
				if !ok {
					t.Fatalf("expected latest pinned version to exist for %s", tt.repo)
				}
				expectedVersion = latestPin.Version
			}

			// Check that the result contains the expected version in the comment
			if !strings.Contains(result, "# "+expectedVersion) {
				t.Errorf("getActionPinWithData(%s, %s) = %s, expected version %s in comment",
					tt.repo, tt.requestedVer, result, expectedVersion)
			}

			// Verify the result format is correct (repo@sha # version)
			if !strings.Contains(result, "@") || !strings.Contains(result, " # ") {
				t.Errorf("getActionPinWithData(%s, %s) = %s, expected format 'repo@sha # version'",
					tt.repo, tt.requestedVer, result)
			}

			if tt.shouldFallback && !strings.Contains(result, "(source ") {
				t.Errorf("getActionPinWithData(%s, %s) = %s, expected fallback to include resolved-version metadata",
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

			result, err := getActionPinWithData(tt.repo, tt.sha, data)

			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)
			stderr := buf.String()

			// Should not error for full SHAs
			if err != nil {
				t.Errorf("getActionPinWithData() unexpected error = %v", err)
				return
			}

			// Should return the SHA as-is
			if result == "" {
				t.Errorf("getActionPinWithData() returned empty result")
				return
			}

			// Result should contain the original SHA
			if !strings.Contains(result, tt.sha) {
				t.Errorf("getActionPinWithData() = %s, expected to contain SHA %s", result, tt.sha)
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

func TestApplyActionPinsToTypedSteps(t *testing.T) {
	// Create a minimal WorkflowData for testing
	data := &WorkflowData{
		StrictMode: false,
	}

	tests := []struct {
		name    string
		steps   []*WorkflowStep
		want    []*WorkflowStep
		wantErr bool
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
		{
			name: "unversioned action not in pins returns error",
			steps: []*WorkflowStep{
				{
					Name: "Unknown action",
					Uses: "my-org/my-unpinned-action",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyActionPinsToTypedSteps(tt.steps, data)

			if tt.wantErr {
				if err == nil {
					t.Errorf("applyActionPinsToTypedSteps() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("applyActionPinsToTypedSteps() returned unexpected error: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Errorf("applyActionPinsToTypedSteps() returned %d steps, want %d", len(got), len(tt.want))
				return
			}

			for i := range got {
				if got[i] == nil && tt.want[i] == nil {
					continue
				}
				if got[i] == nil || tt.want[i] == nil {
					t.Errorf("applyActionPinsToTypedSteps() step %d: got nil=%v, want nil=%v",
						i, got[i] == nil, tt.want[i] == nil)
					continue
				}

				// Check basic fields
				if got[i].Name != tt.want[i].Name {
					t.Errorf("applyActionPinsToTypedSteps() step %d name = %s, want %s",
						i, got[i].Name, tt.want[i].Name)
				}
				if got[i].Run != tt.want[i].Run {
					t.Errorf("applyActionPinsToTypedSteps() step %d run = %s, want %s",
						i, got[i].Run, tt.want[i].Run)
				}

				// For uses steps, check that pinning occurred (contains @ symbol and SHA)
				if tt.want[i].Uses != "" {
					if !strings.Contains(got[i].Uses, "@") {
						t.Errorf("applyActionPinsToTypedSteps() step %d uses = %s, expected to contain @",
							i, got[i].Uses)
					}
					// If the original step had a known action, verify it was pinned
					if strings.HasPrefix(tt.want[i].Uses, "actions/checkout@") ||
						strings.HasPrefix(tt.want[i].Uses, "actions/setup-node@") {
						// Verify the result has a SHA (40 hex chars after @)
						parts := strings.Split(got[i].Uses, "@")
						if len(parts) == 2 {
							shaAndComment := parts[1]
							before, _, ok := strings.Cut(shaAndComment, " # ")
							if ok {
								sha := before
								if len(sha) != 40 {
									t.Errorf("applyActionPinsToTypedSteps() step %d uses SHA length = %d, want 40",
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

// TestGetActionPinWithData_V7Fallback verifies v7 fallback preserves source annotation.
func TestGetActionPinWithData_V7Fallback(t *testing.T) {
	data := &WorkflowData{
		StrictMode: false,
	}

	result, err := getActionPinWithData("actions/upload-artifact", "v7", data)

	if err != nil {
		t.Fatalf("getActionPinWithData returned error: %v", err)
	}

	if result == "" {
		t.Fatalf("getActionPinWithData returned empty string")
	}

	t.Logf("Result: %s", result)

	// Should include resolved + source format for fallback.
	if !strings.Contains(result, "# v7.0.1 (source v7)") {
		t.Errorf("Expected resolved/source comment format in result, got: %s", result)
	}

	// Check the SHA matches v7 (resolves to v7.0.1 pin)
	expectedSHA := "043fb46d1a93c77aae656e7c1c64a875d1fc6a0a"
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

			result, err := getActionPinWithData(tt.repo, tt.requestedVer, data)

			if err != nil {
				t.Fatalf("getActionPinWithData returned error: %v", err)
			}

			if result == "" {
				t.Fatalf("getActionPinWithData returned empty string")
			}

			t.Logf("Result: %s", result)

			// Verify we get exactly the version we requested in the comment
			if !strings.Contains(result, tt.expectedComment) {
				t.Errorf("Expected %q in result, got: %s", tt.expectedComment, result)
			}

			// Exact cache hits should preserve the exact requested version without fallback metadata.
			if strings.Contains(result, "(source ") {
				t.Errorf("Did not expect resolved-version fallback metadata for exact cache hit, got: %s", result)
			}
		})
	}
}

// TestFallbackVersionUsesRequestedVersionInComment tests that fallback comments
// now record both resolved and source versions.
func TestFallbackVersionUsesRequestedVersionInComment(t *testing.T) {
	tests := []struct {
		name            string
		repo            string
		requestedVer    string
		expectedComment string
		expectedSHA     string
	}{
		{
			name:            "v8 falls back to v9.0.0 and comment records source v8",
			repo:            "actions/github-script",
			requestedVer:    "v8",
			expectedComment: "# v9.0.0 (source v8)",
			expectedSHA:     "3a2844b7e9c422d3c10d287c895573f7108da1b3",
		},
		{
			name:            "v7 falls back to v9.0.0 and comment records source v7",
			repo:            "actions/github-script",
			requestedVer:    "v7",
			expectedComment: "# v9.0.0 (source v7)",
			expectedSHA:     "3a2844b7e9c422d3c10d287c895573f7108da1b3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				StrictMode: false,
			}

			result, err := getActionPinWithData(tt.repo, tt.requestedVer, data)
			if err != nil {
				t.Fatalf("getActionPinWithData(%s, %s) returned error: %v", tt.repo, tt.requestedVer, err)
			}

			if !strings.Contains(result, tt.expectedComment) {
				t.Errorf("getActionPinWithData(%s, %s) = %s, expected comment to contain %s",
					tt.repo, tt.requestedVer, result, tt.expectedComment)
			}

			if !strings.Contains(result, tt.expectedSHA) {
				t.Errorf("getActionPinWithData(%s, %s) = %s, expected SHA %s",
					tt.repo, tt.requestedVer, result, tt.expectedSHA)
			}

			if tt.requestedVer == "v8" && !strings.Contains(result, "# v9.0.0 (source v8)") {
				t.Errorf("Expected v8 fallback comment to record resolved version v9.0.0, got: %s", result)
			}
		})
	}
}

// TestActionPinWarningDeduplication tests that repeated calls to getActionPinWithData
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
				AllowActionRefs:   true,
				ActionPinWarnings: make(map[string]bool),
			}

			// Capture stderr to count warnings
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			// Call getActionPinWithData multiple times
			for range tt.callCount {
				_, _ = getActionPinWithData(tt.repo, tt.version, data)
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
		AllowActionRefs:   true,
		ActionPinWarnings: make(map[string]bool),
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Call with v1.0.0 twice
	_, _ = getActionPinWithData("unknown/action", "v1.0.0", data)
	_, _ = getActionPinWithData("unknown/action", "v1.0.0", data)

	// Call with v2.0.0 twice
	_, _ = getActionPinWithData("unknown/action", "v2.0.0", data)
	_, _ = getActionPinWithData("unknown/action", "v2.0.0", data)

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

// TestMapToStepWithActionPinning tests the integration of MapToStep and applyActionPinToTypedStep
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
				"uses": "actions/checkout@v6",
			},
			wantErr:      false,
			expectedUses: checkoutV6ExpectedUsesPlaceholder,
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
			expectedUses: setupNodeV6ExpectedUsesPlaceholder,
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
			pinnedStep, pinErr := applyActionPinToTypedStep(typedStep, data)
			if pinErr != nil {
				t.Fatalf("applyActionPinToTypedStep() returned error: %v", pinErr)
			}
			if pinnedStep == nil {
				t.Fatal("applyActionPinToTypedStep returned nil")
			}

			// Verify the result
			expectedUses := tt.expectedUses
			if expectedUses == setupNodeV6ExpectedUsesPlaceholder {
				expectedUses = expectedPinnedUses(t, "actions/setup-node", "v6")
			}
			if expectedUses == checkoutV6ExpectedUsesPlaceholder {
				expectedUses = expectedPinnedUses(t, "actions/checkout", "v6")
			}
			if expectedUses != "" {
				if pinnedStep.Uses != expectedUses {
					t.Errorf("pinnedStep.Uses = %q, want %q", pinnedStep.Uses, expectedUses)
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

// TestSliceToStepsWithActionPinning tests the integration of SliceToSteps and applyActionPinsToTypedSteps
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
			pinnedSteps, pinErr := applyActionPinsToTypedSteps(typedSteps, data)
			if pinErr != nil {
				t.Fatalf("applyActionPinsToTypedSteps() returned unexpected error: %v", pinErr)
			}
			if len(pinnedSteps) != len(typedSteps) {
				t.Errorf("applyActionPinsToTypedSteps() returned %d steps, want %d", len(pinnedSteps), len(typedSteps))
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

// TestGetActionPinGHESArtifactCompat verifies GHES compat mode emits compatible v3 artifact pins.
func TestGetActionPinGHESArtifactCompat(t *testing.T) {
	// Verify default (compat disabled) returns latest (v7/v8)
	defaultCompiler := NewCompiler()
	uploadPin := defaultCompiler.getActionPin("actions/upload-artifact")
	if uploadPin == "" {
		t.Fatal("getActionPin(actions/upload-artifact) returned empty")
	}
	if strings.Contains(uploadPin, "# v3") {
		t.Errorf("Without GHES compat, expected latest upload-artifact pin, got v3: %s", uploadPin)
	}

	downloadPin := defaultCompiler.getActionPin("actions/download-artifact")
	if downloadPin == "" {
		t.Fatal("getActionPin(actions/download-artifact) returned empty")
	}
	if strings.Contains(downloadPin, "# v3") {
		t.Errorf("Without GHES compat, expected latest download-artifact pin, got v3: %s", downloadPin)
	}

	// Capture checkout pin before compat to assert it is unchanged after
	checkoutPinBefore := defaultCompiler.getActionPin("actions/checkout")
	if checkoutPinBefore == "" {
		t.Fatal("getActionPin(actions/checkout) returned empty")
	}

	// Enable GHES compat via a separate Compiler instance
	compatCompiler := NewCompiler()
	compatCompiler.ghesArtifactCompat = true

	uploadPinGHES := compatCompiler.getActionPin("actions/upload-artifact")
	if !strings.Contains(uploadPinGHES, "c6a366c94c3e0affe28c06c8df20a878f24da3cf # v3.2.2") {
		t.Errorf("With GHES compat, expected upload-artifact v3.2.2 pin, got: %s", uploadPinGHES)
	}
	if uploadPinGHES == uploadPin {
		t.Errorf("With GHES compat, expected upload-artifact pin to differ from default, got: %s", uploadPinGHES)
	}

	downloadPinGHES := compatCompiler.getActionPin("actions/download-artifact")
	if !strings.Contains(downloadPinGHES, "a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59 # v3.1.0") {
		t.Errorf("With GHES compat, expected download-artifact v3.1.0 pin, got: %s", downloadPinGHES)
	}
	if downloadPinGHES == downloadPin {
		t.Errorf("With GHES compat, expected download-artifact pin to differ from default, got: %s", downloadPinGHES)
	}

	// Non-artifact actions should be unaffected by GHES compat
	checkoutPinAfter := compatCompiler.getActionPin("actions/checkout")
	if checkoutPinAfter != checkoutPinBefore {
		t.Errorf("GHES compat should not affect non-artifact actions: before=%s after=%s", checkoutPinBefore, checkoutPinAfter)
	}
}

// TestGHESArtifactCompatPinsExist verifies GHES compatibility pins are complete.
func TestGHESArtifactCompatPinsExist(t *testing.T) {
	c := NewCompiler()
	c.ghesArtifactCompat = true
	for _, repo := range []string{"actions/upload-artifact", "actions/download-artifact"} {
		t.Run(repo, func(t *testing.T) {
			result := c.getActionPin(repo)
			if result == "" {
				t.Errorf("getActionPin(%s) returned empty with GHES compat enabled", repo)
			}
			if !strings.Contains(result, "# v3") {
				t.Errorf("getActionPin(%s) should return a v3 pin, got: %s", repo, result)
			}
		})
	}
}

func TestGeneratedArtifactStepsHonorGHESCompat(t *testing.T) {
	t.Run("firewall log upload uses workflow data", func(t *testing.T) {
		data := &WorkflowData{Name: "GHES Firewall", GHES: true}
		step := strings.Join([]string(generateSquidLogsUploadStep(data.Name, data)), "\n")
		if !strings.Contains(step, "actions/upload-artifact@c6a366c94c3e0affe28c06c8df20a878f24da3cf # v3.2.2") {
			t.Fatalf("expected GHES upload-artifact pin in firewall step, got:\n%s", step)
		}
		if strings.Contains(step, "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a") {
			t.Fatalf("expected firewall step not to use default upload-artifact pin, got:\n%s", step)
		}
	})

	t.Run("repo memory download uses compiler compatibility", func(t *testing.T) {
		c := NewCompiler()
		c.SetGHESCompat(true)
		c.configureGHESCompatibility()
		data := &WorkflowData{
			RepoMemoryConfig: &RepoMemoryConfig{
				Memories: []RepoMemoryEntry{{ID: "default"}},
			},
		}
		step := strings.Join(c.buildPushRepoMemoryDownloadSteps(data, false), "\n")
		if !strings.Contains(step, "actions/download-artifact@a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59 # v3.1.0") {
			t.Fatalf("expected GHES download-artifact pin in repo-memory step, got:\n%s", step)
		}
		if strings.Contains(step, "actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c") {
			t.Fatalf("expected repo-memory step not to use default download-artifact pin, got:\n%s", step)
		}
	})
}

func TestGetActionPinPrefersLatestEmbeddedOverStaleCache(t *testing.T) {
	latestCacheRestorePin, ok := getLatestActionPinByRepo("actions/cache/restore")
	if !ok {
		t.Fatal("expected embedded pin for actions/cache/restore")
	}

	tests := []struct {
		name       string
		cacheEntry ActionCacheEntry
		want       string
	}{
		{
			name: "prefers newer embedded pin over stale cache",
			cacheEntry: ActionCacheEntry{
				Repo:    "actions/cache/restore",
				Version: "v4",
				SHA:     "0057852bfaa89a56745cba8c7296529d2fc39830",
			},
			want: getActionPin("actions/cache/restore"),
		},
		{
			name: "keeps equal cached version",
			cacheEntry: ActionCacheEntry{
				Repo:    "actions/cache/restore",
				Version: latestCacheRestorePin.Version,
				SHA:     latestCacheRestorePin.SHA,
			},
			want: getActionPin("actions/cache/restore"),
		},
		{
			name: "keeps newer cached version",
			cacheEntry: ActionCacheEntry{
				Repo:    "actions/cache/restore",
				Version: "v999.0.0",
				SHA:     "1111111111111111111111111111111111111111",
			},
			want: "actions/cache/restore@1111111111111111111111111111111111111111 # v999.0.0",
		},
		{
			name: "falls back when cached version is unparseable",
			cacheEntry: ActionCacheEntry{
				Repo:    "actions/cache/restore",
				Version: "not-a-version",
				SHA:     "2222222222222222222222222222222222222222",
			},
			want: getActionPin("actions/cache/restore"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			cacheKey := formatActionCacheKey(tt.cacheEntry.Repo, tt.cacheEntry.Version)
			c.actionCache = &ActionCache{
				Entries: map[string]ActionCacheEntry{
					cacheKey: tt.cacheEntry,
				},
			}
			c.actionResolver = NewActionResolver(c.actionCache)

			got := c.getActionPin(tt.cacheEntry.Repo)
			if got != tt.want {
				t.Fatalf("expected compiler-generated action pin %q, got %q", tt.want, got)
			}
		})
	}
}

// TestWarnIfOutdatedActionVersion tests the warnIfOutdatedActionVersion helper.
func TestWarnIfOutdatedActionVersion(t *testing.T) {
	const checkoutRepo = "actions/checkout"
	checkoutLatest := latestActionVersionForRepo(t, checkoutRepo)
	checkoutLatestSemver := semverutil.ParseVersion(checkoutLatest)
	if checkoutLatestSemver == nil || checkoutLatestSemver.Major < 1 {
		t.Skipf("need a parseable checkout version with major >= 1, got %q", checkoutLatest)
	}
	checkoutMajorTag := fmt.Sprintf("v%d", checkoutLatestSemver.Major)
	checkoutMinorTag := fmt.Sprintf("v%d.%d", checkoutLatestSemver.Major, checkoutLatestSemver.Minor)
	checkoutOlderMajorTag := fmt.Sprintf("v%d", checkoutLatestSemver.Major-1)
	checkoutOlderMinorTag := fmt.Sprintf("v%d.1", checkoutLatestSemver.Major-1)

	tests := []struct {
		name         string
		repo         string
		rawVersion   string
		latestVer    string
		expectWarn   bool
		warnContains string
	}{
		{
			name:         "older major version warns",
			repo:         checkoutRepo,
			rawVersion:   "v5.0.0",
			latestVer:    checkoutLatest,
			expectWarn:   true,
			warnContains: "v5.0.0",
		},
		{
			name:         "older minor version warns",
			repo:         checkoutRepo,
			rawVersion:   "v7.0.0",
			latestVer:    "v7.1.0",
			expectWarn:   true,
			warnContains: "v7.0.0",
		},
		{
			name:       "same version does not warn",
			repo:       checkoutRepo,
			rawVersion: checkoutLatest,
			latestVer:  checkoutLatest,
			expectWarn: false,
		},
		{
			name:       "partial tag same major does not warn",
			repo:       checkoutRepo,
			rawVersion: checkoutMajorTag,
			latestVer:  checkoutLatest,
			expectWarn: false,
		},
		{
			name:       "minor partial tag same major does not warn",
			repo:       checkoutRepo,
			rawVersion: checkoutMinorTag,
			latestVer:  checkoutLatest,
			expectWarn: false,
		},
		{
			name:         "partial tag older major warns",
			repo:         checkoutRepo,
			rawVersion:   checkoutOlderMajorTag,
			latestVer:    checkoutLatest,
			expectWarn:   true,
			warnContains: checkoutOlderMajorTag,
		},
		{
			name:         "minor partial tag older major warns",
			repo:         checkoutRepo,
			rawVersion:   checkoutOlderMinorTag,
			latestVer:    checkoutLatest,
			expectWarn:   true,
			warnContains: checkoutOlderMinorTag,
		},
		{
			name:       "SHA ref does not warn",
			repo:       checkoutRepo,
			rawVersion: "11bd71901bbe5b1630ceea73d27597364c9af683",
			latestVer:  checkoutLatest,
			expectWarn: false,
		},
		{
			name:       "branch ref does not warn",
			repo:       checkoutRepo,
			rawVersion: "main",
			latestVer:  checkoutLatest,
			expectWarn: false,
		},
		{
			name:       "newer than latest does not warn",
			repo:       checkoutRepo,
			rawVersion: "v8.0.0",
			latestVer:  checkoutLatest,
			expectWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{}
			output := captureStderrOutput(t, func() {
				warnIfOutdatedActionVersion(tt.repo, tt.rawVersion, tt.latestVer, data)
			})

			if tt.expectWarn {
				if !strings.Contains(output, "⚠") || !strings.Contains(output, "is outdated") {
					t.Errorf("expected outdated warning marker/message in %q", output)
				}
				if tt.warnContains != "" && !strings.Contains(output, tt.warnContains) {
					t.Errorf("warning output %q does not contain %q", output, tt.warnContains)
				}
				if !strings.Contains(output, tt.latestVer) {
					t.Errorf("warning output %q does not mention latest version %q", output, tt.latestVer)
				}
				if !strings.Contains(output, "\n  Consider upgrading") {
					t.Errorf("warning output %q does not include upgrade guidance on a new line", output)
				}
			} else {
				if strings.Contains(output, "⚠") || strings.Contains(output, "is outdated") {
					t.Errorf("expected no outdated warning but got: %q", output)
				}
			}
		})
	}
}

func TestWarnIfOutdatedActionVersion_NilDataDoesNotWarn(t *testing.T) {
	output := captureStderrOutput(t, func() {
		warnIfOutdatedActionVersion("actions/checkout", "v5.0.0", "v7.0.0", nil)
	})

	if strings.Contains(output, "⚠") || strings.Contains(output, "is outdated") {
		t.Fatalf("expected nil WorkflowData to suppress outdated warnings, got: %q", output)
	}
}

// TestWarnIfOutdatedActionVersion_Deduplication verifies the warning fires only once per repo@version.
func TestWarnIfOutdatedActionVersion_Deduplication(t *testing.T) {
	data := &WorkflowData{}
	output := captureStderrOutput(t, func() {
		warnIfOutdatedActionVersion("actions/checkout", "v5.0.0", "v7.0.0", data)
		warnIfOutdatedActionVersion("actions/checkout", "v5.0.0", "v7.0.0", data)
	})

	count := strings.Count(output, "⚠ Action actions/checkout@")
	if count != 1 {
		t.Errorf("expected warning to appear exactly once, got %d occurrences in: %q", count, output)
	}
}

// TestApplyActionPinToTypedStep_OutdatedWarning checks that applying an outdated action
// pin via applyActionPinToTypedStep emits a warning on stderr.
func TestApplyActionPinToTypedStep_OutdatedWarning(t *testing.T) {
	latestVersion := latestActionVersionForRepo(t, "actions/checkout")
	latestSemver := semverutil.ParseVersion(latestVersion)
	if latestSemver == nil || latestSemver.Major < 2 {
		t.Skipf("need a latest checkout version with major >= 2, got %q", latestVersion)
	}
	outdatedVersion := fmt.Sprintf("v%d.0.0", latestSemver.Major-1)

	step := &WorkflowStep{
		Name: "Checkout",
		Uses: "actions/checkout@" + outdatedVersion,
	}

	data := &WorkflowData{}
	var pinErr error
	output := captureStderrOutput(t, func() {
		_, pinErr = applyActionPinToTypedStep(step, data)
	})

	if pinErr != nil {
		t.Fatalf("applyActionPinToTypedStep() returned unexpected error: %v", pinErr)
	}
	if !strings.Contains(output, "⚠") || !strings.Contains(output, "is outdated") {
		t.Fatalf("expected outdated-version warning marker/message in stderr, got: %q", output)
	}
	if !strings.Contains(output, outdatedVersion) {
		t.Errorf("expected outdated-version warning mentioning %s in stderr, got: %q", outdatedVersion, output)
	}
	if !strings.Contains(output, latestVersion) {
		t.Errorf("expected outdated-version warning mentioning latest %s in stderr, got: %q", latestVersion, output)
	}
}

// TestApplyActionPinToTypedStep_NoOutdatedWarningForCurrentVersion checks that no
// outdated warning is emitted when the step uses the latest available version.
func TestApplyActionPinToTypedStep_NoOutdatedWarningForCurrentVersion(t *testing.T) {
	latestVersion := latestActionVersionForRepo(t, "actions/checkout")
	step := &WorkflowStep{
		Name: "Checkout",
		Uses: "actions/checkout@" + latestVersion,
	}

	data := &WorkflowData{}
	var pinErr error
	output := captureStderrOutput(t, func() {
		_, pinErr = applyActionPinToTypedStep(step, data)
	})

	if pinErr != nil {
		t.Fatalf("applyActionPinToTypedStep() returned unexpected error: %v", pinErr)
	}
	if strings.Contains(output, "⚠") || strings.Contains(output, "is outdated") {
		t.Errorf("expected no outdated warning for current version, got: %q", output)
	}
}
