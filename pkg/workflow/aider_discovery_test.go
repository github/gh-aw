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
	"gopkg.in/yaml.v3"
)

func aiderDiscoveryCommand(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile("../../.github/workflows/" + name + ".md")
	require.NoError(t, err)
	_, body, found := strings.Cut(string(content), "```bash\n")
	require.True(t, found)
	command, _, found := strings.Cut(body, "\n```")
	require.True(t, found)
	return command
}

func TestAiderCoverageImprovementGates(t *testing.T) {
	cmd := exec.Command("python3", "-B", "../../scripts/test_aider_go_test_coverage.py")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Contains(t, aiderDiscoveryCommand(t, "daily-go-test-stubs-aider"), "aider_go_test_coverage.py discover")
}

func TestAiderDebtDiscoveryOnlySelectsSourceMarkers(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"source.go":         "// TODO: implement this\n\t// FIXME handle error\nfunc printConfigTODO() {}\n// printConfigTODO prints a checklist\nvar text = \"TODO: customize\"\n",
		"source_test.go":    "// TODO: test fixture\n",
		"testdata/input.go": "// FIXME: fixture\n",
		"vendor/example.go": "// TODO: vendored\n",
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	cmd := exec.Command("bash", "-c", aiderDiscoveryCommand(t, "daily-code-debt-aider"))
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Equal(t, "./source.go:1:// TODO: implement this\n./source.go:2:\t// FIXME handle error\n", string(output))
}

func TestAiderDailyGoValidationContracts(t *testing.T) {
	for _, name := range []string{"daily-code-debt-aider", "daily-go-test-stubs-aider"} {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile("../../.github/workflows/" + name + ".md")
			require.NoError(t, err)
			var frontmatter struct {
				Runtimes map[string]struct {
					Version string `yaml:"version"`
				} `yaml:"runtimes"`
			}
			parts := strings.SplitN(string(content), "---", 3)
			require.Len(t, parts, 3)
			require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
			assert.Equal(t, "1.27.3", frontmatter.Runtimes["go"].Version)
			assert.NotContains(t, parts[2], "|| safeoutputs noop")
			assert.NotContains(t, parts[2], "|| true")
			assert.NotContains(t, parts[2], "GOMODCACHE=/tmp/")
			if name == "daily-go-test-stubs-aider" {
				assert.Contains(t, parts[2], "|| safeoutputs report_incomplete")
				assert.Contains(t, parts[2], "aider_go_test_coverage.py publish")
				assert.NotContains(t, parts[2], "git add -A")
				assert.NotContains(t, parts[2], "TestPlaceholder_TODO")
				assert.Contains(t, parts[1], "schedule: weekly")
				assert.Contains(t, parts[1], `"pkg/**/aider_coverage_test.go"`)
				assert.NotContains(t, parts[1], "steer: true")
			} else {
				assert.Contains(t, parts[1], `GH_AW_AIDER_MAX_REFLECTIONS: "8"`)
				assert.Contains(t, parts[2], "make fmt-go\ngo build ./...")
				assert.Contains(t, parts[2], "concrete stderr and exit code in `--details`")
				assert.Contains(t, parts[2], "existing tracked file returned by discovery")
				assert.Contains(t, parts[2], "do not create, rename, or delete files")
				assert.Contains(t, parts[2], "Do not use an empty SEARCH block")
				assert.Contains(t, parts[2], "Never assume discovery found a candidate")
				assert.NotContains(t, parts[2], "git add -A &&")
				assert.NotContains(t, parts[2], "|| safeoutputs report_incomplete --reason")
			}
		})
	}
}
