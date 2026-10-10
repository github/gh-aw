//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunLocalActionlint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test binaries use a POSIX shell")
	}
	dir := t.TempDir()
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
	files := []string{filepath.Join(root, ".github/workflows/test space.lock.yml")}
	for _, tt := range []struct {
		name, exit, output, wantErr string
	}{
		{"clean", "0", "[]", ""},
		{"findings", "1", `[{"message":"invalid syntax","kind":"syntax-check","filepath":".github/workflows/test space.lock.yml","line":1,"column":1}]`, "strict mode: actionlint found 1 errors"},
		{"tooling failure", "2", "", "actionlint failed with exit code 2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ACTIONLINT_TEST_EXIT", tt.exit)
			t.Setenv("ACTIONLINT_TEST_OUTPUT", tt.output)
			var runErr error
			output := testutil.CaptureStderr(t, func() {
				runErr = runActionlintOnFilesWithOptions(context.Background(), files, true, true, actionlintRunOptions{})
			})
			if tt.wantErr == "" {
				require.NoError(t, runErr)
			} else {
				require.ErrorContains(t, runErr, tt.wantErr)
			}
			args, err := os.ReadFile(argsPath)
			require.NoError(t, err)
			want := root + "\n-format\n{{json .}}\n-shellcheck=\n-pyflakes=\n.github/workflows/test space.lock.yml\n"
			assert.Equal(t, want, string(args))
			assert.NotContains(t, output, "Run actionlint directly: docker")
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runErr := runActionlintOnFilesWithOptions(ctx, files, false, true, actionlintRunOptions{})
	require.ErrorContains(t, runErr, "actionlint was canceled")
}
