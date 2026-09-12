//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileManifestManagedAssets_AddsPackageOwnedAssets(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-assets-*")
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))
	t.Chdir(tmpDir)

	originalDownload := downloadPackageFileFromGitHubForHost
	t.Cleanup(func() { downloadPackageFileFromGitHubForHost = originalDownload })
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		if owner != "owner" || repo != "repo" || ref != "v2.0.0" {
			return nil, fmt.Errorf("unexpected package source %s/%s@%s", owner, repo, ref)
		}

		switch path {
		case ".github/workflows/new.yml":
			return []byte("name: new action\n"), nil
		case "skills/review/scripts/check.sh":
			return []byte("#!/bin/sh\n"), nil
		case "agents/reviewer.md":
			return []byte("# Reviewer\n"), nil
		default:
			return nil, fmt.Errorf("unexpected package path %s", path)
		}
	}

	latestPkg := &resolvedRepositoryPackage{
		ResolvedRef: "v2.0.0",
		InstallationSource: []resolvedPackageInstallable{{
			SourcePath:      ".github/workflows/new.yml",
			DestinationPath: ".github/workflows/new.yml",
		}},
		SkillFiles: []resolvedPackageSkillFile{{
			SourcePath: "skills/review/scripts/check.sh",
			SkillName:  "review",
		}},
		AgentFiles: []string{"agents/reviewer.md"},
	}
	err := reconcileManifestManagedAssets(context.Background(), &RepoSpec{RepoSlug: "owner/repo"},
		&resolvedRepositoryPackage{},
		latestPkg,
		"copilot",
		UpdateWorkflowsOptions{},
	)
	require.NoError(t, err)
	require.NoError(t, refreshManifestManagedOwnership(
		&RepoSpec{RepoSlug: "owner/repo"},
		latestPkg,
		"v2.0.0",
		"copilot",
		UpdateWorkflowsOptions{},
	))
	record, err := readPackageOwnershipRecord(packageOwnershipRecordPath(tmpDir, "owner/repo"))
	require.NoError(t, err)
	assert.Len(t, record.Files, 3)
	for _, entry := range record.Files {
		digest, digestErr := fileSHA256(filepath.Join(tmpDir, filepath.FromSlash(entry.Destination)))
		require.NoError(t, digestErr)
		assert.Equal(t, digest, entry.SHA256)
	}
	workflowPath := filepath.Join(tmpDir, ".github", "workflows", "new.yml")
	assert.FileExists(t, workflowPath)
	assert.FileExists(t, filepath.Join(tmpDir, workflow.GetEngineSkillDir("copilot"), "review", "scripts", "check.sh"))
	assert.FileExists(t, filepath.Join(tmpDir, workflow.GetEngineSubAgentDir("copilot"), "reviewer.md"))
	workflowContent, readErr := os.ReadFile(workflowPath)
	require.NoError(t, readErr)
	assert.Equal(t, "name: new action\n", string(workflowContent))
}

func TestReconcileManifestManagedAssets_UsesCustomWorkflowDirectory(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-assets-custom-dir-*")
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))
	t.Chdir(tmpDir)

	originalDownload := downloadPackageFileFromGitHubForHost
	t.Cleanup(func() { downloadPackageFileFromGitHubForHost = originalDownload })
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		return []byte("name: custom action\n"), nil
	}

	customDir := filepath.Join("custom", "workflows")
	err := reconcileManifestManagedAssets(context.Background(), &RepoSpec{RepoSlug: "owner/repo"},
		&resolvedRepositoryPackage{},
		&resolvedRepositoryPackage{
			ResolvedRef: "v2.0.0",
			InstallationSource: []resolvedPackageInstallable{{
				SourcePath:      ".github/workflows/new.yml",
				DestinationPath: ".github/workflows/new.yml",
			}},
		},
		"copilot",
		UpdateWorkflowsOptions{WorkflowsDir: customDir},
	)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(tmpDir, customDir, "new.yml"))
	assert.NoFileExists(t, filepath.Join(tmpDir, ".github", "workflows", "new.yml"))
}

