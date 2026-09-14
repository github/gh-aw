//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const selfImportPackageWorkflow = `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Self Import Package Workflow

Workflow installed from a package whose nested manifest imports the package root.
`

// writeSelfImportPackageFixture creates a committed local package whose root manifest
// declares the package workflow files and imports a nested manifest. The nested manifest
// declares the supplied import path, which authors typically write while intending to
// reference the package root manifest.
func writeSelfImportPackageFixture(t *testing.T, rootImport string) string {
	t.Helper()

	repoDir := testutil.TempDir(t, "test-self-import-package-*")
	packageDir := filepath.Join(repoDir, "local-package")

	writePackageTestFile(t, packageDir, "README.md", "# Local Package\n")
	writePackageTestFile(t, packageDir, "aw.yml", `name: Local Package
includes:
  - workflows/root.md
  - child/aw.yml
`)
	writePackageTestFile(t, packageDir, "workflows/root.md", selfImportPackageWorkflow)
	writePackageTestFile(t, packageDir, "child/aw.yml", "name: Child\nincludes:\n  - "+rootImport+"\n")

	runGitFixtureCommand(t, repoDir, "init")
	runGitFixtureCommand(t, repoDir, "config", "user.name", "Test User")
	runGitFixtureCommand(t, repoDir, "config", "user.email", "test@example.com")
	runGitFixtureCommand(t, repoDir, "add", "local-package")
	runGitFixtureCommand(t, repoDir, "commit", "-m", "Add local package fixture")

	return packageDir
}

func runGitFixtureCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git command failed: %s", string(output))
}

func TestResolveLocalRepositoryPackageNestedManifestSelfImport(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, "./aw.yml")

	pkg, err := resolveLocalRepositoryPackage(packageDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, []string{
		filepath.Join(packageDir, "workflows", "root.md"),
	}, packageInstallableSourcePaths(pkg.InstallationSource))
	assert.Contains(t, pkg.Warnings, "Ignoring includes entry \"aw.yml\" in "+filepath.Join(packageDir, "child", "aw.yml")+" because a manifest cannot import itself")
}

func TestAddWorkflowsLocalPackageNestedManifestSelfImport(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, "./aw.yml")

	targetDir := testutil.TempDir(t, "test-self-import-target-*")
	setupMinimalGitRepo(t, targetDir)

	_, err := AddWorkflows(context.Background(), []string{packageDir}, AddOptions{
		NoGitattributes:        true,
		DisableSecurityScanner: true,
		Quiet:                  true,
	})
	require.NoError(t, err)

	installed := filepath.Join(targetDir, ".github", "workflows", "root.md")
	content, err := os.ReadFile(installed)
	require.NoError(t, err)
	assert.Contains(t, string(content), "# Self Import Package Workflow")
	assert.FileExists(t, filepath.Join(targetDir, ".github", "workflows", "root.lock.yml"))
}

func TestResolveLocalRepositoryPackageNestedManifestImportsPackageRoot(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, "../aw.yml")

	_, err := resolveLocalRepositoryPackage(packageDir)
	require.ErrorContains(t, err, "package manifest import cycle detected")
	require.ErrorContains(t, err, filepath.Join(packageDir, "child", "aw.yml"))
}
