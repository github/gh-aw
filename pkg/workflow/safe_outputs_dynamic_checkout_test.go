//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileDynamicCheckoutWorkflow(t *testing.T, safeOutputs string) string {
	t.Helper()
	tmpDir := testutil.TempDir(t, "dynamic-checkout-test")
	content := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
` + safeOutputs + `
---

# Test

Do something.
`
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))
	lock, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err)
	return string(lock)
}

func TestSafeOutputsDynamicCheckoutCompile(t *testing.T) {
	t.Run("dynamic-checkout removes actions/checkout from safe_outputs", func(t *testing.T) {
		lock := compileDynamicCheckoutWorkflow(t, `  dynamic-checkout: true
  create-pull-request:
  push-to-pull-request-branch:`)
		job := extractJobSection(lock, "safe_outputs")
		require.NotEmpty(t, job, "safe_outputs job should exist")

		assert.NotContains(t, job, "name: Checkout repository", "safe_outputs should not check out the target repository")
		assert.NotContains(t, job, "persist-credentials: true", "safe_outputs should not persist checkout credentials")
		assert.NotContains(t, job, "configure_git_credentials.sh", "safe_outputs should not run the git credential setup step")
		assert.Contains(t, job, "Download patch artifact", "patch artifact must still be downloaded")
		assert.Contains(t, job, "GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", "handlers need GITHUB_TOKEN to materialize repositories")
		assert.Contains(t, job, `\"dynamic_checkout\":true`, "handler config should enable dynamic_checkout")
	})

	t.Run("default keeps actions/checkout with persisted credentials", func(t *testing.T) {
		lock := compileDynamicCheckoutWorkflow(t, `  create-pull-request:`)
		job := extractJobSection(lock, "safe_outputs")
		require.NotEmpty(t, job, "safe_outputs job should exist")

		assert.Contains(t, job, "name: Checkout repository", "safe_outputs should keep the repository checkout by default")
		assert.Contains(t, job, "persist-credentials: true", "default safe_outputs checkout persists credentials")
		assert.NotContains(t, job, "dynamic_checkout", "handler config should not enable dynamic_checkout by default")
	})
}
