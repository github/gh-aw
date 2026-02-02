//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
)

// BenchmarkCompileWithActionlint benchmarks workflow compilation with actionlint validation
// This measures the performance overhead of running actionlint during compilation
func BenchmarkCompileWithActionlint(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-actionlint")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a realistic workflow that will be linted
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
strict: false
tools:
  github:
    allowed: [pull_request_read]
  bash: ["git status", "git diff"]
safe-outputs:
  add-comment:
    max: 3
timeout-minutes: 15
---

# PR Review Workflow

Review the pull request and provide feedback.

PR Number: ${{ github.event.pull_request.number }}
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := workflow.NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Compile with actionlint enabled (per-file mode for benchmarking)
		_ = CompileWorkflowWithValidation(compiler, testFile, false, false, false, true, false, false)
	}
}

// BenchmarkCompileWithZizmor benchmarks workflow compilation with zizmor security scanning
// This measures the performance overhead of running zizmor during compilation
func BenchmarkCompileWithZizmor(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-zizmor")
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
strict: false
tools:
  github:
    allowed: [issue_read]
  bash: ["echo", "cat"]
safe-outputs:
  add-comment:
    max: 3
timeout-minutes: 10
---

# Issue Analysis Workflow

Analyze the issue and provide feedback.

Issue: ${{ needs.activation.outputs.text }}
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := workflow.NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Compile with zizmor enabled
		_ = CompileWorkflowWithValidation(compiler, testFile, false, true, false, false, false, false)
	}
}

// BenchmarkCompileWithPoutine benchmarks workflow compilation with poutine security scanning
// This measures the performance overhead of running poutine during compilation
func BenchmarkCompileWithPoutine(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-poutine")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on:
  push:
    branches: [main]
permissions:
  contents: read
engine: copilot
strict: false
tools:
  github:
    allowed: [get_repository, list_commits]
  bash: ["git log"]
timeout-minutes: 10
---

# Repository Analysis Workflow

Analyze repository commits and provide insights.

Repository: ${{ github.repository }}
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := workflow.NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Compile with poutine enabled
		_ = CompileWorkflowWithValidation(compiler, testFile, false, false, true, false, false, false)
	}
}

// BenchmarkCompileWithAllSecurityTools benchmarks workflow compilation with all security tools enabled
// This measures the combined performance overhead of actionlint, zizmor, and poutine
func BenchmarkCompileWithAllSecurityTools(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-all-security")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a more complex workflow to get realistic security scanning metrics
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
strict: false
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
    - "npm test"
safe-outputs:
  create-pull-request:
    title-prefix: "[ai-review] "
    labels: [automation, ai-generated]
    draft: true
  add-comment:
    max: 3
timeout-minutes: 25
---

# Comprehensive PR Review Workflow

Review pull request with security checks enabled.

PR Details:
- Number: ${{ github.event.pull_request.number }}
- Author: ${{ github.event.pull_request.user.login }}
- Repository: ${{ github.repository }}
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := workflow.NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Compile with all security tools enabled (zizmor, poutine, actionlint)
		_ = CompileWorkflowWithValidation(compiler, testFile, false, true, true, true, false, false)
	}
}

// BenchmarkCompileNoSecurityTools benchmarks workflow compilation without any security tools
// This provides a baseline for comparison with security-enabled benchmarks
func BenchmarkCompileNoSecurityTools(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-no-security")
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
strict: false
tools:
  github:
    allowed: [pull_request_read]
  bash: ["git status", "git diff"]
safe-outputs:
  add-comment:
    max: 3
timeout-minutes: 15
---

# Baseline PR Review Workflow

Review the pull request without security scanning overhead.

PR Number: ${{ github.event.pull_request.number }}
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := workflow.NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Compile without any security tools
		_ = CompileWorkflowWithValidation(compiler, testFile, false, false, false, false, false, false)
	}
}

// BenchmarkBatchActionlint benchmarks batch actionlint validation on multiple lock files
// This tests the performance of the batch linting feature
func BenchmarkBatchActionlint(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-batch-actionlint")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multiple workflow files to simulate batch processing
	workflowTemplate := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
tools:
  github:
    allowed: [issue_read]
safe-outputs:
  add-comment:
    max: 3
timeout-minutes: 10
---

# Workflow

Process issue.
`

	var lockFiles []string
	compiler := workflow.NewCompiler()

	// Create 5 workflows
	for i := 1; i <= 5; i++ {
		testFile := filepath.Join(tmpDir, filepath.Base(tmpDir)+"-workflow-"+string(rune('0'+i))+".md")
		// workflowTemplate is a simple string without format placeholders now
		content := []byte(workflowTemplate)
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			b.Fatal(err)
		}

		// Compile each workflow first to generate lock files
		if err := compiler.CompileWorkflow(testFile); err != nil {
			b.Fatal(err)
		}

		lockFile := filepath.Join(tmpDir, filepath.Base(tmpDir)+"-workflow-"+string(rune('0'+i))+".lock.yml")
		lockFiles = append(lockFiles, lockFile)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Run batch actionlint on all lock files
		_ = RunActionlintOnFiles(lockFiles, false, false)
	}
}

// BenchmarkCompileComplexWithSecurityTools benchmarks complex workflow compilation with security tools
// This tests performance on a feature-rich workflow with all security validations
func BenchmarkCompileComplexWithSecurityTools(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench-complex-security")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a complex workflow with multiple features
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
permissions:
  contents: read
  issues: write
  pull-requests: write
  deployments: write
engine:
  id: copilot
  max-turns: 10
strict: false
mcp-servers:
  github:
    mode: remote
    toolsets: [default, actions, deployments]
  playwright:
    container: "mcr.microsoft.com/playwright:v1.41.0"
    allowed-domains: ["github.com", "*.github.io"]
network:
  allowed:
    - defaults
    - python
    - node
    - containers
tools:
  edit:
  bash:
    - "git status"
    - "git diff"
    - "npm install"
    - "npm test"
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
timeout-minutes: 30
---

# Complex Deployment Workflow

Deploy to environment with comprehensive validation.

Target Environment: ${{ github.event.inputs.environment }}
Repository: ${{ github.repository }}
Triggered by: ${{ github.actor }}
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := workflow.NewCompiler()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Compile with all security tools enabled
		_ = CompileWorkflowWithValidation(compiler, testFile, false, true, true, true, false, false)
	}
}
