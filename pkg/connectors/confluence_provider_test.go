package connectors

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfluenceConnectorDefinition(t *testing.T) {
	spec, err := LoadSpec("testdata/providers/confluence/spec.yaml")
	require.NoError(t, err)
	require.Equal(t, "atlassian.com/confluence", spec.ID)
	require.Equal(t, "https://api.atlassian.com", spec.Endpoints["api"])
	require.Len(t, spec.Triggers, 2)
	require.Len(t, spec.SafeOutputs, 2)
	require.NotContains(t, spec.SafeOutputs, "update-page", "version preflight is not supported")
	require.NotContains(t, spec.SafeOutputs, "update-comment", "version preflight is not supported")

	for _, op := range []Operation{spec.MCP["get-page"].Operation, spec.Context["runbook"]} {
		require.Equal(t, "GET", op.Request.Method)
		require.Equal(t, "/ex/confluence/{cloud}/wiki/api/v2/pages", op.Request.Path)
		require.Equal(t, "page-id", op.Request.Query["id"].Input)
		require.Equal(t, "space-id", op.Request.Query["space-id"].Input)
		require.Equal(t, "storage", op.Request.Query["body-format"].Literal)
		require.Equal(t, json.Number("1"), op.Request.Query["limit"].Literal)
		require.Equal(t, map[string]string{"cloud-id": "clouds", "space-id": "spaces", "page-id": "pages"}, op.Resources)
		require.Equal(t, 65536, op.MaxResponseBytes)
	}

	for _, op := range spec.SafeOutputs {
		require.Equal(t, "POST", op.Request.Method)
		require.Empty(t, op.Request.Body["body"].Input)
		require.Equal(t, map[string]any{
			"representation": "storage",
			"value":          "<p>Review the workflow result before taking action.</p>",
		}, op.Request.Body["body"].Literal)
	}
	for _, trigger := range spec.Triggers {
		require.Equal(t, "atlassian.com/forge-invocation-token", trigger.VerificationProfile)
		require.Equal(t, "page", trigger.Match["/content/type"])
		require.Equal(t, "current", trigger.Match["/content/status"])
		require.Equal(t, "/content/id", trigger.Normalize["subject"])
		require.Equal(t, "/content/space/id", trigger.Normalize["space"])
	}
}

func TestConfluenceConnectorRejectsInvalidConfiguration(t *testing.T) {
	for name, mutate := range map[string]func(*ConnectorSpec){
		"writer exposed to agent": func(spec *ConnectorSpec) {
			tool := spec.MCP["get-page"]
			tool.Credential = "page-writer"
			spec.MCP["get-page"] = tool
		},
		"agent credential used for output": func(spec *ConnectorSpec) {
			op := spec.SafeOutputs["create-runbook"]
			op.Credential = "reader"
			spec.SafeOutputs["create-runbook"] = op
		},
		"unbounded hydration": func(spec *ConnectorSpec) {
			op := spec.Context["runbook"]
			op.MaxResponseBytes = 0
			spec.Context["runbook"] = op
		},
		"wildcard tools": func(spec *ConnectorSpec) {
			tool := spec.MCP["get-page"]
			tool.Tool = "*"
			spec.MCP["get-page"] = tool
		},
		"arbitrary tenant endpoint": func(spec *ConnectorSpec) {
			spec.Endpoints["api"] = "https://api.atlassian.com/ex/confluence/tenant"
		},
		"undefined scope input": func(spec *ConnectorSpec) {
			op := spec.Context["runbook"]
			op.Resources["untrusted-page"] = "pages"
			spec.Context["runbook"] = op
		},
		"mutable read": func(spec *ConnectorSpec) {
			tool := spec.MCP["get-page"]
			tool.Request.Method = "PUT"
			spec.MCP["get-page"] = tool
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec, err := LoadSpec("testdata/providers/confluence/spec.yaml")
			require.NoError(t, err)
			mutate(spec)
			require.Error(t, spec.Validate())
		})
	}
}

func TestConfluenceConnectorUnsupportedPrimitives(t *testing.T) {
	for file, field := range map[string]string{
		"unsupported-version-preflight.yaml":  "versionPreflight",
		"unsupported-content-conversion.yaml": "encoding",
		"unsupported-webhook-verifier.yaml":   "hmac",
	} {
		t.Run(file, func(t *testing.T) {
			_, err := LoadSpec("testdata/providers/confluence/" + file)
			require.ErrorContains(t, err, `unknown field "`+field+`"`)
		})
	}
}
