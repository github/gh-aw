//go:build !integration

package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type datadogAccess struct{ secrets, requests int }

func datadogRuntime(t *testing.T, phase, secret string, respond func(*http.Request) (int, string)) (connectors.Runtime, *datadogAccess) {
	t.Helper()
	access := &datadogAccess{}
	return connectors.Runtime{
		Phase: phase, AllowWrites: phase == "safe-output",
		AllowedOrigins: []string{"https://api.datadoghq.com"},
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			access.secrets++
			require.Equal(t, phase, actualPhase)
			require.Equal(t, secret, name)
			return "fixture-oauth-access-token", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			access.requests++
			require.Equal(t, "api.datadoghq.com", request.URL.Host)
			require.Equal(t, "Bearer fixture-oauth-access-token", request.Header.Get("Authorization"))
			require.Empty(t, request.Header.Get("DD-API-KEY"))
			require.Empty(t, request.Header.Get("DD-APPLICATION-KEY"))
			require.Empty(t, request.URL.RawQuery)
			status, body := respond(request)
			return &http.Response{
				StatusCode: status, Request: request,
				Header: http.Header{"Retry-After": {"1"}, "Location": {"https://untrusted.invalid/private"}},
				Body:   io.NopCloser(strings.NewReader(body)),
			}, nil
		})},
	}, access
}

func TestDatadogConnectorFrozenMetadataReads(t *testing.T) {
	reader, _ := datadogPlans(t)
	for _, test := range []struct {
		operation, input, id, secret, path, fixture, want string
	}{
		{"get-monitor", "monitor-id", "12345678", "DATADOG_MONITOR_READ_OAUTH_TOKEN", "/api/v1/monitor/12345678", "monitor.json",
			`{"monitor-id":12345678,"name":"Build latency","type":"metric alert","state":"Alert"}`},
		{"get-incident", "incident-id", datadogIncidentID, "DATADOG_INCIDENT_READ_OAUTH_TOKEN", "/api/v2/incidents/" + datadogIncidentID, "incident.json",
			`{"incident-id":"` + datadogIncidentID + `","title":"Build latency investigation"}`},
	} {
		t.Run(test.operation, func(t *testing.T) {
			runtime, access := datadogRuntime(t, "agent", test.secret, func(request *http.Request) (int, string) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, test.path, request.URL.Path)
				return http.StatusOK, datadogFixture(t, test.fixture)
			})
			result, err := reader.Execute(t.Context(), runtime, "mcp", test.operation, map[string]any{test.input: test.id})
			require.NoError(t, err)
			require.True(t, result.Untrusted)
			require.JSONEq(t, test.want, string(result.Data))
			require.NotContains(t, string(result.Data), "synthetic-private")
			require.Equal(t, &datadogAccess{secrets: 1, requests: 1}, access)
		})
	}
}

