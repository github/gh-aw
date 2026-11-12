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
	var expected []string
	for _, declaration := range declarations {
		provider := strings.TrimSuffix(filepath.Base(declaration), ".md")
		path := filepath.Join("../../.github/workflows", "connector-"+provider+"-conformance.yml")
		expected = append(expected, path)
		t.Run(provider, func(t *testing.T) {
			source, err := os.ReadFile(path)
			require.NoError(t, err, "every connector needs a daily conformance workflow")
			var document struct {
				Name string `yaml:"name"`
				On   struct {
					Schedule []struct {
						Cron string `yaml:"cron"`
					} `yaml:"schedule"`
					Dispatch yaml.Node `yaml:"workflow_dispatch"`
				} `yaml:"on"`
				Permissions map[string]string `yaml:"permissions"`
				Jobs        map[string]struct {
					Uses string            `yaml:"uses"`
					With map[string]string `yaml:"with"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(source, &document))
			require.Equal(t, "connector-"+provider+"-conformance", document.Name)
			require.NotZero(t, document.On.Dispatch.Kind, "manual dispatch must remain available")
			require.Len(t, document.On.Schedule, 1)
			require.Regexp(t, `^([0-5]?[0-9]) ([01]?[0-9]|2[0-3]) \* \* \*$`, document.On.Schedule[0].Cron)
			require.Equal(t, map[string]string{"contents": "read"}, document.Permissions)
			require.Len(t, document.Jobs, 1)
			job := document.Jobs["conformance"]
			require.Equal(t, "./.github/workflows/test-connector-conformance.yml", job.Uses)
			require.Equal(t, map[string]string{"provider": provider}, job.With)
		})
	}
	workflows, err := filepath.Glob("../../.github/workflows/connector-*-conformance.yml")
	require.NoError(t, err)
	require.ElementsMatch(t, expected, workflows, "daily workflows must match the connector inventory")
}
