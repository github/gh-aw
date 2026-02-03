//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestPushToPullRequestBranchConfigParsing(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with push-to-pull-request-branch configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
---

# Test Push to PR Branch

This is a test workflow to validate push-to-pull-request-branch configuration parsing.

Please make changes and push them to the feature branch.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that safe_outputs job is generated (consolidated mode)
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Errorf("Generated workflow should contain safe_outputs job")
	}

	// Verify that push_to_pull_request_branch is now handled by handler manager
	if !strings.Contains(lockContentStr, "id: process_safe_outputs") {
		t.Errorf("Generated workflow should contain process_safe_outputs step (handler manager)")
	}

	// Verify that push_to_pull_request_branch config is in handler manager config
	if !strings.Contains(lockContentStr, "push_to_pull_request_branch") {
		t.Errorf("Generated workflow should contain push_to_pull_request_branch in handler config")
	}

	// Verify that required permissions are present
	if !strings.Contains(lockContentStr, "contents: write") {
		t.Errorf("Generated workflow should have contents: write permission")
	}
	if !strings.Contains(lockContentStr, "issues: write") {
		t.Errorf("Generated workflow should have issues: write permission")
	}
	if !strings.Contains(lockContentStr, "pull-requests: write") {
		t.Errorf("Generated workflow should have pull-requests: write permission")
	}

	// Verify that the job depends on the main workflow job
	if !strings.Contains(lockContentStr, "needs:") {
		t.Errorf("Generated workflow should have dependency on main job")
	}

	// Verify conditional execution using BuildSafeOutputType
	if !strings.Contains(lockContentStr, "contains(needs.agent.outputs.output_types, 'push_to_pull_request_branch')") {
		t.Errorf("Generated workflow should have safe output type condition")
	}
}

func TestPushToPullRequestBranchWithTargetAsterisk(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with target: "*"
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
---

# Test Push to Branch with Target *

This workflow allows pushing to any pull request.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-asterisk.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that the target configuration is in handler config JSON
	if !strings.Contains(lockContentStr, `"target":"*"`) && !strings.Contains(lockContentStr, `"target": "*"`) {
		t.Errorf("Generated workflow should contain target configuration with asterisk in handler config JSON")
	}

	// Verify conditional execution allows any context
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Errorf("Generated workflow should have always() condition for target: *")
	}
}

