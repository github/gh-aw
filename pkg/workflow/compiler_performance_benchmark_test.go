//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkCompileSimpleWorkflow benchmarks compilation of a simple workflow
// Baseline target: <100ms for simple workflows
func BenchmarkCompileSimpleWorkflow(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-simple")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: issues
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
tools:
  bash: ["echo", "cat"]
timeout-minutes: 5
---

# Simple Issue Handler

Analyze the issue: ${{ needs.activation.outputs.text }}
`

	testFile := filepath.Join(tmpDir, "simple.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkCompileComplexWorkflow benchmarks compilation of a complex workflow
// Baseline target: <500ms for complex workflows
func BenchmarkCompileComplexWorkflow(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-complex")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on:
  pull_request:
    types: [opened, synchronize, reopened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine:
  id: copilot
  max-turns: 5
mcp-servers:
  github:
    mode: remote
    toolsets: [default, actions]
network:
  allowed:
    - defaults
    - python
tools:
  edit:
  bash:
    - "git status"
    - "git diff"
safe-outputs:
  create-pull-request:
    title-prefix: "[ai] "
    labels: [automation]
  add-comment:
    max: 3
timeout-minutes: 20
---

# Complex PR Review

Review the pull request: ${{ github.event.pull_request.number }}
`

	testFile := filepath.Join(tmpDir, "complex.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkCompileMCPWorkflow benchmarks compilation of a workflow with multiple MCP servers
// Baseline target: <1s for MCP-heavy workflows
func BenchmarkCompileMCPWorkflow(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-mcp")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: pull_request
permissions:
  contents: read
  pull-requests: write
engine: copilot
features:
  dangerous-permissions-write: true
mcp-servers:
  github:
    mode: remote
    toolsets: [default, actions, discussions]
  playwright:
    container: "mcr.microsoft.com/playwright:v1.41.0"
    allowed-domains: ["github.com"]
  cache-memory:
    key: pr-${{ github.run_id }}
tools:
  edit:
  bash: ["git status", "git diff"]
timeout-minutes: 15
---

# MCP-Heavy Workflow

Review and test the pull request with multiple tools.
`

	testFile := filepath.Join(tmpDir, "mcp.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkCompileMemoryUsage benchmarks memory allocations for typical workflows
// This helps identify memory hotspots and potential optimization opportunities
func BenchmarkCompileMemoryUsage(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-memory")
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
    toolsets: [default]
network:
  allowed: [defaults]
tools:
  edit:
  bash: ["git status"]
safe-outputs:
  add-comment:
    max: 2
timeout-minutes: 10
---

# Memory Benchmark Workflow

Standard workflow for memory profiling.
`

	testFile := filepath.Join(tmpDir, "memory.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()

	// Track memory allocations
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkParseWorkflow benchmarks just the parsing phase
func BenchmarkParseWorkflow(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-parse")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: issues
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
tools:
  bash: ["echo"]
---

# Parse Benchmark

Test parsing performance.
`

	testFile := filepath.Join(tmpDir, "parse.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = compiler.ParseWorkflowFile(testFile)
	}
}

// BenchmarkValidation benchmarks the validation phase
func BenchmarkValidation(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-validate")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: pull_request
permissions:
  contents: read
  pull-requests: write
engine: copilot
features:
  dangerous-permissions-write: true
mcp-servers:
  github:
    mode: remote
    toolsets: [default]
strict: true
tools:
  bash: ["git status"]
timeout-minutes: 10
---

# Validation Benchmark

Test validation performance.
`

	testFile := filepath.Join(tmpDir, "validate.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()
	compiler.SetStrictMode(true)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}

// BenchmarkYAMLGeneration benchmarks YAML generation from workflow data
func BenchmarkYAMLGeneration(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-yaml")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: push
permissions:
  contents: read
engine: claude
tools:
  bash: ["echo"]
---

# YAML Generation Benchmark

Test YAML generation.
`

	testFile := filepath.Join(tmpDir, "yaml.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()
	compiler.SetNoEmit(true)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = compiler.CompileWorkflow(testFile)
	}
}
