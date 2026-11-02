package connectors

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func loadJiraProvider(t *testing.T) *ConnectorSpec {
	t.Helper()
	spec, err := LoadSpec("testdata/providers/jira/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestJiraProviderDefinition(t *testing.T) {
	fixture := loadJiraProvider(t)
	definition, err := LoadSpec("../../.github/aw/connectors/jira.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fixture, definition) {
		t.Fatal("provider fixture must exercise the published definition exactly")
	}
	if len(definition.SafeOutputs) != 4 || len(definition.Triggers) != 3 || len(definition.Context) != 1 || len(definition.HTTPMCP) != 2 {
		t.Fatal("Jira must declare native lifecycle events, two HTTP MCP profiles, four REST writes and bounded context")
	}
	if definition.Context["issue-details"].MaxResponseBytes != 65536 {
		t.Fatal("context must retain the documented 65536-byte ceiling")
	}
}

func TestJiraProviderBinding(t *testing.T) {
	data, err := os.ReadFile("testdata/providers/jira/binding.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var binding Binding
	if err := Decode(data, &binding); err != nil {
		t.Fatal(err)
	}
	plan, err := Lower("../..", binding)
	if err != nil || len(plan.Digest) != 64 {
		t.Fatalf("published binding must lower without credentials or network: %+v %v", plan, err)
	}
}

func TestJiraProviderNegativeDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ConnectorSpec)
		want   string
	}{
		{
			name: "plaintext endpoint",
			change: func(spec *ConnectorSpec) {
				spec.Endpoints["site"] = "http://example.atlassian.net"
			},
			want: "HTTPS origin",
		},
		{
			name: "credentials in endpoint",
			change: func(spec *ConnectorSpec) {
				spec.Endpoints["site"] = "https://user:token@example.atlassian.net"
			},
			want: "HTTPS origin",
		},
		{
			name: "agent credential for mutation",
			change: func(spec *ConnectorSpec) {
				auth := spec.Credentials["rest-writer"]
				auth.Phase = "agent"
				spec.Credentials["rest-writer"] = auth
			},
			want: "credential declared for that phase",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := loadJiraProvider(t)
			test.change(spec)
			if err := spec.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestJiraProviderNegativeOperations(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ConnectorSpec)
		want   string
	}{
		{
			name: "context mutation",
			change: func(spec *ConnectorSpec) {
				op := spec.Context["issue-details"]
				op.Request.Method = "PUT"
				spec.Context["issue-details"] = op
			},
			want: "GET or fixed GraphQL query",
		},
		{
			name: "unbounded response",
			change: func(spec *ConnectorSpec) {
				op := spec.Context["issue-details"]
				op.MaxResponseBytes = 0
				spec.Context["issue-details"] = op
			},
			want: "maxResponseBytes",
		},
		{
			name: "dynamic endpoint path",
			change: func(spec *ConnectorSpec) {
				op := spec.SafeOutputs["add-comment"]
				op.Request.Path = "/rest/api/3/issue/${{ inputs.issue }}/comment"
				spec.SafeOutputs["add-comment"] = op
			},
			want: "fixed absolute path",
		},
		{
			name: "undeclared resource input",
			change: func(spec *ConnectorSpec) {
				op := spec.SafeOutputs["add-label"]
				op.Resources["project"] = "projects"
				spec.SafeOutputs["add-label"] = op
			},
			want: "resource constraints",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := loadJiraProvider(t)
			test.change(spec)
			if err := spec.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestJiraProviderRejectsProviderScripts(t *testing.T) {
	data, err := os.ReadFile("testdata/providers/jira/invalid-script.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec ConnectorSpec
	if err := Decode(data, &spec); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected strict decoding failure, got %v", err)
	}
}
