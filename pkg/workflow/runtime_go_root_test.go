//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoRootCaptureReplacesStaleToolchainRoot(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"selected", "query-failure", "missing-binary"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			root := filepath.Join(dir, "go toolchain", "1.27.4")
			require.NoError(t, os.MkdirAll(bin, 0o755))
			require.NoError(t, os.MkdirAll(filepath.Join(root, "bin"), 0o755))
			mock := `#!/bin/sh
[ -z "${GOROOT:-}" ] && [ "$GOTOOLCHAIN" = local ] || exit 42
if [ "$SCENARIO" = query-failure ]; then exit 1; fi
printf '%s\n' "$SELECTED_GO_ROOT"
`
			require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(mock), 0o755))
			if scenario != "missing-binary" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "bin", "go"), []byte(mock), 0o755))
			}
			envFile := filepath.Join(dir, "github-env")
			step := generateGoRootCaptureStep()
			require.Contains(t, strings.Join(step, "\n"), "shell: bash")
			var script strings.Builder
			for _, line := range step[3:] {
				script.WriteString(strings.TrimPrefix(line, "          ") + "\n")
			}
			cmd := exec.Command("bash", "-c", script.String())
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"),
				"GOROOT="+filepath.Join(dir, "stale-go"), "GOTOOLCHAIN=auto",
				"SELECTED_GO_ROOT="+root, "SCENARIO="+scenario, "GITHUB_ENV="+envFile)
			output, err := cmd.CombinedOutput()
			if scenario == "selected" {
				require.NoError(t, err, "%s", output)
				exported, err := os.ReadFile(envFile)
				require.NoError(t, err)
				require.Equal(t, "GOROOT="+root+"\n", string(exported))
			} else {
				require.Error(t, err, "%s", output)
				require.NoFileExists(t, envFile, "failed discovery must not export an empty or stale root")
			}
		})
	}
}
