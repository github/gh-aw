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

const selfImportRootWorkflow = `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Self Import Root Workflow

Workflow declared by the package root manifest.
`

const selfImportChildWorkflow = `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Self Import Child Workflow

Workflow declared by the nested child manifest.
`

// writeSelfImportPackageFixture creates a committed local package with root workflow files
// and a self-contained nested child package. rootImportsChild controls whether the root
// manifest imports child/aw.yml; childImport is the single import declared by the child
// manifest, which authors typically write while intending to reference the package root.
func writeSelfImportPackageFixture(t *testing.T, rootImportsChild bool, childImport string) string {
	t.Helper()

	repoDir := testutil.TempDir(t, "test-self-import-package-*")
	packageDir := filepath.Join(repoDir, "local-package")

	rootManifest := "name: Local Package\nincludes:\n  - workflows/root.md\n"
	if rootImportsChild {
		rootManifest += "  - child/aw.yml\n"
	}

	writePackageTestFile(t, packageDir, "README.md", "# Local Package\n")
	writePackageTestFile(t, packageDir, "aw.yml", rootManifest)
	writePackageTestFile(t, packageDir, "workflows/root.md", selfImportRootWorkflow)
	writePackageTestFile(t, packageDir, "child/README.md", "# Child Package\n")
	writePackageTestFile(t, packageDir, "child/aw.yml", "name: Child\nincludes:\n  - "+childImport+"\n  - workflows/child.md\n")
	writePackageTestFile(t, packageDir, "child/workflows/child.md", selfImportChildWorkflow)

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

func selfImportWarning(packageDir string) string {
	return "Ignoring includes entry \"aw.yml\" in " + filepath.Join(packageDir, "child", "aw.yml") +
		" because a manifest cannot import itself"
}

func TestResolveLocalRepositoryPackageNestedManifestSelfImport(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, true, "./aw.yml")

	pkg, err := resolveLocalRepositoryPackage(packageDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, []string{
		filepath.Join(packageDir, "child", "workflows", "child.md"),
		filepath.Join(packageDir, "workflows", "root.md"),
	}, packageInstallableSourcePaths(pkg.InstallationSource))
	assert.Contains(t, pkg.Warnings, selfImportWarning(packageDir))
}

func TestAddWorkflowsLocalPackageNestedManifestSelfImport(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, true, "./aw.yml")

	targetDir := testutil.TempDir(t, "test-self-import-target-*")
	setupMinimalGitRepo(t, targetDir)

	_, err := AddWorkflows(context.Background(), []string{packageDir}, AddOptions{
		NoGitattributes:        true,
		DisableSecurityScanner: true,
		Quiet:                  true,
	})
	require.NoError(t, err)

	rootContent, err := os.ReadFile(filepath.Join(targetDir, ".github", "workflows", "root.md"))
	require.NoError(t, err)
	assert.Contains(t, string(rootContent), "# Self Import Root Workflow")
	assert.FileExists(t, filepath.Join(targetDir, ".github", "workflows", "root.lock.yml"))

	childContent, err := os.ReadFile(filepath.Join(targetDir, ".github", "workflows", "child.md"))
	require.NoError(t, err)
	assert.Contains(t, string(childContent), "# Self Import Child Workflow")
}

// TestResolveLocalRepositoryPackageChildPackageSelfImport covers the child manifest installed
// as its own package, with a repository root manifest that does not import it.
func TestResolveLocalRepositoryPackageChildPackageSelfImport(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, false, "./aw.yml")
	childDir := filepath.Join(packageDir, "child")

	pkg, err := resolveLocalRepositoryPackage(childDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, []string{
		filepath.Join(childDir, "workflows", "child.md"),
	}, packageInstallableSourcePaths(pkg.InstallationSource))
	assert.Contains(t, pkg.Warnings, selfImportWarning(packageDir))
}

// TestResolveLocalRepositoryPackageChildPackageImportsParentManifest covers a nested
// package importing the manifest above it. The root manifest does not import the child, so
// this is a plain upward dependency rather than a cycle.
func TestResolveLocalRepositoryPackageChildPackageImportsParentManifest(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, false, "../aw.yml")
	childDir := filepath.Join(packageDir, "child")

	pkg, err := resolveLocalRepositoryPackage(childDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, []string{
		filepath.Join(packageDir, "workflows", "root.md"),
		filepath.Join(childDir, "workflows", "child.md"),
	}, packageInstallableSourcePaths(pkg.InstallationSource))
}

// TestAddWorkflowsLocalPackageChildPackageImportsParentManifest installs a nested package
// that imports the manifest above it and verifies the resolved root files.
func TestAddWorkflowsLocalPackageChildPackageImportsParentManifest(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, false, "../aw.yml")

	targetDir := testutil.TempDir(t, "test-parent-import-target-*")
	setupMinimalGitRepo(t, targetDir)

	_, err := AddWorkflows(context.Background(), []string{filepath.Join(packageDir, "child")}, AddOptions{
		NoGitattributes:        true,
		DisableSecurityScanner: true,
		Quiet:                  true,
	})
	require.NoError(t, err)

	rootContent, err := os.ReadFile(filepath.Join(targetDir, ".github", "workflows", "root.md"))
	require.NoError(t, err)
	assert.Contains(t, string(rootContent), "# Self Import Root Workflow")
	assert.FileExists(t, filepath.Join(targetDir, ".github", "workflows", "root.lock.yml"))

	childContent, err := os.ReadFile(filepath.Join(targetDir, ".github", "workflows", "child.md"))
	require.NoError(t, err)
	assert.Contains(t, string(childContent), "# Self Import Child Workflow")
}