func TestReconcileManifestManagedAssets_RollsBackPartialUpdate(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-assets-rollback-*")
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))
	t.Chdir(tmpDir)

	originalDownload := downloadPackageFileFromGitHubForHost
	t.Cleanup(func() { downloadPackageFileFromGitHubForHost = originalDownload })
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		if path == ".github/workflows/new.yml" {
			return []byte("name: new action\n"), nil
		}
		return nil, errors.New("download failed")
	}

	err := reconcileManifestManagedAssets(context.Background(), &RepoSpec{RepoSlug: "owner/repo"},
		&resolvedRepositoryPackage{},
		&resolvedRepositoryPackage{
			ResolvedRef: "v2.0.0",
			InstallationSource: []resolvedPackageInstallable{{
				SourcePath:      ".github/workflows/new.yml",
				DestinationPath: ".github/workflows/new.yml",
			}},
			SkillFiles: []resolvedPackageSkillFile{{
				SourcePath: "skills/review/SKILL.md",
				SkillName:  "review",
			}},
		},
		"copilot",
		UpdateWorkflowsOptions{},
	)
	require.ErrorContains(t, err, "download failed")
	assert.NoFileExists(t, filepath.Join(tmpDir, ".github", "workflows", "new.yml"))
}

func TestReconcileManifestManagedAssets_BranchTrackingInstallsMissingAssets(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-assets-branch-main-*")
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))
	t.Chdir(tmpDir)

	originalDownload := downloadPackageFileFromGitHubForHost
	t.Cleanup(func() { downloadPackageFileFromGitHubForHost = originalDownload })
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		if owner != "owner" || repo != "repo" || ref != "7d8e9f0" {
			return nil, fmt.Errorf("unexpected package source %s/%s@%s", owner, repo, ref)
		}
		switch path {
		case ".github/workflows/new.yml":
			return []byte("name: branch action\n"), nil
		case "skills/review/scripts/check.sh":
			return []byte("#!/bin/sh\n"), nil
		case "agents/reviewer.md":
			return []byte("# Reviewer\n"), nil
		default:
			return nil, fmt.Errorf("unexpected package path %s", path)
		}
	}

	currentAndLatest := &resolvedRepositoryPackage{
		ResolvedRef: "7d8e9f0",
		InstallationSource: []resolvedPackageInstallable{{
			SourcePath:      ".github/workflows/new.yml",
			DestinationPath: ".github/workflows/new.yml",
		}},
		SkillFiles: []resolvedPackageSkillFile{{
			SourcePath: "skills/review/scripts/check.sh",
			SkillName:  "review",
		}},
		AgentFiles: []string{"agents/reviewer.md"},
	}
	err := reconcileManifestManagedAssets(context.Background(), &RepoSpec{RepoSlug: "owner/repo"}, currentAndLatest, currentAndLatest, "copilot", UpdateWorkflowsOptions{})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(tmpDir, ".github", "workflows", "new.yml"))
	assert.FileExists(t, filepath.Join(tmpDir, workflow.GetEngineSkillDir("copilot"), "review", "scripts", "check.sh"))
	assert.FileExists(t, filepath.Join(tmpDir, workflow.GetEngineSubAgentDir("copilot"), "reviewer.md"))
}

