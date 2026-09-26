//go:build !integration

package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/workflow"
)

func TestCompileWorkflowWithImports(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared tool file
	sharedToolPath := filepath.Join(tempDir, "shared-tool.md")
	sharedToolContent := `---
on: push
tools:
  custom-mcp:
    url: "https://example.com/mcp"
    allowed: ["*"]
---
`
	if err := os.WriteFile(sharedToolPath, []byte(sharedToolContent), 0644); err != nil {
		t.Fatalf("Failed to write shared tool file: %v", err)
	}

	// Create a workflow file that imports the shared tool
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared-tool.md
tools:
  cache-memory:
    retention-days: 7
---

# Test Workflow

This is a test workflow.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	workflowData := string(lockFileContent)

	// Verify that the compiled workflow contains the imported tool
	if !strings.Contains(workflowData, "custom-mcp") {
		t.Error("Expected compiled workflow to contain custom-mcp from imported file")
	}

	// Verify the MCP URL is present
	if !strings.Contains(workflowData, "https://example.com/mcp") {
		t.Error("Expected compiled workflow to contain MCP URL from imported file")
	}
}

func TestCompileWorkflowWithMultipleImports(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create first shared tool file
	sharedTool1Path := filepath.Join(tempDir, "shared-tool-1.md")
	sharedTool1Content := `---
on: push
tools:
  tool1:
    url: "https://example1.com/mcp"
    allowed: ["*"]
---
`
	if err := os.WriteFile(sharedTool1Path, []byte(sharedTool1Content), 0644); err != nil {
		t.Fatalf("Failed to write shared tool 1 file: %v", err)
	}

	// Create second shared tool file
	sharedTool2Path := filepath.Join(tempDir, "shared-tool-2.md")
	sharedTool2Content := `---
on: push
tools:
  tool2:
    url: "https://example2.com/mcp"
    allowed: ["*"]
---
`
	if err := os.WriteFile(sharedTool2Path, []byte(sharedTool2Content), 0644); err != nil {
		t.Fatalf("Failed to write shared tool 2 file: %v", err)
	}

	// Create a workflow file that imports both shared tools
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared-tool-1.md
  - shared-tool-2.md
tools:
  cache-memory:
    retention-days: 7
---

# Test Workflow

This is a test workflow with multiple imports.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	workflowData := string(lockFileContent)

	// Verify that the compiled workflow contains both imported tools
	if !strings.Contains(workflowData, "tool1") {
		t.Error("Expected compiled workflow to contain tool1 from first import")
	}

	if !strings.Contains(workflowData, "tool2") {
		t.Error("Expected compiled workflow to contain tool2 from second import")
	}

	// Verify both URLs are present
	if !strings.Contains(workflowData, "https://example1.com/mcp") {
		t.Error("Expected compiled workflow to contain URL from first import")
	}

	if !strings.Contains(workflowData, "https://example2.com/mcp") {
		t.Error("Expected compiled workflow to contain URL from second import")
	}
}

