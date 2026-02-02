//go:build integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/require"
)

// TestActionModeDetection tests the DetectActionMode function
func TestActionModeDetection(t *testing.T) {
	tests := []struct {
		name         string
		githubRef    string
		githubEvent  string
		envOverride  string
		expectedMode ActionMode
		description  string
	}{
		{
			name:         "main branch",
			githubRef:    "refs/heads/main",
			githubEvent:  "push",
			expectedMode: ActionModeDev,
			description:  "Main branch should use dev mode (not release)",
		},
		{
			name:         "release tag",
			githubRef:    "refs/tags/v1.0.0",
			githubEvent:  "push",
			expectedMode: ActionModeRelease,
			description:  "Release tags should use release mode",
		},
		{
			name:         "release branch",
			githubRef:    "refs/heads/release-1.0",
			githubEvent:  "push",
			expectedMode: ActionModeRelease,
			description:  "Release branches should use release mode",
		},
		{
			name:         "release event",
			githubRef:    "refs/heads/main",
			githubEvent:  "release",
			expectedMode: ActionModeRelease,
			description:  "Release events should use release mode",
		},
		{
			name:         "pull request",
			githubRef:    "refs/pull/123/merge",
			githubEvent:  "pull_request",
			expectedMode: ActionModeDev,
			description:  "Pull requests should use dev mode",
		},
		{
			name:         "feature branch",
			githubRef:    "refs/heads/feature/test",
			githubEvent:  "push",
			expectedMode: ActionModeDev,
			description:  "Feature branches should use dev mode",
		},
		{
			name:         "local development",
			githubRef:    "",
			githubEvent:  "",
			expectedMode: ActionModeDev,
			description:  "Local development (no GITHUB_REF) should use dev mode",
		},
		// Removed inline mode test case as inline mode no longer exists
		{
			name:         "env override to dev",
			githubRef:    "refs/heads/main",
			githubEvent:  "push",
			envOverride:  "dev",
			expectedMode: ActionModeDev,
			description:  "Environment variable should override to dev mode",
		},
		{
			name:         "env override to release",
			githubRef:    "refs/heads/feature/test",
			githubEvent:  "push",
			envOverride:  "release",
			expectedMode: ActionModeRelease,
			description:  "Environment variable should override to release mode",
		},
		{
			name:         "invalid env override",
			githubRef:    "refs/heads/main",
			githubEvent:  "push",
			envOverride:  "invalid",
			expectedMode: ActionModeDev,
			description:  "Invalid environment variable should be ignored, main branch uses dev mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original environment
			origRef := os.Getenv("GITHUB_REF")
			origEvent := os.Getenv("GITHUB_EVENT_NAME")
			origMode := os.Getenv("GH_AW_ACTION_MODE")
			defer func() {
				// Restore environment variables properly
				if origRef != "" {
					os.Setenv("GITHUB_REF", origRef)
				} else {
					os.Unsetenv("GITHUB_REF")
				}
				if origEvent != "" {
					os.Setenv("GITHUB_EVENT_NAME", origEvent)
				} else {
					os.Unsetenv("GITHUB_EVENT_NAME")
				}
				if origMode != "" {
					os.Setenv("GH_AW_ACTION_MODE", origMode)
				} else {
					os.Unsetenv("GH_AW_ACTION_MODE")
				}
			}()

			// Set test environment
			if tt.githubRef != "" {
				os.Setenv("GITHUB_REF", tt.githubRef)
			} else {
				os.Unsetenv("GITHUB_REF")
			}

			if tt.githubEvent != "" {
				os.Setenv("GITHUB_EVENT_NAME", tt.githubEvent)
			} else {
				os.Unsetenv("GITHUB_EVENT_NAME")
			}

			if tt.envOverride != "" {
				os.Setenv("GH_AW_ACTION_MODE", tt.envOverride)
			} else {
				os.Unsetenv("GH_AW_ACTION_MODE")
			}

			// Test detection
			mode := DetectActionMode("dev")
			if mode != tt.expectedMode {
				t.Errorf("%s: expected mode %s, got %s", tt.description, tt.expectedMode, mode)
			}
		})
	}
}

// TestActionModeReleaseValidation tests that release mode is valid
func TestActionModeReleaseValidation(t *testing.T) {
	if !ActionModeRelease.IsValid() {
		t.Error("ActionModeRelease should be valid")
	}

	if ActionModeRelease.String() != "release" {
		t.Errorf("Expected string 'release', got %q", ActionModeRelease.String())
	}
}

