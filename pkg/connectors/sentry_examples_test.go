package connectors

import (
	"os"
	"reflect"
	"testing"
)

// These artifact checks do not replace compilation with the new shared compiler.
func TestSentryWorkflowSelectionArtifacts(t *testing.T) {
	for _, file := range []string{"workflow.yaml", "workflow-tools-only.yaml", "workflow-ingress-unavailable.yaml"} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile("testdata/providers/sentry/" + file)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := Decode(data, &document); err != nil {
				t.Fatal(err)
			}
			if document["on"] != "workflow_dispatch" {
				t.Fatal("root trigger must stay manual")
			}
			if _, ok := document["tools"]; ok {
				t.Fatal("provider examples must not scatter root tool selectors")
			}
			if _, ok := document["safe-outputs"]; ok {
				t.Fatal("provider effects must stay under the connector instance")
			}
			instances, ok := document["connectors"].(map[string]any)
			if !ok || len(instances) != 1 {
				t.Fatal("expected one connector instance")
			}
			selection, ok := instances["sentry"].(map[string]any)
			if !ok {
				t.Fatal("expected Sentry instance")
			}
			if selection["definition"] != ".github/aw/connectors/sentry.yaml" || selection["connection"] != "pkg/connectors/testdata/providers/sentry/connection.yaml" {
				t.Fatal("connector paths must remain repository-relative")
			}
			assertSentryArtifactSelection(t, file, selection)
		})
	}
}

func assertSentryArtifactSelection(t *testing.T, file string, selection map[string]any) {
	t.Helper()
	if file == "workflow-ingress-unavailable.yaml" {
		if !reflect.DeepEqual(selection["on"], []any{"error-created", "error-regressed"}) || len(selection) != 3 {
			t.Fatal("negative ingress example must select only native triggers")
		}
		return
	}
	if !reflect.DeepEqual(selection["tools"], []any{"get-issue"}) {
		t.Fatal("manual examples must select the declared REST read")
	}
	if file == "workflow-tools-only.yaml" {
		if len(selection) != 3 {
			t.Fatal("omitted capabilities must not gain implicit selections")
		}
		return
	}
	if !reflect.DeepEqual(selection["safe-outputs"], []any{"resolve-issue"}) {
		t.Fatal("effect selection must use the declared resolution")
	}
	want := map[string]any{"issue-event": map[string]any{
		"organization": "example-org", "issue": "1234567890", "event": "0123456789abcdef0123456789abcdef",
	}}
	if !reflect.DeepEqual(selection["context"], want) {
		t.Fatal("context recipe must use literal scoped inputs")
	}
}
