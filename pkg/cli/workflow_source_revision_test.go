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
	"time"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowSourceRevision_ResourceOwnershipSeparatesTrackingRef(t *testing.T) {
	const commitSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	dir := testutil.TempDir(t, "resource-source-revision-*")
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	t.Chdir(dir)
	originalDownload := downloadPackageFileFromGitHubForHost
	t.Cleanup(func() { downloadPackageFileFromGitHubForHost = originalDownload })
	downloadPackageFileFromGitHubForHost = func(_ context.Context, _, _, _, ref, _ string) ([]byte, error) {
		assert.Equal(t, commitSHA, ref)
		return []byte("{\"version\":1}\n"), nil
	}
	require.NoError(t, syncManifestManagedResources(context.Background(),
		&RepoSpec{RepoSlug: "owner/repo", Version: "main"},
		&resolvedRepositoryPackage{
			ResolvedRef: commitSHA,
			ResourceFiles: []resolvedPackageResource{{
				SourcePath: "config.json", DestinationPath: "config.json",
			}},
		},
		commitSHA, "main", UpdateWorkflowsOptions{},
	))
	record, err := readPackageOwnershipRecord(packageOwnershipRecordPath(dir, "owner/repo"))
	require.NoError(t, err)
	assert.Equal(t, "owner/repo@main", record.Source)
	assert.Equal(t, commitSHA, record.ResolvedCommit)
}

func TestWorkflowSourceRevision_ManifestDefaultRefKeepsCommitSource(t *testing.T) {
	const commitSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, requestedRef := range []string{"", "main"} {
		t.Run("requested="+requestedRef, func(t *testing.T) {
			specs := appendRepositoryPackageWorkflowSpecs(nil, &RepoSpec{RepoSlug: "owner/repo", Version: requestedRef},
				&resolvedRepositoryPackage{
					ResolvedRef: "main",
					InstallationSource: []resolvedPackageInstallable{{
						SourcePath: "workflows/triage.md", DestinationPath: ".github/workflows/triage.md",
					}},
				})
			require.Len(t, specs, 1)
			assert.Equal(t, "main", specs[0].Version, "fetching still uses the resolved package ref")
			wantRef := requestedRef
			if wantRef == "" {
				wantRef = commitSHA
			}
			assert.Equal(t, "owner/repo@"+wantRef, buildSourceStringWithCommitSHA(specs[0], commitSHA))
			assert.Equal(t, "owner/repo@"+wantRef, packageSourceForSpec(specs[0], &FetchedWorkflow{CommitSHA: commitSHA}))
		})
	}
}

