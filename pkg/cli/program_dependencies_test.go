//go:build !integration

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProgramDependencyResolvedRevision(t *testing.T) {
	dir := t.TempDir()
	old := downloadFileFromGitHubForHost
	t.Cleanup(func() { downloadFileFromGitHubForHost = old })
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	downloadFileFromGitHubForHost = func(_ context.Context, owner, repo, filePath, ref, host string) ([]byte, error) {
		require.Equal(t, "owner", owner)
		require.Equal(t, "repo", repo)
		require.Equal(t, "enterprise.example", host)
		require.Equal(t, sha, ref)
		require.Equal(t, "workflows/programs/run.ts", filePath)
		return []byte("export default () => {}"), nil
	}
	spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "main"}, WorkflowPath: "short.md", Host: "enterprise.example"}
	err := fetchWorkflowDependencies(context.Background(), spec, &FetchedWorkflow{CommitSHA: sha, SourcePath: "workflows/main.md"}, []byte("---\nengine: none\nprogram: programs/run.ts\n---\n"), dir, NewFileTracker(), AddOptions{})
	require.NoError(t, err)
	require.Equal(t, "main", spec.Version)
	require.FileExists(t, filepath.Join(dir, "programs", "run.ts"))
}

func TestProgramDependencyUpdate(t *testing.T) {
	oldResolve, oldWorkflow, oldDownload := resolveLatestRefFn, downloadWorkflowContentFn, downloadFileFromGitHubForHost
	t.Cleanup(func() {
		resolveLatestRefFn, downloadWorkflowContentFn, downloadFileFromGitHubForHost = oldResolve, oldWorkflow, oldDownload
	})
	const base = "---\non: workflow_dispatch\nengine: none\nprogram: run.js\n---\n"
	resolveLatestRefFn = func(context.Context, string, string, bool, bool, time.Duration) (latestRefResolution, error) {
		return latestRefResolution{Ref: "v2.0.0"}, nil
	}
	downloadWorkflowContentFn = func(_ context.Context, _, _, _ string, _ bool) ([]byte, error) {
		return []byte(base), nil
	}
	downloadFileFromGitHubForHost = func(_ context.Context, _, _, filePath, ref, _ string) ([]byte, error) {
		require.Equal(t, "workflows/run.js", filePath)
		switch ref {
		case "v1.0.0":
			return []byte("old"), nil
		case "v2.0.0":
			return []byte("new"), nil
		default:
			return nil, errors.New("unexpected ref")
		}
	}
	for _, tt := range []struct {
		name, local, want string
		noMerge, fail     bool
	}{
		{name: "unchanged", local: "old", want: "new"},
		{name: "local changes", local: "edited", want: "edited", fail: true},
		{name: "explicit overwrite", local: "edited", want: "new", noMerge: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			workflowPath := filepath.Join(dir, "example.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte("---\non: workflow_dispatch\nengine: none\nprogram: run.js\nsource: owner/repo/workflows/example.md@v1.0.0\n---\n"), 0600))
			programPath := filepath.Join(dir, "run.js")
			require.NoError(t, os.WriteFile(programPath, []byte(tt.local), 0600))
			err := updateWorkflow(context.Background(), &workflowWithSource{Name: "example", Path: workflowPath, SourceSpec: "owner/repo/workflows/example.md@v1.0.0"}, UpdateWorkflowsOptions{NoCompile: true, NoMerge: tt.noMerge})
			if tt.fail {
				require.ErrorContains(t, err, "local changes")
			} else {
				require.NoError(t, err)
			}
			got, readErr := os.ReadFile(programPath)
			require.NoError(t, readErr)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestProgramDependencyManifestUpdateConflict(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "example.md")
	const content = "---\non: workflow_dispatch\nengine: none\nprogram: run.js\n---\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run.js"), []byte("edited"), 0600))
	oldWorkflow, oldDownload := downloadWorkflowContentFn, downloadFileFromGitHubForHost
	t.Cleanup(func() { downloadWorkflowContentFn, downloadFileFromGitHubForHost = oldWorkflow, oldDownload })
	downloadWorkflowContentFn = func(context.Context, string, string, string, bool) ([]byte, error) {
		return []byte(content), nil
	}
	downloadFileFromGitHubForHost = func(_ context.Context, _, _, _, ref, _ string) ([]byte, error) {
		if ref == "v1" {
			return []byte("old"), nil
		}
		return []byte("new"), nil
	}
	err := updateManifestManagedWorkflow(context.Background(), manifestManagedWorkflowUpdate{
		wf: &workflowWithSource{Name: "example", Path: workflowPath}, repo: "owner/repo",
		currentPath: "workflows/example.md", latestPath: "workflows/example.md", currentRef: "v1", latestRef: "v2", manifestSource: "owner/repo@v2",
	}, UpdateWorkflowsOptions{NoCompile: true})
	require.ErrorContains(t, err, "local changes")
	got, err := os.ReadFile(filepath.Join(dir, "run.js"))
	require.NoError(t, err)
	require.Equal(t, "edited", string(got))
}