func TestReconcileManifestManagedAssets_RefreshesExistingPackageOwnedAssets(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-assets-refresh-*")
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))
	t.Chdir(tmpDir)

	engine := "copilot"
	existingWorkflowPath := filepath.Join(tmpDir, ".github", "workflows", "new.yml")
	existingSkillPath := filepath.Join(tmpDir, workflow.GetEngineSkillDir(engine), "review", "scripts", "check.sh")
	existingAgentPath := filepath.Join(tmpDir, workflow.GetEngineSubAgentDir(engine), "reviewer.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(existingWorkflowPath), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(existingSkillPath), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(existingAgentPath), 0o755))
	require.NoError(t, os.WriteFile(existingWorkflowPath, []byte("name: old action\n"), 0o644))
	require.NoError(t, os.WriteFile(existingSkillPath, []byte("#!/bin/sh\necho old\n"), 0o644))
	require.NoError(t, os.WriteFile(existingAgentPath, []byte("# Old Reviewer\n"), 0o644))

	packageBase := "owner/repo"
	record := packageOwnershipRecord{
		SchemaVersion: packageOwnershipSchemaVersion,
		Package:       packageBase,
		Source:        packageBase + "@v1.0.0",
		Installer:     "gh-aw test",
		Files: []packageOwnershipFileEntry{
			{Source: ".github/workflows/new.yml", Destination: ".github/workflows/new.yml", SHA256: sha256Bytes([]byte("name: old action\n"))},
			{Source: "skills/review/scripts/check.sh", Destination: filepath.ToSlash(filepath.Join(workflow.GetEngineSkillDir(engine), "review", "scripts", "check.sh")), SHA256: sha256Bytes([]byte("#!/bin/sh\necho old\n"))},
			{Source: "agents/reviewer.md", Destination: filepath.ToSlash(filepath.Join(workflow.GetEngineSubAgentDir(engine), "reviewer.md")), SHA256: sha256Bytes([]byte("# Old Reviewer\n"))},
		},
	}
	recordPath := packageOwnershipRecordPath(tmpDir, packageBase)
	require.NoError(t, os.MkdirAll(filepath.Dir(recordPath), 0o755))
	recordData, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(recordPath, recordData, 0o644))

	originalDownload := downloadPackageFileFromGitHubForHost
	t.Cleanup(func() { downloadPackageFileFromGitHubForHost = originalDownload })
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		if owner != "owner" || repo != "repo" || ref != "v2.0.0" {
			return nil, fmt.Errorf("unexpected package source %s/%s@%s", owner, repo, ref)
		}
		switch path {
		case ".github/workflows/new.yml":
			return []byte("name: new action\n"), nil
		case "skills/review/scripts/check.sh":
			return []byte("#!/bin/sh\necho new\n"), nil
		case "agents/reviewer.md":
			return []byte("# New Reviewer\n"), nil
		default:
			return nil, fmt.Errorf("unexpected package path %s", path)
		}
	}

	err = reconcileManifestManagedAssets(context.Background(), &RepoSpec{RepoSlug: packageBase},
		&resolvedRepositoryPackage{},
		&resolvedRepositoryPackage{
			ResolvedRef: "v2.0.0",
			InstallationSource: []resolvedPackageInstallable{{
				SourcePath:      ".github/workflows/new.yml",
				DestinationPath: ".github/workflows/new.yml",
			}},
			SkillFiles: []resolvedPackageSkillFile{{
				SourcePath: "skills/review/scripts/check.sh",
				SkillName:  "review",
			}},
			AgentFiles: []string{"agents/reviewer.md"},
		},
		engine,
		UpdateWorkflowsOptions{},
	)
	require.NoError(t, err)
	latestPkg := &resolvedRepositoryPackage{
		ResolvedRef: "v2.0.0",
		InstallationSource: []resolvedPackageInstallable{{
			SourcePath:      ".github/workflows/new.yml",
			DestinationPath: ".github/workflows/new.yml",
		}},
		SkillFiles: []resolvedPackageSkillFile{{
			SourcePath: "skills/review/scripts/check.sh",
			SkillName:  "review",
		}},
		AgentFiles: []string{"agents/reviewer.md"},
	}
	require.NoError(t, refreshManifestManagedOwnership(&RepoSpec{RepoSlug: packageBase}, latestPkg, "v2.0.0", engine, UpdateWorkflowsOptions{}))

	workflowContent, readErr := os.ReadFile(existingWorkflowPath)
	require.NoError(t, readErr)
	assert.Equal(t, "name: new action\n", string(workflowContent))

	skillContent, readErr := os.ReadFile(existingSkillPath)
	require.NoError(t, readErr)
	assert.Equal(t, "#!/bin/sh\necho new\n", string(skillContent))

	agentContent, readErr := os.ReadFile(existingAgentPath)
	require.NoError(t, readErr)
	assert.Equal(t, "# New Reviewer\n", string(agentContent))

	updatedRecord, readErr := readPackageOwnershipRecord(recordPath)
	require.NoError(t, readErr)
	require.Len(t, updatedRecord.Files, 3)
	for _, entry := range updatedRecord.Files {
		digest, digestErr := fileSHA256(filepath.Join(tmpDir, filepath.FromSlash(entry.Destination)))
		require.NoError(t, digestErr)
		assert.Equal(t, digest, entry.SHA256)
	}
}

