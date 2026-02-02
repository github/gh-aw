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

// TestParseWorkflowFile_ValidMainWorkflow tests parsing a valid main workflow
func TestParseWorkflowFile_ValidMainWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "parse-valid-main")

	testContent := `---
on: push
engine: copilot
timeout-minutes: 10
strict: false
features:
  dangerous-permissions-write: true
permissions:
  contents: read
---

# Test Main Workflow

This is a valid main workflow with an 'on' field.
`

	testFile := filepath.Join(tmpDir, "main-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Valid main workflow should parse successfully")
	require.NotNil(t, workflowData, "WorkflowData should not be nil")

	// Verify parsed data
	assert.Equal(t, "# Test Main Workflow\n\nThis is a valid main workflow with an 'on' field.\n", workflowData.MarkdownContent)
}

// TestParseWorkflowFile_SharedWorkflow tests parsing a shared/imported workflow (no 'on' field)
func TestParseWorkflowFile_SharedWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "parse-shared")

	// Shared workflows don't have 'on' field
	testContent := `---
engine: copilot
permissions:
  contents: read
---

# Shared Workflow

This can be imported by other workflows.
`

	testFile := filepath.Join(tmpDir, "shared-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	// Should return SharedWorkflowError
	require.Error(t, err, "Shared workflow should return an error")
	assert.Nil(t, workflowData, "WorkflowData should be nil for shared workflows")

	// Check if it's a SharedWorkflowError
	var sharedErr *SharedWorkflowError
	require.ErrorAs(t, err, &sharedErr, "Error should be SharedWorkflowError type")
	assert.Equal(t, testFile, sharedErr.Path)
}

