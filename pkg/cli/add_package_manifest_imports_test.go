//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractManifestIncludesWithPackageImports(t *testing.T) {
	manifest, _, err := parseRepositoryPackageManifest("aw.yml", []byte(`name: Bundle
includes:
  - activity/aw.yml
  - dashboard/../activity/aw.yml
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"activity/aw.yml"}, manifest.Imports)

	_, _, err = parseRepositoryPackageManifest("aw.yml", []byte("name: Bundle\nincludes:\n  - /tmp/aw.yml\n"))
	require.ErrorContains(t, err, "absolute paths are not allowed")
}

func TestResolveRepositoryPackageManifestGraph(t *testing.T) {
	manifests := map[string]string{
		"aw.yml":            "name: Root\nincludes:\n  - packages/a/aw.yml\n",
		"packages/a/aw.yml": "name: A\nincludes:\n  - ../b/aw.yml\n",
		"packages/b/aw.yml": "name: B\nincludes:\n  - workflows/b.md\n",
	}
	root, _, err := parseRepositoryPackageManifest("aw.yml", []byte(manifests["aw.yml"]))
	require.NoError(t, err)

	nodes, _, err := resolveRepositoryPackageManifestGraph("aw.yml", root, func(path string) ([]byte, error) {
		content, ok := manifests[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	})
	require.NoError(t, err)
	require.Len(t, nodes, 3)
	assert.Equal(t, []string{"packages/b/aw.yml", "packages/a/aw.yml", "aw.yml"}, []string{nodes[0].Path, nodes[1].Path, nodes[2].Path})
}

func TestResolveRepositoryPackageManifestGraphSharedImport(t *testing.T) {
	manifests := map[string]string{
		"aw.yml":                 "name: Root\nincludes:\n  - packages/a/aw.yml\n  - packages/b/aw.yml\n",
		"packages/a/aw.yml":      "name: A\nincludes:\n  - ../shared/aw.yml\n",
		"packages/b/aw.yml":      "name: B\nincludes:\n  - ../shared/aw.yml\n",
		"packages/shared/aw.yml": "name: Shared\nincludes:\n  - workflows/shared.md\n",
	}
	root, _, err := parseRepositoryPackageManifest("aw.yml", []byte(manifests["aw.yml"]))
	require.NoError(t, err)

	nodes, _, err := resolveRepositoryPackageManifestGraph("aw.yml", root, func(path string) ([]byte, error) {
		content, ok := manifests[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"packages/shared/aw.yml",
		"packages/a/aw.yml",
		"packages/b/aw.yml",
		"aw.yml",
	}, []string{nodes[0].Path, nodes[1].Path, nodes[2].Path, nodes[3].Path})
}

func TestResolveRepositoryPackageManifestGraphCycle(t *testing.T) {
	manifests := map[string]string{
		"aw.yml":            "name: Root\nincludes:\n  - packages/a/aw.yml\n",
		"packages/a/aw.yml": "name: A\nincludes:\n  - ../../aw.yml\n",
	}
	root, _, err := parseRepositoryPackageManifest("aw.yml", []byte(manifests["aw.yml"]))
	require.NoError(t, err)

	_, _, err = resolveRepositoryPackageManifestGraph("aw.yml", root, func(path string) ([]byte, error) {
		return []byte(manifests[path]), nil
	})
	require.ErrorContains(t, err, "package manifest import cycle detected")
	require.ErrorContains(t, err, "aw.yml -> packages/a/aw.yml -> aw.yml")
}

func TestReadLocalImportedManifestSymlinks(t *testing.T) {
	realRoot := t.TempDir()
	writePackageTestFile(t, realRoot, "child/aw.yml", "name: Child\n")
	symlinkRoot := filepath.Join(t.TempDir(), "package")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	_, err := readLocalImportedManifest(filepath.Join(symlinkRoot, "child", "aw.yml"), symlinkRoot)
	require.NoError(t, err)

	outsideManifest := filepath.Join(t.TempDir(), "aw.yml")
	require.NoError(t, os.WriteFile(outsideManifest, []byte("name: Outside\n"), 0o644))
	internalLink := filepath.Join(realRoot, "linked")
	require.NoError(t, os.Symlink(filepath.Dir(outsideManifest), internalLink))
	_, err = readLocalImportedManifest(filepath.Join(realRoot, "linked", "aw.yml"), realRoot)
	require.ErrorContains(t, err, "outside the package root")
}

func TestIsPathWithinPackageRoot(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		root      string
		want      bool
	}{
		{name: "empty root allows sibling path", candidate: "activity/aw.yml", root: "", want: true},
		{name: "empty root rejects parent traversal", candidate: "..", root: "", want: false},
		{name: "empty root rejects parent-prefixed path", candidate: "../aw.yml", root: "", want: false},
		{name: "empty root rejects leading backslash", candidate: `\aw.yml`, root: "", want: false},
		{name: "matches root exactly", candidate: "packages/a", root: "packages/a", want: true},
		{name: "matches path under root", candidate: "packages/a/aw.yml", root: "packages/a", want: true},
		{name: "rejects path outside root", candidate: "packages/b/aw.yml", root: "packages/a", want: false},
		{name: "rejects backslash immediately after root separator", candidate: `packages/a/\..\aw.yml`, root: "packages/a", want: false},
		{name: "rejects embedded backslash anywhere in path", candidate: `packages/a/sub\dir/aw.yml`, root: "packages/a", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPathWithinPackageRoot(tt.candidate, tt.root))
		})
	}
}

func TestResolveLocalRepositoryPackageImports(t *testing.T) {
	packageDir := t.TempDir()
	writePackageTestFile(t, packageDir, "README.md", "# Bundle\n")
	writePackageTestFile(t, packageDir, "aw.yml", `name: Bundle
includes:
  - activity/aw.yml
  - dashboard/aw.yml
`)
	writePackageTestFile(t, packageDir, "activity/aw.yml", `name: Activity
includes:
  - workflows/activity.md
resources:
  - source: config.json
    destination: .github/aw/activity/config.json
`)
	writePackageTestFile(t, packageDir, "activity/workflows/activity.md", "# Activity\n")
	writePackageTestFile(t, packageDir, "activity/config.json", "{}\n")
	writePackageTestFile(t, packageDir, "dashboard/aw.yml", `name: Dashboard
includes:
  - workflows/dashboard.md
`)
	writePackageTestFile(t, packageDir, "dashboard/workflows/dashboard.md", "# Dashboard\n")

	pkg, err := resolveLocalRepositoryPackage(packageDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	require.Len(t, pkg.InstallationSource, 2)
	assert.Equal(t, []string{
		filepath.Join(packageDir, "activity", "workflows", "activity.md"),
		filepath.Join(packageDir, "dashboard", "workflows", "dashboard.md"),
	}, packageInstallableSourcePaths(pkg.InstallationSource))
	require.Len(t, pkg.ResourceFiles, 1)
	assert.Equal(t, filepath.Join(packageDir, "activity", "config.json"), pkg.ResourceFiles[0].SourcePath)
}

func TestResolveLocalRepositoryPackageImportRootRelativeWorkflow(t *testing.T) {
	packageDir := t.TempDir()
	writePackageTestFile(t, packageDir, "README.md", "# Bundle\n")
	writePackageTestFile(t, packageDir, "aw.yml", `name: Bundle
includes:
  - ambient-context/aw.yml
`)
	writePackageTestFile(t, packageDir, "ambient-context/aw.yml", `name: Ambient Context
includes:
  - .github/workflows/ambient-context.md
`)
	writePackageTestFile(t, packageDir, ".github/workflows/ambient-context.md", "# Ambient Context\n")

	pkg, err := resolveLocalRepositoryPackage(packageDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, []string{
		filepath.Join(packageDir, ".github", "workflows", "ambient-context.md"),
	}, packageInstallableSourcePaths(pkg.InstallationSource))
	assert.NoFileExists(t, filepath.Join(packageDir, "ambient-context", ".github", "workflows", "ambient-context.md"))
}

func TestResolveLocalRepositoryPackageImportClash(t *testing.T) {
	packageDir := t.TempDir()
	writePackageTestFile(t, packageDir, "README.md", "# Bundle\n")
	writePackageTestFile(t, packageDir, "aw.yml", `name: Bundle
includes:
  - first/aw.yml
  - second/aw.yml
`)
	for _, dir := range []string{"first", "second"} {
		writePackageTestFile(t, packageDir, dir+"/aw.yml", `name: Child
includes:
  - workflows/shared.md
`)
		writePackageTestFile(t, packageDir, dir+"/workflows/shared.md", "# Shared\n")
	}

	_, err := resolveLocalRepositoryPackage(packageDir)
	require.ErrorContains(t, err, "both install to")
	require.ErrorContains(t, err, ".github/workflows/shared.md")
}

func writePackageTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}
