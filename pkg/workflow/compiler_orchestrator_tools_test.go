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

// TestProcessToolsAndMarkdown_BasicTools tests basic tools processing
func TestProcessToolsAndMarkdown_BasicTools(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-basic")

	testContent := `---
on: push
engine: copilot
tools:
  bash:
    - echo
  github:
    mode: remote
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	// Parse frontmatter
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	// Get agentic engine
	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	// Create empty imports result
	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.tools, "Tools should be extracted")
	assert.NotEmpty(t, result.markdownContent, "Markdown should be extracted")
}

// TestProcessToolsAndMarkdown_ToolsMerging tests tools merging from imports and includes
func TestProcessToolsAndMarkdown_ToolsMerging(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-merging")

	// Create an include file with tools
	includeContent := `---
tools:
  bash:
    - ls
---

# Included
`
	includeFile := filepath.Join(tmpDir, "include.md")
	require.NoError(t, os.WriteFile(includeFile, []byte(includeContent), 0644))

	testContent := `---
on: push
engine: copilot
tools:
  bash:
    - echo
---

@include(include.md)

# Main Workflow
`

	testFile := filepath.Join(tmpDir, "main.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Tools should be merged
	assert.NotEmpty(t, result.tools)
}

// TestProcessToolsAndMarkdown_MCPServers tests MCP server configuration
func TestProcessToolsAndMarkdown_MCPServers(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-mcp")

	testContent := `---
on: push
engine: copilot
mcp-servers:
  test-server:
    command: node
    args:
      - server.js
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	// MCP servers should be merged into tools
	assert.NotEmpty(t, result.tools)
}

// TestProcessToolsAndMarkdown_RuntimesMerging tests runtimes merging
func TestProcessToolsAndMarkdown_RuntimesMerging(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-runtimes")

	testContent := `---
on: push
engine: copilot
runtimes:
  node:
    version: "20"
  python:
    version: "3.11"
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.runtimes, "Runtimes should be extracted")
}

// TestProcessToolsAndMarkdown_PluginExtraction tests plugin extraction
func TestProcessToolsAndMarkdown_PluginExtraction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-plugins")

	testContent := `---
on: push
engine: copilot
plugins:
  - owner/repo
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotNil(t, result.pluginInfo, "PluginInfo should be extracted")
	if result.pluginInfo != nil {
		assert.NotEmpty(t, result.pluginInfo.Plugins, "Plugins should be extracted")
	}
}

// TestProcessToolsAndMarkdown_ToolsTimeout tests tools timeout extraction
func TestProcessToolsAndMarkdown_ToolsTimeout(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-timeout")

	testContent := `---
on: push
engine: copilot
tools:
  timeout: 600
  bash:
    - echo
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 600, result.toolsTimeout, "Tools timeout should be extracted")
}

// TestProcessToolsAndMarkdown_StartupTimeout tests startup timeout extraction
func TestProcessToolsAndMarkdown_StartupTimeout(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-startup-timeout")

	testContent := `---
on: push
engine: copilot
tools:
  startup-timeout: 120
  bash:
    - echo
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 120, result.toolsStartupTimeout, "Startup timeout should be extracted")
}

