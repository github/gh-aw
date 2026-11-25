//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

var azureAIGatewayScenario = connectorCompilerScenario{"azure-ai-gateway", nil, "", "create-issue"}

func TestAzureAIGatewayImportOnlyPreservesEngine(t *testing.T) {
	compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
		delete(f["engine"].(map[string]any), "inference")
		delete(f["permissions"].(map[string]any), "id-token")
	})
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	require.Empty(t, data.Connectors)
	require.Nil(t, data.EngineConfig.Auth)
	require.Equal(t, "gpt-5.2", data.Model)
	require.NotContains(t, data.EngineConfig.Env, "COPILOT_PROVIDER_BASE_URL")
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	for _, excluded := range []string{"AWF_AUTH_AZURE_", "secrets.AZURE_AI_GATEWAY_KEY", "vars.AZURE_CLIENT_ID", "vars.AZURE_TENANT_ID"} {
		require.NotContains(t, output, excluded)
	}
}

func TestAzureAIGatewayInferenceCompilation(t *testing.T) {
	for _, profile := range []string{"responses", "completions", "responses-key", "completions-key"} {
		t.Run(profile, func(t *testing.T) {
			key := strings.HasSuffix(profile, "-key")
			compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(consumer, _ map[string]any) {
				consumer["engine"].(map[string]any)["inference"].(map[string]any)["profile"] = profile
				delete(consumer["permissions"].(map[string]any), "id-token")
			})
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			assertConnectorScenarioAuthority(t, data, azureAIGatewayScenario)
			require.Nil(t, data.Connectors[azureAIGatewayScenario.provider].Agent)
			require.Nil(t, data.Connectors[azureAIGatewayScenario.provider].SafeOutputs)
			env := data.EngineConfig.Env
			require.Equal(t, "https://example.ai.gateway.azure.com/default/models/openai/v1", env["COPILOT_PROVIDER_BASE_URL"])
			require.Equal(t, "openai", env["COPILOT_PROVIDER_TYPE"])
			require.Equal(t, "deployment-checklist", env["COPILOT_PROVIDER_MODEL_ID"])
			require.Equal(t, "gpt-5.2", data.Model, "catalog metadata must not replace the granted wire alias")
			require.True(t, data.EngineConfig.ModelCatalogDisabled, "the gateway has no /models route")
			wire := "responses"
			if strings.HasPrefix(profile, "completions") {
				wire = "completions"
			}
			require.Equal(t, wire, env["COPILOT_PROVIDER_WIRE_API"])
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			require.Contains(t, output, "GH_AW_MODEL_CATALOG: false")
			agent, effects := connectorPhaseJobJSON(t, output)
			require.NotContains(t, output, "GH_AW_CONNECTOR_PLAN")
			require.NotContains(t, effects, "AZURE_AI_GATEWAY_KEY")
			require.NotContains(t, effects, "AWF_AUTH_AZURE_")
			require.NotContains(t, effects, "COPILOT_PROVIDER_")
			config, err := BuildAWFConfigJSON(AWFCommandConfig{
				EngineName: "copilot", AllowedDomains: "example.ai.gateway.azure.com", WorkflowData: data,
			})
			require.NoError(t, err)
			require.Contains(t, config, `"modelFallback":{"enabled":false}`)
			if key {
				require.Nil(t, data.EngineConfig.Auth)
				require.Equal(t, "${{ secrets.AZURE_AI_GATEWAY_KEY }}", env["COPILOT_PROVIDER_API_KEY"])
				require.Contains(t, agent, "AZURE_AI_GATEWAY_KEY")
				require.NotContains(t, output, "AWF_AUTH_AZURE_TENANT_ID")
				require.NotContains(t, output, "Mint connector")
			} else {
				require.Equal(t, "azure", data.EngineConfig.Auth.Provider)
				require.Equal(t, "https://ai.gateway.azure.com/.default", data.EngineConfig.Auth.AzureScope)
				require.Contains(t, agent, "AWF_AUTH_AZURE_SCOPE")
				require.Contains(t, output, "login.microsoftonline.com")
				require.Contains(t, output, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_TOKEN")
				require.NotContains(t, output, "secrets.AZURE_AI_GATEWAY_KEY")
			}
		})
	}
}

