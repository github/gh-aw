//go:build !integration

package workflow

import (
	"encoding/base64"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const serviceNowImportIncident = "0123456789abcdef0123456789abcdef"
const serviceNowImportItem = "fedcba9876543210fedcba9876543210"

const serviceNowImportGrants = `      instance: https://example.service-now.com
      incidents: ["0123456789abcdef0123456789abcdef"]
      requested-items: ["fedcba9876543210fedcba9876543210"]
      reader-secret: APPROVED_SERVICENOW_READER
      writer-secret: APPROVED_SERVICENOW_WRITER
      reader-scope: x_example.metadata
      writer-scope: x_example.journal
`

func serviceNowImportCompiler(t *testing.T, with, selection string) (*Compiler, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "servicenow.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	source := awConnectorHeader + "imports:\n  - uses: \".github/workflows/connectors/servicenow.md\"\n"
	if with != "" {
		source += "    with:\n" + with
	}
	source += selection + "---\nRead only explicitly approved metadata.\n"
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeDev)
	compiler.version = "v0.0.0-1-gdc0f368c6c"
	return compiler, path
}

func TestServiceNowMarkdownDocumentation(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/connectors/servicenow.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---\n", 3)
	require.Len(t, parts, 3)
	body := strings.TrimSpace(parts[2])
	require.True(t, strings.HasPrefix(body, "<!--\n<connector-documentation "))
	require.Equal(t, 1, strings.Count(body, "<!--"))
	require.Equal(t, 1, strings.Count(body, "-->"))
	comment, _, found := strings.Cut(strings.TrimPrefix(body, "<!--\n"), "\n-->")
	require.True(t, found)
	assert.NotContains(t, comment, "--", "HTML comments cannot contain internal double hyphens, even in CDATA")
	assert.NotContains(t, comment, "connector_10_", "document the direct provider tool names")
	assert.Empty(t, scanImportedMarkdownSecurity(string(source)),
		"the self-contained XML comment must be accepted by the actual local-import scanner")

	var doc struct {
		XMLName        xml.Name `xml:"connector-documentation"`
		ID             string   `xml:"id,attr"`
		Version        string   `xml:"version,attr"`
		Purpose        string   `xml:"purpose"`
		Administration struct {
			Setup         string `xml:"setup"`
			Scopes        string `xml:"scopes"`
			Acquisition   string `xml:"token-acquisition"`
			Storage       string `xml:"storage"`
			Compatibility string `xml:"authentication-compatibility"`
		} `xml:"administration"`
		Configuration struct {
			Import     string `xml:"import"`
			Parameters string `xml:"parameters"`
			Resources  string `xml:"resources"`
		} `xml:"configuration"`
		Activation struct {
			Triggers string `xml:"triggers"`
		} `xml:"activation"`
		Capabilities struct {
			Reads   string `xml:"reads"`
			Effects string `xml:"safe-effects"`
		} `xml:"capabilities"`
		Boundaries   string `xml:"boundaries"`
		Verification string `xml:"verification"`
		Example      struct {
			Instructions string `xml:"instructions"`
			YAML         string `xml:"frontmatter"`
			Prompt       string `xml:"body"`
		} `xml:"example"`
		Sources struct {
			Entries []struct {
				URL  string `xml:"url,attr"`
				Text string `xml:",chardata"`
			} `xml:"source"`
		} `xml:"sources"`
	}
	require.NoError(t, xml.Unmarshal([]byte(comment), &doc))
	assert.Equal(t, "servicenow", doc.ID)
	assert.Equal(t, "1", doc.Version)
	for _, section := range []string{
		doc.Purpose, doc.Administration.Setup, doc.Administration.Scopes,
		doc.Administration.Acquisition, doc.Administration.Storage, doc.Administration.Compatibility,
		doc.Configuration.Import, doc.Configuration.Parameters, doc.Configuration.Resources,
		doc.Activation.Triggers, doc.Capabilities.Reads, doc.Capabilities.Effects,
		doc.Boundaries, doc.Verification, doc.Example.Instructions, doc.Example.YAML, doc.Example.Prompt,
	} {
		assert.NotEmpty(t, strings.TrimSpace(section))
	}
	assert.GreaterOrEqual(t, len(doc.Sources.Entries), 4)
	assert.Contains(t, doc.Administration.Scopes, "not universal ServiceNow OAuth scopes")
	assert.Contains(t, doc.Verification, "not that")
	assert.Contains(t, doc.Verification, "persisted durably")
	require.Contains(t, comment, "<frontmatter><![CDATA[")
	require.Contains(t, comment, "<body><![CDATA[")

	var consumer map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(doc.Example.YAML), &consumer))
	assert.Equal(t, "workflow_dispatch", consumer["on"])
	effects, ok := consumer["safe-outputs"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, effects["staged"])

	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows", "connectors")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "servicenow.md"), source, 0600))
	path := filepath.Join(root, ".github", "workflows", "servicenow-review.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	content := "---\n" + strings.TrimSpace(doc.Example.YAML) + "\n---\n" + strings.TrimSpace(doc.Example.Prompt) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeDev)
	compiler.version = "v0.0.0-1-gdc0f368c6c"
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	config := data.Connectors["servicenow"]
	require.NotNil(t, config)
	assert.Nil(t, config.Context)
	require.NotNil(t, config.Agent)
	require.NotNil(t, config.SafeOutputs)
	assert.Equal(t, []string{"SERVICENOW_READ_TOKEN"}, config.Agent.Secrets())
	assert.Equal(t, []string{"SERVICENOW_WRITE_TOKEN"}, config.SafeOutputs.Secrets())
	assert.True(t, templatableBoolIsTrue(data.SafeOutputs.Staged))
	for _, operation := range []string{"append-incident-work-note", "append-requested-item-work-note"} {
		key := "servicenow-" + operation
		script := data.SafeOutputs.Scripts[key]
		require.NotNil(t, script)
		tool := generateCustomScriptToolDefinition(key, script)
		assert.Equal(t, key, tool["name"])
	}
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	assert.Contains(t, output, "servicenow_append_incident_work_note")
	assert.Contains(t, output, "servicenow_append_requested_item_work_note")
	assert.NotContains(t, output, "${{ secrets.SERVICENOW_WRITE_TOKEN }}", "staging must not inject writer credentials")
	assert.NotContains(t, output, "connector_10_servicenow_")
	for _, removed := range []string{"Checkout trusted connector runtime", "Build trusted connector runtime"} {
		assert.NotContains(t, output, removed)
	}
	require.NoError(t, compiler.CompileWorkflow(path), "compile the actual extracted consumer, not a parse-only proxy")
}

