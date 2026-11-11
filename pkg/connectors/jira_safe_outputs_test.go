package connectors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func jiraWriteArguments(name string) map[string]any {
	switch name {
	case "create-issue":
		return map[string]any{"project": "APP", "issue-type": "Task", "summary": "Conformance <&>", "description": "First <&>\r\nSecond ${{ data }}"}
	case "update-issue":
		return map[string]any{"issue": "APP-1", "summary": "Updated", "description": "New description"}
	case "add-comment":
		return map[string]any{"issue": "APP-1", "text": "Comment <&>\n$(not-code)"}
	default:
		return map[string]any{"issue": "APP-1", "label": "connector-conformance"}
	}
}

func checkJiraWriteBody(t *testing.T, name string, body map[string]any) {
	t.Helper()
	args := jiraWriteArguments(name)
	switch name {
	case "create-issue", "update-issue":
		fields, ok := body["fields"].(map[string]any)
		if !ok || fields["summary"] != args["summary"] {
			t.Fatalf("invalid issue fields: %#v", body)
		}
		if name == "create-issue" && (!reflect.DeepEqual(fields["project"], map[string]any{"key": "APP"}) || !reflect.DeepEqual(fields["issuetype"], map[string]any{"name": "Task"})) {
			t.Fatalf("create lost project/type constraints: %#v", fields)
		}
		checkJiraADF(t, fields["description"], args["description"].(string))
	case "add-comment":
		checkJiraADF(t, body["body"], args["text"].(string))
	case "add-label":
		want := map[string]any{"update": map[string]any{"labels": []any{map[string]any{"add": "connector-conformance"}}}}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("label operation must be additive, got %#v", body)
		}
	}
}

func checkJiraADF(t *testing.T, value any, text string) {
	t.Helper()
	doc, ok := value.(map[string]any)
	if !ok || doc["type"] != "doc" || doc["version"] != float64(1) {
		t.Fatalf("expected ADF v1, got %#v", value)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		quoted, err := json.Marshal(line)
		if err != nil || !strings.Contains(string(encoded), `"text":`+string(quoted)) {
			t.Fatalf("ADF did not preserve literal text %q: %s (%v)", line, encoded, err)
		}
	}
}

func TestJiraProviderSafeOutputRequests(t *testing.T) {
	tests := []struct {
		name, method, path, response string
		status                       int
	}{
		{"create-issue", "POST", "/rest/api/3/issue", `{"id":"10002","key":"APP-2","self":"https://example.atlassian.net/rest/api/3/issue/10002"}`, 201},
		{"update-issue", "PUT", "/rest/api/3/issue/APP-1", "", 204},
		{"add-comment", "POST", "/rest/api/3/issue/APP-1/comment", `{"id":"20001"}`, 201},
		{"add-label", "PUT", "/rest/api/3/issue/APP-1", "", 204},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != test.method || r.URL.Path != test.path || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected Jira write: %s %s", r.Method, r.URL)
				}
				expected := base64.StdEncoding.EncodeToString([]byte("fixture:JIRA_REST_BASIC_AUTH"))
				if r.Header.Get("Authorization") != "Basic "+expected {
					t.Error("write used MCP/context credentials")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				checkJiraWriteBody(t, test.name, body)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()
			plan := jiraPlan(t, server.URL, "service-account")
			staged, err := plan.PrepareSafeOutput(test.name, jiraWriteArguments(test.name))
			if err != nil || calls != 0 {
				t.Fatalf("preparing a write sent traffic: %v calls=%d", err, calls)
			}
			result, err := plan.ExecuteSafeOutput(t.Context(), jiraRuntime(server.Client(), server.URL, "safe-output"), *staged)
			if err != nil || result.StatusCode != test.status || !result.Untrusted || calls != 1 {
				t.Fatalf("write failed: %+v %v calls=%d", result, err, calls)
			}
		})
	}
}

