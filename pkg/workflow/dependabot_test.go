//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/testutil"

	"github.com/goccy/go-yaml"
)

func TestParseNpmPackage(t *testing.T) {
	tests := []struct {
		name            string
		pkg             string
		expectedName    string
		expectedVersion string
	}{
		{
			name:            "scoped package with version",
			pkg:             "@playwright/mcp@latest",
			expectedName:    "@playwright/mcp",
			expectedVersion: "latest",
		},
		{
			name:            "scoped package with specific version",
			pkg:             "@playwright/mcp@1.2.3",
			expectedName:    "@playwright/mcp",
			expectedVersion: "1.2.3",
		},
		{
			name:            "scoped package without version",
			pkg:             "@playwright/mcp",
			expectedName:    "@playwright/mcp",
			expectedVersion: "latest",
		},
		{
			name:            "non-scoped package with version",
			pkg:             "playwright@1.0.0",
			expectedName:    "playwright",
			expectedVersion: "1.0.0",
		},
		{
			name:            "non-scoped package without version",
			pkg:             "playwright",
			expectedName:    "playwright",
			expectedVersion: "latest",
		},
		{
			name:            "package with semver range",
			pkg:             "react@^18.0.0",
			expectedName:    "react",
			expectedVersion: "^18.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := parseNpmPackage(tt.pkg)
			if dep.Name != tt.expectedName {
				t.Errorf("expected name %q, got %q", tt.expectedName, dep.Name)
			}
			if dep.Version != tt.expectedVersion {
				t.Errorf("expected version %q, got %q", tt.expectedVersion, dep.Version)
			}
		})
	}
}

func TestCollectNpmDependencies(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name         string
		workflows    []*WorkflowData
		expectedDeps []NpmDependency
	}{
		{
			name: "single workflow with npm dependencies",
			workflows: []*WorkflowData{
				{
					CustomSteps: "npx @playwright/mcp@latest",
				},
			},
			expectedDeps: []NpmDependency{
				{Name: "@playwright/mcp", Version: "latest"},
			},
		},
		{
			name: "multiple workflows with different dependencies",
			workflows: []*WorkflowData{
				{
					CustomSteps: "npx @playwright/mcp@latest",
				},
				{
					CustomSteps: "npx typescript@5.0.0",
				},
			},
			expectedDeps: []NpmDependency{
				{Name: "@playwright/mcp", Version: "latest"},
				{Name: "typescript", Version: "5.0.0"},
			},
		},
		{
			name: "duplicate dependencies use last version",
			workflows: []*WorkflowData{
				{
					CustomSteps: "npx typescript@4.0.0",
				},
				{
					CustomSteps: "npx typescript@5.0.0",
				},
			},
			expectedDeps: []NpmDependency{
				{Name: "typescript", Version: "5.0.0"},
			},
		},
		{
			name:         "no npm dependencies",
			workflows:    []*WorkflowData{},
			expectedDeps: []NpmDependency{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := compiler.collectNpmDependencies(tt.workflows)
			if len(deps) != len(tt.expectedDeps) {
				t.Errorf("expected %d dependencies, got %d", len(tt.expectedDeps), len(deps))
			}
			for i, dep := range deps {
				if i >= len(tt.expectedDeps) {
					break
				}
				expected := tt.expectedDeps[i]
				if dep.Name != expected.Name {
					t.Errorf("dependency %d: expected name %q, got %q", i, expected.Name, dep.Name)
				}
				if dep.Version != expected.Version {
					t.Errorf("dependency %d: expected version %q, got %q", i, expected.Version, dep.Version)
				}
			}
		})
	}
}

func TestGeneratePackageJSON(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	packageJSONPath := filepath.Join(tempDir, "package.json")

	deps := []NpmDependency{
		{Name: "@playwright/mcp", Version: "latest"},
		{Name: "typescript", Version: "5.0.0"},
	}

	// Test creating new package.json
	err := compiler.generatePackageJSON(packageJSONPath, deps, false)
	if err != nil {
		t.Fatalf("failed to generate package.json: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(packageJSONPath); os.IsNotExist(err) {
		t.Fatal("package.json was not created")
	}

	// Read and verify content
	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		t.Fatalf("failed to read package.json: %v", err)
	}

	var pkgJSON PackageJSON
	if err := json.Unmarshal(data, &pkgJSON); err != nil {
		t.Fatalf("failed to parse package.json: %v", err)
	}

	// Verify structure
	if pkgJSON.Name != "gh-aw-workflows-deps" {
		t.Errorf("expected name 'gh-aw-workflows-deps', got %q", pkgJSON.Name)
	}
	if !pkgJSON.Private {
		t.Error("expected private to be true")
	}
	if len(pkgJSON.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(pkgJSON.Dependencies))
	}

	// Verify dependencies
	if pkgJSON.Dependencies["@playwright/mcp"] != "latest" {
		t.Errorf("expected @playwright/mcp@latest, got %q", pkgJSON.Dependencies["@playwright/mcp"])
	}
	if pkgJSON.Dependencies["typescript"] != "5.0.0" {
		t.Errorf("expected typescript@5.0.0, got %q", pkgJSON.Dependencies["typescript"])
	}
}

