//go:build !integration

package workflow

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
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
		{ActionModeAction, true},
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
		{ActionModeAction, "action"},
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
	compiler := NewCompiler(WithVersion("1.0.0"))
	if compiler.GetActionMode() != ActionModeDev {
		t.Errorf("Default action mode should be dev, got %s", compiler.GetActionMode())
	}
}

// TestCompilerSetActionMode tests setting the action mode
func TestCompilerSetActionMode(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

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

	compiler.SetActionMode(ActionModeAction)
	if compiler.GetActionMode() != ActionModeAction {
		t.Errorf("Expected action mode action, got %s", compiler.GetActionMode())
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
		{ActionModeAction, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsScript(); got != tt.isScript {
				t.Errorf("ActionMode(%q).IsScript() = %v, want %v", tt.mode, got, tt.isScript)
			}
		})
	}
}

// TestActionModeIsAction tests the IsAction() method
func TestActionModeIsAction(t *testing.T) {
	tests := []struct {
		mode     ActionMode
		isAction bool
	}{
		{ActionModeDev, false},
		{ActionModeRelease, false},
		{ActionModeScript, false},
		{ActionModeAction, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsAction(); got != tt.isAction {
				t.Errorf("ActionMode(%q).IsAction() = %v, want %v", tt.mode, got, tt.isAction)
			}
		})
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
	compiler := NewCompiler(WithVersion("1.0.0"))
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
	compiler := NewCompiler(WithVersion("1.0.0"))
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
	if !strings.Contains(lockStr, "INPUT_DESTINATION: ${{ runner.temp }}/gh-aw/actions") {
		t.Error("Expected INPUT_DESTINATION environment variable in setup step for script mode")
	}

	// 6. Should not use "uses:" for setup action in script mode
	setupActionPattern := "uses: ./actions/setup"
	if strings.Contains(lockStr, setupActionPattern) {
		t.Error("Expected script mode to NOT use 'uses: ./actions/setup' but instead run bash script directly")
	}

	// 7. Checkout should include ref: for the version
	if !strings.Contains(lockStr, "ref: 1.0.0") {
		t.Error("Expected 'ref: 1.0.0' in checkout step for script mode when version is set")
	}

	// 8. Setup step should include INPUT_JOB_NAME for OTLP span job name attribute
	if !strings.Contains(lockStr, "INPUT_JOB_NAME: ${{ github.job }}") {
		t.Error("Expected INPUT_JOB_NAME env var in setup step for script mode")
	}

	// 9. Cleanup step should be generated for script mode (mirrors post.js)
	if !strings.Contains(lockStr, "bash /tmp/gh-aw/actions-source/actions/setup/clean.sh") {
		t.Error("Expected 'Clean Scripts' step with clean.sh in script mode")
	}

	// 10. Cleanup step should run with if: always()
	if !strings.Contains(lockStr, "if: always()") {
		t.Error("Expected 'if: always()' guard on cleanup step in script mode")
	}

	// 11. Checkout actions folder should set clean: false to avoid workspace wipe
	if !strings.Contains(lockStr, "clean: false") {
		t.Error("Expected 'clean: false' in checkout step for script mode to avoid workspace cleanup removing .git")
	}
}

// TestVersionToGitRef tests the versionToGitRef helper function used to derive
// a clean git ref from `git describe` output for use in actions/checkout ref: fields.
func TestVersionToGitRef(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name:    "empty version returns empty ref",
			version: "",
			want:    "",
		},
		{
			name:    "dev version returns empty ref",
			version: "dev",
			want:    "",
		},
		{
			name:    "plain short SHA used as-is",
			version: "e284d1e",
			want:    "e284d1e",
		},
		{
			name:    "short SHA with -dirty suffix stripped",
			version: "e284d1e-dirty",
			want:    "e284d1e",
		},
		{
			name:    "simple version tag used as-is",
			version: "v1.2.3",
			want:    "v1.2.3",
		},
		{
			name:    "version tag with -dirty stripped",
			version: "v1.2.3-dirty",
			want:    "v1.2.3",
		},
		{
			name:    "git describe output with N commits extracts SHA",
			version: "v0.57.2-60-ge284d1e",
			want:    "e284d1e",
		},
		{
			name:    "git describe output with -dirty extracts SHA",
			version: "v0.57.2-60-ge284d1e-dirty",
			want:    "e284d1e",
		},
		{
			name:    "numeric version tag used as-is",
			version: "1.0.0",
			want:    "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionToGitRef(tt.version); got != tt.want {
				t.Errorf("versionToGitRef(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

// TestCheckoutActionsFolderDevModeHasRepository verifies that the Checkout actions folder
// step in dev mode includes repository: github/gh-aw so that cross-repo callers (e.g.
// event-driven relays) can find the actions/ directory instead of defaulting to the
// caller's repo which has no actions/ directory.
func TestCheckoutActionsFolderDevModeHasRepository(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	compiler.SetActionMode(ActionModeDev)

	lines := compiler.generateCheckoutActionsFolder(nil)
	combined := strings.Join(lines, "")

	if !strings.Contains(combined, "repository: github/gh-aw") {
		t.Error("Dev mode Checkout actions folder should include 'repository: github/gh-aw' (fix for #20658)")
	}
	if !strings.Contains(combined, "clean: false") {
		t.Error("Dev mode Checkout actions folder should include 'clean: false' to avoid workspace cleanup removing .git")
	}
}

// TestRestoreActionsSetupStepHasCleanFalse verifies that the Restore actions folder
// step includes clean: false to avoid the workspace wipe that would break post-step
// git commands (e.g. fatal: --local can only be used inside a git repository).
func TestRestoreActionsSetupStepHasCleanFalse(t *testing.T) {
	compiler := NewCompiler(WithVersion("v1.0.0"))
	step := compiler.generateRestoreActionsSetupStep()
	if !strings.Contains(step, "clean: false") {
		t.Error("Restore actions folder step should include 'clean: false' to avoid workspace cleanup removing .git")
	}
}

// TestCheckoutActionsFolderDevModeAlwaysEmitsCheckout verifies that dev mode always
// emits the checkout step regardless of the compiler version, using a runtime macro
// for the ref instead of a compile-time SHA.
func TestCheckoutActionsFolderDevModeAlwaysEmitsCheckout(t *testing.T) {
	versions := []string{"dev", "e284d1e", "v0.57.2-60-ge284d1e", "v1.2.3"}
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			compiler := NewCompiler(WithVersion(version))
			compiler.SetActionMode(ActionModeDev)

			lines := compiler.generateCheckoutActionsFolder(nil)
			if lines == nil {
				t.Errorf("Dev mode should always emit checkout step (version=%q)", version)
			}
		})
	}
}

