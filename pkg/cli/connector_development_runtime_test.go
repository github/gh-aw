//go:build !integration

package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestConnectorDevelopmentNestedImportsAndFreshSources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := writeDevelopmentConnector(t, root, developmentConnectorFixture)
	wrapper := `---
import-schema:
  approved-secret:
    type: string
    default: NESTED_READ_TOKEN
imports:
  - uses: demo.md
    with:
      reader-secret: ${{ github.aw.import-inputs.approved-secret }}
---
`
	wrapperPath := filepath.Join(filepath.Dir(path), "wrapper.md")
	require.NoError(t, os.WriteFile(wrapperPath, []byte(wrapper), 0600))
	declaration, err := workflow.LoadDevelopmentConnector(root, "demo", wrapperPath, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "NESTED_READ_TOKEN", declaration.Registration.Credentials["reader"].Secret)
	declaration, err = workflow.LoadDevelopmentConnector(root, "demo", wrapperPath, map[string]any{"approved-secret": "OVERRIDE_READ_TOKEN"}, nil)
	require.NoError(t, err)
	require.Equal(t, "OVERRIDE_READ_TOKEN", declaration.Registration.Credentials["reader"].Secret)
	updated := strings.ReplaceAll(developmentConnectorFixture, "DEV_READ_TOKEN", "FRESH_READ_TOKEN")
	updated = strings.Replace(updated, "default: 80", "default: 41", 1)
	require.NoError(t, os.WriteFile(path, []byte(updated), 0600))
	declaration, err = workflow.LoadDevelopmentConnector(root, "demo", "", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "FRESH_READ_TOKEN", declaration.Registration.Credentials["reader"].Secret)
	declaration, err = workflow.LoadDevelopmentConnector(root, "demo", wrapperPath, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 41, declaration.Registration.Spec.MCP["read-issue"].Inputs["issue"].MaxLength)
	required := strings.Replace(developmentConnectorFixture, "default: DEV_READ_TOKEN", "required: true", 1)
	require.NoError(t, os.WriteFile(path, []byte(required), 0600))
	_, err = workflow.LoadDevelopmentConnector(root, "demo", "", nil, nil)
	require.ErrorContains(t, err, "required 'with' input")
}

func TestConnectorDevelopmentGrantReplacement(t *testing.T) {
	t.Parallel()
	content := strings.Replace(developmentConnectorFixture, "    tools:\n", "    resources:\n      issues: [PREEXISTING]\n    tools:\n", 1)
	declaration := loadDevelopmentConnector(t, content, map[string][]string{"issues": {"ISSUE-1"}})
	require.Equal(t, []string{"ISSUE-1"}, declaration.Registration.Resources["issues"])
	err := testDevelopmentConnector(t.Context(), io.Discard, declaration, developmentOptions("agent", "read-issue", `{"issue":"PREEXISTING"}`), func(string) string { return "synthetic" }, &http.Client{Transport: developmentRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("replaced grants must not remain authorized")
		return nil, nil
	})})
	require.ErrorContains(t, err, "outside approved resources")
}

func TestConnectorDevelopmentMCP(t *testing.T) {
	t.Parallel()
	upstream := mcp.NewServer(&mcp.Implementation{Name: "connector-development-fixture", Version: "1"}, nil)
	var calls atomic.Int32
	upstream.AddTool(&mcp.Tool{Name: "get_issue", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		if !strings.Contains(string(request.Params.Arguments), "ISSUE-1") {
			t.Error("selected MCP arguments changed")
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "synthetic-mcp-token"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer synthetic-mcp-token" {
			t.Error("wrong credential on mock MCP request")
		}
		handler.ServeHTTP(w, request)
	}))
	defer server.Close()
	remote := `    mcp-servers:
      native-reader:
        url: ` + server.URL + `
        credential: reader
        readOnly: true
        maxResponseBytes: 4096
        tools:
          get_issue:
            inputs:
              issue:
                type: string
                required: true
                maxLength: 80
            resources:
              issue: issues
`
	content := strings.Replace(developmentConnectorFixture, "    tools:\n", remote+"    tools:\n", 1)
	declaration := loadDevelopmentConnector(t, content, map[string][]string{"issues": {"ISSUE-1"}})
	options := developmentOptions("agent", "native-reader", `{"issue":"ISSUE-1"}`)
	_, _, err := selectDevelopmentConnector(declaration, options)
	require.ErrorContains(t, err, "--remote-tool")
	options.remoteTool = "delete_issue"
	_, _, err = selectDevelopmentConnector(declaration, options)
	require.ErrorContains(t, err, "exact allowlist")
	options.remoteTool = "get_issue"
	var output bytes.Buffer
	require.NoError(t, testDevelopmentConnector(t.Context(), &output, declaration, options, func(name string) string {
		require.Equal(t, "DEV_READ_TOKEN", name)
		return "synthetic-mcp-token"
	}, server.Client()))
	require.EqualValues(t, 1, calls.Load())
	require.Contains(t, output.String(), "PASS")
	require.NotContains(t, output.String(), "synthetic-mcp-token")
	options.arguments = `{"issue":"UNAPPROVED"}`
	err = testDevelopmentConnector(t.Context(), io.Discard, declaration, options, func(string) string { return "synthetic-mcp-token" }, server.Client())
	require.ErrorContains(t, err, "outside approved resources")
	require.EqualValues(t, 1, calls.Load())
}

