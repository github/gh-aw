package connectors

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const azureDevOpsFixture = "testdata/providers/azure-devops/"

func loadAzureDevOpsSpec(t *testing.T) *ConnectorSpec {
	t.Helper()
	spec, err := LoadSpec(azureDevOpsFixture + "spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestAzureDevOpsProviderDefinition(t *testing.T) {
	spec := loadAzureDevOpsSpec(t)
	external, err := LoadSpec("../../.github/aw/connectors/azure-devops.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec, external) {
		t.Fatal("provider fixture drifted from the external definition")
	}
	for _, name := range []string{"created", "updated", "deleted", "restored"} {
		trigger := spec.Triggers["work-item-"+name]
		if !reflect.DeepEqual(trigger.NativeEvents, []string{"workitem." + name}) {
			t.Errorf("%s must register its native event", name)
		}
		if trigger.VerificationProfile != "dev.azure.com/configured-https-auth" {
			t.Error("Azure service hooks require configured HTTPS authentication, not an invented HMAC signature")
		}
		if trigger.Match["/resourceContainers/collection/id"] == "" || trigger.Match["/resourceContainers/project/id"] == "" {
			t.Errorf("%s must pin both source identities", name)
		}
		subject := "/resource/id"
		if name == "updated" {
			subject = "/resource/workItemId"
		}
		if trigger.Normalize["subject"] != subject {
			t.Errorf("%s must identify the work item, not an update or revision", name)
		}
	}
	comment := spec.SafeOutputs["comment-on-work-item"]
	if comment.Request.Query["api-version"].Literal != "7.1-preview.4" || comment.Request.Query["format"].Literal != "Markdown" || comment.Request.Body["text"].Input != "text" {
		t.Fatal("comment must use the documented Markdown JSON endpoint")
	}
	for _, op := range []Operation{comment, spec.Context["work-item-details"]} {
		if op.MaxResponseBytes != 65536 || op.Resources["organization"] != "organizations" || op.Resources["project"] != "projects" || op.Resources["work-item"] != "work-items" {
			t.Fatal("REST operations must have explicit organization, project, item and response bounds")
		}
	}
	server := spec.HTTPMCP["azure-devops"]
	if server.URL != "https://mcp.dev.azure.com/fabrikam" || !server.ReadOnly || server.Headers["X-MCP-Readonly"] != "true" || server.Headers["X-MCP-Tools"] != "core_list_projects" || len(server.Tools) != 1 || len(server.Tools["core_list_projects"].Inputs) != 0 {
		t.Fatal("remote MCP must expose only the verified no-argument project listing")
	}
}

func TestAzureDevOpsProviderRejectsInvalidConfiguration(t *testing.T) {
	tests := map[string]func(*ConnectorSpec){
		"insecure-origin": func(s *ConnectorSpec) { s.Endpoints["api"] = "http://dev.azure.com" },
		"credential-in-url": func(s *ConnectorSpec) {
			s.Endpoints["api"] = "https://token@dev.azure.com"
		},
		"dynamic-origin": func(s *ConnectorSpec) { s.Endpoints["api"] = "https://dev.azure.com/{organization}" },
		"wrong-writer-phase": func(s *ConnectorSpec) {
			c := s.Credentials["writer"]
			c.Phase = "agent"
			s.Credentials["writer"] = c
		},
		"context-writes": func(s *ConnectorSpec) {
			op := s.Context["work-item-details"]
			op.Request.Method = "PATCH"
			s.Context["work-item-details"] = op
		},
		"unbounded-response": func(s *ConnectorSpec) {
			op := s.Context["work-item-details"]
			op.MaxResponseBytes = 0
			s.Context["work-item-details"] = op
		},
		"unbounded-comment": func(s *ConnectorSpec) {
			op := s.SafeOutputs["comment-on-work-item"]
			text := op.Inputs["text"]
			text.MaxLength = 0
			op.Inputs["text"] = text
		},
		"unknown-body-input": func(s *ConnectorSpec) {
			op := s.SafeOutputs["comment-on-work-item"]
			op.Request.Body["text"] = Value{Input: "undeclared"}
		},
		"invalid-patch-operation": func(s *ConnectorSpec) {
			op := s.SafeOutputs["create-work-item"]
			op.Request.Patch[0].Op = "remove"
		},
		"dynamic-patch-path": func(s *ConnectorSpec) {
			op := s.SafeOutputs["update-work-item"]
			op.Request.Patch[0].Path = "${path}"
		},
		"mixed-patch-body": func(s *ConnectorSpec) {
			op := s.SafeOutputs["create-work-item"]
			op.Request.Body = map[string]Value{"arbitrary": {Literal: "value"}}
			s.SafeOutputs["create-work-item"] = op
		},
		"wildcard-mcp-tool": func(s *ConnectorSpec) {
			server := s.HTTPMCP["azure-devops"]
			server.Tools["*"] = MCPTool{}
		},
		"writable-remote-mcp": func(s *ConnectorSpec) {
			server := s.HTTPMCP["azure-devops"]
			server.ReadOnly = false
			s.HTTPMCP["azure-devops"] = server
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			spec := loadAzureDevOpsSpec(t)
			mutate(spec)
			if err := spec.Validate(); err == nil {
				t.Fatal("invalid provider configuration was accepted")
			}
		})
	}
}

func TestAzureDevOpsProviderBindingFixtures(t *testing.T) {
	var connection Connection
	var binding Binding
	for file, target := range map[string]any{"connection.yaml": &connection, "binding.yaml": &binding} {
		data, err := os.ReadFile(azureDevOpsFixture + file)
		if err != nil {
			t.Fatal(err)
		}
		if err := Decode(data, target); err != nil {
			t.Fatal(err)
		}
	}
	spec := loadAzureDevOpsSpec(t)
	if connection.Connector != spec.ID || connection.APIVersion != APIVersion || binding.APIVersion != APIVersion {
		t.Fatal("connection and binding must reference the shared connector contract")
	}
	if len(connection.Resources["organizations"]) != 1 || len(connection.Resources["projects"]) != 1 || len(connection.Resources["work-items"]) != 2 {
		t.Fatal("fixture must restrict organization, project and work item targets")
	}
	for _, credential := range connection.Credentials {
		if !strings.HasPrefix(credential.Secret, "AZURE_DEVOPS_") {
			t.Fatal("fixture credentials must be secret names, not values")
		}
	}
}