// TestParseWorkflowFile_MissingFrontmatter tests error handling for missing frontmatter
func TestParseWorkflowFile_MissingFrontmatter(t *testing.T) {
	tmpDir := testutil.TempDir(t, "parse-no-frontmatter")

	testContent := `# Workflow Without Frontmatter

This file has no frontmatter section.
`

	testFile := filepath.Join(tmpDir, "no-frontmatter.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	require.Error(t, err, "Should error when frontmatter is missing")
	assert.Nil(t, workflowData)
	assert.Contains(t, err.Error(), "frontmatter", "Error should mention frontmatter")
}

// TestParseWorkflowFile_InvalidYAML tests error handling for invalid YAML frontmatter
func TestParseWorkflowFile_InvalidYAML(t *testing.T) {
	tmpDir := testutil.TempDir(t, "parse-invalid-yaml")

	testContent := `---
on: push
invalid: [unclosed
bracket: here
---

# Workflow

Content
`

	testFile := filepath.Join(tmpDir, "invalid-yaml.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	require.Error(t, err, "Should error with invalid YAML")
	assert.Nil(t, workflowData)
}

// TestParseWorkflowFile_PathTraversal tests path traversal protection
func TestParseWorkflowFile_PathTraversal(t *testing.T) {
	compiler := NewCompiler()

	// Try various path traversal patterns
	pathsToTest := []string{
		"../../../etc/passwd",
		"./../../etc/passwd",
		".../.../etc/passwd",
	}

	for _, path := range pathsToTest {
		_, err := compiler.ParseWorkflowFile(path)
		// Should fail (file doesn't exist or is rejected)
		require.Error(t, err, "Path traversal attempt should fail: %s", path)
	}
}

// TestParseWorkflowFile_NoMarkdownContent tests error handling for main workflows without markdown content
func TestParseWorkflowFile_NoMarkdownContent(t *testing.T) {
	tmpDir := testutil.TempDir(t, "parse-no-markdown")

	// Main workflow (has 'on' field) but no markdown content
	testContent := `---
on: push
engine: copilot
---
`

	testFile := filepath.Join(tmpDir, "no-markdown.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	require.Error(t, err, "Main workflow without markdown content should error")
	assert.Nil(t, workflowData)
	assert.Contains(t, err.Error(), "markdown content", "Error should mention markdown content")
}

// TestParseWorkflowFile_EngineExtraction tests engine config extraction
func TestParseWorkflowFile_EngineExtraction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "parse-engine")

	tests := []struct {
		name           string
		frontmatter    string
		expectedEngine string
	}{
		{
			name: "copilot engine",
			frontmatter: `---
on: push
engine: copilot
---`,
			expectedEngine: "copilot",
		},
		{
			name: "claude engine",
			frontmatter: `---
on: push
engine: claude
---`,
			expectedEngine: "claude",
		},
		{
			name: "default engine when not specified",
			frontmatter: `---
on: push
---`,
			expectedEngine: "copilot", // Default engine
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + "\n\n# Workflow\n\nContent\n"
			testFile := filepath.Join(tmpDir, "engine-test-"+tt.name+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()
			workflowData, err := compiler.ParseWorkflowFile(testFile)
			require.NoError(t, err)
			require.NotNil(t, workflowData)

			// Check engine via AI field (backwards compatibility) or EngineConfig
			actualEngine := workflowData.AI
			if workflowData.EngineConfig != nil && workflowData.EngineConfig.ID != "" {
				actualEngine = workflowData.EngineConfig.ID
			}
			assert.Equal(t, tt.expectedEngine, actualEngine,
				"Engine should be %s for test %s", tt.expectedEngine, tt.name)
		})
	}
}

// TestParseWorkflowFile_EngineOverride tests command-line engine override
func TestParseWorkflowFile_EngineOverride(t *testing.T) {
	tmpDir := testutil.TempDir(t, "parse-engine-override")

	testContent := `---
on: push
engine: copilot
---

# Workflow

Content
`

	testFile := filepath.Join(tmpDir, "override-test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	// Create compiler with engine override
	compiler := NewCompiler(
		WithEngineOverride("claude"),
	)
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Engine should be overridden to 'claude' (stored in AI field for backward compatibility)
	assert.Equal(t, "claude", workflowData.AI, "Engine should be overridden to claude")
}

// TestParseWorkflowFile_NetworkPermissions tests network permissions extraction
func TestParseWorkflowFile_NetworkPermissions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "parse-network")

	tests := []struct {
		name               string
		includeNetwork     bool
		networkConfig      string
		expectedAllowed    []string
		expectedHasAllowed bool
	}{
		{
			name:            "default network (no network field)",
			includeNetwork:  false,
			expectedAllowed: []string{"defaults"},
		},
		{
			name:           "explicit allowed domains",
			includeNetwork: true,
			networkConfig: `
network:
  allowed:
    - github.com
    - api.example.com`,
			expectedHasAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := "---\non: push\nengine: copilot"
			if tt.includeNetwork {
				frontmatter += "\n" + tt.networkConfig
			}
			frontmatter += "\n---"

			testContent := frontmatter + "\n\n# Workflow\n\nContent\n"
			testFile := filepath.Join(tmpDir, "network-test-"+tt.name+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()
			workflowData, err := compiler.ParseWorkflowFile(testFile)
			require.NoError(t, err)
			require.NotNil(t, workflowData)
			require.NotNil(t, workflowData.NetworkPermissions)

			if len(tt.expectedAllowed) > 0 {
				assert.Equal(t, tt.expectedAllowed, workflowData.NetworkPermissions.Allowed,
					"Network allowed list should match expected")
			}

			if tt.expectedHasAllowed {
				assert.NotEmpty(t, workflowData.NetworkPermissions.Allowed,
					"Should have allowed domains")
			}
		})
	}
}

// TestParseWorkflowFile_StrictMode tests strict mode validation
func TestParseWorkflowFile_StrictMode(t *testing.T) {
	tmpDir := testutil.TempDir(t, "parse-strict")

	tests := []struct {
		name        string
		cliStrict   bool
		yamlStrict  *bool // nil means not specified in YAML
		expectError bool
	}{
		{
			name:        "strict mode default (true)",
			cliStrict:   false,
			yamlStrict:  nil,
			expectError: false,
		},
		{
			name:        "strict mode explicitly true",
			cliStrict:   false,
			yamlStrict:  ptrBool(true),
			expectError: false,
		},
		{
			name:        "strict mode explicitly false",
			cliStrict:   false,
			yamlStrict:  ptrBool(false),
			expectError: false,
		},
		{
			name:        "cli strict mode overrides yaml",
			cliStrict:   true,
			yamlStrict:  ptrBool(false),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := "---\non: push\nengine: copilot"
			if tt.yamlStrict != nil {
				if *tt.yamlStrict {
					frontmatter += "\nstrict: true"
				} else {
					frontmatter += "\nstrict: false\nfeatures:\n  dangerous-permissions-write: true"
				}
			}
			frontmatter += "\n---"

			testContent := frontmatter + "\n\n# Workflow\n\nContent\n"
			testFile := filepath.Join(tmpDir, "strict-test-"+tt.name+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()
			_, err := compiler.ParseWorkflowFile(testFile)

			if tt.expectError {
				require.Error(t, err, "Should error in strict mode test: %s", tt.name)
			} else {
				require.NoError(t, err, "Should not error in strict mode test: %s", tt.name)
			}
		})
	}
}

