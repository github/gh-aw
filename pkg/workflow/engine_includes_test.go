//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/constants"
)

func TestEngineInheritanceFromIncludes(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create include file with engine specification
	includeContent := `---
engine: codex
tools:
  github:
    allowed: ["list_issues"]
---

# Include with Engine
This include specifies the codex engine.
`
	includeFile := filepath.Join(workflowsDir, "include-with-engine.md")
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main workflow without engine specification
	mainContent := `---
on: push
---

# Main Workflow Without Engine

@include include-with-engine.md

This should inherit the engine from the included file.
`
	mainFile := filepath.Join(workflowsDir, "main-inherit-engine.md")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mainFile)
	if err != nil {
		t.Fatalf("Expected successful compilation, got error: %v", err)
	}

	// Check that lock file was created
	lockFile := filepath.Join(workflowsDir, "main-inherit-engine.lock.yml")
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Fatal("Expected lock file to be created")
	}

	// Verify lock file contains codex engine configuration
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	lockStr := string(lockContent)

	// Should contain references to codex installation and execution
	if !strings.Contains(lockStr, "Install Codex") {
		t.Error("Expected lock file to contain 'Install Codex' step")
	}
	if !strings.Contains(lockStr, "codex") || !strings.Contains(lockStr, "exec") {
		t.Error("Expected lock file to contain 'codex exec' command")
	}
}

func TestEngineConflictDetection(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create include file with codex engine
	includeContent := `---
engine: codex
tools:
  github:
    allowed: ["list_issues"]
---

# Include with Codex Engine
`
	includeFile := filepath.Join(workflowsDir, "include-codex.md")
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main workflow with claude engine (conflict)
	mainContent := `---
on: push
engine: claude
---

# Main Workflow with Claude Engine

@include include-codex.md

This should fail due to multiple engine specifications.
`
	mainFile := filepath.Join(workflowsDir, "main-conflict.md")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow - should fail
	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mainFile)
	if err == nil {
		t.Fatal("Expected compilation to fail due to multiple engine specifications")
	}

	// Check error message contains expected content
	errMsg := err.Error()
	if !strings.Contains(errMsg, "multiple engine fields found") {
		t.Errorf("Expected error message to contain 'multiple engine fields found', got: %s", errMsg)
	}
}

func TestEngineObjectFormatInIncludes(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create include file with object-format engine specification
	includeContent := `---
engine:
  id: claude
  model: claude-3-5-sonnet-20241022
  max-turns: 5
tools:
  github:
    allowed: ["list_issues"]
---

# Include with Object Engine
`
	includeFile := filepath.Join(workflowsDir, "include-object-engine.md")
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main workflow without engine specification
	mainContent := `---
on: push
---

# Main Workflow

@include include-object-engine.md

This should inherit the claude engine from the included file.
`
	mainFile := filepath.Join(workflowsDir, "main-object-engine.md")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mainFile)
	if err != nil {
		t.Fatalf("Expected successful compilation, got error: %v", err)
	}

	// Check that lock file was created
	lockFile := filepath.Join(workflowsDir, "main-object-engine.lock.yml")
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Fatal("Expected lock file to be created")
	}
}

func TestNoEngineSpecifiedAnywhere(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create include file without engine specification
	includeContent := `---
tools:
  github:
    allowed: ["list_issues"]
---

# Include without Engine
`
	includeFile := filepath.Join(workflowsDir, "include-no-engine.md")
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main workflow without engine specification
	mainContent := `---
on: push
---

# Main Workflow without Engine

@include include-no-engine.md

This should use the default engine.
`
	mainFile := filepath.Join(workflowsDir, "main-default.md")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mainFile)
	if err != nil {
		t.Fatalf("Expected successful compilation, got error: %v", err)
	}

	// Check that lock file was created
	lockFile := filepath.Join(workflowsDir, "main-default.lock.yml")
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Fatal("Expected lock file to be created")
	}

	// Verify lock file contains default copilot engine configuration
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	lockStr := string(lockContent)

	// Should contain references to copilot CLI (default engine) using install script wrapper
	if !strings.Contains(lockStr, "/opt/gh-aw/actions/install_copilot_cli.sh") {
		t.Error("Expected lock file to contain copilot CLI installation using install script wrapper")
	}

	// Should NOT use deprecated formats
	if strings.Contains(lockStr, "gh.io/copilot-install | sudo bash") {
		t.Error("Lock file should not pipe installer directly to bash")
	}
}

