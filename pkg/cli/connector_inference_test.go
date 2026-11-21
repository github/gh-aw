//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestAzureAIGatewayInferenceSetup(t *testing.T) {
	root := t.TempDir()
	const source = ".github/workflows/connectors/azure-ai-gateway.md"
	content, err := os.ReadFile(filepath.Join("../..", source))
	require.NoError(t, err)
	writeConnectorSetupFixture(t, root, source, string(content))
	t.Setenv("AZURE_AI_GATEWAY_KEY", "synthetic-value-must-not-be-read")
	output, err := executeDevelopmentCommand("azure-ai-gateway", "setup", "--root", root, "--phase", "inference")
	require.NoError(t, err)
	for _, expected := range []string{"Azure OIDC", "AZURE_AI_GATEWAY_KEY", "openai-responses", "engine.inference", "not a callable tool"} {
		require.Contains(t, output, expected)
	}
	require.NotContains(t, output, "synthetic-value-must-not-be-read")
	require.NotContains(t, output, "GH_AW_CONNECTOR_APP_")
	declaration, err := workflow.LoadDevelopmentConnector(root, "azure-ai-gateway", source, nil, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []connectorSetupRequirement{
		{kind: "variable", name: "AZURE_TENANT_ID"},
		{kind: "variable", name: "AZURE_CLIENT_ID"},
		{kind: "secret", name: "AZURE_AI_GATEWAY_KEY"},
	}, connectorSetupRequirements(declaration.Registration, "inference"))
	_, err = executeDevelopmentCommand("azure-ai-gateway", "test", "--root", root, "--phase", "inference", "--capability", "responses")
	require.ErrorContains(t, err, "compile --strict --no-emit")
}
