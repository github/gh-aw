//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func connectorHostPlan(t *testing.T, endpoint, phase string) *connectors.Plan {
	t.Helper()
	root := t.TempDir()
	spec, err := connectors.LoadSpec("../connectors/testdata/example.yaml")
	require.NoError(t, err)
	spec.Endpoints["api"] = endpoint
	document, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "spec.json"), document, 0600))
	connection := `{"apiVersion":"github.com/gh-aw/connectors/v1","kind":"ConnectorConnection","connector":"example.com/issues","credentials":{"reader":{"secret":"READ_TOKEN"},"writer":{"secret":"WRITE_TOKEN"},"context-reader":{"secret":"CONTEXT_TOKEN"}},"resources":{"issues":["ISSUE-1"]}}`
	require.NoError(t, os.WriteFile(filepath.Join(root, "connection.json"), []byte(connection), 0600))
	plan, err := connectors.Lower(root, connectors.Binding{
		APIVersion: connectors.APIVersion, Kind: "ConnectorBinding", Definition: "spec.json", Connection: "connection.json",
		MCP: []string{"get-issue"}, Context: []string{"issue-details"}, SafeOutputs: []string{"add-comment"},
	})
	require.NoError(t, err)
	if phase != "" {
		plan, err = plan.ForPhase(phase)
		require.NoError(t, err)
	}
	return plan
}

func connectorTestEnv(t *testing.T, plan *connectors.Plan) func(string) string {
	t.Helper()
	encoded, err := plan.Encode()
	require.NoError(t, err)
	return func(name string) string {
		if name == "GH_AW_CONNECTOR_PLAN" {
			return encoded
		}
		if strings.HasPrefix(name, "GH_AW_CONNECTOR_SECRET_") {
			return "test-only"
		}
		return ""
	}
}

func TestConnectorHostRejectsMixedPhaseAndUnknownSecrets(t *testing.T) {
	plan := connectorHostPlan(t, "https://api.example.com", "")
	_, _, err := connectorHost("agent", connectorTestEnv(t, plan))
	require.ErrorContains(t, err, "single-phase")
	agent, err := plan.ForPhase("agent")
	require.NoError(t, err)
	_, runtime, err := connectorHost("agent", connectorTestEnv(t, agent))
	require.NoError(t, err)
	_, err = runtime.ResolveSecret(t.Context(), "agent", "WRITE_TOKEN")
	require.ErrorContains(t, err, "outside the compiled phase")
	_, err = runtime.ResolveSecret(t.Context(), "context", "READ_TOKEN")
	require.ErrorContains(t, err, "outside the compiled phase")
}

func TestConnectorInvokeStagingAndAuthorization(t *testing.T) {
	plan := connectorHostPlan(t, "https://api.example.com", "safe-output")
	encoded, err := plan.Encode()
	require.NoError(t, err)
	secretsRead := 0
	getenv := func(name string) string {
		if name == "GH_AW_CONNECTOR_PLAN" {
			return encoded
		}
		secretsRead++
		return ""
	}
	var output bytes.Buffer
	err = invokeConnector(t.Context(), strings.NewReader(`{"issue":"ISSUE-1","text":"hello"}`), &output, "safe-output", "add-comment", false, true, getenv)
	require.NoError(t, err)
	assert.Contains(t, output.String(), `"staged":true`)
	assert.Zero(t, secretsRead)
	err = invokeConnector(t.Context(), strings.NewReader(`{"issue":"ISSUE-1","text":"hello"}`), io.Discard, "safe-output", "add-comment", false, false, getenv)
	require.ErrorContains(t, err, "explicit host write authorization")
	assert.Zero(t, secretsRead)
	err = invokeConnector(t.Context(), strings.NewReader(`{"issue":"OTHER","text":"hello"}`), io.Discard, "safe-output", "add-comment", false, true, getenv)
	require.ErrorContains(t, err, "outside approved resources")
	err = invokeConnector(t.Context(), strings.NewReader(`{}`), io.Discard, "safe-output", "add-comment", true, true, getenv)
	require.ErrorContains(t, err, "without --allow-writes")
}

func TestConnectorInvokeExecutesFrozenOperations(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, "Bearer test-only", r.Header.Get("Authorization"))
		if r.Method == http.MethodPost {
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "hello", body["body"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, err := io.WriteString(w, `{"id":"ISSUE-1","title":"context"}`)
		assert.NoError(t, err)
	}))
	defer server.Close()
	original := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, test := range []struct{ phase, operation, input string }{
		{"context", "issue-details", `{"issue":"ISSUE-1"}`},
		{"safe-output", "add-comment", `{"issue":"ISSUE-1","text":"hello"}`},
	} {
		plan := connectorHostPlan(t, server.URL, test.phase)
		var output bytes.Buffer
		err := invokeConnector(context.Background(), strings.NewReader(test.input), &output, test.phase, test.operation, test.phase == "safe-output", false, connectorTestEnv(t, plan))
		require.NoError(t, err)
		assert.Contains(t, output.String(), `"untrusted":true`)
	}
	assert.Equal(t, 2, requests)
}
