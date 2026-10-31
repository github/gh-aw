//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
)

func TestCompileDependabotIntegration(t *testing.T) {

	// Check if npm is available and functional
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		t.Skip("Skipping test - npm not available")
	}
	// Test if npm actually works
	cmd := exec.Command(npmPath, "--version")
	if err := cmd.Run(); err != nil {
		t.Skipf("Skipping test - npm is not functional: %v", err)
	}

	// Create temp directory for test
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	// Initialize git repo
	initGitRepo(t, tempDir)

	// Create a test workflow with npm dependencies
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
steps:
  - run: npx @playwright/mcp@latest --help
---

# Test Workflow

This workflow uses npx to run Playwright MCP.
`
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	// Compile with Dependabot flag (compile all files, not specific ones)
	config := CompileConfig{
		MarkdownFiles:  nil, // Compile all markdown files
		Verbose:        true,
		Validate:       false, // Skip validation for faster test
		WorkflowDir:    ".github/workflows",
		Dependabot:     true,
		ForceOverwrite: false,
		Strict:         false,
	}

	workflowDataList, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	if len(workflowDataList) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(workflowDataList))
	}

	// Verify package.json was created
	packageJSONPath := filepath.Join(workflowsDir, "package.json")
	if _, err := os.Stat(packageJSONPath); os.IsNotExist(err) {
		t.Fatal("package.json was not created")
	}

	// Verify package.json content
	packageData, err := os.ReadFile(packageJSONPath)
	if err != nil {
		t.Fatalf("failed to read package.json: %v", err)
	}

	var pkgJSON workflow.PackageJSON
	if err := json.Unmarshal(packageData, &pkgJSON); err != nil {
		t.Fatalf("failed to parse package.json: %v", err)
	}

	if pkgJSON.Name != "gh-aw-workflows-deps" {
		t.Errorf("expected name 'gh-aw-workflows-deps', got %q", pkgJSON.Name)
	}

	if len(pkgJSON.Dependencies) == 0 {
		t.Error("expected at least one dependency (@playwright/mcp)")
	}

	// Verify package-lock.json was created
	packageLockPath := filepath.Join(workflowsDir, "package-lock.json")
	if _, err := os.Stat(packageLockPath); os.IsNotExist(err) {
		t.Error("package-lock.json was not created")
	}

	// Verify dependabot.yml was created
	dependabotPath := filepath.Join(tempDir, ".github", "dependabot.yml")
	if _, err := os.Stat(dependabotPath); os.IsNotExist(err) {
		t.Fatal("dependabot.yml was not created")
	}

	// Verify dependabot.yml content
	dependabotData, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read dependabot.yml: %v", err)
	}

	var dependabotConfig workflow.DependabotConfig
	if err := yaml.Unmarshal(dependabotData, &dependabotConfig); err != nil {
		t.Fatalf("failed to parse dependabot.yml: %v", err)
	}

	if dependabotConfig.Version != 2 {
		t.Errorf("expected version 2, got %d", dependabotConfig.Version)
	}

	npmFound := false
	for _, update := range dependabotConfig.Updates {
		if update.PackageEcosystem == "npm" && update.Directory == "/.github/workflows" {
			npmFound = true
			if update.Schedule.Interval != "weekly" {
				t.Errorf("expected interval 'weekly', got %q", update.Schedule.Interval)
			}
			break
		}
	}

	if !npmFound {
		t.Error("npm ecosystem not found in dependabot.yml")
	}
}

func TestCompileDependabotEngineOnly(t *testing.T) {
	// Keep this compiler regression independent of npm registry availability.
	binDir := testutil.TempDir(t, "npm-bin-*")
	if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/sh\nprintf '{\"lockfileVersion\":3}\\n' > package-lock.json\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Create temp directory for test
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tempDir)

	// Initialize git repo
	initGitRepo(t, tempDir)

	// Even without authored tools, the default engine is a dependency.
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow

This workflow only uses the default engine.
`
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	expressionWorkflow := `---
on: push
engine:
  id: codex
  version: "${{ vars.CODEX_VERSION }}"
---

# Dynamic Engine Version
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "dynamic-engine.md"), []byte(expressionWorkflow), 0644); err != nil {
		t.Fatalf("failed to write expression workflow: %v", err)
	}

	// Compile with Dependabot flag (compile all files, not specific ones)
	config := CompileConfig{
		MarkdownFiles:  nil, // Compile all markdown files
		Verbose:        true,
		Validate:       false,
		WorkflowDir:    ".github/workflows",
		Dependabot:     true,
		ForceOverwrite: false,
		Strict:         false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	// Verify the default engine's effective version is tracked.
	packageJSONPath := filepath.Join(workflowsDir, "package.json")
	packageData, err := os.ReadFile(packageJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var pkgJSON workflow.PackageJSON
	if err := json.Unmarshal(packageData, &pkgJSON); err != nil {
		t.Fatal(err)
	}
	if got := pkgJSON.Dependencies["@github/copilot"]; got != string(constants.DefaultCopilotVersion) {
		t.Errorf("expected default Copilot version %q, got %q", constants.DefaultCopilotVersion, got)
	}
	if got := pkgJSON.Dependencies["@openai/codex"]; got != "" {
		t.Errorf("expected GitHub Actions expression version to be omitted, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(workflowsDir, "package-lock.json")); err != nil {
		t.Errorf("package-lock.json was not created: %v", err)
	}

	dependabotPath := filepath.Join(tempDir, ".github", "dependabot.yml")
	if _, err := os.Stat(dependabotPath); err != nil {
		t.Errorf("dependabot.yml was not created: %v", err)
	}
}

func TestCompileDependabotPreserveExisting(t *testing.T) {
	// Create temp directory for test
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	githubDir := filepath.Join(tempDir, ".github")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tempDir)

	// Initialize git repo
	initGitRepo(t, tempDir)

	// Create existing dependabot.yml with custom config
	existingDependabot := workflow.DependabotConfig{
		Version: 2,
		Updates: []workflow.DependabotUpdateEntry{
			{
				PackageEcosystem: "pip",
				Directory:        "/",
			},
		},
	}
	existingDependabot.Updates[0].Schedule.Interval = "daily"

	dependabotPath := filepath.Join(githubDir, "dependabot.yml")
	dependabotData, _ := yaml.Marshal(&existingDependabot)
	if err := os.WriteFile(dependabotPath, dependabotData, 0644); err != nil {
		t.Fatalf("failed to write existing dependabot.yml: %v", err)
	}

	// Create a test workflow with npm dependencies
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
steps:
  - run: npx @playwright/mcp@latest --help
---

# Test Workflow
`
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	// Compile with Dependabot flag (without force, compile all files)
	config := CompileConfig{
		MarkdownFiles:  nil, // Compile all markdown files
		Verbose:        true,
		Validate:       false,
		WorkflowDir:    ".github/workflows",
		Dependabot:     true,
		ForceOverwrite: false, // Don't force overwrite
		Strict:         false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	// Verify existing dependabot.yml was merged (not just preserved)
	dependabotData, err = os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read dependabot.yml: %v", err)
	}

	var dependabotConfig workflow.DependabotConfig
	if err := yaml.Unmarshal(dependabotData, &dependabotConfig); err != nil {
		t.Fatalf("failed to parse dependabot.yml: %v", err)
	}

	// Should still have the original pip entry
	pipFound := false
	npmFound := false
	for _, update := range dependabotConfig.Updates {
		if update.PackageEcosystem == "pip" {
			pipFound = true
		}
		if update.PackageEcosystem == "npm" && update.Directory == "/.github/workflows" {
			npmFound = true
		}
	}

	if !pipFound {
		t.Error("existing pip ecosystem should be preserved")
	}

	if !npmFound {
		t.Error("npm ecosystem should be added to existing dependabot.yml")
	}

	// Verify we have both entries
	if len(dependabotConfig.Updates) != 2 {
		t.Errorf("expected 2 update entries (pip and npm), got %d", len(dependabotConfig.Updates))
	}
}

