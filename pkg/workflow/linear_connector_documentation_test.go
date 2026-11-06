//go:build !integration

package workflow

import (
	"encoding/xml"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type linearDocumentation struct {
	XMLName     xml.Name                     `xml:"connector-documentation"`
	ID          string                       `xml:"id,attr"`
	Version     string                       `xml:"version,attr"`
	Credentials []linearDocumentedCredential `xml:"authentication>credential"`
	Tools       []linearDocumentedTool       `xml:"read-tools>tool"`
	MCPServers  []linearDocumentedTool       `xml:"read-tools>mcp-server"`
	Outputs     []linearDocumentedTool       `xml:"safe-outputs>operation"`
	Examples    []linearDocumentedExample    `xml:"examples>example"`
}

type linearDocumentedCredential struct {
	ID     string `xml:"id,attr"`
	Secret string `xml:"secret,attr"`
	Phase  string `xml:"phase,attr"`
	Scheme string `xml:"scheme,attr"`
}

type linearDocumentedTool struct {
	Selection string `xml:"selection,attr"`
	Tool      string `xml:"tool,attr"`
	Argument  string `xml:"argument,attr"`
	Arguments string `xml:"arguments,attr"`
}

type linearDocumentedExample struct {
	ID   string `xml:"id,attr"`
	Path string `xml:"path,attr"`
	YAML string `xml:"workflow-yaml"`
	Body string `xml:"workflow-body"`
}

func loadLinearDocumentation(t *testing.T) linearDocumentation {
	t.Helper()
	source, err := os.ReadFile("../../.github/workflows/connectors/linear.md")
	require.NoError(t, err)
	parsed, err := parser.ExtractFrontmatterFromContent(string(source))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(parsed.Markdown, "<connector-documentation "))
	require.Equal(t, 1, strings.Count(parsed.Markdown, "</connector-documentation>"))
	comment, rest, found := strings.Cut(strings.TrimPrefix(parsed.Markdown, "<!--\n"), "\n-->")
	require.True(t, found)
	require.True(t, strings.HasPrefix(parsed.Markdown, "<!--\n<connector-documentation "))
	require.NotContains(t, comment, "--", "HTML comments cannot contain internal double hyphens, including in CDATA")
	require.NotContains(t, rest, "<!--", "the connector has one documentation comment")
	var documentation linearDocumentation
	require.NoError(t, xml.Unmarshal([]byte(comment), &documentation))
	require.Equal(t, "linear", documentation.ID)
	require.Equal(t, "1", documentation.Version)
	for _, section := range []string{
		"purpose", "authentication", "endpoints", "import-inputs", "triggers",
		"read-tools", "safe-outputs", "context-and-privacy", "limitations", "examples", "sources",
	} {
		require.Contains(t, comment, "<"+section)
	}
	require.Contains(t, comment, `operational="false"`)
	require.Equal(t, 2, strings.Count(comment, "<![CDATA["))
	checkLinearDocumentedCredentials(t, parsed.Frontmatter, documentation.Credentials)
	return documentation
}

func checkLinearDocumentedCredentials(t *testing.T, frontmatter map[string]any, documented []linearDocumentedCredential) {
	t.Helper()
	declarations, ok := frontmatter["connectors"].(map[string]any)
	require.True(t, ok)
	linear, ok := declarations["linear"].(map[string]any)
	require.True(t, ok)
	credentials, ok := linear["credentials"].(map[string]any)
	require.True(t, ok)
	schema, ok := frontmatter["import-schema"].(map[string]any)
	require.True(t, ok)
	require.Len(t, credentials, len(documented))
	for _, credential := range documented {
		actual, ok := credentials[credential.ID].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "Authorization", actual["header"])
		require.Equal(t, credential.Scheme, actual["scheme"])
		require.Equal(t, credential.Phase, actual["phase"])
		input := credential.ID + "-secret"
		require.Equal(t, "${{ github.aw.import-inputs."+input+" }}", actual["secret"])
		definition, ok := schema[input].(map[string]any)
		require.True(t, ok)
		require.Equal(t, credential.Secret, definition["default"])
	}
}

