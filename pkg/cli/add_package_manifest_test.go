//go:build !integration

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createRepositoryPackageNotFoundError(path string) error {
	return normalizeRepositoryPackageRemoteError(fmt.Errorf("404 not found: %s", path))
}

func TestRepositoryPackageVisibility(t *testing.T) {
	t.Run("defaults both fields to false", func(t *testing.T) {
		manifest, warnings, err := parseRepositoryPackageManifest("aw.yml", []byte("name: Public Package\n"))
		require.NoError(t, err)
		assert.False(t, manifest.Private)
		assert.False(t, manifest.Experimental)
		assert.Empty(t, warnings)
	})

	t.Run("parses explicit boolean fields", func(t *testing.T) {
		manifest, _, err := parseRepositoryPackageManifest("aw.yml", []byte(`name: Preview Package
private: true
experimental: true
`))
		require.NoError(t, err)
		assert.True(t, manifest.Private)
		assert.True(t, manifest.Experimental)
	})

	t.Run("rejects non-boolean fields", func(t *testing.T) {
		_, _, err := parseRepositoryPackageManifest("aw.yml", []byte(`name: Invalid Package
private: "true"
`))
		require.ErrorContains(t, err, "private")
		require.ErrorContains(t, err, "boolean")
	})

	t.Run("refuses private packages", func(t *testing.T) {
		warnings, err := validateRepositoryPackageVisibility(&repositoryPackageManifest{Private: true}, "owner/private-package")
		require.ErrorContains(t, err, `package "owner/private-package" is private and cannot be added`)
		assert.Empty(t, warnings)
	})

	t.Run("warns for experimental packages", func(t *testing.T) {
		warnings, err := validateRepositoryPackageVisibility(&repositoryPackageManifest{Experimental: true}, "owner/preview-package")
		require.NoError(t, err)
		assert.Equal(t, []string{`Package "owner/preview-package" is experimental and may change without notice.`}, warnings)
	})
}

func TestRepositoryPackageIcon(t *testing.T) {
	t.Run("omitted icon field", func(t *testing.T) {
		manifest, warnings, err := parseRepositoryPackageManifest("aw.yml", []byte("name: Public Package\n"))
		require.NoError(t, err)
		assert.Empty(t, manifest.Icon)
		assert.Empty(t, warnings)
	})

	t.Run("valid emoji icon", func(t *testing.T) {
		manifest, _, err := parseRepositoryPackageManifest("aw.yml", []byte("name: Emoji Package\nicon: \"🚀\"\n"))
		require.NoError(t, err)
		assert.Equal(t, "🚀", manifest.Icon)
	})
	t.Run("rejects invalid emoji sequences", func(t *testing.T) {
		for _, icon := range []string{"\u200d", "\ufe0f", "🚀123", "123"} {
			_, _, err := parseRepositoryPackageManifest("aw.yml", []byte("name: Invalid Package\nicon: \""+icon+"\"\n"))
			require.Error(t, err, icon)
		}
	})
	t.Run("trims icon value", func(t *testing.T) {
		manifest, _, err := parseRepositoryPackageManifest("aw.yml", []byte("name: Emoji Package\nicon: \" 🚀 \"\n"))
		require.NoError(t, err)
		assert.Equal(t, "🚀", manifest.Icon)
	})

	t.Run("valid primer octicon syntax", func(t *testing.T) {
		manifest, _, err := parseRepositoryPackageManifest("aw.yml", []byte("name: Octicon Package\nicon: \":check-circle:\"\n"))
		require.NoError(t, err)
		assert.Equal(t, ":check-circle:", manifest.Icon)

		manifest, _, err = parseRepositoryPackageManifest("aw.yml", []byte("name: Octicon Package\nicon: \":git-pull-request:\"\n"))
		require.NoError(t, err)
		assert.Equal(t, ":git-pull-request:", manifest.Icon)
	})

	t.Run("invalid octicon syntax", func(t *testing.T) {
		_, _, err := parseRepositoryPackageManifest("aw.yml", []byte("name: Invalid Package\nicon: \"::\"\n"))
		require.ErrorContains(t, err, "icon octicon name must use :name: syntax")

		_, _, err = parseRepositoryPackageManifest("aw.yml", []byte("name: Invalid Package\nicon: \":invalid name:\"\n"))
		require.ErrorContains(t, err, "icon octicon name must use :name: syntax")
	})

	t.Run("valid SVG resource file matching source", func(t *testing.T) {
		yamlContent := `name: Resource Package
resources:
  - source: assets/logo.svg
    destination: .github/aw/logo.svg
icon: assets/logo.svg
`
		manifest, _, err := parseRepositoryPackageManifest("aw.yml", []byte(yamlContent))
		require.NoError(t, err)
		assert.Equal(t, "assets/logo.svg", manifest.Icon)
	})

	t.Run("valid SVG resource file matching destination", func(t *testing.T) {
		yamlContent := `name: Resource Package
resources:
  - source: assets/logo.svg
    destination: .github/aw/logo.svg
icon: .github/aw/logo.svg
`
		manifest, _, err := parseRepositoryPackageManifest("aw.yml", []byte(yamlContent))
		require.NoError(t, err)
		assert.Equal(t, ".github/aw/logo.svg", manifest.Icon)
	})

	t.Run("non-SVG resource file", func(t *testing.T) {
		yamlContent := `name: Non SVG Package
resources:
  - source: assets/logo.png
    destination: .github/aw/logo.png
icon: assets/logo.png
`
		_, _, err := parseRepositoryPackageManifest("aw.yml", []byte(yamlContent))
		require.ErrorContains(t, err, `icon file "assets/logo.png" in package resources must be an SVG file (.svg)`)
	})

	t.Run("SVG icon file not declared in package resources", func(t *testing.T) {
		yamlContent := `name: Undeclared Package
icon: assets/missing.svg
`
		_, _, err := parseRepositoryPackageManifest("aw.yml", []byte(yamlContent))
		require.ErrorContains(t, err, `icon file "assets/missing.svg" must be declared in package resources`)
	})

	t.Run("invalid plain text icon", func(t *testing.T) {
		yamlContent := `name: Invalid Icon Package
icon: plain_text_icon
`
		_, _, err := parseRepositoryPackageManifest("aw.yml", []byte(yamlContent))
		require.ErrorContains(t, err, `icon "plain_text_icon" is invalid: must be an emoji, a GitHub primer octicon name (e.g. :check-circle:), or an SVG file declared in package resources`)
	})

	t.Run("empty icon string", func(t *testing.T) {
		yamlContent := `name: Empty Icon Package
icon: ""
`
		_, _, err := parseRepositoryPackageManifest("aw.yml", []byte(yamlContent))
		require.ErrorContains(t, err, `icon must be a non-empty string`)
	})

	t.Run("non-string icon value", func(t *testing.T) {
		yamlContent := `name: Non String Icon Package
icon: 123
`
		_, _, err := parseRepositoryPackageManifest("aw.yml", []byte(yamlContent))
		require.Error(t, err)
	})
}