func TestJiraProviderRejectsUnsafeWrites(t *testing.T) {
	plan := jiraPlan(t, "https://example.atlassian.net", "service-account")
	tests := []struct {
		name, operation, input string
		value                  any
	}{
		{"cross project", "create-issue", "project", "OTHER"},
		{"unknown issue type", "create-issue", "issue-type", "Unapproved"},
		{"oversized summary", "create-issue", "summary", strings.Repeat("x", 256)},
		{"blank summary", "create-issue", "summary", " \n\t"},
		{"oversized comment", "add-comment", "text", strings.Repeat("x", 32768)},
		{"non-string comment", "add-comment", "text", 7},
		{"label whitespace", "add-label", "label", "two words"},
		{"blank label", "add-label", "label", ""},
		{"cross issue", "add-label", "issue", "OTHER-1"},
		{"unresolved temporary ID", "add-comment", "issue", "#aw_123456789012"},
		{"path traversal", "update-issue", "issue", "../APP-1"},
		{"agent-selected URL", "add-comment", "url", "https://other.atlassian.net"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := jiraWriteArguments(test.operation)
			args[test.input] = test.value
			if _, err := plan.PrepareSafeOutput(test.operation, args); err == nil {
				t.Fatal("unsafe or undeclared write accepted")
			}
		})
	}
	calls := 0
	runtime := jiraRuntime(nil, "https://example.atlassian.net", "agent")
	runtime.ResolveSecret = func(context.Context, string, string) (string, error) {
		calls++
		return "must-not-resolve", nil
	}
	if _, err := plan.Execute(t.Context(), runtime, "safe-output", "add-label", jiraWriteArguments("add-label")); err == nil || calls != 0 {
		t.Fatal("agent phase could reach write credentials")
	}
	runtime.Phase, runtime.AllowWrites = "safe-output", false
	if _, err := plan.Execute(t.Context(), runtime, "safe-output", "add-label", jiraWriteArguments("add-label")); err == nil || calls != 0 {
		t.Fatal("write authorization was not enforced before credentials")
	}
}

func TestJiraProviderWriteErrors(t *testing.T) {
	for _, status := range []int{400, 401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"errorMessages":["secret provider diagnostic"]}`)
			}))
			defer server.Close()
			plan := jiraPlan(t, server.URL, "service-account")
			_, err := plan.Execute(t.Context(), jiraRuntime(server.Client(), server.URL, "safe-output"), "safe-output", "add-label", jiraWriteArguments("add-label"))
			if err == nil || strings.Contains(err.Error(), "secret provider diagnostic") || calls != 1 {
				t.Fatalf("write failure must be explicit, redacted, and not retried: %v calls=%d", err, calls)
			}
		})
	}
}

func TestJiraProviderWriteBoundaries(t *testing.T) {
	plan := jiraPlan(t, "https://example.atlassian.net", "service-account")
	for _, test := range []struct {
		operation, input string
		max              int
	}{{"create-issue", "summary", 255}, {"create-issue", "description", 32767}, {"add-comment", "text", 32767}, {"add-label", "label", 255}} {
		t.Run(test.operation+"/"+test.input, func(t *testing.T) {
			args := jiraWriteArguments(test.operation)
			args[test.input] = strings.Repeat("x", test.max)
			if _, err := plan.PrepareSafeOutput(test.operation, args); err != nil {
				t.Fatalf("exact declared input limit must be accepted: %v", err)
			}
			args[test.input] = strings.Repeat("x", test.max+1)
			if _, err := plan.PrepareSafeOutput(test.operation, args); err == nil {
				t.Fatal("one character over the declared limit was accepted")
			}
		})
	}
}

func TestJiraProviderIncompleteWriteResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	plan := jiraPlan(t, server.URL, "service-account")
	for _, name := range []string{"create-issue", "add-comment"} {
		if _, err := plan.Execute(t.Context(), jiraRuntime(server.Client(), server.URL, "safe-output"), "safe-output", name, jiraWriteArguments(name)); err == nil {
			t.Fatalf("%s accepted a success status without required evidence", name)
		}
	}
}