func TestLinearConnectorDocumentationContract(t *testing.T) {
	documentation := loadLinearDocumentation(t)
	require.Equal(t, []linearDocumentedCredential{
		{ID: "reader", Secret: "LINEAR_API_KEY", Phase: "agent", Scheme: "raw"},
		{ID: "writer", Secret: "LINEAR_API_KEY", Phase: "safe-output", Scheme: "raw"},
		{ID: "mcp-reader", Secret: "LINEAR_READ_MCP_TOKEN", Phase: "agent", Scheme: "bearer"},
	}, documentation.Credentials)
	require.Len(t, documentation.Tools, 3)
	require.Equal(t, []linearDocumentedTool{{Selection: "read-only"}}, documentation.MCPServers)
	outputs := make([]string, 0, len(documentation.Outputs))
	for _, output := range documentation.Outputs {
		outputs = append(outputs, output.Selection)
	}
	require.ElementsMatch(t, []string{
		"linear-create-issue", "linear-create-issue-in-project", "linear-update-issue", "linear-add-comment",
	}, outputs)
	require.Len(t, documentation.Examples, 2)
}

func compileLinearDocumentationExample(t *testing.T, example linearDocumentedExample) (*WorkflowData, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(example.Path))
	require.Equal(t, filepath.Join(root, ".github", "workflows"), filepath.Dir(path))
	require.NotEmpty(t, strings.TrimSpace(example.Body))
	var frontmatter map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(example.YAML), &frontmatter))
	require.Equal(t, "workflow_dispatch", frontmatter["on"])
	outputs, ok := frontmatter["safe-outputs"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, outputs["staged"], "copyable write examples must start staged")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	source := "---\n" + strings.TrimSpace(example.YAML) + "\n---\n" + strings.TrimSpace(example.Body) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithVersion("v0.0.0-1-gabcdef0"))
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeDev)
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err, "the documentation must compile as a real importing consumer")
	compiled, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, ".github", "workflows", "connectors"))
	require.NoError(t, err, "examples use editable local declarations")
	require.Nil(t, data.Connectors["linear"].Context)
	return data, compiled
}

func TestLinearConnectorDocumentationConsumers(t *testing.T) {
	documentation := loadLinearDocumentation(t)
	for _, example := range documentation.Examples {
		t.Run(example.ID, func(t *testing.T) {
			data, compiled := compileLinearDocumentationExample(t, example)
			config := data.Connectors["linear"]
			require.NotNil(t, config)
			require.NotNil(t, config.SafeOutputs)
			agent := extractJobSection(compiled, "agent")
			effects := extractJobSection(compiled, "safe_outputs")
			require.NotContains(t, agent, "${{ secrets.LINEAR_WRITE_API_KEY }}")
			require.NotContains(t, agent, "${{ secrets.LINEAR_TEAM_ID }}")
			require.NotContains(t, effects, "${{ secrets.LINEAR_WRITE_API_KEY }}")
			require.NotContains(t, effects, "${{ secrets.LINEAR_TEAM_ID }}")
			require.NotContains(t, effects, "${{ secrets.LINEAR_READ_API_KEY }}")
			require.NotContains(t, effects, "${{ secrets.LINEAR_READ_MCP_TOKEN }}")
			checkLinearDocumentedOutputs(t, documentation.Outputs, data)
			switch example.ID {
			case "read-and-stage-writes":
				require.Equal(t, []string{"LINEAR_READ_API_KEY", "LINEAR_READ_MCP_TOKEN"}, config.Agent.Secrets())
				require.Equal(t, []string{"get-issue", "read-only__get_issue", "resolve-project", "resolve-team"}, config.Agent.ToolNames())
				require.Equal(t, []string{"add-comment", "create-issue", "update-issue"}, config.SafeOutputs.SafeOutputNames())
				checkLinearDocumentedReads(t, documentation.Tools, data)
			case "trusted-project-creation":
				require.Nil(t, config.Agent)
				require.Equal(t, []string{"LINEAR_TEAM_ID", "LINEAR_WRITE_API_KEY"}, config.SafeOutputs.Secrets())
				require.Equal(t, []string{"create-issue-in-project"}, config.SafeOutputs.SafeOutputNames())
				_, err := config.SafeOutputs.PrepareSafeOutputIntent("create-issue-in-project", map[string]any{"title": "A reviewed connector title", "body": "Body"})
				require.NoError(t, err, "trusted creation can stage without resolving secrets or running preflight")
				_, err = config.SafeOutputs.PrepareSafeOutputIntent("create-issue-in-project", map[string]any{
					"title": "A reviewed connector title", "body": "Body", "team-id": "33333333-3333-4333-8333-333333333333",
				})
				require.Error(t, err, "an example must not allow the agent to rebind a trusted destination")
			default:
				t.Fatalf("add assertions for documentation example %q", example.ID)
			}
		})
	}
}