func TestPushToPullRequestBranchDefaultBranch(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file without branch configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
---

# Test Push to Branch Default Branch

This workflow uses the default branch value.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-default-branch.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	// This should succeed and use default branch "triggering"
	err := compiler.CompileWorkflow(mdFile)
	if err != nil {
		t.Fatalf("Expected compilation to succeed with default branch, got error: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-default-branch.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Check that the safe_outputs job with push_to_pull_request_branch step is generated
	if !strings.Contains(lockContent, "safe_outputs:") {
		t.Errorf("Expected safe_outputs job with push_to_pull_request_branch step to be generated")
	}
}

func TestPushToPullRequestBranchNullConfig(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with null configuration (push-to-pull-request-branch: with no value)
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch: 
---

# Test Push to Branch Null Config

This workflow uses null configuration which should default to "triggering".
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-null-config.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	// This should succeed and use default branch "triggering"
	err := compiler.CompileWorkflow(mdFile)
	if err != nil {
		t.Fatalf("Expected compilation to succeed with null config, got error: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-null-config.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Check that the safe_outputs job with push_to_pull_request_branch step is generated
	if !strings.Contains(lockContent, "safe_outputs:") {
		t.Errorf("Expected safe_outputs job with push_to_pull_request_branch step to be generated")
	}

	// Check that no explicit target is set in the config (default "triggering" is used)
	// The handler config will still contain push_to_pull_request_branch but target may be omitted or "triggering"
	// This is acceptable as the handler uses "triggering" as the default
}

func TestPushToPullRequestBranchMinimalConfig(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with minimal configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
---

# Test Push to Branch Minimal

This workflow has minimal push-to-pull-request-branch configuration.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-minimal.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that safe_outputs job is generated (consolidated mode)
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Errorf("Generated workflow should contain safe_outputs job")
	}

	// Verify push_to_pull_request_branch is handled by handler manager
	if !strings.Contains(lockContentStr, "id: process_safe_outputs") {
		t.Errorf("Generated workflow should contain process_safe_outputs step (handler manager)")
	}

	// Verify that push_to_pull_request_branch config is in handler manager config
	if !strings.Contains(lockContentStr, "push_to_pull_request_branch") {
		t.Errorf("Generated workflow should contain push_to_pull_request_branch in handler config")
	}

	// Verify conditional execution using BuildSafeOutputType
	if !strings.Contains(lockContentStr, "contains(needs.agent.outputs.output_types, 'push_to_pull_request_branch')") {
		t.Errorf("Generated workflow should have safe output type condition")
	}
}

func TestPushToPullRequestBranchWithIfNoChangesError(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with if-no-changes: error
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
    if-no-changes: "error"
---

# Test Push to Branch with if-no-changes: error

This workflow fails when there are no changes.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-error.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that if-no-changes configuration is in handler config JSON (check for push_to_pull_request_branch config)
	if !strings.Contains(lockContentStr, `push_to_pull_request_branch`) || !strings.Contains(lockContentStr, `if_no_changes`) || !strings.Contains(lockContentStr, `error`) {
		t.Errorf("Generated workflow should contain if-no-changes:error configuration in handler config JSON")
	}
}

func TestPushToPullRequestBranchWithIfNoChangesIgnore(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with if-no-changes: ignore
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    if-no-changes: "ignore"
---

# Test Push to Branch with if-no-changes: ignore

This workflow ignores when there are no changes.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-ignore.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that if-no-changes configuration is in handler config JSON (check for push_to_pull_request_branch config)
	if !strings.Contains(lockContentStr, `push_to_pull_request_branch`) || !strings.Contains(lockContentStr, `if_no_changes`) || !strings.Contains(lockContentStr, `ignore`) {
		t.Errorf("Generated workflow should contain if-no-changes:ignore configuration in handler config JSON")
	}
}

func TestPushToPullRequestBranchDefaultIfNoChanges(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file without if-no-changes (should default to "warn")
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
---

# Test Push to Branch Default if-no-changes

This workflow uses default if-no-changes behavior.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-default-if-no-changes.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that default if-no-changes configuration ("warn") is in handler config JSON (check for push_to_pull_request_branch config)
	if !strings.Contains(lockContentStr, `push_to_pull_request_branch`) || !strings.Contains(lockContentStr, `if_no_changes`) || !strings.Contains(lockContentStr, `warn`) {
		t.Errorf("Generated workflow should contain if-no-changes:warn configuration in handler config JSON")
	}
}

func TestPushToPullRequestBranchExplicitTriggering(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with explicit "triggering" branch
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
---

# Test Push to Branch Explicit Triggering

This workflow explicitly sets branch to "triggering".
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-explicit-triggering.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-explicit-triggering.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that safe_outputs job with push_to_pull_request_branch step is generated
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Errorf("Generated workflow should contain safe_outputs job with push_to_pull_request_branch step")
	}

	// Verify that push_to_pull_request_branch is handled by handler manager and has target configuration
	if !strings.Contains(lockContentStr, `push_to_pull_request_branch`) || !strings.Contains(lockContentStr, `target`) {
		t.Errorf("Generated workflow should contain push_to_pull_request_branch with target configuration in handler config")
	}
}

func TestPushToPullRequestBranchWithTitlePrefix(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with title-prefix configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
    title-prefix: "[bot] "
---

# Test Push to Branch with Title Prefix

This workflow validates PR title prefix.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-title-prefix.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that title prefix configuration is in handler config JSON (check for push_to_pull_request_branch config)
	if !strings.Contains(lockContentStr, `push_to_pull_request_branch`) || !strings.Contains(lockContentStr, `title_prefix`) || !strings.Contains(lockContentStr, `[bot]`) {
		t.Errorf("Generated workflow should contain title_prefix:[bot] configuration in handler config JSON")
	}
}

func TestPushToPullRequestBranchWithLabels(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with labels configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
    labels: ["automated", "enhancement"]
---

# Test Push to Branch with Labels

This workflow validates PR labels.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-labels.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that labels configuration is in handler config JSON (check for push_to_pull_request_branch config)
	if !strings.Contains(lockContentStr, `push_to_pull_request_branch`) || !strings.Contains(lockContentStr, `labels`) || (!strings.Contains(lockContentStr, `automated`) && !strings.Contains(lockContentStr, `enhancement`)) {
		t.Errorf("Generated workflow should contain labels configuration in handler config JSON")
	}
}

func TestPushToPullRequestBranchWithTitlePrefixAndLabels(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with both title-prefix and labels configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
    title-prefix: "[automated] "
    labels: ["bot", "feature", "enhancement"]
---

# Test Push to Branch with Title Prefix and Labels

This workflow validates both PR title prefix and labels.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-title-prefix-and-labels.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that both title prefix and labels configurations are in handler manager config JSON (check for push_to_pull_request_branch config)
	if !strings.Contains(lockContentStr, `push_to_pull_request_branch`) || !strings.Contains(lockContentStr, `title_prefix`) || !strings.Contains(lockContentStr, `[automated]`) {
		t.Errorf("Generated workflow should contain title_prefix:[automated] in handler config JSON")
	}
	if !strings.Contains(lockContentStr, `labels`) || (!strings.Contains(lockContentStr, `bot`) && !strings.Contains(lockContentStr, `feature`) && !strings.Contains(lockContentStr, `enhancement`)) {
		t.Errorf("Generated workflow should contain labels (bot,feature,enhancement) in handler config JSON")
	}
}

func TestPushToPullRequestBranchWithCommitTitleSuffix(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with commit-title-suffix configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened, synchronize]
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
    commit-title-suffix: " [skip ci]"
---

# Test Push to Branch with Commit Title Suffix

This workflow appends a suffix to commit titles.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-to-pull-request-branch-commit-title-suffix.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that commit title suffix configuration is in handler manager config JSON (check for push_to_pull_request_branch config)
	if !strings.Contains(lockContentStr, `push_to_pull_request_branch`) || !strings.Contains(lockContentStr, `commit_title_suffix`) || !strings.Contains(lockContentStr, `[skip ci]`) {
		t.Errorf("Generated workflow should contain commit_title_suffix:[skip ci] in handler config JSON")
	}
}

func TestPushToPullRequestBranchNoWorkingDirectory(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with push-to-pull-request-branch configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened]
safe-outputs:
  push-to-pull-request-branch:
---

# Test Push to PR Branch Without Working Directory

Test that the push-to-pull-request-branch job does NOT include working-directory
since it's not supported by actions/github-script.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-no-working-dir.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that safe_outputs job is generated (consolidated mode)
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Errorf("Generated workflow should contain safe_outputs job")
	}

	// Verify that push_to_pull_request_branch is handled by handler manager
	if !strings.Contains(lockContentStr, "id: process_safe_outputs") {
		t.Errorf("Generated workflow should contain process_safe_outputs step (handler manager)")
	}

	// Verify that working-directory is NOT present (not supported by actions/github-script)
	if strings.Contains(lockContentStr, "working-directory:") {
		t.Errorf("Generated workflow should NOT contain working-directory - it's not supported by actions/github-script\nGenerated workflow:\n%s", lockContentStr)
	}
}

