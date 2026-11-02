package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func azureDevOpsPlan(t *testing.T, origin string) *Plan {
	t.Helper()
	data, err := os.ReadFile(azureDevOpsFixture + "binding.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var binding Binding
	if err := Decode(data, &binding); err != nil {
		t.Fatal(err)
	}
	plan, err := Lower("../..", binding)
	if err != nil {
		t.Fatal(err)
	}
	if origin != "" {
		spec := plan.spec
		spec.Endpoints["api"] = origin
		server := spec.HTTPMCP["azure-devops"]
		server.URL = origin + "/fabrikam"
		spec.HTTPMCP["azure-devops"] = server
		plan, err = freezePlan(spec, plan.connection, plan.binding)
		if err != nil {
			t.Fatal(err)
		}
	}
	return plan
}

func azureDevOpsRuntime(client *http.Client, origin, phase string) Runtime {
	return Runtime{
		Phase: phase, AllowedOrigins: []string{origin}, Client: client, AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			expected := map[string]string{
				"agent": "AZURE_DEVOPS_MCP_ENTRA_TOKEN", "context": "AZURE_DEVOPS_CONTEXT_TOKEN", "safe-output": "AZURE_DEVOPS_WRITE_TOKEN",
			}
			if actualPhase != phase || name != expected[phase] {
				return "", errors.New("wrong Azure credential phase")
			}
			return phase + "-fixture", nil
		},
	}
}

func azureDevOpsArgs() map[string]any {
	return map[string]any{"organization": "fabrikam", "project": "22222222-2222-4222-8222-222222222222", "work-item": "123"}
}

