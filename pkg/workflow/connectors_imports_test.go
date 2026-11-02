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
	require.NoError(t, os.Remove(filepath.Join(root, "connection.yaml")))
	header := strings.Replace(connectorImportHeader, "    with:\n      connection: connection.yaml\n", "", 1)
	content := header + "tools:\n  tracker: [get-issue]\nsafe-outputs:\n  tracker-add-comment:\n---\nInvestigate.\n"
	for _, objectForm := range []bool{false, true} {
		t.Run(map[bool]string{false: "array", true: "aw"}[objectForm], func(t *testing.T) {
			source := content
			if objectForm {
				source = strings.Replace(source, "imports:\n  - uses: connector.md", "imports:\n  aw:\n    - uses: connector.md", 1)
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
		{"safe-outputs", "github-token", []string{"github"}, "cannot shadow built-in GitHub"},
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
	doc, err := os.ReadFile("../../docs/src/content/docs/reference/connectors.md")
	require.NoError(t, err)
	_, snippet, found := strings.Cut(string(doc), "```aw wrap\n")
	require.True(t, found)
	snippet, _, found = strings.Cut(snippet, "```")
	require.True(t, found)
	for _, provider := range []string{"linear", "jira", "azure-devops", "slack", "teams", "confluence", "sentry", "servicenow"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".github/aw/connectors")
			require.NoError(t, os.MkdirAll(dir, 0700))
			wrapper, err := os.ReadFile(filepath.Join("../../.github/aw/connectors", provider+".md"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, provider+".md"), wrapper, 0600))
			fixtureDir := filepath.Join("../connectors/testdata/providers", provider)
			fixture, err := os.ReadFile(filepath.Join(fixtureDir, "workflow.md"))
			require.NoError(t, err)
			negative, err := os.ReadFile(filepath.Join(fixtureDir, "workflow-ingress-unavailable.md"))
			require.NoError(t, err)
			workflows := filepath.Join(root, ".github/workflows")
			require.NoError(t, os.MkdirAll(workflows, 0700))
			sources := []struct {
				name, content string
				reject        bool
			}{
				{"fixture", string(fixture), false},
				{"unavailable-ingress", string(negative), true},
			}
			if provider == "linear" {
				sources = append(sources, struct {
					name, content string
					reject        bool
				}{"documentation", snippet, false})
			}
			for _, source := range sources {
				t.Run(source.name, func(t *testing.T) {
					c := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
					c.gitRoot = root
					c.SetActionMode(ActionModeDev)
					c.version = "v0.0.0-1-g683ddf9617"
					path := filepath.Join(workflows, source.name+".md")
					require.NoError(t, os.WriteFile(path, []byte(source.content), 0600))
					data, err := c.ParseWorkflowFile(path)
					if source.reject {
						require.ErrorContains(t, err, "ingress is not operational")
						return
					}
					require.NoError(t, err, "ordinary Markdown imports must compile without external definition or connection files")
					require.NotNil(t, data.Connectors[provider])
					assert.Nil(t, data.Connectors[provider].Context)
					_, err = c.CompileToYAML(data, path)
					require.NoError(t, err)
				})
			}
		})
	}
}

func TestConnectorDocumentationMatchesDeclarations(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile("../../docs/src/content/docs/reference/connectors.md")
	require.NoError(t, err)
	files, err := filepath.Glob("../../.github/aw/connectors/*.md")
	require.NoError(t, err)
	for _, file := range files {
		provider := strings.TrimSuffix(filepath.Base(file), ".md")
		if provider == "example" {
			continue
		}
		t.Run(provider, func(t *testing.T) {
			_, section, found := strings.Cut(string(doc), "Declaration: `.github/aw/connectors/"+provider+".md`.")
			require.True(t, found, "the consolidated reference must document every provider")
			section, _, _ = strings.Cut(section, "\n## ")
			for _, surface := range []string{"### Secrets", "| Triggers |", "| MCPs/read tools |", "| Safe outputs |", "| Context |"} {
				assert.Contains(t, section, surface)
			}
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			frontmatter, err := parser.ExtractFrontmatterFromContent(string(data))
			require.NoError(t, err)
			declarations, ok := frontmatter.Frontmatter["connectors"].(map[string]any)
			require.True(t, ok)
			declaration, ok := declarations[provider].(map[string]any)
			require.True(t, ok)
			for field, prefix := range map[string]string{"triggers": provider + "-", "tools": "", "mcp-servers": "", "safe-outputs": provider + "-"} {
				value, present := declaration[field]
				if !present {
					continue
				}
				capabilities, ok := value.(map[string]any)
				require.True(t, ok)
				for name := range capabilities {
					assert.Contains(t, section, "`"+prefix+name+"`")
				}
			}
			inputs, ok := frontmatter.Frontmatter["import-schema"].(map[string]any)
			require.True(t, ok)
			for name, value := range inputs {
				if !strings.HasSuffix(name, "-secret") {
					continue
				}
				input, ok := value.(map[string]any)
				require.True(t, ok)
				name, ok := input["default"].(string)
				require.True(t, ok)
				assert.Contains(t, section, "`"+name+"`", "document the credential defaults actually shipped")
			}
		})
	}
}

func TestConnectorAuthoringUsesOnlyMarkdown(t *testing.T) {
	t.Parallel()
	for _, extension := range []string{"yaml", "yml"} {
		files, err := filepath.Glob("../../.github/aw/connectors/*." + extension)
		require.NoError(t, err)
		assert.Empty(t, files, "author connectors in AW Markdown; keep versioned runtime documents under testdata")
	}
}