func TestResolveRepositoryPackage(t *testing.T) {
	originalVersion := GetVersion()
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	originalLatestRelease := getRepositoryPackageLatestRelease
	t.Cleanup(func() {
		SetVersionInfo(originalVersion)
		downloadPackageFileFromGitHubForHost = originalDownload
		listPackageWorkflowFilesForHost = originalList
		listPackageDirFilesForHost = originalDirFiles
		listPackageDirSubdirsForHost = originalDirSubdirs
		getRepositoryPackageDefaultBranch = originalDefaultBranch
		getRepositoryPackageLatestRelease = originalLatestRelease
	})
	SetVersionInfo("v1.2.3")
	getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
		return "main", nil
	}

	getRepositoryPackageLatestRelease = func(_ context.Context, repoSlug, host string) (string, error) {
		return "", errors.New("no releases found")
	}
	listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}
	listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}

	t.Run("refuses private packages", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte("name: Private Package\nprivate: true\n"), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/private-package"}, "")
		require.ErrorContains(t, err, `package "owner/private-package" is private and cannot be added`)
	})

	t.Run("warns for experimental packages", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte("name: Preview Package\nexperimental: true\nincludes:\n  - workflows/review.md\n"), nil
			case "README.md":
				return []byte("# Preview Package\n"), nil
			case "workflows/review.md":
				return []byte("---\non: issues\n---\n# Review\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/preview-package"}, "")
		require.NoError(t, err)
		assert.False(t, pkg.Private)
		assert.True(t, pkg.Experimental)
		assert.Contains(t, pkg.Warnings, `Package "owner/preview-package" is experimental and may change without notice.`)
	})

	t.Run("uses aw manifest files and README docs", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`name: Repo Assist
emoji: 🤖
description: Friendly repository automation
license: MIT
files:
  - workflows/review.md
  - .github/workflows/nightly-review.md
  - README.md
`), nil
			case "README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Equal(t, "aw.yml", pkg.ManifestPath)
		assert.Equal(t, "Repo Assist", pkg.Name)
		assert.Equal(t, "🤖", pkg.Emoji)
		assert.Equal(t, "MIT", pkg.License)
		assert.Equal(t, "README.md", pkg.DocsPath)
		assert.Equal(t, []string{"workflows/review.md", ".github/workflows/nightly-review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
		require.NotEmpty(t, pkg.Warnings)
		assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "Ignoring files entry")
	})

	t.Run("uses repository default branch when version is omitted", func(t *testing.T) {
		previousDefaultBranch := getRepositoryPackageDefaultBranch
		previousLatestRelease := getRepositoryPackageLatestRelease
		t.Cleanup(func() {
			getRepositoryPackageDefaultBranch = previousDefaultBranch
			getRepositoryPackageLatestRelease = previousLatestRelease
		})

		t.Run("uses resources mappings", func(t *testing.T) {
			downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
				switch path {
				case "packages/repo-assist/aw.yml":
					return []byte(`name: Repo Assist
resources:
  - source: templates/bug.yml
    destination: .github/ISSUE_TEMPLATE/bug.yml
  - source: policy/controls.json
    destination: .github/aw/policy/controls.json
`), nil
				case "packages/repo-assist/README.md":
					return []byte("# Repo Assist\n"), nil
				default:
					return nil, createRepositoryPackageNotFoundError(path)
				}
			}
			listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
				return nil, createRepositoryPackageNotFoundError(workflowPath)
			}

			pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "packages/repo-assist"}, "")
			require.NoError(t, err)
			require.Len(t, pkg.ResourceFiles, 2)
			assert.Equal(t, "packages/repo-assist/templates/bug.yml", pkg.ResourceFiles[0].SourcePath)
			assert.Equal(t, ".github/ISSUE_TEMPLATE/bug.yml", pkg.ResourceFiles[0].DestinationPath)
			assert.Equal(t, "packages/repo-assist/policy/controls.json", pkg.ResourceFiles[1].SourcePath)
			assert.Equal(t, ".github/aw/policy/controls.json", pkg.ResourceFiles[1].DestinationPath)
		})

		t.Run("adds grader evaluators to package resources", func(t *testing.T) {
			downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
				switch path {
				case "packages/repo-assist/aw.yml":
					return []byte(`name: Repo Assist
files:
  - workflows/review.md
  - workflows/triage.md
`), nil
				case "packages/repo-assist/README.md":
					return []byte("# Repo Assist\n"), nil
				case "packages/repo-assist/workflows/review.md":
					return []byte(`---
on: pull_request
graders:
  operational-value:
    run: .github/graders/shared-operational-value.sh
---
# Review
`), nil
				case "packages/repo-assist/workflows/triage.md":
					return []byte(`---
on: issues
graders:
  operational-value:
    run: ./graders/triage-operational-value.sh
---
# Triage
`), nil
				default:
					return nil, createRepositoryPackageNotFoundError(path)
				}
			}
			listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
				t.Fatalf("unexpected scan of %s", workflowPath)
				return nil, nil
			}

			pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "packages/repo-assist"}, "")
			require.NoError(t, err)
			require.Len(t, pkg.ResourceFiles, 2)
			assert.Equal(t, ".github/graders/shared-operational-value.sh", pkg.ResourceFiles[0].SourcePath)
			assert.Equal(t, ".github/graders/shared-operational-value.sh", pkg.ResourceFiles[0].DestinationPath)
			assert.Equal(t, "packages/repo-assist/workflows/graders/triage-operational-value.sh", pkg.ResourceFiles[1].SourcePath)
			assert.Equal(t, ".github/workflows/graders/triage-operational-value.sh", pkg.ResourceFiles[1].DestinationPath)
			assert.True(t, isPackageResourceDestination(pkg.ResourceFiles[1].DestinationPath))
		})

		t.Run("dedupes a repository-root evaluator shared across imported manifests", func(t *testing.T) {
			downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
				switch path {
				case "packages/repo-assist/aw.yml":
					return []byte(`name: Repo Assist
includes:
  - modules/a/aw.yml
  - modules/b/aw.yml
`), nil
				case "packages/repo-assist/README.md":
					return []byte("# Repo Assist\n"), nil
				case "packages/repo-assist/modules/a/aw.yml":
					return []byte(`name: Module A
files:
  - workflows/a.md
`), nil
				case "packages/repo-assist/modules/b/aw.yml":
					return []byte(`name: Module B
files:
  - workflows/b.md
`), nil
				case "packages/repo-assist/modules/a/workflows/a.md":
					return []byte(`---
on: pull_request
graders:
  operational-value:
    run: .github/graders/shared-operational-value.sh
---
# A
`), nil
				case "packages/repo-assist/modules/b/workflows/b.md":
					return []byte(`---
on: issues
graders:
  operational-value:
    run: .github/graders/shared-operational-value.sh
---
# B
`), nil
				default:
					return nil, createRepositoryPackageNotFoundError(path)
				}
			}
			listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
				t.Fatalf("unexpected scan of %s", workflowPath)
				return nil, nil
			}

			pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "packages/repo-assist"}, "")
			require.NoError(t, err)
			require.Len(t, pkg.ResourceFiles, 1)
			assert.Equal(t, ".github/graders/shared-operational-value.sh", pkg.ResourceFiles[0].SourcePath)
			assert.Equal(t, ".github/graders/shared-operational-value.sh", pkg.ResourceFiles[0].DestinationPath)
		})
		getRepositoryPackageLatestRelease = func(_ context.Context, repoSlug, host string) (string, error) {
			assert.Equal(t, "owner/repo", repoSlug)
			assert.Equal(t, "github.com", host)
			return "", errors.New("no releases found")
		}
		getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
			assert.Equal(t, "owner/repo", repoSlug)
			assert.Equal(t, "github.com", host)
			return "master", nil
		}
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			assert.Equal(t, "master", ref)
			switch path {
			case "aw.yml":
				return []byte("name: Repo Assist\n"), nil
			case "README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			assert.Equal(t, "master", ref)
			switch workflowPath {
			case "workflows":
				return []string{"workflows/review.md"}, nil
			case ".github/workflows":
				return nil, createRepositoryPackageNotFoundError(workflowPath)
			default:
				return nil, fmt.Errorf("unexpected workflow path %s", workflowPath)
			}
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "github.com")
		require.NoError(t, err)
		assert.Equal(t, []string{"workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("uses latest release for github/gh-aw when version is omitted", func(t *testing.T) {
		previousDefaultBranch := getRepositoryPackageDefaultBranch
		previousLatestRelease := getRepositoryPackageLatestRelease
		t.Cleanup(func() {
			getRepositoryPackageDefaultBranch = previousDefaultBranch
			getRepositoryPackageLatestRelease = previousLatestRelease
		})
		getRepositoryPackageLatestRelease = func(_ context.Context, repoSlug, host string) (string, error) {
			assert.Equal(t, "github/gh-aw", repoSlug)
			assert.Equal(t, "github.com", host)
			return "v1.2.3", nil
		}
		getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
			t.Fatalf("default branch lookup should not be called when latest release is available")
			return "", nil
		}
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			assert.Equal(t, "v1.2.3", ref)
			switch path {
			case "aw.yml":
				return []byte("name: gh-aw package\n"), nil
			case "README.md":
				return []byte("# gh-aw package\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			assert.Equal(t, "v1.2.3", ref)
			switch workflowPath {
			case "workflows":
				return []string{"workflows/review.md"}, nil
			case ".github/workflows":
				return nil, createRepositoryPackageNotFoundError(workflowPath)
			default:
				return nil, fmt.Errorf("unexpected workflow path %s", workflowPath)
			}
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "github/gh-aw"}, "github.com")
		require.NoError(t, err)
		assert.Equal(t, "v1.2.3", pkg.ResolvedRef)
		assert.Equal(t, []string{"workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("falls back to default branch for github/gh-aw when latest release lookup fails", func(t *testing.T) {
		previousDefaultBranch := getRepositoryPackageDefaultBranch
		previousLatestRelease := getRepositoryPackageLatestRelease
		t.Cleanup(func() {
			getRepositoryPackageDefaultBranch = previousDefaultBranch
			getRepositoryPackageLatestRelease = previousLatestRelease
		})
		getRepositoryPackageLatestRelease = func(_ context.Context, repoSlug, host string) (string, error) {
			assert.Equal(t, "github/gh-aw", repoSlug)
			assert.Equal(t, "github.com", host)
			return "", errors.New("release lookup failed")
		}
		getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
			assert.Equal(t, "github/gh-aw", repoSlug)
			assert.Equal(t, "github.com", host)
			return "main", nil
		}
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			assert.Equal(t, "main", ref)
			switch path {
			case "aw.yml":
				return []byte("name: gh-aw package\n"), nil
			case "README.md":
				return []byte("# gh-aw package\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			assert.Equal(t, "main", ref)
			switch workflowPath {
			case "workflows":
				return []string{"workflows/review.md"}, nil
			case ".github/workflows":
				return nil, createRepositoryPackageNotFoundError(workflowPath)
			default:
				return nil, fmt.Errorf("unexpected workflow path %s", workflowPath)
			}
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "github/gh-aw"}, "github.com")
		require.NoError(t, err)
		assert.Equal(t, "main", pkg.ResolvedRef)
		assert.Equal(t, []string{"workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("uses slash branch ref from manifest route", func(t *testing.T) {
		previousDefaultBranch := getRepositoryPackageDefaultBranch
		t.Cleanup(func() {
			getRepositoryPackageDefaultBranch = previousDefaultBranch
		})
		getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
			t.Fatalf("default branch lookup should not be called when version is provided")
			return "", nil
		}
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			assert.Equal(t, "feature/github-agentic-workflow", ref)
			switch path {
			case "agentic-workflows/aw.yml":
				return []byte("name: Repo Assist\nfiles:\n  - workflows/review.md\n"), nil
			case "agentic-workflows/README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{
			RepoSlug:    "owner/repo",
			PackagePath: "agentic-workflows",
			Version:     "feature/github-agentic-workflow",
		}, "github.com")
		require.NoError(t, err)
		assert.Equal(t, []string{"agentic-workflows/workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("falls back to scanning supported workflow directories", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte("name: Repo Assist\n"), nil
			case "README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			assert.Empty(t, host)
			switch workflowPath {
			case "workflows":
				return []string{"workflows/review.md"}, nil
			case ".github/workflows":
				return []string{".github/workflows/nightly-review.md"}, nil
			default:
				return nil, fmt.Errorf("unexpected workflow path %s", workflowPath)
			}
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Equal(t, "README.md", pkg.DocsPath)
		assert.Equal(t, []string{"workflows/review.md", ".github/workflows/nightly-review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("passes explicit host to scanning fallback", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				assert.Equal(t, "github.com", host)
				return []byte("name: Repo Assist\n"), nil
			case "README.md":
				assert.Equal(t, "github.com", host)
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			assert.Equal(t, "github.com", host)
			switch workflowPath {
			case "workflows":
				return []string{"workflows/review.md"}, nil
			case ".github/workflows":
				return nil, createRepositoryPackageNotFoundError(workflowPath)
			default:
				return nil, fmt.Errorf("unexpected workflow path %s", workflowPath)
			}
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "github.com")
		require.NoError(t, err)
		assert.Equal(t, []string{"workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("rejects manifest without name field", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte("description: missing name\n"), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, `name must be a non-empty string`)
	})

	t.Run("requires aw manifest when only legacy alias exists", func(t *testing.T) {
		var requestedPaths []string
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			requestedPaths = append(requestedPaths, path)
			if path == "agents.yml" {
				return []byte("name: Legacy Alias\n"), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		assert.Equal(t, []string{"aw.yml"}, requestedPaths)
		require.ErrorContains(t, err, `no aw.yml manifest found`)
	})

	t.Run("accepts manifest-version and compatible min-version", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`manifest-version: "1"
min-version: v1.0.0
name: Repo Assist
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("accepts git describe compiler version for matching min-version", func(t *testing.T) {
		SetVersionInfo("v1.0.0-27-g117acb7f9c")
		t.Cleanup(func() {
			SetVersionInfo("v1.2.3")
		})

		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`manifest-version: "1"
min-version: v1.0.0
name: Repo Assist
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("accepts manifest without manifest-version", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`name: Repo Assist
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("rejects unsupported manifest-version", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte(`manifest-version: "2"
name: Repo Assist
`), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, `manifest-version`)
	})

	t.Run("accepts branding field", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`name: Repo Assist
branding:
  icon: zap
  color: blue
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Equal(t, "Repo Assist", pkg.Name)
	})

	t.Run("accepts bootstrap action metadata", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`name: Repo Assist
config:
  - type: require-owner-type
    owner: repo
    value: org
  - type: repo-variable
    name: CENTRAL_AGENTIC_OPS_MODE
    prompt: Rollout mode
    default: preview
    enum: [preview, review, live]
  - type: handoff
    message: Run gh aw run readiness.
`), nil
			case "README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			switch workflowPath {
			case "workflows":
				return []string{"workflows/review.md"}, nil
			case ".github/workflows":
				return nil, createRepositoryPackageNotFoundError(workflowPath)
			default:
				return nil, fmt.Errorf("unexpected workflow path %s", workflowPath)
			}
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		require.NotNil(t, pkg.Bootstrap)
		require.Len(t, pkg.Bootstrap.Config, 3)
		assert.Equal(t, "require-owner-type", pkg.Bootstrap.Config[0].Type)
		assert.Equal(t, "repo-variable", pkg.Bootstrap.Config[1].Type)
		assert.Equal(t, []string{"preview", "review", "live"}, pkg.Bootstrap.Config[1].Enum)
		assert.Equal(t, "handoff", pkg.Bootstrap.Config[2].Type)
		assert.Contains(t, pkg.Warnings, "Using experimental feature: config")
	})

	t.Run("rejects old bootstrap key with schema error", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`name: Repo Assist
bootstrap:
  config:
    - type: repo-variable
      name: MY_VAR
      prompt: Enter a value
`), nil
			case "README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err, "old bootstrap key must produce an error, not be silently ignored")
		require.ErrorContains(t, err, "bootstrap")
	})

	t.Run("rejects unsupported branding icon", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte(`name: Repo Assist
branding:
  icon: not-a-feather-icon
  color: blue
`), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, `icon`)
	})

	t.Run("rejects docs field", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte(`name: Repo Assist
docs: docs/overview.md
`), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, `docs`)
	})

	t.Run("rejects non-string emoji field", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte(`name: Repo Assist
emoji:
  icon: 🤖
`), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, `emoji`)
	})

	t.Run("rejects non-string license field", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte(`name: Repo Assist
license:
  id: MIT
`), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, `license`)
	})

	t.Run("rejects incompatible min-version", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte(`min-version: v9.9.9
name: Repo Assist
`), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, `requires gh-aw`)
	})

	t.Run("requires package README", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte(`name: Repo Assist
files:
  - workflows/review.md
`), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.EqualError(t, err, "repository \"owner/repo\" is not a valid Agentic Workflow package: missing required README.md at \"README.md\". Add a README.md describing the package. Example:\n# My Package\n\nDescribe what this package does")
	})

	t.Run("reports nested package path when README is missing", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "packages/repo-assist/aw.yml" {
				return []byte(`name: Repo Assist
files:
  - workflows/review.md
`), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "packages/repo-assist"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, `owner/repo/packages/repo-assist`)
		require.ErrorContains(t, err, `packages/repo-assist/README.md`)
	})

	t.Run("rejects unknown manifest fields", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			if path == "aw.yml" {
				return []byte(`name: Repo Assist
unknown-field: true
`), nil
			}
			return nil, createRepositoryPackageNotFoundError(path)
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, `unknown-field`)
	})

	t.Run("resolves nested package manifests", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "packages/repo-assist/aw.yml":
				return []byte(`name: Repo Assist
files:
  - workflows/review.md
`), nil
			case "packages/repo-assist/README.md":
				return []byte("# Repo Assist\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "packages/repo-assist"}, "")
		require.NoError(t, err)
		assert.Equal(t, "packages/repo-assist/aw.yml", pkg.ManifestPath)
		assert.Equal(t, "packages/repo-assist/README.md", pkg.DocsPath)
		assert.Equal(t, []string{"packages/repo-assist/workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
	})

	t.Run("nested bundle github workflows paths are repo-root-relative", func(t *testing.T) {
		// When a nested bundle manifest (at "dependabot/aw.yml") lists a
		// ".github/workflows/" path, it must resolve to the repo-root path
		// ".github/workflows/foo.md" — NOT to "dependabot/.github/workflows/foo.md".
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "dependabot/aw.yml":
				return []byte(`name: Dependabot
files:
  - workflows/review.md
  - .github/workflows/dependabot-orchestrator.md
`), nil
			case "dependabot/README.md":
				return []byte("# Dependabot\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo", PackagePath: "dependabot"}, "")
		require.NoError(t, err)
		// workflows/ path is package-relative; .github/workflows/ is repo-root-relative.
		assert.Equal(t, []string{
			"dependabot/workflows/review.md",
			".github/workflows/dependabot-orchestrator.md",
		}, packageInstallableSourcePaths(pkg.InstallationSource))
	})
}

