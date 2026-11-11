//go:build !integration

package workflow

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const datadogIncidentID = "11111111-2222-4333-8444-555555555555"

func datadogFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../connectors/testdata/providers/datadog/" + name)
	require.NoError(t, err)
	return string(data)
}

func parseDatadogConsumer(t *testing.T, source string) (*Compiler, *WorkflowData, string, error) {
	t.Helper()
	return parseConnectorConsumer(t, "datadog-triage.md", source)
}

func datadogPlans(t *testing.T) (*connectors.Plan, *connectors.Plan) {
	t.Helper()
	_, data, _, err := parseDatadogConsumer(t, datadogFixture(t, "workflow.md"))
	require.NoError(t, err)
	config := data.Connectors["datadog"]
	require.NotNil(t, config)
	freeze := func(plan *connectors.Plan) *connectors.Plan {
		encoded, err := plan.Encode()
		require.NoError(t, err)
		decoded, err := connectors.DecodePlan(encoded)
		require.NoError(t, err)
		require.Equal(t, plan.Digest, decoded.Digest)
		return decoded
	}
	return freeze(config.Agent), freeze(config.SafeOutputs)
}

func TestDatadogConnectorLocalImportCompilation(t *testing.T) {
	source := strings.Replace(datadogFixture(t, "workflow.md"), "  staged: true\n", "", 1)
	compiler, data, path, err := parseDatadogConsumer(t, source)
	require.NoError(t, err)
	config := data.Connectors["datadog"]
	require.NotNil(t, config)
	require.Nil(t, config.Context)
	require.Equal(t, []string{"DATADOG_INCIDENT_READ_OAUTH_TOKEN", "DATADOG_MONITOR_READ_OAUTH_TOKEN"}, config.Agent.Secrets())
	require.Equal(t, []string{"DATADOG_INCIDENT_WRITE_OAUTH_TOKEN"}, config.SafeOutputs.Secrets())
	require.Equal(t, []string{"datadog_get_incident", "datadog_get_monitor"}, config.Agent.ToolNames())
	require.Equal(t, []string{"rename-incident"}, config.SafeOutputs.SafeOutputNames())
	require.Nil(t, data.SafeOutputs.CreateIssues)
	require.Contains(t, data.SafeOutputs.Scripts, "datadog-rename-incident")
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	agent, effects := connectorPhaseJobJSON(t, output)
	for _, name := range config.Agent.Secrets() {
		require.Contains(t, agent, "${{ secrets."+name+" }}")
		require.NotContains(t, effects, "${{ secrets."+name+" }}")
	}
	require.NotContains(t, agent, "${{ secrets.DATADOG_INCIDENT_WRITE_OAUTH_TOKEN }}")
	require.Contains(t, effects, "${{ secrets.DATADOG_INCIDENT_WRITE_OAUTH_TOKEN }}")
	require.NotContains(t, output, "DD-API-KEY")
	require.NotContains(t, output, "DD-APPLICATION-KEY")
	require.NotContains(t, output, `node "${RUNNER_TEMP}/gh-aw/actions/connector_runtime.cjs" invoke context`)
	require.Contains(t, output, "datadog_rename_incident")
	require.Contains(t, output, "connector_runtime.cjs")
	require.NotContains(t, output, ".gh-aw-connector-runtime")
	require.NotContains(t, output, "Build trusted connector runtime")
	require.NotContains(t, output, "connector_7_datadog")
	_, err = os.Stat(filepath.Join(compiler.gitRoot, ".github", "workflows", "connectors"))
	require.NoError(t, err, "normal imports read editable declarations")
}

func TestDatadogConnectorIndependentSelections(t *testing.T) {
	source := datadogFixture(t, "workflow.md")
	reads := "tools:\n  datadog: [get-monitor, get-incident]\n"
	effects := "safe-outputs:\n  staged: true\n  datadog-rename-incident:\n"
	for _, test := range []struct {
		name, reads, effects string
		secrets              []string
		write                bool
	}{
		{"omitted", "", "", nil, false},
		{"monitor", "tools:\n  datadog: [get-monitor]\n", "", []string{"DATADOG_MONITOR_READ_OAUTH_TOKEN"}, false},
		{"incident", "tools:\n  datadog: [get-incident]\n", "", []string{"DATADOG_INCIDENT_READ_OAUTH_TOKEN"}, false},
		{"effect", "", effects, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			consumer := strings.Replace(strings.Replace(source, reads, test.reads, 1), effects, test.effects, 1)
			if test.name != "monitor" {
				consumer = strings.Replace(consumer, `      monitors: ["12345678"]`+"\n", "", 1)
			}
			if test.name == "monitor" || test.name == "omitted" {
				consumer = strings.Replace(consumer, "      incidents: ["+datadogIncidentID+"]\n", "", 1)
			}
			_, data, _, err := parseDatadogConsumer(t, consumer)
			require.NoError(t, err)
			if test.name == "omitted" {
				require.Empty(t, data.Connectors)
				return
			}
			config := data.Connectors["datadog"]
			require.Nil(t, config.Context)
			require.Equal(t, test.write, config.SafeOutputs != nil)
			if len(test.secrets) == 0 {
				require.Nil(t, config.Agent)
			} else {
				require.Equal(t, test.secrets, config.Agent.Secrets())
			}
		})
	}
}

