//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrontmatterDeclaresImports(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		want        bool
	}{
		{name: "array", frontmatter: map[string]any{"imports": []any{"shared/engine.md"}}, want: true},
		{name: "string array", frontmatter: map[string]any{"imports": []string{"shared/engine.md"}}, want: true},
		{name: "object array", frontmatter: map[string]any{"imports": map[string]any{"aw": []any{"shared/engine.md"}}}, want: true},
		{name: "object string array", frontmatter: map[string]any{"imports": map[string]any{"aw": []string{"shared/engine.md"}}}, want: true},
		{name: "empty object array", frontmatter: map[string]any{"imports": map[string]any{"aw": []any{}}}, want: false},
		{name: "missing imports", frontmatter: map[string]any{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, frontmatterDeclaresImports(tt.frontmatter))
		})
	}
}

// TestSetupEngineAndImports_ValidSetup tests successful engine setup with imports
func TestSetupEngineAndImports_ValidSetup(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-setup-valid")

	testContent := `---
on: push
engine: copilot
network:
  allowed:
    - python
---

# Test Workflow

Test content
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	// Parse frontmatter first
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	// Call setupEngineAndImports
	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err, "Valid setup should succeed")
	require.NotNil(t, result)

	// Verify result structure
	assert.Equal(t, "copilot", result.engineSetting)
	assert.NotNil(t, result.engineConfig)
	assert.NotNil(t, result.agenticEngine)
	assert.NotNil(t, result.networkPermissions)
	assert.NotNil(t, result.importsResult)
}

// TestSetupEngineAndImports_DefaultEngine tests engine defaulting when not specified
func TestSetupEngineAndImports_DefaultEngine(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-default")

	testContent := `---
on: push
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should default to copilot
	assert.Equal(t, "copilot", result.engineSetting)
}

func TestSetupEngineAndImports_DefaultMaxTurnsFromEnv(t *testing.T) {
	t.Setenv(compilerenv.DefaultMaxTurns, "9")

	tmpDir := testutil.TempDir(t, "engine-default-max-turns")
	testContent := `---
on: push
engine: claude
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, "9", result.engineConfig.MaxTurns)
}

func TestSetupEngineAndImports_ExplicitMaxTurnsOverridesEnvDefault(t *testing.T) {
	t.Setenv(compilerenv.DefaultMaxTurns, "9")

	tmpDir := testutil.TempDir(t, "engine-explicit-max-turns")
	testContent := `---
on: push
engine:
  id: claude
  max-turns: 4
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, "4", result.engineConfig.MaxTurns)
}

func TestSetupEngineAndImports_TopLevelMaxTurnsOverridesEnvDefault(t *testing.T) {
	t.Setenv(compilerenv.DefaultMaxTurns, "9")

	tmpDir := testutil.TempDir(t, "engine-top-level-max-turns")
	testContent := "---\n" +
		"on: push\n" +
		"engine: codex\n" +
		`max-turns: "${{ inputs.max-turns }}"` + "\n" +
		"---\n\n" +
		"# Test Workflow\n"
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, "${{ inputs.max-turns }}", result.engineConfig.MaxTurns)
}

func TestSetupEngineAndImports_ImportedTopLevelMaxTurns(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-imported-max-turns")

	sharedContent := `---
engine:
  id: claude
max-turns: 4
---

# Shared Workflow
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "common.md"), []byte(sharedContent), 0644))

	testContent := `---
on: push
imports:
  - shared/common.md
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, "claude", result.engineSetting)
	assert.Equal(t, "4", result.engineConfig.MaxTurns)
}

func TestSetupEngineAndImports_ImportedTopLevelMaxToolDenials(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-imported-max-tool-denials")

	sharedContent := `---
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 9
---

# Shared Workflow
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "common.md"), []byte(sharedContent), 0644))

	testContent := `---
on: push
imports:
  - shared/common.md
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, "copilot", result.engineSetting)
	assert.Equal(t, "9", result.engineConfig.MaxToolDenials)
}

