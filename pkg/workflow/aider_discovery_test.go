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

func TestAiderDiscoveryUsesGoPackagesAndActualNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspace with spaces")
	require.NoError(t, os.Mkdir(dir, 0o700))
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	for name, content := range map[string]string{
		"go.mod":                    "module example.test/aider\n\ngo 1.20\n",
		"cmd/hyphenated/main.go":    "package main\nfunc main() {}\n",
		"untested/source.go":        "package actualname\n",
		"tested/source.go":          "package tested\n",
		"tested/source_test.go":     "package tested\n",
		"external/source.go":        "package external\n",
		"external/source_test.go":   "package external_test\n",
		"testdata/fixture/input.go": "not a Go package\n",
		".hidden/source.go":         "not a Go package\n",
		"nested/go.mod":             "module example.test/nested\n\ngo 1.20\n",
		"nested/source.go":          "not a Go package\n",
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	command := aiderDiscoveryCommand(t, "daily-go-test-stubs-aider")
	// Keep the workflow's scratch file confined to this fixture.
	command = strings.ReplaceAll(command, "/tmp/gh-aw/agent/aider-untested-packages", `"`+filepath.Join(dir, "packages")+`"`)
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Equal(t, filepath.Join(dir, "cmd/hyphenated")+" main\n"+filepath.Join(dir, "untested")+" actualname\n", string(output))

	for path, name := range map[string]string{
		"cmd/hyphenated/main_test.go": "main",
		"untested/actualname_test.go": "actualname",
	} {
		content, err := os.ReadFile(filepath.Join(dir, path))
		require.NoError(t, err)
		assert.Equal(t, "package "+name+"\n\nimport \"testing\"\n\nfunc TestPlaceholder_TODO(t *testing.T) {\n\tt.Skip(\"not yet implemented\")\n}\n", string(content))
	}
	check := exec.Command("go", "test", "./...", "-run", "^TestPlaceholder_TODO$")
	check.Dir = dir
	check.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	output, err = check.CombinedOutput()
	require.NoError(t, err, "generated stubs must compile: %s", output)

	cmd = exec.Command("bash", "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Empty(t, string(output), "packages with existing internal or external tests need no stubs")
	for path, expected := range map[string]string{
		"tested/source_test.go":   "package tested\n",
		"external/source_test.go": "package external_test\n",
	} {
		content, err := os.ReadFile(filepath.Join(dir, path))
		require.NoError(t, err)
		assert.Equal(t, expected, string(content))
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "untested/broken.go"), []byte("not a Go package\n"), 0o600))
	cmd = exec.Command("bash", "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	output, err = cmd.CombinedOutput()
	require.Error(t, err, "failed discovery must not look like an empty package list: %s", output)
}

func TestAiderStubGenerationLimitsWrites(t *testing.T) {
	t.Run("at most three packages", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/aider\n\ngo 1.20\n"), 0o600))
		for _, name := range []string{"a", "b", "c", "d"} {
			packageDir := filepath.Join(dir, name+"-command")
			require.NoError(t, os.Mkdir(packageDir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(packageDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600))
		}
		command := strings.ReplaceAll(aiderDiscoveryCommand(t, "daily-go-test-stubs-aider"),
			"/tmp/gh-aw/agent/aider-untested-packages", filepath.Join(dir, "packages"))
		cmd := exec.Command("bash", "-c", command)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		assert.Len(t, strings.Split(strings.TrimSpace(string(output)), "\n"), 3)
		for _, name := range []string{"a", "b", "c"} {
			content, err := os.ReadFile(filepath.Join(dir, name+"-command", "main_test.go"))
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(string(content), "package main\n"))
		}
		_, err = os.Stat(filepath.Join(dir, "d-command", "main_test.go"))
		require.True(t, os.IsNotExist(err))
	})

	t.Run("never overwrite an excluded test file", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/aider\n\ngo 1.20\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600))
		existing := []byte("//go:build ignore\n\npackage main\n")
		path := filepath.Join(dir, "main_test.go")
		require.NoError(t, os.WriteFile(path, existing, 0o600))
		command := strings.ReplaceAll(aiderDiscoveryCommand(t, "daily-go-test-stubs-aider"),
			"/tmp/gh-aw/agent/aider-untested-packages", filepath.Join(dir, "packages"))
		cmd := exec.Command("bash", "-c", command)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
		output, err := cmd.CombinedOutput()
		require.Error(t, err, "an existing filename must stop generation: %s", output)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, existing, content)
	})
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
			assert.Contains(t, parts[2], "make fmt-go && go ")
			assert.Contains(t, parts[2], "|| safeoutputs report_incomplete")
			assert.NotContains(t, parts[2], "|| safeoutputs noop")
			assert.NotContains(t, parts[2], "|| true")
			assert.NotContains(t, parts[2], "GOMODCACHE=/tmp/")
			if name == "daily-go-test-stubs-aider" {
				assert.Contains(t, parts[2], "go test ./... -run '^TestPlaceholder_TODO$'")
			}
		})
	}
}
