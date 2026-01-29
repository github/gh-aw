//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetWorkflowDescription(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	tests := []struct {
		name        string
		content     string
		wantDesc    string
		description string
	}{
		{
			name: "workflow with description",
			content: `---
description: Daily CI workflow for testing
engine: copilot
---

# Test workflow
`,
			wantDesc:    "Daily CI workflow for testing",
			description: "Should extract description field",
		},
		{
			name: "workflow with long description",
			content: `---
description: This is a very long description that exceeds the sixty character limit and should be truncated
engine: copilot
---

# Test workflow
`,
			wantDesc:    "This is a very long description that exceeds the sixty ch...",
			description: "Should truncate description to 60 characters",
		},
		{
			name: "workflow with name but no description",
			content: `---
name: Production deployment workflow
engine: copilot
---

# Test workflow
`,
			wantDesc:    "Production deployment workflow",
			description: "Should fallback to name field",
		},
		{
			name: "workflow with neither description nor name",
			content: `---
engine: copilot
---

# Test workflow
`,
			wantDesc:    "",
			description: "Should return empty string",
		},
		{
			name: "workflow without frontmatter",
			content: `# Test workflow

This is a test workflow without frontmatter.
`,
			wantDesc:    "",
			description: "Should return empty string for no frontmatter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test workflow file
			testFile := filepath.Join(workflowsDir, "test-workflow.md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.content), 0644))

			// Test getWorkflowDescription
			desc := getWorkflowDescription(testFile)
			assert.Equal(t, tt.wantDesc, desc, tt.description)

			// Clean up
			require.NoError(t, os.Remove(testFile))
		})
	}
}

func TestCompleteWorkflowNamesWithDescriptions(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create test workflow files with descriptions
	workflows := map[string]string{
		"test-workflow.md": `---
description: Daily CI workflow for testing
engine: copilot
---

Test workflow content
`,
		"ci-doctor.md": `---
description: Automated CI health checks
engine: copilot
---

CI doctor workflow
`,
		"weekly-research.md": `---
name: Research workflow
engine: copilot
---

Weekly research workflow
`,
		"no-desc.md": `---
engine: copilot
---

No description workflow
`,
	}

	for filename, content := range workflows {
		f, err := os.Create(filepath.Join(workflowsDir, filename))
		require.NoError(t, err)
		_, err = f.WriteString(content)
		require.NoError(t, err)
		f.Close()
	}

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	cmd := &cobra.Command{}

	tests := []struct {
		name       string
		toComplete string
		wantCount  int
		wantNames  []string
		checkDescs bool
	}{
		{
			name:       "empty prefix returns all workflows with descriptions",
			toComplete: "",
			wantCount:  4,
			wantNames:  []string{"test-workflow", "ci-doctor", "weekly-research", "no-desc"},
			checkDescs: true,
		},
		{
			name:       "c prefix returns ci-doctor",
			toComplete: "c",
			wantCount:  1,
			wantNames:  []string{"ci-doctor"},
			checkDescs: true,
		},
		{
			name:       "test prefix returns test-workflow",
			toComplete: "test",
			wantCount:  1,
			wantNames:  []string{"test-workflow"},
			checkDescs: true,
		},
		{
			name:       "x prefix returns nothing",
			toComplete: "x",
			wantCount:  0,
			wantNames:  []string{},
			checkDescs: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completions, directive := CompleteWorkflowNames(cmd, nil, tt.toComplete)
			assert.Len(t, completions, tt.wantCount)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

			if tt.checkDescs && tt.wantCount > 0 {
				// Verify that completions are in the correct format
				for _, completion := range completions {
					// Check if it has a tab-separated description or is just a name
					parts := strings.Split(completion, "\t")
					assert.True(t, len(parts) == 1 || len(parts) == 2,
						"Completion should be 'name' or 'name\\tdescription', got: %s", completion)

					// Verify the name part matches expected names
					name := parts[0]
					found := false
					for _, expectedName := range tt.wantNames {
						if name == expectedName {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected name %s to be in %v", name, tt.wantNames)
				}
			}
		})
	}
}

func TestValidEngineNames(t *testing.T) {
	engines := ValidEngineNames()

	// Verify the list is not empty
	assert.NotEmpty(t, engines, "Engine names list should not be empty")

	// Verify expected engines are present
	expectedEngines := []string{"copilot", "claude", "codex", "custom"}
	for _, expected := range expectedEngines {
		assert.Contains(t, engines, expected, "Expected engine '%s' to be in the list", expected)
	}
}

func TestCompleteEngineNames(t *testing.T) {
	cmd := &cobra.Command{}

	tests := []struct {
		name       string
		toComplete string
		wantLen    int
	}{
		{
			name:       "empty prefix returns all engines",
			toComplete: "",
			wantLen:    4, // copilot, claude, codex, custom
		},
		{
			name:       "c prefix returns claude, codex, copilot, custom",
			toComplete: "c",
			wantLen:    4,
		},
		{
			name:       "co prefix returns copilot, codex",
			toComplete: "co",
			wantLen:    2,
		},
		{
			name:       "cop prefix returns copilot",
			toComplete: "cop",
			wantLen:    1,
		},
		{
			name:       "x prefix returns nothing",
			toComplete: "x",
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completions, directive := CompleteEngineNames(cmd, nil, tt.toComplete)
			assert.Len(t, completions, tt.wantLen)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
		})
	}
}

