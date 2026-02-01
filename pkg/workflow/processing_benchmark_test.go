//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkProcessToolsSimple benchmarks simple tool configuration via compilation
func BenchmarkProcessToolsSimple(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-tools-simple")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: push
permissions:
  contents: read
engine: claude
strict: false
tools:
  github:
    allowed: [issue_read, add_issue_comment]
  bash: ["echo", "ls"]
  edit:
---

# Test Workflow

Simple tool processing test.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiler.ParseWorkflowFile(testFile)
	}
}

// BenchmarkProcessToolsComplex benchmarks complex tool configuration
func BenchmarkProcessToolsComplex(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-tools-complex")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
tools:
  github:
    mode: remote
    toolsets: [default, actions, discussions]
  bash:
    - "echo"
    - "ls"
    - "git status"
    - "git diff"
    - "npm test"
  edit:
  web-fetch:
  web-search:
  playwright:
    container: "mcr.microsoft.com/playwright:v1.41.0"
    allowed-domains: ["github.com", "*.github.io"]
---

# Complex Tools Test

Complex tool configuration processing.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiler.ParseWorkflowFile(testFile)
	}
}

// BenchmarkProcessSafeOutputsSimple benchmarks simple safe outputs processing
func BenchmarkProcessSafeOutputsSimple(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-safe-outputs-simple")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: push
permissions:
  contents: read
engine: claude
strict: false
safe-outputs:
  create-issues:
    title-prefix: "[ai] "
    labels: [automation]
  add-comments:
    max: 3
---

# Safe Outputs Test

Simple safe outputs configuration.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiler.ParseWorkflowFile(testFile)
	}
}

// BenchmarkProcessSafeOutputsComplex benchmarks complex safe outputs processing
func BenchmarkProcessSafeOutputsComplex(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-safe-outputs-complex")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: pull_request
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-issues:
    title-prefix: "[ai] "
    labels: [automation, ai-generated, bug]
    max: 5
  create-discussions:
    title-prefix: "[report] "
    category: "general"
    max: 3
  add-comments:
    max: 3
    target: "*"
  create-pull-requests:
    title-prefix: "[bot] "
    labels: [automation]
    draft: true
  update-issues:
    status: true
    title: true
    body: true
    max: 3
---

# Complex Safe Outputs

Complex safe outputs configuration.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiler.ParseWorkflowFile(testFile)
	}
}

// BenchmarkProcessNetworkPermissions benchmarks network permission processing
func BenchmarkProcessNetworkPermissions(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-network")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
network:
  allowed:
    - defaults
    - python
    - node
    - github.com
    - "*.github.io"
  firewall: true
---

# Network Test

Network permissions processing.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiler.ParseWorkflowFile(testFile)
	}
}

// BenchmarkProcessPermissions benchmarks permission configuration processing
func BenchmarkProcessPermissions(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-permissions")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: write
  actions: read
  discussions: write
  deployments: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Permissions Test

Permission processing test.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiler.ParseWorkflowFile(testFile)
	}
}

// BenchmarkProcessRoles benchmarks role configuration processing
func BenchmarkProcessRoles(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "benchmark-roles")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testContent := `---
on: issues
roles: [admin, maintainer, write, read]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Roles Test

Role processing test.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		b.Fatal(err)
	}

	compiler := NewCompiler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiler.ParseWorkflowFile(testFile)
	}
}