func TestResolveLocalRepositoryPackagePreservesIcon(t *testing.T) {
	packageDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "aw.yml"), []byte("name: Local Package\nicon: \"📦\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "README.md"), []byte("# Local Package\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(packageDir, "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "workflows", "review.md"), []byte("---\non: issues\n---\n# Review\n"), 0o644))

	pkg, err := resolveLocalRepositoryPackage(packageDir)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, "📦", pkg.Icon)
}

func TestResolveLocalRepositoryPackageDiscoversProjectFile(t *testing.T) {
	packageDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "aw.yml"), []byte("name: Local Package\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "README.md"), []byte("# Local Package\n"), 0o644))
	projectFile := filepath.Join(packageDir, filepath.FromSlash(workflow.RepoConfigFileName))
	require.NoError(t, os.MkdirAll(filepath.Dir(projectFile), 0o755))
	require.NoError(t, os.WriteFile(projectFile, []byte(`{"utc":"+01:00"}`), 0o644))

	pkg, err := resolveLocalRepositoryPackage(packageDir)
	require.NoError(t, err)
	require.NotNil(t, pkg.ProjectFile)
	assert.Equal(t, projectFile, pkg.ProjectFile.SourcePath)
	assert.Equal(t, workflow.RepoConfigFileName, pkg.ProjectFile.DestinationPath)
}