func TestCompleteWorkflowNames(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create test workflow files
	testWorkflows := []string{"test-workflow.md", "ci-doctor.md", "weekly-research.md"}
	for _, wf := range testWorkflows {
		f, err := os.Create(filepath.Join(workflowsDir, wf))
		require.NoError(t, err)
		f.Close()
	}

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	cmd := &cobra.Command{}

	tests := []struct {
		name       string
		toComplete string
		wantLen    int
	}{
		{
			name:       "empty prefix returns all workflows",
			toComplete: "",
			wantLen:    3,
		},
		{
			name:       "c prefix returns ci-doctor",
			toComplete: "c",
			wantLen:    1,
		},
		{
			name:       "test prefix returns test-workflow",
			toComplete: "test",
			wantLen:    1,
		},
		{
			name:       "x prefix returns nothing",
			toComplete: "x",
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completions, directive := CompleteWorkflowNames(cmd, nil, tt.toComplete)
			assert.Len(t, completions, tt.wantLen)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
		})
	}
}

func TestCompleteWorkflowNamesNoWorkflowsDir(t *testing.T) {
	// Create a temporary directory without .github/workflows
	tmpDir := t.TempDir()

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	cmd := &cobra.Command{}

	completions, directive := CompleteWorkflowNames(cmd, nil, "")
	assert.Empty(t, completions)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestCompleteDirectories(t *testing.T) {
	cmd := &cobra.Command{}

	completions, directive := CompleteDirectories(cmd, nil, "")
	assert.Nil(t, completions)
	assert.Equal(t, cobra.ShellCompDirectiveFilterDirs, directive)
}

func TestRegisterEngineFlagCompletion(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringP("engine", "e", "", "AI engine")

	// This should not panic
	RegisterEngineFlagCompletion(cmd)
}

func TestRegisterDirFlagCompletion(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringP("dir", "d", "", "Directory")

	// This should not panic
	RegisterDirFlagCompletion(cmd, "dir")
}

// TestCompleteWorkflowNamesWithSpecialCharacters tests handling of workflow names with special characters
func TestCompleteWorkflowNamesWithSpecialCharacters(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create test workflow files with various special characters
	testWorkflows := []string{
		"test-workflow.md",      // dashes
		"test_workflow.md",      // underscores
		"test.workflow.md",      // dots
		"test-123.md",           // numbers with dashes
		"test_v2.0.md",          // version-like naming
		"my-complex_test.v1.md", // mixed special chars
	}
	for _, wf := range testWorkflows {
		f, err := os.Create(filepath.Join(workflowsDir, wf))
		require.NoError(t, err)
		f.Close()
	}

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	cmd := &cobra.Command{}

	tests := []struct {
		name           string
		toComplete     string
		expectedCount  int
		expectContains []string
	}{
		{
			name:          "all workflows with empty prefix",
			toComplete:    "",
			expectedCount: 6,
		},
		{
			name:           "workflows with test prefix",
			toComplete:     "test",
			expectedCount:  5, // test-workflow, test_workflow, test.workflow, test-123, test_v2.0
			expectContains: []string{"test-workflow", "test_workflow", "test.workflow", "test-123"},
		},
		{
			name:           "workflows with test- prefix",
			toComplete:     "test-",
			expectedCount:  2,
			expectContains: []string{"test-workflow", "test-123"},
		},
		{
			name:           "workflows with test_ prefix",
			toComplete:     "test_",
			expectedCount:  2,
			expectContains: []string{"test_workflow", "test_v2.0"},
		},
		{
			name:           "workflows with my- prefix",
			toComplete:     "my-",
			expectedCount:  1,
			expectContains: []string{"my-complex_test.v1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completions, directive := CompleteWorkflowNames(cmd, nil, tt.toComplete)
			assert.Len(t, completions, tt.expectedCount, "Expected %d completions", tt.expectedCount)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

			// Verify expected completions are present
			for _, expected := range tt.expectContains {
				assert.Contains(t, completions, expected, "Expected completion '%s' not found", expected)
			}
		})
	}
}

// TestCompleteWorkflowNamesWithInvalidFiles tests handling of invalid or non-.md files
func TestCompleteWorkflowNamesWithInvalidFiles(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create valid .md files
	validWorkflows := []string{"valid-workflow.md", "another-valid.md"}
	for _, wf := range validWorkflows {
		f, err := os.Create(filepath.Join(workflowsDir, wf))
		require.NoError(t, err)
		f.Close()
	}

	// Create non-.md files that should be ignored
	nonMdFiles := []string{
		"readme.txt",
		"config.yml",
		"script.sh",
	}
	for _, file := range nonMdFiles {
		f, err := os.Create(filepath.Join(workflowsDir, file))
		require.NoError(t, err)
		f.Close()
	}

	// Create a hidden .md file (starts with dot)
	hiddenMd := filepath.Join(workflowsDir, ".hidden.md")
	fHidden, err := os.Create(hiddenMd)
	require.NoError(t, err)
	fHidden.Close()

	// Create empty .md file
	emptyMd := filepath.Join(workflowsDir, "empty-workflow.md")
	fEmpty, err := os.Create(emptyMd)
	require.NoError(t, err)
	fEmpty.Close()

	// Create .md file with invalid content
	invalidMd := filepath.Join(workflowsDir, "invalid-workflow.md")
	err = os.WriteFile(invalidMd, []byte("not valid yaml frontmatter"), 0644)
	require.NoError(t, err)

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	cmd := &cobra.Command{}

	completions, directive := CompleteWorkflowNames(cmd, nil, "")

	// Should return all .md files (5 total: 2 valid, 1 empty, 1 invalid, 1 hidden)
	// Completion doesn't validate file content, just finds .md files (including hidden ones)
	assert.Len(t, completions, 5, "Should return all .md files regardless of content")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	// Verify valid workflows are present
	assert.Contains(t, completions, "valid-workflow")
	assert.Contains(t, completions, "another-valid")
	assert.Contains(t, completions, "empty-workflow")
	assert.Contains(t, completions, "invalid-workflow")
	assert.Contains(t, completions, ".hidden") // Hidden files are included by filepath.Glob

	// Verify non-.md files are NOT present
	assert.NotContains(t, completions, "readme")
	assert.NotContains(t, completions, "config")
	assert.NotContains(t, completions, "script")
}

// TestCompleteWorkflowNamesCaseSensitivity tests prefix matching is case-sensitive
func TestCompleteWorkflowNamesCaseSensitivity(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create test workflow files with different cases
	testWorkflows := []string{
		"test-workflow.md",
		"Test-Workflow.md",
		"TEST-WORKFLOW.md",
		"other-workflow.md",
	}
	for _, wf := range testWorkflows {
		f, err := os.Create(filepath.Join(workflowsDir, wf))
		require.NoError(t, err)
		f.Close()
	}

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	cmd := &cobra.Command{}

	tests := []struct {
		name           string
		toComplete     string
		expectContains []string
		expectMissing  []string
	}{
		{
			name:           "lowercase test prefix",
			toComplete:     "test",
			expectContains: []string{"test-workflow"},
			expectMissing:  []string{"Test-Workflow", "TEST-WORKFLOW"},
		},
		{
			name:           "capitalized Test prefix",
			toComplete:     "Test",
			expectContains: []string{"Test-Workflow"},
			expectMissing:  []string{"test-workflow", "TEST-WORKFLOW"},
		},
		{
			name:           "uppercase TEST prefix",
			toComplete:     "TEST",
			expectContains: []string{"TEST-WORKFLOW"},
			expectMissing:  []string{"test-workflow", "Test-Workflow"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completions, directive := CompleteWorkflowNames(cmd, nil, tt.toComplete)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

			// Verify expected completions are present
			for _, expected := range tt.expectContains {
				assert.Contains(t, completions, expected, "Expected completion '%s' not found", expected)
			}

			// Verify unwanted completions are not present
			for _, missing := range tt.expectMissing {
				assert.NotContains(t, completions, missing, "Unexpected completion '%s' found", missing)
			}
		})
	}
}

// TestCompleteWorkflowNamesExactMatch tests when toComplete exactly matches a workflow name
func TestCompleteWorkflowNamesExactMatch(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create test workflow files
	testWorkflows := []string{
		"test.md",
		"test-workflow.md",
		"testing.md",
	}
	for _, wf := range testWorkflows {
		f, err := os.Create(filepath.Join(workflowsDir, wf))
		require.NoError(t, err)
		f.Close()
	}

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	cmd := &cobra.Command{}

	// Test exact match - should still return the match and any others with same prefix
	completions, directive := CompleteWorkflowNames(cmd, nil, "test")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.Len(t, completions, 3, "Should return all workflows starting with 'test'")
	assert.Contains(t, completions, "test")
	assert.Contains(t, completions, "test-workflow")
	assert.Contains(t, completions, "testing")
}

// TestCompleteWorkflowNamesLongNames tests handling of very long workflow names
func TestCompleteWorkflowNamesLongNames(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create workflow with very long name (255 chars is typical filesystem limit for filename)
	longName := strings.Repeat("a", 200) + "-workflow"
	longWorkflowFile := longName + ".md"
	f, err := os.Create(filepath.Join(workflowsDir, longWorkflowFile))
	require.NoError(t, err)
	f.Close()

	// Create normal workflow
	normalWorkflow := "normal-workflow.md"
	f, err = os.Create(filepath.Join(workflowsDir, normalWorkflow))
	require.NoError(t, err)
	f.Close()

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	cmd := &cobra.Command{}

	// Test completion with empty prefix should include both
	completions, directive := CompleteWorkflowNames(cmd, nil, "")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.Len(t, completions, 2)
	assert.Contains(t, completions, longName)
	assert.Contains(t, completions, "normal-workflow")

	// Test completion with long prefix
	longPrefix := strings.Repeat("a", 100)
	completions, directive = CompleteWorkflowNames(cmd, nil, longPrefix)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.Len(t, completions, 1)
	assert.Contains(t, completions, longName)
}

// TestCompleteEngineNamesExactMatch tests when toComplete exactly matches an engine name
func TestCompleteEngineNamesExactMatch(t *testing.T) {
	cmd := &cobra.Command{}

	// Test exact match - should still return the matching engine
	completions, directive := CompleteEngineNames(cmd, nil, "copilot")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.Len(t, completions, 1)
	assert.Contains(t, completions, "copilot")
}

// TestCompleteEngineNamesCaseSensitivity tests engine name completion is case-sensitive
func TestCompleteEngineNamesCaseSensitivity(t *testing.T) {
	cmd := &cobra.Command{}

	tests := []struct {
		name       string
		toComplete string
		wantLen    int
	}{
		{
			name:       "lowercase copilot",
			toComplete: "copilot",
			wantLen:    1,
		},
		{
			name:       "uppercase COPILOT should not match",
			toComplete: "COPILOT",
			wantLen:    0,
		},
		{
			name:       "mixed case Copilot should not match",
			toComplete: "Copilot",
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completions, directive := CompleteEngineNames(cmd, nil, tt.toComplete)
			assert.Len(t, completions, tt.wantLen, "Expected %d completions", tt.wantLen)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
		})
	}
}

// TestValidEngineNamesConsistency tests that ValidEngineNames returns consistent results
func TestValidEngineNamesConsistency(t *testing.T) {
	// Call multiple times to ensure consistency
	firstCall := ValidEngineNames()
	secondCall := ValidEngineNames()
	thirdCall := ValidEngineNames()

	// Verify same length across calls
	assert.Len(t, secondCall, len(firstCall), "Engine names list length should be consistent")
	assert.Len(t, thirdCall, len(secondCall), "Engine names list length should be consistent")

	// Verify all expected engines are present in all calls
	expectedEngines := []string{"copilot", "claude", "codex", "custom"}
	for _, engine := range expectedEngines {
		assert.Contains(t, firstCall, engine, "Expected engine '%s' in first call", engine)
		assert.Contains(t, secondCall, engine, "Expected engine '%s' in second call", engine)
		assert.Contains(t, thirdCall, engine, "Expected engine '%s' in third call", engine)
	}

	// Verify no empty strings
	for _, engine := range firstCall {
		assert.NotEmpty(t, engine, "Engine name should not be empty")
	}
}