func TestGeneratePackageJSON_MergeExisting(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	packageJSONPath := filepath.Join(tempDir, "package.json")

	// Create existing package.json with some fields
	existingPkg := PackageJSON{
		Name:    "my-custom-name",
		Private: true,
		License: "Apache-2.0",
		Dependencies: map[string]string{
			"lodash": "^4.17.21",
		},
	}
	existingData, _ := json.MarshalIndent(existingPkg, "", "  ")
	os.WriteFile(packageJSONPath, existingData, 0644)

	// Generate with new dependencies
	newDeps := []NpmDependency{
		{Name: "@playwright/mcp", Version: "latest"},
	}

	err := compiler.generatePackageJSON(packageJSONPath, newDeps, false)
	if err != nil {
		t.Fatalf("failed to merge package.json: %v", err)
	}

	// Read and verify merged content
	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		t.Fatalf("failed to read package.json: %v", err)
	}

	var pkgJSON PackageJSON
	if err := json.Unmarshal(data, &pkgJSON); err != nil {
		t.Fatalf("failed to parse package.json: %v", err)
	}

	// Verify existing fields were preserved
	if pkgJSON.Name != "my-custom-name" {
		t.Errorf("expected name 'my-custom-name', got %q", pkgJSON.Name)
	}
	if pkgJSON.License != "Apache-2.0" {
		t.Errorf("expected license 'Apache-2.0', got %q", pkgJSON.License)
	}

	// Verify dependencies were merged
	if len(pkgJSON.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(pkgJSON.Dependencies))
	}
	if pkgJSON.Dependencies["lodash"] != "^4.17.21" {
		t.Error("existing lodash dependency should be preserved")
	}
	if pkgJSON.Dependencies["@playwright/mcp"] != "latest" {
		t.Error("new @playwright/mcp dependency should be added")
	}
}

func TestGenerateDependabotConfig(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	ecosystems := map[string]struct{}{"npm": {}}

	// Test creating new dependabot.yml
	err := compiler.generateDependabotConfig(dependabotPath, ecosystems, false)
	if err != nil {
		t.Fatalf("failed to generate dependabot.yml: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(dependabotPath); os.IsNotExist(err) {
		t.Fatal("dependabot.yml was not created")
	}

	// Read and verify content
	data, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read dependabot.yml: %v", err)
	}

	var config DependabotConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse dependabot.yml: %v", err)
	}

	// Verify structure
	if config.Version != 2 {
		t.Errorf("expected version 2, got %d", config.Version)
	}
	if len(config.Updates) != 1 {
		t.Fatalf("expected 1 update entry, got %d", len(config.Updates))
	}

	update := config.Updates[0]
	if update.PackageEcosystem != "npm" {
		t.Errorf("expected package-ecosystem 'npm', got %q", update.PackageEcosystem)
	}
	if update.Directory != "/.github/workflows" {
		t.Errorf("expected directory '/.github/workflows', got %q", update.Directory)
	}
	if update.Schedule.Interval != "weekly" {
		t.Errorf("expected interval 'weekly', got %q", update.Schedule.Interval)
	}
}

func TestGenerateDependabotConfig_PreserveExisting(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	// Create existing dependabot.yml with npm entry
	existingConfig := DependabotConfig{
		Version: 2,
		Updates: []DependabotUpdateEntry{
			{
				PackageEcosystem: "npm",
				Directory:        "/.github/workflows",
			},
		},
	}
	existingConfig.Updates[0].Schedule.Interval = "weekly"
	existingData, _ := yaml.Marshal(&existingConfig)
	os.WriteFile(dependabotPath, existingData, 0644)

	ecosystems := map[string]struct{}{"npm": {}}

	// Try to generate without force - should preserve
	err := compiler.generateDependabotConfig(dependabotPath, ecosystems, false)
	if err != nil {
		t.Fatalf("failed to check existing dependabot.yml: %v", err)
	}

	// Verify file was preserved (no error means it was skipped)
	data, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read dependabot.yml: %v", err)
	}
	var config DependabotConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to unmarshal dependabot.yml: %v", err)
	}
	if len(config.Updates) != 1 {
		t.Error("existing config should be preserved without force flag")
	}
}

func TestReconcileManagedDependabotIgnores_NoDependabotFile(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	err := compiler.ReconcileManagedDependabotIgnores(dependabotPath)
	if err != nil {
		t.Fatalf("expected no error when dependabot.yml is missing, got: %v", err)
	}

	if _, statErr := os.Stat(dependabotPath); !os.IsNotExist(statErr) {
		t.Fatal("dependabot.yml should not be created when missing")
	}
}

