//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func assertInvalidAWJSONWarning(t *testing.T, content string) {
	t.Helper()

	gitRoot := t.TempDir()
	workflowsDir := filepath.Join(gitRoot, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "aw.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write invalid aw.json: %v", err)
	}

	compiler := NewCompiler()
	compiler.gitRoot = gitRoot

	var (
		cfg *RepoConfig
		err error
	)
	stderr := captureStderr(func() {
		cfg, err = compiler.loadRepoConfig()
	})

	if err == nil {
		t.Fatal("Expected loadRepoConfig to fail for invalid config")
	}
	if cfg != nil {
		t.Fatal("Expected nil config on loadRepoConfig error")
	}
	if !strings.Contains(stderr, RepoConfigFileName) {
		t.Fatalf("Expected warning to mention %s, got: %s", RepoConfigFileName, stderr)
	}
	if !strings.Contains(stderr, strconv.Itoa(DefaultActionFailureIssueExpiresHours)) {
		t.Fatalf("Expected warning to mention fallback %d, got: %s", DefaultActionFailureIssueExpiresHours, stderr)
	}
	if compiler.GetWarningCount() != 1 {
		t.Fatalf("Expected warning count 1, got %d", compiler.GetWarningCount())
	}
}

func TestCompilerLoadRepoConfig_CachesResult(t *testing.T) {
	gitRoot := t.TempDir()
	workflowsDir := filepath.Join(gitRoot, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	configPath := filepath.Join(workflowsDir, "aw.json")
	if err := os.WriteFile(configPath, []byte(`{"maintenance":{"action_failure_issue_expires":48}}`), 0o600); err != nil {
		t.Fatalf("Failed to write aw.json: %v", err)
	}

	compiler := NewCompiler()
	compiler.gitRoot = gitRoot

	cfg1, err := compiler.loadRepoConfig()
	if err != nil {
		t.Fatalf("Expected first loadRepoConfig call to succeed: %v", err)
	}
	if cfg1 == nil {
		t.Fatal("Expected first loadRepoConfig call to return config")
	}
	if cfg1.ActionFailureIssueExpiresHours() != 48 {
		t.Fatalf("Expected first load to use expiration 48, got %d", cfg1.ActionFailureIssueExpiresHours())
	}

	// Change file contents to verify the second call uses cached values.
	if err := os.WriteFile(configPath, []byte(`{"maintenance":{"action_failure_issue_expires":12}}`), 0o600); err != nil {
		t.Fatalf("Failed to overwrite aw.json: %v", err)
	}

	cfg2, err := compiler.loadRepoConfig()
	if err != nil {
		t.Fatalf("Expected second loadRepoConfig call to use cached config without error: %v", err)
	}
	if cfg2 != cfg1 {
		t.Fatal("Expected cached repo config pointer to be reused")
	}
	if cfg2.ActionFailureIssueExpiresHours() != 48 {
		t.Fatalf("Expected cached expiration to remain 48, got %d", cfg2.ActionFailureIssueExpiresHours())
	}
}

func TestCompilerRepoConfigStrictOverridesFrontmatter(t *testing.T) {
	gitRoot := t.TempDir()
	workflowsDir := filepath.Join(gitRoot, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "aw.json"), []byte(`{"strict":true}`), 0o600); err != nil {
		t.Fatalf("Failed to write aw.json: %v", err)
	}

	compiler := NewCompiler()
	compiler.gitRoot = gitRoot

	if !compiler.effectiveStrictMode(map[string]any{"strict": false}) {
		t.Fatal("Expected repository strict mode to override workflow frontmatter")
	}
}

func TestCompilerLoadRepoConfig_CachesError(t *testing.T) {
	gitRoot := t.TempDir()
	workflowsDir := filepath.Join(gitRoot, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	configPath := filepath.Join(workflowsDir, "aw.json")
	if err := os.WriteFile(configPath, []byte(`{"maintenance":{"action_failure_issue_expires":0}}`), 0o600); err != nil {
		t.Fatalf("Failed to write invalid aw.json: %v", err)
	}

	compiler := NewCompiler()
	compiler.gitRoot = gitRoot

	cfg1, err1 := compiler.loadRepoConfig()
	if err1 == nil {
		t.Fatal("Expected first loadRepoConfig call to fail for invalid config")
	}
	if cfg1 != nil {
		t.Fatal("Expected nil config on first loadRepoConfig error")
	}

	// Fix file contents; second call should still return cached error.
	if err := os.WriteFile(configPath, []byte(`{"maintenance":{"action_failure_issue_expires":24}}`), 0o600); err != nil {
		t.Fatalf("Failed to overwrite aw.json with valid config: %v", err)
	}

	cfg2, err2 := compiler.loadRepoConfig()
	if err2 == nil {
		t.Fatal("Expected second loadRepoConfig call to return cached error")
	}
	if cfg2 != nil {
		t.Fatal("Expected nil config on cached error")
	}
	if err2.Error() != err1.Error() {
		t.Fatalf("Expected cached error to match first error, got %q vs %q", err2.Error(), err1.Error())
	}
}

func TestCompilerLoadRepoConfig_InvalidAWJSONWarnsWithPathAndFallback(t *testing.T) {
	assertInvalidAWJSONWarning(t, `{"maintenance":{"action_failure_issue_expires":0}}`)
}

func TestCompilerLoadRepoConfig_MalformedAWJSONWarnsWithPathAndFallback(t *testing.T) {
	assertInvalidAWJSONWarning(t, `{"maintenance":`)
}

func TestCompilerLoadRepoConfig_EmptyGitRoot(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to chdir to temp dir: %v", err)
	}
	defer func() {
		if restoreErr := os.Chdir(cwd); restoreErr != nil {
			t.Fatalf("Failed to restore working directory: %v", restoreErr)
		}
	}()

	compiler := NewCompiler()
	compiler.gitRoot = ""

	cfg, err := compiler.loadRepoConfig()
	if err != nil {
		t.Fatalf("Expected loadRepoConfig with empty gitRoot to succeed when aw.json is absent: %v", err)
	}
	if cfg == nil {
		t.Fatal("Expected config for empty gitRoot")
	}
	if cfg.ActionFailureIssueExpiresHours() != DefaultActionFailureIssueExpiresHours {
		t.Fatalf(
			"Expected default expiration %d for empty gitRoot, got %d",
			DefaultActionFailureIssueExpiresHours,
			cfg.ActionFailureIssueExpiresHours(),
		)
	}
}
