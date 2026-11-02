package connectors

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServiceNowRESTBackedMCP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/now/table/incident/"+serviceNowIncidentID || r.Header.Get("Authorization") != "Bearer test-agent" {
			t.Error("MCP adapter must use only the authenticated fixed Table API read")
		}
		if _, err := io.WriteString(w, `{"result":{"sys_id":"`+serviceNowIncidentID+`","number":"INC0009009","state":"2","sys_mod_count":"17"}}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	plan := serviceNowPlan(t, server.URL)
	adapter, err := plan.MCPServer(serviceNowRuntime(server.Client(), server.URL, "agent"))
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := adapter.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "servicenow-fixture", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 2 {
		t.Fatal("only two scoped ServiceNow read tools may be exposed")
	}
	for _, tool := range tools.Tools {
		if tool.Name != "get-incident" && tool.Name != "get-requested-item" {
			t.Fatalf("unexpected exposed tool %s", tool.Name)
		}
	}
	reply, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "get-incident", Arguments: map[string]any{"record": serviceNowIncidentID},
	})
	if err != nil || reply.IsError || len(reply.Content) != 1 {
		t.Fatalf("REST-backed MCP read failed: %+v %v", reply, err)
	}
	text, ok := reply.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected a structured read result in MCP text content")
	}
	var result Result
	if err := json.Unmarshal([]byte(text.Text), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Untrusted || calls.Load() != 1 {
		t.Fatal("MCP result must be untrusted and perform exactly one bounded read")
	}
	for _, input := range []struct {
		tool string
		args map[string]any
	}{
		{"get-incident", map[string]any{"record": serviceNowRequestedItemID}},
		{"append-incident-work-note", map[string]any{"record": serviceNowIncidentID, "note": "not permitted"}},
	} {
		reply, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: input.tool, Arguments: input.args})
		if err == nil && !reply.IsError {
			t.Fatal("MCP accepted an out-of-scope read or a write tool")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("rejected MCP calls reached the provider")
	}
}
