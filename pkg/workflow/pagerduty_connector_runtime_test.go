//go:build !integration

package workflow

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type pagerDutyTransport func(*http.Request) (*http.Response, error)

func (transport pagerDutyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func pagerDutyFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pagerdutyFixtureDirectory, name))
	require.NoError(t, err)
	return string(data)
}

func pagerDutyReadArgs() map[string]any {
	return map[string]any{"incident-id": "PINC001", "service-id": "PSVC001", "team-id": "PTEAM01"}
}

func pagerDutyNoteArgs() map[string]any {
	return map[string]any{"incident-id": "PINC001", "content": "Synthetic investigation recorded."}
}

func pagerDutyPlans(t *testing.T) (*connectors.Plan, *connectors.Plan) {
	t.Helper()
	_, data, _, err := parsePagerDutyWorkflow(t, pagerdutyConnectorImport,
		"tools:\n  pagerduty: [get-incident]\nsafe-outputs:\n  pagerduty-add-note:\n")
	require.NoError(t, err)
	return data.Connectors["pagerduty"].Agent, data.Connectors["pagerduty"].SafeOutputs
}

func pagerDutyRuntime(t *testing.T, phase string, secrets *int, transport pagerDutyTransport) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{
		Phase: phase, AllowWrites: phase == "safe-output",
		AllowedOrigins: []string{"https://api.pagerduty.com"},
		Client:         &http.Client{Transport: transport},
		ResolveSecret: func(_ context.Context, requestedPhase, name string) (string, error) {
			*secrets++
			require.Equal(t, phase, requestedPhase)
			expected := "PAGERDUTY_READ_AUTHORIZATION"
			if phase == "safe-output" {
				expected = "PAGERDUTY_WRITE_AUTHORIZATION"
			}
			require.Equal(t, expected, name)
			return "Token token=fixture-" + phase, nil
		},
	}
}

func pagerDutyResponse(request *http.Request, status int, body string) *http.Response {
	header := http.Header{}
	if status == http.StatusFound {
		header.Set("Location", "https://unapproved.example.invalid/notes")
	}
	return &http.Response{StatusCode: status, Header: header,
		Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestPagerDutyConnectorReadOnlyMCP(t *testing.T) {
	read, _ := pagerDutyPlans(t)
	calls, secrets := 0, 0
	runtime := pagerDutyRuntime(t, "agent", &secrets, func(request *http.Request) (*http.Response, error) {
		calls++
		return pagerDutyResponse(request, 200, pagerDutyFixture(t, "incident.json")), nil
	})
	server, err := read.MCPServer(runtime)
	require.NoError(t, err)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "pagerduty-fixture", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)
	require.Equal(t, "pagerduty_get_incident", tools.Tools[0].Name)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "pagerduty_get_incident", Arguments: pagerDutyReadArgs(),
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	for _, invocation := range []*mcp.CallToolParams{
		{Name: "add-note", Arguments: pagerDutyNoteArgs()},
		{Name: "pagerduty_get_incident", Arguments: map[string]any{"incident-id": "POTHER1", "service-id": "PSVC001", "team-id": "PTEAM01"}},
	} {
		result, err := session.CallTool(t.Context(), invocation)
		require.True(t, err != nil || result.IsError)
	}
	require.Equal(t, 1, calls)
	require.Equal(t, 1, secrets)
}

func TestPagerDutyConnectorReadProjection(t *testing.T) {
	read, _ := pagerDutyPlans(t)
	calls, secrets := 0, 0
	runtime := pagerDutyRuntime(t, "agent", &secrets, func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "https://api.pagerduty.com/incidents/PINC001", request.URL.String())
		require.Equal(t, "Token token=fixture-agent", request.Header.Get("Authorization"))
		require.Equal(t, "application/vnd.pagerduty+json;version=2", request.Header.Get("Accept"))
		require.Empty(t, request.Header.Get("From"), "read phase must not acquire the write actor")
		require.Zero(t, request.ContentLength)
		return pagerDutyResponse(request, 200, pagerDutyFixture(t, "incident.json")), nil
	})
	result, err := read.Execute(t.Context(), runtime, "mcp", "get-incident", pagerDutyReadArgs())
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.JSONEq(t, `{"incident-id":"PINC001","service-id":"PSVC001","status":"triggered","urgency":"high","title":"Synthetic integration incident"}`, string(result.Data))
	require.NotContains(t, string(result.Data), "private")
	require.Equal(t, 1, calls)
	require.Equal(t, 1, secrets)
}

