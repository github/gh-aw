//go:build !integration

package workflow

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestPushToPullRequestBranchWildcardFetchWarning tests that a warning is emitted
// when push-to-pull-request-branch has target: "*" but no wildcard fetch in checkout.
func TestPushToPullRequestBranchWildcardFetchWarning(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectWarning bool
		warningText   string
	}{
		{
			name: "target=* without wildcard fetch emits warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
    title-prefix: "[bot] "
    required-labels: [automated]
---

# Test Workflow
`,
			expectWarning: true,
			warningText:   "push-to-pull-request-branch: target: \"*\" requires that all PR branches are fetched at checkout",
		},
		{
			name: "target=* with fetch=[*] does not emit warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
    title-prefix: "[bot] "
    required-labels: [automated]
checkout:
  fetch: ["*"]
  fetch-depth: 0
---

# Test Workflow
`,
			expectWarning: false,
			warningText:   "push-to-pull-request-branch: target: \"*\" requires that all PR branches are fetched at checkout",
		},
		{
			name: "target=* with wildcard glob fetch does not emit warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
    title-prefix: "[bot] "
    required-labels: [automated]
checkout:
  fetch: ["feature/*"]
---

# Test Workflow
`,
			expectWarning: false,
			warningText:   "push-to-pull-request-branch: target: \"*\" requires that all PR branches are fetched at checkout",
		},
		{
			name: "target=triggering does not emit warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
---

# Test Workflow
`,
			expectWarning: false,
			warningText:   "push-to-pull-request-branch: target: \"*\" requires that all PR branches are fetched at checkout",
		},
		{
			name: "no push-to-pull-request-branch does not emit warning",
			content: `---
on: push
safe-outputs:
  add-comment:
    max: 1
---

# Test Workflow
`,
			expectWarning: false,
			warningText:   "push-to-pull-request-branch: target: \"*\" requires that all PR branches are fetched at checkout",
		},
		{
			name: "target=* with non-wildcard fetch emits warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
    title-prefix: "[bot] "
    required-labels: [automated]
checkout:
  fetch: ["main"]
---

# Test Workflow
`,
			expectWarning: true,
			warningText:   "push-to-pull-request-branch: target: \"*\" requires that all PR branches are fetched at checkout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "push-pr-branch-fetch-warning-*")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Capture stderr to check for warnings
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			// Restore stderr
			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			io.Copy(&buf, r) //nolint:errcheck
			stderrOutput := buf.String()

			if err != nil {
				t.Fatalf("Expected compilation to succeed but got error: %v", err)
			}

			if tt.expectWarning {
				if !strings.Contains(stderrOutput, tt.warningText) {
					t.Errorf("Expected warning containing %q\ngot stderr:\n%s", tt.warningText, stderrOutput)
				}
			} else {
				if strings.Contains(stderrOutput, tt.warningText) {
					t.Errorf("Unexpected warning %q in stderr output:\n%s", tt.warningText, stderrOutput)
				}
			}
		})
	}
}

// TestPushToPullRequestBranchNoConstraintsWarning tests that a warning is emitted
// when push-to-pull-request-branch has target: "*" without title-prefix or required-labels.
func TestPushToPullRequestBranchNoConstraintsWarning(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectWarning bool
	}{
		{
			name: "target=* without constraints emits warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
checkout:
  fetch: ["*"]
  fetch-depth: 0
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "target=* with required-title-prefix and required-labels does not emit constraint warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
    required-title-prefix: "[bot] "
    required-labels: [automated]
checkout:
  fetch: ["*"]
  fetch-depth: 0
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "target=* with required-author does not emit constraint warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
    required-author: workflow-bot
checkout:
  fetch: ["*"]
  fetch-depth: 0
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "target=* with required-labels does not emit constraint warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
    required-labels: [automated]
checkout:
  fetch: ["*"]
  fetch-depth: 0
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "target=* with both title-prefix and required-labels does not emit constraint warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
    title-prefix: "[bot] "
    required-labels: [automated]
checkout:
  fetch: ["*"]
  fetch-depth: 0
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "target=triggering without constraints does not emit warning",
			content: `---
on: push
safe-outputs:
  push-to-pull-request-branch:
    target: "triggering"
---

# Test Workflow
`,
			expectWarning: false,
		},
	}

	const constraintWarningText = "push-to-pull-request-branch: target: \"*\" allows pushing to any PR branch with no author, title, or label constraints"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "push-pr-branch-constraint-warning-*")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Capture stderr to check for warnings
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			// Restore stderr
			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			io.Copy(&buf, r) //nolint:errcheck
			stderrOutput := buf.String()

			if err != nil {
				t.Fatalf("Expected compilation to succeed but got error: %v", err)
			}

			if tt.expectWarning {
				if !strings.Contains(stderrOutput, constraintWarningText) {
					t.Errorf("Expected warning containing %q\ngot stderr:\n%s", constraintWarningText, stderrOutput)
				}
			} else {
				if strings.Contains(stderrOutput, constraintWarningText) {
					t.Errorf("Unexpected warning %q in stderr output:\n%s", constraintWarningText, stderrOutput)
				}
			}
		})
	}
}

// TestHasWildcardFetch tests the hasWildcardFetch helper function.
func TestHasWildcardFetch(t *testing.T) {
	ref := func(s string) []string { return []string{s} }

	tests := []struct {
		name     string
		configs  []*CheckoutConfig
		expected bool
	}{
		{"nil configs", nil, false},
		{"empty configs", []*CheckoutConfig{}, false},
		{"no fetch", []*CheckoutConfig{{Repository: "owner/repo"}}, false},
		{"exact ref no wildcard", []*CheckoutConfig{{Fetch: ref("main")}}, false},
		{"star wildcard", []*CheckoutConfig{{Fetch: ref("*")}}, true},
		{"glob wildcard", []*CheckoutConfig{{Fetch: ref("feature/*")}}, true},
		{"multiple refs one wildcard", []*CheckoutConfig{{Fetch: []string{"main", "feature/*"}}}, true},
		{"multiple configs one with wildcard", []*CheckoutConfig{
			{Fetch: ref("main")},
			{Fetch: ref("*")},
		}, true},
		{"multiple configs none with wildcard", []*CheckoutConfig{
			{Fetch: ref("main")},
			{Fetch: ref("develop")},
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasWildcardFetch(tt.configs)
			if got != tt.expected {
				t.Errorf("hasWildcardFetch() = %v, want %v", got, tt.expected)
			}
		})
	}
}