func TestDependabotConfigPath(t *testing.T) {
	root := "/path/to/repo"
	expected := filepath.Join(root, ".github", "dependabot.yml")
	if actual := DependabotConfigPath(root); actual != expected {
		t.Fatalf("expected dependabot path %q, got %q", expected, actual)
	}
}

func TestReconcileManagedDependabotIgnoresInRepo(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	githubDir := filepath.Join(tempDir, ".github")
	if err := os.MkdirAll(githubDir, 0755); err != nil {
		t.Fatalf("failed to create .github directory: %v", err)
	}

	dependabotPath := filepath.Join(githubDir, "dependabot.yml")
	original := `version: 2
updates:
  - package-ecosystem: github-actions
    directory: "/.github/workflows"
    schedule:
      interval: weekly
    ignore:
      - dependency-name: "actions/checkout"
`
	if err := os.WriteFile(dependabotPath, []byte(original), 0644); err != nil {
		t.Fatalf("failed to write test dependabot.yml: %v", err)
	}

	err := compiler.ReconcileManagedDependabotIgnoresInRepo(tempDir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read updated dependabot.yml: %v", err)
	}

	updatedStr := string(updated)
	if !strings.Contains(updatedStr, `dependency-name: "github/gh-aw-actions/*"`) {
		t.Fatal("managed github/gh-aw-actions wildcard ignore entry should be added")
	}
}

func TestReconcileManagedDependabotIgnores_NoGitHubActionsUpdate(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	original := `version: 2
updates:
  - package-ecosystem: npm
    directory: "/.github/workflows"
    schedule:
      interval: weekly
`
	if err := os.WriteFile(dependabotPath, []byte(original), 0644); err != nil {
		t.Fatalf("failed to write test dependabot.yml: %v", err)
	}

	err := compiler.ReconcileManagedDependabotIgnores(dependabotPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read updated dependabot.yml: %v", err)
	}
	if string(updated) != original {
		t.Fatal("dependabot.yml should be unchanged when github-actions updates are absent")
	}
}

func TestReconcileManagedDependabotIgnores_AddsManagedEntry(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	original := `version: 2
updates:
  - package-ecosystem: github-actions
    directory: "/.github/workflows"
    schedule:
      interval: weekly
    ignore:
      - dependency-name: "actions/checkout"
`
	if err := os.WriteFile(dependabotPath, []byte(original), 0644); err != nil {
		t.Fatalf("failed to write test dependabot.yml: %v", err)
	}

	err := compiler.ReconcileManagedDependabotIgnores(dependabotPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read updated dependabot.yml: %v", err)
	}

	updatedStr := string(updated)
	if !strings.Contains(updatedStr, `dependency-name: "actions/checkout"`) {
		t.Fatal("user-defined ignore entry should be preserved")
	}
	if !strings.Contains(updatedStr, `dependency-name: "github/gh-aw-actions/*"`) {
		t.Fatal("managed github/gh-aw-actions wildcard ignore entry should be added")
	}
	if !strings.Contains(updatedStr, managedDependabotIgnoreComment) {
		t.Fatal("managed ignore entry should include the compiler-managed inline comment")
	}
}

func TestReconcileManagedDependabotIgnores_ReplacesNullIgnoreWithManagedEntry(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	original := `version: 2
updates:
  - package-ecosystem: github-actions
    directory: "/.github/workflows"
    schedule:
      interval: weekly
    ignore:
`
	if err := os.WriteFile(dependabotPath, []byte(original), 0644); err != nil {
		t.Fatalf("failed to write test dependabot.yml: %v", err)
	}

	err := compiler.ReconcileManagedDependabotIgnores(dependabotPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read updated dependabot.yml: %v", err)
	}

	updatedStr := string(updated)
	if !strings.Contains(updatedStr, "ignore:") {
		t.Fatal("ignore block should still be present")
	}
	if !strings.Contains(updatedStr, `dependency-name: "github/gh-aw-actions/*"`) {
		t.Fatal("managed github/gh-aw-actions wildcard ignore entry should be added when ignore is null")
	}
}

func TestReconcileManagedDependabotIgnores_DoesNotDuplicateExistingWildcard(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	original := `version: 2
updates:
  - package-ecosystem: github-actions
    directory: "/.github/workflows"
    schedule:
      interval: weekly
    ignore:
      - dependency-name: "github/gh-aw-actions/*" # Managed by gh aw compile. Version-locked to the gh-aw compiler; do not bump.
`
	if err := os.WriteFile(dependabotPath, []byte(original), 0644); err != nil {
		t.Fatalf("failed to write test dependabot.yml: %v", err)
	}

	if err := compiler.ReconcileManagedDependabotIgnores(dependabotPath); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read updated dependabot.yml: %v", err)
	}
	if string(updated) != original {
		t.Fatal("existing compiler-managed wildcard ignore entry should not be duplicated or rewritten")
	}
}