// TestResolveLocalRepositoryPackageImportOutsideRepository rejects an import that reaches
// above the git repository containing the package.
func TestResolveLocalRepositoryPackageImportOutsideRepository(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, false, "../../../aw.yml")

	_, err := resolveLocalRepositoryPackage(filepath.Join(packageDir, "child"))
	require.ErrorContains(t, err, `import "../../../aw.yml" resolves outside the repository root`)
	require.ErrorContains(t, err, filepath.Join(packageDir, "child", "aw.yml"))
}

// TestResolveLocalRepositoryPackageImportCycleThroughNestedManifest covers a genuine cycle:
// the root manifest imports the child, and the child imports the root back.
func TestResolveLocalRepositoryPackageImportCycleThroughNestedManifest(t *testing.T) {
	packageDir := writeSelfImportPackageFixture(t, true, "../aw.yml")

	_, err := resolveLocalRepositoryPackage(packageDir)
	require.ErrorContains(t, err, "package manifest import cycle detected")
	require.ErrorContains(t, err, filepath.Join(packageDir, "child", "aw.yml"))
}

// TestResolveLocalRepositoryPackageImportAboveRootWithoutGit covers the non-git fallback
// branch of localPackageImportRoot: outside a git repository, imports remain bounded by the
// package directory itself, so a nested package reaching above it with "../aw.yml" is
// rejected the same way it would be if it reached above the git root.
func TestResolveLocalRepositoryPackageImportAboveRootWithoutGit(t *testing.T) {
	packageDir := testutil.TempDir(t, "test-no-git-package-*")

	writePackageTestFile(t, packageDir, "README.md", "# Local Package\n")
	writePackageTestFile(t, packageDir, "aw.yml", "name: Local Package\nincludes:\n  - workflows/root.md\n")
	writePackageTestFile(t, packageDir, "workflows/root.md", selfImportRootWorkflow)
	writePackageTestFile(t, packageDir, "child/README.md", "# Child Package\n")
	writePackageTestFile(t, packageDir, "child/aw.yml", "name: Child\nincludes:\n  - ../aw.yml\n  - workflows/child.md\n")
	writePackageTestFile(t, packageDir, "child/workflows/child.md", selfImportChildWorkflow)

	_, err := resolveLocalRepositoryPackage(filepath.Join(packageDir, "child"))
	require.ErrorContains(t, err, `import "../aw.yml" resolves outside the repository root`)
	require.ErrorContains(t, err, filepath.Join(packageDir, "child", "aw.yml"))
}

// TestResolveLocalRepositoryPackageChildPackageImportsParentManifestRootRelativeAsset covers
// a nested package importing a parent manifest that lists a repository-root-relative
// ".github/workflows/" entry. That entry must resolve against the enclosing git repository
// root, not against the nested package directory selected for installation.
func TestResolveLocalRepositoryPackageChildPackageImportsParentManifestRootRelativeAsset(t *testing.T) {
	repoDir := testutil.TempDir(t, "test-parent-import-root-relative-*")
	packageDir := filepath.Join(repoDir, "local-package")
	childDir := filepath.Join(packageDir, "child")

	writePackageTestFile(t, repoDir, ".github/workflows/root.md", selfImportRootWorkflow)
	writePackageTestFile(t, packageDir, "README.md", "# Local Package\n")
	writePackageTestFile(t, packageDir, "aw.yml", "name: Local Package\nincludes:\n  - .github/workflows/root.md\n")
	writePackageTestFile(t, packageDir, "child/README.md", "# Child Package\n")
	writePackageTestFile(t, packageDir, "child/aw.yml", "name: Child\nincludes:\n  - ../aw.yml\n  - workflows/child.md\n")
	writePackageTestFile(t, packageDir, "child/workflows/child.md", selfImportChildWorkflow)

	runGitFixtureCommand(t, repoDir, "init")
	runGitFixtureCommand(t, repoDir, "config", "user.name", "Test User")
	runGitFixtureCommand(t, repoDir, "config", "user.email", "test@example.com")
	runGitFixtureCommand(t, repoDir, "add", ".github", "local-package")
	runGitFixtureCommand(t, repoDir, "commit", "-m", "Add local package fixture")

	pkg, err := resolveLocalRepositoryPackage(childDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, []string{
		filepath.Join(repoDir, ".github", "workflows", "root.md"),
		filepath.Join(childDir, "workflows", "child.md"),
	}, packageInstallableSourcePaths(pkg.InstallationSource))
}
