//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractManifestIncludesWithWildcard(t *testing.T) {
	t.Parallel()
	includes, warnings, err := extractManifestIncludes([]any{
		"workflows/*",
		".github/workflows/*",
		"workflows/**",
		"workflows/review*.md",
	}, "aw.yml")
	require.NoError(t, err)
	assert.Equal(t, []string{"workflows/*", ".github/workflows/*"}, manifestIncludeSources(includes))
	require.Len(t, warnings, 2)
	assert.Contains(t, warnings[0], "workflows/**")
	assert.Contains(t, warnings[1], "workflows/review*.md")
}

func TestExpandManifestWildcardMatches(t *testing.T) {
	t.Parallel()
	matches, err := expandManifestWildcardMatches(repositoryPackageInclude{Source: "workflows/*"}, "workflows", []string{
		"workflows/z.md",
		"workflows/nested/ignored.md",
		"workflows/README.txt",
		"workflows/a.md",
	}, isSupportedPackageInstallablePath)
	require.NoError(t, err)
	assert.Equal(t, []string{"workflows/a.md", "workflows/z.md"}, manifestIncludeSources(matches))
}

func TestExtractManifestIncludesWithWildcardMapping(t *testing.T) {
	t.Parallel()
	includes, warnings, err := extractManifestIncludes([]any{
		map[string]any{
			"source":      "payload/workflows/*",
			"destination": ".github/workflows/",
		},
	}, "aw.yml")
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, []repositoryPackageInclude{{
		Source:      "payload/workflows/*",
		Destination: ".github/workflows",
	}}, includes)
}

func TestExtractManifestIncludesRejectsWildcardMappingToFile(t *testing.T) {
	t.Parallel()
	_, _, err := extractManifestIncludes([]any{
		map[string]any{
			"source":      "payload/workflows/*",
			"destination": ".github/workflows/reviewer.md",
		},
	}, "aw.yml")
	require.Error(t, err)
	assert.ErrorContains(t, err, "wildcard destinations must be the .github/workflows folder")
}

func TestExpandRepositoryPackageWildcardIncludes(t *testing.T) {
	originalFiles := listPackageDirFilesForHost
	originalSubdirs := listPackageDirSubdirsForHost
	t.Cleanup(func() {
		listPackageDirFilesForHost = originalFiles
		listPackageDirSubdirsForHost = originalSubdirs
	})

	listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		switch dirPath {
		case "bundle/workflows":
			return []string{
				"bundle/workflows/z.md",
				"bundle/workflows/nested/ignored.md",
				"bundle/workflows/README.txt",
				"bundle/workflows/a.md",
			}, nil
		case ".github/workflows":
			return []string{
				".github/workflows/ci.yml",
				".github/workflows/ci.lock.yml",
			}, nil
		case "bundle/agents":
			return []string{"bundle/agents/reviewer.md"}, nil
		case "bundle/skills":
			return []string{"bundle/skills/README.md"}, nil
		default:
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
	}
	listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		if dirPath == "bundle/skills" {
			return []string{"bundle/skills/review"}, nil
		}
		return nil, nil
	}

	expanded, err := expandRepositoryPackageWildcardIncludes(
		t.Context(),
		"owner",
		"repo",
		"bundle",
		"main",
		"",
		[]repositoryPackageInclude{
			{Source: "workflows/a.md"},
			{Source: "workflows/*"},
			{Source: ".github/workflows/*"},
			{Source: "agents/*"},
			{Source: "skills/*"},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"workflows/a.md",
		"workflows/z.md",
		".github/workflows/ci.yml",
		"agents/reviewer.md",
		"skills/review",
	}, manifestIncludeSources(expanded))
}

func TestExpandRepositoryPackageWildcardMapping(t *testing.T) {
	originalFiles := listPackageDirFilesForHost
	originalSubdirs := listPackageDirSubdirsForHost
	t.Cleanup(func() {
		listPackageDirFilesForHost = originalFiles
		listPackageDirSubdirsForHost = originalSubdirs
	})

	listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		require.Equal(t, "bundle/payload/workflows", dirPath)
		return []string{
			"bundle/payload/workflows/reviewer.md",
			"bundle/payload/workflows/controller.yml",
			"bundle/payload/workflows/controller.lock.yml",
			"bundle/payload/workflows/README.txt",
		}, nil
	}
	listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, nil
	}

	expanded, err := expandRepositoryPackageWildcardIncludes(
		t.Context(),
		"owner",
		"repo",
		"bundle",
		"main",
		"",
		[]repositoryPackageInclude{{
			Source:      "payload/workflows/*",
			Destination: ".github/workflows",
		}},
	)
	require.NoError(t, err)
	assert.Equal(t, []repositoryPackageInclude{
		{
			Source:      "payload/workflows/controller.yml",
			Destination: ".github/workflows/controller.yml",
			Kind:        manifestIncludeKindActionWorkflow,
		},
		{
			Source:      "payload/workflows/reviewer.md",
			Destination: ".github/workflows/reviewer.md",
			Kind:        manifestIncludeKindAgenticWorkflow,
		},
	}, expanded)
}