func TestPagerDutyConnectorStagedNote(t *testing.T) {
	read, write := pagerDutyPlans(t)
	calls, secrets := 0, 0
	runtime := pagerDutyRuntime(t, "safe-output", &secrets, func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "POST", request.Method)
		require.Equal(t, "https://api.pagerduty.com/incidents/PINC001/notes", request.URL.String())
		require.Equal(t, "Token token=fixture-safe-output", request.Header.Get("Authorization"))
		require.Equal(t, "application/vnd.pagerduty+json;version=2", request.Header.Get("Accept"))
		require.Equal(t, "incident-automation@example.invalid", request.Header.Get("From"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"note":{"content":"Synthetic investigation recorded."}}`, string(body))
		return pagerDutyResponse(request, 201, pagerDutyFixture(t, "note.json")), nil
	})
	encoded, err := write.Encode()
	require.NoError(t, err)
	write, err = connectors.DecodePlan(encoded)
	require.NoError(t, err, "static metadata headers must survive the immutable snapshot")
	staged, err := write.PrepareSafeOutput("add-note", pagerDutyNoteArgs())
	require.NoError(t, err)
	for _, content := range []string{strings.Repeat("x", 2000), strings.Repeat("\u00e9", 2000), "Untrusted $(not-code); <xml>& data"} {
		intent, err := write.PrepareSafeOutput("add-note", map[string]any{"incident-id": "PINC001", "content": content})
		require.NoError(t, err)
		require.Equal(t, content, intent.Arguments["content"])
	}
	require.Zero(t, calls)
	require.Zero(t, secrets)
	_, err = read.PrepareSafeOutput("add-note", pagerDutyNoteArgs())
	require.Error(t, err)
	blocked := runtime
	blocked.Phase = "agent"
	_, err = write.ExecuteSafeOutput(t.Context(), blocked, *staged)
	require.Error(t, err)
	blocked = runtime
	blocked.AllowWrites = false
	_, err = write.ExecuteSafeOutput(t.Context(), blocked, *staged)
	require.Error(t, err)
	corrupt := *staged
	corrupt.Digest = "different-plan"
	_, err = write.ExecuteSafeOutput(t.Context(), runtime, corrupt)
	require.Error(t, err)
	require.Zero(t, calls)
	require.Zero(t, secrets)
	result, err := write.ExecuteSafeOutput(t.Context(), runtime, *staged)
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.JSONEq(t, `{"note-id":"PNOTE01","created-at":"2026-01-01T00:00:00Z"}`, string(result.Data))
	require.Equal(t, 1, calls)
	require.Equal(t, 1, secrets)
}

func TestPagerDutyConnectorArgumentsRejectedBeforeCredentials(t *testing.T) {
	read, write := pagerDutyPlans(t)
	calls, secrets := 0, 0
	transport := pagerDutyTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected network")
	})
	readRuntime := pagerDutyRuntime(t, "agent", &secrets, transport)
	for _, field := range []string{"incident-id", "service-id", "team-id"} {
		for _, bad := range []any{"POTHER1", "*", "../PINC001", "PINC001%2fnotes", "${{ secrets.NOPE }}", 123, strings.Repeat("P", 33)} {
			args := pagerDutyReadArgs()
			args[field] = bad
			_, err := read.Execute(t.Context(), readRuntime, "mcp", "get-incident", args)
			require.Error(t, err)
		}
		args := pagerDutyReadArgs()
		delete(args, field)
		_, err := read.Execute(t.Context(), readRuntime, "mcp", "get-incident", args)
		require.Error(t, err)
	}
	args := pagerDutyReadArgs()
	args["include"] = "users"
	_, err := read.Execute(t.Context(), readRuntime, "mcp", "get-incident", args)
	require.Error(t, err)
	for _, args := range []map[string]any{
		{"incident-id": "POTHER1", "content": "note"},
		{"incident-id": "PINC001", "content": ""},
		{"incident-id": "PINC001", "content": " \n\t"},
		{"incident-id": "PINC001", "content": strings.Repeat("x", 2001)},
		{"incident-id": "PINC001", "content": "note", "From": "agent@example.invalid"},
		{"incident-id": "PINC001", "content": "note", "status": "acknowledged"},
	} {
		_, err := write.PrepareSafeOutput("add-note", args)
		require.Error(t, err)
	}
	require.Zero(t, calls)
	require.Zero(t, secrets)
}

func TestPagerDutyConnectorResponseEvidence(t *testing.T) {
	imports := strings.NewReplacer(
		"incidents: [PINC001]", "incidents: [PINC001, POTHER1]",
		"services: [PSVC001]", "services: [PSVC001, POTHER1]",
		"teams: [PTEAM01]", "teams: [PTEAM01, POTHER1]",
	).Replace(pagerdutyConnectorImport)
	_, data, _, err := parsePagerDutyWorkflow(t, imports,
		"tools:\n  pagerduty: [get-incident]\nsafe-outputs:\n  pagerduty-add-note:\n")
	require.NoError(t, err)
	read, write := data.Connectors["pagerduty"].Agent, data.Connectors["pagerduty"].SafeOutputs
	for _, test := range []struct {
		name, fixture, from, to string
		plan                    *connectors.Plan
		phase, capability, op   string
		args                    map[string]any
	}{
		{"different-incident", "incident.json", "PINC001", "POTHER1", read, "agent", "mcp", "get-incident", pagerDutyReadArgs()},
		{"different-service", "incident.json", "PSVC001", "POTHER1", read, "agent", "mcp", "get-incident", pagerDutyReadArgs()},
		{"different-team", "incident.json", "PTEAM01", "POTHER1", read, "agent", "mcp", "get-incident", pagerDutyReadArgs()},
		{"wrong-resource-type", "incident.json", `"type": "incident"`, `"type": "service"`, read, "agent", "mcp", "get-incident", pagerDutyReadArgs()},
		{"missing-title", "incident.json", `"title": "Synthetic integration incident",`, "", read, "agent", "mcp", "get-incident", pagerDutyReadArgs()},
		{"wrong-status-type", "incident.json", `"status": "triggered"`, `"status": 12`, read, "agent", "mcp", "get-incident", pagerDutyReadArgs()},
		{"wrong-note-content", "note.json", "Synthetic investigation recorded.", "other note", write, "safe-output", "safe-output", "add-note", pagerDutyNoteArgs()},
		{"missing-note-id", "note.json", `"id": "PNOTE01",`, "", write, "safe-output", "safe-output", "add-note", pagerDutyNoteArgs()},
		{"wrong-note-id-type", "note.json", `"id": "PNOTE01"`, `"id": 12`, write, "safe-output", "safe-output", "add-note", pagerDutyNoteArgs()},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, secrets := 0, 0
			body := strings.Replace(pagerDutyFixture(t, test.fixture), test.from, test.to, 1)
			runtime := pagerDutyRuntime(t, test.phase, &secrets, func(request *http.Request) (*http.Response, error) {
				calls++
				return pagerDutyResponse(request, 200, body), nil
			})
			result, err := test.plan.Execute(t.Context(), runtime, test.capability, test.op, test.args)
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, 1, calls)
			require.Equal(t, 1, secrets)
		})
	}
}

func TestPagerDutyConnectorNoRetryOrSuccessFallback(t *testing.T) {
	_, write := pagerDutyPlans(t)
	for _, test := range []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{"unauthorized", 401, `{"error":"private diagnostic"}`, nil},
		{"forbidden", 403, `{}`, nil},
		{"rate-limit", 429, `{}`, nil},
		{"server-error", 500, `{}`, nil},
		{"redirect", 302, `{}`, nil},
		{"empty-success", 201, "", nil},
		{"invalid-json", 201, "private diagnostic", nil},
		{"oversized", 201, strings.Repeat("x", 16385), nil},
		{"network-error", 0, "", errors.New("private transport diagnostic")},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, secrets := 0, 0
			runtime := pagerDutyRuntime(t, "safe-output", &secrets, func(request *http.Request) (*http.Response, error) {
				calls++
				if test.err != nil {
					return nil, test.err
				}
				return pagerDutyResponse(request, test.status, test.body), nil
			})
			result, err := write.Execute(t.Context(), runtime, "safe-output", "add-note", pagerDutyNoteArgs())
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private")
			require.Nil(t, result)
			require.Equal(t, 1, calls)
			require.Equal(t, 1, secrets)
		})
	}
}
