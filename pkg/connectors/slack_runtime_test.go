package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const slackThread = "1515449522.000016"

type slackTransport func(*http.Request) (*http.Response, error)

func (transport slackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func slackPlan(t *testing.T) *Plan {
	t.Helper()
	data, err := os.ReadFile("testdata/providers/slack/binding.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var binding Binding
	if err := Decode(data, &binding); err != nil {
		t.Fatal(err)
	}
	plan, err := Lower("../..", binding)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func slackRuntime(t *testing.T, phase string, transport slackTransport) Runtime {
	t.Helper()
	return Runtime{
		Phase: phase, AllowedOrigins: []string{"https://slack.com"},
		Client: &http.Client{Transport: transport}, AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			expected := map[string]string{"agent": "SLACK_READ_TOKEN", "context": "SLACK_CONTEXT_TOKEN", "safe-output": "SLACK_WRITE_TOKEN"}[phase]
			if actualPhase != phase || name != expected {
				t.Fatal("wrong Slack phase credential requested")
			}
			return "fixture-" + phase, nil
		},
	}
}

func slackResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func slackReadBody() string {
	return `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016","text":"untrusted ${{ text }}; $(not-code)"}],"response_metadata":{"next_cursor":""},"excluded":"private metadata"}`
}

func slackArgs() map[string]any {
	return map[string]any{"channel": "C123ABC456", "thread": slackThread}
}

func TestSlackEventProjection(t *testing.T) {
	plan := slackPlan(t)
	data, err := os.ReadFile("testdata/providers/slack/event.json")
	if err != nil {
		t.Fatal(err)
	}
	projection, err := plan.ProjectEvent("threaded-app-mention", "app_mention", data)
	if err != nil || projection.Operational || !projection.Untrusted || projection.Fields["thread-id"] != slackThread {
		t.Fatalf("unexpected Slack projection: %+v %v", projection, err)
	}
	if !errors.Is(plan.ActivateTriggers(), ErrIngressUnavailable) {
		t.Fatal("Slack declaration must not activate unverified ingress")
	}
	rejected, err := os.ReadFile("testdata/providers/slack/event-rejected.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.ProjectEvent("threaded-app-mention", "app_mention", rejected); err == nil {
		t.Fatal("negative channel fixture must fail")
	}
	for _, replacement := range [][2]string{
		{"T123ABC456", "TUNAPPROVED"}, {"A123ABC456", "AUNAPPROVED"},
		{"C123ABC456", "CUNAPPROVED"}, {slackThread, "1515449522.000018"},
		{"app_mention", "message"}, {"event_callback", "url_verification"},
	} {
		payload := strings.ReplaceAll(string(data), replacement[0], replacement[1])
		if _, err := plan.ProjectEvent("threaded-app-mention", "app_mention", []byte(payload)); err == nil {
			t.Fatalf("unapproved event accepted: %v", replacement)
		}
	}
	payload := strings.ReplaceAll(string(data), `"thread_ts": "1515449522.000016"`, `"thread_ts": 1515449522.000016`)
	if _, err := plan.ProjectEvent("threaded-app-mention", "app_mention", []byte(payload)); err == nil {
		t.Fatal("numeric thread evidence must not be accepted as a string ID")
	}
}

func TestSlackBoundedThreadReads(t *testing.T) {
	plan := slackPlan(t)
	for _, capability := range []string{"mcp", "context"} {
		t.Run(capability, func(t *testing.T) {
			phase, name := "agent", "read-thread"
			if capability == "context" {
				phase, name = "context", "thread-context"
			}
			calls := 0
			runtime := slackRuntime(t, phase, func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Host != "slack.com" || request.URL.Path != "/api/conversations.replies" || request.Method != http.MethodGet {
					t.Fatal("read changed fixed Slack endpoint")
				}
				if request.URL.Query().Get("limit") != "15" || request.URL.Query().Get("ts") != slackThread || request.URL.Query().Get("channel") != "C123ABC456" {
					t.Fatal("read changed bounded thread query")
				}
				if request.Header.Get("Authorization") != "Bearer fixture-"+phase {
					t.Fatal("read exposed wrong credential")
				}
				return slackResponse(http.StatusOK, slackReadBody()), nil
			})
			result, err := plan.Execute(t.Context(), runtime, capability, name, slackArgs())
			if err != nil || !result.Untrusted || calls != 1 || strings.Contains(string(result.Data), "excluded") {
				t.Fatalf("bounded read failed: %+v %v calls=%d", result, err, calls)
			}
		})
	}
}

func TestSlackSafeReplyStaging(t *testing.T) {
	plan := slackPlan(t)
	calls := 0
	runtime := slackRuntime(t, "safe-output", func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodPost || request.URL.Host != "slack.com" || request.URL.Path != "/api/chat.postMessage" || request.Header.Get("Authorization") != "Bearer fixture-safe-output" {
			t.Fatal("reply changed endpoint or write credential")
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["channel"] != "C123ABC456" || body["thread_ts"] != slackThread || body["text"] != "${{ unsafe }}; $(not-code)" {
			t.Fatal("reply arguments were not serialized as data")
		}
		for _, field := range []string{"reply_broadcast", "unfurl_links", "unfurl_media", "mrkdwn"} {
			if body[field] != false {
				t.Fatalf("unsafe reply option %s", field)
			}
		}
		return slackResponse(http.StatusOK, `{"ok":true,"channel":"C123ABC456","ts":"1515449599.000001","message":{"thread_ts":"1515449522.000016"},"excluded":"private"}`), nil
	})
	args := slackArgs()
	args["text"] = "${{ unsafe }}; $(not-code)"
	staged, err := plan.PrepareSafeOutput("reply-in-thread", args)
	if err != nil || calls != 0 {
		t.Fatal("staging must not write", err)
	}
	result, err := plan.ExecuteSafeOutput(t.Context(), runtime, *staged)
	if err != nil || calls != 1 || strings.Contains(string(result.Data), "excluded") {
		t.Fatalf("reply execution failed: %+v %v calls=%d", result, err, calls)
	}
	staged.Digest = "tampered"
	if _, err := plan.ExecuteSafeOutput(t.Context(), runtime, *staged); err == nil || calls != 1 {
		t.Fatal("tampered staged reply must fail without a second write")
	}
}

func TestSlackRejectsUnsafeArguments(t *testing.T) {
	plan := slackPlan(t)
	runtime := Runtime{Phase: "agent", AllowedOrigins: []string{"https://slack.com"}, ResolveSecret: func(context.Context, string, string) (string, error) {
		t.Fatal("denied input must not resolve secrets")
		return "", errors.New("unreachable")
	}}
	for _, args := range []map[string]any{
		{}, {"channel": "COTHER", "thread": slackThread},
		{"channel": "C123ABC456", "thread": 1515449522.000016},
		{"channel": "C123ABC456", "thread": "1515449522.000017"},
		{"channel": "C123ABC456", "thread": "not-a-timestamp"},
		{"channel": "C123ABC456", "thread": slackThread, "cursor": "escape"},
	} {
		if _, err := plan.Execute(t.Context(), runtime, "mcp", "read-thread", args); err == nil {
			t.Fatal("unsafe Slack read accepted", args)
		}
	}
	args := slackArgs()
	args["text"] = "reply"
	if _, err := plan.Execute(t.Context(), runtime, "safe-output", "reply-in-thread", args); err == nil {
		t.Fatal("agent must not write")
	}
	args["text"] = strings.Repeat("x", 4001)
	if _, err := plan.PrepareSafeOutput("reply-in-thread", args); err == nil {
		t.Fatal("overlong reply accepted")
	}
}

func TestSlackResponseFailures(t *testing.T) {
	for name, body := range map[string]string{
		"application-error": `{"ok":false,"error":"not_in_channel"}`,
		"missing-ok":        `{"messages":[]}`,
		"wrong-ok-type":     `{"ok":"true"}`,
		"truncated-thread":  `{"ok":true,"has_more":true,"messages":[{"ts":"1515449522.000016"}]}`,
		"wrong-parent":      `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000017"}]}`,
		"oversized":         strings.Repeat("x", 65537),
		"invalid-json":      "not JSON",
	} {
		t.Run(name, func(t *testing.T) {
			plan := slackPlan(t)
			runtime := slackRuntime(t, "context", func(*http.Request) (*http.Response, error) {
				return slackResponse(http.StatusOK, body), nil
			})
			if _, err := plan.Execute(t.Context(), runtime, "context", "thread-context", slackArgs()); err == nil {
				t.Fatal("unsafe Slack context response accepted")
			}
		})
	}
}

func TestSlackWriteFailuresNeverRetry(t *testing.T) {
	for _, failure := range []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{"ok-false", http.StatusOK, `{"ok":false,"error":"internal_error"}`, nil},
		{"missing-id", http.StatusOK, `{"ok":true,"channel":"C123ABC456"}`, nil},
		{"wrong-channel", http.StatusOK, `{"ok":true,"channel":"COTHER","ts":"1515449599.000001","message":{"thread_ts":"1515449522.000016"}}`, nil},
		{"wrong-thread", http.StatusOK, `{"ok":true,"channel":"C123ABC456","ts":"1515449599.000001","message":{"thread_ts":"1515449522.000017"}}`, nil},
		{"rate-limit", http.StatusTooManyRequests, `{"ok":false,"error":"ratelimited"}`, nil},
		{"server-error", http.StatusServiceUnavailable, `{"ok":false}`, nil},
		{"redirect", http.StatusTemporaryRedirect, "", nil},
		{"ambiguous-network", 0, "", errors.New("response lost after send")},
	} {
		t.Run(failure.name, func(t *testing.T) {
			plan, calls := slackPlan(t), 0
			runtime := slackRuntime(t, "safe-output", func(*http.Request) (*http.Response, error) {
				calls++
				if failure.err != nil {
					return nil, failure.err
				}
				response := slackResponse(failure.status, failure.body)
				response.Header.Set("Retry-After", "30")
				response.Header.Set("Location", "https://unapproved.invalid")
				return response, nil
			})
			args := slackArgs()
			args["text"] = "reply"
			if _, err := plan.Execute(t.Context(), runtime, "safe-output", "reply-in-thread", args); err == nil || calls != 1 {
				t.Fatalf("failed write must error without retries: %v calls=%d", err, calls)
			}
		})
	}
}

func TestSlackMCPReadAdapter(t *testing.T) {
	plan := slackPlan(t)
	runtime := slackRuntime(t, "agent", func(*http.Request) (*http.Response, error) {
		return slackResponse(http.StatusOK, slackReadBody()), nil
	})
	server, err := plan.MCPServer(runtime)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	session, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "slack-fixture", Version: "1"}, nil)
	downstream, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = downstream.Close() }()
	result, err := downstream.CallTool(t.Context(), &mcp.CallToolParams{Name: "read-thread", Arguments: slackArgs()})
	if err != nil || result.IsError {
		t.Fatalf("REST-backed Slack MCP read failed: %+v %v", result, err)
	}
	result, err = downstream.CallTool(t.Context(), &mcp.CallToolParams{Name: "reply-in-thread", Arguments: slackArgs()})
	if err == nil && !result.IsError {
		t.Fatal("write operation must not be registered on the read MCP adapter")
	}
}