func TestReconcileManifestManagedAssets_RefusesToOverwriteUnownedAsset(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-assets-collision-*")
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))
	t.Chdir(tmpDir)

	existingWorkflowPath := filepath.Join(tmpDir, ".github", "workflows", "new.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(existingWorkflowPath), 0o755))
	require.NoError(t, os.WriteFile(existingWorkflowPath, []byte("name: unrelated workflow\n"), 0o644))

	originalDownload := downloadPackageFileFromGitHubForHost
	t.Cleanup(func() { downloadPackageFileFromGitHubForHost = originalDownload })
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		return []byte("name: new action\n"), nil
	}

	err := reconcileManifestManagedAssets(context.Background(), &RepoSpec{RepoSlug: "owner/repo"},
		&resolvedRepositoryPackage{},
		&resolvedRepositoryPackage{
			ResolvedRef: "v2.0.0",
			InstallationSource: []resolvedPackageInstallable{{
				SourcePath:      ".github/workflows/new.yml",
				DestinationPath: ".github/workflows/new.yml",
			}},
		},
		"copilot",
		UpdateWorkflowsOptions{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not tracked as owned")

	workflowContent, readErr := os.ReadFile(existingWorkflowPath)
	require.NoError(t, readErr)
	assert.Equal(t, "name: unrelated workflow\n", string(workflowContent), "unowned file must not be overwritten")
}

func TestReconcileManifestManagedAssets_WarnsWhenUpstreamRemovesSkillOrAgent(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-assets-removed-*")
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))
	t.Chdir(tmpDir)

	engine := "copilot"
	existingSkillPath := filepath.Join(tmpDir, workflow.GetEngineSkillDir(engine), "review", "scripts", "check.sh")
	existingAgentPath := filepath.Join(tmpDir, workflow.GetEngineSubAgentDir(engine), "reviewer.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(existingSkillPath), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(existingAgentPath), 0o755))
	require.NoError(t, os.WriteFile(existingSkillPath, []byte("#!/bin/sh\necho old\n"), 0o644))
	require.NoError(t, os.WriteFile(existingAgentPath, []byte("# Old Reviewer\n"), 0o644))

	originalDownload := downloadPackageFileFromGitHubForHost
	t.Cleanup(func() { downloadPackageFileFromGitHubForHost = originalDownload })
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		return nil, fmt.Errorf("unexpected download for removed package path %s", path)
	}

	currentPkg := &resolvedRepositoryPackage{
		ResolvedRef: "v1.0.0",
		SkillFiles: []resolvedPackageSkillFile{{
			SourcePath: "skills/review/scripts/check.sh",
			SkillName:  "review",
		}},
		AgentFiles: []string{"agents/reviewer.md"},
	}
	latestPkg := &resolvedRepositoryPackage{
		ResolvedRef: "v2.0.0",
	}

	var err error
	output := testutil.CaptureStderr(t, func() {
		err = reconcileManifestManagedAssets(context.Background(), &RepoSpec{RepoSlug: "owner/repo"}, currentPkg, latestPkg, engine, UpdateWorkflowsOptions{})
	})
	require.NoError(t, err)

	assert.Contains(t, output, "skills/review/scripts/check.sh")
	assert.Contains(t, output, "removed from the upstream package")
	assert.Contains(t, output, "agents/reviewer.md")

	skillContent, readErr := os.ReadFile(existingSkillPath)
	require.NoError(t, readErr)
	assert.Equal(t, "#!/bin/sh\necho old\n", string(skillContent), "removed-upstream skill must not be modified")

	agentContent, readErr := os.ReadFile(existingAgentPath)
	require.NoError(t, readErr)
	assert.Equal(t, "# Old Reviewer\n", string(agentContent), "removed-upstream agent must not be modified")
}