func TestReconcileManagedDependabotIgnores_UsesCustomActionsRepoWildcard(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetActionsRepo("owner/repo")
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	original := `version: 2
updates:
  - package-ecosystem: github-actions
    directory: "/.github/workflows"
    schedule:
      interval: weekly
`
	if err := os.WriteFile(dependabotPath, []byte(original), 0644); err != nil {
		t.Fatalf("failed to write test dependabot.yml: %v", err)
	}

	if err := compiler.ReconcileManagedDependabotIgnores(dependabotPath); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read updated dependabot.yml: %v", err)
	}
	if !strings.Contains(string(updated), `dependency-name: "owner/repo/*"`) {
		t.Fatal("custom actions repository wildcard ignore entry should be added")
	}
}

func TestReconcileManagedDependabotIgnores_PreservesUserAuthoredExactPattern(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	original := `version: 2
updates:
  - package-ecosystem: github-actions
    directory: "/.github/workflows"
    schedule:
      interval: weekly
    ignore:
      - dependency-name: "github/gh-aw-actions"
`
	if err := os.WriteFile(dependabotPath, []byte(original), 0644); err != nil {
		t.Fatalf("failed to write test dependabot.yml: %v", err)
	}

	if err := compiler.ReconcileManagedDependabotIgnores(dependabotPath); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read updated dependabot.yml: %v", err)
	}
	updatedStr := string(updated)
	if !strings.Contains(updatedStr, `dependency-name: "github/gh-aw-actions"`+"\n") {
		t.Fatal("user-authored exact ignore entry should be preserved")
	}
	if !strings.Contains(updatedStr, `dependency-name: "github/gh-aw-actions/*"`) {
		t.Fatal("managed wildcard ignore entry should be added alongside user-authored exact entry")
	}
}

func TestGenerateDependabotManifests_NoDependencies(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")

	// Workflow with no npm dependencies
	workflows := []*WorkflowData{
		{
			CustomSteps: "echo 'hello world'",
		},
	}

	err := compiler.GenerateDependabotManifests(context.Background(), workflows, tempDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no files were created
	packageJSONPath := filepath.Join(tempDir, "package.json")
	if _, err := os.Stat(packageJSONPath); !os.IsNotExist(err) {
		t.Error("package.json should not be created when there are no dependencies")
	}
}

// setupFakeFailingNpm puts a fake "npm" binary on PATH for the duration of the test
// that always fails immediately, instead of letting tests hit the real npm registry
// over the network, which is slow and unreliable in offline/sandboxed CI.
func setupFakeFailingNpm(t *testing.T) {
	t.Helper()
	fakeBinDir := testutil.TempDir(t, "fake-bin-*")
	fakeNpm := filepath.Join(fakeBinDir, "npm")
	script := "#!/bin/sh\necho 'simulated npm failure' >&2\nexit 1\n"
	if err := os.WriteFile(fakeNpm, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake npm binary: %v", err)
	}
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestGenerateDependabotManifests_WithDependencies(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	os.MkdirAll(workflowDir, 0755)
	fakeBinDir := testutil.TempDir(t, "fake-bin-*")
	fakeNpm := filepath.Join(fakeBinDir, "npm")
	if err := os.WriteFile(fakeNpm, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("failed to write fake npm binary: %v", err)
	}
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	setupFakeFailingNpm(t)

	// Workflow with npm dependencies
	workflows := []*WorkflowData{
		{
			CustomSteps: "npx @playwright/mcp@latest",
		},
	}

	// Note: This will fail npm install, but we can test the package.json generation
	_ = compiler.GenerateDependabotManifests(context.Background(), workflows, workflowDir, false)

	// In non-strict mode, npm failure is just a warning
	// Check that package.json was created
	packageJSONPath := filepath.Join(workflowDir, "package.json")
	if _, err := os.Stat(packageJSONPath); os.IsNotExist(err) {
		t.Error("package.json should be created even if npm install fails in non-strict mode")
	}

	// Verify package.json content
	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		t.Fatalf("failed to read package.json: %v", err)
	}
	var pkgJSON PackageJSON
	if err := json.Unmarshal(data, &pkgJSON); err != nil {
		t.Fatalf("failed to unmarshal package.json: %v", err)
	}

	if len(pkgJSON.Dependencies) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(pkgJSON.Dependencies))
	}
	if pkgJSON.Dependencies["@playwright/mcp"] != "latest" {
		t.Error("@playwright/mcp dependency should be present")
	}
}

