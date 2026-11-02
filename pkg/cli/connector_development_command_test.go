//go:build !integration

package cli

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/stretchr/testify/require"
)

const developmentConnectorFixture = `---
import-schema:
  reader-secret:
    type: string
    default: DEV_READ_TOKEN
  writer-secret:
    type: string
    default: DEV_WRITE_TOKEN
  max-length:
    type: number
    default: 80
  enabled:
    type: boolean
    default: true
connectors:
  demo:
    experimental: true
    docs: [https://docs.example.test/authentication]
    endpoints:
      api: https://api.example.test
    credentials:
      reader:
        phase: agent
        header: Authorization
        scheme: bearer
        scopes: [issues:read]
        token:
          secret: ${{ github.aw.import-inputs.reader-secret }}
      writer:
        phase: safe-output
        header: Authorization
        scheme: bearer
        scopes: [issues:write]
        secret: ${{ github.aw.import-inputs.writer-secret }}
    tools:
      read-issue:
        tool: read-issue
        credential: reader
        inputs:
          issue:
            type: string
            required: true
            maxLength: ${{ github.aw.import-inputs.max-length }}
        resources:
          issue: issues
        request:
          endpoint: api
          method: GET
          path: /issues/{issue}
          pathInputs:
            issue: issue
          query:
            enabled:
              literal: ${{ github.aw.import-inputs.enabled }}
        maxResponseBytes: 1024
        responseChecks:
          /ok: true
        responseRequired: [/id]
        responseTypes:
          /id: string
        responseInputs:
          /id: issue
        responseResources:
          /id: issues
        project:
          id: /id
    safe-outputs:
      add-note:
        credential: writer
        inputs:
          issue:
            type: string
            required: true
            maxLength: 80
          text:
            type: string
            required: true
            maxLength: 4000
        resources:
          issue: issues
        request:
          endpoint: api
          method: POST
          path: /issues/{issue}/notes
          pathInputs:
            issue: issue
          body:
            text:
              input: text
        maxResponseBytes: 1024
---
<!--
<connector-documentation id="demo" version="1">
  <credentials>
    <acquisition>Create a provider app with the approved issue scope.</acquisition>
    <reader>Use a read-only token &amp; keep write tokens separate.</reader>
  </credentials>
</connector-documentation>
-->
`

func writeDevelopmentConnector(t *testing.T, root, content string) string {
	t.Helper()
	path := filepath.Join(root, ".github", "workflows", "connectors", "demo.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func loadDevelopmentConnector(t *testing.T, content string, grants map[string][]string) *workflow.DevelopmentConnector {
	t.Helper()
	root := t.TempDir()
	writeDevelopmentConnector(t, root, content)
	declaration, err := workflow.LoadDevelopmentConnector(root, "demo", "", nil, grants)
	require.NoError(t, err)
	return declaration
}

func developmentOptions(phase, capability, arguments string) *connectorDevelopmentOptions {
	return &connectorDevelopmentOptions{phase: phase, capability: capability, arguments: arguments, timeout: 2 * time.Second}
}

func executeDevelopmentCommand(args ...string) (string, error) {
	command := NewConnectorCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}

func TestConnectorDevelopmentSetup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDevelopmentConnector(t, root, developmentConnectorFixture)
	output, err := executeDevelopmentCommand("demo", "setup", "--root", root, "--input", `reader-secret="CUSTOM_READ_TOKEN"`)
	require.NoError(t, err)
	for _, expected := range []string{"CUSTOM_READ_TOKEN", "DEV_WRITE_TOKEN", "agent", "safe-output", "gh secret set CUSTOM_READ_TOKEN", "read -r -s CUSTOM_READ_TOKEN", "Create a provider app", "read-only token & keep write tokens separate", "https://docs.example.test/authentication", "No secrets were read"} {
		require.Contains(t, output, expected)
	}
	output, err = executeDevelopmentCommand("demo", "setup", "--root", root, "--phase", "agent")
	require.NoError(t, err)
	require.Contains(t, output, "DEV_READ_TOKEN")
	require.NotContains(t, output, "DEV_WRITE_TOKEN")
}

func TestConnectorDevelopmentSetupStability(t *testing.T) {
	root := t.TempDir()
	writeDevelopmentConnector(t, root, strings.Replace(developmentConnectorFixture, "experimental: true", "experimental: false", 1))
	output, err := executeDevelopmentCommand("demo", "setup", "--root", root)
	require.NoError(t, err)
	require.Contains(t, output, "Connector demo\n")
	require.NotContains(t, output, "Experimental connector")
}

func TestConnectorDevelopmentSourcesAndInputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	output, err := executeDevelopmentCommand("slack", "setup", "--root", root)
	require.ErrorContains(t, err, "not found locally")
	require.ErrorContains(t, err, "normal imports URL syntax")
	require.Empty(t, output)
	path := writeDevelopmentConnector(t, root, developmentConnectorFixture)
	declaration, err := workflow.LoadDevelopmentConnector(root, "demo", path, map[string]any{"max-length": float64(37), "enabled": false}, nil)
	require.NoError(t, err)
	require.Equal(t, 37, declaration.Registration.Spec.MCP["read-issue"].Inputs["issue"].MaxLength)
	require.Equal(t, false, declaration.Registration.Spec.MCP["read-issue"].Request.Query["enabled"].Literal)
	for _, test := range []struct {
		id, file string
		inputs   map[string]any
		match    string
	}{
		{"demo", "missing.md", nil, "no source fallback"},
		{"demo", "../outside.md", nil, "within the repository"},
		{"../slack", "", nil, "lowercase"},
		{"other", path, nil, "exactly connector"},
		{"demo", "", map[string]any{"unknown": true}, "unknown 'with'"},
		{"demo", "", map[string]any{"enabled": "true"}, "must be a boolean"},
		{"demo", "", map[string]any{"max-length": "80"}, "must be a number"},
	} {
		_, err := workflow.LoadDevelopmentConnector(root, test.id, test.file, test.inputs, nil)
		require.ErrorContains(t, err, test.match)
	}
	slack := filepath.Join(root, ".github", "workflows", "connectors", "slack.md")
	require.NoError(t, os.WriteFile(slack, []byte("---\nconnectors:\n  slack: invalid\n---\n"), 0600))
	_, err = workflow.LoadDevelopmentConnector(root, "slack", "", nil, nil)
	require.Error(t, err, "invalid local source must not use a provider catalog")
	require.NoError(t, os.Remove(slack))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing.md"), slack))
	_, err = workflow.LoadDevelopmentConnector(root, "slack", "", nil, nil)
	require.ErrorContains(t, err, "no source fallback", "dangling local sources must not fall back")
}

func TestConnectorDevelopmentTypedBindings(t *testing.T) {
	t.Parallel()
	options := &connectorDevelopmentOptions{
		inputs: []string{`name="text"`, "count=3", "enabled=false", `values=["one"]`, `config={"key":"value"}`},
		grants: []string{`issues=["ISSUE-1"]`},
	}
	inputs, grants, err := connectorDevelopmentBindings(options)
	require.NoError(t, err)
	require.Equal(t, "text", inputs["name"])
	require.Equal(t, int64(3), inputs["count"])
	require.Equal(t, false, inputs["enabled"])
	require.Equal(t, []string{"ISSUE-1"}, grants["issues"])
	for _, bad := range []*connectorDevelopmentOptions{
		{inputs: []string{"name=unquoted"}},
		{inputs: []string{"=true"}},
		{inputs: []string{"name=true", "name=false"}},
		{inputs: []string{"count=9007199254740993"}},
		{inputs: []string{`config={"count":1e999}`}},
		{grants: []string{"issues=[]"}},
		{grants: []string{`issues=["one"]`, `issues=["two"]`}},
		{grants: []string{`issues=[1]`}},
	} {
		_, _, err := connectorDevelopmentBindings(bad)
		require.Error(t, err)
	}
	_, err = workflow.LoadDevelopmentConnector(t.TempDir(), "missing", "", nil, nil)
	require.ErrorContains(t, err, "not found")
}

func TestConnectorDevelopmentMissingSecretsAndPhaseIsolation(t *testing.T) {
	t.Parallel()
	declaration := loadDevelopmentConnector(t, developmentConnectorFixture, map[string][]string{"issues": {"ISSUE-1"}})
	var output bytes.Buffer
	var names []string
	err := testDevelopmentConnector(t.Context(), &output, declaration, developmentOptions("agent", "read-issue", `{"issue":"ISSUE-1"}`), func(name string) string {
		names = append(names, name)
		return ""
	}, nil)
	require.ErrorContains(t, err, "DEV_READ_TOKEN")
	require.Equal(t, []string{"DEV_READ_TOKEN"}, names)
	require.Contains(t, output.String(), "gh secret set DEV_READ_TOKEN")
	require.Contains(t, output.String(), "read -r -s DEV_READ_TOKEN; export DEV_READ_TOKEN")
	require.NotContains(t, output.String(), "DEV_WRITE_TOKEN")
}