func TestCompileDependabotMergeExistingNpm(t *testing.T) {
	// Create temp directory for test
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	githubDir := filepath.Join(tempDir, ".github")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tempDir)

	// Initialize git repo
	initGitRepo(t, tempDir)

	// Create existing dependabot.yml with npm already configured
	existingDependabot := workflow.DependabotConfig{
		Version: 2,
		Updates: []workflow.DependabotUpdateEntry{
			{
				PackageEcosystem: "npm",
				Directory:        "/.github/workflows",
			},
			{
				PackageEcosystem: "pip",
				Directory:        "/",
			},
		},
	}
	existingDependabot.Updates[0].Schedule.Interval = "daily"
	existingDependabot.Updates[1].Schedule.Interval = "weekly"

	dependabotPath := filepath.Join(githubDir, "dependabot.yml")
	dependabotData, _ := yaml.Marshal(&existingDependabot)
	if err := os.WriteFile(dependabotPath, dependabotData, 0644); err != nil {
		t.Fatalf("failed to write existing dependabot.yml: %v", err)
	}

	// Create a test workflow with npm dependencies
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
steps:
  - run: npx @playwright/mcp@latest --help
---

# Test Workflow
`
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	// Compile with Dependabot flag
	config := CompileConfig{
		MarkdownFiles:  nil,
		Verbose:        true,
		Validate:       false,
		WorkflowDir:    ".github/workflows",
		Dependabot:     true,
		ForceOverwrite: false,
		Strict:         false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	// Verify existing dependabot.yml was not duplicated
	dependabotData, err = os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read dependabot.yml: %v", err)
	}

	var dependabotConfig workflow.DependabotConfig
	if err := yaml.Unmarshal(dependabotData, &dependabotConfig); err != nil {
		t.Fatalf("failed to parse dependabot.yml: %v", err)
	}

	// Should still have both entries, but not duplicate npm
	npmCount := 0
	pipFound := false
	for _, update := range dependabotConfig.Updates {
		if update.PackageEcosystem == "npm" && update.Directory == "/.github/workflows" {
			npmCount++
		}
		if update.PackageEcosystem == "pip" {
			pipFound = true
		}
	}

	if npmCount != 1 {
		t.Errorf("expected exactly 1 npm entry, got %d", npmCount)
	}

	if !pipFound {
		t.Error("existing pip ecosystem should be preserved")
	}

	// Verify we still have both entries and no duplicates
	if len(dependabotConfig.Updates) != 2 {
		t.Errorf("expected 2 update entries (npm and pip), got %d", len(dependabotConfig.Updates))
	}
}

// TestCompileWithoutDependabotFlagDoesNotTouchDependabotYML verifies that compiling
// without --dependabot does not modify an existing dependabot.yml file.
func TestCompileWithoutDependabotFlagDoesNotTouchDependabotYML(t *testing.T) {
	// Create temp directory for test
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	githubDir := filepath.Join(tempDir, ".github")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tempDir)

	// Initialize git repo
	initGitRepo(t, tempDir)

	// Write a dependabot.yml with comments that should be preserved
	dependabotContent := `# This comment must be preserved.
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
`
	dependabotPath := filepath.Join(githubDir, "dependabot.yml")
	if err := os.WriteFile(dependabotPath, []byte(dependabotContent), 0644); err != nil {
		t.Fatalf("failed to write existing dependabot.yml: %v", err)
	}

	// Create a minimal workflow file
	workflowContent := `---
