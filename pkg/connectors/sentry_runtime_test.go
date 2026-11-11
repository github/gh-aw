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

func sentryPayload(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/providers/sentry/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sentryPlan(t *testing.T, origin string) *Plan {
	t.Helper()
	spec, err := LoadSpec("testdata/providers/sentry/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/providers/sentry/connection.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var connection Connection
	if err := Decode(data, &connection); err != nil {
		t.Fatal(err)
	}
	spec.Endpoints["api"] = origin
	hosted := spec.HTTPMCP["sentry"]
	hosted.URL = origin + "/mcp/example-org/example-project"
	spec.HTTPMCP["sentry"] = hosted
	binding := Binding{
		APIVersion: APIVersion, Kind: "ConnectorBinding", Definition: "spec.yaml", Connection: "connection.yaml",
		Triggers: []string{"error-created", "error-regressed", "issue-alert-triggered"},
		MCP:      []string{"get-issue"}, HTTPMCP: []string{"sentry"},
		SafeOutputs: []string{"resolve-issue"}, Context: []string{"issue-event"},
	}
	return writePlan(t, *spec, connection, binding)
}

func sentryRuntime(client *http.Client, origin, phase, secret string) Runtime {
	return Runtime{
		Phase: phase, AllowedOrigins: []string{origin}, Client: client, AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			if actualPhase != phase || name != secret {
				return "", errors.New("Sentry credential crossed a phase boundary")
			}
			return "synthetic-token", nil
		},
	}
}

func TestSentryTriggerProjection(t *testing.T) {
	plan := sentryPlan(t, "https://sentry.io")
	for _, fixture := range []struct{ file, trigger, native string }{
		{"issue-created", "error-created", "issue.created"},
		{"issue-regressed", "error-regressed", "issue.unresolved"},
		{"alert-triggered", "issue-alert-triggered", "event_alert.triggered"},
	} {
		t.Run(fixture.file, func(t *testing.T) {
			projection, err := plan.ProjectEvent(fixture.trigger, fixture.native, sentryPayload(t, fixture.file))
			if err != nil || projection.Operational || !projection.Untrusted || projection.Fields["subject"] != "1234567890" {
				t.Fatalf("unexpected Sentry projection: %+v %v", projection, err)
			}
			data, err := json.Marshal(projection)
			if err != nil || strings.Contains(string(data), "synthetic-private") || strings.Contains(string(data), "untrusted.invalid") {
				t.Fatalf("projection leaked native payload fields: %s %v", data, err)
			}
		})
	}
	if !errors.Is(plan.ActivateTriggers(), ErrIngressUnavailable) {
		t.Fatal("fixture projection must not activate a Sentry receiver")
	}
}

func TestSentryTriggerRejections(t *testing.T) {
	plan := sentryPlan(t, "https://sentry.io")
	for _, fixture := range []struct{ file, trigger, native, from, to string }{
		{"issue-created", "error-created", "issue.created", `"error"`, `"outage"`},
		{"issue-created", "error-created", "event_alert.triggered", "", ""},
		{"issue-regressed", "error-regressed", "issue.unresolved", `"regressed"`, `"ongoing"`},
		{"issue-created", "error-created", "issue.created", "4500000000000001", "4500000000000002"},
		{"issue-created", "error-created", "issue.created", "11111111-2222-4333-8444-555555555555", "other-installation"},
		{"issue-created", "error-created", "issue.created", "1234567890", "9999999999"},
		{"alert-triggered", "issue-alert-triggered", "event_alert.triggered", "4500000000000001", "4500000000000002"},
	} {
		t.Run(fixture.trigger+"/"+fixture.to, func(t *testing.T) {
			data := sentryPayload(t, fixture.file)
			if fixture.from != "" {
				data = []byte(strings.ReplaceAll(string(data), fixture.from, fixture.to))
			}
			if _, err := plan.ProjectEvent(fixture.trigger, fixture.native, data); err == nil {
				t.Fatal("Sentry accepted unrelated or unauthorized trigger evidence")
			}
		})
	}
	if _, err := plan.ProjectEvent("error-created", "issue.created", []byte(strings.Repeat("x", MaxDocumentBytes+1))); err == nil {
		t.Fatal("oversized Sentry trigger was accepted")
	}
}

func sentryAPIHandler(t *testing.T, calls *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Error("Sentry request lacked its phase-specific credential")
		}
		issuePath := "/api/0/organizations/example-org/issues/1234567890/"
		body := sentryPayload(t, "issue-details")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == issuePath:
		case r.Method == http.MethodGet && r.URL.Path == issuePath+"events/0123456789abcdef0123456789abcdef/":
			body = sentryPayload(t, "issue-event")
		case r.Method == http.MethodPut && r.URL.Path == issuePath:
			data, err := io.ReadAll(r.Body)
			if err != nil || string(data) != `{"status":"resolved"}` || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("unexpected Sentry status request %s: %v", data, err)
			}
			body = []byte(strings.Replace(string(body), `"status": "unresolved"`, `"status": "resolved"`, 1))
		default:
			t.Errorf("unexpected Sentry request %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(body); err != nil {
			t.Error(err)
		}
	}
}

