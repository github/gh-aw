package connectors_test

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
)

const teamsFixture = "testdata/providers/teams/"

func TestTeamsConnectorDefinition(t *testing.T) {
	spec, err := connectors.LoadSpec(teamsFixture + "spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if spec.ID != "microsoft.com/teams" || len(spec.Triggers) != 1 || len(spec.MCP) != 2 || len(spec.Context) != 2 || len(spec.SafeOutputs) != 2 {
		t.Fatal("Teams definition must cover trigger, local read facade, context, and bot reply/card plans")
	}
	for _, name := range []string{"reader", "context-reader"} {
		if !reflect.DeepEqual(spec.Credentials[name].Scopes, []string{"ChannelMessage.Read.Group"}) {
			t.Fatalf("%s must use team-specific Graph read permission", name)
		}
	}
	if !reflect.DeepEqual(spec.Credentials["bot-writer"].Scopes, []string{"https://api.botframework.com/.default"}) {
		t.Fatal("bot writes must use Bot Connector authorization, not Graph application or migration permissions")
	}
	for name, operation := range spec.SafeOutputs {
		if operation.Credential != "bot-writer" || operation.Request.Endpoint != "bot" || operation.Request.Method != http.MethodPost ||
			operation.Request.Path != "/teams/v3/conversations/{conversation}/activities/{activity}" {
			t.Fatalf("%s must reply through the documented Bot Connector activity endpoint", name)
		}
		if operation.Resources["conversation"] != "conversations" || operation.Resources["activity"] != "activities" {
			t.Fatalf("%s must constrain conversation and activity independently", name)
		}
	}
	for name, operation := range spec.Context {
		if operation.Credential != "context-reader" || operation.Request.Endpoint != "graph" ||
			operation.Request.Method != http.MethodGet || operation.MaxResponseBytes != 65536 {
			t.Fatalf("%s must be a bounded credential-separated Graph context read", name)
		}
	}
	for name, tool := range spec.MCP {
		if tool.Credential != "reader" || tool.Request.Endpoint != "graph" || tool.Request.Method != http.MethodGet {
			t.Fatalf("%s must be a local read-only Graph tool, not an asserted external MCP service", name)
		}
	}
	pageSize, err := json.Marshal(spec.Context["channel-thread-replies"].Request.Query["$top"].Literal)
	if err != nil || string(pageSize) != "20" {
		t.Fatal("thread replies must use a fixed bounded page size")
	}
}

func TestTeamsConnectorInvalidDefinitions(t *testing.T) {
	data, err := os.ReadFile(teamsFixture + "spec.yaml")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		from string
		to   string
		want string
	}{
		{"client-secret-is-not-bearer", "scheme: bearer", "scheme: client-credentials", "unsupported authentication scheme"},
		{"no-token-in-origin", "https://graph.microsoft.com", "https://secret@graph.microsoft.com", "HTTPS origin"},
		{"no-dynamic-service-url", "https://smba.trafficmanager.net", "https://smba.trafficmanager.net/teams", "HTTPS origin"},
		{"no-agent-write-credential", "credential: bot-writer", "credential: reader", "credential declared for that phase"},
		{"no-writer-read-credential", "credential: context-reader", "credential: bot-writer", "credential declared for that phase"},
		{"no-unbounded-context", "maxResponseBytes: 65536", "maxResponseBytes: 0", "maxResponseBytes"},
		{"no-arbitrary-card-object", "maxLength: 4000", "maxLength: 0", "maxLength"},
		{"no-remote-mcp-substitution", "tool: get-channel-message", "tool: '*'", "exact tool name"},
		{"no-unknown-oauth-mode", "kind: ConnectorSpec", "kind: ConnectorSpec\nauthenticationMode: application-graph-send", "unknown field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := strings.Replace(string(data), test.from, test.to, 1)
			if changed == string(data) {
				t.Fatal("negative fixture mutation did not match")
			}
			var spec connectors.ConnectorSpec
			err := connectors.Decode([]byte(changed), &spec)
			if err == nil {
				err = spec.Validate()
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want rejection containing %q", err, test.want)
			}
		})
	}
}

func TestTeamsConnectorNegativeFiles(t *testing.T) {
	tests := map[string]string{
		"invalid-client-secret-auth.yaml": "unsupported authentication scheme",
		"invalid-agent-writer.yaml":       "credential declared for that phase",
		"invalid-graph-write-mode.yaml":   "unknown field",
	}
	for file, want := range tests {
		t.Run(file, func(t *testing.T) {
			_, err := connectors.LoadSpec(teamsFixture + file)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want rejection containing %q", err, want)
			}
		})
	}
}
