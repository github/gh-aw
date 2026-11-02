package connectors

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSentryProviderDefinition(t *testing.T) {
	fixture, err := LoadSpec("testdata/providers/sentry/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := LoadSpec("../../.github/aw/connectors/sentry.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fixture, definition) {
		t.Fatal("Sentry fixture must match the shipped provider definition")
	}
	if definition.ID != "sentry.io/issues" || len(definition.Triggers) != 3 || len(definition.MCP) != 1 || len(definition.HTTPMCP) != 1 || len(definition.SafeOutputs) != 1 || len(definition.Context) != 1 {
		t.Fatal("Sentry must exercise the four declared capability surfaces")
	}
	if definition.MCP["get-issue"].Tool != "get_issue_details" {
		t.Fatal("Sentry read tool must preserve its officially documented name")
	}
	regression := definition.Triggers["error-regressed"]
	if regression.Match["/action"] != "unresolved" || regression.Match["/data/issue/substatus"] != "regressed" || regression.Match["/data/issue/issueCategory"] != "error" {
		t.Fatal("unresolved alone is not an error regression")
	}
	alert := definition.Triggers["issue-alert-triggered"]
	if !reflect.DeepEqual(alert.NativeEvents, []string{"event_alert.triggered"}) || alert.Normalize["subject"] != "/data/event/issue_id" {
		t.Fatal("issue alerts must not be interpreted as native issue creation")
	}
	status := definition.SafeOutputs["resolve-issue"]
	if status.Request.Method != http.MethodPut || len(status.Request.Body) != 1 || status.Request.Body["status"].Literal != "resolved" {
		t.Fatal("safe output must expose only the scoped fixed resolution")
	}
	for _, op := range []Operation{definition.MCP["get-issue"].Operation, status, definition.Context["issue-event"]} {
		if op.Resources["organization"] != "organizations" || op.Resources["issue"] != "issues" || op.MaxResponseBytes != 65536 {
			t.Fatal("Sentry operations require explicit issue/org scope and response bounds")
		}
	}
}

func TestSentryProviderNegativeFixtures(t *testing.T) {
	for _, fixture := range []struct {
		path string
		want string
	}{
		{"invalid-write-in-read.yaml", "must be a GET"},
		{"invalid-unbounded-context.yaml", "maxResponseBytes"},
	} {
		t.Run(fixture.path, func(t *testing.T) {
			_, err := LoadSpec("testdata/providers/sentry/" + fixture.path)
			if err == nil || !strings.Contains(err.Error(), fixture.want) {
				t.Fatalf("expected %q rejection, got %v", fixture.want, err)
			}
		})
	}
}

func TestSentryProviderRejectsUnknownFields(t *testing.T) {
	data, err := os.ReadFile("testdata/providers/sentry/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec ConnectorSpec
	if err := Decode(append(data, []byte("\narbitraryScript: run-anything\n")...), &spec); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("provider definitions must use the shared strict parser: %v", err)
	}
}
