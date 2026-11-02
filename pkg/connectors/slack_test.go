package connectors

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"
)

func loadSlackSpec(t *testing.T) *ConnectorSpec {
	t.Helper()
	spec, err := LoadSpec("testdata/providers/slack/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestSlackConnectorSpec(t *testing.T) {
	spec := loadSlackSpec(t)
	provider, err := LoadSpec("../../.github/aw/connectors/slack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec, provider) {
		t.Fatal("Slack fixture must match the shipped provider definition")
	}
	if spec.ID != "slack.com/threads" || len(spec.Triggers) != 1 || len(spec.MCP) != 1 || len(spec.SafeOutputs) != 1 || len(spec.Context) != 1 {
		t.Fatal("Slack must exercise all four shared declaration capabilities")
	}
	if spec.Endpoints["api"] != "https://slack.com" {
		t.Fatal("Slack must use the documented fixed API origin")
	}
	if len(spec.HTTPMCP) != 0 {
		t.Fatal("Slack hosted MCP must not be enabled without supported unattended OAuth")
	}
	for _, read := range []Operation{spec.MCP["read-thread"].Operation, spec.Context["thread-context"]} {
		if read.Request.Method != http.MethodGet || read.Request.Path != "/api/conversations.replies" || read.MaxResponseBytes != 65536 {
			t.Fatal("thread reads must use the bounded documented REST endpoint")
		}
		if read.Request.Query["limit"].Literal != json.Number("15") {
			t.Fatal("thread reads must request only one bounded page of 15 messages")
		}
		if read.Resources["channel"] != "channels" || read.Resources["thread"] != "threads" {
			t.Fatal("thread reads must constrain both channel and parent thread")
		}
		if read.Inputs["thread"].Pattern != `^[0-9]{10,20}\.[0-9]{6}$` {
			t.Fatal("Slack parent IDs must preserve timestamp string syntax")
		}
	}
	write := spec.SafeOutputs["reply-in-thread"]
	if write.Request.Method != http.MethodPost || write.Request.Path != "/api/chat.postMessage" || write.Resources["channel"] != "channels" || write.Resources["thread"] != "threads" {
		t.Fatal("replies must use the documented endpoint and approved channel/thread")
	}
	for _, field := range []string{"reply_broadcast", "unfurl_links", "unfurl_media", "mrkdwn"} {
		if write.Request.Body[field].Literal != false {
			t.Fatalf("%s must be fixed false", field)
		}
	}
	if !reflect.DeepEqual(spec.Credentials["writer"].Scopes, []string{"chat:write"}) {
		t.Fatal("write credentials must not request public-channel bypass or impersonation")
	}
}

func TestSlackConnectorRejectsUnsafeDefinitions(t *testing.T) {
	for name, mutate := range map[string]func(*ConnectorSpec){
		"agent-write-credential": func(spec *ConnectorSpec) {
			tool := spec.MCP["read-thread"]
			tool.Credential = "writer"
			spec.MCP["read-thread"] = tool
		},
		"write-via-read-tool": func(spec *ConnectorSpec) {
			tool := spec.MCP["read-thread"]
			tool.Request.Method = http.MethodPost
			spec.MCP["read-thread"] = tool
		},
		"unbounded-response": func(spec *ConnectorSpec) {
			op := spec.Context["thread-context"]
			op.MaxResponseBytes = 0
			spec.Context["thread-context"] = op
		},
		"numeric-thread-id": func(spec *ConnectorSpec) {
			op := spec.SafeOutputs["reply-in-thread"]
			op.Inputs["thread"] = Input{Type: "number", Required: true}
			spec.SafeOutputs["reply-in-thread"] = op
		},
		"unbounded-message": func(spec *ConnectorSpec) {
			op := spec.SafeOutputs["reply-in-thread"]
			op.Inputs["text"] = Input{Type: "string", Required: true}
			spec.SafeOutputs["reply-in-thread"] = op
		},
		"dynamic-url": func(spec *ConnectorSpec) {
			spec.Endpoints["api"] = "https://slack.com/api?token=untrusted"
		},
		"wildcard-tool": func(spec *ConnectorSpec) {
			tool := spec.MCP["read-thread"]
			tool.Tool = "*"
			spec.MCP["read-thread"] = tool
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec := loadSlackSpec(t)
			mutate(spec)
			if err := spec.Validate(); err == nil {
				t.Fatal("unsafe Slack definition must fail shared validation")
			}
		})
	}
}

func TestSlackConnectorFixtureDocuments(t *testing.T) {
	for _, fixture := range []struct {
		path   string
		target any
	}{
		{"connection.yaml", &Connection{}},
		{"binding.yaml", &Binding{}},
	} {
		data, err := os.ReadFile("testdata/providers/slack/" + fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := Decode(data, fixture.target); err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
	}
}
