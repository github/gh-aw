//go:build !integration && !windows

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestArchitectureGuardianCollectsExportCounts(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/architecture-guardian.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	var script string
	for _, step := range frontmatter.Steps {
		if step.Name == "Collect architecture metrics" {
			script = step.Run
		}
	}
	require.NotEmpty(t, script)

	dir := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.test/guardian\n\ngo 1.21\n",
		"empty.go":     "package guardian\n\nfunc private() {}\n",
		"exported.go":  "package guardian\n\nfunc Exported() {}\n",
		"empty.js":     "const value = 1;\n",
		"commonjs.cjs": "module.exports = {};\n",
		"module.mjs":   "export const value = 1;\n",
		"combined.js":  "exports.value = 1;\nexport const other = 2;\n",
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}

	outputDir := filepath.Join(dir, "output")
	script = strings.ReplaceAll(script, "/tmp/gh-aw/agent", outputDir)
	cmd := exec.Command("bash", "-e", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.NotContains(t, string(output), "syntax error")

	data, err := os.ReadFile(filepath.Join(outputDir, "arch-metrics.json"))
	require.NoError(t, err)
	var metrics struct {
		Noop  bool `json:"noop"`
		Files []struct {
			File        string `json:"file"`
			ExportCount int    `json:"export_count"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal(data, &metrics))
	require.False(t, metrics.Noop)
	counts := make(map[string]int)
	for _, file := range metrics.Files {
		counts[file.File] = file.ExportCount
	}
	require.Equal(t, map[string]int{
		"empty.go": 0, "exported.go": 1, "empty.js": 0,
		"commonjs.cjs": 1, "module.mjs": 1, "combined.js": 2,
	}, counts)
}
