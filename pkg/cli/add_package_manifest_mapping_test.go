//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanManifestRelativePathRejectsAbsoluteForms(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"/tmp/reviewer.md", `\tmp\reviewer.md`, `C:/tmp/reviewer.md`} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := cleanManifestRelativePath(input)
			assert.EqualError(t, err, "absolute paths are not allowed")
		})
	}
}

// setupMappingPackageTest wires the package resolution hooks so that only the manifest and
// README of a package are available, and auto-scan is disabled.
func setupMappingPackageTest(t *testing.T, manifest map[string]string) {
	t.Helper()
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		downloadPackageFileFromGitHubForHost = originalDownload
		listPackageWorkflowFilesForHost = originalList
		listPackageDirFilesForHost = originalDirFiles
		listPackageDirSubdirsForHost = originalDirSubdirs
		getRepositoryPackageDefaultBranch = originalDefaultBranch
	})

	getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
		return "main", nil
	}
	listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}
	listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}
	listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(workflowPath)
	}
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		if content, ok := manifest[path]; ok {
			return []byte(content), nil
		}
		return nil, createRepositoryPackageNotFoundError(path)
	}
}

func TestResolveRepositoryPackage_IncludeMappings(t *testing.T) {
	t.Run("nested package installs inert sources into .github/workflows", func(t *testing.T) {
		setupMappingPackageTest(t, map[string]string{
			"factory/aw.yml": `name: Factory
includes:
  - source: payload/workflows/reviewer.md
    destination: .github/workflows/reviewer.md
    kind: agentic-workflow
  - source: payload/workflows/controller.yml
    destination: .github/workflows/controller.yml
    kind: action-workflow
`,
			"factory/README.md": "# Factory\n",
		})

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "factory"}, "")
		require.NoError(t, err)
		assert.Equal(t, []resolvedPackageInstallable{
			{SourcePath: "factory/payload/workflows/reviewer.md", DestinationPath: ".github/workflows/reviewer.md"},
			{SourcePath: "factory/payload/workflows/controller.yml", DestinationPath: ".github/workflows/controller.yml"},
		}, pkg.InstallationSource)
	})

	t.Run("mapping source is package-relative even under .github", func(t *testing.T) {
		setupMappingPackageTest(t, map[string]string{
			"factory/aw.yml": `name: Factory
includes:
  - source: .github/workflows/reviewer.md
    destination: .github/workflows/reviewer.md
`,
			"factory/README.md": "# Factory\n",
		})

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "factory"}, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"factory/.github/workflows/reviewer.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("mixes legacy string entries with mappings", func(t *testing.T) {
		setupMappingPackageTest(t, map[string]string{
			"factory/aw.yml": `name: Factory
includes:
  - workflows/review.md
  - .github/workflows/legacy.md
  - source: payload/workflows/reviewer.md
    destination: .github/workflows/reviewer.md
`,
			"factory/README.md": "# Factory\n",
		})

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "factory"}, "")
		require.NoError(t, err)
		assert.Equal(t, []resolvedPackageInstallable{
			{SourcePath: "factory/workflows/review.md", DestinationPath: ".github/workflows/review.md"},
			{SourcePath: ".github/workflows/legacy.md", DestinationPath: ".github/workflows/legacy.md"},
			{SourcePath: "factory/payload/workflows/reviewer.md", DestinationPath: ".github/workflows/reviewer.md"},
		}, pkg.InstallationSource)
	})

	t.Run("renames the installed workflow to the destination name", func(t *testing.T) {
		setupMappingPackageTest(t, map[string]string{
			"aw.yml": `name: Factory
includes:
  - source: payload/reviewer.md
    destination: .github/workflows/code-reviewer.md
`,
			"README.md": "# Factory\n",
		})

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		specs := appendRepositoryPackageWorkflowSpecs(nil, &RepoSpec{RepoSlug: "owner/repo"}, pkg)
		require.Len(t, specs, 1)
		assert.Equal(t, "payload/reviewer.md", specs[0].WorkflowPath)
		assert.Equal(t, "code-reviewer", specs[0].WorkflowName)
		assert.Equal(t, ".github/workflows/code-reviewer.md", specs[0].DestinationPath)
	})

	invalidCases := []struct {
		name     string
		includes string
		contains string
	}{
		{
			name: "rejects source path traversal",
			includes: `  - source: ../../etc/passwd.md
    destination: .github/workflows/reviewer.md`,
			contains: "path traversal outside the root is not allowed",
		},
		{
			name: "rejects absolute source",
			includes: `  - source: /etc/reviewer.md
    destination: .github/workflows/reviewer.md`,
			contains: "absolute paths are not allowed",
		},
		{
			name: "rejects protocol-relative source",
			includes: `  - source: //evil/reviewer.md
    destination: .github/workflows/reviewer.md`,
			contains: "absolute paths are not allowed",
		},
		{
			name: "rejects slash-backslash source",
			includes: `  - source: /\evil/reviewer.md
    destination: .github/workflows/reviewer.md`,
			contains: "absolute paths are not allowed",
		},
		{
			name: "rejects double-leading-backslash source",
			includes: `  - source: '\\server\share\reviewer.md'
    destination: .github/workflows/reviewer.md`,
			contains: "absolute paths are not allowed",
		},
		{
			name: "rejects destination path traversal",
			includes: `  - source: payload/reviewer.md
    destination: ../../.github/workflows/reviewer.md`,
			contains: "path traversal outside the root is not allowed",
		},
		{
			name: "rejects absolute destination",
			includes: `  - source: payload/reviewer.md
    destination: /tmp/reviewer.md`,
			contains: "absolute paths are not allowed",
		},
		{
			name: "rejects destination outside .github/workflows",
			includes: `  - source: payload/reviewer.md
    destination: .github/agents/reviewer.md`,
			contains: "destinations must be under .github/workflows/",
		},
		{
			name: "rejects nested destination",
			includes: `  - source: payload/reviewer.md
    destination: .github/workflows/nested/reviewer.md`,
			contains: "destinations must be a direct child of .github/workflows/",
		},
		{
			name: "rejects extension mismatch",
			includes: `  - source: payload/reviewer.md
    destination: .github/workflows/reviewer.yml`,
			contains: "destination file extension must match the source",
		},
		{
			name: "rejects unsupported extension",
			includes: `  - source: payload/reviewer.txt
    destination: .github/workflows/reviewer.txt`,
			contains: "only agentic workflows (.md) and action workflows (.yml) are supported",
		},
		{
			name: "rejects lock files",
			includes: `  - source: payload/reviewer.lock.yml
    destination: .github/workflows/reviewer.lock.yml`,
			contains: "compiled lock files (.lock.yml) cannot be installed",
		},
		{
			name: "rejects mismatched kind",
			includes: `  - source: payload/reviewer.md
    destination: .github/workflows/reviewer.md
    kind: action-workflow`,
			contains: "declares kind \"action-workflow\"",
		},
		{
			name: "rejects duplicate destinations",
			includes: `  - source: payload/a/controller.yml
    destination: .github/workflows/controller.yml
  - source: payload/b/other.yml
    destination: .github/workflows/controller.yml`,
			contains: "both install to \".github/workflows/controller.yml\"",
		},
	}

	t.Run("rejects duplicate markdown destinations", func(t *testing.T) {
		setupMappingPackageTest(t, map[string]string{
			"factory/aw.yml": `name: Factory
includes:
  - source: payload/a/reviewer.md
    destination: .github/workflows/reviewer.md
  - source: payload/b/other.md
    destination: .github/workflows/reviewer.md
`,
			"factory/README.md": "# Factory\n",
		})

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "factory"}, "")
		require.Error(t, err)
		assert.ErrorContains(t, err, "duplicate workflow filename")
	})

	for _, tt := range invalidCases {
		t.Run(tt.name, func(t *testing.T) {
			setupMappingPackageTest(t, map[string]string{
				"factory/aw.yml":    "name: Factory\nincludes:\n" + tt.includes + "\n",
				"factory/README.md": "# Factory\n",
			})

			_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "factory"}, "")
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.contains)
		})
	}
}

