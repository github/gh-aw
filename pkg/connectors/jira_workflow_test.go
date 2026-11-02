package connectors

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func jiraWorkflowFrontmatter(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/providers/jira/" + name)
	if err != nil {
		t.Fatal(err)
	}
	frontmatter, _, ok := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
	if !ok {
		t.Fatal("workflow fixture must contain delimited frontmatter")
	}
	var document map[string]any
	if err := Decode([]byte(frontmatter), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestJiraProviderWorkflowSelections(t *testing.T) {
	document := jiraWorkflowFrontmatter(t, "workflow.md")
	if document["on"] != "workflow_dispatch" {
		t.Fatal("the runnable example must use the manual workflow trigger")
	}
	services, ok := document["connectors"].(map[string]any)
	if !ok || len(services) != 1 {
		t.Fatal("all Jira selections must be under one named service block")
	}
	want := map[string]any{
		"definition":   ".github/aw/connectors/jira.yaml",
		"connection":   "pkg/connectors/testdata/providers/jira/connection.yaml",
		"tools":        []any{"service-account"},
		"context":      map[string]any{"issue-details": map[string]any{"issue": "APP-1"}},
		"safe-outputs": []any{"add-comment"},
	}
	if !reflect.DeepEqual(services["jira"], want) {
		t.Fatalf("unexpected workflow service selections: %#v", services["jira"])
	}
	for _, root := range []string{"tools", "safe-outputs"} {
		if _, exists := document[root]; exists {
			t.Fatalf("fixture must not scatter Jira selectors under root %s", root)
		}
	}
}

func TestJiraProviderWorkflowIngressFixture(t *testing.T) {
	document := jiraWorkflowFrontmatter(t, "workflow-trigger-unavailable.md")
	if document["on"] != "workflow_dispatch" {
		t.Fatal("root workflow trigger must remain independent from connector events")
	}
	services, ok := document["connectors"].(map[string]any)
	if !ok {
		t.Fatal("missing connector service block")
	}
	want := map[string]any{
		"definition": ".github/aw/connectors/jira.yaml",
		"connection": "pkg/connectors/testdata/providers/jira/connection.yaml",
		"on":         []any{"issue-created"},
	}
	if !reflect.DeepEqual(services["jira"], want) {
		t.Fatalf("negative fixture must select only connector ingress: %#v", services["jira"])
	}
}