func TestAzureDevOpsProviderEventProjection(t *testing.T) {
	plan := azureDevOpsPlan(t, "")
	data, err := os.ReadFile(azureDevOpsFixture + "native-events.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Deliveries []struct {
			Name    string          `json:"name"`
			Payload json.RawMessage `json:"payload"`
		} `json:"deliveries"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, delivery := range fixture.Deliveries {
		t.Run(delivery.Name, func(t *testing.T) {
			result, err := plan.ProjectEvent("work-item-"+delivery.Name, "workitem."+delivery.Name, delivery.Payload)
			if err != nil || result.Operational || !result.Untrusted || result.Fields["subject"] != json.Number("123") {
				t.Fatalf("unexpected native projection: %+v %v", result, err)
			}
			for _, changed := range []string{
				strings.ReplaceAll(string(delivery.Payload), "11111111-1111-4111-8111-111111111111", "other-collection"),
				strings.ReplaceAll(string(delivery.Payload), "22222222-2222-4222-8222-222222222222", "other-project"),
				strings.ReplaceAll(string(delivery.Payload), "123", "999"),
			} {
				if _, err := plan.ProjectEvent("work-item-"+delivery.Name, "workitem."+delivery.Name, []byte(changed)); err == nil {
					t.Fatal("out-of-scope native evidence accepted")
				}
			}
		})
	}
	if !errors.Is(plan.ActivateTriggers(), ErrIngressUnavailable) {
		t.Fatal("fixture validation must never activate source ingress")
	}
}

func TestAzureDevOpsProviderRESTExecution(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(azureDevOpsRESTHandler(t, &calls))
	defer server.Close()
	plan := azureDevOpsPlan(t, server.URL)
	for _, operation := range []string{"create-work-item", "update-work-item", "assign-work-item", "link-work-items", "comment-on-work-item"} {
		t.Run(operation, func(t *testing.T) {
			args := azureDevOpsArgs()
			switch operation {
			case "create-work-item":
				delete(args, "work-item")
				args["title"] = "Create title"
			case "update-work-item":
				args["title"] = "Update title"
			case "assign-work-item":
				args["assignee"] = "reviewer@example.invalid"
			case "comment-on-work-item":
				args["text"] = "literal ${{ text }} $(not-code)"
			}
			before := calls
			request, err := plan.PrepareSafeOutput(operation, args)
			if err != nil || calls != before {
				t.Fatal("preparing a safe output must not make HTTP requests", err)
			}
			if _, err := plan.ExecuteSafeOutput(t.Context(), azureDevOpsRuntime(server.Client(), server.URL, "safe-output"), *request); err != nil {
				t.Fatal(err)
			}
		})
	}
	result, err := plan.Execute(t.Context(), azureDevOpsRuntime(server.Client(), server.URL, "context"), "context", "work-item-details", azureDevOpsArgs())
	if err != nil || !result.Untrusted || strings.Contains(string(result.Data), "private") || strings.Contains(string(result.Data), "System.TeamProject") {
		t.Fatalf("context was not bounded/projected: %+v %v", result, err)
	}
	if calls != 6 {
		t.Fatalf("expected exactly 6 requests, got %d", calls)
	}
}

func azureDevOpsRESTHandler(t *testing.T, calls *int) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if !strings.HasPrefix(r.URL.Path, "/fabrikam/22222222-2222-4222-8222-222222222222/_apis/wit/") {
			t.Errorf("unscoped path %s", r.URL.Path)
		}
		phase := "safe-output"
		if r.Method == http.MethodGet {
			phase = "context"
		}
		if r.Header.Get("Authorization") != "Bearer "+phase+"-fixture" {
			t.Error("wrong credential phase")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if strings.HasSuffix(r.URL.Path, "/comments") {
			if r.URL.Query().Get("format") != "Markdown" || r.URL.Query().Get("api-version") != "7.1-preview.4" || string(body) != `{"text":"literal ${{ text }} $(not-code)"}` {
				t.Errorf("invalid comment request: %s %s", r.URL, body)
			}
			_, _ = io.WriteString(w, `{"workItemId":123,"text":"not projected"}`)
			return
		}
		if r.URL.Query().Get("api-version") != "7.1" {
			t.Error("wrong Azure API version")
		}
		if r.Method != http.MethodGet {
			assertAzureDevOpsPatch(t, r, body)
		}
		_, _ = io.WriteString(w, `{"id":123,"fields":{"System.TeamProject":"Fabrikam","System.Title":"untrusted title","System.State":"Active"},"private":"not projected"}`)
	})
}

func assertAzureDevOpsPatch(t *testing.T, r *http.Request, body []byte) {
	t.Helper()
	if r.Header.Get("Content-Type") != "application/json-patch+json" {
		t.Error("Azure mutations require JSON Patch media type")
	}
	var patch []map[string]any
	if err := json.Unmarshal(body, &patch); err != nil || len(patch) != 1 || patch[0]["op"] != "add" {
		t.Fatalf("invalid patch %s: %v", body, err)
	}
	switch patch[0]["path"] {
	case "/fields/System.Title":
		expected := "Update title"
		if r.Method == http.MethodPost {
			expected = "Create title"
			if !strings.HasSuffix(r.URL.Path, "/workitems/$Task") {
				t.Error("create must use fixed Task type")
			}
		}
		if patch[0]["value"] != expected {
			t.Error("title was not serialized as data")
		}
	case "/fields/System.AssignedTo":
		if patch[0]["value"] != "reviewer@example.invalid" {
			t.Error("assignment escaped allowlist")
		}
	case "/relations/-":
		value, ok := patch[0]["value"].(map[string]any)
		if !ok || value["rel"] != "System.LinkTypes.Related" || value["url"] != "https://dev.azure.com/fabrikam/22222222-2222-4222-8222-222222222222/_apis/wit/workItems/124" {
			t.Error("relation escaped fixed scoped target")
		}
	default:
		t.Error("unexpected patch path")
	}
}

func TestAzureDevOpsProviderResponseFailures(t *testing.T) {
	tests := map[string]struct {
		status int
		body   string
	}{
		"denied":           {http.StatusForbidden, `{"message":"denied"}`},
		"deleted":          {http.StatusNotFound, `{"message":"deleted"}`},
		"redirect":         {http.StatusFound, `{}`},
		"oversized":        {http.StatusOK, strings.Repeat("x", 65537)},
		"missing-evidence": {http.StatusOK, `{"id":123}`},
		"wrong-project":    {http.StatusOK, `{"id":123,"fields":{"System.TeamProject":"Other","System.Title":"x","System.State":"Active"}}`},
	}
	for name, fixture := range tests {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/must-not-follow")
				w.WriteHeader(fixture.status)
				_, _ = io.WriteString(w, fixture.body)
			}))
			defer server.Close()
			plan := azureDevOpsPlan(t, server.URL)
			_, err := plan.Execute(t.Context(), azureDevOpsRuntime(server.Client(), server.URL, "context"), "context", "work-item-details", azureDevOpsArgs())
			if err == nil || calls != 1 {
				t.Fatalf("invalid response accepted or retried: %v calls=%d", err, calls)
			}
		})
	}
}

func TestAzureDevOpsProviderDeniedBeforeCredentials(t *testing.T) {
	plan := azureDevOpsPlan(t, "")
	calls := 0
	runtime := azureDevOpsRuntime(nil, "https://dev.azure.com", "safe-output")
	runtime.ResolveSecret = func(context.Context, string, string) (string, error) {
		calls++
		return "", errors.New("must not resolve")
	}
	for name, value := range map[string]any{"organization": "other", "project": "other", "work-item": "999", "extra": "arbitrary"} {
		args := azureDevOpsArgs()
		args[name] = value
		args["text"] = "comment"
		if _, err := plan.Execute(t.Context(), runtime, "safe-output", "comment-on-work-item", args); err == nil {
			t.Fatal("invalid request accepted", name)
		}
	}
	args := azureDevOpsArgs()
	args["assignee"] = "unapproved@example.invalid"
	if _, err := plan.Execute(t.Context(), runtime, "safe-output", "assign-work-item", args); err == nil {
		t.Fatal("unapproved assignee accepted")
	}
	runtime.Phase = "agent"
	if _, err := plan.Execute(t.Context(), runtime, "safe-output", "link-work-items", azureDevOpsArgs()); err == nil {
		t.Fatal("agent phase allowed mutation")
	}
	if calls != 0 {
		t.Fatal("denied requests accessed credentials")
	}
}

func TestAzureDevOpsProviderHTTPMCP(t *testing.T) {
	calls := 0
	upstream := mcp.NewServer(&mcp.Implementation{Name: "azure-fixture", Version: "1"}, nil)
	upstream.AddTool(&mcp.Tool{Name: "core_list_projects", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls++
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fixture projects"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return upstream }, nil)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fabrikam" || r.Header.Get("X-MCP-Readonly") != "true" || r.Header.Get("X-MCP-Tools") != "core_list_projects" || r.Header.Get("Authorization") != "Bearer agent-fixture" {
			t.Error("remote Azure MCP boundary is missing")
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	plan := azureDevOpsPlan(t, server.URL)
	runtime := azureDevOpsRuntime(server.Client(), server.URL, "agent")
	if _, err := plan.CallMCP(t.Context(), runtime, "azure-devops", "core_list_projects", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"*", "wit_work_item_write", "wit_work_item"} {
		if _, err := plan.CallMCP(t.Context(), runtime, "azure-devops", tool, map[string]any{}); err == nil {
			t.Fatal("undeclared remote tool accepted", tool)
		}
	}
	if _, err := plan.CallMCP(t.Context(), runtime, "azure-devops", "core_list_projects", map[string]any{"action": "create"}); err == nil {
		t.Fatal("undeclared argument accepted")
	}
	if calls != 1 {
		t.Fatal("denied calls reached upstream")
	}
}
