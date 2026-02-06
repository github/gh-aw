//go:build !integration

package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// TestImportPlaywrightTool tests that playwright tool can be imported from a shared workflow
func TestImportPlaywrightTool(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with playwright tool
	sharedPath := filepath.Join(tempDir, "shared-playwright.md")
	sharedContent := `---
description: "Shared playwright configuration"
tools:
  playwright:
    version: "v1.41.0"
    allowed_domains:
      - "example.com"
      - "github.com"
network:
  allowed:
    - playwright
---

# Shared Playwright Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports playwright
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-playwright.md
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Main Workflow

Uses imported playwright tool.
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

	// Verify playwright is configured in the MCP config
	if !strings.Contains(workflowData, `"playwright"`) {
		t.Error("Expected compiled workflow to contain playwright tool")
	}

	// Verify playwright Docker image
	if !strings.Contains(workflowData, "mcr.microsoft.com/playwright/mcp") {
		t.Error("Expected compiled workflow to contain playwright Docker image")
	}

	// Verify allowed domains are present
	if !strings.Contains(workflowData, "example.com") {
		t.Error("Expected compiled workflow to contain example.com domain")
	}
	if !strings.Contains(workflowData, "github.com") {
		t.Error("Expected compiled workflow to contain github.com domain")
	}
}

// TestImportSerenaTool tests that serena tool can be imported from a shared workflow
func TestImportSerenaTool(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with serena tool
	sharedPath := filepath.Join(tempDir, "shared-serena.md")
	sharedContent := `---
description: "Shared serena configuration"
tools:
  serena:
    - go
    - typescript
---

# Shared Serena Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports serena
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-serena.md
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Main Workflow

Uses imported serena tool.
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

	// Verify serena is configured in the MCP config
	if !strings.Contains(workflowData, `"serena"`) {
		t.Error("Expected compiled workflow to contain serena tool")
	}

	// Verify serena container (now using Docker instead of uvx)
	if !strings.Contains(workflowData, "ghcr.io/github/serena-mcp-server:latest") {
		t.Error("Expected compiled workflow to contain serena Docker container")
	}

	// Verify that language service setup steps are NOT present
	// since Serena now runs in a container with language services included
	if strings.Contains(workflowData, "Install Go language service") {
		t.Error("Did not expect Go language service installation step (Serena runs in container)")
	}

	if strings.Contains(workflowData, "Install TypeScript language service") {
		t.Error("Did not expect TypeScript language service installation step (Serena runs in container)")
	}
}

// TestImportAgenticWorkflowsTool tests that agentic-workflows tool can be imported
func TestImportAgenticWorkflowsTool(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with agentic-workflows tool
	sharedPath := filepath.Join(tempDir, "shared-aw.md")
	sharedContent := `---
description: "Shared agentic-workflows configuration"
tools:
  agentic-workflows: true
permissions:
  actions: read
---

# Shared Agentic Workflows Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports agentic-workflows
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-aw.md
permissions:
  actions: read
  contents: read
  issues: read
  pull-requests: read
---

# Main Workflow

Uses imported agentic-workflows tool.
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

	// Verify containerized agenticworkflows server is present (per MCP Gateway Specification v1.0.0)
	// In dev mode, no entrypoint or entrypointArgs (uses container's defaults)
	if strings.Contains(workflowData, `"entrypointArgs"`) {
		t.Error("Did not expect entrypointArgs field in dev mode (uses container's CMD)")
	}

	if strings.Contains(workflowData, `"--cmd"`) {
		t.Error("Did not expect --cmd argument in dev mode")
	}

	// Verify container format is used (not command format)
	// In dev mode, should use locally built image
	if !strings.Contains(workflowData, `"container": "localhost/gh-aw:dev"`) {
		t.Error("Expected compiled workflow to contain localhost/gh-aw:dev container for agentic-workflows in dev mode")
	}

	// Verify NO entrypoint field (uses container's default ENTRYPOINT)
	if strings.Contains(workflowData, `"entrypoint"`) {
		t.Error("Did not expect entrypoint field in dev mode (uses container's ENTRYPOINT)")
	}

	// Verify binary mounts are NOT present in dev mode
	if strings.Contains(workflowData, `/opt/gh-aw:/opt/gh-aw:ro`) {
		t.Error("Did not expect /opt/gh-aw mount in dev mode (binary is in image)")
	}

	// Verify DEBUG and GITHUB_TOKEN are present
	if !strings.Contains(workflowData, `"DEBUG": "*"`) {
		t.Error("Expected DEBUG set to literal '*' in env vars")
	}
	if !strings.Contains(workflowData, `"GITHUB_TOKEN"`) {
		t.Error("Expected GITHUB_TOKEN in env vars")
	}

	// Verify working directory args are present
	if !strings.Contains(workflowData, `"args": ["--network", "host", "-w", "${{ github.workspace }}"]`) {
		t.Error("Expected args with network access and working directory")
	}
}

// TestImportAllThreeTools tests importing all three tools together
func TestImportAllThreeTools(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with all three tools
	sharedPath := filepath.Join(tempDir, "shared-all.md")
	sharedContent := `---
description: "Shared configuration with all tools"
tools:
  agentic-workflows: true
  serena:
    - go
  playwright:
    version: "v1.41.0"
    allowed_domains:
      - "example.com"
permissions:
  actions: read
network:
  allowed:
    - playwright
---

# Shared All Tools Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports all tools
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-all.md
permissions:
  actions: read
  contents: read
  issues: read
  pull-requests: read
---

# Main Workflow

Uses all imported tools.
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

	// Verify all three tools are present
	if !strings.Contains(workflowData, `"playwright"`) {
		t.Error("Expected compiled workflow to contain playwright tool")
	}
	if !strings.Contains(workflowData, `"serena"`) {
		t.Error("Expected compiled workflow to contain serena tool")
	}
	// Per MCP Gateway Specification v1.0.0, agentic-workflows uses containerized format
	if !strings.Contains(workflowData, `"`+constants.AgenticWorkflowsMCPServerID+`"`) {
		t.Error("Expected compiled workflow to contain agentic-workflows tool")
	}

	// Verify specific configurations
	if !strings.Contains(workflowData, "mcr.microsoft.com/playwright/mcp") {
		t.Error("Expected compiled workflow to contain playwright Docker image")
	}
	if !strings.Contains(workflowData, "ghcr.io/github/serena-mcp-server:latest") {
		t.Error("Expected compiled workflow to contain serena Docker container")
	}
	if !strings.Contains(workflowData, "example.com") {
		t.Error("Expected compiled workflow to contain example.com domain for playwright")
	}
}