func TestResolveLocalRepositoryPackageRejectsProjectFileSymlink(t *testing.T) {
	packageDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "aw.yml"), []byte("name: Local Package\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "README.md"), []byte("# Local Package\n"), 0o644))
	projectDir := filepath.Join(packageDir, filepath.FromSlash(filepath.Dir(workflow.RepoConfigFileName)))
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	target := filepath.Join(t.TempDir(), "outside.json")
	require.NoError(t, os.Symlink(target, filepath.Join(projectDir, filepath.Base(workflow.RepoConfigFileName))))

	_, err := resolveLocalRepositoryPackage(packageDir)
	require.ErrorContains(t, err, "symbolic link")
}

func TestResolveRepositoryPackageProjectFile(t *testing.T) {
	originalDownload := downloadPackageFileFromGitHubForHost
	t.Cleanup(func() {
		downloadPackageFileFromGitHubForHost = originalDownload
	})
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
		assert.Equal(t, "packages/repo-assist/.github/workflows/aw.json", filePath)
		return []byte(`{"utc":"+01:00"}`), nil
	}

	projectFile, err := resolveRepositoryPackageProjectFile(t.Context(), "owner", "repo", "packages/repo-assist", "main", "github.com")
	require.NoError(t, err)
	require.NotNil(t, projectFile)
	assert.Equal(t, "packages/repo-assist/.github/workflows/aw.json", projectFile.SourcePath)
	assert.Equal(t, workflow.RepoConfigFileName, projectFile.DestinationPath)
}

func TestResolveWorkflows_RepositoryPackage(t *testing.T) {
	originalFetchFn := fetchWorkflowFromSourceWithContextFn
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		fetchWorkflowFromSourceWithContextFn = originalFetchFn
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

	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		switch path {
		case "aw.yml":
			return []byte(`name: Repo Assist
files:
  - workflows/review.md
  - .github/workflows/nightly-review.md
`), nil
		case "README.md":
			return []byte("# Repo Assist\n"), nil
		}
		return nil, createRepositoryPackageNotFoundError(path)
	}
	listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
		t.Fatalf("unexpected scan of %s", workflowPath)
		return nil, nil
	}
	fetchWorkflowFromSourceWithContextFn = func(_ context.Context, spec *WorkflowSpec, _ bool) (*FetchedWorkflow, error) {
		return &FetchedWorkflow{
			Content:    []byte("---\nname: Test\non: push\n---\n"),
			CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			IsLocal:    false,
			SourcePath: spec.WorkflowPath,
		}, nil
	}

	resolved, err := ResolveWorkflows(context.Background(), []string{"owner/repo"}, false)
	require.NoError(t, err)
	require.Len(t, resolved.Workflows, 2)
	assert.Equal(t, "workflows/review.md", resolved.Workflows[0].Spec.WorkflowPath)
	assert.Equal(t, ".github/workflows/nightly-review.md", resolved.Workflows[1].Spec.WorkflowPath)
}

func TestResolveWorkflows_RepositoryPackageRejectsPrivateTrue(t *testing.T) {
	originalFetchFn := fetchWorkflowFromSourceWithContextFn
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		fetchWorkflowFromSourceWithContextFn = originalFetchFn
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
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		switch path {
		case "aw.yml":
			return []byte("name: Repo Assist\nfiles:\n  - workflows/review.md\n"), nil
		case "README.md":
			return []byte("# Repo Assist\n"), nil
		case "workflows/review.md":
			return []byte("---\nprivate: true\n---\n\n# Review\n"), nil
		default:
			return nil, createRepositoryPackageNotFoundError(path)
		}
	}
	listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
		t.Fatalf("unexpected scan of %s", workflowPath)
		return nil, nil
	}
	fetchWorkflowFromSourceWithContextFn = func(_ context.Context, spec *WorkflowSpec, _ bool) (*FetchedWorkflow, error) {
		return &FetchedWorkflow{
			Content:    []byte("---\nprivate: true\n---\n\n# Review\n"),
			CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			IsLocal:    false,
			SourcePath: spec.WorkflowPath,
		}, nil
	}

	_, err := ResolveWorkflows(context.Background(), []string{"owner/repo"}, false)
	require.Error(t, err)
	require.ErrorContains(t, err, `workflow "workflows/review.md" sets private: true`)
}

func TestResolveWorkflows_NestedRepositoryPackage(t *testing.T) {
	originalFetchFn := fetchWorkflowFromSourceWithContextFn
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		fetchWorkflowFromSourceWithContextFn = originalFetchFn
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

	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		switch path {
		case "folder/aw.yml":
			return []byte(`name: Repo Assist
files:
  - workflows/review.md
`), nil
		case "folder/README.md":
			return []byte("# Repo Assist\n"), nil
		}
		return nil, createRepositoryPackageNotFoundError(path)
	}
	listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
		t.Fatalf("unexpected scan of %s", workflowPath)
		return nil, nil
	}
	fetchWorkflowFromSourceWithContextFn = func(_ context.Context, spec *WorkflowSpec, _ bool) (*FetchedWorkflow, error) {
		return &FetchedWorkflow{
			Content:    []byte("---\nname: Test\non: push\n---\n"),
			CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			IsLocal:    false,
			SourcePath: spec.WorkflowPath,
		}, nil
	}

	resolved, err := ResolveWorkflows(context.Background(), []string{"owner/repo/folder"}, false)
	require.NoError(t, err)
	require.Len(t, resolved.Workflows, 1)
	assert.Equal(t, "folder/workflows/review.md", resolved.Workflows[0].Spec.WorkflowPath)
}

// TestResolveWorkflows_NestedRepositoryPackage_GithubWorkflowsPathIsRepoRoot reproduces
// the bug reported in gh-aw#41789: a ".github/workflows/" path listed in the manifest of
// a nested bundle (e.g. "dependabot/aw.yml") must resolve to the repository-root path
// ".github/workflows/foo.md", not to "dependabot/.github/workflows/foo.md".
func TestResolveWorkflows_NestedRepositoryPackage_GithubWorkflowsPathIsRepoRoot(t *testing.T) {
	originalFetchFn := fetchWorkflowFromSourceWithContextFn
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		fetchWorkflowFromSourceWithContextFn = originalFetchFn
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

	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		switch path {
		case "dependabot/aw.yml":
			return []byte(`name: Dependabot Workflows
files:
  - .github/workflows/dependabot-orchestrator.md
`), nil
		case "dependabot/README.md":
			return []byte("# Dependabot Workflows\n"), nil
		}
		return nil, createRepositoryPackageNotFoundError(path)
	}
	listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
		t.Fatalf("unexpected scan of %s", workflowPath)
		return nil, nil
	}
	fetchWorkflowFromSourceWithContextFn = func(_ context.Context, spec *WorkflowSpec, _ bool) (*FetchedWorkflow, error) {
		return &FetchedWorkflow{
			Content:    []byte("---\nname: Dependabot Orchestrator\non: push\n---\n"),
			CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			IsLocal:    false,
			SourcePath: spec.WorkflowPath,
		}, nil
	}

	resolved, err := ResolveWorkflows(context.Background(), []string{"owner/repo/dependabot"}, false)
	require.NoError(t, err)
	require.Len(t, resolved.Workflows, 1)
	// Must be the repo-root path, NOT "dependabot/.github/workflows/dependabot-orchestrator.md".
	assert.Equal(t, ".github/workflows/dependabot-orchestrator.md", resolved.Workflows[0].Spec.WorkflowPath)
}