// TestActionModeDetectionWithReleaseFlag tests that DetectActionMode uses the release flag
func TestActionModeDetectionWithReleaseFlag(t *testing.T) {
	tests := []struct {
		name         string
		isRelease    bool
		githubRef    string
		githubEvent  string
		envOverride  string
		expectedMode ActionMode
		description  string
	}{
		{
			name:         "release flag true",
			isRelease:    true,
			githubRef:    "",
			githubEvent:  "",
			expectedMode: ActionModeRelease,
			description:  "Release flag set to true should use release mode",
		},
		{
			name:         "release flag false",
			isRelease:    false,
			githubRef:    "",
			githubEvent:  "",
			expectedMode: ActionModeDev,
			description:  "Release flag set to false should use dev mode",
		},
		{
			name:         "release flag true with main branch",
			isRelease:    true,
			githubRef:    "refs/heads/main",
			githubEvent:  "push",
			expectedMode: ActionModeRelease,
			description:  "Release flag should take precedence over branch",
		},
		{
			name:         "release flag false with release tag",
			isRelease:    false,
			githubRef:    "refs/tags/v1.0.0",
			githubEvent:  "push",
			expectedMode: ActionModeRelease,
			description:  "GitHub release tag should still work when release flag is false",
		},
		{
			name:         "env override with release flag",
			isRelease:    true,
			githubRef:    "",
			githubEvent:  "",
			envOverride:  "dev",
			expectedMode: ActionModeDev,
			description:  "Environment variable should override release flag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original environment and release flag
			origRef := os.Getenv("GITHUB_REF")
			origEvent := os.Getenv("GITHUB_EVENT_NAME")
			origMode := os.Getenv("GH_AW_ACTION_MODE")
			origRelease := IsRelease()

			defer func() {
				// Restore environment variables
				if origRef != "" {
					os.Setenv("GITHUB_REF", origRef)
				} else {
					os.Unsetenv("GITHUB_REF")
				}
				if origEvent != "" {
					os.Setenv("GITHUB_EVENT_NAME", origEvent)
				} else {
					os.Unsetenv("GITHUB_EVENT_NAME")
				}
				if origMode != "" {
					os.Setenv("GH_AW_ACTION_MODE", origMode)
				} else {
					os.Unsetenv("GH_AW_ACTION_MODE")
				}
				// Restore release flag
				SetIsRelease(origRelease)
			}()

			// Set test environment
			if tt.githubRef != "" {
				os.Setenv("GITHUB_REF", tt.githubRef)
			} else {
				os.Unsetenv("GITHUB_REF")
			}

			if tt.githubEvent != "" {
				os.Setenv("GITHUB_EVENT_NAME", tt.githubEvent)
			} else {
				os.Unsetenv("GITHUB_EVENT_NAME")
			}

			if tt.envOverride != "" {
				os.Setenv("GH_AW_ACTION_MODE", tt.envOverride)
			} else {
				os.Unsetenv("GH_AW_ACTION_MODE")
			}

			// Set release flag
			SetIsRelease(tt.isRelease)

			// Test detection (version parameter is ignored now)
			mode := DetectActionMode("ignored-version")
			if mode != tt.expectedMode {
				t.Errorf("%s: expected mode %s, got %s", tt.description, tt.expectedMode, mode)
			}
		})
	}
}