func TestProgramDependencyManifestInstallRollback(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "asset.js"), []byte("existing"), 0600))
	oldWorkflow, oldDownload := downloadWorkflowContentFn, downloadFileFromGitHubForHost
	t.Cleanup(func() { downloadWorkflowContentFn, downloadFileFromGitHubForHost = oldWorkflow, oldDownload })
	downloadWorkflowContentFn = func(context.Context, string, string, string, bool) ([]byte, error) {
		return []byte("---\non: workflow_dispatch\nengine: none\nprogram: run.js\nresources: [asset.js]\n---\n"), nil
	}
	downloadFileFromGitHubForHost = func(context.Context, string, string, string, string, string) ([]byte, error) {
		return []byte("program"), nil
	}
	err := addManifestManagedWorkflow(context.Background(), dir, "example", "owner/repo", "source/example.md", "sha", "owner/repo@sha", UpdateWorkflowsOptions{NoCompile: true})
	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(dir, "run.js"))
	require.NoFileExists(t, filepath.Join(dir, "example.md"))
	got, err := os.ReadFile(filepath.Join(dir, "asset.js"))
	require.NoError(t, err)
	require.Equal(t, "existing", string(got))
}

func TestProgramDependencyFrontmatterImportRollback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "helper.md")
	require.NoError(t, os.WriteFile(target, []byte("original"), 0600))
	old := downloadRemoteImportFile
	t.Cleanup(func() { downloadRemoteImportFile = old })
	downloadRemoteImportFile = func(_ context.Context, _, _, filePath, _ string) ([]byte, error) {
		if filePath == "source/helper.md" {
			return []byte("---\nimports: [missing.md]\n---\nupdated"), nil
		}
		return nil, errors.New("missing nested import")
	}
	tracker := NewFileTracker()
	spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "sha"}, WorkflowPath: "source/main.md"}
	err := fetchAndSaveRemoteFrontmatterImportsWithOptions(context.Background(), "---\nimports: [helper.md]\n---\n", spec, dir, false, true, tracker, true)
	require.ErrorContains(t, err, "missing nested import")
	require.NoError(t, tracker.RollbackAllFiles(false))
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "original", string(got))
}

func TestProgramDependencyUpdateSkipsNonProgramWorkers(t *testing.T) {
	dir := t.TempDir()
	workerPath := filepath.Join(dir, "worker.md")
	require.NoError(t, os.WriteFile(workerPath, []byte("local worker"), 0600))
	spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "new"}, WorkflowPath: "source/main.md"}
	download := func(context.Context, string, string, string, string) ([]byte, error) {
		return []byte("---\non: workflow_dispatch\nengine: copilot\n---\nupstream worker\n"), nil
	}
	ctx := withProgramWorkersOnly(context.Background())
	err := fetchAndSaveRemoteDispatchWorkflows(ctx, "---\nsafe-outputs:\n  dispatch-workflow:\n    workflows: [worker]\n---\n", spec, dir, false, true, nil, download)
	require.NoError(t, err)
	got, err := os.ReadFile(workerPath)
	require.NoError(t, err)
	require.Equal(t, "local worker", string(got))
}

