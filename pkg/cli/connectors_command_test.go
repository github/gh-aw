//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