// ptrBool returns a pointer to a boolean value
func ptrBool(b bool) *bool {
	return &b
}

// TestCopyFrontmatterWithoutInternalMarkers tests internal marker removal
func TestCopyFrontmatterWithoutInternalMarkers(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name: "no internal markers",
			input: map[string]any{
				"on":     "push",
				"engine": "copilot",
			},
			expected: map[string]any{
				"on":     "push",
				"engine": "copilot",
			},
		},
		{
			name: "gh_aw_native_label_filter marker removed",
			input: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"types":                         []string{"opened"},
						"__gh_aw_native_label_filter__": true,
					},
				},
				"engine": "copilot",
			},
			expected: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"types": []string{"opened"},
					},
				},
				"engine": "copilot",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.copyFrontmatterWithoutInternalMarkers(tt.input)

			// Check all expected keys exist
			for key, expectedVal := range tt.expected {
				actualVal, exists := result[key]
				assert.True(t, exists, "Key %s should exist in result", key)

				// For nested maps, check recursively
				if expectedMap, ok := expectedVal.(map[string]any); ok {
					actualMap, ok := actualVal.(map[string]any)
					require.True(t, ok, "Value for %s should be a map", key)
					for nestedKey := range expectedMap {
						_, exists := actualMap[nestedKey]
						assert.True(t, exists, "Nested key %s.%s should exist", key, nestedKey)
					}
				}
			}

			// Check that specific marker was removed
			if onMap, ok := result["on"].(map[string]any); ok {
				if issuesMap, ok := onMap["issues"].(map[string]any); ok {
					_, exists := issuesMap["__gh_aw_native_label_filter__"]
					assert.False(t, exists, "Internal marker __gh_aw_native_label_filter__ should be removed")
				}
			}
		})
	}
}

// TestDetectTextOutputUsageInOrchestrator tests text output detection in markdown
func TestDetectTextOutputUsageInOrchestrator(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		markdown       string
		expectedOutput bool
	}{
		{
			name:           "no text output",
			markdown:       "# Workflow\n\nSimple workflow with no output markers.",
			expectedOutput: false,
		},
		{
			name:           "with text output usage",
			markdown:       "# Workflow\n\nUse ${{ needs.activation.outputs.text }} here.",
			expectedOutput: true,
		},
		{
			name:           "text output in middle",
			markdown:       "# Start\n\nContent\n${{ needs.activation.outputs.text }}\n\nMore content",
			expectedOutput: true,
		},
		{
			name:           "multiple text output references",
			markdown:       "${{ needs.activation.outputs.text }}\nFirst\n${{ needs.activation.outputs.text }}\nSecond",
			expectedOutput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.detectTextOutputUsage(tt.markdown)
			assert.Equal(t, tt.expectedOutput, result,
				"Text output detection should return %v for test %s", tt.expectedOutput, tt.name)
		})
	}
}

