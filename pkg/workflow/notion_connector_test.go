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

const notionPageID = "11111111-2222-4333-8444-555555555555"

type notionDocumentation struct {
	XMLName xml.Name `xml:"connector-documentation"`
	ID      string   `xml:"id,attr"`
	Version string   `xml:"version,attr"`
	Example struct {
		Frontmatter string `xml:"frontmatter"`
		Body        string `xml:"body"`
	} `xml:"example"`
}

func notionConsumerExample(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("../../.github/workflows/connectors/notion.md")
	require.NoError(t, err)
	comment := standaloneConnectorDocumentation(t, string(source))
	var documentation notionDocumentation
	require.NoError(t, xml.Unmarshal([]byte(comment), &documentation))
	require.Equal(t, "notion", documentation.ID)
	require.Equal(t, "1", documentation.Version)
	require.NotEmpty(t, documentation.Example.Frontmatter)
	require.NotEmpty(t, documentation.Example.Body)
	return "---\n" + strings.TrimSpace(documentation.Example.Frontmatter) +
		"\n---\n" + strings.TrimSpace(documentation.Example.Body) + "\n"
}

func parseNotionConsumer(t *testing.T, source string) (*Compiler, *WorkflowData, string, error) {
	t.Helper()
	return parseConnectorConsumer(t, "notion.md", source)
}

func TestNotionConnectorDocumentedConsumer(t *testing.T) {
	compiler, data, path, err := parseNotionConsumer(t, notionConsumerExample(t))
	require.NoError(t, err)
	config := data.Connectors["notion"]
	require.NotNil(t, config)
	require.Nil(t, config.Context)
	require.Equal(t, []string{"NOTION_READ_TOKEN"}, config.Agent.Secrets())
	require.Equal(t, []string{"NOTION_COMMENT_WRITE_TOKEN"}, config.SafeOutputs.Secrets())
	require.Equal(t, []string{"get_page"}, config.Agent.ToolNames())
	require.Equal(t, []string{"add-comment"}, config.SafeOutputs.SafeOutputNames())
	require.True(t, templatableBoolIsTrue(data.SafeOutputs.Staged))
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	agent, effects := connectorPhaseJobJSON(t, output)
	require.Contains(t, agent, "${{ secrets.NOTION_READ_TOKEN }}")
	require.NotContains(t, agent, "${{ secrets.NOTION_COMMENT_WRITE_TOKEN }}")
	require.NotContains(t, effects, "${{ secrets.NOTION_READ_TOKEN }}")
	require.NotContains(t, effects, "${{ secrets.NOTION_COMMENT_WRITE_TOKEN }}", "staged effects must not materialize credentials")
	require.NotContains(t, output, "connectors invoke context")
	_, err = os.Stat(filepath.Join(compiler.gitRoot, ".github", "workflows", "connectors"))
	require.NoError(t, err, "normal imports use editable consumer-side declarations")

	compiler, data, path, err = parseNotionConsumer(t, strings.Replace(notionConsumerExample(t), "  staged: true\n", "", 1))
	require.NoError(t, err)
	output, err = compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	agent, effects = connectorPhaseJobJSON(t, output)
	require.NotContains(t, agent, "${{ secrets.NOTION_COMMENT_WRITE_TOKEN }}")
	require.Contains(t, effects, "${{ secrets.NOTION_COMMENT_WRITE_TOKEN }}")
	require.NotContains(t, effects, "${{ secrets.NOTION_READ_TOKEN }}")
	require.Contains(t, data.Tools, "notion")
	require.NotContains(t, data.Tools, "connector-notion")
	require.Contains(t, output, "notion_add_comment")
	require.NotContains(t, output, "connector_6_notion")
	require.NotContains(t, output, "Checkout trusted connector runtime")
	require.NotContains(t, output, "Build trusted connector runtime")
	require.Contains(t, output, "connector_runtime.cjs")
}

func TestNotionConnectorIndependentSelections(t *testing.T) {
	source := notionConsumerExample(t)
	for _, selection := range []struct {
		name        string
		read, write bool
	}{
		{"omitted", false, false},
		{"read", true, false},
		{"write", false, true},
		{"combined", true, true},
	} {
		t.Run(selection.name, func(t *testing.T) {
			consumer := source
			if !selection.read {
				consumer = strings.Replace(consumer, "tools:\n  notion: [get-page]\n", "", 1)
			}
			if !selection.write {
				consumer = strings.Replace(consumer, "safe-outputs:\n  staged: true\n  notion-add-comment:\n", "", 1)
			}
			_, data, _, err := parseNotionConsumer(t, consumer)
			require.NoError(t, err)
			if !selection.read && !selection.write {
				require.Empty(t, data.Connectors)
				return
			}
			config := data.Connectors["notion"]
			require.NotNil(t, config)
			require.Equal(t, selection.read, config.Agent != nil)
			require.Equal(t, selection.write, config.SafeOutputs != nil)
			require.Nil(t, config.Context)
		})
	}
}

func TestNotionConnectorConsumerRejections(t *testing.T) {
	source := notionConsumerExample(t)
	for _, test := range []struct{ name, from, to string }{
		{"empty-grant", "pages: [" + notionPageID + "]", "pages: []"},
		{"wildcard", "pages: [" + notionPageID + "]", "pages: ['*']"},
		{"missing-import", "    with:\n      pages: [" + notionPageID + "]", "    with: {}"},
		{"write-as-read", "notion: [get-page]", "notion: [add-comment]"},
		{"read-as-write", "notion-add-comment:", "notion-get-page:"},
		{"unknown-tool", "notion: [get-page]", "notion: [missing]"},
		{"native-ingress", "on: workflow_dispatch", "on: notion-page-updated"},
		{"public-context", "engine: copilot", "context:\n  notion: {}\nengine: copilot"},
		{"effect-options", "notion-add-comment:", "notion-add-comment: {target: '*'}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Contains(t, source, test.from)
			_, _, path, err := parseNotionConsumer(t, strings.Replace(source, test.from, test.to, 1))
			require.Error(t, err)
			_, err = os.Stat(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestNotionConnectorCustomSecretNames(t *testing.T) {
	source := strings.Replace(notionConsumerExample(t), "      pages:", "      reader-secret: CUSTOM_NOTION_READER\n      writer-secret: CUSTOM_NOTION_WRITER\n      pages:", 1)
	_, data, _, err := parseNotionConsumer(t, source)
	require.NoError(t, err)
	require.Equal(t, []string{"CUSTOM_NOTION_READER"}, data.Connectors["notion"].Agent.Secrets())
	require.Equal(t, []string{"CUSTOM_NOTION_WRITER"}, data.Connectors["notion"].SafeOutputs.Secrets())
}

func notionFrozenPlans(t *testing.T) (*connectors.Plan, *connectors.Plan) {
	t.Helper()
	_, data, _, err := parseNotionConsumer(t, notionConsumerExample(t))
	require.NoError(t, err)
	plans := make([]*connectors.Plan, 0, 2)
	for _, plan := range []*connectors.Plan{data.Connectors["notion"].Agent, data.Connectors["notion"].SafeOutputs} {
		encoded, err := plan.Encode()
		require.NoError(t, err)
		frozen, err := connectors.DecodePlan(encoded)
		require.NoError(t, err)
		plans = append(plans, frozen)
	}
	return plans[0], plans[1]
}