// TestImportSerenaWithLanguageConfig tests serena with detailed language configuration
func TestImportSerenaWithLanguageConfig(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with serena tool with detailed language config
	sharedPath := filepath.Join(tempDir, "shared-serena-config.md")
	sharedContent := `---
description: "Shared serena with language config"
tools:
  serena:
    languages:
      go:
        version: "1.21"
        gopls-version: "latest"
      typescript:
        version: "22"
---

# Shared Serena Language Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports serena
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-serena-config.md
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Main Workflow

Uses imported serena with language config.
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

	// Verify serena is configured
	if !strings.Contains(workflowData, `"serena"`) {
		t.Error("Expected compiled workflow to contain serena tool")
	}

	// Verify that language runtime setup steps are NOT present
	// since Serena now runs in a container with language services included
	// Note: "Setup Go for CLI build" is for building the gh-aw CLI in dev mode, not for runtime
	if strings.Contains(workflowData, "- name: Setup Go\n") {
		t.Error("Did not expect Go runtime setup step (Serena runs in container)")
	}

	if strings.Contains(workflowData, "- name: Setup Node.js\n") {
		t.Error("Did not expect Node.js setup step (Serena runs in container)")
	}

	// Verify serena container is present
	if !strings.Contains(workflowData, "ghcr.io/github/serena-mcp-server") {
		t.Error("Expected serena to use Docker container")
	}
}

// TestImportPlaywrightWithCustomArgs tests playwright with custom arguments
func TestImportPlaywrightWithCustomArgs(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with playwright tool with custom args
	sharedPath := filepath.Join(tempDir, "shared-playwright-args.md")
	sharedContent := `---
description: "Shared playwright with custom args"
tools:
  playwright:
    version: "v1.41.0"
    allowed_domains:
      - "example.com"
    args:
      - "--custom-flag"
      - "value"
network:
  allowed:
    - playwright
---

# Shared Playwright with Args
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports playwright
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-playwright-args.md
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Main Workflow

Uses imported playwright with custom args.
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

	// Verify playwright is configured
	if !strings.Contains(workflowData, `"playwright"`) {
		t.Error("Expected compiled workflow to contain playwright tool")
	}

	// Verify custom args are present
	if !strings.Contains(workflowData, "--custom-flag") {
		t.Error("Expected compiled workflow to contain --custom-flag custom argument")
	}
	if !strings.Contains(workflowData, "value") {
		t.Error("Expected compiled workflow to contain custom argument value")
	}
}