func TestProgramDependencyUpdateRecompilesWorkerLock(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.md")
	workerPath := filepath.Join(dir, "worker.md")
	lockPath := filepath.Join(dir, "worker.lock.yml")
	require.NoError(t, os.WriteFile(mainPath, []byte("---\non: workflow_dispatch\nsafe-outputs:\n  dispatch-workflow:\n    workflows: [worker]\n---\n# Main\n"), 0600))
	require.NoError(t, os.WriteFile(workerPath, []byte("---\non: workflow_dispatch\nengine: none\nprogram: missing.js\n---\n"), 0600))
	require.NoError(t, os.WriteFile(lockPath, []byte("old lock"), 0600))
	tracker := NewFileTracker()
	tracker.TrackModified(workerPath)
	err := compileUpdatedProgramWorkers(context.Background(), mainPath, tracker, UpdateWorkflowsOptions{})
	require.Error(t, err, "missing program must cause the worker recompilation to fail")
	require.Contains(t, tracker.OriginalContent, lockPath)
	require.NoError(t, tracker.RollbackAllFiles(false))
	got, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	require.Equal(t, "old lock", string(got))
}

func TestProgramDependencyWorkflowDownloadUsesResolvedRevision(t *testing.T) {
	oldResolve, oldDownload := resolveRefToSHAForHost, downloadFileFromGitHubForHost
	t.Cleanup(func() {
		resolveRefToSHAForHost, downloadFileFromGitHubForHost = oldResolve, oldDownload
	})
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	resolveRefToSHAForHost = func(_ context.Context, _, _, ref, _ string) (string, error) {
		require.Equal(t, "main", ref)
		return sha, nil
	}
	downloadFileFromGitHubForHost = func(_ context.Context, _, _, filePath, ref, _ string) ([]byte, error) {
		require.Equal(t, sha, ref)
		if filePath == "main.md" {
			return nil, errors.New("not found")
		}
		require.Equal(t, "workflows/main.md", filePath)
		return []byte("---\nprogram: run.js\n---\n"), nil
	}
	result, err := fetchRemoteWorkflow(context.Background(), &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "main"}, WorkflowPath: "main.md"}, false)
	require.NoError(t, err)
	require.Equal(t, sha, result.CommitSHA)
	require.Equal(t, "workflows/main.md", result.SourcePath)
}

func TestProgramDependencyLocal(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(source, "scripts"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "scripts", "run.ts"), []byte("code"), 0600))
	require.NoError(t, copyLocalWorkflowProgram("---\nprogram: scripts/run.ts\n---\n", source, destination, false, nil))
	require.FileExists(t, filepath.Join(destination, "scripts", "run.ts"))
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "escape.ts"), []byte("code"), 0600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "escape.ts"), filepath.Join(source, "escape.ts")))
	require.Error(t, copyLocalWorkflowProgram("---\nprogram: escape.ts\n---\n", source, destination, false, nil))
}

func TestProgramDependencyWorkers(t *testing.T) {
	for _, workerType := range []string{"dispatch-workflow", "call-workflow"} {
		t.Run(workerType, func(t *testing.T) {
			dir := t.TempDir()
			spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "resolved-sha"}, WorkflowPath: "source/main.md"}
			paths := []string{}
			download := func(_ context.Context, _, _, filePath, ref string) ([]byte, error) {
				require.Equal(t, "resolved-sha", ref)
				paths = append(paths, filePath)
				switch filePath {
				case "source/nested/worker.md":
					return []byte("---\nengine: none\nprogram: scripts/job.js\n---\n"), nil
				case "source/nested/scripts/job.js":
					return []byte("export default () => {}"), nil
				default:
					return nil, errors.New("unexpected file")
				}
			}
			content := "---\nsafe-outputs:\n  " + workerType + ":\n    workflows: [nested/worker]\n---\n"
			var err error
			if workerType == "dispatch-workflow" {
				err = fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, dir, false, false, nil, download)
			} else {
				err = fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, dir, false, false, nil, download)
			}
			require.NoError(t, err)
			require.Equal(t, []string{"source/nested/worker.md", "source/nested/scripts/job.js"}, paths)
			require.FileExists(t, filepath.Join(dir, "nested", "scripts", "job.js"))
		})
	}
}