func checkLinearDocumentedOutputs(t *testing.T, documented []linearDocumentedTool, data *WorkflowData) {
	t.Helper()
	plan := data.Connectors["linear"].SafeOutputs
	for _, operation := range documented {
		name := strings.TrimPrefix(operation.Selection, "linear-")
		if !slices.Contains(plan.SafeOutputNames(), name) {
			continue
		}
		t.Run(operation.Tool, func(t *testing.T) {
			inputs, err := plan.Inputs("safe-output", name)
			require.NoError(t, err)
			expected := strings.Fields(operation.Arguments)
			require.Len(t, inputs, len(expected))
			for _, name := range expected {
				require.Contains(t, inputs, name)
				require.True(t, inputs[name].Required)
				require.Equal(t, "string", inputs[name].Type)
			}
			for _, name := range []string{"title", "body"} {
				if input, ok := inputs[name]; ok {
					require.Equal(t, map[string]int{"title": 128, "body": 65000}[name], input.MaxLength)
					require.Equal(t, `\S`, input.Pattern)
				}
			}
			checkLinearDocumentedIntent(t, plan, name, inputs)
			scriptName := connectorSafeOutputName("linear", name)
			require.NotNil(t, data.SafeOutputs.Scripts[scriptName])
			require.Equal(t, operation.Tool, strings.ReplaceAll(scriptName, "-", "_"))
		})
	}
}

func checkLinearDocumentedIntent(t *testing.T, plan *connectors.Plan, operation string, inputs map[string]connectors.Input) {
	t.Helper()
	args := linearDocumentedArguments(t, inputs)
	_, err := plan.PrepareSafeOutputIntent(operation, args)
	require.NoError(t, err, "every documented output can stage with its documented fields")
	for name, input := range inputs {
		missing := maps.Clone(args)
		delete(missing, name)
		_, err := plan.PrepareSafeOutputIntent(operation, missing)
		require.Error(t, err, "%s requires %s", operation, name)
		if name != "title" && name != "body" {
			continue
		}
		bounded := maps.Clone(args)
		bounded[name] = strings.Repeat("x", input.MaxLength)
		_, err = plan.PrepareSafeOutputIntent(operation, bounded)
		require.NoError(t, err, "%s accepts the documented %s length bound", operation, name)
		bounded[name] = strings.Repeat("x", input.MaxLength+1)
		_, err = plan.PrepareSafeOutputIntent(operation, bounded)
		require.Error(t, err, "%s rejects %s above the documented length bound", operation, name)
	}
}

func linearDocumentedArguments(t *testing.T, inputs map[string]connectors.Input) map[string]any {
	t.Helper()
	values := map[string]string{
		"title": "A reviewed connector title", "body": "Body",
		"issue-id":   "22222222-2222-4222-8222-222222222222",
		"team-id":    "33333333-3333-4333-8333-333333333333",
		"project-id": "44444444-4444-4444-8444-444444444444",
	}
	args := map[string]any{}
	for name := range inputs {
		require.Contains(t, values, name)
		args[name] = values[name]
	}
	return args
}

func checkLinearDocumentedReads(t *testing.T, documented []linearDocumentedTool, data *WorkflowData) {
	t.Helper()
	plan := data.Connectors["linear"].Agent
	for _, tool := range documented {
		inputs, err := plan.Inputs("mcp", tool.Selection)
		require.NoError(t, err)
		require.Len(t, inputs, 1)
		require.Contains(t, inputs, tool.Argument)
		require.True(t, inputs[tool.Argument].Required)
	}
}