func TestSetupEngineAndImports_ImportedTopLevelMaxTurnCacheMisses(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-imported-max-turn-cache-misses")

	sharedContent := `---
engine: copilot
max-turn-cache-misses: 7
---

# Shared Workflow
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "common.md"), []byte(sharedContent), 0644))

	testContent := `---
on: push
imports:
  - shared/common.md
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, "copilot", result.engineSetting)
	assert.Equal(t, 7, result.engineConfig.MaxTurnCacheMisses)
}

// TestSetupEngineAndImports_ImportedEngineVersionDefault verifies that a shared/imported
// engine definition's top-level `version` field is applied as the default
// EngineConfig.Version when the workflow's own `engine:` frontmatter selects the same
// engine ID but omits `version`. This mirrors how shared/goose.md pins a default
// version for the Goose engine, so workflows that only set `engine: { id: goose }`
// still get a non-empty GH_AW_ENGINE_VERSION rather than crashing at runtime.
func TestSetupEngineAndImports_ImportedEngineVersionDefault(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-imported-version-default")

	sharedContent := `---
engine:
  id: harness-engine
  version: "1.45.0"
  display-name: Harness Engine
  behaviors:
    secret-strategy: universal-llm-consumer
    execution:
      command-name: harness-engine
      step-name: Execute Harness Engine
---

# Shared Engine Definition
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "engine.md"), []byte(sharedContent), 0644))

	testContent := `---
on: push
engine:
  id: harness-engine
imports:
  - shared/engine.md
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, "harness-engine", result.engineSetting)
	assert.Equal(t, "1.45.0", result.engineConfig.Version)
}

func TestSetupEngineAndImports_ExplicitVersionNotOverriddenByDefault(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-explicit-version-wins")

	sharedContent := `---
engine:
  id: harness-engine
  version: "1.45.0"
  display-name: Harness Engine
  behaviors:
    secret-strategy: universal-llm-consumer
    execution:
      command-name: harness-engine
      step-name: Execute Harness Engine
---

# Shared Engine Definition
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "engine.md"), []byte(sharedContent), 0644))

	testContent := `---
on: push
engine:
  id: harness-engine
  version: "2.0.0"
imports:
  - shared/engine.md
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, "harness-engine", result.engineSetting)
	assert.Equal(t, "2.0.0", result.engineConfig.Version)
}