func TestProgramDependencyImportedWorkers(t *testing.T) {
	for _, workerType := range []string{"dispatch-workflow", "call-workflow"} {
		t.Run(workerType, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\nsafe-outputs:\n  "+workerType+":\n    workflows: [worker]\n---\n"), 0600))
			mainPath := filepath.Join(dir, "main.md")
			require.NoError(t, os.WriteFile(mainPath, []byte("---\non: workflow_dispatch\nengine: copilot\nimports: [shared.md]\n---\n# Main\n"), 0600))
			old := downloadFileFromGitHubForHost
			t.Cleanup(func() { downloadFileFromGitHubForHost = old })
			paths := []string{}
			downloadFileFromGitHubForHost = func(_ context.Context, _, _, filePath, ref, host string) ([]byte, error) {
				require.Equal(t, "sha", ref)
				require.Equal(t, "enterprise.example", host)
				paths = append(paths, filePath)
				switch filePath {
				case "source/worker.md":
					return []byte("---\non: workflow_dispatch\nengine: none\nprogram: run.js\n---\n"), nil
				case "source/run.js":
					return []byte("export default () => {}"), nil
				default:
					return nil, errors.New("unexpected file")
				}
			}
			spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "sha"}, WorkflowPath: "source/main.md", Host: "enterprise.example"}
			var err error
			if workerType == "dispatch-workflow" {
				err = fetchAndSaveDispatchWorkflowsFromParsedFile(context.Background(), mainPath, spec, dir, false, false, nil)
			} else {
				err = fetchAndSaveCallWorkflowsFromParsedFile(context.Background(), mainPath, spec, dir, false, false, nil)
			}
			require.NoError(t, err)
			require.Equal(t, []string{"source/worker.md", "source/run.js"}, paths)
			require.FileExists(t, filepath.Join(dir, "run.js"))
		})
	}
}

func TestProgramDependencyWorkerFailure(t *testing.T) {
	dir := t.TempDir()
	spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "sha"}, WorkflowPath: "source/main.md"}
	download := func(_ context.Context, _, _, filePath, _ string) ([]byte, error) {
		if filePath == "source/worker.md" {
			return []byte("---\nprogram: run.js\n---\n"), nil
		}
		return nil, errors.New("missing program")
	}
	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), "---\nsafe-outputs:\n  dispatch-workflow:\n    workflows: [worker]\n---\n", spec, dir, false, false, nil, download)
	require.ErrorContains(t, err, "missing program")
	require.NoFileExists(t, filepath.Join(dir, "worker.md"))
}

func TestProgramDependencyWorkerUpdateConflict(t *testing.T) {
	dir := t.TempDir()
	programPath := filepath.Join(dir, "run.js")
	require.NoError(t, os.WriteFile(programPath, []byte("edited"), 0600))
	spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "new"}, WorkflowPath: "source/main.md"}
	download := func(_ context.Context, _, _, filePath, ref string) ([]byte, error) {
		if filePath == "source/worker.md" {
			return []byte("---\nprogram: run.js\n---\n"), nil
		}
		if ref == "old" {
			return []byte("old"), nil
		}
		return []byte("new"), nil
	}
	ctx := withProgramUpdateBaseline(context.Background(), "owner/repo", "source/main.md", "source/main.md", "old", false)
	err := fetchAndSaveRemoteCallWorkflows(ctx, "---\nsafe-outputs:\n  call-workflow:\n    workflows: [worker]\n---\n", spec, dir, false, true, nil, download)
	require.ErrorContains(t, err, "local changes")
	require.NoFileExists(t, filepath.Join(dir, "worker.md"))
	got, err := os.ReadFile(programPath)
	require.NoError(t, err)
	require.Equal(t, "edited", string(got))
}

func TestProgramDependencyWorkerMarkdownConflict(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "worker.md")
	require.NoError(t, os.WriteFile(target, []byte("---\non: workflow_dispatch\n---\nlocal edit\n"), 0600))
	ctx := withProgramUpdateBaseline(context.Background(), "owner/repo", "source/main.md", "source/main.md", "old", false)
	err := validateWorkerUpdate(ctx, dir, target, "source/worker.md", []byte("---\nprogram: run.js\n---\n"), func(_ context.Context, _, _, filePath, ref string) ([]byte, error) {
		require.Equal(t, "source/worker.md", filePath)
		require.Equal(t, "old", ref)
		return []byte("---\non: workflow_dispatch\n---\noriginal\n"), nil
	})
	require.ErrorContains(t, err, "local changes")
}