func TestWorkflowSourceRevision_BranchAdvancePreservesMergeBase(t *testing.T) {
	const oldSHA = "1111111111111111111111111111111111111111"
	const nextSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const upstreamPath = ".github/workflows/triage.md"
	const oldContent = `---
on: workflow_dispatch
---
# Original upstream title

## Instructions

First instruction.

Second instruction.

Third instruction.

## Local configuration

Local setting: original.
`
	newContent := strings.Replace(oldContent, "# Original upstream title", "# Updated upstream title", 1)

	for _, tt := range []struct {
		name       string
		manifest   bool
		localEdits bool
	}{
		{name: "direct"},
		{name: "direct with local edits", localEdits: true},
		{name: "manifest", manifest: true},
		{name: "manifest with local edits", manifest: true, localEdits: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "workflow-source-revision-*")
			spec := &WorkflowSpec{
				RepoSpec:               RepoSpec{RepoSlug: "owner/repo", Version: "main"},
				WorkflowPath:           upstreamPath,
				FromRepositoryManifest: tt.manifest,
			}

			installed, err := applySourceAndIncludeModifications(oldContent, spec,
				&FetchedWorkflow{CommitSHA: oldSHA}, dir, AddOptions{})
			require.NoError(t, err)
			if tt.localEdits {
				installed = strings.Replace(installed, "Local setting: original.", "Local setting: customized.", 1)
			}
			path := filepath.Join(dir, "triage.md")
			require.NoError(t, os.WriteFile(path, []byte(installed), 0o644))

			expectedSource := "owner/repo/" + upstreamPath + "@main"
			if tt.manifest {
				expectedSource = "owner/repo@main"
			}
			findInstalled := func(wantSHA string) *workflowWithSource {
				t.Helper()
				workflows, err := findWorkflowsWithSource(dir, nil, false)
				require.NoError(t, err)
				require.Len(t, workflows, 1)
				assert.Equal(t, expectedSource, workflows[0].SourceSpec)
				assert.Equal(t, wantSHA, workflows[0].SourceSHA)
				return workflows[0]
			}
			wf := findInstalled(oldSHA)
			parsed, err := parser.ExtractFrontmatterFromContent(installed)
			require.NoError(t, err)
			require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(parsed.Frontmatter, path))

			originalResolve := resolveLatestRefFn
			originalDownload := downloadWorkflowContentFn
			t.Cleanup(func() {
				resolveLatestRefFn = originalResolve
				downloadWorkflowContentFn = originalDownload
			})
			resolveLatestRefFn = func(_ context.Context, repo, currentRef string, _, _ bool, _ time.Duration) (latestRefResolution, error) {
				assert.Equal(t, "owner/repo", repo)
				assert.Equal(t, "main", currentRef, "updates must continue tracking the requested branch")
				return latestRefResolution{Ref: nextSHA}, nil
			}
			var requestedRefs []string
			downloadWorkflowContentFn = func(_ context.Context, repo, sourcePath, ref string, _ bool) ([]byte, error) {
				assert.Equal(t, "owner/repo", repo)
				assert.Equal(t, upstreamPath, sourcePath)
				requestedRefs = append(requestedRefs, ref)
				switch ref {
				case oldSHA:
					return []byte(oldContent), nil
				case nextSHA:
					return []byte(newContent), nil
				default:
					t.Errorf("workflow download must use a resolved SHA, not %q", ref)
					return nil, fmt.Errorf("unexpected workflow ref %s", ref)
				}
			}

			opts := UpdateWorkflowsOptions{NoCompile: true, WorkflowsDir: dir}
			update := func(wf *workflowWithSource) {
				t.Helper()
				if tt.manifest {
					require.NoError(t, updateManifestManagedWorkflow(context.Background(), manifestManagedWorkflowUpdate{
						wf:             wf,
						repo:           "owner/repo",
						currentPath:    upstreamPath,
						latestPath:     upstreamPath,
						currentRef:     "main",
						latestRef:      nextSHA,
						manifestSource: expectedSource,
					}, opts))
				} else {
					require.NoError(t, updateWorkflow(context.Background(), wf, opts))
				}
			}

			update(wf)
			assert.Contains(t, requestedRefs, oldSHA, "default merge mode must fetch the installed revision as its base")
			assert.Contains(t, requestedRefs, nextSHA)
			assert.NotContains(t, requestedRefs, "main")
			updated, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(updated), "# Updated upstream title")
			assert.NotContains(t, string(updated), "# Original upstream title")
			assert.NotContains(t, string(updated), "<<<<<<<")
			if tt.localEdits {
				assert.Contains(t, string(updated), "Local setting: customized.")
			} else {
				assert.Contains(t, string(updated), "Local setting: original.")
			}

			wf = findInstalled(nextSHA)
			requestedRefs = nil
			output := testutil.CaptureStderr(t, func() { update(wf) })
			if !tt.manifest || !tt.localEdits {
				assert.Contains(t, output, "already up to date")
			}
			assert.NotContains(t, requestedRefs, oldSHA, "a repeated update must use the newly installed revision")
			assert.NotContains(t, requestedRefs, "main")
			repeated, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(updated), string(repeated), "a repeated update must preserve the installed content")
			findInstalled(nextSHA)
		})
	}
}

func TestWorkflowSourceRevision_ReleaseDownloadUsesCommit(t *testing.T) {
	const commitSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	originalResolve := resolveLatestRefFn
	originalDownload := downloadWorkflowContentFn
	t.Cleanup(func() {
		resolveLatestRefFn = originalResolve
		downloadWorkflowContentFn = originalDownload
	})
	resolveLatestRefFn = func(context.Context, string, string, bool, bool, time.Duration) (latestRefResolution, error) {
		return latestRefResolution{Ref: "v1.2.0", CommitSHA: commitSHA}, nil
	}
	downloadWorkflowContentFn = func(_ context.Context, _, _, ref string, _ bool) ([]byte, error) {
		assert.Equal(t, commitSHA, ref)
		return []byte("---\non: workflow_dispatch\n---\n# Workflow\n"), nil
	}
	result, err := resolveRedirectedUpdateLocation(context.Background(), "triage",
		&SourceSpec{Repo: "owner/repo", Path: "workflows/triage.md", Ref: "v1"}, false, false, false, 0)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0", result.sourceFieldRef)
	assert.Equal(t, commitSHA, result.latestRef)
}