func TestSetupEngineAndImports_ImportedEngineVersionDefaultDoesNotLeakAcrossWorkflows(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-imported-version-no-leak")

	sharedContent := `---
engine:
  id: harness-engine
  version: "1.45.0"
  display-name: Harness Engine
  behaviors:
    secret-strategy: universal-llm-consumer
    execution:
      command-name: harness-engine
      step-name: Execute Harness Engine
---

# Shared Engine Definition
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "engine.md"), []byte(sharedContent), 0644))

	importingContent := `---
on: push
engine:
  id: harness-engine
imports:
  - shared/engine.md
---

# Importing Workflow
`
	importingFile := filepath.Join(tmpDir, "importing.md")
	require.NoError(t, os.WriteFile(importingFile, []byte(importingContent), 0644))

	compiler := NewCompiler()
	importingFrontmatter, err := parser.ExtractFrontmatterFromContent(importingContent)
	require.NoError(t, err)
	importingResult, err := compiler.setupEngineAndImports(importingFrontmatter, importingFile, []byte(importingContent), tmpDir)
	require.NoError(t, err)
	require.NotNil(t, importingResult)
	require.NotNil(t, importingResult.engineConfig)
	require.Equal(t, "1.45.0", importingResult.engineConfig.Version)

	plainContent := `---
on: push
engine:
  id: harness-engine
---

# Plain Workflow
`
	plainFile := filepath.Join(tmpDir, "plain.md")
	require.NoError(t, os.WriteFile(plainFile, []byte(plainContent), 0644))
	plainFrontmatter, err := parser.ExtractFrontmatterFromContent(plainContent)
	require.NoError(t, err)

	plainResult, err := compiler.setupEngineAndImports(plainFrontmatter, plainFile, []byte(plainContent), tmpDir)
	require.NoError(t, err)
	require.NotNil(t, plainResult)
	require.NotNil(t, plainResult.engineConfig)
	assert.Equal(t, "harness-engine", plainResult.engineSetting)
	assert.Empty(t, plainResult.engineConfig.Version)
}

// TestSetupEngineAndImports_MainMaxTurnCacheMissesTakesPrecedenceOverImport verifies that
// a main workflow's max-turn-cache-misses frontmatter wins over the same field in an import.
func TestSetupEngineAndImports_MainMaxTurnCacheMissesTakesPrecedenceOverImport(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-main-cache-misses-wins")

	sharedContent := `---
engine: copilot
max-turn-cache-misses: 7
---

# Shared Workflow
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "common.md"), []byte(sharedContent), 0644))

	testContent := `---
on: push
max-turn-cache-misses: 3
imports:
  - shared/common.md
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, 3, result.engineConfig.MaxTurnCacheMisses, "main workflow max-turn-cache-misses must win over import")
}

// TestSetupEngineAndImports_ImportedModelPreservedWithTopLevelMaxAICredits is a regression
// test for the bug where an engine.model pin from an imported file was silently dropped
// when the main workflow also set max-ai-credits.
//
// Root cause: top-level max-ai-credits causes ExtractEngineConfig to return a non-nil
// engineConfig, which caused the model-extraction branch in resolveEngineFromIncludesAndImports
// to be skipped (it was guarded by engineConfig == nil).
func TestSetupEngineAndImports_ImportedModelPreservedWithTopLevelMaxAICredits(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-imported-model-max-ai-credits")

	sharedContent := `---
engine:
  id: copilot
  model: gpt-5.6-sol
---

# Shared Workflow
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "model.md"), []byte(sharedContent), 0644))

	testContent := `---
on: push
max-ai-credits: 1500
imports:
  - shared/model.md
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	assert.Equal(t, "copilot", result.engineSetting)
	assert.Equal(t, "gpt-5.6-sol", result.model, "imported engine.model must not be dropped when main workflow sets max-ai-credits")
	assert.Equal(t, int64(1500), result.engineConfig.MaxAICredits, "max-ai-credits from main workflow must be preserved")
}

func TestSetupEngineAndImports_ImportedAuthPreservedWithTopLevelEnginePreferences(t *testing.T) {
	tests := []struct {
		name        string
		rootSetting string
		verify      func(t *testing.T, result *engineSetupResult)
	}{
		{
			name:        "model",
			rootSetting: "model: claude-sonnet-4-5",
			verify: func(t *testing.T, result *engineSetupResult) {
				assert.Equal(t, "claude-sonnet-4-5", result.model)
			},
		},
		{
			name:        "max turns",
			rootSetting: "max-turns: 40",
			verify: func(t *testing.T, result *engineSetupResult) {
				assert.Equal(t, "40", result.engineConfig.MaxTurns)
			},
		},
		{
			name:        "max AI credits",
			rootSetting: "max-ai-credits: 500",
			verify: func(t *testing.T, result *engineSetupResult) {
				assert.Equal(t, int64(500), result.engineConfig.MaxAICredits)
			},
		},
	}

	sharedContent := `---
engine:
  id: claude
  auth:
    type: github-oidc
    provider: anthropic
    federation-rule-id: fr_01ABC
    organization-id: org_01XYZ
    service-account-id: sa_01DEF
    workspace-id: ws_01GHI
---

# Shared Workflow
`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "engine-imported-auth-preferences")
			sharedDir := filepath.Join(tmpDir, "shared")
			require.NoError(t, os.MkdirAll(sharedDir, 0755))
			require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "engine.md"), []byte(sharedContent), 0644))

			testContent := `---
on: push
` + tt.rootSetting + `
imports:
  - shared/engine.md
---