func TestServiceNowMarkdownIndependentSelections(t *testing.T) {
	for _, test := range []struct {
		name, selection string
		read, write     bool
	}{
		{"registration", "", false, false},
		{"incident-read", "tools:\n  servicenow: [get-incident]\n", true, false},
		{"item-read", "tools:\n  servicenow: [get-requested-item]\n", true, false},
		{"incident-write", "safe-outputs:\n  servicenow-append-incident-work-note:\n", false, true},
		{"item-write", "safe-outputs:\n  servicenow-append-requested-item-work-note: {}\n", false, true},
		{"combined", "tools:\n  servicenow: [get-incident, get-requested-item]\nsafe-outputs:\n  servicenow-append-incident-work-note:\n  servicenow-append-requested-item-work-note:\n", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, path := serviceNowImportCompiler(t, serviceNowImportGrants, test.selection)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			if test.read || test.write {
				config := data.Connectors["servicenow"]
				require.NotNil(t, config)
				assert.Equal(t, test.read, config.Agent != nil)
				assert.Equal(t, test.write, config.SafeOutputs != nil)
				assert.Nil(t, config.Context)
				if test.read {
					assert.Equal(t, []string{"APPROVED_SERVICENOW_READER"}, config.Agent.Secrets())
					assert.Equal(t, []string{"https://example.service-now.com"}, config.Agent.Origins())
					tool, ok := data.Tools["servicenow"].(map[string]any)
					require.True(t, ok)
					assert.Equal(t, "node", tool["entrypoint"])
					assert.Equal(t, []string{"${RUNNER_TEMP}/gh-aw/actions/connector_runtime.cjs", "serve"}, tool["entrypointArgs"])
				}
				if test.write {
					assert.Equal(t, []string{"APPROVED_SERVICENOW_WRITER"}, config.SafeOutputs.Secrets())
				}
				encoded, err := config.Plan.Encode()
				require.NoError(t, err)
				plan, err := base64.StdEncoding.DecodeString(encoded)
				require.NoError(t, err)
				if test.read {
					assert.Contains(t, string(plan), "x_example.metadata")
				}
				if test.write {
					assert.Contains(t, string(plan), "x_example.journal")
				}
			} else {
				assert.Empty(t, data.Connectors)
			}
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			assertServiceNowMarkdownCredentials(t, output, test.read, test.write)
			_, err = os.Stat(filepath.Join(compiler.gitRoot, ".github", "workflows", "connectors"))
			assert.NoError(t, err, "normal imports use editable checkout declarations")
		})
	}
	for _, test := range []struct{ grant, selection string }{
		{"incidents", "tools:\n  servicenow: [get-incident]\n"},
		{"requested-items", "tools:\n  servicenow: [get-requested-item]\n"},
		{"incidents", "safe-outputs:\n  servicenow-append-incident-work-note:\n"},
		{"requested-items", "safe-outputs:\n  servicenow-append-requested-item-work-note:\n"},
	} {
		t.Run("single-grant-"+test.selection, func(t *testing.T) {
			id := serviceNowImportIncident
			if test.grant == "requested-items" {
				id = serviceNowImportItem
			}
			compiler, path := serviceNowImportCompiler(t, "      "+test.grant+": [\""+id+"\"]\n", test.selection)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err, "unselected table grants must not be required")
			_, err = compiler.CompileToYAML(data, path)
			require.NoError(t, err)
		})
	}
}