on: push
permissions:
  contents: read
---

# Test Workflow
`
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	// Compile WITHOUT the Dependabot flag
	config := CompileConfig{
		MarkdownFiles:  nil,
		Verbose:        false,
		Validate:       false,
		WorkflowDir:    ".github/workflows",
		Dependabot:     false,
		ForceOverwrite: false,
		Strict:         false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	// Verify dependabot.yml was not modified
	actual, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read dependabot.yml: %v", err)
	}

	if string(actual) != dependabotContent {
		t.Errorf("dependabot.yml was modified without --dependabot flag:\ngot:\n%s\nwant:\n%s", string(actual), dependabotContent)
	}
}

// TestCompileSpecificMarkdownWithoutDependabotFlagDoesNotTouchDependabotYML verifies
// the specific-file compilation path also leaves dependabot.yml unchanged without --dependabot.
func TestCompileSpecificMarkdownWithoutDependabotFlagDoesNotTouchDependabotYML(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	githubDir := filepath.Join(tempDir, ".github")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("failed to create workflows directory: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	initGitRepo(t, tempDir)

	dependabotContent := `# This comment must be preserved.
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
`
	dependabotPath := filepath.Join(githubDir, "dependabot.yml")
	if err := os.WriteFile(dependabotPath, []byte(dependabotContent), 0644); err != nil {
		t.Fatalf("failed to write existing dependabot.yml: %v", err)
	}

	workflowContent := `---
on: push
permissions:
  contents: read
---

# Test Workflow
`
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	config := CompileConfig{
		MarkdownFiles:  []string{workflowPath},
		Verbose:        false,
		Validate:       false,
		WorkflowDir:    ".github/workflows",
		Dependabot:     false,
		ForceOverwrite: false,
		Strict:         false,
	}

	_, err = CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	actual, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read dependabot.yml: %v", err)
	}

	if string(actual) != dependabotContent {
		t.Errorf("dependabot.yml was modified without --dependabot flag:\ngot:\n%s\nwant:\n%s", string(actual), dependabotContent)
	}
}

// Helper function to initialize a git repo for testing
func initGitRepo(t *testing.T, dir string) {
	// Use exec to run git init to properly initialize the repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to initialize git repo: %v", err)
	}
}
