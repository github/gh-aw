package connectors_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLinearConnectorNativeMCP(t *testing.T) {
	plan := linearPlan(t)
	upstream := mcp.NewServer(&mcp.Implementation{Name: "linear-fixture", Version: "1"}, nil)
	var calls, credentials atomic.Int32
	upstream.AddTool(&mcp.Tool{Name: "get_issue", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		if !strings.Contains(string(request.Params.Arguments), linearIssue) {
			t.Error("native MCP issue argument changed")
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Untrusted Linear issue fixture"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return upstream }, nil)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/mcp/readonly" || request.Header.Get("Authorization") != "Bearer fixture-mcp-read" {
			t.Error("native MCP must use readonly path and separate bearer credential")
		}
		handler.ServeHTTP(w, request)
	}))
	defer server.Close()
	local, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: linearTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://mcp.linear.app/mcp/readonly" {
			t.Error("remote MCP endpoint was not fixed")
			return nil, errors.New("unexpected endpoint")
		}
		copy := request.Clone(request.Context())
		copy.URL.Scheme, copy.URL.Host = local.Scheme, local.Host
		copy.Host = local.Host
		return server.Client().Transport.RoundTrip(copy)
	})}
	runtime := connectors.Runtime{
		Phase: "agent", AllowedOrigins: []string{"https://mcp.linear.app"}, Client: client,
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			credentials.Add(1)
			if phase != "agent" || name != "LINEAR_READ_MCP_TOKEN" {
				t.Error("native MCP selected a non-MCP or non-agent credential")
				return "", errors.New("wrong credential")
			}
			return "fixture-mcp-read", nil
		},
	}
	adapter, err := plan.MCPServer(runtime)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	session, err := adapter.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	downstreamClient := mcp.NewClient(&mcp.Implementation{Name: "linear-gateway-fixture", Version: "1"}, nil)
	downstream, err := downstreamClient.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = downstream.Close() }()
	tools, err := downstream.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "read-only__get_issue" {
			found = true
		}
		if strings.Contains(tool.Name, "create") || strings.Contains(tool.Name, "update") || strings.Contains(tool.Name, "comment") {
			t.Fatal("agent MCP adapter exposed write tools")
		}
	}
	if !found {
		t.Fatal("native readonly Linear MCP tool was not registered")
	}
	result, err := downstream.CallTool(t.Context(), &mcp.CallToolParams{Name: "read-only__get_issue", Arguments: map[string]any{"id": linearIssue}})
	if err != nil || result.IsError || calls.Load() != 1 || len(result.Content) != 1 {
		if result != nil {
			for _, content := range result.Content {
				if text, ok := content.(*mcp.TextContent); ok {
					t.Log(text.Text)
				}
			}
		}
		t.Fatalf("native MCP adapter failed: %+v %v calls=%d", result, err, calls.Load())
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, `"untrusted":true`) || !strings.Contains(text.Text, "Untrusted Linear issue fixture") {
		t.Fatal("native MCP result must remain explicitly untrusted")
	}
	before := credentials.Load()
	for _, test := range []struct {
		tool string
		args map[string]any
	}{
		{"create_issue", map[string]any{"id": linearIssue}},
		{"*", map[string]any{"id": linearIssue}},
		{"get_issue", map[string]any{"id": "55555555-5555-4555-8555-555555555555"}},
		{"get_issue", map[string]any{"id": linearIssue, "includeRelations": true}},
		{"get_issue", map[string]any{"id": "ENG-12"}},
		{"get_issue", map[string]any{}},
	} {
		if _, err := plan.CallMCP(t.Context(), runtime, "read-only", test.tool, test.args); err == nil {
			t.Fatal("unapproved native MCP call accepted", test.tool, test.args)
		}
	}
	if credentials.Load() != before || calls.Load() != 1 {
		t.Fatal("denied MCP calls must not resolve credentials or reach the provider")
	}
}
