//go:build !integration && !windows

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAiderSmokeRequiresRepositoryEvidence(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/smoke-aider.md")
	require.NoError(t, err)
	var frontmatter struct {
		PostSteps []struct {
			Run string
		} `yaml:"post-steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(strings.SplitN(string(content), "---", 3)[1]), &frontmatter))
	require.Len(t, frontmatter.PostSteps, 1)
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	for _, test := range []struct {
		name, command, output string
		success               bool
		wantError             bool
	}{
		{"verified git log", "git log --oneline -1", "abc1234 Commit subject\n", true, false},
		{"claim without git log", "safeoutputs create_issue --body PASS", `{"result":"success"}`, true, true},
		{"git log without commit", "git log --oneline -1", "", true, true},
		{"failed git log", "git log --oneline -1", "abc1234 Commit subject\n", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runAiderSmokeEvidenceCheck(t, actionsDir, frontmatter.PostSteps[0].Run, test.command, test.output, test.success, test.wantError)
		})
	}
}

func runAiderSmokeEvidenceCheck(t *testing.T, actionsDir, script, command, output string, success, wantError bool) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "gh-aw", "actions"), 0o700))
	require.NoError(t, os.Symlink(filepath.Join(actionsDir, "log_parser_shared.cjs"), filepath.Join(dir, "gh-aw", "actions", "log_parser_shared.cjs")))
	filePath := filepath.Join(dir, "smoke.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("Smoke test passed for Aider\n"), 0o600))
	logPath := filepath.Join(dir, "agent-stdio.log")
	events := []map[string]any{
		{"type": "tool.execution_start", "data": map[string]any{"sourceEngine": "aider", "toolCallId": "git", "input": map[string]any{"command": command}}},
		{"type": "tool.execution_complete", "data": map[string]any{"sourceEngine": "aider", "toolCallId": "git", "success": success, "output": output}},
	}
	var log strings.Builder
	for _, event := range events {
		line, err := json.Marshal(event)
		require.NoError(t, err)
		log.Write(line)
		log.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(logPath, []byte(log.String()), 0o600))
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "RUNNER_TEMP="+dir, "SMOKE_FILE_PATH="+filePath, "SMOKE_STDIO_PATH="+logPath)
	result, err := cmd.CombinedOutput()
	if wantError {
		require.Error(t, err)
		assert.Contains(t, string(result), "Aider did not verify repository access")
	} else {
		require.NoError(t, err, "%s", result)
		assert.Contains(t, string(result), "Verified Aider file writing")
	}
}