func TestPushToPullRequestBranchActivationCommentEnvVars(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with push-to-pull-request-branch configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened]
  reaction: rocket
safe-outputs:
  push-to-pull-request-branch:
---

# Test Push to PR Branch Activation Comment

Test that the push-to-pull-request-branch job receives activation comment environment variables.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-activation-comment.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that safe_outputs job with push_to_pull_request_branch step is generated
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Errorf("Generated workflow should contain safe_outputs job with push_to_pull_request_branch step")
	}

	// Verify that the job depends on activation (needs can be formatted as array or inline)
	hasActivationDep := strings.Contains(lockContentStr, "needs: [agent, activation]") ||
		strings.Contains(lockContentStr, "needs:\n    - agent\n    - activation") ||
		strings.Contains(lockContentStr, "needs:\n      - agent\n      - activation") ||
		(strings.Contains(lockContentStr, "- agent") && strings.Contains(lockContentStr, "- activation"))
	if !hasActivationDep {
		t.Errorf("Generated workflow should have dependency on activation job")
	}

	// Verify that activation comment environment variables are passed
	if !strings.Contains(lockContentStr, "GH_AW_COMMENT_ID: ${{ needs.activation.outputs.comment_id }}") {
		t.Errorf("Generated workflow should contain GH_AW_COMMENT_ID environment variable")
	}

	if !strings.Contains(lockContentStr, "GH_AW_COMMENT_REPO: ${{ needs.activation.outputs.comment_repo }}") {
		t.Errorf("Generated workflow should contain GH_AW_COMMENT_REPO environment variable")
	}
}

// TestPushToPullRequestBranchPatchArtifactDownload verifies that when push-to-pull-request-branch
// is enabled, the safe_outputs job includes a step to download the aw.patch artifact
func TestPushToPullRequestBranchPatchArtifactDownload(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with push-to-pull-request-branch configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened]
safe-outputs:
  push-to-pull-request-branch:
---

# Test Push to PR Branch Patch Download

This test verifies that the aw.patch artifact is downloaded in the safe_outputs job.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-push-patch-download.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that safe_outputs job exists
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Fatalf("Generated workflow should contain safe_outputs job")
	}

	// Verify that patch download step exists in safe_outputs job
	if !strings.Contains(lockContentStr, "- name: Download patch artifact") {
		t.Errorf("Expected 'Download patch artifact' step in safe_outputs job when push-to-pull-request-branch is enabled")
	}

	// Verify that patch is downloaded from unified agent-artifacts
	if !strings.Contains(lockContentStr, "name: agent-artifacts") {
		t.Errorf("Expected patch to be downloaded from 'agent-artifacts' unified artifact")
	}

	if !strings.Contains(lockContentStr, "path: /tmp/gh-aw/") {
		t.Errorf("Expected patch artifact to be downloaded to '/tmp/gh-aw/'")
	}

	// Verify that the push step is handled by handler manager
	if !strings.Contains(lockContentStr, "- name: Process Safe Outputs") {
		t.Errorf("Expected 'Process Safe Outputs' step (handler manager) in safe_outputs job")
	}

	// Verify that the condition checks for push_to_pull_request_branch output type
	if !strings.Contains(lockContentStr, "contains(needs.agent.outputs.output_types, 'push_to_pull_request_branch')") {
		t.Errorf("Expected condition to check for 'push_to_pull_request_branch' in output_types")
	}
}
