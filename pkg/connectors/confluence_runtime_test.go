package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const confluenceFixtureCloud = "11111111-2222-4333-8444-555555555555"

func confluenceFixturePlan(t *testing.T, origin string) *Plan {
	t.Helper()
	spec, err := LoadSpec("testdata/providers/confluence/spec.yaml")
	require.NoError(t, err)
	var connection Connection
	var binding Binding
	for file, target := range map[string]any{"connection.yaml": &connection, "binding.yaml": &binding} {
		data, err := os.ReadFile("testdata/providers/confluence/" + file)
		require.NoError(t, err)
		require.NoError(t, Decode(data, target))
	}
	if origin == "" {
		plan, err := Lower("../..", binding)
		require.NoError(t, err)
		return plan
	}
	spec.Endpoints["api"] = origin
	binding.Definition, binding.Connection = "spec.yaml", "connection.yaml"
	return writePlan(t, *spec, connection, binding)
}

func confluenceRuntime(t *testing.T, server *httptest.Server, phase string) Runtime {
	t.Helper()
	return Runtime{
		Phase: phase, AllowedOrigins: []string{server.URL}, Client: server.Client(),
		AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			phases := map[string]string{
				"CONFLUENCE_READ_ACCESS_TOKEN":          "agent",
				"CONFLUENCE_CONTEXT_ACCESS_TOKEN":       "context",
				"CONFLUENCE_PAGE_WRITE_ACCESS_TOKEN":    "safe-output",
				"CONFLUENCE_COMMENT_WRITE_ACCESS_TOKEN": "safe-output",
			}
			require.Equal(t, phase, actualPhase)
			require.Equal(t, phase, phases[name])
			return "fixture-only-" + name, nil
		},
	}
}

func confluenceReadArgs() map[string]any {
	return map[string]any{"cloud-id": confluenceFixtureCloud, "space-id": "123", "page-id": "456"}
}

func TestConfluenceConnectorLowerAndProject(t *testing.T) {
	plan := confluenceFixturePlan(t, "")
	require.NotEmpty(t, plan.Digest)
	require.ErrorIs(t, plan.ActivateTriggers(), ErrIngressUnavailable)
	data, err := os.ReadFile("testdata/providers/confluence/page-created.json")
	require.NoError(t, err)
	event, err := plan.ProjectEvent("page-created", "avi:confluence:created:page", data)
	require.NoError(t, err)
	require.True(t, event.Untrusted)
	require.False(t, event.Operational)
	require.Equal(t, "456", event.Fields["subject"])
	require.Equal(t, json.Number("123"), event.Fields["space"])
	for _, invalid := range []string{
		strings.ReplaceAll(string(data), `"456"`, `"999"`),
		strings.ReplaceAll(string(data), `"id": 123`, `"id": 999`),
		strings.ReplaceAll(string(data), `"type": "page"`, `"type": "blogpost"`),
		strings.ReplaceAll(string(data), `"status": "current"`, `"status": "draft"`),
	} {
		_, err := plan.ProjectEvent("page-created", "avi:confluence:created:page", []byte(invalid))
		require.Error(t, err)
	}
	_, err = plan.ProjectEvent("page-created", "avi:confluence:updated:page", data)
	require.Error(t, err)
}

func TestConfluenceConnectorReadRuntime(t *testing.T) {
	data, err := os.ReadFile("testdata/providers/confluence/pages-response.json")
	require.NoError(t, err)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/ex/confluence/"+confluenceFixtureCloud+"/wiki/api/v2/pages", r.URL.Path)
		for key, value := range map[string]string{"id": "456", "space-id": "123", "status": "current", "body-format": "storage", "limit": "1"} {
			assert.Equal(t, value, r.URL.Query().Get(key))
		}
		assert.Contains(t, []string{"Bearer fixture-only-CONFLUENCE_READ_ACCESS_TOKEN", "Bearer fixture-only-CONFLUENCE_CONTEXT_ACCESS_TOKEN"}, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write(data)
		assert.NoError(t, err)
	}))
	defer server.Close()
	plan := confluenceFixturePlan(t, server.URL)
	for _, phase := range []string{"agent", "context"} {
		capability, name := "mcp", "get-page"
		if phase == "context" {
			capability, name = "context", "runbook"
		}
		result, err := plan.Execute(t.Context(), confluenceRuntime(t, server, phase), capability, name, confluenceReadArgs())
		require.NoError(t, err)
		require.True(t, result.Untrusted)
		var projected map[string]any
		require.NoError(t, json.Unmarshal(result.Data, &projected))
		require.Len(t, projected, 6)
		require.Equal(t, "456", projected["page"])
		require.Equal(t, "storage", projected["representation"])
	}
}

func TestConfluenceConnectorDenyBeforeCredentials(t *testing.T) {
	plan := confluenceFixturePlan(t, "")
	resolved := 0
	runtime := Runtime{Phase: "agent", AllowedOrigins: []string{"https://api.atlassian.com"},
		ResolveSecret: func(context.Context, string, string) (string, error) {
			resolved++
			return "must-not-resolve", nil
		}}
	for _, input := range []string{"cloud-id", "space-id", "page-id", "unexpected"} {
		args := confluenceReadArgs()
		args[input] = "999"
		_, err := plan.Execute(t.Context(), runtime, "mcp", "get-page", args)
		require.Error(t, err)
	}
	_, err := plan.Execute(t.Context(), runtime, "safe-output", "create-runbook", map[string]any{})
	require.Error(t, err)
	_, err = plan.PrepareSafeOutput("update-page", map[string]any{"expected-version": 1})
	require.Error(t, err)
	require.Zero(t, resolved)
}

