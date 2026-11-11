//go:build !integration

package workflow

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func connectorSpecificationExample(t *testing.T, title string) string {
	t.Helper()
	source, err := os.ReadFile("../../docs/src/content/docs/specs/connector-specification.md")
	require.NoError(t, err)
	_, example, found := strings.Cut(string(source), "```aw title=\""+title+"\"\n")
	require.True(t, found, "specification must retain its complete example")
	example, _, found = strings.Cut(example, "\n```")
	require.True(t, found)
	return example + "\n"
}

func TestConnectorSpecificationExample(t *testing.T) {
	declaration := connectorSpecificationExample(t, ".github/workflows/connectors/example.md")
	consumer := connectorSpecificationExample(t, ".github/workflows/example-handoff.md")
	for _, selection := range []string{"import-only", "read-only", "staged-write", "combined"} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			sourcePath := filepath.Join(root, ".github/workflows/connectors/example.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0700))
			require.NoError(t, os.WriteFile(sourcePath, []byte(declaration), 0600))
			source := consumer
			if selection == "import-only" || selection == "staged-write" {
				source = strings.Replace(source, "tools:\n  example: [get-record]\n", "", 1)
			}
			if selection == "import-only" || selection == "read-only" {
				source = strings.Replace(source, "safe-outputs:\n  example-add-note:\n    max: 1\n    staged: true\n", "", 1)
			}
			path := filepath.Join(root, ".github/workflows/example-handoff.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			compiler := NewCompiler(WithNoEmit(true), WithVersion("v0.0.0-1-gabcdef0"))
			compiler.gitRoot = root
			compiler.SetActionMode(ActionModeDev)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			require.NoError(t, compiler.CompileWorkflow(path), "compile the documented consumer, not only its YAML")
			if selection == "import-only" {
				require.Empty(t, data.Connectors)
				return
			}
			config := data.Connectors["example"]
			require.NotNil(t, config)
			require.Nil(t, config.Context)
			if selection == "read-only" || selection == "combined" {
				require.NotNil(t, config.Agent)
				require.Equal(t, []string{"EXAMPLE_READ_TOKEN"}, config.Agent.Secrets())
				require.Equal(t, []string{"get_record"}, config.Agent.ToolNames())
			} else {
				require.Nil(t, config.Agent)
			}
			if selection == "staged-write" || selection == "combined" {
				require.NotNil(t, config.SafeOutputs)
				require.Equal(t, []string{"EXAMPLE_WRITE_TOKEN"}, config.SafeOutputs.Secrets())
				require.Empty(t, data.SafeOutputs.Env, "staged consumer must receive no writer credential")
				args := map[string]any{
					"record-id": "RECORD-1", "text": "Offline documentation conformance",
				}
				request, err := config.SafeOutputs.PrepareSafeOutput("add-note", args)
				require.NoError(t, err)
				require.Equal(t, "add-note", request.Operation)
				require.Equal(t, config.SafeOutputs.Digest, request.Digest)
				require.Equal(t, args, request.Arguments)
				require.Empty(t, request.UnresolvedInputs)
			} else {
				require.Nil(t, config.SafeOutputs)
			}
		})
	}
}

func TestConnectorSpecificationExampleReadBoundaries(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, ".github/workflows/connectors/example.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0700))
	require.NoError(t, os.WriteFile(sourcePath, []byte(connectorSpecificationExample(t, ".github/workflows/connectors/example.md")), 0600))
	declaration, err := LoadDevelopmentConnector(root, "example", "", map[string]any{
		"records": []any{"RECORD-1", "RECORD-2"},
	}, nil)
	require.NoError(t, err)
	plan, err := declaration.Service.Select(connectors.Selection{MCP: []string{"get-record"}})
	require.NoError(t, err)
	response := `{"id":"RECORD-1","status":"open","body":"not projected"}`
	secrets, requests := 0, 0
	runtime := connectors.Runtime{
		Phase: "agent", AllowedOrigins: []string{"https://api.example.com"},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			secrets++
			require.Equal(t, "agent", phase)
			require.Equal(t, "EXAMPLE_READ_TOKEN", name)
			return "fixture-token", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			requests++
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "https://api.example.com/v1/records/RECORD-1", request.URL.String())
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(response)), Request: request,
			}, nil
		})},
	}
	for _, id := range []any{"RECORD-3", "../RECORD-1", 1, nil} {
		_, err := plan.Execute(t.Context(), runtime, "mcp", "get-record", map[string]any{"record-id": id})
		require.Error(t, err)
	}
	require.Zero(t, secrets, "invalid arguments cannot resolve credentials")
	require.Zero(t, requests)
	args := map[string]any{"record-id": "RECORD-1"}
	result, err := plan.Execute(t.Context(), runtime, "mcp", "get-record", args)
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.JSONEq(t, `{"id":"RECORD-1","status":"open"}`, string(result.Data))
	for _, invalid := range []string{
		`{"id":"RECORD-2","status":"open"}`,
		`{"id":"RECORD-1"}`,
		`{"id":"RECORD-1","status":1}`,
		`not JSON`,
	} {
		response = invalid
		_, err := plan.Execute(t.Context(), runtime, "mcp", "get-record", args)
		require.Error(t, err, "grant membership alone is not requested-identity or response evidence")
	}
}