func TestConnectorDevelopmentSafeOutputStaysCredentialFree(t *testing.T) {
	t.Parallel()
	declaration := loadDevelopmentConnector(t, developmentConnectorFixture, map[string][]string{"issues": {"ISSUE-1"}})
	var output bytes.Buffer
	err := testDevelopmentConnector(t.Context(), &output, declaration, developmentOptions("safe-output", "add-note", `{"issue":"ISSUE-1","text":"Local note"}`), func(string) string {
		t.Fatal("staging must not inspect credentials")
		return ""
	}, &http.Client{Transport: developmentRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("staging must not issue requests")
		return nil, nil
	})})
	require.NoError(t, err)
	require.Contains(t, output.String(), "staged")
	require.Contains(t, output.String(), "Provider credentials and response checks were not tested")
	require.NotContains(t, output.String(), "Local note", "do not print potentially sensitive arguments")
}

type developmentRoundTripper func(*http.Request) (*http.Response, error)

func (f developmentRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestConnectorDevelopmentReadConformance(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, body, match string
		status            int
	}{
		{"success", `{"ok":true,"id":"ISSUE-1","hidden":"synthetic-token"}`, "", 200},
		{"response-check", `{"ok":false,"id":"ISSUE-1"}`, "response check failed", 200},
		{"response-binding", `{"ok":true,"id":"OTHER"}`, "outside approved resources", 200},
		{"status", `{"error":"synthetic-token"}`, "HTTP status 401", 401},
		{"bounded", strings.Repeat("x", 1025), "byte bound", 200},
		{"malformed", `{"token":"synthetic-token"`, "valid JSON", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			declaration := loadDevelopmentConnector(t, developmentConnectorFixture, map[string][]string{"issues": {"ISSUE-1"}})
			var output bytes.Buffer
			calls := 0
			client := &http.Client{Transport: developmentRoundTripper(func(request *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "Bearer synthetic-token", request.Header.Get("Authorization"))
				require.Equal(t, "GET", request.Method)
				require.Equal(t, "/issues/ISSUE-1", request.URL.Path)
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body)), Header: http.Header{}}, nil
			})}
			err := testDevelopmentConnector(t.Context(), &output, declaration, developmentOptions("agent", "read-issue", `{"issue":"ISSUE-1"}`), func(name string) string {
				require.Equal(t, "DEV_READ_TOKEN", name)
				return "synthetic-token"
			}, client)
			if test.match == "" {
				require.NoError(t, err)
				require.Contains(t, output.String(), "provider response checks passed")
			} else {
				require.ErrorContains(t, err, test.match)
				require.NotContains(t, err.Error(), "synthetic-token")
			}
			require.Equal(t, 1, calls, "never retry")
			require.NotContains(t, output.String(), "synthetic-token")
			require.NotContains(t, output.String(), "hidden")
		})
	}
}

func TestConnectorDevelopmentExactGrantsAndArguments(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		grants map[string][]string
		args   string
		match  string
	}{
		{nil, `{"issue":"ISSUE-1"}`, "allowlist"},
		{map[string][]string{"issues": {"ISSUE-1"}}, `{"issue":"OTHER"}`, "outside approved resources"},
		{map[string][]string{"issues": {"ISSUE-1"}}, `{"issue":1}`, "bounded UTF-8 string"},
		{map[string][]string{"issues": {"ISSUE-1"}}, `{}`, "required connector argument"},
		{map[string][]string{"issues": {"ISSUE-1"}}, `{"issue":"ISSUE-1","extra":true}`, "unknown connector argument"},
	} {
		declaration := loadDevelopmentConnector(t, developmentConnectorFixture, test.grants)
		err := testDevelopmentConnector(t.Context(), io.Discard, declaration, developmentOptions("agent", "read-issue", test.args), func(string) string { return "synthetic" }, &http.Client{Transport: developmentRoundTripper(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid grants or arguments must fail before HTTP")
			return nil, nil
		})})
		require.ErrorContains(t, err, test.match)
	}
	root := t.TempDir()
	writeDevelopmentConnector(t, root, developmentConnectorFixture)
	for _, grants := range []map[string][]string{
		{"issues": {"*"}}, {"issues": {""}}, {"issues": {}}, {"unknown": {"ISSUE-1"}},
	} {
		_, err := workflow.LoadDevelopmentConnector(root, "demo", "", nil, grants)
		require.Error(t, err)
	}
}

