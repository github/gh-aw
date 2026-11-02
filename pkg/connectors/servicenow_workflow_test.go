package connectors

import (
	"bytes"
	"os"
	"reflect"
	"testing"
)

func TestServiceNowWorkflowFixtureShape(t *testing.T) {
	tests := []struct {
		file         string
		capabilities []string
	}{
		{"workflow.md", []string{"tools", "context", "safe-outputs"}},
		{"workflow-tools-only.md", []string{"tools"}},
		{"workflow-context-only.md", []string{"context"}},
		{"workflow-effects-only.md", []string{"safe-outputs"}},
		{"workflow-ingress-unavailable.md", []string{"on"}},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			data, err := os.ReadFile("testdata/providers/servicenow/" + test.file)
			if err != nil {
				t.Fatal(err)
			}
			parts := bytes.SplitN(data, []byte("---\n"), 3)
			if len(parts) != 3 || len(parts[0]) != 0 || len(bytes.TrimSpace(parts[2])) == 0 {
				t.Fatal("workflow fixture must have frontmatter and a prompt")
			}
			var frontmatter map[string]any
			if err := Decode(parts[1], &frontmatter); err != nil {
				t.Fatal(err)
			}
			if frontmatter["on"] != "workflow_dispatch" {
				t.Fatal("example must use manual root activation")
			}
			for _, root := range []string{"tools", "safe-outputs"} {
				if _, exists := frontmatter[root]; exists {
					t.Fatalf("provider selection must not be scattered under root %s", root)
				}
			}
			connectors, ok := frontmatter["connectors"].(map[string]any)
			if !ok || len(connectors) != 1 {
				t.Fatal("expected only the ServiceNow connector")
			}
			selection, ok := connectors["servicenow"].(map[string]any)
			if !ok || len(selection) != len(test.capabilities)+2 {
				t.Fatal("unexpected ServiceNow selectors; omitted capabilities must stay omitted")
			}
			if selection["definition"] != ".github/aw/connectors/servicenow.yaml" || selection["connection"] != "pkg/connectors/testdata/providers/servicenow/connection.yaml" {
				t.Fatal("provider paths must remain repository-relative")
			}
			for _, name := range test.capabilities {
				if _, exists := selection[name]; !exists {
					t.Fatalf("expected selected capability %s", name)
				}
			}
			if value, ok := selection["context"]; ok {
				expected := map[string]any{"incident-metadata": map[string]any{"record": serviceNowIncidentID}}
				if test.file == "workflow-context-only.md" {
					expected = map[string]any{"requested-item-metadata": map[string]any{"record": serviceNowRequestedItemID}}
				}
				if !reflect.DeepEqual(value, expected) {
					t.Fatal("context recipes require explicit literal inputs")
				}
			}
		})
	}
}
