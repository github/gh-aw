package connectors

import (
	"bytes"
	"net/http"
	"os"
	"reflect"
	"testing"
)

func TestServiceNowDefinition(t *testing.T) {
	fixture, err := os.ReadFile("testdata/providers/servicenow/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile("../../.github/aw/connectors/servicenow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fixture, definition) {
		t.Fatal("provider fixture must match the published definition")
	}
	spec, err := LoadSpec("testdata/providers/servicenow/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if spec.ID != "servicenow.com/records" || spec.Endpoints["instance"] != "https://example.service-now.com" {
		t.Fatal("unexpected provider identity or instance origin")
	}
	if len(spec.Triggers) != 2 || len(spec.MCP) != 2 || len(spec.Context) != 2 || len(spec.SafeOutputs) != 2 {
		t.Fatal("incident and requested-item capabilities must remain explicitly scoped")
	}
	for _, name := range []string{"incident", "requested-item"} {
		t.Run(name, func(t *testing.T) {
			read := spec.MCP["get-"+name]
			context := spec.Context[name+"-metadata"]
			write := spec.SafeOutputs["append-"+name+"-work-note"]
			if read.Request.Method != http.MethodGet || context.Request.Method != http.MethodGet || write.Request.Method != http.MethodPatch {
				t.Fatal("read/context must be GET and journal append must be PATCH")
			}
			if !reflect.DeepEqual(read.Request, context.Request) || read.Credential == context.Credential {
				t.Fatal("context must use the same bounded read with a distinct credential")
			}
			if read.Request.Path != write.Request.Path || read.Resources["record"] != write.Resources["record"] {
				t.Fatal("read and write must target the same explicitly allowlisted table and record")
			}
			if read.Inputs["record"].MaxLength != 32 || write.Inputs["note"].MaxLength != 4000 {
				t.Fatal("record IDs and journal text must remain bounded")
			}
			if len(write.Request.Body) != 1 || write.Request.Body["work_notes"].Input != "note" {
				t.Fatal("only a new internal work_notes entry may be written")
			}
			if write.Request.Query["sysparm_fields"].Literal != "sys_id" {
				t.Fatal("mutation receipts must not request journal content")
			}
			if read.Request.Query["sysparm_fields"].Literal != "sys_id,number,state,sys_mod_count" {
				t.Fatal("read context must not request sensitive record content")
			}
			if read.MaxResponseBytes != 8192 || context.MaxResponseBytes != 8192 || write.MaxResponseBytes != 1024 {
				t.Fatal("response limits must remain explicit")
			}
		})
	}
}

func TestServiceNowInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ConnectorSpec)
	}{
		{"insecure-instance", func(s *ConnectorSpec) { s.Endpoints["instance"] = "http://example.service-now.com" }},
		{"instance-credentials", func(s *ConnectorSpec) { s.Endpoints["instance"] = "https://user:password@example.service-now.com" }},
		{"instance-path", func(s *ConnectorSpec) { s.Endpoints["instance"] = "https://example.service-now.com/api" }},
		{"read-using-writer", func(s *ConnectorSpec) {
			op := s.MCP["get-incident"]
			op.Credential = "writer"
			s.MCP["get-incident"] = op
		}},
		{"write-using-reader", func(s *ConnectorSpec) {
			op := s.SafeOutputs["append-incident-work-note"]
			op.Credential = "reader"
			s.SafeOutputs["append-incident-work-note"] = op
		}},
		{"unbounded-response", func(s *ConnectorSpec) {
			op := s.Context["incident-metadata"]
			op.MaxResponseBytes = 0
			s.Context["incident-metadata"] = op
		}},
		{"mutating-read-tool", func(s *ConnectorSpec) {
			op := s.MCP["get-incident"]
			op.Request.Method = "PATCH"
			s.MCP["get-incident"] = op
		}},
		{"wildcard-tool", func(s *ConnectorSpec) {
			op := s.MCP["get-incident"]
			op.Tool = "*"
			s.MCP["get-incident"] = op
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec, err := LoadSpec("testdata/providers/servicenow/spec.yaml")
			if err != nil {
				t.Fatal(err)
			}
			test.change(spec)
			if err := spec.Validate(); err == nil {
				t.Fatal("unsafe definition must fail shared validation")
			}
		})
	}
}