// TestResolveWorkflows_NestedRepositoryPackage_AutoScan tests that auto-scan (no explicit
// files: in the manifest) finds workflows inside a nested bundle's own directories.
func TestResolveWorkflows_NestedRepositoryPackage_AutoScan(t *testing.T) {
	originalFetchFn := fetchWorkflowFromSourceWithContextFn
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		fetchWorkflowFromSourceWithContextFn = originalFetchFn
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

	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		switch path {
		case "mypkg/aw.yml":
			return []byte("name: My Package\n"), nil
		case "mypkg/README.md":
			return []byte("# My Package\n"), nil
		}
		return nil, createRepositoryPackageNotFoundError(path)
	}
	// Auto-scan returns full repo-root-relative paths, as the GitHub API does.
	listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
		switch workflowPath {
		case "mypkg/workflows":
			return []string{"mypkg/workflows/pr-review.md"}, nil
		default:
			return nil, createRepositoryPackageNotFoundError(workflowPath)
		}
	}
	fetchWorkflowFromSourceWithContextFn = func(_ context.Context, spec *WorkflowSpec, _ bool) (*FetchedWorkflow, error) {
		return &FetchedWorkflow{
			Content:    []byte("---\nname: PR Review\non: push\n---\n"),
			CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			IsLocal:    false,
			SourcePath: spec.WorkflowPath,
		}, nil
	}

	resolved, err := ResolveWorkflows(context.Background(), []string{"owner/repo/mypkg"}, false)
	require.NoError(t, err)
	require.Len(t, resolved.Workflows, 1)
	// Auto-scanned path must be the full repo-root path returned by the API.
	assert.Equal(t, "mypkg/workflows/pr-review.md", resolved.Workflows[0].Spec.WorkflowPath)
}

func TestResolveWorkflows_FallsBackToWorkflowWhenNestedManifestMissing(t *testing.T) {
	originalFetchFn := fetchWorkflowFromSourceWithContextFn
	originalDownload := downloadPackageFileFromGitHubForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		fetchWorkflowFromSourceWithContextFn = originalFetchFn
		downloadPackageFileFromGitHubForHost = originalDownload
		getRepositoryPackageDefaultBranch = originalDefaultBranch
	})
	getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
		return "main", nil
	}

	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		return nil, createRepositoryPackageNotFoundError(path)
	}
	fetchWorkflowFromSourceWithContextFn = func(_ context.Context, spec *WorkflowSpec, _ bool) (*FetchedWorkflow, error) {
		return &FetchedWorkflow{
			Content:    []byte("---\nname: Test\non: push\n---\n"),
			CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			IsLocal:    false,
			SourcePath: spec.WorkflowPath,
		}, nil
	}

	resolved, err := ResolveWorkflows(context.Background(), []string{"owner/repo/review"}, false)
	require.NoError(t, err)
	require.Len(t, resolved.Workflows, 1)
	assert.Equal(t, "workflows/review.md", resolved.Workflows[0].Spec.WorkflowPath)
}

func TestParseRepositoryPackageSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		spec            string
		wantOK          bool
		wantErr         string
		wantRepoSlug    string
		wantPackagePath string
		wantVersion     string
	}{
		{
			name:         "repo only package",
			spec:         "owner/repo",
			wantOK:       true,
			wantRepoSlug: "owner/repo",
		},
		{
			name:         "repo only package with slash branch ref",
			spec:         "owner/repo@feature/github-agentic-workflow",
			wantOK:       true,
			wantRepoSlug: "owner/repo",
			wantVersion:  "feature/github-agentic-workflow",
		},
		{
			name:         "repo only package with sanitized branch characters",
			spec:         "owner/repo@release/2026.05.27-rc_1",
			wantOK:       true,
			wantRepoSlug: "owner/repo",
			wantVersion:  "release/2026.05.27-rc_1",
		},
		{
			name:         "repo only package with long hyphenated repo name",
			spec:         "owner/this-repository-name-is-significantly-longer-than-thirty-nine",
			wantOK:       true,
			wantRepoSlug: "owner/this-repository-name-is-significantly-longer-than-thirty-nine",
		},
		{
			name:            "nested package path",
			spec:            "owner/repo/packages/repo-assist",
			wantOK:          true,
			wantRepoSlug:    "owner/repo",
			wantPackagePath: "packages/repo-assist",
		},
		{
			name:            "nested package path with slash branch ref",
			spec:            "owner/repo/agentic-workflows@feature/github-agentic-workflow",
			wantOK:          true,
			wantRepoSlug:    "owner/repo",
			wantPackagePath: "agentic-workflows",
			wantVersion:     "feature/github-agentic-workflow",
		},
		{
			name:            "nested package path with sanitized branch characters",
			spec:            "owner/repo/agentic-workflows@hotfix/github-aw_fix-1.2.3",
			wantOK:          true,
			wantRepoSlug:    "owner/repo",
			wantPackagePath: "agentic-workflows",
			wantVersion:     "hotfix/github-aw_fix-1.2.3",
		},
		{
			name:   "workflow path is not package",
			spec:   "owner/repo/workflows/review.md",
			wantOK: false,
		},
		{
			name:   "workflow path with branch ref is not package",
			spec:   "owner/repo/agentic-workflows/pr-review.md@feature/github-agentic-workflows",
			wantOK: false,
		},
		{
			name:   "url is not package",
			spec:   "https://github.com/owner/repo",
			wantOK: false,
		},
		{
			name:    "rejects path traversal",
			spec:    "owner/repo/../secrets",
			wantOK:  true,
			wantErr: `invalid repository package path "../secrets"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoSpec, ok, err := parseRepositoryPackageSpec(tt.spec)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			if !tt.wantOK {
				assert.Nil(t, repoSpec)
				return
			}
			require.NotNil(t, repoSpec)
			assert.Equal(t, tt.wantRepoSlug, repoSpec.RepoSlug)
			assert.Equal(t, tt.wantPackagePath, repoSpec.PackagePath)
			assert.Equal(t, tt.wantVersion, repoSpec.Version)
		})
	}
}

func TestIsSupportedPackageInstallablePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		// .md files: allowed under workflows/, agentic-workflows/, and .github/workflows/
		{"workflows/review.md", true},
		{"agentic-workflows/review.md", true},
		{".github/workflows/nightly-review.md", true},
		// .yml action workflow files: allowed only under .github/workflows/ (direct children only)
		{".github/workflows/deploy.yml", true},
		{".github/workflows/ci.yml", true},
		// mixed-case extensions are accepted
		{".github/workflows/CI.YML", true},
		// .yml files under workflows/ and agentic-workflows/ are NOT supported
		{"workflows/deploy.yml", false},
		{"agentic-workflows/deploy.yml", false},
		// nested subdirectories under .github/workflows/ are NOT supported for .yml
		{".github/workflows/subdir/ci.yml", false},
		// .lock.yml files are NOT supported (generated artifacts)
		{".github/workflows/deploy.lock.yml", false},
		{"workflows/deploy.lock.yml", false},
		// unsupported extensions
		{".github/workflows/script.sh", false},
		{"README.md", false},
		{".github/workflows/config.yaml", false},
		// path traversal
		{"../evil.md", false},
		{"workflows/../README.md", false},
		{".github/workflows/../x.yml", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isSupportedPackageInstallablePath(tt.path))
		})
	}
}

func TestExtractManifestIncludes(t *testing.T) {
	t.Parallel()
	includes, warnings, err := extractManifestIncludes([]any{
		"workflows/review.md",
		"agentic-workflows/review.md",
		"skills/code-review",
		"agents/reviewer.md",
		".github/workflows/ci.yml",
	}, "aw.yml")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"workflows/review.md",
		"agentic-workflows/review.md",
		"skills/code-review",
		"agents/reviewer.md",
		".github/workflows/ci.yml",
	}, manifestIncludeSources(includes))
	assert.Empty(t, warnings)
}

func TestCodemodManifestFilesToIncludes(t *testing.T) {
	t.Parallel()
	converted := codemodManifestFilesToIncludes([]string{
		"workflows/review.md",
		".github/workflows/ci.yml",
	})
	assert.Equal(t, []string{
		"workflows/review.md",
		".github/workflows/ci.yml",
	}, converted)
}

func TestResolveRepositoryPackage_ActionWorkflowYML(t *testing.T) {
	originalVersion := GetVersion()
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		SetVersionInfo(originalVersion)
		downloadPackageFileFromGitHubForHost = originalDownload
		listPackageWorkflowFilesForHost = originalList
		listPackageDirFilesForHost = originalDirFiles
		listPackageDirSubdirsForHost = originalDirSubdirs
		getRepositoryPackageDefaultBranch = originalDefaultBranch
	})
	SetVersionInfo("v1.2.3")
	getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
		return "main", nil
	}
	listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}
	listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}

	t.Run("includes yml action workflow from files list", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`name: CI Pack
files:
  - workflows/triage.md
  - .github/workflows/ci.yml
`), nil
			case "README.md":
				return []byte("# CI Pack\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"workflows/triage.md", ".github/workflows/ci.yml"}, packageInstallableSourcePaths(pkg.InstallationSource))
		assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "Field 'files'")
	})

	t.Run("rejects yml files outside .github/workflows with warning", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`name: CI Pack
files:
  - workflows/triage.md
  - workflows/deploy.yml
`), nil
			case "README.md":
				return []byte("# CI Pack\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		// Only the .md file should be accepted; the yml under workflows/ is rejected
		assert.Equal(t, []string{"workflows/triage.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
		require.NotEmpty(t, pkg.Warnings)
		assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "Ignoring files entry")
	})

	t.Run("rejects duplicate markdown filenames across different folders", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
			switch path {
			case "aw.yml":
				return []byte(`name: Duplicate Filenames
files:
  - workflows/triage.md
  - workflows/subdir/triage.md
`), nil
			case "README.md":
				return []byte("# Duplicate Filenames\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(path)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected scan of %s", workflowPath)
			return nil, nil
		}

		_, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.Error(t, err)
		require.ErrorContains(t, err, "duplicate workflow filename")
	})
}

func TestResolveWorkflows_ActionWorkflowYML(t *testing.T) {
	originalFetchFn := fetchWorkflowFromSourceWithContextFn
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		fetchWorkflowFromSourceWithContextFn = originalFetchFn
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

	rawYML := []byte("name: CI\non: [push]\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: []\n")

	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		switch path {
		case "aw.yml":
			return []byte(`name: CI Pack
files:
  - workflows/triage.md
  - .github/workflows/ci.yml
`), nil
		case "README.md":
			return []byte("# CI Pack\n"), nil
		}
		return nil, createRepositoryPackageNotFoundError(path)
	}
	listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
		t.Fatalf("unexpected scan of %s", workflowPath)
		return nil, nil
	}
	fetchWorkflowFromSourceWithContextFn = func(_ context.Context, spec *WorkflowSpec, _ bool) (*FetchedWorkflow, error) {
		switch spec.WorkflowPath {
		case "workflows/triage.md":
			return &FetchedWorkflow{
				Content:    []byte("---\nname: Triage\non: issues\n---\n"),
				CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				IsLocal:    false,
				SourcePath: spec.WorkflowPath,
			}, nil
		case ".github/workflows/ci.yml":
			return &FetchedWorkflow{
				Content:    rawYML,
				CommitSHA:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				IsLocal:    false,
				SourcePath: spec.WorkflowPath,
			}, nil
		}
		return nil, fmt.Errorf("unexpected path: %s", spec.WorkflowPath)
	}

	resolved, err := ResolveWorkflows(context.Background(), []string{"owner/repo"}, false)
	require.NoError(t, err)
	require.Len(t, resolved.Workflows, 2)

	// First workflow is the .md agentic workflow
	assert.Equal(t, "workflows/triage.md", resolved.Workflows[0].Spec.WorkflowPath)
	assert.Equal(t, "triage", resolved.Workflows[0].Spec.WorkflowName)
	assert.False(t, resolved.Workflows[0].IsActionWorkflow)

	// Second workflow is the .yml action workflow
	assert.Equal(t, ".github/workflows/ci.yml", resolved.Workflows[1].Spec.WorkflowPath)
	assert.Equal(t, "ci", resolved.Workflows[1].Spec.WorkflowName)
	assert.True(t, resolved.Workflows[1].IsActionWorkflow)
	assert.YAMLEq(t, string(rawYML), string(resolved.Workflows[1].Content))
}

func TestIsSupportedSkillDirPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		// Valid: direct children of skills/
		{"skills/my-skill", true},
		{"skills/code-review", true},
		{"skills/a", true},
		// Invalid: nested further
		{"skills/my-skill/subdir", false},
		// Invalid: wrong prefix
		{"agents/my-skill", false},
		{"workflows/my-skill", false},
		{".github/skills/my-skill", true},
		// Invalid: empty
		{"", false},
		// Invalid: path traversal
		{"skills/../evil", false},
		// Invalid: just the prefix
		{"skills/", false},
		{"skills", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, isSupportedSkillDirPath(tt.path))
		})
	}
}

func TestIsSupportedAgentFilePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		// Valid: .md files directly under agents/
		{"agents/my-agent.md", true},
		{"agents/code-review.md", true},
		{"agents/a.md", true},
		// Invalid: non-.md extension
		{"agents/my-agent.sh", false},
		{"agents/my-agent.yml", false},
		{"agents/my-agent", false},
		// Invalid: nested subdirectory
		{"agents/subdir/my-agent.md", false},
		// Invalid: wrong prefix
		{"skills/my-agent.md", false},
		{"workflows/my-agent.md", false},
		{".github/agents/my-agent.md", true},
		// Invalid: empty
		{"", false},
		// Invalid: path traversal
		{"agents/../evil.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, isSupportedAgentFilePath(tt.path))
		})
	}
}

func TestExtractManifestSkillDirs(t *testing.T) {
	t.Parallel()
	t.Run("valid entries are accepted", func(t *testing.T) {
		dirs, warnings := extractManifestSkillDirs([]any{"skills/review", "skills/triage"}, "aw.yml")
		assert.Equal(t, []string{"skills/review", "skills/triage"}, dirs)
		assert.Empty(t, warnings)
	})

	t.Run("invalid entries produce warnings", func(t *testing.T) {
		dirs, warnings := extractManifestSkillDirs([]any{"skills/valid", "not-skills/bad", ".github/skills/bad"}, "aw.yml")
		assert.Equal(t, []string{"skills/valid", ".github/skills/bad"}, dirs)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "not-skills/bad")
	})

	t.Run("duplicate entries are deduplicated", func(t *testing.T) {
		dirs, warnings := extractManifestSkillDirs([]any{"skills/review", "skills/review"}, "aw.yml")
		assert.Equal(t, []string{"skills/review"}, dirs)
		assert.Empty(t, warnings)
	})

	t.Run("non-list value produces warning", func(t *testing.T) {
		dirs, warnings := extractManifestSkillDirs("not-a-list", "aw.yml")
		assert.Empty(t, dirs)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "not a list of strings")
	})
}

func TestExtractManifestAgentFiles(t *testing.T) {
	t.Parallel()
	t.Run("valid entries are accepted", func(t *testing.T) {
		files, warnings := extractManifestAgentFiles([]any{"agents/review.md", "agents/triage.md"}, "aw.yml")
		assert.Equal(t, []string{"agents/review.md", "agents/triage.md"}, files)
		assert.Empty(t, warnings)
	})

	t.Run("invalid entries produce warnings", func(t *testing.T) {
		files, warnings := extractManifestAgentFiles([]any{"agents/valid.md", "agents/bad.sh", "skills/bad.md"}, "aw.yml")
		assert.Equal(t, []string{"agents/valid.md"}, files)
		require.Len(t, warnings, 2)
		assert.Contains(t, warnings[0], "agents/bad.sh")
		assert.Contains(t, warnings[1], "skills/bad.md")
	})

	t.Run("duplicate entries are deduplicated", func(t *testing.T) {
		files, warnings := extractManifestAgentFiles([]any{"agents/review.md", "agents/review.md"}, "aw.yml")
		assert.Equal(t, []string{"agents/review.md"}, files)
		assert.Empty(t, warnings)
	})

	t.Run("non-list value produces warning", func(t *testing.T) {
		files, warnings := extractManifestAgentFiles("not-a-list", "aw.yml")
		assert.Empty(t, files)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "not a list of strings")
	})
}

func TestResolveRepositoryPackage_SkillsAndAgents(t *testing.T) {
	originalVersion := GetVersion()
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirFilesRecursively := listPackageDirFilesRecursivelyForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		SetVersionInfo(originalVersion)
		downloadPackageFileFromGitHubForHost = originalDownload
		listPackageWorkflowFilesForHost = originalList
		listPackageDirFilesForHost = originalDirFiles
		listPackageDirFilesRecursivelyForHost = originalDirFilesRecursively
		listPackageDirSubdirsForHost = originalDirSubdirs
		getRepositoryPackageDefaultBranch = originalDefaultBranch
	})
	SetVersionInfo("v1.2.3")
	getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
		return "main", nil
	}

	t.Run("explicit skills and agents from manifest", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
			switch filePath {
			case "aw.yml":
				return []byte(`name: My Package
skills:
  - skills/code-review
agents:
  - agents/triage.md
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# My Package\n"), nil
			case "skills/code-review/SKILL.md":
				return []byte("# skill\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(filePath)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected workflow scan of %s", workflowPath)
			return nil, nil
		}
		listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			if dirPath == "skills/code-review" {
				return []string{"skills/code-review/SKILL.md", "skills/code-review/prompt.sh"}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		// Auto-scan runs after manifest skills; return empty so no extras are added.
		listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
		require.Len(t, pkg.SkillFiles, 2)
		assert.Equal(t, "skills/code-review/SKILL.md", pkg.SkillFiles[0].SourcePath)
		assert.Equal(t, "code-review", pkg.SkillFiles[0].SkillName)
		assert.Equal(t, "skills/code-review/prompt.sh", pkg.SkillFiles[1].SourcePath)
		assert.Equal(t, "code-review", pkg.SkillFiles[1].SkillName)
		assert.Equal(t, []string{"agents/triage.md"}, pkg.AgentFiles)
		assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "Field 'files'")
	})

	t.Run("includes field infers workflow skill and agent types", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
			switch filePath {
			case "aw.yml":
				return []byte(`name: My Package
includes:
  - workflows/review.md
  - skills/code-review
  - .github/agents/triage.md
`), nil
			case "README.md":
				return []byte("# My Package\n"), nil
			case "skills/code-review/SKILL.md":
				return []byte("# skill\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(filePath)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected workflow scan of %s", workflowPath)
			return nil, nil
		}
		listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			if dirPath == "skills/code-review" {
				return []string{"skills/code-review/SKILL.md", "skills/code-review/prompt.md"}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		// Auto-scan runs after manifest skills; return empty so no extras are added.
		listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"workflows/review.md"}, packageInstallableSourcePaths(pkg.InstallationSource))
		require.Len(t, pkg.SkillFiles, 2)
		assert.Equal(t, []string{".github/agents/triage.md"}, pkg.AgentFiles)
	})

	t.Run("auto-scans skills and agents when absent from manifest", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
			switch filePath {
			case "aw.yml":
				return []byte(`name: My Package
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# My Package\n"), nil
			// SKILL.md marker check
			case "skills/auto-skill/SKILL.md":
				return []byte("# Auto Skill\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(filePath)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected workflow scan of %s", workflowPath)
			return nil, nil
		}
		// Agent files are listed with the non-recursive helper.
		listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			if dirPath == "agents" {
				return []string{"agents/my-agent.md", "agents/helper.sh"}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		// Skill files are listed with the recursive helper.
		listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			if dirPath == "skills/auto-skill" {
				return []string{"skills/auto-skill/SKILL.md"}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			if dirPath == "skills" {
				return []string{"skills/auto-skill"}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		require.Len(t, pkg.SkillFiles, 1)
		assert.Equal(t, "skills/auto-skill/SKILL.md", pkg.SkillFiles[0].SourcePath)
		assert.Equal(t, "auto-skill", pkg.SkillFiles[0].SkillName)
		// Only .md files from agents/ are included; helper.sh is filtered out
		assert.Equal(t, []string{"agents/my-agent.md"}, pkg.AgentFiles)
	})

	t.Run("missing skill directory produces warning not error", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
			switch filePath {
			case "aw.yml":
				return []byte(`name: My Package
skills:
  - skills/missing-skill
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# My Package\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(filePath)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected workflow scan of %s", workflowPath)
			return nil, nil
		}
		listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		// Auto-scan runs after manifest skills; return empty so no extras are added.
		listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Empty(t, pkg.SkillFiles)
		require.NotEmpty(t, pkg.Warnings)
		assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "skills/missing-skill")
	})

	t.Run("explicit skill without marker produces warning", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
			switch filePath {
			case "aw.yml":
				return []byte(`name: My Package
skills:
  - skills/no-marker
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# My Package\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(filePath)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected workflow scan of %s", workflowPath)
			return nil, nil
		}
		listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			if dirPath == "skills/no-marker" {
				return []string{"skills/no-marker/prompt.md"}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Empty(t, pkg.SkillFiles)
		assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "missing required SKILL.md")
	})

	t.Run("no skills or agents when directories absent", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
			switch filePath {
			case "aw.yml":
				return []byte(`name: My Package
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# My Package\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(filePath)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			// Auto-scan of agents/ returns not-found
			return nil, createRepositoryPackageNotFoundError(workflowPath)
		}
		listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		assert.Empty(t, pkg.SkillFiles)
		assert.Empty(t, pkg.AgentFiles)
		assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "Field 'files'")
	})

	t.Run("manifest skills copied first then auto-scanned additional skills appended", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
			switch filePath {
			case "aw.yml":
				return []byte(`name: My Package
skills:
  - skills/review
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# My Package\n"), nil
			case "skills/review/SKILL.md":
				return []byte("# Review Skill\n"), nil
			// SKILL.md marker check for auto-scanned skill
			case "skills/triage/SKILL.md":
				return []byte("# Triage Skill\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(filePath)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected workflow scan of %s", workflowPath)
			return nil, nil
		}
		listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			switch dirPath {
			case "skills/review":
				return []string{"skills/review/SKILL.md"}, nil
			case "skills/triage":
				return []string{"skills/triage/SKILL.md", "skills/triage/prompts/detailed.md"}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		// Auto-scan finds "triage" in addition to the manifest-specified "review".
		listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			if dirPath == "skills" {
				return []string{"skills/review", "skills/triage"}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		// Manifest skill "review" appears first; auto-scanned "triage" appended after.
		require.Len(t, pkg.SkillFiles, 3)
		assert.Equal(t, "review", pkg.SkillFiles[0].SkillName)
		assert.Equal(t, "triage", pkg.SkillFiles[1].SkillName)
		assert.Equal(t, "triage", pkg.SkillFiles[2].SkillName)
		assert.Equal(t, "skills/triage/prompts/detailed.md", pkg.SkillFiles[2].SourcePath)
		assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "Field 'files'")
	})

	t.Run("auto-scan errors are warnings when manifest skills are explicit", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
			switch filePath {
			case "aw.yml":
				return []byte(`name: My Package
skills:
  - skills/review
files:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# My Package\n"), nil
			case "skills/review/SKILL.md":
				return []byte("# Review Skill\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(filePath)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected workflow scan of %s", workflowPath)
			return nil, nil
		}
		listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			if dirPath == "skills/review" {
				return []string{"skills/review/SKILL.md", "skills/review/prompts/default.md"}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, errors.New("rate limit")
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		require.Len(t, pkg.SkillFiles, 2)
		assert.Equal(t, "skills/review/SKILL.md", pkg.SkillFiles[0].SourcePath)
		assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "failed to auto-scan skills directory")
	})

	t.Run("skill folder nested files are included recursively", func(t *testing.T) {
		downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
			switch filePath {
			case "aw.yml":
				return []byte(`name: My Package
skills:
  - skills/my-skill
includes:
  - workflows/review.md
`), nil
			case "README.md":
				return []byte("# My Package\n"), nil
			case "skills/my-skill/SKILL.md":
				return []byte("# My Skill\n"), nil
			default:
				return nil, createRepositoryPackageNotFoundError(filePath)
			}
		}
		listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
			t.Fatalf("unexpected workflow scan of %s", workflowPath)
			return nil, nil
		}
		listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			if dirPath == "skills/my-skill" {
				// Simulate a skill with nested files in subdirectories.
				return []string{
					"skills/my-skill/SKILL.md",
					"skills/my-skill/ssl.json",
					"skills/my-skill/scripts/query.sh",
					"skills/my-skill/scripts/helpers/util.sh",
				}, nil
			}
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}
		listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
			return nil, createRepositoryPackageNotFoundError(dirPath)
		}

		pkg, err := resolveRepositoryPackage(t.Context(), &RepoSpec{RepoSlug: "owner/repo"}, "")
		require.NoError(t, err)
		require.Len(t, pkg.SkillFiles, 4)
		assert.Equal(t, "skills/my-skill/SKILL.md", pkg.SkillFiles[0].SourcePath)
		assert.Equal(t, "skills/my-skill/ssl.json", pkg.SkillFiles[1].SourcePath)
		assert.Equal(t, "skills/my-skill/scripts/query.sh", pkg.SkillFiles[2].SourcePath)
		assert.Equal(t, "skills/my-skill/scripts/helpers/util.sh", pkg.SkillFiles[3].SourcePath)
		for _, sf := range pkg.SkillFiles {
			assert.Equal(t, "my-skill", sf.SkillName)
		}
	})
}