# Test Workflow
`
			testFile := filepath.Join(tmpDir, "test.md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()
			frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
			require.NoError(t, err)

			result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, []byte(testContent), tmpDir)
			require.NoError(t, err)
			require.NotNil(t, result.engineConfig.Auth)
			assert.Equal(t, "github-oidc", result.engineConfig.Auth.Type)
			assert.Equal(t, "anthropic", result.engineConfig.Auth.Provider)
			assert.Equal(t, "fr_01ABC", result.engineConfig.Auth.AnthropicFederationRuleID)
			assert.Equal(t, "org_01XYZ", result.engineConfig.Auth.AnthropicOrganizationID)
			assert.Equal(t, "sa_01DEF", result.engineConfig.Auth.AnthropicServiceAccountID)
			assert.Equal(t, "ws_01GHI", result.engineConfig.Auth.AnthropicWorkspaceID)
			tt.verify(t, result)
		})
	}
}

// TestSetupEngineAndImports_EngineOverride tests command-line engine override
func TestSetupEngineAndImports_EngineOverride(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-override")

	testContent := `---
on: push
engine: copilot
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	// Create compiler with engine override
	compiler := NewCompiler(WithEngineOverride("claude"))
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Engine should be overridden
	assert.Equal(t, "claude", result.engineSetting)
}

// TestSetupEngineAndImports_InvalidEngine tests error handling for invalid engine
func TestSetupEngineAndImports_InvalidEngine(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-invalid")

	testContent := `---
on: push
engine: invalid-engine-name
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.Error(t, err, "Invalid engine should cause error")
	assert.Nil(t, result)
	require.ErrorContains(t, err, "invalid-engine-name")
}

// TestSetupEngineAndImports_StrictModeHandling tests strict mode state management
func TestSetupEngineAndImports_StrictModeHandling(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-strict")

	tests := []struct {
		name          string
		frontmatter   string
		cliStrict     bool
		expectStrict  bool
		shouldSucceed bool
	}{
		{
			name: "default strict mode",
			frontmatter: `---
on: push
engine: copilot
---`,
			cliStrict:     false,
			expectStrict:  true,
			shouldSucceed: true,
		},
		{
			name: "explicit strict false",
			frontmatter: `---
on: push
engine: copilot
strict: false
---`,
			cliStrict:     false,
			expectStrict:  false,
			shouldSucceed: true,
		},
		{
			name: "cli overrides frontmatter",
			frontmatter: `---
on: push
engine: copilot
strict: false
---`,
			cliStrict:     true,
			expectStrict:  true,
			shouldSucceed: false, // Will fail validation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + "\n\n# Test Workflow\n"
			testFile := filepath.Join(tmpDir, "strict-"+tt.name+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()
			if tt.cliStrict {
				compiler.SetStrictMode(true)
			}

			content := []byte(testContent)
			frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
			require.NoError(t, err)

			result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)

			if tt.shouldSucceed {
				require.NoError(t, err, "Should succeed for test: %s", tt.name)
				require.NotNil(t, result)
			} else {
				// CLI strict mode with strict: false in frontmatter may fail validation
				if err != nil {
					require.Error(t, err)
				}
			}

			// Verify compiler's strict mode was restored after processing
			// (strict mode should not leak between workflows)
			assert.Equal(t, tt.cliStrict, compiler.strictMode,
				"Compiler strict mode should be restored to CLI setting")
		})
	}
}

func TestShouldScanImportedMarkdown(t *testing.T) {
	tests := []struct {
		name           string
		importFilePath string
		want           bool
	}{
		{
			name:           "scans regular markdown imports",
			importFilePath: "shared/workflow.md",
			want:           true,
		},
		{
			name:           "skips builtin markdown imports",
			importFilePath: parser.BuiltinPathPrefix + "engines/claude.md",
			want:           false,
		},
		{
			name:           "skips non-markdown imports",
			importFilePath: "shared/workflow.lock.yml",
			want:           false,
		},
		{
			name:           "skips uppercase markdown extension",
			importFilePath: "shared/workflow.MD",
			want:           false,
		},
		{
			name:           "skips builtin non-markdown imports",
			importFilePath: parser.BuiltinPathPrefix + "engines/claude.yml",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldScanImportedMarkdown(tt.importFilePath)
			assert.Equal(t, tt.want, got, "shouldScanImportedMarkdown(%q)", tt.importFilePath)
		})
	}
}

// TestSetupEngineAndImports_NetworkMerging tests network permissions merging from imports
func TestSetupEngineAndImports_NetworkMerging(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-network")

	// Create an import file with network permissions
	importContent := `---
network:
  allowed:
    - python
---

# Imported Workflow
`
	importFile := filepath.Join(tmpDir, "imported.md")
	require.NoError(t, os.WriteFile(importFile, []byte(importContent), 0644))

	// Main workflow imports the file
	testContent := `---
on: push
engine: copilot
imports:
  - imported.md
network:
  allowed:
    - node
---

# Main Workflow
`

	testFile := filepath.Join(tmpDir, "main.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Network permissions should be merged
	assert.NotNil(t, result.networkPermissions)
	assert.NotEmpty(t, result.networkPermissions.Allowed)
}

// TestSetupEngineAndImports_DefaultNetworkPermissions tests default network configuration
func TestSetupEngineAndImports_DefaultNetworkPermissions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-default-network")

	testContent := `---
on: push
engine: copilot
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should default to "defaults" ecosystem
	assert.NotNil(t, result.networkPermissions)
	assert.Equal(t, []string{"defaults"}, result.networkPermissions.Allowed)
}

