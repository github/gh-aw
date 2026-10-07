//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

func TestCompileWorkflowWithRuntimes(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "runtime-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create workflow with runtime overrides
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
runtimes:
  node:
    version: "22"
  python:
    version: "3.12"
---

# Test Workflow

Test workflow with runtime overrides.
`
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify runtimes were extracted
	if workflowData.Runtimes == nil {
		t.Fatal("Expected Runtimes to be non-nil")
	}

	// Check node runtime
	nodeRuntime, ok := workflowData.Runtimes["node"]
	if !ok {
		t.Fatal("Expected 'node' runtime to be present")
	}
	nodeConfig, ok := nodeRuntime.(map[string]any)
	if !ok {
		t.Fatal("Expected node runtime to be a map")
	}
	if nodeConfig["version"] != "22" {
		t.Errorf("Expected node version '22', got '%v'", nodeConfig["version"])
	}

	// Check python runtime
	pythonRuntime, ok := workflowData.Runtimes["python"]
	if !ok {
		t.Fatal("Expected 'python' runtime to be present")
	}
	pythonConfig, ok := pythonRuntime.(map[string]any)
	if !ok {
		t.Fatal("Expected python runtime to be a map")
	}
	if pythonConfig["version"] != "3.12" {
		t.Errorf("Expected python version '3.12', got '%v'", pythonConfig["version"])
	}
}

func TestCompileWorkflowWithRuntimesFromImports(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "runtime-import-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create shared directory
	sharedDir := filepath.Join(tempDir, ".github", "workflows", "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	// Create shared workflow with runtime overrides
	sharedContent := `---
runtimes:
  ruby:
    version: "3.2"
  go:
    version: "1.22"
---
`
	sharedPath := filepath.Join(sharedDir, "shared-runtimes.md")
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports the shared runtimes
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared/shared-runtimes.md
runtimes:
  node:
    version: "22"
---

# Test Workflow

Test workflow with imported runtimes.
`
	workflowPath := filepath.Join(tempDir, ".github", "workflows", "test-workflow.md")
	workflowDir := filepath.Dir(workflowPath)
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("Failed to create workflow directory: %v", err)
	}
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify runtimes were merged
	if workflowData.Runtimes == nil {
		t.Fatal("Expected Runtimes to be non-nil")
	}

	// Check node runtime (from main workflow)
	nodeRuntime, ok := workflowData.Runtimes["node"]
	if !ok {
		t.Fatal("Expected 'node' runtime to be present")
	}
	nodeConfig, ok := nodeRuntime.(map[string]any)
	if !ok {
		t.Fatal("Expected node runtime to be a map")
	}
	if nodeConfig["version"] != "22" {
		t.Errorf("Expected node version '22', got '%v'", nodeConfig["version"])
	}

	// Check ruby runtime (from imported workflow)
	rubyRuntime, ok := workflowData.Runtimes["ruby"]
	if !ok {
		t.Fatal("Expected 'ruby' runtime to be present (from import)")
	}
	rubyConfig, ok := rubyRuntime.(map[string]any)
	if !ok {
		t.Fatal("Expected ruby runtime to be a map")
	}
	if rubyConfig["version"] != "3.2" {
		t.Errorf("Expected ruby version '3.2', got '%v'", rubyConfig["version"])
	}

	// Check go runtime (from imported workflow)
	goRuntime, ok := workflowData.Runtimes["go"]
	if !ok {
		t.Fatal("Expected 'go' runtime to be present (from import)")
	}
	goConfig, ok := goRuntime.(map[string]any)
	if !ok {
		t.Fatal("Expected go runtime to be a map")
	}
	if goConfig["version"] != "1.22" {
		t.Errorf("Expected go version '1.22', got '%v'", goConfig["version"])
	}
}

