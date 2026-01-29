//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkCompileWorkflow benchmarks full workflow compilation with basic configuration
func BenchmarkCompileWorkflow(b *testing.B) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "benchmark-workflow")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a realistic workflow file
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read, add_issue_comment, list_issues]
  bash: ["echo", "ls", "cat"]
timeout-minutes: 10
---

# Issue Analysis Workflow

Analyze the issue and provide helpful feedback.

Issue details: ${{ needs.activation.outputs.text }}
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkCompileWorkflow_WithMCP benchmarks workflow compilation with MCP servers
func BenchmarkCompileWorkflow_WithMCP(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-workflow-mcp")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on:
  pull_request:
    types: [opened, synchronize]
permissions:
  contents: read
  pull-requests: write
engine: copilot
features:
  dangerous-permissions-write: true
mcp-servers:
  github:
    mode: remote
    toolsets: [default, actions]
  playwright:
    container: "mcr.microsoft.com/playwright:v1.41.0"
    allowed-domains: ["github.com", "*.github.io"]
tools:
  edit:
  bash: ["git status", "git diff"]
timeout-minutes: 15
---

# PR Review Agent

Review the pull request changes and provide feedback.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkCompileWorkflow_WithImports benchmarks workflow compilation with imports
func BenchmarkCompileWorkflow_WithImports(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-workflow-imports")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create shared import file
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		b.Fatal(err)
	}

	sharedContent := `---
tools:
  web-fetch: true
  web-search: true
---

Use web search and fetch tools to gather information.
`
	if err := os.WriteFile(filepath.Join(sharedDir, "web-tools.md"), []byte(sharedContent), 0644); err != nil {
		b.Fatal(err)
	}

	testContent := `---
on:
  schedule:
    - cron: "0 9 * * 1"
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
imports:
  - shared/web-tools.md
timeout-minutes: 20
---

# Weekly Research Report

Research latest developments and create a summary.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkCompileWorkflow_WithValidate benchmarks workflow compilation with validation enabled
func BenchmarkCompileWorkflow_WithValidate(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-workflow-validate")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read, add_issue_comment]
strict: true
timeout-minutes: 10
---

# Issue Analysis with Validation

Analyze the issue with strict validation enabled.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler(
		WithVersion("test"),
		WithStrictMode(true),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkCompileWorkflow_Complex benchmarks workflow compilation with complex configuration
func BenchmarkCompileWorkflow_Complex(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-workflow-complex")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on:
  pull_request:
    types: [opened, synchronize, reopened]
    forks: ["org/*", "trusted/repo"]
permissions:
  contents: read
  issues: write
  pull-requests: write
  actions: read
engine:
  id: copilot
  max-turns: 5
  max-concurrency: 3
mcp-servers:
  github:
    mode: remote
    toolsets: [default, actions, discussions]
  cache-memory:
    key: pr-review-${{ github.run_id }}
network:
  allowed:
    - defaults
    - python
    - node
  firewall: true
tools:
  edit:
  bash:
    - "git status"
    - "git diff"
    - "npm test"
safe-outputs:
  create-pull-request:
    title-prefix: "[ai-review] "
    labels: [automation, ai-generated]
    draft: true
  add-comment:
    max: 3
timeout-minutes: 30
concurrency:
  group: pr-review-${{ github.event.pull_request.number }}
  cancel-in-progress: true
---

# Complex PR Review Workflow

Comprehensive pull request review with multiple features enabled.

PR Number: ${{ github.event.pull_request.number }}
Repository: ${{ github.repository }}
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkGenerateYAML benchmarks YAML generation from workflow data
func BenchmarkGenerateYAML(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-yaml")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: push
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [get_repository, list_commits]
---

# Simple Workflow

Analyze repository commits.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler(
		WithVersion("test"),
		WithNoEmit(true), // Don't write files
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkGenerateYAML_Complex benchmarks YAML generation with complex nested structures
func BenchmarkGenerateYAML_Complex(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-yaml-complex")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on:
  workflow_dispatch:
    inputs:
      environment:
        description: 'Target environment'
        required: true
        type: choice
        options:
          - development
          - staging
          - production
      debug:
        description: 'Enable debug mode'
        type: boolean
        default: false
permissions:
  contents: read
  issues: write
  pull-requests: write
  deployments: write
engine:
  id: copilot
  max-turns: 10
mcp-servers:
  github:
    mode: remote
    toolsets: [default, actions, deployments]
network:
  allowed:
    - defaults
    - python
    - node
    - containers
safe-outputs:
  create-issues:
    title-prefix: "[deployment] "
    labels: [deployment, automation]
    max: 5
  create-discussions:
    category: "deployments"
    max: 1
  add-comments:
    max: 3
    target: "*"
  create-pull-requests:
    title-prefix: "[ai] "
    labels: [automation]
    draft: true
steps:
  - name: Setup environment
    env:
      ENVIRONMENT: ${{ github.event.inputs.environment }}
      DEBUG: ${{ github.event.inputs.debug }}
    run: echo "Setting up $ENVIRONMENT"
post-steps:
  - name: Cleanup
    run: echo "Cleaning up resources"
---

# Complex Deployment Workflow

Deploy to environment: ${{ github.event.inputs.environment }}
Debug mode: ${{ github.event.inputs.debug }}
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler(
		WithVersion("test"),
		WithNoEmit(true), // Don't write files
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}
