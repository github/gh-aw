package connectors_test

import (
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
)

// These checks establish fixture coherence, not new-grammar compiler support.
func TestTeamsWorkflowBlockFixtures(t *testing.T) {
	tests := []struct {
		file string
		keys []string
	}{
		{"legacy/workflow.yaml", []string{"definition", "connection", "tools", "context", "safe-outputs"}},
		{"legacy/workflow-context-only.yaml", []string{"definition", "connection", "context"}},
		{"legacy/workflow-ingress-unavailable.yaml", []string{"definition", "connection", "on"}},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			workflow, block := teamsWorkflowBlock(t, test.file)
			if workflow["on"] != "workflow_dispatch" || len(workflow) != 2 {
				t.Fatal("fixture must use manual root trigger and one connectors block only")
			}
			if len(block) != len(test.keys) {
				t.Fatal("fixture has unexpected or scattered capability selectors")
			}
			for key := range block {
				if !slices.Contains(test.keys, key) {
					t.Fatalf("unexpected selector %q", key)
				}
			}
			if block["definition"] != "pkg/connectors/testdata/providers/teams/spec.yaml" ||
				block["connection"] != "pkg/connectors/testdata/providers/teams/connection.yaml" {
				t.Fatal("definition and connection must use the existing repository-relative files")
			}
		})
	}
}

func teamsWorkflowBlock(t *testing.T, file string) (map[string]any, map[string]any) {
	t.Helper()
	var workflow map[string]any
	if err := connectors.Decode(teamsData(t, file), &workflow); err != nil {
		t.Fatal(err)
	}
	providers, ok := workflow["connectors"].(map[string]any)
	if !ok || len(providers) != 1 {
		t.Fatal("fixture must have exactly one connectors provider")
	}
	block, ok := providers["teams"].(map[string]any)
	if !ok {
		t.Fatal("fixture must use connectors.teams")
	}
	return workflow, block
}

func TestTeamsWorkflowDeclaredCapabilities(t *testing.T) {
	spec, err := connectors.LoadSpec(teamsFixture + "spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, block := teamsWorkflowBlock(t, "legacy/workflow.yaml")
	for _, value := range block["tools"].([]any) {
		name := value.(string)
		if _, ok := spec.MCP[name]; !ok {
			t.Fatalf("tool %q is not declared in the unchanged external v1 definition", name)
		}
	}
	for _, value := range block["safe-outputs"].([]any) {
		if _, ok := spec.SafeOutputs[value.(string)]; !ok {
			t.Fatalf("effect %q is not declared in external safeOutputs", value)
		}
	}
	_, ingress := teamsWorkflowBlock(t, "legacy/workflow-ingress-unavailable.yaml")
	for _, value := range ingress["on"].([]any) {
		if _, ok := spec.Triggers[value.(string)]; !ok {
			t.Fatalf("native trigger %q is not declared in external triggers", value)
		}
	}
}

func TestTeamsWorkflowLiteralContext(t *testing.T) {
	plan := teamsPlan(t)
	for _, file := range []string{"legacy/workflow.yaml", "legacy/workflow-context-only.yaml"} {
		t.Run(file, func(t *testing.T) {
			_, block := teamsWorkflowBlock(t, file)
			recipes := block["context"].(map[string]any)
			for recipe, value := range recipes {
				args := value.(map[string]any)
				if !reflect.DeepEqual(args, teamsReadArgs()) {
					t.Fatal("context recipe must preserve the approved literal scope")
				}
				runtime := teamsRuntime("context", func(request *http.Request) (*http.Response, error) {
					response := "message.json"
					if recipe == "channel-thread-replies" {
						response = "replies.json"
					}
					return teamsResponse(request, http.StatusOK, teamsData(t, response)), nil
				})
				if _, err := plan.Execute(t.Context(), runtime, "context", recipe, args); err != nil {
					t.Fatalf("literal recipe %q is incompatible with the external definition: %v", recipe, err)
				}
			}
		})
	}
}

func TestTeamsMarkdownWorkflowMatchesLegacySelections(t *testing.T) {
	doc, err := os.ReadFile(teamsFixture + "workflow.md")
	if err != nil {
		t.Fatal(err)
	}
	_, snippet, ok := strings.Cut(string(doc), "---\n")
	if !ok {
		t.Fatal("missing manual workflow example")
	}
	frontmatter, _, ok := strings.Cut(snippet, "\n---\n")
	if !ok {
		t.Fatal("workflow example is missing its closing frontmatter separator")
	}
	var documented map[string]any
	if err := connectors.Decode([]byte(frontmatter), &documented); err != nil {
		t.Fatal(err)
	}
	_, fixture := teamsWorkflowBlock(t, "legacy/workflow.yaml")
	if !reflect.DeepEqual(documented["imports"], []any{".github/aw/connectors/teams.md"}) {
		t.Fatal("documented workflow must import the self-contained Teams Markdown")
	}
	tools, ok := documented["tools"].(map[string]any)
	if !ok || !reflect.DeepEqual(tools["teams"], fixture["tools"]) {
		t.Fatal("documented read tools must preserve the provider fixture")
	}
	if _, configured := documented["context"]; configured {
		t.Fatal("event references must not require a separate context configuration")
	}
	effects, ok := documented["safe-outputs"].(map[string]any)
	if !ok {
		t.Fatal("documented effects must use native prefixed keys")
	}
	selected, ok := fixture["safe-outputs"].([]any)
	if !ok || len(effects) != len(selected) {
		t.Fatal("documented effects must preserve the provider fixture")
	}
	for _, value := range selected {
		name, ok := value.(string)
		if !ok {
			t.Fatal("fixture effect must be a string")
		}
		if _, present := effects["teams-"+name]; !present {
			t.Fatalf("missing prefixed Teams effect %q", name)
		}
	}
}