func TestCompileWorkflowWithRuntimesAppliedToSteps(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "runtime-steps-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create workflow with custom steps and runtime overrides
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Install dependencies
    run: npm install
runtimes:
  node:
    version: "22"
---

# Test Workflow

Test workflow with runtime overrides applied to steps.
`
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify that Node.js setup step is included with version 22
	if !strings.Contains(lockStr, "uses: actions/setup-node@") { // SHA varies
		t.Error("Expected setup-node action in lock file")
	}
	if !strings.Contains(lockStr, "node-version: '22'") {
		t.Error("Expected node-version: '22' in lock file")
	}
}

func TestCompileWorkflowWithCustomActionRepo(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "runtime-custom-action-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create workflow with custom action-repo and action-version
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Install dependencies
    run: npm install
runtimes:
  node:
    version: "22"
    action-repo: "custom/setup-node"
    action-version: "v5"
---

# Test Workflow

Test workflow with custom setup action.
`
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify that custom setup action is used
	if !strings.Contains(lockStr, "custom/setup-node@v5") {
		t.Error("Expected custom/setup-node@v5 action in lock file")
	}
	if !strings.Contains(lockStr, "node-version: '22'") {
		t.Error("Expected node-version: '22' in lock file")
	}
}

func TestCompileWorkflowWithGoRuntimeWithoutGoMod(t *testing.T) {
	// Create temp directory for test (without go.mod file)
	tempDir, err := os.MkdirTemp("", "go-runtime-no-gomod-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create workflow that uses Go commands but doesn't have go.mod
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Check Go version
    run: go version
---

# Test Workflow

Test workflow that uses Go without go.mod file.
`
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify that Go setup step is included with default version
	if !strings.Contains(lockStr, "Setup Go") {
		t.Error("Expected 'Setup Go' step in lock file")
	}
	if !strings.Contains(lockStr, "actions/setup-go@") {
		t.Error("Expected actions/setup-go action in lock file")
	}
	if !strings.Contains(lockStr, "go-version: '1.26'") {
		t.Error("Expected go-version: '1.26' in lock file (default version)")
	}
	// Ensure it does NOT use go-version-file
	if strings.Contains(lockStr, "go-version-file") {
		t.Error("Should not use go-version-file when go.mod doesn't exist")
	}
}

func TestRuntimeIfConditionIntegration(t *testing.T) {
	tests := []struct {
		name           string
		frontmatter    map[string]any
		expectedIfGo   bool
		expectedIfPy   bool
		expectedIfNode bool
		expectedGoIf   string
		expectedPyIf   string
		expectedNodeIf string
	}{
		{
			name: "go runtime with hashFiles if condition",
			frontmatter: map[string]any{
				"name":   "test-workflow",
				"engine": "copilot",
				"runtimes": map[string]any{
					"go": map[string]any{
						"version": "1.25",
						"if":      "hashFiles('go.mod') != ''",
					},
				},
			},
			expectedIfGo: true,
			expectedGoIf: "hashFiles('go.mod') != ''",
		},
		{
			name: "multiple runtimes with different if conditions",
			frontmatter: map[string]any{
				"name":   "test-workflow",
				"engine": "copilot",
				"runtimes": map[string]any{
					"go": map[string]any{
						"version": "1.25",
						"if":      "hashFiles('go.mod') != ''",
					},
					"python": map[string]any{
						"version": "3.11",
						"if":      "hashFiles('requirements.txt') != '' || hashFiles('pyproject.toml') != ''",
					},
					"node": map[string]any{
						"version": "24",
						"if":      "hashFiles('package.json') != ''",
					},
				},
			},
			expectedIfGo:   true,
			expectedIfPy:   true,
			expectedIfNode: true,
			expectedGoIf:   "hashFiles('go.mod') != ''",
			expectedPyIf:   "hashFiles('requirements.txt') != '' || hashFiles('pyproject.toml') != ''",
			expectedNodeIf: "hashFiles('package.json') != ''",
		},
		{
			name: "runtime with only if condition, no version",
			frontmatter: map[string]any{
				"name":   "test-workflow",
				"engine": "copilot",
				"runtimes": map[string]any{
					"uv": map[string]any{
						"if": "hashFiles('uv.lock') != ''",
					},
				},
			},
			// Note: We're not tracking UV in this test, but it would have the if condition
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseFrontmatterConfig(tt.frontmatter)
			if err != nil {
				t.Fatalf("Failed to parse frontmatter: %v", err)
			}

			// Verify typed runtimes are parsed with if conditions
			if tt.expectedIfGo && config.RuntimesTyped != nil && config.RuntimesTyped.Go != nil {
				if config.RuntimesTyped.Go.If != tt.expectedGoIf {
					t.Errorf("Go if condition: got %q, want %q", config.RuntimesTyped.Go.If, tt.expectedGoIf)
				}
			}

			if tt.expectedIfPy && config.RuntimesTyped != nil && config.RuntimesTyped.Python != nil {
				if config.RuntimesTyped.Python.If != tt.expectedPyIf {
					t.Errorf("Python if condition: got %q, want %q", config.RuntimesTyped.Python.If, tt.expectedPyIf)
				}
			}

			if tt.expectedIfNode && config.RuntimesTyped != nil && config.RuntimesTyped.Node != nil {
				if config.RuntimesTyped.Node.If != tt.expectedNodeIf {
					t.Errorf("Node if condition: got %q, want %q", config.RuntimesTyped.Node.If, tt.expectedNodeIf)
				}
			}

			// Apply runtime overrides to simulate the workflow compilation process
			requirements := make(map[string]*RuntimeRequirement)
			if config.Runtimes != nil {
				applyRuntimeOverrides(config.Runtimes, requirements)
			}

			// Verify requirements have if conditions
			if tt.expectedIfGo {
				if goReq, exists := requirements["go"]; exists {
					if goReq.IfCondition != tt.expectedGoIf {
						t.Errorf("Go requirement if condition: got %q, want %q", goReq.IfCondition, tt.expectedGoIf)
					}
				} else {
					t.Error("Go requirement not found")
				}
			}

			if tt.expectedIfPy {
				if pyReq, exists := requirements["python"]; exists {
					if pyReq.IfCondition != tt.expectedPyIf {
						t.Errorf("Python requirement if condition: got %q, want %q", pyReq.IfCondition, tt.expectedPyIf)
					}
				} else {
					t.Error("Python requirement not found")
				}
			}

			if tt.expectedIfNode {
				if nodeReq, exists := requirements["node"]; exists {
					if nodeReq.IfCondition != tt.expectedNodeIf {
						t.Errorf("Node requirement if condition: got %q, want %q", nodeReq.IfCondition, tt.expectedNodeIf)
					}
				} else {
					t.Error("Node requirement not found")
				}
			}

			// Generate setup steps and verify they contain the if conditions
			var reqSlice []RuntimeRequirement
			for _, req := range requirements {
				reqSlice = append(reqSlice, *req)
			}

			steps := GenerateRuntimeSetupSteps(reqSlice, nil)
			allSteps := ""
			for _, step := range steps {
				for _, line := range step {
					allSteps += line + "\n"
				}
			}

			if tt.expectedIfGo && !strings.Contains(allSteps, tt.expectedGoIf) {
				t.Errorf("Generated steps do not contain expected Go if condition %q\nGot:\n%s", tt.expectedGoIf, allSteps)
			}

			if tt.expectedIfPy && !strings.Contains(allSteps, tt.expectedPyIf) {
				t.Errorf("Generated steps do not contain expected Python if condition %q\nGot:\n%s", tt.expectedPyIf, allSteps)
			}

			if tt.expectedIfNode && !strings.Contains(allSteps, tt.expectedNodeIf) {
				t.Errorf("Generated steps do not contain expected Node if condition %q\nGot:\n%s", tt.expectedNodeIf, allSteps)
			}
		})
	}
}
