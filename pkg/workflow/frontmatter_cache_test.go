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

// TestParsedFrontmatterCaching tests that the ParsedFrontmatter field is properly initialized
// and contains the cached 'on' field from frontmatter
func TestParsedFrontmatterCaching(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-cache-test")

	tests := []struct {
		name           string
		frontmatter    string
		markdown       string
		expectCached   bool
		expectOnExists bool
	}{
		{
			name: "simple push trigger",
			frontmatter: `---
on: push
engine: copilot
---`,
			markdown:       "# Test workflow\n\nTest content",
			expectCached:   false, // Simple string 'on' values don't populate the cache (no benefit anyway)
			expectOnExists: false,
		},
		{
			name: "complex on section with pull_request",
			frontmatter: `---
on:
  pull_request:
    types: [opened, synchronize]
    branches: [main]
engine: copilot
---`,
			markdown:       "# Test workflow\n\nTest content",
			expectCached:   true,
			expectOnExists: true,
		},
		{
			name: "on section with draft filter",
			frontmatter: `---
on:
  pull_request:
    draft: true
engine: copilot
---`,
			markdown:       "# Test workflow\n\nTest content",
			expectCached:   true,
			expectOnExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + "\n\n" + tt.markdown
			testFile := filepath.Join(tmpDir, "test-"+tt.name+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()
			workflowData, err := compiler.ParseWorkflowFile(testFile)
			require.NoError(t, err)
			require.NotNil(t, workflowData)

			// Verify ParsedFrontmatter is initialized
			if tt.expectCached {
				assert.NotNil(t, workflowData.ParsedFrontmatter, "ParsedFrontmatter should be initialized")

				if tt.expectOnExists && workflowData.ParsedFrontmatter != nil {
					assert.NotNil(t, workflowData.ParsedFrontmatter.On, "ParsedFrontmatter.On should not be nil")
				}
			}
		})
	}
}

// TestParsedFrontmatterUsedInFilters tests that filter functions use the cached field
func TestParsedFrontmatterUsedInFilters(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-filter-test")

	testContent := `---
on:
  pull_request:
    draft: false
    branches: [main]
engine: copilot
---

# Test workflow

Test content`

	testFile := filepath.Join(tmpDir, "test-filter.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify ParsedFrontmatter is initialized and contains On field
	assert.NotNil(t, workflowData.ParsedFrontmatter, "ParsedFrontmatter should be initialized")
	assert.NotNil(t, workflowData.ParsedFrontmatter.On, "ParsedFrontmatter.On should not be nil")

	// Verify the If condition was applied (draft filter should add a condition)
	assert.NotEmpty(t, workflowData.If, "If condition should be set by draft filter")
}
