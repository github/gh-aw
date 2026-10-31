//go:build !integration && !windows

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAiderEntrypointContinuesAfterShellResults(t *testing.T) {
	dir := t.TempDir()
	packageDir := filepath.Join(dir, "aider")
	codersDir := filepath.Join(packageDir, "coders")
	require.NoError(t, os.MkdirAll(codersDir, 0o700))
	for _, name := range []string{"__init__.py", "coders/__init__.py"} {
		require.NoError(t, os.WriteFile(filepath.Join(packageDir, name), nil, 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "io.py"), []byte(`class InputOutput:
    def tool_error(self, message="", strip=True):
        print(message)
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(codersDir, "base_coder.py"), []byte(`import os
from types import SimpleNamespace

class Coder:
    max_reflections = 3
    gpt_prompts = SimpleNamespace(files_no_full_files="I am not sharing any files that you can edit yet.")
    file_context = "empty"

    def get_chat_files_messages(self):
        if self.file_context == "editable":
            content = "I have added these files to the chat: sample.go\npackage sample"
        elif self.file_context == "repo-map":
            content = "Ask me to add files from the repository map."
        else:
            content = self.gpt_prompts.files_no_full_files
        return [{"role": "user", "content": content}, {"role": "assistant", "content": "Ok."}]

    def show_send_output(self, completion):
        pass

    def calculate_and_show_tokens_and_cost(self, messages, completion=None):
        pass

    def run_shell_commands(self):
        if not self.shell_commands:
            return ""
        exit_code, output = run_cmd(self.shell_commands[0])
        if os.environ["AIDER_TEST_SCENARIO"] == "existing":
            self.reflected_message = "native retry"
        return output

    def run(self, with_message):
        self.num_reflections = 0
        self.cur_messages = []
        self.shell_commands = []
        self.aider_edited_files = set()
        if os.environ["AIDER_TEST_SCENARIO"] == "empty-context":
            context = self.get_chat_files_messages()[0]["content"]
            if context == self.gpt_prompts.files_no_full_files:
                self.io.assistant_output("Please provide the necessary files.")
                return
        while True:
            self.reflected_message = None
            context = self.get_chat_files_messages()
            self.show_send_output(SimpleNamespace(choices=[]))
            self.io.assistant_output("discovery" if self.num_reflections == 0 else "continuation")
            if self.num_reflections:
                if os.environ["AIDER_TEST_SCENARIO"] == "malformed":
                    assert with_message == "native edit error"
                elif os.environ["AIDER_TEST_SCENARIO"] == "existing":
                    assert with_message == "native retry"
                elif os.environ["AIDER_TEST_SCENARIO"] == "edit-only" and self.num_reflections == 2:
                    assert "applied edits" in with_message
                else:
                    assert "Exit code:" in with_message
                    assert str(1 if os.environ["AIDER_TEST_SCENARIO"] == "failed" else 0) in with_message
            self.shell_commands += ["discover"] if self.num_reflections == 0 else ["safeoutputs noop"]
            if os.environ["AIDER_TEST_SCENARIO"] == "edit-only" and self.num_reflections == 1:
                self.shell_commands = []
                self.aider_edited_files.add("sample.go")
            if os.environ["AIDER_TEST_SCENARIO"] == "no-shell":
                self.shell_commands = []
            if os.environ["AIDER_TEST_SCENARIO"] == "loop":
                self.shell_commands = self.shell_commands[:-1] + ["discover"]
            if os.environ["AIDER_TEST_SCENARIO"] in ("budget", "malformed", "extended-budget"):
                self.shell_commands = ["discover"]
                if self.num_reflections == self.max_reflections:
                    assert "This is your last available reflection." in context[-1]["content"]
                    assert "safeoutputs report_incomplete" in context[-1]["content"]
                    self.shell_commands = ["safeoutputs report_incomplete"]
            if os.environ["AIDER_TEST_SCENARIO"] == "malformed" and self.num_reflections < self.max_reflections:
                self.reflected_message = "native edit error"
                output = ""
            else:
                output = self.run_shell_commands()
            if output:
                self.cur_messages.extend([{"role": "user", "content": output}, {"role": "assistant", "content": "Ok"}])
            if os.environ["AIDER_TEST_SCENARIO"] == "edit-only" and self.num_reflections == 2:
                return
            if not self.reflected_message or self.num_reflections >= self.max_reflections:
                return
            self.num_reflections += 1
            with_message = self.reflected_message
            if self.num_reflections == 1 and os.environ["AIDER_TEST_SCENARIO"] == "output":
                assert self.cur_messages[0]["content"] == "discovered packages"
            if self.num_reflections == 2:
                self.shell_commands = []
                self.reflected_message = None
                if os.environ["AIDER_TEST_SCENARIO"] not in ("loop", "budget", "malformed", "edit-only", "extended-budget"):
                    return

def run_cmd(command, *args, **kwargs):
    if command == "safeoutputs report_incomplete":
        with open(os.environ["AIDER_TEST_TERMINAL"], "w") as record:
            record.write("report_incomplete")
        return 0, "incomplete recorded"
    if command == "safeoutputs noop":
        with open(os.environ["AIDER_TEST_TERMINAL"], "w") as record:
            record.write("noop")
        return 0, "safe output recorded"
    scenario = os.environ["AIDER_TEST_SCENARIO"]
    if scenario == "exception":
        raise OSError("discovery interrupted")
    return (1, "") if scenario == "failed" else (0, "discovered packages" if scenario == "output" else "")
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "main.py"), []byte(`import os
from .io import InputOutput
from .coders import base_coder

def main():
    coder = base_coder.Coder()
    coder.io = InputOutput()
    for context, expected in (
        ("editable", "I have added these files to the chat: sample.go\npackage sample"),
        ("repo-map", "Ask me to add files from the repository map."),
    ):
        coder.file_context = context
        assert coder.get_chat_files_messages() == [
            {"role": "user", "content": expected}, {"role": "assistant", "content": "Ok."},
        ]
    coder.file_context = "empty"
    messages = coder.get_chat_files_messages()
    assert "first run its repository discovery commands in fenced bash blocks" in messages[0]["content"]
    assert messages[1] == {"role": "assistant", "content": "Ok."}
    coder.run(with_message="perform workflow")
    limit = int(os.environ.get("GH_AW_AIDER_MAX_REFLECTIONS", "3"))
    assert coder.max_reflections == limit
    if os.environ["AIDER_TEST_SCENARIO"] in ("loop", "budget", "malformed", "extended-budget"):
        assert coder.num_reflections == limit
    return 0
`), 0o600))
	prompt := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(prompt, []byte("perform workflow"), 0o600))
	for _, scenario := range []string{"empty-context", "output", "empty", "failed", "existing", "no-shell", "loop", "budget", "malformed", "edit-only", "exception", "extended-budget", "invalid-budget", "invalid-budget-format"} {
		t.Run(scenario, func(t *testing.T) {
			terminal := filepath.Join(t.TempDir(), "terminal")
			cmd := exec.Command("python3", loadAiderSample(t).Behaviors.Execution.Args...)
			cmd.Env = append(os.Environ(), "PYTHONPATH="+dir, "GH_AW_PROMPT="+prompt,
				"AIDER_TEST_SCENARIO="+scenario, "AIDER_TEST_TERMINAL="+terminal)
			switch scenario {
			case "extended-budget":
				cmd.Env = append(cmd.Env, "GH_AW_AIDER_MAX_REFLECTIONS=8")
			case "invalid-budget":
				cmd.Env = append(cmd.Env, "GH_AW_AIDER_MAX_REFLECTIONS=9")
			case "invalid-budget-format":
				cmd.Env = append(cmd.Env, "GH_AW_AIDER_MAX_REFLECTIONS=invalid")
			default:
				cmd.Env = append(cmd.Env, "GH_AW_AIDER_MAX_REFLECTIONS=3")
			}
			output, err := cmd.CombinedOutput()
			if strings.HasPrefix(scenario, "invalid-budget") {
				require.Error(t, err, "%s", output)
				assert.Contains(t, string(output), "GH_AW_AIDER_MAX_REFLECTIONS must be an integer from 1 to 8")
			} else if scenario == "exception" {
				require.Error(t, err, "%s", output)
				assert.Contains(t, string(output), "discovery interrupted")
			} else {
				require.NoError(t, err, "%s", output)
			}
			record, readErr := os.ReadFile(terminal)
			switch scenario {
			case "no-shell", "loop", "exception", "invalid-budget", "invalid-budget-format":
				require.ErrorIs(t, readErr, os.ErrNotExist, "must not fabricate a terminal output")
			case "budget", "malformed":
				require.NoError(t, readErr, "%s", output)
				assert.Equal(t, "report_incomplete", string(record))
				assert.Contains(t, string(output), `"numTurns": 4`)
			case "extended-budget":
				require.NoError(t, readErr, "%s", output)
				assert.Equal(t, "report_incomplete", string(record))
				assert.Contains(t, string(output), `"numTurns": 9`)
			case "edit-only":
				require.NoError(t, readErr, "%s", output)
				assert.Equal(t, "noop", string(record))
				assert.Contains(t, string(output), `"numTurns": 3`)
			default:
				require.NoError(t, readErr, "must consume discovery results and execute the next model command: %s", output)
				assert.Equal(t, "noop", string(record))
				assert.Contains(t, string(output), `"numTurns": 2`)
			}
			if scenario == "output" || scenario == "edit-only" {
				assert.Equal(t, 1, strings.Count(string(output), `"command": "discover"`),
					"edit-only replies must not replay earlier discovery commands")
			}
		})
	}
}