func TestConnectorInferenceRejectsInvalidSelection(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
		error  string
	}{
		{"unknown connector", func(f map[string]any) { inferenceSelection(f)["connector"] = "missing" }, "unknown connector"},
		{"unknown profile", func(f map[string]any) { inferenceSelection(f)["profile"] = "missing" }, "unknown inference profile"},
		{"ungranted model", func(f map[string]any) { inferenceSelection(f)["model"] = "other" }, "outside approved resources"},
		{"dynamic model", func(f map[string]any) { inferenceSelection(f)["model"] = "${{ inputs.model }}" }, "literal model"},
		{"missing model", func(f map[string]any) { delete(inferenceSelection(f), "model") }, "literal model"},
		{"extra field", func(f map[string]any) { inferenceSelection(f)["endpoint"] = "https://other.invalid" }, "unknown field"},
		{"different engine", func(f map[string]any) { f["engine"].(map[string]any)["id"] = "codex" }, "only the copilot"},
		{"conflicting auth", func(f map[string]any) { f["engine"].(map[string]any)["auth"] = map[string]any{"type": "github-oidc"} }, "conflicts"},
		{"provider alias", func(f map[string]any) { f["engine"].(map[string]any)["model-provider"] = "openai" }, "conflicts"},
		{"conflicting env", func(f map[string]any) {
			f["engine"].(map[string]any)["env"] = map[string]any{"COPILOT_PROVIDER_API_KEY": "${{ secrets.OTHER }}"}
		}, "conflicts"},
		{"global auth env", func(f map[string]any) { f["env"] = map[string]any{"AWF_AUTH_PROVIDER": "openai"} }, "conflicts"},
		{"routing", func(f map[string]any) { f["model-routing"] = map[string]any{"allowed-models": []any{"gpt-5.2"}} }, "conflicts"},
		{"CLI override", func(f map[string]any) { f["engine"].(map[string]any)["args"] = []any{"--model", "other"} }, "conflicts"},
		{"dynamic metadata", func(f map[string]any) { f["engine"].(map[string]any)["model"] = "${{ vars.INFERENCE_MODEL }}" }, "literal engine.model"},
		{"interpolated metadata", func(f map[string]any) { f["engine"].(map[string]any)["model"] = "gpt-${{ vars.SUFFIX }}" }, "literal engine.model"},
		{"native model env", func(f map[string]any) {
			f["engine"].(map[string]any)["env"] = map[string]any{"COPILOT_MODEL": "other"}
		}, "COPILOT_MODEL conflicts"},
		{"SDK mode", func(f map[string]any) { f["engine"].(map[string]any)["copilot-sdk"] = true }, "cannot preserve the granted wire model"},
		{"model catalogue", func(f map[string]any) { f["engine"].(map[string]any)["model-catalog"] = true }, "model-catalog: true conflicts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) { tc.change(f) })
			_, err := compiler.ParseWorkflowFile(path)
			require.ErrorContains(t, err, tc.error)
		})
	}
}

func TestConnectorInferenceAutoAllowsSelectedProfileHosts(t *testing.T) {
	const gateway, login = "example.ai.gateway.azure.com", "login.microsoftonline.com"
	for _, tc := range []struct {
		profile string
		want    []string
		absent  []string
	}{
		{"responses", []string{gateway, login}, nil},
		{"completions-key", []string{gateway}, []string{login}},
		{"", nil, []string{gateway, login}},
	} {
		t.Run("profile="+tc.profile, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
				f["network"] = map[string]any{"allowed": []any{"defaults"}}
				if tc.profile == "" {
					delete(f["engine"].(map[string]any), "inference")
				} else {
					inferenceSelection(f)["profile"] = tc.profile
				}
				if tc.profile != "responses" {
					delete(f["permissions"].(map[string]any), "id-token")
				}
			})
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			domains, err := compiler.computeAllowedDomainsForFirewall(data)
			require.NoError(t, err)
			allowed := strings.Split(domains, ",")
			for _, host := range tc.want {
				require.Contains(t, allowed, host)
			}
			for _, host := range tc.absent {
				require.NotContains(t, allowed, host)
			}
		})
	}
}

