//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

// TestCompileWorkflow_HostedWebPolicy verifies that network.hosted-web frontmatter
// compiles end-to-end into the AWF config embedded in the generated lock file,
// covering the Claude/Codex allow/block cases, the deny-by-default fallback, and
// the presence-or-false enable semantics.
func TestCompileWorkflow_HostedWebPolicy(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "claude allowlist enables hostedWeb with allowedDomains and maxUses",
			frontmatter: `---
on: workflow_dispatch
engine: claude
network:
  allowed:
    - defaults
  hosted-web:
    allowed:
      - docs.github.com
    max-uses: 5
---

# Test
Test workflow.`,
			wantContains: []string{
				`\"hostedWeb\"`,
				`\"claude\"`,
				`\"enabled\":true`,
				`\"allowedDomains\":[\"docs.github.com\"]`,
				`\"maxUses\":5`,
			},
		},
		{
			name: "codex blocklist enables hostedWeb with blockedDomains",
			frontmatter: `---
on: workflow_dispatch
engine: codex
network:
  allowed:
    - defaults
  hosted-web:
    blocked:
      - example.com
---

# Test
Test workflow.`,
			wantContains: []string{
				`\"hostedWeb\"`,
				`\"codex\"`,
				`\"enabled\":true`,
				`\"blockedDomains\":[\"example.com\"]`,
			},
		},
		{
			name: "claude without hosted-web defaults to deny",
			frontmatter: `---
on: workflow_dispatch
engine: claude
network:
  allowed:
    - defaults
---

# Test
Test workflow.`,
			wantContains: []string{
				`\"hostedWeb\"`,
				`\"claude\":{\"enabled\":false}`,
			},
		},
		{
			name: "hosted-web: false disables the policy",
			frontmatter: `---
on: workflow_dispatch
engine: claude
network:
  allowed:
    - defaults
  hosted-web: false
---

# Test
Test workflow.`,
			wantContains: []string{
				`\"hostedWeb\"`,
				`\"claude\":{\"enabled\":false}`,
			},
		},
		{
			name: "no network configuration omits hostedWeb entirely",
			frontmatter: `---
on: workflow_dispatch
engine: claude
---

# Test
Test workflow.`,
			wantAbsent: []string{
				`\"hostedWeb\"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "hosted-web-compile-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			lockFile := stringutil.MarkdownToLockFile(testFile)
			yamlBytes, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			yamlStr := string(yamlBytes)

			for _, want := range tt.wantContains {
				if !strings.Contains(yamlStr, want) {
					t.Errorf("Expected lock file to contain %q, but it did not.\n%s", want, yamlStr)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(yamlStr, absent) {
					t.Errorf("Expected lock file to NOT contain %q, but it did", absent)
				}
			}
		})
	}
}

// TestCompileWorkflow_HostedWebRejectsUnsupportedEngine verifies that declaring
// network.hosted-web for an engine other than Claude/Codex fails compilation
// instead of silently dropping the restriction.
func TestCompileWorkflow_HostedWebRejectsUnsupportedEngine(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
network:
  allowed:
    - defaults
  hosted-web:
    allowed:
      - docs.github.com
---

# Test
Test workflow.`

	tmpDir := testutil.TempDir(t, "hosted-web-unsupported-engine-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("Expected compilation to fail for hosted-web on an unsupported engine")
	}
	if !strings.Contains(err.Error(), "hosted-web") {
		t.Errorf("Expected error to mention hosted-web, got: %v", err)
	}
}

// TestCompileWorkflow_HostedWebRejectsOldAWFVersion verifies that pinning an AWF
// firewall version older than constants.AWFHostedWebMinVersion fails compilation
// when network.hosted-web is declared, rather than silently omitting the policy.
func TestCompileWorkflow_HostedWebRejectsOldAWFVersion(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: claude
sandbox:
  agent:
    id: awf
    version: v0.28.24
network:
  allowed:
    - defaults
  hosted-web:
    allowed:
      - docs.github.com
---

# Test
Test workflow.`

	tmpDir := testutil.TempDir(t, "hosted-web-old-awf-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("Expected compilation to fail for hosted-web pinned to an AWF version that predates support")
	}
	if !strings.Contains(err.Error(), "network.hosted-web") {
		t.Errorf("Expected error to reference network.hosted-web, got: %v", err)
	}
}
