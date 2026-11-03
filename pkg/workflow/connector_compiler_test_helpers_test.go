//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func parseConnectorConsumer(t *testing.T, filename, source string) (*Compiler, *WorkflowData, string, error) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", filename)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithVersion("v0.0.0-1-gabcdef0"))
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeDev)
	data, err := compiler.ParseWorkflowFile(path)
	return compiler, data, path, err
}

func connectorPhaseJobJSON(t *testing.T, output string) (agent, effects string) {
	t.Helper()
	var document struct {
		Jobs map[string]any `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &document))
	require.NotEmpty(t, document.Jobs)
	agentJSON, err := json.Marshal(document.Jobs["agent"])
	require.NoError(t, err)
	effectsJSON, err := json.Marshal(document.Jobs["safe_outputs"])
	require.NoError(t, err)
	return string(agentJSON), string(effectsJSON)
}