func assertServiceNowMarkdownCredentials(t *testing.T, output string, read, write bool) {
	t.Helper()
	var workflow struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &workflow))
	agent, err := yaml.Marshal(workflow.Jobs["agent"])
	require.NoError(t, err)
	effects, err := yaml.Marshal(workflow.Jobs["safe_outputs"])
	require.NoError(t, err)
	assert.Equal(t, read, strings.Contains(string(agent), "${{ secrets.APPROVED_SERVICENOW_READER }}"))
	assert.NotContains(t, string(agent), "${{ secrets.APPROVED_SERVICENOW_WRITER }}")
	assert.Equal(t, write, strings.Contains(string(effects), "${{ secrets.APPROVED_SERVICENOW_WRITER }}"))
	assert.NotContains(t, string(effects), "${{ secrets.APPROVED_SERVICENOW_READER }}")
	assert.NotContains(t, output, "SERVICENOW_CONTEXT_TOKEN")
}

func TestServiceNowMarkdownRejectsUnsupportedAuthority(t *testing.T) {
	for _, test := range []struct{ name, with, selection string }{
		{"no-grants", "", "tools:\n  servicenow: [get-incident]\n"},
		{"wrong-table-grant", "      requested-items: [\"" + serviceNowImportItem + "\"]\n", "tools:\n  servicenow: [get-incident]\n"},
		{"no-write-grants", "", "safe-outputs:\n  servicenow-append-incident-work-note:\n"},
		{"wildcard", "      incidents: ['*']\n", "tools:\n  servicenow: [get-incident]\n"},
		{"http", "      instance: http://example.service-now.com\n", "tools:\n  servicenow: [get-incident]\n"},
		{"instance-path", "      instance: https://example.service-now.com/api\n", "tools:\n  servicenow: [get-incident]\n"},
		{"unknown-tool", serviceNowImportGrants, "tools:\n  servicenow: [get-record]\n"},
		{"write-as-read", serviceNowImportGrants, "tools:\n  servicenow: [append-incident-work-note]\n"},
		{"context", serviceNowImportGrants, "context:\n  servicenow:\n    incident-metadata: {record: " + serviceNowImportIncident + "}\n"},
		{"unknown-effect-option", serviceNowImportGrants, "safe-outputs:\n  servicenow-append-incident-work-note: {unknown: true}\n"},
		{"general-update", serviceNowImportGrants, "safe-outputs:\n  servicenow-update-record:\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, path := serviceNowImportCompiler(t, test.with, test.selection)
			_, err := compiler.ParseWorkflowFile(path)
			require.Error(t, err)
		})
	}
	for _, trigger := range []string{"servicenow-incident-updated", "servicenow-requested-item-updated"} {
		t.Run(trigger, func(t *testing.T) {
			compiler, path := serviceNowImportCompiler(t, serviceNowImportGrants, "")
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			content = []byte(strings.Replace(string(content), "on: workflow_dispatch", "on:\n  "+trigger+":", 1))
			require.NoError(t, os.WriteFile(path, content, 0600))
			_, err = compiler.ParseWorkflowFile(path)
			require.ErrorContains(t, err, "gateway-dispatch")
		})
	}
}
