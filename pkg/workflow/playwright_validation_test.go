//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidatePlaywrightMode tests the CLI-only Playwright mode validation.
func TestValidatePlaywrightMode(t *testing.T) {
	tests := []struct {
		name        string
		tools       map[string]any
		expectError bool
		errorSubstr string
	}{
		{
			name:  "playwright not configured",
			tools: map[string]any{},
		},
		{
			name:  "playwright set to false",
			tools: map[string]any{"playwright": false},
		},
		{
			name:  "playwright nil defaults to CLI",
			tools: map[string]any{"playwright": nil},
		},
		{
			name:  "playwright empty map defaults to CLI",
			tools: map[string]any{"playwright": map[string]any{}},
		},
		{
			name:        "playwright explicit MCP mode",
			tools:       map[string]any{"playwright": map[string]any{"mode": "mcp"}},
			expectError: true,
			errorSubstr: "built-in Playwright MCP support has been removed",
		},
		{
			name:  "playwright CLI mode",
			tools: map[string]any{"playwright": map[string]any{"mode": "cli"}},
		},
		{
			name:  "playwright CLI mode uppercase",
			tools: map[string]any{"playwright": map[string]any{"mode": "CLI"}},
		},
		{
			name:        "playwright mode expression is rejected",
			tools:       map[string]any{"playwright": map[string]any{"mode": "${{ inputs.playwright-mode }}"}},
			expectError: true,
			errorSubstr: "mode must be a literal value; expressions are not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Tools: tt.tools,
			}

			err := compiler.validatePlaywrightMode(workflowData)

			if tt.expectError {
				require.Error(t, err, "expected an error but got none")
				require.ErrorContains(t, err, tt.errorSubstr,
					"error %q should contain %q", err.Error(), tt.errorSubstr)
			} else {
				assert.NoError(t, err, "expected no error")
			}
		})
	}
}

func TestCompileWorkflowRejectsPlaywrightMCPModeWithMigrationGuidance(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "test-workflow.md")
	content := `---
on: push
engine: claude
tools:
  playwright:
    mode: mcp
---

# Test Workflow
`
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0644))

	err := NewCompiler().CompileWorkflow(mdPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "built-in Playwright MCP support has been removed")
	assert.Contains(t, err.Error(), "Remove `mode: mcp`")
	assert.NotContains(t, err.Error(), "mode: cli")
	assert.Contains(t, err.Error(), "playwright-cli <command>")
	assert.Contains(t, err.Error(), "mcp-servers")
}

func TestPiEngineAcceptsPlaywrightWithImplicitCLIMode(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "test-workflow.md")
	content := `---
name: pi-playwright-cli
on: push
engine: pi
permissions:
  contents: read
  issues: read

tools:
  github:
    mode: gh-proxy
  cli-proxy: true
  playwright:
---

# Test Workflow
`
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(mdPath))

	lockPath := filepath.Join(tmpDir, "test-workflow.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	lockStr := string(lockContent)

	assert.Contains(t, lockStr, "@playwright/cli")
	assert.Contains(t, lockStr, "playwright-cli install --skills")
	assert.NotContains(t, lockStr, "@playwright/mcp")
	assert.NotContains(t, lockStr, "mode: cli")
}

// TestCompileWorkflowRejectsLegacyPlaywrightMCPModeWithArgs ensures that a legacy
// configuration combining `mode: mcp` with the removed MCP-only `args` field still
// surfaces the actionable migration error instead of a generic JSON schema
// "Unknown property: args" failure. Schema validation runs before
// validatePlaywrightMode, so `args` must remain schema-accepted for this
// dedicated validator to ever run.
func TestCompileWorkflowRejectsLegacyPlaywrightMCPModeWithArgs(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "test-workflow.md")
	content := `---
on: push
engine: claude
tools:
  playwright:
    mode: mcp
    args: ["--no-sandbox"]
---

# Test Workflow
`
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0644))

	err := NewCompiler().CompileWorkflow(mdPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "built-in Playwright MCP support has been removed")
	assert.NotContains(t, err.Error(), "Unknown property")
}

// TestCompileWorkflowRejectsPlaywrightModeExpression ensures that a full compile
// of a workflow with an expression-valued tools.playwright.mode surfaces the
// field-specific error from validatePlaywrightMode, rather than the generic
// JSON schema enum error. This guards against the schema's enum constraint
// preempting the dedicated validator.
func TestCompileWorkflowRejectsPlaywrightModeExpression(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "test-workflow.md")
	content := `---
on: push
engine: claude
tools:
  playwright:
    mode: ${{ inputs.playwright-mode }}
---

# Test Workflow

Test playwright mode expression rejection.
`
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mdPath)

	require.Error(t, err, "expected compilation to fail for expression-valued playwright mode")
	assert.Contains(t, err.Error(), "tools.playwright.mode")
	assert.Contains(t, err.Error(), "mode must be a literal value; expressions are not allowed")
}

