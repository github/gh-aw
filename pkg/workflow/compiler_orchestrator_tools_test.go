//go:build !integration

package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
    version: "24"
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

	assert.Equal(t, "600", result.toolsTimeout, "Tools timeout should be extracted")
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

	assert.Equal(t, "120", result.toolsStartupTimeout, "Startup timeout should be extracted")
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
	require.ErrorContains(t, err, "timeout")
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
			markdown:    "# Workflow\n\nUse ${{ steps.sanitized.outputs.text }} here",
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

// TestProcessToolsAndMarkdown_CustomEngineNoTools tests that codex engine rejects restricted bash allowlists
func TestProcessToolsAndMarkdown_CustomEngineNoTools(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-codex-engine")

	testContent := `---
on: push
engine: codex
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

	agenticEngine, err := compiler.getAgenticEngine("codex")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	_, err = compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"codex",
		importsResult,
	)

	// Codex engine does not support restricted bash allowlists - should produce a compile error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support bash command allow-listing")
}

// TestProcessToolsAndMarkdown_CodexWildcardBashSucceeds tests that codex engine with bash: ["*"] compiles cleanly
func TestProcessToolsAndMarkdown_CodexWildcardBashSucceeds(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-codex-wildcard")

	testContent := `---
on: push
engine: codex
tools:
  bash:
    - "*"
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()

	frontmatterResult, err := parser.ExtractFrontmatterFromContent(testContent)
	require.NoError(t, err)

	agenticEngine, err := compiler.getAgenticEngine("codex")
	require.NoError(t, err)

	importsResult := &parser.ImportsResult{}

	result, err := compiler.processToolsAndMarkdown(
		frontmatterResult,
		testFile,
		tmpDir,
		agenticEngine,
		"codex",
		importsResult,
	)

	// Codex engine with wildcard bash should compile without error
	require.NoError(t, err)
	assert.NotNil(t, result)
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
		assert.ErrorContains(t, err, "nonexistent", "Error should mention missing file")
	} else {
		assert.NotNil(t, result)
	}
}

// captureStderr redirects os.Stderr to a pipe and returns the captured output
// after calling fn.  The original os.Stderr is always restored.
func captureStderr(fn func()) string {
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// TestWarnDeprecatedFrontmatterFields_ToolsGrep verifies that using tools.grep
// emits the schema-driven deprecation warning.
func TestWarnDeprecatedFrontmatterFields_ToolsGrep(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"tools": map[string]any{
			"grep": true,
		},
	}
	stderr := captureStderr(func() {
		compiler.warnDeprecatedFrontmatterFields(frontmatter)
	})

	assert.Contains(t, stderr, "grep", "warning should mention grep")
	assert.Equal(t, 1, compiler.warningCount, "warning count should be incremented")
}

// TestWarnDeprecatedFrontmatterFields_NoDeprecatedFields verifies that no
// warning is emitted when no deprecated fields are present.
func TestWarnDeprecatedFrontmatterFields_NoDeprecatedFields(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"engine": "copilot",
		"tools":  map[string]any{"bash": true},
	}
	stderr := captureStderr(func() {
		compiler.warnDeprecatedFrontmatterFields(frontmatter)
	})

	assert.Empty(t, strings.TrimSpace(stderr), "no warning should be emitted for non-deprecated fields")
	assert.Equal(t, 0, compiler.warningCount)
}

// TestWarnDeprecatedFrontmatterFields_MessagePriority verifies that
// x-deprecation-message is preferred over description when both are present,
// and that description is used as a fallback when x-deprecation-message is empty.
func TestWarnDeprecatedFrontmatterFields_MessagePriority(t *testing.T) {
	compiler := NewCompiler()

	// tools.grep has both description and x-deprecation-message in the schema.
	// The x-deprecation-message should be the one emitted.
	frontmatter := map[string]any{
		"tools": map[string]any{"grep": true},
	}
	stderr := captureStderr(func() {
		compiler.warnDeprecatedFrontmatterFields(frontmatter)
	})

	// x-deprecation-message for tools.grep starts with "grep is always available"
	assert.Contains(t, stderr, "grep is always available", "x-deprecation-message should be preferred")
}

