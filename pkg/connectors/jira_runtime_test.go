package connectors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func jiraPlan(t *testing.T, origin, registration string) *Plan {
	t.Helper()
	spec := loadJiraProvider(t)
	var connection Connection
	var binding Binding
	for file, target := range map[string]any{"connection.yaml": &connection, "binding.yaml": &binding} {
		data, err := os.ReadFile("testdata/providers/jira/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if err := Decode(data, target); err != nil {
			t.Fatal(err)
		}
	}
	spec.Endpoints["site"] = origin
	for name, server := range spec.HTTPMCP {
		server.URL = origin + "/v2/mcp"
		spec.HTTPMCP[name] = server
	}
	binding.Definition, binding.Connection = "spec.yaml", "connection.yaml"
	binding.HTTPMCP = []string{registration}
	return writePlan(t, *spec, connection, binding)
}

func jiraRuntime(client *http.Client, origin, phase string) Runtime {
	return Runtime{
		Phase: phase, AllowedOrigins: []string{origin}, Client: client, AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, requestedPhase, secret string) (string, error) {
			if requestedPhase != phase {
				return "", errors.New("unexpected credential phase")
			}
			expected := map[string][]string{
				"agent":       {"JIRA_MCP_SERVICE_ACCOUNT_KEY", "JIRA_MCP_PERSONAL_BASIC_AUTH"},
				"context":     {"JIRA_CONTEXT_BASIC_AUTH"},
				"safe-output": {"JIRA_REST_BASIC_AUTH"},
			}
			if slices.Contains(expected[phase], secret) {
				return base64.StdEncoding.EncodeToString([]byte("fixture:" + secret)), nil
			}
			return "", errors.New("unexpected credential reference")
		},
	}
}

func TestJiraProviderTriggerProjection(t *testing.T) {
	plan := jiraPlan(t, "https://example.atlassian.net", "service-account")
	for _, action := range []string{"created", "updated", "deleted"} {
		native := "jira:issue_" + action
		payload := `{"webhookEvent":"` + native + `","issue":{"id":"10001","key":"APP-1","self":"https://example.atlassian.net/rest/api/3/issue/10001","fields":{"project":{"key":"APP"},"status":{"name":"Done"},"description":"not forwarded"}},"changelog":{"items":[{"fieldtype":"jira","field":"resolution","from":null,"to":"1"}]}}`
		projection, err := plan.ProjectEvent("issue-"+action, native, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"issue-id": "10001", "issue-key": "APP-1", "project": "APP", "url": "https://example.atlassian.net/rest/api/3/issue/10001"}
		if projection.Operational || !projection.Untrusted || !reflect.DeepEqual(projection.Fields, want) {
			t.Fatalf("unexpected projection: %+v", projection)
		}
	}
	if !errors.Is(plan.ActivateTriggers(), ErrIngressUnavailable) {
		t.Fatal("fixture projection must never activate ingress")
	}
}

func TestJiraProviderTriggerRejections(t *testing.T) {
	plan := jiraPlan(t, "https://example.atlassian.net", "service-account")
	payload := `{"webhookEvent":"jira:issue_updated","issue":{"id":"10001","key":"APP-1","self":"https://example.atlassian.net/rest/api/3/issue/10001","fields":{"project":{"key":"APP"}}}}`
	tests := map[string]string{
		"wrong tenant":    strings.Replace(payload, "example.atlassian.net", "other.atlassian.net", 1),
		"wrong project":   strings.Replace(payload, `"key":"APP"`, `"key":"OTHER"`, 1),
		"wrong issue":     strings.Replace(payload, `"id":"10001"`, `"id":"10002"`, 1),
		"missing key":     strings.Replace(payload, `"key":"APP-1",`, "", 1),
		"missing project": strings.Replace(payload, `"project":{"key":"APP"}`, `"project":{}`, 1),
		"wrong event":     strings.Replace(payload, "jira:issue_updated", "comment_created", 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := plan.ProjectEvent("issue-updated", "jira:issue_updated", []byte(data)); err == nil {
				t.Fatal("unsupported or cross-scope event accepted")
			}
		})
	}
	if _, err := plan.ProjectEvent("issue-updated", "jira:issue_created", []byte(payload)); err == nil {
		t.Fatal("native event selector was ignored")
	}
	if _, err := plan.ProjectEvent("issue-closed", "jira:issue_updated", []byte(payload)); err == nil {
		t.Fatal("prototype must not invent resolution normalization")
	}
}

