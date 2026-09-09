//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReportBlockedVersionFrontmatterFlag tests that on.report-blocked-version: false
// suppresses the blocked-version notification issue (env var + issues: write permission)
// without disabling the version check step itself, unlike check-for-updates: false.
func TestReportBlockedVersionFrontmatterFlag(t *testing.T) {
	baseWorkflowMD := `---
engine: copilot
on:
  issues:
    types: [opened]
---
Test workflow for blocked-version reporting.
`
	disabledWorkflowMD := `---
engine: copilot
on:
  issues:
    types: [opened]
  report-blocked-version: false
---
Test workflow for blocked-version reporting disabled.
`
	enabledExplicitWorkflowMD := `---
engine: copilot
on:
  issues:
    types: [opened]
  report-blocked-version: true
---
Test workflow for blocked-version reporting explicitly enabled.
`

	tests := []struct {
		name           string
		workflowMD     string
		wantReportTrue bool
	}{
		{
			name:           "reporting enabled when report-blocked-version not set (default)",
			workflowMD:     baseWorkflowMD,
			wantReportTrue: true,
		},
		{
			name:           "reporting disabled when report-blocked-version: false",
			workflowMD:     disabledWorkflowMD,
			wantReportTrue: false,
		},
		{
			name:           "reporting enabled when report-blocked-version: true explicitly",
			workflowMD:     enabledExplicitWorkflowMD,
			wantReportTrue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "report-blocked-version-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.workflowMD), 0644), "Should write workflow file")

			compiler := NewCompiler(WithVersion("v1.2.3"))
			originalIsRelease := isReleaseBuild
			isReleaseBuild = true
			t.Cleanup(func() { isReleaseBuild = originalIsRelease })

			err := compiler.CompileWorkflow(testFile)
			require.NoError(t, err, "Workflow should compile without errors")

			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Lock file should be readable")
			lockStr := string(lockContent)

			// The version check step itself must always be present; only the
			// notification issue reporting is toggled by report-blocked-version.
			assert.Contains(t, lockStr, "Check compile-agentic version",
				"Version check step should always be present regardless of report-blocked-version")

			if tt.wantReportTrue {
				assert.Contains(t, lockStr, "GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE: \"true\"")
				assert.Contains(t, lockStr, "issues: write")
			} else {
				assert.Contains(t, lockStr, "GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE: \"false\"")
			}

			// Verify report-blocked-version is commented out in the generated lock file when present
			if strings.Contains(tt.workflowMD, "report-blocked-version:") {
				assert.NotContains(t, lockStr, "\n  report-blocked-version:",
					"report-blocked-version should be commented out in the lock file, not left as an active YAML key")
				assert.Contains(t, lockStr, "# report-blocked-version:",
					"report-blocked-version should appear as a comment in the lock file")
			}
		})
	}
}