func TestGenerateDependabotManifests_StrictMode(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	tempDir := testutil.TempDir(t, "test-*")
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	os.MkdirAll(workflowDir, 0755)

	setupFakeFailingNpm(t)

	// Workflow with npm dependencies
	workflows := []*WorkflowData{
		{
			CustomSteps: "npx @playwright/mcp@latest",
		},
	}

	// In strict mode, npm failure should cause an error
	strictErr := compiler.GenerateDependabotManifests(context.Background(), workflows, workflowDir, false)

	// We expect an error in strict mode since the fake npm always fails
	if strictErr == nil {
		t.Error("expected an error in strict mode when npm install fails")
	}
}

func TestGeneratePackageLock_DisablesNpmScripts(t *testing.T) {
	compiler := NewCompiler()
	workflowDir := testutil.TempDir(t, "workflow-*")
	fakeBinDir := testutil.TempDir(t, "fake-bin-*")

	argsFile := filepath.Join(workflowDir, "npm-args.txt")
	envFile := filepath.Join(workflowDir, "npm-ignore-scripts-env.txt")

	fakeNpm := filepath.Join(fakeBinDir, "npm")
	script := `#!/bin/sh
printf "%s\n" "$@" > "$GH_AW_TEST_ARGS_FILE"
printf "%s" "$NPM_CONFIG_IGNORE_SCRIPTS" > "$GH_AW_TEST_ENV_FILE"
touch package-lock.json
`
	if err := os.WriteFile(fakeNpm, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake npm binary: %v", err)
	}

	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_AW_TEST_ARGS_FILE", argsFile)
	t.Setenv("GH_AW_TEST_ENV_FILE", envFile)

	if err := compiler.generatePackageLock(context.Background(), workflowDir); err != nil {
		t.Fatalf("generatePackageLock() error = %v", err)
	}

	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("failed to read recorded npm args: %v", err)
	}
	args := string(argsData)
	if !strings.Contains(args, "install\n") {
		t.Fatalf("expected npm args to contain install, got: %q", args)
	}
	if !strings.Contains(args, "--package-lock-only\n") {
		t.Fatalf("expected npm args to contain --package-lock-only, got: %q", args)
	}
	if !strings.Contains(args, "--ignore-scripts\n") {
		t.Fatalf("expected npm args to contain --ignore-scripts, got: %q", args)
	}

	envData, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("failed to read recorded NPM_CONFIG_IGNORE_SCRIPTS: %v", err)
	}
	if string(envData) != "true" {
		t.Fatalf("expected NPM_CONFIG_IGNORE_SCRIPTS=true, got: %q", string(envData))
	}
}

func TestGeneratePackageLock_UsesNormalizedWorkflowDir(t *testing.T) {
	compiler := NewCompiler()
	parentDir := testutil.TempDir(t, "parent-*")
	workflowDir := filepath.Join(parentDir, "workflow")
	fakeBinDir := testutil.TempDir(t, "fake-bin-*")

	if err := os.Mkdir(workflowDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow directory: %v", err)
	}

	pwdFile := filepath.Join(parentDir, "npm-pwd.txt")
	fakeNpm := filepath.Join(fakeBinDir, "npm")
	script := `#!/bin/sh
pwd > "$GH_AW_TEST_PWD_FILE"
touch package-lock.json
`
	if err := os.WriteFile(fakeNpm, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake npm binary: %v", err)
	}

	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_AW_TEST_PWD_FILE", pwdFile)
	t.Chdir(parentDir)

	if err := compiler.generatePackageLock(context.Background(), "workflow"); err != nil {
		t.Fatalf("generatePackageLock() error = %v", err)
	}

	pwdData, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("failed to read recorded npm working directory: %v", err)
	}
	if strings.TrimSpace(string(pwdData)) != workflowDir {
		t.Fatalf("expected npm to run in %q, got %q", workflowDir, strings.TrimSpace(string(pwdData)))
	}
	if _, err := os.Stat(filepath.Join(workflowDir, "package-lock.json")); err != nil {
		t.Fatalf("expected package-lock.json in normalized workflow directory: %v", err)
	}
}

func TestGeneratePackageLock_RejectsInvalidWorkflowDir(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name        string
		workflowDir string
	}{
		{name: "empty", workflowDir: ""},
		{name: "whitespace", workflowDir: "   "},
		{name: "control character", workflowDir: "bad\nworkflow-dir"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := compiler.generatePackageLock(context.Background(), tt.workflowDir)
			if err == nil {
				t.Fatal("expected error for invalid workflow directory")
			}
			if !strings.Contains(err.Error(), "invalid workflow directory") {
				t.Fatalf("expected invalid workflow directory error, got: %v", err)
			}
		})
	}
}

// Tests for Python (pip) support

