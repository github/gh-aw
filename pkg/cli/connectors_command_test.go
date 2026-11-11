//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestConnectorDevelopmentIDsDoNotCollideWithLegacyCommands(t *testing.T) {
	for _, id := range []string{"plan", "schema", "validate", "serve", "invoke"} {
		t.Run(id, func(t *testing.T) {
			repository := t.TempDir()
			source := strings.Replace(developmentConnectorFixture, "  demo:", "  "+id+":", 1)
			path := filepath.Join(repository, ".github", "workflows", "connectors", id+".md")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			root := &cobra.Command{Use: "aw"}
			root.AddCommand(NewConnectorsCommand(), NewConnectorCommand())
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs([]string{"connector", id, "setup", "--root", repository})
			require.NoError(t, root.Execute())
			require.Contains(t, output.String(), "Experimental connector "+id)
		})
	}
}

func TestConnectorJiraSetupExampleUsesDeclaredInput(t *testing.T) {
	repository, err := filepath.Abs("../..")
	require.NoError(t, err)
	command := NewConnectorCommand()
	require.Contains(t, command.Example, `mcp-personal-secret="JIRA_DEV_BASIC_AUTH"`)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"jira", "setup", "--root", repository, "--input", `mcp-personal-secret="JIRA_DEV_BASIC_AUTH"`})
	require.NoError(t, command.Execute())
	require.Contains(t, output.String(), "JIRA_DEV_BASIC_AUTH")
	_, guidance, found := strings.Cut(output.String(), "```text\n")
	require.True(t, found)
	guidance, _, found = strings.Cut(guidance, "\n```")
	require.True(t, found)
	for _, expected := range []string{
		"credential id=rest-reader", "RFC 4648 Base-64 encoding of the UTF-8 account-email:unscoped-api-token pair",
		"account-email:scoped-MCP-api-token pair", `without the "Basic " prefix or a newline`,
		"Browse users and groups", "read:jira:agent-interface", "search:jira:agent-interface",
		"raw key", "api.atlassian.com/ex/jira/{cloudId}",
	} {
		require.Contains(t, guidance, expected, "setup must print the complete credential recipe, not refer to omitted descriptions")
	}
	require.NotContains(t, guidance, "input description specifies")
}

func TestConnectorsSchemaCommand(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"ConnectorSpec", "ConnectorConnection", "ConnectorBinding"} {
		cmd := NewConnectorsCommand()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetArgs([]string{"schema", kind})
		require.NoError(t, cmd.Execute())
		var schema map[string]any
		require.NoError(t, json.Unmarshal(output.Bytes(), &schema))
		require.Equal(t, "object", schema["type"])
	}
	cmd := NewConnectorsCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"schema", "unsupported"})
	require.Error(t, cmd.Execute())
}

func TestConnectorsPlanCommand(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	data, err := os.ReadFile("../connectors/testdata/example.yaml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "spec.yaml"), data, 0600))
	connection := `apiVersion: github.com/gh-aw/connectors/v1
kind: ConnectorConnection
connector: example.com/issues
credentials:
  reader: {secret: EXAMPLE_READ_TOKEN}
  writer: {secret: EXAMPLE_WRITE_TOKEN}
  context-reader: {secret: EXAMPLE_CONTEXT_TOKEN}
resources:
  issues: [ISSUE-1]
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "connection.yaml"), []byte(connection), 0600))
	binding := `apiVersion: github.com/gh-aw/connectors/v1
kind: ConnectorBinding
definition: spec.yaml
connection: connection.yaml
mcp: [get-issue]
safeOutputs: [add-comment]
context: [issue-details]
`
	path := filepath.Join(root, "binding.yaml")
	require.NoError(t, os.WriteFile(path, []byte(binding), 0600))
	cmd := NewConnectorsCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"plan", path, "--root", root})
	require.NoError(t, cmd.Execute())
	var result struct {
		Digest      string `json:"digest"`
		Operational bool   `json:"operational"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.Len(t, result.Digest, 64)
	require.False(t, result.Operational)
	require.NotContains(t, output.String(), "EXAMPLE_WRITE_TOKEN")
	require.NoError(t, os.WriteFile(filepath.Join(root, "connection.yaml"), []byte(connection+"unknown: true\n"), 0600))
	cmd = NewConnectorsCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", path, "--root", root})
	require.ErrorContains(t, cmd.Execute(), "unknown field")
}

func TestConnectorsPlanRejectsInvalidFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := readConnectorBinding(dir)
	require.ErrorContains(t, err, "regular file")
	_, err = readConnectorBinding(filepath.Join(dir, "missing.yaml"))
	require.ErrorContains(t, err, "open connector binding")
	path := filepath.Join(dir, "large.yaml")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte(" "), 256*1024+1), 0600))
	_, err = readConnectorBinding(path)
	require.Error(t, err)
}