// TestImportAgenticWorkflowsRequiresPermissions tests that agentic-workflows tool requires actions:read permission
func TestImportAgenticWorkflowsRequiresPermissions(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with agentic-workflows tool
	sharedPath := filepath.Join(tempDir, "shared-aw.md")
	sharedContent := `---
description: "Shared agentic-workflows configuration"
tools:
  agentic-workflows: true
permissions:
  actions: read
---

# Shared Agentic Workflows Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow WITHOUT actions:read permission
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-aw.md
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Main Workflow

Missing actions:read permission.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow - should fail due to missing permission
	compiler := workflow.NewCompiler()
	err := compiler.CompileWorkflow(workflowPath)

	if err == nil {
		t.Fatal("Expected CompileWorkflow to fail due to missing actions:read permission")
	}

	// Verify error message mentions permissions
	if !strings.Contains(err.Error(), "actions: read") {
		t.Errorf("Expected error to mention 'actions: read', got: %v", err)
	}
}

// TestImportEditTool tests that edit tool can be imported from a shared workflow
func TestImportEditTool(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with edit tool
	sharedPath := filepath.Join(tempDir, "shared-edit.md")
	sharedContent := `---
description: "Shared edit tool configuration"
tools:
  edit:
---

# Shared Edit Tool Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports edit tool
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-edit.md
permissions:
  contents: read
---

# Main Workflow

Uses imported edit tool.
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

	// Verify edit tool functionality is present
	// The edit tool enables --allow-all-paths flag in Copilot
	if !strings.Contains(workflowData, "--allow-all-paths") {
		t.Error("Expected compiled workflow to contain --allow-all-paths flag for edit tool")
	}
}

// TestImportWebFetchTool tests that web-fetch tool can be imported from a shared workflow
func TestImportWebFetchTool(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with web-fetch tool
	sharedPath := filepath.Join(tempDir, "shared-web-fetch.md")
	sharedContent := `---
description: "Shared web-fetch tool configuration"
tools:
  web-fetch:
---

# Shared Web Fetch Tool Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports web-fetch tool
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-web-fetch.md
permissions:
  contents: read
---

# Main Workflow

Uses imported web-fetch tool.
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

	// Verify compilation succeeded
	// Note: Copilot has built-in web-fetch support, so no explicit MCP configuration is needed
	// The test verifies that the workflow compiles successfully when web-fetch is imported
	_ = lockFileContent // Compilation success is sufficient verification
}

// TestImportWebSearchTool tests that web-search tool can be imported from a shared workflow
func TestImportWebSearchTool(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with web-search tool
	sharedPath := filepath.Join(tempDir, "shared-web-search.md")
	sharedContent := `---
description: "Shared web-search tool configuration"
tools:
  web-search:
---

# Shared Web Search Tool Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports web-search tool
	// Use Claude engine since Copilot doesn't support web-search
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: claude
imports:
  - shared-web-search.md
permissions:
  contents: read
---

# Main Workflow

Uses imported web-search tool.
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

	// Verify web-search tool is configured
	// For Claude, web-search is a native tool capability
	if !strings.Contains(workflowData, "WebSearch") {
		t.Error("Expected compiled workflow to contain WebSearch tool configuration for Claude")
	}
}

// TestImportTimeoutTool tests that timeout tool setting can be imported from a shared workflow
func TestImportTimeoutTool(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with timeout setting
	sharedPath := filepath.Join(tempDir, "shared-timeout.md")
	sharedContent := `---
description: "Shared timeout configuration"
tools:
  timeout: 90
---

# Shared Timeout Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports timeout setting
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-timeout.md
permissions:
  contents: read
---

# Main Workflow

Uses imported timeout setting.
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

	// Verify timeout is configured
	// The timeout setting sets environment variables for MCP and bash tools
	hasTimeout := strings.Contains(workflowData, "MCP_TOOL_TIMEOUT") ||
		strings.Contains(workflowData, "90000") ||
		strings.Contains(workflowData, "GH_AW_TOOL_TIMEOUT")
	if !hasTimeout {
		t.Error("Expected compiled workflow to contain timeout configuration (90 seconds)")
	}
}

// TestImportStartupTimeoutTool tests that startup-timeout tool setting can be imported from a shared workflow
func TestImportStartupTimeoutTool(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with startup-timeout setting
	sharedPath := filepath.Join(tempDir, "shared-startup-timeout.md")
	sharedContent := `---
description: "Shared startup-timeout configuration"
tools:
  startup-timeout: 60
---

# Shared Startup Timeout Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports startup-timeout setting
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-startup-timeout.md
permissions:
  contents: read
---

# Main Workflow

Uses imported startup-timeout setting.
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

	// Verify startup-timeout is configured
	// The startup-timeout setting sets environment variables for MCP startup
	hasStartupTimeout := strings.Contains(workflowData, "MCP_TIMEOUT") ||
		strings.Contains(workflowData, "60000") ||
		strings.Contains(workflowData, "GH_AW_STARTUP_TIMEOUT")
	if !hasStartupTimeout {
		t.Error("Expected compiled workflow to contain startup-timeout configuration (60 seconds)")
	}
}

