//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const connectorImportHeader = `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
imports:
  - uses: connector.md
    with:
      connection: connection.yaml
`

func writeConnectorImport(t *testing.T, root string, inline bool) {
	t.Helper()
	definition := "definition: spec.yaml"
	if inline {
		data, err := os.ReadFile(filepath.Join(root, "spec.yaml"))
		require.NoError(t, err)
		definition = "spec:\n      " + strings.ReplaceAll(strings.TrimSuffix(string(data), "\n"), "\n", "\n      ")
	}
	content := "---\nimport-schema:\n  connection:\n    type: string\n    required: true\nconnectors:\n  tracker:\n    " +
		definition + "\n    connection: ${{ github.aw.import-inputs.connection }}\n---\nUse only explicitly selected tracker capabilities.\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "connector.md"), []byte(content), 0600))
}

func TestConnectorImportWithNativeSelections(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, selection       string
		agent, context, write bool
	}{
		{"registration", "", false, false, false},
		{"tools", "tools:\n  tracker: [get-issue]\n", true, false, false},
		{"context", "context:\n  tracker:\n    issue-details: {issue: ISSUE-1}\n", false, true, false},
		{"effects", "safe-outputs:\n  tracker-add-comment:\n", false, false, true},
		{"combined", "tools:\n  tracker: [get-issue]\ncontext:\n  tracker:\n    issue-details: {issue: ISSUE-1}\nsafe-outputs:\n  tracker-add-comment: {}\n", true, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, inline := range []bool{false, true} {
				c, root := connectorCompiler(t)
				writeConnectorImport(t, root, inline)
				path := filepath.Join(root, "proof.md")
				content := connectorImportHeader + test.selection + "---\nInvestigate the approved issue.\n"
				data, err := c.ParseWorkflowString(content, path)
				require.NoError(t, err)
				if test.name == "registration" {
					assert.Empty(t, data.Connectors)
					continue
				}
				config := data.Connectors["tracker"]
				require.NotNil(t, config)
				assert.Equal(t, test.agent, config.Agent != nil)
				assert.Equal(t, test.context, config.Context != nil)
				assert.Equal(t, test.write, config.SafeOutputs != nil)
				output, err := c.CompileToYAML(data, path)
				require.NoError(t, err)
				assert.Equal(t, test.agent, strings.Contains(output, "${{ secrets.CONNECTOR_READ_TOKEN }}"))
				assert.Equal(t, test.context, strings.Contains(output, "${{ secrets.CONNECTOR_CONTEXT_TOKEN }}"))
				assert.Equal(t, test.write, strings.Contains(output, "${{ secrets.CONNECTOR_WRITE_TOKEN }}"))
				require.NoError(t, os.WriteFile(path, []byte(content), 0600))
				fromFile, err := c.ParseWorkflowFile(path)
				require.NoError(t, err)
				assert.Equal(t, config.Plan.Digest, fromFile.Connectors["tracker"].Plan.Digest)
			}
		})
	}
}

func TestConnectorImportRejectsInvalidAuthority(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, header, selection, message string }{
		{"missing-with", strings.Replace(connectorImportHeader, "    with:\n      connection: connection.yaml\n", "", 1), "", "required 'with' input"},
		{"unknown-with", strings.Replace(connectorImportHeader, "connection: connection.yaml", "unknown: connection.yaml", 1), "", "unknown 'with' input"},
		{"escape", strings.Replace(connectorImportHeader, "connection: connection.yaml", "connection: ../connection.yaml", 1), "", "repository-relative"},
		{"duplicate", connectorImportHeader + "connectors:\n  tracker:\n    definition: spec.yaml\n    connection: connection.yaml\n", "", "already registered"},
		{"unknown-tool", connectorImportHeader, "tools:\n  tracker: [missing]\n", "must name exactly one"},
		{"write-as-tool", connectorImportHeader, "tools:\n  tracker: [add-comment]\n", "must name exactly one"},
		{"scope", connectorImportHeader, "context:\n  tracker:\n    issue-details: {issue: OTHER}\n", "outside approved resources"},
		{"trigger", strings.Replace(connectorImportHeader, "on: workflow_dispatch", "on:\n  tracker-issue-created:", 1), "", "ingress is not operational"},
		{"trigger-string", strings.Replace(connectorImportHeader, "on: workflow_dispatch", "on: tracker-issue-created", 1), "", "ingress is not operational"},
		{"trigger-list", strings.Replace(connectorImportHeader, "on: workflow_dispatch", "on: [workflow_dispatch, tracker-issue-created]", 1), "", "ingress is not operational"},
		{"trigger-duplicate", strings.Replace(connectorImportHeader, "on: workflow_dispatch", "on: [tracker-issue-created, tracker-issue-created]", 1), "", "duplicate trigger"},
		{"effect-options", connectorImportHeader, "safe-outputs:\n  tracker-add-comment: {token: injected}\n", "accepts null or {}"},
		{"nested-effects", connectorImportHeader, "safe-outputs:\n  tracker: [add-comment]\n", ""},
		{"unknown-effect", connectorImportHeader, "safe-outputs:\n  tracker-missing:\n", "unknown safe-output"},
		{"dot-effect", connectorImportHeader, "safe-outputs:\n  tracker.add-comment:\n", ""},
		{"unregistered", connectorImportHeader, "tools:\n  unknown-service: [get-issue]\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root := connectorCompiler(t)
			writeConnectorImport(t, root, true)
			_, err := c.ParseWorkflowString(test.header+test.selection+"---\nInvestigate.\n", filepath.Join(root, "proof.md"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.message)
		})
	}
}

func TestConnectorImportUsesNormalNestedWithDefaults(t *testing.T) {
	c, root := connectorCompiler(t)
	writeConnectorImport(t, root, true)
	outer := "---\nimport-schema:\n  approved-connection:\n    type: string\n    default: connection.yaml\nimports:\n  - uses: connector.md\n    with:\n      connection: ${{ github.aw.import-inputs.approved-connection }}\n---\nNested connector import.\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "outer.md"), []byte(outer), 0600))
	content := strings.Replace(connectorImportHeader, "  - uses: connector.md\n    with:\n      connection: connection.yaml", "  - outer.md", 1)
	data, err := c.ParseWorkflowString(content+"tools:\n  tracker: [get-issue]\n---\nInvestigate.\n", filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	require.NotNil(t, data.Connectors["tracker"].Agent)
	assert.Equal(t, []string{"CONNECTOR_READ_TOKEN"}, data.Connectors["tracker"].Agent.Secrets())
}

func TestConnectorImportWithoutParameters(t *testing.T) {
	t.Parallel()
	c, root := connectorCompiler(t)
	wrapper := "---\nconnectors:\n  tracker:\n    definition: spec.yaml\n    connection: connection.yaml\n---\nSelect tracker APIs explicitly.\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "connector.md"), []byte(wrapper), 0600))
	header := strings.Replace(connectorImportHeader, "  - uses: connector.md\n    with:\n      connection: connection.yaml", "  - connector.md", 1)
	data, err := c.ParseWorkflowString(header+"tools:\n  tracker: [get-issue]\n---\nInvestigate.\n", filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	require.NotNil(t, data.Connectors["tracker"].Agent)
}

func TestConnectorSelfContainedMarkdownExample(t *testing.T) {
	c, root := connectorCompiler(t)
	wrapper, err := os.ReadFile("../../.github/aw/connectors/example.md")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "connector.md"), wrapper, 0600))
	require.NoError(t, os.Remove(filepath.Join(root, "spec.yaml")))
	content := connectorImportHeader + "tools:\n  tracker: [get-issue]\nsafe-outputs:\n  tracker-add-comment:\n---\nInvestigate.\n"
	for _, objectForm := range []bool{false, true} {
		t.Run(map[bool]string{false: "array", true: "aw"}[objectForm], func(t *testing.T) {
			source := content
			if objectForm {
				source = strings.Replace(source, "imports:\n  - uses: connector.md\n    with:\n      connection: connection.yaml", "imports:\n  aw:\n    - uses: connector.md\n      with:\n        connection: connection.yaml", 1)
			}
			path := filepath.Join(root, "proof.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			require.NoError(t, c.CompileWorkflow(path))
		})
	}
}

func TestConnectorPrefixedEffectsDoNotShadowGitHub(t *testing.T) {
	t.Parallel()
	for _, connectorEffect := range []bool{false, true} {
		c, root := connectorCompiler(t)
		writeConnectorImport(t, root, true)
		source := connectorImportHeader + "safe-outputs:\n  add-comment:\n"
		if connectorEffect {
			source += "  tracker-add-comment:\n"
		}
		path := filepath.Join(root, "proof.md")
		data, err := c.ParseWorkflowString(source+"---\nInvestigate.\n", path)
		require.NoError(t, err)
		require.NotNil(t, data.SafeOutputs.AddComments, "unqualified add-comment must remain GitHub's built-in")
		assert.Equal(t, connectorEffect, data.Connectors["tracker"] != nil)
		output, err := c.CompileToYAML(data, path)
		require.NoError(t, err)
		assert.Equal(t, connectorEffect, strings.Contains(output, "connector_7_tracker_add_comment"))
	}
}

func TestConnectorPrefixesRejectAmbiguousAndReservedNames(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		section, name string
		ids           []string
		message       string
	}{
		{"safe-outputs", "tracker-sub-add-comment", []string{"tracker", "tracker-sub"}, "ambiguous connector prefixes"},
		{"on", "tracker-sub-created", []string{"tracker", "tracker-sub"}, "ambiguous connector prefixes"},
		{"safe-outputs", "add-comment", []string{"add"}, "cannot shadow built-in GitHub"},
		{"safe-outputs", "allowed-domains", []string{"allowed"}, "cannot shadow built-in GitHub"},
		{"safe-outputs", "linear-token", []string{"linear"}, "cannot shadow built-in GitHub"},
		{"on", "github-app", []string{"github"}, "cannot shadow built-in trigger"},
	} {
		t.Run(test.name, func(t *testing.T) {
			frontmatter := map[string]any{test.section: map[string]any{test.name: nil}}
			err := validateQualifiedConnectorNames(frontmatter, test.ids)
			require.ErrorContains(t, err, test.message)
			assert.Contains(t, frontmatter[test.section], test.name)
		})
	}
	config := map[string]any{}
	frontmatter := map[string]any{"safe-outputs": map[string]any{"tracker-long-add-comment": nil}}
	require.NoError(t, consumeQualifiedConnectorSection(frontmatter, config, "safe-outputs", "tracker-long"))
	assert.Equal(t, []string{"add-comment"}, config["safe-outputs"])
}

func TestConnectorImportAllProviderWrappers(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"linear", "jira", "azure-devops", "slack", "teams", "confluence", "sentry", "servicenow"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			fixture := filepath.Join("../connectors/testdata/providers", provider)
			require.NoError(t, os.CopyFS(root, os.DirFS(fixture)))
			dir := filepath.Join(root, ".github/aw/connectors")
			require.NoError(t, os.MkdirAll(dir, 0700))
			wrapper, err := os.ReadFile(filepath.Join("../../.github/aw/connectors", provider+".md"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, provider+".md"), wrapper, 0600))
			spec, err := os.ReadFile(filepath.Join(root, "spec.yaml"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, provider+".yaml"), spec, 0600))
			content := providerNativeImportWorkflow(t, root, provider)
			c := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
			c.gitRoot = root
			c.SetActionMode(ActionModeDev)
			c.version = "v0.0.0-1-g683ddf9617"
			path := filepath.Join(root, "proof.md")
			data, err := c.ParseWorkflowString(content, path)
			require.NoError(t, err)
			require.Contains(t, data.Connectors, provider)
			_, err = c.CompileToYAML(data, path)
			require.NoError(t, err)
		})
	}
}

func providerNativeImportWorkflow(t *testing.T, root, provider string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "workflow.md"))
	if os.IsNotExist(err) {
		content, err = os.ReadFile(filepath.Join(root, "workflow.yaml"))
	}
	require.NoError(t, err)
	var frontmatter map[string]any
	if strings.HasPrefix(string(content), "---") {
		parsed, err := parser.ExtractFrontmatterFromContent(string(content))
		require.NoError(t, err)
		frontmatter = parsed.Frontmatter
	} else {
		require.NoError(t, yaml.Unmarshal(content, &frontmatter))
	}
	registrations, ok := frontmatter["connectors"].(map[string]any)
	require.True(t, ok)
	require.Len(t, registrations, 1)
	var selected map[string]any
	for _, config := range registrations {
		selected, ok = config.(map[string]any)
		require.True(t, ok)
	}
	native := map[string]any{
		"on": "workflow_dispatch", "engine": "copilot",
		"permissions": map[string]any{"contents": "read"},
		"imports": []any{map[string]any{
			"uses": ".github/aw/connectors/" + provider + ".md",
			"with": map[string]any{"connection": "connection.yaml"},
		}},
	}
	for _, field := range []string{"tools", "context"} {
		if value, exists := selected[field]; exists {
			native[field] = map[string]any{provider: value}
		}
	}
	if effects, ok := selected["safe-outputs"].([]any); ok {
		qualified := map[string]any{}
		for _, effect := range effects {
			name, ok := effect.(string)
			require.True(t, ok)
			qualified[provider+"-"+name] = nil
		}
		native["safe-outputs"] = qualified
	}
	encoded, err := yaml.Marshal(native)
	require.NoError(t, err)
	return "---\n" + string(encoded) + "---\nUse only explicitly selected provider capabilities.\n"
}
