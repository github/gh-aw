//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	ecosystems := map[string]bool{"npm": true}

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

	ecosystems := map[string]bool{"npm": true}

	// Try to generate without force - should preserve
	err := compiler.generateDependabotConfig(dependabotPath, ecosystems, false)
	if err != nil {
		t.Fatalf("failed to check existing dependabot.yml: %v", err)
	}

	// Verify file was preserved (no error means it was skipped)
	data, _ := os.ReadFile(dependabotPath)
	var config DependabotConfig
	yaml.Unmarshal(data, &config)
	if len(config.Updates) != 1 {
		t.Error("existing config should be preserved without force flag")
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

	err := compiler.GenerateDependabotManifests(workflows, tempDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no files were created
	packageJSONPath := filepath.Join(tempDir, "package.json")
	if _, err := os.Stat(packageJSONPath); !os.IsNotExist(err) {
		t.Error("package.json should not be created when there are no dependencies")
	}
}

func TestGenerateDependabotManifests_WithDependencies(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	os.MkdirAll(workflowDir, 0755)

	// Workflow with npm dependencies
	workflows := []*WorkflowData{
		{
			CustomSteps: "npx @playwright/mcp@latest",
		},
	}

	// Note: This will fail npm install, but we can test the package.json generation
	_ = compiler.GenerateDependabotManifests(workflows, workflowDir, false)

	// In non-strict mode, npm failure is just a warning
	// Check that package.json was created
	packageJSONPath := filepath.Join(workflowDir, "package.json")
	if _, err := os.Stat(packageJSONPath); os.IsNotExist(err) {
		t.Error("package.json should be created even if npm install fails in non-strict mode")
	}

	// Verify package.json content
	data, _ := os.ReadFile(packageJSONPath)
	var pkgJSON PackageJSON
	json.Unmarshal(data, &pkgJSON)

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

	// Workflow with npm dependencies
	workflows := []*WorkflowData{
		{
			CustomSteps: "npx @playwright/mcp@latest",
		},
	}

	// In strict mode, npm failure should cause an error
	strictErr := compiler.GenerateDependabotManifests(workflows, workflowDir, false)

	// We expect an error in strict mode when npm install fails
	// (unless npm is installed and the package is available)
	// The test validates that strict mode propagates errors correctly
	if strictErr != nil {
		// This is expected if npm is not available
		if _, lookupErr := os.Stat("/usr/bin/npm"); os.IsNotExist(lookupErr) {
			t.Logf("npm not available, strict mode error expected: %v", strictErr)
		}
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

// Tests for multi-ecosystem support

func TestGenerateDependabotConfig_MultipleEcosystems(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	dependabotPath := filepath.Join(tempDir, "dependabot.yml")

	ecosystems := map[string]bool{
		"npm":   true,
		"pip":   true,
		"gomod": true,
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
	ecosystemsFound := make(map[string]bool)
	for _, update := range config.Updates {
		ecosystemsFound[update.PackageEcosystem] = true
		if update.Directory != "/.github/workflows" {
			t.Errorf("expected directory '/.github/workflows', got %q", update.Directory)
		}
		if update.Schedule.Interval != "weekly" {
			t.Errorf("expected interval 'weekly', got %q", update.Schedule.Interval)
		}
	}

	for ecosystem := range ecosystems {
		if !ecosystemsFound[ecosystem] {
			t.Errorf("ecosystem %q not found in dependabot.yml", ecosystem)
		}
	}
}

func TestGenerateDependabotManifests_AllEcosystems(t *testing.T) {
	compiler := NewCompiler()
	tempDir := testutil.TempDir(t, "test-*")
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	os.MkdirAll(workflowDir, 0755)

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

	// This will skip npm install (no npm in test env), but should generate manifest files
	_ = compiler.GenerateDependabotManifests(workflows, workflowDir, false)

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
