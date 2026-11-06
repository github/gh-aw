package connectors_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type teamsTransport func(*http.Request) (*http.Response, error)

func (transport teamsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func teamsPlan(t *testing.T) *connectors.Plan {
	t.Helper()
	var binding connectors.Binding
	if err := connectors.Decode(teamsData(t, "binding.yaml"), &binding); err != nil {
		t.Fatal(err)
	}
	plan, err := connectors.Lower("../..", binding)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func teamsData(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(teamsFixture + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func teamsReadArgs() map[string]any {
	return map[string]any{"team": "11111111-1111-4111-8111-111111111111", "channel": "19:approved-channel@thread.tacv2", "thread": "1730000000000"}
}

func teamsWriteArgs() map[string]any {
	return map[string]any{"conversation": "19:approved-channel@thread.tacv2;messageid=1730000000000", "activity": "1730000000000", "text": "Review ${{ data }}; $(not-code)"}
}

func teamsRuntime(phase string, transport teamsTransport) connectors.Runtime {
	return connectors.Runtime{
		Phase: phase, AllowedOrigins: []string{"https://graph.microsoft.com", "https://smba.trafficmanager.net"},
		Client: &http.Client{Transport: transport}, AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			expected := map[string]string{"agent": "TEAMS_GRAPH_READ_ACCESS_TOKEN", "context": "TEAMS_GRAPH_CONTEXT_ACCESS_TOKEN", "safe-output": "TEAMS_BOT_ACCESS_TOKEN"}
			if actualPhase != phase || name != expected[phase] {
				return "", errors.New("unexpected phase or credential")
			}
			return "fixture-" + phase, nil
		},
	}
}

func teamsResponse(request *http.Request, status int, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: request}
}

func TestTeamsConnectorEventProjection(t *testing.T) {
	plan := teamsPlan(t)
	var batch struct {
		Value []json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(teamsData(t, "notification.json"), &batch); err != nil {
		t.Fatal(err)
	}
	for _, entry := range batch.Value {
		event, err := plan.ProjectEvent("channel-message-created", "created", entry)
		if err != nil || event.Operational || !event.Untrusted || event.Fields["subject"] != "1730000000000" {
			t.Fatalf("unexpected nonoperational fixture projection: %+v, %v", event, err)
		}
		if _, leaked := event.Fields["clientState"]; leaked {
			t.Fatal("notification secret must not enter projected context")
		}
	}
	if !errors.Is(plan.ActivateTriggers(), connectors.ErrIngressUnavailable) {
		t.Fatal("Teams fixture must never activate unauthenticated ingress")
	}
}

func TestTeamsConnectorEventScopeRejection(t *testing.T) {
	plan := teamsPlan(t)
	var batch struct {
		Value []json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(teamsData(t, "notification.json"), &batch); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		strings.Replace(string(batch.Value[0]), "33333333-3333-4333-8333-333333333333", "other-tenant", 1),
		strings.Replace(string(batch.Value[0]), "22222222-2222-4222-8222-222222222222", "other-subscription", 1),
		strings.Replace(string(batch.Value[0]), "approved-channel", "other-channel", 1),
		strings.Replace(string(batch.Value[0]), `"created"`, `"deleted"`, 1),
		strings.Replace(string(batch.Value[0]), "#Microsoft.Graph.chatMessage", "#Microsoft.Graph.chat", 1),
		string(teamsData(t, "notification.json")),
	} {
		if _, err := plan.ProjectEvent("channel-message-created", "created", []byte(value)); err == nil {
			t.Fatal("out-of-scope, wrong-kind, deleted, or unsplit batch evidence accepted")
		}
	}
}

func TestTeamsConnectorProjectionIsNotAuthentication(t *testing.T) {
	plan := teamsPlan(t)
	var batch struct {
		Value []json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(teamsData(t, "notification.json"), &batch); err != nil {
		t.Fatal(err)
	}
	unverified := strings.Replace(string(batch.Value[0]), "fixture-only-not-a-live-secret", "unverified-client-state", 1)
	event, err := plan.ProjectEvent("channel-message-created", "created", []byte(unverified))
	if err != nil || event.Operational || !event.Untrusted {
		t.Fatalf("projection must remain explicitly untrusted, not pretend to verify clientState: %+v %v", event, err)
	}
	if !errors.Is(plan.ActivateTriggers(), connectors.ErrIngressUnavailable) {
		t.Fatal("missing clientState verifier must block operational ingress")
	}
}

func TestTeamsConnectorReadRuntime(t *testing.T) {
	plan := teamsPlan(t)
	for _, phase := range []string{"agent", "context"} {
		t.Run(phase, func(t *testing.T) {
			requests := 0
			runtime := teamsRuntime(phase, func(request *http.Request) (*http.Response, error) {
				requests++
				if request.URL.Host != "graph.microsoft.com" || request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer fixture-"+phase {
					t.Fatal("Graph read used the wrong endpoint, method or phase credential")
				}
				name := "message.json"
				if strings.HasSuffix(request.URL.Path, "/replies") {
					name = "replies.json"
					if request.URL.Query().Get("$top") != "20" {
						t.Fatal("reply read must preserve the fixed page bound")
					}
				}
				return teamsResponse(request, http.StatusOK, teamsData(t, name)), nil
			})
			capability, names := "mcp", []string{"get-channel-message", "list-channel-replies"}
			if phase == "context" {
				capability, names = "context", []string{"channel-thread-root", "channel-thread-replies"}
			}
			for _, name := range names {
				result, err := plan.Execute(t.Context(), runtime, capability, name, teamsReadArgs())
				if err != nil || !result.Untrusted || result.StatusCode != http.StatusOK {
					t.Fatalf("%s: %+v %v", name, result, err)
				}
				if strings.Contains(string(result.Data), "displayName") {
					t.Fatal("root projection retained unselected personal metadata")
				}
			}
			if requests != 2 {
				t.Fatal("reads must not follow pages or retry")
			}
		})
	}
}

func TestTeamsConnectorBotSafeOutputs(t *testing.T) {
	plan := teamsPlan(t)
	for _, name := range []string{"reply-to-activity", "reply-with-review-card"} {
		t.Run(name, func(t *testing.T) {
			requests := 0
			runtime := teamsRuntime("safe-output", func(request *http.Request) (*http.Response, error) {
				requests++
				assertTeamsBotRequest(t, request, name)
				return teamsResponse(request, http.StatusCreated, []byte(`{"id":"fixture-sent-activity","extra":"omit"}`)), nil
			})
			staged, err := plan.PrepareSafeOutput(name, teamsWriteArgs())
			if err != nil || requests != 0 {
				t.Fatal("staging must be credential-free and effect-free", err)
			}
			result, err := plan.ExecuteSafeOutput(t.Context(), runtime, *staged)
			if err != nil || string(result.Data) != `{"id":"fixture-sent-activity"}` || requests != 1 {
				t.Fatalf("unexpected bot result: %+v %v calls=%d", result, err, requests)
			}
		})
	}
}

func assertTeamsBotRequest(t *testing.T, request *http.Request, operation string) {
	t.Helper()
	want := "/teams/v3/conversations/19:approved-channel@thread.tacv2;messageid=1730000000000/activities/1730000000000"
	if request.URL.Host != "smba.trafficmanager.net" || request.URL.Path != want || request.Method != http.MethodPost ||
		request.Header.Get("Authorization") != "Bearer fixture-safe-output" {
		t.Fatal("bot request escaped its fixed service/conversation/activity or used Graph authorization")
	}
	var body map[string]any
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["type"] != "message" || body["replyToId"] != "1730000000000" || body["text"] != teamsWriteArgs()["text"] {
		t.Fatalf("activity fields must remain typed literal data: %+v", body)
	}
	attachments, hasCard := body["attachments"].([]any)
	if hasCard != (operation == "reply-with-review-card") {
		t.Fatal("card presence differs from the selected operation")
	}
	if hasCard {
		card := attachments[0].(map[string]any)
		content := card["content"].(map[string]any)
		if card["contentType"] != "application/vnd.microsoft.card.adaptive" || content["type"] != "AdaptiveCard" || content["actions"] != nil {
			t.Fatal("card must have the reviewed Adaptive Card shape without actions")
		}
	}
}

func TestTeamsConnectorMCPProtocol(t *testing.T) {
	plan := teamsPlan(t)
	calls := 0
	runtime := teamsRuntime("agent", func(request *http.Request) (*http.Response, error) {
		calls++
		return teamsResponse(request, http.StatusOK, teamsData(t, "message.json")), nil
	})
	server, err := plan.MCPServer(runtime)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "teams-fixture", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get-channel-message", Arguments: teamsReadArgs()})
	if err != nil || result.IsError || calls != 1 {
		t.Fatalf("local Graph-backed MCP tool failed: %+v %v calls=%d", result, err, calls)
	}
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "reply-to-activity", Arguments: teamsWriteArgs()})
	if err == nil && !result.IsError {
		t.Fatal("safe outputs must never be exposed as agent read tools")
	}
	if calls != 1 {
		t.Fatal("rejected MCP operation caused a provider request")
	}
}
