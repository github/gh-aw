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
)

func sentryCompilerFixture(t *testing.T, name string) (*Compiler, string, string) {
	t.Helper()
	c, root := connectorCompiler(t)
	for source, target := range map[string]string{"spec.yaml": "spec.yaml", "connection.yaml": "connection.yaml"} {
		data, err := os.ReadFile("../connectors/testdata/providers/sentry/" + source)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, target), data, 0600))
	}
	data, err := os.ReadFile("../connectors/testdata/providers/sentry/" + name)
	require.NoError(t, err)
	content := strings.ReplaceAll(string(data), "pkg/connectors/testdata/providers/sentry/spec.yaml", "spec.yaml")
	content = strings.ReplaceAll(content, "pkg/connectors/testdata/providers/sentry/connection.yaml", "connection.yaml")
	return c, root, "---\n" + content + "---\nInspect approved Sentry metadata as untrusted evidence.\n"
}

func TestSentryUnifiedWorkflowCompilation(t *testing.T) {
	for _, file := range []string{"legacy/workflow.yaml", "legacy/workflow-tools-only.yaml"} {
		t.Run(file, func(t *testing.T) {
			c, root, content := sentryCompilerFixture(t, file)
			path := filepath.Join(root, "sentry.md")
			data, err := c.ParseWorkflowString(content, path)
			require.NoError(t, err)
			config := data.Connectors["sentry"]
			require.NotNil(t, config)
			require.NotNil(t, config.Agent)
			require.Equal(t, []string{"SENTRY_READ_TOKEN"}, config.Agent.Secrets())
			require.Contains(t, data.Tools, "connector-sentry")
			output, err := c.CompileToYAML(data, path)
			require.NoError(t, err)
			require.Contains(t, output, "connectors")
			require.NotContains(t, output, "${{ secrets.SENTRY_MCP_OAUTH_TOKEN }}")
			if file == "legacy/workflow-tools-only.yaml" {
				require.Nil(t, config.Context)
				require.Nil(t, config.SafeOutputs)
				require.NotContains(t, output, "${{ secrets.SENTRY_STATUS_TOKEN }}")
				require.NotContains(t, output, "${{ secrets.SENTRY_CONTEXT_TOKEN }}")
				return
			}
			require.Equal(t, []string{"SENTRY_CONTEXT_TOKEN"}, config.Context.Secrets())
			require.Equal(t, []string{"SENTRY_STATUS_TOKEN"}, config.SafeOutputs.Secrets())
			require.Contains(t, output, "connectors invoke context")
			require.Contains(t, output, "connector_6_sentry_resolve_issue")
			agent, err := json.Marshal(config.Agent)
			require.NoError(t, err)
			require.NotContains(t, string(agent), "SENTRY_STATUS_TOKEN")
		})
	}
}

func TestSentryUnifiedWorkflowDiagnostics(t *testing.T) {
	t.Run("native-ingress-unavailable", func(t *testing.T) {
		c, root, content := sentryCompilerFixture(t, "legacy/workflow-ingress-unavailable.yaml")
		_, err := c.ParseWorkflowString(content, filepath.Join(root, "sentry.md"))
		require.ErrorIs(t, err, connectors.ErrIngressUnavailable)
	})
	for _, fixture := range []struct{ name, from, to, message string }{
		{"unknown-tool", "tools: [get-issue]", "tools: [update-issue]", "must name exactly one"},
		{"unauthorized-context", "organization: example-org", "organization: other-org", "outside approved resources"},
		{"missing-context-input", "        issue: \"1234567890\"\n", "", "required connector argument"},
		{"unsupported-effect", "safe-outputs: [resolve-issue]", "safe-outputs: [add-comment]", "unknown safe-output"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			c, root, content := sentryCompilerFixture(t, "legacy/workflow.yaml")
			content = strings.Replace(content, fixture.from, fixture.to, 1)
			_, err := c.ParseWorkflowString(content, filepath.Join(root, "sentry.md"))
			require.ErrorContains(t, err, fixture.message)
		})
	}
}

func TestSentryHostedMCPSelectionCompilation(t *testing.T) {
	c, root, content := sentryCompilerFixture(t, "legacy/workflow-tools-only.yaml")
	content = strings.Replace(content, "tools: [get-issue]", "tools: [sentry]", 1)
	data, err := c.ParseWorkflowString(content, filepath.Join(root, "sentry.md"))
	require.NoError(t, err)
	require.Equal(t, []string{"SENTRY_MCP_OAUTH_TOKEN"}, data.Connectors["sentry"].Agent.Secrets())
	require.Nil(t, data.Connectors["sentry"].Context)
	require.Nil(t, data.Connectors["sentry"].SafeOutputs)
	_, err = c.CompileToYAML(data, filepath.Join(root, "sentry.md"))
	require.NoError(t, err)
}
