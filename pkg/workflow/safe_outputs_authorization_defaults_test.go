//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputAuthorizationRepoDefaults(t *testing.T) {
	for _, setting := range []string{"true", `{"endpoint":"https://authority.example/a","audience":"https://authority.example/gh-aw"}`} {
		t.Run(setting, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, RepoConfigFileName), []byte(`{"safe_output_authorization":`+setting+`}`), 0o600))
			compiler := NewCompiler(WithNoEmit(true))
			compiler.gitRoot = root
			file := filepath.Join(root, "workflow.md")
			data, err := compiler.ParseWorkflowString("---\non: workflow_dispatch\nsafe-outputs:\n  add-comment:\n  actions:\n    notify:\n      uses: ./reviewed-action\n---\nAuthorize outputs.\n", file)
			require.NoError(t, err)
			compiled, err := compiler.CompileToYAML(data, file)
			require.NoError(t, err)
			require.Contains(t, compiled, "safe_output_authorization_gate.cjs")
			for _, name := range []string{"add_comment", "notify"} {
				require.NotNil(t, data.SafeOutputs.AuthorizationRequirements[name])
				require.Equal(t, "global", data.SafeOutputs.AuthorizationRequirements[name].Source)
			}
			require.Nil(t, data.SafeOutputs.AuthorizationRequirements["noop"])
			require.Contains(t, ComputePermissionsForSafeOutputs(data.SafeOutputs).RenderToYAML(), "id-token: write")
			environment := compiler.buildJobLevelSafeOutputEnvVars(data, "workflow")
			require.Contains(t, environment["GH_AW_SAFE_OUTPUTS_AUTHORIZATION_CONFIG"], "authorization")
			steps, err := compiler.buildActionSteps(data)
			require.NoError(t, err)
			gate := strings.Join(steps, "")
			if setting == "true" {
				require.True(t, data.SafeOutputs.Authorization.FromEnvironment)
				for _, name := range []string{compilerenv.DefaultSafeOutputAuthorizationURL, compilerenv.DefaultSafeOutputAuthorizationAudience} {
					require.Equal(t, "${{ vars."+name+" }}", environment[name])
					require.Contains(t, gate, name+": ${{ vars."+name+" }}")
				}
				require.Contains(t, environment["GH_AW_SAFE_OUTPUTS_AUTHORIZATION_CONFIG"], `\"from_environment\":true`)
				require.Contains(t, gate, `\"from_environment\":true`)
			} else {
				require.False(t, data.SafeOutputs.Authorization.FromEnvironment)
				require.Equal(t, "https://authority.example/a", data.SafeOutputs.Authorization.Endpoint)
				require.NotContains(t, environment, compilerenv.DefaultSafeOutputAuthorizationURL)
				require.NotContains(t, gate, compilerenv.DefaultSafeOutputAuthorizationURL)
			}
		})
	}
}

func TestSafeOutputAuthorizationRepoConflictsAndImports(t *testing.T) {
	for _, setting := range []string{"true", `{"endpoint":"https://authority.example/a","audience":"https://authority.example/gh-aw"}`} {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, RepoConfigFileName), []byte(`{"safe_output_authorization":`+setting+`}`), 0o600))
		compiler := NewCompiler()
		compiler.gitRoot = root
		require.NoError(t, os.WriteFile(filepath.Join(root, "shared.md"), []byte("---\nsafe-outputs:\n  add-comment:\n---\nShared output.\n"), 0o600))
		source := "---\non: workflow_dispatch\nimports:\n  - ./shared.md\n---\nImported outputs.\n"
		data, err := compiler.ParseWorkflowString(source, filepath.Join(root, "workflow.md"))
		require.NoError(t, err)
		require.NotNil(t, data.SafeOutputs.AuthorizationRequirements["add_comment"])

		for _, placement := range []string{"  authorization:\n", "  add-comment:\n    authorization:\n"} {
			indent := "    "
			if strings.Contains(placement, "add-comment") {
				indent = "      "
			}
			_, err := compiler.ParseWorkflowString("---\non: workflow_dispatch\nsafe-outputs:\n"+placement+indent+"endpoint: https://different.example/a\n"+indent+"audience: different\n---\nConflicting authority.\n", filepath.Join(root, "conflict.md"))
			require.ErrorContains(t, err, "conflicts")
			if setting != "true" {
				data, err := compiler.ParseWorkflowString("---\non: workflow_dispatch\nimports:\n  - ./shared.md\nsafe-outputs:\n"+placement+indent+"endpoint: https://authority.example/a\n"+indent+"audience: https://authority.example/gh-aw\n---\nIdentical authority.\n", filepath.Join(root, "identical.md"))
				require.NoError(t, err)
				require.NotNil(t, data.SafeOutputs.AuthorizationRequirements["add_comment"])
			}
		}
	}
}

func TestSafeOutputAuthorizationRepoStagingAndValidation(t *testing.T) {
	compiler := NewCompiler()
	compiler.repoConfigLoaded = true
	compiler.repoConfig = &RepoConfig{SafeOutputAuthorization: &SafeOutputAuthorization{FromEnvironment: true}}
	config := authorizationTestConfig(t, map[string]any{"staged": true, "add-comment": map[string]any{}})
	data := &WorkflowData{SafeOutputs: config}
	require.NoError(t, compiler.applyRepoSafeOutputAuthorization(data))
	require.NoError(t, resolveWorkflowSafeOutputAuthorizations(data))
	require.Equal(t, "staged", config.AuthorizationCoverage["add_comment"])
	require.NotContains(t, ComputePermissionsForSafeOutputs(config).RenderToYAML(), "id-token:")
	empty := &WorkflowData{}
	require.NoError(t, compiler.applyRepoSafeOutputAuthorization(empty))
	require.Nil(t, empty.SafeOutputs, "repository policy must not enable outputs")
	for _, raw := range []string{"false", "null", `"true"`, `{}`, `{"endpoint":"http://insecure.example","audience":"a"}`, `{"endpoint":"https://example.test","audience":"a","from_environment":true}`} {
		document := []byte(`{"safe_output_authorization":` + raw + `}`)
		var config RepoConfig
		require.Error(t, json.Unmarshal(document, &config))
	}
}
