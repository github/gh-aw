//go:build !integration && !windows

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAiderEntrypointAcceptsOnlyShellConfirmations(t *testing.T) {
	execution := loadAiderSample(t).Behaviors.Execution
	require.Equal(t, "python3", execution.CommandName)
	require.GreaterOrEqual(t, len(execution.Args), 2)
	require.Equal(t, "-c", execution.Args[0])

	dir := t.TempDir()
	packageDir := filepath.Join(dir, "aider")
	require.NoError(t, os.Mkdir(packageDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "__init__.py"), nil, 0o600))
	ioSource := `class InputOutput:
    def confirm_ask(self, question, default="y", subject=None, explicit_yes_required=False, group=None, allow_never=False):
        return not explicit_yes_required

    def tool_error(self, message="", strip=True):
        print(message)
`
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "io.py"), []byte(ioSource), 0o600))
	codersDir := filepath.Join(packageDir, "coders")
	require.NoError(t, os.Mkdir(codersDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(codersDir, "__init__.py"), nil, 0o600))
	commandSource := `def run_cmd(command, *args, **kwargs):
    return (7, "command failed") if command == "fail" else (0, "command output")
`
	require.NoError(t, os.WriteFile(filepath.Join(codersDir, "base_coder.py"), []byte(commandSource), 0o600))
	mainSource := `import os
import sys
from .coders import base_coder
from .io import InputOutput

def main():
    io = InputOutput()
    for question in ("Run shell command?", "Run shell commands?"):
        assert io.confirm_ask(question, subject="safeoutputs noop", explicit_yes_required=True, allow_never=True)
    assert not io.confirm_ask("Install dependency?", explicit_yes_required=True)
    assert not io.confirm_ask("Run an unknown command?", explicit_yes_required=True)
    assert io.confirm_ask("Apply edit?", explicit_yes_required=False)
    assert "--yes-always" in sys.argv
    assert "--message-file" in sys.argv
    io.assistant_output("model reply")
    assert base_coder.run_cmd("test", cwd=os.getcwd()) == (0, "command output")
    assert base_coder.run_cmd("fail") == (7, "command failed")
    print("shell confirmations accepted; other explicit confirmations preserved")
    return int(os.environ.get("AIDER_TEST_EXIT_CODE", "0"))
`
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "main.py"), []byte(mainSource), 0o600))
	for _, exitCode := range []string{"0", "7"} {
		t.Run("exit "+exitCode, func(t *testing.T) {
			cmd := aiderHarnessCommand(t, t.TempDir(), execution.CommandName, execution.Args...)
			cmd.Env = append(cmd.Env, "PYTHONPATH="+dir, "AIDER_TEST_EXIT_CODE="+exitCode)
			output, err := cmd.CombinedOutput()
			if exitCode == "0" {
				require.NoError(t, err, "%s", output)
			} else {
				var exitError *exec.ExitError
				require.ErrorAs(t, err, &exitError, "%s", output)
				assert.Equal(t, 7, exitError.ExitCode())
			}
			assert.Contains(t, string(output), "shell confirmations accepted; other explicit confirmations preserved")
			assert.Contains(t, string(output), `"type": "user.message"`)
			assert.Contains(t, string(output), `"type": "assistant.message"`)
			assert.Contains(t, string(output), `"type": "tool.execution_start"`)
			assert.Contains(t, string(output), `"type": "tool.execution_complete"`)
			assert.Contains(t, string(output), `"success": false`)
			assert.Contains(t, string(output), `"numTurns": 1`)
		})
	}
}
