//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNpmBinPathSetupNestedShell(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "node tools", "bin")
	require.NoError(t, os.MkdirAll(bin, 0700))
	script := GetNpmBinPathSetup() + `; printf "%s" "$PATH"`
	command := exec.Command("bash", "-c", WrapCommandInShell("bash -c "+shellEscapeArg(script)))
	command.Env = []string{"PATH=/usr/bin:/bin", "RUNNER_TOOL_CACHE=" + dir, "GOROOT=", "ERLANG_HOME="}
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Equal(t, "/usr/bin:/bin:"+bin, string(output))
}
