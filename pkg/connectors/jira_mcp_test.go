package connectors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestJiraProviderHTTPMCPProfiles(t *testing.T) {
	for _, profile := range []struct{ registration, secret, scheme string }{
		{"service-account", "JIRA_MCP_SERVICE_ACCOUNT_KEY", "Bearer "},
		{"personal", "JIRA_MCP_PERSONAL_BASIC_AUTH", "Basic "},
	} {
		t.Run(profile.registration, func(t *testing.T) {
			toolCalls := 0
			upstream := mcp.NewServer(&mcp.Implementation{Name: "jira-fixture", Version: "1"}, nil)
			for _, name := range []string{"getJiraIssue", "searchJiraIssuesUsingJql", "createJiraIssue"} {
				upstream.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					toolCalls++
					var args map[string]any
					if err := json.Unmarshal(request.Params.Arguments, &args); err != nil || args["cloudId"] != "00000000-0000-4000-8000-000000000001" {
						t.Error("MCP cloudId was not forwarded exactly")
					}
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Untrusted Jira read fixture"}}}, nil
				})
			}
			handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return upstream }, nil)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expected := profile.scheme + base64.StdEncoding.EncodeToString([]byte("fixture:"+profile.secret))
				if r.URL.Path != "/v2/mcp" || r.Header.Get("Authorization") != expected {
					t.Error("wrong MCP endpoint or profile credential")
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			plan := jiraPlan(t, server.URL, profile.registration)
			runtime := jiraRuntime(server.Client(), server.URL, "agent")
			for _, tool := range []string{"getJiraIssue", "searchJiraIssuesUsingJql"} {
				args := jiraMCPArguments(tool)
				result, err := plan.CallMCP(t.Context(), runtime, profile.registration, tool, args)
				if err != nil || result.IsError {
					t.Fatalf("read-only MCP call failed: %+v %v", result, err)
				}
			}
			if toolCalls != 2 {
				t.Fatalf("unexpected tool call count: %d", toolCalls)
			}
			checkJiraMCPAdapter(t, plan, runtime, profile.registration)
		})
	}
}

func jiraMCPArguments(tool string) map[string]any {
	args := map[string]any{"cloudId": "00000000-0000-4000-8000-000000000001"}
	if tool == "getJiraIssue" {
		args["issueIdOrKey"] = "APP-1"
	} else {
		args["jql"], args["maxResults"] = "project = APP ORDER BY created DESC", 25
	}
	return args
}

func checkJiraMCPAdapter(t *testing.T, plan *Plan, runtime Runtime, registration string) {
	t.Helper()
	adapter, err := plan.MCPServer(runtime)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := adapter.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "jira-gateway-fixture", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 2 {
		t.Fatalf("adapter must expose only two approved read tools: %+v %v", tools, err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != registration+"__getJiraIssue" && tool.Name != registration+"__searchJiraIssuesUsingJql" {
			t.Fatalf("unapproved tool exposed: %s", tool.Name)
		}
	}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: registration + "__getJiraIssue", Arguments: jiraMCPArguments("getJiraIssue")})
	if err != nil || result.IsError {
		t.Fatalf("adapter could not forward allowed call: %+v %v", result, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `\"untrusted\":true`) {
		t.Fatalf("gateway must label remote content untrusted: %s %v", encoded, err)
	}
}

func TestJiraProviderMCPRejections(t *testing.T) {
	plan := jiraPlan(t, "https://example.atlassian.net", "service-account")
	tests := []struct {
		name, tool, field string
		value             any
	}{
		{"write tool", "createJiraIssue", "", nil},
		{"wildcard", "*", "", nil},
		{"generic executor", "executeRead", "", nil},
		{"wrong cloud", "getJiraIssue", "cloudId", "other-cloud"},
		{"wrong issue", "getJiraIssue", "issueIdOrKey", "OTHER-1"},
		{"temporary issue", "getJiraIssue", "issueIdOrKey", "#aw_123456789012"},
		{"unknown argument", "getJiraIssue", "url", "https://other.atlassian.net"},
		{"cross-project JQL", "searchJiraIssuesUsingJql", "jql", "project = OTHER"},
		{"widened JQL", "searchJiraIssuesUsingJql", "jql", "project = APP OR project = OTHER"},
		{"unbounded page", "searchJiraIssuesUsingJql", "maxResults", 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := jiraMCPArguments(test.tool)
			if test.field != "" {
				args[test.field] = test.value
			}
			resolved := 0
			runtime := jiraRuntime(nil, "https://example.atlassian.net", "agent")
			runtime.ResolveSecret = func(context.Context, string, string) (string, error) {
				resolved++
				return "must-not-resolve", nil
			}
			if _, err := plan.CallMCP(t.Context(), runtime, "service-account", test.tool, args); err == nil || resolved != 0 {
				t.Fatalf("unsafe call reached credentials: %v, resolutions=%d", err, resolved)
			}
		})
	}
}

func TestJiraProviderMCPPolicyFailure(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		expected := "Bearer " + base64.StdEncoding.EncodeToString([]byte("fixture:JIRA_MCP_SERVICE_ACCOUNT_KEY"))
		if request.Header.Get("Authorization") != expected {
			t.Error("policy failure attempted an alternate credential")
		}
		t.Logf("blocked MCP protocol request %d: %s %s", requests, request.Method, request.URL.Path)
		http.Error(w, "organization disabled API-token MCP authentication", http.StatusForbidden)
	}))
	defer server.Close()
	plan := jiraPlan(t, server.URL, "service-account")
	_, err := plan.CallMCP(t.Context(), jiraRuntime(server.Client(), server.URL, "agent"), "service-account", "getJiraIssue", jiraMCPArguments("getJiraIssue"))
	if err == nil || !strings.Contains(err.Error(), "no credential fallback") || requests < 1 || requests > 2 {
		t.Fatalf("policy failure must terminate bounded protocol initialization without credential fallback: %v requests=%d", err, requests)
	}
}

func TestJiraProviderMCPDefinitionRejections(t *testing.T) {
	tests := map[string]func(*HTTPMCP){
		"write phase credential": func(server *HTTPMCP) {
			server.Credential = "rest-writer"
		},
		"missing read only": func(server *HTTPMCP) {
			server.ReadOnly = false
		},
		"wildcard tools": func(server *HTTPMCP) {
			server.Tools["*"] = server.Tools["getJiraIssue"]
		},
		"URL credential": func(server *HTTPMCP) {
			server.URL = "https://mcp.atlassian.com/v2/mcp?token=forbidden"
		},
		"override Authorization header": func(server *HTTPMCP) {
			server.Headers = map[string]string{"Authorization": "must-not-override"}
		},
		"unbounded response": func(server *HTTPMCP) {
			server.MaxResponseBytes = 0
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			spec := loadJiraProvider(t)
			server := spec.HTTPMCP["service-account"]
			change(&server)
			spec.HTTPMCP["service-account"] = server
			if err := spec.Validate(); err == nil {
				t.Fatal("unsafe MCP registration accepted")
			}
		})
	}
}