// TestSetupEngineAndImports_SandboxConfiguration tests sandbox config extraction
func TestSetupEngineAndImports_SandboxConfiguration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-sandbox")

	testContent := `---
on: push
engine: copilot
sandbox:
  enabled: true
  mounts:
    - /tmp
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Sandbox config should be extracted
	assert.NotNil(t, result.sandboxConfig)
}

// TestSetupEngineAndImports_MultipleEngineConflict tests error when multiple engines specified
func TestSetupEngineAndImports_MultipleEngineConflict(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-conflict")

	// Create an import with different engine
	importContent := `---
engine: claude
---

# Imported
`
	importFile := filepath.Join(tmpDir, "imported.md")
	require.NoError(t, os.WriteFile(importFile, []byte(importContent), 0644))

	// Main workflow with different engine
	testContent := `---
on: push
engine: copilot
imports:
  - imported.md
---

# Main
`

	testFile := filepath.Join(tmpDir, "main.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)

	// Should error due to conflicting engines
	require.Error(t, err, "Conflicting engines should cause error")
	assert.Nil(t, result)
	require.ErrorContains(t, err, "engine")
}

// TestSetupEngineAndImports_FirewallEnablement tests automatic firewall enablement
func TestSetupEngineAndImports_FirewallEnablement(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-firewall")

	testContent := `---
on: push
engine: copilot
network:
  allowed:
    - python
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Firewall should be enabled by default for copilot with network restrictions
	assert.NotNil(t, result.networkPermissions)
}

// TestSetupEngineAndImports_ImportProcessingError tests error handling during import processing
func TestSetupEngineAndImports_ImportProcessingError(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-import-error")

	// Reference a non-existent import file
	testContent := `---
on: push
engine: copilot
imports:
  - nonexistent.md
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)

	// Should error due to missing import
	require.Error(t, err, "Missing import should cause error")
	assert.Nil(t, result)
}

// TestSetupEngineAndImports_PermissionsValidation tests imported permissions validation
func TestSetupEngineAndImports_PermissionsValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-perms")

	testContent := `---
on: push
engine: copilot
permissions:
  contents: read
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestSetupEngineAndImports_ExperimentalEngine tests codex engine setup
func TestSetupEngineAndImports_ExperimentalEngine(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-experimental")

	testContent := `---
on: push
engine: codex
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler(WithVerbose(true))
	content := []byte(testContent)

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Codex engine should be set up successfully
	assert.NotNil(t, result.agenticEngine)
	assert.Equal(t, "codex", result.engineSetting)
}