func TestConnectorInferenceExplicitHostsAreNotDuplicated(t *testing.T) {
	compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
		f["network"] = map[string]any{"allowed": []any{"defaults", "example.ai.gateway.azure.com", "login.microsoftonline.com"}}
	})
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	domains, err := compiler.computeAllowedDomainsForFirewall(data)
	require.NoError(t, err)
	for _, host := range []string{"example.ai.gateway.azure.com", "login.microsoftonline.com"} {
		require.Equal(t, 1, strings.Count(","+domains+",", ","+host+","))
	}
}

func TestConnectorAzureOIDCLoginHost(t *testing.T) {
	for cloud, want := range map[string]string{
		"": "login.microsoftonline.com", "public": "login.microsoftonline.com",
		"usgovernment": "login.microsoftonline.us", "china": "login.chinacloudapi.cn",
	} {
		require.Equal(t, want, (&ConnectorAzureOIDC{Cloud: cloud}).loginHost())
	}
}

func TestConnectorInferenceRejectsSandboxOverrides(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
		err   string
	}{
		{"model-fallback", true, "model-fallback must be false"},
		{"model-fallback", "${{ vars.MODEL_FALLBACK }}", "model-fallback must be false"},
		{"env", map[string]any{"COPILOT_PROVIDER_BASE_URL": "https://other.invalid"}, "sandbox.agent.env.COPILOT_PROVIDER_BASE_URL"},
		{"targets", map[string]any{"copilot": map[string]any{"auth-header": "api-key"}}, "auth-header"},
	} {
		t.Run(tc.err+"/"+tc.field, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
				f["strict"] = false
				f["sandbox"] = map[string]any{"agent": map[string]any{tc.field: tc.value}}
			})
			_, err := compiler.ParseWorkflowFile(path)
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestConnectorInferenceRejectsDetectorOverrides(t *testing.T) {
	for _, field := range []string{"engine", "engine-config"} {
		t.Run(field, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
				f["safe-outputs"].(map[string]any)["threat-detection"] = map[string]any{field: map[string]any{
					"id": "copilot", "env": map[string]any{"COPILOT_PROVIDER_BASE_URL": "https://other.invalid"},
				}}
			})
			_, err := compiler.ParseWorkflowFile(path)
			require.ErrorContains(t, err, "cannot override an engine that inherits connector inference")

			compiler, path = prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
				f["safe-outputs"].(map[string]any)["threat-detection"] = map[string]any{field: map[string]any{
					"id": "copilot", "inference": inferenceSelection(f),
				}}
			})
			_, err = compiler.ParseWorkflowFile(path)
			require.Error(t, err, "detector engine fields must not accept inference")
		})
	}
}

func TestConnectorInferenceRejectsInternalSelection(t *testing.T) {
	err := consumeConnectorInferenceSelection(nil, map[string]any{
		"prototype": map[string]any{"selected-inference": map[string]any{}},
	})
	require.ErrorContains(t, err, "selected-inference is internal")
}

func TestConnectorInferenceDeclarationRequiresGrantAndSupportedRoute(t *testing.T) {
	registration := ConnectorRegistration{
		Spec: &connectors.ConnectorSpec{Inference: map[string]connectors.Inference{
			"responses": {Models: "models", Credential: "inference"},
		}},
		Credentials: map[string]connectors.SecretRef{"inference": {Secret: "GATEWAY_KEY"}},
	}
	require.ErrorContains(t, validateConnectorInferenceProfiles(registration), "nonempty model resource collection")
	registration.Resources = map[string][]string{"models": {}}
	require.ErrorContains(t, validateConnectorInferenceProfiles(registration), "nonempty model resource collection")
	registration.Resources["models"] = []string{"approved"}
	require.NoError(t, validateConnectorInferenceProfiles(registration))

	registration.GitHubApps = map[string]*ConnectorGitHubApp{"inference": {}}
	require.ErrorContains(t, validateConnectorInferenceProfiles(registration), "stored token or Azure OIDC")
	registration.GitHubApps = nil
	registration.OIDCTokens = map[string]*ConnectorGitHubOIDC{"inference": {}}
	require.ErrorContains(t, validateConnectorInferenceProfiles(registration), "stored token or Azure OIDC")
	registration.OIDCTokens = nil
	registration.Credentials = nil
	require.ErrorContains(t, validateConnectorInferenceProfiles(registration), "stored token or Azure OIDC")
	registration.AzureOIDC = map[string]*ConnectorAzureOIDC{"inference": {}}
	require.NoError(t, validateConnectorInferenceProfiles(registration))
}