const jiraContextResponse = `{"id":"10001","key":"APP-1","self":"https://example.atlassian.net/rest/api/3/issue/10001","fields":{"project":{"key":"APP"},"summary":"Read-only fixture","description":null,"labels":["bug"],"status":{"name":"Done"},"resolution":null},"secret":"must not project"}`

func TestJiraProviderBoundedContext(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/api/3/issue/APP-1" || r.URL.Query().Get("fields") != "summary,project,labels,status" {
			t.Errorf("unexpected context request: %s %s", r.Method, r.URL)
		}
		expected := base64.StdEncoding.EncodeToString([]byte("fixture:JIRA_CONTEXT_BASIC_AUTH"))
		if r.Header.Get("Authorization") != "Basic "+expected {
			t.Error("context did not use its independent Basic credential")
		}
		_, _ = io.WriteString(w, jiraContextResponse)
	}))
	defer server.Close()
	plan := jiraPlan(t, server.URL, "service-account")
	result, err := plan.Execute(t.Context(), jiraRuntime(server.Client(), server.URL, "context"), "context", "issue-details", map[string]any{"issue": "APP-1"})
	if err != nil || !result.Untrusted || result.StatusCode != http.StatusOK {
		t.Fatalf("context failed: %+v %v", result, err)
	}
	var data map[string]any
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data) != 7 || data["issue-key"] != "APP-1" || data["summary"] != "Read-only fixture" || strings.Contains(string(result.Data), "must not project") {
		t.Fatalf("context projection widened: %s", result.Data)
	}
}

func TestJiraProviderContextFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"oversized", 200, strings.Repeat("x", 65537)},
		{"deleted", 404, `{"errorMessages":["not found"]}`},
		{"forbidden", 403, `{"errorMessages":["denied"]}`},
		{"invalid JSON", 200, `not JSON`},
		{"wrong project", 200, strings.Replace(jiraContextResponse, `"key":"APP"`, `"key":"OTHER"`, 1)},
		{"wrong tenant", 200, strings.Replace(jiraContextResponse, "example.atlassian.net", "other.atlassian.net", 1)},
		{"missing evidence", 200, strings.Replace(jiraContextResponse, `"summary":"Read-only fixture",`, "", 1)},
		{"redirect", 302, `{}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/must-not-follow")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			plan := jiraPlan(t, server.URL, "service-account")
			if _, err := plan.Execute(t.Context(), jiraRuntime(server.Client(), server.URL, "context"), "context", "issue-details", map[string]any{"issue": "APP-1"}); err == nil {
				t.Fatal("failed or out-of-scope context accepted")
			}
			if calls != 1 {
				t.Fatalf("request was retried or redirected: %d", calls)
			}
		})
	}
}

func TestJiraProviderContextExactByteLimit(t *testing.T) {
	for _, size := range []int{65536, 65537} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			template := strings.TrimSuffix(jiraContextResponse, "}") + `,"padding":""}`
			body := strings.TrimSuffix(jiraContextResponse, "}") + `,"padding":"` + strings.Repeat("x", size-len(template)) + `"}`
			if len(body) != size {
				t.Fatal("fixture byte count is incorrect")
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			plan := jiraPlan(t, server.URL, "service-account")
			result, err := plan.Execute(t.Context(), jiraRuntime(server.Client(), server.URL, "context"), "context", "issue-details", map[string]any{"issue": "APP-1"})
			if size == 65536 && (err != nil || strings.Contains(string(result.Data), "padding")) {
				t.Fatalf("exact byte limit must succeed with a narrow projection: %+v %v", result, err)
			}
			if size == 65537 && (err == nil || !strings.Contains(err.Error(), "byte bound")) {
				t.Fatalf("one byte over the limit must fail: %v", err)
			}
		})
	}
}
