//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestRepositoryGoToolingBaseline(t *testing.T) {
	t.Parallel()
	require.Equal(t, "1.27.4", string(constants.DefaultGoVersion))
	root := filepath.Join("..", "..")
	for _, path := range []string{"go.mod", ".github/workflows/go.mod"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.Contains(t, "\n"+string(data), "\ngo 1.27.4\n", path)
	}
	data, err := os.ReadFile(filepath.Join(root, ".golangci.yml"))
	require.NoError(t, err)
	var lint struct {
		Run struct {
			Go string `yaml:"go"`
		} `yaml:"run"`
	}
	require.NoError(t, yaml.Unmarshal(data, &lint))
	require.Equal(t, "1.27.4", lint.Run.Go)
	data, err = os.ReadFile(filepath.Join(root, ".devcontainer", "devcontainer.json"))
	require.NoError(t, err)
	var devcontainer struct {
		Features map[string]struct {
			Version string `json:"version"`
		} `json:"features"`
	}
	require.NoError(t, json.Unmarshal(data, &devcontainer))
	require.Equal(t, "1.27.4", devcontainer.Features["ghcr.io/devcontainers/features/go:1"].Version)
}

func TestOwnedWorkflowGoVersionPins(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.md"))
	require.NoError(t, err)
	checked := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		parts := strings.SplitN(string(data), "---", 3)
		if len(parts) != 3 || strings.TrimSpace(parts[0]) != "" {
			continue
		}
		var config struct {
			Source   string `yaml:"source"`
			Runtimes struct {
				Go struct {
					Version string `yaml:"version"`
				} `yaml:"go"`
			} `yaml:"runtimes"`
		}
		require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &config), path)
		if config.Source != "" || config.Runtimes.Go.Version == "" {
			continue
		}
		require.Equal(t, "1.27.4", config.Runtimes.Go.Version, path)
		checked++
	}
	require.Positive(t, checked, "owned runtime pins must actually be inspected")
}

func TestNewWorkflowDependencyModuleUsesGoBaseline(t *testing.T) {
	t.Parallel()
	lines, err := NewCompiler().loadOrInitGoModLines(filepath.Join(t.TempDir(), "go.mod"))
	require.NoError(t, err)
	require.Contains(t, lines, "go "+string(constants.DefaultGoVersion))
}