func TestResolveWorkflows_SkillsAndAgents(t *testing.T) {
	originalFetchFn := fetchWorkflowFromSourceWithContextFn
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirFilesRecursively := listPackageDirFilesRecursivelyForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		fetchWorkflowFromSourceWithContextFn = originalFetchFn
		downloadPackageFileFromGitHubForHost = originalDownload
		listPackageWorkflowFilesForHost = originalList
		listPackageDirFilesForHost = originalDirFiles
		listPackageDirFilesRecursivelyForHost = originalDirFilesRecursively
		listPackageDirSubdirsForHost = originalDirSubdirs
		getRepositoryPackageDefaultBranch = originalDefaultBranch
	})
	getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
		return "main", nil
	}

	skillMD := []byte("# My Skill\nThis is a skill.\n")
	agentMD := []byte("# My Agent\nThis is an agent.\n")
	workflowMD := []byte("---\nname: Review\non: issues\n---\n")

	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
		switch filePath {
		case "aw.yml":
			return []byte(`name: Full Package
skills:
  - skills/my-skill
agents:
  - agents/my-agent.md
files:
  - workflows/review.md
`), nil
		case "README.md":
			return []byte("# Full Package\n"), nil
		case "skills/my-skill/SKILL.md":
			return []byte("# My Skill\nThis is a skill.\n"), nil
		default:
			return nil, createRepositoryPackageNotFoundError(filePath)
		}
	}
	listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
		t.Fatalf("unexpected workflow scan of %s", workflowPath)
		return nil, nil
	}
	listPackageDirFilesRecursivelyForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		if dirPath == "skills/my-skill" {
			return []string{"skills/my-skill/SKILL.md"}, nil
		}
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}
	// Auto-scan runs after manifest skills; return empty so no extras are added.
	listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}
	fetchWorkflowFromSourceWithContextFn = func(_ context.Context, spec *WorkflowSpec, _ bool) (*FetchedWorkflow, error) {
		switch spec.WorkflowPath {
		case "workflows/review.md":
			return &FetchedWorkflow{Content: workflowMD, CommitSHA: "aaaa", IsLocal: false, SourcePath: spec.WorkflowPath}, nil
		case "skills/my-skill/SKILL.md":
			return &FetchedWorkflow{Content: skillMD, CommitSHA: "bbbb", IsLocal: false, SourcePath: spec.WorkflowPath}, nil
		case "agents/my-agent.md":
			return &FetchedWorkflow{Content: agentMD, CommitSHA: "cccc", IsLocal: false, SourcePath: spec.WorkflowPath}, nil
		}
		return nil, fmt.Errorf("unexpected path: %s", spec.WorkflowPath)
	}

	resolved, err := ResolveWorkflows(context.Background(), []string{"owner/repo"}, false)
	require.NoError(t, err)
	require.Len(t, resolved.Workflows, 3)

	// Workflow
	wf := resolved.Workflows[0]
	assert.Equal(t, "workflows/review.md", wf.Spec.WorkflowPath)
	assert.False(t, wf.IsPackageSkillFile)
	assert.False(t, wf.IsPackageAgentFile)

	// Skill file
	skill := resolved.Workflows[1]
	assert.Equal(t, "skills/my-skill/SKILL.md", skill.Spec.WorkflowPath)
	assert.True(t, skill.IsPackageSkillFile)
	assert.False(t, skill.IsPackageAgentFile)
	assert.Equal(t, "my-skill", skill.SkillName)
	assert.Equal(t, skillMD, skill.Content)

	// Agent file
	agent := resolved.Workflows[2]
	assert.Equal(t, "agents/my-agent.md", agent.Spec.WorkflowPath)
	assert.False(t, agent.IsPackageSkillFile)
	assert.True(t, agent.IsPackageAgentFile)
	assert.Equal(t, agentMD, agent.Content)
}