func TestSentryRuntimeCapabilities(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(sentryAPIHandler(t, &calls))
	defer server.Close()
	plan := sentryPlan(t, server.URL)
	args := map[string]any{"organization": "example-org", "issue": "1234567890"}
	for _, operation := range []struct{ capability, name, phase, secret, want string }{
		{"mcp", "get-issue", "agent", "SENTRY_READ_TOKEN", `{"issue":"1234567890","project":"4500000000000001","status":"unresolved"}`},
		{"context", "issue-event", "context", "SENTRY_CONTEXT_TOKEN", `{"event":"0123456789abcdef0123456789abcdef","issue":"1234567890","type":"error"}`},
	} {
		input := map[string]any{"organization": args["organization"], "issue": args["issue"]}
		if operation.capability == "context" {
			input["event"] = "0123456789abcdef0123456789abcdef"
		}
		result, err := plan.Execute(t.Context(), sentryRuntime(server.Client(), server.URL, operation.phase, operation.secret), operation.capability, operation.name, input)
		if err != nil || !result.Untrusted || string(result.Data) != operation.want {
			t.Fatalf("%s: unexpected result %+v %v", operation.capability, result, err)
		}
	}
	staged, err := plan.PrepareSafeOutput("resolve-issue", args)
	if err != nil || calls != 2 {
		t.Fatalf("staging must not write: calls=%d %v", calls, err)
	}
	result, err := plan.ExecuteSafeOutput(t.Context(), sentryRuntime(server.Client(), server.URL, "safe-output", "SENTRY_STATUS_TOKEN"), *staged)
	if err != nil || string(result.Data) != `{"issue":"1234567890","project":"4500000000000001","status":"resolved"}` || calls != 3 {
		t.Fatalf("unexpected resolution %+v %v calls=%d", result, err, calls)
	}
}

func TestSentryResponseRejections(t *testing.T) {
	unresolved := string(sentryPayload(t, "issue-details"))
	valid := strings.Replace(unresolved, `"status": "unresolved"`, `"status": "resolved"`, 1)
	for _, fixture := range []struct {
		name, body string
		status     int
	}{
		{"unauthorized", valid, 401}, {"forbidden", valid, 403}, {"throttled", valid, 429},
		{"server-error", valid, 500}, {"invalid-json", "{", 200}, {"application-error", `{"detail":"failure"}`, 200},
		{"wrong-project", strings.Replace(valid, "4500000000000001", "4500000000000002", 1), 200},
		{"wrong-issue", strings.Replace(valid, "1234567890", "9999999999", 1), 200},
		{"status-not-applied", unresolved, 200}, {"empty", "", 200},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("X-Sentry-Rate-Limit-Remaining", "0")
				w.WriteHeader(fixture.status)
				if _, err := io.WriteString(w, fixture.body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			plan := sentryPlan(t, server.URL)
			_, err := plan.Execute(t.Context(), sentryRuntime(server.Client(), server.URL, "safe-output", "SENTRY_STATUS_TOKEN"),
				"safe-output", "resolve-issue", map[string]any{"organization": "example-org", "issue": "1234567890"})
			if err == nil || calls != 1 {
				t.Fatalf("invalid resolution must fail without retries: %v calls=%d", err, calls)
			}
		})
	}
}