// TestReleaseModeCompilation tests workflow compilation in release mode
// Note: This test uses create_issue which already has ScriptName set.
// Other safe outputs (add_labels, etc.) don't have ScriptName yet and will use inline mode.
func TestReleaseModeCompilation(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Save original environment
	origSHA := os.Getenv("GITHUB_SHA")
	origRef := os.Getenv("GITHUB_REF")
	defer func() {
		if origSHA != "" {
			os.Setenv("GITHUB_SHA", origSHA)
		} else {
			os.Unsetenv("GITHUB_SHA")
		}
		if origRef != "" {
			os.Setenv("GITHUB_REF", origRef)
		} else {
			os.Unsetenv("GITHUB_REF")
		}
	}()

	// Set release tag for testing
	os.Setenv("GITHUB_REF", "refs/tags/v1.0.0") // Simulate release tag for auto-detection

	// Create a test workflow file
	workflowContent := `---
name: Test Release Mode
on: issues
safe-outputs:
  create-issue:
    max: 1
---

Test workflow with release mode.
`

	workflowPath := tempDir + "/test-workflow.md"
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Save the original script to restore after test
	origScript := DefaultScriptRegistry.Get("create_issue")
	origActionPath := DefaultScriptRegistry.GetActionPath("create_issue")

	// Register test script with action path
	testScript := `const { core } = require('@actions/core'); core.info('test');`
	err := DefaultScriptRegistry.RegisterWithAction(
		"create_issue",
		testScript,
		RuntimeModeGitHubScript,
		"./actions/create-issue",
	)
	require.NoError(t, err)

	// Restore after test
	defer func() {
		if origActionPath != "" {
			_ = DefaultScriptRegistry.RegisterWithAction("create_issue", origScript, RuntimeModeGitHubScript, origActionPath)
		} else {
			_ = DefaultScriptRegistry.RegisterWithMode("create_issue", origScript, RuntimeModeGitHubScript)
		}
	}()

	// Compile - should auto-detect release mode from GITHUB_REF
	compiler := NewCompilerWithVersion("1.0.0")
	// Don't set action mode explicitly - let it auto-detect
	compiler.SetActionMode(DetectActionMode("1.0.0"))
	compiler.SetNoEmit(false)

	if compiler.GetActionMode() != ActionModeRelease {
		t.Fatalf("Expected auto-detected release mode, got %s", compiler.GetActionMode())
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	// Read lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify safe_outputs job exists (consolidated mode)
	if !strings.Contains(lockStr, "safe_outputs:") {
		t.Error("Expected safe_outputs job in compiled workflow")
	}

	// Verify handler manager step is present (create_issue is now handled by handler manager)
	if !strings.Contains(lockStr, "id: process_safe_outputs") {
		t.Error("Expected process_safe_outputs step in compiled workflow (create-issue is now handled by handler manager)")
	}
	// Verify handler config contains create_issue
	if !strings.Contains(lockStr, "create_issue") {
		t.Error("Expected create_issue in handler config")
	}
}

// TestDevModeCompilation tests workflow compilation in dev mode
// Note: This test uses create_issue which already has ScriptName set.
func TestDevModeCompilation(t *testing.T) {
	tempDir := t.TempDir()

	// Save original environment
	origRef := os.Getenv("GITHUB_REF")
	defer os.Setenv("GITHUB_REF", origRef)

	// Set environment for dev mode
	os.Setenv("GITHUB_REF", "") // Local development (no GITHUB_REF)

	workflowContent := `---
name: Test Dev Mode  
on: issues
safe-outputs:
  create-issue:
    max: 1
---

Test
`

	workflowPath := tempDir + "/test-workflow.md"
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow: %v", err)
	}

	// Save original script
	origScript := DefaultScriptRegistry.Get("create_issue")
	origActionPath := DefaultScriptRegistry.GetActionPath("create_issue")

	testScript := `const { core } = require('@actions/core'); core.info('test');`
	err := DefaultScriptRegistry.RegisterWithAction("create_issue", testScript, RuntimeModeGitHubScript, "./actions/create-issue")
	require.NoError(t, err)

	defer func() {
		if origActionPath != "" {
			_ = DefaultScriptRegistry.RegisterWithAction("create_issue", origScript, RuntimeModeGitHubScript, origActionPath)
		} else {
			_ = DefaultScriptRegistry.RegisterWithMode("create_issue", origScript, RuntimeModeGitHubScript)
		}
	}()

	compiler := NewCompilerWithVersion("1.0.0")
	compiler.SetActionMode(DetectActionMode("dev"))
	compiler.SetNoEmit(false)

	if compiler.GetActionMode() != ActionModeDev {
		t.Fatalf("Expected auto-detected dev mode, got %s", compiler.GetActionMode())
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify safe_outputs job exists (consolidated mode)
	if !strings.Contains(lockStr, "safe_outputs:") {
		t.Error("Expected safe_outputs job in compiled workflow")
	}

	// Verify handler manager step is present (create_issue is now handled by handler manager)
	if !strings.Contains(lockStr, "id: process_safe_outputs") {
		t.Error("Expected process_safe_outputs step in compiled workflow (create-issue is now handled by handler manager)")
	}
	// Verify handler config contains create_issue
	if !strings.Contains(lockStr, "create_issue") {
		t.Error("Expected create_issue in handler config")
	}
}
