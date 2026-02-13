//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionsAutoInference_Integration(t *testing.T) {
	tests := []struct {
		name              string
		frontmatter       string
		expectedToolsets  string
		shouldHaveWarning bool
		description       string
	}{
		{
			name: "auto-infer from partial permissions",
			frontmatter: `---
on: daily
permissions:
  contents: read
  actions: read
  issues: read
safe-outputs:
  create-issue:
---
Say hi`,
			expectedToolsets:  "context,repos,issues",
			shouldHaveWarning: false,
			description:       "Should infer compatible toolsets when permissions don't include pull-requests",
		},
		{
			name: "auto-infer from all default permissions",
			frontmatter: `---
on: daily
permissions:
  contents: read
  issues: read
  pull-requests: read
safe-outputs:
  create-issue:
---
Say hi`,
			expectedToolsets:  "context,repos,issues,pull_requests",
			shouldHaveWarning: false,
			description:       "Should infer all default toolsets when all permissions are provided",
		},
		{
			name: "explicit toolsets override auto-inference",
			frontmatter: `---
on: daily
permissions:
  contents: read
  issues: read
tools:
  github:
    toolsets: [repos, issues, pull_requests]
safe-outputs:
  create-issue:
---
Say hi`,
			expectedToolsets:  "repos,issues,pull_requests",
			shouldHaveWarning: true,
			description:       "Should use explicit toolsets even if incompatible, but show warning",
		},
		{
			name: "no permissions gives only context",
			frontmatter: `---
on: daily
permissions: {}
safe-outputs:
  create-issue:
---
Say hi`,
			expectedToolsets:  "context",
			shouldHaveWarning: false,
			description:       "Should infer only context toolset when no permissions granted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary markdown file
			tmpDir := t.TempDir()
			mdPath := filepath.Join(tmpDir, "test.md")
			require.NoError(t, os.WriteFile(mdPath, []byte(tt.frontmatter), 0644))

			// Compile the workflow
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(mdPath)

			if tt.shouldHaveWarning {
				// In non-strict mode, warnings don't cause errors
				assert.NoError(t, err, "Compilation should succeed with warnings")
				assert.Greater(t, compiler.GetWarningCount(), 0, "Should have warnings")
			} else {
				assert.NoError(t, err, "Compilation should succeed without warnings")
				assert.Equal(t, 0, compiler.GetWarningCount(), "Should have no warnings")
			}

			// Read the generated lock file
			lockPath := strings.TrimSuffix(mdPath, ".md") + ".lock.yml"
			lockContent, err := os.ReadFile(lockPath)
			require.NoError(t, err, "Lock file should be generated")

			// Verify the toolsets in the lock file
			lockStr := string(lockContent)
			if strings.Contains(lockStr, "GITHUB_TOOLSETS") {
				assert.Contains(t, lockStr, `"GITHUB_TOOLSETS": "`+tt.expectedToolsets+`"`, tt.description)
			}
		})
	}
}