func TestResolveManifestAssetEngine(t *testing.T) {
	tmpDir := testutil.TempDir(t, "manifest-assets-engine-*")
	workflowPath := filepath.Join(tmpDir, "existing.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte("---\nengine: claude\nsource: owner/repo@main\n---\n\n# Existing\n"), 0o644))

	engine := resolveManifestAssetEngine([]*workflowWithSource{{Name: "existing", Path: workflowPath}}, UpdateWorkflowsOptions{})
	assert.Equal(t, "claude", engine)

	overrideEngine := resolveManifestAssetEngine([]*workflowWithSource{{Name: "existing", Path: workflowPath}}, UpdateWorkflowsOptions{EngineOverride: "copilot"})
	assert.Equal(t, "copilot", overrideEngine)
}

func TestUpdateManifestWorkflowGroup_AddsUpdatesRemoves(t *testing.T) {
	originalResolveLatestRef := resolveLatestRefFn
	originalDownloadPackage := downloadPackageFileFromGitHubForHost
	originalListPackage := listPackageWorkflowFilesForHost
	originalDefaultBranch := getRepositoryPackageDefaultBranch
	originalDownloadWorkflow := downloadWorkflowContentFn
	originalDownloadImport := downloadRemoteImportFile
	originalDirSubdirs := listPackageDirSubdirsForHost
	originalDirFiles := listPackageDirFilesForHost
	t.Cleanup(func() {
		resolveLatestRefFn = originalResolveLatestRef
		downloadPackageFileFromGitHubForHost = originalDownloadPackage
		listPackageWorkflowFilesForHost = originalListPackage
		getRepositoryPackageDefaultBranch = originalDefaultBranch
		downloadWorkflowContentFn = originalDownloadWorkflow
		downloadRemoteImportFile = originalDownloadImport
		listPackageDirSubdirsForHost = originalDirSubdirs
		listPackageDirFilesForHost = originalDirFiles
	})

	resolveLatestRefFn = func(ctx context.Context, repo, currentRef string, allowMajor, verbose bool, coolDown time.Duration) (latestRefResolution, error) {
		return latestRefResolution{Ref: "v2.0.0"}, nil
	}
	getRepositoryPackageDefaultBranch = func(_ context.Context, repoSlug, host string) (string, error) {
		return "main", nil
	}
	downloadPackageFileFromGitHubForHost = func(_ context.Context, owner, repo, path, ref, host string) ([]byte, error) {
		switch path {
		case "aw.yml":
			if ref == "v1.0.0" {
				return []byte("name: Test Package\nfiles:\n  - workflows/existing.md\n  - workflows/removed.md\n"), nil
			}
			if ref == "v2.0.0" {
				return []byte("name: Test Package\nfiles:\n  - workflows/existing.md\n  - workflows/new.md\n"), nil
			}
		case "README.md":
			return []byte("# Test Package\n"), nil
		}
		return nil, createRepositoryPackageNotFoundError(path)
	}
	listPackageWorkflowFilesForHost = func(_ context.Context, owner, repo, ref, workflowPath, host string) ([]string, error) {
		return nil, errors.New("unexpected scan")
	}
	// Return not-found so skill/agent auto-scan skips gracefully (no real network needed)
	listPackageDirSubdirsForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}
	listPackageDirFilesForHost = func(_ context.Context, owner, repo, ref, dirPath, host string) ([]string, error) {
		return nil, createRepositoryPackageNotFoundError(dirPath)
	}

	downloadWorkflowContentFn = func(_ context.Context, repo, path, ref string, _ bool) ([]byte, error) {
		if repo != "owner/repo" {
			return nil, fmt.Errorf("unexpected repo %s", repo)
		}
		switch path + "@" + ref {
		case "workflows/existing.md@v1.0.0":
			return []byte("---\non: push\n---\n\n# Existing old\n"), nil
		case "workflows/existing.md@v2.0.0":
			return []byte("---\non: push\nimports:\n  - shared/control.md\n---\n\n# Existing new\n"), nil
		case "workflows/new.md@v2.0.0":
			return []byte("---\non: push\nimports:\n  - shared/new-helper.md\n---\n\n# New workflow\n"), nil
		}
		return nil, fmt.Errorf("unexpected download %s@%s", path, ref)
	}
	downloadRemoteImportFile = func(_ context.Context, owner, repo, path, ref string) ([]byte, error) {
		if owner != "owner" || repo != "repo" || ref != "v2.0.0" {
			return nil, fmt.Errorf("unexpected import source %s/%s@%s", owner, repo, ref)
		}
		switch path {
		case "workflows/shared/control.md":
			return []byte("---\nimports:\n  - control-precompute.md\n---\n\n# Control v2\n"), nil
		case "workflows/shared/control-precompute.md":
			return []byte("# Control precompute v2\n"), nil
		case "workflows/shared/new-helper.md":
			return []byte("# New helper v2\n"), nil
		default:
			return nil, fmt.Errorf("unexpected import download %s", path)
		}
	}

	tmpDir := testutil.TempDir(t, "manifest-update-*")
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))
	t.Chdir(tmpDir)
	existingPath := filepath.Join(tmpDir, "existing.md")
	removedPath := filepath.Join(tmpDir, "removed.md")
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("create shared directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "control.md"), []byte("# Control v1\n"), 0o644); err != nil {
		t.Fatalf("write stale shared control: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "control-precompute.md"), []byte("# Control precompute v1\n"), 0o644); err != nil {
		t.Fatalf("write stale shared precompute: %v", err)
	}
	if err := os.WriteFile(existingPath, []byte("---\nsource: owner/repo@v1.0.0\n---\n\n# Existing old\n"), 0o644); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	if err := os.WriteFile(removedPath, []byte("---\nsource: owner/repo@v1.0.0\n---\n\n# Removed old\n"), 0o644); err != nil {
		t.Fatalf("write removed: %v", err)
	}

	successes, failures := updateManifestWorkflowGroup(context.Background(), "owner/repo@v1.0.0", []*workflowWithSource{
		{Name: "existing", Path: existingPath, SourceSpec: "owner/repo@v1.0.0"},
		{Name: "removed", Path: removedPath, SourceSpec: "owner/repo@v1.0.0"},
	}, UpdateWorkflowsOptions{
		NoMerge:                true,
		NoCompile:              true,
		DisableSecurityScanner: true,
	})
	if len(failures) > 0 {
		t.Fatalf("unexpected failures: %+v", failures)
	}
	if len(successes) != 3 {
		t.Fatalf("expected 3 successful operations, got %d", len(successes))
	}

	if _, err := os.Stat(removedPath); !os.IsNotExist(err) {
		t.Fatalf("expected removed workflow to be deleted, got err=%v", err)
	}
	updatedExisting, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read existing: %v", err)
	}
	if !strings.Contains(string(updatedExisting), "# Existing new") || !strings.Contains(string(updatedExisting), "source: owner/repo@v2.0.0") {
		t.Fatalf("existing workflow not updated as expected:\n%s", string(updatedExisting))
	}
	newPath := filepath.Join(tmpDir, "new.md")
	newContent, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new workflow not added: %v", err)
	}
	if !strings.Contains(string(newContent), "# New workflow") || !strings.Contains(string(newContent), "source: owner/repo@v2.0.0") {
		t.Fatalf("new workflow content unexpected:\n%s", string(newContent))
	}
	updatedControl, err := os.ReadFile(filepath.Join(sharedDir, "control.md"))
	if err != nil {
		t.Fatalf("read updated shared control: %v", err)
	}
	if !strings.Contains(string(updatedControl), "# Control v2") {
		t.Fatalf("shared control was not updated:\n%s", string(updatedControl))
	}
	updatedPrecompute, err := os.ReadFile(filepath.Join(sharedDir, "control-precompute.md"))
	if err != nil {
		t.Fatalf("read updated shared precompute: %v", err)
	}
	if !strings.Contains(string(updatedPrecompute), "# Control precompute v2") {
		t.Fatalf("transitive shared dependency was not updated:\n%s", string(updatedPrecompute))
	}
	newHelper, err := os.ReadFile(filepath.Join(sharedDir, "new-helper.md"))
	if err != nil {
		t.Fatalf("read new workflow dependency: %v", err)
	}
	if !strings.Contains(string(newHelper), "# New helper v2") {
		t.Fatalf("new workflow dependency was not installed:\n%s", string(newHelper))
	}

	downloadRemoteImportFile = func(_ context.Context, owner, repo, path, ref string) ([]byte, error) {
		t.Errorf("unexpected dependency fetch for unchanged workflow: %s/%s/%s@%s", owner, repo, path, ref)
		return nil, errors.New("unexpected dependency fetch")
	}
	if err := updateManifestManagedWorkflow(context.Background(), manifestManagedWorkflowUpdate{
		wf:             &workflowWithSource{Name: "existing", Path: existingPath},
		repo:           "owner/repo",
		currentPath:    "workflows/existing.md",
		latestPath:     "workflows/existing.md",
		currentRef:     "v2.0.0",
		latestRef:      "v2.0.0",
		manifestSource: "owner/repo@v2.0.0",
	}, UpdateWorkflowsOptions{
		NoMerge:                true,
		NoCompile:              true,
		DisableSecurityScanner: true,
	}); err != nil {
		t.Fatalf("update unchanged workflow: %v", err)
	}

	downloadRemoteImportFile = func(_ context.Context, owner, repo, path, ref string) ([]byte, error) {
		return nil, errors.New("dependency download failed")
	}
	err = updateManifestManagedWorkflow(context.Background(), manifestManagedWorkflowUpdate{
		wf:             &workflowWithSource{Name: "existing", Path: existingPath},
		repo:           "owner/repo",
		currentPath:    "workflows/existing.md",
		latestPath:     "workflows/existing.md",
		currentRef:     "v2.0.0",
		latestRef:      "v2.0.0",
		manifestSource: "owner/repo@v2.0.0",
	}, UpdateWorkflowsOptions{
		Force:                  true,
		NoMerge:                true,
		NoCompile:              true,
		DisableSecurityScanner: true,
	})
	if err == nil || !strings.Contains(err.Error(), "dependency download failed") {
		t.Fatalf("expected dependency refresh failure, got %v", err)
	}
	err = addManifestManagedWorkflow(context.Background(), tmpDir, "failing", "owner/repo", "workflows/new.md", "v2.0.0", "owner/repo@v2.0.0", UpdateWorkflowsOptions{
		NoCompile:              true,
		DisableSecurityScanner: true,
	})
	if err == nil || !strings.Contains(err.Error(), "dependency download failed") {
		t.Fatalf("expected dependency installation failure, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, "failing.md")); !os.IsNotExist(statErr) {
		t.Fatalf("failed workflow was written, got err=%v", statErr)
	}
}
