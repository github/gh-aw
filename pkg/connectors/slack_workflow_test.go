package connectors

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// These fixture-shape checks are not compiler acceptance tests.
func TestSlackWorkflowServiceFixtures(t *testing.T) {
	for _, name := range []string{"legacy/workflow.md", "legacy/workflow-trigger-rejected.md"} {
		t.Run(name, func(t *testing.T) {
			workflow := slackWorkflowFrontmatter(t, name)
			if workflow["on"] != "workflow_dispatch" {
				t.Fatal("Slack examples must use explicit manual root activation")
			}
			for _, key := range []string{"tools", "safe-outputs"} {
				if _, exists := workflow[key]; exists {
					t.Fatalf("connector selections must not be scattered under root %s", key)
				}
			}
			service := workflow["connectors"].(map[string]any)["slack"].(map[string]any)
			if service["definition"] != "pkg/connectors/testdata/providers/slack/spec.yaml" || service["connection"] != "pkg/connectors/testdata/providers/slack/connection.yaml" {
				t.Fatal("Slack workflow must refer to the real provider and connection fixtures")
			}
			if name == "legacy/workflow-trigger-rejected.md" {
				if !reflect.DeepEqual(service["on"], []any{"threaded-app-mention"}) || len(service) != 3 {
					t.Fatal("negative fixture must select only unsupported ingress")
				}
				return
			}
			if _, exists := service["on"]; exists {
				t.Fatal("manual example must not activate unsupported Slack ingress")
			}
			if !reflect.DeepEqual(service["tools"], []any{"read-thread"}) || !reflect.DeepEqual(service["safe-outputs"], []any{"reply-in-thread"}) {
				t.Fatal("manual example must select exact read tool and safe effect")
			}
			context := service["context"].(map[string]any)["thread-context"].(map[string]any)
			if context["channel"] != "C123ABC456" || context["thread"] != slackThread {
				t.Fatal("context recipe requires literal approved channel and string parent")
			}
		})
	}
}

func slackWorkflowFrontmatter(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/providers/slack/" + name)
	if err != nil {
		t.Fatal(err)
	}
	sections := strings.SplitN(string(data), "---", 3)
	if len(sections) != 3 || sections[0] != "" {
		t.Fatal("workflow fixture must have YAML frontmatter")
	}
	var document map[string]any
	if err := Decode([]byte(sections[1]), &document); err != nil {
		t.Fatal(err)
	}
	return document
}