func TestDatadogConnectorTitleOnlyStagingAndExecution(t *testing.T) {
	_, writer := datadogPlans(t)
	title := `Build evidence: literal $(not-code) ${{ not.an.expression }}`
	args := map[string]any{"incident-id": datadogIncidentID, "title": title}
	runtime, access := datadogRuntime(t, "safe-output", "DATADOG_INCIDENT_WRITE_OAUTH_TOKEN", func(request *http.Request) (int, string) {
		require.Equal(t, http.MethodPatch, request.Method)
		require.Equal(t, "/api/v2/incidents/"+datadogIncidentID, request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"data":{"id":"`+datadogIncidentID+`","type":"incidents","attributes":{"title":"`+title+`"}}}`, string(body))
		return http.StatusOK, strings.Replace(datadogFixture(t, "incident.json"), "Build latency investigation", title, 1)
	})
	staged, err := writer.PrepareSafeOutput("rename-incident", args)
	require.NoError(t, err)
	require.Zero(t, access.secrets)
	require.Zero(t, access.requests)
	serialized, err := json.Marshal(staged)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "TOKEN")
	require.NotContains(t, string(serialized), "fixture-oauth")
	tampered := *staged
	tampered.Digest = strings.Repeat("0", len(staged.Digest))
	_, err = writer.ExecuteSafeOutput(t.Context(), runtime, tampered)
	require.Error(t, err)
	require.Zero(t, access.secrets)
	result, err := writer.ExecuteSafeOutput(t.Context(), runtime, *staged)
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.NotContains(t, string(result.Data), "synthetic-private")
	require.JSONEq(t, `{"incident-id":"`+datadogIncidentID+`","title":"`+title+`"}`, string(result.Data))
	require.Equal(t, &datadogAccess{secrets: 1, requests: 1}, access)
}

func TestDatadogConnectorRejectsBeforeCredentialAccess(t *testing.T) {
	reader, writer := datadogPlans(t)
	runtime, access := datadogRuntime(t, "safe-output", "DATADOG_INCIDENT_WRITE_OAUTH_TOKEN", func(*http.Request) (int, string) {
		t.Fatal("invalid arguments must not contact Datadog")
		return 0, ""
	})
	for _, args := range []map[string]any{
		{"incident-id": "99999999-2222-4333-8444-555555555555", "title": "title"},
		{"incident-id": "../" + datadogIncidentID, "title": "title"},
		{"incident-id": strings.ToUpper(datadogIncidentID + "a"), "title": "title"},
		{"incident-id": datadogIncidentID, "title": ""},
		{"incident-id": datadogIncidentID, "title": " \n\t"},
		{"incident-id": datadogIncidentID, "title": strings.Repeat("x", 129)},
		{"incident-id": datadogIncidentID, "title": "title", "state": "resolved"},
		{"incident-id": datadogIncidentID, "title": "title", "url": "https://untrusted.invalid"},
		{"incident-id": datadogIncidentID, "title": "title", "credential": "other"},
		{"incident-id": datadogIncidentID},
	} {
		_, err := writer.PrepareSafeOutput("rename-incident", args)
		require.Error(t, err)
		_, err = writer.Execute(t.Context(), runtime, "safe-output", "rename-incident", args)
		require.Error(t, err)
	}
	for _, id := range []any{"0", "012345678", "12345679", "../12345678", strings.Repeat("1", 16), 12345678} {
		agent := runtime
		agent.Phase = "agent"
		_, err := reader.Execute(t.Context(), agent, "mcp", "get-monitor", map[string]any{"monitor-id": id})
		require.Error(t, err)
	}
	require.Zero(t, access.secrets)
	require.Zero(t, access.requests)
}

func TestDatadogConnectorHostAndPhaseBoundaries(t *testing.T) {
	reader, writer := datadogPlans(t)
	runtime, access := datadogRuntime(t, "safe-output", "DATADOG_INCIDENT_WRITE_OAUTH_TOKEN", func(*http.Request) (int, string) {
		t.Fatal("unauthorized host must not contact Datadog")
		return 0, ""
	})
	args := map[string]any{"incident-id": datadogIncidentID, "title": "title"}
	for _, change := range []func(*connectors.Runtime){
		func(r *connectors.Runtime) { r.Phase = "agent" },
		func(r *connectors.Runtime) { r.AllowWrites = false },
		func(r *connectors.Runtime) { r.AllowedOrigins = []string{"https://untrusted.invalid"} },
	} {
		denied := runtime
		change(&denied)
		_, err := writer.Execute(t.Context(), denied, "safe-output", "rename-incident", args)
		require.Error(t, err)
	}
	_, err := reader.Execute(t.Context(), runtime, "safe-output", "rename-incident", args)
	require.Error(t, err)
	_, err = writer.Execute(t.Context(), runtime, "mcp", "get-incident", map[string]any{"incident-id": datadogIncidentID})
	require.Error(t, err)
	require.Zero(t, access.secrets)
	require.Zero(t, access.requests)
}

func TestDatadogConnectorProviderErrorsAndEvidence(t *testing.T) {
	reader, writer := datadogPlans(t)
	valid := datadogFixture(t, "incident.json")
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"unauthenticated", `{"errors":["private provider detail"]}`, 401},
		{"forbidden", valid, 403}, {"throttled", valid, 429}, {"provider-outage", valid, 503},
		{"redirect", valid, 307}, {"empty", "", 200}, {"malformed", "{", 200},
		{"error-envelope", `{"errors":["failure"]}`, 200},
		{"wrong-type", strings.Replace(valid, `"type": "incidents"`, `"type": "users"`, 1), 200},
		{"wrong-id", strings.ReplaceAll(valid, datadogIncidentID, "99999999-2222-4333-8444-555555555555"), 200},
		{"title-not-applied", strings.Replace(valid, "Build latency investigation", "Different title", 1), 200},
		{"title-object", strings.Replace(valid, `"Build latency investigation"`, `{"private":"not a title"}`, 1), 200},
		{"oversized", strings.Repeat("x", 65537), 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime, access := datadogRuntime(t, "safe-output", "DATADOG_INCIDENT_WRITE_OAUTH_TOKEN", func(*http.Request) (int, string) {
				return test.status, test.body
			})
			_, err := writer.Execute(t.Context(), runtime, "safe-output", "rename-incident",
				map[string]any{"incident-id": datadogIncidentID, "title": "Build latency investigation"})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private provider detail")
			require.Equal(t, 1, access.requests, "provider errors must not retry writes")
			require.Equal(t, 1, access.secrets)
		})
	}
	for _, body := range []string{
		`{"id":12345679,"name":"other","type":"metric alert","overall_state":"OK"}`,
		`{"id":"12345678","name":"wrong ID type","type":"metric alert","overall_state":"OK"}`,
		`{"id":12345678,"name":{},"type":"metric alert","overall_state":"OK"}`,
		`{"id":12345678,"name":"missing state","type":"metric alert"}`,
	} {
		runtime, access := datadogRuntime(t, "agent", "DATADOG_MONITOR_READ_OAUTH_TOKEN", func(*http.Request) (int, string) { return 200, body })
		_, err := reader.Execute(t.Context(), runtime, "mcp", "get-monitor", map[string]any{"monitor-id": "12345678"})
		require.Error(t, err)
		require.Equal(t, 1, access.requests)
	}
}

func TestDatadogConnectorCredentialFailureNoFallback(t *testing.T) {
	t.Setenv("DD_API_KEY", "must-not-use")
	t.Setenv("DD_APP_KEY", "must-not-use")
	reader, _ := datadogPlans(t)
	runtime, access := datadogRuntime(t, "agent", "DATADOG_MONITOR_READ_OAUTH_TOKEN", func(*http.Request) (int, string) {
		t.Fatal("credential failure cannot fall back to keys")
		return 0, ""
	})
	runtime.ResolveSecret = func(_ context.Context, phase, name string) (string, error) {
		access.secrets++
		require.Equal(t, "agent", phase)
		require.Equal(t, "DATADOG_MONITOR_READ_OAUTH_TOKEN", name)
		return "", errors.New("OAuth token unavailable")
	}
	_, err := reader.Execute(t.Context(), runtime, "mcp", "get-monitor", map[string]any{"monitor-id": "12345678"})
	require.ErrorContains(t, err, "credential resolution failed")
	require.Equal(t, 1, access.secrets)
	require.Zero(t, access.requests)
}

func TestDatadogConnectorLocalMCPAllowlist(t *testing.T) {
	reader, _ := datadogPlans(t)
	runtime, access := datadogRuntime(t, "agent", "DATADOG_MONITOR_READ_OAUTH_TOKEN", func(*http.Request) (int, string) {
		return http.StatusOK, datadogFixture(t, "monitor.json")
	})
	server, err := reader.MCPServer(runtime)
	require.NoError(t, err)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "datadog-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer session.Close()
	listed, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	require.ElementsMatch(t, []string{"datadog_get_monitor", "datadog_get_incident"}, names)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "datadog_get_monitor", Arguments: map[string]any{"monitor-id": "12345678"}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, 1, access.requests)
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "datadog_rename_incident", Arguments: map[string]any{"incident-id": datadogIncidentID, "title": "not allowed"}})
	require.True(t, err != nil || result.IsError)
	require.Equal(t, 1, access.requests)
}

func TestDatadogConnectorResponseByteBoundary(t *testing.T) {
	reader, _ := datadogPlans(t)
	base := strings.TrimSuffix(strings.TrimSpace(datadogFixture(t, "monitor.json")), "}") + `,"padding":""}`
	for _, size := range []int{65536, 65537} {
		body := strings.TrimSuffix(base, `"}`) + strings.Repeat("x", size-len(base)) + `"}`
		require.Len(t, body, size)
		runtime, access := datadogRuntime(t, "agent", "DATADOG_MONITOR_READ_OAUTH_TOKEN", func(*http.Request) (int, string) { return 200, body })
		result, err := reader.Execute(t.Context(), runtime, "mcp", "get-monitor", map[string]any{"monitor-id": "12345678"})
		if size == 65536 {
			require.NoError(t, err)
			require.Less(t, len(result.Data), 128)
		} else {
			require.Error(t, err)
		}
		require.Equal(t, 1, access.requests)
	}
}

func TestDatadogConnectorExactIncidentEvidence(t *testing.T) {
	const otherID = "99999999-2222-4333-8444-555555555555"
	source := strings.Replace(datadogFixture(t, "workflow.md"), "["+datadogIncidentID+"]", "["+datadogIncidentID+", "+otherID+"]", 1)
	_, data, _, err := parseDatadogConsumer(t, source)
	require.NoError(t, err)
	config := data.Connectors["datadog"]
	for _, test := range []struct {
		plan       *connectors.Plan
		phase      string
		capability string
		operation  string
		secret     string
		args       map[string]any
	}{
		{config.Agent, "agent", "mcp", "get-incident", "DATADOG_INCIDENT_READ_OAUTH_TOKEN", map[string]any{"incident-id": datadogIncidentID}},
		{config.SafeOutputs, "safe-output", "safe-output", "rename-incident", "DATADOG_INCIDENT_WRITE_OAUTH_TOKEN",
			map[string]any{"incident-id": datadogIncidentID, "title": "Build latency investigation"}},
	} {
		runtime, access := datadogRuntime(t, test.phase, test.secret, func(*http.Request) (int, string) {
			return 200, strings.ReplaceAll(datadogFixture(t, "incident.json"), datadogIncidentID, otherID)
		})
		_, err := test.plan.Execute(t.Context(), runtime, test.capability, test.operation, test.args)
		require.ErrorContains(t, err, "does not match request input", "another approved resource cannot satisfy this request")
		require.Equal(t, 1, access.requests)
	}
}

func TestDatadogConnectorTitleBoundaryAndPrunedPlans(t *testing.T) {
	reader, writer := datadogPlans(t)
	_, err := writer.PrepareSafeOutput("rename-incident", map[string]any{"incident-id": datadogIncidentID, "title": strings.Repeat("x", 128)})
	require.NoError(t, err)
	_, err = writer.PrepareSafeOutput("rename-incident", map[string]any{"incident-id": datadogIncidentID, "title": strings.Repeat("\u00e9", 128)})
	require.NoError(t, err)
	_, err = writer.PrepareSafeOutput("rename-incident", map[string]any{"incident-id": datadogIncidentID, "title": strings.Repeat("\u00e9", 129)})
	require.Error(t, err)
	for _, test := range []struct {
		plan    *connectors.Plan
		exclude []string
	}{
		{reader, []string{"DATADOG_INCIDENT_WRITE_OAUTH_TOKEN", "rename-incident", "incident_write"}},
		{writer, []string{"DATADOG_MONITOR_READ_OAUTH_TOKEN", "DATADOG_INCIDENT_READ_OAUTH_TOKEN", "get-monitor", "get-incident", "monitors_read", "incident_read"}},
	} {
		encoded, err := test.plan.Encode()
		require.NoError(t, err)
		planJSON, err := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, err)
		for _, name := range test.exclude {
			require.NotContains(t, string(planJSON), name)
		}
	}
}

func TestDatadogConnectorExactNumericMonitorEvidence(t *testing.T) {
	source := strings.Replace(datadogFixture(t, "workflow.md"), `["12345678"]`, `["12345678", "12345679", "999999999999999"]`, 1)
	_, data, _, err := parseDatadogConsumer(t, source)
	require.NoError(t, err)
	encoded, err := data.Connectors["datadog"].Agent.Encode()
	require.NoError(t, err)
	reader, err := connectors.DecodePlan(encoded)
	require.NoError(t, err)
	for _, token := range []string{
		"12345679", "12345678.0", "1.2345678e7", "12345678.0000000001",
		"9007199254740991.1", "9007199254740992", "9007199254740993", "012345678",
	} {
		t.Run(token, func(t *testing.T) {
			runtime, access := datadogRuntime(t, "agent", "DATADOG_MONITOR_READ_OAUTH_TOKEN", func(*http.Request) (int, string) {
				return 200, strings.Replace(datadogFixture(t, "monitor.json"), `"id": 12345678`, `"id": `+token, 1)
			})
			_, err := reader.Execute(t.Context(), runtime, "mcp", "get-monitor", map[string]any{"monitor-id": "12345678"})
			require.Error(t, err, "only the exact canonical requested monitor ID establishes identity")
			require.Equal(t, 1, access.requests)
		})
	}
	runtime, access := datadogRuntime(t, "agent", "DATADOG_MONITOR_READ_OAUTH_TOKEN", func(request *http.Request) (int, string) {
		require.Equal(t, "/api/v1/monitor/999999999999999", request.URL.Path)
		return 200, strings.Replace(datadogFixture(t, "monitor.json"), `"id": 12345678`, `"id": 999999999999999`, 1)
	})
	result, err := reader.Execute(t.Context(), runtime, "mcp", "get-monitor", map[string]any{"monitor-id": "999999999999999"})
	require.NoError(t, err)
	require.Contains(t, string(result.Data), "999999999999999")
	require.Equal(t, 1, access.requests)
}
