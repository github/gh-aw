//go:build integration

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

// TestSandboxRuntimeExperimentalWarning tests that the sandbox-runtime feature
// emits an experimental warning when enabled.
func TestSandboxRuntimeExperimentalWarning(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectWarning bool
	}{
		{
			name: "sandbox-runtime enabled produces experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
sandbox: sandbox-runtime
features:
  sandbox-runtime: true
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "sandbox default does not produce experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
sandbox: default
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "no sandbox config does not produce experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "sandbox-runtime with custom config produces experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
sandbox:
  type: sandbox-runtime
  config:
    filesystem:
      allowWrite:
        - "."
        - "/tmp"
features:
  sandbox-runtime: true
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
`,
			expectWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "sandbox-experimental-warning-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Capture stderr to check for warnings
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			compiler := NewCompiler()
			compiler.SetStrictMode(false)
			err := compiler.CompileWorkflow(testFile)

			// Restore stderr
			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			io.Copy(&buf, r)
			stderrOutput := buf.String()

			if err != nil {
				t.Errorf("Expected compilation to succeed but it failed: %v", err)
				return
			}

			expectedMessage := "Using experimental feature: sandbox-runtime firewall"

			if tt.expectWarning {
				if !strings.Contains(stderrOutput, expectedMessage) {
					t.Errorf("Expected warning containing '%s', got stderr:\n%s", expectedMessage, stderrOutput)
				}
			} else {
				if strings.Contains(stderrOutput, expectedMessage) {
					t.Errorf("Did not expect warning '%s', but got stderr:\n%s", expectedMessage, stderrOutput)
				}
			}

			// Verify warning count includes sandbox-runtime warning
			if tt.expectWarning {
				warningCount := compiler.GetWarningCount()
				if warningCount == 0 {
					t.Error("Expected warning count > 0 but got 0")
				}
			}
		})
	}
}

// TestSandboxRuntimeFeatureFlagRequired tests that sandbox-runtime requires
// the feature flag to be enabled, otherwise compilation fails.
func TestSandboxRuntimeFeatureFlagRequired(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		expectError  bool
		errorMessage string
	}{
		{
			name: "sandbox-runtime without feature flag fails",
			content: `---
on: workflow_dispatch
engine: copilot
sandbox: sandbox-runtime
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
`,
			expectError:  true,
			errorMessage: "sandbox-runtime feature is experimental and requires the feature flag to be enabled",
		},
		{
			name: "sandbox-runtime with feature flag succeeds",
			content: `---
on: workflow_dispatch
engine: copilot
sandbox: sandbox-runtime
features:
  sandbox-runtime: true
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
`,
			expectError: false,
		},
		{
			name: "sandbox-runtime with feature flag disabled fails",
			content: `---
on: workflow_dispatch
engine: copilot
sandbox: sandbox-runtime
features:
  sandbox-runtime: false
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
`,
			expectError:  true,
			errorMessage: "sandbox-runtime feature is experimental and requires the feature flag to be enabled",
		},
		{
			name: "sandbox default does not require feature flag",
			content: `---
on: workflow_dispatch
engine: copilot
sandbox: default
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "sandbox-feature-flag-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Capture stderr
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			compiler := NewCompiler()
			compiler.SetStrictMode(false)
			err := compiler.CompileWorkflow(testFile)

			// Restore stderr
			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			io.Copy(&buf, r)

			if tt.expectError {
				if err == nil {
					t.Error("Expected compilation to fail but it succeeded")
					return
				}
				if !strings.Contains(err.Error(), tt.errorMessage) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorMessage, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected compilation to succeed but it failed: %v", err)
				}
			}
		})
	}
}

// TestSandboxRuntimeFeatureFlagViaEnv tests that the sandbox-runtime feature
// can be enabled via the GH_AW_FEATURES environment variable.
func TestSandboxRuntimeFeatureFlagViaEnv(t *testing.T) {
	content := `---
on: workflow_dispatch
engine: copilot
sandbox: sandbox-runtime
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
`

	tmpDir := testutil.TempDir(t, "sandbox-feature-flag-env-test")

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Set the feature flag via environment variable
	t.Setenv("GH_AW_FEATURES", "sandbox-runtime")

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	compiler := NewCompiler()
	compiler.SetStrictMode(false)
	err := compiler.CompileWorkflow(testFile)

	// Restore stderr
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Errorf("Expected compilation to succeed with GH_AW_FEATURES=sandbox-runtime but it failed: %v", err)
	}
}