func TestConnectorsValidateCommand(t *testing.T) {
	t.Parallel()
	cmd := NewConnectorsCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"validate", "../connectors/testdata/example.yaml"})
	require.NoError(t, cmd.Execute())
	require.Contains(t, output.String(), "valid ConnectorSpec example.com/issues")
	require.Contains(t, output.String(), "no provider access or webhook activation")
}

func TestConnectorsValidatePublicProviderSpecs(t *testing.T) {
	t.Parallel()
	repository, err := filepath.Abs("../..")
	require.NoError(t, err)
	declarations, err := filepath.Glob(filepath.Join(repository, ".github", "workflows", "connectors", "*.md"))
	require.NoError(t, err)
	require.Len(t, declarations, 13)
	args := []string{"connectors", "validate", "--json"}
	var expectedIDs []string
	directory := t.TempDir()
	for _, path := range declarations {
		provider := strings.TrimSuffix(filepath.Base(path), ".md")
		golden, err := os.ReadFile(filepath.Join(repository, "pkg", "cli", "workflows", provider+"-golden.md"))
		require.NoError(t, err)
		parsed, err := parser.ExtractFrontmatterFromContent(string(golden))
		require.NoError(t, err)
		imports, ok := parsed.Frontmatter["imports"].([]any)
		require.True(t, ok)
		require.Len(t, imports, 1)
		entry, ok := imports[0].(map[string]any)
		require.True(t, ok)
		var inputs map[string]any
		if bindings := entry["with"]; bindings != nil {
			inputs, ok = bindings.(map[string]any)
			require.True(t, ok, provider)
		}
		declaration, err := workflow.LoadDevelopmentConnector(repository, provider, path, inputs, nil)
		require.NoError(t, err)
		spec, err := json.Marshal(declaration.Registration.Spec)
		require.NoError(t, err)
		definition := filepath.Join(directory, provider+".json")
		require.NoError(t, os.WriteFile(definition, spec, 0600))
		args = append(args, definition)
		expectedIDs = append(expectedIDs, "aw.github.com/"+provider)
	}

	var output bytes.Buffer
	connectorsCommand := NewConnectorsCommand()
	connectorsCommand.SetOut(&output)
	root := &cobra.Command{Use: "aw"}
	root.AddCommand(connectorsCommand)
	root.SetArgs(args)
	require.NoError(t, root.ExecuteContext(t.Context()))
	var results []connectorValidationResult
	require.NoError(t, json.Unmarshal(output.Bytes(), &results), "JSON output must contain only one result array")
	require.Len(t, results, len(declarations))
	for index, result := range results {
		require.Equal(t, args[index+3], result.File)
		require.Equal(t, expectedIDs[index], result.ID)
		require.True(t, result.Valid, result.File)
		require.Empty(t, result.Error)
	}
}

func TestConnectorsValidateRequiresFiles(t *testing.T) {
	t.Parallel()
	cmd := NewConnectorsCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"validate"})
	require.ErrorContains(t, cmd.Execute(), "requires at least 1 arg")
}

func TestConnectorsValidateAllFilesJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.yaml")
	require.NoError(t, os.WriteFile(invalid, []byte("apiVersion: unsupported\nkind: ConnectorSpec\n"), 0600))
	missing := filepath.Join(dir, "missing.yaml")
	cmd := NewConnectorsCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"validate", invalid, "../connectors/testdata/example.yaml", missing, "--json"})
	require.ErrorContains(t, cmd.Execute(), "connector validation failed for 2 of 3")
	var results []connectorValidationResult
	require.NoError(t, json.Unmarshal(output.Bytes(), &results))
	require.Len(t, results, 3)
	require.False(t, results[0].Valid)
	require.NotEmpty(t, results[0].Error)
	require.Equal(t, "example.com/issues", results[1].ID)
	require.True(t, results[1].Valid)
	require.Empty(t, results[1].Error)
	require.Equal(t, missing, results[2].File)
	require.NotEmpty(t, results[2].Error)
}

func TestConnectorsValidateRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../connectors/testdata/example.yaml")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "unknown.yaml")
	require.NoError(t, os.WriteFile(path, append(data, []byte("\nrun: arbitrary-code\n")...), 0600))
	var output bytes.Buffer
	require.Error(t, validateConnectorDefinitions(&output, []string{path}, false))
	require.Contains(t, output.String(), "invalid")
	require.Contains(t, output.String(), "unknown field")
}

type failedConnectorValidationWriter struct{}

func (failedConnectorValidationWriter) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}

func TestConnectorsValidateReportsOutputFailure(t *testing.T) {
	t.Parallel()
	for _, jsonOutput := range []bool{false, true} {
		err := validateConnectorDefinitions(failedConnectorValidationWriter{}, []string{"../connectors/testdata/example.yaml"}, jsonOutput)
		require.ErrorContains(t, err, "output unavailable")
	}
}
