package connectors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestStrictDocuments(t *testing.T) {
	tests := []string{
		"apiVersion: x\napiVersion: y\n",
		"apiVersion: x\n---\nkind: ConnectorSpec\n",
		`{"credentials":{"reader":{"phase":"agent","typo":true}}}`,
		`{"request":{"shell":"echo injected"}}`,
		strings.Repeat("x", MaxDocumentBytes+1),
	}
	for _, source := range tests {
		var spec ConnectorSpec
		if err := Decode([]byte(source), &spec); err == nil {
			t.Errorf("unsafe document accepted: %.100s", source)
		}
	}
}

func TestConnectorSecurityValidation(t *testing.T) {
	cases := map[string]func(*ConnectorSpec){
		"version":         func(s *ConnectorSpec) { s.APIVersion = "future" },
		"credentials-url": func(s *ConnectorSpec) { s.Endpoints["api"] = "https://secret@api.example.com" },
		"endpoint-query":  func(s *ConnectorSpec) { s.Endpoints["api"] = "https://api.example.com?key=secret" },
		"write-phase": func(s *ConnectorSpec) {
			op := s.SafeOutputs["add-comment"]
			op.Credential = "reader"
			s.SafeOutputs["add-comment"] = op
		},
		"read-post": func(s *ConnectorSpec) {
			op := s.Context["issue-details"]
			op.Request.Method = "POST"
			s.Context["issue-details"] = op
		},
		"hidden-mutation": func(s *ConnectorSpec) {
			op := s.Context["issue-details"]
			op.Request.Method = "POST"
			op.Request.GraphQL = "query Read { issue { id } } mutation Write { deleteIssue { id } }"
			s.Context["issue-details"] = op
		},
		"input-expression": func(s *ConnectorSpec) {
			op := s.Context["issue-details"]
			op.Request.Path = "/${{ secrets.TOKEN }}"
			s.Context["issue-details"] = op
		},
		"dynamic-patch-path": func(s *ConnectorSpec) {
			op := s.SafeOutputs["add-comment"]
			op.Request.Body = nil
			op.Request.Patch = []Patch{{Op: "add", Path: "/{input}", Value: Value{Literal: "x"}}}
			s.SafeOutputs["add-comment"] = op
		},
		"unknown-input": func(s *ConnectorSpec) {
			op := s.SafeOutputs["add-comment"]
			op.Request.Body["body"] = Value{Input: "undeclared"}
			s.SafeOutputs["add-comment"] = op
		},
		"unbounded-input": func(s *ConnectorSpec) {
			op := s.Context["issue-details"]
			op.Inputs["issue"] = Input{Type: "string", Required: true}
			s.Context["issue-details"] = op
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			spec, err := LoadSpec("testdata/example.yaml")
			if err != nil {
				t.Fatal(err)
			}
			mutate(spec)
			if err := spec.Validate(); err == nil {
				t.Fatal("unsafe connector definition accepted")
			}
		})
	}
}

func TestLowerConfinesAndFreezes(t *testing.T) {
	plan := fixturePlan(t, "https://api.example.com")
	copy := writePlan(t, plan.spec, plan.connection, plan.binding)
	if plan.Digest != copy.Digest {
		t.Fatal("identical plans must have identical digests")
	}
	binding := plan.binding
	other := t.TempDir()
	path := filepath.Join(other, "spec.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(path, filepath.Join(root, "spec.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := Lower(root, binding); err == nil {
		t.Fatal("symlink escape accepted")
	}
	for _, path := range []string{"../spec.yaml", "/tmp/spec.yaml", "https://example.com/spec.yaml"} {
		binding.Definition = path
		if _, err := Lower(root, binding); err == nil {
			t.Fatal("unconfined definition accepted", path)
		}
	}
	if _, err := plan.ExecuteSafeOutput(t.Context(), Runtime{}, SafeOutputRequest{Digest: "other"}); err == nil {
		t.Fatal("stale plan request accepted")
	}
}

func TestConnectorSchemas(t *testing.T) {
	plan := fixturePlan(t, "https://api.example.com")
	for kind, document := range map[string]any{"ConnectorSpec": plan.spec, "ConnectorConnection": plan.connection, "ConnectorBinding": plan.binding} {
		t.Run(kind, func(t *testing.T) {
			encoded, err := Schema(kind)
			if err != nil {
				t.Fatal(err)
			}
			var schema jsonschema.Schema
			if err := json.Unmarshal(encoded, &schema); err != nil {
				t.Fatal(err)
			}
			resolved, err := schema.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if err := resolved.Validate(value); err != nil {
				t.Fatal(err)
			}
			value["unknown"] = true
			if err := resolved.Validate(value); err == nil {
				t.Fatal("structural schema accepted unknown field")
			}
		})
	}
}

func TestEvidenceResourceAndMissingValues(t *testing.T) {
	plan := fixturePlan(t, "https://api.example.com")
	plan.connection.Resources["projects"] = []string{"9007199254740993"}
	trigger := plan.spec.Triggers["issue-created"]
	trigger.Resources = map[string]string{"/project": "projects"}
	plan.spec.Triggers["issue-created"] = trigger
	payload := `{"event":"issue.created","project":9007199254740993,"issue":{"id":"ISSUE-1","title":"hello"}}`
	if _, err := plan.ProjectEvent("issue-created", "issue.created", []byte(payload)); err != nil {
		t.Fatal("numeric resource ID precision lost", err)
	}
	for _, bad := range []string{strings.ReplaceAll(payload, "9007199254740993", "9007199254740992"), `{"event":"issue.created","project":9007199254740993,"issue":{"id":"ISSUE-1"}}`} {
		if _, err := plan.ProjectEvent("issue-created", "issue.created", []byte(bad)); err == nil {
			t.Fatal("invalid trigger evidence accepted")
		}
	}

}

func TestResponseTypesAndExactInputBinding(t *testing.T) {
	plan := fixturePlan(t, "https://api.example.com")
	op := plan.spec.Context["issue-details"]
	op.ResponseTypes = map[string]string{"/id": "string", "/state": "string"}
	op.ResponseInputs = map[string]string{"/id": "issue"}
	op.Project = map[string]string{"id": "/id", "state": "/state"}
	args := map[string]any{"issue": "ISSUE-1"}
	for _, response := range []string{
		`{"id":"ISSUE-2","state":"open"}`,
		`{"id":"ISSUE-1","state":{"private":"must not project"}}`,
		`{"id":"ISSUE-1"}`,
	} {
		if _, err := plan.validateResponse(op, []byte(response), args); err == nil {
			t.Fatal("invalid response evidence accepted", response)
		}
	}
	if _, err := plan.validateResponse(op, []byte(`{"id":"ISSUE-1","state":"open","private":"excluded"}`), args); err != nil {
		t.Fatal(err)
	}
	op.Resources = nil
	op.ResponseResources = map[string]string{"/project": "projects"}
	plan.spec.Context["issue-details"] = op
	if err := validateSelection(plan.spec, plan.binding, plan.connection); err == nil {
		t.Fatal("missing response resource accepted at lowering without input resource constraints")
	}
	trigger := plan.spec.Triggers["issue-created"]
	trigger.Types = map[string]string{"/issue/id": "string"}
	plan.spec.Triggers["issue-created"] = trigger
	if _, err := plan.ProjectEvent("issue-created", "issue.created", []byte(`{"event":"issue.created","issue":{"id":123,"title":"hello"}}`)); err == nil {
		t.Fatal("numeric identifier accepted where native string type is required")
	}
}