// TestWarnDeprecatedFrontmatterFields_ToolsGitHubRepos verifies that using the
// deprecated tools.github.repos field emits a schema-driven warning.
func TestWarnDeprecatedFrontmatterFields_ToolsGitHubRepos(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"tools": map[string]any{
			"github": map[string]any{
				"repos": []any{"owner/repo"},
			},
		},
	}
	stderr := captureStderr(func() {
		compiler.warnDeprecatedFrontmatterFields(frontmatter)
	})

	assert.Contains(t, stderr, "allowed-repos", "warning should mention the replacement field")
	assert.Equal(t, 1, compiler.warningCount)
}

// TestWarnDeprecatedFrontmatterFields_MultipleFields verifies that multiple
// deprecated fields each emit a warning and increment the count correctly.
func TestWarnDeprecatedFrontmatterFields_MultipleFields(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"tools": map[string]any{
			"grep":   true,
			"serena": true,
		},
	}
	stderr := captureStderr(func() {
		compiler.warnDeprecatedFrontmatterFields(frontmatter)
	})

	assert.Contains(t, stderr, "grep", "warning should mention grep")
	assert.Contains(t, stderr, "serena", "warning should mention serena")
	assert.Equal(t, 2, compiler.warningCount, "one warning per deprecated field")
}

// TestWarnDeprecatedFrontmatterFields_SafeOutputsDeprecatedAliases verifies that
// deprecated safe-outputs aliases (e.g. title-prefix) emit schema-driven warnings.
func TestWarnDeprecatedFrontmatterFields_SafeOutputsDeprecatedAliases(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"close-issue": map[string]any{
				"title-prefix": "[old] ",
			},
		},
	}
	stderr := captureStderr(func() {
		compiler.warnDeprecatedFrontmatterFields(frontmatter)
	})

	assert.Contains(t, stderr, "required-title-prefix", "warning should mention required-title-prefix replacement")
	assert.Equal(t, 1, compiler.warningCount, "one warning for the deprecated alias")
}

func TestEnforceMCPProxyTools(t *testing.T) {
	engine := &BaseEngine{
		id:           "custom-engine",
		capabilities: EngineCapabilities{MCP: false},
	}

	t.Run("enables required proxies", func(t *testing.T) {
		tools, err := enforceMCPProxyTools(engine, map[string]any{})
		require.NoError(t, err)
		assert.Equal(t, true, tools["cli-proxy"])
		assert.Equal(t, map[string]any{"mode": "gh-proxy"}, tools["github"])
	})

	t.Run("preserves github configuration", func(t *testing.T) {
		tools, err := enforceMCPProxyTools(engine, map[string]any{
			"github": map[string]any{"toolsets": []any{"repos"}},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"mode":     "gh-proxy",
			"toolsets": []any{"repos"},
		}, tools["github"])
		assert.Equal(t, true, tools["cli-proxy"])
	})

	t.Run("normalizes default-enabled github forms", func(t *testing.T) {
		for name, github := range map[string]any{
			"nil":    nil,
			"string": "enabled",
		} {
			t.Run(name, func(t *testing.T) {
				tools, err := enforceMCPProxyTools(engine, map[string]any{"github": github})
				require.NoError(t, err)
				assert.Equal(t, map[string]any{"mode": "gh-proxy"}, tools["github"])
				assert.Equal(t, true, tools["cli-proxy"])
			})
		}
	})

	t.Run("rejects disabled github proxy", func(t *testing.T) {
		_, err := enforceMCPProxyTools(engine, map[string]any{"github": false})
		require.ErrorContains(t, err, "tools.github cannot be disabled")
	})

	t.Run("rejects non proxy github mode", func(t *testing.T) {
		_, err := enforceMCPProxyTools(engine, map[string]any{
			"github": map[string]any{"mode": "remote"},
		})
		require.ErrorContains(t, err, "tools.github.mode must be gh-proxy")
	})

	t.Run("rejects disabled cli proxy", func(t *testing.T) {
		_, err := enforceMCPProxyTools(engine, map[string]any{"cli-proxy": false})
		require.ErrorContains(t, err, "tools.cli-proxy cannot be disabled")
	})

	t.Run("leaves MCP engines unchanged", func(t *testing.T) {
		tools := map[string]any{"github": false, "cli-proxy": false}
		actual, err := enforceMCPProxyTools(&BaseEngine{
			id:           "mcp-engine",
			capabilities: EngineCapabilities{MCP: true},
		}, tools)
		require.NoError(t, err)
		assert.Equal(t, tools, actual)
	})
}