func TestManifestWorkflowPathByName_UsesDestination(t *testing.T) {
	t.Parallel()
	byName := manifestWorkflowPathByName([]resolvedPackageInstallable{
		{SourcePath: "factory/payload/workflows/reviewer.md", DestinationPath: ".github/workflows/code-reviewer.md"},
		{SourcePath: "factory/payload/workflows/controller.yml", DestinationPath: ".github/workflows/controller.yml"},
	})
	assert.Equal(t, map[string]string{
		"code-reviewer": "factory/payload/workflows/reviewer.md",
	}, byName)
}

func TestResolveLocalRepositoryPackage_IncludeMappings(t *testing.T) {
	writePackage := func(t *testing.T, manifest string) string {
		t.Helper()
		packageDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(packageDir, "aw.yml"), []byte(manifest), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(packageDir, "README.md"), []byte("# Factory\n"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(packageDir, "payload", "workflows"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(packageDir, "payload", "workflows", "reviewer.md"), []byte("# Reviewer\n"), 0o600))
		return packageDir
	}

	t.Run("resolves mapping entries", func(t *testing.T) {
		packageDir := writePackage(t, `name: Factory
includes:
  - source: payload/workflows/reviewer.md
    destination: .github/workflows/reviewer.md
`)
		pkg, err := resolveLocalRepositoryPackage(packageDir)
		require.NoError(t, err)
		require.Len(t, pkg.InstallationSource, 1)
		assert.Equal(t, filepath.Join(packageDir, "payload", "workflows", "reviewer.md"), pkg.InstallationSource[0].SourcePath)
		assert.Equal(t, ".github/workflows/reviewer.md", pkg.InstallationSource[0].DestinationPath)
	})

	t.Run("rejects symlinked mapping sources", func(t *testing.T) {
		packageDir := writePackage(t, `name: Factory
includes:
  - source: payload/workflows/linked.md
    destination: .github/workflows/linked.md
`)
		target := filepath.Join(packageDir, "payload", "workflows", "reviewer.md")
		link := filepath.Join(packageDir, "payload", "workflows", "linked.md")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks are not supported on this platform: %v", err)
		}

		_, err := resolveLocalRepositoryPackage(packageDir)
		require.Error(t, err)
		assert.ErrorContains(t, err, "is a symbolic link")
	})

	t.Run("rejects missing mapping sources", func(t *testing.T) {
		packageDir := writePackage(t, `name: Factory
includes:
  - source: payload/workflows/missing.md
    destination: .github/workflows/missing.md
`)
		_, err := resolveLocalRepositoryPackage(packageDir)
		require.Error(t, err)
		assert.ErrorContains(t, err, "missing.md")
	})
}

