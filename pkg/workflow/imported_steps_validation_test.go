//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateImportedStepsNoAgenticSecrets_Copilot(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflowsDir, 0755), "Failed to create workflows directory")

	// Create an import file with custom engine using COPILOT_GITHUB_TOKEN
	importContent := `---
engine:
  id: custom
  steps:
    - name: Call Copilot CLI
      run: |
        gh copilot suggest "How do I use Git?"
      env:
        GH_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}
---

# Shared Custom Engine
This shared config uses Copilot CLI with the agentic secret.
`
	importFile := filepath.Join(workflowsDir, "shared", "copilot-custom-engine.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(importFile), 0755))
	require.NoError(t, os.WriteFile(importFile, []byte(importContent), 0644))

	// Create main workflow that imports this
	mainContent := `---
name: Test Workflow
on: push
imports:
  - shared/copilot-custom-engine.md
---

# Test Workflow
This workflow imports a custom engine with agentic secrets.
`
	mainFile := filepath.Join(workflowsDir, "test-copilot-secret.md")
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0644))

	// Test in strict mode - should fail
	t.Run("strict mode error", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(true)
		err := compiler.CompileWorkflow(mainFile)

		require.Error(t, err, "Expected error in strict mode")
		if err != nil {
			assert.Contains(t, err.Error(), "strict mode", "Error should mention strict mode")
			assert.Contains(t, err.Error(), "COPILOT_GITHUB_TOKEN", "Error should mention the secret name")
			assert.Contains(t, err.Error(), "GitHub Copilot CLI", "Error should mention the engine")
			assert.Contains(t, err.Error(), "custom engine steps", "Error should mention custom engine steps")
		}
	})

	// Test in non-strict mode - should succeed with warning
	t.Run("non-strict mode warning", func(t *testing.T) {
		// Update main file to explicitly disable strict mode
		mainContentNonStrict := `---
name: Test Workflow
on: push
strict: false
imports:
  - shared/copilot-custom-engine.md
---

# Test Workflow
This workflow imports a custom engine with agentic secrets.
`
		mainFileNonStrict := filepath.Join(workflowsDir, "test-copilot-secret-nonstrict.md")
		require.NoError(t, os.WriteFile(mainFileNonStrict, []byte(mainContentNonStrict), 0644))

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(mainFileNonStrict)

		require.NoError(t, err, "Should not error in non-strict mode")
		assert.Positive(t, compiler.GetWarningCount(), "Should have warnings")
	})
}

func TestValidateImportedStepsNoAgenticSecrets_Anthropic(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create an import file with custom engine using ANTHROPIC_API_KEY
	importContent := `---
engine:
  id: custom
  steps:
    - name: Use Claude API
      uses: actions/claude-action@v1
      with:
        api-key: ${{ secrets.ANTHROPIC_API_KEY }}
---

# Shared Custom Engine
`
	importFile := filepath.Join(workflowsDir, "shared", "claude-custom-engine.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(importFile), 0755))
	require.NoError(t, os.WriteFile(importFile, []byte(importContent), 0644))

	// Create main workflow
	mainContent := `---
name: Test Workflow
on: push
imports:
  - shared/claude-custom-engine.md
---

# Test
`
	mainFile := filepath.Join(workflowsDir, "test-anthropic-secret.md")
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0644))

	// Test in strict mode
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err := compiler.CompileWorkflow(mainFile)

	require.Error(t, err, "Expected error in strict mode")
	if err != nil {
		assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
		assert.Contains(t, err.Error(), "Claude Code")
	}
}

func TestValidateImportedStepsNoAgenticSecrets_Codex(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create an import file with custom engine using both CODEX_API_KEY and OPENAI_API_KEY
	importContent := `---
engine:
  id: custom
  steps:
    - name: Use OpenAI API
      run: |
        curl -X POST https://api.openai.com/v1/completions \
          -H "Authorization: Bearer $OPENAI_API_KEY"
      env:
        OPENAI_API_KEY: ${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}
---

# Shared Custom Engine
`
	importFile := filepath.Join(workflowsDir, "shared", "codex-custom-engine.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(importFile), 0755))
	require.NoError(t, os.WriteFile(importFile, []byte(importContent), 0644))

	// Create main workflow
	mainContent := `---
name: Test Workflow
on: push
imports:
  - shared/codex-custom-engine.md
---

# Test
`
	mainFile := filepath.Join(workflowsDir, "test-codex-secret.md")
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0644))

	// Test in strict mode
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err := compiler.CompileWorkflow(mainFile)

	require.Error(t, err, "Expected error in strict mode")
	if err != nil {
		errMsg := err.Error()
		// Should detect both secrets
		hasCodex := strings.Contains(errMsg, "CODEX_API_KEY")
		hasOpenAI := strings.Contains(errMsg, "OPENAI_API_KEY")
		assert.True(t, hasCodex || hasOpenAI, "Should mention at least one of the Codex secrets")
		assert.Contains(t, errMsg, "Codex")
	}
}

