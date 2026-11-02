//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func jiraConnectorCompiler(t *testing.T, fixture string) (*Compiler, string, string) {
	t.Helper()
	root := t.TempDir()
	provider := filepath.Join("..", "connectors", "testdata", "providers", "jira")
	for source, target := range map[string]string{
		filepath.Join(provider, "spec.yaml"):       "pkg/connectors/testdata/providers/jira/spec.yaml",
		filepath.Join(provider, "connection.yaml"): "pkg/connectors/testdata/providers/jira/connection.yaml",
	} {
		data, err := os.ReadFile(source)
		require.NoError(t, err)
		destination := filepath.Join(root, target)
		require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0700))
		require.NoError(t, os.WriteFile(destination, data, 0600))
	}
	content, err := os.ReadFile(filepath.Join(provider, fixture))
	require.NoError(t, err)
	c := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	c.gitRoot = root
	c.SetActionMode(ActionModeDev)
	c.version = "v0.0.0-1-g683ddf9617"
	return c, root, string(content)
}

func TestJiraConnectorWorkflowCompilation(t *testing.T) {
	c, root, content := jiraConnectorCompiler(t, "legacy/workflow.md")
	data, err := c.ParseWorkflowString(content, filepath.Join(root, "jira.md"))
	require.NoError(t, err)
	require.Contains(t, data.Connectors, "jira")
	config := data.Connectors["jira"]
	assert.Equal(t, []string{"JIRA_MCP_SERVICE_ACCOUNT_KEY"}, config.Agent.Secrets())
	assert.Equal(t, []string{"JIRA_CONTEXT_BASIC_AUTH"}, config.Context.Secrets())
	assert.Equal(t, []string{"JIRA_REST_BASIC_AUTH"}, config.SafeOutputs.Secrets())
	assert.Equal(t, []string{"add-comment"}, config.SafeOutputs.SafeOutputNames())
	assert.Equal(t, map[string]any{"issue": "APP-1"}, config.ContextArgs["issue-details"])
	assert.Equal(t, []string{"service-account__getJiraIssue", "service-account__searchJiraIssuesUsingJql"}, config.Agent.ToolNames())
	output, err := c.CompileToYAML(data, filepath.Join(root, "jira.md"))
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(output), &document))
	jobs, ok := document["jobs"].(map[string]any)
	require.True(t, ok)
	agent, err := json.Marshal(jobs["agent"])
	require.NoError(t, err)
	safe, err := json.Marshal(jobs["safe_outputs"])
	require.NoError(t, err)
	assert.Contains(t, string(agent), "connectors invoke context")
	assert.Contains(t, string(agent), "${{ secrets.JIRA_MCP_SERVICE_ACCOUNT_KEY }}")
	assert.Contains(t, string(agent), "${{ secrets.JIRA_CONTEXT_BASIC_AUTH }}")
	assert.NotContains(t, string(agent), "${{ secrets.JIRA_REST_BASIC_AUTH }}")
	assert.NotContains(t, output, "${{ secrets.JIRA_MCP_PERSONAL_BASIC_AUTH }}")
	assert.Contains(t, string(safe), "${{ secrets.JIRA_REST_BASIC_AUTH }}")
	assert.NotContains(t, string(safe), "${{ secrets.JIRA_CONTEXT_BASIC_AUTH }}")
	assert.Contains(t, output, "untrusted third-party data")
	assert.Contains(t, output, "connector_4_jira_add_comment")
	assert.NotContains(t, output, "connector_4_jira_create_issue")
}

const jiraWorkflowSelections = "    tools: [service-account]\n    context:\n      issue-details:\n        issue: APP-1\n    safe-outputs: [add-comment]\n"

