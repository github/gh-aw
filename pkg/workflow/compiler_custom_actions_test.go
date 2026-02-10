//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/require"
)

// TestActionModeValidation tests the ActionMode type validation
func TestActionModeValidation(t *testing.T) {
	tests := []struct {
		mode  ActionMode
		valid bool
	}{
		{ActionModeDev, true},
		{ActionModeRelease, true},
		{ActionModeScript, true},
		{ActionMode("invalid"), false},
		{ActionMode(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.valid {
				t.Errorf("ActionMode(%q).IsValid() = %v, want %v", tt.mode, got, tt.valid)
			}
		})
	}
}

// TestActionModeString tests the String() method
func TestActionModeString(t *testing.T) {
	tests := []struct {
		mode ActionMode
		want string
	}{
		{ActionModeDev, "dev"},
		{ActionModeRelease, "release"},
		{ActionModeScript, "script"},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.String(); got != tt.want {
				t.Errorf("ActionMode.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCompilerActionModeDefault tests that the compiler defaults to dev mode
func TestCompilerActionModeDefault(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")
	if compiler.GetActionMode() != ActionModeDev {
		t.Errorf("Default action mode should be dev, got %s", compiler.GetActionMode())
	}
}

// TestCompilerSetActionMode tests setting the action mode
func TestCompilerSetActionMode(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	compiler.SetActionMode(ActionModeRelease)
	if compiler.GetActionMode() != ActionModeRelease {
		t.Errorf("Expected action mode release, got %s", compiler.GetActionMode())
	}

	compiler.SetActionMode(ActionModeDev)
	if compiler.GetActionMode() != ActionModeDev {
		t.Errorf("Expected action mode dev, got %s", compiler.GetActionMode())
	}

	compiler.SetActionMode(ActionModeScript)
	if compiler.GetActionMode() != ActionModeScript {
		t.Errorf("Expected action mode script, got %s", compiler.GetActionMode())
	}
}

// TestActionModeIsScript tests the IsScript() method
func TestActionModeIsScript(t *testing.T) {
	tests := []struct {
		mode     ActionMode
		isScript bool
	}{
		{ActionModeDev, false},
		{ActionModeRelease, false},
		{ActionModeScript, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsScript(); got != tt.isScript {
				t.Errorf("ActionMode(%q).IsScript() = %v, want %v", tt.mode, got, tt.isScript)
			}
		})
	}
}

// TestScriptRegistryWithAction tests registering scripts with action paths
func TestScriptRegistryWithAction(t *testing.T) {
	registry := NewScriptRegistry()

	testScript := `console.log('test');`
	actionPath := "./actions/test-action"

	err := registry.RegisterWithAction("test_script", testScript, RuntimeModeGitHubScript, actionPath)
	require.NoError(t, err)

	if !registry.Has("test_script") {
		t.Error("Script should be registered")
	}

	if got := registry.GetActionPath("test_script"); got != actionPath {
		t.Errorf("Expected action path %q, got %q", actionPath, got)
	}

	if got := registry.GetSource("test_script"); got != testScript {
		t.Errorf("Expected source %q, got %q", testScript, got)
	}
}

// TestScriptRegistryActionPathEmpty tests that scripts without action paths return empty string
func TestScriptRegistryActionPathEmpty(t *testing.T) {
	registry := NewScriptRegistry()

	testScript := `console.log('test');`
	registry.Register("test_script", testScript)

	if got := registry.GetActionPath("test_script"); got != "" {
		t.Errorf("Expected empty action path, got %q", got)
	}
}

// TestCustomActionModeCompilation tests workflow compilation with custom action mode
func TestCustomActionModeCompilation(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create a test workflow file
	workflowContent := `---
name: Test Custom Actions
on: issues
safe-outputs:
  create-issue:
    max: 1
---

Test workflow with safe-outputs.
`

	workflowPath := tempDir + "/test-workflow.md"
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Register a test script with an action path
	// Save original state first
	origSource := DefaultScriptRegistry.GetSource("create_issue")
	origActionPath := DefaultScriptRegistry.GetActionPath("create_issue")

	testScript := `
const { core } = require('@actions/core');
core.info('Creating issue');
`
	err := DefaultScriptRegistry.RegisterWithAction(
		"create_issue",
		testScript,
		RuntimeModeGitHubScript,
		"./actions/create-issue",
	)
	require.NoError(t, err)

	// Restore after test
	defer func() {
		if origSource != "" {
			if origActionPath != "" {
				_ = DefaultScriptRegistry.RegisterWithAction("create_issue", origSource, RuntimeModeGitHubScript, origActionPath)
			} else {
				_ = DefaultScriptRegistry.RegisterWithMode("create_issue", origSource, RuntimeModeGitHubScript)
			}
		}
	}()

	// Compile with dev action mode
	compiler := NewCompilerWithVersion("1.0.0")
	compiler.SetActionMode(ActionModeDev)
	compiler.SetNoEmit(false)

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify safe_outputs job exists (consolidated mode)
	safeOutputsJobStart := strings.Index(lockStr, "safe_outputs:")
	if safeOutputsJobStart == -1 {
		t.Fatal("safe_outputs job not found in lock file")
	}

	// Verify handler manager step is present (create_issue is now handled by handler manager)
	if !strings.Contains(lockStr, "id: process_safe_outputs") {
		t.Error("Expected process_safe_outputs step in compiled workflow (create-issue is now handled by handler manager)")
	}
	// Verify handler config contains create_issue
	if !strings.Contains(lockStr, "create_issue") {
		t.Error("Expected create_issue in handler config")
	}

	// Verify the workflow compiles successfully with custom action mode
	if !strings.Contains(lockStr, "actions/github-script") {
		t.Error("Expected github-script action in compiled workflow")
	}
}

// TestInlineActionModeCompilation tests workflow compilation with inline mode (default)
func TestInlineActionModeCompilation(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create a test workflow file
	workflowContent := `---
name: Test Inline Actions
on: issues
safe-outputs:
  create-issue:
    max: 1
---

Test workflow with dev mode.
`

	workflowPath := tempDir + "/test-workflow.md"
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Compile with dev mode (default)
	compiler := NewCompilerWithVersion("1.0.0")
	compiler.SetActionMode(ActionModeDev)
	compiler.SetNoEmit(false)

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify it uses actions/github-script
	if !strings.Contains(lockStr, "actions/github-script@") {
		t.Error("Expected 'actions/github-script@' not found in lock file for inline mode")
	}

	// Verify it has github-token parameter
	if !strings.Contains(lockStr, "github-token:") {
		t.Error("Expected 'github-token:' parameter not found for inline mode")
	}

	// Verify it has script: parameter
	if !strings.Contains(lockStr, "script: |") {
		t.Error("Expected 'script: |' parameter not found for inline mode")
	}
}

// TestCustomActionModeFallback tests that compilation falls back to inline mode
// when action path is not registered
func TestCustomActionModeFallback(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create a test workflow file
	workflowContent := `---
name: Test Fallback
on: issues
safe-outputs:
  create-issue:
    max: 1
---

Test fallback to inline mode.
`

	workflowPath := tempDir + "/test-workflow.md"
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Ensure create_issue is registered without an action path
	// Save original state first
	origSource := DefaultScriptRegistry.GetSource("create_issue")
	origActionPath := DefaultScriptRegistry.GetActionPath("create_issue")

	testScript := `console.log('test');`
	err := DefaultScriptRegistry.RegisterWithMode("create_issue", testScript, RuntimeModeGitHubScript)
	require.NoError(t, err)

	// Restore after test
	defer func() {
		if origSource != "" {
			if origActionPath != "" {
				_ = DefaultScriptRegistry.RegisterWithAction("create_issue", origSource, RuntimeModeGitHubScript, origActionPath)
			} else {
				_ = DefaultScriptRegistry.RegisterWithMode("create_issue", origSource, RuntimeModeGitHubScript)
			}
		}
	}()

	// Compile with dev action mode
	compiler := NewCompilerWithVersion("1.0.0")
	compiler.SetActionMode(ActionModeDev)
	compiler.SetNoEmit(false)

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify it falls back to actions/github-script when action path is not found
	if !strings.Contains(lockStr, "actions/github-script@") {
		t.Error("Expected fallback to 'actions/github-script@' when action path not found")
	}
}

// TestScriptActionModeCompilation tests workflow compilation with script mode
func TestScriptActionModeCompilation(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create a test workflow file with action-mode: script feature flag
	workflowContent := `---
name: Test Script Mode
on: workflow_dispatch
features:
  action-mode: "script"
permissions:
  contents: read
---

Test workflow with script mode.
`

	workflowPath := tempDir + "/test-workflow.md"
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Compile with script mode (will be overridden by feature flag)
	compiler := NewCompilerWithVersion("1.0.0")
	compiler.SetNoEmit(false)

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify script mode behavior:
	// 1. Checkout should use repository: github/gh-aw
	if !strings.Contains(lockStr, "repository: github/gh-aw") {
		t.Error("Expected 'repository: github/gh-aw' in checkout step for script mode")
	}

	// 2. Checkout should target path: /tmp/gh-aw/actions-source
	if !strings.Contains(lockStr, "path: /tmp/gh-aw/actions-source") {
		t.Error("Expected 'path: /tmp/gh-aw/actions-source' in checkout step for script mode")
	}

	// 3. Checkout should use shallow clone (fetch-depth: 1)
	if !strings.Contains(lockStr, "fetch-depth: 1") {
		t.Error("Expected 'fetch-depth: 1' in checkout step for script mode (shallow checkout)")
	}

	// 4. Setup step should run bash script instead of using "uses:"
	if !strings.Contains(lockStr, "bash /tmp/gh-aw/actions-source/actions/setup/setup.sh") {
		t.Error("Expected setup script to run bash directly in script mode")
	}

	// 5. Setup step should have INPUT_DESTINATION environment variable
	if !strings.Contains(lockStr, "INPUT_DESTINATION: /opt/gh-aw/actions") {
		t.Error("Expected INPUT_DESTINATION environment variable in setup step for script mode")
	}

	// 6. Should not use "uses:" for setup action in script mode
	setupActionPattern := "uses: ./actions/setup"
	if strings.Contains(lockStr, setupActionPattern) {
		t.Error("Expected script mode to NOT use 'uses: ./actions/setup' but instead run bash script directly")
	}
}
