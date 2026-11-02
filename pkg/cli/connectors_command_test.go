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
