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

func TestUsageArtifactUploadIncludesDurableSessionMetadata(t *testing.T) {
	steps := strings.Join(buildUsageArtifactUploadStep("", func(action string) string { return action }), "")
	require.Equal(t, 2, strings.Count(steps, "            /tmp/gh-aw/usage/agent/durable-session.jsonl\n"),
		"both the initial upload and retry must preserve raw durable metadata")
}

func TestUsageArtifactCollectionDurableSessionCopy(t *testing.T) {
	script, err := os.ReadFile("../../actions/setup/sh/collect_usage_artifact_files.sh")
	require.NoError(t, err)
	cpExecutable, err := exec.LookPath("cp")
	require.NoError(t, err)

	for _, scenario := range []struct {
		name       string
		hasDurable bool
		failCopy   bool
	}{
		{name: "successful copy", hasDurable: true},
		{name: "failed copy continues", hasDurable: true, failCopy: true},
		{name: "missing metadata is optional"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{
				"threat-detection/execution.json":                                         "{\"status\":\"success\"}\n",
				"evals/evals.jsonl":                                                       "{\"score\":1}\n",
				"evals/execution.json":                                                    "{\"status\":\"success\"}\n",
				"sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl":                  "{\"tokens\":10}\n",
				"threat-detection/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl": "{\"tokens\":2}\n",
			}
			if scenario.hasDurable {
				files["agent/durable-session.jsonl"] = "{\"type\":\"durable_session\",\"phase\":\"resume\"}\n"
			}
			for name, contents := range files {
				path := filepath.Join(dir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
				require.NoError(t, os.WriteFile(path, []byte(contents), 0600))
			}

			collector := strings.ReplaceAll(string(script), "/tmp/gh-aw", dir)
			pathPrefix := ""
			if scenario.failCopy {
				binDir := filepath.Join(dir, "bin")
				require.NoError(t, os.MkdirAll(binDir, 0755))
				cpShim := `#!/usr/bin/env bash
for arg in "$@"; do
  case "$arg" in
    */agent/durable-session.jsonl) echo "private-copy-error-sentinel" >&2; exit 1 ;;
  esac
done
exec "$REAL_CP" "$@"
`
				require.NoError(t, os.WriteFile(filepath.Join(binDir, "cp"), []byte(cpShim), 0755))
				pathPrefix = binDir + string(os.PathListSeparator)
			}
			command := exec.Command("bash", "-c", collector)
			if scenario.failCopy {
				command.Env = append(os.Environ(), "PATH="+pathPrefix+os.Getenv("PATH"), "REAL_CP="+cpExecutable)
			}
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.NotContains(t, string(output), "private-copy-error-sentinel")
			warning := "::warning::Durable session metadata could not be copied; continuing usage evidence collection."
			if scenario.failCopy {
				require.Contains(t, string(output), warning)
			} else {
				require.NotContains(t, string(output), warning)
			}

			expected := map[string]string{
				"usage/detection/execution.json":    files["threat-detection/execution.json"],
				"usage/evals.jsonl":                 files["evals/evals.jsonl"],
				"usage/evals/execution.json":        files["evals/execution.json"],
				"usage/agent/token_usage.jsonl":     files["sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl"],
				"usage/detection/token_usage.jsonl": files["threat-detection/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl"],
			}
			for name, contents := range expected {
				got, err := os.ReadFile(filepath.Join(dir, name))
				require.NoError(t, err, "subsequent evidence %s must be collected", name)
				require.Equal(t, contents, string(got))
			}
			durable, err := os.ReadFile(filepath.Join(dir, "usage/agent/durable-session.jsonl"))
			if scenario.hasDurable && !scenario.failCopy {
				require.NoError(t, err)
				require.Equal(t, files["agent/durable-session.jsonl"], string(durable))
			} else {
				require.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}