func TestMainEngineWithoutIncludes(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create main workflow with claude engine (no includes, so no conflict)
	mainContent := `---
on: push
engine: claude
---

# Main Workflow with Claude Engine

This workflow specifies claude engine directly without any includes.
`
	mainFile := filepath.Join(workflowsDir, "main-claude.md")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow (no includes, so no conflict)
	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mainFile)
	if err != nil {
		t.Fatalf("Expected successful compilation, got error: %v", err)
	}

	// Check that lock file contains claude engine
	lockFile := filepath.Join(workflowsDir, "main-claude.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	lockStr := string(lockContent)

	// Should contain references to claude command and npm install
	if !strings.Contains(lockStr, "claude --print") {
		t.Error("Expected lock file to contain claude command reference")
	}
	if !strings.Contains(lockStr, "npm install -g --silent @anthropic-ai/claude-code") {
		t.Error("Expected lock file to contain npm install command")
	}
}

func TestMultipleIncludesWithEnginesFailure(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create first include file with codex engine
	includeContent1 := `---
engine: codex
tools:
  github:
    allowed: ["list_issues"]
---

# Include with Codex Engine
`
	includeFile1 := filepath.Join(workflowsDir, "include-codex.md")
	if err := os.WriteFile(includeFile1, []byte(includeContent1), 0644); err != nil {
		t.Fatal(err)
	}

	// Create second include file with claude engine
	includeContent2 := `---
engine: claude
tools:
  github:
    allowed: ["create_issue"]
---

# Include with Claude Engine
`
	includeFile2 := filepath.Join(workflowsDir, "include-claude.md")
	if err := os.WriteFile(includeFile2, []byte(includeContent2), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main workflow without engine specification but with multiple includes
	mainContent := `---
on: push
---

# Main Workflow

@include include-codex.md
@include include-claude.md

This should fail due to multiple engine specifications in includes.
`
	mainFile := filepath.Join(workflowsDir, "main-multiple-engines.md")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow - should fail
	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mainFile)
	if err == nil {
		t.Fatal("Expected compilation to fail due to multiple engine specifications")
	}

	// Check error message contains expected content
	errMsg := err.Error()
	if !strings.Contains(errMsg, "multiple engine fields found") {
		t.Errorf("Expected error message to contain 'multiple engine fields found', got: %s", errMsg)
	}
}

// TestImportedEngineWithCustomSteps tests importing a custom engine configuration with steps
func TestImportedEngineWithCustomSteps(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create shared file with custom engine and steps
	sharedContent := `---
engine:
  id: custom
  steps:
    - name: Run AI Inference
      uses: actions/ai-inference@v1
      with:
        prompt-file: ${{ env.GH_AW_PROMPT }}
        model: gpt-4o-mini
---

<!--
This shared configuration sets up a custom agentic engine using GitHub's AI inference action.
-->
`
	sharedFile := filepath.Join(sharedDir, "actions-ai-inference.md")
	if err := os.WriteFile(sharedFile, []byte(sharedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main workflow that imports the shared engine config
	mainContent := `---
name: Test Imported Custom Engine
on:
  issues:
    types: [opened]
permissions:
  contents: read
  models: read
  issues: read
  pull-requests: read
imports:
  - shared/actions-ai-inference.md
---

# Test Workflow

This workflow imports a custom engine with steps.
`
	mainFile := filepath.Join(workflowsDir, "test-imported-engine.md")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mainFile)
	if err != nil {
		t.Fatalf("Expected successful compilation, got error: %v", err)
	}

	// Check that lock file was created
	lockFile := filepath.Join(workflowsDir, "test-imported-engine.lock.yml")
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Fatal("Expected lock file to be created")
	}

	// Verify lock file contains the custom step
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	lockStr := string(lockContent)

	// Should contain the AI Inference step
	if !strings.Contains(lockStr, "name: Run AI Inference") {
		t.Error("Expected lock file to contain 'name: Run AI Inference' step")
	}
	// Since ai-inference is not in hardcoded pins, it will either be dynamically resolved
	// (if gh CLI is available) or remain as @v1 (if resolution fails)
	if !strings.Contains(lockStr, "uses: actions/ai-inference@") {
		t.Error("Expected lock file to contain 'uses: actions/ai-inference@' (either @v1 or @<sha>)")
	}
	if !strings.Contains(lockStr, "prompt-file:") {
		t.Error("Expected lock file to contain 'prompt-file:' parameter")
	}
	if !strings.Contains(lockStr, "model: gpt-4o-mini") {
		t.Error("Expected lock file to contain 'model: gpt-4o-mini'")
	}
}