func TestWorkflowSourceRevision_PackageOwnershipSeparatesTrackingRef(t *testing.T) {
	const commitSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	dir := testutil.TempDir(t, "package-source-revision-*")
	const destination = ".github/workflows/triage.md"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(destination)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, destination), []byte("# Workflow\n"), 0o644))
	resolved := &ResolvedWorkflow{
		Spec: &WorkflowSpec{
			RepoSpec:               RepoSpec{RepoSlug: "owner/repo", Version: "main"},
			WorkflowPath:           "workflows/triage.md",
			WorkflowName:           "triage",
			FromRepositoryManifest: true,
		},
		SourceInfo: &FetchedWorkflow{CommitSHA: commitSHA},
	}
	source := packageSourceForSpec(resolved.Spec, resolved.SourceInfo)
	assert.Equal(t, "owner/repo@main", source)
	record, err := buildPackageOwnershipRecord(dir, source, []*ResolvedWorkflow{resolved}, AddOptions{})
	require.NoError(t, err)
	assert.Equal(t, source, record.Source)
	assert.Equal(t, commitSHA, record.ResolvedCommit)
}

func TestWorkflowSourceRevision_ManifestUnavailableBasePreservesInstalledFile(t *testing.T) {
	const oldSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const nextSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const upstreamPath = ".github/workflows/triage.md"
	dir := testutil.TempDir(t, "manifest-unavailable-source-revision-*")
	spec := &WorkflowSpec{
		RepoSpec:               RepoSpec{RepoSlug: "owner/repo", Version: "main"},
		WorkflowPath:           upstreamPath,
		FromRepositoryManifest: true,
	}
	installed, err := applySourceAndIncludeModifications(
		"---\non: workflow_dispatch\n---\n# Triage\n\nLocal instructions must be preserved.\n",
		spec, &FetchedWorkflow{CommitSHA: oldSHA}, dir, AddOptions{})
	require.NoError(t, err)
	path := filepath.Join(dir, "triage.md")
	require.NoError(t, os.WriteFile(path, []byte(installed), 0o644))
	workflows, err := findWorkflowsWithSource(dir, nil, false)
	require.NoError(t, err)
	require.Len(t, workflows, 1)
	require.Equal(t, oldSHA, workflows[0].SourceSHA)

	originalDownload := downloadWorkflowContentFn
	t.Cleanup(func() { downloadWorkflowContentFn = originalDownload })
	baseDownloadErr := errors.New("recorded source revision is unavailable")
	var requestedRefs []string
	downloadWorkflowContentFn = func(_ context.Context, repo, sourcePath, ref string, _ bool) ([]byte, error) {
		assert.Equal(t, "owner/repo", repo)
		assert.Equal(t, upstreamPath, sourcePath)
		requestedRefs = append(requestedRefs, ref)
		switch ref {
		case nextSHA:
			return []byte("---\non: workflow_dispatch\n---\n# Updated upstream workflow\n"), nil
		case oldSHA:
			return nil, baseDownloadErr
		default:
			t.Errorf("unexpected workflow ref %s", ref)
			return nil, fmt.Errorf("unexpected workflow ref %s", ref)
		}
	}

	err = updateManifestManagedWorkflow(context.Background(), manifestManagedWorkflowUpdate{
		wf:             workflows[0],
		repo:           "owner/repo",
		currentPath:    upstreamPath,
		latestPath:     upstreamPath,
		currentRef:     "main",
		latestRef:      nextSHA,
		manifestSource: "owner/repo@main",
	}, UpdateWorkflowsOptions{NoCompile: true, WorkflowsDir: dir})
	require.ErrorIs(t, err, baseDownloadErr)
	assert.Equal(t, []string{nextSHA, oldSHA}, requestedRefs)
	unchanged, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, installed, string(unchanged), "an unavailable recorded merge base must never cause an overwrite")
	workflows, err = findWorkflowsWithSource(dir, nil, false)
	require.NoError(t, err)
	require.Len(t, workflows, 1)
	assert.Equal(t, "owner/repo@main", workflows[0].SourceSpec)
	assert.Equal(t, oldSHA, workflows[0].SourceSHA, "failed updates must not advance installed revision metadata")
}

func TestWorkflowSourceRevision_RefreshManifestOwnershipSeparatesTrackingRef(t *testing.T) {
	const commitSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	dir := testutil.TempDir(t, "refresh-manifest-source-revision-*")
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	t.Chdir(dir)

	require.NoError(t, refreshManifestManagedOwnership(
		&RepoSpec{RepoSlug: "owner/repo", Version: "main"},
		&resolvedRepositoryPackage{ResolvedRef: commitSHA},
		"main", "copilot", UpdateWorkflowsOptions{},
	))
	record, err := readPackageOwnershipRecord(packageOwnershipRecordPath(dir, "owner/repo"))
	require.NoError(t, err)
	assert.Equal(t, "owner/repo@main", record.Source)
	assert.Equal(t, commitSHA, record.ResolvedCommit)
	assert.Empty(t, record.Files)
}
