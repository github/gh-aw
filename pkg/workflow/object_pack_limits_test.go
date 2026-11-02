//go:build !integration

package workflow

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObjectPackLimitNamesMatchRuntime(t *testing.T) {
	source, err := os.ReadFile("../../actions/setup/js/object_pack_limits.cjs")
	require.NoError(t, err)
	matches := regexp.MustCompile(`"GH_AW_OBJECT_PACK_[A-Z_]+"`).FindAllString(string(source), -1)
	var names []string
	for _, match := range matches {
		names = append(names, strings.Trim(match, `"`))
	}
	assert.Equal(t, objectPackLimitEnvNames, names)
}

func TestObjectPackLimitRepositoryBindings(t *testing.T) {
	compiler := NewCompiler()
	compiler.repoConfigLoaded = true
	compiler.repoConfig = &RepoConfig{}
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}}}
	envs := []map[string]string{
		compiler.buildMainJobEnv(data),
		compiler.buildJobLevelSafeOutputEnvVars(data, "test"),
		collectMCPEnvironmentVariables(nil, []string{"safe-outputs"}, data, false),
	}
	for _, env := range envs {
		for _, name := range objectPackLimitEnvNames {
			assert.Equal(t, "${{ vars."+name+" }}", env[name])
		}
	}
	var guard strings.Builder
	generateRecoveryEnvelopeValidationStep(&guard, "test_guard", data, func(name string) string { return name + "@pin" })
	for _, name := range objectPackLimitEnvNames {
		assert.Contains(t, guard.String(), name+": ${{ vars."+name+" }}")
	}
}

func TestObjectPackLimitMCPForwarding(t *testing.T) {
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{}}
	var gateway strings.Builder
	appendMCPGatewayBaseEnvFlags(&gateway, "")
	added := buildAddedGatewayEnvVarSet(data, nil, false, nil, nil, nil)
	for _, name := range objectPackLimitEnvNames {
		assert.Equal(t, 1, strings.Count(gateway.String(), " -e "+name+" "))
		assert.Contains(t, added, name)
	}
	for _, format := range []string{"json", "toml"} {
		t.Run(format, func(t *testing.T) {
			renderer := NewMCPConfigRenderer(MCPRendererOptions{Format: format, IsLast: true})
			var output strings.Builder
			renderer.RenderSafeOutputsMCP(&output, data)
			for _, name := range objectPackLimitEnvNames {
				assert.Contains(t, output.String(), `"`+name+`"`)
				if format == "json" {
					assert.Contains(t, output.String(), `"\${`+name+`}"`)
				}
			}
		})
	}
}
