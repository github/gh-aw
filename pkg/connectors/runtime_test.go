package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixturePlan(t *testing.T, endpoint string) *Plan {
	t.Helper()
	spec, err := LoadSpec("testdata/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec.Endpoints["api"] = endpoint
	connection := Connection{APIVersion: APIVersion, Kind: "ConnectorConnection", Connector: spec.ID,
		Credentials: map[string]SecretRef{"reader": {Secret: "READ_TOKEN"}, "writer": {Secret: "WRITE_TOKEN"}, "context-reader": {Secret: "CONTEXT_TOKEN"}},
		Resources:   map[string][]string{"issues": {"ISSUE-1"}},
	}
	binding := Binding{APIVersion: APIVersion, Kind: "ConnectorBinding", Definition: "spec.yaml", Connection: "connection.yaml",
		Triggers: []string{"issue-created"}, MCP: []string{"get-issue"}, SafeOutputs: []string{"add-comment"}, Context: []string{"issue-details"}}
	return writePlan(t, *spec, connection, binding)
}

func writePlan(t *testing.T, spec ConnectorSpec, connection Connection, binding Binding) *Plan {
	t.Helper()
	root := t.TempDir()
	for name, document := range map[string]any{"spec.yaml": spec, "connection.yaml": connection} {
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := Lower(root, binding)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func testRuntime(client *http.Client, origin, phase string) Runtime {
	return Runtime{Phase: phase, AllowedOrigins: []string{origin}, Client: client, AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			expected := map[string]string{"agent": "READ_TOKEN", "context": "CONTEXT_TOKEN", "safe-output": "WRITE_TOKEN"}[phase]
			if name != expected {
				return "", errors.New("wrong phase credential")
			}
			return phase + "-only", nil
		}}
}

func TestRuntimeFourCapabilities(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/issues/ISSUE-1" && r.URL.Path != "/issues/ISSUE-1/comments" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Method == http.MethodPost {
			if r.Header.Get("Authorization") != "Bearer safe-output-only" {
				t.Error("write used wrong credential")
			}
			data, err := io.ReadAll(r.Body)
			if err != nil || string(data) != `{"body":"untrusted ${{ text }}; $(not-code)"}` {
				t.Errorf("body was not serialized as data: %s (%v)", data, err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"id":"ISSUE-1","title":"hello"}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	plan := fixturePlan(t, server.URL)
	projection, err := plan.ProjectEvent("issue-created", "issue.created", []byte(`{"event":"issue.created","issue":{"id":"ISSUE-1","title":"hello"}}`))
	if err != nil || projection.Operational || !projection.Untrusted || projection.Fields["subject"] != "ISSUE-1" {
		t.Fatalf("unexpected projection: %+v %v", projection, err)
	}
	if !errors.Is(plan.ActivateTriggers(), ErrIngressUnavailable) {
		t.Fatal("declaring a trigger must not activate ingress")
	}
	for _, capability := range []string{"mcp", "context"} {
		name, phase := "get-issue", "agent"
		if capability == "context" {
			name, phase = "issue-details", "context"
		}
		result, err := plan.Execute(t.Context(), testRuntime(server.Client(), server.URL, phase), capability, name, map[string]any{"issue": "ISSUE-1"})
		if err != nil || !result.Untrusted || result.StatusCode != http.StatusOK {
			t.Fatalf("%s: %+v %v", capability, result, err)
		}
	}
	staged, err := plan.PrepareSafeOutput("add-comment", map[string]any{"issue": "ISSUE-1", "text": "untrusted ${{ text }}; $(not-code)"})
	if err != nil || requests != 2 {
		t.Fatal("preparing a safe output must not issue a request", err)
	}
	if _, err := plan.ExecuteSafeOutput(t.Context(), testRuntime(server.Client(), server.URL, "safe-output"), *staged); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("expected exactly 3 requests, got %d", requests)
	}
}

func TestRuntimeDenyBeforeCredentials(t *testing.T) {
	plan := fixturePlan(t, "https://api.example.com")
	calls := 0
	runtime := Runtime{Phase: "agent", AllowedOrigins: []string{"https://api.example.com"}, ResolveSecret: func(context.Context, string, string) (string, error) {
		calls++
		return "secret", nil
	}}
	for _, args := range []map[string]any{{}, {"issue": "OTHER"}, {"issue": 123}, {"issue": "ISSUE-1", "extra": true}} {
		if _, err := plan.Execute(t.Context(), runtime, "mcp", "get-issue", args); err == nil {
			t.Fatal("unsafe request accepted", args)
		}
	}
	if _, err := plan.Execute(t.Context(), runtime, "safe-output", "add-comment", map[string]any{"issue": "ISSUE-1", "text": "x"}); err == nil {
		t.Fatal("agent phase wrote")
	}
	runtime.AllowedOrigins = nil
	if _, err := plan.Execute(t.Context(), runtime, "mcp", "get-issue", map[string]any{"issue": "ISSUE-1"}); err == nil {
		t.Fatal("unapproved endpoint accepted")
	}
	if calls != 0 {
		t.Fatal("denied requests resolved credentials")
	}
}

func TestRuntimeResponseBoundsAndFailures(t *testing.T) {
	for _, body := range []string{strings.Repeat("x", 65537), `{"ok":false}`, `{"ok":true,"secret":"omit"}`} {
		t.Run(body[:8], func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, err := io.WriteString(w, body)
				if err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			plan := fixturePlan(t, server.URL)
			op := plan.spec.Context["issue-details"]
			op.ResponseChecks = map[string]any{"/ok": true}
			op.Project = map[string]string{"ok": "/ok"}
			plan.spec.Context["issue-details"] = op
			result, err := plan.Execute(t.Context(), testRuntime(server.Client(), server.URL, "context"), "context", "issue-details", map[string]any{"issue": "ISSUE-1"})
			if body == `{"ok":true,"secret":"omit"}` {
				if err != nil || string(result.Data) != `{"ok":true}` {
					t.Fatalf("projection failed: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("unsafe or failed response accepted")
			}
		})
	}
}

func TestRemoteMCPAndGatewayAdapter(t *testing.T) {
	upstream := mcp.NewServer(&mcp.Implementation{Name: "provider", Version: "1"}, nil)
	calls := 0
	upstream.AddTool(&mcp.Tool{Name: "get_issue", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls++
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "native MCP reply"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return upstream }, nil)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer agent-only" || r.Header.Get("X-MCP-Readonly") != "true" {
			t.Error("MCP headers missing")
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	plan := fixturePlan(t, server.URL)
	plan.spec.HTTPMCP = map[string]HTTPMCP{"native": {URL: server.URL, Credential: "reader", ReadOnly: true, Headers: map[string]string{"X-MCP-Readonly": "true"}, MaxResponseBytes: 65536,
		Tools: map[string]MCPTool{"get_issue": {Inputs: map[string]Input{"issue": {Type: "string", Required: true, MaxLength: 80}}, Resources: map[string]string{"issue": "issues"}}}}}
	plan.binding.HTTPMCP = []string{"native"}
	runtime := testRuntime(server.Client(), server.URL, "agent")
	if _, err := plan.CallMCP(t.Context(), runtime, "native", "get_issue", map[string]any{"issue": "ISSUE-1"}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"*", "write_issue"} {
		if _, err := plan.CallMCP(t.Context(), runtime, "native", tool, map[string]any{"issue": "ISSUE-1"}); err == nil {
			t.Fatal("unallowlisted MCP call accepted")
		}
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
	client := mcp.NewClient(&mcp.Implementation{Name: "gateway-fixture", Version: "1"}, nil)
	downstream, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = downstream.Close() }()
	result, err := downstream.CallTool(t.Context(), &mcp.CallToolParams{Name: "native__get_issue", Arguments: map[string]any{"issue": "ISSUE-1"}})
	if err != nil || result.IsError || calls != 2 {
		t.Fatalf("MCP adapter failed: %+v %v calls=%d", result, err, calls)
	}
}
