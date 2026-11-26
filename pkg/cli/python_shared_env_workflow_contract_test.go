//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/stretchr/testify/require"
)

const portableVenvCommand = "uv venv --python 3.12 --python-preference only-managed --seed /tmp/gh-aw/python/venv"

// Each shared component installs its own packages additively into the shared portable environment.
var sharedPythonPackageInstalls = map[string]string{
	"python-dataviz.md":         "/tmp/gh-aw/python/venv/bin/pip install --quiet numpy==2.5.4 pandas==3.0.6 matplotlib==3.11.2 seaborn==0.13.2 scipy==1.18.1",
	"python-nlp.md":             "/tmp/gh-aw/python/venv/bin/pip install --quiet nltk==3.10.3 scikit-learn==1.9.1 textblob==0.20.1 wordcloud==1.9.6",
	"trending-charts-simple.md": "uv pip install --quiet --python /tmp/gh-aw/python/venv/bin/python contourpy==1.4.0 cycler==0.12.1 fonttools==4.69.0 kiwisolver==1.5.1 matplotlib==3.11.2 numpy==2.5.4 packaging==26.4 pandas==3.0.6 pillow==13.0.0 pyparsing==3.3.3 python-dateutil==4.0.0 scipy==1.18.1 seaborn==0.13.2 tzdata==2026.6",
}

// Shared Python imports write into the same /tmp/gh-aw/python/venv, so every component that can
// create it first must provision the same uv-managed CPython. Otherwise a runner-CPython venv
// created by one import is reused by the others and fails to load native wheels in the sandbox.
func TestSharedPythonImportsUsePortableManagedPython(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	require.NoError(t, err)

	for sharedWorkflow, packageInstall := range sharedPythonPackageInstalls {
		t.Run(sharedWorkflow, func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "shared", sharedWorkflow))
			require.NoError(t, err)
			workflowContent := string(content)

			require.Contains(t, workflowContent, "UV_PYTHON_INSTALL_DIR: /tmp/gh-aw/python/uv-python")
			require.Contains(t, workflowContent, portableVenvCommand)
			require.Contains(t, workflowContent, packageInstall)
			require.NotContains(t, workflowContent, "python3 -m venv")
			// Recreating the shared environment would discard a sibling import's packages.
			require.NotContains(t, workflowContent, "rm -rf /tmp/gh-aw/python/venv")
		})
	}
}

// Consumers compile the shared setup steps into their own job, so the compiled lock files must
// carry the same contract as the markdown sources: a hand-edited lock file that reverts to
// runner CPython, or that recreates the shared environment, has to fail here.
func TestCompiledConsumersOnlyCreatePortableSharedVenv(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	require.NoError(t, err)

	workflowsDir := filepath.Join(repoRoot, ".github", "workflows")
	lockFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	require.NoError(t, err)

	// Discover consumers from the compiled output itself rather than a hardcoded list, so a new
	// workflow importing a shared Python component is covered without updating this test.
	consumers := 0
	for _, lockFile := range lockFiles {
		content, err := os.ReadFile(lockFile)
		require.NoError(t, err)
		lockContent := string(content)

		imported := strings.Contains(lockContent, "/tmp/gh-aw/python/venv/bin/pip install --quiet") ||
			strings.Contains(lockContent, "uv pip install --quiet --python /tmp/gh-aw/python/venv/bin/python")
		if !imported {
			continue
		}
		consumers++

		t.Run(filepath.Base(lockFile), func(t *testing.T) {
			t.Parallel()
			require.NotContains(t, lockContent, "python3 -m venv /tmp/gh-aw/python/venv")
			require.NotContains(t, lockContent, "rm -rf /tmp/gh-aw/python/venv")
			require.GreaterOrEqual(t, strings.Count(lockContent, portableVenvCommand), 1)
			require.NotContains(t, lockContent, "pip install --quiet numpy pandas")
			require.NotContains(t, lockContent, "pip install --quiet nltk scikit-learn")
			require.NotContains(t, lockContent, "uv pip install --quiet --python /tmp/gh-aw/python/venv/bin/python numpy pandas")
			if strings.Contains(lockContent, "/tmp/gh-aw/python/venv/bin/pip install --quiet numpy") {
				require.Contains(t, lockContent, sharedPythonPackageInstalls["python-dataviz.md"])
			}
			if strings.Contains(lockContent, "/tmp/gh-aw/python/venv/bin/pip install --quiet nltk") {
				require.Contains(t, lockContent, sharedPythonPackageInstalls["python-nlp.md"])
			}
			if strings.Contains(lockContent, "uv pip install --quiet --python /tmp/gh-aw/python/venv/bin/python") {
				require.Contains(t, lockContent, sharedPythonPackageInstalls["trending-charts-simple.md"])
			}
		})
	}

	// Guard against the discovery loop silently matching nothing.
	require.NotZero(t, consumers, "expected compiled consumers of the shared Python components")
}