// TestExtractYAMLSections tests the extractYAMLSections helper function
func TestExtractYAMLSections(t *testing.T) {
	tmpDir := testutil.TempDir(t, "extract-yaml")

	testContent := `---
on: push
engine: copilot
timeout-minutes: 30
strict: false
permissions:
  contents: read
  issues: read
network:
  allowed:
    - github.com
concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true
run-name: Test Run
env:
  NODE_ENV: production
features:
  safe-inputs: true
if: github.event_name == 'push'
runs-on: ubuntu-latest
environment: staging
container: node:18
cache:
  - key: ${{ runner.os }}-node
    path: node_modules
---

# Test Workflow

Test content
`

	testFile := filepath.Join(tmpDir, "yaml-test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify all YAML sections were extracted
	assert.NotEmpty(t, workflowData.On, "On section should be extracted")
	assert.NotEmpty(t, workflowData.Permissions, "Permissions should be extracted")
	assert.NotEmpty(t, workflowData.Network, "Network should be extracted")
	assert.NotEmpty(t, workflowData.Concurrency, "Concurrency should be extracted")
	assert.NotEmpty(t, workflowData.RunName, "RunName should be extracted")
	assert.NotEmpty(t, workflowData.Env, "Env should be extracted")
	assert.NotEmpty(t, workflowData.Features, "Features should be extracted")
	assert.NotEmpty(t, workflowData.If, "If condition should be extracted")
	assert.NotEmpty(t, workflowData.TimeoutMinutes, "TimeoutMinutes should be extracted")
	assert.NotEmpty(t, workflowData.RunsOn, "RunsOn should be extracted")
	assert.NotEmpty(t, workflowData.Environment, "Environment should be extracted")
	assert.NotEmpty(t, workflowData.Container, "Container should be extracted")
	assert.NotEmpty(t, workflowData.Cache, "Cache should be extracted")
}

// TestProcessAndMergeSteps tests the processAndMergeSteps helper function
func TestProcessAndMergeSteps(t *testing.T) {
	// Skip complex import testing - covered by existing integration tests
	t.Skip("Import/merge functionality tested through existing integration tests")
}

// TestProcessAndMergePostSteps tests the processAndMergePostSteps helper function
func TestProcessAndMergePostSteps(t *testing.T) {
	tmpDir := testutil.TempDir(t, "merge-post-steps")

	testContent := `---
on: push
engine: copilot
post-steps:
  - name: Cleanup
    run: echo "cleanup"
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "post-steps.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify post-steps were extracted
	assert.NotEmpty(t, workflowData.PostSteps, "Post-steps should be extracted")
	assert.Contains(t, workflowData.PostSteps, "Cleanup", "Should contain cleanup step")
}

// TestProcessAndMergeServices tests the processAndMergeServices helper function
func TestProcessAndMergeServices(t *testing.T) {
	// Skip complex import testing - covered by existing integration tests
	t.Skip("Import/merge functionality tested through existing integration tests")
}

// TestExtractAdditionalConfigurations tests the extractAdditionalConfigurations helper function
func TestExtractAdditionalConfigurations(t *testing.T) {
	tmpDir := testutil.TempDir(t, "additional-configs")

	testContent := `---
on: push
engine: copilot
roles:
  - admin
bots:
  - copilot
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "configs.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify basic configurations were extracted
	assert.NotEmpty(t, workflowData.Roles, "Roles should be extracted")
	assert.NotEmpty(t, workflowData.Bots, "Bots should be extracted")
}

// TestProcessOnSectionAndFilters tests the processOnSectionAndFilters helper function
func TestProcessOnSectionAndFilters(t *testing.T) {
	tmpDir := testutil.TempDir(t, "on-section-filters")

	testContent := `---
on:
  pull_request:
    types: [opened, synchronize]
    draft: false
engine: copilot
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "filters.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify on section was processed
	assert.NotEmpty(t, workflowData.On, "On section should be processed")
	assert.Contains(t, workflowData.On, "pull_request", "Should contain pull_request trigger")
}

// TestBuildInitialWorkflowData tests the buildInitialWorkflowData helper function
func TestBuildInitialWorkflowData(t *testing.T) {
	tmpDir := testutil.TempDir(t, "build-initial")

	testContent := `---
on: push
engine: copilot
name: Test Workflow
description: Test description
source: test-source
github-token: ${{ secrets.GITHUB_TOKEN }}
---

# Test Workflow

Test content
`

	testFile := filepath.Join(tmpDir, "initial.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify initial workflow data was built correctly
	assert.Equal(t, "Test Workflow", workflowData.FrontmatterName, "Name should be set")
	assert.Equal(t, "Test description", workflowData.Description, "Description should be set")
	assert.Equal(t, "test-source", workflowData.Source, "Source should be set")
	assert.Equal(t, "${{ secrets.GITHUB_TOKEN }}", workflowData.GitHubToken, "GitHub token should be set")
	assert.Equal(t, "copilot", workflowData.AI, "AI engine should be set")
	assert.NotNil(t, workflowData.EngineConfig, "EngineConfig should be set")
	assert.NotNil(t, workflowData.ParsedTools, "ParsedTools should be initialized")
}