// TestValidatePlaywrightModeNilWorkflow ensures no panic on nil/empty input.
func TestValidatePlaywrightModeNilWorkflow(t *testing.T) {
	compiler := NewCompiler()

	err := compiler.validatePlaywrightMode(nil)
	require.NoError(t, err, "nil workflowData should not return error")

	err = compiler.validatePlaywrightMode(&WorkflowData{Tools: nil})
	require.NoError(t, err, "nil tools should not return error")
}

func TestValidatePlaywrightBrowsers(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validatePlaywrightMode(&WorkflowData{Tools: map[string]any{
		"playwright": map[string]any{"browsers": []any{"chrome", "chrome-for-testing", "firefox"}},
	}})
	require.NoError(t, err)

	err = compiler.validatePlaywrightMode(&WorkflowData{Tools: map[string]any{
		"playwright": map[string]any{"browsers": []any{"safari"}},
	}})
	require.Error(t, err)
}

func TestEmitPlaywrightBrowserInstallWarning(t *testing.T) {
	tests := []struct {
		name          string
		tools         map[string]any
		preSteps      string
		customSteps   string
		preAgentSteps string
		postSteps     string
		wantWarning   bool
	}{
		{
			name:  "npm exec browser install",
			tools: map[string]any{"playwright": nil},
			customSteps: `steps:
- name: Install Playwright Chromium
  run: npm exec playwright install --with-deps chromium
`,
			wantWarning: true,
		},
		{
			name:  "npx browser install",
			tools: map[string]any{"playwright": nil},
			customSteps: `steps:
- run: npx --yes playwright@latest install firefox
`,
			wantWarning: true,
		},
		{
			name:  "browser install after another command",
			tools: map[string]any{"playwright": nil},
			customSteps: `steps:
- run: npm ci && pnpm exec playwright install webkit
`,
			wantWarning: true,
		},
		{
			name:  "bare browser install in pre-steps",
			tools: map[string]any{"playwright": nil},
			preSteps: `pre-steps:
- run: playwright install chromium
`,
			wantWarning: true,
		},
		{
			name:  "browser install in pre-agent steps",
			tools: map[string]any{"playwright": nil},
			preAgentSteps: `pre-agent-steps:
- run: bunx playwright install chromium
`,
			wantWarning: true,
		},
		{
			name:  "browser install in post-steps",
			tools: map[string]any{"playwright": nil},
			postSteps: `post-steps:
- run: yarn exec playwright install webkit
`,
			wantWarning: true,
		},
		{
			name:  "skills install is not a browser install",
			tools: map[string]any{"playwright": nil},
			customSteps: `steps:
- run: playwright-cli install --skills
`,
		},
		{
			name:  "package install is not a browser install",
			tools: map[string]any{"playwright": nil},
			customSteps: `steps:
- run: npm install playwright
`,
		},
		{
			name:  "disabled Playwright tool",
			tools: map[string]any{"playwright": false},
			customSteps: `steps:
- run: npx playwright install chromium
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			output := testutil.CaptureStderr(t, func() {
				compiler.emitPlaywrightBrowserInstallWarning(&WorkflowData{
					Tools:         tt.tools,
					PreSteps:      tt.preSteps,
					CustomSteps:   tt.customSteps,
					PreAgentSteps: tt.preAgentSteps,
					PostSteps:     tt.postSteps,
				}, "test.md")
			})

			if tt.wantWarning {
				assert.Contains(t, output, "use `tools.playwright.browsers` instead")
				assert.Equal(t, 1, compiler.GetWarningCount())
			} else {
				assert.Empty(t, output)
				assert.Zero(t, compiler.GetWarningCount())
			}
		})
	}
}

func TestCompileWorkflowWarnsAboutPlaywrightBrowserInstallStep(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "test-workflow.md")
	content := `---
on: push
permissions:
  contents: read
engine: copilot
tools:
  playwright:
    browsers: [chrome-for-testing]
steps:
  - name: Install Playwright Chromium
    run: npm exec playwright install --with-deps chromium
---

# Test Workflow
`
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o644))

	compiler := NewCompiler()
	output := testutil.CaptureStderr(t, func() {
		require.NoError(t, compiler.CompileWorkflow(mdPath))
	})

	assert.Contains(t, output, "use `tools.playwright.browsers` instead")
	assert.Equal(t, 1, compiler.GetWarningCount())
	lockContent, err := os.ReadFile(filepath.Join(tmpDir, "test-workflow.lock.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(lockContent), `install_playwright_browsers.sh" chromium`)
}