// TestResolveSetupActionReferenceActionMode tests that action mode resolves to the external gh-aw-actions repo
func TestResolveSetupActionReferenceActionMode(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	ref := ResolveSetupActionReference(context.Background(), ActionModeAction, sha, "", nil)
	expected := "github/gh-aw-actions/setup@" + sha + " # " + sha
	if ref != expected {
		t.Errorf("Action mode should resolve to %q, got %q", expected, ref)
	}
}

// TestResolveSetupActionReferenceActionModeWithTag tests action mode with an explicit action tag
func TestResolveSetupActionReferenceActionModeWithTag(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	ref := ResolveSetupActionReference(context.Background(), ActionModeAction, "v1.0.0", sha, nil)
	expected := "github/gh-aw-actions/setup@" + sha + " # " + sha
	if ref != expected {
		t.Errorf("Action mode with tag should resolve to %q, got %q", expected, ref)
	}
}

// TestResolveSetupActionReferenceActionModeDevVersion tests action mode falls back to local path for dev version
func TestResolveSetupActionReferenceActionModeDevVersion(t *testing.T) {
	ref := ResolveSetupActionReference(context.Background(), ActionModeAction, "dev", "", nil)
	if ref != "./actions/setup" {
		t.Errorf("Action mode with dev version should fall back to './actions/setup', got %q", ref)
	}
}

// TestCheckoutActionsFolderActionModeNoCheckout verifies that action mode does not generate a checkout step
func TestCheckoutActionsFolderActionModeNoCheckout(t *testing.T) {
	compiler := NewCompiler(WithVersion("v1.2.3"))
	compiler.SetActionMode(ActionModeAction)

	lines := compiler.generateCheckoutActionsFolder(nil)
	if len(lines) > 0 {
		t.Error("Action mode should not generate a checkout step for actions folder")
	}
}

// TestActionModeCompilation tests workflow compilation with action mode
func TestActionModeCompilation(t *testing.T) {
	tempDir := t.TempDir()

	workflowContent := `---
name: Test Action Mode
on: issues
safe-outputs:
  create-issue:
    max: 1
---

Test workflow with action mode.
`

	workflowPath := tempDir + "/test-workflow.md"
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	const sha = "0123456789abcdef0123456789abcdef01234567"
	compiler := NewCompiler(WithVersion("v1.2.3"))
	compiler.SetActionMode(ActionModeAction)
	compiler.SetNoEmit(false)
	cache := compiler.GetSharedActionCache()
	cache.Set("github/gh-aw-actions/setup", "v1.2.3", sha)

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify it uses the external gh-aw-actions/setup action
	expected := "github/gh-aw-actions/setup@" + sha + " # v1.2.3"
	if !strings.Contains(lockStr, expected) {
		t.Errorf("Action mode should use %q, lock file:\n%s", expected, lockStr)
	}
	if strings.Contains(lockStr, "github/gh-aw-actions/setup@v1.2.3") {
		t.Errorf("Action mode should not emit mutable gh-aw-actions setup reference, lock file:\n%s", lockStr)
	}

	// Verify it does NOT use the internal gh-aw/actions/setup path
	if strings.Contains(lockStr, "github/gh-aw/actions/setup@") {
		t.Error("Action mode should NOT use 'github/gh-aw/actions/setup@', use external repo instead")
	}

	// Verify no local checkout step for actions folder
	if strings.Contains(lockStr, "Checkout actions folder") {
		t.Error("Action mode should NOT include a 'Checkout actions folder' step")
	}
}

// TestResolveSetupActionReferenceActionModeWithResolver tests that action mode uses SHA resolver when available
func TestResolveSetupActionReferenceActionModeWithResolver(t *testing.T) {
	t.Run("action mode with resolver attempts SHA resolution", func(t *testing.T) {
		// Create mock action resolver and cache
		cache := NewActionCache("")
		resolver := NewActionResolver(cache)

		const sha = "0123456789abcdef0123456789abcdef01234567"
		cache.Set("github/gh-aw-actions/setup", "v1.0.0", sha)
		ref := ResolveSetupActionReference(context.Background(), ActionModeAction, "v1.0.0", "", resolver)

		expected := "github/gh-aw-actions/setup@" + sha + " # v1.0.0"
		if ref != expected {
			t.Errorf("expected %q, got %q", expected, ref)
		}
	})

	t.Run("action mode with nil resolver fails closed when unresolved", func(t *testing.T) {
		ref := ResolveSetupActionReference(context.Background(), ActionModeAction, "v1.0.0", "", nil)
		if ref != "" {
			t.Errorf("expected empty ref, got %q", ref)
		}
	})
}