func TestConfluenceConnectorResponseGuards(t *testing.T) {
	fixture, err := os.ReadFile("testdata/providers/confluence/pages-response.json")
	require.NoError(t, err)
	for name, body := range map[string]string{
		"wrong page":     strings.ReplaceAll(string(fixture), `"456"`, `"999"`),
		"wrong space":    strings.ReplaceAll(string(fixture), `"123"`, `"999"`),
		"wrong format":   strings.ReplaceAll(string(fixture), `"representation": "storage"`, `"representation": "view"`),
		"empty results":  `{"results":[]}`,
		"oversize body":  strings.Repeat("x", 65537),
		"malformed body": `{`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(body))
				assert.NoError(t, err)
			}))
			defer server.Close()
			plan := confluenceFixturePlan(t, server.URL)
			_, err := plan.Execute(t.Context(), confluenceRuntime(t, server, "context"), "context", "runbook", confluenceReadArgs())
			require.Error(t, err)
		})
	}
}

func TestConfluenceConnectorExactResponseBound(t *testing.T) {
	fixture, err := os.ReadFile("testdata/providers/confluence/pages-response.json")
	require.NoError(t, err)
	for _, size := range []int{65535, 65536, 65537} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			body := append([]byte(strings.Repeat(" ", size-len(fixture))), fixture...)
			require.Len(t, body, size)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write(body)
				assert.NoError(t, err)
			}))
			defer server.Close()
			plan := confluenceFixturePlan(t, server.URL)
			_, err := plan.Execute(t.Context(), confluenceRuntime(t, server, "context"), "context", "runbook", confluenceReadArgs())
			if size > 65536 {
				require.ErrorContains(t, err, "byte bound")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfluenceConnectorMCPAdapter(t *testing.T) {
	data, err := os.ReadFile("testdata/providers/confluence/pages-response.json")
	require.NoError(t, err)
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, err := w.Write(data)
		assert.NoError(t, err)
	}))
	defer server.Close()
	plan := confluenceFixturePlan(t, server.URL)
	adapter, err := plan.MCPServer(confluenceRuntime(t, server, "agent"))
	require.NoError(t, err)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	session, err := adapter.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, session.Close()) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "confluence-fixture", Version: "1"}, nil)
	downstream, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, downstream.Close()) }()
	tools, err := downstream.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)
	require.Equal(t, "get-confluence-page", tools.Tools[0].Name)
	result, err := downstream.CallTool(t.Context(), &mcp.CallToolParams{Name: "get-confluence-page", Arguments: confluenceReadArgs()})
	require.NoError(t, err)
	require.False(t, result.IsError)
	args := confluenceReadArgs()
	args["page-id"] = "999"
	denied, err := downstream.CallTool(t.Context(), &mcp.CallToolParams{Name: "get-confluence-page", Arguments: args})
	require.NoError(t, err)
	require.True(t, denied.IsError)
	require.EqualValues(t, 1, calls.Load())
}

func TestConfluenceConnectorStagedWrites(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, map[string]any{"representation": "storage", "value": "<p>Review the workflow result before taking action.</p>"}, body["body"])
		if strings.HasSuffix(r.URL.Path, "/footer-comments") {
			assert.Equal(t, "Bearer fixture-only-CONFLUENCE_COMMENT_WRITE_ACCESS_TOKEN", r.Header.Get("Authorization"))
			assert.Equal(t, "456", body["pageId"])
			_, err := w.Write([]byte(`{"id":"789","pageId":"456","version":{"number":1}}`))
			assert.NoError(t, err)
			return
		}
		assert.Equal(t, "Bearer fixture-only-CONFLUENCE_PAGE_WRITE_ACCESS_TOKEN", r.Header.Get("Authorization"))
		assert.Equal(t, "123", body["spaceId"])
		assert.Equal(t, "456", body["parentId"])
		_, err := w.Write([]byte(`{"id":"789","spaceId":"123","parentId":"456","version":{"number":1}}`))
		assert.NoError(t, err)
	}))
	defer server.Close()
	plan := confluenceFixturePlan(t, server.URL)
	for name, args := range map[string]map[string]any{
		"create-runbook":     {"cloud-id": confluenceFixtureCloud, "space-id": "123", "parent-id": "456"},
		"add-review-comment": {"cloud-id": confluenceFixtureCloud, "page-id": "456"},
	} {
		before := calls.Load()
		staged, err := plan.PrepareSafeOutput(name, args)
		require.NoError(t, err)
		require.Equal(t, before, calls.Load())
		_, err = plan.ExecuteSafeOutput(t.Context(), confluenceRuntime(t, server, "safe-output"), *staged)
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, calls.Load())
}

func TestConfluenceConnectorNoWriteRetry(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusConflict, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			defer server.Close()
			plan := confluenceFixturePlan(t, server.URL)
			_, err := plan.Execute(t.Context(), confluenceRuntime(t, server, "safe-output"), "safe-output", "add-review-comment",
				map[string]any{"cloud-id": confluenceFixtureCloud, "page-id": "456"})
			require.Error(t, err)
			require.EqualValues(t, 1, calls.Load())
		})
	}
}