func TestJiraConnectorIndependentSelections(t *testing.T) {
	for _, test := range []struct {
		name, selection       string
		agent, context, write bool
	}{
		{"registration", "", false, false, false},
		{"tools", "    tools: [personal]\n", true, false, false},
		{"context", "    context:\n      issue-details: {issue: APP-1}\n", false, true, false},
		{"safe-outputs", "    safe-outputs: [create-issue, update-issue, add-comment, add-label]\n", false, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root, content := jiraConnectorCompiler(t, "legacy/workflow.md")
			require.Contains(t, content, jiraWorkflowSelections)
			content = strings.Replace(content, jiraWorkflowSelections, test.selection, 1)
			data, err := c.ParseWorkflowString(content, filepath.Join(root, "jira.md"))
			require.NoError(t, err)
			if test.name == "registration" {
				assert.Empty(t, data.Connectors)
				assert.NotContains(t, data.Tools, "connector-jira")
				return
			}
			config := data.Connectors["jira"]
			require.NotNil(t, config)
			assert.Equal(t, test.agent, config.Agent != nil)
			assert.Equal(t, test.context, config.Context != nil)
			assert.Equal(t, test.write, config.SafeOutputs != nil)
			if test.agent {
				assert.Equal(t, []string{"JIRA_MCP_PERSONAL_BASIC_AUTH"}, config.Agent.Secrets())
			}
			if test.write {
				assert.Len(t, config.SafeOutputs.SafeOutputNames(), 4)
				script := data.SafeOutputs.Scripts["connector-4-jira-add-comment"]
				require.NotNil(t, script)
				schema, err := config.SafeOutputs.InputSchema("safe-output", "add-comment")
				require.NoError(t, err)
				assert.Equal(t, false, schema["additionalProperties"])
				assert.Contains(t, script.Script, "connectors")
			}
			_, err = c.CompileToYAML(data, filepath.Join(root, "jira.md"))
			require.NoError(t, err)
		})
	}
}

func TestJiraConnectorCompilationRejections(t *testing.T) {
	for _, test := range []struct{ name, selection, message string }{
		{"write-in-tools", "    tools: [add-comment]\n", "must name exactly one"},
		{"native-tool-outside-registration", "    tools: [getJiraIssue]\n", "does not match pattern"},
		{"unapproved-context", "    context:\n      issue-details: {issue: OTHER-1}\n", "outside approved resources"},
		{"expression-context", "    context:\n      issue-details: {issue: '${{ github.event.issue.key }}'}\n", ""},
		{"missing-context-input", "    context:\n      issue-details: {}\n", "required connector argument"},
		{"context-list", "    context: [issue-details]\n", ""},
		{"undeclared-effect", "    safe-outputs: [transition-issue]\n", "unknown safe-output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root, content := jiraConnectorCompiler(t, "legacy/workflow.md")
			content = strings.Replace(content, jiraWorkflowSelections, test.selection, 1)
			_, err := c.ParseWorkflowString(content, filepath.Join(root, "jira.md"))
			require.Error(t, err)
			if test.message != "" {
				assert.ErrorContains(t, err, test.message)
			}
		})
	}
	t.Run("authenticated-ingress-required", func(t *testing.T) {
		c, root, content := jiraConnectorCompiler(t, "legacy/workflow-trigger-unavailable.md")
		_, err := c.ParseWorkflowString(content, filepath.Join(root, "jira.md"))
		require.ErrorIs(t, err, connectors.ErrIngressUnavailable)
		assert.ErrorContains(t, err, "connectors.jira: on:")
	})
	for _, root := range []string{
		"tools:\n  connectors: [jira]\n",
		"safe-outputs:\n  connectors: [jira]\n",
		"on:\n  connectors: [jira]\n",
	} {
		t.Run("scattered-"+strings.Split(root, ":")[0], func(t *testing.T) {
			c, directory, content := jiraConnectorCompiler(t, "legacy/workflow.md")
			content = strings.Replace(content, jiraWorkflowSelections, "", 1)
			content = strings.Replace(content, "on: workflow_dispatch\n", root, 1)
			if !strings.HasPrefix(root, "on:") {
				content = strings.Replace(content, root, "on: workflow_dispatch\n"+root, 1)
			}
			_, err := c.ParseWorkflowString(content, filepath.Join(directory, "jira.md"))
			require.Error(t, err)
		})
	}
}
