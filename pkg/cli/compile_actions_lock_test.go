//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileWorkflowsActionsLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh is not supported on Windows")
	}

	for _, tt := range []struct {
		name         string
		config       CompileConfig
		invalid      bool
		extensionErr bool
		wantCall     bool
		wantErr      string
	}{
		{name: "disabled"},
		{name: "directory", config: CompileConfig{ActionsLock: true}, wantCall: true},
		{name: "specific file", config: CompileConfig{ActionsLock: true, MarkdownFiles: []string{"test"}}, wantCall: true},
		{name: "JSON output", config: CompileConfig{ActionsLock: true, JSONOutput: true}, wantCall: true},
		{name: "compilation failure", config: CompileConfig{ActionsLock: true}, invalid: true, wantErr: "failed"},
		{name: "missing extension", config: CompileConfig{ActionsLock: true}, extensionErr: true, wantCall: true, wantErr: "gh extension install github/gh-actions-lock"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, initTestGitRepo(root))
			t.Chdir(root)
			workflowsDir := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
			source := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
---
Test workflow.
`
			if tt.invalid {
				source = "---\non: [\n---\nInvalid workflow.\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "test.md"), []byte(source), 0o644))

			binDir := t.TempDir()
			t.Setenv("GH_AW_ACTIONS_LOCK_TEST_LOG", filepath.Join(root, "gh-args.log"))
			script := `#!/bin/sh
case "$1" in
  actions-lock|extension) ;;
  *) exit 1 ;;
esac
printf '%s\n' "$*" >> "$GH_AW_ACTIONS_LOCK_TEST_LOG"
test "$1" = actions-lock || exit 98
test "$2" = --no-narrow && test "$#" = 2 || exit 97
test -f .github/workflows/test.lock.yml || exit 96
printf '%s\n' 'extension stdout'
`
			if tt.extensionErr {
				script += "printf '%s\\n' \"gh: unknown command 'actions-lock'\" >&2\nexit 1\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0o755))
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			stdout, err := os.Create(filepath.Join(root, "stdout"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stdout.Close()) })
			originalStdout := os.Stdout
			os.Stdout = stdout
			t.Cleanup(func() { os.Stdout = originalStdout })

			_, err = CompileWorkflows(context.Background(), tt.config)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			args, readErr := os.ReadFile(filepath.Join(root, "gh-args.log"))
			if tt.wantCall {
				require.NoError(t, readErr)
				require.Equal(t, "actions-lock --no-narrow\n", string(args))
			} else {
				require.True(t, os.IsNotExist(readErr), "extension must not run: %s", args)
			}
			if tt.config.JSONOutput {
				output, err := os.ReadFile(stdout.Name())
				require.NoError(t, err)
				var results []ValidationResult
				require.NoError(t, json.Unmarshal(output, &results), "stdout must contain only compiler JSON")
				require.Len(t, results, 1)
				require.True(t, results[0].Valid)
			}
		})
	}
}

func TestCompileActionsLockRejectsIncompatibleModes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config CompileConfig
	}{
		{name: "--no-emit", config: CompileConfig{ActionsLock: true, NoEmit: true}},
		{name: "--read-only", config: CompileConfig{ActionsLock: true, ReadOnly: true}},
		{name: "--watch", config: CompileConfig{ActionsLock: true, Watch: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCompileConfig(tt.config)
			require.ErrorContains(t, err, "--actions-lock cannot be used with")
			require.ErrorContains(t, err, tt.name)
		})
	}
}

func TestRunCompileActionsLockUsesRepositoryRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh is not supported on Windows")
	}
	root := t.TempDir()
	require.NoError(t, initTestGitRepo(root))
	subdir := filepath.Join(root, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0o755))
	t.Chdir(subdir)

	binDir := t.TempDir()
	t.Setenv("GH_AW_ACTIONS_LOCK_TEST_LOG", filepath.Join(root, "cwd"))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte("#!/bin/sh\npwd > \"$GH_AW_ACTIONS_LOCK_TEST_LOG\"\n"), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	require.NoError(t, runCompileActionsLock(context.Background()))
	cwd, err := os.ReadFile(filepath.Join(root, "cwd"))
	require.NoError(t, err)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, resolvedRoot, strings.TrimSpace(string(cwd)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, runCompileActionsLock(ctx), context.Canceled)
}