func TestParsePipPackage(t *testing.T) {
	tests := []struct {
		name            string
		pkg             string
		expectedName    string
		expectedVersion string
	}{
		{
			name:            "package with == version",
			pkg:             "requests==2.28.0",
			expectedName:    "requests",
			expectedVersion: "==2.28.0",
		},
		{
			name:            "package with >= version",
			pkg:             "django>=3.2.0",
			expectedName:    "django",
			expectedVersion: ">=3.2.0",
		},
		{
			name:            "package with ~= version",
			pkg:             "flask~=2.0.0",
			expectedName:    "flask",
			expectedVersion: "~=2.0.0",
		},
		{
			name:            "package without version",
			pkg:             "numpy",
			expectedName:    "numpy",
			expectedVersion: "",
		},
		{
			name:            "package with != version",
			pkg:             "pytest!=7.0.0",
			expectedName:    "pytest",
			expectedVersion: "!=7.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := parsePipPackage(tt.pkg)
			if dep.Name != tt.expectedName {
				t.Errorf("expected name %q, got %q", tt.expectedName, dep.Name)
			}
			if dep.Version != tt.expectedVersion {
				t.Errorf("expected version %q, got %q", tt.expectedVersion, dep.Version)
			}
		})
	}
}

func TestCollectPipDependencies(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name         string
		workflows    []*WorkflowData
		expectedDeps []PipDependency
	}{
		{
			name: "single workflow with pip dependencies",
			workflows: []*WorkflowData{
				{
					CustomSteps: "pip install requests==2.28.0",
				},
			},
			expectedDeps: []PipDependency{
				{Name: "requests", Version: "==2.28.0"},
			},
		},
		{
			name: "multiple workflows with different dependencies",
			workflows: []*WorkflowData{
				{
					CustomSteps: "pip install requests==2.28.0",
				},
				{
					CustomSteps: "pip3 install django>=3.2.0",
				},
			},
			expectedDeps: []PipDependency{
				{Name: "django", Version: ">=3.2.0"},
				{Name: "requests", Version: "==2.28.0"},
			},
		},
		{
			name: "duplicate dependencies use last version",
			workflows: []*WorkflowData{
				{
					CustomSteps: "pip install requests==2.27.0",
				},
				{
					CustomSteps: "pip install requests==2.28.0",
				},
			},
			expectedDeps: []PipDependency{
				{Name: "requests", Version: "==2.28.0"},
			},
		},
		{
			name:         "no pip dependencies",
			workflows:    []*WorkflowData{},
			expectedDeps: []PipDependency{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := compiler.collectPipDependencies(tt.workflows)
			if len(deps) != len(tt.expectedDeps) {
				t.Errorf("expected %d dependencies, got %d", len(tt.expectedDeps), len(deps))
			}
			for i, dep := range deps {
				if i >= len(tt.expectedDeps) {
					break
				}
				expected := tt.expectedDeps[i]
				if dep.Name != expected.Name {
					t.Errorf("dependency %d: expected name %q, got %q", i, expected.Name, dep.Name)
				}
				if dep.Version != expected.Version {
					t.Errorf("dependency %d: expected version %q, got %q", i, expected.Version, dep.Version)
				}
			}
		})
	}
}