func TestIsGhAwRepository(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		repoSlug string
		want     bool
	}{
		{name: "exact match", repoSlug: "github/gh-aw", want: true},
		{name: "case-insensitive with whitespace", repoSlug: "  GitHub/GH-AW  ", want: true},
		{name: "different repository", repoSlug: "github/other", want: false},
		{name: "fork-like suffix does not match", repoSlug: "github/gh-aw-fork", want: false},
		{name: "different owner does not match", repoSlug: "other/gh-aw", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGhAwRepository(tt.repoSlug)
			assert.Equal(t, tt.want, got)
		})
	}
}

// bootstrapTestHelpers sets up the common mock functions used by bootstrap profile
// propagation tests and registers their cleanup.
func bootstrapTestHelpers(t *testing.T) {
	t.Helper()
	originalFetchFn := fetchWorkflowFromSourceWithContextFn
	originalDownload := downloadPackageFileFromGitHubForHost
	originalList := listPackageWorkflowFilesForHost
	originalDirFiles := listPackageDirFilesForHost
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	t.Cleanup(func() {
		fetchWorkflowFromSourceWithContextFn = originalFetchFn
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
		t.Fatalf("unexpected scan of %s", workflowPath)
		return nil, nil
	}
	fetchWorkflowFromSourceWithContextFn = func(_ context.Context, spec *WorkflowSpec, _ bool) (*FetchedWorkflow, error) {
		return &FetchedWorkflow{
			Content:    []byte("---\nname: Test\non: push\n---\n"),
			CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			IsLocal:    false,
			SourcePath: spec.WorkflowPath,
		}, nil
	}
}

func TestResolveWorkflows_BootstrapProfile_SinglePackage(t *testing.T) {
	bootstrapTestHelpers(t)

	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		switch path {
		case "aw.yml":
			return []byte(`name: My Package
files:
  - workflows/review.md
config:
  - type: repo-variable
    name: MY_VAR
    prompt: Enter a value
`), nil
		case "README.md":
			return []byte("# My Package\n"), nil
		}
		return nil, createRepositoryPackageNotFoundError(path)
	}

	resolved, err := ResolveWorkflows(context.Background(), []string{"owner/repo"}, false)
	require.NoError(t, err)
	require.Len(t, resolved.Workflows, 1)

	require.NotNil(t, resolved.BootstrapProfile, "BootstrapProfile should be populated from the package config")
	assert.Equal(t, "owner/repo", resolved.BootstrapProfile.PackageID)
	require.Len(t, resolved.BootstrapProfile.Profile.Config, 1)
	assert.Equal(t, "repo-variable", resolved.BootstrapProfile.Profile.Config[0].Type)
	assert.Equal(t, "MY_VAR", resolved.BootstrapProfile.Profile.Config[0].Name)
}

func TestResolveWorkflows_BootstrapProfile_MultiplePackagesWarnsAndSuppresses(t *testing.T) {
	bootstrapTestHelpers(t)

	// Two separate repository packages, each declaring a config section.
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		var pkgName, varName string
		switch repo {
		case "pkg-a":
			pkgName, varName = "Package A", "VAR_A"
		case "pkg-b":
			pkgName, varName = "Package B", "VAR_B"
		default:
			return nil, createRepositoryPackageNotFoundError(path)
		}
		switch path {
		case "aw.yml":
			return fmt.Appendf(nil, `name: %s
files:
  - workflows/review.md
config:
  - type: repo-variable
    name: %s
    prompt: Enter a value
`, pkgName, varName), nil
		case "README.md":
			return []byte("# " + pkgName + "\n"), nil
		}
		return nil, createRepositoryPackageNotFoundError(path)
	}

	resolved, err := ResolveWorkflows(context.Background(), []string{"owner/pkg-a", "owner/pkg-b"}, false)
	require.NoError(t, err)

	assert.Nil(t, resolved.BootstrapProfile, "BootstrapProfile should be nil when multiple packages declare config")

	// Verify the multi-profile warning is present (other deprecation/experimental warnings may also be present)
	found := false
	for _, w := range resolved.Warnings {
		if strings.Contains(w, "multiple bootstrap profiles found") {
			assert.Contains(t, w, "owner/pkg-a")
			assert.Contains(t, w, "owner/pkg-b")
			found = true
			break
		}
	}
	assert.True(t, found, "expected a warning about multiple bootstrap profiles, got: %v", resolved.Warnings)
}

func TestPrintBootstrapConfigTODO(t *testing.T) {
	t.Parallel()
	t.Run("noop when profile is nil", func(t *testing.T) {
		var buf strings.Builder
		printBootstrapConfigTODO(&buf, nil)
		assert.Empty(t, buf.String())
	})

	t.Run("prints checklist items to provided writer", func(t *testing.T) {
		profile := &resolvedBootstrapProfile{
			PackageID: "owner/repo",
			Profile: &repositoryPackageBootstrap{
				Config: []repositoryPackageBootstrapAction{
					{Type: "require-owner-type", Value: "org"},
					{Type: "repo-variable", Name: "MY_VAR", Prompt: "Enter a value"},
					{Type: "repo-secret", Name: "MY_SECRET", Prompt: "Enter secret"},
					{Type: "copilot-auth", Secret: "COPILOT_TOKEN"},
					{Type: "commit-and-push", Message: "Bootstrap repository changes"},
					{Type: "handoff", Message: "Run the bootstrap wizard."},
				},
			},
		}
		var buf strings.Builder
		printBootstrapConfigTODO(&buf, profile)
		out := buf.String()
		assert.Contains(t, out, "owner/repo")
		assert.Contains(t, out, "☐ Verify repository owner type: org")
		assert.Contains(t, out, "☐ Set repository variable: MY_VAR")
		assert.Contains(t, out, "☐ Set repository secret: MY_SECRET")
		assert.Contains(t, out, "☐ Set Copilot PAT secret: COPILOT_TOKEN")
		assert.Contains(t, out, "☐ Commit and push local changes — Bootstrap repository changes")
		assert.Contains(t, out, "Run the bootstrap wizard.")
		assert.NotContains(t, out, "gh aw bootstrap")
	})
}

// manifestIncludeSources returns the source path of each manifest include entry.
func manifestIncludeSources(includes []repositoryPackageInclude) []string {
	sources := make([]string, 0, len(includes))
	for _, include := range includes {
		sources = append(sources, include.Source)
	}
	return sources
}