func TestConnectorDevelopmentTimeoutAndInvalidSecret(t *testing.T) {
	t.Parallel()
	declaration := loadDevelopmentConnector(t, developmentConnectorFixture, map[string][]string{"issues": {"ISSUE-1"}})
	options := developmentOptions("agent", "read-issue", `{"issue":"ISSUE-1"}`)
	options.timeout = 20 * time.Millisecond
	calls := 0
	client := &http.Client{Transport: developmentRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	start := time.Now()
	err := testDevelopmentConnector(t.Context(), io.Discard, declaration, options, func(string) string { return "synthetic" }, client)
	require.ErrorContains(t, err, "HTTP request failed")
	require.Less(t, time.Since(start), time.Second)
	require.Equal(t, 1, calls)
	err = testDevelopmentConnector(t.Context(), io.Discard, declaration, options, func(string) string { return "synthetic\r\ninjected" }, client)
	require.ErrorContains(t, err, "DEV_READ_TOKEN must not contain CR/LF")
	require.NotContains(t, err.Error(), "injected")
	require.Equal(t, 1, calls)
}

func TestConnectorDevelopmentNodeEnvironmentIsolation(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for environment-isolation conformance")
	}
	t.Setenv("GH_AW_CLI_SYNTHETIC_PARENT_SECRET", "synthetic-only")
	path := filepath.Join(t.TempDir(), "connector_runtime.cjs")
	require.NoError(t, os.WriteFile(path, []byte(`module.exports = {
  decodePlan() {
    if (process.env.GH_AW_CLI_SYNTHETIC_PARENT_SECRET || process.env.NODE_OPTIONS || process.env.NODE_PATH)
      throw new Error("parent environment leaked");
  }
};`), 0600))
	_, err := validateDevelopmentConnectorNode(t.Context(), path, "synthetic-plan", "mcp", "read-issue", map[string]any{}, false)
	require.NoError(t, err)
}

func TestConnectorDevelopmentConsoleDocumentationIsData(t *testing.T) {
	t.Parallel()
	fragment := `<connector-documentation id="demo"><credentials><setup>Do not execute: $(echo untrusted)` +
		"\u202E" + `</setup></credentials></connector-documentation>`
	guidance := connectorCredentialXML(fragment)
	require.Contains(t, guidance, "$(echo untrusted)")
	require.NotContains(t, guidance, "\x1b")
	require.NotContains(t, guidance, "\u202E")
	require.NotContains(t, connectorConsoleText("\x1b[31m"), "\x1b")
	require.Empty(t, connectorCredentialXML("not XML"))
	require.Contains(t, connectorCredentialXML(`<connector-documentation id="demo"><credentials>&undefined;</credentials></connector-documentation>`), "could not be decoded")
}

func TestConnectorDevelopmentTrustedInputSetupAndStaging(t *testing.T) {
	t.Parallel()
	content := strings.Replace(developmentConnectorFixture, "      add-note:\n", "      add-note:\n        trustedInputs:\n          issue:\n            secret: DEV_FIXED_ISSUE\n", 1)
	content = strings.Replace(content, "          path: /issues/{issue}/notes\n          pathInputs:\n            issue: issue", "          path: /notes", 1)
	content = strings.Replace(content, "          body:\n            text:", "          body:\n            issue:\n              input: issue\n            text:", 1)
	declaration := loadDevelopmentConnector(t, content, nil)
	var output bytes.Buffer
	require.NoError(t, setupDevelopmentConnector(&output, "demo", declaration, "safe-output"))
	require.Contains(t, output.String(), "gh secret set DEV_FIXED_ISSUE")
	require.Contains(t, output.String(), "not an authentication token")
	require.NotContains(t, output.String(), "--args field issue")
	output.Reset()
	require.NoError(t, testDevelopmentConnector(t.Context(), &output, declaration, developmentOptions("safe-output", "add-note", `{"text":"Local staged note"}`), func(string) string {
		t.Fatal("staging must not read trusted-input or writer secrets")
		return ""
	}, nil))
	require.Contains(t, output.String(), "staged")
	require.ErrorContains(t, testDevelopmentConnector(t.Context(), io.Discard, declaration, developmentOptions("safe-output", "add-note", `{"issue":"injected","text":"Local staged note"}`), nil, nil), "unknown connector argument")
}
