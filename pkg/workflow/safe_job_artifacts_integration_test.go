//go:build integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

const safeJobArtifactsWorkflow = `---
on: workflow_dispatch
permissions: read-all
engine: copilot
safe-outputs:
  jobs:
    publish-review-bundle:
      description: "Publish a prepared review bundle"
      runs-on: ubuntu-latest
      artifacts:
%s
      inputs:
        source_dir:
          description: "Directory containing prepared files"
          required: true
          type: string
      steps:
        - run: echo "process bundle"
---

# Safe-job artifacts
`

func TestSafeJobArtifactsIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "safe-job-artifacts-integration")
	workflowPath := filepath.Join(tmpDir, "safe-job-artifacts.md")
	content := fmt.Sprintf(safeJobArtifactsWorkflow, `        - /tmp/gh-aw/agent/review-bundles/
        - /tmp/gh-aw/agent/review-bundles/*.json`)
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	compiled, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	agentJob := extractJobSection(string(compiled), "agent")
	require.Contains(t, agentJob, "name: Upload agent artifacts")
	for _, ext := range secretRedactionScannedExtensions {
		require.Contains(t, agentJob, "/tmp/gh-aw/agent/review-bundles/**/*"+ext)
	}
	require.Contains(t, agentJob, "/tmp/gh-aw/agent/review-bundles/*.json")
	require.NotContains(t, agentJob, "\n            /tmp/gh-aw/agent/review-bundles/\n")

	redactIndex := strings.Index(agentJob, "name: Redact secrets in logs")
	uploadIndex := strings.Index(agentJob, "name: Upload agent artifacts")
	require.Greater(t, redactIndex, -1)
	require.Greater(t, uploadIndex, redactIndex)
	require.Contains(t, agentJob[uploadIndex:], "if: always() && steps.redact_secrets.outcome == 'success'")
}

func TestSafeJobArtifactsValidationIntegration(t *testing.T) {
	tests := []struct {
		name              string
		artifactPath      string
		errorPart         string
		safeJobValidation bool
	}{
		{
			name:         "outside redaction root",
			artifactPath: "/tmp/review-bundles/",
			errorPart:    "does not match pattern '^/tmp/gh-aw/'",
		},
		{
			name:              "path traversal",
			artifactPath:      "/tmp/gh-aw/../outside.json",
			errorPart:         "path traversal",
			safeJobValidation: true,
		},
		{
			name:              "GitHub Actions expression",
			artifactPath:      "/tmp/gh-aw/${{ '..' }}/outside.json",
			errorPart:         "must be literal",
			safeJobValidation: true,
		},
		{
			name:              "hidden file",
			artifactPath:      "/tmp/gh-aw/agent/review-bundles/.manifest",
			errorPart:         "hidden files or directories",
			safeJobValidation: true,
		},
		{
			name:              "unscannable path",
			artifactPath:      "/tmp/gh-aw/agent/review-bundle-data",
			errorPart:         "extension covered by secret redaction",
			safeJobValidation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "safe-job-artifacts-validation")
			workflowPath := filepath.Join(tmpDir, "invalid.md")
			artifacts := fmt.Sprintf("        - %s", tt.artifactPath)
			require.NoError(t, os.WriteFile(workflowPath, []byte(fmt.Sprintf(safeJobArtifactsWorkflow, artifacts)), 0o644))

			err := NewCompiler().CompileWorkflow(workflowPath)
			require.Error(t, err)
			if tt.safeJobValidation {
				require.ErrorContains(t, err, "invalid artifacts for safe-job 'publish_review_bundle'")
			}
			require.ErrorContains(t, err, tt.errorPart)
			require.NotContains(t, err.Error(), "This is a compiler bug")
		})
	}
}