func TestGenerateRequirementsTxt(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	requirementsPath := filepath.Join(tempDir, "requirements.txt")

	deps := []PipDependency{
		{Name: "requests", Version: "==2.28.0"},
		{Name: "django", Version: ">=3.2.0"},
	}

	// Test creating new requirements.txt
	err := compiler.generateRequirementsTxt(requirementsPath, deps, false)
	if err != nil {
		t.Fatalf("failed to generate requirements.txt: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(requirementsPath); os.IsNotExist(err) {
		t.Fatal("requirements.txt was not created")
	}

	// Read and verify content
	data, err := os.ReadFile(requirementsPath)
	if err != nil {
		t.Fatalf("failed to read requirements.txt: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "django>=3.2.0") {
		t.Error("requirements.txt should contain django>=3.2.0")
	}
	if !strings.Contains(content, "requests==2.28.0") {
		t.Error("requirements.txt should contain requests==2.28.0")
	}
}

// Tests for Golang support

func TestParseGoPackage(t *testing.T) {
	tests := []struct {
		name            string
		pkg             string
		expectedPath    string
		expectedVersion string
	}{
		{
			name:            "package with version",
			pkg:             "github.com/user/repo@v1.2.3",
			expectedPath:    "github.com/user/repo",
			expectedVersion: "v1.2.3",
		},
		{
			name:            "package without version",
			pkg:             "github.com/user/repo",
			expectedPath:    "github.com/user/repo",
			expectedVersion: "latest",
		},
		{
			name:            "package with pseudo-version",
			pkg:             "golang.org/x/tools@v0.1.12-0.20220713141851-7464d2807d88",
			expectedPath:    "golang.org/x/tools",
			expectedVersion: "v0.1.12-0.20220713141851-7464d2807d88",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := parseGoPackage(tt.pkg)
			if dep.Path != tt.expectedPath {
				t.Errorf("expected path %q, got %q", tt.expectedPath, dep.Path)
			}
			if dep.Version != tt.expectedVersion {
				t.Errorf("expected version %q, got %q", tt.expectedVersion, dep.Version)
			}
		})
	}
}

func TestCollectGoDependencies(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name         string
		workflows    []*WorkflowData
		expectedDeps []GoDependency
	}{
		{
			name: "single workflow with go install",
			workflows: []*WorkflowData{
				{
					CustomSteps: "go install github.com/user/tool@v1.0.0",
				},
			},
			expectedDeps: []GoDependency{
				{Path: "github.com/user/tool", Version: "v1.0.0"},
			},
		},
		{
			name: "multiple workflows with different dependencies",
			workflows: []*WorkflowData{
				{
					CustomSteps: "go install github.com/user/tool@v1.0.0",
				},
				{
					CustomSteps: "go get golang.org/x/tools@latest",
				},
			},
			expectedDeps: []GoDependency{
				{Path: "github.com/user/tool", Version: "v1.0.0"},
				{Path: "golang.org/x/tools", Version: "latest"},
			},
		},
		{
			name: "duplicate dependencies use last version",
			workflows: []*WorkflowData{
				{
					CustomSteps: "go install github.com/user/tool@v1.0.0",
				},
				{
					CustomSteps: "go install github.com/user/tool@v2.0.0",
				},
			},
			expectedDeps: []GoDependency{
				{Path: "github.com/user/tool", Version: "v2.0.0"},
			},
		},
		{
			name:         "no go dependencies",
			workflows:    []*WorkflowData{},
			expectedDeps: []GoDependency{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := compiler.collectGoDependencies(tt.workflows)
			if len(deps) != len(tt.expectedDeps) {
				t.Errorf("expected %d dependencies, got %d", len(tt.expectedDeps), len(deps))
			}
			for i, dep := range deps {
				if i >= len(tt.expectedDeps) {
					break
				}
				expected := tt.expectedDeps[i]
				if dep.Path != expected.Path {
					t.Errorf("dependency %d: expected path %q, got %q", i, expected.Path, dep.Path)
				}
				if dep.Version != expected.Version {
					t.Errorf("dependency %d: expected version %q, got %q", i, expected.Version, dep.Version)
				}
			}
		})
	}
}

func TestGenerateGoMod(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	goModPath := filepath.Join(tempDir, "go.mod")

	deps := []GoDependency{
		{Path: "github.com/user/tool", Version: "v1.0.0"},
		{Path: "golang.org/x/tools", Version: "v0.1.0"},
	}

	// Test creating new go.mod
	err := compiler.generateGoMod(goModPath, deps, false)
	if err != nil {
		t.Fatalf("failed to generate go.mod: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		t.Fatal("go.mod was not created")
	}

	// Read and verify content
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "module github.com/github/gh-aw-workflows-deps") {
		t.Error("go.mod should contain module declaration")
	}
	if !strings.Contains(content, "require (") {
		t.Error("go.mod should contain require section")
	}
	if !strings.Contains(content, "github.com/user/tool v1.0.0") {
		t.Error("go.mod should contain github.com/user/tool v1.0.0")
	}
	if !strings.Contains(content, "golang.org/x/tools v0.1.0") {
		t.Error("go.mod should contain golang.org/x/tools v0.1.0")
	}
}

func TestGenerateGoMod_SkipsEmptyRequireBlock(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	goModPath := filepath.Join(tempDir, "go.mod")

	deps := []GoDependency{
		{Path: "github.com/user/tool", Version: "latest"},
		{Path: "golang.org/x/tools"},
	}

	if err := compiler.generateGoMod(goModPath, deps, false); err != nil {
		t.Fatalf("failed to generate go.mod: %v", err)
	}

	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "module github.com/github/gh-aw-workflows-deps") {
		t.Fatalf("go.mod should contain the generated module declaration:\n%s", content)
	}
	if strings.Contains(content, "require (") {
		t.Fatalf("go.mod should not contain a require block when all dependencies are skipped:\n%s", content)
	}
}

// Tests for multi-ecosystem support