func TestDatadogConnectorConsumerRejections(t *testing.T) {
	source := datadogFixture(t, "workflow.md")
	for _, test := range []struct{ name, from, to string }{
		{"monitor-grant-missing", `      monitors: ["12345678"]`, ""},
		{"incident-grant-missing", "      incidents: [" + datadogIncidentID + "]", ""},
		{"monitor-wildcard", `["12345678"]`, `["*"]`},
		{"incident-wildcard", "[" + datadogIncidentID + "]", `["*"]`},
		{"numeric-id", `["12345678"]`, "[12345678]"},
		{"http-origin", "https://api.datadoghq.com", "http://api.datadoghq.com"},
		{"custom-origin", "https://api.datadoghq.com", "https://untrusted.invalid"},
		{"site-path", "https://api.datadoghq.com", "https://api.datadoghq.com/private"},
		{"effect-as-read", "[get-monitor, get-incident]", "[rename-incident]"},
		{"invented-mcp", "[get-monitor, get-incident]", "[datadog_search_logs]"},
		{"read-as-effect", "datadog-rename-incident:", "datadog-get-monitor:"},
		{"effect-options", "datadog-rename-incident:", "datadog-rename-incident: {title: bypass}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := parseDatadogConsumer(t, strings.Replace(source, test.from, test.to, 1))
			require.Error(t, err)
		})
	}
	_, _, _, err := parseDatadogConsumer(t, strings.Replace(source, "tools:\n", "context:\n  datadog: {}\ntools:\n", 1))
	require.ErrorContains(t, err, "context.datadog is unsupported")
	_, _, _, err = parseDatadogConsumer(t, strings.Replace(source, "on: workflow_dispatch", "on: datadog-monitor-alert", 1))
	require.Error(t, err, "no invented Datadog trigger may activate ingress")
}

func TestDatadogConnectorSitesAndSecretNames(t *testing.T) {
	for _, site := range []string{
		"https://api.datadoghq.com", "https://api.us3.datadoghq.com", "https://api.us5.datadoghq.com",
		"https://api.datadoghq.eu", "https://api.ap1.datadoghq.com", "https://api.ap2.datadoghq.com", "https://api.uk1.datadoghq.com",
	} {
		t.Run(site, func(t *testing.T) {
			source := strings.Replace(datadogFixture(t, "workflow.md"), "https://api.datadoghq.com", site, 1)
			source = strings.Replace(source, "      monitors:", "      monitor-reader-secret: APPROVED_MONITOR_READER\n      incident-reader-secret: APPROVED_INCIDENT_READER\n      incident-writer-secret: APPROVED_INCIDENT_WRITER\n      monitors:", 1)
			_, data, _, err := parseDatadogConsumer(t, source)
			require.NoError(t, err)
			config := data.Connectors["datadog"]
			require.Equal(t, []string{site}, config.Agent.Origins())
			require.Equal(t, []string{site}, config.SafeOutputs.Origins())
			require.Equal(t, []string{"APPROVED_INCIDENT_READER", "APPROVED_MONITOR_READER"}, config.Agent.Secrets())
			require.Equal(t, []string{"APPROVED_INCIDENT_WRITER"}, config.SafeOutputs.Secrets())
		})
	}
}

func TestDatadogConnectorXMLDocumentation(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/connectors/datadog.md")
	require.NoError(t, err)
	document := commentedConnectorDocumentation(t, string(data))
	var parsed struct {
		XMLName  xml.Name `xml:"connector-documentation"`
		ID       string   `xml:"id,attr"`
		Version  string   `xml:"version,attr"`
		Consumer struct {
			Frontmatter string `xml:"frontmatter"`
			Body        string `xml:"body"`
		} `xml:"consumer"`
		Sources []struct {
			URL string `xml:"url,attr"`
		} `xml:"sources>source"`
	}
	require.NoError(t, xml.Unmarshal([]byte(document), &parsed))
	require.Equal(t, "datadog", parsed.ID)
	require.Equal(t, "1", parsed.Version)
	require.Len(t, parsed.Sources, 8)
	consumer := "---\n" + strings.TrimSpace(parsed.Consumer.Frontmatter) + "\n---\n\n" + strings.TrimSpace(parsed.Consumer.Body) + "\n"
	require.Equal(t, datadogFixture(t, "workflow.md"), consumer)
	compiler, dataParsed, path, err := parseDatadogConsumer(t, consumer)
	require.NoError(t, err)
	require.Contains(t, GetAllowedDomains(dataParsed.NetworkPermissions),
		strings.TrimPrefix(dataParsed.Connectors["datadog"].Agent.Origins()[0], "https://"),
		"documented consumer must approve its selected API host")
	output, err := compiler.CompileToYAML(dataParsed, path)
	require.NoError(t, err)
	require.NotContains(t, output, "${{ secrets.DATADOG_INCIDENT_WRITE_OAUTH_TOKEN }}", "staged example must not inject writer credentials")
}