func TestSentryResponseByteBoundary(t *testing.T) {
	base := strings.TrimSpace(string(sentryPayload(t, "issue-details")))
	base = strings.TrimSuffix(base, "}") + `,"padding":""}`
	for _, size := range []int{65536, 65537} {
		body := strings.TrimSuffix(base, `"}`) + strings.Repeat("x", size-len(base)) + `"}`
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := io.WriteString(w, body); err != nil {
				t.Error(err)
			}
		}))
		plan := sentryPlan(t, server.URL)
		result, err := plan.Execute(t.Context(), sentryRuntime(server.Client(), server.URL, "agent", "SENTRY_READ_TOKEN"),
			"mcp", "get-issue", map[string]any{"organization": "example-org", "issue": "1234567890"})
		server.Close()
		if len(body) != size || (size == 65536 && (err != nil || len(result.Data) > 128)) || (size == 65537 && err == nil) {
			t.Fatalf("incorrect byte-boundary behavior size=%d actual=%d result=%+v err=%v", size, len(body), result, err)
		}
	}
}

func TestSentryRejectBeforeCredentialResolution(t *testing.T) {
	plan := sentryPlan(t, "https://sentry.io")
	resolutions := 0
	runtime := Runtime{Phase: "agent", AllowedOrigins: []string{"https://sentry.io"},
		ResolveSecret: func(context.Context, string, string) (string, error) {
			resolutions++
			return "unused", nil
		}}
	for _, args := range []map[string]any{
		{"organization": "other-org", "issue": "1234567890"},
		{"organization": "example-org", "issue": "9999999999"},
		{"organization": "example-org", "issue": "../1234567890"},
		{"organization": "example-org", "issue": "1234567890", "url": "https://untrusted.invalid"},
	} {
		if _, err := plan.Execute(t.Context(), runtime, "mcp", "get-issue", args); err == nil {
			t.Fatal("unsafe Sentry arguments accepted")
		}
	}
	if _, err := plan.Execute(t.Context(), runtime, "safe-output", "resolve-issue", map[string]any{"organization": "example-org", "issue": "1234567890"}); err == nil {
		t.Fatal("agent phase must not mutate Sentry")
	}
	if resolutions != 0 {
		t.Fatal("denied Sentry requests resolved credentials")
	}
}

func TestSentryHostedMCP(t *testing.T) {
	calls := 0
	upstream := mcp.NewServer(&mcp.Implementation{Name: "sentry-fixture", Version: "1"}, nil)
	upstream.AddTool(&mcp.Tool{Name: "get_issue_details", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls++
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "synthetic Sentry read"}}}, nil
		})
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return upstream }, nil)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/example-org/example-project" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Error("hosted Sentry scope or credential changed")
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	plan := sentryPlan(t, server.URL)
	runtime := sentryRuntime(server.Client(), server.URL, "agent", "SENTRY_MCP_OAUTH_TOKEN")
	args := map[string]any{"organizationSlug": "example-org", "issueId": "1234567890"}
	if _, err := plan.CallMCP(t.Context(), runtime, "sentry", "get_issue_details", args); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"update_issue", "analyze_issue_with_seer", "*"} {
		if _, err := plan.CallMCP(t.Context(), runtime, "sentry", tool, args); err == nil {
			t.Fatal("unapproved Sentry MCP tool accepted")
		}
	}
	args["issueUrl"] = "https://untrusted.invalid"
	if _, err := plan.CallMCP(t.Context(), runtime, "sentry", "get_issue_details", args); err == nil || calls != 1 {
		t.Fatalf("event URL must not reach hosted MCP: %v calls=%d", err, calls)
	}
}

func TestSentryRedirectDenied(t *testing.T) {
	targetCalls := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		targetCalls++
	}))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	plan := sentryPlan(t, source.URL)
	_, err := plan.Execute(t.Context(), sentryRuntime(source.Client(), source.URL, "agent", "SENTRY_READ_TOKEN"),
		"mcp", "get-issue", map[string]any{"organization": "example-org", "issue": "1234567890"})
	if err == nil || targetCalls != 0 {
		t.Fatalf("redirect must not forward Sentry credentials: %v targetCalls=%d", err, targetCalls)
	}
}