func TestProgramDependencyImportedWorkerRollback(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.md")
	require.NoError(t, os.WriteFile(mainPath, []byte("---\non: workflow_dispatch\nsafe-outputs:\n  dispatch-workflow:\n    workflows: [first, second]\n---\n# Main\n"), 0600))
	workerPath := filepath.Join(dir, "first.md")
	require.NoError(t, os.WriteFile(workerPath, []byte("original worker"), 0600))
	old := downloadFileFromGitHubForHost
	t.Cleanup(func() { downloadFileFromGitHubForHost = old })
	downloadFileFromGitHubForHost = func(_ context.Context, _, _, filePath, _, _ string) ([]byte, error) {
		switch filePath {
		case "source/first.md":
			return []byte("---\nprogram: first.js\n---\n"), nil
		case "source/first.js":
			return []byte("first"), nil
		case "source/second.md":
			return []byte("---\nprogram: second.js\n---\n"), nil
		default:
			return nil, errors.New("missing program")
		}
	}
	spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "sha"}, WorkflowPath: "source/main.md"}
	tracker := NewFileTracker()
	err := fetchAndSaveDispatchWorkflowsFromParsedFile(context.Background(), mainPath, spec, dir, false, true, tracker)
	require.ErrorContains(t, err, "missing program")
	require.NoError(t, tracker.RollbackAllFiles(false))
	got, err := os.ReadFile(workerPath)
	require.NoError(t, err)
	require.Equal(t, "original worker", string(got))
	require.NoFileExists(t, filepath.Join(dir, "first.js"))
}

func TestProgramDependencyValidationAndConflicts(t *testing.T) {
	spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "sha"}, WorkflowPath: "source/main.md"}
	for _, program := range []string{"../outside.js", "/absolute.js", ".hidden/x.js", "x.yaml", "${{ inputs.path }}", `dir\file.js`} {
		t.Run(program, func(t *testing.T) {
			dir := t.TempDir()
			called := false
			err := fetchWorkflowProgram(context.Background(), "---\nprogram: '"+program+"'\n---\n", spec, spec.WorkflowPath, dir, "main.md", false, nil, func(context.Context, string, string, string, string) ([]byte, error) {
				called = true
				return nil, nil
			})
			require.Error(t, err)
			require.False(t, called)
		})
	}
	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		outside := t.TempDir()
		require.NoError(t, os.Symlink(outside, filepath.Join(dir, "scripts")))
		require.ErrorContains(t, writeWorkflowProgram(dir, "scripts/run.js", []byte("evil"), true, nil), "symlink")
		require.NoFileExists(t, filepath.Join(outside, "run.js"))
	})
	t.Run("force and tracking", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "run.js")
		require.NoError(t, os.WriteFile(file, []byte("original"), 0600))
		tracker := NewFileTracker()
		require.ErrorContains(t, writeWorkflowProgram(dir, "run.js", []byte("new"), false, tracker), "--force")
		require.NoError(t, writeWorkflowProgram(dir, "run.js", []byte("new"), true, tracker))
		require.Equal(t, []byte("original"), tracker.OriginalContent[file])
		require.NoError(t, tracker.RollbackAllFiles(false))
		got, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, "original", string(got))
	})
	t.Run("rollback newly created then overwritten", func(t *testing.T) {
		dir := t.TempDir()
		tracker := NewFileTracker()
		require.NoError(t, writeWorkflowProgram(dir, "run.js", []byte("first"), false, tracker))
		require.NoError(t, writeWorkflowProgram(dir, "run.js", []byte("second"), true, tracker))
		require.NoError(t, tracker.RollbackAllFiles(false))
		require.NoFileExists(t, filepath.Join(dir, "run.js"))
	})
	t.Run("missing remote file", func(t *testing.T) {
		dir := t.TempDir()
		err := fetchWorkflowProgram(context.Background(), "---\nprogram: run.js\n---\n", spec, spec.WorkflowPath, dir, "main.md", false, nil, func(context.Context, string, string, string, string) ([]byte, error) {
			return nil, errors.New("not found")
		})
		require.ErrorContains(t, err, "not found")
		require.NoFileExists(t, filepath.Join(dir, "run.js"))
	})
}