func TestConnectorInferenceImportedOverrides(t *testing.T) {
	for _, field := range []string{"env", "global env", "auth", "inference"} {
		t.Run(field, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
				imports := f["imports"].([]any)
				f["imports"] = append(imports, map[string]any{"uses": "shared/override.md"})
			})
			source := "---\nengine:\n  id: copilot\n"
			switch field {
			case "env":
				source += "  env:\n    COPILOT_PROVIDER_BEARER_TOKEN: '${{ secrets.OTHER_KEY }}'\n"
			case "global env":
				source = "---\nenv:\n  COPILOT_PROVIDER_BEARER_TOKEN: '${{ secrets.OTHER_KEY }}'\n"
			case "auth":
				source += "  auth:\n    type: github-oidc\n    provider: azure\n    audience: https://unapproved.invalid\n"
			case "inference":
				source += "  inference:\n    connector: azure-ai-gateway\n    profile: responses-key\n    model: deployment-checklist\n"
			}
			file := filepath.Join(filepath.Dir(path), "shared", "override.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
			require.NoError(t, os.WriteFile(file, []byte(source+"---\n"), 0600))
			_, err := compiler.ParseWorkflowFile(path)
			require.Error(t, err)
			switch field {
			case "inference":
				require.Contains(t, err.Error(), "main workflow")
			case "global env":
				require.Contains(t, err.Error(), "conflict")
			default:
				require.Contains(t, err.Error(), "multiple engine fields")
			}
		})
	}
}

func inferenceSelection(frontmatter map[string]any) map[string]any {
	return frontmatter["engine"].(map[string]any)["inference"].(map[string]any)
}