func TestCompileWorkflowWithConditionalImport(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	sharedPath := filepath.Join(tempDir, "shared-conditional.md")
	sharedContent := `---
steps:
  - name: Conditional Imported Step
    run: echo "from import"
---

Imported conditional instructions.
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared conditional file: %v", err)
	}

	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
experiments:
  strategy: [eager, lazy]
imports:
  - path: shared-conditional.md
    if: "experiments.strategy == 'eager'"
---

# Test Workflow

Main workflow body.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := workflow.NewCompiler()
	err := compiler.CompileWorkflow(workflowPath)
	if err == nil {
		t.Fatal("Expected CompileWorkflow to fail for imports.if, but it succeeded")
	}
	// imports.if is rejected — either by schema validation ("Unknown property: if")
	// or by the migration guard ("import 'if' is no longer supported").
	errMsg := err.Error()
	if !strings.Contains(errMsg, "Unknown property: if") && !strings.Contains(errMsg, "import 'if' is no longer supported") {
		t.Errorf("Expected rejection of imports.if, got unrelated error: %v", err)
	}
}

func TestCompileWorkflowWithImportedSandboxMounts(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-imported-sandbox-mounts-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	sharedAPath := filepath.Join(workflowsDir, "shared-a.md")
	sharedAContent := `---
sandbox:
  agent:
    mounts:
      - /tool-a/bin/my-cli:/tool-a/bin/my-cli:ro
      - /shared/bin/tool:/shared/bin/tool:ro
---

# Shared A
`
	if err := os.WriteFile(sharedAPath, []byte(sharedAContent), 0644); err != nil {
		t.Fatalf("Failed to write shared A workflow: %v", err)
	}

	sharedBPath := filepath.Join(workflowsDir, "shared-b.md")
	sharedBContent := `---
sandbox:
  agent:
    mounts:
      - /tool-b/bin/other-cli:/tool-b/bin/other-cli:ro
      - /shared/bin/tool:/shared/bin/tool:ro
---

# Shared B
`
	if err := os.WriteFile(sharedBPath, []byte(sharedBContent), 0644); err != nil {
		t.Fatalf("Failed to write shared B workflow: %v", err)
	}

	workflowPath := filepath.Join(workflowsDir, "main.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
imports:
  - ./shared-a.md
  - ./shared-b.md
sandbox:
  agent:
    mounts:
      - /main/bin/main-cli:/main/bin/main-cli:ro
      - /shared/bin/tool:/shared/bin/tool:ro
---

# Main Workflow

Validate imported sandbox mounts.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write main workflow: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	compiled := string(lockFileContent)

	for _, mount := range []string{
		"--mount /tool-a/bin/my-cli:/tool-a/bin/my-cli:ro",
		"--mount /tool-b/bin/other-cli:/tool-b/bin/other-cli:ro",
		"--mount /main/bin/main-cli:/main/bin/main-cli:ro",
	} {
		if !strings.Contains(compiled, mount) {
			t.Errorf("Expected compiled workflow to contain mount %q", mount)
		}
	}

	if count := strings.Count(compiled, "--mount /shared/bin/tool:/shared/bin/tool:ro"); count != 1 {
		t.Errorf("Expected deduplicated shared mount exactly once, got %d", count)
	}
}

func TestCompileWorkflowWithMCPServersImport(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared mcp-servers file (like tavily-mcp.md)
	sharedMCPPath := filepath.Join(tempDir, "shared-mcp.md")
	sharedMCPContent := `---
on: push
mcp-servers:
  tavily:
    url: "https://mcp.tavily.com/mcp/?tavilyApiKey=test"
    allowed: ["*"]
---
`
	if err := os.WriteFile(sharedMCPPath, []byte(sharedMCPContent), 0644); err != nil {
		t.Fatalf("Failed to write shared MCP file: %v", err)
	}

	// Create a workflow file that imports the shared MCP server
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared-mcp.md
tools:
  cache-memory:
    retention-days: 7
---

# Test Workflow

This is a test workflow with imported MCP server.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	workflowData := string(lockFileContent)

	// Verify that the compiled workflow contains the imported MCP server
	if !strings.Contains(workflowData, "tavily") {
		t.Error("Expected compiled workflow to contain tavily MCP server from imported file")
	}

	// Verify the MCP URL is present
	if !strings.Contains(workflowData, "https://mcp.tavily.com/mcp") {
		t.Error("Expected compiled workflow to contain Tavily MCP URL from imported file")
	}

	// Verify it's configured as an HTTP MCP server
	if !strings.Contains(workflowData, `"type": "http"`) {
		t.Error("Expected tavily to be configured as HTTP MCP server")
	}
}

// TestCompileWorkflowWithTopLevelModel verifies that a workflow declaring a top-level
// model: field (without engine.model) compiles successfully.
func TestCompileWorkflowWithTopLevelModel(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
model: small
---

# Test Workflow

This workflow uses the top-level model field to express a model-size preference.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed for top-level model: field: %v", err)
	}
}

// TestCompileWorkflowWithImportedTopLevelModel verifies that a workflow importing a shared
// file that declares only a top-level model: field (without engine or engine.model) compiles
// successfully and that the shared workflow's model is applied to the compiled output.
func TestCompileWorkflowWithImportedTopLevelModel(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Shared workflow that only declares a model preference using the top-level model: field
	sharedPath := filepath.Join(tempDir, "shared-workflow.md")
	sharedContent := `---
model: claude-sonnet-4-5
---
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared workflow file: %v", err)
	}

	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared-workflow.md
---

# Test Workflow

This workflow imports a shared file that declares a model preference via top-level model:.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed when imported shared workflow has top-level model: field: %v", err)
	}

	// Verify the shared workflow's model is reflected in the compiled output.
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockFileContent)
	if !strings.Contains(lockStr, `GH_AW_INFO_MODEL: "claude-sonnet-4-5"`) {
		t.Errorf("Expected compiled workflow to contain GH_AW_INFO_MODEL from shared workflow's model: field")
	}
}