func TestConnectorDevelopmentIdentityRoutes(t *testing.T) {
	t.Parallel()
	for _, route := range []struct {
		name, declaration, match string
	}{
		{"GitHub App", "github-app:\n          client-id: ${{ vars.DEMO_APP_ID }}\n          private-key: ${{ secrets.DEMO_APP_KEY }}\n          permissions:\n            issues: read", "GitHub App identity"},
		{"GitHub OIDC", "github-oidc:\n          audience: https://identity.example.test", "GitHub Actions OIDC identity"},
	} {
		t.Run(route.name, func(t *testing.T) {
			content := strings.Replace(developmentConnectorFixture, "token:\n          secret: ${{ github.aw.import-inputs.reader-secret }}", route.declaration, 1)
			declaration := loadDevelopmentConnector(t, content, map[string][]string{"issues": {"ISSUE-1"}})
			var output bytes.Buffer
			require.NoError(t, setupDevelopmentConnector(&output, "demo", declaration, "agent"))
			require.Contains(t, output.String(), route.name)
			if route.name == "GitHub App" {
				require.Contains(t, output.String(), "gh secret set DEMO_APP_KEY < path/to/private-key.pem")
				require.Contains(t, output.String(), "gh variable set DEMO_APP_ID")
			} else {
				require.Contains(t, output.String(), "id-token: write")
				require.Contains(t, output.String(), "No static token secret")
			}
			err := testDevelopmentConnector(t.Context(), io.Discard, declaration, developmentOptions("agent", "read-issue", `{"issue":"ISSUE-1"}`), func(string) string {
				t.Fatal("identity routes must not try token environment fallback")
				return ""
			}, nil)
			require.ErrorContains(t, err, route.match)
		})
	}
}

func TestConnectorDevelopmentRedirectAndTransportFailures(t *testing.T) {
	t.Parallel()
	var redirected atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		http.Redirect(w, request, target.URL, http.StatusFound)
	}))
	defer server.Close()
	declaration := loadDevelopmentConnector(t, strings.ReplaceAll(developmentConnectorFixture, "https://api.example.test", server.URL), map[string][]string{"issues": {"ISSUE-1"}})
	options := developmentOptions("agent", "read-issue", `{"issue":"ISSUE-1"}`)
	err := testDevelopmentConnector(t.Context(), io.Discard, declaration, options, func(string) string { return "synthetic-token" }, server.Client())
	require.ErrorContains(t, err, "HTTP request failed")
	require.EqualValues(t, 1, calls.Load())
	require.Zero(t, redirected.Load())
	err = testDevelopmentConnector(t.Context(), io.Discard, declaration, options, func(string) string { return "synthetic-token" }, &http.Client{Transport: developmentRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("Authorization: Bearer synthetic-token")
	})})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "synthetic-token")
}

func TestConnectorDevelopmentNodeConformance(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for shared Node conformance tests")
	}
	declaration := loadDevelopmentConnector(t, developmentConnectorFixture, map[string][]string{"issues": {"ISSUE-1"}})
	options := developmentOptions("safe-output", "add-note", `{"issue":"ISSUE-1","text":"Local test"}`)
	options.timeout = 20 * time.Second
	options.nodeRuntime = "../../actions/setup/js/connector_runtime.cjs"
	var output bytes.Buffer
	require.NoError(t, testDevelopmentConnector(t.Context(), &output, declaration, options, func(string) string {
		t.Fatal("Node conformance and staging must not inspect credentials")
		return ""
	}, nil))
	require.Contains(t, output.String(), "Shared Node plan conformance also passed")
	options.arguments = `{"issue":"UNAPPROVED","text":"Local test"}`
	require.ErrorContains(t, testDevelopmentConnector(t.Context(), io.Discard, declaration, options, nil, nil), "Node connector conformance failed")
	options.nodeRuntime = "missing-runtime.cjs"
	require.ErrorContains(t, testDevelopmentConnector(t.Context(), io.Discard, declaration, options, nil, nil), "trusted local")
}

func TestConnectorDevelopmentHelpAndInvalidOptions(t *testing.T) {
	t.Parallel()
	output, err := executeDevelopmentCommand("demo", "test", "--help")
	require.NoError(t, err)
	for _, flag := range []string{"--file", "--input", "--grant", "--phase", "--capability", "--args", "--remote-tool", "--node-runtime", "--timeout"} {
		require.Contains(t, output, flag)
	}
	for _, args := range [][]string{{}, {"demo"}, {"demo", "unknown"}, {"demo", "setup", "extra"}} {
		_, err := executeDevelopmentCommand(args...)
		require.Error(t, err)
	}
	declaration := loadDevelopmentConnector(t, developmentConnectorFixture, map[string][]string{"issues": {"ISSUE-1"}})
	for _, options := range []*connectorDevelopmentOptions{
		developmentOptions("", "read-issue", "{}"),
		developmentOptions("agent", "", "{}"),
		developmentOptions("agent", "unknown", "{}"),
		developmentOptions("agent", "read-issue", "[]"),
		developmentOptions("agent", "read-issue", "null"),
		developmentOptions("agent", "read-issue", "{} {}"),
		{phase: "agent", capability: "read-issue", arguments: "{}", timeout: 21 * time.Second},
		{phase: "agent", capability: "read-issue", arguments: "{}", timeout: time.Second, remoteTool: "unrelated"},
	} {
		require.Error(t, testDevelopmentConnector(context.Background(), io.Discard, declaration, options, nil, nil))
	}
}