func TestExpandRepositoryPackageWildcardMappingIsPackageRelative(t *testing.T) {
	originalFiles := listPackageDirFilesForHost
	originalSubdirs := listPackageDirSubdirsForHost
	t.Cleanup(func() {
		listPackageDirFilesForHost = originalFiles
		listPackageDirSubdirsForHost = originalSubdirs
	})

	listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		require.Equal(t, "bundle/.github/workflows", dirPath)
		return []string{"bundle/.github/workflows/reviewer.md"}, nil
	}
	listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		t.Fatal("wildcard mappings must not enumerate subdirectories")
		return nil, nil
	}

	expanded, err := expandRepositoryPackageWildcardIncludes(
		t.Context(),
		"owner",
		"repo",
		"bundle",
		"main",
		"",
		[]repositoryPackageInclude{{
			Source:      ".github/workflows/*",
			Destination: ".github/workflows",
		}},
	)
	require.NoError(t, err)
	assert.Equal(t, []repositoryPackageInclude{{
		Source:      ".github/workflows/reviewer.md",
		Destination: ".github/workflows/reviewer.md",
		Kind:        manifestIncludeKindAgenticWorkflow,
	}}, expanded)
}

func TestResolveLocalRepositoryPackageWildcardIncludes(t *testing.T) {
	packageDir := t.TempDir()
	writePackageTestFile(t, packageDir, "README.md", "# Wildcard package\n")
	writePackageTestFile(t, packageDir, "aw.yml", `name: Wildcard package
includes:
  - workflows/*
  - .github/workflows/*
`)
	writePackageTestFile(t, packageDir, "workflows/z.md", "# Z\n")
	writePackageTestFile(t, packageDir, "workflows/a.md", "# A\n")
	writePackageTestFile(t, packageDir, "workflows/README.txt", "ignored\n")
	writePackageTestFile(t, packageDir, "workflows/nested/ignored.md", "# Ignored\n")
	writePackageTestFile(t, packageDir, ".github/workflows/ci.yml", "name: CI\n")
	writePackageTestFile(t, packageDir, ".github/workflows/ci.lock.yml", "name: Ignored\n")
	outsideWorkflow := filepath.Join(t.TempDir(), "outside.md")
	require.NoError(t, os.WriteFile(outsideWorkflow, []byte("# Outside\n"), 0o644))
	if err := os.Symlink(outsideWorkflow, filepath.Join(packageDir, "workflows", "linked.md")); err != nil {
		t.Logf("symlinks unavailable; skipping symlink fixture: %v", err)
	}

	pkg, err := resolveLocalRepositoryPackage(packageDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, []string{
		filepath.Join(packageDir, "workflows", "a.md"),
		filepath.Join(packageDir, "workflows", "z.md"),
		filepath.Join(packageDir, ".github", "workflows", "ci.yml"),
	}, packageInstallableSourcePaths(pkg.InstallationSource))
}

func TestResolveLocalRepositoryPackageWildcardMapping(t *testing.T) {
	packageDir := t.TempDir()
	writePackageTestFile(t, packageDir, "README.md", "# Wildcard mapping package\n")
	writePackageTestFile(t, packageDir, "aw.yml", `name: Wildcard mapping package
includes:
  - source: payload/workflows/*
    destination: .github/workflows/
`)
	writePackageTestFile(t, packageDir, "payload/workflows/reviewer.md", "# Reviewer\n")
	writePackageTestFile(t, packageDir, "payload/workflows/controller.yml", "name: Controller\n")
	writePackageTestFile(t, packageDir, "payload/workflows/controller.lock.yml", "name: Ignored\n")
	writePackageTestFile(t, packageDir, "payload/workflows/README.txt", "ignored\n")

	pkg, err := resolveLocalRepositoryPackage(packageDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, []resolvedPackageInstallable{
		{
			SourcePath:      filepath.Join(packageDir, "payload", "workflows", "controller.yml"),
			DestinationPath: ".github/workflows/controller.yml",
		},
		{
			SourcePath:      filepath.Join(packageDir, "payload", "workflows", "reviewer.md"),
			DestinationPath: ".github/workflows/reviewer.md",
		},
	}, pkg.InstallationSource)
}

func TestExpandLocalPackageWildcardMappingIsPackageRelative(t *testing.T) {
	packageRoot := t.TempDir()
	packageDir := filepath.Join(packageRoot, "bundle")
	writePackageTestFile(t, packageDir, ".github/workflows/package.md", "# Package\n")
	writePackageTestFile(t, packageRoot, ".github/workflows/root.md", "# Root\n")

	expanded, err := expandLocalPackageWildcardIncludes([]repositoryPackageInclude{{
		Source:      ".github/workflows/*",
		Destination: ".github/workflows",
	}}, packageDir, packageRoot)
	require.NoError(t, err)
	assert.Equal(t, []repositoryPackageInclude{{
		Source:      ".github/workflows/package.md",
		Destination: ".github/workflows/package.md",
		Kind:        manifestIncludeKindAgenticWorkflow,
	}}, expanded)
}