// TestSharedWorkflowModelAppliedToMainWorkflow verifies the model resolution algorithm:
// when a shared workflow declares model: and the importing main workflow does not specify
// its own model, the shared workflow's model is considered and applied.
func TestSharedWorkflowModelAppliedToMainWorkflow(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Shared workflow with a model preference
	sharedPath := filepath.Join(tempDir, "shared-config.md")
	sharedContent := `---
model: anthropic/claude-sonnet-4-5
---
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared workflow file: %v", err)
	}

	// Main workflow with no model: field — should inherit from shared workflow
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared-config.md
---

# Test Workflow

This workflow inherits its model from the imported shared workflow.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockFileContent)

	// The shared workflow's model must be present in the compiled output.
	if !strings.Contains(lockStr, `GH_AW_INFO_MODEL: "anthropic/claude-sonnet-4-5"`) {
		t.Errorf("Expected compiled workflow to contain GH_AW_INFO_MODEL from shared workflow's model: field, got lock file without it")
	}
}

// TestMainWorkflowModelTakesPrecedenceOverSharedWorkflowModel verifies the model resolution
// hierarchy: the main workflow's own model: field takes precedence over any model declared
// in an imported shared workflow.
func TestMainWorkflowModelTakesPrecedenceOverSharedWorkflowModel(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Shared workflow with a model preference
	sharedPath := filepath.Join(tempDir, "shared-config.md")
	sharedContent := `---
model: anthropic/claude-sonnet-4-5
---
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared workflow file: %v", err)
	}

	// Main workflow with its own model: field — must override the shared workflow's model
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
model: openai/gpt-5.4
imports:
  - shared-config.md
---

# Test Workflow

This workflow overrides the shared workflow's model with its own model: field.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockFileContent)

	// Main workflow's model must win.
	if !strings.Contains(lockStr, `GH_AW_INFO_MODEL: "openai/gpt-5.4"`) {
		t.Errorf("Expected main workflow model 'openai/gpt-5.4' to take precedence over shared workflow model")
	}
	if strings.Contains(lockStr, "claude-sonnet-4-5") {
		t.Errorf("Shared workflow model 'claude-sonnet-4-5' must not appear when main workflow specifies its own model")
	}
}

// TestCompileWorkflowWithModelOnlyEngine verifies that a workflow declaring
// engine.model without engine.id compiles successfully. This allows workflow
// authors to express a model-size preference (e.g. "small") without committing
// to a specific engine, letting the runtime select the appropriate engine.
func TestCompileWorkflowWithModelOnlyEngine(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  model: small
---

# Test Workflow

This workflow expresses a model-size preference without specifying an engine id.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed for engine.model without engine.id: %v", err)
	}
}

// TestCompileWorkflowWithImportedModelOnlyEngine verifies that a workflow importing
// a shared file that declares engine.model (without engine.id) compiles successfully.
// This is the primary use case: gh aw add / gh aw add-wizard add shared workflows that
// only express a model preference, not a specific engine selection.
func TestCompileWorkflowWithImportedModelOnlyEngine(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Shared workflow that only declares a model preference (no engine.id)
	sharedPath := filepath.Join(tempDir, "shared-workflow.md")
	sharedContent := `---
on: push
engine:
  model: small
---
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared workflow file: %v", err)
	}

	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared-workflow.md
---

# Test Workflow

This workflow imports a shared file that declares a model preference.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed when imported shared workflow has engine.model without engine.id: %v", err)
	}
}

func TestMergeNetworkPermissionsPreservesImportedRestrictions(t *testing.T) {
	compiler := workflow.NewCompiler()

	t.Run("blocked domains", func(t *testing.T) {
		permissions, err := compiler.MergeNetworkPermissions(
			&workflow.NetworkPermissions{Allowed: []string{"defaults"}},
			`{"blocked":["tracker.example.com"]}`,
		)

		if err != nil {
			t.Fatalf("MergeNetworkPermissions returned an error: %v", err)
		}
		if len(permissions.Blocked) != 1 || permissions.Blocked[0] != "tracker.example.com" {
			t.Errorf("expected imported blocked domain to be preserved, got %v", permissions.Blocked)
		}
	})

	t.Run("empty network object", func(t *testing.T) {
		permissions, err := compiler.MergeNetworkPermissions(
			&workflow.NetworkPermissions{Allowed: []string{"defaults"}},
			"{}\n{\"allowed\":[\"example.com\"]}",
		)

		if err != nil {
			t.Fatalf("MergeNetworkPermissions returned an error: %v", err)
		}
		if len(permissions.Allowed) != 0 || !permissions.ExplicitlyDefined {
			t.Errorf("expected imported empty network object to preserve deny-all semantics, got %+v", permissions)
		}
	})
}