func TestGenerateDependabotConfig_MultipleEcosystems(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	ecosystems := map[string]struct{}{
		"npm":   {},
		"pip":   {},
		"gomod": {},
	}

	// Test creating new dependabot.yml with multiple ecosystems
	err := compiler.generateDependabotConfig(dependabotPath, ecosystems, false)
	if err != nil {
		t.Fatalf("failed to generate dependabot.yml: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(dependabotPath); os.IsNotExist(err) {
		t.Fatal("dependabot.yml was not created")
	}

	// Read and verify content
	data, err := os.ReadFile(dependabotPath)
	if err != nil {
		t.Fatalf("failed to read dependabot.yml: %v", err)
	}

	var config DependabotConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse dependabot.yml: %v", err)
	}

	// Verify structure
	if config.Version != 2 {
		t.Errorf("expected version 2, got %d", config.Version)
	}
	if len(config.Updates) != 3 {
		t.Fatalf("expected 3 update entries, got %d", len(config.Updates))
	}

	// Check that all ecosystems are present
	ecosystemsFound := make(map[string]struct{})
	for _, update := range config.Updates {
		ecosystemsFound[update.PackageEcosystem] = struct{}{}
		if update.Directory != "/.github/workflows" {
			t.Errorf("expected directory '/.github/workflows', got %q", update.Directory)
		}
		if update.Schedule.Interval != "weekly" {
			t.Errorf("expected interval 'weekly', got %q", update.Schedule.Interval)
		}
	}

	for ecosystem := range ecosystems {
		if !setutil.Contains(ecosystemsFound, ecosystem) {
			t.Errorf("ecosystem %q not found in dependabot.yml", ecosystem)
		}
	}
}

func TestGenerateDependabotManifests_AllEcosystems(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	os.MkdirAll(workflowDir, 0755)

	setupFakeFailingNpm(t)

	// Workflow with npm, pip, and go dependencies
	workflows := []*WorkflowData{
		{
			CustomSteps: `
npx @playwright/mcp@latest
pip install requests==2.28.0
go install github.com/user/tool@v1.0.0
`,
		},
	}

	// This will fail npm install (fake npm always fails), but should still generate manifest files
	_ = compiler.GenerateDependabotManifests(context.Background(), workflows, workflowDir, false)

	// Check that package.json was created
	packageJSONPath := filepath.Join(workflowDir, "package.json")
	if _, err := os.Stat(packageJSONPath); os.IsNotExist(err) {
		t.Error("package.json should be created")
	}

	// Check that requirements.txt was created
	requirementsPath := filepath.Join(workflowDir, "requirements.txt")
	if _, err := os.Stat(requirementsPath); os.IsNotExist(err) {
		t.Error("requirements.txt should be created")
	}

	// Check that go.mod was created
	goModPath := filepath.Join(workflowDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		t.Error("go.mod should be created")
	}

	// Check dependabot.yml
	dependabotPath := filepath.Join(tempDir, ".github", "dependabot.yml")
	if _, err := os.Stat(dependabotPath); os.IsNotExist(err) {
		t.Error("dependabot.yml should be created")
	}
}

// Tests for extractGoFromCommands function

func TestExtractGoFromCommands(t *testing.T) {
	tests := []struct {
		name     string
		commands string
		want     []string
	}{
		{
			name:     "simple go install",
			commands: "go install github.com/user/tool@v1.0.0",
			want:     []string{"github.com/user/tool@v1.0.0"},
		},
		{
			name:     "go get",
			commands: "go get golang.org/x/tools@latest",
			want:     []string{"golang.org/x/tools@latest"},
		},
		{
			name: "mixed go install and go get",
			commands: `go install github.com/user/tool@v1.0.0
go get golang.org/x/lint@latest`,
			want: []string{"github.com/user/tool@v1.0.0", "golang.org/x/lint@latest"},
		},
		{
			name:     "go install with flags",
			commands: "go install -v github.com/user/tool",
			want:     []string{"github.com/user/tool"},
		},
		{
			name:     "go without install or get",
			commands: "go build main.go",
			want:     nil,
		},
		{
			name:     "go mod command (not extracted)",
			commands: "go mod tidy",
			want:     nil,
		},
		{
			name:     "empty command",
			commands: "",
			want:     nil,
		},
		{
			name:     "go get with flags",
			commands: "go get -u github.com/user/tool@latest",
			want:     []string{"github.com/user/tool@latest"},
		},
		{
			name: "multiple go install commands",
			commands: `go install github.com/tool1/pkg@v1.0.0
go install github.com/tool2/pkg@v2.0.0`,
			want: []string{"github.com/tool1/pkg@v1.0.0", "github.com/tool2/pkg@v2.0.0"},
		},
		{
			name:     "go install with trailing semicolon",
			commands: "go install github.com/user/tool@v1.0.0;",
			want:     []string{"github.com/user/tool@v1.0.0"},
		},
		{
			name:     "go get with trailing ampersand",
			commands: "go get github.com/user/tool@latest&",
			want:     []string{"github.com/user/tool@latest"},
		},
		{
			name:     "go install and go get on same line",
			commands: "go install github.com/tool1/pkg@v1.0.0 && go get github.com/tool2/pkg@latest",
			want:     []string{"github.com/tool1/pkg@v1.0.0", "github.com/tool2/pkg@latest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGoFromCommands(tt.commands)
			if len(got) != len(tt.want) {
				t.Errorf("extractGoFromCommands() = %v, want %v", got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("extractGoFromCommands()[%d] = %v, want %v", i, v, tt.want[i])
				}
			}
		})
	}
}
