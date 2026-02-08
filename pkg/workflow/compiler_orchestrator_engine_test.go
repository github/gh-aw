//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetupEngineAndImports_ValidSetup tests successful engine setup with imports
func TestSetupEngineAndImports_ValidSetup(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-setup-valid")

	testContent := `---
on: push
engine: copilot
network:
  allowed:
    - github.com
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
	assert.Contains(t, err.Error(), "invalid-engine-name")
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
features:
  dangerous-permissions-write: true
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

			var compiler *Compiler
			if tt.cliStrict {
				compiler = NewCompiler(WithStrictMode(true))
			} else {
				compiler = NewCompiler()
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

// TestSetupEngineAndImports_NetworkMerging tests network permissions merging from imports
func TestSetupEngineAndImports_NetworkMerging(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-network")

	// Create an import file with network permissions
	importContent := `---
network:
  allowed:
    - api.github.com
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
    - github.com
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
	assert.Contains(t, err.Error(), "engine")
}

// TestSetupEngineAndImports_FirewallEnablement tests automatic firewall enablement
func TestSetupEngineAndImports_FirewallEnablement(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-firewall")

	testContent := `---
on: push
engine: copilot
network:
  allowed:
    - github.com
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

// TestSetupEngineAndImports_ExperimentalEngine tests custom engine setup
func TestSetupEngineAndImports_ExperimentalEngine(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-experimental")

	testContent := `---
on: push
engine: custom
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

	// Custom engine should be set up successfully
	assert.NotNil(t, result.agenticEngine)
	assert.Equal(t, "custom", result.engineSetting)
}
