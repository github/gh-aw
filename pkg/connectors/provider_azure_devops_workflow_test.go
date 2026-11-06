package connectors

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func azureDevOpsWorkflowFrontmatter(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(azureDevOpsFixture + name)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(data), "---", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[0]) != "" {
		t.Fatal("workflow fixture must contain YAML frontmatter")
	}
	var document map[string]any
	if err := Decode([]byte(parts[1]), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestAzureDevOpsProviderUnifiedWorkflowFixture(t *testing.T) {
	document := azureDevOpsWorkflowFrontmatter(t, "legacy/workflow.md")
	if document["on"] != "workflow_dispatch" {
		t.Fatal("runnable fixture must use ordinary manual dispatch, not connector ingress")
	}
	for _, root := range []string{"tools", "safe-outputs", "context"} {
		if _, exists := document[root]; exists {
			t.Fatalf("%s must be selected inside the connector entry", root)
		}
	}
	connectors, ok := document["connectors"].(map[string]any)
	if !ok || len(connectors) != 1 {
		t.Fatal("fixture must declare one unified connector entry")
	}
	entry, ok := connectors["azure-devops"].(map[string]any)
	if !ok || len(entry) != 5 {
		t.Fatal("fixture requires definition, connection, tools, context and safe-outputs")
	}
	if entry["definition"] != "pkg/connectors/testdata/providers/azure-devops/spec.yaml" || entry["connection"] != "pkg/connectors/testdata/providers/azure-devops/connection.yaml" {
		t.Fatal("workflow must reference repository-relative external v1 documents")
	}
	if _, exists := entry["on"]; exists {
		t.Fatal("omitted connector ingress must stay disabled")
	}
	if !reflect.DeepEqual(entry["tools"], []any{"azure-devops"}) || !reflect.DeepEqual(entry["safe-outputs"], []any{"comment-on-work-item"}) {
		t.Fatal("workflow must select the exact read registration and comment effect")
	}
	context, ok := entry["context"].(map[string]any)
	if !ok || len(context) != 1 || !reflect.DeepEqual(context["work-item-details"], azureDevOpsArgs()) {
		t.Fatal("workflow context requires literal, scoped recipe inputs")
	}
}

func TestAzureDevOpsProviderIngressOnlyWorkflowFixture(t *testing.T) {
	document := azureDevOpsWorkflowFrontmatter(t, "legacy/ingress-unavailable.md")
	connectors, ok := document["connectors"].(map[string]any)
	if !ok || len(document) != 1 {
		t.Fatal("negative fixture must select connector ingress only")
	}
	entry, ok := connectors["azure-devops"].(map[string]any)
	if !ok || len(entry) != 3 || !reflect.DeepEqual(entry["on"], []any{"work-item-created"}) {
		t.Fatal("ingress-only selection must not implicitly enable tools, context or effects")
	}
}
