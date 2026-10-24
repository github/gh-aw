//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestGrypeWorkflowImageCompleteness(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("../../.github/workflows/static-analysis-report.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(content), "---", 3)
	require.Len(t, parts, 3)
	var config struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &config))
	var script string
	for _, step := range config.Steps {
		if step.Name == "Assert Grype image scan completeness" {
			script = step.Run
		}
	}
	require.NotEmpty(t, script)
	planned := "i Grype scan planned for alpine:latest\n"
	completed := "i Grype scan completed for alpine:latest: 0 vulnerability finding(s)\n"
	sourceExcerpt := "  planned = collections.Counter(re.findall(r\"Grype scan planned for (\\S+)\", log))\n" +
		"  completed = collections.Counter(re.findall(r\"Grype scan completed for (\\S+): \\d+ vulnerability finding\\(s\\)\", log))\n" +
		"  failed = re.findall(r\"Grype scan failed for ([^\\n]+)\", log)\n"
	tests := []struct {
		name    string
		log     string
		wantErr bool
	}{
		{"zero findings", planned + completed, false},
		{"findings", planned + strings.Replace(completed, ": 0 ", ": 5 ", 1), false},
		{"cached repeat", planned + completed + planned + completed, false},
		{"colored status lines", "\x1b[36mi \x1b[0mGrype scan planned for alpine:latest\n" + "\x1b[36mi \x1b[0mGrype scan completed for alpine:latest: 0 vulnerability finding(s)\n", false},
		{"source excerpt", planned + completed + sourceExcerpt, false},
		{"no containers", "Running grype vulnerability scanner (0 container images found in lock files)", false},
		{"invocation only", "Running grype vulnerability scanner on 1 container image", true},
		{"omitted image", planned, true},
		{"failed image", planned + "⚠ Grype scan failed for alpine:latest: invalid JSON\n", true},
		{"unexpected image", planned + strings.ReplaceAll(completed, "alpine", "ubuntu"), true},
		{"omitted repeat", planned + planned + completed, true},
		{"failure after success", planned + completed + "⚠ Grype scan failed for alpine:latest: failed\n", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logFile := filepath.Join(t.TempDir(), "compile-output.txt")
			require.NoError(t, os.WriteFile(logFile, []byte(tc.log), 0o600))
			command := strings.ReplaceAll(script, "/tmp/gh-aw/agent/compile-output.txt", shellJoinArgs([]string{logFile}))
			output, err := exec.Command("bash", "-c", command).CombinedOutput()
			if tc.wantErr {
				require.Error(t, err, "%s", output)
				require.Contains(t, string(output), "Error:")
			} else {
				require.NoError(t, err, "%s", output)
				require.Contains(t, string(output), "completeness check passed")
			}
		})
	}
}