// TestImportedEngineWithEnvVars tests importing an engine configuration with environment variables
func TestImportedEngineWithEnvVars(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create shared file with custom engine, steps, and env vars
	sharedContent := `---
engine:
  id: custom
  env:
    CUSTOM_VAR: "test-value"
    ANOTHER_VAR: "another-value"
  steps:
    - name: Test Step
      run: echo "Testing with env vars"
---

# Shared Config
`
	sharedFile := filepath.Join(sharedDir, "custom-with-env.md")
	if err := os.WriteFile(sharedFile, []byte(sharedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main workflow that imports the shared engine config
	mainContent := `---
name: Test Imported Engine With Env
on: push
imports:
  - shared/custom-with-env.md
---

# Test Workflow

This workflow imports a custom engine with env vars.
`
	mainFile := filepath.Join(workflowsDir, "test-env.md")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mainFile)
	if err != nil {
		t.Fatalf("Expected successful compilation, got error: %v", err)
	}

	// Check that lock file was created
	lockFile := filepath.Join(workflowsDir, "test-env.lock.yml")
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Fatal("Expected lock file to be created")
	}

	// Verify lock file contains the environment variables
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	lockStr := string(lockContent)

	// Should contain the custom environment variables
	if !strings.Contains(lockStr, "CUSTOM_VAR: test-value") {
		t.Error("Expected lock file to contain 'CUSTOM_VAR: test-value'")
	}
	if !strings.Contains(lockStr, "ANOTHER_VAR: another-value") {
		t.Error("Expected lock file to contain 'ANOTHER_VAR: another-value'")
	}
}

// TestExtractEngineConfigFromJSON tests the extractEngineConfigFromJSON function
func TestExtractEngineConfigFromJSON(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name          string
		engineJSON    string
		expectedID    string
		expectedSteps int
		expectedEnv   map[string]string
		expectError   bool
	}{
		{
			name:          "simple string engine",
			engineJSON:    `"claude"`,
			expectedID:    "claude",
			expectedSteps: 0,
			expectError:   false,
		},
		{
			name:          "object with id only",
			engineJSON:    `{"id": "custom"}`,
			expectedID:    "custom",
			expectedSteps: 0,
			expectError:   false,
		},
		{
			name:          "custom engine with steps",
			engineJSON:    `{"id": "custom", "steps": [{"name": "Test", "run": "echo test"}]}`,
			expectedID:    "custom",
			expectedSteps: 1,
			expectError:   false,
		},
		{
			name:          "custom engine with env vars",
			engineJSON:    `{"id": "custom", "env": {"VAR1": "value1", "VAR2": "value2"}, "steps": [{"name": "Test", "run": "echo test"}]}`,
			expectedID:    "custom",
			expectedSteps: 1,
			expectedEnv:   map[string]string{"VAR1": "value1", "VAR2": "value2"},
			expectError:   false,
		},
		{
			name:        "invalid JSON",
			engineJSON:  `{invalid}`,
			expectError: true,
		},
		{
			name:        "empty string",
			engineJSON:  ``,
			expectedID:  "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := compiler.extractEngineConfigFromJSON(tt.engineJSON)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.engineJSON == "" {
				if config != nil {
					t.Error("Expected nil config for empty JSON")
				}
				return
			}

			if config == nil {
				t.Fatal("Expected non-nil config")
			}

			if config.ID != tt.expectedID {
				t.Errorf("Expected ID %q, got %q", tt.expectedID, config.ID)
			}

			if len(config.Steps) != tt.expectedSteps {
				t.Errorf("Expected %d steps, got %d", tt.expectedSteps, len(config.Steps))
			}

			if tt.expectedEnv != nil {
				if config.Env == nil {
					t.Error("Expected env vars but got nil")
				} else {
					for key, expectedValue := range tt.expectedEnv {
						if actualValue, exists := config.Env[key]; !exists {
							t.Errorf("Expected env var %q to exist", key)
						} else if actualValue != expectedValue {
							t.Errorf("Expected env var %q to be %q, got %q", key, expectedValue, actualValue)
						}
					}
				}
			}
		})
	}
}
