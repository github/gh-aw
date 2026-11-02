//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func connectorCompiler(t *testing.T) (*Compiler, string) {
	t.Helper()
	root := t.TempDir()
	spec, err := os.ReadFile("../connectors/testdata/example.yaml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "spec.yaml"), spec, 0600))
	connection := `apiVersion: github.com/gh-aw/connectors/v1
kind: ConnectorConnection
connector: example.com/issues
credentials:
  reader: {secret: CONNECTOR_READ_TOKEN}
  writer: {secret: CONNECTOR_WRITE_TOKEN}
  context-reader: {secret: CONNECTOR_CONTEXT_TOKEN}
resources:
  issues: [ISSUE-1]
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "connection.yaml"), []byte(connection), 0600))
	c := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	c.gitRoot = root
	c.SetActionMode(ActionModeDev)
	c.version = "v0.0.0-1-gabcdef0"
	return c, root
}

func connectorWorkflow(selections string) string {
	return "---\nname: connector-proof\non: workflow_dispatch\nengine: copilot\n" +
		"permissions:\n  contents: read\nconnectors:\n  tracker:\n    definition: spec.yaml\n    connection: connection.yaml\n" +
		selections + "\n---\nInvestigate the approved issue using only the selected capabilities.\n"
}

func TestConnectorIndependentFrontmatter(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, selection       string
		agent, context, write bool
	}{
		{"registration", "", false, false, false},
		{"tools", "    tools: [get-issue]\n", true, false, false},
		{"context", "    context:\n      issue-details: {issue: ISSUE-1}\n", false, true, false},
		{"safe-outputs", "    safe-outputs: [add-comment]\n", false, false, true},
		{"combined", "    tools: [get-issue]\n    context:\n      issue-details: {issue: ISSUE-1}\n    safe-outputs: [add-comment]\n", true, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root := connectorCompiler(t)
			data, err := c.ParseWorkflowString(connectorWorkflow(test.selection), filepath.Join(root, "proof.md"))
			require.NoError(t, err)
			if test.name == "registration" {
				assert.Empty(t, data.Connectors)
				assert.NotContains(t, data.Tools, "connector-tracker")
				return
			}
			require.Contains(t, data.Connectors, "tracker")
			config := data.Connectors["tracker"]
			assert.Equal(t, test.agent, config.Agent != nil)
			assert.Equal(t, test.context, config.Context != nil)
			assert.Equal(t, test.write, config.SafeOutputs != nil)
			if test.agent {
				assert.Equal(t, []string{"CONNECTOR_READ_TOKEN"}, config.Agent.Secrets())
				assert.Contains(t, data.Tools, "connector-tracker")
				tool, ok := data.Tools["connector-tracker"].(map[string]any)
				require.True(t, ok)
				encoded, err := json.Marshal(tool)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "CONNECTOR_WRITE_TOKEN")
				assert.NotContains(t, string(encoded), "CONNECTOR_CONTEXT_TOKEN")
			}
			if test.context {
				assert.Equal(t, []string{"CONNECTOR_CONTEXT_TOKEN"}, config.Context.Secrets())
			}
			if test.write {
				assert.Equal(t, []string{"CONNECTOR_WRITE_TOKEN"}, config.SafeOutputs.Secrets())
				require.NotNil(t, data.SafeOutputs)
				assert.Contains(t, data.SafeOutputs.Scripts, "connector-7-tracker-add-comment")
				script := data.SafeOutputs.Scripts["connector-7-tracker-add-comment"]
				tool := generateCustomScriptToolDefinition("connector-7-tracker-add-comment", script)
				schema, ok := tool["inputSchema"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, false, schema["additionalProperties"])
				properties, ok := schema["properties"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, map[string]any{"type": "string", "maxLength": 4000}, properties["text"])
			}
		})
	}
}

func TestConnectorOnlySelectedCredentialsAreRequired(t *testing.T) {
	t.Parallel()
	c, root := connectorCompiler(t)
	path := filepath.Join(root, "connection.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	connection := strings.ReplaceAll(string(data), "  reader: {secret: CONNECTOR_READ_TOKEN}\n", "")
	connection = strings.ReplaceAll(connection, "  context-reader: {secret: CONNECTOR_CONTEXT_TOKEN}\n", "")
	require.NoError(t, os.WriteFile(path, []byte(connection), 0600))
	_, err = c.ParseWorkflowString(connectorWorkflow("    safe-outputs: [add-comment]\n"), filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	_, err = c.ParseWorkflowString(connectorWorkflow("    tools: [get-issue]\n"), filepath.Join(root, "proof.md"))
	require.ErrorContains(t, err, "no connection credential")
}

func TestConnectorCompilationStaysStaged(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"staged", "trial", "dry-run"} {
		t.Run(mode, func(t *testing.T) {
			c, root := connectorCompiler(t)
			c.forceStaged = mode == "staged"
			c.trialMode = mode == "trial"
			c.dryRun = mode == "dry-run"
			data, err := c.ParseWorkflowString(connectorWorkflow("    safe-outputs: [add-comment]\n"), filepath.Join(root, "proof.md"))
			require.NoError(t, err)
			script := data.SafeOutputs.Scripts["connector-7-tracker-add-comment"]
			assert.Contains(t, script.Script, "const staged = true || isStagedMode();")
			assert.Contains(t, script.Script, "staged ? '--stage' : '--allow-writes'")
			if mode == "dry-run" {
				_, err := c.CompileToYAML(data, filepath.Join(root, "proof.md"))
				require.ErrorContains(t, err, "--dry-run cannot safely execute")
			}
		})
	}
}

func TestConnectorGeneratedSafeOutputHost(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required to exercise the generated safe-output host")
	}
	for _, mode := range []struct {
		name              string
		compiled, runtime bool
	}{
		{"live", false, false},
		{"compile-staged", true, false},
		{"runtime-staged", false, true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			source, err := json.Marshal(connectorSafeOutputScript("frozen", "add-comment", "PREFIX_", []string{"WRITER"}, mode.compiled))
			require.NoError(t, err)
			driver := fmt.Sprintf(`
const assert = require('node:assert/strict');
let call;
const adaptedRequire = name => {
  if (name === 'node:child_process') return {spawnSync: (binary, args, options) => {
    call = {binary, args, options};
    return {status: 0, stdout: '{"ok":true}'};
  }};
  if (name === './safe_output_helpers.cjs') return {isStagedMode: () => %t};
  return require(name);
};
const result = new Function('item', 'require', 'process', %s)(
  {type: 'connector_effect', issue: 'ISSUE-1', text: 'safe'},
  adaptedRequire,
  {env: {RUNNER_TEMP: '/trusted', PREFIX_WRITER: 'test-token', OTHER: 'not-inherited'}}
);
const staged = %t;
assert.deepEqual(result, {ok:true});
assert.equal(call.binary, require('node:path').join('/trusted', 'gh-aw', 'gh-aw'));
assert.deepEqual(call.args, ['connectors','invoke','safe-output','add-comment',staged ? '--stage' : '--allow-writes']);
assert.deepEqual(JSON.parse(call.options.input), {issue:'ISSUE-1',text:'safe'});
assert.deepEqual(call.options.env, staged ? {GH_AW_CONNECTOR_PLAN:'frozen'} : {GH_AW_CONNECTOR_PLAN:'frozen',GH_AW_CONNECTOR_SECRET_WRITER:'test-token'});
assert.equal(call.options.shell, undefined);
`, mode.runtime, source, mode.compiled || mode.runtime)
			output, err := exec.CommandContext(t.Context(), "node", "-e", driver).CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}

func TestConnectorCompilerDiagnostics(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, selection, message string }{
		{"unknown-option", "    unknow: [get-issue]\n", ""},
		{"unknown-read", "    tools: [missing]\n", "must name exactly one"},
		{"write-in-tools", "    tools: [add-comment]\n", "must name exactly one"},
		{"unknown-write", "    safe-outputs: [missing]\n", "unknown safe-output"},
		{"unknown-context", "    context:\n      missing: {}\n", "unknown context"},
		{"missing-context-input", "    context:\n      issue-details: {}\n", "required connector argument"},
		{"unapproved-context", "    context:\n      issue-details: {issue: OTHER}\n", "outside approved resources"},
		{"unknown-context-input", "    context:\n      issue-details: {issue: ISSUE-1, extra: true}\n", "unknown connector argument"},
		{"mcp-collision", "    tools: [get-issue]\nmcp-servers:\n  connector-tracker:\n    command: echo\n", "conflicts"},
		{"duplicate-read", "    tools: [get-issue, get-issue]\n", ""},
		{"empty-selection", "    tools: []\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root := connectorCompiler(t)
			_, err := c.ParseWorkflowString(connectorWorkflow(test.selection), filepath.Join(root, "proof.md"))
			require.Error(t, err)
			if test.message != "" {
				assert.ErrorContains(t, err, test.message)
			}
		})
	}
	t.Run("trigger-ingress", func(t *testing.T) {
		c, root := connectorCompiler(t)
		content := strings.Replace(connectorWorkflow("    on: [issue-created]\n"), "on: workflow_dispatch\n", "", 1)
		_, err := c.ParseWorkflowString(content, filepath.Join(root, "proof.md"))
		require.ErrorIs(t, err, connectors.ErrIngressUnavailable)
		require.ErrorContains(t, err, "connectors.tracker: on:")
	})
}

func TestConnectorCompilesIntoRuntimeBoundaries(t *testing.T) {
	c, root := connectorCompiler(t)
	selections := "    tools: [get-issue]\n    context:\n      issue-details: {issue: ISSUE-1}\n    safe-outputs: [add-comment]\n"
	data, err := c.ParseWorkflowString(connectorWorkflow(selections), filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	output, err := c.CompileToYAML(data, filepath.Join(root, "proof.md"))
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
	assert.Contains(t, string(agent), "CONNECTOR_CONTEXT_TOKEN")
	assert.Contains(t, string(agent), "CONNECTOR_READ_TOKEN")
	assert.NotContains(t, string(agent), "${{ secrets.CONNECTOR_WRITE_TOKEN }}")
	assert.Contains(t, string(safe), "${{ secrets.CONNECTOR_WRITE_TOKEN }}")
	assert.NotContains(t, string(safe), "${{ secrets.CONNECTOR_CONTEXT_TOKEN }}")
	assert.Contains(t, output, connectorContextPath)
	assert.Contains(t, output, "untrusted third-party data")
	assert.Contains(t, output, "connector_7_tracker_add_comment")
	assert.Contains(t, string(agent), "connectors")
	assert.Contains(t, string(agent), "serve")
	assert.NotContains(t, output, "api.example.com/issues/{issue}")
}

func TestConnectorFileAndStringCompilationAgree(t *testing.T) {
	c, root := connectorCompiler(t)
	path := filepath.Join(root, "proof.md")
	content := connectorWorkflow("    tools: [get-issue]\n")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	fromFile, err := c.ParseWorkflowFile(path)
	require.NoError(t, err)
	fromString, err := c.ParseWorkflowString(content, path)
	require.NoError(t, err)
	assert.Equal(t, fromFile.Connectors["tracker"].Plan.Digest, fromString.Connectors["tracker"].Plan.Digest)
	require.NotNil(t, fromFile.SourceDocument)
}

func TestConnectorImportAuthorityIsExplicit(t *testing.T) {
	t.Parallel()
	frontmatter := map[string]any{"connectors": map[string]any{"tracker": map[string]any{"definition": "spec.yaml", "connection": "connection.yaml"}}}
	require.ErrorContains(t, parser.ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatter, "shared.md"), "main workflow")
}
