//go:build !integration

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func actionlintTestDir(t *testing.T) string {
	t.Helper()
	root, err := gitutil.FindGitRoot()
	require.NoError(t, err)
	dir, err := os.MkdirTemp(root, ".actionlint-test-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	resolvedDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return resolvedDir
}

func TestRunLocalActionlint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test binaries use a POSIX shell")
	}
	dir := actionlintTestDir(t)
	script := `#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = "--version" ]; then
  printf '1.7.12\n'
  exit 0
fi
printf '%s\n' "$PWD" "$@" > "$ACTIONLINT_TEST_ARGS"
printf '%s\n' "$ACTIONLINT_TEST_OUTPUT"
exit "$ACTIONLINT_TEST_EXIT"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "actionlint"), []byte(script), 0o755))
	t.Setenv("PATH", dir)
	argsPath := filepath.Join(dir, "args.txt")
	t.Setenv("ACTIONLINT_TEST_ARGS", argsPath)
	root, err := gitutil.FindGitRoot()
	require.NoError(t, err)
	originalVersion, originalStats := actionlintVersion, actionlintStats
	actionlintVersion, actionlintStats = "", nil
	t.Cleanup(func() { actionlintVersion, actionlintStats = originalVersion, originalStats })
	files := []string{filepath.Join(dir, "test space.lock.yml")}
	require.NoError(t, os.WriteFile(files[0], []byte("on: push\njobs: {}\n"), 0o600))
	relPath, err := filepath.Rel(root, files[0])
	require.NoError(t, err)
	for _, tt := range []struct {
		name, exit, output, wantErr string
		defaultIntegrations         bool
	}{
		{"clean", "0", "[]", "", false},
		{"default integrations", "0", "[]", "", true},
		{"findings", "1", fmt.Sprintf(`[{"message":"invalid syntax","kind":"syntax-check","filepath":%q,"line":1,"column":1}]`, relPath), "strict mode: actionlint found 1 errors", false},
		{"tooling failure", "2", "", "actionlint failed with exit code 2", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ACTIONLINT_TEST_EXIT", tt.exit)
			t.Setenv("ACTIONLINT_TEST_OUTPUT", tt.output)
			var runErr error
			output := testutil.CaptureStderr(t, func() {
				if tt.defaultIntegrations {
					runErr = runActionlintOnFiles(context.Background(), files, true, true)
				} else {
					runErr = runActionlintOnFilesWithOptions(context.Background(), files, true, true, actionlintRunOptions{})
				}
			})
			if tt.wantErr == "" {
				require.NoError(t, runErr)
			} else {
				require.ErrorContains(t, runErr, tt.wantErr)
			}
			args, err := os.ReadFile(argsPath)
			require.NoError(t, err)
			wantArgs := []string{"-format", "{{json .}}"}
			if tt.defaultIntegrations {
				for _, pattern := range defaultGhAwActionlintIgnorePatterns {
					wantArgs = append(wantArgs, "-ignore", pattern)
				}
			} else {
				wantArgs = append(wantArgs, "-shellcheck=", "-pyflakes=")
			}
			want := root + "\n" + strings.Join(append(wantArgs, relPath), "\n") + "\n"
			assert.Equal(t, want, string(args))
			assert.NotContains(t, output, "Run actionlint directly: docker")
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runErr := runActionlintOnFilesWithOptions(ctx, files, false, true, actionlintRunOptions{})
	require.ErrorContains(t, runErr, "actionlint was canceled")
}