// TestImportMultipleNeutralTools tests importing multiple neutral tools together
func TestImportMultipleNeutralTools(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with multiple neutral tools
	sharedPath := filepath.Join(tempDir, "shared-neutral-tools.md")
	sharedContent := `---
description: "Shared configuration with multiple neutral tools"
tools:
  edit:
  web-fetch:
  safety-prompt: true
  timeout: 120
  startup-timeout: 90
---

# Shared Neutral Tools Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports all neutral tools
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-neutral-tools.md
permissions:
  contents: read
---

# Main Workflow

Uses all imported neutral tools.
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

	// Verify edit tool is present (--allow-all-paths flag)
	if !strings.Contains(workflowData, "--allow-all-paths") {
		t.Error("Expected compiled workflow to contain --allow-all-paths flag for edit tool")
	}

	// Note: web-fetch has built-in Copilot support, so no explicit MCP configuration is needed
	// The test verifies that web-fetch compiles successfully when imported

	// Verify timeout is configured (120 seconds)
	hasTimeout := strings.Contains(workflowData, "120000") ||
		strings.Contains(workflowData, "MCP_TOOL_TIMEOUT") ||
		strings.Contains(workflowData, "GH_AW_TOOL_TIMEOUT")
	if !hasTimeout {
		t.Error("Expected compiled workflow to contain timeout configuration (120 seconds)")
	}

	// Verify startup-timeout is configured (90 seconds)
	hasStartupTimeout := strings.Contains(workflowData, "90000") ||
		strings.Contains(workflowData, "MCP_TIMEOUT") ||
		strings.Contains(workflowData, "GH_AW_STARTUP_TIMEOUT")
	if !hasStartupTimeout {
		t.Error("Expected compiled workflow to contain startup-timeout configuration (90 seconds)")
	}
}

// TestImportSerenaLocalMode tests serena with local mode using uvx
func TestImportSerenaLocalMode(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with serena in local mode
	sharedPath := filepath.Join(tempDir, "shared-serena-local.md")
	sharedContent := `---
description: "Shared serena in local mode"
tools:
  serena:
    mode: local
    languages:
      go: {}
---

# Shared Serena Local Mode Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports serena in local mode
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-serena-local.md
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Main Workflow

Uses imported serena in local mode.
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

	// Verify serena is configured in the MCP config
	if !strings.Contains(workflowData, `"serena"`) {
		t.Error("Expected compiled workflow to contain serena tool")
	}

	// Verify HTTP transport is used (not container)
	if !strings.Contains(workflowData, `"type": "http"`) {
		t.Error("Expected serena local mode to use HTTP transport")
	}

	// Verify port configuration
	if !strings.Contains(workflowData, "GH_AW_SERENA_PORT") {
		t.Error("Expected serena local mode to have port configuration")
	}

	// Verify serena startup steps are present
	if !strings.Contains(workflowData, "Start Serena MCP HTTP Server") {
		t.Error("Expected serena local mode to have startup step")
	}

	// Verify shell script is called
	if !strings.Contains(workflowData, "start_serena_server.sh") {
		t.Error("Expected serena local mode to call start_serena_server.sh")
	}

	// Verify language runtime setup (Go in this case)
	if !strings.Contains(workflowData, "Setup Go") {
		t.Error("Expected serena local mode to setup Go runtime")
	}

	// Verify uv runtime setup
	if !strings.Contains(workflowData, "Setup uv") {
		t.Error("Expected serena local mode to setup uv runtime")
	}

	// Verify NO container is used
	if strings.Contains(workflowData, "ghcr.io/github/serena-mcp-server:latest") {
		t.Error("Did not expect serena local mode to use Docker container")
	}
}

// TestImportSerenaLocalModeMultipleLanguages tests serena local mode with multiple languages
func TestImportSerenaLocalModeMultipleLanguages(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow with serena in local mode with multiple languages
	sharedPath := filepath.Join(tempDir, "shared-serena-multi.md")
	sharedContent := `---
description: "Shared serena in local mode with multiple languages"
tools:
  serena:
    mode: local
    languages:
      go:
        version: "1.21"
      typescript: {}
      python: {}
---

# Shared Serena Local Mode Multiple Languages
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow
	workflowPath := filepath.Join(tempDir, "main-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-serena-multi.md
permissions:
  contents: read
  issues: read
---

# Main Workflow
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

	// Verify multiple language runtimes are setup
	if !strings.Contains(workflowData, "Setup Go") {
		t.Error("Expected Go runtime setup")
	}
	if !strings.Contains(workflowData, "Setup Node.js") {
		t.Error("Expected Node.js runtime setup for TypeScript")
	}
	if !strings.Contains(workflowData, "Setup Python") {
		t.Error("Expected Python runtime setup")
	}

	// Verify HTTP transport
	if !strings.Contains(workflowData, `"type": "http"`) {
		t.Error("Expected HTTP transport for serena local mode")
	}
}
