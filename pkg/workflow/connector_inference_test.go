//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
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
				if key {
					delete(consumer["permissions"].(map[string]any), "id-token")
				}
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
			wire := "responses"
			if strings.HasPrefix(profile, "completions") {
				wire = "completions"
			}
			require.Equal(t, wire, env["COPILOT_PROVIDER_WIRE_API"])
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
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
		{"missing network approval", func(f map[string]any) { f["network"] = map[string]any{"allowed": []any{"defaults"}} }, "explicit network.allowed"},
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) { tc.change(f) })
			_, err := compiler.ParseWorkflowFile(path)
			require.ErrorContains(t, err, tc.error)
		})
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

func TestConnectorInferenceUsesExistingOIDCGates(t *testing.T) {
	for _, mode := range []string{"missing permission", "old firewall"} {
		t.Run(mode, func(t *testing.T) {
			compiler, path := prepareConnectorScenario(t, azureAIGatewayScenario, false, func(f, _ map[string]any) {
				if mode == "missing permission" {
					delete(f["permissions"].(map[string]any), "id-token")
				} else {
					f["sandbox"] = map[string]any{"agent": map[string]any{"version": "v0.25.2"}}
				}
			})
			err := compiler.CompileWorkflow(path)
			require.Error(t, err)
			if mode == "missing permission" {
				require.Contains(t, err.Error(), "id-token")
			} else {
				require.Contains(t, err.Error(), "v0.25.3")
			}
		})
	}
}