// TestAddWorkflowsWithTracking_InstallsMappedDestinations verifies that both agentic
// markdown and deterministic action workflows declared through source-to-destination
// mappings are installed under their declared destination file names. This is the shared
// installation path used by both `gh aw add` and `gh aw add-wizard`.
func TestAddWorkflowsWithTracking_InstallsMappedDestinations(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-add-mapped-destination-*")
	workflowsDir := setupMinimalGitRepo(t, tempDir)

	markdownEntry := resolvedPackageInstallable{
		SourcePath:      "factory/payload/workflows/reviewer.md",
		DestinationPath: ".github/workflows/code-reviewer.md",
	}
	actionEntry := resolvedPackageInstallable{
		SourcePath:      "factory/payload/workflows/controller.yml",
		DestinationPath: ".github/workflows/deterministic-controller.yml",
	}

	workflows := []*ResolvedWorkflow{
		{
			Spec: &WorkflowSpec{
				WorkflowPath:           markdownEntry.SourcePath,
				WorkflowName:           packageInstallableWorkflowName(markdownEntry),
				DestinationPath:        markdownEntry.DestinationPath,
				FromRepositoryManifest: true,
			},
			Content:    []byte("---\non: workflow_dispatch\nengine: claude\npermissions: read-all\n---\n\n# Reviewer\n"),
			SourceInfo: &FetchedWorkflow{IsLocal: true},
		},
		{
			Spec: &WorkflowSpec{
				WorkflowPath:           actionEntry.SourcePath,
				WorkflowName:           packageInstallableWorkflowName(actionEntry),
				DestinationPath:        actionEntry.DestinationPath,
				FromRepositoryManifest: true,
			},
			Content:          []byte("name: Controller\non: workflow_dispatch\njobs: {}\n"),
			IsActionWorkflow: true,
			SourceInfo:       &FetchedWorkflow{IsLocal: true},
		},
	}

	err := addWorkflowsWithTracking(context.Background(), workflows, NewFileTracker(), AddOptions{
		NoGitattributes:        true,
		DisableSecurityScanner: true,
		Quiet:                  true,
	})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(workflowsDir, "code-reviewer.md"))
	assert.FileExists(t, filepath.Join(workflowsDir, "deterministic-controller.yml"))
	assert.NoFileExists(t, filepath.Join(workflowsDir, "reviewer.md"))
	assert.NoFileExists(t, filepath.Join(workflowsDir, "controller.yml"))

	// Deterministic action workflows are copied verbatim.
	written, err := os.ReadFile(filepath.Join(workflowsDir, "deterministic-controller.yml"))
	require.NoError(t, err)
	assert.Equal(t, "name: Controller\non: workflow_dispatch\njobs: {}\n", string(written))
}
