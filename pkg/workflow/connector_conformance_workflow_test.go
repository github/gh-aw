//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestConnectorConformanceWorkflows(t *testing.T) {
	declarations, err := filepath.Glob("../../.github/workflows/connectors/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, declarations)
	for _, declaration := range declarations {
		provider := strings.TrimSuffix(filepath.Base(declaration), ".md")
		require.Regexp(t, `^[a-z][a-z0-9]*(-[a-z0-9]+)*$`, provider, "connector declaration names must be safe matrix values")
	}

	source, err := os.ReadFile("../../.github/workflows/test-connectors.yml")
	require.NoError(t, err)
	var document struct {
		On struct {
			Schedule []struct {
				Cron string `yaml:"cron"`
			} `yaml:"schedule"`
			Dispatch yaml.Node `yaml:"workflow_dispatch"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Outputs     map[string]string `yaml:"outputs"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
			Needs    any               `yaml:"needs"`
			Uses     string            `yaml:"uses"`
			With     map[string]string `yaml:"with"`
			Strategy struct {
				Matrix map[string]any `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(source, &document))
	require.Len(t, document.On.Schedule, 1)
	require.Regexp(t, `^([0-5]?[0-9]) ([01]?[0-9]|2[0-3]) \* \* \*$`, document.On.Schedule[0].Cron)
	require.NotZero(t, document.On.Dispatch.Kind, "manual dispatch must remain available")
	require.Equal(t, map[string]string{"contents": "read"}, document.Permissions)

	discovery := document.Jobs["discover"]
	require.Len(t, discovery.Steps, 2)
	require.Contains(t, discovery.Steps[1].Run, ".github/workflows/connectors")
	require.Contains(t, discovery.Steps[1].Run, `^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	require.Contains(t, discovery.Steps[1].Run, "GITHUB_OUTPUT")
	conformance := document.Jobs["conformance"]
	require.Equal(t, "discover", conformance.Needs)
	require.Equal(t, "./.github/workflows/test-connector-conformance.yml", conformance.Uses)
	require.Equal(t, "${{ fromJSON(needs.discover.outputs.providers) }}", conformance.Strategy.Matrix["provider"])
	require.Equal(t, "${{ matrix.provider }}", conformance.With["provider"])
	require.Equal(t, map[string]string{"issues": "write"}, document.Jobs["report-failure"].Permissions)

	workflows, err := filepath.Glob("../../.github/workflows/connector-*-conformance.yml")
	require.NoError(t, err)
	require.Empty(t, workflows, "connector coverage should be grouped in one daily workflow")
}
