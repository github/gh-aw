//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

// TestStrictModeStepEnvSecretsAllowed tests full compilation of workflows
// that use secrets in steps and pre-steps env: bindings under strict mode.
// These should compile successfully because env: bindings are controlled,
// masked surfaces in GitHub Actions.
func TestStrictModeStepEnvSecretsAllowed(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "single secret in step env binding compiles in strict mode",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Run scan with secret
    env:
      SONAR_TOKEN: ${{ secrets.SONAR_TOKEN }}
    run: sonar-scanner
---

# Step Env Secret Test

Run a tool with a secret from env.
`,
		},
		{
			name: "multiple secrets across multiple steps compile in strict mode",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Run scan with credentials
    env:
      SONAR_TOKEN: ${{ secrets.SONAR_TOKEN }}
      CORONA_TOKEN: ${{ secrets.CORONA_TOKEN }}
    run: |
      sonar-scanner
      corona-lint check
  - name: Run auth check
    env:
      CIAM_CLIENT_ID: ${{ secrets.CIAM_CLIENT_ID }}
      CIAM_CLIENT_SECRET: ${{ secrets.CIAM_CLIENT_SECRET }}
      SI_TOKEN: ${{ secrets.SI_TOKEN }}
    run: |
      ciam-auth verify
      si-tool check
---

# Multi-Secret Step Env Test

Agent workflow with multiple tool credentials in step env bindings.
`,
		},
		{
			name: "GITHUB_TOKEN in step env alongside user secrets compiles in strict mode",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Run tool with tokens
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      API_KEY: ${{ secrets.API_KEY }}
    run: my-tool --authenticate
---

# GITHUB_TOKEN Mixed Test

Step env with both GITHUB_TOKEN and user secrets.
`,
		},
		{
			name: "pre-steps with secrets in env compile in strict mode",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
pre-steps:
  - name: Run pre-check with credentials
    env:
      SONAR_TOKEN: ${{ secrets.SONAR_TOKEN }}
    run: sonar-scanner --pre-check
---

# Pre-Steps Env Secret Test

Pre-steps with secrets in env bindings.
`,
		},
		{
			name: "secrets in with for uses action step compile in strict mode",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - uses: my-org/secrets-action@v2
    with:
      username: ${{ secrets.VAULT_USERNAME }}
      password: ${{ secrets.VAULT_PASSWORD }}
      secret_map: static-value
---

# With Secrets in Uses Action Test

Action steps with secrets in with: inputs should compile in strict mode.
`,
		},
		{
			name: "mixed env and with secrets for uses action step compile in strict mode",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - uses: my-org/secrets-action@v2
    env:
      EXTRA_TOKEN: ${{ secrets.EXTRA_TOKEN }}
    with:
      username: ${{ secrets.VAULT_USERNAME }}
---

# Mixed Env and With Secrets Test

Both env: and with: secrets in a uses: action step should compile.
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "strict-step-env-secrets-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Errorf("Expected compilation to succeed with secrets in step env bindings, but got error: %v", err)
			}

			// Verify lock file was generated
			lockFile := stringutil.MarkdownToLockFile(testFile)
			if _, err := os.Stat(lockFile); os.IsNotExist(err) {
				t.Errorf("Expected lock file %s to be generated", lockFile)
			}
		})
	}
}

// TestStrictModeStepUnsafeSecretsBlocked tests that secrets in non-env step
// fields (run, etc.) are still blocked in strict mode during full compilation.
// Post-steps cannot expose secrets, even through env: bindings.
// Note: secrets in with: for uses: action steps are now allowed (safe binding).
func TestStrictModeStepUnsafeSecretsBlocked(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		errorMsg string
	}{
		{
			name: "post-steps with secrets in env are blocked",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
post-steps:
  - name: Send notification
    env:
      SLACK_TOKEN: ${{ secrets.SLACK_TOKEN }}
    run: send-notification
---

# Post-Steps Env Secret Test

Post-steps must not expose secrets through env bindings.
`,
			errorMsg: "secrets are not allowed in post-steps",
		},
		{
			name: "secret in run field is blocked in strict mode",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Inline secret leak
    run: echo ${{ secrets.API_TOKEN }}
---

# Unsafe Run Secret Test

This should fail because secrets are used inline in run.
`,
			errorMsg: "strict mode: secrets expressions detected in 'steps' section",
		},
		{
			name: "secret in with field without uses is blocked in strict mode",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Step with with but no uses
    with:
      token: ${{ secrets.MY_API_TOKEN }}
    run: echo hi
---

# Unsafe With Secret Test (no uses)

This should fail because with: without uses: is not a safe binding.
`,
			errorMsg: "strict mode: secrets expressions detected in 'steps' section",
		},
		{
			name: "mixed env and run secrets - run secret still blocked",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Mixed secrets
    env:
      SAFE_KEY: ${{ secrets.SAFE_KEY }}
    run: echo ${{ secrets.LEAKED_KEY }}
---

# Mixed Secret Test

Should fail because run field contains a secret even though env is safe.
`,
			errorMsg: "strict mode: secrets expressions detected in 'steps' section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "strict-step-unsafe-secrets-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.CompileWorkflow(testFile)

			if err == nil {
				t.Error("Expected compilation to fail with secrets in non-env step fields, but it succeeded")
			} else if !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
			}
			if _, err := os.Stat(stringutil.MarkdownToLockFile(testFile)); !os.IsNotExist(err) {
				t.Errorf("Expected no lock file after rejected compilation, got stat error: %v", err)
			}
		})
	}
}

// TestStrictModeStepEnvSecretsErrorSuggestsEnvBindings verifies that the error
// message for unsafe secrets suggests using step-level env: bindings as an
// alternative.
func TestStrictModeStepEnvSecretsErrorSuggestsEnvBindings(t *testing.T) {
	content := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Inline secret
    run: echo ${{ secrets.MY_TOKEN }}
---

# Error Message Test

Check that error suggests env bindings.
`
	tmpDir := testutil.TempDir(t, "strict-error-msg-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err := compiler.CompileWorkflow(testFile)

	if err == nil {
		t.Fatal("Expected compilation to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "env: bindings") {
		t.Errorf("Expected error to suggest env: bindings, got: %v", err)
	}
	if !strings.Contains(err.Error(), "with: inputs") {
		t.Errorf("Expected error to suggest with: inputs for action steps, got: %v", err)
	}
}

// TestNonStrictModeStepSecretsAllowedWithWarning verifies that in non-strict
// mode, secrets in any step field (including non-env) are allowed with a
// warning.
func TestNonStrictModeStepSecretsAllowedWithWarning(t *testing.T) {
	content := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
steps:
  - name: Use inline secret
    run: echo ${{ secrets.API_KEY }}
---

# Non-Strict Step Secret Test

Should compile with a warning in non-strict mode.
`
	tmpDir := testutil.TempDir(t, "non-strict-step-secrets-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	// Do NOT set strict mode - let frontmatter control it
	err := compiler.CompileWorkflow(testFile)

	if err != nil {
		t.Errorf("Non-strict mode should allow secrets in step fields with a warning, but got error: %v", err)
	}
}