func TestValidateImportedStepsNoAgenticSecrets_SafeSecrets(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create an import file with custom engine using non-agentic secrets (should be fine)
	importContent := `---
engine:
  id: custom
  steps:
    - name: Use Custom API
      run: |
        curl -X POST https://api.example.com/v1/data \
          -H "Authorization: Bearer $MY_CUSTOM_TOKEN"
      env:
        MY_CUSTOM_TOKEN: ${{ secrets.MY_CUSTOM_API_KEY }}
        GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
---

# Shared Custom Engine
This uses safe, non-agentic secrets.
`
	importFile := filepath.Join(workflowsDir, "shared", "safe-custom-engine.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(importFile), 0755))
	require.NoError(t, os.WriteFile(importFile, []byte(importContent), 0644))

	// Create main workflow
	mainContent := `---
name: Test Workflow
on: push
strict: false
permissions:
  contents: read
  issues: read
  pull-requests: read
imports:
  - shared/safe-custom-engine.md
---

# Test
`
	mainFile := filepath.Join(workflowsDir, "test-safe-secrets.md")
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0644))

	// Test in strict mode - should succeed
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err := compiler.CompileWorkflow(mainFile)

	assert.NoError(t, err, "Should not error when using safe secrets")
	// Note: We may have warning for experimental feature (custom engine), but not for our secret validation
}

func TestValidateImportedStepsNoAgenticSecrets_NonCustomEngine(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create workflow with non-custom engine (copilot) - should not validate
	mainContent := `---
name: Test Workflow
on: push
engine: copilot
strict: false
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test
This uses the standard copilot engine, not custom.
`
	mainFile := filepath.Join(workflowsDir, "test-copilot-engine.md")
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0644))

	// Test in strict mode - should succeed (validation doesn't apply)
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err := compiler.CompileWorkflow(mainFile)

	assert.NoError(t, err, "Should not error for non-custom engines")
}

func TestValidateImportedStepsNoAgenticSecrets_MultipleSecrets(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create an import file with custom engine using multiple agentic secrets
	importContent := `---
engine:
  id: custom
  steps:
    - name: Use Multiple AI APIs
      run: |
        echo "Using Copilot"
        gh copilot suggest "help"
      env:
        GH_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}
    - name: Use Claude
      run: |
        echo "Using Claude"
      env:
        ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
---

# Shared Custom Engine
`
	importFile := filepath.Join(workflowsDir, "shared", "multi-secret-engine.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(importFile), 0755))
	require.NoError(t, os.WriteFile(importFile, []byte(importContent), 0644))

	// Create main workflow
	mainContent := `---
name: Test Workflow
on: push
imports:
  - shared/multi-secret-engine.md
---

# Test
`
	mainFile := filepath.Join(workflowsDir, "test-multi-secrets.md")
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0644))

	// Test in strict mode
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err := compiler.CompileWorkflow(mainFile)

	require.Error(t, err, "Expected error in strict mode")
	if err != nil {
		errMsg := err.Error()
		// Should mention both secrets
		assert.Contains(t, errMsg, "COPILOT_GITHUB_TOKEN")
		assert.Contains(t, errMsg, "ANTHROPIC_API_KEY")
		// Should mention the engines (GitHub Copilot CLI and/or Claude Code)
		hasCopilot := strings.Contains(errMsg, "GitHub Copilot CLI")
		hasClaude := strings.Contains(errMsg, "Claude Code")
		assert.True(t, hasCopilot || hasClaude, "Should mention the engines")
	}
}

func TestValidateImportedStepsNoAgenticSecrets_OpenCodeExemption(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create an import file with OpenCode custom engine using agentic secrets
	importContent := `---
engine:
  id: custom
  env:
    GH_AW_AGENT_VERSION: "0.15.13"
  steps:
    - name: Install OpenCode
      run: |
        npm install -g "opencode-ai@${GH_AW_AGENT_VERSION}"
      env:
        GH_AW_AGENT_VERSION: ${{ env.GH_AW_AGENT_VERSION }}
    - name: Run OpenCode
      run: |
        opencode run "test prompt"
      env:
        ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
---

# OpenCode Engine
This is a custom agentic engine wrapper.
`
	importFile := filepath.Join(workflowsDir, "shared", "opencode.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(importFile), 0755))
	require.NoError(t, os.WriteFile(importFile, []byte(importContent), 0644))

	// Create main workflow with strict mode
	mainContent := `---
name: Test OpenCode Workflow
on: push
strict: true
permissions:
  contents: read
  issues: read
  pull-requests: read
imports:
  - shared/opencode.md
---

# Test
This workflow uses OpenCode which is a custom agentic engine.
`
	mainFile := filepath.Join(workflowsDir, "test-opencode.md")
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0644))

	// Test in strict mode - should succeed because OpenCode is exempt
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err := compiler.CompileWorkflow(mainFile)

	assert.NoError(t, err, "Should not error for OpenCode custom engine even in strict mode")
}