func TestConnectorInferenceOIDCPermissionScope(t *testing.T) {
	for _, tc := range []struct {
		name              string
		detector          any
		inheritsInference bool
	}{
		{"inherited", true, true},
		{"different engine", map[string]any{"engine": "claude"}, false},
		{"custom steps only", map[string]any{"engine": false, "steps": []any{map[string]any{"run": "echo scanned"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Compare with stored-key inference so independent grants (such as
			// admission OIDC) are not mistaken for connector-inference grants.
			var baseline map[string]string
			for _, profile := range []string{"responses-key", "responses"} {
				compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
					delete(f["permissions"].(map[string]any), "id-token")
					inferenceSelection(f)["profile"] = profile
					f["safe-outputs"].(map[string]any)["threat-detection"] = tc.detector
				})
				data, err := compiler.ParseWorkflowFile(path)
				require.NoError(t, err)
				output, err := compiler.CompileToYAML(data, path)
				require.NoError(t, err)
				var lock struct {
					Permissions map[string]string `yaml:"permissions"`
					Jobs        map[string]struct {
						Permissions map[string]string `yaml:"permissions"`
					} `yaml:"jobs"`
				}
				require.NoError(t, yaml.Unmarshal([]byte(output), &lock))
				require.NotContains(t, lock.Permissions, "id-token")
				require.Contains(t, lock.Jobs, "detection")
				grants := map[string]string{}
				for name, job := range lock.Jobs {
					grants[name] = job.Permissions["id-token"]
				}
				if profile == "responses-key" {
					require.NotEqual(t, "write", grants["agent"])
					require.NotEqual(t, "write", grants["detection"])
					baseline = grants
					continue
				}
				baseline["agent"] = "write"
				if tc.inheritsInference {
					baseline["detection"] = "write"
				}
				require.Equal(t, baseline, grants)
			}
		})
	}
}

func TestConnectorInferenceOIDCAuthProvenance(t *testing.T) {
	compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
		delete(f["permissions"].(map[string]any), "id-token")
	})
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	require.True(t, usesConnectorInferenceOIDC(data))
	require.NoError(t, validateOIDCPermissions(data, nil))

	agentPermission := func(t *testing.T, data *WorkflowData) string {
		t.Helper()
		c := NewCompiler()
		job := &Job{Name: "agent"}
		require.NoError(t, c.jobManager.AddJob(job))
		c.ensureConnectorOIDCJobPermissions(data)
		return job.Permissions
	}
	require.Contains(t, agentPermission(t, data), "id-token: write")
	auth := *data.EngineConfig.Auth
	t.Run("equivalent authored auth is not connector auth", func(t *testing.T) {
		copyData := *data
		engine := *data.EngineConfig
		copyData.EngineConfig = &engine
		engine.Auth = &auth
		require.False(t, usesConnectorInferenceOIDC(&copyData))
		require.ErrorContains(t, validateOIDCPermissions(&copyData, nil), "requires permissions.id-token: write")
		require.NotContains(t, agentPermission(t, &copyData), "id-token: write")
	})
	t.Run("unrelated connector cannot supply provenance", func(t *testing.T) {
		copyData := *data
		other := *data.Connectors[azureAIGatewayScenario.provider].Inference
		other.Selection.Connector = "other"
		copyData.Connectors = map[string]*WorkflowConnector{"other": {Inference: &other}}
		require.False(t, usesConnectorInferenceOIDC(&copyData))
		require.ErrorContains(t, validateOIDCPermissions(&copyData, nil), "requires permissions.id-token: write")
	})
	t.Run("matching auth without expansion cannot establish provenance", func(t *testing.T) {
		inference := *data.Connectors[azureAIGatewayScenario.provider].Inference
		inference.authExpanded = false
		engine := *data.EngineConfig
		engine.Auth = &auth
		copyData := *data
		copyData.EngineConfig = &engine
		copyData.Connectors = map[string]*WorkflowConnector{inference.Selection.Connector: {Inference: &inference}}
		require.NoError(t, validateResolvedConnectorInference(&copyData, copyData.Connectors, nil))
		require.False(t, usesConnectorInferenceOIDC(&copyData))
		require.ErrorContains(t, validateOIDCPermissions(&copyData, nil), "requires permissions.id-token: write")
	})

	encoded, err := json.Marshal(auth)
	require.NoError(t, err)
	var authored map[string]any
	require.NoError(t, json.Unmarshal(encoded, &authored))
	for key, value := range authored {
		if value == "" {
			delete(authored, key)
		}
	}
	t.Run("matching authored override is rejected", func(t *testing.T) {
		compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
			f["engine"].(map[string]any)["auth"] = authored
		})
		_, err := compiler.ParseWorkflowFile(path)
		require.ErrorContains(t, err, "engine.auth conflicts")
	})
	t.Run("matching imported override is rejected", func(t *testing.T) {
		compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
			f["imports"] = append(f["imports"].([]any), map[string]any{"uses": "shared/override.md"})
		})
		source, err := yaml.Marshal(map[string]any{"engine": map[string]any{"id": "copilot", "auth": authored}})
		require.NoError(t, err)
		file := filepath.Join(filepath.Dir(path), "shared", "override.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
		require.NoError(t, os.WriteFile(file, append(append([]byte("---\n"), source...), []byte("---\n")...), 0600))
		_, err = compiler.ParseWorkflowFile(path)
		require.ErrorContains(t, err, "multiple engine fields")
	})
}

func TestConnectorInferenceOIDCSafetyGates(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"old firewall", "v0.25.3"},
		{"dry-run", "cannot safely execute connector inference"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
				delete(f["permissions"].(map[string]any), "id-token")
				if tc.mode == "old firewall" {
					f["safe-outputs"].(map[string]any)["threat-detection"] = false
					f["sandbox"] = map[string]any{"agent": map[string]any{"version": "v0.25.2"}}
				}
			})
			compiler.SetDryRun(tc.mode == "dry-run")
			require.ErrorContains(t, compiler.CompileWorkflow(path), tc.want)
		})
	}
}