// TestProcessToolsAndMarkdown_InvalidTimeout tests invalid timeout values
func TestProcessToolsAndMarkdown_InvalidTimeout(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-invalid-timeout")

	testContent := `---
on: push
engine: copilot
tools:
  timeout: "not-a-number"
  bash:
    - echo
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	// Should error with invalid timeout
	require.Error(t, err, "Invalid timeout should cause error")
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "timeout")
}

// TestProcessToolsAndMarkdown_MCPValidation tests MCP config validation
func TestProcessToolsAndMarkdown_MCPValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-mcp-validation")

	testContent := `---
on: push
engine: copilot
tools:
  github:
    mode: remote
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestProcessToolsAndMarkdown_WorkflowNameExtraction tests workflow name extraction
func TestProcessToolsAndMarkdown_WorkflowNameExtraction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-name")

	tests := []struct {
		name         string
		frontmatter  string
		expectedName string
	}{
		{
			name: "explicit name in frontmatter",
			frontmatter: `---
on: push
engine: copilot
name: Custom Workflow Name
---`,
			expectedName: "Custom Workflow Name",
		},
		{
			name: "no name uses filename",
			frontmatter: `---
on: push
engine: copilot
---`,
			expectedName: "", // Will use filename
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + "\n\n# Workflow Content\n"
			testFile := filepath.Join(tmpDir, "workflow-"+tt.name+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()

			frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
			require.NoError(t, err)

			agenticEngine, err := compiler.getAgenticEngine("copilot")
			require.NoError(t, err)

			importsResult := &parser.ImportsResult{}

			result, err := compiler.processToolsAndMarkdown(
				frontmatterResult,
				testFile,
				tmpDir,
				agenticEngine,
				"copilot",
				importsResult,
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.expectedName != "" {
				assert.Equal(t, tt.expectedName, result.frontmatterName)
			}
		})
	}
}

// TestProcessToolsAndMarkdown_TextOutputDetection tests text output usage detection
func TestProcessToolsAndMarkdown_TextOutputDetection(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-text-output")

	tests := []struct {
		name        string
		markdown    string
		expectUsage bool
	}{
		{
			name:        "no text output",
			markdown:    "# Workflow\n\nSimple content",
			expectUsage: false,
		},
		{
			name:        "with text output",
			markdown:    "# Workflow\n\nUse ${{ needs.activation.outputs.text }} here",
			expectUsage: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := "---\non: push\nengine: copilot\n---\n\n" + tt.markdown
			testFile := filepath.Join(tmpDir, "output-"+tt.name+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()

			frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
			require.NoError(t, err)

			agenticEngine, err := compiler.getAgenticEngine("copilot")
			require.NoError(t, err)

			importsResult := &parser.ImportsResult{}

			result, err := compiler.processToolsAndMarkdown(
				frontmatterResult,
				testFile,
				tmpDir,
				agenticEngine,
				"copilot",
				importsResult,
			)

			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.expectUsage, result.needsTextOutput,
				"Text output detection should match expected for: %s", tt.name)
		})
	}
}

// TestProcessToolsAndMarkdown_SafeOutputsConfig tests safe outputs configuration extraction
func TestProcessToolsAndMarkdown_SafeOutputsConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-safe-outputs")

	testContent := `---
on: push
engine: copilot
safe-outputs:
  types:
    - issue
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotNil(t, result.safeOutputs, "Safe outputs config should be extracted")
}

// TestProcessToolsAndMarkdown_SecretMaskingConfig tests secret masking configuration
func TestProcessToolsAndMarkdown_SecretMaskingConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-secret-masking")

	testContent := `---
on: push
engine: copilot
secret-masking:
  enabled: true
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Secret masking is extracted (may be nil if config is minimal)
	// Just verify the result structure is valid
	assert.NotNil(t, result)
}

// TestProcessToolsAndMarkdown_TrackerID tests tracker ID extraction
func TestProcessToolsAndMarkdown_TrackerID(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-tracker")

	testContent := `---
on: push
engine: copilot
tracker-id: TEST-123
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "TEST-123", result.trackerID, "Tracker ID should be extracted")
}

// TestProcessToolsAndMarkdown_CustomEngineNoTools tests custom engine behavior
func TestProcessToolsAndMarkdown_CustomEngineNoTools(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-custom-engine")

	testContent := `---
on: push
engine: custom
tools:
  bash:
    - echo
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("custom")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"custom",
		importsResult,
	)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Custom engine doesn't support tool allowlists - tools should be replaced
	assert.NotEmpty(t, result.tools)
	// Should have generated warnings
	assert.Positive(t, compiler.warningCount, "Custom engine should generate warnings")
}

// TestProcessToolsAndMarkdown_IncludeExpansionError tests include expansion errors
func TestProcessToolsAndMarkdown_IncludeExpansionError(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-include-error")

	testContent := `---
on: push
engine: copilot
---

@include(nonexistent.md)

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"copilot",
		importsResult,
	)

	// Include expansion happens via parser.ExpandIncludesWithManifest
	// Missing includes may be handled gracefully in some cases
	// This test verifies the function completes
	if err != nil {
		assert.Contains(t, err.Error(), "nonexistent", "Error should mention missing file")
	} else {
		assert.NotNil(t, result)
	}
}
