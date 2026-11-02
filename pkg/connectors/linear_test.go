package connectors_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
)

const linearDefinition = "testdata/providers/linear/spec.yaml"

func loadLinear(t *testing.T) *connectors.ConnectorSpec {
	t.Helper()
	spec, err := connectors.LoadSpec(linearDefinition)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestLinearConnectorDefinition(t *testing.T) {
	spec := loadLinear(t)
	if spec.ID != "linear.app/issues" || len(spec.Triggers) != 3 || len(spec.MCP) != 3 || len(spec.HTTPMCP) != 1 || len(spec.SafeOutputs) != 3 || len(spec.Context) != 1 {
		t.Fatal("Linear must exercise all four capability types")
	}
	if spec.HTTPMCP["read-only"].URL != "https://mcp.linear.app/mcp/readonly" || !spec.HTTPMCP["read-only"].ReadOnly {
		t.Fatal("native Linear MCP must use its fixed readonly endpoint")
	}
	for name, action := range map[string]string{
		"issue-created": "create",
		"issue-updated": "update",
		"issue-removed": "remove",
	} {
		trigger := spec.Triggers[name]
		if trigger.Match["/type"] != "Issue" || trigger.Match["/action"] != action || !reflect.DeepEqual(trigger.NativeEvents, []string{action}) {
			t.Fatalf("%s must match a documented Issue data-change event", name)
		}
		if trigger.Normalize["organization-id"] != "/organizationId" || trigger.Normalize["team-id"] != "/data/teamId" {
			t.Fatalf("%s must retain native organization/team evidence", name)
		}
	}
	for name, operation := range spec.MCP {
		if !strings.HasPrefix(operation.Request.GraphQL, "query ") || operation.Request.Path != "/graphql" {
			t.Fatalf("%s must use a fixed GraphQL query", name)
		}
	}
	for _, name := range []string{"resolve-team", "resolve-project"} {
		if !strings.Contains(spec.MCP[name].Request.GraphQL, "first: 1") {
			t.Fatalf("%s resolution must have an explicit result bound", name)
		}
	}
	for name, operation := range spec.SafeOutputs {
		if !strings.HasPrefix(operation.Request.GraphQL, "mutation ") || len(operation.Resources) == 0 {
			t.Fatalf("%s must use a fixed mutation and constrained target inputs", name)
		}
	}
}

func TestLinearConnectorRejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		change func(*connectors.ConnectorSpec)
	}{
		{"insecure endpoint", func(s *connectors.ConnectorSpec) { s.Endpoints["api"] = "http://api.linear.app" }},
		{"endpoint credentials", func(s *connectors.ConnectorSpec) { s.Endpoints["api"] = "https://key@api.linear.app" }},
		{"endpoint query", func(s *connectors.ConnectorSpec) { s.Endpoints["api"] = "https://api.linear.app?token=key" }},
		{"write credential in read tool", func(s *connectors.ConnectorSpec) {
			tool := s.MCP["get-issue"]
			tool.Credential = "writer"
			s.MCP["get-issue"] = tool
		}},
		{"read credential in write operation", func(s *connectors.ConnectorSpec) {
			op := s.SafeOutputs["add-comment"]
			op.Credential = "reader"
			s.SafeOutputs["add-comment"] = op
		}},
		{"mutation in read tool", func(s *connectors.ConnectorSpec) {
			tool := s.MCP["get-issue"]
			tool.Request.GraphQL = s.SafeOutputs["add-comment"].Request.GraphQL
			s.MCP["get-issue"] = tool
		}},
		{"unbounded response", func(s *connectors.ConnectorSpec) {
			op := s.Context["issue-details"]
			op.MaxResponseBytes = 0
			s.Context["issue-details"] = op
		}},
		{"unbounded title", func(s *connectors.ConnectorSpec) {
			op := s.SafeOutputs["create-issue"]
			input := op.Inputs["title"]
			input.MaxLength = 0
			op.Inputs["title"] = input
		}},
		{"optional target", func(s *connectors.ConnectorSpec) {
			op := s.SafeOutputs["update-issue"]
			input := op.Inputs["issue-id"]
			input.Required = false
			op.Inputs["issue-id"] = input
		}},
		{"unknown target binding", func(s *connectors.ConnectorSpec) {
			s.SafeOutputs["create-issue"].Resources["unknown"] = "teams"
		}},
		{"unknown variable binding", func(s *connectors.ConnectorSpec) {
			s.SafeOutputs["add-comment"].Request.Query["id"] = connectors.Value{Input: "unknown"}
		}},
		{"ambiguous variable binding", func(s *connectors.ConnectorSpec) {
			s.SafeOutputs["add-comment"].Request.Query["id"] = connectors.Value{Input: "issue-id", Literal: "ENG-999"}
		}},
		{"arbitrary request body", func(s *connectors.ConnectorSpec) {
			op := s.SafeOutputs["add-comment"]
			op.Request.Body = map[string]connectors.Value{"query": {Input: "body"}}
			s.SafeOutputs["add-comment"] = op
		}},
		{"invalid projection pointer", func(s *connectors.ConnectorSpec) {
			s.Triggers["issue-created"].Normalize["issue-id"] = "data.id"
		}},
		{"invalid input pattern", func(s *connectors.ConnectorSpec) {
			op := s.SafeOutputs["add-comment"]
			input := op.Inputs["body"]
			input.Pattern = "["
			op.Inputs["body"] = input
		}},
		{"native MCP is not read-only", func(s *connectors.ConnectorSpec) {
			server := s.HTTPMCP["read-only"]
			server.ReadOnly = false
			s.HTTPMCP["read-only"] = server
		}},
		{"write credential in native MCP", func(s *connectors.ConnectorSpec) {
			server := s.HTTPMCP["read-only"]
			server.Credential = "writer"
			s.HTTPMCP["read-only"] = server
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := loadLinear(t)
			test.change(spec)
			if err := spec.Validate(); err == nil {
				t.Fatal("invalid provider definition was accepted")
			}
		})
	}
}

func TestLinearConnectorRejectsWildcardConnection(t *testing.T) {
	binding := connectors.Binding{
		APIVersion: connectors.APIVersion, Kind: "ConnectorBinding",
		Definition: "pkg/connectors/testdata/providers/linear/spec.yaml",
		Connection: "pkg/connectors/testdata/providers/linear/connection-invalid.yaml",
		MCP:        []string{"get-issue"},
	}
	if _, err := connectors.Lower("../..", binding); err == nil || !strings.Contains(err.Error(), "exact allowlist") {
		t.Fatal("negative fixture must reject wildcard issue scope", err)
	}
}

func TestLinearConnectorConnectionFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/providers/linear/connection.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var connection connectors.Connection
	if err := connectors.Decode(data, &connection); err != nil {
		t.Fatal(err)
	}
	if connection.Connector != loadLinear(t).ID {
		t.Fatal("connection must select the Linear provider")
	}
	secrets := map[string]bool{}
	for _, credential := range connection.Credentials {
		if secrets[credential.Secret] || credential.Secret == "" {
			t.Fatal("read, context and write secrets must be distinct")
		}
		secrets[credential.Secret] = true
	}
	for _, resource := range []string{"organizations", "teams", "projects", "issues", "team-keys", "project-slugs"} {
		if len(connection.Resources[resource]) != 1 {
			t.Fatalf("fixture requires an exact %s scope", resource)
		}
	}
}
